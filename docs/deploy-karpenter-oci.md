# Deploying Karpenter with OCI Provider

This guide walks through deploying and testing Karpenter with the OCI (Oracle Cloud Infrastructure) provider for flexible shape node provisioning.

## Prerequisites

1. **Oracle Kubernetes Engine (OKE) cluster** with:
   - At least one node with label `node_pool=generic`
   - Toleration for `CriticalAddonsOnly` taint

2. **FluxCD** installed and configured in your cluster

3. **OCI Configuration** including:
   - Compartment ID
   - Subnet IDs
   - Instance Principal or User Principal authentication configured
   - Appropriate IAM policies for creating/managing compute instances

## Step 1: Update FluxCD Repository

Update your FluxCD repository to pull the latest Helm chart (version 0.1.9):

```yaml
# In your FluxCD repository, update the HelmRelease
apiVersion: helm.toolkit.fluxcd.io/v2beta2
kind: HelmRelease
metadata:
  name: karpenter
  namespace: flux-system
spec:
  interval: 1m
  targetNamespace: karpenter
  install:
    createNamespace: true
  chart:
    spec:
      chart: ./helm/karpenter-oci
      sourceRef:
        kind: GitRepository
        name: karpenter
      version: "0.1.9"
  values:
    image:
      repository: ghcr.io/startappdev/karpenter
      tag: "start-io-1da0394"
    
    controller:
      env:
        - name: OCI_REGION
          value: "us-ashburn-1"  # Your OCI region
        - name: OCI_COMPARTMENT_ID
          value: "ocid1.compartment.oc1..."  # Your compartment
        - name: OCI_CLUSTER_ID
          value: "ocid1.cluster.oc1..."  # Your OKE cluster
        - name: OCI_SUBNET_IDS
          value: "ocid1.subnet.oc1...,ocid1.subnet.oc1..."  # Comma-separated subnet IDs
        - name: OCI_IMAGE_ID
          value: "ocid1.image.oc1..."  # OKE-compatible node image
        - name: CLUSTER_NAME
          value: "your-cluster-name"
        - name: OCI_USE_INSTANCE_PRINCIPAL
          value: "true"  # Or false if using user principal
        - name: ENABLE_OCI_DYNAMIC_SHAPES
          value: "true"
```

## Step 2: Trigger FluxCD Reconciliation

```bash
# Force reconciliation of the GitRepository
flux reconcile source git karpenter -n flux-system

# Force reconciliation of the HelmRelease
flux reconcile helmrelease karpenter -n flux-system
```

## Step 3: Verify Karpenter Deployment

```bash
# Check if the pod is running
kubectl get pods -n karpenter -l app.kubernetes.io/name=karpenter-oci

# Check pod logs for any errors
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci -f

# Expected log output:
# Karpenter OCI Version: start-io-1da0394
# OCI Provider initialized successfully
# Starting Karpenter operator...
```

## Step 4: Deploy OCI NodePool with Flexible Shapes

Create the NodePool for flexible shape provisioning:

```bash
kubectl apply -f - <<EOF
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: oci-flexible-pool
  namespace: karpenter
spec:
  # Dynamic provisioning configuration for OCI flexible shapes
  dynamicProvisioning:
    enabled: true
    constraints:
      minOCPUs: 1
      maxOCPUs: 32
      minMemoryGB: 8
      maxMemoryGB: 256
      shapes:
        - "VM.Standard.E4.Flex"
        - "VM.Standard.E5.Flex"
        - "VM.Standard.A1.Flex"
    overhead:
      systemReservedCPU: "100m"
      systemReservedMemory: "500Mi"
      kubernetesReservedCPU: "100m"
      kubernetesReservedMemory: "500Mi"
    buffers:
      cpuHeadroom: 10        # 10% CPU buffer
      memoryHeadroom: 10     # 10% memory buffer
      packingEfficiency: 85  # Target 85% bin packing efficiency
    strategy: CostOptimized
    capacityType:
      onDemand: 70    # 70% on-demand instances
      preemptible: 30 # 30% preemptible instances
  
  # Template for nodes
  template:
    metadata:
      labels:
        karpenter.sh/nodepool: oci-flexible-pool
        node-type: flexible
      annotations:
        karpenter.sh/managed-by: oci-provider
    spec:
      requirements:
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
        - key: kubernetes.io/os
          operator: In
          values: ["linux"]
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["on-demand", "preemptible"]
        - key: node.kubernetes.io/instance-type
          operator: Exists
      nodeClassRef:
        apiVersion: karpenter.sh/v1
        kind: NodeClass
        name: default
      taints:
        - key: karpenter.sh/new-node
          value: "true"
          effect: NoSchedule
      startupTaints:
        - key: karpenter.sh/node-initializing
          value: "true"
          effect: NoSchedule
      expireAfter: 24h
  
  # Disruption settings
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
    consolidateAfter: 30s
    budgets:
      - nodes: "20%"
  
  # Resource limits
  limits:
    cpu: "1000"
    memory: "1000Gi"
  
  # Weight for scheduling preference
  weight: 100
EOF

# Verify NodePool is created
kubectl get nodepool -n karpenter
kubectl describe nodepool oci-flexible-pool -n karpenter
```

## Step 5: Deploy Test Workload

Deploy a test application to trigger node provisioning:

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: karpenter-test
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inflate
  namespace: karpenter-test
spec:
  replicas: 0
  selector:
    matchLabels:
      app: inflate
  template:
    metadata:
      labels:
        app: inflate
    spec:
      terminationGracePeriodSeconds: 0
      tolerations:
        - key: karpenter.sh/new-node
          operator: Exists
          effect: NoSchedule
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app: inflate
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchExpressions:
                  - key: app
                    operator: In
                    values:
                      - inflate
              topologyKey: kubernetes.io/hostname
      containers:
        - name: inflate
          image: public.ecr.aws/eks-distro/kubernetes/pause:3.9
          resources:
            requests:
              cpu: "2"
              memory: "8Gi"
EOF
```

## Step 6: Trigger Autoscaling

Scale the deployment to trigger Karpenter provisioning:

```bash
# Scale to 4 replicas (should provision 4 nodes with flexible shapes)
kubectl scale deployment inflate -n karpenter-test --replicas=4

# Watch Karpenter logs
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci -f

# Expected logs:
# INFO  controller.provisioning  Found 4 unschedulable pods
# INFO  controller.provisioning  Computing flexible shape for pod requirements
# INFO  controller.provisioning  Selected shape: VM.Standard.E4.Flex with 4 OCPUs, 32GB memory
# INFO  controller.provisioning  Creating node claim with flexible shape
# INFO  controller.nodeclaim     Launched instance: oci://ocid1.instance.oc1...
```

## Step 7: Monitor Node Creation

```bash
# Watch for new nodes
watch kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool

# Check NodeClaims
kubectl get nodeclaims -A

# Check pod scheduling
kubectl get pods -n karpenter-test -o wide

# View node details (once created)
kubectl describe node -l karpenter.sh/nodepool=oci-flexible-pool
```

## Step 8: Verify Flexible Shape Configuration

Once nodes are created, verify they have the correct flexible shape configuration:

```bash
# Check node capacity
kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool -o json | \
  jq '.items[] | {name: .metadata.name, cpu: .status.capacity.cpu, memory: .status.capacity.memory}'

# Check node labels for shape information
kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool -o json | \
  jq '.items[] | {name: .metadata.name, shape: .metadata.labels["oci.oraclecloud.com/shape"], ocpus: .metadata.labels["oci.oraclecloud.com/ocpus"]}'
```

## Step 9: Test Consolidation

Test Karpenter's ability to consolidate nodes:

```bash
# Scale down to 2 replicas
kubectl scale deployment inflate -n karpenter-test --replicas=2

# Watch nodes being consolidated (after 30s consolidateAfter period)
watch kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool

# Karpenter should:
# 1. Identify underutilized nodes
# 2. Attempt to reschedule pods to fewer nodes
# 3. Terminate empty nodes
```

## Step 10: Test Different Shape Strategies

Test different provisioning strategies:

```bash
# Update NodePool to use BalancedCompute strategy
kubectl patch nodepool oci-flexible-pool -n karpenter --type merge -p '
spec:
  dynamicProvisioning:
    strategy: BalancedCompute
'

# Create workload with different requirements
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cpu-intensive
  namespace: karpenter-test
spec:
  replicas: 2
  selector:
    matchLabels:
      app: cpu-intensive
  template:
    metadata:
      labels:
        app: cpu-intensive
    spec:
      containers:
        - name: stress
          image: progrium/stress
          args:
            - "--cpu"
            - "4"
          resources:
            requests:
              cpu: "4"
              memory: "2Gi"
EOF
```

## Troubleshooting

### If nodes aren't being created:

1. **Check Karpenter logs**:
   ```bash
   kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci --tail=100
   ```

2. **Verify OCI credentials**:
   ```bash
   kubectl describe secret oci-config -n karpenter
   ```

3. **Check NodePool status**:
   ```bash
   kubectl get nodepool oci-flexible-pool -n karpenter -o yaml
   ```

4. **Verify webhook configuration**:
   ```bash
   kubectl get validatingwebhookconfigurations | grep karpenter
   kubectl get mutatingwebhookconfigurations | grep karpenter
   ```

### Common Issues:

1. **"No shape available for requirements"**:
   - Check if the requested resources fit within the shape constraints
   - Verify subnet availability in OCI

2. **"Failed to create instance"**:
   - Check OCI API errors in Karpenter logs
   - Verify IAM policies allow instance creation
   - Check service limits in OCI console

3. **Pods remain Pending**:
   - Verify tolerations match node taints
   - Check if resource requests exceed shape limits

## Cleanup

```bash
# Delete test resources
kubectl delete namespace karpenter-test
kubectl delete nodepool oci-flexible-pool -n karpenter

# Karpenter will automatically clean up any nodes it created
```

## Success Metrics

✅ Karpenter pod running without errors  
✅ NodePool recognized and ready  
✅ Pods trigger flexible shape node provisioning  
✅ Nodes created with appropriate OCPU/memory configuration  
✅ Pods scheduled successfully on new nodes  
✅ Consolidation works when scaling down  
✅ Different shape strategies produce expected results  

Once all these criteria are met, Karpenter OCI provider with flexible shape support is working correctly!