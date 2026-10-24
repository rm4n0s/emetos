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
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// The tests of auto save (step 7.3, files.autoSave): the window feeds the controller of core/document with
// edits, saves, focus and the active editor, and saves through the path of Ctrl+S. The timers run on the fake
// clock of the tester.

// newAutoApp is a window with the given settings that shows the files named in open.
func newAutoApp(t *testing.T, extra map[string]any, texts map[string]string, open ...string) *saveApp {
	t.Helper()
	names := make([]string, 0, len(texts))
	for n := range texts {
		names = append(names, n)
	}
	slices.Sort(names)
	return newSaveAppWith(t, settingsWindow(t, extra), names, texts, open...)
}

// afterDelay are the settings of auto save after a delay.
func afterDelay(ms int) map[string]any {
	return map[string]any{"files.autoSave": "afterDelay", "files.autoSaveDelay": ms}
}

// errorNotes are the notifications of errors.
func (a *saveApp) errorNotes() []notify.Item {
	var out []notify.Item
	for _, n := range a.notes() {
		if n.Severity == notify.Error {
			out = append(out, n)
		}
	}
	return out
}

// settle pumps the frames and the work that the goroutines handed over.
func (a *saveApp) settle() {
	a.ts.FlushPosted()
	a.pump()
	a.ts.FlushPosted()
}

// changedByAnotherProgram writes a file as another program would, so that saving it is a conflict.
func (a *saveApp) changedByAnotherProgram(name string) {
	a.t.Helper()
	if err := os.WriteFile(a.files[name], []byte("changed by another program, longer than before\n"), 0o644); err != nil {
		a.t.Fatal(err)
	}
}

// untitled makes a dirty untitled document the active editor.
func (a *saveApp) untitled(text string) *document.Document {
	a.t.Helper()
	a.run("workbench.action.files.newUntitledFile")
	a.typeText(text)
	doc, ok := a.win.Documents.Get(document.UntitledURI(1))
	if !ok {
		a.t.Fatal("no untitled document")
	}
	return doc
}

func TestAutoSaveAfterDelaySavesOnceTheTypingStops(t *testing.T) {
	a := newAutoApp(t, afterDelay(500), map[string]string{"a.txt": "one\n"}, "a.txt")
	a.typeText("x")
	a.ts.Pump(400 * time.Millisecond)
	if !a.doc("a.txt").Dirty() || a.fileText("a.txt") != "one\n" {
		t.Fatal("saved before the delay")
	}
	// typing again starts the delay over
	a.typeText("y")
	a.ts.Pump(400 * time.Millisecond)
	if !a.doc("a.txt").Dirty() || a.fileText("a.txt") != "one\n" {
		t.Fatal("saved although the user went on typing")
	}
	a.ts.Pump(200 * time.Millisecond)
	a.waitFor("the auto save", func() bool { return !a.doc("a.txt").Dirty() })
	if got := a.fileText("a.txt"); got != "xyone\n" {
		t.Errorf("file = %q", got)
	}
	if a.dirtyTab("a.txt") || strings.Contains(a.win.Title(), "●") {
		t.Errorf("tab %+v, title %q: still dirty", a.item("a.txt"), a.win.Title())
	}
	a.wantNoDialog()
	if len(a.notes()) != 0 {
		t.Errorf("notifications: %v", a.notes())
	}
	// the history survives, as with Ctrl+S
	if !a.doc("a.txt").CanUndo() {
		t.Error("the history is gone")
	}
	a.noErrors()
}

func TestAutoSaveSavesWithoutDialogsAndEveryFileOnItsOwn(t *testing.T) {
	a := newAutoApp(t, afterDelay(300), map[string]string{"a.txt": "a\n", "b.txt": "b\n"}, "a.txt", "b.txt")
	a.edit("a.txt")
	a.edit("b.txt")
	a.ts.Pump(400 * time.Millisecond)
	a.waitFor("both files", func() bool { return !a.doc("a.txt").Dirty() && !a.doc("b.txt").Dirty() })
	if a.fileText("a.txt") != "xa\n" || a.fileText("b.txt") != "xb\n" {
		t.Errorf("a.txt %q, b.txt %q", a.fileText("a.txt"), a.fileText("b.txt"))
	}
	a.wantNoDialog()
}

func TestAutoSaveIsCancelledByASaveAndByAnUndoToTheSavedText(t *testing.T) {
	a := newAutoApp(t, afterDelay(300), map[string]string{"a.txt": "one\n"}, "a.txt")
	a.typeText("x")
	a.press("ctrl+z")
	if a.doc("a.txt").Dirty() {
		t.Fatal("undo left the document dirty")
	}
	a.ts.Pump(time.Second)
	a.settle()
	if a.fileText("a.txt") != "one\n" {
		t.Errorf("a clean document was written: %q", a.fileText("a.txt"))
	}
	if a.shell().saves.running[a.doc("a.txt")] != nil {
		t.Error("a save is going on")
	}
	a.typeText("y")
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	a.ts.Pump(time.Second)
	a.settle()
	if got := a.fileText("a.txt"); got != "yone\n" {
		t.Errorf("file = %q", got)
	}
}

func TestAutoSaveKeepsTheTrailingWhiteSpaceOfTheCursorLine(t *testing.T) {
	extra := afterDelay(300)
	extra["files.trimTrailingWhitespace"] = true
	a := newAutoApp(t, extra, map[string]string{"a.txt": "one  \ntwo  \n"}, "a.txt")
	// the cursor is at the start of the first line; the white space at the end of the lines is trimmed, but not
	// where the user is typing
	if err := a.vm().RunCommand("cursorBottom", nil); err != nil {
		t.Fatal(err)
	}
	a.typeText("x ")
	a.ts.Pump(400 * time.Millisecond)
	a.waitFor("the auto save", func() bool { return !a.doc("a.txt").Dirty() })
	if got := a.fileText("a.txt"); got != "one\ntwo\nx " {
		t.Errorf("an auto save wrote %q: the white space of the cursor line must stay", got)
	}
	// a save of the user trims it
	a.typeText("y ")
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	if got := a.fileText("a.txt"); got != "one\ntwo\nx y" {
		t.Errorf("Ctrl+S wrote %q", got)
	}
}

func TestAutoSaveOnFocusChange(t *testing.T) {
	t.Run("another editor becomes active", func(t *testing.T) {
		a := newAutoApp(t, map[string]any{"files.autoSave": "onFocusChange"}, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}, "a.txt", "b.txt")
		a.clickTab("a.txt")
		a.typeText("x")
		a.ts.Pump(10 * time.Second) // time alone saves nothing
		a.settle()
		if !a.doc("a.txt").Dirty() {
			t.Fatal("saved without a focus change")
		}
		a.clickTab("b.txt")
		a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
		if a.fileText("a.txt") != "xa\n" || a.fileText("b.txt") != "b\n" {
			t.Errorf("a.txt %q b.txt %q", a.fileText("a.txt"), a.fileText("b.txt"))
		}
		a.wantNoDialog()
	})
	t.Run("the editor loses the focus to another widget", func(t *testing.T) {
		a := newAutoApp(t, map[string]any{"files.autoSave": "onFocusChange"}, map[string]string{"a.txt": "a\n"}, "a.txt")
		a.typeText("x")
		a.settle()
		if !a.doc("a.txt").Dirty() {
			t.Fatal("saved while the editor has the focus")
		}
		a.run("workbench.action.showCommands") // the quick input takes the focus
		a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
		if a.fileText("a.txt") != "xa\n" {
			t.Errorf("file = %q", a.fileText("a.txt"))
		}
	})
	t.Run("another file opens", func(t *testing.T) {
		a := newAutoApp(t, map[string]any{"files.autoSave": "onFocusChange"}, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}, "a.txt")
		a.typeText("x")
		a.show("b.txt", false)
		a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	})
}

func TestAutoSaveEventsPerModeInTheWindow(t *testing.T) {
	const (
		tabs   = "another tab is clicked"
		window = "the window loses the focus"
		timer  = "ten seconds pass"
	)
	tests := []struct {
		mode  string
		event string
		saved bool
	}{
		{"off", tabs, false}, {"off", window, false}, {"off", timer, false},
		{"afterDelay", tabs, false}, {"afterDelay", window, false},
		{"onFocusChange", tabs, true}, {"onFocusChange", window, true}, {"onFocusChange", timer, false},
		{"onWindowChange", tabs, false}, {"onWindowChange", window, true}, {"onWindowChange", timer, false},
	}
	for _, tt := range tests {
		t.Run(tt.mode+"/"+tt.event, func(t *testing.T) {
			// a delay of an hour: afterDelay does not save within the test
			extra := map[string]any{"files.autoSave": tt.mode, "files.autoSaveDelay": 3600 * 1000}
			a := newAutoApp(t, extra, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}, "a.txt", "b.txt")
			a.clickTab("a.txt")
			a.typeText("x")
			a.edit("b.txt")
			switch tt.event {
			case tabs:
				a.clickTab("b.txt")
			case window:
				a.ts.SetViewFocus(ui.ImplicitViewID, false)
			case timer:
				a.ts.Pump(10 * time.Second)
			}
			if tt.saved {
				a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
			} else {
				a.settle()
			}
			if got := !a.doc("a.txt").Dirty(); got != tt.saved {
				t.Errorf("a.txt saved = %v, want %v", got, tt.saved)
			}
			// the window losing the focus saves every dirty file; a tab change only the one that was left
			bSaved := tt.saved && tt.event == window
			if tt.saved && tt.event == window {
				a.waitFor("b.txt", func() bool { return !a.doc("b.txt").Dirty() })
			}
			if got := !a.doc("b.txt").Dirty(); got != bSaved {
				t.Errorf("b.txt saved = %v, want %v", got, bSaved)
			}
			a.wantNoDialog()
			a.noErrors()
		})
	}
}

func TestAutoSaveNeverSavesUntitledReadOnlyOrExcludedDocuments(t *testing.T) {
	t.Run("untitled", func(t *testing.T) {
		a := newAutoApp(t, afterDelay(100), map[string]string{"a.txt": "a\n"}, "a.txt")
		doc := a.untitled("hello")
		a.ts.Pump(time.Second)
		a.ts.SetViewFocus(ui.ImplicitViewID, false)
		a.settle()
		if !doc.Dirty() || doc.URI().Scheme != document.SchemeUntitled {
			t.Errorf("the untitled document was saved: %v dirty = %v", doc.URI(), doc.Dirty())
		}
		a.wantNoDialog()
		if a.fd.PendingFileChoosers() != 0 {
			t.Error("a save dialog opened")
		}
	})
	t.Run("read-only", func(t *testing.T) {
		a := newAutoApp(t, afterDelay(100), map[string]string{"a.txt": "a\n"}, "a.txt")
		a.typeText("x")
		a.doc("a.txt").SetReadOnly(true)
		a.ts.Pump(time.Second)
		a.settle()
		if a.fileText("a.txt") != "a\n" || len(a.notes()) != 0 {
			t.Errorf("file %q, notifications %v", a.fileText("a.txt"), a.notes())
		}
	})
	t.Run("outside the folder with autoSaveWorkspaceFilesOnly", func(t *testing.T) {
		extra := afterDelay(100)
		extra["files.autoSaveWorkspaceFilesOnly"] = true
		a := newAutoApp(t, extra, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}, "a.txt", "b.txt")
		// the folder of the window holds b.txt only
		folder := filepath.Join(filepath.Dir(a.files["a.txt"]), "inside")
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		a.win.Folder = folder
		a.edit("a.txt")
		a.ts.Pump(time.Second)
		a.settle()
		if !a.doc("a.txt").Dirty() || a.fileText("a.txt") != "a\n" {
			t.Fatal("a file outside the folder was saved")
		}
		// the setting off, the file is saved
		a.setting("files.autoSaveWorkspaceFilesOnly", false)
		a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
		// a file in the folder is saved with it on
		a.setting("files.autoSaveWorkspaceFilesOnly", true)
		a.win.Folder = filepath.Dir(a.files["b.txt"])
		a.edit("b.txt")
		a.ts.Pump(time.Second)
		a.waitFor("the file in the folder", func() bool { return !a.doc("b.txt").Dirty() })
	})
}

func TestAutoSaveWhenNoErrorsWaitsForTheMarkersToGo(t *testing.T) {
	extra := afterDelay(100)
	extra["files.autoSaveWhenNoErrors"] = true
	a := newAutoApp(t, extra, map[string]string{"a.txt": "a\n"}, "a.txt")
	path := a.files["a.txt"]
	marker := problems.Marker{Severity: problems.SeverityError, Message: "boom", StartLine: 1, StartColumn: 1, EndLine: 1, EndColumn: 2}
	a.win.Markers.Set("test", path, []problems.Marker{marker})
	a.edit("a.txt")
	a.ts.Pump(time.Second)
	a.settle()
	if !a.doc("a.txt").Dirty() {
		t.Fatal("a file with an error was saved")
	}
	// a warning is not an error
	warning := marker
	warning.Severity = problems.SeverityWarning
	a.win.Markers.Set("test", path, []problems.Marker{warning})
	a.waitFor("the save once the error is gone", func() bool { return !a.doc("a.txt").Dirty() })
	if a.fileText("a.txt") != "xa\n" {
		t.Errorf("file = %q", a.fileText("a.txt"))
	}
}

func TestAutoSaveFailureIsOneNotificationAndPausesTheDocument(t *testing.T) {
	a := newAutoApp(t, afterDelay(100), map[string]string{"a.txt": "one\n"}, "a.txt")
	a.changedByAnotherProgram("a.txt")
	a.typeText("x")
	a.ts.Pump(200 * time.Millisecond)
	a.waitFor("the error", func() bool { return len(a.errorNotes()) == 1 })
	n := a.errorNote()
	if !strings.Contains(n.Message, "'a.txt'") || !slices.Contains(actionLabels(n), "Overwrite") || !slices.Contains(actionLabels(n), "Save As...") {
		t.Errorf("message %q, buttons %v", n.Message, actionLabels(n))
	}
	if !a.shell().autosave.ctl.Paused(a.doc("a.txt")) {
		t.Error("the document is not paused")
	}
	// typing on does not try again, and no other notification is made
	disk := a.fileText("a.txt")
	for range 5 {
		a.typeText("y")
		a.ts.Pump(time.Second)
	}
	a.settle()
	if len(a.errorNotes()) != 1 || a.fileText("a.txt") != disk {
		t.Errorf("%d notifications, file %q", len(a.errorNotes()), a.fileText("a.txt"))
	}
	a.ts.SetViewFocus(ui.ImplicitViewID, false)
	a.settle()
	if len(a.errorNotes()) != 1 {
		t.Errorf("a focus change tried again: %d notifications", len(a.errorNotes()))
	}
	// Overwrite is the user's save: it succeeds, the notification goes and auto save works again
	a.pressAction(a.errorNote(), "Overwrite")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	if len(a.errorNotes()) != 0 || a.shell().autosave.ctl.Paused(a.doc("a.txt")) {
		t.Errorf("notifications %v, paused %v", a.errorNotes(), a.shell().autosave.ctl.Paused(a.doc("a.txt")))
	}
	a.typeText("z")
	a.ts.Pump(200 * time.Millisecond)
	a.waitFor("the next auto save", func() bool { return !a.doc("a.txt").Dirty() })
	if !strings.Contains(a.fileText("a.txt"), "z") {
		t.Errorf("file = %q", a.fileText("a.txt"))
	}
}

func TestAutoSaveResumesWhenTheFileIsRevertedOrReadAgain(t *testing.T) {
	a := newAutoApp(t, afterDelay(100), map[string]string{"a.txt": "one\n"}, "a.txt")
	a.changedByAnotherProgram("a.txt")
	a.typeText("x")
	a.ts.Pump(200 * time.Millisecond)
	a.waitFor("the error", func() bool { return len(a.errorNotes()) == 1 })
	a.pressAction(a.errorNote(), "Revert")
	a.waitFor("the revert", func() bool { return !a.doc("a.txt").Dirty() })
	if a.shell().autosave.ctl.Paused(a.doc("a.txt")) {
		t.Fatal("still paused after the revert")
	}
	a.typeText("y")
	a.ts.Pump(200 * time.Millisecond)
	a.waitFor("the auto save", func() bool { return !a.doc("a.txt").Dirty() })
	if !strings.Contains(a.fileText("a.txt"), "y") {
		t.Errorf("file = %q", a.fileText("a.txt"))
	}
}

func TestAutoSaveLeavesADocumentAloneWhileItsSaveAsDialogIsOpen(t *testing.T) {
	a := newAutoApp(t, afterDelay(100), map[string]string{"a.txt": "one\n"}, "a.txt")
	release := make(chan struct{})
	a.win.SaveDialog = func(ctx context.Context, _ widgets.BuildContext, _ SaveDialogOptions) (string, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "", nil // cancelled
	}
	a.typeText("x")
	a.run("workbench.action.files.saveAs")
	a.settle()
	a.ts.Pump(time.Second)
	a.settle()
	if !a.doc("a.txt").Dirty() || a.fileText("a.txt") != "one\n" {
		t.Fatal("auto save wrote the file while the Save As dialog was open")
	}
	close(release)
	// the dialog gave nothing: the document is tried again after the delay
	a.waitFor("the dialog to end", func() bool { return !a.shell().saves.asOpen[a.doc("a.txt")] })
	a.ts.Pump(200 * time.Millisecond)
	a.waitFor("the auto save", func() bool { return !a.doc("a.txt").Dirty() })
	if a.fileText("a.txt") != "xone\n" {
		t.Errorf("file = %q", a.fileText("a.txt"))
	}
}

func TestAutoSaveFollowsTheSettingsWhileTheWindowRuns(t *testing.T) {
	t.Run("switching on saves what is dirty at once", func(t *testing.T) {
		a := newAutoApp(t, nil, map[string]string{"a.txt": "one\n"}, "a.txt")
		a.typeText("x")
		a.setting("files.autoSave", "afterDelay")
		a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
		if a.fileText("a.txt") != "xone\n" {
			t.Errorf("file = %q", a.fileText("a.txt"))
		}
	})
	t.Run("switching off cancels the timers", func(t *testing.T) {
		a := newAutoApp(t, afterDelay(5000), map[string]string{"a.txt": "one\n"}, "a.txt")
		a.typeText("x")
		a.setting("files.autoSave", "off")
		a.ts.Pump(20 * time.Second)
		a.settle()
		if !a.doc("a.txt").Dirty() || a.fileText("a.txt") != "one\n" {
			t.Error("saved after auto save was switched off")
		}
	})
	t.Run("a new delay applies to the next edit", func(t *testing.T) {
		a := newAutoApp(t, afterDelay(3600*1000), map[string]string{"a.txt": "one\n"}, "a.txt")
		a.setting("files.autoSaveDelay", 200)
		a.typeText("x")
		a.ts.Pump(300 * time.Millisecond)
		a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	})
	t.Run("a value that is not a mode means off", func(t *testing.T) {
		a := newAutoApp(t, map[string]any{"files.autoSave": "sometimes"}, map[string]string{"a.txt": "one\n"}, "a.txt")
		a.typeText("x")
		a.ts.Pump(10 * time.Second)
		a.ts.SetViewFocus(ui.ImplicitViewID, false)
		a.settle()
		if !a.doc("a.txt").Dirty() {
			t.Error("saved with an unknown mode")
		}
	})
}

func TestACloseSavesInsteadOfAskingWithOnFocusChangeAndOnWindowChange(t *testing.T) {
	for _, mode := range []string{"onFocusChange", "onWindowChange"} {
		t.Run(mode, func(t *testing.T) {
			a := newAutoApp(t, map[string]any{"files.autoSave": mode}, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}, "a.txt", "b.txt")
			a.edit("a.txt")
			a.clickTab("a.txt")
			a.run("workbench.action.closeActiveEditor")
			a.waitFor("the tab to close", func() bool { return len(a.tabNames()) == 1 })
			a.wantNoDialog()
			if a.fileText("a.txt") != "xa\n" {
				t.Errorf("a.txt = %q", a.fileText("a.txt"))
			}
			if a.docOpen("a.txt") {
				t.Error("the document is still open")
			}
			if a.shell().autosave.ctl.Tracked(a.docs["a.txt"]) {
				t.Error("the controller still follows a closed document")
			}
		})
	}
}

func TestACloseAsksWithOffAndAfterDelay(t *testing.T) {
	for name, extra := range map[string]map[string]any{
		"off":        nil,
		"afterDelay": {"files.autoSave": "afterDelay", "files.autoSaveDelay": 3600 * 1000},
	} {
		t.Run(name, func(t *testing.T) {
			a := newAutoApp(t, extra, map[string]string{"a.txt": "a\n"}, "a.txt")
			a.edit("a.txt")
			a.run("workbench.action.closeActiveEditor")
			a.wantAsked(askOf("a.txt"))
			a.tapDialog("Don't Save")
			wantNames(t, a.tabNames())
			if a.fileText("a.txt") != "a\n" {
				t.Errorf("a.txt = %q", a.fileText("a.txt"))
			}
		})
	}
}

func TestAClosingBatchSavesWhatAutoSaveSavesAndAsksAboutTheRest(t *testing.T) {
	a := newAutoApp(t, map[string]any{"files.autoSave": "onFocusChange"}, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}, "a.txt", "b.txt")
	a.edit("a.txt")
	a.edit("b.txt")
	untitled := a.untitled("scratch")
	a.run("workbench.action.closeAllEditors")
	// the two files are saved; the untitled document, which has no file, is asked about
	a.waitFor("the question", a.win.Dialogs.IsShowing)
	a.wantAsked(askOf("Untitled-1"))
	if a.fileText("a.txt") != "xa\n" || a.fileText("b.txt") != "xb\n" {
		t.Errorf("a.txt %q b.txt %q", a.fileText("a.txt"), a.fileText("b.txt"))
	}
	// Cancel keeps the editors, and the saved files stay saved
	a.tapDialog("Cancel")
	wantNames(t, a.tabNames(), "a.txt", "Untitled-1", "b.txt")
	if a.fileText("a.txt") != "xa\n" || a.doc("a.txt").Dirty() || !untitled.Dirty() {
		t.Errorf("a.txt %q dirty %v, untitled dirty %v", a.fileText("a.txt"), a.doc("a.txt").Dirty(), untitled.Dirty())
	}
}

func TestACloseWhoseAutoSaveFailsAsksAndDontSaveIsNotUndoneByTheFocus(t *testing.T) {
	a := newAutoApp(t, map[string]any{"files.autoSave": "onFocusChange"}, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}, "a.txt", "b.txt")
	a.clickTab("a.txt")
	a.typeText("x")
	a.changedByAnotherProgram("a.txt")
	elsewhere := a.fileText("a.txt")
	a.run("workbench.action.closeActiveEditor")
	a.waitFor("the error", func() bool { return len(a.errorNotes()) == 1 })
	a.wantAsked(askOf("a.txt"))
	a.tapDialog("Don't Save")
	a.settle()
	wantNames(t, a.tabNames(), "b.txt")
	a.ts.SetViewFocus(ui.ImplicitViewID, false)
	a.settle()
	if got := a.fileText("a.txt"); got != elsewhere {
		t.Errorf("a.txt = %q: Don't Save was undone by an auto save", got)
	}
	// Cancel on the same question keeps the editor
	a.clickTab("b.txt")
	a.show("a.txt", false)
	a.typeText("y")
	a.changedByAnotherProgram("a.txt")
	a.run("workbench.action.closeActiveEditor")
	a.waitFor("the second error", func() bool { return len(a.errorNotes()) >= 1 })
	a.wantAsked(askOf("a.txt"))
	a.tapDialog("Cancel")
	wantNames(t, a.tabNames(), "b.txt", "a.txt")
}

func TestACloseOutsideTheFolderAsksWithAutoSaveWorkspaceFilesOnly(t *testing.T) {
	a := newAutoApp(t, map[string]any{"files.autoSave": "onFocusChange", "files.autoSaveWorkspaceFilesOnly": true}, map[string]string{"a.txt": "a\n"}, "a.txt")
	a.win.Folder = filepath.Join(filepath.Dir(a.files["a.txt"]), "elsewhere")
	a.edit("a.txt")
	a.run("workbench.action.closeActiveEditor")
	a.wantAsked(askOf("a.txt"))
	a.tapDialog("Cancel")
	if a.fileText("a.txt") != "a\n" {
		t.Errorf("a.txt = %q", a.fileText("a.txt"))
	}
}

func TestClosingTheWindowSavesWhatAutoSaveSavesInEveryMode(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]any
		saved bool
	}{
		{"off", nil, false},
		{"afterDelay", map[string]any{"files.autoSave": "afterDelay", "files.autoSaveDelay": 3600 * 1000}, true},
		{"onFocusChange", map[string]any{"files.autoSave": "onFocusChange"}, true},
		{"onWindowChange", map[string]any{"files.autoSave": "onWindowChange"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extra := map[string]any{"files.trimTrailingWhitespace": true}
			for k, v := range tt.extra {
				extra[k] = v
			}
			a := newAutoApp(t, extra, map[string]string{"a.txt": "a\n"}, "a.txt")
			quits := a.quits()
			if err := a.vm().RunCommand("cursorBottom", nil); err != nil {
				t.Fatal(err)
			}
			a.typeText("x  ")
			a.win.RequestClose(CloseReasonWindow)
			if !tt.saved {
				a.wantAsked(askOf("a.txt"))
				a.tapDialog("Cancel")
				return
			}
			a.waitFor("the quit", func() bool { return *quits == 1 })
			a.wantNoDialog()
			// as VS Code's shutdown save, the save participants are skipped
			if got := a.fileText("a.txt"); got != "a\nx  " {
				t.Errorf("a.txt = %q", got)
			}
		})
	}
}

func TestClosingTheWindowAsksAboutWhatAutoSaveCannotSave(t *testing.T) {
	t.Run("an untitled document", func(t *testing.T) {
		a := newAutoApp(t, afterDelay(3600*1000), map[string]string{"a.txt": "a\n"}, "a.txt")
		quits := a.quits()
		a.edit("a.txt")
		a.untitled("scratch")
		a.win.RequestClose(CloseReasonWindow)
		a.waitFor("the question", a.win.Dialogs.IsShowing)
		a.wantAsked(askOf("Untitled-1"))
		if a.fileText("a.txt") != "xa\n" {
			t.Errorf("a.txt = %q", a.fileText("a.txt"))
		}
		a.tapDialog("Cancel")
		if *quits != 0 {
			t.Error("the window quit")
		}
	})
	t.Run("a save that fails", func(t *testing.T) {
		a := newAutoApp(t, afterDelay(3600*1000), map[string]string{"a.txt": "a\n"}, "a.txt")
		quits := a.quits()
		a.edit("a.txt")
		a.changedByAnotherProgram("a.txt")
		a.win.RequestClose(CloseReasonWindow)
		a.waitFor("the error", func() bool { return len(a.errorNotes()) == 1 })
		a.wantAsked(askOf("a.txt"))
		a.tapDialog("Cancel")
		if *quits != 0 {
			t.Error("the window quit")
		}
	})
}

func TestToggleAutoSave(t *testing.T) {
	a := newAutoApp(t, nil, map[string]string{"a.txt": "one\n"}, "a.txt")
	value := func() string { return settings.Get[string](a.win.Settings, "files.autoSave", "") }
	if value() != "off" {
		t.Fatalf("files.autoSave = %q", value())
	}
	a.run("workbench.action.toggleAutoSave")
	if value() != "afterDelay" {
		t.Errorf("after the first toggle: %q", value())
	}
	a.run("workbench.action.toggleAutoSave")
	if value() != "off" {
		t.Errorf("after the second toggle: %q", value())
	}
	// every mode but off becomes off
	for _, mode := range []string{"onFocusChange", "onWindowChange", "afterDelay"} {
		a.setting("files.autoSave", mode)
		a.run("workbench.action.toggleAutoSave")
		if value() != "off" {
			t.Errorf("toggling %s gave %q", mode, value())
		}
	}
	// the toggle on a window with changes saves them (the setting changed)
	a.typeText("x")
	a.run("workbench.action.toggleAutoSave")
	a.waitFor("the save", func() bool { return !a.doc("a.txt").Dirty() })
	a.noErrors()
}

func TestToggleAutoSaveWritesTheWorkspaceFileWhenTheWorkspaceSetsIt(t *testing.T) {
	folder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, ".emetos", "settings.json"), []byte(`{"files.autoSave": "onFocusChange"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	win := newTestWindow(t, folder)
	a := newSaveAppWith(t, win, []string{"a.txt"}, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.run("workbench.action.toggleAutoSave")
	if got := settings.Get[string](win.Settings, "files.autoSave", ""); got != "off" {
		t.Errorf("files.autoSave = %q", got)
	}
	if in := win.Settings.Inspect("files.autoSave", ""); !in.Workspace.Ok || in.Workspace.Value != "off" || in.User.Ok {
		t.Errorf("workspace %+v, user %+v", in.Workspace, in.User)
	}
}

func TestTheFileMenuShowsAutoSaveChecked(t *testing.T) {
	a := newAutoApp(t, nil, map[string]string{"a.txt": "one\n"}, "a.txt")
	row := func() kit.MenuItem {
		t.Helper()
		if err := a.win.Commands.Run(command.Context{Ctx: t.Context(), Keys: a.win.Keys, Source: command.SourceAPI}, "dev.openMenu", map[string]any{"id": command.MenubarFileMenu}); err != nil {
			t.Fatal(err)
		}
		a.pump()
		els := a.ts.Elements(gluttertest.ByType[*kit.Menu]())
		if len(els) == 0 {
			t.Fatal("no menu is open")
		}
		it, ok := menuItem(els[0].Widget().(*kit.Menu).Items(), "workbench.action.toggleAutoSave")
		a.ts.SendKeyEvent(services.LogicalKeyboardKeyEscape)
		a.pump()
		if !ok {
			t.Fatal("the File menu has no Auto Save row")
		}
		return it
	}
	if it := row(); it.Checked || it.Disabled {
		t.Errorf("row %+v with auto save off", it)
	}
	for _, mode := range []string{"afterDelay", "onFocusChange", "onWindowChange"} {
		a.setting("files.autoSave", mode)
		if it := row(); !it.Checked || it.Disabled {
			t.Errorf("row %+v with %s", it, mode)
		}
	}
	a.setting("files.autoSave", "off")
	if it := row(); it.Checked {
		t.Errorf("row %+v with auto save off again", it)
	}
	// the row runs the toggle
	a.setting("files.autoSave", "off")
	if err := a.win.Commands.Run(command.Context{Ctx: t.Context(), Keys: a.win.Keys, Source: command.SourceAPI}, "dev.openMenu", map[string]any{"id": command.MenubarFileMenu}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	a.ts.Tap(gluttertest.ByText("Auto Save"))
	a.pump()
	if got := settings.Get[string](a.win.Settings, "files.autoSave", ""); got != "afterDelay" {
		t.Errorf("a click on the row gave %q", got)
	}
}

func TestConfigKeysAreContextKeysOfTheKeybindings(t *testing.T) {
	win := newTestWindow(t, "")
	ran := 0
	if err := win.Commands.Register(command.Meta{ID: "test.whenAutoSaveIsOn", Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := win.Commands.SetHandler("test.whenAutoSaveIsOn", func(command.Context, any) error { ran++; return nil }); err != nil {
		t.Fatal(err)
	}
	kb, err := keybinding.ParseKeybinding("ctrl+alt+shift+f")
	if err != nil {
		t.Fatal(err)
	}
	when, err := contextkey.Parse("config.files.autoSave != 'off'")
	if err != nil {
		t.Fatal(err)
	}
	item := keybinding.NewItem(kb, "test.whenAutoSaveIsOn", nil, when, false)
	win.Keybindings = keybinding.NewResolver(nil, []keybinding.Item{item}, nil)
	win.Dispatcher = keybinding.NewDispatcher(win.Keybindings, time.Now)
	chord := keybinding.Chord{Ctrl: true, Alt: true, Shift: true, Key: keybinding.KeyF}
	if win.DispatchKey(chord, nil) || ran != 0 {
		t.Fatalf("the key was used with auto save off: ran %d", ran)
	}
	if err := win.Settings.Update(context.Background(), "files.autoSave", "onWindowChange", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if !win.DispatchKey(chord, nil) || ran != 1 {
		t.Errorf("the key was not used with auto save on: ran %d", ran)
	}
}

func TestAutoSaveCommandAndMenuAreRegistered(t *testing.T) {
	win := newTestWindow(t, "")
	meta, ok := win.Commands.Lookup("workbench.action.toggleAutoSave")
	if !ok || meta.Title != "Toggle Auto Save" || meta.Category != "File" {
		t.Fatalf("command %+v, %v", meta, ok)
	}
	if _, bound := win.Keybindings.LookupPrimaryKeybinding("workbench.action.toggleAutoSave", win.Keys, false); bound {
		t.Error("Toggle Auto Save has a default key, VS Code gives it none")
	}
}
