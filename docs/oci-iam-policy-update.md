# OCI IAM Policy Update for Cross-Compartment Image Access

## Issue
Karpenter is getting a 404 NotAuthorizedOrNotFound error when trying to launch instances with image ID:
`ocid1.image.oc1.iad.aaaaaaaaug6zsgyvt52ybxifdiitp6ywrudnlbxsaclurlla3zkw4umchbha`

This is because the image is in Oracle's compartment and requires cross-compartment access permissions.

## Solution

Add the following statements to your existing karpenter-policy:

```bash
# Update the existing policy with additional statements
oci iam policy update \
  --policy-id ocid1.policy.oc1..aaaaaaaa4ydfyuoenejknpmmrmqziyjqe5bvjlyhhybs52slynw47vzc574q \
  --statements '[
    "Allow dynamic-group karpenter-nodes-dg to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to manage instance-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to use volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to inspect clusters in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to read cluster-node-pools in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to read cluster-workload-mappings in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to use vnics in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to use subnets in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq",
    "Allow dynamic-group karpenter-nodes-dg to inspect instance-images in tenancy",
    "Allow dynamic-group karpenter-nodes-dg to read instance-images in tenancy where target.image.id='"'"'ocid1.image.oc1.iad.aaaaaaaaug6zsgyvt52ybxifdiitp6ywrudnlbxsaclurlla3zkw4umchbha'"'"'",
    "Allow dynamic-group karpenter-nodes-dg to read app-catalog-listing in tenancy"
  ]'
```

## Key New Permissions

1. **`inspect instance-images in tenancy`**: Allows listing/searching for images across all compartments
2. **`read instance-images in tenancy where target.image.id='<image-ocid>'`**: Allows reading the specific image from any compartment
3. **`read app-catalog-listing in tenancy`**: Allows access to marketplace and partner images

## Alternative Approach: Use Custom Image in Your Compartment

If the policy update doesn't work or you prefer more control, you can:

1. Create a custom image in your compartment from an existing OKE node:
```bash
# From an existing OKE node, create a custom image
oci compute image create \
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
  --instance-id <existing-oke-node-instance-id> \
  --display-name "oke-ol8-custom-karpenter"
```

2. Update the OCINodeClass to use your custom image:
```yaml
apiVersion: karpenter.sh/v1alpha1
kind: OCINodeClass
metadata:
  name: default
  namespace: karpenter
spec:
  imageId: <your-custom-image-ocid>
  # ... rest of config
```

## Verification

After applying the policy update:

1. Wait 1-2 minutes for policy propagation
2. Check if Karpenter can now launch instances:
```bash
kubectl logs -n karpenter -l app.kubernetes.io/name=karpenter-oci -f | grep -i "image"
```

3. If still failing, check the exact error in OCI audit logs:
```bash
oci audit event list \
  --compartment-id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq \
  --start-time $(date -u -d '10 minutes ago' +%Y-%m-%dT%H:%M:%SZ) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%SZ) \
  --query "data[?contains(data.eventName, 'LaunchInstance')]"
```

## Additional Considerations

- Oracle-provided images are typically in the root compartment of the tenancy
- Some images may require accepting terms and conditions in the marketplace
- Platform images (like OKE-optimized images) should be accessible with the permissions above