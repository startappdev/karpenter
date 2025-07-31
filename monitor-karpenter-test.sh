#!/bin/bash
# Monitor Karpenter OCI deployment and test results

set -euo pipefail

echo "========================================="
echo "Karpenter OCI Monitoring Script"
echo "========================================="

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to check pod status
check_pod_status() {
    local namespace=$1
    local label_selector=$2
    local expected_status=${3:-Running}
    
    echo -e "\n${YELLOW}Checking pods in namespace: $namespace with selector: $label_selector${NC}"
    kubectl get pods -n "$namespace" -l "$label_selector" -o wide
    
    local ready_count=$(kubectl get pods -n "$namespace" -l "$label_selector" -o jsonpath='{.items[?(@.status.phase=="'$expected_status'")].metadata.name}' | wc -w | xargs)
    local total_count=$(kubectl get pods -n "$namespace" -l "$label_selector" -o jsonpath='{.items[*].metadata.name}' | wc -w | xargs)
    
    if [ "$ready_count" -eq "$total_count" ] && [ "$total_count" -gt 0 ]; then
        echo -e "${GREEN}✓ All $ready_count pods are $expected_status${NC}"
        return 0
    else
        echo -e "${RED}✗ Only $ready_count/$total_count pods are $expected_status${NC}"
        return 1
    fi
}

# Function to get recent logs
get_recent_logs() {
    local namespace=$1
    local pod_selector=$2
    local lines=${3:-50}
    
    echo -e "\n${YELLOW}Recent logs from $pod_selector in $namespace:${NC}"
    kubectl logs -n "$namespace" -l "$pod_selector" --tail="$lines" --timestamps=true || echo "No logs available yet"
}

# Monitor Karpenter controller
echo -e "\n${YELLOW}=== Karpenter Controller Status ===${NC}"
check_pod_status "karpenter" "app.kubernetes.io/name=karpenter-oci"
get_recent_logs "karpenter" "app.kubernetes.io/name=karpenter-oci" 30

# Check NodePools
echo -e "\n${YELLOW}=== NodePools ===${NC}"
kubectl get nodepools -n karpenter -o wide || echo "NodePool CRD not found or no NodePools created"

# Check NodeClaims
echo -e "\n${YELLOW}=== NodeClaims ===${NC}"
kubectl get nodeclaims -n karpenter -o wide || echo "No NodeClaims found"

# Check Nodes provisioned by Karpenter
echo -e "\n${YELLOW}=== Karpenter Provisioned Nodes ===${NC}"
kubectl get nodes -l karpenter.sh/nodepool -o wide || echo "No Karpenter provisioned nodes found"

# Check test deployments
echo -e "\n${YELLOW}=== Test Deployments ===${NC}"
for app in test-e4-flex-app test-e5-flex-app test-mixed-app; do
    echo -e "\n${YELLOW}Deployment: $app${NC}"
    kubectl get deployment "$app" -n default -o wide 2>/dev/null || echo "Deployment $app not found"
    kubectl get pods -n default -l "app=${app%-app}" -o wide 2>/dev/null || echo "No pods found for $app"
done

# Check events
echo -e "\n${YELLOW}=== Recent Karpenter Events ===${NC}"
kubectl get events -n karpenter --sort-by='.lastTimestamp' | tail -20

# Check webhook configuration
echo -e "\n${YELLOW}=== Webhook Configurations ===${NC}"
kubectl get validatingwebhookconfigurations | grep karpenter || echo "No Karpenter validating webhooks found"
kubectl get mutatingwebhookconfigurations | grep karpenter || echo "No Karpenter mutating webhooks found"

# Summary
echo -e "\n${YELLOW}========================================="
echo "Summary"
echo "=========================================${NC}"

# Check if Karpenter is healthy
if check_pod_status "karpenter" "app.kubernetes.io/name=karpenter-oci" >/dev/null 2>&1; then
    echo -e "${GREEN}✓ Karpenter controller is running${NC}"
else
    echo -e "${RED}✗ Karpenter controller is not healthy${NC}"
fi

# Check if any nodes were provisioned
node_count=$(kubectl get nodes -l karpenter.sh/nodepool --no-headers 2>/dev/null | wc -l || echo "0")
if [ "$node_count" -gt 0 ]; then
    echo -e "${GREEN}✓ Karpenter has provisioned $node_count nodes${NC}"
else
    echo -e "${YELLOW}⚠ No nodes provisioned by Karpenter yet${NC}"
fi

# Check test workloads
pending_pods=$(kubectl get pods -n default -l 'app in (test-e4-flex,test-e5-flex,test-mixed)' --field-selector status.phase=Pending --no-headers 2>/dev/null | wc -l || echo "0")
if [ "$pending_pods" -gt 0 ]; then
    echo -e "${YELLOW}⚠ $pending_pods test pods are pending (might trigger node provisioning)${NC}"
else
    echo -e "${GREEN}✓ No test pods pending${NC}"
fi

echo -e "\n${YELLOW}Run this script again to check for updates${NC}"