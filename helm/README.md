# Karpenter OCI Helm Charts

This directory contains Helm charts for deploying Karpenter with OCI provider support.

## Available Charts

### karpenter-oci

The main Helm chart for deploying Karpenter with Oracle Cloud Infrastructure (OCI) provider support on Oracle Kubernetes Engine (OKE).

- **Chart Version**: 0.1.0
- **App Version**: v1.0.0-oci
- **Location**: `./karpenter-oci/`

## Usage

See the [karpenter-oci README](./karpenter-oci/README.md) for detailed installation and configuration instructions.

## Quick Start

```bash
# Using existing sealed secret (recommended)
helm install karpenter ./karpenter-oci \
  --namespace karpenter \
  --create-namespace \
  --set settings.clusterName=my-oke-cluster \
  --set oci.existingSecret=karpenter-oci-config
```

## Development

To package the chart:

```bash
helm package ./karpenter-oci
```

To lint the chart:

```bash
helm lint ./karpenter-oci
```

## Contributing

Please ensure all charts pass linting before submitting pull requests.