package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/jsonc"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/settingsui"
)

// The tests of step 8.2 in the window: the arguments of the commands that open the settings, the JSON commands, and
// the read-only document of the default settings.

// settingsState is the view state of the one settings editor.
func (a *groupApp) settingsState() editorgroup.SettingsViewState {
	a.t.Helper()
	in := a.settingsInputs()
	if len(in) != 1 {
		a.t.Fatalf("%d settings editors", len(in))
	}
	return in[0].ViewState()
}

func TestOpenSettingsTakesTheSearchTextInEveryForm(t *testing.T) {
	for _, c := range []struct {
		name string
		id   string
		args any
		want string
	}{
		{"a string", "workbench.action.openSettings", "tab size", "tab size"},
		{"a query object", "workbench.action.openSettings", map[string]any{"query": "@modified"}, "@modified"},
		{"a language filter", "workbench.action.openSettings", "@lang:go tab", "@lang:go tab"},
		{"ids", "workbench.action.openSettings", map[string]any{"query": "@id:editor.fontSize,editor.tabSize"}, "@id:editor.fontSize,editor.tabSize"},
		{"no argument", "workbench.action.openSettings", nil, ""},
		{"an object without a query", "workbench.action.openSettings", map[string]any{"focusSearch": true}, ""},
		{"a wrong type", "workbench.action.openSettings", 42, ""},
		{"a revealed setting", "workbench.action.openSettings", map[string]any{"revealSetting": map[string]any{"key": "editor.fontSize"}}, "@id:editor.fontSize"},
		{"the query wins over a revealed setting", "workbench.action.openSettings", map[string]any{"query": "tab", "revealSetting": map[string]any{"key": "editor.fontSize"}}, "tab"},
		{"Open Settings (UI)", "workbench.action.openSettings2", map[string]any{"query": "font"}, "font"},
		{"Open User Settings", "workbench.action.openGlobalSettings", "font", "font"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := newSettingsApp(t, "")
			a.run2(c.id, c.args)
			if got := a.settingsState(); got.Query != c.want || got.Target == editorgroup.SettingsWorkspace {
				t.Errorf("view %+v, want the query %q", got, c.want)
			}
			a.noErrors()
		})
	}
	// the search box has the text and the list is the result
	a := newSettingsApp(t, "")
	a.searchSettings("@id:editor.tabSize")
	if !a.ts.Exists(gluttertest.ByText("1 Setting Found")) {
		t.Error("the count of the search is not shown")
	}
	// with the editor open, a new search replaces the old one, and none leaves it
	a.run2("workbench.action.openSettings", "font")
	a.run("workbench.action.openSettings")
	if got := a.settingsState().Query; got != "font" {
		t.Errorf("the query is %q", got)
	}
	if len(a.settingsInputs()) != 1 {
		t.Error("a second editor opened")
	}
}

func TestOpenUserSettingsJSON(t *testing.T) {
	a := newSettingsApp(t, "")
	user := a.userSettings()
	if err := os.Remove(user); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.action.openSettingsJson")
	a.waitFor("settings.json to open", func() bool { return a.active() == "settings.json" })
	if got := readFile(t, user); got != "{\n}\n" {
		t.Errorf("the file was made as %q", got)
	}
	if len(a.settingsInputs()) != 0 {
		t.Error("the settings editor opened")
	}
	if strings.TrimSpace(a.vm().Document().Text()) != "{\n}" {
		t.Errorf("text %q", a.vm().Document().Text())
	}
	a.noErrors()

	// an existing file is not touched, and the cursor goes to the key
	text := "{\n  // size\n  \"editor.fontSize\": 16,\n  \"editor\": {\n    \"tabSize\": 2\n  }\n}\n"
	if err := os.WriteFile(user, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.action.closeActiveEditor")
	for _, c := range []struct {
		key  string
		line int
	}{
		{"editor.fontSize", 3},
		{"editor.tabSize", 5},
		{"editor.wordWrap", 1},
	} {
		a.run2("workbench.action.openSettingsJson", map[string]any{"revealSetting": map[string]any{"key": c.key, "edit": false}})
		a.waitFor("settings.json to open", func() bool { return a.active() == "settings.json" })
		if got := a.cursorLine(); got != c.line {
			t.Errorf("%s: the cursor is on line %d, want %d", c.key, got, c.line)
		}
		a.run("workbench.action.closeActiveEditor")
	}
	if got := readFile(t, user); got != text {
		t.Errorf("the file changed: %q", got)
	}
	// a wrong argument is ignored
	a.run2("workbench.action.openSettingsJson", map[string]any{"revealSetting": "editor.fontSize"})
	a.waitFor("settings.json to open", func() bool { return a.active() == "settings.json" })
	a.noErrors()
}

func TestOpenWorkspaceSettingsFile(t *testing.T) {
	dir := t.TempDir()
	a := newSettingsApp(t, dir)
	want := filepath.Join(dir, ".emetos", "settings.json")
	a.run("workbench.action.openWorkspaceSettingsFile")
	a.waitFor("the workspace settings.json to open", func() bool { return a.active() == "settings.json" })
	if got := readFile(t, want); got != "{\n}\n" {
		t.Errorf("the file was made as %q", got)
	}
	if got := a.vm().Document().URI(); got != document.FileURI(want) {
		t.Errorf("the editor shows %v, want %s", got, want)
	}
	a.noErrors()

	// without a folder the palette does not list it, and the command says why
	b := newSettingsApp(t, "")
	b.win.Keys.Set("workbenchState", "empty")
	if paletteShows(b.win, "workbench.action.openWorkspaceSettingsFile") {
		t.Error("the palette lists Open Workspace Settings (JSON) without a folder")
	}
	b.run("workbench.action.openWorkspaceSettingsFile")
	b.pump()
	found := false
	for _, n := range b.win.Notify.Items() {
		if strings.Contains(n.Message, "Open a folder") {
			found = true
		}
	}
	if !found || len(b.settingsInputs()) != 0 || b.active() != "a.txt" {
		t.Errorf("no notice, or something opened: %v", b.tabNames())
	}
	// and with one it does
	a.win.Keys.Set("workbenchState", "folder")
	if !paletteShows(a.win, "workbench.action.openWorkspaceSettingsFile") {
		t.Error("the palette does not list it with a folder")
	}
}

// paletteShows tells whether the rules of the palette show a command in a window.
func paletteShows(win *Window, id string) bool {
	for _, it := range win.Process.Menus.All() {
		if it.Menu == command.CommandPalette && it.Command == id && it.When != nil && !it.When.Evaluate(contextkey.Context(win.Keys)) {
			return false
		}
	}
	_, ok := win.Commands.Lookup(id)
	return ok
}

func TestTheJSONButtonAndLinkRunTheCommands(t *testing.T) {
	dir := t.TempDir()
	a := newSettingsApp(t, dir)
	// the commands that the button and the link run are replaced by spies
	var ran []string
	for _, id := range []string{"workbench.action.openSettingsJson", "workbench.action.openWorkspaceSettingsFile"} {
		remove, err := a.win.Commands.SetHandler(id, func(_ command.Context, args any) error {
			ran = append(ran, id+" "+jsonOf(args))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		defer remove()
	}
	a.run("workbench.action.openSettings")
	a.shell().runOpenJSON(settings.TargetUser, "")
	a.shell().runOpenJSON(settings.TargetUser, "editor.fontSize")
	a.shell().runOpenJSON(settings.TargetWorkspace, "")
	want := []string{
		"workbench.action.openSettingsJson null",
		`workbench.action.openSettingsJson {"revealSetting":{"key":"editor.fontSize"}}`,
		"workbench.action.openWorkspaceSettingsFile null",
	}
	if strings.Join(ran, "|") != strings.Join(want, "|") {
		t.Errorf("ran %q, want %q", ran, want)
	}
}

func TestSettingsArgsOf(t *testing.T) {
	for _, c := range []struct {
		name string
		in   any
		want settingsArgs
	}{
		{"nil", nil, settingsArgs{}},
		{"a string", "font", settingsArgs{query: "font"}},
		{"a query", map[string]any{"query": "font"}, settingsArgs{query: "font"}},
		{"a query that is not text", map[string]any{"query": 3}, settingsArgs{}},
		{"a reveal", map[string]any{"revealSetting": map[string]any{"key": "a.b", "edit": true}}, settingsArgs{reveal: "a.b"}},
		{"a reveal without a key", map[string]any{"revealSetting": map[string]any{"edit": true}}, settingsArgs{}},
		{"a reveal that is not an object", map[string]any{"revealSetting": "a.b"}, settingsArgs{}},
		{"both", map[string]any{"query": "q", "revealSetting": map[string]any{"key": "a.b"}}, settingsArgs{query: "q", reveal: "a.b"}},
		{"a number", 3.5, settingsArgs{}},
	} {
		if got := settingsArgsOf(c.in); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestOpenDefaultSettingsIsAReadOnlyDocument(t *testing.T) {
	a := newSettingsApp(t, "")
	a.run("workbench.action.openRawDefaultSettings")
	wantNames(t, a.tabNames(), "a.txt", "Default Settings")
	if a.active() != "Default Settings" {
		t.Fatalf("active tab %q", a.active())
	}
	doc := a.vm().Document()
	uri := document.VirtualURI(DefaultSettingsPath)
	if doc.URI() != uri || !doc.ReadOnly() || doc.Dirty() || doc.LanguageID() != "jsonc" || uri.String() != "emetos-virtual:/defaultSettings.json" {
		t.Fatalf("document %v readonly %v dirty %v language %q", doc.URI(), doc.ReadOnly(), doc.Dirty(), doc.LanguageID())
	}
	// the text is the registry, valid JSONC with every setting
	text := doc.Text()
	if _, errs := jsonc.Parse(text, jsonc.ParseOptions{}); len(errs) > 0 {
		t.Fatalf("parse errors %v", errs)
	}
	if text != settings.DefaultSettingsJSON(a.win.Process.Settings) {
		t.Error("the document is not the default settings of the registry")
	}
	for _, want := range []string{"\t// Controls the font size in pixels.\n\t// Minimum: 6\n\t// Maximum: 100\n\t\"editor.fontSize\": 14,\n", "// Text Editor > Font"} {
		if !strings.Contains(text, want) {
			t.Errorf("the document lacks %q", want)
		}
	}
	// the read-only state shows
	if in := a.shell().activeGroup().Model.ActiveEditor(); !in.Capabilities().Has(editorgroup.CapReadonly) {
		t.Errorf("the editor is not read-only: %v", in.Capabilities())
	}

	// typing, pasting and deleting do nothing, and nothing is dirty
	a.vm().Cursors().SetSelections(nil)
	for _, cmd := range []struct {
		id   string
		args any
	}{
		{"type", map[string]any{"text": "xyz"}}, {"deleteLeft", nil}, {"deleteRight", nil}, {"editor.action.clipboardPasteAction", nil},
		{"editor.action.insertLineAfter", nil}, {"tab", nil},
	} {
		_ = a.vm().RunCommand(cmd.id, cmd.args)
	}
	a.pump()
	if doc.Text() != text || doc.Dirty() || a.item("Default Settings").Dirty {
		t.Errorf("the read-only document changed: dirty %v", doc.Dirty())
	}

	// Save does nothing: no error, no file, no dialog
	before := len(a.win.Notify.Items())
	a.run("workbench.action.files.save")
	a.run("workbench.action.files.saveAll")
	fd := a.ts.FakeDesktop()
	a.run("workbench.action.files.saveAs")
	if fd.PendingFileChoosers() != 0 {
		t.Error("Save As asks for a file for a read-only document")
	}
	quiet := 0
	for _, n := range a.win.Notify.Items()[before:] {
		if n.Silent && strings.Contains(n.Message, "read-only") {
			quiet++
		}
	}
	if quiet < 1 {
		t.Errorf("%d quiet messages about the read-only document, want one", quiet)
	}
	a.pump()
	for _, n := range a.win.Notify.Items()[before:] {
		if n.Severity != 0 && n.Severity.String() == "error" {
			t.Errorf("an error: %+v", n)
		}
	}
	if doc.Dirty() || doc.URI() != uri {
		t.Error("the document was saved as something else")
	}
	if _, err := os.Stat(DefaultSettingsPath); err == nil {
		t.Error("a file was written at the path of the URI")
	}
	a.noErrors()
}

func TestOpenDefaultSettingsAgainMakesItAgain(t *testing.T) {
	a := newSettingsApp(t, "")
	a.run("workbench.action.openRawDefaultSettings")
	doc := a.vm().Document()
	if strings.Contains(doc.Text(), `"zzz.new"`) {
		t.Fatal("the setting is there before it is registered")
	}
	// a setting registered since: the open document is refreshed, not duplicated
	a.win.Process.Settings.MustRegister(settings.Setting{Key: "zzz.new", Types: []settings.Type{settings.Boolean}, Default: true, Description: "A new setting."})
	a.run("workbench.action.openRawDefaultSettings")
	if !strings.Contains(a.vm().Document().Text(), "\t// A new setting.\n\t\"zzz.new\": true") {
		t.Error("the document does not show the new setting")
	}
	wantNames(t, a.tabNames(), "a.txt", "Default Settings")
	if a.vm().Document() != doc || doc.Dirty() {
		t.Error("another document was made, or it is dirty")
	}
	// closing the tab closes the document without a question, and opening makes a new one
	a.run("workbench.action.closeActiveEditor")
	wantNames(t, a.tabNames(), "a.txt")
	if _, open := a.win.Documents.Get(document.VirtualURI(DefaultSettingsPath)); open {
		t.Error("the document stays open")
	}
	if a.win.Dialogs.IsShowing() {
		t.Error("closing a read-only document asked something")
	}
	a.run("workbench.action.openRawDefaultSettings")
	if a.vm().Document() == doc {
		t.Error("the closed document came back")
	}
	a.noErrors()
}

func TestTheDefaultSettingsDocumentComesBackAfterARestart(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.command("workbench.action.openRawDefaultSettings", nil)
	want := first.layout()
	if !strings.Contains(strings.Join(want, "|"), "Default Settings") {
		t.Fatalf("layout %v", want)
	}
	first.close()

	second := env.start(env.root)
	if got := second.layout(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("layout %v, want %v", got, want)
	}
	doc, ok := second.win.Documents.Get(document.VirtualURI(DefaultSettingsPath))
	if !ok || !doc.ReadOnly() || doc.Dirty() || doc.Text() != settings.DefaultSettingsJSON(second.win.Process.Settings) {
		t.Errorf("the restored document: %v, %v", ok, doc)
	}
	if n := len(second.win.Notify.Items()); n != 0 {
		t.Errorf("%d notifications", n)
	}
	second.close()
}

func TestTheVirtualDocumentIsNotBackedUp(t *testing.T) {
	a := newSettingsApp(t, "")
	a.run("workbench.action.openRawDefaultSettings")
	doc := a.vm().Document()
	if storage.Backs(doc) {
		t.Error("the backup tracker follows a virtual document")
	}
}

func TestOpenVirtualErrors(t *testing.T) {
	win := newTestWindow(t, "")
	ctx := context.Background()
	boom := fs.ErrPermission
	if err := win.Process.Virtual.Register("/broken.json", document.VirtualProvider{Text: func(context.Context) (string, error) { return "", boom }}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		uri   document.URI
		route string
	}{
		{"no provider", document.VirtualURI("/nothing.json"), "Documents.OpenVirtual->VirtualRegistry.Generate.VirtualURIUnknown"},
		{"a file", document.FileURI("/tmp/x"), "Documents.OpenVirtual->VirtualRegistry.Generate.VirtualURIUnknown"},
		{"a provider that fails", document.VirtualURI("/broken.json"), "Documents.OpenVirtual->VirtualRegistry.Generate.VirtualGenerateFailed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := win.Documents.OpenVirtual(ctx, c.uri)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(c.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if _, open := win.Documents.Get(c.uri); open {
				t.Error("a document was opened")
			}
			if _, err := win.Documents.AcquireVirtual(ctx, c.uri); err == nil {
				t.Error("AcquireVirtual succeeded")
			}
		})
	}
	// the command fails with the tag too, and opens nothing
	win2 := newTestWindow(t, "")
	win2.Process.Virtual = document.NewVirtualRegistry()
	win2.Documents.SetVirtual(win2.Process.Virtual)
	a := newSettingsApp(t, "")
	a.win.Documents.SetVirtual(document.NewVirtualRegistry())
	err := a.win.Commands.Run(command.Context{Ctx: ctx, Keys: a.win.Keys, Source: command.SourceAPI}, "workbench.action.openRawDefaultSettings", nil)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("VirtualURIUnknown") {
		t.Errorf("the command's error %v", err)
	}
	wantNames(t, a.tabNames(), "a.txt")
}

func TestTheSettingsEditorGetsTheLanguagesOfTheProcess(t *testing.T) {
	win := newTestWindow(t, "")
	for name, want := range map[string]string{"go": "Go", "GO": "Go", "Markdown": "Markdown", " json ": "JSON with Comments"} {
		id, display, ok := win.resolveLanguage(name)
		if !ok || id == "" {
			t.Errorf("%q: no language", name)
			continue
		}
		if name == "go" && (id != "go" || display != want) {
			t.Errorf("%q: %q %q", name, id, display)
		}
	}
	if _, _, ok := win.resolveLanguage("zzz"); ok {
		t.Error("zzz is a language")
	}
	// the filter in the editor
	a := newSettingsApp(t, "")
	a.searchSettings("@lang:Go")
	if !a.ts.Exists(gluttertest.ByText(settingsui.LanguageHeaderPrefix + "Go")) {
		t.Error("no header for Go")
	}
}

func jsonOf(v any) string {
	data, err := jsonMarshal(v)
	if err != nil {
		return err.Error()
	}
	return data
}

func jsonMarshal(v any) (string, error) {
	data, err := json.Marshal(v)
	return string(data), err
}
