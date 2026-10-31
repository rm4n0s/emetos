package fakeapi

import (
	"context"
	"log/slog"
	"slices"
	"sort"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The Editor Decorations API of the fake (SPEC §10.30 "Blame", step 24.22): the sets of annotations an extension made, in memory, with the
// rules of the host (internal/gui/exthost/editordecorations.go): the same errors, the same cuts, the same disposal with the activation, and
// the links of a hover that run the commands of the extension itself only. extension/editordecorationstest runs one suite on this and on
// the host.

// annotationSet is the annotations of one key.
type annotationSet struct {
	uri  string
	list []extension.LineAnnotation
}

// EditorDecorations implements extension.API.
func (a *API) EditorDecorations() extension.EditorDecorations { return editorDecorationsAPI{a} }

type editorDecorationsAPI struct{ a *API }

// Set implements extension.EditorDecorations with the errors of the host: ExtensionDeactivated and ExtensionEditorDecorationsInvalid.
func (d editorDecorationsAPI) Set(key, uri string, list []extension.LineAnnotation) error {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.scm.dead {
		return errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	if key == "" || uri == "" {
		return errors.New("ExtensionEditorDecorationsInvalid", "a set of annotations needs a key and the URI of a document", "key", key, "uri", uri)
	}
	if len(list) > extension.MaxLineAnnotations {
		return errors.New("ExtensionEditorDecorationsInvalid", "a set has too many annotations", "key", key, "count", len(list))
	}
	for _, an := range list {
		if an.Line < 0 {
			return errors.New("ExtensionEditorDecorationsInvalid", "the line of an annotation is not negative", "key", key, "line", an.Line)
		}
	}
	if a.annotationsUnavailable {
		return errors.New("ExtensionEditorDecorationsUnavailable", "the scope has no editors to decorate")
	}
	if a.annotations == nil {
		a.annotations = map[string]*annotationSet{}
	}
	if len(list) == 0 {
		delete(a.annotations, key)
		return nil
	}
	cp := make([]extension.LineAnnotation, len(list))
	for i, an := range list {
		if utf8.RuneCountInString(an.Text) > extension.MaxLineAnnotationText {
			an.Text = string([]rune(an.Text)[:extension.MaxLineAnnotationText])
		}
		if len(an.Hover) > extension.MaxLineAnnotationHover {
			n := extension.MaxLineAnnotationHover
			for n > 0 && !utf8.RuneStart(an.Hover[n]) {
				n--
			}
			an.Hover = an.Hover[:n]
		}
		cp[i] = an
	}
	a.annotations[key] = &annotationSet{uri: uri, list: cp}
	return nil
}

// Clear implements extension.EditorDecorations.
func (d editorDecorationsAPI) Clear(key string) {
	d.a.mu.Lock()
	defer d.a.mu.Unlock()
	delete(d.a.annotations, key)
}

// Annotations are the annotations the editors of the document would draw, sorted by line (the sets in the order of their keys).
func (a *API) Annotations(uri string) []extension.LineAnnotation {
	a.mu.Lock()
	defer a.mu.Unlock()
	keys := make([]string, 0, len(a.annotations))
	for k := range a.annotations {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []extension.LineAnnotation
	for _, k := range keys {
		if s := a.annotations[k]; s.uri == uri {
			out = append(out, s.list...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// AnnotationKeys are the keys that hold a set, sorted.
func (a *API) AnnotationKeys() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var keys []string
	for k := range a.annotations {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// FollowAnnotationLink clicks the link dest in the hover of the annotation at the line of the document, as the editor does: a link to a
// command of the extension (declared by its manifest or registered by it, with a handler) runs it; everything else is ignored. It does
// nothing when the document has no annotation at the line with a hover. The command runs on this goroutine, as the UI thread would.
func (a *API) FollowAnnotationLink(uri string, line int, dest string) {
	a.t.Helper()
	found := false
	for _, an := range a.Annotations(uri) {
		found = found || an.Line == line && an.Hover != ""
	}
	if !found {
		return
	}
	id, args, err := extension.ParseCommandLink(dest)
	if err != nil {
		a.logger.Warn("a link of a hover is not a command link", "link", dest, "err", err)
		return
	}
	a.mu.Lock()
	h := a.handlers[id]
	a.mu.Unlock()
	if h == nil {
		a.logger.Warn("a link of a hover names a command that is not the extension's and was ignored", "command", id)
		return
	}
	if err := h(context.Background(), args); err != nil {
		a.logger.Warn("the command of a link of a hover failed", "command", id, "err", err)
	}
}

// SetEditorDecorationsUnavailable makes Set fail as the host does in a scope that has no editors (a host without a workbench), with the tag
// ExtensionEditorDecorationsUnavailable.
func (a *API) SetEditorDecorationsUnavailable(unavailable bool) {
	a.mu.Lock()
	a.annotationsUnavailable = unavailable
	a.mu.Unlock()
}

// SetLogger replaces the logger of the extension, which the API gives it with Log. Call it before the extension is activated; a test uses it
// to see what the extension logs.
func (a *API) SetLogger(l *slog.Logger) {
	a.mu.Lock()
	a.logger = l
	a.mu.Unlock()
}
