package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"github.com/thanos-io/objstore"

	"github.com/dynoinc/skyvault/gen/proto/worker/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/background"
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
		WorkerMiddleware: []rivertype.WorkerMiddleware{
			&loggingMiddleware{},
		},
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

type loggingMiddleware struct {
	river.WorkerMiddlewareDefaults
}

func (m *loggingMiddleware) Work(ctx context.Context, job *rivertype.JobRow, doInner func(ctx context.Context) error) error {
	startTime := time.Now()
	slog.InfoContext(ctx, "working on job", "job_id", job.ID, "kind", job.Kind)

	defer func() {
		if r := recover(); r != nil {
			duration := time.Since(startTime)
			slog.ErrorContext(ctx, "panic working on job", "job_id", job.ID, "kind", job.Kind, "error", r, "duration", duration)
			panic(r)
		}
	}()

	err := doInner(ctx)
	duration := time.Since(startTime)
	if err != nil {
		slog.ErrorContext(ctx, "error working on job", "job_id", job.ID, "kind", job.Kind, "error", err, "duration", duration)
	} else {
		slog.InfoContext(ctx, "successfully worked on job", "job_id", job.ID, "kind", job.Kind, "duration", duration)
	}

	return err
}
