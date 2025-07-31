# Node Pool Selector Fix

## Issue
The Karpenter pod was getting an unexpected `karpenter.sh/controller=true` nodeSelector that prevented it from being scheduled on nodes with `node_pool=generic`.

## Root Cause
The HelmRelease in your FluxCD repository appears to be setting nodeSelector values that include `karpenter.sh/controller=true`. When HelmRelease sets values, they override the chart defaults.

## Solution
Modified the deployment template to always include `node_pool: generic` in the nodeSelector, similar to how we hardcoded the toleration. This ensures the selector is present even if HelmRelease provides other nodeSelector values.

## Changes Made
1. Updated `helm/karpenter-oci/templates/deployment.yaml` to always include `node_pool: generic`
2. Bumped Chart version from 0.1.1 to 0.1.2

## Verification Steps
After FluxCD reconciles:

```bash
# Force reconciliation
flux reconcile source git karpenter -n flux-system
flux reconcile helmrelease karpenter -n karpenter

# Check the deployment
kubectl get deploy karpenter-karpenter-oci -n karpenter -o yaml | grep -A10 nodeSelector

# Check pod status
kubectl get pods -n karpenter -o wide

# Check pod nodeSelector
kubectl get pods -n karpenter -o jsonpath='{.items[0].spec.nodeSelector}' | jq .
```

## Expected Result
The deployment should now have:
- `node_pool: generic` (forced by template)
- `kubernetes.io/os: linux` (common default)
- `karpenter.sh/controller: true` (if still set by HelmRelease)

The pod should be schedulable on nodes with the label `node_pool=generic` and toleration for `node_pool=generic:NoSchedule`.