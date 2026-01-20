//  Copyright (c) 2025 Couchbase, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 		http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package zap

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ajroetker/go-highway/hwy/contrib/varint"
)

// TestStreamVByteChunkedIntCoderRoundTrip tests encoding and decoding
func TestStreamVByteChunkedIntCoderRoundTrip(t *testing.T) {
	// Simulate location data: fieldID, pos, start, end, numArrayPos, [arrayPos...]
	testData := [][]uint64{
		{0, 1, 0, 5, 0},                     // Simple location, no array positions
		{0, 2, 5, 10, 2, 0, 1},              // Location with 2 array positions
		{1, 3, 10, 20, 3, 0, 1, 2},          // Different field, 3 array positions
		{0, 100, 500, 600, 1, 5},            // Larger offsets
		{2, 1000, 5000, 6000, 0},            // Even larger offsets
	}

	coder := newStreamVByteChunkedIntCoder(1024, 100)

	// Add all data as if from document 0
	for _, locData := range testData {
		err := coder.Add(0, locData...)
		if err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}
	coder.Close()

	// Write to buffer
	var buf bytes.Buffer
	_, err := coder.Write(&buf)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Build a buffer with the data at a non-zero offset
	// (offset=0 means "not encoded" in zapx)
	bufBytes := buf.Bytes()
	const testOffset = 8 // Use non-zero offset to avoid termNotEncoded sentinel
	fullBuf := make([]byte, testOffset+len(bufBytes))
	copy(fullBuf[testOffset:], bufBytes)

	// Create decoder with the non-zero offset
	decoder := newStreamVByteChunkedIntDecoder(fullBuf, testOffset, nil)

	err = decoder.loadChunk(0)
	if err != nil {
		t.Fatalf("loadChunk failed: %v", err)
	}

	// Verify format is StreamVByte
	if decoder.format != ChunkFormatStreamVByte {
		t.Fatalf("Expected StreamVByte format, got %d", decoder.format)
	}

	// Read back and verify
	for i, locData := range testData {
		for j, expected := range locData {
			got, err := decoder.readUvarint()
			if err != nil {
				t.Fatalf("readUvarint failed at loc %d, field %d: %v", i, j, err)
			}
			if got != expected {
				t.Errorf("Mismatch at loc %d, field %d: got %d, want %d", i, j, got, expected)
			}
		}
	}
}

// BenchmarkLocationDecode_Varint benchmarks legacy varint decoding
func BenchmarkLocationDecode_Varint(b *testing.B) {
	// Generate typical location data
	numLocs := 100
	locData := generateLocationData(numLocs)

	// Encode using legacy varint
	encoded := encodeLocationsVarint(locData)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		decodeLocationsVarint(encoded, numLocs)
	}
}

// BenchmarkLocationDecode_StreamVByte benchmarks StreamVByte decoding
func BenchmarkLocationDecode_StreamVByte(b *testing.B) {
	// Generate typical location data
	numLocs := 100
	locData := generateLocationData(numLocs)

	// Flatten to uint32 slice
	var values []uint32
	for _, loc := range locData {
		for _, v := range loc {
			values = append(values, uint32(v))
		}
	}

	// Encode using StreamVByte
	control, data := varint.EncodeStreamVByte32(values)

	// Pre-allocate decode buffer
	decoded := make([]uint32, len(values))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		varint.DecodeStreamVByte32Into(control, data, decoded)
	}
}

// BenchmarkLocationDecode_Comparison runs both for direct comparison
func BenchmarkLocationDecode_Comparison(b *testing.B) {
	numLocs := 100
	locData := generateLocationData(numLocs)

	b.Run("Varint", func(b *testing.B) {
		encoded := encodeLocationsVarint(locData)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			decodeLocationsVarint(encoded, numLocs)
		}
	})

	b.Run("StreamVByte", func(b *testing.B) {
		var values []uint32
		for _, loc := range locData {
			for _, v := range loc {
				values = append(values, uint32(v))
			}
		}
		control, data := varint.EncodeStreamVByte32(values)
		decoded := make([]uint32, len(values))

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			varint.DecodeStreamVByte32Into(control, data, decoded)
		}
	})
}

// generateLocationData generates realistic location data
// Each location: fieldID, pos, start, end, numArrayPos, [arrayPos...]
func generateLocationData(numLocs int) [][]uint64 {
	locData := make([][]uint64, numLocs)
	for i := 0; i < numLocs; i++ {
		// Simulate typical location patterns
		fieldID := uint64(i % 3)           // 0-2 fields
		pos := uint64(i + 1)               // Position 1-N
		start := uint64(i * 10)            // Start offset
		end := uint64(i*10 + 5 + i%5)      // End offset
		numArrayPos := i % 4               // 0-3 array positions

		loc := []uint64{fieldID, pos, start, end, uint64(numArrayPos)}
		for j := 0; j < numArrayPos; j++ {
			loc = append(loc, uint64(j))
		}
		locData[i] = loc
	}
	return locData
}

// encodeLocationsVarint encodes locations using legacy varint
func encodeLocationsVarint(locData [][]uint64) []byte {
	var buf bytes.Buffer
	tmp := make([]byte, binary.MaxVarintLen64)

	for _, loc := range locData {
		for _, v := range loc {
			n := binary.PutUvarint(tmp, v)
			buf.Write(tmp[:n])
		}
	}
	return buf.Bytes()
}

// decodeLocationsVarint decodes locations using legacy varint
func decodeLocationsVarint(data []byte, numLocs int) [][]uint64 {
	result := make([][]uint64, 0, numLocs)
	pos := 0

	for i := 0; i < numLocs && pos < len(data); i++ {
		// Read fixed fields
		fieldID, n := binary.Uvarint(data[pos:])
		pos += n
		p, n := binary.Uvarint(data[pos:])
		pos += n
		start, n := binary.Uvarint(data[pos:])
		pos += n
		end, n := binary.Uvarint(data[pos:])
		pos += n
		numArrayPos, n := binary.Uvarint(data[pos:])
		pos += n

		loc := []uint64{fieldID, p, start, end, numArrayPos}
		for j := 0; j < int(numArrayPos); j++ {
			ap, n := binary.Uvarint(data[pos:])
			pos += n
			loc = append(loc, ap)
		}
		result = append(result, loc)
	}
	return result
}
