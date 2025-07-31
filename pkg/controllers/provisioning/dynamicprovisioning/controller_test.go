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

package dynamicprovisioning_test

import (
	"context"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
	"sigs.k8s.io/karpenter/pkg/controllers/provisioning/dynamicprovisioning"
	"sigs.k8s.io/karpenter/pkg/controllers/provisioning/scheduling"
	"sigs.k8s.io/karpenter/pkg/providers/oci/binpacking"
	"sigs.k8s.io/karpenter/pkg/providers/oci/dynamicshape"
	"sigs.k8s.io/karpenter/pkg/test"
	"sigs.k8s.io/karpenter/pkg/test/fake"
)

func TestDynamicProvisioning(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dynamic Provisioning Controller Suite")
}

// Mock pricing provider
type mockPricingProvider struct{}

func (m *mockPricingProvider) GetShapePrice(ctx context.Context, shape string, ocpus, memoryGB int32, isPreemptible bool) (float64, error) {
	basePrice := float64(ocpus)*0.05 + float64(memoryGB)*0.01
	if isPreemptible {
		basePrice *= 0.2
	}
	return basePrice, nil
}

var _ = Describe("Dynamic Provisioning Controller", func() {
	var (
		ctx        context.Context
		controller *dynamicprovisioning.Controller
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Feature Flag", func() {
		It("should be disabled by default", func() {
			controller = dynamicprovisioning.NewController()
			Expect(controller.IsEnabled()).To(BeFalse())
		})
		
		It("should be enabled when environment variable is set", func() {
			os.Setenv(dynamicprovisioning.EnvVarFeatureFlag, "true")
			defer os.Unsetenv(dynamicprovisioning.EnvVarFeatureFlag)
			
			controller = dynamicprovisioning.NewController()
			Expect(controller.IsEnabled()).To(BeTrue())
		})
		
		It("should handle invalid environment variable values", func() {
			os.Setenv(dynamicprovisioning.EnvVarFeatureFlag, "invalid")
			defer os.Unsetenv(dynamicprovisioning.EnvVarFeatureFlag)
			
			controller = dynamicprovisioning.NewController()
			Expect(controller.IsEnabled()).To(BeFalse())
		})
	})

	Describe("ShouldUseDynamicProvisioning", func() {
		BeforeEach(func() {
			os.Setenv(dynamicprovisioning.EnvVarFeatureFlag, "true")
			controller = dynamicprovisioning.NewController()
		})
		
		AfterEach(func() {
			os.Unsetenv(dynamicprovisioning.EnvVarFeatureFlag)
		})
		
		It("should return false when feature flag is disabled", func() {
			os.Setenv(dynamicprovisioning.EnvVarFeatureFlag, "false")
			controller = dynamicprovisioning.NewController()
			
			nodePool := &v1.NodePool{
				Spec: v1.NodePoolSpec{
					DynamicProvisioning: &v1.DynamicProvisioning{
						Enabled: true,
					},
				},
			}
			
			Expect(controller.ShouldUseDynamicProvisioning(nodePool)).To(BeFalse())
		})
		
		It("should return false when NodePool has no dynamic provisioning config", func() {
			nodePool := &v1.NodePool{
				Spec: v1.NodePoolSpec{},
			}
			
			Expect(controller.ShouldUseDynamicProvisioning(nodePool)).To(BeFalse())
		})
		
		It("should return false when dynamic provisioning is disabled in NodePool", func() {
			nodePool := &v1.NodePool{
				Spec: v1.NodePoolSpec{
					DynamicProvisioning: &v1.DynamicProvisioning{
						Enabled: false,
					},
				},
			}
			
			Expect(controller.ShouldUseDynamicProvisioning(nodePool)).To(BeFalse())
		})
		
		It("should return true when both feature flag and NodePool config are enabled", func() {
			nodePool := &v1.NodePool{
				Spec: v1.NodePoolSpec{
					DynamicProvisioning: &v1.DynamicProvisioning{
						Enabled: true,
					},
				},
			}
			
			Expect(controller.ShouldUseDynamicProvisioning(nodePool)).To(BeTrue())
		})
	})

	Describe("InterceptGetInstanceTypes", func() {
		var (
			cloudProvider *fake.CloudProvider
			nodePool      *v1.NodePool
		)
		
		BeforeEach(func() {
			os.Setenv(dynamicprovisioning.EnvVarFeatureFlag, "true")
			controller = dynamicprovisioning.NewController()
			cloudProvider = &fake.CloudProvider{}
		})
		
		AfterEach(func() {
			os.Unsetenv(dynamicprovisioning.EnvVarFeatureFlag)
		})
		
		It("should fall back to standard instance types when dynamic provisioning is disabled", func() {
			nodePool = &v1.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pool"},
				Spec:       v1.NodePoolSpec{},
			}
			
			instanceTypes, err := controller.InterceptGetInstanceTypes(ctx, cloudProvider, nodePool)
			Expect(err).ToNot(HaveOccurred())
			Expect(instanceTypes).ToNot(BeEmpty())
			// Should return standard instance types from fake provider
		})
		
		It("should use dynamic provisioning when enabled", func() {
			nodePool = &v1.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pool"},
				Spec: v1.NodePoolSpec{
					DynamicProvisioning: &v1.DynamicProvisioning{
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
							AllowedShapes: []string{"VM.Standard.E4.Flex"},
						},
					},
				},
			}
			
			instanceTypes, err := controller.InterceptGetInstanceTypes(ctx, cloudProvider, nodePool)
			Expect(err).ToNot(HaveOccurred())
			Expect(instanceTypes).ToNot(BeEmpty())
		})
	})

	Describe("GenerateDynamicNodeClaim", func() {
		var (
			nodePool   *v1.NodePool
			pods       []*corev1.Pod
			calculator *dynamicshape.Calculator
			packer     *binpacking.FlexiblePacker
		)
		
		BeforeEach(func() {
			os.Setenv(dynamicprovisioning.EnvVarFeatureFlag, "true")
			controller = dynamicprovisioning.NewController()
			
			nodePool = &v1.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pool"},
				Spec: v1.NodePoolSpec{
					DynamicProvisioning: &v1.DynamicProvisioning{
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
							AllowedShapes: []string{"VM.Standard.E4.Flex"},
						},
						Overhead: &v1.SystemOverhead{
							SystemReservedCPU:    "200m",
							SystemReservedMemory: "1Gi",
						},
						Buffers: &v1.ResourceBuffers{
							CPUHeadroomPercent:    10,
							MemoryHeadroomPercent: 15,
						},
					},
				},
			}
			
			dp := nodePool.Spec.DynamicProvisioning
			calculator = dynamicshape.NewCalculator(&dp.Constraints, dp.Overhead, dp.Buffers)
			packer = binpacking.NewFlexiblePacker(&dp.Constraints, dp.Overhead, dp.Buffers, &mockPricingProvider{})
			
			pods = []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				}),
			}
		})
		
		AfterEach(func() {
			os.Unsetenv(dynamicprovisioning.EnvVarFeatureFlag)
		})
		
		It("should generate a dynamic node claim for pods", func() {
			claim, err := controller.GenerateDynamicNodeClaim(ctx, nodePool, pods, calculator, packer)
			Expect(err).ToNot(HaveOccurred())
			Expect(claim).ToNot(BeNil())
			
			Expect(claim.NodePoolName).To(Equal("test-pool"))
			Expect(claim.Shape).To(Equal("VM.Standard.E4.Flex"))
			Expect(claim.OCPUs).To(BeNumerically(">=", 2))
			Expect(claim.MemoryGB).To(BeNumerically(">=", 8))
			Expect(claim.Pods).To(HaveLen(1))
			Expect(claim.EstimatedCost).To(BeNumerically(">", 0))
		})
		
		It("should generate correct instance type name", func() {
			claim, err := controller.GenerateDynamicNodeClaim(ctx, nodePool, pods, calculator, packer)
			Expect(err).ToNot(HaveOccurred())
			
			instanceTypeName := claim.ToInstanceTypeName()
			Expect(instanceTypeName).To(MatchRegexp(`VM\.Standard\.E4\.Flex-\d+-\d+`))
		})
		
		It("should handle multiple pods", func() {
			morePods := append(pods,
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
			)
			
			claim, err := controller.GenerateDynamicNodeClaim(ctx, nodePool, morePods, calculator, packer)
			Expect(err).ToNot(HaveOccurred())
			Expect(claim).ToNot(BeNil())
			
			// Should be sized to fit all pods
			Expect(claim.OCPUs).To(BeNumerically(">=", 2)) // 3.5 CPU rounds to 2 OCPUs minimum
			Expect(claim.MemoryGB).To(BeNumerically(">=", 14)) // 14Gi + overhead
		})
		
		It("should return error when dynamic provisioning is not enabled", func() {
			nodePool.Spec.DynamicProvisioning.Enabled = false
			
			_, err := controller.GenerateDynamicNodeClaim(ctx, nodePool, pods, calculator, packer)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("dynamic provisioning not enabled"))
		})
	})
})

var _ = Describe("Scheduler Integration", func() {
	var (
		ctx         context.Context
		controller  *dynamicprovisioning.Controller
		integration *dynamicprovisioning.SchedulerIntegration
	)
	
	BeforeEach(func() {
		ctx = context.Background()
		os.Setenv(dynamicprovisioning.EnvVarFeatureFlag, "true")
		controller = dynamicprovisioning.NewController()
		integration = dynamicprovisioning.NewSchedulerIntegration(controller)
	})
	
	AfterEach(func() {
		os.Unsetenv(dynamicprovisioning.EnvVarFeatureFlag)
	})
	
	Describe("GenerateDynamicInstanceTypes", func() {
		It("should generate instance types for a nodepool", func() {
			nodePool := &v1.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pool"},
				Spec: v1.NodePoolSpec{
					DynamicProvisioning: &v1.DynamicProvisioning{
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
							AllowedShapes: []string{"VM.Standard.E4.Flex"},
						},
					},
				},
			}
			
			instanceTypes, err := integration.GenerateDynamicInstanceTypes(ctx, nodePool, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(instanceTypes).ToNot(BeEmpty())
			
			// Verify instance type properties
			for _, it := range instanceTypes {
				Expect(it.Name).To(MatchRegexp(`VM\.Standard\.E4\.Flex-\d+-\d+`))
				Expect(it.Capacity).ToNot(BeEmpty())
				Expect(it.Offerings).ToNot(BeEmpty())
				
				// Verify offerings include both capacity types
				hasOnDemand := false
				hasPreemptible := false
				for _, o := range it.Offerings {
					if o.Requirements.Get(v1.CapacityTypeLabelKey).Has(v1.CapacityTypeOnDemand) {
						hasOnDemand = true
					}
					if o.Requirements.Get(v1.CapacityTypeLabelKey).Has("preemptible") {
						hasPreemptible = true
					}
				}
				Expect(hasOnDemand).To(BeTrue())
				Expect(hasPreemptible).To(BeTrue())
			}
		})
		
		It("should generate instance types based on pod requirements", func() {
			nodePool := &v1.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pool"},
				Spec: v1.NodePoolSpec{
					DynamicProvisioning: &v1.DynamicProvisioning{
						Enabled:  true,
						Strategy: v1.StrategyExactFit,
						CapacityType: v1.CapacityTypeRatio{
							Preemptible: 100,
							OnDemand:    0,
						},
						Constraints: v1.DynamicConstraints{
							MinOCPUs:      1,
							MaxOCPUs:      32,
							MinMemoryGB:   1,
							MaxMemoryGB:   256,
							AllowedShapes: []string{"VM.Standard.E4.Flex"},
						},
					},
				},
			}
			
			pods := []*corev1.Pod{
				test.Pod(test.PodOptions{
					ResourceRequirements: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("4"),
							corev1.ResourceMemory: resource.MustParse("16Gi"),
						},
					},
				}),
			}
			
			instanceTypes, err := integration.GenerateDynamicInstanceTypes(ctx, nodePool, pods)
			Expect(err).ToNot(HaveOccurred())
			Expect(instanceTypes).ToNot(BeEmpty())
			
			// Should have instance types that can fit the pod
			for _, it := range instanceTypes {
				cpu := it.Capacity[corev1.ResourceCPU]
				memory := it.Capacity[corev1.ResourceMemory]
				
				// Account for overhead
				Expect(cpu.MilliValue()).To(BeNumerically(">=", 4000))
				Expect(memory.Value()).To(BeNumerically(">=", 16*1024*1024*1024))
			}
		})
	})
	
	Describe("EnhanceNodeClaimTemplate", func() {
		It("should add dynamic provisioning metadata to template", func() {
			template := &scheduling.NodeClaimTemplate{
				NodePoolName: "test-pool",
			}
			
			nodePool := &v1.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pool"},
				Spec: v1.NodePoolSpec{
					DynamicProvisioning: &v1.DynamicProvisioning{
						Enabled:  true,
						Strategy: v1.StrategyBestFit,
						CapacityType: v1.CapacityTypeRatio{
							Preemptible: 50,
							OnDemand:    50,
						},
						Constraints: v1.DynamicConstraints{
							MinOCPUs:      1,
							MaxOCPUs:      32,
							MinMemoryGB:   1,
							MaxMemoryGB:   256,
							AllowedShapes: []string{"VM.Standard.E4.Flex"},
						},
					},
				},
			}
			
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
			
			err := integration.EnhanceNodeClaimTemplate(ctx, template, nodePool, pods, &mockPricingProvider{})
			Expect(err).ToNot(HaveOccurred())
			
			// Verify metadata was added
			Expect(template.Annotations).ToNot(BeNil())
			Expect(template.Annotations["karpenter.sh/dynamic-shape"]).To(Equal("VM.Standard.E4.Flex"))
			Expect(template.Annotations["karpenter.sh/dynamic-ocpus"]).ToNot(BeEmpty())
			Expect(template.Annotations["karpenter.sh/dynamic-memory-gb"]).ToNot(BeEmpty())
			Expect(template.Annotations["karpenter.sh/dynamic-efficiency"]).ToNot(BeEmpty())
		})
	})
})