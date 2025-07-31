# Complete Guide: Deploy Custom Karpenter with OCI Support using FluxCD

This guide provides comprehensive instructions for building and deploying our custom Karpenter implementation with Oracle Cloud Infrastructure (OCI) support.

## Prerequisites

✅ **Already Completed**:
- OCI IAM policies configured
- Dynamic group `karpenter-nodes-dg` created
- OKE cluster running
- FluxCD installed and configured
- Git repository set up

## Architecture Overview

We've built a custom Karpenter that includes:
- OCI cloud provider implementation (`pkg/providers/oci/`)
- Dynamic node provisioning for flexible shapes
- Bin packing algorithms for optimal resource utilization
- Integration with OKE (Oracle Kubernetes Engine)

## Step 1: Build the Custom Karpenter Image

### 1.1 Create Dockerfile

```dockerfile
# Dockerfile
FROM golang:1.21-alpine AS builder

# Install dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /workspace

# Copy go mod files
COPY go.mod go.mod
COPY go.sum go.sum

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the OCI provider binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o karpenter ./cmd/controller/main.go

# Runtime image
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/karpenter .
USER 65532:65532

ENTRYPOINT ["/karpenter"]
```

### 1.2 Build and Push Image

```bash
# Set your registry
export REGISTRY="your-registry.io/karpenter"
export VERSION="v1.0.0-oci"

# Build the image
docker build -t ${REGISTRY}/karpenter-oci:${VERSION} .

# Push to registry
docker push ${REGISTRY}/karpenter-oci:${VERSION}
```

## Step 2: Create Helm Chart for OCI Karpenter

### 2.1 Chart Structure

```bash
mkdir -p karpenter-oci-chart
cd karpenter-oci-chart

# Create chart structure
mkdir -p templates
```

### 2.2 Chart.yaml

```yaml
# karpenter-oci-chart/Chart.yaml
apiVersion: v2
name: karpenter-oci
description: Karpenter with OCI provider support
type: application
version: 1.0.0
appVersion: "v1.0.0-oci"
keywords:
  - karpenter
  - oci
  - oracle
  - autoscaling
  - kubernetes
```

### 2.3 Values.yaml

```yaml
# karpenter-oci-chart/values.yaml
replicaCount: 1

image:
  repository: your-registry.io/karpenter/karpenter-oci
  tag: v1.0.0-oci
  pullPolicy: IfNotPresent

serviceAccount:
  create: false
  name: karpenter

settings:
  # OCI-specific settings
  ociRegion: "us-phoenix-1"
  ociCompartmentId: ""
  ociClusterId: ""
  ociSubnetIds: []
  ociImageId: ""
  # Feature flags
  enableDynamicShapes: true
  dynamicProvisioning: true
  # General settings
  clusterName: ""
  logLevel: info

controller:
  env:
    - name: CLOUD_PROVIDER
      value: "oci"
    - name: ENABLE_OCI_DYNAMIC_SHAPES
      value: "true"
    - name: OCI_USE_INSTANCE_PRINCIPAL
      value: "true"
    - name: FEATURE_GATES
      value: "DynamicProvisioning=true"
    - name: KARPENTER_NAMESPACE
      valueFrom:
        fieldRef:
          fieldPath: metadata.namespace

resources:
  requests:
    cpu: 200m
    memory: 500Mi
  limits:
    cpu: 1
    memory: 1Gi

webhook:
  enabled: true
  port: 8443

metrics:
  enabled: true
  port: 8080

podSecurityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  fsGroup: 65532
  seccompProfile:
    type: RuntimeDefault

securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
  readOnlyRootFilesystem: true

nodeSelector:
  kubernetes.io/os: linux

tolerations:
  - key: CriticalAddonsOnly
    operator: Exists

# Volume mounts for OCI config
volumeMounts:
  - name: oci-config
    mountPath: /etc/oci
    readOnly: true

volumes:
  - name: oci-config
    secret:
      secretName: oci-config
```

### 2.4 Deployment Template

```yaml
# karpenter-oci-chart/templates/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: karpenter
  namespace: {{ .Release.Namespace }}
  labels:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/instance: {{ .Release.Name }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      app.kubernetes.io/name: karpenter
      app.kubernetes.io/instance: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: karpenter
        app.kubernetes.io/instance: {{ .Release.Name }}
    spec:
      serviceAccountName: {{ .Values.serviceAccount.name }}
      securityContext:
        {{- toYaml .Values.podSecurityContext | nindent 8 }}
      containers:
        - name: controller
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          args:
            - --v={{ .Values.settings.logLevel }}
            - --cluster-name={{ .Values.settings.clusterName }}
          env:
            {{- toYaml .Values.controller.env | nindent 12 }}
            - name: OCI_REGION
              value: {{ .Values.settings.ociRegion }}
            - name: OCI_COMPARTMENT_ID
              value: {{ .Values.settings.ociCompartmentId }}
            - name: OCI_CLUSTER_ID
              value: {{ .Values.settings.ociClusterId }}
            - name: OCI_SUBNET_IDS
              value: {{ join "," .Values.settings.ociSubnetIds }}
            - name: OCI_IMAGE_ID
              value: {{ .Values.settings.ociImageId }}
          ports:
            - name: http-metrics
              containerPort: {{ .Values.metrics.port }}
              protocol: TCP
            - name: webhook
              containerPort: {{ .Values.webhook.port }}
              protocol: TCP
          livenessProbe:
            httpGet:
              path: /healthz
              port: http-metrics
            initialDelaySeconds: 30
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /readyz
              port: http-metrics
            initialDelaySeconds: 10
            periodSeconds: 10
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
          securityContext:
            {{- toYaml .Values.securityContext | nindent 12 }}
          volumeMounts:
            {{- toYaml .Values.volumeMounts | nindent 12 }}
      volumes:
        {{- toYaml .Values.volumes | nindent 8 }}
      nodeSelector:
        {{- toYaml .Values.nodeSelector | nindent 8 }}
      tolerations:
        {{- toYaml .Values.tolerations | nindent 8 }}
```

### 2.5 Service Template

```yaml
# karpenter-oci-chart/templates/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: karpenter
  namespace: {{ .Release.Namespace }}
  labels:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/instance: {{ .Release.Name }}
spec:
  type: ClusterIP
  ports:
    - port: {{ .Values.webhook.port }}
      targetPort: webhook
      protocol: TCP
      name: webhook
    - port: {{ .Values.metrics.port }}
      targetPort: http-metrics
      protocol: TCP
      name: http-metrics
  selector:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/instance: {{ .Release.Name }}
```

### 2.6 Webhook Configuration

```yaml
# karpenter-oci-chart/templates/webhook.yaml
{{- if .Values.webhook.enabled }}
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: karpenter-validation-webhook
  labels:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/instance: {{ .Release.Name }}
webhooks:
  - name: validation.nodepools.karpenter.sh
    clientConfig:
      service:
        name: karpenter
        namespace: {{ .Release.Namespace }}
        path: "/validate-karpenter-sh-v1-nodepool"
    failurePolicy: Fail
    rules:
      - apiGroups: ["karpenter.sh"]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["nodepools"]
    admissionReviewVersions: ["v1", "v1beta1"]
    sideEffects: None
---
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: karpenter-mutation-webhook
  labels:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/instance: {{ .Release.Name }}
webhooks:
  - name: mutation.nodepools.karpenter.sh
    clientConfig:
      service:
        name: karpenter
        namespace: {{ .Release.Namespace }}
        path: "/mutate-karpenter-sh-v1-nodepool"
    failurePolicy: Fail
    rules:
      - apiGroups: ["karpenter.sh"]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["nodepools"]
    admissionReviewVersions: ["v1", "v1beta1"]
    sideEffects: None
{{- end }}
```

## Step 3: Package and Deploy with FluxCD

### 3.1 Package Helm Chart

```bash
# Package the chart
helm package karpenter-oci-chart

# Create a chart museum or use OCI registry
# For OCI registry:
helm push karpenter-oci-1.0.0.tgz oci://your-registry.io/charts
```

### 3.2 FluxCD Configuration

Create the following files in your Git repository:

#### HelmRepository

```yaml
# flux-system/karpenter-oci-helmrepo.yaml
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: HelmRepository
metadata:
  name: karpenter-oci-charts
  namespace: flux-system
spec:
  interval: 30m
  type: oci
  url: oci://your-registry.io/charts
```

#### Karpenter Namespace and RBAC

```yaml
# karpenter/namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: karpenter
---
# karpenter/service-account.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: karpenter
  namespace: karpenter
---
# karpenter/clusterrole.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: karpenter
rules:
  # Core Karpenter permissions
  - apiGroups: ["karpenter.sh"]
    resources: ["nodepools", "nodepools/status", "nodeclaims", "nodeclaims/status"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["nodes", "nodes/status"]
    verbs: ["get", "list", "watch", "patch", "delete"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods/eviction"]
    verbs: ["create"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
  - apiGroups: [""]
    resources: ["configmaps", "secrets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
# karpenter/clusterrolebinding.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: karpenter
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: karpenter
subjects:
  - kind: ServiceAccount
    name: karpenter
    namespace: karpenter
```

#### OCI Configuration Secret

```yaml
# karpenter/oci-config-sealed-secret.yaml
# Use kubeseal to create this from your actual config
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: oci-config
  namespace: karpenter
spec:
  encryptedData:
    config.yaml: # Encrypted OCI configuration
```

#### HelmRelease

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
      chart: karpenter-oci
      version: "1.0.0"
      sourceRef:
        kind: HelmRepository
        name: karpenter-oci-charts
        namespace: flux-system
  values:
    serviceAccount:
      create: false
      name: karpenter
    
    settings:
      clusterName: "OKE-ASH-STG-OKE"
      ociRegion: "us-ashburn-1"
      ociCompartmentId: "ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
      ociClusterId: "${OCI_CLUSTER_ID}"
      ociSubnetIds:
        - "${OCI_SUBNET_ID_1}"
        - "${OCI_SUBNET_ID_2}"
      ociImageId: "${OCI_NODE_IMAGE_ID}"
      enableDynamicShapes: true
      dynamicProvisioning: true
    
    controller:
      env:
        - name: CLOUD_PROVIDER
          value: "oci"
        - name: ENABLE_OCI_DYNAMIC_SHAPES
          value: "true"
        - name: OCI_USE_INSTANCE_PRINCIPAL
          value: "true"
```

#### Kustomization

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
  - oci-config-sealed-secret.yaml
  - release.yaml
```

### 3.3 Deploy with FluxCD

```bash
# Commit and push to Git
git add .
git commit -m "Deploy custom Karpenter with OCI support"
git push

# Apply FluxCD kustomization
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
    - apiVersion: helm.toolkit.fluxcd.io/v2beta1
      kind: HelmRelease
      name: karpenter
      namespace: karpenter
EOF

# Monitor deployment
flux get kustomizations -w
flux get helmreleases -A
kubectl get pods -n karpenter
```

## Step 4: Create NodePool with OCI Support

```yaml
# nodepool-oci.yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: oci-dynamic-nodepool
spec:
  template:
    metadata:
      labels:
        karpenter.sh/nodepool: oci-dynamic
    spec:
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["preemptible", "on-demand"]
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
      nodeClassRef:
        apiVersion: karpenter.oci/v1alpha1
        kind: OCINodeClass
        name: default
      taints: []
  
  # Dynamic provisioning configuration
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
  
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
    consolidateAfter: 30s
---
apiVersion: karpenter.oci/v1alpha1
kind: OCINodeClass
metadata:
  name: default
spec:
  # OCI-specific configuration
  userData: |
    #!/bin/bash
    # OKE node bootstrap script
```

## Step 5: Verify Deployment

```bash
# Check Karpenter is running
kubectl get pods -n karpenter

# Check logs
kubectl logs -n karpenter deployment/karpenter

# Check NodePool
kubectl get nodepool

# Test provisioning
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-provisioning
spec:
  replicas: 3
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: app
        image: nginx
        resources:
          requests:
            cpu: "2"
            memory: "4Gi"
EOF

# Watch nodes being created
kubectl get nodes -w
```

## Troubleshooting

### Build Issues
```bash
# Check Go dependencies
go mod tidy
go mod vendor

# Build locally
make build
```

### Deployment Issues
```bash
# Check Flux status
flux get all -A

# Check events
kubectl get events -n karpenter --sort-by='.lastTimestamp'

# Detailed logs
kubectl logs -n karpenter deployment/karpenter -f --tail=100
```

### OCI Authentication Issues
```bash
# Verify instance principal is working
kubectl exec -n karpenter deployment/karpenter -- curl -H "Authorization: Bearer Oracle" http://169.254.169.254/opc/v2/instance/
```

## Summary

This deployment includes:
1. ✅ Custom Karpenter build with OCI provider
2. ✅ Helm chart packaging
3. ✅ FluxCD GitOps deployment
4. ✅ Dynamic node provisioning for OCI flexible shapes
5. ✅ Instance principal authentication
6. ✅ Integration with OKE

The deployment is now ready to automatically provision OCI compute instances based on your workload requirements!