package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	v1 "github.com/dynoinc/skyvault/gen/proto/orchestrator/v1"
	"github.com/dynoinc/skyvault/gen/proto/orchestrator/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/background"
	"github.com/dynoinc/skyvault/internal/database"
)

// Config holds the configuration for the orchestrator service
type Config struct {
	Enabled bool `default:"false"`

	MinWALsToCompact int `default:"16"`
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
	river.AddWorker(workers, &background.CompactWALsWorker{})

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

func (h *handler) maybeCompactWAL(wals []database.WriteAheadLog) error {
	if len(wals) < h.config.MinWALsToCompact {
		return nil
	}

	seqNos := make([]int64, len(wals))
	for i, batch := range wals {
		seqNos[i] = batch.SeqNo
	}

	tx, err := h.db.Begin(h.ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(h.ctx)

	qtx := database.New(tx)
	if _, err := qtx.MarkWriteAheadLogsAsCompacting(h.ctx, seqNos); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.WarnContext(h.ctx, "concurrent update, skipping", "seqNo", seqNos)
			return nil
		}

		return fmt.Errorf("failed to update WALs: %w", err)
	}

	if _, err = h.riverClient.InsertTx(h.ctx, tx, background.CompactWALsArgs{
		WALs: wals,
	}, nil); err != nil {
		return fmt.Errorf("failed to schedule merge job: %w", err)
	}

	slog.InfoContext(h.ctx, "scheduled merge job", "count", len(wals))
	return tx.Commit(h.ctx)
}

func (h *handler) processLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			wals, err := database.New(h.db).GetWriteAheadLogsToCompact(h.ctx)
			if err != nil {
				slog.ErrorContext(h.ctx, "failed to get all L0 batches", "error", err)
				continue
			}

			if err := h.maybeCompactWAL(wals); err != nil {
				slog.ErrorContext(h.ctx, "failed to schedule merge job", "error", err)
			}

		case <-h.ctx.Done():
		}
	}
}

func (h *handler) ListWriteAheadLogs(context.Context, *connect.Request[v1.ListWriteAheadLogsRequest]) (*connect.Response[v1.ListWriteAheadLogsResponse], error) {
	wals, err := database.New(h.db).GetWriteAheadLogs(h.ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get write ahead logs: %w", err))
	}

	protoBatches := make([]*v1.WriteAheadLog, len(wals))
	for i, batch := range wals {
		protoBatches[i] = v1.WriteAheadLog_builder{
			SeqNo: batch.SeqNo,
			Attrs: batch.Attrs,
		}.Build()
	}

	slices.SortFunc(protoBatches, func(a, b *v1.WriteAheadLog) int {
		return int(a.GetSeqNo()) - int(b.GetSeqNo())
	})

	return connect.NewResponse(v1.ListWriteAheadLogsResponse_builder{
		WriteAheadLogs: protoBatches,
	}.Build()), nil
}
