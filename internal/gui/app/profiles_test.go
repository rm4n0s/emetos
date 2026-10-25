package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/snippet"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The tests of the profiles in the window (step 8.6): the profile of a window, switching it in place, two windows
// with two profiles, --profile, the profile last used with a folder, and the files the window follows.

// profileFolders is a set of isolated folders with two profiles: the default one and "Work". They differ in what a
// window shows and does: the font size, the tab size, the color theme, a key and a snippet.
type profileFolders struct {
	t     *testing.T
	paths storage.Paths
	clock *storagetest.Clock
}

const (
	workKeys     = `[{ "key": "ctrl+alt+9", "command": "workbench.action.openGlobalKeybindings" }, { "key": "ctrl+,", "command": "-workbench.action.openSettings" }]`
	defaultSnip  = `{ "d": { "prefix": "dflt", "body": "DEFAULT" } }`
	workSnippets = `{ "w": { "prefix": "wrk", "body": "WORK" } }`
)

func newProfileFolders(t *testing.T) *profileFolders {
	t.Helper()
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"editor.fontSize": 14, "editor.tabSize": 4, "files.hotExit": "off"})
	f := &profileFolders{t: t, paths: paths, clock: storagetest.NewClock()}
	f.write(storage.DefaultProfile, "snippets/plaintext.json", defaultSnip)
	if _, err := f.profiles().Create(context.Background(), "Work", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	f.write("Work", "settings.json", fmt.Sprintf(
		`{ "editor.fontFamily": %q, "editor.fontSize": 22, "editor.tabSize": 2, "workbench.colorTheme": "Light Modern", "files.hotExit": "off" }`, testutil.MonoFamily))
	f.write("Work", "keybindings.json", workKeys)
	f.write("Work", "snippets/plaintext.json", workSnippets)
	return f
}

func (f *profileFolders) profiles() *storage.Profiles {
	return storage.NewProfiles(f.paths, storage.ProfilesOptions{})
}

// write puts a file in the configuration folder of a profile.
func (f *profileFolders) write(profile, rel, text string) string {
	f.t.Helper()
	dir, err := f.paths.ConfigDir(profile)
	if err != nil {
		f.t.Fatal(err)
	}
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		f.t.Fatal(err)
	}
	return path
}

// process is a process over the folders, with the clock of the state moved by hand.
func (f *profileFolders) process(opts ProcessOptions) *Process {
	opts.Paths = f.paths
	if opts.StateAfter == nil {
		opts.StateAfter = f.clock.AfterFunc
	}
	return newTestProcess(opts)
}

// window makes a window over a folder ("" for none) with a profile ("" is the default one).
func (f *profileFolders) window(p *Process, folder, profile string) *Workspace {
	f.t.Helper()
	id, ok, err := p.Profiles.Lookup(context.Background(), profile)
	if err != nil || !ok {
		f.t.Fatalf("no profile %q: %v", profile, err)
	}
	win, err := newWorkspace(context.Background(), p, workspaceOptions{folder: folder, profile: id})
	if err != nil {
		f.t.Fatal(err)
	}
	return win
}

func snippetNames(w *Workspace) []string {
	var out []string
	for _, s := range w.Snippets.Snippets(snippet.Query{LanguageID: "plaintext"}) {
		out = append(out, s.Name)
	}
	return out
}

func themeLabel(w *Workspace) string { return w.Themes.CurrentInfo().Label }

func tabSizeOf(w *Workspace) int { return w.indentationFor("plaintext").TabSize }

// profileApp is a window with two files open, shown in a tester.
type profileApp struct {
	*groupApp
	f *profileFolders
}

func newProfileApp(t *testing.T, opts ProcessOptions, profile string) *profileApp {
	t.Helper()
	f := newProfileFolders(t)
	win := f.window(f.process(opts), "", profile)
	g := newGroupAppWith(t, win, testWindowSize, []string{"a.go", "long.go"}, nil, "a.go", "long.go")
	return &profileApp{groupApp: g, f: f}
}

// editors are the view models of the editors that show the files, by file name.
func (a *profileApp) editors() map[string]*editor.ViewModel {
	a.t.Helper()
	out := map[string]*editor.ViewModel{}
	for name, doc := range a.docs {
		for _, e := range a.shell().group.editors {
			if e.view.Document() == doc {
				out[name] = e.view
			}
		}
	}
	return out
}

func (a *profileApp) switchTo(name string) {
	a.t.Helper()
	if err := a.win.SwitchProfile(context.Background(), name); err != nil {
		a.t.Fatalf("SwitchProfile(%q): %v", name, err)
	}
	a.pump()
}

// waitFontSize pumps until the editor has the font size (it follows the settings through the binding).
func (a *profileApp) waitFontSize(vm *editor.ViewModel, size float64) {
	a.t.Helper()
	for range 100 {
		if vm.Options().FontSize == size {
			return
		}
		a.ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	a.t.Fatalf("the editor has the font size %v, want %v", vm.Options().FontSize, size)
}

func TestSwitchingTheProfileChangesTheSettingsAndTheKeysWithoutClosingEditors(t *testing.T) {
	a := newProfileApp(t, ProcessOptions{}, "")
	if a.win.ProfileName() != "Default" || a.win.Profile() != storage.DefaultProfile {
		t.Fatalf("the profile of the window is %q", a.win.ProfileName())
	}
	// two editors with a cursor each, one of them scrolled
	vms := a.editors()
	if len(vms) != 2 {
		t.Fatalf("%d editors", len(vms))
	}
	cursors := map[string][]buffer.Selection{"a.go": {cursorAt(1, 3)}, "long.go": {cursorAt(40, 5)}}
	for name, vm := range vms {
		vm.Cursors().SetSelections(cursors[name])
	}
	vms["long.go"].SetScrollTop(480)
	a.pump()
	docText := map[string]string{}
	docVersion := map[string]int{}
	for name, doc := range a.docs {
		docText[name], docVersion[name] = doc.Text(), doc.Version()
	}
	tabs := a.tabNames()

	// what the default profile does
	if got := a.vm().Options().FontSize; got != 14 {
		t.Errorf("font size %v before the switch", got)
	}
	if got := tabSizeOf(a.win); got != 4 {
		t.Errorf("tab size %d before the switch", got)
	}
	if got := themeLabel(a.win); got != "Dark Modern" {
		t.Errorf("theme %q before the switch", got)
	}
	if got := a.win.keyLabelOf("workbench.action.openSettings"); got != "Ctrl+," {
		t.Errorf("the key of Settings is %q before the switch", got)
	}
	if got := a.win.keyLabelOf("workbench.action.openGlobalKeybindings"); got == "Ctrl+Alt+9" {
		t.Errorf("the key of Work is bound in the default profile")
	}
	if got := snippetNames(a.win); !slices.Equal(got, []string{"d"}) {
		t.Errorf("snippets %v before the switch", got)
	}
	var heard []string
	stop := a.win.OnProfileChange(func(profile string) { heard = append(heard, profile) })
	defer stop()

	a.switchTo("work")

	if a.win.ProfileName() != "Work" || a.win.Profile() != "Work" {
		t.Errorf("the profile of the window is %q", a.win.ProfileName())
	}
	for _, vm := range vms {
		a.waitFontSize(vm, 22)
	}
	if got := tabSizeOf(a.win); got != 2 {
		t.Errorf("tab size %d after the switch", got)
	}
	if got := themeLabel(a.win); got != "Light Modern" {
		t.Errorf("theme %q after the switch", got)
	}
	if got := uiBackground(t, a.ts); got != lightBackground {
		t.Errorf("the background is %v after the switch", got)
	}
	if got := snippetNames(a.win); !slices.Equal(got, []string{"w"}) {
		t.Errorf("snippets %v after the switch", got)
	}
	if got := a.win.keyLabelOf("workbench.action.openGlobalKeybindings"); got != "Ctrl+Alt+9" {
		t.Errorf("the key of Work is %q after the switch", got)
	}
	if got := a.win.keyLabelOf("workbench.action.openSettings"); got != "" {
		t.Errorf("the default key of Settings is %q after the switch", got)
	}
	a.pressUnbound("ctrl+,") // the default key is gone
	wantNames(t, a.tabNames(), tabs...)

	// the editors, documents, cursors and scroll were not touched
	after := a.editors()
	for name, vm := range vms {
		if after[name] != vm {
			t.Errorf("the editor of %s was made again", name)
		}
		if got := vm.Cursors().Selections(); !slices.Equal(got, cursors[name]) {
			t.Errorf("the cursors of %s are %v, want %v", name, got, cursors[name])
		}
	}
	if got := vms["long.go"].ScrollTop(); got != 480 {
		t.Errorf("the scroll of long.go is %v", got)
	}
	for name, doc := range a.docs {
		if doc.Dirty() || doc.Text() != docText[name] || doc.Version() != docVersion[name] {
			t.Errorf("the document %s changed: dirty %v version %d", name, doc.Dirty(), doc.Version())
		}
	}
	wantNames(t, a.tabNames(), tabs...)

	// the key of the new profile works
	a.press("ctrl+alt+9")
	if got := a.tabNames(); len(got) != len(tabs)+1 || !slices.Contains(got, "Keyboard Shortcuts") {
		t.Errorf("tabs %v after the key of Work", got)
	}

	// and back: everything is as it was
	a.switchTo("Default")
	for _, vm := range vms {
		a.waitFontSize(vm, 14)
	}
	if got := tabSizeOf(a.win); got != 4 {
		t.Errorf("tab size %d after the switch back", got)
	}
	if got := themeLabel(a.win); got != "Dark Modern" {
		t.Errorf("theme %q after the switch back", got)
	}
	if got := snippetNames(a.win); !slices.Equal(got, []string{"d"}) {
		t.Errorf("snippets %v after the switch back", got)
	}
	if got := a.win.keyLabelOf("workbench.action.openSettings"); got != "Ctrl+," {
		t.Errorf("the key of Settings is %q after the switch back", got)
	}
	if got := a.win.keyLabelOf("workbench.action.openGlobalKeybindings"); got == "Ctrl+Alt+9" {
		t.Errorf("the key of Work is still bound after the switch back")
	}
	if !slices.Equal(heard, []string{"Work", storage.DefaultProfile}) {
		t.Errorf("OnProfileChange heard %q", heard)
	}
	// a switch to the profile the window has is nothing
	a.switchTo("default")
	if len(heard) != 2 {
		t.Errorf("a switch to the same profile told the listeners: %q", heard)
	}
	a.noErrors()
}

func TestSwitchingTheProfileChangesTheSettingsEditorAndTheKeybindingsEditor(t *testing.T) {
	a := newProfileApp(t, ProcessOptions{}, "")
	a.run("workbench.action.openSettings")
	a.run("workbench.action.openGlobalKeybindings")
	if len(a.settingsInputs()) != 1 || len(a.keybindingsInputs()) != 1 {
		t.Fatal("the editors are not open")
	}
	tabs := a.tabNames()
	row := func(key string) settings.Row {
		t.Helper()
		r, ok := a.win.SettingsModel().Row(key, settings.TargetUser, "")
		if !ok {
			t.Fatalf("no row %s", key)
		}
		return r
	}
	hasKey := func(command, key string) bool {
		for _, r := range a.win.KeybindingsModel().Rows() {
			if r.Command == command && r.Key == key {
				return true
			}
		}
		return false
	}
	if got := row("editor.tabSize"); got.Value != float64(4) {
		t.Errorf("the settings editor shows %v for editor.tabSize", got.Value)
	}
	if hasKey("workbench.action.openGlobalKeybindings", "ctrl+alt+9") {
		t.Error("the keybindings editor shows the key of Work in the default profile")
	}
	model := a.win.KeybindingsModel()

	a.switchTo("Work")

	if got := row("editor.tabSize"); got.Value != float64(2) || !got.Modified {
		t.Errorf("after the switch the settings editor shows %v (modified %v)", got.Value, got.Modified)
	}
	if got := row("editor.fontSize"); got.Value != float64(22) {
		t.Errorf("after the switch the settings editor shows %v for editor.fontSize", got.Value)
	}
	if !hasKey("workbench.action.openGlobalKeybindings", "ctrl+alt+9") {
		t.Error("the keybindings editor does not show the key of Work")
	}
	if a.win.KeybindingsModel() == model {
		t.Error("the model of the keybindings editor was not made again")
	}
	for _, r := range a.win.KeybindingsModel().Rows() {
		if r.Command == "workbench.action.openSettings" && r.Key == "ctrl+," && r.Source == keybinding.SourceDefault {
			t.Error("the default key of Settings is shown as in use")
		}
	}
	wantNames(t, a.tabNames(), tabs...)
	if len(a.settingsInputs()) != 1 || len(a.keybindingsInputs()) != 1 {
		t.Error("an editor was closed")
	}
	a.noErrors()
}

func TestAnOpenSettingsFileOfTheOldProfileIsAPlainFileAfterTheSwitch(t *testing.T) {
	a := newProfileApp(t, ProcessOptions{}, "")
	userPath, _ := a.win.Settings.Paths()
	workPath, err := settings.UserSettingsPath(a.f.paths, "Work")
	if err != nil {
		t.Fatal(err)
	}
	a.files["settings.json"] = userPath
	a.show("settings.json", false)
	doc := a.doc("settings.json")

	a.switchTo("Work")
	if got := tabSizeOf(a.win); got != 2 {
		t.Fatalf("tab size %d", got)
	}
	// the document of the default profile's file is edited and saved: it is a file, and the window does not read it
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "// edited\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Save(context.Background(), document.SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	data, err := os.ReadFile(userPath)
	if err != nil || !strings.HasPrefix(string(data), "// edited\n") {
		t.Errorf("the file of the old profile: %q, %v", data, err)
	}
	if got, _ := a.win.Settings.Paths(); got != workPath {
		t.Errorf("the settings file of the window is %q", got)
	}
	if got := tabSizeOf(a.win); got != 2 {
		t.Errorf("the tab size is %d: the window read the file of the old profile", got)
	}
	// the settings of the window are written to the file of the new profile
	if err := a.win.Settings.Update(context.Background(), "editor.tabSize", 6, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if text, err := os.ReadFile(workPath); err != nil || !strings.Contains(string(text), "6") {
		t.Errorf("the file of the new profile: %q, %v", text, err)
	}
	if text, err := os.ReadFile(userPath); err != nil || strings.Contains(string(text), "6") {
		t.Errorf("the file of the old profile changed: %q, %v", text, err)
	}
	// a document of the file of the new profile is read and edited as the settings are
	a.files["work.json"] = workPath
	a.show("work.json", false)
	a.switchTo("default")
	a.switchTo("work")
	if got := tabSizeOf(a.win); got != 6 {
		t.Errorf("tab size %d after the switches", got)
	}
	a.noErrors()
}

func TestTheStateOfTheWindowGoesToTheFolderOfTheNewProfile(t *testing.T) {
	a := newProfileApp(t, ProcessOptions{}, "")
	id := a.win.StateID()
	if id == "" {
		t.Fatal("the window has no state")
	}
	p := a.win.Process
	statePath := func(profile string) string {
		path, err := p.Paths.WindowStatePath(profile, id)
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	a.shell().flushState()
	if _, err := os.Stat(statePath(storage.DefaultProfile)); err != nil {
		t.Fatalf("the state of the default profile: %v", err)
	}
	if _, err := os.Stat(statePath("Work")); !os.IsNotExist(err) {
		t.Fatalf("the state of Work exists before the switch: %v", err)
	}
	defaultState, err := os.ReadFile(statePath(storage.DefaultProfile))
	if err != nil {
		t.Fatal(err)
	}

	a.switchTo("Work")
	// the state of the old profile is as it was when the window left it
	if got, err := os.ReadFile(statePath(storage.DefaultProfile)); err != nil || string(got) != string(defaultState) {
		t.Errorf("the state of the old profile changed: %v", err)
	}
	// a change of the window is saved in the folder of the new profile after the delay
	a.show("a.go", false)
	a.clickTab("long.go")
	a.shell().flushState()
	state, found, err := p.mustStateFor(t, "Work").LoadWindow(context.Background(), id)
	if err != nil || !found || state.Grid == nil {
		t.Fatalf("the state of Work: found %v, %v", found, err)
	}
	var names []string
	for _, n := range state.Grid.Root {
		if n.Model != nil {
			for _, e := range n.Model.Editors {
				names = append(names, filepath.Base(strings.TrimPrefix(e.Value, "file://")))
			}
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"a.go", "long.go"}) {
		t.Errorf("the editors in the state of Work: %v", names)
	}
}

// mustStateFor is StateFor for a test.
func (p *Process) mustStateFor(t *testing.T, profile string) *storage.StateStore {
	t.Helper()
	s, err := p.StateFor(profile)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTwoWindowsWithTwoProfilesDoNotMeet(t *testing.T) {
	env := newMultiEnv(t, nil)
	work := workProfile(t, env)
	one, two, three := env.folder("one", "a.go"), env.folder("two", "b.go"), env.folder("three", "c.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two, Profile: "work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.m.Open(WorkspaceSpec{Folder: three}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2, w3 := a.workspaceOf(one), a.workspaceOf(two), a.workspaceOf(three)
	if w1.ProfileName() != "Default" || w2.ProfileName() != "Work" || w3.ProfileName() != "Default" {
		t.Fatalf("profiles %s %s %s", w1.ProfileName(), w2.ProfileName(), w3.ProfileName())
	}
	size := func(w *Workspace) float64 { return settings.Get[float64](w.Settings, "editor.fontSize", "") }
	if size(w1) != 14 || size(w2) != 22 || size(w3) != 14 {
		t.Errorf("font sizes %v %v %v", size(w1), size(w2), size(w3))
	}
	if themeLabel(w1) != "Dark Modern" || themeLabel(w2) != "Light Modern" || themeLabel(w3) != "Dark Modern" {
		t.Errorf("themes %s %s %s", themeLabel(w1), themeLabel(w2), themeLabel(w3))
	}
	// the window shows one tab: each tab is shown in turn, and the active one comes back
	backgrounds := func() []string {
		var out []string
		active := a.m.ActiveTab()
		for _, tab := range a.m.Tabs() {
			a.m.Activate(tab)
			a.settle()
			for _, w := range a.ts.Widgets(gluttertest.ByType[*kit.Theme]()) {
				switch w.(*kit.Theme).Colors().EditorBackground {
				case darkBackground:
					out = append(out, "dark")
				case lightBackground:
					out = append(out, "light")
				default:
					out = append(out, "other")
				}
			}
		}
		a.m.Activate(active)
		a.settle()
		slices.Sort(out)
		return out
	}
	if got := backgrounds(); !slices.Equal(got, []string{"dark", "dark", "light"}) {
		t.Errorf("backgrounds %v", got)
	}
	if !slices.Equal(snippetNames(w1), []string{"d"}) || !slices.Equal(snippetNames(w2), []string{"w"}) {
		t.Errorf("snippets %v %v", snippetNames(w1), snippetNames(w2))
	}

	// a setting changed in one profile reaches the windows of that profile only
	if err := w1.Settings.Update(a.ctx, "editor.fontSize", 18, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if size(w1) != 18 || size(w3) != 18 || size(w2) != 22 {
		t.Errorf("after a change in the default profile: %v %v %v", size(w1), size(w2), size(w3))
	}
	if err := w2.Settings.Update(a.ctx, "editor.fontSize", 30, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if size(w1) != 18 || size(w3) != 18 || size(w2) != 30 {
		t.Errorf("after a change in Work: %v %v %v", size(w1), size(w2), size(w3))
	}
	if data, err := os.ReadFile(work); err != nil || !strings.Contains(string(data), "30") {
		t.Errorf("the file of Work: %q, %v", data, err)
	}
	if got := a.m.UsingProfile("Work"); len(got) != 1 || got[0] != w2 {
		t.Errorf("windows using Work: %v", got)
	}
	if got := a.m.UsingProfile(storage.DefaultProfile); len(got) != 2 {
		t.Errorf("windows using the default profile: %v", got)
	}
	// the open windows are noted with their profiles
	g, err := a.p.State.LoadGlobal(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var noted []string
	for _, w := range g.WorkspaceTabs {
		noted = append(noted, filepath.Base(w.Folder)+":"+w.Profile)
	}
	if !slices.Equal(noted, []string{"one:", "two:Work", "three:"}) {
		t.Errorf("open windows in the state: %v", noted)
	}
	// the window of Work leaves the profile: it is the default one now, and the windows follow their own file
	if err := w2.SwitchProfile(a.ctx, "Default"); err != nil {
		t.Fatal(err)
	}
	if size(w2) != 18 || themeLabel(w2) != "Dark Modern" {
		t.Errorf("after the switch: %v %s", size(w2), themeLabel(w2))
	}
	if got := a.m.UsingProfile("Work"); len(got) != 0 {
		t.Errorf("windows using Work: %v", got)
	}
}

// workProfile makes the Work profile of a multiEnv with the settings of the tests and gives the path of its
// settings.json.
func workProfile(t *testing.T, env *multiEnv) string {
	t.Helper()
	f := &profileFolders{t: t, paths: env.paths, clock: env.clock}
	if _, err := f.profiles().Create(context.Background(), "Work", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	f.write(storage.DefaultProfile, "snippets/plaintext.json", defaultSnip)
	f.write("Work", "keybindings.json", workKeys)
	f.write("Work", "snippets/plaintext.json", workSnippets)
	return f.write("Work", "settings.json", fmt.Sprintf(
		`{ "editor.fontFamily": %q, "editor.fontSize": 22, "editor.tabSize": 2, "workbench.colorTheme": "Light Modern", "files.hotExit": "off" }`, testutil.MonoFamily))
}

func TestTheCommandLineProfileOfTheFirstInstance(t *testing.T) {
	t.Run("a profile that exists", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		workProfile(t, env)
		one := env.folder("one", "a.go")
		a := env.run(Options{Folder: one, Open: &ipc.OpenArgs{Folder: one, Profile: "work"}})
		w := a.workspaceOf(one)
		if w.ProfileName() != "Work" {
			t.Fatalf("the profile of the window is %q", w.ProfileName())
		}
		if got := settings.Get[float64](w.Settings, "editor.fontSize", ""); got != 22 {
			t.Errorf("font size %v", got)
		}
		if got := w.Notify.Items(); len(got) != 0 {
			t.Errorf("notices: %v", got)
		}
		// the profile is the one of the folder from now on
		if p, ok, err := a.p.Profiles.LastProfile(a.ctx, one); err != nil || !ok || p != "Work" {
			t.Errorf("last profile %q %v %v", p, ok, err)
		}
	})
	t.Run("a profile that does not exist is made empty", func(t *testing.T) {
		env := newMultiEnv(t, map[string]any{"editor.tabSize": 7})
		one := env.folder("one", "a.go")
		a := env.run(Options{Folder: one, Open: &ipc.OpenArgs{Folder: one, Profile: "Fresh"}})
		w := a.workspaceOf(one)
		if w.ProfileName() != "Fresh" {
			t.Fatalf("the profile of the window is %q", w.ProfileName())
		}
		if !a.p.Profiles.Exists(a.ctx, "fresh") {
			t.Error("the profile was not made")
		}
		// it is empty: the settings are the defaults, not those of the default profile
		if got := tabSizeOf(w); got != 4 {
			t.Errorf("tab size %d, want the default 4 and not the 7 of the default profile", got)
		}
		if user, _ := w.Settings.Paths(); !strings.Contains(user, filepath.Join("profiles", "Fresh")) {
			t.Errorf("the settings file is %q", user)
		}
	})
	t.Run("no folder and no files: a window without a folder", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		workProfile(t, env)
		a := env.run(Options{Open: &ipc.OpenArgs{Profile: "Work"}})
		all := a.m.All()
		if len(all) != 1 || all[0].ProfileName() != "Work" {
			t.Errorf("windows: %v", all)
		}
	})
	t.Run("a profile that cannot be made leaves the default one and says so", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		root := filepath.Join(env.paths.ConfigHome, "emetos", "profiles")
		if err := os.WriteFile(root, nil, 0o644); err != nil { // a file where the folder of the profiles should be
			t.Fatal(err)
		}
		one := env.folder("one", "a.go")
		p := newTestProcess(ProcessOptions{Paths: env.paths})
		pw, err := p.Workspaces.prepare(context.Background(), WorkspaceSpec{Folder: one, Profile: "Work"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if pw.ws.ProfileName() != "Default" {
			t.Errorf("the profile of the window is %q", pw.ws.ProfileName())
		}
		items := pw.ws.Notify.Items()
		if len(items) != 1 || !strings.HasPrefix(items[0].Message, "Cannot use the profile 'Work': ") {
			t.Errorf("notices: %v", items)
		}
	})
}

func TestTheProfileOfALaunchInTheRunningInstance(t *testing.T) {
	setup := func(t *testing.T) (*multiApp, string, string) {
		env := newMultiEnv(t, nil)
		workProfile(t, env)
		one := env.folder("one", "a.go")
		two := env.folder("two", "b.go")
		return env.run(Options{Folder: one}), one, two
	}
	notices := func(w *Workspace) []string {
		var out []string
		for _, it := range w.Notify.Items() {
			out = append(out, it.Message)
		}
		return out
	}
	t.Run("a folder that is not open yet opens in a new window with the profile", func(t *testing.T) {
		a, one, two := setup(t)
		r := a.launched(ipc.OpenArgs{Folder: two, Profile: "Work"})
		wantOutcomes(t, r, LaunchOutcome{})
		if a.m.Count() != 2 {
			t.Fatalf("%d windows", a.m.Count())
		}
		if w := a.workspaceOf(two); w.ProfileName() != "Work" {
			t.Errorf("the new window uses %q", w.ProfileName())
		}
		if w := a.workspaceOf(one); w.ProfileName() != "Default" || len(notices(w)) != 0 {
			t.Errorf("the first window uses %q: %v", w.ProfileName(), notices(w))
		}
	})
	t.Run("a folder that is open with another profile comes to the front and says so", func(t *testing.T) {
		a, one, _ := setup(t)
		r := a.launched(ipc.OpenArgs{Folder: one, Profile: "Work"})
		wantOutcomes(t, r, LaunchOutcome{})
		if a.m.Count() != 1 {
			t.Fatalf("%d windows", a.m.Count())
		}
		w := a.workspaceOf(one)
		if w.ProfileName() != "Default" {
			t.Errorf("the window uses %q", w.ProfileName())
		}
		if got := notices(w); !slices.Equal(got, []string{"The folder is already open with the profile 'Default'."}) {
			t.Errorf("notices %q", got)
		}
	})
	t.Run("a folder that is open with the profile is nothing to say", func(t *testing.T) {
		a, one, _ := setup(t)
		r := a.launched(ipc.OpenArgs{Folder: one, Profile: "default"})
		wantOutcomes(t, r, LaunchOutcome{})
		if a.m.Count() != 1 || len(notices(a.workspaceOf(one))) != 0 {
			t.Errorf("%d windows, notices %q", a.m.Count(), notices(a.workspaceOf(one)))
		}
	})
	t.Run("files open in a new window when the window has another profile", func(t *testing.T) {
		a, one, _ := setup(t)
		r := a.launched(ipc.OpenArgs{Files: []ipc.FileTarget{{Path: filepath.Join(one, "a.go")}}, Profile: "Work"})
		wantOutcomes(t, r, LaunchOutcome{})
		if a.m.Count() != 2 {
			t.Fatalf("%d windows", a.m.Count())
		}
		w := a.m.All()[1]
		if w.ProfileName() != "Work" || w.Folder != "" {
			t.Errorf("the new window uses %q over %q", w.ProfileName(), w.Folder)
		}
		a.waitFor("the file", func() bool { return w.shell != nil && w.shell.editorCount() == 1 })
	})
	t.Run("files open in the window when it has the profile", func(t *testing.T) {
		a, one, _ := setup(t)
		r := a.launched(ipc.OpenArgs{Files: []ipc.FileTarget{{Path: filepath.Join(one, "a.go")}}, Profile: "Default"})
		wantOutcomes(t, r, LaunchOutcome{})
		if a.m.Count() != 1 {
			t.Errorf("%d windows", a.m.Count())
		}
	})
	t.Run("--reuse-window keeps the window and says which profile it has", func(t *testing.T) {
		a, one, _ := setup(t)
		r := a.launched(ipc.OpenArgs{Files: []ipc.FileTarget{{Path: filepath.Join(one, "a.go")}}, Profile: "Work", ReuseWindow: true})
		wantOutcomes(t, r, LaunchOutcome{})
		if a.m.Count() != 1 {
			t.Fatalf("%d windows", a.m.Count())
		}
		if got := notices(a.workspaceOf(one)); !slices.Equal(got, []string{"The window uses the profile 'Default'."}) {
			t.Errorf("notices %q", got)
		}
	})
	t.Run("only a profile opens a window with it, once", func(t *testing.T) {
		a, _, _ := setup(t)
		wantOutcomes(t, a.launched(ipc.OpenArgs{Profile: "Work"}), LaunchOutcome{})
		if a.m.Count() != 2 {
			t.Fatalf("%d windows", a.m.Count())
		}
		wantOutcomes(t, a.launched(ipc.OpenArgs{Profile: "Work"}), LaunchOutcome{})
		if a.m.Count() != 2 {
			t.Errorf("%d windows after the second launch", a.m.Count())
		}
		if got := a.m.Active(); got == nil || got.ProfileName() != "Work" {
			t.Errorf("the active window uses %v", got)
		}
	})
	t.Run("a profile that does not exist is made", func(t *testing.T) {
		a, _, two := setup(t)
		wantOutcomes(t, a.launched(ipc.OpenArgs{Folder: two, Profile: "Brand New"}), LaunchOutcome{})
		if w := a.workspaceOf(two); w.ProfileName() != "Brand New" {
			t.Errorf("the window uses %q", w.ProfileName())
		}
		if !a.p.Profiles.Exists(a.ctx, "brand new") {
			t.Error("the profile was not made")
		}
	})
	t.Run("an empty window is replaced by the folder with the profile", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		workProfile(t, env)
		two := env.folder("two", "b.go")
		a := env.run(Options{})
		wantOutcomes(t, a.launched(ipc.OpenArgs{Folder: two, Profile: "Work"}), LaunchOutcome{})
		if a.m.Count() != 1 {
			t.Fatalf("%d windows", a.m.Count())
		}
		if w := a.workspaceOf(two); w.ProfileName() != "Work" {
			t.Errorf("the window uses %q", w.ProfileName())
		}
	})
}

func TestTheProfileLastUsedWithAFolder(t *testing.T) {
	env := newMultiEnv(t, nil)
	workProfile(t, env)
	one := env.folder("one", "a.go")
	a := env.run(Options{Folder: one})
	w := a.workspaceOf(one)
	ctx := a.ctx
	last := func() (string, bool) {
		p, ok, err := a.p.Profiles.LastProfile(ctx, one)
		if err != nil {
			t.Fatal(err)
		}
		return p, ok
	}
	if _, ok := last(); ok {
		t.Error("a folder opened with the default profile has a record")
	}
	if err := w.SwitchProfile(ctx, "Work"); err != nil {
		t.Fatal(err)
	}
	if p, ok := last(); !ok || p != "Work" {
		t.Errorf("after the switch: %q %v", p, ok)
	}
	// the record is written with the state
	env.clock.Advance(storage.DefaultStateDelay)
	again := newTestProcess(ProcessOptions{Paths: env.paths})
	if p, ok, err := again.Profiles.LastProfile(ctx, one); err != nil || !ok || p != "Work" {
		t.Errorf("another process reads %q %v %v", p, ok, err)
	}
	// a window for the folder that the command line does not give a profile opens with it
	if got, problem := again.Workspaces.workspaceProfile(ctx, WorkspaceSpec{Folder: one}); problem != nil || got != "Work" {
		t.Errorf("the profile of a window for the folder is %q, %v", got, problem)
	}
	plan, _ := again.startupPlan(ctx, Options{Folder: env.folder("elsewhere")})
	if got, _ := again.Workspaces.workspaceProfile(ctx, plan[0]); got != storage.DefaultProfile {
		t.Errorf("a folder with no record opens with %q", got)
	}
	// an explicit profile wins, and becomes the record
	if got, _ := again.Workspaces.workspaceProfile(ctx, WorkspaceSpec{Folder: one, Profile: "Default"}); got != storage.DefaultProfile {
		t.Errorf("--profile Default opens with %q", got)
	}
	// the default profile again leaves no record
	if err := w.SwitchProfile(ctx, "default"); err != nil {
		t.Fatal(err)
	}
	if _, ok := last(); ok {
		t.Error("the default profile left a record")
	}
	// a profile that is deleted is not the last one
	if err := w.SwitchProfile(ctx, "Work"); err != nil {
		t.Fatal(err)
	}
	if err := w.SwitchProfile(ctx, "Default"); err != nil {
		t.Fatal(err)
	}
	if err := a.p.Profiles.SetLastProfile(ctx, one, "Work"); err != nil {
		t.Fatal(err)
	}
	if err := a.p.Profiles.Delete(ctx, "Work"); err != nil {
		t.Fatal(err)
	}
	if _, ok := last(); ok {
		t.Error("a deleted profile is still the last one")
	}
}

func TestTheFolderOfAWindowOpensWithTheProfileOfTheFolder(t *testing.T) {
	env := newMultiEnv(t, nil)
	workProfile(t, env)
	one := env.folder("one", "a.go")
	two := env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if err := a.p.Profiles.SetLastProfile(a.ctx, two, "Work"); err != nil {
		t.Fatal(err)
	}
	w1 := a.workspaceOf(one)
	// Open Folder replaces the window: the new window has the profile of the folder
	done := make(chan *Workspace, 1)
	a.m.OpenFolder(w1, two, FolderFromApp, false, false, func(w *Workspace) { done <- w })
	a.settle()
	var w2 *Workspace
	select {
	case w2 = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the folder did not open")
	}
	if w2 == nil || w2.Folder != two || w2.ProfileName() != "Work" {
		t.Fatalf("the window: %+v", w2)
	}
	// Close Folder leaves the profile of the window as it is
	done = make(chan *Workspace, 1)
	a.m.OpenFolder(w2, "", FolderFromApp, false, false, func(w *Workspace) { done <- w })
	a.settle()
	select {
	case w3 := <-done:
		if w3 == nil || w3.Folder != "" || w3.ProfileName() != "Work" {
			t.Errorf("after Close Folder: %+v", w3)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the folder did not close")
	}
}

func TestTheWindowsComeBackWithTheirProfiles(t *testing.T) {
	env := newMultiEnv(t, map[string]any{"window.restoreWindows": "all"})
	workProfile(t, env)
	one, two := env.folder("one", "a.go"), env.folder("two", "b.go")
	a := env.run(Options{Folder: one})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two, Profile: "Work"}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w2 := a.workspaceOf(two)
	if err := a.command(w2, "dev.openFile", map[string]any{"path": filepath.Join(two, "b.go")}); err != nil {
		t.Fatal(err)
	}
	w2.shell.flushState()
	if err := a.p.flushStates(a.ctx); err != nil {
		t.Fatal(err)
	}
	// the state of the window of Work is in the state folder of Work
	id := w2.StateID()
	path, err := env.paths.WindowStatePath("Work", id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the state of the window of Work: %v", err)
	}
	if def, _ := env.paths.WindowStatePath(storage.DefaultProfile, id); fileExists(def) {
		t.Errorf("the state of the window of Work is in the folder of the default profile too")
	}

	b := newTestProcess(ProcessOptions{Paths: env.paths, StateAfter: env.clock.AfterFunc})
	plan, _ := b.startupPlan(a.ctx, Options{})
	var got []string
	for _, spec := range plan {
		got = append(got, filepath.Base(spec.Folder)+":"+spec.Profile)
	}
	if !slices.Equal(got, []string{"one:Default", "two:Work"}) {
		t.Errorf("the plan: %v", got)
	}
	// the windows of the second run use their profiles and have their editors back
	c := env.run(Options{})
	if c.m.Count() != 2 {
		t.Fatalf("%d windows", c.m.Count())
	}
	c.waitFor("the second window", func() bool { return c.m.workspaceForFolder(two) != nil })
	r2 := c.workspaceOf(two)
	if r2.ProfileName() != "Work" || c.workspaceOf(one).ProfileName() != "Default" {
		t.Errorf("profiles %s %s", c.workspaceOf(one).ProfileName(), r2.ProfileName())
	}
	c.waitFor("the editor of b.go", func() bool { return r2.shell != nil && r2.shell.editorCount() == 1 })

	// a profile that is gone comes back as the default one
	if err := b.Profiles.Delete(a.ctx, "Work"); err != nil {
		t.Fatal(err)
	}
	plan, _ = b.startupPlan(a.ctx, Options{})
	got = nil
	for _, spec := range plan {
		got = append(got, filepath.Base(spec.Folder)+":"+spec.Profile)
	}
	if !slices.Equal(got, []string{"one:Default", "two:Default"}) {
		t.Errorf("the plan after the profile was deleted: %v", got)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestTheStartPlanGivesTheCommandLineProfileToTheRightWindow(t *testing.T) {
	env := newMultiEnv(t, map[string]any{"window.restoreWindows": "preserve"})
	workProfile(t, env)
	one, two := env.folder("one", "a.go"), env.folder("two", "b.go")
	p := newTestProcess(ProcessOptions{Paths: env.paths, StateAfter: env.clock.AfterFunc})
	ctx := t.Context()
	if err := p.State.UpdateGlobal(ctx, func(g *storage.GlobalState) bool {
		g.WorkspaceTabs = []storage.WorkspaceTab{{ID: "1111111111111111", Folder: one}, {ID: "2222222222222222", Folder: two, Profile: "Work"}}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	describe := func(plan []WorkspaceSpec) []string {
		var out []string
		for _, s := range plan {
			folder := ""
			if s.Folder != "" {
				folder = filepath.Base(s.Folder)
			}
			out = append(out, fmt.Sprintf("%s:%s:launch=%v:files=%d", folder, s.Profile, s.Launch != nil, len(s.Files)))
		}
		return out
	}
	three := env.folder("three", "c.go")
	tests := []struct {
		name string
		open ipc.OpenArgs
		want []string
	}{
		{"a new folder gets the profile", ipc.OpenArgs{Folder: three, Profile: "Work"},
			[]string{"one:Default:launch=false:files=0", "two:Work:launch=false:files=0", "three:Work:launch=true:files=0"}},
		{"a folder that was open keeps its profile (the launch says so)", ipc.OpenArgs{Folder: one, Profile: "Work"},
			[]string{"one:Default:launch=true:files=0", "two:Work:launch=false:files=0"}},
		{"a folder that was open with the profile", ipc.OpenArgs{Folder: two, Profile: "work"},
			[]string{"one:Default:launch=false:files=0", "two:Work:launch=true:files=0"}},
		{"only a profile: a new window", ipc.OpenArgs{Profile: "Work"},
			[]string{"one:Default:launch=false:files=0", "two:Work:launch=false:files=0", ":Work:launch=true:files=0"}},
		{"no profile: the plan is as before", ipc.OpenArgs{Folder: three},
			[]string{"one:Default:launch=false:files=0", "two:Work:launch=false:files=0", "three::launch=true:files=0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			open := tt.open
			plan, _ := p.startupPlan(ctx, Options{Folder: open.Folder, Open: &open})
			if got := describe(plan); !slices.Equal(got, tt.want) {
				t.Errorf("the plan\n got %v\nwant %v", got, tt.want)
			}
		})
	}
}

func TestSwitchProfileFailures(t *testing.T) {
	a := newProfileApp(t, ProcessOptions{}, "")
	ctx := context.Background()
	tests := []struct {
		name, profile, route string
	}{
		{"a profile that does not exist", "Nope", "Workspace.SwitchProfile.ProfileSwitchNotFound"},
		{"an invalid name", "a/b", "Workspace.SwitchProfile.ProfileSwitchNotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := a.win.SwitchProfile(ctx, tt.profile)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if a.win.Profile() != storage.DefaultProfile {
				t.Errorf("the profile changed to %q", a.win.Profile())
			}
		})
	}
	t.Run("the profiles cannot be listed", func(t *testing.T) {
		b := newProfileApp(t, ProcessOptions{}, "")
		root := filepath.Join(b.f.paths.ConfigHome, "emetos", "profiles")
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		err := b.win.SwitchProfile(ctx, "Work")
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("Workspace.SwitchProfile->Profiles.Lookup->Profiles.List.ProfileListFailed") {
			t.Errorf("SwitchProfile = %v", err)
		}
	})
	t.Run("a settings file that cannot be read is reported after the switch", func(t *testing.T) {
		b := newProfileApp(t, ProcessOptions{}, "")
		path, err := settings.UserSettingsPath(b.f.paths, "Work")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o755); err != nil { // a folder where the file should be
			t.Fatal(err)
		}
		err = b.win.SwitchProfile(ctx, "Work")
		if err == nil {
			t.Fatal("expected the error of the file")
		}
		if b.win.Profile() != "Work" {
			t.Errorf("the switch did not complete: the profile is %q", b.win.Profile())
		}
		if user, _ := b.win.Settings.Paths(); user != path {
			t.Errorf("the settings file is %q", user)
		}
	})
}

func TestTheDevCommandSwitchesTheProfile(t *testing.T) {
	a := newProfileApp(t, ProcessOptions{}, "")
	run := func(args any) error {
		return a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, "dev.switchProfile", args)
	}
	if err := run(map[string]any{"name": "Work"}); err != nil {
		t.Fatal(err)
	}
	if a.win.ProfileName() != "Work" {
		t.Errorf("the profile is %q", a.win.ProfileName())
	}
	if err := run("Default"); err != nil {
		t.Fatal(err)
	}
	if a.win.ProfileName() != "Default" {
		t.Errorf("the profile is %q", a.win.ProfileName())
	}
	tests := []struct {
		name  string
		args  any
		route string
	}{
		{"no argument", nil, "DevSwitchProfileInvalid"},
		{"an empty name", map[string]any{"name": ""}, "DevSwitchProfileInvalid"},
		{"a name that is not a text", map[string]any{"name": 3.0}, "DevSwitchProfileInvalid"},
		{"a profile that is not there", "Nope", "ProfileSwitchNotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.args)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Errorf("run = %v", err)
			}
		})
	}
}

// userKeyLabels are the keys of a command in the keybindings.json of the window. They are read under the lock of the
// keybindings, not from the resolver, which the goroutine of the watcher replaces when the window has no shell.
func userKeyLabels(w *Workspace, command string) []string {
	k := w.userKeys
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for _, it := range k.items {
		if it.Command == command && it.Keybinding != nil {
			out = append(out, it.Keybinding.Label())
		}
	}
	return out
}

// watchedProfileWindow is a window with a watcher over a profile.
func watchedProfileWindow(t *testing.T, f *profileFolders, profile string) *Workspace {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	win := f.window(f.process(ProcessOptions{WatchFiles: true}), "", profile)
	ready := win.StartWatching(ctx)
	t.Cleanup(func() {
		cancel()
		if w := win.Watcher(); w != nil {
			select {
			case <-w.Done():
			case <-time.After(5 * time.Second):
				t.Error("the watcher did not stop")
			}
		}
	})
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher was not ready")
	}
	return win
}

func TestTheWatcherFollowsTheProfile(t *testing.T) {
	f := newProfileFolders(t)
	win := watchedProfileWindow(t, f, "")
	ctx := context.Background()
	defaultSettings, _ := win.Settings.Paths()
	workSettings, err := settings.UserSettingsPath(f.paths, "Work")
	if err != nil {
		t.Fatal(err)
	}
	tab := func() int { return tabSizeOf(win) }

	// before the switch the file of the default profile counts
	f.write(storage.DefaultProfile, "settings.json", `{ "editor.tabSize": 5 }`)
	eventually(t, "the tab size of the default profile", func() bool { return tab() == 5 })

	if err := win.SwitchProfile(ctx, "Work"); err != nil {
		t.Fatal(err)
	}
	if tab() != 2 {
		t.Fatalf("tab size %d after the switch", tab())
	}
	// the new profile's files are followed
	f.write("Work", "settings.json", `{ "editor.tabSize": 7 }`)
	eventually(t, "the tab size of Work", func() bool { return tab() == 7 })
	f.write("Work", "keybindings.json", `[{ "key": "ctrl+alt+8", "command": "workbench.action.openSettings" }]`)
	eventually(t, "the keys of Work", func() bool {
		return slices.Equal(userKeyLabels(win, "workbench.action.openSettings"), []string{"Ctrl+Alt+8"})
	})
	f.write("Work", "snippets/plaintext.json", `{ "n": { "prefix": "nn", "body": "N" } }`)
	eventually(t, "the snippets of Work", func() bool { return slices.Equal(snippetNames(win), []string{"n"}) })

	// the old profile's files are not: an edit of them does not reach the window
	f.write(storage.DefaultProfile, "settings.json", `{ "editor.tabSize": 9 }`)
	f.write(storage.DefaultProfile, "keybindings.json", `[{ "key": "ctrl+alt+7", "command": "workbench.action.openSettings" }]`)
	f.write(storage.DefaultProfile, "snippets/plaintext.json", `{ "o": { "prefix": "oo", "body": "O" } }`)
	// a change of the new profile that comes after them proves the events of the old ones were handled
	f.write("Work", "settings.json", `{ "editor.tabSize": 8 }`)
	eventually(t, "the tab size of Work again", func() bool { return tab() == 8 })
	if got := userKeyLabels(win, "workbench.action.openSettings"); !slices.Equal(got, []string{"Ctrl+Alt+8"}) {
		t.Errorf("the keys of Settings are %q", got)
	}
	if got := snippetNames(win); !slices.Equal(got, []string{"n"}) {
		t.Errorf("snippets %v", got)
	}
	if user, _ := win.Settings.Paths(); user != workSettings || user == defaultSettings {
		t.Errorf("the settings file is %q", user)
	}

	// and back
	if err := win.SwitchProfile(ctx, "Default"); err != nil {
		t.Fatal(err)
	}
	if tab() != 9 {
		t.Errorf("tab size %d after the switch back", tab())
	}
	f.write(storage.DefaultProfile, "settings.json", `{ "editor.tabSize": 3 }`)
	eventually(t, "the tab size of the default profile again", func() bool { return tab() == 3 })
	f.write("Work", "settings.json", `{ "editor.tabSize": 11 }`)
	f.write(storage.DefaultProfile, "settings.json", `{ "editor.tabSize": 12 }`)
	eventually(t, "the last edit", func() bool { return tab() == 12 })
}

func TestTheProfileOfTheWindowIsKeptWhenItsFolderChanges(t *testing.T) {
	f := newProfileFolders(t)
	p := f.process(ProcessOptions{})
	one, two := t.TempDir(), t.TempDir()
	win := f.window(p, one, "Work")
	if got := win.ProfileName(); got != "Work" {
		t.Fatalf("profile %q", got)
	}
	if err := win.SetFolder(context.Background(), two); err != nil {
		t.Fatal(err)
	}
	if got := win.ProfileName(); got != "Work" {
		t.Errorf("profile %q after the folder changed", got)
	}
	// the new folder is used with the profile from now on
	if got, ok, err := p.Profiles.LastProfile(context.Background(), two); err != nil || !ok || got != "Work" {
		t.Errorf("last profile of the new folder: %q %v %v", got, ok, err)
	}
	if got, ok, err := p.Profiles.LastProfile(context.Background(), one); err != nil || !ok || got != "Work" {
		t.Errorf("last profile of the old folder: %q %v %v", got, ok, err)
	}
}

func TestTheWorkspaceSettingsStayWhenTheProfileChanges(t *testing.T) {
	f := newProfileFolders(t)
	p := f.process(ProcessOptions{})
	folder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, ".emetos", "settings.json"), []byte(`{ "editor.tabSize": 3 }`), 0o644); err != nil {
		t.Fatal(err)
	}
	win := f.window(p, folder, "")
	if tabSizeOf(win) != 3 {
		t.Fatalf("tab size %d", tabSizeOf(win))
	}
	if err := win.SwitchProfile(context.Background(), "Work"); err != nil {
		t.Fatal(err)
	}
	if tabSizeOf(win) != 3 {
		t.Errorf("the workspace layer was lost: tab size %d", tabSizeOf(win))
	}
	// the user layer is the new profile's
	if got := settings.Get[float64](win.Settings, "editor.fontSize", ""); got != 22 {
		t.Errorf("font size %v", got)
	}
}
