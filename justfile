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

run: gen
        go run ./cmd/skyvault

reset:
        docker rm --force skyvault-db
        docker volume rm skyvault_data
