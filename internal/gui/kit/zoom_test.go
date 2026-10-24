package kit

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// pumpZoomed shows a page in a window under a Zoom that sits above the navigator, as the window of the application
// has it, and gives the notifier that changes the factor.
func pumpZoomed(t *testing.T, size ui.Size, factor float64, page widgets.Widget) (*gluttertest.Tester, *foundation.ValueNotifier[float64]) {
	t.Helper()
	notifier := foundation.NewValueNotifier(factor)
	ts := testutil.New(t, size)
	ts.PumpWidget(widgets.NewWidgetsApp(DarkModern().EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		Builder: func(_ widgets.BuildContext, navigator widgets.Widget) widgets.Widget {
			return widgets.NewListenableBuilder(notifier, func(_ widgets.BuildContext, child widgets.Widget) widgets.Widget {
				return NewZoom(notifier.Value(), child)
			}, navigator)
		},
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				return NewTheme(DarkModern(), page, ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	ts.Pump(time.Millisecond)
	return ts, notifier
}

// zoomProbe records the constraints and the media query that its context sees.
type zoomProbe struct {
	constraints rendering.BoxConstraints
	media       widgets.MediaQueryData
}

func (p *zoomProbe) widget() widgets.Widget {
	return widgets.NewLayoutBuilder(func(bc widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		p.constraints = c
		p.media = widgets.MediaQueryOf(bc)
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Key: zoomProbeKey})
	})
}

var zoomProbeKey = foundation.NewValueKey("zoom-probe")

func TestZoomGivesTheChildTheScaledRoom(t *testing.T) {
	cases := []struct {
		name   string
		factor float64
	}{
		{"no zoom", 1},
		{"one level in", 1.2},
		{"two levels in", 1.44},
		{"twice", 2},
		{"one level out", 1 / 1.2},
		{"half", 0.5},
	}
	window := ui.Size{Width: 960, Height: 600}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p zoomProbe
			ts, _ := pumpZoomed(t, window, tc.factor, p.widget())
			wantW, wantH := window.Width/tc.factor, window.Height/tc.factor
			if math.Abs(p.constraints.MaxWidth-wantW) > 1e-9 || math.Abs(p.constraints.MaxHeight-wantH) > 1e-9 {
				t.Errorf("the child has the room %vx%v, want %vx%v", p.constraints.MaxWidth, p.constraints.MaxHeight, wantW, wantH)
			}
			if p.media.Size.Width != p.constraints.MaxWidth || p.media.Size.Height != p.constraints.MaxHeight {
				t.Errorf("the media query size %v is not the room of the child", p.media.Size)
			}
			if got, want := p.media.DevicePixelRatio, ts.View.DevicePixelRatio()*tc.factor; math.Abs(got-want) > 1e-9 {
				t.Errorf("device pixel ratio %v, want %v", got, want)
			}
			// painted scaled back: the page covers the whole window
			if got := ts.Size(gluttertest.ByKey(zoomProbeKey)); math.Abs(got.Width*tc.factor-window.Width) > 1e-6 || math.Abs(got.Height*tc.factor-window.Height) > 1e-6 {
				t.Errorf("the child is %v and covers %vx%v of the window", got, got.Width*tc.factor, got.Height*tc.factor)
			}
		})
	}
}

func TestZoomOfAnythingButAPositiveNumberIsNone(t *testing.T) {
	for _, f := range []float64{0, -2, math.NaN(), math.Inf(-1)} {
		if got := NewZoom(f, widgets.NewSizedBox(nil)).Factor(); got != 1 {
			t.Errorf("NewZoom(%v).Factor() = %v, want 1", f, got)
		}
	}
	if got := NewZoom(1.44, widgets.NewSizedBox(nil)).Factor(); got != 1.44 {
		t.Errorf("Factor() = %v", got)
	}
}

// tapPage is two buttons side by side at the top left (100x40 each) and a third at the right edge of the room the
// child has, in which a tap counts which one it hit.
func tapPage(hits *[]string, roomWidth float64) widgets.Widget {
	button := func(name string, left float64) widgets.Widget {
		return widgets.NewPositioned(widgets.NewGestureDetector(
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(100.0), Height: foundation.Some(40.0)}),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { *hits = append(*hits, name) }}),
			widgets.PositionedOpts{Left: foundation.Some(left), Top: foundation.Some(0.0)})
	}
	return widgets.NewStack([]widgets.Widget{button("a", 0), button("b", 100), button("right", roomWidth-100)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

func TestZoomHitTestingIsScaled(t *testing.T) {
	window := ui.Size{Width: 800, Height: 600}
	cases := []struct {
		name   string
		factor float64
		at     ui.Offset
		want   string
	}{
		{"the second button at its scaled place", 2, ui.Offset{Dx: 300, Dy: 40}, "b"},
		{"the first button at its scaled place", 2, ui.Offset{Dx: 100, Dy: 40}, "a"},
		{"the unscaled place of the second button is the first", 2, ui.Offset{Dx: 150, Dy: 20}, "a"},
		{"beyond the scaled buttons nothing is hit", 2, ui.Offset{Dx: 450, Dy: 40}, ""},
		{"below the buttons nothing is hit", 2, ui.Offset{Dx: 100, Dy: 90}, ""},
		{"the far right of the window is reached", 2, ui.Offset{Dx: 790, Dy: 40}, "right"},
		{"a zoom out", 0.5, ui.Offset{Dx: 75, Dy: 10}, "b"},
		{"the far right at a zoom out", 0.5, ui.Offset{Dx: 790, Dy: 10}, "right"},
		{"one level", 1.2, ui.Offset{Dx: 1.2 * 150, Dy: 1.2 * 20}, "b"},
		{"no zoom", 1, ui.Offset{Dx: 150, Dy: 20}, "b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits []string
			ts, _ := pumpZoomed(t, window, tc.factor, tapPage(&hits, window.Width/tc.factor))
			ts.TapAt(tc.at)
			ts.Pump(time.Millisecond)
			got := ""
			if len(hits) > 0 {
				got = hits[0]
			}
			if got != tc.want || len(hits) > 1 {
				t.Errorf("a tap at %v hit %v, want %q", tc.at, hits, tc.want)
			}
		})
	}
}

func TestZoomScrollsAndDrags(t *testing.T) {
	window := ui.Size{Width: 800, Height: 600}
	t.Run("the wheel scrolls", func(t *testing.T) {
		controller := widgets.NewScrollController()
		items := make([]widgets.Widget, 100)
		for i := range items {
			items[i] = widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(30.0)})
		}
		list := widgets.NewSingleChildScrollView(widgets.NewColumn(items), widgets.SingleChildScrollViewOpts{Controller: controller})
		ts, _ := pumpZoomed(t, window, 2, list)
		ts.ScrollAt(ui.Offset{Dx: 200, Dy: 200}, ui.Offset{Dx: 0, Dy: 60})
		ts.Pump(time.Millisecond)
		if controller.Offset() <= 0 {
			t.Errorf("the list did not scroll: %v", controller.Offset())
		}
	})
	t.Run("a drag moves by the scaled distance", func(t *testing.T) {
		var total ui.Offset
		var ended int
		area := widgets.NewGestureDetector(
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(300.0), Height: foundation.Some(300.0)}),
			widgets.GestureDetectorOpts{
				Behavior: rendering.HitTestBehaviorOpaque,
				OnPanUpdate: func(d gestures.DragUpdateDetails) {
					total = ui.Offset{Dx: total.Dx + d.Delta.Dx, Dy: total.Dy + d.Delta.Dy}
				},
				OnPanEnd: func(gestures.DragEndDetails) { ended++ },
			})
		ts, _ := pumpZoomed(t, window, 2, area)
		ts.DragFrom(ui.Offset{Dx: 100, Dy: 100}, ui.Offset{Dx: 100, Dy: 60})
		ts.Pump(time.Millisecond)
		if ended != 1 {
			t.Fatalf("the drag ended %d times", ended)
		}
		// the pointer moved 100 x 60 on the screen, which is 50 x 30 in the child's logical pixels (less the slop that
		// starts the gesture)
		if total.Dx <= 0 || total.Dx > 50.001 || total.Dy <= 0 || total.Dy > 30.001 {
			t.Errorf("the drag moved the child's coordinates by %v, want at most (50, 30)", total)
		}
	})
}

// counter is a stateful widget that counts its builds' taps, to see that a change of the factor keeps state.
type counter struct {
	widgets.StatefulWidget
}

func newCounter() *counter { return &counter{StatefulWidget: widgets.NewStatefulWidgetBase()} }

func (c *counter) CreateState() widgets.State { return &counterState{} }

type counterState struct {
	widgets.StateOf[*counter]
	n int
}

func (s *counterState) Build(widgets.BuildContext) widgets.Widget {
	return widgets.NewGestureDetector(
		widgets.NewSizedBox(widgets.NewText(fmt.Sprint("count ", s.n)), widgets.SizedBoxOpts{Width: foundation.Some(100.0), Height: foundation.Some(40.0)}),
		widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { s.SetState(func() { s.n++ }) }})
}

func TestChangingTheFactorKeepsTheStateOfTheChild(t *testing.T) {
	ts, factor := pumpZoomed(t, ui.Size{Width: 800, Height: 600}, 1, newCounter())
	ts.TapAt(ui.Offset{Dx: 10, Dy: 10})
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByText("count 1")) {
		t.Fatal("the first tap did not count")
	}
	for i, f := range []float64{2, 1.2, 0.5, 1} {
		factor.SetValue(f)
		ts.Pump(time.Millisecond)
		ts.Pump(time.Millisecond)
		if want := fmt.Sprint("count ", i+1); !ts.Exists(gluttertest.ByText(want)) {
			t.Fatalf("the child lost its state at the factor %v (want %q)", f, want)
		}
		// the same place of the child, scaled, still counts
		ts.TapAt(ui.Offset{Dx: 10 * f, Dy: 10 * f})
		ts.Pump(time.Millisecond)
		if want := fmt.Sprint("count ", i+2); !ts.Exists(gluttertest.ByText(want)) {
			t.Fatalf("at the factor %v the tap did not count (want %q)", f, want)
		}
	}
}

func TestAContextMenuUnderAZoomIsAtThePointerAndScaled(t *testing.T) {
	window := ui.Size{Width: 800, Height: 600}
	secondary := gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)}
	var ran []string
	items := func() []MenuItem { return testMenuItems() }
	for _, factor := range []float64{1, 2, 0.5} {
		t.Run(fmt.Sprint("factor ", factor), func(t *testing.T) {
			ran = nil
			child := widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(window.Width / factor), Height: foundation.Some(window.Height / factor)})
			ts, _ := pumpZoomed(t, window, factor, NewContextMenuRegion(child, items, ContextMenuOpts{OnRun: func(it MenuItem) { ran = append(ran, it.ID) }}))
			at := ui.Offset{Dx: 200, Dy: 150}
			ts.TapAt(at, secondary)
			ts.Pump(time.Millisecond)
			ts.Pump(time.Millisecond)
			menu := gluttertest.ByType[*Menu]()
			if !ts.Exists(menu) {
				t.Fatal("no menu")
			}
			// the menu's corner is 2 px below the pointer, in the logical pixels of the child, which are `factor` pixels
			// of the screen
			got := ts.TopLeft(menu)
			want := ui.Offset{Dx: at.Dx, Dy: at.Dy + 2*factor}
			if math.Abs(got.Dx-want.Dx) > 1e-6 || math.Abs(got.Dy-want.Dy) > 1e-6 {
				t.Errorf("the menu is at %v on the screen, want %v", got, want)
			}
			// an item is hit where it is painted
			c := ts.Center(gluttertest.ByText("New File"))
			ts.TapAt(c)
			ts.Pump(time.Millisecond)
			if fmt.Sprint(ran) != "[new]" {
				t.Errorf("a tap on the item at %v ran %v", c, ran)
			}
		})
	}
}

func TestGoldenZoom(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			colors := th.colors()
			notifier := foundation.NewValueNotifier(1.44)
			ts := testutil.New(t, ui.Size{Width: 520, Height: 480})
			handle := NewMenuHandle()
			menu := NewMenu(goldenMenuItems(), MenuOpts{Handle: handle, EnableMnemonics: true, OnRun: func(MenuItem) {}})
			ts.PumpWidget(widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
				DebugShowCheckedModeBanner: foundation.Some(false),
				Builder: func(_ widgets.BuildContext, navigator widgets.Widget) widgets.Widget {
					return NewZoom(notifier.Value(), navigator)
				},
				OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
					return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
						page := widgets.NewContainer(widgets.NewAlign(padded(menu), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}), widgets.ContainerOpts{Color: foundation.Some(colors.EditorBackground)})
						return NewTheme(colors, page, ThemeOpts{UIFont: testutil.MonoFamily})
					}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
				},
			}))
			ts.Pump(time.Millisecond)
			handle.Focus(true)
			ts.Pump(time.Millisecond)
			testutil.MatchesGolden(ts, "zoom_"+th.name)
		})
	}
}
