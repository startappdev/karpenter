# Creating Karpenter Service Account for OKE with FluxCD

## Overview

When deploying Karpenter with FluxCD on OKE, you need to create a service account with the proper permissions. This guide covers setting up the service account with OCI Instance Principal authentication.

## Prerequisites

- OKE cluster with Instance Principal enabled on worker nodes
- FluxCD installed and configured
- OCI CLI configured with appropriate permissions

## Step 1: Create OCI Dynamic Group and Policies

### 1.1 Create Dynamic Group for Karpenter Nodes

First, create a dynamic group that includes all nodes where Karpenter might run:

```bash
# Get the compartment OCID where your cluster resides
COMPARTMENT_ID="ocid1.compartment.oc1..xxxxxx"
CLUSTER_ID="ocid1.cluster.oc1.phx.xxxxxx"

# Create dynamic group via OCI Console or CLI
oci iam dynamic-group create \
  --name "karpenter-nodes-dg" \
  --description "Dynamic group for Karpenter nodes" \
  --matching-rule "ALL {instance.compartment.id = '$COMPARTMENT_ID', tag.oke-cluster-id.value = '$CLUSTER_ID'}"
```

### 1.2 Create IAM Policies

For creating IAM policies, please refer to the [OCI IAM Policy Setup Guide](./oci-iam-policy-setup.md) which provides:
- Step-by-step instructions for creating the dynamic group and policies
- Multiple policy options (basic, OKE-specific, flexible shapes)
- Troubleshooting for common errors
- Valid OCI resource types reference

## Step 2: Create Kubernetes Resources

### 2.1 Create Namespace

```yaml
# karpenter-namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: karpenter
  labels:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/managed-by: flux
```

### 2.2 Create Service Account

```yaml
# karpenter-service-account.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: karpenter
  namespace: karpenter
  labels:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/managed-by: flux
  annotations:
    # Add any required annotations for your setup
    eks.amazonaws.com/role-arn: "" # Not used for OKE, but kept for compatibility
automountServiceAccountToken: true
```

### 2.3 Create ClusterRole

```yaml
# karpenter-clusterrole.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: karpenter
  labels:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/managed-by: flux
rules:
  # Core Karpenter permissions
  - apiGroups: ["karpenter.sh"]
    resources: ["nodepools", "nodepools/status", "nodeclaims", "nodeclaims/status"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  
  # Node management
  - apiGroups: [""]
    resources: ["nodes", "nodes/status"]
    verbs: ["get", "list", "watch", "patch", "delete"]
  
  # Pod management
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
  
  # Pod eviction
  - apiGroups: [""]
    resources: ["pods/eviction"]
    verbs: ["create"]
  
  # Events
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
  
  # Scheduling
  - apiGroups: [""]
    resources: ["persistentvolumeclaims", "persistentvolumes"]
    verbs: ["get", "list", "watch"]
  
  - apiGroups: ["storage.k8s.io"]
    resources: ["storageclasses", "volumeattachments"]
    verbs: ["get", "list", "watch"]
  
  # ConfigMaps for leader election
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  
  # For pricing and instance type information
  - apiGroups: [""]
    resources: ["namespaces", "secrets"]
    verbs: ["get", "list", "watch"]
  
  # For daemon set calculations
  - apiGroups: ["apps"]
    resources: ["daemonsets", "deployments", "replicasets", "statefulsets"]
    verbs: ["get", "list", "watch"]
  
  # For PDB calculations
  - apiGroups: ["policy"]
    resources: ["poddisruptionbudgets"]
    verbs: ["get", "list", "watch"]
  
  # For nodepool validation webhook
  - apiGroups: ["admissionregistration.k8s.io"]
    resources: ["validatingwebhookconfigurations", "mutatingwebhookconfigurations"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
```

### 2.4 Create ClusterRoleBinding

```yaml
# karpenter-clusterrolebinding.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: karpenter
  labels:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/managed-by: flux
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: karpenter
subjects:
  - kind: ServiceAccount
    name: karpenter
    namespace: karpenter
```

### 2.5 Create Additional Role for Namespace Access

```yaml
# karpenter-role.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: karpenter
  namespace: karpenter
  labels:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/managed-by: flux
rules:
  # For storing OCI configuration
  - apiGroups: [""]
    resources: ["configmaps", "secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  
  # For leader election
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: karpenter
  namespace: karpenter
  labels:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/managed-by: flux
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: karpenter
subjects:
  - kind: ServiceAccount
    name: karpenter
    namespace: karpenter
```

## Step 3: FluxCD Configuration

### 3.1 Create Flux Kustomization

```yaml
# flux-system/karpenter-kustomization.yaml
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
```

### 3.2 Directory Structure

Create the following simplified directory structure in your Git repository:

```
karpenter/
├── kustomization.yaml
├── namespace.yaml
├── service-account.yaml
├── clusterrole.yaml
├── clusterrolebinding.yaml
├── role.yaml
├── oci-config-secret.yaml
└── release.yaml
```

### 3.3 Kustomization File

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
  - oci-config-secret.yaml
  - release.yaml

# Add any patches or configurations
patches:
  - patch: |-
      - op: add
        path: /metadata/annotations
        value:
          fluxcd.io/managed: "true"
    target:
      kind: ServiceAccount
      name: karpenter
```

### 3.4 OCI Configuration Secret

```yaml
# karpenter/oci-config-secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: oci-config
  namespace: karpenter
type: Opaque
stringData:
  config.yaml: |
    region: us-phoenix-1
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
```

### 3.5 Helm Release Configuration

```yaml
# karpenter/release.yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: karpenter
  namespace: karpenter
spec:
  interval: 10m
  chart:
    spec:
      chart: karpenter
      version: "1.5.0"  # Latest stable version
      sourceRef:
        kind: HelmRepository
        name: karpenter-oci
        namespace: flux-system
  # No dependsOn needed since all resources are in the same kustomization
  values:
    serviceAccount:
      create: false  # We already created it
      name: karpenter
    
    settings:
      cloudProvider: oci
      featureGates:
        dynamicProvisioning: true
    
    controller:
      env:
        - name: ENABLE_OCI_DYNAMIC_SHAPES
          value: "true"
        - name: OCI_CONFIG_PATH
          value: "/etc/oci/config.yaml"
      
      volumeMounts:
        - name: oci-config
          mountPath: /etc/oci
          readOnly: true
      
      volumes:
        - name: oci-config
          secret:
            secretName: oci-config
    
    webhook:
      enabled: true
      
    # Resource limits
    resources:
      limits:
        memory: 1Gi
      requests:
        cpu: 200m
        memory: 500Mi
    
    # Pod security context
    podSecurityContext:
      runAsNonRoot: true
      runAsUser: 65536
      runAsGroup: 65536
      fsGroup: 65536
      seccompProfile:
        type: RuntimeDefault
    
    # Container security context
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop:
          - ALL
      readOnlyRootFilesystem: true
      runAsNonRoot: true
```

### 3.6 HelmRepository Configuration

```yaml
# flux-system/karpenter-oci-helmrepo.yaml
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: HelmRepository
metadata:
  name: karpenter-oci
  namespace: flux-system
spec:
  interval: 30m
  type: oci
  url: oci://public.ecr.aws/karpenter
```

## Step 4: Deploy with FluxCD

### 4.1 Commit and Push

```bash
# Add all files to git
git add karpenter/
git commit -m "Add Karpenter service account and RBAC"
git push
```

### 4.2 Apply FluxCD Resources

```bash
# Apply the Kustomization
kubectl apply -f flux-system/karpenter-kustomization.yaml

# Watch the reconciliation
flux reconcile kustomization karpenter --with-source

# Check service account creation
kubectl -n karpenter get serviceaccount karpenter
kubectl -n karpenter describe serviceaccount karpenter

# Check HelmRelease
kubectl -n karpenter get helmrelease karpenter
kubectl -n karpenter describe helmrelease karpenter
```

### 4.3 Verify Permissions

```bash
# Check if all RBAC resources are created
kubectl get clusterrole karpenter
kubectl get clusterrolebinding karpenter
kubectl -n karpenter get role karpenter
kubectl -n karpenter get rolebinding karpenter

# Test permissions
kubectl auth can-i --as=system:serviceaccount:karpenter:karpenter \
  create nodeclaims.karpenter.sh --all-namespaces

kubectl auth can-i --as=system:serviceaccount:karpenter:karpenter \
  delete nodes --all-namespaces
```

## Step 5: Secrets Management with FluxCD

### 5.1 Using Sealed Secrets (Recommended)

If you're using Sealed Secrets with FluxCD, follow these steps to create the sealed secret:

#### 5.1.1 Install Sealed Secrets Controller

First, ensure you have the Sealed Secrets controller installed:

```bash
# Install Sealed Secrets controller if not already installed
kubectl apply -f https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.24.0/controller.yaml

# Install kubeseal CLI
# For macOS:
brew install kubeseal

# For Linux:
wget https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.24.0/kubeseal-0.24.0-linux-amd64.tar.gz
tar -xvzf kubeseal-0.24.0-linux-amd64.tar.gz
sudo install -m 755 kubeseal /usr/local/bin/kubeseal
```

#### 5.1.2 Create the OCI Configuration File

First, create a temporary file with your OCI configuration:

```bash
# Create a temporary config file with your actual values
cat > /tmp/oci-config.yaml <<EOF
region: us-phoenix-1
compartmentID: ocid1.compartment.oc1..xxxxxxxxxx
clusterID: ocid1.cluster.oc1.phx.xxxxxxxxxx
subnetIDs:
  - ocid1.subnet.oc1.phx.xxxxxxxxxx
  - ocid1.subnet.oc1.phx.yyyyyyyyyy
imageID: ocid1.image.oc1.phx.xxxxxxxxxx
instancePrincipal:
  enabled: true
defaultShapes:
  - VM.Standard.E4.Flex
  - VM.Standard.E5.Flex
EOF
```

#### 5.1.3 Create a Regular Secret First

Create a regular Kubernetes secret from the config file:

```bash
# Create the secret (but don't apply it)
kubectl create secret generic oci-config \
  --namespace karpenter \
  --from-file=config.yaml=/tmp/oci-config.yaml \
  --dry-run=client \
  -o yaml > /tmp/oci-config-secret.yaml

# Clean up the temporary config file
rm /tmp/oci-config.yaml
```

#### 5.1.4 Create the Sealed Secret

Convert the regular secret to a sealed secret:

```bash
# Create sealed secret
kubeseal \
  --format=yaml \
  --cert=<(kubectl get secret -n kube-system sealed-secrets-key -o jsonpath='{.data.tls\.crt}' | base64 -d) \
  < /tmp/oci-config-secret.yaml \
  > karpenter/oci-config-sealed-secret.yaml

# Clean up temporary files
rm /tmp/oci-config-secret.yaml
```

#### 5.1.5 Update Kustomization

Update your `karpenter/kustomization.yaml` to use the sealed secret:

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
  - oci-config-sealed-secret.yaml  # Changed from oci-config-secret.yaml
  - release.yaml
```

#### 5.1.6 The Sealed Secret File

Your sealed secret will look like this:

```yaml
# karpenter/oci-config-sealed-secret.yaml
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: oci-config
  namespace: karpenter
spec:
  encryptedData:
    config.yaml: AgCF3... # Long encrypted string
  template:
    metadata:
      name: oci-config
      namespace: karpenter
    type: Opaque
```

### 5.2 Alternative: Using SOPS

If using SOPS with FluxCD, here's how to create the encrypted secret:

#### 5.2.1 Create SOPS Configuration

First, create a `.sops.yaml` file in your repository root:

```yaml
# .sops.yaml
creation_rules:
  - path_regex: .*\.enc\.yaml$
    encrypted_regex: ^(data|stringData)$
    age: age1... # Your age public key
```

#### 5.2.2 Create the Secret File

Create the secret file that will be encrypted:

```yaml
# karpenter/oci-config-secret.enc.yaml
apiVersion: v1
kind: Secret
metadata:
  name: oci-config
  namespace: karpenter
type: Opaque
stringData:
  config.yaml: |
    region: us-phoenix-1
    compartmentID: ocid1.compartment.oc1..xxxxxxxxxx
    clusterID: ocid1.cluster.oc1.phx.xxxxxxxxxx
    subnetIDs:
      - ocid1.subnet.oc1.phx.xxxxxxxxxx
      - ocid1.subnet.oc1.phx.yyyyyyyyyy
    imageID: ocid1.image.oc1.phx.xxxxxxxxxx
    instancePrincipal:
      enabled: true
    defaultShapes:
      - VM.Standard.E4.Flex
      - VM.Standard.E5.Flex
```

#### 5.2.3 Encrypt the File

```bash
# Encrypt the file
sops -e -i karpenter/oci-config-secret.enc.yaml
```

#### 5.2.4 Update Kustomization for SOPS

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
  - oci-config-secret.enc.yaml  # SOPS encrypted file
  - release.yaml
```

### 5.3 Alternative: Using External Secrets Operator

If using External Secrets Operator with OCI Vault:

```yaml
# karpenter/oci-config-external-secret.yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: oci-config
  namespace: karpenter
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: oci-vault
    kind: SecretStore
  target:
    name: oci-config
    creationPolicy: Owner
  data:
    - secretKey: config.yaml
      remoteRef:
        key: karpenter-oci-config  # OCI Vault secret name
```

### 5.4 Verify Secret Creation

After applying your chosen secret method:

```bash
# Verify the secret exists
kubectl -n karpenter get secret oci-config

# Verify the secret contains the config.yaml key
kubectl -n karpenter get secret oci-config -o jsonpath='{.data}' | jq 'keys'

# Decode and verify the content (be careful not to expose secrets)
kubectl -n karpenter get secret oci-config -o jsonpath='{.data.config\.yaml}' | base64 -d
```

## Step 6: Post-Deployment Verification

### 6.1 Check Karpenter Logs

```bash
# Check if Karpenter can authenticate with OCI
kubectl -n karpenter logs -l app.kubernetes.io/name=karpenter \
  --tail=100 | grep -i "oci\|auth\|principal"

# Check for any permission errors
kubectl -n karpenter logs -l app.kubernetes.io/name=karpenter \
  --tail=100 | grep -i "error\|denied"
```

### 6.2 Test Instance Principal

```bash
# Exec into Karpenter pod and test OCI access
kubectl -n karpenter exec -it deployment/karpenter -- sh

# Inside the pod, test instance principal (if curl is available)
curl -H "Authorization: Bearer Oracle" \
  http://169.254.169.254/opc/v2/instance/

# Exit the pod
exit
```

## Troubleshooting

### Service Account Not Found

```bash
# Ensure namespace exists
kubectl get namespace karpenter

# Check if FluxCD created the resources
flux get kustomizations -A | grep karpenter

# Force reconciliation
flux reconcile kustomization karpenter --with-source
```

### Permission Denied Errors

```bash
# Verify ClusterRoleBinding
kubectl get clusterrolebinding karpenter -o yaml

# Check if service account token is mounted
kubectl -n karpenter get pod -l app.kubernetes.io/name=karpenter -o yaml | \
  grep -A5 serviceAccount
```

### Instance Principal Issues

```bash
# Verify dynamic group membership
# SSH to a node and run:
curl -s http://169.254.169.254/opc/v1/instance/ | jq '.definedTags'

# Check if the node has the correct tags
kubectl get nodes -o json | jq '.items[].metadata.labels'
```

## Summary

This setup ensures:
1. Proper service account with all required permissions
2. Integration with OKE's Instance Principal authentication
3. GitOps-friendly configuration with FluxCD
4. Security best practices with least privilege
5. Easy troubleshooting with proper labels and annotations