#!/bin/bash
# Script to check FluxCD sync status and force update

echo "1. Checking GitRepository status:"
kubectl get gitrepository karpenter -n flux-system -o wide

echo -e "\n2. Checking last sync commit:"
kubectl get gitrepository karpenter -n flux-system -o jsonpath='{.status.artifact.revision}' && echo

echo -e "\n3. Checking HelmRelease status:"
kubectl get helmrelease karpenter -n karpenter -o wide

echo -e "\n4. Getting HelmRelease values to see if it includes nodeSelector:"
kubectl get helmrelease karpenter -n karpenter -o jsonpath='{.spec.values}' | grep -A5 -B5 "nodeSelector\|tolerations" || echo "No nodeSelector/tolerations found in HelmRelease values"

echo -e "\n5. Checking HelmChart status:"
kubectl get helmchart -n flux-system | grep karpenter

echo -e "\n6. Force reconciliation:"
echo "flux reconcile source git karpenter -n flux-system"
flux reconcile source git karpenter -n flux-system

echo -e "\n7. Wait a few seconds for git sync..."
sleep 5

echo -e "\n8. Force HelmRelease reconciliation:"
echo "flux reconcile helmrelease karpenter -n karpenter"
flux reconcile helmrelease karpenter -n karpenter

echo -e "\n9. Check deployment again after reconciliation:"
kubectl get deploy karpenter-karpenter-oci -n karpenter -o yaml | grep -A10 "nodeSelector:\|tolerations:"