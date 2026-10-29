package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	extui "github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The window tabs of extensions in the real window (step 11.7, SPEC §10.14, §10.15): the "+" menu, opening, restoring and
// closing a tab, its scope in the links between tabs, its place in the tray, the picker and the keys, and what happens when
// its extension fails or is turned off.

// wtxKind is the kind of the test extension "wtx".
const wtxKind = "wtx.tab"

// wtxProbe is what the test extension wtx does, for the tests to read. Its fields are guarded by mu.
type wtxProbe struct {
	mu sync.Mutex
	// tabs are the contents the extension made, in the order of their creation.
	tabs []*wtxTab
	// panicBuild makes the Build function of the next contents panic.
	panicBuild bool
	// heard are the topics of the events of other tabs the extension got, with the name of the tab they came from.
	heard []string
	// activated counts the activations.
	activated int
	// states are the states the contents got in Create.
	states []string
}

// wtxTab is one tab of the test extension.
type wtxTab struct {
	tab    extension.WindowTab
	note   string
	closed int
}

// factory makes the extension; it is the same extension in every scope.
func (p *wtxProbe) factory() extension.Factory {
	return func() extension.Extension { return &wtxExt{p: p} }
}

type wtxExt struct{ p *wtxProbe }

func (e *wtxExt) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: "wtx", Name: "Wtx", Version: "1.0.0",
		Contributes: extension.Contributes{
			Commands:       []extension.CommandSpec{{ID: "wtx.hello", Title: "Hello Wtx", Category: "Wtx"}},
			WindowTabKinds: []extension.WindowTabKindSpec{{ID: wtxKind, Title: "Wtx Tab", Icon: "note", PlusMenuLabel: "Wtx Tab..."}},
		},
		Events: extension.EventDecl{CrossTab: []string{extension.TopicDocumentChanged}},
	}
}

func (e *wtxExt) Activate(_ context.Context, api extension.API) error {
	p := e.p
	p.mu.Lock()
	p.activated++
	p.mu.Unlock()
	if _, err := api.Events().Subscribe(extension.TopicDocumentChanged, func(ev extension.Event) {
		p.mu.Lock()
		p.heard = append(p.heard, ev.Scope.Name)
		p.mu.Unlock()
	}, extension.FromOtherScopes()); err != nil {
		return err
	}
	if err := api.Commands().Handle("wtx.hello", func(context.Context, any) error { return nil }); err != nil {
		return err
	}
	return api.WindowTabs().Provide(wtxKind, extension.WindowTabKind{Create: func(tab extension.WindowTab, state json.RawMessage) (extension.WindowTabContent, error) {
		c := &wtxTab{tab: tab}
		if len(state) > 0 {
			if err := json.Unmarshal(state, &c.note); err != nil {
				return extension.WindowTabContent{}, err
			}
		}
		p.mu.Lock()
		p.tabs = append(p.tabs, c)
		p.states = append(p.states, c.note)
		explode := p.panicBuild
		p.mu.Unlock()
		return extension.WindowTabContent{
			Build: func(bc widgets.BuildContext) widgets.Widget {
				if explode {
					panic("the widget broke")
				}
				return widgets.NewColoredBox(extui.Theme(bc).EditorBackground, widgets.NewCenter(
					widgets.NewText("wtx note: "+c.note, widgets.TextOpts{Style: extui.TextStyle(bc, 14, extui.Theme(bc).Foreground)})))
			},
			SaveState: func() (any, error) { return c.note, nil },
			OnClose: func() {
				p.mu.Lock()
				c.closed++
				p.mu.Unlock()
			},
		}, nil
	}})
}

// last is the content the extension made last, or nil.
func (p *wtxProbe) last() *wtxTab {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tabs) == 0 {
		return nil
	}
	return p.tabs[len(p.tabs)-1]
}

// extEnv is a run of the program with the extension wtx and a workspace tab.
func (e *multiEnv) runExt(p *wtxProbe, opts Options) *multiApp {
	e.t.Helper()
	return e.runWith(opts, ProcessOptions{Extensions: []extension.Factory{p.factory()}})
}

// openExt opens a tab of the kind wtx.tab and settles.
func (a *multiApp) openExt() *WindowTab {
	a.t.Helper()
	t, err := a.m.OpenExtensionTab(wtxKind, nil)
	if err != nil {
		a.t.Fatal(err)
	}
	a.settle()
	return t
}

// tabNames are the names the strip shows.
func (a *multiApp) tabNames() []string {
	var out []string
	for _, tab := range a.m.workspaceTabs() {
		out = append(out, tab.Name)
	}
	return out
}

func (a *multiApp) text(s string) bool { return a.ts.Exists(gluttertest.ByText(s)) }

func TestAWindowTabOfAnExtensionOpensAfterTheActiveTabAndShowsItsContentUnderTheStrip(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a := env.runExt(p, Options{Folder: one})
	tab := a.openExt()
	if a.m.Count() != 2 || a.m.ActiveTab() != tab || tab.Kind() != TabExtension || tab.KindID() != wtxKind || a.m.IndexOf(tab) != 1 {
		t.Fatalf("%d tabs, active %v kind %v %q", a.m.Count(), a.m.ActiveTab() == tab, tab.Kind(), tab.KindID())
	}
	if !slices.Equal(a.tabNames(), []string{"one", "Wtx Tab"}) {
		t.Errorf("the strip = %q", a.tabNames())
	}
	if !a.text("wtx note: ") {
		t.Fatal("the content of the tab is not on screen")
	}
	// the strip is there, with both tabs, and nothing of a workbench is: no activity bar, side bar, panel or status bar
	if !a.text("Wtx Tab") || !a.text("one") {
		t.Error("the strip does not show both tabs")
	}
	for _, part := range []workbench.Part{workbench.PartActivityBar, workbench.PartSideBar, workbench.PartPanel, workbench.PartStatusBar} {
		if a.ts.Exists(gluttertest.ByKey(workbench.PartKey(part))) {
			t.Errorf("the %v is on screen in a window tab of an extension", part)
		}
	}
	if !a.ts.Exists(gluttertest.ByKey(workbench.PartKey(workbench.PartEditor))) {
		t.Error("the area of the tab is not on screen")
	}
	w := tab.Workspace()
	if w.ext == nil || w.Folder != "" || w.Extensions() == nil || w.Extensions().Info().Kind != extension.ScopeWindowTab {
		t.Fatalf("the workspace of the tab = %+v", w.ext)
	}
	if got, want := w.Extensions().Info().ID, "wtx:"+tab.ID(); got != want {
		t.Errorf("the scope id = %q, want %q", got, want)
	}
	// the title of the window
	if w.ext.windowTitle() != "Wtx Tab - Emetos" {
		t.Errorf("the window title = %q", w.ext.windowTitle())
	}
	// the title the extension sets is on the tab and in the state
	p.last().tab.SetTitle("Ideas")
	p.last().tab.SetHint("my ideas")
	a.settle()
	if !slices.Equal(a.tabNames(), []string{"one", "Ideas"}) || a.m.workspaceTabs()[1].Tooltip != "my ideas" {
		t.Errorf("the strip = %q, tooltip %q", a.tabNames(), a.m.workspaceTabs()[1].Tooltip)
	}
	g, _ := a.p.State.LoadGlobal(a.ctx)
	if len(g.WorkspaceTabs) != 2 || g.WorkspaceTabs[1].Kind != wtxKind || g.WorkspaceTabs[1].Title != "Ideas" || g.WorkspaceTabs[1].ID != tab.ID() || g.ActiveWorkspaceTab != tab.ID() {
		t.Errorf("the state = %+v active %q", g.WorkspaceTabs, g.ActiveWorkspaceTab)
	}
	if len(g.WorkspaceTabs[0].Folder) == 0 || g.LastWindow == nil || g.LastWindow.Folder != one {
		t.Errorf("the workspace tab or the last folder changed: %+v %+v", g.WorkspaceTabs[0], g.LastWindow)
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestThePlusMenuListsWorkspaceThenEachKindAndFollowsTheExtension(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	w := a.m.Active()
	labels := func() []string {
		var out []string
		for _, it := range w.shell.titleBarOptions(w).NewItems() {
			out = append(out, it.Label)
		}
		return out
	}
	if got := labels(); !slices.Equal(got, []string{"Workspace...", "Wtx Tab..."}) {
		t.Fatalf("the entries = %q", got)
	}
	// the menu on screen
	if !w.shell.newMenu.Open() {
		t.Fatal("the menu cannot be opened")
	}
	a.settle()
	if !a.text("Workspace...") || !a.text("Wtx Tab...") {
		t.Error("the menu on screen lacks an entry")
	}
	// choosing the entry opens the tab
	a.ts.Tap(gluttertest.ByText("Wtx Tab..."))
	a.settle()
	a.settle()
	if a.m.Count() != 2 || a.m.ActiveTab().KindID() != wtxKind {
		t.Fatalf("%d tabs, the active one is of kind %q", a.m.Count(), a.m.ActiveTab().KindID())
	}
	// the entry goes with the extension, and comes back with it
	if err := w.setExtensionEnabled("wtx", false); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if got := labels(); !slices.Equal(got, []string{"Workspace..."}) {
		t.Errorf("the entries with the extension off = %q", got)
	}
	if err := w.setExtensionEnabled("wtx", true); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if got := labels(); !slices.Equal(got, []string{"Workspace...", "Wtx Tab..."}) {
		t.Errorf("the entries with the extension on = %q", got)
	}
}

func TestATabOfAnExtensionComesBackAfterARestartWithItsTitleStateAndScope(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a := env.runExt(p, Options{Folder: one})
	tab := a.openExt()
	first := p.last()
	first.note = "remember the milk"
	first.tab.SetTitle("Shopping")
	first.tab.StateChanged()
	a.settle()
	id, scope := tab.ID(), tab.Workspace().Extensions().Info().ID
	// quit: the state is written for the next start
	tab.Workspace().RequestClose(CloseReasonQuit)
	a.waitFor("the quit", func() bool { return a.quits == 1 })
	if first.closed != 0 {
		t.Error("the extension was told that its tab closed for good when Emetos quit")
	}

	p2 := &wtxProbe{}
	b := env.runExt(p2, Options{})
	b.waitFor("both tabs", func() bool { return b.m.Count() == 2 })
	b.settle()
	tabs := b.m.Tabs()
	if len(tabs) != 2 || tabs[1].KindID() != wtxKind || tabs[1].ID() != id {
		t.Fatalf("the restored tabs = %d, %q %q (want %q)", len(tabs), tabs[len(tabs)-1].KindID(), tabs[len(tabs)-1].ID(), id)
	}
	if b.m.ActiveTab() != tabs[1] {
		t.Error("the tab that was active is not the active one")
	}
	if !slices.Equal(p2.states, []string{"remember the milk"}) || !b.text("wtx note: remember the milk") {
		t.Errorf("the restored content got the states %q", p2.states)
	}
	if !slices.Equal(b.tabNames(), []string{"one", "Shopping"}) {
		t.Errorf("the strip = %q", b.tabNames())
	}
	if got := tabs[1].Workspace().Extensions().Info().ID; got != scope {
		t.Errorf("the scope id after the restart = %q, want %q", got, scope)
	}
}

func TestAnExtensionTabThatNoExtensionHasAnyMoreIsLeftOut(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	a.openExt()
	a.m.Active().RequestClose(CloseReasonQuit)
	a.waitFor("the quit", func() bool { return a.quits == 1 })
	logger, logged := savedNotesLog()
	b := env.runWith(Options{}, ProcessOptions{Logger: logger}) // an Emetos without the extension
	b.settle()
	if b.m.Count() != 1 || b.m.Tabs()[0].Kind() != TabWorkspace {
		t.Errorf("%d tabs, the first of kind %v", b.m.Count(), b.m.Tabs()[0].Kind())
	}
	// step 17e.1 (SPEC §10.26): the tab of a removed extension (Scratchpad was one) is dropped with one information line, no dialog
	if lines := strings.Count(logged.String(), "a window tab of an extension is left out"); lines != 1 ||
		!strings.Contains(logged.String(), "level=INFO") || !strings.Contains(logged.String(), "kind="+wtxKind) {
		t.Errorf("the log has %d lines about the tab:\n%s", lines, logged.String())
	}
	if items := b.m.Active().Notify.Items(); len(items) != 0 {
		t.Errorf("the tab that was left out told the user: %+v", items)
	}
	if errs := b.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// removedExtensionWord is the name of the extension of M16 that step 23.1 removed (D32), built here from its rot13 form so that the
// repository never holds it as a word.
func removedExtensionWord() string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return 'a' + (r-'a'+13)%26
		case r >= 'A' && r <= 'Z':
			return 'A' + (r-'A'+13)%26
		}
		return r
	}, "cnenfvgr")
}

// TestTheSavedStateOfTheRemovedEventControlExtensionIsLeftOutAndItsFilesStay is the old state of step 23.1: the last session had a
// window tab of the extension that was removed, with the state it saved, and the extension kept state files of its own. The window
// starts without a dialog, the tab is left out with one line at information level, and nothing on disk is deleted or changed.
// The ids of the extension are built from parts, so that this file does not hold its name.
func TestTheSavedStateOfTheRemovedEventControlExtensionIsLeftOutAndItsFilesStay(t *testing.T) {
	word := removedExtensionWord()
	kind := "emetos." + word + ".tab"
	env := newMultiEnv(t, nil)
	folder := env.folder("one", "a.go")
	state, err := env.paths.StateDir(storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	// the files of the extension, as it left them in the state folder of Emetos: they are the user's, and they stay
	files := map[string]string{
		word + ".json":       `{"version":1,"listening":true,"port":47124,"workspaces":[],"keys":[]}`,
		word + "-hosts.json": `{"version":1,"hosts":{}}`,
		word + "-host-key":   "-----BEGIN OPENSSH PRIVATE KEY-----\nnot a real key\n-----END OPENSSH PRIVATE KEY-----\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(state, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	global, err := json.Marshal(map[string]any{
		"version": 1, "activeWorkspaceTab": "fedcba9876543210",
		"workspaceTabs": []any{
			map[string]any{"id": "0123456789abcdef", "folder": folder},
			map[string]any{"id": "fedcba9876543210", "kind": kind, "title": "Old tab", "state": map[string]any{"draft": "kept in the file"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	globalPath, err := env.paths.GlobalStatePath(storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, global, 0o600); err != nil {
		t.Fatal(err)
	}

	logger, logged := savedNotesLog()
	a := env.runWith(Options{}, ProcessOptions{Logger: logger})
	a.settle()
	if a.m.Count() != 1 || a.m.Tabs()[0].Kind() != TabWorkspace {
		t.Errorf("%d tabs, the first of kind %v", a.m.Count(), a.m.Tabs()[0].Kind())
	}
	if lines := strings.Count(logged.String(), "a window tab of an extension is left out"); lines != 1 || !strings.Contains(logged.String(), "level=INFO") {
		t.Errorf("the log has %d lines about the tab:\n%s", lines, logged.String())
	}
	if items := a.m.Active().Notify.Items(); len(items) != 0 {
		t.Errorf("the tab that was left out told the user: %+v", items)
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
	a.m.Active().RequestClose(CloseReasonQuit)
	a.waitFor("the quit", func() bool { return a.quits == 1 })
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(state, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q (%v), want it left as it was", name, got, err)
		}
	}
}

func TestClosingATabOfAnExtensionAsksNothingTellsTheExtensionAndForgetsTheTab(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	tab := a.openExt()
	c := p.last()
	c.note = "x"
	closed := false
	a.m.Close(tab, func(ok bool) { closed = ok })
	a.settle()
	if !closed || a.m.Count() != 1 || a.m.Active().ext != nil {
		t.Fatalf("closed %v, %d tabs", closed, a.m.Count())
	}
	if a.dialog(tab.Workspace()) != "" {
		t.Error("closing the tab asked a question")
	}
	if c.closed != 1 {
		t.Errorf("OnClose was called %d times", c.closed)
	}
	g, _ := a.p.State.LoadGlobal(a.ctx)
	if len(g.WorkspaceTabs) != 1 || g.WorkspaceTabs[0].Kind != "" {
		t.Errorf("the state = %+v", g.WorkspaceTabs)
	}
	if scopes := a.p.Extensions.Scopes(); len(scopes) != 1 {
		t.Errorf("the host has %d scopes, want the workspace's only", len(scopes))
	}
	// the tab that closes itself, through its handle
	tab2 := a.openExt()
	p.last().tab.Close()
	a.settle()
	if a.m.IndexOf(tab2) != -1 || a.m.Count() != 1 {
		t.Error("Close of the handle did not close the tab")
	}
}

func TestTheLastTabBeingAnExtensionTabClosingLeavesANoFolderTab(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	tab := a.openExt()
	a.m.Close(a.m.Tabs()[0], nil)
	a.settle()
	if a.m.Count() != 1 || a.m.Tabs()[0] != tab {
		t.Fatalf("%d tabs after the workspace closed", a.m.Count())
	}
	a.m.Close(tab, nil)
	a.waitFor("the new tab", func() bool { return a.m.Count() == 1 && a.m.Tabs()[0] != tab })
	a.settle()
	if w := a.m.Active(); w == nil || w.ext != nil || w.Folder != "" || a.m.ActiveTab().Kind() != TabWorkspace {
		t.Error("closing the last tab did not leave a No Folder workspace tab")
	}
}

func TestTheScopeOfATabGetsNoEventsWithoutALinkAndGetsThemAfterAllow(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a := env.runExt(p, Options{Folder: one})
	w1 := a.m.Active()
	// the workspace is in a scope with the extension "wtx" too? no: it has no onStartup, so only a document event reaches nothing
	ext := a.openExt().Workspace()
	// the file is edited in the workspace "one": its core events are published in its scope, which the window tab hears
	// only along a link
	a.edit(w1, filepath.Join(one, "a.go"))
	a.settle()
	if len(p.heard) != 0 {
		t.Fatalf("the tab heard %v without a link", p.heard)
	}
	if !strings.Contains(a.dialog(ext), "one") || !strings.Contains(a.dialog(ext), "Wtx Tab") {
		t.Fatalf("the question in the tab of the extension is %q", a.dialog(ext))
	}
	a.answer(ext, "Allow")
	// the link is saved, from the folder to the tab
	snap := a.p.Extensions.SharingSnapshot()
	if len(snap.Links) != 1 || snap.Links[0].From != filepath.Clean(one) || snap.Links[0].To != ext.Extensions().Info().ID {
		t.Fatalf("links = %+v", snap.Links)
	}
	a.edit(w1, filepath.Join(one, "a.go"))
	a.settle()
	if len(p.heard) == 0 || p.heard[0] != "one" {
		t.Errorf("the tab heard %v after Allow", p.heard)
	}
	// the other direction is not allowed by it: the workspace sends nothing back (it has no subscriber), and the tab publishes
	// nothing the workspace could hear
}

func TestTheTrayMenuAndThePickerListATabOfAnExtension(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a, fd := env.runRoot(Options{Folder: one}, ProcessOptions{Extensions: []extension.Factory{p.factory()}}, true)
	a.waitFor("the tray", fd.TrayRegistered)
	tab := a.openExt()
	p.last().tab.SetTitle("Ideas")
	a.settle()
	// the tray's menu: one radio item for each tab, the extension's by its title
	if labels := fd.TrayMenu(); len(labels) < 2 || labels[0] != "one" || labels[1] != "Ideas" {
		t.Errorf("the tray menu = %q", labels)
	}
	if got := a.m.trayTooltipBody(); got != "1 workspace" {
		t.Errorf("the tooltip counts %q: a tab of an extension is not a workspace", got)
	}
	// the picker, opened from the tab of the extension
	w := tab.Workspace()
	if err := a.command(w, "workbench.action.switchWindow", nil); err != nil {
		t.Fatal(err)
	}
	if !w.QuickInput.Visible() {
		t.Fatal("the picker is not shown in the tab of the extension")
	}
	a.settle()
	items := w.QuickInput.Picker().Items()
	if len(items) != 2 || items[0].Label != "Ideas" || !strings.HasPrefix(items[0].Description, SwitchTabCurrent) || !strings.Contains(items[0].Description, "Wtx") {
		t.Fatalf("the rows = %+v", items)
	}
	if items[1].Label != "one" {
		t.Errorf("the second row = %+v", items[1])
	}
	// choosing the workspace tab leaves the tab of the extension
	w.QuickInput.Accept(quickinput.KeyMods{})
	a.settle()
	if a.m.ActiveTab() == tab {
		t.Error("choosing the row did not switch")
	}
}

func TestTheKeysThatSwitchTabsAndCloseWorkInATabOfAnExtension(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	tab := a.openExt()
	w := tab.Workspace()
	// the key of the previous tab, through the dispatcher of the tab
	chord, err := keybinding.ParseKeybinding("ctrl+alt+pageup")
	if err != nil || len(chord) != 1 {
		t.Fatal(err)
	}
	if !w.DispatchKey(chord[0], nil) {
		t.Fatal("the key was not used")
	}
	a.settle()
	if a.m.ActiveTab() == tab {
		t.Error("the key did not switch to the previous tab")
	}
	// and back, with the number key of a place of the strip
	if err := a.command(a.m.Active(), "emetos.workspaceTab.open2", nil); err != nil {
		t.Fatal(err)
	}
	if a.m.ActiveTab() != tab {
		t.Error("Alt+2 did not make the second tab the active one")
	}
	// the command of the palette: the tab of the extension has the commands of the window
	if _, ok := w.Commands.Lookup("wtx.hello"); !ok {
		t.Error("the command the extension declares is not in the tab")
	}
	// New Workspace Tab from the tab of the extension
	if err := a.command(w, "workbench.action.newWindow", nil); err != nil {
		t.Fatal(err)
	}
	if a.m.Count() != 3 || a.m.ActiveTab().Kind() != TabWorkspace {
		t.Errorf("%d tabs after New Workspace Tab", a.m.Count())
	}
	// Close Window closes the tab of the extension, Quit asks every tab and ends the application
	if err := a.command(w, "workbench.action.closeWindow", nil); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if a.m.Count() != 2 {
		t.Errorf("%d tabs after Close Workspace Tab", a.m.Count())
	}
	ext := a.openExt().Workspace()
	if err := a.command(ext, "workbench.action.quit", nil); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the quit", func() bool { return a.quits == 1 })
}

func TestAPanicInTheWidgetOfATabShowsTheFailureInTheTabOnly(t *testing.T) {
	p := &wtxProbe{panicBuild: true}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	tab := a.openExt()
	a.settle()
	a.settle()
	scope := tab.Workspace().Extensions()
	if st := scope.State("wtx"); st != exthost.StateFailed {
		t.Fatalf("the extension is %v", st)
	}
	appErr, ok := errors.FromError(scope.Failure("wtx").Err)
	if !ok || !appErr.HasRoute("ExtensionViewBuildPanicked") {
		t.Errorf("the failure = %v", scope.Failure("wtx").Err)
	}
	if !a.text("Wtx failed") || !a.text("Show Log") {
		t.Error("the tab does not show the failure with a Show Log button")
	}
	// Show Log has no panel in this tab: the workspace tab comes to the front with the Output panel
	w1 := a.m.Tabs()[0].Workspace()
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByKey(workbench.PartKey(workbench.PartEditor)), gluttertest.ByText("Show Log")))
	a.settle()
	a.settle()
	if a.m.Active() != w1 || !w1.Layout.State().PanelVisible || w1.Views.ActivePanel() != command.ViewContainerOutput {
		t.Errorf("Show Log: the active tab is the workspace %v, the panel is shown %v, shows %q", a.m.Active() == w1, w1.Layout.State().PanelVisible, w1.Views.ActivePanel())
	}
	// the strip and the other tab are untouched
	a.m.ActivateIndex(0)
	a.settle()
	if !a.ts.Exists(gluttertest.ByKey(workbench.PartKey(workbench.PartSideBar))) || a.text("Wtx failed") {
		t.Error("the failure reached the workspace tab")
	}
	if a.m.Count() != 2 {
		t.Errorf("%d tabs: a failure closed one", a.m.Count())
	}
}

func TestTurningTheOwnerOffKeepsTheTabWithItsStateAndTurningItOnBringsTheContentBack(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	tab := a.openExt()
	p.last().note = "kept"
	w := tab.Workspace()
	if err := w.setExtensionEnabled("wtx", false); err != nil {
		t.Fatal(err)
	}
	a.settle()
	a.settle()
	if a.m.IndexOf(tab) != 1 || a.m.Count() != 2 {
		t.Fatal("turning the extension off closed its tab")
	}
	if !a.text("Wtx is turned off. Turn it on in the Extensions page to use this tab.") {
		t.Error("the tab does not say that its extension is off")
	}
	g, _ := a.p.State.LoadGlobal(a.ctx)
	if len(g.WorkspaceTabs) != 2 || string(g.WorkspaceTabs[1].State) != `"kept"` {
		t.Errorf("the state of the tab = %+v", g.WorkspaceTabs)
	}
	// a tab cannot be opened while the extension is off
	_, err := a.m.OpenExtensionTab(wtxKind, nil)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("OpenExtensionTab.WindowTabKindDisabled") {
		t.Errorf("OpenExtensionTab with the extension off = %v", err)
	}
	if err := w.setExtensionEnabled("wtx", true); err != nil {
		t.Fatal(err)
	}
	a.settle()
	a.settle()
	if !a.text("wtx note: kept") {
		t.Error("the content did not come back with its state")
	}
}

func TestALaunchWhileATabOfAnExtensionIsActiveGoesToAWorkspace(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go", "b.go")
	a := env.runExt(p, Options{Folder: one})
	tab := a.openExt()
	r := a.launched(ipc.OpenArgs{Files: []ipc.FileTarget{{Path: filepath.Join(one, "b.go")}}})
	if r.outcomes[0].ExitCode != 0 {
		t.Fatalf("the launch failed: %+v", r.outcomes)
	}
	a.settle()
	if a.m.ActiveTab() == tab || a.m.Active().Folder != one {
		t.Error("the launch did not bring the workspace tab to the front")
	}
	if _, ok := a.m.Active().Documents.Get(document.FileURI(filepath.Join(one, "b.go"))); !ok {
		t.Error("the file was not opened in the workspace")
	}
	// only tabs of extensions are open: the launch opens a tab of its own
	a.m.Close(a.m.Tabs()[0], nil)
	a.settle()
	if a.m.Count() != 1 || a.m.Tabs()[0] != tab {
		t.Fatalf("%d tabs", a.m.Count())
	}
	r = a.launched(ipc.OpenArgs{Files: []ipc.FileTarget{{Path: filepath.Join(one, "a.go")}}})
	a.settle()
	if r.outcomes[0].ExitCode != 0 || a.m.Count() != 2 || a.m.Active().ext != nil {
		t.Errorf("exit %d, %d tabs: the launch did not open a workspace tab", r.outcomes[0].ExitCode, a.m.Count())
	}
}

func TestTheDevCommandsOpenATabAndThePlusMenu(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	w := a.m.Active()
	if err := a.command(w, "dev.openWindowTab", map[string]any{"kind": wtxKind}); err != nil {
		t.Fatal(err)
	}
	if a.m.Count() != 2 || a.m.ActiveTab().KindID() != wtxKind {
		t.Fatalf("%d tabs", a.m.Count())
	}
	// the next step runs in the tab that is active now
	if err := a.command(a.m.Active(), "dev.openPlusMenu", nil); err != nil {
		t.Fatal(err)
	}
	if !a.text("Wtx Tab...") {
		t.Error("the menu is not open")
	}
	if err := a.command(a.m.Active(), "dev.openWindowTab", wtxKind); err != nil || a.m.Count() != 3 {
		t.Errorf("the kind as a string: %v, %d tabs", err, a.m.Count())
	}
	// no strip is on screen to open the menu of
	a.m.Active().shell.newMenu = workbench.NewNewMenuController()
	tests := []struct {
		name string
		id   string
		args any
		want string
	}{
		{"no strip on screen", "dev.openPlusMenu", nil, "DevOpenPlusMenuUnavailable"},
		{"no kind", "dev.openWindowTab", nil, "DevOpenWindowTabInvalid"},
		{"a number", "dev.openWindowTab", 3.0, "DevOpenWindowTabInvalid"},
		{"a kind that is not there", "dev.openWindowTab", "wtx.nothing", "OpenExtensionTab->Workspaces.prepareExtension.WindowTabKindUnknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.m.Active().Commands.Run(command.Context{Ctx: a.ctx, Source: command.SourceAPI}, tt.id, tt.args)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tt.want) {
				t.Errorf("error = %v, want the route %s", err, tt.want)
			}
		})
	}
}

func TestShowLogOfATabWithNoWorkspaceOpensOne(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	tab := a.openExt()
	a.m.Close(a.m.Tabs()[0], nil)
	a.settle()
	if a.m.Count() != 1 || a.m.Tabs()[0] != tab {
		t.Fatalf("%d tabs", a.m.Count())
	}
	if err := a.command(tab.Workspace(), "workbench.panel.output.focus", nil); err != nil {
		t.Fatal(err)
	}
	a.waitFor("a workspace tab with the panel", func() bool {
		w := a.m.Active()
		return w != nil && w.ext == nil && w.Layout.State().PanelVisible
	})
	if a.m.Count() != 2 {
		t.Errorf("%d tabs, want the tab of the extension and a new workspace", a.m.Count())
	}
}

func TestThePaletteAndThePickerOpenATabOfAnExtension(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	w := a.m.Active()
	if err := a.command(w, "emetos.windowTab.open", nil); err != nil {
		t.Fatal(err)
	}
	if !w.QuickInput.Visible() {
		t.Fatal("the picker of the kinds is not shown")
	}
	a.settle()
	items := w.QuickInput.Picker().Items()
	if len(items) != 1 || items[0].Label != "Wtx Tab" || items[0].Description != "Wtx" {
		t.Fatalf("the rows = %+v", items)
	}
	w.QuickInput.Accept(quickinput.KeyMods{})
	a.settle()
	if a.m.Count() != 2 || a.m.ActiveTab().KindID() != wtxKind {
		t.Errorf("%d tabs", a.m.Count())
	}
	// with the extension off the command says so
	if err := w.setExtensionEnabled("wtx", false); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if err := a.command(a.m.Active(), "emetos.windowTab.open", map[string]any{"kind": wtxKind}); err != nil {
		t.Fatal(err)
	}
	if a.m.Count() != 2 {
		t.Error("a tab of a turned off extension opened")
	}
	var notes []string
	for _, it := range a.m.Active().Notify.Items() {
		notes = append(notes, it.Message)
	}
	if !slices.ContainsFunc(notes, func(s string) bool { return strings.Contains(s, "Cannot open the window tab") }) {
		t.Errorf("notifications = %q", notes)
	}
}

func TestThePlusMenuOfAnExtensionTabWorksFromTheTabItself(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	tab := a.openExt()
	w := tab.Workspace()
	if !w.shell.newMenu.Open() {
		t.Fatal("the menu cannot be opened from the tab of an extension")
	}
	a.settle()
	if !a.text("Workspace...") || !a.text("Wtx Tab...") {
		t.Error("the menu of the strip drawn by the tab lacks an entry")
	}
}

// ---- goldens ----

// goldenExt shows the window with a workspace tab and a tab of the extension in the theme, in the test font.
func goldenExt(t *testing.T, theme string) (*multiApp, *wtxProbe) {
	t.Helper()
	p := &wtxProbe{}
	env := newMultiEnv(t, map[string]any{"workbench.colorTheme": theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false})
	a := env.runExt(p, Options{Folder: env.folder("emetos", "main.go", "go.mod")})
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	for _, w := range a.m.All() {
		w.shell.SetState(func() {})
	}
	a.settle()
	return a, p
}

func TestGoldenAWindowTabOfAnExtension(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			a, p := goldenExt(t, th.theme)
			a.openExt()
			p.last().note = "remember the milk"
			p.last().tab.Refresh()
			a.settle()
			testutil.MatchesGolden(a.ts, "window_tab_extension_"+th.name)
			if errs := a.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestGoldenThePlusMenuWithAnExtensionEntry(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			a, _ := goldenExt(t, th.theme)
			if !a.m.Active().shell.newMenu.Open() {
				t.Fatal("the menu cannot be opened")
			}
			a.settle()
			testutil.MatchesGolden(a.ts, "plus_menu_extension_"+th.name)
			if errs := a.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestAWindowTabThatCapturesTheKeysKeepsOnlyTheShortcutsOfTheWindow(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	a := env.runExt(p, Options{Folder: env.folder("one", "a.go")})
	tab := a.openExt()
	w := tab.Workspace()
	host := extTabHost{w: w}
	focusView := func(on bool) {
		if on {
			w.Keys.Set("focusedView", exthost.WindowTabViewID)
		} else {
			w.Keys.Reset("focusedView")
		}
	}
	palette := keybinding.Chord{Ctrl: true, Shift: true, Key: keybinding.KeyP}
	cases := []struct {
		name    string
		capture bool
		focused bool
		chord   keybinding.Chord
		want    bool
	}{
		{"the tab does not capture", false, true, palette, false},
		{"the content has no focus", true, false, palette, false},
		{"a shortcut of the editor is the content's", true, true, palette, true},
		{"a plain key is the content's", true, true, keybinding.Chord{Key: keybinding.KeyA}, true},
		{"next tab stays with the window", true, true, keybinding.Chord{Ctrl: true, Alt: true, Key: keybinding.PageDown}, false},
		{"tab number 3 stays with the window", true, true, keybinding.Chord{Ctrl: true, Alt: true, Key: keybinding.Digit3}, false},
		{"close the tab stays with the window", true, true, keybinding.Chord{Ctrl: true, Shift: true, Key: keybinding.KeyW}, false},
		{"quit stays with the window", true, true, keybinding.Chord{Ctrl: true, Key: keybinding.KeyQ}, false},
		{"full screen stays with the window", true, true, keybinding.Chord{Key: keybinding.F11}, false},
		{"a modifier alone is left to the dispatcher", true, true, keybinding.Chord{Ctrl: true, Key: keybinding.Ctrl}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host.CaptureKeys(tc.capture)
			focusView(tc.focused)
			if got := w.capturesKey(tc.chord); got != tc.want {
				t.Errorf("capturesKey(%s) = %v, want %v", tc.chord.Label(), got, tc.want)
			}
		})
	}
	// the dispatch itself: a captured key runs nothing and is not used, a window shortcut runs its command
	host.CaptureKeys(true)
	focusView(true)
	ran := ""
	for _, id := range []string{"workbench.action.showCommands", "emetos.workspaceTab.next"} {
		remove, err := w.Commands.SetHandler(id, func(command.Context, any) error { ran += id + ";"; return nil })
		if err != nil {
			t.Fatal(err)
		}
		defer remove()
	}
	if w.DispatchKey(palette, nil) || ran != "" {
		t.Errorf("a captured key was used: ran %q", ran)
	}
	if !w.DispatchKey(keybinding.Chord{Ctrl: true, Alt: true, Key: keybinding.PageDown}, nil) || ran != "emetos.workspaceTab.next;" {
		t.Errorf("the window shortcut did not run: %q", ran)
	}
}

// TestAnExtensionOpensItsWindowTabOnceAndShowsTheOpenOne is the OpenWindowTab of the API (step 17e.4: the open command of Remote Work): the
// first call opens the tab, the next ones show it, also from another tab, and a kind nobody has fails with the window's error.
func TestAnExtensionOpensItsWindowTabOnceAndShowsTheOpenOne(t *testing.T) {
	p := &wtxProbe{}
	env := newMultiEnv(t, nil)
	one := env.folder("one", "a.go")
	a := env.runExt(p, Options{Folder: one})
	first := a.m.ActiveTab()
	ws := first.Workspace()
	if err := ws.openWindowTab(wtxKind); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tab := a.m.ActiveTab()
	if a.m.Count() != 2 || tab == first || tab.KindID() != wtxKind {
		t.Fatalf("after the first call: %d tabs, the active one is the kind %q", a.m.Count(), tab.KindID())
	}
	// back on the first tab, the second call shows the tab that is open and makes no other
	a.m.Activate(first)
	a.settle()
	if err := ws.openWindowTab(wtxKind); err != nil {
		t.Fatal(err)
	}
	a.settle()
	if a.m.Count() != 2 || a.m.ActiveTab() != tab {
		t.Errorf("after the second call: %d tabs, the extension's tab is active: %v", a.m.Count(), a.m.ActiveTab() == tab)
	}
	err := ws.openWindowTab("nobody.tab")
	if err == nil {
		t.Fatal("a kind nobody has opened a tab")
	}
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "WindowTabKindUnknown" {
		t.Errorf("error = %v, want the tag WindowTabKindUnknown", err)
	}
}
