# Karpenter OCI Helm Chart

This Helm chart deploys Karpenter with OCI (Oracle Cloud Infrastructure) provider support for Oracle Kubernetes Engine (OKE).

## Prerequisites

- Kubernetes 1.25+
- Helm 3.0+
- OKE cluster with instance principal enabled
- OCI IAM policies configured for node management

## Installation

### Using FluxCD (Recommended)

See the main deployment guide: [Deploy Karpenter OCI with FluxCD](../../docs/deploy-karpenter-oci-fluxcd.md)

### Using Helm CLI

```bash
# Clone the repository
git clone https://github.com/startappdev/karpenter.git
cd karpenter

# Install the chart
helm install karpenter ./helm/karpenter-oci \
  --namespace karpenter \
  --create-namespace \
  --set image.repository=your-registry.io/karpenter-oci \
  --set image.tag=v1.0.0-oci \
  --set settings.clusterName=your-cluster-name \
  --set oci.region=us-ashburn-1 \
  --set oci.compartmentId=ocid1.compartment.oc1..xxxxx \
  --set oci.clusterId=ocid1.cluster.oc1.iad.xxxxx \
  --set-string oci.subnetIds[0]=ocid1.subnet.oc1.iad.xxxxx \
  --set-string oci.subnetIds[1]=ocid1.subnet.oc1.iad.xxxxx
```

## Configuration

See [values.yaml](values.yaml) for the full list of configurable parameters.

### Required Values

| Parameter | Description | Example |
|-----------|-------------|---------|
| `image.repository` | Container image repository | `your-registry.io/karpenter-oci` |
| `settings.clusterName` | OKE cluster name | `my-oke-cluster` |
| `oci.region` | OCI region | `us-ashburn-1` |
| `oci.compartmentId` | OCI compartment OCID | `ocid1.compartment.oc1..xxxxx` |
| `oci.clusterId` | OKE cluster OCID | `ocid1.cluster.oc1.iad.xxxxx` |
| `oci.subnetIds` | List of subnet OCIDs | `["ocid1.subnet.oc1.iad.xxxxx"]` |

### Common Configuration Examples

#### Enable Debug Logging

```yaml
settings:
  logLevel: debug
```

#### Configure Resource Limits

```yaml
resources:
  requests:
    cpu: 500m
    memory: 1Gi
  limits:
    cpu: 1000m
    memory: 2Gi
```

#### Enable Prometheus Metrics

```yaml
metrics:
  serviceMonitor:
    enabled: true
    additionalLabels:
      prometheus: kube-prometheus
```

## Upgrading

```bash
helm upgrade karpenter ./helm/karpenter-oci \
  --namespace karpenter \
  --reuse-values \
  --set image.tag=v1.1.0-oci
```

## Uninstallation

```bash
helm uninstall karpenter --namespace karpenter
```

## Values Reference

See the [values.yaml](values.yaml) file for a complete list of configurable values.