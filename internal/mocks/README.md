# Mocks for Testing

This directory contains mock implementations of interfaces used in the SkyVault project, generated
using [go.uber.org/mock](https://github.com/uber-go/mock).

## Available Mocks

- `mock_database.go`: Mocks for the `database.Querier` interface
- `mock_k8s.go`: Mocks for the Kubernetes client interface
- `mock_cache_client.go`: Mocks for the `CacheServiceClient` interface

## How to Use

Here's a simple example of how to use these mocks in your tests:

```go
import (
"context"
"testing"

"github.com/dynoinc/skyvault/internal/mocks"
"go.uber.org/mock/gomock"
)

func TestMyFunction(t *testing.T) {
// Create a new controller
ctrl := gomock.NewController(t)
defer ctrl.Finish()

// Create a mock database
mockDB := mocks.NewMockQuerier(ctrl)

// Set expectations
mockDB.EXPECT().
GetAllL0Batches(gomock.Any()).
Return([]database.L0Batch{
{
ID:   1,
Path: "batch-1",
},
}, nil)

// Use the mock in your code
// ...
}
```

## Generating Mocks Using go:generate

This project uses Go's `go:generate` tool to automate mock generation. To regenerate all mocks:

```bash
# Regenerate all mocks in the project
go generate ./...

# Or specifically regenerate mocks in the mocks package
go generate ./internal/mocks/generate.go
```

### How go:generate Works

Mock generation commands are stored as `//go:generate` directives in:

1. The source files that define the interfaces (for our own interfaces)
2. The `generate.go` file in the mocks directory (for all interfaces in one place)
3. Separate mock-specific files for external packages

### Adding a New Mock

To add a new interface mock:

1. Add a new `//go:generate` directive to `internal/mocks/generate.go`
2. Run `go generate ./internal/mocks/generate.go`

Example directive:

```go
//go:generate go tool mockgen -destination=mock_myinterface.go -package=mocks github.com/dynoinc/skyvault/internal/mypackage MyInterface
```

## Manual Mock Generation

You can still manually generate mocks using the `go tool mockgen` command:

```bash
# Example: generate mock for an interface in your project
go tool mockgen -destination=internal/mocks/mock_myinterface.go -package=mocks github.com/dynoinc/skyvault/internal/mypackage MyInterface

# Example: generate mock for an external interface
go tool mockgen -destination=internal/mocks/mock_external.go -package=mocks external.package/path ExternalInterface
``` 