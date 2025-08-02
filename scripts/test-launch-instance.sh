#!/bin/bash
# Test launching an instance with the same parameters as Karpenter

COMPARTMENT_ID="ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
IMAGE_ID="ocid1.image.oc1.iad.aaaaaaaaknzkygdhz6n3vmaovv3ouh2sxt7cudrdomefqfxnampnfrtpp6rq"
SUBNET_ID="ocid1.subnet.oc1.iad.aaaaaaaaznwweno45m7klssbzt2kyl6qa4ec34335patiolemkti6d4ioita"
SHAPE="VM.Standard.E4.Flex"

# Get availability domain
AD=$(oci iam availability-domain list --compartment-id "$COMPARTMENT_ID" --query "data[0].name" --raw-output)

echo "Testing instance launch with:"
echo "  Compartment: $COMPARTMENT_ID"
echo "  Image: $IMAGE_ID"
echo "  Subnet: $SUBNET_ID"
echo "  Shape: $SHAPE"
echo "  AD: $AD"

# Test launching instance
oci compute instance launch \
  --compartment-id "$COMPARTMENT_ID" \
  --availability-domain "$AD" \
  --shape "$SHAPE" \
  --shape-config '{"ocpus": 1, "memoryInGBs": 16}' \
  --image-id "$IMAGE_ID" \
  --subnet-id "$SUBNET_ID" \
  --display-name "karpenter-test-manual" \
  --assign-public-ip false