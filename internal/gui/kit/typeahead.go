package kit

import (
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"
)

// TypeAhead takes the keys typed in the time between a key that opens an input (Ctrl+F, F2) and the frame that builds the
// input and gives it the focus. In that time the focus is still on the editor (or whatever had it), so the characters of a
// fast typist went into the document. A TypeAhead sees the keys before the focus tree does (a handler of the hardware
// keyboard), puts the typed characters and Backspace into the text of the input, and ends when the input has the focus, or
// after a few frames when it never gets it. The keys it does not take (Enter, Escape, the arrows, anything with Control,
// Alt or Meta) go on as usual.
//
// StartTypeAhead is called by the code that opens the input, in the same turn as the request for the focus (SPEC §10.2 "As
// built (step 17b.1)"; the quick input has its own: quickinput.hostState.keysKey, since its text is the picker's).
type TypeAhead struct {
	binding *widgets.WidgetsBinding
	text    *widgets.TextEditingController
	node    *widgets.FocusNode
	handle  services.KeyHandlerHandle
	// typed tells whether any key was taken, and caret is where the caret is then.
	typed bool
	caret int
	// landed is true from the moment the input has the focus until the caret is put back.
	landed bool
	// frames counts the frames since the start, the limit of the wait.
	frames int
	done   bool
	// listener is the focus listener of the node.
	listener foundation.ListenerHandle
}

// typeAheadFrames is how many frames a TypeAhead waits for the input to get the focus.
const typeAheadFrames = 4

// StartTypeAhead gives the input the focus (the request waits for the node to be in the tree, which the next build does) and
// starts taking the typed keys into its text, at the caret or over the selection the text has now (the caller selects what the
// user is about to replace, such as the old search text of the find widget, before it starts).
func StartTypeAhead(binding *widgets.WidgetsBinding, text *widgets.TextEditingController, node *widgets.FocusNode) *TypeAhead {
	t := &TypeAhead{binding: binding, text: text, node: node}
	t.handle = binding.Keyboard().AddHandler(t.key)
	t.listener = node.AddListener(t.focusChanged)
	t.waitFrame()
	node.RequestFocus()
	return t
}

// key takes a typed character or Backspace into the text, at the caret or over the selection.
func (t *TypeAhead) key(e services.KeyEvent) bool {
	if t.done {
		return false
	}
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return false
	}
	kb := t.binding.Keyboard()
	if kb.IsControlPressed() || kb.IsAltPressed() || kb.IsMetaPressed() {
		return false
	}
	value := t.text.Text()
	start, end := len(value), len(value)
	if t.landed {
		// the input has the focus, which selected its text; the caret is put back in a moment, and until then it is here
		start, end = min(t.caret, len(value)), min(t.caret, len(value))
	} else if sel := t.text.Selection(); sel.IsValid() && sel.End() <= len(value) {
		start, end = sel.Start(), sel.End()
	}
	switch {
	case e.LogicalKey() == services.LogicalKeyboardKeyBackspace:
		switch {
		case start != end:
		case start == 0:
			return true
		default:
			_, size := utf8.DecodeLastRuneInString(value[:start])
			start -= size
		}
		value = value[:start] + value[end:]
	case printable(e.Character()):
		value = value[:start] + e.Character() + value[end:]
		start += len(e.Character())
	default:
		return false
	}
	t.typed, t.caret = true, start
	t.text.SetValue(services.NewTextEditingValue(services.TextEditingValueOpts{
		Text: value, Selection: foundation.Some(services.NewTextSelectionCollapsed(start)),
	}))
	return true
}

// focusChanged ends the wait when the input has the focus. The focus selects all the text of the input, so the caret is put
// back where the typing left it in a moment, and the keys that come before that are still taken, at that caret.
func (t *TypeAhead) focusChanged() {
	if t.done || t.landed || !t.node.HasPrimaryFocus() {
		return
	}
	if !t.typed {
		t.finish()
		return
	}
	t.landed = true
	t.binding.Post(t.finish)
}

// waitFrame counts a frame, and gives up after a few.
func (t *TypeAhead) waitFrame() {
	t.binding.Scheduler().ScheduleFrame()
	t.binding.RunPostFrame(func(time.Duration) {
		if t.done {
			return
		}
		t.frames++
		if t.frames >= typeAheadFrames {
			t.finish()
			return
		}
		t.waitFrame()
	})
}

// finish stops taking keys; the caret goes where the typing left it, which the focus has overridden.
func (t *TypeAhead) finish() {
	if t.done {
		return
	}
	t.done = true
	t.binding.Keyboard().RemoveHandler(t.handle)
	t.node.RemoveListener(t.listener)
	if t.typed && t.node.HasPrimaryFocus() && t.caret <= len(t.text.Text()) {
		t.text.SetSelection(services.NewTextSelectionCollapsed(t.caret))
	}
}

// Stop ends the wait at once (the input was closed).
func (t *TypeAhead) Stop() { t.finish() }
