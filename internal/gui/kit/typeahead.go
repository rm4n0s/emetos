package kit

import (
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// TypeAhead keeps the keys typed in the time between a key that opens an input (Ctrl+F, F2) and the frame that builds the
// input and gives it the focus. In that time the focus is still on the editor (or the tree, or whatever had it), whose own
// handling of a key is the wrong one: a letter went into the document or started the find of the tree, Enter made a new line.
// A TypeAhead is a handler of the hardware keyboard, which sees every key, also while no node has the focus (the editor that had
// it went away with its tab), and an early key handler of the focus manager that claims the keys it took: a key it takes
// reaches no focus node (and so no text input, which the platform feeds only with the keys nobody handled). It puts the typed characters and Backspace into the text of
// the input, and it keeps Enter, the arrows, Home, End and Delete until the input has the focus and then sends them to it as
// key events, in the order they came (once a key is kept, the keys after it are kept too). It ends when the input has the
// focus, or after a few frames when it never gets it. Keys with Control, Alt, Meta or Shift (other than a typed character)
// and the rest (Escape, Tab, F-keys) go on as usual.
//
// The owner of the input hears of what a TypeAhead typed through onChange, as it would of a key typed into the input itself
// (the OnChanged of the input box): the find widget searches, the rename box keeps its name, an input of the Explorer checks
// its name. Without it a key taken here would be in the text of the input but unknown to its owner.
//
// StartTypeAhead is called by the code that opens the input, in the same turn as the request for the focus (SPEC §10.2 "As
// built (step 17b.1)"; the quick input has its own: quickinput.hostState.keysKey, since its text is the picker's).
//
// Glutter: a hardware handler cannot keep the focus tree from seeing a key, and an early handler of the focus manager is not
// called while no node has the focus; both are used for that (docs/GLUTTER-ISSUES.md). Glutter should let one handler do it.
type TypeAhead struct {
	binding *widgets.WidgetsBinding
	text    *widgets.TextEditingController
	node    *widgets.FocusNode
	// resolve finds the text and the node of an input that is not built yet, at each frame, until it does (nil when the input
	// is known from the start).
	resolve func() (*widgets.TextEditingController, *widgets.FocusNode)
	// limit is how many frames the wait lasts.
	limit int
	// onChange is told the new text after a key was taken; it may be nil.
	onChange func(text string)
	// hw takes the keys; handle claims them from the focus tree, and taken is the key it took last.
	hw     services.KeyHandlerHandle
	handle widgets.FocusHandlerHandle
	taken  services.KeyEvent
	// typed tells whether any character was taken, and caret is where the caret is then.
	typed bool
	caret int
	// kept are the keys held for the input, in order; they are sent when the input has the focus.
	kept []services.KeyEvent
	// landed is true from the moment the input has the focus until the caret is put back.
	landed bool
	// frames counts the frames since the start, the limit of the wait.
	frames int
	done   bool
	// listener is the focus listener of the node.
	listener foundation.ListenerHandle
}

// typeAheadFrames is how many frames a TypeAhead waits for the input to get the focus, and typeAheadLaterFrames the same for an
// input that has to be built first (StartTypeAheadLater).
const (
	typeAheadFrames      = 4
	typeAheadLaterFrames = 10
)

// StartTypeAhead gives the input the focus (the request waits for the node to be in the tree, which the next build does) and
// starts taking the typed keys into its text, at the caret or over the selection the text has now (the caller selects what the
// user is about to replace, such as the old search text of the find widget, before it starts).
func StartTypeAhead(binding *widgets.WidgetsBinding, text *widgets.TextEditingController, node *widgets.FocusNode, onChange func(text string)) *TypeAhead {
	t := &TypeAhead{binding: binding, text: text, node: node, onChange: onChange, limit: typeAheadFrames}
	t.handle, t.hw = binding.FocusManager().AddEarlyKeyEventHandler(t.claim), binding.Keyboard().AddHandler(t.key)
	t.listener = node.AddListener(t.focusChanged)
	t.waitFrame()
	node.RequestFocus()
	return t
}

// StartTypeAheadLater is StartTypeAhead for an input that does not exist yet: the search box of an editor that was just opened
// is made when the editor is built. resolve is asked after each frame until it gives the text and the node of the input. Until
// then no key can be typed into anything, so every key is kept, and they all go to the input once it has the focus.
func StartTypeAheadLater(binding *widgets.WidgetsBinding, resolve func() (*widgets.TextEditingController, *widgets.FocusNode), onChange func(text string)) *TypeAhead {
	t := &TypeAhead{binding: binding, resolve: resolve, onChange: onChange, limit: typeAheadLaterFrames}
	t.handle, t.hw = binding.FocusManager().AddEarlyKeyEventHandler(t.claim), binding.Keyboard().AddHandler(t.key)
	t.attach()
	t.waitFrame()
	return t
}

// attach asks for the input; when it is there the TypeAhead follows its focus and asks for it. An editor can be built twice
// before it settles, so it asks again at every frame and follows the input it is given last.
func (t *TypeAhead) attach() {
	if t.resolve == nil {
		return
	}
	text, node := t.resolve()
	if text == nil || node == nil || node == t.node {
		return
	}
	if t.node != nil {
		t.node.RemoveListener(t.listener)
	}
	if t.typed && t.text != nil {
		// what was typed into the input that is gone goes into the new one
		value := t.text.Text()
		text.SetValue(services.NewTextEditingValue(services.TextEditingValueOpts{
			Text: value, Selection: foundation.Some(services.NewTextSelectionCollapsed(min(t.caret, len(value)))),
		}))
		if t.onChange != nil {
			t.onChange(value)
		}
	}
	t.text, t.node, t.landed = text, node, false
	t.listener = node.AddListener(t.focusChanged)
	node.RequestFocus()
	t.focusChanged()
}

// keptKey tells whether a key is one that TypeAhead keeps for the input and sends to it: Enter, the arrows, Home, End and
// Delete, which the editor or the tree would act on.
func keptKey(key services.LogicalKeyboardKey) bool {
	switch key {
	case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter,
		services.LogicalKeyboardKeyArrowLeft, services.LogicalKeyboardKeyArrowRight,
		services.LogicalKeyboardKeyArrowUp, services.LogicalKeyboardKeyArrowDown,
		services.LogicalKeyboardKeyHome, services.LogicalKeyboardKeyEnd, services.LogicalKeyboardKeyDelete:
		return true
	}
	return false
}

// claim keeps the focus tree from seeing the key that key took.
func (t *TypeAhead) claim(e services.KeyEvent) widgets.KeyEventResult {
	if t.taken != nil && e == t.taken {
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

// key takes a typed character, Backspace, or a key to keep for the input; it reports whether it took the key.
func (t *TypeAhead) key(e services.KeyEvent) bool {
	t.taken = nil
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
	switch {
	case e.LogicalKey() == services.LogicalKeyboardKeyBackspace, printable(e.Character()):
	case keptKey(e.LogicalKey()) && !kb.IsShiftPressed():
	default:
		return false
	}
	t.taken = e
	if keptKey(e.LogicalKey()) || len(t.kept) > 0 || t.text == nil {
		// keys after a kept one wait behind it, so that the input sees them in the order they were typed
		t.kept = append(t.kept, e)
		return true
	}
	t.take(e)
	return true
}

// take puts a typed character or Backspace into the text, at the caret or over the selection.
func (t *TypeAhead) take(e services.KeyEvent) {
	value := t.text.Text()
	start, end := len(value), len(value)
	if t.landed && t.typed {
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
			return
		default:
			_, size := utf8.DecodeLastRuneInString(value[:start])
			start -= size
		}
		value = value[:start] + value[end:]
	default:
		value = value[:start] + e.Character() + value[end:]
		start += len(e.Character())
	}
	t.typed, t.caret = true, start
	t.text.SetValue(services.NewTextEditingValue(services.TextEditingValueOpts{
		Text: value, Selection: foundation.Some(services.NewTextSelectionCollapsed(start)),
	}))
	if t.onChange != nil {
		t.onChange(value)
	}
}

// flush sends the kept keys: characters and Backspace into the text, the others as key events (a press and a release) to the
// focus, which is the input when it got the focus. It runs after the handler is removed, so the events are not taken again.
func (t *TypeAhead) flush() {
	kept := t.kept
	t.kept = nil
	for _, e := range kept {
		switch {
		case keptKey(e.LogicalKey()):
			t.send(e)
		case t.text != nil:
			t.take(e)
		}
	}
}

// send gives a key to the focus as the platform would.
func (t *TypeAhead) send(e services.KeyEvent) {
	data := ui.KeyData{
		TimeStamp: e.TimeStamp(), Type: ui.KeyEventTypeDown, Physical: uint64(e.PhysicalKey()), Logical: uint64(e.LogicalKey()),
		Character: e.Character(), Synthesized: true,
	}
	kb := t.binding.Keyboard()
	kb.HandleKeyData(data)
	data.Type, data.Character = ui.KeyEventTypeUp, ""
	kb.HandleKeyData(data)
}

// focusChanged ends the wait when the input has the focus. The focus selects all the text of the input, so the caret is put
// back where the typing left it in a moment, and the keys that come before that are still taken, at that caret.
func (t *TypeAhead) focusChanged() {
	if t.done || t.landed || t.node == nil || !t.node.HasPrimaryFocus() {
		return
	}
	if !t.typed && len(t.kept) == 0 {
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
		t.attach()
		if t.done {
			return
		}
		if t.frames >= t.limit {
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
	t.binding.FocusManager().RemoveEarlyKeyEventHandler(t.handle)
	t.binding.Keyboard().RemoveHandler(t.hw)
	t.attach()
	if t.node != nil {
		t.node.RemoveListener(t.listener)
	}
	t.flush()
	if t.typed && t.node != nil && t.node.HasPrimaryFocus() && t.caret <= len(t.text.Text()) {
		t.text.SetSelection(services.NewTextSelectionCollapsed(t.caret))
	}
}

// Stop ends the wait at once (the input was closed); the keys it holds are dropped.
func (t *TypeAhead) Stop() {
	t.kept = nil
	t.finish()
}
