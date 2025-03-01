package worker

import (
	"context"
	"log/slog"

	v1connect "github.com/dynoinc/skyvault/gen/proto/worker/v1/v1connect"
)

// Config holds the configuration for the worker service
type Config struct {
	Enabled bool `default:"false"`
}

// handler implements the WorkerService
type handler struct {
	v1connect.UnimplementedWorkerServiceHandler

	config Config
	ctx    context.Context
}

// NewHandler creates a new worker service handler
func NewHandler(
	ctx context.Context,
	cfg Config,
) *handler {
	slog.InfoContext(ctx, "initializing worker service", "enabled", cfg.Enabled)

	return &handler{
		config: cfg,
		ctx:    ctx,
	}
}
