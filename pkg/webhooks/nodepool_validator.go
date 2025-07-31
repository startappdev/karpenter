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

package webhooks

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// NodePoolValidator validates NodePool resources
type NodePoolValidator struct{}

// ValidateCreate validates a NodePool on creation
func (v *NodePoolValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	nodePool, ok := obj.(*v1.NodePool)
	if !ok {
		return nil, fmt.Errorf("expected NodePool, got %T", obj)
	}
	
	return v.validate(nodePool)
}

// ValidateUpdate validates a NodePool on update
func (v *NodePoolValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	nodePool, ok := newObj.(*v1.NodePool)
	if !ok {
		return nil, fmt.Errorf("expected NodePool, got %T", newObj)
	}
	
	return v.validate(nodePool)
}

// ValidateDelete validates a NodePool on deletion
func (v *NodePoolValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// No validation needed on delete
	return nil, nil
}

// validate performs validation on NodePool fields
func (v *NodePoolValidator) validate(nodePool *v1.NodePool) (admission.Warnings, error) {
	var warnings admission.Warnings
	
	// Validate dynamic provisioning if present
	if nodePool.Spec.DynamicProvisioning != nil {
		if err := v.validateDynamicProvisioning(nodePool.Spec.DynamicProvisioning); err != nil {
			return warnings, err
		}
		
		// Add warning if dynamic provisioning is enabled but feature flag might not be set
		if nodePool.Spec.DynamicProvisioning.Enabled {
			warnings = append(warnings, "Dynamic provisioning is enabled. Ensure ENABLE_OCI_DYNAMIC_SHAPES environment variable is set to true.")
		}
	}
	
	return warnings, nil
}

// validateDynamicProvisioning validates dynamic provisioning configuration
func (v *NodePoolValidator) validateDynamicProvisioning(dp *v1.DynamicProvisioning) error {
	if !dp.Enabled {
		// If not enabled, no further validation needed
		return nil
	}
	
	// Validate capacity type percentages
	if dp.CapacityType.Preemptible+dp.CapacityType.OnDemand != 100 {
		return fmt.Errorf("capacityType percentages must sum to 100, got %d", 
			dp.CapacityType.Preemptible+dp.CapacityType.OnDemand)
	}
	
	// Validate constraints
	if err := v.validateConstraints(&dp.Constraints); err != nil {
		return fmt.Errorf("invalid constraints: %w", err)
	}
	
	// Validate buffers if present
	if dp.Buffers != nil {
		if err := v.validateBuffers(dp.Buffers); err != nil {
			return fmt.Errorf("invalid buffers: %w", err)
		}
	}
	
	// Validate strategy
	if err := v.validateStrategy(dp.Strategy); err != nil {
		return fmt.Errorf("invalid strategy: %w", err)
	}
	
	return nil
}

// validateConstraints validates dynamic constraints
func (v *NodePoolValidator) validateConstraints(constraints *v1.DynamicConstraints) error {
	// Validate OCPU constraints
	if constraints.MinOCPUs < 1 {
		return fmt.Errorf("minOCPUs must be at least 1, got %d", constraints.MinOCPUs)
	}
	if constraints.MaxOCPUs < constraints.MinOCPUs {
		return fmt.Errorf("maxOCPUs (%d) must be greater than or equal to minOCPUs (%d)", 
			constraints.MaxOCPUs, constraints.MinOCPUs)
	}
	if constraints.MaxOCPUs > 128 { // OCI limit
		return fmt.Errorf("maxOCPUs cannot exceed 128, got %d", constraints.MaxOCPUs)
	}
	
	// Validate memory constraints
	if constraints.MinMemoryGB < 1 {
		return fmt.Errorf("minMemoryGB must be at least 1, got %d", constraints.MinMemoryGB)
	}
	if constraints.MaxMemoryGB < constraints.MinMemoryGB {
		return fmt.Errorf("maxMemoryGB (%d) must be greater than or equal to minMemoryGB (%d)", 
			constraints.MaxMemoryGB, constraints.MinMemoryGB)
	}
	if constraints.MaxMemoryGB > 2048 { // OCI limit
		return fmt.Errorf("maxMemoryGB cannot exceed 2048, got %d", constraints.MaxMemoryGB)
	}
	
	// Validate allowed shapes
	if len(constraints.AllowedShapes) == 0 {
		return fmt.Errorf("at least one allowed shape must be specified")
	}
	
	// Validate shape names (basic validation)
	for _, shape := range constraints.AllowedShapes {
		if len(shape) == 0 {
			return fmt.Errorf("empty shape name is not allowed")
		}
		// Check if it's a flexible shape (ends with .Flex)
		if len(shape) < 5 || shape[len(shape)-5:] != ".Flex" {
			return fmt.Errorf("shape %s is not a flexible shape (must end with .Flex)", shape)
		}
	}
	
	return nil
}

// validateBuffers validates resource buffer configuration
func (v *NodePoolValidator) validateBuffers(buffers *v1.ResourceBuffers) error {
	if buffers.CPUHeadroomPercent < 0 || buffers.CPUHeadroomPercent > 100 {
		return fmt.Errorf("cpuHeadroomPercent must be between 0 and 100, got %d", buffers.CPUHeadroomPercent)
	}
	
	if buffers.MemoryHeadroomPercent < 0 || buffers.MemoryHeadroomPercent > 100 {
		return fmt.Errorf("memoryHeadroomPercent must be between 0 and 100, got %d", buffers.MemoryHeadroomPercent)
	}
	
	return nil
}

// validateStrategy validates provisioning strategy
func (v *NodePoolValidator) validateStrategy(strategy v1.ProvisioningStrategy) error {
	switch strategy {
	case v1.StrategyExactFit, v1.StrategyBestFit, v1.StrategyCostOptimized, "":
		return nil
	default:
		return fmt.Errorf("invalid provisioning strategy: %s", strategy)
	}
}