package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// File editors in the window (step 36.1): a test extension, "Viewers", is the editor of PNG files (an image preview, the default)
// and of every file (a hex view, an option).

const (
	viewersID   = "emetos.viewers"
	imageEditor = "emetos.viewers.image"
	hexEditor   = "emetos.viewers.hex"
)

// viewerLog records what the file editors of the test extension were asked.
type viewerLog struct {
	mu      sync.Mutex
	opened  []string          // "<editor> <base name>"
	states  []json.RawMessage // the state each tab started with
	changes []extension.FileEditorChange
	closed  int
}

func (l *viewerLog) openedList() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.opened)
}

// viewersFactory is the extension Viewers.
func viewersFactory(log *viewerLog) extension.Factory {
	open := func(id string) func(extension.FileEditorFile, extension.FileEditorTab) (extension.FileEditorContent, error) {
		return func(file extension.FileEditorFile, tab extension.FileEditorTab) (extension.FileEditorContent, error) {
			zoom := 1
			var saved struct{ Zoom int }
			if json.Unmarshal(file.State(), &saved) == nil && saved.Zoom != 0 {
				zoom = saved.Zoom
			}
			log.mu.Lock()
			log.opened = append(log.opened, id+" "+filepath.Base(file.Path()))
			log.states = append(log.states, file.State())
			log.mu.Unlock()
			file.OnChange(func(c extension.FileEditorChange) {
				log.mu.Lock()
				log.changes = append(log.changes, c)
				log.mu.Unlock()
			})
			tab.SetStatus([]extension.StatusItem{{ID: "size", Text: "1920x1080", Alignment: extension.StatusRight}})
			label := id + ":" + filepath.Base(file.Path())
			return extension.FileEditorContent{
				Build:     func(widgets.BuildContext) widgets.Widget { return widgets.NewText(label) },
				SaveState: func() (any, error) { return map[string]int{"Zoom": zoom + 1}, nil },
				OnClose: func() {
					log.mu.Lock()
					log.closed++
					log.mu.Unlock()
				},
			}, nil
		}
	}
	return func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{
				ID: viewersID, Name: "Viewers", Version: "1.0.0",
				Contributes: extension.Contributes{FileEditors: []extension.FileEditorSpec{
					{ID: imageEditor, Name: "Image Preview", Globs: []string{"*.png"}, Priority: extension.FileEditorDefault},
					{ID: hexEditor, Name: "Hex", Globs: []string{"*"}, Priority: extension.FileEditorOption},
				}},
			},
			activate: func(_ context.Context, api extension.API) error {
				for _, id := range []string{imageEditor, hexEditor} {
					if err := api.FileEditors().Provide(id, extension.FileEditorProvider{Open: open(id)}); err != nil {
						return err
					}
				}
				return nil
			},
		}
	}
}

// pngBytes look like a PNG: the NUL bytes make the file binary for the text editor.
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

// viewersApp is a window over a folder with cat.png, dog.png and notes.txt, with the extension Viewers.
func viewersApp(t *testing.T, values map[string]any) (*groupApp, *viewerLog, string) {
	t.Helper()
	log := &viewerLog{}
	paths := testutil.Isolate(t)
	all := map[string]any{"files.hotExit": "off"}
	for k, v := range values {
		all[k] = v
	}
	testutil.Settings(t, paths, all)
	p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{viewersFactory(log)}})
	folder := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"cat.png": pngBytes, "dog.png": pngBytes, "notes.txt": []byte("notes\n")} {
		if err := os.WriteFile(filepath.Join(folder, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	win := newExtensionWorkspace(t, p, folder)
	a := &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
	a.ts = testutil.New(t, testWindowSize)
	a.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	a.pump()
	return a, log, folder
}

// openFile opens a file as the Explorer does (a double click keeps it; preview: a single click).
func (a *groupApp) openFile(path string, preview bool) {
	a.shell().openPath(widgets.WidgetsBindingOf(a.shell().Context()), nil, path, preview)
	for range 3 {
		a.pump()
	}
}

// activeInput is the active editor of the active group.
func (a *groupApp) activeInput() editorgroup.EditorInput {
	return a.shell().activeGroup().Model.ActiveEditor()
}

// statusTexts are the texts of the items of the status bar.
func (a *groupApp) statusTexts() []string {
	var out []string
	for _, it := range a.win.StatusBar.Items(0) {
		out = append(out, it.Entry.Text)
	}
	for _, it := range a.win.StatusBar.Items(1) {
		out = append(out, it.Entry.Text)
	}
	return out
}

func TestAMatchingFileOpensInItsFileEditor(t *testing.T) {
	a, log, folder := viewersApp(t, nil)
	a.openFile(filepath.Join(folder, "cat.png"), false)
	in, ok := a.activeInput().(*editorgroup.FileEditorInput)
	if !ok || in.Editor() != imageEditor || in.Path() != filepath.Join(folder, "cat.png") {
		t.Fatalf("the active editor is %#v", a.activeInput())
	}
	if a.active() != "cat.png" || !a.ts.Exists(gluttertest.ByText(imageEditor+":cat.png")) {
		t.Errorf("tab %q; the editor's widget is not on screen", a.active())
	}
	if got := log.openedList(); !slices.Equal(got, []string{imageEditor + " cat.png"}) {
		t.Errorf("opened %v", got)
	}
	// the tab's status bar item shows while it is the active editor
	if !slices.Contains(a.statusTexts(), "1920x1080") {
		t.Errorf("status %v", a.statusTexts())
	}
	a.openFile(filepath.Join(folder, "notes.txt"), false)
	// the text is read on a goroutine of its own
	waitForRequirements(t, a, func() bool { _, ok := a.activeInput().(*editorgroup.TextInput); return ok })
	if _, ok := a.activeInput().(*editorgroup.TextInput); !ok {
		t.Fatalf("notes.txt opened in %#v", a.activeInput())
	}
	if slices.Contains(a.statusTexts(), "1920x1080") {
		t.Error("the item stays when the text editor is in front")
	}
	// opening the file again reveals its tab
	a.openFile(filepath.Join(folder, "cat.png"), false)
	if a.active() != "cat.png" || len(a.tabNames()) != 2 {
		t.Errorf("tabs %v, active %q", a.tabNames(), a.active())
	}
	// a change on disk reaches the editor; so does a deletion
	a.win.onFileEvents(context.Background(), []workspace.Event{{Kind: workspace.EventChanged, Path: filepath.Join(folder, "cat.png")}})
	a.win.onFileEvents(context.Background(), []workspace.Event{{Kind: workspace.EventDeleted, Path: filepath.Join(folder, "cat.png")}})
	a.pump()
	log.mu.Lock()
	changes := slices.Clone(log.changes)
	log.mu.Unlock()
	if len(changes) != 2 || changes[0].Deleted || !changes[1].Deleted {
		t.Errorf("changes %+v", changes)
	}
	// closing tells the extension
	a.run("workbench.action.closeActiveEditor")
	log.mu.Lock()
	closed := log.closed
	log.mu.Unlock()
	if closed != 1 {
		t.Errorf("OnClose ran %d times", closed)
	}
}

func TestAPreviewFileEditorIsReplacedByTheNextPreview(t *testing.T) {
	a, _, folder := viewersApp(t, nil)
	a.openFile(filepath.Join(folder, "cat.png"), true)
	a.openFile(filepath.Join(folder, "dog.png"), true)
	if got := a.tabNames(); !slices.Equal(got, []string{"dog.png"}) {
		t.Errorf("tabs %v", got)
	}
}

func TestReopenEditorWith(t *testing.T) {
	a, log, folder := viewersApp(t, nil)
	notes := filepath.Join(folder, "notes.txt")
	a.openFile(notes, false)
	waitForRequirements(t, a, func() bool { _, ok := a.activeInput().(*editorgroup.TextInput); return ok })
	a.run("workbench.action.reopenWithEditor")
	if got := a.pickerLabels(); !slices.Equal(got, []string{"Text Editor", "Hex", "Configure default editor for '*.txt'..."}) {
		t.Fatalf("rows %v", got)
	}
	rows := a.win.QuickInput.Picker().Rows()
	if rows[0].Item.Description != "Active and Default" || rows[0].Item.Detail != "Built-in" || rows[1].Item.Detail != "Viewers" {
		t.Errorf("rows %+v", rows)
	}
	a.pickRow("Hex")
	a.pump()
	in, ok := a.activeInput().(*editorgroup.FileEditorInput)
	if !ok || in.Editor() != hexEditor || len(a.tabNames()) != 1 {
		t.Fatalf("after Hex: %#v, tabs %v", a.activeInput(), a.tabNames())
	}
	// and back to the text editor
	a.run("workbench.action.reopenWithEditor")
	a.pickRow("Text Editor")
	waitForRequirements(t, a, func() bool { _, ok := a.activeInput().(*editorgroup.TextInput); return ok })
	if _, ok := a.activeInput().(*editorgroup.TextInput); !ok || len(a.tabNames()) != 1 {
		t.Fatalf("after Text Editor: %#v, tabs %v", a.activeInput(), a.tabNames())
	}
	log.mu.Lock()
	closed := log.closed
	log.mu.Unlock()
	if closed != 1 {
		t.Errorf("the hex view was told %d times that it closed", closed)
	}
}

func TestConfigureTheDefaultEditor(t *testing.T) {
	a, _, folder := viewersApp(t, nil)
	cat := filepath.Join(folder, "cat.png")
	a.openFile(cat, false)
	a.run("workbench.action.reopenWithEditor")
	a.pickRow("Configure default editor for '*.png'...")
	a.pickRow("Hex")
	a.pump()
	got := settings.Get[map[string]any](a.win.Settings, editorAssociationsSetting, "")
	if got["*.png"] != hexEditor {
		t.Fatalf("workbench.editorAssociations = %v", got)
	}
	// the next PNG opens in the hex view; text by association too
	a.openFile(filepath.Join(folder, "dog.png"), false)
	if in, ok := a.activeInput().(*editorgroup.FileEditorInput); !ok || in.Editor() != hexEditor {
		t.Errorf("dog.png opened in %#v", a.activeInput())
	}
}

func TestABinaryFileOffersOpenWith(t *testing.T) {
	a, _, folder := viewersApp(t, map[string]any{editorAssociationsSetting: map[string]any{"*.png": "default"}})
	cat := filepath.Join(folder, "cat.png")
	a.openFile(cat, false)
	if a.shell().blocked != cat {
		t.Fatalf("the binary notice is not shown (blocked %q)", a.shell().blocked)
	}
	a.ts.Tap(gluttertest.ByText("Open With..."))
	a.pump()
	if got := a.pickerLabels(); !slices.Equal(got, []string{"Text Editor", "Image Preview", "Hex"}) {
		t.Fatalf("rows %v", got)
	}
	a.pickRow("Image Preview")
	a.pump()
	if in, ok := a.activeInput().(*editorgroup.FileEditorInput); !ok || in.Editor() != imageEditor || a.shell().blocked != "" {
		t.Errorf("after Image Preview: %#v, blocked %q", a.activeInput(), a.shell().blocked)
	}
}

func TestAFileEditorIsSplitWithItsState(t *testing.T) {
	a, log, folder := viewersApp(t, nil)
	a.openFile(filepath.Join(folder, "cat.png"), false)
	a.run("workbench.action.splitEditor")
	for range 3 {
		a.pump()
	}
	if got := log.openedList(); !slices.Equal(got, []string{imageEditor + " cat.png", imageEditor + " cat.png"}) {
		t.Fatalf("opened %v", got)
	}
	log.mu.Lock()
	second := string(log.states[1])
	log.mu.Unlock()
	if second != `{"Zoom":2}` {
		t.Errorf("the copy started with %s", second)
	}
}

func TestATurnedOffFileEditorFallsBackToText(t *testing.T) {
	a, _, folder := viewersApp(t, nil)
	notes := filepath.Join(folder, "notes.txt")
	a.openFile(notes, false)
	waitForRequirements(t, a, func() bool { _, ok := a.activeInput().(*editorgroup.TextInput); return ok })
	a.run("workbench.action.reopenWithEditor")
	a.pickRow("Hex")
	a.pump()
	if err := a.win.setExtensionEnabled(viewersID, false); err != nil {
		t.Fatal(err)
	}
	waitForRequirements(t, a, func() bool { _, ok := a.activeInput().(*editorgroup.TextInput); return ok })
	if _, ok := a.activeInput().(*editorgroup.TextInput); !ok || len(a.tabNames()) != 1 {
		t.Errorf("after turning Viewers off: %#v, tabs %v", a.activeInput(), a.tabNames())
	}
	// a PNG opens as text now: the binary notice
	a.openFile(filepath.Join(folder, "cat.png"), false)
	if _, ok := a.activeInput().(*editorgroup.FileEditorInput); ok {
		t.Error("a PNG opened in a file editor of a turned-off extension")
	}
}

func TestAFileEditorComesBackAfterARestart(t *testing.T) {
	env := newStateEnv(t, nil)
	log := &viewerLog{}
	cat := filepath.Join(env.root, "cat.png")
	if err := os.WriteFile(cat, pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := ProcessOptions{Extensions: []extension.Factory{viewersFactory(log)}}
	first := env.startWith(env.root, opts)
	first.shell().openPath(widgets.WidgetsBindingOf(first.shell().Context()), nil, cat, false)
	first.pump()
	first.pump()
	first.close()
	second := env.startWith(env.root, opts)
	second.pump()
	in, ok := second.shell().activeGroup().Model.ActiveEditor().(*editorgroup.FileEditorInput)
	if !ok || in.Path() != cat || in.Editor() != imageEditor {
		t.Fatalf("the tab did not come back: %#v", second.shell().activeGroup().Model.ActiveEditor())
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if len(log.states) != 2 || string(log.states[1]) != `{"Zoom":2}` {
		t.Errorf("states %s", log.states)
	}
}
