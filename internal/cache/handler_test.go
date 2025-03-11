package cache

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"google.golang.org/protobuf/types/known/emptypb"

	v1 "github.com/dynoinc/skyvault/gen/proto/cache/v1"
	"github.com/dynoinc/skyvault/internal/sstable"
)

func TestHandler_Get(t *testing.T) {
	// Create a mock bucket
	bucket := objstore.NewInMemBucket()
	ctx := context.Background()

	// Create test data with sstable
	records := []sstable.Record{
		{Key: "key1", Value: []byte("value1")},
		{Key: "key2", Value: []byte("value2")},
		{Key: "key3", Value: []byte("value3")},
		{Key: "deleted-key", Tombstone: true},
	}

	data := sstable.WriteRecords(slices.Values(records))

	// Upload test data to the mock bucket
	objPath := "test/object.data"
	err := bucket.Upload(ctx, objPath, bytes.NewReader(data))
	require.NoError(t, err)

	// Create the handler
	h := NewHandler(ctx, Config{
		Enabled:      true,
		MaxSizeBytes: 1024 * 1024, // 1MB is enough for tests
	}, bucket)

	// Test case 1: Get existing keys
	req := connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath(objPath)
	req.Msg.SetKeys(map[string]*emptypb.Empty{
		"key1": {},
		"key3": {},
	})

	resp, err := h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response
	found := resp.Msg.GetFound()
	require.Equal(t, 2, len(found))

	// Check first result - Should have found value
	assert.Equal(t, "value1", string(found["key1"]))

	// Check second result - Should have found value
	assert.Equal(t, "value3", string(found["key3"]))

	// Test case 2: Get tombstone key
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath(objPath)
	req.Msg.SetKeys(map[string]*emptypb.Empty{
		"deleted-key": {},
	})

	resp, err = h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response - should indicate deleted status
	deleted := resp.Msg.GetDeleted()
	require.Equal(t, 1, len(deleted))
	assert.Equal(t, deleted["deleted-key"], &emptypb.Empty{})

	// Test case 3: Get non-existent key
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath(objPath)
	req.Msg.SetKeys(map[string]*emptypb.Empty{
		"key4": {},
	})

	resp, err = h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response does not contain the key in found or deleted
	found = resp.Msg.GetFound()
	require.Equal(t, 0, len(found))
	deleted = resp.Msg.GetDeleted()
	require.Equal(t, 0, len(deleted))

	// Test case 4: Get mix of existing, deleted, and non-existent keys
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath(objPath)
	req.Msg.SetKeys(map[string]*emptypb.Empty{
		"key2":         {},
		"deleted-key":  {},
		"non-existent": {},
	})

	resp, err = h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response - should have three results with correct status types
	found = resp.Msg.GetFound()
	require.Equal(t, 1, len(found))

	// First key (key2) - should be found
	assert.Equal(t, "value2", string(found["key2"]))

	// Second key (deleted-key) - should be marked as deleted
	deleted = resp.Msg.GetDeleted()
	require.Equal(t, 1, len(deleted))
	assert.Equal(t, deleted["deleted-key"], &emptypb.Empty{})

	// Third key (non-existent) - should not be marked as found or deleted
	assert.NotContains(t, found, "non-existent")
	assert.NotContains(t, deleted, "non-existent")

	// Test case 5: Get from non-existent object
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath("non-existent-object")
	req.Msg.SetKeys(map[string]*emptypb.Empty{
		"key1": {},
	})

	_, err = h.Get(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error retrieving object")

	// Test case 6: Invalid request - empty object path
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys(map[string]*emptypb.Empty{
		"key1": {},
	})

	_, err = h.Get(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object_path is required")

	// Test case 7: Invalid request - no keys
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath(objPath)
	req.Msg.SetKeys(map[string]*emptypb.Empty{})

	_, err = h.Get(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one key is required")

	// Test case 8: Verify caching works
	// First request should have cached the object
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath(objPath)
	req.Msg.SetKeys(map[string]*emptypb.Empty{
		"key2": {},
	})

	// Delete the object from the bucket to verify we're using the cache
	err = bucket.Delete(ctx, objPath)
	require.NoError(t, err)

	// This should still work because the object is cached
	resp, err = h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response
	found = resp.Msg.GetFound()
	require.Equal(t, 1, len(found))
	assert.Equal(t, "value2", string(found["key2"]))
}

func TestSizeBasedEviction(t *testing.T) {
	// Create a mock bucket
	bucket := objstore.NewInMemBucket()
	ctx := context.Background()

	// Create a small cache
	maxCacheSize := 100 // Just 100 bytes
	h := NewHandler(ctx, Config{
		Enabled:      true,
		MaxSizeBytes: maxCacheSize,
	}, bucket)

	// Create objects of different sizes
	obj1Data := make([]byte, 40) // 40 bytes
	obj2Data := make([]byte, 40) // 40 bytes
	obj3Data := make([]byte, 40) // 40 bytes
	// Total of these three would exceed our 100 byte limit

	// Fill with different values to identify them
	for i := range obj1Data {
		obj1Data[i] = 1
	}
	for i := range obj2Data {
		obj2Data[i] = 2
	}
	for i := range obj3Data {
		obj3Data[i] = 3
	}

	// Upload objects to the bucket
	objPath1 := "test/object1.data"
	objPath2 := "test/object2.data"
	objPath3 := "test/object3.data"

	err := bucket.Upload(ctx, objPath1, bytes.NewReader(obj1Data))
	require.NoError(t, err)
	err = bucket.Upload(ctx, objPath2, bytes.NewReader(obj2Data))
	require.NoError(t, err)
	err = bucket.Upload(ctx, objPath3, bytes.NewReader(obj3Data))
	require.NoError(t, err)

	// Put all objects in cache
	data1, err := h.getObjectData(ctx, objPath1)
	require.NoError(t, err)
	assert.Equal(t, obj1Data, data1)

	data2, err := h.getObjectData(ctx, objPath2)
	require.NoError(t, err)
	assert.Equal(t, obj2Data, data2)

	// After adding obj3, obj1 should be evicted since it's the oldest
	data3, err := h.getObjectData(ctx, objPath3)
	require.NoError(t, err)
	assert.Equal(t, obj3Data, data3)

	// Delete the objects from the bucket to verify cache behavior
	err = bucket.Delete(ctx, objPath1)
	require.NoError(t, err)
	err = bucket.Delete(ctx, objPath2)
	require.NoError(t, err)
	err = bucket.Delete(ctx, objPath3)
	require.NoError(t, err)

	// Check what's in the cache
	// obj1 should be evicted
	_, err = h.getObjectData(ctx, objPath1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "downloading object")

	// obj2 should still be in cache
	data2, err = h.getObjectData(ctx, objPath2)
	require.NoError(t, err)
	assert.Equal(t, obj2Data, data2)

	// obj3 should still be in cache
	data3, err = h.getObjectData(ctx, objPath3)
	require.NoError(t, err)
	assert.Equal(t, obj3Data, data3)
}
