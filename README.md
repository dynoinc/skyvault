# skyvault

[![build](https://github.com/dynoinc/skyvault/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/dynoinc/skyvault/actions/workflows/build.yml)

Objectstore backed key-value store

## Features

### gRPC Reflection Support

SkyVault now includes gRPC reflection support via the [connectrpc/grpcreflect](https://github.com/connectrpc/grpcreflect-go) library. This enables tools like [grpcurl](https://github.com/fullstorydev/grpcurl) and [grpcui](https://github.com/fullstorydev/grpcui) to discover and interact with SkyVault's services without needing the protocol buffer definitions.

#### Using with grpcurl

Example:

```bash
# List all available services
grpcurl -plaintext localhost:5001 list

# Describe a specific service
grpcurl -plaintext localhost:5001 describe batcher.v1.BatcherService

# Call a method
grpcurl -plaintext -d '{"key": "example-key"}' localhost:5001 batcher.v1.BatcherService/Get
```

#### Using with grpcui

Start the interactive web UI:

```bash
grpcui -plaintext localhost:5001
```

This will open a browser window with a UI for interacting with all SkyVault services.
