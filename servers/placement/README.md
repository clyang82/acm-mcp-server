# Placement MCP Server

A Model Context Protocol (MCP) server for Open Cluster Management that enables AI-powered placement generation and cluster discovery.

## Features

- **AI-Powered Placement Generation**: Generate OCM Placement YAML from natural language descriptions
- **VM-Aware Placement**: Generate Placement from KubeVirt VirtualMachine specs with automatic resource requirement extraction
- **Dry Run Capability**: Preview which clusters match your placement criteria before deployment
- **Cluster Discovery**: List and query managed clusters with label-based filtering
- **Smart Filtering**: Filter by environment, region, cloud provider, OpenShift version, and resource capacity

## Quick Start

### Building

```bash
# Build the server
go build -o placement-mcp ./cmd
```

### Running Locally

```bash
# Use default kubeconfig
./placement-mcp

# Specify custom kubeconfig
./placement-mcp -kubeconfig=/path/to/kubeconfig
```

### Deployment

For deploying to Kubernetes, see [deploy/DEPLOYMENT.md](deploy/DEPLOYMENT.md).

### Installing into Claude

For remote MCP server deployed on Kubernetes, use token-based authentication:

```bash
# Create a service account token
oc create token placement-mcp -n open-cluster-management

# Add the MCP server to Claude
claude mcp add \
  --env NODE_TLS_REJECT_UNAUTHORIZED=0 \
  --transport http \
  placement \
  https://placement-mcp-open-cluster-management.apps.YOUR-CLUSTER-DOMAIN/mcp \
  --header "Authorization: Bearer YOUR_TOKEN"
```

Replace:
- `YOUR-CLUSTER-DOMAIN` with your OpenShift cluster domain
- `YOUR_TOKEN` with the token from the `oc create token` command

## MCP Tools

The server provides five main tools:

1. **list_clusters** - List all managed clusters with optional label filtering
2. **get_cluster** - Get detailed information about a specific cluster
3. **generate_placement** - Generate Placement YAML from natural language and perform dry run
4. **generate_placement_from_vm** - Generate Placement YAML from KubeVirt VirtualMachine spec with automatic dry run
5. **dryrun_placement** - Preview which clusters would be selected by a Placement

## Example Usage

### Natural Language Placement

```
User: "Deploy to all production clusters in us-east with OpenShift >= 4.18"

AI: [Uses generate_placement tool to create Placement YAML]
    - Generates OCM Placement with appropriate predicates
    - Performs dry run to show which clusters match
    - Returns both YAML and selected clusters
```

### VirtualMachine-Based Placement

```
User: "Generate a placement for @vm.yaml"

AI: [Uses generate_placement_from_vm tool]
    - Extracts VM resource requirements (CPU cores, memory)
    - Identifies node selectors and anti-affinity rules
    - Generates intelligent Placement YAML
    - Automatically performs dry run
    - Returns Placement YAML and matching clusters

Example VM requirements extracted:
- CPU: 2 cores
- Memory: 4Gi
- Node selector: env=prod
- Anti-affinity: kubernetes.io/hostname (spread across nodes)
- Result: Selects 1 production cluster with sufficient capacity
```

## VirtualMachine Placement Features

The `generate_placement_from_vm` tool analyzes KubeVirt VirtualMachine specifications and automatically generates OCM Placements that:

- **Extract Resource Requirements**: Automatically parses CPU cores and memory requests from VM domain specs
- **Map Node Selectors**: Converts VM node selectors to cluster labels/claims
- **Handle Anti-Affinity**: Detects pod anti-affinity rules and adjusts cluster selection accordingly
  - `kubernetes.io/hostname` → Selects 1 cluster (spread VMs across nodes within cluster)
  - Other topology keys → Adjusts cluster count as needed
- **Intelligent Prioritization**: Uses AddOnPlacementScore (real-time metrics) when available, falls back to BuiltIn scores
- **Automatic Dry Run**: Shows which clusters match the VM requirements before deployment

### How It Works

1. Parses VirtualMachine YAML using official KubeVirt API (`kubevirt.io/api/core/v1`)
2. Extracts placement requirements from `spec.template.spec`:
   - Domain CPU cores and memory
   - Node selectors
   - Affinity/anti-affinity rules
3. Generates CEL expressions for capacity constraints
4. Creates prioritizers for optimal cluster selection
5. Performs automatic dry run to validate placement

## Requirements

- Go 1.25.0 or higher
- Access to an OCM hub cluster
- Valid kubeconfig with permissions to list/read ManagedCluster resources
- KubeVirt API v1.4.0 (for VM-based placement generation)

## License

Apache License 2.0
