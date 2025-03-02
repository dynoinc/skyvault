package recordio

import (
	"encoding/binary"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionEncoding(t *testing.T) {
	// Create an empty record set
	records := []Record{}
	data := WriteRecords(slices.Values(records))

	// Even with no records, we should have a version
	require.NotEmpty(t, data)

	// Verify the version can be decoded as a varint
	version, n := binary.Uvarint(data)
	require.Greater(t, n, 0)
	require.Equal(t, currentVersion, version)
}

func TestComputeSize(t *testing.T) {
	records := []Record{
		{Key: "key1", Value: []byte("value1")},
		{Key: "key2", Value: []byte("value2")},
		{Key: "key3", Tombstone: true},
	}

	// Compute expected size
	calculatedSize := ComputeSize(slices.Values(records))

	// Write records and check actual size
	data := WriteRecords(slices.Values(records))
	require.Equal(t, len(data), calculatedSize)
}

func TestWriteAndReadSingleRecord(t *testing.T) {
	records := []Record{{
		Key:   "key1",
		Value: []byte("value1"),
	}}

	// Write records
	data := WriteRecords(slices.Values(records))

	// Read and verify
	var readRecords []Record
	for r := range Records(data) {
		readRecords = append(readRecords, r)
	}

	require.Equal(t, records, readRecords)
}

func TestWriteAndReadMultipleRecords(t *testing.T) {
	records := []Record{
		{Key: "key1", Value: []byte("value1")},
		{Key: "key2", Value: []byte("value2")},
		{Key: "key3", Value: []byte("value3")},
	}

	data := WriteRecords(slices.Values(records))
	var readRecords []Record
	for r := range Records(data) {
		readRecords = append(readRecords, r)
	}

	require.Equal(t, records, readRecords)
}

func TestDuplicateKeys(t *testing.T) {
	records := []Record{
		{Key: "key1", Value: []byte("value1")},
		{Key: "key1", Value: []byte("value2")},
		{Key: "key1", Value: []byte("value3")},
	}

	data := WriteRecords(slices.Values(records))
	var readRecords []Record
	for r := range Records(data) {
		readRecords = append(readRecords, r)
	}

	// We now expect all records to be preserved (no deduplication)
	require.Len(t, readRecords, 3)

	// Verify all values appear in the output
	valueSet := make(map[string]bool)
	for _, r := range readRecords {
		valueSet[string(r.Value)] = true
	}

	require.True(t, valueSet["value1"])
	require.True(t, valueSet["value2"])
	require.True(t, valueSet["value3"])
}

func TestTombstones(t *testing.T) {
	records := []Record{
		{Key: "key1", Value: []byte("value1")},
		{Key: "key2", Tombstone: true},
		{Key: "key3", Value: []byte("value2")},
	}

	data := WriteRecords(slices.Values(records))
	var readRecords []Record
	for r := range Records(data) {
		readRecords = append(readRecords, r)
	}

	require.Len(t, readRecords, 3)

	// Map results for easy lookup since order is not guaranteed
	resultMap := make(map[string]Record)
	for _, r := range readRecords {
		resultMap[r.Key] = r
	}

	require.Equal(t, []byte("value1"), resultMap["key1"].Value)
	require.True(t, resultMap["key2"].Tombstone)
	require.Equal(t, []byte("value2"), resultMap["key3"].Value)
}

func TestPreservesOrder(t *testing.T) {
	records := []Record{
		{Key: "key3", Value: []byte("value3")},
		{Key: "key1", Value: []byte("value1")},
		{Key: "key2", Value: []byte("value2")},
	}

	data := WriteRecords(slices.Values(records))
	var readRecords []Record
	for r := range Records(data) {
		readRecords = append(readRecords, r)
	}

	require.Len(t, readRecords, 3)

	// Verify the original order is preserved (no sorting)
	require.Equal(t, "key3", readRecords[0].Key)
	require.Equal(t, "key1", readRecords[1].Key)
	require.Equal(t, "key2", readRecords[2].Key)
}
