package app

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
)

// physicalKeys names a key by its place on a US keyboard, as VS Code's `keyboard.dispatch:
// code` does (SPEC §9.3). Modifier keys are in the table too: the dispatcher needs their
// presses to keep a pending chord.
var physicalKeys = map[services.PhysicalKeyboardKey]keybinding.Key{
	services.PhysicalKeyboardKeyKeyA: keybinding.KeyA, services.PhysicalKeyboardKeyKeyB: keybinding.KeyB,
	services.PhysicalKeyboardKeyKeyC: keybinding.KeyC, services.PhysicalKeyboardKeyKeyD: keybinding.KeyD,
	services.PhysicalKeyboardKeyKeyE: keybinding.KeyE, services.PhysicalKeyboardKeyKeyF: keybinding.KeyF,
	services.PhysicalKeyboardKeyKeyG: keybinding.KeyG, services.PhysicalKeyboardKeyKeyH: keybinding.KeyH,
	services.PhysicalKeyboardKeyKeyI: keybinding.KeyI, services.PhysicalKeyboardKeyKeyJ: keybinding.KeyJ,
	services.PhysicalKeyboardKeyKeyK: keybinding.KeyK, services.PhysicalKeyboardKeyKeyL: keybinding.KeyL,
	services.PhysicalKeyboardKeyKeyM: keybinding.KeyM, services.PhysicalKeyboardKeyKeyN: keybinding.KeyN,
	services.PhysicalKeyboardKeyKeyO: keybinding.KeyO, services.PhysicalKeyboardKeyKeyP: keybinding.KeyP,
	services.PhysicalKeyboardKeyKeyQ: keybinding.KeyQ, services.PhysicalKeyboardKeyKeyR: keybinding.KeyR,
	services.PhysicalKeyboardKeyKeyS: keybinding.KeyS, services.PhysicalKeyboardKeyKeyT: keybinding.KeyT,
	services.PhysicalKeyboardKeyKeyU: keybinding.KeyU, services.PhysicalKeyboardKeyKeyV: keybinding.KeyV,
	services.PhysicalKeyboardKeyKeyW: keybinding.KeyW, services.PhysicalKeyboardKeyKeyX: keybinding.KeyX,
	services.PhysicalKeyboardKeyKeyY: keybinding.KeyY, services.PhysicalKeyboardKeyKeyZ: keybinding.KeyZ,

	services.PhysicalKeyboardKeyDigit0: keybinding.Digit0, services.PhysicalKeyboardKeyDigit1: keybinding.Digit1,
	services.PhysicalKeyboardKeyDigit2: keybinding.Digit2, services.PhysicalKeyboardKeyDigit3: keybinding.Digit3,
	services.PhysicalKeyboardKeyDigit4: keybinding.Digit4, services.PhysicalKeyboardKeyDigit5: keybinding.Digit5,
	services.PhysicalKeyboardKeyDigit6: keybinding.Digit6, services.PhysicalKeyboardKeyDigit7: keybinding.Digit7,
	services.PhysicalKeyboardKeyDigit8: keybinding.Digit8, services.PhysicalKeyboardKeyDigit9: keybinding.Digit9,

	services.PhysicalKeyboardKeyEnter: keybinding.Enter, services.PhysicalKeyboardKeyNumpadEnter: keybinding.Enter,
	services.PhysicalKeyboardKeyEscape: keybinding.Escape, services.PhysicalKeyboardKeyBackspace: keybinding.Backspace,
	services.PhysicalKeyboardKeyTab: keybinding.Tab, services.PhysicalKeyboardKeySpace: keybinding.Space,
	services.PhysicalKeyboardKeyMinus: keybinding.Minus, services.PhysicalKeyboardKeyEqual: keybinding.Equal,
	services.PhysicalKeyboardKeyBracketLeft: keybinding.BracketLeft, services.PhysicalKeyboardKeyBracketRight: keybinding.BracketRight,
	services.PhysicalKeyboardKeyBackslash: keybinding.Backslash, services.PhysicalKeyboardKeySemicolon: keybinding.Semicolon,
	services.PhysicalKeyboardKeyQuote: keybinding.Quote, services.PhysicalKeyboardKeyBackquote: keybinding.Backquote,
	services.PhysicalKeyboardKeyComma: keybinding.Comma, services.PhysicalKeyboardKeyPeriod: keybinding.Period,
	services.PhysicalKeyboardKeySlash: keybinding.Slash, services.PhysicalKeyboardKeyIntlBackslash: keybinding.IntlBackslash,
	services.PhysicalKeyboardKeyCapsLock: keybinding.CapsLock,

	services.PhysicalKeyboardKeyF1: keybinding.F1, services.PhysicalKeyboardKeyF2: keybinding.F2,
	services.PhysicalKeyboardKeyF3: keybinding.F3, services.PhysicalKeyboardKeyF4: keybinding.F4,
	services.PhysicalKeyboardKeyF5: keybinding.F5, services.PhysicalKeyboardKeyF6: keybinding.F6,
	services.PhysicalKeyboardKeyF7: keybinding.F7, services.PhysicalKeyboardKeyF8: keybinding.F8,
	services.PhysicalKeyboardKeyF9: keybinding.F9, services.PhysicalKeyboardKeyF10: keybinding.F10,
	services.PhysicalKeyboardKeyF11: keybinding.F11, services.PhysicalKeyboardKeyF12: keybinding.F12,
	services.PhysicalKeyboardKeyF13: keybinding.F13, services.PhysicalKeyboardKeyF14: keybinding.F14,
	services.PhysicalKeyboardKeyF15: keybinding.F15, services.PhysicalKeyboardKeyF16: keybinding.F16,
	services.PhysicalKeyboardKeyF17: keybinding.F17, services.PhysicalKeyboardKeyF18: keybinding.F18,
	services.PhysicalKeyboardKeyF19: keybinding.F19, services.PhysicalKeyboardKeyF20: keybinding.F20,
	services.PhysicalKeyboardKeyF21: keybinding.F21, services.PhysicalKeyboardKeyF22: keybinding.F22,
	services.PhysicalKeyboardKeyF23: keybinding.F23, services.PhysicalKeyboardKeyF24: keybinding.F24,

	services.PhysicalKeyboardKeyPause: keybinding.PauseBreak, services.PhysicalKeyboardKeyScrollLock: keybinding.ScrollLock,
	services.PhysicalKeyboardKeyInsert: keybinding.Insert, services.PhysicalKeyboardKeyHome: keybinding.Home,
	services.PhysicalKeyboardKeyPageUp: keybinding.PageUp, services.PhysicalKeyboardKeyDelete: keybinding.Delete,
	services.PhysicalKeyboardKeyEnd: keybinding.End, services.PhysicalKeyboardKeyPageDown: keybinding.PageDown,
	services.PhysicalKeyboardKeyArrowRight: keybinding.RightArrow, services.PhysicalKeyboardKeyArrowLeft: keybinding.LeftArrow,
	services.PhysicalKeyboardKeyArrowDown: keybinding.DownArrow, services.PhysicalKeyboardKeyArrowUp: keybinding.UpArrow,
	services.PhysicalKeyboardKeyContextMenu: keybinding.ContextMenu,

	services.PhysicalKeyboardKeyNumLock: keybinding.NumLock, services.PhysicalKeyboardKeyNumpadDivide: keybinding.NumpadDivide,
	services.PhysicalKeyboardKeyNumpadMultiply: keybinding.NumpadMultiply, services.PhysicalKeyboardKeyNumpadSubtract: keybinding.NumpadSubtract,
	services.PhysicalKeyboardKeyNumpadAdd: keybinding.NumpadAdd, services.PhysicalKeyboardKeyNumpadDecimal: keybinding.NumpadDecimal,
	services.PhysicalKeyboardKeyNumpad0: keybinding.Numpad0, services.PhysicalKeyboardKeyNumpad1: keybinding.Numpad1,
	services.PhysicalKeyboardKeyNumpad2: keybinding.Numpad2, services.PhysicalKeyboardKeyNumpad3: keybinding.Numpad3,
	services.PhysicalKeyboardKeyNumpad4: keybinding.Numpad4, services.PhysicalKeyboardKeyNumpad5: keybinding.Numpad5,
	services.PhysicalKeyboardKeyNumpad6: keybinding.Numpad6, services.PhysicalKeyboardKeyNumpad7: keybinding.Numpad7,
	services.PhysicalKeyboardKeyNumpad8: keybinding.Numpad8, services.PhysicalKeyboardKeyNumpad9: keybinding.Numpad9,

	services.PhysicalKeyboardKeyControlLeft: keybinding.Ctrl, services.PhysicalKeyboardKeyControlRight: keybinding.Ctrl,
	services.PhysicalKeyboardKeyShiftLeft: keybinding.Shift, services.PhysicalKeyboardKeyShiftRight: keybinding.Shift,
	services.PhysicalKeyboardKeyAltLeft: keybinding.Alt, services.PhysicalKeyboardKeyAltRight: keybinding.Alt,
	services.PhysicalKeyboardKeyMetaLeft: keybinding.Meta, services.PhysicalKeyboardKeyMetaRight: keybinding.Meta,
}

// latinKey is the key of a logical key that is a Latin letter or a digit: the character the
// layout gives it, so a shortcut works on any layout that has the Latin letters (Dvorak, AZERTY).
func latinKey(logical services.LogicalKeyboardKey) (keybinding.Key, bool) {
	id := logical.KeyID()
	switch {
	case id >= 'a' && id <= 'z':
		return keybinding.KeyA + keybinding.Key(id-'a'), true
	case id >= 'A' && id <= 'Z':
		return keybinding.KeyA + keybinding.Key(id-'A'), true
	case id >= '0' && id <= '9':
		return keybinding.Digit0 + keybinding.Key(id-'0'), true
	}
	return keybinding.Unknown, false
}

// KeyOf is the key of a key event for the dispatcher (SPEC §9.3): letters and digits come from
// the logical key, everything else, and letters of a layout without Latin letters (Greek,
// Cyrillic), from the physical key's name on a US keyboard. It is Unknown for a key without a
// name.
func KeyOf(e services.KeyEvent) keybinding.Key {
	if k, ok := latinKey(e.LogicalKey()); ok {
		return k
	}
	return physicalKeys[e.PhysicalKey()]
}

// ChordOf is the chord a key event presses, with the modifiers the keyboard holds now. ok is
// false for a key up, which dispatches nothing.
func ChordOf(e services.KeyEvent, keyboard *services.HardwareKeyboard) (chord keybinding.Chord, ok bool) {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return keybinding.Chord{}, false
	}
	return keybinding.Chord{
		Ctrl: keyboard.IsControlPressed(), Shift: keyboard.IsShiftPressed(),
		Alt: keyboard.IsAltPressed(), Meta: keyboard.IsMetaPressed(),
		Key: KeyOf(e),
	}, true
}

// KeyDispatcher is the widget at the root of a window's tree that turns key presses into
// commands (SPEC §5.4). It sees every key before the focused widget and before focus
// traversal (an early handler of the focus manager), resolves it against the context keys of
// the widget that has the focus, runs the command of a binding and tells the caller that the
// key was used. A key with no binding is left alone, so that Glutter types its character into
// the focused text input; a chord's first key is held back until the rest comes.
type KeyDispatcher struct {
	widgets.StatefulWidget
	child widgets.Widget
}

// NewKeyDispatcher makes the dispatcher of the window above bc around a child.
func NewKeyDispatcher(child widgets.Widget) *KeyDispatcher {
	return &KeyDispatcher{StatefulWidget: widgets.NewStatefulWidgetBase(), child: child}
}

// CreateState implements widgets.StatefulWidget.
func (w *KeyDispatcher) CreateState() widgets.State { return &keyDispatcherState{} }

type keyDispatcherState struct {
	widgets.StateOf[*KeyDispatcher]
	handle  widgets.FocusHandlerHandle
	manager *widgets.FocusManager
	window  *Window
	binding *widgets.WidgetsBinding
}

// InitState registers the early handler.
func (s *keyDispatcherState) InitState() {
	s.window = PeekWindow(s.Context())
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.manager = s.binding.FocusManager()
	s.handle = s.manager.AddEarlyKeyEventHandler(s.handleKey)
}

// Dispose removes the handler.
func (s *keyDispatcherState) Dispose() { s.manager.RemoveEarlyKeyEventHandler(s.handle) }

// handleKey resolves one key event.
func (s *keyDispatcherState) handleKey(e services.KeyEvent) widgets.KeyEventResult {
	chord, ok := ChordOf(e, s.binding.Keyboard())
	if !ok {
		return widgets.KeyEventResultIgnored
	}
	if s.window.DispatchKey(chord, s.manager.PrimaryFocus()) {
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

// Build implements widgets.State.
func (s *keyDispatcherState) Build(widgets.BuildContext) widgets.Widget {
	return widgets.NewFocus(s.Widget().child, widgets.FocusOpts{
		SkipTraversal: foundation.Some(true), DebugLabel: "KeyDispatcher",
	})
}

// DispatchKey resolves a key press in the context of the widget that has the focus, runs the
// command it is bound to, and reports whether the key was used. focus may be nil. While a dialog is
// showing no key runs a command (the window is modal; the dialog takes its own keys) and none is
// used.
func (w *Window) DispatchKey(chord keybinding.Chord, focus *widgets.FocusNode) bool {
	if w.Dialogs != nil && w.Dialogs.IsShowing() {
		return false
	}
	keys := w.Scopes.ScopeOf(focus, w.Keys)
	out := w.Dispatcher.Dispatch(keys, chord)
	if out.Status != "" || out.ClearStatus {
		w.chordStatus(out.Status, out.ClearStatus)
	}
	if out.Command != "" {
		err := w.Process.Commands.Run(command.Context{Ctx: w.ctx, Keys: keys, Source: command.SourceKeybinding}, out.Command, out.Args)
		if err != nil {
			w.Process.Logger.Warn("command run by a key failed", "command", out.Command, "key", chord.Label(), "error", describe(err))
			if out.Command == "editor.emmet.action.expandAbbreviation" {
				w.Process.Notify.Warn("Emmet", "Failed to expand abbreviation: "+describe(err))
			}
		}
	}
	return out.Handled
}
