package zap

import (
	"encoding/binary"
	"testing"

	"github.com/ajroetker/go-highway/hwy/contrib/varint"
)

// Simulate typical location data with count prefix: [count] + 5 values per location
// 20 locations per document = 1 + 100 = 101 values
func generateMergeLocationData() []uint32 {
	values := make([]uint32, 0, 101)
	// Add count prefix (what the merge path does)
	values = append(values, 100) // 100 values follow
	for loc := 0; loc < 20; loc++ {
		values = append(values, uint32(loc%3))    // fieldID (small)
		values = append(values, uint32(loc*5))    // pos (increasing)
		values = append(values, uint32(loc*10))   // start (increasing)
		values = append(values, uint32(loc*10+5)) // end
		values = append(values, 0)                // numAP (usually 0)
	}
	return values
}

// BenchmarkMergeCycle_Varint simulates the merge cycle with varint
func BenchmarkMergeCycle_Varint(b *testing.B) {
	srcValues := generateMergeLocationData()

	// Encode source
	srcEncoded := make([]byte, len(srcValues)*5)
	offset := 0
	for _, v := range srcValues {
		n := binary.PutUvarint(srcEncoded[offset:], uint64(v))
		offset += n
	}
	srcEncoded = srcEncoded[:offset]

	// Buffers for decode/re-encode
	decoded := make([]uint32, len(srcValues))
	dstEncoded := make([]byte, len(srcValues)*5)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Decode (simulating read from source segment)
		readOffset := 0
		for j := 0; j < len(decoded); j++ {
			v, n := binary.Uvarint(srcEncoded[readOffset:])
			decoded[j] = uint32(v)
			readOffset += n
		}

		// Re-encode (simulating write to dest segment)
		writeOffset := 0
		for _, v := range decoded {
			n := binary.PutUvarint(dstEncoded[writeOffset:], uint64(v))
			writeOffset += n
		}
	}
}

// BenchmarkMergeCycle_StreamVByte simulates the merge cycle with row-oriented StreamVByte
func BenchmarkMergeCycle_StreamVByte(b *testing.B) {
	// Disable columnar for this benchmark
	oldColumnar := UseColumnarLocations
	UseColumnarLocations = false
	defer func() { UseColumnarLocations = oldColumnar }()

	srcValues := generateMergeLocationData()

	// Encode source
	srcControl, srcData := varint.EncodeStreamVByte32(srcValues)

	// Buffers for decode/re-encode
	decoded := make([]uint32, len(srcValues))
	ctrlBuf := make([]byte, 0, 32)
	dataBuf := make([]byte, 0, 500)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Decode (simulating read from source segment)
		varint.DecodeStreamVByte32Into(srcControl, srcData, decoded)

		// Re-encode (simulating write to dest segment)
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decoded, ctrlBuf, dataBuf)
	}
}

// generateLargeValueData generates data with larger values (2-4 byte varints)
func generateLargeValueData() []uint32 {
	values := make([]uint32, 0, 100)
	for i := 0; i < 100; i++ {
		// Values that require 2-4 bytes in varint encoding
		values = append(values, uint32(1000+i*1000)) // 1000-100000 range
	}
	return values
}

// BenchmarkMergeCycle_Varint_LargeValues tests varint with larger values
func BenchmarkMergeCycle_Varint_LargeValues(b *testing.B) {
	srcValues := generateLargeValueData()

	// Encode source
	srcEncoded := make([]byte, len(srcValues)*5)
	offset := 0
	for _, v := range srcValues {
		n := binary.PutUvarint(srcEncoded[offset:], uint64(v))
		offset += n
	}
	srcEncoded = srcEncoded[:offset]

	// Buffers for decode/re-encode
	decoded := make([]uint32, len(srcValues))
	dstEncoded := make([]byte, len(srcValues)*5)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		readOffset := 0
		for j := 0; j < len(decoded); j++ {
			v, n := binary.Uvarint(srcEncoded[readOffset:])
			decoded[j] = uint32(v)
			readOffset += n
		}
		writeOffset := 0
		for _, v := range decoded {
			n := binary.PutUvarint(dstEncoded[writeOffset:], uint64(v))
			writeOffset += n
		}
	}
}

// BenchmarkMergeCycle_StreamVByte_LargeValues tests StreamVByte with larger values
func BenchmarkMergeCycle_StreamVByte_LargeValues(b *testing.B) {
	srcValues := generateLargeValueData()

	// Encode source
	srcControl, srcData := varint.EncodeStreamVByte32(srcValues)

	// Buffers for decode/re-encode
	decoded := make([]uint32, len(srcValues))
	ctrlBuf := make([]byte, 0, 32)
	dataBuf := make([]byte, 0, 500)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		varint.DecodeStreamVByte32Into(srcControl, srcData, decoded)
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decoded, ctrlBuf, dataBuf)
	}
}

// BenchmarkDecodeOnly_Varint tests varint decode-only performance
func BenchmarkDecodeOnly_Varint(b *testing.B) {
	srcValues := generateMergeLocationData()

	// Encode source
	srcEncoded := make([]byte, len(srcValues)*5)
	offset := 0
	for _, v := range srcValues {
		n := binary.PutUvarint(srcEncoded[offset:], uint64(v))
		offset += n
	}
	srcEncoded = srcEncoded[:offset]

	decoded := make([]uint32, len(srcValues))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		readOffset := 0
		for j := 0; j < len(decoded); j++ {
			v, n := binary.Uvarint(srcEncoded[readOffset:])
			decoded[j] = uint32(v)
			readOffset += n
		}
	}
}

// BenchmarkDecodeOnly_StreamVByte tests StreamVByte decode-only performance
func BenchmarkDecodeOnly_StreamVByte(b *testing.B) {
	srcValues := generateMergeLocationData()

	srcControl, srcData := varint.EncodeStreamVByte32(srcValues)
	decoded := make([]uint32, len(srcValues))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		varint.DecodeStreamVByte32Into(srcControl, srcData, decoded)
	}
}

// BenchmarkDecodeOnly_Varint_LargeValues tests varint decode with larger values
func BenchmarkDecodeOnly_Varint_LargeValues(b *testing.B) {
	srcValues := generateLargeValueData()

	srcEncoded := make([]byte, len(srcValues)*5)
	offset := 0
	for _, v := range srcValues {
		n := binary.PutUvarint(srcEncoded[offset:], uint64(v))
		offset += n
	}
	srcEncoded = srcEncoded[:offset]

	decoded := make([]uint32, len(srcValues))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		readOffset := 0
		for j := 0; j < len(decoded); j++ {
			v, n := binary.Uvarint(srcEncoded[readOffset:])
			decoded[j] = uint32(v)
			readOffset += n
		}
	}
}

// BenchmarkDecodeOnly_StreamVByte_LargeValues tests StreamVByte decode with larger values
func BenchmarkDecodeOnly_StreamVByte_LargeValues(b *testing.B) {
	srcValues := generateLargeValueData()

	srcControl, srcData := varint.EncodeStreamVByte32(srcValues)
	decoded := make([]uint32, len(srcValues))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		varint.DecodeStreamVByte32Into(srcControl, srcData, decoded)
	}
}

// BenchmarkMergeCycle_ColumnarDelta tests columnar with delta encoding for monotonic fields
func BenchmarkMergeCycle_ColumnarDelta(b *testing.B) {
	numLocs := 20
	// Delta-encode monotonically increasing fields
	fieldIDs := make([]uint32, numLocs)    // not delta (small values 0-2)
	posDelta := make([]uint32, numLocs)    // delta encoded
	startDelta := make([]uint32, numLocs)  // delta encoded
	endDelta := make([]uint32, numLocs)    // delta encoded (end - start = length)
	numAPs := make([]uint32, numLocs)      // not delta (always 0)

	for i := 0; i < numLocs; i++ {
		fieldIDs[i] = uint32(i % 3)
		if i == 0 {
			posDelta[i] = 0
			startDelta[i] = 0
		} else {
			posDelta[i] = 5   // constant delta
			startDelta[i] = 10 // constant delta
		}
		endDelta[i] = 5  // end - start = 5 (length)
		numAPs[i] = 0
	}

	// Encode each column
	ctrlFieldIDs, dataFieldIDs := varint.EncodeStreamVByte32(fieldIDs)
	ctrlPosDelta, dataPosDelta := varint.EncodeStreamVByte32(posDelta)
	ctrlStartDelta, dataStartDelta := varint.EncodeStreamVByte32(startDelta)
	ctrlEndDelta, dataEndDelta := varint.EncodeStreamVByte32(endDelta)
	ctrlNumAPs, dataNumAPs := varint.EncodeStreamVByte32(numAPs)

	// Decode buffers
	decFieldIDs := make([]uint32, numLocs)
	decPosDelta := make([]uint32, numLocs)
	decStartDelta := make([]uint32, numLocs)
	decEndDelta := make([]uint32, numLocs)
	decNumAPs := make([]uint32, numLocs)

	// Encode buffers
	ctrlBuf := make([]byte, 0, 32)
	dataBuf := make([]byte, 0, 128)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Decode all columns
		varint.DecodeStreamVByte32Into(ctrlFieldIDs, dataFieldIDs, decFieldIDs)
		varint.DecodeStreamVByte32Into(ctrlPosDelta, dataPosDelta, decPosDelta)
		varint.DecodeStreamVByte32Into(ctrlStartDelta, dataStartDelta, decStartDelta)
		varint.DecodeStreamVByte32Into(ctrlEndDelta, dataEndDelta, decEndDelta)
		varint.DecodeStreamVByte32Into(ctrlNumAPs, dataNumAPs, decNumAPs)

		// Re-encode all columns
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decFieldIDs, ctrlBuf, dataBuf)
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decPosDelta, ctrlBuf, dataBuf)
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decStartDelta, ctrlBuf, dataBuf)
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decEndDelta, ctrlBuf, dataBuf)
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decNumAPs, ctrlBuf, dataBuf)
	}
}

// TestEncodedSizes reports the encoded sizes for each format
func TestEncodedSizes(t *testing.T) {
	// Small values (typical location data)
	smallValues := generateMergeLocationData()

	// Large values
	largeValues := generateLargeValueData()

	// Varint encoding size
	varintSmall := make([]byte, len(smallValues)*5)
	offset := 0
	for _, v := range smallValues {
		n := binary.PutUvarint(varintSmall[offset:], uint64(v))
		offset += n
	}
	varintSmallSize := offset

	varintLarge := make([]byte, len(largeValues)*5)
	offset = 0
	for _, v := range largeValues {
		n := binary.PutUvarint(varintLarge[offset:], uint64(v))
		offset += n
	}
	varintLargeSize := offset

	// StreamVByte encoding size
	ctrlSmall, dataSmall := varint.EncodeStreamVByte32(smallValues)
	svbSmallSize := len(ctrlSmall) + len(dataSmall)

	ctrlLarge, dataLarge := varint.EncodeStreamVByte32(largeValues)
	svbLargeSize := len(ctrlLarge) + len(dataLarge)

	// Columnar encoding size (20 locations × 5 fields) - small positions
	numLocs := 20
	fieldIDs := make([]uint32, numLocs)
	positions := make([]uint32, numLocs)
	starts := make([]uint32, numLocs)
	ends := make([]uint32, numLocs)
	numAPs := make([]uint32, numLocs)

	for i := 0; i < numLocs; i++ {
		fieldIDs[i] = uint32(i % 3)
		positions[i] = uint32(i * 5)
		starts[i] = uint32(i * 10)
		ends[i] = uint32(i*10 + 5)
		numAPs[i] = 0
	}

	c1, d1 := varint.EncodeStreamVByte32(fieldIDs)
	c2, d2 := varint.EncodeStreamVByte32(positions)
	c3, d3 := varint.EncodeStreamVByte32(starts)
	c4, d4 := varint.EncodeStreamVByte32(ends)
	c5, d5 := varint.EncodeStreamVByte32(numAPs)
	columnarSize := len(c1) + len(d1) + len(c2) + len(d2) + len(c3) + len(d3) + len(c4) + len(d4) + len(c5) + len(d5)

	// Columnar with delta encoding (small positions)
	posDelta := make([]uint32, numLocs)
	startDelta := make([]uint32, numLocs)
	endDelta := make([]uint32, numLocs) // stores length (end - start)

	for i := 0; i < numLocs; i++ {
		if i == 0 {
			posDelta[i] = positions[i]
			startDelta[i] = starts[i]
		} else {
			posDelta[i] = positions[i] - positions[i-1]
			startDelta[i] = starts[i] - starts[i-1]
		}
		endDelta[i] = ends[i] - starts[i] // length
	}

	c1d, d1d := varint.EncodeStreamVByte32(fieldIDs)
	c2d, d2d := varint.EncodeStreamVByte32(posDelta)
	c3d, d3d := varint.EncodeStreamVByte32(startDelta)
	c4d, d4d := varint.EncodeStreamVByte32(endDelta)
	c5d, d5d := varint.EncodeStreamVByte32(numAPs)
	columnarDeltaSize := len(c1d) + len(d1d) + len(c2d) + len(d2d) + len(c3d) + len(d3d) + len(c4d) + len(d4d) + len(c5d) + len(d5d)

	// Large document positions (simulating a document with 20 term occurrences spread across 50KB)
	// Each position is ~2500 bytes apart on average
	positionsLarge := make([]uint32, numLocs)
	startsLarge := make([]uint32, numLocs)
	endsLarge := make([]uint32, numLocs)

	for i := 0; i < numLocs; i++ {
		positionsLarge[i] = uint32(i * 2500)       // 0, 2500, 5000, ...47500
		startsLarge[i] = uint32(i * 2500)          // byte offset
		endsLarge[i] = uint32(i*2500 + 50)         // 50 byte terms
	}

	// Columnar without delta (large positions)
	c2Large, d2Large := varint.EncodeStreamVByte32(positionsLarge)
	c3Large, d3Large := varint.EncodeStreamVByte32(startsLarge)
	c4Large, d4Large := varint.EncodeStreamVByte32(endsLarge)
	columnarLargeSize := len(c1) + len(d1) + len(c2Large) + len(d2Large) + len(c3Large) + len(d3Large) + len(c4Large) + len(d4Large) + len(c5) + len(d5)

	// Columnar with delta (large positions)
	posDeltaLarge := make([]uint32, numLocs)
	startDeltaLarge := make([]uint32, numLocs)
	endLengthsLarge := make([]uint32, numLocs)

	for i := 0; i < numLocs; i++ {
		if i == 0 {
			posDeltaLarge[i] = positionsLarge[i]
			startDeltaLarge[i] = startsLarge[i]
		} else {
			posDeltaLarge[i] = positionsLarge[i] - positionsLarge[i-1]   // constant 2500
			startDeltaLarge[i] = startsLarge[i] - startsLarge[i-1]       // constant 2500
		}
		endLengthsLarge[i] = endsLarge[i] - startsLarge[i] // constant 50
	}

	c2dLarge, d2dLarge := varint.EncodeStreamVByte32(posDeltaLarge)
	c3dLarge, d3dLarge := varint.EncodeStreamVByte32(startDeltaLarge)
	c4dLarge, d4dLarge := varint.EncodeStreamVByte32(endLengthsLarge)
	columnarDeltaLargeSize := len(c1d) + len(d1d) + len(c2dLarge) + len(d2dLarge) + len(c3dLarge) + len(d3dLarge) + len(c4dLarge) + len(d4dLarge) + len(c5d) + len(d5d)

	// Row-oriented StreamVByte for large positions
	rowLargeValues := make([]uint32, 0, 101)
	rowLargeValues = append(rowLargeValues, 100)
	for i := 0; i < numLocs; i++ {
		rowLargeValues = append(rowLargeValues, fieldIDs[i], positionsLarge[i], startsLarge[i], endsLarge[i], numAPs[i])
	}
	ctrlRowLarge, dataRowLarge := varint.EncodeStreamVByte32(rowLargeValues)
	rowLargeSize := len(ctrlRowLarge) + len(dataRowLarge)

	// Varint for large positions
	varintRowLarge := make([]byte, len(rowLargeValues)*5)
	offset = 0
	for _, v := range rowLargeValues {
		n := binary.PutUvarint(varintRowLarge[offset:], uint64(v))
		offset += n
	}
	varintRowLargeSize := offset

	t.Logf("=== Encoded Sizes (bytes) ===")
	t.Logf("")
	t.Logf("Small values (%d values):", len(smallValues))
	t.Logf("  Varint:      %4d bytes (%.2f bytes/value)", varintSmallSize, float64(varintSmallSize)/float64(len(smallValues)))
	t.Logf("  StreamVByte: %4d bytes (%.2f bytes/value) [ctrl=%d, data=%d]", svbSmallSize, float64(svbSmallSize)/float64(len(smallValues)), len(ctrlSmall), len(dataSmall))
	t.Logf("  Ratio: %.2fx", float64(varintSmallSize)/float64(svbSmallSize))
	t.Logf("")
	t.Logf("Large values (%d values):", len(largeValues))
	t.Logf("  Varint:      %4d bytes (%.2f bytes/value)", varintLargeSize, float64(varintLargeSize)/float64(len(largeValues)))
	t.Logf("  StreamVByte: %4d bytes (%.2f bytes/value) [ctrl=%d, data=%d]", svbLargeSize, float64(svbLargeSize)/float64(len(largeValues)), len(ctrlLarge), len(dataLarge))
	t.Logf("  Ratio: %.2fx", float64(varintLargeSize)/float64(svbLargeSize))
	t.Logf("")
	t.Logf("Columnar - small positions (20 locations × 5 fields = 100 values):")
	t.Logf("  Row-oriented SVB: %4d bytes", svbSmallSize)
	t.Logf("  Columnar SVB:     %4d bytes", columnarSize)
	t.Logf("  Columnar+Delta:   %4d bytes", columnarDeltaSize)
	t.Logf("  Delta savings: %.1f%%", 100*(1-float64(columnarDeltaSize)/float64(columnarSize)))
	t.Logf("")
	t.Logf("Columnar - LARGE positions (positions up to 47500, ~2500 apart):")
	t.Logf("  Varint row:       %4d bytes", varintRowLargeSize)
	t.Logf("  Row-oriented SVB: %4d bytes", rowLargeSize)
	t.Logf("  Columnar SVB:     %4d bytes", columnarLargeSize)
	t.Logf("  Columnar+Delta:   %4d bytes  <-- WINNER", columnarDeltaLargeSize)
	t.Logf("  Delta vs Columnar: %.1f%% smaller", 100*(1-float64(columnarDeltaLargeSize)/float64(columnarLargeSize)))
	t.Logf("  Delta vs Varint:   %.1f%% smaller", 100*(1-float64(columnarDeltaLargeSize)/float64(varintRowLargeSize)))
}

// BenchmarkWithDiskIO models total time including disk I/O
// Assumes NVMe SSD: ~3GB/s sequential read/write
func BenchmarkWithDiskIO(b *testing.B) {
	// Disk speed assumptions (bytes per nanosecond)
	// NVMe SSD: ~3 GB/s = 3 bytes/ns
	// SATA SSD: ~500 MB/s = 0.5 bytes/ns
	const nvmeBytesPerNs = 3.0
	const sataBytesPerNs = 0.5

	srcValues := generateMergeLocationData()

	// Varint
	varintEncoded := make([]byte, len(srcValues)*5)
	offset := 0
	for _, v := range srcValues {
		n := binary.PutUvarint(varintEncoded[offset:], uint64(v))
		offset += n
	}
	varintEncoded = varintEncoded[:offset]
	varintSize := len(varintEncoded)

	// StreamVByte
	svbCtrl, svbData := varint.EncodeStreamVByte32(srcValues)
	svbSize := len(svbCtrl) + len(svbData)

	b.Run("Varint_NVMe", func(b *testing.B) {
		decoded := make([]uint32, len(srcValues))
		dstEncoded := make([]byte, len(srcValues)*5)

		// Model: read time + decode time + encode time + write time
		readTime := float64(varintSize) / nvmeBytesPerNs
		writeTime := float64(varintSize) / nvmeBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Decode
			readOffset := 0
			for j := 0; j < len(decoded); j++ {
				v, n := binary.Uvarint(varintEncoded[readOffset:])
				decoded[j] = uint32(v)
				readOffset += n
			}
			// Encode
			writeOffset := 0
			for _, v := range decoded {
				n := binary.PutUvarint(dstEncoded[writeOffset:], uint64(v))
				writeOffset += n
			}
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(varintSize), "bytes")
	})

	b.Run("StreamVByte_NVMe", func(b *testing.B) {
		decoded := make([]uint32, len(srcValues))
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 500)

		readTime := float64(svbSize) / nvmeBytesPerNs
		writeTime := float64(svbSize) / nvmeBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(svbCtrl, svbData, decoded)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decoded, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(svbSize), "bytes")
	})

	b.Run("Varint_SATA", func(b *testing.B) {
		decoded := make([]uint32, len(srcValues))
		dstEncoded := make([]byte, len(srcValues)*5)

		readTime := float64(varintSize) / sataBytesPerNs
		writeTime := float64(varintSize) / sataBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			readOffset := 0
			for j := 0; j < len(decoded); j++ {
				v, n := binary.Uvarint(varintEncoded[readOffset:])
				decoded[j] = uint32(v)
				readOffset += n
			}
			writeOffset := 0
			for _, v := range decoded {
				n := binary.PutUvarint(dstEncoded[writeOffset:], uint64(v))
				writeOffset += n
			}
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(varintSize), "bytes")
	})

	b.Run("StreamVByte_SATA", func(b *testing.B) {
		decoded := make([]uint32, len(srcValues))
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 500)

		readTime := float64(svbSize) / sataBytesPerNs
		writeTime := float64(svbSize) / sataBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(svbCtrl, svbData, decoded)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decoded, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(svbSize), "bytes")
	})

	// Large values
	largeValues := generateLargeValueData()

	// Varint large
	varintLarge := make([]byte, len(largeValues)*5)
	offset = 0
	for _, v := range largeValues {
		n := binary.PutUvarint(varintLarge[offset:], uint64(v))
		offset += n
	}
	varintLarge = varintLarge[:offset]
	varintLargeSize := len(varintLarge)

	// StreamVByte large
	svbCtrlLarge, svbDataLarge := varint.EncodeStreamVByte32(largeValues)
	svbLargeSize := len(svbCtrlLarge) + len(svbDataLarge)

	b.Run("Varint_Large_NVMe", func(b *testing.B) {
		decoded := make([]uint32, len(largeValues))
		dstEncoded := make([]byte, len(largeValues)*5)

		readTime := float64(varintLargeSize) / nvmeBytesPerNs
		writeTime := float64(varintLargeSize) / nvmeBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			readOffset := 0
			for j := 0; j < len(decoded); j++ {
				v, n := binary.Uvarint(varintLarge[readOffset:])
				decoded[j] = uint32(v)
				readOffset += n
			}
			writeOffset := 0
			for _, v := range decoded {
				n := binary.PutUvarint(dstEncoded[writeOffset:], uint64(v))
				writeOffset += n
			}
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(varintLargeSize), "bytes")
	})

	b.Run("StreamVByte_Large_NVMe", func(b *testing.B) {
		decoded := make([]uint32, len(largeValues))
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 500)

		readTime := float64(svbLargeSize) / nvmeBytesPerNs
		writeTime := float64(svbLargeSize) / nvmeBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(svbCtrlLarge, svbDataLarge, decoded)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decoded, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(svbLargeSize), "bytes")
	})

	b.Run("Varint_Large_SATA", func(b *testing.B) {
		decoded := make([]uint32, len(largeValues))
		dstEncoded := make([]byte, len(largeValues)*5)

		readTime := float64(varintLargeSize) / sataBytesPerNs
		writeTime := float64(varintLargeSize) / sataBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			readOffset := 0
			for j := 0; j < len(decoded); j++ {
				v, n := binary.Uvarint(varintLarge[readOffset:])
				decoded[j] = uint32(v)
				readOffset += n
			}
			writeOffset := 0
			for _, v := range decoded {
				n := binary.PutUvarint(dstEncoded[writeOffset:], uint64(v))
				writeOffset += n
			}
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(varintLargeSize), "bytes")
	})

	b.Run("StreamVByte_Large_SATA", func(b *testing.B) {
		decoded := make([]uint32, len(largeValues))
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 500)

		readTime := float64(svbLargeSize) / sataBytesPerNs
		writeTime := float64(svbLargeSize) / sataBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(svbCtrlLarge, svbDataLarge, decoded)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decoded, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(svbLargeSize), "bytes")
	})

	// Columnar encoding (20 locations × 5 fields)
	numLocs := 20
	fieldIDs := make([]uint32, numLocs)
	positions := make([]uint32, numLocs)
	starts := make([]uint32, numLocs)
	ends := make([]uint32, numLocs)
	numAPs := make([]uint32, numLocs)

	for i := 0; i < numLocs; i++ {
		fieldIDs[i] = uint32(i % 3)
		positions[i] = uint32(i * 5)
		starts[i] = uint32(i * 10)
		ends[i] = uint32(i*10 + 5)
		numAPs[i] = 0
	}

	// Encode columnar
	ctrlFieldIDs, dataFieldIDs := varint.EncodeStreamVByte32(fieldIDs)
	ctrlPositions, dataPositions := varint.EncodeStreamVByte32(positions)
	ctrlStarts, dataStarts := varint.EncodeStreamVByte32(starts)
	ctrlEnds, dataEnds := varint.EncodeStreamVByte32(ends)
	ctrlNumAPs, dataNumAPs := varint.EncodeStreamVByte32(numAPs)

	columnarSize := len(ctrlFieldIDs) + len(dataFieldIDs) +
		len(ctrlPositions) + len(dataPositions) +
		len(ctrlStarts) + len(dataStarts) +
		len(ctrlEnds) + len(dataEnds) +
		len(ctrlNumAPs) + len(dataNumAPs)

	b.Run("Columnar_NVMe", func(b *testing.B) {
		decFieldIDs := make([]uint32, numLocs)
		decPositions := make([]uint32, numLocs)
		decStarts := make([]uint32, numLocs)
		decEnds := make([]uint32, numLocs)
		decNumAPs := make([]uint32, numLocs)
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 128)

		readTime := float64(columnarSize) / nvmeBytesPerNs
		writeTime := float64(columnarSize) / nvmeBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(ctrlFieldIDs, dataFieldIDs, decFieldIDs)
			varint.DecodeStreamVByte32Into(ctrlPositions, dataPositions, decPositions)
			varint.DecodeStreamVByte32Into(ctrlStarts, dataStarts, decStarts)
			varint.DecodeStreamVByte32Into(ctrlEnds, dataEnds, decEnds)
			varint.DecodeStreamVByte32Into(ctrlNumAPs, dataNumAPs, decNumAPs)

			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decFieldIDs, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decPositions, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decStarts, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decEnds, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decNumAPs, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(columnarSize), "bytes")
	})

	b.Run("Columnar_SATA", func(b *testing.B) {
		decFieldIDs := make([]uint32, numLocs)
		decPositions := make([]uint32, numLocs)
		decStarts := make([]uint32, numLocs)
		decEnds := make([]uint32, numLocs)
		decNumAPs := make([]uint32, numLocs)
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 128)

		readTime := float64(columnarSize) / sataBytesPerNs
		writeTime := float64(columnarSize) / sataBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(ctrlFieldIDs, dataFieldIDs, decFieldIDs)
			varint.DecodeStreamVByte32Into(ctrlPositions, dataPositions, decPositions)
			varint.DecodeStreamVByte32Into(ctrlStarts, dataStarts, decStarts)
			varint.DecodeStreamVByte32Into(ctrlEnds, dataEnds, decEnds)
			varint.DecodeStreamVByte32Into(ctrlNumAPs, dataNumAPs, decNumAPs)

			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decFieldIDs, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decPositions, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decStarts, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decEnds, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decNumAPs, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(columnarSize), "bytes")
	})

	// Columnar with delta encoding
	posDelta := make([]uint32, numLocs)
	startDelta := make([]uint32, numLocs)
	endLengths := make([]uint32, numLocs) // end - start = length

	for i := 0; i < numLocs; i++ {
		if i == 0 {
			posDelta[i] = positions[i]
			startDelta[i] = starts[i]
		} else {
			posDelta[i] = positions[i] - positions[i-1]
			startDelta[i] = starts[i] - starts[i-1]
		}
		endLengths[i] = ends[i] - starts[i]
	}

	ctrlFieldIDsDelta, dataFieldIDsDelta := varint.EncodeStreamVByte32(fieldIDs)
	ctrlPosDelta, dataPosDelta := varint.EncodeStreamVByte32(posDelta)
	ctrlStartDelta, dataStartDelta := varint.EncodeStreamVByte32(startDelta)
	ctrlEndLengths, dataEndLengths := varint.EncodeStreamVByte32(endLengths)
	ctrlNumAPsDelta, dataNumAPsDelta := varint.EncodeStreamVByte32(numAPs)

	columnarDeltaSize := len(ctrlFieldIDsDelta) + len(dataFieldIDsDelta) +
		len(ctrlPosDelta) + len(dataPosDelta) +
		len(ctrlStartDelta) + len(dataStartDelta) +
		len(ctrlEndLengths) + len(dataEndLengths) +
		len(ctrlNumAPsDelta) + len(dataNumAPsDelta)

	b.Run("ColumnarDelta_NVMe", func(b *testing.B) {
		decFieldIDs := make([]uint32, numLocs)
		decPosDelta := make([]uint32, numLocs)
		decStartDelta := make([]uint32, numLocs)
		decEndLengths := make([]uint32, numLocs)
		decNumAPs := make([]uint32, numLocs)
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 128)

		readTime := float64(columnarDeltaSize) / nvmeBytesPerNs
		writeTime := float64(columnarDeltaSize) / nvmeBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(ctrlFieldIDsDelta, dataFieldIDsDelta, decFieldIDs)
			varint.DecodeStreamVByte32Into(ctrlPosDelta, dataPosDelta, decPosDelta)
			varint.DecodeStreamVByte32Into(ctrlStartDelta, dataStartDelta, decStartDelta)
			varint.DecodeStreamVByte32Into(ctrlEndLengths, dataEndLengths, decEndLengths)
			varint.DecodeStreamVByte32Into(ctrlNumAPsDelta, dataNumAPsDelta, decNumAPs)

			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decFieldIDs, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decPosDelta, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decStartDelta, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decEndLengths, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decNumAPs, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(columnarDeltaSize), "bytes")
	})

	b.Run("ColumnarDelta_SATA", func(b *testing.B) {
		decFieldIDs := make([]uint32, numLocs)
		decPosDelta := make([]uint32, numLocs)
		decStartDelta := make([]uint32, numLocs)
		decEndLengths := make([]uint32, numLocs)
		decNumAPs := make([]uint32, numLocs)
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 128)

		readTime := float64(columnarDeltaSize) / sataBytesPerNs
		writeTime := float64(columnarDeltaSize) / sataBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(ctrlFieldIDsDelta, dataFieldIDsDelta, decFieldIDs)
			varint.DecodeStreamVByte32Into(ctrlPosDelta, dataPosDelta, decPosDelta)
			varint.DecodeStreamVByte32Into(ctrlStartDelta, dataStartDelta, decStartDelta)
			varint.DecodeStreamVByte32Into(ctrlEndLengths, dataEndLengths, decEndLengths)
			varint.DecodeStreamVByte32Into(ctrlNumAPsDelta, dataNumAPsDelta, decNumAPs)

			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decFieldIDs, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decPosDelta, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decStartDelta, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decEndLengths, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decNumAPs, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(columnarDeltaSize), "bytes")
	})

	// Large positions (simulating document with terms spread across 50KB)
	positionsLarge := make([]uint32, numLocs)
	startsLarge := make([]uint32, numLocs)
	endsLarge := make([]uint32, numLocs)

	for i := 0; i < numLocs; i++ {
		positionsLarge[i] = uint32(i * 2500)
		startsLarge[i] = uint32(i * 2500)
		endsLarge[i] = uint32(i*2500 + 50)
	}

	// Row-oriented for large positions
	rowLargeValues := make([]uint32, 0, 101)
	rowLargeValues = append(rowLargeValues, 100)
	for i := 0; i < numLocs; i++ {
		rowLargeValues = append(rowLargeValues, fieldIDs[i], positionsLarge[i], startsLarge[i], endsLarge[i], numAPs[i])
	}

	// Varint for large positions
	varintRowLarge := make([]byte, len(rowLargeValues)*5)
	voffset := 0
	for _, v := range rowLargeValues {
		n := binary.PutUvarint(varintRowLarge[voffset:], uint64(v))
		voffset += n
	}
	varintRowLarge = varintRowLarge[:voffset]
	varintRowLargeSize := voffset

	// StreamVByte row for large positions
	ctrlRowLarge, dataRowLarge := varint.EncodeStreamVByte32(rowLargeValues)
	rowLargeSize := len(ctrlRowLarge) + len(dataRowLarge)

	// Columnar large positions
	ctrlPosLarge, dataPosLarge := varint.EncodeStreamVByte32(positionsLarge)
	ctrlStartLarge, dataStartLarge := varint.EncodeStreamVByte32(startsLarge)
	ctrlEndLarge, dataEndLarge := varint.EncodeStreamVByte32(endsLarge)
	columnarLargePosSize := len(ctrlFieldIDs) + len(dataFieldIDs) +
		len(ctrlPosLarge) + len(dataPosLarge) +
		len(ctrlStartLarge) + len(dataStartLarge) +
		len(ctrlEndLarge) + len(dataEndLarge) +
		len(ctrlNumAPs) + len(dataNumAPs)

	// Columnar delta for large positions
	posDeltaLarge := make([]uint32, numLocs)
	startDeltaLarge := make([]uint32, numLocs)
	endLengthsLarge := make([]uint32, numLocs)

	for i := 0; i < numLocs; i++ {
		if i == 0 {
			posDeltaLarge[i] = positionsLarge[i]
			startDeltaLarge[i] = startsLarge[i]
		} else {
			posDeltaLarge[i] = positionsLarge[i] - positionsLarge[i-1]
			startDeltaLarge[i] = startsLarge[i] - startsLarge[i-1]
		}
		endLengthsLarge[i] = endsLarge[i] - startsLarge[i]
	}

	ctrlPosDeltaLarge, dataPosDeltaLarge := varint.EncodeStreamVByte32(posDeltaLarge)
	ctrlStartDeltaLarge, dataStartDeltaLarge := varint.EncodeStreamVByte32(startDeltaLarge)
	ctrlEndLengthsLarge, dataEndLengthsLarge := varint.EncodeStreamVByte32(endLengthsLarge)
	columnarDeltaLargePosSize := len(ctrlFieldIDs) + len(dataFieldIDs) +
		len(ctrlPosDeltaLarge) + len(dataPosDeltaLarge) +
		len(ctrlStartDeltaLarge) + len(dataStartDeltaLarge) +
		len(ctrlEndLengthsLarge) + len(dataEndLengthsLarge) +
		len(ctrlNumAPs) + len(dataNumAPs)

	b.Run("Varint_LargePos_NVMe", func(b *testing.B) {
		decoded := make([]uint32, len(rowLargeValues))
		dstEncoded := make([]byte, len(rowLargeValues)*5)

		readTime := float64(varintRowLargeSize) / nvmeBytesPerNs
		writeTime := float64(varintRowLargeSize) / nvmeBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			readOffset := 0
			for j := 0; j < len(decoded); j++ {
				v, n := binary.Uvarint(varintRowLarge[readOffset:])
				decoded[j] = uint32(v)
				readOffset += n
			}
			writeOffset := 0
			for _, v := range decoded {
				n := binary.PutUvarint(dstEncoded[writeOffset:], uint64(v))
				writeOffset += n
			}
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(varintRowLargeSize), "bytes")
	})

	b.Run("StreamVByte_LargePos_NVMe", func(b *testing.B) {
		decoded := make([]uint32, len(rowLargeValues))
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 500)

		readTime := float64(rowLargeSize) / nvmeBytesPerNs
		writeTime := float64(rowLargeSize) / nvmeBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(ctrlRowLarge, dataRowLarge, decoded)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decoded, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(rowLargeSize), "bytes")
	})

	b.Run("Columnar_LargePos_NVMe", func(b *testing.B) {
		decFieldIDs := make([]uint32, numLocs)
		decPositions := make([]uint32, numLocs)
		decStarts := make([]uint32, numLocs)
		decEnds := make([]uint32, numLocs)
		decNumAPs := make([]uint32, numLocs)
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 256)

		readTime := float64(columnarLargePosSize) / nvmeBytesPerNs
		writeTime := float64(columnarLargePosSize) / nvmeBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(ctrlFieldIDs, dataFieldIDs, decFieldIDs)
			varint.DecodeStreamVByte32Into(ctrlPosLarge, dataPosLarge, decPositions)
			varint.DecodeStreamVByte32Into(ctrlStartLarge, dataStartLarge, decStarts)
			varint.DecodeStreamVByte32Into(ctrlEndLarge, dataEndLarge, decEnds)
			varint.DecodeStreamVByte32Into(ctrlNumAPs, dataNumAPs, decNumAPs)

			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decFieldIDs, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decPositions, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decStarts, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decEnds, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decNumAPs, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(columnarLargePosSize), "bytes")
	})

	b.Run("ColumnarDelta_LargePos_NVMe", func(b *testing.B) {
		decFieldIDs := make([]uint32, numLocs)
		decPosDelta := make([]uint32, numLocs)
		decStartDelta := make([]uint32, numLocs)
		decEndLengths := make([]uint32, numLocs)
		decNumAPs := make([]uint32, numLocs)
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 256)

		readTime := float64(columnarDeltaLargePosSize) / nvmeBytesPerNs
		writeTime := float64(columnarDeltaLargePosSize) / nvmeBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(ctrlFieldIDs, dataFieldIDs, decFieldIDs)
			varint.DecodeStreamVByte32Into(ctrlPosDeltaLarge, dataPosDeltaLarge, decPosDelta)
			varint.DecodeStreamVByte32Into(ctrlStartDeltaLarge, dataStartDeltaLarge, decStartDelta)
			varint.DecodeStreamVByte32Into(ctrlEndLengthsLarge, dataEndLengthsLarge, decEndLengths)
			varint.DecodeStreamVByte32Into(ctrlNumAPs, dataNumAPs, decNumAPs)

			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decFieldIDs, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decPosDelta, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decStartDelta, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decEndLengths, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decNumAPs, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(columnarDeltaLargePosSize), "bytes")
	})

	b.Run("ColumnarDelta_LargePos_SATA", func(b *testing.B) {
		decFieldIDs := make([]uint32, numLocs)
		decPosDelta := make([]uint32, numLocs)
		decStartDelta := make([]uint32, numLocs)
		decEndLengths := make([]uint32, numLocs)
		decNumAPs := make([]uint32, numLocs)
		ctrlBuf := make([]byte, 0, 32)
		dataBuf := make([]byte, 0, 256)

		readTime := float64(columnarDeltaLargePosSize) / sataBytesPerNs
		writeTime := float64(columnarDeltaLargePosSize) / sataBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(ctrlFieldIDs, dataFieldIDs, decFieldIDs)
			varint.DecodeStreamVByte32Into(ctrlPosDeltaLarge, dataPosDeltaLarge, decPosDelta)
			varint.DecodeStreamVByte32Into(ctrlStartDeltaLarge, dataStartDeltaLarge, decStartDelta)
			varint.DecodeStreamVByte32Into(ctrlEndLengthsLarge, dataEndLengthsLarge, decEndLengths)
			varint.DecodeStreamVByte32Into(ctrlNumAPs, dataNumAPs, decNumAPs)

			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decFieldIDs, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decPosDelta, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decStartDelta, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decEndLengths, ctrlBuf, dataBuf)
			ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decNumAPs, ctrlBuf, dataBuf)
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(columnarDeltaLargePosSize), "bytes")
	})

	b.Run("Varint_LargePos_SATA", func(b *testing.B) {
		decoded := make([]uint32, len(rowLargeValues))
		dstEncoded := make([]byte, len(rowLargeValues)*5)

		readTime := float64(varintRowLargeSize) / sataBytesPerNs
		writeTime := float64(varintRowLargeSize) / sataBytesPerNs

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			readOffset := 0
			for j := 0; j < len(decoded); j++ {
				v, n := binary.Uvarint(varintRowLarge[readOffset:])
				decoded[j] = uint32(v)
				readOffset += n
			}
			writeOffset := 0
			for _, v := range decoded {
				n := binary.PutUvarint(dstEncoded[writeOffset:], uint64(v))
				writeOffset += n
			}
		}
		b.StopTimer()

		cpuTime := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		totalTime := cpuTime + readTime + writeTime
		b.ReportMetric(totalTime, "ns/op+io")
		b.ReportMetric(float64(varintRowLargeSize), "bytes")
	})
}

// BenchmarkMergeCycle_Columnar simulates the merge cycle with columnar StreamVByte
// This tests field-by-field encoding where similar values are grouped together
func BenchmarkMergeCycle_Columnar(b *testing.B) {
	// Generate columnar data - 20 locations with 5 fields each
	numLocs := 20
	fieldIDs := make([]uint32, numLocs)
	positions := make([]uint32, numLocs)
	starts := make([]uint32, numLocs)
	ends := make([]uint32, numLocs)
	numAPs := make([]uint32, numLocs)

	for i := 0; i < numLocs; i++ {
		fieldIDs[i] = uint32(i % 3)      // 0, 1, 2, 0, 1, 2, ...
		positions[i] = uint32(i * 5)     // 0, 5, 10, 15, ...
		starts[i] = uint32(i * 10)       // 0, 10, 20, 30, ...
		ends[i] = uint32(i*10 + 5)       // 5, 15, 25, 35, ...
		numAPs[i] = 0                    // always 0
	}

	// Encode each column separately
	ctrlFieldIDs, dataFieldIDs := varint.EncodeStreamVByte32(fieldIDs)
	ctrlPositions, dataPositions := varint.EncodeStreamVByte32(positions)
	ctrlStarts, dataStarts := varint.EncodeStreamVByte32(starts)
	ctrlEnds, dataEnds := varint.EncodeStreamVByte32(ends)
	ctrlNumAPs, dataNumAPs := varint.EncodeStreamVByte32(numAPs)

	// Decode buffers
	decFieldIDs := make([]uint32, numLocs)
	decPositions := make([]uint32, numLocs)
	decStarts := make([]uint32, numLocs)
	decEnds := make([]uint32, numLocs)
	decNumAPs := make([]uint32, numLocs)

	// Encode buffers
	ctrlBuf := make([]byte, 0, 32)
	dataBuf := make([]byte, 0, 128)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Decode all columns
		varint.DecodeStreamVByte32Into(ctrlFieldIDs, dataFieldIDs, decFieldIDs)
		varint.DecodeStreamVByte32Into(ctrlPositions, dataPositions, decPositions)
		varint.DecodeStreamVByte32Into(ctrlStarts, dataStarts, decStarts)
		varint.DecodeStreamVByte32Into(ctrlEnds, dataEnds, decEnds)
		varint.DecodeStreamVByte32Into(ctrlNumAPs, dataNumAPs, decNumAPs)

		// Re-encode all columns
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decFieldIDs, ctrlBuf, dataBuf)
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decPositions, ctrlBuf, dataBuf)
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decStarts, ctrlBuf, dataBuf)
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decEnds, ctrlBuf, dataBuf)
		ctrlBuf, dataBuf = varint.EncodeStreamVByte32Into(decNumAPs, ctrlBuf, dataBuf)
	}
}
