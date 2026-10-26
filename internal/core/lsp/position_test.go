package lsp

import (
	"math/rand"
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// The line used below: a (1 byte), é (2 bytes), € (3 bytes), 😀 (4 bytes, a surrogate pair in UTF-16), b.
// Its byte columns are 0, 1, 3, 6, 10, 11 at the start of each character and at the end.
const mixedLine = "aé€😀b"

func TestEncodingColumn(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		byteCol int
		utf8    int
		utf16   int
		utf32   int
	}{
		{"empty line", "", 0, 0, 0, 0},
		{"ascii", "hello", 3, 3, 3, 3},
		{"ascii end", "hello", 5, 5, 5, 5},
		{"start of the mixed line", mixedLine, 0, 0, 0, 0},
		{"after a", mixedLine, 1, 1, 1, 1},
		{"after the 2-byte é", mixedLine, 3, 3, 2, 2},
		{"after the 3-byte €", mixedLine, 6, 6, 3, 3},
		{"after the 4-byte 😀", mixedLine, 10, 10, 5, 4},
		{"end of the mixed line", mixedLine, 11, 11, 6, 5},
		{"inside é counts as its start", mixedLine, 2, 1, 1, 1},
		{"inside € counts as its start", mixedLine, 5, 3, 2, 2},
		{"inside 😀 counts as its start", mixedLine, 8, 6, 3, 3},
		{"past the end is the end", mixedLine, 99, 11, 6, 5},
		{"before the start is the start", mixedLine, -3, 0, 0, 0},
		{"a byte that is not UTF-8 is U+FFFD for the server", "a\xffb", 2, 4, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for enc, want := range map[Encoding]int{UTF8: tt.utf8, UTF16: tt.utf16, UTF32: tt.utf32} {
				if got := enc.Column(tt.line, tt.byteCol); got != want {
					t.Errorf("%v.Column(%q, %d) = %d, want %d", enc, tt.line, tt.byteCol, got, want)
				}
			}
		})
	}
}

func TestEncodingByteColumn(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		offset int
		utf8   int
		utf16  int
		utf32  int
	}{
		{"empty line", "", 0, 0, 0, 0},
		{"empty line, past the end", "", 4, 0, 0, 0},
		{"ascii", "hello", 3, 3, 3, 3},
		{"start of the mixed line", mixedLine, 0, 0, 0, 0},
		{"offset 1", mixedLine, 1, 1, 1, 1},
		{"offset 2", mixedLine, 2, 2, 3, 3},
		{"offset 3", mixedLine, 3, 3, 6, 6},
		{"offset 4", mixedLine, 4, 4, 6, 10},
		{"offset 5", mixedLine, 5, 5, 10, 11},
		{"offset 6", mixedLine, 6, 6, 11, 11},
		{"offset 7", mixedLine, 7, 7, 11, 11},
		{"past the end is the end", mixedLine, 99, 11, 11, 11},
		{"negative is the start", mixedLine, -1, 0, 0, 0},
		{"a byte that is not UTF-8 is U+FFFD for the server", "a\xffb", 4, 2, 3, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for enc, want := range map[Encoding]int{UTF8: tt.utf8, UTF16: tt.utf16, UTF32: tt.utf32} {
				if got := enc.ByteColumn(tt.line, tt.offset); got != want {
					t.Errorf("%v.ByteColumn(%q, %d) = %d, want %d", enc, tt.line, tt.offset, got, want)
				}
			}
		})
	}
}

// TestColumnsRoundTrip: every column on a character boundary survives the two conversions, for lines of
// random characters of every width.
func TestColumnsRoundTrip(t *testing.T) {
	alphabet := []rune{'a', ' ', '\t', 'é', '€', '😀', 'ß', '中', '\U0001F9D1'}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 300; i++ {
		var line []rune
		for n := rng.Intn(20); n > 0; n-- {
			line = append(line, alphabet[rng.Intn(len(alphabet))])
		}
		s := string(line)
		for col := 0; col <= len(s); col++ {
			if col < len(s) && !utf8.RuneStart(s[col]) {
				continue
			}
			for _, enc := range []Encoding{UTF8, UTF16, UTF32} {
				if got := enc.ByteColumn(s, enc.Column(s, col)); got != col {
					t.Fatalf("%v: %q byte column %d came back as %d", enc, s, col, got)
				}
			}
		}
	}
}

func TestParseEncoding(t *testing.T) {
	tests := []struct {
		name string
		want Encoding
		ok   bool
	}{
		{"utf-8", UTF8, true}, {"utf-16", UTF16, true}, {"utf-32", UTF32, true}, {"utf-7", UTF16, false}, {"", UTF16, false},
	}
	for _, tt := range tests {
		got, ok := ParseEncoding(tt.name)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseEncoding(%q) = %v, %v", tt.name, got, ok)
		}
		if ok && got.String() != tt.name {
			t.Errorf("String() = %q, want %q", got.String(), tt.name)
		}
	}
}

func TestPositionsOfASnapshot(t *testing.T) {
	text := mixedLine + "\nsecond\n\nlast"
	snap := buffer.New(text).Snapshot()
	tests := []struct {
		name  string
		enc   Encoding
		buf   buffer.Position
		lsp   Position
		round buffer.Position // what the LSP position converts back to
	}{
		{"start", UTF16, buffer.Position{}, Position{}, buffer.Position{}},
		{"after the emoji, utf-16", UTF16, buffer.Position{Line: 0, Col: 10}, Position{Line: 0, Character: 5}, buffer.Position{Line: 0, Col: 10}},
		{"after the emoji, utf-8", UTF8, buffer.Position{Line: 0, Col: 10}, Position{Line: 0, Character: 10}, buffer.Position{Line: 0, Col: 10}},
		{"after the emoji, utf-32", UTF32, buffer.Position{Line: 0, Col: 10}, Position{Line: 0, Character: 4}, buffer.Position{Line: 0, Col: 10}},
		{"end of the first line", UTF16, buffer.Position{Line: 0, Col: 11}, Position{Line: 0, Character: 6}, buffer.Position{Line: 0, Col: 11}},
		{"middle of the second line", UTF16, buffer.Position{Line: 1, Col: 3}, Position{Line: 1, Character: 3}, buffer.Position{Line: 1, Col: 3}},
		{"the empty line", UTF16, buffer.Position{Line: 2, Col: 0}, Position{Line: 2, Character: 0}, buffer.Position{Line: 2, Col: 0}},
		{"end of the last line", UTF16, buffer.Position{Line: 3, Col: 4}, Position{Line: 3, Character: 4}, buffer.Position{Line: 3, Col: 4}},
		{"a column past the end of its line", UTF16, buffer.Position{Line: 1, Col: 40}, Position{Line: 1, Character: 6}, buffer.Position{Line: 1, Col: 6}},
		{"a line past the end is the end of the text", UTF16, buffer.Position{Line: 9, Col: 2}, Position{Line: 3, Character: 4}, buffer.Position{Line: 3, Col: 4}},
		{"a line before the start", UTF16, buffer.Position{Line: -2, Col: 0}, Position{Line: 0, Character: 0}, buffer.Position{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.enc.PositionToLSP(snap, tt.buf); got != tt.lsp {
				t.Errorf("PositionToLSP(%+v) = %+v, want %+v", tt.buf, got, tt.lsp)
			}
			if got := tt.enc.PositionFromLSP(snap, tt.lsp); got != tt.round {
				t.Errorf("PositionFromLSP(%+v) = %+v, want %+v", tt.lsp, got, tt.round)
			}
		})
	}
	// Positions the server sends that are outside the text.
	fromServer := []struct {
		name string
		in   Position
		want buffer.Position
	}{
		{"a line past the end", Position{Line: 50, Character: 0}, buffer.Position{Line: 3, Col: 4}},
		{"a character past the end of the line", Position{Line: 0, Character: 100}, buffer.Position{Line: 0, Col: 11}},
		{"the second half of a surrogate pair", Position{Line: 0, Character: 4}, buffer.Position{Line: 0, Col: 6}},
		{"negative numbers", Position{Line: -1, Character: -1}, buffer.Position{}},
	}
	for _, tt := range fromServer {
		t.Run(tt.name, func(t *testing.T) {
			if got := UTF16.PositionFromLSP(snap, tt.in); got != tt.want {
				t.Errorf("PositionFromLSP(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestRangesOfASnapshot(t *testing.T) {
	snap := buffer.New("aé€😀b\nsecond").Snapshot()
	r := buffer.Range{Start: buffer.Position{Line: 0, Col: 3}, End: buffer.Position{Line: 1, Col: 2}}
	got := UTF16.RangeToLSP(snap, r)
	want := Range{Start: Position{Line: 0, Character: 2}, End: Position{Line: 1, Character: 2}}
	if got != want {
		t.Errorf("RangeToLSP = %+v, want %+v", got, want)
	}
	if back := UTF16.RangeFromLSP(snap, got); back != r {
		t.Errorf("RangeFromLSP = %+v, want %+v", back, r)
	}
	// A server that gives the ends the wrong way round is read as the range between them.
	swapped := Range{Start: want.End, End: want.Start}
	if back := UTF16.RangeFromLSP(snap, swapped); back != r {
		t.Errorf("RangeFromLSP(swapped) = %+v, want %+v", back, r)
	}
}
