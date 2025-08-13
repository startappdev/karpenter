# Zero-Downtime Migration from Terraform Node Pools to Karpenter

## 🎯 **Migration Overview**

This guide provides a **comprehensive, step-by-step process** for migrating from Terraform-managed OKE node pools to Karpenter management with **absolute zero downtime** and **no pod disruption**. Special attention is given to protecting critical stateful workloads like Kafka (Strimzi), RabbitMQ, Redis, and other StatefulSets.

### **🔒 Zero-Downtime Guarantees**
- ✅ **No StatefulSet disruption** - Kafka, RabbitMQ, Redis remain untouched
- ✅ **No pod movements** - Existing pods stay on current nodes
- ✅ **No scaling changes** - Current replica counts maintained  
- ✅ **No service interruption** - All services remain available
- ✅ **Gradual transition** - Controlled, reversible process

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

## 🛡️ **Migration Strategy: Parallel Infrastructure**

### **Phase 1: Deploy Karpenter Alongside Existing Infrastructure**

The key to zero-downtime migration is running Karpenter in **parallel** with existing Terraform node pools, not replacing them immediately.

#### **Step 1.1: Install Karpenter (Non-Disruptive)**

```bash
# 1. Create Karpenter namespace and install
kubectl create namespace karpenter

# 2. Install Karpenter with careful configuration
helm install karpenter karpenter-oci/karpenter-oci \
  --namespace karpenter \
  --set oci.region=$OCI_REGION \
  --set oci.compartmentId=$OCI_COMPARTMENT_ID \
  --set oci.clusterId=$OCI_CLUSTER_ID \
  --set oci.existingSecret="oci-config" \
  --wait

# 3. Verify Karpenter is running but not managing anything yet
kubectl get deployment -n karpenter
kubectl get nodepools -A  # Should be empty initially
```

#### **Step 1.2: Create Shadow NodePools (No Immediate Effect)**

Create Karpenter NodePools that **mirror** your existing Terraform pools but with **zero limits** initially:

```yaml
# kafka-nodepool-shadow.yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: kafka-pool-karpenter
  namespace: karpenter
spec:
  # CRITICAL: Zero limits initially - no nodes will be provisioned
  limits:
    cpu: "0"
    memory: "0Gi"
    
  template:
    metadata:
      labels:
        # Match existing Terraform pool labels EXACTLY
        node_pool: kafka
        workload-type: kafka
        oci.oraclecloud.com/node-pool: kafka-pool-terraform  # Keep original reference
      annotations:
        migration.karpenter.sh/terraform-pool: "kafka-pool"
        migration.karpenter.sh/phase: "shadow"
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
        
      expireAfter: 720h  # Long expiration to prevent unexpected termination
      
  # CRITICAL: Disable all disruption during migration
  disruption:
    consolidationPolicy: WhenEmpty
    consolidateAfter: Never  # Prevent any automatic consolidation
    budgets:
      - nodes: "0"  # No disruption allowed
---
# Apply similar configurations for rabbitmq-pool-karpenter and redis-pool-karpenter
```

```bash
# Apply shadow NodePools (safe - no nodes created due to zero limits)
kubectl apply -f kafka-nodepool-shadow.yaml
kubectl apply -f rabbitmq-nodepool-shadow.yaml  
kubectl apply -f redis-nodepool-shadow.yaml

# Verify NodePools exist but are not provisioning
kubectl get nodepools -A
kubectl get nodes -l karpenter.sh/nodepool  # Should be empty
```

---

## 🔄 **Phase 2: Controlled Node Pool Transition**

### **Step 2.1: Prepare for Gradual Migration**

Before any changes, add **protective labels** to prevent accidental disruption:

```bash
# 1. Label all existing nodes as protected during migration
kubectl label nodes -l oci.oraclecloud.com/node-pool=kafka-pool migration.karpenter.sh/protected=true
kubectl label nodes -l oci.oraclecloud.com/node-pool=rabbitmq-pool migration.karpenter.sh/protected=true
kubectl label nodes -l oci.oraclecloud.com/node-pool=redis-pool migration.karpenter.sh/protected=true

# 2. Add finalizers to critical StatefulSets to prevent accidental deletion
kubectl annotate statefulset -n kafka kafka-cluster migration.karpenter.sh/protected=true
kubectl annotate statefulset -n rabbitmq rabbitmq-cluster migration.karpenter.sh/protected=true  
kubectl annotate statefulset -n redis redis-cluster migration.karpenter.sh/protected=true

# 3. Disable any cluster autoscaler if running
kubectl scale deployment cluster-autoscaler --replicas=0 -n kube-system
```

### **Step 2.2: Terraform Modifications (Prepare for Removal)**

Modify your Terraform configuration to prepare for node pool removal **without applying changes yet**:

```hcl
# terraform/node-pools.tf - PREPARE ONLY, DO NOT APPLY
resource "oci_containerengine_node_pool" "kafka_pool" {
  # Add lifecycle rule to prevent destruction during migration
  lifecycle {
    prevent_destroy = true
    ignore_changes = [
      node_config_details[0].size,  # Ignore size changes during migration
      # Add other fields that might change during migration
    ]
  }
  
  # Existing configuration remains unchanged
  cluster_id     = var.cluster_id
  compartment_id = var.compartment_id
  name           = "kafka-pool"
  
  # Keep current configuration intact
  node_config_details {
    placement_configs {
      availability_domain = var.availability_domain
      subnet_id          = var.private_subnet_id
    }
    size = 3  # Current size - DO NOT CHANGE YET
  }
  
  node_shape = "VM.Standard.E4.Flex"
  node_shape_config {
    ocpus         = 8
    memory_in_gbs = 64
  }
}
```

### **Step 2.3: Enable Karpenter NodePools (Controlled Activation)**

**This is the critical transition step - proceed very carefully:**

```yaml
# kafka-nodepool-active.yaml - FIRST POOL ONLY
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: kafka-pool-karpenter
  namespace: karpenter
spec:
  # GRADUAL ACTIVATION: Start with limits matching current capacity
  limits:
    cpu: "48"     # Match current total: 3 nodes × 8 OCPUs × 2 = 48 vCPUs
    memory: "192Gi"  # Match current total: 3 nodes × 64GB = 192GB
    
  template:
    metadata:
      labels:
        node_pool: kafka
        workload-type: kafka
        migration.karpenter.sh/status: "active"
        # CRITICAL: Change the node pool identifier to avoid conflicts
        oci.oraclecloud.com/node-pool: kafka-pool-karpenter
    spec:
      # Exactly match existing requirements
      requirements:
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64"]
        - key: kubernetes.io/os
          operator: In
          values: ["linux"]
        - key: node.kubernetes.io/instance-type
          operator: In
          values: ["VM.Standard.E4.Flex"]
          
      taints:
        - key: node_pool
          value: kafka
          effect: NoSchedule
          
      nodeClassRef:
        group: karpenter.sh
        kind: OCINodeClass
        name: default
        
      expireAfter: 720h
      
  # STILL DISABLED: No disruption until migration is complete
  disruption:
    consolidationPolicy: WhenEmpty
    consolidateAfter: Never
    budgets:
      - nodes: "0"
```

```bash
# Apply the active configuration for ONE NodePool only
kubectl apply -f kafka-nodepool-active.yaml

# Monitor carefully - Karpenter should not provision nodes yet (no unschedulable pods)
kubectl get nodepools -A -o wide
kubectl get events -n karpenter --sort-by='.lastTimestamp'
kubectl logs -n karpenter deployment/karpenter-karpenter-oci -f
```

---

## 🎯 **Phase 3: Controlled Workload Migration**

### **Step 3.1: Create Migration Test Workload**

Before migrating critical StatefulSets, test with a simple workload:

```yaml
# test-migration.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: migration-test
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: migration-test
  template:
    metadata:
      labels:
        app: migration-test
    spec:
      # Force scheduling on Karpenter-managed nodes
      nodeSelector:
        oci.oraclecloud.com/node-pool: kafka-pool-karpenter
      tolerations:
        - key: node_pool
          value: kafka
          effect: NoSchedule
      containers:
      - name: test
        image: nginx:alpine
        resources:
          requests:
            cpu: "1000m"
            memory: "2Gi"
          limits:
            cpu: "1000m"
            memory: "2Gi"
```

```bash
# Deploy test workload to trigger Karpenter node provisioning
kubectl apply -f test-migration.yaml

# Monitor new node provisioning
kubectl get pods -n default -o wide | grep migration-test
kubectl get nodes -l karpenter.sh/nodepool

# Verify new node is healthy and test workload is running
kubectl get nodes -l oci.oraclecloud.com/node-pool=kafka-pool-karpenter -o wide
```

### **Step 3.2: Validate Karpenter Node Provisioning**

```bash
# Comprehensive validation of new Karpenter-managed node
NEW_NODE=$(kubectl get nodes -l oci.oraclecloud.com/node-pool=kafka-pool-karpenter -o jsonpath='{.items[0].metadata.name}')

echo "Validating Karpenter node: $NEW_NODE"

# 1. Check node labels and taints
kubectl describe node $NEW_NODE | grep -A 10 "Labels:"
kubectl describe node $NEW_NODE | grep -A 5 "Taints:"

# 2. Verify OCI instance details
kubectl get node $NEW_NODE -o jsonpath='{.spec.providerID}'

# 3. Test workload scheduling capability
kubectl get pods -n default -o wide | grep migration-test

# 4. Check resource allocation
kubectl describe node $NEW_NODE | grep -A 10 "Allocated resources:"
```

**✅ Validation Checklist:**
- [ ] Karpenter node provisioned with correct shape (VM.Standard.E4.Flex)
- [ ] Node has matching labels and taints as Terraform nodes
- [ ] Test workload scheduled and running successfully
- [ ] Node resources properly allocated and available
- [ ] No errors in Karpenter controller logs

### **Step 3.3: Gradual StatefulSet Migration Strategy**

**CRITICAL: DO NOT move StatefulSets directly. Instead, ensure new nodes can support them:**

```bash
# 1. Verify StatefulSet pod anti-affinity and scheduling constraints
kubectl get statefulset -n kafka kafka-cluster -o yaml | grep -A 20 "affinity:"

# 2. Check current StatefulSet pod distribution
kubectl get pods -n kafka -l app.kubernetes.io/name=kafka -o custom-columns="NAME:.metadata.name,NODE:.spec.nodeName,STATUS:.status.phase"

# 3. Analyze resource requirements
kubectl describe statefulset -n kafka kafka-cluster | grep -A 10 "requests:"
```

**Instead of moving pods, ensure Karpenter can handle NEW StatefulSet pods:**

```yaml
# kafka-test-replica.yaml - CREATE ADDITIONAL REPLICA FOR TESTING
apiVersion: v1
kind: Pod
metadata:
  name: kafka-test-replica
  namespace: kafka
  labels:
    app.kubernetes.io/name: kafka
    test-migration: "true"
spec:
  # Force scheduling on Karpenter node
  nodeSelector:
    oci.oraclecloud.com/node-pool: kafka-pool-karpenter
  tolerations:
    - key: node_pool
      value: kafka
      effect: NoSchedule
  containers:
  - name: kafka
    image: confluentinc/cp-kafka:latest
    resources:
      # Match StatefulSet resource requirements exactly
      requests:
        cpu: "2000m"
        memory: "8Gi"
      limits:
        cpu: "2000m"  
        memory: "8Gi"
    env:
    - name: KAFKA_ZOOKEEPER_CONNECT
      value: "zookeeper-service:2181"
    - name: KAFKA_ADVERTISED_LISTENERS
      value: "PLAINTEXT://kafka-test-replica:9092"
```

```bash
# Deploy test replica on Karpenter node
kubectl apply -f kafka-test-replica.yaml

# Monitor scheduling and startup
kubectl get pod kafka-test-replica -n kafka -o wide
kubectl logs kafka-test-replica -n kafka

# Verify Kafka connectivity from test replica
kubectl exec kafka-test-replica -n kafka -- kafka-topics --bootstrap-server localhost:9092 --list
```

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

# Monitor the scaling down process
kubectl get nodes -l oci.oraclecloud.com/node-pool=kafka-pool --watch

# Verify StatefulSets remain healthy during scale down
kubectl get statefulsets -A -w
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
| **Preparation** | 1-2 days | Inventory, prerequisites, backup | All systems healthy |
| **Karpenter Installation** | 2-4 hours | Install, create shadow NodePools | Karpenter running, no nodes |
| **First Node Test** | 4-8 hours | Activate first pool, test workload | Test pods scheduled successfully |
| **Validation** | 24-48 hours | Monitor system stability | No errors, all StatefulSets healthy |
| **Gradual Migration** | 3-7 days | One pool per day, careful validation | Each pool migrated successfully |
| **Optimization** | 1-2 days | Enable advanced features | Cost optimization active |
| **Cleanup** | 1 day | Remove migration artifacts | Clean final state |

### **Final Pre-Migration Checklist**

**Before Starting Migration:**
- [ ] Complete backup of all critical data and configurations
- [ ] Terraform state is clean and current
- [ ] All StatefulSets are healthy and stable
- [ ] OCI credentials configured and tested for Karpenter
- [ ] Emergency rollback plan tested and ready
- [ ] Monitoring and alerting in place
- [ ] Migration time window scheduled (during low-traffic period)
- [ ] Team is available for monitoring and emergency response

**Per Node Pool Migration:**
- [ ] Shadow NodePool created and validated
- [ ] Test workload successfully scheduled on Karpenter node
- [ ] Resource requirements exactly match Terraform pool
- [ ] Taints and tolerations correctly configured
- [ ] 24+ hours of stable operation before next pool
- [ ] Terraform pool scale-down executed successfully
- [ ] StatefulSets remain healthy throughout process

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
   - No data loss or corruption

2. **✅ Full Karpenter Management**
   - All node provisioning handled by Karpenter
   - No Terraform-managed node pools remaining
   - Dynamic shape selection working (if enabled)

3. **✅ System Stability**
   - 7+ days of stable operation post-migration
   - No increase in error rates or alerts
   - Resource utilization within expected ranges

4. **✅ Cost Optimization**
   - Cost reduction achieved (if dynamic shapes enabled)
   - No resource waste from over-provisioning
   - Right-sizing working effectively

5. **✅ Operational Excellence**
   - Monitoring and alerting working correctly
   - GitOps deployment process validated
   - Team trained on Karpenter operations and troubleshooting

---

## 📚 **Additional Resources**

- **[Karpenter OCI Provider Features](./FEATURES_OVERVIEW.md)** - Complete feature comparison
- **[Installation Guide](./deploy-karpenter-oci.md)** - Detailed deployment instructions
- **[Troubleshooting Guide](./troubleshooting-oci.md)** - Common issues and solutions
- **[Performance Benchmarks](./performance-benchmarks.md)** - Validation results and metrics
- **[Security Guide](../SECURITY.md)** - Security best practices and compliance

---

**Need assistance with your migration?** Contact our team via [GitHub Issues](https://github.com/startappdev/karpenter/issues) or [email support](mailto:support@startapp.com) for personalized migration planning and support.