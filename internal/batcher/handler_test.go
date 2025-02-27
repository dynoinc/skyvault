package batcher

import (
	"context"
	"sync"
	"testing"
	"time"

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
		MaxBatchSize:  4,
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

	_, err = handler.BatchWrite(ctx, v1.BatchWriteRequest_builder{
		Writes: []*v1.WriteRequest{
			v1.WriteRequest_builder{
				Key: []byte("key1"),
				Put: []byte("value1"),
			}.Build(),
		},
	}.Build())
	require.NoError(t, err)

	batches, err = db.GetAllL0Batches(ctx)
	require.NoError(t, err)
	require.Len(t, batches, 1)
}

func TestBatchBySize(t *testing.T) {
	handler, db, _ := newTestHandler(t)
	ctx := t.Context()

	errCh := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			_, err := handler.BatchWrite(ctx, v1.BatchWriteRequest_builder{
				Writes: []*v1.WriteRequest{
					v1.WriteRequest_builder{
						Key: []byte("key"),
						Put: []byte("value"),
					}.Build(),
				},
			}.Build())
			errCh <- err
		}()
	}

	for i := 0; i < 5; i++ {
		require.NoError(t, <-errCh)
	}

	batches, err := db.GetAllL0Batches(ctx)
	require.NoError(t, err)
	require.Len(t, batches, 2) // Should create 2 batches (4 + 1 records)
}

func TestGracefulShutdown(t *testing.T) {
	handler, db, _ := newTestHandler(t)
	ctx := t.Context()

	errCh := make(chan error)
	go func() {
		_, err := handler.BatchWrite(ctx, v1.BatchWriteRequest_builder{
			Writes: []*v1.WriteRequest{
				v1.WriteRequest_builder{
					Key: []byte("key5"),
					Put: []byte("value5"),
				}.Build(),
			},
		}.Build())
		errCh <- err
	}()
	require.NoError(t, <-errCh)

	batches, err := db.GetAllL0Batches(ctx)
	require.NoError(t, err)
	require.Len(t, batches, 1)

	err = handler.Shutdown(ctx)
	require.NoError(t, err)
}
