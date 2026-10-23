package kit

import (
	"fmt"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// pumpControl shows a control in a small window with a theme that uses the test font.
func pumpControl(t *testing.T, child widgets.Widget) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, ui.Size{Width: 240, Height: 60})
	ts.PumpWidget(widgets.NewWidgetsApp(DarkModern().EditorBackground, widgets.WidgetsAppOpts{
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				return NewTheme(DarkModern(), widgets.NewAlign(child), ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	ts.Pump(time.Millisecond)
	return ts
}

func TestIconButtonCallsOnPressed(t *testing.T) {
	cases := []struct {
		name string
		opts func(pressed *int) IconButtonOpts
		want int
	}{
		{"enabled", func(p *int) IconButtonOpts { return IconButtonOpts{OnPressed: func() { *p++ }} }, 1},
		{"disabled", func(p *int) IconButtonOpts { return IconButtonOpts{OnPressed: func() { *p++ }, Disabled: true} }, 0},
		{"without a handler", func(*int) IconButtonOpts { return IconButtonOpts{} }, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pressed int
			b := NewIconButton(fonts.CodiconClose, tc.opts(&pressed))
			ts := pumpControl(t, b)
			ts.Tap(gluttertest.ByType[*IconButton]())
			ts.Pump(time.Millisecond)
			if pressed != tc.want {
				t.Errorf("pressed %d times, want %d", pressed, tc.want)
			}
			if b.Code() != fonts.CodiconClose {
				t.Errorf("code %x", b.Code())
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestToggleReportsTheNewValue(t *testing.T) {
	for _, checked := range []bool{false, true} {
		var got []bool
		tg := NewToggle(fonts.CodiconRegex, ToggleOpts{Checked: checked, OnChanged: func(v bool) { got = append(got, v) }})
		ts := pumpControl(t, tg)
		ts.Tap(gluttertest.ByType[*Toggle]())
		if len(got) != 1 || got[0] == checked {
			t.Errorf("checked %v: reported %v", checked, got)
		}
		if tg.Checked() != checked || tg.Code() != fonts.CodiconRegex {
			t.Errorf("accessors: %v %x", tg.Checked(), tg.Code())
		}
	}
	// a toggle without a handler does nothing
	ts := pumpControl(t, NewToggle(fonts.CodiconRegex))
	ts.Tap(gluttertest.ByType[*Toggle]())
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestInputBox(t *testing.T) {
	controller := widgets.NewTextEditingController()
	node := widgets.NewFocusNode(widgets.FocusNodeOpts{})
	defer node.Dispose()
	var changed []string
	var focus []bool
	box := NewInputBox(controller, node, InputBoxOpts{
		Placeholder: "Find", OnChanged: func(s string) { changed = append(changed, s) },
		OnFocusChange: func(f bool) { focus = append(focus, f) },
	})
	ts := pumpControl(t, widgets.NewSizedBox(box, widgets.SizedBoxOpts{}))
	if !ts.Exists(gluttertest.ByText("Find")) {
		t.Error("the placeholder is not shown while the box is empty")
	}
	ts.EnterText(gluttertest.ByType[*widgets.EditableText](), "abc")
	if ts.Exists(gluttertest.ByText("Find")) {
		t.Error("the placeholder is shown over text")
	}
	if len(changed) == 0 || changed[len(changed)-1] != "abc" || controller.Text() != "abc" {
		t.Errorf("changed %v, text %q", changed, controller.Text())
	}
	if len(focus) == 0 || !focus[len(focus)-1] || !node.HasFocus() {
		t.Errorf("focus %v, node %v", focus, node.HasFocus())
	}
	// text set from outside shows without the box reporting a change
	n := len(changed)
	controller.SetText("")
	ts.Pump(time.Millisecond)
	if len(changed) != n || !ts.Exists(gluttertest.ByText("Find")) {
		t.Errorf("a change from outside: %d reports, placeholder %v", len(changed)-n, ts.Exists(gluttertest.ByText("Find")))
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestIconButtonStates(t *testing.T) {
	c := DarkModern()
	transparent := ui.Color{}
	cases := []struct {
		name   string
		opts   IconButtonOpts
		act    string // "", "hover", "press" or "focus"
		bg     ui.Color
		border ui.Color
	}{
		{"idle", IconButtonOpts{OnPressed: func() {}}, "", transparent, transparent},
		{"under the pointer", IconButtonOpts{OnPressed: func() {}}, "hover", c.ToolbarHoverBackground, transparent},
		{"pressed", IconButtonOpts{OnPressed: func() {}}, "press", c.ToolbarActiveBackground, transparent},
		{"disabled under the pointer", IconButtonOpts{OnPressed: func() {}, Disabled: true}, "hover", transparent, transparent},
		{"without a handler under the pointer", IconButtonOpts{}, "hover", transparent, transparent},
		{"checked", IconButtonOpts{OnPressed: func() {}, Checked: true}, "", c.ActionBarToggledBackground, transparent},
		{"checked under the pointer stays toggled", IconButtonOpts{OnPressed: func() {}, Checked: true}, "hover", c.ActionBarToggledBackground, transparent},
		{"with the focus", IconButtonOpts{OnPressed: func() {}, Autofocus: true}, "", transparent, c.FocusBorder},
		{"disabled with the focus asked for", IconButtonOpts{OnPressed: func() {}, Autofocus: true, Disabled: true}, "", transparent, transparent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := pumpControl(t, NewIconButton(fonts.CodiconClose, tc.opts))
			root := gluttertest.ByType[*IconButton]()
			switch tc.act {
			case "hover":
				ts.Hover(ts.Center(root))
			case "press":
				ts.StartGesture(ts.Center(root))
			}
			ts.Pump(time.Millisecond)
			if got := backgroundOf(t, ts, root); got != tc.bg {
				t.Errorf("background %v, want %v", got, tc.bg)
			}
			if got := borderOf(t, ts, root); got != tc.border {
				t.Errorf("border %v, want %v", got, tc.border)
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestIconButtonKeyboard(t *testing.T) {
	cases := []struct {
		name string
		opts IconButtonOpts
		key  services.LogicalKeyboardKey
		want int
	}{
		{"Enter", IconButtonOpts{Autofocus: true}, services.LogicalKeyboardKeyEnter, 1},
		{"Space", IconButtonOpts{Autofocus: true}, services.LogicalKeyboardKeySpace, 1},
		{"another key", IconButtonOpts{Autofocus: true}, services.LogicalKeyboardKeyKeyA, 0},
		{"disabled", IconButtonOpts{Autofocus: true, Disabled: true}, services.LogicalKeyboardKeyEnter, 0},
		{"without the focus", IconButtonOpts{}, services.LogicalKeyboardKeyEnter, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var pressed int
			tc.opts.OnPressed = func() { pressed++ }
			ts := pumpControl(t, NewIconButton(fonts.CodiconClose, tc.opts))
			key(ts, tc.key)
			if pressed != tc.want {
				t.Errorf("pressed %d times, want %d", pressed, tc.want)
			}
		})
	}
	t.Run("a click does not take the focus", func(t *testing.T) {
		var focus []bool
		ts := pumpControl(t, NewIconButton(fonts.CodiconClose, IconButtonOpts{OnPressed: func() {}, OnFocusChange: func(f bool) { focus = append(focus, f) }}))
		ts.Tap(gluttertest.ByType[*IconButton]())
		ts.Pump(time.Millisecond)
		if len(focus) != 0 {
			t.Errorf("focus changes %v", focus)
		}
	})
	t.Run("the focus is reported", func(t *testing.T) {
		var focus []bool
		node := widgets.NewFocusNode(widgets.FocusNodeOpts{})
		defer node.Dispose()
		ts := pumpControl(t, NewIconButton(fonts.CodiconClose, IconButtonOpts{OnPressed: func() {}, FocusNode: node, OnFocusChange: func(f bool) { focus = append(focus, f) }}))
		node.RequestFocus()
		ts.Pump(time.Millisecond)
		node.Unfocus()
		ts.Pump(time.Millisecond)
		if fmt.Sprint(focus) != "[true false]" {
			t.Errorf("focus changes %v", focus)
		}
	})
}

func TestIconButtonTooltip(t *testing.T) {
	cases := []struct {
		name    string
		opts    IconButtonOpts
		wantTip bool
	}{
		{"shown after the delay", IconButtonOpts{OnPressed: func() {}, Tooltip: "Close"}, true},
		{"shown for a disabled button too", IconButtonOpts{Disabled: true, Tooltip: "Close"}, true},
		{"without a tooltip", IconButtonOpts{OnPressed: func() {}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewIconButton(fonts.CodiconClose, tc.opts)
			ts := pumpSized(t, ui.Size{Width: 240, Height: 80}, b)
			ts.Hover(ts.Center(gluttertest.ByType[*IconButton]()))
			ts.Pump(DefaultHoverDelay - time.Millisecond)
			if shown(ts, "Close") {
				t.Error("shown before the delay")
			}
			ts.Pump(2 * time.Millisecond)
			if shown(ts, "Close") != tc.wantTip {
				t.Errorf("tooltip shown: %v, want %v", shown(ts, "Close"), tc.wantTip)
			}
			if b.Tooltip() != tc.opts.Tooltip {
				t.Errorf("Tooltip() %q", b.Tooltip())
			}
		})
	}
}
