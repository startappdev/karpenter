# Dynamic Node Provisioning with Karpenter for OCI Flexible Shapes

This guide provides comprehensive instructions for using Karpenter's Dynamic Node Provisioning feature with Oracle Cloud Infrastructure (OCI) flexible shapes in Oracle Kubernetes Engine (OKE).

## Table of Contents
- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [NodePool Configuration](#nodepool-configuration)
- [Provisioning Strategies](#provisioning-strategies)
- [Monitoring and Observability](#monitoring-and-observability)
- [CLI Tools](#cli-tools)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)
- [Examples](#examples)

## Overview

Dynamic Node Provisioning enables Karpenter to automatically provision OCI flexible shapes with precise CPU and memory configurations based on actual pod requirements, eliminating the need to select from predefined instance types. This feature can reduce costs by up to 30% while improving resource utilization.

### Key Benefits
- **Cost Optimization**: Pay only for the exact resources needed
- **Improved Utilization**: Achieve 70-90% resource efficiency vs 40-60% with fixed shapes
- **Flexible Scaling**: Support workloads with diverse resource requirements
- **Automatic Optimization**: Built-in strategies for different workload patterns

### Supported OCI Flexible Shapes
- VM.Standard.E4.Flex
- VM.Standard.E5.Flex
- VM.Standard.A1.Flex (ARM-based)
- VM.Optimized3.Flex
- VM.Standard3.Flex

## Prerequisites

1. **OKE Cluster**: Version 1.28+ with cluster autoscaler disabled
2. **OCI Permissions**: Instance principal or user principal with required policies
3. **Karpenter**: Version with dynamic provisioning support
4. **Kubernetes RBAC**: Appropriate permissions for Karpenter

### Required OCI Policies

For detailed OCI IAM policy setup, see the [OCI IAM Policy Setup Guide](./oci-iam-policy-setup.md).

The minimum required policies are:
```hcl
Allow dynamic-group karpenter-nodes-dg to manage instances in compartment <compartment-name>
Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment <compartment-name>
Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment <compartment-name>
```

**Note**: Some resource types mentioned in older documentation (like `compute-capacity-reports`, `compute-global-price-list`) are not valid OCI resource types.

## Installation

### 1. Install Karpenter with Dynamic Provisioning Support

```bash
# Add Karpenter Helm repository
helm repo add karpenter https://charts.karpenter.sh
helm repo update

# Create namespace
kubectl create namespace karpenter

# Install Karpenter with OCI provider and dynamic provisioning enabled
helm install karpenter karpenter/karpenter \
  --namespace karpenter \
  --set serviceAccount.create=true \
  --set settings.cloudProvider=oci \
  --set settings.featureGates.dynamicProvisioning=true \
  --set controller.env[0].name=ENABLE_OCI_DYNAMIC_SHAPES \
  --set controller.env[0].value="true" \
  --set webhook.enabled=true \
  --version ${KARPENTER_VERSION}
```

### 2. Configure OCI Provider

Create the OCI provider configuration:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: karpenter-oci-config
  namespace: karpenter
data:
  config.yaml: |
    region: us-phoenix-1
    compartmentID: ocid1.compartment.oc1..xxxxxx
    subnetIDs:
      - ocid1.subnet.oc1.phx.xxxxxx
      - ocid1.subnet.oc1.phx.yyyyyy
    imageID: ocid1.image.oc1.phx.xxxxxx  # OKE worker node image
    defaultShapes:
      - VM.Standard.E4.Flex
      - VM.Standard.E5.Flex
    instancePrincipal:
      enabled: true
```

### 3. Apply RBAC Configuration

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: karpenter-dynamic-provisioning
rules:
- apiGroups: ["karpenter.sh"]
  resources: ["nodepools", "nodeclaims"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: [""]
  resources: ["nodes", "pods"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["create", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: karpenter-dynamic-provisioning
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: karpenter-dynamic-provisioning
subjects:
- kind: ServiceAccount
  name: karpenter
  namespace: karpenter
```

## Configuration

### Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `ENABLE_OCI_DYNAMIC_SHAPES` | Enable dynamic provisioning feature | `false` | Yes |
| `OCI_REGION` | OCI region | - | Yes |
| `OCI_COMPARTMENT_ID` | Compartment OCID | - | Yes |
| `OCI_USE_INSTANCE_PRINCIPAL` | Use instance principal auth | `true` | No |
| `KARPENTER_LOG_LEVEL` | Logging level | `info` | No |

### Feature Flags

Dynamic provisioning requires both:
1. Environment variable: `ENABLE_OCI_DYNAMIC_SHAPES=true`
2. NodePool configuration: `dynamicProvisioning.enabled: true`

## NodePool Configuration

### Basic Dynamic Provisioning NodePool

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: dynamic-general-purpose
spec:
  # Standard Karpenter configuration
  template:
    metadata:
      labels:
        karpenter.sh/nodepool: dynamic-general-purpose
    spec:
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["preemptible", "on-demand"]
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
      nodeClassRef:
        group: karpenter.k8s.oci/v1
        kind: OCINodeClass
        name: default
      
      # Taints for specialized workloads (optional)
      taints:
        - key: workload-type
          value: batch
          effect: NoSchedule
  
  # Dynamic provisioning configuration
  dynamicProvisioning:
    enabled: true
    
    # Provisioning strategy
    strategy: cost-optimized  # exact-fit, best-fit, or cost-optimized
    
    # Capacity type distribution
    capacityType:
      preemptible: 80  # Percentage of preemptible instances
      onDemand: 20      # Percentage of on-demand instances
    
    # Resource constraints
    constraints:
      minOCPUs: 1       # Minimum OCPUs (1 OCPU = 2 vCPUs)
      maxOCPUs: 32      # Maximum OCPUs
      minMemoryGB: 1    # Minimum memory in GB
      maxMemoryGB: 256  # Maximum memory in GB
      allowedShapes:    # Allowed OCI flexible shapes
        - VM.Standard.E4.Flex
        - VM.Standard.E5.Flex
    
    # System overhead configuration
    overhead:
      systemReservedCPU: "200m"      # Reserved for system processes
      systemReservedMemory: "1Gi"    # Reserved for system processes
      kubeletReservedCPU: "100m"     # Reserved for kubelet
      kubeletReservedMemory: "500Mi" # Reserved for kubelet
      evictionThresholdMemory: "500Mi" # Memory eviction threshold
    
    # Resource buffers for headroom
    buffers:
      cpuHeadroomPercent: 10    # Extra CPU buffer (percentage)
      memoryHeadroomPercent: 15 # Extra memory buffer (percentage)
  
  # Node pool limits
  limits:
    cpu: "10000"    # Total CPU limit for this NodePool
    memory: "40Ti"  # Total memory limit
  
  # Disruption settings
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
    ttlSecondsAfterEmpty: 30
```

### Advanced Configuration Examples

#### High-Performance Computing NodePool

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: dynamic-hpc
spec:
  template:
    spec:
      requirements:
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["VM.Optimized3.Flex-*"]
      taints:
        - key: workload-type
          value: hpc
          effect: NoSchedule
  
  dynamicProvisioning:
    enabled: true
    strategy: exact-fit  # Minimize resource waste for HPC
    capacityType:
      preemptible: 0
      onDemand: 100      # HPC needs stable instances
    constraints:
      minOCPUs: 8
      maxOCPUs: 64
      minMemoryGB: 64
      maxMemoryGB: 512
      allowedShapes:
        - VM.Optimized3.Flex  # Optimized for compute
    overhead:
      systemReservedCPU: "500m"
      systemReservedMemory: "2Gi"
    buffers:
      cpuHeadroomPercent: 5     # Minimal buffer for HPC
      memoryHeadroomPercent: 5
```

#### Cost-Optimized Batch Processing NodePool

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: dynamic-batch
spec:
  template:
    spec:
      taints:
        - key: workload-type
          value: batch
          effect: NoSchedule
  
  dynamicProvisioning:
    enabled: true
    strategy: cost-optimized
    capacityType:
      preemptible: 100  # All preemptible for maximum savings
      onDemand: 0
    constraints:
      minOCPUs: 1
      maxOCPUs: 16
      minMemoryGB: 1
      maxMemoryGB: 128
      allowedShapes:
        - VM.Standard.E4.Flex
        - VM.Standard.E5.Flex
    buffers:
      cpuHeadroomPercent: 20     # Higher buffer for batch variance
      memoryHeadroomPercent: 25
```

## Provisioning Strategies

### 1. Exact-Fit Strategy
- **Use Case**: Predictable workloads with known resource requirements
- **Behavior**: Provisions exactly what pods request plus overhead
- **Efficiency**: Highest (85-95%)
- **Cost**: Lowest for stable workloads

### 2. Best-Fit Strategy
- **Use Case**: General-purpose workloads
- **Behavior**: Rounds to efficient configurations (powers of 2)
- **Efficiency**: Good (75-85%)
- **Cost**: Balanced

### 3. Cost-Optimized Strategy
- **Use Case**: Batch processing, development environments
- **Behavior**: Consolidates workloads, prefers larger instances
- **Efficiency**: Moderate (65-75%)
- **Cost**: Lowest overall through consolidation

## Monitoring and Observability

### Prometheus Metrics

Key metrics exposed by dynamic provisioning:

```yaml
# Efficiency metrics
karpenter_dynamic_provisioning_shape_efficiency_percentage{nodepool="...", shape="...", resource_type="cpu|memory"}

# Cost metrics
karpenter_dynamic_provisioning_cost_savings_percentage{nodepool="..."}

# Provisioning performance
karpenter_dynamic_provisioning_provisioning_duration_seconds{nodepool="...", strategy="...", result="success|failure"}

# Capacity distribution
karpenter_dynamic_provisioning_capacity_type_distribution{nodepool="...", capacity_type="preemptible|on-demand"}

# Success rate
karpenter_dynamic_provisioning_provisioning_success_rate{nodepool="..."}
```

### Grafana Dashboard

Import the dynamic provisioning dashboard:

```json
{
  "dashboard": {
    "title": "Karpenter Dynamic Provisioning - OCI",
    "panels": [
      {
        "title": "Resource Efficiency",
        "targets": [
          {
            "expr": "avg(karpenter_dynamic_provisioning_shape_efficiency_percentage) by (nodepool, resource_type)"
          }
        ]
      },
      {
        "title": "Cost Savings",
        "targets": [
          {
            "expr": "sum(karpenter_dynamic_provisioning_cost_savings_percentage) by (nodepool)"
          }
        ]
      },
      {
        "title": "Provisioning Success Rate",
        "targets": [
          {
            "expr": "karpenter_dynamic_provisioning_provisioning_success_rate"
          }
        ]
      }
    ]
  }
}
```

### Events

Monitor Kubernetes events for provisioning activities:

```bash
kubectl get events -n karpenter --field-selector reason=DynamicShapeProvisioned
kubectl get events -n karpenter --field-selector reason=BinPackingCompleted
kubectl get events -n karpenter --field-selector reason=CostOptimizationApplied
```

## CLI Tools

### Analyze Dynamic Provisioning

```bash
# Install Karpenter CLI plugin
kubectl karpenter plugin install

# Analyze efficiency
kubectl karpenter analyze-dynamic-provisioning \
  --nodepool dynamic-general-purpose \
  --since 24h \
  --efficiency-report

# Generate cost report
kubectl karpenter analyze-dynamic-provisioning \
  --cost-report \
  --output table

# Export metrics as JSON
kubectl karpenter analyze-dynamic-provisioning \
  --since 7d \
  --output json > provisioning-report.json
```

### Sample Output

```
DYNAMIC PROVISIONING SUMMARY
================================================================================
Time Range:     2024-01-15 to 2024-01-16
NodePools:      3
NodeClaims:     45
Avg CPU Efficiency:     78.5%
Avg Memory Efficiency:  82.3%
Preemptible:    79.2%
Total Cost:     $127.43
Est. Savings:   $42.89 (33.7%)

NODEPOOL               STRATEGY        NODES   AVG CPU EFF   AVG MEM EFF   TOTAL COST
--------------------------------------------------------------------------------
dynamic-general-purpose cost-optimized  25      76.2%        80.1%         $67.21
dynamic-hpc            exact-fit       10      89.3%        91.2%         $45.32
dynamic-batch          cost-optimized  10      71.8%        77.5%         $14.90
```

## Best Practices

### 1. Shape Selection
- Use E4.Flex for general workloads (best price/performance)
- Use E5.Flex for larger memory requirements
- Use Optimized3.Flex for compute-intensive workloads
- Consider A1.Flex for ARM-compatible workloads

### 2. Capacity Type Distribution
- **Production**: 70-80% on-demand, 20-30% preemptible
- **Development**: 20-30% on-demand, 70-80% preemptible  
- **Batch/CI**: 0-10% on-demand, 90-100% preemptible

### 3. Buffer Configuration
- **Stable workloads**: 5-10% CPU, 10-15% memory
- **Variable workloads**: 15-20% CPU, 20-25% memory
- **Batch processing**: 20-30% CPU, 25-35% memory

### 4. Constraint Settings
- Set realistic minimums to avoid tiny instances
- Set maximums based on largest expected workload
- Allow multiple shapes for better availability

### 5. Monitoring
- Alert on efficiency below 60%
- Track cost savings weekly
- Monitor provisioning failures
- Review shape distribution monthly

## Troubleshooting

### Common Issues

#### 1. Dynamic Provisioning Not Working

```bash
# Check feature flag
kubectl -n karpenter get deployment karpenter -o yaml | grep ENABLE_OCI_DYNAMIC_SHAPES

# Check NodePool configuration
kubectl get nodepool <name> -o yaml | grep -A20 dynamicProvisioning

# Check webhook
kubectl get validatingwebhookconfigurations | grep karpenter
```

#### 2. Shape Constraints Violations

```bash
# Check events
kubectl get events --field-selector reason=ConstraintViolation

# Review pod requirements
kubectl describe pod <pod-name> | grep -A10 Resources

# Adjust constraints
kubectl edit nodepool <name>
```

#### 3. Low Efficiency

```bash
# Analyze shape distribution
kubectl karpenter analyze-dynamic-provisioning --efficiency-report

# Check overhead configuration
kubectl get nodepool <name> -o yaml | grep -A10 overhead

# Review buffer settings
kubectl get nodepool <name> -o yaml | grep -A5 buffers
```

### Debug Logging

Enable debug logging for detailed provisioning information:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: karpenter-config
  namespace: karpenter
data:
  config.yaml: |
    logLevel: debug
    logEncoding: json
    dynamicProvisioning:
      logProvisioningDecisions: true
      logBinPackingDetails: true
      logShapeCalculations: true
```

## Examples

### Example 1: Web Application with Mixed Workloads

```yaml
# NodePool for web tier
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: web-tier-dynamic
spec:
  template:
    spec:
      requirements:
        - key: workload
          operator: In
          values: ["web"]
  dynamicProvisioning:
    enabled: true
    strategy: best-fit
    capacityType:
      preemptible: 50
      onDemand: 50
    constraints:
      minOCPUs: 1
      maxOCPUs: 8
      minMemoryGB: 2
      maxMemoryGB: 64
      allowedShapes:
        - VM.Standard.E4.Flex
---
# Deployment using dynamic provisioning
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
spec:
  replicas: 10
  template:
    spec:
      nodeSelector:
        workload: web
      containers:
      - name: app
        image: myapp:latest
        resources:
          requests:
            cpu: "1.5"      # Will provision 1 OCPU (2 vCPUs)
            memory: "6Gi"   # Will provision 8GB with overhead
          limits:
            cpu: "2"
            memory: "8Gi"
```

### Example 2: Machine Learning Training

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: ml-training-dynamic
spec:
  template:
    spec:
      requirements:
        - key: workload
          operator: In
          values: ["ml-training"]
  dynamicProvisioning:
    enabled: true
    strategy: exact-fit
    capacityType:
      preemptible: 100  # ML training can handle interruptions
      onDemand: 0
    constraints:
      minOCPUs: 4
      maxOCPUs: 32
      minMemoryGB: 32
      maxMemoryGB: 256
      allowedShapes:
        - VM.Standard.E5.Flex  # Better memory bandwidth
    overhead:
      systemReservedCPU: "1"
      systemReservedMemory: "4Gi"
    buffers:
      cpuHeadroomPercent: 5
      memoryHeadroomPercent: 10
---
apiVersion: batch/v1
kind: Job
metadata:
  name: ml-training-job
spec:
  template:
    spec:
      nodeSelector:
        workload: ml-training
      containers:
      - name: training
        image: ml-training:latest
        resources:
          requests:
            cpu: "15"      # Will provision 8 OCPUs
            memory: "120Gi" # Will provision 128GB with overhead
```

### Example 3: Multi-Tenant SaaS Platform

```yaml
# Small tenant workloads
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: tenant-small-dynamic
spec:
  template:
    spec:
      requirements:
        - key: tenant-size
          operator: In
          values: ["small"]
  dynamicProvisioning:
    enabled: true
    strategy: cost-optimized  # Pack small tenants together
    capacityType:
      preemptible: 70
      onDemand: 30
    constraints:
      minOCPUs: 1
      maxOCPUs: 4
      minMemoryGB: 2
      maxMemoryGB: 32
      allowedShapes:
        - VM.Standard.E4.Flex
---
# Large tenant workloads
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: tenant-large-dynamic
spec:
  template:
    spec:
      requirements:
        - key: tenant-size
          operator: In
          values: ["large"]
  dynamicProvisioning:
    enabled: true
    strategy: exact-fit  # Precise resources for large tenants
    capacityType:
      preemptible: 20
      onDemand: 80    # Large tenants need stability
    constraints:
      minOCPUs: 4
      maxOCPUs: 32
      minMemoryGB: 16
      maxMemoryGB: 256
      allowedShapes:
        - VM.Standard.E5.Flex
```

## Migration Guide

### Migrating from Fixed Instance Types

1. **Analyze current usage**:
   ```bash
   kubectl top nodes
   kubectl describe nodes | grep -E "Name:|cpu:|memory:"
   ```

2. **Create dynamic NodePool**:
   - Set constraints based on largest workload
   - Start with best-fit strategy
   - Use 50/50 capacity type split initially

3. **Gradual migration**:
   ```bash
   # Cordon old nodes
   kubectl cordon -l karpenter.sh/nodepool=old-fixed-pool
   
   # Delete old NodePool
   kubectl delete nodepool old-fixed-pool
   ```

4. **Monitor and optimize**:
   - Track efficiency metrics
   - Adjust buffers based on utilization
   - Fine-tune strategies per workload type

## Conclusion

Dynamic Node Provisioning with Karpenter and OCI flexible shapes provides a powerful way to optimize Kubernetes infrastructure costs while improving resource utilization. By following this guide and best practices, you can achieve significant cost savings and operational efficiency in your OKE clusters.

For additional support and updates, refer to:
- [Karpenter Documentation](https://karpenter.sh)
- [OCI Documentation](https://docs.oracle.com/en-us/iaas/Content/Compute/References/computeshapes.htm)
- [OKE Best Practices](https://docs.oracle.com/en-us/iaas/Content/ContEng/Concepts/contengbestpractices.htm)