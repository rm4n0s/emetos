package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/gui/mergeeditor"
)

// The tests of the actions of the merge editor and of Complete Merge in the window (step 9.8).

// resolvedFile is markerFile with the incoming side of both conflicts.
const resolvedFile = "a\nincoming1\nb\ninc2\nc\n"

// waitMergeClosed waits until the window has no merge editor.
func (a *mergeApp) waitMergeClosed() {
	a.t.Helper()
	a.waitFor("the merge editor to close", func() bool { return a.pane() == nil })
}

func TestCompleteMergeOfAMarkerFile(t *testing.T) {
	for _, how := range []string{"the command", "the key", "the button"} {
		t.Run(how, func(t *testing.T) {
			a := newMergeApp(t, map[string]string{"c.go": markerFile})
			p := a.resolve("c.go")
			if err := p.Model().AcceptAll(diff.Input1); err != nil {
				t.Fatal(err)
			}
			a.pump()
			if got := a.doc("c.go").Text(); got != resolvedFile {
				t.Fatalf("the result is %q", got)
			}
			switch how {
			case "the command":
				if err := a.runWith("mergeEditor.acceptMerge", nil); err != nil {
					t.Fatal(err)
				}
			case "the key":
				a.press("ctrl+enter")
			default:
				a.ts.Tap(gluttertest.ByText("Complete Merge"))
			}
			a.waitMergeClosed()
			a.pump()
			// no question: every conflict was handled; the file is written and the document is clean
			a.wantNoDialog()
			if got := a.fileText("c.go"); got != resolvedFile {
				t.Errorf("the file is %q", got)
			}
			doc := a.doc("c.go")
			if doc.Dirty() {
				t.Error("the document is dirty after the merge was completed")
			}
			// the file shows in a text editor in the place of the merge editor
			wantNames(t, a.tabNames(), "c.go")
			if e := a.shell().activeEditor(); e == nil || e.doc != doc {
				t.Errorf("the active editor is %v", e)
			}
			if got := a.win.Documents.Refs(doc); got != 1 {
				t.Errorf("uses of the document: %d", got)
			}
			if a.key("isMergeEditor") != nil {
				t.Errorf("isMergeEditor is %v after the merge editor closed", a.key("isMergeEditor"))
			}
			// the text editor does not offer the merge editor again: the markers are gone
			if n := a.mergeNotes(); n != 1 {
				t.Errorf("%d notifications about conflicts (the one of the first opening)", n)
			}
			a.noErrors()
		})
	}
}

func TestCompleteMergeWithUnhandledConflictsAsksFirst(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile})
	p := a.resolve("c.go")
	if err := p.Model().Accept(0, diff.AcceptInput2); err != nil { // one conflict is left
		t.Fatal(err)
	}
	a.pump()
	if err := a.runWith("mergeEditor.acceptMerge", nil); err != nil {
		t.Fatal(err)
	}
	a.pump()
	spec := a.dialogSpec()
	if spec.Message != "Do you want to complete the merge of c.go?" || spec.Detail != "The file contains unhandled conflicts." ||
		!slices.Equal(spec.Buttons, []string{"Complete with Conflicts", "Cancel"}) {
		t.Fatalf("the question is %+v", spec)
	}
	// a second press while it asks does not ask again
	if err := a.runWith("mergeEditor.acceptMerge", nil); err != nil {
		t.Fatal(err)
	}
	// Cancel keeps everything: the merge editor, the text of the result, the file on disk
	a.tapDialog("Cancel")
	a.wantNoDialog()
	if a.pane() == nil {
		t.Fatal("Cancel closed the merge editor")
	}
	if got := a.fileText("c.go"); got != markerFile {
		t.Errorf("the file changed: %q", got)
	}
	if !a.doc("c.go").Dirty() || p.Model().Remaining() != "1 Conflict Remaining" {
		t.Errorf("dirty %v, %q", a.doc("c.go").Dirty(), p.Model().Remaining())
	}
	// Complete with Conflicts saves the result as it is (the lines of the base where a conflict is not handled)
	if err := a.runWith("mergeEditor.acceptMerge", nil); err != nil {
		t.Fatal(err)
	}
	a.tapDialog("Complete with Conflicts")
	a.waitMergeClosed()
	if got, want := a.fileText("c.go"), "a\ncurrent1\nb\nc\n"; got != want {
		t.Errorf("the file is %q, want %q", got, want)
	}
	wantNames(t, a.tabNames(), "c.go")
	a.noErrors()
}

func TestCompleteMergeThatCannotSaveKeepsTheMergeEditor(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile})
	p := a.resolve("c.go")
	if err := p.Model().AcceptAll(diff.Input1); err != nil {
		t.Fatal(err)
	}
	// the file was changed by another program since it was read: the save fails and says so
	a.changedByAnotherProgram("c.go")
	if err := a.runWith("mergeEditor.acceptMerge", nil); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the failure", func() bool { return len(a.errorNotes()) > 0 })
	if a.pane() == nil {
		t.Error("the merge editor closed although the save failed")
	}
	wantNames(t, a.tabNames(), "c.go", "Merging: c.go")
	if _, closed := a.shell().merge.completed[p.input]; closed {
		t.Error("the merge counts as completed")
	}
}

func TestMergeCommandsActOnTheMergeEditor(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile, "plain.go": "no markers\n"})
	// outside a merge editor they do nothing
	a.show("plain.go", false)
	for _, id := range []string{"merge.acceptAllInput1", "mergeEditor.resetResultToBaseAndAutoMerge", "mergeEditor.acceptAllCombination",
		"merge.toggleActiveConflictInput1", "mergeEditor.acceptMerge"} {
		if err := a.runWith(id, nil); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	a.pump()
	if a.doc("plain.go").Dirty() || len(a.notes()) != 0 || a.doc("plain.go").Text() != "no markers\n" {
		t.Errorf("a command changed something: %v", a.notes())
	}
	p := a.resolve("c.go")
	m := p.Model()
	states := func() []diff.StateKind {
		var out []diff.StateKind
		for _, c := range m.Session().Model().Conflicts() {
			out = append(out, c.State.Kind)
		}
		return out
	}
	run := func(id string) {
		t.Helper()
		if err := a.runWith(id, nil); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		a.pump()
	}
	run("merge.acceptAllInput1")
	if got := a.doc("c.go").Text(); got != resolvedFile || !slices.Equal(states(), []diff.StateKind{diff.StateInput1, diff.StateInput1}) {
		t.Errorf("accept all incoming: %q %v", got, states())
	}
	run("mergeEditor.resetResultToBaseAndAutoMerge")
	if got := a.doc("c.go").Text(); got != mergedMarkerFile || m.Remaining() != "2 Conflicts Remaining" {
		t.Errorf("reset: %q, %q", got, m.Remaining())
	}
	run("merge.acceptAllInput2")
	if got := a.doc("c.go").Text(); got != "a\ncurrent1\nb\ncur2\nc\n" {
		t.Errorf("accept all current: %q", got)
	}
	run("mergeEditor.resetResultToBaseAndAutoMerge")
	run("mergeEditor.acceptAllCombination")
	if got := a.doc("c.go").Text(); got != "a\nincoming1\ncurrent1\nb\ninc2\ncur2\nc\n" {
		t.Errorf("accept all combination: %q", got)
	}
	// toggling the active conflict: none is active until the cursor is in one
	run("mergeEditor.resetResultToBaseAndAutoMerge")
	run("merge.toggleActiveConflictInput1")
	if a.doc("c.go").Text() != mergedMarkerFile {
		t.Errorf("a conflict was toggled with none active: %q", a.doc("c.go").Text())
	}
	if n := a.errorNotes(); len(n) != 1 || n[0].Message != "There is currently no conflict focused that can be toggled." {
		t.Errorf("notifications %+v", n)
	}
	run("merge.goToNextUnhandledConflict")
	run("merge.toggleActiveConflictInput2")
	if got := a.doc("c.go").Text(); got != "a\ncurrent1\nb\nc\n" {
		t.Errorf("toggle from the right: %q", got)
	}
	run("merge.toggleActiveConflictInput2")
	if got := a.doc("c.go").Text(); got != mergedMarkerFile {
		t.Errorf("a second toggle: %q", got)
	}
	run("merge.toggleActiveConflictInput1")
	if got := a.doc("c.go").Text(); got != "a\nincoming1\nb\nc\n" {
		t.Errorf("toggle from the left: %q", got)
	}
	a.noErrors()
}

func TestMergeActionsAreOnScreenAndWork(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile})
	p := a.resolve("c.go")
	a.pump()
	for _, text := range []string{"Accept Incoming", "Accept Current", "No Changes Accepted", "Complete Merge"} {
		if !a.ts.Exists(gluttertest.ByText(text)) {
			t.Errorf("%q is not on screen", text)
		}
	}
	// the links are widgets of the rows over the conflicts; the tests of gui/mergeeditor press them (the fonts of this window
	// give a text no width), here the action does what the link says
	m := p.Model()
	items := m.Actions(mergeeditor.Current, 0)
	if len(items) != 2 || items[0].Text != "Accept Current" {
		t.Fatalf("the actions of the current side: %+v", items)
	}
	items[0].Run()
	a.pump()
	if got := a.doc("c.go").Text(); got != "a\ncurrent1\nb\nc\n" {
		t.Errorf("the result after the link: %q", got)
	}
	if p.Model().Remaining() != "1 Conflict Remaining" {
		t.Errorf("remaining %q", p.Model().Remaining())
	}
	// Undo takes it back, conflict and text
	if _, err := a.doc("c.go").Undo(); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if got := a.doc("c.go").Text(); got != mergedMarkerFile || p.Model().Remaining() != "2 Conflicts Remaining" {
		t.Errorf("after the undo: %q, %q", got, p.Model().Remaining())
	}
	a.noErrors()
}

// ---- --merge ----

func TestCompleteMergeOfFilesWritesTheResultPath(t *testing.T) {
	a := newMergeApp(t, map[string]string{"in1.txt": "a\nONE\nc\n", "in2.txt": "a\nTWO\nc\n", "base.txt": "a\nb\nc\n", "out.txt": "stale\n"})
	if err := a.runWith("_open.mergeEditor", map[string]any{"input1": a.files["in1.txt"], "input2": a.files["in2.txt"], "base": a.files["base.txt"], "output": a.files["out.txt"]}); err != nil {
		t.Fatal(err)
	}
	p := a.waitMerge()
	if err := p.Model().Accept(0, diff.AcceptBoth); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if err := a.runWith("mergeEditor.acceptMerge", nil); err != nil {
		t.Fatal(err)
	}
	a.waitMergeClosed()
	if got := a.fileText("out.txt"); got != "a\nTWO\nONE\nc\n" {
		t.Errorf("the result file is %q", got)
	}
	// the files of --merge are not opened in a text editor, and nothing is left of the merge
	if len(a.tabNames()) != 0 || len(a.win.Documents.All()) != 0 {
		t.Errorf("tabs %v, documents %d", a.tabNames(), len(a.win.Documents.All()))
	}
	for _, name := range []string{"in1.txt", "in2.txt", "base.txt"} {
		if got := a.fileText(name); got == "" {
			t.Errorf("%s was emptied", name)
		}
	}
	a.noErrors()
}

func TestCompleteMergeOfFilesCreatesTheResultFile(t *testing.T) {
	a := newMergeApp(t, map[string]string{"in1.txt": "a\nb\nONE\n", "in2.txt": "a\nb\nc\n", "base.txt": "a\nb\nc\n"})
	out := filepath.Join(filepath.Dir(a.files["in1.txt"]), "new.txt")
	if err := a.runWith("_open.mergeEditor", map[string]any{"input1": a.files["in1.txt"], "input2": a.files["in2.txt"], "base": a.files["base.txt"], "output": out}); err != nil {
		t.Fatal(err)
	}
	a.waitMerge()
	if err := a.runWith("mergeEditor.acceptMerge", nil); err != nil { // no conflict: no question
		t.Fatal(err)
	}
	a.waitMergeClosed()
	if data, err := os.ReadFile(out); err != nil || string(data) != "a\nb\nONE\n" {
		t.Errorf("the file is %q, %v", data, err)
	}
	a.wantNoDialog()
}

// ---- the exit codes of --merge --wait ----

func TestLaunchMergeWaitEndsWithZeroWhenTheMergeIsCompleted(t *testing.T) {
	for _, tc := range []struct {
		name     string
		complete func(a *launchApp, p *mergePane)
	}{
		{"all conflicts handled", func(a *launchApp, p *mergePane) {
			if err := p.Model().AcceptAll(diff.Input1); err != nil {
				a.t.Fatal(err)
			}
			if err := a.run("mergeEditor.acceptMerge", nil); err != nil {
				a.t.Fatal(err)
			}
		}},
		{"with the conflicts that are left", func(a *launchApp, p *mergePane) {
			if err := a.run("mergeEditor.acceptMerge", nil); err != nil {
				a.t.Fatal(err)
			}
			a.pump()
			tapDialogButton(a, "Complete with Conflicts")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newLaunchApp(t)
			paths := a.mergeTargets("a\nONE\nc\n", "a\nTWO\nc\n", "a\nb\nc\n", "stale\n")
			r := a.launch(ipc.OpenArgs{Wait: true, Merge: paths})
			a.waitFor("the merge editor", func() bool { return a.mergePane() != nil })
			wantOutcomes(t, r)
			tc.complete(a, a.mergePane())
			a.waitFor("the end of the wait", func() bool { return len(r.outcomes) == 1 })
			wantOutcomes(t, r, LaunchOutcome{})
			want := "a\nONE\nc\n"
			if tc.name != "all conflicts handled" {
				want = "a\nb\nc\n"
			}
			if got, err := os.ReadFile(paths[3]); err != nil || string(got) != want {
				t.Errorf("the result file is %q, %v; want %q", got, err, want)
			}
			if len(a.shell().merge.completed) != 0 {
				t.Errorf("the completed merges are not forgotten: %v", a.shell().merge.completed)
			}
		})
	}
}

func TestLaunchMergeWaitEndsWithOneWhenTheWindowCloses(t *testing.T) {
	a := newLaunchApp(t)
	paths := a.mergeTargets("a\nONE\nc\n", "a\nTWO\nc\n", "a\nb\nc\n", "stale\n")
	r := a.launch(ipc.OpenArgs{Wait: true, Merge: paths})
	a.waitFor("the merge editor", func() bool { return a.mergePane() != nil })
	a.shell().disposeLaunch()
	wantOutcomes(t, r, LaunchOutcome{ExitCode: 1, Message: MergeNotCompletedNotice})
	if got, err := os.ReadFile(paths[3]); err != nil || string(got) != "stale\n" {
		t.Errorf("the result file is %q, %v", got, err)
	}
}

func TestLaunchMergeWaitEndsWithOneWhenOnlyTheResultWasSaved(t *testing.T) {
	// saving is not completing: the tab is closed with Save from the question about the changes
	a := newLaunchApp(t)
	paths := a.mergeTargets("a\nONE\nc\n", "a\nTWO\nc\n", "a\nb\nc\n", "stale\n")
	r := a.launch(ipc.OpenArgs{Wait: true, Merge: paths})
	a.waitFor("the merge editor", func() bool { return a.mergePane() != nil })
	s := a.shell()
	s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{s.activeGroup().Model.ActiveEditor()})
	a.pump()
	tapDialogButton(a, "Save")
	a.waitFor("the end of the wait", func() bool { return len(r.outcomes) == 1 })
	wantOutcomes(t, r, LaunchOutcome{ExitCode: 1, Message: MergeNotCompletedNotice})
	if got, err := os.ReadFile(paths[3]); err != nil || string(got) != "a\nb\nc\n" {
		t.Errorf("the result file is %q, %v", got, err)
	}
}

func TestLaunchMergeWaitStaysWhileTheResultCannotBeWritten(t *testing.T) {
	a := newLaunchApp(t)
	paths := a.mergeTargets("a\nONE\nc\n", "a\nTWO\nc\n", "a\nb\nc\n", "<none>")
	paths[3] = a.path("nodir/result.txt") // its folder is not there: the write fails
	r := a.launch(ipc.OpenArgs{Wait: true, Merge: paths})
	a.waitFor("the merge editor", func() bool { return a.mergePane() != nil })
	p := a.mergePane()
	if err := p.Model().AcceptAll(diff.Input1); err != nil {
		t.Fatal(err)
	}
	if err := a.run("mergeEditor.acceptMerge", nil); err != nil {
		t.Fatal(err)
	}
	a.waitFor("the failure", func() bool {
		for _, it := range a.win.Notify.Items() {
			if it.Severity == notify.Error {
				return true
			}
		}
		return false
	})
	if a.mergePane() == nil {
		t.Error("the merge editor closed although the result was not written")
	}
	wantOutcomes(t, r)
}

func TestLauncherMergeWaitReplies(t *testing.T) {
	for _, completed := range []bool{true, false} {
		name := "a merge that was not completed"
		if completed {
			name = "a merge that was completed"
		}
		t.Run(name, func(t *testing.T) {
			a, l := newLauncherApp(t, true)
			paths := make([]string, 4)
			for i, text := range []string{"a\nONE\nc\n", "a\nTWO\nc\n", "a\nb\nc\n", "stale\n"} {
				paths[i] = a.path([]string{"in1.txt", "in2.txt", "base.txt", "result.txt"}[i])
				if err := os.WriteFile(paths[i], []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			out := a.handle(l, context.Background(), ipc.OpenArgs{Wait: true, Merge: paths})
			a.waitFor("the merge editor", func() bool { return a.shell().activeMergePane() != nil })
			select {
			case r := <-out:
				t.Fatalf("a reply before the merge was dealt with: %+v", r)
			default:
			}
			s := a.shell()
			p := s.activeMergePane()
			if completed {
				if err := p.model.AcceptAll(diff.Input2); err != nil {
					t.Fatal(err)
				}
				s.completeMerge(p)
			} else {
				s.closeEditors(s.activeGroup(), []editorgroup.EditorInput{p.input})
				a.pump()
				tapDialogButton(&launchApp{a.explorerApp}, "Don't Save")
			}
			reply := a.await(out)
			if completed && (reply.Type != ipc.TypeDone || reply.ExitCode != 0 || reply.Message != "") {
				t.Errorf("reply %+v", reply)
			}
			if !completed && (reply.Type != ipc.TypeError || reply.ExitCode != 1 || reply.Message != MergeNotCompletedNotice) {
				t.Errorf("reply %+v", reply)
			}
			if completed {
				if got, err := os.ReadFile(paths[3]); err != nil || string(got) != "a\nTWO\nc\n" {
					t.Errorf("the result file is %q, %v", got, err)
				}
			}
		})
	}
}

// ---- dev commands ----

func TestDevOpenMergeEditor(t *testing.T) {
	t.Run("a file with markers, by its path", func(t *testing.T) {
		a := newMergeApp(t, map[string]string{"c.go": markerFile})
		if err := a.runWith("dev.openMergeEditor", map[string]any{"path": a.files["c.go"]}); err != nil {
			t.Fatal(err)
		}
		// the merge editor is there for the next step of a script: no waiting
		a.pump()
		p := a.pane()
		if p == nil {
			t.Fatal("no merge editor after the command")
		}
		if got := p.Model().Session().Model().ConflictCount(); got != 2 {
			t.Errorf("%d conflicts", got)
		}
		wantNames(t, a.tabNames(), "Merging: c.go")
		if got := a.doc("c.go").Text(); got != mergedMarkerFile {
			t.Errorf("the result is %q", got)
		}
		// the file is the result and is not written
		if got := a.fileText("c.go"); got != markerFile {
			t.Errorf("the file changed: %q", got)
		}
		// the same file again is the same tab
		if err := a.runWith("dev.openMergeEditor", a.files["c.go"]); err != nil {
			t.Fatal(err)
		}
		a.pump()
		wantNames(t, a.tabNames(), "Merging: c.go")
		if got := a.win.Documents.Refs(a.doc("c.go")); got != 1 {
			t.Errorf("uses of the document: %d", got)
		}
		a.noErrors()
	})
	t.Run("a relative path is relative to the folder Emetos runs in", func(t *testing.T) {
		a := newMergeApp(t, map[string]string{"c.go": markerFile})
		t.Chdir(filepath.Dir(a.files["c.go"]))
		if err := a.runWith("dev.openMergeEditor", map[string]any{"path": "c.go"}); err != nil {
			t.Fatal(err)
		}
		a.pump()
		if a.pane() == nil {
			t.Fatal("no merge editor")
		}
	})
	t.Run("a relative path is relative to the folder of the window when one is open", func(t *testing.T) {
		a := newLaunchApp(t)
		if err := os.WriteFile(a.path("sub/m.txt"), []byte(markerFile), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := a.run("dev.openMergeEditor", map[string]any{"path": "sub/m.txt"}); err != nil {
			t.Fatal(err)
		}
		a.pump()
		if a.mergePane() == nil {
			t.Fatal("no merge editor")
		}
	})
	t.Run("four files", func(t *testing.T) {
		a := newMergeApp(t, map[string]string{"in1.txt": "a\nONE\nc\n", "in2.txt": "a\nTWO\nc\n", "base.txt": "a\nb\nc\n", "out.txt": "stale\n"})
		if err := a.runWith("dev.openMergeEditor", map[string]any{"input1": a.files["in1.txt"], "input2": a.files["in2.txt"], "base": a.files["base.txt"], "result": a.files["out.txt"]}); err != nil {
			t.Fatal(err)
		}
		p := a.waitMerge()
		if got := p.input.Source().Kind; got != editorgroup.MergeFiles {
			t.Errorf("kind %v", got)
		}
		wantNames(t, a.tabNames(), "Merging: out.txt")
	})
}

func TestDevOpenMergeEditorErrors(t *testing.T) {
	a := newMergeApp(t, map[string]string{"plain.go": "no markers\n", "c.go": markerFile})
	for _, tc := range []struct {
		name  string
		args  any
		route string
	}{
		{"no arguments", nil, "parseOpenMerge.DevOpenMergeInvalid"},
		{"an empty path", map[string]any{"path": ""}, "parseOpenMerge.DevOpenMergeInvalid"},
		{"a number", 3.0, "parseOpenMerge.DevOpenMergeInvalid"},
		{"a path and files", map[string]any{"path": "x", "input1": "a", "input2": "b", "base": "c", "result": "d"}, "parseOpenMerge.DevOpenMergeInvalid"},
		{"three files", map[string]any{"input1": "a", "input2": "b", "base": "c"}, "parseOpenMerge.DevOpenMergeInvalid"},
		{"a file without markers", map[string]any{"path": a.files["plain.go"]}, "shellState.devOpenMergeEditor->markersMerge->MergeInputsFromMarkerText.MergeMarkersNotFound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := a.runWith("dev.openMergeEditor", tc.args)
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
	if n := a.win.Documents.Refs(a.doc("plain.go")); n != 0 {
		t.Errorf("the document of the file without markers is still used %d times", n)
	}
	// a file that is not there fails as the files of dev.openFile do
	if err := a.runWith("dev.openMergeEditor", map[string]any{"path": filepath.Join(filepath.Dir(a.files["c.go"]), "missing.go")}); err == nil {
		t.Error("a missing file was opened")
	}
	// a working folder that is gone makes a relative path impossible
	gone := t.TempDir()
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string]any{"path": "c.go", "files": map[string]any{"input1": "a", "input2": "b", "base": "c", "result": "d"}} {
		err := a.runWith("dev.openMergeEditor", args)
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("shellState.devMergePath.DevOpenMergePathFailed") {
			t.Errorf("%s: unexpected failure %v", name, err)
		}
	}
}

func TestDevAcceptMerge(t *testing.T) {
	for _, tc := range []struct {
		side  string
		text  string
		state diff.StateKind
	}{
		{"incoming", "a\nincoming1\nb\nc\n", diff.StateInput1},
		{"current", "a\ncurrent1\nb\nc\n", diff.StateInput2},
		{"both", "a\ncurrent1\nincoming1\nb\nc\n", diff.StateBoth},
		{"base", "a\nb\nc\n", diff.StateBase},
	} {
		t.Run(tc.side, func(t *testing.T) {
			a := newMergeApp(t, map[string]string{"c.go": markerFile})
			p := a.resolve("c.go")
			if err := a.runWith("dev.acceptMerge", map[string]any{"conflict": 0.0, "side": tc.side}); err != nil {
				t.Fatal(err)
			}
			a.pump()
			c := p.Model().Session().Model().Conflicts()[0]
			if got := a.doc("c.go").Text(); got != tc.text || c.State.Kind != tc.state || !c.Handled {
				t.Errorf("the result is %q, the conflict %+v", got, c)
			}
			if p.Model().Remaining() != "1 Conflict Remaining" {
				t.Errorf("remaining %q", p.Model().Remaining())
			}
			// the second conflict: the index counts the conflicts, from 0
			if err := a.runWith("dev.acceptMerge", map[string]any{"conflict": 1.0, "side": "incoming"}); err != nil {
				t.Fatal(err)
			}
			if p.Model().Remaining() != "0 Conflicts Remaining" {
				t.Errorf("remaining %q", p.Model().Remaining())
			}
		})
	}
}

func TestDevAcceptMergeErrors(t *testing.T) {
	a := newMergeApp(t, map[string]string{"c.go": markerFile})
	good := map[string]any{"conflict": 0.0, "side": "incoming"}
	// without a merge editor
	err := a.runWith("dev.acceptMerge", good)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("shellState.devAcceptMerge.DevAcceptMergeNoEditor") {
		t.Errorf("without a merge editor: %v", err)
	}
	a.resolve("c.go")
	for _, tc := range []struct {
		name  string
		args  any
		route string
	}{
		{"no arguments", nil, "parseAcceptMerge.DevAcceptMergeInvalid"},
		{"a string", "incoming", "parseAcceptMerge.DevAcceptMergeInvalid"},
		{"no conflict", map[string]any{"side": "incoming"}, "parseAcceptMerge.DevAcceptMergeConflictInvalid"},
		{"a conflict that is text", map[string]any{"conflict": "0", "side": "incoming"}, "parseAcceptMerge.DevAcceptMergeConflictInvalid"},
		{"a negative conflict", map[string]any{"conflict": -1.0, "side": "incoming"}, "parseAcceptMerge.DevAcceptMergeConflictInvalid"},
		{"a conflict that is not whole", map[string]any{"conflict": 0.5, "side": "incoming"}, "parseAcceptMerge.DevAcceptMergeConflictInvalid"},
		{"no side", map[string]any{"conflict": 0.0}, "parseAcceptMerge.DevAcceptMergeSideInvalid"},
		{"a side that is none", map[string]any{"conflict": 0.0, "side": "left"}, "parseAcceptMerge.DevAcceptMergeSideInvalid"},
		{"a side that is a number", map[string]any{"conflict": 0.0, "side": 1.0}, "parseAcceptMerge.DevAcceptMergeSideInvalid"},
		{"a conflict that does not exist", map[string]any{"conflict": 5.0, "side": "incoming"}, "MergeConflictIndexInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := a.runWith("dev.acceptMerge", tc.args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	if got := a.doc("c.go").Text(); got != mergedMarkerFile {
		t.Errorf("a failed command changed the result: %q", got)
	}
}
