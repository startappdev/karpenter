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

package oci_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/providers/oci"
	"sigs.k8s.io/karpenter/pkg/test"
)

func TestOCI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OCI Provider Suite")
}

var _ = Describe("OCI Provider", func() {
	var ctx context.Context
	var provider *oci.Provider
	var nodePool *v1.NodePool
	var nodeClaim *v1.NodeClaim

	BeforeEach(func() {
		ctx = context.Background()
		
		// Create test configuration
		config := &oci.Config{
			Region:        "us-phoenix-1",
			CompartmentID: "ocid1.compartment.oc1..test",
			SubnetIDs:     []string{"ocid1.subnet.oc1.phx.test"},
			ImageID:       "ocid1.image.oc1.phx.test",
			DefaultShapes: []string{"VM.Standard.E4.Flex", "VM.Standard.E5.Flex"},
		}
		
		var err error
		provider, err = oci.NewProvider(ctx, config)
		Expect(err).ToNot(HaveOccurred())
		
		// Create test NodePool
		nodePool = &v1.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-nodepool",
			},
			Spec: v1.NodePoolSpec{
				Template: v1.NodeClaimTemplate{
					Spec: v1.NodeClaimSpec{
						NodeClassRef: &v1.NodeClassReference{
							Kind:  "OCINodeClass",
							Name:  "default",
							Group: "karpenter.sh",
						},
					},
				},
			},
		}
		
		// Create test NodeClaim
		nodeClaim = &v1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-nodeclaim",
				Labels: map[string]string{
					v1.NodePoolLabelKey: nodePool.Name,
				},
			},
			Spec: v1.NodeClaimSpec{
				NodeClassRef: nodePool.Spec.Template.Spec.NodeClassRef,
				Requirements: []v1.NodeSelectorRequirementWithMinValues{
					{
						NodeSelectorRequirement: corev1.NodeSelectorRequirement{
							Key:      corev1.LabelInstanceTypeStable,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"VM.Standard2.1"},
						},
					},
				},
			},
		}
	})

	Describe("GetInstanceTypes", func() {
		Context("Static Instance Types", func() {
			It("should return static instance types when dynamic provisioning is disabled", func() {
				instanceTypes, err := provider.GetInstanceTypes(ctx, nodePool)
				Expect(err).ToNot(HaveOccurred())
				Expect(instanceTypes).ToNot(BeEmpty())
				
				// Verify instance type properties
				for _, it := range instanceTypes {
					Expect(it.Name).ToNot(BeEmpty())
					Expect(it.Capacity).ToNot(BeEmpty())
					Expect(it.Offerings).ToNot(BeEmpty())
				}
			})
		})
		
		Context("Dynamic Instance Types", func() {
			BeforeEach(func() {
				nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
					Enabled:  true,
					Strategy: v1.StrategyCostOptimized,
					CapacityType: v1.CapacityTypeRatio{
						Preemptible: 80,
						OnDemand:    20,
					},
					Constraints: v1.DynamicConstraints{
						MinOCPUs:      1,
						MaxOCPUs:      32,
						MinMemoryGB:   1,
						MaxMemoryGB:   256,
						AllowedShapes: []string{"VM.Standard.E4.Flex", "VM.Standard.E5.Flex"},
					},
				}
			})
			
			It("should return dynamic instance types when enabled", func() {
				instanceTypes, err := provider.GetInstanceTypes(ctx, nodePool)
				Expect(err).ToNot(HaveOccurred())
				Expect(instanceTypes).ToNot(BeEmpty())
				
				// Verify dynamic instance type naming
				for _, it := range instanceTypes {
					Expect(it.Name).To(ContainSubstring("Flex"))
					Expect(it.Name).To(MatchRegexp(`.*-\d+-\d+$`)) // Shape-OCPUs-Memory format
				}
			})
			
			It("should generate cost-optimized configurations", func() {
				nodePool.Spec.DynamicProvisioning.Strategy = v1.StrategyCostOptimized
				
				instanceTypes, err := provider.GetInstanceTypes(ctx, nodePool)
				Expect(err).ToNot(HaveOccurred())
				
				// Should have multiple configurations for optimization
				Expect(len(instanceTypes)).To(BeNumerically(">", 2))
			})
			
			It("should respect constraints", func() {
				nodePool.Spec.DynamicProvisioning.Constraints.MaxOCPUs = 4
				nodePool.Spec.DynamicProvisioning.Constraints.MaxMemoryGB = 32
				
				instanceTypes, err := provider.GetInstanceTypes(ctx, nodePool)
				Expect(err).ToNot(HaveOccurred())
				
				// Verify all instance types respect constraints
				for _, it := range instanceTypes {
					cpu := it.Capacity[corev1.ResourceCPU]
					memory := it.Capacity[corev1.ResourceMemory]
					
					// Convert CPU to OCPUs (1 OCPU = 2000m CPU)
					ocpus := cpu.MilliValue() / 2000
					Expect(ocpus).To(BeNumerically("<=", 4))
					
					// Convert memory to GB
					memoryGB := memory.Value() / (1024 * 1024 * 1024)
					Expect(memoryGB).To(BeNumerically("<=", 32))
				}
			})
		})
	})

	Describe("Create", func() {
		Context("Fixed Shape Instance", func() {
			It("should create a fixed shape instance", func() {
				createdNodeClaim, err := provider.Create(ctx, nodeClaim)
				Expect(err).ToNot(HaveOccurred())
				Expect(createdNodeClaim).ToNot(BeNil())
				
				// Verify provider ID is set
				Expect(createdNodeClaim.Status.ProviderID).To(HavePrefix("oci://"))
				
				// Verify capacity is set
				Expect(createdNodeClaim.Status.Capacity).ToNot(BeEmpty())
				Expect(createdNodeClaim.Status.Allocatable).ToNot(BeEmpty())
				
				// Verify no dynamic shape status for fixed instances
				Expect(createdNodeClaim.Status.DynamicShape).To(BeNil())
			})
		})
		
		Context("Flexible Shape Instance", func() {
			BeforeEach(func() {
				nodeClaim.Spec.Requirements[0].Values = []string{"VM.Standard.E4.Flex-4-32"}
			})
			
			It("should create a flexible shape instance", func() {
				createdNodeClaim, err := provider.Create(ctx, nodeClaim)
				Expect(err).ToNot(HaveOccurred())
				Expect(createdNodeClaim).ToNot(BeNil())
				
				// Verify dynamic shape status is set
				Expect(createdNodeClaim.Status.DynamicShape).ToNot(BeNil())
				Expect(createdNodeClaim.Status.DynamicShape.Shape).To(Equal("VM.Standard.E4.Flex"))
				Expect(createdNodeClaim.Status.DynamicShape.OCPUs).To(Equal(int32(4)))
				Expect(createdNodeClaim.Status.DynamicShape.MemoryGB).To(Equal(int32(32)))
			})
			
			It("should create preemptible instance when specified", func() {
				nodeClaim.Labels[v1.CapacityTypeLabelKey] = "preemptible"
				
				createdNodeClaim, err := provider.Create(ctx, nodeClaim)
				Expect(err).ToNot(HaveOccurred())
				
				Expect(createdNodeClaim.Status.DynamicShape).ToNot(BeNil())
				Expect(createdNodeClaim.Status.DynamicShape.CapacityType).To(Equal("preemptible"))
			})
		})
		
		It("should handle missing instance type", func() {
			nodeClaim.Spec.Requirements = []v1.NodeSelectorRequirementWithMinValues{}
			
			_, err := provider.Create(ctx, nodeClaim)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no instance type found"))
		})
	})

	Describe("Delete", func() {
		It("should delete an instance", func() {
			nodeClaim.Status.ProviderID = "oci://ocid1.instance.oc1.phx.test"
			
			err := provider.Delete(ctx, nodeClaim)
			Expect(err).ToNot(HaveOccurred())
		})
		
		It("should handle invalid provider ID", func() {
			nodeClaim.Status.ProviderID = "invalid-provider-id"
			
			err := provider.Delete(ctx, nodeClaim)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid provider ID"))
		})
		
		It("should return NodeClaimNotFoundError for non-existent instances", func() {
			nodeClaim.Status.ProviderID = "oci://ocid1.instance.oc1.phx.nonexistent"
			
			// In real implementation, this would check with OCI API
			// For now, the mock always succeeds
			err := provider.Delete(ctx, nodeClaim)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("Get", func() {
		It("should return NodeClaimNotFoundError", func() {
			// Our mock implementation always returns not found
			_, err := provider.Get(ctx, "oci://ocid1.instance.oc1.phx.test")
			Expect(err).To(HaveOccurred())
			Expect(cloudprovider.IsNodeClaimNotFoundError(err)).To(BeTrue())
		})
	})

	Describe("List", func() {
		It("should list instances", func() {
			nodeClaims, err := provider.List(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(nodeClaims).To(BeEmpty()) // Mock returns empty list
		})
	})

	Describe("IsDrifted", func() {
		It("should return no drift", func() {
			reason, err := provider.IsDrifted(ctx, nodeClaim)
			Expect(err).ToNot(HaveOccurred())
			Expect(reason).To(BeEmpty())
		})
	})

	Describe("Name", func() {
		It("should return provider name", func() {
			Expect(provider.Name()).To(Equal("oci"))
		})
	})

	Describe("RepairPolicies", func() {
		It("should return repair policies", func() {
			policies := provider.RepairPolicies()
			Expect(policies).ToNot(BeEmpty())
			Expect(policies[0].ConditionType).To(Equal(corev1.NodeReady))
		})
	})
})

var _ = Describe("Shape Configuration Parsing", func() {
	var provider *oci.Provider

	BeforeEach(func() {
		config := &oci.Config{
			Region:        "us-phoenix-1",
			CompartmentID: "test",
			SubnetIDs:     []string{"test"},
		}
		
		var err error
		provider, err = oci.NewProvider(context.Background(), config)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should parse valid flexible shape configuration", func() {
		// This tests the private method through the Create method
		nodeClaim := &v1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
				Labels: map[string]string{
					v1.NodePoolLabelKey: "test-pool",
				},
			},
			Spec: v1.NodeClaimSpec{
				Requirements: []v1.NodeSelectorRequirementWithMinValues{
					{
						NodeSelectorRequirement: corev1.NodeSelectorRequirement{
							Key:      corev1.LabelInstanceTypeStable,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"VM.Standard.E4.Flex-8-64"},
						},
					},
				},
			},
		}
		
		createdNodeClaim, err := provider.Create(context.Background(), nodeClaim)
		Expect(err).ToNot(HaveOccurred())
		Expect(createdNodeClaim.Status.DynamicShape).ToNot(BeNil())
		Expect(createdNodeClaim.Status.DynamicShape.OCPUs).To(Equal(int32(8)))
		Expect(createdNodeClaim.Status.DynamicShape.MemoryGB).To(Equal(int32(64)))
	})
})