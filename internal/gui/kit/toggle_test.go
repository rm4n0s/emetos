package kit

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

func TestToggleActivation(t *testing.T) {
	space, enter := services.LogicalKeyboardKeySpace, services.LogicalKeyboardKeyEnter
	cases := []struct {
		name string
		opts func(on func(bool)) ToggleOpts
		act  func(ts *gluttertest.Tester)
		want []bool
	}{
		{"a click on an unchecked toggle", func(on func(bool)) ToggleOpts { return ToggleOpts{OnChanged: on} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByType[*Toggle]()) }, []bool{true}},
		{"a click on a checked toggle", func(on func(bool)) ToggleOpts { return ToggleOpts{Checked: true, OnChanged: on} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByType[*Toggle]()) }, []bool{false}},
		{"a click on a disabled toggle", func(on func(bool)) ToggleOpts { return ToggleOpts{Disabled: true, OnChanged: on} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByType[*Toggle]()) }, nil},
		{"Space", func(on func(bool)) ToggleOpts { return ToggleOpts{Autofocus: true, OnChanged: on} },
			func(ts *gluttertest.Tester) { key(ts, space) }, []bool{true}},
		{"Enter", func(on func(bool)) ToggleOpts { return ToggleOpts{Autofocus: true, Checked: true, OnChanged: on} },
			func(ts *gluttertest.Tester) { key(ts, enter) }, []bool{false}},
		{"another key", func(on func(bool)) ToggleOpts { return ToggleOpts{Autofocus: true, OnChanged: on} },
			func(ts *gluttertest.Tester) { key(ts, services.LogicalKeyboardKeyKeyA) }, nil},
		{"Space on a disabled toggle", func(on func(bool)) ToggleOpts { return ToggleOpts{Autofocus: true, Disabled: true, OnChanged: on} },
			func(ts *gluttertest.Tester) { key(ts, space) }, nil},
		{"Space on a toggle that is not focusable", func(on func(bool)) ToggleOpts { return ToggleOpts{Autofocus: true, NotFocusable: true, OnChanged: on} },
			func(ts *gluttertest.Tester) { key(ts, space) }, nil},
		{"a click on a toggle that is not focusable", func(on func(bool)) ToggleOpts { return ToggleOpts{NotFocusable: true, OnChanged: on} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByType[*Toggle]()) }, []bool{true}},
		{"a click on a toggle without a handler", func(func(bool)) ToggleOpts { return ToggleOpts{} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByType[*Toggle]()) }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []bool
			ts := pumpControl(t, NewToggle(fonts.CodiconCaseSensitive, tc.opts(func(v bool) { got = append(got, v) })))
			tc.act(ts)
			ts.Pump(time.Millisecond)
			if len(got) != len(tc.want) || (len(got) == 1 && got[0] != tc.want[0]) {
				t.Errorf("reported %v, want %v", got, tc.want)
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestToggleFocus(t *testing.T) {
	node := widgets.NewFocusNode()
	defer node.Dispose()
	other := widgets.NewFocusNode()
	defer other.Dispose()
	var changes []bool
	box := column(
		NewToggle(fonts.CodiconRegex, ToggleOpts{FocusNode: node, OnChanged: func(bool) {}, OnFocusChange: func(f bool) { changes = append(changes, f) }}),
		widgets.NewFocus(widgets.NewSizedBox(nil), widgets.FocusOpts{FocusNode: other}),
	)
	ts := pumpControl(t, box)
	other.RequestFocus()
	ts.Pump(time.Millisecond)
	// a click flips the toggle but leaves the focus where it was
	ts.Tap(gluttertest.ByType[*Toggle]())
	ts.Pump(time.Millisecond)
	if !other.HasPrimaryFocus() || node.HasPrimaryFocus() {
		t.Error("a click moved the focus to the toggle")
	}
	// a key flips it only with the focus; Tab traversal can get there
	node.RequestFocus()
	ts.Pump(time.Millisecond)
	if !node.HasPrimaryFocus() || len(changes) == 0 || !changes[len(changes)-1] {
		t.Errorf("focus %v, reports %v", node.HasPrimaryFocus(), changes)
	}
	// a disabled toggle cannot take the focus
	ts2 := pumpControl(t, NewToggle(fonts.CodiconRegex, ToggleOpts{FocusNode: node, Disabled: true, OnChanged: func(bool) {}}))
	node.RequestFocus()
	ts2.Pump(time.Millisecond)
	if node.HasPrimaryFocus() {
		t.Error("a disabled toggle took the focus")
	}
}

func TestToggleLook(t *testing.T) {
	for _, theme := range []struct {
		name   string
		colors Colors
	}{{"Dark Modern", DarkModern()}, {"Light Modern", LightModern()}} {
		c := theme.colors
		cases := []struct {
			name       string
			opts       ToggleOpts
			hover      bool
			bg, border ui.Color
			opacity    bool
		}{
			{"off", ToggleOpts{OnChanged: func(bool) {}}, false, ui.NewColor(0), ui.NewColor(0), false},
			{"off under the pointer", ToggleOpts{OnChanged: func(bool) {}}, true, c.InputOptionHoverBackground, ui.NewColor(0), false},
			{"on", ToggleOpts{Checked: true, OnChanged: func(bool) {}}, false, c.InputOptionActiveBackground, c.InputOptionActiveBorder, false},
			{"on under the pointer", ToggleOpts{Checked: true, OnChanged: func(bool) {}}, true, c.InputOptionActiveBackground, c.InputOptionActiveBorder, false},
			{"with the focus", ToggleOpts{Autofocus: true, OnChanged: func(bool) {}}, false, ui.NewColor(0), c.FocusBorder, false},
			{"on with the focus", ToggleOpts{Autofocus: true, Checked: true, OnChanged: func(bool) {}}, false, c.InputOptionActiveBackground, c.FocusBorder, false},
			{"disabled", ToggleOpts{Disabled: true, OnChanged: func(bool) {}}, true, ui.NewColor(0), ui.NewColor(0), true},
			{"without a handler under the pointer", ToggleOpts{}, true, ui.NewColor(0), ui.NewColor(0), false},
		}
		for _, tc := range cases {
			t.Run(theme.name+" "+tc.name, func(t *testing.T) {
				ts := pumpThemed(t, c, ui.Size{Width: 60, Height: 80}, padded(NewToggle(fonts.CodiconCaseSensitive, tc.opts)))
				if tc.hover {
					ts.Hover(ts.Center(gluttertest.ByType[*Toggle]()))
				}
				ts.Pump(time.Millisecond)
				root := gluttertest.ByType[*Toggle]()
				if got := backgroundOf(t, ts, root); got != tc.bg {
					t.Errorf("background %v, want %v", got, tc.bg)
				}
				if got := borderOf(t, ts, root); got != tc.border {
					t.Errorf("border %v, want %v", got, tc.border)
				}
				if got := ts.Exists(gluttertest.Descendant(root, gluttertest.ByType[*widgets.Opacity]())); got != tc.opacity {
					t.Errorf("dimmed %v, want %v", got, tc.opacity)
				}
				if got := ts.Size(root); got.Width != toggleSize || got.Height != toggleSize {
					t.Errorf("size %v", got)
				}
			})
		}
	}
}

func TestToggleTooltip(t *testing.T) {
	ts := pumpThemed(t, DarkModern(), ui.Size{Width: 200, Height: 100}, padded(NewToggle(fonts.CodiconRegex, ToggleOpts{OnChanged: func(bool) {}, Tooltip: "Use Regular Expression (Alt+R)"})))
	ts.Hover(ts.Center(gluttertest.ByType[*Toggle]()))
	ts.Pump(DefaultHoverDelay / 2)
	if ts.Exists(gluttertest.ByText("Use Regular Expression (Alt+R)")) {
		t.Error("the tooltip shows before the delay")
	}
	ts.Pump(DefaultHoverDelay)
	if !ts.Exists(gluttertest.ByText("Use Regular Expression (Alt+R)")) {
		t.Error("no tooltip after the delay")
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}
