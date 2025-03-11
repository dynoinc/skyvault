package index

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/emptypb"
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
	batch1 := database.WriteAheadLog{
		Attrs: commonv1.WriteAheadLog_builder{
			Path:      "batch1",
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
	wals := []database.WriteAheadLog{batch1}
	h.wals.Store(&wals)

	// Create a test request
	req := connect.NewRequest(&v1.BatchGetRequest{})
	req.Msg.SetKeys([]string{"test-key"})

	// Define what the mock cache client should return
	cacheResp := &cachev1.GetResponse{}
	found := map[string][]byte{
		"test-key": []byte("test-value"),
	}
	cacheResp.SetFound(found)

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
	assert.Equal(t, "test-value", string(results["test-key"]))
}

func TestValuePrecedence(t *testing.T) {
	// Create a new gomock controller
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	// Create mock objects
	mockCacheClient := mocks.NewMockCacheServiceClient(ctrl)

	// Setup test data: 2 WALs with timestamps in descending order
	now := time.Now()
	batch1 := database.WriteAheadLog{
		Attrs: commonv1.WriteAheadLog_builder{
			Path:      "batch1",
			CreatedAt: timestamppb.New(now),
		}.Build(),
	}
	batch2 := database.WriteAheadLog{
		Attrs: commonv1.WriteAheadLog_builder{
			Path:      "batch2",
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
	wals := []database.WriteAheadLog{batch1, batch2}
	h.wals.Store(&wals)

	// Setup request with a key to look up
	req := connect.NewRequest(&v1.BatchGetRequest{})
	req.Msg.SetKeys([]string{"key1"})

	// Setup mock responses for batch1
	resp1 := &cachev1.GetResponse{}
	found := map[string][]byte{
		"key1": []byte("newer-value"),
	}
	resp1.SetFound(found)

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
	assert.Equal(t, "newer-value", string(results["key1"]))
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
	h.wals.Store(&[]database.WriteAheadLog{})

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
	batch1 := database.WriteAheadLog{
		Attrs: commonv1.WriteAheadLog_builder{
			Path:      "batch1",
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
	wals := []database.WriteAheadLog{batch1}
	h.wals.Store(&wals)

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

	// Setup test data: 3 WALs with IDs in descending order
	// Batch 3 is newest, Batch 1 is oldest
	batches := []database.WriteAheadLog{
		{
			Attrs: commonv1.WriteAheadLog_builder{
				Path: "wal/batch3",
			}.Build(),
		},
		{
			Attrs: commonv1.WriteAheadLog_builder{
				Path: "wal/batch2",
			}.Build(),
		},
		{
			Attrs: commonv1.WriteAheadLog_builder{
				Path: "wal/batch1",
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
	h.wals.Store(&batches)

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
	resp3.SetDeleted(map[string]*emptypb.Empty{
		"key1": {},
	})

	// Batch 2 response
	resp2 := &cachev1.GetResponse{}
	resp2.SetFound(map[string][]byte{
		"key2": []byte("value2-from-batch2"),
	})

	// Batch 1 response (oldest)
	resp1 := &cachev1.GetResponse{}
	resp1.SetFound(map[string][]byte{
		"key3": []byte("value3-from-batch1"),
	})

	// Setup expectations for cache client calls
	gomock.InOrder(
		mockCacheClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, req *connect.Request[cachev1.GetRequest]) (*connect.Response[cachev1.GetResponse], error) {
				// Verify we're querying the right object path for batch3
				assert.Equal(t, "wal/batch3", req.Msg.GetObjectPath())
				// Verify we're querying all three keys initially
				assert.Equal(t, 3, len(req.Msg.GetKeys()))
				assert.Contains(t, req.Msg.GetKeys(), "key1")
				assert.Contains(t, req.Msg.GetKeys(), "key2")
				assert.Contains(t, req.Msg.GetKeys(), "key3")
				return connect.NewResponse(resp3), nil
			}),
		mockCacheClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, req *connect.Request[cachev1.GetRequest]) (*connect.Response[cachev1.GetResponse], error) {
				// Verify we're querying the right object path for batch2
				assert.Equal(t, "wal/batch2", req.Msg.GetObjectPath())
				// Verify we're only querying key2 and key3 since key1 was already found in batch3
				assert.Equal(t, 2, len(req.Msg.GetKeys()))
				assert.Contains(t, req.Msg.GetKeys(), "key2")
				assert.Contains(t, req.Msg.GetKeys(), "key3")
				return connect.NewResponse(resp2), nil
			}),
		mockCacheClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, req *connect.Request[cachev1.GetRequest]) (*connect.Response[cachev1.GetResponse], error) {
				// Verify we're querying the right object path for batch1
				assert.Equal(t, "wal/batch1", req.Msg.GetObjectPath())
				// Verify we're only querying key3 since key2 was found in batch2
				assert.Equal(t, 1, len(req.Msg.GetKeys()))
				assert.Contains(t, req.Msg.GetKeys(), "key3")
				return connect.NewResponse(resp1), nil
			}),
	)

	// Call the handler
	resp, err := h.BatchGet(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response has the expected results
	results := resp.Msg.GetResults()
	require.Equal(t, 2, len(results)) // key1 was deleted, so only key2 and key3 should be present

	// key2 should have the value from batch2
	assert.Equal(t, "value2-from-batch2", string(results["key2"]))

	// key3 should have the value from batch1
	assert.Equal(t, "value3-from-batch1", string(results["key3"]))
}
