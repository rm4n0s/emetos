package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
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
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/explorer"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// explorerApp is a window over a folder, with the Explorer in its side bar.
type explorerApp struct {
	t    *testing.T
	win  *Workspace
	ts   *gluttertest.Tester
	root string
}

// folderEntries are the files and folders of the folder of the tests: a name that ends in "/" is a folder.
func writeTree(t *testing.T, root string, entries ...string) {
	t.Helper()
	for _, e := range entries {
		p := filepath.Join(root, filepath.FromSlash(e))
		if strings.HasSuffix(e, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package x\n// "+e+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// newExplorerApp makes the folder and a window over it, shown at a size, with the files in open (paths
// relative to the folder) opened in order.
func newExplorerApp(t *testing.T, size ui.Size, entries []string, open ...string) *explorerApp {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, entries...)
	win := newTestWorkspace(t, root)
	var docs []*document.Document
	for _, p := range open {
		doc, err := win.Documents.OpenFile(context.Background(), filepath.Join(root, p), document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	a := &explorerApp{t: t, win: win, root: win.Folder}
	a.ts = testutil.New(t, size)
	a.ts.PumpWidget(newAppDocs(context.Background(), win, docs, nil, ""))
	a.pump()
	return a
}

// pump runs frames and the work that the background reads hand to the UI thread, until nothing changes.
func (a *explorerApp) pump() {
	for range 6 {
		a.ts.Pump(20 * time.Millisecond)
		time.Sleep(2 * time.Millisecond)
	}
}

// replaceFolder opens a folder in the tab of the window, as --reuse-window does (the default setting would open a new
// tab, SPEC §10.14): the editors close first, with the questions about unsaved changes.
func (a *explorerApp) replaceFolder(path string) {
	a.win.Process.Workspaces.OpenFolder(a.win, path, false, true, nil)
}

func (a *explorerApp) shell() *shellState {
	return a.ts.Element(gluttertest.ByType[*shell]()).(*widgets.StatefulElement).State().(*shellState)
}

// rows are the paths of the rows of the tree relative to the folder, a folder with a slash at the end and the
// selected one after a star.
func (a *explorerApp) rows() []string {
	s := a.shell()
	if s.explorer.view == nil {
		return nil
	}
	tree := s.explorer.view.Tree()
	var out []string
	for _, n := range tree.Rows() {
		rel, _ := filepath.Rel(a.root, n.Key)
		if n.Expandable {
			rel += "/"
		}
		if tree.IsSelected(n.Key) {
			rel = "*" + rel
		}
		out = append(out, rel)
	}
	return out
}

func (a *explorerApp) run(id string, args any) error {
	return a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, id, args)
}

func (a *explorerApp) openFile(rel string) {
	a.t.Helper()
	if err := a.run("dev.openFile", map[string]any{"path": filepath.Join(a.root, rel)}); err != nil {
		a.t.Fatal(err)
	}
	a.pump()
}

func (a *explorerApp) setting(key string, value any) {
	a.t.Helper()
	if err := a.win.Settings.Update(context.Background(), key, value, settings.TargetUser); err != nil {
		a.t.Fatal(err)
	}
	a.pump()
}

func (a *explorerApp) path(rel string) string { return filepath.Join(a.root, filepath.FromSlash(rel)) }

// selected is the path of the selected row relative to the folder.
func (a *explorerApp) selected() string {
	rel, _ := filepath.Rel(a.root, a.shell().explorer.view.SelectedPath())
	if rel == "." {
		return ""
	}
	return rel
}

// inTree finds a widget below the tree of the folder.
func inTree(f gluttertest.Finder) gluttertest.Finder {
	return gluttertest.Descendant(gluttertest.ByKey(explorer.TreeKey), f)
}

// tapRow presses a row of the tree by its label.
func (a *explorerApp) tapRow(label string) {
	a.t.Helper()
	a.ts.Tap(inTree(gluttertest.ByText(label)))
	a.ts.Pump(time.Millisecond)
}

var projectFiles = []string{"main.go", "go.mod", "internal/server/server.go", "internal/server/util.go", "docs/guide.md", "docs/more/deep.md"}

func TestExplorerRevealsTheActiveFile(t *testing.T) {
	a := newExplorerApp(t, ui.Size{Width: 900, Height: 600}, []string{"main.go", "internal/server/server.go", "internal/server/util.go", "docs/guide.md"}, "main.go")
	if got, want := a.rows(), []string{"docs/", "internal/", "*main.go"}; !slices.Equal(got, want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	a.openFile("internal/server/server.go")
	want := []string{"docs/", "internal/", "internal/server/", "*internal/server/server.go", "internal/server/util.go", "main.go"}
	if got := a.rows(); !slices.Equal(got, want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	a.openFile("main.go")
	want = []string{"docs/", "internal/", "internal/server/", "internal/server/server.go", "internal/server/util.go", "*main.go"}
	if got := a.rows(); !slices.Equal(got, want) {
		t.Errorf("rows %v, want %v (the folders stay open)", got, want)
	}
}

func TestExplorerAutoRevealSetting(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		revealed bool
		scroll   bool
	}{
		{"true reveals and scrolls", true, true, true},
		{"false does nothing", false, false, false},
		{"focusNoScroll reveals without scrolling", "focusNoScroll", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newExplorerApp(t, ui.Size{Width: 900, Height: 600}, projectFiles, "main.go")
			a.setting("explorer.autoReveal", tt.value)
			a.openFile("internal/server/server.go")
			got := a.selected()
			if tt.revealed && got != "internal/server/server.go" {
				t.Errorf("selected %q, want server.go revealed", got)
			}
			if !tt.revealed && got != "main.go" {
				t.Errorf("selected %q, want main.go still selected (rows %v)", got, a.rows())
			}
			if tt.revealed != a.shell().explorer.view.Tree().Has(a.path("internal/server/server.go")) {
				t.Errorf("rows %v", a.rows())
			}
		})
	}
}

func TestExplorerRevealsWhenItComesOnScreen(t *testing.T) {
	a := newExplorerApp(t, ui.Size{Width: 900, Height: 600}, projectFiles, "main.go")
	a.win.Layout.SetSideBarVisible(false)
	a.pump()
	a.openFile("internal/server/server.go")
	if a.shell().explorer.view.Tree().Has(a.path("internal/server/server.go")) {
		t.Fatal("the file was revealed in a tree nobody sees")
	}
	if a.win.Keys.Get("explorerViewletVisible") != false {
		t.Errorf("explorerViewletVisible = %v", a.win.Keys.Get("explorerViewletVisible"))
	}
	a.win.Layout.SetSideBarVisible(true)
	a.pump()
	if got := a.selected(); got != "internal/server/server.go" {
		t.Errorf("selected %q once the side bar is back, want server.go", got)
	}
	if a.win.Keys.Get("explorerViewletVisible") != true || a.win.Keys.Get("foldersViewVisible") != true {
		t.Errorf("explorerViewletVisible = %v, foldersViewVisible = %v", a.win.Keys.Get("explorerViewletVisible"), a.win.Keys.Get("foldersViewVisible"))
	}
}

func TestExplorerClicksOpenFiles(t *testing.T) {
	a := newExplorerApp(t, ui.Size{Width: 900, Height: 600}, projectFiles, "go.mod")
	group := func() *editorgroup.Group { return a.shell().activeGroup() }
	// a click opens a preview editor, which the next click replaces
	a.tapRow("main.go")
	a.pump()
	active := group().Model.ActiveEditor()
	if name := active.Name(); name != "main.go" || group().Model.IsPinned(active) {
		t.Fatalf("a click opened %s (pinned %v), want a preview of main.go", name, group().Model.IsPinned(active))
	}
	// a double click keeps it
	a.ts.Pump(500 * time.Millisecond)
	a.tapRow("main.go")
	a.tapRow("main.go")
	a.pump()
	if active = group().Model.ActiveEditor(); !group().Model.IsPinned(active) {
		t.Error("a double click did not keep the editor")
	}
	// a click on a folder opens it and not an editor
	before := group().Model.Count()
	a.ts.Pump(500 * time.Millisecond)
	a.tapRow("docs")
	a.pump()
	if group().Model.Count() != before {
		t.Error("a click on a folder opened an editor")
	}
	if got := a.rows(); !slices.Contains(got, "docs/guide.md") {
		t.Errorf("docs did not open: %v", got)
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestExplorerCommands(t *testing.T) {
	a := newExplorerApp(t, ui.Size{Width: 900, Height: 600}, projectFiles, "main.go", "internal/server/server.go")
	view := func() *explorer.View { return a.shell().explorer.view }
	topLevel := []string{"docs/", "internal/", "go.mod", "*main.go"}
	run := func(id string, args any) {
		t.Helper()
		if err := a.run(id, args); err != nil {
			t.Fatal(err)
		}
		a.pump()
	}
	t.Run("collapse folders", func(t *testing.T) {
		run("workbench.files.action.collapseExplorerFolders", nil)
		if got := a.rows(); len(got) != 4 {
			t.Errorf("rows %v, want only the top level", got)
		}
	})
	t.Run("reveal the active file", func(t *testing.T) {
		a.openFile("internal/server/server.go")
		run("workbench.files.action.collapseExplorerFolders", nil)
		run("workbench.files.action.showActiveFileInExplorer", nil)
		if got := a.selected(); got != "internal/server/server.go" {
			t.Errorf("selected %q, want server.go", got)
		}
		if !view().Controller().HasFocus() {
			t.Error("the tree has not the focus")
		}
	})
	t.Run("reveal a tab", func(t *testing.T) {
		run("workbench.files.action.collapseExplorerFolders", nil)
		// the context the menu of the first tab gives
		run("revealInExplorer", tabContext(a.shell().activeGroup(), 0))
		if got := a.selected(); got != "main.go" {
			t.Errorf("selected %q, want main.go", got)
		}
		if got := a.rows(); len(got) != len(topLevel) {
			t.Errorf("rows %v", got)
		}
	})
	t.Run("reveal a path", func(t *testing.T) {
		run("revealInExplorer", a.path("docs/more/deep.md"))
		if got := a.selected(); got != "docs/more/deep.md" {
			t.Errorf("selected %q", got)
		}
	})
	t.Run("a file outside the folder is not revealed", func(t *testing.T) {
		before := a.selected()
		run("revealInExplorer", filepath.Join(t.TempDir(), "outside.go"))
		if got := a.selected(); got != before {
			t.Errorf("selected %q, was %q", got, before)
		}
	})
	t.Run("refresh reads the folders again", func(t *testing.T) {
		if err := os.WriteFile(a.path("new.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		run("workbench.files.action.refreshFilesExplorer", nil)
		a.waitFor("the new file", func() bool { return slices.Contains(a.rows(), "new.txt") || slices.Contains(a.rows(), "*new.txt") })
	})
	t.Run("show the explorer", func(t *testing.T) {
		a.win.Layout.SetSideBarVisible(false)
		a.pump()
		run("workbench.view.explorer", nil)
		if !a.win.Layout.State().SideBarVisible {
			t.Error("the side bar is hidden")
		}
		if !view().Controller().HasFocus() {
			t.Error("the tree has not the focus")
		}
		if a.win.Keys.Get("explorerViewletFocus") != true || a.win.Keys.Get("filesExplorerFocus") != true {
			t.Errorf("explorerViewletFocus = %v, filesExplorerFocus = %v", a.win.Keys.Get("explorerViewletFocus"), a.win.Keys.Get("filesExplorerFocus"))
		}
	})
	t.Run("the other focus commands", func(t *testing.T) {
		for _, id := range []string{"workbench.explorer.fileView.focus", "workbench.files.action.focusFilesExplorer"} {
			a.win.Layout.SetSideBarVisible(false)
			a.pump()
			run(id, nil)
			if !a.win.Layout.State().SideBarVisible || !view().Controller().HasFocus() {
				t.Errorf("%s did not show and focus the tree", id)
			}
		}
	})
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// waitFor pumps until the condition holds.
func (a *explorerApp) waitFor(what string, cond func() bool) {
	a.t.Helper()
	for range 200 {
		if cond() {
			return
		}
		a.pump()
	}
	a.t.Fatalf("timed out waiting for %s; rows %v", what, a.rows())
}

func TestExplorerOpenEditors(t *testing.T) {
	a := newExplorerApp(t, ui.Size{Width: 900, Height: 600}, projectFiles, "main.go", "go.mod")
	list := gluttertest.ByKey(explorer.OpenEditorsKey)
	if a.ts.Exists(list) {
		t.Fatal("the Open Editors show before they were asked for, as in VS Code")
	}
	run := func(id string) {
		t.Helper()
		if err := a.run(id, nil); err != nil {
			t.Fatal(err)
		}
		a.pump()
	}
	run("workbench.files.action.focusOpenEditorsView")
	if !a.ts.Exists(list) {
		t.Fatal("the command did not show the Open Editors")
	}
	if !a.shell().explorer.editors.Controller().HasFocus() || a.win.Keys.Get("openEditorsFocus") != true {
		t.Errorf("the list has not the focus (openEditorsFocus = %v)", a.win.Keys.Get("openEditorsFocus"))
	}
	for _, name := range []string{"main.go", "go.mod"} {
		if !a.ts.Exists(gluttertest.Descendant(list, gluttertest.ByText(name))) {
			t.Errorf("no row for %s", name)
		}
	}
	// a click on a row makes its editor the active one
	a.ts.Tap(gluttertest.Descendant(list, gluttertest.ByText("main.go")))
	a.pump()
	if got := a.shell().activeGroup().Model.ActiveEditor().Name(); got != "main.go" {
		t.Errorf("active editor %s, want main.go", got)
	}
	// a second group shows the groups
	run("workbench.action.splitEditorRight")
	if !a.ts.Exists(gluttertest.Descendant(list, gluttertest.ByText("GROUP 1"))) || !a.ts.Exists(gluttertest.Descendant(list, gluttertest.ByText("GROUP 2"))) {
		t.Error("the groups have no rows")
	}
	// the view is hidden again and the focus command shows it
	a.win.Views.SetHidden(command.ViewOpenEditors, true)
	a.pump()
	if a.ts.Exists(list) {
		t.Error("the Open Editors stay when hidden")
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// TestExplorerOpenEditorsStrikesThroughADeletedFile checks that the row of an editor whose file another program
// deleted is struck through (in the error color unless workbench.editor.decorations.colors is off), and that the tree
// of the folder and the editors follow the same event (step 7.4).
func TestExplorerOpenEditorsStrikesThroughADeletedFile(t *testing.T) {
	a := newExplorerApp(t, ui.Size{Width: 900, Height: 600}, projectFiles, "main.go", "go.mod")
	a.win.Views.SetHidden(command.ViewOpenEditors, false)
	a.pump()
	list := gluttertest.ByKey(explorer.OpenEditorsKey)
	rowStyle := func(name string) (struck bool, errorColor bool) {
		t.Helper()
		style := a.ts.Widget(gluttertest.Descendant(list, gluttertest.ByText(name))).(*widgets.Text).Style()
		deco, _ := style.Decoration.Get()
		color, _ := style.Color.Get()
		return deco.Contains(ui.TextDecorationLineThrough), color == kit.ThemeOf(a.ts.Element(list)).ListErrorForeground
	}
	if struck, _ := rowStyle("go.mod"); struck {
		t.Fatal("a file that is there is struck through")
	}
	gone := filepath.Join(a.root, "go.mod")
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	a.win.onFileEvents(context.Background(), []workspace.Event{{Kind: workspace.EventDeleted, Path: gone}})
	a.waitFor("the row to be struck through", func() bool {
		struck, _ := rowStyle("go.mod")
		return struck
	})
	if struck, colored := rowStyle("go.mod"); !struck || !colored {
		t.Errorf("go.mod: struck through %v, error color %v", struck, colored)
	}
	if struck, colored := rowStyle("main.go"); struck || colored {
		t.Errorf("main.go: struck through %v, error color %v", struck, colored)
	}
	// the tree stopped showing the file at the same event, and the editor is still there
	if slices.Contains(a.rows(), "go.mod") || slices.Contains(a.rows(), "*go.mod") {
		t.Errorf("the tree still has go.mod: %v", a.rows())
	}
	// with the colors off the row is struck through only
	if err := a.win.Settings.Update(context.Background(), "workbench.editor.decorations.colors", false, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the colors to go", func() bool {
		_, colored := rowStyle("go.mod")
		return !colored
	})
	if struck, _ := rowStyle("go.mod"); !struck {
		t.Error("the row is not struck through any more")
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestExplorerOpenEditorsCloseButtonAndSettings(t *testing.T) {
	a := newExplorerApp(t, ui.Size{Width: 900, Height: 600}, projectFiles, "main.go", "go.mod")
	a.win.Views.SetHidden(command.ViewOpenEditors, false)
	a.pump()
	rows := func() []string {
		var out []string
		for _, r := range a.shell().explorer.editors.List().Rows() {
			out = append(out, r.Label)
		}
		return out
	}
	if want := []string{"main.go", "go.mod"}; !slices.Equal(rows(), want) {
		t.Fatalf("rows %v, want %v", rows(), want)
	}
	a.setting("explorer.openEditors.sortOrder", "alphabetical")
	if want := []string{"go.mod", "main.go"}; !slices.Equal(rows(), want) {
		t.Errorf("alphabetical: %v, want %v", rows(), want)
	}
	// the height follows explorer.openEditors.visible
	if got := a.shell().explorer.editors.BodyHeight(); got != 44 {
		t.Errorf("height %v, want 44", got)
	}
	// the close button of the selected row closes the editor
	a.shell().explorer.editors.List().SetSelected(a.shell().explorer.editors.List().Rows()[1].Key)
	a.shell().explorer.editors.Refresh()
	a.pump()
	g := a.shell().activeGroup()
	active := g.Model.ActiveEditor()
	n := g.Model.Count()
	closeAt := gluttertest.Descendant(gluttertest.ByKey(explorer.OpenEditorsKey), gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconClose)))
	if !a.ts.Exists(closeAt) {
		t.Fatal("the selected row has no close button")
	}
	a.ts.Tap(closeAt)
	a.pump()
	if g.Model.Count() != n-1 || g.Model.Contains(active) {
		t.Errorf("the editor %s was not closed (%d editors)", active.Name(), g.Model.Count())
	}
}

// The empty Explorer warns that opening a folder closes the open editors only when the folder would replace the tab
// (window.openFoldersInNewWindow "off"); otherwise the folder opens in a new tab and the editors stay (step 9b.3).
func TestExplorerWelcomeWarnsOnlyWhenTheFolderReplacesTheTab(t *testing.T) {
	cases := []struct {
		setting string
		warns   bool
	}{
		{"default", false},
		{"on", false},
		{"off", true},
	}
	for _, tc := range cases {
		t.Run(tc.setting, func(t *testing.T) {
			paths := testutil.Isolate(t)
			testutil.Settings(t, paths, map[string]any{"files.hotExit": "off", "window.menuBarVisibility": "classic",
				"window.openFoldersInNewWindow": tc.setting})
			win, err := NewWorkspace(context.Background(), newTestProcess(ProcessOptions{Paths: paths}), "")
			if err != nil {
				t.Fatalf("NewWorkspace: %v", err)
			}
			doc, err := win.Documents.OpenFile(context.Background(), sampleFile(t), document.NewOptions())
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			ts := testutil.New(t, ui.Size{Width: 900, Height: 600})
			ts.PumpWidget(newAppDocs(context.Background(), win, []*document.Document{doc}, nil, ""))
			for range 4 {
				ts.Pump(20 * time.Millisecond)
			}
			if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText(explorer.WelcomeText)); err != nil {
				t.Fatal(err)
			}
			if got := ts.Exists(gluttertest.ByText(explorer.WelcomeEditorsText)); got != tc.warns {
				t.Errorf("the warning is shown: %v, want %v", got, tc.warns)
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestExplorerWelcomeAndOpenFolder(t *testing.T) {
	win := newTestWorkspace(t, "")
	ts := testutil.New(t, ui.Size{Width: 900, Height: 600})
	fd := ts.FakeDesktop()
	ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	for range 4 {
		ts.Pump(20 * time.Millisecond)
	}
	for _, text := range []string{"NO FOLDER OPENED", explorer.WelcomeText, explorer.WelcomeButton} {
		if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText(text)); err != nil {
			t.Error(err)
		}
	}
	if win.Keys.Get("workbenchState") != "empty" {
		t.Errorf("workbenchState = %v", win.Keys.Get("workbenchState"))
	}
	folder := t.TempDir()
	writeTree(t, folder, "a.go", "sub/b.go")
	pump := func() {
		for range 4 {
			ts.Pump(20 * time.Millisecond)
			time.Sleep(2 * time.Millisecond)
		}
	}
	// a dialog that is cancelled changes nothing
	ts.Tap(gluttertest.ByKey(explorer.OpenFolderKey))
	fd.WaitFor("the dialog", func() bool { return fd.PendingFileChoosers() == 1 })
	fd.CancelFileChooser()
	pump()
	if win.Folder != "" {
		t.Errorf("the folder is %q after a cancelled dialog", win.Folder)
	}
	// a folder that is chosen is opened in the window
	ts.Tap(gluttertest.ByKey(explorer.OpenFolderKey))
	fd.WaitFor("the dialog", func() bool { return fd.PendingFileChoosers() == 1 })
	fd.AnswerFileChooser(folder)
	fd.WaitFor("the folder", func() bool { return win.Folder == folder })
	pump()
	if win.Workspace == nil || win.Keys.Get("workbenchState") != "folder" {
		t.Errorf("workspace %v, workbenchState %v", win.Workspace, win.Keys.Get("workbenchState"))
	}
	// (the name of the folder is the label of its workspace tab too: the explorer is looked at on its own)
	side := gluttertest.ByKey(workbench.PartKey(workbench.PartSideBar))
	for _, text := range []string{strings.ToUpper(filepath.Base(folder)), "a.go", "sub"} {
		if err := gluttertest.FindsOneWidget(ts, gluttertest.Descendant(side, gluttertest.ByText(text))); err != nil {
			t.Error(err)
		}
	}
	if ts.Exists(gluttertest.ByText(explorer.WelcomeText)) {
		t.Error("the welcome stays")
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestChooseFolderFailureHasItsOwnTag(t *testing.T) {
	win := newTestWorkspace(t, "")
	ts := testutil.New(t, ui.Size{Width: 400, Height: 300})
	ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	ts.Pump(20 * time.Millisecond)
	// the dialog waits for the user, so it is refused on the UI thread, which is where this test runs
	_, err := chooseFolder(context.Background(), ts.Element(gluttertest.ByType[*shell]()), "")
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("chooseFolder.OpenFolderDialogFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	if appErr.OriginalErr == nil {
		t.Error("the error of the dialog is gone")
	}
	// the window tells the user
	s := (&explorerApp{t: t, win: win, ts: ts}).shell()
	s.folderChosen("", err)
	if !notified(win, "Cannot show the dialog to open a folder") {
		t.Errorf("no notification: %v", win.Notify.Items())
	}
}

// notified tells whether a notification of the process has a text in its message.
func notified(win *Workspace, text string) bool {
	for _, n := range win.Notify.Items() {
		if strings.Contains(n.Message, text) {
			return true
		}
	}
	return false
}

func TestOpenFolder(t *testing.T) {
	a := newExplorerApp(t, ui.Size{Width: 900, Height: 600}, projectFiles, "main.go", "go.mod")
	other := t.TempDir()
	writeTree(t, other, "x.go", ".emetos/settings.json")
	if err := os.WriteFile(filepath.Join(other, ".emetos", "settings.json"), []byte(`{"explorer.compactFolders": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := a.shell()
	doc, ok := a.win.Documents.Get(document.FileURI(a.path("main.go")))
	if !ok {
		t.Fatal("main.go is not open")
	}
	// a document with unsaved changes keeps the window where it is
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "x"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	a.replaceFolder(other)
	a.pump()
	if !a.win.Dialogs.IsShowing() || !a.ts.Exists(gluttertest.ByText("Do you want to save the changes you made to main.go?")) {
		t.Fatal("no dialog asks about the changes")
	}
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText("Cancel")))
	a.pump()
	if a.win.Folder != a.root || a.win.Dialogs.IsShowing() || s.editorCount() != 2 {
		t.Errorf("the folder is %q after Cancel, %d editors", a.win.Folder, s.editorCount())
	}
	// without changes the editors close and the folder changes
	if _, err := doc.Undo(); err != nil {
		t.Fatal(err)
	}
	if doc.Dirty() {
		t.Fatal("the document is still dirty")
	}
	changes := 0
	stop := a.win.OnFolderChange(func() { changes++ })
	defer stop()
	a.replaceFolder(other)
	a.pump()
	want, _ := filepath.EvalSymlinks(other)
	if got, _ := filepath.EvalSymlinks(a.win.Folder); got != want || changes != 1 {
		t.Fatalf("the folder is %q (%d changes), want %q", a.win.Folder, changes, other)
	}
	if n := a.shell().editorCount(); n != 0 {
		t.Errorf("%d editors stay open", n)
	}
	a.root = a.win.Folder
	if got := a.rows(); !slices.Equal(got, []string{".emetos/", "x.go"}) {
		t.Errorf("rows %v, want .emetos and x.go", got)
	}
	if got := settings.Get[bool](a.win.Settings, "explorer.compactFolders", ""); got {
		t.Error("the settings of the new folder are not in use")
	}
	// the same folder again is nothing to do
	a.replaceFolder(other)
	if changes != 1 {
		t.Errorf("%d changes after opening the same folder", changes)
	}
	// Close Folder gives the window no folder
	if err := a.run("workbench.action.closeFolder", nil); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if a.win.Folder != "" || a.win.Workspace != nil || a.win.Keys.Get("workbenchState") != "empty" {
		t.Errorf("folder %q, workspace %v, workbenchState %v", a.win.Folder, a.win.Workspace, a.win.Keys.Get("workbenchState"))
	}
	if !a.ts.Exists(gluttertest.ByText(explorer.WelcomeText)) {
		t.Error("the welcome does not show")
	}
	if got := settings.Get[bool](a.win.Settings, "explorer.compactFolders", ""); !got {
		t.Error("the settings of the closed folder are still in use")
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestSetFolderFailureKeepsTheFolder(t *testing.T) {
	win := newTestWorkspace(t, t.TempDir())
	before := win.Folder
	err := win.SetFolder(context.Background(), filepath.Join(t.TempDir(), "missing"))
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("Workspace.SetFolder->Open.WorkspaceFolderNotFound") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	if win.Folder != before || win.Workspace == nil {
		t.Errorf("the folder is %q, workspace %v", win.Folder, win.Workspace)
	}
}

func TestFileIcons(t *testing.T) {
	win := newTestWorkspace(t, "")
	ctx := context.Background()
	if th := win.FileIcons(); th == nil || th.ID() != theme.DefaultFileIconTheme {
		t.Fatalf("the default theme is %v", th)
	}
	if err := win.Settings.Update(ctx, "workbench.iconTheme", nil, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if th := win.FileIcons(); th != nil {
		t.Errorf("null gives %v, want no icons", th)
	}
	if err := win.Settings.Update(ctx, "workbench.iconTheme", "no-such-theme", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if th := win.FileIcons(); th != nil {
			t.Errorf("an unknown theme gives %v", th)
		}
	}
	// and the user is told once
	var n int
	for _, item := range win.Notify.Items() {
		if strings.Contains(item.Message, "no-such-theme") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d notifications about the theme, want 1", n)
	}
}

func TestWindowStateKey(t *testing.T) {
	tests := []struct {
		name   string
		folder func(t *testing.T) string
		want   string
	}{
		{"a folder", func(t *testing.T) string { return t.TempDir() }, "folder"},
		{"no folder", func(*testing.T) string { return "" }, "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			win := newTestWorkspace(t, tt.folder(t))
			if got := win.Keys.Get("workbenchState"); got != tt.want {
				t.Errorf("workbenchState = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTheTabMenuRevealsInTheExplorer(t *testing.T) {
	a := newExplorerApp(t, ui.Size{Width: 900, Height: 600}, projectFiles, "main.go", "go.mod")
	s := a.shell()
	g := s.activeGroup()
	items := s.tabMenuItems(g, 0)
	var reveal *string
	for _, it := range items {
		if it.ID == "revealInExplorer" {
			label := it.Label
			reveal = &label
			if want := tabContext(g, 0); it.Data.(map[string]any)["editorIndex"] != want["editorIndex"] {
				t.Errorf("the row runs with %v", it.Data)
			}
		}
	}
	if reveal == nil || *reveal != "Reveal in Explorer View" {
		t.Fatalf("no Reveal in Explorer View row in %v", menuRows(items))
	}
}
