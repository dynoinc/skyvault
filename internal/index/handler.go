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
	"google.golang.org/protobuf/types/known/emptypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	cachev1 "github.com/dynoinc/skyvault/gen/proto/cache/v1"
	cachev1connect "github.com/dynoinc/skyvault/gen/proto/cache/v1/v1connect"
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

	// WALs
	wals atomic.Pointer[[]database.WriteAheadLog]

	// Shared runs
	sharedRuns atomic.Pointer[[]database.SharedRun]

	// Partitions
	partitions atomic.Pointer[[]database.Partition]

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

	// Start watching for WALs
	if err := h.watchWALs(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to watch WALs: %w", err)
	}

	// Start watching for shared runs
	if err := h.watchSharedRuns(ctx, db); err != nil {
		return nil, fmt.Errorf("failed to watch shared runs: %w", err)
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
					slog.Error("Watch channel closed unexpectedly, creating new watcher")

					// Create new watcher
					newWatcher, err := h.kubeClient.CoreV1().Pods(h.config.Namespace).Watch(ctx, metav1.ListOptions{
						LabelSelector: fmt.Sprintf("app.kubernetes.io/component=cache,skyvault.io/instance=%s", h.config.Instance),
						Watch:         true,
					})
					if err != nil {
						slog.Error("Failed to create new watcher", "error", err)
						return
					}

					// Replace old watcher and continue watching
					watcher = newWatcher
					continue
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
func (h *handler) watchWALs(ctx context.Context, db *pgxpool.Pool) error {
	updateFn := func() error {
		wals, err := database.New(db).GetWriteAheadLogs(ctx)
		if err != nil {
			return fmt.Errorf("failed to get WALs: %w", err)
		}

		// Sort the wals by SeqNo, highest first (newest first)
		sort.Slice(wals, func(i, j int) bool {
			return wals[i].SeqNo > wals[j].SeqNo
		})

		h.wals.Store(&wals)
		return nil
	}

	if err := database.Watch(ctx, db, "new_write_ahead_log", updateFn); err != nil {
		return fmt.Errorf("failed to watch WALs: %w", err)
	}

	return nil
}

// watchSharedRuns watches for changes in the shared runs
func (h *handler) watchSharedRuns(ctx context.Context, db *pgxpool.Pool) error {
	updateFn := func() error {
		sharedRuns, err := database.New(db).GetSharedRuns(ctx)
		if err != nil {
			return fmt.Errorf("failed to get shared runs: %w", err)
		}

		// Sort the shared runs by SeqNo, highest first (newest first)
		sort.Slice(sharedRuns, func(i, j int) bool {
			return sharedRuns[i].SeqNo > sharedRuns[j].SeqNo
		})

		h.sharedRuns.Store(&sharedRuns)
		return nil
	}

	if err := database.Watch(ctx, db, "shared_run_change", updateFn); err != nil {
		return fmt.Errorf("failed to watch shared runs: %w", err)
	}

	return nil
}

// watchPartitions watches for changes in the partitions
func (h *handler) watchPartitions(ctx context.Context, db *pgxpool.Pool) error {
	updateFn := func() error {
		partitions, err := database.New(db).GetPartitions(ctx)
		if err != nil {
			return fmt.Errorf("failed to get partitions: %w", err)
		}

		// Sort the partitions by InclusiveStartKey
		sort.Slice(partitions, func(i, j int) bool {
			return partitions[i].InclusiveStartKey < partitions[j].InclusiveStartKey
		})

		h.partitions.Store(&partitions)
		return nil
	}

	if err := database.Watch(ctx, db, "partition_change", updateFn); err != nil {
		return fmt.Errorf("failed to watch partitions: %w", err)
	}

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

func (h *handler) lookupSSTable(ctx context.Context, keys map[string]*emptypb.Empty, path string) (*cachev1.GetResponse, error) {
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

	return resp.Msg, nil
}

// Get retrieves values for requested keys by checking all l0_batches
func (h *handler) BatchGet(
	ctx context.Context,
	req *connect.Request[v1.BatchGetRequest],
) (*connect.Response[v1.BatchGetResponse], error) {
	var empty = &emptypb.Empty{}
	remainingKeys := make(map[string]*emptypb.Empty)
	for _, key := range req.Msg.GetKeys() {
		remainingKeys[key] = empty
	}

	resp := make(map[string][]byte, len(remainingKeys))

	// First check WAL
	for _, wal := range *h.wals.Load() {
		if len(remainingKeys) == 0 {
			break
		}

		pr, err := h.lookupSSTable(ctx, remainingKeys, wal.Attrs.GetPath())
		if err != nil {
			return nil, err
		}

		for key, result := range pr.GetFound() {
			resp[key] = result
			delete(remainingKeys, key)
		}

		for key := range pr.GetDeleted() {
			delete(remainingKeys, key)
		}
	}

	// Then check shared runs
	if len(remainingKeys) > 0 {
		for _, sharedRun := range *h.sharedRuns.Load() {
			pr, err := h.lookupSSTable(ctx, remainingKeys, sharedRun.Attrs.GetPath())
			if err != nil {
				return nil, err
			}

			for key, result := range pr.GetFound() {
				resp[key] = result
				delete(remainingKeys, key)
			}

			for key := range pr.GetDeleted() {
				delete(remainingKeys, key)
			}
		}
	}

	// Then check partitions
	if len(remainingKeys) > 0 {
		partitions := *h.partitions.Load()

		// Create requests for each group of keys in the same partition
		reqs := make(map[int]map[string]*emptypb.Empty)
		for key := range remainingKeys {
			i := sort.Search(len(partitions), func(i int) bool {
				return partitions[i].InclusiveStartKey > key
			})
			if i == 0 {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("key %q not found in any partition", key))
			}
			if _, ok := reqs[i-1]; !ok {
				reqs[i-1] = make(map[string]*emptypb.Empty)
			}
			reqs[i-1][key] = empty
		}

		for idx := range reqs {
			p := partitions[idx]

			for _, run := range p.Attrs.GetRuns() {
				pr, err := h.lookupSSTable(ctx, reqs[idx], run.GetPath())
				if err != nil {
					return nil, err
				}

				for key, result := range pr.GetFound() {
					resp[key] = result
					delete(remainingKeys, key)
				}

				for key := range pr.GetDeleted() {
					delete(remainingKeys, key)
				}
			}
		}
	}

	return connect.NewResponse(v1.BatchGetResponse_builder{
		Results: resp,
	}.Build()), nil
}
