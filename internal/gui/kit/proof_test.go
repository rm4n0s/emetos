package kit

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// TestProofKit makes the proof image of step 6.5 (go test ./internal/gui/kit -run TestProofKit -proof): a
// gallery of the kit widgets (buttons, inputs, toggles, checkbox, dropdown, progress bars, badges, a menu)
// with a dialog on top of it.
func TestProofKit(t *testing.T) {
	if !testutil.ProofEnabled() {
		t.Skip("run with -proof")
	}
	noop := func(bool) {}
	press := func() {}
	history := NewHistory([]string{"main.go", "README.md"})
	find := NewInputBoxController("needle")
	name := NewInputBoxController("README.md")
	notes := NewInputBoxController("")
	for _, c := range []*InputBoxController{find, name, notes} {
		defer c.Dispose()
	}
	ctrl := NewDialogController()
	section := func(bc widgets.BuildContext, title string, children ...widgets.Widget) widgets.Widget {
		colors := ThemeOf(bc)
		head := widgets.NewText(title, widgets.TextOpts{Style: TextStyleOf(bc, 11, colors.DescriptionForeground)})
		out := []widgets.Widget{head, vgap(6)}
		out = append(out, children...)
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 0, 16), column(out...))
	}
	left := themed(func(bc widgets.BuildContext) widgets.Widget {
		w := foundation.Some(280.0)
		return column(
			section(bc, "BUTTONS",
				row(NewButton("Primary", ButtonOpts{OnPressed: press}), gap(8), NewButton("Secondary", ButtonOpts{OnPressed: press, Secondary: true}), gap(8),
					NewButton("Run", ButtonOpts{OnPressed: press, Icon: fonts.CodiconPlay}), gap(8), NewButton("Disabled", ButtonOpts{Disabled: true, OnPressed: press}))),
			section(bc, "INPUT BOX",
				NewInputBox(nil, nil, InputBoxOpts{Controller: find, Width: w, Placeholder: "Find", History: history, Trailing: []widgets.Widget{
					NewToggle(fonts.CodiconCaseSensitive, ToggleOpts{Checked: true, NotFocusable: true, OnChanged: noop}),
					NewToggle(fonts.CodiconWholeWord, ToggleOpts{NotFocusable: true, OnChanged: noop}),
					NewToggle(fonts.CodiconRegex, ToggleOpts{NotFocusable: true, OnChanged: noop}),
				}}), vgap(8),
				NewInputBox(nil, nil, InputBoxOpts{Controller: name, Width: w, Invalid: true}), vgap(8),
				NewInputBox(nil, nil, InputBoxOpts{Controller: notes, Width: w, Placeholder: "Notes (Ctrl+Enter to send)", MultiLine: true})),
			section(bc, "TOGGLE, CHECKBOX, DROPDOWN",
				row(NewToggle(fonts.CodiconWordWrap, ToggleOpts{Checked: true, OnChanged: noop}), gap(6), NewToggle(fonts.CodiconListSelection, ToggleOpts{OnChanged: noop}), gap(6),
					NewToggle(fonts.CodiconPreserveCase, ToggleOpts{Disabled: true, OnChanged: noop}), gap(18),
					NewCheckbox("Auto save", CheckboxOpts{Checked: true, OnChanged: noop}), gap(14), NewCheckbox("Wrap", CheckboxOpts{OnChanged: noop})), vgap(8),
				NewDropdown(fruit, DropdownOpts{Selected: 2, Width: foundation.Some(200.0), OnChanged: func(int) {}})),
			section(bc, "PROGRESS BAR AND BADGE",
				widgets.NewSizedBox(NewProgressBar(ProgressBarOpts{Total: 100, Worked: 60}), widgets.SizedBoxOpts{Width: foundation.Some(280.0)}), vgap(8),
				widgets.NewSizedBox(NewProgressBar(ProgressBarOpts{Infinite: true}), widgets.SizedBoxOpts{Width: foundation.Some(280.0)}), vgap(10),
				row(NewBadge(3), gap(8), NewBadge(42), gap(8), NewBadge(1280))),
		)
	})
	right := themed(func(bc widgets.BuildContext) widgets.Widget {
		return section(bc, "MENU",
			NewMenu(goldenMenuItems(), MenuOpts{OnRun: func(MenuItem) {}}))
	})
	content := widgets.NewPadding(painting.NewEdgeInsetsAll(20),
		widgets.NewRow([]widgets.Widget{left, gap(36), right}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}))
	size := ui.Size{Width: 760, Height: 880}
	host := NewDialogHost(content, ctrl)
	ts := pumpThemed(t, DarkModern(), size, widgets.NewSizedBox(host, widgets.SizedBoxOpts{Width: foundation.Some(size.Width), Height: foundation.Some(size.Height)}))
	ts.EnterText(gluttertest.First(gluttertest.ByType[*InputBox]()), "needle")
	ts.Pump(time.Millisecond)
	ts.Pump(2 * time.Second)
	ctrl.Show(DialogSpec{
		Type: DialogWarning, Message: "Do you want to save the changes you made to main.go?", Detail: "Your changes will be lost if you don't save them.",
		Buttons: []string{"&&Save", "Do&&n't Save", "Cancel"}, CancelButton: foundation.Some(2),
		CheckboxLabel: "Remember my choice",
	})
	for range 3 {
		ts.Pump(time.Millisecond)
	}
	ts.SendKeyEvent(services.LogicalKeyboardKeyTab)
	ts.Pump(time.Millisecond)
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	testutil.Proof(t, ts, "6.5-kit.png")
}
