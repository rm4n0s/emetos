package terminal

import (
	"strconv"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The screens of a hosted workspace (SPEC §10.25 "Clients of a shared workspace", step 24.25, D51). The Terminal panel is built once for each screen
// that shows it: the window of the host and the view of each client of Remote Work. The terminals, their processes and their emulators are
// the workspace's, one each; a screen has its own pane over a terminal (the scroll, the selection, the find box, the focus), and what a
// command does it does on the pane of the screen it was run on (Origins().Current). The size of a terminal is the size of the pane of the
// screen that typed last, as the size of a shared tmux session is the size of the client that was active last; a pane of another screen
// draws the terminal as it is. The terminal that git asks its questions in, typed by a client, asks them in that client's view: the extension
// remembers who typed last in each terminal (Origins().Remember).

// screenState is what the extension keeps of the view of one client.
type screenState struct {
	// focus is the terminal whose pane has the keyboard on the screen; tabsFocus is whether the tabs list has it.
	focus     *entry
	tabsFocus bool
	// keys are the context keys last set for the screen.
	keys map[string]any
	// menu is the context menu controller of the screen's widgets.
	menu *ui.ContextMenuController
}

// screen is the state of a client's screen; it is made when it is first needed.
func (t *terminals) screen(o extension.Origin) *screenState {
	if t.screens == nil {
		t.screens = map[extension.Origin]*screenState{}
	}
	sc := t.screens[o]
	if sc == nil {
		sc = &screenState{}
		t.screens[o] = sc
	}
	return sc
}

// dropScreen forgets a client's screen when its view goes off screen: its panes are disposed and its keys reset.
func (t *terminals) dropScreen(o extension.Origin) {
	for _, e := range t.all() {
		if pv := e.views[o]; pv != nil {
			pv.dispose()
			delete(e.views, o)
		}
	}
	if sc := t.screens[o]; sc != nil {
		delete(t.screens, o)
		for k := range sc.keys {
			if !globalKeys[k] {
				t.api.Context().ResetFor(o, k)
			}
		}
	}
}

// menuFor is the controller of the context menu of the widgets on a screen.
func (t *terminals) menuFor(o extension.Origin) *ui.ContextMenuController {
	if o == extension.HostOrigin {
		return t.menu
	}
	sc := t.screen(o)
	if sc.menu == nil {
		sc.menu = ui.NewContextMenuController()
	}
	return sc.menu
}

// pvFor is the pane of a terminal on a screen; a client's pane is made when its view first draws the terminal.
func (t *terminals) pvFor(e *entry, o extension.Origin) *paneView {
	if o == extension.HostOrigin {
		return e.pv
	}
	if pv := e.views[o]; pv != nil {
		return pv
	}
	if e.views == nil {
		e.views = map[extension.Origin]*paneView{}
	}
	t.screen(o) // the screen exists while it has a pane
	pv := newPaneView(t.newEnv(), e.term, o)
	e.views[o] = pv
	return pv
}

// pvIf is the pane of a terminal on a screen when it has one.
func (t *terminals) pvIf(e *entry, o extension.Origin) *paneView {
	if o == extension.HostOrigin {
		return e.pv
	}
	return e.views[o]
}

// pvOf is the pane a command acts on: the one of the screen the command was run on.
func (t *terminals) pvOf(e *entry) *paneView { return t.pvFor(e, t.api.Origins().Current()) }

// disposePanes lets go of the panes of every screen when the terminal is gone.
func (e *entry) disposePanes() {
	e.pv.dispose()
	for o, pv := range e.views {
		pv.dispose()
		delete(e.views, o)
	}
}

// ownsPane says that a pane shows the terminal.
func (e *entry) ownsPane(pv *paneView) bool {
	if e.pv == pv {
		return true
	}
	for _, v := range e.views {
		if v == pv {
			return true
		}
	}
	return false
}

// entryOf is the terminal a pane shows, nil when it is gone.
func (t *terminals) entryOf(pv *paneView) *entry {
	for _, e := range t.all() {
		if e.ownsPane(pv) {
			return e
		}
	}
	return nil
}

// remoteFocusChanged follows the keyboard into and out of the pane of a client's view.
func (t *terminals) remoteFocusChanged(pv *paneView, has bool) {
	e := t.entryOf(pv)
	if e == nil {
		return
	}
	sc := t.screen(pv.origin)
	switch {
	case has:
		sc.focus = e
		if g, gi, ei, ok := t.find(e); ok {
			t.cur, g.active = gi, ei
		}
	case sc.focus == e:
		sc.focus = nil
	}
	t.syncKeysOf(pv.origin)
	if has {
		t.handle.Refresh()
	}
}

// anyPaneFocus says that the pane of a terminal has the keyboard on some screen.
func (t *terminals) anyPaneFocus() bool {
	if t.paneFocus != nil {
		return true
	}
	for _, sc := range t.screens {
		if sc.focus != nil {
			return true
		}
	}
	return false
}

// targetFor is the terminal a command run on a screen acts on: the one with the keyboard there, else the active one.
func (t *terminals) targetFor(o extension.Origin) *entry {
	if o == extension.HostOrigin {
		return t.hostTarget()
	}
	if sc := t.screens[o]; sc != nil && sc.focus != nil && t.alive(sc.focus) {
		return sc.focus
	}
	return t.activeEntry()
}

// typed is told that a person typed in a pane: the terminal remembers the screen, so that the questions of a program that runs in it, such as
// the password that git asks for, are shown there.
func (t *terminals) typed(pv *paneView) {
	if e := t.entryOf(pv); e != nil {
		t.api.Origins().Remember(typistKey(e.term.ID()), pv.origin)
	}
}

// typistKey is the key the screen of the last typist of a terminal is remembered under (Origins().Recall); the shell of the terminal has
// the number in EMETOS_TERMINAL.
func typistKey(id int) string { return "terminal:" + strconv.Itoa(id) }
