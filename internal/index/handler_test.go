package index

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/btree"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	cachev1 "github.com/dynoinc/skyvault/gen/proto/cache/v1"
	cachev1connect "github.com/dynoinc/skyvault/gen/proto/cache/v1/v1connect"
	commonv1 "github.com/dynoinc/skyvault/gen/proto/common/v1"
	v1 "github.com/dynoinc/skyvault/gen/proto/index/v1"
	"github.com/dynoinc/skyvault/internal/database"
	"github.com/dynoinc/skyvault/internal/mocks"
)

func TestHandler_Get(t *testing.T) {
	// Create a new gomock controller
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create mock objects
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

	// Setup fake Kubernetes client with test pods
	fakeKubeClient := fake.NewClientset(&corev1.Pod{
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

	now := time.Now()
	batch1 := database.L0Batch{
		Attrs: commonv1.L0Batch_builder{
			Path:      proto.String("batch1"),
			CreatedAt: timestamppb.New(now),
		}.Build(),
	}

	// Create the handler with mocks
	h := &handler{
		config: Config{
			Enabled:   true,
			Namespace: "default",
		},
		ctx:          context.Background(),
		kubeClient:   fakeKubeClient,
		ring:         newConsistentRing(),
		cacheClients: make(map[string]cachev1connect.CacheServiceClient),
	}
	l0Batches := []database.L0Batch{batch1}
	h.l0Batches.Store(&l0Batches)

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
	h.ring.add("10.0.0.1:5002")
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
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

	// Setup test data: 2 l0 batches with timestamps in descending order
	now := time.Now()
	batch1 := database.L0Batch{
		Attrs: commonv1.L0Batch_builder{
			Path:      proto.String("batch1"),
			CreatedAt: timestamppb.New(now),
		}.Build(),
	}
	batch2 := database.L0Batch{
		Attrs: commonv1.L0Batch_builder{
			Path:      proto.String("batch2"),
			CreatedAt: timestamppb.New(now.Add(-1 * time.Hour)),
		}.Build(),
	}

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		ring: &consistentRing{
			members: []member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"test-cache:5002": mockCacheClient,
		},
	}
	l0Batches := []database.L0Batch{batch1, batch2}
	h.l0Batches.Store(&l0Batches)

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
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		ring: &consistentRing{
			members: []member{"test-cache:5002"},
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

func TestCacheServiceError(t *testing.T) {
	// Create a new gomock controller
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	// Create mock objects
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

	// Setup test data
	now := time.Now()
	batch1 := database.L0Batch{
		Attrs: commonv1.L0Batch_builder{
			Path:      proto.String("batch1"),
			CreatedAt: timestamppb.New(now),
		}.Build(),
	}

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
		ring: &consistentRing{
			members: []member{"primary:5002", "fallback:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"primary:5002":  mockCacheClient,
			"fallback:5002": mockCacheClient,
		},
	}
	l0Batches := []database.L0Batch{batch1}
	h.l0Batches.Store(&l0Batches)

	// Setup request
	req := connect.NewRequest(&v1.BatchGetRequest{})
	req.Msg.SetKeys([]string{"key1"})

	// Call the handler
	_, err := h.BatchGet(ctx, req)
	// Should return an error since we can't process the batch
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache services unavailable for")
}

// TestValueAndTombstonePrecedence tests that values and tombstones from newer batches
// take precedence over older batches
func TestValueAndTombstonePrecedence(t *testing.T) {
	// Create a new gomock controller
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	// Create mock objects
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

	// Setup test data: 3 l0 batches with IDs in descending order
	// Batch 3 is newest, Batch 1 is oldest
	batches := []database.L0Batch{
		{
			Attrs: commonv1.L0Batch_builder{
				Path: proto.String("l0_batches/batch3"),
			}.Build(),
		},
		{
			Attrs: commonv1.L0Batch_builder{
				Path: proto.String("l0_batches/batch2"),
			}.Build(),
		},
		{
			Attrs: commonv1.L0Batch_builder{
				Path: proto.String("l0_batches/batch1"),
			}.Build(),
		},
	}

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		ring: &consistentRing{
			members: []member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"test-cache:5002": mockCacheClient,
		},
	}
	h.l0Batches.Store(&batches)

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

	// key1 should be marked as not found (from newest batch3)
	assert.True(t, results[0].HasNotFound())

	// key2 should have the value from batch2
	assert.True(t, results[1].HasFound())
	assert.Equal(t, "value2-from-batch2", string(results[1].GetFound()))

	// key3 should have the value from batch1
	assert.True(t, results[2].HasFound())
	assert.Equal(t, "value3-from-batch1", string(results[2].GetFound()))
}

func TestPartitionLookup(t *testing.T) {
	// Create a new gomock controller
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	// Create mock objects
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

	// Setup test data: partitions with different key ranges
	partitions := btree.Map[string, *commonv1.Partition]{}

	// Partition 1: keys < "k2"
	partition1 := commonv1.Partition_builder{
		Path: proto.String("partition1/data"),
	}.Build()
	partitions.Set("", partition1) // Empty string is the start key for first partition

	// Partition 2: k2 <= keys < k4
	partition2 := commonv1.Partition_builder{
		Path: proto.String("partition2/data"),
	}.Build()
	partitions.Set("k2", partition2)

	// Partition 3: k4 <= keys
	partition3 := commonv1.Partition_builder{
		Path: proto.String("partition3/data"),
	}.Build()
	partitions.Set("k4", partition3)

	// Create the handler
	h := &handler{
		config: Config{
			Enabled: true,
		},
		ctx: ctx,
		ring: &consistentRing{
			members: []member{"test-cache:5002"},
			hasher:  hasher{},
		},
		cacheClients: map[string]cachev1connect.CacheServiceClient{
			"test-cache:5002": mockCacheClient,
		},
	}
	l0Batches := []database.L0Batch{}
	h.l0Batches.Store(&l0Batches)
	h.partitions.Store(&partitions)

	// Setup request with keys that span different partitions
	req := connect.NewRequest(&v1.BatchGetRequest{})
	req.Msg.SetKeys([]string{
		"k1", // Should be served by partition1
		"k2", // Should be served by partition2
		"k3", // Should be served by partition2
		"k5", // Should be served by partition3
	})

	// Setup mock responses for each partition

	// Partition 1 response (k1)
	resp1 := &cachev1.GetResponse{}
	k1Result := &cachev1.Result{}
	k1Result.SetFound([]byte("value1"))
	resp1.SetResults([]*cachev1.Result{k1Result})

	// Partition 2 response (k2, k3)
	resp2 := &cachev1.GetResponse{}
	k2Result := &cachev1.Result{}
	k2Result.SetFound([]byte("value2"))
	k3Result := &cachev1.Result{}
	k3Result.SetFound([]byte("value3"))
	resp2.SetResults([]*cachev1.Result{k2Result, k3Result})

	// Partition 3 response (k5)
	resp3 := &cachev1.GetResponse{}
	k5Result := &cachev1.Result{}
	k5Result.SetFound([]byte("value5"))
	resp3.SetResults([]*cachev1.Result{k5Result})

	// Setup expectations for cache client calls

	mockCacheClient.EXPECT().
		Get(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, req *connect.Request[cachev1.GetRequest]) (*connect.Response[cachev1.GetResponse], error) {
			if req.Msg.GetObjectPath() == "partition1/data" {
				assert.Equal(t, []string{"k1"}, req.Msg.GetKeys())
				return connect.NewResponse(resp1), nil
			} else if req.Msg.GetObjectPath() == "partition2/data" {
				assert.Equal(t, []string{"k2", "k3"}, req.Msg.GetKeys())
				return connect.NewResponse(resp2), nil
			} else if req.Msg.GetObjectPath() == "partition3/data" {
				assert.Equal(t, []string{"k5"}, req.Msg.GetKeys())
				return connect.NewResponse(resp3), nil
			}

			return nil, assert.AnError
		}).Times(3)

	// Call the handler
	resp, err := h.BatchGet(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response has the expected results in the correct order
	results := resp.Msg.GetResults()
	require.Equal(t, 4, len(results))

	// k1 from partition1
	assert.True(t, results[0].HasFound())
	assert.Equal(t, "value1", string(results[0].GetFound()))

	// k2 from partition2
	assert.True(t, results[1].HasFound())
	assert.Equal(t, "value2", string(results[1].GetFound()))

	// k3 from partition2
	assert.True(t, results[2].HasFound())
	assert.Equal(t, "value3", string(results[2].GetFound()))

	// k5 from partition3
	assert.True(t, results[3].HasFound())
	assert.Equal(t, "value5", string(results[3].GetFound()))
}
