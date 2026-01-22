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

	"github.com/ajroetker/go-highway/hwy/contrib/algo"
	"github.com/ajroetker/go-highway/hwy/contrib/varint"
)

// StreamVByte format marker for location data
// This is used to identify StreamVByte-encoded location data vs legacy varint
const (
	LocFormatVarint          = 0x00 // Legacy varint encoding
	LocFormatStreamVByte     = 0x01 // StreamVByte encoding (row-oriented)
	LocFormatColumnarDelta   = 0x02 // Columnar format with delta encoding for start/end
)

// UseColumnarLocations is defined in streamvbyte_coder.go

// streamVByteLocEncoder collects location values and encodes them using StreamVByte.
// Supports two formats:
// 1. Row-oriented (LocFormatStreamVByte): [fieldID, pos, start, end, numAP, arrayPos...]
// 2. Columnar with delta (LocFormatColumnarDelta): separate columns for each field type
//
// Columnar format enables delta encoding for start/end offsets which are monotonically increasing.
type streamVByteLocEncoder struct {
	// Row-oriented storage (legacy)
	values []uint32

	// Columnar storage (for delta encoding)
	fieldIDs       []uint32
	positions      []uint32
	starts         []uint32
	ends           []uint32
	numArrayPos    []uint32
	arrayPositions []uint32
}

func newStreamVByteLocEncoder() *streamVByteLocEncoder {
	return &streamVByteLocEncoder{
		values:         make([]uint32, 0, 64),
		fieldIDs:       make([]uint32, 0, 16),
		positions:      make([]uint32, 0, 16),
		starts:         make([]uint32, 0, 16),
		ends:           make([]uint32, 0, 16),
		numArrayPos:    make([]uint32, 0, 16),
		arrayPositions: make([]uint32, 0, 16),
	}
}

func (e *streamVByteLocEncoder) Reset() {
	e.values = e.values[:0]
	e.fieldIDs = e.fieldIDs[:0]
	e.positions = e.positions[:0]
	e.starts = e.starts[:0]
	e.ends = e.ends[:0]
	e.numArrayPos = e.numArrayPos[:0]
	e.arrayPositions = e.arrayPositions[:0]
}

// AddLocation adds a single location's fields to the encoder
func (e *streamVByteLocEncoder) AddLocation(fieldID, pos, start, end uint64, arrayPositions []uint64) {
	if UseColumnarLocations {
		// Columnar storage
		e.fieldIDs = append(e.fieldIDs, uint32(fieldID))
		e.positions = append(e.positions, uint32(pos))
		e.starts = append(e.starts, uint32(start))
		e.ends = append(e.ends, uint32(end))
		e.numArrayPos = append(e.numArrayPos, uint32(len(arrayPositions)))
		for _, ap := range arrayPositions {
			e.arrayPositions = append(e.arrayPositions, uint32(ap))
		}
	} else {
		// Row-oriented storage (legacy)
		e.values = append(e.values, uint32(fieldID), uint32(pos), uint32(start), uint32(end), uint32(len(arrayPositions)))
		for _, ap := range arrayPositions {
			e.values = append(e.values, uint32(ap))
		}
	}
}

// Encode returns the StreamVByte-encoded location data
func (e *streamVByteLocEncoder) Encode() []byte {
	if UseColumnarLocations {
		return e.encodeColumnar()
	}
	return e.encodeRowOriented()
}

// encodeRowOriented encodes using the legacy row-oriented format
func (e *streamVByteLocEncoder) encodeRowOriented() []byte {
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

// encodeColumnar encodes using columnar format with delta encoding for positions/starts
// and lengths (end-start) instead of delta-encoded ends for better compression.
// Format: [format][numLocs][numArrayPos][fieldIDs][posDelta][startDelta][lengths][numAPs][arrayPositions]
func (e *streamVByteLocEncoder) encodeColumnar() []byte {
	numLocs := len(e.fieldIDs)
	if numLocs == 0 {
		return []byte{LocFormatColumnarDelta, 0}
	}

	// Delta encode positions (monotonically increasing)
	posDeltas := make([]uint32, numLocs)
	copy(posDeltas, e.positions)
	algo.DeltaEncode(posDeltas, 0)

	// Delta encode starts (monotonically increasing byte offsets)
	startDeltas := make([]uint32, numLocs)
	copy(startDeltas, e.starts)
	algo.DeltaEncode(startDeltas, 0)

	// Store lengths (end - start) instead of delta-encoded ends
	// Lengths are typically small and constant, compressing very well
	lengths := make([]uint32, numLocs)
	for i := 0; i < numLocs; i++ {
		lengths[i] = e.ends[i] - e.starts[i]
	}

	// Encode each column
	fieldIDCtrl, fieldIDData := varint.EncodeStreamVByte32(e.fieldIDs)
	posCtrl, posData := varint.EncodeStreamVByte32(posDeltas)
	startCtrl, startData := varint.EncodeStreamVByte32(startDeltas)
	lengthCtrl, lengthData := varint.EncodeStreamVByte32(lengths)
	numAPCtrl, numAPData := varint.EncodeStreamVByte32(e.numArrayPos)

	var apCtrl, apData []byte
	if len(e.arrayPositions) > 0 {
		apCtrl, apData = varint.EncodeStreamVByte32(e.arrayPositions)
	}

	// Build result
	// Header: format + numLocs + numArrayPositions
	headerBuf := make([]byte, 1+binary.MaxVarintLen64*2)
	headerBuf[0] = LocFormatColumnarDelta
	n := 1
	n += binary.PutUvarint(headerBuf[n:], uint64(numLocs))
	n += binary.PutUvarint(headerBuf[n:], uint64(len(e.arrayPositions)))

	// Each column: [controlLen][control][data]
	estimatedSize := n +
		1 + len(fieldIDCtrl) + len(fieldIDData) +
		1 + len(posCtrl) + len(posData) +
		1 + len(startCtrl) + len(startData) +
		1 + len(lengthCtrl) + len(lengthData) +
		1 + len(numAPCtrl) + len(numAPData) +
		1 + len(apCtrl) + len(apData)

	result := make([]byte, 0, estimatedSize)
	result = append(result, headerBuf[:n]...)

	// Helper to append a column
	appendColumn := func(ctrl, data []byte) {
		lenBuf := make([]byte, binary.MaxVarintLen64)
		ln := binary.PutUvarint(lenBuf, uint64(len(ctrl)))
		result = append(result, lenBuf[:ln]...)
		result = append(result, ctrl...)
		result = append(result, data...)
	}

	appendColumn(fieldIDCtrl, fieldIDData)
	appendColumn(posCtrl, posData)
	appendColumn(startCtrl, startData)
	appendColumn(lengthCtrl, lengthData)
	appendColumn(numAPCtrl, numAPData)
	if len(e.arrayPositions) > 0 {
		appendColumn(apCtrl, apData)
	}

	return result
}

// streamVByteLocDecoder decodes StreamVByte-encoded location data
type streamVByteLocDecoder struct {
	// Row-oriented (legacy)
	values []uint32
	pos    int

	// Columnar format
	fieldIDs       []uint32
	positions      []uint32
	starts         []uint32
	ends           []uint32
	numArrayPos    []uint32
	arrayPositions []uint32
	locIdx         int
	apIdx          int

	numLocs int
	format  byte
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

	if d.format == LocFormatColumnarDelta {
		return d.decodeColumnar(data)
	}

	// Row-oriented StreamVByte format
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

// decodeColumnar decodes the columnar format with delta encoding
func (d *streamVByteLocDecoder) decodeColumnar(data []byte) (*streamVByteLocDecoder, error) {
	offset := 1

	// Read numLocs
	numLocs, n := binary.Uvarint(data[offset:])
	if n <= 0 {
		return nil, ErrInvalidStreamVByteData
	}
	offset += n
	d.numLocs = int(numLocs)

	if numLocs == 0 {
		return d, nil
	}

	// Read numArrayPositions
	numAP, n := binary.Uvarint(data[offset:])
	if n <= 0 {
		return nil, ErrInvalidStreamVByteData
	}
	offset += n

	// Helper to read a column
	readColumn := func(numValues int) ([]uint32, error) {
		ctrlLen, n := binary.Uvarint(data[offset:])
		if n <= 0 {
			return nil, ErrInvalidStreamVByteData
		}
		offset += n

		ctrl := data[offset : offset+int(ctrlLen)]
		offset += int(ctrlLen)

		// Calculate data length from control bytes
		dataLen := 0
		for i := 0; i < int(ctrlLen); i++ {
			for j := 0; j < 4 && i*4+j < numValues; j++ {
				size := int((ctrl[i]>>(j*2))&0x03) + 1
				dataLen += size
			}
		}

		dataBytes := data[offset : offset+dataLen]
		offset += dataLen

		result := make([]uint32, numValues)
		varint.DecodeStreamVByte32Into(ctrl, dataBytes, result)
		return result, nil
	}

	var err error

	// Read columns
	d.fieldIDs, err = readColumn(int(numLocs))
	if err != nil {
		return nil, err
	}

	posDeltas, err := readColumn(int(numLocs))
	if err != nil {
		return nil, err
	}

	startDeltas, err := readColumn(int(numLocs))
	if err != nil {
		return nil, err
	}

	lengths, err := readColumn(int(numLocs))
	if err != nil {
		return nil, err
	}

	d.numArrayPos, err = readColumn(int(numLocs))
	if err != nil {
		return nil, err
	}

	if numAP > 0 {
		d.arrayPositions, err = readColumn(int(numAP))
		if err != nil {
			return nil, err
		}
	}

	// Delta decode positions using SIMD-accelerated prefix sum
	d.positions = posDeltas // reuse slice
	algo.DeltaDecode(d.positions, 0)

	// Delta decode starts using SIMD-accelerated prefix sum
	d.starts = startDeltas // reuse slice
	algo.DeltaDecode(d.starts, 0)

	// Reconstruct ends from starts + lengths
	d.ends = make([]uint32, numLocs)
	for i := 0; i < int(numLocs); i++ {
		d.ends[i] = d.starts[i] + lengths[i]
	}

	return d, nil
}

// IsStreamVByte returns true if the data is StreamVByte-encoded (any format)
func (d *streamVByteLocDecoder) IsStreamVByte() bool {
	return d.format == LocFormatStreamVByte || d.format == LocFormatColumnarDelta
}

// ReadLocation reads the next location from the decoded values
// Returns: fieldID, pos, start, end, arrayPositions, ok
func (d *streamVByteLocDecoder) ReadLocation() (fieldID, pos, start, end uint64, arrayPositions []uint64, ok bool) {
	if d.format == LocFormatColumnarDelta {
		return d.readLocationColumnar()
	}
	return d.readLocationRowOriented()
}

func (d *streamVByteLocDecoder) readLocationRowOriented() (fieldID, pos, start, end uint64, arrayPositions []uint64, ok bool) {
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

func (d *streamVByteLocDecoder) readLocationColumnar() (fieldID, pos, start, end uint64, arrayPositions []uint64, ok bool) {
	if d.locIdx >= d.numLocs {
		return 0, 0, 0, 0, nil, false
	}

	fieldID = uint64(d.fieldIDs[d.locIdx])
	pos = uint64(d.positions[d.locIdx])
	start = uint64(d.starts[d.locIdx])
	end = uint64(d.ends[d.locIdx])
	numAP := int(d.numArrayPos[d.locIdx])
	d.locIdx++

	if numAP > 0 {
		if d.apIdx+numAP > len(d.arrayPositions) {
			return 0, 0, 0, 0, nil, false
		}
		arrayPositions = make([]uint64, numAP)
		for i := 0; i < numAP; i++ {
			arrayPositions[i] = uint64(d.arrayPositions[d.apIdx+i])
		}
		d.apIdx += numAP
	}

	return fieldID, pos, start, end, arrayPositions, true
}

// Reset resets the decoder position for re-reading
func (d *streamVByteLocDecoder) Reset() {
	d.pos = 0
	d.locIdx = 0
	d.apIdx = 0
}

// Remaining returns the number of uint32 values remaining to be read
func (d *streamVByteLocDecoder) Remaining() int {
	if d.format == LocFormatColumnarDelta {
		return d.numLocs - d.locIdx
	}
	return len(d.values) - d.pos
}

var ErrInvalidStreamVByteData = fmt.Errorf("invalid StreamVByte location data")
