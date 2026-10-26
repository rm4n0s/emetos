package editor

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/theme"
)

// Semantic highlighting (SPEC §9.12 "As built (step 12.9)"): the language server classifies the identifiers of the document
// (a type, a function, a parameter with its modifiers) and the editor draws them in the colors the theme gives to the
// classes, over the colors of the TextMate grammar. The tokens are asked for through the cache the editors of the window share
// (full and delta requests), a little after an edit; the tokens the editor holds follow the edits until the answer for the new
// text comes, so that typing does not make the colors flicker.
//
// Ported from VS Code: src/vs/editor/contrib/semanticTokens/browser/documentSemanticTokens.ts (the requests, their delay, the
// first view by range) and src/vs/editor/common/model/tokens/sparseTokensStore.ts (the tokens that follow the edits)

// semanticState is what a view model knows of the semantic tokens of its document.
type semanticState struct {
	// tokens are the tokens held: those of the last answer, moved with the edits since. has says there are some.
	tokens *langfeatures.SemanticTokens
	// full says that the tokens are those of the whole document (a range answer is held only until the first full one).
	full bool
	// gen numbers the requests; cancel ends the one in flight and cancelWait the wait before the next.
	gen        int
	cancel     context.CancelFunc
	cancelWait func()
	deb        *featureDebounce
	unsub      func()
	// theme is the token theme of the editor, which says whether the theme asks for semantic highlighting and how it styles
	// the tokens; styler is the semantic theme made from it with the rules of the settings.
	theme     *theme.TokenTheme
	styler    *theme.SemanticTheme
	stylerOf  *theme.SemanticTheme
	stylerFor map[string]any
	// styles remembers the style of each (type, modifiers) of the legend of the tokens held.
	styles map[uint64]semanticLook
	// edits counts the edits applied to the tokens, for the benchmark of the cost of a key.
	edits int
}

func newSemanticState() semanticState {
	return semanticState{deb: newFeatureDebounce(300*time.Millisecond, 2000*time.Millisecond)}
}

// semanticLook is how a token is drawn: whether it says anything, and the style.
type semanticLook struct {
	style theme.SemanticStyle
	color ui.Color
	ok    bool
}

// SetSemanticTheme gives the editor the token theme (the text layer sets it): its semantic part says whether the theme asks for
// semantic highlighting and how the classes are styled.
func (vm *ViewModel) SetSemanticTheme(t *theme.TokenTheme) {
	s := &vm.sem
	if s.theme == t {
		return
	}
	s.theme, s.styler, s.styles = t, nil, nil
	vm.semanticEnabledChanged()
}

// semanticTheme is the semantic theme: the theme's rules with those of editor.semanticTokenColorCustomizations on top.
func (vm *ViewModel) semanticTheme() *theme.SemanticTheme {
	s := &vm.sem
	if s.theme == nil || s.theme.Semantic() == nil {
		return nil
	}
	base := s.theme.Semantic()
	if s.styler == nil || s.stylerOf != base || !reflect.DeepEqual(s.stylerFor, vm.opts.Lens.SemanticRules) {
		s.stylerOf, s.stylerFor = base, vm.opts.Lens.SemanticRules
		s.styler = base
		if len(vm.opts.Lens.SemanticRules) > 0 {
			s.styler = base.WithRules(vm.opts.Lens.SemanticRules)
		}
		s.styles = nil
	}
	return s.styler
}

// SemanticHighlightingOn tells whether semantic highlighting is on: the setting, else the theme.
func (vm *ViewModel) SemanticHighlightingOn() bool {
	switch vm.opts.Lens.Semantic {
	case SemanticOn:
		return true
	case SemanticOff:
		return false
	}
	st := vm.semanticTheme()
	return st != nil && st.Enabled
}

// SemanticTokenCount is how many semantic tokens the editor holds.
func (vm *ViewModel) SemanticTokenCount() int {
	if vm.sem.tokens == nil {
		return 0
	}
	return len(vm.sem.tokens.Tokens)
}

// semanticCache is the cache of the window, or nil.
func (vm *ViewModel) semanticCache() *langfeatures.SemanticTokensCache {
	if h := vm.sg.host; h != nil {
		return h.Semantic
	}
	return nil
}

// semanticWanted tells whether tokens should be asked for.
func (vm *ViewModel) semanticWanted() bool {
	f := vm.hoverFeatures()
	return f != nil && vm.semanticCache() != nil && !vm.embedded && !vm.doc.LargeFile() && vm.SemanticHighlightingOn() &&
		f.Has(vm.langDocument(), langfeatures.FeatureSemanticTokens)
}

// semanticHostChanged starts or stops following the document when the host is set or what the providers offer changed.
func (vm *ViewModel) semanticHostChanged() {
	s := &vm.sem
	if vm.semanticCache() == nil || vm.hoverFeatures() == nil {
		vm.semanticStop()
		return
	}
	if s.unsub == nil {
		s.unsub = vm.doc.Subscribe(vm.semanticDocumentChanged)
	}
	vm.semanticSchedule(0)
}

// semanticEnabledChanged follows a change of the theme or of the settings that decides whether the tokens are used.
func (vm *ViewModel) semanticEnabledChanged() {
	s := &vm.sem
	if vm.semanticCache() == nil {
		return
	}
	if vm.SemanticHighlightingOn() {
		vm.semanticSchedule(0)
	} else if s.tokens != nil {
		vm.semanticCancel()
		s.tokens, s.full = nil, false
	}
	// SetSemanticTheme runs while the widgets are built (the render object takes the token theme then), when no listener may
	// mark itself as needing a build: the listeners hear of it after the frame.
	vm.post(vm.NotifyListeners)
}

// semanticStop stops following the document and drops the tokens.
func (vm *ViewModel) semanticStop() {
	s := &vm.sem
	if s.unsub != nil {
		s.unsub()
		s.unsub = nil
	}
	vm.semanticCancel()
	if s.tokens != nil {
		s.tokens, s.full = nil, false
		vm.NotifyListeners()
	}
}

// DisposeSemanticTokens stops following the document and drops the tokens.
func (vm *ViewModel) DisposeSemanticTokens() { vm.semanticStop() }

func (vm *ViewModel) semanticCancel() {
	s := &vm.sem
	s.gen++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.cancelWait != nil {
		s.cancelWait()
		s.cancelWait = nil
	}
}

// semanticSchedule asks for the tokens after a delay (at once for 0), replacing a request that is waiting.
func (vm *ViewModel) semanticSchedule(delay time.Duration) {
	s := &vm.sem
	if s.cancelWait != nil {
		s.cancelWait()
		s.cancelWait = nil
	}
	if delay <= 0 || vm.afterTimer == nil {
		vm.semanticRequest()
		return
	}
	s.cancelWait = vm.afterTimer(delay, func() {
		s.cancelWait = nil
		vm.semanticRequest()
	})
}

// semanticRequest asks the cache for the tokens of the document as it is now, off the UI thread. While the tokens of the whole
// document are not known (the first view of a document), the visible lines are asked for as a range at the same time, and
// drawn until the whole arrives.
func (vm *ViewModel) semanticRequest() {
	s := &vm.sem
	cache := vm.semanticCache()
	if cache == nil || !vm.semanticWanted() {
		return
	}
	doc := vm.langDocument()
	if s.cancel != nil {
		s.cancel()
	}
	s.gen++
	gen := s.gen
	base := context.Background()
	if h := vm.sg.host; h != nil && h.Ctx != nil {
		base = h.Ctx
	}
	ctx, cancel := context.WithCancel(base)
	s.cancel = cancel
	started := time.Now()
	deliver := func(t *langfeatures.SemanticTokens, err error, rangeOnly bool) {
		vm.post(func() {
			if vm.sem.gen != gen || vm.doc.Version() != doc.Version {
				return // superseded: a newer request, or the text changed
			}
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					if h := vm.sg.host; h != nil && h.Log != nil {
						h.Log("semantic tokens failed", err)
					}
				}
				return
			}
			if rangeOnly {
				if vm.sem.full {
					return // the whole came first
				}
			} else {
				vm.sem.cancel = nil
				cancel()
				vm.sem.deb.update(time.Since(started))
			}
			vm.semanticSet(t, !rangeOnly)
		})
	}
	if !s.full {
		rng := vm.semanticVisibleRange()
		go func() {
			t, err := cache.GetRange(ctx, doc, rng)
			if t != nil || err != nil {
				deliver(t, err, true)
			}
		}()
	}
	go func() {
		t, err := cache.Get(ctx, doc)
		deliver(t, err, false)
	}()
}

// semanticVisibleRange is the visible lines with a margin, for the range request of the first view.
func (vm *ViewModel) semanticVisibleRange() buffer.Range {
	proj := vm.Projection()
	top := vm.ScrollTop()
	_, height := vm.ViewportSize()
	first, last := proj.RowAt(top), proj.RowAt(top+height)
	rows := max(last-first, 1)
	firstLine, _, _ := proj.RowSpan(max(first-rows, 0))
	lastLine, _, _ := proj.RowSpan(min(last+rows, max(proj.RowCount()-1, 0)))
	return vm.doc.Snapshot().ValidateRange(buffer.Range{Start: buffer.Position{Line: firstLine}, End: buffer.Position{Line: lastLine + 1}})
}

// semanticSet takes an answer: the tokens are copied, so that the edits that follow move the editor's own.
func (vm *ViewModel) semanticSet(t *langfeatures.SemanticTokens, full bool) {
	s := &vm.sem
	if t == nil {
		if s.tokens != nil {
			s.tokens, s.full = nil, false
			vm.NotifyListeners()
		}
		return
	}
	own := *t
	own.Tokens = slices.Clone(t.Tokens)
	s.tokens, s.full, s.styles = &own, full, nil
	vm.NotifyListeners()
}

// semanticDocumentChanged follows an edit: the request in flight is for a text that is gone, the tokens held move with the
// text, and new ones are asked for after the delay the answers took.
func (vm *ViewModel) semanticDocumentChanged(ev document.ChangeEvent) {
	s := &vm.sem
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.gen++
	if s.tokens != nil {
		moveTokens(s.tokens, ev.Changes)
		s.edits++
	}
	if vm.semanticWanted() {
		vm.semanticSchedule(s.deb.delay())
	}
}

// moveTokens makes the tokens follow a batch of changes: a token before a change stays, one inside the replaced text or
// crossing its start is dropped, one after it moves with the text. The changes are in the coordinates of the text before the
// batch and in order of position, so they are applied from the last.
//
// Ported from VS Code: src/vs/editor/common/tokens/sparseMultilineTokens.ts (acceptEdit)
func moveTokens(t *langfeatures.SemanticTokens, changes []document.Change) {
	for i := len(changes) - 1; i >= 0; i-- {
		moveTokensBy(t, changes[i])
	}
}

func moveTokensBy(t *langfeatures.SemanticTokens, c document.Change) {
	oldStart, oldEnd := c.OldRange.Start, c.OldRange.End
	// where the end of the replaced text goes: the new text has newLines line breaks, and its last line lastLen bytes
	newLines := strings.Count(c.NewText, "\n")
	tailCol := oldStart.Col + len(c.NewText)
	if newLines > 0 {
		tailCol = len(c.NewText) - strings.LastIndexByte(c.NewText, '\n') - 1
	}
	toks := t.Tokens
	// the first token that does not end before the change
	from := sort.Search(len(toks), func(i int) bool {
		tk := toks[i]
		return tk.Line > oldStart.Line || tk.Line == oldStart.Line && tk.Col+tk.Len > oldStart.Col
	})
	out := toks[:from]
	for _, tk := range toks[from:] {
		start := buffer.Position{Line: tk.Line, Col: tk.Col}
		if start.IsBefore(oldEnd) {
			continue // inside the replaced text, or crossing its start: gone
		}
		if tk.Line == oldEnd.Line {
			tk.Col += tailCol - oldEnd.Col
		}
		tk.Line += oldStart.Line + newLines - oldEnd.Line
		out = append(out, tk)
	}
	t.Tokens = out
}

// semanticRefresh asks for the tokens again: the server said they are stale.
func (vm *ViewModel) semanticRefresh() {
	if vm.semanticWanted() {
		vm.semanticSchedule(0)
	}
}

// semanticLookOf is how a token is drawn: the style the theme gives to its type and modifiers. The result is remembered for the
// legend of the tokens held.
func (vm *ViewModel) semanticLookOf(t *langfeatures.SemanticTokens, tk langfeatures.SemanticToken) semanticLook {
	s := &vm.sem
	st := vm.semanticTheme()
	if st == nil {
		return semanticLook{}
	}
	key := uint64(uint32(tk.Type))<<32 | uint64(tk.Modifiers)
	if look, ok := s.styles[key]; ok {
		return look
	}
	if s.styles == nil {
		s.styles = map[uint64]semanticLook{}
	}
	var look semanticLook
	if name := t.TypeName(tk); name != "" {
		if style, ok := st.Style(name, t.ModifierNames(tk), vm.doc.LanguageID()); ok {
			look = semanticLook{style: style, ok: true}
			if c, ok := theme.ParseColor(style.Foreground); ok {
				look.color = ui.NewColor(c.ARGB())
			}
		}
	}
	s.styles[key] = look
	return look
}

// semanticSpans are the spans of the semantic tokens of the bytes [start, end) of a line, relative to start, in the order of the
// text; nil when there are none or the highlighting is off. A span with no color of its own (the theme sets a font style only)
// has the zero color.
func (vm *ViewModel) semanticSpans(line, start, end int) []semanticSpan {
	t := vm.sem.tokens
	if t == nil || !vm.SemanticHighlightingOn() {
		return nil
	}
	toks := t.OnLine(line)
	if len(toks) == 0 {
		return nil
	}
	var out []semanticSpan
	for _, tk := range toks {
		if tk.Col+tk.Len <= start || tk.Col >= end {
			continue
		}
		look := vm.semanticLookOf(t, tk)
		if !look.ok {
			continue
		}
		s, e := max(tk.Col, start), min(tk.Col+tk.Len, end)
		out = append(out, semanticSpan{start: s - start, end: e - start, color: look.color, hasColor: look.color.Alpha() != 0, style: look.style})
	}
	return out
}

// semanticSpan is a span of text that a semantic token styles: the color when the theme gives one, and the font styles it sets.
type semanticSpan struct {
	start, end int
	color      ui.Color
	hasColor   bool
	style      theme.SemanticStyle
}

// mergeSemantic draws the semantic spans on top of the spans of the TextMate tokens: where they meet, the color is the semantic
// one when it has one (the TextMate one stays when it does not), and the font style is the TextMate one with the flags the
// semantic style sets changed. base is the color of the text no span covers.
//
// Ported from VS Code: src/vs/editor/common/model/tokens/tokenizationTextModelPart.ts and
// src/vs/editor/common/tokens/lineTokens.ts (the metadata of a semantic token on top of the grammar's, per property)
func mergeSemantic(textmate []span, semantic []semanticSpan, base ui.Color) []span {
	if len(semantic) == 0 {
		return textmate
	}
	var out []span
	// pieces adds the textmate spans (or the base style where none covers) between from and to
	pieces := func(from, to int) {
		at := from
		for _, b := range textmate {
			if b.end <= from || b.start >= to {
				continue
			}
			if s := max(b.start, from); s > at {
				out = append(out, span{start: at, end: s, color: base})
			}
			s, e := max(b.start, from), min(b.end, to)
			out = append(out, span{start: s, end: e, color: b.color, style: b.style})
			at = e
		}
		if at < to {
			out = append(out, span{start: at, end: to, color: base})
		}
	}
	at := 0
	for _, sm := range semantic {
		if sm.start < at {
			continue
		}
		if sm.start > at {
			pieces(at, sm.start)
		}
		from := len(out)
		pieces(sm.start, sm.end)
		for i := from; i < len(out); i++ {
			if sm.hasColor {
				out[i].color = sm.color
			}
			out[i].style = sm.style.Apply(out[i].style)
		}
		at = sm.end
	}
	last := at
	for _, b := range textmate {
		last = max(last, b.end)
	}
	if last > at {
		pieces(at, last)
	}
	return out
}
