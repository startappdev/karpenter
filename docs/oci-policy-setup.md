# Setting Up OCI IAM Policies for Karpenter

## Correct Policy Format

The OCI CLI expects policy statements as a simple array of strings. Here are the correct commands:

### Option 1: Using a JSON Array File

Create a file with just the statements array:

```bash
# Create the policy statements file
cat > karpenter-policy-statements.json <<'EOF'
[
  "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use compute-capacity-reports in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to read compute-global-price-list in tenancy",
  "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use oke-clusters in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
]
EOF

# Create the policy
oci iam policy create \
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
  --name "karpenter-policy" \
  --description "Policy for Karpenter to manage instances" \
  --statements file://karpenter-policy-statements.json
```

### Option 2: Using Inline Statements

You can also provide the statements directly on the command line:

```bash
# Set your compartment ID
COMPARTMENT_ID="ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"

# Create the policy with inline statements
oci iam policy create \
  --compartment-id $COMPARTMENT_ID \
  --name "karpenter-policy" \
  --description "Policy for Karpenter to manage instances" \
  --statements '[
    "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id '"$COMPARTMENT_ID"'",
    "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id '"$COMPARTMENT_ID"'",
    "Allow dynamic-group karpenter-nodes-dg to use compute-capacity-reports in compartment id '"$COMPARTMENT_ID"'",
    "Allow dynamic-group karpenter-nodes-dg to read compute-global-price-list in tenancy",
    "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id '"$COMPARTMENT_ID"'",
    "Allow dynamic-group karpenter-nodes-dg to use oke-clusters in compartment id '"$COMPARTMENT_ID"'"
  ]'
```

### Option 3: Using a Cleaner Script

For better readability and maintenance, create a script:

```bash
#!/bin/bash

# Set variables
COMPARTMENT_ID="ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
DYNAMIC_GROUP_NAME="karpenter-nodes-dg"
POLICY_NAME="karpenter-policy"

# Create policy statements array
STATEMENTS=$(cat <<EOF
[
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to manage instances in compartment id ${COMPARTMENT_ID}",
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to use virtual-network-family in compartment id ${COMPARTMENT_ID}",
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to use compute-capacity-reports in compartment id ${COMPARTMENT_ID}",
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to read compute-global-price-list in tenancy",
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to manage volume-family in compartment id ${COMPARTMENT_ID}",
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to use oke-clusters in compartment id ${COMPARTMENT_ID}"
]
EOF
)

# Create the policy
oci iam policy create \
  --compartment-id "${COMPARTMENT_ID}" \
  --name "${POLICY_NAME}" \
  --description "Policy for Karpenter to manage instances" \
  --statements "${STATEMENTS}"
```

## Additional Policy Statements for Dynamic Provisioning

For the dynamic provisioning feature with flexible shapes, you might also need these additional permissions:

```bash
# Additional statements for flexible shapes
ADDITIONAL_STATEMENTS=$(cat <<EOF
[
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to read compute-capacity-reservations in compartment id ${COMPARTMENT_ID}",
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to manage compute-capacity-reservations in compartment id ${COMPARTMENT_ID}",
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to read instance-configurations in compartment id ${COMPARTMENT_ID}",
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to manage instance-configurations in compartment id ${COMPARTMENT_ID}",
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to read instance-pools in compartment id ${COMPARTMENT_ID}",
  "Allow dynamic-group ${DYNAMIC_GROUP_NAME} to manage instance-pools in compartment id ${COMPARTMENT_ID}"
]
EOF
)
```

## Verify the Policy

After creating the policy, verify it:

```bash
# List policies in the compartment
oci iam policy list \
  --compartment-id $COMPARTMENT_ID \
  --name "karpenter-policy"

# Get policy details
POLICY_ID=$(oci iam policy list \
  --compartment-id $COMPARTMENT_ID \
  --name "karpenter-policy" \
  --query 'data[0].id' \
  --raw-output)

oci iam policy get --policy-id $POLICY_ID
```

## Update Existing Policy

If you need to update an existing policy:

```bash
# Update policy statements
oci iam policy update \
  --policy-id $POLICY_ID \
  --statements '[
    "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id '"$COMPARTMENT_ID"'",
    "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id '"$COMPARTMENT_ID"'",
    "Allow dynamic-group karpenter-nodes-dg to use compute-capacity-reports in compartment id '"$COMPARTMENT_ID"'",
    "Allow dynamic-group karpenter-nodes-dg to read compute-global-price-list in tenancy",
    "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id '"$COMPARTMENT_ID"'",
    "Allow dynamic-group karpenter-nodes-dg to use oke-clusters in compartment id '"$COMPARTMENT_ID"'"
  ]'
```

## Delete Policy (if needed)

```bash
# Delete policy
oci iam policy delete --policy-id $POLICY_ID --force
```

## Important Notes

1. The `--statements` parameter expects a JSON array of strings, not a JSON object
2. Each statement must be a complete OCI IAM policy statement
3. The compartment ID in the statements must match actual compartment OCIDs
4. The dynamic group name must match the actual dynamic group you created
5. Make sure the dynamic group exists before creating the policy