# Placement MCP Server - Prompt Examples

This guide provides example prompts and tips for using the Placement MCP Server to generate OCM Placements from natural language descriptions.

## Quick Start Examples

### Virtual Machine Deployments

#### Example 1: VM with Specific Resources
```
As a Virt user, I want to create a VM in 1 production cluster. My VM needs 8GB memory and 2 CPU and 100GB disk.
```

**Generated placement will:**
- Select exactly 1 production cluster
- Require minimum 2 CPUs
- Require minimum 8GB memory
- Require minimum 100GB disk space

#### Example 2: VM with Highest CPU Priority
```
As a Virt user, I want to create a VM in 1 production cluster. My VM needs 8GB memory and 500GB disk with highest CPU.
```

**Generated placement will:**
- Select exactly 1 production cluster
- Require minimum 8GB memory
- Require minimum 500GB disk space
- Prioritize clusters with the most available CPU (weight: 8)

#### Example 3: VM with Highest Memory Priority
```
As a Virt user, I want to create a VM in 1 production cluster. My VM needs 4 CPUs and 100GB disk with highest memory.
```

**Generated placement will:**
- Select exactly 1 production cluster
- Require minimum 4 CPUs
- Require minimum 100GB disk space
- Prioritize clusters with the most available memory (weight: 8)

### Application Deployments

#### Example 4: Multi-Region Production Deployment
```
As an ACM user, I want to deploy this workload to all production clusters in us-east with enough capacity and OpenShift >= 4.18.
```

**Generated placement will:**
- Select all production clusters in the us-east region
- Require OpenShift version 4.18 or higher
- Include capacity requirements if specified

#### Example 5: Regional Deployment with Resource Requirements
```
Deploy to 3 production clusters in us-west with at least 16 CPUs and 32GB memory.
```

**Generated placement will:**
- Select exactly 3 production clusters
- Filter to us-west region
- Require minimum 16 CPUs
- Require minimum 32GB memory

#### Example 6: Cloud Provider Specific
```
Deploy to 2 production clusters on AWS with OpenShift >= 4.16 and at least 8 CPUs.
```

**Generated placement will:**
- Select exactly 2 production clusters
- Filter to AWS cloud provider
- Require OpenShift version 4.16 or higher
- Require minimum 8 CPUs

### Staging and Development Environments

#### Example 7: Staging Environment
```
Deploy to all staging clusters in eu-west with at least 4 CPUs and 8GB memory.
```

**Generated placement will:**
- Select all staging clusters
- Filter to eu-west region
- Require minimum 4 CPUs
- Require minimum 8GB memory

#### Example 8: Development Environment
```
Deploy to 1 development cluster with at least 2 CPUs.
```

**Generated placement will:**
- Select exactly 1 development cluster
- Require minimum 2 CPUs

## Supported Keywords and Patterns

### Environment Labels
- `production`, `prod` → Matches `environment=production` or `env=prod`
- `staging`, `stage` → Matches `environment=staging` or `env=staging`
- `development`, `dev` → Matches `environment=development` or `env=dev`

### Regions
- `us-east`, `us-west`, `us-central`
- `eu-west`, `eu-central`, `eu-north`
- `ap-southeast`, `ap-northeast`, `ap-south`
- Supports availability zones: `us-east-1`, `us-west-2`, etc.

### Cloud Providers
- `AWS`, `Amazon`
- `Azure`
- `GCP`, `Google`

### Resource Requirements
- **CPU:** `N cpu`, `N cores`, `N cpus`
  - Example: `8 CPU`, `16 cores`
- **Memory:** `N GB memory`, `N Gi memory`, `N GiB memory`
  - Example: `16GB memory`, `32Gi memory`
- **Disk/Storage:** `N GB disk`, `N GB storage`, `N Ti storage`
  - Example: `100GB disk`, `500GB storage`, `1TB disk`

### OpenShift Version
- `OpenShift >= 4.16`
- `OpenShift ≥ 4.18`

### Number of Clusters
- `1 cluster`, `1 production cluster`
- `3 clusters`, `select 2 clusters`
- `all clusters`, `all production clusters`

### Prioritization
- `highest CPU`, `most CPU`, `best CPU` → Prioritizes clusters with most available CPU
- `highest memory`, `most memory`, `best memory` → Prioritizes clusters with most available memory
- `best` → Prioritizes both CPU and memory equally

## Tips for Writing Effective Prompts

### 1. Be Specific About Requirements
**Good:**
```
Deploy to 1 production cluster with 8GB memory and 4 CPUs
```

**Less Specific:**
```
Deploy to production
```

### 2. Combine Multiple Criteria
```
As a Virt user, I want to create a VM in 1 production cluster in us-east on AWS with OpenShift >= 4.18, 16GB memory, 8 CPUs, and 200GB disk with highest CPU.
```

This will generate a placement that:
- Targets production environment
- Filters to us-east region
- Filters to AWS cloud provider
- Requires OpenShift 4.18+
- Requires minimum 16GB memory, 8 CPUs, 200GB disk
- Prioritizes clusters with highest CPU

### 3. Use Natural Language
The server understands conversational prompts:
- "As a Virt user, I want to..."
- "Deploy this application to..."
- "I need a cluster with..."
- "Select production clusters that have..."

### 4. Specify Prioritization When Needed
If you want the best cluster for your workload:
- Add `with highest CPU` for CPU-intensive workloads
- Add `with highest memory` for memory-intensive workloads
- Add `best` for general purpose workloads

## Common Use Cases

### High-Availability Deployments
```
Deploy to 3 production clusters in different regions with at least 16GB memory and 8 CPUs.
```

### Resource-Intensive Workloads
```
Deploy to 1 production cluster with highest CPU, at least 32GB memory and 500GB disk.
```

### Multi-Cloud Deployments
```
Deploy to all production clusters on AWS and Azure with OpenShift >= 4.17.
```

### Regional Compliance
```
Deploy to all production clusters in eu-central with at least 8GB memory.
```

### Testing Environments
```
Deploy to 1 staging cluster with at least 4 CPUs and 8GB memory.
```

## Understanding Dry Run Results

When you generate a placement, the MCP server automatically performs a dry run to show which clusters match your criteria:

```json
{
  "decisions": [
    {
      "clusterName": "cluster-01",
      "reason": "Total score: 856 (ResourceAllocatableCPU=100*8, ResourceAllocatableMemory=50*3, Steady=2*2)"
    }
  ],
  "totalMatched": 1,
  "summary": "Selected 1 out of 5 total clusters"
}
```

**Understanding the score:**
- Each prioritizer contributes to the total score
- Format: `PrioritizerName=score*weight`
- Higher total score = better match
- If multiple clusters match and `numberOfClusters` is specified, the top N clusters by score are selected

## Troubleshooting

### No Clusters Selected

If your dry run shows `"totalMatched": 0`, check:
1. Are your resource requirements too high?
2. Are your clusters properly labeled (environment, region, cloud)?
3. Does the OpenShift version requirement match your clusters?
4. Do your clusters have the capacity fields populated in their status?

### Adjusting Requirements

Start with broader requirements and narrow down:
```
# Start broad
Deploy to production clusters

# Add region
Deploy to production clusters in us-east

# Add resources
Deploy to production clusters in us-east with 8GB memory

# Add version
Deploy to production clusters in us-east with 8GB memory and OpenShift >= 4.16
```

## Advanced Features

### CEL Expressions
The generated placements use Common Expression Language (CEL) for flexible filtering:
- Safe field access with `has()` checks
- Support for semver comparisons
- String manipulation and numeric comparisons

### Built-in Prioritizers
- **ResourceAllocatableCPU:** Prioritizes clusters with more available CPU
- **ResourceAllocatableMemory:** Prioritizes clusters with more available memory
- **Steady:** Maintains stable placement decisions over time

### Label Flexibility
The server supports multiple label variations:
- `environment=production` or `env=production`
- `environment=prod` or `env=prod`

This ensures compatibility with different cluster labeling conventions.
