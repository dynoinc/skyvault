package index

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"sync"
	"time"

	"net/http"

	"connectrpc.com/connect"
	"github.com/cespare/xxhash/v2"
	cachev1 "github.com/dynoinc/skyvault/gen/proto/cache/v1"
	cachev1connect "github.com/dynoinc/skyvault/gen/proto/cache/v1/v1connect"
	v1 "github.com/dynoinc/skyvault/gen/proto/index/v1"
	"github.com/dynoinc/skyvault/gen/proto/index/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/database"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Config holds the configuration for the index service
type Config struct {
	Enabled     bool          `default:"false"`
	Namespace   string        `default:"default"`
	RefreshRate time.Duration `default:"30s"`
}

// hasher implements the consistent hashing interface
type hasher struct{}

func (h hasher) Sum64(data []byte) uint64 {
	return xxhash.Sum64(data)
}

// handler implements the IndexService
type handler struct {
	v1connect.UnimplementedIndexServiceHandler

	config Config
	ctx    context.Context
	db     database.Querier

	// Cache service ring
	ring         *consistentRing
	ringMu       sync.RWMutex
	cacheClients map[string]cachev1connect.CacheServiceClient

	// Kubernetes client for discovering cache service pods
	kubeClient kubernetes.Interface
}

// Member represents a node in the consistent hash ring
type Member string

func (m Member) String() string {
	return string(m)
}

// consistentRing is a simplified interface for consistent hashing
type consistentRing struct {
	members []Member
	hasher  hasher
}

// NewConsistentRing creates a new consistent hash ring
func NewConsistentRing() *consistentRing {
	return &consistentRing{
		members: []Member{},
		hasher:  hasher{},
	}
}

// Add adds a member to the ring
func (r *consistentRing) Add(member Member) {
	r.members = append(r.members, member)
}

// Remove removes a member from the ring
func (r *consistentRing) Remove(member Member) {
	for i, m := range r.members {
		if m == member {
			r.members = append(r.members[:i], r.members[i+1:]...)
			return
		}
	}
}

// LocateKey finds the member responsible for a key
func (r *consistentRing) LocateKey(key []byte) (Member, Member) {
	if len(r.members) == 0 {
		return "", ""
	}
	hash := r.hasher.Sum64(key) % uint64(len(r.members))
	fallbackHash := (hash + 1) % uint64(len(r.members))
	return r.members[hash], r.members[fallbackHash]
}

// CountMembers returns the number of members in the ring
func (r *consistentRing) CountMembers() int {
	return len(r.members)
}

// NewHandler creates a new index service handler
func NewHandler(
	ctx context.Context,
	cfg Config,
	db database.Querier,
) (*handler, error) {
	h := &handler{
		config:       cfg,
		ctx:          ctx,
		db:           db,
		ring:         NewConsistentRing(),
		cacheClients: make(map[string]cachev1connect.CacheServiceClient),
	}

	// Initialize Kubernetes client
	var err error
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create in-cluster config: %w", err)
	}

	h.kubeClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Start watching for cache service pods
	go h.watchCacheServices(ctx)
	return h, nil
}

// watchCacheServices watches for changes in the cache service pods
func (h *handler) watchCacheServices(ctx context.Context) {
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		pods, err := h.kubeClient.CoreV1().Pods(h.config.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/component=cache",
		})
		if err != nil {
			slog.Error("Failed to list cache service pods", "error", err)
			return
		}

		// Collect all cache service endpoints
		endpoints := make(map[string]bool)
		for _, pod := range pods.Items {
			if pod.Status.Phase == "Running" {
				// Use pod IP address and cache service port
				endpoint := fmt.Sprintf("%s:%d", pod.Status.PodIP, 5002) // Assuming port 5002
				endpoints[endpoint] = true
			}
		}

		// Update the ring with the current endpoints
		h.ringMu.Lock()
		defer h.ringMu.Unlock()

		// Remove endpoints that no longer exist
		for endpoint := range h.cacheClients {
			if !endpoints[endpoint] {
				h.ring.Remove(Member(endpoint))
				delete(h.cacheClients, endpoint)
				slog.Info("Removed cache service from ring", "endpoint", endpoint)
			}
		}

		// Add new endpoints
		for endpoint := range endpoints {
			if _, exists := h.cacheClients[endpoint]; !exists {
				h.addCacheServiceLocked(endpoint)
				slog.Info("Added cache service to ring", "endpoint", endpoint)
			}
		}
	}, h.config.RefreshRate)
}

// addCacheServiceLocked adds a cache service to the consistent hash ring (without locking)
func (h *handler) addCacheServiceLocked(endpoint string) {
	// Add to the ring
	h.ring.Add(Member(endpoint))

	// Create a client for this endpoint
	baseURL := fmt.Sprintf("http://%s", endpoint)
	client := cachev1connect.NewCacheServiceClient(
		http.DefaultClient,
		baseURL,
	)

	// Store the client
	h.cacheClients[endpoint] = client
}

// Get retrieves values for requested keys by checking all l0_batches
func (h *handler) Get(
	ctx context.Context,
	req *connect.Request[v1.BatchGetRequest],
) (*connect.Response[v1.BatchGetResponse], error) {
	keys := req.Msg.GetKeys()
	if len(keys) == 0 {
		return connect.NewResponse(&v1.BatchGetResponse{}), nil
	}

	// Get all l0 batches from the database
	l0Batches, err := h.db.GetL0Batches(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("error retrieving l0 batches: %w", err))
	}

	// Sort batches by ID, highest first (newest first)
	sort.Slice(l0Batches, func(i, j int) bool {
		return l0Batches[i].ID > l0Batches[j].ID
	})

	// Track which keys we've found
	remainingKeys := make(map[string]string)
	for _, key := range keys {
		remainingKeys[key] = key
	}

	// Results storage - map of results keyed by string key
	resultsByKey := make(map[string]*v1.Result, len(keys))

	// Check each batch for the keys we need
	for _, batch := range l0Batches {
		// If we've found all keys, stop searching
		if len(remainingKeys) == 0 {
			break
		}

		// Create the object path for this batch
		objectPath := path.Join("l0_batches", batch.Path)

		// Prepare the keys we still need to look for
		keysToFind := make([]string, 0, len(remainingKeys))
		for key := range remainingKeys {
			keysToFind = append(keysToFind, key)
		}
		// Sort keys for deterministic order
		sort.Strings(keysToFind)

		// Get the primary and fallback endpoints while holding the ring lock
		h.ringMu.RLock()
		if h.ring.CountMembers() == 0 {
			h.ringMu.RUnlock()
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("no cache services available"))
		}

		primary, fallback := h.ring.LocateKey([]byte(objectPath))
		primaryEndpoint := primary.String()
		fallbackEndpoint := fallback.String()
		h.ringMu.RUnlock()

		// Try primary and fallback endpoints in order
		cacheReq := connect.NewRequest(&cachev1.GetRequest{})
		cacheReq.Msg.SetObjectPath(objectPath)
		cacheReq.Msg.SetKeys(keysToFind)

		endpoints := []string{primaryEndpoint, fallbackEndpoint}
		var lastErr error
		var resp *connect.Response[cachev1.GetResponse]

		for _, endpoint := range endpoints {
			client, exists := h.cacheClients[endpoint]
			if !exists {
				lastErr = fmt.Errorf("no client for cache service: %s", endpoint)
				continue
			}

			resp, err = client.Get(ctx, cacheReq)
			if err != nil {
				lastErr = err
				continue
			}

			// Success
			lastErr = nil
			break
		}

		if lastErr != nil {
			// Return an error since we're unable to process this batch
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("cache services unavailable for batch %d: %w", batch.ID, lastErr))
		}

		// Process the results
		cacheResults := resp.Msg.GetResults()
		for i, cacheResult := range cacheResults {
			key := keysToFind[i]

			// Only process keys that we're still looking for
			if _, needsProcessing := remainingKeys[key]; !needsProcessing {
				continue
			}

			// Create an index result from the cache result
			indexResult := &v1.Result{}

			// Record this result based on the status
			if cacheResult.HasFound() {
				// Key found with a value - store it and remove from remaining
				indexResult.SetFound(cacheResult.GetFound())
				resultsByKey[key] = indexResult
				delete(remainingKeys, key)
			} else if cacheResult.HasDeleted() {
				// Key has a tombstone - store it as deleted and remove from remaining
				// Tombstones in newer batches take precedence over older values
				indexResult.SetDeleted(true)
				resultsByKey[key] = indexResult
				delete(remainingKeys, key)
			} else if cacheResult.HasNotFound() {
				// Key was not found in this batch
				// Continue searching in older batches - don't remove from remainingKeys
				continue
			}
		}
	}

	// Create the final results array in same order as requested keys
	results := make([]*v1.Result, 0, len(keys))
	for _, key := range keys {
		if result, found := resultsByKey[key]; found {
			// We found this key in one of the batches
			results = append(results, result)
		} else {
			// Key was not found in any batch
			notFound := &v1.Result{}
			notFound.SetNotFound(true)
			results = append(results, notFound)
		}
	}

	// Create the response
	resp := &v1.BatchGetResponse{}
	resp.SetResults(results)

	return connect.NewResponse(resp), nil
}
