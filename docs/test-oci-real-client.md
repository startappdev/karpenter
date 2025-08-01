# Testing the Real OCI Client Implementation

This document describes how to test the newly implemented OCI client for Karpenter that actually provisions nodes in Oracle Cloud Infrastructure.

## Overview

The OCI client has been updated from a mock implementation to a real client that:
- Uses the official Oracle Cloud Infrastructure Go SDK v65
- Supports instance principal authentication for secure, credential-free operation
- Actually launches compute instances in OCI with proper cloud-init configuration
- Handles the full instance lifecycle (launch, query, terminate)
- Includes proper error handling and retry logic

## Prerequisites

1. **OCI Environment Setup**:
   - An OKE (Oracle Kubernetes Engine) cluster running
   - Karpenter deployed with instance principal authentication configured
   - Dynamic groups and policies set up for Karpenter to manage compute instances

2. **Required IAM Policies**:
   ```
   # Allow Karpenter to manage compute instances
   allow dynamic-group karpenter-nodes to manage compute-management-family in compartment <compartment-name>
   allow dynamic-group karpenter-nodes to manage virtual-network-family in compartment <compartment-name>
   allow dynamic-group karpenter-nodes to read oke-clusters in compartment <compartment-name>
   ```

## Cost-Optimized Testing

To minimize costs during testing, use the smallest available instance shapes:

### Recommended Test Shapes

1. **VM.Standard.E4.Flex** (Flexible shape - most cost-effective)
   - Minimum: 1 OCPU, 1 GB memory
   - Cost: ~$0.01 per OCPU hour
   - Perfect for testing basic functionality

2. **VM.Standard.E3.Flex** (Alternative flexible shape)
   - Minimum: 1 OCPU, 1 GB memory
   - Similar pricing to E4.Flex

3. **VM.Standard2.1** (Fixed shape)
   - 1 OCPU, 15 GB memory
   - Higher cost but simpler configuration

### Example NodePool Configuration for Cost Optimization

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: test-pool-cost-optimized
spec:
  # Template configuration
  template:
    metadata:
      labels:
        karpenter.sh/test: "true"
    spec:
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["on-demand"]  # Use "preemptible" for even lower costs
        - key: node.kubernetes.io/instance-type
          operator: In
          values: 
            - "VM.Standard.E4.Flex"  # Flexible shape
      nodeClassRef:
        apiVersion: karpenter.sh/v1alpha1
        kind: OCINodeClass
        name: default
      # Resource limits for testing
      resources:
        requests:
          cpu: "1"      # Minimum 1 OCPU
          memory: "1Gi" # Minimum 1 GB
  # Disruption settings
  disruption:
    ttlSecondsAfterEmpty: 30  # Quick cleanup for cost savings
  # Limits
  limits:
    cpu: "10"     # Limit total CPUs for cost control
    memory: "10Gi"
---
apiVersion: karpenter.sh/v1alpha1
kind: OCINodeClass
metadata:
  name: default
spec:
  # OCI-specific configuration
  compartmentId: "ocid1.compartment.oc1..xxxxx"
  imageId: "ocid1.image.oc1.region.xxxxx"  # Use latest OKE node image
  subnetIds:
    - "ocid1.subnet.oc1.region.xxxxx"
  # Shape configuration for flexible instances
  shapeConfig:
    ocpus: 1        # Minimum OCPUs
    memoryInGBs: 1  # Minimum memory
```

## Testing Steps

1. **Deploy the Updated Karpenter**:
   ```bash
   # Build the new image
   make build
   
   # Push to registry
   docker push ghcr.io/your-org/karpenter:latest
   
   # Update deployment
   kubectl set image deployment/karpenter karpenter=ghcr.io/your-org/karpenter:latest -n karpenter
   ```

2. **Create a Test Workload**:
   ```yaml
   apiVersion: v1
   kind: Pod
   metadata:
     name: test-pod
   spec:
     terminationGracePeriodSeconds: 0
     containers:
     - name: pause
       image: k8s.gcr.io/pause:3.9
       resources:
         requests:
           cpu: "500m"
           memory: "512Mi"
     nodeSelector:
       karpenter.sh/test: "true"
   ```

3. **Monitor Instance Creation**:
   ```bash
   # Watch Karpenter logs
   kubectl logs -f deployment/karpenter -n karpenter
   
   # Check OCI console for new instances
   # Or use OCI CLI:
   oci compute instance list --compartment-id <compartment-id> --lifecycle-state RUNNING
   ```

4. **Verify Node Joins Cluster**:
   ```bash
   # Watch for new nodes
   kubectl get nodes -w
   
   # Check node details
   kubectl describe node <new-node-name>
   ```

5. **Test Cleanup**:
   ```bash
   # Delete the test pod
   kubectl delete pod test-pod
   
   # Karpenter should terminate the instance after ttlSecondsAfterEmpty
   ```

## Troubleshooting

### Instance Fails to Launch

1. **Check Karpenter logs**:
   ```bash
   kubectl logs deployment/karpenter -n karpenter | grep -i error
   ```

2. **Verify IAM policies**:
   - Ensure dynamic group includes Karpenter instances
   - Check policy statements allow compute management

3. **Validate configuration**:
   - Correct compartment ID
   - Valid subnet IDs
   - Available image ID for the region

### Node Fails to Join Cluster

1. **Check cloud-init logs** (SSH to instance):
   ```bash
   sudo cat /var/log/cloud-init-output.log
   sudo journalctl -u cloud-init
   ```

2. **Verify OKE metadata**:
   ```bash
   curl -H "Authorization: Bearer Oracle" http://169.254.169.254/opc/v2/instance/metadata/
   ```

3. **Check kubelet logs**:
   ```bash
   sudo journalctl -u kubelet -f
   ```

## Cost Monitoring

To track costs during testing:

1. **Use OCI Cost Analysis**:
   - Filter by compartment
   - Group by service (Compute)
   - Set date range to testing period

2. **Set Budget Alerts**:
   - Create a budget for the test compartment
   - Set alert at $10 to avoid surprises

3. **Clean Up Resources**:
   ```bash
   # Delete all test pods
   kubectl delete pods -l karpenter.sh/test=true
   
   # Delete NodePool to ensure cleanup
   kubectl delete nodepool test-pool-cost-optimized
   ```

## Performance Metrics

The real OCI client includes:
- Retry logic with exponential backoff
- Concurrent API call handling
- Efficient instance state polling

Expected timings:
- Instance launch: 2-3 minutes
- Node ready in cluster: 4-5 minutes total
- Instance termination: < 1 minute

## Next Steps

After successful testing:
1. Implement more sophisticated availability domain selection
2. Add subnet selection based on capacity
3. Implement instance pool support for better performance
4. Add comprehensive metrics and monitoring

## Security Considerations

The implementation uses instance principal authentication, which:
- Eliminates the need for API keys in configuration
- Automatically rotates credentials
- Provides secure access to OCI APIs
- Requires proper dynamic group and policy configuration

Ensure your dynamic groups are properly scoped to limit permissions to only what Karpenter needs.