#!/bin/bash

# Script to help gather OCI values needed for Karpenter configuration
# Requires OCI CLI to be installed and configured

set -e

echo "=== Gathering OCI Configuration Values for Karpenter ==="
echo ""

# Get current region
REGION=$(oci iam region list --query 'data[?key==`home-region`].name' --raw-output 2>/dev/null || echo "")
if [ -z "$REGION" ]; then
    echo "Could not determine region. Please ensure OCI CLI is configured."
    echo "Run: oci setup config"
    exit 1
fi
echo "OCI_REGION: $REGION"

# Get compartment ID (assuming we want the root compartment or can be specified)
if [ -z "$COMPARTMENT_ID" ]; then
    echo ""
    echo "Enter your compartment OCID (or press Enter to use tenancy root):"
    read -r COMPARTMENT_ID
    if [ -z "$COMPARTMENT_ID" ]; then
        COMPARTMENT_ID=$(oci iam compartment list --query 'data[0]."compartment-id"' --raw-output 2>/dev/null)
    fi
fi
echo "OCI_COMPARTMENT_ID: $COMPARTMENT_ID"

# List OKE clusters
echo ""
echo "Available OKE clusters in compartment:"
oci ce cluster list --compartment-id "$COMPARTMENT_ID" --query 'data[*].[name, id]' --output table 2>/dev/null || echo "No clusters found"

echo ""
echo "Enter your OKE cluster OCID:"
read -r CLUSTER_ID
echo "OCI_CLUSTER_ID: $CLUSTER_ID"

# Get cluster details to find subnets
echo ""
echo "Fetching cluster details..."
CLUSTER_JSON=$(oci ce cluster get --cluster-id "$CLUSTER_ID" --query 'data' 2>/dev/null)

# Get VCN ID from cluster
VCN_ID=$(echo "$CLUSTER_JSON" | jq -r '.["vcn-id"]' 2>/dev/null || echo "")

if [ -n "$VCN_ID" ]; then
    echo "VCN ID: $VCN_ID"
    echo ""
    echo "Available subnets in the cluster's VCN:"
    oci network subnet list --compartment-id "$COMPARTMENT_ID" --vcn-id "$VCN_ID" \
        --query 'data[*].[join(``, [`"Name: "`, "display-name", `", OCID: "`, id, `", CIDR: "`, "cidr-block"]), id]' \
        --output table 2>/dev/null || echo "No subnets found"
fi

echo ""
echo "Enter subnet OCIDs for node provisioning (comma-separated, at least 2 for HA):"
read -r SUBNET_IDS
echo "OCI_SUBNET_IDS: $SUBNET_IDS"

# Get available node images
echo ""
echo "Fetching OKE-compatible node images..."
echo "Available Oracle Linux images for OKE:"

# Get the Kubernetes version from cluster
K8S_VERSION=$(echo "$CLUSTER_JSON" | jq -r '.["kubernetes-version"]' 2>/dev/null || echo "")
echo "Cluster Kubernetes version: $K8S_VERSION"

# List compatible images
oci compute image list --compartment-id "$COMPARTMENT_ID" \
    --operating-system "Oracle Linux" \
    --operating-system-version "7.9" \
    --shape "VM.Standard.E4.Flex" \
    --query 'data[?contains("display-name", `OKE`) == `true`].[join(``, [`"Name: "`, "display-name", `", OCID: "`, id]), id]' \
    --output table 2>/dev/null || echo "Could not fetch images"

# Alternative: Get from OKE node pool
echo ""
echo "Alternatively, checking existing node pools for image IDs..."
NODE_POOLS=$(oci ce node-pool list --compartment-id "$COMPARTMENT_ID" --cluster-id "$CLUSTER_ID" --query 'data[*].id' --raw-output 2>/dev/null)
for pool_id in $NODE_POOLS; do
    IMAGE_ID=$(oci ce node-pool get --node-pool-id "$pool_id" --query 'data."node-image-id"' --raw-output 2>/dev/null)
    if [ -n "$IMAGE_ID" ]; then
        echo "Found image from existing node pool: $IMAGE_ID"
        break
    fi
done

echo ""
echo "Enter the OKE-compatible node image OCID:"
read -r IMAGE_ID
echo "OCI_IMAGE_ID: $IMAGE_ID"

# Get cluster name
CLUSTER_NAME=$(echo "$CLUSTER_JSON" | jq -r '.name' 2>/dev/null || echo "")
echo ""
echo "CLUSTER_NAME: $CLUSTER_NAME"

# Generate configuration summary
echo ""
echo "=== Configuration Summary ==="
echo ""
echo "Add these values to your FluxCD HelmRelease:"
echo ""
cat <<EOF
    controller:
      env:
        - name: OCI_REGION
          value: "$REGION"
        - name: OCI_COMPARTMENT_ID
          value: "$COMPARTMENT_ID"
        - name: OCI_CLUSTER_ID
          value: "$CLUSTER_ID"
        - name: OCI_SUBNET_IDS
          value: "$SUBNET_IDS"
        - name: OCI_IMAGE_ID
          value: "$IMAGE_ID"
        - name: CLUSTER_NAME
          value: "$CLUSTER_NAME"
        - name: OCI_USE_INSTANCE_PRINCIPAL
          value: "true"  # or "false" if using user principal
        - name: ENABLE_OCI_DYNAMIC_SHAPES
          value: "true"
EOF

echo ""
echo "=== Next Steps ==="
echo "1. Copy the above configuration to your FluxCD HelmRelease"
echo "2. Ensure your OKE cluster has the required IAM policies for Karpenter"
echo "3. If using instance principal, ensure the nodes have the correct dynamic group and policies"
echo "4. Commit and push the changes to trigger FluxCD reconciliation"