package rust

import (
	"context"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The read-only tabs of Expand macro and View Syntax Tree (SPEC §10.34 "Rust"): documents of the scheme rust-analyzer, whose text is
// what the server answered. Each command has one document: the next answer replaces the text of the tab that is open, as VS Code's
// extension does.

// The paths of the documents.
const (
	expansionPath = "expand-macro/expansion.rs"
	treePath      = "syntax-tree/syntax.rast"
)

// provideContent registers the provider of the documents. A scope that cannot show documents has none: showContent then opens an
// untitled document.
func (r *rust) provideContent() error {
	reg, err := r.api.Documents().ProvideContent(extension.ContentProvider{
		Scheme: ContentScheme,
		Label:  func(uri string) string { return lastElement(uri) },
		Language: func(uri string) string {
			if strings.HasSuffix(uri, ".rs") {
				return "rust"
			}
			return "plaintext"
		},
		Text: r.contentText,
	})
	if err != nil {
		// a scope that cannot show documents (a view of a client, a tab of an extension) is no failure of the pack
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ExtensionDocumentUnavailable" {
			r.api.Log().Debug("the read-only tabs of rust-analyzer are not available in this scope", "err", err)
			return nil
		}
		r.api.Log().Warn("the read-only tabs of rust-analyzer are not available", "err", err)
		return nil
	}
	r.reg = reg
	return nil
}

// lastElement is the last element of a URI.
func lastElement(uri string) string {
	if i := strings.LastIndexByte(uri, '/'); i >= 0 {
		return uri[i+1:]
	}
	return uri
}

// contentText is ContentProvider.Text: it runs off the UI thread.
func (r *rust) contentText(_ context.Context, uri string) (string, error) {
	_, path, ok := extension.SplitContentURI(uri)
	r.mu.Lock()
	text, found := r.content[path]
	r.mu.Unlock()
	if !ok || !found {
		return "", errors.New("RustContentUnknown", "rust-analyzer has no such document", "uri", uri)
	}
	return text, nil
}

// showContent puts the text in the read-only document at the path and opens its tab, in the language given. A tab that is open shows
// the new text.
func (r *rust) showContent(path, title, language, text string) {
	if r.reg == nil {
		if _, err := r.api.Documents().OpenUntitled(language, text); err != nil {
			r.api.Log().Warn("the text of rust-analyzer cannot be shown", "err", err)
			r.api.Notifications().Error(title + ": " + describe(err))
		}
		return
	}
	r.mu.Lock()
	r.content[path] = text
	r.mu.Unlock()
	uri := extension.ContentURI(ContentScheme, path)
	r.reg.Changed(uri)
	if err := r.api.Documents().OpenDocument(extension.OpenSpec{URI: uri, Title: title, Preview: true}); err != nil {
		r.api.Log().Warn("the text of rust-analyzer cannot be shown", "err", err)
		r.api.Notifications().Error(title + ": " + describe(err))
	}
}
