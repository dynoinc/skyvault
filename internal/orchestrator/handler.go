package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	v1connect "github.com/dynoinc/skyvault/gen/proto/orchestrator/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/background"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// Config holds the configuration for the orchestrator service
type Config struct {
	Enabled bool `default:"false"`
}

// handler implements the OrchestratorService
type handler struct {
	v1connect.UnimplementedOrchestratorServiceHandler

	config      Config
	ctx         context.Context
	riverClient *river.Client[pgx.Tx]
}

// NewHandler creates a new orchestrator service handler
func NewHandler(
	ctx context.Context,
	cfg Config,
	db *pgxpool.Pool,
) (*handler, error) {
	slog.InfoContext(ctx, "initializing orchestrator service", "enabled", cfg.Enabled)

	riverClient, err := background.New(db, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create river client: %w", err)
	}

	h := &handler{
		config:      cfg,
		ctx:         ctx,
		riverClient: riverClient,
	}

	go h.processLoop()
	return h, nil
}

func (h *handler) processLoop() {
	<-h.ctx.Done()
}
