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
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	namespace = "karpenter"
	subsystem = "dynamic_provisioning"
)

var (
	// ShapeEfficiencyGauge tracks the efficiency of dynamically provisioned shapes
	ShapeEfficiencyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "shape_efficiency_percentage",
			Help:      "The efficiency percentage of dynamically provisioned shapes (requested/provisioned * 100)",
		},
		[]string{"nodepool", "shape", "resource_type"},
	)

	// ProvisioningDurationHistogram tracks the duration of dynamic provisioning operations
	ProvisioningDurationHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "provisioning_duration_seconds",
			Help:      "The duration of dynamic provisioning operations in seconds",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 10), // 1s to ~17min
		},
		[]string{"nodepool", "strategy", "result"},
	)

	// CostSavingsGauge tracks estimated cost savings from dynamic provisioning
	CostSavingsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "cost_savings_percentage",
			Help:      "Estimated cost savings percentage compared to fixed instance types",
		},
		[]string{"nodepool"},
	)

	// CapacityTypeDistributionGauge tracks the distribution of capacity types
	CapacityTypeDistributionGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "capacity_type_distribution",
			Help:      "Distribution of capacity types (preemptible vs on-demand)",
		},
		[]string{"nodepool", "capacity_type"},
	)

	// ProvisioningSuccessRateGauge tracks the success rate of provisioning operations
	ProvisioningSuccessRateGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "provisioning_success_rate",
			Help:      "Success rate of dynamic provisioning operations (rolling window)",
		},
		[]string{"nodepool"},
	)

	// ShapeConfigurationCounter tracks the number of times each shape configuration is used
	ShapeConfigurationCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "shape_configuration_total",
			Help:      "Total number of times each shape configuration has been provisioned",
		},
		[]string{"nodepool", "shape", "ocpus", "memory_gb", "capacity_type"},
	)

	// UnschedulablePodsGauge tracks pods that couldn't be scheduled with dynamic provisioning
	UnschedulablePodsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "unschedulable_pods",
			Help:      "Number of pods that couldn't be scheduled with dynamic provisioning",
		},
		[]string{"nodepool", "reason"},
	)

	// ResourceUtilizationHistogram tracks resource utilization distribution
	ResourceUtilizationHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "resource_utilization_percentage",
			Help:      "Distribution of resource utilization percentages",
			Buckets:   prometheus.LinearBuckets(0, 10, 11), // 0% to 100% in 10% increments
		},
		[]string{"nodepool", "resource_type"},
	)

	// BinPackingEfficiencyGauge tracks the efficiency of bin packing
	BinPackingEfficiencyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "bin_packing_efficiency",
			Help:      "Efficiency of bin packing algorithm (packed pods / total pods)",
		},
		[]string{"nodepool", "strategy"},
	)

	// DynamicShapeCalculationDurationHistogram tracks shape calculation duration
	DynamicShapeCalculationDurationHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "shape_calculation_duration_seconds",
			Help:      "Duration of dynamic shape calculations in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
		},
		[]string{"nodepool", "pod_count"},
	)
)

func init() {
	// Register all metrics
	metrics.Registry.MustRegister(
		ShapeEfficiencyGauge,
		ProvisioningDurationHistogram,
		CostSavingsGauge,
		CapacityTypeDistributionGauge,
		ProvisioningSuccessRateGauge,
		ShapeConfigurationCounter,
		UnschedulablePodsGauge,
		ResourceUtilizationHistogram,
		BinPackingEfficiencyGauge,
		DynamicShapeCalculationDurationHistogram,
	)
}

// Collector handles metric collection for dynamic provisioning
type Collector struct {
	// Track success/failure rates with a rolling window
	successWindow *RollingWindow
	failureWindow *RollingWindow
}

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	return &Collector{
		successWindow: NewRollingWindow(5 * time.Minute),
		failureWindow: NewRollingWindow(5 * time.Minute),
	}
}

// RecordShapeEfficiency records the efficiency of a dynamically provisioned shape
func (c *Collector) RecordShapeEfficiency(nodePool string, shape string, cpuEfficiency, memoryEfficiency float64) {
	ShapeEfficiencyGauge.WithLabelValues(nodePool, shape, "cpu").Set(cpuEfficiency)
	ShapeEfficiencyGauge.WithLabelValues(nodePool, shape, "memory").Set(memoryEfficiency)
}

// RecordProvisioningDuration records the duration of a provisioning operation
func (c *Collector) RecordProvisioningDuration(nodePool string, strategy v1.ProvisioningStrategy, duration time.Duration, success bool) {
	result := "success"
	if !success {
		result = "failure"
	}
	ProvisioningDurationHistogram.WithLabelValues(nodePool, string(strategy), result).Observe(duration.Seconds())
	
	// Update success rate
	if success {
		c.successWindow.Add(1)
	} else {
		c.failureWindow.Add(1)
	}
	c.updateSuccessRate(nodePool)
}

// RecordCostSavings records estimated cost savings
func (c *Collector) RecordCostSavings(nodePool string, savingsPercentage float64) {
	CostSavingsGauge.WithLabelValues(nodePool).Set(savingsPercentage)
}

// RecordCapacityTypeDistribution records the distribution of capacity types
func (c *Collector) RecordCapacityTypeDistribution(nodePool string, preemptibleCount, onDemandCount int) {
	total := float64(preemptibleCount + onDemandCount)
	if total > 0 {
		CapacityTypeDistributionGauge.WithLabelValues(nodePool, "preemptible").Set(float64(preemptibleCount) / total * 100)
		CapacityTypeDistributionGauge.WithLabelValues(nodePool, "on-demand").Set(float64(onDemandCount) / total * 100)
	}
}

// RecordShapeConfiguration records a shape configuration that was provisioned
func (c *Collector) RecordShapeConfiguration(nodePool, shape string, ocpus, memoryGB int32, capacityType string) {
	ShapeConfigurationCounter.WithLabelValues(
		nodePool,
		shape,
		fmt.Sprintf("%d", ocpus),
		fmt.Sprintf("%d", memoryGB),
		capacityType,
	).Inc()
}

// RecordUnschedulablePods records the number of unschedulable pods
func (c *Collector) RecordUnschedulablePods(nodePool string, count int, reason string) {
	UnschedulablePodsGauge.WithLabelValues(nodePool, reason).Set(float64(count))
}

// RecordResourceUtilization records resource utilization
func (c *Collector) RecordResourceUtilization(nodePool string, cpuUtilization, memoryUtilization float64) {
	ResourceUtilizationHistogram.WithLabelValues(nodePool, "cpu").Observe(cpuUtilization)
	ResourceUtilizationHistogram.WithLabelValues(nodePool, "memory").Observe(memoryUtilization)
}

// RecordBinPackingEfficiency records bin packing efficiency
func (c *Collector) RecordBinPackingEfficiency(nodePool string, strategy v1.ProvisioningStrategy, packedPods, totalPods int) {
	efficiency := float64(0)
	if totalPods > 0 {
		efficiency = float64(packedPods) / float64(totalPods) * 100
	}
	BinPackingEfficiencyGauge.WithLabelValues(nodePool, string(strategy)).Set(efficiency)
}

// RecordShapeCalculationDuration records the duration of shape calculations
func (c *Collector) RecordShapeCalculationDuration(nodePool string, podCount int, duration time.Duration) {
	DynamicShapeCalculationDurationHistogram.WithLabelValues(
		nodePool,
		fmt.Sprintf("%d", podCount),
	).Observe(duration.Seconds())
}

// updateSuccessRate updates the rolling success rate
func (c *Collector) updateSuccessRate(nodePool string) {
	successes := c.successWindow.Sum()
	failures := c.failureWindow.Sum()
	total := successes + failures
	
	if total > 0 {
		successRate := float64(successes) / float64(total) * 100
		ProvisioningSuccessRateGauge.WithLabelValues(nodePool).Set(successRate)
	}
}

// RollingWindow tracks values over a time window
type RollingWindow struct {
	window   time.Duration
	buckets  map[time.Time]float64
}

// NewRollingWindow creates a new rolling window
func NewRollingWindow(window time.Duration) *RollingWindow {
	return &RollingWindow{
		window:  window,
		buckets: make(map[time.Time]float64),
	}
}

// Add adds a value to the current bucket
func (rw *RollingWindow) Add(value float64) {
	now := time.Now().Truncate(time.Second)
	rw.buckets[now] = rw.buckets[now] + value
	rw.cleanup()
}

// Sum returns the sum of all values in the window
func (rw *RollingWindow) Sum() float64 {
	rw.cleanup()
	sum := float64(0)
	for _, v := range rw.buckets {
		sum += v
	}
	return sum
}

// cleanup removes old buckets outside the window
func (rw *RollingWindow) cleanup() {
	cutoff := time.Now().Add(-rw.window)
	for t := range rw.buckets {
		if t.Before(cutoff) {
			delete(rw.buckets, t)
		}
	}
}

// MetricsLogger provides structured logging with metrics context
type MetricsLogger struct {
	collector *Collector
}

// NewMetricsLogger creates a new metrics logger
func NewMetricsLogger(collector *Collector) *MetricsLogger {
	return &MetricsLogger{
		collector: collector,
	}
}

// LogProvisioningDecision logs a provisioning decision with metrics
func (ml *MetricsLogger) LogProvisioningDecision(ctx context.Context, decision *ProvisioningDecision) {
	logger := log.FromContext(ctx)
	
	logger.Info("dynamic provisioning decision",
		"nodepool", decision.NodePool,
		"strategy", decision.Strategy,
		"shape", decision.Shape,
		"ocpus", decision.OCPUs,
		"memoryGB", decision.MemoryGB,
		"capacityType", decision.CapacityType,
		"podCount", decision.PodCount,
		"cpuRequested", decision.CPURequested,
		"memoryRequested", decision.MemoryRequested,
		"cpuEfficiency", decision.CPUEfficiency,
		"memoryEfficiency", decision.MemoryEfficiency,
		"estimatedCost", decision.EstimatedCost,
	)
	
	// Record metrics
	ml.collector.RecordShapeEfficiency(decision.NodePool, decision.Shape, decision.CPUEfficiency, decision.MemoryEfficiency)
	ml.collector.RecordShapeConfiguration(decision.NodePool, decision.Shape, decision.OCPUs, decision.MemoryGB, decision.CapacityType)
}

// ProvisioningDecision represents a dynamic provisioning decision
type ProvisioningDecision struct {
	NodePool          string
	Strategy          v1.ProvisioningStrategy
	Shape             string
	OCPUs             int32
	MemoryGB          int32
	CapacityType      string
	PodCount          int
	CPURequested      string
	MemoryRequested   string
	CPUEfficiency     float64
	MemoryEfficiency  float64
	EstimatedCost     float64
}