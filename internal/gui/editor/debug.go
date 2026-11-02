package editor

import (
	"unicode/utf8"

	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// The debugger in the editor (SPEC §10.31 "The editor", step 25.5): the breakpoint glyphs in the glyph margin, the faint hint under the
// pointer, the lines of the stack frames, the inline breakpoints and their candidates, the breakpoint widget, the debug hover, the inline
// values and the exception widget. The editor knows nothing of sessions: the window gives it marks and the functions to call when the
// user presses a glyph, asks for a hover or accepts a widget (DebugHost). The marks are decorations of the document that this editor
// owns, so they follow its edits at once, and another editor of the same document is given its own.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/breakpointEditorContribution.ts, callStackEditorContribution.ts,
// debugEditorContribution.ts, debugHover.ts, breakpointWidget.ts, exceptionWidget.ts and media/debug.contribution.css

// debugOwner is the slot of the decorations of the debug marks (see ViewModel.owner).
const debugOwner = 15

// DebugGlyph is the kind of a breakpoint, which decides its icon.
type DebugGlyph int

// The kinds of breakpoint.
const (
	// DebugGlyphBreakpoint is a plain breakpoint, DebugGlyphConditional one with a condition or a hit count, DebugGlyphLog a logpoint
	// and DebugGlyphFunction a function breakpoint (shown in the margin where the adapter resolved it).
	DebugGlyphBreakpoint DebugGlyph = iota
	DebugGlyphConditional
	DebugGlyphLog
	DebugGlyphFunction
)

// DebugBreakpointMark is a breakpoint in the editor.
type DebugBreakpointMark struct {
	// ID is the id of the breakpoint in the debug model.
	ID string
	// Line is the line, counted from 0; Col is the byte column of an inline breakpoint (Inline is true), where it is.
	Line, Col int
	Inline    bool
	Kind      DebugGlyph
	// Enabled and Verified are the state of the breakpoint; Activated is false while the breakpoints as a whole are switched off.
	Enabled, Verified, Activated bool
	// Unsupported is a breakpoint with a condition or message that the adapter cannot honor; Pending a triggered one that waits for
	// another to be hit.
	Unsupported, Pending bool
	// Hover is the Markdown of the hover of the glyph (the adapter's message, or the condition); empty for none.
	Hover string
}

// DebugFrameMark is a stack frame in the editor: the line the program stopped at (Top), or the frame that the user focused.
type DebugFrameMark struct {
	// Line is counted from 0 and Col is the byte column of the frame (0 for the start of the line).
	Line, Col int
	// Top is the top frame of its thread, which has the stronger highlight; the others are the focused frame.
	Top bool
	// Glyph draws the arrow in the glyph margin: the frame belongs to the focused session.
	Glyph bool
}

// DebugCandidate is a place of a line where an inline breakpoint can be set (the adapter's breakpointLocations answer).
type DebugCandidate struct {
	Line, Col int
}

// DebugColors are the colors of the debug marks.
type DebugColors struct {
	Breakpoint, BreakpointDisabled, BreakpointUnverified ui.Color
	// StackFrame is the arrow of the top frame in the margin and FocusedFrame the arrow of the others.
	StackFrame, FocusedFrame ui.Color
	// StackFrameLine and FocusedFrameLine are the backgrounds of the lines of the two kinds of frame.
	StackFrameLine, FocusedFrameLine ui.Color
	// Ruler marks: the breakpoints, the top frame and the focused frame on the overview ruler.
	RulerBreakpoint ui.Color
}

// DebugOptions are the settings debug.* that the editor reads.
type DebugOptions struct {
	// InlineValues is debug.inlineValues: "auto", "on" or "off".
	InlineValues string
	// OverviewRuler is debug.showBreakpointsInOverviewRuler.
	OverviewRuler bool
	// InlineCandidates is debug.showInlineBreakpointCandidates.
	InlineCandidates bool
	// MiddleClickAction is debug.gutterMiddleClickAction: "logpoint", "conditionalBreakpoint", "triggeredBreakpoint" or "none".
	MiddleClickAction string
}

// DebugHost is what the window gives the editor to act on the debugger. Every function is called on the UI thread; any may be nil.
type DebugHost struct {
	// Enabled says whether breakpoints can be set in the document: a debugger is available for its language (VS Code's
	// canSetBreakpointsIn). Without it the glyph margin does nothing.
	Enabled func() bool
	// GlyphPress is a press of the primary or the middle button on the glyph margin of a line (counted from 0); with the right button
	// the menu is asked for (see TakeDebugMenuTarget).
	GlyphPress func(line int, mods MouseModifiers, middle bool)
	// InlinePress is a click on an inline breakpoint or on a candidate: id is the breakpoint, empty for a candidate with none.
	InlinePress func(line, col int, id string)
	// HoverReady says that the debug hover works in this editor: a session is stopped in a frame of this document.
	HoverReady func() bool
	// Evaluate evaluates the expression for the hover (context hover) at the place and calls done on the UI thread with what it shows,
	// or with nil when the expression has no value.
	Evaluate func(expr string, rng buffer.Range, done func(*DebugHoverContent))
}

// DebugMenuTarget is where the secondary button was pressed last, for the menu of the glyph margin.
type DebugMenuTarget struct {
	// Line is counted from 0 and Col is the byte column of an inline breakpoint or candidate, -1 for the glyph margin.
	Line, Col int
	// ID is the breakpoint of an inline mark that was pressed.
	ID string
	// InGlyph says that the press was in the glyph margin (or on an inline mark): the window shows the menu of the breakpoints.
	InGlyph bool
}

// debugData is what a decoration of the debug owner carries.
type debugData struct {
	bp    *DebugBreakpointMark
	frame *DebugFrameMark
	cand  *DebugCandidate
}

// debugState is the debugger in a view model.
type debugState struct {
	host *DebugHost
	// ids are the decorations of the three groups: the breakpoints, the frames and the candidates.
	ids [3][]int
	// epoch changes with the marks, for the caches that depend on them.
	epoch int
	// hintLine is the line (from 0) the faint breakpoint hint shows on, and hintOn whether it shows.
	hintLine int
	hintOn   bool
	// menu is the last secondary press.
	menu DebugMenuTarget
	// colors are the colors of the marks (the widget sets them).
	colors DebugColors
	// focus gives the keyboard back to the editor (the widget sets it).
	focus      func()
	focusInput func(*widgets.FocusNode)
	// bpw is the breakpoint widget and exc the exception widget; hv the debug hover.
	bpw *breakpointWidget
	exc *exceptionWidget
	hv  debugHover
}

// SetDebugHost gives the editor the means to act on the debugger; nil takes them away.
func (vm *ViewModel) SetDebugHost(h *DebugHost) {
	vm.dbg.host = h
	vm.dbgChanged()
}

// DebugHostSet tells whether the editor has a debug host.
func (vm *ViewModel) DebugHostSet() bool { return vm.dbg.host != nil }

// SetDebugColors gives the editor the colors of the debug marks (the widget sets them every build).
func (vm *ViewModel) SetDebugColors(c DebugColors) { vm.dbg.colors = c }

// debugEnabled says whether the glyph margin sets breakpoints.
func (vm *ViewModel) debugEnabled() bool {
	h := vm.dbg.host
	return h != nil && vm.opts.GlyphMargin && (h.Enabled == nil || h.Enabled())
}

// dbgChanged makes the layers draw the marks again.
func (vm *ViewModel) dbgChanged() {
	vm.dbg.epoch++
	vm.inlay.epoch++
	vm.inlayRelayout()
}

// setDebugGroup replaces the decorations of a group.
func (vm *ViewModel) setDebugGroup(group int, add []document.NewDecoration) {
	vm.dbg.ids[group] = vm.doc.DeltaDecorations(vm.owner(debugOwner), vm.dbg.ids[group], add)
	vm.dbgChanged()
}

// markRange is the range a mark's decoration covers: one character at its column (an empty range at the end of a line, which has none).
func (vm *ViewModel) markRange(line, col int) (buffer.Range, bool) {
	snap := vm.doc.Snapshot()
	if line < 0 || line >= snap.LineCount() {
		return buffer.Range{}, false
	}
	text, err := snap.LineContent(line)
	if err != nil {
		return buffer.Range{}, false
	}
	col = min(max(col, 0), len(text))
	end := col
	if col < len(text) {
		_, n := utf8.DecodeRuneInString(text[col:])
		end = col + n
	}
	return buffer.Range{Start: buffer.Position{Line: line, Col: col}, End: buffer.Position{Line: line, Col: end}}, true
}

// firstNonSpace is the byte offset of the first character of a line that is not a space or a tab.
func firstNonSpace(text string) int {
	for i := 0; i < len(text); i++ {
		if text[i] != ' ' && text[i] != '\t' {
			return i
		}
	}
	return len(text)
}

// SetDebugBreakpoints replaces the breakpoints the editor shows. A breakpoint of a line the document does not have is not kept. A
// breakpoint of a whole line sits at the first character of the line that is not a space (a decoration needs a width), an inline one at
// its column.
func (vm *ViewModel) SetDebugBreakpoints(list []DebugBreakpointMark) {
	add := make([]document.NewDecoration, 0, len(list))
	snap := vm.doc.Snapshot()
	for _, b := range list {
		col := b.Col
		if !b.Inline {
			text, err := snap.LineContent(b.Line)
			if err != nil {
				continue
			}
			col = firstNonSpace(text)
		}
		r, ok := vm.markRange(b.Line, col)
		if !ok {
			continue
		}
		mark := b
		add = append(add, document.NewDecoration{Range: r, Options: document.DecorationOptions{
			Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: "debugBreakpoint", Data: debugData{bp: &mark},
		}})
	}
	if len(add) == 0 && len(vm.dbg.ids[0]) == 0 {
		return
	}
	vm.setDebugGroup(0, add)
}

// SetDebugFrames replaces the stack frames the editor shows (the top frame and the focused one).
func (vm *ViewModel) SetDebugFrames(list []DebugFrameMark) {
	add := make([]document.NewDecoration, 0, len(list))
	for _, f := range list {
		r, ok := vm.markRange(f.Line, f.Col)
		if !ok {
			continue
		}
		mark := f
		add = append(add, document.NewDecoration{Range: r, Options: document.DecorationOptions{
			Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: "debugFrame", Data: debugData{frame: &mark},
		}})
	}
	if len(add) == 0 && len(vm.dbg.ids[1]) == 0 {
		return
	}
	vm.setDebugGroup(1, add)
}

// SetDebugCandidates replaces the places the adapter says an inline breakpoint can be set at, for the lines that have a breakpoint.
func (vm *ViewModel) SetDebugCandidates(list []DebugCandidate) {
	add := make([]document.NewDecoration, 0, len(list))
	for _, c := range list {
		r, ok := vm.markRange(c.Line, c.Col)
		if !ok {
			continue
		}
		cand := c
		add = append(add, document.NewDecoration{Range: r, Options: document.DecorationOptions{
			Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: "debugCandidate", Data: debugData{cand: &cand},
		}})
	}
	if len(add) == 0 && len(vm.dbg.ids[2]) == 0 {
		return
	}
	vm.setDebugGroup(2, add)
}

// DebugBreakpoints are the breakpoints of the editor as they are now (moved by the edits), in the order of the document.
func (vm *ViewModel) DebugBreakpoints() []DebugBreakpointMark {
	var out []DebugBreakpointMark
	for _, d := range vm.doc.DecorationsOf(vm.owner(debugOwner)) {
		if data, ok := d.Options.Data.(debugData); ok && data.bp != nil {
			b := *data.bp
			b.Line, b.Col = d.Range.Start.Line, d.Range.Start.Col
			out = append(out, b)
		}
	}
	return out
}

// DebugFrames are the frames of the editor as they are now.
func (vm *ViewModel) DebugFrames() []DebugFrameMark {
	var out []DebugFrameMark
	for _, d := range vm.doc.DecorationsOf(vm.owner(debugOwner)) {
		if data, ok := d.Options.Data.(debugData); ok && data.frame != nil {
			f := *data.frame
			f.Line, f.Col = d.Range.Start.Line, d.Range.Start.Col
			out = append(out, f)
		}
	}
	return out
}

// DisposeDebug takes the debug marks and the widgets off the editor.
func (vm *ViewModel) DisposeDebug() {
	vm.DebugCloseBreakpointWidget()
	vm.DebugCloseException()
	vm.DebugHideHover()
	vm.doc.RemoveDecorationsOf(vm.owner(debugOwner))
	vm.dbg.ids = [3][]int{}
	vm.dbg.host = nil
	vm.dbgChanged()
}

// debugMarksIn are the marks of the lines first to last, by line.
type debugLine struct {
	bps    []*DebugBreakpointMark
	frames []*DebugFrameMark
	cands  []*DebugCandidate
	// cols are the columns the marks stand at, matching bps, frames and cands.
	bpCols, candCols []int
}

// debugMarksIn collects the marks that touch the lines of the rows first to last (the document's lines from..to).
func (vm *ViewModel) debugMarksIn(from, to int) map[int]*debugLine {
	if len(vm.dbg.ids[0])+len(vm.dbg.ids[1])+len(vm.dbg.ids[2]) == 0 {
		return nil
	}
	span := buffer.Range{Start: buffer.Position{Line: from}, End: buffer.Position{Line: to + 1}}
	var out map[int]*debugLine
	for _, d := range vm.doc.DecorationsIn(span, vm.owner(debugOwner)) {
		data, ok := d.Options.Data.(debugData)
		if !ok {
			continue
		}
		if out == nil {
			out = map[int]*debugLine{}
		}
		line := d.Range.Start.Line
		l := out[line]
		if l == nil {
			l = &debugLine{}
			out[line] = l
		}
		switch {
		case data.bp != nil:
			b := *data.bp
			b.Line, b.Col = line, d.Range.Start.Col
			l.bps = append(l.bps, &b)
			l.bpCols = append(l.bpCols, d.Range.Start.Col)
		case data.frame != nil:
			f := *data.frame
			f.Line, f.Col = line, d.Range.Start.Col
			l.frames = append(l.frames, &f)
		case data.cand != nil:
			c := *data.cand
			c.Line, c.Col = line, d.Range.Start.Col
			l.cands = append(l.cands, &c)
			l.candCols = append(l.candCols, d.Range.Start.Col)
		}
	}
	return out
}

// glyphMark is the breakpoint whose icon the glyph margin shows for a line: the one of the whole line, else the first inline one.
func (l *debugLine) glyphMark() *DebugBreakpointMark {
	var inline *DebugBreakpointMark
	for _, b := range l.bps {
		if !b.Inline {
			return b
		}
		if inline == nil {
			inline = b
		}
	}
	return inline
}

// SetDebugGutterPointer tells the editor where the pointer is in the gutter: y is the distance from the top of the gutter (the scroll is
// added here). A line with no breakpoint shows the faint hint of one; -1 as y (the pointer left) takes it away.
//
// Ported from VS Code: breakpointEditorContribution.ts (ensureBreakpointHintDecoration, onMouseMove)
func (vm *ViewModel) SetDebugGutterPointer(y float64, inGlyph bool) {
	s := &vm.dbg
	line, on := 0, false
	if y >= 0 && vm.debugEnabled() && vm.viewport > 0 && y < vm.viewport {
		proj := vm.Projection()
		row := proj.RowAt(y + vm.ScrollTop())
		if row >= 0 && row < proj.RowCount() {
			if _, _, zone := proj.RowZone(row); !zone {
				l, _, _ := proj.RowSpan(row)
				line, on = l, true
			}
		}
	}
	if on == s.hintOn && (!on || line == s.hintLine) {
		return
	}
	s.hintOn, s.hintLine = on, line
	if !on {
		vm.hoverStaticLeave(debugGlyphKey)
	}
	vm.NotifyListeners()
	if on && inGlyph {
		vm.debugGlyphHover(line)
	}
}

// debugGlyphKey is the key of the static hover of the glyph margin.
const debugGlyphKey = "debug-glyph"

// debugGlyphHover shows the hover of the glyph of a line after the hover delay: the message of its breakpoint, or the hint.
func (vm *ViewModel) debugGlyphHover(line int) {
	by := vm.debugMarksIn(line, line)
	text := "Click to add a breakpoint"
	if l := by[line]; l != nil {
		if g := l.glyphMark(); g != nil {
			text = g.Hover
		}
	}
	if text == "" {
		vm.hoverStaticLeave(debugGlyphKey)
		return
	}
	vm.hoverStaticMoved(debugGlyphKey, line, buffer.Position{Line: line}, text, nil)
}

// DebugGlyphHint is the line the faint breakpoint hint shows on, and whether it shows.
func (vm *ViewModel) DebugGlyphHint() (line int, shown bool) {
	if !vm.dbg.hintOn || !vm.debugEnabled() {
		return 0, false
	}
	if l := vm.debugMarksIn(vm.dbg.hintLine, vm.dbg.hintLine)[vm.dbg.hintLine]; l != nil && len(l.bps) > 0 {
		return 0, false
	}
	return vm.dbg.hintLine, true
}

// glyphPress is a press of the primary or the middle button at a distance from the left edge of the gutter: it tells the host when the
// press is in the glyph margin on a line of text. It says whether the press was used.
func (vm *ViewModel) glyphPress(x, y float64, mods MouseModifiers, middle bool) bool {
	h := vm.dbg.host
	if h == nil || h.GlyphPress == nil || !vm.debugEnabled() {
		return false
	}
	line, ok := vm.glyphLineAt(x, y)
	if !ok {
		return false
	}
	h.GlyphPress(line, mods, middle)
	return true
}

// glyphLineAt is the line whose glyph margin a point of the gutter is in; false when the point is elsewhere (or on a view zone, or past
// the last line).
func (vm *ViewModel) glyphLineAt(x, y float64) (int, bool) {
	w := vm.glyphWidth()
	if w <= 0 || x < 0 || x >= w {
		return 0, false
	}
	proj := vm.Projection()
	row := proj.RowAt(y + vm.ScrollTop())
	if row < 0 || row >= proj.RowCount() {
		return 0, false
	}
	if _, _, zone := proj.RowZone(row); zone {
		return 0, false
	}
	line, _, _ := proj.RowSpan(row)
	// past the last row of text the pointer is after the lines
	if top := proj.RowTop(row); y+vm.ScrollTop() < top || y+vm.ScrollTop() >= top+proj.RowHeight(row) {
		return 0, false
	}
	return line, true
}

// glyphWidth is the width of the glyph margin.
func (vm *ViewModel) glyphWidth() float64 {
	if !vm.opts.GlyphMargin {
		return 0
	}
	return vm.opts.LineHeight
}

// NoteDebugMenuTarget remembers where the secondary button was pressed (x and y as in glyphLineAt).
func (vm *ViewModel) noteDebugMenuTarget(x, y float64) {
	vm.dbg.menu = DebugMenuTarget{Col: -1}
	if line, ok := vm.glyphLineAt(x, y); ok && vm.debugEnabled() {
		vm.dbg.menu = DebugMenuTarget{Line: line, Col: -1, InGlyph: true}
	}
}

// TakeDebugMenuTarget gives the place of the last secondary press and forgets it: the window asks when it opens the context menu, and
// shows the menu of the breakpoints when the press was in the glyph margin or on an inline mark.
func (vm *ViewModel) TakeDebugMenuTarget() DebugMenuTarget {
	t := vm.dbg.menu
	vm.dbg.menu = DebugMenuTarget{Col: -1}
	return t
}

// SetDebugFocus gives the editor the function that returns the keyboard to it, which the breakpoint widget calls as it closes (the
// widget sets it).
func (vm *ViewModel) SetDebugFocus(f func()) { vm.dbg.focus = f }

// SetDebugInputFocus gives the editor the function that gives the keyboard to the input of a widget as soon as it is on screen (the
// widget sets it).
func (vm *ViewModel) SetDebugInputFocus(f func(*widgets.FocusNode)) { vm.dbg.focusInput = f }
