package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// Completion in the window (SPEC §9.12 "Completion", "As built (step 12.5)"): the editors get the completion providers of the
// tab, the words of the open documents are a provider of the tab, and the command of an accepted item runs through the server
// that sent it or the commands of the window.

// suggestHost is what the editors of the tab complete with: the registry of the providers of the extension host, the state the
// editors share, and the means to run a command and to report a failure.
func (w *Workspace) suggestHost() *editor.SuggestHost {
	if w.suggest == nil {
		w.suggest = editor.NewSuggestShared()
	}
	h := &editor.SuggestHost{
		Ctx: w.ctx, Shared: w.suggest, Execute: w.executeCompletionCommand,
		Report:   func(err error) { w.Notify.Error("Emetos", describe(err)) },
		Tokenize: w.tokenizeBlock,
		Log:      func(msg string, err error) { w.Process.Logger.Debug(msg, "err", err) },
	}
	if scope := w.Extensions(); scope != nil {
		h.Features = scope.Features()
		h.Symbols = w.symbolCache(h.Features)
		h.Semantic = w.semanticCache(h.Features)
	}
	w.navigationHost(h)
	h.ApplyEdit = w.applyWorkspaceEdit
	h.KeyLabel = w.keyLabel
	return h
}

// applyWorkspaceEdit makes the edit of a code action or a rename (UI thread). A refusal is returned for the editor to show.
func (w *Workspace) applyWorkspaceEdit(ctx context.Context, we *langfeatures.WorkspaceEdit) error {
	_, err := langfeatures.Apply(ctx, workspaceEdits{w}, we)
	return err
}

// keyLabel is the label of the primary key of a command in the context of the editor, or "".
func (w *Workspace) keyLabel(id string) string {
	s := w.liveShell()
	if s == nil || w.Keybindings == nil {
		return ""
	}
	if kb, ok := w.Keybindings.LookupPrimaryKeybinding(id, s.editorMenuContext(), false); ok && kb.Keybinding != nil {
		return kb.Keybinding.Label()
	}
	return ""
}

// tokenizeBlock colors the code of a Markdown block of the hover: the language is named as a fence names it, and the grammar
// and the theme are the editor's.
func (w *Workspace) tokenizeBlock(language, text string) ([][]extension.Token, bool) {
	l := extLanguages{w: w}
	id, ok := l.Resolve(language)
	if !ok {
		return nil, false
	}
	lines, err := l.Tokenize(id, text)
	return lines, err == nil
}

// executeCompletionCommand runs the command of an accepted item. The command of a language server goes to the server, off the
// UI thread; any other is a command of the window.
func (w *Workspace) executeCompletionCommand(ctx context.Context, cmd langfeatures.Command) error {
	if scope := w.Extensions(); scope != nil && cmd.Owner != "" {
		go func() {
			found, err := scope.ExecuteServerCommand(ctx, cmd)
			switch {
			case err != nil && ctx.Err() == nil:
				w.onUI(func() { w.Notify.Error("Emetos", describe(err)) })
			case !found:
				w.onUI(func() {
					if err := w.runItemCommand(ctx, cmd); err != nil {
						w.Notify.Error("Emetos", describe(err))
					}
				})
			}
		}()
		return nil
	}
	return w.runItemCommand(ctx, cmd)
}

// runItemCommand runs the command of an item as a command of the window, with its first argument.
func (w *Workspace) runItemCommand(ctx context.Context, cmd langfeatures.Command) error {
	var arg any
	if len(cmd.Arguments) > 0 {
		if err := json.Unmarshal(cmd.Arguments[0], &arg); err != nil {
			arg = nil
		}
	}
	return w.Commands.Run(command.Context{Ctx: ctx, Keys: w.Keys, Source: command.SourceAPI}, cmd.ID, arg)
}

// startCompletion registers the provider of the words of the open documents (editor.wordBasedSuggestions) with the registry of
// the scope. It has the score of every document, so it is only asked when the providers of the language have nothing to say.
func (w *Workspace) startCompletion(scope *exthost.Scope) {
	wp := &langfeatures.WordProvider{
		Mode: func(doc langfeatures.Document) string {
			return settings.Get[string](w.Settings, "editor.wordBasedSuggestions", doc.LanguageID)
		},
		Documents: w.openDocumentSnapshots,
		WordAt: func(line string, col int) (int, int, bool) {
			wc := edit.NewWordClassifier(settings.Get[string](w.Settings, "editor.wordSeparators", ""))
			word, ok := wc.WordAt(line, col)
			return word.Start, word.End, ok
		},
	}
	remove := scope.Features().RegisterCompletion(langfeatures.WordOwner, langfeatures.Selector{}, wp)
	w.stops = append(w.stops, remove)
}

// openDocumentSnapshots gives snapshots of the open documents: the documents belong to the UI thread, so the goroutine of a
// request asks it to take them.
func (w *Workspace) openDocumentSnapshots(ctx context.Context) []langfeatures.Document {
	out := make(chan []langfeatures.Document, 1)
	w.onUI(func() {
		var docs []langfeatures.Document
		for _, d := range w.Documents.All() {
			if strings.HasPrefix(d.URI().Scheme, "output") {
				continue
			}
			docs = append(docs, langfeatures.Document{URI: d.URI(), LanguageID: d.LanguageID(), Version: d.Version(), Text: d.Snapshot()})
		}
		out <- docs
	})
	select {
	case docs := <-out:
		return docs
	case <-ctx.Done():
		return nil
	}
}
