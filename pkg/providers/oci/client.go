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
	"time"

	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// Client wraps OCI API operations
type Client struct {
	config *Config
	// In a real implementation, this would contain the actual OCI SDK clients
	// For now, we'll implement a simplified version
}

// NewClient creates a new OCI client
func NewClient(config *Config) (*Client, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	// Validate required configuration
	if config.CompartmentID == "" {
		return nil, fmt.Errorf("compartment ID is required")
	}
	if config.Region == "" {
		return nil, fmt.Errorf("region is required")
	}
	if len(config.SubnetIDs) == 0 {
		return nil, fmt.Errorf("at least one subnet ID is required")
	}

	return &Client{
		config: config,
	}, nil
}

// LaunchInstance launches a standard instance with fixed shape
func (c *Client) LaunchInstance(ctx context.Context, nodeClaim *v1.NodeClaim, shape string) (*Instance, error) {
	logger := log.FromContext(ctx)
	logger.Info("launching OCI instance", "shape", shape)

	details := &LaunchInstanceDetails{
		AvailabilityDomain: c.selectAvailabilityDomain(),
		CompartmentID:      c.config.CompartmentID,
		Shape:              shape,
		ImageID:            c.config.ImageID,
		SubnetID:           c.selectSubnet(),
		Metadata:           c.buildMetadata(nodeClaim),
		FreeformTags:       c.buildFreeformTags(nodeClaim),
		DefinedTags:        c.buildDefinedTags(nodeClaim),
	}

	var instance *Instance
	err := WithRetry(ctx, DefaultRetryConfig(), "launch-instance", func() error {
		// Simulate instance launch with potential failures
		if shouldSimulateError() {
			return ErrShapeNotAvailable
		}
		
		instance = &Instance{
			ID:                 fmt.Sprintf("ocid1.instance.oc1.%s.%s", c.config.Region, generateID()),
			Shape:              shape,
			ImageID:            details.ImageID,
			CompartmentID:      details.CompartmentID,
			AvailabilityDomain: details.AvailabilityDomain,
			State:              "RUNNING",
			TimeCreated:        time.Now(),
			Metadata:           details.Metadata,
			FreeformTags:       details.FreeformTags,
			DefinedTags:        details.DefinedTags,
		}
		return nil
	})
	
	if err != nil {
		return nil, HandleError(ctx, err, "launch-instance")
	}

	return instance, nil
}

// LaunchFlexibleInstance launches a flexible shape instance with custom CPU/memory
func (c *Client) LaunchFlexibleInstance(ctx context.Context, nodeClaim *v1.NodeClaim, shapeConfig *ShapeConfig) (*Instance, error) {
	logger := log.FromContext(ctx)
	
	// Determine shape family from NodePool configuration
	shape := c.selectFlexibleShape(nodeClaim)
	
	logger.Info("launching OCI flexible instance", 
		"shape", shape,
		"ocpus", lo.FromPtr(shapeConfig.OCPUs),
		"memory", lo.FromPtr(shapeConfig.MemoryInGBs))

	// Determine capacity type from nodeClaim labels or annotations
	isPreemptible := c.shouldUsePreemptible(nodeClaim)
	
	details := &LaunchInstanceDetails{
		AvailabilityDomain: c.selectAvailabilityDomain(),
		CompartmentID:      c.config.CompartmentID,
		Shape:              shape,
		ShapeConfig:        shapeConfig,
		ImageID:            c.config.ImageID,
		SubnetID:           c.selectSubnet(),
		Metadata:           c.buildMetadata(nodeClaim),
		FreeformTags:       c.buildFreeformTags(nodeClaim),
		DefinedTags:        c.buildDefinedTags(nodeClaim),
	}

	if isPreemptible {
		details.PreemptibleInstanceConfig = &PreemptibleInstanceConfig{
			PreemptionAction: "TERMINATE",
		}
	}

	var instance *Instance
	err := WithRetry(ctx, DefaultRetryConfig(), "launch-flexible-instance", func() error {
		// Simulate instance launch with potential failures
		if shouldSimulateError() {
			return ErrShapeNotAvailable
		}
		
		instance = &Instance{
			ID:                 fmt.Sprintf("ocid1.instance.oc1.%s.%s", c.config.Region, generateID()),
			Shape:              shape,
			ShapeConfig:        shapeConfig,
			ImageID:            details.ImageID,
			CompartmentID:      details.CompartmentID,
			AvailabilityDomain: details.AvailabilityDomain,
			State:              "RUNNING",
			TimeCreated:        time.Now(),
			IsPreemptible:      isPreemptible,
			Metadata:           details.Metadata,
			FreeformTags:       details.FreeformTags,
			DefinedTags:        details.DefinedTags,
		}
		return nil
	})
	
	if err != nil {
		return nil, HandleError(ctx, err, "launch-flexible-instance")
	}

	return instance, nil
}

// TerminateInstance terminates an instance
func (c *Client) TerminateInstance(ctx context.Context, instanceID string) error {
	logger := log.FromContext(ctx)
	logger.Info("terminating OCI instance", "instanceID", instanceID)

	// In real implementation, this would call OCI API
	// For now, we'll simulate success
	return nil
}

// GetInstance retrieves instance details
func (c *Client) GetInstance(ctx context.Context, instanceID string) (*Instance, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("getting OCI instance", "instanceID", instanceID)

	// In real implementation, this would call OCI API
	// For now, return not found
	return nil, &NotFoundError{
		ResourceType: "Instance",
		ResourceID:   instanceID,
	}
}

// ListInstances lists all instances in the compartment
func (c *Client) ListInstances(ctx context.Context) ([]*Instance, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("listing OCI instances", "compartmentID", c.config.CompartmentID)

	// In real implementation, this would call OCI API
	// For now, return empty list
	return []*Instance{}, nil
}

// ListShapes lists available shapes
func (c *Client) ListShapes(ctx context.Context) ([]*Shape, error) {
	// In real implementation, this would call OCI API
	// For now, return some example shapes
	return []*Shape{
		{
			Name:               "VM.Standard.E4.Flex",
			IsFlexible:         true,
			OCPUOptions:        &OCPUOptions{Min: 1, Max: 64},
			MemoryOptions:      &MemoryOptions{MinInGBs: 1, MaxInGBs: 1024, DefaultPerOCPUInGBs: 16},
			NetworkingBandwidthInGbps: 1,
		},
		{
			Name:               "VM.Standard.E5.Flex",
			IsFlexible:         true,
			OCPUOptions:        &OCPUOptions{Min: 1, Max: 94},
			MemoryOptions:      &MemoryOptions{MinInGBs: 1, MaxInGBs: 1024, DefaultPerOCPUInGBs: 16},
			NetworkingBandwidthInGbps: 2,
		},
		{
			Name:               "VM.Standard2.1",
			OCPUs:              1,
			MemoryInGBs:        15,
			IsFlexible:         false,
			NetworkingBandwidthInGbps: 1,
		},
	}, nil
}

// WaitForInstanceReady waits for an instance to be ready
func (c *Client) WaitForInstanceReady(ctx context.Context, instanceID string) error {
	logger := log.FromContext(ctx)
	logger.Info("waiting for instance to be ready", "instanceID", instanceID)

	// Wait with exponential backoff
	return wait.ExponentialBackoff(wait.Backoff{
		Duration: 5 * time.Second,
		Factor:   1.5,
		Jitter:   0.1,
		Steps:    20,
		Cap:      2 * time.Minute,
	}, func() (bool, error) {
		// In real implementation, check instance state
		// For now, simulate immediate readiness
		return true, nil
	})
}

// Helper methods

func (c *Client) selectAvailabilityDomain() string {
	// In real implementation, this would select based on capacity and spread
	return fmt.Sprintf("AD-%d", 1)
}

func (c *Client) selectSubnet() string {
	// In real implementation, this would select based on capacity and spread
	if len(c.config.SubnetIDs) > 0 {
		return c.config.SubnetIDs[0]
	}
	return ""
}

func (c *Client) selectFlexibleShape(nodeClaim *v1.NodeClaim) string {
	// Extract from nodepool annotations or use default
	// In real implementation, this would check NodePool's allowed shapes
	if len(c.config.DefaultShapes) > 0 {
		for _, shape := range c.config.DefaultShapes {
			if isFlexibleShape(shape) {
				return shape
			}
		}
	}
	return "VM.Standard.E4.Flex"
}

func (c *Client) shouldUsePreemptible(nodeClaim *v1.NodeClaim) bool {
	// Check for capacity type label
	if capacityType, ok := nodeClaim.Labels[v1.CapacityTypeLabelKey]; ok {
		return capacityType == "preemptible" || capacityType == "spot"
	}
	return false
}

func (c *Client) buildMetadata(nodeClaim *v1.NodeClaim) map[string]string {
	metadata := make(map[string]string)
	
	// Add standard metadata
	metadata["karpenter.sh/nodeclaim"] = nodeClaim.Name
	metadata["karpenter.sh/nodepool"] = nodeClaim.Labels[v1.NodePoolLabelKey]
	
	// Add user data for node initialization
	// In real implementation, this would include cloud-init scripts
	
	return metadata
}

func (c *Client) buildFreeformTags(nodeClaim *v1.NodeClaim) map[string]string {
	tags := make(map[string]string)
	
	// Add Karpenter tags
	tags["karpenter.sh/discovery"] = nodeClaim.Labels[v1.NodePoolLabelKey]
	tags["karpenter.sh/nodeclaim"] = nodeClaim.Name
	tags["Name"] = fmt.Sprintf("karpenter-%s", nodeClaim.Name)
	
	// Copy relevant labels as tags
	for k, v := range nodeClaim.Labels {
		if isValidTagKey(k) {
			tags[k] = v
		}
	}
	
	return tags
}

func (c *Client) buildDefinedTags(nodeClaim *v1.NodeClaim) map[string]map[string]interface{} {
	// In real implementation, this would build OCI defined tags
	// based on organization's tagging strategy
	return make(map[string]map[string]interface{})
}

func isFlexibleShape(shape string) bool {
	return len(shape) > 4 && (shape[len(shape)-4:] == "Flex" || shape[len(shape)-4:] == "flex")
}

func isValidTagKey(key string) bool {
	// OCI has specific tag key requirements
	// This is a simplified validation
	return len(key) > 0 && len(key) <= 100
}

func generateID() string {
	// In real implementation, this would be handled by OCI
	// For testing, generate a simple ID
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func shouldSimulateError() bool {
	// For testing purposes, randomly simulate errors
	// In production, this would always return false
	return false
}