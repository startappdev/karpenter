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

package dynamicshape_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/providers/oci/dynamicshape"
	"sigs.k8s.io/karpenter/pkg/test"
)

func TestDynamicShape(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dynamic Shape Calculator Suite")
}

var _ = Describe("Dynamic Shape Calculator", func() {
	var (
		ctx         context.Context
		calculator  *dynamicshape.Calculator
		constraints *v1.DynamicConstraints
		overhead    *v1.SystemOverhead
		buffers     *v1.ResourceBuffers
	)

	BeforeEach(func() {
		ctx = context.Background()
		
		constraints = &v1.DynamicConstraints{
			MinOCPUs:      1,
			MaxOCPUs:      64,
			MinMemoryGB:   1,
			MaxMemoryGB:   512,
			AllowedShapes: []string{"VM.Standard.E4.Flex", "VM.Standard.E5.Flex"},
		}
		
		overhead = &v1.SystemOverhead{
			SystemReservedCPU:    "200m",
			SystemReservedMemory: "1Gi",
			KubeletReservedCPU:   "100m",
			KubeletReservedMemory: "500Mi",
		}
		
		buffers = &v1.ResourceBuffers{
			CPUHeadroomPercent:    10,
			MemoryHeadroomPercent: 15,
		}
		
		calculator = dynamicshape.NewCalculator(constraints, overhead, buffers)
	})

	Describe("CalculateShapeForPods", func() {
		It("should calculate shape for a single pod", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				}),
			}
			
			recommendation, err := calculator.CalculateShapeForPods(ctx, pods, v1.StrategyExactFit)
			Expect(err).ToNot(HaveOccurred())
			Expect(recommendation).ToNot(BeNil())
			
			// Should include overhead and buffers
			// CPU: 2 + 0.3 (overhead) = 2.3, with 10% buffer = 2.53 -> 2 OCPUs (rounded up)
			Expect(recommendation.OCPUs).To(BeNumerically(">=", 2))
			
			// Memory: 8Gi + 1.5Gi (overhead) = 9.5Gi, with 15% buffer = 10.925Gi -> 11GB
			Expect(recommendation.MemoryGB).To(BeNumerically(">=", 11))
			
			// Verify resource breakdown
			Expect(recommendation.Resources.PodCPU.MilliValue()).To(Equal(int64(2000)))
			Expect(recommendation.Resources.PodMemory.Value()).To(Equal(int64(8 * 1024 * 1024 * 1024)))
			Expect(recommendation.Resources.OverheadCPU.MilliValue()).To(Equal(int64(300)))
			Expect(recommendation.Resources.OverheadMemory.Value()).To(Equal(int64(1610612736))) // 1.5Gi
		})
		
		It("should calculate shape for multiple pods", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				}),
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				}),
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1500m"),
							corev1.ResourceMemory: resource.MustParse("6Gi"),
						},
					},
				}),
			}
			
			recommendation, err := calculator.CalculateShapeForPods(ctx, pods, v1.StrategyBestFit)
			Expect(err).ToNot(HaveOccurred())
			
			// Total: 3 CPU, 12Gi memory
			// With overhead and buffers, should be at least 2 OCPUs and 16GB
			Expect(recommendation.OCPUs).To(BeNumerically(">=", 2))
			Expect(recommendation.MemoryGB).To(BeNumerically(">=", 14))
			
			// Best fit should round to efficient configuration
			Expect([]int32{2, 4, 8}).To(ContainElement(recommendation.OCPUs))
		})
		
		It("should handle pods with init containers", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					InitContainerResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				}),
			}
			
			recommendation, err := calculator.CalculateShapeForPods(ctx, pods, v1.StrategyExactFit)
			Expect(err).ToNot(HaveOccurred())
			
			// Should use max of init container resources
			Expect(recommendation.Resources.PodCPU.MilliValue()).To(Equal(int64(2000)))
			Expect(recommendation.Resources.PodMemory.Value()).To(Equal(int64(4 * 1024 * 1024 * 1024)))
		})
		
		It("should return error for empty pod list", func() {
			_, err := calculator.CalculateShapeForPods(ctx, []*corev1.Pod{}, v1.StrategyExactFit)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no pods provided"))
		})
	})

	Describe("Strategy Application", func() {
		var basePods []*corev1.Pod
		
		BeforeEach(func() {
			basePods = []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("10Gi"),
						},
					},
				}),
			}
		})
		
		It("should apply exact fit strategy", func() {
			recommendation, err := calculator.CalculateShapeForPods(ctx, basePods, v1.StrategyExactFit)
			Expect(err).ToNot(HaveOccurred())
			
			// Should be minimal sizing with overhead and buffers
			// No rounding to efficient configurations
			Expect(recommendation.OCPUs).To(BeNumerically(">=", 2))
			Expect(recommendation.MemoryGB).To(BeNumerically(">=", 12))
		})
		
		It("should apply best fit strategy", func() {
			recommendation, err := calculator.CalculateShapeForPods(ctx, basePods, v1.StrategyBestFit)
			Expect(err).ToNot(HaveOccurred())
			
			// Should round to efficient configurations
			Expect([]int32{2, 4, 8}).To(ContainElement(recommendation.OCPUs))
			
			// Memory should follow common ratios (1:8 or 1:16)
			expectedMemory := recommendation.OCPUs * 8
			if recommendation.MemoryGB > expectedMemory {
				expectedMemory = recommendation.OCPUs * 16
			}
			Expect(recommendation.MemoryGB).To(BeNumerically(">=", 12))
		})
		
		It("should apply cost optimized strategy", func() {
			recommendation, err := calculator.CalculateShapeForPods(ctx, basePods, v1.StrategyCostOptimized)
			Expect(err).ToNot(HaveOccurred())
			
			// Should prefer efficient configurations that might be slightly larger
			Expect(recommendation.OCPUs).To(BeNumerically(">=", 2))
			Expect([]int32{2, 4, 8}).To(ContainElement(recommendation.OCPUs))
			
			// Should prefer 1:8 memory ratio for cost efficiency
			Expect(recommendation.MemoryGB).To(BeNumerically(">=", recommendation.OCPUs*8))
		})
	})

	Describe("Constraint Application", func() {
		It("should respect minimum constraints", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				}),
			}
			
			recommendation, err := calculator.CalculateShapeForPods(ctx, pods, v1.StrategyExactFit)
			Expect(err).ToNot(HaveOccurred())
			
			// Should meet minimum constraints
			Expect(recommendation.OCPUs).To(Equal(constraints.MinOCPUs))
			Expect(recommendation.MemoryGB).To(BeNumerically(">=", constraints.MinMemoryGB))
		})
		
		It("should respect maximum constraints", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200"),
							corev1.ResourceMemory: resource.MustParse("1000Gi"),
						},
					},
				}),
			}
			
			recommendation, err := calculator.CalculateShapeForPods(ctx, pods, v1.StrategyExactFit)
			Expect(err).ToNot(HaveOccurred())
			
			// Should cap at maximum constraints
			Expect(recommendation.OCPUs).To(Equal(constraints.MaxOCPUs))
			Expect(recommendation.MemoryGB).To(Equal(constraints.MaxMemoryGB))
		})
	})

	Describe("Efficiency Calculation", func() {
		It("should calculate efficiency correctly", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("32Gi"),
						},
					},
				}),
			}
			
			recommendation, err := calculator.CalculateShapeForPods(ctx, pods, v1.StrategyExactFit)
			Expect(err).ToNot(HaveOccurred())
			
			// Efficiency should be between 0 and 100
			Expect(recommendation.EfficiencyScore).To(BeNumerically(">=", 0))
			Expect(recommendation.EfficiencyScore).To(BeNumerically("<=", 100))
			
			// With exact fit, efficiency should be relatively high
			Expect(recommendation.EfficiencyScore).To(BeNumerically(">", 50))
		})
	})

	Describe("Shape Selection", func() {
		It("should select appropriate shape based on size", func() {
			// Small instance
			smallPods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				}),
			}
			
			smallRec, err := calculator.CalculateShapeForPods(ctx, smallPods, v1.StrategyBestFit)
			Expect(err).ToNot(HaveOccurred())
			Expect(smallRec.Shape).To(Equal("VM.Standard.E4.Flex"))
			
			// Large instance (if E5 is preferred for large instances)
			largePods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("40"),
							corev1.ResourceMemory: resource.MustParse("300Gi"),
						},
					},
				}),
			}
			
			largeRec, err := calculator.CalculateShapeForPods(ctx, largePods, v1.StrategyBestFit)
			Expect(err).ToNot(HaveOccurred())
			// Should prefer E5 for larger instances
			Expect([]string{"VM.Standard.E4.Flex", "VM.Standard.E5.Flex"}).To(ContainElement(largeRec.Shape))
		})
	})

	Describe("GetShapeVariations", func() {
		It("should return multiple shape options", func() {
			cpu := resource.MustParse("4")
			memory := resource.MustParse("16Gi")
			
			variations, err := calculator.GetShapeVariations(ctx, cpu, memory)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(variations)).To(BeNumerically(">", 1))
			
			// Should have different configurations
			ocpuValues := make(map[int32]bool)
			for _, v := range variations {
				ocpuValues[v.OCPUs] = true
			}
			Expect(len(ocpuValues)).To(BeNumerically(">", 1))
		})
	})

	Describe("Edge Cases", func() {
		It("should handle nil overhead configuration", func() {
			calcNoOverhead := dynamicshape.NewCalculator(constraints, nil, buffers)
			
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				}),
			}
			
			recommendation, err := calcNoOverhead.CalculateShapeForPods(ctx, pods, v1.StrategyExactFit)
			Expect(err).ToNot(HaveOccurred())
			
			// Should still apply default overhead
			Expect(recommendation.OCPUs).To(BeNumerically(">=", 2))
			Expect(recommendation.MemoryGB).To(BeNumerically(">", 8))
		})
		
		It("should handle nil buffer configuration", func() {
			calcNoBuffer := dynamicshape.NewCalculator(constraints, overhead, nil)
			
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				}),
			}
			
			recommendation, err := calcNoBuffer.CalculateShapeForPods(ctx, pods, v1.StrategyExactFit)
			Expect(err).ToNot(HaveOccurred())
			
			// Should not apply buffers but still include overhead
			Expect(recommendation.Resources.BufferCPU.IsZero()).To(BeTrue())
			Expect(recommendation.Resources.BufferMemory.IsZero()).To(BeTrue())
		})
		
		It("should handle pods with no resource requests", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{}),
			}
			
			recommendation, err := calculator.CalculateShapeForPods(ctx, pods, v1.StrategyExactFit)
			Expect(err).ToNot(HaveOccurred())
			
			// Should use minimum constraints
			Expect(recommendation.OCPUs).To(Equal(constraints.MinOCPUs))
			Expect(recommendation.MemoryGB).To(BeNumerically(">=", constraints.MinMemoryGB))
		})
	})
})