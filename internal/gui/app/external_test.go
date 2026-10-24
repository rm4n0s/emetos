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

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The tests of files changed on disk (step 7.4). The events are those the watcher reports, handed to the window the
// way the watcher does (Window.onFileEvents), in files of a temporary folder; one test runs the real watcher.

// extApp is an app with the files of a temporary folder, which another program changes.
type extApp struct{ *saveApp }

func newExtApp(t *testing.T, texts map[string]string, open ...string) *extApp {
	t.Helper()
	return &extApp{newSaveApp(t, texts, open...)}
}

// extBase is the modification time the files start at; put gives each version of a file a later one, so that two
// writes within one tick of the clock of the file system still differ.
var extBase = time.Now().Add(-time.Hour).Truncate(time.Second)

// put writes a file as another program does, with the modification time extBase+n seconds.
func (a *extApp) put(name, content string, n int) {
	a.t.Helper()
	putExternal(a.t, a.files[name], content, n)
}

func putExternal(t *testing.T, path, content string, n int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	at := extBase.Add(time.Duration(n) * time.Second)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// settle makes the stat of the file the one of the document, as if the document had just read it: the files of
// these tests are made before the window opens them, with the time of the write.
func (a *extApp) settle(name string) {
	a.t.Helper()
	path := a.files[name]
	putExternal(a.t, path, readFile(a.t, path), 0)
	doc := a.doc(name)
	if err := doc.Revert(context.Background(), fileio.ReadOptions{}); err != nil {
		a.t.Fatal(err)
	}
}

// deliver hands events to the window as the watcher does and waits until the work they started is done.
func (a *extApp) deliver(events ...workspace.Event) {
	a.t.Helper()
	a.win.onFileEvents(context.Background(), events)
	a.settleWork()
}

// settleWork pumps the posted work until no look at a file is going on.
func (a *extApp) settleWork() {
	a.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.ts.FlushPosted()
		a.ts.Pump(10 * time.Millisecond)
		if len(a.shell().ext.running) == 0 {
			a.ts.FlushPosted()
			a.pump()
			if len(a.shell().ext.running) == 0 {
				return
			}
		}
		if time.Now().After(deadline) {
			a.t.Fatal("a look at a file did not end")
		}
		time.Sleep(time.Millisecond)
	}
}

func changed(path string) workspace.Event {
	return workspace.Event{Kind: workspace.EventChanged, Path: path}
}
func deleted(path string) workspace.Event {
	return workspace.Event{Kind: workspace.EventDeleted, Path: path}
}
func created(path string) workspace.Event {
	return workspace.Event{Kind: workspace.EventCreated, Path: path}
}
func renamed(from, to string) workspace.Event {
	return workspace.Event{Kind: workspace.EventRenamed, OldPath: from, Path: to}
}

// warnings are the notifications of the window that are warnings.
func (a *extApp) warnings() []notify.Item {
	var out []notify.Item
	for _, n := range a.notes() {
		if n.Severity == notify.Warning {
			out = append(out, n)
		}
	}
	return out
}

func TestExternalChangeReloadsAnEditorAndKeepsItsView(t *testing.T) {
	tests := []struct {
		name    string
		content string
		// wantCursor is the line the cursor must be on afterwards
		wantLine int
	}{
		{"a line changed", strings.Replace(longText(300), "// line 5\n", "// line five\n", 1), 120},
		{"lines added", longText(400), 120},
		{"the file got shorter", longText(40), 41},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newExtApp(t, map[string]string{"long.go": longText(300)}, "long.go")
			a.settle("long.go")
			vm := a.vm()
			vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 120, Col: 3})})
			vm.SetScrollTop(1500)
			a.pump()
			scrolled := vm.ScrollTop()
			if scrolled < 1000 {
				t.Fatalf("the editor did not scroll: %v", scrolled)
			}
			doc := a.doc("long.go")
			a.put("long.go", tt.content, 100)
			a.deliver(changed(a.files["long.go"]))
			if doc.Text() != tt.content {
				t.Fatalf("the text was not read again (%d bytes, want %d)", len(doc.Text()), len(tt.content))
			}
			if doc.Dirty() || a.dirtyTab("long.go") || a.item("long.go").Deleted {
				t.Errorf("tab %+v", a.item("long.go"))
			}
			if got := vm.Cursors().Primary().Selection.Active; got.Line != tt.wantLine {
				t.Errorf("the cursor is at %v, want line %d", got, tt.wantLine)
			}
			if tt.wantLine == 120 && vm.Cursors().Primary().Selection.Active.Col != 3 {
				t.Errorf("the cursor moved to %v", vm.Cursors().Primary().Selection.Active)
			}
			if got := vm.ScrollTop(); tt.wantLine == 120 && got != scrolled {
				t.Errorf("the scroll position is %v, want %v", got, scrolled)
			}
			if n := len(a.notes()); n != 0 {
				t.Errorf("%d notifications: %v", n, a.notes())
			}
			a.noErrors()
		})
	}
}

func TestExternalChangeKeepsTheFolds(t *testing.T) {
	text := "func a() {\n\tone()\n\ttwo()\n}\n\nfunc b() {\n\tthree()\n}\n"
	a := newExtApp(t, map[string]string{"a.go": text}, "a.go")
	vm := a.vm()
	a.waitFor("the folding regions", func() bool { return vm.FoldingModel() != nil && vm.FoldingModel().Regions().Len() > 0 })
	rows := vm.Projection().RowCount()
	if err := vm.RunCommand("editor.foldAll", nil); err != nil {
		t.Fatal(err)
	}
	folded := vm.Projection().RowCount()
	if folded >= rows || len(vm.ViewState().Folds) == 0 {
		t.Fatalf("folding all left %d rows of %d, %d folds", folded, rows, len(vm.ViewState().Folds))
	}
	a.put("a.go", text+"// added at the end\n", 50)
	a.deliver(changed(a.files["a.go"]))
	a.waitFor("the folds", func() bool {
		return a.doc("a.go").Text() == text+"// added at the end\n" && vm.Projection().RowCount() == folded+1
	})
	a.noErrors()
}

func TestExternalChangeOfAnEditorOutsideTheViewport(t *testing.T) {
	// the file of a tab that is not shown is read again too, and its view is kept for when it is shown
	a := newExtApp(t, map[string]string{"a.go": "package a\n", "long.go": longText(300)}, "a.go", "long.go")
	a.settle("long.go")
	a.clickTab("long.go")
	vm := a.vm()
	vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 100, Col: 1})})
	vm.SetScrollTop(900)
	a.pump()
	a.clickTab("a.go")
	a.put("long.go", longText(250), 70)
	a.deliver(changed(a.files["long.go"]))
	a.clickTab("long.go")
	if got := vm.Cursors().Primary().Selection.Active; got.Line != 100 || got.Col != 1 {
		t.Errorf("the cursor is at %v", got)
	}
	if got := vm.ScrollTop(); got < 800 {
		t.Errorf("the scroll position is %v", got)
	}
	a.noErrors()
}

func TestExternalSpuriousEventChangesNothing(t *testing.T) {
	a := newExtApp(t, map[string]string{"a.go": "package a\n"}, "a.go")
	a.settle("a.go")
	a.typeText("// ")
	if _, err := a.doc("a.go").Undo(); err != nil {
		t.Fatal(err)
	}
	doc := a.doc("a.go")
	changes := 0
	doc.Subscribe(func(document.ChangeEvent) { changes++ })
	a.deliver(changed(a.files["a.go"]), created(a.files["a.go"]), changed(a.files["a.go"]))
	if changes != 0 || len(a.notes()) != 0 || !doc.CanRedo() {
		t.Errorf("changes %d, notes %v, can redo %v", changes, a.notes(), doc.CanRedo())
	}
}

func TestExternalChangeOfADirtyEditorAsksOnce(t *testing.T) {
	setup := func(t *testing.T) *extApp {
		a := newExtApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
		a.settle("a.txt")
		a.typeText("mine ")
		a.put("a.txt", "theirs\n", 100)
		a.deliver(changed(a.files["a.txt"]))
		return a
	}
	t.Run("the notification", func(t *testing.T) {
		a := setup(t)
		doc := a.doc("a.txt")
		if doc.Text() != "mine one\n" || !doc.Dirty() || !doc.Conflicted() {
			t.Fatalf("text %q, dirty %v, conflicted %v", doc.Text(), doc.Dirty(), doc.Conflicted())
		}
		warnings := a.warnings()
		if len(warnings) != 1 || len(a.notes()) != 1 {
			t.Fatalf("notifications: %v", a.notes())
		}
		n := warnings[0]
		if n.Message != "The file 'a.txt' has changed on disk and you have unsaved changes." || !n.Sticky {
			t.Errorf("notification %q, sticky %v", n.Message, n.Sticky)
		}
		if got := actionLabels(n); !slices.Equal(got, []string{"Overwrite", "Revert"}) {
			t.Errorf("buttons %v", got)
		}
		// more changes of the file: still one question
		a.put("a.txt", "theirs again\n", 200)
		a.deliver(changed(a.files["a.txt"]), changed(a.files["a.txt"]))
		if len(a.notes()) != 1 || a.notes()[0].ID != n.ID {
			t.Errorf("notifications after more changes: %v", a.notes())
		}
		if doc.Text() != "mine one\n" {
			t.Errorf("the text changed: %q", doc.Text())
		}
		a.noErrors()
	})
	t.Run("Overwrite", func(t *testing.T) {
		a := setup(t)
		a.pressAction(a.warnings()[0], "Overwrite")
		a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
		if got := readFile(t, a.files["a.txt"]); got != "mine one\n" {
			t.Errorf("file = %q", got)
		}
		if a.doc("a.txt").Conflicted() || len(a.notes()) != 0 {
			t.Errorf("conflicted %v, notifications %v", a.doc("a.txt").Conflicted(), a.notes())
		}
		a.noErrors()
	})
	t.Run("Revert", func(t *testing.T) {
		a := setup(t)
		a.pressAction(a.warnings()[0], "Revert")
		a.waitFor("the revert", func() bool { return !a.doc("a.txt").Dirty() })
		if got := a.doc("a.txt").Text(); got != "theirs\n" {
			t.Errorf("text = %q", got)
		}
		if a.doc("a.txt").Conflicted() || len(a.notes()) != 0 {
			t.Errorf("conflicted %v, notifications %v", a.doc("a.txt").Conflicted(), a.notes())
		}
		a.noErrors()
	})
	t.Run("the file returns to the same stat", func(t *testing.T) {
		a := setup(t)
		a.put("a.txt", "one\n", 0)
		a.deliver(changed(a.files["a.txt"]))
		if a.doc("a.txt").Conflicted() || len(a.notes()) != 0 || a.doc("a.txt").Text() != "mine one\n" {
			t.Errorf("conflicted %v, notifications %v, text %q", a.doc("a.txt").Conflicted(), a.notes(), a.doc("a.txt").Text())
		}
	})
	t.Run("a plain save is the conflict of the save with the same choices", func(t *testing.T) {
		a := setup(t)
		a.press("ctrl+s")
		a.waitFor("the failure", func() bool {
			for _, n := range a.notes() {
				if n.Severity == notify.Error {
					return true
				}
			}
			return false
		})
		if len(a.notes()) != 1 {
			t.Fatalf("notifications: %v", a.notes())
		}
		if got := actionLabels(a.errorNote()); !slices.Equal(got, []string{"Overwrite", "Revert", "Save As..."}) {
			t.Errorf("buttons %v", got)
		}
		if got := readFile(t, a.files["a.txt"]); got != "theirs\n" {
			t.Errorf("file = %q", got)
		}
	})
	t.Run("the changes are undone and the editor takes the file", func(t *testing.T) {
		a := setup(t)
		a.press("ctrl+z")
		a.settleWork()
		if got := a.doc("a.txt").Text(); got != "theirs\n" || a.doc("a.txt").Conflicted() || len(a.notes()) != 0 {
			t.Errorf("text %q, conflicted %v, notifications %v", got, a.doc("a.txt").Conflicted(), a.notes())
		}
	})
	t.Run("closing asks about the document", func(t *testing.T) {
		a := setup(t)
		a.press("ctrl+w")
		a.wantAsked("Do you want to save the changes you made to a.txt?")
		a.tapDialog("Cancel")
		if a.tabNames()[0] != "a.txt" {
			t.Errorf("tabs %v", a.tabNames())
		}
		// the answer "Don't Save" closes the editor and the notification goes with its document
		a.press("ctrl+w")
		a.wantAsked("Do you want to save the changes you made to a.txt?")
		a.tapDialog("Don't Save")
		if len(a.tabNames()) != 0 || len(a.notes()) != 0 {
			t.Errorf("tabs %v, notifications %v", a.tabNames(), a.notes())
		}
	})
}

func TestExternalStateStopsAutoSave(t *testing.T) {
	a := newExtApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
	a.settle("a.txt")
	a.settle("b.txt")
	a.setting("files.autoSave", "afterDelay")
	ctl := a.shell().autosave.ctl
	a.clickTab("a.txt")
	a.typeText("x")
	a.put("a.txt", "theirs\n", 100)
	a.deliver(changed(a.files["a.txt"]))
	a.clickTab("b.txt")
	a.typeText("y")
	if err := os.Remove(a.files["b.txt"]); err != nil {
		t.Fatal(err)
	}
	a.deliver(deleted(a.files["b.txt"]))
	for _, name := range []string{"a.txt", "b.txt"} {
		if got := ctl.Check(a.doc(name), document.SaveAuto); got != document.AutoSaveBlockedExternal {
			t.Errorf("%s: auto save check = %v", name, got)
		}
	}
	// nothing was saved by itself
	if got := readFile(t, a.files["a.txt"]); got != "theirs\n" {
		t.Errorf("a.txt = %q", got)
	}
	if _, err := os.Stat(a.files["b.txt"]); err == nil {
		t.Error("b.txt was written by auto save")
	}
}

func TestExternalDeletedFileKeepsItsEditor(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		t.Run(fmt.Sprintf("dirty=%v", dirty), func(t *testing.T) {
			a := newExtApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
			a.settle("a.txt")
			a.clickTab("a.txt")
			if dirty {
				a.typeText("x")
			}
			if err := os.Remove(a.files["a.txt"]); err != nil {
				t.Fatal(err)
			}
			a.deliver(deleted(a.files["a.txt"]))
			it := a.item("a.txt")
			if !it.Deleted || it.Dirty != dirty || !strings.HasSuffix(it.Title, "• Deleted") || a.item("b.txt").Deleted {
				t.Errorf("tabs %+v", a.tabs())
			}
			if !a.doc("a.txt").Orphaned() || len(a.notes()) != 0 {
				t.Errorf("orphaned %v, notifications %v", a.doc("a.txt").Orphaned(), a.notes())
			}
			wantNames(t, a.tabNames(), "a.txt", "b.txt")
			// the deleted editor is in the Open Editors list as a row that is struck through
			a.noErrors()
		})
	}
}

func TestExternalSaveMakesADeletedFileAgain(t *testing.T) {
	a := newExtApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.settle("a.txt")
	a.typeText("mine ")
	if err := os.Remove(a.files["a.txt"]); err != nil {
		t.Fatal(err)
	}
	a.deliver(deleted(a.files["a.txt"]))
	if !a.item("a.txt").Deleted {
		t.Fatalf("tabs %+v", a.tabs())
	}
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	if got := readFile(t, a.files["a.txt"]); got != "mine one\n" {
		t.Errorf("file = %q", got)
	}
	if a.item("a.txt").Deleted || a.doc("a.txt").Orphaned() {
		t.Errorf("the tab is still deleted: %+v", a.item("a.txt"))
	}
	// the watcher's events of the new file find nothing to do
	a.deliver(created(a.files["a.txt"]))
	if a.doc("a.txt").Text() != "mine one\n" || a.doc("a.txt").Dirty() {
		t.Errorf("text %q", a.doc("a.txt").Text())
	}
	a.noErrors()
}

func TestExternalFileThatIsBack(t *testing.T) {
	tests := []struct {
		name      string
		dirty     bool
		content   string
		time      int
		wantText  string
		wantNotes int
	}{
		{"clean, other content", false, "restored\n", 100, "restored\n", 0},
		{"clean, as it was", false, "one\n", 0, "one\n", 0},
		{"dirty, as it was", true, "one\n", 0, "mine one\n", 0},
		{"dirty, other content", true, "restored\n", 100, "mine one\n", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newExtApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
			a.settle("a.txt")
			if tt.dirty {
				a.typeText("mine ")
			}
			if err := os.Remove(a.files["a.txt"]); err != nil {
				t.Fatal(err)
			}
			a.deliver(deleted(a.files["a.txt"]))
			a.put("a.txt", tt.content, tt.time)
			a.deliver(created(a.files["a.txt"]))
			doc := a.doc("a.txt")
			if doc.Text() != tt.wantText || doc.Orphaned() || a.item("a.txt").Deleted || len(a.warnings()) != tt.wantNotes {
				t.Errorf("text %q, orphaned %v, tab %+v, warnings %v", doc.Text(), doc.Orphaned(), a.item("a.txt"), a.warnings())
			}
		})
	}
}

func TestExternalCloseOnFileDelete(t *testing.T) {
	tests := []struct {
		name    string
		setting bool
		dirty   bool
		want    []string
	}{
		{"off keeps a clean editor", false, false, []string{"a.txt", "b.txt"}},
		{"on closes a clean editor", true, false, []string{"b.txt"}},
		{"on keeps an editor with unsaved changes", true, true, []string{"a.txt", "b.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			win := settingsWindow(t, map[string]any{"workbench.editor.closeOnFileDelete": tt.setting})
			a := &extApp{newSaveAppWith(t, win, []string{"a.txt", "b.txt"}, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")}
			a.settle("a.txt")
			a.clickTab("a.txt")
			if tt.dirty {
				a.typeText("x")
			}
			if err := os.Remove(a.files["a.txt"]); err != nil {
				t.Fatal(err)
			}
			a.deliver(deleted(a.files["a.txt"]))
			wantNames(t, a.tabNames(), tt.want...)
			a.noErrors()
		})
	}
}

func TestExternalRenameMovesTheTab(t *testing.T) {
	a := newExtApp(t, map[string]string{"a.txt": "one\n", "b.go": "package b\n"}, "a.txt", "b.go")
	a.settle("a.txt")
	a.clickTab("a.txt")
	a.typeText("mine ")
	from := a.files["a.txt"]
	to := filepath.Join(filepath.Dir(from), "renamed.go")
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	a.win.Recent.Add(from)
	doc := a.doc("a.txt")
	ev := renamed(from, to)
	a.deliver(ev)
	wantNames(t, a.tabNames(), "renamed.go", "b.go")
	if doc.URI() != document.FileURI(to) || !doc.Dirty() || doc.Text() != "mine one\n" || doc.Orphaned() || doc.Conflicted() {
		t.Errorf("uri %v, dirty %v, text %q, state %+v", doc.URI(), doc.Dirty(), doc.Text(), doc.ExternalState())
	}
	if doc.LanguageID() != "go" {
		t.Errorf("the language is %q after the new extension", doc.LanguageID())
	}
	if slices.Contains(a.win.Recent.List(), from) || !slices.Contains(a.win.Recent.List(), to) {
		t.Errorf("recent files %v", a.win.Recent.List())
	}
	if got, ok := a.win.Documents.Get(document.FileURI(to)); !ok || got != doc {
		t.Error("the set of documents does not have the document at its new path")
	}
	// the echo of the same rename, and of an operation of the Explorer, changes nothing
	a.deliver(ev, ev)
	wantNames(t, a.tabNames(), "renamed.go", "b.go")
	if !doc.Dirty() || doc.Text() != "mine one\n" || len(a.notes()) != 0 {
		t.Errorf("after the echo: dirty %v, text %q, notifications %v", doc.Dirty(), doc.Text(), a.notes())
	}
	// the document saves in its new file
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !doc.Dirty() })
	if got := readFile(t, to); got != "mine one\n" {
		t.Errorf("the new file = %q", got)
	}
	a.noErrors()
}

func TestExternalRenameOfAFolderMovesTheTabsBelowIt(t *testing.T) {
	a := newExtApp(t, map[string]string{"dir/a.txt": "one\n", "dir/sub/b.txt": "two\n", "other.txt": "three\n"}, "dir/a.txt", "dir/sub/b.txt", "other.txt")
	dir := filepath.Dir(a.files["dir/a.txt"])
	moved := filepath.Join(filepath.Dir(dir), "moved")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	a.deliver(workspace.Event{Kind: workspace.EventRenamed, OldPath: dir, Path: moved, IsDir: true})
	if a.doc("dir/a.txt").URI().Path != filepath.Join(moved, "a.txt") || a.doc("dir/sub/b.txt").URI().Path != filepath.Join(moved, "sub", "b.txt") {
		t.Errorf("uris %v, %v", a.doc("dir/a.txt").URI(), a.doc("dir/sub/b.txt").URI())
	}
	if a.doc("other.txt").URI().Path != a.files["other.txt"] || a.doc("dir/a.txt").Orphaned() || a.doc("dir/sub/b.txt").Orphaned() {
		t.Error("a document that did not move changed, or one that moved is deleted")
	}
	wantNames(t, a.tabNames(), "a.txt", "b.txt", "other.txt")
	a.noErrors()
}

func TestExternalDeleteOfAFolderDeletesTheEditorsBelowIt(t *testing.T) {
	a := newExtApp(t, map[string]string{"dir/a.txt": "one\n", "dir/sub/b.txt": "two\n", "other.txt": "three\n"}, "dir/a.txt", "dir/sub/b.txt", "other.txt")
	a.settle("dir/a.txt")
	a.settle("dir/sub/b.txt")
	a.settle("other.txt")
	dir := filepath.Dir(a.files["dir/a.txt"])
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	a.deliver(workspace.Event{Kind: workspace.EventDeleted, Path: dir, IsDir: true})
	if !a.doc("dir/a.txt").Orphaned() || !a.doc("dir/sub/b.txt").Orphaned() || a.doc("other.txt").Orphaned() {
		t.Errorf("orphaned: %v %v %v", a.doc("dir/a.txt").Orphaned(), a.doc("dir/sub/b.txt").Orphaned(), a.doc("other.txt").Orphaned())
	}
	if !a.item("a.txt").Deleted || !a.item("b.txt").Deleted || a.item("other.txt").Deleted {
		t.Errorf("tabs %+v", a.tabs())
	}
	a.noErrors()
}

func TestExternalRescanLooksAtEveryOpenFile(t *testing.T) {
	a := newExtApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n", "c.txt": "three\n"}, "a.txt", "b.txt", "c.txt")
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		a.settle(n)
	}
	a.clickTab("c.txt")
	a.typeText("mine ")
	a.put("a.txt", "one changed\n", 100)
	if err := os.Remove(a.files["b.txt"]); err != nil {
		t.Fatal(err)
	}
	a.put("c.txt", "three changed\n", 100)
	root := filepath.Dir(a.files["a.txt"])
	a.deliver(workspace.Event{Kind: workspace.EventRescan, Path: root, IsDir: true})
	if a.doc("a.txt").Text() != "one changed\n" || !a.doc("b.txt").Orphaned() || !a.doc("c.txt").Conflicted() {
		t.Errorf("a %q, b orphaned %v, c conflicted %v", a.doc("a.txt").Text(), a.doc("b.txt").Orphaned(), a.doc("c.txt").Conflicted())
	}
	if len(a.warnings()) != 1 {
		t.Errorf("notifications %v", a.notes())
	}
}

func TestExternalFilesWithoutAFileAreIgnored(t *testing.T) {
	a := newExtApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.run("workbench.action.files.newUntitledFile")
	a.typeText("untitled text")
	untitled := a.shell().activeEditor().doc
	// an event for every path there is, and for the numbers of the untitled documents
	a.deliver(workspace.Event{Kind: workspace.EventRescan, Path: "/", IsDir: true}, changed(untitled.URI().Path), deleted("1"))
	if untitled.Text() != "untitled text" || untitled.Orphaned() || untitled.Conflicted() || len(a.warnings()) != 0 {
		t.Errorf("text %q, state %+v, notifications %v", untitled.Text(), untitled.ExternalState(), a.notes())
	}
	a.noErrors()
}

func TestExternalReadFailureIsLeftForTheNextEvent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any file")
	}
	a := newExtApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.settle("a.txt")
	a.put("a.txt", "two\n", 100)
	if err := os.Chmod(a.files["a.txt"], 0); err != nil {
		t.Fatal(err)
	}
	a.deliver(changed(a.files["a.txt"]))
	if a.doc("a.txt").Text() != "one\n" || a.doc("a.txt").Dirty() || len(a.warnings()) != 0 {
		t.Errorf("text %q, notifications %v", a.doc("a.txt").Text(), a.notes())
	}
	if err := os.Chmod(a.files["a.txt"], 0o644); err != nil {
		t.Fatal(err)
	}
	a.deliver(changed(a.files["a.txt"]))
	if a.doc("a.txt").Text() != "two\n" {
		t.Errorf("text %q after the next event", a.doc("a.txt").Text())
	}
}

func TestExternalRetriesAfterAFailedRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any file")
	}
	a := newExtApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.settle("a.txt")
	// the timers of the retries are the test's: it fires them
	type timer struct {
		after time.Duration
		f     func()
		done  bool
	}
	var timers []*timer
	a.win.after = func(d time.Duration, f func()) func() {
		tm := &timer{after: d, f: f}
		timers = append(timers, tm)
		return func() { tm.done = true }
	}
	pending := func() []*timer {
		var out []*timer
		for _, tm := range timers {
			if !tm.done {
				out = append(out, tm)
			}
		}
		return out
	}
	fire := func() {
		t.Helper()
		p := pending()
		if len(p) != 1 {
			t.Fatalf("%d timers are pending, want 1", len(p))
		}
		p[0].done = true
		p[0].f()
		a.settleWork()
	}
	a.put("a.txt", "two\n", 100)
	if err := os.Chmod(a.files["a.txt"], 0); err != nil {
		t.Fatal(err)
	}
	a.deliver(changed(a.files["a.txt"]))
	if p := pending(); len(p) != 1 || p[0].after != 300*time.Millisecond {
		t.Fatalf("timers after the first failure: %v", len(p))
	}
	fire() // still not readable: the second wait
	if p := pending(); len(p) != 1 || p[0].after != 2*time.Second {
		t.Fatalf("timers after the second failure: %v", len(p))
	}
	if err := os.Chmod(a.files["a.txt"], 0o644); err != nil {
		t.Fatal(err)
	}
	fire() // readable now
	if a.doc("a.txt").Text() != "two\n" || len(pending()) != 0 {
		t.Errorf("text %q, %d timers", a.doc("a.txt").Text(), len(pending()))
	}
	// after the second wait there is no third: the next event of the watcher is the one to act on
	a.put("a.txt", "three\n", 200)
	if err := os.Chmod(a.files["a.txt"], 0); err != nil {
		t.Fatal(err)
	}
	a.deliver(changed(a.files["a.txt"]))
	fire()
	fire()
	if len(pending()) != 0 {
		t.Errorf("%d retries are still pending", len(pending()))
	}
}

func TestExternalOpenEditorsListShowsADeletedFile(t *testing.T) {
	a := newExtApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
	a.settle("a.txt")
	if err := os.Remove(a.files["a.txt"]); err != nil {
		t.Fatal(err)
	}
	a.deliver(deleted(a.files["a.txt"]))
	g := a.shell().activeGroup()
	var gone []bool
	for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
		gone = append(gone, editorgroup.IsDeleted(in))
	}
	if !slices.Equal(gone, []bool{true, false}) {
		t.Errorf("deleted editors %v", gone)
	}
}

// ---- the plan of a batch of events ----

func TestRenamesOfEvents(t *testing.T) {
	tests := []struct {
		name   string
		events []workspace.Event
		want   []workspace.PathMove
	}{
		{"none", []workspace.Event{changed("/w/a"), deleted("/w/b")}, nil},
		{"one", []workspace.Event{renamed("/w/a", "/w/b")}, []workspace.PathMove{{Old: "/w/a", New: "/w/b"}}},
		{"in order", []workspace.Event{renamed("/w/a", "/w/b"), changed("/w/c"), renamed("/w/b", "/w/c")}, []workspace.PathMove{{Old: "/w/a", New: "/w/b"}, {Old: "/w/b", New: "/w/c"}}},
		{"a rename without a path", []workspace.Event{{Kind: workspace.EventRenamed, Path: "/w/b"}}, nil},
		{"to itself", []workspace.Event{renamed("/w/a", "/w/a")}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renamesOf(tt.events); !slices.Equal(got, tt.want) {
				t.Errorf("renamesOf = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFollowMoves(t *testing.T) {
	moves := []workspace.PathMove{{Old: "/w/dir", New: "/w/moved"}, {Old: "/w/moved/a.txt", New: "/w/moved/b.txt"}, {Old: "/w/x", New: "/w/y"}}
	tests := []struct{ path, want string }{
		{"/w/dir", "/w/moved"},
		{"/w/dir/a.txt", "/w/moved/b.txt"}, // renamed twice in a batch: the last name
		{"/w/dir/sub/c.txt", "/w/moved/sub/c.txt"},
		{"/w/dirty.txt", "/w/dirty.txt"}, // a name that only starts like the folder
		{"/w/x", "/w/y"},
		{"/elsewhere/a.txt", "/elsewhere/a.txt"},
	}
	follow := followMoves(moves)
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, gone := follow(tt.path)
			if got != tt.want || gone {
				t.Errorf("follow = %q, %v; want %q", got, gone, tt.want)
			}
		})
	}
}

func TestEventsTouch(t *testing.T) {
	tests := []struct {
		name   string
		events []workspace.Event
		path   string
		want   bool
	}{
		{"a change of the file", []workspace.Event{changed("/w/a.txt")}, "/w/a.txt", true},
		{"a change of another file", []workspace.Event{changed("/w/b.txt")}, "/w/a.txt", false},
		{"a change of a folder says nothing of its files", []workspace.Event{changed("/w")}, "/w/a.txt", false},
		{"the file is created", []workspace.Event{created("/w/a.txt")}, "/w/a.txt", true},
		{"the file is deleted", []workspace.Event{deleted("/w/a.txt")}, "/w/a.txt", true},
		{"a folder above is deleted", []workspace.Event{deleted("/w")}, "/w/sub/a.txt", true},
		{"a folder with a similar name is deleted", []workspace.Event{deleted("/w/su")}, "/w/sub/a.txt", false},
		{"a rescan of a folder above", []workspace.Event{{Kind: workspace.EventRescan, Path: "/w", IsDir: true}}, "/w/sub/a.txt", true},
		{"a rescan of another folder", []workspace.Event{{Kind: workspace.EventRescan, Path: "/v", IsDir: true}}, "/w/sub/a.txt", false},
		{"the file was renamed away", []workspace.Event{renamed("/w/a.txt", "/w/b.txt")}, "/w/a.txt", true},
		{"the file was renamed here", []workspace.Event{renamed("/w/b.txt", "/w/a.txt")}, "/w/a.txt", true},
		{"a folder was renamed over it", []workspace.Event{renamed("/v", "/w")}, "/w/a.txt", true},
		{"nothing", nil, "/w/a.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventsTouch(tt.events, tt.path); got != tt.want {
				t.Errorf("eventsTouch = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---- an own save, and the real watcher ----

func TestExternalOwnSaveIsNeverReadAgain(t *testing.T) {
	a := newExtApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.settle("a.txt")
	a.typeText("mine ")
	doc := a.doc("a.txt")
	s := a.shell()
	job, err := doc.BeginSave(s.saveOptions(doc, saveRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	stat, err := job.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// the event of our own write comes before the save is finished: it waits
	a.deliver(changed(a.files["a.txt"]))
	if !s.ext.deferred[doc] || doc.Text() != "mine one\n" {
		t.Fatalf("deferred %v, text %q", s.ext.deferred[doc], doc.Text())
	}
	changes := 0
	doc.Subscribe(func(document.ChangeEvent) { changes++ })
	doc.FinishSave(job, stat)
	s.saveSettled(doc)
	a.settleWork()
	if changes != 0 || doc.Dirty() || doc.Text() != "mine one\n" || !doc.CanUndo() || len(a.notes()) != 0 {
		t.Errorf("changes %d, dirty %v, text %q, can undo %v, notifications %v", changes, doc.Dirty(), doc.Text(), doc.CanUndo(), a.notes())
	}
	if s.ext.deferred[doc] {
		t.Error("the look still waits")
	}
}

// watchedApp is an app over a folder with the real watcher.
func newWatchedApp(t *testing.T, texts map[string]string, open ...string) (*extApp, string) {
	t.Helper()
	folder := t.TempDir()
	for name, text := range texts {
		path := filepath.Join(folder, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"editor.fontSize": 14, "editor.fontFamily": testutil.MonoFamily})
	ctx, cancel := context.WithCancel(context.Background())
	win, err := NewWindow(ctx, NewProcess(ProcessOptions{Paths: paths, WatchFiles: true}), folder)
	if err != nil {
		cancel()
		t.Fatalf("NewWindow: %v", err)
	}
	a := &extApp{&saveApp{groupApp: &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}}}
	var docs []*document.Document
	for _, n := range open {
		a.files[n] = filepath.Join(folder, n)
		docs = append(docs, a.doc(n))
	}
	// the watcher is started here, with the context of the test, before the shell asks for it (a second start does
	// nothing): the tester does not take a shell down, so nothing else would end it
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
	a.ts = testutil.New(t, testWindowSize)
	a.fd = a.ts.FakeDesktop()
	a.ts.PumpWidget(newAppDocs(ctx, win, docs, nil, ""))
	a.pump()
	return a, folder
}

// waitEvent waits until the window has heard of an event for a path.
func (a *extApp) waitEvent(path string, do func()) {
	a.t.Helper()
	seen := make(chan struct{}, 16)
	stop := a.win.OnFileEvents(func(events []workspace.Event) {
		for _, e := range events {
			if e.Path == path {
				select {
				case seen <- struct{}{}:
				default:
				}
			}
		}
	})
	defer stop()
	do()
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case <-seen:
			a.settleWork()
			return
		default:
		}
		if time.Now().After(deadline) {
			a.t.Fatalf("no event for %s", path)
		}
		a.ts.FlushPosted()
		a.ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
}

func TestExternalWatcherEndToEnd(t *testing.T) {
	a, folder := newWatchedApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n", "c.txt": "three\n"}, "a.txt", "b.txt", "c.txt")
	a.clickTab("a.txt")
	// another program writes a file the editor shows
	putExternal(t, a.files["a.txt"], "one, written by another program\n", int(time.Since(extBase)/time.Second)+5)
	a.waitFor("the file to be read again", func() bool { return a.doc("a.txt").Text() == "one, written by another program\n" })
	if a.doc("a.txt").Dirty() || len(a.notes()) != 0 {
		t.Errorf("dirty %v, notifications %v", a.doc("a.txt").Dirty(), a.notes())
	}
	// our own save is not taken for a change by another program
	a.typeText("mine ")
	changes := 0
	a.doc("a.txt").Subscribe(func(document.ChangeEvent) { changes++ })
	marker := filepath.Join(folder, "marker.txt")
	a.waitEvent(marker, func() {
		a.press("ctrl+s")
		a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
		if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	if changes != 0 || a.doc("a.txt").Text() != "mine one, written by another program\n" || !a.doc("a.txt").CanUndo() || len(a.notes()) != 0 {
		t.Errorf("after our own save: changes %d, text %q, can undo %v, notifications %v", changes, a.doc("a.txt").Text(), a.doc("a.txt").CanUndo(), a.notes())
	}
	// a delete, and the file that is back
	a.waitEvent(a.files["b.txt"], func() {
		if err := os.Remove(a.files["b.txt"]); err != nil {
			t.Fatal(err)
		}
	})
	a.waitFor("the tab to be struck through", func() bool { return a.item("b.txt").Deleted })
	a.waitEvent(a.files["b.txt"], func() { putExternal(t, a.files["b.txt"], "two again\n", 400) })
	a.waitFor("the tab to be back", func() bool { return !a.item("b.txt").Deleted && a.doc("b.txt").Text() == "two again\n" })
	// a rename, where inotify pairs the two halves
	renamedTo := filepath.Join(folder, "renamed.txt")
	a.waitEvent(renamedTo, func() {
		if err := os.Rename(a.files["c.txt"], renamedTo); err != nil {
			t.Fatal(err)
		}
	})
	a.waitFor("the tab to follow the rename", func() bool { return slices.Contains(a.tabNames(), "renamed.txt") })
	if slices.Contains(a.tabNames(), "c.txt") || a.doc("c.txt").URI().Path != renamedTo {
		t.Errorf("tabs %v, uri %v", a.tabNames(), a.doc("c.txt").URI())
	}
	a.noErrors()
}

func TestExternalWatcherOfAFileOutsideTheFolder(t *testing.T) {
	outside := t.TempDir()
	path := filepath.Join(outside, "out.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := newWatchedApp(t, map[string]string{"a.txt": "x\n"}, "a.txt")
	doc, err := a.win.Documents.OpenFile(context.Background(), path, document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	a.win.Documents.Retain(doc) // the editor that shows it counts as a use, as the window does
	a.shell().show(widgets.WidgetsBindingOf(a.shell().Context()), doc, openOptions{})
	a.pump()
	a.waitEvent(path, func() { putExternal(t, path, "two\n", int(time.Since(extBase)/time.Second)+5) })
	a.waitFor("the file outside the folder to be read again", func() bool { return doc.Text() == "two\n" })
	a.noErrors()
}
