//  Copyright (c) 2017 Couchbase, Inc.
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
	"io"

	"github.com/RoaringBitmap/roaring/v2"
	index "github.com/blevesearch/bleve_index_api"
)

// writes out the length of the roaring bitmap in bytes as varint
// then writes out the roaring bitmap itself
func writeRoaringWithLen(r *roaring.Bitmap, w io.Writer,
	reuseBufVarint []byte) (int, error) {
	var tw int

	// write out the length using the precomputed serialized size
	n := binary.PutUvarint(reuseBufVarint, r.GetSerializedSizeInBytes())
	nw, err := w.Write(reuseBufVarint[:n])
	tw += nw
	if err != nil {
		return tw, err
	}

	// write the roaring bytes directly to the writer
	nw64, err := r.WriteTo(w)
	tw += int(nw64)
	if err != nil {
		return tw, err
	}

	return tw, nil
}

func persistFieldsSection(fieldsInv []string, fieldsOptions map[string]index.FieldIndexingOptions, w *CountHashWriter, opaque map[int]resetable) (uint64, error) {
	var rv uint64
	fieldsOffsets := make([]uint64, 0, len(fieldsInv))

	for fieldID, fieldName := range fieldsInv {
		// record start of this field
		fieldsOffsets = append(fieldsOffsets, uint64(w.Count()))

		// write field name length
		_, err := writeUvarints(w, uint64(len(fieldName)))
		if err != nil {
			return 0, err
		}

		// write out the field name
		_, err = w.Write([]byte(fieldName))
		if err != nil {
			return 0, err
		}

		// write out the field options
		fieldOpts := fieldsOptions[fieldName]
		_, err = writeUvarints(w, uint64(fieldOpts))
		if err != nil {
			return 0, err
		}

		// write out the number of field-specific indexes
		_, err = writeUvarints(w, uint64(len(segmentSections)))
		if err != nil {
			return 0, err
		}

		// now write pairs of index section ids, and start addresses for each field
		// which has a specific section's data. this serves as the starting point
		// using which a field's section data can be read and parsed.
		for segmentSectionType, segmentSectionImpl := range segmentSections {
			binary.Write(w, binary.BigEndian, segmentSectionType)
			binary.Write(w, binary.BigEndian, uint64(segmentSectionImpl.AddrForField(opaque, fieldID)))
		}
	}

	rv = uint64(w.Count())
	// write out number of fields
	_, err := writeUvarints(w, uint64(len(fieldsInv)))
	if err != nil {
		return 0, err
	}
	// now write out the fields index
	for fieldID := range fieldsInv {
		err := binary.Write(w, binary.BigEndian, fieldsOffsets[fieldID])
		if err != nil {
			return 0, err
		}
	}

	return rv, nil
}

// FooterSize is the size of the footer record in bytes
// crc + id length + ver + chunk + sectionsIndexOffset + stored offset + num docs
// Does not include the length of the id because it is variable length
const FooterSize = 4 + 4 + 4 + 4 + 8 + 8 + 8

func persistFooter(numDocs, storedIndexOffset, sectionsIndexOffset uint64,
	chunkMode, crcBeforeFooter uint32, writerIn io.Writer) error {
	w := NewCountHashWriter(writerIn)
	w.crc = crcBeforeFooter

	// Pre-encode all footer fields into a single buffer to avoid
	// per-field binary.Write allocations (which use reflection).
	// Layout: idLen(4) + numDocs(8) + storedIndexOffset(8) +
	//         sectionsIndexOffset(8) + chunkMode(4) + version(4) + crc(4) = 40 bytes
	var buf [FooterSize]byte
	pos := 0

	// writer id length (unused, always 0)
	binary.BigEndian.PutUint32(buf[pos:], 0)
	pos += 4

	// number of docs
	binary.BigEndian.PutUint64(buf[pos:], numDocs)
	pos += 8

	// stored field index location
	binary.BigEndian.PutUint64(buf[pos:], storedIndexOffset)
	pos += 8

	// sections index location
	binary.BigEndian.PutUint64(buf[pos:], sectionsIndexOffset)
	pos += 8

	// chunk mode
	binary.BigEndian.PutUint32(buf[pos:], chunkMode)
	pos += 4

	// version
	binary.BigEndian.PutUint32(buf[pos:], Version)
	pos += 4

	// write everything except CRC to update the hash
	_, err := w.Write(buf[:pos])
	if err != nil {
		return err
	}

	// write CRC-32 of everything up to but not including this CRC
	binary.BigEndian.PutUint32(buf[pos:], w.crc)
	_, err = w.Write(buf[pos : pos+4])
	return err
}

func writeUvarints(w io.Writer, vals ...uint64) (tw int, err error) {
	var tmp [binary.MaxVarintLen64]byte
	buf := tmp[:]
	for _, val := range vals {
		n := binary.PutUvarint(buf, val)
		var nw int
		nw, err = w.Write(buf[:n])
		tw += nw
		if err != nil {
			return tw, err
		}
	}
	return tw, err
}
