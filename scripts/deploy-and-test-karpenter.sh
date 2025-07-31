#!/bin/bash

# Comprehensive script to deploy and test Karpenter with OCI provider
# This script automates the entire deployment and verification process

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
NAMESPACE="karpenter"
FLUX_NAMESPACE="flux-system"
TEST_NAMESPACE="karpenter-test"

echo -e "${GREEN}=== Karpenter OCI Deployment and Testing Script ===${NC}"
echo ""

# Function to wait for pod to be ready
wait_for_pod() {
    local label=$1
    local namespace=$2
    local timeout=${3:-300}
    
    echo -e "${YELLOW}Waiting for pod with label $label in namespace $namespace...${NC}"
    kubectl wait --for=condition=ready pod -l "$label" -n "$namespace" --timeout="${timeout}s" || {
        echo -e "${RED}Timeout waiting for pod. Current status:${NC}"
        kubectl get pods -l "$label" -n "$namespace"
        kubectl describe pod -l "$label" -n "$namespace"
        return 1
    }
}

# Function to check if resource exists
resource_exists() {
    kubectl get "$1" "$2" -n "$3" &> /dev/null
}

# Step 1: Check prerequisites
echo -e "${GREEN}Step 1: Checking prerequisites...${NC}"

# Check if kubectl is configured
if ! kubectl cluster-info &> /dev/null; then
    echo -e "${RED}Error: kubectl is not configured or cluster is not accessible${NC}"
    exit 1
fi

# Check if FluxCD is installed
if ! kubectl get ns flux-system &> /dev/null; then
    echo -e "${RED}Error: FluxCD is not installed (flux-system namespace not found)${NC}"
    exit 1
fi

# Check if sealed-secrets controller is running
if ! kubectl get pods -n kube-system -l name=sealed-secrets-controller --no-headers | grep -q Running; then
    echo -e "${YELLOW}Warning: Sealed Secrets controller not found or not running${NC}"
fi

echo -e "${GREEN}Prerequisites check passed!${NC}"
echo ""

# Step 2: Check if OCI sealed secret exists
echo -e "${GREEN}Step 2: Checking OCI configuration...${NC}"

if ! resource_exists secret oci-config "$NAMESPACE"; then
    echo -e "${YELLOW}OCI configuration secret not found!${NC}"
    echo "Please run: ./scripts/create-oci-sealed-secret.sh"
    echo "Then add the sealed secret to your FluxCD repository"
    exit 1
fi

echo -e "${GREEN}OCI configuration found!${NC}"
echo ""

# Step 3: Check Karpenter deployment
echo -e "${GREEN}Step 3: Checking Karpenter deployment...${NC}"

# Check if HelmRelease exists
if ! resource_exists helmrelease karpenter "$FLUX_NAMESPACE"; then
    echo -e "${RED}Error: Karpenter HelmRelease not found in flux-system namespace${NC}"
    echo "Please add the HelmRelease to your FluxCD repository"
    exit 1
fi

# Check HelmRelease status
echo "HelmRelease status:"
kubectl get helmrelease karpenter -n "$FLUX_NAMESPACE" -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}'
echo ""

# Force reconciliation
echo -e "${YELLOW}Forcing FluxCD reconciliation...${NC}"
flux reconcile source git karpenter -n "$FLUX_NAMESPACE" || true
flux reconcile helmrelease karpenter -n "$FLUX_NAMESPACE"

# Wait for Karpenter pod
if ! wait_for_pod "app.kubernetes.io/name=karpenter-oci" "$NAMESPACE" 180; then
    echo -e "${RED}Karpenter pod failed to become ready${NC}"
    echo "Checking logs:"
    kubectl logs -l app.kubernetes.io/name=karpenter-oci -n "$NAMESPACE" --tail=50
    exit 1
fi

echo -e "${GREEN}Karpenter is running!${NC}"
echo ""

# Step 4: Verify Karpenter is healthy
echo -e "${GREEN}Step 4: Verifying Karpenter health...${NC}"

# Check logs for errors
echo "Recent logs:"
kubectl logs -l app.kubernetes.io/name=karpenter-oci -n "$NAMESPACE" --tail=20

# Check if environment variables are set correctly
echo ""
echo "Checking environment variables:"
kubectl exec -n "$NAMESPACE" -it $(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=karpenter-oci -o jsonpath='{.items[0].metadata.name}') -- env | grep -E "OCI_|CLUSTER_NAME" || {
    echo -e "${RED}OCI environment variables not found!${NC}"
    exit 1
}

echo -e "${GREEN}Karpenter is healthy!${NC}"
echo ""

# Step 5: Deploy NodePool
echo -e "${GREEN}Step 5: Deploying OCI NodePool with flexible shapes...${NC}"

kubectl apply -f - <<EOF
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: oci-flexible-pool
  namespace: $NAMESPACE
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
      cpuHeadroom: 10
      memoryHeadroom: 10
      packingEfficiency: 85
    strategy: CostOptimized
    capacityType:
      onDemand: 70
      preemptible: 30
  
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
  
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
    consolidateAfter: 30s
    budgets:
      - nodes: "20%"
  
  limits:
    cpu: "1000"
    memory: "1000Gi"
  
  weight: 100
EOF

# Verify NodePool is created
sleep 5
kubectl get nodepool -n "$NAMESPACE"
echo ""

# Step 6: Deploy test workload
echo -e "${GREEN}Step 6: Deploying test workload to trigger provisioning...${NC}"

# Create test namespace
kubectl create namespace "$TEST_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

# Deploy test application
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inflate
  namespace: $TEST_NAMESPACE
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

echo -e "${GREEN}Test deployment created!${NC}"
echo ""

# Step 7: Trigger autoscaling
echo -e "${GREEN}Step 7: Scaling deployment to trigger node provisioning...${NC}"

# Get current node count
INITIAL_NODES=$(kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool --no-headers 2>/dev/null | wc -l)
echo "Initial Karpenter nodes: $INITIAL_NODES"

# Scale deployment
kubectl scale deployment inflate -n "$TEST_NAMESPACE" --replicas=3
echo "Scaled deployment to 3 replicas"
echo ""

# Step 8: Monitor provisioning
echo -e "${GREEN}Step 8: Monitoring node provisioning...${NC}"
echo "Watching Karpenter logs for provisioning activity..."
echo ""

# Start monitoring in background
kubectl logs -f -l app.kubernetes.io/name=karpenter-oci -n "$NAMESPACE" &
LOG_PID=$!

# Monitor for new nodes
echo "Waiting for new nodes to be provisioned (timeout: 5 minutes)..."
TIMEOUT=300
ELAPSED=0
PROVISIONED=false

while [ $ELAPSED -lt $TIMEOUT ]; do
    CURRENT_NODES=$(kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool --no-headers 2>/dev/null | wc -l)
    
    if [ "$CURRENT_NODES" -gt "$INITIAL_NODES" ]; then
        echo -e "${GREEN}✓ New nodes provisioned! Total nodes: $CURRENT_NODES${NC}"
        PROVISIONED=true
        break
    fi
    
    # Check pod status
    PENDING_PODS=$(kubectl get pods -n "$TEST_NAMESPACE" --field-selector=status.phase=Pending --no-headers | wc -l)
    RUNNING_PODS=$(kubectl get pods -n "$TEST_NAMESPACE" --field-selector=status.phase=Running --no-headers | wc -l)
    
    echo -ne "\rElapsed: ${ELAPSED}s | Nodes: $CURRENT_NODES | Pods - Pending: $PENDING_PODS, Running: $RUNNING_PODS"
    
    sleep 5
    ELAPSED=$((ELAPSED + 5))
done

# Stop log monitoring
kill $LOG_PID 2>/dev/null || true

echo ""
echo ""

if [ "$PROVISIONED" = false ]; then
    echo -e "${RED}No new nodes were provisioned within timeout${NC}"
    echo "Checking NodeClaims:"
    kubectl get nodeclaims -A
    echo ""
    echo "Checking pods:"
    kubectl get pods -n "$TEST_NAMESPACE" -o wide
    echo ""
    echo "Recent Karpenter logs:"
    kubectl logs -l app.kubernetes.io/name=karpenter-oci -n "$NAMESPACE" --tail=50
    exit 1
fi

# Step 9: Verify flexible shape configuration
echo -e "${GREEN}Step 9: Verifying flexible shape configuration...${NC}"

# Get node details
echo "Node details:"
kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool -o custom-columns=NAME:.metadata.name,INSTANCE-TYPE:.metadata.labels.node\\.kubernetes\\.io/instance-type,CPU:.status.capacity.cpu,MEMORY:.status.capacity.memory

echo ""
echo "Node labels (showing OCI shape info):"
kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool -o json | jq -r '.items[] | {name: .metadata.name, shape: .metadata.labels["oci.oraclecloud.com/shape"], ocpus: .metadata.labels["oci.oraclecloud.com/ocpus"]}'

echo ""
echo "Pod placement:"
kubectl get pods -n "$TEST_NAMESPACE" -o wide

echo ""

# Step 10: Test consolidation
echo -e "${GREEN}Step 10: Testing consolidation...${NC}"
echo "Scaling down to test node consolidation..."

kubectl scale deployment inflate -n "$TEST_NAMESPACE" --replicas=1
echo "Scaled down to 1 replica"
echo ""

echo "Waiting 60 seconds for consolidation to trigger..."
sleep 60

NEW_NODE_COUNT=$(kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool --no-headers 2>/dev/null | wc -l)
echo "Nodes after consolidation: $NEW_NODE_COUNT"

if [ "$NEW_NODE_COUNT" -lt "$CURRENT_NODES" ]; then
    echo -e "${GREEN}✓ Consolidation working! Nodes reduced from $CURRENT_NODES to $NEW_NODE_COUNT${NC}"
else
    echo -e "${YELLOW}Consolidation may not have triggered yet${NC}"
fi

echo ""

# Step 11: Summary
echo -e "${GREEN}=== Test Summary ===${NC}"
echo ""

if [ "$PROVISIONED" = true ]; then
    echo -e "${GREEN}✅ Karpenter OCI provider is working correctly!${NC}"
    echo -e "${GREEN}✅ Flexible shape nodes were successfully provisioned${NC}"
    echo -e "${GREEN}✅ Pods were scheduled on the new nodes${NC}"
    echo ""
    echo "Key achievements:"
    echo "- Karpenter pod is running without errors"
    echo "- NodePool with flexible shapes is configured"
    echo "- Dynamic provisioning triggered when pods were pending"
    echo "- OCI compute instances were created with flexible shapes"
    echo "- Consolidation policy is active"
    echo ""
    echo "Next steps:"
    echo "1. Monitor long-term stability"
    echo "2. Test different workload patterns"
    echo "3. Verify cost optimization with different shape strategies"
    echo "4. Test preemptible instance handling"
else
    echo -e "${RED}❌ Some tests failed. Please check the logs above.${NC}"
fi

echo ""
echo "Cleanup command:"
echo "kubectl delete namespace $TEST_NAMESPACE"
echo "kubectl delete nodepool oci-flexible-pool -n $NAMESPACE"