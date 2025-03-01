# Mocks for Testing

This directory contains mock implementations of interfaces used in the SkyVault project, generated using [go.uber.org/mock](https://github.com/uber-go/mock).

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

## Generating New Mocks

To generate a new mock for an interface, use the `go tool mockgen` command:

```bash
# Example: generate mock for an interface in your project
go tool mockgen -destination=internal/mocks/mock_myinterface.go -package=mocks github.com/dynoinc/skyvault/internal/mypackage MyInterface

# Example: generate mock for an external interface
go tool mockgen -destination=internal/mocks/mock_external.go -package=mocks external.package/path ExternalInterface
```

## Regenerating Existing Mocks

If an interface changes, you can regenerate the mock with the same command used to create it originally:

```bash
go tool mockgen -destination=internal/mocks/mock_database.go -package=mocks github.com/dynoinc/skyvault/internal/database Querier
``` 