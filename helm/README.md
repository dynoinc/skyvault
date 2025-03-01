# SkyVault Helm Charts

This directory contains a Helm chart for deploying SkyVault services to Kubernetes.

## Prerequisites

Before deploying, make sure you have the following prerequisites installed:

1. [Minikube](https://minikube.sigs.k8s.io/docs/start/) - A local Kubernetes cluster
2. [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) - The Kubernetes command-line tool
3. [Helm](https://helm.sh/docs/intro/install/) - The package manager for Kubernetes
4. [Docker](https://docs.docker.com/get-docker/) - For building container images

## Deploying to Minikube

### Step 1: Start Minikube

Start your Minikube cluster with adequate resources for SkyVault services:

```bash
minikube start --cpus=4 --memory=8g --disk-size=20g
```

You can adjust the resources based on your machine capabilities.

### Step 2: Build and load the Docker image

Build the SkyVault Docker image from the project root:

```bash
docker build -t skyvault:latest .
```

Load the built image into Minikube's Docker daemon:

```bash
minikube image load skyvault:latest
```

### Step 3: Enable the necessary add-ons

Enable the storage add-on for persistent volumes:

```bash
minikube addons enable storage-provisioner
```

### Step 4: Handle dependencies (PostgreSQL and MinIO)

You have two options for handling the PostgreSQL and MinIO dependencies:

#### Option A: Pull and load the images into Minikube (if using imagePullPolicy: Never)

```bash
# Pull the images
docker pull postgres:16
docker pull minio/minio:latest

# Load them into Minikube
minikube image load postgres:16
minikube image load minio/minio:latest
```

#### Option B: Use imagePullPolicy: IfNotPresent (default in values-minikube.yaml)

With this configuration, Kubernetes will first check if the image exists locally before trying to pull it from a registry.

### Step 5: Install the Helm chart

From the project root directory, install the Helm chart using the Minikube-specific values:

```bash
helm install skyvault ./helm/skyvault -f ./helm/skyvault/values-minikube.yaml
```

If you encounter an error about missing persistence settings, make sure your values-minikube.yaml file includes both of these sections:

```yaml
database:
  persistence:
    enabled: true
    size: 1Gi

storage:
  minio:
    persistence:
      enabled: true
      size: 2Gi
```

This will deploy SkyVault with:
- 2 instances of the batcher service
- PostgreSQL 16 database
- MinIO S3-compatible storage
- Services configured as NodePort for easier access

The `values-minikube.yaml` file contains optimized settings for local development, including:
- Lower resource requests suitable for Minikube
- Development mode enabled
- NodePort services for easier access
- Configurable image pull policies

### Step 6: Verify the deployment

Check that the pods are running:

```bash
kubectl get pods
```

You should see 2 batcher pods, 1 PostgreSQL pod, and 1 MinIO pod:

```
NAME                                READY   STATUS    RESTARTS   AGE
skyvault-batcher-5b8d7f9c96-j4gt7   1/1     Running   0          1m
skyvault-batcher-5b8d7f9c96-x2pn3   1/1     Running   0          1m
skyvault-postgres-7c7b6b6c74-z8h5v  1/1     Running   0          1m
skyvault-minio-8f7b9c7d6-t5hj2      1/1     Running   0          1m
```

### Step 7: Accessing the services

#### Minikube IP and NodePorts

First, determine your Minikube IP address:

```bash
minikube ip
```

Get the NodePort assigned to each service:

```bash
kubectl get svc
```

With the NodePort configuration in the Minikube values file, you can access services directly using:
`http://<minikube-ip>:<nodeport>`

#### Batcher Service

There are two ways to access the services:

1. Using port forwarding (simplest method):

```bash
kubectl port-forward svc/skyvault-batcher 5001:80
```

Now you can access the batcher service at http://localhost:5001.

2. Using Minikube service command (creates a tunnel to the service):

```bash
minikube service skyvault-batcher
```

This will automatically open your default browser with the correct URL.

#### MinIO S3 Console

Using port forwarding:

```bash
kubectl port-forward svc/skyvault-minio 9001:9001
```

Visit http://localhost:9001 and log in with:
- Username: minioadmin
- Password: minioadmin

Alternatively, use the Minikube service command:

```bash
minikube service skyvault-minio --https=false --url
```

This will return URLs for both the API (port 9000) and Console (port 9001).

#### MinIO S3 API Endpoint

Using port forwarding:

```bash
kubectl port-forward svc/skyvault-minio 9000:9000
```

The S3 API will be available at http://localhost:9000.

## Configuration

The Helm chart can be customized using a values file. Create a file named `custom-values.yaml` with your desired configuration:

```yaml
# Example custom values file

# Increase batcher replicas
batcher:
  replicas: 3  

# Configure MinIO S3 settings
storage:
  s3:
    access_key: "customuser"
    secret_key: "custompassword"
    
# Use an external S3 service instead of deploying MinIO
storage:
  deploy: false
  s3:
    endpoint: "https://your-external-s3.example.com"
    access_key: "your-access-key"
    secret_key: "your-secret-key"
    bucket: "your-bucket-name"
    region: "us-west-1"
    secure: true

# Use an external PostgreSQL database
database:
  deploy: false
  url: "postgres://username:password@your-external-postgres:5432/dbname?sslmode=disable"
```

Apply the custom values:

```bash
helm upgrade skyvault ./helm/skyvault -f custom-values.yaml
```

## Troubleshooting

### Common Installation Errors

#### Image Pull Errors (ErrImageNeverPull)

If you see errors like `ErrImageNeverPull` for PostgreSQL or MinIO pods:

1. **Option 1**: Change the pull policy in values-minikube.yaml:
   ```yaml
   image:
     pullPolicy: IfNotPresent  # Instead of Never
   ```

2. **Option 2**: Pre-load the required images into Minikube:
   ```bash
   docker pull postgres:16
   docker pull minio/minio:latest
   minikube image load postgres:16
   minikube image load minio/minio:latest
   ```

3. **Option 3**: Use specific pull policies for each component:
   ```yaml
   database:
     image:
       pullPolicy: IfNotPresent
   storage:
     minio:
       image:
         pullPolicy: IfNotPresent
   ```

After making these changes, uninstall and reinstall the chart:
```bash
helm uninstall skyvault
helm install skyvault ./helm/skyvault -f ./helm/skyvault/values-minikube.yaml
```

#### Missing Persistence Configuration

If you encounter this error during installation:
```
Error: INSTALLATION FAILED: template: skyvault/templates/postgres-pvc.yaml:1:42: executing "skyvault/templates/postgres-pvc.yaml" at <.Values.database.persistence.enabled>: nil pointer evaluating interface {}.enabled
```

It means the persistence configuration for either the database or MinIO is missing in your values file. Fix it by updating your values-minikube.yaml to include:

```yaml
database:
  # ... other database settings
  persistence:
    enabled: true
    size: 1Gi

storage:
  # ... other storage settings
  minio:
    # ... other minio settings
    persistence:
      enabled: true
      size: 2Gi
```

Then retry the installation.

### Persistent Volume Issues

If you encounter issues with persistent volumes in Minikube:

```bash
# Check the status of persistent volumes and claims
kubectl get pv,pvc

# If the PVCs are stuck in Pending state, you may need to restart the storage provisioner
kubectl delete pod -l k8s-app=storage-provisioner -n kube-system
```

### Image Pull Issues

If Minikube has trouble finding your locally built image:

```bash
# Rebuild and reload the image to Minikube
eval $(minikube docker-env)
docker build -t skyvault:latest .
```

### MinIO Bucket Creation Failure

If the MinIO bucket creation job fails:

```bash
# Check the job status
kubectl get jobs

# Check the job pod logs
kubectl logs job/skyvault-create-bucket

# Delete the job to retry
kubectl delete job skyvault-create-bucket
```

## Uninstalling

To uninstall the chart:

```bash
helm uninstall skyvault
```

Note: This will not delete the Persistent Volume Claims. To delete them:

```bash
kubectl delete pvc -l app.kubernetes.io/instance=skyvault
```

To stop Minikube when you're done:

```bash
minikube stop
``` 