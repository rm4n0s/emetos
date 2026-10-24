package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/snippet"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// watchedWindow makes a window over a process that watches files, with the watcher started and
// stopped with the test.
func watchedWindow(t *testing.T, folder string) *Window {
	t.Helper()
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"editor.fontSize": 14})
	ctx, cancel := context.WithCancel(context.Background())
	win, err := NewWindow(ctx, newTestProcess(ProcessOptions{Paths: paths, WatchFiles: true}), folder)
	if err != nil {
		cancel()
		t.Fatalf("NewWindow: %v", err)
	}
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

// eventually polls a condition for up to five seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func fontSize(win *Window) float64 {
	return editor.OptionsFromSettings(win.Settings, "", "", 4).FontSize
}

func TestWindowOpensTheFolderAsAWorkspace(t *testing.T) {
	folder := t.TempDir()
	win := newTestWindow(t, folder)
	if win.Workspace == nil || win.Workspace.Path() != folder || win.Workspace.Name() != filepath.Base(folder) || win.Folder != folder {
		t.Fatalf("Workspace %+v Folder %q", win.Workspace, win.Folder)
	}
	if got := win.SnippetWorkspace(); got.Name != filepath.Base(folder) || got.Folder != folder {
		t.Errorf("SnippetWorkspace = %+v", got)
	}
	// the tree follows the settings of the folder
	cfg := win.Workspace.Config()
	if cfg.SortOrder != workspace.SortDefault || !cfg.CompactFolders {
		t.Errorf("config %+v", cfg)
	}
	none := newTestWindow(t, "")
	if none.Workspace != nil || none.Folder != "" || none.SnippetWorkspace() != (snippet.WorkspaceResolver{}) {
		t.Error("a window with no folder has no workspace")
	}
}

func TestWindowWithAFolderThatIsNotThereFails(t *testing.T) {
	paths := testutil.Isolate(t)
	_, err := NewWindow(context.Background(), newTestProcess(ProcessOptions{Paths: paths}), filepath.Join(t.TempDir(), "gone"))
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("NewWindow->newWindow->Window.openWorkspace->Open.WorkspaceFolderNotFound") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestWatcherReloadsTheUserSettings(t *testing.T) {
	win := watchedWindow(t, "")
	if got := fontSize(win); got != 14 {
		t.Fatalf("font size %v", got)
	}
	path, err := settings.UserSettingsPath(win.Process.Paths, storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	var heard atomic.Int32
	win.Settings.OnChange(func(settings.ChangeEvent) { heard.Add(1) })
	// somebody else edits the file, as an editor that saves by renaming a temporary file does
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(`{ "editor.fontSize": 22 }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the new font size", func() bool { return fontSize(win) == 22 })
	if heard.Load() == 0 {
		t.Error("the listeners of the settings were not told")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the default font size", func() bool { return fontSize(win) != 22 })
}

func TestWatcherReloadsTheWorkspaceSettingsAndTheTree(t *testing.T) {
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	win := watchedWindow(t, folder)
	if _, err := win.Workspace.Root().Children(context.Background()); err != nil {
		t.Fatal(err)
	}
	// the .emetos folder does not exist yet: it is made and the file written in it
	if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, ".emetos", "settings.json"), []byte(`{ "editor.fontSize": 31, "explorer.sortOrder": "filesFirst" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the font size of the workspace", func() bool { return fontSize(win) == 31 })
	// the settings of the tree reach the workspace, which reads its folders again
	eventually(t, "the sort order", func() bool { return win.Workspace.Config().SortOrder == workspace.SortFilesFirst })
	// a file made in the folder shows in the tree
	if err := os.WriteFile(filepath.Join(folder, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	eventually(t, "b.txt in the tree", func() bool { return win.Workspace.Find(filepath.Join(folder, "b.txt")) != nil })
	if err := os.Remove(filepath.Join(folder, "a.txt")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a.txt out of the tree", func() bool { return win.Workspace.Find(filepath.Join(folder, "a.txt")) == nil })
}

func TestFilesExcludeSettingHidesFilesFromTheTree(t *testing.T) {
	folder := t.TempDir()
	for _, n := range []string{"a.txt", "skip.log"} {
		if err := os.WriteFile(filepath.Join(folder, n), []byte("a"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	win := watchedWindow(t, folder)
	if kids, err := win.Workspace.Root().Children(context.Background()); err != nil || len(kids) != 2 {
		t.Fatalf("children %v %v", kids, err)
	}
	if err := win.Settings.Update(context.Background(), "files.exclude", map[string]any{"**/*.log": true}, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	eventually(t, "skip.log to leave the tree", func() bool { return win.Workspace.Find(filepath.Join(folder, "skip.log")) == nil })
	if win.Workspace.Find(filepath.Join(folder, "a.txt")) == nil {
		t.Error("a.txt left the tree")
	}
}

func TestWatcherReloadsTheSnippets(t *testing.T) {
	win := watchedWindow(t, "")
	dir := win.Snippets.Dir()
	query := snippet.Query{LanguageID: "go"}
	if got := win.Snippets.Snippets(query); len(got) != 0 {
		t.Fatalf("snippets: %v", got)
	}
	// the folder of the snippets does not exist; it is made, and a file written in it
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "go.json")
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{ "Hello": { "prefix": "hello", "body": "hello ${WORKSPACE_NAME}" } }`)
	eventually(t, "the snippet", func() bool { return len(win.Snippets.Snippets(query)) == 1 })
	write(`{ "Hello": { "prefix": "hello", "body": "one" }, "Bye": { "prefix": "bye", "body": "two" } }`)
	eventually(t, "two snippets", func() bool { return len(win.Snippets.Snippets(query)) == 2 })
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	eventually(t, "no snippets", func() bool { return len(win.Snippets.Snippets(query)) == 0 })
}

func TestFilesOutsideTheFolderAreFollowed(t *testing.T) {
	folder, outside := t.TempDir(), t.TempDir()
	inside, away := filepath.Join(folder, "in.txt"), filepath.Join(outside, "out.txt")
	for _, p := range []string{inside, away} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	win := watchedWindow(t, folder)
	followed := func() int {
		win.watch.mu.Lock()
		defer win.watch.mu.Unlock()
		return len(win.watch.files)
	}
	ctx := context.Background()
	if _, err := win.Documents.OpenFile(ctx, inside, document.NewOptions()); err != nil {
		t.Fatal(err)
	}
	doc, err := win.Documents.OpenFile(ctx, away, document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the file outside to be followed", func() bool {
		win.watch.mu.Lock()
		defer win.watch.mu.Unlock()
		return win.watch.files[away] != nil
	})
	if followed() != 1 {
		t.Errorf("%d files are followed one by one; the one in the folder is watched with the folder", followed())
	}
	win.Documents.Close(doc.URI())
	eventually(t, "the file to be let go", func() bool { return followed() == 0 })
}

func TestFilesOpenBeforeTheWatcherStartedAreFollowed(t *testing.T) {
	outside := t.TempDir()
	away := filepath.Join(outside, "out.txt")
	if err := os.WriteFile(away, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := testutil.Isolate(t)
	ctx, cancel := context.WithCancel(context.Background())
	win, err := NewWindow(ctx, newTestProcess(ProcessOptions{Paths: paths, WatchFiles: true}), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := win.Documents.OpenFile(ctx, away, document.NewOptions()); err != nil {
		t.Fatal(err)
	}
	ready := win.StartWatching(ctx)
	t.Cleanup(func() { cancel(); <-win.Watcher().Done() })
	<-ready
	win.watch.mu.Lock()
	defer win.watch.mu.Unlock()
	if win.watch.files[away] == nil {
		t.Error("a file that was open before the watcher started is not followed")
	}
}

func TestProcessWithoutWatchFilesStartsNoWatcher(t *testing.T) {
	win := newTestWindow(t, "")
	<-win.StartWatching(context.Background())
	if win.Watcher() != nil {
		t.Error("a watcher was started")
	}
}

func TestBreadcrumbsAndSnippetsUseTheFolder(t *testing.T) {
	folder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(folder, "pkg", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, "pkg", "sub", "main.go")
	if err := os.WriteFile(file, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	win := newTestWindow(t, folder)
	doc, err := win.Documents.OpenFile(context.Background(), file, document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))

	// the bar starts at the folder and goes on with the path inside it
	bar := gluttertest.ByType[*editor.Breadcrumbs]()
	if err := gluttertest.FindsOneWidget(ts, bar); err != nil {
		t.Fatal(err)
	}
	b := ts.Widget(bar).(*editor.Breadcrumbs)
	var got []string
	for _, item := range b.Items() {
		got = append(got, item.Name)
	}
	if want := []string{filepath.Base(folder), "pkg", "sub", "main.go"}; strings.Join(got, "/") != strings.Join(want, "/") {
		t.Errorf("segments %q, want %q", got, want)
	}
	if err := gluttertest.FindsOneWidget(ts, inBar(filepath.Base(folder))); err != nil {
		t.Error("the name of the folder is on the bar:", err)
	}

	// the title names the folder
	waitFor(t, ts, "the title", func() bool { return strings.Contains(win.Title(), filepath.Base(folder)) })

	// the variables of a snippet
	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
	if err := vm.InsertSnippet("${WORKSPACE_NAME}|${WORKSPACE_FOLDER}|${RELATIVE_FILEPATH}", snippet.DefaultInsertOptions()); err != nil {
		t.Fatal(err)
	}
	want := filepath.Base(folder) + "|" + folder + "|" + filepath.Join("pkg", "sub", "main.go")
	if text := doc.Text(); !strings.HasPrefix(text, want) {
		t.Errorf("the snippet gave %q, want it to start with %q", text, want)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}
