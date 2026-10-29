package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The tests of saving, Save As, Save All, Revert and untitled files (step 7.1).

// saveApp is a window with a fake desktop, which answers the save dialog.
type saveApp struct {
	*groupApp
	fd *gluttertest.FakeDesktop
}

func newSaveApp(t *testing.T, texts map[string]string, open ...string) *saveApp {
	t.Helper()
	names := make([]string, 0, len(texts))
	for n := range texts {
		names = append(names, n)
	}
	slices.Sort(names)
	return newSaveAppWith(t, newTestWorkspace(t, ""), names, texts, open...)
}

func newSaveAppWith(t *testing.T, win *Workspace, names []string, texts map[string]string, open ...string) *saveApp {
	t.Helper()
	g := newGroupAppWith(t, win, testWindowSize, names, texts, open...)
	return &saveApp{groupApp: g, fd: g.ts.FakeDesktop()}
}

// settingsWindow is a window whose settings file has the given settings.
func settingsWindow(t *testing.T, extra map[string]any) *Workspace {
	t.Helper()
	paths := testutil.Isolate(t)
	// closing a window with unsaved changes asks unless a test turns hot exit on (the backups are step 7.6)
	// (the menu bar of the tests is the classic one: the tests tap its titles; the compact menu has tests of its own)
	values := map[string]any{"editor.fontSize": 14, "editor.fontFamily": testutil.MonoFamily, "files.hotExit": "off", "window.menuBarVisibility": "classic"}
	for k, v := range extra {
		values[k] = v
	}
	// written as a file: the store of the test helper takes no language blocks
	userPath, err := settings.UserSettingsPath(paths, storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	win, err := NewWorkspace(context.Background(), newTestProcess(ProcessOptions{Paths: paths}), "")
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	return win
}

// waitFor pumps the work the goroutines hand over until the condition holds.
func (a *saveApp) waitFor(what string, cond func() bool) {
	a.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			a.t.Fatalf("timed out waiting for %s", what)
		}
		a.ts.FlushPosted()
		a.ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	a.pump()
}

func (a *saveApp) typeText(text string) {
	a.t.Helper()
	if err := a.vm().RunCommand("type", map[string]any{"text": text}); err != nil {
		a.t.Fatal(err)
	}
	a.pump()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// notes are the notifications of the window.
func (a *saveApp) notes() []notify.Item { return a.win.Notify.Items() }

// errorNote is the one notification of an error, which must be there.
func (a *saveApp) errorNote() notify.Item {
	a.t.Helper()
	var found []notify.Item
	for _, n := range a.notes() {
		if n.Severity == notify.Error {
			found = append(found, n)
		}
	}
	if len(found) != 1 {
		a.t.Fatalf("%d notifications of errors: %v", len(found), found)
	}
	return found[0]
}

func actionLabels(n notify.Item) []string {
	var out []string
	for _, a := range n.Actions {
		out = append(out, a.Label)
	}
	return out
}

// pressAction presses the button of a notification the way the shell does: it runs it on the UI thread.
func (a *saveApp) pressAction(n notify.Item, label string) {
	a.t.Helper()
	for _, act := range n.Actions {
		if act.Label == label {
			act.Run()
			a.pump()
			return
		}
	}
	a.t.Fatalf("no button %q in %v", label, actionLabels(n))
}

func (a *saveApp) dirtyTab(name string) bool { return a.item(name).Dirty }

func TestCtrlSSavesTheFileAndClearsTheDirtyMark(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
	a.clickTab("a.txt")
	a.typeText("hello ")
	if !a.dirtyTab("a.txt") || a.dirtyTab("b.txt") {
		t.Fatalf("tabs %+v", a.tabs())
	}
	if !strings.Contains(a.win.Title(), "●") {
		t.Errorf("the title %q has no dirty mark", a.win.Title())
	}
	if v, _ := a.win.Keys.Get("activeEditorIsDirty").(bool); !v {
		t.Error("activeEditorIsDirty is not set")
	}
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	if got := readFile(t, a.files["a.txt"]); got != "hello one\n" {
		t.Errorf("file = %q", got)
	}
	if a.dirtyTab("a.txt") || strings.Contains(a.win.Title(), "●") {
		t.Errorf("tab %+v, title %q: still dirty", a.item("a.txt"), a.win.Title())
	}
	if v, _ := a.win.Keys.Get("activeEditorIsDirty").(bool); v {
		t.Error("activeEditorIsDirty is still set")
	}
	if v, _ := a.win.Keys.Get("dirtyWorkingCopies").(bool); v {
		t.Error("dirtyWorkingCopies is still set")
	}
	// the undo history survives the save
	if !a.doc("a.txt").CanUndo() {
		t.Error("the history is gone")
	}
	a.noErrors()
}

func TestSaveKeepsWhatWasTypedDuringTheWrite(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.typeText("x")
	s := a.shell()
	doc := a.doc("a.txt")
	job, err := doc.BeginSave(s.saveOptions(doc, saveRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	a.typeText("y") // typed while the write goes on
	stat, err := job.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	doc.FinishSave(job, stat)
	a.pump()
	if !doc.Dirty() || !a.dirtyTab("a.txt") {
		t.Error("the document counts as saved although it was edited after the write began")
	}
}

func TestSaveWithoutFormattingSkipsTheParticipants(t *testing.T) {
	win := settingsWindow(t, map[string]any{"files.trimTrailingWhitespace": true, "files.insertFinalNewline": true})
	a := newSaveAppWith(t, win, []string{"a.txt"}, map[string]string{"a.txt": "one  \ntwo"}, "a.txt")
	a.typeText("x")
	a.press("ctrl+k s")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	if got := readFile(t, a.files["a.txt"]); got != "xone  \ntwo" {
		t.Errorf("file = %q", got)
	}
	a.typeText("y")
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	if got := readFile(t, a.files["a.txt"]); got != "xyone\ntwo\n" {
		t.Errorf("file = %q", got)
	}
}

func TestSaveParticipantsAreOneUndoStepAndFollowTheLanguage(t *testing.T) {
	win := settingsWindow(t, map[string]any{
		"files.trimTrailingWhitespace": true,
		"[plaintext]":                  map[string]any{"files.insertFinalNewline": true},
	})
	a := newSaveAppWith(t, win, []string{"a.txt", "b.go"}, map[string]string{"a.txt": "one  ", "b.go": "package b  "}, "a.txt", "b.go")
	// plain text: trimmed and given a final newline; Go: only trimmed
	a.clickTab("a.txt")
	a.typeText("x")
	a.press("ctrl+s")
	a.waitFor("the save of a.txt", func() bool { return !a.doc("a.txt").Dirty() })
	if got := readFile(t, a.files["a.txt"]); got != "xone\n" {
		t.Errorf("a.txt = %q", got)
	}
	// the participants are a step of their own: one undo gives the text of the save
	a.press("ctrl+z")
	if got := a.doc("a.txt").Text(); got != "xone  " {
		t.Errorf("after one undo %q", got)
	}
	a.clickTab("b.go")
	a.typeText("//")
	a.press("ctrl+s")
	a.waitFor("the save of b.go", func() bool { return !a.doc("b.go").Dirty() })
	if got := readFile(t, a.files["b.go"]); got != "//package b" {
		t.Errorf("b.go = %q", got)
	}
}

func TestSaveOfAReadOnlyDocumentDoesNothing(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.doc("a.txt").SetReadOnly(true)
	a.press("ctrl+s")
	a.pump()
	found := false
	for _, n := range a.notes() {
		if strings.Contains(n.Message, "read-only") && n.Silent {
			found = true
		}
	}
	if !found {
		t.Errorf("no quiet message: %v", a.notes())
	}
	if got := readFile(t, a.files["a.txt"]); got != "one\n" {
		t.Errorf("file = %q", got)
	}
}

func TestSaveCommandsAreDispatchedThroughTheirKeys(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.typeText("x")
	a.press("ctrl+shift+s")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	a.fd.CancelFileChooser()
	a.pump()
	if !a.doc("a.txt").Dirty() {
		t.Error("a cancelled Save As saved the file")
	}
}

// ---- Save As ----

func TestSaveAsOfAFileMovesTheEditorToTheNewFile(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.typeText("x")
	doc := a.doc("a.txt")
	target := filepath.Join(t.TempDir(), "renamed.go")
	a.run("workbench.action.files.saveAs")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	a.fd.AnswerFileChooser(target)
	a.waitFor("the save", func() bool { return doc.URI() == document.FileURI(target) })
	a.waitFor("the clean document", func() bool { return !doc.Dirty() })
	if got := readFile(t, target); got != "xone\n" {
		t.Errorf("new file = %q", got)
	}
	if got := readFile(t, a.files["a.txt"]); got != "one\n" {
		t.Errorf("the old file became %q", got)
	}
	wantNames(t, a.tabNames(), "renamed.go")
	if a.dirtyTab("renamed.go") {
		t.Error("the tab is dirty")
	}
	if got := a.statusText("status.editor.mode"); got != "Go" {
		t.Errorf("the language is %q, want it found again from the new name", got)
	}
	if d, ok := a.win.Documents.Get(document.FileURI(target)); !ok || d != doc {
		t.Error("the document is not kept under its new name")
	}
	if _, ok := a.win.Documents.Get(document.FileURI(a.files["a.txt"])); ok {
		t.Error("the old name still has a document")
	}
	recent := a.win.Recent.List()
	if !slices.Contains(recent, target) {
		t.Errorf("recent files %v lack the new file", recent)
	}
	if !doc.CanUndo() {
		t.Error("the history is gone")
	}
	a.noErrors()
}

func TestSaveAsSuggestsTheNameOfTheFile(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	s := a.shell()
	got := s.saveAsSuggestion(a.doc("a.txt"))
	if got.Name != "a.txt" || got.Folder != filepath.Dir(a.files["a.txt"]) {
		t.Errorf("suggestion %+v", got)
	}
	a.run("workbench.action.files.newUntitledFile")
	a.typeText("x")
	var untitled *document.Document
	for _, d := range a.win.Documents.All() {
		if d.URI().Scheme == document.SchemeUntitled {
			untitled = d
		}
	}
	if got := s.saveAsSuggestion(untitled); got.Name != "Untitled-1" {
		t.Errorf("suggestion %+v", got)
	}
	untitled.SetLanguageID("go")
	if got := s.saveAsSuggestion(untitled); got.Name != "Untitled-1.go" {
		t.Errorf("suggestion %+v", got)
	}
}

func TestSaveAsOverAFileAsksToReplaceIt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		button string
		want   string
	}{
		{"replace", "Replace", "mine"},
		{"cancel", "Cancel", "theirs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
			target := filepath.Join(t.TempDir(), "t.txt")
			if err := os.WriteFile(target, []byte("theirs"), 0o644); err != nil {
				t.Fatal(err)
			}
			doc := a.doc("a.txt")
			a.edit("a.txt")
			a.run("workbench.action.files.saveAs")
			a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
			a.fd.AnswerFileChooser(target)
			a.waitFor("the question", func() bool { return a.ts.Exists(gluttertest.ByText(tc.button)) })
			if !a.ts.Exists(gluttertest.ByText("'t.txt' already exists. Do you want to replace it?")) {
				t.Error("the question is not on screen")
			}
			a.ts.Tap(gluttertest.ByText(tc.button))
			a.pump()
			if tc.button == "Replace" {
				a.waitFor("the save", func() bool { return doc.URI() == document.FileURI(target) })
				if got := readFile(t, target); got != "xone\n" {
					t.Errorf("file = %q", got)
				}
				return
			}
			if got := readFile(t, target); got != tc.want || doc.URI() != document.FileURI(a.files["a.txt"]) || !doc.Dirty() {
				t.Errorf("file %q uri %v dirty %v", got, doc.URI(), doc.Dirty())
			}
		})
	}
}

func TestSaveAsOverAFileThatIsOpenIsRefused(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
	a.clickTab("a.txt")
	a.edit("a.txt")
	a.run("workbench.action.files.saveAs")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	a.fd.AnswerFileChooser(a.files["b.txt"])
	a.waitFor("the message", func() bool { return len(a.notes()) > 0 })
	n := a.errorNote()
	if !strings.Contains(n.Message, "open in another editor") {
		t.Errorf("message %q", n.Message)
	}
	if got := readFile(t, a.files["b.txt"]); got != "two\n" {
		t.Errorf("b.txt = %q", got)
	}
}

func TestSaveAsTargetOpenRoute(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
	err := checkSaveAsTarget(a.win, a.doc("a.txt"), a.files["b.txt"])
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("checkSaveAsTarget.SaveAsTargetOpen") {
		t.Fatalf("err = %v", err)
	}
	if err := checkSaveAsTarget(a.win, a.doc("a.txt"), a.files["a.txt"]); err != nil {
		t.Errorf("its own file: %v", err)
	}
}

func TestSaveDialogFailureHasItsOwnTag(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	// the dialog waits for the user, so it is refused on the UI thread, which is where this test runs
	_, err := chooseSavePath(context.Background(), a.ts.Element(gluttertest.ByType[*shell]()), SaveDialogOptions{Name: "x"})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("chooseSavePath.SaveDialogFailed") || appErr.OriginalErr == nil {
		t.Fatalf("err = %v", err)
	}
}

func TestADialogThatFailsIsReported(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.win.SaveDialog = func(context.Context, widgets.BuildContext, SaveDialogOptions) (string, error) {
		return "", errors.New("SaveDialogFailed", "the portal broke")
	}
	a.edit("a.txt")
	a.run("workbench.action.files.saveAs")
	a.waitFor("the message", func() bool { return notified(a.win, "Cannot show the dialog to save a file") })
	if a.win.QuickInput.Visible() {
		t.Error("a failure other than the missing portal opened the prompt")
	}
}

func TestDialogUnavailable(t *testing.T) {
	// the fake desktop without a file chooser portal fails the dialog the way a desktop without one does
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.fd.DisableFileChooser()
	bc := a.ts.Element(gluttertest.ByType[*shell]())
	result := make(chan error, 1)
	go func() {
		_, err := chooseSavePath(context.Background(), bc, SaveDialogOptions{Name: "x"})
		result <- err
	}()
	var err error
	a.waitFor("the dialog to fail", func() bool {
		select {
		case err = <-result:
			return true
		default:
			return false
		}
	})
	other := errors.NewErr("SaveDialogFailed", errors.New("Other", "x"))
	if !dialogUnavailable(err) || dialogUnavailable(other) || dialogUnavailable(nil) {
		t.Errorf("dialogUnavailable is wrong for %v", err)
	}
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("chooseSavePath.SaveDialogFailed") {
		t.Errorf("unexpected failure path: %v", err)
	}
}

func TestWithoutAPortalSaveAsAsksForThePathInTheQuickInput(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.fd.DisableFileChooser()
	doc := a.doc("a.txt")
	a.edit("a.txt")
	qi := a.win.QuickInput
	open := func() {
		a.run("workbench.action.files.saveAs")
		a.waitFor("the prompt", func() bool { return qi.Prompting() })
	}
	open()
	if got := qi.Picker().Value(); got != a.files["a.txt"] {
		t.Errorf("the prompt starts with %q, want the path of the file", got)
	}
	// a folder that is not there
	missing := filepath.Join(t.TempDir(), "no", "such", "f.txt")
	qi.SetValue(missing)
	if m := qi.Picker().Message(); !strings.Contains(m, "does not exist") {
		t.Errorf("message %q", m)
	}
	a.press("enter")
	if !qi.Visible() || doc.URI() != document.FileURI(a.files["a.txt"]) {
		t.Error("an invalid path was accepted")
	}
	// Escape cancels
	a.press("escape")
	if qi.Visible() || !doc.Dirty() {
		t.Errorf("visible %v dirty %v after Escape", qi.Visible(), doc.Dirty())
	}
	// a valid path saves
	open()
	target := filepath.Join(t.TempDir(), "new.txt")
	qi.SetValue(target)
	if m := qi.Picker().Message(); m != "" {
		t.Errorf("message %q for a valid path", m)
	}
	a.press("enter")
	a.waitFor("the save", func() bool { return doc.URI() == document.FileURI(target) && !doc.Dirty() })
	if got := readFile(t, target); got != "xone\n" {
		t.Errorf("file = %q", got)
	}
}

func TestValidateSavePath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, value, want string
	}{
		{"empty", "", "Enter the path"},
		{"blank", "   ", "Enter the path"},
		{"relative", "a/b.txt", "absolute"},
		{"a folder name", dir + "/", "not of a folder"},
		{"a folder", dir, "is a folder"},
		{"missing folder", filepath.Join(dir, "no", "f.txt"), "does not exist"},
		{"the parent is a file", filepath.Join(file, "f.txt"), "does not exist"},
		{"a new file", filepath.Join(dir, "new.txt"), ""},
		{"a file that exists", file, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateSavePath(tt.value)
			if (tt.want == "") != (got == "") || !strings.Contains(got, tt.want) {
				t.Errorf("validateSavePath(%q) = %q, want it to contain %q", tt.value, got, tt.want)
			}
		})
	}
}

// ---- untitled ----

func TestCtrlNMakesUntitledFiles(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.press("ctrl+n")
	wantNames(t, a.tabNames(), "a.txt", "Untitled-1")
	if a.active() != "Untitled-1" || a.item("Untitled-1").Preview {
		t.Errorf("active %s, tab %+v: the new file is shown and kept", a.active(), a.item("Untitled-1"))
	}
	if got := a.editorStatus(); got[len(got)-1] != "Plain Text" || got[2] != "UTF-8" || got[3] != "LF" {
		t.Errorf("status %v", got)
	}
	if a.dirtyTab("Untitled-1") {
		t.Error("an empty untitled file is dirty")
	}
	a.typeText("hi")
	if !a.dirtyTab("Untitled-1") {
		t.Error("typing did not make it dirty")
	}
	a.press("ctrl+n")
	wantNames(t, a.tabNames(), "a.txt", "Untitled-1", "Untitled-2")
	// a number that is free again is used again
	a.run("workbench.action.closeActiveEditor")
	wantNames(t, a.tabNames(), "a.txt", "Untitled-1")
	a.press("ctrl+n")
	wantNames(t, a.tabNames(), "a.txt", "Untitled-1", "Untitled-2")
	a.noErrors()
}

func TestNewUntitledFollowsTheSettingsAndTheArgument(t *testing.T) {
	win := settingsWindow(t, map[string]any{"files.defaultLanguage": "go", "files.eol": "\r\n", "files.encoding": "utf8bom"})
	a := newSaveAppWith(t, win, []string{"a.txt"}, map[string]string{"a.txt": "x"}, "a.txt")
	if err := a.runWith("workbench.action.files.newUntitledFile", nil); err != nil {
		t.Fatal(err)
	}
	if got := a.editorStatus(); got[len(got)-1] != "Go" || got[2] != "UTF-8 with BOM" || got[3] != "CRLF" {
		t.Errorf("status %v", got)
	}
	if err := a.runWith("workbench.action.files.newUntitledFile", map[string]any{"languageId": "markdown"}); err != nil {
		t.Fatal(err)
	}
	if got := a.editorStatus(); got[len(got)-1] != "Markdown" {
		t.Errorf("status %v", got)
	}
	a.setting("files.defaultLanguage", "${activeEditorLanguage}")
	a.clickTab("a.txt")
	a.press("ctrl+n")
	if got := a.editorStatus(); got[len(got)-1] != "Plain Text" {
		t.Errorf("status %v", got)
	}
	if err := a.runWith("workbench.action.files.newUntitledFile", map[string]any{"languageId": "nonsense"}); err != nil {
		t.Fatal(err)
	}
	if got := a.editorStatus(); got[len(got)-1] != "Plain Text" {
		t.Errorf("a language that does not exist gives %v", got)
	}
}

func TestSavingAnUntitledFileRunsSaveAs(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.press("ctrl+n")
	a.typeText("package main\n")
	doc := a.vm().Document()
	target := filepath.Join(t.TempDir(), "main.go")
	a.press("ctrl+s")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	a.fd.AnswerFileChooser(target)
	a.waitFor("the save", func() bool { return doc.URI() == document.FileURI(target) && !doc.Dirty() })
	wantNames(t, a.tabNames(), "a.txt", "main.go")
	if got := readFile(t, target); got != "package main\n" {
		t.Errorf("file = %q", got)
	}
	if got := a.statusText("status.editor.mode"); got != "Go" {
		t.Errorf("language %q", got)
	}
	if a.shell().activeEditor().input.Capabilities().Has(editorgroup.CapUntitled) {
		t.Error("the input is still untitled")
	}
	// the number is free again
	a.press("ctrl+n")
	if !slices.Contains(a.tabNames(), "Untitled-1") {
		t.Errorf("tabs %v: Untitled-1 should be made again", a.tabNames())
	}
	// and a second save goes to the file without a dialog
	a.clickTab("main.go")
	a.typeText("// x\n")
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !doc.Dirty() })
	if a.fd.PendingFileChoosers() != 0 {
		t.Error("a dialog opened for a file that has a name")
	}
}

func TestRevertOfAnUntitledFileEmptiesIt(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.press("ctrl+n")
	a.typeText("abc")
	doc := a.vm().Document()
	a.run("workbench.action.files.revert")
	a.waitFor("the revert", func() bool { return doc.Text() == "" })
	if doc.Dirty() || a.dirtyTab("Untitled-1") {
		t.Error("dirty after the revert")
	}
}

// ---- Save All ----

func TestSaveAllSavesTheDirtyFiles(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n", "c.txt": "c\n"}, "a.txt", "b.txt", "c.txt")
	a.edit("a.txt")
	a.edit("c.txt")
	if v, _ := a.win.Keys.Get("dirtyWorkingCopies").(bool); !v {
		t.Error("dirtyWorkingCopies is not set")
	}
	a.run("saveAll")
	a.waitFor("the saves", func() bool { return !a.doc("a.txt").Dirty() && !a.doc("c.txt").Dirty() })
	if readFile(t, a.files["a.txt"]) != "xa\n" || readFile(t, a.files["b.txt"]) != "b\n" || readFile(t, a.files["c.txt"]) != "xc\n" {
		t.Error("the wrong files were written")
	}
	a.edit("b.txt")
	a.run("workbench.action.files.saveAll")
	a.waitFor("the save", func() bool { return !a.doc("b.txt").Dirty() })
	if v, _ := a.win.Keys.Get("dirtyWorkingCopies").(bool); v {
		t.Error("dirtyWorkingCopies is still set")
	}
}

func TestSaveAllStopsWhenTheSaveAsOfAnUntitledFileIsCancelled(t *testing.T) {
	for _, tc := range []struct {
		name      string
		untitled  int // 0: after the files, 1: before them
		wantSaved bool
	}{
		{"untitled last: the files were saved before", 0, true},
		{"untitled first: nothing else is saved", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newSaveApp(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}, "a.txt", "b.txt")
			if tc.untitled == 1 {
				a.press("ctrl+n")
				a.typeText("u")
				if err := a.runWith("moveActiveEditor", map[string]any{"to": "first", "by": "tab"}); err != nil {
					t.Fatal(err)
				}
				wantNames(t, a.tabNames(), "Untitled-1", "a.txt", "b.txt")
			} else {
				a.clickTab("b.txt") // a new file opens to the right of the active one
				a.press("ctrl+n")
				a.typeText("u")
				wantNames(t, a.tabNames(), "a.txt", "b.txt", "Untitled-1")
			}
			a.edit("a.txt")
			a.edit("b.txt")
			a.run("saveAll")
			a.waitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
			a.fd.CancelFileChooser()
			a.pump()
			for _, n := range []string{"a.txt", "b.txt"} {
				if saved := !a.doc(n).Dirty(); saved != tc.wantSaved {
					t.Errorf("%s saved = %v", n, saved)
				}
			}
		})
	}
}

func TestSaveAllAsksForEveryUntitledFileInTurn(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "a\n"}, "a.txt")
	a.press("ctrl+n")
	a.typeText("u1")
	a.press("ctrl+n")
	a.typeText("u2")
	dir := t.TempDir()
	a.run("saveAll")
	for _, name := range []string{"one.txt", "two.txt"} {
		a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
		a.fd.AnswerFileChooser(filepath.Join(dir, name))
		a.pump()
	}
	a.waitFor("the saves", func() bool { return len(a.shell().dirtyDocuments()) == 0 && a.fd.PendingFileChoosers() == 0 })
	if readFile(t, filepath.Join(dir, "one.txt")) != "u1" || readFile(t, filepath.Join(dir, "two.txt")) != "u2" {
		t.Error("the untitled files were not saved in order")
	}
}

// ---- Revert ----

func TestRevertReadsTheFileAgain(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\ntwo\n"}, "a.txt")
	doc := a.doc("a.txt")
	a.typeText("typed ")
	a.vm().GoToPosition(doc.Snapshot().PositionAt(doc.Snapshot().Len())) // the cursor is at the end
	if err := os.WriteFile(a.files["a.txt"], []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.action.files.revert")
	a.waitFor("the revert", func() bool { return doc.Text() == "changed\n" })
	if doc.Dirty() || a.dirtyTab("a.txt") || doc.CanUndo() {
		t.Errorf("dirty %v undo %v", doc.Dirty(), doc.CanUndo())
	}
	if p := a.vm().Cursors().Primary().Position(); p.Line > 1 {
		t.Errorf("the cursor %v is out of the text", p)
	}
	a.noErrors()
}

func TestRevertOfADeletedFileIsReported(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.typeText("x")
	if err := os.Remove(a.files["a.txt"]); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.action.files.revert")
	a.waitFor("the message", func() bool { return notified(a.win, "Failed to revert 'a.txt'") })
	if !a.doc("a.txt").Dirty() {
		t.Error("a failed revert dropped the changes")
	}
}

// ---- failures ----

func TestAFileThatChangedOnDiskIsAConflict(t *testing.T) {
	for _, tc := range []struct {
		button string
		check  func(t *testing.T, a *saveApp)
	}{
		{"Overwrite", func(t *testing.T, a *saveApp) {
			a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
			if got := readFile(t, a.files["a.txt"]); got != "xone\n" {
				t.Errorf("file = %q", got)
			}
		}},
		{"Revert", func(t *testing.T, a *saveApp) {
			a.waitFor("the revert", func() bool { return a.doc("a.txt").Text() == "elsewhere\n" })
			if a.doc("a.txt").Dirty() {
				t.Error("dirty after Revert")
			}
		}},
	} {
		t.Run(tc.button, func(t *testing.T) {
			a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
			a.typeText("x")
			if err := os.WriteFile(a.files["a.txt"], []byte("elsewhere\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			a.press("ctrl+s")
			a.waitFor("the notification", func() bool { return len(a.notes()) > 0 })
			n := a.errorNote()
			if !slices.Equal(actionLabels(n), []string{"Compare", "Overwrite", "Revert", "Save As..."}) || !n.IsSticky() {
				t.Errorf("buttons %v sticky %v", actionLabels(n), n.IsSticky())
			}
			if !strings.Contains(n.Message, "'a.txt'") || !strings.Contains(n.Message, "newer") {
				t.Errorf("message %q", n.Message)
			}
			if got := readFile(t, a.files["a.txt"]); got != "elsewhere\n" || !a.doc("a.txt").Dirty() {
				t.Errorf("a refused save changed the file to %q", got)
			}
			a.pressAction(n, tc.button)
			tc.check(t, a)
			if len(a.notes()) != 0 {
				t.Errorf("notifications left: %v", a.notes())
			}
		})
	}
}

func TestAConflictCanBeSavedAsAnotherFile(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.typeText("x")
	if err := os.WriteFile(a.files["a.txt"], []byte("elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.press("ctrl+s")
	a.waitFor("the notification", func() bool { return len(a.notes()) > 0 })
	a.pressAction(a.errorNote(), "Save As...")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	target := filepath.Join(t.TempDir(), "mine.txt")
	a.fd.AnswerFileChooser(target)
	a.waitFor("the save", func() bool { return a.doc("a.txt").URI() == document.FileURI(target) })
	if readFile(t, target) != "xone\n" || readFile(t, a.files["a.txt"]) != "elsewhere\n" {
		t.Error("the files are wrong")
	}
	a.waitFor("the notification to go", func() bool { return len(a.notes()) == 0 })
}

func TestTheConflictSettingOverwritesWithoutAsking(t *testing.T) {
	win := settingsWindow(t, map[string]any{"files.saveConflictResolution": "overwriteFileOnDisk"})
	a := newSaveAppWith(t, win, []string{"a.txt"}, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.typeText("x")
	if err := os.WriteFile(a.files["a.txt"], []byte("elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	if got := readFile(t, a.files["a.txt"]); got != "xone\n" || len(a.notes()) != 0 {
		t.Errorf("file %q notes %v", got, a.notes())
	}
}

func TestAReadOnlyFileOffersToOverwrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions don't apply")
	}
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	if err := os.Chmod(a.files["a.txt"], 0o444); err != nil {
		t.Fatal(err)
	}
	a.typeText("x")
	a.press("ctrl+s")
	a.waitFor("the notification", func() bool { return len(a.notes()) > 0 })
	n := a.errorNote()
	if !slices.Equal(actionLabels(n), []string{"Overwrite", "Save As..."}) {
		t.Fatalf("buttons %v", actionLabels(n))
	}
	a.pressAction(n, "Overwrite")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	if got := readFile(t, a.files["a.txt"]); got != "xone\n" {
		t.Errorf("file = %q", got)
	}
}

func TestAFailureOffersToRetryAndOneNotificationStaysForTheDocument(t *testing.T) {
	a := newSaveApp(t, map[string]string{"sub/a.txt": "one\n"}, "sub/a.txt")
	dir := filepath.Dir(a.files["sub/a.txt"])
	a.typeText("x")
	moved := dir + ".gone"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	a.press("ctrl+s")
	a.waitFor("the notification", func() bool { return len(a.notes()) == 1 })
	a.press("ctrl+s")
	a.waitFor("the second failure", func() bool { return len(a.notes()) == 1 && a.notes()[0].ID > 1 })
	n := a.errorNote()
	if !slices.Equal(actionLabels(n), []string{"Retry", "Save As..."}) || !strings.Contains(n.Message, "Failed to save 'a.txt'") {
		t.Fatalf("buttons %v message %q", actionLabels(n), n.Message)
	}
	if err := os.Rename(moved, dir); err != nil {
		t.Fatal(err)
	}
	a.pressAction(n, "Retry")
	a.waitFor("the save", func() bool { return !a.doc("sub/a.txt").Dirty() })
	if got := readFile(t, a.files["sub/a.txt"]); got != "xone\n" || len(a.notes()) != 0 {
		t.Errorf("file %q notes %v", got, a.notes())
	}
}

func TestACharacterTheEncodingLacksOffersAnotherEncoding(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	if err := a.doc("a.txt").SetSaveEncoding("windows1252"); err != nil {
		t.Fatal(err)
	}
	a.typeText("日本")
	a.press("ctrl+s")
	a.waitFor("the notification", func() bool { return len(a.notes()) > 0 })
	n := a.errorNote()
	if !slices.Equal(actionLabels(n), []string{"Save with Encoding...", "Save As..."}) {
		t.Fatalf("buttons %v", actionLabels(n))
	}
	a.pressAction(n, "Save with Encoding...")
	if got := a.win.QuickInput.Picker().Placeholder(); got != "Select File Encoding to Save with" {
		t.Errorf("placeholder %q", got)
	}
	a.pickRow("UTF-8")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	if got := readFile(t, a.files["a.txt"]); got != "日本one\n" {
		t.Errorf("file = %q", got)
	}
}

// ---- Save with Encoding ----

func TestSaveWithEncoding(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "café\n"}, "a.txt")
	a.clickStatus("status.editor.encoding")
	if got := a.pickerLabels(); !slices.Equal(got, []string{"Reopen with Encoding", "Save with Encoding"}) {
		t.Fatalf("actions %v", got)
	}
	a.pickRow("Save with Encoding")
	if got := a.win.QuickInput.Picker().Placeholder(); got != "Select File Encoding to Save with" {
		t.Fatalf("placeholder %q", got)
	}
	a.pickRow("UTF-16 LE")
	// the document is not dirty before the save either (a new encoding changes no text), so the test waits for the file
	want := "\xFF\xFEc\x00a\x00f\x00\xe9\x00\n\x00"
	a.waitFor("the save", func() bool { return readFileIf(a.files["a.txt"]) == want })
	if got := a.doc("a.txt").Encoding(); got != "utf16le" || a.doc("a.txt").Dirty() {
		t.Errorf("encoding %q, dirty %v", got, a.doc("a.txt").Dirty())
	}
	if got := a.statusText("status.editor.encoding"); got != "UTF-16 LE" {
		t.Errorf("encoding item %q", got)
	}
}

func TestSaveWithEncodingOfAnUntitledFile(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "x\n"}, "a.txt")
	a.press("ctrl+n")
	a.typeText("hi")
	a.clickStatus("status.editor.encoding")
	if got := a.pickerLabels(); !slices.Equal(got, []string{"Save with Encoding"}) {
		t.Fatalf("actions %v", got)
	}
	a.pickRow("Save with Encoding")
	a.pickRow("UTF-8 with BOM")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	target := filepath.Join(t.TempDir(), "u.txt")
	a.fd.AnswerFileChooser(target)
	a.waitFor("the save", func() bool { return readFileIf(target) != "" })
	if got := readFile(t, target); got != "\xEF\xBB\xBFhi" {
		t.Errorf("file = %q", got)
	}
}

func readFileIf(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// ---- menus ----

func TestTheFileMenuAndTheTabMenuShowTheStateOfSaving(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
	s := a.shell()
	file := func() []string {
		return menuRows(s.menus.Items(command.MenubarFileMenu, s.menuContext()))
	}
	tail := []string{"-", "Open Folder...", "Open Recent>", "-", "Save", "Save As...", "Save All~", "-", "Auto Save", "Preferences>", "-", "Revert File", "Close Editor", "Close Workspace Tab", "-", "Exit"}
	if got := file(); got[0] != "New Text File" || got[1] != "New Workspace Tab" || !slices.Equal(got[2:], tail) {
		t.Errorf("file menu %v", got)
	}
	a.edit("a.txt")
	if got := file(); !slices.Contains(got, "Save All") {
		t.Errorf("Save All is dimmed with a dirty file: %v", got)
	}
	a.press("ctrl+n")
	if got := file(); !slices.Contains(got, "Revert File~") {
		t.Errorf("an untitled file can be reverted: %v", got)
	}
	// the tab menu has Save and Save As for a dirty tab only
	g := s.activeGroup()
	rows := func(i int) []string { return menuRows(s.tabMenuItems(g, i)) }
	if !slices.Contains(rows(0), "Save") || !slices.Contains(rows(0), "Save As...") {
		t.Errorf("dirty tab: %v", rows(0))
	}
	if slices.Contains(rows(1), "Save") {
		t.Errorf("clean tab: %v", rows(1))
	}
	// a row runs the command for its tab
	for _, it := range s.tabMenuItems(g, 0) {
		if it.ID == "workbench.action.files.save" {
			s.menus.Run(it)
		}
	}
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	if got := readFile(t, a.files["a.txt"]); got != "xone\n" {
		t.Errorf("a.txt = %q", got)
	}
}

// ---- the watcher ----

func TestTheWatcherDoesNotReportOurOwnWrite(t *testing.T) {
	win := watchedWindow(t, "")
	var mu sync.Mutex
	var seen []string
	win.OnFileEvents(func(events []workspace.Event) {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range events {
			seen = append(seen, filepath.Base(e.Path))
		}
	})
	count := func(name string) int {
		mu.Lock()
		defer mu.Unlock()
		n := 0
		for _, s := range seen {
			if s == name {
				n++
			}
		}
		return n
	}
	a := newSaveAppWith(t, win, []string{"a.txt", "control.txt"}, map[string]string{"a.txt": "one\n", "control.txt": "c\n"}, "a.txt", "control.txt")
	win.followFile(a.files["a.txt"])
	win.followFile(a.files["control.txt"])
	eventually(t, "the files to be watched", func() bool {
		win.watch.mu.Lock()
		defer win.watch.mu.Unlock()
		return win.watch.files[a.files["a.txt"]] != nil && win.watch.files[a.files["control.txt"]] != nil
	})
	a.clickTab("a.txt")
	a.typeText("x")
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	// another program writes the other file: when its event is there, the events of our write would be too
	if err := os.WriteFile(a.files["control.txt"], []byte("changed by someone else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the event of the control file", func() bool { return count("control.txt") > 0 })
	if n := count("a.txt"); n != 0 {
		t.Errorf("the watcher reported our own write %d times", n)
	}
	// a write by another program is reported
	if err := os.WriteFile(a.files["a.txt"], []byte("someone else, longer than before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the event of another program's write", func() bool { return count("a.txt") > 0 })
}

// ---- the registry ----

func TestSaveCommandsAreRegisteredWithTheirKeys(t *testing.T) {
	win := newTestWorkspace(t, "")
	for id, key := range map[string]string{
		"workbench.action.files.save":                  "Ctrl+S",
		"workbench.action.files.saveAs":                "Ctrl+Shift+S",
		"workbench.action.files.saveWithoutFormatting": "Ctrl+K S",
		"workbench.action.files.newUntitledFile":       "Ctrl+N",
		"saveAll":                                      "",
		"workbench.action.files.saveAll":               "",
		"workbench.action.files.revert":                "",
	} {
		if _, ok := win.Commands.Lookup(id); !ok {
			t.Errorf("command %s is not registered", id)
			continue
		}
		item, ok := win.Keybindings.LookupPrimaryKeybinding(id, win.Keys, false)
		got := ""
		if ok && item.Keybinding != nil {
			got = item.Keybinding.Label()
		}
		if got != key {
			t.Errorf("%s has the key %q, want %q", id, got, key)
		}
	}
}
