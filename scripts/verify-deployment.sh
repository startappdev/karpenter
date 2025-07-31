#!/bin/bash

# Quick verification script for Karpenter deployment

set -e

echo "=== Verifying Karpenter Deployment ==="
echo ""

# Check if the sealed secret is created
echo "1. Checking SealedSecret..."
kubectl get sealedsecret oci-config-new -n karpenter -o wide || echo "SealedSecret not found"

# Check if the secret is unsealed
echo ""
echo "2. Checking if secret is unsealed..."
kubectl get secret oci-config-new -n karpenter -o wide || echo "Secret not found"

# Check Karpenter deployment
echo ""
echo "3. Checking Karpenter deployment..."
kubectl get deployment -n karpenter -l app.kubernetes.io/name=karpenter-oci

# Check pod status
echo ""
echo "4. Checking pod status..."
kubectl get pods -n karpenter -l app.kubernetes.io/name=karpenter-oci

# Check environment variables
echo ""
echo "5. Checking environment variables in pod..."
POD=$(kubectl get pods -n karpenter -l app.kubernetes.io/name=karpenter-oci -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$POD" ]; then
    echo "Pod: $POD"
    kubectl exec -n karpenter "$POD" -- env | grep -E "OCI_|CLUSTER_NAME" | sort
else
    echo "No running pod found"
fi

# Check logs
echo ""
echo "6. Recent logs..."
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci --tail=20 || echo "No logs available"

echo ""
echo "=== Summary ==="
kubectl get all -n karpenter -l app.kubernetes.io/name=karpenter-oci