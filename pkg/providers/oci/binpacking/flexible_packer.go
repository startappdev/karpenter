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

package binpacking

import (
	"context"
	"fmt"
	"sort"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/utils/resources"
)

// FlexiblePacker implements bin packing for dynamically sized nodes
type FlexiblePacker struct {
	// constraints from NodePool dynamic provisioning
	constraints *v1.DynamicConstraints
	// overhead configuration
	overhead *v1.SystemOverhead
	// buffer configuration
	buffers *v1.ResourceBuffers
	// pricing provider for cost calculations
	pricingProvider PricingProvider
}

// PricingProvider interface for cost calculations
type PricingProvider interface {
	GetShapePrice(ctx context.Context, shape string, ocpus, memoryGB int32, isPreemptible bool) (float64, error)
}

// NewFlexiblePacker creates a new flexible bin packer
func NewFlexiblePacker(constraints *v1.DynamicConstraints, overhead *v1.SystemOverhead, buffers *v1.ResourceBuffers, pricingProvider PricingProvider) *FlexiblePacker {
	return &FlexiblePacker{
		constraints:     constraints,
		overhead:        overhead,
		buffers:         buffers,
		pricingProvider: pricingProvider,
	}
}

// PackResult represents the result of bin packing
type PackResult struct {
	// Nodes contains the proposed node configurations
	Nodes []*NodeConfiguration
	// UnschedulablePods contains pods that couldn't be packed
	UnschedulablePods []*corev1.Pod
	// Efficiency metrics
	TotalCPURequested      resource.Quantity
	TotalCPUProvisioned    resource.Quantity
	TotalMemoryRequested   resource.Quantity
	TotalMemoryProvisioned resource.Quantity
	// Cost metrics
	EstimatedHourlyCost float64
}

// NodeConfiguration represents a proposed node configuration
type NodeConfiguration struct {
	// Shape family (e.g., VM.Standard.E4.Flex)
	Shape string
	// Number of OCPUs
	OCPUs int32
	// Memory in GB
	MemoryGB int32
	// Whether this should be a preemptible instance
	IsPreemptible bool
	// Pods assigned to this node
	Pods []*corev1.Pod
	// Resource utilization
	CPURequested    resource.Quantity
	MemoryRequested resource.Quantity
	// Estimated hourly cost
	EstimatedCost float64
}

// Pack performs bin packing for the given pods using the specified strategy
func (p *FlexiblePacker) Pack(ctx context.Context, pods []*corev1.Pod, strategy v1.ProvisioningStrategy, capacityType v1.CapacityTypeRatio) (*PackResult, error) {
	logger := log.FromContext(ctx)
	logger.Info("starting flexible bin packing", "pods", len(pods), "strategy", strategy)

	// Sort pods by resource requirements (largest first)
	sortedPods := p.sortPodsByResources(pods)

	// Initialize result
	result := &PackResult{
		Nodes:             make([]*NodeConfiguration, 0),
		UnschedulablePods: make([]*corev1.Pod, 0),
	}

	// Determine packing strategy
	switch strategy {
	case v1.StrategyExactFit:
		return p.packExactFit(ctx, sortedPods, capacityType)
	case v1.StrategyBestFit:
		return p.packBestFit(ctx, sortedPods, capacityType)
	case v1.StrategyCostOptimized:
		return p.packCostOptimized(ctx, sortedPods, capacityType)
	default:
		return p.packCostOptimized(ctx, sortedPods, capacityType)
	}
}

// packExactFit creates nodes that exactly fit the pod requirements
func (p *FlexiblePacker) packExactFit(ctx context.Context, pods []*corev1.Pod, capacityType v1.CapacityTypeRatio) (*PackResult, error) {
	result := &PackResult{
		Nodes:             make([]*NodeConfiguration, 0),
		UnschedulablePods: make([]*corev1.Pod, 0),
	}

	// For exact fit, create one node per pod
	for i, pod := range pods {
		cpu, memory := p.getPodResources(pod)

		// Add overhead
		cpuWithOverhead, memoryWithOverhead := p.addOverhead(cpu, memory)

		// Add buffers
		cpuWithBuffer := p.addBuffer(cpuWithOverhead, p.buffers.CPUHeadroomPercent)
		memoryWithBuffer := p.addBuffer(memoryWithOverhead, p.buffers.MemoryHeadroomPercent)

		// Convert to OCPUs and GB
		ocpus := p.calculateOCPUs(cpuWithBuffer)
		memoryGB := p.calculateMemoryGB(memoryWithBuffer)

		// Apply constraints
		ocpus = max(ocpus, p.constraints.MinOCPUs)
		ocpus = min(ocpus, p.constraints.MaxOCPUs)
		memoryGB = max(memoryGB, p.constraints.MinMemoryGB)
		memoryGB = min(memoryGB, p.constraints.MaxMemoryGB)

		// Check if pod can fit within constraints
		if ocpus > p.constraints.MaxOCPUs || memoryGB > p.constraints.MaxMemoryGB {
			result.UnschedulablePods = append(result.UnschedulablePods, pod)
			continue
		}

		// Determine capacity type
		isPreemptible := p.shouldUsePreemptible(i, len(pods), capacityType)

		// Select shape
		shape := p.selectShape(ocpus, memoryGB)

		// Calculate cost
		cost, _ := p.pricingProvider.GetShapePrice(ctx, shape, ocpus, memoryGB, isPreemptible)

		node := &NodeConfiguration{
			Shape:           shape,
			OCPUs:           ocpus,
			MemoryGB:        memoryGB,
			IsPreemptible:   isPreemptible,
			Pods:            []*corev1.Pod{pod},
			CPURequested:    cpu,
			MemoryRequested: memory,
			EstimatedCost:   cost,
		}

		result.Nodes = append(result.Nodes, node)
		result.TotalCPURequested.Add(cpu)
		result.TotalMemoryRequested.Add(memory)
		result.TotalCPUProvisioned.Add(*resource.NewQuantity(int64(ocpus)*2000, resource.DecimalSI))
		result.TotalMemoryProvisioned.Add(*resource.NewQuantity(int64(memoryGB)*1024*1024*1024, resource.BinarySI))
		result.EstimatedHourlyCost += cost
	}

	return result, nil
}

// packBestFit uses best-fit decreasing algorithm with efficient node sizes
func (p *FlexiblePacker) packBestFit(ctx context.Context, pods []*corev1.Pod, capacityType v1.CapacityTypeRatio) (*PackResult, error) {
	result := &PackResult{
		Nodes:             make([]*NodeConfiguration, 0),
		UnschedulablePods: make([]*corev1.Pod, 0),
	}

	// Define efficient node configurations
	nodeConfigs := p.getEfficientNodeConfigs()

	// Track remaining pods
	remainingPods := make([]*corev1.Pod, len(pods))
	copy(remainingPods, pods)

	nodeIndex := 0
	for len(remainingPods) > 0 {
		// Try to create a node with the best configuration
		bestNode := p.findBestNodeConfig(ctx, remainingPods, nodeConfigs, p.shouldUsePreemptible(nodeIndex, -1, capacityType))
		if bestNode == nil {
			// No configuration can fit any remaining pods
			result.UnschedulablePods = append(result.UnschedulablePods, remainingPods...)
			break
		}

		result.Nodes = append(result.Nodes, bestNode)
		nodeIndex++

		// Update result metrics
		result.TotalCPURequested.Add(bestNode.CPURequested)
		result.TotalMemoryRequested.Add(bestNode.MemoryRequested)
		result.TotalCPUProvisioned.Add(*resource.NewQuantity(int64(bestNode.OCPUs)*2000, resource.DecimalSI))
		result.TotalMemoryProvisioned.Add(*resource.NewQuantity(int64(bestNode.MemoryGB)*1024*1024*1024, resource.BinarySI))
		result.EstimatedHourlyCost += bestNode.EstimatedCost

		// Remove packed pods from remaining
		remainingPods = p.removePackedPods(remainingPods, bestNode.Pods)
	}

	return result, nil
}

// packCostOptimized uses cost-aware bin packing
func (p *FlexiblePacker) packCostOptimized(ctx context.Context, pods []*corev1.Pod, capacityType v1.CapacityTypeRatio) (*PackResult, error) {
	// Start with best-fit algorithm
	bestFitResult, err := p.packBestFit(ctx, pods, capacityType)
	if err != nil {
		return nil, err
	}

	// Try to optimize by combining small nodes
	optimizedResult := p.optimizeNodeConfigurations(ctx, bestFitResult, capacityType)

	// Compare costs and return the better option
	if optimizedResult.EstimatedHourlyCost < bestFitResult.EstimatedHourlyCost {
		return optimizedResult, nil
	}

	return bestFitResult, nil
}

// Helper methods

func (p *FlexiblePacker) sortPodsByResources(pods []*corev1.Pod) []*corev1.Pod {
	sorted := make([]*corev1.Pod, len(pods))
	copy(sorted, pods)

	sort.Slice(sorted, func(i, j int) bool {
		cpuI, memI := p.getPodResources(sorted[i])
		cpuJ, memJ := p.getPodResources(sorted[j])

		// Sort by total resource score (CPU + memory normalized)
		scoreI := cpuI.MilliValue() + memI.Value()/(1024*1024) // Memory in MB
		scoreJ := cpuJ.MilliValue() + memJ.Value()/(1024*1024)

		return scoreI > scoreJ
	})

	return sorted
}

func (p *FlexiblePacker) getPodResources(pod *corev1.Pod) (cpu, memory resource.Quantity) {
	for _, container := range pod.Spec.Containers {
		if cpuReq := container.Resources.Requests.Cpu(); cpuReq != nil {
			cpu.Add(*cpuReq)
		}
		if memReq := container.Resources.Requests.Memory(); memReq != nil {
			memory.Add(*memReq)
		}
	}
	return cpu, memory
}

func (p *FlexiblePacker) addOverhead(cpu, memory resource.Quantity) (resource.Quantity, resource.Quantity) {
	cpuWithOverhead := cpu.DeepCopy()
	memoryWithOverhead := memory.DeepCopy()

	// Default overhead if not specified
	defaultCPUOverhead := resource.NewMilliQuantity(300, resource.DecimalSI)     // 300m
	defaultMemoryOverhead := resource.NewQuantity(2*1024*1024*1024, resource.BinarySI) // 2Gi

	if p.overhead != nil {
		// System reserved
		if p.overhead.SystemReservedCPU != "" {
			if q, err := resource.ParseQuantity(p.overhead.SystemReservedCPU); err == nil {
				cpuWithOverhead.Add(q)
			}
		} else {
			cpuWithOverhead.Add(*defaultCPUOverhead)
		}

		if p.overhead.SystemReservedMemory != "" {
			if q, err := resource.ParseQuantity(p.overhead.SystemReservedMemory); err == nil {
				memoryWithOverhead.Add(q)
			}
		} else {
			memoryWithOverhead.Add(*defaultMemoryOverhead)
		}

		// Kubelet reserved
		if p.overhead.KubeletReservedCPU != "" {
			if q, err := resource.ParseQuantity(p.overhead.KubeletReservedCPU); err == nil {
				cpuWithOverhead.Add(q)
			}
		}

		if p.overhead.KubeletReservedMemory != "" {
			if q, err := resource.ParseQuantity(p.overhead.KubeletReservedMemory); err == nil {
				memoryWithOverhead.Add(q)
			}
		}
	} else {
		cpuWithOverhead.Add(*defaultCPUOverhead)
		memoryWithOverhead.Add(*defaultMemoryOverhead)
	}

	return cpuWithOverhead, memoryWithOverhead
}

func (p *FlexiblePacker) addBuffer(quantity resource.Quantity, bufferPercent int32) resource.Quantity {
	if bufferPercent <= 0 {
		return quantity
	}

	result := quantity.DeepCopy()
	bufferAmount := quantity.Value() * int64(bufferPercent) / 100
	result.Add(*resource.NewQuantity(bufferAmount, quantity.Format))

	return result
}

func (p *FlexiblePacker) calculateOCPUs(cpu resource.Quantity) int32 {
	// Convert CPU to OCPUs (1 OCPU = 2 vCPUs = 2000m)
	milliCPU := cpu.MilliValue()
	ocpus := (milliCPU + 1999) / 2000 // Round up

	if ocpus < 1 {
		ocpus = 1
	}

	return int32(ocpus)
}

func (p *FlexiblePacker) calculateMemoryGB(memory resource.Quantity) int32 {
	// Convert memory to GB
	bytes := memory.Value()
	gb := (bytes + (1024*1024*1024 - 1)) / (1024 * 1024 * 1024) // Round up

	if gb < 1 {
		gb = 1
	}

	return int32(gb)
}

func (p *FlexiblePacker) shouldUsePreemptible(nodeIndex, totalNodes int, capacityType v1.CapacityTypeRatio) bool {
	// Distribute capacity types according to ratio
	preemptibleRatio := float64(capacityType.Preemptible) / 100.0

	if totalNodes > 0 {
		// Use modulo to distribute evenly
		preemptibleCount := int(float64(totalNodes) * preemptibleRatio)
		return nodeIndex < preemptibleCount
	}

	// For dynamic distribution, use probability
	return nodeIndex%100 < int(capacityType.Preemptible)
}

func (p *FlexiblePacker) selectShape(ocpus, memoryGB int32) string {
	// Select the most appropriate shape based on requirements
	if len(p.constraints.AllowedShapes) == 0 {
		return "VM.Standard.E4.Flex"
	}

	// For now, return the first allowed shape
	// In a real implementation, this would consider shape capabilities
	return p.constraints.AllowedShapes[0]
}

func (p *FlexiblePacker) getEfficientNodeConfigs() []struct{ ocpus, memoryGB int32 } {
	// Define common efficient configurations
	configs := []struct{ ocpus, memoryGB int32 }{
		{1, 8},
		{2, 16},
		{4, 32},
		{8, 64},
		{16, 128},
		{32, 256},
		{64, 512},
	}

	// Filter by constraints
	var filtered []struct{ ocpus, memoryGB int32 }
	for _, config := range configs {
		if config.ocpus >= p.constraints.MinOCPUs && config.ocpus <= p.constraints.MaxOCPUs &&
			config.memoryGB >= p.constraints.MinMemoryGB && config.memoryGB <= p.constraints.MaxMemoryGB {
			filtered = append(filtered, config)
		}
	}

	return filtered
}

func (p *FlexiblePacker) findBestNodeConfig(ctx context.Context, pods []*corev1.Pod, configs []struct{ ocpus, memoryGB int32 }, isPreemptible bool) *NodeConfiguration {
	var bestNode *NodeConfiguration
	bestEfficiency := float64(0)

	for _, config := range configs {
		// Try to pack pods into this configuration
		node := &NodeConfiguration{
			Shape:         p.selectShape(config.ocpus, config.memoryGB),
			OCPUs:         config.ocpus,
			MemoryGB:      config.memoryGB,
			IsPreemptible: isPreemptible,
			Pods:          make([]*corev1.Pod, 0),
		}

		// Available resources (accounting for overhead)
		availableCPU := resource.NewQuantity(int64(config.ocpus)*2000, resource.DecimalSI)
		availableMemory := resource.NewQuantity(int64(config.memoryGB)*1024*1024*1024, resource.BinarySI)

		// Subtract overhead
		cpuOverhead, memoryOverhead := p.getNodeOverhead()
		availableCPU.Sub(cpuOverhead)
		availableMemory.Sub(memoryOverhead)

		// Try to pack pods
		for _, pod := range pods {
			cpu, memory := p.getPodResources(pod)

			if availableCPU.Cmp(cpu) >= 0 && availableMemory.Cmp(memory) >= 0 {
				node.Pods = append(node.Pods, pod)
				node.CPURequested.Add(cpu)
				node.MemoryRequested.Add(memory)
				availableCPU.Sub(cpu)
				availableMemory.Sub(memory)
			}
		}

		if len(node.Pods) == 0 {
			continue
		}

		// Calculate efficiency
		cpuEfficiency := float64(node.CPURequested.MilliValue()) / float64(config.ocpus*2000)
		memoryEfficiency := float64(node.MemoryRequested.Value()) / float64(config.memoryGB*1024*1024*1024)
		efficiency := (cpuEfficiency + memoryEfficiency) / 2

		// Get cost
		cost, _ := p.pricingProvider.GetShapePrice(ctx, node.Shape, node.OCPUs, node.MemoryGB, node.IsPreemptible)
		node.EstimatedCost = cost

		// Consider both efficiency and cost
		score := efficiency / (cost + 0.01) // Avoid division by zero

		if score > bestEfficiency {
			bestEfficiency = score
			bestNode = node
		}
	}

	return bestNode
}

func (p *FlexiblePacker) getNodeOverhead() (cpu, memory resource.Quantity) {
	// Default overhead
	cpu = *resource.NewMilliQuantity(300, resource.DecimalSI)         // 300m
	memory = *resource.NewQuantity(2*1024*1024*1024, resource.BinarySI) // 2Gi

	if p.overhead != nil {
		// Parse configured overhead
		if p.overhead.SystemReservedCPU != "" {
			if q, err := resource.ParseQuantity(p.overhead.SystemReservedCPU); err == nil {
				cpu = q
			}
		}
		if p.overhead.SystemReservedMemory != "" {
			if q, err := resource.ParseQuantity(p.overhead.SystemReservedMemory); err == nil {
				memory = q
			}
		}
		if p.overhead.KubeletReservedCPU != "" {
			if q, err := resource.ParseQuantity(p.overhead.KubeletReservedCPU); err == nil {
				cpu.Add(q)
			}
		}
		if p.overhead.KubeletReservedMemory != "" {
			if q, err := resource.ParseQuantity(p.overhead.KubeletReservedMemory); err == nil {
				memory.Add(q)
			}
		}
	}

	return cpu, memory
}

func (p *FlexiblePacker) removePackedPods(pods []*corev1.Pod, packed []*corev1.Pod) []*corev1.Pod {
	packedSet := make(map[string]bool)
	for _, pod := range packed {
		packedSet[string(pod.UID)] = true
	}

	var remaining []*corev1.Pod
	for _, pod := range pods {
		if !packedSet[string(pod.UID)] {
			remaining = append(remaining, pod)
		}
	}

	return remaining
}

func (p *FlexiblePacker) optimizeNodeConfigurations(ctx context.Context, result *PackResult, capacityType v1.CapacityTypeRatio) *PackResult {
	// Try to combine small nodes into larger ones
	optimized := &PackResult{
		Nodes:                  make([]*NodeConfiguration, 0),
		UnschedulablePods:      result.UnschedulablePods,
		TotalCPURequested:      result.TotalCPURequested,
		TotalMemoryRequested:   result.TotalMemoryRequested,
		TotalCPUProvisioned:    resource.Quantity{},
		TotalMemoryProvisioned: resource.Quantity{},
	}

	// Group nodes by capacity type
	var preemptibleNodes, onDemandNodes []*NodeConfiguration
	for _, node := range result.Nodes {
		if node.IsPreemptible {
			preemptibleNodes = append(preemptibleNodes, node)
		} else {
			onDemandNodes = append(onDemandNodes, node)
		}
	}

	// Optimize each group separately
	optimizedPreemptible := p.combineNodes(ctx, preemptibleNodes, true)
	optimizedOnDemand := p.combineNodes(ctx, onDemandNodes, false)

	optimized.Nodes = append(optimized.Nodes, optimizedPreemptible...)
	optimized.Nodes = append(optimized.Nodes, optimizedOnDemand...)

	// Recalculate metrics
	for _, node := range optimized.Nodes {
		optimized.TotalCPUProvisioned.Add(*resource.NewQuantity(int64(node.OCPUs)*2000, resource.DecimalSI))
		optimized.TotalMemoryProvisioned.Add(*resource.NewQuantity(int64(node.MemoryGB)*1024*1024*1024, resource.BinarySI))
		optimized.EstimatedHourlyCost += node.EstimatedCost
	}

	return optimized
}

func (p *FlexiblePacker) combineNodes(ctx context.Context, nodes []*NodeConfiguration, isPreemptible bool) []*NodeConfiguration {
	if len(nodes) <= 1 {
		return nodes
	}

	// Collect all pods
	var allPods []*corev1.Pod
	for _, node := range nodes {
		allPods = append(allPods, node.Pods...)
	}

	// Try to repack with larger configurations
	configs := p.getEfficientNodeConfigs()
	var combined []*NodeConfiguration

	remainingPods := allPods
	for len(remainingPods) > 0 && len(configs) > 0 {
		// Try largest configuration first
		bestNode := p.findBestNodeConfig(ctx, remainingPods, configs, isPreemptible)
		if bestNode == nil {
			// Cannot pack any more pods efficiently
			break
		}

		combined = append(combined, bestNode)
		remainingPods = p.removePackedPods(remainingPods, bestNode.Pods)
	}

	// If we have unpacked pods, fall back to original configuration
	if len(remainingPods) > 0 {
		return nodes
	}

	// Calculate total cost for comparison
	combinedCost := float64(0)
	for _, node := range combined {
		combinedCost += node.EstimatedCost
	}

	originalCost := float64(0)
	for _, node := range nodes {
		originalCost += node.EstimatedCost
	}

	// Only use combined if it's cheaper
	if combinedCost < originalCost {
		return combined
	}

	return nodes
}