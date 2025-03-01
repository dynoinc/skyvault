package index

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	cachev1 "github.com/dynoinc/skyvault/gen/proto/cache/v1"
	cachev1connect "github.com/dynoinc/skyvault/gen/proto/cache/v1/v1connect"
	v1 "github.com/dynoinc/skyvault/gen/proto/index/v1"
	"github.com/dynoinc/skyvault/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mockQuerier implements database.Querier for testing
type mockQuerier struct {
	mock.Mock
}

func (m *mockQuerier) AddL0Batch(ctx context.Context, path string) (int64, error) {
	args := m.Called(ctx, path)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockQuerier) GetAllL0Batches(ctx context.Context) ([]database.L0Batch, error) {
	args := m.Called(ctx)
	return args.Get(0).([]database.L0Batch), args.Error(1)
}

// mockCacheClient mocks the cache service client
type mockCacheClient struct {
	mock.Mock
}

func (m *mockCacheClient) Get(ctx context.Context, req *connect.Request[cachev1.GetRequest]) (*connect.Response[cachev1.GetResponse], error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*connect.Response[cachev1.GetResponse]), args.Error(1)
}

func TestHandler_Get(t *testing.T) {
	ctx := context.Background()

	// Create mock database with l0 batches
	db := &mockQuerier{}

	// Setup test data: 3 l0 batches with timestamps in descending order
	now := time.Now()
	batch1 := database.L0Batch{
		ID:        1,
		Path:      "batch1",
		CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	}
	batch2 := database.L0Batch{
		ID:        2,
		Path:      "batch2",
		CreatedAt: pgtype.Timestamptz{Time: now.Add(-1 * time.Hour), Valid: true},
	}
	batch3 := database.L0Batch{
		ID:        3,
		Path:      "batch3",
		CreatedAt: pgtype.Timestamptz{Time: now.Add(-2 * time.Hour), Valid: true},
	}

	// Setup database mock to return our batches
	db.On("GetAllL0Batches", ctx).Return([]database.L0Batch{batch1, batch2, batch3}, nil)

	// Setup mock cache client that will respond with different values for each batch
	cacheClient := &mockCacheClient{}

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		db:  db,
		ring: &consistentRing{
			members: []Member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"test-cache:5002": cacheClient,
		},
	}

	// Setup request with keys to look up
	req := connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys([][]byte{[]byte("key1"), []byte("key2"), []byte("key3")})

	// Setup mock responses for each batch

	// Batch 1 has key1 and key2
	resp1 := connect.NewResponse(&cachev1.GetResponse{})
	resp1.Msg.SetKeys([][]byte{[]byte("key1"), []byte("key2")})
	resp1.Msg.SetValues([][]byte{[]byte("value1-batch1"), []byte("value2-batch1")})

	// Batch 2 has key3 only
	resp2 := connect.NewResponse(&cachev1.GetResponse{})
	resp2.Msg.SetKeys([][]byte{[]byte("key3")})
	resp2.Msg.SetValues([][]byte{[]byte("value3-batch2")})

	// Setup mock cache client to return these responses
	// The first call should be for batch1, etc.
	cacheClient.On("Get", ctx, mock.Anything).Return(resp1, nil).Once()
	cacheClient.On("Get", ctx, mock.Anything).Return(resp2, nil).Once()
	// Batch 3 should not be queried since we found all keys in batch1 and batch2

	// Call the handler
	resp, err := h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response
	// We should have all 3 keys, with values from the newest batches
	keys := resp.Msg.GetKeys()
	values := resp.Msg.GetValues()

	// Create a map of key-value pairs for easier verification
	kvMap := make(map[string]string)
	for i, key := range keys {
		kvMap[string(key)] = string(values[i])
	}

	// Check that we got the values from the newest batches
	assert.Equal(t, "value1-batch1", kvMap["key1"]) // key1 from batch1
	assert.Equal(t, "value2-batch1", kvMap["key2"]) // key2 from batch1
	assert.Equal(t, "value3-batch2", kvMap["key3"]) // key3 from batch2

	// Verify that the mock expectations were met
	mock.AssertExpectationsForObjects(t, db, cacheClient)
}

func TestValuePrecedence(t *testing.T) {
	ctx := context.Background()

	// Create mock database with l0 batches
	db := &mockQuerier{}

	// Setup test data: 2 l0 batches with timestamps in descending order
	now := time.Now()
	batch1 := database.L0Batch{
		ID:        1,
		Path:      "batch1",
		CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	}
	batch2 := database.L0Batch{
		ID:        2,
		Path:      "batch2",
		CreatedAt: pgtype.Timestamptz{Time: now.Add(-1 * time.Hour), Valid: true},
	}

	// Setup database mock to return our batches
	db.On("GetAllL0Batches", ctx).Return([]database.L0Batch{batch1, batch2}, nil)

	// Setup mock cache client
	cacheClient := &mockCacheClient{}

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		db:  db,
		ring: &consistentRing{
			members: []Member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"test-cache:5002": cacheClient,
		},
	}

	// Setup request with a key to look up
	req := connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys([][]byte{[]byte("key1")})

	// Setup mock responses for each batch

	// Batch 1 has key1 with one value
	resp1 := connect.NewResponse(&cachev1.GetResponse{})
	resp1.Msg.SetKeys([][]byte{[]byte("key1")})
	resp1.Msg.SetValues([][]byte{[]byte("newer-value")})

	// Batch 2 has key1 with a different value
	resp2 := connect.NewResponse(&cachev1.GetResponse{})
	resp2.Msg.SetKeys([][]byte{[]byte("key1")})
	resp2.Msg.SetValues([][]byte{[]byte("older-value")})

	// Setup mock cache client
	cacheClient.On("Get", ctx, mock.Anything).Return(resp1, nil).Once()
	// Batch 2 should not be queried since we found the key in batch1

	// Call the handler
	resp, err := h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response
	keys := resp.Msg.GetKeys()
	values := resp.Msg.GetValues()
	require.Equal(t, 1, len(keys))
	require.Equal(t, 1, len(values))

	// Check that we got the newer value
	assert.Equal(t, "key1", string(keys[0]))
	assert.Equal(t, "newer-value", string(values[0]))

	// Verify that the mock expectations were met
	mock.AssertExpectationsForObjects(t, db, cacheClient)
}

func TestEmptyKeyRequest(t *testing.T) {
	ctx := context.Background()
	db := &mockQuerier{}

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		db:  db,
		ring: &consistentRing{
			members: []Member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"test-cache:5002": &mockCacheClient{},
		},
	}

	// Setup request with no keys
	req := connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys([][]byte{})

	// Call the handler
	_, err := h.Get(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one key is required")
}

func TestDatabaseError(t *testing.T) {
	ctx := context.Background()
	db := &mockQuerier{}

	// Setup database mock to return an error
	db.On("GetAllL0Batches", ctx).Return([]database.L0Batch{}, assert.AnError)

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		db:  db,
		ring: &consistentRing{
			members: []Member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"test-cache:5002": &mockCacheClient{},
		},
	}

	// Setup request
	req := connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys([][]byte{[]byte("key1")})

	// Call the handler
	_, err := h.Get(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error retrieving l0 batches")
}

func TestCacheServiceError(t *testing.T) {
	ctx := context.Background()

	// Create mock database with l0 batches
	db := &mockQuerier{}

	// Setup test data
	now := time.Now()
	batch1 := database.L0Batch{
		ID:        1,
		Path:      "batch1",
		CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	}

	// Setup database mock
	db.On("GetAllL0Batches", ctx).Return([]database.L0Batch{batch1}, nil)

	// Setup mock cache client that will return an error
	cacheClient := &mockCacheClient{}
	// First call (primary endpoint)
	cacheClient.On("Get", ctx, mock.Anything).Return(
		(*connect.Response[cachev1.GetResponse])(nil),
		assert.AnError,
	).Once()
	// Second call (fallback endpoint)
	cacheClient.On("Get", ctx, mock.Anything).Return(
		(*connect.Response[cachev1.GetResponse])(nil),
		assert.AnError,
	).Once()

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		db:  db,
		ring: &consistentRing{
			members: []Member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"test-cache:5002": cacheClient,
		},
	}

	// Setup request
	req := connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys([][]byte{[]byte("key1")})

	// Call the handler
	_, err := h.Get(ctx, req)
	// Should return an error since we can't process the batch
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache services unavailable for batch 1")

	// Verify that the mock expectations were met
	mock.AssertExpectationsForObjects(t, db, cacheClient)
}
