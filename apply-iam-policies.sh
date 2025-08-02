#!/bin/bash

# OCI IAM Policy Setup for Karpenter
# This script provides the commands to set up IAM policies for Karpenter

echo "Setting up OCI IAM policies for Karpenter..."
echo ""
echo "Karpenter is running on instance: ocid1.instance.oc1.iad.anuwcljt76bmgkacykz74nldg7lcxbozv4vj2s244udptlygxt56tf5ayd4q"
echo "Compartment ID: ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq"
echo ""
echo "Please run the following commands in OCI Console or using OCI CLI:"
echo ""
echo "1. Create a dynamic group for Karpenter:"
echo "   Name: karpenter-controller-group"
echo "   Matching Rule (choose one option):"
echo ""
echo "   Option A - For ALL OKE worker nodes in this cluster (recommended):"
echo "   ALL {instance.compartment.id = 'ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq', tag.oke-cluster.value = 'ocid1.cluster.oc1.iad.aaaaaaaazqquz2h5rspvdpafhnnmorehg2w3n2pveworuuovbcijgzk2jgda'}"
echo ""
echo "   Option B - For specific node pool (more restrictive):"
echo "   ALL {instance.compartment.id = 'ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq', tag.oke-node-pool.value = '<node-pool-ocid>'}"
echo ""
echo "   Option C - For labeled nodes (requires adding tags to nodes):"
echo "   ALL {instance.compartment.id = 'ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq', tag.karpenter-controller.value = 'true'}"
echo ""
echo "   Current instance (temporary for testing only):"
echo "   ALL {instance.id = 'ocid1.instance.oc1.iad.anuwcljt76bmgkacykz74nldg7lcxbozv4vj2s244udptlygxt56tf5ayd4q'}"
echo ""
echo "2. Create a policy in the compartment or root compartment:"
echo "   Name: karpenter-controller-policy"
echo "   Statements:"
cat << 'EOF'

Allow dynamic-group karpenter-controller-group to manage instances in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to manage instance-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to use volume-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to inspect clusters in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to read cluster-node-pools in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to read cluster-workload-mappings in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to use vnics in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to use subnets in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to use network-security-groups in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to read virtual-network-family in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to read images in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to read shapes in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to inspect availability-domains in tenancy
Allow dynamic-group karpenter-controller-group to read compute-capacity-reports in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq
Allow dynamic-group karpenter-controller-group to read dedicated-vm-hosts in compartment id ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq

EOF

echo ""
echo "3. Wait 1-2 minutes for policies to take effect"
echo ""
echo "4. Restart Karpenter pod to test:"
echo "   kubectl delete pod -n karpenter -l app.kubernetes.io/name=karpenter-oci"
echo ""
echo "Note: The 404 NotAuthorizedOrNotFound error indicates either:"
echo "- The dynamic group doesn't include the Karpenter instance"
echo "- The policies are not in effect yet"
echo "- The resource OCIDs are incorrect or in a different region"
echo ""
echo "To verify the dynamic group membership, you can use:"
echo "oci iam dynamic-group get --dynamic-group-id <dynamic-group-ocid>"