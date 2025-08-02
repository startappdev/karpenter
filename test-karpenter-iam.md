# Testing Karpenter with IAM Policies

## Current Status
- ✅ IAM policy applied for karpenter-nodes-dg dynamic group
- ✅ Karpenter scaled back to 1 replica
- ⏳ Waiting for cluster connectivity to test

## Test Plan

### 1. Verify Karpenter Pod is Running
```bash
kubectl get pods -n karpenter
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci -f
```

### 2. Check NodePools Detection
```bash
kubectl get nodepools
kubectl describe nodepool minimal-e4-flex
```

### 3. Deploy Test Workload
```bash
# Create a deployment that requires E5.Flex shape
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: e5-flex-test
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: e5-flex-test
  template:
    metadata:
      labels:
        app: e5-flex-test
    spec:
      nodeSelector:
        node.kubernetes.io/instance-type: "E5.Flex"
      tolerations:
      - key: "karpenter.sh/nodepool"
        operator: "Equal"
        value: "minimal-e4-flex"
        effect: "NoSchedule"
      containers:
      - name: nginx
        image: nginx:latest
        resources:
          requests:
            cpu: "2"
            memory: "4Gi"
          limits:
            cpu: "2"
            memory: "4Gi"
EOF
```

### 4. Monitor NodeClaim Creation
```bash
# Watch for NodeClaim creation
kubectl get nodeclaims -w

# Check NodeClaim details
kubectl describe nodeclaim -l karpenter.sh/nodepool=minimal-e4-flex
```

### 5. Monitor Instance Launch in OCI
```bash
# Check OCI audit logs for LaunchInstance calls
oci audit event list \
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
  --start-time $(date -u -v-10M '+%Y-%m-%dT%H:%M:%SZ') \
  --end-time $(date -u '+%Y-%m-%dT%H:%M:%SZ') \
  --query "data[?contains(data.eventName, 'LaunchInstance')]"
```

### 6. Verify Node Registration
```bash
# Watch for new nodes
kubectl get nodes -w

# Check node labels
kubectl get nodes -L karpenter.sh/nodepool,node.kubernetes.io/instance-type
```

### 7. Test Consolidation
```bash
# Delete the test deployment
kubectl delete deployment e5-flex-test

# Watch Karpenter consolidate the node
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci -f | grep -i consolidation
```

## Expected Results

1. **Karpenter Pod**: Should be running without permission errors
2. **NodePool Detection**: Should show "minimal-e4-flex" NodePool
3. **Instance Launch**: Should succeed without 404 errors
4. **Node Registration**: New node should join cluster within 5 minutes
5. **Pod Scheduling**: Test pod should be scheduled on new node
6. **Consolidation**: Node should be terminated after pod deletion

## Troubleshooting Commands

```bash
# Check Karpenter controller logs for errors
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci --tail=100

# Check events
kubectl get events -n karpenter --sort-by='.lastTimestamp'

# Check NodeClaim status
kubectl get nodeclaims -o yaml

# Check if instance principal is working
kubectl exec -n karpenter -it $(kubectl get pods -n karpenter -l app.kubernetes.io/name=karpenter-oci -o name) -- curl -H "Authorization: Bearer Oracle" http://169.254.169.254/opc/v2/instance/

# Check OCI permissions
kubectl exec -n karpenter -it $(kubectl get pods -n karpenter -l app.kubernetes.io/name=karpenter-oci -o name) -- oci compute instance list --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq --auth instance_principal
```

## Success Criteria

- [ ] No IAM permission errors in Karpenter logs
- [ ] NodeClaim created successfully
- [ ] OCI instance launched without 404 errors
- [ ] Node registered and Ready in cluster
- [ ] Pod scheduled on Karpenter-provisioned node
- [ ] Node consolidated after workload removal