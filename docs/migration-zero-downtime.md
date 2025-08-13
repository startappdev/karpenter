# Zero-Downtime Migration from Terraform Node Pools to Karpenter

## 🎯 **Migration Overview**

This guide provides a **simple, safe approach** for migrating from Terraform-managed OKE node pools to Karpenter management with **absolute zero downtime** and **no pod disruption**. The strategy focuses on **node adoption** where Karpenter takes over management of existing Terraform nodes without provisioning new nodes or moving any pods.

### **🔒 Zero-Downtime Guarantees**
- ✅ **No StatefulSet disruption** - Kafka, RabbitMQ, Redis remain untouched
- ✅ **No pod movements** - Existing pods stay on exact same nodes
- ✅ **No new node provisioning** - Karpenter adopts existing Terraform nodes
- ✅ **No service interruption** - All services remain completely available
- ✅ **Simple label adoption** - Just label existing nodes for Karpenter management
- ✅ **Instant rollback** - Remove labels to revert to Terraform control

---

## 🔧 **Prerequisites**

Before starting the migration, ensure you have:

- **Oracle Kubernetes Engine (OKE)** cluster running
- **OCI credentials** configured (CLI or Instance Principal)
- **FluxCD v2.x** installed and managing your cluster
- **GitOps repository** where Kubernetes manifests are stored
- **`flux` CLI** installed locally for manual reconciliation
- **Terraform** managing your current node pools

---

## 🏗️ **Pre-Migration Assessment**

### **1. Inventory Current State**

First, document your existing Terraform-managed infrastructure:

```bash
# 1. Document existing node pools
kubectl get nodes -o custom-columns="NAME:.metadata.name,POOL:.metadata.labels.oci\.oraclecloud\.com/node-pool" --show-labels

# 2. Document StatefulSet distribution
kubectl get statefulsets -A -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name,REPLICAS:.spec.replicas,READY:.status.readyReplicas"

# 3. Document pod-to-node mapping for critical workloads  
kubectl get pods -A -l app.kubernetes.io/component=kafka -o wide
kubectl get pods -A -l app=rabbitmq -o wide
kubectl get pods -A -l app=redis -o wide

# 4. Document current node pool specifications
terraform show | grep -A 20 "node_pool"
```

**📋 Create Migration Inventory:**
```yaml
# migration-inventory.yaml
current_state:
  terraform_node_pools:
    - name: "kafka-pool"
      shape: "VM.Standard.E4.Flex"  
      node_count: 3
      ocpu_per_node: 8
      memory_per_node: 64
      critical_workloads: ["kafka-cluster", "zookeeper"]
      
    - name: "rabbitmq-pool"  
      shape: "VM.Standard.E4.Flex"
      node_count: 2
      ocpu_per_node: 4
      memory_per_node: 32
      critical_workloads: ["rabbitmq-cluster"]
      
    - name: "redis-pool"
      shape: "VM.Standard.E5.Flex" 
      node_count: 2
      ocpu_per_node: 2
      memory_per_node: 16
      critical_workloads: ["redis-cluster"]
      
  stateful_workloads:
    kafka:
      replicas: 3
      storage_per_replica: "100Gi"
      current_nodes: ["node-1", "node-2", "node-3"]
    rabbitmq:
      replicas: 2  
      storage_per_replica: "20Gi"
      current_nodes: ["node-4", "node-5"]
    redis:
      replicas: 2
      storage_per_replica: "10Gi"
      current_nodes: ["node-6", "node-7"]
```

### **2. Validate Prerequisites**

```bash
# 1. Verify OCI credentials and permissions
oci compute instance list --compartment-id $OCI_COMPARTMENT_ID --limit 1

# 2. Verify Kubernetes cluster health
kubectl get nodes --show-labels
kubectl get pods -A | grep -E "(Pending|Failed|CrashLoopBackOff)"

# 3. Verify StatefulSet health
kubectl get statefulsets -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"/"}{.metadata.name}{": "}{.status.readyReplicas}{"/"}{.spec.replicas}{"\n"}{end}'

# 4. Check PVC status (critical for StatefulSets)  
kubectl get pvc -A --show-labels
```

**⚠️ Pre-Migration Checklist:**
- [ ] All StatefulSets are healthy (ready replicas = desired replicas)
- [ ] No failed or pending pods in critical namespaces
- [ ] All PVCs are bound and accessible
- [ ] OCI credentials configured for Karpenter
- [ ] Terraform state is clean and up-to-date
- [ ] Full backup of critical data completed

---

## 🛡️ **Migration Strategy: Node Adoption**

### **Phase 1: Install Karpenter Without Disruption**

The key to zero-downtime migration is installing Karpenter, then using labels to adopt existing Terraform nodes without any provisioning or pod movement.

#### **Step 1.1: Install Karpenter (Non-Disruptive)**

```yaml
# Add to your GitOps repository: karpenter/karpenter-release.yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: karpenter-oci
  namespace: karpenter
spec:
  interval: 15m
  chart:
    spec:
      chart: karpenter-oci
      version: "0.1.57"  # Use latest version
      sourceRef:
        kind: HelmRepository
        name: karpenter-oci
        namespace: karpenter
  values:
    oci:
      region: us-ashburn-1
      compartmentId: "ocid1.compartment.oc1..."
      clusterId: "ocid1.cluster.oc1..."
      existingSecret: "oci-config"
```

```bash
# Commit and deploy via GitOps
git add karpenter/karpenter-release.yaml
git commit -m "Install Karpenter OCI Provider for migration"
git push origin main

# Trigger FluxCD reconciliation
flux reconcile source git flux-system
flux reconcile helmrelease karpenter-oci -n karpenter

# Verify Karpenter is running
kubectl get deployment -n karpenter
kubectl get nodepools -A  # Should be empty initially
```

#### **Step 1.2: Create NodePools That Match Existing Nodes**

Create Karpenter NodePools that **exactly match** your existing Terraform nodes so Karpenter can adopt them:

```yaml
# kafka-nodepool.yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: kafka-pool-karpenter
  namespace: karpenter
spec:
  # Match current Terraform capacity
  limits:
    cpu: "48"     # 3 nodes × 8 OCPUs × 2 = 48 vCPUs  
    memory: "192Gi" # 3 nodes × 64GB = 192GB
    
  template:
    metadata:
      labels:
        # Match existing Terraform pool labels EXACTLY
        node_pool: kafka
        workload-type: kafka
        oci.oraclecloud.com/node-pool: kafka-pool-karpenter
      annotations:
        migration.karpenter.sh/source: "terraform-handoff"
    spec:
      # Mirror existing node configuration
      requirements:
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
        - key: kubernetes.io/os
          operator: In  
          values: ["linux"]
        # Match current shape configuration
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["VM.Standard.E4.Flex"]
          
      taints:
        # Copy existing taints to ensure workload compatibility
        - key: node_pool
          value: kafka
          effect: NoSchedule
          
      nodeClassRef:
        group: karpenter.sh
        kind: OCINodeClass
        name: default
        
      expireAfter: 24h   # Standard expiration
      
  # Enable normal disruption (safe since we're not moving existing pods)
  disruption:
    consolidationPolicy: WhenEmpty
    consolidateAfter: "30s"
---
# Create similar configurations for other pools
```

```bash
# Commit NodePool manifests to your GitOps repository
git add kafka-nodepool.yaml rabbitmq-nodepool.yaml redis-nodepool.yaml
git commit -m "Add Karpenter NodePools for migration handoff"
git push origin main

# Wait for FluxCD to reconcile
flux reconcile source git flux-system
flux reconcile kustomization karpenter

# Verify NodePools are deployed
kubectl get nodepools -A
# No immediate provisioning will happen unless pods become unschedulable
```

---

## 🔄 **Phase 2: Node Adoption**

### **Step 2.1: Label Existing Nodes for Karpenter**

Simply label your existing Terraform nodes so Karpenter adopts them without any changes:

```bash
# 1. List current Terraform nodes by pool
kubectl get nodes -l oci.oraclecloud.com/node-pool=kafka-pool --show-labels
kubectl get nodes -l oci.oraclecloud.com/node-pool=rabbitmq-pool --show-labels  
kubectl get nodes -l oci.oraclecloud.com/node-pool=redis-pool --show-labels

# 2. Verify all StatefulSets are healthy before proceeding
kubectl get statefulsets -A -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name,READY:.status.readyReplicas,DESIRED:.spec.replicas"

# 3. Label existing nodes for Karpenter adoption (START WITH ONE POOL)
kubectl label nodes -l oci.oraclecloud.com/node-pool=kafka-pool karpenter.sh/nodepool=kafka-pool-karpenter

# 4. Verify labels were applied
kubectl get nodes -l karpenter.sh/nodepool=kafka-pool-karpenter --show-labels
```

### **Step 2.2: Verify Karpenter Adoption**

Confirm that Karpenter has successfully adopted the labeled nodes:

```bash
# 1. Check Karpenter controller logs for node adoption
kubectl logs -n karpenter deployment/karpenter-karpenter-oci --tail=20

# 2. Verify NodePool status shows adopted nodes  
kubectl describe nodepool kafka-pool-karpenter -n karpenter

# 3. Confirm nodes are now managed by Karpenter (SAME NODES, NO NEW ONES)
kubectl get nodes -l karpenter.sh/nodepool=kafka-pool-karpenter -o wide

# 4. Verify all pods are still running on the exact same nodes (NO MOVEMENT)
kubectl get pods -A -o wide | grep -E "(kafka|rabbitmq|redis)"

# 5. Most importantly: Check that NO new nodes were provisioned
kubectl get events -n karpenter | grep -i provision  # Should be empty for adoption
```

### **Step 2.3: Adopt Additional Node Pools**

Once the first pool adoption is successful, repeat for remaining pools:

```bash
# 1. Label remaining node pools for Karpenter adoption
kubectl label nodes -l oci.oraclecloud.com/node-pool=rabbitmq-pool karpenter.sh/nodepool=rabbitmq-pool-karpenter
kubectl label nodes -l oci.oraclecloud.com/node-pool=redis-pool karpenter.sh/nodepool=redis-pool-karpenter

# 2. Verify all pools are now managed by Karpenter
kubectl get nodes -l karpenter.sh/nodepool --show-labels

# 3. Confirm no new nodes were created - just adoption
kubectl get nodes -o wide | wc -l  # Same count as before migration
```

#### **📋 Real Example - Node Adoption**
```bash
# Before labeling: 3 Terraform-only nodes
kubectl get nodes --show-labels | grep kafka-pool
# kafka-pool-node-1   Ready   oci.oraclecloud.com/node-pool=kafka-pool
# kafka-pool-node-2   Ready   oci.oraclecloud.com/node-pool=kafka-pool  
# kafka-pool-node-3   Ready   oci.oraclecloud.com/node-pool=kafka-pool

# After labeling: Same 3 nodes, now ALSO managed by Karpenter
kubectl get nodes -l karpenter.sh/nodepool=kafka-pool-karpenter --show-labels
# kafka-pool-node-1   Ready   oci.oraclecloud.com/node-pool=kafka-pool,karpenter.sh/nodepool=kafka-pool-karpenter
# kafka-pool-node-2   Ready   oci.oraclecloud.com/node-pool=kafka-pool,karpenter.sh/nodepool=kafka-pool-karpenter
# kafka-pool-node-3   Ready   oci.oraclecloud.com/node-pool=kafka-pool,karpenter.sh/nodepool=kafka-pool-karpenter
```

#### **🔍 Monitor the Process**

```bash
# Monitor Karpenter's response to the Terraform scale-down
kubectl get events -n karpenter --sort-by='.lastTimestamp' | tail -10

# Check if new Karpenter nodes are being provisioned
kubectl get nodes -l karpenter.sh/nodepool -w

# Verify no pods are stuck in pending state
kubectl get pods -A --field-selector=status.phase=Pending

# Check Karpenter logs for provisioning activity
kubectl logs -n karpenter deployment/karpenter-karpenter-oci --tail=20
```

---

## 🎯 **Phase 3: Remove Terraform Management (Optional)**

### **Step 3.1: Understanding Dual Management**

At this point, your nodes are managed by **both** Terraform (infrastructure) and Karpenter (lifecycle). This is actually a **valid end state** and many organizations stop here. However, if you want to remove Terraform management entirely:

```bash
# Option 1: Keep dual management (RECOMMENDED)
# - Terraform manages the infrastructure (node pools)
# - Karpenter manages the lifecycle (scaling, replacement)
# - This is the safest approach with easy rollback

# Option 2: Full Karpenter management (ADVANCED)
# Remove Terraform node pools entirely, but this will TERMINATE existing nodes
# and force Karpenter to provision new ones (defeats the purpose of our zero-disruption approach)
```

### **Step 3.2: Recommended Approach - Keep Dual Management**

The **recommended approach** is to **keep both Terraform and Karpenter** managing the nodes:

```bash
# Verify current state - dual management working
kubectl get nodes -o custom-columns="NAME:.metadata.name,TERRAFORM:.metadata.labels.oci\.oraclecloud\.com/node-pool,KARPENTER:.metadata.labels.karpenter\.sh/nodepool"

# This shows nodes with BOTH labels - perfectly valid and safe
echo "✅ Migration complete! Nodes are managed by both Terraform (infrastructure) and Karpenter (lifecycle)"
```

### **Step 3.2: Final Terraform Pool Removal**

Once you're confident in Karpenter's management, remove the remaining Terraform pools entirely:

```hcl
# terraform/node-pools.tf - FINAL REMOVAL
# Comment out or remove entire node pool resources:

# resource "oci_containerengine_node_pool" "kafka_pool" {
#   # Entire resource commented out or deleted
# }

# resource "oci_containerengine_node_pool" "rabbitmq_pool" {
#   # Entire resource commented out or deleted  
# }

# resource "oci_containerengine_node_pool" "redis_pool" {
#   # Entire resource commented out or deleted
# }
```

```bash
# Apply final Terraform changes
terraform plan  # Review carefully - should show resource destruction
terraform apply

# Monitor node termination
kubectl get nodes --watch

# Verify all workloads remain healthy
kubectl get statefulsets -A
kubectl get pods -A | grep -E "(Pending|Failed)"
```

**✅ Validation Checklist:**
- [ ] All Terraform nodes gracefully terminated
- [ ] Karpenter nodes provisioned as replacements
- [ ] All StatefulSets healthy and running
- [ ] No failed or pending pods
- [ ] All services accessible and functional

---

## 📊 **Phase 4: Post-Migration Optimization**

### **Step 4.1: Enable Advanced Karpenter Features**

Now that migration is complete, enable cost optimization features:

```yaml
# kafka-nodepool-optimized.yaml
# Enable flexible shape selection for cost optimization
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: kafka-pool-karpenter
spec:
  limits:
    cpu: "100"     # Allow growth capacity
    memory: "400Gi"
    
  template:
    spec:
      # Remove static instance type constraints for cost optimization
      requirements:
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
        # Remove: node.kubernetes.io/instance-type constraint
        
      expireAfter: 24h  # Enable regular node cycling for cost optimization
      
  disruption:
    consolidationPolicy: WhenEmpty
    consolidateAfter: "30s"  # Enable aggressive consolidation
```

```bash
# Update NodePool configurations in GitOps repository
git add kafka-nodepool-optimized.yaml
git commit -m "Enable cost optimization features post-migration"
git push origin main

# Trigger FluxCD reconciliation
flux reconcile source git flux-system
flux reconcile kustomization karpenter
```

### **Step 4.2: Final Validation**

```bash
# Comprehensive final validation
echo "=== Migration Validation Report ==="

# 1. Node distribution
echo "Current nodes:"
kubectl get nodes -o custom-columns="NAME:.metadata.name,POOL:.metadata.labels.oci\.oraclecloud\.com/node-pool,STATUS:.status.conditions[?(@.type=='Ready')].status"

# 2. StatefulSet health
echo -e "\nStatefulSet status:"
kubectl get statefulsets -A -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name,READY:.status.readyReplicas,DESIRED:.spec.replicas"

# 3. Critical workload validation
echo -e "\nCritical workload validation:"
kubectl exec -n kafka kafka-cluster-0 -- kafka-topics --bootstrap-server localhost:9092 --list | head -5
kubectl exec -n rabbitmq rabbitmq-cluster-0 -- rabbitmqctl node_health_check
kubectl exec -n redis redis-cluster-0 -- redis-cli info replication

---

## 📊 **Phase 4: Infrastructure Transition**

### **Step 4.1: Prepare Terraform for Node Pool Removal**

**Only proceed if all validations in Phase 3 passed successfully.**

```bash
# 1. Final validation before Terraform changes
kubectl get nodes --show-labels | grep -E "(terraform|karpenter)"
kubectl get statefulsets -A -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name,READY:.status.readyReplicas,DESIRED:.spec.replicas" | grep -v "READY"

# 2. Create infrastructure backup
terraform plan -out=pre-migration.tfplan
terraform show pre-migration.tfplan > pre-migration-state.txt

# 3. Document current OCI instances for rollback reference
oci compute instance list --compartment-id $OCI_COMPARTMENT_ID --display-name "kafka-pool*" > pre-migration-instances.json
```

### **Step 4.2: Gradual Terraform Node Pool Scaling Down**

**Scale down Terraform node pools gradually, not immediately removing them:**

```hcl
# terraform/node-pools.tf - GRADUAL SCALE DOWN
resource "oci_containerengine_node_pool" "kafka_pool" {
  # ... existing configuration ...
  
  node_config_details {
    placement_configs {
      availability_domain = var.availability_domain
      subnet_id          = var.private_subnet_id
    }
    # GRADUAL: Reduce size by 1 node first
    size = 2  # Was 3, now 2 (Karpenter has 1 node to compensate)
  }
  
  # Add lifecycle to prevent accidental destruction
  lifecycle {
    prevent_destroy = true
  }
}
```

```bash
# Apply gradual scale down
terraform plan -target=oci_containerengine_node_pool.kafka_pool
terraform apply -target=oci_containerengine_node_pool.kafka_pool

# Monitor the process
kubectl get nodes -l oci.oraclecloud.com/node-pool=kafka-pool --watch &
kubectl get pods -A | grep -E "(Pending|Unschedulable)" --watch &

# If pods become unschedulable, Karpenter will provision new nodes automatically
kubectl get events -n karpenter --watch
```

### **Step 4.3: Increase Karpenter Capacity**

As Terraform nodes are reduced, increase Karpenter capacity proportionally:

```yaml
# kafka-nodepool-expanded.yaml
apiVersion: karpenter.sh/v1  
kind: NodePool
metadata:
  name: kafka-pool-karpenter
  namespace: karpenter
spec:
  # Increase limits to accommodate workload from scaled-down Terraform pool
  limits:
    cpu: "64"     # Increased from 48 to accommodate additional workload  
    memory: "256Gi" # Increased from 192Gi to accommodate additional workload
    
  # ... rest of configuration unchanged ...
```

```bash
# Apply increased capacity
kubectl apply -f kafka-nodepool-expanded.yaml

# Monitor Karpenter scaling up if needed (due to resource pressure)
kubectl get nodes -l karpenter.sh/nodepool --watch
kubectl get events -n karpenter | grep -i provision
```

### **Step 4.4: Complete Infrastructure Transition**

**Only proceed after confirming system stability for 24+ hours:**

```bash
# Final validation before completing migration
kubectl get pods -A | grep -E "(Pending|Failed|CrashLoopBackOff)"  # Should be empty
kubectl get statefulsets -A | grep -v "READY"  # Should be empty

# Verify all critical workloads are healthy
kubectl exec -n kafka kafka-cluster-0 -- kafka-topics --bootstrap-server localhost:9092 --list
kubectl exec -n rabbitmq rabbitmq-cluster-0 -- rabbitmqctl cluster_status  
kubectl exec -n redis redis-cluster-0 -- redis-cli ping
```

**Complete the Terraform node pool removal:**

```hcl
# terraform/node-pools.tf - FINAL REMOVAL
# Comment out or remove the entire node pool resource
# resource "oci_containerengine_node_pool" "kafka_pool" {
#   # Remove entire block
# }
```

```bash
# Final Terraform apply to remove old node pool
terraform plan  # Review carefully
terraform apply

# Verify old nodes are being terminated gracefully
kubectl get nodes --show-labels | grep kafka-pool-terraform
```

---

## 🔧 **Phase 5: Post-Migration Optimization**

### **Step 5.1: Enable Karpenter Advanced Features**

Once migration is complete and stable, enable advanced Karpenter features:

```yaml
# kafka-nodepool-final.yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: kafka-pool-karpenter
  namespace: karpenter
spec:
  limits:
    cpu: "100"     # Allow some growth capacity
    memory: "400Gi"
    
  template:
    metadata:
      labels:
        node_pool: kafka
        workload-type: kafka
        migration.karpenter.sh/status: "complete"
    spec:
      # Enable flexible shapes for cost optimization
      requirements:
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
        - key: kubernetes.io/os
          operator: In
          values: ["linux"]
        # Remove static instance type - enable dynamic selection
        # - key: node.kubernetes.io/instance-type  # REMOVED
        #   operator: In
        #   values: ["VM.Standard.E4.Flex"]
          
      taints:
        - key: node_pool
          value: kafka
          effect: NoSchedule
          
      nodeClassRef:
        group: karpenter.sh
        kind: OCINodeClass
        name: default
        
      expireAfter: 24h  # Reduce expiration for better cost optimization
      
  # Enable gentle disruption for optimization
  disruption:
    consolidationPolicy: WhenEmpty
    consolidateAfter: "30s"  # Allow consolidation after 30s
    budgets:
      - nodes: "10%"  # Allow 10% of nodes to be disrupted for optimization
```

### **Step 5.2: Monitor and Validate Final State**

```bash
# Comprehensive final validation
echo "=== Migration Validation Report ==="

# 1. Node distribution
echo "Current nodes:"
kubectl get nodes -o custom-columns="NAME:.metadata.name,POOL:.metadata.labels.oci\.oraclecloud\.com/node-pool,STATUS:.status.conditions[?(@.type=='Ready')].status"

# 2. StatefulSet health
echo -e "\nStatefulSet status:"
kubectl get statefulsets -A -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name,READY:.status.readyReplicas,DESIRED:.spec.replicas"

# 3. Critical workload validation
echo -e "\nCritical workload validation:"
kubectl exec -n kafka kafka-cluster-0 -- kafka-topics --bootstrap-server localhost:9092 --list | head -5
kubectl exec -n rabbitmq rabbitmq-cluster-0 -- rabbitmqctl node_health_check
kubectl exec -n redis redis-cluster-0 -- redis-cli info replication

# 4. Resource utilization  
echo -e "\nResource utilization:"
kubectl top nodes

# 5. Cost analysis
echo -e "\nCost optimization (if dynamic shapes enabled):"
kubectl get nodes -l karpenter.sh/nodepool -o jsonpath='{range .items[*]}{.metadata.name}{": "}{.metadata.labels.node\.kubernetes\.io/instance-type}{"\n"}{end}'
```

### **Step 5.3: Cleanup Migration Artifacts**

```bash
# Remove migration-specific labels and annotations
kubectl label nodes -l migration.karpenter.sh/protected=true migration.karpenter.sh/protected-
kubectl annotate statefulset -n kafka kafka-cluster migration.karpenter.sh/protected-
kubectl annotate statefulset -n rabbitmq rabbitmq-cluster migration.karpenter.sh/protected-
kubectl annotate statefulset -n redis redis-cluster migration.karpenter.sh/protected-

# Remove test workloads
kubectl delete pod kafka-test-replica -n kafka
kubectl delete deployment migration-test -n default

# Clean up migration files
rm -f kafka-nodepool-shadow.yaml kafka-nodepool-active.yaml kafka-test-replica.yaml test-migration.yaml
```

---

## 🚨 **Emergency Rollback Procedures**

### **Quick Rollback (If Issues Detected)**

```bash
# EMERGENCY: Immediate rollback to Terraform management

# 1. Scale down Karpenter NodePools to zero immediately
kubectl patch nodepool kafka-pool-karpenter -n karpenter --type='merge' -p='{"spec":{"limits":{"cpu":"0","memory":"0Gi"}}}'

# 2. Restore Terraform node pools
terraform apply -target=oci_containerengine_node_pool.kafka_pool -var="node_count=3"  # Restore original size

# 3. Verify StatefulSets remain healthy
kubectl get statefulsets -A -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name,READY:.status.readyReplicas,DESIRED:.spec.replicas"

# 4. Monitor system stability
kubectl get pods -A | grep -E "(Pending|Failed|CrashLoopBackOff)"
```

### **Complete Rollback (If Migration Must Be Reversed)**

```bash
# COMPLETE ROLLBACK: Remove Karpenter entirely

# 1. Ensure all workloads are on Terraform-managed nodes
kubectl get pods -A -o wide | grep -v $(kubectl get nodes -l oci.oraclecloud.com/node-pool | tail -n +2 | awk '{print $1}' | tr '\n' '|' | sed 's/|$//')

# 2. Delete all Karpenter NodePools
kubectl delete nodepools -A --all

# 3. Uninstall Karpenter
helm uninstall karpenter -n karpenter

# 4. Restore original Terraform configuration
git checkout terraform/node-pools.tf
terraform apply

# 5. Validate complete rollback
kubectl get nodes --show-labels | grep -v karpenter
```

---

## 📊 **Migration Monitoring and Alerts**

### **Critical Metrics to Monitor**

```bash
# Create monitoring script for migration
cat > monitor-migration.sh << 'EOF'
#!/bin/bash

echo "=== Migration Status Report $(date) ==="

# 1. Node health
echo "Total nodes: $(kubectl get nodes --no-headers | wc -l)"
echo "Terraform nodes: $(kubectl get nodes -l oci.oraclecloud.com/node-pool --no-headers 2>/dev/null | grep -v karpenter | wc -l)"
echo "Karpenter nodes: $(kubectl get nodes -l karpenter.sh/nodepool --no-headers 2>/dev/null | wc -l)"

# 2. StatefulSet health
FAILED_STATEFULSETS=$(kubectl get statefulsets -A -o json | jq -r '.items[] | select(.status.readyReplicas != .spec.replicas) | "\(.metadata.namespace)/\(.metadata.name)"')
if [ -z "$FAILED_STATEFULSETS" ]; then
    echo "✅ All StatefulSets healthy"
else
    echo "❌ Failed StatefulSets: $FAILED_STATEFULSETS"
fi

# 3. Pod status
FAILED_PODS=$(kubectl get pods -A --no-headers | grep -E "(Pending|Failed|CrashLoopBackOff)" | wc -l)
echo "Failed pods: $FAILED_PODS"

# 4. Karpenter events
echo "Recent Karpenter events:"
kubectl get events -n karpenter --sort-by='.lastTimestamp' | tail -5

EOF

chmod +x monitor-migration.sh

# Run monitoring script
./monitor-migration.sh
```

### **Automated Health Checks**

```yaml
# migration-health-check.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: migration-health-check
  namespace: karpenter
spec:
  schedule: "*/5 * * * *"  # Every 5 minutes during migration
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: health-check
            image: bitnami/kubectl:latest
            command:
            - /bin/bash
            - -c
            - |
              # Check StatefulSet health
              FAILED_STATEFULSETS=$(kubectl get statefulsets -A -o json | jq -r '.items[] | select(.status.readyReplicas != .spec.replicas) | "\(.metadata.namespace)/\(.metadata.name)"')
              
              if [ ! -z "$FAILED_STATEFULSETS" ]; then
                echo "ALERT: StatefulSets not healthy: $FAILED_STATEFULSETS"
                exit 1
              fi
              
              # Check for failed pods
              FAILED_PODS=$(kubectl get pods -A --field-selector=status.phase!=Running,status.phase!=Succeeded --no-headers | wc -l)
              
              if [ $FAILED_PODS -gt 0 ]; then
                echo "ALERT: $FAILED_PODS failed pods detected"
                exit 1
              fi
              
              echo "✅ All health checks passed at $(date)"
          restartPolicy: OnFailure
```

---

## 📋 **Migration Timeline and Checklist**

### **Recommended Timeline**

| **Phase** | **Duration** | **Activities** | **Validation** |
|-----------|--------------|----------------|----------------|
| **Preparation** | 1-2 hours | Inventory, prerequisites, backup | All systems healthy |
| **Karpenter Installation** | 1-2 hours | Install via GitOps, create matching NodePools | Karpenter running, NodePools ready |
| **Node Labeling** | 30 minutes | Label existing Terraform nodes for adoption | Karpenter adopts existing nodes |
| **Validation** | 2-4 hours | Verify dual management, no pod movement | All workloads on same nodes, healthy |
| **Additional Pools** | 1-2 hours | Label remaining pools for adoption | All pools managed by both systems |
| **Optimization (Optional)** | 1-2 hours | Enable cost optimization features | Dynamic shapes active |
| **Terraform Cleanup (Optional)** | Variable | Remove Terraform resources if desired | Full Karpenter management |

### **Final Pre-Migration Checklist**

**Before Starting Migration:**
- [ ] Complete backup of all critical data and configurations
- [ ] Terraform state is clean and current
- [ ] All StatefulSets are healthy and stable
- [ ] OCI credentials configured and tested for Karpenter
- [ ] Karpenter installed and running
- [ ] Monitoring and alerting in place

**Per Node Pool Adoption:**
- [ ] Matching Karpenter NodePool created with correct specs
- [ ] Resource requirements exactly match existing Terraform nodes
- [ ] Taints and tolerations correctly configured
- [ ] Existing nodes labeled with karpenter.sh/nodepool
- [ ] Karpenter successfully adopted existing nodes (no new provisioning)
- [ ] All workloads remain on exact same nodes throughout process  
- [ ] No pod movements or disruptions during adoption

**Post-Migration:**
- [ ] All workloads running on Karpenter-managed nodes
- [ ] Zero failed or pending pods
- [ ] All StatefulSets have desired replica counts
- [ ] Cost optimization features enabled and working
- [ ] Monitoring shows healthy resource utilization
- [ ] Migration artifacts cleaned up
- [ ] Documentation updated with new architecture

---

## 🎯 **Success Criteria**

### **Migration is Complete and Successful When:**

1. **✅ Zero Downtime Achieved**
   - No service interruptions during entire migration
   - All StatefulSets maintained desired replica counts
   - No pod movements or disruptions

2. **✅ Successful Node Adoption**
   - All existing nodes now managed by Karpenter
   - Dual management with Terraform (optional) or full Karpenter control
   - Zero new node provisioning during migration

3. **✅ System Stability**
   - All workloads running on exact same infrastructure as before
   - No increase in error rates or alerts
   - Zero pod movements or service disruptions

4. **✅ Future Scalability Ready**
   - Karpenter will handle all future scaling needs
   - Dynamic shape selection available for new nodes
   - Cost optimization features enabled

5. **✅ Operational Simplicity**
   - Simple label-based adoption completed in minutes
   - Clear rollback path (remove labels)
   - Minimal risk compared to traditional migration approaches

---

## 📚 **Additional Resources**

- **[Karpenter OCI Provider Features](./FEATURES_OVERVIEW.md)** - Complete feature comparison
- **[Installation Guide](./deploy-karpenter-oci.md)** - Detailed deployment instructions
- **[Troubleshooting Guide](./troubleshooting-oci.md)** - Common issues and solutions
- **[Performance Benchmarks](./performance-benchmarks.md)** - Validation results and metrics
- **[Security Guide](../SECURITY.md)** - Security best practices and compliance

---

## 🏆 **Migration Approach Summary**

This **simple node adoption approach** provides significant advantages over all other migration strategies:

### **✅ Why This Approach Works Best**
- **Zero Risk**: StatefulSets like Kafka, RabbitMQ, Redis are never touched
- **No New Nodes**: Karpenter adopts existing Terraform nodes - no provisioning needed
- **Instant Rollback**: Simply remove labels to revert to Terraform-only management
- **Ultra-Fast Migration**: Complete in 30 minutes instead of hours/days
- **No Disruption**: Existing pods stay on exact same nodes forever
- **Proven Safe**: Just labels - no infrastructure changes whatsoever

### **🚀 Key Insight**
Instead of migrating workloads or provisioning new nodes, we simply **label existing nodes** so Karpenter can manage their lifecycle while Terraform continues managing their infrastructure. This provides the best of both worlds with zero risk.

**Need assistance with your migration?** Contact our team via [GitHub Issues](https://github.com/startappdev/karpenter/issues) or [email support](mailto:support@startapp.com) for personalized migration planning and support.