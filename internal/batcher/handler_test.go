package batcher

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/mock/gomock"

	v1 "github.com/dynoinc/skyvault/gen/proto/batcher/v1"
	commonv1 "github.com/dynoinc/skyvault/gen/proto/common/v1"
	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/mocks"
	"github.com/dynoinc/skyvault/internal/storage"
)

func newTestHandler(t *testing.T) (*handler, *mocks.MockQuerier, objstore.Bucket) {
	ctx := t.Context()
	ctrl := gomock.NewController(t)
	db := mocks.NewMockQuerier(ctrl)
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

func TestEmptyKey(t *testing.T) {
	handler, _, _ := newTestHandler(t)
	ctx := t.Context()

	req := v1.BatchWriteRequest_builder{
		Writes: []*v1.WriteRequest{
			func() *v1.WriteRequest {
				req := v1.WriteRequest_builder{}.Build()
				req.SetKey("")
				return req
			}(),
		},
	}.Build()

	_, err := handler.BatchWrite(ctx, connect.NewRequest(req))
	require.Error(t, err)
	require.Contains(t, err.Error(), "key is required")
}

func TestSingleWrite(t *testing.T) {
	handler, db, _ := newTestHandler(t)
	ctx := t.Context()

	// Setup expectations for the first GetAllL0Batches call
	db.EXPECT().GetWriteAheadLogs(gomock.Any()).Return([]database.WriteAheadLog{}, nil)

	// Expect AddWriteAheadLog to be called once
	db.EXPECT().AddWriteAheadLog(gomock.Any(), gomock.Any()).Return(nil)

	// Setup expectations for the second GetAllL0Batches call
	db.EXPECT().GetWriteAheadLogs(gomock.Any()).Return([]database.WriteAheadLog{
		{
			SeqNo: 1,
			Attrs: commonv1.WriteAheadLog_builder{
				Path: "some/path",
			}.Build(),
		},
	}, nil)

	batches, err := db.GetWriteAheadLogs(ctx)
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

	batches, err = db.GetWriteAheadLogs(ctx)
	require.NoError(t, err)
	require.Len(t, batches, 1)
}

func TestBatchBySize(t *testing.T) {
	handler, db, _ := newTestHandler(t)
	ctx := t.Context()

	// Create a matcher that will only match the final call
	finalCallCtx := ctx

	// Setup expectations - use AnyTimes but don't match the finalCallCtx
	db.EXPECT().GetWriteAheadLogs(gomock.Not(gomock.Eq(finalCallCtx))).Return([]database.WriteAheadLog{}, nil).AnyTimes()
	db.EXPECT().AddWriteAheadLog(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// For the final check, match only the finalCallCtx
	db.EXPECT().GetWriteAheadLogs(finalCallCtx).Return([]database.WriteAheadLog{
		{
			SeqNo: 1,
			Attrs: commonv1.WriteAheadLog_builder{
				Path: "some/path",
			}.Build(),
		},
		{
			SeqNo: 2,
			Attrs: commonv1.WriteAheadLog_builder{
				Path: "another/path",
			}.Build(),
		},
	}, nil)

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
	batches, err := db.GetWriteAheadLogs(finalCallCtx)
	require.NoError(t, err)
	require.Len(t, batches, 2)
}

func TestGracefulShutdown(t *testing.T) {
	handler, db, _ := newTestHandler(t)
	ctx := t.Context()

	// Create a matcher that will only match the final call
	finalCallCtx := ctx

	// Setup expectations
	db.EXPECT().GetWriteAheadLogs(gomock.Not(gomock.Eq(finalCallCtx))).Return([]database.WriteAheadLog{}, nil).AnyTimes()
	db.EXPECT().AddWriteAheadLog(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// For the final check
	db.EXPECT().GetWriteAheadLogs(finalCallCtx).Return([]database.WriteAheadLog{
		{
			SeqNo: 1,
			Attrs: commonv1.WriteAheadLog_builder{
				Path: "some/path",
			}.Build(),
		},
	}, nil)

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

	batches, err := db.GetWriteAheadLogs(finalCallCtx)
	require.NoError(t, err)
	require.Len(t, batches, 1)

	err = handler.shutdown(ctx)
	require.NoError(t, err)
}
