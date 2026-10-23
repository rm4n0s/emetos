package editor

import (
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func TestShownLength(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		after int
		want  int
	}{
		{"short", "hello", 10, 5},
		{"exactly the limit", "hello", 5, 5},
		{"cut", "hello world", 5, 5},
		{"no limit with a negative value", "hello world", -1, 11},
		{"no limit with zero", "hello world", 0, 11},
		{"cut at the start of a character", "aé", 2, 1},
		{"cut before a four byte character", "ab😀cd", 3, 2},
		{"empty", "", 3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shownLength(tc.text, tc.after)
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
			if !utf8.ValidString(tc.text[:got]) {
				t.Errorf("cut inside a character: %q", tc.text[:got])
			}
		})
	}
}

func longLineRig(t *testing.T, line string, stopAfter int) (*ViewModel, geometry) {
	t.Helper()
	opts := goOptions()
	opts.StopRenderingLineAfter = stopAfter
	vm := NewViewModel(newTestDoc(line+"\nshort\n"), opts, nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 200})
	return vm, newGeometry(vm, ts.Dispatcher().TextEngine())
}

func TestPositionsBeyondTheDrawnPartUseTheWidthOfACharacter(t *testing.T) {
	line := strings.Repeat("abcdefghij", 50) // 500 bytes
	_, g := longLineRig(t, line, 100)
	cw := g.charWidth
	if cw <= 0 {
		t.Fatal("no character width")
	}
	// the drawn part is as wide as 100 characters
	if got := g.textWidth(line[:100]); math.Abs(got-100*cw) > 0.01 {
		t.Fatalf("width of the drawn part %v, want %v", got, 100*cw)
	}
	cases := []struct {
		name string
		col  int
		want float64
	}{
		{"start", 0, 0},
		{"in the drawn part", 40, 40 * cw},
		{"at the edge", 100, 100 * cw},
		{"past the edge", 250, 250 * cw},
		{"the end of the line", 500, 500 * cw},
		{"past the end of the line", 900, 500 * cw},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := g.x(0, tc.col); math.Abs(got-tc.want) > 0.01 {
				t.Errorf("x(%d) = %v, want %v", tc.col, got, tc.want)
			}
		})
	}
	if got := g.rowEnd(0); math.Abs(got-500*cw) > 0.01 {
		t.Errorf("rowEnd %v, want %v", got, 500*cw)
	}
	// and back: the column of a point
	for _, col := range []int{0, 40, 100, 250, 499, 500} {
		p := g.positionAt(g.x(0, col)+0.1, 1)
		if p.Line != 0 || p.Col != col {
			t.Errorf("positionAt of column %d gives %v", col, p)
		}
	}
	if p := g.positionAt(1e6, 1); p.Col != 500 {
		t.Errorf("far to the right is the end of the line: %v", p)
	}
	// a character past the edge has the width of a character
	r := g.rectOf(buffer.Position{Line: 0, Col: 300})
	if math.Abs((r.Right-r.Left)-cw) > 0.01 || math.Abs(r.Left-300*cw) > 0.01 {
		t.Errorf("rect %v", r)
	}
}

func TestLongLineIsCutAtACharacter(t *testing.T) {
	line := strings.Repeat("é", 100) // 200 bytes
	_, g := longLineRig(t, line, 51)
	text, _ := g.rowText(0)
	if len(text) != 50 || !utf8.ValidString(text) {
		t.Errorf("shown %d bytes %q", len(text), text)
	}
}

func TestAHugeSingleLineStaysResponsive(t *testing.T) {
	const size = 5 << 20
	line := strings.Repeat("0123456789abcdef ", size/17)
	opts := goOptions()
	opts.StopRenderingLineAfter = 10000
	opts.WordWrap = "on"
	vm := NewViewModel(newTestDoc(line+"\nsecond line\n"), opts, nil)
	start := time.Now()
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 300})
	cursorAt(vm, 0, size/2)
	ts.Pump(time.Millisecond)
	if err := vm.RunCommand("cursorEnd", nil); err != nil {
		t.Fatal(err)
	}
	ts.Pump(time.Millisecond)
	if err := vm.RunCommand("cursorDown", nil); err != nil {
		t.Fatal(err)
	}
	ts.Pump(time.Millisecond)
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("a 5 MB line took %v to show and move in", elapsed)
	}
	if w, _ := vm.ViewportSize(); w <= 0 {
		t.Error("no viewport")
	}
	if rows := vm.Projection().RowCount(); rows != 3 {
		t.Errorf("a line longer than editor.stopRenderingLineAfter is not wrapped: %d rows (two lines and the empty one after the last line break)", rows)
	}
}
