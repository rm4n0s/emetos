package editor

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func TestWhitespaceMarks(t *testing.T) {
	sp := func(cols ...int) []wsMark {
		var out []wsMark
		for _, c := range cols {
			out = append(out, wsMark{Col: c})
		}
		return out
	}
	cases := []struct {
		name      string
		text      string
		mode      string
		selection []colSpan
		continues bool
		stopAfter int
		want      []wsMark
	}{
		{"none", "  a b  ", "none", nil, false, -1, nil},
		{"an unknown mode is none", "  a b  ", "sometimes", nil, false, -1, nil},
		{"all", " a b  ", "all", nil, false, -1, sp(0, 2, 4, 5)},
		{"all with a tab", "\ta\tb", "all", nil, false, -1, []wsMark{{0, true}, {2, true}}},
		{"boundary: leading, trailing and doubled", "  a b  c ", "boundary", nil, false, -1, sp(0, 1, 5, 6, 8)},
		{"boundary leaves a single space between words", "a b c", "boundary", nil, false, -1, nil},
		{"boundary draws every tab", "a\tb", "boundary", nil, false, -1, []wsMark{{1, true}}},
		{"boundary: a blank line", "   ", "boundary", nil, false, -1, sp(0, 1, 2)},
		{"boundary: a single space ending a row that continues", "ab cd ", "boundary", nil, true, -1, nil},
		{"boundary: a double space ending a row that continues", "ab cd  ", "boundary", nil, true, -1, sp(5, 6)},
		{"boundary: a tab then a space ending a row that continues", "ab\t ", "boundary", nil, true, -1, []wsMark{{2, true}, {3, false}}},
		{"trailing", "  a b  ", "trailing", nil, false, -1, sp(5, 6)},
		{"trailing: a blank line", "  \t", "trailing", nil, false, -1, []wsMark{{0, false}, {1, false}, {2, true}}},
		{"trailing: a row that continues has none", "a  ", "trailing", nil, true, -1, nil},
		{"trailing: nothing at the end", "a b", "trailing", nil, false, -1, nil},
		{"selection with no selection", "a b", "selection", nil, false, -1, nil},
		{"selection: only the selected part", " a b c ", "selection", []colSpan{{2, 5}}, false, -1, sp(2, 4)},
		{"selection: two parts", " a b c ", "selection", []colSpan{{0, 2}, {5, 7}}, false, -1, sp(0, 6)},
		{"selection: a tab inside", "a\tb", "selection", []colSpan{{0, 3}}, false, -1, []wsMark{{1, true}}},
		{"selection: ends where the white space starts", "ab  ", "selection", []colSpan{{0, 2}}, false, -1, nil},
		{"stop rendering after a column", "a b c d", "all", nil, false, 4, sp(1, 3)},
		{"stop rendering after 0", "a b", "all", nil, false, 0, nil},
		{"non-ASCII text keeps byte columns", "é é", "all", nil, false, -1, sp(2)},
		{"an empty line", "", "all", nil, false, -1, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := whitespaceMarks(tc.text, tc.mode, tc.selection, tc.continues, tc.stopAfter); !slices.Equal(got, tc.want) {
				t.Errorf("whitespaceMarks(%q, %s) = %v, want %v", tc.text, tc.mode, got, tc.want)
			}
		})
	}
}

func TestSelectionSpans(t *testing.T) {
	sel := func(l1, c1, l2, c2 int) buffer.Selection {
		return buffer.Selection{Anchor: buffer.Position{Line: l1, Col: c1}, Active: buffer.Position{Line: l2, Col: c2}}
	}
	cases := []struct {
		name             string
		sels             []buffer.Selection
		line, start, end int
		want             []colSpan
	}{
		{"none", nil, 1, 0, 10, nil},
		{"inside the row", []buffer.Selection{sel(1, 2, 1, 5)}, 1, 0, 10, []colSpan{{2, 5}}},
		{"backwards", []buffer.Selection{sel(1, 5, 1, 2)}, 1, 0, 10, []colSpan{{2, 5}}},
		{"on another line", []buffer.Selection{sel(0, 2, 0, 5)}, 1, 0, 10, nil},
		{"over the whole line", []buffer.Selection{sel(0, 2, 2, 5)}, 1, 0, 10, []colSpan{{0, 10}}},
		{"starts on the line and goes on", []buffer.Selection{sel(1, 4, 3, 0)}, 1, 0, 10, []colSpan{{4, 10}}},
		{"ends on the line", []buffer.Selection{sel(0, 4, 1, 3)}, 1, 0, 10, []colSpan{{0, 3}}},
		{"a cursor", []buffer.Selection{sel(1, 3, 1, 3)}, 1, 0, 10, nil},
		{"a row in the middle of a line", []buffer.Selection{sel(1, 2, 1, 25)}, 1, 10, 20, []colSpan{{0, 10}}},
		{"a selection inside a later row", []buffer.Selection{sel(1, 2, 1, 5)}, 1, 10, 20, nil},
		{"a selection that ends in the row", []buffer.Selection{sel(1, 2, 1, 14)}, 1, 10, 20, []colSpan{{0, 4}}},
		{"two selections, out of order", []buffer.Selection{sel(1, 6, 1, 8), sel(1, 1, 1, 3)}, 1, 0, 10, []colSpan{{1, 3}, {6, 8}}},
		{"selections that touch join", []buffer.Selection{sel(1, 1, 1, 3), sel(1, 3, 1, 5)}, 1, 0, 10, []colSpan{{1, 5}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := selectionSpans(tc.sels, tc.line, tc.start, tc.end); !slices.Equal(got, tc.want) {
				t.Errorf("selectionSpans = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestControlMarks(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []ctrlMark
	}{
		{"plain text", "hello\tworld", nil},
		{"a C0 control", "a\x01b", []ctrlMark{{1, 1, 0x2401}}},
		{"NUL, ESC and DEL", "\x00\x1b\x7f", []ctrlMark{{0, 1, 0x2400}, {1, 1, 0x241B}, {2, 1, 0x2421}}},
		{"the tab is not one", "\t", nil},
		{"a bidi mark", "a\u202ab", []ctrlMark{{1, 3, 0}}},
		{"an arabic letter mark", "\u061c", []ctrlMark{{0, 2, 0}}},
		{"left-to-right mark and isolates", "\u200e\u2066\u2069", []ctrlMark{{0, 3, 0}, {3, 3, 0}, {6, 3, 0}}},
		{"other non-ASCII text", "é 日本", nil},
		{"C1 controls are not drawn", "\u0085", nil},
		{"empty", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := controlMarks(tc.text); !slices.Equal(got, tc.want) {
				t.Errorf("controlMarks(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
