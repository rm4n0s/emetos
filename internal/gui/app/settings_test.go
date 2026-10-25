package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/settingsui"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The tests of the settings editor in the window (step 8.1): the editor input and its commands, the edits it makes
// in settings.json, and the rule of a settings file that is open in a text editor.

// newSettingsApp is a window over a folder (none when empty) with a.txt open, whose text is in the test font.
func newSettingsApp(t *testing.T, folder string) *groupApp {
	t.Helper()
	win := newTestWorkspace(t, folder)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &groupApp{t: t, win: win, files: map[string]string{"a.txt": path}, docs: map[string]*document.Document{}}
	doc := a.doc("a.txt")
	a.ts = testutil.New(t, testWindowSize)
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a.ts.PumpWidget(newAppDocs(context.Background(), win, []*document.Document{doc}, nil, ""))
	a.pump()
	return a
}

func (a *groupApp) userSettings() string {
	a.t.Helper()
	user, _ := a.win.Settings.Paths()
	return user
}

// settingsInputs are the settings inputs of all the groups.
func (a *groupApp) settingsInputs() []*editorgroup.SettingsInput {
	var out []*editorgroup.SettingsInput
	for _, g := range a.shell().group.grid.Groups(editorgroup.GridAppearance) {
		for _, e := range g.Model.Editors(editorgroup.Sequential, false) {
			if in, ok := e.(*editorgroup.SettingsInput); ok {
				out = append(out, in)
			}
		}
	}
	return out
}

func (a *groupApp) settingRow(category, key string) gluttertest.Finder {
	return gluttertest.ByKey(settingsui.RowKey(category, key))
}

// searchSettings filters the settings editor by a text.
func (a *groupApp) searchSettings(query string) {
	a.t.Helper()
	a.run2("workbench.action.openSettings", map[string]any{"query": query})
}

// run2 is run with arguments.
func (a *groupApp) run2(id string, args any) {
	a.t.Helper()
	err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, id, args)
	if err != nil {
		a.t.Fatalf("%s: %v", id, err)
	}
	a.pump()
}

func TestCtrlCommaOpensTheSettingsEditor(t *testing.T) {
	a := newSettingsApp(t, "")
	a.press("ctrl+,")
	wantNames(t, a.tabNames(), "a.txt", "Settings")
	if got := a.active(); got != "Settings" {
		t.Errorf("active tab %q", got)
	}
	if got := len(a.settingsInputs()); got != 1 {
		t.Fatalf("%d settings inputs", got)
	}
	if !a.ts.Exists(gluttertest.ByType[*settingsui.Editor]()) {
		t.Error("the editor is not shown")
	}
	item := a.item("Settings")
	if item.Dirty || item.Preview || item.Icon == nil {
		t.Errorf("tab %+v: dirty, preview or without the icon", item)
	}
	if v := a.win.Keys.Get("activeEditor"); v != editorgroup.SettingsInputTypeID {
		t.Errorf("activeEditor %v", v)
	}
	// the search box has the focus
	if !a.shell().group.panes[a.shell().activeGroup().Model.ActiveEditor()].hasFocus() {
		t.Error("the settings editor has not the focus")
	}
	// the command again reveals it
	a.clickTab("a.txt")
	if got := a.active(); got != "a.txt" {
		t.Fatalf("active tab %q", got)
	}
	a.press("ctrl+,")
	wantNames(t, a.tabNames(), "a.txt", "Settings")
	if got := a.active(); got != "Settings" {
		t.Errorf("active tab %q after the second Ctrl+,", got)
	}
	// closing it asks nothing
	a.run("workbench.action.closeActiveEditor")
	wantNames(t, a.tabNames(), "a.txt")
	if len(a.settingsInputs()) != 0 || len(a.shell().group.panes) != 0 {
		t.Error("the input or its pane is left")
	}
	a.noErrors()
}

func TestTheSettingsCommandTakesTheSearchText(t *testing.T) {
	a := newSettingsApp(t, "")
	a.run2("workbench.action.openSettings", "tab size")
	if got := a.settingsInputs()[0].ViewState().Query; got != "tab size" {
		t.Errorf("query %q", got)
	}
	if !a.ts.Exists(a.settingRow("editor", "editor.tabSize")) || a.ts.Exists(a.settingRow("editor", "editor.insertSpaces")) {
		t.Error("the list is not filtered")
	}
	a.run2("workbench.action.openSettings", map[string]any{"query": "font family"})
	if got := len(a.settingsInputs()); got != 1 {
		t.Errorf("%d inputs", got)
	}
	if !a.ts.Exists(a.settingRow("editor/font", "editor.fontFamily")) {
		t.Error("the second search is not shown")
	}
	// the palette command and Open Settings (UI) are the same
	a.run("workbench.action.openSettings2")
	if got := len(a.settingsInputs()); got != 1 {
		t.Errorf("%d inputs after openSettings2", got)
	}
	a.noErrors()
}

func TestWorkspaceSettingsNeedAFolder(t *testing.T) {
	dir := t.TempDir()
	a := newSettingsApp(t, dir)
	a.run("workbench.action.openWorkspaceSettings")
	in := a.settingsInputs()
	if len(in) != 1 || in[0].ViewState().Target != editorgroup.SettingsWorkspace {
		t.Fatalf("inputs %v", in)
	}
	// a window without a folder shows the user's settings
	b := newSettingsApp(t, "")
	b.run("workbench.action.openWorkspaceSettings")
	if got := b.settingsInputs()[0].ViewState().Target; got == editorgroup.SettingsWorkspace {
		t.Errorf("target %q in a window without a folder", got)
	}
}

func TestTheSettingsEditorWritesTheUserFile(t *testing.T) {
	a := newSettingsApp(t, "")
	a.searchSettings("editor.insertSpaces")
	a.ts.Tap(gluttertest.Descendant(a.settingRow("editor", "editor.insertSpaces"), gluttertest.ByType[*kit.Checkbox]()))
	a.pump()
	user := a.userSettings()
	if got := readFile(t, user); !strings.Contains(got, `"editor.insertSpaces": false`) {
		t.Errorf("file %q", got)
	}
	if settings.Get[bool](a.win.Settings, "editor.insertSpaces", "") {
		t.Error("the setting did not change")
	}
	a.noErrors()
}

// settingsDoc opens the settings file of the user in an editor.
func (a *groupApp) settingsDoc() *document.Document {
	a.t.Helper()
	doc, err := a.win.Documents.Acquire(context.Background(), a.userSettings(), document.NewOptions())
	if err != nil {
		a.t.Fatal(err)
	}
	s := a.shell()
	s.show(widgets.WidgetsBindingOf(s.Context()), doc, openOptions{})
	a.pump()
	return doc
}

// toggle flips the check box of a setting in the settings editor.
func (a *groupApp) toggle(key string) {
	a.t.Helper()
	a.searchSettings(key)
	a.ts.Tap(gluttertest.Descendant(a.settingRow("editor", key), gluttertest.ByType[*kit.Checkbox]()))
	a.pump()
}

func TestADirtySettingsDocumentGetsTheChange(t *testing.T) {
	a := newSettingsApp(t, "")
	doc := a.settingsDoc()
	if _, err := doc.Edit([]document.Edit{{Text: "// typed, not saved\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if !doc.Dirty() {
		t.Fatal("the document is not dirty")
	}
	user := a.userSettings()
	onDisk := readFile(t, user)
	a.toggle("editor.insertSpaces")
	if got := readFile(t, user); got != onDisk {
		t.Errorf("the file was written: %q", got)
	}
	text := doc.Text()
	if !strings.HasPrefix(text, "// typed, not saved\n") || !strings.Contains(text, `"editor.insertSpaces": false`) {
		t.Errorf("document %q", text)
	}
	if !doc.Dirty() {
		t.Error("the document is not dirty after the change")
	}
	if settings.Get[bool](a.win.Settings, "editor.insertSpaces", "") {
		t.Error("the setting does not follow the document")
	}
	// the tab of the document shows the dirty mark
	if !a.item("settings.json").Dirty {
		t.Error("the tab is not dirty")
	}
	// the change is one undo step of the document
	if _, err := doc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := doc.Text(); strings.Contains(got, "editor.insertSpaces") || !strings.HasPrefix(got, "// typed") {
		t.Errorf("after an undo %q", got)
	}
	a.noErrors()
}

func TestACleanSettingsDocumentGetsTheChangeAndIsSaved(t *testing.T) {
	a := newSettingsApp(t, "")
	doc := a.settingsDoc()
	if doc.Dirty() {
		t.Fatal("the document is dirty")
	}
	user := a.userSettings()
	a.toggle("editor.insertSpaces")
	a.waitFor("the file to be written", func() bool { return strings.Contains(readFile(t, user), `"editor.insertSpaces": false`) })
	a.waitFor("the document to be saved", func() bool { return !doc.Dirty() })
	if got, want := doc.Text(), readFile(t, user); got != want {
		t.Errorf("document %q, file %q", got, want)
	}
	if settings.Get[bool](a.win.Settings, "editor.insertSpaces", "") {
		t.Error("the setting did not change")
	}
	a.noErrors()
}

func TestAClosedSettingsDocumentIsNotUsed(t *testing.T) {
	a := newSettingsApp(t, "")
	a.settingsDoc()
	a.run("workbench.action.closeActiveEditor")
	if _, ok := a.win.settingsDocs.OpenText(a.userSettings()); ok {
		t.Fatal("the closed document is still used")
	}
	a.toggle("editor.insertSpaces")
	if got := readFile(t, a.userSettings()); !strings.Contains(got, `"editor.insertSpaces": false`) {
		t.Errorf("file %q", got)
	}
	a.noErrors()
}

func TestOtherChangesGoThroughTheOpenDocumentToo(t *testing.T) {
	a := newSettingsApp(t, "")
	doc := a.settingsDoc()
	if _, err := doc.Edit([]document.Edit{{Text: "// typed\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	onDisk := readFile(t, a.userSettings())
	// a setting that other code writes (the language picker, the zoom) goes the same way
	if err := a.win.Settings.Update(context.Background(), "editor.wordWrap", "on", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if got := readFile(t, a.userSettings()); got != onDisk {
		t.Errorf("the file was written: %q", got)
	}
	if !strings.Contains(doc.Text(), `"editor.wordWrap": "on"`) {
		t.Errorf("document %q", doc.Text())
	}
	a.noErrors()
}

func TestRevertingTheSettingsDocumentRestoresTheSettings(t *testing.T) {
	a := newSettingsApp(t, "")
	doc := a.settingsDoc()
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "// x\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	a.toggle("editor.insertSpaces")
	if settings.Get[bool](a.win.Settings, "editor.insertSpaces", "") {
		t.Fatal("the setting did not change")
	}
	a.clickTab("settings.json")
	a.run("workbench.action.files.revert")
	a.waitFor("the revert", func() bool { return !doc.Dirty() })
	a.waitFor("the settings to follow", func() bool { return settings.Get[bool](a.win.Settings, "editor.insertSpaces", "") })
	a.noErrors()
}

// activeDocName is the name of the active tab and the line of its cursor, counted from 1.
func (a *groupApp) cursorLine() int {
	a.t.Helper()
	return a.vm().Cursors().Primary().Position().Line + 1
}

func TestEditInSettingsJSONOpensTheFileAtTheKey(t *testing.T) {
	a := newSettingsApp(t, "")
	user := a.userSettings()
	text := "{\n  \"editor.fontFamily\": \"x\",\n  // the associations\n  \"files.associations\": {\"*.x\": \"go\"}\n}\n"
	if err := os.WriteFile(user, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.win.Settings.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.searchSettings("files.associations")
	a.ts.Tap(gluttertest.Descendant(a.settingRow("editor/files", "files.associations"), gluttertest.ByText("Edit in settings.json")))
	a.waitFor("settings.json to open", func() bool { return a.active() == "settings.json" })
	if got := a.cursorLine(); got != 4 {
		t.Errorf("the cursor is on line %d, want 4", got)
	}
	// the header's button opens the file too, without a place
	a.noErrors()
}

func TestSettingsInTheManageAndTheFileMenus(t *testing.T) {
	t.Run("the Manage menu", func(t *testing.T) {
		a := newSettingsApp(t, "")
		a.run2("dev.openMenu", map[string]any{"id": "GlobalActivity"})
		a.ts.Tap(gluttertest.ByText("Settings"))
		a.pump()
		wantNames(t, a.tabNames(), "a.txt", "Settings")
		a.noErrors()
	})
	t.Run("the Preferences submenu of the File menu", func(t *testing.T) {
		a := newSettingsApp(t, "")
		a.run2("dev.openMenu", map[string]any{"id": "MenubarFileMenu"})
		if !a.ts.Exists(gluttertest.ByText("Preferences")) {
			t.Fatal("the File menu has no Preferences row")
		}
		a.ts.Tap(gluttertest.ByText("Preferences"))
		a.pump()
		a.ts.Tap(gluttertest.ByText("Settings"))
		a.pump()
		wantNames(t, a.tabNames(), "a.txt", "Settings")
		a.noErrors()
	})
}

func TestTheSettingsEditorIsOnlyInOneGroup(t *testing.T) {
	a := newSettingsApp(t, "")
	a.run("workbench.action.openSettings")
	a.run("workbench.action.splitEditorRight")
	if got := len(a.settingsInputs()); got != 1 {
		t.Errorf("%d settings inputs after a split", got)
	}
	a.run("workbench.action.openSettings")
	if got := len(a.settingsInputs()); got != 1 {
		t.Errorf("%d settings inputs after opening again", got)
	}
	a.noErrors()
}

func TestAnEditorCommandWorksAroundTheSettingsEditor(t *testing.T) {
	a := newSettingsApp(t, "")
	a.run("workbench.action.openSettings")
	// commands of the text editor have no editor to act on, and say nothing
	for _, id := range []string{"workbench.action.files.save", "workbench.action.files.saveAll", "workbench.action.files.revert",
		"workbench.action.closeOtherEditors", "workbench.action.files.saveWithoutFormatting", "copyFilePath"} {
		a.run(id)
	}
	if a.win.Keys.Get("activeEditorIsDirty") != false {
		t.Error("the settings editor is dirty")
	}
	a.noErrors()
}

func TestTheSettingsEditorComesBackAfterARestart(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.command("workbench.action.openSettings", map[string]any{"query": "tab size"})
	first.command("workbench.action.openWorkspaceSettings", nil)
	input := func(r *stateRun) *editorgroup.SettingsInput {
		for _, in := range r.shell().activeGroup().Model.Editors(editorgroup.Sequential, false) {
			if si, ok := in.(*editorgroup.SettingsInput); ok {
				return si
			}
		}
		return nil
	}
	if in := input(first); in == nil || in.ViewState().Target != editorgroup.SettingsWorkspace || in.ViewState().Query != "tab size" {
		t.Fatalf("view state %+v", in)
	}
	wantLayout := first.layout()
	first.close()

	second := env.start(env.root)
	in := input(second)
	if in == nil {
		t.Fatalf("the settings editor did not come back: %v", second.layout())
	}
	if got := in.ViewState(); got.Target != editorgroup.SettingsWorkspace || got.Query != "tab size" {
		t.Errorf("view state %+v", got)
	}
	if got := second.layout(); strings.Join(got, "|") != strings.Join(wantLayout, "|") {
		t.Errorf("layout %v, want %v", got, wantLayout)
	}
	if !second.ts.Exists(gluttertest.ByType[*settingsui.Editor]()) {
		t.Error("the editor is not shown")
	}
	// the pane has the state of the input
	if got := second.shell().group.panes[in].(*settingsPane).ctl.ViewState(); got.Target != editorgroup.SettingsWorkspace || got.Query != "tab size" {
		t.Errorf("pane view state %+v", got)
	}
}

func TestChangingTheSettingsViewSavesTheState(t *testing.T) {
	env := newStateEnv(t, nil)
	run := env.start(env.root, env.path("main.go"))
	run.command("workbench.action.openSettings", nil)
	run.ts.Pump(stateSnapshotDelay + 50*time.Millisecond)
	env.clock.Advance(storage.DefaultStateDelay)
	// the search of the editor is part of the state: it is written after the delays
	run.command("workbench.action.openSettings", "font")
	run.ts.Pump(stateSnapshotDelay + 50*time.Millisecond)
	env.clock.Advance(storage.DefaultStateDelay)
	st, ok := env.savedWindow(env.root)
	if !ok || st.Grid == nil {
		t.Fatalf("no saved state: %v", ok)
	}
	var found string
	for _, e := range st.Grid.Root[0].Model.Editors {
		if e.TypeID == editorgroup.SettingsInputTypeID {
			found = e.Value
		}
	}
	if !strings.Contains(found, `"query":"font"`) {
		t.Errorf("saved settings editor %q", found)
	}
}

// TestTheSettingsEditorKind checks the kind of editor of the settings input: what is registered and how a saved value
// comes back.
func TestTheSettingsEditorKind(t *testing.T) {
	if _, ok := kindOf("emetos.nothing"); ok {
		t.Fatal("an unknown kind exists")
	}
	if k, ok := kindOf(editorgroup.SettingsInputTypeID); !ok || k.restore == nil || k.newPane == nil || k.icon == 0 {
		t.Fatalf("settings kind %+v", k)
	}
	if _, ok := settingsKind().restore("{nope"); ok {
		t.Error("a broken saved value came back")
	}
	if in, ok := settingsKind().restore(""); !ok || in == nil {
		t.Error("an empty saved value does not give the default view")
	}
}

func TestSettingsKeyPosition(t *testing.T) {
	const text = "{\n  \"editor.fontSize\": 14,\n  // c\n  \"files.associations\": {},\n  \"editor\": {\n    \"tabSize\": 2\n  }\n}\n"
	for _, c := range []struct {
		key       string
		line, col int
	}{
		{"editor.fontSize", 2, 3},
		{"files.associations", 4, 3},
		{"editor.tabSize", 6, 5},
		{"editor.wordWrap", 0, 0},
		{"", 0, 0},
	} {
		if line, col := settingsKeyPosition(text, c.key); line != c.line || col != c.col {
			t.Errorf("%q: %d:%d, want %d:%d", c.key, line, col, c.line, c.col)
		}
	}
	if line, col := settingsKeyPosition("", "editor.fontSize"); line != 0 || col != 0 {
		t.Errorf("empty text %d:%d", line, col)
	}
}

func TestEnsureSettingsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", ".emetos", "settings.json")
	if err := ensureSettingsFile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "{\n}\n" {
		t.Errorf("file %q", got)
	}
	// an existing file is left alone
	if err := os.WriteFile(path, []byte("{ // mine\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureSettingsFile(context.Background(), path); err != nil || readFile(t, path) != "{ // mine\n}" {
		t.Errorf("err %v, file %q", err, readFile(t, path))
	}
	// a path under a file cannot be made
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := ensureSettingsFile(context.Background(), filepath.Join(blocker, "x", "settings.json"))
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("ensureSettingsFile.SettingsFileCreateFailed") {
		t.Errorf("error %v", err)
	}
}

func TestOpenSettingsJSONOfTheWorkspaceNeedsAFolder(t *testing.T) {
	a := newSettingsApp(t, "")
	a.shell().openSettingsJSON(settings.TargetWorkspace, "")
	a.pump()
	found := false
	for _, n := range a.win.Notify.Items() {
		if strings.Contains(n.Message, "Open a folder") {
			found = true
		}
	}
	if !found {
		t.Error("no notification")
	}
}

func TestTheSettingsJSONOfAFolderIsMadeAndOpened(t *testing.T) {
	dir := t.TempDir()
	a := newSettingsApp(t, dir)
	a.shell().openSettingsJSON(settings.TargetWorkspace, "editor.tabSize")
	a.waitFor("settings.json to open", func() bool { return a.active() == "settings.json" })
	if got := readFile(t, filepath.Join(dir, ".emetos", "settings.json")); got != "{\n}\n" {
		t.Errorf("file %q", got)
	}
	a.noErrors()
}

func TestWorkbenchSettingsEditorJSONOpensTheFile(t *testing.T) {
	a := newSettingsApp(t, "")
	a.setting("workbench.settings.editor", "json")
	a.run("workbench.action.openSettings")
	a.waitFor("settings.json to open", func() bool { return a.active() == "settings.json" })
	if len(a.settingsInputs()) != 0 {
		t.Error("the settings editor opened")
	}
	a.noErrors()
}
