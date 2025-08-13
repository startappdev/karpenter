# Zero-Downtime Migration from Terraform Node Pools to Karpenter

## 🎯 **Migration Overview**

This guide provides a **simple, safe approach** for migrating from Terraform-managed OKE node pools to Karpenter management with **absolute zero downtime** and **no pod disruption**. The strategy focuses on **gradual handoff** where Karpenter takes over provisioning while existing Terraform nodes continue running until naturally replaced.

### **🔒 Zero-Downtime Guarantees**
- ✅ **No StatefulSet disruption** - Kafka, RabbitMQ, Redis remain untouched
- ✅ **No pod movements** - Existing pods stay on current nodes
- ✅ **No service interruption** - All services remain available
- ✅ **Simple handoff** - Terraform stops provisioning, Karpenter takes over
- ✅ **Easy rollback** - Reverse the process at any time

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

## 🛡️ **Migration Strategy: Simple Handoff**

### **Phase 1: Install Karpenter Without Disruption**

The key to zero-downtime migration is installing Karpenter alongside existing infrastructure, then gradually handing over provisioning responsibility.

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

#### **Step 1.2: Create Matching NodePools (Ready for Handoff)**

Create Karpenter NodePools that **exactly match** your existing Terraform pools:

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

## 🔄 **Phase 2: Gradual Handoff**

### **Step 2.1: Begin Terraform Scale-Down**

Start reducing Terraform node pool sizes while Karpenter is ready to provision replacement nodes:

```bash
# 1. Verify current state before changes
kubectl get nodes -o custom-columns="NAME:.metadata.name,POOL:.metadata.labels.oci\.oraclecloud\.com/node-pool,STATUS:.status.conditions[?(@.type=='Ready')].status"

# 2. Check StatefulSet health before proceeding
kubectl get statefulsets -A -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name,READY:.status.readyReplicas,DESIRED:.spec.replicas"

# 3. Disable any cluster autoscaler if running (to avoid conflicts)
kubectl scale deployment cluster-autoscaler --replicas=0 -n kube-system 2>/dev/null || echo "No cluster autoscaler found"
```

### **Step 2.2: Gradual Terraform Scale-Down**

Reduce Terraform node pool sizes gradually (start with one pool):

```hcl
# terraform/node-pools.tf - GRADUAL SCALE DOWN
resource "oci_containerengine_node_pool" "kafka_pool" {
  cluster_id     = var.cluster_id
  compartment_id = var.compartment_id
  name           = "kafka-pool"
  
  node_config_details {
    placement_configs {
      availability_domain = var.availability_domain
      subnet_id          = var.private_subnet_id
    }
    # REDUCE SIZE: Start with 1 less node
    size = 2  # Was 3, now 2 (Karpenter will handle new capacity needs)
  }
  
  node_shape = "VM.Standard.E4.Flex"
  node_shape_config {
    ocpus         = 8
    memory_in_gbs = 64
  }
}
```

### **Step 2.3: Understanding the Node Provisioning Trigger**

**Here's exactly how new Karpenter nodes get provisioned during migration:**

#### **🎯 The Triggering Mechanism**
1. **Terraform Scale-Down**: When Terraform reduces node pool size (e.g., 3→2 nodes)
2. **Node Termination**: OCI terminates one of the existing nodes
3. **Pod Eviction**: Pods on the terminated node are evicted by Kubernetes  
4. **Rescheduling**: Kubernetes scheduler tries to reschedule evicted pods
5. **Unschedulable State**: If remaining nodes lack capacity, pods become "Pending"
6. **Karpenter Trigger**: Karpenter detects unschedulable pods and provisions new nodes
7. **New Node**: Karpenter creates a new OCI instance matching NodePool requirements
8. **Pod Scheduling**: Pending pods are scheduled on the new Karpenter-managed node

#### **📋 Real Example**
```bash
# Before: 3 Terraform nodes, 0 Karpenter nodes
kubectl get nodes | grep -E "(kafka-pool|karpenter)"
# kafka-pool-terraform-node-1   Ready   <none>   1d   v1.28.2
# kafka-pool-terraform-node-2   Ready   <none>   1d   v1.28.2  
# kafka-pool-terraform-node-3   Ready   <none>   1d   v1.28.2

# Apply Terraform scale-down (3→2)
terraform apply -target=oci_containerengine_node_pool.kafka_pool

# After: 2 Terraform nodes, 1 Karpenter node (automatically provisioned)
kubectl get nodes | grep -E "(kafka-pool|karpenter)"
# kafka-pool-terraform-node-1   Ready   <none>   1d   v1.28.2
# kafka-pool-terraform-node-2   Ready   <none>   1d   v1.28.2
# kafka-pool-karpenter-abcd123   Ready   <none>   5m   v1.28.2   # <- New Karpenter node
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

## 🎯 **Phase 3: Complete the Handoff**

### **Step 3.1: Continue Terraform Scale-Down**

Once Karpenter has successfully provisioned replacement nodes, continue scaling down remaining pools:

```hcl
# Continue scaling down other Terraform pools
resource "oci_containerengine_node_pool" "rabbitmq_pool" {
  # ... existing configuration ...
  node_config_details {
    # Reduce size gradually
    size = 1  # Was 2, now 1
  }
}

resource "oci_containerengine_node_pool" "redis_pool" {
  # ... existing configuration ...
  node_config_details {
    # Reduce size gradually  
    size = 1  # Was 2, now 1
  }
}
```

```bash
# Apply changes to additional pools (one at a time)
terraform plan -target=oci_containerengine_node_pool.rabbitmq_pool
terraform apply -target=oci_containerengine_node_pool.rabbitmq_pool

# Wait and monitor before proceeding to next pool
kubectl get nodes --watch
kubectl get pods -A --field-selector=status.phase=Pending
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
| **Preparation** | 2-4 hours | Inventory, prerequisites, backup | All systems healthy |
| **Karpenter Installation** | 1-2 hours | Install, create matching NodePools | Karpenter running, ready |
| **First Pool Handoff** | 2-4 hours | Scale down one Terraform pool | Karpenter provisions replacements |
| **Validation** | 4-8 hours | Monitor system stability | No errors, all workloads healthy |
| **Remaining Pools** | 1-2 days | One pool at a time, gradual handoff | Each pool transitioned successfully |
| **Final Terraform Removal** | 1-2 hours | Remove Terraform pool resources | All nodes managed by Karpenter |
| **Optimization** | 1-2 hours | Enable cost optimization features | Dynamic shapes active |

### **Final Pre-Migration Checklist**

**Before Starting Migration:**
- [ ] Complete backup of all critical data and configurations
- [ ] Terraform state is clean and current
- [ ] All StatefulSets are healthy and stable
- [ ] OCI credentials configured and tested for Karpenter
- [ ] Karpenter installed and running
- [ ] Monitoring and alerting in place

**Per Node Pool Handoff:**
- [ ] Matching Karpenter NodePool created with correct specs
- [ ] Resource requirements exactly match Terraform pool
- [ ] Taints and tolerations correctly configured
- [ ] Terraform pool gradually scaled down
- [ ] Karpenter successfully provisioned replacement nodes
- [ ] All workloads remain healthy throughout process
- [ ] No pending or failed pods after handoff

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

2. **✅ Complete Handoff**
   - All node provisioning handled by Karpenter
   - No Terraform-managed node pools remaining
   - Smooth transition without complex migration scripts

3. **✅ System Stability**
   - All workloads running normally on new infrastructure
   - No increase in error rates or alerts
   - Resource utilization optimized

4. **✅ Cost Optimization Ready**
   - Dynamic shape selection enabled (if desired)
   - Right-sizing and consolidation active
   - No resource waste from static provisioning

5. **✅ Operational Simplicity**
   - Simple, reversible process completed
   - Clear rollback path available if needed
   - Minimal complexity compared to complex migration approaches

---

## 📚 **Additional Resources**

- **[Karpenter OCI Provider Features](./FEATURES_OVERVIEW.md)** - Complete feature comparison
- **[Installation Guide](./deploy-karpenter-oci.md)** - Detailed deployment instructions
- **[Troubleshooting Guide](./troubleshooting-oci.md)** - Common issues and solutions
- **[Performance Benchmarks](./performance-benchmarks.md)** - Validation results and metrics
- **[Security Guide](../SECURITY.md)** - Security best practices and compliance

---

## 🏆 **Migration Approach Summary**

This **simple handoff approach** provides significant advantages over complex pod-by-pod migration strategies:

### **✅ Why This Approach Works Best**
- **Zero Risk**: StatefulSets like Kafka, RabbitMQ, Redis are never touched
- **Natural Transition**: Karpenter only provisions when Terraform stops
- **Easy Rollback**: Simply reverse the Terraform scaling if needed
- **Fast Migration**: Complete in hours instead of days
- **No Disruption**: Existing pods continue running on their current nodes
- **Proven Safe**: Leverages Kubernetes' natural scheduling behavior

### **🚀 Key Insight**
Instead of complex migration procedures, we simply **change who provisions new nodes** while letting existing infrastructure continue running until naturally replaced through normal operations.

**Need assistance with your migration?** Contact our team via [GitHub Issues](https://github.com/startappdev/karpenter/issues) or [email support](mailto:support@startapp.com) for personalized migration planning and support.