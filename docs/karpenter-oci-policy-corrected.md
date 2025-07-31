# Correct OCI IAM Policy Statements for Karpenter

## Updated Policy Statements

The `compute-capacity-reports` resource doesn't support the "use" verb. Here are the corrected policy statements:

### Create the Corrected Policy File

```bash
cat > karpenter-policy-statements.json <<'EOF'
[
  "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to read compute-capacity-reports in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to read compute-global-price-list in tenancy",
  "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use oke-clusters in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
]
EOF
```

### Create the Policy

```bash
oci iam policy create \
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
  --name "karpenter-policy" \
  --description "Policy for Karpenter to manage instances" \
  --statements file://karpenter-policy-statements.json
```

## Complete Karpenter Policy for OCI

For full Karpenter functionality with dynamic provisioning, here's a comprehensive policy:

```bash
cat > karpenter-policy-complete.json <<'EOF'
[
  "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to read compute-management-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to read instance-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to inspect compartments in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use oke-clusters in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
]
EOF
```

## Minimal Working Policy

If you want to start with a minimal policy and add permissions as needed:

```bash
cat > karpenter-policy-minimal.json <<'EOF'
[
  "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
]
EOF
```

## Policy for Flexible Shapes (Dynamic Provisioning)

For dynamic provisioning with flexible shapes, you'll need these additional permissions:

```bash
cat > karpenter-policy-flexible-shapes.json <<'EOF'
[
  "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to read instance-configurations in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to read compute-management-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to use oke-clusters in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
  "Allow dynamic-group karpenter-nodes-dg to inspect shapes in tenancy"
]
EOF
```

## Create the Policy

```bash
# Use the corrected minimal policy first
oci iam policy create \
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
  --name "karpenter-policy" \
  --description "Policy for Karpenter to manage instances" \
  --statements file://karpenter-policy-minimal.json
```

## Verify Valid Resource Types

To see what resource types are valid in your tenancy:

```bash
# This command requires appropriate permissions
oci iam resource-type list --all
```

## Common Policy Verbs and Resources

Here's a reference for valid OCI IAM policy verbs and resources:

### Valid Verbs:
- `inspect`: Read metadata
- `read`: Read resources and metadata
- `use`: Read and use existing resources (cannot create/delete)
- `manage`: Full control (create, update, delete)

### Valid Compute Resources:
- `instances`: Virtual machines
- `instance-family`: All instance-related resources
- `instance-configurations`: Instance configuration resources
- `instance-pools`: Instance pool resources
- `compute-management-family`: Compute management resources
- `shapes`: Available shapes information
- `volume-family`: Block storage volumes
- `virtual-network-family`: VCN, subnets, security lists, etc.

## Troubleshooting

If you continue to get errors:

1. **Check the dynamic group exists**:
   ```bash
   oci iam dynamic-group list --compartment-id <tenancy-ocid> --all
   ```

2. **Verify compartment ID**:
   ```bash
   oci iam compartment get --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
   ```

3. **Start with minimal permissions** and add more as needed based on Karpenter logs

4. **Check OCI documentation** for the latest policy syntax and resource types