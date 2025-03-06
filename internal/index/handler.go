package index

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"

	"connectrpc.com/connect"
	"github.com/cespare/xxhash/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tidwall/btree"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	cachev1 "github.com/dynoinc/skyvault/gen/proto/cache/v1"
	cachev1connect "github.com/dynoinc/skyvault/gen/proto/cache/v1/v1connect"
	commonv1 "github.com/dynoinc/skyvault/gen/proto/common/v1"
	v1 "github.com/dynoinc/skyvault/gen/proto/index/v1"
	"github.com/dynoinc/skyvault/gen/proto/index/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/database"
)

// Config holds the configuration for the index service
type Config struct {
	Enabled bool `default:"false"`

	Namespace string `default:"default"`
	Instance  string `default:"default"`
	CachePort int    `default:"5002"`
}

// hasher implements the consistent hashing interface
type hasher struct{}

func (h hasher) sum64(data []byte) uint64 {
	return xxhash.Sum64(data)
}

// handler implements the IndexService
type handler struct {
	v1connect.UnimplementedIndexServiceHandler

	config Config
	ctx    context.Context

	// Cache service ring
	ringMu       sync.RWMutex
	ring         *consistentRing
	cacheClients map[string]cachev1connect.CacheServiceClient

	// L0 batches
	l0Batches atomic.Pointer[[]database.L0Batch]

	// Partitions
	partitions atomic.Pointer[btree.Map[string, *commonv1.Partition]]

	// Kubernetes client for discovering cache service pods
	kubeClient kubernetes.Interface
}

// member represents a node in the consistent hash ring
type member string

func (m member) String() string {
	return string(m)
}

// consistentRing is a simplified interface for consistent hashing
type consistentRing struct {
	members []member
	hasher  hasher
}

// newConsistentRing creates a new consistent hash ring
func newConsistentRing() *consistentRing {
	return &consistentRing{
		members: []member{},
		hasher:  hasher{},
	}
}

// add adds a member to the ring
func (r *consistentRing) add(member member) {
	r.members = append(r.members, member)
}

// remove removes a member from the ring
func (r *consistentRing) remove(member member) {
	for i, m := range r.members {
		if m == member {
			r.members = append(r.members[:i], r.members[i+1:]...)
			return
		}
	}
}

// locateKey finds the member responsible for a key
func (r *consistentRing) locateKey(key []byte) []member {
	if len(r.members) == 0 {
		return []member{}
	}
	hash := r.hasher.sum64(key) % uint64(len(r.members))
	fallbackHash := (hash + 1) % uint64(len(r.members))

	members := []member{r.members[hash]}
	if r.members[fallbackHash] != r.members[hash] {
		members = append(members, r.members[fallbackHash])
	}

	return members
}

// NewHandler creates a new index service handler
func NewHandler(
	ctx context.Context,
	cfg Config,
	db *pgxpool.Pool,
) (*handler, error) {
	h := &handler{
		config: cfg,
		ctx:    ctx,

		ring:         newConsistentRing(),
		cacheClients: make(map[string]cachev1connect.CacheServiceClient),
	}

	// Initialize Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create in-cluster config: %w", err)
	}

	h.kubeClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Start watching for cache service pods
	if err := h.watchCacheServices(ctx); err != nil {
		return nil, fmt.Errorf("failed to watch cache services: %w", err)
	}

	// Start watching for l0_batches
	if err := h.watchL0Batches(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to watch l0_batches: %w", err)
	}

	// Start watching for partitions
	if err := h.watchPartitions(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to watch partitions: %w", err)
	}

	return h, nil
}

// watchCacheServices watches for changes in the cache service pods
func (h *handler) watchCacheServices(ctx context.Context) error {
	watcher, err := h.kubeClient.CoreV1().Pods(h.config.Namespace).Watch(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/component=cache,skyvault.io/instance=%s", h.config.Instance),
		Watch:         true,
	})
	if err != nil {
		return fmt.Errorf("failed to watch cache service pods: %w", err)
	}

	go func() {
		defer watcher.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.ResultChan():
				if !ok {
					slog.Error("Watch channel closed unexpectedly")
					return
				}

				pod, ok := event.Object.(*corev1.Pod)
				if !ok {
					slog.Error("Received non-pod object from watch")
					continue
				}

				h.ringMu.Lock()

				switch event.Type {
				case watch.Added, watch.Modified:
					endpoint := fmt.Sprintf("%s:%d", pod.Status.PodIP, h.config.CachePort)
					if pod.Status.Phase == corev1.PodRunning {
						if _, exists := h.cacheClients[endpoint]; !exists {
							h.addCacheServiceLocked(endpoint)
							slog.Info("Added cache service to ring", "endpoint", endpoint, "instance", h.config.Instance)
						}
					} else {
						// Remove pod if it's no longer running
						if _, exists := h.cacheClients[endpoint]; exists {
							h.ring.remove(member(endpoint))
							delete(h.cacheClients, endpoint)
							slog.Info("Removed non-running cache service from ring", "endpoint", endpoint, "instance", h.config.Instance)
						}
					}

				case watch.Deleted:
					endpoint := fmt.Sprintf("%s:%d", pod.Status.PodIP, h.config.CachePort)
					if _, exists := h.cacheClients[endpoint]; exists {
						h.ring.remove(member(endpoint))
						delete(h.cacheClients, endpoint)
						slog.Info("Removed cache service from ring", "endpoint", endpoint, "instance", h.config.Instance)
					}
				}

				h.ringMu.Unlock()
			}
		}
	}()

	return nil
}

// watchL0Batches watches for changes in the l0_batches
func (h *handler) watchL0Batches(ctx context.Context, db *pgxpool.Pool) error {
	l0Batches, err := database.New(db).GetL0Batches(ctx)
	if err != nil {
		return fmt.Errorf("failed to get l0_batches: %w", err)
	}

	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire db conn: %w", err)
	}

	_, err = conn.Exec(ctx, "LISTEN new_l0_batch")
	if err != nil {
		return fmt.Errorf("failed to listen for notifications: %w", err)
	}

	// Sort the l0 batches by SeqNo, highest first (newest first)
	sort.Slice(l0Batches, func(i, j int) bool {
		return l0Batches[i].SeqNo > l0Batches[j].SeqNo
	})

	h.l0Batches.Store(&l0Batches)

	go func() {
		defer conn.Release()

		for {
			notification, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}

				slog.ErrorContext(ctx, "error waiting for notification", "error", err)
				continue
			}

			// Get updated l0 batches
			l0Batches, err := database.New(db).GetL0Batches(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get l0 batches after notification", "error", err)
				continue
			}

			// Sort the l0 batches by SeqNo, highest first (newest first)
			sort.Slice(l0Batches, func(i, j int) bool {
				return l0Batches[i].SeqNo > l0Batches[j].SeqNo
			})

			h.l0Batches.Store(&l0Batches)

			slog.InfoContext(ctx, "updated l0 batches from notification", "payload", notification.Payload, "count", len(l0Batches))
		}
	}()

	return nil
}

// watchPartitions watches for changes in the partitions
func (h *handler) watchPartitions(ctx context.Context, db *pgxpool.Pool) error {
	partitions, err := database.New(db).GetPartitions(ctx)
	if err != nil {
		return fmt.Errorf("failed to get partitions: %w", err)
	}

	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire db conn: %w", err)
	}

	_, err = conn.Exec(ctx, "LISTEN new_partition")
	if err != nil {
		return fmt.Errorf("failed to listen for notifications: %w", err)
	}

	var tree btree.Map[string, *commonv1.Partition]
	for _, partition := range partitions {
		tree.Set(partition.InclusiveStartKey, partition.Attrs)
	}

	h.partitions.Store(&tree)

	go func() {
		defer conn.Release()

		for {
			notification, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}

				slog.ErrorContext(ctx, "error waiting for notification", "error", err)
				continue
			}

			partitions, err := database.New(db).GetPartitions(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get partitions after notification", "error", err)
				continue
			}

			var tree btree.Map[string, *commonv1.Partition]
			for _, partition := range partitions {
				tree.Set(partition.InclusiveStartKey, partition.Attrs)
			}

			h.partitions.Store(&tree)
			slog.InfoContext(ctx, "updated partitions from notification", "payload", notification.Payload, "count", len(partitions))
		}
	}()

	return nil
}

// addCacheServiceLocked adds a cache service to the consistent hash ring (without locking)
func (h *handler) addCacheServiceLocked(endpoint string) {
	// Add to the ring
	h.ring.add(member(endpoint))

	// Create a client for this endpoint
	baseURL := fmt.Sprintf("http://%s", endpoint)
	client := cachev1connect.NewCacheServiceClient(
		http.DefaultClient,
		baseURL,
	)

	// Store the client
	h.cacheClients[endpoint] = client
}

func (h *handler) getClients(path string) ([]cachev1connect.CacheServiceClient, error) {
	// Get the primary and fallback endpoints while holding the ring lock
	h.ringMu.RLock()
	defer h.ringMu.RUnlock()

	members := h.ring.locateKey([]byte(path))
	if len(members) == 0 {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("no cache service available"))
	}

	clients := make([]cachev1connect.CacheServiceClient, 0, len(members))
	for _, member := range members {
		client, exists := h.cacheClients[member.String()]
		if !exists {
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("no cache service available"))
		}

		clients = append(clients, client)
	}

	return clients, nil
}

// Get retrieves values for requested keys by checking all l0_batches
func (h *handler) BatchGet(
	ctx context.Context,
	req *connect.Request[v1.BatchGetRequest],
) (*connect.Response[v1.BatchGetResponse], error) {
	keys := req.Msg.GetKeys()
	if len(keys) == 0 {
		return connect.NewResponse(&v1.BatchGetResponse{}), nil
	}

	l0Batches := *h.l0Batches.Load()

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

		// Prepare the keys we still need to look for
		keysToFind := make([]string, 0, len(remainingKeys))
		for key := range remainingKeys {
			keysToFind = append(keysToFind, key)
		}
		// Sort keys for deterministic order
		sort.Strings(keysToFind)

		clients, err := h.getClients(batch.Attrs.GetPath())
		if err != nil {
			return nil, err
		}

		cacheReq := connect.NewRequest(&cachev1.GetRequest{})
		cacheReq.Msg.SetObjectPath(batch.Attrs.GetPath())
		cacheReq.Msg.SetKeys(keysToFind)

		var resp *connect.Response[cachev1.GetResponse]
		var lastErr error

		for _, client := range clients {
			var err error
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
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("cache services unavailable for path %s: %w", batch.Attrs.GetPath(), lastErr))
		}

		// Process the results
		cacheResults := resp.Msg.GetResults()
		for i, cacheResult := range cacheResults {
			key := keysToFind[i]

			// Create an index result from the cache result
			indexResult := &v1.Result{}

			// Record this result based on the status
			if cacheResult.HasFound() {
				// Key found with a value - store it and remove from remaining
				indexResult.SetFound(cacheResult.GetFound())
				resultsByKey[key] = indexResult
				delete(remainingKeys, key)
			} else if cacheResult.HasDeleted() {
				// Key has a tombstone - store it as not found and remove from remaining
				// Tombstones in newer batches take precedence over older values
				indexResult.SetNotFound(true)
				resultsByKey[key] = indexResult
				delete(remainingKeys, key)
			} else if cacheResult.HasNotFound() {
				// Key was not found in this batch
				// Continue searching in older batches - don't remove from remainingKeys
				continue
			}
		}
	}

	if len(remainingKeys) > 0 {
		partitions := *h.partitions.Load()

		// Create requests for each group of keys in the same partition
		reqs := make(map[string][]string)
		for key := range remainingKeys {
			found := false
			partitions.Descend(key, func(pk string, pv *commonv1.Partition) bool {
				reqs[pk] = append(reqs[pk], key)
				found = true
				return false
			})

			if !found {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("key %q not found in any partition", key))
			}
		}

		for pk, keys := range reqs {
			pv, ok := partitions.Get(pk)
			if !ok {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("partition %q not found", pk))
			}

			path := pv.GetPath()
			if path == "" {
				// partition has not data, mark all the keys as not found
				for _, key := range keys {
					resultsByKey[key] = v1.Result_builder{
						NotFound: proto.Bool(true),
					}.Build()
				}
				continue
			}

			clients, err := h.getClients(path)
			if err != nil {
				return nil, err
			}
			cacheReq := connect.NewRequest(&cachev1.GetRequest{})
			cacheReq.Msg.SetObjectPath(path)
			cacheReq.Msg.SetKeys(keys)

			var resp *connect.Response[cachev1.GetResponse]
			var lastErr error

			for _, client := range clients {
				var err error
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
				return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("cache services unavailable for path %s: %w", path, lastErr))
			}

			// Process the results
			cacheResults := resp.Msg.GetResults()
			for i, cacheResult := range cacheResults {
				key := keys[i]

				// Create an index result from the cache result
				indexResult := &v1.Result{}

				// Record this result based on the status
				if cacheResult.HasFound() {
					// Key found with a value - store it and remove from remaining
					indexResult.SetFound(cacheResult.GetFound())
					resultsByKey[key] = indexResult
					delete(remainingKeys, key)
				} else if cacheResult.HasDeleted() {
					// Key has a tombstone - store it as not found and remove from remaining
					// Tombstones in newer batches take precedence over older values
					indexResult.SetNotFound(true)
					resultsByKey[key] = indexResult
					delete(remainingKeys, key)
				} else if cacheResult.HasNotFound() {
					// Key was not found in this batch
					// Continue searching in older batches - don't remove from remainingKeys
					continue
				}
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
