package editor

import (
	"math"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/wrap"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Ported from VS Code: src/vs/editor/common/viewModel/viewModelLines.ts and
// src/vs/editor/common/viewModel/modelLineProjection.ts (the lines of a view split by word wrap)

// wrapSliceLines is the most lines one step of the computation of the breaks takes, and
// wrapSliceTime the longest: the rest waits for the next step, so that the editor stays quick
// while the breaks of a long document are made.
const (
	wrapSliceLines = 3000
	wrapSliceTime  = 4 * time.Millisecond
	// wrapWindow is how many lines around the top of the editor are broken before the others.
	wrapWindow = 80
)

// wrapKey is everything that the breaks of the lines depend on: when it changes, they are all
// made again. The zero value is no wrapping.
type wrapKey struct {
	on                  bool
	columns             int
	charWidth           float64
	indent              wrap.Indent
	tabSize             int
	advanced, monospace bool
	stopAfter           int
}

// wrapLink is the word wrap of a view model: the breaks of the lines, which are made in steps.
type wrapLink struct {
	key wrapKey
	// breaks and known are kept for every line while wrapping is on: nil breaks is a line of one
	// row, and a line that is not known is shown as one row until it is.
	breaks  []*wrap.Breaks
	known   []bool
	pending int
	// cursor is where the steps go on from.
	cursor int
	// dirty are the lines the last edit made, which are broken at once.
	dirty []int
	// epoch counts the changes of the breaks; the projection was made for syncedEpoch.
	epoch, syncedEpoch int
	unsubscribe        func()
	cancelStep         func()
	// factory is the text engine that lines are broken with when they cannot be by counting
	// columns; without one (a test) the columns are counted for every line. monospace says
	// whether the font has one width for every character.
	factory   ui.ParagraphFactory
	monospace bool
	// override is the mode a command that toggles word wrap chose: "" for the setting's.
	override string
}

// SetTextEngine gives the view model the text engine that breaks the lines whose characters do
// not all have the width of a column (SPEC §9.2), and says whether the font is monospace. The
// editor widget does it after every layout.
func (vm *ViewModel) SetTextEngine(factory ui.ParagraphFactory, monospace bool) {
	vm.wrap.factory = factory
	vm.wrap.monospace = monospace
}

// wrapMode is "off", "on", "wordWrapColumn" or "bounded": the setting, or what the command that
// toggles word wrap chose.
func (vm *ViewModel) wrapMode() string {
	if vm.wrap.override != "" {
		return vm.wrap.override
	}
	if vm.opts.WordWrap == "" {
		return "off"
	}
	return vm.opts.WordWrap
}

// wrapKeyNow is the key of the wrapping the editor has now: none for a document in large file
// mode, before the text layer has measured the editor, and for the mode "off".
func (vm *ViewModel) wrapKeyNow() wrapKey {
	mode := vm.wrapMode()
	if (mode != "on" && mode != "wordWrapColumn" && mode != "bounded") || vm.doc.LargeFile() || vm.charWidth <= 0 {
		return wrapKey{}
	}
	viewColumns := 0
	if vm.viewportW > 0 {
		viewColumns = max(int(math.Floor((vm.viewportW-kit.DefaultVerticalScrollbarSize)/vm.charWidth+1e-6)), 1)
	}
	var columns int
	switch mode {
	case "on":
		columns = viewColumns
	case "wordWrapColumn":
		columns = max(vm.opts.WordWrapColumn, 1)
	default: // bounded
		columns = max(vm.opts.WordWrapColumn, 1)
		if viewColumns > 0 {
			columns = min(columns, viewColumns)
		}
	}
	if columns == 0 {
		return wrapKey{}
	}
	return wrapKey{
		on: true, columns: columns, charWidth: vm.charWidth, indent: wrap.ParseIndent(vm.opts.WrappingIndent),
		tabSize: max(vm.opts.TabSize, 1), advanced: vm.opts.WrappingStrategy == "advanced",
		monospace: vm.wrap.factory == nil || vm.wrap.monospace, stopAfter: vm.opts.StopRenderingLineAfter,
	}
}

// Wrapping says whether the lines are split into rows.
func (vm *ViewModel) Wrapping() bool {
	vm.Sync()
	return vm.wrap.key.on
}

// SetWordWrapOverride makes the editor wrap with a mode of editor.wordWrap whatever the setting
// says; "" goes back to the setting.
func (vm *ViewModel) SetWordWrapOverride(mode string) {
	if vm.wrap.override == mode {
		return
	}
	vm.wrap.override = mode
	vm.NotifyListeners()
}

// WrapColumn is the number of columns of a row now, or 0 when the lines are not wrapped.
func (vm *ViewModel) WrapColumn() int {
	vm.Sync()
	return vm.wrap.key.columns
}

// wrapStale tells whether the breaks are not those of the key.
func (vm *ViewModel) wrapStale(key wrapKey) bool {
	return key != vm.wrap.key || vm.wrap.epoch != vm.wrap.syncedEpoch
}

// anchor is a place of the text that stays where it is on the screen while the rows above it
// change in number.
type anchor struct {
	line, row int
	// offset is how far the top of the editor is below the top of the row.
	offset float64
	valid  bool
}

// captureAnchor is the place of the text at the top of the editor.
func (vm *ViewModel) captureAnchor() anchor {
	if vm.proj == nil {
		return anchor{}
	}
	row := vm.proj.RowAt(vm.scrollTop)
	line, _, _ := vm.proj.RowSpan(row)
	first, _ := vm.proj.LineRows(line)
	return anchor{line: line, row: row - first, offset: vm.scrollTop - vm.proj.RowTop(row), valid: true}
}

// restoreAnchor scrolls so that the place of the text is at the top again.
func (vm *ViewModel) restoreAnchor(a anchor) {
	if !a.valid || vm.scroller == nil {
		return
	}
	vm.Sync()
	first, count := vm.proj.LineRows(a.line)
	row := first + min(a.row, max(count-1, 0))
	if top := vm.proj.RowTop(row) + a.offset; math.Abs(top-vm.scrollTop) > 0.5 {
		vm.scroller(top)
	}
}

// post runs a function on the UI thread soon, or now when the view model has no means to.
func (vm *ViewModel) post(fn func()) {
	if post := vm.poster(); post != nil {
		post(fn)
		return
	}
	fn()
}

// syncWrap brings the breaks up to date with the key and with the edits: a change of the key
// makes them all again, an edit made the breaks of the lines it touched. It reports the anchor to
// restore when the number of rows changed.
func (vm *ViewModel) syncWrap(key wrapKey) {
	w := &vm.wrap
	lines := vm.doc.Snapshot().LineCount()
	if key != w.key {
		a := vm.captureAnchor()
		was := w.key.on
		w.key = key
		switch {
		case !key.on:
			vm.stopWrapping()
		default:
			w.resetBreaks(lines)
			if !was {
				w.unsubscribe = vm.doc.Subscribe(vm.wrapDocumentChanged)
			}
			vm.scheduleWrapStep()
		}
		defer vm.post(func() { vm.restoreAnchor(a) })
	}
	if !key.on {
		w.syncedEpoch = w.epoch
		return
	}
	if len(w.breaks) != lines {
		w.resetBreaks(lines)
		vm.scheduleWrapStep()
	}
	vm.breakDirtyLines()
	if w.pending > 0 && vm.afterTimer == nil {
		vm.breakLines(lines, math.MaxInt) // nothing to run steps: all at once
	}
	w.syncedEpoch = w.epoch
}

// stopWrapping drops the breaks and stops following the document.
func (vm *ViewModel) stopWrapping() {
	w := &vm.wrap
	if w.unsubscribe != nil {
		w.unsubscribe()
		w.unsubscribe = nil
	}
	if w.cancelStep != nil {
		w.cancelStep()
		w.cancelStep = nil
	}
	w.breaks, w.known, w.pending, w.cursor, w.dirty = nil, nil, 0, 0, nil
}

// DisposeWrap stops the word wrap: no more steps and no more following of the document.
func (vm *ViewModel) DisposeWrap() {
	vm.stopWrapping()
	vm.wrap.key = wrapKey{}
	vm.wrap.epoch++
}

// resetBreaks forgets the breaks of every line.
func (w *wrapLink) resetBreaks(lines int) {
	w.breaks = make([]*wrap.Breaks, lines)
	w.known = make([]bool, lines)
	w.pending, w.cursor, w.dirty = lines, 0, nil
	w.epoch++
}

// wrapDocumentChanged follows an edit: the lines that it replaced are replaced by as many that
// are not known, and are broken before the next frame.
func (vm *ViewModel) wrapDocumentChanged(e document.ChangeEvent) {
	w := &vm.wrap
	if !w.key.on || len(w.breaks) != e.Before.LineCount() {
		return // not in step: the next Sync starts again
	}
	for i := len(e.Changes) - 1; i >= 0; i-- {
		c := e.Changes[i]
		first, last := c.OldRange.Start.Line, c.OldRange.End.Line
		added := strings.Count(c.NewText, "\n") + 1
		w.breaks = splice(w.breaks, first, last+1, make([]*wrap.Breaks, added))
		w.known = splice(w.known, first, last+1, make([]bool, added))
		for j := range w.dirty {
			if w.dirty[j] > last {
				w.dirty[j] += added - (last - first + 1)
			}
		}
		for k := range added {
			w.dirty = append(w.dirty, first+k)
		}
	}
	w.pending = 0
	for _, k := range w.known {
		if !k {
			w.pending++
		}
	}
	w.epoch++
	w.cursor = min(w.cursor, len(w.breaks))
}

// splice replaces s[from:to] by what.
func splice[T any](s []T, from, to int, what []T) []T {
	out := make([]T, 0, len(s)-(to-from)+len(what))
	out = append(out, s[:from]...)
	out = append(out, what...)
	return append(out, s[to:]...)
}

// breakDirtyLines breaks the lines an edit made, so that they are right in the frame that shows
// them. An edit that made many (a paste) leaves most to the steps.
func (vm *ViewModel) breakDirtyLines() {
	w := &vm.wrap
	dirty := w.dirty
	w.dirty = nil
	if len(dirty) > 2*wrapWindow {
		dirty = dirty[:2*wrapWindow]
	}
	for _, line := range dirty {
		vm.breakLine(line)
	}
}

// breakLine makes the breaks of a line that is not known.
func (vm *ViewModel) breakLine(line int) {
	w := &vm.wrap
	if line < 0 || line >= len(w.breaks) || w.known[line] {
		return
	}
	text, err := vm.doc.Snapshot().LineContent(line)
	if err != nil {
		text = ""
	}
	b := vm.breaksOf(text)
	w.breaks[line], w.known[line] = b, true
	w.pending--
	if b != nil {
		w.epoch++
	}
}

// breakLines breaks lines that are not known, those around the top of the editor first, and goes
// on from where the last step stopped, until the limit of lines or of time.
func (vm *ViewModel) breakLines(lines, limit int) {
	w := &vm.wrap
	start := time.Now()
	done := 0
	top := 0
	if vm.proj != nil {
		top, _, _ = vm.proj.RowSpan(vm.proj.RowAt(vm.scrollTop))
	}
	for line := max(top-wrapWindow, 0); line < min(top+wrapWindow*2, lines); line++ {
		vm.breakLine(line)
	}
	for w.pending > 0 && done < limit {
		for w.cursor < lines && w.known[w.cursor] {
			w.cursor++
		}
		if w.cursor >= lines {
			break
		}
		vm.breakLine(w.cursor)
		done++
		if done%64 == 0 && limit != math.MaxInt && time.Since(start) > wrapSliceTime {
			break
		}
	}
	w.epoch++
}

// scheduleWrapStep asks for the next step of the computation of the breaks.
func (vm *ViewModel) scheduleWrapStep() {
	w := &vm.wrap
	if w.cancelStep != nil || vm.afterTimer == nil || w.pending == 0 {
		return
	}
	w.cancelStep = vm.afterTimer(time.Millisecond, func() {
		w.cancelStep = nil
		vm.wrapStep()
	})
}

// wrapStep breaks a slice of the lines that are not known, brings the projection up to date, keeps
// the top of the editor where it is, and asks for the next slice.
func (vm *ViewModel) wrapStep() {
	w := &vm.wrap
	if !w.key.on || w.pending == 0 {
		return
	}
	a := vm.captureAnchor()
	vm.breakLines(len(w.breaks), wrapSliceLines)
	vm.Sync()
	vm.restoreAnchor(a)
	vm.NotifyListeners()
	vm.scheduleWrapStep()
}

// WrapPending is the number of lines whose breaks are not known yet.
func (vm *ViewModel) WrapPending() int { return vm.wrap.pending }

// breaksOf breaks a line: by counting columns when its characters are all one column wide in a
// monospace font, otherwise by laying the text out. A line longer than editor.stopRenderingLineAfter is
// not wrapped.
func (vm *ViewModel) breaksOf(text string) *wrap.Breaks {
	key := vm.wrap.key
	if key.stopAfter >= 0 && len(text) > key.stopAfter {
		return nil
	}
	opts := wrap.Options{
		TabSize: key.tabSize, Column: key.columns, Indent: key.indent,
		BreakBefore: wrap.DefaultBreakBefore, BreakAfter: wrap.DefaultBreakAfter,
	}
	if vm.wrap.factory == nil || (!key.advanced && key.monospace && columnCountable(text)) {
		return wrap.Compute(text, opts)
	}
	return vm.breaksByLayout(text, opts)
}

// columnCountable tells whether every character of a text is printable ASCII or a tab: one
// column each, or up to the next tab stop.
func columnCountable(text string) bool {
	for i := range len(text) {
		if c := text[i]; c != '\t' && (c < ' ' || c >= 0x7F) {
			return false
		}
	}
	return true
}

// breaksByLayout breaks a text by laying it out with the width of a row: the rows are the lines the
// paragraph makes. The rows after the first are narrower by the indentation they get, which a
// paragraph cannot do, so they are made by a second layout of what the first leaves.
func (vm *ViewModel) breaksByLayout(text string, opts wrap.Options) *wrap.Breaks {
	key := vm.wrap.key
	style := newTextStyle(vm.wrap.factory, vm.opts, ui.NewColor(0xFF000000))
	width := float64(key.columns) * key.charWidth
	indent := wrap.IndentColumns(text, opts)
	first := style.make(text, nil)
	first.Layout(ui.NewParagraphConstraints(width))
	end := first.GetLineBoundary(ui.NewTextPosition(0)).End
	if end <= 0 || end >= len(text) {
		return nil
	}
	end = advanceOne(text, 0, end)
	offsets := []int{end}
	rest := text[end:]
	restWidth := max(width-float64(indent)*key.charWidth, key.charWidth)
	p := style.make(rest, nil)
	p.Layout(ui.NewParagraphConstraints(restWidth))
	at := 0
	for at < len(rest) {
		next := p.GetLineBoundary(ui.NewTextPosition(at)).End
		next = advanceOne(rest, at, next)
		if next >= len(rest) {
			break
		}
		offsets = append(offsets, end+next)
		at = next
	}
	offsets = append(offsets, len(text))
	return &wrap.Breaks{Offsets: offsets, VisibleColumns: make([]int, len(offsets)), IndentLength: indent}
}

// advanceOne makes an end of a row at least one character after its start, and on the start of a
// character.
func advanceOne(text string, start, end int) int {
	if end <= start {
		end = start + 1
	}
	end = min(end, len(text))
	for end < len(text) && text[end]&0xC0 == 0x80 {
		end++
	}
	return end
}
