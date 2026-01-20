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
	"fmt"
	"io"

	"github.com/ajroetker/go-highway/hwy/contrib/varint"
)

// UseStreamVByte controls whether new segments use StreamVByte encoding
// for location data. Set to false to disable StreamVByte encoding.
var UseStreamVByte = true

// StreamVByte chunk format:
//   [format byte] [numValues varint] [controlLen varint] [control bytes] [data bytes]
//
// Format byte values:
const (
	ChunkFormatVarint      = 0x00 // Legacy varint encoding
	ChunkFormatStreamVByte = 0x01 // StreamVByte encoding
)

// streamVByteChunkedIntCoder encodes integers using StreamVByte within chunks.
// This is a drop-in replacement for chunkedIntCoder when UseStreamVByte is true.
type streamVByteChunkedIntCoder struct {
	final     []byte
	chunkSize uint64
	chunkBuf  bytes.Buffer
	chunkLens []uint64
	currChunk uint64

	// Current chunk's values (collected before encoding)
	chunkValues []uint32

	buf []byte

	bytesWritten uint64
}

// newStreamVByteChunkedIntCoder returns a new StreamVByte chunk int coder
func newStreamVByteChunkedIntCoder(chunkSize uint64, maxDocNum uint64) *streamVByteChunkedIntCoder {
	total := maxDocNum/chunkSize + 1
	return &streamVByteChunkedIntCoder{
		chunkSize:   chunkSize,
		chunkLens:   make([]uint64, total),
		final:       make([]byte, 0, 64),
		chunkValues: make([]uint32, 0, 256),
	}
}

// Reset resets the coder for reuse
func (c *streamVByteChunkedIntCoder) Reset() {
	c.final = c.final[:0]
	c.bytesWritten = 0
	c.chunkBuf.Reset()
	c.currChunk = 0
	c.chunkValues = c.chunkValues[:0]
	for i := range c.chunkLens {
		c.chunkLens[i] = 0
	}
}

// SetChunkSize changes the chunk size
func (c *streamVByteChunkedIntCoder) SetChunkSize(chunkSize uint64, maxDocNum uint64) {
	total := int(maxDocNum/chunkSize + 1)
	c.chunkSize = chunkSize
	if cap(c.chunkLens) < total {
		c.chunkLens = make([]uint64, total)
	} else {
		c.chunkLens = c.chunkLens[:total]
	}
}

func (c *streamVByteChunkedIntCoder) incrementBytesWritten(val uint64) {
	c.bytesWritten += val
}

func (c *streamVByteChunkedIntCoder) getBytesWritten() uint64 {
	return c.bytesWritten
}

// Add encodes the provided integers into the correct chunk
func (c *streamVByteChunkedIntCoder) Add(docNum uint64, vals ...uint64) error {
	chunk := docNum / c.chunkSize
	if chunk != c.currChunk {
		// Starting a new chunk - encode and flush the current one
		c.Close()
		c.chunkValues = c.chunkValues[:0]
		c.currChunk = chunk
	}

	// Collect values for StreamVByte encoding
	for _, val := range vals {
		c.chunkValues = append(c.chunkValues, uint32(val))
	}

	return nil
}

// AddBytes adds raw bytes to the current chunk (not StreamVByte encoded)
func (c *streamVByteChunkedIntCoder) AddBytes(docNum uint64, buf []byte) error {
	chunk := docNum / c.chunkSize
	if chunk != c.currChunk {
		c.Close()
		c.chunkValues = c.chunkValues[:0]
		c.currChunk = chunk
	}

	// For raw bytes, we fall back to direct storage
	// This is used in some edge cases
	_, err := c.chunkBuf.Write(buf)
	return err
}

// Close encodes and flushes the current chunk
func (c *streamVByteChunkedIntCoder) Close() {
	if len(c.chunkValues) == 0 && c.chunkBuf.Len() == 0 {
		c.chunkLens[c.currChunk] = 0
		return
	}

	// If we have raw bytes in chunkBuf (from AddBytes), use them directly
	if c.chunkBuf.Len() > 0 && len(c.chunkValues) == 0 {
		encodingBytes := c.chunkBuf.Bytes()
		c.incrementBytesWritten(uint64(len(encodingBytes)))
		c.chunkLens[c.currChunk] = uint64(len(encodingBytes))
		c.final = append(c.final, encodingBytes...)
		c.chunkBuf.Reset()
		c.currChunk = uint64(cap(c.chunkLens))
		return
	}

	// Encode values using StreamVByte
	control, data := varint.EncodeStreamVByte32(c.chunkValues)

	// Build chunk: [format] [numValues] [controlLen] [control] [data]
	var buf bytes.Buffer

	// Format byte
	buf.WriteByte(ChunkFormatStreamVByte)

	// Number of values
	numBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(numBuf, uint64(len(c.chunkValues)))
	buf.Write(numBuf[:n])

	// Control length
	n = binary.PutUvarint(numBuf, uint64(len(control)))
	buf.Write(numBuf[:n])

	// Control and data bytes
	buf.Write(control)
	buf.Write(data)

	encodingBytes := buf.Bytes()
	c.incrementBytesWritten(uint64(len(encodingBytes)))
	c.chunkLens[c.currChunk] = uint64(len(encodingBytes))
	c.final = append(c.final, encodingBytes...)
	c.chunkBuf.Reset()
	c.currChunk = uint64(cap(c.chunkLens))
}

// Write commits all encoded chunks to the provided writer
func (c *streamVByteChunkedIntCoder) Write(w io.Writer) (int, error) {
	bufNeeded := binary.MaxVarintLen64 * (1 + len(c.chunkLens))
	if len(c.buf) < bufNeeded {
		c.buf = make([]byte, bufNeeded)
	}
	buf := c.buf

	// Convert chunk lengths to offsets
	chunkOffsets := modifyLengthsToEndOffsets(c.chunkLens)

	// Write number of chunks and each offset
	n := binary.PutUvarint(buf, uint64(len(chunkOffsets)))
	for _, chunkOffset := range chunkOffsets {
		n += binary.PutUvarint(buf[n:], chunkOffset)
	}

	tw, err := w.Write(buf[:n])
	if err != nil {
		return tw, err
	}

	// Write data
	nw, err := w.Write(c.final)
	tw += nw
	return tw, err
}

// writeAt commits all encoded chunks and returns start offset
func (c *streamVByteChunkedIntCoder) writeAt(w io.Writer) (uint64, int, error) {
	startOffset := uint64(termNotEncoded)
	if len(c.final) <= 0 {
		return startOffset, 0, nil
	}

	if chw := w.(*CountHashWriter); chw != nil {
		startOffset = uint64(chw.Count())
	}

	tw, err := c.Write(w)
	return startOffset, tw, err
}

// FinalSize returns the size of the final encoded data
func (c *streamVByteChunkedIntCoder) FinalSize() int {
	return len(c.final)
}

// =============================================================================
// StreamVByte Chunked Int Decoder
// =============================================================================

// streamVByteChunkedIntDecoder decodes StreamVByte-encoded chunks
type streamVByteChunkedIntDecoder struct {
	startOffset     uint64
	dataStartOffset uint64
	chunkOffsets    []uint64
	curChunkBytes   []byte
	data            []byte

	// Decoded values from current chunk
	values []uint32
	pos    int
	format byte

	// For StreamVByte byte position tracking (enables copying optimization)
	headerLen  int    // Length of format + numValues + controlLen prefix
	controlLen int    // Length of control bytes
	control    []byte // Control bytes (for computing byte positions)

	// Fallback varint reader for legacy chunks
	r *memUvarintReader

	bytesRead uint64
}

// newStreamVByteChunkedIntDecoder creates a new decoder
func newStreamVByteChunkedIntDecoder(buf []byte, offset uint64, rv *streamVByteChunkedIntDecoder) *streamVByteChunkedIntDecoder {
	if rv == nil {
		rv = &streamVByteChunkedIntDecoder{startOffset: offset, data: buf}
	} else {
		rv.startOffset = offset
		rv.data = buf
	}

	var n, numChunks uint64
	var read int
	if offset == termNotEncoded {
		numChunks = 0
	} else {
		numChunks, read = binary.Uvarint(buf[offset+n : offset+n+binary.MaxVarintLen64])
	}

	n += uint64(read)
	if cap(rv.chunkOffsets) >= int(numChunks) {
		rv.chunkOffsets = rv.chunkOffsets[:int(numChunks)]
	} else {
		rv.chunkOffsets = make([]uint64, int(numChunks))
	}
	for i := 0; i < int(numChunks); i++ {
		rv.chunkOffsets[i], read = binary.Uvarint(buf[offset+n : offset+n+binary.MaxVarintLen64])
		n += uint64(read)
	}
	rv.bytesRead += n
	rv.dataStartOffset = offset + n
	return rv
}

func (d *streamVByteChunkedIntDecoder) getBytesRead() uint64 {
	return d.bytesRead
}

func (d *streamVByteChunkedIntDecoder) loadChunk(chunk int) error {
	if d.startOffset == termNotEncoded {
		d.values = d.values[:0]
		d.pos = 0
		return nil
	}

	if chunk >= len(d.chunkOffsets) {
		return fmt.Errorf("tried to load chunk that doesn't exist %d/(%d)",
			chunk, len(d.chunkOffsets))
	}

	end, start := d.dataStartOffset, d.dataStartOffset
	s, e := readChunkBoundary(chunk, d.chunkOffsets)
	start += s
	end += e
	d.curChunkBytes = d.data[start:end]
	d.bytesRead += uint64(len(d.curChunkBytes))

	if len(d.curChunkBytes) == 0 {
		d.values = d.values[:0]
		d.pos = 0
		return nil
	}

	// Check format byte
	d.format = d.curChunkBytes[0]

	if d.format == ChunkFormatStreamVByte {
		// StreamVByte format
		offset := 1

		// Read number of values
		numValues, n := binary.Uvarint(d.curChunkBytes[offset:])
		if n <= 0 {
			return fmt.Errorf("invalid StreamVByte chunk: can't read numValues")
		}
		offset += n

		// Read control length
		controlLen, n := binary.Uvarint(d.curChunkBytes[offset:])
		if n <= 0 {
			return fmt.Errorf("invalid StreamVByte chunk: can't read controlLen")
		}
		offset += n

		// Save header info for byte position tracking
		d.headerLen = offset
		d.controlLen = int(controlLen)
		d.control = d.curChunkBytes[offset : offset+int(controlLen)]

		// Extract data bytes
		dataBytes := d.curChunkBytes[offset+int(controlLen):]

		// Decode all values at once using SIMD
		if cap(d.values) >= int(numValues) {
			d.values = d.values[:numValues]
		} else {
			d.values = make([]uint32, numValues)
		}
		varint.DecodeStreamVByte32Into(d.control, dataBytes, d.values)
		d.pos = 0
	} else {
		// Legacy varint format - use memUvarintReader
		if d.r == nil {
			d.r = newMemUvarintReader(d.curChunkBytes)
		} else {
			d.r.Reset(d.curChunkBytes)
		}
		d.values = d.values[:0]
		d.pos = 0
	}

	return nil
}

func (d *streamVByteChunkedIntDecoder) reset() {
	d.startOffset = 0
	d.dataStartOffset = 0
	d.chunkOffsets = d.chunkOffsets[:0]
	d.curChunkBytes = d.curChunkBytes[:0]
	d.bytesRead = 0
	d.data = d.data[:0]
	d.values = d.values[:0]
	d.pos = 0
	if d.r != nil {
		d.r.Reset([]byte(nil))
	}
}

func (d *streamVByteChunkedIntDecoder) isNil() bool {
	return d.curChunkBytes == nil || len(d.curChunkBytes) == 0
}

func (d *streamVByteChunkedIntDecoder) readUvarint() (uint64, error) {
	if d.format == ChunkFormatStreamVByte {
		if d.pos >= len(d.values) {
			return 0, io.EOF
		}
		val := d.values[d.pos]
		d.pos++
		return uint64(val), nil
	}
	// Fallback to legacy varint reader
	if d.r == nil {
		return 0, io.EOF
	}
	return d.r.ReadUvarint()
}

func (d *streamVByteChunkedIntDecoder) readBytes(start, end int) []byte {
	return d.curChunkBytes[start:end]
}

func (d *streamVByteChunkedIntDecoder) SkipUvarint() {
	if d.format == ChunkFormatStreamVByte {
		d.pos++
	} else if d.r != nil {
		d.r.SkipUvarint()
	}
}

func (d *streamVByteChunkedIntDecoder) SkipBytes(count int) {
	if d.format == ChunkFormatStreamVByte {
		// For StreamVByte, count is actually a VALUE count (not byte count)
		// because the writer stores value count for StreamVByte format
		d.pos += count
	} else if d.r != nil {
		d.r.SkipBytes(count)
	}
}

func (d *streamVByteChunkedIntDecoder) Len() int {
	if d.format == ChunkFormatStreamVByte {
		return len(d.values) - d.pos
	}
	if d.r == nil {
		return 0
	}
	return d.r.Len()
}

func (d *streamVByteChunkedIntDecoder) remainingLen() int {
	if d.format == ChunkFormatStreamVByte {
		// Compute byte position by walking control bytes
		return d.bytePositionForValue(d.pos)
	}
	if d.r == nil {
		return len(d.curChunkBytes)
	}
	return len(d.curChunkBytes) - d.r.Len()
}

// bytePositionForValue computes the byte offset in curChunkBytes for value index.
// This enables the byte-copying optimization for StreamVByte.
func (d *streamVByteChunkedIntDecoder) bytePositionForValue(valueIdx int) int {
	if d.format != ChunkFormatStreamVByte || len(d.control) == 0 {
		return 0
	}

	// Count control bytes consumed: each control byte handles 4 values
	controlBytesConsumed := valueIdx / 4

	// Count data bytes consumed by walking control bytes
	dataBytesConsumed := 0
	for i := 0; i < valueIdx; i++ {
		controlIdx := i / 4
		if controlIdx >= len(d.control) {
			break
		}
		shift := (i % 4) * 2
		size := int((d.control[controlIdx]>>shift)&0x03) + 1
		dataBytesConsumed += size
	}

	// Total position = header + control bytes consumed + data bytes consumed
	return d.headerLen + controlBytesConsumed + dataBytesConsumed
}
