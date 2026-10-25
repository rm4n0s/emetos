package app

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/diffeditor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The tests of the ways to open a diff, of the inline view in the window and of the commands that choose it (step 9.6).

// activeDiff is the diff input of the active editor of the group, which must be one.
func activeDiff(t *testing.T, s *shellState) *editorgroup.DiffInput {
	t.Helper()
	in, ok := s.activeGroup().Model.ActiveEditor().(*editorgroup.DiffInput)
	if !ok {
		t.Fatalf("the active editor is %T", s.activeGroup().Model.ActiveEditor())
	}
	return in
}

func (a *saveApp) waitDiffPane() *diffPane {
	a.t.Helper()
	a.waitFor("the diff editor", func() bool { return len(a.shell().diffPanes()) > 0 })
	return a.shell().diffPanes()[0]
}

func TestCompareWithSavedByKeyShowsTheFileOnDiskAgainstTheDocument(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "one\ntwo\n"}, "a.go")
	a.typeText("zero\n")
	a.press("ctrl+k d")
	pane := a.waitDiffPane()
	wantNames(t, a.tabNames(), "a.go", "a.go (in file) ↔ a.go")
	in := activeDiff(t, a.shell())
	if in.Original().Text() != "one\ntwo\n" || !in.Original().ReadOnly() || in.Original().URI().Scheme != document.SchemeVirtual {
		t.Errorf("the left side is %v with %q (read only %v)", in.Original().URI(), in.Original().Text(), in.Original().ReadOnly())
	}
	if in.Modified() != a.doc("a.go") || in.Modified().Text() != "zero\none\ntwo\n" || !in.Modified().Dirty() {
		t.Errorf("the right side is the document with %q (dirty %v)", in.Modified().Text(), in.Modified().Dirty())
	}
	if !a.shell().activeGroup().Model.IsPinned(in) {
		t.Error("the diff editor is a preview")
	}
	// the language of the file is the language of the saved text
	if in.Original().LanguageID() != a.doc("a.go").LanguageID() {
		t.Errorf("the saved text is %q, the document %q", in.Original().LanguageID(), a.doc("a.go").LanguageID())
	}
	pane.Model().Recompute()
	a.waitFor("the changes", func() bool { return len(pane.Model().Changes()) == 1 })

	// a save makes the saved text what was saved: the left side follows
	a.press("ctrl+s")
	a.waitFor("the new saved text", func() bool { return in.Original().Text() == "zero\none\ntwo\n" })
	a.waitFor("no changes", func() bool { return pane.Model().NoChanges() })
	// another program changes the file: the left side follows that too
	if err := os.WriteFile(a.files["a.go"], []byte("elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.win.Commands.Run(apiContext(a.win), "workbench.action.focusActiveEditorGroup", nil); err != nil {
		t.Fatal(err)
	}
	if err := pane.Model().View(diffeditor.Modified).RunCommand("type", map[string]any{"text": "more\n"}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if err := a.win.Commands.Run(apiContext(a.win), "workbench.files.action.compareWithSaved", nil); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the text on disk", func() bool { return in.Original().Text() == "elsewhere\n" })
	wantNames(t, a.tabNames(), "a.go", "a.go (in file) ↔ a.go")
	a.noErrors()
}

func TestCompareWithSavedFollowsAChangeOfTheFileOnDisk(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "one\n"}, "a.go")
	a.typeText("x")
	a.run("workbench.files.action.compareWithSaved")
	a.waitDiffPane()
	in := activeDiff(t, a.shell())
	a.changedByAnotherProgram("a.go")
	// the watcher tells the window; here the batch of events is handed over as the window does
	a.shell().externalFileEvents(nil)
	events := []workspace.Event{{Kind: workspace.EventChanged, Path: a.files["a.go"]}}
	a.shell().externalFileEvents(events)
	a.waitFor("the new saved text", func() bool { return strings.HasPrefix(in.Original().Text(), "changed by another program") })
	a.noErrors()
}

func TestCompareWithSavedOnlyForFiles(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "one\n"})
	// no editor
	a.run("workbench.files.action.compareWithSaved")
	// an untitled document has no saved version
	a.untitled("text")
	a.run("workbench.files.action.compareWithSaved")
	a.pump()
	if n := len(a.shell().diffPanes()); n != 0 {
		t.Errorf("%d diff editors", n)
	}
	// a virtual document has none either
	if err := a.runVirtual(); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.files.action.compareWithSaved")
	if n := len(a.shell().diffPanes()); n != 0 {
		t.Errorf("%d diff editors", n)
	}
	a.noErrors()
}

func (a *saveApp) runVirtual() error {
	return a.win.Commands.Run(apiContext(a.win), "workbench.action.openGlobalKeybindings", nil)
}

func TestCompareWithSavedTakesAPathAndFailsOnOtherArguments(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "one\n", "b.go": "two\n"}, "a.go", "b.go")
	a.show("b.go", false)
	if a.active() != "b.go" {
		t.Fatalf("the active tab is %q", a.active())
	}
	if err := a.win.Commands.Run(apiContext(a.win), "workbench.files.action.compareWithSaved", a.files["a.go"]); err != nil {
		t.Fatal(err)
	}
	a.waitDiffPane()
	if in := activeDiff(t, a.shell()); in.Modified() != a.doc("a.go") {
		t.Errorf("the right side is %v", in.Modified().URI())
	}
	// a path that no editor has open does nothing
	before := len(a.tabNames())
	if err := a.win.Commands.Run(apiContext(a.win), "workbench.files.action.compareWithSaved", "/not/open.go"); err != nil {
		t.Fatal(err)
	}
	if len(a.tabNames()) != before {
		t.Error("a tab was opened")
	}
	err := a.win.Commands.Run(apiContext(a.win), "workbench.files.action.compareWithSaved", 42)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("shellState.compareWithSaved.CompareResourceInvalid") {
		t.Errorf("a number: %v", err)
	}
}

func TestCompareWithSavedOfADiffEditorComparesItsModifiedFile(t *testing.T) {
	a := newDiffApp(t, "a\n", "a\nb\n")
	a.typeIn("x")
	a.run("workbench.files.action.compareWithSaved")
	a.waitFor("a second diff editor", func() bool { return len(a.tabNames()) == 2 })
	wantNames(t, a.tabNames(), "old.go ↔ new.go", "new.go (in file) ↔ new.go")
}

func TestCompareWithTheClipboard(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "one\ntwo\n"}, "a.go")
	a.ts.SetClipboardText("one\n2\n")
	a.press("ctrl+k c")
	a.waitDiffPane()
	wantNames(t, a.tabNames(), "a.go", "Clipboard ↔ a.go")
	in := activeDiff(t, a.shell())
	if in.Original().Text() != "one\n2\n" || !in.Original().ReadOnly() || in.Modified() != a.doc("a.go") {
		t.Errorf("the sides are %q (read only %v) and %v", in.Original().Text(), in.Original().ReadOnly(), in.Modified().URI())
	}
	// the same text again is another comparison, as the clipboard may have changed
	a.ts.SetClipboardText("something else\n")
	a.run("workbench.files.action.compareWithClipboard")
	a.waitFor("a second comparison", func() bool { return len(a.tabNames()) == 3 })
	if got := activeDiff(t, a.shell()).Original().Text(); got != "something else\n" {
		t.Errorf("the clipboard of the second comparison is %q", got)
	}
	a.noErrors()
}

func TestCompareProvidersFailWithTheirOwnTags(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "one\n"})
	win := a.win
	for _, tc := range []struct {
		name  string
		uri   document.URI
		route string
	}{
		{"a number of the clipboard that is not one", document.VirtualURI("/clipboard/x/a.go"), "CompareClipboardNumberInvalid"},
		{"a text that is gone", document.VirtualURI("/clipboard/99/a.go"), "CompareClipboardGone"},
		{"a file that is not there", savedURI("/not/there/a.go"), "FileNotFound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := win.Documents.OpenVirtual(context.Background(), tc.uri)
			appErr, ok := errors.FromError(err)
			if !ok || appErr.Tag != "VirtualGenerateFailed" || !appErr.HasRoute("Documents.OpenVirtual->VirtualRegistry.Generate.VirtualGenerateFailed") {
				t.Fatalf("the failure is %v", err)
			}
			cause, ok := errors.FromError(errors.Unwrap(err))
			if !ok {
				t.Fatalf("the cause is %T: %v", errors.Unwrap(err), errors.Unwrap(err))
			}
			if !strings.Contains(cause.Route(), tc.route) && cause.Tag != tc.route {
				t.Errorf("the cause is %s (%s), want %s", cause.Tag, cause.Route(), tc.route)
			}
		})
	}
}

func TestCompareFileWithAsksForAFileInTheQuickOpen(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.openFile("go.mod")
	a.openFile("main.go")
	if err := a.run("workbench.files.action.compareFileWith", nil); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if !a.win.QuickInput.Visible() {
		t.Fatal("the quick open is not shown")
	}
	// the active file is the first item, and the next one is the active one
	a.waitFor("the files", func() bool { return len(a.qoLabels()) > 1 })
	if got := a.win.QuickInput.Picker().ActiveIndex(); got != 1 {
		t.Errorf("the active row is %d, want the second", got)
	}
	a.qoType("deep")
	a.qoWait("deep.md")
	a.chord("enter")
	a.waitFor("the diff editor", func() bool { return len(a.shell().diffPanes()) == 1 })
	in := activeDiff(t, a.shell())
	if in.Original().URI() != document.FileURI(a.path("main.go")) || in.Modified().URI() != document.FileURI(a.path("docs/more/deep.md")) {
		t.Errorf("the sides are %v and %v: the active file on the left, the picked one on the right", in.Original().URI(), in.Modified().URI())
	}
	// the file was not opened as a text editor
	for _, input := range a.shell().activeGroup().Model.Editors(editorgroup.Sequential, false) {
		if strings.HasSuffix(input.Name(), "deep.md") && input.TypeID() != editorgroup.DiffInputTypeID {
			t.Errorf("deep.md was opened as an editor too")
		}
	}
}

func TestCompareFileWithCancelledOrWithoutAnEditorDoesNothing(t *testing.T) {
	a := newFileApp(t, projectFiles)
	if err := a.run("workbench.files.action.compareFileWith", nil); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if a.win.QuickInput.Visible() {
		t.Error("the quick open is shown without an editor")
	}
	a.openFile("main.go")
	if err := a.run("workbench.files.action.compareFileWith", nil); err != nil {
		t.Fatal(err)
	}
	a.pump()
	a.chord("escape")
	if a.win.QuickInput.Visible() || len(a.shell().diffPanes()) != 0 {
		t.Error("Escape did not end the pick without a diff")
	}
	// the next quick open opens files as usual
	a.mustRunQuickOpenAndPick(t)
}

func (a *explorerApp) mustRunQuickOpenAndPick(t *testing.T) {
	t.Helper()
	if err := a.run("workbench.action.quickOpen", nil); err != nil {
		t.Fatal(err)
	}
	a.pump()
	a.qoType("deep")
	a.qoWait("deep.md")
	a.chord("enter")
	a.waitFor("the file", func() bool { n, _ := a.activeName(); return n == "deep.md" })
	if len(a.shell().diffPanes()) != 0 {
		t.Error("a plain quick open opened a diff")
	}
}

func TestSelectForCompareAndCompareWithSelectedFromTheExplorer(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	if a.win.Keys.Get("resourceSelectedForCompare") == true {
		t.Fatal("a file is selected for compare at the start")
	}
	a.ts.TapAt(a.rowCenter("go.mod"), secondaryDown)
	a.pump()
	a.chooseMenuRow("Select for Compare")
	if a.win.Keys.Get("resourceSelectedForCompare") != true || a.shell().compare.source != a.path("go.mod") {
		t.Fatalf("key %v, source %q", a.win.Keys.Get("resourceSelectedForCompare"), a.shell().compare.source)
	}
	// the menu of another file offers the comparison, and of a folder does not
	a.ts.TapAt(a.rowCenter("main.go"), secondaryDown)
	a.pump()
	if rows := menuRows(a.openMenu0()); !slices.Contains(rows, "Compare with Selected") || !slices.Contains(rows, "Select for Compare") {
		t.Errorf("the menu of a file: %v", rows)
	}
	a.chooseMenuRow("Compare with Selected")
	a.waitFor("the diff editor", func() bool { return len(a.shell().diffPanes()) == 1 })
	in := activeDiff(t, a.shell())
	if in.Original().URI() != document.FileURI(a.path("go.mod")) || in.Modified().URI() != document.FileURI(a.path("main.go")) {
		t.Errorf("the sides are %v and %v: the selected file on the left, the pressed one on the right", in.Original().URI(), in.Modified().URI())
	}
	a.ts.TapAt(a.rowCenter("docs"), secondaryDown)
	a.pump()
	if rows := menuRows(a.openMenu0()); slices.Contains(rows, "Compare with Selected") || slices.Contains(rows, "Select for Compare") {
		t.Errorf("the menu of a folder: %v", rows)
	}
	// the key goes with the window's shell
	a.ts.PumpWidget(widgets.NewSizedBoxShrink(nil))
	if a.win.Keys.Get("resourceSelectedForCompare") == true {
		t.Error("the key is still set after the shell is gone")
	}
}

func TestCompareWithSelectedWithoutASelectionDoesNothing(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	a.focusTree("main.go")
	a.mustRun("compareFiles", nil)
	a.mustRun("compareSelected", nil)
	if len(a.shell().diffPanes()) != 0 {
		t.Error("a diff opened")
	}
	// a folder is not selected for compare
	a.focusTree("docs")
	a.mustRun("selectForCompare", nil)
	if a.win.Keys.Get("resourceSelectedForCompare") == true {
		t.Error("a folder was selected for compare")
	}
}

func TestCompareSelectedComparesTheTwoSelectedFiles(t *testing.T) {
	a := newFileApp(t, projectFiles)
	a.view().Tree().SetSelected(a.path("go.mod"))
	a.view().Tree().ClickWith(a.path("main.go"), kit.ClickModifiers{Ctrl: true}, false, false)
	a.view().Tree().SetFocus(a.path("main.go"))
	a.pump()
	a.ts.TapAt(a.rowCenter("main.go"), secondaryDown)
	a.pump()
	if rows := menuRows(a.openMenu0()); !slices.Contains(rows, "Compare Selected") || slices.Contains(rows, "Select for Compare") {
		t.Fatalf("the menu: %v", rows)
	}
	a.chooseMenuRow("Compare Selected")
	a.waitFor("the diff editor", func() bool { return len(a.shell().diffPanes()) == 1 })
	in := activeDiff(t, a.shell())
	// the tree lists go.mod before main.go: the first selected row is on the left
	if in.Original().URI() != document.FileURI(a.path("go.mod")) || in.Modified().URI() != document.FileURI(a.path("main.go")) {
		t.Errorf("the sides are %v and %v", in.Original().URI(), in.Modified().URI())
	}
	// with a folder among the two nothing opens
	a.view().Tree().SetSelected(a.path("docs"))
	a.view().Tree().ClickWith(a.path("go.mod"), kit.ClickModifiers{Ctrl: true}, false, false)
	a.view().Tree().SetFocus(a.path("go.mod"))
	a.mustRun("compareSelected", nil)
	if len(a.shell().diffPanes()) != 1 || len(a.tabNames0()) != 1 {
		t.Errorf("tabs %v", a.tabNames0())
	}
}

func (a *explorerApp) tabNames0() []string {
	var out []string
	for _, in := range a.shell().activeGroup().Model.Editors(editorgroup.Sequential, false) {
		out = append(out, in.Name())
	}
	return out
}

func TestCompareOfAConflictOpensTheFileOnDiskAgainstTheDirtyDocument(t *testing.T) {
	for _, via := range []string{"the notice of a change on disk", "the notice of a save that found the file newer"} {
		t.Run(via, func(t *testing.T) {
			a := newSaveApp(t, map[string]string{"a.go": "one\ntwo\n"}, "a.go")
			a.typeText("mine\n")
			a.changedByAnotherProgram("a.go")
			var note notify.Item
			if strings.HasPrefix(via, "the notice of a change") {
				a.shell().externalFileEvents([]workspace.Event{{Kind: workspace.EventChanged, Path: a.files["a.go"]}})
				a.waitFor("the notice", func() bool { return len(a.notes()) == 1 })
				note = a.notes()[0]
			} else {
				a.press("ctrl+s")
				a.waitFor("the error", func() bool { return len(a.notes()) == 1 })
				note = a.errorNote()
			}
			if got := actionLabels(note); !slices.Equal(got[:3], []string{"Compare", "Overwrite", "Revert"}) {
				t.Fatalf("the buttons are %v", got)
			}
			a.pressAction(note, "Compare")
			a.waitDiffPane()
			in := activeDiff(t, a.shell())
			wantNames(t, a.tabNames(), "a.go", "a.go (in file) ↔ a.go")
			if !strings.HasPrefix(in.Original().Text(), "changed by another program") || !in.Original().ReadOnly() {
				t.Errorf("the left side is %q (read only %v): the file on disk", in.Original().Text(), in.Original().ReadOnly())
			}
			if in.Modified() != a.doc("a.go") || in.Modified().Text() != "mine\none\ntwo\n" {
				t.Errorf("the right side is the document with %q", in.Modified().Text())
			}
			// the notification is still there, with the same buttons
			if len(a.notes()) != 1 || !slices.Contains(actionLabels(a.notes()[0]), "Overwrite") {
				t.Errorf("notifications %v", a.notes())
			}
			// overwriting resolves it as before, and the comparison shows the saved text
			a.pressAction(a.notes()[0], "Overwrite")
			a.waitFor("the save", func() bool { return !a.doc("a.go").Dirty() })
			a.waitFor("the saved text", func() bool { return in.Original().Text() == "mine\none\ntwo\n" })
			a.waitFor("the notice to go", func() bool { return len(a.notes()) == 0 })
			a.noErrors()
		})
	}
}

// ---- the views of the diff editor in the window ----

func TestToggleInlineViewWritesTheSettingAndSwitchesTheView(t *testing.T) {
	a := newDiffApp(t, "a\nremoved\nb\n", "a\nb\nc\n")
	m := a.model()
	if m.Mode() != diffeditor.SideBySide {
		t.Fatalf("the mode is %v at 1280 pixels", m.Mode())
	}
	if !a.hasCommandInPalette("toggle.diff.renderSideBySide") {
		t.Error("Toggle Inline View is not in the palette of a compare editor")
	}
	a.run("toggle.diff.renderSideBySide")
	a.waitFor("the inline view", func() bool { return m.Mode() == diffeditor.Inline })
	if v := settingValue(a.win, "diffEditor.renderSideBySide"); v != false {
		t.Errorf("diffEditor.renderSideBySide is %v", v)
	}
	if got := a.shellScopeKey("diffEditorInlineMode"); got != true {
		t.Errorf("diffEditorInlineMode is %v", got)
	}
	a.run("toggle.diff.renderSideBySide")
	a.waitFor("side by side", func() bool { return m.Mode() == diffeditor.SideBySide })
	if got := a.shellScopeKey("diffEditorInlineMode"); got != false {
		t.Errorf("diffEditorInlineMode is %v", got)
	}
	a.noErrors()
}

func TestTheViewModeCommands(t *testing.T) {
	a := newDiffApp(t, "a\nremoved\nb\n", "a\nb\nc\n")
	m := a.model()
	for _, tc := range []struct {
		id                  string
		sideBySide, limited bool
		mode                diffeditor.Mode
	}{
		{"diffEditor.setViewMode.inline", false, true, diffeditor.Inline},
		{"diffEditor.setViewMode.sideBySide", true, false, diffeditor.SideBySide},
		{"diffEditor.setViewMode.automatic", true, true, diffeditor.SideBySide},
		{"diffEditor.toggleUseInlineViewWhenSpaceIsLimited", true, false, diffeditor.SideBySide},
		{"diffEditor.toggleUseInlineViewWhenSpaceIsLimited", true, true, diffeditor.SideBySide},
	} {
		a.run(tc.id)
		a.waitFor(tc.id, func() bool { return m.Mode() == tc.mode })
		if settingValue(a.win, "diffEditor.renderSideBySide") != tc.sideBySide || settingValue(a.win, "diffEditor.useInlineViewWhenSpaceIsLimited") != tc.limited {
			t.Errorf("%s: renderSideBySide %v, useInlineViewWhenSpaceIsLimited %v", tc.id,
				settingValue(a.win, "diffEditor.renderSideBySide"), settingValue(a.win, "diffEditor.useInlineViewWhenSpaceIsLimited"))
		}
	}
}

func TestTheInlineViewInTheWindowKeepsTheEditorAndNavigates(t *testing.T) {
	var orig, mod strings.Builder
	for i := range 60 {
		orig.WriteString("line " + string(rune('a'+i%26)) + "\n")
		if i == 9 || i == 29 {
			mod.WriteString("changed\n")
			continue
		}
		mod.WriteString("line " + string(rune('a'+i%26)) + "\n")
	}
	a := newDiffApp(t, orig.String(), mod.String())
	m := a.view(diffeditor.Modified)
	a.setting("diffEditor.renderSideBySide", false)
	a.waitFor("the inline view", func() bool { return a.model().Mode() == diffeditor.Inline })
	a.waitFor("the zones", func() bool { return len(a.model().InlineZones()) == 2 })
	a.press("alt+f5")
	if got := m.Cursors().Primary().Position().Line + 1; got != 10 {
		t.Errorf("the cursor is on line %d, want 10", got)
	}
	a.press("alt+f5")
	if got := m.Cursors().Primary().Position().Line + 1; got != 30 {
		t.Errorf("the cursor is on line %d, want 30", got)
	}
	if !a.pane().Model().Focus(diffeditor.Modified).HasFocus() {
		t.Error("the modified editor does not have the focus")
	}
	// typing works in the inline view: it edits the modified document
	a.typeIn("x")
	if !a.doc("new.go").Dirty() {
		t.Error("the modified document is not edited")
	}
	// the sides' commands all act on the one editor
	a.run("workbench.action.compareEditor.focusSecondarySide")
	if !a.pane().Model().Focus(diffeditor.Modified).HasFocus() {
		t.Error("focusing the second side in the inline view lost the editor")
	}
	a.run("diffEditor.switchSide")
	a.noErrors()
}

func TestTheKeysOfTheBreakpointFollowTheWidth(t *testing.T) {
	a := newDiffApp(t, "a\nb\n", "a\nc\n")
	m := a.model()
	if got := a.shellScopeKey("diffEditorRenderSideBySideInlineBreakpointReached"); got != false {
		t.Errorf("the breakpoint is reached at 1280 pixels: %v", got)
	}
	a.setting("diffEditor.renderSideBySideInlineBreakpoint", 3000)
	a.waitFor("the inline view", func() bool { return m.Mode() == diffeditor.Inline })
	if got := a.shellScopeKey("diffEditorRenderSideBySideInlineBreakpointReached"); got != true {
		t.Errorf("the breakpoint is not reached: %v", got)
	}
	a.setting("diffEditor.useInlineViewWhenSpaceIsLimited", false)
	a.waitFor("side by side", func() bool { return m.Mode() == diffeditor.SideBySide })
	if got := a.shellScopeKey("diffEditorRenderSideBySideInlineBreakpointReached"); got != true {
		t.Errorf("the breakpoint is reached whatever the other setting says: %v", got)
	}
}

// shellScopeKey is the value of a context key in the scope of the focused editor.
func (a *diffApp) shellScopeKey(key string) any {
	a.t.Helper()
	a.pump()
	return a.win.Scopes.ScopeOf(a.ts.Binding.FocusManager().PrimaryFocus(), a.win.Keys).Get(key)
}

func (a *diffApp) hasCommandInPalette(id string) bool {
	for _, it := range a.win.Process.Menus.Items("CommandPalette", a.win.Keys) {
		if it.Command == id {
			return true
		}
	}
	return false
}

func settingValue(win *Window, key string) any { return settings.Get[bool](win.Settings, key, "") }

// apiContext is the context a command runs in when it is run from code.
func apiContext(win *Window) command.Context {
	return command.Context{Ctx: context.Background(), Keys: win.Keys, Source: command.SourceAPI}
}
