# Karpenter OCI GitOps Deployment Guide

## Overview

This guide documents the GitOps deployment architecture for Karpenter NodePool manifests and the resolution of deployment issues encountered during the OCI 429 rate limiting solution implementation.

## GitOps Architecture

### Repository Structure
```
karpenter/
├── manifests/
│   ├── nodepool-production.yaml    # Production workload NodePool
│   ├── nodepool-default.yaml       # Default/test workload NodePool  
│   ├── nodepool-kafka.yaml         # Kafka workload NodePool
│   ├── service-account.yaml        # Karpenter service account
│   └── kustomization.yaml          # Manifest selection for OCI compatibility
├── helm/karpenter-oci/             # Helm chart for Karpenter controller
└── karpenter-nodepools-kustomization.yaml  # Flux kustomization definition
```

### Deployment Flow
```
Git Repository (start-io branch)
    ↓
Flux GitRepository Source
    ↓  
Flux Kustomization (karpenter-nodepools)
    ↓
Kubernetes NodePool Resources
    ↓
Karpenter Controller
```

## Flux Configuration

### GitRepository Source
```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: karpenter
  namespace: flux-system
spec:
  interval: 1m
  ref:
    branch: start-io
  url: https://github.com/startappdev/karpenter
```

### Kustomization Resource
```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: karpenter-nodepools
  namespace: flux-system
spec:
  interval: 5m
  path: ./manifests
  prune: true
  sourceRef:
    kind: GitRepository
    name: karpenter
    namespace: flux-system
  targetNamespace: karpenter
  timeout: 2m
```

## Deployment Issues Resolved

### Issue 1: EC2NodeClass Compatibility
**Problem**: Kustomization failed with "no matches for kind EC2NodeClass" error
**Root Cause**: Manifests directory contained AWS-specific NodePool definitions
**Solution**: Created `manifests/kustomization.yaml` to include only OCI-compatible resources:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
- nodepool-default.yaml
- nodepool-kafka.yaml  
- nodepool-production.yaml
- service-account.yaml

namespace: karpenter
```

### Issue 2: Missing Service Account
**Problem**: Kustomization failed looking for `service-account.yaml`
**Root Cause**: Service account managed by Helm chart, not available as standalone manifest
**Solution**: Created dedicated `manifests/service-account.yaml`:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: karpenter
  namespace: karpenter
  labels:
    app.kubernetes.io/name: karpenter
    app.kubernetes.io/component: controller
automountServiceAccountToken: true
```

### Issue 3: Branch Mismatch
**Problem**: Existing kustomization referenced master branch instead of start-io
**Root Cause**: Legacy kustomization `oci-ash-stg-apps` used wrong Git source
**Solution**: Created dedicated kustomization targeting correct repository and branch

## Best Practices

### GitOps Compliance
- ✅ All changes via Git commits
- ✅ Flux reconciliation for deployments
- ✅ No manual `kubectl apply` commands
- ✅ Version controlled configuration

### Kustomization Design
- Separate kustomizations for different resource types
- Explicit resource inclusion (avoid wildcards)
- Proper namespace targeting
- Cloud provider compatibility validation

### Deployment Validation
```bash
# Check kustomization status
flux get kustomizations -A

# Verify resource application
kubectl get nodepool -n karpenter -o yaml

# Monitor deployment logs
flux logs --kind=Kustomization --name=karpenter-nodepools
```

## Troubleshooting Commands

```bash
# Force reconciliation
flux reconcile kustomization karpenter-nodepools -n flux-system

# Check Git source status
flux get sources git -A

# View kustomization events
kubectl describe kustomization karpenter-nodepools -n flux-system

# Validate resource deployment
kubectl get nodepool -n karpenter -o json | jq '.items[] | {name: .metadata.name, disruption: .spec.disruption}'
```

## Success Metrics

### Deployment Success
- ✅ Kustomization status: Applied revision start-io@de5aca8a
- ✅ All NodePools updated with disruption configuration
- ✅ Zero manual interventions required

### Configuration Validation
- ✅ production-pool: consolidateAfter=Never, budgets="0" 
- ✅ default-pool: consolidateAfter=Never, budgets="0"
- ✅ kafka-pool: consolidateAfter=Never, budgets="0"

This GitOps architecture enables reliable, version-controlled deployment of Karpenter NodePool configurations while maintaining cloud provider compatibility and operational best practices.