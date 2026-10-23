package editor

import (
	"time"

	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

const (
	// doubleClickInterval is the time within which a press after a press is a double click.
	doubleClickInterval = 400 * time.Millisecond
	// doubleClickSlop is how far, in pixels, a press can be from the one before and still count
	// as the next click of a double or triple click.
	doubleClickSlop = 5
	// maxClicks is the click count that selects everything; more clicks stay there.
	maxClicks = 4
)

// mouseState is what the editor remembers of the mouse: the clicks in a row and the press that
// is held down.
type mouseState struct {
	// lastDown and lastAt are the time and place of the last press, and clicks its count.
	lastDown time.Duration
	lastAt   ui.Offset
	clicks   int

	// pressed is whether the primary button is held; pointer says which pointer holds it.
	pressed bool
	pointer int
	// mods, onNumbers and the click count are those of the press, which a drag keeps.
	mods      MouseModifiers
	onNumbers bool
	// last is the last event dispatched, so that a pointer that moves within a character is
	// not dispatched again.
	last MouseEvent
	// middle says the held button is the middle one; moved that the pointer went somewhere
	// since the press, which a middle click that pastes must not have done.
	middle, moved bool
	// dnd is the drag of selected text that the press started, if it did, and cancelKey the
	// handler that ends it with Escape.
	dnd       *textDrag
	cancelKey services.KeyHandlerHandle
	// fold is the press on a folding control or on the placeholder of a folded line, which
	// folds or unfolds on release.
	fold *foldPress
}

// foldPress is a press that the folding gets: the line, whether it is on the control in the
// gutter (else on the placeholder), which pointer and whether the middle button.
type foldPress struct {
	line    int
	control bool
	pointer int
	middle  bool
}

// modifiers reads the modifier keys held now.
func (s *editorState) modifiers() MouseModifiers {
	kb := s.binding.Keyboard()
	return MouseModifiers{Shift: kb.IsShiftPressed(), Ctrl: kb.IsControlPressed(), Alt: kb.IsAltPressed(), Meta: kb.IsMetaPressed()}
}

// countClick counts a press with the ones before it.
func (m *mouseState) countClick(at ui.Offset, stamp time.Duration) int {
	near := abs(at.Dx-m.lastAt.Dx) <= doubleClickSlop && abs(at.Dy-m.lastAt.Dy) <= doubleClickSlop
	if m.clicks > 0 && near && stamp-m.lastDown <= doubleClickInterval {
		m.clicks = min(m.clicks+1, maxClicks)
	} else {
		m.clicks = 1
	}
	m.lastDown, m.lastAt = stamp, at
	return m.clicks
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// eventAt is the mouse event for a point of the editor, given in the coordinates of the row of
// the gutter and the text. A point in the gutter is the start of its row; one on the line
// numbers makes the selection go by lines.
func (s *editorState) eventAt(local ui.Offset, gutterWidth float64) MouseEvent {
	vm := s.Widget().vm
	if s.textRO == nil {
		return MouseEvent{}
	}
	if local.Dx < gutterWidth {
		w := vm.GutterWidths(textFactory(s.Context()))
		pos := s.textRO.PositionAt(ui.Offset{Dx: -vm.ScrollLeft(), Dy: local.Dy})
		pos.Col = 0
		numbers := vm.Options().LineNumbers != LineNumbersOff && local.Dx >= w.GlyphMargin && local.Dx < w.GlyphMargin+w.LineNumbers
		return MouseEvent{Position: pos, OnLineNumbers: numbers}
	}
	in := ui.Offset{Dx: local.Dx - gutterWidth, Dy: local.Dy}
	return MouseEvent{Position: s.textRO.PositionAt(in), VisualColumn: s.textRO.VisualColumnAt(in)}
}

// pointerDown starts a selection with the primary button, or pastes with the middle one: it
// takes the focus and does what the click, the double click or the triple click, with the
// modifiers held, asks for. A press inside the selection can start dragging its text instead.
func (s *editorState) pointerDown(e *gestures.PointerDownEvent, gutterWidth float64) {
	buttons := e.Buttons()
	middle := buttons&gestures.TertiaryButton != 0 && buttons&gestures.PrimaryButton == 0
	if buttons&gestures.PrimaryButton == 0 && !middle {
		return
	}
	s.node.RequestFocus()
	m := &s.mouse
	vm := s.Widget().vm
	ev := s.eventAt(e.LocalPosition(), gutterWidth)
	if s.startsFoldPress(e, ev, middle, gutterWidth) {
		return
	}
	ev.Mods = s.modifiers()
	ev.Middle = middle
	ev.Clicks = m.countClick(e.LocalPosition(), e.TimeStamp())
	if ev.OnLineNumbers {
		ev.Clicks = 1
	}
	wasPressed := m.pressed
	m.pressed, m.pointer, m.middle, m.moved = true, e.Pointer(), middle, false
	m.mods, m.onNumbers = ev.Mods, ev.OnLineNumbers
	m.last = ev
	s.stopAutoScroll()
	if !middle && !wasPressed && s.startsTextDrag(e, ev, gutterWidth) {
		return
	}
	if e.Kind() == ui.PointerDeviceKindTouch {
		ev.Mods = MouseModifiers{}
	}
	s.reportInputError(vm.DispatchMouse(ev))
}

// startsTextDrag begins dragging the selected text when the press is a single click on the
// text of the primary selection, which has something selected: Alt (many cursors), the column
// selection and a read-only document leave it to the usual selecting.
//
// Ported from VS Code: src/vs/editor/browser/controller/mouseHandler.ts (MouseDownOperation.start)
func (s *editorState) startsTextDrag(e *gestures.PointerDownEvent, ev MouseEvent, gutterWidth float64) bool {
	vm := s.Widget().vm
	opts := vm.Options()
	primary := vm.Cursors().Primary().Selection
	if !opts.DragAndDrop || opts.ColumnSelection || ev.Mods.Alt || ev.Clicks >= 2 || ev.OnLineNumbers ||
		vm.Document().ReadOnly() || primary.IsEmpty() || e.LocalPosition().Dx < gutterWidth ||
		!primary.Range().ContainsPosition(ev.Position) || !s.overText(e.LocalPosition(), gutterWidth, ev.Position) {
		return false
	}
	s.mouse.dnd = &textDrag{}
	if found := vm.selectionsAt(ev.Position); len(found) == 1 {
		s.mouse.dnd.pressed = &found[0]
	}
	s.mouse.cancelKey = s.binding.Keyboard().AddHandler(func(k services.KeyEvent) bool {
		if _, down := k.(*services.KeyDownEvent); down && k.LogicalKey() == services.LogicalKeyboardKeyEscape {
			s.endPress()
			return true
		}
		return false
	})
	return true
}

// overText says whether a point is over the text of its line, not in the empty space after it.
func (s *editorState) overText(local ui.Offset, gutterWidth float64, pos buffer.Position) bool {
	vm := s.Widget().vm
	return local.Dx-gutterWidth <= s.textRO.RectOf(vm.lineEndOf(pos)).Left
}

// pointerMove goes on selecting while the button is held: the pointer moves the end of the
// selection by characters, words or lines, as the press asked for. Beyond an edge of the editor
// it scrolls with every frame (see autoScrollFrame); with selected text dragged it moves the
// drop caret.
func (s *editorState) pointerMove(e *gestures.PointerMoveEvent, gutterWidth float64) {
	m := &s.mouse
	if !m.pressed || e.Pointer() != m.pointer || e.Kind() == ui.PointerDeviceKindTouch {
		return
	}
	m.moved = true
	vm := s.Widget().vm
	ev := s.eventAt(e.LocalPosition(), gutterWidth)
	if m.dnd != nil {
		s.dragText(ev.Position)
		return
	}
	edge, distance := s.edgeOf(e.LocalPosition(), gutterWidth)
	s.followEdge(edge, distance, e.LocalPosition(), ev.Position.Line)
	if edge != edgeNone {
		return
	}
	ev.Mods, ev.OnLineNumbers, ev.Clicks, ev.Drag, ev.Middle = m.mods, m.onNumbers, m.last.Clicks, true, m.middle
	if ev.Position == m.last.Position && ev.VisualColumn == m.last.VisualColumn && m.last.Drag {
		return
	}
	m.last = ev
	s.reportInputError(vm.DispatchMouse(ev))
}

// dragText follows the pointer of a drag of selected text: the pointer shows whether the drop
// copies (Control) or moves, and the drop caret goes where the text would land.
//
// Ported from VS Code: src/vs/editor/contrib/dnd/browser/dnd.ts (DragAndDropController._onEditorMouseDrag)
func (s *editorState) dragText(pos buffer.Position) {
	drag := s.mouse.dnd
	s.Widget().vm.dragOver(drag, pos)
	if drag.selection == nil {
		return
	}
	style := "default"
	if s.modifiers().Ctrl {
		style = "copy"
	}
	s.setDragStyle(style)
}

// setDragStyle changes the mouse pointer for the duration of a text drag ("" ends it).
func (s *editorState) setDragStyle(style string) {
	if s.dragStyle == style {
		return
	}
	s.dragStyle = style
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// mouseStyle is the mouse pointer over the text: the one of the setting, or the one of the
// drag of text that goes on.
func (s *editorState) mouseStyle(vm *ViewModel) string {
	if s.dragStyle != "" {
		return s.dragStyle
	}
	return vm.Options().MouseStyle
}

// endPress ends the press without doing anything more: the text drag is dropped nowhere.
func (s *editorState) endPress() {
	m := &s.mouse
	m.pressed = false
	s.stopAutoScroll()
	if m.dnd != nil {
		m.dnd = nil
		s.Widget().vm.endDrag()
		s.binding.Keyboard().RemoveHandler(m.cancelKey)
		s.setDragStyle("")
	}
}

// pointerUp ends the selection. After a drag of text it drops the text; a middle click that
// did not move pastes the primary selection.
func (s *editorState) pointerUp(e *gestures.PointerUpEvent, gutterWidth float64) {
	m := &s.mouse
	s.endFoldPress(e, gutterWidth)
	if !m.pressed || m.pointer != e.Pointer() {
		return
	}
	vm := s.Widget().vm
	drag, middle, moved := m.dnd, m.middle, m.moved
	local := e.LocalPosition()
	mods := s.modifiers()
	s.endPress()
	switch {
	case drag != nil:
		if s.insideEditor(local, gutterWidth) {
			ev := s.eventAt(local, gutterWidth)
			s.reportInputError(vm.drop(drag, ev.Position, mods.Shift, mods.Ctrl))
		}
	case middle && !moved && vm.Options().SelectionClipboard:
		s.reportInputError(vm.PastePrimary())
	}
}

// insideEditor says whether a point, in the coordinates of the row of gutter and text, is over
// the editor.
func (s *editorState) insideEditor(local ui.Offset, gutterWidth float64) bool {
	w, h := s.Widget().vm.ViewportSize()
	return local.Dx >= 0 && local.Dx <= gutterWidth+w && local.Dy >= 0 && local.Dy <= h
}

// pointerCancel ends the selection when the pointer's events stop reaching the editor.
func (s *editorState) pointerCancel(pointer int) {
	if s.mouse.pressed && s.mouse.pointer == pointer {
		s.endPress()
	}
}

// mouseCursor is the shape of the mouse pointer over the text, from editor.mouseStyle.
func mouseCursor(style string) services.MouseCursor {
	switch style {
	case "default":
		return services.SystemMouseCursors.Basic
	case "copy":
		return services.SystemMouseCursors.Copy
	}
	return services.SystemMouseCursors.Text
}

// pointerListener makes the widget that hands the pointer events of the editor's gutter and
// text to the selection.
func (s *editorState) pointerListener(child widgets.Widget, gutterWidth float64) widgets.Widget {
	return widgets.NewListener(child, widgets.ListenerOpts{
		Behavior:        rendering.HitTestBehaviorOpaque,
		OnPointerDown:   func(e *gestures.PointerDownEvent) { s.pointerDown(e, gutterWidth) },
		OnPointerMove:   func(e *gestures.PointerMoveEvent) { s.pointerMove(e, gutterWidth) },
		OnPointerUp:     func(e *gestures.PointerUpEvent) { s.pointerUp(e, gutterWidth) },
		OnPointerCancel: func(e *gestures.PointerCancelEvent) { s.pointerCancel(e.Pointer()) },

		OnPointerSignal:        s.pointerSignal,
		OnPointerPanZoomUpdate: s.panZoomUpdate,
	})
}

// foldTarget tells what a point is for the folding: the line of the control in the gutter it is
// on, or of the folded line whose placeholder (or, with editor.unfoldOnClickAfterEndOfLine, the
// empty space after its text) it is on.
//
// Ported from VS Code: src/vs/editor/contrib/folding/browser/folding.ts (onEditorMouseDown)
func (s *editorState) foldTarget(local ui.Offset, pos buffer.Position, gutterWidth float64) (line int, control, ok bool) {
	vm := s.Widget().vm
	opts := vm.Options()
	if !vm.foldingEnabled() {
		return 0, false, false
	}
	if local.Dx < gutterWidth {
		w := vm.GutterWidths(textFactory(s.Context()))
		left := w.GlyphMargin + w.LineNumbers
		// The first 4 pixels of the decorations are the space before the control.
		if opts.ShowFoldingControls == "never" || local.Dx < left+4 || vm.FoldingStateOf(pos.Line) == FoldingNone {
			return 0, false, false
		}
		return pos.Line, true, true
	}
	if s.textRO == nil || !vm.FoldedLine(pos.Line) {
		return 0, false, false
	}
	g := newGeometry(vm, textFactory(s.Context()))
	rect := g.foldPlaceholder(vm.RowOfPosition(pos))
	x := local.Dx - gutterWidth + vm.ScrollLeft()
	if x >= rect.Left && (x < rect.Right || opts.UnfoldOnClickAfterEndOfLine) {
		return pos.Line, false, true
	}
	return 0, false, false
}

// startsFoldPress notes a press on a folding control or a placeholder. A press on a control is
// the folding's alone (the cursor stays); one on a placeholder also places the cursor.
func (s *editorState) startsFoldPress(e *gestures.PointerDownEvent, ev MouseEvent, middle bool, gutterWidth float64) bool {
	s.mouse.fold = nil
	line, control, ok := s.foldTarget(e.LocalPosition(), ev.Position, gutterWidth)
	if !ok {
		return false
	}
	s.mouse.fold = &foldPress{line: line, control: control, pointer: e.Pointer(), middle: middle}
	return control
}

// endFoldPress folds or unfolds when the pointer is released on the control or the placeholder
// that was pressed. Alt works on the regions around, Shift or the middle button on the regions
// inside too.
func (s *editorState) endFoldPress(e *gestures.PointerUpEvent, gutterWidth float64) {
	fp := s.mouse.fold
	if fp == nil || fp.pointer != e.Pointer() {
		return
	}
	s.mouse.fold = nil
	ev := s.eventAt(e.LocalPosition(), gutterWidth)
	line, control, ok := s.foldTarget(e.LocalPosition(), ev.Position, gutterWidth)
	if !ok || line != fp.line || control != fp.control {
		return
	}
	mods := s.modifiers()
	vm := s.Widget().vm
	vm.ToggleFoldAt(line, control, mods.Alt, mods.Shift || fp.middle)
	vm.RevealPosition(buffer.Position{Line: line})
}
