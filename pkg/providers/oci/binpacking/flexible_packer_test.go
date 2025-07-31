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

package binpacking_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/providers/oci/binpacking"
	"sigs.k8s.io/karpenter/pkg/test"
)

func TestBinPacking(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Flexible Bin Packing Suite")
}

// Mock pricing provider for tests
type mockPricingProvider struct {
	pricePerOCPU   float64
	pricePerGB     float64
	preemptDiscount float64
}

func (m *mockPricingProvider) GetShapePrice(ctx context.Context, shape string, ocpus, memoryGB int32, isPreemptible bool) (float64, error) {
	basePrice := float64(ocpus)*m.pricePerOCPU + float64(memoryGB)*m.pricePerGB
	if isPreemptible {
		basePrice *= (1 - m.preemptDiscount)
	}
	return basePrice, nil
}

var _ = Describe("Flexible Bin Packer", func() {
	var (
		ctx             context.Context
		packer          *binpacking.FlexiblePacker
		pricingProvider *mockPricingProvider
		constraints     *v1.DynamicConstraints
		overhead        *v1.SystemOverhead
		buffers         *v1.ResourceBuffers
	)

	BeforeEach(func() {
		ctx = context.Background()
		
		constraints = &v1.DynamicConstraints{
			MinOCPUs:      1,
			MaxOCPUs:      32,
			MinMemoryGB:   1,
			MaxMemoryGB:   256,
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
		
		pricingProvider = &mockPricingProvider{
			pricePerOCPU:    0.05,
			pricePerGB:      0.01,
			preemptDiscount: 0.8,
		}
		
		packer = binpacking.NewFlexiblePacker(constraints, overhead, buffers, pricingProvider)
	})

	Describe("Exact Fit Strategy", func() {
		It("should create one node per pod", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				}),
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
					},
				}),
			}
			
			capacityType := v1.CapacityTypeRatio{
				Preemptible: 50,
				OnDemand:    50,
			}
			
			result, err := packer.Pack(ctx, pods, v1.StrategyExactFit, capacityType)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Nodes).To(HaveLen(2))
			Expect(result.UnschedulablePods).To(BeEmpty())
			
			// Verify each node has exactly one pod
			for _, node := range result.Nodes {
				Expect(node.Pods).To(HaveLen(1))
			}
			
			// Verify resource calculations include overhead and buffers
			// First pod: 2 CPU + 300m overhead = 2.3 CPU, with 10% buffer = 2.53 CPU = 2 OCPUs
			// 8Gi + 1.5Gi overhead = 9.5Gi, with 15% buffer = 10.925Gi = 11GB
			Expect(result.Nodes[0].OCPUs).To(BeNumerically(">=", 2))
			Expect(result.Nodes[0].MemoryGB).To(BeNumerically(">=", 11))
		})
		
		It("should mark pods as unschedulable if they exceed constraints", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100"),
							corev1.ResourceMemory: resource.MustParse("500Gi"),
						},
					},
				}),
			}
			
			capacityType := v1.CapacityTypeRatio{
				Preemptible: 0,
				OnDemand:    100,
			}
			
			result, err := packer.Pack(ctx, pods, v1.StrategyExactFit, capacityType)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Nodes).To(BeEmpty())
			Expect(result.UnschedulablePods).To(HaveLen(1))
		})
		
		It("should respect capacity type ratios", func() {
			pods := make([]*corev1.Pod, 10)
			for i := range pods {
				pods[i] = test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				})
			}
			
			capacityType := v1.CapacityTypeRatio{
				Preemptible: 80,
				OnDemand:    20,
			}
			
			result, err := packer.Pack(ctx, pods, v1.StrategyExactFit, capacityType)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Nodes).To(HaveLen(10))
			
			// Count preemptible nodes
			preemptibleCount := 0
			for _, node := range result.Nodes {
				if node.IsPreemptible {
					preemptibleCount++
				}
			}
			
			// Should be approximately 80% preemptible
			Expect(preemptibleCount).To(BeNumerically(">=", 7))
			Expect(preemptibleCount).To(BeNumerically("<=", 9))
		})
	})

	Describe("Best Fit Strategy", func() {
		It("should pack multiple pods into efficient node sizes", func() {
			pods := []*corev1.Pod{
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
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				}),
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
					},
				}),
			}
			
			capacityType := v1.CapacityTypeRatio{
				Preemptible: 100,
				OnDemand:    0,
			}
			
			result, err := packer.Pack(ctx, pods, v1.StrategyBestFit, capacityType)
			Expect(err).ToNot(HaveOccurred())
			
			// Should pack efficiently - likely 1 or 2 nodes
			Expect(len(result.Nodes)).To(BeNumerically("<=", 2))
			Expect(result.UnschedulablePods).To(BeEmpty())
			
			// All nodes should be preemptible
			for _, node := range result.Nodes {
				Expect(node.IsPreemptible).To(BeTrue())
			}
		})
		
		It("should use efficient node configurations", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3"),
							corev1.ResourceMemory: resource.MustParse("12Gi"),
						},
					},
				}),
			}
			
			capacityType := v1.CapacityTypeRatio{
				Preemptible: 0,
				OnDemand:    100,
			}
			
			result, err := packer.Pack(ctx, pods, v1.StrategyBestFit, capacityType)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Nodes).To(HaveLen(1))
			
			// Should round up to efficient configuration (likely 4 OCPUs, 32GB)
			node := result.Nodes[0]
			Expect(node.OCPUs).To(BeNumerically(">=", 4))
			Expect(node.MemoryGB).To(BeNumerically(">=", 16))
		})
	})

	Describe("Cost Optimized Strategy", func() {
		It("should minimize total cost", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				}),
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				}),
			}
			
			capacityType := v1.CapacityTypeRatio{
				Preemptible: 100,
				OnDemand:    0,
			}
			
			result, err := packer.Pack(ctx, pods, v1.StrategyCostOptimized, capacityType)
			Expect(err).ToNot(HaveOccurred())
			
			// Should optimize for cost - might combine pods
			Expect(result.UnschedulablePods).To(BeEmpty())
			
			// Verify cost calculation
			Expect(result.EstimatedHourlyCost).To(BeNumerically(">", 0))
		})
		
		It("should consider combining small nodes", func() {
			// Create many small pods that could be combined
			pods := make([]*corev1.Pod, 8)
			for i := range pods {
				pods[i] = test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("250m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				})
			}
			
			capacityType := v1.CapacityTypeRatio{
				Preemptible: 50,
				OnDemand:    50,
			}
			
			result, err := packer.Pack(ctx, pods, v1.StrategyCostOptimized, capacityType)
			Expect(err).ToNot(HaveOccurred())
			
			// Should combine into fewer nodes than exact fit would create
			Expect(len(result.Nodes)).To(BeNumerically("<", 8))
			Expect(result.UnschedulablePods).To(BeEmpty())
		})
	})

	Describe("Edge Cases", func() {
		It("should handle empty pod list", func() {
			pods := []*corev1.Pod{}
			
			capacityType := v1.CapacityTypeRatio{
				Preemptible: 50,
				OnDemand:    50,
			}
			
			result, err := packer.Pack(ctx, pods, v1.StrategyBestFit, capacityType)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Nodes).To(BeEmpty())
			Expect(result.UnschedulablePods).To(BeEmpty())
		})
		
		It("should handle pods with no resource requests", func() {
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{}),
			}
			
			capacityType := v1.CapacityTypeRatio{
				Preemptible: 0,
				OnDemand:    100,
			}
			
			result, err := packer.Pack(ctx, pods, v1.StrategyExactFit, capacityType)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Nodes).To(HaveLen(1))
			
			// Should use minimum sizes with overhead
			node := result.Nodes[0]
			Expect(node.OCPUs).To(BeNumerically(">=", constraints.MinOCPUs))
			Expect(node.MemoryGB).To(BeNumerically(">=", constraints.MinMemoryGB))
		})
		
		It("should handle nil overhead and buffers", func() {
			packerNoOverhead := binpacking.NewFlexiblePacker(constraints, nil, nil, pricingProvider)
			
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
			
			capacityType := v1.CapacityTypeRatio{
				Preemptible: 100,
				OnDemand:    0,
			}
			
			result, err := packerNoOverhead.Pack(ctx, pods, v1.StrategyExactFit, capacityType)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Nodes).To(HaveLen(1))
			
			// Should still apply default overhead
			node := result.Nodes[0]
			Expect(node.OCPUs).To(BeNumerically(">=", 2))
			Expect(node.MemoryGB).To(BeNumerically(">=", 10)) // 8Gi + default overhead
		})
	})

	Describe("Resource Calculations", func() {
		It("should calculate efficiency metrics", func() {
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
			
			capacityType := v1.CapacityTypeRatio{
				Preemptible: 0,
				OnDemand:    100,
			}
			
			result, err := packer.Pack(ctx, pods, v1.StrategyExactFit, capacityType)
			Expect(err).ToNot(HaveOccurred())
			
			// Verify metrics are calculated
			Expect(result.TotalCPURequested.MilliValue()).To(Equal(int64(4000)))
			Expect(result.TotalMemoryRequested.Value()).To(Equal(int64(32 * 1024 * 1024 * 1024)))
			
			// Provisioned should be larger due to overhead and buffers
			Expect(result.TotalCPUProvisioned.MilliValue()).To(BeNumerically(">", result.TotalCPURequested.MilliValue()))
			Expect(result.TotalMemoryProvisioned.Value()).To(BeNumerically(">", result.TotalMemoryRequested.Value()))
		})
	})
})