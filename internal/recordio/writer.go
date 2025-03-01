package recordio

import (
	"slices"
	"strings"
)

const (
	tombstoneBit = 1 // Use lowest bit for tombstone flag
)

// WriteRecords takes a slice of records, sorts them by key, deduplicates them
// and writes them to a buffer
func WriteRecords(records []Record) []byte {
	// Sort records by key
	slices.SortFunc(records, func(a, b Record) int {
		return strings.Compare(a.Key, b.Key)
	})

	// Deduplicate records
	records = deduplicateRecords(records)

	// Pre-calculate buffer size to avoid reallocations
	size := 0
	for _, r := range records {
		size += 1 // key length byte
		size += len(r.Key)
		if !r.Tombstone {
			size += 1 // value length byte
			size += len(r.Value)
		}
	}

	buf := make([]byte, 0, size)

	// Write records
	for _, r := range records {
		// Write key length with tombstone bit
		keyLen := (len(r.Key) << 1)
		if r.Tombstone {
			keyLen |= tombstoneBit
		}
		buf = append(buf, byte(keyLen))

		// Write key
		buf = append(buf, []byte(r.Key)...)

		if !r.Tombstone {
			// Write value length
			buf = append(buf, byte(len(r.Value)))
			// Write value
			buf = append(buf, r.Value...)
		}
	}

	return buf
}

func deduplicateRecords(records []Record) []Record {
	if len(records) <= 1 {
		return records
	}

	// Since records are sorted, we can just keep the first record for each key
	result := make([]Record, 0, len(records))
	for i := 0; i < len(records); i++ {
		if i == 0 || records[i].Key != records[i-1].Key {
			result = append(result, records[i])
		}
	}
	return result
}
