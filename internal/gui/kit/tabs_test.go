package kit

import (
	"image/color"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// tabsRecorder records the calls of the callbacks of a strip.
type tabsRecorder struct {
	activated, closed, pinned, unpinned []int
}

func (r *tabsRecorder) opts(o TabsOpts) TabsOpts {
	o.OnActivate = func(i int) { r.activated = append(r.activated, i) }
	o.OnClose = func(i int) { r.closed = append(r.closed, i) }
	o.OnPin = func(i int) { r.pinned = append(r.pinned, i) }
	o.OnUnpin = func(i int) { r.unpinned = append(r.unpinned, i) }
	return o
}

func pumpTabs(t *testing.T, colors Colors, width float64, items []TabItem, opts TabsOpts) *gluttertest.Tester {
	t.Helper()
	strip := widgets.NewSizedBox(NewTabs(items, opts), widgets.SizedBoxOpts{Width: foundation.Some(width)})
	ts := pumpThemed(t, colors, ui.Size{Width: width, Height: 80}, strip)
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	return ts
}

func names(ns ...string) []TabItem {
	out := make([]TabItem, len(ns))
	for i, n := range ns {
		out[i] = TabItem{Key: n, Name: n, Title: "/w/" + n}
	}
	return out
}

func pixel(ts *gluttertest.Tester, x, y int) color.RGBA {
	return color.RGBAModel.Convert(ts.Screenshot().At(x, y)).(color.RGBA)
}

func rgb(c ui.Color) color.RGBA {
	return color.RGBA{R: uint8(c.Red()), G: uint8(c.Green()), B: uint8(c.Blue()), A: 255}
}

func tabCenter(ts *gluttertest.Tester, name string) ui.Offset {
	return ts.Center(gluttertest.ByText(name))
}

var mouse = gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse}

func TestTabsLayout(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []TabItem
		opts  TabsOpts
		// widths are the widths of the tabs, left to right; 0 leaves one unchecked
		min []float64
	}{
		{"a short name fills 120 px", names("a.go", "b.go"), TabsOpts{Active: 0}, []float64{120, 120}},
		{"a long name is wider", names("a-very-long-file-name-for-a-tab.go"), TabsOpts{Active: 0}, []float64{121}},
		{"a compact pinned tab is 38 px", []TabItem{{Key: "a", Name: "a.go", Sticky: true}, {Key: "b", Name: "b.go"}}, TabsOpts{Active: 1, PinnedSizing: PinnedCompact}, []float64{38, 120}},
		{"a shrunk pinned tab is 80 px", []TabItem{{Key: "a", Name: "a-longer-name.go", Sticky: true}, {Key: "b", Name: "b.go"}}, TabsOpts{Active: 1, PinnedSizing: PinnedShrink}, []float64{80, 120}},
		{"a normal pinned tab is like the others", []TabItem{{Key: "a", Name: "a.go", Sticky: true}}, TabsOpts{Active: 0}, []float64{120}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := pumpTabs(t, DarkModern(), 900, tc.items, tc.opts)
			if h := ts.Size(gluttertest.ByType[*Tabs]()).Height; h != TabHeight {
				t.Errorf("the strip is %v high, want %v", h, TabHeight)
			}
			x := 0.0
			for i, want := range tc.min {
				got := tabWidthAt(ts, i)
				if want == 121 {
					if got <= tabMinWidth {
						t.Errorf("tab %d is %v wide, want more than 120", i, got)
					}
				} else if got != want {
					t.Errorf("tab %d is %v wide, want %v", i, got, want)
				}
				x += got
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

// tabWidthAt is the width of the i-th tab of the strip.
func tabWidthAt(ts *gluttertest.Tester, i int) float64 {
	return ts.Size(gluttertest.At(gluttertest.ByType[*tabWidget](), i)).Width
}

func TestTabsTheLabelAndTheDescription(t *testing.T) {
	items := []TabItem{
		{Key: "a", Name: "index.ts", Description: "src/a"},
		{Key: "b", Name: "index.ts", Description: "src/b"},
		{Key: "c", Name: "main.go"},
	}
	ts := pumpTabs(t, DarkModern(), 700, items, TabsOpts{Active: 0})
	for _, text := range []string{"src/a", "src/b", "main.go"} {
		if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText(text)); err != nil {
			t.Error(err)
		}
	}
	if n := ts.Count(gluttertest.ByText("index.ts")); n != 2 {
		t.Errorf("%d labels of index.ts", n)
	}
}

func TestTabsPreviewIsItalic(t *testing.T) {
	items := []TabItem{{Key: "a", Name: "kept.go"}, {Key: "b", Name: "preview.go", Preview: true}}
	ts := pumpTabs(t, DarkModern(), 600, items, TabsOpts{Active: 0})
	for name, italic := range map[string]bool{"kept.go": false, "preview.go": true} {
		style := ts.Widget(gluttertest.ByText(name)).(*widgets.Text).Style()
		if got := style.FontStyle == ui.FontStyleItalic; got != italic {
			t.Errorf("%s has font style %v, italic = %v", name, style.FontStyle, italic)
		}
	}
}

func TestTabsDeletedIsStruckThrough(t *testing.T) {
	items := []TabItem{
		{Key: "a", Name: "kept.go", Description: "src"},
		{Key: "b", Name: "gone.go", Description: "lib", Deleted: true},
	}
	tests := []struct {
		name      string
		opts      TabsOpts
		wantError bool
	}{
		{"with colors", TabsOpts{Active: 0, DecorationColors: true}, true},
		{"without colors", TabsOpts{Active: 0}, false},
		{"single title of a deleted editor", TabsOpts{Active: 1, Mode: TabsSingle, DecorationColors: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := pumpTabs(t, DarkModern(), 600, items, tt.opts)
			deleted := map[string]bool{"kept.go": false, "src": false, "gone.go": true, "lib": true}
			for text, want := range deleted {
				if tt.opts.Mode == TabsSingle && text != "gone.go" && text != "lib" {
					continue // the single title shows the active editor alone
				}
				style := ts.Widget(gluttertest.ByText(text)).(*widgets.Text).Style()
				deco, _ := style.Decoration.Get()
				if got := deco.Contains(ui.TextDecorationLineThrough); got != want {
					t.Errorf("%s: struck through = %v, want %v", text, got, want)
				}
				if text == "gone.go" {
					color, _ := style.Color.Get()
					if got := color == DarkModern().ListErrorForeground; got != tt.wantError {
						t.Errorf("%s: drawn in the error color = %v, want %v", text, got, tt.wantError)
					}
				}
			}
		})
	}
}

func TestTabsColors(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			c := th.colors()
			ts := pumpTabs(t, c, 600, names("active.go", "other.go"), TabsOpts{Active: 0})
			y := 17
			// the active tab, the other tab (the strip's background shows past the last tab)
			if got, want := pixel(ts, 4, y), rgb(c.TabActiveBackground); got != want {
				t.Errorf("active tab %v, want %v", got, want)
			}
			if got, want := pixel(ts, 124, y), rgb(c.TabInactiveBackground); got != want {
				t.Errorf("inactive tab %v, want %v", got, want)
			}
			if got, want := pixel(ts, 500, y), rgb(c.EditorGroupHeaderTabsBackground); got != want {
				t.Errorf("strip %v, want %v", got, want)
			}
			// the top line of the active tab
			if got, want := pixel(ts, 60, 0), rgb(c.TabActiveBorderTop); got != want {
				t.Errorf("top border %v, want %v", got, want)
			}
			// the line between the tabs
			if got, want := pixel(ts, 119, y), rgb(c.TabBorder); got != want {
				t.Errorf("tab border %v, want %v", got, want)
			}
			// the same tabs in a group that is not the active one
			ts = pumpTabs(t, c, 600, names("active.go", "other.go"), TabsOpts{Active: 0, Unfocused: true})
			if got, want := pixel(ts, 4, y), rgb(c.TabUnfocusedActiveBackground); got != want {
				t.Errorf("unfocused active tab %v, want %v", got, want)
			}
			if got, want := pixel(ts, 60, 0), rgb(c.TabUnfocusedActiveBorderTop); got != want {
				t.Errorf("unfocused top border %v, want %v", got, want)
			}
		})
	}
}

func TestTabsClicks(t *testing.T) {
	items := names("a.go", "b.go", "c.go")
	items[1].Preview = true
	for _, tc := range []struct {
		name string
		act  func(ts *gluttertest.Tester)
		want tabsRecorder
	}{
		{"a press activates the tab", func(ts *gluttertest.Tester) { ts.TapAt(tabCenter(ts, "c.go"), mouse) },
			tabsRecorder{activated: []int{2}}},
		{"the middle button closes it", func(ts *gluttertest.Tester) {
			ts.TapAt(tabCenter(ts, "c.go"), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.TertiaryButton)})
		}, tabsRecorder{closed: []int{2}}},
		{"the secondary button does nothing", func(ts *gluttertest.Tester) {
			ts.TapAt(tabCenter(ts, "c.go"), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)})
		}, tabsRecorder{}},
		{"a double click on a preview tab keeps it", func(ts *gluttertest.Tester) {
			ts.TapAt(tabCenter(ts, "b.go"), mouse)
			ts.Pump(100 * time.Millisecond)
			ts.TapAt(tabCenter(ts, "b.go"), mouse)
		}, tabsRecorder{activated: []int{1, 1}, pinned: []int{1}}},
		{"a double click on a tab that is kept does not pin it", func(ts *gluttertest.Tester) {
			ts.TapAt(tabCenter(ts, "a.go"), mouse)
			ts.Pump(100 * time.Millisecond)
			ts.TapAt(tabCenter(ts, "a.go"), mouse)
		}, tabsRecorder{activated: []int{0, 0}}},
		{"two slow clicks are not a double click", func(ts *gluttertest.Tester) {
			ts.TapAt(tabCenter(ts, "b.go"), mouse)
			ts.Pump(600 * time.Millisecond)
			ts.TapAt(tabCenter(ts, "b.go"), mouse)
		}, tabsRecorder{activated: []int{1, 1}}},
		{"two clicks on two tabs are not a double click", func(ts *gluttertest.Tester) {
			ts.TapAt(tabCenter(ts, "b.go"), mouse)
			ts.TapAt(tabCenter(ts, "c.go"), mouse)
		}, tabsRecorder{activated: []int{1, 2}}},
		{"the close button closes and does not activate", func(ts *gluttertest.Tester) {
			ts.Hover(tabCenter(ts, "a.go"))
			ts.Pump(time.Millisecond)
			ts.TapAt(ts.Center(gluttertest.At(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconClose)), 0)), mouse)
		}, tabsRecorder{closed: []int{0}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r tabsRecorder
			ts := pumpTabs(t, DarkModern(), 600, items, r.opts(TabsOpts{Active: 0}))
			tc.act(ts)
			ts.Pump(time.Millisecond)
			if !equalInts(r.activated, tc.want.activated) || !equalInts(r.closed, tc.want.closed) ||
				!equalInts(r.pinned, tc.want.pinned) || !equalInts(r.unpinned, tc.want.unpinned) {
				t.Errorf("activated %v closed %v pinned %v unpinned %v, want %+v", r.activated, r.closed, r.pinned, r.unpinned, tc.want)
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTabsActions(t *testing.T) {
	closeIcon := fonts.CodiconIcon(fonts.CodiconClose)
	dot := fonts.CodiconIcon(fonts.CodiconCircleFilled)
	pin := fonts.CodiconIcon(fonts.CodiconPinned)
	pinDirty := fonts.CodiconIcon(fonts.CodiconPinnedDirty)
	has := func(ts *gluttertest.Tester, icon any) int { return ts.Count(gluttertest.ByIcon(icon)) }
	opacityOf := func(ts *gluttertest.Tester, icon any) float64 {
		f := gluttertest.Ancestor(gluttertest.ByIcon(icon), gluttertest.ByType[*widgets.Opacity]())
		return ts.Widget(gluttertest.First(f)).(*widgets.Opacity).Opacity()
	}

	t.Run("the close button shows in the active tab and the one under the pointer", func(t *testing.T) {
		ts := pumpTabs(t, DarkModern(), 600, names("a.go", "b.go"), TabsOpts{Active: 0})
		if has(ts, closeIcon) != 2 {
			t.Fatalf("%d close buttons, want one in each tab (one hidden)", has(ts, closeIcon))
		}
		if got := opacityOf(ts, closeIcon); got != 1 {
			t.Errorf("the close button of the active tab has opacity %v", got)
		}
		hidden := ts.Widget(gluttertest.At(gluttertest.Ancestor(gluttertest.ByIcon(closeIcon), gluttertest.ByType[*widgets.Opacity]()), 1)).(*widgets.Opacity)
		if hidden.Opacity() != 0 {
			t.Errorf("the close button of the other tab has opacity %v", hidden.Opacity())
		}
		ts.Hover(tabCenter(ts, "b.go"))
		ts.Pump(time.Millisecond)
		hovered := ts.Widget(gluttertest.At(gluttertest.Ancestor(gluttertest.ByIcon(closeIcon), gluttertest.ByType[*widgets.Opacity]()), 1)).(*widgets.Opacity)
		if hovered.Opacity() != 1 {
			t.Errorf("the close button of the tab under the pointer has opacity %v", hovered.Opacity())
		}
	})
	t.Run("the buttons of an unfocused group are dimmed", func(t *testing.T) {
		ts := pumpTabs(t, DarkModern(), 600, names("a.go"), TabsOpts{Active: 0, Unfocused: true})
		if got := opacityOf(ts, closeIcon); got != tabActionDimmed {
			t.Errorf("opacity %v, want %v", got, tabActionDimmed)
		}
	})
	t.Run("a modified tab shows a dot that becomes the x under the pointer", func(t *testing.T) {
		items := names("a.go", "b.go")
		items[1].Dirty = true
		var r tabsRecorder
		ts := pumpTabs(t, DarkModern(), 600, items, r.opts(TabsOpts{Active: 0}))
		if has(ts, dot) != 1 || has(ts, closeIcon) != 1 {
			t.Fatalf("%d dots, %d x", has(ts, dot), has(ts, closeIcon))
		}
		if got := opacityOf(ts, dot); got != 1 {
			t.Errorf("the dot has opacity %v", got)
		}
		at := ts.Center(gluttertest.ByIcon(dot))
		ts.Hover(at)
		ts.Pump(time.Millisecond)
		if has(ts, dot) != 0 || has(ts, closeIcon) != 2 {
			t.Errorf("under the pointer: %d dots, %d x", has(ts, dot), has(ts, closeIcon))
		}
		ts.TapAt(at, mouse)
		ts.Pump(time.Millisecond)
		if !equalInts(r.closed, []int{1}) || len(r.activated) != 0 {
			t.Errorf("closed %v, activated %v", r.closed, r.activated)
		}
	})
	t.Run("a pinned tab shows the pin, which unpins", func(t *testing.T) {
		items := []TabItem{{Key: "a", Name: "a.go", Sticky: true}, {Key: "b", Name: "b.go", Sticky: true, Dirty: true}, {Key: "c", Name: "c.go"}}
		var r tabsRecorder
		ts := pumpTabs(t, DarkModern(), 600, items, r.opts(TabsOpts{Active: 2}))
		if has(ts, pin) != 1 || has(ts, pinDirty) != 1 {
			t.Fatalf("%d pins, %d dirty pins", has(ts, pin), has(ts, pinDirty))
		}
		ts.TapAt(ts.Center(gluttertest.ByIcon(pin)), mouse)
		ts.Pump(time.Millisecond)
		if !equalInts(r.unpinned, []int{0}) || len(r.closed) != 0 || len(r.activated) != 0 {
			t.Errorf("unpinned %v closed %v activated %v", r.unpinned, r.closed, r.activated)
		}
	})
	t.Run("with no close button a modified tab keeps its dot, which does nothing", func(t *testing.T) {
		items := names("a.go", "b.go")
		items[0].Dirty = true
		var r tabsRecorder
		ts := pumpTabs(t, DarkModern(), 600, items, r.opts(TabsOpts{Active: 0, CloseButton: TabCloseOff}))
		if has(ts, closeIcon) != 0 || has(ts, dot) != 1 {
			t.Fatalf("%d x, %d dots", has(ts, closeIcon), has(ts, dot))
		}
		at := ts.Center(gluttertest.ByIcon(dot))
		ts.Hover(at)
		ts.Pump(time.Millisecond)
		if has(ts, closeIcon) != 0 {
			t.Error("the dot became an x")
		}
		ts.TapAt(at, mouse)
		ts.Pump(time.Millisecond)
		if len(r.closed) != 0 {
			t.Errorf("closed %v", r.closed)
		}
	})
	t.Run("with no close button a clean tab has no action", func(t *testing.T) {
		ts := pumpTabs(t, DarkModern(), 600, names("a.go"), TabsOpts{Active: 0, CloseButton: TabCloseOff})
		if has(ts, closeIcon) != 0 {
			t.Error("a close button is there")
		}
	})
	t.Run("the close button can be at the left", func(t *testing.T) {
		ts := pumpTabs(t, DarkModern(), 600, names("a.go"), TabsOpts{Active: 0, CloseButton: TabCloseLeft})
		if x, label := ts.Center(gluttertest.ByIcon(closeIcon)).Dx, tabCenter(ts, "a.go").Dx; x >= label {
			t.Errorf("the button is at %v, after the label at %v", x, label)
		}
		right := pumpTabs(t, DarkModern(), 600, names("a.go"), TabsOpts{Active: 0})
		if x, label := right.Center(gluttertest.ByIcon(closeIcon)).Dx, tabCenter(right, "a.go").Dx; x <= label {
			t.Errorf("the button is at %v, before the label at %v", x, label)
		}
	})
	t.Run("a compact pinned tab has no action", func(t *testing.T) {
		items := []TabItem{{Key: "a", Name: "app.go", Sticky: true}}
		ts := pumpTabs(t, DarkModern(), 600, items, TabsOpts{Active: 0, PinnedSizing: PinnedCompact})
		if has(ts, pin) != 0 {
			t.Error("a pin is there")
		}
		if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText("A")); err != nil {
			t.Error("the first letter shows:", err)
		}
	})
}

func TestTabsIconSlot(t *testing.T) {
	icon := widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconFileCode))
	items := []TabItem{{Key: "a", Name: "a.go", Icon: icon}, {Key: "b", Name: "b.go"}}
	ts := pumpTabs(t, DarkModern(), 600, items, TabsOpts{Active: 0})
	if n := ts.Count(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconFileCode))); n != 1 {
		t.Errorf("%d file icons, want 1", n)
	}
	if ix, lx := ts.Center(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconFileCode))).Dx, tabCenter(ts, "a.go").Dx; ix >= lx {
		t.Errorf("the icon is at %v, after the label at %v", ix, lx)
	}
}

func TestTabsOverflow(t *testing.T) {
	ns := []string{"one.go", "two.go", "three.go", "four.go", "five.go", "six.go", "seven.go", "eight.go"}
	t.Run("the active tab is revealed", func(t *testing.T) {
		ts := pumpTabs(t, DarkModern(), 400, names(ns...), TabsOpts{Active: 7})
		ts.Pump(time.Millisecond)
		ts.Pump(time.Millisecond)
		if c := tabCenter(ts, "eight.go").Dx; c < 0 || c > 400 {
			t.Errorf("the active tab is at %v, outside the 400 px strip", c)
		}
	})
	t.Run("a new active tab is revealed", func(t *testing.T) {
		active := 0
		var set widgets.StateSetter
		host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, setState widgets.StateSetter) widgets.Widget {
			set = setState
			return widgets.NewSizedBox(NewTabs(names(ns...), TabsOpts{Active: active}), widgets.SizedBoxOpts{Width: foundation.Some(400.0)})
		})
		ts := pumpThemed(t, DarkModern(), ui.Size{Width: 400, Height: 80}, host)
		ts.Pump(time.Millisecond)
		if c := tabCenter(ts, "one.go").Dx; c < 0 || c > 400 {
			t.Fatalf("the first tab is at %v", c)
		}
		if c := tabCenter(ts, "eight.go").Dx; c >= 0 && c <= 400 {
			t.Fatalf("the last tab is in view at %v", c)
		}
		set(func() { active = 7 })
		ts.Pump(time.Millisecond)
		ts.Pump(time.Millisecond)
		ts.Pump(time.Millisecond)
		if c := tabCenter(ts, "eight.go").Dx; c < 0 || c > 400 {
			t.Errorf("the last tab is at %v after it became active", c)
		}
		set(func() { active = 0 })
		ts.Pump(time.Millisecond)
		ts.Pump(time.Millisecond)
		ts.Pump(time.Millisecond)
		if c := tabCenter(ts, "one.go").Dx; c < 0 || c > 400 {
			t.Errorf("the first tab is at %v after it became active again", c)
		}
	})
	t.Run("the wheel scrolls the strip sideways", func(t *testing.T) {
		ts := pumpTabs(t, DarkModern(), 400, names(ns...), TabsOpts{Active: 0})
		before := tabCenter(ts, "one.go").Dx
		ts.ScrollAt(ui.Offset{Dx: 200, Dy: 17}, ui.Offset{Dy: wheelNotch})
		ts.Pump(time.Millisecond)
		ts.Pump(time.Millisecond)
		if after := tabCenter(ts, "one.go").Dx; after >= before {
			t.Errorf("the first tab moved from %v to %v", before, after)
		}
	})
}

func TestTabsSingleMode(t *testing.T) {
	items := []TabItem{{Key: "a", Name: "a.go"}, {Key: "b", Name: "b.go", Description: "src", Dirty: true}, {Key: "c", Name: "c.go"}}
	ts := pumpTabs(t, DarkModern(), 600, items, TabsOpts{Active: 1, Mode: TabsSingle})
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText("b.go")); err != nil {
		t.Error(err)
	}
	for _, n := range []string{"a.go", "c.go"} {
		if ts.Exists(gluttertest.ByText(n)) {
			t.Errorf("%s is shown in single mode", n)
		}
	}
	if !ts.Exists(gluttertest.ByText("src")) || !ts.Exists(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCircleFilled))) {
		t.Error("the description and the dot of the active editor show")
	}
	if ts.Exists(gluttertest.ByType[*tabWidget]()) {
		t.Error("single mode shows tabs")
	}
	if h := ts.Size(gluttertest.ByType[*Tabs]()).Height; h != TabHeight {
		t.Errorf("the header is %v high", h)
	}
	// no active editor: an empty header
	empty := pumpTabs(t, DarkModern(), 300, nil, TabsOpts{Active: -1, Mode: TabsSingle})
	if errs := empty.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestTabsHighlightModifiedAndColors(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			c := th.colors()
			items := names("a.go", "b.go")
			items[1].Dirty = true
			ts := pumpTabs(t, c, 600, items, TabsOpts{Active: 0, HighlightModified: true})
			if got, want := pixel(ts, 180, 0), rgb(c.TabInactiveModifiedBorder); got == (color.RGBA{}) || got == rgb(c.TabInactiveBackground) {
				t.Errorf("the top line of a modified tab is %v, want a blend of %v", got, want)
			}
			if got, want := pixel(ts, 180, 1), rgb(c.TabActiveModifiedBorder); got == rgb(c.TabInactiveBackground) {
				t.Errorf("the line is 2 px high: %v at the second row, %v at full strength", got, want)
			}
			plain := pumpTabs(t, c, 600, items, TabsOpts{Active: 0})
			if got := pixel(plain, 180, 0); got != rgb(c.TabInactiveBackground) {
				t.Errorf("without the setting the top of the tab is %v", got)
			}
		})
	}
}

func TestTabsHoverBackground(t *testing.T) {
	c := DarkModern()
	ts := pumpTabs(t, c, 600, names("a.go", "b.go"), TabsOpts{Active: 0})
	y := 17
	before := pixel(ts, 124, y)
	ts.Hover(ui.Offset{Dx: 124, Dy: float64(y)})
	ts.Pump(time.Millisecond)
	after := pixel(ts, 124, y)
	// Dark Modern's hover background is the active one
	if before != rgb(c.TabInactiveBackground) || after != rgb(c.TabHoverBackground) || before == after {
		t.Errorf("hover: %v then %v, want %v then %v", before, after, rgb(c.TabInactiveBackground), rgb(c.TabHoverBackground))
	}
}

func TestDarkModernTabColors(t *testing.T) {
	c, l := DarkModern(), LightModern()
	for _, tc := range []struct {
		name string
		got  ui.Color
		want uint32
	}{
		{"dark tab.activeBackground", c.TabActiveBackground, 0xFF1F1F1F},
		{"dark tab.inactiveBackground", c.TabInactiveBackground, 0xFF181818},
		{"dark tab.activeForeground", c.TabActiveForeground, 0xFFFFFFFF},
		{"dark tab.inactiveForeground", c.TabInactiveForeground, 0xFF9D9D9D},
		{"dark tab.activeBorderTop", c.TabActiveBorderTop, 0xFF0078D4},
		{"dark tab.border", c.TabBorder, 0xFF2B2B2B},
		{"dark editorGroupHeader.tabsBackground", c.EditorGroupHeaderTabsBackground, 0xFF181818},
		{"dark editorGroupHeader.tabsBorder", c.EditorGroupHeaderTabsBorder, 0xFF2B2B2B},
		{"light tab.activeBackground", l.TabActiveBackground, 0xFFFFFFFF},
		{"light tab.inactiveBackground", l.TabInactiveBackground, 0xFFF8F8F8},
		{"light tab.activeBorderTop", l.TabActiveBorderTop, 0xFF005FB8},
		{"light editorGroupHeader.tabsBackground", l.EditorGroupHeaderTabsBackground, 0xFFF8F8F8},
	} {
		if tc.got != ui.NewColor(tc.want) {
			t.Errorf("%s = %v, want %#08x", tc.name, tc.got, tc.want)
		}
	}
}

func TestTabsActionsAreAtTheRightEndAndGetTheWidthOfTheStrip(t *testing.T) {
	for _, tc := range []struct {
		name   string
		single bool
	}{{"tabs", false}, {"single", true}} {
		t.Run(tc.name, func(t *testing.T) {
			var width float64
			opts := TabsOpts{Active: 0, Actions: func(w float64) widgets.Widget {
				width = w
				return NewIconButton(fonts.CodiconAdd, IconButtonOpts{Tooltip: "Add"})
			}}
			if tc.single {
				opts.Mode = TabsSingle
			}
			ts := pumpTabs(t, DarkModern(), 400, names("a", "b"), opts)
			if width != 400 {
				t.Errorf("the actions were asked for the width %v, want 400", width)
			}
			at := ts.Center(gluttertest.ByType[*IconButton]())
			if at.Dx < 400-30 || at.Dy < 10 || at.Dy > 25 {
				t.Errorf("the action is at %v, not at the right end of the strip", at)
			}
		})
	}
	// an actions function that gives nothing leaves the strip as it was
	pumpTabs(t, DarkModern(), 400, names("a"), TabsOpts{Actions: func(float64) widgets.Widget { return nil }})
}
