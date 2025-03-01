package orchestrator

import (
	"context"
	"log/slog"

	v1connect "github.com/dynoinc/skyvault/gen/proto/orchestrator/v1/v1connect"
)

// Config holds the configuration for the orchestrator service
type Config struct {
	Enabled bool `default:"false"`
}

// handler implements the OrchestratorService
type handler struct {
	v1connect.UnimplementedOrchestratorServiceHandler

	config Config
	ctx    context.Context
}

// NewHandler creates a new orchestrator service handler
func NewHandler(
	ctx context.Context,
	cfg Config,
) *handler {
	slog.InfoContext(ctx, "initializing orchestrator service", "enabled", cfg.Enabled)

	return &handler{
		config: cfg,
		ctx:    ctx,
	}
}
