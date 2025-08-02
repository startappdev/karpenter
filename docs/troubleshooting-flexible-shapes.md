# Troubleshooting OCI Flexible Shapes with Karpenter

## Issue: 404 NotAuthorizedOrNotFound when launching flexible instances

### Symptoms
- Karpenter fails to launch instances with flexible shapes (e.g., VM.Standard.E4.Flex)
- Error message: "404 NotAuthorizedOrNotFound" when calling LaunchInstance API
- Instance launch request doesn't include ShapeConfig for flexible shapes

### Root Cause
The OCI API requires `ShapeConfig` to be included in the `LaunchInstanceDetails` when launching flexible shape instances. The ShapeConfig must specify:
- `Ocpus`: Number of OCPUs
- `MemoryInGBs`: Amount of memory in GB

### Solution
The fix was implemented in commit 8dfdb50:

```go
// Add shape config for flexible shapes
if isFlexibleShape(shape) {
    // Parse shape name to extract OCPUs and memory
    // Format: VM.Standard.E4.Flex-1-16 (1 OCPU, 16GB memory)
    var ocpus, memory int32 = 1, 16
    if _, err := fmt.Sscanf(shape, "VM.Standard.E4.Flex-%d-%d", &ocpus, &memory); err == nil {
        request.LaunchInstanceDetails.ShapeConfig = &core.LaunchInstanceShapeConfigDetails{
            Ocpus:       common.Float32(float32(ocpus)),
            MemoryInGBs: common.Float32(float32(memory)),
        }
        logger.Info("added shape config for flexible shape", 
            "ocpus", ocpus, "memory", memory)
    }
}
```

### Verification
After deploying the fix, you should see in the logs:
1. "added shape config for flexible shape" message
2. The API request body should include `shapeConfig` with the OCPU and memory values
3. Instances should launch successfully

### Related Issues
- Custom OKE image was created to avoid cross-compartment image access issues
- IAM policies were updated to include instance-images permissions
- Dynamic group membership was verified for instance principal authentication