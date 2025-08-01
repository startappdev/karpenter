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

package v1alpha1

import (
	"github.com/awslabs/operatorpkg/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// OCINodeClass is the Schema for the OCINodeClass API
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=ocinodeclasses,scope=Cluster,shortName=ocinc
// +kubebuilder:subresource:status
type OCINodeClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              OCINodeClassSpec   `json:"spec,omitempty"`
	Status            OCINodeClassStatus `json:"status,omitempty"`
}

// OCINodeClassSpec defines the desired state of OCINodeClass
type OCINodeClassSpec struct {
	// SubnetIDs is a list of subnet IDs to use for launching instances
	SubnetIDs []string `json:"subnetIds,omitempty"`
	
	// ImageID is the OCI image ID to use for instances
	ImageID string `json:"imageId,omitempty"`
	
	// Shape is the default shape to use if not specified in the NodePool
	Shape string `json:"shape,omitempty"`
	
	// Tags to be applied to OCI resources
	Tags map[string]string `json:"tags,omitempty"`
}

// OCINodeClassStatus defines the observed state of OCINodeClass
type OCINodeClassStatus struct {
	// Conditions contains the observed conditions of the OCINodeClass
	Conditions []status.Condition `json:"conditions,omitempty"`
}

// GetObjectKind returns the TypeMeta
func (n *OCINodeClass) GetObjectKind() schema.ObjectKind {
	return &n.TypeMeta
}

// GetConditions returns the conditions
func (n *OCINodeClass) GetConditions() []status.Condition {
	return n.Status.Conditions
}

// SetConditions sets the conditions
func (n *OCINodeClass) SetConditions(conditions []status.Condition) {
	n.Status.Conditions = conditions
}

// StatusConditions returns the status conditions
func (n *OCINodeClass) StatusConditions() status.ConditionSet {
	return status.NewReadyConditions().For(n)
}

// DeepCopyObject implements runtime.Object
func (n *OCINodeClass) DeepCopyObject() runtime.Object {
	if n == nil {
		return nil
	}
	out := new(OCINodeClass)
	n.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver into out
func (n *OCINodeClass) DeepCopyInto(out *OCINodeClass) {
	*out = *n
	out.TypeMeta = n.TypeMeta
	n.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	n.Spec.DeepCopyInto(&out.Spec)
	n.Status.DeepCopyInto(&out.Status)
}

// DeepCopy creates a deep copy
func (n *OCINodeClass) DeepCopy() *OCINodeClass {
	if n == nil {
		return nil
	}
	out := new(OCINodeClass)
	n.DeepCopyInto(out)
	return out
}

// DeepCopyInto for OCINodeClassSpec
func (s *OCINodeClassSpec) DeepCopyInto(out *OCINodeClassSpec) {
	*out = *s
	if s.SubnetIDs != nil {
		out.SubnetIDs = make([]string, len(s.SubnetIDs))
		copy(out.SubnetIDs, s.SubnetIDs)
	}
	if s.Tags != nil {
		out.Tags = make(map[string]string, len(s.Tags))
		for k, v := range s.Tags {
			out.Tags[k] = v
		}
	}
}

// DeepCopyInto for OCINodeClassStatus
func (s *OCINodeClassStatus) DeepCopyInto(out *OCINodeClassStatus) {
	*out = *s
	if s.Conditions != nil {
		out.Conditions = make([]status.Condition, len(s.Conditions))
		copy(out.Conditions, s.Conditions)
	}
}

// OCINodeClassList contains a list of OCINodeClass
// +kubebuilder:object:root=true
type OCINodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OCINodeClass `json:"items"`
}

