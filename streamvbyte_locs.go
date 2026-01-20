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
	"encoding/binary"
	"fmt"

	"github.com/ajroetker/go-highway/hwy/contrib/varint"
)

// StreamVByte format marker for location data
// This is used to identify StreamVByte-encoded location data vs legacy varint
const (
	LocFormatVarint     = 0x00 // Legacy varint encoding
	LocFormatStreamVByte = 0x01 // StreamVByte encoding
)

// streamVByteLocEncoder collects location values and encodes them using StreamVByte.
// Location data format (StreamVByte):
//   [format byte] [num_values varint] [control bytes] [data bytes]
//
// Each location consists of 5 fields + variable array positions:
//   fieldID, pos, start, end, numArrayPos, [arrayPos...]
type streamVByteLocEncoder struct {
	values []uint32
}

func newStreamVByteLocEncoder() *streamVByteLocEncoder {
	return &streamVByteLocEncoder{
		values: make([]uint32, 0, 64),
	}
}

func (e *streamVByteLocEncoder) Reset() {
	e.values = e.values[:0]
}

// AddLocation adds a single location's fields to the encoder
func (e *streamVByteLocEncoder) AddLocation(fieldID, pos, start, end uint64, arrayPositions []uint64) {
	e.values = append(e.values, uint32(fieldID), uint32(pos), uint32(start), uint32(end), uint32(len(arrayPositions)))
	for _, ap := range arrayPositions {
		e.values = append(e.values, uint32(ap))
	}
}

// Encode returns the StreamVByte-encoded location data
func (e *streamVByteLocEncoder) Encode() []byte {
	if len(e.values) == 0 {
		return []byte{LocFormatStreamVByte, 0} // format byte + 0 values
	}

	control, data := varint.EncodeStreamVByte32(e.values)

	// Calculate total size: format(1) + numValues(varint) + controlLen(varint) + control + data
	numValuesBytes := make([]byte, binary.MaxVarintLen64)
	n1 := binary.PutUvarint(numValuesBytes, uint64(len(e.values)))

	controlLenBytes := make([]byte, binary.MaxVarintLen64)
	n2 := binary.PutUvarint(controlLenBytes, uint64(len(control)))

	result := make([]byte, 0, 1+n1+n2+len(control)+len(data))
	result = append(result, LocFormatStreamVByte)
	result = append(result, numValuesBytes[:n1]...)
	result = append(result, controlLenBytes[:n2]...)
	result = append(result, control...)
	result = append(result, data...)

	return result
}

// streamVByteLocDecoder decodes StreamVByte-encoded location data
type streamVByteLocDecoder struct {
	values   []uint32
	pos      int
	numLocs  int
	format   byte
}

func newStreamVByteLocDecoder(data []byte) (*streamVByteLocDecoder, error) {
	if len(data) == 0 {
		return &streamVByteLocDecoder{}, nil
	}

	d := &streamVByteLocDecoder{
		format: data[0],
	}

	if d.format == LocFormatVarint {
		// Legacy format - caller should use varint decoder
		return d, nil
	}

	// StreamVByte format
	offset := 1

	// Read number of values
	numValues, n := binary.Uvarint(data[offset:])
	if n <= 0 {
		return nil, ErrInvalidStreamVByteData
	}
	offset += n

	// Read control length
	controlLen, n := binary.Uvarint(data[offset:])
	if n <= 0 {
		return nil, ErrInvalidStreamVByteData
	}
	offset += n

	// Extract control and data
	control := data[offset : offset+int(controlLen)]
	dataBytes := data[offset+int(controlLen):]

	// Decode all values at once using SIMD
	d.values = make([]uint32, numValues)
	varint.DecodeStreamVByte32Into(control, dataBytes, d.values)

	return d, nil
}

// IsStreamVByte returns true if the data is StreamVByte-encoded
func (d *streamVByteLocDecoder) IsStreamVByte() bool {
	return d.format == LocFormatStreamVByte
}

// ReadLocation reads the next location from the decoded values
// Returns: fieldID, pos, start, end, arrayPositions, ok
func (d *streamVByteLocDecoder) ReadLocation() (fieldID, pos, start, end uint64, arrayPositions []uint64, ok bool) {
	if d.pos+5 > len(d.values) {
		return 0, 0, 0, 0, nil, false
	}

	fieldID = uint64(d.values[d.pos])
	pos = uint64(d.values[d.pos+1])
	start = uint64(d.values[d.pos+2])
	end = uint64(d.values[d.pos+3])
	numArrayPos := int(d.values[d.pos+4])
	d.pos += 5

	if numArrayPos > 0 {
		if d.pos+numArrayPos > len(d.values) {
			return 0, 0, 0, 0, nil, false
		}
		arrayPositions = make([]uint64, numArrayPos)
		for i := 0; i < numArrayPos; i++ {
			arrayPositions[i] = uint64(d.values[d.pos+i])
		}
		d.pos += numArrayPos
	}

	return fieldID, pos, start, end, arrayPositions, true
}

// Reset resets the decoder position for re-reading
func (d *streamVByteLocDecoder) Reset() {
	d.pos = 0
}

// Remaining returns the number of uint32 values remaining to be read
func (d *streamVByteLocDecoder) Remaining() int {
	return len(d.values) - d.pos
}

var ErrInvalidStreamVByteData = fmt.Errorf("invalid StreamVByte location data")
