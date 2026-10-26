package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// Navigation in the window (SPEC §9.12 "Navigation", "As built (step 12.7)"): what the editors need of the window to go to a
// location, to show a file in the peek view, to read the text of the files the peek view lists and to share the document
// symbols. The commands, the requests and the views are in the editor package; the history of locations is in navigation.go.

// symbolCache is the cache of the document symbols of the tab over a registry; a new registry (the extension host of the tab
// was made again) gets a new cache.
func (w *Workspace) symbolCache(reg *langfeatures.Registry) *langfeatures.SymbolCache {
	if w.symbols == nil || w.symbolsReg != reg {
		w.symbols, w.symbolsReg = langfeatures.NewSymbolCache(w.ctx, reg), reg
	}
	return w.symbols
}

// liveShell is the shell of the window while it is on screen, else nil.
func (w *Workspace) liveShell() *shellState {
	if s := w.shell; s != nil && s.Mounted() {
		return s
	}
	return nil
}

// navigationHost gives the host of the editors the means of navigation: showing a location, the editor of the peek view, the
// text of the files, and the jump of the cursor.
func (w *Workspace) navigationHost(h *editor.SuggestHost) {
	h.Open = func(loc langfeatures.Location, o editor.OpenLocation) {
		if s := w.liveShell(); s != nil {
			s.navOpen(loc, o)
		}
	}
	h.Embedded = func(ctx context.Context, uri document.URI, done func(*editor.ViewModel, func(), error)) {
		if s := w.liveShell(); s != nil {
			s.openEmbedded(ctx, uri, done)
		}
	}
	h.Texts = w.navTexts
	h.DirLabel = w.dirLabel
	h.JumpTo = func(vm *editor.ViewModel, p buffer.Position) {
		if s := w.liveShell(); s != nil {
			for _, e := range s.group.editors {
				if e.view == vm {
					s.jumpTo(e, p)
					return
				}
			}
		}
		vm.GoToPosition(p)
	}
}

// navTexts gives the text of a document for the rows of the peek view, from any goroutine: the snapshot of the open document,
// taken on the UI thread, else the file as it is on disk.
func (w *Workspace) navTexts(ctx context.Context, uri document.URI) (lsp.Lines, error) {
	got := make(chan lsp.Lines, 1)
	w.onUI(func() {
		if doc, ok := w.Documents.Get(uri); ok {
			got <- doc.Snapshot()
			return
		}
		got <- nil
	})
	select {
	case l := <-got:
		if l != nil {
			return l, nil
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return workspaceLSP{w}.ReadText(ctx, uri)
}

// followFeatures makes the editor ask for its symbols and highlights again when a language server starts or stops.
func (s *shellState) followFeatures(binding *widgets.WidgetsBinding, e *textEditor, win *Workspace) {
	scope := win.Extensions()
	if scope == nil {
		return
	}
	e.stopFeatures = scope.Features().OnChange(func() {
		binding.Post(func() {
			if !e.disposed {
				e.view.FeaturesChanged()
			}
		})
	})
}

// navOpen shows a location as a jump of the history (SPEC §10.3): in the active editor when it holds the file, else in the
// editor of the file, opened as a preview editor unless the caller asks to keep it (the group beside the active one for Side).
// The cursor is put at the start of the range and the location is scrolled into view.
func (s *shellState) navOpen(loc langfeatures.Location, o editor.OpenLocation) {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	at := func(e *textEditor) {
		if e.disposed {
			return
		}
		p := e.doc.Snapshot().ValidatePosition(loc.Range.Start)
		s.setSelection(e, []buffer.Selection{buffer.NewCursor(p)}, editorgroup.ReasonJump)
		e.view.ClearRangeHighlight()
		if o.After != nil {
			o.After(e.view)
		}
	}
	if src := s.activeEditor(); src != nil && !o.Side && src.doc.URI() == loc.URI {
		at(src)
		src.view.RevealPositionInCenterIfOutsideViewport(src.view.Cursors().Primary().Position())
		return
	}
	group := s.activeGroup()
	if o.Side {
		if g := s.sideGroup(); g != nil {
			group = g
		}
	}
	place := func(e *textEditor) {
		at(e)
		binding.Scheduler().ScheduleFrame()
		binding.Scheduler().AddPostFrameCallback(func(time.Duration) {
			if !e.disposed {
				e.view.RevealPositionInCenter(e.view.Cursors().Primary().Position())
			}
		})
	}
	opts := openOptions{Preview: !o.Pinned, Group: group, Place: place}
	if doc, ok := win.Documents.Get(loc.URI); ok {
		win.Documents.Retain(doc)
		s.show(binding, doc, opts)
		return
	}
	if loc.URI.Scheme != document.SchemeFile {
		win.Notify.Warn("Emetos", "Cannot open "+loc.URI.String())
		return
	}
	s.Go(func(life context.Context) {
		doc, err := win.Documents.Acquire(life, loc.URI.Path, document.NewOptions())
		binding.Post(func() {
			if !s.Mounted() {
				if err == nil {
					win.Documents.Release(doc)
				}
				return
			}
			if err != nil {
				win.Notify.Error("Emetos", "Cannot open "+loc.URI.Path+": "+describe(err))
				return
			}
			s.show(binding, doc, opts)
		})
	})
}

// openEmbedded makes the editor that the peek view shows a file in: an editor over the document (the open one, or the file
// read for it) that is in no group and has no tab. done is called on the UI thread with its view model and the function that
// lets go of it, or with the error; a URI that is not a file and not open cannot be shown.
func (s *shellState) openEmbedded(ctx context.Context, uri document.URI, done func(*editor.ViewModel, func(), error)) {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	build := func(doc *document.Document) {
		input := editorgroup.NewTextInput(doc, s.inputOptions())
		ed := s.newTextEditorWith(binding, input, embeddedOptions)
		ed.view.SetEmbedded(true)
		done(ed.view, func() {
			ed.dispose()
			if win.Documents.Release(doc) {
				s.autoSaveUntrack(doc)
				s.externalUntrack(doc)
			}
			input.Dispose()
		}, nil)
	}
	if doc, ok := win.Documents.Get(uri); ok {
		win.Documents.Retain(doc)
		build(doc)
		return
	}
	if uri.Scheme != document.SchemeFile {
		done(nil, nil, errors.New("PeekFileUnsupported", "only a file can be shown in the peek view", "uri", uri.String()))
		return
	}
	s.Go(func(life context.Context) {
		doc, err := win.Documents.Acquire(life, uri.Path, document.NewOptions())
		binding.Post(func() {
			switch {
			case !s.Mounted() || ctx.Err() != nil:
				if err == nil {
					win.Documents.Release(doc)
				}
				// the peek view is gone: nobody waits for the editor
			case err != nil:
				done(nil, nil, err)
			default:
				build(doc)
			}
		})
	})
}

// embeddedOptions adjusts the options of the editor of the peek view: it has no minimap, folding controls or sticky scroll,
// as VS Code's embedded editor of the peek view.
func embeddedOptions(o editor.Options) editor.Options {
	o.Minimap.Enabled = false
	o.Folding = false
	o.StickyScroll.Enabled = false
	o.GlyphMargin = false
	return o
}

// dirLabel is the folder of a file as the peek view writes it: relative to the folder of the window (empty for the folder
// itself), with the home folder as "~", else as it is.
func (w *Workspace) dirLabel(dir string) string {
	if w.Folder != "" {
		if rel, err := filepath.Rel(w.Folder, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if rel == "." {
				return ""
			}
			return rel
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if rel == "." {
				return "~"
			}
			return "~/" + rel
		}
	}
	return dir
}
