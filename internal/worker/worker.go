package worker

import (
	"context"
	"fmt"
	"log/slog"

	v1connect "github.com/dynoinc/skyvault/gen/proto/worker/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/background"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/thanos-io/objstore"
)

// Config holds the configuration for the worker service
type Config struct {
	Enabled    bool `default:"false"`
	NumWorkers int  `default:"10"`
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
	db *pgxpool.Pool,
	objstore objstore.Bucket,
) (*handler, error) {
	slog.InfoContext(ctx, "initializing worker service", "enabled", cfg.Enabled)

	workers := river.NewWorkers()
	river.AddWorker(workers, background.NewMergeL0BatchesWorker(db, objstore))
	riverClient, err := river.NewClient(riverpgxv5.New(db), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {
				MaxWorkers: cfg.NumWorkers,
			},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create river client: %w", err)
	}

	go riverClient.Start(ctx)
	return &handler{
		config: cfg,
		ctx:    ctx,
	}, nil
}
