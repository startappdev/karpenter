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

package metrics

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/events"
)

// Event reasons for dynamic provisioning
const (
	// DynamicProvisioningEnabled is emitted when dynamic provisioning is enabled for a NodePool
	DynamicProvisioningEnabled = "DynamicProvisioningEnabled"
	
	// DynamicShapeCalculated is emitted when a dynamic shape is calculated
	DynamicShapeCalculated = "DynamicShapeCalculated"
	
	// DynamicShapeProvisioned is emitted when a dynamic shape is successfully provisioned
	DynamicShapeProvisioned = "DynamicShapeProvisioned"
	
	// DynamicShapeProvisioningFailed is emitted when dynamic shape provisioning fails
	DynamicShapeProvisioningFailed = "DynamicShapeProvisioningFailed"
	
	// BinPackingCompleted is emitted when bin packing is completed
	BinPackingCompleted = "BinPackingCompleted"
	
	// BinPackingFailed is emitted when bin packing fails
	BinPackingFailed = "BinPackingFailed"
	
	// CostOptimizationApplied is emitted when cost optimization is applied
	CostOptimizationApplied = "CostOptimizationApplied"
	
	// CapacityTypeSelected is emitted when a capacity type is selected
	CapacityTypeSelected = "CapacityTypeSelected"
	
	// ConstraintViolation is emitted when a constraint is violated
	ConstraintViolation = "ConstraintViolation"
	
	// InsufficientCapacity is emitted when there's insufficient capacity
	InsufficientCapacity = "InsufficientCapacity"
)

// EventRecorder records events for dynamic provisioning
type EventRecorder struct {
	recorder events.Recorder
}

// NewEventRecorder creates a new event recorder
func NewEventRecorder(recorder events.Recorder) *EventRecorder {
	return &EventRecorder{
		recorder: recorder,
	}
}

// DynamicProvisioningEnabledEvent records that dynamic provisioning was enabled
func (er *EventRecorder) DynamicProvisioningEnabledEvent(nodePool *v1.NodePool) events.Event {
	return events.Event{
		InvolvedObject: nodePool,
		Type:           corev1.EventTypeNormal,
		Reason:         DynamicProvisioningEnabled,
		Message: fmt.Sprintf("Dynamic provisioning enabled with strategy %s, constraints: OCPUs [%d-%d], Memory [%dGB-%dGB]",
			nodePool.Spec.DynamicProvisioning.Strategy,
			nodePool.Spec.DynamicProvisioning.Constraints.MinOCPUs,
			nodePool.Spec.DynamicProvisioning.Constraints.MaxOCPUs,
			nodePool.Spec.DynamicProvisioning.Constraints.MinMemoryGB,
			nodePool.Spec.DynamicProvisioning.Constraints.MaxMemoryGB),
	}
}

// DynamicShapeCalculatedEvent records that a dynamic shape was calculated
func (er *EventRecorder) DynamicShapeCalculatedEvent(object runtime.Object, shape string, ocpus, memoryGB int32, efficiency float64) events.Event {
	return events.Event{
		InvolvedObject: object,
		Type:           corev1.EventTypeNormal,
		Reason:         DynamicShapeCalculated,
		Message: fmt.Sprintf("Calculated dynamic shape: %s with %d OCPUs, %dGB memory (%.1f%% efficiency)",
			shape, ocpus, memoryGB, efficiency),
	}
}

// DynamicShapeProvisionedEvent records successful provisioning
func (er *EventRecorder) DynamicShapeProvisionedEvent(nodeClaim *v1.NodeClaim, shape string, ocpus, memoryGB int32, capacityType string) events.Event {
	return events.Event{
		InvolvedObject: nodeClaim,
		Type:           corev1.EventTypeNormal,
		Reason:         DynamicShapeProvisioned,
		Message: fmt.Sprintf("Successfully provisioned %s shape: %d OCPUs, %dGB memory (%s)",
			shape, ocpus, memoryGB, capacityType),
	}
}

// DynamicShapeProvisioningFailedEvent records provisioning failure
func (er *EventRecorder) DynamicShapeProvisioningFailedEvent(object runtime.Object, shape string, err error) events.Event {
	return events.Event{
		InvolvedObject: object,
		Type:           corev1.EventTypeWarning,
		Reason:         DynamicShapeProvisioningFailed,
		Message:        fmt.Sprintf("Failed to provision shape %s: %v", shape, err),
	}
}

// BinPackingCompletedEvent records bin packing completion
func (er *EventRecorder) BinPackingCompletedEvent(nodePool *v1.NodePool, packedPods, totalPods, nodeCount int) events.Event {
	return events.Event{
		InvolvedObject: nodePool,
		Type:           corev1.EventTypeNormal,
		Reason:         BinPackingCompleted,
		Message: fmt.Sprintf("Bin packing completed: %d/%d pods packed into %d nodes",
			packedPods, totalPods, nodeCount),
	}
}

// BinPackingFailedEvent records bin packing failure
func (er *EventRecorder) BinPackingFailedEvent(nodePool *v1.NodePool, err error) events.Event {
	return events.Event{
		InvolvedObject: nodePool,
		Type:           corev1.EventTypeWarning,
		Reason:         BinPackingFailed,
		Message:        fmt.Sprintf("Bin packing failed: %v", err),
	}
}

// CostOptimizationAppliedEvent records cost optimization
func (er *EventRecorder) CostOptimizationAppliedEvent(nodePool *v1.NodePool, originalCost, optimizedCost float64) events.Event {
	savings := (originalCost - optimizedCost) / originalCost * 100
	return events.Event{
		InvolvedObject: nodePool,
		Type:           corev1.EventTypeNormal,
		Reason:         CostOptimizationApplied,
		Message: fmt.Sprintf("Cost optimization applied: $%.2f -> $%.2f (%.1f%% savings)",
			originalCost, optimizedCost, savings),
	}
}

// CapacityTypeSelectedEvent records capacity type selection
func (er *EventRecorder) CapacityTypeSelectedEvent(nodeClaim *v1.NodeClaim, capacityType string, reason string) events.Event {
	return events.Event{
		InvolvedObject: nodeClaim,
		Type:           corev1.EventTypeNormal,
		Reason:         CapacityTypeSelected,
		Message:        fmt.Sprintf("Selected %s capacity type: %s", capacityType, reason),
	}
}

// ConstraintViolationEvent records a constraint violation
func (er *EventRecorder) ConstraintViolationEvent(nodePool *v1.NodePool, constraint string, requested, limit interface{}) events.Event {
	return events.Event{
		InvolvedObject: nodePool,
		Type:           corev1.EventTypeWarning,
		Reason:         ConstraintViolation,
		Message:        fmt.Sprintf("Constraint violation: %s requested %v exceeds limit %v", constraint, requested, limit),
	}
}

// InsufficientCapacityEvent records insufficient capacity
func (er *EventRecorder) InsufficientCapacityEvent(nodePool *v1.NodePool, shape string, reason string) events.Event {
	return events.Event{
		InvolvedObject: nodePool,
		Type:           corev1.EventTypeWarning,
		Reason:         InsufficientCapacity,
		Message:        fmt.Sprintf("Insufficient capacity for shape %s: %s", shape, reason),
	}
}

// Record sends an event to the recorder
func (er *EventRecorder) Record(event events.Event) {
	er.recorder.Publish(event)
}

// RecordDynamicProvisioningEnabled is a convenience method
func (er *EventRecorder) RecordDynamicProvisioningEnabled(nodePool *v1.NodePool) {
	er.Record(er.DynamicProvisioningEnabledEvent(nodePool))
}

// RecordDynamicShapeCalculated is a convenience method
func (er *EventRecorder) RecordDynamicShapeCalculated(object runtime.Object, shape string, ocpus, memoryGB int32, efficiency float64) {
	er.Record(er.DynamicShapeCalculatedEvent(object, shape, ocpus, memoryGB, efficiency))
}

// RecordDynamicShapeProvisioned is a convenience method
func (er *EventRecorder) RecordDynamicShapeProvisioned(nodeClaim *v1.NodeClaim, shape string, ocpus, memoryGB int32, capacityType string) {
	er.Record(er.DynamicShapeProvisionedEvent(nodeClaim, shape, ocpus, memoryGB, capacityType))
}

// RecordDynamicShapeProvisioningFailed is a convenience method
func (er *EventRecorder) RecordDynamicShapeProvisioningFailed(object runtime.Object, shape string, err error) {
	er.Record(er.DynamicShapeProvisioningFailedEvent(object, shape, err))
}

// RecordBinPackingCompleted is a convenience method
func (er *EventRecorder) RecordBinPackingCompleted(nodePool *v1.NodePool, packedPods, totalPods, nodeCount int) {
	er.Record(er.BinPackingCompletedEvent(nodePool, packedPods, totalPods, nodeCount))
}

// RecordBinPackingFailed is a convenience method
func (er *EventRecorder) RecordBinPackingFailed(nodePool *v1.NodePool, err error) {
	er.Record(er.BinPackingFailedEvent(nodePool, err))
}

// RecordCostOptimizationApplied is a convenience method
func (er *EventRecorder) RecordCostOptimizationApplied(nodePool *v1.NodePool, originalCost, optimizedCost float64) {
	er.Record(er.CostOptimizationAppliedEvent(nodePool, originalCost, optimizedCost))
}

// RecordCapacityTypeSelected is a convenience method
func (er *EventRecorder) RecordCapacityTypeSelected(nodeClaim *v1.NodeClaim, capacityType string, reason string) {
	er.Record(er.CapacityTypeSelectedEvent(nodeClaim, capacityType, reason))
}

// RecordConstraintViolation is a convenience method
func (er *EventRecorder) RecordConstraintViolation(nodePool *v1.NodePool, constraint string, requested, limit interface{}) {
	er.Record(er.ConstraintViolationEvent(nodePool, constraint, requested, limit))
}

// RecordInsufficientCapacity is a convenience method
func (er *EventRecorder) RecordInsufficientCapacity(nodePool *v1.NodePool, shape string, reason string) {
	er.Record(er.InsufficientCapacityEvent(nodePool, shape, reason))
}