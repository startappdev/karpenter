# Karpenter OCI Documentation

## Main Deployment Guide

For deploying Karpenter with OCI support using FluxCD, see:
- [Deploy Karpenter OCI with FluxCD](./deploy-karpenter-oci-fluxcd.md)

## Other Documentation

### OCI Setup
- [OCI IAM Policy Setup](./oci-iam-policy-setup.md) - IAM policies and dynamic groups configuration

### Technical Guides
- [Dynamic Node Provisioning Guide](./dynamic-node-provisioning-guide.md) - Deep dive into dynamic provisioning features
- [Rate Limiting and Cost Optimization](./rate-limiting-and-cost-optimization.md) - Recent improvements for OCI API rate limiting and cost optimization
- [Migrate from Cluster Autoscaler](./migrate-from-cluster-autoscaler.md) - Migration guide from CA to Karpenter

### Troubleshooting
- [Troubleshooting OCI](./troubleshooting-oci.md) - Common issues and solutions including rate limiting fixes

### Release Information
- [CHANGELOG.md](./CHANGELOG.md) - Detailed version history and improvements

### Legacy Documentation
The following files are kept for reference but have been superseded by the main deployment guide:
- `deploy-karpenter-oci-with-fluxcd.md` (old version)
- `fix-karpenter-flux-deployment.md` 
- `karpenter-deployment-status.md`
- `karpenter-service-account-oke.md`