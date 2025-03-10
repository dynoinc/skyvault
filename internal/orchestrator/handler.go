package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	commonv1 "github.com/dynoinc/skyvault/gen/proto/common/v1"
	v1 "github.com/dynoinc/skyvault/gen/proto/orchestrator/v1"
	"github.com/dynoinc/skyvault/gen/proto/orchestrator/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/background"
	"github.com/dynoinc/skyvault/internal/database"
)

// Config holds the configuration for the orchestrator service
type Config struct {
	Enabled bool `default:"false"`

	MinL0Batches         int `default:"16"`       // minimum number of batches beyond which we merge.
	MinL0MergedBatchSize int `default:"16777216"` // minimum size of merged batch.
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

	if err := database.New(db).InitPartitions(ctx, commonv1.Partition_builder{}.Build()); err != nil {
		return nil, fmt.Errorf("failed to initialize partitions: %w", err)
	}

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

func mergeableBatches(l0Batches []database.L0Batch, minL0Batches int, minL0MergedBatchSize int) ([]database.L0Batch, int64) {
	// Sort batches by SeqNo (ascending)
	sort.Slice(l0Batches, func(i, j int) bool {
		return l0Batches[i].SeqNo < l0Batches[j].SeqNo
	})

	for len(l0Batches) > 0 && l0Batches[0].Attrs.GetState() != commonv1.L0Batch_NEW {
		l0Batches = l0Batches[1:]
	}

	var batchesToMerge []database.L0Batch
	var totalSize int64
	for len(l0Batches) > 0 && totalSize < int64(minL0MergedBatchSize) {
		batchesToMerge = append(batchesToMerge, l0Batches[0])
		totalSize += l0Batches[0].Attrs.GetSizeBytes()
		l0Batches = l0Batches[1:]
	}

	if len(batchesToMerge) < minL0Batches && totalSize < int64(minL0MergedBatchSize) {
		return nil, 0
	}

	return batchesToMerge, totalSize
}

func (h *handler) maybeScheduleMergeJob(l0Batches []database.L0Batch) error {
	batchesToMerge, totalSize := mergeableBatches(
		l0Batches,
		h.config.MinL0Batches,
		h.config.MinL0MergedBatchSize,
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

	// Lock all these batches and insert job in 1 txn. We send updated batches to river
	// so that it has the latest version of the batch.
	updatedBatchesToMerge := make([]database.L0Batch, len(batchesToMerge))
	for i, batch := range batchesToMerge {
		updatedBatchesToMerge[i], err = qtx.UpdateL0Batch(h.ctx, database.UpdateL0BatchParams{
			SeqNo:   batch.SeqNo,
			Version: batch.Version,
			Attrs:   commonv1.L0Batch_builder{State: commonv1.L0Batch_MERGING}.Build(),
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
		Batches: updatedBatchesToMerge,
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

func (h *handler) ListL0Batches(context.Context, *connect.Request[v1.ListL0BatchesRequest]) (*connect.Response[v1.ListL0BatchesResponse], error) {
	l0Batches, err := database.New(h.db).GetL0Batches(h.ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get L0 batches: %w", err))
	}

	protoBatches := make([]*v1.L0Batch, len(l0Batches))
	for i, batch := range l0Batches {
		protoBatches[i] = v1.L0Batch_builder{
			SeqNo:   batch.SeqNo,
			Version: batch.Version,
			Attrs:   batch.Attrs,
		}.Build()
	}

	slices.SortFunc(protoBatches, func(a, b *v1.L0Batch) int {
		return int(a.GetSeqNo()) - int(b.GetSeqNo())
	})

	return connect.NewResponse(v1.ListL0BatchesResponse_builder{
		L0Batches: protoBatches,
	}.Build()), nil
}
