package app

import (
	"slices"
	"testing"

	apperrors "github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/references"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// The References extension in a workspace (step 17e.3, SPEC §10.28): its entries of the context menu of the editor.

// contextMenuOf lists the commands of the context menu of the editor, with "-" for the line between two groups, when the keys
// hold.
func contextMenuOf(w *Workspace, keys ...string) []string {
	ctx := contextkey.NewRoot()
	for _, k := range keys {
		ctx.Set(k, true)
	}
	var out []string
	for _, e := range w.Menus.Items(command.EditorContext, ctx) {
		switch {
		case e.Separator:
			out = append(out, "-")
		case e.Command != "":
			out = append(out, e.Command)
		default:
			out = append(out, e.Title)
		}
	}
	return out
}

func TestTheContextMenuHasFindAllReferencesOnlyWithTheProvider(t *testing.T) {
	p := extensionProcess(t, references.New)
	w := newExtensionWorkspace(t, p, "", "go.mod")
	tests := []struct {
		name string
		keys []string
		want []string
	}{
		{"no provider", nil, []string{
			"editor.action.clipboardCutAction", "editor.action.clipboardCopyAction", "editor.action.clipboardPasteAction", "-", "editor.action.selectAll",
		}},
		{"references only", []string{"editorHasReferenceProvider"}, []string{
			"editor.action.goToReferences", "Peek", "-", references.FindReferences,
			"-", "editor.action.clipboardCutAction", "editor.action.clipboardCopyAction", "editor.action.clipboardPasteAction", "-", "editor.action.selectAll",
		}},
		{"implementations only", []string{"editorHasImplementationProvider"}, []string{
			"editor.action.goToImplementation", "Peek", "-", references.FindImplementations,
			"-", "editor.action.clipboardCutAction", "editor.action.clipboardCopyAction", "editor.action.clipboardPasteAction", "-", "editor.action.selectAll",
		}},
		{"both: the group of the extension comes after the group of the editor, in VS Code's order", []string{"editorHasReferenceProvider", "editorHasImplementationProvider"}, []string{
			"editor.action.goToImplementation", "editor.action.goToReferences", "Peek", "-", references.FindReferences, references.FindImplementations,
			"-", "editor.action.clipboardCutAction", "editor.action.clipboardCopyAction", "editor.action.clipboardPasteAction", "-", "editor.action.selectAll",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextMenuOf(w, tc.keys...); !slices.Equal(got, tc.want) {
				t.Errorf("menu = %q\nwant   %q", got, tc.want)
			}
		})
	}
}

func TestFindAllReferencesIsInTheCommandPaletteOnlyWithAProvider(t *testing.T) {
	p := extensionProcess(t, references.New)
	w := newExtensionWorkspace(t, p, "", "go.mod")
	inPalette := func(keys ...string) []string {
		ctx := contextkey.NewRoot()
		for _, k := range keys {
			ctx.Set(k, true)
		}
		var out []string
		for _, e := range w.Menus.Items(command.CommandPalette, ctx) {
			if len(e.Command) > len("references-view.") && e.Command[:len("references-view.")] == "references-view." {
				out = append(out, e.Command)
			}
		}
		return out
	}
	if got := inPalette(); slices.Contains(got, references.FindReferences) {
		t.Errorf("Find All References is in the palette without a provider: %v", got)
	}
	if got := inPalette("editorHasReferenceProvider"); !slices.Contains(got, references.FindReferences) {
		t.Errorf("Find All References is not in the palette with a provider: %v", got)
	}
	for _, id := range []string{references.RemoveReferenceItem, references.Copy, references.CopyAll, references.CopyPath, references.Refind, references.Next, references.Prev} {
		if got := inPalette("editorHasReferenceProvider", "emetos.references.hasResult", "emetos.references.isActive"); slices.Contains(got, id) {
			t.Errorf("%s is in the palette: %v", id, got)
		}
	}
}

func TestTheClipboardOfAnExtensionNeedsAWindowOnScreen(t *testing.T) {
	p := extensionProcess(t, references.New)
	w := newExtensionWorkspace(t, p, "", "go.mod")
	err := w.extensionClipboard("text")
	appErr, ok := apperrors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("Workspace.extensionClipboard.ExtClipboardUnavailable") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestTheActiveDocumentNeedsAnEditorOnScreen(t *testing.T) {
	p := extensionProcess(t, references.New)
	w := newExtensionWorkspace(t, p, "", "go.mod")
	if _, ok := (extensionDocuments{w: w}).Active(); ok {
		t.Error("a workspace that is not on screen has an active document")
	}
}

func TestTheActiveDocumentIsTheEditorInFront(t *testing.T) {
	a := newKeyApp(t, "alpha beta\ngamma delta\n")
	a.vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 1, Col: 2}, Active: buffer.Position{Line: 1, Col: 5}}})
	a.vm.SelectionsChanged()
	a.pump()
	got, ok := (extensionDocuments{w: a.win}).Active()
	if !ok {
		t.Fatal("no active document")
	}
	if got.URI != a.vm.Document().URI().String() || got.Version != a.vm.Document().Version() || got.LanguageID != a.vm.Document().LanguageID() {
		t.Errorf("active = %+v", got)
	}
	want := extension.Selection{Anchor: extension.Position{Line: 1, Col: 2}, Active: extension.Position{Line: 1, Col: 5}}
	if len(got.Selections) != 1 || got.Selections[0] != want {
		t.Errorf("selections = %+v, want %+v", got.Selections, want)
	}
}
