#!/bin/bash
# Script to find OKE-compatible images in OCI

echo "Finding OKE-compatible images..."

# Get the compartment ID from environment or use the one from the docs
COMPARTMENT_ID="${COMPARTMENT_ID:-ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq}"

echo "Using compartment: $COMPARTMENT_ID"

# List all Oracle Linux 8 images (OKE typically uses OL8)
echo -e "\n=== Oracle Linux 8 Images ==="
oci compute image list \
  --compartment-id $COMPARTMENT_ID \
  --operating-system "Oracle Linux" \
  --operating-system-version "8" \
  --shape "VM.Standard.E4.Flex" \
  --query "data[?contains(\"display-name\", 'OKE') || contains(\"display-name\", 'oke')].{Name:\"display-name\", ID:id, TimeCreated:\"time-created\"}" \
  --output table

# Check if the specific image exists and get its details
IMAGE_ID="ocid1.image.oc1.iad.aaaaaaaaug6zsgyvt52ybxifdiitp6ywrudnlbxsaclurlla3zkw4umchbha"
echo -e "\n=== Checking specific image: $IMAGE_ID ==="
oci compute image get --image-id $IMAGE_ID 2>&1 | grep -E "(display-name|compartment-id|operating-system|lifecycle-state)"

# List platform images (Oracle-provided)
echo -e "\n=== Platform Images (Oracle-provided) ==="
oci compute image list \
  --compartment-id $COMPARTMENT_ID \
  --all \
  --query "data[?\"lifecycle-state\"=='AVAILABLE' && contains(\"display-name\", 'Oracle-Linux-8')].{Name:\"display-name\", ID:id}" \
  --output table

# Get images from an existing OKE node pool (if available)
echo -e "\n=== Images from existing OKE node pools ==="
CLUSTER_ID=$(oci ce cluster list --compartment-id $COMPARTMENT_ID --query "data[0].id" --raw-output 2>/dev/null)
if [ ! -z "$CLUSTER_ID" ]; then
    echo "Found cluster: $CLUSTER_ID"
    NODE_POOLS=$(oci ce node-pool list --compartment-id $COMPARTMENT_ID --cluster-id $CLUSTER_ID --query "data[].id" --raw-output)
    for pool in $NODE_POOLS; do
        echo -e "\nNode Pool: $pool"
        oci ce node-pool get --node-pool-id $pool --query "data.\"node-source\".\"image-id\"" --raw-output
    done
else
    echo "No OKE cluster found in compartment"
fi

echo -e "\n=== Recommendation ==="
echo "If the above commands show permission errors for the specific image ID,"
echo "it confirms the image is in a different compartment (likely Oracle's)."
echo "Apply the IAM policy updates in oci-iam-policy-update.md to fix this."