package recordio

import (
	"encoding/binary"
	"iter"
)

// Record represents a single key-value record
type Record struct {
	Key       string
	Value     []byte
	Tombstone bool
}

// Records returns an iterator over the records in the data
func Records(data []byte) iter.Seq[Record] {
	return func(yield func(Record) bool) {
		if len(data) == 0 {
			return
		}

		// Read version varint
		version, n := binary.Uvarint(data)
		if n <= 0 {
			// Invalid varint
			return
		}
		pos := n // Start after the version varint

		// Currently we only support version 1
		if version != currentVersion {
			// We can't yield an error directly in the iterator,
			// so we yield nothing if version is unsupported
			return
		}

		for pos < len(data) {
			// Read key length and tombstone bit as varint
			keyLen, bytesRead := binary.Uvarint(data[pos:])
			if bytesRead <= 0 {
				// Invalid varint
				return
			}
			pos += bytesRead

			// Extract tombstone bit and key length
			tombstone := (keyLen & tombstoneBit) != 0
			keyLenValue := int(keyLen >> 1)

			// Read key
			if pos+keyLenValue > len(data) {
				return
			}
			key := string(data[pos : pos+keyLenValue])
			pos += keyLenValue

			if tombstone {
				if !yield(Record{Key: key, Tombstone: true}) {
					return
				}
				continue
			}

			// Read value length as varint
			valueLen, bytesRead := binary.Uvarint(data[pos:])
			if bytesRead <= 0 {
				// Invalid varint
				return
			}
			pos += bytesRead

			// Read value
			if pos+int(valueLen) > len(data) {
				return
			}
			value := data[pos : pos+int(valueLen)]
			pos += int(valueLen)

			if !yield(Record{Key: key, Value: value}) {
				return
			}
		}
	}
}
