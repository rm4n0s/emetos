package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/mergeeditor"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The tests of the merge editor in the window (step 9.7).

// markerFile has two conflicts, the first with the labels HEAD and feature; its base has no section, so the merged result
// is the text outside the conflicts.
const markerFile = "a\n<<<<<<< HEAD\ncurrent1\n=======\nincoming1\n>>>>>>> feature\nb\n<<<<<<< HEAD\ncur2\n=======\ninc2\n>>>>>>> feature\nc\n"

// mergedMarkerFile is what the merge editor puts in the result of markerFile when it opens.
const mergedMarkerFile = "a\nb\nc\n"

// mergeApp is a window over files, one of which has conflict markers.
type mergeApp struct {
	*saveApp
}

func newMergeApp(t *testing.T, texts map[string]string, open ...string) *mergeApp {
	t.Helper()
	return &mergeApp{newSaveApp(t, texts, open...)}
}

// runWith runs a command with arguments.
func (a *mergeApp) runWith(id string, args any) error {
	return a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, id, args)
}

func (a *mergeApp) key(name string) any { return a.win.Keys.Get(name) }

// pane is the merge pane of the active group, or nil.
func (a *mergeApp) pane() *mergePane { return a.shell().activeMergePane() }

// waitMerge waits for the merge editor of the active group.
func (a *mergeApp) waitMerge() *mergePane {
	a.t.Helper()
	a.waitFor("the merge editor", func() bool { return a.pane() != nil })
	return a.pane()
}

// resolve opens the merge editor of a file the way the notification does.
func (a *mergeApp) resolve(name string) *mergePane {
	a.t.Helper()
	a.show(name, false)
	a.pressAction(a.mergeNote(), MergeConflictsButton)
	return a.waitMerge()
}

// mergeNote is the notification that offers the merge editor.
func (a *mergeApp) mergeNote() notify.Item {
	a.t.Helper()
	for _, n := range a.notes() {
		if strings.HasPrefix(n.Message, MergeConflictsNotice) {
			return n
		}
	}
	a.t.Fatalf("no notification offers the merge editor: %v", a.notes())
	return notify.Item{}
}

func (a *mergeApp) mergeNotes() int {
	n := 0
	for _, it := range a.notes() {
		if strings.HasPrefix(it.Message, MergeConflictsNotice) {
			n++
		}
	}
	return n
}

func TestAFileWithConflictMarkersOffersTheMergeEditor(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile, "plain.go": "package main\n"})
	// a file with no conflict opens quietly
	a.show("plain.go", false)
	if n := a.mergeNotes(); n != 0 {
		t.Fatalf("%d notifications for a file without conflicts", n)
	}
	a.show("c.go", false)
	if a.mergeNotes() != 1 {
		t.Fatalf("notifications %v", a.notes())
	}
	note := a.mergeNote()
	if note.Severity != notify.Info || len(note.Actions) != 1 || note.Actions[0].Label != "Resolve in Merge Editor" {
		t.Errorf("the notification is %+v", note)
	}
	if !strings.Contains(note.Message, "c.go") {
		t.Errorf("the notification does not name the file: %q", note.Message)
	}
	// the button opens the merge editor in the same group
	a.pressAction(note, MergeConflictsButton)
	p := a.waitMerge()
	wantNames(t, a.tabNames(), "plain.go", "c.go", "Merging: c.go")
	input, ok := a.shell().activeGroup().Model.ActiveEditor().(*editorgroup.MergeInput)
	if !ok || !a.shell().activeGroup().Model.IsPinned(input) {
		t.Fatalf("the active editor is %T", a.shell().activeGroup().Model.ActiveEditor())
	}
	if input.Source().Kind != editorgroup.MergeMarkers || input.Result() != a.doc("c.go") {
		t.Errorf("source %+v", input.Source())
	}
	if item := a.item("Merging: c.go"); item.Icon == nil || !strings.HasPrefix(item.Title, "Merging: ") {
		t.Errorf("tab icon %v title %q", item.Icon, item.Title)
	}
	// the inputs come from the markers, with the branch of the incoming marker as its label, and the model counts the conflicts
	m := p.Model()
	if got := m.Session().Model().ConflictCount(); got != 2 {
		t.Errorf("%d conflicts", got)
	}
	// the result is labelled with the name of its file, never a path
	if m.Label(mergeeditor.Incoming) != "feature" || m.Label(mergeeditor.Current) != "HEAD" || m.Label(mergeeditor.Result) != "c.go" {
		t.Errorf("labels %q %q %q", m.Label(mergeeditor.Incoming), m.Label(mergeeditor.Current), m.Label(mergeeditor.Result))
	}
	if m.Remaining() != "2 Conflicts Remaining" {
		t.Errorf("remaining: %q", m.Remaining())
	}
	if got := m.View(mergeeditor.Incoming).Document().Text(); got != "a\nincoming1\nb\ninc2\nc\n" {
		t.Errorf("the incoming side is %q", got)
	}
	if got := m.View(mergeeditor.Current).Document().Text(); got != "a\ncurrent1\nb\ncur2\nc\n" {
		t.Errorf("the current side is %q", got)
	}
	// the result is the document of the file, with the merged text as one change that is not saved
	if got := a.doc("c.go").Text(); got != mergedMarkerFile {
		t.Errorf("the result is %q", got)
	}
	if !a.doc("c.go").Dirty() || !a.item("Merging: c.go").Dirty {
		t.Error("the replaced result is not dirty")
	}
	if got := a.fileText("c.go"); got != markerFile {
		t.Errorf("the file on disk changed: %q", got)
	}
	// one undo brings the markers back
	if _, err := a.doc("c.go").Undo(); err != nil {
		t.Fatal(err)
	}
	if a.doc("c.go").Text() != markerFile || a.doc("c.go").Dirty() {
		t.Errorf("after the undo: %q dirty %v", a.doc("c.go").Text(), a.doc("c.go").Dirty())
	}
	// keys and title
	if a.key("activeEditor") != "emetos.merge" || a.key("isMergeEditor") != true || a.key("mergeEditorLayout") != "mixed" ||
		a.key("mergeEditorResultUri") != document.FileURI(a.files["c.go"]).String() {
		t.Errorf("keys: %v %v %v %v", a.key("activeEditor"), a.key("isMergeEditor"), a.key("mergeEditorLayout"), a.key("mergeEditorResultUri"))
	}
	if !strings.Contains(a.win.title, "Merging: c.go") {
		t.Errorf("the title of the window is %q", a.win.title)
	}
	a.noErrors()
}

func TestTheNoticeIsOnlyForAFileThatOpens(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile, "d.go": "x\n"})
	a.show("c.go", false)
	if a.mergeNotes() != 1 {
		t.Fatalf("notifications %v", a.notes())
	}
	a.win.Notify.ClearAll()
	// the same file again, while its editor is open, is not an open
	a.show("c.go", false)
	if a.mergeNotes() != 0 {
		t.Errorf("notifications %v", a.notes())
	}
	// an editor that comes back from the closed ones is not an open of the file either
	a.run("workbench.action.closeActiveEditor")
	a.run("workbench.action.reopenClosedEditor")
	a.waitFor("the editor", func() bool { return len(a.tabNames()) == 1 })
	wantNames(t, a.tabNames(), "c.go")
	if a.mergeNotes() != 0 {
		t.Errorf("notifications after reopening: %v", a.notes())
	}
	a.noErrors()
}

func TestAFileWithIncompleteMarkersGetsNoNotice(t *testing.T) {
	for name, text := range map[string]string{
		"open only":   "<<<<<<< HEAD\nx\n",
		"no splitter": "<<<<<<< HEAD\nx\n>>>>>>> feature\n",
		"indented":    "  <<<<<<< HEAD\nx\n=======\ny\n>>>>>>> feature\n",
	} {
		t.Run(name, func(t *testing.T) {
			a := newMergeApp(t, map[string]string{"c.go": text})
			a.show("c.go", false)
			if a.mergeNotes() != 0 {
				t.Errorf("notifications %v", a.notes())
			}
		})
	}
}

func TestResolveInMergeEditorCommand(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile, "plain.go": "x\n"}, "plain.go")
	// the active file has no conflict: it says so
	if err := a.runWith("git.openMergeEditor", nil); err != nil {
		t.Fatal(err)
	}
	if got := a.notes(); len(got) != 1 || got[0].Message != MergeNoConflictsNotice {
		t.Fatalf("notifications %v", got)
	}
	a.win.Notify.ClearAll()
	// the active file has the markers: the merge editor opens, as the notification's button does
	a.show("c.go", false)
	a.win.Notify.ClearAll()
	if err := a.runWith("git.openMergeEditor", nil); err != nil {
		t.Fatal(err)
	}
	a.waitMerge()
	wantNames(t, a.tabNames(), "plain.go", "c.go", "Merging: c.go")
	// asking again activates the merge editor that is open, without a second one
	a.clickTab("c.go")
	if err := a.runWith("git.openMergeEditor", nil); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the merge editor", func() bool { return a.pane() != nil })
	wantNames(t, a.tabNames(), "plain.go", "c.go", "Merging: c.go")
	if got := a.doc("c.go").Text(); got != mergedMarkerFile {
		t.Errorf("the result was replaced again: %q", got)
	}
	if got := a.notes(); len(got) != 0 {
		t.Errorf("notifications %v", got)
	}
	a.noErrors()
}

func TestResolveInMergeEditorNeedsAFile(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile})
	a.show("c.go", false)
	a.win.Notify.ClearAll()
	a.doc("c.go").SetReadOnly(true)
	if err := a.runWith("git.openMergeEditor", nil); err != nil {
		t.Fatal(err)
	}
	if got := a.notes(); len(got) != 1 || !strings.Contains(got[0].Message, "can be edited") {
		t.Errorf("notifications %v", got)
	}
	// no editor at all
	b := newMergeApp(t, map[string]string{"c.go": markerFile})
	if err := b.runWith("git.openMergeEditor", nil); err != nil {
		t.Fatal(err)
	}
	if got := b.notes(); len(got) != 1 || got[0].Message != MergeNoConflictsNotice {
		t.Errorf("notifications without an editor %v", got)
	}
}

func TestTheMergeEditorSavesAndClosesWithTheResult(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile})
	p := a.resolve("c.go")
	p.Model().Toggle(0, diff.Input1)
	p.Model().Toggle(1, diff.Input2)
	a.pump()
	if got := a.doc("c.go").Text(); got != "a\nincoming1\nb\ncur2\nc\n" {
		t.Fatalf("the result is %q", got)
	}
	if p.Model().Remaining() != "0 Conflicts Remaining" {
		t.Errorf("remaining %q", p.Model().Remaining())
	}
	// Save saves the result document, which is the file
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("c.go").Dirty() })
	if got := a.fileText("c.go"); got != "a\nincoming1\nb\ncur2\nc\n" {
		t.Errorf("the file is %q", got)
	}
	if a.item("Merging: c.go").Dirty {
		t.Error("the tab is dirty after the save")
	}
	// the text editor of the file shows the same document: closing the merge editor loses nothing while it stays, so it
	// asks nothing; with the text editor gone, a change after the save is a question
	a.clickTab("c.go")
	a.run("workbench.action.closeActiveEditor")
	a.clickTab("Merging: c.go")
	p.Model().Toggle(0, diff.Input2)
	a.pump()
	a.run("workbench.action.closeActiveEditor")
	a.wantAsked(askOf("c.go"))
	a.tapDialog("Cancel")
	a.wantNoDialog()
	if a.pane() == nil {
		t.Fatal("Cancel closed the merge editor")
	}
	a.run("workbench.action.closeActiveEditor")
	a.tapDialog("Don't Save")
	a.pump()
	if got := a.fileText("c.go"); got != "a\nincoming1\nb\ncur2\nc\n" {
		t.Errorf("the file after Don't Save is %q", got)
	}
	if len(a.tabNames()) != 0 || a.docOpen("c.go") {
		t.Errorf("tabs %v, c.go open %v", a.tabNames(), a.docOpen("c.go"))
	}
	if a.key("isMergeEditor") != nil || a.key("mergeEditorResultUri") != nil || a.key("mergeEditorLayout") != nil {
		t.Errorf("keys after closing: %v %v", a.key("isMergeEditor"), a.key("mergeEditorResultUri"))
	}
	a.noErrors()
}

func TestAMergeThatIsNotDirtyClosesWithoutAskingAndLetsGoOfTheDocument(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile})
	a.resolve("c.go")
	if _, err := a.doc("c.go").Undo(); err != nil { // back to the file as it is on disk
		t.Fatal(err)
	}
	if a.doc("c.go").Dirty() {
		t.Fatal("the document is dirty")
	}
	doc := a.doc("c.go")
	// the text editor of the file and the merge editor use the document
	if got := a.win.Documents.Refs(doc); got != 2 {
		t.Errorf("uses of the document: %d", got)
	}
	a.run("workbench.action.closeActiveEditor")
	a.wantNoDialog()
	wantNames(t, a.tabNames(), "c.go")
	if got := a.win.Documents.Refs(doc); got != 1 {
		t.Errorf("uses of the document after closing the merge editor: %d", got)
	}
	if len(a.shell().group.panes) != 0 {
		t.Error("the pane is still there")
	}
	a.noErrors()
}

func TestTheMergeEditorCannotBeSplit(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile})
	a.resolve("c.go")
	a.run("workbench.action.splitEditorRight")
	a.pump()
	panes := 0
	for _, g := range a.shell().group.grid.Groups(editorgroup.GridAppearance) {
		for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
			if _, ok := in.(*editorgroup.MergeInput); ok {
				panes++
			}
		}
	}
	if panes != 1 {
		t.Errorf("%d merge editors after a split", panes)
	}
	if got := a.doc("c.go").Text(); got != mergedMarkerFile {
		t.Errorf("the result was replaced again: %q", got)
	}
	a.noErrors()
}

func TestTheConflictCommandsMoveTheCursors(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile, "plain.go": "x\ny\n"}, "plain.go")
	// outside a merge editor the commands do nothing
	for _, id := range []string{"merge.goToNextUnhandledConflict", "merge.goToPreviousUnhandledConflict"} {
		if err := a.runWith(id, nil); err != nil {
			t.Logf("%s outside a merge editor: %v", id, err)
		}
	}
	p := a.resolve("c.go")
	result := p.Model().View(mergeeditor.Result)
	line := func() int { return result.Cursors().Primary().Position().Line + 1 }
	for i, step := range []struct {
		id   string
		want int
	}{
		{"merge.goToNextUnhandledConflict", 2}, {"merge.goToNextUnhandledConflict", 3}, {"merge.goToNextUnhandledConflict", 2},
		{"merge.goToPreviousUnhandledConflict", 3}, {"merge.goToPreviousUnhandledConflict", 2},
	} {
		if err := a.runWith(step.id, nil); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		a.pump()
		if line() != step.want {
			t.Errorf("step %d (%s): the result cursor is on line %d, want %d", i, step.id, line(), step.want)
		}
		if a, ok := p.Model().Active(); !ok || a.ConflictIndex != step.want-2 {
			t.Errorf("step %d: active %+v", i, a)
		}
	}
	// the command is in the palette of a merge editor only
	if meta, ok := a.win.Commands.Lookup("merge.goToNextUnhandledConflict"); !ok || meta.Title != "Go to Next Unhandled Conflict" || meta.Category != "Merge Editor" || meta.Precondition == nil {
		t.Errorf("meta %+v", meta)
	}
	a.noErrors()
}

func TestOpenMergeEditorCommand(t *testing.T) {
	a := newMergeApp(t, map[string]string{"in1.txt": "a\nONE\nc\n", "in2.txt": "a\nTWO\nc\n", "base.txt": "a\nb\nc\n", "out.txt": "stale\n"})
	args := map[string]any{"input1": a.files["in1.txt"], "input2": map[string]any{"uri": "file://" + a.files["in2.txt"], "description": "other branch"},
		"base": a.files["base.txt"], "output": a.files["out.txt"]}
	if err := a.runWith("_open.mergeEditor", args); err != nil {
		t.Fatal(err)
	}
	p := a.waitMerge()
	wantNames(t, a.tabNames(), "Merging: out.txt")
	m := p.Model()
	if m.Label(mergeeditor.Incoming) != "in1.txt" || m.Label(mergeeditor.Current) != "other branch" {
		t.Errorf("labels %q %q", m.Label(mergeeditor.Incoming), m.Label(mergeeditor.Current))
	}
	if got := m.Session().Model().ConflictCount(); got != 1 {
		t.Errorf("%d conflicts", got)
	}
	// the result file exists: its text is replaced by the merged one, which is not saved
	out := a.win.Documents.All()
	var result *document.Document
	for _, d := range out {
		if d.URI().Path == a.files["out.txt"] {
			result = d
		}
	}
	if result == nil || result.Text() != "a\nb\nc\n" || !result.Dirty() {
		t.Errorf("the result is %v", result)
	}
	if got := a.fileText("out.txt"); got != "stale\n" {
		t.Errorf("the file on disk changed: %q", got)
	}
	input := a.shell().activeGroup().Model.ActiveEditor().(*editorgroup.MergeInput)
	if input.Source().Kind != editorgroup.MergeFiles || input.Source().Base != a.files["base.txt"] {
		t.Errorf("source %+v", input.Source())
	}
	a.noErrors()
}

func TestOpenMergeEditorCreatesAResultThatIsNotThere(t *testing.T) {
	a := newMergeApp(t, map[string]string{"in1.txt": "a\nb\nONE\n", "in2.txt": "a\nb\nc\n", "base.txt": "a\nb\nc\n"})
	out := filepath.Join(filepath.Dir(a.files["in1.txt"]), "new-result.txt")
	if err := a.runWith("_open.mergeEditor", map[string]any{"input1": a.files["in1.txt"], "input2": a.files["in2.txt"], "base": a.files["base.txt"], "output": out}); err != nil {
		t.Fatal(err)
	}
	a.waitMerge()
	wantNames(t, a.tabNames(), "Merging: new-result.txt")
	doc, ok := a.win.Documents.Get(document.FileURI(out))
	if !ok || doc.Text() != "a\nb\nONE\n" || !doc.Dirty() {
		t.Fatalf("the result is %v", doc)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("the file was made before the merge was completed")
	}
	// saving makes the file
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !doc.Dirty() })
	if data, err := os.ReadFile(out); err != nil || string(data) != "a\nb\nONE\n" {
		t.Errorf("the file is %q, %v", data, err)
	}
	a.noErrors()
}

func TestOpenMergeEditorWithAFileThatCannotBeRead(t *testing.T) {
	a := newMergeApp(t, map[string]string{"in1.txt": "a\n", "in2.txt": "a\n", "base.txt": "a\n"})
	missing := filepath.Join(filepath.Dir(a.files["in1.txt"]), "missing.txt")
	if err := a.runWith("_open.mergeEditor", map[string]any{"input1": a.files["in1.txt"], "input2": missing, "base": a.files["base.txt"], "output": a.files["in1.txt"]}); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the notification", func() bool { return len(a.notes()) == 1 })
	if got := a.notes()[0]; !strings.Contains(got.Message, "Cannot open the merge editor") || !strings.Contains(got.Message, "missing.txt") || got.Severity != notify.Error {
		t.Errorf("notification %+v", got)
	}
	if len(a.tabNames()) != 0 || len(a.win.Documents.All()) != 0 {
		t.Errorf("tabs %v, documents %d", a.tabNames(), len(a.win.Documents.All()))
	}
}

func TestOpenMergeEditorArgumentErrors(t *testing.T) {
	a := newMergeApp(t, map[string]string{"x.txt": "x\n"})
	for _, tc := range []struct {
		name  string
		args  any
		route string
	}{
		{"not an object", "x", "shellState.openMergeEditorWith.MergeOpenInvalid"},
		{"a relative path", map[string]any{"input1": "x", "input2": "/b", "base": "/c", "output": "/d"}, "shellState.openMergeEditorWith.MergeOpenSideInvalid"},
		{"a missing side", map[string]any{"input1": "/a", "input2": "/b", "base": "/c"}, "shellState.openMergeEditorWith.MergeOpenSideInvalid"},
		{"a side that is a number", map[string]any{"input1": 1.0, "input2": "/b", "base": "/c", "output": "/d"}, "shellState.openMergeEditorWith.MergeOpenSideInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := a.runWith("_open.mergeEditor", tc.args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	if len(a.tabNames()) != 0 {
		t.Errorf("tabs %v", a.tabNames())
	}
}

func TestAResultThatCannotBeLookedAtIsReported(t *testing.T) {
	a := newMergeApp(t, map[string]string{"in1.txt": "a\n", "in2.txt": "a\n", "base.txt": "a\n"})
	// a path below a file is not a missing file: it cannot be looked at
	bad := filepath.Join(a.files["in1.txt"], "result.txt")
	var got error
	done := make(chan struct{})
	a.shell().openMergeFiles(a.files["in1.txt"], a.files["in2.txt"], a.files["base.txt"], bad, "", "", func(_ *editorgroup.MergeInput, err error) {
		got = err
		close(done)
	})
	a.waitFor("the end", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	appErr, ok := errors.FromError(got)
	if !ok || appErr.Tag != "MergeResultUnreadable" || appErr.Metadata["path"] != bad {
		t.Fatalf("error %v", got)
	}
	if !appErr.HasRoute("shellState.openMergeFiles.func1.MergeResultUnreadable") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestTheMergePaneOfAMergeThatCannotBeMadeSaysSo(t *testing.T) {
	a := newMergeApp(t, map[string]string{"plain.go": "no markers\n"})
	a.show("plain.go", false)
	// an input of a marker merge over a file that has no markers, as a restore of a file that lost them would be
	input := editorgroup.NewMergeInput(a.doc("plain.go"), editorgroup.MergeSource{Kind: editorgroup.MergeMarkers, Result: a.files["plain.go"]}, a.shell().inputOptions())
	a.win.Documents.Retain(a.doc("plain.go"))
	s := a.shell()
	s.SetState(func() {
		s.activeGroup().Model.OpenEditor(input, editorgroup.OpenOptions{Pinned: true, Active: true})
	})
	a.pump()
	p, ok := s.group.panes[input].(*mergePane)
	if !ok || p.model != nil || p.failed == nil {
		t.Fatalf("pane %T %+v", s.group.panes[input], p)
	}
	appErr, ok := errors.FromError(p.failed)
	if !ok || appErr.Tag != "MergeMarkersNotFound" {
		t.Errorf("failure %v", p.failed)
	}
	if a.doc("plain.go").Text() != "no markers\n" {
		t.Error("the document was changed")
	}
	a.noErrors()
}

// ---- --merge ----

// mergeTargets writes the four files of a --merge into the folder of the app and gives their paths: the two sides, the
// base and the result.
func (a *launchApp) mergeTargets(in1, in2, base, result string) []string {
	a.t.Helper()
	paths := []string{a.path("in1.txt"), a.path("in2.txt"), a.path("base.txt"), a.path("result.txt")}
	for i, text := range []string{in1, in2, base, result} {
		if text == "<none>" {
			continue
		}
		if err := os.WriteFile(paths[i], []byte(text), 0o600); err != nil {
			a.t.Fatal(err)
		}
	}
	return paths
}

func (a *launchApp) mergePane() *mergePane { return a.shell().activeMergePane() }

func TestLaunchMergeOpensTheMergeEditor(t *testing.T) {
	a := newLaunchApp(t, "c.go")
	paths := a.mergeTargets("a\nONE\nc\n", "a\nTWO\nc\n", "a\nb\nc\n", "stale\n")
	r := a.launch(ipc.OpenArgs{Merge: paths})
	// the client does not wait: it is told at once
	wantOutcomes(t, r, LaunchOutcome{})
	a.waitFor("the merge editor", func() bool { return a.mergePane() != nil })
	if got := a.openNames(); !slices.Equal(got, []string{"c.go", "Merging: result.txt"}) {
		t.Errorf("tabs %v", got)
	}
	p := a.mergePane()
	if p.input.Source().Input1 != paths[0] || p.input.Source().Input2 != paths[1] || p.input.Source().Base != paths[2] || p.input.Source().Result != paths[3] {
		t.Errorf("source %+v", p.input.Source())
	}
	m := p.Model()
	// the first path is the incoming side, the second the current side
	if m.View(mergeeditor.Incoming).Document().Text() != "a\nONE\nc\n" || m.View(mergeeditor.Current).Document().Text() != "a\nTWO\nc\n" {
		t.Error("the sides are the wrong way round")
	}
	if m.Label(mergeeditor.Incoming) != "in1.txt" || m.Label(mergeeditor.Current) != "in2.txt" || m.Label(mergeeditor.Result) != "result.txt" {
		t.Errorf("labels %q %q %q", m.Label(mergeeditor.Incoming), m.Label(mergeeditor.Current), m.Label(mergeeditor.Result))
	}
	if got := m.View(mergeeditor.Result).Document().Text(); got != "a\nb\nc\n" {
		t.Errorf("the result is %q", got)
	}
	if got := a.notices(); len(got) != 0 {
		t.Errorf("notices %q", got)
	}
}

func TestLaunchMergeWaitsForTheMergeEditorToClose(t *testing.T) {
	a := newLaunchApp(t)
	paths := a.mergeTargets("a\nONE\nc\n", "a\nTWO\nc\n", "a\nb\nc\n", "stale\n")
	r := a.launch(ipc.OpenArgs{Wait: true, Merge: paths})
	a.waitFor("the merge editor", func() bool { return a.mergePane() != nil })
	wantOutcomes(t, r)
	s := a.shell()
	// a merge that was not completed leaves with exit code 1 (Complete Merge leaves with 0: mergeactions_test.go)
	s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{s.activeGroup().Model.ActiveEditor()})
	a.pump()
	// the result was replaced by the merged text, so the close asks first
	tapDialogButton(a, "Don't Save")
	a.waitFor("the end of the wait", func() bool { return len(r.outcomes) == 1 })
	wantOutcomes(t, r, LaunchOutcome{ExitCode: 1, Message: MergeNotCompletedNotice})
}

func TestLaunchMergeWithAFileThatCannotBeRead(t *testing.T) {
	for _, wait := range []bool{false, true} {
		name := "without --wait"
		if wait {
			name = "with --wait"
		}
		t.Run(name, func(t *testing.T) {
			a := newLaunchApp(t)
			paths := a.mergeTargets("a\n", "<none>", "a\n", "a\n")
			r := a.launch(ipc.OpenArgs{Wait: wait, Merge: paths})
			a.waitFor("the notice", func() bool { return len(a.notices()) == 1 })
			if got := a.notices(); !strings.Contains(got[0], "in2.txt") || !strings.Contains(got[0], "Cannot open the merge editor") {
				t.Errorf("notices %q", got)
			}
			if a.mergePane() != nil || a.shell().editorCount() != 0 {
				t.Error("something was opened")
			}
			if wait {
				a.waitFor("the end of the wait", func() bool { return len(r.outcomes) == 1 })
				wantOutcomes(t, r, LaunchOutcome{ExitCode: 1, Message: "none of the files could be opened"})
			}
			if n := len(a.win.Documents.All()); n != 0 {
				t.Errorf("%d documents are kept", n)
			}
		})
	}
}

func TestLaunchMergeNeedsFourFiles(t *testing.T) {
	a := newLaunchApp(t)
	r := a.launch(ipc.OpenArgs{Wait: true, Merge: []string{a.path("a.go"), a.path("b.go")}})
	a.pump()
	if got := a.notices(); !slices.Equal(got, []string{"--merge needs four files"}) {
		t.Errorf("notices %q", got)
	}
	wantOutcomes(t, r, LaunchOutcome{ExitCode: 1, Message: "none of the files could be opened"})
}

func TestStartupMergeOfTheFirstInstance(t *testing.T) {
	win := newTestWindow(t, "")
	dir := t.TempDir()
	paths := make([]string, 4)
	for i, f := range []struct{ name, text string }{{"theirs", "a\nONE\nc\n"}, {"ours", "a\nTWO\nc\n"}, {"base", "a\nb\nc\n"}, {"merged", "stale\n"}} {
		paths[i] = filepath.Join(dir, f.name)
		if err := os.WriteFile(paths[i], []byte(f.text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	win.startup = &ipc.OpenArgs{Merge: paths}
	a := &launchApp{&explorerApp{t: t, win: win, ts: testutil.New(t, ui.Size{Width: 900, Height: 600})}}
	a.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	a.waitFor("the merge editor", func() bool { return a.mergePane() != nil })
	if got := a.openNames(); !slices.Equal(got, []string{"Merging: merged"}) {
		t.Errorf("tabs %v", got)
	}
	if got := a.notices(); len(got) != 0 {
		t.Errorf("notices %q", got)
	}
	if got := a.mergePane().Model().View(mergeeditor.Result).Document().Text(); got != "a\nb\nc\n" {
		t.Errorf("the result is %q", got)
	}
	if win.startup != nil {
		t.Error("the shell did not take the startup arguments")
	}
}

func TestLauncherHandleMerge(t *testing.T) {
	a, l := newLauncherApp(t, true)
	paths := make([]string, 4)
	for i, text := range []string{"a\nONE\nc\n", "a\nTWO\nc\n", "a\nb\nc\n", "stale\n"} {
		paths[i] = a.path([]string{"in1.txt", "in2.txt", "base.txt", "result.txt"}[i])
		if err := os.WriteFile(paths[i], []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// without --wait the reply comes at once
	reply := a.await(a.handle(l, context.Background(), ipc.OpenArgs{Merge: paths}))
	if reply.Type != ipc.TypeOK || reply.ExitCode != 0 {
		t.Errorf("reply %+v", reply)
	}
	a.waitFor("the merge editor", func() bool { return a.shell().activeMergePane() != nil })
	// with --wait the reply comes when the merge editor closes: the merge was not completed
	out := a.handle(l, context.Background(), ipc.OpenArgs{Wait: true, Merge: paths})
	a.pump()
	select {
	case r := <-out:
		t.Fatalf("a reply before the merge editor closed: %+v", r)
	default:
	}
	s := a.shell()
	for range 3 {
		if p := s.activeMergePane(); p != nil {
			s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{p.input})
			a.pump()
			if a.win.Dialogs.IsShowing() {
				tapDialogButton(a, "Don't Save")
			}
		}
	}
	reply = a.await(out)
	if reply.Type != ipc.TypeError || reply.ExitCode != 1 || reply.Message != MergeNotCompletedNotice {
		t.Errorf("reply %+v", reply)
	}
}

// ---- restoring ----

// mergeRun is a window of the state tests with the merge editor open over a marker file of the project, and hot exit on
// (the result of a merge is changed from the first moment, so the window would ask when it closes).
func mergeRun(t *testing.T) (*stateEnv, *stateRun) {
	t.Helper()
	env := newStateEnv(t, map[string]any{"files.hotExit": "onExit"})
	if err := os.WriteFile(env.path("conflict.go"), []byte(markerFile), 0o644); err != nil {
		t.Fatal(err)
	}
	run := env.start(env.root)
	run.open(env.path("conflict.go"), 0)
	run.command("git.openMergeEditor", nil)
	run.waitMerge()
	return env, run
}

// waitMerge waits for the merge editor of the active group of a window of the state tests.
func (r *stateRun) waitMerge() *mergePane {
	r.env.t.Helper()
	for range 500 {
		r.ts.FlushPosted()
		r.pump()
		if p := r.shell().activeMergePane(); p != nil {
			return p
		}
	}
	r.env.t.Fatal("timed out waiting for the merge editor")
	return nil
}

func TestAMarkerMergeComesBackAfterARestart(t *testing.T) {
	env, first := mergeRun(t)
	if got := first.layout(); !slices.Equal(got, []string{"1!: conflict.go, Merging: conflict.go*"}) {
		t.Fatalf("groups %v", got)
	}
	// the first conflict gets the incoming side before the window closes
	first.shell().activeMergePane().Model().Toggle(0, diff.Input1)
	first.pump()
	first.close()
	if data, err := os.ReadFile(env.path("conflict.go")); err != nil || string(data) != markerFile {
		t.Fatalf("the file on disk changed: %q, %v", data, err)
	}
	second := env.start(env.root)
	if got := second.layout(); !slices.Equal(got, []string{"1!: conflict.go, Merging: conflict.go*"}) {
		t.Fatalf("groups after the restart %v", got)
	}
	p := second.waitMerge()
	m := p.Model()
	// the inputs come from the markers on the disk, the result from the backup: the conflict that was resolved is handled
	if got := m.Session().Model().ConflictCount(); got != 2 {
		t.Errorf("%d conflicts", got)
	}
	if got := m.View(mergeeditor.Result).Document().Text(); got != "a\nincoming1\nb\nc\n" {
		t.Errorf("the restored result is %q", got)
	}
	if c := m.Session().Model().Conflicts()[0]; c.State.Kind != diff.StateInput1 || !c.Handled {
		t.Errorf("the first conflict is %v, handled %v", c.State, c.Handled)
	}
	if m.Remaining() != "1 Conflict Remaining" || m.Label(mergeeditor.Incoming) != "feature" {
		t.Errorf("remaining %q, label %q", m.Remaining(), m.Label(mergeeditor.Incoming))
	}
	if !p.input.Dirty() {
		t.Error("the restored merge is not dirty")
	}
	if n := len(second.win.Notify.Items()); n != 0 {
		t.Errorf("%d notifications after a restore with nothing missing", n)
	}
	if errs := second.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestAMergeOfFilesComesBackAfterARestart(t *testing.T) {
	env := newStateEnv(t, map[string]any{"files.hotExit": "onExit"})
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	args := map[string]any{"input1": write("in1", "a\nONE\nc\n"), "input2": write("in2", "a\nTWO\nc\n"), "base": write("base", "a\nb\nc\n"), "output": write("out", "stale\n")}
	first := env.start(env.root)
	first.command("_open.mergeEditor", args)
	first.waitMerge()
	if got := first.layout(); !slices.Equal(got, []string{"1!: Merging: out*"}) {
		t.Fatalf("groups %v", got)
	}
	first.shell().activeMergePane().Model().Toggle(0, diff.Input2)
	first.pump()
	first.close()
	second := env.start(env.root)
	if got := second.layout(); !slices.Equal(got, []string{"1!: Merging: out*"}) {
		t.Fatalf("groups after the restart %v", got)
	}
	p := second.waitMerge()
	if p.input.Source().Kind != editorgroup.MergeFiles || p.input.Source().Input1 != args["input1"] || p.input.Source().Result != args["output"] {
		t.Errorf("source %+v", p.input.Source())
	}
	if got := p.Model().View(mergeeditor.Result).Document().Text(); got != "a\nTWO\nc\n" {
		t.Errorf("the restored result is %q", got)
	}
	if p.Model().Remaining() != "0 Conflicts Remaining" || p.Model().Label(mergeeditor.Current) != "in2" {
		t.Errorf("remaining %q, label %q", p.Model().Remaining(), p.Model().Label(mergeeditor.Current))
	}
	if got := p.Model().View(mergeeditor.Incoming).Document().Text(); got != "a\nONE\nc\n" {
		t.Errorf("the incoming side is %q", got)
	}
}

func TestAMergeThatCannotBeMadeAgainOpensItsResultAsAText(t *testing.T) {
	t.Run("a file of --merge is gone", func(t *testing.T) {
		env := newStateEnv(t, map[string]any{"files.hotExit": "onExit"})
		dir := t.TempDir()
		paths := map[string]string{"in1": "a\nONE\nc\n", "in2": "a\nTWO\nc\n", "base": "a\nb\nc\n", "out": "stale\n"}
		for name, text := range paths {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		first := env.start(env.root)
		first.command("_open.mergeEditor", map[string]any{"input1": filepath.Join(dir, "in1"), "input2": filepath.Join(dir, "in2"),
			"base": filepath.Join(dir, "base"), "output": filepath.Join(dir, "out")})
		first.waitMerge()
		first.close()
		if err := os.Remove(filepath.Join(dir, "base")); err != nil {
			t.Fatal(err)
		}
		second := env.start(env.root)
		// the merge cannot be made again, but its result has unsaved changes: it opens as a text editor
		if got := second.layout(); !slices.Equal(got, []string{"1!: out*"}) {
			t.Errorf("groups %v", got)
		}
		if n := len(second.win.Notify.Items()); n != 0 {
			t.Errorf("%d notifications", n)
		}
	})
	t.Run("the marker file lost its markers", func(t *testing.T) {
		env, first := mergeRun(t)
		first.close()
		if err := os.WriteFile(env.path("conflict.go"), []byte("a\nb\nc\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		second := env.start(env.root)
		// the text editor of the file was saved with it; the merge cannot come back and the result stays as a text
		if got := second.layout(); !slices.Equal(got, []string{"1!: conflict.go*"}) {
			t.Errorf("groups %v", got)
		}
	})
}

func TestAMergeFollowsTheSettingsOfItsEditors(t *testing.T) {
	// the editors of a merge read the settings as any editor does: a font size reaches all three
	_, run := mergeRun(t)
	m := run.shell().activeMergePane().Model()
	if err := run.win.Settings.Update(context.Background(), "editor.fontSize", 20, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	run.pump()
	for _, s := range []mergeeditor.Side{mergeeditor.Incoming, mergeeditor.Current, mergeeditor.Result} {
		if got := m.View(s).Options().FontSize; got != 20 {
			t.Errorf("%v: font size %v", s, got)
		}
		if got := m.View(s).Options().Minimap.Enabled; got {
			t.Errorf("%v: the minimap is on", s)
		}
	}
	if !m.View(mergeeditor.Incoming).ReadOnly() || m.View(mergeeditor.Result).ReadOnly() {
		t.Error("read-only options changed with the settings")
	}
}
