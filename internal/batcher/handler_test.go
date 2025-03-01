package batcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/dynoinc/skyvault/gen/proto/batcher/v1"
	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/storage"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

type mockQuerier struct {
	batches []database.L0Batch
	mu      sync.Mutex
}

func (m *mockQuerier) AddL0Batch(ctx context.Context, path string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := int64(len(m.batches) + 1)
	m.batches = append(m.batches, database.L0Batch{
		ID:   id,
		Path: path,
	})
	return id, nil
}

func (m *mockQuerier) GetAllL0Batches(ctx context.Context) ([]database.L0Batch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.batches, nil
}

func newTestHandler(t *testing.T) (*handler, *mockQuerier, objstore.Bucket) {
	ctx := t.Context()
	db := &mockQuerier{}
	store, err := storage.New(ctx, "inmemory://")
	require.NoError(t, err)

	handler := NewHandler(ctx, Config{
		Enabled:       true,
		MaxBatchBytes: 20, // Small size for testing
		MaxBatchAge:   100 * time.Millisecond,
		MaxConcurrent: 2,
	}, db, store)

	return handler, db, store
}

func TestSingleWrite(t *testing.T) {
	handler, db, _ := newTestHandler(t)
	ctx := t.Context()

	batches, err := db.GetAllL0Batches(ctx)
	require.NoError(t, err)
	require.Empty(t, batches)

	req := v1.BatchWriteRequest_builder{
		Writes: []*v1.WriteRequest{
			func() *v1.WriteRequest {
				req := v1.WriteRequest_builder{}.Build()
				req.SetKey("key1")
				req.SetPut([]byte("value1"))
				return req
			}(),
		},
	}.Build()

	_, err = handler.BatchWrite(ctx, connect.NewRequest(req))
	require.NoError(t, err)

	batches, err = db.GetAllL0Batches(ctx)
	require.NoError(t, err)
	require.Len(t, batches, 1)
}

func TestBatchBySize(t *testing.T) {
	handler, db, _ := newTestHandler(t)
	ctx := t.Context()

	// First request with a record of size 15 bytes
	req1 := v1.BatchWriteRequest_builder{
		Writes: []*v1.WriteRequest{
			func() *v1.WriteRequest {
				req := v1.WriteRequest_builder{}.Build()
				req.SetKey("key1")
				req.SetPut([]byte("value12345678")) // 15 bytes total (4 + 11)
				return req
			}(),
		},
	}.Build()

	_, err := handler.BatchWrite(ctx, connect.NewRequest(req1))
	require.NoError(t, err)

	// Second request that will push it over the limit (MaxBatchBytes = 20)
	// This should create a second batch
	req2 := v1.BatchWriteRequest_builder{
		Writes: []*v1.WriteRequest{
			func() *v1.WriteRequest {
				req := v1.WriteRequest_builder{}.Build()
				req.SetKey("key2")
				req.SetPut([]byte("value2")) // 10 bytes total (4 + 6)
				return req
			}(),
		},
	}.Build()

	_, err = handler.BatchWrite(ctx, connect.NewRequest(req2))
	require.NoError(t, err)

	// Verify two batches were created
	batches, err := db.GetAllL0Batches(ctx)
	require.NoError(t, err)
	require.Len(t, batches, 2)
}

func TestGracefulShutdown(t *testing.T) {
	handler, db, _ := newTestHandler(t)
	ctx := t.Context()

	errCh := make(chan error)
	go func() {
		req := v1.BatchWriteRequest_builder{
			Writes: []*v1.WriteRequest{
				func() *v1.WriteRequest {
					req := v1.WriteRequest_builder{}.Build()
					req.SetKey("key5")
					req.SetPut([]byte("value5"))
					return req
				}(),
			},
		}.Build()
		_, err := handler.BatchWrite(ctx, connect.NewRequest(req))
		errCh <- err
	}()
	require.NoError(t, <-errCh)

	batches, err := db.GetAllL0Batches(ctx)
	require.NoError(t, err)
	require.Len(t, batches, 1)

	err = handler.Shutdown(ctx)
	require.NoError(t, err)
}
