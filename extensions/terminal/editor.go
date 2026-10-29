package terminal

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// Terminals in the editor area (SPEC §10.25 "Terminals in the editor area"): a terminal in a tab beside the files, which a
// terminal of the panel becomes and becomes again with the same process and the same cells. A tab is an editor tab of the
// kind EditorKindID; its content is the pane of a terminal, and the terminal is kept by the extension (entry.tab says which tab
// shows it). VS Code's TerminalEditorInput and TerminalEditorService are the model.

const (
	// EditorKindID is the editor kind of a tab that shows a terminal.
	EditorKindID = ID + ".editor"
	// locView and locEditor are the two places a terminal can be in (terminal.integrated.defaultLocation).
	locView   = "view"
	locEditor = "editor"
)

// editorKind is the declaration of the kind in the manifest.
func editorKind() extension.EditorKindSpec {
	return extension.EditorKindSpec{ID: EditorKindID, Name: "Terminal", Icon: "terminal"}
}

// tabBinding joins a terminal to its tab. The callbacks of the tab (closing it, saving its state) go through it, so that a
// terminal that leaves its tab, and a tab that closes because its terminal left, do not end the terminal.
type tabBinding struct {
	e *entry
	// detached says that the terminal is no longer the tab's: it moved to the panel or ended, and the tab is going away.
	detached bool
}

// tabState is the state a tab is made with: the number of a terminal of this window tab that moves into it, or the saved
// session of a terminal that starts anew in it (the tab came back after a restart, or it is the copy of a tab).
type tabState struct {
	Adopt   int        `json:"adopt,omitempty"`
	Session *termState `json:"session,omitempty"`
}

// createTab is EditorKind.Create: it gives a new tab its terminal.
func (t *terminals) createTab(tab extension.EditorTab, raw json.RawMessage) (extension.EditorTabContent, error) {
	var st tabState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return extension.EditorTabContent{}, errors.NewErr("TerminalTabStateInvalid", err)
		}
	}
	var e *entry
	switch {
	case st.Adopt != 0:
		if e = t.entryByID(st.Adopt); e == nil {
			e = t.parked[st.Adopt]
		}
		if e == nil || e.inEditor() {
			return extension.EditorTabContent{}, errors.New("TerminalAdoptUnknown", "there is no terminal of the panel with this number to move into the tab", "id", st.Adopt)
		}
		// a terminal of the panel leaves it for the tab; finishLeaving tells the panel once the tab is shown
		if hadFocus, ok := t.unlink(e); ok {
			t.leaving = &leave{hadFocus: hadFocus}
		}
	case st.Session != nil && t.persisting():
		var err error
		if e, err = t.makeEntry(openOpts{restore: st.Session, quiet: true}); err != nil {
			return extension.EditorTabContent{}, err // the error of the pseudo-terminal, with its own tag and route
		}
		defer t.publishCreated(e)
	default:
		// a tab that came back and has no session to start again (the sessions are not kept): it has nothing to show
		t.api.Post(tab.Close)
		return extension.EditorTabContent{Build: func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) }}, nil
	}
	b := &tabBinding{e: e}
	e.tab, e.bind = tab, b
	t.editors = append(t.editors, e)
	t.syncTab(e)
	return extension.EditorTabContent{
		Build: func(bc widgets.BuildContext) widgets.Widget {
			colors := ui.Theme(bc)
			bg := colors.TerminalBackground
			if bg.Alpha() == 0 {
				bg = colors.EditorBackground
			}
			return widgets.NewColoredBox(bg, paneWidget(e.pv))
		},
		SaveState: func() (any, error) {
			if b.detached || !t.persisting() {
				return nil, nil
			}
			ts := t.termStateOf(e)
			return tabState{Session: &ts}, nil
		},
		OnClose:      func() { t.tabClosed(b) },
		AllowCommand: t.allowCommand,
	}, nil
}

// leave is what the panel remembers of a terminal that left it for an editor tab.
type leave struct{ hadFocus bool }

// finishLeaving tells the panel that a terminal left it for an editor tab that is now shown: the view follows, the last terminal
// gone hides the panel, and the focus goes to another terminal of the panel when the one that left had it.
func (t *terminals) finishLeaving() {
	l := t.leaving
	t.leaving = nil
	if l != nil {
		t.afterLeaving(l.hadFocus)
	}
}

// placeInEditor opens an editor tab for a terminal, which leaves the panel when it is in it. A failure leaves the terminal alive and
// in no place; the caller puts it somewhere (rescue).
func (t *terminals) placeInEditor(e *entry, opts extension.EditorOpenOptions) error {
	if t.parked == nil {
		t.parked = map[int]*entry{}
	}
	id := e.term.ID()
	t.parked[id] = e // a terminal that is in no place yet is found here
	e.adopting = true
	_, err := t.api.Editors().Open(EditorKindID, tabState{Adopt: id}, opts)
	e.adopting = false
	delete(t.parked, id)
	if err != nil {
		t.unlinkEditor(e)
		return err
	}
	t.finishLeaving()
	return nil
}

// unlinkEditor takes a terminal out of the list of the terminals of editor tabs and forgets its tab.
func (t *terminals) unlinkEditor(e *entry) {
	for i, x := range t.editors {
		if x == e {
			t.editors = append(t.editors[:i:i], t.editors[i+1:]...)
			break
		}
	}
	e.tab, e.bind = nil, nil
	e.pv.moving()
	if t.lastEditor == e {
		t.lastEditor = nil
	}
	if t.paneFocus == e {
		t.paneFocus = nil
	}
}

// tabClosed is OnClose of a tab: the user closed it, which ends the terminal (VS Code disposes the instance with its input).
func (t *terminals) tabClosed(b *tabBinding) {
	if b.detached {
		return
	}
	e := b.e
	if e.adopting {
		// the window could not show the tab: the caller of the opening takes the terminal back (rescue)
		t.unlinkEditor(e)
		return
	}
	b.detached = true
	e.ended = true
	e.term.Kill()
	e.term.flushOutput(true)
	t.dropEditor(e, false)
}

// removeEditor ends a terminal of an editor tab that is already ended: its tab closes with it.
func (t *terminals) removeEditor(e *entry) { t.dropEditor(e, true) }

// dropEditor takes a terminal that ended out of its tab, closing the tab when it is still there.
func (t *terminals) dropEditor(e *entry, closeTab bool) {
	tab, b := e.tab, e.bind
	if b != nil {
		b.detached = true
	}
	t.unlinkEditor(e)
	e.gone = true
	e.pv.dispose()
	t.publish(TopicClosed, closedEvent{ID: e.term.ID()})
	if tab != nil && closeTab {
		tab.Close()
	}
	t.syncKeys()
	t.handle.Refresh()
}

// focusEditor gives the keyboard to a terminal of an editor tab, showing the tab first when reveal is set or the tab is behind
// another.
func (t *terminals) focusEditor(e *entry, reveal bool) {
	t.lastEditor = e
	t.wantFocus = e.pv
	if reveal || e.pv.render == nil {
		e.tab.Reveal(true)
	}
	if e.pv.render != nil {
		t.wantFocus = nil
		e.pv.requestFocus()
	}
	t.api.Post(func() {
		if e.pv.render != nil && t.alive(e) {
			e.pv.requestFocus()
		}
	})
	t.syncKeys()
}

// moveToEditor makes an editor tab of a terminal of the panel (workbench.action.terminal.moveToEditor): the same process and the
// same cells, in a tab of the active group.
func (t *terminals) moveToEditor(e *entry) {
	if e == nil || e.inEditor() {
		return
	}
	if err := t.placeInEditor(e, extension.EditorOpenOptions{}); err != nil {
		t.api.Log().Warn("the terminal cannot move to an editor tab", "err", err)
		t.api.Notifications().Warn("The terminal cannot move to the editor area: " + err.Error())
		t.rescue(e)
		return
	}
	t.focusEntry(e, true)
}

// moveToPanel puts a terminal of an editor tab back in the panel (workbench.action.terminal.moveToTerminalPanel), in a tab of
// the list of its own: the tab closes, the terminal stays.
func (t *terminals) moveToPanel(e *entry) {
	if e == nil || !e.inEditor() {
		return
	}
	tab := e.tab
	e.bind.detached = true
	t.unlinkEditor(e)
	t.placeInPanel(e, openOpts{})
	t.suppressOpen = false
	tab.Close()
	t.syncKeys()
	t.handle.Refresh()
	t.focusEntry(e, true)
	t.focusAgain(e)
}

// focusAgainAfter is how long after a tab closed the terminal that left it asks for the keyboard again.
const focusAgainAfter = 200 * time.Millisecond

// focusAgain gives the keyboard to a terminal once more a moment later. The window gives the focus to an editor of the group
// when the tab that had it closes, and it does so over the next frames, after the terminal that moved to the panel asked: the
// last word is the terminal's, unless another terminal got the focus meanwhile.
func (t *terminals) focusAgain(e *entry) {
	time.AfterFunc(focusAgainAfter, func() {
		t.api.Post(func() {
			if t.alive(e) && !e.inEditor() && (t.paneFocus == nil || t.paneFocus == e) {
				t.focusEntry(e, false)
			}
		})
	})
}

// locationFor is where a new terminal goes: the place the caller asked for, else the setting terminal.integrated.defaultLocation;
// a terminal that nobody is waiting at (a quiet one, one of a restored session) goes to the panel.
func (t *terminals) locationFor(o openOpts) string {
	switch {
	case o.location != "":
		return o.location
	case o.quiet || o.restore != nil:
		return locView
	case t.cfg.defaultLocation == locEditor:
		return locEditor
	}
	return locView
}

// ---- what the tab shows ----

// syncTab gives the editor tab of a terminal its title, icon and hover.
func (t *terminals) syncTab(e *entry) {
	if e.tab == nil {
		return
	}
	e.tab.SetTitle(t.title(e))
	e.tab.SetIcon(t.tabIconOf(e), e.term.Meta().color)
	e.tab.SetHint(t.hoverText(e))
}

// tabIconOf is the Codicon of a terminal's tab: the one the user chose, else its profile's, else terminal.integrated.tabs.defaultIcon.
func (t *terminals) tabIconOf(e *entry) string {
	icon := e.term.Meta().icon
	if icon == "" {
		icon = t.cfg.defaultIcon
	}
	if _, ok := ui.CodiconCode(icon); !ok {
		icon = "terminal"
	}
	return icon
}

// hoverText is what the hover of a terminal's tab says (VS Code's getInstanceHoverInfo): its title, the process, the command
// line, the folder and whether the shell tells where its commands are.
func (t *terminals) hoverText(e *entry) string {
	term := e.term
	var b strings.Builder
	b.WriteString(t.title(e))
	if pid := term.Pid(); pid > 0 {
		b.WriteString("\nProcess ID (PID): " + strconv.Itoa(pid))
	}
	prof := term.Profile()
	cmd := prof.path
	if len(cmd) > 32 {
		cmd = "…" + string(filepath.Separator) + filepath.Base(cmd)
	}
	if len(prof.args) > 0 {
		cmd += " " + shellWords(prof.args)
	}
	b.WriteString("\nCommand line: " + cmd)
	b.WriteString("\nTerminal: " + strconv.Itoa(term.ID()))
	if cwd := term.Cwd(); cwd != "" {
		b.WriteString("\nFolder: " + cwd)
	}
	switch {
	case term.ShellIntegration().Active():
		b.WriteString("\nShell integration activated")
	case term.injected:
		b.WriteString("\nShell integration injected, waiting for the shell")
	default:
		b.WriteString("\nShell integration is not active")
	}
	if m := term.Meta(); m.exited {
		b.WriteString("\nThe process exited with code " + strconv.Itoa(m.exitCode))
	}
	return b.String()
}

// shellWords joins arguments for display: one with a space in it is quoted.
func shellWords(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t") {
			a = "'" + a + "'"
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}
