# Testing Karpenter OCI with Flexible Shapes

## Prerequisites

1. **Update HelmRelease** in your FluxCD repository with the new image tag:
   ```yaml
   spec:
     values:
       image:
         repository: ghcr.io/startappdev/karpenter
         tag: start-io-a5c59bc  # Latest build with full controller
   ```

2. **Ensure environment variables** are configured in the HelmRelease:
   ```yaml
   spec:
     values:
       controller:
         env:
           - name: OCI_REGION
             value: "us-ashburn-1"  # Your OCI region
           - name: OCI_COMPARTMENT_ID
             value: "ocid1.compartment.oc1..."  # Your compartment
           - name: OCI_CLUSTER_ID
             value: "ocid1.cluster.oc1..."  # Your OKE cluster
           - name: OCI_SUBNET_IDS
             value: "ocid1.subnet.oc1...,ocid1.subnet.oc1..."  # Comma-separated
           - name: OCI_IMAGE_ID
             value: "ocid1.image.oc1..."  # Node image OCID
           - name: CLUSTER_NAME
             value: "your-cluster-name"
           - name: OCI_USE_INSTANCE_PRINCIPAL
             value: "true"  # Or false if using user principal
           - name: ENABLE_OCI_DYNAMIC_SHAPES
             value: "true"  # Enable flexible shapes
   ```

## Step 1: Apply the OCI NodePool

```bash
# Apply the NodePool for flexible shapes
kubectl apply -f manifests/nodepool-oci-flexible.yaml

# Verify NodePool is created
kubectl get nodepool -n karpenter
kubectl describe nodepool oci-flexible-pool -n karpenter
```

## Step 2: Deploy Test Workload

```bash
# Create test namespace and deployment
kubectl apply -f manifests/test-autoscaling.yaml

# Check initial state (should be 0 pods)
kubectl get pods -n karpenter-test
```

## Step 3: Trigger Node Provisioning

```bash
# Scale up to trigger Karpenter
kubectl scale deployment inflate -n karpenter-test --replicas=4

# Watch Karpenter logs for provisioning activity
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci -f
```

## Step 4: Monitor Node Creation

In separate terminals, watch:

```bash
# Terminal 1: Watch for new nodes
watch kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool

# Terminal 2: Watch pod scheduling
watch kubectl get pods -n karpenter-test -o wide

# Terminal 3: Check NodeClaims
kubectl get nodeclaims -A
```

## Expected Behavior

When working correctly, you should see:

1. **Karpenter Logs** showing:
   ```
   INFO  controller.provisioning  Found 4 unschedulable pods
   INFO  controller.provisioning  Computed 1 new nodeclaim(s) for flexible shape
   INFO  controller.provisioning  Created nodeclaim with flexible shape VM.Standard.E4.Flex
   ```

2. **New Node** appearing with:
   - Label: `karpenter.sh/nodepool=oci-flexible-pool`
   - Flexible shape configuration (e.g., 4 OCPUs, 64GB memory)
   - OCI-specific annotations

3. **Pods** transitioning from Pending to Running on the new node

## Step 5: Test Consolidation

```bash
# Scale down to test node removal
kubectl scale deployment inflate -n karpenter-test --replicas=0

# Watch nodes being removed (after 30s consolidateAfter period)
watch kubectl get nodes -l karpenter.sh/nodepool=oci-flexible-pool
```

## Troubleshooting

### If nodes aren't being created:

1. **Check Karpenter pod is running**:
   ```bash
   kubectl get pods -n karpenter
   kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci --tail=100
   ```

2. **Verify OCI permissions**:
   - Instance principal or user principal has correct policies
   - Can create/delete instances in the compartment
   - Can access subnets and VCN

3. **Check NodePool status**:
   ```bash
   kubectl get nodepool -A -o yaml | grep -A10 status:
   ```

4. **Verify webhook is working**:
   ```bash
   kubectl get validatingwebhookconfigurations | grep karpenter
   kubectl get mutatingwebhookconfigurations | grep karpenter
   ```

### Common Issues:

1. **"No instance types satisfied requirements"**:
   - Check subnet availability
   - Verify shape limits in your tenancy
   - Ensure flexible shapes are enabled

2. **"Failed to create instance"**:
   - Check OCI API errors in logs
   - Verify compartment and subnet IDs
   - Check service limits

3. **Pods still Pending**:
   - Verify tolerations match node taints
   - Check nodeSelector matches node labels
   - Ensure sufficient resources requested

## Cleanup

```bash
# Delete test resources
kubectl delete -f manifests/test-autoscaling.yaml
kubectl delete -f manifests/nodepool-oci-flexible.yaml

# Karpenter should automatically clean up any nodes it created
```

## Success Criteria

✅ Karpenter pod running without errors
✅ NodePool recognized and ready
✅ Pods trigger node provisioning
✅ Flexible shape nodes created in OCI
✅ Pods scheduled on new nodes
✅ Nodes removed when empty

Once all criteria are met, Karpenter OCI with flexible shapes is working correctly!