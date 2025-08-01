/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package oci

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

// InstanceTypeProvider handles instance type operations
type InstanceTypeProvider struct {
	client          *Client
	pricingProvider *PricingProvider
}

// NewInstanceTypeProvider creates a new instance type provider
func NewInstanceTypeProvider(client *Client, pricingProvider *PricingProvider) *InstanceTypeProvider {
	return &InstanceTypeProvider{
		client:          client,
		pricingProvider: pricingProvider,
	}
}

// GetStaticInstanceTypes returns predefined static instance types
func (p *InstanceTypeProvider) GetStaticInstanceTypes(ctx context.Context, nodePool *v1.NodePool) ([]*cloudprovider.InstanceType, error) {
	shapes, err := p.client.ListShapes(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing shapes: %w", err)
	}

	var instanceTypes []*cloudprovider.InstanceType
	
	for _, shape := range shapes {
		if shape.IsFlexible {
			// For flexible shapes, generate a few standard configurations
			instanceTypes = append(instanceTypes, p.generateFlexibleInstanceTypes(shape)...)
		} else {
			// For fixed shapes, create a single instance type
			instanceType := p.shapeToInstanceType(shape)
			instanceTypes = append(instanceTypes, instanceType)
		}
	}
	
	return instanceTypes, nil
}

// GetDynamicInstanceTypes generates instance types based on pod requirements and NodePool configuration
func (p *InstanceTypeProvider) GetDynamicInstanceTypes(ctx context.Context, nodePool *v1.NodePool, pods []*corev1.Pod) ([]*cloudprovider.InstanceType, error) {
	logger := log.FromContext(ctx)
	
	if nodePool.Spec.DynamicProvisioning == nil || !nodePool.Spec.DynamicProvisioning.Enabled {
		return nil, fmt.Errorf("dynamic provisioning not enabled for NodePool %s", nodePool.Name)
	}
	
	dp := nodePool.Spec.DynamicProvisioning
	
	// If no pods are provided, generate a range of instance types
	if len(pods) == 0 {
		return p.generateInstanceTypeRange(ctx, dp)
	}
	
	// Calculate resource requirements from pods
	totalCPU, totalMemory := p.calculatePodResources(pods)
	
	// Add overhead
	overhead := p.calculateOverhead(dp.Overhead, totalCPU, totalMemory)
	totalCPU.Add(overhead.CPU)
	totalMemory.Add(overhead.Memory)
	
	// Add buffers
	cpuWithBuffer := p.addBuffer(totalCPU, dp.Buffers.CPUHeadroomPercent)
	memoryWithBuffer := p.addBuffer(totalMemory, dp.Buffers.MemoryHeadroomPercent)
	
	// Convert to OCPUs and GB
	ocpus := p.calculateOCPUs(cpuWithBuffer)
	memoryGB := p.calculateMemoryGB(memoryWithBuffer)
	
	// Apply constraints
	ocpus = max(ocpus, dp.Constraints.MinOCPUs)
	ocpus = min(ocpus, dp.Constraints.MaxOCPUs)
	memoryGB = max(memoryGB, dp.Constraints.MinMemoryGB)
	memoryGB = min(memoryGB, dp.Constraints.MaxMemoryGB)
	
	logger.Info("calculated dynamic shape requirements",
		"pods", len(pods),
		"ocpus", ocpus,
		"memoryGB", memoryGB,
		"strategy", dp.Strategy)
	
	// Generate instance types based on strategy
	switch dp.Strategy {
	case v1.StrategyExactFit:
		return p.generateExactFitInstanceTypes(ctx, dp, ocpus, memoryGB)
	case v1.StrategyBestFit:
		return p.generateBestFitInstanceTypes(ctx, dp, ocpus, memoryGB)
	case v1.StrategyCostOptimized:
		return p.generateCostOptimizedInstanceTypes(ctx, dp, ocpus, memoryGB)
	default:
		return p.generateCostOptimizedInstanceTypes(ctx, dp, ocpus, memoryGB)
	}
}

// Helper methods

func (p *InstanceTypeProvider) shapeToInstanceType(shape *Shape) *cloudprovider.InstanceType {
	// Build requirements
	requirements := scheduling.NewRequirements(
		scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, shape.Name),
		scheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, "amd64"),
		scheduling.NewRequirement("oci.oraclecloud.com/instance-category", corev1.NodeSelectorOpIn, "general-purpose"),
	)
	
	// Calculate capacity
	capacity := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewQuantity(int64(shape.OCPUs*2), resource.DecimalSI), // 1 OCPU = 2 vCPUs
		corev1.ResourceMemory: *resource.NewQuantity(int64(shape.MemoryInGBs*1024*1024*1024), resource.BinarySI),
		corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),
		corev1.ResourceEphemeralStorage: *resource.NewQuantity(100*1024*1024*1024, resource.BinarySI),
	}
	
	// Create offerings
	offerings := p.createOfferings(shape.Name, shape.OCPUs, shape.MemoryInGBs, false)
	
	// Calculate overhead
	overhead := &cloudprovider.InstanceTypeOverhead{
		KubeReserved: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewMilliQuantity(100, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewQuantity(500*1024*1024, resource.BinarySI),
		},
		SystemReserved: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewMilliQuantity(100, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewQuantity(500*1024*1024, resource.BinarySI),
		},
		EvictionThreshold: corev1.ResourceList{
			corev1.ResourceMemory: *resource.NewQuantity(100*1024*1024, resource.BinarySI),
		},
	}
	
	return &cloudprovider.InstanceType{
		Name:         shape.Name,
		Requirements: requirements,
		Offerings:    offerings,
		Capacity:     capacity,
		Overhead:     overhead,
	}
}

func (p *InstanceTypeProvider) calculatePodResources(pods []*corev1.Pod) (resource.Quantity, resource.Quantity) {
	totalCPU := resource.Quantity{}
	totalMemory := resource.Quantity{}
	
	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			if cpu := container.Resources.Requests.Cpu(); cpu != nil {
				totalCPU.Add(*cpu)
			}
			if memory := container.Resources.Requests.Memory(); memory != nil {
				totalMemory.Add(*memory)
			}
		}
	}
	
	return totalCPU, totalMemory
}

func (p *InstanceTypeProvider) calculateOverhead(overhead *v1.SystemOverhead, cpu, memory resource.Quantity) struct{ CPU, Memory resource.Quantity } {
	result := struct{ CPU, Memory resource.Quantity }{}
	
	if overhead == nil {
		// Default overhead
		result.CPU = *resource.NewMilliQuantity(300, resource.DecimalSI)     // 300m
		result.Memory = *resource.NewQuantity(2*1024*1024*1024, resource.BinarySI) // 2Gi
		return result
	}
	
	// Parse overhead values
	if overhead.SystemReservedCPU != "" {
		if q, err := resource.ParseQuantity(overhead.SystemReservedCPU); err == nil {
			result.CPU.Add(q)
		}
	}
	if overhead.KubeletReservedCPU != "" {
		if q, err := resource.ParseQuantity(overhead.KubeletReservedCPU); err == nil {
			result.CPU.Add(q)
		}
	}
	if overhead.SystemReservedMemory != "" {
		if q, err := resource.ParseQuantity(overhead.SystemReservedMemory); err == nil {
			result.Memory.Add(q)
		}
	}
	if overhead.KubeletReservedMemory != "" {
		if q, err := resource.ParseQuantity(overhead.KubeletReservedMemory); err == nil {
			result.Memory.Add(q)
		}
	}
	
	return result
}

func (p *InstanceTypeProvider) addBuffer(quantity resource.Quantity, bufferPercent int32) resource.Quantity {
	if bufferPercent <= 0 {
		return quantity
	}
	
	// Calculate buffer amount
	bufferAmount := quantity.DeepCopy()
	bufferAmount.Set(quantity.Value() * int64(bufferPercent) / 100)
	
	result := quantity.DeepCopy()
	result.Add(bufferAmount)
	return result
}

func (p *InstanceTypeProvider) calculateOCPUs(cpu resource.Quantity) int32 {
	// Convert CPU to OCPUs (1 OCPU = 2 vCPUs)
	vcpus := float64(cpu.MilliValue()) / 1000.0
	ocpus := int32((vcpus + 1) / 2) // Round up
	
	if ocpus < 1 {
		ocpus = 1
	}
	
	return ocpus
}

func (p *InstanceTypeProvider) calculateMemoryGB(memory resource.Quantity) int32 {
	// Convert memory to GB
	bytes := memory.Value()
	gb := int32((bytes + (1024*1024*1024 - 1)) / (1024 * 1024 * 1024)) // Round up
	
	if gb < 1 {
		gb = 1
	}
	
	return gb
}

func (p *InstanceTypeProvider) createOfferings(shape string, ocpus float32, memoryGB float32, isFlexible bool) []*cloudprovider.Offering {
	var offerings []*cloudprovider.Offering
	
	// On-demand offering
	onDemandPrice, _ := p.pricingProvider.GetShapePrice(context.Background(), shape, int32(ocpus), int32(memoryGB), false)
	offerings = append(offerings, &cloudprovider.Offering{
		Requirements: scheduling.NewRequirements(
			scheduling.NewRequirement(v1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, v1.CapacityTypeOnDemand),
			scheduling.NewRequirement(corev1.LabelTopologyZone, corev1.NodeSelectorOpIn, "zone-1", "zone-2", "zone-3"),
		),
		Price:     onDemandPrice,
		Available: true,
	})
	
	// Preemptible offering
	preemptiblePrice, _ := p.pricingProvider.GetShapePrice(context.Background(), shape, int32(ocpus), int32(memoryGB), true)
	offerings = append(offerings, &cloudprovider.Offering{
		Requirements: scheduling.NewRequirements(
			scheduling.NewRequirement(v1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, "preemptible"),
			scheduling.NewRequirement(corev1.LabelTopologyZone, corev1.NodeSelectorOpIn, "zone-1", "zone-2", "zone-3"),
		),
		Price:     preemptiblePrice,
		Available: true,
	})
	
	return offerings
}

func (p *InstanceTypeProvider) generateExactFitInstanceTypes(ctx context.Context, dp *v1.DynamicProvisioning, ocpus, memoryGB int32) ([]*cloudprovider.InstanceType, error) {
	var instanceTypes []*cloudprovider.InstanceType
	
	for _, shape := range dp.Constraints.AllowedShapes {
		// Create exact-fit instance type
		instanceName := fmt.Sprintf("%s-%d-%d", shape, ocpus, memoryGB)
		
		requirements := scheduling.NewRequirements(
			scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, instanceName),
			scheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, "amd64"),
			scheduling.NewRequirement("oci.oraclecloud.com/instance-category", corev1.NodeSelectorOpIn, "flex"),
			scheduling.NewRequirement("oci.oraclecloud.com/shape", corev1.NodeSelectorOpIn, shape),
		)
		
		capacity := corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewQuantity(int64(ocpus)*2000, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewQuantity(int64(memoryGB)*1024*1024*1024, resource.BinarySI),
			corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),
			corev1.ResourceEphemeralStorage: *resource.NewQuantity(100*1024*1024*1024, resource.BinarySI),
		}
		
		offerings := p.createOfferings(shape, float32(ocpus), float32(memoryGB), true)
		
		overhead := &cloudprovider.InstanceTypeOverhead{
			KubeReserved:      p.getReservedResources(dp.Overhead, "kubelet"),
			SystemReserved:    p.getReservedResources(dp.Overhead, "system"),
			EvictionThreshold: p.getReservedResources(dp.Overhead, "eviction"),
		}
		
		instanceTypes = append(instanceTypes, &cloudprovider.InstanceType{
			Name:         instanceName,
			Requirements: requirements,
			Offerings:    offerings,
			Capacity:     capacity,
			Overhead:     overhead,
		})
	}
	
	return instanceTypes, nil
}

func (p *InstanceTypeProvider) generateBestFitInstanceTypes(ctx context.Context, dp *v1.DynamicProvisioning, ocpus, memoryGB int32) ([]*cloudprovider.InstanceType, error) {
	// Round up to efficient configurations
	// Common OCPU configurations: 1, 2, 4, 8, 16, 32, 64
	ocpuOptions := []int32{1, 2, 4, 8, 16, 32, 64}
	bestOCPUs := int32(1)
	for _, opt := range ocpuOptions {
		if opt >= ocpus && opt <= dp.Constraints.MaxOCPUs {
			bestOCPUs = opt
			break
		}
	}
	
	// Round memory to 1:8 or 1:16 ratio
	bestMemory := bestOCPUs * 8 // Default 1:8 ratio
	if memoryGB > bestMemory {
		bestMemory = bestOCPUs * 16 // Use 1:16 ratio if needed
	}
	
	// Ensure within constraints
	bestMemory = min(bestMemory, dp.Constraints.MaxMemoryGB)
	bestMemory = max(bestMemory, memoryGB)
	
	return p.generateExactFitInstanceTypes(ctx, dp, bestOCPUs, bestMemory)
}

func (p *InstanceTypeProvider) generateCostOptimizedInstanceTypes(ctx context.Context, dp *v1.DynamicProvisioning, minOCPUs, minMemoryGB int32) ([]*cloudprovider.InstanceType, error) {
	// Generate multiple options for the scheduler to choose from
	var instanceTypes []*cloudprovider.InstanceType
	
	// Configuration options to explore
	ocpuOptions := []int32{minOCPUs}
	if minOCPUs < 2 {
		ocpuOptions = append(ocpuOptions, 2)
	}
	if minOCPUs < 4 {
		ocpuOptions = append(ocpuOptions, 4)
	}
	if minOCPUs < 8 {
		ocpuOptions = append(ocpuOptions, 8)
	}
	
	for _, shape := range dp.Constraints.AllowedShapes {
		for _, ocpus := range ocpuOptions {
			if ocpus > dp.Constraints.MaxOCPUs {
				continue
			}
			
			// Try different memory ratios
			for _, ratio := range []int32{8, 16} {
				memoryGB := max(minMemoryGB, ocpus*ratio)
				if memoryGB > dp.Constraints.MaxMemoryGB {
					continue
				}
				
				instanceName := fmt.Sprintf("%s-%d-%d", shape, ocpus, memoryGB)
				
				requirements := scheduling.NewRequirements(
					scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, instanceName),
					scheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, "amd64"),
					scheduling.NewRequirement("oci.oraclecloud.com/instance-category", corev1.NodeSelectorOpIn, "flex"),
					scheduling.NewRequirement("oci.oraclecloud.com/shape", corev1.NodeSelectorOpIn, shape),
				)
				
				capacity := corev1.ResourceList{
					corev1.ResourceCPU:    *resource.NewQuantity(int64(ocpus)*2000, resource.DecimalSI),
					corev1.ResourceMemory: *resource.NewQuantity(int64(memoryGB)*1024*1024*1024, resource.BinarySI),
					corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),
					corev1.ResourceEphemeralStorage: *resource.NewQuantity(100*1024*1024*1024, resource.BinarySI),
				}
				
				offerings := p.createOfferings(shape, float32(ocpus), float32(memoryGB), true)
				
				overhead := &cloudprovider.InstanceTypeOverhead{
					KubeReserved:      p.getReservedResources(dp.Overhead, "kubelet"),
					SystemReserved:    p.getReservedResources(dp.Overhead, "system"),
					EvictionThreshold: p.getReservedResources(dp.Overhead, "eviction"),
				}
				
				instanceTypes = append(instanceTypes, &cloudprovider.InstanceType{
					Name:         instanceName,
					Requirements: requirements,
					Offerings:    offerings,
					Capacity:     capacity,
					Overhead:     overhead,
				})
			}
		}
	}
	
	// Sort by price (offerings are already sorted by price in OrderByPrice)
	return instanceTypes, nil
}

func (p *InstanceTypeProvider) generateInstanceTypeRange(ctx context.Context, dp *v1.DynamicProvisioning) ([]*cloudprovider.InstanceType, error) {
	// Generate a range of instance types when no specific pods are provided
	// This is useful for general capacity provisioning
	
	var instanceTypes []*cloudprovider.InstanceType
	
	// Common configurations
	configs := []struct {
		ocpus    int32
		memoryGB int32
	}{
		{1, 8},
		{2, 16},
		{4, 32},
		{8, 64},
		{16, 128},
		{32, 256},
	}
	
	for _, shape := range dp.Constraints.AllowedShapes {
		for _, config := range configs {
			if config.ocpus > dp.Constraints.MaxOCPUs || config.memoryGB > dp.Constraints.MaxMemoryGB {
				continue
			}
			if config.ocpus < dp.Constraints.MinOCPUs || config.memoryGB < dp.Constraints.MinMemoryGB {
				continue
			}
			
			instanceTypes = append(instanceTypes, p.generateSingleInstanceType(dp, shape, config.ocpus, config.memoryGB))
		}
	}
	
	return instanceTypes, nil
}

func (p *InstanceTypeProvider) generateSingleInstanceType(dp *v1.DynamicProvisioning, shape string, ocpus, memoryGB int32) *cloudprovider.InstanceType {
	instanceName := fmt.Sprintf("%s-%d-%d", shape, ocpus, memoryGB)
	
	requirements := scheduling.NewRequirements(
		scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, instanceName),
		scheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, "amd64"),
		scheduling.NewRequirement("oci.oraclecloud.com/instance-category", corev1.NodeSelectorOpIn, "flex"),
		scheduling.NewRequirement("oci.oraclecloud.com/shape", corev1.NodeSelectorOpIn, shape),
	)
	
	capacity := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewQuantity(int64(ocpus)*2000, resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(int64(memoryGB)*1024*1024*1024, resource.BinarySI),
		corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),
		corev1.ResourceEphemeralStorage: *resource.NewQuantity(100*1024*1024*1024, resource.BinarySI),
	}
	
	offerings := p.createOfferings(shape, float32(ocpus), float32(memoryGB), true)
	
	overhead := &cloudprovider.InstanceTypeOverhead{
		KubeReserved:      p.getReservedResources(dp.Overhead, "kubelet"),
		SystemReserved:    p.getReservedResources(dp.Overhead, "system"),
		EvictionThreshold: p.getReservedResources(dp.Overhead, "eviction"),
	}
	
	return &cloudprovider.InstanceType{
		Name:         instanceName,
		Requirements: requirements,
		Offerings:    offerings,
		Capacity:     capacity,
		Overhead:     overhead,
	}
}

func (p *InstanceTypeProvider) getReservedResources(overhead *v1.SystemOverhead, resourceType string) corev1.ResourceList {
	result := corev1.ResourceList{}
	
	if overhead == nil {
		// Default reservations
		switch resourceType {
		case "kubelet":
			result[corev1.ResourceCPU] = *resource.NewMilliQuantity(200, resource.DecimalSI)
			result[corev1.ResourceMemory] = *resource.NewQuantity(1*1024*1024*1024, resource.BinarySI)
		case "system":
			result[corev1.ResourceCPU] = *resource.NewMilliQuantity(100, resource.DecimalSI)
			result[corev1.ResourceMemory] = *resource.NewQuantity(500*1024*1024, resource.BinarySI)
		case "eviction":
			result[corev1.ResourceMemory] = *resource.NewQuantity(500*1024*1024, resource.BinarySI)
		}
		return result
	}
	
	// Parse configured reservations
	switch resourceType {
	case "kubelet":
		if overhead.KubeletReservedCPU != "" {
			if q, err := resource.ParseQuantity(overhead.KubeletReservedCPU); err == nil {
				result[corev1.ResourceCPU] = q
			}
		}
		if overhead.KubeletReservedMemory != "" {
			if q, err := resource.ParseQuantity(overhead.KubeletReservedMemory); err == nil {
				result[corev1.ResourceMemory] = q
			}
		}
	case "system":
		if overhead.SystemReservedCPU != "" {
			if q, err := resource.ParseQuantity(overhead.SystemReservedCPU); err == nil {
				result[corev1.ResourceCPU] = q
			}
		}
		if overhead.SystemReservedMemory != "" {
			if q, err := resource.ParseQuantity(overhead.SystemReservedMemory); err == nil {
				result[corev1.ResourceMemory] = q
			}
		}
	case "eviction":
		if overhead.EvictionThresholdCPU != "" {
			if q, err := resource.ParseQuantity(overhead.EvictionThresholdCPU); err == nil {
				result[corev1.ResourceCPU] = q
			}
		}
		if overhead.EvictionThresholdMemory != "" {
			if q, err := resource.ParseQuantity(overhead.EvictionThresholdMemory); err == nil {
				result[corev1.ResourceMemory] = q
			}
		}
	}
	
	return result
}

// generateFlexibleInstanceTypes generates common configurations for flexible shapes
func (p *InstanceTypeProvider) generateFlexibleInstanceTypes(shape *Shape) []*cloudprovider.InstanceType {
	var instanceTypes []*cloudprovider.InstanceType
	
	// Common flexible shape configurations
	configs := []struct {
		ocpus    int32
		memoryGB int32
	}{
		{1, 16},   // 1 OCPU, 16GB RAM
		{2, 32},   // 2 OCPUs, 32GB RAM
		{4, 64},   // 4 OCPUs, 64GB RAM
		{8, 128},  // 8 OCPUs, 128GB RAM
		{16, 256}, // 16 OCPUs, 256GB RAM
	}
	
	for _, config := range configs {
		// Skip configurations that exceed shape limits
		if shape.OCPUOptions != nil && (float32(config.ocpus) > shape.OCPUOptions.Max || float32(config.ocpus) < shape.OCPUOptions.Min) {
			continue
		}
		if shape.MemoryOptions != nil && (float32(config.memoryGB) > shape.MemoryOptions.MaxInGBs || float32(config.memoryGB) < shape.MemoryOptions.MinInGBs) {
			continue
		}
		
		// Create instance type name with configuration details
		instanceName := fmt.Sprintf("%s-%d-%d", shape.Name, config.ocpus, config.memoryGB)
		
		requirements := scheduling.NewRequirements(
			scheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, instanceName),
			scheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, "amd64"),
			scheduling.NewRequirement("oci.oraclecloud.com/instance-category", corev1.NodeSelectorOpIn, "flex"),
			scheduling.NewRequirement("node.kubernetes.io/instance-type", corev1.NodeSelectorOpIn, shape.Name, instanceName),
			scheduling.NewRequirement("oci.oraclecloud.com/shape", corev1.NodeSelectorOpIn, shape.Name),
		)
		
		capacity := corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewQuantity(int64(config.ocpus)*2000, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewQuantity(int64(config.memoryGB)*1024*1024*1024, resource.BinarySI),
			corev1.ResourcePods:   *resource.NewQuantity(110, resource.DecimalSI),
			corev1.ResourceEphemeralStorage: *resource.NewQuantity(100*1024*1024*1024, resource.BinarySI),
		}
		
		offerings := p.createOfferings(shape.Name, float32(config.ocpus), float32(config.memoryGB), true)
		
		overhead := &cloudprovider.InstanceTypeOverhead{
			KubeReserved: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(100, resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewQuantity(500*1024*1024, resource.BinarySI),
			},
			SystemReserved: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewMilliQuantity(100, resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewQuantity(500*1024*1024, resource.BinarySI),
			},
			EvictionThreshold: corev1.ResourceList{
				corev1.ResourceMemory: *resource.NewQuantity(100*1024*1024, resource.BinarySI),
			},
		}
		
		instanceTypes = append(instanceTypes, &cloudprovider.InstanceType{
			Name:         instanceName,
			Requirements: requirements,
			Offerings:    offerings,
			Capacity:     capacity,
			Overhead:     overhead,
		})
	}
	
	return instanceTypes
}