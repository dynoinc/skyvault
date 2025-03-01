package recordio

import "iter"

// Record represents a single key-value record
type Record struct {
	Key       string
	Value     []byte
	Tombstone bool
}

// Records returns an iterator over the records in the data
func Records(data []byte) iter.Seq[Record] {
	return func(yield func(Record) bool) {
		pos := 0
		for pos < len(data) {
			// Read key length and tombstone bit
			if pos >= len(data) {
				return
			}
			b := data[pos]
			pos++

			keyLen := int(b >> 1)
			tombstone := (b & tombstoneBit) != 0

			// Read key
			if pos+keyLen > len(data) {
				return
			}
			key := string(data[pos : pos+keyLen])
			pos += keyLen

			if tombstone {
				if !yield(Record{Key: key, Tombstone: true}) {
					return
				}
				continue
			}

			// Read value length
			if pos >= len(data) {
				return
			}
			valueLen := int(data[pos])
			pos++

			// Read value
			if pos+valueLen > len(data) {
				return
			}
			value := data[pos : pos+valueLen]
			pos += valueLen

			if !yield(Record{Key: key, Value: value}) {
				return
			}
		}
	}
}
