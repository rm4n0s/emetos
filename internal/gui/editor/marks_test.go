package editor

import (
	"image/color"
	"reflect"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

func TestBackgroundMarksFollowTheEditsOfTheDocument(t *testing.T) {
	doc := newTestDoc("a\nb\nc\nd\n")
	vm := NewViewModel(doc, testOptions(), nil)
	other := NewViewModel(doc, testOptions(), nil)
	red, green := ui.NewColor(0xFFFF0000), ui.NewColor(0xFF00FF00)
	marks := []BackgroundMark{
		{Range: buffer.Range{Start: positionOf(1, 0), End: positionOf(2, 1)}, WholeLine: true, Background: red, Gutter: green},
		{Range: buffer.Range{Start: positionOf(3, 0), End: positionOf(3, 1)}, Background: green},
	}
	vm.SetBackgroundMarks(marks)
	if got := vm.BackgroundMarks(); !reflect.DeepEqual(got, marks) {
		t.Fatalf("marks %+v, want %+v", got, marks)
	}
	if got := other.BackgroundMarks(); len(got) != 0 {
		t.Errorf("another editor of the document has the marks: %+v", got)
	}
	// a line above pushes the marks down
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "new\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	got := vm.BackgroundMarks()
	if len(got) != 2 || got[0].Range.Start.Line != 2 || got[0].Range.End.Line != 3 || got[1].Range.Start.Line != 4 {
		t.Errorf("marks after the edit: %+v", got)
	}
	// setting marks replaces them
	vm.SetBackgroundMarks(marks[1:])
	if got := vm.BackgroundMarks(); len(got) != 1 || got[0].Background != green {
		t.Errorf("marks after replacing: %+v", got)
	}
	notified := 0
	vm.AddListener(func() { notified++ })
	vm.SetBackgroundMarks(nil)
	if len(vm.BackgroundMarks()) != 0 || notified == 0 {
		t.Errorf("marks after clearing: %+v, notified %d", vm.BackgroundMarks(), notified)
	}
	vm.SetBackgroundMarks(marks)
	vm.DisposeMarks()
	if len(vm.BackgroundMarks()) != 0 || len(doc.AllDecorations()) != 0 {
		t.Errorf("marks after disposing: %+v", vm.BackgroundMarks())
	}
}

func TestBackgroundMarksArePainted(t *testing.T) {
	r := newMouseRig(t, "one\ntwo\nthree\nfour\nfive\n", func(o *Options) { o.RenderLineHighlight = "none" })
	r.settle()
	line, gutterStrip, chars := ui.NewColor(0xFF112233), ui.NewColor(0xFF445566), ui.NewColor(0xFF778899)
	r.vm.SetBackgroundMarks([]BackgroundMark{
		{Range: buffer.Range{Start: positionOf(1, 0), End: positionOf(2, 5)}, WholeLine: true, Background: line, Gutter: gutterStrip},
		{Range: buffer.Range{Start: positionOf(4, 1), End: positionOf(4, 3)}, Background: chars},
	})
	r.settle()
	rgba := func(c ui.Color) color.NRGBA {
		v := c.ToARGB32()
		return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xFF}
	}
	row := func(n int) int { return int(float64(n)*r.lineH) + 1 } // the top of a row: no glyph reaches it
	far := int(r.gutter) + 500
	for _, tc := range []struct {
		name string
		x, y int
		want color.NRGBA
	}{
		{"first marked line, text area", far, row(1), rgba(line)},
		{"second marked line, text area", far, row(2), rgba(line)},
		{"first marked line, gutter", 3, row(1), rgba(gutterStrip)},
		{"second marked line, gutter", 3, row(2), rgba(gutterStrip)},
		{"characters of a line", int(r.gutter + 1.5*r.cw), row(4), rgba(chars)},
	} {
		if got := pixel(r.ts, tc.x, tc.y); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	// the lines around them, and the rest of the characters' line, are not colored
	for _, tc := range []struct {
		name string
		x, y int
	}{
		{"line before, text area", far, row(0)},
		{"line after, text area", far, row(3)},
		{"line after, gutter", 3, row(3)},
		{"rest of the characters' line", far, row(4)},
	} {
		got := pixel(r.ts, tc.x, tc.y)
		if got == rgba(line) || got == rgba(gutterStrip) || got == rgba(chars) {
			t.Errorf("%s is colored: %v", tc.name, got)
		}
	}
	noErrors(t, r.ts)
}

func TestScrollListenersAreToldOfScrolls(t *testing.T) {
	vm := NewViewModel(newTestDoc("a"), testOptions(), nil)
	var tops int
	h := vm.AddScrollListener(func() { tops++ })
	vm.SetScrollTop(40)
	vm.SetScrollTop(40) // the same place
	if tops != 1 || vm.ScrollTop() != 40 {
		t.Errorf("told %d times, top %v", tops, vm.ScrollTop())
	}
	vm.SetTextMetrics(500, 100, 50, 900, 8)
	vm.SetScrollLeft(30)
	if tops != 2 || vm.ScrollLeft() != 30 {
		t.Errorf("after a horizontal scroll told %d times, left %v", tops, vm.ScrollLeft())
	}
	vm.RemoveScrollListener(h)
	vm.SetScrollTop(10)
	if tops != 2 {
		t.Errorf("told after the listener was removed: %d", tops)
	}
}

func TestBackgroundMarksOfWrappedLinesAndZones(t *testing.T) {
	long := "aaaa bbbb cccc dddd eeee ffff gggg hhhh iiii jjjj kkkk llll mmmm nnnn oooo pppp qqqq rrrr ssss tttt"
	r := newWrapRig(t, ui.Size{Width: 300, Height: 400}, "x\n"+long+"\n\ny\n", func(o *Options) { o.RenderLineHighlight = "none" })
	rowsOfLong := rowsOf(r.vm, 1)
	if rowsOfLong < 3 {
		t.Fatalf("the long line has %d rows", rowsOfLong)
	}
	// the marked lines are the long line, an empty one, and a zone sits between them
	if _, err := r.vm.AddZone(ZoneSpec{AfterLine: 2, Rows: 2}); err != nil {
		t.Fatal(err)
	}
	line := ui.NewColor(0xFF112233)
	r.vm.SetBackgroundMarks([]BackgroundMark{{Range: buffer.Range{Start: positionOf(1, 0), End: positionOf(2, 0)}, WholeLine: true, Background: line}})
	r.settle()
	rgba := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xFF}
	row := func(n int) int { return int(float64(n)*r.lineH) + 1 }
	far := int(r.gutter) + int(r.ts.Size(gluttertest.ByType[*Editor]()).Width) - int(r.gutter) - 40
	// every row of the wrapped line, and the empty line after the zone, are colored; the zone's rows are not
	for n := 1; n <= rowsOfLong; n++ {
		if got := pixel(r.ts, far, row(n)); got != rgba {
			t.Errorf("row %d of the wrapped line: %v", n, got)
		}
	}
	zone := 1 + rowsOfLong
	for n := zone; n < zone+2; n++ {
		if got := pixel(r.ts, far, row(n)); got == rgba {
			t.Errorf("row %d of the zone is colored", n)
		}
	}
	if got := pixel(r.ts, far, row(zone+2)); got != rgba {
		t.Errorf("the empty line after the zone: %v", got)
	}
	if got := pixel(r.ts, far, row(zone+3)); got == rgba {
		t.Errorf("the line after the marks is colored")
	}
	noErrors(t, r.ts)
}
