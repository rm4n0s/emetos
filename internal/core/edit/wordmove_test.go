package edit

import (
	"sort"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// deserializePipes removes the "|" marks of text and returns what is left. The
// marks are the positions that VS Code's tests expect the cursor to stop at.
func deserializePipes(marked string) string {
	return strings.ReplaceAll(marked, "|", "")
}

// serializePipes puts a "|" in text at each position (byte columns), in order. It
// is VS Code's wordTestUtils.serializePipePositions.
func serializePipes(t *testing.T, text string, positions []buffer.Position) string {
	t.Helper()
	sort.Slice(positions, func(i, j int) bool { return positions[i].Compare(positions[j]) < 0 })
	var sb strings.Builder
	line, col := 0, 0
	for i := 0; i <= len(text); i++ {
		if len(positions) > 0 && positions[0] == (buffer.Position{Line: line, Col: col}) {
			sb.WriteByte('|')
			positions = positions[1:]
		}
		if i == len(text) {
			break
		}
		sb.WriteByte(text[i])
		if text[i] == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	if len(positions) > 0 {
		t.Fatalf("positions left over: %+v", positions)
	}
	return sb.String()
}

// repeatUntil runs an action from a position until the cursor is at stop, and
// returns every position it stopped at (VS Code's testRepeatedActionAndExtractPositions).
func repeatUntil(t *testing.T, h *harness, action func(), stop buffer.Position) []buffer.Position {
	t.Helper()
	var stops []buffer.Position
	for {
		action()
		stops = append(stops, h.cur.Position())
		if h.cur.Position() == stop {
			return stops
		}
		if len(stops) > 1000 {
			t.Fatalf("endless loop at %+v", h.cur.Position())
		}
	}
}

// Ported from VS Code: reference/vscode/src/vs/editor/contrib/wordOperations/test/browser/wordOperations.test.ts
// The cursor movement tests: cursorWordLeft (WordStartFast), cursorWordStartLeft,
// cursorWordEndLeft, cursorWordRight and moveWordEndRight (WordEnd),
// moveWordStartRight, the accessibility variants. Each expected string has a "|"
// at every stop; positions are byte columns here.
func TestVSCodeWordMovement(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		start    [2]int // VS Code position; 0, 0 means the end of the text
		stop     [2]int
		left     bool
		nav      WordNavigation
		selectIt bool
	}{
		{"cursorWordLeft - simple", "|    \t|My |First |Line\t \n|\t|My |Second |Line\n|    |Third |Line🐶\n|\n|1", [2]int{0, 0}, [2]int{1, 1}, true, WordStartFast, false},
		{"cursorWordLeft - issue #832", "|   |/* |Just |some   |more   |text |a|+= |3 |+|5-|3 |+ |7 |*/  ", [2]int{0, 0}, [2]int{1, 1}, true, WordStartFast, false},
		{"cursorWordLeft - issue #48046: Word selection doesn't work as usual", "|deep.|object.|property", [2]int{1, 21}, [2]int{1, 1}, true, WordStartFast, false},
		{"cursorWordLeft - Does not recognize words", "|/* |これはテストです |/*", [2]int{0, 0}, [2]int{1, 1}, true, WordStartFast, true},
		{"cursorWordLeftSelect - issue #74369", "|this.|is.|a.|test", [2]int{1, 15}, [2]int{1, 1}, true, WordStartFast, true},
		{"cursorWordStartLeft", "|   |/* |Just |some   |more   |text |a|+= |3 |+|5|-|3 |+ |7 |*/  ", [2]int{0, 0}, [2]int{1, 1}, true, WordStart, false},
		{"cursorWordStartLeft - issue #51119: regression makes VS compatibility impossible", "|this|.|is|.|a|.|test", [2]int{0, 0}, [2]int{1, 1}, true, WordStart, false},
		{"cursorWordEndLeft", "|   /*| Just| some|   more|   text| a|+=| 3| +|5|-|3| +| 7| */|  ", [2]int{0, 0}, [2]int{1, 1}, true, WordEnd, false},
		{"cursorWordAccessibilityLeft", "|   /* |Just |some   |more   |text |a+= |3 +|5-|3 + |7 */  ", [2]int{0, 0}, [2]int{1, 1}, true, WordAccessibility, false},
		{"cursorWordRight - simple", "    \tMy| First| Line|\t |\n\tMy| Second| Line|\n    Third| Line🐶|\n|\n1|", [2]int{1, 1}, [2]int{5, 2}, false, WordEnd, false},
		{"cursorWordRight - issue #832", "   /*| Just| some|   more|   text| a|+=| 3| +5|-3| +| 7| */|  |", [2]int{1, 1}, [2]int{1, 50}, false, WordEnd, false},
		{"cursorWordRight - issue #41199", "console|.log|(err|)|", [2]int{1, 1}, [2]int{1, 17}, false, WordEnd, false},
		{"cursorWordRight - Does not recognize words", "/*| これはテストです|/*|", [2]int{1, 1}, [2]int{1, 14}, false, WordEnd, false},
		{"moveWordEndRight", "   /*| Just| some|   more|   text| a|+=| 3| +5|-3| +| 7| */|  |", [2]int{1, 1}, [2]int{1, 50}, false, WordEnd, false},
		{"moveWordStartRight", "   |/* |Just |some   |more   |text |a|+= |3 |+|5|-|3 |+ |7 |*/  |", [2]int{1, 1}, [2]int{1, 50}, false, WordStart, false},
		{"issue #51119: cursorWordStartRight regression makes VS compatibility impossible", "this|.|is|.|a|.|test|", [2]int{1, 1}, [2]int{1, 15}, false, WordStart, false},
		{"issue #64810: cursorWordStartRight skips first word after newline", "Hello |World|\n|Hei |mailman|", [2]int{1, 1}, [2]int{2, 12}, false, WordStart, false},
		{"cursorWordAccessibilityRight", "   /* |Just |some   |more   |text |a+= |3 +|5-|3 + |7 */  |", [2]int{1, 1}, [2]int{1, 50}, false, WordAccessibility, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := deserializePipes(tt.expected)
			h := newHarness(t, text)
			if tt.start == ([2]int{0, 0}) {
				h.cur = CursorOf(buffer.NewCursor(lineEnd(h.mv.Model, lastLine(h.mv.Model))))
			} else {
				h.cur = CursorOf(buffer.NewCursor(h.vsPos(tt.start[0], tt.start[1])))
			}
			action := func() {
				if tt.left {
					h.cur = h.mv.WordLeft(h.cur, tt.selectIt, tt.nav, false)
				} else {
					h.cur = h.mv.WordRight(h.cur, tt.selectIt, tt.nav)
				}
			}
			stops := repeatUntil(t, h, action, h.vsPos(tt.stop[0], tt.stop[1]))
			if got := serializePipes(t, text, stops); got != tt.expected {
				t.Errorf("stops\n got: %q\nwant: %q", got, tt.expected)
			}
		})
	}
}

// Ported from VS Code: wordOperations.test.ts ('cursorWordLeft - with selection',
// 'cursorWordRight - selection', 'cursorWordLeft - issue #169904: cursors out of
// sync').
func TestVSCodeWordMovementSelections(t *testing.T) {
	t.Run("cursorWordLeft - with selection", func(t *testing.T) {
		h := newHarness(t, cursorText)
		h.cur = CursorOf(buffer.NewCursor(h.vsPos(5, 2)))
		h.cur = h.mv.WordLeft(h.cur, true, WordStartFast, false)
		h.assertSelection(5, 2, 5, 1)
	})
	t.Run("cursorWordRight - selection", func(t *testing.T) {
		h := newHarness(t, cursorText)
		h.cur = h.mv.WordRight(h.cur, true, WordEnd)
		h.assertSelection(1, 1, 1, 8)
	})
	t.Run("cursorWordLeft - issue #169904: cursors out of sync", func(t *testing.T) {
		text := strings.Join([]string{
			".grid1 {",
			"  display: grid;",
			"  grid-template-columns:",
			"    [full-start] minmax(1em, 1fr)",
			"    [main-start] minmax(0, 40em) [main-end]",
			"    minmax(1em, 1fr) [full-end];",
			"}",
			".grid2 {",
			"  display: grid;",
			"  grid-template-columns:",
			"    [full-start] minmax(1em, 1fr)",
			"    [main-start] minmax(0, 40em) [main-end] minmax(1em, 1fr) [full-end];",
			"}",
		}, "\n")
		h := newHarness(t, text)
		starts := [][2]int{{5, 44}, {6, 32}, {12, 44}, {12, 72}}
		wants := [][2]int{{5, 43}, {6, 31}, {12, 43}, {12, 71}}
		for i, s := range starts {
			c := CursorOf(buffer.NewCursor(h.vsPos(s[0], s[1])))
			c = h.mv.WordLeft(c, false, WordStartFast, true)
			if want := h.vsPos(wants[i][0], wants[i][1]); c.Selection != buffer.NewCursor(want) {
				t.Errorf("cursor %d: %+v, want %+v", i, c.Selection, buffer.NewCursor(want))
			}
		}
	})
}

// A lone separator before a word is stepped over by cursorWordLeft only when there
// is one cursor (VS Code issue #169904 keeps several cursors in sync).
func TestWordStartFastWithSeveralCursors(t *testing.T) {
	h := newHarness(t, "foo.bar")
	c := CursorOf(buffer.NewCursor(buffer.Position{Col: 4}))
	if got := h.mv.WordLeft(c, false, WordStartFast, false).Position().Col; got != 0 {
		t.Errorf("one cursor stops at %d, want 0", got)
	}
	if got := h.mv.WordLeft(c, false, WordStartFast, true).Position().Col; got != 3 {
		t.Errorf("several cursors stop at %d, want 3", got)
	}
}
