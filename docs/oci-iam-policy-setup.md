# OCI IAM Policy Setup for Karpenter

This is the single source of truth for setting up OCI IAM policies for Karpenter.

## Prerequisites

Before creating the policy, ensure you have:

1. **Dynamic Group Created**: The dynamic group must exist before creating the policy
2. **Correct Compartment ID**: Verify you're using the right compartment
3. **Proper Permissions**: You need manage permissions on policies in the compartment

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
COMPARTMENT_ID="ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
CLUSTER_ID="<your-cluster-ocid>"

# Create dynamic group
oci iam dynamic-group create \
  --compartment-id $TENANCY_OCID \
  --name "karpenter-nodes-dg" \
  --description "Dynamic group for Karpenter nodes" \
  --matching-rule "ALL {instance.compartment.id = '${COMPARTMENT_ID}', tag.oke-cluster-id.value = '${CLUSTER_ID}'}"
```

## Step 2: Create IAM Policy

### Option A: Basic Policy (Start with this)

Create a minimal policy file:

```bash
cat > karpenter-policy-basic.json <<'EOF'
[
  "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
]
EOF
```

Create the policy:

```bash
oci iam policy create \
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
  --name "karpenter-policy" \
  --description "Policy for Karpenter to manage instances" \
  --statements file://karpenter-policy-basic.json
```

### Option B: Extended Policy for OKE

If you need OKE-specific permissions:

```bash
cat > karpenter-policy-oke.json <<'EOF'
[
  "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use oke-clusters in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
]
EOF
```

### Option C: Policy for Flexible Shapes

For dynamic provisioning with flexible shapes:

```bash
cat > karpenter-policy-flexible.json <<'EOF'
[
  "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use oke-clusters in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to read instance-configurations in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to inspect shapes in tenancy"
]
EOF
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

### Error: "Invalid resource type"

Some resource types mentioned in documentation may not be valid in your OCI region/tenancy:
- `compute-capacity-reports` - Not a valid resource type
- `compute-global-price-list` - Not a valid resource type

**Solution:** Use the basic or OKE policy options above which only include valid resource types.

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
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
  --query "data[?name=='karpenter-policy'].{name:name, id:id}" \
  --output table

# Get policy details
POLICY_ID=$(oci iam policy list \
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
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

Here are the valid resource types for OCI IAM policies:

- `instances` - Virtual machines
- `instance-family` - All instance-related resources  
- `instance-configurations` - Instance configuration resources
- `virtual-network-family` - VCN, subnets, security lists
- `volume-family` - Block storage volumes
- `oke-clusters` - OKE cluster resources
- `shapes` - Shape information (inspect only)

## Important Notes

1. Always verify the dynamic group exists before creating policies
2. Use the exact dynamic group name in policy statements
3. Ensure compartment IDs are correct
4. Start with minimal permissions and add more as needed
5. Some resources in documentation may not be valid in all regions