#!/bin/bash
# Update Karpenter deployment with the new image containing ShapeConfig fix

IMAGE_TAG="${1:-start-io}"
FULL_IMAGE="ghcr.io/startappdev/karpenter:${IMAGE_TAG}"

echo "Updating Karpenter deployment to use image: ${FULL_IMAGE}"

# Update the deployment
kubectl patch deployment karpenter-karpenter-oci -n karpenter \
  --type='json' -p="[{\"op\": \"replace\", \"path\": \"/spec/template/spec/containers/0/image\", \"value\": \"${FULL_IMAGE}\"}]"

# Wait for rollout
echo "Waiting for deployment rollout..."
kubectl rollout status deployment/karpenter-karpenter-oci -n karpenter

# Check logs
echo "Checking Karpenter logs..."
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci --tail=10