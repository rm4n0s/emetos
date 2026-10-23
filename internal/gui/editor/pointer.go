package editor

import (
	"time"

	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
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

// pointerDown starts a selection with the primary button: it takes the focus and does what
// the click, the double click or the triple click, with the modifiers held, asks for.
func (s *editorState) pointerDown(e *gestures.PointerDownEvent, gutterWidth float64) {
	if e.Buttons()&gestures.PrimaryButton == 0 {
		return
	}
	s.node.RequestFocus()
	m := &s.mouse
	ev := s.eventAt(e.LocalPosition(), gutterWidth)
	ev.Mods = s.modifiers()
	ev.Clicks = m.countClick(e.LocalPosition(), e.TimeStamp())
	if ev.OnLineNumbers {
		ev.Clicks = 1
	}
	m.pressed, m.pointer = true, e.Pointer()
	m.mods, m.onNumbers = ev.Mods, ev.OnLineNumbers
	m.last = ev
	if e.Kind() == ui.PointerDeviceKindTouch {
		ev.Mods = MouseModifiers{}
	}
	s.reportInputError(s.Widget().vm.DispatchMouse(ev))
}

// pointerMove goes on selecting while the button is held: the pointer moves the end of the
// selection by characters, words or lines, as the press asked for.
func (s *editorState) pointerMove(e *gestures.PointerMoveEvent, gutterWidth float64) {
	m := &s.mouse
	if !m.pressed || e.Pointer() != m.pointer || e.Kind() == ui.PointerDeviceKindTouch {
		return
	}
	ev := s.eventAt(e.LocalPosition(), gutterWidth)
	ev.Mods, ev.OnLineNumbers, ev.Clicks, ev.Drag = m.mods, m.onNumbers, m.last.Clicks, true
	if ev.Position == m.last.Position && ev.VisualColumn == m.last.VisualColumn && m.last.Drag {
		return
	}
	m.last = ev
	s.reportInputError(s.Widget().vm.DispatchMouse(ev))
}

// pointerUp ends the selection.
func (s *editorState) pointerUp(pointer int) {
	if s.mouse.pressed && s.mouse.pointer == pointer {
		s.mouse.pressed = false
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
		OnPointerUp:     func(e *gestures.PointerUpEvent) { s.pointerUp(e.Pointer()) },
		OnPointerCancel: func(e *gestures.PointerCancelEvent) { s.pointerUp(e.Pointer()) },
	})
}
