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

package dynamicprovisioning

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/controllers/provisioning/scheduling"
	"sigs.k8s.io/karpenter/pkg/providers/oci/binpacking"
	"sigs.k8s.io/karpenter/pkg/providers/oci/dynamicshape"
	karscheduling "sigs.k8s.io/karpenter/pkg/scheduling"
)

// SchedulerIntegration provides integration between dynamic provisioning and Karpenter's scheduler
type SchedulerIntegration struct {
	controller *Controller
}

// NewSchedulerIntegration creates a new scheduler integration
func NewSchedulerIntegration(controller *Controller) *SchedulerIntegration {
	return &SchedulerIntegration{
		controller: controller,
	}
}

// EnhanceNodeClaimTemplate enhances a NodeClaimTemplate with dynamic provisioning information
func (s *SchedulerIntegration) EnhanceNodeClaimTemplate(
	ctx context.Context,
	template *scheduling.NodeClaimTemplate,
	nodePool *v1.NodePool,
	pods []*corev1.Pod,
	pricingProvider binpacking.PricingProvider,
) error {
	if !s.controller.ShouldUseDynamicProvisioning(nodePool) {
		return nil
	}
	
	logger := log.FromContext(ctx)
	dp := nodePool.Spec.DynamicProvisioning
	
	// Create calculator and packer
	calculator := dynamicshape.NewCalculator(&dp.Constraints, dp.Overhead, dp.Buffers)
	packer := binpacking.NewFlexiblePacker(&dp.Constraints, dp.Overhead, dp.Buffers, pricingProvider)
	
	// Calculate shape recommendation based on pods
	recommendation, err := calculator.CalculateShapeForPods(ctx, pods, dp.Strategy)
	if err != nil {
		return fmt.Errorf("calculating shape for pods: %w", err)
	}
	
	logger.V(1).Info("calculated dynamic shape recommendation",
		"shape", recommendation.Shape,
		"ocpus", recommendation.OCPUs,
		"memoryGB", recommendation.MemoryGB,
		"efficiency", recommendation.EfficiencyScore)
	
	// Add dynamic provisioning metadata to template
	if template.Annotations == nil {
		template.Annotations = make(map[string]string)
	}
	template.Annotations["karpenter.sh/dynamic-shape"] = recommendation.Shape
	template.Annotations["karpenter.sh/dynamic-ocpus"] = fmt.Sprintf("%d", recommendation.OCPUs)
	template.Annotations["karpenter.sh/dynamic-memory-gb"] = fmt.Sprintf("%d", recommendation.MemoryGB)
	template.Annotations["karpenter.sh/dynamic-efficiency"] = fmt.Sprintf("%.2f", recommendation.EfficiencyScore)
	
	return nil
}

// GenerateDynamicInstanceTypes generates dynamic instance types for a node pool
func (s *SchedulerIntegration) GenerateDynamicInstanceTypes(
	ctx context.Context,
	nodePool *v1.NodePool,
	pods []*corev1.Pod,
) ([]*cloudprovider.InstanceType, error) {
	if !s.controller.ShouldUseDynamicProvisioning(nodePool) {
		return nil, nil
	}
	
	logger := log.FromContext(ctx)
	dp := nodePool.Spec.DynamicProvisioning
	
	// Create calculator
	calculator := dynamicshape.NewCalculator(&dp.Constraints, dp.Overhead, dp.Buffers)
	
	// If no pods provided, generate a range of instance types
	if len(pods) == 0 {
		return s.generateInstanceTypeRange(ctx, nodePool, calculator)
	}
	
	// Calculate optimal configurations based on pods
	recommendation, err := calculator.CalculateShapeForPods(ctx, pods, dp.Strategy)
	if err != nil {
		return nil, fmt.Errorf("calculating shape for pods: %w", err)
	}
	
	// Get shape variations for flexibility
	variations, err := calculator.GetShapeVariations(ctx, recommendation.Resources.PodCPU, recommendation.Resources.PodMemory)
	if err != nil {
		logger.V(1).Info("failed to get shape variations, using single recommendation", "error", err)
		variations = []*dynamicshape.ShapeRecommendation{recommendation}
	}
	
	// Convert recommendations to instance types
	var instanceTypes []*cloudprovider.InstanceType
	for _, rec := range variations {
		instanceTypes = append(instanceTypes, s.recommendationToInstanceType(nodePool, rec))
	}
	
	logger.Info("generated dynamic instance types",
		"nodepool", nodePool.Name,
		"count", len(instanceTypes))
	
	return instanceTypes, nil
}

// generateInstanceTypeRange generates a range of instance types when no specific pods are provided
func (s *SchedulerIntegration) generateInstanceTypeRange(
	ctx context.Context,
	nodePool *v1.NodePool,
	calculator *dynamicshape.Calculator,
) ([]*cloudprovider.InstanceType, error) {
	dp := nodePool.Spec.DynamicProvisioning
	
	// Define common configurations
	configs := []struct {
		cpu    string
		memory string
	}{
		{"1", "8Gi"},
		{"2", "16Gi"},
		{"4", "32Gi"},
		{"8", "64Gi"},
		{"16", "128Gi"},
		{"32", "256Gi"},
	}
	
	var instanceTypes []*cloudprovider.InstanceType
	
	for _, config := range configs {
		cpu := resource.MustParse(config.cpu)
		memory := resource.MustParse(config.memory)
		
		rec, err := calculator.CalculateShapeForResources(ctx, cpu, memory, dp.Strategy)
		if err != nil {
			continue
		}
		
		// Check constraints
		if rec.OCPUs < dp.Constraints.MinOCPUs || rec.OCPUs > dp.Constraints.MaxOCPUs {
			continue
		}
		if rec.MemoryGB < dp.Constraints.MinMemoryGB || rec.MemoryGB > dp.Constraints.MaxMemoryGB {
			continue
		}
		
		instanceTypes = append(instanceTypes, s.recommendationToInstanceType(nodePool, rec))
	}
	
	return instanceTypes, nil
}

// recommendationToInstanceType converts a shape recommendation to a CloudProvider InstanceType
func (s *SchedulerIntegration) recommendationToInstanceType(
	nodePool *v1.NodePool,
	rec *dynamicshape.ShapeRecommendation,
) *cloudprovider.InstanceType {
	instanceName := fmt.Sprintf("%s-%d-%d", rec.Shape, rec.OCPUs, rec.MemoryGB)
	
	// Build requirements
	requirements := karscheduling.NewRequirements(
		karscheduling.NewRequirement(corev1.LabelInstanceTypeStable, corev1.NodeSelectorOpIn, instanceName),
		karscheduling.NewRequirement(corev1.LabelArchStable, corev1.NodeSelectorOpIn, "amd64"),
		karscheduling.NewRequirement("karpenter.sh/instance-category", corev1.NodeSelectorOpIn, "flex"),
		karscheduling.NewRequirement("oci.oraclecloud.com/shape", corev1.NodeSelectorOpIn, rec.Shape),
		karscheduling.NewRequirement("karpenter.sh/dynamic-provisioning", corev1.NodeSelectorOpIn, "true"),
	)
	
	// Calculate capacity
	capacity := corev1.ResourceList{
		corev1.ResourceCPU:              *resource.NewQuantity(int64(rec.OCPUs)*2000, resource.DecimalSI),
		corev1.ResourceMemory:           *resource.NewQuantity(int64(rec.MemoryGB)*1024*1024*1024, resource.BinarySI),
		corev1.ResourcePods:             *resource.NewQuantity(110, resource.DecimalSI),
		corev1.ResourceEphemeralStorage: *resource.NewQuantity(100*1024*1024*1024, resource.BinarySI),
	}
	
	// Create offerings based on capacity type configuration
	offerings := s.createOfferings(nodePool, rec)
	
	// Calculate overhead
	overhead := &cloudprovider.InstanceTypeOverhead{
		KubeReserved: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewMilliQuantity(200, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewQuantity(1*1024*1024*1024, resource.BinarySI),
		},
		SystemReserved: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewMilliQuantity(100, resource.DecimalSI),
			corev1.ResourceMemory: *resource.NewQuantity(500*1024*1024, resource.BinarySI),
		},
		EvictionThreshold: corev1.ResourceList{
			corev1.ResourceMemory: *resource.NewQuantity(500*1024*1024, resource.BinarySI),
		},
	}
	
	return &cloudprovider.InstanceType{
		Name:         instanceName,
		Requirements: requirements,
		Offerings:    offerings,
		Capacity:     capacity,
		Overhead:     overhead,
	}
}

// createOfferings creates offerings based on the capacity type configuration
func (s *SchedulerIntegration) createOfferings(
	nodePool *v1.NodePool,
	rec *dynamicshape.ShapeRecommendation,
) cloudprovider.Offerings {
	var offerings cloudprovider.Offerings
	
	dp := nodePool.Spec.DynamicProvisioning
	zones := []string{"zone-1", "zone-2", "zone-3"} // This should come from the cloud provider
	
	// Create on-demand offerings if configured
	if dp.CapacityType.OnDemand > 0 {
		for _, zone := range zones {
			offerings = append(offerings, cloudprovider.Offering{
				Requirements: karscheduling.NewRequirements(
					karscheduling.NewRequirement(v1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, v1.CapacityTypeOnDemand),
					karscheduling.NewRequirement(corev1.LabelTopologyZone, corev1.NodeSelectorOpIn, zone),
				),
				Price:     lo.ToPtr(rec.EstimatedCost),
				Available: true,
			})
		}
	}
	
	// Create preemptible offerings if configured
	if dp.CapacityType.Preemptible > 0 {
		for _, zone := range zones {
			offerings = append(offerings, cloudprovider.Offering{
				Requirements: karscheduling.NewRequirements(
					karscheduling.NewRequirement(v1.CapacityTypeLabelKey, corev1.NodeSelectorOpIn, "preemptible"),
					karscheduling.NewRequirement(corev1.LabelTopologyZone, corev1.NodeSelectorOpIn, zone),
				),
				Price:     lo.ToPtr(rec.EstimatedCost * 0.2), // Preemptible is typically 80% cheaper
				Available: true,
			})
		}
	}
	
	return offerings
}