package recordio

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteAndReadSingleRecord(t *testing.T) {
	records := []Record{{
		Key:   "key1",
		Value: []byte("value1"),
	}}

	// Write records
	data := WriteRecords(records)

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

	data := WriteRecords(records)
	var readRecords []Record
	for r := range Records(data) {
		readRecords = append(readRecords, r)
	}

	require.Equal(t, records, readRecords)
}

func TestDeduplication(t *testing.T) {
	records := []Record{
		{Key: "key1", Value: []byte("value1")},
		{Key: "key1", Value: []byte("value2")},
		{Key: "key1", Value: []byte("value3")},
	}

	data := WriteRecords(records)
	var readRecords []Record
	for r := range Records(data) {
		readRecords = append(readRecords, r)
	}

	require.Len(t, readRecords, 1)
	require.Contains(t, []string{"value1", "value2", "value3"}, string(readRecords[0].Value))
}

func TestTombstones(t *testing.T) {
	records := []Record{
		{Key: "key1", Value: []byte("value1")},
		{Key: "key2", Tombstone: true},
		{Key: "key3", Value: []byte("value2")},
	}

	data := WriteRecords(records)
	var readRecords []Record
	for r := range Records(data) {
		readRecords = append(readRecords, r)
	}

	require.Len(t, readRecords, 3)
	require.Equal(t, "key1", readRecords[0].Key)
	require.Equal(t, []byte("value1"), readRecords[0].Value)
	require.Equal(t, "key2", readRecords[1].Key)
	require.True(t, readRecords[1].Tombstone)
	require.Equal(t, "key3", readRecords[2].Key)
	require.Equal(t, []byte("value2"), readRecords[2].Value)
}

func TestSorting(t *testing.T) {
	records := []Record{
		{Key: "key3", Value: []byte("value3")},
		{Key: "key1", Value: []byte("value1")},
		{Key: "key2", Value: []byte("value2")},
	}

	data := WriteRecords(records)
	var readRecords []Record
	for r := range Records(data) {
		readRecords = append(readRecords, r)
	}

	require.Len(t, readRecords, 3)
	require.Equal(t, "key1", readRecords[0].Key)
	require.Equal(t, "key2", readRecords[1].Key)
	require.Equal(t, "key3", readRecords[2].Key)
}
