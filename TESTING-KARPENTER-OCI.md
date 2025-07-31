# Testing Karpenter OCI Provider

This guide provides instructions for testing the Karpenter OCI provider with flexible shape node provisioning.

## Prerequisites

1. Karpenter controller must be running successfully
2. FluxCD must have reconciled the latest chart version (0.1.14+)
3. OCI credentials must be properly configured

## Step 1: Update FluxCD HelmRelease

Ensure your FluxCD HelmRelease has the latest chart version:

```yaml
spec:
  chart:
    spec:
      version: "0.1.14"  # or later
```

## Step 2: Deploy NodePools and Test Workloads

### Option A: Using kubectl directly (for testing)

```bash
# Apply the test manifests
kubectl apply -f test-karpenter-oci.yaml

# This will create:
# - Two NodePools (E4.Flex and E5.Flex)
# - One NodeClass for OCI
# - Three test deployments with different resource requirements
```

### Option B: Using FluxCD GitOps (recommended)

1. Copy the content from `flux-karpenter-test.yaml` to your FluxCD repository
2. Place it at: `clusters/your-cluster/karpenter/karpenter-test.yaml`
3. Commit and push to trigger FluxCD reconciliation

## Step 3: Monitor Deployment

Run the monitoring script to check the status:

```bash
./monitor-karpenter-test.sh
```

This script will show:
- Karpenter controller status
- NodePools created
- Nodes provisioned by Karpenter
- Test deployment status
- Recent events

## Step 4: Verify Node Provisioning

### Expected Behavior

1. **Initial State**: Test pods will be in Pending state
2. **Node Provisioning**: Karpenter should detect pending pods and provision nodes
3. **Node Types**:
   - E4.Flex nodes for the `test-e4-flex-app`
   - E5.Flex nodes for the `test-e5-flex-app`
   - Either type for the `test-mixed-app`

### Verification Commands

```bash
# Check NodeClaims (Karpenter's record of provisioning requests)
kubectl get nodeclaims -n karpenter

# Check nodes provisioned by Karpenter
kubectl get nodes -l karpenter.sh/nodepool

# Check pod scheduling
kubectl get pods -n default -o wide | grep test-

# Check Karpenter logs for provisioning activity
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci -f
```

## Step 5: Test Different Scenarios

### Scenario 1: Scale Up Test
```bash
# Scale up deployments to trigger more node provisioning
kubectl scale deployment test-e4-flex-app -n default --replicas=5
kubectl scale deployment test-e5-flex-app -n default --replicas=3
```

### Scenario 2: Node Consolidation Test
```bash
# Scale down to test consolidation
kubectl scale deployment test-e4-flex-app -n default --replicas=0
# Wait 30 seconds (consolidateAfter setting)
# Karpenter should deprovision unused nodes
```

### Scenario 3: Mixed Resource Requirements
```bash
# Deploy app with varying resource requirements
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-variable-resources
  namespace: default
spec:
  replicas: 5
  selector:
    matchLabels:
      app: test-variable
  template:
    metadata:
      labels:
        app: test-variable
    spec:
      tolerations:
        - key: node-type
          operator: Exists
      containers:
        - name: app
          image: nginx:alpine
          resources:
            requests:
              cpu: "1"
              memory: "2Gi"
            limits:
              cpu: "2"
              memory: "4Gi"
EOF
```

## Troubleshooting

### Pods Stuck in Pending

1. Check NodePool configuration:
```bash
kubectl describe nodepool -n karpenter
```

2. Check Karpenter logs for errors:
```bash
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci --tail=100
```

3. Verify webhook is working:
```bash
kubectl get validatingwebhookconfigurations | grep karpenter
```

### Nodes Not Provisioning

1. Check OCI credentials:
```bash
kubectl get secret oci-config-new -n karpenter -o jsonpath='{.data}' | jq 'keys'
```

2. Check for quota issues in OCI console

3. Verify subnet and network configuration

### Clean Up Test Resources

```bash
# Delete test workloads
kubectl delete -f test-karpenter-oci.yaml

# Or if using individual resources:
kubectl delete deployment -n default test-e4-flex-app test-e5-flex-app test-mixed-app
kubectl delete nodepool -n karpenter oci-e4-flex-pool oci-e5-flex-pool
kubectl delete nodeclass -n karpenter oci-nodeclass
```

## Success Criteria

✅ Karpenter controller is running without errors
✅ NodePools are created and show as Ready
✅ Test pods trigger node provisioning
✅ Nodes are provisioned with correct shapes (E4.Flex, E5.Flex)
✅ Pods are scheduled on provisioned nodes
✅ Node consolidation works when scaling down
✅ Multiple flexible shapes can be provisioned simultaneously

## Next Steps

Once testing is successful:
1. Create production NodePools with appropriate limits
2. Configure disruption policies for production workloads
3. Set up monitoring and alerting for Karpenter metrics
4. Document OCI-specific configurations for your team