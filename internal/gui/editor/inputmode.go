package editor

import "github.com/rm4n0s/glutter/foundation"

// InputMode is the type mode of the editors: insert, where a typed character is put before the one after the cursor, or
// overtype, where it replaces that one. The mode is shared by every editor that was given the same InputMode, as VS
// Code's InputMode singleton is shared by every editor of its window: the app makes one for the process, so the Insert
// key changes all the editors of all the workspace tabs, and the status bar shows it once.
//
// Ported from VS Code: src/vs/editor/common/inputMode.ts
type InputMode struct {
	notifier *foundation.ChangeNotifier
	overtype bool
}

// NewInputMode makes an input mode in insert mode.
func NewInputMode() *InputMode { return &InputMode{notifier: foundation.NewChangeNotifier()} }

// Overtype reports whether the mode is overtype.
func (m *InputMode) Overtype() bool { return m.overtype }

// SetOvertype sets the mode and tells the listeners when it changed.
func (m *InputMode) SetOvertype(overtype bool) {
	if overtype == m.overtype {
		return
	}
	m.overtype = overtype
	m.notifier.NotifyListeners()
}

// Toggle changes insert mode to overtype mode and back (the command editor.action.toggleOvertypeInsertMode).
func (m *InputMode) Toggle() { m.SetOvertype(!m.overtype) }

// AddListener calls fn when the mode changes.
func (m *InputMode) AddListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return m.notifier.AddListener(fn)
}

// RemoveListener removes a listener.
func (m *InputMode) RemoveListener(h foundation.ListenerHandle) { m.notifier.RemoveListener(h) }

// InputMode is the input mode of the editor: the one it shares with the other editors when the widget was given one.
func (vm *ViewModel) InputMode() *InputMode { return vm.inputMode }

// SetInputMode makes the editor follow an input mode, usually the one of the process; the editor is redrawn when the mode
// it shows changes. With nil the editor goes back to a mode of its own, in silence: the widget does this when it is
// disposed (or no longer shows the editor), so that a shared mode does not keep the editor alive and nothing is told while
// the widgets are being taken down.
func (vm *ViewModel) SetInputMode(m *InputMode) {
	if m == vm.inputMode {
		return
	}
	silent := m == nil
	if silent {
		m = NewInputMode()
	}
	vm.inputMode.RemoveListener(vm.inputModeHandle)
	vm.inputMode = m
	vm.inputModeHandle = m.AddListener(vm.inputModeChanged)
	if !silent {
		vm.inputModeChanged()
	}
}

// inputModeChanged redraws the cursors, which have the style of the mode, and starts their blinking again.
func (vm *ViewModel) inputModeChanged() {
	vm.restartBlink()
	vm.NotifyListeners()
	vm.sel.NotifyListeners()
}

// EffectiveCursorStyle is the style the cursors are drawn in: editor.overtypeCursorStyle in overtype mode (block when the
// option is empty), else editor.cursorStyle.
//
// Ported from VS Code: src/vs/editor/common/config/editorOptions.ts (EffectiveCursorStyle)
func (vm *ViewModel) EffectiveCursorStyle() string {
	if vm.inputMode.Overtype() {
		if vm.opts.OvertypeCursorStyle == "" {
			return "block"
		}
		return vm.opts.OvertypeCursorStyle
	}
	return vm.opts.CursorStyle
}
