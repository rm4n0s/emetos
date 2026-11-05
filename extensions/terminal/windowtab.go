package terminal

import (
	"encoding/json"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// A terminal as a window tab (SPEC §10.25 "A terminal as a window tab", step 28.1): the "+" menu's "Terminal" entry opens one
// terminal with no workspace of its own, beside the workspace tabs. It is activated in the scope of its own window tab
// (extension.ScopeWindowTab, SPEC §10.15 "As built (step 11.7)"), a new instance of this extension that holds exactly one
// terminal and its pane: the same *entry and *Terminal of a panel's or an editor tab's terminal, kept in one *group of its
// own so that the panel's existing machinery (target, focusEntry, kill, the context keys) needs no change for it, but with
// noEvents set, so that it takes no part in the emetos.terminal.* events and requests, which are of a terminal "of the
// workspace". There is no split and no tabs list: opening the kind again from the "+" menu makes another, independent
// window tab.

// winTabState is the state WindowTabContent.SaveState keeps and Create revives: the terminal's session, the same shape
// persist.go gives the panel and the editor tab's terminal (SPEC §10.25 "Persistent sessions").
type winTabState struct {
	Session *termState `json:"session,omitempty"`
}

// activateWindowTab provides the window tab kind: Create is called once, at once, for the tab's own activation.
func (t *terminals) activateWindowTab() error {
	for id, h := range t.windowTabHandlers() {
		if err := t.api.Commands().Handle(id, h); err != nil {
			return err
		}
	}
	t.noEvents = true
	return t.api.WindowTabs().Provide(WindowTabKindID, extension.WindowTabKind{Create: t.createWindowTab})
}

// windowTabExcluded are the commands of the panel and the editor tab that do not apply to a window tab's one terminal: it
// has no split, no second terminal to join, no editor tab or panel to move to or from, no tab icon or color of its own to
// change (SPEC §10.25 "A terminal as a window tab": "one terminal per window tab", "no split and no tabs list").
var windowTabExcluded = []string{
	CmdToggle, CmdNew, CmdNewWithProfile, CmdSplit, CmdCreateEditor, CmdMoveToEditor, CmdMoveToPanel, CmdKillEditor,
	CmdChangeIcon, CmdChangeColor, CmdJoin, CmdUnsplit, CmdKillAll, CmdNewWithCwd,
}

// windowTabHandlers is the handlers of the commands that apply to a window tab's one terminal: every command of the panel
// and the editor tab except windowTabExcluded.
func (t *terminals) windowTabHandlers() map[string]extension.CommandHandler {
	h := t.handlers()
	for _, id := range windowTabExcluded {
		delete(h, id)
	}
	return h
}

// windowTabHandle adapts a WindowTab to a ViewHandle, so the panel's own machinery (terminals.go: target, focusEntry, kill,
// the context keys) works unchanged for the one terminal of a window tab, which has no panel of its own: Hide, which the
// panel calls when its last terminal is gone, closes the tab instead (there is nothing left to show in it); Reveal needs do
// nothing (the tab is already the one on screen when its own commands run).
type windowTabHandle struct{ tab extension.WindowTab }

var _ extension.ViewHandle = windowTabHandle{}

func (h windowTabHandle) Refresh()                           { h.tab.Refresh() }
func (h windowTabHandle) SetBadge(count int, tooltip string) {}
func (h windowTabHandle) SetTitle(title string)              {}
func (h windowTabHandle) Reveal(focus bool)                  {}
func (h windowTabHandle) Hide()                              { h.tab.Close() }
func (h windowTabHandle) Dispose()                           {}

// createWindowTab is WindowTabKind.Create: it makes the one terminal of the tab, from the saved session when there is one and
// the setting allows it, with no workspace folder (its working folder is terminal.integrated.cwd when it is an absolute
// path, else the home folder: profile.go's startDir already falls back to the home folder when there is no workspace
// folder, as for a terminal of a No Folder workspace tab, and pane_links.go's isFileUnder refuses every file link the same
// way, since there is no folder to confine them to).
func (t *terminals) createWindowTab(tab extension.WindowTab, raw json.RawMessage) (extension.WindowTabContent, error) {
	t.handle = windowTabHandle{tab: tab}
	var st winTabState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return extension.WindowTabContent{}, errors.NewErr("TerminalWindowTabStateInvalid", err)
		}
	}
	o := openOpts{quiet: true}
	if st.Session != nil && t.persisting() {
		o.restore = st.Session
	}
	e, err := t.makeEntry(o)
	if err != nil {
		return extension.WindowTabContent{}, err
	}
	t.nextGr++
	t.groups = []*group{{id: t.nextGr, entries: []*entry{e}, sizes: []float64{1000}, split: ui.NewSplitViewController()}}
	t.cur = 0
	t.wantFocus = e.pv
	t.syncKeys()
	tab.SetTitle("Terminal")
	tab.SetHint("Terminal")
	return extension.WindowTabContent{
		Build: func(bc widgets.BuildContext) widgets.Widget {
			colors := ui.Theme(bc)
			bg := colors.TerminalBackground
			if bg.Alpha() == 0 {
				bg = colors.EditorBackground
			}
			return widgets.NewColoredBox(bg, paneWidget(e.pv))
		},
		SaveState: func() (any, error) {
			if e.gone || !t.persisting() {
				return nil, nil
			}
			ts := t.termStateOf(e)
			return winTabState{Session: &ts}, nil
		},
		OnClose: func() { t.windowTabClosed(e) },
	}, nil
}

// windowTabClosed ends the terminal when the window tab closes for good (WindowTabContent.OnClose): the user closed the tab
// while the terminal was still running (Kill and the process exiting already take it out, through the panel's own
// kill/remove, before the tab closes itself through windowTabHandle.Hide, so this is then a no-op). Closing asks nothing,
// as an editor tab's terminal already does (no veto, D226): the process is killed at once (SIGHUP, then SIGKILL after the
// grace time, as every terminal).
func (t *terminals) windowTabClosed(e *entry) {
	if e.gone {
		return
	}
	e.ended = true
	e.term.Kill()
	e.term.flushOutput(true)
	t.unlink(e)
	e.gone = true
	e.disposePanes()
}
