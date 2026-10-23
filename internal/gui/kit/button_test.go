package kit

import (
	"fmt"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// pumpThemed shows a widget at the top left of a window of a size, with the colors and the test font.
func pumpThemed(t *testing.T, colors Colors, size ui.Size, child widgets.Widget) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, size)
	ts.PumpWidget(widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				page := widgets.NewContainer(widgets.NewAlign(child, widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}), widgets.ContainerOpts{Color: foundation.Some(colors.EditorBackground)})
				return NewTheme(colors, page, ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	ts.Pump(time.Millisecond)
	return ts
}

// backgroundOf is the background color of the first container below the widget the finder finds.
func backgroundOf(t *testing.T, ts *gluttertest.Tester, root gluttertest.Finder) ui.Color {
	t.Helper()
	f := gluttertest.First(gluttertest.Descendant(root, gluttertest.ByType[*widgets.Container]()))
	c, ok := ts.Widget(f).(*widgets.Container)
	if !ok {
		t.Fatal("no container below the widget")
	}
	d, ok := c.Decoration().(*painting.BoxDecoration)
	if !ok {
		t.Fatalf("decoration %T", c.Decoration())
	}
	return d.Color.OrElse(ui.Color{})
}

// outlineOf is the border color of the first container below the widget.
func borderOf(t *testing.T, ts *gluttertest.Tester, root gluttertest.Finder) ui.Color {
	t.Helper()
	f := gluttertest.First(gluttertest.Descendant(root, gluttertest.ByType[*widgets.Container]()))
	d := ts.Widget(f).(*widgets.Container).Decoration().(*painting.BoxDecoration)
	b, ok := d.Border.(painting.Border)
	if !ok {
		t.Fatalf("border %T", d.Border)
	}
	return b.Top.Color
}

func key(ts *gluttertest.Tester, k services.LogicalKeyboardKey) {
	ts.SendKeyDownEvent(k)
	ts.SendKeyUpEvent(k)
	ts.Pump(time.Millisecond)
}

func TestButtonPress(t *testing.T) {
	cases := []struct {
		name string
		opts func(pressed *int) ButtonOpts
		act  func(ts *gluttertest.Tester)
		want int
	}{
		{"a click", func(p *int) ButtonOpts { return ButtonOpts{OnPressed: func() { *p++ }} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByType[*Button]()) }, 1},
		{"a click on a secondary button", func(p *int) ButtonOpts { return ButtonOpts{Secondary: true, OnPressed: func() { *p++ }} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByType[*Button]()) }, 1},
		{"a click on a disabled button", func(p *int) ButtonOpts { return ButtonOpts{Disabled: true, OnPressed: func() { *p++ }} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByType[*Button]()) }, 0},
		{"a click on a button without a handler", func(*int) ButtonOpts { return ButtonOpts{} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByType[*Button]()) }, 0},
		{"Enter after the button has the focus", func(p *int) ButtonOpts { return ButtonOpts{Autofocus: true, OnPressed: func() { *p++ }} },
			func(ts *gluttertest.Tester) { key(ts, services.LogicalKeyboardKeyEnter) }, 1},
		{"the numpad's Enter", func(p *int) ButtonOpts { return ButtonOpts{Autofocus: true, OnPressed: func() { *p++ }} },
			func(ts *gluttertest.Tester) { key(ts, services.LogicalKeyboardKeyNumpadEnter) }, 1},
		{"Space", func(p *int) ButtonOpts { return ButtonOpts{Autofocus: true, OnPressed: func() { *p++ }} },
			func(ts *gluttertest.Tester) { key(ts, services.LogicalKeyboardKeySpace) }, 1},
		{"another key", func(p *int) ButtonOpts { return ButtonOpts{Autofocus: true, OnPressed: func() { *p++ }} },
			func(ts *gluttertest.Tester) { key(ts, services.LogicalKeyboardKeyKeyA) }, 0},
		{"Enter on a disabled button", func(p *int) ButtonOpts {
			return ButtonOpts{Autofocus: true, Disabled: true, OnPressed: func() { *p++ }}
		},
			func(ts *gluttertest.Tester) { key(ts, services.LogicalKeyboardKeyEnter) }, 0},
		{"Enter without the focus", func(p *int) ButtonOpts { return ButtonOpts{OnPressed: func() { *p++ }} },
			func(ts *gluttertest.Tester) { key(ts, services.LogicalKeyboardKeyEnter) }, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pressed int
			b := NewButton("Open", tc.opts(&pressed))
			ts := pumpControl(t, b)
			tc.act(ts)
			ts.Pump(time.Millisecond)
			if pressed != tc.want {
				t.Errorf("pressed %d times, want %d", pressed, tc.want)
			}
			if b.Label() != "Open" {
				t.Errorf("label %q", b.Label())
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

// buttonColors are the colors of the states of a button in a theme.
func TestButtonStates(t *testing.T) {
	for _, theme := range []struct {
		name   string
		colors Colors
	}{{"Dark Modern", DarkModern()}, {"Light Modern", LightModern()}} {
		c := theme.colors
		cases := []struct {
			name      string
			secondary bool
			disabled  bool
			state     string // "", "hover" or "focus"
			bg        ui.Color
			border    ui.Color
		}{
			{"primary", false, false, "", c.ButtonBackground, c.ButtonBorder},
			{"primary under the pointer", false, false, "hover", c.ButtonHoverBackground, c.ButtonBorder},
			{"primary with the focus", false, false, "focus", c.ButtonHoverBackground, c.ButtonBorder},
			{"primary disabled under the pointer", false, true, "hover", c.ButtonBackground, c.ButtonBorder},
			{"secondary", true, false, "", c.ButtonSecondaryBackground, c.ButtonSecondaryBorder},
			{"secondary under the pointer", true, false, "hover", c.ButtonSecondaryHoverBackground, c.ButtonSecondaryBorder},
			{"secondary with the focus", true, false, "focus", c.ButtonSecondaryHoverBackground, c.ButtonSecondaryBorder},
		}
		for _, tc := range cases {
			t.Run(theme.name+" "+tc.name, func(t *testing.T) {
				b := NewButton("Open", ButtonOpts{Secondary: tc.secondary, Disabled: tc.disabled, OnPressed: func() {}})
				ts := pumpThemed(t, c, ui.Size{Width: 240, Height: 60}, b)
				root := gluttertest.ByType[*Button]()
				switch tc.state {
				case "hover":
					ts.Hover(ts.Center(root))
				case "focus":
					ts.Tap(root)
					ts.Hover(ui.Offset{Dx: 230, Dy: 55}) // away from the button
				}
				ts.Pump(time.Millisecond)
				if got := backgroundOf(t, ts, root); got != tc.bg {
					t.Errorf("background %v, want %v", got, tc.bg)
				}
				if got := borderOf(t, ts, root); got != tc.border {
					t.Errorf("border %v, want %v", got, tc.border)
				}
			})
		}
	}
}

// Ported from VS Code: src/vs/base/test/browser/ui/button/button.test.ts ("disabling a hovered
// button clears its hover background")
func TestButtonDisablingAHoveredButtonClearsItsHoverBackground(t *testing.T) {
	c := DarkModern()
	root := gluttertest.ByType[*Button]()
	disabled := false
	host := newHost(func(widgets.BuildContext) widgets.Widget {
		return NewButton("Open", ButtonOpts{Disabled: disabled, OnPressed: func() {}})
	})
	ts := pumpThemed(t, c, ui.Size{Width: 240, Height: 60}, host)
	var got []ui.Color
	got = append(got, backgroundOf(t, ts, root))
	ts.Hover(ts.Center(root))
	ts.Pump(time.Millisecond)
	got = append(got, backgroundOf(t, ts, root))
	disabled = true
	host.Rebuild()
	ts.Pump(time.Millisecond)
	got = append(got, backgroundOf(t, ts, root))
	ts.Hover(ui.Offset{Dx: 230, Dy: 55})
	ts.Hover(ts.Center(root))
	ts.Pump(time.Millisecond)
	got = append(got, backgroundOf(t, ts, root))
	want := []ui.Color{c.ButtonBackground, c.ButtonHoverBackground, c.ButtonBackground, c.ButtonBackground}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("backgrounds %v, want %v", got, want)
	}
}

// host is a widget that builds what a function gives and rebuilds when asked, so that a test can
// change the options of a widget while it is shown.
type host struct {
	widgets.StatefulWidget
	build func(bc widgets.BuildContext) widgets.Widget
	state *hostState
}

func newHost(build func(bc widgets.BuildContext) widgets.Widget) *host {
	return &host{StatefulWidget: widgets.NewStatefulWidgetBase(), build: build}
}

// Rebuild builds the widget again.
func (h *host) Rebuild() { h.state.SetState(func() {}) }

func (h *host) CreateState() widgets.State { h.state = &hostState{}; return h.state }

type hostState struct{ widgets.StateOf[*host] }

func (s *hostState) Build(bc widgets.BuildContext) widgets.Widget { return s.Widget().build(bc) }

func TestButtonFocus(t *testing.T) {
	t.Run("a click gives the focus and Escape takes it away", func(t *testing.T) {
		var focus []bool
		var escaped int
		b := NewButton("Open", ButtonOpts{OnPressed: func() {}, OnEscape: func() { escaped++ }, OnFocusChange: func(f bool) { focus = append(focus, f) }})
		ts := pumpControl(t, b)
		ts.Tap(gluttertest.ByType[*Button]())
		ts.Pump(time.Millisecond)
		if fmt.Sprint(focus) != "[true]" {
			t.Fatalf("focus changes %v after a click", focus)
		}
		key(ts, services.LogicalKeyboardKeyEscape)
		if escaped != 1 || fmt.Sprint(focus) != "[true false]" {
			t.Errorf("escaped %d times, focus changes %v", escaped, focus)
		}
	})
	t.Run("Escape without a handler still gives the focus up", func(t *testing.T) {
		node := widgets.NewFocusNode(widgets.FocusNodeOpts{})
		defer node.Dispose()
		ts := pumpControl(t, NewButton("Open", ButtonOpts{OnPressed: func() {}, Autofocus: true, FocusNode: node}))
		if !node.HasFocus() {
			t.Fatal("the button did not take the focus it asked for")
		}
		key(ts, services.LogicalKeyboardKeyEscape)
		if node.HasFocus() {
			t.Error("the button kept the focus")
		}
	})
	t.Run("a disabled button does not take the focus", func(t *testing.T) {
		node := widgets.NewFocusNode(widgets.FocusNodeOpts{})
		defer node.Dispose()
		ts := pumpControl(t, NewButton("Open", ButtonOpts{OnPressed: func() {}, Disabled: true, Autofocus: true, FocusNode: node}))
		ts.Tap(gluttertest.ByType[*Button]())
		ts.Pump(time.Millisecond)
		if node.HasFocus() {
			t.Error("a disabled button took the focus")
		}
	})
	t.Run("the focus outline is drawn while it has the focus", func(t *testing.T) {
		ts := pumpControl(t, NewButton("Open", ButtonOpts{OnPressed: func() {}}))
		before := ts.Count(gluttertest.ByType[*widgets.DecoratedBox]())
		ts.Tap(gluttertest.ByType[*Button]())
		ts.Pump(time.Millisecond)
		if after := ts.Count(gluttertest.ByType[*widgets.DecoratedBox]()); after != before+1 {
			t.Errorf("%d decorated boxes before the focus, %d after", before, after)
		}
	})
}

func TestButtonMetrics(t *testing.T) {
	cases := []struct {
		name string
		opts ButtonOpts
		// the height is the line height and the padding and border; the width of a label depends on the
		// font, so only the part outside the label is checked
		height, extra float64
	}{
		{"normal", ButtonOpts{OnPressed: func() {}}, 16 + 2*4 + 2, 2*8 + 2},
		{"small", ButtonOpts{OnPressed: func() {}, Small: true}, 14 + 2*3 + 2, 2*6 + 2},
		{"with an icon", ButtonOpts{OnPressed: func() {}, Icon: fonts.CodiconAdd}, 16 + 2*4 + 2, 2*8 + 2 + iconSize + buttonIconGap},
	}
	var labelWidth float64
	{
		ts := pumpControl(t, NewButton("Open", ButtonOpts{OnPressed: func() {}}))
		labelWidth = ts.Size(gluttertest.ByType[*Button]()).Width - (2*8 + 2)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := pumpControl(t, NewButton("Open", tc.opts))
			size := ts.Size(gluttertest.ByType[*Button]())
			if size.Height != tc.height {
				t.Errorf("height %v, want %v", size.Height, tc.height)
			}
			scale := 1.0
			if tc.opts.Small {
				scale = 11.0 / 12.0
			}
			want := tc.extra + labelWidth*scale
			if d := size.Width - want; d < -1 || d > 1 {
				t.Errorf("width %v, want about %v", size.Width, want)
			}
		})
	}
	t.Run("Expand fills the width", func(t *testing.T) {
		ts := pumpThemed(t, DarkModern(), ui.Size{Width: 240, Height: 60},
			widgets.NewSizedBox(NewButton("Open", ButtonOpts{OnPressed: func() {}, Expand: true}), widgets.SizedBoxOpts{Width: foundation.Some(200.0)}))
		if w := ts.Size(gluttertest.ByType[*Button]()).Width; w != 200 {
			t.Errorf("width %v", w)
		}
	})
}

func TestButtonTooltip(t *testing.T) {
	ts := pumpSized(t, ui.Size{Width: 300, Height: 100}, NewButton("Open", ButtonOpts{OnPressed: func() {}, Tooltip: "Open the file"}))
	ts.Hover(ts.Center(gluttertest.ByType[*Button]()))
	ts.Pump(DefaultHoverDelay + time.Millisecond)
	if !ts.Exists(gluttertest.ByText("Open the file")) {
		t.Error("the tooltip is not shown")
	}
}
