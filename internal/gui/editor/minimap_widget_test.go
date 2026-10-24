package editor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// minimapSample is n functions of Go, eight lines each.
func minimapSample(n int) string {
	var b strings.Builder
	b.WriteString("package sample\n\n")
	for i := range n {
		fmt.Fprintf(&b, "func f%d(a, b int) int {\n\tif a > b {\n\t\treturn a + %d\n\t}\n\t// the other case %d\n\treturn \"b\" + b\n}\n\n", i, i, i)
	}
	return b.String()
}

type minimapRig struct {
	t  *testing.T
	ts *gluttertest.Tester
	vm *ViewModel
}

// newMinimapRig shows a Go document with a minimap in a window of a size.
func newMinimapRig(t *testing.T, size ui.Size, text string, tweak func(*Options)) *minimapRig {
	t.Helper()
	vm, tok := highlighted(t, text)
	opts := vm.Options()
	opts.Minimap = MinimapOptions{Enabled: true, Side: "right", Size: "proportional", ShowSlider: "mouseover", Scale: 1, MaxColumn: 120}
	opts.ScrollBeyondLastLine = true
	opts.RenderLineHighlight = "none"
	opts.OccurrencesHighlight, opts.SelectionHighlight = "off", false
	if tweak != nil {
		tweak(&opts)
	}
	vm.SetOptions(opts)
	t.Cleanup(vm.DisposeHighlights)
	ts := testutil.New(t, size)
	pumpHighlighted(t, ts, vm, kit.DarkModern(), darkModernTokens(t))
	tok.Settle()
	r := &minimapRig{t: t, ts: ts, vm: vm}
	r.settle()
	return r
}

func (r *minimapRig) settle() {
	r.t.Helper()
	for range 3 {
		r.ts.Pump(time.Millisecond)
	}
	r.ts.FlushPosted()
	for range 3 {
		r.ts.Pump(time.Millisecond)
	}
}

// painter is the painter of the minimap on screen.
func (r *minimapRig) render() *RenderEditorMinimap {
	r.t.Helper()
	return r.ts.RenderObject(gluttertest.ByType[*minimapLayer]()).(*RenderEditorMinimap)
}

func (r *minimapRig) mouse() gluttertest.GestureOpts {
	return gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse}
}

// minimapPoint is a point of the window at a height of the minimap, in its middle column.
func (r *minimapRig) minimapPoint(y float64) ui.Offset {
	info := r.vm.mm.info
	return ui.Offset{Dx: info.Left + info.Width/2, Dy: y}
}

func TestMinimapTakesRoomFromTheText(t *testing.T) {
	size := ui.Size{Width: 640, Height: 300}
	with := newMinimapRig(t, size, minimapSample(40), nil)
	info := with.vm.mm.info
	if !info.Show || info.Width < 60 || info.Left+info.Width > size.Width-kit.DefaultVerticalScrollbarSize+0.5 {
		t.Fatalf("layout %+v", info)
	}
	without := newMinimapRig(t, size, minimapSample(40), func(o *Options) { o.Minimap.Enabled = false })
	if without.vm.mm.info.Show {
		t.Error("a minimap that is off")
	}
	wWith, _ := with.vm.ViewportSize()
	wWithout, _ := without.vm.ViewportSize()
	if d := wWithout - wWith; d < info.Width-0.5 || d > info.Width+0.5 {
		t.Errorf("the text lost %v pixels, the minimap is %v wide", d, info.Width)
	}
	noErrors(t, with.ts)
}

func TestMinimapOnTheLeft(t *testing.T) {
	r := newMinimapRig(t, ui.Size{Width: 640, Height: 300}, minimapSample(40), func(o *Options) { o.Minimap.Side = "left" })
	if info := r.vm.mm.info; info.Left != 0 || !info.Show {
		t.Fatalf("layout %+v", info)
	}
	w, _ := r.vm.ViewportSize()
	right := newMinimapRig(t, ui.Size{Width: 640, Height: 300}, minimapSample(40), nil)
	if w2, _ := right.vm.ViewportSize(); w != w2 {
		t.Errorf("the text is %v wide with the minimap on the left and %v on the right", w, w2)
	}
	// a click on the first line of the text lands in the text, after the minimap
	r.vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 5})})
	g := newGeometry(r.vm, r.ts.Dispatcher().TextEngine())
	gutter := r.vm.GutterWidths(r.ts.Dispatcher().TextEngine()).Total()
	r.ts.TapAt(ui.Offset{Dx: r.vm.mm.info.Width + gutter + 3*g.charWidth + 1, Dy: 19*2 + 9}, r.mouse())
	r.ts.Pump(time.Second)
	if got := r.vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 2, Col: 3}) {
		t.Errorf("a click in the text: %v", got)
	}
	noErrors(t, r.ts)
}

func TestMinimapIsOffInLargeFilesAndWhenDisabled(t *testing.T) {
	r := newMinimapRig(t, ui.Size{Width: 640, Height: 300}, minimapSample(10), func(o *Options) { o.Minimap.Enabled = false })
	if r.vm.mm.info.Show || r.ts.Exists(gluttertest.ByType[*minimapLayer]()) {
		t.Error("a minimap that is turned off")
	}
	noErrors(t, r.ts)
}

func TestMinimapPaintsOnlyTheLinesThatChanged(t *testing.T) {
	r := newMinimapRig(t, ui.Size{Width: 640, Height: 300}, minimapSample(60), nil)
	m := r.render()
	if m.painter.drawn == 0 && m.painter.copied == 0 && m.painter.pix == nil {
		t.Fatal("nothing drawn")
	}
	// type a character at the end of a line near the top
	r.vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 4, Col: 0})})
	if err := r.vm.RunCommand("type", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	r.vm.DocumentChanged()
	r.settle()
	if m.painter.drawn != 1 || m.painter.copied < 90 {
		t.Errorf("after an edit: %d lines drawn and %d copied", m.painter.drawn, m.painter.copied)
	}
	// scrolling by a few lines draws the new ones only
	r.vm.scroller(r.vm.ScrollTop() + 19*3)
	r.settle()
	if m.painter.drawn > 4 || m.painter.copied < 80 {
		t.Errorf("after scrolling by three lines: %d lines drawn and %d copied", m.painter.drawn, m.painter.copied)
	}
	noErrors(t, r.ts)
}

func TestClickingTheMinimapRevealsTheLine(t *testing.T) {
	r := newMinimapRig(t, ui.Size{Width: 640, Height: 300}, minimapSample(100), nil)
	// the minimap has 3 pixels a line: a click at y=150 is on the line 51 of the minimap, the row 50
	r.ts.TapAt(r.minimapPoint(150), r.mouse())
	r.ts.Pump(time.Second)
	proj := r.vm.Projection()
	_, vh := r.vm.ViewportSize()
	// the row clicked is in the middle of the editor
	row := proj.RowAt(r.vm.ScrollTop() + vh/2)
	want := 150 / 3
	if row < want-1 || row > want+1 {
		t.Errorf("the editor is centered on the row %d, want %d (scroll top %v)", row, want, r.vm.ScrollTop())
	}
	noErrors(t, r.ts)
}

func TestDraggingTheMinimapSlider(t *testing.T) {
	r := newMinimapRig(t, ui.Size{Width: 640, Height: 300}, minimapSample(100), nil)
	l := r.vm.mm.layout
	if !l.SliderNeeded {
		t.Fatal("no slider")
	}
	start := r.vm.ScrollTop()
	y := l.SliderTop + l.SliderHeight/2
	g := r.ts.StartGesture(r.minimapPoint(y), r.mouse())
	g.MoveTo(r.minimapPoint(y + 30))
	r.settle()
	if !r.vm.mm.active {
		t.Error("the slider is not active while dragged")
	}
	if got, want := r.vm.ScrollTop(), l.desiredScrollTopFromDelta(30); got != want {
		t.Errorf("scroll top %v after dragging by 30 pixels, want %v", got, want)
	}
	if r.vm.ScrollTop() <= start {
		t.Errorf("did not scroll down: %v", r.vm.ScrollTop())
	}
	g.Up()
	r.settle()
	if r.vm.mm.active {
		t.Error("the slider is still active after the pointer was released")
	}
	noErrors(t, r.ts)
}

func TestMinimapHoverShowsTheSlider(t *testing.T) {
	r := newMinimapRig(t, ui.Size{Width: 640, Height: 300}, minimapSample(100), nil)
	if r.vm.mm.hover {
		t.Fatal("hover")
	}
	r.ts.Hover(r.minimapPoint(10))
	r.settle()
	if !r.vm.mm.hover {
		t.Error("the pointer over the minimap is not hovering")
	}
	r.ts.Hover(ui.Offset{Dx: 100, Dy: 100})
	r.settle()
	if r.vm.mm.hover {
		t.Error("the pointer left the minimap")
	}
	noErrors(t, r.ts)
}

func TestWheelOverTheMinimapScrollsTheEditor(t *testing.T) {
	r := newMinimapRig(t, ui.Size{Width: 640, Height: 300}, minimapSample(100), nil)
	before := r.vm.ScrollTop()
	r.ts.ScrollAt(r.minimapPoint(100), ui.Offset{Dy: wheelNotch * 3})
	r.ts.Pump(time.Second)
	if r.vm.ScrollTop() <= before {
		t.Errorf("the wheel over the minimap did not scroll: %v -> %v", before, r.vm.ScrollTop())
	}
}

func TestGoldenMinimap(t *testing.T) {
	r := newMinimapRig(t, ui.Size{Width: 640, Height: 300}, minimapSample(40), func(o *Options) { o.Minimap.ShowSlider = "always" })
	r.vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 44, Col: 1}, Active: buffer.Position{Line: 52, Col: 8}}})
	r.vm.SelectionsChanged()
	r.vm.scroller(19 * 40)
	r.settle()
	noErrors(t, r.ts)
	testutil.MatchesGolden(r.ts, "minimap")
}

func TestGoldenMinimapLeftWithFind(t *testing.T) {
	r := newMinimapRig(t, ui.Size{Width: 640, Height: 300}, minimapSample(40), func(o *Options) { o.Minimap.Side, o.Minimap.ShowSlider = "left", "always" })
	if err := r.vm.RunCommand("editor.actions.findWithArgs", map[string]any{"searchString": "return"}); err != nil {
		t.Fatal(err)
	}
	r.settle()
	noErrors(t, r.ts)
	testutil.MatchesGolden(r.ts, "minimap_left_find")
}

func TestMinimapIsOffInLargeFiles(t *testing.T) {
	doc := document.FromRead(document.FileURI("/big.txt"), fileio.ReadResult{Text: minimapSample(20), LineEnding: fileio.LF, LargeFile: true}, document.NewOptions())
	opts := goOptions()
	opts.Minimap = MinimapOptions{Enabled: true, Side: "right", Size: "proportional", ShowSlider: "mouseover", Scale: 1, MaxColumn: 120}
	vm := NewViewModel(doc, opts, nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 300})
	if vm.mm.info.Show || ts.Exists(gluttertest.ByType[*minimapLayer]()) {
		t.Error("a minimap in a large file")
	}
	w, _ := vm.ViewportSize()
	other := NewViewModel(newTestDoc(minimapSample(20)), goOptions(), nil)
	pumpEditor(t, other, ui.Size{Width: 640, Height: 300})
	if w2, _ := other.ViewportSize(); w != w2 {
		t.Errorf("the text of a large file is %v wide, %v with no minimap at all", w, w2)
	}
	noErrors(t, ts)
}
