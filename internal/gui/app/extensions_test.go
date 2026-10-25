package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/extensionsui"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests of the extensions in the window (step 11.1): a workspace is a scope of the host, the contributions of an
// extension are those of its workspace only, and the Extensions page lists and switches the extensions.

// testExtension is an extension of the tests: its manifest declares a command, a menu item and a key, and its Activate
// is what a test sets.
type testExtension struct {
	manifest extension.Manifest
	activate func(ctx context.Context, api extension.API) error
}

func (e *testExtension) Manifest() extension.Manifest { return e.manifest }
func (e *testExtension) Activate(ctx context.Context, api extension.API) error {
	if e.activate == nil {
		return nil
	}
	return e.activate(ctx, api)
}

// extensionLog records the activations of an extension.
type extensionLog struct {
	mu   sync.Mutex
	ctxs []context.Context
	runs []string
}

func (l *extensionLog) activations() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.ctxs)
}

// greeterFactory makes the extension "emetos.greeter": it declares the command greeter.hello, a menu item, the key
// ctrl+alt+h and a status item that it adds when it is activated by event. The log records its activations, and the
// runs of the command with the name of the workspace.
func greeterFactory(events []string, extra func(api extension.API) error) (extension.Factory, *extensionLog) {
	log := &extensionLog{}
	return func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{
				ID: "emetos.greeter", Name: "Greeter", Version: "1.2.3", Description: "Says hello.",
				ActivationEvents: events,
				Contributes: extension.Contributes{
					Commands:    []extension.CommandSpec{{ID: "greeter.hello", Title: "Say Hello", Category: "Greeter"}},
					Menus:       []extension.MenuItem{{Menu: extension.MenuView, Command: "greeter.hello", Group: "9_greeter"}},
					Keybindings: []extension.Keybinding{{Command: "greeter.hello", Key: "ctrl+alt+h"}},
				},
			},
			activate: func(ctx context.Context, api extension.API) error {
				log.mu.Lock()
				log.ctxs = append(log.ctxs, ctx)
				log.mu.Unlock()
				if err := api.Commands().Handle("greeter.hello", func(context.Context, any) error {
					log.mu.Lock()
					log.runs = append(log.runs, api.Scope().Name)
					log.mu.Unlock()
					return nil
				}); err != nil {
					return err
				}
				if _, err := api.StatusBar().Add(extension.StatusItem{ID: "hello", Text: "hello"}); err != nil {
					return err
				}
				if extra != nil {
					return extra(api)
				}
				return nil
			},
		}
	}, log
}

// extensionProcess is a process with the extensions, over isolated folders.
func extensionProcess(t *testing.T, factories ...extension.Factory) *Process {
	t.Helper()
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"files.hotExit": "off"})
	return newTestProcess(ProcessOptions{Paths: paths, Extensions: factories})
}

// newExtensionWorkspace makes a workspace over a folder (a new one with the files when it is empty), and closes it with the test.
func newExtensionWorkspace(t *testing.T, p *Process, folder string, files ...string) *Workspace {
	t.Helper()
	if folder == "" && len(files) > 0 {
		folder = t.TempDir()
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(folder, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w, err := NewWorkspace(context.Background(), p, folder)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.release)
	return w
}

func statusIDs(w *Workspace) []string {
	var out []string
	for _, a := range []workbench.StatusAlignment{workbench.StatusLeft, workbench.StatusRight} {
		for _, it := range w.StatusBar.Items(a) {
			if strings.HasPrefix(it.ID, "extension.") {
				out = append(out, it.ID)
			}
		}
	}
	return out
}

func TestEachWorkspaceIsAScopeOfTheHost(t *testing.T) {
	f, log := greeterFactory([]string{extension.OnStartup}, nil)
	p := extensionProcess(t, f)
	a := newExtensionWorkspace(t, p, "", "go.mod")
	if a.Extensions() == nil || a.Extensions().State("emetos.greeter") != exthost.StateActive {
		t.Fatalf("the workspace is not a scope with the extension active: %v", a.Extensions())
	}
	// the contributions are the workspace's: its commands and menus, not the process's and not another workspace's
	if _, ok := a.Commands.Lookup("greeter.hello"); !ok {
		t.Error("the command of the extension is not in the registry of the workspace")
	}
	if _, ok := p.Commands.Lookup("greeter.hello"); ok {
		t.Error("the command of the extension leaked into the registry of the process")
	}
	if got := entries(a, command.MenubarViewMenu); !slices.Contains(got, "greeter.hello") {
		t.Errorf("the View menu of the workspace has %v", got)
	}
	if got := entries(&Workspace{Menus: p.Menus}, command.MenubarViewMenu); slices.Contains(got, "greeter.hello") {
		t.Errorf("the menus of the process have the item: %v", got)
	}
	if got := statusIDs(a); !slices.Equal(got, []string{"extension.emetos.greeter.hello"}) {
		t.Errorf("status items = %v", got)
	}
	if _, ok := a.Keybindings.LookupPrimaryKeybinding("greeter.hello", a.Keys, false); !ok {
		t.Error("the key of the extension is not in the keymap of the workspace")
	}

	b := newExtensionWorkspace(t, p, "", "go.mod")
	if log.activations() != 2 {
		t.Fatalf("%d activations for two workspaces, want 2", log.activations())
	}
	if len(p.Extensions.Scopes()) != 2 {
		t.Errorf("the host has %d scopes", len(p.Extensions.Scopes()))
	}
	for _, w := range []*Workspace{a, b} {
		if err := w.runCommand("greeter.hello", nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(log.runs) != 2 || log.runs[0] == "" {
		t.Errorf("runs = %v", log.runs)
	}

	// releasing a workspace takes away what the extension gave it and nothing else
	a.release()
	if _, ok := a.Commands.Lookup("greeter.hello"); ok {
		t.Error("the command stayed after the workspace was released")
	}
	if got := statusIDs(a); len(got) != 0 {
		t.Errorf("status items left: %v", got)
	}
	if _, ok := a.Keybindings.LookupPrimaryKeybinding("greeter.hello", a.Keys, false); ok {
		t.Error("the key stayed after the workspace was released")
	}
	if log.ctxs[0].Err() == nil || log.ctxs[1].Err() != nil {
		t.Errorf("contexts: %v, %v; only the first workspace was released", log.ctxs[0].Err(), log.ctxs[1].Err())
	}
	if _, ok := b.Commands.Lookup("greeter.hello"); !ok {
		t.Error("the other workspace lost its command")
	}
}

// entries lists the commands of the entries of a menu of a workspace.
func entries(w *Workspace, menu string) []string {
	var out []string
	for _, e := range w.Menus.Items(menu, contextkey.NewRoot()) {
		out = append(out, e.Command)
	}
	return out
}

func TestExtensionsStartOnTheirEvents(t *testing.T) {
	tests := []struct {
		name   string
		events []string
		// setup happens after the workspace is made and returns true when the extension should then be active.
		setup func(t *testing.T, w *Workspace)
		want  bool
	}{
		{"onStartup", []string{extension.OnStartup}, func(*testing.T, *Workspace) {}, true},
		{"onLanguage waits for a document of the language", []string{extension.OnLanguage("go")}, func(t *testing.T, w *Workspace) {
			path := filepath.Join(w.Folder, "main.go")
			if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Documents.OpenFile(context.Background(), path, document.NewOptions()); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"onLanguage is not woken by another language", []string{extension.OnLanguage("go")}, func(t *testing.T, w *Workspace) {
			path := filepath.Join(w.Folder, "notes.txt")
			if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Documents.OpenFile(context.Background(), path, document.NewOptions()); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"onFile finds the file in the folder", []string{extension.OnFile("go.mod")}, func(t *testing.T, w *Workspace) {
			w.FileIndex().Start(context.Background())
			w.FileIndex().Wait()
		}, true},
		{"onFile is not woken by another file", []string{extension.OnFile("Cargo.toml")}, func(t *testing.T, w *Workspace) {
			w.FileIndex().Start(context.Background())
			w.FileIndex().Wait()
		}, false},
		{"onCommand runs with the command", []string{extension.OnCommand("greeter.hello")}, func(t *testing.T, w *Workspace) {
			if err := w.runCommand("greeter.hello", nil); err != nil {
				t.Fatal(err)
			}
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, log := greeterFactory(tt.events, nil)
			p := extensionProcess(t, f)
			w := newExtensionWorkspace(t, p, "", "go.mod")
			if tt.events[0] != extension.OnStartup && log.activations() != 0 {
				t.Fatalf("activated before its event")
			}
			tt.setup(t, w)
			// the events that come from other goroutines are handed over to the UI thread: without a shell that is at once
			if got := w.Extensions().State("emetos.greeter") == exthost.StateActive; got != tt.want {
				t.Errorf("active = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtensionsDisabledTurnsExtensionsOffInEveryWorkspace(t *testing.T) {
	f, log := greeterFactory([]string{extension.OnStartup}, nil)
	p := extensionProcess(t, f)
	a := newExtensionWorkspace(t, p, "", "go.mod")
	b := newExtensionWorkspace(t, p, "", "go.mod")
	if log.activations() != 2 {
		t.Fatalf("%d activations", log.activations())
	}
	if err := a.setExtensionEnabled("emetos.greeter", false); err != nil {
		t.Fatal(err)
	}
	reloadOthers(t, a, b)
	for name, w := range map[string]*Workspace{"a": a, "b": b} {
		if got := w.Extensions().State("emetos.greeter"); got != exthost.StateDisabled {
			t.Errorf("workspace %s: %v, want disabled", name, got)
		}
		if _, ok := w.Commands.Lookup("greeter.hello"); ok {
			t.Errorf("workspace %s: the command of a disabled extension is still there", name)
		}
	}
	if log.ctxs[0].Err() == nil || log.ctxs[1].Err() == nil {
		t.Errorf("the contexts were not cancelled")
	}
	if got := a.disabledExtensions(); !slices.Equal(got, []string{"emetos.greeter"}) {
		t.Errorf("extensions.disabled = %v", got)
	}

	// a workspace made now does not activate it
	c := newExtensionWorkspace(t, p, "", "go.mod")
	if got := c.Extensions().State("emetos.greeter"); got != exthost.StateDisabled {
		t.Errorf("a new workspace has the extension %v", got)
	}

	// and on again
	if err := b.setExtensionEnabled("emetos.greeter", true); err != nil {
		t.Fatal(err)
	}
	reloadOthers(t, b, a, c)
	for name, w := range map[string]*Workspace{"a": a, "b": b, "c": c} {
		if got := w.Extensions().State("emetos.greeter"); got != exthost.StateActive {
			t.Errorf("workspace %s: %v, want active", name, got)
		}
	}
	if got := a.disabledExtensions(); len(got) != 0 {
		t.Errorf("extensions.disabled = %v after enabling", got)
	}
}

// reloadOthers does what Workspaces.followUserSettings does for the tabs of a window: the workspaces that share the
// settings.json of the user read it again when one of them wrote it. The workspaces of these tests belong to no window.
func reloadOthers(t *testing.T, _ *Workspace, others ...*Workspace) {
	t.Helper()
	for _, w := range others {
		if _, err := w.Settings.Reload(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestACommandThatPanicsFailsTheExtensionInItsWorkspaceOnly(t *testing.T) {
	f, _ := greeterFactory([]string{extension.OnStartup}, func(api extension.API) error {
		return api.Commands().Register(extension.CommandSpec{ID: "greeter.boom", Title: "Boom"}, func(context.Context, any) error {
			panic("kaboom")
		})
	})
	p := extensionProcess(t, f)
	a := newExtensionWorkspace(t, p, "", "go.mod")
	b := newExtensionWorkspace(t, p, "", "go.mod")
	// the palette and the keys run commands through the registry: nothing is returned for the panic, and one notification
	if err := a.runCommand("greeter.boom", nil); err != nil {
		t.Fatalf("a panic in a command came back as an error: %v", err)
	}
	if got := a.Extensions().State("emetos.greeter"); got != exthost.StateFailed {
		t.Fatalf("state = %v", got)
	}
	appErr, ok := errors.FromError(a.Extensions().Failure("emetos.greeter").Err)
	if !ok || !appErr.HasRoute("Scope.runCommand.func1.ExtensionCommandPanicked") {
		t.Errorf("failure = %+v", a.Extensions().Failure("emetos.greeter"))
	}
	var failures int
	for _, it := range a.Notify.Items() {
		if strings.HasPrefix(it.Message, "Greeter failed") {
			failures++
		}
	}
	if failures != 1 {
		t.Errorf("%d notifications of the failure, want 1: %v", failures, a.Notify.Items())
	}
	if got := b.Extensions().State("emetos.greeter"); got != exthost.StateActive {
		t.Errorf("the other workspace has the extension %v", got)
	}
	if len(b.Notify.Items()) != 0 {
		t.Errorf("the other workspace was notified: %v", b.Notify.Items())
	}
}

func TestTheKeyOfAnExtensionRunsItsCommandThroughTheDispatcher(t *testing.T) {
	f, log := greeterFactory([]string{extension.OnStartup}, nil)
	p := extensionProcess(t, f)
	win := newExtensionWorkspace(t, p, "", "go.mod")
	a := &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
	a.ts = testutil.New(t, testWindowSize)
	a.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	a.pump()
	kb, err := keybinding.ParseKeybinding("ctrl+alt+h")
	if err != nil {
		t.Fatal(err)
	}
	if !sendChord(a.ts, kb[0]) {
		t.Fatal("the key was not used")
	}
	a.pump()
	if len(log.runs) != 1 {
		t.Errorf("the key ran the command %d times", len(log.runs))
	}
	// the key is in the keybindings editor's model too
	found := false
	for _, r := range win.KeybindingsModel().Rows() {
		found = found || r.Command == "greeter.hello"
	}
	if !found {
		t.Errorf("the keybindings editor does not list the key of the extension")
	}
}

func TestTheExtensionsPageListsAndSwitchesTheExtensions(t *testing.T) {
	greeter, _ := greeterFactory([]string{extension.OnStartup}, nil)
	idle := func() extension.Extension {
		return &testExtension{manifest: extension.Manifest{ID: "emetos.idle", Name: "Idle", ActivationEvents: []string{extension.OnLanguage("rust")}}}
	}
	broken := func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{ID: "emetos.broken", Name: "Broken", Description: "Always fails.", ActivationEvents: []string{extension.OnStartup}},
			activate: func(context.Context, extension.API) error { panic("no") },
		}
	}
	p := extensionProcess(t, greeter, idle, broken)
	folder := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	win := newExtensionWorkspace(t, p, folder)
	a := &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
	a.ts = testutil.New(t, testWindowSize)
	a.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	a.pump()

	a.run("workbench.extensions.action.showInstalledExtensions")
	if a.active() != "Extensions" {
		t.Fatalf("the active tab is %q, want Extensions", a.active())
	}
	// running the command again reveals the page and does not open another
	a.run("workbench.extensions.action.showInstalledExtensions")
	count := 0
	for _, n := range a.tabNames() {
		if n == "Extensions" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%d Extensions tabs", count)
	}
	for _, want := range []string{"Greeter", "1.2.3", "Says hello.", "emetos.greeter", "Idle", "Broken", "Always fails.", "Active in proj.", "Not active yet.",
		"Failed in proj: Broken failed.", "activates on onStartup", "activates on onLanguage:rust"} {
		if !a.ts.Exists(gluttertest.ByTextContaining(want)) {
			t.Errorf("the page does not show %q", want)
		}
	}
	// Disable the greeter
	a.ts.Tap(gluttertest.ByKey(extensionsui.ToggleKey("emetos.greeter")))
	a.pump()
	if got := win.Extensions().State("emetos.greeter"); got != exthost.StateDisabled {
		t.Fatalf("after Disable the state is %v", got)
	}
	if !a.ts.Exists(gluttertest.ByText("Disabled")) {
		t.Errorf("the page does not say Disabled")
	}
	if got := settings.Get[[]string](win.Settings, "extensions.disabled", ""); !slices.Equal(got, []string{"emetos.greeter"}) {
		t.Errorf("extensions.disabled = %v", got)
	}
	// and Enable again
	a.ts.Tap(gluttertest.ByKey(extensionsui.ToggleKey("emetos.greeter")))
	a.pump()
	if got := win.Extensions().State("emetos.greeter"); got != exthost.StateActive {
		t.Fatalf("after Enable the state is %v", got)
	}
	if got := settings.Get[[]string](win.Settings, "extensions.disabled", ""); len(got) != 0 {
		t.Errorf("extensions.disabled = %v after Enable", got)
	}
}

func TestTheExtensionsPageHasMenuRowsAndAKey(t *testing.T) {
	p := extensionProcess(t)
	w := newExtensionWorkspace(t, p, "")
	const id = "workbench.extensions.action.showInstalledExtensions"
	if _, ok := w.Commands.Lookup(id); !ok {
		t.Fatalf("%s is not a command", id)
	}
	for _, menu := range []string{command.MenubarViewMenu, command.GlobalActivity, command.MenubarPreferencesMenu} {
		if !slices.Contains(entries(w, menu), id) {
			t.Errorf("the menu %s lacks Extensions", menu)
		}
	}
	item, ok := w.Keybindings.LookupPrimaryKeybinding(id, w.Keys, false)
	if !ok || item.Keybinding == nil {
		t.Errorf("no key for the page: %+v", item)
	}
}

func TestAProcessWithNoExtensionsStillGivesEachWorkspaceAScope(t *testing.T) {
	p := extensionProcess(t)
	w := newExtensionWorkspace(t, p, "")
	if w.Extensions() == nil || len(p.Extensions.Manifests()) != 0 {
		t.Errorf("scope %v, manifests %v", w.Extensions(), p.Extensions.Manifests())
	}
}

func TestExtensionsReadTheDocumentsOfTheirWorkspace(t *testing.T) {
	var api extension.API
	f, _ := greeterFactory([]string{extension.OnStartup}, func(a extension.API) error { api = a; return nil })
	p := extensionProcess(t, f)
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "on-disk.txt"), []byte("from disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := newExtensionWorkspace(t, p, folder)
	openPath := filepath.Join(folder, "open.txt")
	if err := os.WriteFile(openPath, []byte("saved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := w.Documents.OpenFile(context.Background(), openPath, document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Edit([]document.Edit{{Text: "unsaved "}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	read := func(uri string) (string, error) { return api.Documents().Read(context.Background(), uri) }

	if got, err := read(document.FileURI(openPath).String()); err != nil || got != "unsaved saved\n" {
		t.Errorf("an open document: %q, %v; want the text with its unsaved changes", got, err)
	}
	if got, err := read(document.FileURI(filepath.Join(folder, "on-disk.txt")).String()); err != nil || got != "from disk\n" {
		t.Errorf("a file of the folder: %q, %v", got, err)
	}
	if got := api.Documents().Open(); !slices.Equal(got, []string{document.FileURI(openPath).String()}) {
		t.Errorf("Open = %v", got)
	}
	tests := []struct{ name, uri, route string }{
		{"a file outside the folder", document.FileURI(outside).String(), "extensionDocuments.Read.ExtDocumentOutsideFolder"},
		{"a document that is not open and not a file", "untitled:7", "extensionDocuments.Read.ExtDocumentOutsideFolder"},
		{"text that is not a URI", "not a uri", "URIInvalid"},
		{"a file of the folder that does not exist", document.FileURI(filepath.Join(folder, "missing.txt")).String(), "FileNotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := read(tt.uri)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
