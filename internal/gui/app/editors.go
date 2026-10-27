package app

import (
	"context"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/folding"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
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
	// focus gives the editor the keyboard focus while its widget is on screen.
	focus *editor.EditorFocus
	// tokenizer keeps the tokens of the document the editor shows.
	tokenizer *tokens.Tokenizer
	// stopBrackets stops the bracket pairs of the document following it, stopGuides the indentation
	// levels the guides are drawn from, stopFolding the folding regions and unsubscribe the settings.
	stopBrackets, stopGuides, stopFolding, unsubscribe func()
	// stopDiagnostics stops following the markers of the file and takes the squiggles away (diagnostics.go).
	stopDiagnostics func()
	// stopFeatures stops listening to the language servers that start and stop (navhost.go).
	stopFeatures func()
	// done is closed when the editor is disposed; it ends the background loop of the tokenizer.
	done     chan struct{}
	disposed bool
	// tokDone is closed to end the loop of the current tokenizer when the language changes.
	tokDone chan struct{}
	// nav is what the history of locations remembers of the editor (navigation.go).
	nav editorNav
	// stopState stops following the view state for the UI state of the window, and stateSig is the view state
	// as last noted (state.go).
	stopState func()
	stateSig  viewSig
	// fileView is the id of the file view the editor shows, "" for the text (fileviews.go), and docHandle the document as
	// the extension's views see it.
	fileView  string
	docHandle *docHandle
}

// newTextEditor makes the editor of an input: its view model, kept current as the settings change
// (the change may come from any goroutine, so it is posted to the UI thread), and the background
// work. spawn starts a goroutine that ends with the shell.
func (s *shellState) newTextEditor(binding *widgets.WidgetsBinding, input *editorgroup.TextInput) *textEditor {
	return s.newTextEditorWith(binding, input, nil)
}

// newTextEditorWith is newTextEditor for an editor whose options the caller adjusts, after the settings have made them
// (the editors of the diff editor are read-only on the left and have no minimap, SPEC §9.11).
func (s *shellState) newTextEditorWith(binding *widgets.WidgetsBinding, input *editorgroup.TextInput, adjust func(editor.Options) editor.Options) *textEditor {
	win := PeekWorkspace(s.Context())
	doc := input.Document()
	e := &textEditor{input: input, doc: doc, done: make(chan struct{}), focus: &editor.EditorFocus{}}
	options := func() editor.Options {
		desktop, _ := binding.PlatformDispatcher().MonospaceFont()
		tabSize, _ := doc.Indentation()
		o := editor.OptionsFromSettings(win.Settings, doc.LanguageID(), desktop, tabSize)
		if win.zenHidesLineNumbers() {
			o.LineNumbers = editor.LineNumbersOff
		}
		if win.isView() {
			o.CursorBlinking = "solid" // a blink is a picture twice a second (headless_engine.go)
		}
		if adjust != nil {
			o = adjust(o)
		}
		return o
	}
	e.view = editor.NewViewModel(doc, options(), nil)
	if win.Documents.AnnounceLarge(doc) {
		win.Notify.Warn("Emetos", LargeFileNotice)
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
	e.view.SetSnippets(win.Snippets)
	e.view.SetSuggest(win.suggestHost())
	e.view.SetQuickPick(win.QuickInput.Pick)
	e.view.SetSnippetWorkspace(win.SnippetWorkspace())
	e.view.SetAutoClosing(win.Process.AutoClosing().Language(win.Process.Languages.Code(doc.LanguageID())))
	e.unsubscribe = win.Settings.OnChange(func(ev settings.ChangeEvent) {
		if ev.Affects("editor", "") || ev.Affects("emmet", "") || ev.Affects("zenMode", "") {
			binding.Post(func() {
				if !e.disposed {
					e.view.SetOptions(options())
				}
			})
		}
	})
	// zen mode hides the line numbers without a setting, so the editor looks at it as the layout changes
	zenOff := win.zenHidesLineNumbers()
	stopSettings, stopZen := e.unsubscribe, win.Layout.OnChange(func() {
		binding.Post(func() {
			if now := win.zenHidesLineNumbers(); !e.disposed && now != zenOff {
				zenOff = now
				e.view.SetOptions(options())
			}
		})
	})
	e.unsubscribe = func() { stopSettings(); stopZen() }
	s.watchSelection(e)
	s.followFeatures(binding, e, win)
	e.stopDiagnostics = s.followDiagnostics(binding, e)
	s.followEditorState(e)
	s.autoSaveTrack(doc)
	s.externalTrack(doc)
	return e
}

// startTokenizer makes the tokenizer of the document, with the grammar of its language, gives it
// to the editor and starts its background loop, which ends with the editor. A language with no
// grammar has none; a grammar that cannot be made is reported and the text stays plain.
func (s *shellState) startTokenizer(e *textEditor, win *Workspace) {
	languageID := e.doc.LanguageID()
	maxLine := int(settings.Get[float64](win.Settings, "editor.maxTokenizationLineLength", languageID))
	grammar, err := win.Process.Grammar(languageID, maxLine)
	if err != nil {
		win.Notify.Warn("Emetos", "Cannot highlight the "+languageID+" file: "+describe(err))
	}
	e.tokenizer = tokens.New(e.doc, grammar, tokens.Options{})
	e.view.SetTokenizer(e.tokenizer)
	tokenizer, tokDone := e.tokenizer, make(chan struct{})
	e.tokDone = tokDone
	s.Go(func(life context.Context) {
		ctx, cancel := context.WithCancel(life)
		defer cancel()
		go func() {
			select {
			case <-e.done:
				cancel()
			case <-tokDone:
				cancel()
			case <-ctx.Done():
			}
		}()
		tokenizer.Run(ctx)
	})
}

// languageChanged makes the editor follow a new language of its document (the file was renamed to another
// extension): the tokens, the bracket pairs, the guides, the folding, the comments, the auto-closing pairs and
// the options of the language are made again.
func (s *shellState) languageChanged(binding *widgets.WidgetsBinding, e *textEditor) {
	if e.disposed {
		return
	}
	win := PeekWorkspace(s.Context())
	if e.stopFolding != nil {
		e.stopFolding()
		e.stopFolding = nil
	}
	if e.stopGuides != nil {
		e.view.SetGuides(nil, false)
		e.stopGuides()
		e.stopGuides = nil
	}
	if e.stopBrackets != nil {
		e.view.SetBrackets(nil)
		e.stopBrackets()
		e.stopBrackets = nil
	}
	if e.tokenizer != nil {
		e.view.SetTokenizer(nil)
		close(e.tokDone)
		e.tokenizer.Close()
		e.tokenizer = nil
	}
	s.startTokenizer(e, win)
	e.startBrackets(win)
	e.startGuides(win)
	e.startFolding(win)
	e.view.SetIndenter(win.Process.Indenter(), win.Process.Languages.Code(e.doc.LanguageID()))
	e.view.SetAutoClosing(win.Process.AutoClosing().Language(win.Process.Languages.Code(e.doc.LanguageID())))
	desktop, _ := binding.PlatformDispatcher().MonospaceFont()
	tabSize, _ := e.doc.Indentation()
	o := editor.OptionsFromSettings(win.Settings, e.doc.LanguageID(), desktop, tabSize)
	if win.zenHidesLineNumbers() {
		o.LineNumbers = editor.LineNumbersOff
	}
	if win.isView() {
		o.CursorBlinking = "solid"
	}
	e.view.SetOptions(o)
}

// startBrackets makes the bracket pairs of the document and gives them to the editor.
func (e *textEditor) startBrackets(win *Workspace) {
	code := win.Process.Languages.Code(e.doc.LanguageID())
	ix, stop := brackets.Attach(e.doc, e.tokenizer, win.Process.BracketLanguages(), code)
	e.stopBrackets = stop
	e.view.SetBrackets(ix)
}

// startGuides makes the indentation levels of the document, which its indent guides and bracket
// pair guides are drawn from, and gives them to the editor.
func (e *textEditor) startGuides(win *Workspace) {
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
func (e *textEditor) startFolding(win *Workspace) {
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
	var p folding.Provider = provider
	// the ranges of a language server replace the indentation provider (step 12.9); it stays the fallback
	if scope := win.Extensions(); scope != nil {
		lang := e.doc.LanguageID()
		p = langfeatures.FoldingProvider{
			Registry: scope.Features(), Fallback: provider, Limit: provider.Limit,
			Strategy: func() string { return settings.Get[string](win.Settings, "editor.foldingStrategy", lang) },
			Log:      func(msg string, err error) { win.Process.Logger.Debug(msg, "err", err) },
		}
	}
	e.stopFolding = e.view.StartFolding(p, 0)
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
	if e.docHandle != nil {
		e.docHandle.release()
	}
	e.unwatchSelection()
	if e.stopState != nil {
		e.stopState()
		e.stopState = nil
	}
	if e.stopDiagnostics != nil {
		e.stopDiagnostics()
		e.stopDiagnostics = nil
	}
	if e.stopFeatures != nil {
		e.stopFeatures()
		e.stopFeatures = nil
	}
	e.view.PeekClose()
	e.view.CancelGoTo()
	e.view.LinkClear()
	e.view.DisposeSymbols()
	e.view.DisposeInlayHints()
	e.view.DisposeCodeLenses()
	e.view.DisposeSemanticTokens()
	e.view.SelectionRangesCancel()
	e.view.DisposeFind()
	e.view.DisposeWrap()
	e.view.DisposeZones()
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
