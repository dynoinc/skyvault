package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	v1connect "github.com/dynoinc/skyvault/gen/proto/orchestrator/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/background"
	"github.com/dynoinc/skyvault/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Config holds the configuration for the orchestrator service
type Config struct {
	Enabled        bool  `default:"false"`
	MaxL0Batches   int   `default:"4"`
	MaxL0BatchSize int64 `default:"67108864"` // 64MB
}

// handler implements the OrchestratorService
type handler struct {
	v1connect.UnimplementedOrchestratorServiceHandler

	config      Config
	ctx         context.Context
	riverClient *river.Client[pgx.Tx]
	db          *pgxpool.Pool
}

// NewHandler creates a new orchestrator service handler
func NewHandler(
	ctx context.Context,
	cfg Config,
	db *pgxpool.Pool,
) (*handler, error) {
	slog.InfoContext(ctx, "initializing orchestrator service", "enabled", cfg.Enabled)

	workers := river.NewWorkers()
	river.AddWorker(workers, &background.MergeL0BatchesWorker{})

	riverClient, err := river.NewClient(riverpgxv5.New(db), &river.Config{
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create river client: %w", err)
	}

	h := &handler{
		config:      cfg,
		ctx:         ctx,
		riverClient: riverClient,
		db:          db,
	}

	go h.processLoop()
	return h, nil
}

func (h *handler) maybeScheduleMergeJob(l0Batches []database.L0Batch) error {
	if len(l0Batches) <= h.config.MaxL0Batches {
		return nil
	}

	var batchesToMerge []int64
	var totalSize int64

	// Sort batches by ID (descending)
	sort.Slice(l0Batches, func(i, j int) bool {
		return l0Batches[i].ID > l0Batches[j].ID
	})

	// Select the latest set of ACTIVE batches that make up max batch size
	for _, batch := range l0Batches {
		// Stop as soon as we find a non-active batch
		if *batch.Status != "ACTIVE" {
			break
		}

		batchesToMerge = append(batchesToMerge, batch.ID)
		totalSize += batch.SizeBytes

		// Once we have enough batches to merge, stop
		if totalSize >= h.config.MaxL0BatchSize {
			break
		}
	}

	if len(batchesToMerge) <= 1 {
		return nil
	}

	tx, err := h.db.Begin(h.ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(h.ctx)

	qtx := database.New(tx)

	// Lock all these batches and insert job in 1 txn.
	locked, err := qtx.UpdateL0BatchesStatus(h.ctx, database.UpdateL0BatchesStatusParams{
		BatchIds:      batchesToMerge,
		CurrentStatus: "ACTIVE",
		NewStatus:     "LOCKED",
	})
	if err != nil {
		return fmt.Errorf("failed to lock L0 batches: %w", err)
	}

	if locked != int64(len(batchesToMerge)) {
		return fmt.Errorf("failed to lock all L0 batches: %d", locked)
	}

	_, err = h.riverClient.InsertTx(h.ctx, tx, background.MergeL0BatchesArgs{
		BatchIDs: batchesToMerge,
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to schedule merge job: %w", err)
	}

	slog.InfoContext(h.ctx, "scheduled merge job", "batchCount", len(batchesToMerge), "totalSize", totalSize)
	return nil
}

func (h *handler) processLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l0Batches, err := database.New(h.db).GetL0Batches(h.ctx)
			if err != nil {
				slog.ErrorContext(h.ctx, "failed to get all L0 batches", "error", err)
				continue
			}

			if err := h.maybeScheduleMergeJob(l0Batches); err != nil {
				slog.ErrorContext(h.ctx, "failed to schedule merge job", "error", err)
			}

		case <-h.ctx.Done():
		}
	}
}
