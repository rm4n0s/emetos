package scm

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
)

// TestDraggingARowOpensTheFile checks that pulling a file row past the slop starts the drag of the window (with the file name as
// its label), tells it where the pointer goes, and at the release opens the file in the group and at the place the window gives;
// that a click moves nothing; and that a group row cannot be dragged.
func TestDraggingARowOpensTheFile(t *testing.T) {
	r := newRig(t)
	r.addRepo("a", gitGroups()...)
	r.show(goldenThemes[0].colors, ui.Size{Width: 300, Height: 300})
	mouse := gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse}

	// row 3 is notes.md of the staged group
	r.ts.DragFrom(ui.Offset{Dx: 100, Dy: r.y(3)}, ui.Offset{Dx: 200, Dy: 10}, mouse)
	r.settle()
	if len(r.drags) != 1 {
		t.Fatalf("%d drags started, want 1", len(r.drags))
	}
	d := r.drags[0]
	if d.label != "notes.md" || len(d.moves) == 0 || !d.ended || !d.drop {
		t.Errorf("the drag is %+v", d)
	}
	if last := d.moves[len(d.moves)-1]; last.Dx < 250 {
		t.Errorf("the drag was last told the pointer is at %v", last)
	}
	if len(r.opened) != 1 || r.opened[0] != (dropped{"file:///work/a/docs/notes.md", 2, 1}) {
		t.Errorf("opened %+v", r.opened)
	}

	// a click is no drag, and neither is a move below the slop
	r.drags, r.opened = nil, nil
	r.tapRow(3)
	g := r.ts.StartGesture(ui.Offset{Dx: 100, Dy: r.y(3)}, mouse)
	g.MoveBy(ui.Offset{Dx: 2, Dy: 1})
	g.Up()
	r.settle()
	if len(r.drags) != 0 || len(r.opened) != 0 {
		t.Errorf("a click or a small move dragged: %+v %+v", r.drags, r.opened)
	}

	// a group row is not dragged
	r.drags, r.opened = nil, nil
	r.ts.DragFrom(ui.Offset{Dx: 100, Dy: r.y(2)}, ui.Offset{Dx: 200, Dy: 10}, mouse)
	r.settle()
	if len(r.drags) != 0 {
		t.Errorf("a group was dragged")
	}
	r.noErrors()
}
