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
	"github.com/awslabs/operatorpkg/status"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTypeLaunched             = "Launched"
	ConditionTypeRegistered           = "Registered"
	ConditionTypeInitialized          = "Initialized"
	ConditionTypeConsolidatable       = "Consolidatable"
	ConditionTypeDrifted              = "Drifted"
	ConditionTypeDrained              = "Drained"
	ConditionTypeVolumesDetached      = "VolumesDetached"
	ConditionTypeInstanceTerminating  = "InstanceTerminating"
	ConditionTypeConsistentStateFound = "ConsistentStateFound"
	ConditionTypeDisruptionReason     = "DisruptionReason"
)

// NodeClaimStatus defines the observed state of NodeClaim
type NodeClaimStatus struct {
	// NodeName is the name of the corresponding node object
	// +optional
	NodeName string `json:"nodeName,omitempty"`
	// ProviderID of the corresponding node object
	// +optional
	ProviderID string `json:"providerID,omitempty"`
	// ImageID is an identifier for the image that runs on the node
	// +optional
	ImageID string `json:"imageID,omitempty"`
	// Capacity is the estimated full capacity of the node
	// +optional
	Capacity v1.ResourceList `json:"capacity,omitempty"`
	// Allocatable is the estimated allocatable capacity of the node
	// +optional
	Allocatable v1.ResourceList `json:"allocatable,omitempty"`
	// Conditions contains signals for health and readiness
	// +optional
	Conditions []status.Condition `json:"conditions,omitempty"`
	// LastPodEventTime is updated with the last time a pod was scheduled
	// or removed from the node. A pod going terminal or terminating
	// is also considered as removed.
	// +optional
	LastPodEventTime metav1.Time `json:"lastPodEventTime,omitempty"`
	// DynamicShape contains information about the actual shape configuration
	// when using dynamic provisioning
	// +optional
	DynamicShape *DynamicShapeStatus `json:"dynamicShape,omitempty"`
}

func (in *NodeClaim) StatusConditions() status.ConditionSet {
	return status.NewReadyConditions(
		ConditionTypeLaunched,
		ConditionTypeRegistered,
		ConditionTypeInitialized,
	).For(in)
}

func (in *NodeClaim) GetConditions() []status.Condition {
	return in.Status.Conditions
}

func (in *NodeClaim) SetConditions(conditions []status.Condition) {
	in.Status.Conditions = conditions
}

// DynamicShapeStatus contains information about the actual provisioned shape
type DynamicShapeStatus struct {
	// Shape is the actual shape family used
	Shape string `json:"shape"`
	// OCPUs is the number of OCPUs provisioned
	OCPUs int32 `json:"ocpus"`
	// MemoryGB is the amount of memory in GB provisioned
	MemoryGB int32 `json:"memoryGB"`
	// CapacityType is the capacity type selected (preemptible or on-demand)
	CapacityType string `json:"capacityType"`
	// Strategy is the provisioning strategy used
	Strategy ProvisioningStrategy `json:"strategy"`
	// Efficiency contains resource efficiency metrics
	Efficiency ResourceEfficiency `json:"efficiency"`
	// Pricing contains cost information
	Pricing *PricingInfo `json:"pricing,omitempty"`
}

// ResourceEfficiency tracks how efficiently resources are utilized
type ResourceEfficiency struct {
	// CPURequested is the amount of CPU requested by pods
	CPURequested string `json:"cpuRequested"`
	// CPUProvisioned is the amount of CPU actually provisioned
	CPUProvisioned string `json:"cpuProvisioned"`
	// CPUEfficiency is the percentage of CPU utilization (0-100)
	CPUEfficiency int32 `json:"cpuEfficiency"`
	// MemoryRequested is the amount of memory requested by pods
	MemoryRequested string `json:"memoryRequested"`
	// MemoryProvisioned is the amount of memory actually provisioned
	MemoryProvisioned string `json:"memoryProvisioned"`
	// MemoryEfficiency is the percentage of memory utilization (0-100)
	MemoryEfficiency int32 `json:"memoryEfficiency"`
}

// PricingInfo contains cost information for the provisioned shape
type PricingInfo struct {
	// HourlyRate is the cost per hour in USD (stored as string to avoid float precision issues)
	HourlyRate string `json:"hourlyRate"`
	// EstimatedMonthlyCost is the estimated monthly cost in USD (stored as string to avoid float precision issues)
	EstimatedMonthlyCost string `json:"estimatedMonthlyCost"`
	// SavingsVsOnDemand is the percentage saved compared to on-demand pricing
	SavingsVsOnDemand int32 `json:"savingsVsOnDemand,omitempty"`
}
