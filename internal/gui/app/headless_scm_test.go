package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/annotations"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// What the Git extension puts in the editors reaches a headless view (step 24.23, SPEC §10.18 and §10.30): the quick diff marks and the
// line annotations of blame belong to the window tab, not to a window, so the view of a client draws them in its editor. (The Source
// Control view, the Graph and the Timeline are views of the window's own shell, and the view's workspace has the shell of a view:
// SPEC §10.18 "Not in the view".)

func (r *viewRig) waitView(t *testing.T, what string, cond func(vm *editor.ViewModel) bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		r.pump()
		if e := r.view.ws.shell.activeEditor(); e != nil && cond(e.view) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestTheQuickDiffMarksOfTheWindowTabAreInTheEditorOfAView(t *testing.T) {
	r := newViewRig(t, "one\nTWO\nthree\n", extension.Size{Width: 800, Height: 500})
	r.frame(t)
	e := r.view.ws.shell.activeEditor()
	if e == nil {
		t.Fatal("the view has no editor")
	}
	path := e.doc.URI().Path
	if err := os.WriteFile(path+".orig", []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := r.frame(t)
	r.win.QuickDiff.Add(quickdiff.Provider{Label: "Test", Kind: quickdiff.KindPrimary, RootURI: document.FileURI(filepath.Dir(path)).String(),
		Original: func(_ context.Context, uri string) (string, error) { return uri + ".orig", nil }})
	r.waitView(t, "the marks in the view", func(vm *editor.ViewModel) bool { return vm.QuickDiffCount() == 1 })
	after := r.frame(t)
	if !differs(before, after) {
		t.Error("the picture of the view did not change with the marks")
	}
	// and the window's own editor has them too: one service, two editors
	if n := r.vm.QuickDiffCount(); n != 1 {
		t.Errorf("the window's editor has %d marks", n)
	}
}

func TestTheBlameAnnotationsOfTheWindowTabAreInTheEditorOfAView(t *testing.T) {
	r := newViewRig(t, "one\ntwo\nthree\n", extension.Size{Width: 800, Height: 500})
	r.frame(t)
	uri := r.view.ws.shell.activeEditor().doc.URI().String()
	before := r.frame(t)
	r.win.Annotations.Set("emetos.gitscm", "blame", uri, []annotations.Annotation{{Line: 1, Text: "Ada Example, 3 days ago"}})
	r.waitView(t, "the annotation in the view", func(vm *editor.ViewModel) bool {
		got := vm.LineAnnotations()
		return len(got) == 1 && got[0].Line == 1 && got[0].Text == "Ada Example, 3 days ago"
	})
	if !differs(before, r.frame(t)) {
		t.Error("the picture of the view did not change with the annotation")
	}
	r.win.Annotations.Clear("emetos.gitscm", "blame")
	r.waitView(t, "the annotation to go", func(vm *editor.ViewModel) bool { return len(vm.LineAnnotations()) == 0 })
}
