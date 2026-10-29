package terminal

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// termVersion is the version of the terminal that programs read in TERM_PROGRAM_VERSION.
const termVersion = "0.1.0"

// entry is one terminal of a window tab and the pane that shows it. The terminal does not know the pane (it can move to an
// editor tab later, and Relay reads it through events).
type entry struct {
	term *Terminal
	pv   *paneView
	// ended says that the user or the extension ended the terminal, so that its exit is not reported.
	ended bool
	// clients are the comments of the keys of the Relay clients that typed in the terminal and are still connected.
	clients []string
	// fixes are the quick fixes of the last command that ended (shell integration).
	fixes *fixSet
	// tab is the editor tab that shows the terminal, nil while it is in the panel (SPEC §10.25 "Terminals in the editor
	// area"); bind is the link between the two while they are joined.
	tab  extension.EditorTab
	bind *tabBinding
	// adopting says that a tab is being made for the terminal, which stays alive if that fails (placeInEditor).
	adopting bool
	// gone says that the terminal ended and was taken out of its place for good: nothing puts it back.
	gone bool
}

// inEditor says that the terminal is shown in an editor tab.
func (e *entry) inEditor() bool { return e.tab != nil }

// group is a tab of the tabs list: terminals side by side.
type group struct {
	id      int
	entries []*entry
	// sizes are the sizes of the panes in the split view, kept across the rebuilds of the view.
	sizes  []float64
	active int
	split  *ui.SplitViewController
}

// terminals is the extension of one window tab: the terminals in groups, the view of the panel, the context keys. All of it
// belongs to the UI thread.
type terminals struct {
	api    extension.API
	ctx    context.Context
	folder string
	env    environ
	// start starts the programs; nil is a real pseudo-terminal. The tests replace it.
	start starter
	// outputInterval is how long the output of a terminal is gathered before it is published; 0 is outputInterval. The tests
	// shorten it.
	outputInterval time.Duration

	cfg   *config
	fonts fontCache

	handle extension.ViewHandle
	groups []*group
	// editors are the terminals shown in editor tabs, in the order they were made (editor.go).
	editors []*entry
	// parked holds a terminal that is in no place while the tab that adopts it is made.
	parked map[int]*entry
	cur    int
	nextID int
	nextGr int
	// created says that a terminal was made in this window tab (terminalHasBeenCreated).
	created bool
	// visible says that the view is on screen.
	visible bool
	// activated says that Activate is done: a saved session that comes after it is restored when it comes.
	activated bool

	// paneFocus is the entry whose pane has the keyboard focus; tabsFocus that the tabs list has it.
	paneFocus *entry
	tabsFocus bool
	// lastEditor is the terminal of an editor tab that had the keyboard last: the commands without a focus act on it when the
	// editor tab is the one in front.
	lastEditor *entry
	// wantFocus is the pane that gets the keyboard focus as soon as it is on screen.
	wantFocus *paneView

	keys    map[string]any
	menu    *ui.ContextMenuController
	ask     *pasteAsk
	failure string
	// editor is the text editor in front, which the commands that run its text and file follow.
	editor editorState
	// hist is the history of commands and folders of the terminals of all the window tabs (terminal-history.json).
	hist *historyStore
	// opening says that a terminal is being made for a view that is shown with none; suppressOpen that the last terminal just
	// ended and the panel is on its way to hide, so the view must not make another.
	opening      bool
	suppressOpen bool
	// tabDrag is the press on a tab of the list that may be a drag, dragOver the terminal whose row a drag in the list is over
	// and dragEnd that it is below the last row (tabsdrag.go).
	tabDrag  *tabDrag
	dragOver *entry
	dragEnd  bool
	// leaving is the state of the panel when a terminal left it for an editor tab (createTab), until finishLeaving.
	leaving *leave
	// saved is the state the view saved in the session before (ViewContent.RestoreState); restoredOnce says it was taken, so that
	// the box being made again does not restore twice.
	saved        json.RawMessage
	restoredOnce bool
	// metaPosted says that a refresh for a change of a tab is waiting for the UI thread.
	metaPosted atomic.Bool
	// sugg computes the suggestions of terminal IntelliSense.
	sugg *suggester
	// histLimit is terminal.integrated.shellIntegration.history for the goroutines that write the history, which must not read
	// t.cfg (the UI thread replaces it when a setting changes).
	histLimit atomic.Int64
}

// pasteAsk is the question about pasting several lines (enableMultiLinePasteWarning), shown over the terminal.
type pasteAsk struct {
	lines int
	paste func()
	pane  *paneView
}

// newEnv is what the panes of this window tab need from the extension.
func (t *terminals) newEnv() *paneEnv {
	return &paneEnv{
		cfg: func() *config { return t.cfg }, fonts: &t.fonts, post: t.api.Post,
		focusChanged: t.paneFocusChanged, selectionChanged: func(*paneView) { t.syncKeys() },
		menu: t.showMenu, confirmPaste: t.confirmPaste, clipboardCtx: clipboardContext(t.ctx),
		mounted: t.paneMounted, altChanged: func(*paneView) { t.syncKeys() },
		ctx: t.ctx, findChanged: func(*paneView) { t.syncKeys() }, folder: func() string { return t.folder }, home: t.env.home,
		altClick: t.altClick, openFile: t.openFile, openLink: t.openLink,
		fixes: t.fixesOf, runFix: t.runFix, commandMenu: t.showCommandMenu, stickyMenu: t.showStickyMenu, info: t.infoOf,
		suggester: t.sugg, suggestEnv: t.suggestEnvOf, suggestChanged: func(*paneView) { t.syncKeys() },
	}
}

// ---- the terminals ----

// panelEntries lists the entries of every group of the panel.
func (t *terminals) panelEntries() []*entry {
	var out []*entry
	for _, g := range t.groups {
		out = append(out, g.entries...)
	}
	return out
}

// all lists every terminal: the ones of the panel, then the ones in editor tabs.
func (t *terminals) all() []*entry {
	return append(t.panelEntries(), t.editors...)
}

// panelCount is the number of terminals in the panel.
func (t *terminals) panelCount() int {
	n := 0
	for _, g := range t.groups {
		n += len(g.entries)
	}
	return n
}

// count is the number of terminals, in the panel and in editor tabs.
func (t *terminals) count() int { return t.panelCount() + len(t.editors) }

// alive says that a terminal is in the panel or in an editor tab.
func (t *terminals) alive(e *entry) bool {
	if _, _, _, ok := t.find(e); ok {
		return true
	}
	return slices.Contains(t.editors, e)
}

// activeGroup is the group of the tab that is selected, nil when there is none.
func (t *terminals) activeGroup() *group {
	if t.cur < 0 || t.cur >= len(t.groups) {
		return nil
	}
	return t.groups[t.cur]
}

// activeEntry is the terminal that has the focus in the active group, nil when there is none.
func (t *terminals) activeEntry() *entry {
	g := t.activeGroup()
	if g == nil || len(g.entries) == 0 {
		return nil
	}
	return g.entries[min(g.active, len(g.entries)-1)]
}

// find returns the group and the index of an entry; ok is false for one that is gone.
func (t *terminals) find(e *entry) (g *group, gi, ei int, ok bool) {
	for gi, g := range t.groups {
		for ei, x := range g.entries {
			if x == e {
				return g, gi, ei, true
			}
		}
	}
	return nil, 0, 0, false
}

// profiles are the profiles of the settings whose program exists.
func (t *terminals) profiles() []profile { return profilesOf(t.api.Settings(), t.env) }

// defaultProfile is the profile a new terminal gets.
func (t *terminals) defaultProfile() profile {
	return defaultProfileOf(t.api.Settings(), t.env, t.profiles())
}

// openOpts say how to make a terminal.
type openOpts struct {
	// profile is the shell; nil is the default profile.
	profile *profile
	// dir is the working folder; empty is the folder of the settings.
	dir string
	// split puts the terminal beside the active one, in the same tab.
	split bool
	// name is the name the user gave it.
	name string
	// quiet makes the terminal without showing the panel, giving it the keyboard or making it the active tab: a terminal a
	// client of Relay asked for does not take the screen of the person at the computer.
	quiet bool
	// by is who asked for the terminal (ByUser, a client of Relay or an extension); empty is the user.
	by string
	// location is where the terminal goes: "view" the panel, "editor" an editor tab, empty the setting
	// terminal.integrated.defaultLocation (the panel for a quiet terminal).
	location string
	// restore makes the terminal of a session that was saved (persist.go): its profile, folder, name, icon, color and the text
	// it showed.
	restore *termState
}

// open makes a terminal, shows it and gives it the focus. A failure is shown to the user and returned.
func (t *terminals) open(o openOpts) (*entry, error) {
	e, err := t.makeEntry(o)
	if err != nil {
		return nil, err
	}
	loc := t.locationFor(o)
	if loc == locEditor {
		if err := t.placeInEditor(e, extension.EditorOpenOptions{}); err != nil {
			t.api.Log().Warn("the terminal cannot be shown in an editor tab, it goes to the panel", "err", err)
			loc = locView
		}
	}
	if loc == locView {
		t.placeInPanel(e, o)
	}
	t.typedBy(e, o.by)
	t.publishCreated(e)
	t.syncKeys()
	t.handle.Refresh()
	if !o.quiet {
		t.focusEntry(e, true)
	}
	return e, nil
}

// makeEntry makes a terminal and the pane that shows it, and does not place them anywhere.
func (t *terminals) makeEntry(o openOpts) (*entry, error) {
	prof := t.defaultProfile()
	if o.profile != nil {
		prof = *o.profile
	}
	dir := o.dir
	home := t.env.home
	if dir == "" {
		if o.split {
			if cur := t.target(); cur != nil {
				dir = splitDir(t.cfg.splitCwd, cur.term)
			}
		}
		if dir == "" {
			cwdSetting, _ := extension.SettingValue[string](t.api.Settings(), settingCwd)
			dir = startDir(cwdSetting, t.folder, home)
		}
	}
	settingEnvVars, _ := extension.SettingValue[map[string]*string](t.api.Settings(), settingEnv)
	locale, _ := extension.SettingValue[string](t.api.Settings(), settingDetectLocale)
	rows, cols := 24, 80
	if cur := t.target(); cur != nil {
		cols, rows = cur.term.Size()
	}
	var initial []byte
	if r := o.restore; r != nil {
		prof, dir, rows, cols, initial = t.restoreParts(r, prof, dir, rows, cols)
	}
	t.nextID++
	env := buildEnv(t.env.vars, settingEnvVars, prof.env, locale, termVersion)
	prof, injected, nonce, ok := t.shellIntegration(prof)
	if ok {
		env = setEnv(env, injected)
	}
	term, err := newTerminal(t.ctx, terminalOpts{
		id: t.nextID, profile: prof, dir: dir, env: env,
		rows: rows, cols: cols, scrollback: t.cfg.scrollback, version: termVersion, start: t.start, log: t.api.Log(),
		obs: t.observerFor(t.nextID), interval: t.outputInterval, nonce: nonce, injected: ok, initial: initial,
	})
	if err != nil {
		t.failure = "The terminal process " + prof.path + " failed to launch: " + err.Error()
		t.api.Notifications().Error(t.failure)
		if t.handle != nil {
			t.handle.Refresh()
		}
		return nil, err
	}
	t.failure = ""
	t.created = true
	if o.name != "" {
		term.SetName(o.name)
	}
	if r := o.restore; r != nil {
		if r.Name != "" {
			term.SetName(r.Name)
		}
		if r.Icon != "" {
			term.SetIcon(r.Icon)
		}
		term.SetColor(r.Color)
	}
	e := &entry{term: term}
	e.pv = newPaneView(t.newEnv(), term)
	t.hookShell(e)
	term.SetOnMeta(t.metaChanged)
	go t.watch(e)
	return e, nil
}

// placeInPanel puts a terminal in the tabs list of the panel: in a tab of its own, or beside the active terminal of the tab
// for a split.
func (t *terminals) placeInPanel(e *entry, o openOpts) {
	if g := t.activeGroup(); o.split && g != nil {
		g.entries = append(g.entries, e)
		g.sizes = append(g.sizes, g.meanSize())
		g.active = len(g.entries) - 1
		return
	}
	first := len(t.groups) == 0
	t.nextGr++
	t.groups = append(t.groups, &group{id: t.nextGr, entries: []*entry{e}, sizes: []float64{1000}, split: ui.NewSplitViewController()})
	if !o.quiet || first {
		t.cur = len(t.groups) - 1
	}
}

// publishCreated tells the world that a terminal was made.
func (t *terminals) publishCreated(e *entry) {
	ev := createdEvent{ID: e.term.ID(), Name: t.nameOf(e), Profile: e.term.Profile().name, Cwd: e.term.Cwd()}
	if e.inEditor() {
		ev.Location = locEditor
	}
	t.publish(TopicCreated, ev)
}

// meanSize is the size a new pane starts with in a split.
func (g *group) meanSize() float64 {
	if len(g.sizes) == 0 {
		return 1000
	}
	total := 0.0
	for _, s := range g.sizes {
		total += s
	}
	return total / float64(len(g.sizes))
}

// watch ends the terminal with the window tab and tells the UI thread when it is gone. It runs on its own goroutine.
func (t *terminals) watch(e *entry) {
	select {
	case <-t.ctx.Done():
		e.term.Kill()
		e.term.Wait()
	case <-e.term.Exited():
		t.api.Post(func() { t.exited(e) })
	}
}

// metaChanged is told from any goroutine that a tab has a new title, folder, process or bell; the tabs are built again once.
func (t *terminals) metaChanged() {
	if t.metaPosted.CompareAndSwap(false, true) {
		t.api.Post(func() {
			t.metaPosted.Store(false)
			for _, e := range t.editors {
				t.syncTab(e)
			}
			t.handle.Refresh()
		})
	}
}

// exited is the program of a terminal ending by itself: the terminal goes (VS Code closes it); a failure status is reported.
func (t *terminals) exited(e *entry) {
	if !t.alive(e) {
		return
	}
	code, _ := e.term.ExitCode()
	t.publish(TopicExited, exitedEvent{ID: e.term.ID(), Code: code})
	if !e.ended && code != 0 && code != 130 && code != 129 && code != 143 && code != -1 {
		t.api.Notifications().Warn("The terminal process \"" + e.term.Profile().path + "\" terminated with exit code: " + strconv.Itoa(code) + ".")
	}
	t.remove(e)
}

// kill ends a terminal and takes it away.
func (t *terminals) kill(e *entry) {
	if !t.alive(e) {
		return
	}
	e.ended = true
	e.term.Kill()
	e.term.flushOutput(true)
	t.remove(e)
}

// remove takes a terminal out of its group and the group out of the list when it is empty; the last terminal gone hides the
// panel.
func (t *terminals) remove(e *entry) {
	if e.inEditor() {
		t.removeEditor(e)
		return
	}
	hadFocus, ok := t.unlink(e)
	if !ok {
		return
	}
	e.gone = true
	e.pv.dispose()
	t.publish(TopicClosed, closedEvent{ID: e.term.ID()})
	t.afterLeaving(hadFocus)
}

// unlink takes a terminal out of its group, and the group out of the list when it is empty, and tells whether the terminal had
// the keyboard focus. It ends nothing: the callers end the terminal or move it.
func (t *terminals) unlink(e *entry) (hadFocus, ok bool) {
	g, gi, ei, ok := t.find(e)
	if !ok {
		return false, false
	}
	hadFocus = t.paneFocus == e
	e.pv.moving()
	g.entries = append(g.entries[:ei:ei], g.entries[ei+1:]...)
	if ei < len(g.sizes) {
		g.sizes = append(g.sizes[:ei:ei], g.sizes[ei+1:]...)
	}
	if g.active >= len(g.entries) || g.active > ei {
		g.active = max(min(g.active-1, len(g.entries)-1), 0)
	}
	if len(g.entries) == 0 {
		t.groups = append(t.groups[:gi:gi], t.groups[gi+1:]...)
		if t.cur > gi || t.cur >= len(t.groups) {
			t.cur = max(t.cur-1, 0)
		}
	}
	if hadFocus {
		t.paneFocus = nil
	}
	return hadFocus, true
}

// afterLeaving is what the panel does when a terminal left it: the keys and the view follow, the last terminal gone hides the
// panel (and the view makes no new one), and the focus goes to the terminal that is now active.
func (t *terminals) afterLeaving(hadFocus bool) {
	t.syncKeys()
	t.handle.Refresh()
	switch {
	case len(t.groups) == 0:
		t.suppressOpen = true
		t.handle.Hide()
	case hadFocus:
		t.focusEntry(t.activeEntry(), false)
	}
}

// ---- the focus ----

// focusEntry makes a terminal the active one of its group and tab and gives its pane the keyboard focus; reveal also shows the
// view (or the editor tab).
func (t *terminals) focusEntry(e *entry, reveal bool) {
	if e == nil {
		return
	}
	if e.inEditor() {
		t.focusEditor(e, reveal)
		return
	}
	g, gi, ei, ok := t.find(e)
	if !ok {
		return
	}
	changed := t.cur != gi || g.active != ei
	t.cur, g.active = gi, ei
	t.lastEditor = nil
	if reveal {
		t.handle.Reveal(true)
	}
	t.wantFocus = e.pv
	if e.pv.render != nil {
		t.wantFocus = nil
		e.pv.requestFocus()
	} else {
		// the pane is not on screen yet: it takes the focus when it is, and the next frame shows it
		t.api.Post(t.handle.Refresh)
	}
	if reveal || changed {
		t.api.Post(func() {
			if e.pv.render != nil {
				e.pv.requestFocus()
			}
		})
	}
	t.syncKeys()
	if changed {
		t.handle.Refresh()
	}
}

// paneMounted is told that a pane came on screen.
func (t *terminals) paneMounted(pv *paneView) {
	if t.wantFocus == pv {
		t.wantFocus = nil
		pv.requestFocus()
	}
}

// paneFocusChanged follows the keyboard focus into and out of the panes.
func (t *terminals) paneFocusChanged(pv *paneView, has bool) {
	for _, e := range t.all() {
		if e.pv != pv {
			continue
		}
		switch {
		case has:
			t.paneFocus = e
			if g, gi, ei, ok := t.find(e); ok {
				t.cur, g.active = gi, ei
			}
			t.lastEditor = nil
			if e.inEditor() {
				t.lastEditor = e
			}
		case t.paneFocus == e:
			t.paneFocus = nil
		}
	}
	t.syncKeys()
	if has {
		t.handle.Refresh()
	}
}

// ---- the context keys ----

// syncKeys gives the context keys their values; only the ones that changed are set.
func (t *terminals) syncKeys() {
	// the terminal the keys describe is the one with the keyboard, also when it is in an editor tab (the panel's active one
	// is not the terminal the user types in then): the shell, the screen and the find widget are its.
	e := t.activeEntry()
	if t.paneFocus != nil {
		e = t.paneFocus
	}
	g := t.activeGroup()
	focused := t.paneFocus
	selectedHere := focused != nil && focused.pv.hasSelection()
	selectedAny := false
	for _, x := range t.all() {
		if x.pv.hasSelection() {
			selectedAny = true
		}
	}
	alt, shellActive := false, false
	if e != nil {
		alt = e.term.Emulator().Modes().AltScreen
		shellActive = e.term.si.Active()
	}
	split := g != nil && len(g.entries) > 1
	// the find widget: shown in the pane of the active terminal; its input may have the keyboard
	findVisible, findInput := false, false
	if e != nil {
		findVisible = e.pv.find.visible
	}
	for _, x := range t.all() {
		findInput = findInput || x.pv.find.inputFocused
	}
	vals := map[string]any{
		keyFocus: focused != nil, keyFocusInAny: focused != nil || findInput, keyIsOpen: t.count() > 0, keyCount: float64(t.count()),
		keyGroupCount: float64(len(t.groups)), keyProcessSupported: true, keyTextSelected: selectedAny,
		keyTextSelectedInFocused: selectedHere, keyFindVisible: findVisible, keyFindFocus: findInput, keyFindInputFocus: findInput,
		keySplitTerminal:   split,
		keySplitPaneActive: split && focused != nil, keyHasBeenCreated: t.created, keyAltBuffer: alt,
		keyTabsFocus: t.tabsFocus, keyEditorFocus: focused != nil && focused.inEditor(), keyShellIntegration: shellActive,
		keySuggestVisible: focused != nil && focused.pv.suggestVisible(),
		keySuggestEnabled: e != nil && !alt && shellActive && t.cfg.sugEnabled && t.cfg.shellInteg,
	}
	if t.keys == nil {
		t.keys = map[string]any{}
	}
	for k, v := range vals {
		if old, ok := t.keys[k]; ok && old == v {
			continue
		}
		t.keys[k] = v
		if err := t.api.Context().Set(k, v); err != nil {
			// The extension is active from the start of the window (SPEC §10.25: the terminals are events and requests), also in a
			// scope that has no context keys; that is not a problem of the terminal.
			if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ExtensionContextUnavailable" {
				t.api.Log().Debug("a context key cannot be set here", "key", k)
				delete(t.keys, k) // tried again at the next change
				continue
			}
			t.api.Log().Warn("a context key cannot be set", "key", k, "err", err)
		}
	}
}

// ---- the tabs ----

// title is the name of the tab of a terminal: the name the user gave it, else terminal.integrated.tabs.title.
func (t *terminals) title(e *entry) string {
	m := e.term.Meta()
	if m.name != "" {
		return m.name
	}
	return tabTitle(t.cfg.tabsTitle, m, e.term.Profile().name, e.term.Cwd())
}

// tabTitle expands the variables of terminal.integrated.tabs.title.
func tabTitle(format string, m meta, profileName, cwd string) string {
	process := m.process
	if process == "" {
		process = profileName
	}
	r := strings.NewReplacer(
		"${process}", process, "${cwd}", cwd, "${cwdFolder}", filepath.Base(cwd), "${sequence}", m.title,
		"${local}", "", "${separator}", "", "${task}", "", "${shellType}", "", "${shellCommand}", "", "${shellPromptInput}", "",
		"${workspaceFolder}", "")
	s := strings.TrimSpace(r.Replace(format))
	if s == "" {
		s = process
	}
	return s
}

// bellOn says that the tab of a terminal shows the bell: the program rang it less than bellDuration ago and
// terminal.integrated.enableVisualBell is on.
func (t *terminals) bellOn(e *entry) bool {
	if !t.cfg.visualBell {
		return false
	}
	at := e.term.Meta().bellAt
	return !at.IsZero() && time.Since(at) < time.Duration(t.cfg.bellDuration)*time.Millisecond
}

// showTabs tells whether the tabs list is on screen (terminal.integrated.tabs.enabled and hideCondition).
func (t *terminals) showTabs() bool {
	if !t.cfg.tabsEnabled {
		return false
	}
	switch t.cfg.tabsHide {
	case "never":
		return t.panelCount() > 0
	case "singleGroup":
		return len(t.groups) > 1
	default:
		return t.panelCount() > 1
	}
}

// ---- the settings ----

// settingsChanged reads the settings again: the panes follow the new font and colors, the emulators the new scrollback.
func (t *terminals) settingsChanged() {
	c := readConfig(t.api.Settings())
	old := t.cfg
	t.cfg = &c
	t.histLimit.Store(int64(c.historyLimit))
	if old == nil || old.scrollback != c.scrollback {
		for _, e := range t.all() {
			e.term.Emulator().SetScrollback(c.scrollback)
		}
	}
	for _, e := range t.all() {
		e.pv.configChanged()
	}
	t.syncKeys() // (terminalSuggestEnabled follows terminal.integrated.suggest.enabled)
	t.handle.Refresh()
}

// infoOf tells where the terminal of a pane is shown and what its tab looks like, for dev.terminals.
func (t *terminals) infoOf(pv *paneView) (location, title, icon, iconColor string) {
	for _, e := range t.all() {
		if e.pv == pv {
			location = locView
			if e.inEditor() {
				location = locEditor
			}
			return location, t.title(e), t.tabIconOf(e), e.term.Meta().color
		}
	}
	return locView, "", "", ""
}
