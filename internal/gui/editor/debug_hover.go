package editor

import (
	"math"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The debug hover (step 25.5): while a session is stopped in a frame of the document, the expression under the pointer is evaluated
// (context hover) and its value shown in a box over the text, as a tree when it has children. Alt held shows the hover of the language
// instead. The box and its place are the editor's; what is in it is built by the window (DebugHoverContent.Body).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugHover.ts, debugEditorContribution.ts (onEditorMouseMove,
// hoverDelay, showHover) and common/debugUtils.ts (getEvaluatableExpressionAtPosition)

// DebugHoverContent is what the debug hover shows.
type DebugHoverContent struct {
	// Range is the expression under the pointer; the box stands over its start.
	Range buffer.Range
	// Complex says that the value has children: the box shows the title, the tree and the tip about the Alt key.
	Complex bool
	// Body builds the content of the box.
	Body func(bc widgets.BuildContext) widgets.Widget
	// Width and Height are the size the content wants, to place the box before it is laid out.
	Width, Height float64
}

// debugHover is the state of the debug hover of a view model.
type debugHover struct {
	shown *DebugHoverContent
	// at is the position the pointer rested on when the evaluation was asked, and gen numbers the evaluations.
	at      buffer.Position
	waiting bool
	gen     int
	cancel  func()
	// pointerIn says that the pointer is over the box, which keeps it.
	pointerIn bool
	changed   *foundation.ChangeNotifier
}

func (vm *ViewModel) debugHoverNotifier() *foundation.ChangeNotifier {
	if vm.dbg.hv.changed == nil {
		vm.dbg.hv.changed = foundation.NewChangeNotifier()
	}
	return vm.dbg.hv.changed
}

// AddDebugHoverListener calls fn when the debug hover shows or hides.
func (vm *ViewModel) AddDebugHoverListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return vm.debugHoverNotifier().AddListener(fn)
}

// RemoveDebugHoverListener removes a listener of AddDebugHoverListener.
func (vm *ViewModel) RemoveDebugHoverListener(h foundation.ListenerHandle) {
	vm.debugHoverNotifier().RemoveListener(h)
}

// DebugHover is the debug hover that shows now, or nil.
func (vm *ViewModel) DebugHover() *DebugHoverContent { return vm.dbg.hv.shown }

// debugHoverDelay is how long the pointer rests before the value is evaluated: the delay of the hover, longer for the short ones, as
// VS Code does (a debug hover can be very large): 300 ms becomes 600, 100 becomes 200, 600 becomes 900 and 1000 stays.
func (vm *ViewModel) debugHoverDelay() time.Duration {
	base := float64(vm.opts.Hover.Delay)
	factor := math.Min(math.Max(2-(base-300)/600, 1), 2)
	return time.Duration(base*factor) * time.Millisecond
}

// DebugHoverPointer tells the debug hover that the pointer rests on the text at a position. It says whether the debug hover has the
// pointer (the hover of the language does not start): a session is stopped in this document and Alt is not held. The expression is
// evaluated after the delay; a pointer that stays in the expression keeps the box, one that goes into the box (the hover is sticky) too.
func (vm *ViewModel) DebugHoverPointer(p buffer.Position, alt bool) bool {
	h := vm.dbg.host
	if h == nil || h.HoverReady == nil || h.Evaluate == nil || !vm.hoverEnabled() || !h.HoverReady() {
		vm.DebugHideHover()
		return false
	}
	if alt {
		vm.DebugHideHover()
		return false
	}
	s := &vm.dbg.hv
	if s.shown != nil && rangeHolds(s.shown.Range, p) {
		return true
	}
	if s.waiting && s.at == p {
		return true
	}
	if s.shown != nil && vm.opts.Hover.Sticky && s.pointerIn {
		return true
	}
	vm.debugHoverCancel()
	s.at, s.waiting = p, true
	gen := s.gen
	run := func() { vm.debugHoverEvaluate(p, gen, false) }
	if d := vm.debugHoverDelay(); d > 0 && vm.afterTimer != nil {
		s.cancel = vm.afterTimer(d, run)
	} else {
		run()
	}
	return true
}

// DebugShowHover shows the debug hover at once at a position (the command Debug: Show Hover). It says whether there is an expression.
func (vm *ViewModel) DebugShowHover(p buffer.Position) bool {
	h := vm.dbg.host
	if h == nil || h.HoverReady == nil || h.Evaluate == nil || !h.HoverReady() {
		return false
	}
	vm.debugHoverCancel()
	s := &vm.dbg.hv
	s.at, s.waiting = p, true
	return vm.debugHoverEvaluate(p, s.gen, true)
}

// debugHoverCancel stops the wait and the evaluation in progress.
func (vm *ViewModel) debugHoverCancel() {
	s := &vm.dbg.hv
	s.gen++
	s.waiting = false
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// debugHoverEvaluate asks the host to evaluate the expression at a position and shows what comes back. Only the latest evaluation shows.
func (vm *ViewModel) debugHoverEvaluate(p buffer.Position, gen int, _ bool) bool {
	s := &vm.dbg.hv
	if gen != s.gen {
		return false
	}
	s.cancel = nil
	line, err := vm.doc.Snapshot().LineContent(p.Line)
	if err != nil {
		s.waiting = false
		return false
	}
	col := utf8.RuneCountInString(line[:min(p.Col, len(line))]) + 1
	text, startCol, endCol := debug.EvaluatableExpression(line, col)
	if text == "" {
		s.waiting = false
		vm.DebugHideHover()
		return false
	}
	rng := buffer.Range{
		Start: buffer.Position{Line: p.Line, Col: byteOfRune(line, startCol-1)},
		End:   buffer.Position{Line: p.Line, Col: byteOfRune(line, endCol-1)},
	}
	h := vm.dbg.host
	version := vm.doc.Version()
	h.Evaluate(text, rng, func(c *DebugHoverContent) {
		if gen != s.gen || version != vm.doc.Version() {
			return
		}
		s.waiting = false
		if c == nil {
			// no value: the hover of the language takes the place, as in VS Code
			vm.DebugHideHover()
			vm.hoverReact(p)
			return
		}
		c.Range = rng
		s.shown = c
		vm.debugHoverNotifier().NotifyListeners()
	})
	return true
}

// byteOfRune is the byte offset of the n-th character (from 0) of a line, clamped to its end.
func byteOfRune(line string, n int) int {
	off := 0
	for i := 0; i < n && off < len(line); i++ {
		_, size := utf8.DecodeRuneInString(line[off:])
		off += size
	}
	return off
}

// DebugHideHover hides the debug hover and cancels the one that is coming.
func (vm *ViewModel) DebugHideHover() {
	s := &vm.dbg.hv
	was := s.shown != nil
	vm.debugHoverCancel()
	s.shown, s.pointerIn = nil, false
	if was {
		vm.debugHoverNotifier().NotifyListeners()
	}
}

// DebugHoverBoxEntered tells the hover that the pointer entered or left its box: a sticky hover stays while the pointer is in it; one
// that left hides unless the pointer is on the expression.
func (vm *ViewModel) DebugHoverBoxEntered(in bool) {
	s := &vm.dbg.hv
	s.pointerIn = in
	if !in && s.shown != nil && !vm.opts.Hover.Sticky {
		vm.DebugHideHover()
	}
}

// DebugPointerLeft tells the debug hover that the pointer left the text: a box the pointer is not in goes.
func (vm *ViewModel) DebugPointerLeft() {
	s := &vm.dbg.hv
	if s.pointerIn {
		return
	}
	if s.shown != nil || s.waiting {
		vm.DebugHideHover()
	}
}

// debugHoverLayer is the box of the debug hover over the text, or nil while there is none.
func (s *editorState) debugHoverLayer(bc widgets.BuildContext, vm *ViewModel, colors kit.Colors) widgets.Widget {
	c := vm.DebugHover()
	if c == nil || c.Body == nil || s.textRO == nil || !s.textRO.Attached() || vm.viewport <= 0 {
		return nil
	}
	rect := s.textRO.RectOf(c.Range.Start)
	if rect.Bottom < 0 || rect.Top > vm.viewport {
		return nil
	}
	origin := s.gutterWidth
	if vm.mm.info.Show && vm.Options().Minimap.Side == "left" {
		origin += vm.mm.info.Width
	}
	width, height := c.Width, c.Height
	left := origin + rect.Left
	if viewW, _ := vm.ViewportSize(); viewW > 0 {
		left = max(min(left, origin+viewW-width), 0)
	}
	pos := widgets.PositionedOpts{Left: foundation.Some(left)}
	// above the line when it fits, else below (VS Code prefers above)
	fitsAbove, fitsBelow := rect.Top-height >= 0, rect.Bottom+height <= vm.viewport
	room := 0.0
	if fitsAbove || !fitsBelow && rect.Top > vm.viewport-rect.Bottom {
		pos.Bottom = foundation.Some(vm.viewport - rect.Top)
		room = rect.Top
	} else {
		pos.Top = foundation.Some(max(rect.Bottom, 0))
		room = vm.viewport - max(rect.Bottom, 0)
	}
	body := c.Body(bc)
	box := widgets.NewContainer(widgets.NewClipRRect(body, widgets.ClipRRectOpts{BorderRadius: painting.NewBorderRadiusCircular(hoverRadius)}),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.EditorHoverWidgetBackground),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.EditorHoverWidgetBorder), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(hoverRadius),
			BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: hoverShadowBlur})},
		})})
	constrained := widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{
		MaxWidth: foundation.Some(math.Max(width, 60)), MaxHeight: foundation.Some(math.Max(math.Min(vm.viewport, room), hoverLineHeight*2)),
	}), box)
	return widgets.NewStack([]widgets.Widget{widgets.NewPositioned(widgets.NewMouseRegion(
		widgets.NewGestureDetector(constrained, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque}),
		widgets.MouseRegionOpts{
			OnEnter: func(*gestures.PointerEnterEvent) { vm.DebugHoverBoxEntered(true) },
			OnExit:  func(*gestures.PointerExitEvent) { vm.DebugHoverBoxEntered(false) },
		}), pos)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// ---- the hover of a place that is not text

// hoverStaticMoved shows the hover of a place that is not text (the glyph margin) after the hover delay: the Markdown at a position. A
// hover that shows for the same key and line keeps showing. It is the hover of an annotation with no annotation behind it.
func (vm *ViewModel) hoverStaticMoved(key string, line int, at buffer.Position, md string, links func(string)) {
	h := &vm.hover
	if !vm.hoverEnabled() || vm.hoverMouseBlocked() || h.focused {
		return
	}
	cur := h.shownAnn
	if h.op != nil && h.op.anchor.ann != nil {
		cur = h.op.anchor.ann
	}
	if cur != nil && cur.key == key && cur.line == line {
		vm.cancelHoverHide()
		vm.cancelHoverReact()
		return
	}
	vm.cancelHoverHide()
	vm.cancelHoverReact()
	vm.hoverHideShown()
	if md == "" {
		vm.HideHover()
		return
	}
	entry := annotationEntry{LineAnnotation{Hover: md, Link: links}}
	vm.hoverStart(hoverAnchor{rng: buffer.Range{Start: at, End: at}, pos: at, ann: &annotationAnchor{line: line, index: -1, key: key, entry: entry}}, false, false)
}

// hoverStaticLeave hides the hover of a place that is not text when it is the one with the key that shows or is coming.
func (vm *ViewModel) hoverStaticLeave(key string) {
	h := &vm.hover
	cur := h.shownAnn
	if h.op != nil && h.op.anchor.ann != nil {
		cur = h.op.anchor.ann
	}
	if cur != nil && cur.key == key {
		vm.HideHover()
	}
}
