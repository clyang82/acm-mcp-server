# Deploying Placement MCP Server to Kubernetes

This guide explains how to deploy the Placement MCP Server as a pod in your ACM hub cluster.

## Prerequisites

- The `open-cluster-management` namespace exists in your cluster

## Step 1: Build the Container Image

From the repository root directory:

```bash
# Build the Docker image
docker build -f Dockerfile -t placement-mcp:latest .

# Or using Podman
podman build -f Dockerfile -t placement-mcp:latest .
```

## Step 2: Push to Container Registry

```bash
# Tag the image for your registry
docker tag placement-mcp:latest quay.io/YOUR_USERNAME/placement-mcp:latest

# Login to your registry
docker login quay.io

# Push the image
docker push quay.io/YOUR_USERNAME/placement-mcp:latest
```

**Note:** Update `YOUR_USERNAME` with your actual registry username.

## Step 3: Update Deployment Manifest

Edit `deploy/deployment.yaml` and replace `YOUR_QUAY_USERNAME` with your actual username:

```yaml
image: quay.io/YOUR_USERNAME/placement-mcp:latest
```

## Step 4: Deploy to Kubernetes

```bash

# Deploy RBAC resources
kubectl apply -f deploy/rbac.yaml

# Deploy the MCP server
kubectl apply -f deploy/deployment.yaml
```

## Step 5: Verify Deployment

```bash
# Check if the pod is running
kubectl get pods -n open-cluster-management -l app=placement-mcp

# Check logs
kubectl logs -n open-cluster-management -l app=placement-mcp

# Describe the deployment
kubectl describe deployment placement-mcp -n open-cluster-management
```
