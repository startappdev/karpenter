# Fix Image Tag Format

## Issue
The deployment is showing an invalid image format: `ghcr.io/startappdev/karpenter::start-io-56a7f5a`

This happens when the HelmRelease values include the full image path in the `tag` field instead of just the tag.

## Solution

In your FluxCD HelmRelease, update the image configuration:

### ❌ Incorrect:
```yaml
spec:
  values:
    image:
      repository: ghcr.io/startappdev/karpenter
      tag: ghcr.io/startappdev/karpenter:start-io-56a7f5a
```

### ✅ Correct:
```yaml
spec:
  values:
    image:
      repository: ghcr.io/startappdev/karpenter
      tag: start-io-56a7f5a
```

Or use the latest successful build tag:
```yaml
spec:
  values:
    image:
      repository: ghcr.io/startappdev/karpenter
      tag: start-io-dc91241
```

## Available Image Tags

Recent successful builds:
- `start-io-dc91241` (latest with pull secret)
- `start-io-56a7f5a` 
- `start-io-07b8d4f`

## Verify Fix

After updating:
```bash
flux reconcile helmrelease karpenter -n karpenter
kubectl get pods -n karpenter -o wide
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci
```