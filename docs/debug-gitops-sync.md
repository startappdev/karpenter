# Debugging GitOps Sync Issues

## Check Commands

1. **Verify GitRepository is on correct branch and latest commit:**
```bash
# Check which branch and commit FluxCD is using
kubectl get gitrepository karpenter -n flux-system -o jsonpath='{.spec.ref.branch}' && echo
kubectl get gitrepository karpenter -n flux-system -o jsonpath='{.status.artifact.revision}' && echo

# Compare with latest commit on GitHub
git log --oneline -1 origin/start-io
```

2. **Check if HelmChart is using the correct path:**
```bash
# Check HelmChart resource
kubectl get helmchart -n flux-system -l app.kubernetes.io/instance=karpenter -o yaml | grep -A5 "sourceRef:\|chart:"
```

3. **Verify the values.yaml content in the HelmChart artifact:**
```bash
# Get the artifact URL
kubectl get helmchart -n flux-system -l app.kubernetes.io/instance=karpenter -o jsonpath='{.items[0].status.artifact.url}' && echo

# You might need to exec into a pod to check the actual values being used
```

4. **Check HelmRelease values:**
```bash
# See what values the HelmRelease is using
kubectl get helmrelease karpenter -n karpenter -o yaml | grep -A50 "values:"
```

## Common Issues

1. **GitRepository not updating**: 
   - FluxCD might be stuck on an old commit
   - Check events: `kubectl describe gitrepository karpenter -n flux-system`

2. **HelmRelease overriding values**:
   - The HelmRelease might have explicit nodeSelector/tolerations that override chart defaults
   - Check if there are any value overrides in your HelmRelease

3. **Cache issues**:
   - Sometimes FluxCD caches the Helm chart
   - Try: `flux reconcile source git karpenter -n flux-system --with-source`

## What to Check in Your Git Repo

Make sure in your FluxCD git repository, the HelmRelease doesn't have values that override the defaults. It should either:

1. Not specify nodeSelector/tolerations at all (use chart defaults)
2. Or include the node_pool values:

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: karpenter
  namespace: karpenter
spec:
  values:
    # Either don't specify nodeSelector/tolerations at all
    # Or include them explicitly:
    nodeSelector:
      kubernetes.io/os: linux
      node_pool: generic
    tolerations:
      - key: CriticalAddonsOnly
        operator: Exists
      - key: node_pool
        operator: Equal
        value: generic
        effect: NoSchedule
```