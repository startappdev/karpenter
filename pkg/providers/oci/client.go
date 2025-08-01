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
	"encoding/base64"
	"fmt"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// Client wraps OCI API operations
type Client struct {
	config         *Config
	computeClient  core.ComputeClient
	configProvider common.ConfigurationProvider
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

	// Create configuration provider based on auth type
	var configProvider common.ConfigurationProvider
	var err error

	switch config.AuthType {
	case "instance_principal":
		// Use instance principal authentication
		configProvider, err = auth.InstancePrincipalConfigurationProvider()
		if err != nil {
			return nil, fmt.Errorf("creating instance principal configuration provider: %w", err)
		}
	case "user_principal":
		// Use user principal authentication with provided credentials
		configProvider = common.NewRawConfigurationProvider(
			config.TenancyOCID,
			config.UserOCID,
			config.Region,
			config.Fingerprint,
			config.PrivateKey,
			&config.Passphrase,
		)
	default:
		return nil, fmt.Errorf("unsupported auth type: %s", config.AuthType)
	}

	// Create compute client
	computeClient, err := core.NewComputeClientWithConfigurationProvider(configProvider)
	if err != nil {
		return nil, fmt.Errorf("creating compute client: %w", err)
	}

	// Override region if specified in config
	if config.Region != "" {
		computeClient.SetRegion(config.Region)
	}

	return &Client{
		config:         config,
		computeClient:  computeClient,
		configProvider: configProvider,
	}, nil
}

// LaunchInstance launches a standard instance with fixed shape
func (c *Client) LaunchInstance(ctx context.Context, nodeClaim *v1.NodeClaim, shape string) (*Instance, error) {
	logger := log.FromContext(ctx)
	logger.Info("launching OCI instance", "shape", shape)

	// Get availability domains for the compartment
	ad, err := c.getAvailabilityDomain(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting availability domain: %w", err)
	}

	var ociInstance core.Instance
	err = WithRetry(ctx, DefaultRetryConfig(), "launch-instance", func() error {
		// Create launch instance request
		request := core.LaunchInstanceRequest{
			LaunchInstanceDetails: core.LaunchInstanceDetails{
				AvailabilityDomain: &ad,
				CompartmentId:      &c.config.CompartmentID,
				Shape:              &shape,
				DisplayName:        common.String(fmt.Sprintf("karpenter-%s", nodeClaim.Name)),
				
				// Source details - using platform image
				SourceDetails: &core.InstanceSourceViaImageDetails{
					ImageId: &c.config.ImageID,
				},
				
				// Network configuration
				CreateVnicDetails: &core.CreateVnicDetails{
					SubnetId:       common.String(c.selectSubnet()),
					AssignPublicIp: common.Bool(false),
					DisplayName:    common.String(fmt.Sprintf("karpenter-%s", nodeClaim.Name)),
				},
				
				// Metadata including cloud-init user_data
				Metadata:     c.buildMetadata(nodeClaim),
				FreeformTags: c.buildFreeformTags(nodeClaim),
				DefinedTags:  c.buildDefinedTags(nodeClaim),
			},
		}

		// Launch the instance
		response, err := c.computeClient.LaunchInstance(ctx, request)
		if err != nil {
			return WrapOCIError(err, "instance")
		}

		ociInstance = response.Instance
		return nil
	})
	
	if err != nil {
		return nil, HandleError(ctx, err, "launch-instance")
	}

	// Convert OCI instance to our Instance type
	instance := &Instance{
		ID:                 *ociInstance.Id,
		Shape:              *ociInstance.Shape,
		ImageID:            c.config.ImageID,
		CompartmentID:      *ociInstance.CompartmentId,
		AvailabilityDomain: *ociInstance.AvailabilityDomain,
		State:              string(ociInstance.LifecycleState),
		TimeCreated:        ociInstance.TimeCreated.Time,
		Metadata:           ociInstance.Metadata,
		FreeformTags:       ociInstance.FreeformTags,
		DefinedTags:        ociInstance.DefinedTags,
	}

	// Wait for instance to be running
	if err := c.WaitForInstanceReady(ctx, instance.ID); err != nil {
		return nil, fmt.Errorf("waiting for instance ready: %w", err)
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

	// Get availability domains for the compartment
	ad, err := c.getAvailabilityDomain(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting availability domain: %w", err)
	}

	// Determine capacity type from nodeClaim labels or annotations
	isPreemptible := c.shouldUsePreemptible(nodeClaim)
	
	var ociInstance core.Instance
	err = WithRetry(ctx, DefaultRetryConfig(), "launch-flexible-instance", func() error {
		// Create launch instance request
		request := core.LaunchInstanceRequest{
			LaunchInstanceDetails: core.LaunchInstanceDetails{
				AvailabilityDomain: &ad,
				CompartmentId:      &c.config.CompartmentID,
				Shape:              &shape,
				DisplayName:        common.String(fmt.Sprintf("karpenter-%s", nodeClaim.Name)),
				
				// Shape configuration for flexible shapes
				ShapeConfig: &core.LaunchInstanceShapeConfigDetails{
					Ocpus:       common.Float32(float32(*shapeConfig.OCPUs)),
					MemoryInGBs: common.Float32(float32(*shapeConfig.MemoryInGBs)),
				},
				
				// Source details - using platform image
				SourceDetails: &core.InstanceSourceViaImageDetails{
					ImageId: &c.config.ImageID,
				},
				
				// Network configuration
				CreateVnicDetails: &core.CreateVnicDetails{
					SubnetId:       common.String(c.selectSubnet()),
					AssignPublicIp: common.Bool(false),
					DisplayName:    common.String(fmt.Sprintf("karpenter-%s", nodeClaim.Name)),
				},
				
				// Metadata including cloud-init user_data
				Metadata:     c.buildMetadata(nodeClaim),
				FreeformTags: c.buildFreeformTags(nodeClaim),
				DefinedTags:  c.buildDefinedTags(nodeClaim),
			},
		}

		// Add preemptible configuration if needed
		if isPreemptible {
			request.LaunchInstanceDetails.PreemptibleInstanceConfig = &core.PreemptibleInstanceConfigDetails{
				PreemptionAction: core.TerminatePreemptionAction{
					PreserveBootVolume: common.Bool(false),
				},
			}
		}

		// Launch the instance
		response, err := c.computeClient.LaunchInstance(ctx, request)
		if err != nil {
			return WrapOCIError(err, "instance")
		}

		ociInstance = response.Instance
		return nil
	})
	
	if err != nil {
		return nil, HandleError(ctx, err, "launch-flexible-instance")
	}

	// Convert OCI instance to our Instance type
	instance := &Instance{
		ID:                 *ociInstance.Id,
		Shape:              *ociInstance.Shape,
		ShapeConfig:        shapeConfig,
		ImageID:            c.config.ImageID,
		CompartmentID:      *ociInstance.CompartmentId,
		AvailabilityDomain: *ociInstance.AvailabilityDomain,
		State:              string(ociInstance.LifecycleState),
		TimeCreated:        ociInstance.TimeCreated.Time,
		IsPreemptible:      isPreemptible,
		Metadata:           ociInstance.Metadata,
		FreeformTags:       ociInstance.FreeformTags,
		DefinedTags:        ociInstance.DefinedTags,
	}

	// Wait for instance to be running
	if err := c.WaitForInstanceReady(ctx, instance.ID); err != nil {
		return nil, fmt.Errorf("waiting for instance ready: %w", err)
	}

	return instance, nil
}

// TerminateInstance terminates an instance
func (c *Client) TerminateInstance(ctx context.Context, instanceID string) error {
	logger := log.FromContext(ctx)
	logger.Info("terminating OCI instance", "instanceID", instanceID)

	return WithRetry(ctx, DefaultRetryConfig(), "terminate-instance", func() error {
		request := core.TerminateInstanceRequest{
			InstanceId:         &instanceID,
			PreserveBootVolume: common.Bool(false),
		}

		_, err := c.computeClient.TerminateInstance(ctx, request)
		if err != nil {
			return WrapOCIError(err, "instance")
		}

		return nil
	})
}

// GetInstance retrieves instance details
func (c *Client) GetInstance(ctx context.Context, instanceID string) (*Instance, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("getting OCI instance", "instanceID", instanceID)

	request := core.GetInstanceRequest{
		InstanceId: &instanceID,
	}

	response, err := c.computeClient.GetInstance(ctx, request)
	if err != nil {
		return nil, WrapOCIError(err, "instance")
	}

	ociInstance := response.Instance

	// Convert OCI instance to our Instance type
	instance := &Instance{
		ID:                 *ociInstance.Id,
		Shape:              *ociInstance.Shape,
		ImageID:            getImageIDFromSourceDetails(ociInstance.SourceDetails),
		CompartmentID:      *ociInstance.CompartmentId,
		AvailabilityDomain: *ociInstance.AvailabilityDomain,
		State:              string(ociInstance.LifecycleState),
		TimeCreated:        ociInstance.TimeCreated.Time,
		Metadata:           ociInstance.Metadata,
		FreeformTags:       ociInstance.FreeformTags,
		DefinedTags:        ociInstance.DefinedTags,
	}

	// Set shape config if available
	if ociInstance.ShapeConfig != nil {
		instance.ShapeConfig = &ShapeConfig{
			OCPUs:       lo.ToPtr(int32(*ociInstance.ShapeConfig.Ocpus)),
			MemoryInGBs: lo.ToPtr(int32(*ociInstance.ShapeConfig.MemoryInGBs)),
		}
	}

	return instance, nil
}

// ListInstances lists all instances in the compartment
func (c *Client) ListInstances(ctx context.Context) ([]*Instance, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("listing OCI instances", "compartmentID", c.config.CompartmentID)

	var instances []*Instance

	request := core.ListInstancesRequest{
		CompartmentId: &c.config.CompartmentID,
		Limit:         common.Int(1000),
	}

	for {
		response, err := c.computeClient.ListInstances(ctx, request)
		if err != nil {
			return nil, WrapOCIError(err, "instances")
		}

		for _, ociInstance := range response.Items {
			instance := &Instance{
				ID:                 *ociInstance.Id,
				Shape:              *ociInstance.Shape,
				CompartmentID:      *ociInstance.CompartmentId,
				AvailabilityDomain: *ociInstance.AvailabilityDomain,
				State:              string(ociInstance.LifecycleState),
				TimeCreated:        ociInstance.TimeCreated.Time,
				Metadata:           ociInstance.Metadata,
				FreeformTags:       ociInstance.FreeformTags,
				DefinedTags:        ociInstance.DefinedTags,
			}

			// Set shape config if available
			if ociInstance.ShapeConfig != nil {
				instance.ShapeConfig = &ShapeConfig{
					OCPUs:       lo.ToPtr(int32(*ociInstance.ShapeConfig.Ocpus)),
					MemoryInGBs: lo.ToPtr(int32(*ociInstance.ShapeConfig.MemoryInGBs)),
				}
			}

			instances = append(instances, instance)
		}

		if response.OpcNextPage == nil {
			break
		}

		request.Page = response.OpcNextPage
	}

	return instances, nil
}

// ListShapes lists available shapes
func (c *Client) ListShapes(ctx context.Context) ([]*Shape, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("listing available shapes", "compartmentID", c.config.CompartmentID)

	// Get first availability domain
	ad, err := c.getAvailabilityDomain(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting availability domain: %w", err)
	}

	var shapes []*Shape

	request := core.ListShapesRequest{
		CompartmentId:      &c.config.CompartmentID,
		AvailabilityDomain: &ad,
		Limit:              common.Int(1000),
	}

	for {
		response, err := c.computeClient.ListShapes(ctx, request)
		if err != nil {
			return nil, WrapOCIError(err, "shapes")
		}

		for _, ociShape := range response.Items {
			shape := &Shape{
				Name:        *ociShape.Shape,
				IsFlexible:  isFlexibleShape(*ociShape.Shape),
			}

			// Set fixed shape properties
			if ociShape.Ocpus != nil {
				shape.OCPUs = *ociShape.Ocpus
			}
			if ociShape.MemoryInGBs != nil {
				shape.MemoryInGBs = *ociShape.MemoryInGBs
			}
			if ociShape.NetworkingBandwidthInGbps != nil {
				shape.NetworkingBandwidthInGbps = *ociShape.NetworkingBandwidthInGbps
			}

			// Set flexible shape options
			if ociShape.OcpuOptions != nil {
				shape.OCPUOptions = &OCPUOptions{
					Min: *ociShape.OcpuOptions.Min,
					Max: *ociShape.OcpuOptions.Max,
				}
			}
			if ociShape.MemoryOptions != nil {
				shape.MemoryOptions = &MemoryOptions{
					MinInGBs:            *ociShape.MemoryOptions.MinInGBs,
					MaxInGBs:            *ociShape.MemoryOptions.MaxInGBs,
					DefaultPerOCPUInGBs: *ociShape.MemoryOptions.DefaultPerOcpuInGBs,
				}
				if ociShape.MemoryOptions.MinPerOcpuInGBs != nil {
					shape.MemoryOptions.MinPerOCPUInGBs = *ociShape.MemoryOptions.MinPerOcpuInGBs
				}
				if ociShape.MemoryOptions.MaxPerOcpuInGBs != nil {
					shape.MemoryOptions.MaxPerOCPUInGBs = *ociShape.MemoryOptions.MaxPerOcpuInGBs
				}
			}

			shapes = append(shapes, shape)
		}

		if response.OpcNextPage == nil {
			break
		}

		request.Page = response.OpcNextPage
	}

	return shapes, nil
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
		Steps:    40, // Increased from 20 to allow more time for instance startup
		Cap:      2 * time.Minute,
	}, func() (bool, error) {
		request := core.GetInstanceRequest{
			InstanceId: &instanceID,
		}

		response, err := c.computeClient.GetInstance(ctx, request)
		if err != nil {
			// Don't retry on not found errors
			if IsNotFoundError(WrapOCIError(err, "instance")) {
				return false, err
			}
			// Retry on other errors
			logger.V(1).Info("error getting instance state, will retry", "error", err)
			return false, nil
		}

		state := response.Instance.LifecycleState
		logger.V(1).Info("instance state", "state", state)

		switch state {
		case core.InstanceLifecycleStateRunning:
			return true, nil
		case core.InstanceLifecycleStateTerminated, core.InstanceLifecycleStateTerminating:
			return false, fmt.Errorf("instance entered terminated state")
		default:
			// Continue waiting for other states
			return false, nil
		}
	})
}

// Helper methods

// getAvailabilityDomain gets the first available availability domain
func (c *Client) getAvailabilityDomain(ctx context.Context) (string, error) {
	request := identity.ListAvailabilityDomainsRequest{
		CompartmentId: &c.config.CompartmentID,
	}

	// We need to create an identity client for this
	identityClient, err := identity.NewIdentityClientWithConfigurationProvider(c.configProvider)
	if err != nil {
		return "", fmt.Errorf("creating identity client: %w", err)
	}
	// Note: IdentityClient doesn't have a Close method in the OCI SDK

	response, err := identityClient.ListAvailabilityDomains(ctx, request)
	if err != nil {
		return "", WrapOCIError(err, "availability domains")
	}

	if len(response.Items) == 0 {
		return "", fmt.Errorf("no availability domains found")
	}

	// For now, return the first AD. In production, this should be more sophisticated
	// based on capacity, spread, and fault domain distribution
	return *response.Items[0].Name, nil
}

func (c *Client) selectAvailabilityDomain() string {
	// This is a fallback for backward compatibility
	// Real implementation should use getAvailabilityDomain
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
	// OKE requires cloud-init to bootstrap nodes and join them to the cluster
	// The oke_init_script is a special metadata key that OKE uses to bootstrap nodes
	cloudInitScript := `#!/bin/bash
# OKE Node Bootstrap Script
# This script is executed by cloud-init to join the node to the OKE cluster

# Wait for OKE metadata to be available
attempts=0
while [ $attempts -lt 60 ]; do
    if curl -f -H "Authorization: Bearer Oracle" http://169.254.169.254/opc/v2/instance/metadata/oke_init_script > /dev/null 2>&1; then
        break
    fi
    echo "Waiting for OKE metadata service..."
    sleep 5
    attempts=$((attempts + 1))
done

# Download and execute the OKE initialization script
curl --fail -H "Authorization: Bearer Oracle" -L0 http://169.254.169.254/opc/v2/instance/metadata/oke_init_script | base64 --decode >/var/run/oke-init.sh

# Make the script executable and run it
chmod +x /var/run/oke-init.sh
bash /var/run/oke-init.sh

# Log the result
if [ $? -eq 0 ]; then
    echo "OKE node initialization completed successfully"
else
    echo "OKE node initialization failed"
    exit 1
fi
`
	
	// In OCI, user_data must be base64-encoded
	metadata["user_data"] = base64.StdEncoding.EncodeToString([]byte(cloudInitScript))
	
	// Add any additional metadata that might be needed for OKE
	// For example, if there's an SSH key specified
	if sshKey, ok := nodeClaim.Annotations["karpenter.sh/ssh-key"]; ok {
		metadata["ssh_authorized_keys"] = sshKey
	}
	
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

// getImageIDFromSourceDetails extracts the image ID from instance source details
func getImageIDFromSourceDetails(sourceDetails core.InstanceSourceDetails) string {
	// OCI uses polymorphic types for source details
	// We need to type assert to get the image ID
	if sourceDetails == nil {
		return ""
	}
	
	// The source details could be of different types, but for instances
	// launched from images, it will be InstanceSourceViaImageDetails
	switch sd := sourceDetails.(type) {
	case *core.InstanceSourceViaImageDetails:
		if sd.ImageId != nil {
			return *sd.ImageId
		}
	case core.InstanceSourceViaImageDetails:
		if sd.ImageId != nil {
			return *sd.ImageId
		}
	}
	
	return ""
}