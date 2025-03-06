# SkyVault Helm Charts

This directory contains a Helm chart for deploying SkyVault services to Kubernetes.

## Prerequisites

Before deploying, make sure you have the following prerequisites installed:

1. [Minikube](https://minikube.sigs.k8s.io/docs/start/) - A local Kubernetes cluster
2. [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) - The Kubernetes command-line tool
3. [Helm](https://helm.sh/docs/intro/install/) - The package manager for Kubernetes
4. [Podman](https://podman.io/getting-started/installation) - For building container images

## Quick Start with Justfile

This project includes a `justfile` with commands to simplify development and deployment.

### Step 1: Start Minikube

Start your Minikube cluster with adequate resources using the Podman driver:

```bash
minikube start --driver=podman --cpus=4 --memory=8g
```

### Step 2: Build and Deploy

For development, use the fast development mode which builds a minimal container image:

```bash
just k8s-dev
```

This command will:

- Build the SkyVault binary for Linux
- Create a minimal container image
- Load the image into Minikube
- Deploy all SkyVault components using Helm
- Restart and wait for all deployments
- Show the status of running pods

### Step 3: Monitor Logs

To view logs from all SkyVault components:

```bash
just k8s-logs
```

### Step 4: Run Stress Tests (Optional)

To test the deployment under load:

```bash
just k8s-stress
```

Stress test parameters can be customized using environment variables:

- `CONCURRENCY`: Number of concurrent clients (default: 10)
- `DURATION`: Test duration (default: 10s)
- `KEY_SIZE`: Size of keys in bytes (default: 16)
- `VALUE_SIZE`: Size of values in bytes (default: 100)
- `BATCH_SIZE`: Number of operations per batch (default: 10)
- `READ_MODE`: Read mode [mixed|readonly|writeonly] (default: mixed)
- `DEBUG`: Enable debug logging (default: false)

### Cleanup

To remove the SkyVault deployment:

```bash
just k8s-reset
```

## Configuration

The deployment can be customized by modifying the values in `values.yaml`. Key configuration options include:

- Resource limits and requests
- Replica counts
- Service configurations
- Storage settings

For detailed configuration options, refer to the comments in `values.yaml`.

## Development Workflow

1. Make code changes
2. Run `just k8s-dev` to rebuild and redeploy
3. Monitor logs with `just k8s-logs`
4. Test changes with `just k8s-stress`
5. Repeat as needed

## Troubleshooting

If you encounter issues:

1. Check pod status: `kubectl get pods`
2. View detailed pod logs: `kubectl logs <pod-name>`
3. Verify Minikube status: `minikube status`
4. Reset deployment if needed: `just k8s-reset`





