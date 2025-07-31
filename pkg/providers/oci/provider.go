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
	"strconv"
	"strings"
	"time"

	"github.com/awslabs/operatorpkg/status"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/scheduling"
)

const (
	ProviderName = "oci"
)

var _ cloudprovider.CloudProvider = (*Provider)(nil)

// Provider implements the CloudProvider interface for Oracle Cloud Infrastructure
type Provider struct {
	client               *Client
	instanceTypeCache    map[string][]*cloudprovider.InstanceType
	pricingProvider      *PricingProvider
	instanceTypeProvider *InstanceTypeProvider
}

// NewProvider creates a new OCI cloud provider
func NewProvider(ctx context.Context, config *Config) (*Provider, error) {
	client, err := NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("creating OCI client: %w", err)
	}

	pricingProvider := NewPricingProvider(client)
	instanceTypeProvider := NewInstanceTypeProvider(client, pricingProvider)

	return &Provider{
		client:               client,
		instanceTypeCache:    make(map[string][]*cloudprovider.InstanceType),
		pricingProvider:      pricingProvider,
		instanceTypeProvider: instanceTypeProvider,
	}, nil
}

// Create launches a NodeClaim with the given resource requests
func (p *Provider) Create(ctx context.Context, nodeClaim *v1.NodeClaim) (*v1.NodeClaim, error) {
	logger := log.FromContext(ctx)
	logger.Info("creating OCI instance", "nodeClaim", nodeClaim.Name)

	// Extract instance type from requirements
	instanceType := p.getInstanceTypeFromRequirements(nodeClaim.Spec.Requirements)
	if instanceType == "" {
		return nil, fmt.Errorf("no instance type found in requirements")
	}

	// Determine if this is a dynamic shape
	isDynamicShape := p.isDynamicShape(instanceType)
	
	var instance *Instance
	var err error
	
	if isDynamicShape {
		// Extract shape configuration from instance type name (encoded in the format)
		shapeConfig := p.parseShapeConfig(instanceType)
		instance, err = p.client.LaunchFlexibleInstance(ctx, nodeClaim, shapeConfig)
	} else {
		// Standard fixed shape instance
		instance, err = p.client.LaunchInstance(ctx, nodeClaim, instanceType)
	}
	
	if err != nil {
		return nil, fmt.Errorf("launching instance: %w", err)
	}

	// Update NodeClaim with provider information
	nodeClaim.Status.ProviderID = fmt.Sprintf("oci://%s", instance.ID)
	nodeClaim.Status.ImageID = instance.ImageID
	
	// Set capacity and allocatable
	capacity := p.getInstanceCapacity(instance)
	nodeClaim.Status.Capacity = capacity
	nodeClaim.Status.Allocatable = p.getAllocatable(capacity)

	// Set dynamic shape status if applicable
	if isDynamicShape && instance.ShapeConfig != nil {
		nodeClaim.Status.DynamicShape = &v1.DynamicShapeStatus{
			Shape:        instance.Shape,
			OCPUs:        lo.FromPtr(instance.ShapeConfig.OCPUs),
			MemoryGB:     lo.FromPtr(instance.ShapeConfig.MemoryInGBs),
			CapacityType: p.getCapacityType(instance),
			Strategy:     p.getProvisioningStrategy(nodeClaim),
			Efficiency:   p.calculateEfficiency(nodeClaim, instance),
			Pricing:      p.getPricingInfo(instance),
		}
	}

	return nodeClaim, nil
}

// Delete removes a NodeClaim from OCI
func (p *Provider) Delete(ctx context.Context, nodeClaim *v1.NodeClaim) error {
	logger := log.FromContext(ctx)
	logger.Info("deleting OCI instance", "nodeClaim", nodeClaim.Name, "providerID", nodeClaim.Status.ProviderID)

	instanceID := p.getInstanceIDFromProviderID(nodeClaim.Status.ProviderID)
	if instanceID == "" {
		return fmt.Errorf("invalid provider ID: %s", nodeClaim.Status.ProviderID)
	}

	err := p.client.TerminateInstance(ctx, instanceID)
	if err != nil {
		if IsNotFoundError(err) {
			return cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("instance %s not found", instanceID))
		}
		return fmt.Errorf("terminating instance: %w", err)
	}

	return nil
}

// Get retrieves a NodeClaim from OCI
func (p *Provider) Get(ctx context.Context, providerID string) (*v1.NodeClaim, error) {
	instanceID := p.getInstanceIDFromProviderID(providerID)
	if instanceID == "" {
		return nil, fmt.Errorf("invalid provider ID: %s", providerID)
	}

	instance, err := p.client.GetInstance(ctx, instanceID)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, cloudprovider.NewNodeClaimNotFoundError(fmt.Errorf("instance %s not found", instanceID))
		}
		return nil, fmt.Errorf("getting instance: %w", err)
	}

	// Convert instance to NodeClaim
	return p.instanceToNodeClaim(instance), nil
}

// List retrieves all NodeClaims from OCI
func (p *Provider) List(ctx context.Context) ([]*v1.NodeClaim, error) {
	instances, err := p.client.ListInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}

	nodeClaims := make([]*v1.NodeClaim, 0, len(instances))
	for _, instance := range instances {
		nodeClaims = append(nodeClaims, p.instanceToNodeClaim(instance))
	}

	return nodeClaims, nil
}

// GetInstanceTypes returns instance types available for the given NodePool
func (p *Provider) GetInstanceTypes(ctx context.Context, nodePool *v1.NodePool) ([]*cloudprovider.InstanceType, error) {
	// Check if dynamic provisioning is enabled
	if nodePool.Spec.DynamicProvisioning != nil && nodePool.Spec.DynamicProvisioning.Enabled {
		return p.getDynamicInstanceTypes(ctx, nodePool)
	}

	// Return static instance types
	return p.getStaticInstanceTypes(ctx, nodePool)
}

// IsDrifted returns whether a NodeClaim has drifted from its requirements
func (p *Provider) IsDrifted(ctx context.Context, nodeClaim *v1.NodeClaim) (cloudprovider.DriftReason, error) {
	// For now, no drift detection for OCI
	return "", nil
}

// RepairPolicies returns repair policies for the provider
func (p *Provider) RepairPolicies() []cloudprovider.RepairPolicy {
	return []cloudprovider.RepairPolicy{
		{
			ConditionType:      corev1.NodeReady,
			ConditionStatus:    corev1.ConditionFalse,
			TolerationDuration: 5 * time.Minute,
		},
	}
}

// Name returns the provider implementation name
func (p *Provider) Name() string {
	return ProviderName
}

// GetSupportedNodeClasses returns the supported node class types
func (p *Provider) GetSupportedNodeClasses() []status.Object {
	// This would return OCI-specific node class implementations
	// For now, returning empty as we focus on the core provider
	return []status.Object{}
}

// Helper methods

func (p *Provider) getInstanceTypeFromRequirements(requirements []v1.NodeSelectorRequirementWithMinValues) string {
	for _, req := range requirements {
		if req.Key == corev1.LabelInstanceTypeStable && len(req.Values) > 0 {
			return req.Values[0]
		}
	}
	return ""
}

func (p *Provider) isDynamicShape(shape string) bool {
	return len(shape) > 4 && (shape[len(shape)-4:] == "Flex" || shape[len(shape)-4:] == "flex")
}

func (p *Provider) parseShapeConfig(instanceType string) *ShapeConfig {
	// Instance type format for dynamic shapes: "VM.Standard.E4.Flex-4-32"
	// Parse the OCPU and memory values from the instance type name
	
	parts := strings.Split(instanceType, "-")
	if len(parts) < 3 {
		// Invalid format, return defaults
		ocpus := int32(2)
		memory := int32(16)
		return &ShapeConfig{
			OCPUs:       &ocpus,
			MemoryInGBs: &memory,
		}
	}
	
	// Parse OCPUs and memory from the last two parts
	ocpus, err := strconv.ParseInt(parts[len(parts)-2], 10, 32)
	if err != nil {
		ocpus = 2
	}
	
	memory, err := strconv.ParseInt(parts[len(parts)-1], 10, 32)
	if err != nil {
		memory = 16
	}
	
	ocpus32 := int32(ocpus)
	memory32 := int32(memory)
	
	return &ShapeConfig{
		OCPUs:       &ocpus32,
		MemoryInGBs: &memory32,
	}
}

func (p *Provider) getInstanceIDFromProviderID(providerID string) string {
	// Provider ID format: "oci://instance-id"
	if len(providerID) > 6 && providerID[:6] == "oci://" {
		return providerID[6:]
	}
	return ""
}

func (p *Provider) getInstanceCapacity(instance *Instance) corev1.ResourceList {
	capacity := corev1.ResourceList{}
	
	if instance.ShapeConfig != nil {
		// Dynamic shape
		ocpus := lo.FromPtr(instance.ShapeConfig.OCPUs)
		memoryGB := lo.FromPtr(instance.ShapeConfig.MemoryInGBs)
		
		// 1 OCPU = 2 vCPUs
		capacity[corev1.ResourceCPU] = *resource.NewQuantity(int64(ocpus)*2000, resource.DecimalSI)
		capacity[corev1.ResourceMemory] = *resource.NewQuantity(int64(memoryGB)*1024*1024*1024, resource.BinarySI)
	} else {
		// Fixed shape - would need shape details from OCI
		// For now, returning empty
	}
	
	// Standard resources
	capacity[corev1.ResourcePods] = *resource.NewQuantity(110, resource.DecimalSI)
	capacity[corev1.ResourceEphemeralStorage] = *resource.NewQuantity(100*1024*1024*1024, resource.BinarySI)
	
	return capacity
}

func (p *Provider) getAllocatable(capacity corev1.ResourceList) corev1.ResourceList {
	// Simple allocatable calculation - subtract system reserved
	allocatable := capacity.DeepCopy()
	
	// Reserve 100m CPU and 500Mi memory for system
	if cpu, ok := allocatable[corev1.ResourceCPU]; ok {
		cpu.Sub(*resource.NewMilliQuantity(100, resource.DecimalSI))
		allocatable[corev1.ResourceCPU] = cpu
	}
	
	if memory, ok := allocatable[corev1.ResourceMemory]; ok {
		memory.Sub(*resource.NewQuantity(500*1024*1024, resource.BinarySI))
		allocatable[corev1.ResourceMemory] = memory
	}
	
	return allocatable
}

func (p *Provider) getCapacityType(instance *Instance) string {
	if instance.IsPreemptible {
		return "preemptible"
	}
	return "on-demand"
}

func (p *Provider) getProvisioningStrategy(nodeClaim *v1.NodeClaim) v1.ProvisioningStrategy {
	// Extract from labels or annotations set during provisioning
	// For now, return default
	return v1.StrategyCostOptimized
}

func (p *Provider) calculateEfficiency(nodeClaim *v1.NodeClaim, instance *Instance) v1.ResourceEfficiency {
	// This would calculate actual efficiency based on pod requests vs provisioned
	// For now, returning placeholder values
	return v1.ResourceEfficiency{
		CPURequested:      "0",
		CPUProvisioned:    "0",
		CPUEfficiency:     0,
		MemoryRequested:   "0",
		MemoryProvisioned: "0",
		MemoryEfficiency:  0,
	}
}

func (p *Provider) getPricingInfo(instance *Instance) *v1.PricingInfo {
	// Would integrate with pricing provider
	return nil
}

func (p *Provider) instanceToNodeClaim(instance *Instance) *v1.NodeClaim {
	return &v1.NodeClaim{
		Status: v1.NodeClaimStatus{
			ProviderID: fmt.Sprintf("oci://%s", instance.ID),
			ImageID:    instance.ImageID,
			Capacity:   p.getInstanceCapacity(instance),
		},
	}
}

func (p *Provider) getDynamicInstanceTypes(ctx context.Context, nodePool *v1.NodePool) ([]*cloudprovider.InstanceType, error) {
	// For dynamic provisioning, we generate instance types without specific pod requirements
	// The actual pod requirements will be handled during the Create phase
	return p.instanceTypeProvider.GetDynamicInstanceTypes(ctx, nodePool, nil)
}

func (p *Provider) getStaticInstanceTypes(ctx context.Context, nodePool *v1.NodePool) ([]*cloudprovider.InstanceType, error) {
	return p.instanceTypeProvider.GetStaticInstanceTypes(ctx, nodePool)
}