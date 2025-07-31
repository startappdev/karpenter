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

package dynamicprovisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// AnalyzerCmd returns the analyzer command
func AnalyzerCmd(kubeClient client.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze-dynamic-provisioning",
		Short: "Analyze dynamic provisioning efficiency and costs",
		Long: `Analyze dynamic provisioning efficiency and generate cost reports.
		
This command provides insights into:
- Resource utilization efficiency
- Cost savings compared to fixed instance types
- Capacity type distribution
- Shape configuration usage patterns`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalyzer(cmd.Context(), kubeClient, cmd)
		},
	}
	
	cmd.Flags().String("nodepool", "", "Filter by specific NodePool name")
	cmd.Flags().String("output", "table", "Output format (table, json, yaml)")
	cmd.Flags().Duration("since", 24*time.Hour, "Analyze data from this duration ago")
	cmd.Flags().Bool("cost-report", false, "Generate detailed cost report")
	cmd.Flags().Bool("efficiency-report", false, "Generate detailed efficiency report")
	
	return cmd
}

// runAnalyzer runs the analyzer
func runAnalyzer(ctx context.Context, kubeClient client.Client, cmd *cobra.Command) error {
	nodePoolFilter, _ := cmd.Flags().GetString("nodepool")
	outputFormat, _ := cmd.Flags().GetString("output")
	since, _ := cmd.Flags().GetDuration("since")
	costReport, _ := cmd.Flags().GetBool("cost-report")
	efficiencyReport, _ := cmd.Flags().GetBool("efficiency-report")
	
	// Get all NodePools with dynamic provisioning enabled
	nodePools := &v1.NodePoolList{}
	if err := kubeClient.List(ctx, nodePools); err != nil {
		return fmt.Errorf("listing nodepools: %w", err)
	}
	
	// Filter for dynamic provisioning enabled
	dynamicNodePools := lo.Filter(nodePools.Items, func(np v1.NodePool, _ int) bool {
		if nodePoolFilter != "" && np.Name != nodePoolFilter {
			return false
		}
		return np.Spec.DynamicProvisioning != nil && np.Spec.DynamicProvisioning.Enabled
	})
	
	if len(dynamicNodePools) == 0 {
		fmt.Println("No NodePools found with dynamic provisioning enabled")
		return nil
	}
	
	// Get all NodeClaims
	nodeClaims := &v1.NodeClaimList{}
	if err := kubeClient.List(ctx, nodeClaims); err != nil {
		return fmt.Errorf("listing nodeclaims: %w", err)
	}
	
	// Filter NodeClaims by time and dynamic provisioning
	cutoff := time.Now().Add(-since)
	dynamicNodeClaims := lo.Filter(nodeClaims.Items, func(nc v1.NodeClaim, _ int) bool {
		if nc.CreationTimestamp.Time.Before(cutoff) {
			return false
		}
		return nc.Status.DynamicShape != nil
	})
	
	// Generate reports
	analyzer := &Analyzer{
		nodePools:   dynamicNodePools,
		nodeClaims:  dynamicNodeClaims,
		kubeClient:  kubeClient,
	}
	
	switch outputFormat {
	case "table":
		if costReport {
			return analyzer.printCostReport()
		}
		if efficiencyReport {
			return analyzer.printEfficiencyReport()
		}
		return analyzer.printSummary()
	case "json":
		return analyzer.printJSON()
	case "yaml":
		return analyzer.printYAML()
	default:
		return fmt.Errorf("unsupported output format: %s", outputFormat)
	}
}

// Analyzer analyzes dynamic provisioning data
type Analyzer struct {
	nodePools  []v1.NodePool
	nodeClaims []v1.NodeClaim
	kubeClient client.Client
}

// AnalysisResult contains the analysis results
type AnalysisResult struct {
	Summary           Summary                       `json:"summary"`
	NodePoolAnalysis  []NodePoolAnalysis           `json:"nodePoolAnalysis"`
	ShapeDistribution map[string]ShapeStats        `json:"shapeDistribution"`
	CostAnalysis      CostAnalysis                 `json:"costAnalysis"`
	TimeRange         TimeRange                    `json:"timeRange"`
}

// Summary contains summary statistics
type Summary struct {
	TotalNodePools      int     `json:"totalNodePools"`
	TotalNodeClaims     int     `json:"totalNodeClaims"`
	AverageCPUEfficiency   float64 `json:"averageCpuEfficiency"`
	AverageMemoryEfficiency float64 `json:"averageMemoryEfficiency"`
	TotalEstimatedCost     float64 `json:"totalEstimatedCost"`
	TotalCostSavings       float64 `json:"totalCostSavings"`
	PreemptiblePercentage  float64 `json:"preemptiblePercentage"`
}

// NodePoolAnalysis contains analysis for a specific NodePool
type NodePoolAnalysis struct {
	NodePoolName    string          `json:"nodePoolName"`
	Strategy        string          `json:"strategy"`
	NodeClaimCount  int             `json:"nodeClaimCount"`
	ShapeUsage      []ShapeUsage    `json:"shapeUsage"`
	EfficiencyStats EfficiencyStats `json:"efficiencyStats"`
	CostStats       CostStats       `json:"costStats"`
}

// ShapeUsage tracks usage of a specific shape
type ShapeUsage struct {
	Shape    string `json:"shape"`
	Count    int    `json:"count"`
	AvgOCPUs int32  `json:"avgOcpus"`
	AvgMemGB int32  `json:"avgMemoryGb"`
}

// EfficiencyStats contains efficiency statistics
type EfficiencyStats struct {
	AvgCPUEfficiency    float64 `json:"avgCpuEfficiency"`
	MinCPUEfficiency    float64 `json:"minCpuEfficiency"`
	MaxCPUEfficiency    float64 `json:"maxCpuEfficiency"`
	AvgMemoryEfficiency float64 `json:"avgMemoryEfficiency"`
	MinMemoryEfficiency float64 `json:"minMemoryEfficiency"`
	MaxMemoryEfficiency float64 `json:"maxMemoryEfficiency"`
}

// CostStats contains cost statistics
type CostStats struct {
	TotalCost        float64 `json:"totalCost"`
	AverageCost      float64 `json:"averageCost"`
	EstimatedSavings float64 `json:"estimatedSavings"`
}

// ShapeStats contains statistics for a shape
type ShapeStats struct {
	Count             int     `json:"count"`
	TotalOCPUs        int32   `json:"totalOcpus"`
	TotalMemoryGB     int32   `json:"totalMemoryGb"`
	PreemptibleCount  int     `json:"preemptibleCount"`
	OnDemandCount     int     `json:"onDemandCount"`
	AvgEfficiency     float64 `json:"avgEfficiency"`
}

// CostAnalysis contains cost analysis
type CostAnalysis struct {
	TotalEstimatedCost      float64            `json:"totalEstimatedCost"`
	CostByCapacityType      map[string]float64 `json:"costByCapacityType"`
	CostByShape             map[string]float64 `json:"costByShape"`
	PotentialSavings        float64            `json:"potentialSavings"`
	SavingsPercentage       float64            `json:"savingsPercentage"`
}

// TimeRange contains the time range of analysis
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// analyze performs the analysis
func (a *Analyzer) analyze() (*AnalysisResult, error) {
	result := &AnalysisResult{
		NodePoolAnalysis:  make([]NodePoolAnalysis, 0),
		ShapeDistribution: make(map[string]ShapeStats),
		CostAnalysis: CostAnalysis{
			CostByCapacityType: make(map[string]float64),
			CostByShape:        make(map[string]float64),
		},
	}
	
	// Calculate time range
	if len(a.nodeClaims) > 0 {
		oldest := a.nodeClaims[0].CreationTimestamp.Time
		newest := a.nodeClaims[0].CreationTimestamp.Time
		
		for _, nc := range a.nodeClaims {
			if nc.CreationTimestamp.Time.Before(oldest) {
				oldest = nc.CreationTimestamp.Time
			}
			if nc.CreationTimestamp.Time.After(newest) {
				newest = nc.CreationTimestamp.Time
			}
		}
		
		result.TimeRange = TimeRange{
			Start: oldest,
			End:   newest,
		}
	}
	
	// Analyze each NodePool
	for _, np := range a.nodePools {
		npAnalysis := a.analyzeNodePool(np)
		result.NodePoolAnalysis = append(result.NodePoolAnalysis, npAnalysis)
	}
	
	// Aggregate statistics
	a.aggregateStats(result)
	
	return result, nil
}

// analyzeNodePool analyzes a specific NodePool
func (a *Analyzer) analyzeNodePool(np v1.NodePool) NodePoolAnalysis {
	analysis := NodePoolAnalysis{
		NodePoolName: np.Name,
		Strategy:     string(np.Spec.DynamicProvisioning.Strategy),
		ShapeUsage:   make([]ShapeUsage, 0),
	}
	
	// Filter NodeClaims for this NodePool
	nodeClaims := lo.Filter(a.nodeClaims, func(nc v1.NodeClaim, _ int) bool {
		return nc.Labels[v1.NodePoolLabelKey] == np.Name
	})
	
	analysis.NodeClaimCount = len(nodeClaims)
	
	// Calculate shape usage
	shapeMap := make(map[string]*ShapeUsage)
	var totalCPUEff, totalMemEff float64
	var minCPUEff, maxCPUEff float64 = 100, 0
	var minMemEff, maxMemEff float64 = 100, 0
	var totalCost float64
	
	for _, nc := range nodeClaims {
		if nc.Status.DynamicShape == nil {
			continue
		}
		
		ds := nc.Status.DynamicShape
		key := ds.Shape
		
		if _, ok := shapeMap[key]; !ok {
			shapeMap[key] = &ShapeUsage{Shape: ds.Shape}
		}
		
		shapeMap[key].Count++
		shapeMap[key].AvgOCPUs += ds.OCPUs
		shapeMap[key].AvgMemGB += ds.MemoryGB
		
		// Update efficiency stats
		cpuEff := float64(ds.Efficiency.CPUEfficiency)
		memEff := float64(ds.Efficiency.MemoryEfficiency)
		
		totalCPUEff += cpuEff
		totalMemEff += memEff
		
		if cpuEff < minCPUEff {
			minCPUEff = cpuEff
		}
		if cpuEff > maxCPUEff {
			maxCPUEff = cpuEff
		}
		if memEff < minMemEff {
			minMemEff = memEff
		}
		if memEff > maxMemEff {
			maxMemEff = memEff
		}
		
		// Update cost
		if ds.Pricing != nil {
			cost, _ := parseFloat(ds.Pricing.HourlyRate)
			totalCost += cost
		}
	}
	
	// Calculate averages
	for _, usage := range shapeMap {
		usage.AvgOCPUs /= int32(usage.Count)
		usage.AvgMemGB /= int32(usage.Count)
		analysis.ShapeUsage = append(analysis.ShapeUsage, *usage)
	}
	
	// Sort by count
	sort.Slice(analysis.ShapeUsage, func(i, j int) bool {
		return analysis.ShapeUsage[i].Count > analysis.ShapeUsage[j].Count
	})
	
	// Set efficiency stats
	if len(nodeClaims) > 0 {
		analysis.EfficiencyStats = EfficiencyStats{
			AvgCPUEfficiency:    totalCPUEff / float64(len(nodeClaims)),
			MinCPUEfficiency:    minCPUEff,
			MaxCPUEfficiency:    maxCPUEff,
			AvgMemoryEfficiency: totalMemEff / float64(len(nodeClaims)),
			MinMemoryEfficiency: minMemEff,
			MaxMemoryEfficiency: maxMemEff,
		}
		
		analysis.CostStats = CostStats{
			TotalCost:   totalCost,
			AverageCost: totalCost / float64(len(nodeClaims)),
			// Estimate savings (assumes 30% savings vs fixed instances)
			EstimatedSavings: totalCost * 0.3,
		}
	}
	
	return analysis
}

// aggregateStats aggregates statistics across all NodePools
func (a *Analyzer) aggregateStats(result *AnalysisResult) {
	var totalCPUEff, totalMemEff float64
	var totalCost, totalSavings float64
	var preemptibleCount, onDemandCount int
	
	for _, npAnalysis := range result.NodePoolAnalysis {
		result.Summary.TotalNodeClaims += npAnalysis.NodeClaimCount
		
		if npAnalysis.NodeClaimCount > 0 {
			totalCPUEff += npAnalysis.EfficiencyStats.AvgCPUEfficiency * float64(npAnalysis.NodeClaimCount)
			totalMemEff += npAnalysis.EfficiencyStats.AvgMemoryEfficiency * float64(npAnalysis.NodeClaimCount)
			totalCost += npAnalysis.CostStats.TotalCost
			totalSavings += npAnalysis.CostStats.EstimatedSavings
		}
	}
	
	// Calculate shape distribution
	for _, nc := range a.nodeClaims {
		if nc.Status.DynamicShape == nil {
			continue
		}
		
		ds := nc.Status.DynamicShape
		key := ds.Shape
		
		stats, ok := result.ShapeDistribution[key]
		if !ok {
			stats = ShapeStats{}
		}
		
		stats.Count++
		stats.TotalOCPUs += ds.OCPUs
		stats.TotalMemoryGB += ds.MemoryGB
		
		if ds.CapacityType == "preemptible" {
			stats.PreemptibleCount++
			preemptibleCount++
		} else {
			stats.OnDemandCount++
			onDemandCount++
		}
		
		stats.AvgEfficiency = (float64(ds.Efficiency.CPUEfficiency) + float64(ds.Efficiency.MemoryEfficiency)) / 2
		
		result.ShapeDistribution[key] = stats
		
		// Update cost analysis
		if ds.Pricing != nil {
			cost, _ := parseFloat(ds.Pricing.HourlyRate)
			result.CostAnalysis.CostByCapacityType[ds.CapacityType] += cost
			result.CostAnalysis.CostByShape[ds.Shape] += cost
		}
	}
	
	// Set summary
	result.Summary.TotalNodePools = len(a.nodePools)
	
	if result.Summary.TotalNodeClaims > 0 {
		result.Summary.AverageCPUEfficiency = totalCPUEff / float64(result.Summary.TotalNodeClaims)
		result.Summary.AverageMemoryEfficiency = totalMemEff / float64(result.Summary.TotalNodeClaims)
		result.Summary.PreemptiblePercentage = float64(preemptibleCount) / float64(result.Summary.TotalNodeClaims) * 100
	}
	
	result.Summary.TotalEstimatedCost = totalCost
	result.Summary.TotalCostSavings = totalSavings
	
	result.CostAnalysis.TotalEstimatedCost = totalCost
	result.CostAnalysis.PotentialSavings = totalSavings
	if totalCost > 0 {
		result.CostAnalysis.SavingsPercentage = totalSavings / totalCost * 100
	}
}

// printSummary prints a summary table
func (a *Analyzer) printSummary() error {
	result, err := a.analyze()
	if err != nil {
		return err
	}
	
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()
	
	fmt.Fprintln(w, "DYNAMIC PROVISIONING SUMMARY")
	fmt.Fprintln(w, strings.Repeat("=", 80))
	fmt.Fprintf(w, "Time Range:\t%s to %s\n", result.TimeRange.Start.Format(time.RFC3339), result.TimeRange.End.Format(time.RFC3339))
	fmt.Fprintf(w, "NodePools:\t%d\n", result.Summary.TotalNodePools)
	fmt.Fprintf(w, "NodeClaims:\t%d\n", result.Summary.TotalNodeClaims)
	fmt.Fprintf(w, "Avg CPU Efficiency:\t%.1f%%\n", result.Summary.AverageCPUEfficiency)
	fmt.Fprintf(w, "Avg Memory Efficiency:\t%.1f%%\n", result.Summary.AverageMemoryEfficiency)
	fmt.Fprintf(w, "Preemptible:\t%.1f%%\n", result.Summary.PreemptiblePercentage)
	fmt.Fprintf(w, "Total Cost:\t$%.2f\n", result.Summary.TotalEstimatedCost)
	fmt.Fprintf(w, "Est. Savings:\t$%.2f (%.1f%%)\n", result.Summary.TotalCostSavings, result.CostAnalysis.SavingsPercentage)
	fmt.Fprintln(w)
	
	// NodePool details
	fmt.Fprintln(w, "NODEPOOL\tSTRATEGY\tNODES\tAVG CPU EFF\tAVG MEM EFF\tTOTAL COST")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	
	for _, np := range result.NodePoolAnalysis {
		fmt.Fprintf(w, "%s\t%s\t%d\t%.1f%%\t%.1f%%\t$%.2f\n",
			np.NodePoolName,
			np.Strategy,
			np.NodeClaimCount,
			np.EfficiencyStats.AvgCPUEfficiency,
			np.EfficiencyStats.AvgMemoryEfficiency,
			np.CostStats.TotalCost,
		)
	}
	fmt.Fprintln(w)
	
	// Shape distribution
	fmt.Fprintln(w, "SHAPE DISTRIBUTION")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	fmt.Fprintln(w, "SHAPE\tCOUNT\tTOTAL OCPUS\tTOTAL MEM(GB)\tPREEMPTIBLE\tON-DEMAND")
	
	for shape, stats := range result.ShapeDistribution {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\n",
			shape,
			stats.Count,
			stats.TotalOCPUs,
			stats.TotalMemoryGB,
			stats.PreemptibleCount,
			stats.OnDemandCount,
		)
	}
	
	return nil
}

// printCostReport prints a detailed cost report
func (a *Analyzer) printCostReport() error {
	result, err := a.analyze()
	if err != nil {
		return err
	}
	
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()
	
	fmt.Fprintln(w, "DYNAMIC PROVISIONING COST REPORT")
	fmt.Fprintln(w, strings.Repeat("=", 80))
	fmt.Fprintf(w, "Time Range:\t%s to %s\n", result.TimeRange.Start.Format(time.RFC3339), result.TimeRange.End.Format(time.RFC3339))
	fmt.Fprintf(w, "Total Estimated Cost:\t$%.2f\n", result.CostAnalysis.TotalEstimatedCost)
	fmt.Fprintf(w, "Potential Savings:\t$%.2f (%.1f%%)\n", result.CostAnalysis.PotentialSavings, result.CostAnalysis.SavingsPercentage)
	fmt.Fprintln(w)
	
	// Cost by capacity type
	fmt.Fprintln(w, "COST BY CAPACITY TYPE")
	fmt.Fprintln(w, strings.Repeat("-", 40))
	for capType, cost := range result.CostAnalysis.CostByCapacityType {
		percentage := cost / result.CostAnalysis.TotalEstimatedCost * 100
		fmt.Fprintf(w, "%s:\t$%.2f (%.1f%%)\n", capType, cost, percentage)
	}
	fmt.Fprintln(w)
	
	// Cost by shape
	fmt.Fprintln(w, "COST BY SHAPE")
	fmt.Fprintln(w, strings.Repeat("-", 40))
	
	// Sort shapes by cost
	type shapeCost struct {
		shape string
		cost  float64
	}
	shapes := make([]shapeCost, 0, len(result.CostAnalysis.CostByShape))
	for shape, cost := range result.CostAnalysis.CostByShape {
		shapes = append(shapes, shapeCost{shape, cost})
	}
	sort.Slice(shapes, func(i, j int) bool {
		return shapes[i].cost > shapes[j].cost
	})
	
	for _, sc := range shapes {
		percentage := sc.cost / result.CostAnalysis.TotalEstimatedCost * 100
		fmt.Fprintf(w, "%s:\t$%.2f (%.1f%%)\n", sc.shape, sc.cost, percentage)
	}
	fmt.Fprintln(w)
	
	// NodePool cost breakdown
	fmt.Fprintln(w, "NODEPOOL COST BREAKDOWN")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	fmt.Fprintln(w, "NODEPOOL\tTOTAL COST\tAVG COST/NODE\tEST. SAVINGS")
	
	for _, np := range result.NodePoolAnalysis {
		fmt.Fprintf(w, "%s\t$%.2f\t$%.2f\t$%.2f\n",
			np.NodePoolName,
			np.CostStats.TotalCost,
			np.CostStats.AverageCost,
			np.CostStats.EstimatedSavings,
		)
	}
	
	return nil
}

// printEfficiencyReport prints a detailed efficiency report
func (a *Analyzer) printEfficiencyReport() error {
	result, err := a.analyze()
	if err != nil {
		return err
	}
	
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()
	
	fmt.Fprintln(w, "DYNAMIC PROVISIONING EFFICIENCY REPORT")
	fmt.Fprintln(w, strings.Repeat("=", 80))
	fmt.Fprintf(w, "Time Range:\t%s to %s\n", result.TimeRange.Start.Format(time.RFC3339), result.TimeRange.End.Format(time.RFC3339))
	fmt.Fprintf(w, "Overall CPU Efficiency:\t%.1f%%\n", result.Summary.AverageCPUEfficiency)
	fmt.Fprintf(w, "Overall Memory Efficiency:\t%.1f%%\n", result.Summary.AverageMemoryEfficiency)
	fmt.Fprintln(w)
	
	// NodePool efficiency details
	fmt.Fprintln(w, "NODEPOOL EFFICIENCY DETAILS")
	fmt.Fprintln(w, strings.Repeat("-", 100))
	fmt.Fprintln(w, "NODEPOOL\tCPU (MIN/AVG/MAX)\tMEMORY (MIN/AVG/MAX)")
	
	for _, np := range result.NodePoolAnalysis {
		fmt.Fprintf(w, "%s\t%.1f%% / %.1f%% / %.1f%%\t%.1f%% / %.1f%% / %.1f%%\n",
			np.NodePoolName,
			np.EfficiencyStats.MinCPUEfficiency,
			np.EfficiencyStats.AvgCPUEfficiency,
			np.EfficiencyStats.MaxCPUEfficiency,
			np.EfficiencyStats.MinMemoryEfficiency,
			np.EfficiencyStats.AvgMemoryEfficiency,
			np.EfficiencyStats.MaxMemoryEfficiency,
		)
	}
	fmt.Fprintln(w)
	
	// Shape usage patterns
	fmt.Fprintln(w, "SHAPE USAGE PATTERNS")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	
	for _, np := range result.NodePoolAnalysis {
		if len(np.ShapeUsage) == 0 {
			continue
		}
		
		fmt.Fprintf(w, "\n%s (Strategy: %s):\n", np.NodePoolName, np.Strategy)
		fmt.Fprintln(w, "SHAPE\tCOUNT\tAVG OCPUS\tAVG MEM(GB)")
		
		for _, usage := range np.ShapeUsage {
			fmt.Fprintf(w, "  %s\t%d\t%d\t%d\n",
				usage.Shape,
				usage.Count,
				usage.AvgOCPUs,
				usage.AvgMemGB,
			)
		}
	}
	
	// Efficiency distribution
	fmt.Fprintln(w)
	fmt.Fprintln(w, "EFFICIENCY DISTRIBUTION")
	fmt.Fprintln(w, strings.Repeat("-", 40))
	
	// Create efficiency buckets
	effBuckets := make(map[string]int)
	for _, nc := range a.nodeClaims {
		if nc.Status.DynamicShape == nil {
			continue
		}
		
		avgEff := (float64(nc.Status.DynamicShape.Efficiency.CPUEfficiency) + 
		          float64(nc.Status.DynamicShape.Efficiency.MemoryEfficiency)) / 2
		
		bucket := fmt.Sprintf("%d-%d%%", int(avgEff/10)*10, int(avgEff/10)*10+10)
		effBuckets[bucket]++
	}
	
	// Sort and display buckets
	bucketNames := []string{"0-10%", "10-20%", "20-30%", "30-40%", "40-50%", "50-60%", "60-70%", "70-80%", "80-90%", "90-100%"}
	for _, bucket := range bucketNames {
		count := effBuckets[bucket]
		if count > 0 {
			fmt.Fprintf(w, "%s:\t%d nodes\n", bucket, count)
		}
	}
	
	return nil
}

// printJSON prints results as JSON
func (a *Analyzer) printJSON() error {
	result, err := a.analyze()
	if err != nil {
		return err
	}
	
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// printYAML prints results as YAML
func (a *Analyzer) printYAML() error {
	result, err := a.analyze()
	if err != nil {
		return err
	}
	
	data, err := yaml.Marshal(result)
	if err != nil {
		return err
	}
	
	fmt.Print(string(data))
	return nil
}

// parseFloat parses a string to float64
func parseFloat(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}