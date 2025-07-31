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
	"os"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/providers/oci/binpacking"
	"sigs.k8s.io/karpenter/pkg/providers/oci/dynamicshape"
)

const (
	// EnvVarFeatureFlag is the environment variable to enable dynamic provisioning
	EnvVarFeatureFlag = "ENABLE_OCI_DYNAMIC_SHAPES"
)

// Controller handles dynamic provisioning integration with Karpenter
type Controller struct {
	enabled bool
}

// NewController creates a new dynamic provisioning controller
func NewController() *Controller {
	enabled := false
	if val := os.Getenv(EnvVarFeatureFlag); val != "" {
		if parsedVal, err := strconv.ParseBool(val); err == nil {
			enabled = parsedVal
		}
	}
	return &Controller{
		enabled: enabled,
	}
}

// IsEnabled returns whether dynamic provisioning is enabled
func (c *Controller) IsEnabled() bool {
	return c.enabled
}

// ShouldUseDynamicProvisioning checks if a NodePool should use dynamic provisioning
func (c *Controller) ShouldUseDynamicProvisioning(nodePool *v1.NodePool) bool {
	if !c.enabled {
		return false
	}
	
	if nodePool.Spec.DynamicProvisioning == nil {
		return false
	}
	
	return nodePool.Spec.DynamicProvisioning.Enabled
}

// InterceptGetInstanceTypes intercepts GetInstanceTypes calls for dynamic provisioning
func (c *Controller) InterceptGetInstanceTypes(
	ctx context.Context,
	cloudProvider cloudprovider.CloudProvider,
	nodePool *v1.NodePool,
) ([]*cloudprovider.InstanceType, error) {
	if !c.ShouldUseDynamicProvisioning(nodePool) {
		// Fall back to standard instance type retrieval
		return cloudProvider.GetInstanceTypes(ctx, nodePool)
	}
	
	logger := log.FromContext(ctx)
	logger.Info("using dynamic provisioning for nodepool", "nodepool", nodePool.Name)
	
	// For dynamic provisioning, we need to generate instance types based on the current
	// scheduling context. This will be refined during the actual scheduling loop.
	return cloudProvider.GetInstanceTypes(ctx, nodePool)
}

// GenerateDynamicNodeClaim generates a dynamic node claim based on pod requirements
func (c *Controller) GenerateDynamicNodeClaim(
	ctx context.Context,
	nodePool *v1.NodePool,
	pods []*corev1.Pod,
	calculator *dynamicshape.Calculator,
	packer *binpacking.FlexiblePacker,
) (*DynamicNodeClaim, error) {
	if !c.ShouldUseDynamicProvisioning(nodePool) {
		return nil, fmt.Errorf("dynamic provisioning not enabled for nodepool %s", nodePool.Name)
	}
	
	dp := nodePool.Spec.DynamicProvisioning
	
	// Use bin packing to determine optimal node configurations
	packResult, err := packer.Pack(ctx, pods, dp.Strategy, dp.CapacityType)
	if err != nil {
		return nil, fmt.Errorf("bin packing failed: %w", err)
	}
	
	if len(packResult.Nodes) == 0 {
		return nil, fmt.Errorf("no valid node configurations found")
	}
	
	// For now, return the first configuration
	// In a more sophisticated implementation, we might return multiple options
	nodeConfig := packResult.Nodes[0]
	
	return &DynamicNodeClaim{
		NodePoolName:  nodePool.Name,
		Shape:         nodeConfig.Shape,
		OCPUs:         nodeConfig.OCPUs,
		MemoryGB:      nodeConfig.MemoryGB,
		IsPreemptible: nodeConfig.IsPreemptible,
		Pods:          nodeConfig.Pods,
		Efficiency: v1.ResourceEfficiency{
			CPURequested:      nodeConfig.CPURequested.String(),
			CPUProvisioned:    fmt.Sprintf("%dm", nodeConfig.OCPUs*2000),
			CPUEfficiency:     calculateEfficiency(nodeConfig.CPURequested.MilliValue(), int64(nodeConfig.OCPUs)*2000),
			MemoryRequested:   nodeConfig.MemoryRequested.String(),
			MemoryProvisioned: fmt.Sprintf("%dGi", nodeConfig.MemoryGB),
			MemoryEfficiency:  calculateEfficiency(nodeConfig.MemoryRequested.Value(), int64(nodeConfig.MemoryGB)*1024*1024*1024),
		},
		EstimatedCost: nodeConfig.EstimatedCost,
	}, nil
}

// DynamicNodeClaim represents a dynamically sized node claim
type DynamicNodeClaim struct {
	NodePoolName  string
	Shape         string
	OCPUs         int32
	MemoryGB      int32
	IsPreemptible bool
	Pods          []*corev1.Pod
	Efficiency    v1.ResourceEfficiency
	EstimatedCost float64
}

// ToInstanceTypeName generates a unique instance type name for this configuration
func (d *DynamicNodeClaim) ToInstanceTypeName() string {
	return fmt.Sprintf("%s-%d-%d", d.Shape, d.OCPUs, d.MemoryGB)
}

// calculateEfficiency calculates the efficiency percentage
func calculateEfficiency(requested, provisioned int64) float32 {
	if provisioned == 0 {
		return 0
	}
	return float32(requested) / float32(provisioned) * 100
}