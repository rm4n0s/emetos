package app

import (
	"slices"
	"sync/atomic"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/annotations"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// Line annotations in the window (SPEC §10.30 "Blame", step 24.22): the texts that extensions put after the end of lines through
// api.EditorDecorations(). The editor of a document shows the annotations the store has for its URI.

// followAnnotations keeps the line annotations of an editor those of the store for its document: they are set when the editor is made,
// again whenever the store changes them (the change may come from any goroutine, so it is posted to the UI thread, once for any number of
// changes), and when the document is saved under another name. It returns the function that stops it.
func (s *shellState) followAnnotations(binding *widgets.WidgetsBinding, e *textEditor) func() {
	win := PeekWorkspace(s.Context())
	if win == nil || win.Annotations == nil {
		return func() {}
	}
	store := win.Annotations
	var uri atomic.Value
	uri.Store(e.doc.URI().String())
	var pending atomic.Bool
	sync := func() {
		pending.Store(false)
		if e.disposed {
			return
		}
		mine, _ := uri.Load().(string)
		e.view.SetLineAnnotations(editorAnnotations(store.For(mine)))
	}
	unsubscribe := store.Subscribe(func(uris []string) {
		mine, _ := uri.Load().(string)
		if slices.Contains(uris, mine) && pending.CompareAndSwap(false, true) {
			binding.Post(sync)
		}
	})
	unwatchURI := e.doc.OnURIChange(func(_, now document.URI) {
		uri.Store(now.String())
		binding.Post(sync)
	})
	if len(store.For(e.doc.URI().String())) > 0 {
		sync()
	}
	return func() {
		unsubscribe()
		unwatchURI()
		e.view.DisposeLineAnnotations()
	}
}

// editorAnnotations are the annotations of the store as those of an editor.
func editorAnnotations(list []annotations.Annotation) []editor.LineAnnotation {
	out := make([]editor.LineAnnotation, len(list))
	for i, a := range list {
		out[i] = editor.LineAnnotation{Line: a.Line, Text: a.Text, Color: a.Color, Hover: a.Hover, Link: a.Link}
	}
	return out
}
