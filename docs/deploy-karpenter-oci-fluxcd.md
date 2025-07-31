# Deploy Karpenter OCI with FluxCD

This guide provides complete instructions for deploying our custom Karpenter with OCI provider support to Oracle Kubernetes Engine (OKE) using FluxCD.

## Prerequisites

Before starting, ensure you have:

- ✅ OKE cluster running
- ✅ kubectl configured to access your cluster
- ✅ FluxCD installed in your cluster
- ✅ OCI CLI configured with appropriate credentials
- ✅ Git repository for FluxCD configurations
- ✅ Docker or similar container runtime for building images

## Architecture Overview

Our custom Karpenter implementation includes:
- OCI cloud provider for Oracle Cloud Infrastructure
- Dynamic node provisioning with flexible shapes support
- Bin packing algorithms for optimal resource utilization
- Native OKE integration with instance principal authentication

## Step 1: Build and Push Custom Karpenter Image

### 1.1 Build the Custom Image

```bash
# Clone this repository
git clone https://github.com/startappdev/karpenter.git
cd karpenter

# Set your container registry
export REGISTRY="your-registry.io"
export IMAGE_TAG="v1.0.0-oci"

# Create Dockerfile in the repository root
cat > Dockerfile << 'EOF'
FROM golang:1.21-alpine AS builder

# Install dependencies
RUN apk add --no-cache git make

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the controller with OCI provider
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o karpenter ./cmd/controller/main.go

# Runtime image
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/karpenter .
USER 65532:65532

ENTRYPOINT ["/karpenter"]
EOF

# Build and push the image
docker build -t ${REGISTRY}/karpenter-oci:${IMAGE_TAG} .
docker push ${REGISTRY}/karpenter-oci:${IMAGE_TAG}
```

## Step 2: Configure OCI IAM Policies

### 2.1 Create Dynamic Group

```bash
# Create dynamic group for Karpenter nodes
oci iam dynamic-group create \
  --name "karpenter-nodes-dg" \
  --description "Dynamic group for Karpenter managed nodes" \
  --matching-rule "All {instance.compartment.id = '<your-compartment-ocid>'}"
```

### 2.2 Create IAM Policy

```bash
# Create policy file
cat > karpenter-policy.json << 'EOF'
[
  "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id <your-compartment-ocid>",
  "Allow dynamic-group karpenter-nodes-dg to use virtual-network-family in compartment id <your-compartment-ocid>",
  "Allow dynamic-group karpenter-nodes-dg to manage volume-family in compartment id <your-compartment-ocid>",
  "Allow dynamic-group karpenter-nodes-dg to read cluster-family in compartment id <your-compartment-ocid>"
]
EOF

# Create the policy
oci iam policy create \
  --compartment-id <your-compartment-ocid> \
  --name "karpenter-policy" \
  --description "Policy for Karpenter to manage OKE nodes" \
  --statements file://karpenter-policy.json
```

## Step 3: Configure FluxCD

### 3.1 Create Karpenter Namespace

Create the following file in your FluxCD repository:

```yaml
# clusters/your-cluster/karpenter/namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: karpenter
```

### 3.2 Create GitRepository Source

```yaml
# clusters/your-cluster/karpenter/source.yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: karpenter
  namespace: flux-system
spec:
  interval: 1m
  ref:
    branch: main
  url: https://github.com/startappdev/karpenter.git
```

### 3.3 Create HelmRelease

```yaml
# clusters/your-cluster/karpenter/release.yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta2
kind: HelmRelease
metadata:
  name: karpenter
  namespace: karpenter
spec:
  interval: 10m
  chart:
    spec:
      chart: ./helm/karpenter-oci
      sourceRef:
        kind: GitRepository
        name: karpenter
        namespace: flux-system
  values:
    # Image configuration
    image:
      repository: your-registry.io/karpenter-oci
      tag: v1.0.0-oci
      
    # Service account (managed by Flux)
    serviceAccount:
      create: false
      name: karpenter
    
    # Karpenter settings
    settings:
      clusterName: "your-cluster-name"
      logLevel: info
      featureGates:
        dynamicProvisioning: true
        drift: true
    
    # OCI-specific configuration
    oci:
      # Reference the existing sealed secret containing OCI configuration
      existingSecret: "karpenter-oci-config"  # Name of your SealedSecret
      
      # These values are not needed when using existingSecret
      # They will be read from the secret instead
    
    # Resources
    resources:
      requests:
        cpu: 200m
        memory: 500Mi
      limits:
        memory: 1Gi
    
    # Security context
    podSecurityContext:
      runAsNonRoot: true
      runAsUser: 65532
      runAsGroup: 65532
      fsGroup: 65532
    
    # Node selector to run on OKE nodes
    nodeSelector:
      kubernetes.io/os: linux
      karpenter.sh/controller: "true"  # Optional: dedicate nodes
    
    # Tolerations
    tolerations:
      - key: CriticalAddonsOnly
        operator: Exists
```

### 3.4 Create SealedSecret for OCI Configuration

Create a sealed secret containing your OCI configuration:

```bash
# First create a regular secret
kubectl create secret generic karpenter-oci-config \
  --namespace=karpenter \
  --from-literal=region="us-ashburn-1" \
  --from-literal=compartmentId="ocid1.compartment.oc1..aaaaaaaa..." \
  --from-literal=clusterId="ocid1.cluster.oc1.iad.aaaaaaaa..." \
  --from-literal=subnetIds="ocid1.subnet.oc1.iad.aaaaaaaa...,ocid1.subnet.oc1.iad.bbbbbbb..." \
  --from-literal=imageId="ocid1.image.oc1.iad.aaaaaaaa..." \
  --from-literal=useInstancePrincipal="true" \
  --from-literal=enableDynamicShapes="true" \
  --dry-run=client -o yaml > oci-config-secret.yaml

# Seal the secret
kubeseal --format=yaml < oci-config-secret.yaml > sealed-oci-config.yaml

# Clean up temporary file
rm oci-config-secret.yaml
```

Place the sealed secret in your Git repository:
```yaml
# clusters/your-cluster/karpenter/sealed-oci-config.yaml
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: karpenter-oci-config
  namespace: karpenter
spec:
  encryptedData:
    region: "..." # Your sealed values
    compartmentId: "..."
    clusterId: "..."
    subnetIds: "..."
    imageId: "..."
    useInstancePrincipal: "..."
    enableDynamicShapes: "..."
```

### 3.5 Create ServiceAccount and RBAC

Since you mentioned you already have a SealedSecret for the ServiceAccount, ensure it includes:

```yaml
# clusters/your-cluster/karpenter/rbac.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: karpenter
  namespace: karpenter
---
# The ClusterRole and ClusterRoleBinding are created by the Helm chart
# Only create if you need additional permissions beyond the default
```

### 3.6 Create Kustomization

```yaml
# clusters/your-cluster/karpenter/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: karpenter

resources:
  - namespace.yaml
  - source.yaml
  - sealed-oci-config.yaml  # The SealedSecret with OCI configuration
  - rbac.yaml  # If not using Helm-managed RBAC
  - release.yaml
  # Your existing SealedSecret.yaml for ServiceAccount if needed
```

## Step 4: Deploy with FluxCD

```bash
# Commit and push your changes
git add .
git commit -m "Add Karpenter OCI deployment"
git push

# Reconcile Flux
flux reconcile source git flux-system
flux reconcile kustomization flux-system

# Monitor deployment
flux get helmreleases -n karpenter
kubectl get pods -n karpenter
```

## Step 5: Create NodePool Configuration

Once Karpenter is running, create a NodePool for dynamic provisioning:

```yaml
# nodepool-oci-dynamic.yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: oci-dynamic
spec:
  # Pod requirements to trigger provisioning
  template:
    metadata:
      labels:
        karpenter.sh/nodepool: oci-dynamic
    spec:
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["on-demand", "preemptible"]
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
      nodeClassRef:
        apiVersion: karpenter.oci/v1alpha1
        kind: OCINodeClass
        name: default
      
      # Instance user data for OKE
      userData: |
        #!/bin/bash
        # OKE node bootstrap will be handled automatically
  
  # Dynamic provisioning configuration
  dynamicProvisioning:
    enabled: true
    strategy: cost-optimized
    capacityType:
      preemptible: 70  # 70% preemptible instances
      onDemand: 30     # 30% on-demand instances
    constraints:
      minOCPUs: 1
      maxOCPUs: 64
      minMemoryGB: 2
      maxMemoryGB: 256
      allowedShapes:
        - VM.Standard.E4.Flex
        - VM.Standard.E5.Flex
        - VM.Standard3.Flex
    overhead:
      systemReservedCPU: "100m"
      systemReservedMemory: "500Mi"
    buffers:
      cpuHeadroomPercent: 10
      memoryHeadroomPercent: 10
  
  # Resource limits for this NodePool
  limits:
    cpu: "1000"
    memory: "4Ti"
  
  # Disruption settings
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
    consolidateAfter: 30s
    expireAfter: 30m
---
apiVersion: karpenter.oci/v1alpha1
kind: OCINodeClass
metadata:
  name: default
spec:
  # OCI-specific instance configuration
  userData: |
    #!/bin/bash
    # Add any custom initialization here
```

Apply the NodePool:

```bash
kubectl apply -f nodepool-oci-dynamic.yaml
```

## Step 6: Verify Deployment

### 6.1 Check Karpenter Status

```bash
# Check pods
kubectl get pods -n karpenter

# Check logs
kubectl logs -n karpenter deployment/karpenter-karpenter-oci

# Check NodePools
kubectl get nodepool
kubectl describe nodepool oci-dynamic
```

### 6.2 Test Auto-Scaling

Create a test deployment that requires resources:

```bash
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-scaling
spec:
  replicas: 5
  selector:
    matchLabels:
      app: test-scaling
  template:
    metadata:
      labels:
        app: test-scaling
    spec:
      containers:
      - name: app
        image: nginx
        resources:
          requests:
            cpu: "2"
            memory: "8Gi"
          limits:
            cpu: "2"
            memory: "8Gi"
EOF

# Watch nodes being created
kubectl get nodes -w
```

## Monitoring and Troubleshooting

### Check Karpenter Metrics

```bash
# Port-forward to access metrics
kubectl port-forward -n karpenter svc/karpenter-karpenter-oci 8080:8080

# Access metrics
curl http://localhost:8080/metrics
```

### Common Issues

1. **Instance Launch Failures**
   - Check IAM policies are correctly configured
   - Verify subnet IDs and availability
   - Check OCI quotas and limits

2. **Authentication Errors**
   - Ensure instance principal is enabled on OKE nodes
   - Verify dynamic group membership rules

3. **Provisioning Not Triggered**
   - Check NodePool requirements match pod specifications
   - Verify Karpenter controller is running
   - Check for webhook configuration issues

### Debug Commands

```bash
# Check Karpenter events
kubectl get events -n karpenter --sort-by='.lastTimestamp'

# Check NodeClaim status
kubectl get nodeclaim -o wide

# Detailed Karpenter logs
kubectl logs -n karpenter deployment/karpenter-karpenter-oci -f --tail=100
```

## Updating Karpenter

To update Karpenter configuration:

1. Update values in the HelmRelease
2. Commit and push changes
3. Flux will automatically reconcile

```bash
# Force reconciliation
flux reconcile helmrelease karpenter -n karpenter

# Check rollout status
kubectl rollout status deployment/karpenter-karpenter-oci -n karpenter
```

## Security Best Practices

1. **Never commit sensitive OCIDs** - Use environment variables or sealed secrets
2. **Use instance principal** authentication instead of API keys
3. **Implement least-privilege IAM policies**
4. **Enable audit logging** for all Karpenter actions
5. **Regularly update** the Karpenter image for security patches

## Summary

You now have Karpenter with OCI support deployed via FluxCD using a Git repository as the Helm chart source. The deployment will:

- ✅ Automatically provision OCI compute instances based on pod requirements
- ✅ Support flexible shapes with dynamic CPU/memory sizing
- ✅ Optimize costs with spot/preemptible instances
- ✅ Scale down unused nodes automatically
- ✅ Integrate natively with OKE using instance principal authentication

For updates, simply modify the values in your FluxCD repository and push the changes.