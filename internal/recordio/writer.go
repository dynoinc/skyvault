package recordio

import "encoding/binary"

const (
	tombstoneBit   = 1         // Use lowest bit for tombstone flag
	currentVersion = uint64(1) // Current file format version
)

// ComputeSize calculates the size in bytes of the recordio file after serialization
func ComputeSize(records []Record) int {
	buf := make([]byte, binary.MaxVarintLen64)

	// version
	versionBytes := binary.PutUvarint(buf, currentVersion)
	size := versionBytes

	for _, r := range records {
		// Key length with tombstone flag
		keyLen := uint64(len(r.Key) << 1)
		if r.Tombstone {
			keyLen |= tombstoneBit
		}

		keyLenBytes := binary.PutUvarint(buf, keyLen)
		size += keyLenBytes
		size += len(r.Key)

		if !r.Tombstone {
			// Value length
			valueLen := uint64(len(r.Value))
			valueLenBytes := binary.PutUvarint(buf, valueLen)
			size += valueLenBytes
			size += len(r.Value)
		}
	}

	return size
}

// WriteRecords takes a slice of records and writes them to a buffer
func WriteRecords(records []Record) []byte {
	// Pre-calculate buffer size
	size := ComputeSize(records)
	buf := make([]byte, 0, size)

	// version
	buf = binary.AppendUvarint(buf, currentVersion)

	for _, r := range records {
		// Write key length with tombstone bit
		keyLen := uint64(len(r.Key) << 1)
		if r.Tombstone {
			keyLen |= tombstoneBit
		}

		// Write key length as varint
		buf = binary.AppendUvarint(buf, keyLen)

		// Write key
		buf = append(buf, []byte(r.Key)...)

		if !r.Tombstone {
			// Write value length as varint
			buf = binary.AppendUvarint(buf, uint64(len(r.Value)))

			// Write value
			buf = append(buf, r.Value...)
		}
	}

	return buf
}
