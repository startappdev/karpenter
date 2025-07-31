# OCI IAM Policy Setup for Karpenter

This is the single source of truth for setting up OCI IAM policies for Karpenter.

## Prerequisites

Before creating the policy, ensure you have:

1. **Dynamic Group Created**: The dynamic group must exist before creating the policy
2. **Correct Compartment ID**: Verify you're using the right compartment
3. **Proper Permissions**: You need manage permissions on policies in the compartment

## ⚠️ Important Note on OCI Resource Types

Through testing, we've found that many resource types mentioned in various documentation are **not valid** in OCI IAM policies:
- ❌ `compute-capacity-reports` - Invalid resource type
- ❌ `compute-global-price-list` - Invalid resource type
- ❌ `oke-clusters` - Invalid resource type
- ❌ `shapes` with "inspect" verb - Invalid combination

These permissions are not required for Karpenter to function properly.

## Step 1: Verify or Create Dynamic Group

First, check if the dynamic group exists:

```bash
# Replace with your actual tenancy OCID
TENANCY_OCID="ocid1.tenancy.oc1..your-tenancy-ocid"

# List all dynamic groups
oci iam dynamic-group list --compartment-id $TENANCY_OCID --all
```

If the dynamic group doesn't exist, create it:

```bash
# Set your variables
COMPARTMENT_ID="ocid1.compartment.oc1..<your-compartment-ocid>"
CLUSTER_ID="<your-cluster-ocid>"

# Create dynamic group
oci iam dynamic-group create \
  --compartment-id $TENANCY_OCID \
  --name "karpenter-nodes-dg" \
  --description "Dynamic group for Karpenter nodes" \
  --matching-rule "ALL {instance.compartment.id = '${COMPARTMENT_ID}', tag.oke-cluster-id.value = '${CLUSTER_ID}'}"
```

## Step 2: Create IAM Policy

### ✅ Working Policy (Use This)

Based on testing, here's the policy that actually works with OCI:

```bash
cat > karpenter-policy.json <<'EOF'
[
  "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..<your-compartment-ocid>",
  "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id ocid1.compartment.oc1..<your-compartment-ocid>",
  "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id ocid1.compartment.oc1..<your-compartment-ocid>"
]
EOF
```

Create the policy:

```bash
oci iam policy create \
  --compartment-id ocid1.compartment.oc1..<your-compartment-ocid> \
  --name "karpenter-policy" \
  --description "Policy for Karpenter to manage instances" \
  --statements file://karpenter-policy.json
```

This policy provides Karpenter with:
- **manage instances**: Create, delete, and manage compute instances
- **use virtual-network-family**: Attach instances to subnets and configure networking
- **manage volume-family**: Create and attach block storage volumes

### Additional Permissions (Optional)

You may also want to add these permissions if they're supported in your tenancy:

```bash
# For reading instance configurations
"Allow dynamic-group karpenter-nodes-dg to read instance-configurations in compartment id <compartment-id>"

# For reading compute management resources
"Allow dynamic-group karpenter-nodes-dg to read compute-management-family in compartment id <compartment-id>"
```

## Troubleshooting Common Errors

### Error: "No permissions found"

This error usually means:
1. The dynamic group doesn't exist
2. The dynamic group name is misspelled
3. You're in the wrong compartment

**Solution:**
```bash
# Verify dynamic group exists
oci iam dynamic-group list --compartment-id <tenancy-ocid> --all | grep karpenter

# If not found, create it first (see Step 1)
```


### Error: "Permission denied"

You may not have permission to create policies in the compartment.

**Solution:**
```bash
# Check your permissions
oci iam user get --user-id <your-user-ocid>

# You need: Allow group <your-group> to manage policies in compartment <compartment-name>
```

## Step 3: Verify Policy Creation

After creating the policy:

```bash
# List policies
oci iam policy list \
  --compartment-id ocid1.compartment.oc1..<your-compartment-ocid> \
  --query "data[?name=='karpenter-policy'].{name:name, id:id}" \
  --output table

# Get policy details
POLICY_ID=$(oci iam policy list \
  --compartment-id ocid1.compartment.oc1..<your-compartment-ocid> \
  --name "karpenter-policy" \
  --query 'data[0].id' \
  --raw-output)

oci iam policy get --policy-id $POLICY_ID
```

## Step 4: Update Existing Policy

If you need to add more permissions later:

```bash
# Update policy
oci iam policy update \
  --policy-id $POLICY_ID \
  --statements file://karpenter-policy-flexible.json
```

## Next Steps

Once the policy is created successfully:
1. Continue with Karpenter installation
2. Configure the OCI provider with Instance Principal authentication
3. Create NodePools with dynamic provisioning enabled

## Valid OCI Resource Types Reference

Here are the **confirmed valid** resource types for OCI IAM policies based on our testing:

✅ **Working Resource Types:**
- `instances` - Virtual machines (use with manage, use, read verbs)
- `instance-family` - All instance-related resources  
- `virtual-network-family` - VCN, subnets, security lists
- `volume-family` - Block storage volumes
- `instance-configurations` - Instance configuration resources (may work with read verb)
- `compute-management-family` - Compute management resources (may work with read verb)

❌ **Invalid Resource Types (Do Not Use):**
- `compute-capacity-reports` - Not a valid resource type
- `compute-global-price-list` - Not a valid resource type
- `oke-clusters` - Not a valid resource type
- `shapes` - Cannot be used with "inspect" verb at tenancy level

## Important Notes

1. Always verify the dynamic group exists before creating policies
2. Use the exact dynamic group name in policy statements
3. Ensure compartment IDs are correct
4. Start with minimal permissions and add more as needed
5. Some resources in documentation may not be valid in all regions