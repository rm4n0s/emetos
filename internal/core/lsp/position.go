package lsp

import (
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// Position is a position of a document as the protocol has it: a zero-based line and a zero-based
// character offset in the line, counted in the units of the negotiated Encoding (LSP 3.17 "Position").
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a span of a document from Start up to End, which is not in it.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Encoding is how a server counts the characters of a line (LSP 3.17 "Position encoding kind"). Emetos
// counts bytes of UTF-8 (D6), so every position that crosses the wire is converted.
type Encoding int

const (
	// UTF16 counts UTF-16 code units; it is what a server uses unless it says otherwise, so it is the
	// zero value.
	UTF16 Encoding = iota
	// UTF8 counts bytes of UTF-8, as Emetos does; the client asks for it first.
	UTF8
	// UTF32 counts code points. The client does not offer it, but understands a server that picks it.
	UTF32
)

// The names of the encodings in the protocol.
const (
	EncodingNameUTF8  = "utf-8"
	EncodingNameUTF16 = "utf-16"
	EncodingNameUTF32 = "utf-32"
)

// ParseEncoding reads the name of an encoding as the protocol spells it; ok is false for another name.
func ParseEncoding(name string) (e Encoding, ok bool) {
	switch name {
	case EncodingNameUTF8:
		return UTF8, true
	case EncodingNameUTF16:
		return UTF16, true
	case EncodingNameUTF32:
		return UTF32, true
	}
	return UTF16, false
}

// String is the name of the encoding in the protocol.
func (e Encoding) String() string {
	switch e {
	case UTF8:
		return EncodingNameUTF8
	case UTF32:
		return EncodingNameUTF32
	}
	return EncodingNameUTF16
}

// unitWidth is how many units a character takes. A byte that is not UTF-8 is a character of its own
// that JSON carries as U+FFFD, so it counts as that character does for the server: three bytes of UTF-8,
// one unit otherwise.
func (e Encoding) unitWidth(r rune, size int) int {
	if r == utf8.RuneError && size == 1 {
		r, size = utf8.RuneError, utf8.RuneLen(utf8.RuneError)
	}
	switch e {
	case UTF8:
		return size
	case UTF32:
		return 1
	}
	if r >= 0x10000 {
		return 2
	}
	return 1
}

// Column converts a byte column of a line to a character offset of the encoding. A byte column in the
// middle of a character counts as the start of it, one past the end of the line as the end.
func (e Encoding) Column(line string, byteCol int) int {
	byteCol = min(max(byteCol, 0), len(line))
	if e == UTF8 && utf8.ValidString(line[:byteCol]) {
		return byteCol
	}
	units := 0
	for i := 0; i < byteCol; {
		r, size := utf8.DecodeRuneInString(line[i:])
		if i+size > byteCol {
			break
		}
		units += e.unitWidth(r, size)
		i += size
	}
	return units
}

// ByteColumn converts a character offset of the encoding to a byte column of a line. An offset in the
// middle of a character (the second unit of a surrogate pair) is the start of it, and one past the end of
// the line is the end, as the protocol asks of a server.
func (e Encoding) ByteColumn(line string, offset int) int {
	if offset <= 0 {
		return 0
	}
	if e == UTF8 && utf8.ValidString(line) {
		return min(offset, len(line))
	}
	units := 0
	i := 0
	for i < len(line) {
		r, size := utf8.DecodeRuneInString(line[i:])
		w := e.unitWidth(r, size)
		if units+w > offset {
			break
		}
		units += w
		i += size
	}
	return i
}

// Lines is what the position conversions need of a text; buffer.Snapshot is one.
type Lines interface {
	LineCount() int
	LineContent(line int) (string, error)
}

// lineAt gives the line to convert in: line is moved into the text, so that a position past the end
// becomes the end of the last line (the protocol says a line past the end is the end of the document).
func lineAt(text Lines, line int) (int, string, bool) {
	line = min(max(line, 0), text.LineCount()-1)
	content, err := text.LineContent(line)
	if err != nil {
		return line, "", false
	}
	return line, content, true
}

// PositionToLSP converts a position of text, in byte columns, to the encoding.
func (e Encoding) PositionToLSP(text Lines, p buffer.Position) Position {
	line, content, ok := lineAt(text, p.Line)
	if !ok {
		return Position{Line: line}
	}
	if p.Line > line {
		return Position{Line: line, Character: e.Column(content, len(content))}
	}
	return Position{Line: line, Character: e.Column(content, p.Col)}
}

// PositionFromLSP converts a position the server sent, counted in the encoding, to byte columns of text. A
// character past the end of its line is the end of the line.
func (e Encoding) PositionFromLSP(text Lines, p Position) buffer.Position {
	line, content, ok := lineAt(text, p.Line)
	if !ok {
		return buffer.Position{Line: line}
	}
	if p.Line > line {
		return buffer.Position{Line: line, Col: len(content)}
	}
	return buffer.Position{Line: line, Col: e.ByteColumn(content, p.Character)}
}

// RangeToLSP converts a range of text to the encoding.
func (e Encoding) RangeToLSP(text Lines, r buffer.Range) Range {
	return Range{Start: e.PositionToLSP(text, r.Start), End: e.PositionToLSP(text, r.End)}
}

// RangeFromLSP converts a range the server sent to byte columns of text; the ends are swapped when the
// server gave them the wrong way round.
func (e Encoding) RangeFromLSP(text Lines, r Range) buffer.Range {
	return buffer.NewRange(e.PositionFromLSP(text, r.Start), e.PositionFromLSP(text, r.End))
}
