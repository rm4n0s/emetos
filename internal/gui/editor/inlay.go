package editor

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// Inlay hints (SPEC §9.12 "As built (step 12.9)"): labels the language server wants shown between characters of the text. They
// are injected into the rows the editor draws and measures, and nowhere else: the document, the cursors, the clipboard and the
// search never see them, and a cursor moves over the characters of the document, so it skips the hints.
//
// Ported from VS Code: src/vs/editor/contrib/inlayHints/browser/inlayHintsController.ts (the requests for the visible ranges,
// the render modes, the padding and the truncation of the labels)

// InlayColors are the colors of the inlay hints: editorInlayHint.*.
type InlayColors struct {
	Foreground, Background                   ui.Color
	TypeForeground, TypeBackground           ui.Color
	ParameterForeground, ParameterBackground ui.Color
}

// colors are the foreground and the background of a hint of a kind.
func (c InlayColors) colors(k langfeatures.InlayHintKind) (fg, bg ui.Color) {
	switch k {
	case langfeatures.InlayHintType:
		return c.TypeForeground, c.TypeBackground
	case langfeatures.InlayHintParameter:
		return c.ParameterForeground, c.ParameterBackground
	}
	return c.Foreground, c.Background
}

// inlayExtraLines is how many lines beyond the viewport, above and below, the hints are asked for (VS Code asks for the
// viewport above and below the visible lines, and thirty lines more).
const inlayExtraLines = 30

// inlayEntry is a hint that is held: the decoration that follows it through the edits and the hint itself.
type inlayEntry struct {
	id   int
	hint langfeatures.InlayHint
}

// injection is a hint as a row shows it: the text between two characters of the line.
type injection struct {
	// col is the byte column of the line the hint is before.
	col int
	// text is what is drawn: the margins, the padding and the label. The background is drawn behind text[bgFrom:bgTo].
	text         string
	bgFrom, bgTo int
	// parts are the label parts, as byte ranges of text.
	parts []injectionPart
	entry *inlayEntry
	// fg and bg are the colors; size and family the font of the text (0 and empty: the editor's).
	fg, bg ui.Color
	size   float64
	family string
	// lineHeight is the height of the row the text is in.
	lineHeight float64
}

// injectionPart is one label part of an injection.
type injectionPart struct {
	from, to int
	index    int
}

// inlayState is what a view model knows of the inlay hints of its document.
type inlayState struct {
	entries map[int]*inlayEntry
	ids     []int
	// gen numbers the requests; cancel ends the one in flight and cancelWait the wait before the next.
	gen        int
	cancel     context.CancelFunc
	cancelWait func()
	deb        *featureDebounce
	unsub      func()
	scrollOn   bool
	scrollH    foundation.ListenerHandle
	// pressed says that Ctrl+Alt are held (and nothing else of Shift and Meta), which switches the modes "...UnlessPressed".
	pressed bool
	// epoch changes when the injections would: the hints, the mode, the options. The lines are worked out once for an epoch and a
	// version of the document.
	epoch      int
	cacheEpoch int
	cacheVer   int
	cache      map[int][]*injection
}

func newInlayState() inlayState {
	return inlayState{entries: map[int]*inlayEntry{}, deb: newFeatureDebounce(25*time.Millisecond, 2500*time.Millisecond), cacheVer: -1, cacheEpoch: -1}
}

// InlayHintsShown tells whether the inlay hints are drawn now: the mode of editor.inlayHints.enabled, and for the modes that
// depend on the keys, whether Ctrl+Alt are held.
func (vm *ViewModel) InlayHintsShown() bool {
	switch vm.opts.Lens.inlayMode() {
	case inlayOn:
		return true
	case inlayOnUnlessPressed:
		return !vm.inlay.pressed
	case inlayOffUnlessPressed:
		return vm.inlay.pressed
	}
	return false
}

// InlayHintCount is how many inlay hints the editor holds, shown or not.
func (vm *ViewModel) InlayHintCount() int { return len(vm.inlay.entries) }

// InlayModifiersChanged tells the editor which modifier keys are held. The modes onUnlessPressed and offUnlessPressed follow
// Ctrl+Alt (without Shift and Meta).
//
// Ported from VS Code: inlayHintsController.ts (ModifierKeyEmitter: e.altKey && e.ctrlKey && !(e.shiftKey || e.metaKey))
func (vm *ViewModel) InlayModifiersChanged(m MouseModifiers) {
	pressed := m.Ctrl && m.Alt && !m.Shift && !m.Meta
	s := &vm.inlay
	if s.pressed == pressed {
		return
	}
	s.pressed = pressed
	switch vm.opts.Lens.inlayMode() {
	case inlayOnUnlessPressed, inlayOffUnlessPressed:
		s.epoch++
		vm.inlayRelayout()
		if vm.InlayHintsShown() {
			vm.inlaySchedule(0)
		}
	}
}

// inlayRelayout lays the text out again for hints that appeared, went or changed.
func (vm *ViewModel) inlayRelayout() {
	vm.maxLineWidth = 0
	vm.NotifyListeners()
	vm.sel.NotifyListeners()
}

// inlayWanted tells whether hints should be asked for: the editor has the means, a provider offers them and the mode shows them.
func (vm *ViewModel) inlayWanted() bool {
	f := vm.hoverFeatures()
	return f != nil && !vm.embedded && !vm.doc.LargeFile() && vm.InlayHintsShown() && f.Has(vm.langDocument(), langfeatures.FeatureInlayHints)
}

// inlayHostChanged starts or stops following the document when the host is set or what the providers offer changed.
func (vm *ViewModel) inlayHostChanged() {
	s := &vm.inlay
	if vm.hoverFeatures() == nil {
		vm.inlayStop()
		return
	}
	if s.unsub == nil {
		s.unsub = vm.doc.Subscribe(func(document.ChangeEvent) { vm.inlayDocumentChanged() })
	}
	if !s.scrollOn {
		s.scrollH, s.scrollOn = vm.AddScrollListener(vm.inlayScrolled), true
	}
	vm.inlaySchedule(0)
}

// inlayStop stops following the document and the scroll, cancels what is pending and takes the hints away.
func (vm *ViewModel) inlayStop() {
	s := &vm.inlay
	if s.unsub != nil {
		s.unsub()
		s.unsub = nil
	}
	if s.scrollOn {
		vm.RemoveScrollListener(s.scrollH)
		s.scrollOn = false
	}
	vm.inlayCancel()
	vm.inlayClear()
}

// DisposeInlayHints stops following the document and takes the hints away.
func (vm *ViewModel) DisposeInlayHints() { vm.inlayStop() }

// inlayCancel ends the request in flight and the wait.
func (vm *ViewModel) inlayCancel() {
	s := &vm.inlay
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

// inlayClear removes every hint.
func (vm *ViewModel) inlayClear() {
	s := &vm.inlay
	if len(s.ids) == 0 {
		return
	}
	vm.doc.DeltaDecorations(vm.owner(inlayOwner), s.ids, nil)
	s.ids, s.entries = nil, map[int]*inlayEntry{}
	s.epoch++
	vm.inlayRelayout()
}

// inlayDocumentChanged follows an edit: the request in flight is for a text that is gone, and a new one comes after the delay
// the answers took.
func (vm *ViewModel) inlayDocumentChanged() {
	s := &vm.inlay
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.gen++
	if len(s.ids) > 0 {
		s.epoch++ // the hints moved with the text
	}
	if vm.inlayWanted() {
		vm.inlaySchedule(s.deb.delay())
	}
}

// inlayScrolled asks for the hints of the new lines.
func (vm *ViewModel) inlayScrolled() {
	if vm.inlayWanted() && vm.inlay.cancelWait == nil {
		vm.inlaySchedule(vm.inlay.deb.delay())
	}
}

// inlayOptionsChanged follows a change of the settings: the injections are made again and the hints asked for as the mode
// says.
func (vm *ViewModel) inlayOptionsChanged() {
	s := &vm.inlay
	s.epoch++
	if vm.hoverFeatures() == nil {
		return
	}
	if !vm.InlayHintsShown() || vm.opts.Lens.inlayMode() == inlayOff {
		vm.inlayCancel()
		vm.inlayRelayout()
		return
	}
	vm.inlaySchedule(s.deb.delay())
	vm.inlayRelayout()
}

// inlaySchedule asks for the hints after a delay (at once for 0), replacing a request that is waiting.
func (vm *ViewModel) inlaySchedule(delay time.Duration) {
	s := &vm.inlay
	if s.cancelWait != nil {
		s.cancelWait()
		s.cancelWait = nil
	}
	if delay <= 0 || vm.afterTimer == nil {
		vm.inlayRequest()
		return
	}
	s.cancelWait = vm.afterTimer(delay, func() {
		s.cancelWait = nil
		vm.inlayRequest()
	})
}

// inlayRange is the range the hints are asked for: the visible lines, the height of the viewport above and below, and thirty
// lines more.
func (vm *ViewModel) inlayRange() buffer.Range {
	proj := vm.Projection()
	top := vm.ScrollTop()
	_, height := vm.ViewportSize()
	first, last := proj.RowAt(top), proj.RowAt(top+height)
	rows := max(last-first, 1)
	firstLine, _, _ := proj.RowSpan(max(first-rows, 0))
	lastLine, _, _ := proj.RowSpan(min(last+rows, max(proj.RowCount()-1, 0)))
	snap := vm.doc.Snapshot()
	r := buffer.Range{Start: buffer.Position{Line: max(firstLine-inlayExtraLines, 0)}, End: buffer.Position{Line: lastLine + inlayExtraLines + 1}}
	return snap.ValidateRange(r)
}

// inlayRequest asks the providers for the hints of the range, off the UI thread. A request that is in flight is cancelled; an
// answer for a document that changed, or that was asked for before another request began, is dropped.
func (vm *ViewModel) inlayRequest() {
	s := &vm.inlay
	f := vm.hoverFeatures()
	if f == nil {
		return
	}
	doc := vm.langDocument()
	if !vm.inlayWanted() {
		return
	}
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
	rng := vm.inlayRange()
	started := time.Now()
	go func() {
		hints, errs := f.InlayHints(ctx, doc, rng)
		vm.post(func() {
			if vm.inlay.gen != gen || vm.doc.Version() != doc.Version {
				return // superseded: a newer request, or the text changed
			}
			vm.inlay.cancel = nil
			cancel()
			vm.inlay.deb.update(time.Since(started))
			if h := vm.sg.host; h != nil && h.Log != nil {
				for _, e := range errs {
					if !errors.Is(e.Err, context.Canceled) {
						h.Log("inlay hint provider "+e.Owner+" failed", e.Err)
					}
				}
			}
			if len(errs) > 0 && len(hints) == 0 {
				return // a failure keeps what is shown
			}
			vm.inlaySet(rng, hints)
		})
	}()
}

// inlaySet replaces the hints inside a range by the ones the providers gave for it; the hints outside stay.
func (vm *ViewModel) inlaySet(rng buffer.Range, hints []langfeatures.InlayHint) {
	s := &vm.inlay
	snap := vm.doc.Snapshot()
	var remove []int
	keep := s.ids[:0:0]
	for _, id := range s.ids {
		d, ok := vm.doc.Decoration(id)
		if ok && !rng.ContainsPosition(d.Range.Start) {
			keep = append(keep, id)
			continue
		}
		remove = append(remove, id)
		delete(s.entries, id)
	}
	add := make([]document.NewDecoration, 0, len(hints))
	var kept []langfeatures.InlayHint
	for _, h := range hints {
		p := snap.ValidatePosition(h.Position)
		if !rng.ContainsPosition(p) {
			continue
		}
		h.Position = p
		add = append(add, document.NewDecoration{Range: buffer.Range{Start: p, End: p}, Options: document.DecorationOptions{
			Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: "inlayHint",
		}})
		kept = append(kept, h)
	}
	ids := vm.doc.DeltaDecorations(vm.owner(inlayOwner), remove, add)
	for i, id := range ids {
		s.entries[id] = &inlayEntry{id: id, hint: kept[i]}
	}
	s.ids = append(keep, ids...)
	// the hints of the ranges are in the order of their positions
	slices.SortStableFunc(s.ids, func(a, b int) int {
		da, _ := vm.doc.Decoration(a)
		db, _ := vm.doc.Decoration(b)
		if da.Range.Start.Line != db.Range.Start.Line {
			return da.Range.Start.Line - db.Range.Start.Line
		}
		return da.Range.Start.Col - db.Range.Start.Col
	})
	s.epoch++
	vm.inlayRelayout()
}

// inlayFont is the font size and family of the text of the hints: editor.inlayHints.fontSize (the editor's 90% when it says
// nothing sensible) and editor.inlayHints.fontFamily.
func (vm *ViewModel) inlayFont() (size float64, family string) {
	size = vm.opts.Lens.InlayFontSize
	if size < 5 || size > vm.opts.FontSize {
		size = math.Max(math.Round(vm.opts.FontSize*0.9), 5)
	}
	if size == vm.opts.FontSize {
		size = 0
	}
	return size, vm.opts.Lens.InlayFontFamily
}

// InlayColorsSet gives the editor the colors of the hints (the widget sets them).
func (vm *ViewModel) InlayColorsSet(c InlayColors) {
	if vm.inlayColors == c {
		return
	}
	vm.inlayColors = c
	vm.inlay.epoch++
}

// lineInjections are the hints of a line as the rows show them, ordered by column; none while the hints are not shown. The
// result is kept for the epoch of the hints and the version of the document.
func (vm *ViewModel) lineInjections(line int) []*injection {
	s := &vm.inlay
	if len(s.ids) == 0 || !vm.InlayHintsShown() {
		return nil
	}
	if s.cacheEpoch != s.epoch || s.cacheVer != vm.doc.Version() || s.cache == nil {
		s.cache, s.cacheEpoch, s.cacheVer = map[int][]*injection{}, s.epoch, vm.doc.Version()
	}
	if inj, ok := s.cache[line]; ok {
		return inj
	}
	inj := vm.makeInjections(line)
	s.cache[line] = inj
	return inj
}

// makeInjections works out the hints of a line: their text with the padding, cut to editor.inlayHints.maximumLength for the
// line.
func (vm *ViewModel) makeInjections(line int) []*injection {
	s := &vm.inlay
	span := buffer.Range{Start: buffer.Position{Line: line}, End: buffer.Position{Line: line + 1}}
	decos := vm.doc.DecorationsIn(span, vm.owner(inlayOwner))
	var out []*injection
	size, family := vm.inlayFont()
	budget := vm.opts.Lens.InlayMaxLength
	for _, d := range decos {
		e := s.entries[d.ID]
		if e == nil || d.Range.Start.Line != line {
			continue
		}
		label := e.hint.Parts
		var text strings.Builder
		var parts []injectionPart
		inner := ""
		if vm.opts.Lens.InlayPadding {
			inner = " "
		}
		margin := func(on bool) string {
			if on {
				return " "
			}
			return ""
		}
		text.WriteString(margin(e.hint.PaddingLeft))
		bgFrom := text.Len()
		text.WriteString(inner)
		stop := false
		for i, p := range label {
			value := strings.ReplaceAll(strings.ReplaceAll(p.Value, "\n", " "), "\t", " ")
			if budget > 0 {
				n := utf8.RuneCountInString(value)
				switch {
				case n <= budget:
					budget -= n
				case budget > 1:
					value = string([]rune(value)[:budget-1]) + "…"
					budget, stop = 0, true
				default:
					value, budget, stop = "…", 0, true
				}
			}
			from := text.Len()
			text.WriteString(value)
			parts = append(parts, injectionPart{from: from, to: text.Len(), index: i})
			if stop {
				break
			}
		}
		text.WriteString(inner)
		bgTo := text.Len()
		text.WriteString(margin(e.hint.PaddingRight))
		fg, bg := vm.inlayColors.colors(e.hint.Kind)
		out = append(out, &injection{
			col: d.Range.Start.Col, text: text.String(), bgFrom: bgFrom, bgTo: bgTo, parts: parts, entry: e,
			fg: fg, bg: bg, size: size, family: family, lineHeight: vm.opts.LineHeight,
		})
		if budget == 0 && vm.opts.Lens.InlayMaxLength > 0 && stop {
			// the line has no room for more hints
			break
		}
	}
	return out
}

// rowInjections are the hints that a row of a line shows: those before a character of the row, and, on the last row of the
// line, those at its end. start and end are the bytes of the line the row has (end negative for the end of the line) and shown
// the length of the part that is drawn (editor.stopRenderingLineAfter).
func (vm *ViewModel) rowInjections(line, start, end, shown int, lastRow bool) []*injection {
	all := vm.lineInjections(line)
	if len(all) == 0 {
		return nil
	}
	var out []*injection
	for _, inj := range all {
		rel := inj.col - start
		if rel < 0 || rel > shown {
			continue
		}
		if end >= 0 && inj.col >= end && (!lastRow || inj.col != end) {
			continue
		}
		out = append(out, inj)
	}
	return out
}

// injectedRow is the text of a row with the hints in it, and where each hint is.
type injectedRow struct {
	// text is the row as drawn.
	text string
	// at are the byte offsets of text where each hint starts, rel the byte offsets of the model text of the row that each hint is
	// before.
	injs []*injection
	at   []int
	rel  []int
}

// inject puts the hints in the text of a row (start is the byte of the line the row starts at).
func inject(text string, start int, injs []*injection) injectedRow {
	if len(injs) == 0 {
		return injectedRow{text: text}
	}
	var b strings.Builder
	r := injectedRow{injs: injs, at: make([]int, len(injs)), rel: make([]int, len(injs))}
	prev := 0
	for i, inj := range injs {
		rel := min(max(inj.col-start, prev), len(text))
		b.WriteString(text[prev:rel])
		r.at[i], r.rel[i] = b.Len(), rel
		b.WriteString(inj.text)
		prev = rel
	}
	b.WriteString(text[prev:])
	r.text = b.String()
	return r
}

// viewOffset is the offset of the drawn text for an offset of the model text of the row: before the hints at that offset, after
// those before it.
func (r injectedRow) viewOffset(rel int) int {
	n := rel
	for i, inj := range r.injs {
		if r.rel[i] < rel {
			n += len(inj.text)
		}
	}
	return n
}

// modelOffset is the offset of the model text of the row for an offset of the drawn text: inside a hint, the place it is before.
func (r injectedRow) modelOffset(v int) int {
	cum := 0
	for i, inj := range r.injs {
		if v <= r.at[i] {
			break
		}
		if v < r.at[i]+len(inj.text) {
			return r.rel[i]
		}
		cum += len(inj.text)
	}
	return v - cum
}

// spans are the spans of the drawn text: the spans of the model text moved past the hints (split where a hint is in them), and
// the text of each hint in the color and font of the hint.
func (r injectedRow) spans(model []span) []span {
	if len(r.injs) == 0 {
		return model
	}
	out := make([]span, 0, len(model)+2*len(r.injs))
	// shift is the length of the hints before an offset of the model text
	shiftBefore := func(rel int, inclusive bool) int {
		n := 0
		for i, inj := range r.injs {
			if r.rel[i] < rel || inclusive && r.rel[i] == rel {
				n += len(inj.text)
			}
		}
		return n
	}
	for _, sp := range model {
		from := sp.start
		for i := range r.injs {
			c := r.rel[i]
			if c > from && c < sp.end {
				out = append(out, moved(sp, from, c, shiftBefore(from, true), shiftBefore(c, false)))
				from = c
			}
		}
		out = append(out, moved(sp, from, sp.end, shiftBefore(from, true), shiftBefore(sp.end, false)))
	}
	for i, inj := range r.injs {
		out = append(out, span{start: r.at[i], end: r.at[i] + len(inj.text), color: inj.fg, size: inj.size, family: inj.family, lineHeight: inj.lineHeight})
	}
	slices.SortStableFunc(out, func(a, b span) int { return a.start - b.start })
	return out
}

// moved is the part [from, to) of a span of the model text, in the drawn text: the offsets move by the hints before them.
func moved(sp span, from, to, shiftFrom, shiftTo int) span {
	sp.start, sp.end = from+shiftFrom, to+shiftTo
	return sp
}

// hintSpans are the spans of the hints alone, for measuring.
func (r injectedRow) hintSpans() []span { return r.spans(nil) }
