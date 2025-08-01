package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/containerengine"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
)

func main() {
	ctx := context.Background()

	// Get config from environment
	region := os.Getenv("OCI_REGION")
	compartmentID := os.Getenv("OCI_COMPARTMENT_ID")
	clusterID := os.Getenv("OCI_CLUSTER_ID")
	subnetID := "ocid1.subnet.oc1.iad.aaaaaaaaznwweno45m7klssbzt2kyl6qa4ec34335patiolemkti6d4ioita"
	imageID := "ocid1.image.oc1.iad.aaaaaaaaspio4cg2ttycualigcfb74yd5ixp5ovryxfjesm7y7xiayjf53dq"

	fmt.Printf("Testing OCI authentication and permissions...\n")
	fmt.Printf("Region: %s\n", region)
	fmt.Printf("Compartment: %s\n", compartmentID)
	fmt.Printf("Cluster: %s\n", clusterID)

	// Create configuration provider
	var configProvider common.ConfigurationProvider
	var err error

	// Try instance principal authentication
	fmt.Println("\n1. Testing instance principal authentication...")
	configProvider, err = auth.InstancePrincipalConfigurationProvider()
	if err != nil {
		log.Fatalf("Failed to create instance principal provider: %v", err)
	}

	// Test 1: Get tenancy information
	fmt.Println("\n2. Testing identity client...")
	identityClient, err := identity.NewIdentityClientWithConfigurationProvider(configProvider)
	if err != nil {
		log.Fatalf("Failed to create identity client: %v", err)
	}
	identityClient.SetRegion(region)

	// Get availability domains
	adRequest := identity.ListAvailabilityDomainsRequest{
		CompartmentId: &compartmentID,
	}
	adResponse, err := identityClient.ListAvailabilityDomains(ctx, adRequest)
	if err != nil {
		fmt.Printf("ERROR: Failed to list availability domains: %v\n", err)
	} else {
		fmt.Printf("SUCCESS: Found %d availability domains\n", len(adResponse.Items))
		for _, ad := range adResponse.Items {
			fmt.Printf("  - %s\n", *ad.Name)
		}
	}

	// Test 2: Get cluster details
	fmt.Println("\n3. Testing container engine client...")
	ceClient, err := containerengine.NewContainerEngineClientWithConfigurationProvider(configProvider)
	if err != nil {
		log.Fatalf("Failed to create container engine client: %v", err)
	}
	ceClient.SetRegion(region)

	clusterRequest := containerengine.GetClusterRequest{
		ClusterId: &clusterID,
	}
	clusterResponse, err := ceClient.GetCluster(ctx, clusterRequest)
	if err != nil {
		fmt.Printf("ERROR: Failed to get cluster: %v\n", err)
	} else {
		cluster := clusterResponse.Cluster
		fmt.Printf("SUCCESS: Got cluster %s\n", *cluster.Name)
		if cluster.Endpoints != nil {
			if cluster.Endpoints.Kubernetes != nil {
				fmt.Printf("  Kubernetes endpoint: %s\n", *cluster.Endpoints.Kubernetes)
			} else {
				fmt.Printf("  WARNING: Kubernetes endpoint is nil\n")
			}
			if cluster.Endpoints.PrivateEndpoint != nil {
				fmt.Printf("  Private endpoint: %s\n", *cluster.Endpoints.PrivateEndpoint)
			}
			if cluster.Endpoints.PublicEndpoint != nil {
				fmt.Printf("  Public endpoint: %s\n", *cluster.Endpoints.PublicEndpoint)
			}
		} else {
			fmt.Printf("  WARNING: Endpoints object is nil\n")
		}
		fmt.Printf("  Lifecycle state: %s\n", cluster.LifecycleState)
		fmt.Printf("  Type: %s\n", cluster.Type)
		if cluster.EndpointConfig != nil {
			fmt.Printf("  Endpoint config: IsPublicIpEnabled=%v\n", 
				cluster.EndpointConfig.IsPublicIpEnabled != nil && *cluster.EndpointConfig.IsPublicIpEnabled)
			if cluster.EndpointConfig.SubnetId != nil {
				fmt.Printf("  Endpoint subnet: %s\n", *cluster.EndpointConfig.SubnetId)
			}
		}
	}

	// Test 3: Check compute permissions
	fmt.Println("\n4. Testing compute client...")
	computeClient, err := core.NewComputeClientWithConfigurationProvider(configProvider)
	if err != nil {
		log.Fatalf("Failed to create compute client: %v", err)
	}
	computeClient.SetRegion(region)

	// Get subnet details
	subnetRequest := core.GetSubnetRequest{
		SubnetId: &subnetID,
	}
	subnetResponse, err := computeClient.GetSubnet(ctx, subnetRequest)
	if err != nil {
		fmt.Printf("ERROR: Failed to get subnet: %v\n", err)
	} else {
		fmt.Printf("SUCCESS: Got subnet %s\n", *subnetResponse.Subnet.DisplayName)
		fmt.Printf("  CIDR: %s\n", *subnetResponse.Subnet.CidrBlock)
		fmt.Printf("  VCN: %s\n", *subnetResponse.Subnet.VcnId)
	}

	// Get image details
	imageRequest := core.GetImageRequest{
		ImageId: &imageID,
	}
	imageResponse, err := computeClient.GetImage(ctx, imageRequest)
	if err != nil {
		fmt.Printf("ERROR: Failed to get image: %v\n", err)
	} else {
		fmt.Printf("SUCCESS: Got image %s\n", *imageResponse.Image.DisplayName)
		fmt.Printf("  OS: %s %s\n", *imageResponse.Image.OperatingSystem, *imageResponse.Image.OperatingSystemVersion)
		fmt.Printf("  State: %s\n", imageResponse.Image.LifecycleState)
		fmt.Printf("  Compartment: %s\n", *imageResponse.Image.CompartmentId)
	}

	// List shapes in compartment
	fmt.Println("\n5. Testing shape availability...")
	shapeRequest := core.ListShapesRequest{
		CompartmentId: &compartmentID,
		Limit:         common.Int(10),
	}
	if len(adResponse.Items) > 0 {
		shapeRequest.AvailabilityDomain = adResponse.Items[0].Name
	}
	shapeResponse, err := computeClient.ListShapes(ctx, shapeRequest)
	if err != nil {
		fmt.Printf("ERROR: Failed to list shapes: %v\n", err)
	} else {
		fmt.Printf("SUCCESS: Found %d shapes\n", len(shapeResponse.Items))
		for i, shape := range shapeResponse.Items {
			if i < 5 { // Print first 5
				fmt.Printf("  - %s\n", *shape.Shape)
			}
		}
	}

	fmt.Println("\n6. Summary:")
	fmt.Println("If you see errors above, check:")
	fmt.Println("- Instance principal has required IAM policies")
	fmt.Println("- Resources are in the correct compartment/region")
	fmt.Println("- Image and subnet OCIDs are valid")
}