#!/bin/bash

# Quick script to patch Karpenter deployment with OCI environment variables
# Use this if you need to quickly add env vars without going through FluxCD

set -e

echo "=== Quick Fix: Adding OCI Environment Variables to Karpenter ==="
echo ""

# Check if Karpenter is deployed
if ! kubectl get deployment -n karpenter -l app.kubernetes.io/name=karpenter-oci &> /dev/null; then
    echo "Error: Karpenter deployment not found"
    exit 1
fi

# Get deployment name
DEPLOYMENT=$(kubectl get deployment -n karpenter -l app.kubernetes.io/name=karpenter-oci -o jsonpath='{.items[0].metadata.name}')
echo "Found deployment: $DEPLOYMENT"
echo ""

# Prompt for values
echo "Enter OCI Region (e.g., us-ashburn-1):"
read -r OCI_REGION

echo "Enter OCI Compartment OCID:"
read -r OCI_COMPARTMENT_ID

echo "Enter OKE Cluster OCID:"
read -r OCI_CLUSTER_ID

echo "Enter Subnet OCIDs (comma-separated):"
read -r OCI_SUBNET_IDS

echo "Enter OKE Node Image OCID:"
read -r OCI_IMAGE_ID

echo "Enter Cluster Name:"
read -r CLUSTER_NAME

# Create patch
cat > /tmp/karpenter-env-patch.yaml <<EOF
spec:
  template:
    spec:
      containers:
      - name: controller
        env:
        - name: OCI_REGION
          value: "$OCI_REGION"
        - name: OCI_COMPARTMENT_ID
          value: "$OCI_COMPARTMENT_ID"
        - name: OCI_CLUSTER_ID
          value: "$OCI_CLUSTER_ID"
        - name: OCI_SUBNET_IDS
          value: "$OCI_SUBNET_IDS"
        - name: OCI_IMAGE_ID
          value: "$OCI_IMAGE_ID"
        - name: CLUSTER_NAME
          value: "$CLUSTER_NAME"
        - name: OCI_USE_INSTANCE_PRINCIPAL
          value: "true"
        - name: ENABLE_OCI_DYNAMIC_SHAPES
          value: "true"
EOF

echo ""
echo "Applying patch to deployment..."
kubectl patch deployment "$DEPLOYMENT" -n karpenter --patch-file=/tmp/karpenter-env-patch.yaml

echo ""
echo "Waiting for rollout to complete..."
kubectl rollout status deployment "$DEPLOYMENT" -n karpenter

echo ""
echo "Checking new pod logs..."
sleep 5
kubectl logs -l app.kubernetes.io/name=karpenter-oci -n karpenter --tail=20

echo ""
echo "✅ Environment variables added!"
echo ""
echo "⚠️  WARNING: This is a temporary fix!"
echo "The proper solution is to:"
echo "1. Create a sealed secret with: ./scripts/create-oci-sealed-secret.sh"
echo "2. Add it to your FluxCD repository"
echo "3. Update HelmRelease to reference the secret"

rm -f /tmp/karpenter-env-patch.yaml