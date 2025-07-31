# Verifying Karpenter OCI

## Current Status

The Karpenter pod is running, but it's using a minimal HTTP server stub because the full Karpenter functionality needs to be restored with proper OCI provider integration.

## Check Karpenter Health

```bash
# Check pod status
kubectl get pods -n karpenter

# Check logs
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci -f

# Check metrics endpoint
kubectl port-forward -n karpenter svc/karpenter-karpenter-oci 8080:8080 &
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz

# Check if Karpenter webhook is working
kubectl get validatingwebhookconfigurations | grep karpenter
kubectl get mutatingwebhookconfigurations | grep karpenter
```

## Deploy NodePool

Before applying the NodePool, you need to update the configuration with your OCI values:

1. Edit `manifests/nodepool-oci-flexible.yaml` if needed
2. Apply the NodePool:

```bash
kubectl apply -f manifests/nodepool-oci-flexible.yaml
```

3. Verify NodePool creation:

```bash
kubectl get nodepool -n karpenter
kubectl describe nodepool oci-flexible-pool -n karpenter
```

## Test Autoscaling

1. Create test namespace and deployment:

```bash
kubectl apply -f manifests/test-autoscaling.yaml
```

2. Check initial state:

```bash
# Should show no nodes with karpenter labels yet
kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool

# Should show 0 pods
kubectl get pods -n karpenter-test
```

3. Scale up the deployment to trigger node provisioning:

```bash
# This will request 8 CPUs and 16Gi memory (4 pods × 2 CPU × 4Gi each)
kubectl scale deployment inflate -n karpenter-test --replicas=4
```

4. Watch Karpenter provision nodes:

```bash
# Watch Karpenter logs
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci -f

# Watch nodes being created
watch kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool

# Watch pods scheduling
watch kubectl get pods -n karpenter-test -o wide
```

5. Test scale down:

```bash
# Scale down to 0
kubectl scale deployment inflate -n karpenter-test --replicas=0

# Watch Karpenter consolidate/remove nodes after consolidateAfter period (30s)
watch kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool
```

## Expected Behavior

When fully functional, Karpenter should:

1. **On scale up**: 
   - Detect unschedulable pods
   - Calculate required capacity
   - Create OCI instances with flexible shapes
   - Register new nodes with the cluster
   - Schedule pods on new nodes

2. **On scale down**:
   - Detect underutilized nodes
   - Cordon and drain nodes safely
   - Terminate OCI instances
   - Clean up node resources

## Troubleshooting

If autoscaling doesn't work:

1. **Check Karpenter permissions**:
   ```bash
   kubectl get clusterrole karpenter-karpenter-oci -o yaml
   kubectl get clusterrolebinding karpenter-karpenter-oci -o yaml
   ```

2. **Check OCI configuration**:
   ```bash
   kubectl get secret oci-config -n karpenter -o yaml
   kubectl get cm karpenter-karpenter-oci-settings -n karpenter -o yaml
   ```

3. **Check for webhook issues**:
   ```bash
   kubectl get events -n karpenter --sort-by='.lastTimestamp'
   ```

4. **Verify NodePool is recognized**:
   ```bash
   kubectl get nodepool -A
   kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci | grep -i nodepool
   ```

## Cleanup

```bash
# Delete test resources
kubectl delete -f manifests/test-autoscaling.yaml
kubectl delete -f manifests/nodepool-oci-flexible.yaml

# Any nodes created by Karpenter should be automatically cleaned up
```

## Next Steps

The current pod is running a stub HTTP server. To get full Karpenter functionality:

1. The main.go needs to be restored to the actual Karpenter operator code
2. The OCI provider needs to be properly integrated
3. The webhook server needs to be functioning
4. The controller needs to watch for pods and NodePools

Currently, the autoscaling won't work because the pod is just serving HTTP endpoints without the actual Karpenter controller logic.