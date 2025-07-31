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

package v1

import (
	"context"
	"fmt"

	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
)

// RuntimeValidate will be used to validate any part of the CRD that can not be validated at CRD creation
func (in *NodePool) RuntimeValidate(ctx context.Context) (errs error) {
	errs = multierr.Combine(in.Spec.Template.validateLabels(), in.Spec.Template.Spec.validateTaints(), in.Spec.Template.Spec.validateRequirements(ctx), in.Spec.Template.validateRequirementsNodePoolKeyDoesNotExist(), in.Spec.validateDynamicProvisioning())
	return errs
}

func (in *NodeClaimTemplate) validateLabels() (errs error) {
	for key, value := range in.Labels {
		if key == NodePoolLabelKey {
			errs = multierr.Append(errs, fmt.Errorf("invalid key name %q in labels, restricted", key))
		}
		for _, err := range validation.IsQualifiedName(key) {
			errs = multierr.Append(errs, fmt.Errorf("invalid key name %q in labels, %q", key, err))
		}
		for _, err := range validation.IsValidLabelValue(value) {
			errs = multierr.Append(errs, fmt.Errorf("invalid value: %s for label[%s], %s", value, key, err))
		}
		if err := IsRestrictedLabel(key); err != nil {
			errs = multierr.Append(errs, fmt.Errorf("invalid key name %q in labels, %s", key, err.Error()))
		}
	}
	return errs
}

func (in *NodeClaimTemplate) validateRequirementsNodePoolKeyDoesNotExist() (errs error) {
	for _, requirement := range in.Spec.Requirements {
		if requirement.Key == NodePoolLabelKey {
			errs = multierr.Append(errs, fmt.Errorf("invalid key: %q in requirements, restricted", requirement.Key))
		}
	}
	return errs
}

// validateDynamicProvisioning validates the dynamic provisioning configuration
func (in *NodePoolSpec) validateDynamicProvisioning() (errs error) {
	if in.DynamicProvisioning == nil || !in.DynamicProvisioning.Enabled {
		return nil
	}

	dp := in.DynamicProvisioning

	// Validate capacity type distribution
	if dp.CapacityType.Preemptible+dp.CapacityType.OnDemand != 100 {
		errs = multierr.Append(errs, fmt.Errorf("capacityType percentages must sum to 100, got %d", dp.CapacityType.Preemptible+dp.CapacityType.OnDemand))
	}

	// Validate constraints
	if dp.Constraints.MinOCPUs > dp.Constraints.MaxOCPUs {
		errs = multierr.Append(errs, fmt.Errorf("minOCPUs (%d) cannot be greater than maxOCPUs (%d)", dp.Constraints.MinOCPUs, dp.Constraints.MaxOCPUs))
	}

	if dp.Constraints.MinMemoryGB > dp.Constraints.MaxMemoryGB {
		errs = multierr.Append(errs, fmt.Errorf("minMemoryGB (%d) cannot be greater than maxMemoryGB (%d)", dp.Constraints.MinMemoryGB, dp.Constraints.MaxMemoryGB))
	}

	// Validate allowed shapes
	if len(dp.Constraints.AllowedShapes) == 0 {
		errs = multierr.Append(errs, fmt.Errorf("at least one allowed shape must be specified"))
	}

	for _, shape := range dp.Constraints.AllowedShapes {
		if !isFlexibleShape(shape) {
			errs = multierr.Append(errs, fmt.Errorf("shape %q is not a flexible shape", shape))
		}
	}

	// Validate overhead resources if specified
	if dp.Overhead != nil {
		if err := validateResourceQuantity("systemReservedCPU", dp.Overhead.SystemReservedCPU); err != nil {
			errs = multierr.Append(errs, err)
		}
		if err := validateResourceQuantity("systemReservedMemory", dp.Overhead.SystemReservedMemory); err != nil {
			errs = multierr.Append(errs, err)
		}
		if err := validateResourceQuantity("kubeletReservedCPU", dp.Overhead.KubeletReservedCPU); err != nil {
			errs = multierr.Append(errs, err)
		}
		if err := validateResourceQuantity("kubeletReservedMemory", dp.Overhead.KubeletReservedMemory); err != nil {
			errs = multierr.Append(errs, err)
		}
		if err := validateResourceQuantity("evictionThresholdCPU", dp.Overhead.EvictionThresholdCPU); err != nil {
			errs = multierr.Append(errs, err)
		}
		if err := validateResourceQuantity("evictionThresholdMemory", dp.Overhead.EvictionThresholdMemory); err != nil {
			errs = multierr.Append(errs, err)
		}
	}

	// Validate buffer percentages
	if dp.Buffers != nil {
		if dp.Buffers.CPUHeadroomPercent > 50 {
			errs = multierr.Append(errs, fmt.Errorf("cpuHeadroomPercent cannot exceed 50%%, got %d", dp.Buffers.CPUHeadroomPercent))
		}
		if dp.Buffers.MemoryHeadroomPercent > 50 {
			errs = multierr.Append(errs, fmt.Errorf("memoryHeadroomPercent cannot exceed 50%%, got %d", dp.Buffers.MemoryHeadroomPercent))
		}
	}

	return errs
}

// isFlexibleShape validates if a shape supports flexible configuration
func isFlexibleShape(shape string) bool {
	// OCI flexible shapes contain "Flex" in their name
	return len(shape) > 0 && (shape[len(shape)-4:] == "Flex" || shape[len(shape)-4:] == "flex")
}

// validateResourceQuantity validates a Kubernetes resource quantity string
func validateResourceQuantity(fieldName, value string) error {
	if value == "" {
		return nil
	}
	if _, err := resource.ParseQuantity(value); err != nil {
		return fmt.Errorf("invalid %s: %q, %w", fieldName, value, err)
	}
	return nil
}
