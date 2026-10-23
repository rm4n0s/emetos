package editor

import (
	"reflect"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/comment"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/folding"
	"github.com/rm4n0s/emetos/internal/core/indent"
)

// Options are the settings an editor reads, resolved for one document. The widget makes
// them from the settings store; a view model takes them as plain values, so that it is
// tested without a window.
type Options struct {
	// FontFamilies are the font families in the order to try; the first is the family and
	// the rest are the fallback.
	FontFamilies []string
	// FontSize is the font size in pixels.
	FontSize float64
	// FontWeight is the weight of the font, from 1 to 1000 (400 is normal, 700 bold); 0 is
	// normal.
	FontWeight int
	// LetterSpacing is the space between letters, in pixels.
	LetterSpacing float64
	// LineHeight is the height of a row in pixels (see LineHeight).
	LineHeight float64
	// TabSize is the distance between tab stops in characters.
	TabSize int
	// FontFeatures are OpenType features such as "liga" 0; nil leaves the font's own.
	FontFeatures []FontFeature
	// LineNumbers says how line numbers are shown.
	LineNumbers LineNumbersMode
	// LineNumbersMinChars is the least width of the line numbers in characters.
	LineNumbersMinChars int
	// GlyphMargin says whether the gutter has a glyph margin.
	GlyphMargin bool
	// LineDecorationsWidth is the width of the line decorations.
	LineDecorationsWidth float64
	// Folding is editor.folding: whether lines can be folded.
	Folding bool
	// ShowFoldingControls is editor.showFoldingControls: "always", "mouseover" or "never"; with
	// "never" the gutter has no room for the controls.
	ShowFoldingControls string
	// FoldingHighlight is editor.foldingHighlight: the folded lines have a background.
	FoldingHighlight bool
	// UnfoldOnClickAfterEndOfLine is editor.unfoldOnClickAfterEndOfLine.
	UnfoldOnClickAfterEndOfLine bool
	// ScrollBeyondLastLine lets the last line be scrolled up to the top of the editor.
	ScrollBeyondLastLine bool
	// RenderLineHighlight is where the current line is highlighted: "none", "gutter", "line"
	// or "all".
	RenderLineHighlight string
	// RenderLineHighlightOnlyWhenFocus highlights the current line only while the editor has
	// the focus.
	RenderLineHighlightOnlyWhenFocus bool
	// RoundedSelection gives selections rounded corners.
	RoundedSelection bool
	// CursorWidth is the width of the cursor in pixels; 0 means 2.
	CursorWidth float64
	// CursorBlinking is the style of the cursor animation: "blink", "smooth", "phase",
	// "expand" or "solid".
	CursorBlinking string
	// CursorStyle is the shape of the cursor: "line", "block", "underline", "line-thin",
	// "block-outline" or "underline-thin".
	CursorStyle string
	// MouseWheelZoom zooms the font with Control and the mouse wheel.
	MouseWheelZoom bool
	// MultiCursorModifier is the key that adds cursors with the mouse: "alt" or "ctrlCmd".
	MultiCursorModifier string
	// ColumnSelection makes a selection with the mouse a column selection.
	ColumnSelection bool
	// MouseStyle is the mouse pointer over the text: "text", "default" or "copy".
	MouseStyle string
	// MouseWheelScrollSensitivity multiplies the distance the mouse wheel and the touchpad
	// scroll.
	MouseWheelScrollSensitivity float64
	// FastScrollSensitivity multiplies it again while Alt is held.
	FastScrollSensitivity float64
	// SmoothScrolling animates the scrolling of the mouse wheel.
	SmoothScrolling bool
	// DragAndDrop lets a selection be moved with the mouse.
	DragAndDrop bool
	// SelectionClipboard makes the selection the primary selection of the desktop, and the
	// middle button paste it.
	SelectionClipboard bool
	// MultiCursorPaste is "spread" (a text of as many lines as there are cursors goes line by
	// line to the cursors) or "full" (every cursor pastes the whole text).
	MultiCursorPaste string
	// Emmet are the settings emmet.* that abbreviation expansion reads (SPEC §9.10.2).
	Emmet EmmetOptions
	// TabCompletion is "off", "on" or "onlySnippets": with the last the Tab key inserts the snippet
	// whose prefix is before the cursor. ("on" needs the suggestions of area 3 and does nothing.)
	TabCompletion string
	// EmptySelectionClipboard makes a copy or a cut without a selection take the line.
	EmptySelectionClipboard bool
	// MatchBrackets is editor.matchBrackets: "always" highlights the brackets of the pair
	// around a cursor too, "near" only the match of a bracket next to it, "never" nothing.
	MatchBrackets string
	// BracketPairColorization colors the brackets by their nesting level
	// (editor.bracketPairColorization.enabled).
	BracketPairColorization bool
	// IndependentColorPool counts the levels for each type of bracket on its own
	// (editor.bracketPairColorization.independentColorPoolPerBracketType).
	IndependentColorPool bool
	// Guides are the options editor.guides.*.
	Guides indent.GuideOptions
	// AutoIndent is editor.autoIndent: "none", "keep", "brackets", "advanced" or "full".
	AutoIndent string
	// AutoIndentOnPaste is editor.autoIndentOnPaste.
	AutoIndentOnPaste bool
	// AutoIndentOnPasteWithinString is editor.autoIndentOnPasteWithinString.
	AutoIndentOnPasteWithinString bool
	// AutoClosingBrackets, AutoClosingComments and AutoClosingQuotes are editor.autoClosingBrackets,
	// editor.autoClosingComments and editor.autoClosingQuotes: "languageDefined", "beforeWhitespace",
	// "always" or "never" (SPEC §8.6.4).
	AutoClosingBrackets, AutoClosingComments, AutoClosingQuotes string
	// AutoClosingDelete and AutoClosingOvertype are editor.autoClosingDelete and
	// editor.autoClosingOvertype: "auto", "always" or "never".
	AutoClosingDelete, AutoClosingOvertype string
	// AutoSurround is editor.autoSurround: "languageDefined", "quotes", "brackets" or "never".
	AutoSurround string
	// CommentsInsertSpace and CommentsIgnoreEmptyLines are editor.comments.insertSpace and
	// editor.comments.ignoreEmptyLines.
	CommentsInsertSpace, CommentsIgnoreEmptyLines bool
	// WordSeparators is editor.wordSeparators; empty means VS Code's default.
	WordSeparators string
	// FindSeedSearchStringFromSelection is editor.find.seedSearchStringFromSelection ("never",
	// "always" or "selection"), FindAutoInSelection editor.find.autoFindInSelection ("never",
	// "always" or "multiline"), FindCloseOnResult editor.find.closeOnResult, FindCursorMoveOnType
	// editor.find.cursorMoveOnType and FindLoop editor.find.loop.
	FindSeedSearchStringFromSelection, FindAutoInSelection string
	FindCloseOnResult, FindCursorMoveOnType, FindLoop      bool
	// WordWrap is editor.wordWrap ("off", "on", "wordWrapColumn" or "bounded"), WordWrapColumn
	// editor.wordWrapColumn, WrappingIndent editor.wrappingIndent ("none", "same", "indent" or
	// "deepIndent"), WrappingStrategy editor.wrappingStrategy ("simple" or "advanced") and
	// StopRenderingLineAfter editor.stopRenderingLineAfter (negative: no limit).
	WordWrap                         string
	WordWrapColumn                   int
	WrappingIndent, WrappingStrategy string
	StopRenderingLineAfter           int
	// RenderWhitespace is editor.renderWhitespace ("none", "boundary", "selection", "trailing" or
	// "all"; empty is "none"), RenderControlCharacters editor.renderControlCharacters and Rulers
	// editor.rulers, sorted by column.
	RenderWhitespace        string
	RenderControlCharacters bool
	Rulers                  []ColumnRuler
	// OccurrencesHighlight is editor.occurrencesHighlight ("off", "singleFile" or "multiFile"),
	// OccurrencesHighlightDelay editor.occurrencesHighlightDelay in milliseconds, SelectionHighlight
	// editor.selectionHighlight, SelectionHighlightMaxLength editor.selectionHighlightMaxLength and
	// SelectionHighlightMultiline editor.selectionHighlightMultiline.
	OccurrencesHighlight        string
	OccurrencesHighlightDelay   int
	SelectionHighlight          bool
	SelectionHighlightMaxLength int
	SelectionHighlightMultiline bool
	// Minimap are the settings of the minimap (editor.minimap.*).
	Minimap MinimapOptions
	// StickyScroll are the settings of sticky scroll (editor.stickyScroll.*).
	StickyScroll StickyScrollOptions
}

// ColumnRuler is a vertical line drawn after a number of characters (editor.rulers).
type ColumnRuler struct {
	// Column is the number of characters from the left edge of the text.
	Column int
	// Color is the color of the line, or the transparent color when the theme's
	// editorRuler.foreground is used.
	Color ui.Color
}

// FontFeature is an OpenType feature of a font: a tag and its value (0 is off).
type FontFeature struct {
	Tag   string
	Value int
}

// gutterOptions are the parts of the options the width of the gutter depends on.
func (o Options) gutterOptions(digitWidth float64) GutterOptions {
	return GutterOptions{
		LineNumbers: o.LineNumbers, LineNumbersMinChars: o.LineNumbersMinChars, DigitWidth: digitWidth,
		LineHeight: o.LineHeight, GlyphMargin: o.GlyphMargin, LineDecorationsWidth: o.LineDecorationsWidth,
		Folding: o.Folding && o.ShowFoldingControls != "never",
	}
}

// ViewState is what the editor remembers about a document to come back to it: the
// selections, the scroll position and the folded ranges (SPEC §9.1).
type ViewState struct {
	// Selections are the selections, the primary first.
	Selections []buffer.Selection
	// ScrollTop is the vertical scroll position in pixels.
	ScrollTop float64
	// ScrollLeft is the horizontal scroll position in pixels.
	ScrollLeft float64
	// Folds are the folded and the user-made ranges, or nil.
	Folds folding.Memento
}

// ViewModel is the state of one editor over one document: the cursors, the scroll
// position, the projection from lines to rows and the cache of paragraphs. Several editors
// of one document each have a view model (SPEC §9.1). It is used on the UI thread only, and
// tells its listeners (render objects) when something that is drawn changed.
type ViewModel struct {
	foundation.ChangeNotifier
	doc        *document.Document
	cursors    *edit.Cursors
	proj       Projection
	opts       Options
	cache      *ParagraphCache
	syncedAt   int // the version of the document the projection was made for
	scrollTop  float64
	scrollLeft float64
	viewport   float64 // the height of the viewport, set by the render object

	// sel tells the layers that show the selections, the current line and the cursors that
	// they changed, without making the text layer lay out again.
	sel     *foundation.ChangeNotifier
	blink   *cursorBlink
	focused bool

	// history is where the cursors were, for cursor undo and redo.
	history edit.CursorHistory
	// sessionSelections are the selections the last command left; a command that goes on
	// with a session (column selection, occurrences, the expansion of a selection) checks
	// that nothing moved them since.
	sessionSelections []buffer.Selection
	// sessionOwner is the kind of session the command that runs belongs to.
	sessionOwner int
	columns      *edit.ColumnSelectData
	// mouseRange is the word or line a mouse selection extends from while the button is down.
	mouseRange buffer.Range
	// noReveal keeps a command from scrolling to the cursor, for what scrolls by itself.
	noReveal bool
	// keepSessions and skipReveal are set by a command that leaves the sessions of the column
	// selection, the occurrences and the expansion alone, and does not scroll (a copy).
	keepSessions, skipReveal bool
	// base are the options of the settings; opts, which the editor works with, are those at the
	// zoom level.
	base       Options
	zoom       *Zoom
	zoomHandle foundation.ListenerHandle
	// framer runs a function after every frame (the widget sets it).
	framer framer
	// animate starts the animation of the cursors (the widget sets it).
	animate func()
	// clip is the clipboard of the editor, once the widget gives it one.
	clip *clipboardLink
	// dropCaret is where dropped text would go, while text is dragged.
	dropCaret    buffer.Position
	hasDropCaret bool
	occurrences  *edit.OccurrenceSession
	expansion    *edit.Expansion
	// preedit is the text an input method is composing, or nil.
	preedit *Preedit
	// find are the options of the search that finds the occurrences of a selection.
	find edit.SearchOptions
	// scroller scrolls the editor to a vertical position (the widget sets it).
	scroller func(top float64)

	// tok is the tokenization of the document the text layer draws.
	tok tokenLink
	// bracketIndex are the bracket pairs of the document, or nil.
	bracketIndex *brackets.Index
	// autoClosing is the auto-closing and surrounding configuration of the language of the
	// document, or nil.
	autoClosing *autoclose.Language
	// comments gives the comment tokens of the languages, or nil.
	comments comment.Languages
	// ownerBase is the first of the document owners of this view model (see owner).
	ownerBase int
	// autoClosed tracks the closing characters the editor typed.
	autoClosed *edit.AutoClosedTracker
	// indenter applies the indentation rules of the languages, or is nil (SetIndenter), and
	// languageCode is the code of the language of the document.
	indenter     *indent.Indenter
	languageCode int
	// levels is the indentation of the lines, for the guides, or nil; offSide tells that the
	// language is indented to nest.
	levels  *indent.Levels
	offSide bool
	// fold is the folding of the document (SPEC §8.7).
	fold foldLink
	// find is the find model of the editor and what it is bound to (find.go).
	findw findLink
	// wrap is the word wrap of the editor (wordwrap.go).
	wrap wrapLink
	// snip is the snippet session of the editor and what it offers (snippets.go).
	snip snippetLink
	// hl holds the highlights of the occurrences of the word at the cursor and of the selection.
	hl highlightLink
	// mm is the minimap.
	mm minimapState
	// sticky is sticky scroll.
	sticky stickyState
	// afterTimer is the source of timers the editor widget gave.
	afterTimer TimerFunc
	// indentDefaults give editor.tabSize and editor.insertSpaces, for Detect Indentation.
	indentDefaults func() (tabSize int, insertSpaces bool)

	// metrics tells the scrollbars that the sizes of the text area or of the content changed.
	metrics *foundation.ChangeNotifier
	// The layout of the text layer fills these in (see SetTextMetrics).
	viewportW, scrollExtent, maxLineWidth, charWidth float64
}

// The decorations a view model puts on its document each have an owner of their own, because
// several view models can show one document (two editor groups, SPEC §10.3) and each must see
// and replace only its own marks. A view model takes ownerSlots consecutive owners from
// Document.AllocOwners; the constants named ...Owner are the slots (1 to ownerSlots) of the
// features in that range, and ViewModel.owner turns a slot into the owner.
const (
	ownerSlots   = 8
	snippetOwner = 6
)

// owner is the document owner of a slot of this view model.
func (vm *ViewModel) owner(slot int) int { return vm.ownerBase + slot - 1 }

// NewViewModel makes the view model of a document with the cursor at its start.
func NewViewModel(doc *document.Document, opts Options, cache *ParagraphCache) *ViewModel {
	if cache == nil {
		cache = NewParagraphCache(0)
	}
	eo := edit.DefaultOptions()
	eo.TabSize = opts.TabSize
	vm := &ViewModel{
		ChangeNotifier: *foundation.NewChangeNotifier(),
		doc:            doc,
		cursors:        edit.NewCursors(eo),
		opts:           opts,
		base:           opts,
		zoom:           NewZoom(),
		cache:          cache,
		syncedAt:       -1,
		sel:            foundation.NewChangeNotifier(),
		blink:          newCursorBlink(),
		focused:        true,
		metrics:        foundation.NewChangeNotifier(),
	}
	vm.zoomHandle = vm.zoom.AddListener(vm.zoomChanged)
	vm.ownerBase = doc.AllocOwners(ownerSlots)
	vm.autoClosed = edit.NewAutoClosedTracker(doc, vm.owner(autoClosedOwner))
	vm.cursors.SetOnChange(vm.cursorsChanged)
	vm.hl.seenVersion = doc.Version()
	vm.mm.changed = foundation.NewChangeNotifier()
	vm.sticky.changed, vm.sticky.line = foundation.NewChangeNotifier(), -1
	vm.Sync()
	vm.restartBlink()
	return vm
}

// Document is the document the editor shows.
func (vm *ViewModel) Document() *document.Document { return vm.doc }

// Cursors are the cursors of the editor.
func (vm *ViewModel) Cursors() *edit.Cursors { return vm.cursors }

// Options are the settings of the editor.
func (vm *ViewModel) Options() Options { return vm.opts }

// SetOptions changes the settings. A change of the line height makes a new projection; the
// paragraph cache needs no clearing, since its keys hold everything that changes a paragraph.
func (vm *ViewModel) SetOptions(opts Options) {
	vm.base = opts
	vm.applyOptions()
}

// applyOptions makes the options the editor works with, from the settings and the zoom, and
// lays the text out again when they changed.
func (vm *ViewModel) applyOptions() {
	opts := zoomed(vm.base, vm.zoom.Level())
	if sameOptions(vm.opts, opts) {
		return
	}
	vm.opts = opts
	vm.syncedAt = -1
	vm.maxLineWidth = 0 // the font may have changed: the rows measure it again
	vm.Sync()
	vm.restartBlink()
	vm.highlightsOptionsChanged()
	vm.NotifyListeners()
}

func sameOptions(a, b Options) bool { return reflect.DeepEqual(a, b) }

// DocumentChanged tells the view model that the document changed: the projection is made
// again and the layers are redrawn. The unchanged rows keep their paragraphs in the cache.
func (vm *ViewModel) DocumentChanged() {
	vm.maxLineWidth = 0 // the longest line may be gone: the rows measure it again
	vm.Sync()
	vm.restartBlink()
	vm.NotifyListeners()
	vm.sel.NotifyListeners()
}

// Paragraphs is the cache of paragraphs, shared by the layers of the editor.
func (vm *ViewModel) Paragraphs() *ParagraphCache { return vm.cache }

// Projection is the projection of lines to rows, brought up to date with the document.
func (vm *ViewModel) Projection() Projection {
	vm.Sync()
	return vm.proj
}

// Sync makes the projection match the document; it reports whether it changed.
func (vm *ViewModel) Sync() bool {
	version := vm.doc.Version()
	key := vm.wrapKeyNow()
	if version == vm.syncedAt && vm.fold.epoch == vm.fold.syncedEpoch && vm.proj != nil && !vm.wrapStale(key) {
		return false
	}
	vm.syncedAt, vm.fold.syncedEpoch = version, vm.fold.epoch
	vm.syncWrap(key)
	lines := vm.doc.Snapshot().LineCount()
	var folded *foldedProjection
	if hidden := vm.hiddenLines(); len(hidden) > 0 {
		folded = newFoldedProjection(lines, vm.opts.LineHeight, hidden)
	}
	vm.fold.hidden = folded
	switch {
	case vm.wrap.key.on:
		vm.proj = newWrappedProjection(lines, vm.opts.LineHeight, folded, vm.wrap.breaks)
	case folded != nil:
		vm.proj = folded
	default:
		vm.proj = newIdentityProjection(lines, vm.opts.LineHeight)
	}
	return true
}

// ScrollTop is the vertical scroll position in pixels.
func (vm *ViewModel) ScrollTop() float64 { return vm.scrollTop }

// ScrollLeft is the horizontal scroll position in pixels.
func (vm *ViewModel) ScrollLeft() float64 { return vm.scrollLeft }

// SetScrollTop sets the vertical scroll position; the render object follows it through the
// scrollable's offset, and the view model records what it was told.
func (vm *ViewModel) SetScrollTop(top float64) { vm.scrollTop = max(top, 0) }

// SetScrollLeft sets the horizontal scroll position, within the content once the text layer
// has measured it, and repaints.
func (vm *ViewModel) SetScrollLeft(left float64) {
	left = max(left, 0)
	if vm.viewportW > 0 {
		left = min(left, vm.MaxScrollLeft())
	}
	if left == vm.scrollLeft {
		return
	}
	vm.scrollLeft = left
	vm.NotifyListeners()
}

// SetViewportHeight records the height of the viewport, which sets the size of a page.
func (vm *ViewModel) SetViewportHeight(h float64) { vm.viewport = h }

// ViewState reads what to remember about the editor.
func (vm *ViewModel) ViewState() ViewState {
	return ViewState{Selections: vm.cursors.Selections(), ScrollTop: vm.scrollTop, ScrollLeft: vm.scrollLeft, Folds: vm.foldingMemento()}
}

// RestoreViewState puts the editor back as the state says: the folds are applied (when the
// ranges of the document are not known yet, as soon as they are), the selections are moved into
// the text, and the scroll position is used the next time the editor is laid out. A fold that
// cannot be restored is dropped; the error says why.
func (vm *ViewModel) RestoreViewState(s ViewState) error {
	var err error
	if len(s.Folds) > 0 {
		err = vm.restoreFolding(s.Folds)
	}
	if len(s.Selections) > 0 {
		model := edit.NewModel(vm.doc.Snapshot())
		sels := make([]buffer.Selection, len(s.Selections))
		for i, sel := range s.Selections {
			sels[i] = buffer.Selection{Anchor: model.ValidatePosition(sel.Anchor), Active: model.ValidatePosition(sel.Active)}
		}
		vm.cursors.SetSelections(sels)
	}
	vm.scrollTop, vm.scrollLeft = max(s.ScrollTop, 0), max(s.ScrollLeft, 0)
	vm.NotifyListeners()
	return err
}

// PageRows is the number of rows in a page: the viewport's rows less one, at least 1.
func (vm *ViewModel) pageRows() int {
	if vm.opts.LineHeight <= 0 || vm.viewport <= 0 {
		return edit.DefaultPageRows
	}
	return max(int(vm.viewport/vm.opts.LineHeight)-1, 1)
}

// Layout is the edit.LineLayout of the editor: how cursor movement sees the rows. Every line is
// one row, and the lines a fold hides have none.
func (vm *ViewModel) Layout() edit.LineLayout {
	l := edit.ModelLineLayout{Model: edit.NewModel(vm.doc.Snapshot()), TabSize: vm.opts.TabSize, Page: vm.pageRows()}
	vm.Sync()
	if vm.fold.hidden != nil {
		l.Hidden = vm.fold.hidden.hiddenSpan
	}
	if wp, ok := vm.proj.(*wrappedProjection); ok {
		l.Rows = wp
	}
	return l
}

// SelectionsChanged tells the view model that the cursors moved or the selections changed:
// the layers of the selections, the current line and the cursors are drawn again, and the
// cursors are shown and start blinking anew.
func (vm *ViewModel) SelectionsChanged() {
	vm.restartBlink()
	vm.sel.NotifyListeners()
}

// AddSelectionListener calls fn whenever the selections, the focus or the document change.
func (vm *ViewModel) AddSelectionListener(fn func()) foundation.ListenerHandle {
	return vm.sel.AddListener(fn)
}

// RemoveSelectionListener removes a listener of AddSelectionListener.
func (vm *ViewModel) RemoveSelectionListener(h foundation.ListenerHandle) { vm.sel.RemoveListener(h) }

// AddBlinkListener calls fn whenever the cursors are shown or hidden by the blinking.
func (vm *ViewModel) AddBlinkListener(fn func()) foundation.ListenerHandle {
	return vm.blink.AddListener(fn)
}

// RemoveBlinkListener removes a listener of AddBlinkListener.
func (vm *ViewModel) RemoveBlinkListener(h foundation.ListenerHandle) { vm.blink.RemoveListener(h) }

// Focused tells whether the editor has the focus: only then it has cursors that are shown,
// and its selections have the active color.
func (vm *ViewModel) Focused() bool { return vm.focused }

// SetFocused tells the view model that the editor got or lost the focus. Without the focus
// the cursors are hidden and no timer runs.
func (vm *ViewModel) SetFocused(focused bool) {
	if focused == vm.focused {
		return
	}
	vm.focused = focused
	vm.restartBlink()
	vm.sel.NotifyListeners()
}

// CursorsVisible tells whether the cursors are drawn now: the editor has the focus and the
// blinking is in its shown half.
func (vm *ViewModel) CursorsVisible() bool { return vm.blink.Visible() }

// SetTimer gives the view model the source of timers for the blinking (the editor widget
// passes the timers of the window's binding); nil stops the blinking.
func (vm *ViewModel) SetTimer(after TimerFunc) {
	vm.afterTimer = after
	vm.blink.SetTimer(after)
}

func (vm *ViewModel) restartBlink() {
	style := blinkStyleOf(vm.focused, vm.opts.CursorBlinking)
	vm.blink.Restart(style)
	if style.animated() && vm.animate != nil {
		vm.animate()
	}
}

// CursorFrame is how opaque the cursors are and how much of their height is left, in the
// animated styles of editor.cursorBlinking.
func (vm *ViewModel) CursorFrame() (opacity, scaleY float64) {
	f := vm.blink.Frame()
	return f.Opacity, f.ScaleY
}

// SetAnimator gives the view model the function that runs the animation of the cursors every
// frame; it is called whenever the animation begins again. The editor widget sets it.
func (vm *ViewModel) SetAnimator(start func()) { vm.animate = start }

// RowOfPosition is the row that shows a position of the document.
func (vm *ViewModel) RowOfPosition(p buffer.Position) int {
	proj := vm.Projection()
	first, count := proj.LineRows(p.Line)
	for row := first; row < first+count; row++ {
		_, start, end := proj.RowSpan(row)
		if p.Col >= start && (end < 0 || p.Col < end || row == first+count-1) {
			return row
		}
	}
	return first
}

// SetTextMetrics records what the layout of the text layer measured: the size of the text
// area, how far the content scrolls vertically, the width of the widest row it laid out and the
// width of a character. The widest row only grows until the document or the font changes. The
// listeners of the metrics (the scrollbars) are told when something changed.
func (vm *ViewModel) SetTextMetrics(viewportW, viewportH, scrollExtent, widestRow, charWidth float64) {
	widest := max(vm.maxLineWidth, widestRow)
	if viewportW == vm.viewportW && viewportH == vm.viewport && scrollExtent == vm.scrollExtent &&
		widest == vm.maxLineWidth && charWidth == vm.charWidth {
		return
	}
	wrapChanged := vm.wrapMode() != "off" && (viewportW != vm.viewportW || charWidth != vm.charWidth)
	vm.viewportW, vm.viewport, vm.scrollExtent, vm.maxLineWidth, vm.charWidth = viewportW, viewportH, scrollExtent, widest, charWidth
	vm.metrics.NotifyListeners()
	if wrapChanged {
		// the rows depend on the width: the layer is laid out again after this layout
		vm.post(vm.NotifyListeners)
	}
}

// ViewportSize is the size of the text area as the last layout measured it.
func (vm *ViewModel) ViewportSize() (width, height float64) { return vm.viewportW, vm.viewport }

// ContentWidth is the width of the content: the widest row laid out so far and room for a few
// more characters, or the text area when that is wider.
//
// Ported from VS Code: src/vs/editor/common/viewLayout/viewLayout.ts (scrollWidth, scrollBeyondLastColumn)
func (vm *ViewModel) ContentWidth() float64 {
	return max(vm.maxLineWidth+scrollBeyondLastColumn*vm.charWidth, vm.viewportW)
}

// ContentHeight is the height the content scrolls through: the text area plus how far it scrolls.
func (vm *ViewModel) ContentHeight() float64 { return vm.scrollExtent + vm.viewport }

// MaxScrollLeft is how far the content scrolls horizontally.
func (vm *ViewModel) MaxScrollLeft() float64 { return max(vm.ContentWidth()-vm.viewportW, 0) }

// AddMetricsListener calls fn when the sizes of the text area or of the content change. fn can
// be called during layout.
func (vm *ViewModel) AddMetricsListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return vm.metrics.AddListener(fn)
}

// RemoveMetricsListener removes a listener of AddMetricsListener.
func (vm *ViewModel) RemoveMetricsListener(h foundation.ListenerHandle) { vm.metrics.RemoveListener(h) }

// SetScroller gives the view model the function that scrolls the editor vertically; without
// it commands do not scroll.
func (vm *ViewModel) SetScroller(scroll func(top float64)) { vm.scroller = scroll }

// RevealPrimary scrolls the editor, if it has to, so that the row of the primary cursor is in
// view: the row goes to the nearest edge it left through.
func (vm *ViewModel) RevealPrimary() { vm.RevealPosition(vm.cursors.Primary().Position()) }

// revealPadding is how many rows of room a revealed position keeps from the top of the editor and
// how many from its bottom: with sticky scroll on, the lines that can stick at the top (a line less
// at the bottom), at most half the rows on the screen.
//
// Ported from VS Code: src/vs/editor/common/viewModel/viewModelImpl.ts (getViewRangeWithCursorPadding)
func (vm *ViewModel) revealPadding() (topRows, bottomRows int) {
	if !vm.opts.StickyScroll.Enabled || vm.opts.LineHeight <= 0 {
		return 0, 0
	}
	rows := int(vm.viewport / vm.opts.LineHeight)
	pad := min(vm.opts.StickyScroll.MaxLineCount, rows/2)
	return pad, max(pad-1, 0)
}

// RevealPosition scrolls the editor, if it has to, so that the row of a position is in view, and
// clear of the sticky lines.
func (vm *ViewModel) RevealPosition(p buffer.Position) {
	if vm.scroller == nil || vm.viewport <= 0 {
		return
	}
	proj := vm.Projection()
	row := vm.RowOfPosition(p)
	top, bottom := proj.RowTop(row), proj.RowTop(row)+proj.RowHeight(row)
	topRows, bottomRows := vm.revealPadding()
	topMargin, bottomMargin := float64(topRows)*vm.opts.LineHeight, float64(bottomRows)*vm.opts.LineHeight
	switch scroll := vm.scrollTop; {
	case top < scroll+topMargin:
		vm.scroller(max(top-topMargin, 0))
	case bottom > scroll+vm.viewport-bottomMargin:
		vm.scroller(bottom + bottomMargin - vm.viewport)
	}
}

// EmmetOptions are the settings of Emmet that the editor reads: the values of emmet.includeLanguages,
// emmet.excludeLanguages, emmet.variables and emmet.syntaxProfiles as the settings store holds them,
// and emmet.triggerExpansionOnTab.
type EmmetOptions struct {
	// TriggerExpansionOnTab makes the Tab key expand the abbreviation before the cursor.
	TriggerExpansionOnTab bool
	// IncludeLanguages, ExcludeLanguages, Variables and SyntaxProfiles are read by core/emmet.
	IncludeLanguages, ExcludeLanguages, Variables, SyntaxProfiles any
}
