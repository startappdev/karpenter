# Migrating from Cluster Autoscaler to Karpenter on OKE

## Overview

This guide walks you through safely removing the OKE cluster autoscaler and replacing it with Karpenter. Running both autoscalers simultaneously will cause conflicts, as both will try to manage node scaling decisions.

## Prerequisites

- Access to OKE cluster with cluster autoscaler enabled
- kubectl configured to access your cluster
- Helm 3.x installed
- OCI CLI configured (for updating node pool settings)

## Step 1: Prepare for Migration

### 1.1 Document Current Autoscaler Configuration

First, capture your current autoscaler settings:

```bash
# Check current cluster autoscaler deployment
kubectl -n kube-system get deployment cluster-autoscaler -o yaml > cluster-autoscaler-backup.yaml

# Check ConfigMap if exists
kubectl -n kube-system get configmap cluster-autoscaler-status -o yaml > cluster-autoscaler-config-backup.yaml

# List current node pools and their autoscaling settings
oci ce node-pool list --compartment-id <compartment-ocid> --cluster-id <cluster-ocid> \
  --query "data[*].{name:name, id:id, \"node-config-details\":\"node-config-details\"}"
```

### 1.2 Check Current Node Distribution

```bash
# Get current nodes and their pools
kubectl get nodes -L oke.oraclecloud.com/node-pool

# Check current workload distribution
kubectl get pods --all-namespaces -o wide | grep -v "kube-system\|kube-public" | awk '{print $8}' | sort | uniq -c
```

## Step 2: Disable Autoscaling on OKE Node Pools

Before removing the cluster autoscaler, disable autoscaling on all node pools:

```bash
# For each node pool, disable autoscaling
oci ce node-pool update \
  --node-pool-id <node-pool-ocid> \
  --node-config-details '{
    "size": <current-size>,
    "isAutoScalingEnabled": false
  }'

# Verify autoscaling is disabled
oci ce node-pool get --node-pool-id <node-pool-ocid> \
  --query 'data."node-config-details"."is-auto-scaling-enabled"'
```

## Step 3: Remove Cluster Autoscaler

### 3.1 Scale Down Cluster Autoscaler

First, scale down the autoscaler to prevent it from making changes:

```bash
# Scale down the deployment
kubectl -n kube-system scale deployment cluster-autoscaler --replicas=0

# Verify it's scaled down
kubectl -n kube-system get deployment cluster-autoscaler
```

### 3.2 Remove Cluster Autoscaler Resources

```bash
# Delete the deployment
kubectl -n kube-system delete deployment cluster-autoscaler

# Delete associated ConfigMaps
kubectl -n kube-system delete configmap cluster-autoscaler-status --ignore-not-found

# Delete ServiceAccount if dedicated
kubectl -n kube-system delete serviceaccount cluster-autoscaler --ignore-not-found

# Delete ClusterRole and ClusterRoleBinding
kubectl delete clusterrole cluster-autoscaler --ignore-not-found
kubectl delete clusterrolebinding cluster-autoscaler --ignore-not-found

# Delete any remaining resources with cluster-autoscaler labels
kubectl -n kube-system delete all -l app=cluster-autoscaler
```

### 3.3 Clean Up Node Labels

Remove any cluster autoscaler specific labels/annotations:

```bash
# Remove cluster autoscaler annotations from nodes
kubectl get nodes -o name | while read node; do
  kubectl annotate $node cluster-autoscaler.kubernetes.io/scale-down-disabled- || true
  kubectl label $node cluster-autoscaler.kubernetes.io/safe-to-evict- || true
done
```

## Step 4: Install Karpenter

### 4.1 Create Karpenter Namespace and Service Account

```bash
kubectl create namespace karpenter

# Create service account
kubectl create serviceaccount karpenter -n karpenter
```

### 4.2 Install Karpenter with Dynamic Provisioning

```bash
# Add Karpenter Helm repository
helm repo add karpenter https://charts.karpenter.sh
helm repo update

# Install Karpenter
helm install karpenter karpenter/karpenter \
  --namespace karpenter \
  --set serviceAccount.create=false \
  --set serviceAccount.name=karpenter \
  --set settings.cloudProvider=oci \
  --set settings.featureGates.dynamicProvisioning=true \
  --set controller.env[0].name=ENABLE_OCI_DYNAMIC_SHAPES \
  --set controller.env[0].value="true" \
  --set webhook.enabled=true \
  --wait
```

### 4.3 Configure OCI Provider

Create the OCI configuration:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: karpenter-oci-config
  namespace: karpenter
data:
  config.yaml: |
    region: <your-region>
    compartmentID: <your-compartment-ocid>
    clusterID: <your-cluster-ocid>
    subnetIDs:
      - <worker-subnet-ocid-1>
      - <worker-subnet-ocid-2>
    imageID: <oke-node-image-ocid>
    instancePrincipal:
      enabled: true
    defaultShapes:
      - VM.Standard.E4.Flex
      - VM.Standard.E5.Flex
```

## Step 5: Create Initial NodePool

Create a NodePool to replace your existing autoscaled nodes:

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: default-nodepool
spec:
  # Replace the disruption settings
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
    ttlSecondsAfterEmpty: 30
    
  # Template for nodes
  template:
    metadata:
      labels:
        karpenter.sh/nodepool: default-nodepool
    spec:
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["on-demand", "preemptible"]
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
      nodeClassRef:
        group: karpenter.k8s.oci/v1
        kind: OCINodeClass
        name: default
      # Start without taints to accept existing workloads
      taints: []
      
  # Dynamic provisioning configuration
  dynamicProvisioning:
    enabled: true
    strategy: best-fit
    capacityType:
      preemptible: 50
      onDemand: 50
    constraints:
      minOCPUs: 1
      maxOCPUs: 16
      minMemoryGB: 2
      maxMemoryGB: 128
      allowedShapes:
        - VM.Standard.E4.Flex
        - VM.Standard.E5.Flex
    overhead:
      systemReservedCPU: "200m"
      systemReservedMemory: "1Gi"
    buffers:
      cpuHeadroomPercent: 10
      memoryHeadroomPercent: 15
      
  # Set appropriate limits
  limits:
    cpu: "1000"
    memory: "4Ti"
```

## Step 6: Migrate Workloads

### 6.1 Verify Karpenter is Working

```bash
# Check Karpenter pods
kubectl -n karpenter get pods

# Check NodePool
kubectl get nodepool

# Watch Karpenter logs
kubectl -n karpenter logs -l app.kubernetes.io/name=karpenter -f
```

### 6.2 Test with a Sample Workload

Deploy a test pod to verify Karpenter provisions nodes:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: karpenter-test
spec:
  containers:
  - name: test
    image: nginx
    resources:
      requests:
        cpu: "1"
        memory: "2Gi"
```

Monitor node creation:

```bash
kubectl get nodes -w
kubectl get events -w | grep -i "karpenter\|provision"
```

### 6.3 Gradual Migration

Migrate existing workloads from old nodes to Karpenter-managed nodes:

```bash
# Cordon old nodes one by one
OLD_NODES=$(kubectl get nodes -l 'oke.oraclecloud.com/node-pool' -o name)

for node in $OLD_NODES; do
  # Cordon the node
  kubectl cordon $node
  
  # Wait for Karpenter to provision replacement capacity
  sleep 60
  
  # Drain the node
  kubectl drain $node --ignore-daemonsets --delete-emptydir-data
  
  # Monitor new node provisioning
  kubectl get nodes -w
done
```

## Step 7: Clean Up Old Infrastructure

### 7.1 Delete Old Nodes

Once all workloads are migrated:

```bash
# Delete old nodes from OKE node pools
for node in $OLD_NODES; do
  kubectl delete $node
done

# Or terminate through OCI Console/CLI
oci ce node-pool update \
  --node-pool-id <node-pool-ocid> \
  --node-config-details '{"size": 0}'
```

### 7.2 Update Node Pool Configuration

Keep minimal node pools for system components if needed:

```bash
# Update node pool to minimal size for system components
oci ce node-pool update \
  --node-pool-id <system-node-pool-ocid> \
  --node-config-details '{
    "size": 1,
    "isAutoScalingEnabled": false
  }'
```

## Step 8: Post-Migration Validation

### 8.1 Verify All Workloads Running

```bash
# Check all pods are running
kubectl get pods --all-namespaces | grep -v "Running\|Completed" | grep -v "NAMESPACE"

# Check node utilization
kubectl top nodes

# Check Karpenter metrics
kubectl -n karpenter exec -it deployment/karpenter -- curl http://localhost:8080/metrics | grep karpenter_
```

### 8.2 Test Scaling

```bash
# Deploy a scaling test
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: scale-test
spec:
  replicas: 10
  selector:
    matchLabels:
      app: scale-test
  template:
    metadata:
      labels:
        app: scale-test
    spec:
      containers:
      - name: test
        image: nginx
        resources:
          requests:
            cpu: "500m"
            memory: "1Gi"
EOF

# Watch nodes scale up
kubectl get nodes -w

# Scale down and watch consolidation
kubectl scale deployment scale-test --replicas=1
kubectl get nodes -w
```

## Rollback Plan

If issues arise, you can rollback:

1. **Re-enable OKE autoscaling**:
   ```bash
   oci ce node-pool update \
     --node-pool-id <node-pool-ocid> \
     --node-config-details '{
       "size": <desired-size>,
       "isAutoScalingEnabled": true,
       "minNodes": <min>,
       "maxNodes": <max>
     }'
   ```

2. **Reinstall cluster autoscaler**:
   ```bash
   kubectl apply -f cluster-autoscaler-backup.yaml
   ```

3. **Remove Karpenter**:
   ```bash
   helm uninstall karpenter -n karpenter
   kubectl delete nodepool --all
   ```

## Best Practices

1. **Timing**: Perform migration during low-traffic periods
2. **Monitoring**: Set up monitoring before migration
3. **Gradual approach**: Migrate node by node, not all at once
4. **Backup**: Keep backups of all configurations
5. **Communication**: Notify teams about the migration
6. **Testing**: Test in non-production environment first

## Troubleshooting

### Karpenter Not Provisioning Nodes

```bash
# Check Karpenter logs
kubectl -n karpenter logs -l app.kubernetes.io/name=karpenter --tail=100

# Check events
kubectl get events --field-selector involvedObject.kind=NodePool

# Verify IAM/Instance Principal permissions
kubectl -n karpenter describe pod -l app.kubernetes.io/name=karpenter
```

### Pods Pending After Migration

```bash
# Check pod events
kubectl describe pod <pending-pod>

# Check NodePool constraints
kubectl get nodepool -o yaml

# Verify no conflicting taints/tolerations
kubectl get nodes -o json | jq '.items[].spec.taints'
```

### Cost Tracking

After migration, monitor cost improvements:

```bash
# Use Karpenter CLI to analyze
kubectl karpenter analyze-dynamic-provisioning \
  --since 24h \
  --cost-report
```

## Summary

Migrating from cluster autoscaler to Karpenter requires careful planning but provides significant benefits:
- Better resource utilization with dynamic provisioning
- Cost savings through flexible shapes
- Faster scaling decisions
- More granular control over node provisioning

The key is to migrate gradually and monitor each step to ensure workload stability.