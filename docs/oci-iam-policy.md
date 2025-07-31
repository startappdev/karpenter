# OCI IAM Policies for Karpenter

This document describes the required IAM policies for Karpenter to manage OCI compute instances.

## Option 1: Instance Principal (Recommended)

### Step 1: Create Dynamic Group

Create a dynamic group that includes all instances in your OKE cluster:

```
ALL {instance.compartment.id = '<compartment-ocid>', tag.<tag-namespace>.<cluster-tag-key>.value = '<cluster-name>'}
```

Or more broadly for all OKE nodes:

```
ALL {instance.compartment.id = '<compartment-ocid>', tag.oke.cluster.value}
```

### Step 2: Create Policies

Create the following policies for the dynamic group:

```hcl
# Policy: karpenter-oke-policy
Allow dynamic-group <dynamic-group-name> to manage compute-instances in compartment <compartment-name> where request.permission != 'INSTANCE_DELETE'
Allow dynamic-group <dynamic-group-name> to use vnics in compartment <compartment-name>
Allow dynamic-group <dynamic-group-name> to use subnets in compartment <compartment-name>
Allow dynamic-group <dynamic-group-name> to use network-security-groups in compartment <compartment-name>
Allow dynamic-group <dynamic-group-name> to read virtual-network-family in compartment <compartment-name>
Allow dynamic-group <dynamic-group-name> to read instance-configurations in compartment <compartment-name>
Allow dynamic-group <dynamic-group-name> to read cluster-family in compartment <compartment-name>
Allow dynamic-group <dynamic-group-name> to read compute-capacity-reservations in compartment <compartment-name>
Allow dynamic-group <dynamic-group-name> to read compute-dedicated-vm-hosts in compartment <compartment-name>

# For instance termination (Karpenter consolidation)
Allow dynamic-group <dynamic-group-name> to manage instance-family in compartment <compartment-name> where request.operation = 'TerminateInstance'

# For reading images
Allow dynamic-group <dynamic-group-name> to read compute-images in compartment <compartment-name>

# For managing volumes (if using block storage)
Allow dynamic-group <dynamic-group-name> to manage volumes in compartment <compartment-name>
Allow dynamic-group <dynamic-group-name> to manage volume-attachments in compartment <compartment-name>
```

## Option 2: User Principal

If using user principal authentication, create an IAM user with API keys and apply similar policies:

### Step 1: Create User and Group

```bash
# Create group
oci iam group create --name karpenter-group --description "Group for Karpenter OKE autoscaler"

# Create user
oci iam user create --name karpenter-user --description "User for Karpenter OKE autoscaler"

# Add user to group
oci iam group add-user --group-id <group-ocid> --user-id <user-ocid>
```

### Step 2: Create API Key

```bash
# Generate API key
oci iam user api-key upload --user-id <user-ocid> --key-file <path-to-public-key>
```

### Step 3: Create Policies

```hcl
# Policy: karpenter-user-policy
Allow group <group-name> to manage compute-instances in compartment <compartment-name>
Allow group <group-name> to use vnics in compartment <compartment-name>
Allow group <group-name> to use subnets in compartment <compartment-name>
Allow group <group-name> to use network-security-groups in compartment <compartment-name>
Allow group <group-name> to read virtual-network-family in compartment <compartment-name>
Allow group <group-name> to read instance-configurations in compartment <compartment-name>
Allow group <group-name> to read cluster-family in compartment <compartment-name>
Allow group <group-name> to read compute-images in compartment <compartment-name>
Allow group <group-name> to manage volumes in compartment <compartment-name>
Allow group <group-name> to manage volume-attachments in compartment <compartment-name>
```

### Step 4: Create Kubernetes Secret

Create a secret with the OCI configuration:

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

Then reference this secret in your HelmRelease:

```yaml
spec:
  values:
    oci:
      existingSecret: "oci-config"
      existingSecretConfigKey: "config"
    controller:
      env:
        - name: OCI_USE_INSTANCE_PRINCIPAL
          value: "false"
```

## Required Permissions Summary

Karpenter needs the following permissions to function:

1. **Compute Instance Management**
   - Launch instances with flexible shapes
   - Terminate instances
   - List and describe instances

2. **Networking**
   - Attach VNICs to instances
   - Use subnets and security groups
   - Read VCN configuration

3. **Storage** (if using block volumes)
   - Create and attach volumes
   - Manage boot volumes

4. **OKE Integration**
   - Read cluster configuration
   - Read node pool configuration

## Troubleshooting

### Permission Denied Errors

If you see permission errors in Karpenter logs:

1. Check dynamic group matching rules:
   ```bash
   oci iam dynamic-group get --dynamic-group-id <dynamic-group-ocid>
   ```

2. Verify policies are attached:
   ```bash
   oci iam policy list --compartment-id <compartment-ocid> --name karpenter
   ```

3. Test permissions with OCI CLI from a node:
   ```bash
   # SSH to a node and test
   oci compute instance list --compartment-id <compartment-ocid>
   ```

### Instance Principal Not Working

1. Verify instance metadata service is enabled:
   ```bash
   curl -H "Authorization: Bearer Oracle" http://169.254.169.254/opc/v2/instance/
   ```

2. Check if the instance is in the dynamic group:
   ```bash
   # Get instance OCID
   curl -H "Authorization: Bearer Oracle" http://169.254.169.254/opc/v2/instance/id
   ```

3. Ensure the node has the correct tags for dynamic group matching