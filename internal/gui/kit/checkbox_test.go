package kit

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

func TestCheckboxActivation(t *testing.T) {
	space, enter := services.LogicalKeyboardKeySpace, services.LogicalKeyboardKeyEnter
	cases := []struct {
		name string
		opts func(on func(bool)) CheckboxOpts
		act  func(ts *gluttertest.Tester)
		want []bool
	}{
		{"a click on the box", func(on func(bool)) CheckboxOpts { return CheckboxOpts{OnChanged: on} },
			func(ts *gluttertest.Tester) {
				ts.TapAt(ts.TopLeft(gluttertest.ByType[*Checkbox]()).Add(ui.Offset{Dx: 9, Dy: 9}))
			}, []bool{true}},
		{"a click on the label", func(on func(bool)) CheckboxOpts { return CheckboxOpts{OnChanged: on, Checked: true} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByText("Remember")) }, []bool{false}},
		{"a click on a disabled checkbox", func(on func(bool)) CheckboxOpts { return CheckboxOpts{OnChanged: on, Disabled: true} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByText("Remember")) }, nil},
		{"a click without a handler", func(func(bool)) CheckboxOpts { return CheckboxOpts{} },
			func(ts *gluttertest.Tester) { ts.Tap(gluttertest.ByText("Remember")) }, nil},
		{"Space", func(on func(bool)) CheckboxOpts { return CheckboxOpts{OnChanged: on, Autofocus: true} },
			func(ts *gluttertest.Tester) { key(ts, space) }, []bool{true}},
		{"Enter", func(on func(bool)) CheckboxOpts { return CheckboxOpts{OnChanged: on, Autofocus: true, Checked: true} },
			func(ts *gluttertest.Tester) { key(ts, enter) }, []bool{false}},
		{"Space on a disabled checkbox", func(on func(bool)) CheckboxOpts { return CheckboxOpts{OnChanged: on, Autofocus: true, Disabled: true} },
			func(ts *gluttertest.Tester) { key(ts, space) }, nil},
		{"another key", func(on func(bool)) CheckboxOpts { return CheckboxOpts{OnChanged: on, Autofocus: true} },
			func(ts *gluttertest.Tester) { key(ts, services.LogicalKeyboardKeyKeyA) }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []bool
			ts := pumpThemed(t, DarkModern(), ui.Size{Width: 240, Height: 80}, padded(NewCheckbox("Remember", tc.opts(func(v bool) { got = append(got, v) }))))
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

func TestCheckboxClickTakesTheFocus(t *testing.T) {
	node := widgets.NewFocusNode()
	defer node.Dispose()
	ts := pumpThemed(t, DarkModern(), ui.Size{Width: 240, Height: 80}, padded(NewCheckbox("Remember", CheckboxOpts{FocusNode: node, OnChanged: func(bool) {}})))
	ts.Tap(gluttertest.ByText("Remember"))
	ts.Pump(time.Millisecond)
	if !node.HasPrimaryFocus() {
		t.Error("a click did not give the checkbox the focus")
	}
	if got := borderOf(t, ts, gluttertest.ByType[*Checkbox]()); got != DarkModern().FocusBorder {
		t.Errorf("border %v with the focus", got)
	}
}

func TestCheckboxLook(t *testing.T) {
	for _, theme := range []struct {
		name   string
		colors Colors
	}{{"Dark Modern", DarkModern()}, {"Light Modern", LightModern()}} {
		c := theme.colors
		on := func(bool) {}
		cases := []struct {
			name       string
			opts       CheckboxOpts
			check      bool
			bg, border ui.Color
			iconColor  ui.Color
		}{
			{"unchecked", CheckboxOpts{OnChanged: on}, false, c.CheckboxBackground, c.CheckboxBorder, c.CheckboxForeground},
			{"checked", CheckboxOpts{OnChanged: on, Checked: true}, true, c.CheckboxBackground, c.CheckboxBorder, c.CheckboxForeground},
			{"checked and disabled", CheckboxOpts{OnChanged: on, Checked: true, Disabled: true}, true, c.CheckboxDisabledBackground, c.CheckboxDisabledBackground, c.CheckboxDisabledForeground},
			{"in a selected row", CheckboxOpts{OnChanged: on, Checked: true, Selected: true}, true, c.CheckboxSelectBackground, c.CheckboxSelectBorder, c.CheckboxForeground},
			{"with the focus", CheckboxOpts{OnChanged: on, Autofocus: true}, false, c.CheckboxBackground, c.FocusBorder, c.CheckboxForeground},
		}
		for _, tc := range cases {
			t.Run(theme.name+" "+tc.name, func(t *testing.T) {
				ts := pumpThemed(t, c, ui.Size{Width: 200, Height: 80}, padded(NewCheckbox("Label", tc.opts)))
				ts.Pump(time.Millisecond)
				root := gluttertest.ByType[*Checkbox]()
				if got := backgroundOf(t, ts, root); got != tc.bg {
					t.Errorf("background %v, want %v", got, tc.bg)
				}
				if got := borderOf(t, ts, root); got != tc.border {
					t.Errorf("border %v, want %v", got, tc.border)
				}
				icons := ts.Count(gluttertest.Descendant(root, gluttertest.ByType[*widgets.Icon]()))
				if (icons == 1) != tc.check {
					t.Errorf("%d check marks, want %v", icons, tc.check)
				}
				if tc.check {
					icon := ts.Widget(gluttertest.Descendant(root, gluttertest.ByType[*widgets.Icon]())).(*widgets.Icon)
					if got := icon.Color().OrElse(ui.Color{}); got != tc.iconColor {
						t.Errorf("check color %v, want %v", got, tc.iconColor)
					}
				}
				box := ts.Size(gluttertest.First(gluttertest.Descendant(root, gluttertest.ByType[*widgets.Container]())))
				if box.Width != checkboxSize || box.Height != checkboxSize {
					t.Errorf("box %v", box)
				}
				// the label is 9 px from the box
				if gap := ts.TopLeft(gluttertest.ByText("Label")).Dx - ts.TopLeft(root).Dx - checkboxSize; gap != checkboxLabelGap {
					t.Errorf("gap %v", gap)
				}
			})
		}
	}
}
