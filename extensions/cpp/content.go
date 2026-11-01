package cpp

import (
	"context"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The read-only tab of Show AST (SPEC §10.34 "C and C++"): a document of the scheme clangd, whose text is the tree clangd answered. The
// command has one document: the next answer replaces the text of the tab that is open.

// astPath is the path of the document.
const astPath = "ast/ast.txt"

// provideContent registers the provider of the documents. A scope that cannot show documents has none: showContent then opens an
// untitled document.
func (c *cpp) provideContent() error {
	reg, err := c.api.Documents().ProvideContent(extension.ContentProvider{
		Scheme:   ContentScheme,
		Label:    func(uri string) string { return lastElement(uri) },
		Language: func(string) string { return "plaintext" },
		Text:     c.contentText,
	})
	if err != nil {
		// a scope that cannot show documents (a view of a client, a tab of an extension) is no failure of the pack, and says nothing
		// that is worth a warning: the log is read by people who have no tab to show it in
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ExtensionDocumentUnavailable" {
			c.api.Log().Debug("the read-only tab of clangd is not available in this scope", "err", err)
			return nil
		}
		c.api.Log().Warn("the read-only tab of clangd is not available", "err", err)
		return nil
	}
	c.reg = reg
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
func (c *cpp) contentText(_ context.Context, uri string) (string, error) {
	_, path, ok := extension.SplitContentURI(uri)
	c.mu.Lock()
	text, found := c.content[path]
	c.mu.Unlock()
	if !ok || !found {
		return "", errors.New("ClangdContentUnknown", "clangd has no such document", "uri", uri)
	}
	return text, nil
}

// showContent puts the text in the read-only document at the path and opens its tab, in the language given. A tab that is open shows
// the new text.
func (c *cpp) showContent(path, title, language, text string) {
	if c.reg == nil {
		if _, err := c.api.Documents().OpenUntitled(language, text); err != nil {
			c.api.Log().Warn("the text of clangd cannot be shown", "err", err)
			c.api.Notifications().Error(title + ": " + describe(err))
		}
		return
	}
	c.mu.Lock()
	c.content[path] = text
	c.mu.Unlock()
	uri := extension.ContentURI(ContentScheme, path)
	c.reg.Changed(uri)
	if err := c.api.Documents().OpenDocument(extension.OpenSpec{URI: uri, Title: title, Preview: true}); err != nil {
		c.api.Log().Warn("the text of clangd cannot be shown", "err", err)
		c.api.Notifications().Error(title + ": " + describe(err))
	}
}
