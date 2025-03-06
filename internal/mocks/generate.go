// Package mocks contains mock implementations of interfaces used in the SkyVault project.
// This file is not used for anything except generating mocks - it shouldn't be imported.
// Execute `go generate ./internal/mocks/generate.go` to regenerate all mocks.
package mocks

//go:generate go tool mockgen -destination=mock_database.go -package=mocks github.com/dynoinc/skyvault/internal/database Querier
//go:generate go tool mockgen -destination=mock_k8s.go -package=mocks k8s.io/client-go/kubernetes Interface
//go:generate go tool mockgen -destination=mock_cache_client.go -package=mocks github.com/dynoinc/skyvault/gen/proto/cache/v1/v1connect CacheServiceClient
