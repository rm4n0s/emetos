package langserver

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/extension"
)

// Positions of a server's own protocol. The positions of the extension API are a zero-based line and a column in bytes of the UTF-8
// line; the protocol counts the column in the encoding the server chose (LanguageServers().PositionEncoding): bytes, UTF-16 code
// units (the default) or code points. The packs that send a request of their own (rust-analyzer's) convert with these.

// The names of the encodings, as the protocol spells them.
const (
	EncodingUTF8  = "utf-8"
	EncodingUTF16 = "utf-16"
	EncodingUTF32 = "utf-32"
)

// Position is a position of the protocol: a zero-based line and a column in the units of the encoding.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a range of the protocol.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// lineOf returns the text of the zero-based line without its line break; ok is false for a line past the end.
func lineOf(text string, line int) (string, bool) {
	for ; line > 0; line-- {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			return "", false
		}
		text = text[i+1:]
	}
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSuffix(text, "\r"), true
}

// ToProtocol converts a position of the API in the text to the protocol's, counted in the encoding. A column past the end of the line is
// the end of the line, and a column inside a character is the start of it.
func ToProtocol(text string, p extension.Position, encoding string) Position {
	line, _ := lineOf(text, p.Line)
	col := min(max(p.Col, 0), len(line))
	for col > 0 && col < len(line) && !utf8.RuneStart(line[col]) {
		col--
	}
	switch encoding {
	case EncodingUTF8:
		return Position{Line: p.Line, Character: col}
	case EncodingUTF32:
		return Position{Line: p.Line, Character: utf8.RuneCountInString(line[:col])}
	}
	return Position{Line: p.Line, Character: utf16Units(line[:col])}
}

// FromProtocol converts a position of the protocol in the text to the API's. A column past the end of the line is the end of the line.
func FromProtocol(text string, p Position, encoding string) extension.Position {
	line, _ := lineOf(text, p.Line)
	n := max(p.Character, 0)
	switch encoding {
	case EncodingUTF8:
		return extension.Position{Line: p.Line, Col: min(n, len(line))}
	case EncodingUTF32:
		col := 0
		for _, r := range line {
			if n == 0 {
				break
			}
			col += utf8.RuneLen(r)
			n--
		}
		return extension.Position{Line: p.Line, Col: min(col, len(line))}
	}
	col := 0
	for _, r := range line {
		w := utf16.RuneLen(r)
		if w < 1 {
			w = 1
		}
		if n < w {
			break
		}
		n -= w
		col += utf8.RuneLen(r)
	}
	return extension.Position{Line: p.Line, Col: min(col, len(line))}
}

// RangeToProtocol converts a range of the API to the protocol's.
func RangeToProtocol(text string, r extension.Range, encoding string) Range {
	return Range{Start: ToProtocol(text, r.Start, encoding), End: ToProtocol(text, r.End, encoding)}
}

// RangeFromProtocol converts a range of the protocol to the API's.
func RangeFromProtocol(text string, r Range, encoding string) extension.Range {
	return extension.Range{Start: FromProtocol(text, r.Start, encoding), End: FromProtocol(text, r.End, encoding)}
}

// UTF16Column is the column of the position in UTF-16 code units: the column Documents().Show takes (counted from 0 here, from 1
// there).
func UTF16Column(text string, p extension.Position) int {
	return ToProtocol(text, p, EncodingUTF16).Character
}

// utf16Units counts the UTF-16 code units of a string; a byte that is no UTF-8 counts one.
func utf16Units(s string) int {
	n := 0
	for _, r := range s {
		w := utf16.RuneLen(r)
		if w < 1 {
			w = 1
		}
		n += w
	}
	return n
}
