# Karpenter Deployment Status

## ✅ Completed Steps

1. **Dynamic Node Provisioning Implementation** - All code has been implemented for the feature
2. **OCI Dynamic Group Created** - `karpenter-nodes-dg` created successfully
3. **OCI IAM Policy Created** - Basic policy created with the following permissions:
   - `Allow dynamic-group karpenter-nodes-dg to manage instances in compartment`
   - `Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment`
   - `Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment`

## Current Policy Details

```bash
Policy Name: karpenter-policy
Policy ID: ocid1.policy.oc1..<your-policy-ocid>
Compartment: ocid1.compartment.oc1..<your-compartment-ocid>
```

## Notes on Invalid Resource Types

The following resource types were found to be invalid in OCI IAM policies:
- `compute-capacity-reports` - Not a valid resource type
- `compute-global-price-list` - Not a valid resource type  
- `oke-clusters` - Not a valid resource type
- `shapes` - Cannot be used with "inspect" verb in tenancy scope

These permissions are not required for basic Karpenter functionality.

## Next Steps

1. **Configure OCI Authentication** - Set up Instance Principal or API key authentication
2. **Deploy Karpenter with FluxCD** - Follow the deployment guide
3. **Create NodePools** - Configure NodePools with dynamic provisioning enabled
4. **Test Dynamic Provisioning** - Deploy workloads to verify flexible shape provisioning

## Useful Commands

### View Current Policy
```bash
oci iam policy get --policy-id ocid1.policy.oc1..<your-policy-ocid>
```

### View Dynamic Group
```bash
oci iam dynamic-group list --compartment-id <tenancy-ocid> --all --query "data[?name=='karpenter-nodes-dg']"
```

### Add Additional Permissions (if needed later)
To add more permissions to the existing policy, you would need to:
1. Delete the current policy
2. Recreate with additional valid statements

Currently, the basic policy should be sufficient for Karpenter to:
- Create and manage compute instances
- Attach instances to subnets
- Create and attach block volumes