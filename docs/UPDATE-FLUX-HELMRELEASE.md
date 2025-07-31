# Update Your FluxCD HelmRelease for Karpenter

In your FluxCD repository, update the HelmRelease for Karpenter with these changes:

## 1. Update the OCI secret configuration

Change FROM:
```yaml
  values:
    oci:
      existingSecret: oci-config
      existingSecretConfigKey: config.yaml
```

TO:
```yaml
  values:
    oci:
      existingSecret: "oci-config-new"
      existingSecretConfigKey: ""  # Empty string to use individual keys
```

## 2. Update the chart version

Ensure the chart version is set to the latest:
```yaml
  chart:
    spec:
      version: "0.1.11"
```

## 3. Complete example of the updated HelmRelease

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta2
kind: HelmRelease
metadata:
  name: karpenter
  namespace: karpenter
spec:
  interval: 1m
  targetNamespace: karpenter
  install:
    createNamespace: true
  chart:
    spec:
      chart: ./helm/karpenter-oci
      sourceRef:
        kind: GitRepository
        name: karpenter
      version: "0.1.11"
  values:
    nodeSelector:
      karpenter.sh/controller: "true"
      kubernetes.io/os: linux
    oci:
      existingSecret: "oci-config-new"  # Updated secret name
      existingSecretConfigKey: ""       # Empty to use individual keys
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

## Why these changes?

1. **Secret name**: We created a new sealed secret called `oci-config-new` that has individual keys for each environment variable
2. **Config key**: By setting `existingSecretConfigKey` to empty string, the Helm chart will read individual keys (region, compartmentId, etc.) instead of expecting a single config file
3. **Chart version**: 0.1.11 includes the fixes for the volume mount issues

## After making these changes:

1. Commit and push to your FluxCD repository
2. FluxCD will automatically reconcile and update the deployment
3. The new configuration will use the correct sealed secret with individual environment variables