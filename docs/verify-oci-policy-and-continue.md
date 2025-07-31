# Verify OCI Policy and Continue Karpenter Deployment

## Step 1: Verify the Policy Was Created Successfully

Check if the policy was created with the corrected statements:

```bash
# List policies in your compartment to find the karpenter-policy
oci iam policy list \
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
  --query "data[?name=='karpenter-policy'].{name:name, id:id}" \
  --output table

# Get the policy ID and view its statements
POLICY_ID=$(oci iam policy list \
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
  --name "karpenter-policy" \
  --query 'data[0].id' \
  --raw-output)

# View the policy statements
oci iam policy get --policy-id $POLICY_ID --query 'data.statements'
```

The statements should include:
- `"Allow dynamic-group karpenter-nodes-dg to read compute-capacity-reports..."`

## Step 2: Continue with Karpenter Deployment

Now that the OCI IAM policies are in place, continue with the FluxCD deployment:

### 2.1 Create the Karpenter Directory Structure

```bash
# Create the karpenter directory in your Git repository
mkdir -p karpenter

# Copy the YAML files from the documentation
cd karpenter
```

### 2.2 Create the Required YAML Files

Create all the YAML files as described in the [karpenter-service-account-oke.md](./karpenter-service-account-oke.md) documentation:

1. `namespace.yaml` - Karpenter namespace
2. `service-account.yaml` - Service account
3. `clusterrole.yaml` - ClusterRole with permissions
4. `clusterrolebinding.yaml` - ClusterRoleBinding
5. `role.yaml` - Role and RoleBinding for namespace access
6. `oci-config-sealed-secret.yaml` - Sealed secret with OCI configuration
7. `release.yaml` - HelmRelease for Karpenter

### 2.3 Create the OCI Configuration

Replace the placeholder values with your actual OCI resources:

```bash
# Set your OCI values
export OCI_REGION="us-phoenix-1"
export OCI_COMPARTMENT_ID="ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
export OCI_CLUSTER_ID="<your-cluster-ocid>"
export OCI_SUBNET_ID_1="<your-subnet-ocid-1>"
export OCI_SUBNET_ID_2="<your-subnet-ocid-2>"
export OCI_NODE_IMAGE_ID="<your-oke-node-image-ocid>"

# Create the config file
cat > /tmp/oci-config.yaml <<EOF
region: ${OCI_REGION}
compartmentID: ${OCI_COMPARTMENT_ID}
clusterID: ${OCI_CLUSTER_ID}
subnetIDs:
  - ${OCI_SUBNET_ID_1}
  - ${OCI_SUBNET_ID_2}
imageID: ${OCI_NODE_IMAGE_ID}
instancePrincipal:
  enabled: true
defaultShapes:
  - VM.Standard.E4.Flex
  - VM.Standard.E5.Flex
EOF
```

### 2.4 Create the Sealed Secret

Follow the instructions in [karpenter-service-account-oke.md](./karpenter-service-account-oke.md#51-using-sealed-secrets-recommended) to create the sealed secret.

### 2.5 Create Kustomization File

```yaml
# karpenter/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: karpenter

resources:
  - namespace.yaml
  - service-account.yaml
  - clusterrole.yaml
  - clusterrolebinding.yaml
  - role.yaml
  - oci-config-sealed-secret.yaml
  - release.yaml
```

### 2.6 Commit and Push

```bash
# Add all files
git add karpenter/
git commit -m "Add Karpenter deployment configuration for FluxCD"
git push
```

### 2.7 Deploy with FluxCD

```bash
# Apply the Flux kustomization
kubectl apply -f - <<EOF
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: karpenter
  namespace: flux-system
spec:
  interval: 10m
  path: "./karpenter"
  prune: true
  sourceRef:
    kind: GitRepository
    name: flux-system
  healthChecks:
    - apiVersion: v1
      kind: ServiceAccount
      name: karpenter
      namespace: karpenter
    - apiVersion: helm.toolkit.fluxcd.io/v2beta1
      kind: HelmRelease
      name: karpenter
      namespace: karpenter
EOF

# Force reconciliation
flux reconcile kustomization karpenter --with-source
```

## Step 3: Verify Karpenter Installation

### 3.1 Check Deployment Status

```bash
# Check if namespace was created
kubectl get namespace karpenter

# Check if service account was created
kubectl -n karpenter get serviceaccount karpenter

# Check HelmRelease status
kubectl -n karpenter get helmrelease karpenter

# Check if Karpenter pods are running
kubectl -n karpenter get pods
```

### 3.2 Check Karpenter Logs

```bash
# View Karpenter logs
kubectl -n karpenter logs -l app.kubernetes.io/name=karpenter --tail=50

# Check for OCI authentication success
kubectl -n karpenter logs -l app.kubernetes.io/name=karpenter | grep -i "instance principal"
```

## Step 4: Create Your First NodePool

Once Karpenter is running, create a NodePool with dynamic provisioning:

```bash
kubectl apply -f - <<EOF
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: default-nodepool
spec:
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
    ttlSecondsAfterEmpty: 30
  template:
    metadata:
      labels:
        karpenter.sh/nodepool: default-nodepool
    spec:
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["on-demand", "preemptible"]
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
      nodeClassRef:
        group: karpenter.k8s.oci/v1
        kind: OCINodeClass
        name: default
      taints: []
  dynamicProvisioning:
    enabled: true
    strategy: best-fit
    capacityType:
      preemptible: 80
      onDemand: 20
    constraints:
      minOCPUs: 1
      maxOCPUs: 64
      minMemoryGB: 2
      maxMemoryGB: 256
      allowedShapes:
        - VM.Standard.E4.Flex
        - VM.Standard.E5.Flex
    overhead:
      systemReservedCPU: "100m"
      systemReservedMemory: "500Mi"
    buffers:
      cpuHeadroomPercent: 10
      memoryHeadroomPercent: 10
  limits:
    cpu: "1000"
    memory: "4Ti"
EOF
```

## Step 5: Test Dynamic Provisioning

Deploy a test workload to verify Karpenter provisions nodes dynamically:

```bash
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dynamic-test
spec:
  replicas: 3
  selector:
    matchLabels:
      app: dynamic-test
  template:
    metadata:
      labels:
        app: dynamic-test
    spec:
      containers:
      - name: app
        image: nginx
        resources:
          requests:
            cpu: "2.5"
            memory: "5Gi"
          limits:
            cpu: "2.5"
            memory: "5Gi"
EOF
```

Watch Karpenter provision custom-sized nodes:

```bash
# Watch nodes being created
kubectl get nodes -w

# Check NodeClaim creation
kubectl get nodeclaims -w

# View Karpenter events
kubectl get events --field-selector involvedObject.kind=NodePool -w
```

## Troubleshooting

If you encounter issues:

1. **Policy Issues**: 
   ```bash
   # Verify the dynamic group exists
   oci iam dynamic-group list --compartment-id <tenancy-ocid> --all | grep karpenter-nodes-dg
   ```

2. **Instance Principal Issues**:
   ```bash
   # Check if nodes have the correct tags
   kubectl get nodes -o jsonpath='{.items[*].metadata.labels}' | jq
   ```

3. **Karpenter Not Starting**:
   ```bash
   # Check pod events
   kubectl -n karpenter describe pod -l app.kubernetes.io/name=karpenter
   
   # Check if secret was created
   kubectl -n karpenter get secret oci-config
   ```

## Next Steps

1. Review the [dynamic-node-provisioning-guide.md](./dynamic-node-provisioning-guide.md) for detailed configuration options
2. If migrating from cluster autoscaler, follow [migrate-from-cluster-autoscaler.md](./migrate-from-cluster-autoscaler.md)
3. Use the CLI analyzer to monitor efficiency:
   ```bash
   kubectl karpenter analyze-dynamic-provisioning --cost-report
   ```