package kit

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// inputRow is a labelled widget of a golden: the label is the state it shows.
func labelled(bc widgets.BuildContext, name string, w widgets.Widget) widgets.Widget {
	colors := ThemeOf(bc)
	return row(
		widgets.NewSizedBox(widgets.NewText(name, widgets.TextOpts{Style: TextStyleOf(bc, 12, colors.DescriptionForeground)}), widgets.SizedBoxOpts{Width: foundation.Some(96.0)}),
		w)
}

// themed builds a widget that needs the theme from a function of the build context.
func themed(build func(bc widgets.BuildContext) widgets.Widget) widgets.Widget {
	return widgets.NewBuilder(build)
}

func TestGoldenInputBox(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name+" states", func(t *testing.T) {
			text := func(s string) *widgets.TextEditingController { return widgets.NewTextEditingController(s) }
			plain, filled, focused, invalid, disabled, flags := text(""), text("src/main.go"), text("needle"), text("no match"), text("fixed text"), text("Aa")
			for _, c := range []*widgets.TextEditingController{plain, filled, focused, invalid, disabled, flags} {
				defer c.Dispose()
			}
			focusNode := widgets.NewFocusNode()
			defer focusNode.Dispose()
			w := foundation.Some(220.0)
			content := padded(themed(func(bc widgets.BuildContext) widgets.Widget {
				return column(
					labelled(bc, "placeholder", NewInputBox(plain, nil, InputBoxOpts{Placeholder: "Search", Width: w})), vgap(10),
					labelled(bc, "text", NewInputBox(filled, nil, InputBoxOpts{Width: w})), vgap(10),
					labelled(bc, "focus", NewInputBox(focused, focusNode, InputBoxOpts{Width: w, Autofocus: true})), vgap(10),
					labelled(bc, "invalid", NewInputBox(invalid, nil, InputBoxOpts{Width: w, Invalid: true})), vgap(10),
					labelled(bc, "disabled", NewInputBox(disabled, nil, InputBoxOpts{Width: w, Disabled: true})), vgap(10),
					labelled(bc, "toggles", NewInputBox(flags, nil, InputBoxOpts{Width: w, Trailing: []widgets.Widget{
						NewToggle(fonts.CodiconCaseSensitive, ToggleOpts{NotFocusable: true, Checked: true, OnChanged: func(bool) {}}),
						NewToggle(fonts.CodiconRegex, ToggleOpts{NotFocusable: true, OnChanged: func(bool) {}}),
					}})),
				)
			}))
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 360, Height: 290}, content)
			ts.Pump(time.Millisecond)
			ts.MatchesGolden("inputbox_states_" + th.name)
		})
		for _, m := range []struct {
			name string
			msg  InputMessage
		}{
			{"error", InputMessage{Type: ValidationError, Content: "The name is already used in this folder."}},
			{"warning", InputMessage{Type: ValidationWarning, Content: "The name starts with a dot: the file will be hidden."}},
			{"info", InputMessage{Type: ValidationInfo, Content: "Press Enter to confirm or Escape to cancel."}},
		} {
			t.Run(th.name+" "+m.name, func(t *testing.T) {
				ctrl := NewInputBoxController("README.md")
				defer ctrl.Dispose()
				msg := m.msg
				content := padded(NewInputBox(nil, nil, InputBoxOpts{Controller: ctrl, Width: foundation.Some(260.0), Message: &msg, Autofocus: true}))
				ts := pumpThemed(t, th.colors(), ui.Size{Width: 300, Height: 110}, content)
				ts.Pump(time.Millisecond)
				ts.Pump(time.Millisecond)
				if !ts.Exists(gluttertest.ByText(msg.Content)) {
					t.Fatal("the message is not shown")
				}
				ts.MatchesGolden("inputbox_" + m.name + "_" + th.name)
			})
		}
		t.Run(th.name+" multiline", func(t *testing.T) {
			ctrl := NewInputBoxController("")
			defer ctrl.Dispose()
			content := padded(NewInputBox(nil, nil, InputBoxOpts{
				Controller: ctrl, Width: foundation.Some(260.0), MultiLine: true, MaxHeight: foundation.Some(25.0 + 3*inputBoxLineHeight),
				Trailing: []widgets.Widget{NewToggle(fonts.CodiconRegex, ToggleOpts{NotFocusable: true, OnChanged: func(bool) {}})},
			}))
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 300, Height: 130}, content)
			ts.EnterText(gluttertest.ByType[*InputBox](), "Fix the crash on startup\n\nCloses #42\nSecond line of the body\nA fifth line")
			for range 3 {
				ts.Pump(50 * time.Millisecond)
			}
			if h := ts.Size(gluttertest.ByType[*InputBox]()).Height; h != 25+3*inputBoxLineHeight {
				t.Errorf("height %v: the box should stop growing at its maximum", h)
			}
			ts.MatchesGolden("inputbox_multiline_" + th.name)
		})
	}
}

func TestGoldenToggleAndCheckbox(t *testing.T) {
	noop := func(bool) {}
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			focusedToggle, focusedBox := widgets.NewFocusNode(), widgets.NewFocusNode()
			defer focusedToggle.Dispose()
			defer focusedBox.Dispose()
			content := padded(themed(func(bc widgets.BuildContext) widgets.Widget {
				return column(
					labelled(bc, "toggle", row(
						NewToggle(fonts.CodiconCaseSensitive, ToggleOpts{OnChanged: noop}), gap(6),
						NewToggle(fonts.CodiconWholeWord, ToggleOpts{OnChanged: noop}), gap(6),
						NewToggle(fonts.CodiconRegex, ToggleOpts{Checked: true, OnChanged: noop}), gap(6),
						NewToggle(fonts.CodiconPreserveCase, ToggleOpts{FocusNode: focusedToggle, OnChanged: noop}), gap(6),
						NewToggle(fonts.CodiconListSelection, ToggleOpts{Disabled: true, OnChanged: noop}),
					)), vgap(14),
					labelled(bc, "checkbox", row(
						NewCheckbox("", CheckboxOpts{OnChanged: noop}), gap(14),
						NewCheckbox("", CheckboxOpts{Checked: true, OnChanged: noop}), gap(14),
						NewCheckbox("", CheckboxOpts{FocusNode: focusedBox, Checked: true, OnChanged: noop}), gap(14),
						NewCheckbox("", CheckboxOpts{Disabled: true, OnChanged: noop}), gap(14),
						NewCheckbox("", CheckboxOpts{Disabled: true, Checked: true, OnChanged: noop}), gap(14),
						NewCheckbox("", CheckboxOpts{Selected: true, Checked: true, OnChanged: noop}),
					)), vgap(14),
					labelled(bc, "with a label", NewCheckbox("Remember my choice", CheckboxOpts{Checked: true, OnChanged: noop})),
				)
			}))
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 380, Height: 140}, content)
			// the fourth toggle and the third checkbox have the focus (one after the other is not
			// possible: the toggle takes it, the checkbox looks as the focus is on it by a request)
			focusedBox.RequestFocus()
			ts.Pump(time.Millisecond)
			ts.MatchesGolden("toggles_checkbox_" + th.name)
		})
	}
}

func TestGoldenToggleFocus(t *testing.T) {
	noop := func(bool) {}
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			content := padded(row(
				NewToggle(fonts.CodiconCaseSensitive, ToggleOpts{OnChanged: noop}), gap(6),
				NewToggle(fonts.CodiconWholeWord, ToggleOpts{Autofocus: true, OnChanged: noop}), gap(6),
				NewToggle(fonts.CodiconRegex, ToggleOpts{Checked: true, OnChanged: noop}),
			))
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 140, Height: 56}, content)
			ts.Pump(time.Millisecond)
			ts.MatchesGolden("toggle_focus_" + th.name)
		})
	}
}

func TestGoldenDropdown(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name+" closed", func(t *testing.T) {
			node := widgets.NewFocusNode()
			defer node.Dispose()
			noop := func(int) {}
			content := padded(themed(func(bc widgets.BuildContext) widgets.Widget {
				return column(
					labelled(bc, "closed", NewDropdown(fruit, DropdownOpts{Selected: 1, Width: foundation.Some(150.0), OnChanged: noop})), vgap(10),
					labelled(bc, "focus", NewDropdown(fruit, DropdownOpts{Selected: 2, Width: foundation.Some(150.0), FocusNode: node, OnChanged: noop})), vgap(10),
					labelled(bc, "placeholder", NewDropdown(fruit, DropdownOpts{Selected: -1, Placeholder: "Select...", Width: foundation.Some(150.0), OnChanged: noop})), vgap(10),
					labelled(bc, "disabled", NewDropdown(fruit, DropdownOpts{Selected: 0, Disabled: true, Width: foundation.Some(150.0), OnChanged: noop})),
				)
			}))
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 300, Height: 190}, content)
			node.RequestFocus()
			ts.Pump(time.Millisecond)
			ts.MatchesGolden("dropdown_closed_" + th.name)
		})
		t.Run(th.name+" open", func(t *testing.T) {
			r := newDropdownRig(t, th.colors(), ui.Size{Width: 240, Height: 200}, nil)
			r.ts.Tap(gluttertest.ByType[*Dropdown]())
			r.ts.Pump(time.Millisecond)
			r.press(services.LogicalKeyboardKeyArrowDown) // on Cherry
			if !r.isOpen() {
				t.Fatal("not open")
			}
			r.ts.MatchesGolden("dropdown_open_" + th.name)
		})
	}
}

func TestGoldenProgressBar(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			bar := func(o ProgressBarOpts) widgets.Widget {
				return widgets.NewSizedBox(NewProgressBar(o), widgets.SizedBoxOpts{Width: foundation.Some(240.0)})
			}
			content := padded(themed(func(bc widgets.BuildContext) widgets.Widget {
				return column(
					labelled(bc, "25 %", bar(ProgressBarOpts{Total: 100, Worked: 25})), vgap(14),
					labelled(bc, "50 %", bar(ProgressBarOpts{Total: 100, Worked: 50})), vgap(14),
					labelled(bc, "done", bar(ProgressBarOpts{Total: 8, Worked: 8})), vgap(14),
					labelled(bc, "infinite", bar(ProgressBarOpts{Infinite: true})),
				)
			}))
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 380, Height: 130}, content)
			// the infinite bit is at the middle of its trip, 2 s after the start
			ts.Pump(2 * time.Second)
			ts.MatchesGolden("progressbar_" + th.name)
		})
	}
}

func TestGoldenBadge(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			content := padded(row(
				NewBadge(1), gap(10), NewBadge(8), gap(10), NewBadge(42), gap(10), NewBadge(999), gap(10), NewBadge(12345), gap(10), NewBadge(3, BadgeOpts{Format: "{0}+"}),
			))
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 260, Height: 56}, content)
			ts.MatchesGolden("badge_" + th.name)
		})
	}
}
