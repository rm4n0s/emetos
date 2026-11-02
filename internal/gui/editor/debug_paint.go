package editor

import (
	"math"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The drawing of the debug marks (step 25.5, debug.go): the glyphs in the margin, the lines of the stack frames, the inline breakpoints
// and their candidates in the text, and the marks on the overview ruler.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/breakpointEditorContribution.ts, callStackEditorContribution.ts,
// breakpointsView.ts (getBreakpointMessageAndIcon) and media/debug.contribution.css

// debugHintOpacity is the opacity of the faint breakpoint under the pointer (VS Code: `.codicon-debug-hint` has 0.4), and
// debugCandidateOpacity the one of the places where an inline breakpoint can be set (0.7 for the disabled icon).
const (
	debugHintOpacity      = 0.4
	debugCandidateOpacity = 0.7
)

// debugInject marks an injection as an inline breakpoint, a candidate or the arrow of the stopped column.
type debugInject struct {
	// id is the breakpoint of the injection, empty for a candidate with none or the arrow.
	id string
	// line and col are where the injection is, and arrow says that it is the arrow of a stopped frame.
	line, col int
	arrow     bool
}

// breakpointIcon is the Codicon and the color of the icon of a breakpoint: the kind, whether it is on and verified.
//
// Ported from VS Code: breakpointsView.ts (getBreakpointMessageAndIcon)
func breakpointIcon(b DebugBreakpointMark, c DebugColors) (code int, color ui.Color) {
	active := b.Activated && b.Enabled && b.Verified
	color = c.Breakpoint
	if !active {
		color = c.BreakpointDisabled
		if b.Activated && b.Enabled && !b.Verified {
			color = c.BreakpointUnverified
		}
	}
	if b.Pending {
		return fonts.CodiconIssueDraft, c.Breakpoint
	}
	switch b.Kind {
	case DebugGlyphFunction:
		if active {
			return fonts.CodiconDebugBreakpointFunction, color
		}
		return fonts.CodiconDebugBreakpointFunctionUnverified, color
	case DebugGlyphConditional:
		if active {
			return fonts.CodiconDebugBreakpointConditional, color
		}
		return fonts.CodiconDebugBreakpointConditionalUnverified, color
	case DebugGlyphLog:
		if active {
			return fonts.CodiconDebugBreakpointLog, color
		}
		return fonts.CodiconDebugBreakpointLogUnverified, color
	}
	if b.Unsupported {
		return fonts.CodiconDebugBreakpointUnsupported, color
	}
	if !active {
		return fonts.CodiconCircle, color
	}
	return fonts.CodiconCircleFilled, color
}

// glyphSize is the size of the icons of the glyph margin: most of a line, between 10 and 18 pixels.
func (vm *ViewModel) glyphSize() float64 {
	return math.Min(math.Max(math.Round(vm.opts.LineHeight*0.85), 10), 18)
}

// paintDebugGlyphs draws the icons of the glyph margin: the breakpoint of each visible line, the arrow of a stack frame over it, and the
// faint hint under the pointer.
func (r *RenderEditorGutter) paintDebugGlyphs(canvas *ui.Canvas, offset ui.Offset, top float64, first, last int, widths GutterWidths) {
	vm := r.vm
	if widths.GlyphMargin <= 0 {
		return
	}
	proj := vm.Projection()
	firstLine, _, _ := proj.RowSpan(first)
	lastLine, _, _ := proj.RowSpan(last)
	by := vm.debugMarksIn(firstLine, lastLine)
	hintLine, hint := vm.DebugGlyphHint()
	if len(by) == 0 && !hint {
		return
	}
	colors := r.colors.Debug
	icons := vm.Options()
	icons.FontFamilies, icons.FontSize, icons.FontWeight, icons.LetterSpacing, icons.FontFeatures = []string{fonts.Codicon}, vm.glyphSize(), 0, 0, nil
	draw := func(row, code int, color ui.Color) {
		p := newTextStyle(r.factory, icons, color).paragraph(vm.Paragraphs(), string(rune(code)))
		lane := float64(vm.glyphLanes() - 1) // the breakpoints and the frames have the right lane of the margin
		x := offset.Dx + lane*vm.opts.LineHeight + (vm.opts.LineHeight-p.LongestLine())/2
		canvas.DrawParagraph(p, ui.Offset{Dx: x, Dy: offset.Dy + proj.RowTop(row) - top})
	}
	for row := first; row <= last; row++ {
		line, start, _ := proj.RowSpan(row)
		if _, _, zone := proj.RowZone(row); start != 0 || zone {
			continue // the glyph stands at the first row of a line
		}
		l := by[line]
		if l == nil {
			if hint && line == hintLine {
				draw(row, fonts.CodiconCircleFilled, colors.Breakpoint.WithOpacity(debugHintOpacity))
			}
			continue
		}
		if g := l.glyphMark(); g != nil {
			code, color := breakpointIcon(*g, colors)
			draw(row, code, color)
		}
		for _, f := range l.frames {
			if !f.Glyph {
				continue
			}
			color := colors.FocusedFrame
			if f.Top {
				color = colors.StackFrame
			}
			draw(row, fonts.CodiconDebugStackframe, color)
		}
	}
}

// paintDebugFrames draws the lines of the stack frames across the text: the top frame and the focused one.
func (v overlayView) paintDebugFrames() {
	vm := v.geo.vm
	if len(vm.dbg.ids[1]) == 0 {
		return
	}
	proj := v.geo.proj
	firstLine, _, _ := proj.RowSpan(v.first)
	lastLine, _, _ := proj.RowSpan(v.last)
	by := vm.debugMarksIn(firstLine, lastLine)
	colors := vm.dbg.colors
	for line, l := range by {
		for _, f := range l.frames {
			color := colors.FocusedFrameLine
			if f.Top {
				color = colors.StackFrameLine
			}
			if color.Alpha() == 0 {
				continue
			}
			from, to := wholeLineRows(vm, buffer.Range{Start: buffer.Position{Line: line}, End: buffer.Position{Line: line}})
			for row := max(from, v.first); row <= min(to, v.last); row++ {
				if v.geo.isZoneRow(row) {
					continue
				}
				v.canvas.DrawRect(v.rowRect(row, vm.ScrollLeft(), vm.ScrollLeft()+v.width), fill(color))
			}
		}
	}
}

// debugRulerMarks are the marks of the breakpoints (left lane, with debug.showBreakpointsInOverviewRuler) and of the stack frames (full
// lane) for a ruler of a height.
func (vm *ViewModel) debugRulerMarks(height float64) []rulerMark {
	if height <= 0 || len(vm.dbg.ids[0])+len(vm.dbg.ids[1]) == 0 {
		return nil
	}
	total := vm.ContentHeight()
	if total <= 0 {
		return nil
	}
	proj := vm.Projection()
	scale := height / total
	var marks []rulerMark
	for _, d := range vm.doc.DecorationsOf(vm.owner(debugOwner)) {
		data, ok := d.Options.Data.(debugData)
		if !ok || data.cand != nil {
			continue
		}
		var lane rulerLane
		var color ui.Color
		switch {
		case data.bp != nil && vm.opts.Debug.OverviewRuler:
			lane, color = laneLeft, vm.dbg.colors.RulerBreakpoint
		case data.frame != nil:
			lane, color = laneFull, vm.dbg.colors.FocusedFrameLine
			if data.frame.Top {
				color = vm.dbg.colors.StackFrameLine
			}
		default:
			continue
		}
		row := vm.RowOfPosition(d.Range.Start)
		y := math.Round(proj.RowTop(row) * scale)
		h := max(math.Round((proj.RowTop(row)+proj.RowHeight(row))*scale)-y, rulerMinMarkHeight)
		marks = append(marks, rulerMark{y: y, height: h, lane: lane, custom: color, hasColor: true})
	}
	return marks
}

// hasDebugInjections tells whether the debug marks may put something into the text of a line.
func (vm *ViewModel) hasDebugInjections() bool {
	return len(vm.dbg.ids[0])+len(vm.dbg.ids[1])+len(vm.dbg.ids[2]) > 0
}

// debugInjections are the inline marks of a line: the inline breakpoints (when the line has another breakpoint or the breakpoint is not
// at the first character of the line), the places where one can be set, and the arrow of the stopped column.
//
// Ported from VS Code: breakpointEditorContribution.ts (createBreakpointDecorations: renderInline, createCandidateDecorations) and
// callStackEditorContribution.ts (makeStackFrameColumnDecoration)
func (vm *ViewModel) debugInjections(line int) []*injection {
	l := vm.debugMarksIn(line, line)[line]
	if l == nil {
		return nil
	}
	text, err := vm.doc.Snapshot().LineContent(line)
	if err != nil {
		return nil
	}
	first := firstNonSpace(text)
	lastCol := len(text)
	for lastCol > 0 && (text[lastCol-1] == ' ' || text[lastCol-1] == '\t') {
		lastCol--
	}
	colors := vm.dbg.colors
	size := vm.opts.FontSize
	var out []*injection
	add := func(col int, code int, color ui.Color, in *debugInject) {
		out = append(out, &injection{
			col: col, text: string(rune(code)), bgFrom: 0, bgTo: utf8.RuneLen(rune(code)),
			fg: color, size: size, family: fonts.Codicon, lineHeight: vm.opts.LineHeight, dbg: in,
		})
	}
	occupied := map[int]bool{}
	for _, b := range l.bps {
		if !b.Inline {
			continue
		}
		others := len(l.bps) > 1
		if b.Col > first || others {
			code, color := breakpointIcon(*b, colors)
			add(b.Col, code, color, &debugInject{id: b.ID, line: line, col: b.Col})
			occupied[b.Col] = true
		}
	}
	if vm.opts.Debug.InlineCandidates {
		hasInline := false
		for _, b := range l.bps {
			if b.Inline && b.Col > first {
				hasInline = true
			}
		}
		for _, c := range l.cands {
			if (c.Col <= first && !hasInline) || c.Col > lastCol || occupied[c.Col] {
				continue // a candidate at the start of a line is the line's breakpoint; one past the text, or taken, is left out
			}
			var at *DebugBreakpointMark
			for _, b := range l.bps {
				if b.Col == c.Col {
					at = b
				}
			}
			if at != nil {
				code, color := breakpointIcon(*at, colors)
				add(c.Col, code, color, &debugInject{id: at.ID, line: line, col: c.Col})
				continue
			}
			add(c.Col, fonts.CodiconCircle, colors.BreakpointDisabled.WithOpacity(debugCandidateOpacity), &debugInject{line: line, col: c.Col})
		}
	}
	for _, f := range l.frames {
		if f.Top && f.Col > first {
			add(f.Col, fonts.CodiconDebugStackframe, colors.StackFrame, &debugInject{line: line, col: f.Col, arrow: true})
			break
		}
	}
	return out
}

// DebugInlineAt finds the inline mark at a point of the text layer (in the layer's coordinates): the breakpoint or candidate under it.
func (r *RenderEditorText) DebugInlineAt(local ui.Offset) (line, col int, id string, ok bool) {
	if !r.vm.hasDebugInjections() {
		return 0, 0, "", false
	}
	g := newGeometry(r.vm, r.factory)
	row := g.proj.RowAt(local.Dy + r.top())
	if row < 0 || row >= g.proj.RowCount() || g.isZoneRow(row) {
		return 0, 0, "", false
	}
	inj, _, found := g.hintAt(row, local.Dx+r.vm.ScrollLeft())
	if !found || inj.dbg == nil || inj.dbg.arrow {
		return 0, 0, "", false
	}
	return inj.dbg.line, inj.dbg.col, inj.dbg.id, true
}
