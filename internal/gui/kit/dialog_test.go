package kit

import (
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

var windowSize = ui.Size{Width: 700, Height: 480}

// dialogRig is a window with a button below a DialogHost, to see what a dialog does to it.
type dialogRig struct {
	t       *testing.T
	ts      *gluttertest.Tester
	ctrl    *DialogController
	below   *widgets.FocusNode
	pressed int
	answers []DialogResult
}

func newDialogRig(t *testing.T, colors Colors, size ui.Size) *dialogRig {
	t.Helper()
	r := &dialogRig{t: t, ctrl: NewDialogController(), below: widgets.NewFocusNode()}
	t.Cleanup(r.below.Dispose)
	child := widgets.NewAlign(
		NewButton("Below", ButtonOpts{FocusNode: r.below, Autofocus: true, OnPressed: func() { r.pressed++ }}),
		widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})
	r.ts = pumpThemed(t, colors, size, widgets.NewSizedBox(NewDialogHost(child, r.ctrl), widgets.SizedBoxOpts{Width: foundation.Some(size.Width), Height: foundation.Some(size.Height)}))
	return r
}

// show queues a dialog that records its answer and lets the frames run.
func (r *dialogRig) show(spec DialogSpec) {
	r.t.Helper()
	spec.OnAnswer = func(a DialogResult) { r.answers = append(r.answers, a) }
	r.ctrl.Show(spec)
	r.settle()
}

func (r *dialogRig) settle() {
	r.t.Helper()
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
}

func (r *dialogRig) press(k services.LogicalKeyboardKey) {
	r.t.Helper()
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	r.settle()
}

func (r *dialogRig) pressWith(k services.LogicalKeyboardKey, modifier services.LogicalKeyboardKey) {
	r.t.Helper()
	r.ts.SendKeyDownEvent(modifier)
	r.press(k)
	r.ts.SendKeyUpEvent(modifier)
	r.settle()
}

// button is the finder of a button of the dialog by its label.
func dialogButton(label string) gluttertest.Finder {
	return gluttertest.Descendant(gluttertest.ByType[*Dialog](), gluttertest.ByPredicate("button "+label, func(w widgets.Widget) bool {
		b, ok := w.(*Button)
		return ok && b.Label() == label
	}))
}

// focusedLabel is the label of the button of the dialog that has the focus, "checkbox" for the
// checkbox and "" for none.
func (r *dialogRig) focusedLabel() string {
	r.t.Helper()
	for _, b := range r.ts.Widgets(gluttertest.Descendant(gluttertest.ByType[*Dialog](), gluttertest.ByType[*Button]())) {
		label := b.(*Button).Label()
		el := r.ts.Element(dialogButton(label))
		if focusIn(el) {
			return label
		}
	}
	if el := r.ts.Elements(gluttertest.Descendant(gluttertest.ByType[*Dialog](), gluttertest.ByType[*Checkbox]())); len(el) > 0 && focusIn(el[0]) {
		return "checkbox"
	}
	return ""
}

// focusIn says whether the primary focus is inside the element.
func focusIn(el widgets.Element) bool {
	has := false
	var visit func(e widgets.Element)
	visit = func(e widgets.Element) {
		if has {
			return
		}
		if f, ok := e.Widget().(*widgets.Focus); ok {
			if n := f.FocusNode(); n != nil && n.HasPrimaryFocus() {
				has = true
				return
			}
		}
		e.VisitChildren(visit)
	}
	visit(el)
	return has
}

func (r *dialogRig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("errors: %v", errs)
	}
}

func (r *dialogRig) dialogShown() bool { return r.ts.Exists(gluttertest.ByType[*Dialog]()) }

var saveSpec = DialogSpec{
	Type: DialogWarning, Message: "Do you want to save the changes?", Detail: "Your changes will be lost if you don't save them.",
	Buttons: []string{"&&Save", "Do&&n't Save", "Cancel"}, CancelButton: foundation.Some(2),
}

func TestDialogButtonOrder(t *testing.T) {
	cases := []struct {
		name   string
		count  int
		cancel foundation.Opt[int]
		want   []int
	}{
		{"none", 0, foundation.None[int](), []int{}},
		{"one button", 1, foundation.None[int](), []int{0}},
		{"two buttons, no cancel button: reversed", 2, foundation.None[int](), []int{1, 0}},
		{"two buttons, the second cancels", 2, foundation.Some(1), []int{1, 0}},
		{"two buttons, the first cancels", 2, foundation.Some(0), []int{0, 1}},
		{"three buttons, no cancel button: reversed", 3, foundation.None[int](), []int{2, 1, 0}},
		{"three buttons, the last cancels", 3, foundation.Some(2), []int{1, 2, 0}},
		{"three buttons, the middle cancels", 3, foundation.Some(1), []int{2, 1, 0}},
		{"four buttons, the last cancels", 4, foundation.Some(3), []int{2, 1, 3, 0}},
		{"a cancel button out of range is ignored", 3, foundation.Some(7), []int{2, 1, 0}},
	}
	for _, tc := range cases {
		if got := DialogButtonOrder(tc.count, tc.cancel); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDialogButtonLabel(t *testing.T) {
	cases := map[string]string{"&&Save": "Save", "Do&&n't Save": "Don't Save", "Cancel": "Cancel", "Save (&&S)": "Save ", "A & B": "A & B"}
	for in, want := range cases {
		if got := dialogButtonLabel(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestDialogContent(t *testing.T) {
	r := newDialogRig(t, DarkModern(), windowSize)
	r.show(saveSpec)
	for _, text := range []string{"Do you want to save the changes?", "Your changes will be lost if you don't save them.", "Save", "Don't Save", "Cancel"} {
		if !r.ts.Exists(gluttertest.ByText(text)) {
			t.Errorf("no %q", text)
		}
	}
	// the buttons are in the order of Linux: Don't Save, Cancel, Save
	x := func(label string) float64 { return r.ts.TopLeft(dialogButton(label)).Dx }
	if !(x("Don't Save") < x("Cancel") && x("Cancel") < x("Save")) {
		t.Errorf("buttons at %v %v %v", x("Don't Save"), x("Cancel"), x("Save"))
	}
	// Save is the primary button, the others are secondary
	for label, secondary := range map[string]bool{"Save": false, "Don't Save": true, "Cancel": true} {
		if got := r.ts.Widget(dialogButton(label)).(*Button).Secondary(); got != secondary {
			t.Errorf("%s secondary %v", label, got)
		}
	}
	// the close button, the icon and the default focus
	if !r.ts.Exists(gluttertest.Descendant(gluttertest.ByType[*Dialog](), gluttertest.ByType[*IconButton]())) {
		t.Error("no close button")
	}
	if got := r.focusedLabel(); got != "Save" {
		t.Errorf("focus on %q, want the default button", got)
	}
	// the box is 480 wide and in the middle of the window
	box := gluttertest.First(gluttertest.Descendant(gluttertest.ByType[*Dialog](), gluttertest.ByType[*widgets.Container]()))
	size, at := r.ts.Size(box), r.ts.TopLeft(box)
	if size.Width != dialogWidth || !near(at.Dx+size.Width/2, windowSize.Width/2) || !near(at.Dy+size.Height/2, windowSize.Height/2) {
		t.Errorf("box %v at %v", size, at)
	}
	r.noErrors()
}

func TestDialogWithoutDetailShowsTheMessageAsText(t *testing.T) {
	cases := []struct {
		name string
		spec DialogSpec
		bold bool
	}{
		{"a message alone is regular text", DialogSpec{Message: "Done."}, false},
		{"a message with a detail is a bold title", DialogSpec{Message: "Done.", Detail: "All of it."}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newDialogRig(t, DarkModern(), windowSize)
			r.show(tc.spec)
			text := r.ts.Widget(gluttertest.ByText("Done.")).(*widgets.Text)
			if got := text.Style().FontWeight == ui.FontWeightW600; got != tc.bold {
				t.Errorf("bold %v, want %v", got, tc.bold)
			}
			if tc.spec.Detail != "" && !r.ts.Exists(gluttertest.ByText("All of it.")) {
				t.Error("no detail")
			}
			// the single default button is OK
			if !r.ts.Exists(dialogButton("OK")) {
				t.Error("no OK button")
			}
			r.noErrors()
		})
	}
}

func TestDialogAnswers(t *testing.T) {
	enter, esc, tab := services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyEscape, services.LogicalKeyboardKeyTab
	right, left := services.LogicalKeyboardKeyArrowRight, services.LogicalKeyboardKeyArrowLeft
	space := services.LogicalKeyboardKeySpace
	withBox := saveSpec
	withBox.CheckboxLabel = "Do not ask again"
	cases := []struct {
		name string
		spec DialogSpec
		act  func(r *dialogRig)
		want DialogResult
	}{
		{"Enter presses the default button", saveSpec, func(r *dialogRig) { r.press(enter) }, DialogResult{Button: 0}},
		{"Space presses the focused button", saveSpec, func(r *dialogRig) { r.press(space) }, DialogResult{Button: 0}},
		{"Escape cancels with the cancel button", saveSpec, func(r *dialogRig) { r.press(esc) }, DialogResult{Button: 2, Cancelled: true}},
		{"Escape without a cancel button answers -1", DialogSpec{Message: "x", Buttons: []string{"A", "B"}}, func(r *dialogRig) { r.press(esc) }, DialogResult{Button: -1, Cancelled: true}},
		{"the close button cancels", saveSpec, func(r *dialogRig) {
			r.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*Dialog](), gluttertest.ByType[*IconButton]()))
			r.settle()
		}, DialogResult{Button: 2, Cancelled: true}},
		{"a click on a button", saveSpec, func(r *dialogRig) { r.ts.Tap(dialogButton("Don't Save")); r.settle() }, DialogResult{Button: 1}},
		{"Tab from the default button, the last on the screen, wraps to the first", saveSpec, func(r *dialogRig) { r.press(tab); r.press(enter) }, DialogResult{Button: 1}},
		{"Tab twice", saveSpec, func(r *dialogRig) { r.press(tab); r.press(tab); r.press(enter) }, DialogResult{Button: 2}},
		{"Shift+Tab goes back", saveSpec, func(r *dialogRig) { r.pressWith(tab, services.LogicalKeyboardKeyShift); r.press(enter) }, DialogResult{Button: 2}},
		{"Left goes back, Right forward", saveSpec, func(r *dialogRig) { r.press(left); r.press(left); r.press(right); r.press(enter) }, DialogResult{Button: 2}},
		{"Tab with a checkbox goes through it", withBox, func(r *dialogRig) { r.press(tab); r.press(space); r.press(tab); r.press(enter) }, DialogResult{Button: 1, Checked: true}},
		{"Enter on the checkbox flips it", withBox, func(r *dialogRig) {
			r.press(tab)
			r.press(enter)
			r.press(tab)
			r.press(tab)
			r.press(tab)
			r.press(enter)
		}, DialogResult{Button: 0, Checked: true}},
		{"Escape on the checkbox cancels and keeps its state", withBox, func(r *dialogRig) { r.press(tab); r.press(space); r.press(esc) }, DialogResult{Button: 2, Cancelled: true, Checked: true}},
		{"a click on the label of the checkbox", withBox, func(r *dialogRig) {
			r.ts.Tap(gluttertest.ByText("Do not ask again"))
			r.settle()
			r.press(tab)
			r.press(enter)
		}, DialogResult{Button: 1, Checked: true}},
		{"a checkbox checked at first", DialogSpec{Message: "x", CheckboxLabel: "Again", CheckboxChecked: true}, func(r *dialogRig) { r.press(enter) }, DialogResult{Checked: true}},
		{"a checkbox flipped twice", withBox, func(r *dialogRig) { r.press(tab); r.press(space); r.press(space); r.press(tab); r.press(enter) }, DialogResult{Button: 1}},
		{"Escape does nothing in a dialog that cannot be closed", DialogSpec{Message: "x", DisableClose: true, Buttons: []string{"A", "B"}}, func(r *dialogRig) {
			r.press(esc)
			if len(r.answers) != 0 || !r.dialogShown() {
				r.t.Errorf("Escape closed a dialog that cannot be closed: %v", r.answers)
			}
			r.press(enter)
		}, DialogResult{Button: 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newDialogRig(t, DarkModern(), windowSize)
			r.show(tc.spec)
			tc.act(r)
			if len(r.answers) != 1 || r.answers[0] != tc.want {
				t.Errorf("answers %+v, want [%+v]", r.answers, tc.want)
			}
			if r.dialogShown() || r.ctrl.IsShowing() {
				t.Error("the dialog is still showing")
			}
			r.noErrors()
		})
	}
}

func TestDialogCloseButtonIsLeftOutWhenTheDialogCannotBeClosed(t *testing.T) {
	r := newDialogRig(t, DarkModern(), windowSize)
	r.show(DialogSpec{Message: "x", DisableClose: true})
	if r.ts.Exists(gluttertest.Descendant(gluttertest.ByType[*Dialog](), gluttertest.ByType[*IconButton]())) {
		t.Error("a close button")
	}
	r.ctrl.Dismiss()
	r.settle()
	if len(r.answers) != 1 || !r.answers[0].Cancelled || r.dialogShown() {
		t.Errorf("Dismiss: answers %+v", r.answers)
	}
	r.noErrors()
}

func TestDialogFocusCycleStaysInside(t *testing.T) {
	tab := services.LogicalKeyboardKeyTab
	spec := saveSpec
	spec.CheckboxLabel = "Do not ask again"
	r := newDialogRig(t, DarkModern(), windowSize)
	r.show(spec)
	// the ring is the checkbox, Don't Save, Cancel, Save; the focus starts on Save
	want := []string{"Save", "checkbox", "Don't Save", "Cancel", "Save", "checkbox"}
	for i, w := range want {
		if i > 0 {
			r.press(tab)
		}
		if got := r.focusedLabel(); got != w {
			t.Fatalf("step %d: focus on %q, want %q", i, got, w)
		}
	}
	// Shift+Tab goes the other way
	r.pressWith(tab, services.LogicalKeyboardKeyShift)
	if got := r.focusedLabel(); got != "Save" {
		t.Errorf("Shift+Tab: focus on %q", got)
	}
	// the widget below cannot take the focus
	r.below.RequestFocus()
	r.settle()
	if r.below.HasPrimaryFocus() || r.focusedLabel() == "" {
		t.Errorf("the focus left the dialog: below %v, inside %q", r.below.HasPrimaryFocus(), r.focusedLabel())
	}
	r.noErrors()
}

func TestDialogIsModal(t *testing.T) {
	r := newDialogRig(t, DarkModern(), windowSize)
	r.ts.Tap(gluttertest.ByText("Below"))
	r.settle()
	if r.pressed != 1 {
		t.Fatalf("the button below does not work without a dialog: %d", r.pressed)
	}
	below := r.ts.Element(gluttertest.ByType[*Button]())
	r.show(saveSpec)
	if !r.ctrl.IsShowing() {
		t.Fatal("not showing")
	}
	// the button below keeps its element (its state) and gets neither clicks nor keys
	if got := r.ts.Elements(gluttertest.ByType[*Button]()); !slices.Contains(got, below) {
		t.Error("the button below was rebuilt from scratch")
	}
	r.ts.TapAt(r.ts.Center(gluttertest.ByText("Below")))
	r.settle()
	r.ts.TapAt(ui.Offset{Dx: 5, Dy: 5})
	r.settle()
	if r.pressed != 1 || len(r.answers) != 0 {
		t.Errorf("pressed %d, answers %v: the dialog let clicks through to the window", r.pressed, r.answers)
	}
	// a click on the scrim keeps the focus in the dialog
	if got := r.focusedLabel(); got != "Save" {
		t.Errorf("focus on %q after a click on the scrim", got)
	}
	// the scrim is 50 % black over the window
	scrim := scrimColor(t, r)
	if scrim != dialogScrim {
		t.Errorf("scrim %v", scrim)
	}
	r.press(services.LogicalKeyboardKeyEscape)
	// after the dialog the window works again
	r.ts.Tap(gluttertest.ByText("Below"))
	r.settle()
	if r.pressed != 2 {
		t.Errorf("pressed %d after the dialog closed", r.pressed)
	}
	if got := r.ts.Element(gluttertest.ByType[*Button]()); got != below {
		t.Error("the button below was rebuilt from scratch after the dialog")
	}
	r.noErrors()
}

// scrimColor is the color of the block over the window while a dialog is showing.
func scrimColor(t *testing.T, r *dialogRig) ui.Color {
	t.Helper()
	cb, ok := r.ts.Widget(gluttertest.First(gluttertest.Descendant(gluttertest.ByType[*DialogHost](), gluttertest.ByType[*widgets.ColoredBox]()))).(*widgets.ColoredBox)
	if !ok {
		t.Fatal("no scrim")
	}
	return cb.Color()
}

func TestDialogRestoresTheFocus(t *testing.T) {
	r := newDialogRig(t, DarkModern(), windowSize)
	r.settle()
	if !r.below.HasPrimaryFocus() {
		t.Fatal("the button below does not have the focus at first")
	}
	r.show(saveSpec)
	if r.below.HasPrimaryFocus() || r.focusedLabel() != "Save" {
		t.Fatalf("below %v, dialog focus %q", r.below.HasPrimaryFocus(), r.focusedLabel())
	}
	r.press(services.LogicalKeyboardKeyEscape)
	if !r.below.HasPrimaryFocus() {
		t.Error("the focus did not go back to the button below")
	}
	r.noErrors()
}

func TestDialogQueue(t *testing.T) {
	r := newDialogRig(t, DarkModern(), windowSize)
	r.show(DialogSpec{Message: "First", Buttons: []string{"A"}})
	r.show(DialogSpec{Message: "Second", Buttons: []string{"B"}})
	r.show(DialogSpec{Message: "Third", Buttons: []string{"C"}})
	if r.ctrl.Pending() != 2 {
		t.Errorf("pending %d", r.ctrl.Pending())
	}
	if n := r.ts.Count(gluttertest.ByType[*Dialog]()); n != 1 || !r.ts.Exists(gluttertest.ByText("First")) || r.ts.Exists(gluttertest.ByText("Second")) {
		t.Fatalf("%d dialogs shown", n)
	}
	if spec, ok := r.ctrl.Current(); !ok || spec.Message != "First" {
		t.Errorf("current %q", spec.Message)
	}
	// the focus the window had is kept while the dialogs follow each other
	r.press(services.LogicalKeyboardKeyEnter)
	if !r.ts.Exists(gluttertest.ByText("Second")) || r.ts.Exists(gluttertest.ByText("First")) {
		t.Fatal("the second dialog does not follow the first")
	}
	if got := r.focusedLabel(); got != "B" {
		t.Errorf("the second dialog has the focus on %q", got)
	}
	if r.below.HasPrimaryFocus() {
		t.Error("the focus went back to the window between two dialogs")
	}
	r.press(services.LogicalKeyboardKeyEscape)
	if !r.ts.Exists(gluttertest.ByText("Third")) || r.ctrl.Pending() != 0 {
		t.Fatalf("the third dialog does not follow: pending %d", r.ctrl.Pending())
	}
	r.press(services.LogicalKeyboardKeyEnter)
	if r.dialogShown() || r.ctrl.IsShowing() || !r.below.HasPrimaryFocus() {
		t.Errorf("shown %v, focus below %v", r.dialogShown(), r.below.HasPrimaryFocus())
	}
	want := []DialogResult{{Button: 0}, {Button: -1, Cancelled: true}, {Button: 0}}
	if !slices.Equal(r.answers, want) {
		t.Errorf("answers %+v, want %+v", r.answers, want)
	}
	r.noErrors()
}

func TestDialogQueueFromAnAnswerAndClear(t *testing.T) {
	r := newDialogRig(t, DarkModern(), windowSize)
	var order []string
	r.ctrl.Show(DialogSpec{Message: "First", OnAnswer: func(DialogResult) {
		order = append(order, "first answered")
		// a dialog shown from an answer comes after the one that was waiting
		r.ctrl.Show(DialogSpec{Message: "Third", OnAnswer: func(DialogResult) { order = append(order, "third answered") }})
	}})
	r.ctrl.Show(DialogSpec{Message: "Second", OnAnswer: func(DialogResult) { order = append(order, "second answered") }})
	r.settle()
	r.press(services.LogicalKeyboardKeyEnter)
	r.press(services.LogicalKeyboardKeyEnter)
	r.press(services.LogicalKeyboardKeyEnter)
	if !slices.Equal(order, []string{"first answered", "second answered", "third answered"}) {
		t.Errorf("order %v", order)
	}
	// Clear drops the dialogs that wait
	r.show(DialogSpec{Message: "A"})
	r.ctrl.Show(DialogSpec{Message: "B", OnAnswer: func(DialogResult) { t.Error("a dialog that was cleared was answered") }})
	r.ctrl.Clear()
	r.press(services.LogicalKeyboardKeyEnter)
	if r.dialogShown() {
		t.Error("a cleared dialog showed")
	}
	// Dismiss with nothing showing does nothing
	r.ctrl.Dismiss()
	r.noErrors()
}

func TestDialogColors(t *testing.T) {
	for _, theme := range []struct {
		name   string
		colors Colors
	}{{"Dark Modern", DarkModern()}, {"Light Modern", LightModern()}} {
		c := theme.colors
		cases := []struct {
			typ   DialogType
			icon  int
			color ui.Color
		}{
			{DialogError, fonts.CodiconError, c.ProblemsErrorIconForeground},
			{DialogWarning, fonts.CodiconWarning, c.ProblemsWarningIconForeground},
			{DialogInfo, fonts.CodiconInfo, c.ProblemsInfoIconForeground},
			{DialogQuestion, fonts.CodiconInfo, c.ProblemsInfoIconForeground},
			{DialogNone, 0, ui.Color{}},
		}
		for _, tc := range cases {
			t.Run(theme.name, func(t *testing.T) {
				r := newDialogRig(t, c, windowSize)
				r.show(DialogSpec{Type: tc.typ, Message: "m", Detail: "d"})
				root := gluttertest.ByType[*Dialog]()
				if got := backgroundOf(t, r.ts, root); got != c.EditorWidgetBackground {
					t.Errorf("background %v, want %v", got, c.EditorWidgetBackground)
				}
				if got := borderOf(t, r.ts, root); got != c.WidgetBorder {
					t.Errorf("border %v, want %v", got, c.WidgetBorder)
				}
				// the icon of the type is the first icon that is not the close button's
				var icon *widgets.Icon
				for _, w := range r.ts.Widgets(gluttertest.Descendant(root, gluttertest.ByType[*widgets.Icon]())) {
					if ic := w.(*widgets.Icon); ic.Size().OrElse(0) == dialogIconSize {
						icon = ic
					}
				}
				switch {
				case tc.icon == 0 && icon != nil:
					t.Errorf("type %v has an icon", tc.typ)
				case tc.icon != 0 && icon == nil:
					t.Errorf("type %v has no icon", tc.typ)
				case tc.icon != 0:
					if icon.Color().OrElse(ui.Color{}) != tc.color {
						t.Errorf("type %v: icon color %v, want %v", tc.typ, icon.Color(), tc.color)
					}
				}
				r.noErrors()
			})
		}
	}
}

func TestDialogOnANarrowWindow(t *testing.T) {
	r := newDialogRig(t, DarkModern(), ui.Size{Width: 400, Height: 480})
	r.show(saveSpec)
	box := gluttertest.First(gluttertest.Descendant(gluttertest.ByType[*Dialog](), gluttertest.ByType[*widgets.Container]()))
	if w := r.ts.Size(box).Width; !near(w, 360) {
		t.Errorf("width %v on a window of 400, want 90 %%", w)
	}
	r.noErrors()
}

func TestDialogTallDetailScrolls(t *testing.T) {
	r := newDialogRig(t, DarkModern(), ui.Size{Width: 700, Height: 300})
	detail := ""
	for range 60 {
		detail += "line\n"
	}
	r.show(DialogSpec{Message: "Long", Detail: detail})
	box := gluttertest.First(gluttertest.Descendant(gluttertest.ByType[*Dialog](), gluttertest.ByType[*widgets.Container]()))
	if h := r.ts.Size(box).Height; h > 300*dialogMaxFraction+0.5 {
		t.Errorf("height %v does not fit the window", h)
	}
	if !r.ts.Exists(dialogButton("OK")) {
		t.Error("the buttons are gone")
	}
	r.noErrors()
}
