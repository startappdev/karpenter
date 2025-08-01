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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetObjectKind returns the TypeMeta
func (n *OCINodeClass) GetObjectKind() schema.ObjectKind {
	return &n.TypeMeta
}

// GetConditions returns the conditions
func (n *OCINodeClass) GetConditions() []metav1.Condition {
	return n.Status.Conditions
}

// SetConditions sets the conditions
func (n *OCINodeClass) SetConditions(conditions []metav1.Condition) {
	n.Status.Conditions = conditions
}

// OCINodeClassList contains a list of OCINodeClass
// +kubebuilder:object:root=true
type OCINodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OCINodeClass `json:"items"`
}

func init() {
	// Register the types with the scheme
	SchemeGroupVersion = schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
}

var (
	// SchemeGroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
)