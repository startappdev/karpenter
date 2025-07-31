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

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"sigs.k8s.io/karpenter/pkg/controllers"
	"sigs.k8s.io/karpenter/pkg/controllers/state"
	"sigs.k8s.io/karpenter/pkg/operator"
	"sigs.k8s.io/karpenter/pkg/providers/oci"
)

func main() {
	ctx, op := operator.NewOperator()
	
	// Initialize OCI configuration
	config := &oci.Config{
		Region:        lo.Must(os.LookupEnv("OCI_REGION")),
		CompartmentID: lo.Must(os.LookupEnv("OCI_COMPARTMENT_ID")),
		ClusterID:     lo.Must(os.LookupEnv("OCI_CLUSTER_ID")),
		SubnetIDs:     []string{}, // Will be populated from environment
		ImageID:       os.Getenv("OCI_IMAGE_ID"),
		UseInstancePrincipal: os.Getenv("OCI_USE_INSTANCE_PRINCIPAL") == "true",
		EnableDynamicShapes:  os.Getenv("ENABLE_OCI_DYNAMIC_SHAPES") == "true",
	}
	
	// Parse subnet IDs from environment
	if subnetIDs := os.Getenv("OCI_SUBNET_IDS"); subnetIDs != "" {
		config.SubnetIDs = lo.Map(lo.Split(subnetIDs, ","), func(s string, _ int) string {
			return lo.Trim(s, " ")
		})
	}
	
	// Create OCI cloud provider
	cloudProvider, err := oci.NewProvider(ctx, config)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to create OCI cloud provider")
		os.Exit(1)
	}
	
	// Create cluster state
	cluster := state.NewCluster(op.Clock, op.GetClient())
	
	// Register OCI-specific controllers
	op = op.WithControllers(
		controllers.NewControllers(
			op.Clock,
			op.GetClient(),
			op.EventRecorder,
			cluster,
			cloudProvider,
		)...,
	).WithWebhooks()
	
	// Start the operator
	if err := op.Start(ctx); err != nil {
		log.FromContext(ctx).Error(err, "failed to start operator")
		os.Exit(1)
	}
}

// version is set via ldflags at build time
var version = "dev"

func init() {
	if v := os.Getenv("VERSION"); v != "" {
		version = v
	}
	fmt.Printf("Karpenter OCI Version: %s\n", version)
}