package kit

import (
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
			ta := StartTypeAhead(binding, r.ctrl.TextController(), r.ctrl.FocusNode())
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
	StartTypeAhead(binding, r.ctrl.TextController(), r.ctrl.FocusNode())
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
	StartTypeAhead(binding, r2.ctrl.TextController(), r2.ctrl.FocusNode())
	for range typeAheadFrames + 1 {
		r2.ts.Pump(time.Millisecond)
	}
	r2.typeKeys("x")
	if got := r2.editor.Value(); got != "x" {
		t.Errorf("after the wait the editor holds %q, want x", got)
	}
}
