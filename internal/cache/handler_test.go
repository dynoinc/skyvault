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
	req.Msg.SetKeys([]string{"key1", "key3"})

	resp, err := h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response
	results := resp.Msg.GetResults()
	require.Equal(t, 2, len(results))

	// Check first result - Should have found value
	assert.True(t, results[0].HasFound())
	assert.Equal(t, "value1", string(results[0].GetFound()))

	// Check second result - Should have found value
	assert.True(t, results[1].HasFound())
	assert.Equal(t, "value3", string(results[1].GetFound()))

	// Test case 2: Get tombstone key
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath(objPath)
	req.Msg.SetKeys([]string{"deleted-key"})

	resp, err = h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response - should indicate deleted status
	results = resp.Msg.GetResults()
	require.Equal(t, 1, len(results))
	assert.True(t, results[0].HasDeleted())
	assert.True(t, results[0].GetDeleted())

	// Test case 3: Get non-existent key
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath(objPath)
	req.Msg.SetKeys([]string{"key4"})

	resp, err = h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response - should indicate not found
	results = resp.Msg.GetResults()
	require.Equal(t, 1, len(results))
	assert.True(t, results[0].HasNotFound())
	assert.True(t, results[0].GetNotFound())

	// Test case 4: Get mix of existing, deleted, and non-existent keys
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath(objPath)
	req.Msg.SetKeys([]string{
		"key2",         // Exists
		"deleted-key",  // Tombstone
		"non-existent", // Not found
	})

	resp, err = h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response - should have three results with correct status types
	results = resp.Msg.GetResults()
	require.Equal(t, 3, len(results))

	// First key (key2) - should be found
	assert.True(t, results[0].HasFound())
	assert.Equal(t, "value2", string(results[0].GetFound()))

	// Second key (deleted-key) - should be marked as deleted
	assert.True(t, results[1].HasDeleted())
	assert.True(t, results[1].GetDeleted())

	// Third key (non-existent) - should be marked as not found
	assert.True(t, results[2].HasNotFound())
	assert.True(t, results[2].GetNotFound())

	// Test case 5: Get from non-existent object
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath("non-existent-object")
	req.Msg.SetKeys([]string{"key1"})

	_, err = h.Get(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error retrieving object")

	// Test case 6: Invalid request - empty object path
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetKeys([]string{"key1"})

	_, err = h.Get(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object_path is required")

	// Test case 7: Invalid request - no keys
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath(objPath)
	req.Msg.SetKeys([]string{})

	_, err = h.Get(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one key is required")

	// Test case 8: Verify caching works
	// First request should have cached the object
	req = connect.NewRequest(&v1.GetRequest{})
	req.Msg.SetObjectPath(objPath)
	req.Msg.SetKeys([]string{"key2"})

	// Delete the object from the bucket to verify we're using the cache
	err = bucket.Delete(ctx, objPath)
	require.NoError(t, err)

	// This should still work because the object is cached
	resp, err = h.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the response
	results = resp.Msg.GetResults()
	require.Equal(t, 1, len(results))
	assert.True(t, results[0].HasFound())
	assert.Equal(t, "value2", string(results[0].GetFound()))
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
