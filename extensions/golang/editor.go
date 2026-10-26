package golang

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The editor in front, for the commands that act on the cursor (SPEC §10.17 "As built (step 14.2)"): the extension follows the
// events of the editor, as the Agents extension does, and reads the text of the document when a command runs.

// lastEditor is the text editor the user looked at last: the file and its selection.
type lastEditor struct {
	// uri is the URI of the file and language its language id; empty before any editor was shown.
	uri, language string
	// sel is the primary selection, and hasSel says that it is known.
	sel    extension.Selection
	hasSel bool
}

// editorEvent follows the editor in front (the status item shows for a Go file) and its selection.
func (g *golang) editorEvent(ev extension.Event) {
	switch ev.Topic {
	case extension.TopicEditorSelectionChanged:
		var es extension.EditorSelection
		if ev.Decode(&es) != nil || es.URI == "" || len(es.Selections) == 0 {
			return
		}
		if es.URI != g.editor.uri {
			g.editor = lastEditor{uri: es.URI, language: g.editor.language}
		}
		g.editor.sel, g.editor.hasSel = es.Selections[0], true
	case extension.TopicEditorActiveChanged:
		var ae extension.ActiveEditor
		if ev.Decode(&ae) != nil || ae.URI == "" {
			return // no text editor in front: the last one stays the one to refer to
		}
		if ae.URI != g.editor.uri {
			g.editor = lastEditor{uri: ae.URI}
		}
		g.editor.language = ae.LanguageID
		g.activeGo = goLanguage(ae.LanguageID)
		g.updateStatus()
	}
}

// goFile is a Go file the user is in: where it is, its text and the selection in it.
type goFile struct {
	uri, path, text string
	// start and end are the byte offsets of the selection in the text, start <= end, and sel the selection as it was told.
	start, end int
	sel        extension.Selection
}

// activeGoFile reads the Go file of the editor in front. When there is none it tells the user what the command needs and
// reports false. A file other than a .go file is not a Go file here (go.mod has no functions or structs).
func (g *golang) activeGoFile(ctx context.Context, what string) (goFile, bool) {
	e := g.editor
	path, ok := filePath(e.uri)
	if !ok || !strings.HasSuffix(path, ".go") {
		g.api.Notifications().Info(what + " needs a Go file: open one and put the cursor in it.")
		return goFile{}, false
	}
	text, err := g.api.Documents().Read(ctx, e.uri)
	if err != nil {
		g.api.Log().Warn("the Go file cannot be read", "uri", e.uri, "err", err)
		g.api.Notifications().Error(what + ": " + filepath.Base(path) + " cannot be read: " + err.Error())
		return goFile{}, false
	}
	f := goFile{uri: e.uri, path: path, text: text, sel: e.sel}
	if e.hasSel {
		a, b := offsetOf(text, e.sel.Anchor), offsetOf(text, e.sel.Active)
		f.start, f.end = min(a, b), max(a, b)
	}
	return f, true
}

// refreshed is the file as it is now: its text, and the selection when it is still the editor in front. A file that cannot be read
// any more is returned as it was.
func (g *golang) refreshed(ctx context.Context, f goFile) goFile {
	text, err := g.api.Documents().Read(ctx, f.uri)
	if err != nil {
		return f
	}
	f.text = text
	if g.editor.uri == f.uri && g.editor.hasSel {
		f.sel = g.editor.sel
	}
	a, b := offsetOf(text, f.sel.Anchor), offsetOf(text, f.sel.Active)
	f.start, f.end = min(a, b), max(a, b)
	return f
}

// filePath is the path of a file: URI.
func filePath(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Path == "" {
		return "", false
	}
	return filepath.FromSlash(u.Path), true
}

// fileURI is the URI of a path.
func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

// utf16Column is the column in UTF-16 units of the byte column in the line of the text: how gopls counts (it does not take up
// the UTF-8 encoding of positions that the client offers).
func utf16Column(text string, p extension.Position) int {
	off := offsetOf(text, extension.Position{Line: p.Line})
	line := text[off:]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if p.Col > len(line) {
		p.Col = len(line)
	}
	prefix := line[:p.Col]
	if !utf8.ValidString(prefix) {
		return p.Col
	}
	n := 0
	for _, r := range prefix {
		n += utf16.RuneLen(r)
	}
	return n
}

// describe is the text of an error for the user.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}
