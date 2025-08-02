# OCI IAM Policies for Karpenter

The following IAM policies are required for Karpenter to work with instance principal authentication:

## 1. Dynamic Group for Karpenter Controller

First, create a dynamic group that includes the nodes where Karpenter can run. Choose the most appropriate option:

### Option A: All OKE Worker Nodes (Recommended)
This allows Karpenter to run on any node in the cluster:
```
ALL {instance.compartment.id = '<compartment-ocid>', tag.oke-cluster.value = '<cluster-ocid>'}
```

### Option B: Specific Node Pool
If Karpenter runs in a dedicated node pool:
```
ALL {instance.compartment.id = '<compartment-ocid>', tag.oke-node-pool.value = '<node-pool-ocid>'}
```

### Option C: Tagged Nodes
For fine-grained control, tag specific nodes:
```
ALL {instance.compartment.id = '<compartment-ocid>', tag.karpenter-controller.value = 'true'}
```

### Option D: Single Instance (Not Recommended)
Only for testing - not suitable for production:
```
ALL {instance.id = '<instance-ocid-of-node-running-karpenter>'}
```

## 2. Required IAM Policies

Create these policies in the **root compartment** or the compartment containing your resources:

```
# Allow Karpenter to manage compute instances
Allow dynamic-group <karpenter-dynamic-group> to manage instances in compartment <compartment-name>
Allow dynamic-group <karpenter-dynamic-group> to manage instance-family in compartment <compartment-name>
Allow dynamic-group <karpenter-dynamic-group> to use volume-family in compartment <compartment-name>

# Allow Karpenter to read cluster information
Allow dynamic-group <karpenter-dynamic-group> to inspect clusters in compartment <compartment-name>
Allow dynamic-group <karpenter-dynamic-group> to read cluster-node-pools in compartment <compartment-name>
Allow dynamic-group <karpenter-dynamic-group> to read cluster-workload-mappings in compartment <compartment-name>

# Allow Karpenter to use network resources
Allow dynamic-group <karpenter-dynamic-group> to use vnics in compartment <compartment-name>
Allow dynamic-group <karpenter-dynamic-group> to use subnets in compartment <compartment-name>
Allow dynamic-group <karpenter-dynamic-group> to use network-security-groups in compartment <compartment-name>
Allow dynamic-group <karpenter-dynamic-group> to read virtual-network-family in compartment <compartment-name>

# Allow Karpenter to read images
Allow dynamic-group <karpenter-dynamic-group> to read images in compartment <compartment-name>
Allow dynamic-group <karpenter-dynamic-group> to read shapes in compartment <compartment-name>

# Allow Karpenter to read availability domains
Allow dynamic-group <karpenter-dynamic-group> to inspect availability-domains in tenancy

# Allow Karpenter to read compute capacity
Allow dynamic-group <karpenter-dynamic-group> to read compute-capacity-reports in compartment <compartment-name>
Allow dynamic-group <karpenter-dynamic-group> to read dedicated-vm-hosts in compartment <compartment-name>
```

## 3. Optional: Cross-Compartment Access

If your resources are in different compartments:

```
# If images are in a different compartment
Allow dynamic-group <karpenter-dynamic-group> to read images in compartment <images-compartment>

# If network resources are in a different compartment  
Allow dynamic-group <karpenter-dynamic-group> to use subnets in compartment <network-compartment>
Allow dynamic-group <karpenter-dynamic-group> to use vnics in compartment <network-compartment>
```

## 4. Verification

To verify the policies are working, check the Karpenter logs for successful OCI API calls without authorization errors.

## Common Issues

1. **404 NotAuthorizedOrNotFound**: Usually means either:
   - The dynamic group doesn't include the Karpenter instance
   - The policies are not in effect (wait 1-2 minutes after creation)
   - The resource OCID is incorrect or in a different region
   - The compartment OCID in the policy doesn't match the resource compartment

2. **Resource in Different Region**: Ensure all resources (subnet, image, cluster) are in the same region as configured in Karpenter.

3. **Private Cluster Endpoints**: For private OKE clusters, ensure Karpenter can reach the cluster endpoint through the private network.