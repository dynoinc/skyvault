package cache

import (
	"context"
	"fmt"
	"io"
	"sync"

	"connectrpc.com/connect"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/thanos-io/objstore"
	"google.golang.org/protobuf/types/known/emptypb"

	v1 "github.com/dynoinc/skyvault/gen/proto/cache/v1"
	"github.com/dynoinc/skyvault/gen/proto/cache/v1/v1connect"
	"github.com/dynoinc/skyvault/internal/sstable"
)

// Config holds the configuration for the cache service
type Config struct {
	Enabled      bool `default:"false"`
	MaxSizeBytes int  `default:"67108864"` // Default to 64MB cache size
}

// sizeAwareCache implements a cache with a total byte size limit
type sizeAwareCache struct {
	cache       *lru.Cache[string, []byte]
	currentSize int
	maxSize     int
	mu          sync.Mutex
}

// newSizeAwareCache creates a new cache with a total byte size limit
func newSizeAwareCache(maxSizeBytes int) (*sizeAwareCache, error) {
	// Initialize with a reasonable max items count (1024)
	// The actual limit will be enforced by the size checks
	cache, err := lru.New[string, []byte](1024)
	if err != nil {
		return nil, err
	}

	return &sizeAwareCache{
		cache:       cache,
		currentSize: 0,
		maxSize:     maxSizeBytes,
	}, nil
}

// add adds a key-value pair to the cache, respecting size limits
func (c *sizeAwareCache) add(key string, value []byte) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if the single item is too large for the cache
	if len(value) > c.maxSize {
		return false
	}

	// If the key exists, remove its size from the current total
	if oldValue, found := c.cache.Get(key); found {
		c.currentSize -= len(oldValue)
	}

	// Check if we need to make room
	for c.currentSize+len(value) > c.maxSize && c.cache.Len() > 0 {
		// Remove oldest items until we have enough space
		oldestKey, oldestValue, _ := c.cache.GetOldest()
		c.cache.Remove(oldestKey)
		c.currentSize -= len(oldestValue)
	}

	// Add the new item
	c.cache.Add(key, value)
	c.currentSize += len(value)
	return true
}

// get retrieves a value from the cache
func (c *sizeAwareCache) get(key string) ([]byte, bool) {
	return c.cache.Get(key)
}

// handler implements the CacheService
type handler struct {
	v1connect.UnimplementedCacheServiceHandler

	config Config
	ctx    context.Context
	store  objstore.Bucket

	cache *sizeAwareCache
	mu    sync.Mutex
}

// NewHandler creates a new cache service handler
func NewHandler(
	ctx context.Context,
	cfg Config,
	store objstore.Bucket,
) *handler {
	// Create a new size-aware cache with the specified max size
	cache, err := newSizeAwareCache(cfg.MaxSizeBytes)
	if err != nil {
		// This should only happen if there's a problem creating the underlying LRU cache
		panic(fmt.Sprintf("failed to create cache: %v", err))
	}

	return &handler{
		config: cfg,
		ctx:    ctx,
		store:  store,
		cache:  cache,
	}
}

// Get retrieves values for requested keys from a specific object
func (h *handler) Get(
	ctx context.Context,
	req *connect.Request[v1.GetRequest],
) (*connect.Response[v1.GetResponse], error) {
	objPath := req.Msg.GetObjectPath()
	keys := req.Msg.GetKeys()

	if objPath == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("object_path is required"))
	}
	if len(keys) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("at least one key is required"))
	}

	// Get object data from cache or object store
	data, err := h.getObjectData(ctx, objPath)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("error retrieving object: %w", err))
	}

	empty := &emptypb.Empty{}
	found := make(map[string][]byte, len(keys))
	deleted := make(map[string]*emptypb.Empty, len(keys))

	// Iterate through the records
	for record := range sstable.Records(data) {
		recordKeyStr := record.Key
		if _, ok := keys[recordKeyStr]; ok {
			// Create a result for this key
			if record.Tombstone {
				deleted[recordKeyStr] = empty
			} else {
				found[recordKeyStr] = record.Value
			}
		}
	}

	return connect.NewResponse(v1.GetResponse_builder{
		Found:   found,
		Deleted: deleted,
	}.Build()), nil
}

// getObjectData retrieves the object data from cache or object store
func (h *handler) getObjectData(ctx context.Context, objPath string) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if the object is in cache
	if data, found := h.cache.get(objPath); found {
		return data, nil
	}

	// Object not in cache, download it
	reader, err := h.store.Get(ctx, objPath)
	if err != nil {
		return nil, fmt.Errorf("downloading object: %w", err)
	}
	defer reader.Close()

	// Read all data from the reader
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading object: %w", err)
	}

	// Store in cache
	h.cache.add(objPath, data)

	return data, nil
}
