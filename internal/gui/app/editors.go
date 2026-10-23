package app

import (
	"context"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/folding"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/tokens"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// textEditor is the editor of a text input: a view model of its own over the document the input
// shows (its cursors, scroll position, folds and undo selections are the editor's, and come back
// when its tab is activated again), with the background work that feeds the view: the tokens, the
// bracket pairs, the indentation levels of the guides and the folding regions. The document is
// shared with the other editors of it.
type textEditor struct {
	input *editorgroup.TextInput
	doc   *document.Document
	view  *editor.ViewModel
	// tokenizer keeps the tokens of the document the editor shows.
	tokenizer *tokens.Tokenizer
	// stopBrackets stops the bracket pairs of the document following it, stopGuides the indentation
	// levels the guides are drawn from, stopFolding the folding regions and unsubscribe the settings.
	stopBrackets, stopGuides, stopFolding, unsubscribe func()
	// done is closed when the editor is disposed; it ends the background loop of the tokenizer.
	done     chan struct{}
	disposed bool
}

// newTextEditor makes the editor of an input: its view model, kept current as the settings change
// (the change may come from any goroutine, so it is posted to the UI thread), and the background
// work. spawn starts a goroutine that ends with the shell.
func (s *shellState) newTextEditor(binding *widgets.WidgetsBinding, input *editorgroup.TextInput) *textEditor {
	win := PeekWindow(s.Context())
	doc := input.Document()
	e := &textEditor{input: input, doc: doc, done: make(chan struct{})}
	options := func() editor.Options {
		desktop, _ := binding.PlatformDispatcher().MonospaceFont()
		tabSize, _ := doc.Indentation()
		return editor.OptionsFromSettings(win.Settings, doc.LanguageID(), desktop, tabSize)
	}
	e.view = editor.NewViewModel(doc, options(), nil)
	if win.Documents.AnnounceLarge(doc) {
		win.Process.Notify.Warn("Emetos", LargeFileNotice)
	}
	// The background work of the editor (the folding regions, the tokens) hands its results to the
	// UI thread from the start, before the editor widget exists to say how.
	e.view.SetPoster(binding.Post)
	e.view.SetIndentationDefaults(func() (int, bool) {
		p := win.indentationFor(doc.LanguageID())
		return p.TabSize, p.InsertSpaces
	})
	s.startTokenizer(e, win)
	e.startBrackets(win)
	e.startGuides(win)
	e.startFolding(win)
	e.view.SetIndenter(win.Process.Indenter(), win.Process.Languages.Code(doc.LanguageID()))
	e.view.SetComments(win.Process.Comments())
	e.view.SetSnippets(win.Process.Snippets)
	e.view.SetAutoClosing(win.Process.AutoClosing().Language(win.Process.Languages.Code(doc.LanguageID())))
	e.unsubscribe = win.Settings.OnChange(func(ev settings.ChangeEvent) {
		if ev.Affects("editor", "") || ev.Affects("emmet", "") {
			binding.Post(func() {
				if !e.disposed {
					e.view.SetOptions(options())
				}
			})
		}
	})
	return e
}

// startTokenizer makes the tokenizer of the document, with the grammar of its language, gives it
// to the editor and starts its background loop, which ends with the editor. A language with no
// grammar has none; a grammar that cannot be made is reported and the text stays plain.
func (s *shellState) startTokenizer(e *textEditor, win *Window) {
	languageID := e.doc.LanguageID()
	maxLine := int(settings.Get[float64](win.Settings, "editor.maxTokenizationLineLength", languageID))
	grammar, err := win.Process.Grammar(languageID, maxLine)
	if err != nil {
		win.Process.Notify.Warn("Emetos", "Cannot highlight the "+languageID+" file: "+describe(err))
	}
	e.tokenizer = tokens.New(e.doc, grammar, tokens.Options{})
	e.view.SetTokenizer(e.tokenizer)
	s.Go(func(life context.Context) {
		ctx, cancel := context.WithCancel(life)
		defer cancel()
		go func() {
			select {
			case <-e.done:
				cancel()
			case <-ctx.Done():
			}
		}()
		e.tokenizer.Run(ctx)
	})
}

// startBrackets makes the bracket pairs of the document and gives them to the editor.
func (e *textEditor) startBrackets(win *Window) {
	code := win.Process.Languages.Code(e.doc.LanguageID())
	ix, stop := brackets.Attach(e.doc, e.tokenizer, win.Process.BracketLanguages(), code)
	e.stopBrackets = stop
	e.view.SetBrackets(ix)
}

// startGuides makes the indentation levels of the document, which its indent guides and bracket
// pair guides are drawn from, and gives them to the editor.
func (e *textEditor) startGuides(win *Window) {
	tabSize, _ := e.doc.Indentation()
	levels, stop := indent.AttachLevels(e.doc, tabSize)
	e.stopGuides = stop
	offSide := false
	if c := win.Process.Languages.Configuration(e.doc.LanguageID()); c != nil && c.Folding != nil {
		offSide = c.Folding.OffSide
	}
	e.view.SetGuides(levels, offSide)
}

// startFolding makes the folding of the document: the regions come from the indentation of its
// lines and the folding rules of its language, up to editor.foldingMaximumRegions, and are
// computed in the background.
func (e *textEditor) startFolding(win *Window) {
	provider := folding.IndentProvider{
		Limit: folding.Limit{Max: int(settings.Get[float64](win.Settings, "editor.foldingMaximumRegions", e.doc.LanguageID()))},
	}
	if c := win.Process.Languages.Configuration(e.doc.LanguageID()); c != nil && c.Folding != nil {
		provider.OffSide = c.Folding.OffSide
		if c.Folding.MarkersStart != nil && c.Folding.MarkersEnd != nil {
			provider.Markers = &folding.Markers{Start: c.Folding.MarkersStart, End: c.Folding.MarkersEnd}
		}
	}
	e.view.SetFoldingMarkers(provider.Markers)
	e.stopFolding = e.view.StartFolding(provider, 0)
}

// dispose stops the background work and the following of the settings and the document, and
// takes the editor's decorations off the document. The document stays open for the other editors
// that show it.
func (e *textEditor) dispose() {
	if e.disposed {
		return
	}
	e.disposed = true
	close(e.done)
	e.view.DisposeFind()
	e.view.DisposeWrap()
	e.view.DisposeHighlights()
	if e.stopFolding != nil {
		e.stopFolding()
		e.stopFolding = nil
	}
	if e.stopGuides != nil {
		e.view.SetGuides(nil, false)
		e.stopGuides()
		e.stopGuides = nil
	}
	if e.unsubscribe != nil {
		e.unsubscribe()
		e.unsubscribe = nil
	}
	if e.stopBrackets != nil {
		e.view.SetBrackets(nil)
		e.stopBrackets()
		e.stopBrackets = nil
	}
	if e.tokenizer != nil {
		e.view.SetTokenizer(nil)
		e.tokenizer.Close()
		e.tokenizer = nil
	}
}
