package kit

import (
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// inputRig is an input box in a window with a controller and the events it reported.
type inputRig struct {
	t       *testing.T
	ts      *gluttertest.Tester
	ctrl    *InputBoxController
	changed []string
	submit  []string
	focus   []bool
}

// newInputRig shows an input box in a window of a size; fill edits the options before they are used.
func newInputRig(t *testing.T, colors Colors, size ui.Size, fill func(r *inputRig, o *InputBoxOpts)) *inputRig {
	t.Helper()
	r := &inputRig{t: t, ctrl: NewInputBoxController("")}
	t.Cleanup(r.ctrl.Dispose)
	o := InputBoxOpts{
		Controller:    r.ctrl,
		OnChanged:     func(s string) { r.changed = append(r.changed, s) },
		OnSubmit:      func(s string) { r.submit = append(r.submit, s) },
		OnFocusChange: func(f bool) { r.focus = append(r.focus, f) },
		Width:         foundation.Some(220.0),
	}
	if fill != nil {
		fill(r, &o)
	}
	r.ts = pumpThemed(t, colors, size, padded(NewInputBox(nil, nil, o)))
	return r
}

// press sends a key (down and up) and lets the frame run.
func (r *inputRig) press(k services.LogicalKeyboardKey) {
	r.t.Helper()
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	r.ts.Pump(time.Millisecond)
}

// focusBox gives the box the focus.
func (r *inputRig) focusBox() {
	r.t.Helper()
	r.ctrl.Focus()
	r.ts.Pump(time.Millisecond)
	if !r.ctrl.HasFocus() {
		r.t.Fatal("the box did not take the focus")
	}
}

// borderColor is the color of the border of the box.
func (r *inputRig) borderColor() ui.Color {
	r.t.Helper()
	return borderOf(r.t, r.ts, gluttertest.ByType[*InputBox]())
}

func (r *inputRig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("errors: %v", errs)
	}
}

var inputSize = ui.Size{Width: 280, Height: 140}

func TestInputBoxTextAndCallbacks(t *testing.T) {
	r := newInputRig(t, DarkModern(), inputSize, nil)
	r.ts.EnterText(gluttertest.ByType[*InputBox](), "hello")
	if r.ctrl.Value() != "hello" || !slices.Equal(r.changed, []string{"hello"}) {
		t.Errorf("value %q, changes %v", r.ctrl.Value(), r.changed)
	}
	if !r.ctrl.HasFocus() || !slices.Equal(r.focus, []bool{true}) {
		t.Errorf("focus %v, reports %v", r.ctrl.HasFocus(), r.focus)
	}
	r.ctrl.Blur()
	r.ts.Pump(time.Millisecond)
	if !slices.Equal(r.focus, []bool{true, false}) {
		t.Errorf("focus reports %v", r.focus)
	}
	r.noErrors()
}

func TestInputBoxPlaceholder(t *testing.T) {
	r := newInputRig(t, DarkModern(), inputSize, func(_ *inputRig, o *InputBoxOpts) { o.Placeholder = "Search" })
	if !r.ts.Exists(gluttertest.ByText("Search")) {
		t.Fatal("no placeholder in the empty box")
	}
	r.ts.EnterText(gluttertest.ByType[*InputBox](), "x")
	if r.ts.Exists(gluttertest.ByText("Search")) {
		t.Error("the placeholder stays while there is text")
	}
	r.ctrl.SetValue("")
	r.ts.Pump(time.Millisecond)
	if !r.ts.Exists(gluttertest.ByText("Search")) {
		t.Error("the placeholder does not come back")
	}
	r.noErrors()
}

func TestInputBoxController(t *testing.T) {
	r := newInputRig(t, DarkModern(), inputSize, nil)
	r.ctrl.SetValue("abc def")
	r.ts.Pump(time.Millisecond)
	if r.ctrl.Value() != "abc def" || !slices.Equal(r.changed, []string{"abc def"}) {
		t.Errorf("value %q, changes %v", r.ctrl.Value(), r.changed)
	}
	// the same value is not a change
	r.ctrl.SetValue("abc def")
	if len(r.changed) != 1 {
		t.Errorf("changes %v", r.changed)
	}
	cases := []struct {
		name       string
		act        func()
		start, end int
	}{
		{"the caret is after a set value", func() {}, 7, 7},
		{"SelectAll", r.ctrl.SelectAll, 0, 7},
		{"Select", func() { r.ctrl.Select(2, 4) }, 2, 4},
		{"Select is cut to the text", func() { r.ctrl.Select(-3, 99) }, 0, 7},
	}
	for _, tc := range cases {
		tc.act()
		if start, end, ok := r.ctrl.Selection(); !ok || start != tc.start || end != tc.end {
			t.Errorf("%s: selection %d..%d (%v), want %d..%d", tc.name, start, end, ok, tc.start, tc.end)
		}
	}
	r.ctrl.Focus()
	r.ts.Pump(time.Millisecond)
	if !r.ctrl.HasFocus() {
		t.Error("Focus did not focus")
	}
	r.noErrors()
}

func TestInputBoxSubmit(t *testing.T) {
	cases := []struct {
		name      string
		multiLine bool
		key       services.LogicalKeyboardKey
		ctrl      bool
		want      []string
	}{
		{"Enter", false, services.LogicalKeyboardKeyEnter, false, []string{"go"}},
		{"the numpad's Enter", false, services.LogicalKeyboardKeyNumpadEnter, false, []string{"go"}},
		{"Ctrl+Enter in a single-line box", false, services.LogicalKeyboardKeyEnter, true, nil},
		{"Enter in a multi-line box makes a line", true, services.LogicalKeyboardKeyEnter, false, nil},
		{"Ctrl+Enter in a multi-line box", true, services.LogicalKeyboardKeyEnter, true, []string{"go"}},
		{"another key", false, services.LogicalKeyboardKeyKeyA, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newInputRig(t, DarkModern(), inputSize, func(_ *inputRig, o *InputBoxOpts) { o.MultiLine = tc.multiLine })
			r.ts.EnterText(gluttertest.ByType[*InputBox](), "go")
			if tc.ctrl {
				r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
			}
			r.press(tc.key)
			if tc.ctrl {
				r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
			}
			if !slices.Equal(r.submit, tc.want) {
				t.Errorf("submitted %v, want %v", r.submit, tc.want)
			}
			r.noErrors()
		})
	}
}

func TestInputBoxDisabled(t *testing.T) {
	r := newInputRig(t, DarkModern(), inputSize, func(_ *inputRig, o *InputBoxOpts) { o.Disabled = true })
	r.ctrl.SetValue("fixed")
	r.ctrl.Focus()
	r.ts.Pump(time.Millisecond)
	if r.ctrl.HasFocus() {
		t.Error("a disabled box took the focus")
	}
	r.ts.Tap(gluttertest.ByType[*InputBox]())
	r.ts.Pump(time.Millisecond)
	r.press(services.LogicalKeyboardKeyEnter)
	if len(r.submit) != 0 || r.ctrl.HasFocus() {
		t.Errorf("submitted %v, focus %v", r.submit, r.ctrl.HasFocus())
	}
	r.noErrors()
}

// historyKeys is a step of the history navigation test: a key, or a new text typed.
func TestInputBoxHistoryNavigation(t *testing.T) {
	up, down := services.LogicalKeyboardKeyArrowUp, services.LogicalKeyboardKeyArrowDown
	type step struct {
		key  services.LogicalKeyboardKey
		typ  string // text typed before the key (empty: none)
		want string
	}
	cases := []struct {
		name    string
		history []string
		steps   []step
		after   []string
	}{
		{"Up goes back through the values", []string{"one", "two", "three"},
			[]step{{up, "", "three"}, {up, "", "two"}, {up, "", "one"}, {up, "", "one"}},
			[]string{"one", "two", "three"}},
		{"Down goes forward and past the last value the box is empty", []string{"one", "two", "three"},
			[]step{{up, "", "three"}, {up, "", "two"}, {down, "", "three"}, {down, "", ""}},
			[]string{"one", "two", "three"}},
		{"Down with no history position does nothing but add the typed text", []string{"one"},
			[]step{{down, "new", ""}},
			[]string{"one", "new"}},
		{"Up keeps the text that was typed in the history", []string{"one", "two"},
			[]step{{up, "draft", "two"}, {down, "", "draft"}, {down, "", ""}},
			[]string{"one", "two", "draft"}},
		{"Up on an empty history does nothing", nil,
			[]step{{up, "", ""}, {down, "", ""}},
			nil},
		{"a value that is in the history is not added again", []string{"one", "two"},
			[]step{{up, "one", "two"}},
			[]string{"one", "two"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHistory(tc.history)
			r := newInputRig(t, DarkModern(), inputSize, func(_ *inputRig, o *InputBoxOpts) { o.History = h })
			r.focusBox()
			for i, s := range tc.steps {
				if s.typ != "" {
					r.ts.EnterText(gluttertest.ByType[*InputBox](), s.typ)
				}
				r.press(s.key)
				if r.ctrl.Value() != s.want {
					t.Fatalf("step %d: the box has %q, want %q", i, r.ctrl.Value(), s.want)
				}
			}
			if !slices.Equal(h.Items(), tc.after) {
				t.Errorf("history %v, want %v", h.Items(), tc.after)
			}
			r.noErrors()
		})
	}
}

func TestInputBoxHistorySubmitAdds(t *testing.T) {
	h := NewHistory([]string{"old"})
	r := newInputRig(t, DarkModern(), inputSize, func(_ *inputRig, o *InputBoxOpts) { o.History = h })
	r.ts.EnterText(gluttertest.ByType[*InputBox](), "fresh")
	r.press(services.LogicalKeyboardKeyEnter)
	r.ts.EnterText(gluttertest.ByType[*InputBox](), "")
	r.press(services.LogicalKeyboardKeyEnter) // an empty text is not added
	if !slices.Equal(h.Items(), []string{"old", "fresh"}) || !slices.Equal(r.submit, []string{"fresh", ""}) {
		t.Errorf("history %v, submitted %v", h.Items(), r.submit)
	}
	// without a history the arrow keys go to the text
	r2 := newInputRig(t, DarkModern(), inputSize, nil)
	r2.ts.EnterText(gluttertest.ByType[*InputBox](), "abc")
	r2.press(services.LogicalKeyboardKeyArrowUp)
	if r2.ctrl.Value() != "abc" {
		t.Errorf("value %q", r2.ctrl.Value())
	}
	r.noErrors()
	r2.noErrors()
}

func TestInputBoxHistoryMultiLine(t *testing.T) {
	up, down := services.LogicalKeyboardKeyArrowUp, services.LogicalKeyboardKeyArrowDown
	h := NewHistory([]string{"first", "second"})
	r := newInputRig(t, DarkModern(), inputSize, func(_ *inputRig, o *InputBoxOpts) { o.History = h; o.MultiLine = true })
	r.ts.EnterText(gluttertest.ByType[*InputBox](), "a\nb")
	// the caret is on the last line: Up moves it, not the history
	r.ctrl.Select(3, 3)
	r.press(up)
	if r.ctrl.Value() != "a\nb" {
		t.Fatalf("Up on the second line changed the text to %q", r.ctrl.Value())
	}
	// now on the first line, Up goes into the history
	r.ctrl.Select(0, 0)
	r.press(up)
	if r.ctrl.Value() != "second" {
		t.Errorf("Up on the first line: %q", r.ctrl.Value())
	}
	// Down on a one-line text is on its last line
	r.press(down)
	if r.ctrl.Value() != "a\nb" {
		t.Errorf("Down on the last line: %q", r.ctrl.Value())
	}
	// Alt+Up goes into the history from anywhere
	r.ctrl.Select(3, 3)
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyAlt)
	r.press(up)
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyAlt)
	if r.ctrl.Value() == "a\nb" {
		t.Errorf("Alt+Up did not go into the history")
	}
	r.noErrors()
}

func TestInputBoxValidation(t *testing.T) {
	colors := DarkModern()
	cases := []struct {
		name    string
		typ     string
		message *InputMessage
		border  ui.Color
		shown   bool
	}{
		{"valid", "ok", nil, colors.FocusBorder, false},
		{"info", "i", &InputMessage{Content: "Just so you know", Type: ValidationInfo}, colors.InputValidationInfoBorder, true},
		{"warning", "w", &InputMessage{Content: "Careful", Type: ValidationWarning}, colors.InputValidationWarningBorder, true},
		{"error", "e", &InputMessage{Content: "No way", Type: ValidationError}, colors.InputValidationErrorBorder, true},
		{"a message without text only colors the border", "b", &InputMessage{Type: ValidationError}, colors.InputValidationErrorBorder, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newInputRig(t, colors, inputSize, func(_ *inputRig, o *InputBoxOpts) {
				o.Validate = func(text string) *InputMessage {
					if text == tc.typ {
						return tc.message
					}
					return nil
				}
			})
			r.ts.EnterText(gluttertest.ByType[*InputBox](), tc.typ)
			r.ts.Pump(time.Millisecond)
			if tc.message != nil && tc.message.Content != "" {
				if got := r.ts.Exists(gluttertest.ByText(tc.message.Content)); got != tc.shown {
					t.Errorf("message shown %v, want %v", got, tc.shown)
				}
			}
			if got := r.borderColor(); got != tc.border {
				t.Errorf("border %v, want %v", got, tc.border)
			}
			if tc.shown {
				// below the box
				msg := r.ts.TopLeft(gluttertest.ByText(tc.message.Content))
				box := r.ts.BottomRight(gluttertest.ByType[*InputBox]())
				if msg.Dy < box.Dy-1 {
					t.Errorf("the message is at y=%v, over the bottom of the box at %v", msg.Dy, box.Dy)
				}
				// leaving the box hides the message but the border keeps its color
				r.ctrl.Blur()
				r.ts.Pump(time.Millisecond)
				if r.ts.Exists(gluttertest.ByText(tc.message.Content)) {
					t.Error("the message stays after the box lost the focus")
				}
				if got := r.borderColor(); got != tc.border {
					t.Errorf("border after blur %v, want %v", got, tc.border)
				}
				// and it comes back with the focus
				r.ctrl.Focus()
				r.ts.Pump(time.Millisecond)
				if !r.ts.Exists(gluttertest.ByText(tc.message.Content)) {
					t.Error("the message does not come back with the focus")
				}
			}
			r.noErrors()
		})
	}
}

func TestInputBoxValidationClearsWhenTheTextIsValid(t *testing.T) {
	r := newInputRig(t, DarkModern(), inputSize, func(_ *inputRig, o *InputBoxOpts) {
		o.Validate = func(text string) *InputMessage {
			if text == "" {
				return &InputMessage{Content: "Required", Type: ValidationError}
			}
			return nil
		}
	})
	r.ts.EnterText(gluttertest.ByType[*InputBox](), "x")
	if r.ts.Exists(gluttertest.ByText("Required")) {
		t.Error("a message for valid text")
	}
	r.ts.EnterText(gluttertest.ByType[*InputBox](), "")
	if !r.ts.Exists(gluttertest.ByText("Required")) {
		t.Error("no message for the empty text")
	}
	r.ts.EnterText(gluttertest.ByType[*InputBox](), "y")
	r.ts.Pump(time.Millisecond)
	if r.ts.Exists(gluttertest.ByText("Required")) {
		t.Error("the message stays")
	}
	if got := r.ctrl.Validate(); got != ValidationNone {
		t.Errorf("Validate gives %v for valid text", got)
	}
	r.ctrl.SetValue("")
	if got := r.ctrl.Validate(); got != ValidationError {
		t.Errorf("Validate gives %v for the empty text", got)
	}
	r.noErrors()
}

func TestInputBoxStaticMessageAndBorders(t *testing.T) {
	colors := DarkModern()
	cases := []struct {
		name    string
		opts    InputBoxOpts
		focus   bool
		border  ui.Color
		message string
	}{
		{"plain", InputBoxOpts{}, false, colors.InputBorder, ""},
		{"focused", InputBoxOpts{}, true, colors.FocusBorder, ""},
		{"invalid", InputBoxOpts{Invalid: true}, false, colors.InputValidationErrorBorder, ""},
		{"invalid and focused", InputBoxOpts{Invalid: true}, true, colors.InputValidationErrorBorder, ""},
		{"a warning of the owner", InputBoxOpts{Message: &InputMessage{Content: "Owner says", Type: ValidationWarning}}, true, colors.InputValidationWarningBorder, "Owner says"},
		{"a message of the owner is not shown without the focus", InputBoxOpts{Message: &InputMessage{Content: "Owner says", Type: ValidationInfo}}, false, colors.InputValidationInfoBorder, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newInputRig(t, colors, inputSize, func(_ *inputRig, o *InputBoxOpts) {
				ctrl, w := o.Controller, o.Width
				*o = tc.opts
				o.Controller, o.Width = ctrl, w
			})
			if tc.focus {
				r.focusBox()
			}
			if got := r.borderColor(); got != tc.border {
				t.Errorf("border %v, want %v", got, tc.border)
			}
			if tc.message != "" && !r.ts.Exists(gluttertest.ByText(tc.message)) {
				t.Error("the message of the owner is not shown")
			}
			if tc.message == "" && tc.opts.Message != nil && r.ts.Exists(gluttertest.ByText(tc.opts.Message.Content)) {
				t.Error("the message is shown without the focus")
			}
			r.noErrors()
		})
	}
}

func TestInputBoxMultiLineGrows(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		maxHeight foundation.Opt[float64]
		height    float64
	}{
		{"one line is as high as a single-line box", "one", foundation.None[float64](), inputBoxHeight},
		{"two lines", "one\ntwo", foundation.None[float64](), inputBoxHeight + inputBoxLineHeight},
		{"four lines", "a\nb\nc\nd", foundation.None[float64](), inputBoxHeight + 3*inputBoxLineHeight},
		{"it stops growing at the maximum height", "a\nb\nc\nd\ne\nf", foundation.Some(inputBoxHeight + 2*inputBoxLineHeight), inputBoxHeight + 2*inputBoxLineHeight},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newInputRig(t, DarkModern(), ui.Size{Width: 280, Height: 260}, func(_ *inputRig, o *InputBoxOpts) {
				o.MultiLine, o.MaxHeight = true, tc.maxHeight
			})
			r.ts.EnterText(gluttertest.ByType[*InputBox](), tc.text)
			r.ts.Pump(time.Millisecond)
			got := r.ts.Size(gluttertest.ByType[*InputBox]()).Height
			if got != tc.height {
				t.Errorf("height %v, want %v", got, tc.height)
			}
			r.noErrors()
		})
	}
	// the text of a long line wraps and so makes the box higher
	r := newInputRig(t, DarkModern(), ui.Size{Width: 280, Height: 260}, func(_ *inputRig, o *InputBoxOpts) {
		o.MultiLine, o.Width = true, foundation.Some(100.0)
	})
	r.ts.EnterText(gluttertest.ByType[*InputBox](), "a long line of text that wraps")
	r.ts.Pump(time.Millisecond)
	if got := r.ts.Size(gluttertest.ByType[*InputBox]()).Height; got <= inputBoxHeight {
		t.Errorf("height %v for wrapped text", got)
	}
	r.noErrors()
}

func TestMultiLineMaxLines(t *testing.T) {
	cases := []struct {
		maxHeight float64
		want      int
	}{{25, 1}, {41, 1}, {42, 2}, {59, 3}, {10, 1}}
	for _, tc := range cases {
		if got := multiLineMaxLines(InputBoxOpts{MaxHeight: foundation.Some(tc.maxHeight)}); got != tc.want {
			t.Errorf("max height %v: %d lines, want %d", tc.maxHeight, got, tc.want)
		}
	}
}

func TestInputBoxKeepsAKeyHandlerOfTheNode(t *testing.T) {
	var seen int
	node := widgets.NewFocusNode(widgets.FocusNodeOpts{OnKeyEvent: func(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
		if _, down := e.(*services.KeyDownEvent); down {
			seen++
		}
		return widgets.KeyEventResultIgnored
	}})
	defer node.Dispose()
	text := widgets.NewTextEditingController("x")
	defer text.Dispose()
	ts := pumpThemed(t, DarkModern(), inputSize, padded(NewInputBox(text, node, InputBoxOpts{Width: foundation.Some(200.0)})))
	node.RequestFocus()
	ts.Pump(time.Millisecond)
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyKeyA)
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyKeyA)
	if seen != 1 {
		t.Errorf("the handler of the node saw %d keys, want 1", seen)
	}
}

// A box that makes its own text controller and focus node releases them when it goes away, and one
// that is given them does not.
func TestInputBoxOwnsWhatItMakes(t *testing.T) {
	ts := pumpThemed(t, DarkModern(), inputSize, padded(NewInputBox(nil, nil, InputBoxOpts{Width: foundation.Some(200.0), Autofocus: true, Validate: func(string) *InputMessage {
		return &InputMessage{Content: "m", Type: ValidationInfo}
	}})))
	ts.EnterText(gluttertest.ByType[*InputBox](), "typed")
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByText("m")) {
		t.Error("the message is not shown")
	}
	// remove the box while its message shows
	ts.PumpWidget(widgets.NewSizedBox(nil))
	ts.Pump(time.Millisecond)
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}

	node := widgets.NewFocusNode()
	defer node.Dispose()
	text := widgets.NewTextEditingController("keep")
	defer text.Dispose()
	ts2 := pumpThemed(t, DarkModern(), inputSize, padded(NewInputBox(text, node)))
	ts2.PumpWidget(widgets.NewSizedBox(nil))
	ts2.Pump(time.Millisecond)
	if text.Text() != "keep" {
		t.Errorf("text %q", text.Text())
	}
	if errs := ts2.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// An obscured box is a password box: the controller holds the text and the screen shows dots in its place. A multi-line box is
// never obscured.
func TestInputBoxObscure(t *testing.T) {
	shot := func(obscure, multi bool) *inputRig {
		r := newInputRig(t, DarkModern(), ui.Size{Width: 300, Height: 120}, func(_ *inputRig, o *InputBoxOpts) { o.Obscure, o.MultiLine = obscure, multi })
		r.ts.EnterText(gluttertest.ByType[*InputBox](), "hunter2")
		r.ts.Pump(time.Millisecond)
		if r.ctrl.Value() != "hunter2" {
			t.Fatalf("Value = %q", r.ctrl.Value())
		}
		r.noErrors()
		return r
	}
	same := func(a, b *inputRig) bool {
		x, y := a.ts.Screenshot(), b.ts.Screenshot()
		return string(x.Pix) == string(y.Pix)
	}
	if same(shot(false, false), shot(true, false)) {
		t.Error("an obscured box looks like a plain one")
	}
	if !same(shot(false, true), shot(true, true)) {
		t.Error("a multi-line box was obscured")
	}
}

// typeKeys types the text key by key, as the Linux platform delivers a keyboard: a key down with the
// character it produces and a key up. The framework hands a key that nothing handled to the attached input
// client (services.TextInput.HandleUnhandledKey), so the text reaches the box only when it holds a connection.
// gluttertest's EnterText and the controller's setters skip that path.
func (r *inputRig) typeKeys(text string) {
	r.t.Helper()
	for _, c := range text {
		logical := services.LogicalKeyboardKey(c)
		r.ts.SendKeyDownEvent(logical, gluttertest.KeyOpts{Character: string(c)})
		r.ts.SendKeyUpEvent(logical)
		r.ts.Pump(time.Millisecond)
	}
}

// TestInputBoxTypedKeysReachTheBox is the regression test of the desktop bug of 2026-10-27: a box that starts
// empty with a placeholder took the first key only, because the first key removed the placeholder's Stack
// and mounted the text again, with the focus already on its node and no input connection.
func TestInputBoxTypedKeysReachTheBox(t *testing.T) {
	for _, tc := range []struct {
		name        string
		placeholder string
		multiLine   bool
		start       string
	}{
		{name: "placeholder", placeholder: "Search"},
		{name: "no placeholder"},
		{name: "multi-line placeholder", placeholder: "Take a note", multiLine: true},
		{name: "text already there", placeholder: "Search", start: "x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newInputRig(t, DarkModern(), inputSize, func(r *inputRig, o *InputBoxOpts) {
				o.Placeholder, o.MultiLine = tc.placeholder, tc.multiLine
			})
			if tc.start != "" {
				r.ctrl.SetValue(tc.start)
			}
			r.focusBox()
			r.ctrl.Select(len(tc.start), len(tc.start))
			r.typeKeys("abc")
			if got, want := r.ctrl.Value(), tc.start+"abc"; got != want {
				t.Errorf("after typing abc the box holds %q, want %q", got, want)
			}
			r.noErrors()
		})
	}
}

// remountKey is a key the test changes to mount the box again over the same controller.
type remountKey struct{ n int }

// TestInputBoxMountedAgainWithTheFocusTakesKeys: a box mounted again while its node has the focus takes keys at once. In
// Glutter 1.9.1 it had no input connection until the focus left it and came back, and Emetos worked around it with a click
// handler; Glutter 1.9.2 (step 29.18) opens the connection of a field that is mounted with the focus.
func TestInputBoxMountedAgainWithTheFocusTakesKeys(t *testing.T) {
	for _, tc := range []struct {
		name        string
		placeholder string
		start       string
		want        string
	}{
		{name: "placeholder", placeholder: "Search", start: "ab", want: "abcde"},
		{name: "no placeholder", start: "ab", want: "abcde"},
		{name: "empty with a placeholder", placeholder: "Search", want: "cde"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := NewInputBoxController("")
			t.Cleanup(ctrl.Dispose)
			generation := 0
			var rebuild widgets.StateSetter
			page := widgets.NewStatefulBuilder(func(_ widgets.BuildContext, setState widgets.StateSetter) widgets.Widget {
				rebuild = setState
				b := NewInputBox(nil, nil, InputBoxOpts{Controller: ctrl, Placeholder: tc.placeholder, Width: foundation.Some(220.0)})
				return widgets.NewKeyedSubtree(b, widgets.WidgetOpts{Key: foundation.NewValueKey(remountKey{generation})})
			})
			r := &inputRig{t: t, ctrl: ctrl, ts: pumpThemed(t, DarkModern(), inputSize, padded(page))}
			r.focusBox()
			r.typeKeys(tc.start)
			generation++
			rebuild(func() {})
			r.ts.Pump(time.Millisecond)
			if !ctrl.HasFocus() {
				t.Fatal("the focus did not survive the new mount")
			}
			r.typeKeys("cde")
			if got := ctrl.Value(); got != tc.want {
				t.Errorf("the box mounted again holds %q, want %q", got, tc.want)
			}
			r.noErrors()
		})
	}
}

// TestInputBoxEditShortcuts: Ctrl+A, Ctrl+Z and Ctrl+Y act on the text of the box. The window binds them to the commands of the
// editor; the box takes them first (found by the display tests of step 17b.1: Ctrl+A in a box selected nothing).
func TestInputBoxEditShortcuts(t *testing.T) {
	r := newInputRig(t, DarkModern(), inputSize, func(_ *inputRig, o *InputBoxOpts) { o.Placeholder = "Search" })
	r.focusBox()
	r.typeKeys("abc")
	r.ts.Pump(time.Second) // the undo history groups typing that comes in quick succession
	withCtrl := func(k services.LogicalKeyboardKey, shift bool) {
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
		if shift {
			r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyShift)
		}
		r.ts.SendKeyEvent(k)
		if shift {
			r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyShift)
		}
		r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
		r.ts.Pump(time.Millisecond)
	}
	withCtrl(services.LogicalKeyboardKeyKeyA, false)
	if s, e, _ := r.ctrl.Selection(); s != 0 || e != 3 {
		t.Errorf("after Ctrl+A the selection is %d..%d, want 0..3", s, e)
	}
	r.typeKeys("x")
	if got := r.ctrl.Value(); got != "x" {
		t.Fatalf("typing over the selection gives %q, want x", got)
	}
	r.ts.Pump(time.Second)
	withCtrl(services.LogicalKeyboardKeyKeyZ, false)
	if got := r.ctrl.Value(); got != "abc" {
		t.Errorf("after Ctrl+Z the box holds %q, want abc", got)
	}
	withCtrl(services.LogicalKeyboardKeyKeyY, false)
	if got := r.ctrl.Value(); got != "x" {
		t.Errorf("after Ctrl+Y the box holds %q, want x", got)
	}
	r.noErrors()
}
