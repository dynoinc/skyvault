package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	v1connect "github.com/dynoinc/skyvault/gen/proto/orchestrator/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/background"
	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/database/dto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Config holds the configuration for the orchestrator service
type Config struct {
	Enabled              bool `default:"false"`
	MinL0Batches         int  `default:"4"`        // number of batches we are ok with in L0 level.
	MaxL0Batches         int  `default:"16"`       // number of batches beyond which we merge even if target size is not met.
	MaxL0MergedBatchSize int  `default:"67108864"` // number of bytes we try to limit merged batch size to.
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

func mergeableBatches(l0Batches []database.L0Batch, minL0Batches int, maxL0Batches int, maxL0MergedBatchSize int) ([]database.L0Batch, int64) {
	// Sort batches by SeqNo (ascending)
	sort.Slice(l0Batches, func(i, j int) bool {
		return l0Batches[i].SeqNo < l0Batches[j].SeqNo
	})

	for len(l0Batches) > 0 && l0Batches[0].Attrs.State != dto.StateCommitted {
		l0Batches = l0Batches[1:]
	}

	// If we have fewer than minL0Batches, nothing to do.
	if len(l0Batches) <= minL0Batches {
		return nil, 0
	}

	var batchesToMerge []database.L0Batch
	var totalSize int64
	for len(l0Batches) > 0 && totalSize < int64(maxL0MergedBatchSize) {
		batchesToMerge = append(batchesToMerge, l0Batches[0])
		totalSize += l0Batches[0].Attrs.SizeBytes
		l0Batches = l0Batches[1:]
	}

	if len(batchesToMerge) > maxL0Batches || totalSize > int64(maxL0MergedBatchSize) {
		return batchesToMerge, totalSize
	}

	return nil, 0
}

func (h *handler) maybeScheduleMergeJob(l0Batches []database.L0Batch) error {
	batchesToMerge, totalSize := mergeableBatches(
		l0Batches,
		h.config.MinL0Batches,
		h.config.MaxL0Batches,
		h.config.MaxL0MergedBatchSize,
	)
	if batchesToMerge == nil {
		return nil
	}

	tx, err := h.db.Begin(h.ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(h.ctx)

	qtx := database.New(tx)

	// Lock all these batches and insert job in 1 txn.
	for _, batch := range batchesToMerge {
		_, err := qtx.UpdateL0Batch(h.ctx, database.UpdateL0BatchParams{
			SeqNo:   batch.SeqNo,
			Version: batch.Version,
			Attrs:   dto.L0BatchAttrs{State: dto.StateMerging},
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.WarnContext(h.ctx, "concurrent update, skipping", "seqNo", batch.SeqNo, "version", batch.Version)
				return nil
			}

			return fmt.Errorf("failed to update L0 batches: %w", err)
		}
	}

	_, err = h.riverClient.InsertTx(h.ctx, tx, background.MergeL0BatchesArgs{
		Batches: batchesToMerge,
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to schedule merge job: %w", err)
	}

	slog.InfoContext(h.ctx, "scheduled merge job", "batchCount", len(batchesToMerge), "totalSize", totalSize)
	return tx.Commit(h.ctx)
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
