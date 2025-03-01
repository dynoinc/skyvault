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
		GetAllL0Batches(gomock.Any()).
		Return([]database.L0Batch{
			{
				ID:   1,
				Path: "batch-1",
			},
		}, nil)

	// Create a test request
	req := connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys([][]byte{[]byte("test-key")})

	// Define what the mock cache client should return
	cacheResp := &cachev1.GetResponse{}
	cacheResp.SetKeys([][]byte{[]byte("test-key")})
	cacheResp.SetValues([][]byte{[]byte("test-value")})

	mockCacheClient.EXPECT().
		Get(gomock.Any(), gomock.Any()).
		Return(connect.NewResponse(cacheResp), nil)

	// Add the mock cache client to the handler
	h.ringMu.Lock()
	h.cacheClients["10.0.0.1:5002"] = mockCacheClient
	h.ring.Add(Member("10.0.0.1:5002"))
	h.ringMu.Unlock()

	// Call the handler method
	resp, err := h.Get(context.Background(), req)

	// Assert the response
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 1, len(resp.Msg.GetKeys()))
	assert.Equal(t, "test-key", string(resp.Msg.GetKeys()[0]))
	assert.Equal(t, "test-value", string(resp.Msg.GetValues()[0]))
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
		GetAllL0Batches(gomock.Any()).
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
	req := connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys([][]byte{[]byte("key1")})

	// Setup mock responses for each batch

	// Batch 1 has key1 with one value
	resp1 := &cachev1.GetResponse{}
	resp1.SetKeys([][]byte{[]byte("key1")})
	resp1.SetValues([][]byte{[]byte("newer-value")})

	// Setup mock cache client expectation for the first batch only
	mockCacheClient.EXPECT().
		Get(gomock.Any(), gomock.Any()).
		Return(connect.NewResponse(resp1), nil)
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
	req := connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys([][]byte{})

	// Call the handler
	_, err := h.Get(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one key is required")
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
		GetAllL0Batches(gomock.Any()).
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
	req := connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys([][]byte{[]byte("key1")})

	// Call the handler
	_, err := h.Get(ctx, req)
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
		GetAllL0Batches(gomock.Any()).
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
	req := connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys([][]byte{[]byte("key1")})

	// Call the handler
	_, err := h.Get(ctx, req)
	// Should return an error since we can't process the batch
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache services unavailable for batch 1")
}
