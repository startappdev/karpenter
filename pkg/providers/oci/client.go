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
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/containerengine"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/providers/oci/apis/v1alpha1"
)

// AvailabilityDomainCache caches availability domains to avoid repeated API calls
type AvailabilityDomainCache struct {
	domains   []string
	lastFetch time.Time
	mutex     sync.RWMutex
	ttl       time.Duration
}

// Client wraps OCI API operations
type Client struct {
	config               *Config
	computeClient        core.ComputeClient
	containerEngineClient containerengine.ContainerEngineClient
	configProvider       common.ConfigurationProvider
	adCache              *AvailabilityDomainCache
	requestDeduplicator  *RequestDeduplicator
}

// RequestDeduplicator prevents concurrent identical requests
type RequestDeduplicator struct {
	inflight map[string]chan result
	mutex    sync.Mutex
}

type result struct {
	value []string
	err   error
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

	// Create container engine client
	containerEngineClient, err := containerengine.NewContainerEngineClientWithConfigurationProvider(configProvider)
	if err != nil {
		return nil, fmt.Errorf("creating container engine client: %w", err)
	}

	// Override region if specified in config
	if config.Region != "" {
		computeClient.SetRegion(config.Region)
		containerEngineClient.SetRegion(config.Region)
	}

	return &Client{
		config:               config,
		computeClient:        computeClient,
		containerEngineClient: containerEngineClient,
		configProvider:       configProvider,
		adCache: &AvailabilityDomainCache{
			ttl: 1 * time.Hour, // Cache ADs for 1 hour since they rarely change
		},
		requestDeduplicator: &RequestDeduplicator{
			inflight: make(map[string]chan result),
		},
	}, nil
}

// LaunchInstance launches a standard instance with fixed shape
func (c *Client) LaunchInstance(ctx context.Context, nodeClaim *v1.NodeClaim, nodeClass *v1alpha1.OCINodeClass, nodePool *v1.NodePool, shape string) (*Instance, error) {
	logger := log.FromContext(ctx)
	logger.Info("launching OCI instance", 
		"shape", shape,
		"nodeClaim", nodeClaim.Name,
		"nodePool", nodeClaim.Labels[v1.NodePoolLabelKey],
		"compartmentID", c.config.CompartmentID,
		"clusterID", c.config.ClusterID,
		"imageID", nodeClass.Spec.ImageID)

	// Get availability domains for the compartment
	ad, err := c.getAvailabilityDomain(ctx)
	if err != nil {
		logger.Error(err, "failed to get availability domain")
		return nil, fmt.Errorf("getting availability domain: %w", err)
	}
	logger.Info("selected availability domain", "ad", ad)

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
					ImageId: &nodeClass.Spec.ImageID,
				},
				
				// Network configuration
				CreateVnicDetails: &core.CreateVnicDetails{
					SubnetId:       common.String(c.selectSubnet(nodeClass)),
					AssignPublicIp: common.Bool(false),
					DisplayName:    common.String(fmt.Sprintf("karpenter-%s", nodeClaim.Name)),
				},
				
				// Metadata including cloud-init user_data
				Metadata:     c.buildMetadata(nodeClaim, nodePool),
				FreeformTags: c.buildFreeformTags(nodeClaim),
				DefinedTags:  c.buildDefinedTags(nodeClaim),
			},
		}

		// Handle flexible shapes - extract base shape and config
		actualShape := shape
		if isFlexibleShape(shape) {
			// Parse shape name to extract OCPUs and memory
			// Format: VM.Standard.E4.Flex-1-16 (1 OCPU, 16GB memory)
			var ocpus, memory int32 = 1, 16
			
			// Extract base shape name and config
			parts := strings.Split(shape, "-")
			if len(parts) >= 3 {
				// Get base shape (e.g., VM.Standard.E4.Flex)
				actualShape = strings.Join(parts[:len(parts)-2], "-")
				
				// Try to parse CPU and memory from the last two parts
				if cpu, err := strconv.Atoi(parts[len(parts)-2]); err == nil {
					ocpus = int32(cpu)
				}
				if mem, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
					memory = int32(mem)
				}
			}
			
			request.LaunchInstanceDetails.ShapeConfig = &core.LaunchInstanceShapeConfigDetails{
				Ocpus:       common.Float32(float32(ocpus)),
				MemoryInGBs: common.Float32(float32(memory)),
			}
			logger.Info("added shape config for flexible shape", 
				"originalShape", shape, "actualShape", actualShape, "ocpus", ocpus, "memory", memory)
		}
		
		// Update the shape in the request
		request.LaunchInstanceDetails.Shape = &actualShape

		// Log the launch request details including shape config
		logFields := []interface{}{
			"displayName", *request.LaunchInstanceDetails.DisplayName,
			"subnet", *request.LaunchInstanceDetails.CreateVnicDetails.SubnetId,
			"shape", actualShape,
		}
		if request.LaunchInstanceDetails.ShapeConfig != nil {
			logFields = append(logFields,
				"shapeConfig.ocpus", *request.LaunchInstanceDetails.ShapeConfig.Ocpus,
				"shapeConfig.memory", *request.LaunchInstanceDetails.ShapeConfig.MemoryInGBs)
		}
		logger.Info("sending launch instance request", logFields...)

		// Launch the instance
		response, err := c.computeClient.LaunchInstance(ctx, request)
		if err != nil {
			logger.Error(err, "failed to launch instance")
			return WrapOCIError(err, "instance")
		}

		ociInstance = response.Instance
		logger.Info("instance launched successfully",
			"instanceID", *ociInstance.Id,
			"lifecycleState", ociInstance.LifecycleState)
		return nil
	})
	
	if err != nil {
		logger.Error(err, "failed to launch instance after retries")
		return nil, HandleError(ctx, err, "launch-instance")
	}

	// Convert OCI instance to our Instance type
	instance := &Instance{
		ID:                 *ociInstance.Id,
		Shape:              *ociInstance.Shape,
		ImageID:            nodeClass.Spec.ImageID,
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
func (c *Client) LaunchFlexibleInstance(ctx context.Context, nodeClaim *v1.NodeClaim, nodeClass *v1alpha1.OCINodeClass, nodePool *v1.NodePool, shapeConfig *ShapeConfig) (*Instance, error) {
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
					ImageId: &nodeClass.Spec.ImageID,
				},
				
				// Network configuration
				CreateVnicDetails: &core.CreateVnicDetails{
					SubnetId:       common.String(c.selectSubnet(nodeClass)),
					AssignPublicIp: common.Bool(false),
					DisplayName:    common.String(fmt.Sprintf("karpenter-%s", nodeClaim.Name)),
				},
				
				// Metadata including cloud-init user_data
				Metadata:     c.buildMetadata(nodeClaim, nodePool),
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
		ImageID:            nodeClass.Spec.ImageID,
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

// GetClusterDetails retrieves OKE cluster information including endpoint and certificates
func (c *Client) GetClusterDetails(ctx context.Context) (*containerengine.Cluster, error) {
	logger := log.FromContext(ctx)
	logger.Info("getting OKE cluster details", "clusterID", c.config.ClusterID)

	if c.config.ClusterID == "" {
		return nil, fmt.Errorf("cluster ID is not configured")
	}

	request := containerengine.GetClusterRequest{
		ClusterId: &c.config.ClusterID,
	}

	response, err := c.containerEngineClient.GetCluster(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("getting cluster details: %w", err)
	}

	return &response.Cluster, nil
}

// CreateClusterKubeconfig generates kubeconfig for the cluster
func (c *Client) CreateClusterKubeconfig(ctx context.Context) (string, error) {
	logger := log.FromContext(ctx)
	logger.Info("creating cluster kubeconfig", "clusterID", c.config.ClusterID)

	request := containerengine.CreateKubeconfigRequest{
		ClusterId: &c.config.ClusterID,
	}

	response, err := c.containerEngineClient.CreateKubeconfig(ctx, request)
	if err != nil {
		return "", fmt.Errorf("creating kubeconfig: %w", err)
	}

	// Read the kubeconfig from the response
	buf := new(strings.Builder)
	_, err = io.Copy(buf, response.Content)
	if err != nil {
		return "", fmt.Errorf("reading kubeconfig: %w", err)
	}

	return buf.String(), nil
}

// Helper methods

// getAvailabilityDomain gets the first available availability domain with caching and deduplication
func (c *Client) getAvailabilityDomain(ctx context.Context) (string, error) {
	logger := log.FromContext(ctx)
	
	// Try cache first
	c.adCache.mutex.RLock()
	if len(c.adCache.domains) > 0 && time.Since(c.adCache.lastFetch) < c.adCache.ttl {
		domain := c.adCache.domains[0]
		c.adCache.mutex.RUnlock()
		logger.V(1).Info("using cached availability domain", "domain", domain)
		return domain, nil
	}
	c.adCache.mutex.RUnlock()

	// Cache expired or empty, fetch new data with deduplication
	domains, err := c.getAvailabilityDomainsWithDeduplication(ctx)
	if err != nil {
		return "", err
	}

	if len(domains) == 0 {
		return "", fmt.Errorf("no availability domains found")
	}

	// For now, return the first AD. In production, this should be more sophisticated
	// based on capacity, spread, and fault domain distribution
	return domains[0], nil
}

// getAvailabilityDomainsWithDeduplication fetches availability domains with request deduplication
func (c *Client) getAvailabilityDomainsWithDeduplication(ctx context.Context) ([]string, error) {
	logger := log.FromContext(ctx)
	key := fmt.Sprintf("ad-%s", c.config.CompartmentID)

	c.requestDeduplicator.mutex.Lock()
	if ch, exists := c.requestDeduplicator.inflight[key]; exists {
		// Another request is in flight, wait for it
		c.requestDeduplicator.mutex.Unlock()
		logger.V(1).Info("waiting for inflight availability domain request")
		select {
		case res := <-ch:
			return res.value, res.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// No inflight request, start new one
	ch := make(chan result, 1)
	c.requestDeduplicator.inflight[key] = ch
	c.requestDeduplicator.mutex.Unlock()

	// Clean up when done
	defer func() {
		c.requestDeduplicator.mutex.Lock()
		delete(c.requestDeduplicator.inflight, key)
		c.requestDeduplicator.mutex.Unlock()
	}()

	logger.V(1).Info("fetching availability domains from OCI API")
	domains, err := c.fetchAvailabilityDomainsFromAPI(ctx)
	
	// Send result to all waiters
	res := result{value: domains, err: err}
	select {
	case ch <- res:
	default:
	}

	if err == nil && len(domains) > 0 {
		// Update cache
		c.adCache.mutex.Lock()
		c.adCache.domains = domains
		c.adCache.lastFetch = time.Now()
		c.adCache.mutex.Unlock()
		logger.Info("cached availability domains", "count", len(domains), "domains", domains)
	}

	return domains, err
}

// fetchAvailabilityDomainsFromAPI makes the actual API call to OCI
func (c *Client) fetchAvailabilityDomainsFromAPI(ctx context.Context) ([]string, error) {
	logger := log.FromContext(ctx)
	
	request := identity.ListAvailabilityDomainsRequest{
		CompartmentId: &c.config.CompartmentID,
	}

	// Create identity client
	identityClient, err := identity.NewIdentityClientWithConfigurationProvider(c.configProvider)
	if err != nil {
		return nil, fmt.Errorf("creating identity client: %w", err)
	}

	// Add exponential backoff for rate limiting
	var domains []string
	err = wait.ExponentialBackoff(wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   2.0,
		Jitter:   0.1,
		Steps:    5,
		Cap:      30 * time.Second,
	}, func() (bool, error) {
		response, apiErr := identityClient.ListAvailabilityDomains(ctx, request)
		if apiErr != nil {
			wrappedErr := WrapOCIError(apiErr, "availability domains")
			if IsRateLimitError(wrappedErr) {
				logger.V(1).Info("rate limited on availability domains API, retrying", "error", apiErr)
				return false, nil // Retry
			}
			return false, wrappedErr // Don't retry on other errors
		}

		// Success - extract domain names
		for _, item := range response.Items {
			if item.Name != nil {
				domains = append(domains, *item.Name)
			}
		}
		return true, nil
	})

	if err != nil {
		logger.Error(err, "failed to fetch availability domains after retries")
		return nil, err
	}

	logger.Info("successfully fetched availability domains", "count", len(domains))
	return domains, nil
}

func (c *Client) selectAvailabilityDomain() string {
	// This is a fallback for backward compatibility
	// Real implementation should use getAvailabilityDomain
	return fmt.Sprintf("AD-%d", 1)
}

func (c *Client) selectSubnet(nodeClass *v1alpha1.OCINodeClass) string {
	// Use subnets from NodeClass if specified, otherwise fall back to config
	if len(nodeClass.Spec.SubnetIDs) > 0 {
		// In real implementation, this would select based on capacity and spread
		return nodeClass.Spec.SubnetIDs[0]
	}
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

// buildNodeLabelsArgs creates the kubelet node-labels argument from NodePool template and Karpenter labels
func (c *Client) buildNodeLabelsArgs(nodeClaim *v1.NodeClaim, nodePool *v1.NodePool) string {
	// Start with basic Karpenter labels
	labels := []string{
		fmt.Sprintf("karpenter.sh/nodeclaim=%s", nodeClaim.Name),
		fmt.Sprintf("karpenter.sh/nodepool=%s", nodeClaim.Labels[v1.NodePoolLabelKey]),
		"karpenter.sh/managed=true",
	}
	
	// Add NodePool template labels if NodePool is available
	if nodePool != nil && nodePool.Spec.Template.ObjectMeta.Labels != nil {
		for k, v := range nodePool.Spec.Template.ObjectMeta.Labels {
			// Keep label keys as-is for Kubernetes node labels
			labels = append(labels, fmt.Sprintf("%s=%s", k, v))
		}
	}
	
	return strings.Join(labels, ",")
}

// buildNodeTaintsArgs creates the kubelet register-with-taints argument from NodePool template taints
func (c *Client) buildNodeTaintsArgs(nodePool *v1.NodePool) string {
	if nodePool == nil || nodePool.Spec.Template.Spec.Taints == nil {
		return ""
	}
	
	var taints []string
	for _, taint := range nodePool.Spec.Template.Spec.Taints {
		// Format: key=value:effect
		taintStr := fmt.Sprintf("%s=%s:%s", taint.Key, taint.Value, taint.Effect)
		taints = append(taints, taintStr)
	}
	
	if len(taints) > 0 {
		return fmt.Sprintf(" --register-with-taints=%s", strings.Join(taints, ","))
	}
	return ""
}

func (c *Client) buildMetadata(nodeClaim *v1.NodeClaim, nodePool *v1.NodePool) map[string]string {
	logger := log.Log.WithValues("nodeClaim", nodeClaim.Name)
	metadata := make(map[string]string)
	
	// Add standard metadata - OCI doesn't allow "/" in metadata keys
	metadata["karpenter_nodeclaim"] = nodeClaim.Name
	metadata["karpenter_nodepool"] = nodeClaim.Labels[v1.NodePoolLabelKey]
	
	logger.Info("building metadata for instance",
		"clusterID", c.config.ClusterID,
		"nodeClaim", nodeClaim.Name)
	
	// Get cluster details for bootstrap script
	ctx := context.Background()
	cluster, err := c.GetClusterDetails(ctx)
	if err != nil {
		// Log error but continue with basic metadata
		logger.Error(err, "failed to get cluster details for bootstrap script",
			"clusterID", c.config.ClusterID)
		metadata["user_data"] = base64.StdEncoding.EncodeToString([]byte("#!/bin/bash\necho 'Failed to get cluster details'"))
		return metadata
	}

	// Extract cluster endpoint
	var clusterEndpoint string
	if cluster.Endpoints != nil {
		if cluster.Endpoints.PublicEndpoint != nil {
			clusterEndpoint = *cluster.Endpoints.PublicEndpoint
		} else if cluster.Endpoints.PrivateEndpoint != nil {
			clusterEndpoint = *cluster.Endpoints.PrivateEndpoint
		} else if cluster.Endpoints.Kubernetes != nil {
			clusterEndpoint = *cluster.Endpoints.Kubernetes
		}
	}
	
	// If no endpoint found, try to extract from kubeconfig later
	if clusterEndpoint == "" {
		logger.Info("no direct cluster endpoint found, will extract from kubeconfig")
	}
	
	logger.Info("got cluster details",
		"clusterName", lo.FromPtr(cluster.Name),
		"clusterEndpoint", clusterEndpoint,
		"kubernetesVersion", lo.FromPtr(cluster.KubernetesVersion),
		"lifecycleState", cluster.LifecycleState,
		"clusterType", cluster.Type,
		"endpointsNil", cluster.Endpoints == nil,
		"publicEndpointNil", cluster.Endpoints == nil || cluster.Endpoints.PublicEndpoint == nil,
		"privateEndpointNil", cluster.Endpoints == nil || cluster.Endpoints.PrivateEndpoint == nil,
		"kubernetesEndpointNil", cluster.Endpoints == nil || cluster.Endpoints.Kubernetes == nil)

	// Get CA certificate from cluster
	var caCertData string
	if cluster.ClusterPodNetworkOptions != nil && len(cluster.ClusterPodNetworkOptions) > 0 {
		// Extract CA cert from cluster metadata if available
		// Note: The actual CA cert location may vary, this is a placeholder
		logger.Info("cluster pod network options available")
	}
	
	// For self-managed nodes, we need to create a kubeconfig
	kubeconfig, err := c.CreateClusterKubeconfig(ctx)
	if err != nil {
		logger.Error(err, "failed to create kubeconfig, falling back to basic script")
		// Fall back to the default OKE init script approach
		cloudInitScript := `#!/bin/bash
# OKE Node Bootstrap Script for Karpenter
# Fallback to default OKE initialization

set -e

echo "Starting OKE node bootstrap"

# Try to use the default OKE init script from metadata
curl --fail -H "Authorization: Bearer Oracle" -L0 http://169.254.169.254/opc/v2/instance/metadata/oke_init_script | base64 --decode >/var/run/oke-init.sh

# Check if the script was downloaded successfully
if [ -f /var/run/oke-init.sh ]; then
    echo "Running OKE init script"
    bash /var/run/oke-init.sh
else
    echo "Failed to download OKE init script"
    exit 1
fi
`
		metadata["user_data"] = base64.StdEncoding.EncodeToString([]byte(cloudInitScript))
		return metadata
	}
	
	// Extract CA certificate and endpoint from kubeconfig
	kubeconfigLines := strings.Split(kubeconfig, "\n")
	for _, line := range kubeconfigLines {
		if strings.Contains(line, "certificate-authority-data:") {
			parts := strings.Split(line, ": ")
			if len(parts) >= 2 {
				caCertData = strings.TrimSpace(parts[1])
			}
		}
		// Extract endpoint if we don't have one yet
		if clusterEndpoint == "" && strings.Contains(line, "server:") {
			parts := strings.Split(line, ": ")
			if len(parts) >= 2 {
				clusterEndpoint = strings.TrimSpace(parts[1])
				logger.Info("extracted cluster endpoint from kubeconfig", "endpoint", clusterEndpoint)
			}
		}
	}
	
	// Build node labels and taints from NodePool template
	nodeLabelsArgs := c.buildNodeLabelsArgs(nodeClaim, nodePool)
	nodeTaintsArgs := c.buildNodeTaintsArgs(nodePool)
	
	// Create OKE bootstrap script for self-managed nodes
	cloudInitScript := fmt.Sprintf(`#!/bin/bash
# OKE Node Bootstrap Script for Karpenter
# Self-managed node bootstrap

set -e

echo "Starting OKE node bootstrap for cluster %s"

# Extend boot volume if needed
if [ -f /usr/libexec/oci-growfs ]; then
    echo "Extending boot volume"
    bash /usr/libexec/oci-growfs -y
fi

# Check if oke-install.sh exists (for self-managed nodes)
if [ -f /etc/oke/oke-install.sh ]; then
    echo "Running OKE install script for self-managed node"
    # Extract endpoint without port (OKE expects endpoint without :6443)
    ENDPOINT="%s"
    ENDPOINT_NO_PORT=$(echo $ENDPOINT | sed 's/:6443$//')
    
    # Run the OKE install script with cluster endpoint and CA cert
    bash /etc/oke/oke-install.sh \
        --apiserver-endpoint "$ENDPOINT_NO_PORT" \
        --kubelet-ca-cert "%s" \
        --kubelet-extra-args "--cloud-provider=external --node-labels=%s%s"
else
    # Fallback: Try the default OKE metadata approach
    echo "oke-install.sh not found, trying metadata approach"
    curl --fail -H "Authorization: Bearer Oracle" -L0 http://169.254.169.254/opc/v2/instance/metadata/oke_init_script | base64 --decode >/var/run/oke-init.sh
    if [ -f /var/run/oke-init.sh ]; then
        bash /var/run/oke-init.sh
    else
        echo "Failed to bootstrap node"
        exit 1
    fi
fi

echo "OKE node bootstrap completed"
`, c.config.ClusterID, clusterEndpoint, caCertData, nodeLabelsArgs, nodeTaintsArgs)
	
	// In OCI, user_data must be base64-encoded
	encodedScript := base64.StdEncoding.EncodeToString([]byte(cloudInitScript))
	metadata["user_data"] = encodedScript
	
	// Log the cloud-init script for debugging (first 500 chars)
	scriptPreview := cloudInitScript
	if len(scriptPreview) > 500 {
		scriptPreview = scriptPreview[:500] + "..."
	}
	logger.Info("generated cloud-init script",
		"preview", scriptPreview,
		"caCertDataPresent", caCertData != "",
		"clusterEndpoint", clusterEndpoint)
	
	// Add any additional metadata that might be needed for OKE
	// For example, if there's an SSH key specified
	if sshKey, ok := nodeClaim.Annotations["karpenter.sh/ssh-key"]; ok {
		metadata["ssh_authorized_keys"] = sshKey
	}
	
	// Add OKE-specific metadata
	metadata["oke_cluster_id"] = c.config.ClusterID
	
	return metadata
}

func (c *Client) buildFreeformTags(nodeClaim *v1.NodeClaim) map[string]string {
	tags := make(map[string]string)
	
	// Add Karpenter tags - OCI allows "/" in tag values but let's be consistent
	tags["karpenter_discovery"] = nodeClaim.Labels[v1.NodePoolLabelKey]
	tags["karpenter_nodeclaim"] = nodeClaim.Name
	tags["Name"] = fmt.Sprintf("karpenter-%s", nodeClaim.Name)
	
	// Copy relevant labels as tags, sanitizing keys
	for k, v := range nodeClaim.Labels {
		// Replace invalid characters in key
		sanitizedKey := strings.ReplaceAll(k, "/", "_")
		sanitizedKey = strings.ReplaceAll(sanitizedKey, ".", "_")
		if isValidTagKey(sanitizedKey) && len(sanitizedKey) > 0 {
			tags[sanitizedKey] = v
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
	return strings.Contains(shape, "Flex") || strings.Contains(shape, "flex")
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