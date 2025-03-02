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
	"github.com/dynoinc/skyvault/internal/mocks"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHandler_Get(t *testing.T) {
	// Create a new gomock controller
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mock objects
	mockDB := mocks.NewMockQuerier(ctrl)
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

	// Setup fake Kubernetes client with test pods
	fakeKubeClient := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cache-pod-1",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/component": "cache",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			PodIP: "10.0.0.1",
		},
	})

	// Create the handler with mocks
	h := &handler{
		config: Config{
			Enabled:     true,
			Namespace:   "default",
			RefreshRate: 30 * time.Second,
		},
		ctx:          context.Background(),
		db:           mockDB,
		kubeClient:   fakeKubeClient,
		ring:         NewConsistentRing(),
		cacheClients: make(map[string]cachev1connect.CacheServiceClient),
	}

	// Mock the database response
	mockDB.EXPECT().
		GetL0Batches(gomock.Any()).
		Return([]database.L0Batch{
			{
				ID:   1,
				Path: "batch-1",
			},
		}, nil)

	// Create a test request
	req := connect.NewRequest(&v1.BatchGetRequest{})
	req.Msg.SetKeys([]string{"test-key"})

	// Define what the mock cache client should return
	cacheResp := &cachev1.GetResponse{}
	testResult := &cachev1.Result{}
	testResult.SetFound([]byte("test-value"))
	cacheResp.SetResults([]*cachev1.Result{testResult})

	mockCacheClient.EXPECT().
		Get(gomock.Any(), gomock.Any()).
		Return(connect.NewResponse(cacheResp), nil)

	// Add the mock cache client to the handler
	h.ringMu.Lock()
	h.cacheClients["10.0.0.1:5002"] = mockCacheClient
	h.ring.Add(Member("10.0.0.1:5002"))
	h.ringMu.Unlock()

	// Call the handler method
	resp, err := h.BatchGet(context.Background(), req)

	// Assert the response
	assert.NoError(t, err)
	assert.NotNil(t, resp)

	results := resp.Msg.GetResults()
	require.Equal(t, 1, len(results))
	assert.True(t, results[0].HasFound())
	assert.Equal(t, "test-value", string(results[0].GetFound()))
}

func TestValuePrecedence(t *testing.T) {
	// Create a new gomock controller
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	// Create mock objects
	mockDB := mocks.NewMockQuerier(ctrl)
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

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
	mockDB.EXPECT().
		GetL0Batches(gomock.Any()).
		Return([]database.L0Batch{batch1, batch2}, nil)

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		db:  mockDB,
		ring: &consistentRing{
			members: []Member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"test-cache:5002": mockCacheClient,
		},
	}

	// Setup request with a key to look up
	req := connect.NewRequest(&v1.BatchGetRequest{})
	req.Msg.SetKeys([]string{"key1"})

	// Setup mock responses for batch1
	resp1 := &cachev1.GetResponse{}
	key1Result := &cachev1.Result{}
	key1Result.SetFound([]byte("newer-value"))
	resp1.SetResults([]*cachev1.Result{key1Result})

	// Setup mock cache client expectation for the first batch only
	mockCacheClient.EXPECT().
		Get(gomock.Any(), gomock.Any()).
		Return(connect.NewResponse(resp1), nil)
	// Batch 2 should not be queried since we found the key in batch1

	// Call the handler
	resp, err := h.BatchGet(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response
	results := resp.Msg.GetResults()
	require.Equal(t, 1, len(results))

	// Check that we got the newer value
	assert.True(t, results[0].HasFound())
	assert.Equal(t, "newer-value", string(results[0].GetFound()))
}

func TestEmptyKeyRequest(t *testing.T) {
	// Create a new gomock controller
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	// Create mock objects
	mockDB := mocks.NewMockQuerier(ctrl)
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		db:  mockDB,
		ring: &consistentRing{
			members: []Member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"test-cache:5002": mockCacheClient,
		},
	}

	// Setup request with no keys
	req := connect.NewRequest(&v1.BatchGetRequest{})
	req.Msg.SetKeys([]string{})

	// Call the handler
	resp, err := h.BatchGet(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Msg)
	assert.Empty(t, resp.Msg.GetResults(), "Expected empty results for empty keys request")
}

func TestDatabaseError(t *testing.T) {
	// Create a new gomock controller
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	// Create mock objects
	mockDB := mocks.NewMockQuerier(ctrl)

	// Setup database mock to return an error
	mockDB.EXPECT().
		GetL0Batches(gomock.Any()).
		Return([]database.L0Batch{}, assert.AnError)

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		db:  mockDB,
		ring: &consistentRing{
			members: []Member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{},
	}

	// Setup request
	req := connect.NewRequest(&v1.BatchGetRequest{})
	req.Msg.SetKeys([]string{"key1"})

	// Call the handler
	_, err := h.BatchGet(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error retrieving l0 batches")
}

func TestCacheServiceError(t *testing.T) {
	// Create a new gomock controller
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	// Create mock objects
	mockDB := mocks.NewMockQuerier(ctrl)
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

	// Setup test data
	now := time.Now()
	batch1 := database.L0Batch{
		ID:        1,
		Path:      "batch1",
		CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	}

	// Setup database mock
	mockDB.EXPECT().
		GetL0Batches(gomock.Any()).
		Return([]database.L0Batch{batch1}, nil)

	// Setup mock cache client that will return an error for both primary and fallback endpoints
	mockCacheClient.EXPECT().
		Get(gomock.Any(), gomock.Any()).
		Return(nil, assert.AnError)

	mockCacheClient.EXPECT().
		Get(gomock.Any(), gomock.Any()).
		Return(nil, assert.AnError)

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		db:  mockDB,
		ring: &consistentRing{
			members: []Member{"primary:5002", "fallback:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"primary:5002":  mockCacheClient,
			"fallback:5002": mockCacheClient,
		},
	}

	// Setup request
	req := connect.NewRequest(&v1.BatchGetRequest{})
	req.Msg.SetKeys([]string{"key1"})

	// Call the handler
	_, err := h.BatchGet(ctx, req)
	// Should return an error since we can't process the batch
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache services unavailable for batch 1")
}

// TestValueAndTombstonePrecedence tests that values and tombstones from newer batches
// take precedence over older batches
func TestValueAndTombstonePrecedence(t *testing.T) {
	// Create a new gomock controller
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	// Create mock objects
	mockDB := mocks.NewMockQuerier(ctrl)
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

	// Setup test data: 3 l0 batches with IDs in descending order
	// Batch 3 is newest, Batch 1 is oldest
	batches := []database.L0Batch{
		{
			ID:   3,
			Path: "l0_batches/batch3",
		},
		{
			ID:   2,
			Path: "l0_batches/batch2",
		},
		{
			ID:   1,
			Path: "l0_batches/batch1",
		},
	}

	// Setup database mock to return our batches
	mockDB.EXPECT().
		GetL0Batches(gomock.Any()).
		Return(batches, nil)

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		db:  mockDB,
		ring: &consistentRing{
			members: []Member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"test-cache:5002": mockCacheClient,
		},
	}

	// Setup request with keys to look up
	req := connect.NewRequest(&v1.BatchGetRequest{})
	req.Msg.SetKeys([]string{
		"key1", // Will be deleted in batch3 (newest)
		"key2", // Will have value in batch2 and different value in batch1
		"key3", // Will be in batch1 only
	})

	// Setup mock responses for each batch in order of access (newest to oldest)

	// Batch 3 response (newest)
	resp3 := &cachev1.GetResponse{}
	key1Result := &cachev1.Result{}
	key1Result.SetDeleted(true)
	key2Result := &cachev1.Result{}
	key2Result.SetNotFound(true) // Not found in batch3, should look in batch2
	key3Result := &cachev1.Result{}
	key3Result.SetNotFound(true) // Not found in batch3, should look in batch2
	resp3.SetResults([]*cachev1.Result{key1Result, key2Result, key3Result})

	// Batch 2 response
	resp2 := &cachev1.GetResponse{}
	// key1 isn't queried anymore since we found its tombstone in batch3
	key2Result2 := &cachev1.Result{}
	key2Result2.SetFound([]byte("value2-from-batch2"))
	key3Result2 := &cachev1.Result{}
	key3Result2.SetNotFound(true) // Not found in batch2, should look in batch1
	resp2.SetResults([]*cachev1.Result{key2Result2, key3Result2})

	// Batch 1 response (oldest)
	resp1 := &cachev1.GetResponse{}
	// Only key3 is left to query
	key3Result1 := &cachev1.Result{}
	key3Result1.SetFound([]byte("value3-from-batch1"))
	resp1.SetResults([]*cachev1.Result{key3Result1})

	// Setup expectations for cache client calls
	gomock.InOrder(
		mockCacheClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, req *connect.Request[cachev1.GetRequest]) (*connect.Response[cachev1.GetResponse], error) {
				// Verify we're querying the right object path for batch3
				assert.Equal(t, "l0_batches/batch3", req.Msg.GetObjectPath())
				// Verify we're querying all three keys initially
				assert.Equal(t, 3, len(req.Msg.GetKeys()))
				// Keys should be sorted
				assert.Equal(t, []string{"key1", "key2", "key3"}, req.Msg.GetKeys())
				return connect.NewResponse(resp3), nil
			}),
		mockCacheClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, req *connect.Request[cachev1.GetRequest]) (*connect.Response[cachev1.GetResponse], error) {
				// Verify we're querying the right object path for batch2
				assert.Equal(t, "l0_batches/batch2", req.Msg.GetObjectPath())
				// Verify we're only querying key2 and key3 since key1 was already found in batch3
				assert.Equal(t, 2, len(req.Msg.GetKeys()))
				// Keys should be sorted
				assert.Equal(t, []string{"key2", "key3"}, req.Msg.GetKeys())
				return connect.NewResponse(resp2), nil
			}),
		mockCacheClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, req *connect.Request[cachev1.GetRequest]) (*connect.Response[cachev1.GetResponse], error) {
				// Verify we're querying the right object path for batch1
				assert.Equal(t, "l0_batches/batch1", req.Msg.GetObjectPath())
				// Verify we're only querying key3 since key2 was found in batch2
				assert.Equal(t, 1, len(req.Msg.GetKeys()))
				assert.Equal(t, "key3", req.Msg.GetKeys()[0])
				return connect.NewResponse(resp1), nil
			}),
	)

	// Call the handler
	resp, err := h.BatchGet(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response has the expected results
	results := resp.Msg.GetResults()
	require.Equal(t, 3, len(results))

	// key1 should be marked as deleted (from newest batch3)
	assert.True(t, results[0].HasDeleted())
	assert.True(t, results[0].GetDeleted())

	// key2 should have the value from batch2
	assert.True(t, results[1].HasFound())
	assert.Equal(t, "value2-from-batch2", string(results[1].GetFound()))

	// key3 should have the value from batch1
	assert.True(t, results[2].HasFound())
	assert.Equal(t, "value3-from-batch1", string(results[2].GetFound()))
}
