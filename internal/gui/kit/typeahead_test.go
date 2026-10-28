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

// typeAheadRig is a text field that stands for the editor and has the focus, and an input box that a call to show puts in
// the window.
type typeAheadRig struct {
	t    *testing.T
	ts   *gluttertest.Tester
	ctrl *InputBoxController
	// editor is the stand-in: what is typed into it is what reached the editor.
	editor *InputBoxController
	show   func()
}

func newTypeAheadRig(t *testing.T) *typeAheadRig {
	t.Helper()
	r := &typeAheadRig{t: t, ctrl: NewInputBoxController("old"), editor: NewInputBoxController("")}
	t.Cleanup(r.ctrl.Dispose)
	t.Cleanup(r.editor.Dispose)
	shown := false
	page := widgets.NewStatefulBuilder(func(_ widgets.BuildContext, setState widgets.StateSetter) widgets.Widget {
		r.show = func() { setState(func() { shown = true }) }
		kids := []widgets.Widget{NewInputBox(nil, nil, InputBoxOpts{Controller: r.editor, Width: foundation.Some(220.0), Autofocus: true})}
		if shown {
			kids = append(kids, NewInputBox(nil, nil, InputBoxOpts{Controller: r.ctrl, Placeholder: "Find", Width: foundation.Some(220.0)}))
		}
		return widgets.NewColumn(kids, widgets.ColumnOpts{})
	})
	r.ts = pumpThemed(t, DarkModern(), ui.Size{Width: 300, Height: 120}, page)
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
	if !r.editor.HasFocus() {
		t.Fatal("the stand-in has no focus")
	}
	return r
}

func (r *typeAheadRig) typeKeys(text string) {
	r.t.Helper()
	for _, c := range text {
		l := services.LogicalKeyboardKey(c)
		r.ts.SendKeyDownEvent(l, gluttertest.KeyOpts{Character: string(c)})
		r.ts.SendKeyUpEvent(l)
	}
}

// TestTypeAheadTakesTheKeysBeforeTheInputIsBuilt: the keys typed between the command that opens an input and the frame that
// builds it go to the input, not to the editor; the first one replaces the selected text, and typing goes on in the box.
func TestTypeAheadTakesTheKeysBeforeTheInputIsBuilt(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selectAll bool
		want      string
	}{
		{"replacing the selected text", true, "hello"},
		{"adding to the text", false, "oldhello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newTypeAheadRig(t)
			binding := widgets.WidgetsBindingOf(r.ts.Element(gluttertest.ByType[*widgets.Column]()))
			// the command: the input is to show, and takes the focus when it is built
			r.show()
			if tc.selectAll {
				r.ctrl.Select(0, 3)
			} else {
				r.ctrl.Select(3, 3)
			}
			ta := StartTypeAhead(binding, r.ctrl.TextController(), r.ctrl.FocusNode(), nil)
			t.Cleanup(ta.Stop)
			r.ts.FlushPosted()
			// keys before the frame
			r.typeKeys("hel")
			r.ts.Pump(time.Millisecond)
			r.ts.Pump(time.Millisecond)
			r.typeKeys("lo")
			r.ts.Pump(time.Millisecond)
			if got := r.ctrl.Value(); got != tc.want {
				t.Errorf("the box holds %q, want %q", got, tc.want)
			}
			if !r.ctrl.HasFocus() {
				t.Error("the box has no focus")
			}
			if got := r.editor.Value(); got != "" {
				t.Errorf("%q reached the editor", got)
			}
			r.typeKeys("!")
			r.ts.Pump(time.Millisecond)
			if got := r.ctrl.Value(); got != tc.want+"!" {
				t.Errorf("after the frames the box holds %q, want %q", got, tc.want+"!")
			}
		})
	}
}

func TestTypeAheadBackspaceAndGivingUp(t *testing.T) {
	r := newTypeAheadRig(t)
	binding := widgets.WidgetsBindingOf(r.ts.Element(gluttertest.ByType[*widgets.Column]()))
	r.ctrl.SetValue("")
	r.show()
	StartTypeAhead(binding, r.ctrl.TextController(), r.ctrl.FocusNode(), nil)
	r.ts.FlushPosted()
	r.typeKeys("abc")
	r.ts.SendKeyEvent(services.LogicalKeyboardKeyBackspace)
	r.ts.Pump(time.Millisecond)
	if got := r.ctrl.Value(); got != "ab" {
		t.Errorf("after abc and Backspace the box holds %q", got)
	}

	// an input that never gets the focus: the wait ends after a few frames and the keys go to the editor again
	r2 := newTypeAheadRig(t)
	binding = widgets.WidgetsBindingOf(r2.ts.Element(gluttertest.ByType[*widgets.Column]()))
	StartTypeAhead(binding, r2.ctrl.TextController(), r2.ctrl.FocusNode(), nil)
	for range typeAheadFrames + 1 {
		r2.ts.Pump(time.Millisecond)
	}
	r2.typeKeys("x")
	if got := r2.editor.Value(); got != "x" {
		t.Errorf("after the wait the editor holds %q, want x", got)
	}
}

// typeAheadRig2 is a window with a stand-in for the editor that counts the keys the focus tree gives it, and an input box that
// a call to show puts in, which reports what it submitted.
type typeAheadRig2 struct {
	t         *testing.T
	ts        *gluttertest.Tester
	ctrl      *InputBoxController
	editorKey []string
	submitted []string
	shown     bool
	show      func()
}

func newTypeAheadRig2(t *testing.T) *typeAheadRig2 {
	t.Helper()
	r := &typeAheadRig2{t: t, ctrl: NewInputBoxController("")}
	t.Cleanup(r.ctrl.Dispose)
	editorNode := widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Editor"})
	t.Cleanup(editorNode.Dispose)
	editorNode.SetOnKeyEvent(func(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
		if _, ok := e.(*services.KeyDownEvent); ok {
			r.editorKey = append(r.editorKey, string(rune(e.LogicalKey())))
		}
		return widgets.KeyEventResultIgnored
	})
	page := widgets.NewStatefulBuilder(func(_ widgets.BuildContext, setState widgets.StateSetter) widgets.Widget {
		r.show = func() { setState(func() { r.shown = true }) }
		kids := []widgets.Widget{widgets.NewFocus(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(10.0), Height: foundation.Some(10.0)}),
			widgets.FocusOpts{FocusNode: editorNode, Autofocus: true})}
		if r.shown {
			kids = append(kids, NewInputBox(nil, nil, InputBoxOpts{
				Controller: r.ctrl, Width: foundation.Some(220.0), OnSubmit: func(text string) { r.submitted = append(r.submitted, text) },
			}))
		}
		return widgets.NewColumn(kids, widgets.ColumnOpts{})
	})
	r.ts = pumpThemed(t, DarkModern(), ui.Size{Width: 300, Height: 120}, page)
	r.ts.Pump(time.Millisecond)
	r.ts.Pump(time.Millisecond)
	if !editorNode.HasPrimaryFocus() {
		t.Fatal("the stand-in has no focus")
	}
	return r
}

func (r *typeAheadRig2) press(logical services.LogicalKeyboardKey, character string) {
	r.ts.SendKeyDownEvent(logical, gluttertest.KeyOpts{Character: character})
	r.ts.SendKeyUpEvent(logical)
}

// TestTypeAheadKeepsEnterAndTheKeysAfterItForTheInput: Enter typed before the input is built reaches the input when it has the
// focus, after the characters before it and before the ones after it, and none of it reaches the focus tree that has the focus
// meanwhile (the editor: a letter typed there starts the find of a tree, Enter makes a new line).
func TestTypeAheadKeepsEnterAndTheKeysAfterItForTheInput(t *testing.T) {
	r := newTypeAheadRig2(t)
	binding := widgets.WidgetsBindingOf(r.ts.Element(gluttertest.ByType[*widgets.Column]()))
	var changes []string
	r.show()
	ta := StartTypeAhead(binding, r.ctrl.TextController(), r.ctrl.FocusNode(), func(text string) { changes = append(changes, text) })
	t.Cleanup(ta.Stop)
	r.ts.FlushPosted()
	r.press(services.LogicalKeyboardKey('h'), "h")
	r.press(services.LogicalKeyboardKey('i'), "i")
	r.press(services.LogicalKeyboardKeyEnter, "")
	r.press(services.LogicalKeyboardKey('!'), "!")
	for range 3 {
		r.ts.Pump(time.Millisecond)
	}
	if got := r.ctrl.Value(); got != "hi!" {
		t.Errorf("the box holds %q, want hi!", got)
	}
	if !slices.Equal(r.submitted, []string{"hi"}) {
		t.Errorf("the box submitted %q, want [hi] (Enter after hi and before !)", r.submitted)
	}
	if !slices.Equal(changes, []string{"h", "hi", "hi!"}) {
		t.Errorf("the owner was told %q", changes)
	}
	if len(r.editorKey) != 0 {
		t.Errorf("the focus tree saw the keys %q", r.editorKey)
	}
}

// TestTypeAheadLaterKeepsEveryKeyUntilTheInputExists: the input of an editor that is just opening is not there to type into, so
// the keys wait, and go into it, in order, once it exists and has the focus.
func TestTypeAheadLaterKeepsEveryKeyUntilTheInputExists(t *testing.T) {
	r := newTypeAheadRig2(t)
	binding := widgets.WidgetsBindingOf(r.ts.Element(gluttertest.ByType[*widgets.Column]()))
	var changes []string
	ready := false
	ta := StartTypeAheadLater(binding, func() (*widgets.TextEditingController, *widgets.FocusNode) {
		if !ready {
			return nil, nil
		}
		return r.ctrl.TextController(), r.ctrl.FocusNode()
	}, func(text string) { changes = append(changes, text) })
	t.Cleanup(ta.Stop)
	r.press(services.LogicalKeyboardKey('a'), "a")
	r.press(services.LogicalKeyboardKey('b'), "b")
	r.ts.Pump(time.Millisecond)
	r.press(services.LogicalKeyboardKey('c'), "c")
	if got := r.ctrl.Value(); got != "" {
		t.Errorf("the box holds %q before it exists", got)
	}
	ready = true
	r.show()
	for range 4 {
		r.ts.Pump(time.Millisecond)
	}
	if got := r.ctrl.Value(); got != "abc" {
		t.Errorf("the box holds %q, want abc", got)
	}
	if !r.ctrl.HasFocus() {
		t.Error("the box has no focus")
	}
	if len(changes) == 0 || changes[len(changes)-1] != "abc" {
		t.Errorf("the owner was told %q", changes)
	}
	if len(r.editorKey) != 0 {
		t.Errorf("the focus tree saw the keys %q", r.editorKey)
	}
	// typing goes on in the box
	r.press(services.LogicalKeyboardKey('d'), "d")
	r.ts.Pump(time.Millisecond)
	if got := r.ctrl.Value(); got != "abcd" {
		t.Errorf("after the wait the box holds %q, want abcd", got)
	}
}

// keys with Control and the others that the type-ahead does not know go on as usual
func TestTypeAheadLeavesShortcutsAlone(t *testing.T) {
	r := newTypeAheadRig2(t)
	binding := widgets.WidgetsBindingOf(r.ts.Element(gluttertest.ByType[*widgets.Column]()))
	r.show()
	ta := StartTypeAhead(binding, r.ctrl.TextController(), r.ctrl.FocusNode(), nil)
	t.Cleanup(ta.Stop)
	r.ts.FlushPosted()
	r.press(services.LogicalKeyboardKeyEscape, "")
	r.press(services.LogicalKeyboardKeyTab, "")
	if len(r.editorKey) != 2 {
		t.Errorf("the focus tree saw %q, want Escape and Tab", r.editorKey)
	}
	if got := r.ctrl.Value(); got != "" {
		t.Errorf("the box holds %q", got)
	}
}
