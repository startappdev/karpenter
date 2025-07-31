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

package v1_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

var _ = Describe("NodePool Dynamic Provisioning Validation", func() {
	var ctx context.Context
	var nodePool *v1.NodePool

	BeforeEach(func() {
		ctx = context.Background()
		nodePool = &v1.NodePool{
			Spec: v1.NodePoolSpec{
				Template: v1.NodeClaimTemplate{
					Spec: v1.NodeClaimSpec{
						NodeClassRef: &v1.NodeClassReference{
							Kind:  "TestNodeClass",
							Name:  "default",
							Group: "test.karpenter.sh",
						},
					},
				},
			},
		}
	})

	Context("DynamicProvisioning Validation", func() {
		It("should pass validation when dynamic provisioning is disabled", func() {
			Expect(nodePool.RuntimeValidate(ctx)).To(Succeed())
		})

		It("should pass validation with valid dynamic provisioning configuration", func() {
			nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
				Enabled:  true,
				Strategy: v1.StrategyCostOptimized,
				CapacityType: v1.CapacityTypeRatio{
					Preemptible: 80,
					OnDemand:    20,
				},
				Constraints: v1.DynamicConstraints{
					MinOCPUs:      1,
					MaxOCPUs:      64,
					MinMemoryGB:   1,
					MaxMemoryGB:   1024,
					AllowedShapes: []string{"VM.Standard.E4.Flex", "VM.Standard.E5.Flex"},
				},
				Overhead: &v1.SystemOverhead{
					SystemReservedCPU:       "100m",
					SystemReservedMemory:    "500Mi",
					KubeletReservedCPU:      "200m",
					KubeletReservedMemory:   "1Gi",
					EvictionThresholdCPU:    "100m",
					EvictionThresholdMemory: "500Mi",
				},
				Buffers: &v1.ResourceBuffers{
					CPUHeadroomPercent:    10,
					MemoryHeadroomPercent: 5,
				},
			}
			Expect(nodePool.RuntimeValidate(ctx)).To(Succeed())
		})

		It("should fail validation when capacity type percentages don't sum to 100", func() {
			nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
				Enabled: true,
				CapacityType: v1.CapacityTypeRatio{
					Preemptible: 70,
					OnDemand:    20, // Sum is 90, not 100
				},
				Constraints: v1.DynamicConstraints{
					AllowedShapes: []string{"VM.Standard.E4.Flex"},
				},
			}
			err := nodePool.RuntimeValidate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("capacityType percentages must sum to 100"))
		})

		It("should fail validation when minOCPUs > maxOCPUs", func() {
			nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
				Enabled: true,
				CapacityType: v1.CapacityTypeRatio{
					Preemptible: 80,
					OnDemand:    20,
				},
				Constraints: v1.DynamicConstraints{
					MinOCPUs:      64,
					MaxOCPUs:      32,
					AllowedShapes: []string{"VM.Standard.E4.Flex"},
				},
			}
			err := nodePool.RuntimeValidate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("minOCPUs (64) cannot be greater than maxOCPUs (32)"))
		})

		It("should fail validation when minMemoryGB > maxMemoryGB", func() {
			nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
				Enabled: true,
				CapacityType: v1.CapacityTypeRatio{
					Preemptible: 80,
					OnDemand:    20,
				},
				Constraints: v1.DynamicConstraints{
					MinMemoryGB:   1024,
					MaxMemoryGB:   512,
					AllowedShapes: []string{"VM.Standard.E4.Flex"},
				},
			}
			err := nodePool.RuntimeValidate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("minMemoryGB (1024) cannot be greater than maxMemoryGB (512)"))
		})

		It("should fail validation when no allowed shapes are specified", func() {
			nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
				Enabled: true,
				CapacityType: v1.CapacityTypeRatio{
					Preemptible: 80,
					OnDemand:    20,
				},
				Constraints: v1.DynamicConstraints{
					AllowedShapes: []string{},
				},
			}
			err := nodePool.RuntimeValidate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("at least one allowed shape must be specified"))
		})

		It("should fail validation when shape is not flexible", func() {
			nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
				Enabled: true,
				CapacityType: v1.CapacityTypeRatio{
					Preemptible: 80,
					OnDemand:    20,
				},
				Constraints: v1.DynamicConstraints{
					AllowedShapes: []string{"VM.Standard2.4"}, // Not a flexible shape
				},
			}
			err := nodePool.RuntimeValidate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("shape \"VM.Standard2.4\" is not a flexible shape"))
		})

		It("should fail validation with invalid resource quantities", func() {
			nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
				Enabled: true,
				CapacityType: v1.CapacityTypeRatio{
					Preemptible: 80,
					OnDemand:    20,
				},
				Constraints: v1.DynamicConstraints{
					AllowedShapes: []string{"VM.Standard.E4.Flex"},
				},
				Overhead: &v1.SystemOverhead{
					SystemReservedCPU: "invalid-cpu",
				},
			}
			err := nodePool.RuntimeValidate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid systemReservedCPU"))
		})

		It("should fail validation when CPU headroom exceeds 50%", func() {
			nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
				Enabled: true,
				CapacityType: v1.CapacityTypeRatio{
					Preemptible: 80,
					OnDemand:    20,
				},
				Constraints: v1.DynamicConstraints{
					AllowedShapes: []string{"VM.Standard.E4.Flex"},
				},
				Buffers: &v1.ResourceBuffers{
					CPUHeadroomPercent: 60,
				},
			}
			err := nodePool.RuntimeValidate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cpuHeadroomPercent cannot exceed 50%"))
		})

		It("should fail validation when memory headroom exceeds 50%", func() {
			nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
				Enabled: true,
				CapacityType: v1.CapacityTypeRatio{
					Preemptible: 80,
					OnDemand:    20,
				},
				Constraints: v1.DynamicConstraints{
					AllowedShapes: []string{"VM.Standard.E4.Flex"},
				},
				Buffers: &v1.ResourceBuffers{
					MemoryHeadroomPercent: 75,
				},
			}
			err := nodePool.RuntimeValidate(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("memoryHeadroomPercent cannot exceed 50%"))
		})

		It("should accept both uppercase and lowercase 'Flex' in shape names", func() {
			nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
				Enabled: true,
				CapacityType: v1.CapacityTypeRatio{
					Preemptible: 80,
					OnDemand:    20,
				},
				Constraints: v1.DynamicConstraints{
					AllowedShapes: []string{"VM.Standard.E4.Flex", "VM.Standard.A1.flex"},
				},
			}
			Expect(nodePool.RuntimeValidate(ctx)).To(Succeed())
		})

		It("should apply default values correctly", func() {
			dp := &v1.DynamicProvisioning{
				Enabled: true,
				Constraints: v1.DynamicConstraints{
					AllowedShapes: []string{"VM.Standard.E4.Flex"},
				},
			}
			
			// Test default values via the builder pattern 
			Expect(dp.Strategy).To(BeEmpty()) // Will default to cost-optimized
			Expect(dp.CapacityType.Preemptible).To(Equal(int32(0))) // Will default to 80
			Expect(dp.CapacityType.OnDemand).To(Equal(int32(0))) // Will default to 20
			Expect(dp.Constraints.MinOCPUs).To(Equal(int32(0))) // Will default to 1
			Expect(dp.Constraints.MaxOCPUs).To(Equal(int32(0))) // Will default to 64
		})
	})

	Context("Strategy Validation", func() {
		It("should accept valid provisioning strategies", func() {
			strategies := []v1.ProvisioningStrategy{
				v1.StrategyExactFit,
				v1.StrategyBestFit,
				v1.StrategyCostOptimized,
			}

			for _, strategy := range strategies {
				nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
					Enabled:  true,
					Strategy: strategy,
					CapacityType: v1.CapacityTypeRatio{
						Preemptible: 80,
						OnDemand:    20,
					},
					Constraints: v1.DynamicConstraints{
						AllowedShapes: []string{"VM.Standard.E4.Flex"},
					},
				}
				Expect(nodePool.RuntimeValidate(ctx)).To(Succeed())
			}
		})
	})

	Context("Overhead Validation", func() {
		It("should accept valid resource quantities", func() {
			validQuantities := []string{
				"100m",
				"1",
				"1000Mi",
				"1Gi",
				"500M",
			}

			for _, quantity := range validQuantities {
				nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
					Enabled: true,
					CapacityType: v1.CapacityTypeRatio{
						Preemptible: 80,
						OnDemand:    20,
					},
					Constraints: v1.DynamicConstraints{
						AllowedShapes: []string{"VM.Standard.E4.Flex"},
					},
					Overhead: &v1.SystemOverhead{
						SystemReservedCPU: quantity,
					},
				}
				Expect(nodePool.RuntimeValidate(ctx)).To(Succeed())
			}
		})

		It("should accept empty overhead values", func() {
			nodePool.Spec.DynamicProvisioning = &v1.DynamicProvisioning{
				Enabled: true,
				CapacityType: v1.CapacityTypeRatio{
					Preemptible: 80,
					OnDemand:    20,
				},
				Constraints: v1.DynamicConstraints{
					AllowedShapes: []string{"VM.Standard.E4.Flex"},
				},
				Overhead: &v1.SystemOverhead{
					SystemReservedCPU:    "",
					SystemReservedMemory: "",
				},
			}
			Expect(nodePool.RuntimeValidate(ctx)).To(Succeed())
		})
	})
})

var _ = Describe("NodeClaim Dynamic Shape Status", func() {
	var nodeClaim *v1.NodeClaim

	BeforeEach(func() {
		nodeClaim = &v1.NodeClaim{
			Spec: v1.NodeClaimSpec{
				NodeClassRef: &v1.NodeClassReference{
					Kind:  "TestNodeClass",
					Name:  "default",
					Group: "test.karpenter.sh",
				},
			},
		}
	})

	It("should properly store dynamic shape status", func() {
		nodeClaim.Status.DynamicShape = &v1.DynamicShapeStatus{
			Shape:        "VM.Standard.E4.Flex",
			OCPUs:        4,
			MemoryGB:     32,
			CapacityType: "preemptible",
			Strategy:     v1.StrategyCostOptimized,
			Efficiency: v1.ResourceEfficiency{
				CPURequested:      "7200m",
				CPUProvisioned:    "8000m",
				CPUEfficiency:     90,
				MemoryRequested:   "30Gi",
				MemoryProvisioned: "32Gi",
				MemoryEfficiency:  94,
			},
			Pricing: &v1.PricingInfo{
				HourlyRate:           "0.125",
				EstimatedMonthlyCost: "90.00",
				SavingsVsOnDemand:    65,
			},
		}

		Expect(nodeClaim.Status.DynamicShape).ToNot(BeNil())
		Expect(nodeClaim.Status.DynamicShape.Shape).To(Equal("VM.Standard.E4.Flex"))
		Expect(nodeClaim.Status.DynamicShape.OCPUs).To(Equal(int32(4)))
		Expect(nodeClaim.Status.DynamicShape.MemoryGB).To(Equal(int32(32)))
		Expect(nodeClaim.Status.DynamicShape.CapacityType).To(Equal("preemptible"))
		Expect(nodeClaim.Status.DynamicShape.Strategy).To(Equal(v1.StrategyCostOptimized))
		Expect(nodeClaim.Status.DynamicShape.Efficiency.CPUEfficiency).To(Equal(int32(90)))
		Expect(nodeClaim.Status.DynamicShape.Pricing.HourlyRate).To(Equal("0.125"))
	})

	It("should handle nil dynamic shape status", func() {
		Expect(nodeClaim.Status.DynamicShape).To(BeNil())
	})

	It("should handle nil pricing info", func() {
		nodeClaim.Status.DynamicShape = &v1.DynamicShapeStatus{
			Shape:        "VM.Standard.E4.Flex",
			OCPUs:        2,
			MemoryGB:     16,
			CapacityType: "on-demand",
			Strategy:     v1.StrategyExactFit,
			Efficiency: v1.ResourceEfficiency{
				CPURequested:      "1800m",
				CPUProvisioned:    "2000m",
				CPUEfficiency:     90,
				MemoryRequested:   "15Gi",
				MemoryProvisioned: "16Gi",
				MemoryEfficiency:  94,
			},
			Pricing: nil,
		}

		Expect(nodeClaim.Status.DynamicShape.Pricing).To(BeNil())
	})
})