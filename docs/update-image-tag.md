# Update Image Tag in FluxCD

The GitHub Actions workflow has successfully built the image with tag: `start-io-07b8d4f`

## Update your FluxCD HelmRelease

In your FluxCD repository, update the HelmRelease to use the new image tag:

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: karpenter
  namespace: karpenter
spec:
  values:
    image:
      repository: ghcr.io/startappdev/karpenter
      tag: start-io-07b8d4f  # Update this line
    imagePullSecrets:
      - name: ghcr-pull-secret  # Add this if using private registry
    nodeSelector:
      # Remove karpenter.sh/controller if present, or ensure nodes have this label
      kubernetes.io/os: linux
      node_pool: generic
```

## Important Notes

1. The image is built from commit `07b8d4f` which includes all the nodeSelector fixes
2. Make sure you have created the `ghcr-pull-secret` if the repository is private
3. The nodes with `node_pool=generic` now also have `karpenter.sh/controller=true` label

## Verify Deployment

After updating:
```bash
flux reconcile helmrelease karpenter -n karpenter
kubectl get pods -n karpenter -o wide
```