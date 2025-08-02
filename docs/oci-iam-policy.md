# OCI IAM Policy Setup for Karpenter

This document describes the IAM policies required for Karpenter to manage OCI compute instances for OKE clusters.

## Overview

Karpenter requires specific IAM permissions to:
- Launch and terminate compute instances
- Manage networking for instances
- Access cluster information
- Read available shapes and images
- Monitor capacity

## Dynamic Group Configuration

### Existing Dynamic Group: karpenter-nodes-dg

- **OCID**: `ocid1.dynamicgroup.oc1..aaaaaaaanmiqapuxzo3ye2uqp7uzvf7plvftwxvmjhekdrnwcisp46tcplmq`
- **Purpose**: Grants permissions to all OKE worker nodes in the cluster
- **Matching Rule**: Includes all worker nodes in the OKE cluster

This dynamic group uses Option A configuration (all OKE worker nodes), which means:
- Any worker node in the cluster can use Karpenter permissions
- Simpler to manage but broader permissions scope
- Suitable for trusted environments

## Applied IAM Policy

The following IAM policy has been created and applied:

### Policy Details
- **Name**: `karpenter-policy`
- **OCID**: `ocid1.policy.oc1..aaaaaaaa4ydfyuoenejknpmmrmqziyjqe5bvjlyhhybs52slynw47vzc574q`
- **Compartment**: `ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq`

### Policy Statements

```
Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-nodes-dg to manage instance-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-nodes-dg to use volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-nodes-dg to inspect clusters in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-nodes-dg to read cluster-node-pools in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-nodes-dg to read cluster-workload-mappings in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-nodes-dg to use vnics in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-nodes-dg to use subnets in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
```

### Permission Breakdown

1. **Instance Management**
   - `manage instances`: Create, update, and terminate individual instances
   - `manage instance-family`: Full control over instance-related resources including:
     - Instance configurations
     - Instance pools
     - Cluster networks
     - Instance console connections

2. **Storage**
   - `use volume-family`: Attach and detach volumes for instances

3. **Cluster Access**
   - `inspect clusters`: Read cluster metadata and configuration
   - `read cluster-node-pools`: View existing node pool configurations
   - `read cluster-workload-mappings`: Access workload mapping information

4. **Networking**
   - `use vnics`: Create and attach virtual network interfaces
   - `use subnets`: Launch instances in specified subnets

## Additional Permissions (Optional)

The following permissions were considered but not included due to OCI API limitations:
- `read shapes`: Not a valid resource type in OCI IAM
- `read images`: Not a valid resource type in OCI IAM  
- `manage compute-capacity-reports`: For checking available capacity

These capabilities are inherently available through the instance-family permissions.

## Applying the Policy

The policy has already been applied using the OCI CLI:

```bash
oci iam policy create \
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
  --name karpenter-policy \
  --description "IAM policy for Karpenter controller using existing karpenter-nodes-dg dynamic group" \
  --statements '[
    "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to manage instance-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to use volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to inspect clusters in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to read cluster-node-pools in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to read cluster-workload-mappings in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to use vnics in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to use subnets in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
  ]'
```

## Verification

To verify the policy is active:

```bash
# Check the policy
oci iam policy get --policy-id ocid1.policy.oc1..aaaaaaaa4ydfyuoenejknpmmrmqziyjqe5bvjlyhhybs52slynw47vzc574q

# Check dynamic group membership
oci iam dynamic-group get --dynamic-group-id ocid1.dynamicgroup.oc1..aaaaaaaanmiqapuxzo3ye2uqp7uzvf7plvftwxvmjhekdrnwcisp46tcplmq

# Test from a node in the cluster
curl -H "Authorization: Bearer Oracle" http://169.254.169.254/opc/v2/instance/
```

## Alternative: User Principal Authentication

If instance principal is not available, you can use user principal authentication:

### Step 1: Create User and API Key
```bash
# Create group
oci iam group create --name karpenter-group --description "Group for Karpenter OKE autoscaler"

# Create user
oci iam user create --name karpenter-user --description "User for Karpenter OKE autoscaler"

# Add user to group
oci iam group add-user --group-id <group-ocid> --user-id <user-ocid>

# Generate and upload API key
oci iam user api-key upload --user-id <user-ocid> --key-file <path-to-public-key>
```

### Step 2: Create Kubernetes Secret
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: oci-config
  namespace: karpenter
type: Opaque
stringData:
  config: |
    [DEFAULT]
    user=<user-ocid>
    fingerprint=<key-fingerprint>
    tenancy=<tenancy-ocid>
    region=<region>
    key_file=/etc/oci/private-key.pem
  private-key.pem: |
    -----BEGIN RSA PRIVATE KEY-----
    <your-private-key-content>
    -----END RSA PRIVATE KEY-----
```

## Production Considerations

For production deployments, consider:

1. **Narrower Dynamic Group Scope**: Create a dedicated dynamic group for Karpenter controller nodes only
   ```
   ALL {instance.compartment.id = '<compartment-id>', tag.karpenter-controller.value = 'true'}
   ```

2. **Compartment Isolation**: Use dedicated compartments for Karpenter-managed resources

3. **Audit Logging**: Enable OCI audit logs to track instance lifecycle events

4. **Resource Limits**: Implement service limits to prevent runaway provisioning

5. **Network Isolation**: Use dedicated subnets for Karpenter-provisioned nodes

## Troubleshooting

### Common Issues

1. **404 NotAuthorizedOrNotFound Error**
   - Dynamic group doesn't include the Karpenter pod's node
   - Policies haven't propagated (wait 1-2 minutes)
   - Incorrect compartment or resource OCIDs

2. **"No permissions found" Error**
   - Invalid resource type in policy (e.g., "shapes", "images")
   - Incorrect verb for resource type
   - Policy syntax error

3. **Instance Principal Not Working**
   ```bash
   # Test from node
   curl -H "Authorization: Bearer Oracle" http://169.254.169.254/opc/v2/instance/id
   ```

4. **Check Audit Logs**
   ```bash
   oci audit event list \
     --compartment-id <compartment-id> \
     --start-time 2025-08-02T00:00:00Z \
     --end-time 2025-08-02T23:59:59Z \
     --query "data[?contains(data.eventName, 'LaunchInstance')]"
   ```

## Current Status

✅ Dynamic group exists and includes all OKE worker nodes  
✅ IAM policy created with necessary permissions  
✅ Karpenter configured to use instance principal authentication  
✅ Ready for node provisioning

## References

- [OCI IAM Policy Syntax](https://docs.oracle.com/en-us/iaas/Content/Identity/Concepts/policygetstarted.htm)
- [OCI Dynamic Groups](https://docs.oracle.com/en-us/iaas/Content/Identity/dynamicgroups/To_create_a_dynamic_group.htm)
- [OKE Instance Principal](https://docs.oracle.com/en-us/iaas/Content/ContEng/Tasks/contengworkingwithproviderflex.htm)