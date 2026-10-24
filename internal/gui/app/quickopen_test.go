package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var quickOpenFiles = []string{
	"main.go", "go.mod", "internal/server/server.go", "internal/server/util.go",
	"docs/guide.md", "docs/more/deep.md", "node_modules/pkg/index.js", "build/out.o", ".gitignore",
}

// newQuickOpenApp is a window over a folder with a .gitignore that hides build/.
func newQuickOpenApp(t *testing.T, open ...string) *explorerApp {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, quickOpenFiles...)
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	win := newTestWindow(t, root)
	a := &explorerApp{t: t, win: win, root: win.Folder}
	var docs []*document.Document
	for _, p := range open {
		doc, err := win.Documents.OpenFile(context.Background(), filepath.Join(root, p), document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	a.ts = testutil.New(t, ui.Size{Width: 900, Height: 600})
	a.ts.PumpWidget(newAppDocs(context.Background(), win, docs, nil, ""))
	a.pump()
	return a
}

func (a *explorerApp) chord(label string) {
	a.t.Helper()
	sendChord(a.ts, chordOf(a.t, label))
	a.pump()
}

// qoType types in the input of the quick input.
func (a *explorerApp) qoType(text string) {
	a.t.Helper()
	a.ts.EnterText(gluttertest.First(gluttertest.ByType[*widgets.EditableText]()), text)
	a.pump()
}

// qoLabels are the labels of the rows of the quick input.
func (a *explorerApp) qoLabels() []string {
	var out []string
	for _, r := range a.win.QuickInput.Picker().Rows() {
		out = append(out, r.Item.Label)
	}
	return out
}

func (a *explorerApp) qoWait(want ...string) {
	a.t.Helper()
	a.waitFor("the rows "+slicesString(want), func() bool { return slices.Equal(a.qoLabels(), want) })
}

func slicesString(s []string) string { return strings.Join(s, ", ") }

func sortedStrings(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

// activeName is the name of the active editor, and whether it is kept.
func (a *explorerApp) activeName() (string, bool) {
	g := a.shell().activeGroup()
	e := g.Model.ActiveEditor()
	if e == nil {
		return "", false
	}
	return e.Name(), g.Model.IsPinned(e)
}

func TestQuickOpenListsTheFilesOfTheFolder(t *testing.T) {
	a := newQuickOpenApp(t)
	// the walk of the folder runs in the background
	idx := a.win.FileIndex()
	a.waitFor("the walk", func() bool { return idx.Snapshot().Complete })
	if got := sortedStrings(idx.Snapshot().Files); !slices.Equal(got, []string{".gitignore", "docs/guide.md", "docs/more/deep.md", "go.mod", "internal/server/server.go", "internal/server/util.go", "main.go"}) {
		t.Fatalf("files %q (node_modules and the ignored build folder are left out)", got)
	}
	a.chord("ctrl+p")
	ctl := a.win.QuickInput
	if !ctl.Visible() || ctl.Provider() == nil || ctl.Provider().Info().Prefix != "" {
		t.Fatalf("Ctrl+P: visible %v provider %v", ctl.Visible(), ctl.Provider())
	}
	if got := ctl.Picker().Placeholder(); got != quickinput.FilesPlaceholder {
		t.Errorf("placeholder %q", got)
	}
	a.qoType("serv")
	a.qoWait("server.go", "util.go")
	rows := ctl.Picker().Rows()
	if rows[0].Separator != quickinput.FilesResultsSeparator || len(rows[0].Label) == 0 {
		t.Errorf("first row: separator %q highlights %v", rows[0].Separator, rows[0].Label)
	}
	if rows[0].Item.Description != "internal/server" {
		t.Errorf("description %q", rows[0].Item.Description)
	}
	if rows[0].Item.FileIcon == nil {
		t.Error("no file icon")
	}
	if !a.ts.Exists(gluttertest.ByText(quickinput.FilesResultsSeparator)) {
		t.Error("the heading is not drawn")
	}
	a.qoType("zzzzq")
	a.qoWait(quickinput.FilesNoResults)
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestQuickOpenOpensFilesAndRemembersThem(t *testing.T) {
	a := newQuickOpenApp(t, "main.go")
	a.waitFor("the walk", func() bool { return a.win.FileIndex().Snapshot().Complete })
	ctl := a.win.QuickInput
	focused := func() bool { e := a.shell().activeEditor(); return e != nil && e.view.Focused() }

	// Enter opens the file in the active group, kept (VS Code's default), and the editor has the focus
	a.chord("ctrl+p")
	a.qoType("util")
	a.qoWait("util.go")
	a.chord("enter")
	a.waitFor("the editor", func() bool { n, _ := a.activeName(); return n == "util.go" })
	if name, kept := a.activeName(); name != "util.go" || !kept {
		t.Errorf("opened %q kept %v", name, kept)
	}
	if ctl.Visible() {
		t.Error("the quick input stays open")
	}
	a.waitFor("the focus in the editor", focused)
	if a.shell().group.grid.Count() != 1 {
		t.Errorf("%d groups, want 1", a.shell().group.grid.Count())
	}

	// the recently opened files come first, the one opened last at the top
	a.chord("ctrl+p")
	a.waitFor("the recent files", func() bool { return slices.Equal(a.qoLabels(), []string{"util.go", "main.go"}) })
	rows := ctl.Picker().Rows()
	if rows[0].Separator != quickinput.FilesRecentSeparator {
		t.Errorf("separator %q", rows[0].Separator)
	}
	// choosing a file that is open already activates its editor and opens no second one
	a.ts.EnterText(gluttertest.First(gluttertest.ByType[*widgets.EditableText]()), "main")
	a.pump()
	a.qoWait("main.go")
	a.chord("enter")
	a.waitFor("main.go", func() bool { n, _ := a.activeName(); return n == "main.go" })
	if n := a.shell().activeGroup().Model.Count(); n != 2 {
		t.Errorf("%d editors, want 2", n)
	}
	a.waitFor("the focus in the editor", focused)
	if got := a.win.Recent.List(); got[0] != a.path("main.go") || len(got) != 2 {
		t.Errorf("recent %q", got)
	}
}

func TestQuickOpenPreviewSetting(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		keys     string
		kept     bool
		groups   int
	}{
		{"default: kept", nil, "enter", true, 1},
		{"preview from quick open", map[string]any{"workbench.editor.enablePreviewFromQuickOpen": true}, "enter", false, 1},
		{"preview setting off wins", map[string]any{"workbench.editor.enablePreviewFromQuickOpen": true, "workbench.editor.enablePreview": false}, "enter", true, 1},
		{"ctrl keeps a preview", map[string]any{"workbench.editor.enablePreviewFromQuickOpen": true}, "ctrl+enter", true, 1},
		{"ctrl opens to the side by default", nil, "ctrl+enter", true, 2},
		{"alt opens to the side", map[string]any{"workbench.editor.enablePreviewFromQuickOpen": true}, "alt+enter", false, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newQuickOpenApp(t, "main.go")
			for k, v := range tc.settings {
				a.setting(k, v)
			}
			a.waitFor("the walk", func() bool { return a.win.FileIndex().Snapshot().Complete })
			a.chord("ctrl+p")
			a.qoType("guide")
			a.qoWait("guide.md")
			a.chord(tc.keys)
			a.waitFor("guide.md", func() bool { n, _ := a.activeName(); return n == "guide.md" })
			name, kept := a.activeName()
			if name != "guide.md" || kept != tc.kept {
				t.Errorf("opened %q kept %v, want kept %v", name, kept, tc.kept)
			}
			if got := a.shell().group.grid.Count(); got != tc.groups {
				t.Errorf("%d groups, want %d", got, tc.groups)
			}
			if tc.groups == 2 && a.shell().activeGroup() == a.shell().groupAt(1) {
				t.Error("the file is not in the group on the side")
			}
		})
	}
}

func TestQuickOpenRevealsTheLine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "poem.txt"), []byte("one\ntwo three\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	win := newTestWindow(t, root)
	a := &explorerApp{t: t, win: win, root: win.Folder}
	a.ts = testutil.New(t, ui.Size{Width: 900, Height: 600})
	a.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	a.pump()
	a.waitFor("the walk", func() bool { return win.FileIndex().Snapshot().Complete })
	a.chord("ctrl+p")
	a.qoType("poem.txt:2:5")
	a.qoWait("poem.txt")
	a.chord("enter")
	a.waitFor("the editor", func() bool { return a.shell().activeEditor() != nil })
	a.pump()
	sels := a.shell().activeEditor().view.Cursors().Selections()
	if len(sels) != 1 || sels[0].Active.Line != 1 || sels[0].Active.Col != 4 {
		t.Errorf("cursor %+v, want line 2 column 5", sels)
	}
}

func TestQuickOpenWithArguments(t *testing.T) {
	a := newQuickOpenApp(t)
	a.waitFor("the walk", func() bool { return a.win.FileIndex().Snapshot().Complete })
	if err := a.run("workbench.action.quickOpen", map[string]any{"value": "guide"}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	a.qoWait("guide.md")
	if v := a.win.QuickInput.Picker().Value(); v != "guide" {
		t.Errorf("value %q", v)
	}
	// the help lists the mode with its key
	if err := a.run("workbench.action.closeQuickOpen", nil); err != nil {
		t.Fatal(err)
	}
	a.chord("ctrl+p")
	a.qoType("?")
	found := false
	for _, r := range a.win.QuickInput.Picker().Rows() {
		if r.Item.Description == "Go to File" && r.Item.Keybinding == "Ctrl+P" {
			found = true
		}
	}
	if !found {
		t.Errorf("the help does not list Go to File: %v", a.qoLabels())
	}
}

func TestQuickOpenWithoutAFolderListsTheRecentFiles(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	win := newTestWindow(t, "")
	win.Recent.Add(file)
	ts := testutil.New(t, ui.Size{Width: 900, Height: 600})
	ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	a := &explorerApp{t: t, win: win, ts: ts}
	a.pump()
	if win.FileIndex() != nil {
		t.Fatal("a window without a folder has a list of files")
	}
	a.chord("ctrl+p")
	a.qoWait("notes.txt")
	a.qoType("notes")
	a.qoWait("notes.txt")
	a.qoType("nothing")
	a.qoWait(quickinput.FilesNoResults)
}

func TestQuickOpenFollowsTheFolder(t *testing.T) {
	a := newQuickOpenApp(t)
	idx := a.win.FileIndex()
	a.waitFor("the walk", func() bool { return idx.Snapshot().Complete })
	// the events of the watcher keep the list current
	if err := os.WriteFile(a.path("docs/fresh.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.win.onFileEvents(context.Background(), []workspace.Event{{Kind: workspace.EventCreated, Path: a.path("docs/fresh.md")}})
	a.chord("ctrl+p")
	a.qoType("fresh")
	a.qoWait("fresh.md")
	// a new file that appears while the picker is open shows up without typing
	if err := os.WriteFile(a.path("docs/fresh2.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.win.onFileEvents(context.Background(), []workspace.Event{{Kind: workspace.EventCreated, Path: a.path("docs/fresh2.md")}})
	a.qoWait("fresh.md", "fresh2.md")
	a.chord("escape")

	// the settings of the file search make the list again
	a.setting("search.exclude", map[string]any{"**/docs": true})
	a.waitFor("the new list", func() bool {
		snap := idx.Snapshot()
		return snap.Complete && !slices.Contains(snap.Files, "docs/guide.md") && slices.Contains(snap.Files, "main.go")
	})
	a.setting("search.useIgnoreFiles", false)
	a.setting("search.exclude", map[string]any{})
	a.waitFor("the ignored folder", func() bool { return slices.Contains(idx.Snapshot().Files, "build/out.o") })

	// another folder: a new list, and the old walk is stopped
	other := t.TempDir()
	writeTree(t, other, "alpha.txt")
	if err := a.win.SetFolder(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	a.pump()
	next := a.win.FileIndex()
	if next == idx || next == nil {
		t.Fatalf("index %p after SetFolder, was %p", next, idx)
	}
	a.waitFor("the new folder", func() bool { return slices.Equal(next.Snapshot().Files, []string{"alpha.txt"}) })
	stopped := make(chan struct{})
	go func() { idx.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Error("the walk of the old folder is not stopped")
	}
	// the window going away stops the walk of the new one (the shell lets go of the quick input)
	a.shell().disposeQuickInput()
	if a.shell().fileIndex != nil {
		t.Error("the shell keeps the list after it was disposed")
	}
	done := make(chan struct{})
	go func() { next.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("the walk is not stopped when the window goes")
	}
}

func TestQuickOpenCommandsAndKeys(t *testing.T) {
	win := newTestWindow(t, "")
	if m, ok := win.Commands.Lookup("workbench.action.quickOpen"); !ok || m.Title != "Go to File..." {
		t.Errorf("command %+v %v", m, ok)
	}
	items := win.Process.Menus.Items(command.MenubarGoMenu, win.Keys)
	found := false
	for _, it := range items {
		if it.Command == "workbench.action.quickOpen" && it.Title == "Go to File..." {
			found = true
		}
	}
	if !found {
		t.Error("the Go menu has no Go to File row")
	}
}

func TestQuickOpenRecentFilesFollowFileOperations(t *testing.T) {
	a := newQuickOpenApp(t)
	a.win.Recent.Add(a.path("docs/guide.md"))
	a.win.Recent.Add(a.path("main.go"))
	a.win.Recent.Add(a.path("internal/server/util.go"))
	binding := widgets.WidgetsBindingOf(a.ts.Element(gluttertest.ByType[*shell]()))
	a.shell().followFileOp(binding, workspace.OpResult{
		Moves:   []workspace.PathMove{{Old: a.path("docs"), New: a.path("manual")}},
		Removed: []string{a.path("main.go")},
	})
	want := []string{a.path("internal/server/util.go"), a.path("manual/guide.md")}
	if got := a.win.Recent.List(); !slices.Equal(got, want) {
		t.Errorf("recent %q, want %q", got, want)
	}
}
