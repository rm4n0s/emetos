package editor

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// hitGeometry shows a text and gives the geometry of its editor.
func hitGeometry(t *testing.T, text string, tabSize int) (geometry, *ViewModel, *gluttertest.Tester) {
	t.Helper()
	opts := goOptions()
	opts.TabSize = tabSize
	vm := NewViewModel(newTestDoc(text), opts, nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	return newGeometry(vm, ts.Dispatcher().TextEngine()), vm, ts
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestPositionAtASCIIUsesTheFastPath(t *testing.T) {
	g, _, _ := hitGeometry(t, "hello world\nfoo", 4)
	cw := g.charWidth
	if !g.isMonospace() || !asciiWithoutTabs("hello world") {
		t.Fatal("Go Mono and plain ASCII take the fast path")
	}
	cases := []struct {
		name string
		x, y float64
		want buffer.Position
	}{
		{"left of the text", -10, 5, positionOf(0, 0)},
		{"at the start", 0, 5, positionOf(0, 0)},
		{"in the left half of the first character", 0.49 * cw, 5, positionOf(0, 0)},
		{"in the right half of the first character", 0.51 * cw, 5, positionOf(0, 1)},
		{"on a boundary", 4 * cw, 5, positionOf(0, 4)},
		{"in the left half", 4.4 * cw, 5, positionOf(0, 4)},
		{"in the right half", 4.6 * cw, 5, positionOf(0, 5)},
		{"at the end of the line", 11 * cw, 5, positionOf(0, 11)},
		{"far past the end", 1000, 5, positionOf(0, 11)},
		{"the second line", 2.2 * cw, 19 + 5, positionOf(1, 2)},
		{"past the end of the short line", 50 * cw, 19 + 5, positionOf(1, 3)},
		{"above the text", 3 * cw, -30, positionOf(0, 3)},
		{"below the text", 1 * cw, 400, positionOf(1, 1)},
		{"the last pixel of a row", 3 * cw, 18.9, positionOf(0, 3)},
		{"the first pixel of the next", 3 * cw, 19, positionOf(1, 3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := g.positionAt(tc.x, tc.y); got != tc.want {
				t.Errorf("positionAt(%v, %v) = %v, want %v", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

func TestFastPathAgreesWithTheParagraph(t *testing.T) {
	g, _, _ := hitGeometry(t, "package main // a line of plain text\n", 4)
	text := "package main // a line of plain text"
	for x := -5.0; x < 40*g.charWidth; x += 0.7 {
		if fast, slow := g.columnAt(text, x), g.slowColumnAt(text, x); fast != slow {
			t.Fatalf("x = %v: fast %d, paragraph %d", x, fast, slow)
		}
	}
}

func TestPositionAtWithTabs(t *testing.T) {
	g, _, _ := hitGeometry(t, "\ta\tb\nab\tc", 4)
	cw := g.charWidth
	if asciiWithoutTabs("\ta") {
		t.Fatal("a tab is not on the fast path")
	}
	cases := []struct {
		name string
		x, y float64
		want buffer.Position
	}{
		{"before the tab", 1.9 * cw, 5, positionOf(0, 0)},
		{"in the right half of the tab", 2.1 * cw, 5, positionOf(0, 1)},
		{"after the tab", 4 * cw, 5, positionOf(0, 1)},
		{"in the left half of a", 4.4 * cw, 5, positionOf(0, 1)},
		{"after a", 5 * cw, 5, positionOf(0, 2)},
		{"in the second tab", 7 * cw, 5, positionOf(0, 3)},
		{"after b", 9 * cw, 5, positionOf(0, 4)},
		{"a tab that fills to the stop", 4 * cw, 24, positionOf(1, 3)},
		{"before it", 2 * cw, 24, positionOf(1, 2)},
		{"after c", 5 * cw, 24, positionOf(1, 4)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := g.positionAt(tc.x, tc.y); got != tc.want {
				t.Errorf("positionAt(%v, %v) = %v, want %v", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

// boundaryXs are the x of the grapheme boundaries of a line, from the paragraph's boxes.
func boundaryXs(g geometry, line string) (bounds []int, xs []float64) {
	bounds = edit.GraphemeBoundaries(line)
	for _, b := range bounds {
		xs = append(xs, g.x(0, b))
	}
	return bounds, xs
}

// The positions of Greek, CJK, emoji and combining text are checked through the paragraph's
// queries, not by goldens: a position is always at a grapheme boundary, a boundary's own x gives
// the boundary back, and the points between two boundaries go to the nearer one.
func TestPositionAtNonLatinText(t *testing.T) {
	lines := map[string]string{
		"greek":        "αβγ δεζ",
		"cjk":          "a日本語b",
		"emoji":        "x😀y",
		"zwj family":   "a👨‍👩‍👧b",
		"flag":         "p🇬🇷q",
		"combining":    "éä",
		"mixed":        "func Ωmega() { return \"日本\" } // 😀",
		"tab and wide": "\t日\tx",
	}
	for name, line := range lines {
		t.Run(name, func(t *testing.T) {
			g, _, _ := hitGeometry(t, line+"\nsecond", 4)
			bounds, xs := boundaryXs(g, line)
			if len(bounds) < 3 {
				t.Fatalf("boundaries %v", bounds)
			}
			for i := 1; i < len(xs); i++ {
				if xs[i] < xs[i-1] {
					t.Fatalf("x does not grow with the column: %v", xs)
				}
			}
			for i, b := range bounds {
				if i > 0 && xs[i] == xs[i-1] {
					continue // a zero-width cluster has no x of its own
				}
				if got := g.positionAt(xs[i], 5); got.Line != 0 || got.Col != b {
					t.Errorf("at the x of column %d: %v", b, got)
				}
				if i+1 < len(bounds) && xs[i+1] > xs[i] {
					w := xs[i+1] - xs[i]
					if got := g.positionAt(xs[i]+w*0.25, 5); got.Col != b {
						t.Errorf("a quarter into the character at %d: %v", b, got)
					}
					if got := g.positionAt(xs[i]+w*0.75, 5); got.Col != bounds[i+1] {
						t.Errorf("three quarters into the character at %d: %v, want column %d", b, got, bounds[i+1])
					}
				}
			}
			last := xs[len(xs)-1]
			for x := -3.0; x < last+10; x += 0.5 {
				got := g.positionAt(x, 5)
				if got.Line != 0 || !slices.Contains(bounds, got.Col) {
					t.Fatalf("x = %v gives %v, which is inside a grapheme (boundaries %v)", x, got, bounds)
				}
			}
			if got := g.positionAt(last+100, 5); got.Col != len(line) {
				t.Errorf("past the end: %v, want column %d", got, len(line))
			}
		})
	}
}

func TestRectOfAPosition(t *testing.T) {
	g, _, _ := hitGeometry(t, "abc\n\tx\nαβ\na😀b\n\nlast", 4)
	cw := g.charWidth
	cases := []struct {
		name         string
		pos          buffer.Position
		left, width  float64
		top          float64
		wantWideOnly bool
	}{
		{"first character", positionOf(0, 0), 0, cw, 0, false},
		{"second character", positionOf(0, 1), cw, cw, 0, false},
		{"end of the line is one character wide", positionOf(0, 3), 3 * cw, cw, 0, false},
		{"past the end is clamped to it", positionOf(0, 9), 3 * cw, cw, 0, false},
		{"a tab fills to the stop", positionOf(1, 0), 0, 4 * cw, 19, false},
		{"after the tab", positionOf(1, 1), 4 * cw, cw, 19, false},
		{"a two byte character", positionOf(2, 0), 0, cw, 38, false},
		{"after it", positionOf(2, 2), cw, cw, 38, false},
		{"an empty line", positionOf(4, 0), 0, cw, 4 * 19, false},
		{"the last line", positionOf(5, 2), 2 * cw, cw, 5 * 19, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := g.rectOf(tc.pos)
			if !near(r.Left, tc.left) || !near(r.Width(), tc.width) || !near(r.Top, tc.top) || !near(r.Height(), 19) {
				t.Errorf("rect %v, want left %v width %v top %v height 19", r, tc.left, tc.width, tc.top)
			}
		})
	}
	// An emoji is as wide as the paragraph says: from its x to the x of what follows.
	emoji := g.rectOf(positionOf(3, 1))
	if want := g.x(3, 5) - g.x(3, 1); emoji.Width() <= 0 || !near(emoji.Width(), want) {
		t.Errorf("the emoji's rect is %v wide, want %v", emoji.Width(), want)
	}
}

func TestRectOfRoundTripsPositionAt(t *testing.T) {
	text := "func f() {\n\tx := \"日本\"\n}"
	g, _, _ := hitGeometry(t, text, 4)
	snap := g.vm.Document().Snapshot()
	for line := range 3 {
		content, _ := snap.LineContent(line)
		for _, col := range edit.GraphemeBoundaries(content) {
			r := g.rectOf(positionOf(line, col))
			if got := g.positionAt(r.Left+r.Width()*0.25, r.Top+r.Height()/2); got != positionOf(line, col) {
				t.Errorf("the left part of the rect of %d:%d is %v", line, col, got)
			}
		}
	}
}

func TestHitTestingInTheWindow(t *testing.T) {
	var b strings.Builder
	for i := range 2000 {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString(" end\n")
	}
	vm := NewViewModel(newTestDoc(b.String()), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 400})
	layer := ts.RenderObject(gluttertest.ByType[*textLayer]()).(*RenderEditorText)
	cw := newGeometry(vm, ts.Dispatcher().TextEngine()).charWidth

	if got := layer.PositionAt(ui.Offset{Dx: 2.2 * cw, Dy: 3*19 + 5}); got != positionOf(3, 2) {
		t.Errorf("unscrolled: %v", got)
	}
	// 190 pixels down and 2 characters to the right: the point is in row 10 and column 2+2.
	vm.SetScrollLeft(0)
	ts.Scroll(gluttertest.ByType[*Editor](), ui.Offset{Dy: 190})
	ts.Pump(100 * time.Millisecond)
	if vm.ScrollTop() != 190 {
		t.Fatalf("scrolled to %v", vm.ScrollTop())
	}
	if got := layer.PositionAt(ui.Offset{Dx: 2.2 * cw, Dy: 5}); got != positionOf(10, 2) {
		t.Errorf("scrolled by 190: %v", got)
	}
	if got := layer.PositionAt(ui.Offset{Dx: 2.2 * cw, Dy: 398}); got != positionOf(30, 2) {
		t.Errorf("the bottom row: %v", got)
	}
	// The rect of a position follows the scroll.
	r := layer.RectOf(positionOf(10, 2))
	if !near(r.Top, 0) || !near(r.Left, 2*cw) || !near(r.Height(), 19) {
		t.Errorf("rect of the first row: %v", r)
	}
	if r := layer.RectOf(positionOf(0, 0)); r.Bottom > 0 {
		t.Errorf("a line scrolled out of view is above the layer: %v", r)
	}
	// The position under the rect's own center is the position.
	c := layer.RectOf(positionOf(12, 3))
	if got := layer.PositionAt(ui.Offset{Dx: c.Left + 1, Dy: (c.Top + c.Bottom) / 2}); got != positionOf(12, 3) {
		t.Errorf("round trip: %v", got)
	}
	noErrors(t, ts)
}

func replaceLine(line int, text string) []document.Edit {
	return []document.Edit{{Range: buffer.Range{Start: positionOf(line, 0), End: positionOf(line, 1000)}, Text: text}}
}

func documentEditOptions() document.EditOptions { return document.EditOptions{} }
