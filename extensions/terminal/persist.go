package terminal

import (
	"encoding/json"
	"os"
	"slices"

	"github.com/rm4n0s/emetos/extension/ui"
)

// Persistent sessions (SPEC §10.25 "Persistent sessions"): when Emetos ends, each terminal's name, icon, color, place, folder
// and its last lines are kept in the state of the window (the file of the window state, mode 0600: the lines are whatever the
// programs printed, so they are user data and are kept nowhere else); at the next start each terminal comes back with those
// lines, a line that says so, and a new shell of the same profile in the same folder. VS Code keeps the processes alive only
// across the reload of a window; Emetos has no such reload, so the processes end with it.

// sessionVersion is the version of the saved state; a state of another version is not restored.
const sessionVersion = 1

// maxSavedText is the most the saved lines of one terminal may be, in bytes; a terminal over it is saved without them.
const maxSavedText = 4 << 20

// termState is one terminal of a saved session.
type termState struct {
	// Profile is the name of the profile and Path its program (used when no profile of that name is there now).
	Profile string `json:"profile,omitempty"`
	Path    string `json:"path,omitempty"`
	// Name is the name the user gave the terminal; Icon and Color those the user chose (empty for the profile's).
	Name  string `json:"name,omitempty"`
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
	// Cwd is the folder the shell was in.
	Cwd string `json:"cwd,omitempty"`
	// Cols and Rows are the size of the terminal; Text is the serialization of its last lines (emulator.SerializeText).
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
	Text string `json:"text,omitempty"`
}

// groupState is one tab of the tabs list.
type groupState struct {
	Terminals []termState `json:"terminals"`
	Active    int         `json:"active,omitempty"`
	Sizes     []float64   `json:"sizes,omitempty"`
}

// sessionState is what the panel saves: the tabs list.
type sessionState struct {
	Version int          `json:"version"`
	Groups  []groupState `json:"groups"`
	Current int          `json:"current,omitempty"`
	// Shown says that the panel was showing the terminals when the state was saved: the window does not keep which tab of the
	// panel was open, so the terminals ask for theirs again at the start.
	Shown bool `json:"shown,omitempty"`
}

// restoreMessage is what VS Code writes after the text of a restored session (formatMessageForTerminal with loud formatting).
const restoreMessage = "\r\n\x1b[0m\x1b[7m * \x1b[0;104m History restored \x1b[0m\n\r"

// persisting says whether the sessions are kept: the setting is on and the sessions are not to be left alone.
func (t *terminals) persisting() bool {
	return t.cfg.persistent && t.cfg.reviveProcess != "never"
}

// termStateOf is the saved state of a terminal.
func (t *terminals) termStateOf(e *entry) termState {
	term := e.term
	cols, rows := term.Size()
	prof := term.Profile()
	m := term.Meta()
	st := termState{Profile: prof.name, Path: prof.path, Name: m.name, Color: m.color, Cwd: term.SpeculativeCwd(), Cols: cols, Rows: rows}
	if m.icon != prof.icon {
		st.Icon = m.icon
	}
	if text := term.Emulator().SerializeText(t.cfg.persistLines); len(text) <= maxSavedText {
		st.Text = string(text)
	}
	return st
}

// sessionState is the state of the panel's tabs list, nil when there is nothing to keep (the sessions are not kept, or there is
// no terminal in the panel).
func (t *terminals) sessionState() *sessionState {
	if !t.persisting() || len(t.groups) == 0 {
		return nil
	}
	st := &sessionState{Version: sessionVersion, Current: t.cur}
	for _, e := range t.panelEntries() {
		st.Shown = st.Shown || e.pv.render != nil
	}
	for _, g := range t.groups {
		gs := groupState{Active: g.active, Sizes: slices.Clone(g.sizes)}
		for _, e := range g.entries {
			gs.Terminals = append(gs.Terminals, t.termStateOf(e))
		}
		st.Groups = append(st.Groups, gs)
	}
	return st
}

// restoreParts turns the saved state of a terminal into what newTerminal needs: the profile (the one of the same name now, else
// the program that was saved), the folder (the saved one when it still exists), the size and the text to show first.
func (t *terminals) restoreParts(r *termState, prof profile, dir string, rows, cols int) (profile, string, int, int, []byte) {
	if p := t.profileNamed(r.Profile); p != nil {
		prof = *p
	} else if r.Path != "" {
		if path := firstFound(t.env, r.Path); path != "" {
			prof = profile{name: r.Profile, path: path}
		}
	}
	if r.Cwd != "" {
		if info, err := os.Stat(r.Cwd); err == nil && info.IsDir() {
			dir = r.Cwd
		}
	}
	if r.Cols > 0 && r.Rows > 0 {
		cols, rows = r.Cols, r.Rows
	}
	var initial []byte
	if r.Text != "" {
		initial = append([]byte(r.Text), restoreMessage...)
	}
	return prof, dir, rows, cols, initial
}

// restoreSession makes the terminals of the saved state of the panel. It runs once, at the start of the extension, before the
// first frame of the panel; a terminal that cannot start is left out.
func (t *terminals) restoreSession(raw json.RawMessage) {
	var st sessionState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.api.Log().Warn("the saved terminal sessions cannot be read", "err", err)
		return
	}
	if st.Version != sessionVersion || !t.persisting() {
		return
	}
	cur := -1
	for gi, gs := range st.Groups {
		var g *group
		for ti := range gs.Terminals {
			e, err := t.makeEntry(openOpts{restore: &gs.Terminals[ti], quiet: true})
			if err != nil {
				t.api.Log().Warn("a terminal of the saved session cannot start", "err", err)
				continue
			}
			if g == nil {
				t.nextGr++
				g = &group{id: t.nextGr, split: ui.NewSplitViewController()}
				t.groups = append(t.groups, g)
				if gi == st.Current {
					cur = len(t.groups) - 1
				}
			}
			g.entries = append(g.entries, e)
			size := 1000.0
			if ti < len(gs.Sizes) && gs.Sizes[ti] > 0 {
				size = gs.Sizes[ti]
			}
			g.sizes = append(g.sizes, size)
			t.publishCreated(e)
		}
		if g != nil {
			g.active = min(max(gs.Active, 0), len(g.entries)-1)
		}
	}
	if cur >= 0 {
		t.cur = cur
	} else if len(t.groups) > 0 {
		t.cur = len(t.groups) - 1
	}
	if st.Shown && len(t.groups) > 0 {
		t.api.Post(func() { t.handle.Reveal(false) })
	}
	t.syncKeys()
}

// ---- the state of the extension API ----

// viewState is what the view saves (ViewContent.SaveState): the session, or nothing. It is called when the view goes off screen
// and also while it stays (the workspace saves its state some time after a change of the layout), so it changes nothing: the
// view going off screen is viewLife's.
func (t *terminals) viewState() (any, error) {
	if st := t.sessionState(); st != nil {
		return st, nil
	}
	return nil, nil
}

// viewRestore is ViewContent.RestoreState: it keeps the state for the activation to restore, once. The view asks again whenever
// its box is made again, and the terminals that are there by then are the truth.
func (t *terminals) viewRestore(raw json.RawMessage) error {
	if !t.restoredOnce {
		t.restoredOnce = true
		t.saved = slices.Clone(raw)
		if t.activated {
			// the window read its state after the start activated the extension: the terminals come now, unless the first frame
			// of the panel made one already
			if t.count() == 0 {
				t.restoreSession(t.saved)
			}
			t.saved = nil
		}
	}
	return nil
}
