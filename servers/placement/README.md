# Placement MCP Server

A Model Context Protocol (MCP) server for Open Cluster Management that enables AI-powered placement generation and cluster discovery.

## Features

- **AI-Powered Placement Generation**: Generate OCM Placement YAML from natural language descriptions
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

The server provides four main tools:

1. **list_clusters** - List all managed clusters with optional label filtering
2. **get_cluster** - Get detailed information about a specific cluster
3. **generate_placement** - Generate Placement YAML from natural language and perform dry run
4. **dryrun_placement** - Preview which clusters would be selected by a Placement

## Example Usage

```
User: "Deploy to all production clusters in us-east with OpenShift >= 4.18"

AI: [Uses generate_placement tool to create Placement YAML]
    - Generates OCM Placement with appropriate predicates
    - Performs dry run to show which clusters match
    - Returns both YAML and selected clusters
```

## Requirements

- Go 1.25.0 or higher
- Access to an OCM hub cluster
- Valid kubeconfig with permissions to list/read ManagedCluster resources

## License

Apache License 2.0
