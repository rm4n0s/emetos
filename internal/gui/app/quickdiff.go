package app

import (
	"context"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// The quick diff in the window (SPEC §10.30 "Quick diff", step 24.11): the editor of a file asks the registry of the providers what the
// file is compared with, and marks the changes in its gutter, its overview ruler and its minimap. The model that does the comparing is
// core/scm/quickdiff; the editor draws what it finds.

// quickDiffText reads the text of an original that a provider named: a document that an extension provides, or a file.
func (w *Workspace) quickDiffText(ctx context.Context, uri string) (string, error) {
	if strings.HasPrefix(uri, "file://") {
		// a file URI of an extension has its path escaped
		res, err := fileio.Read(ctx, scm.PathOfURI(uri), fileio.ReadOptions{})
		return res.Text, err
	}
	u, err := document.ParseURI(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme == document.SchemeVirtual {
		content, err := w.Documents.GenerateVirtual(ctx, u)
		return content.Text, err
	}
	return "", errors.New("QuickDiffOriginalUnsupported", "the original of a quick diff is a provided document or a file", "uri", uri)
}

// followQuickDiff keeps the quick diff marks of an editor those of the model of its document: the model compares the text with the
// originals of the providers, off the UI thread, and the editor is given its changes. It returns the function that stops it and takes the
// marks away. A window without the registry (the headless view of a workspace that has none) marks nothing.
func (s *shellState) followQuickDiff(binding *widgets.WidgetsBinding, e *textEditor) func() {
	win := PeekWorkspace(s.Context())
	if win == nil || win.QuickDiff == nil {
		return func() {}
	}
	lang := func() string { return e.doc.LanguageID() }
	model := quickdiff.NewModel(win.QuickDiff, e.doc, quickdiff.Config{
		Post: binding.Post,
		IgnoreTrimWhitespace: func() bool {
			switch settings.Get[string](win.Settings, "scm.diffDecorationsIgnoreTrimWhitespace", lang()) {
			case "true":
				return true
			case "inherit":
				return settings.Get[bool](win.Settings, "diffEditor.ignoreTrimWhitespace", lang())
			}
			return false
		},
	})
	stopModel := model.OnChange(func() {
		if !e.disposed {
			e.view.SetQuickDiffMarks(quickDiffMarks(model.Result()))
		}
	})
	stopPeek := s.startQuickDiffPeek(binding, e, model)
	unsubscribe := win.Settings.OnChange(func(ev settings.ChangeEvent) {
		if ev.Affects("scm.diffDecorationsIgnoreTrimWhitespace", "") || ev.Affects("diffEditor.ignoreTrimWhitespace", "") {
			binding.Post(func() {
				if !e.disposed {
					model.Recompute()
				}
			})
		}
	})
	return func() {
		stopPeek()
		unsubscribe()
		stopModel()
		model.Close()
		e.view.DisposeQuickDiff()
	}
}

// quickDiffMarks are the changes of a result as the marks of an editor.
func quickDiffMarks(res quickdiff.Result) []editor.QuickDiffMark {
	marks := make([]editor.QuickDiffMark, len(res.Changes))
	for i, c := range res.Changes {
		marks[i] = editor.QuickDiffMark{Type: c.Type(), Start: c.ModifiedStart, End: c.ModifiedEnd, Secondary: c.Kind == quickdiff.KindSecondary}
	}
	return marks
}

// quickDiffBaseline is the text the format on save of modifications compares a file with when a source control has an original for it:
// the original of the primary quick diff provider (SPEC §9.12). It returns false for a file no provider has an original for, and for a
// window with no providers, which compares with the file on disk.
func (w *Workspace) quickDiffBaseline(ctx context.Context, path string) (string, bool) {
	if w.QuickDiff == nil || w.QuickDiff.Count() == 0 {
		return "", false
	}
	text, ok, err := w.QuickDiff.PrimaryOriginal(ctx, quickdiff.URIOf(document.FileURI(path)))
	if err != nil {
		return "", false
	}
	return text, ok
}
