package zap

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ajroetker/go-highway/hwy/contrib/varint"
)

func TestEncodedSizeComparison(t *testing.T) {
	// Generate data similar to what merge produces
	// Multiple documents with varying location counts
	var allValues []uint32
	for doc := 0; doc < 10; doc++ {
		// Count of values for this doc
		numLocs := 5 + doc%5 // 5-9 locations per doc
		numValues := numLocs * 5
		allValues = append(allValues, uint32(numValues))
		
		for loc := 0; loc < numLocs; loc++ {
			allValues = append(allValues, uint32(doc%3))                 // fieldID
			allValues = append(allValues, uint32(loc))                   // pos
			allValues = append(allValues, uint32(doc*1000+loc*10))       // start (increasing)
			allValues = append(allValues, uint32(doc*1000+loc*10+5))     // end (increasing)
			allValues = append(allValues, 0)                             // numAP
		}
	}
	t.Logf("Total values: %d", len(allValues))

	// Varint encoding
	varintBuf := make([]byte, len(allValues)*5)
	offset := 0
	for _, v := range allValues {
		n := binary.PutUvarint(varintBuf[offset:], uint64(v))
		offset += n
	}
	t.Logf("Varint encoded size: %d bytes", offset)

	// Row-oriented StreamVByte encoding
	UseColumnarLocations = false
	coder := newStreamVByteChunkedIntCoder(10240, 100)
	for _, v := range allValues {
		coder.Add(0, uint64(v))
	}
	coder.Close()
	var rowBuf bytes.Buffer
	coder.Write(&rowBuf)
	t.Logf("StreamVByte (row) encoded size: %d bytes", rowBuf.Len())

	// Also test raw StreamVByte without chunking
	ctrl, data := varint.EncodeStreamVByte32(allValues)
	t.Logf("StreamVByte raw (no chunking): %d bytes (ctrl=%d, data=%d)", len(ctrl)+len(data), len(ctrl), len(data))

	// Columnar StreamVByte encoding  
	UseColumnarLocations = true
	coder2 := newStreamVByteChunkedIntCoder(10240, 100)
	for _, v := range allValues {
		coder2.Add(0, uint64(v))
	}
	coder2.Close()
	var colBuf bytes.Buffer
	coder2.Write(&colBuf)
	t.Logf("StreamVByte (columnar) encoded size: %d bytes", colBuf.Len())

	// Reset to default
	UseColumnarLocations = true
}
