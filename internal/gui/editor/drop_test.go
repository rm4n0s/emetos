package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

const dropText = "hello world\nfoo bar baz\nthe end"

// dropRig is an editor with "world" selected (by a double click).
func dropRig(t *testing.T, tweak func(*Options)) *mouseRig {
	t.Helper()
	r := newMouseRig(t, dropText, tweak)
	r.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 6, 0, 11)})
	r.vm.SelectionsChanged()
	r.ts.Pump()
	return r
}

func (r *mouseRig) text() string { return r.vm.Document().Text() }

func TestDraggingSelectedTextMovesIt(t *testing.T) {
	r := dropRig(t, nil)
	g := r.ts.StartGesture(r.at(0, 8), mouseOpts())
	if r.selections()[0] != sel(0, 6, 0, 11) {
		t.Fatalf("a press on the selection keeps it: %v", r.selections())
	}
	g.MoveTo(r.at(1, 4)) // the start of "bar"
	if p, ok := r.vm.DropCaret(); !ok || p != positionOf(1, 4) {
		t.Errorf("drop caret %v %v", p, ok)
	}
	g.Up()
	r.ts.Pump(time.Second)
	if want := "hello \nfoo worldbar baz\nthe end"; r.text() != want {
		t.Errorf("text %q, want %q", r.text(), want)
	}
	r.want(sel(1, 4, 1, 9))
	if _, ok := r.vm.DropCaret(); ok {
		t.Error("the drop caret is gone after the drop")
	}
	noErrors(t, r.ts)
}

func TestDraggingSelectedTextBackwards(t *testing.T) {
	r := dropRig(t, nil)
	r.drag(r.at(0, 8), r.at(0, 0))
	if want := "worldhello \nfoo bar baz\nthe end"; r.text() != want {
		t.Errorf("text %q, want %q", r.text(), want)
	}
	r.want(sel(0, 0, 0, 5))
}

func TestControlDragCopiesTheText(t *testing.T) {
	r := dropRig(t, nil)
	release := r.holdCtrl()
	r.drag(r.at(0, 8), r.at(2, 3))
	release()
	if want := "hello world\nfoo bar baz\ntheworld end"; r.text() != want {
		t.Errorf("text %q, want %q", r.text(), want)
	}
	r.want(sel(2, 3, 2, 8))
}

func TestTheDropIsOneUndoStep(t *testing.T) {
	r := dropRig(t, nil)
	r.drag(r.at(0, 8), r.at(1, 4))
	if _, err := r.vm.Document().Undo(); err != nil {
		t.Fatal(err)
	}
	if r.text() != dropText {
		t.Errorf("undo: %q", r.text())
	}
}

func TestDroppingOnTheSelectionChangesNothing(t *testing.T) {
	r := dropRig(t, nil)
	r.drag(r.at(0, 8), r.at(0, 9))
	if r.text() != dropText {
		t.Errorf("text %q", r.text())
	}
	r.want(sel(0, 6, 0, 11))
}

func TestNoDropCaretOverTheDraggedText(t *testing.T) {
	r := dropRig(t, nil)
	g := r.ts.StartGesture(r.at(0, 8), mouseOpts())
	g.MoveTo(r.at(0, 9))
	if _, ok := r.vm.DropCaret(); ok {
		t.Error("a drop caret over the text being dragged")
	}
	g.MoveTo(r.at(2, 1))
	if _, ok := r.vm.DropCaret(); !ok {
		t.Error("no drop caret elsewhere")
	}
	g.Up()
}

func TestPressInsideTheSelectionWithoutMovingPlacesTheCursor(t *testing.T) {
	r := dropRig(t, nil)
	r.click(r.at(0, 8))
	r.want(sel(0, 8, 0, 8))
	if r.text() != dropText {
		t.Errorf("text %q", r.text())
	}
}

func TestShiftClickOnTheSelectionExtendsFromTheAnchor(t *testing.T) {
	// The press with Shift is a drag of the text too, and its release extends the selection.
	r := dropRig(t, nil)
	release := r.holdShift()
	r.click(r.at(0, 8))
	release()
	r.want(sel(0, 6, 0, 8))
}

func TestOtherSelectionsStayWhenAClickInOneEndsIt(t *testing.T) {
	r := dropRig(t, nil)
	r.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 6, 0, 11), sel(1, 0, 1, 3)})
	r.vm.SelectionsChanged()
	r.click(r.at(0, 8))
	r.want(sel(0, 8, 0, 8), sel(1, 0, 1, 3))
}

func TestDraggingWithManySelectionsMovesTheOneUnderThePointer(t *testing.T) {
	r := dropRig(t, nil)
	r.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 6, 0, 11), sel(1, 0, 1, 3)})
	r.vm.SelectionsChanged()
	r.drag(r.at(0, 8), r.at(2, 4))
	if want := "hello \nfoo bar baz\nthe worldend"; r.text() != want {
		t.Errorf("text %q, want %q", r.text(), want)
	}
}

func TestEscapeCancelsTheDrop(t *testing.T) {
	r := dropRig(t, nil)
	g := r.ts.StartGesture(r.at(0, 8), mouseOpts())
	g.MoveTo(r.at(2, 3))
	if _, ok := r.vm.DropCaret(); !ok {
		t.Fatal("dragging")
	}
	r.ts.SendKeyEvent(services.LogicalKeyboardKeyEscape, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyEscape})
	if _, ok := r.vm.DropCaret(); ok {
		t.Error("the drop caret stays after Escape")
	}
	g.Up()
	if r.text() != dropText {
		t.Errorf("text %q", r.text())
	}
	r.want(sel(0, 6, 0, 11))
	// Escape is a key again once the drag is over: no handler stays.
	if state := r.editorState(t); state.mouse.dnd != nil || state.mouse.pressed {
		t.Error("the drag is over")
	}
}

func (r *mouseRig) editorState(t *testing.T) *editorState {
	t.Helper()
	el, ok := r.ts.Element(gluttertest.ByType[*Editor]()).(*widgets.StatefulElement)
	if !ok {
		t.Fatal("the editor is a stateful widget")
	}
	return el.State().(*editorState)
}

func TestDroppingOutsideTheEditorDoesNothing(t *testing.T) {
	r := dropRig(t, nil)
	r.drag(r.at(0, 8), ui.Offset{Dx: 900, Dy: 200})
	if r.text() != dropText {
		t.Errorf("text %q", r.text())
	}
	r.want(sel(0, 6, 0, 11))
}

func TestDragAndDropOffSelectsInstead(t *testing.T) {
	r := dropRig(t, func(o *Options) { o.DragAndDrop = false })
	r.drag(r.at(0, 8), r.at(1, 4))
	if r.text() != dropText {
		t.Errorf("text %q", r.text())
	}
	r.want(sel(0, 8, 1, 4))
}

func TestDragAndDropNeedsASingleClickWithoutAlt(t *testing.T) {
	r := dropRig(t, nil)
	release := r.holdAlt()
	r.click(r.at(0, 8))
	release()
	// Alt does not drag the text: the click adds a cursor, which the selection takes in.
	r.want(sel(0, 6, 0, 11))
	r.drag(r.at(1, 0), r.at(1, 3))
	r = dropRig(t, nil)
	r.clicks(r.at(0, 8), 2)
	r.want(sel(0, 6, 0, 11)) // a double click selects the word
	r.click(r.at(0, 8))
	r.drag(r.at(0, 2), r.at(0, 4)) // outside the selection: a normal selection
	r.want(sel(0, 2, 0, 4))
}

func TestPressPastTheEndOfALineIsNotADrag(t *testing.T) {
	r := dropRig(t, nil)
	r.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 6, 1, 3)})
	r.vm.SelectionsChanged()
	r.drag(r.at(0, 40), r.at(1, 2)) // in the empty space after "hello world": the selection reaches it
	r.want(sel(0, 11, 1, 2))
}

func TestReadOnlyDocumentsDoNotDragText(t *testing.T) {
	r := dropRig(t, nil)
	r.vm.Document().SetReadOnly(true)
	r.drag(r.at(0, 8), r.at(1, 4))
	r.want(sel(0, 8, 1, 4))
	if r.text() != dropText {
		t.Errorf("text %q", r.text())
	}
}

func TestColumnSelectionSettingDoesNotDragText(t *testing.T) {
	r := dropRig(t, func(o *Options) { o.ColumnSelection = true })
	r.drag(r.at(0, 8), r.at(1, 4))
	if r.text() != dropText {
		t.Errorf("text %q", r.text())
	}
}

func TestMousePointerShowsCopyOrMoveWhileDragging(t *testing.T) {
	r := dropRig(t, nil)
	var shown []string
	r.ts.Binding.Renderer().MouseTracker().SetSystemCursorActivator(func(_ int, kind string) { shown = append(shown, kind) })
	last := func() string {
		if len(shown) == 0 {
			return ""
		}
		return shown[len(shown)-1]
	}
	g := r.ts.StartGesture(r.at(0, 8), mouseOpts())
	g.MoveTo(r.at(1, 4))
	r.ts.Pump()
	if last() != services.SystemMouseCursors.Basic.Kind() {
		t.Errorf("moving shows %q", last())
	}
	release := r.holdCtrl()
	g.MoveTo(r.at(1, 5))
	r.ts.Pump()
	if last() != services.SystemMouseCursors.Copy.Kind() {
		t.Errorf("copying shows %q", last())
	}
	release()
	g.Up()
	r.ts.Pump()
	r.ts.Hover(r.at(2, 1))
	if last() != services.SystemMouseCursors.Text.Kind() {
		t.Errorf("after the drop the pointer shows %q", last())
	}
}

func TestMiddleClickPastesThePrimarySelection(t *testing.T) {
	r := newMouseRig(t, dropText, nil)
	r.ts.SetPrimaryText("XY")
	r.ts.SetClipboardText("not this")
	r.ts.TapAt(r.at(1, 4), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.TertiaryButton)})
	r.ts.Pump(time.Second)
	if want := "hello world\nfoo XYbar baz\nthe end"; r.text() != want {
		t.Errorf("text %q, want %q", r.text(), want)
	}
	r.want(sel(1, 6, 1, 6))
	noErrors(t, r.ts)
}

func TestMiddleClickReplacesNothingWithAnEmptyPrimarySelection(t *testing.T) {
	r := newMouseRig(t, dropText, nil)
	r.ts.TapAt(r.at(1, 4), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.TertiaryButton)})
	r.ts.Pump(time.Second)
	if r.text() != dropText {
		t.Errorf("text %q", r.text())
	}
	r.want(sel(1, 4, 1, 4))
}

func TestMiddleDragDoesNotPaste(t *testing.T) {
	r := newMouseRig(t, dropText, nil)
	r.ts.SetPrimaryText("XY")
	g := r.ts.StartGesture(r.at(1, 4), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.TertiaryButton)})
	g.MoveTo(r.at(1, 8))
	g.Up()
	r.ts.Pump(time.Second)
	if r.text() != dropText {
		t.Errorf("text %q", r.text())
	}
	r.want(sel(1, 4, 1, 8))
}

func TestMiddleClickOnAReadOnlyDocumentReportsAnError(t *testing.T) {
	var reported []error
	r := newMouseRigWith(t, dropText, nil, EditorOpts{Autofocus: true, OnError: func(err error) { reported = append(reported, err) }})
	r.vm.Document().SetReadOnly(true)
	r.ts.SetPrimaryText("XY")
	r.ts.TapAt(r.at(1, 4), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.TertiaryButton)})
	r.ts.Pump(time.Second)
	if r.text() != dropText {
		t.Errorf("text %q", r.text())
	}
	if len(reported) != 1 {
		t.Errorf("errors reported: %v", reported)
	}
}

func TestMiddleButtonSelectsColumnsWithoutThePrimarySelection(t *testing.T) {
	r := newMouseRig(t, dropText, func(o *Options) { o.SelectionClipboard = false })
	r.ts.SetPrimaryText("XY")
	g := r.ts.StartGesture(r.at(0, 2), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.TertiaryButton)})
	g.MoveTo(r.at(2, 5))
	g.Up()
	r.ts.Pump(time.Second)
	r.want(sel(0, 2, 0, 5), sel(1, 2, 1, 5), sel(2, 2, 2, 5))
	if r.text() != dropText {
		t.Errorf("nothing is pasted: %q", r.text())
	}
}

func TestGoldenDropCaret(t *testing.T) {
	r := dropRig(t, nil)
	g := r.ts.StartGesture(r.at(0, 8), mouseOpts())
	g.MoveTo(r.at(1, 4))
	r.ts.Pump()
	r.ts.MatchesGolden("drop_caret")
	g.Up()
}

func TestSecondaryClickMovesTheCursorUnlessASelectionHoldsThePlace(t *testing.T) {
	right := gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)}
	cases := []struct {
		name  string
		start []buffer.Selection
		at    [2]int
		want  buffer.Selection
	}{
		{"outside a selection the cursor goes there", []buffer.Selection{sel(0, 0, 0, 3)}, [2]int{1, 4}, sel(1, 4, 1, 4)},
		{"inside a selection it stays", []buffer.Selection{sel(1, 2, 1, 8)}, [2]int{1, 4}, sel(1, 2, 1, 8)},
		{"at the edge of a selection it stays", []buffer.Selection{sel(1, 2, 1, 8)}, [2]int{1, 8}, sel(1, 2, 1, 8)},
		{"with no selection the cursor goes there", []buffer.Selection{sel(0, 0, 0, 0)}, [2]int{2, 2}, sel(2, 2, 2, 2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMouseRig(t, dropText, nil)
			r.vm.Cursors().SetSelections(tc.start)
			r.vm.SelectionsChanged()
			r.ts.TapAt(r.at(tc.at[0], tc.at[1]), right)
			r.ts.Pump(time.Second)
			r.want(tc.want)
			if r.text() != dropText {
				t.Errorf("the text changed: %q", r.text())
			}
			noErrors(t, r.ts)
		})
	}
}
