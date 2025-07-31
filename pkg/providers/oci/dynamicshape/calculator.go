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

package dynamicshape

import (
	"context"
	"fmt"
	"math"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// Calculator calculates optimal shape configurations for dynamic provisioning
type Calculator struct {
	constraints *v1.DynamicConstraints
	overhead    *v1.SystemOverhead
	buffers     *v1.ResourceBuffers
}

// NewCalculator creates a new dynamic shape calculator
func NewCalculator(constraints *v1.DynamicConstraints, overhead *v1.SystemOverhead, buffers *v1.ResourceBuffers) *Calculator {
	return &Calculator{
		constraints: constraints,
		overhead:    overhead,
		buffers:     buffers,
	}
}

// ShapeRecommendation represents a recommended shape configuration
type ShapeRecommendation struct {
	// Shape family (e.g., VM.Standard.E4.Flex)
	Shape string
	// Number of OCPUs
	OCPUs int32
	// Memory in GB
	MemoryGB int32
	// Whether this should be a preemptible instance
	IsPreemptible bool
	// Efficiency score (0-100)
	EfficiencyScore float64
	// Estimated hourly cost
	EstimatedCost float64
	// Resource breakdown
	Resources ResourceBreakdown
}

// ResourceBreakdown shows how resources are allocated
type ResourceBreakdown struct {
	// Pod requirements
	PodCPU    resource.Quantity
	PodMemory resource.Quantity
	// System overhead
	OverheadCPU    resource.Quantity
	OverheadMemory resource.Quantity
	// Buffer amounts
	BufferCPU    resource.Quantity
	BufferMemory resource.Quantity
	// Total provisioned
	TotalCPU    resource.Quantity
	TotalMemory resource.Quantity
}

// CalculateShapeForPods calculates the optimal shape for a set of pods
func (c *Calculator) CalculateShapeForPods(ctx context.Context, pods []*corev1.Pod, strategy v1.ProvisioningStrategy) (*ShapeRecommendation, error) {
	logger := log.FromContext(ctx)
	
	if len(pods) == 0 {
		return nil, fmt.Errorf("no pods provided")
	}
	
	// Calculate total pod resources
	totalCPU, totalMemory := c.calculatePodResources(pods)
	
	logger.V(1).Info("calculated pod resources",
		"pods", len(pods),
		"totalCPU", totalCPU.String(),
		"totalMemory", totalMemory.String())
	
	// Calculate overhead
	overheadCPU, overheadMemory := c.calculateOverhead(totalCPU, totalMemory)
	
	// Calculate with buffers
	cpuWithBuffer, bufferCPU := c.applyBuffer(totalCPU.DeepCopy().Add(overheadCPU), c.buffers.CPUHeadroomPercent)
	memoryWithBuffer, bufferMemory := c.applyBuffer(totalMemory.DeepCopy().Add(overheadMemory), c.buffers.MemoryHeadroomPercent)
	
	// Convert to OCPUs and GB
	ocpus := c.calculateOCPUs(cpuWithBuffer)
	memoryGB := c.calculateMemoryGB(memoryWithBuffer)
	
	// Apply strategy-specific adjustments
	ocpus, memoryGB = c.applyStrategy(ocpus, memoryGB, strategy)
	
	// Apply constraints
	ocpus = c.applyOCPUConstraints(ocpus)
	memoryGB = c.applyMemoryConstraints(memoryGB)
	
	// Select best shape family
	shape := c.selectOptimalShape(ocpus, memoryGB)
	
	// Calculate efficiency
	efficiency := c.calculateEfficiency(totalCPU, totalMemory, ocpus, memoryGB)
	
	recommendation := &ShapeRecommendation{
		Shape:           shape,
		OCPUs:           ocpus,
		MemoryGB:        memoryGB,
		EfficiencyScore: efficiency,
		Resources: ResourceBreakdown{
			PodCPU:         totalCPU,
			PodMemory:      totalMemory,
			OverheadCPU:    overheadCPU,
			OverheadMemory: overheadMemory,
			BufferCPU:      bufferCPU,
			BufferMemory:   bufferMemory,
			TotalCPU:       *resource.NewQuantity(int64(ocpus)*2000, resource.DecimalSI),
			TotalMemory:    *resource.NewQuantity(int64(memoryGB)*1024*1024*1024, resource.BinarySI),
		},
	}
	
	logger.Info("calculated shape recommendation",
		"shape", shape,
		"ocpus", ocpus,
		"memoryGB", memoryGB,
		"efficiency", efficiency,
		"strategy", strategy)
	
	return recommendation, nil
}

// CalculateShapeForResources calculates the optimal shape for specific resource requirements
func (c *Calculator) CalculateShapeForResources(ctx context.Context, cpu, memory resource.Quantity, strategy v1.ProvisioningStrategy) (*ShapeRecommendation, error) {
	// Create a virtual pod with the given resources
	virtualPod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    cpu,
							corev1.ResourceMemory: memory,
						},
					},
				},
			},
		},
	}
	
	return c.CalculateShapeForPods(ctx, []*corev1.Pod{virtualPod}, strategy)
}

// GetShapeVariations returns multiple shape options for comparison
func (c *Calculator) GetShapeVariations(ctx context.Context, baseCPU, baseMemory resource.Quantity) ([]*ShapeRecommendation, error) {
	var recommendations []*ShapeRecommendation
	
	// Try different strategies
	strategies := []v1.ProvisioningStrategy{
		v1.StrategyExactFit,
		v1.StrategyBestFit,
		v1.StrategyCostOptimized,
	}
	
	for _, strategy := range strategies {
		rec, err := c.CalculateShapeForResources(ctx, baseCPU, baseMemory, strategy)
		if err != nil {
			continue
		}
		recommendations = append(recommendations, rec)
	}
	
	// Try different memory ratios
	ocpus := c.calculateOCPUs(baseCPU)
	for _, ratio := range []int32{8, 16} {
		memoryGB := ocpus * ratio
		if memoryGB >= c.constraints.MinMemoryGB && memoryGB <= c.constraints.MaxMemoryGB {
			rec := &ShapeRecommendation{
				Shape:           c.selectOptimalShape(ocpus, memoryGB),
				OCPUs:           ocpus,
				MemoryGB:        memoryGB,
				EfficiencyScore: c.calculateEfficiency(baseCPU, baseMemory, ocpus, memoryGB),
			}
			recommendations = append(recommendations, rec)
		}
	}
	
	return recommendations, nil
}

// Helper methods

func (c *Calculator) calculatePodResources(pods []*corev1.Pod) (cpu, memory resource.Quantity) {
	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			if cpuReq := container.Resources.Requests.Cpu(); cpuReq != nil {
				cpu.Add(*cpuReq)
			}
			if memReq := container.Resources.Requests.Memory(); memReq != nil {
				memory.Add(*memReq)
			}
		}
		// Include init containers
		for _, container := range pod.Spec.InitContainers {
			if cpuReq := container.Resources.Requests.Cpu(); cpuReq != nil {
				// For init containers, we need max, not sum
				if cpuReq.Cmp(cpu) > 0 {
					cpu = *cpuReq
				}
			}
			if memReq := container.Resources.Requests.Memory(); memReq != nil {
				if memReq.Cmp(memory) > 0 {
					memory = *memReq
				}
			}
		}
	}
	return cpu, memory
}

func (c *Calculator) calculateOverhead(cpu, memory resource.Quantity) (overheadCPU, overheadMemory resource.Quantity) {
	// Default overhead values
	defaultCPUOverhead := resource.NewMilliQuantity(300, resource.DecimalSI)         // 300m
	defaultMemoryOverhead := resource.NewQuantity(2*1024*1024*1024, resource.BinarySI) // 2Gi
	
	if c.overhead == nil {
		return *defaultCPUOverhead, *defaultMemoryOverhead
	}
	
	// System reserved
	if c.overhead.SystemReservedCPU != "" {
		if q, err := resource.ParseQuantity(c.overhead.SystemReservedCPU); err == nil {
			overheadCPU.Add(q)
		}
	} else {
		overheadCPU.Add(*resource.NewMilliQuantity(200, resource.DecimalSI))
	}
	
	if c.overhead.SystemReservedMemory != "" {
		if q, err := resource.ParseQuantity(c.overhead.SystemReservedMemory); err == nil {
			overheadMemory.Add(q)
		}
	} else {
		overheadMemory.Add(*resource.NewQuantity(1*1024*1024*1024, resource.BinarySI))
	}
	
	// Kubelet reserved
	if c.overhead.KubeletReservedCPU != "" {
		if q, err := resource.ParseQuantity(c.overhead.KubeletReservedCPU); err == nil {
			overheadCPU.Add(q)
		}
	} else {
		overheadCPU.Add(*resource.NewMilliQuantity(100, resource.DecimalSI))
	}
	
	if c.overhead.KubeletReservedMemory != "" {
		if q, err := resource.ParseQuantity(c.overhead.KubeletReservedMemory); err == nil {
			overheadMemory.Add(q)
		}
	} else {
		overheadMemory.Add(*resource.NewQuantity(1*1024*1024*1024, resource.BinarySI))
	}
	
	// Eviction threshold (only memory)
	if c.overhead.EvictionThresholdMemory != "" {
		if q, err := resource.ParseQuantity(c.overhead.EvictionThresholdMemory); err == nil {
			overheadMemory.Add(q)
		}
	}
	
	return overheadCPU, overheadMemory
}

func (c *Calculator) applyBuffer(quantity resource.Quantity, bufferPercent int32) (withBuffer, bufferAmount resource.Quantity) {
	if c.buffers == nil || bufferPercent <= 0 {
		return quantity, resource.Quantity{}
	}
	
	bufferAmount = quantity.DeepCopy()
	bufferAmount.Set(quantity.Value() * int64(bufferPercent) / 100)
	
	withBuffer = quantity.DeepCopy()
	withBuffer.Add(bufferAmount)
	
	return withBuffer, bufferAmount
}

func (c *Calculator) calculateOCPUs(cpu resource.Quantity) int32 {
	// Convert CPU to OCPUs (1 OCPU = 2 vCPUs = 2000m)
	milliCPU := cpu.MilliValue()
	ocpus := float64(milliCPU) / 2000.0
	
	// Round up to nearest integer
	return int32(math.Ceil(ocpus))
}

func (c *Calculator) calculateMemoryGB(memory resource.Quantity) int32 {
	// Convert memory to GB
	bytes := memory.Value()
	gb := float64(bytes) / (1024 * 1024 * 1024)
	
	// Round up to nearest integer
	return int32(math.Ceil(gb))
}

func (c *Calculator) applyStrategy(ocpus, memoryGB int32, strategy v1.ProvisioningStrategy) (int32, int32) {
	switch strategy {
	case v1.StrategyExactFit:
		// No adjustment for exact fit
		return ocpus, memoryGB
		
	case v1.StrategyBestFit:
		// Round to efficient configurations
		ocpus = c.roundToEfficientOCPUs(ocpus)
		memoryGB = c.roundToEfficientMemory(ocpus, memoryGB)
		return ocpus, memoryGB
		
	case v1.StrategyCostOptimized:
		// Find the most cost-effective configuration
		return c.optimizeForCost(ocpus, memoryGB)
		
	default:
		return ocpus, memoryGB
	}
}

func (c *Calculator) roundToEfficientOCPUs(ocpus int32) int32 {
	// Common efficient OCPU configurations
	efficientConfigs := []int32{1, 2, 4, 8, 16, 32, 64}
	
	for _, config := range efficientConfigs {
		if config >= ocpus && config <= c.constraints.MaxOCPUs {
			return config
		}
	}
	
	return ocpus
}

func (c *Calculator) roundToEfficientMemory(ocpus, memoryGB int32) int32 {
	// Common memory ratios: 1:8 or 1:16
	ratio8 := ocpus * 8
	ratio16 := ocpus * 16
	
	// If memory requirement is close to 1:8, use that
	if memoryGB <= ratio8 {
		return max(memoryGB, ratio8)
	}
	
	// Otherwise use 1:16
	return min(ratio16, c.constraints.MaxMemoryGB)
}

func (c *Calculator) optimizeForCost(minOCPUs, minMemoryGB int32) (int32, int32) {
	// For cost optimization, we prefer slightly larger but more efficient configurations
	// This reduces the number of nodes needed
	
	ocpus := c.roundToEfficientOCPUs(minOCPUs)
	
	// Use 1:8 ratio unless more memory is specifically needed
	memoryGB := max(minMemoryGB, ocpus*8)
	
	// Ensure within constraints
	memoryGB = min(memoryGB, c.constraints.MaxMemoryGB)
	
	return ocpus, memoryGB
}

func (c *Calculator) applyOCPUConstraints(ocpus int32) int32 {
	if ocpus < c.constraints.MinOCPUs {
		return c.constraints.MinOCPUs
	}
	if ocpus > c.constraints.MaxOCPUs {
		return c.constraints.MaxOCPUs
	}
	return ocpus
}

func (c *Calculator) applyMemoryConstraints(memoryGB int32) int32 {
	if memoryGB < c.constraints.MinMemoryGB {
		return c.constraints.MinMemoryGB
	}
	if memoryGB > c.constraints.MaxMemoryGB {
		return c.constraints.MaxMemoryGB
	}
	return memoryGB
}

func (c *Calculator) selectOptimalShape(ocpus, memoryGB int32) string {
	if len(c.constraints.AllowedShapes) == 0 {
		return "VM.Standard.E4.Flex"
	}
	
	// In a real implementation, this would consider:
	// - Shape availability in the region
	// - Shape capabilities (max OCPUs, memory, network bandwidth)
	// - Shape pricing
	// - Shape performance characteristics
	
	// For now, select based on size
	if ocpus > 32 || memoryGB > 256 {
		// Prefer E5 for larger instances
		for _, shape := range c.constraints.AllowedShapes {
			if shape == "VM.Standard.E5.Flex" {
				return shape
			}
		}
	}
	
	// Default to first allowed shape
	return c.constraints.AllowedShapes[0]
}

func (c *Calculator) calculateEfficiency(requestedCPU, requestedMemory resource.Quantity, provisionedOCPUs, provisionedMemoryGB int32) float64 {
	// Calculate CPU efficiency
	requestedMilliCPU := requestedCPU.MilliValue()
	provisionedMilliCPU := int64(provisionedOCPUs) * 2000
	cpuEfficiency := float64(requestedMilliCPU) / float64(provisionedMilliCPU)
	
	// Calculate memory efficiency
	requestedBytes := requestedMemory.Value()
	provisionedBytes := int64(provisionedMemoryGB) * 1024 * 1024 * 1024
	memoryEfficiency := float64(requestedBytes) / float64(provisionedBytes)
	
	// Average efficiency (weighted)
	// CPU typically more important for efficiency
	efficiency := (cpuEfficiency*0.6 + memoryEfficiency*0.4) * 100
	
	// Cap at 100%
	if efficiency > 100 {
		efficiency = 100
	}
	
	return math.Round(efficiency*100) / 100 // Round to 2 decimal places
}