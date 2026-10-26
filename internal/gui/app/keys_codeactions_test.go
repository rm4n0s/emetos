package app

import (
	"context"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// keysCode answers the requests of the code actions, the formatting and the rename of the keys tests: every kind of action is
// offered (one that renames the first word), and a rename does the same.
type keysCode struct{}

func (keysCode) CodeActionKinds() []string {
	return []string{"quickfix", "refactor", "refactor.extract", "source", "source.organizeImports", "source.fixAll"}
}

func renameFirstWord(doc langfeatures.Document) *langfeatures.WorkspaceEdit {
	return &langfeatures.WorkspaceEdit{Changes: []langfeatures.Change{{
		URI: doc.URI, Edits: []langfeatures.TextEdit{{Range: buffer.Range{End: buffer.Position{Col: 5}}, Text: "ALPHA"}},
	}}}
}

func (keysCode) ProvideCodeActions(_ context.Context, doc langfeatures.Document, _ langfeatures.CodeActionRequest) ([]langfeatures.CodeAction, error) {
	return []langfeatures.CodeAction{
		{Title: "Fix", Kind: "quickfix", Edit: renameFirstWord(doc)},
		{Title: "Extract", Kind: "refactor.extract", Edit: renameFirstWord(doc)},
		{Title: "Organize", Kind: "source.organizeImports", Edit: renameFirstWord(doc)},
	}, nil
}

func (keysCode) ProvideFormatting(context.Context, langfeatures.Document, langfeatures.FormattingOptions) ([]langfeatures.TextEdit, error) {
	return nil, nil
}

func (keysCode) ProvideRangeFormatting(context.Context, langfeatures.Document, langfeatures.Range, langfeatures.FormattingOptions) ([]langfeatures.TextEdit, error) {
	return nil, nil
}

func (keysCode) PrepareRename(context.Context, langfeatures.Document, langfeatures.Position) (*langfeatures.PrepareRename, error) {
	return nil, nil
}

func (keysCode) ProvideRename(_ context.Context, doc langfeatures.Document, _ langfeatures.Position, _ string) (*langfeatures.WorkspaceEdit, error) {
	return renameFirstWord(doc), nil
}

// codeActionsState is the state of an editor whose language has the providers of keysCode, with a selection (for Format Selection),
// and, by what, with the list of actions open ("list"), the rename box open ("rename") or the preview of the rename open
// ("preview").
func codeActionsState(a *keyApp, what string) func() {
	return func() {
		a.leaveFind()
		a.endSnippet()
		a.focusEditor()
		host := a.win.suggestHost()
		reg := langfeatures.NewRegistry()
		p := keysCode{}
		reg.RegisterCodeActions("keys", langfeatures.Selector{}, p)
		reg.RegisterFormatting("keys", langfeatures.Selector{}, p)
		reg.RegisterRename("keys", langfeatures.Selector{}, p)
		host.Features = reg
		a.vm.SetSuggest(host)
		a.vm.CodeActionHide()
		a.vm.RenameCancel()
		a.vm.PreviewDiscard()
		a.vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 1, Col: 0}, Active: buffer.Position{Line: 1, Col: 3}}})
		wait := func(what string, cond func() bool) {
			deadline := time.Now().Add(5 * time.Second)
			for !cond() && time.Now().Before(deadline) {
				time.Sleep(2 * time.Millisecond)
				a.ts.Pump(10 * time.Millisecond)
				a.ts.FlushPosted()
			}
			if !cond() {
				panic("the " + what + " did not show")
			}
		}
		switch what {
		case "list":
			a.vm.TriggerCodeActions(langfeatures.CodeActionTrigger{Type: langfeatures.TriggerInvokedAction, Source: langfeatures.SourceQuickFix}, "none")
			wait("list of actions", a.vm.CodeActionMenuVisible)
		case "rename", "preview":
			a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 2)})
			a.vm.RenameStart()
			wait("rename box", a.vm.RenameVisible)
			if what == "preview" {
				a.vm.RenameSetInput("other")
				a.vm.RenameAccept(true)
				wait("preview", func() bool { return a.vm.RefactorPreview() != nil })
			}
		}
	}
}
