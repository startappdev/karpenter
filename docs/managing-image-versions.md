# Managing Karpenter Image Versions

## Overview

The Karpenter image and tag are now configured in the Helm chart's `values.yaml` file, preventing FluxCD HelmRelease from overriding them.

## Current Configuration

### values.yaml
```yaml
image:
  repository: ghcr.io/startappdev/karpenter
  tag: "start-io-633cb25"  # This is the default tag
```

### Chart.yaml
```yaml
appVersion: "start-io-633cb25"  # Fallback if tag is empty in values.yaml
```

## How It Works

1. The deployment uses the helper template `karpenter-oci.image`
2. If `image.tag` is set in values.yaml, it uses that
3. If `image.tag` is empty, it falls back to `Chart.AppVersion`
4. FluxCD HelmRelease should NOT override these values

## Updating the Image

### Option 1: Update values.yaml (Recommended)

1. Edit `helm/karpenter-oci/values.yaml`:
   ```yaml
   image:
     tag: "start-io-NEW_COMMIT_SHA"
   ```

2. Bump the chart version:
   ```yaml
   version: 0.1.7  # Increment version
   ```

3. Commit and push:
   ```bash
   git add helm/
   git commit -m "chore: Update Karpenter image to start-io-NEW_COMMIT_SHA"
   git push
   ```

### Option 2: Use HelmRelease Override (Not Recommended)

If you must override from FluxCD, ensure you're not setting the full image path:

```yaml
# ❌ WRONG - This creates double colons
spec:
  values:
    image:
      repository: ghcr.io/startappdev/karpenter
      tag: ghcr.io/startappdev/karpenter:start-io-abc123

# ✅ CORRECT - Only set the tag
spec:
  values:
    image:
      tag: start-io-abc123
```

## Checking Current Image

```bash
# Check what image the deployment is using
kubectl get deployment karpenter-karpenter-oci -n karpenter -o jsonpath='{.spec.template.spec.containers[0].image}'

# Check the running pod's image
kubectl get pods -n karpenter -o jsonpath='{.items[0].spec.containers[0].image}'
```

## GitHub Actions Integration

When a new build completes:

1. Check the workflow run for the new image tag
2. Update `values.yaml` with the new tag
3. The tag format is: `start-io-<SHORT_COMMIT_SHA>`

Example:
- Commit: `633cb2567d5e38ac62f3d45678901234abcdef12`
- Tag: `start-io-633cb25`

## Best Practice

1. **Always use GitOps**: Update the values.yaml in the git repository
2. **Don't override in HelmRelease**: Let the chart manage its own defaults
3. **Track versions**: Keep Chart.yaml appVersion in sync with values.yaml tag
4. **Test first**: Verify new images in a test environment before production