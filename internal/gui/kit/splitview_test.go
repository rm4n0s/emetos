package kit

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// paneColors are the colors of the panes of the tests, so that a pixel tells the pane.
var paneColors = []color.NRGBA{
	{R: 0x60, G: 0x30, B: 0x30, A: 0xFF}, {R: 0x30, G: 0x60, B: 0x30, A: 0xFF}, {R: 0x30, G: 0x30, B: 0x60, A: 0xFF}, {R: 0x60, G: 0x60, B: 0x30, A: 0xFF},
}

func paneKey(i int) foundation.Key { return foundation.NewKey(fmt.Sprintf("pane%d", i)) }

// pane makes a pane of a color with a key to find it.
func pane(i int, p SplitPane) SplitPane {
	c := paneColors[i%len(paneColors)]
	p.Child = widgets.NewColoredBox(ui.NewColor(0xFF000000|uint32(c.R)<<16|uint32(c.G)<<8|uint32(c.B)), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Key: paneKey(i)}))
	return p
}

// splitWindow is a window with a split view.
type splitWindow struct {
	ts      *gluttertest.Tester
	changes [][]float64
	resets  []int
}

// pumpSplit shows a split view in a window of a size.
func pumpSplit(t *testing.T, size ui.Size, panes []SplitPane, opts SplitViewOpts) *splitWindow {
	t.Helper()
	w := &splitWindow{ts: gluttertest.New(t, gluttertest.Opts{Size: size})}
	opts.OnDidChange = func(sizes []float64) { w.changes = append(w.changes, sizes) }
	w.show(panes, opts)
	return w
}

func (w *splitWindow) show(panes []SplitPane, opts SplitViewOpts) {
	for i := range panes {
		if panes[i].Child == nil {
			panes[i] = pane(i, panes[i])
		}
	}
	if opts.OnDidChange == nil {
		opts.OnDidChange = func(sizes []float64) { w.changes = append(w.changes, sizes) }
	}
	w.ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR, NewTheme(DarkModern(), NewSplitView(panes, opts))))
	w.ts.Pump(time.Millisecond)
}

// extent is the size of a pane along an axis.
func (w *splitWindow) sizes(horizontal bool, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		if !w.ts.Exists(gluttertest.ByKey(paneKey(i))) {
			continue // a hidden pane is not laid out
		}
		s := w.ts.Size(gluttertest.ByKey(paneKey(i)))
		out[i] = s.Height
		if horizontal {
			out[i] = s.Width
		}
	}
	return out
}

// sash is the center of the sash at an index.
func (w *splitWindow) sash(i int) ui.Offset {
	return w.ts.Center(gluttertest.At(gluttertest.ByType[*Sash](), i))
}

func (w *splitWindow) check(t *testing.T, horizontal bool, want ...float64) {
	t.Helper()
	if got := w.sizes(horizontal, len(want)); !slices.Equal(got, want) {
		t.Errorf("pane sizes %v, want %v", got, want)
	}
	if errs := w.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// three panes in a row: a side bar, an editor that takes what is left, and a panel.
func sideEditorPanel() []SplitPane {
	return []SplitPane{
		{Size: 100, MinSize: 50},
		{Size: 200, MinSize: 80, Priority: LayoutHigh},
		{Size: 100, MinSize: 50, Priority: LayoutLow},
	}
}

var orientations = []struct {
	name        string
	orientation SplitOrientation
	horizontal  bool
	window      ui.Size
	along       func(dx float64) ui.Offset
}{
	{"horizontal", SplitHorizontal, true, ui.Size{Width: 400, Height: 120}, func(d float64) ui.Offset { return ui.Offset{Dx: d} }},
	{"vertical", SplitVertical, false, ui.Size{Width: 120, Height: 400}, func(d float64) ui.Offset { return ui.Offset{Dy: d} }},
}

func TestSplitViewLaysOutPanes(t *testing.T) {
	for _, o := range orientations {
		t.Run(o.name, func(t *testing.T) {
			w := pumpSplit(t, o.window, sideEditorPanel(), SplitViewOpts{Orientation: o.orientation})
			w.check(t, o.horizontal, 100, 200, 100)
			// The panes follow each other along the axis and fill the other.
			if got, want := w.ts.TopLeft(gluttertest.ByKey(paneKey(1))), o.along(100); got != want {
				t.Errorf("second pane at %v, want %v", got, want)
			}
			if s := w.ts.Size(gluttertest.ByKey(paneKey(0))); (o.horizontal && s.Height != 120) || (!o.horizontal && s.Width != 120) {
				t.Errorf("a pane fills the other axis: %v", s)
			}
			if got := w.ts.Count(gluttertest.ByType[*Sash]()); got != 2 {
				t.Errorf("%d sashes, want 2", got)
			}
		})
	}
}

func TestSplitViewFitsThePanesToTheWindow(t *testing.T) {
	// The sizes add up to 300 in a window of 400: the editor, of the highest priority, takes the rest.
	w := pumpSplit(t, ui.Size{Width: 400, Height: 100}, sideEditorPanel(), SplitViewOpts{Orientation: SplitHorizontal})
	w.check(t, true, 100, 200, 100)
	w = pumpSplit(t, ui.Size{Width: 500, Height: 100}, sideEditorPanel(), SplitViewOpts{Orientation: SplitHorizontal})
	w.check(t, true, 100, 300, 100)
	if len(w.changes) != 1 || !slices.Equal(w.changes[0], []float64{100, 300, 100}) {
		t.Errorf("the sizes the layout settled on are reported: %v", w.changes)
	}
}

func TestSplitViewSashDrag(t *testing.T) {
	for _, o := range orientations {
		cases := []struct {
			name  string
			sash  int
			moves []float64
			want  []float64
		}{
			{"moves the boundary", 0, []float64{30}, []float64{130, 170, 100}},
			{"follows the pointer back", 0, []float64{60, 20}, []float64{120, 180, 100}},
			{"the second sash", 1, []float64{-40}, []float64{100, 160, 140}},
			{"stops at the minimum of the panes after the sash", 0, []float64{400}, []float64{270, 80, 50}},
			{"and at the minimum of the panes before it", 1, []float64{-400}, []float64{50, 80, 270}},
			{"the next pane gives way when the neighbour cannot", 0, []float64{150}, []float64{250, 80, 70}},
		}
		for _, tc := range cases {
			t.Run(o.name+" "+tc.name, func(t *testing.T) {
				w := pumpSplit(t, o.window, sideEditorPanel(), SplitViewOpts{Orientation: o.orientation})
				at := w.sash(tc.sash)
				g := w.ts.StartGesture(at)
				w.ts.Pump(time.Millisecond)
				for _, m := range tc.moves {
					g.MoveTo(at.Add(o.along(m)))
					w.ts.Pump(time.Millisecond)
				}
				w.check(t, o.horizontal, tc.want...)
				if len(w.changes) != 0 {
					t.Errorf("nothing is reported during the drag: %v", w.changes)
				}
				g.Up()
				w.ts.Pump(time.Millisecond)
				w.ts.Pump(time.Millisecond)
				w.check(t, o.horizontal, tc.want...)
				if len(w.changes) != 1 || !slices.Equal(w.changes[0], tc.want) {
					t.Errorf("the drag is reported once, when it ends: %v", w.changes)
				}
			})
		}
	}
}

func TestSplitViewMaximumSizes(t *testing.T) {
	panes := []SplitPane{{Size: 100, MaxSize: 130}, {Size: 200, Priority: LayoutHigh}}
	w := pumpSplit(t, ui.Size{Width: 300, Height: 50}, panes, SplitViewOpts{Orientation: SplitHorizontal})
	g := w.ts.StartGesture(w.sash(0))
	g.MoveBy(ui.Offset{Dx: 100})
	w.ts.Pump(time.Millisecond)
	w.check(t, true, 130, 170)
	g.Up()
}

func TestSplitViewSashCursorsAndStates(t *testing.T) {
	cursor := func(w *splitWindow) string {
		c := w.ts.ActivatedCursors()
		if len(c) == 0 {
			return ""
		}
		return c[len(c)-1].Kind
	}
	cases := []struct {
		name        string
		orientation SplitOrientation
		panes       []SplitPane
		window      ui.Size
		want        string
	}{
		{"vertical sash, both ways", SplitHorizontal, sideEditorPanel(), ui.Size{Width: 400, Height: 100}, "resizeLeftRight"},
		{"horizontal sash, both ways", SplitVertical, sideEditorPanel(), ui.Size{Width: 100, Height: 400}, "resizeUpDown"},
		{"a pane at its minimum can only grow", SplitHorizontal, []SplitPane{{Size: 100, MinSize: 100}, {Size: 200, Priority: LayoutHigh}}, ui.Size{Width: 300, Height: 100}, "resizeRight"},
		{"a pane at its maximum can only shrink", SplitHorizontal, []SplitPane{{Size: 100, MaxSize: 100}, {Size: 200, Priority: LayoutHigh}}, ui.Size{Width: 300, Height: 100}, "resizeLeft"},
		{"above at the minimum", SplitVertical, []SplitPane{{Size: 100, MinSize: 100}, {Size: 200, Priority: LayoutHigh}}, ui.Size{Width: 100, Height: 300}, "resizeDown"},
		{"above at the maximum", SplitVertical, []SplitPane{{Size: 100, MaxSize: 100}, {Size: 200, Priority: LayoutHigh}}, ui.Size{Width: 100, Height: 300}, "resizeUp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := pumpSplit(t, tc.window, tc.panes, SplitViewOpts{Orientation: tc.orientation})
			w.ts.Hover(w.sash(0))
			w.ts.Pump(time.Millisecond)
			if got := cursor(w); got != tc.want {
				t.Errorf("cursor %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("a sash that cannot move shows no resize cursor and ignores the pointer", func(t *testing.T) {
		panes := []SplitPane{{Size: 100, MinSize: 100, MaxSize: 100}, {Size: 200, MinSize: 200, MaxSize: 200}}
		w := pumpSplit(t, ui.Size{Width: 300, Height: 100}, panes, SplitViewOpts{Orientation: SplitHorizontal})
		w.ts.Hover(w.sash(0))
		w.ts.Pump(time.Millisecond)
		if got := cursor(w); got == "resizeLeftRight" || got == "resizeRight" || got == "resizeLeft" {
			t.Errorf("cursor %q over a sash that is disabled", got)
		}
		g := w.ts.StartGesture(w.sash(0))
		g.MoveBy(ui.Offset{Dx: 40})
		w.ts.Pump(time.Millisecond)
		w.check(t, true, 100, 200)
		g.Up()
	})

	t.Run("away from the sash the cursor is not a resize cursor", func(t *testing.T) {
		w := pumpSplit(t, ui.Size{Width: 400, Height: 100}, sideEditorPanel(), SplitViewOpts{Orientation: SplitHorizontal})
		w.ts.Hover(w.sash(0))
		w.ts.Pump(time.Millisecond)
		w.ts.Hover(ui.Offset{Dx: 250, Dy: 50})
		w.ts.Pump(time.Millisecond)
		if got := cursor(w); got == "resizeLeftRight" {
			t.Errorf("cursor %q away from the sash", got)
		}
	})
}

func TestSashHoverColor(t *testing.T) {
	hover := DarkModern().SashHoverBorder
	toColor := func(c color.NRGBA) ui.Color {
		return ui.NewColor(0xFF000000 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B))
	}
	_ = toColor
	isHover := func(w *splitWindow, at ui.Offset) bool {
		c := pixelAt(w.ts, int(at.Dx), int(at.Dy))
		return c.R == uint8(hover.Red()) && c.G == uint8(hover.Green()) && c.B == uint8(hover.Blue())
	}
	w := pumpSplit(t, ui.Size{Width: 400, Height: 100}, sideEditorPanel(), SplitViewOpts{Orientation: SplitHorizontal})
	at := w.sash(0)
	if isHover(w, at) {
		t.Fatal("a sash nobody is on is not highlighted")
	}
	w.ts.Hover(at)
	w.ts.Pump(DefaultSashHoverDelay - 50*time.Millisecond)
	if isHover(w, at) {
		t.Error("the pointer has to rest for 300 ms")
	}
	w.ts.Pump(100 * time.Millisecond)
	if !isHover(w, at) {
		t.Logf("pixel %v hover %v", pixelAt(w.ts, int(at.Dx), int(at.Dy)), hover)
		t.Error("the sash shows the hover color after the delay")
	}
	// The line is as wide as the sash.
	if !isHover(w, at.Add(ui.Offset{Dx: 1.5})) || isHover(w, at.Add(ui.Offset{Dx: 3})) {
		t.Error("the highlight is 4 px wide")
	}
	w.ts.Hover(ui.Offset{Dx: 300, Dy: 50})
	w.ts.Pump(time.Millisecond)
	if isHover(w, at) {
		t.Error("leaving the sash takes the highlight away")
	}
	// Passing over it does not highlight it.
	w.ts.Hover(at)
	w.ts.Pump(100 * time.Millisecond)
	w.ts.Hover(ui.Offset{Dx: 300, Dy: 50})
	w.ts.Pump(DefaultSashHoverDelay + 100*time.Millisecond)
	if isHover(w, at) {
		t.Error("the timer of a pointer that left does not highlight")
	}
	// A sash that is pressed shows it at once.
	g := w.ts.StartGesture(at, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	w.ts.Pump(time.Millisecond)
	if !isHover(w, at) {
		t.Error("a sash that is dragged is highlighted at once")
	}
	g.Up()
	w.ts.Pump(time.Millisecond)
}

func TestSplitViewDoubleClickResets(t *testing.T) {
	panes := func() []SplitPane {
		p := sideEditorPanel()
		p[0].PreferredSize = 120
		p[2].PreferredSize = 150
		return p
	}
	cases := []struct {
		name string
		sash int
		want []float64
	}{
		{"the pane before the sash gets its preferred size, the editor takes the difference", 0, []float64{120, 180, 100}},
		{"the pane after the sash does", 1, []float64{90, 160, 150}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := pumpSplit(t, ui.Size{Width: 400, Height: 100}, panes(), SplitViewOpts{Orientation: SplitHorizontal})
			g := w.ts.StartGesture(w.sash(0))
			g.MoveBy(ui.Offset{Dx: -40}) // 60, 240, 100
			g.Up()
			w.ts.Pump(time.Millisecond)
			if tc.sash == 1 {
				// Drag the second sash too, so that the panel is not at its preferred size.
				g = w.ts.StartGesture(w.sash(1))
				g.MoveBy(ui.Offset{Dx: 30}) // 60, 270, 70
				g.Up()
			}
			w.ts.Pump(time.Millisecond)
			w.changes = nil
			at := w.sash(tc.sash)
			w.ts.TapAt(at)
			w.ts.Pump(50 * time.Millisecond)
			w.ts.TapAt(at)
			w.ts.Pump(time.Millisecond)
			w.ts.Pump(time.Millisecond)
			want := tc.want
			if tc.sash == 1 {
				want = []float64{60, 190, 150}
			}
			w.check(t, true, want...)
			if len(w.changes) != 1 || !slices.Equal(w.changes[0], want) {
				t.Errorf("the reset is reported: %v", w.changes)
			}
		})
	}

	t.Run("two clicks far apart in time are not a double click", func(t *testing.T) {
		w := pumpSplit(t, ui.Size{Width: 400, Height: 100}, panes(), SplitViewOpts{Orientation: SplitHorizontal})
		g := w.ts.StartGesture(w.sash(0))
		g.MoveBy(ui.Offset{Dx: -40})
		g.Up()
		w.ts.Pump(time.Second)
		at := w.sash(0)
		w.ts.TapAt(at)
		w.ts.Pump(time.Second)
		w.ts.TapAt(at)
		w.ts.Pump(time.Millisecond)
		w.check(t, true, 60, 240, 100)
	})

	t.Run("OnDidSashReset replaces the reset", func(t *testing.T) {
		w := &splitWindow{ts: gluttertest.New(t, gluttertest.Opts{Size: ui.Size{Width: 400, Height: 100}})}
		w.show(panes(), SplitViewOpts{Orientation: SplitHorizontal, OnDidSashReset: func(i int) { w.resets = append(w.resets, i) }})
		g := w.ts.StartGesture(w.sash(1))
		g.MoveBy(ui.Offset{Dx: 30})
		g.Up()
		w.ts.Pump(time.Millisecond)
		at := w.sash(1)
		w.ts.TapAt(at)
		w.ts.Pump(50 * time.Millisecond)
		w.ts.TapAt(at)
		w.ts.Pump(time.Millisecond)
		if !slices.Equal(w.resets, []int{1}) {
			t.Errorf("resets %v, want [1]", w.resets)
		}
		w.check(t, true, 100, 230, 70)
	})

	t.Run("without preferred sizes the panes are shared equally", func(t *testing.T) {
		w := pumpSplit(t, ui.Size{Width: 300, Height: 100}, []SplitPane{{Size: 100}, {Size: 100}, {Size: 100}}, SplitViewOpts{Orientation: SplitHorizontal})
		g := w.ts.StartGesture(w.sash(0))
		g.MoveBy(ui.Offset{Dx: 50})
		g.Up()
		w.ts.Pump(time.Millisecond)
		at := w.sash(0)
		w.ts.TapAt(at)
		w.ts.Pump(50 * time.Millisecond)
		w.ts.TapAt(at)
		w.ts.Pump(time.Millisecond)
		w.check(t, true, 100, 100, 100)
	})
}

func TestSplitViewWindowResize(t *testing.T) {
	cases := []struct {
		name         string
		proportional bool
		sizes        []float64
		want         [][]float64
	}{
		{"the editor absorbs", false, []float64{600, 500, 300, 700}, [][]float64{{100, 400, 100}, {100, 300, 100}, {100, 100, 100}, {100, 500, 100}}},
		{"at its minimum the others give way, the panel last", false, []float64{240, 200}, [][]float64{{60, 80, 100}, {50, 80, 70}}},
		{"proportional layout keeps the shares", true, []float64{800, 200}, [][]float64{{200, 400, 200}, {50, 100, 50}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			panes := []SplitPane{{Size: 100, MinSize: 50}, {Size: 200, MinSize: 80, Priority: LayoutHigh}, {Size: 100, MinSize: 50, Priority: LayoutLow}}
			if tc.proportional {
				panes[1].Priority = LayoutNormal
			}
			w := pumpSplit(t, ui.Size{Width: 400, Height: 100}, panes, SplitViewOpts{Orientation: SplitHorizontal, ProportionalLayout: tc.proportional})
			for i, width := range tc.sizes {
				w.ts.SetViewSize(ui.Size{Width: width, Height: 100})
				w.ts.Pump(time.Millisecond)
				w.ts.Pump(time.Millisecond)
				w.check(t, true, tc.want[i]...)
				if got := w.changes[len(w.changes)-1]; !slices.Equal(got, tc.want[i]) {
					t.Errorf("the new sizes are reported: %v, want %v", got, tc.want[i])
				}
			}
		})
	}
}

func TestSplitViewController(t *testing.T) {
	ctrl := NewSplitViewController()
	if ctrl.Sizes() != nil || ctrl.IsVisible(0) || ctrl.ContentSize() != 0 {
		t.Error("a controller without a split view has nothing")
	}
	ctrl.SetSize(0, 10) // does nothing
	w := pumpSplit(t, ui.Size{Width: 400, Height: 100}, sideEditorPanel(), SplitViewOpts{Orientation: SplitHorizontal, Controller: ctrl})
	if got := ctrl.Sizes(); !slices.Equal(got, []float64{100, 200, 100}) {
		t.Fatalf("sizes %v", got)
	}
	steps := []struct {
		name string
		do   func()
		want []float64
	}{
		{"set a size", func() { ctrl.SetSize(0, 160) }, []float64{160, 140, 100}},
		{"a size within limits", func() { ctrl.SetSize(0, 10) }, []float64{50, 250, 100}},
		{"hide", func() { ctrl.SetVisible(2, false) }, []float64{50, 350, 0}},
		{"show", func() { ctrl.SetVisible(2, true) }, []float64{50, 250, 100}},
		{"distribute", ctrl.Distribute, []float64{133, 134, 133}},
	}
	for _, s := range steps {
		w.changes = nil
		s.do()
		w.ts.Pump(time.Millisecond)
		w.ts.Pump(time.Millisecond)
		w.check(t, true, s.want...)
		if got := ctrl.Sizes(); !slices.Equal(got, s.want) {
			t.Errorf("%s: controller sizes %v, want %v", s.name, got, s.want)
		}
		if len(w.changes) != 1 || !slices.Equal(w.changes[0], s.want) {
			t.Errorf("%s: reported %v", s.name, w.changes)
		}
	}
	ctrl.SetVisible(2, false)
	w.ts.Pump(time.Millisecond)
	if size, ok := ctrl.CachedVisibleSize(2); !ok || size != 133 {
		t.Errorf("cached %v %v", size, ok)
	}
	if ctrl.IsVisible(2) || !ctrl.IsVisible(0) {
		t.Error("visibility")
	}
	ctrl.SetSize(1, 1000)
	w.ts.Pump(time.Millisecond)
	if !ctrl.IsExpanded(1) {
		t.Errorf("the others are at their minimum: %v", ctrl.Sizes())
	}
	if ctrl.ContentSize() != 400 {
		t.Errorf("content size %v", ctrl.ContentSize())
	}
	// Taking the split view away lets go of the controller.
	w.ts.PumpWidget(widgets.NewSizedBox(nil))
	if ctrl.Sizes() != nil {
		t.Error("the controller of a split view that is gone has nothing")
	}
	if errs := w.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestSplitViewFollowsNewPanes(t *testing.T) {
	w := pumpSplit(t, ui.Size{Width: 400, Height: 100}, sideEditorPanel(), SplitViewOpts{Orientation: SplitHorizontal})
	w.check(t, true, 100, 200, 100)
	g := w.ts.StartGesture(w.sash(0))
	g.MoveBy(ui.Offset{Dx: 40})
	g.Up()
	w.ts.Pump(time.Millisecond)
	w.check(t, true, 140, 160, 100)

	t.Run("a pane hides and shows", func(t *testing.T) {
		p := sideEditorPanel()
		p[0].Hidden = true
		w.show(p, SplitViewOpts{Orientation: SplitHorizontal})
		w.check(t, true, 0, 300, 100)
		if w.ts.Exists(gluttertest.ByKey(paneKey(0))) || !w.ts.Exists(gluttertest.ByKey(paneKey(0), gluttertest.FindOpts{IncludeOffstage: true})) {
			t.Error("a hidden pane stays in the tree, offstage")
		}
		w.show(sideEditorPanel(), SplitViewOpts{Orientation: SplitHorizontal})
		w.check(t, true, 140, 160, 100)
	})

	t.Run("new limits are kept", func(t *testing.T) {
		p := sideEditorPanel()
		p[0].MinSize = 200
		w.show(p, SplitViewOpts{Orientation: SplitHorizontal})
		w.check(t, true, 200, 100, 100)
	})

	t.Run("another number of panes starts over", func(t *testing.T) {
		w.show([]SplitPane{{Size: 150}, {Size: 250, Priority: LayoutHigh}}, SplitViewOpts{Orientation: SplitHorizontal})
		w.check(t, true, 150, 250)
		if w.ts.Count(gluttertest.ByType[*Sash]()) != 1 {
			t.Error("one sash")
		}
	})
}

func TestSplitViewSnap(t *testing.T) {
	panes := []SplitPane{{Size: 200, MinSize: 100, Snap: true, Priority: LayoutLow}, {Size: 400, Priority: LayoutHigh}}
	w := pumpSplit(t, ui.Size{Width: 600, Height: 100}, panes, SplitViewOpts{Orientation: SplitHorizontal})
	at := w.sash(0)
	g := w.ts.StartGesture(at)
	g.MoveTo(at.Add(ui.Offset{Dx: -120}))
	w.ts.Pump(time.Millisecond)
	w.check(t, true, 100, 500)
	g.MoveTo(at.Add(ui.Offset{Dx: -170}))
	w.ts.Pump(time.Millisecond)
	w.check(t, true, 0, 600)
	g.MoveTo(at.Add(ui.Offset{Dx: -130}))
	w.ts.Pump(time.Millisecond)
	w.check(t, true, 100, 500)
	g.MoveTo(at.Add(ui.Offset{Dx: -170}))
	g.Up()
	w.ts.Pump(time.Millisecond)
	w.ts.Pump(time.Millisecond)
	w.check(t, true, 0, 600)
	if len(w.changes) != 1 || !slices.Equal(w.changes[0], []float64{0, 600}) {
		t.Errorf("reported %v", w.changes)
	}

	// The hidden side bar is opened again by dragging the sash that is left at its edge, and a
	// double click on it does not reset anything.
	at = w.sash(0)
	w.ts.TapAt(at)
	w.ts.Pump(50 * time.Millisecond)
	w.ts.TapAt(at)
	w.ts.Pump(time.Millisecond)
	w.check(t, true, 0, 600)
	g = w.ts.StartGesture(at)
	g.MoveBy(ui.Offset{Dx: 150})
	g.Up()
	w.ts.Pump(time.Millisecond)
	w.check(t, true, 150, 450)
}

func TestSplitViewSeparatorBorder(t *testing.T) {
	border := ui.NewColor(0xFFFF00FF)
	w := pumpSplit(t, ui.Size{Width: 400, Height: 100}, sideEditorPanel(), SplitViewOpts{Orientation: SplitHorizontal, SeparatorBorder: border})
	// A line at the start of the second and of the third pane, 1 px wide.
	for _, x := range []int{100, 300} {
		if c := pixelAt(w.ts, x, 50); c.R != 0xFF || c.G != 0 || c.B != 0xFF {
			t.Errorf("no separator at %d: %v", x, c)
		}
		if c := pixelAt(w.ts, x+1, 50); c.R == 0xFF && c.G == 0 {
			t.Errorf("the separator is 1 px wide: %v", c)
		}
	}
	if c := pixelAt(w.ts, 0, 50); c.R == 0xFF && c.G == 0 {
		t.Errorf("none before the first pane: %v", c)
	}
}

func TestSplitViewOverflowScrolls(t *testing.T) {
	// Three panes of at least 200 in 400: the content is 600 and scrolls.
	panes := []SplitPane{{Size: 200, MinSize: 200}, {Size: 200, MinSize: 200}, {Size: 200, MinSize: 200}}
	for _, o := range orientations {
		t.Run(o.name, func(t *testing.T) {
			window := ui.Size{Width: 400, Height: 100}
			wheel := ui.Offset{Dx: 53} // a wheel that scrolls sideways
			if !o.horizontal {
				window = ui.Size{Width: 100, Height: 400}
				wheel = ui.Offset{Dy: 53}
			}
			w := pumpSplit(t, window, slices.Clone(panes), SplitViewOpts{Orientation: o.orientation})
			w.check(t, o.horizontal, 200, 200, 200)
			if got := w.ts.TopLeft(gluttertest.ByKey(paneKey(0))); got != (ui.Offset{}) {
				t.Errorf("not scrolled: %v", got)
			}
			w.ts.ScrollAt(ui.Offset{Dx: 50, Dy: 50}, wheel)
			w.ts.Pump(time.Millisecond)
			w.ts.Pump(scrollSmoothDuration + 50*time.Millisecond)
			w.ts.Pump(time.Millisecond) // the frame the new position asks for
			want := o.along(-50)
			if got := w.ts.TopLeft(gluttertest.ByKey(paneKey(0))); got != want {
				t.Errorf("one notch scrolls 50: first pane at %v, want %v", got, want)
			}
			// Further than the end is the end.
			for range 20 {
				w.ts.ScrollAt(ui.Offset{Dx: 50, Dy: 50}, wheel)
				w.ts.Pump(scrollSmoothDuration + 50*time.Millisecond)
				w.ts.Pump(time.Millisecond)
			}
			if got := w.ts.TopLeft(gluttertest.ByKey(paneKey(0))); got != o.along(-200) {
				t.Errorf("scrolled to the end: %v", got)
			}
			w.check(t, o.horizontal, 200, 200, 200)
		})
	}
}

func TestSplitViewDragAcrossAWiderWindowAndBack(t *testing.T) {
	// The size of the split view changes during a drag: the drag goes on from the sizes it
	// started with.
	w := pumpSplit(t, ui.Size{Width: 400, Height: 100}, sideEditorPanel(), SplitViewOpts{Orientation: SplitHorizontal})
	at := w.sash(0)
	g := w.ts.StartGesture(at)
	g.MoveBy(ui.Offset{Dx: 20})
	w.ts.Pump(time.Millisecond)
	w.check(t, true, 120, 180, 100)
	g.MoveBy(ui.Offset{Dx: -20})
	w.ts.Pump(time.Millisecond)
	w.check(t, true, 100, 200, 100)
	g.Up()
}

func TestSplitViewKeepsTheCursorWhileDragging(t *testing.T) {
	last := func(w *splitWindow) string {
		c := w.ts.ActivatedCursors()
		if len(c) == 0 {
			return ""
		}
		return c[len(c)-1].Kind
	}
	panes := []SplitPane{{Size: 100, MinSize: 50}, {Size: 200, MinSize: 80, Priority: LayoutHigh}}
	w := pumpSplit(t, ui.Size{Width: 300, Height: 100}, panes, SplitViewOpts{Orientation: SplitHorizontal})
	at := w.sash(0)
	g := w.ts.StartGesture(at, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	w.ts.Pump(time.Millisecond)
	// The pointer outruns the sash, which stops at the minimum of the other pane (220): it can only go back.
	g.MoveTo(ui.Offset{Dx: 290, Dy: 50})
	w.ts.Pump(time.Millisecond)
	w.check(t, true, 220, 80)
	if got := last(w); got != "resizeLeft" {
		t.Errorf("the cursor while the sash is held back: %q, want resizeLeft", got)
	}
	g.Up()
	w.ts.Pump(time.Millisecond)
	w.ts.Hover(ui.Offset{Dx: 10, Dy: 50})
	w.ts.Pump(time.Millisecond)
	if got := last(w); strings.HasPrefix(got, "resize") {
		t.Errorf("the cursor is the ordinary one after the drag: %q", got)
	}
}
