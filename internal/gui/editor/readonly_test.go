package editor

import (
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/find"
	"github.com/rm4n0s/emetos/internal/core/snippet"
)

// readOnlyView is a view model with the ReadOnly option over a document that is not read-only, and another over the
// same document that is not.
func readOnlyView(t *testing.T) (ro, rw *ViewModel) {
	t.Helper()
	doc := newTestDoc("abc\ndef")
	opts := testOptions()
	opts.ReadOnly = true
	ro, rw = NewViewModel(doc, opts, nil), NewViewModel(doc, testOptions(), nil)
	ro.Cursors().SetSelections([]buffer.Selection{cur(0, 1)})
	return ro, rw
}

func TestReadOnlyOptionRefusesEveryEdit(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(vm *ViewModel) error
	}{
		{"type", func(vm *ViewModel) error { return vm.RunCommand("type", "x") }},
		{"delete left", func(vm *ViewModel) error { return vm.RunCommand("deleteLeft", nil) }},
		{"line break", func(vm *ViewModel) error { return vm.RunCommand("lineBreakInsert", nil) }},
		{"paste text", func(vm *ViewModel) error { return vm.PasteText("zz") }},
		{"undo", func(vm *ViewModel) error { return vm.RunCommand("undo", nil) }},
		{"redo", func(vm *ViewModel) error { return vm.RunCommand("redo", nil) }},
		{"snippet", func(vm *ViewModel) error { return vm.InsertSnippet("a$0", snippet.DefaultInsertOptions()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ro, rw := readOnlyView(t)
			// something to undo, made through the editor that may edit
			if err := rw.RunCommand("type", "q"); err != nil {
				t.Fatal(err)
			}
			text := ro.Document().Text()
			err := tc.run(ro)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("EditorReadOnly") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if got := ro.Document().Text(); got != text {
				t.Errorf("text %q, want %q", got, text)
			}
		})
	}
}

func TestReadOnlyOptionStillMovesAndReads(t *testing.T) {
	ro, rw := readOnlyView(t)
	if !ro.ReadOnly() || ro.Document().ReadOnly() || rw.ReadOnly() {
		t.Fatalf("read-only: %v, document %v, other editor %v", ro.ReadOnly(), ro.Document().ReadOnly(), rw.ReadOnly())
	}
	if err := ro.RunCommand("cursorRight", nil); err != nil {
		t.Fatal(err)
	}
	if got := ro.Cursors().Primary().Position(); got != positionOf(0, 2) {
		t.Errorf("cursor %v", got)
	}
	if err := ro.RunCommand("editor.action.selectAll", nil); err != nil {
		t.Fatal(err)
	}
	// the other editor of the document edits, and the read-only one follows
	if err := rw.RunCommand("type", "!"); err != nil {
		t.Fatal(err)
	}
	if got := ro.Document().Text(); got != "!abc\ndef" {
		t.Errorf("text %q", got)
	}
}

func TestReadOnlyOptionRefusesReplace(t *testing.T) {
	ro, _ := readOnlyView(t)
	ro.startFind(findStart{seed: "none"}, find.NewState{SearchString: find.Some("a"), ReplaceString: find.Some("z")})
	for _, id := range []string{"editor.action.replaceOne", "editor.action.replaceAll"} {
		err := ro.RunCommand(id, nil)
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("EditorReadOnly") {
			t.Errorf("%s: %v", id, err)
		}
	}
	if got := ro.Document().Text(); got != "abc\ndef" {
		t.Errorf("text %q", got)
	}
}
