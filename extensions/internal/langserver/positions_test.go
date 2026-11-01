package langserver

import (
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// Positions of a server's own protocol, in the three encodings. "é" is two bytes and one UTF-16 unit; "😀" is four bytes and two UTF-16
// units (one code point); "日" is three bytes and one unit. The text of the tests has a line break and a CRLF.
const positionText = "aé😀日z\r\nsecond\nthird"

func TestPositionsConvertBothWays(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		api      extension.Position
		protocol Position
	}{
		{"start", EncodingUTF16, extension.Position{}, Position{}},
		{"after the é, in UTF-8", EncodingUTF8, extension.Position{Col: 3}, Position{Character: 3}},
		{"after the é, in UTF-16", EncodingUTF16, extension.Position{Col: 3}, Position{Character: 2}},
		{"after the é, in UTF-32", EncodingUTF32, extension.Position{Col: 3}, Position{Character: 2}},
		{"after the emoji, in UTF-8", EncodingUTF8, extension.Position{Col: 7}, Position{Character: 7}},
		{"after the emoji, in UTF-16", EncodingUTF16, extension.Position{Col: 7}, Position{Character: 4}},
		{"after the emoji, in UTF-32", EncodingUTF32, extension.Position{Col: 7}, Position{Character: 3}},
		{"after the 日, in UTF-16", EncodingUTF16, extension.Position{Col: 10}, Position{Character: 5}},
		{"the end of the first line, before the CR", EncodingUTF16, extension.Position{Col: 11}, Position{Character: 6}},
		{"the second line", EncodingUTF16, extension.Position{Line: 1, Col: 3}, Position{Line: 1, Character: 3}},
		{"the last line, which has no break", EncodingUTF8, extension.Position{Line: 2, Col: 5}, Position{Line: 2, Character: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToProtocol(positionText, tt.api, tt.encoding); got != tt.protocol {
				t.Errorf("ToProtocol = %+v, want %+v", got, tt.protocol)
			}
			if got := FromProtocol(positionText, tt.protocol, tt.encoding); got != tt.api {
				t.Errorf("FromProtocol = %+v, want %+v", got, tt.api)
			}
		})
	}
}

func TestPositionsOutsideTheTextAreClamped(t *testing.T) {
	tests := []struct {
		name string
		got  Position
		want Position
	}{
		{"a column past the end of the line", ToProtocol(positionText, extension.Position{Col: 99}, EncodingUTF16), Position{Character: 6}},
		{"a negative column", ToProtocol(positionText, extension.Position{Col: -4}, EncodingUTF16), Position{}},
		{"inside the emoji: the start of it", ToProtocol(positionText, extension.Position{Col: 5}, EncodingUTF16), Position{Character: 2}},
		{"a line past the end", ToProtocol(positionText, extension.Position{Line: 9, Col: 3}, EncodingUTF16), Position{Line: 9}},
		{"UTF-32 past the end", ToProtocol(positionText, extension.Position{Col: 99}, EncodingUTF32), Position{Character: 5}},
		{"UTF-8 past the end", ToProtocol(positionText, extension.Position{Col: 99}, EncodingUTF8), Position{Character: 11}},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.name, tt.got, tt.want)
		}
	}
	back := []struct {
		name string
		got  extension.Position
		want extension.Position
	}{
		{"a column past the end of the line", FromProtocol(positionText, Position{Character: 99}, EncodingUTF16), extension.Position{Col: 11}},
		{"UTF-8 past the end", FromProtocol(positionText, Position{Character: 99}, EncodingUTF8), extension.Position{Col: 11}},
		{"UTF-32 past the end", FromProtocol(positionText, Position{Character: 99}, EncodingUTF32), extension.Position{Col: 11}},
		{"a negative column", FromProtocol(positionText, Position{Character: -1}, EncodingUTF16), extension.Position{}},
		{"the middle of an emoji (a UTF-16 unit)", FromProtocol(positionText, Position{Character: 3}, EncodingUTF16), extension.Position{Col: 3}},
		{"a line past the end", FromProtocol(positionText, Position{Line: 7, Character: 3}, EncodingUTF16), extension.Position{Line: 7}},
	}
	for _, tt := range back {
		if tt.got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.name, tt.got, tt.want)
		}
	}
}

func TestRangesAndTheUTF16Column(t *testing.T) {
	r := extension.Range{Start: extension.Position{Col: 1}, End: extension.Position{Line: 1, Col: 3}}
	p := RangeToProtocol(positionText, r, EncodingUTF16)
	if p != (Range{Start: Position{Character: 1}, End: Position{Line: 1, Character: 3}}) {
		t.Errorf("RangeToProtocol = %+v", p)
	}
	if got := RangeFromProtocol(positionText, p, EncodingUTF16); got != r {
		t.Errorf("RangeFromProtocol = %+v", got)
	}
	if got := UTF16Column(positionText, extension.Position{Col: 7}); got != 4 {
		t.Errorf("UTF16Column = %d, want 4", got)
	}
}
