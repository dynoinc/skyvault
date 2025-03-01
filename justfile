gen:
    go generate ./...
    go tool buf generate --template proto/buf.gen.yaml
    go tool sqlc generate -f ./internal/database/sqlc.yaml
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

# Fast development mode using minimal Docker image with just the binary
k8s-dev:
    # Build the binary for Linux (since k8s nodes run Linux)
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./bin/skyvault ./cmd/skyvault
    
    # Build a minimal Docker image with just the binary
    docker build --no-cache -t skyvault:dev -f Dockerfile.dev .
    
    # Save the Docker image to a tar file
    docker save skyvault:dev -o ./bin/skyvault-dev.tar
    
    # Copy the tar file to Minikube VM and load it using ssh
    minikube cp ./bin/skyvault-dev.tar /tmp/skyvault-dev.tar
    minikube ssh "docker load < /tmp/skyvault-dev.tar"
    
    # Deploy with dev settings using the minimal image
    helm upgrade --install skyvault ./helm/skyvault \
        --values ./helm/skyvault/values.yaml
    
    # Restart the deployments to pick up changes
    kubectl rollout restart deployment/skyvault-batcher
    kubectl rollout restart deployment/skyvault-cache
    kubectl rollout restart deployment/skyvault-index
    
    # Show running pods
    kubectl get pods

k8s-reset:
    # Uninstall existing Helm release
    helm uninstall skyvault || true

k8s-logs:
    # Tail logs for all components (batcher, index, and cache)
    kubectl logs -f -l "app.kubernetes.io/component in (batcher,index,cache)" --max-log-requests 1000
