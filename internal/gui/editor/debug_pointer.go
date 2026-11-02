package editor

import (
	"time"

	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The pointer and the debugger (step 25.5): a press on an inline breakpoint or on a candidate, and the colors of the marks.

// debugColorsOf reads the colors of the debug marks from the theme.
func debugColorsOf(colors kit.Colors) DebugColors {
	get := func(id string, fallback ui.Color) ui.Color {
		if c, ok := colors.Named(id); ok {
			return c
		}
		return fallback
	}
	none := ui.Color{}
	return DebugColors{
		Breakpoint:           get("debugIcon.breakpointForeground", none),
		BreakpointDisabled:   get("debugIcon.breakpointDisabledForeground", none),
		BreakpointUnverified: get("debugIcon.breakpointUnverifiedForeground", none),
		StackFrame:           get("debugIcon.breakpointCurrentStackframeForeground", none),
		FocusedFrame:         get("debugIcon.breakpointStackframeForeground", none),
		StackFrameLine:       get("editor.stackFrameHighlightBackground", none),
		FocusedFrameLine:     get("editor.focusedStackFrameHighlightBackground", none),
		RulerBreakpoint:      get("debugIcon.breakpointForeground", none),
	}
}

// debugInlinePress handles a press of the primary button on an inline breakpoint or on a candidate: the host sets, removes or enables
// the breakpoint there. It says whether the press was used.
//
// Ported from VS Code: breakpointEditorContribution.ts (InlineBreakpointWidget, the click listener)
func (s *editorState) debugInlinePress(e *gestures.PointerDownEvent, gutterWidth float64) bool {
	vm := s.Widget().vm
	h := vm.dbg.host
	if h == nil || h.InlinePress == nil || e.Buttons()&gestures.PrimaryButton == 0 || e.LocalPosition().Dx < gutterWidth || s.textRO == nil {
		return false
	}
	in := ui.Offset{Dx: e.LocalPosition().Dx - gutterWidth, Dy: e.LocalPosition().Dy}
	line, col, id, ok := s.textRO.DebugInlineAt(in)
	if !ok {
		return false
	}
	h.InlinePress(line, col, id)
	return true
}

// noteDebugMenu remembers where a press of the secondary button was, for the menu of the breakpoints: in the glyph margin or on an inline
// mark.
func (s *editorState) noteDebugMenu(e *gestures.PointerDownEvent, gutterWidth float64) {
	vm := s.Widget().vm
	local := e.LocalPosition()
	if local.Dx < gutterWidth {
		vm.noteDebugMenuTarget(local.Dx, local.Dy)
		vm.noteTestMenuTarget(local.Dx, local.Dy)
		return
	}
	vm.dbg.menu = DebugMenuTarget{Col: -1}
	if s.textRO == nil || !vm.debugEnabled() {
		return
	}
	in := ui.Offset{Dx: local.Dx - gutterWidth, Dy: local.Dy}
	if line, col, id, ok := s.textRO.DebugInlineAt(in); ok {
		vm.dbg.menu = DebugMenuTarget{Line: line, Col: col, ID: id, InGlyph: true}
	}
}

// focusDebugInput gives the keyboard to the input of a widget of the debugger once it is on screen: the frame after the widget was added.
func (s *editorState) focusDebugInput(node *widgets.FocusNode) {
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if s.Mounted() {
			node.RequestFocus()
		}
	})
}
