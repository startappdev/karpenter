# Troubleshooting Karpenter OCI Provider

## Common Issues and Solutions

### 1. Missing Environment Variables Error

**Error:**
```
ERROR: Required environment variables are not set: OCI_REGION, OCI_COMPARTMENT_ID, OCI_CLUSTER_ID
```

**Solutions:**

#### Option A: Use Sealed Secret (Recommended)
```bash
# Create sealed secret with your OCI configuration
./scripts/create-oci-sealed-secret.sh

# Add to FluxCD repo and update HelmRelease:
spec:
  values:
    oci:
      existingSecret: "oci-config"
```

#### Option B: Quick Fix (Temporary)
```bash
# Patch deployment directly
./scripts/quick-fix-oci-env.sh
```

#### Option C: Add to HelmRelease
```yaml
spec:
  values:
    controller:
      env:
        - name: OCI_REGION
          value: "us-ashburn-1"
        - name: OCI_COMPARTMENT_ID
          value: "ocid1.compartment.oc1..."
        # ... other values
```

### 2. Instance Principal Authentication Fails

**Error:**
```
failed to create OCI client: can't create client, bad configuration
```

**Solution:**
1. Verify instance metadata service is accessible:
   ```bash
   kubectl exec -it <karpenter-pod> -n karpenter -- curl -H "Authorization: Bearer Oracle" http://169.254.169.254/opc/v2/instance/
   ```

2. Check dynamic group membership:
   - Ensure nodes have correct tags
   - Verify dynamic group rules match

3. Switch to user principal if needed:
   ```bash
   # Set OCI_USE_INSTANCE_PRINCIPAL=false
   # Provide user credentials in sealed secret
   ```

### 3. No Nodes Being Provisioned

**Symptoms:**
- Pods remain pending
- No new nodes created
- No errors in Karpenter logs

**Debug Steps:**

1. **Check NodePool status:**
   ```bash
   kubectl get nodepool -n karpenter -o yaml
   ```

2. **Verify Karpenter can see pending pods:**
   ```bash
   kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci | grep -i "pending"
   ```

3. **Check for provisioning attempts:**
   ```bash
   kubectl get nodeclaims -A
   kubectl describe nodeclaim -A
   ```

4. **Verify OCI permissions:**
   - Check IAM policies allow instance creation
   - Verify subnet and VCN access
   - Check service limits in OCI console

### 4. Shape Selection Issues

**Error:**
```
No shape available for requirements: cpu=2, memory=8Gi
```

**Solutions:**

1. **Check shape constraints in NodePool:**
   ```yaml
   dynamicProvisioning:
     constraints:
       minOCPUs: 1      # Lower if needed
       maxOCPUs: 32     # Increase if needed
       minMemoryGB: 8   # Lower if needed
       maxMemoryGB: 256 # Increase if needed
   ```

2. **Verify shape availability:**
   ```bash
   # List available shapes in OCI
   oci compute shape list --compartment-id <compartment-id> --all
   ```

3. **Check subnet capacity:**
   - Ensure subnets have available IP addresses
   - Verify no quota limits are hit

### 5. Webhook Certificate Issues

**Error:**
```
Internal error occurred: failed calling webhook "validation.webhook.karpenter.sh"
```

**Solution:**
```bash
# Check webhook configuration
kubectl get validatingwebhookconfigurations | grep karpenter
kubectl get mutatingwebhookconfigurations | grep karpenter

# Restart Karpenter to regenerate certificates
kubectl rollout restart deployment -n karpenter -l app.kubernetes.io/name=karpenter-oci
```

### 6. Image Pull Errors

**Error:**
```
Failed to pull image "ghcr.io/startappdev/karpenter:..."
```

**Solution:**
1. Verify GHCR authentication secret exists:
   ```bash
   kubectl get secret start-github-pull-secret -n karpenter
   ```

2. Check if secret is properly referenced in deployment
3. Ensure image tag exists in registry

### 7. OCI API Rate Limiting

**Symptoms:**
- Slow provisioning
- 429 errors in logs

**Solution:**
1. Check Karpenter's batching settings:
   ```yaml
   settings:
     batchMaxDuration: 10s
     batchIdleDuration: 1s
   ```

2. Implement exponential backoff in OCI provider code
3. Consider using multiple compartments for load distribution

## Debug Commands Cheatsheet

```bash
# Check Karpenter status
kubectl get pods -n karpenter
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci -f

# Check NodePool
kubectl get nodepool -n karpenter
kubectl describe nodepool oci-flexible-pool -n karpenter

# Check NodeClaims
kubectl get nodeclaims -A
kubectl describe nodeclaim -A

# Check pending pods
kubectl get pods --all-namespaces --field-selector=status.phase=Pending

# Check nodes created by Karpenter
kubectl get nodes -l karpenter.sh/nodepool

# Force reconciliation
flux reconcile helmrelease karpenter -n flux-system

# Check events
kubectl get events -n karpenter --sort-by='.lastTimestamp'

# Verify environment variables
kubectl exec -it <karpenter-pod> -n karpenter -- env | grep OCI
```

## Performance Tuning

### 1. Provisioning Speed
- Reduce `batchIdleDuration` for faster response
- Pre-warm instances using scheduled scaling
- Use dedicated subnets for Karpenter nodes

### 2. Cost Optimization
- Set appropriate `consolidateAfter` duration
- Use preemptible instances for non-critical workloads
- Implement proper `expireAfter` values

### 3. Resource Efficiency
- Tune `packingEfficiency` based on workload patterns
- Adjust CPU and memory headroom percentages
- Use appropriate shape selection strategy

## Getting Help

1. **Check Logs:**
   ```bash
   kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci --tail=100
   ```

2. **Enable Debug Logging:**
   ```yaml
   settings:
     logLevel: debug
   ```

3. **File Issues:**
   - Include full error messages
   - Provide NodePool configuration
   - Share relevant logs
   - Include OCI region and shape details