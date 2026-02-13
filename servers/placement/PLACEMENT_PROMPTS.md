# OCM Placement Natural Language Prompts Guide

This guide documents how natural language descriptions are translated into OCM Placement YAML configurations.

## Overview

OCM Placement is a powerful API that dynamically selects ManagedClusters from ManagedClusterSets for multi-cluster workload scheduling. It enables you to:

- **Select clusters** based on labels, claims, or CEL expressions (predicates - hard requirements)
- **Rank clusters** using built-in or custom scoring (prioritizers - soft requirements)
- **Control distribution** across failure domains (topology-aware placement)
- **Manage rollouts** progressively across cluster groups

## Core Concepts

### Predicates (Hard Requirements)

Predicates define **mandatory** cluster selection criteria. A cluster must satisfy ALL predicates to be selected. There are three types:

1. **Label/Claim Selectors** - Filter by metadata labels or cluster claims
2. **CEL Expressions** - Advanced filtering using Common Expression Language
3. **Taints/Tolerations** - Control cluster eligibility (similar to Kubernetes taints)

### Prioritizers (Soft Requirements)

Prioritizers **rank** clusters that pass predicates by assigning scores. Built-in prioritizers include:

- `Steady` - Stabilizes existing placement decisions
- `Balance` - Distributes selections evenly across clusters
- `ResourceAllocatableCPU` - Ranks by available CPU
- `ResourceAllocatableMemory` - Ranks by available memory
- Custom `AddOnPlacementScore` - Domain-specific scoring

Each prioritizer has a weight (-10 to 10). Final score = sum(weight × prioritizer_score).

### PlacementDecisions

The Placement controller generates `PlacementDecision` objects containing the selected cluster names, ordered by score. These decisions can be grouped using `decisionStrategy` for topology-aware distribution.

### ManagedClusterSets

Placements select from one or more `ManagedClusterSet` resources. You must bind ManagedClusterSets to the Placement namespace using `ManagedClusterSetBinding`.

---

## Quick Reference

| Natural Language Pattern | Predicate Type | YAML Field | Example Value |
|-------------------------|----------------|------------|---------------|
| "production clusters" | Label Selector | `environment` | `production`, `prod` |
| "in us-east-1" | Claim Selector | `region.open-cluster-management.io` | `us-east-1` |
| "OpenShift >= 4.18" | Label Selector | `openshiftVersion-major-minor` | `["4.18", "4.19", "4.20"]` |
| "AWS clusters" | Label Selector | `cloud` | `Amazon` |
| "with enough capacity" | Claim Selector | `schedulable.open-cluster-management.io` | `true` |
| "Kubernetes >= 1.30" | CEL Expression | `semver()` | `semver(...).isGreaterThan(...)` |
| "top 5 clusters" | Spec Field | `numberOfClusters` | `5` |
| "high CPU availability" | Prioritizer | `ResourceAllocatableCPU` | `weight: 8` |

---

## Natural Language Prompt Patterns

## Environment Predicate

### Pattern: "production clusters", "all production clusters"

**Recognized keywords:** `production`, `prod`, `staging`, `stage`, `development`, `dev`, `test`, `qa`

**Maps to (Label Selector Predicate):**
```yaml
predicates:
- requiredClusterSelector:
    labelSelector:
      matchExpressions:
      - key: environment
        operator: In
        values:
        - production
```

**Variations supported:**
- Labels: `environment`, `env`
- Values: `production`, `prod` (for production)
- Values: `development`, `dev` (for development)
- Values: `staging`, `stage` (for staging)

## Region Predicate

### Pattern: "in us-east-1", "us-west-2 region", "europe clusters"

**Recognized keywords:** Region names (e.g., `us-east-1`, `us-west-2`, `eu-central-1`, etc.)

**Maps to (Claim Selector Predicate):**
```yaml
predicates:
- requiredClusterSelector:
    claimSelector:
      matchExpressions:
      - key: region.open-cluster-management.io
        operator: In
        values:
        - us-east-1
```

**Note:** Uses cluster claims (system-defined properties), not labels. Common regions:
- AWS: `us-east-1`, `us-west-2`, `eu-central-1`, etc.
- Azure: `eastus`, `westeurope`, etc.
- GCP: `us-central1`, `europe-west1`, etc.
- Shortened form: `us-east` may match `us-east-1` (implementation-specific)

## OpenShift Version Predicate

### Pattern: "OpenShift >= 4.18", "OpenShift version 4.19 or higher"

**Recognized operators:** `>=`, `>`, `=`, `≥`

**Maps to (Label Selector - Simple Approach):**
```yaml
predicates:
- requiredClusterSelector:
    labelSelector:
      matchExpressions:
      - key: openshiftVersion-major-minor
        operator: In
        values:
        - "4.18"
        - "4.19"
        - "4.20"
        - "4.21"
        - "4.22"
```

**Alternative: CEL Expression (Advanced - True Semantic Versioning):**
```yaml
predicates:
- requiredClusterSelector:
    celSelector:
      celExpressions:
      # Using semver comparison for OpenShift version
      - semver(managedCluster.metadata.labels["openshiftVersion"]).isGreaterThan(semver("4.18.0"))
      # Or use regex pattern matching
      - managedCluster.metadata.labels["openshiftVersion"].matches('^4\\.(1[8-9]|2[0-9])\\.\\d+$')
```

**Important notes:**
- **Label approach:** Uses `openshiftVersion-major-minor` label (format: `4.18`, `4.19`)
  - For `>=` operator, includes current version and future versions up to a reasonable range
  - Values must be quoted strings in YAML
  - Explicit list avoids string comparison issues
- **CEL approach:** Enables true semantic version comparison
  - `semver()` function for proper version comparison
  - Regex patterns with `.matches()` for version ranges
  - Access cluster status, claims, and labels
- Does NOT use cluster claims `version.openshift.io` (which contains full version like `4.19.9`)

### Pattern: "OpenShift 4.19 exactly"

**Maps to:**
```yaml
predicates:
- requiredClusterSelector:
    labelSelector:
      matchExpressions:
      - key: openshiftVersion-major-minor
        operator: In
        values:
        - "4.19"
```

## Cloud Provider Predicate

### Pattern: "AWS clusters", "on Amazon", "Azure only"

**Recognized keywords:** `AWS`, `Amazon`, `Azure`, `GCP`, `Google Cloud`, `vSphere`, `OpenStack`

**Maps to (Label Selector):**
```yaml
predicates:
- requiredClusterSelector:
    labelSelector:
      matchExpressions:
      - key: cloud
        operator: In
        values:
        - Amazon
```

**Alternative: CEL Expression (using cluster claims):**
```yaml
predicates:
- requiredClusterSelector:
    celSelector:
      celExpressions:
      - managedCluster.status.clusterClaims.exists(c, c.name == "platform.open-cluster-management.io" && c.value == "AWS")
```

**Provider name mappings:**
- AWS/Amazon → Label: `Amazon`, Claim: `AWS`
- Azure → `Azure`
- GCP/Google Cloud → `GCP`
- vSphere → `vSphere`
- OpenStack → `OpenStack`

## Capacity Predicate

### Pattern: "with enough capacity", "sufficient resources", "high capacity"

**Recognized keywords:** `capacity`, `resources`, `sufficient`, `enough`

**Maps to (Claim Selector - Schedulability):**
```yaml
predicates:
- requiredClusterSelector:
    claimSelector:
      matchExpressions:
      - key: schedulable.open-cluster-management.io
        operator: In
        values:
        - "true"
```

**Alternative: CEL Expression (Custom Resource Checks):**
```yaml
predicates:
- requiredClusterSelector:
    celSelector:
      celExpressions:
      # Check schedulability
      - managedCluster.status.clusterClaims.exists(c, c.name == "schedulable.open-cluster-management.io" && c.value == "true")
      # Check specific CPU capacity (if mentioned)
      - int(managedCluster.status.capacity.cpu) >= 16
      # Check memory capacity (if mentioned)
      - int(managedCluster.status.capacity.memory.replace("Gi", "")) >= 32
```

**Note:** Capacity checks often work better with **prioritizers** (see Advanced Features section below) rather than hard predicates, allowing selection of clusters with the most available resources.

---

## Advanced Placement Features

### CEL Expressions (Common Expression Language)

CEL provides powerful, flexible cluster selection beyond label/claim selectors.

**Accessing ManagedCluster fields:**
```yaml
predicates:
- requiredClusterSelector:
    celSelector:
      celExpressions:
      # Access cluster status
      - managedCluster.status.version.kubernetes == "v1.31.0"

      # Access labels with regex
      - managedCluster.metadata.labels["version"].matches('^1\\.(30|31)\\.\\d+$')

      # Access cluster claims
      - managedCluster.status.clusterClaims.exists(c, c.name == "kubeversion.open-cluster-management.io" && c.value == "v1.31.0")

      # Semantic version comparison
      - semver(managedCluster.metadata.labels["version"]).isGreaterThan(semver("1.30.0"))

      # Filter by AddOnPlacementScore
      - managedCluster.status.addonPlacementScores.exists(aps, aps.name == "cpuratio" && int(aps.value) < 60)
```

**CEL capabilities:**
- Standard macros and functions (exists, matches, etc.)
- Kubernetes semver library (`semver()`, `isGreaterThan()`, `isLessThan()`)
- Integer/string conversions
- Complex boolean logic

### Prioritizers (Cluster Ranking)

Prioritizers rank clusters that pass predicates. Multiple prioritizers combine using weighted scores.

**Built-in prioritizers:**
```yaml
prioritizerPolicy:
  mode: Exact  # or Additive (default includes Steady + Balance)
  configurations:
  - scoreCoordinate:
      type: BuiltIn
      builtIn: ResourceAllocatableCPU
    weight: 5
  - scoreCoordinate:
      type: BuiltIn
      builtIn: ResourceAllocatableMemory
    weight: 3
  - scoreCoordinate:
      type: BuiltIn
      builtIn: Steady
    weight: 2
```

**Prioritizer types:**
- `Steady` - Stabilizes decisions (avoids churn)
- `Balance` - Even distribution across clusters
- `ResourceAllocatableCPU` - Prefers clusters with more available CPU
- `ResourceAllocatableMemory` - Prefers clusters with more available memory

**Custom AddOnPlacementScore:**
```yaml
prioritizerPolicy:
  configurations:
  - scoreCoordinate:
      type: AddOn
      addOn:
        resourceName: cpuratio
        scoreName: cpuratio
    weight: 8
```

**Weight rules:**
- Range: -10 to 10
- Higher weight = more influence
- Final score = sum(weight × prioritizer_score)

### Number of Clusters

Limit how many clusters to select:

```yaml
spec:
  numberOfClusters: 3  # Select top 3 clusters by score
```

### Tolerations (Cluster Taints)

Control placement on tainted clusters:

```yaml
tolerations:
- key: "node.kubernetes.io/unreachable"
  operator: "Exists"
  tolerationSeconds: 300  # Evict after 5 minutes
- key: "gpu"
  operator: "Equal"
  value: "true"
```

**Operators:**
- `Exists` - Tolerate any value for the key
- `Equal` - Tolerate specific key=value

**TolerationSeconds:** Time to wait before evicting workload from tainted cluster (optional)

### Decision Strategy (Topology-Aware Distribution)

Group clusters by topology for disaster resilience:

```yaml
decisionStrategy:
  groupStrategy:
    # Define failure domain groups
    decisionGroups:
    - groupName: us-east
      groupClusterSelector:
        labelSelector:
          matchLabels:
            region: us-east-1
    - groupName: us-west
      groupClusterSelector:
        labelSelector:
          matchLabels:
            region: us-west-2

    # Max clusters per group
    clustersPerDecisionGroup: "50%"  # or absolute number like 2
```

**Use cases:**
- Multi-region HA: Distribute across regions
- Multi-cloud: Spread across AWS, Azure, GCP
- Multi-zone: Balance across availability zones

### Rollout Strategy

Control how workloads are deployed to selected clusters:

```yaml
rolloutStrategy:
  type: Progressive  # or All, ProgressivePerGroup
  progressive:
    minSuccessTime: 5m
    progressDeadline: 10m
    maxFailures: 2  # or "30%"
    maxConcurrency: 4
    mandatoryDecisionGroups:
      - groupName: us-east
```

**Rollout types:**
- `All` - Deploy to all clusters simultaneously
- `Progressive` - Deploy cluster-by-cluster with validation
- `ProgressivePerGroup` - Deploy group-by-group

**Progressive options:**
- `minSuccessTime` - Wait after successful deployment before proceeding
- `progressDeadline` - Max time for deployment to succeed
- `maxFailures` - Failure tolerance before halting rollout
- `maxConcurrency` - Parallel deployment limit (Progressive mode only)
- `mandatoryDecisionGroups` - Priority groups to deploy first

### ManagedClusterSets

Specify which cluster sets to select from:

```yaml
spec:
  clusterSets:
  - production-clusters
  - staging-clusters
```

**Requirements:**
- ManagedClusterSets must exist
- ManagedClusterSetBinding must exist in Placement namespace
- Clusters in terminating state are automatically excluded (OCM v0.14.0+)

---

## Combined Examples

### Example 1: Production US clusters with modern OpenShift

**Prompt:**
```
Deploy to all production clusters in us-east-1 with OpenShift >= 4.18
```

**Generated YAML:**
```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: workload-placement
  namespace: default
spec:
  predicates:
  - requiredClusterSelector:
      claimSelector:
        matchExpressions:
        - key: region.open-cluster-management.io
          operator: In
          values:
          - us-east-1
      labelSelector:
        matchExpressions:
        - key: environment
          operator: In
          values:
          - production
        - key: openshiftVersion-major-minor
          operator: In
          values:
          - "4.18"
          - "4.19"
          - "4.20"
          - "4.21"
          - "4.22"
```

### Example 2: Development clusters on AWS

**Prompt:**
```
All development clusters on AWS
```

**Generated YAML:**
```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: workload-placement
  namespace: default
spec:
  predicates:
  - requiredClusterSelector:
      labelSelector:
        matchExpressions:
        - key: environment
          operator: In
          values:
          - development
        - key: cloud
          operator: In
          values:
          - Amazon
```

### Example 3: High availability setup

**Prompt:**
```
Production clusters in us-west-2 and eu-central-1 with OpenShift >= 4.19
```

**Generated YAML:**
```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: workload-placement
  namespace: default
spec:
  predicates:
  - requiredClusterSelector:
      claimSelector:
        matchExpressions:
        - key: region.open-cluster-management.io
          operator: In
          values:
          - us-west-2
          - eu-central-1
      labelSelector:
        matchExpressions:
        - key: environment
          operator: In
          values:
          - production
        - key: openshiftVersion-major-minor
          operator: In
          values:
          - "4.19"
          - "4.20"
          - "4.21"
          - "4.22"
```

### Example 4: Advanced - CEL with Prioritizers

**Prompt:**
```
Select top 5 production AWS clusters with Kubernetes >= 1.30 and high CPU availability
```

**Generated YAML:**
```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: advanced-placement
  namespace: default
spec:
  numberOfClusters: 5
  clusterSets:
  - production-clusters
  predicates:
  - requiredClusterSelector:
      celSelector:
        celExpressions:
        # Environment check via label
        - managedCluster.metadata.labels["environment"] == "production"
        # Platform check via cluster claim
        - managedCluster.status.clusterClaims.exists(c, c.name == "platform.open-cluster-management.io" && c.value == "AWS")
        # Kubernetes version with semantic versioning
        - semver(managedCluster.status.version.kubernetes).isGreaterThan(semver("v1.30.0"))
  prioritizerPolicy:
    mode: Exact
    configurations:
    # Prefer clusters with more CPU
    - scoreCoordinate:
        type: BuiltIn
        builtIn: ResourceAllocatableCPU
      weight: 8
    # Prefer clusters with more memory (lower priority)
    - scoreCoordinate:
        type: BuiltIn
        builtIn: ResourceAllocatableMemory
      weight: 3
    # Stabilize decisions to avoid churn
    - scoreCoordinate:
        type: BuiltIn
        builtIn: Steady
      weight: 2
```

### Example 5: Multi-Region HA with Progressive Rollout

**Prompt:**
```
Deploy to production clusters across 3 regions with progressive rollout
```

**Generated YAML:**
```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: Placement
metadata:
  name: ha-progressive-placement
  namespace: default
spec:
  clusterSets:
  - production-clusters
  predicates:
  - requiredClusterSelector:
      labelSelector:
        matchExpressions:
        - key: environment
          operator: In
          values:
          - production
  # Topology-aware distribution
  decisionStrategy:
    groupStrategy:
      decisionGroups:
      - groupName: us-east
        groupClusterSelector:
          labelSelector:
            matchExpressions:
            - key: region
              operator: In
              values:
              - us-east-1
      - groupName: us-west
        groupClusterSelector:
          labelSelector:
            matchExpressions:
            - key: region
              operator: In
              values:
              - us-west-2
      - groupName: europe
        groupClusterSelector:
          labelSelector:
            matchExpressions:
            - key: region
              operator: In
              values:
              - eu-central-1
      # Max 2 clusters per region
      clustersPerDecisionGroup: 2
  # Progressive rollout strategy
  rolloutStrategy:
    type: ProgressivePerGroup
    progressivePerGroup:
      minSuccessTime: 5m
      progressDeadline: 15m
      maxFailures: 1
      mandatoryDecisionGroups:
        # Deploy to us-east first
        - groupName: us-east
```

## Label vs Claim Selectors

### Use labelSelector for:
- `environment` / `env`
- `cloud`
- `openshiftVersion-major-minor`
- `vendor`
- Custom user-defined labels

### Use claimSelector for:
- `region.open-cluster-management.io`
- `platform.open-cluster-management.io`
- `version.openshift.io` (full version string)
- `schedulable.open-cluster-management.io`
- `kubeversion.open-cluster-management.io`
- System-defined cluster properties

## Common Pitfalls

1. **Version format mismatch:**
   - ❌ Don't use: `version.openshift.io` claim with value `"4.18"` (claim contains full version `4.18.5`)
   - ✅ Use: `openshiftVersion-major-minor` label with value `"4.18"`
   - ✅ Or use CEL: `semver(managedCluster.metadata.labels["openshiftVersion"]).isGreaterThan(semver("4.18.0"))`

2. **Region exact matching:**
   - ❌ Don't use: `us-east` when clusters have `us-east-1`
   - ✅ Use: Full region name `us-east-1`

3. **Environment label variations:**
   - ✅ Supported: `environment=production`, `env=prod`
   - ❌ Not auto-matched: `type=production`, `tier=prod` (requires custom mapping)

4. **Version comparison operators:**
   - ❌ Label selectors: String comparison, not semantic versioning
   - ✅ For `>=`: Include all versions in range explicitly
   - ✅ Best practice: Use CEL with `semver()` for true semantic version comparison

5. **Missing ManagedClusterSetBinding:**
   - Placement requires `ManagedClusterSetBinding` in the same namespace
   - Check: `kubectl get managedclustersetbinding -n <namespace>`

6. **Terminating clusters:**
   - OCM v0.14.0+ automatically excludes terminating clusters
   - Earlier versions may include them in decisions

7. **CEL expression errors:**
   - Missing parentheses or quotes in expressions
   - Incorrect field access (use `managedCluster.status.` not `cluster.status.`)
   - Type mismatches (use `int()` for string-to-int conversion)

8. **Prioritizer mode confusion:**
   - `Additive` mode (default): Includes `Steady` and `Balance` automatically
   - `Exact` mode: Only uses explicitly configured prioritizers

## Testing and Troubleshooting

### Dry Run Testing

Use the dry run feature to validate your placement before deploying:

```bash
# Via natural language
/dryrun-placement "production clusters in us-east-1 with OpenShift >= 4.18"

# Via YAML file
/dryrun-placement --file placement.yaml
```

The dry run will show:
- Which clusters match your criteria
- Why each cluster was selected
- Total number of matched clusters

### Monitoring Placement Status

Check Placement conditions to diagnose issues:

```bash
kubectl get placement <placement-name> -n <namespace> -o yaml
```

**Key conditions to monitor:**

1. **PlacementSatisfied**
   - `True` - Successfully selected required number of clusters
   - `False` - Not enough clusters match criteria

2. **PlacementMisconfigured**
   - `True` - Configuration validation errors
   - Check `.status.conditions` for error details

**Example status:**
```yaml
status:
  conditions:
  - type: PlacementSatisfied
    status: "True"
    reason: AllDecisionsScheduled
    message: All cluster decisions scheduled
  - type: PlacementMisconfigured
    status: "False"
    reason: Succeedconfigured
  numberOfSelectedClusters: 3
```

### Viewing PlacementDecisions

Check which clusters were selected:

```bash
kubectl get placementdecisions -l cluster.open-cluster-management.io/placement=<placement-name> -n <namespace>
```

**Example output:**
```yaml
apiVersion: cluster.open-cluster-management.io/v1beta1
kind: PlacementDecision
metadata:
  labels:
    cluster.open-cluster-management.io/placement: workload-placement
    cluster.open-cluster-management.io/decision-group-name: us-east
  name: workload-placement-decision-1
spec:
  decisions:
  - clusterName: cluster1
    reason: "Matched predicates, score: 85"
  - clusterName: cluster2
    reason: "Matched predicates, score: 82"
```

### Debugging with Port-Forward

Access placement controller debug endpoints:

```bash
# Port-forward to placement controller
kubectl port-forward -n open-cluster-management-hub \
  deployment/cluster-manager-placement-controller 8443:8443

# View filtered clusters and scores (requires authentication)
curl -k https://localhost:8443/debug/placements/<namespace>/<placement-name>
```

This shows:
- All clusters that matched predicates
- Prioritizer scores for each cluster
- Final ranking and selection

### Common Troubleshooting Scenarios

**No clusters selected (numberOfSelectedClusters: 0):**
1. Check ManagedClusterSetBinding exists in namespace
2. Verify clusters match all predicates (try removing predicates one-by-one)
3. Check for typos in label/claim keys and values
4. Verify clusters are in `Available` state (not `Unavailable` or terminating)

**Wrong clusters selected:**
1. Review prioritizer weights and scores
2. Check if `Steady` prioritizer is preserving old decisions
3. Verify CEL expressions with simpler test cases

**Placement not updating:**
1. Check if clusters are tainted and tolerations not configured
2. Verify rollout strategy isn't blocking updates
3. Check placement controller logs:
   ```bash
   kubectl logs -n open-cluster-management-hub \
     deployment/cluster-manager-placement-controller
   ```

**CEL expression errors:**
1. Test expressions in smaller increments
2. Verify field paths: `managedCluster.status.`, `managedCluster.metadata.labels`
3. Use proper type conversions: `int()`, `string()`
4. Escape regex special characters: `\\d+`, `\\.`

---

## Additional Resources

- [OCM Placement Official Docs](https://open-cluster-management.io/docs/concepts/content-placement/placement/)
- [CEL Language Specification](https://github.com/google/cel-spec)
- [Kubernetes Label Selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/)
- [AddOnPlacementScore API](https://open-cluster-management.io/docs/concepts/content-placement/addon-placement-score/)
