package app

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/explorer"
)

// fileApp is an explorerApp for the tests of the file operations.
type fileApp struct{ *explorerApp }

var fileOpsSize = ui.Size{Width: 900, Height: 600}

func newFileApp(t *testing.T, entries []string, open ...string) *fileApp {
	t.Helper()
	return &fileApp{newExplorerApp(t, fileOpsSize, entries, open...)}
}

func (a *fileApp) view() *explorer.View { return a.shell().explorer.view }

func (a *fileApp) group() *editorgroup.Group { return a.shell().activeGroup() }

// focusTree selects a path and gives the tree the keyboard focus.
func (a *fileApp) focusTree(rel string) {
	a.t.Helper()
	if err := a.run("revealInExplorer", a.path(rel)); err != nil {
		a.t.Fatal(err)
	}
	a.pump()
	if !a.view().Controller().HasFocus() {
		a.t.Fatal("the tree has not the focus")
	}
}

func (a *fileApp) exists(rel string) bool {
	_, err := os.Lstat(a.path(rel))
	return err == nil
}

func (a *fileApp) read(rel string) string {
	a.t.Helper()
	b, err := os.ReadFile(a.path(rel))
	if err != nil {
		a.t.Fatal(err)
	}
	return string(b)
}

// editors are the names of the editors of the active group, in order.
func (a *fileApp) editors() []string {
	var out []string
	for _, e := range a.group().Model.Editors(editorgroup.Sequential, false) {
		out = append(out, e.Name())
	}
	return out
}

// key sends a key to the window through the dispatcher and the focus.
func (a *fileApp) key(logical services.LogicalKeyboardKey) {
	a.t.Helper()
	a.ts.SendKeyEvent(logical)
	a.pump()
}

func (a *fileApp) text(s string) bool { return a.ts.Exists(gluttertest.ByText(s)) }

func (a *fileApp) dialog() bool { return a.win.Dialogs.IsShowing() }

// answer presses a button of the dialog that shows.
func (a *fileApp) answer(label string) {
	a.t.Helper()
	if !a.dialog() {
		a.t.Fatalf("no dialog shows (looking for %q)", label)
	}
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText(label)))
	a.pump()
}

// trashed lists the names in the trash of the isolated data folder.
func (a *fileApp) trashed() []string {
	entries, _ := os.ReadDir(filepath.Join(os.Getenv("XDG_DATA_HOME"), "Trash", "files"))
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func (a *fileApp) mustRun(id string, args any) {
	a.t.Helper()
	if err := a.run(id, args); err != nil {
		a.t.Fatal(err)
	}
	a.pump()
}

func TestNewFileInputCreatesAndOpensTheFile(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.focusTree("main.go")
	a.mustRun("explorer.newFile", nil)
	if !a.view().Editing() || !a.ts.Exists(gluttertest.ByKey(explorer.EditInputKey)) {
		t.Fatal("the input did not open")
	}
	if got := a.win.Keys.Get("inputFocus"); got != true {
		t.Errorf("inputFocus = %v", got)
	}
	// the input is in the folder of the window, among the files, with the focus
	if got := a.rows(); !slices.Contains(got, "\x00new") && len(got) == 0 {
		t.Errorf("rows %v", got)
	}
	a.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "created.go")
	a.pump()
	a.key(services.LogicalKeyboardKeyEnter)
	a.waitFor("the file", func() bool { return a.exists("created.go") })
	a.pump()
	if a.view().Editing() {
		t.Error("the input stays open")
	}
	if got := a.win.Keys.Get("inputFocus"); got != false {
		t.Errorf("inputFocus = %v after the input closed", got)
	}
	if !slices.Contains(a.editors(), "created.go") {
		t.Errorf("editors %v, want created.go opened", a.editors())
	}
	if active := a.group().Model.ActiveEditor(); active.Name() != "created.go" || !a.group().Model.IsPinned(active) {
		t.Errorf("active editor %s pinned %v", active.Name(), a.group().Model.IsPinned(active))
	}
	if !slices.Contains(a.rows(), "*created.go") && !slices.Contains(a.rows(), "created.go") {
		t.Errorf("rows %v, want created.go", a.rows())
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestNewFolderInputAndNestedNames(t *testing.T) {
	tests := []struct {
		name   string
		cmd    string
		on     string
		typed  string
		want   []string
		absent []string
	}{
		{"folder at the root", "explorer.newFolder", "main.go", "newdir", []string{"newdir"}, nil},
		{"folder in the selected folder", "explorer.newFolder", "docs", "sub", []string{"docs/sub"}, nil},
		{"file in the selected folder", "explorer.newFile", "docs", "readme.md", []string{"docs/readme.md"}, nil},
		{"file next to the selected file", "explorer.newFile", "docs/guide.md", "next.md", []string{"docs/next.md"}, nil},
		{"nested names make the folders", "explorer.newFile", "main.go", "a/b/c.txt", []string{"a/b/c.txt"}, nil},
		{"a name with a slash at the end is a folder", "explorer.newFile", "main.go", "dd/", []string{"dd"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newFileApp(t, projectFiles, "main.go")
			a.focusTree(tt.on)
			a.mustRun(tt.cmd, nil)
			a.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), tt.typed)
			a.pump()
			a.key(services.LogicalKeyboardKeyEnter)
			a.waitFor("the item", func() bool { return a.exists(tt.want[0]) })
			if strings.HasSuffix(tt.typed, "/") || strings.Contains(tt.cmd, "Folder") {
				if !isDir(a.path(tt.want[0])) {
					t.Errorf("%s is not a folder", tt.want[0])
				}
			} else if isDir(a.path(tt.want[0])) {
				t.Errorf("%s is a folder", tt.want[0])
			}
		})
	}
}

func TestNewFileInputEscapeCancels(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.focusTree("main.go")
	a.mustRun("explorer.newFile", nil)
	a.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "never.go")
	a.pump()
	a.key(services.LogicalKeyboardKeyEscape)
	a.pump()
	if a.view().Editing() || a.ts.Exists(gluttertest.ByKey(explorer.EditInputKey)) {
		t.Error("Escape did not close the input")
	}
	time.Sleep(20 * time.Millisecond)
	a.pump()
	if a.exists("never.go") || len(a.editors()) != 1 {
		t.Errorf("a cancelled input made a file (editors %v)", a.editors())
	}
	if !a.view().Controller().HasFocus() {
		t.Error("the tree has not the focus back")
	}
}

func TestNewFileInputShowsWhyAnInvalidNameIsRefused(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.focusTree("main.go")
	a.mustRun("explorer.newFile", nil)
	input := gluttertest.ByKey(explorer.EditInputKey)
	a.ts.EnterText(input, "go.mod")
	a.pump()
	want := "A file or folder go.mod already exists at this location. Please choose a different name."
	if !a.text(want) {
		t.Fatalf("no balloon %q", want)
	}
	// Enter keeps the input open
	a.key(services.LogicalKeyboardKeyEnter)
	if !a.view().Editing() {
		t.Fatal("an invalid name was accepted")
	}
	time.Sleep(20 * time.Millisecond)
	a.pump()
	if got := a.read("go.mod"); strings.Contains(got, "created") {
		t.Errorf("go.mod was changed: %q", got)
	}
	// a warning lets the name through
	a.ts.EnterText(input, " spaced.txt")
	a.pump()
	if !a.text("Leading or trailing whitespace detected in file or folder name.") {
		t.Error("no warning for the space")
	}
	// an empty name is an error
	a.ts.EnterText(input, "")
	a.pump()
	a.key(services.LogicalKeyboardKeyEnter)
	if !a.text("A file or folder name must be provided.") || !a.view().Editing() {
		t.Error("an empty name was accepted or has no balloon")
	}
	// a valid name goes through
	a.ts.EnterText(input, "ok.txt")
	a.pump()
	a.key(services.LogicalKeyboardKeyEnter)
	a.waitFor("the file", func() bool { return a.exists("ok.txt") })
}

func TestRenameSelectsTheStemAndRenames(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.focusTree("main.go")
	a.mustRun("renameFile", nil)
	if kind, ok := a.view().EditKind(); !ok || kind != explorer.EditRename {
		t.Fatalf("edit kind %v %v", kind, ok)
	}
	if start, end := a.view().EditSelection(); a.view().EditText() != "main.go" || start != 0 || end != 4 {
		t.Errorf("text %q selection %d-%d, want main.go with main selected", a.view().EditText(), start, end)
	}
	a.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "app.go")
	a.pump()
	a.key(services.LogicalKeyboardKeyEnter)
	a.waitFor("the rename", func() bool { return a.exists("app.go") })
	if a.exists("main.go") {
		t.Error("main.go is still there")
	}
	a.pump()
	if got := a.selected(); got != "app.go" {
		t.Errorf("selected %q, want app.go", got)
	}
	// the editor follows
	if want := []string{"app.go"}; !slices.Equal(a.editors(), want) {
		t.Errorf("editors %v, want %v", a.editors(), want)
	}
	uri, _ := a.group().Model.ActiveEditor().Resource()
	if uri != document.FileURI(a.path("app.go")) {
		t.Errorf("the editor shows %v", uri)
	}
}

func TestRenameOfAFolderSelectsItsWholeName(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.focusTree("docs")
	a.mustRun("renameFile", nil)
	if start, end := a.view().EditSelection(); start != 0 || end != len("docs") {
		t.Errorf("selection %d-%d", start, end)
	}
	// F2 in the input of a file cycles the selection
	a.key(services.LogicalKeyboardKeyEscape)
	a.focusTree("go.mod")
	a.mustRun("renameFile", nil)
	for _, want := range [][2]int{{0, 2}, {0, 6}, {3, 6}, {0, 2}} {
		if s, e := a.view().EditSelection(); s != want[0] || e != want[1] {
			t.Errorf("selection %d-%d, want %v", s, e, want)
		}
		a.key(services.LogicalKeyboardKeyF2)
	}
	a.key(services.LogicalKeyboardKeyEscape)
}

func TestRenameWithTheSameNameOrEscapeDoesNothing(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.focusTree("main.go")
	a.mustRun("renameFile", nil)
	a.key(services.LogicalKeyboardKeyEnter)
	time.Sleep(20 * time.Millisecond)
	a.pump()
	if a.view().Editing() || !a.exists("main.go") || a.win.FileOps.CanUndo() {
		t.Errorf("editing %v, undo %v", a.view().Editing(), a.win.FileOps.CanUndo())
	}
	a.mustRun("renameFile", nil)
	a.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "other.go")
	a.key(services.LogicalKeyboardKeyEscape)
	if a.exists("other.go") || !a.exists("main.go") {
		t.Error("Escape renamed the file")
	}
}

func TestRenameAFileOverAnotherShowsTheError(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.focusTree("main.go")
	a.mustRun("renameFile", nil)
	a.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "go.mod")
	a.pump()
	if !a.text("A file or folder go.mod already exists at this location. Please choose a different name.") {
		t.Error("no balloon")
	}
	a.key(services.LogicalKeyboardKeyEnter)
	if !a.view().Editing() {
		t.Error("the input closed")
	}
}

func TestDeleteAsksAndMovesToTheTrash(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go", "go.mod")
	a.focusTree("main.go")
	a.mustRun("moveFileToTrash", nil)
	if !a.dialog() {
		t.Fatal("no confirmation")
	}
	dialog := gluttertest.ByType[*kit.Dialog]()
	for _, text := range []string{"Are you sure you want to delete 'main.go'?", "You can restore this file from the Trash.", "Do not ask me again", "Move to Trash", "Cancel"} {
		if !a.ts.Exists(gluttertest.Descendant(dialog, gluttertest.ByText(text))) {
			t.Errorf("the dialog has no %q", text)
		}
	}
	// Cancel keeps the file
	a.answer("Cancel")
	if !a.exists("main.go") || a.dialog() {
		t.Fatal("Cancel deleted the file or left the dialog")
	}
	a.mustRun("moveFileToTrash", nil)
	a.answer("Move to Trash")
	a.waitFor("the delete", func() bool { return !a.exists("main.go") })
	a.pump()
	if got := a.trashed(); !slices.Equal(got, []string{"main.go"}) {
		t.Errorf("trash %v", got)
	}
	if got := a.editors(); !slices.Equal(got, []string{"go.mod"}) {
		t.Errorf("editors %v, want the clean editor of main.go closed", got)
	}
	if slices.Contains(a.rows(), "main.go") || slices.Contains(a.rows(), "*main.go") {
		t.Errorf("rows %v", a.rows())
	}
}

func TestDeleteConfirmationCheckboxWritesTheSetting(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.focusTree("go.mod")
	a.mustRun("moveFileToTrash", nil)
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByType[*kit.Checkbox]()))
	a.pump()
	a.answer("Move to Trash")
	a.waitFor("the delete", func() bool { return !a.exists("go.mod") })
	if got := a.win.Settings.Inspect("explorer.confirmDelete", ""); !got.User.Ok || got.User.Value != false {
		t.Errorf("explorer.confirmDelete = %+v, want false in the user settings", got)
	}
	// with the setting off, nothing is asked
	a.focusTree("main.go")
	a.mustRun("moveFileToTrash", nil)
	if a.dialog() {
		t.Fatal("a dialog asks although explorer.confirmDelete is false")
	}
	a.waitFor("the delete", func() bool { return !a.exists("main.go") })
}

func TestDeleteSeveralFilesListsThem(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.focusTree("main.go")
	a.view().Tree().SetSelected(a.path("main.go"))
	a.view().Tree().ClickWith(a.path("go.mod"), kit.ClickModifiers{Ctrl: true}, false, false)
	a.mustRun("moveFileToTrash", nil)
	if !a.text("Are you sure you want to delete the following 2 files?") {
		t.Fatalf("no message for two files (rows %v)", a.rows())
	}
	if d := a.dialogDetail(); !strings.Contains(d, "go.mod") || !strings.Contains(d, "main.go") {
		t.Errorf("detail %q", d)
	}
	a.answer("Move to Trash")
	a.waitFor("both", func() bool { return !a.exists("main.go") && !a.exists("go.mod") })
}

func (a *fileApp) dialogDetail() string {
	spec, _ := a.win.Dialogs.Current()
	return spec.Detail
}

func TestPermanentDelete(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.focusTree("docs")
	a.mustRun("deleteFile", nil)
	if !a.text("Are you sure you want to permanently delete 'docs' and its contents?") || !strings.Contains(a.dialogDetail(), "This action is irreversible!") {
		t.Fatalf("message/detail: %q", a.dialogDetail())
	}
	a.answer("Delete")
	a.waitFor("the delete", func() bool { return !a.exists("docs") })
	if len(a.trashed()) != 0 {
		t.Errorf("the trash has %v", a.trashed())
	}
	if a.win.FileOps.CanUndo() {
		t.Error("a permanent delete is in the undo history")
	}
}

func TestDeleteWithoutTheTrashSettingDeletesForGood(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.setting("files.enableTrash", false)
	a.focusTree("go.mod")
	if a.win.Keys.Get("explorerResourceMoveableToTrash") != false {
		t.Errorf("explorerResourceMoveableToTrash = %v", a.win.Keys.Get("explorerResourceMoveableToTrash"))
	}
	a.mustRun("moveFileToTrash", nil)
	if !a.text("Are you sure you want to permanently delete 'go.mod'?") {
		t.Fatal("not a permanent delete")
	}
	a.answer("Delete")
	a.waitFor("the delete", func() bool { return !a.exists("go.mod") })
	if len(a.trashed()) != 0 {
		t.Errorf("trash %v", a.trashed())
	}
}

func TestDeleteWhenTheTrashFailsOffersToDeleteForGood(t *testing.T) {
	a := newFileApp(t, projectFiles)
	// a trash that cannot be made: its folder is below a file
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.win.FileOps = workspace.NewFileOps(workspace.FileOpsOptions{Trash: fileio.NewHomeTrash(filepath.Join(blocker, "Trash"))})
	a.focusTree("go.mod")
	a.mustRun("moveFileToTrash", nil)
	a.answer("Move to Trash")
	a.waitFor("the question", func() bool {
		return a.text("Failed to delete using the Trash. Do you want to permanently delete instead?")
	})
	if !a.exists("go.mod") {
		t.Fatal("the file was lost")
	}
	a.answer("Delete Permanently")
	a.waitFor("the delete", func() bool { return !a.exists("go.mod") })
}

func TestDeleteKeepsEditorsWithUnsavedChanges(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go", "go.mod")
	// edit go.mod
	doc, _ := a.win.Documents.Get(document.FileURI(a.path("go.mod")))
	if _, err := doc.Edit([]document.Edit{{Text: "// changed\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	a.setting("explorer.confirmDelete", false)
	a.focusTree("go.mod")
	a.mustRun("moveFileToTrash", nil)
	// there are unsaved changes: the user is asked
	if !a.text("You are deleting go.mod with unsaved changes. Do you want to continue?") {
		t.Fatalf("no warning about the changes (dialog %v)", a.dialog())
	}
	a.answer("Move to Trash")
	a.waitFor("the delete", func() bool { return !a.exists("go.mod") })
	a.pump()
	if got := a.editors(); !slices.Contains(got, "go.mod") {
		t.Errorf("editors %v, want the editor with unsaved changes kept", got)
	}
	// and a clean editor of a deleted file closes
	a.focusTree("main.go")
	a.mustRun("moveFileToTrash", nil)
	a.waitFor("the delete", func() bool { return !a.exists("main.go") })
	a.pump()
	if got := a.editors(); slices.Contains(got, "main.go") {
		t.Errorf("editors %v, want main.go closed", got)
	}
}

func TestCopyAndPasteMakeCopyNames(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	for i, want := range []string{"main copy.go", "main copy 2.go", "main copy 3.go"} {
		a.focusTree("main.go")
		a.mustRun("filesExplorer.copy", nil)
		if a.win.Keys.Get("fileCopied") != true {
			t.Fatal("fileCopied is not set")
		}
		a.mustRun("filesExplorer.paste", nil)
		a.waitFor(want, func() bool { return a.exists(want) })
		if !a.exists("main.go") {
			t.Fatalf("paste %d lost the source", i)
		}
	}
	// the copy opened in an editor (the last one)
	a.pump()
	if got := a.group().Model.ActiveEditor().Name(); got != "main copy 3.go" {
		t.Errorf("active editor %q", got)
	}
}

func TestCopyIntoAFolderKeepsTheNameAndTheSourceIsTheClipboard(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.focusTree("main.go")
	a.mustRun("filesExplorer.copy", nil)
	a.focusTree("docs")
	a.mustRun("filesExplorer.paste", nil)
	a.waitFor("the copy", func() bool { return a.exists("docs/main.go") })
	if a.read("docs/main.go") != a.read("main.go") {
		t.Error("the copy differs")
	}
	// paste on a file pastes into its folder
	a.focusTree("docs/guide.md")
	a.mustRun("filesExplorer.paste", nil)
	a.waitFor("the second copy", func() bool { return a.exists("docs/main copy.go") })
	// the clipboard has the files as a list of URIs, for the file manager
	var got []string
	a.shell().Context() // keep the shell alive
	a.ts.Binding.Clipboard().GetData(a.win.ctx, services.ClipboardKURIList, func(d *services.ClipboardData, err error) {
		if err == nil && d != nil {
			got = d.URIs
		}
	})
	a.pump()
	if want := []string{"file://" + a.path("main.go")}; !slices.Equal(got, want) {
		t.Errorf("clipboard URIs %v, want %v", got, want)
	}
}

func TestCutAndPasteMoveAndDimTheItem(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.focusTree("main.go")
	a.mustRun("filesExplorer.cut", nil)
	if got := a.view().CutPaths(); !slices.Equal(got, []string{a.path("main.go")}) {
		t.Fatalf("cut %v", got)
	}
	if a.win.Keys.Get("explorerResourceCut") != true {
		t.Errorf("explorerResourceCut = %v", a.win.Keys.Get("explorerResourceCut"))
	}
	// the row shows dimmed
	row := inTree(gluttertest.ByText("main.go"))
	if !a.ts.Exists(gluttertest.Ancestor(row, gluttertest.ByType[*widgets.Opacity]())) {
		t.Error("the cut row is not dimmed")
	}
	a.focusTree("docs")
	a.mustRun("filesExplorer.paste", nil)
	a.waitFor("the move", func() bool { return a.exists("docs/main.go") && !a.exists("main.go") })
	if len(a.view().CutPaths()) != 0 || a.win.Keys.Get("explorerResourceCut") != false {
		t.Error("the cut state stays after the paste")
	}
	// the editor follows the move
	a.pump()
	uri, _ := a.group().Model.ActiveEditor().Resource()
	if uri.Path != a.path("docs/main.go") {
		t.Errorf("the editor shows %v", uri)
	}
	if a.ts.Exists(gluttertest.Ancestor(inTree(gluttertest.ByText("main.go")), gluttertest.ByType[*widgets.Opacity]())) {
		t.Error("a row is dimmed after the paste")
	}
}

func TestCancelCutRestoresTheRow(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.focusTree("go.mod")
	a.mustRun("filesExplorer.cut", nil)
	a.mustRun("filesExplorer.cancelCut", nil)
	if len(a.view().CutPaths()) != 0 || a.win.Keys.Get("explorerResourceCut") != false {
		t.Error("the cut stays")
	}
	a.focusTree("docs")
	a.mustRun("filesExplorer.paste", nil)
	time.Sleep(30 * time.Millisecond)
	a.pump()
	// nothing is pasted any more: the cancelled cut cleared the internal clipboard but the system clipboard still
	// has the file, which pastes as a copy
	if a.exists("docs/go.mod") && !a.exists("go.mod") {
		t.Error("the cancelled cut moved the file")
	}
}

func TestPasteOverAnExistingNameAsksWithoutIncrementalNaming(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.setting("explorer.incrementalNaming", "disabled")
	if err := os.WriteFile(a.path("docs/main.go"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.focusTree("main.go")
	a.mustRun("filesExplorer.copy", nil)
	a.focusTree("docs")
	a.mustRun("filesExplorer.paste", nil)
	if !a.text("A file or folder with the name 'main.go' already exists in the destination folder. Do you want to replace it?") {
		t.Fatal("no overwrite question")
	}
	a.answer("Cancel")
	time.Sleep(20 * time.Millisecond)
	a.pump()
	if a.read("docs/main.go") != "old" {
		t.Error("Cancel replaced the file")
	}
	a.mustRun("filesExplorer.paste", nil)
	a.answer("Replace")
	a.waitFor("the replace", func() bool { return a.read("docs/main.go") != "old" })
}

func TestCutPasteOverAnExistingNameAsks(t *testing.T) {
	a := newFileApp(t, projectFiles)
	if err := os.WriteFile(a.path("docs/main.go"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.focusTree("main.go")
	a.mustRun("filesExplorer.cut", nil)
	a.focusTree("docs")
	a.mustRun("filesExplorer.paste", nil)
	if !a.dialog() {
		t.Fatal("no overwrite question")
	}
	a.answer("Replace")
	a.waitFor("the move", func() bool { return !a.exists("main.go") && a.read("docs/main.go") != "old" })
	if got := a.trashed(); !slices.Equal(got, []string{"main.go"}) {
		t.Errorf("the replaced file is not in the trash: %v", got)
	}
}

func TestPasteIntoItselfIsRefused(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.focusTree("docs")
	a.mustRun("filesExplorer.cut", nil)
	a.focusTree("docs/more")
	a.mustRun("filesExplorer.paste", nil)
	time.Sleep(20 * time.Millisecond)
	a.pump()
	if !a.exists("docs/guide.md") || a.exists("docs/more/docs") {
		t.Error("a folder was moved into itself")
	}
	var found bool
	for _, it := range a.win.Notify.Items() {
		found = found || strings.Contains(it.Message, "ancestor")
	}
	if !found {
		t.Error("no error was reported")
	}
}

func TestPasteTheFilesOfAFileManager(t *testing.T) {
	a := newFileApp(t, projectFiles)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("from the file manager"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.ts.Binding.Clipboard().SetData(a.win.ctx, services.ClipboardData{Text: outside, URIs: fileURIs([]string{outside})}, nil)
	a.pump()
	a.setting("explorer.confirmPasteNative", false)
	a.focusTree("docs")
	a.mustRun("filesExplorer.paste", nil)
	a.waitFor("the paste", func() bool { return a.exists("docs/outside.txt") })
	if _, err := os.Stat(outside); err != nil {
		t.Error("the file of the file manager was moved")
	}
}

func TestPasteNativeFilesAsksWhenTheSettingSaysSo(t *testing.T) {
	a := newFileApp(t, projectFiles)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.ts.Binding.Clipboard().SetData(a.win.ctx, services.ClipboardData{Text: outside, URIs: fileURIs([]string{outside})}, nil)
	a.pump()
	a.focusTree("docs")
	a.mustRun("filesExplorer.paste", nil)
	if !a.text("Are you sure you want to paste 'outside.txt'?") {
		t.Fatal("no question")
	}
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByType[*kit.Checkbox]()))
	a.pump()
	a.answer("Paste")
	a.waitFor("the paste", func() bool { return a.exists("docs/outside.txt") })
	if got := a.win.Settings.Inspect("explorer.confirmPasteNative", ""); !got.User.Ok || got.User.Value != false {
		t.Errorf("explorer.confirmPasteNative = %+v", got)
	}
}

func TestUndoAndRedoOfFileOperations(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go", "go.mod")
	a.setting("explorer.confirmDelete", false)
	a.setting("explorer.confirmUndo", "light")

	// delete, then undo restores from the trash
	a.focusTree("go.mod")
	a.mustRun("moveFileToTrash", nil)
	a.waitFor("the delete", func() bool { return !a.exists("go.mod") })
	a.focusTree("main.go")
	a.mustRun("undo", nil)
	a.waitFor("the restore", func() bool { return a.exists("go.mod") })
	if len(a.trashed()) != 0 {
		t.Errorf("the trash has %v", a.trashed())
	}
	a.mustRun("redo", nil)
	a.waitFor("the second delete", func() bool { return !a.exists("go.mod") })
	a.mustRun("undo", nil)
	a.waitFor("the restore", func() bool { return a.exists("go.mod") })

	// rename, undo: the editor follows both ways
	a.focusTree("main.go")
	a.mustRun("renameFile", nil)
	a.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "app.go")
	a.pump()
	a.key(services.LogicalKeyboardKeyEnter)
	a.waitFor("the rename", func() bool { return a.exists("app.go") })
	a.pump()
	if !slices.Contains(a.editors(), "app.go") {
		t.Fatalf("editors %v", a.editors())
	}
	a.focusTree("app.go")
	a.mustRun("undo", nil)
	a.waitFor("the undo", func() bool { return a.exists("main.go") && !a.exists("app.go") })
	a.pump()
	if got := a.editors(); !slices.Contains(got, "main.go") || slices.Contains(got, "app.go") {
		t.Errorf("editors after undo %v", got)
	}
	a.mustRun("redo", nil)
	a.waitFor("the redo", func() bool { return a.exists("app.go") })
	a.pump()
	if got := a.editors(); !slices.Contains(got, "app.go") {
		t.Errorf("editors after redo %v", got)
	}
	// an undo of nothing is quiet
	a.win.FileOps.ClearHistory()
	a.mustRun("undo", nil)
}

func TestUndoOfACreateAsksFirst(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.focusTree("main.go")
	a.mustRun("explorer.newFile", nil)
	a.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "tmp.txt")
	a.pump()
	a.key(services.LogicalKeyboardKeyEnter)
	a.waitFor("the file", func() bool { return a.exists("tmp.txt") })
	a.focusTree("tmp.txt")
	a.mustRun("undo", nil)
	if !a.text("Would you like to undo 'Create tmp.txt'?") {
		t.Fatal("no question")
	}
	a.answer("Yes")
	a.waitFor("the undo", func() bool { return !a.exists("tmp.txt") })
	// the editor of the file closed
	a.pump()
	if slices.Contains(a.editors(), "tmp.txt") {
		t.Errorf("editors %v", a.editors())
	}
}

func TestUndoCanBeTurnedOff(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.setting("explorer.confirmDelete", false)
	a.setting("explorer.enableUndo", false)
	a.focusTree("go.mod")
	a.mustRun("moveFileToTrash", nil)
	a.waitFor("the delete", func() bool { return !a.exists("go.mod") })
	a.focusTree("main.go")
	a.mustRun("undo", nil)
	time.Sleep(30 * time.Millisecond)
	a.pump()
	if a.exists("go.mod") {
		t.Error("undo worked although explorer.enableUndo is false")
	}
}

func TestEditorsFollowTheRenameOfAFile(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go", "go.mod")
	doc, _ := a.win.Documents.Get(document.FileURI(a.path("main.go")))
	if _, err := doc.Edit([]document.Edit{{Text: "// unsaved\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if lang := doc.LanguageID(); lang != "go" {
		t.Fatalf("language %q", lang)
	}
	a.focusTree("main.go")
	a.mustRun("renameFile", nil)
	a.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "main.md")
	a.pump()
	a.key(services.LogicalKeyboardKeyEnter)
	a.waitFor("the rename", func() bool { return a.exists("main.md") })
	a.pump()
	if got := a.editors(); !slices.Equal(got, []string{"main.md", "go.mod"}) {
		t.Fatalf("tabs %v", got)
	}
	// the same document: its unsaved changes, its history and its dirty mark stay; the language follows the extension
	if now, ok := a.win.Documents.Get(document.FileURI(a.path("main.md"))); !ok || now != doc {
		t.Fatal("the document is not at the new path")
	}
	if _, ok := a.win.Documents.Get(document.FileURI(a.path("main.go"))); ok {
		t.Error("the document is still at the old path")
	}
	if !doc.Dirty() || !doc.CanUndo() || !strings.HasPrefix(doc.Text(), "// unsaved") {
		t.Errorf("dirty %v undo %v text %q", doc.Dirty(), doc.CanUndo(), doc.Text())
	}
	if doc.LanguageID() != "markdown" {
		t.Errorf("language %q, want markdown", doc.LanguageID())
	}
	// the tab, the title and the explorer selection
	active := a.group().Model.ActiveEditor()
	if active.Name() != "main.md" || !active.Dirty() || !strings.HasSuffix(active.Title(), "main.md") {
		t.Errorf("tab %q dirty %v title %q", active.Name(), active.Dirty(), active.Title())
	}
	if got := a.selected(); got != "main.md" {
		t.Errorf("selected %q", got)
	}
	if !a.ts.Exists(gluttertest.ByText("main.md")) {
		t.Error("the tab shows no new name")
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestEditorsFollowTheRenameOfAFolder(t *testing.T) {
	a := newFileApp(t, projectFiles, "internal/server/server.go", "internal/server/util.go", "main.go")
	a.focusTree("internal")
	a.mustRun("renameFile", nil)
	a.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "pkg")
	a.pump()
	a.key(services.LogicalKeyboardKeyEnter)
	a.waitFor("the rename", func() bool { return a.exists("pkg/server/server.go") })
	a.pump()
	var paths []string
	for _, e := range a.group().Model.Editors(editorgroup.Sequential, false) {
		p, _ := fileResource(e)
		paths = append(paths, p)
	}
	want := []string{a.path("pkg/server/server.go"), a.path("pkg/server/util.go"), a.path("main.go")}
	if !slices.Equal(paths, want) {
		t.Errorf("editors show %v, want %v", paths, want)
	}
	for _, p := range want {
		if _, ok := a.win.Documents.Get(document.FileURI(p)); !ok {
			t.Errorf("no document at %s", p)
		}
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestDeletingAFolderClosesTheEditorsBelowIt(t *testing.T) {
	a := newFileApp(t, projectFiles, "internal/server/server.go", "internal/server/util.go", "main.go")
	a.setting("explorer.confirmDelete", false)
	a.focusTree("internal")
	a.mustRun("moveFileToTrash", nil)
	a.waitFor("the delete", func() bool { return !a.exists("internal") })
	a.pump()
	if got := a.editors(); !slices.Equal(got, []string{"main.go"}) {
		t.Errorf("editors %v", got)
	}
	a.focusTree("main.go")
	a.mustRun("undo", nil)
	a.waitFor("the restore", func() bool { return a.exists("internal/server/server.go") })
}

func TestExternalDeleteKeepsTheEditorUnlessAsked(t *testing.T) {
	for _, closeOnDelete := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "closeOnFileDelete"}[closeOnDelete], func(t *testing.T) {
			a := newFileApp(t, projectFiles, "main.go", "go.mod")
			if closeOnDelete {
				a.setting("workbench.editor.closeOnFileDelete", true)
			}
			// another program deletes the file: the watcher reports it
			if err := os.Remove(a.path("main.go")); err != nil {
				t.Fatal(err)
			}
			a.shell().externalFileEvents([]workspace.Event{{Kind: workspace.EventDeleted, Path: a.path("main.go")}})
			a.pump()
			has := slices.Contains(a.editors(), "main.go")
			if has == closeOnDelete {
				t.Errorf("editors %v with closeOnFileDelete %v", a.editors(), closeOnDelete)
			}
		})
	}
	t.Run("an editor with unsaved changes stays", func(t *testing.T) {
		a := newFileApp(t, projectFiles, "main.go")
		a.setting("workbench.editor.closeOnFileDelete", true)
		doc, _ := a.win.Documents.Get(document.FileURI(a.path("main.go")))
		if _, err := doc.Edit([]document.Edit{{Text: "x"}}, document.EditOptions{}); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(a.path("main.go"))
		a.shell().externalFileEvents([]workspace.Event{{Kind: workspace.EventDeleted, Path: a.path("main.go")}})
		a.pump()
		if !slices.Contains(a.editors(), "main.go") {
			t.Error("the editor with unsaved changes closed")
		}
	})
	t.Run("a faulty event for a file that is there changes nothing", func(t *testing.T) {
		a := newFileApp(t, projectFiles, "main.go")
		a.setting("workbench.editor.closeOnFileDelete", true)
		a.shell().externalFileEvents([]workspace.Event{{Kind: workspace.EventDeleted, Path: a.path("main.go")}})
		a.pump()
		if !slices.Contains(a.editors(), "main.go") {
			t.Error("the editor closed")
		}
	})
}

// rowCenter is the center of the row of a label in the tree.
func (a *fileApp) rowCenter(label string) ui.Offset {
	a.t.Helper()
	return a.ts.Center(inTree(gluttertest.ByText(label)))
}

var mouseDown = gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.PrimaryButton)}

// dragRow drags the row of a label to a position and releases it there; hold gives the time to rest over the target.
func (a *fileApp) dragRow(from string, to ui.Offset, rest time.Duration) {
	a.t.Helper()
	start := a.rowCenter(from)
	g := a.ts.StartGesture(start, mouseDown)
	g.MoveTo(ui.Offset{Dx: start.Dx, Dy: start.Dy + 6})
	g.MoveTo(to)
	a.ts.Pump(time.Millisecond)
	if rest > 0 {
		a.ts.Pump(rest)
	}
	g.Up()
	a.pump()
}

func TestDragARowToAFolderMovesItAfterAsking(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.dragRow("main.go", a.rowCenter("docs"), 0)
	if !a.text("Are you sure you want to move 'main.go' into 'docs'?") {
		t.Fatalf("no question (dialog %v)", a.dialog())
	}
	a.answer("Move")
	a.waitFor("the move", func() bool { return a.exists("docs/main.go") && !a.exists("main.go") })
	a.pump()
	uri, _ := a.group().Model.ActiveEditor().Resource()
	if uri.Path != a.path("docs/main.go") {
		t.Errorf("the editor shows %v", uri)
	}
}

func TestDragAndDropMoveHasItsCheckboxAndSetting(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.dragRow("go.mod", a.rowCenter("docs"), 0)
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByType[*kit.Checkbox]()))
	a.pump()
	a.answer("Move")
	a.waitFor("the move", func() bool { return a.exists("docs/go.mod") })
	if got := a.win.Settings.Inspect("explorer.confirmDragAndDrop", ""); !got.User.Ok || got.User.Value != false {
		t.Errorf("explorer.confirmDragAndDrop = %+v", got)
	}
	// without the confirmation the drop moves at once
	a.dragRow("main.go", a.rowCenter("docs"), 0)
	if a.dialog() {
		t.Fatal("a dialog asks although explorer.confirmDragAndDrop is false")
	}
	a.waitFor("the second move", func() bool { return a.exists("docs/main.go") })
}

func TestDragWithControlCopies(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.setting("explorer.confirmDragAndDrop", false)
	a.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	a.dragRow("main.go", a.rowCenter("docs"), 0)
	a.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	a.waitFor("the copy", func() bool { return a.exists("docs/main.go") })
	if !a.exists("main.go") {
		t.Error("a copy removed the source")
	}
}

func TestDragCannotDropAFolderOnItselfOrOnItsParent(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.setting("explorer.confirmDragAndDrop", false)
	a.focusTree("internal/server")
	a.pump()
	// onto its own parent: it is there already
	a.dragRow("server", a.rowCenter("internal"), 0)
	time.Sleep(20 * time.Millisecond)
	a.pump()
	if a.dialog() || !a.exists("internal/server/server.go") {
		t.Error("the folder moved onto its parent")
	}
	// onto itself
	a.dragRow("server", a.rowCenter("server"), 0)
	time.Sleep(20 * time.Millisecond)
	a.pump()
	if !a.exists("internal/server/server.go") || a.exists("internal/server/server") {
		t.Error("the folder moved onto itself")
	}
}

func TestDragRestingOnAFolderExpandsIt(t *testing.T) {
	a := newFileApp(t, projectFiles)
	if slices.Contains(a.rows(), "internal/server/") {
		t.Fatal("internal is open already")
	}
	start := a.rowCenter("main.go")
	g := a.ts.StartGesture(start, mouseDown)
	g.MoveTo(ui.Offset{Dx: start.Dx, Dy: start.Dy - 6})
	g.MoveTo(a.rowCenter("internal"))
	a.ts.Pump(kit.DefaultAutoExpandDelay + 100*time.Millisecond)
	a.pump()
	if !slices.Contains(a.rows(), "internal/server/") {
		t.Errorf("rows %v, want internal expanded after 500 ms", a.rows())
	}
	g.Up()
	a.pump()
	a.answerIfAsked("Move")
}

func (a *fileApp) answerIfAsked(label string) {
	if a.dialog() {
		a.answer(label)
	}
}

func TestDragAndDropCanBeTurnedOff(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.setting("explorer.enableDragAndDrop", false)
	a.dragRow("main.go", a.rowCenter("docs"), 0)
	time.Sleep(20 * time.Millisecond)
	a.pump()
	if a.dialog() || !a.exists("main.go") {
		t.Error("a row was dragged although explorer.enableDragAndDrop is false")
	}
}

func TestDropFilesFromTheFileManager(t *testing.T) {
	a := newFileApp(t, projectFiles)
	outside := t.TempDir()
	one := filepath.Join(outside, "dropped one.txt")
	two := filepath.Join(outside, "two.txt")
	for _, p := range []string{one, two} {
		if err := os.WriteFile(p, []byte("from "+filepath.Base(p)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	types := []string{"text/uri-list"}
	over := a.rowCenter("docs")
	a.ts.Drop(0, ui.DropPhaseEnter, over, ui.DropEvent{Types: types, Actions: ui.DropActionCopy})
	a.ts.Drop(0, ui.DropPhaseMove, over, ui.DropEvent{Types: types, Actions: ui.DropActionCopy})
	a.ts.Pump(time.Millisecond)
	if a.view().DropOver() == "" {
		t.Error("the folder under the drag is not marked")
	}
	got := a.ts.Drop(0, ui.DropPhaseDrop, over, ui.DropEvent{
		Types: types, Actions: ui.DropActionCopy,
		URIs: []string{(&url.URL{Scheme: "file", Path: one}).String(), (&url.URL{Scheme: "file", Path: two}).String()},
	})
	if got != ui.DropActionCopy {
		t.Errorf("the drop was answered with %v", got)
	}
	a.waitFor("the import", func() bool { return a.exists("docs/dropped one.txt") && a.exists("docs/two.txt") })
	if a.read("docs/two.txt") != "from two.txt" {
		t.Error("the content differs")
	}
	if _, err := os.Stat(one); err != nil {
		t.Error("the dropped file was moved")
	}
	if a.view().DropOver() != "" {
		t.Error("the mark stays")
	}
	// an undo takes the copies away
	a.win.Notify.ClearAll()
	if !a.win.FileOps.CanUndo() {
		t.Error("the import is not in the history")
	}
}

func TestDropOneFileOpensItAndAsksBeforeReplacing(t *testing.T) {
	a := newFileApp(t, projectFiles)
	outside := t.TempDir()
	src := filepath.Join(outside, "main.go")
	if err := os.WriteFile(src, []byte("package dropped\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	types := []string{"text/uri-list"}
	drop := func(at ui.Offset) {
		a.ts.Drop(0, ui.DropPhaseEnter, at, ui.DropEvent{Types: types, Actions: ui.DropActionCopy})
		a.ts.Drop(0, ui.DropPhaseMove, at, ui.DropEvent{Types: types, Actions: ui.DropActionCopy})
		a.ts.Drop(0, ui.DropPhaseDrop, at, ui.DropEvent{Types: types, Actions: ui.DropActionCopy, URIs: fileURIs([]string{src})})
		a.pump()
	}
	// onto the empty part of the tree: the folder of the window, where main.go is
	drop(ui.Offset{Dx: 100, Dy: 500})
	if !a.text("A file or folder with the name 'main.go' already exists in the destination folder. Do you want to replace it?") {
		t.Fatal("no question")
	}
	a.answer("Cancel")
	time.Sleep(20 * time.Millisecond)
	a.pump()
	if strings.Contains(a.read("main.go"), "dropped") {
		t.Error("Cancel replaced the file")
	}
	drop(ui.Offset{Dx: 100, Dy: 500})
	a.answer("Replace")
	a.waitFor("the replace", func() bool { return strings.Contains(a.read("main.go"), "dropped") })
	a.pump()
	if got := a.group().Model.ActiveEditor(); got == nil || got.Name() != "main.go" {
		t.Error("the dropped file did not open")
	}
	// a drop of something that is not a file is not accepted
	if got := a.ts.Drop(0, ui.DropPhaseEnter, a.rowCenter("docs"), ui.DropEvent{Types: []string{"text/plain"}, Actions: ui.DropActionCopy}); got != ui.DropActionNone {
		t.Errorf("text was accepted: %v", got)
	}
	a.ts.Drop(0, ui.DropPhaseLeave, ui.Offset{}, ui.DropEvent{})
}

// chord sends a chord of keys ("ctrl+c") through the key dispatcher and the focus.
func (a *fileApp) chord(text string) {
	a.t.Helper()
	k, err := keybinding.ParseKeybinding(text)
	if err != nil {
		a.t.Fatal(err)
	}
	for _, c := range k {
		sendChord(a.ts, c)
	}
	a.pump()
}

func TestTheKeysOfTheFileOperationsWorkInTheTree(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.setting("explorer.confirmUndo", "light")

	// F2 renames
	a.focusTree("go.mod")
	a.chord("f2")
	if kind, ok := a.view().EditKind(); !ok || kind != explorer.EditRename {
		t.Fatalf("F2 did not start a rename (%v %v)", kind, ok)
	}
	// while the input has the focus, the keys are the input's: Delete does not delete the file, F2 cycles
	a.chord("delete")
	if a.dialog() || !a.exists("go.mod") {
		t.Fatal("Delete in the input deleted the file")
	}
	a.key(services.LogicalKeyboardKeyEscape)
	if a.view().Editing() {
		t.Fatal("Escape did not close the input")
	}

	// Ctrl+C and Ctrl+V copy
	a.focusTree("go.mod")
	a.chord("ctrl+c")
	a.focusTree("docs")
	a.chord("ctrl+v")
	a.waitFor("the paste", func() bool { return a.exists("docs/go.mod") })

	// Ctrl+X dims, Escape cancels
	a.focusTree("main.go")
	a.chord("ctrl+x")
	if len(a.view().CutPaths()) != 1 {
		t.Fatal("Ctrl+X cut nothing")
	}
	a.chord("escape")
	if len(a.view().CutPaths()) != 0 {
		t.Error("Escape did not cancel the cut")
	}

	// Ctrl+Z undoes the paste (a paste asks in the default level; "light" does not)
	a.focusTree("docs/go.mod")
	a.chord("ctrl+z")
	a.waitFor("the undo", func() bool { return !a.exists("docs/go.mod") })

	// Delete moves to the trash, Shift+Delete deletes for good
	a.setting("explorer.confirmDelete", false)
	a.focusTree("go.mod")
	a.chord("delete")
	a.waitFor("the trash", func() bool { return !a.exists("go.mod") })
	// (the undo of the paste put its copy in the trash too)
	if got := a.trashed(); !slices.Equal(got, []string{"go.mod", "go.mod.2"}) {
		t.Errorf("trash %v", got)
	}
	a.setting("explorer.confirmDelete", true)
	a.focusTree("docs/guide.md")
	a.chord("shift+delete")
	if !a.dialog() {
		t.Fatal("Shift+Delete asked nothing")
	}
	a.answer("Delete")
	a.waitFor("the delete", func() bool { return !a.exists("docs/guide.md") })
	if len(a.trashed()) != 2 {
		t.Errorf("trash %v", a.trashed())
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestTheFileKeysDoNothingOutsideTheTree(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	// the editor has the focus: Delete deletes text, not the file
	a.mustRun("workbench.action.focusActiveEditorGroup", nil)
	a.chord("delete")
	a.chord("f2")
	if a.dialog() || a.view().Editing() || !a.exists("main.go") {
		t.Error("the keys of the Explorer worked in the editor")
	}
}

func TestTheContextMenuOfTheExplorer(t *testing.T) {
	tests := []struct {
		name  string
		setup func(a *fileApp)
		on    string // label of the row; empty is the empty part of the tree
		want  []string
	}{
		{"a file", func(a *fileApp) {}, "main.go", []string{
			"Open to the Side", "Open Containing Folder", "-", "Select for Compare", "-", "Cut", "Copy", "-", "Copy Path", "Copy Relative Path", "-", "Rename...", "Delete"}},
		{"a folder", func(a *fileApp) {}, "docs", []string{
			"New File...", "New Folder...", "Open Containing Folder", "-", "Cut", "Copy", "Paste~", "-", "Copy Path", "Copy Relative Path", "-", "Rename...", "Delete"}},
		{"a folder with something copied", func(a *fileApp) {
			a.focusTree("main.go")
			a.mustRun("filesExplorer.copy", nil)
		}, "docs", []string{
			"New File...", "New Folder...", "Open Containing Folder", "-", "Cut", "Copy", "Paste", "-", "Copy Path", "Copy Relative Path", "-", "Rename...", "Delete"}},
		{"without the trash", func(a *fileApp) { a.setting("files.enableTrash", false) }, "main.go", []string{
			"Open to the Side", "Open Containing Folder", "-", "Select for Compare", "-", "Cut", "Copy", "-", "Copy Path", "Copy Relative Path", "-", "Rename...", "Delete Permanently"}},
		{"a file when another one is selected for compare", func(a *fileApp) {
			a.focusTree("go.mod")
			a.mustRun("selectForCompare", nil)
		}, "main.go", []string{
			"Open to the Side", "Open Containing Folder", "-", "Compare with Selected", "Select for Compare", "-", "Cut", "Copy", "-", "Copy Path", "Copy Relative Path", "-", "Rename...", "Delete"}},
		{"a folder when a file is selected for compare", func(a *fileApp) {
			a.focusTree("go.mod")
			a.mustRun("selectForCompare", nil)
		}, "docs", []string{
			"New File...", "New Folder...", "Open Containing Folder", "-", "Cut", "Copy", "Paste~", "-", "Copy Path", "Copy Relative Path", "-", "Rename...", "Delete"}},
		{"a file of two that are selected", func(a *fileApp) {
			a.view().Tree().SetSelected(a.path("go.mod"))
			a.view().Tree().ClickWith(a.path("main.go"), kit.ClickModifiers{Ctrl: true}, false, false)
			a.view().Tree().SetFocus(a.path("main.go"))
		}, "main.go", []string{
			"Open to the Side", "Open Containing Folder", "-", "Compare Selected", "-", "Cut", "Copy", "-", "Copy Path", "Copy Relative Path", "-", "Rename...", "Delete"}},
		{"the empty part of the tree is the folder of the window", func(a *fileApp) {}, "", []string{
			"New File...", "New Folder...", "Open Containing Folder", "-", "Paste~", "-", "Copy Path", "Copy Relative Path"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newFileApp(t, projectFiles)
			tt.setup(a)
			at := ui.Offset{Dx: 100, Dy: 500}
			if tt.on != "" {
				at = a.rowCenter(tt.on)
			}
			a.ts.TapAt(at, secondaryDown)
			a.pump()
			if got := menuRows(a.openMenu0()); !slices.Equal(got, tt.want) {
				t.Errorf("the menu is\n %v\nwant\n %v", got, tt.want)
			}
		})
	}
}

// openMenu0 is the menu that is open.
func (a *fileApp) openMenu0() []kit.MenuItem {
	a.t.Helper()
	els := a.ts.Elements(gluttertest.ByType[*kit.Menu]())
	if len(els) == 0 {
		a.t.Fatal("no menu is open")
	}
	return els[0].Widget().(*kit.Menu).Items()
}

func TestTheContextMenuRunsOnTheRowThatWasPressed(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.focusTree("main.go")
	// a right click on another row selects it and the commands work on it
	a.ts.TapAt(a.rowCenter("go.mod"), secondaryDown)
	a.pump()
	if got := a.selected(); got != "go.mod" {
		t.Fatalf("selected %q", got)
	}
	a.chooseMenuRow("Rename...")
	if kind, ok := a.view().EditKind(); !ok || kind != explorer.EditRename || a.view().EditText() != "go.mod" {
		t.Fatalf("rename: %v %v %q", kind, ok, a.view().EditText())
	}
	a.key(services.LogicalKeyboardKeyEscape)
	// new file from the menu of a folder
	a.ts.TapAt(a.rowCenter("docs"), secondaryDown)
	a.pump()
	a.chooseMenuRow("New File...")
	if kind, ok := a.view().EditKind(); !ok || kind != explorer.EditNewFile {
		t.Fatalf("new file: %v %v", kind, ok)
	}
	a.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "from-menu.txt")
	a.pump()
	a.key(services.LogicalKeyboardKeyEnter)
	a.waitFor("the file", func() bool { return a.exists("docs/from-menu.txt") })
}

func (a *fileApp) chooseMenuRow(label string) {
	a.t.Helper()
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Menu](), gluttertest.ByText(label)))
	a.pump()
	a.pump()
}

func TestCopyPathCommandsUseTheSelectionOfTheExplorer(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	read := func() string {
		var text string
		a.ts.Binding.Clipboard().GetData(a.win.ctx, services.ClipboardKTextPlain, func(d *services.ClipboardData, err error) {
			if err == nil && d != nil {
				text = d.Text
			}
		})
		a.pump()
		return text
	}
	a.focusTree("docs/guide.md")
	a.mustRun("copyFilePath", nil)
	if got := read(); got != a.path("docs/guide.md") {
		t.Errorf("copyFilePath = %q", got)
	}
	a.mustRun("copyRelativeFilePath", nil)
	if got := read(); got != "docs/guide.md" {
		t.Errorf("copyRelativeFilePath = %q", got)
	}
	// several items go on lines
	a.view().Tree().SetSelected(a.path("go.mod"))
	a.view().Tree().ClickWith(a.path("docs/guide.md"), kit.ClickModifiers{Ctrl: true}, false, false)
	a.view().Tree().SetFocus(a.path("docs/guide.md"))
	a.mustRun("copyRelativeFilePath", nil)
	if got := read(); got != "docs/guide.md\ngo.mod" && got != "go.mod\ndocs/guide.md" {
		t.Errorf("two paths: %q", got)
	}
	// a path in the argument wins; the backslash separator
	a.setting("explorer.copyRelativePathSeparator", "\\")
	a.mustRun("copyRelativeFilePath", a.path("docs/more/deep.md"))
	if got := read(); got != `docs\more\deep.md` {
		t.Errorf("with the backslash: %q", got)
	}
	a.setting("explorer.copyPathSeparator", "auto")
	a.mustRun("copyFilePath", []string{a.path("main.go")})
	if got := read(); got != a.path("main.go") {
		t.Errorf("a path in a list: %q", got)
	}
	// without the explorer focus the tab's file is copied
	a.mustRun("workbench.action.focusActiveEditorGroup", nil)
	a.mustRun("copyFilePath", nil)
	if got := read(); got != a.path("main.go") {
		t.Errorf("the active editor: %q", got)
	}
}

func TestOpenToTheSide(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.focusTree("go.mod")
	a.chord("ctrl+enter")
	a.waitFor("the second group", func() bool { return a.shell().group.grid.Count() == 2 })
	a.pump()
	groups := a.shell().group.grid.Groups(editorgroup.GridAppearance)
	if len(groups) != 2 || groups[1].Model.ActiveEditor() == nil || groups[1].Model.ActiveEditor().Name() != "go.mod" {
		t.Fatalf("groups: %d", len(groups))
	}
	if groups[0].Model.ActiveEditor().Name() != "main.go" {
		t.Error("the first group changed")
	}
	// a folder is not opened
	a.focusTree("docs")
	a.mustRun("explorer.openToSide", nil)
	a.pump()
	if got := a.shell().group.grid.Count(); got != 2 {
		t.Errorf("groups %d", got)
	}
}

func TestRevealInTheFileManager(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	var mu sync.Mutex
	var opened []string
	// the function runs on the goroutine of the command
	a.win.OpenURI = func(_ context.Context, _ widgets.BuildContext, uri string) error {
		mu.Lock()
		defer mu.Unlock()
		opened = append(opened, uri)
		return nil
	}
	a.focusTree("docs/guide.md")
	a.mustRun("revealFileInOS", nil)
	a.waitFor("the call", func() bool { mu.Lock(); defer mu.Unlock(); return len(opened) == 1 })
	mu.Lock()
	got := slices.Clone(opened)
	mu.Unlock()
	if want := "file://" + a.path("docs"); got[0] != want {
		t.Errorf("opened %v, want %s", got, want)
	}
	// a failure is reported
	a.win.OpenURI = func(context.Context, widgets.BuildContext, string) error { return os.ErrPermission }
	a.mustRun("revealFileInOS", a.path("main.go"))
	a.waitFor("the error", func() bool {
		for _, it := range a.win.Notify.Items() {
			if strings.Contains(it.Message, "file manager") {
				return true
			}
		}
		return false
	})
}

func TestNewFileWithoutAFolderSaysSo(t *testing.T) {
	win := newTestWorkspace(t, "")
	ts := testutil.New(t, fileOpsSize)
	ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	for range 4 {
		ts.Pump(20 * time.Millisecond)
	}
	if err := win.Commands.Run(command.Context{Ctx: context.Background(), Keys: win.Keys, Source: command.SourceAPI}, "explorer.newFile", nil); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range win.Notify.Items() {
		found = found || strings.Contains(it.Message, "not yet opened a folder")
	}
	if !found {
		t.Error("no notification")
	}
}
