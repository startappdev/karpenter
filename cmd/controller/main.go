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
	
	// Import OCI provider package
	"sigs.k8s.io/karpenter/pkg/providers/oci"
)

func main() {
	ctx := context.Background()
	logger := log.FromContext(ctx)
	
	// Print version information
	fmt.Printf("Karpenter OCI Version: %s\n", version)
	
	// Create operator with default settings
	op := operator.NewOperator()
	
	// Initialize cluster state
	cluster := state.NewCluster(op.Clock, op.GetClient())
	
	// Parse OCI configuration from environment
	ociConfig := parseOCIConfig()
	logger.Info("Starting Karpenter with OCI provider", "config", ociConfig)
	
	// Create OCI provider
	subnetIDs := []string{}
	if ociConfig["subnetIds"] != "" {
		subnetIDs = strings.Split(ociConfig["subnetIds"], ",")
		for i := range subnetIDs {
			subnetIDs[i] = strings.TrimSpace(subnetIDs[i])
		}
	}
	
	authType := "instance_principal"
	if ociConfig["useInstancePrincipal"] != "true" {
		authType = "user_principal"
	}
	
	ociProvider, err := oci.NewProvider(ctx, &oci.Config{
		AuthType:              authType,
		Region:                ociConfig["region"],
		CompartmentID:         ociConfig["compartmentId"],
		SubnetIDs:             subnetIDs,
		ImageID:               ociConfig["imageId"],
		DefaultShapes:         []string{"VM.Standard.E4.Flex", "VM.Standard.E5.Flex"},
		EnableDetailedMetrics: ociConfig["enableDynamicShapes"] == "true",
		MaxConcurrentLaunches: 10,
	})
	if err != nil {
		logger.Error(err, "failed to create OCI provider")
		os.Exit(1)
	}
	
	// Register core Karpenter controllers
	op = op.WithControllers(
		corecontrollers.NewControllers(
			op.Clock,
			op.GetClient(),
			op.EventRecorder,
			cluster,
			ociProvider,
		)...,
	).WithWebhooks()
	
	// Start the operator
	if err := op.Start(ctx); err != nil {
		logger.Error(err, "failed to start operator")
		os.Exit(1)
	}
}

// version is set via ldflags at build time
var version = "dev"

func init() {
	if v := os.Getenv("VERSION"); v != "" {
		version = v
	}
}

// parseOCIConfig reads OCI configuration from environment variables
func parseOCIConfig() map[string]string {
	config := make(map[string]string)
	
	// Required configuration
	config["region"] = getEnvOrDie("OCI_REGION")
	config["compartmentId"] = getEnvOrDie("OCI_COMPARTMENT_ID")
	config["clusterId"] = getEnvOrDie("OCI_CLUSTER_ID")
	
	// Optional configuration
	if v := os.Getenv("OCI_SUBNET_IDS"); v != "" {
		config["subnetIds"] = v
	}
	if v := os.Getenv("OCI_IMAGE_ID"); v != "" {
		config["imageId"] = v
	}
	if v := os.Getenv("OCI_USE_INSTANCE_PRINCIPAL"); v != "" {
		config["useInstancePrincipal"] = v
	}
	if v := os.Getenv("ENABLE_OCI_DYNAMIC_SHAPES"); v != "" {
		config["enableDynamicShapes"] = v
	}
	
	return config
}

func getEnvOrDie(key string) string {
	value := os.Getenv(key)
	if value == "" {
		fmt.Fprintf(os.Stderr, "ERROR: Required environment variable %s is not set\n", key)
		os.Exit(1)
	}
	return strings.TrimSpace(value)
}