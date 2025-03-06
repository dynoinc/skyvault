package batcher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/dynoinc/skyvault/gen/proto/batcher/v1"
	"github.com/dynoinc/skyvault/gen/proto/batcher/v1/v1connect"
	commonv1 "github.com/dynoinc/skyvault/gen/proto/common/v1"
	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/recordio"
	"github.com/lithammer/shortuuid/v4"
	"github.com/thanos-io/objstore"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Config struct {
	Enabled       bool          `default:"false"`
	MaxBatchBytes int           `default:"4194304"`
	MaxBatchAge   time.Duration `default:"500ms"`
	MaxConcurrent int           `default:"4"`
}

type batch struct {
	records   []recordio.Record
	callbacks []chan error
	totalSize int
}

type writeRequest struct {
	req      *v1.BatchWriteRequest
	callback chan error
}

type handler struct {
	v1connect.UnimplementedBatcherServiceHandler

	config Config
	ctx    context.Context
	db     database.Querier
	store  objstore.Bucket

	// Channel for write requests from clients
	writes chan writeRequest

	// Channel for batches to be flushed
	processing chan *batch

	// for graceful shutdown
	done chan struct{}
}

func NewHandler(
	ctx context.Context,
	cfg Config,
	db database.Querier,
	store objstore.Bucket,
) *handler {
	h := &handler{
		config: cfg,
		ctx:    ctx,
		db:     db,
		store:  store,

		processing: make(chan *batch, cfg.MaxConcurrent),
		writes:     make(chan writeRequest),
		done:       make(chan struct{}),
	}

	go h.processLoop()
	go h.batchLoop()
	return h
}

func (h *handler) BatchWrite(
	ctx context.Context,
	req *connect.Request[v1.BatchWriteRequest],
) (*connect.Response[v1.BatchWriteResponse], error) {
	if req.Msg.GetWrites() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("writes is required"))
	}

	// reject empty key
	for _, w := range req.Msg.GetWrites() {
		if w.GetKey() == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("key is required"))
		}
	}

	callback := make(chan error, 1)
	wr := writeRequest{
		req:      req.Msg,
		callback: callback,
	}

	// Send write request to the batch manager goroutine
	select {
	case h.writes <- wr:
	case <-h.ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, h.ctx.Err())
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	}

	// Wait for processing to complete
	select {
	case err := <-callback:
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	}

	return connect.NewResponse(&v1.BatchWriteResponse{}), nil
}

func (h *handler) batchLoop() {
	var current *batch
	var timer <-chan time.Time

	for {
		select {
		case wr := <-h.writes:
			if current == nil {
				current = &batch{
					records:   make([]recordio.Record, 0),
					callbacks: make([]chan error, 0),
					totalSize: 0,
				}
				t := time.NewTimer(h.config.MaxBatchAge)
				timer = t.C
			}

			current.callbacks = append(current.callbacks, wr.callback)
			for _, r := range wr.req.GetWrites() {
				record := recordio.Record{
					Key:       r.GetKey(),
					Value:     r.GetPut(),
					Tombstone: r.GetDelete(),
				}

				current.records = append(current.records, record)
				recordSize := len(record.Key)
				if record.Value != nil {
					recordSize += len(record.Value)
				}
				current.totalSize += recordSize
			}

			if current.totalSize >= h.config.MaxBatchBytes {
				h.processing <- current
				current = nil
				timer = nil
			}

		case <-timer:
			if current != nil && len(current.records) > 0 {
				h.processing <- current
				current = nil
				timer = nil
			}

		case <-h.ctx.Done():
			close(h.processing)
			if current != nil {
				// Fail all pending requests
				for _, cb := range current.callbacks {
					cb <- context.Canceled
				}
			}
			return
		}
	}
}

func (h *handler) processLoop() {
	defer close(h.done)

	for range h.config.MaxConcurrent {
		go func() {
			for batch := range h.processing {
				err := h.writeBatch(h.ctx, batch.records)

				// Notify all waiting clients
				for _, cb := range batch.callbacks {
					cb <- err
				}
			}
		}()
	}
}

func (h *handler) writeBatch(ctx context.Context, records []recordio.Record) error {
	// Sort records by key
	sort.Slice(records, func(i, j int) bool {
		return records[i].Key < records[j].Key
	})

	// Deduplicate records - keep only the last occurrence of each key
	if len(records) > 1 {
		j := 0
		for i := 1; i < len(records); i++ {
			if records[i].Key != records[j].Key {
				j++
				if i != j {
					records[j] = records[i]
				}
			} else {
				// When keys match, overwrite the previous record
				records[j] = records[i]
			}
		}
		records = records[:j+1]
	}

	// Calculate size bytes using ComputeSize
	sizeBytes := int64(recordio.ComputeSize(slices.Values(records)))

	minKey := records[0].Key
	maxKey := records[len(records)-1].Key

	// Write records to buffer
	buf := recordio.WriteRecords(slices.Values(records))

	// Generate a short UUID for the batch
	id := shortuuid.New()

	// Write batch to object store under l0_batches directory
	objPath := path.Join("l0_batches", id)
	if err := h.store.Upload(ctx, objPath, bytes.NewReader(buf)); err != nil {
		return fmt.Errorf("writing batch to storage: %w", err)
	}

	// Add record to database
	if err := h.db.AddL0Batch(ctx, commonv1.L0Batch_builder{
		Path:      proto.String(objPath),
		CreatedAt: timestamppb.New(time.Now()),
		SizeBytes: proto.Int64(sizeBytes),
		MinKey:    proto.String(minKey),
		MaxKey:    proto.String(maxKey),
	}.Build()); err != nil {
		return fmt.Errorf("adding batch record: %w", err)
	}

	return nil
}

// Shutdown waits for processing to complete
func (h *handler) Shutdown(ctx context.Context) error {
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
