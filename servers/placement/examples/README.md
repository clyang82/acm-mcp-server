# Placement Generation Examples

This directory contains example prompts and their generated Placement YAML outputs demonstrating the capabilities of the Placement MCP Server.

## Table of Contents

- [Natural Language Examples](#natural-language-examples)
  - [Production Workload with Version Requirements](#example-1-production-workload-with-version-requirements)
  - [VM with Resource Requirements](#example-2-vm-with-resource-requirements)
  - [VM with Prioritized CPU](#example-3-vm-with-prioritized-cpu)
- [VirtualMachine-Based Examples](#virtualmachine-based-examples)
  - [Generating Placement from VM YAML](#example-4-generating-placement-from-vm-yaml)

---

## Natural Language Examples

### Example 1: Production Workload with Version Requirements

**Prompt:**
```
As a ACM user, I want to deploy this workload to all production clusters in us-east-1 with enough capacity and OpenShift ≥ 4.18.
```

**Generated Placement:**
```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: workload-placement
  namespace: default
spec:
  predicates:
  - requiredClusterSelector:
      # Production environment check
      celSelector:
        celExpressions:
        - (has(managedCluster.metadata.labels.environment) && (managedCluster.metadata.labels["environment"]
          == "production" || managedCluster.metadata.labels["environment"] == "prod"))
          || (has(managedCluster.metadata.labels.env) && (managedCluster.metadata.labels["env"]
          == "production" || managedCluster.metadata.labels["env"] == "prod"))
        # OpenShift version >= 4.18
        - has(managedCluster.metadata.labels.openshiftVersion) &&
          !semver(managedCluster.metadata.labels["openshiftVersion"]).isLessThan(semver("4.18.0"))
      # Region filter
      claimSelector:
        matchExpressions:
        - key: region.open-cluster-management.io
          operator: In
          values:
          - us-east-1
```

**Key Features:**
- Flexible environment label matching (supports `environment=production`, `env=prod`, etc.)
- Semantic version comparison using CEL
- Region-based cluster selection using claims

---

### Example 2: VM with Resource Requirements

**Prompt:**
```
As a Virt user, I want to create a VM in 1 production cluster. My VM needs 8GB memory, 2 CPU cores and 100GB disk.
```

**Generated Placement:**
```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: vm-placement
  namespace: default
spec:
  numberOfClusters: 1
  predicates:
  - requiredClusterSelector:
      celSelector:
        celExpressions:
        # Production environment - supports multiple label variations
        - (has(managedCluster.metadata.labels.environment) && (managedCluster.metadata.labels["environment"]
          == "production" || managedCluster.metadata.labels["environment"] == "prod"))
          || (has(managedCluster.metadata.labels.env) && (managedCluster.metadata.labels["env"]
          == "production" || managedCluster.metadata.labels["env"] == "prod"))
        # Minimum 2 CPUs
        - has(managedCluster.status.capacity.cpu) && int(managedCluster.status.capacity.cpu)
          >= 2
        # Minimum 8Gi memory
        - has(managedCluster.status.capacity.memory) && int(managedCluster.status.capacity.memory.replace("Gi",
          "").replace("Mi", "").replace("Ki", "")) >= 8
        # Minimum 100GB disk (ephemeral-storage)
        - '"ephemeral-storage" in managedCluster.status.capacity && int(managedCluster.status.capacity["ephemeral-storage"].replace("Ki",
          "")) >= 104857600'
```

**Key Features:**
- Single cluster selection with `numberOfClusters: 1`
- CPU capacity validation
- Memory capacity validation with unit conversion
- Ephemeral storage validation (100GB = 104857600 Ki)

---

### Example 3: VM with Prioritized CPU

**Prompt:**
```
As a Virt user, I want to create a VM in 1 production cluster. My VM needs 8GB memory and a 100GB disk with the highest CPU.
```

**Generated Placement:**
```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: vm-placement-high-cpu
  namespace: default
spec:
  numberOfClusters: 1
  predicates:
  - requiredClusterSelector:
      celSelector:
        celExpressions:
        # Production environment - supports multiple label variations
        - (has(managedCluster.metadata.labels.environment) && (managedCluster.metadata.labels["environment"]
          == "production" || managedCluster.metadata.labels["environment"] == "prod"))
          || (has(managedCluster.metadata.labels.env) && (managedCluster.metadata.labels["env"]
          == "production" || managedCluster.metadata.labels["env"] == "prod"))
        # Minimum 8Gi memory
        - has(managedCluster.status.capacity.memory) && int(managedCluster.status.capacity.memory.replace("Gi",
          "").replace("Mi", "").replace("Ki", "")) >= 8
        # Minimum 500GB disk (ephemeral-storage in Ki = 524288000)
        - '"ephemeral-storage" in managedCluster.status.capacity && int(managedCluster.status.capacity["ephemeral-storage"].replace("Ki",
          "")) >= 524288000'
  prioritizerPolicy:
    mode: Exact
    configurations:
    # Prioritize clusters with highest CPU (weight: 8)
    - scoreCoordinate:
        type: BuiltIn
        builtIn: ResourceAllocatableCPU
      weight: 8
    # Lower priority for memory (weight: 3)
    - scoreCoordinate:
        type: BuiltIn
        builtIn: ResourceAllocatableMemory
      weight: 3
    # Steady placement for stability (weight: 2)
    - scoreCoordinate:
        type: BuiltIn
        builtIn: Steady
      weight: 2
```

### Example 4: VM with Prioritized CPU with real-time resource utilization

It requires to deploy [resource-usage-score](https://github.com/open-cluster-management-io/addon-contrib/blob/main/resource-usage-collect-addon/README.md) to your environment.

**Prompt:**
```
As a Virt user, I want to create a VM in 1 production cluster. My VM needs 8GB memory and a 100GB disk with the highest CPU.
```

**Generated Placement:**
```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: vm-placement-high-cpu
  namespace: default
spec:
  numberOfClusters: 1
  predicates:
  - requiredClusterSelector:
      celSelector:
        celExpressions:
        # Production environment - supports multiple label variations
        - (has(managedCluster.metadata.labels.environment) && (managedCluster.metadata.labels["environment"]
          == "production" || managedCluster.metadata.labels["environment"] == "prod"))
          || (has(managedCluster.metadata.labels.env) && (managedCluster.metadata.labels["env"]
          == "production" || managedCluster.metadata.labels["env"] == "prod"))
        # Minimum 8Gi memory
        - has(managedCluster.status.capacity.memory) && int(managedCluster.status.capacity.memory.replace("Gi",
          "").replace("Mi", "").replace("Ki", "")) >= 8
        # Minimum 500GB disk (ephemeral-storage in Ki = 524288000)
        - '"ephemeral-storage" in managedCluster.status.capacity && int(managedCluster.status.capacity["ephemeral-storage"].replace("Ki",
          "")) >= 524288000'
  prioritizerPolicy:
    mode: Exact
    configurations:
    - scoreCoordinate:
        type: AddOn
        addOn:
          resourceName: resource-usage-score
          scoreName: cpuClusterAvailable
      weight: 5
    - scoreCoordinate:
        type: AddOn
        addOn:
          resourceName: resource-usage-score
          scoreName: memClusterAvailable
      weight: 5
    - scoreCoordinate:
        type: BuiltIn
        builtIn: Steady
      weight: 2
```

**Key Features:**
- Intelligent prioritization based on "highest CPU" keyword
- Weighted scoring: CPU (8), Memory (3), Steady (2)
- Ensures VM lands on cluster with best CPU availability
- Includes Steady prioritizer for placement stability

---

## VirtualMachine-Based Examples

### Example 5: Generating Placement from VM YAML

**Input: VirtualMachine YAML** (`demo-vm.yaml`):
```yaml
apiVersion: kubevirt.io/v1
kind: VirtualMachine
metadata:
  name: demo-vm
  namespace: default
spec:
  running: true
  template:
    metadata:
      labels:
        kubevirt.io/domain: demo-vm
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  kubevirt.io/domain: demo-vm
              topologyKey: kubernetes.io/hostname
      nodeSelector:
        env: prod
      domain:
        cpu:
          cores: 2
        resources:
          requests:
            memory: 4Gi
        devices:
          disks:
            - name: rootdisk
              disk:
                bus: virtio
          interfaces:
            - name: default
              masquerade: {}
      networks:
        - name: default
          pod: {}
      volumes:
        - name: rootdisk
          containerDisk:
            image: quay.io/kubevirt/cirros-container-disk-demo
```

**Command:**
```
User: "Generate a placement for @demo-vm.yaml"
```

**Generated Placement:**
```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: vm-demo-placement
  namespace: default
spec:
  numberOfClusters: 1  # Anti-affinity with kubernetes.io/hostname topology
  predicates:
  - requiredClusterSelector:
      labelSelector:
        matchExpressions:
        - key: env
          operator: In
          values:
          - prod
      celSelector:
        celExpressions:
        # Minimum 2 CPU cores
        - has(managedCluster.status.capacity.cpu) && int(managedCluster.status.capacity.cpu) >= 2
        # Minimum 4Gi memory
        - has(managedCluster.status.capacity.memory) && int(managedCluster.status.capacity.memory.replace("Gi",
          "").replace("Mi", "").replace("Ki", "")) >= 4
  prioritizerPolicy:
    mode: Exact
    configurations:
    - scoreCoordinate:
        type: AddOn
        addOn:
          resourceName: resource-usage-score
          scoreName: cpuClusterAvailable
      weight: 5
    - scoreCoordinate:
        type: AddOn
        addOn:
          resourceName: resource-usage-score
          scoreName: memClusterAvailable
      weight: 5
    - scoreCoordinate:
        type: BuiltIn
        builtIn: Steady
      weight: 2
```

**Extracted Requirements:**
- **CPU**: 2 cores (from `spec.template.spec.domain.cpu.cores`)
- **Memory**: 4Gi (from `spec.template.spec.domain.resources.requests.memory`)
- **Node Selector**: `env=prod` (converted to cluster label selector)
- **Anti-Affinity**: `kubernetes.io/hostname` → Selects 1 cluster (VMs spread across nodes within cluster)

**Key Features:**
- Automatic resource requirement extraction from VM spec
- Node selector mapping to cluster labels
- Anti-affinity rule interpretation (hostname → single cluster selection)
- Equal prioritization for CPU and memory (balanced for VM workloads)
- Automatic dry run to show which clusters match

---

## Usage Tips

1. **Natural Language**: Use descriptive prompts like "deploy to production clusters with OpenShift >= 4.16"
2. **VM YAML**: Reference VM files with `@demo-vm.yaml` to automatically extract requirements
3. **Prioritization Keywords**: Use "highest", "best", "most" to trigger intelligent prioritization
4. **Dry Run**: All placement generations automatically perform a dry run to preview selected clusters

## See Also

- [Main README](../README.md) - Full server documentation
- [demo-vm.yaml](demo-vm.yaml) - Example VirtualMachine specification
