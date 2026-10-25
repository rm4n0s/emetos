package fonts

import (
	"encoding/binary"
	"sort"
)

// Controls is the family of the font that has an empty glyph for the tab and nothing else
// (ControlsFont). The editor puts it first in the families of a row that has a tab.
const Controls = "emetos-controls"

// ControlsFont returns the bytes of a TrueType font with two glyphs: .notdef and an empty glyph
// that the character U+0009 (tab) maps to. It is under 300 bytes.
//
// Glutter: Glutter 1.7 (internal/text/layout, styleFonts.ResolveFace) looks for a font with a glyph
// for every character it shapes, in the order of the font files of the style, and when none has one
// it goes through all of them before the system's fallback. A tab has a glyph in no font, so the
// first row with a tab loaded every font file of the style's families (62 files, 116 MB on a
// desktop, 170 MB of Go heap and 300 MB resident, and a start slower by the time it takes) and
// kept them. With this font first, the tab is found at once. Glutter should take the control
// characters (and the tab, which it lays out itself when the paragraph has a tab size) to the
// first face without the search; docs/GLUTTER-ISSUES.md.
func ControlsFont() []byte {
	const (
		unitsPerEm = 1000
		advance    = 500
		numGlyphs  = 2
	)
	be := binary.BigEndian
	// cmap: format 4, one segment for U+0009 that maps to glyph 1, and the closing segment.
	cmap := make([]byte, 0, 48)
	cmap = be.AppendUint16(cmap, 0) // version
	cmap = be.AppendUint16(cmap, 1) // number of encoding records
	cmap = be.AppendUint16(cmap, 3) // platform: Windows
	cmap = be.AppendUint16(cmap, 1) // encoding: Unicode BMP
	cmap = be.AppendUint32(cmap, 12)
	cmap = be.AppendUint16(cmap, 4)  // format
	cmap = be.AppendUint16(cmap, 32) // length of the subtable
	cmap = be.AppendUint16(cmap, 0)  // language
	cmap = be.AppendUint16(cmap, 4)  // segCountX2
	cmap = be.AppendUint16(cmap, 4)  // searchRange
	cmap = be.AppendUint16(cmap, 1)  // entrySelector
	cmap = be.AppendUint16(cmap, 0)  // rangeShift
	cmap = be.AppendUint16(cmap, 9)  // endCode
	cmap = be.AppendUint16(cmap, 0xFFFF)
	cmap = be.AppendUint16(cmap, 0) // reservedPad
	cmap = be.AppendUint16(cmap, 9) // startCode
	cmap = be.AppendUint16(cmap, 0xFFFF)
	cmap = be.AppendUint16(cmap, 0xFFF8) // idDelta: 9 + 0xFFF8 = glyph 1
	cmap = be.AppendUint16(cmap, 1)
	cmap = be.AppendUint16(cmap, 0) // idRangeOffset
	cmap = be.AppendUint16(cmap, 0)

	head := make([]byte, 0, 54)
	head = be.AppendUint32(head, 0x00010000) // version
	head = be.AppendUint32(head, 0x00010000) // fontRevision
	head = be.AppendUint32(head, 0)          // checkSumAdjustment: not checked by the readers of Glutter
	head = be.AppendUint32(head, 0x5F0F3CF5) // magic
	head = be.AppendUint16(head, 0)          // flags
	head = be.AppendUint16(head, unitsPerEm)
	head = append(head, make([]byte, 16)...) // created, modified
	head = append(head, make([]byte, 8)...)  // xMin, yMin, xMax, yMax
	head = be.AppendUint16(head, 0)          // macStyle
	head = be.AppendUint16(head, 8)          // lowestRecPPEM
	head = be.AppendUint16(head, 2)          // fontDirectionHint
	head = be.AppendUint16(head, 0)          // indexToLocFormat: short offsets
	head = be.AppendUint16(head, 0)          // glyphDataFormat

	hhea := make([]byte, 0, 36)
	hhea = be.AppendUint32(hhea, 0x00010000)
	hhea = append(hhea, make([]byte, 6)...)  // ascender, descender, lineGap: no height of its own
	hhea = be.AppendUint16(hhea, advance)    // advanceWidthMax
	hhea = append(hhea, make([]byte, 6)...)  // minLeftSideBearing, minRightSideBearing, xMaxExtent
	hhea = be.AppendUint16(hhea, 1)          // caretSlopeRise
	hhea = append(hhea, make([]byte, 14)...) // caretSlopeRun, caretOffset, reserved, metricDataFormat
	hhea = be.AppendUint16(hhea, numGlyphs)  // numberOfHMetrics

	maxp := be.AppendUint32(nil, 0x00005000) // version 0.5
	maxp = be.AppendUint16(maxp, numGlyphs)

	var hmtx []byte
	for range numGlyphs {
		hmtx = be.AppendUint16(hmtx, advance)
		hmtx = be.AppendUint16(hmtx, 0) // left side bearing
	}
	loca := make([]byte, 2*(numGlyphs+1)) // every glyph is empty
	glyf := make([]byte, 4)

	tables := map[string][]byte{"cmap": cmap, "glyf": glyf, "head": head, "hhea": hhea, "hmtx": hmtx, "loca": loca, "maxp": maxp}
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	out := be.AppendUint32(nil, 0x00010000)
	out = be.AppendUint16(out, uint16(len(tags)))
	out = be.AppendUint16(out, 64) // searchRange: 16 * the largest power of 2 <= 7
	out = be.AppendUint16(out, 2)  // entrySelector
	out = be.AppendUint16(out, uint16(16*len(tags)-64))
	offset := 12 + 16*len(tags)
	for _, tag := range tags {
		data := tables[tag]
		out = append(out, tag...)
		out = be.AppendUint32(out, checksum(data))
		out = be.AppendUint32(out, uint32(offset))
		out = be.AppendUint32(out, uint32(len(data)))
		offset += (len(data) + 3) &^ 3
	}
	for _, tag := range tags {
		out = append(out, tables[tag]...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	return out
}

// checksum is the OpenType checksum of a table: the sum of its 32-bit words, the last one padded
// with zeros.
func checksum(data []byte) uint32 {
	var sum uint32
	for i := 0; i < len(data); i += 4 {
		var word [4]byte
		copy(word[:], data[i:])
		sum += binary.BigEndian.Uint32(word[:])
	}
	return sum
}
