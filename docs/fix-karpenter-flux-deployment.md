# Fix Karpenter Flux Deployment

## Issue
The Flux HelmRelease is failing because:
1. Karpenter has moved from traditional Helm repository to OCI registry
2. Version 0.32.0 is outdated (latest is 1.5.0)
3. The chart URL `https://charts.karpenter.sh` is no longer valid

## Solution

### Option 1: Use OCI Registry with HelmRelease (Recommended)

Update your `karpenter/release.yaml`:

```yaml
# karpenter/release.yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: karpenter
  namespace: karpenter
spec:
  interval: 10m
  chart:
    spec:
      chart: karpenter
      version: "1.5.0"  # Latest stable version
      sourceRef:
        kind: HelmRepository
        name: karpenter-oci
        namespace: flux-system
  values:
    serviceAccount:
      create: false  # We already created it
      name: karpenter
    
    settings:
      cloudProvider: oci
      featureGates:
        dynamicProvisioning: true
    
    controller:
      env:
        - name: ENABLE_OCI_DYNAMIC_SHAPES
          value: "true"
        - name: OCI_CONFIG_PATH
          value: "/etc/oci/config.yaml"
      
      volumeMounts:
        - name: oci-config
          mountPath: /etc/oci
          readOnly: true
      
      volumes:
        - name: oci-config
          secret:
            secretName: oci-config
    
    webhook:
      enabled: true
      
    # Resource limits
    resources:
      limits:
        memory: 1Gi
      requests:
        cpu: 200m
        memory: 500Mi
    
    # Pod security context
    podSecurityContext:
      runAsNonRoot: true
      runAsUser: 65536
      runAsGroup: 65536
      fsGroup: 65536
      seccompProfile:
        type: RuntimeDefault
    
    # Container security context
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop:
          - ALL
      readOnlyRootFilesystem: true
      runAsNonRoot: true
```

Create the OCI HelmRepository:

```yaml
# flux-system/karpenter-oci-helmrepo.yaml
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: HelmRepository
metadata:
  name: karpenter-oci
  namespace: flux-system
spec:
  interval: 30m
  type: oci
  url: oci://public.ecr.aws/karpenter
```

### Option 2: Direct OCI Chart Reference (Flux 2.0.0+)

If you're using Flux 2.0.0 or later, you can reference the OCI chart directly:

```yaml
# karpenter/release.yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: karpenter
  namespace: karpenter
spec:
  interval: 10m
  chart:
    spec:
      chart: oci://public.ecr.aws/karpenter/karpenter
      version: "1.5.0"
  # ... rest of values remain the same
```

## Apply the Fix

1. **Delete the old HelmRepository** (if it exists):
```bash
kubectl delete helmrepository karpenter -n flux-system
```

2. **Apply the new OCI HelmRepository** (if using Option 1):
```bash
kubectl apply -f flux-system/karpenter-oci-helmrepo.yaml
```

3. **Update the HelmRelease**:
```bash
# Either edit the file in your Git repo and push, or apply directly:
kubectl apply -f karpenter/release.yaml
```

4. **Force reconciliation**:
```bash
flux reconcile source helm karpenter-oci -n flux-system
flux reconcile helmrelease karpenter -n karpenter
```

## Verify the Fix

```bash
# Check HelmRepository status
kubectl get helmrepository -n flux-system
kubectl describe helmrepository karpenter-oci -n flux-system

# Check HelmRelease status
kubectl get helmrelease -n karpenter
kubectl describe helmrelease karpenter -n karpenter

# Watch Flux logs
flux logs --follow --level=info --all-namespaces
```

## Alternative Versions

If version 1.5.0 doesn't work with your OCI provider implementation, you can try:
- `1.4.0` - Previous stable version
- `1.3.4` - Older stable version
- `1.0.10` - Last v1.0.x version

## Important Notes

1. **AWS-Specific Features**: The standard Karpenter chart from AWS ECR includes AWS-specific features. Since you're using OCI, some features may not apply.

2. **CRDs**: You may need to install Karpenter CRDs separately:
```bash
helm upgrade --install karpenter-crd oci://public.ecr.aws/karpenter/karpenter-crd \
  --version 1.5.0 \
  --namespace karpenter \
  --create-namespace
```

3. **Custom Build**: Since you're implementing Karpenter for OCI, you might need to use a custom-built chart that includes your OCI provider implementation.

## Troubleshooting

If you still get errors:

1. **Check available versions**:
```bash
# List available tags (requires AWS CLI)
aws ecr-public describe-image-tags \
  --repository-name karpenter/karpenter \
  --region us-east-1 \
  --query 'imageDetails[?imageTags!=`null`].imageTags[]' \
  --output text | tr '\t' '\n' | sort -V
```

2. **Use Helm directly to test**:
```bash
helm pull oci://public.ecr.aws/karpenter/karpenter --version 1.5.0
helm show values karpenter-1.5.0.tgz
```

3. **Check Flux version compatibility**:
```bash
flux version
# Ensure you have Flux 2.0+ for OCI support
```