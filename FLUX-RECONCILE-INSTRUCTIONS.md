# FluxCD Reconciliation Instructions

To ensure FluxCD picks up the latest changes (chart version 0.1.14), add the following annotation to your HelmRelease in your FluxCD repository:

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta2
kind: HelmRelease
metadata:
  name: karpenter
  namespace: karpenter
  annotations:
    reconcile.fluxcd.io/requestedAt: "2025-07-31T19:05:00Z"  # Update timestamp to force reconciliation
spec:
  interval: 1m  # Temporarily reduce interval for faster updates
  targetNamespace: karpenter
  install:
    createNamespace: true
  chart:
    spec:
      chart: ./helm/karpenter-oci
      sourceRef:
        kind: GitRepository
        name: karpenter
      version: "0.1.14"  # Specify exact version instead of '*'
  values:
    nodeSelector:
      karpenter.sh/controller: "true"
      kubernetes.io/os: linux
    oci:
      existingSecret: "oci-config-new"
      existingSecretConfigKey: ""
    podSecurityContext:
      fsGroup: 65532
      runAsGroup: 65532
      runAsNonRoot: true
      runAsUser: 65532
    resources:
      limits:
        memory: 1Gi
      requests:
        cpu: 200m
        memory: 500Mi
    serviceAccount:
      create: false
      name: karpenter
    settings:
      clusterName: "oke-cluster"  # Add this if not present
```

## Steps to Force Reconciliation:

1. **Update your FluxCD repository** with the above changes
2. **Commit and push** to trigger FluxCD
3. **Monitor the reconciliation**:
   ```bash
   # Watch GitRepository status
   kubectl get gitrepository -n flux-system karpenter -w
   
   # Watch HelmRelease status
   kubectl get helmrelease -n karpenter karpenter -w
   
   # Check events
   kubectl events -n karpenter --for helmrelease/karpenter
   ```

## Alternative: Manual Reconciliation Commands

If you have access to run kubectl commands:

```bash
# Force reconciliation of GitRepository
kubectl annotate gitrepository -n flux-system karpenter \
  reconcile.fluxcd.io/requestedAt="$(date +%s)" --overwrite

# Force reconciliation of HelmRelease
kubectl annotate helmrelease -n karpenter karpenter \
  reconcile.fluxcd.io/requestedAt="$(date +%s)" --overwrite
```

## Verify Updated Deployment

After reconciliation, verify the changes:

```bash
# Check if webhook port is configured
kubectl get webhookconfigurations -o yaml | grep -A 5 "port:"

# Check Karpenter pod status
kubectl get pods -n karpenter -l app.kubernetes.io/name=karpenter-oci

# Check logs
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci --tail=50
```