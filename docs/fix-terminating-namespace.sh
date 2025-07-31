#!/bin/bash
# Script to help fix stuck terminating namespace

NAMESPACE="karpenter"

echo "1. Checking namespace status:"
kubectl get namespace $NAMESPACE -o json | jq '.status'

echo -e "\n2. Checking for resources in the namespace:"
kubectl api-resources --verbs=list --namespaced -o name | xargs -n 1 kubectl get --show-kind --ignore-not-found -n $NAMESPACE

echo -e "\n3. Checking for resources with finalizers:"
kubectl get all,cm,secret,pvc,ingress,servicemonitor,helmrelease -n $NAMESPACE -o json | jq '.items[] | select(.metadata.finalizers != null) | {kind: .kind, name: .metadata.name, finalizers: .metadata.finalizers}'

echo -e "\n4. To force delete the namespace (use with caution):"
echo "kubectl get namespace $NAMESPACE -o json | jq '.spec.finalizers = []' | kubectl replace --raw \"/api/v1/namespaces/$NAMESPACE/finalize\" -f -"

echo -e "\n5. Alternative: Remove specific resource finalizers:"
echo "Example: kubectl patch helmrelease karpenter -n $NAMESPACE -p '{\"metadata\":{\"finalizers\":[]}}' --type=merge"