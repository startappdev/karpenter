#\!/bin/bash

# Check OKE tags on nodes
echo "Checking OKE tags on worker nodes..."
echo ""

# Get the node pool OCID for generic pool
NODE_POOL_ID=$(kubectl get nodes -o json | jq -r '.items[] | select(.metadata.labels.node_pool == "generic") | .spec.providerID' | head -1 | cut -d'.' -f5-)

echo "Nodes in 'generic' node pool:"
kubectl get nodes -L node_pool | grep generic

echo ""
echo "To use Option B (Specific Node Pool), the dynamic group rule would be:"
echo "ALL {instance.compartment.id = 'ocid1.compartment.oc1..aaaaaaaalr5oi5mfqpjedsdsyn3vxn2fh2bltqezqrmk4bi7gaq6i245qnkq', tag.oke-node-pool.value = '<node-pool-ocid>'}"
echo ""
echo "Note: You'll need to find the node pool OCID from OCI Console"
echo "Navigate to: Developer Services > Kubernetes Clusters (OKE) > Your Cluster > Node Pools"
