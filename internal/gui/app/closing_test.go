package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/views/explorer"
)

// The tests of closing editors, groups and the window with unsaved changes (step 7.2).

func TestSaveConfirmation(t *testing.T) {
	names := func(n int) []string {
		var out []string
		for i := 1; i <= n; i++ {
			out = append(out, fmt.Sprintf("f%d.go", i))
		}
		return out
	}
	const tail = "\nYour changes will be lost if you don't save them."
	list := func(n int) string { return strings.Join(names(n), "\n") + "\n" }
	tests := []struct {
		name                      string
		files                     []string
		message, detail, saveText string
	}{
		{"one file", []string{"main.go"}, "Do you want to save the changes you made to main.go?", "Your changes will be lost if you don't save them.", "Save"},
		{"an untitled document", []string{"Untitled-1"}, "Do you want to save the changes you made to Untitled-1?", "Your changes will be lost if you don't save them.", "Save"},
		{"two files", names(2), "Do you want to save the changes to the following 2 files?", list(2) + tail, "Save All"},
		{"ten files", names(10), "Do you want to save the changes to the following 10 files?", list(10) + tail, "Save All"},
		{"eleven files", names(11), "Do you want to save the changes to the following 11 files?",
			list(10) + "...1 additional file not shown\n" + tail, "Save All"},
		{"twelve files", names(12), "Do you want to save the changes to the following 12 files?",
			list(10) + "...2 additional files not shown\n" + tail, "Save All"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			message, detail, save := saveConfirmation(tc.files)
			if message != tc.message || detail != tc.detail || save != tc.saveText {
				t.Errorf("saveConfirmation = %q, %q, %q\nwant %q, %q, %q", message, detail, save, tc.message, tc.detail, tc.saveText)
			}
		})
	}
}

func TestCloseDecision(t *testing.T) {
	const (
		window = CloseReasonTab
		quit   = CloseReasonQuit
	)
	tests := []struct {
		hotExit   string
		reason    CloseReason
		hasFolder bool
		otherTabs bool
		want      closeAction
	}{
		// off always asks
		{"off", quit, true, false, closeAsk},
		{"off", window, true, false, closeAsk},
		{"off", window, false, true, closeAsk},
		{"off", quit, false, true, closeAsk},
		// onExit: quitting, and the last window, are backed up; another window closing asks
		{"onExit", quit, false, false, closeBackup},
		{"onExit", quit, true, true, closeBackup},
		{"onExit", window, true, false, closeBackup},
		{"onExit", window, false, false, closeBackup},
		{"onExit", window, true, true, closeAsk},
		{"onExit", window, false, true, closeAsk},
		// onExitAndWindowClose: as onExit, and a window with a folder is backed up when it closes too
		{"onExitAndWindowClose", quit, false, true, closeBackup},
		{"onExitAndWindowClose", window, false, false, closeBackup},
		{"onExitAndWindowClose", window, true, true, closeBackup},
		{"onExitAndWindowClose", window, false, true, closeAsk},
		// a value the setting does not have asks
		{"", quit, true, false, closeAsk},
		{"unknown", window, true, false, closeAsk},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q/reason %d/folder %v/others %v", tc.hotExit, tc.reason, tc.hasFolder, tc.otherTabs), func(t *testing.T) {
			if got := closeDecision(tc.hotExit, tc.reason, tc.hasFolder, tc.otherTabs); got != tc.want {
				t.Errorf("closeDecision = %d, want %d", got, tc.want)
			}
		})
	}
}

// ---- helpers ----

// dialogSpec is the dialog that shows.
func (a *saveApp) dialogSpec() kit.DialogSpec {
	a.t.Helper()
	spec, ok := a.win.Dialogs.Current()
	if !ok {
		a.t.Fatal("no dialog shows")
	}
	return spec
}

// tapDialog presses a button of the dialog that shows.
func (a *saveApp) tapDialog(label string) {
	a.t.Helper()
	if !a.win.Dialogs.IsShowing() {
		a.t.Fatalf("no dialog shows (looking for %q)", label)
	}
	// the dialog is built by the frame after it was asked for: pump before looking for its button
	a.pump()
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText(label)))
	a.pump()
}

// wantAsked checks that the dialog that asks whether to save shows with a message.
func (a *saveApp) wantAsked(message string) {
	a.t.Helper()
	a.pump()
	spec := a.dialogSpec()
	if spec.Message != message {
		a.t.Fatalf("the dialog says %q, want %q", spec.Message, message)
	}
	if !a.ts.Exists(gluttertest.ByText(message)) {
		a.t.Errorf("the message %q is not on screen", message)
	}
}

// wantNoDialog checks that no dialog shows.
func (a *saveApp) wantNoDialog() {
	a.t.Helper()
	if a.win.Dialogs.IsShowing() {
		spec, _ := a.win.Dialogs.Current()
		a.t.Fatalf("a dialog shows: %q", spec.Message)
	}
}

// quits counts the calls of the Quit function of the window.
func (a *saveApp) quits() *int {
	n := 0
	a.win.Quit = func() { n++ }
	return &n
}

// fileText is the text of a file of the app.
func (a *saveApp) fileText(name string) string { return readFile(a.t, a.files[name]) }

// docOpen tells whether the window still has the document of a file.
func (a *saveApp) docOpen(name string) bool {
	_, ok := a.win.Documents.Get(document.FileURI(a.files[name]))
	return ok
}

func askOf(name string) string { return "Do you want to save the changes you made to " + name + "?" }

// hoverTabAndClose presses the close button of a tab, which shows under the pointer.
func (a *saveApp) hoverTabAndClose(name string) {
	a.t.Helper()
	// a tab with unsaved changes shows a dot where the close button is; the button shows under the pointer
	dot := gluttertest.Descendant(editorTabs(), gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCircleFilled)))
	if !a.ts.Exists(dot) {
		dot = gluttertest.Descendant(editorTabs(), gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconClose)))
	}
	at := a.ts.Center(dot)
	a.ts.Hover(at)
	a.ts.Pump(time.Millisecond)
	a.ts.TapAt(at, mouseButton)
	a.pump()
}

// ---- the editors ----

func TestEveryWayToCloseAnEditorAsks(t *testing.T) {
	tests := []struct {
		name  string
		close func(a *saveApp)
	}{
		{"the close button of the tab", func(a *saveApp) { a.hoverTabAndClose("a.txt") }},
		{"Ctrl+W", func(a *saveApp) { a.press("ctrl+w") }},
		{"the command", func(a *saveApp) { a.run("workbench.action.closeActiveEditor") }},
		{"the middle button", func(a *saveApp) {
			a.ts.TapAt(a.ts.Center(a.tabFinder("a.txt")), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.TertiaryButton)})
			a.pump()
		}},
		{"the command for the editors of the group", func(a *saveApp) { a.run("workbench.action.closeEditorsInGroup") }},
		{"the command for all the editors", func(a *saveApp) { a.run("workbench.action.closeAllEditors") }},
		{"the keys of Close All", func(a *saveApp) { a.press("ctrl+k ctrl+w") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt")
			a.edit("a.txt")
			before := a.tabNames()
			tc.close(a)
			a.wantAsked(askOf("a.txt"))
			wantNames(t, a.tabNames(), before...)
			a.tapDialog("Cancel")
			a.wantNoDialog()
			wantNames(t, a.tabNames(), before...)
			if !a.doc("a.txt").Dirty() || !a.docOpen("a.txt") {
				t.Error("Cancel changed the document")
			}
			a.noErrors()
		})
	}
}

func TestTheAnswersOfTheCloseDialog(t *testing.T) {
	tests := []struct {
		name   string
		answer func(a *saveApp)
		closed bool
		saved  bool
	}{
		{"Save", func(a *saveApp) { a.tapDialog("Save") }, true, true},
		{"Don't Save", func(a *saveApp) { a.tapDialog("Don't Save") }, true, false},
		{"Cancel", func(a *saveApp) { a.tapDialog("Cancel") }, false, false},
		{"Escape", func(a *saveApp) { a.tap(keybinding.Escape); a.pump() }, false, false},
		{"Enter presses the default button, Save", func(a *saveApp) { a.tap(keybinding.Enter); a.pump() }, true, true},
		{"a click beside the dialog does nothing", func(a *saveApp) {
			a.ts.TapAt(ui.Offset{Dx: 4, Dy: 4}, mouseButton)
			a.pump()
			if !a.win.Dialogs.IsShowing() {
				a.t.Error("the dialog went away")
			}
			a.tapDialog("Cancel")
		}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
			a.clickTab("a.txt")
			a.typeText("x")
			a.press("ctrl+w")
			a.wantAsked(askOf("a.txt"))
			spec := a.dialogSpec()
			if !slices.Equal(spec.Buttons, []string{"Save", "Don't Save", "Cancel"}) || spec.Type != kit.DialogWarning ||
				spec.Detail != "Your changes will be lost if you don't save them." {
				t.Errorf("the dialog is %+v", spec)
			}
			tc.answer(a)
			if tc.saved {
				a.waitFor("the save", func() bool { return !a.docOpen("a.txt") })
			}
			a.wantNoDialog()
			if tc.closed {
				wantNames(t, a.tabNames(), "b.txt")
				if a.docOpen("a.txt") {
					t.Error("the document of the closed editor is still open")
				}
			} else {
				wantNames(t, a.tabNames(), "a.txt", "b.txt")
				if !a.doc("a.txt").Dirty() {
					t.Error("the changes are gone")
				}
			}
			want := "one\n"
			if tc.saved {
				want = "xone\n"
			}
			if got := a.fileText("a.txt"); got != want {
				t.Errorf("a.txt = %q, want %q", got, want)
			}
			a.noErrors()
		})
	}
}

func TestSaveOfAnUntitledDocumentAsksForTheFile(t *testing.T) {
	setup := func(t *testing.T) *saveApp {
		a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
		a.press("ctrl+n")
		a.typeText("package main\n")
		a.press("ctrl+w")
		a.wantAsked(askOf("Untitled-1"))
		a.tapDialog("Save")
		a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
		return a
	}
	t.Run("the file is chosen", func(t *testing.T) {
		a := setup(t)
		target := filepath.Join(t.TempDir(), "main.go")
		a.fd.AnswerFileChooser(target)
		a.waitFor("the editor to close", func() bool { return !slices.Contains(a.tabNames(), "main.go") && len(a.tabNames()) == 1 })
		if got := readFile(t, target); got != "package main\n" {
			t.Errorf("file = %q", got)
		}
		wantNames(t, a.tabNames(), "a.txt")
		a.wantNoDialog()
	})
	t.Run("Save As is cancelled", func(t *testing.T) {
		a := setup(t)
		a.fd.CancelFileChooser()
		// The dialog is gone from the fake desktop at once, but its answer travels through goroutines (the signal, then
		// the Save As that was waiting) before it arrives on the UI thread: wait for what the answer does.
		a.waitFor("the window to stop waiting for the answer", func() bool { return !a.shell().closing.busy })
		a.pump()
		wantNames(t, a.tabNames(), "a.txt", "Untitled-1")
		a.wantNoDialog()
		if a.shell().closing.busy {
			t.Error("the window still waits for the answer")
		}
	})
}

func TestACloseWhoseSaveFailsKeepsTheEditors(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
	a.edit("a.txt")
	a.edit("b.txt")
	// another program changed a.txt: saving it fails, and the others are not saved either
	if err := os.WriteFile(a.files["a.txt"], []byte("elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.action.closeAllEditors")
	a.wantAsked("Do you want to save the changes to the following 2 files?")
	spec := a.dialogSpec()
	if spec.Buttons[0] != "Save All" || !strings.HasPrefix(spec.Detail, "a.txt\nb.txt\n\n") {
		t.Errorf("buttons %v detail %q", spec.Buttons, spec.Detail)
	}
	a.tapDialog("Save All")
	a.waitFor("the error", func() bool { return len(a.notes()) > 0 })
	if n := a.errorNote(); !strings.Contains(n.Message, "'a.txt'") {
		t.Errorf("message %q", n.Message)
	}
	wantNames(t, a.tabNames(), "a.txt", "b.txt")
	a.wantNoDialog()
	if got := a.fileText("b.txt"); got != "two\n" {
		t.Errorf("b.txt = %q: the save went on after the failure", got)
	}
	if a.shell().closing.busy {
		t.Error("the window still waits for the answer")
	}
}

func TestSaveAllSavesEveryFileThenCloses(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n", "c.txt": "three\n"}, "a.txt", "b.txt", "c.txt")
	a.edit("a.txt")
	a.edit("c.txt")
	a.run("workbench.action.closeAllEditors")
	a.wantAsked("Do you want to save the changes to the following 2 files?")
	a.tapDialog("Save All")
	a.waitFor("the editors to close", func() bool { return len(a.tabNames()) == 0 })
	if a.fileText("a.txt") != "xone\n" || a.fileText("c.txt") != "xthree\n" || a.fileText("b.txt") != "two\n" {
		t.Errorf("files %q %q %q", a.fileText("a.txt"), a.fileText("b.txt"), a.fileText("c.txt"))
	}
}

func TestDontSaveClosesWithoutTouchingTheFilesAndReopenReadsThem(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
	a.edit("a.txt")
	a.edit("b.txt")
	a.run("workbench.action.closeAllEditors")
	a.tapDialog("Don't Save")
	wantNames(t, a.tabNames())
	for _, n := range []string{"a.txt", "b.txt"} {
		if a.docOpen(n) {
			t.Errorf("%s is still open", n)
		}
	}
	if a.fileText("a.txt") != "one\n" || a.fileText("b.txt") != "two\n" {
		t.Error("a file was touched")
	}
	// the editors come back as the files are: the changes are gone
	a.run("workbench.action.reopenClosedEditor")
	a.waitFor("the editors", func() bool { return len(a.tabNames()) == 2 })
	if got := a.tabNames(); len(got) != 2 || !slices.Contains(got, "a.txt") || !slices.Contains(got, "b.txt") {
		t.Errorf("tabs %v, want a.txt and b.txt", got)
	}
	if doc, ok := a.win.Documents.Get(document.FileURI(a.files["a.txt"])); !ok || doc.Dirty() || doc == a.doc("a.txt") {
		t.Error("the reopened editor has the old document")
	}
}

func TestBatchClosesAskOnlyAboutWhatCloses(t *testing.T) {
	files := map[string]string{"a.txt": "1\n", "b.txt": "2\n", "c.txt": "3\n", "d.txt": "4\n"}
	tests := []struct {
		name string
		// run is the close; the active editor is b.txt
		run     string
		args    any
		dirty   []string
		asks    string // the message, "" for no dialog
		stays   []string
		closedA []string // the tabs after Don't Save
	}{
		{"close others asks about the others", "workbench.action.closeOtherEditors", nil, []string{"a.txt", "b.txt"},
			askOf("a.txt"), []string{"a.txt", "b.txt", "c.txt", "d.txt"}, []string{"b.txt"}},
		{"close to the right asks about those on the right", "workbench.action.closeEditorsToTheRight", nil, []string{"a.txt", "c.txt", "d.txt"},
			"Do you want to save the changes to the following 2 files?", []string{"a.txt", "b.txt", "c.txt", "d.txt"}, []string{"a.txt", "b.txt"}},
		{"close saved closes the clean ones and asks about nothing", "workbench.action.closeUnmodifiedEditors", nil, []string{"b.txt", "d.txt"},
			"", []string{"b.txt", "d.txt"}, []string{"b.txt", "d.txt"}},
		{"close all asks about all of them", "workbench.action.closeAllEditors", nil, []string{"a.txt", "b.txt", "c.txt"},
			"Do you want to save the changes to the following 3 files?", []string{"a.txt", "b.txt", "c.txt", "d.txt"}, nil},
		{"close all of clean editors asks about nothing", "workbench.action.closeAllEditors", nil, nil, "", nil, nil},
		{"close the group asks about its editors", "workbench.action.closeEditorsInGroup", nil, []string{"d.txt"},
			askOf("d.txt"), []string{"a.txt", "b.txt", "c.txt", "d.txt"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newSaveApp(t, files, "a.txt", "b.txt", "c.txt", "d.txt")
			a.clickTab("b.txt")
			for _, n := range tc.dirty {
				a.edit(n)
			}
			a.run(tc.run)
			if tc.asks == "" {
				a.wantNoDialog()
				wantNames(t, a.tabNames(), tc.stays...)
				return
			}
			a.wantAsked(tc.asks)
			wantNames(t, a.tabNames(), "a.txt", "b.txt", "c.txt", "d.txt")
			a.tapDialog("Cancel")
			wantNames(t, a.tabNames(), "a.txt", "b.txt", "c.txt", "d.txt")
			a.run(tc.run)
			a.tapDialog("Don't Save")
			wantNames(t, a.tabNames(), tc.closedA...)
		})
	}
}

func TestADocumentShownInTwoGroupsAsksWhenTheLastEditorCloses(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt")
	a.run("workbench.action.splitEditorRight")
	a.edit("a.txt")
	// the second group closes its editor: the first one still shows the changes
	a.run("workbench.action.closeActiveEditor")
	a.wantNoDialog()
	if !a.doc("a.txt").Dirty() || a.shell().editorCount() != 1 {
		t.Errorf("dirty %v, %d editors", a.doc("a.txt").Dirty(), a.shell().editorCount())
	}
	// the last one asks
	a.run("workbench.action.closeActiveEditor")
	a.wantAsked(askOf("a.txt"))
	a.tapDialog("Cancel")
	a.run("workbench.action.closeActiveEditor")
	a.tapDialog("Don't Save")
	if a.shell().editorCount() != 0 || a.docOpen("a.txt") {
		t.Errorf("%d editors, open %v", a.shell().editorCount(), a.docOpen("a.txt"))
	}
}

func TestClosingAllTheGroupsAsksOnce(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n", "c.txt": "three\n"}, "a.txt")
	a.run("workbench.action.splitEditorRight")
	a.show("b.txt", false)
	a.run("workbench.action.splitEditorDown")
	a.show("c.txt", false)
	a.edit("a.txt")
	a.edit("b.txt")
	groups, editors := len(a.grid().Groups(editorgroup.CreationTime)), a.shell().editorCount()
	a.run("workbench.action.closeAllGroups")
	a.wantAsked("Do you want to save the changes to the following 2 files?")
	if a.win.Dialogs.Pending() != 0 {
		t.Errorf("%d more dialogs wait", a.win.Dialogs.Pending())
	}
	a.tapDialog("Cancel")
	if got := len(a.grid().Groups(editorgroup.CreationTime)); got != groups || a.shell().editorCount() != editors {
		t.Errorf("%d groups (was %d), %d editors (was %d)", got, groups, a.shell().editorCount(), editors)
	}
	a.run("workbench.action.closeAllGroups")
	a.tapDialog("Don't Save")
	if got := len(a.grid().Groups(editorgroup.CreationTime)); got != 1 || a.shell().editorCount() != 0 {
		t.Errorf("%d groups, %d editors left", got, a.shell().editorCount())
	}
}

func TestClosingTheGroupAsksAboutItsEditors(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt")
	a.run("workbench.action.splitEditorRight")
	a.show("b.txt", false)
	a.edit("b.txt")
	groups := len(a.grid().Groups(editorgroup.CreationTime))
	a.run("workbench.action.closeEditorsAndGroup")
	a.wantAsked(askOf("b.txt"))
	a.tapDialog("Cancel")
	if got := len(a.grid().Groups(editorgroup.CreationTime)); got != groups {
		t.Fatalf("%d groups after Cancel, want %d", got, groups)
	}
	a.run("workbench.action.closeEditorsAndGroup")
	a.tapDialog("Save")
	a.waitFor("the group to close", func() bool { return len(a.grid().Groups(editorgroup.CreationTime)) == groups-1 })
	if a.fileText("b.txt") != "xtwo\n" {
		t.Errorf("b.txt = %q", a.fileText("b.txt"))
	}
}

func TestACloseThatAsksIgnoresAnotherRequest(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
	quits := a.quits()
	a.edit("a.txt")
	a.edit("b.txt")
	a.run("workbench.action.closeAllEditors")
	a.wantAsked("Do you want to save the changes to the following 2 files?")
	// the same close, another close and the close of the window while the dialog is open
	a.run("workbench.action.closeAllEditors")
	a.run("workbench.action.closeActiveEditor")
	a.win.RequestClose(CloseReasonQuit)
	a.pump()
	if a.win.Dialogs.Pending() != 0 {
		t.Errorf("%d dialogs wait behind the first", a.win.Dialogs.Pending())
	}
	if *quits != 0 {
		t.Error("the window quit while it was being asked")
	}
	a.tapDialog("Don't Save")
	wantNames(t, a.tabNames())
	a.wantNoDialog()
	// the answer was carried out once, and the window asks again for the next close
	a.press("ctrl+n")
	a.typeText("x")
	a.press("ctrl+w")
	a.wantAsked(askOf("Untitled-1"))
}

// ---- the other places that close editors ----

func TestTheEditorSwitcherAsksWhenItsCrossClosesAnEditorWithChanges(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
	a.edit("a.txt")
	a.run("workbench.action.showEditorsInActiveGroup")
	var row quickinput.Row
	for _, r := range a.win.QuickInput.Picker().Rows() {
		if r.Item.Label == "a.txt" {
			row = r
		}
	}
	if row.Item.Label == "" {
		t.Fatalf("rows %v", a.pickerLabels())
	}
	a.win.QuickInput.Trigger(row, 0, quickinput.KeyMods{})
	a.pump()
	a.wantAsked(askOf("a.txt"))
	if a.win.QuickInput.Visible() {
		t.Error("the picker stays over the dialog")
	}
	wantNames(t, a.tabNames(), "a.txt", "b.txt")
	a.tapDialog("Don't Save")
	wantNames(t, a.tabNames(), "b.txt")
	// a clean editor closes at once
	a.run("workbench.action.showEditorsInActiveGroup")
	a.win.QuickInput.Trigger(a.win.QuickInput.Picker().Rows()[0], 0, quickinput.KeyMods{})
	a.pump()
	a.wantNoDialog()
	wantNames(t, a.tabNames())
}

func TestTheOpenEditorsViewAsksWhenItsCrossClosesAnEditorWithChanges(t *testing.T) {
	a := newExplorerApp(t, fileOpsSize, projectFiles, "main.go", "go.mod")
	a.win.Views.SetHidden(command.ViewOpenEditors, false)
	a.pump()
	doc, ok := a.win.Documents.Get(document.FileURI(a.path("main.go")))
	if !ok {
		t.Fatal("main.go is not open")
	}
	if _, err := doc.Edit([]document.Edit{{Text: "// changed\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	g := a.shell().activeGroup()
	if g.Model.ActiveEditor().Name() != "main.go" {
		t.Fatalf("the active editor is %s", g.Model.ActiveEditor().Name())
	}
	// the row of an editor with unsaved changes shows a dot, and the close button under the pointer
	dot := gluttertest.Descendant(gluttertest.ByKey(explorer.OpenEditorsKey), gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCircleFilled)))
	if !a.ts.Exists(dot) {
		t.Fatal("the row has no dot")
	}
	at := a.ts.Center(dot)
	a.ts.Hover(at)
	a.ts.Pump(time.Millisecond)
	a.ts.TapAt(at, mouseButton)
	a.pump()
	if !a.win.Dialogs.IsShowing() || !a.ts.Exists(gluttertest.ByText("Do you want to save the changes you made to main.go?")) {
		t.Fatal("no dialog asks about the changes")
	}
	if g.Model.Count() != 2 {
		t.Errorf("%d editors", g.Model.Count())
	}
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText("Don't Save")))
	a.pump()
	if g.Model.Count() != 1 {
		t.Errorf("%d editors after Don't Save", g.Model.Count())
	}
}

func TestCloseFolderAsksAboutUnsavedChanges(t *testing.T) {
	a := newExplorerApp(t, fileOpsSize, projectFiles, "main.go", "go.mod")
	doc, ok := a.win.Documents.Get(document.FileURI(a.path("main.go")))
	if !ok {
		t.Fatal("main.go is not open")
	}
	if _, err := doc.Edit([]document.Edit{{Text: "// changed\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if err := a.run("workbench.action.closeFolder", nil); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if !a.ts.Exists(gluttertest.ByText("Do you want to save the changes you made to main.go?")) || a.win.Folder == "" {
		t.Fatalf("no dialog asks (folder %q)", a.win.Folder)
	}
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText("Don't Save")))
	a.pump()
	if a.win.Folder != "" || a.shell().editorCount() != 0 {
		t.Errorf("folder %q, %d editors", a.win.Folder, a.shell().editorCount())
	}
	if got, _ := os.ReadFile(a.path("main.go")); strings.Contains(string(got), "changed") {
		t.Error("Don't Save wrote the file")
	}
}

func TestOpenFolderSavesThenOpens(t *testing.T) {
	a := newExplorerApp(t, fileOpsSize, projectFiles, "main.go")
	other := t.TempDir()
	writeTree(t, other, "x.go")
	doc, _ := a.win.Documents.Get(document.FileURI(a.path("main.go")))
	if _, err := doc.Edit([]document.Edit{{Text: "// changed\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	a.replaceFolder(other)
	a.pump()
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText("Save")))
	deadline := time.Now().Add(5 * time.Second)
	for a.win.Folder == a.root && time.Now().Before(deadline) {
		a.ts.FlushPosted()
		a.ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	if a.win.Folder == a.root {
		t.Fatal("the folder did not change")
	}
	if got, _ := os.ReadFile(a.path("main.go")); !strings.HasPrefix(string(got), "// changed") {
		t.Errorf("main.go = %q: the changes were not saved", got)
	}
}

// ---- the window ----

func TestRequestCloseOfTheWorkspace(t *testing.T) {
	tests := []struct {
		name   string
		reason CloseReason
		answer string // the button, "" for no dialog
		closes bool   // whether the tab closes (CloseReasonTab) or the application quits (CloseReasonQuit)
		saved  bool
	}{
		{"nothing is dirty", CloseReasonTab, "", true, false},
		{"Save", CloseReasonTab, "Save", true, true},
		{"Don't Save", CloseReasonTab, "Don't Save", true, false},
		{"Cancel", CloseReasonTab, "Cancel", false, false},
		{"nothing is dirty when quitting", CloseReasonQuit, "", true, false},
		{"Save when quitting", CloseReasonQuit, "Save", true, true},
		{"Don't Save when quitting", CloseReasonQuit, "Don't Save", true, false},
		{"Cancel when quitting", CloseReasonQuit, "Cancel", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newSaveApp(t, map[string]string{"a.txt": "one\n", "b.txt": "two\n"}, "a.txt", "b.txt")
			quits := a.quits()
			// a tab that closes is gone from the list (and, as it was the last one, another takes its place); the
			// application that quits is told to end, and its tab stays
			ended := func() bool { return a.win.Tab() == nil }
			if tc.reason == CloseReasonQuit {
				ended = func() bool { return *quits == 1 }
			}
			if tc.answer != "" {
				a.edit("b.txt")
			}
			a.win.RequestClose(tc.reason)
			a.pump()
			if tc.answer == "" {
				a.wantNoDialog()
				if !ended() {
					t.Error("the workspace did not close")
				}
				return
			}
			a.wantAsked(askOf("b.txt"))
			if ended() {
				t.Fatal("the workspace closed before the answer")
			}
			a.tapDialog(tc.answer)
			if tc.saved {
				a.waitFor("the end", ended)
			}
			if ended() != tc.closes {
				t.Errorf("closed = %v, want %v", ended(), tc.closes)
			}
			if tc.reason == CloseReasonTab && *quits != 0 {
				t.Error("closing the tab quit the application")
			}
			a.wantNoDialog()
			if !tc.closes {
				wantNames(t, a.tabNames(), "a.txt", "b.txt")
			}
			want := "two\n"
			if tc.saved {
				want = "xtwo\n"
			}
			if got := a.fileText("b.txt"); got != want {
				t.Errorf("b.txt = %q, want %q", got, want)
			}
			// the workspace keeps asking after a Cancel
			if tc.answer == "Cancel" {
				a.win.RequestClose(tc.reason)
				a.wantAsked(askOf("b.txt"))
			}
		})
	}
}

func TestACloseRequestWhileTheDialogIsOpenIsIgnored(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	quits := a.quits()
	a.edit("a.txt")
	for range 3 {
		a.win.RequestClose(CloseReasonQuit)
		a.pump()
	}
	if a.win.Dialogs.Pending() != 0 || !a.win.Dialogs.IsShowing() {
		t.Fatalf("pending %d showing %v", a.win.Dialogs.Pending(), a.win.Dialogs.IsShowing())
	}
	a.tapDialog("Don't Save")
	if *quits != 1 {
		t.Errorf("Quit called %d times for three requests", *quits)
	}
}

func TestAnUntitledDocumentIsSavedBeforeTheWindowCloses(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	quits := a.quits()
	a.press("ctrl+n")
	a.typeText("hello\n")
	a.win.RequestClose(CloseReasonQuit)
	a.wantAsked(askOf("Untitled-1"))
	a.tapDialog("Save")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	if *quits != 0 {
		t.Fatal("the window quit before the file was chosen")
	}
	target := filepath.Join(t.TempDir(), "hello.txt")
	a.fd.AnswerFileChooser(target)
	a.waitFor("the quit", func() bool { return *quits == 1 })
	if got := readFile(t, target); got != "hello\n" {
		t.Errorf("file = %q", got)
	}
	// and when the file dialog is cancelled the window stays
	b := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	quitsB := b.quits()
	b.press("ctrl+n")
	b.typeText("hello\n")
	b.win.RequestClose(CloseReasonQuit)
	b.pump()
	b.tapDialog("Save")
	b.fd.WaitFor("the dialog", func() bool { return b.fd.PendingFileChoosers() == 1 })
	b.fd.CancelFileChooser()
	b.waitFor("the end of the dialog", func() bool { return !b.shell().closing.busy })
	if *quitsB != 0 {
		t.Error("the window quit although Save As was cancelled")
	}
}

func TestThePlatformsRequestToCloseTheWindowIsCancelledAndAnswered(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	quits := a.quits()
	a.edit("a.txt")
	if closes := a.ts.RequestWindowClose(ui.ImplicitViewID); closes {
		t.Fatal("the platform would close the window before the question was answered")
	}
	a.pump()
	a.wantAsked(askOf("a.txt"))
	// a second request while the dialog is open: ignored, still cancelled
	if closes := a.ts.RequestWindowClose(ui.ImplicitViewID); closes {
		t.Error("the second request would close the window")
	}
	a.pump()
	if a.win.Dialogs.Pending() != 0 || *quits != 0 {
		t.Fatalf("pending %d quits %d", a.win.Dialogs.Pending(), *quits)
	}
	a.tapDialog("Cancel")
	if *quits != 0 {
		t.Error("the window quit after Cancel")
	}
	// a clean window is also kept open by the platform: it closes itself
	a.doc("a.txt")
	if _, err := a.doc("a.txt").Undo(); err != nil {
		t.Fatal(err)
	}
	if closes := a.ts.RequestWindowClose(ui.ImplicitViewID); closes {
		t.Error("the platform closes the window itself")
	}
	if *quits != 1 {
		t.Errorf("Quit called %d times for a window with nothing to save", *quits)
	}
}

func TestCloseWorkspaceTabAndQuitAreCommandsWithKeysAndMenuRows(t *testing.T) {
	for _, tc := range []struct {
		keys, command string
		quit          bool
	}{
		{"alt+f4", "workbench.action.closeWindow", false},
		{"ctrl+shift+w", "workbench.action.closeWindow", false},
		{"ctrl+q", "workbench.action.quit", true},
	} {
		t.Run(tc.keys, func(t *testing.T) {
			a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
			quits := a.quits()
			a.edit("a.txt")
			a.press(tc.keys)
			a.wantAsked(askOf("a.txt"))
			a.tapDialog("Don't Save")
			// Close Workspace Tab closes the tab (another takes its place); Quit ends the application
			if tc.quit && *quits != 1 {
				t.Errorf("Quit called %d times", *quits)
			}
			if !tc.quit && (*quits != 0 || a.win.Tab() != nil) {
				t.Errorf("Quit called %d times, the tab is still there: %v", *quits, a.win.Tab() != nil)
			}
		})
	}
	a := newSaveApp(t, map[string]string{"a.txt": "one\n"}, "a.txt")
	s := a.shell()
	rows := menuRows(s.menus.Items(command.MenubarFileMenu, s.menuContext()))
	if !slices.Contains(rows, "Close Workspace Tab") || rows[len(rows)-1] != "Exit" {
		t.Errorf("file menu %v", rows)
	}
	// the menu rows run the commands
	quits := a.quits()
	for _, it := range s.menus.Items(command.MenubarFileMenu, s.menuContext()) {
		if it.ID == "workbench.action.closeWindow" {
			s.menus.Run(it)
		}
	}
	// Close Window closes the tab, which was the last one: another takes its place, and the application stays
	a.waitFor("the tab to close", func() bool { return a.win.Tab() == nil })
	if *quits != 0 {
		t.Error("closing the last tab quit the application")
	}
}

func TestWithoutAShellRequestCloseQuits(t *testing.T) {
	win := newTestWorkspace(t, "")
	n := 0
	win.Quit = func() { n++ }
	win.RequestClose(CloseReasonQuit)
	if n != 1 {
		t.Errorf("Quit called %d times", n)
	}
}

func TestEveryModeOfHotExitThatBacksUpClosesWithoutAsking(t *testing.T) {
	for _, tc := range []struct {
		hotExit string
		asks    bool
	}{{"off", true}, {"onExit", false}, {"onExitAndWindowClose", false}} {
		t.Run(tc.hotExit, func(t *testing.T) {
			win := settingsWindow(t, map[string]any{"files.hotExit": tc.hotExit})
			a := newSaveAppWith(t, win, []string{"a.txt"}, map[string]string{"a.txt": "one\n"}, "a.txt")
			quits := a.quits()
			a.edit("a.txt")
			a.win.RequestClose(CloseReasonQuit)
			if tc.asks {
				a.wantAsked(askOf("a.txt"))
				if *quits != 0 {
					t.Error("the window quit without asking")
				}
				return
			}
			a.waitFor("the quit", func() bool { return *quits == 1 })
			a.wantNoDialog()
		})
	}
}
