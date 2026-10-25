package app

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/diffeditor"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// The tests of the diff editor in the window (step 9.5).

// diffApp is a window with a diff editor open over two files.
type diffApp struct {
	*saveApp
}

// newDiffApp makes a window with a diff of two files, opened with dev.openDiff, and waits for its first diff.
func newDiffApp(t *testing.T, original, modified string) *diffApp {
	t.Helper()
	a := &diffApp{newSaveApp(t, map[string]string{"old.go": original, "new.go": modified})}
	a.openDiff("old.go", "new.go")
	return a
}

// runWith runs a command with arguments.
func (a *diffApp) runWith(id string, args any) error {
	return a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, id, args)
}

// openDiff opens a comparison with dev.openDiff and waits for its first diff.
func (a *diffApp) openDiff(original, modified string) {
	a.t.Helper()
	if err := a.runWith("dev.openDiff", map[string]any{"original": a.files[original], "modified": a.files[modified]}); err != nil {
		a.t.Fatalf("dev.openDiff: %v", err)
	}
	a.pump()
	a.waitDiff()
}

// waitDiff waits until the diff of the active pane is computed and not waiting for another.
func (a *diffApp) waitDiff() {
	a.t.Helper()
	a.waitFor("the diff", func() bool { return a.pane() != nil && a.pane().Model().Computed() && !a.pane().Model().Computing() })
}

// pane is the diff pane of the active group, or nil.
func (a *diffApp) pane() *diffPane {
	panes := a.shell().diffPanes()
	if len(panes) == 0 {
		return nil
	}
	return panes[0]
}

func (a *diffApp) model() *diffeditor.Model { return a.pane().Model() }

// view is the view model of a side of the diff editor.
func (a *diffApp) view(s diffeditor.Side) *editor.ViewModel { return a.pane().Model().View(s) }

// typeIn types text into the modified editor.
func (a *diffApp) typeIn(text string) {
	a.t.Helper()
	if err := a.view(diffeditor.Modified).RunCommand("type", map[string]any{"text": text}); err != nil {
		a.t.Fatal(err)
	}
	a.pump()
}

// afterDebounce lets the delay of the diff pass and waits for the new diff.
func (a *diffApp) afterDebounce() {
	a.t.Helper()
	a.ts.Pump(diffeditor.DebounceDelay + 10*time.Millisecond)
	a.waitDiff()
}

func (a *diffApp) key(name string) any { return a.win.Keys.Get(name) }

func TestDevOpenDiffOpensOneKeptTab(t *testing.T) {
	a := newDiffApp(t, "a\nb\nc\n", "a\nB\nc\nd\n")
	wantNames(t, a.tabNames(), "old.go ↔ new.go")
	g := a.shell().activeGroup()
	input, ok := g.Model.ActiveEditor().(*editorgroup.DiffInput)
	if !ok {
		t.Fatalf("the active editor is %T", g.Model.ActiveEditor())
	}
	if !g.Model.IsPinned(input) {
		t.Error("the diff editor is a preview")
	}
	item := a.item("old.go ↔ new.go")
	if item.Icon == nil || !strings.Contains(item.Title, "old.go ↔ ") || !strings.HasSuffix(item.Title, "new.go") {
		t.Errorf("tab icon %v title %q", item.Icon, item.Title)
	}
	if a.key("activeEditor") != "emetos.diff" || a.key("textCompareEditorVisible") != true || a.key("textCompareEditorActive") != true {
		t.Errorf("keys: activeEditor %v visible %v active %v", a.key("activeEditor"), a.key("textCompareEditorVisible"), a.key("textCompareEditorActive"))
	}
	if !strings.Contains(a.win.title, "old.go ↔ new.go") {
		t.Errorf("the title of the window is %q", a.win.title)
	}
	// both sides are shown and the diff is there
	if n := len(a.model().Changes()); n != 2 {
		t.Errorf("%d changes", n)
	}
	if o, m := a.view(diffeditor.Original), a.view(diffeditor.Modified); o.Document() != a.doc("old.go") || m.Document() != a.doc("new.go") {
		t.Error("the sides show other documents")
	}
	// opening the same comparison again activates it
	a.openDiffNoWait("old.go", "new.go")
	wantNames(t, a.tabNames(), "old.go ↔ new.go")
	a.noErrors()
}

func (a *diffApp) openDiffNoWait(original, modified string) {
	a.t.Helper()
	if err := a.runWith("dev.openDiff", map[string]any{"original": a.files[original], "modified": a.files[modified]}); err != nil {
		a.t.Fatalf("dev.openDiff: %v", err)
	}
	a.pump()
}

func TestTheOriginalIsReadOnlyAndTheDirtyMarkIsTheModifiedSides(t *testing.T) {
	a := newDiffApp(t, "a\nb\n", "a\nb\n")
	orig, mod := a.view(diffeditor.Original), a.view(diffeditor.Modified)
	if !orig.ReadOnly() || mod.ReadOnly() || a.doc("old.go").ReadOnly() {
		t.Fatalf("read-only: original %v modified %v, original document %v", orig.ReadOnly(), mod.ReadOnly(), a.doc("old.go").ReadOnly())
	}
	err := orig.RunCommand("type", "zz")
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("EditorReadOnly") {
		t.Errorf("typing in the original: %v", err)
	}
	if a.item("old.go ↔ new.go").Dirty || a.doc("old.go").Dirty() {
		t.Error("the diff is dirty after a refused edit")
	}
	a.typeIn("x")
	if !a.item("old.go ↔ new.go").Dirty || a.key("activeEditorIsDirty") != true {
		t.Errorf("after typing in the modified side: tab dirty %v, key %v", a.item("old.go ↔ new.go").Dirty, a.key("activeEditorIsDirty"))
	}
	// an edit of the other document that the original side shows (made by a text editor, say) is not the diff's
	a.edit("old.go")
	if !a.doc("old.go").Dirty() {
		t.Fatal("the original document did not change")
	}
	a.run("workbench.action.files.save") // saves the modified side only
	a.waitFor("the save", func() bool { return !a.doc("new.go").Dirty() })
	if a.item("old.go ↔ new.go").Dirty || a.key("activeEditorIsDirty") != false {
		t.Error("the diff is dirty after the save")
	}
	if !a.doc("old.go").Dirty() || a.fileText("old.go") != "a\nb\n" {
		t.Errorf("the original side was saved: dirty %v, file %q", a.doc("old.go").Dirty(), a.fileText("old.go"))
	}
	a.noErrors()
}

func TestCtrlSSavesTheModifiedSide(t *testing.T) {
	a := newDiffApp(t, "a\nb\n", "a\nb\n")
	a.typeIn("x")
	a.press("ctrl+s")
	a.waitFor("the save", func() bool { return !a.doc("new.go").Dirty() })
	if got := a.fileText("new.go"); got != "xa\nb\n" {
		t.Errorf("new.go = %q", got)
	}
	if got := a.fileText("old.go"); got != "a\nb\n" {
		t.Errorf("old.go = %q", got)
	}
	// Save All saves it too
	a.typeIn("y")
	a.run("saveAll")
	a.waitFor("the save", func() bool { return !a.doc("new.go").Dirty() })
	if got := a.fileText("new.go"); got != "xya\nb\n" {
		t.Errorf("new.go after Save All = %q", got)
	}
	a.noErrors()
}

func TestClosingADirtyDiffAsksAboutTheModifiedDocument(t *testing.T) {
	for _, tc := range []struct {
		answer string
		closed bool
		saved  bool
	}{{"Save", true, true}, {"Don't Save", true, false}, {"Cancel", false, false}} {
		t.Run(tc.answer, func(t *testing.T) {
			a := newDiffApp(t, "a\nb\n", "a\nb\n")
			a.typeIn("x")
			a.run("workbench.action.closeActiveEditor")
			a.wantAsked(askOf("new.go"))
			a.tapDialog(tc.answer)
			if tc.saved {
				a.waitFor("the save", func() bool { return !a.docOpen("new.go") })
			}
			a.wantNoDialog()
			if tc.closed {
				if len(a.tabNames()) != 0 || a.docOpen("new.go") || a.docOpen("old.go") {
					t.Errorf("tabs %v, new.go open %v, old.go open %v", a.tabNames(), a.docOpen("new.go"), a.docOpen("old.go"))
				}
			} else if len(a.tabNames()) != 1 || !a.doc("new.go").Dirty() {
				t.Errorf("tabs %v, dirty %v", a.tabNames(), a.doc("new.go").Dirty())
			}
			want := "a\nb\n"
			if tc.saved {
				want = "xa\nb\n"
			}
			if got := a.fileText("new.go"); got != want {
				t.Errorf("new.go = %q, want %q", got, want)
			}
			a.noErrors()
		})
	}
}

func TestACleanDiffClosesWithoutAskingAndLetsGoOfItsDocuments(t *testing.T) {
	a := newDiffApp(t, "a\n", "b\n")
	oldDoc, newDoc := a.doc("old.go"), a.doc("new.go")
	if a.win.Documents.Refs(oldDoc) != 1 || a.win.Documents.Refs(newDoc) != 1 {
		t.Errorf("uses of the documents: %d and %d", a.win.Documents.Refs(oldDoc), a.win.Documents.Refs(newDoc))
	}
	a.run("workbench.action.closeActiveEditor")
	a.wantNoDialog()
	if len(a.tabNames()) != 0 || a.docOpen("old.go") || a.docOpen("new.go") {
		t.Errorf("tabs %v, open: %v %v", a.tabNames(), a.docOpen("old.go"), a.docOpen("new.go"))
	}
	if len(a.shell().group.panes) != 0 || len(a.shell().diffPanes()) != 0 {
		t.Error("the pane is still there")
	}
	if a.key("textCompareEditorVisible") != false || a.key("textCompareEditorActive") != false {
		t.Errorf("keys after closing: %v %v", a.key("textCompareEditorVisible"), a.key("textCompareEditorActive"))
	}
	a.noErrors()
}

func TestADocumentThatATextEditorShowsToo(t *testing.T) {
	a := newDiffApp(t, "a\nb\nc\n", "a\nb\nc\n")
	a.show("new.go", false) // a text editor in the same group
	wantNames(t, a.tabNames(), "old.go ↔ new.go", "new.go")
	if a.win.Documents.Refs(a.doc("new.go")) != 2 {
		t.Errorf("uses of the document: %d", a.win.Documents.Refs(a.doc("new.go")))
	}
	// an edit in the text editor reaches the diff after the delay
	a.edit("new.go")
	a.clickTab("old.go ↔ new.go")
	a.afterDebounce()
	if a.model().NoChanges() {
		t.Error("the diff did not see the edit of the text editor")
	}
	// the diff closes without a question while the text editor shows the changed document
	a.run("workbench.action.closeActiveEditor")
	a.wantNoDialog()
	wantNames(t, a.tabNames(), "new.go")
	if !a.docOpen("new.go") || !a.doc("new.go").Dirty() {
		t.Error("the document went with the diff")
	}
	a.noErrors()
}

func TestSplittingTheDiffMakesAnotherPane(t *testing.T) {
	a := newDiffApp(t, "a\nb\n", "a\nc\n")
	a.run("workbench.action.splitEditorRight")
	a.pump()
	groups := a.shell().group.grid.Groups(editorgroup.GridAppearance)
	if len(groups) != 2 {
		t.Fatalf("%d groups", len(groups))
	}
	var panes []*diffPane
	for _, g := range groups {
		if p, ok := a.shell().group.panes[g.Model.ActiveEditor()].(*diffPane); ok {
			panes = append(panes, p)
		}
	}
	if len(panes) != 2 || panes[0] == panes[1] || panes[0].model == panes[1].model {
		t.Fatalf("panes %v", panes)
	}
	if got := a.win.Documents.Refs(a.doc("new.go")); got != 2 {
		t.Errorf("uses of the modified document: %d", got)
	}
	a.run("workbench.action.closeActiveEditor")
	a.pump()
	if !a.docOpen("new.go") || a.win.Documents.Refs(a.doc("new.go")) != 1 {
		t.Errorf("after closing one of them: open %v uses %d", a.docOpen("new.go"), a.win.Documents.Refs(a.doc("new.go")))
	}
	a.noErrors()
}

func TestNextAndPreviousChangeKeys(t *testing.T) {
	var orig, mod strings.Builder
	for i := range 60 {
		orig.WriteString("line " + strconv.Itoa(i) + "\n")
		if i == 9 || i == 29 || i == 49 {
			mod.WriteString("changed " + strconv.Itoa(i) + "\n")
			continue
		}
		mod.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	a := newDiffApp(t, orig.String(), mod.String())
	m := a.view(diffeditor.Modified)
	line := func() int { return m.Cursors().Primary().Position().Line + 1 }
	for _, step := range []struct {
		keys string
		want int
	}{
		{"alt+f5", 10}, {"alt+f5", 30}, {"alt+f5", 50}, {"alt+f5", 10}, {"shift+alt+f5", 50}, {"shift+alt+f5", 30}, {"shift+alt+f5", 10},
	} {
		a.press(step.keys)
		if line() != step.want || m.Cursors().Primary().Position().Col != 0 {
			t.Fatalf("%s: the cursor is on line %d, want %d", step.keys, line(), step.want)
		}
		if !a.pane().Model().Focus(diffeditor.Modified).HasFocus() {
			t.Errorf("%s: the modified editor lost the focus", step.keys)
		}
	}
	// the commands do the same
	a.run("workbench.action.compareEditor.nextChange")
	if line() != 30 {
		t.Errorf("the next change command went to line %d", line())
	}
	a.noErrors()
}

func TestTheChangeKeysDoNothingWithoutADiffEditor(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "a\nb\n"}, "a.go")
	if a.win.Keys.Get("textCompareEditorVisible") != false {
		t.Errorf("textCompareEditorVisible is %v", a.win.Keys.Get("textCompareEditorVisible"))
	}
	a.pressUnbound("alt+f5")
	// the commands that need one do nothing
	for _, id := range []string{"workbench.action.compareEditor.nextChange", "workbench.action.compareEditor.previousChange",
		"workbench.action.compareEditor.focusOtherSide", "diffEditor.switchSide"} {
		a.run(id)
	}
	a.noErrors()
}

func TestFocusingTheSidesOfTheDiffEditor(t *testing.T) {
	a := newDiffApp(t, "a\nremoved\nb\nc\n", "a\nb\nc\n")
	m := a.pane().Model()
	focused := func() diffeditor.Side {
		a.t.Helper()
		a.pump()
		side, ok := m.FocusedSide()
		if !ok {
			a.t.Fatal("no side has the focus")
		}
		return side
	}
	if focused() != diffeditor.Modified {
		t.Fatal("the modified side has the focus at first")
	}
	a.run("workbench.action.compareEditor.focusSecondarySide")
	if focused() != diffeditor.Original {
		t.Error("focusSecondarySide: the original side does not have the focus")
	}
	// the focused editor is in a diff editor, and the original one is read-only
	scope := a.win.Scopes.ScopeOf(a.ts.Binding.FocusManager().PrimaryFocus(), a.win.Keys)
	if scope.Get("isInDiffEditor") != true || scope.Get("editorReadonly") != true || scope.Get("diffEditorOriginalWritable") != false || scope.Get("diffEditorModifiedWritable") != true {
		t.Errorf("keys of the original: isInDiffEditor %v editorReadonly %v original writable %v modified writable %v",
			scope.Get("isInDiffEditor"), scope.Get("editorReadonly"), scope.Get("diffEditorOriginalWritable"), scope.Get("diffEditorModifiedWritable"))
	}
	a.run("workbench.action.compareEditor.focusPrimarySide")
	if focused() != diffeditor.Modified {
		t.Error("focusPrimarySide: the modified side does not have the focus")
	}
	scope = a.win.Scopes.ScopeOf(a.ts.Binding.FocusManager().PrimaryFocus(), a.win.Keys)
	if scope.Get("editorReadonly") != false || scope.Get("isInDiffEditor") != true {
		t.Errorf("keys of the modified: editorReadonly %v isInDiffEditor %v", scope.Get("editorReadonly"), scope.Get("isInDiffEditor"))
	}
	a.run("workbench.action.compareEditor.focusOtherSide")
	if focused() != diffeditor.Original {
		t.Error("focusOtherSide from the modified side")
	}
	a.run("workbench.action.compareEditor.focusOtherSide")
	if focused() != diffeditor.Modified {
		t.Error("focusOtherSide from the original side")
	}
	// Switch Side maps the selection: line 4 of the original is line 3 of the modified text
	a.view(diffeditor.Original).Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 3, Col: 1})})
	a.run("workbench.action.compareEditor.focusSecondarySide")
	a.run("diffEditor.switchSide")
	if focused() != diffeditor.Modified {
		t.Error("switchSide stayed on the original side")
	}
	if got := a.view(diffeditor.Modified).Cursors().Primary().Position(); got != (buffer.Position{Line: 2, Col: 1}) {
		t.Errorf("the cursor of the modified side is at %v", got)
	}
	a.noErrors()
}

func TestTheDiffFollowsTheSettings(t *testing.T) {
	a := newDiffApp(t, "a\n  b\nc\n", "a\nb\nc\n")
	if !a.model().NoChanges() {
		t.Fatal("white space counts by default")
	}
	a.setting("diffEditor.ignoreTrimWhitespace", false)
	a.waitFor("the new diff", func() bool { return !a.model().NoChanges() && !a.model().Computing() })
	if err := a.win.Settings.Update(context.Background(), "diffEditor.ignoreTrimWhitespace", true, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the diff to ignore white space again", func() bool { return a.model().NoChanges() && !a.model().Computing() })
	a.noErrors()
}

func TestDevOpenDiffArguments(t *testing.T) {
	a := newSaveApp(t, map[string]string{"old.go": "a\n", "new.go": "b\n"})
	good := a.files["old.go"]
	for _, tc := range []struct {
		name  string
		args  any
		route string
	}{
		{"no arguments", nil, "parseOpenDiff.DevOpenDiffInvalid"},
		{"a string", "a.go", "parseOpenDiff.DevOpenDiffInvalid"},
		{"no modified side", map[string]any{"original": good}, "parseOpenDiff.DevOpenDiffInvalid"},
		{"an empty side", map[string]any{"original": good, "modified": ""}, "parseOpenDiff.DevOpenDiffInvalid"},
		{"a side that is not text", map[string]any{"original": good, "modified": 3.0}, "parseOpenDiff.DevOpenDiffInvalid"},
		{"a relative path without a folder", map[string]any{"original": good, "modified": "new.go"}, "acquireSide.DevOpenDiffInvalid"},
		{"an untitled document that is not open", map[string]any{"original": good, "modified": "untitled:4"}, "acquireSide.DevOpenDiffSideMissing"},
		{"a file that is not there", map[string]any{"original": good, "modified": "/missing/file.go"}, "FileNotFound"},
		{"a bad virtual URI", map[string]any{"original": "emetos-virtual:/nothing", "modified": good}, "VirtualURIUnknown"},
		{"a bad untitled URI", map[string]any{"original": good, "modified": "untitled:x"}, "URIInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, "dev.openDiff", tc.args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if len(a.tabNames()) != 0 {
				t.Errorf("a diff opened: %v", a.tabNames())
			}
			// the document that was opened first is not left in use
			if doc, ok := a.win.Documents.Get(document.FileURI(good)); ok && a.win.Documents.Refs(doc) != 0 {
				t.Errorf("the original document is still in use %d times", a.win.Documents.Refs(doc))
			}
		})
	}
}

func TestADiffOfVirtualAndUntitledDocuments(t *testing.T) {
	a := newSaveApp(t, map[string]string{"new.go": "package x\n"})
	err := a.win.Process.Virtual.Register("/saved/new.go", document.VirtualProvider{
		Label: "new.go (Saved)", Language: "go", Text: func(context.Context) (string, error) { return "package y\n", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	d := &diffApp{a}
	if err := d.runWith("dev.openDiff", map[string]any{"original": "emetos-virtual:/saved/new.go", "modified": a.files["new.go"]}); err != nil {
		t.Fatal(err)
	}
	d.pump()
	d.waitDiff()
	wantNames(t, a.tabNames(), "new.go (Saved) ↔ new.go")
	if !d.view(diffeditor.Original).ReadOnly() || !d.view(diffeditor.Original).Document().ReadOnly() {
		t.Error("the virtual original is not read-only")
	}
	if len(d.model().Changes()) != 1 {
		t.Errorf("changes %+v", d.model().Changes())
	}
	// an untitled document as the modified side
	a.press("ctrl+n")
	if err := d.runWith("dev.openDiff", map[string]any{"original": "file://" + a.files["new.go"], "modified": "untitled:1"}); err != nil {
		t.Fatal(err)
	}
	d.pump()
	d.waitDiff()
	if !slices.Contains(a.tabNames(), "new.go ↔ Untitled-1") {
		t.Errorf("tabs %v", a.tabNames())
	}
	if d.pane().input.Capabilities().Has(editorgroup.CapReadonly) || !d.pane().input.Capabilities().Has(editorgroup.CapUntitled) {
		t.Errorf("capabilities %b", d.pane().input.Capabilities())
	}
	a.noErrors()
}

// diffState is a project of the state tests with a diff editor open in its window.
func diffState(t *testing.T, extra map[string]any) (*stateEnv, *stateRun) {
	t.Helper()
	env := newStateEnv(t, extra)
	run := env.start(env.root)
	run.command("dev.openDiff", map[string]any{"original": "src/a.go", "modified": "main.go"}) // relative to the folder
	run.waitDiff()
	return env, run
}

func (r *stateRun) waitDiff() {
	r.env.t.Helper()
	for range 500 {
		r.ts.FlushPosted()
		r.pump()
		if panes := r.shell().diffPanes(); len(panes) > 0 && panes[0].model.Computed() && !panes[0].model.Computing() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	r.env.t.Fatal("timed out waiting for the diff")
}

func TestADiffEditorComesBackAfterARestart(t *testing.T) {
	env, first := diffState(t, nil)
	if got := first.layout(); !slices.Equal(got, []string{"1!: a.go ↔ main.go*"}) {
		t.Fatalf("groups %v", got)
	}
	first.close()
	saved, ok := env.savedWindow(env.root)
	if !ok || saved.Grid == nil {
		t.Fatal("no state was saved")
	}
	second := env.start(env.root)
	if got := second.layout(); !slices.Equal(got, []string{"1!: a.go ↔ main.go*"}) {
		t.Errorf("groups after the restart %v", got)
	}
	second.waitDiff()
	p := second.shell().diffPanes()[0]
	if p.input.Original().URI().Path != env.path("src/a.go") || p.input.Modified().URI().Path != env.path("main.go") {
		t.Errorf("the restored diff compares %v and %v", p.input.Original().URI(), p.input.Modified().URI())
	}
	if !p.sides[0].view.ReadOnly() || p.sides[1].view.ReadOnly() {
		t.Error("the restored sides have the wrong read-only state")
	}
	if n := len(second.win.Notify.Items()); n != 0 {
		t.Errorf("%d notifications after a restore with nothing missing", n)
	}
	if errs := second.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestADiffWhoseFileIsGoneIsDroppedWithOneNotice(t *testing.T) {
	for _, gone := range [][]string{{"src/a.go"}, {"main.go"}, {"src/a.go", "main.go"}} {
		t.Run(strings.Join(gone, "+"), func(t *testing.T) {
			env, first := diffState(t, nil)
			first.open(env.path("e.txt"), 0)
			first.close()
			for _, f := range gone {
				if err := os.Remove(env.path(f)); err != nil {
					t.Fatal(err)
				}
			}
			second := env.start(env.root)
			if got := second.layout(); !slices.Equal(got, []string{"1!: e.txt*"}) {
				t.Errorf("groups %v", got)
			}
			items := second.win.Notify.Items()
			if len(items) != 1 || items[0].Message != "1 editor could not be restored because its file no longer exists." {
				t.Errorf("notifications %+v", items)
			}
			if n := len(second.win.Documents.All()); n != 1 {
				t.Errorf("%d documents are open, want 1", n)
			}
		})
	}
}

func TestADiffWithAVirtualOriginalComesBackOnlyWhileTheProviderExists(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.command("dev.openDiff", map[string]any{"original": document.VirtualURI(DefaultSettingsPath).String(), "modified": "main.go"})
	first.waitDiff()
	first.close()
	second := env.start(env.root)
	if got := second.layout(); len(got) != 1 || !strings.Contains(got[0], "Default Settings ↔ main.go") {
		t.Errorf("groups after the restart %v", got)
	}
	// a saved diff of generated content that no provider makes any more is dropped without a word
	second.close()
	saved, _ := env.savedWindow(env.root)
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	env.writeWindowState(env.root, strings.ReplaceAll(string(raw), "defaultSettings.json", "gone.json"))
	third := env.start(env.root)
	if got := third.layout(); !slices.Equal(got, []string{"1!: "}) {
		t.Errorf("groups %v", got)
	}
	if n := len(third.win.Notify.Items()); n != 0 {
		t.Errorf("%d notifications", n)
	}
}

func TestTheChangesOfADiffInHotExitComeBack(t *testing.T) {
	env, first := diffState(t, map[string]any{"files.hotExit": "onExit"})
	if err := first.shell().diffPanes()[0].sides[1].view.RunCommand("type", map[string]any{"text": "unsaved "}); err != nil {
		t.Fatal(err)
	}
	first.pump()
	first.close()
	second := env.start(env.root)
	second.waitDiff()
	p := second.shell().diffPanes()[0]
	if !p.input.Dirty() || !strings.HasPrefix(p.input.Modified().Text(), "unsaved ") {
		t.Errorf("the restored diff is dirty %v: %.20q", p.input.Dirty(), p.input.Modified().Text())
	}
	if p.input.Original().Dirty() {
		t.Error("the original document is dirty")
	}
	if got := second.layout(); !slices.Equal(got, []string{"1!: a.go ↔ main.go*"}) {
		t.Errorf("groups %v", got)
	}
}

func TestADiffRelativeToTheFolder(t *testing.T) {
	_, run := diffState(t, nil)
	if got := run.layout(); len(got) != 1 {
		t.Errorf("groups %v", got)
	}
	// a path that is not under the folder works too, as a file URI
	run.command("dev.openDiff", map[string]any{"original": "file://" + run.env.path("docs/c.md"), "modified": run.env.path("e.txt")})
	if got := run.layout(); !slices.Equal(got, []string{"1!: a.go ↔ main.go, c.md ↔ e.txt*"}) {
		t.Errorf("groups %v", got)
	}
}
