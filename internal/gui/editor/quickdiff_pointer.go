package editor

import (
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/ui"
)

// The click on the stripe of a change (SPEC §10.30 "The peek", step 24.12; scm.diffDecorationsGutterAction "diff"): a press on a stripe and
// the release on the same line open the peek of the change, or close it when it shows that change. The press moves no cursor.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/quickDiffWidget.ts (QuickDiffEditorController.onEditorMouseDown and
// onEditorMouseUp)

// stripePress is a press on a stripe that the release decides.
type stripePress struct {
	line    int
	pointer int
}

// startsStripePress notes a press of the primary button on the stripe of a change.
func (s *editorState) startsStripePress(e *gestures.PointerDownEvent, ev MouseEvent, gutterWidth float64) bool {
	s.mouse.stripe = nil
	local := e.LocalPosition()
	if e.Buttons()&gestures.PrimaryButton == 0 || local.Dx >= gutterWidth {
		return false
	}
	vm := s.Widget().vm
	line, ok := vm.quickDiffStripeAt(local.Dx, ev.Position.Line, vm.GutterWidths(textFactory(s.Context())))
	if !ok {
		return false
	}
	s.mouse.stripe = &stripePress{line: line, pointer: e.Pointer()}
	return true
}

// endStripePress opens (or closes) the peek when the pointer is released on the stripe of the line that was pressed.
func (s *editorState) endStripePress(e *gestures.PointerUpEvent, gutterWidth float64) {
	sp := s.mouse.stripe
	if sp == nil || sp.pointer != e.Pointer() {
		return
	}
	s.mouse.stripe = nil
	local := e.LocalPosition()
	if local.Dx >= gutterWidth {
		return
	}
	vm := s.Widget().vm
	ev := s.eventAt(ui.Offset{Dx: local.Dx, Dy: local.Dy}, gutterWidth)
	line, ok := vm.quickDiffStripeAt(local.Dx, ev.Position.Line, vm.GutterWidths(textFactory(s.Context())))
	if !ok || line != sp.line || vm.qd.peeker == nil {
		return
	}
	vm.qd.peeker.Click(line + 1)
}
