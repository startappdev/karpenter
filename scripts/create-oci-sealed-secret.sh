#!/bin/bash

# Script to create a sealed secret for OCI configuration
# This creates a secret that Karpenter will use for OCI authentication

set -e

echo "=== Creating Sealed Secret for OCI Configuration ==="
echo ""

# Check if kubeseal is installed
if ! command -v kubeseal &> /dev/null; then
    echo "Error: kubeseal is not installed."
    echo "Install it from: https://github.com/bitnami-labs/sealed-secrets/releases"
    exit 1
fi

# Variables
NAMESPACE="${NAMESPACE:-karpenter}"
SECRET_NAME="oci-config"
TEMP_DIR=$(mktemp -d)

# Cleanup on exit
trap "rm -rf $TEMP_DIR" EXIT

echo "Creating OCI configuration secret..."
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

echo "Use Instance Principal? (true/false) [default: true]:"
read -r USE_INSTANCE_PRINCIPAL
USE_INSTANCE_PRINCIPAL=${USE_INSTANCE_PRINCIPAL:-true}

# Create the configuration file
cat > "$TEMP_DIR/oci-config.yaml" <<EOF
region: "$OCI_REGION"
compartmentId: "$OCI_COMPARTMENT_ID"
clusterId: "$OCI_CLUSTER_ID"
subnetIds: "$OCI_SUBNET_IDS"
imageId: "$OCI_IMAGE_ID"
clusterName: "$CLUSTER_NAME"
useInstancePrincipal: $USE_INSTANCE_PRINCIPAL
enableDynamicShapes: true
EOF

# If not using instance principal, add user credentials
if [ "$USE_INSTANCE_PRINCIPAL" = "false" ]; then
    echo ""
    echo "=== User Principal Configuration ==="
    echo "Enter User OCID:"
    read -r USER_OCID
    
    echo "Enter Tenancy OCID:"
    read -r TENANCY_OCID
    
    echo "Enter API Key Fingerprint:"
    read -r FINGERPRINT
    
    echo "Enter path to private key file:"
    read -r PRIVATE_KEY_PATH
    
    if [ ! -f "$PRIVATE_KEY_PATH" ]; then
        echo "Error: Private key file not found at $PRIVATE_KEY_PATH"
        exit 1
    fi
    
    # Add user principal config
    cat >> "$TEMP_DIR/oci-config.yaml" <<EOF
user: "$USER_OCID"
tenancy: "$TENANCY_OCID"
fingerprint: "$FINGERPRINT"
EOF
    
    # Copy private key
    cp "$PRIVATE_KEY_PATH" "$TEMP_DIR/private-key.pem"
fi

# Create the Kubernetes secret
echo ""
echo "Creating Kubernetes secret..."

if [ "$USE_INSTANCE_PRINCIPAL" = "false" ]; then
    kubectl create secret generic "$SECRET_NAME" \
        --namespace="$NAMESPACE" \
        --from-file=config="$TEMP_DIR/oci-config.yaml" \
        --from-file=private-key.pem="$TEMP_DIR/private-key.pem" \
        --dry-run=client -o yaml > "$TEMP_DIR/secret.yaml"
else
    kubectl create secret generic "$SECRET_NAME" \
        --namespace="$NAMESPACE" \
        --from-file=config="$TEMP_DIR/oci-config.yaml" \
        --dry-run=client -o yaml > "$TEMP_DIR/secret.yaml"
fi

# Seal the secret
echo "Sealing the secret..."
kubeseal --format=yaml < "$TEMP_DIR/secret.yaml" > sealed-secret-oci-config.yaml

echo ""
echo "=== Success! ==="
echo ""
echo "Sealed secret created: sealed-secret-oci-config.yaml"
echo ""
echo "Next steps:"
echo "1. Add this sealed secret to your FluxCD repository"
echo "2. Update your HelmRelease to reference the existing secret:"
echo ""
echo "spec:"
echo "  values:"
echo "    oci:"
echo "      existingSecret: \"$SECRET_NAME\""
echo "      existingSecretConfigKey: \"config\""
echo ""
echo "3. The controller will also need these environment variables:"
echo ""
echo "    controller:"
echo "      env:"
echo "        - name: OCI_REGION"
echo "          value: \"$OCI_REGION\""
echo "        - name: OCI_COMPARTMENT_ID"
echo "          value: \"$OCI_COMPARTMENT_ID\""
echo "        - name: OCI_CLUSTER_ID"
echo "          value: \"$OCI_CLUSTER_ID\""
echo "        - name: OCI_SUBNET_IDS"
echo "          value: \"$OCI_SUBNET_IDS\""
echo "        - name: OCI_IMAGE_ID"
echo "          value: \"$OCI_IMAGE_ID\""
echo "        - name: CLUSTER_NAME"
echo "          value: \"$CLUSTER_NAME\""
echo "        - name: OCI_USE_INSTANCE_PRINCIPAL"
echo "          value: \"$USE_INSTANCE_PRINCIPAL\""
echo ""
echo "4. Commit and push to trigger FluxCD reconciliation"