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

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"
	
	"sigs.k8s.io/karpenter/pkg/operator"
	corecontrollers "sigs.k8s.io/karpenter/pkg/controllers"
	"sigs.k8s.io/karpenter/pkg/controllers/state"
	"sigs.k8s.io/karpenter/pkg/providers/oci"
)

func main() {
	// Print version information
	fmt.Printf("Karpenter OCI Version: %s\n", version)
	
	// Verify required environment variables are set
	verifyEnvironment()
	
	// Create operator context
	ctx, op := operator.NewOperator()
	
	// Create OCI provider
	ociProvider, err := createOCIProvider(ctx)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create OCI provider")
		os.Exit(1)
	}
	
	// Create cluster state
	cluster := state.NewCluster(op.Clock, op.GetClient())
	
	// Register controllers with OCI provider
	op.WithControllers(ctx,
		corecontrollers.NewControllers(
			ctx,
			op.Manager,
			op.Clock,
			op.GetClient(),
			op.EventRecorder,
			ociProvider,
			cluster,
		)...,
	).WithWebhooks()
	
	// Start the operator
	op.Start(ctx)
}

// version is set via ldflags at build time
var version = "dev"

func init() {
	if v := os.Getenv("VERSION"); v != "" {
		version = v
	}
}

// verifyEnvironment checks that required environment variables are set
func verifyEnvironment() {
	required := []string{
		"OCI_REGION",
		"OCI_COMPARTMENT_ID",
		"OCI_CLUSTER_ID",
		"CLUSTER_NAME",
	}
	
	missing := []string{}
	for _, env := range required {
		if os.Getenv(env) == "" {
			missing = append(missing, env)
		}
	}
	
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "ERROR: Required environment variables are not set: %s\n", strings.Join(missing, ", "))
		os.Exit(1)
	}
}

// createOCIProvider creates and configures the OCI cloud provider
func createOCIProvider(ctx context.Context) (*oci.Provider, error) {
	// Parse subnet IDs
	subnetIDs := []string{}
	if subnetIDsStr := os.Getenv("OCI_SUBNET_IDS"); subnetIDsStr != "" {
		subnetIDs = strings.Split(subnetIDsStr, ",")
		for i := range subnetIDs {
			subnetIDs[i] = strings.TrimSpace(subnetIDs[i])
		}
	}
	
	// Determine auth type
	authType := "instance_principal"
	if os.Getenv("OCI_USE_INSTANCE_PRINCIPAL") != "true" {
		authType = "user_principal"
	}
	
	// Create OCI configuration
	config := &oci.Config{
		AuthType:              authType,
		Region:                os.Getenv("OCI_REGION"),
		CompartmentID:         os.Getenv("OCI_COMPARTMENT_ID"),
		SubnetIDs:             subnetIDs,
		ImageID:               os.Getenv("OCI_IMAGE_ID"),
		DefaultShapes:         []string{"VM.Standard.E4.Flex", "VM.Standard.E5.Flex"},
		EnableDetailedMetrics: os.Getenv("ENABLE_OCI_DYNAMIC_SHAPES") == "true",
		MaxConcurrentLaunches: 10,
	}
	
	log.FromContext(ctx).Info("Creating OCI provider", "config", config)
	return oci.NewProvider(ctx, config)
}