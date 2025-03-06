gen:
    go tool buf generate --template proto/buf.gen.yaml
    go tool sqlc generate -f ./internal/database/sqlc.yaml
    go generate ./...
    go mod tidy

lint: gen
    go fmt ./...
    go vet ./...
    go tool staticcheck ./...
    go tool govulncheck

test: lint
  go mod verify
  go build ./...
  go test -v -race ./...

# Fast development mode using minimal container image with just the binary
k8s-dev:
    # Build the binary for Linux (since k8s nodes run Linux)
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./bin/skyvault ./cmd/skyvault
    
    # Build a minimal container image with just the binary
    podman build --no-cache -t skyvault:dev -f Dockerfile.dev .
    
    # Save the container image to a tar file
    podman save skyvault:dev -o ./bin/skyvault-dev.tar
    
    # Copy the tar file to Minikube VM and load it using ssh
    minikube cp ./bin/skyvault-dev.tar /tmp/skyvault-dev.tar
    minikube ssh "docker load < /tmp/skyvault-dev.tar"
    
    # Deploy with dev settings using the minimal image
    helm upgrade --install skyvault ./helm/skyvault \
        --values ./helm/skyvault/values.yaml
    
    # Restart the deployments to pick up changes
    kubectl rollout restart deployment/skyvault-batcher
    kubectl rollout restart deployment/skyvault-cache-default
    kubectl rollout restart deployment/skyvault-index-default
    kubectl rollout restart deployment/skyvault-worker
    kubectl rollout restart deployment/skyvault-orchestrator
    
    # Wait for deployments to finish rolling out
    kubectl rollout status -w deployment/skyvault-batcher
    kubectl rollout status -w deployment/skyvault-cache-default
    kubectl rollout status -w deployment/skyvault-index-default
    kubectl rollout status -w deployment/skyvault-worker
    kubectl rollout status -w deployment/skyvault-orchestrator
    
    # Show running pods
    kubectl get pods

k8s-reset:
    helm uninstall skyvault || true

k8s-logs:
    kubectl logs -f -l "app.kubernetes.io/component in (batcher,index,cache,worker,orchestrator)" --max-log-requests 1000

# Run stress test against the Kubernetes-deployed batcher service
k8s-stress:
    #!/usr/bin/env sh
    set -e
    
    # Build the stress binary
    echo "Building stress binary..."
    go build -o ./bin/stress ./cmd/stress
    
    # Default parameters - can be overridden with env vars
    CONCURRENCY=${CONCURRENCY:-10}
    DURATION=${DURATION:-10s}
    KEY_SIZE=${KEY_SIZE:-16}
    VALUE_SIZE=${VALUE_SIZE:-100}
    BATCH_SIZE=${BATCH_SIZE:-10}
    NAMESPACE=${NAMESPACE:-default}
    SERVICE=${SERVICE:-both}
    BATCHER_NAME=${BATCHER_NAME:-skyvault-batcher}
    INDEX_NAME=${INDEX_NAME:-skyvault-index-default}
    READ_MODE=${READ_MODE:-mixed}
    DEBUG=${DEBUG:-false}
    
    echo "Running stress test against Kubernetes services (${SERVICE})..."
    ./bin/stress \
      --namespace="$NAMESPACE" \
      --service="$SERVICE" \
      --batcher-name="$BATCHER_NAME" \
      --index-name="$INDEX_NAME" \
      --concurrency="$CONCURRENCY" \
      --duration="$DURATION" \
      --key-size="$KEY_SIZE" \
      --value-size="$VALUE_SIZE" \
      --batch-size="$BATCH_SIZE" \
      --read-mode="$READ_MODE" \
      --debug="$DEBUG"
