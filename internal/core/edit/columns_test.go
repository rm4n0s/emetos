package edit

import (
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/vstest"
	"github.com/rm4n0s/errors"
)

// vsToCol converts a VS Code column (1-based, in UTF-16 units) of a one-line
// text to a byte column. A column before the first or after the last is moved
// into the line, as VS Code's functions do. ok is false for a column inside a
// surrogate pair, which has no byte column.
func vsToCol(t *testing.T, line string, column int) (col int, ok bool) {
	t.Helper()
	p, err := vstest.ToPosition(line, 1, column)
	if err == nil {
		return p.Col, true
	}
	appErr, _ := errors.FromError(err)
	switch {
	case appErr.HasRoute("VSColumnSplitsPair"):
		return 0, false
	case column < 1:
		return 0, true
	}
	return len(line), true
}

// colToVS converts a byte column back to a VS Code column.
func colToVS(t *testing.T, line string, col int) int {
	t.Helper()
	_, column, err := vstest.FromPosition(line, buffer.Position{Col: col})
	if err != nil {
		t.Fatalf("FromPosition(%q, %d): %v", line, col, err)
	}
	return column
}

// Ported from VS Code: reference/vscode/src/vs/editor/test/common/core/cursorColumns.test.ts
// The rows are converted from UTF-16 columns to byte columns through
// vstest. A row whose column is in the middle of a surrogate pair has no byte
// column and is skipped, and the tests count the skipped rows.
func TestVisibleColumn(t *testing.T) {
	skipped := 0
	for _, tt := range visibleColumnFromColumnCases {
		col, ok := vsToCol(t, tt.text, tt.arg)
		if !ok {
			skipped++
			continue
		}
		if got := VisibleColumn(tt.text, col, tt.tabSize); got != tt.expected {
			t.Errorf("VisibleColumn(%q, column %d = byte %d, %d) = %d, want %d", tt.text, tt.arg, col, tt.tabSize, got, tt.expected)
		}
	}
	if skipped != 1 {
		t.Errorf("skipped %d rows, want the 1 of '📚az' column 2", skipped)
	}
}

func TestStatusbarColumn(t *testing.T) {
	skipped := 0
	for _, tt := range toStatusbarColumnCases {
		col, ok := vsToCol(t, tt.text, tt.arg)
		if !ok {
			skipped++
			continue
		}
		if got := StatusbarColumn(tt.text, col, tt.tabSize); got != tt.expected {
			t.Errorf("StatusbarColumn(%q, column %d = byte %d, %d) = %d, want %d", tt.text, tt.arg, col, tt.tabSize, got, tt.expected)
		}
	}
	if skipped != 11 {
		t.Errorf("skipped %d rows, want the 11 whose column is inside a pair", skipped)
	}
}

func TestColumnFromVisible(t *testing.T) {
	for _, tt := range columnFromVisibleColumnCases {
		got := ColumnFromVisible(tt.text, tt.arg, tt.tabSize)
		if column := colToVS(t, tt.text, got); column != tt.expected {
			t.Errorf("ColumnFromVisible(%q, %d, %d) = byte %d = column %d, want column %d", tt.text, tt.arg, tt.tabSize, got, column, tt.expected)
		}
	}
}

func TestTabStops(t *testing.T) {
	for _, tt := range nextTabStopCases {
		if got := NextRenderTabStop(tt.visible, tt.tabSize); got != tt.expected {
			t.Errorf("NextRenderTabStop(%d, %d) = %d, want %d", tt.visible, tt.tabSize, got, tt.expected)
		}
		if got := NextIndentTabStop(tt.visible, tt.tabSize); got != tt.expected {
			t.Errorf("NextIndentTabStop(%d, %d) = %d, want %d", tt.visible, tt.tabSize, got, tt.expected)
		}
	}
	for _, tt := range prevTabStopCases {
		if got := PrevRenderTabStop(tt.visible, tt.tabSize); got != tt.expected {
			t.Errorf("PrevRenderTabStop(%d, %d) = %d, want %d", tt.visible, tt.tabSize, got, tt.expected)
		}
		if got := PrevIndentTabStop(tt.visible, tt.tabSize); got != tt.expected {
			t.Errorf("PrevIndentTabStop(%d, %d) = %d, want %d", tt.visible, tt.tabSize, got, tt.expected)
		}
	}
}

// Emetos's own: a tab size below 1 counts as 1 instead of dividing by zero.
func TestTabSizeBelowOne(t *testing.T) {
	for _, size := range []int{0, -3} {
		if got := NextRenderTabStop(5, size); got != 6 {
			t.Errorf("NextRenderTabStop(5, %d) = %d, want 6", size, got)
		}
		if got := PrevRenderTabStop(5, size); got != 4 {
			t.Errorf("PrevRenderTabStop(5, %d) = %d, want 4", size, got)
		}
		if got := VisibleColumn("\ta", 2, size); got != 2 {
			t.Errorf("VisibleColumn with tab size %d = %d, want 2", size, got)
		}
	}
}

// Emetos's own: the same checks as the ported ones, with the widths of SPEC §6.2
// worked out for byte columns over Greek, CJK, emoji, combining marks and joined
// emoji.
func TestVisibleColumnWidths(t *testing.T) {
	tests := []struct {
		name string
		line string
		col  int
		want int
	}{
		{"empty", "", 0, 0},
		{"ascii", "abc", 2, 2},
		{"greek is 1 per letter, 2 bytes each", "αβγ", 4, 2},
		{"cjk is 2 per character, 3 bytes each", "日本語", 6, 4},
		{"half of a cjk character is its start", "日本語", 4, 3},
		{"fullwidth latin is 2", "ＡＢ", 3, 2},
		{"hangul syllable is 2", "한", 3, 2},
		{"emoji is 2", "a😀b", 5, 3},
		{"flag of two regional indicators is one cluster of 2", "🇬🇷a", 8, 2},
		{"combining accent joins its letter", "éx", 3, 1},
		{"joined family emoji is one cluster of 2", "👨‍👩‍👧x", 18, 2},
		{"emoji with a skin tone is one cluster of 2", "👍🏽x", 8, 2},
		{"tab after text goes to the stop", "ab\tc", 3, 4},
		{"tab then text", "\tab", 3, 6},
		{"column past the end is the end", "abc", 99, 3},
		{"negative column is the start", "abc", -1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VisibleColumn(tt.line, tt.col, 4); got != tt.want {
				t.Errorf("VisibleColumn(%q, %d, 4) = %d, want %d", tt.line, tt.col, got, tt.want)
			}
		})
	}
}

// Emetos's own: ColumnFromVisible gives a column at the start of a character,
// and VisibleColumn of it is the nearest the line allows.
func TestColumnFromVisibleRoundTrip(t *testing.T) {
	lines := []string{"", "abc", "\tab\tc", "日本語x", "a😀b😀", "éé", "👨‍👩‍👧x\ty", "αβγ\t日"}
	for _, line := range lines {
		bounds := GraphemeBoundaries(line)
		for _, b := range bounds {
			v := VisibleColumn(line, b, 4)
			if got := ColumnFromVisible(line, v, 4); got != b {
				// Two boundaries can have the same visual column (a cluster of width 0 is
				// not possible, but a tab can end where the next stop is).
				if VisibleColumn(line, got, 4) != v {
					t.Errorf("%q: boundary %d has visual column %d, but ColumnFromVisible gives %d (visual %d)",
						line, b, v, got, VisibleColumn(line, got, 4))
				}
			}
			if got := ColumnFromVisible(line, v, 4); !utf8.RuneStart(byteAt(line, got)) {
				t.Errorf("%q: ColumnFromVisible(%d) = %d, inside a character", line, v, got)
			}
		}
	}
}

func byteAt(s string, i int) byte {
	if i >= len(s) {
		return 'x'
	}
	return s[i]
}
