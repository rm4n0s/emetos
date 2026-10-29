package app

import (
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// The drag of something an extension began (EditorHost.BeginDrag, step 17d.7): shown like the drag of a tab, and a drop over a
// tab strip or the editor area of a group asks the extension to open its tab there.

// begin starts a drag with the host of the window and gives it the pointer.
func (a *groupApp) beginDrag(label string) exthost.DragSession {
	a.t.Helper()
	s, err := extEditorHost{w: a.win}.BeginDrag(label)
	if err != nil {
		a.t.Fatal(err)
	}
	return s
}

// dropAt moves a drag to a position and ends it there; it returns where open was asked to put the tab (-1, -1 for nowhere).
func (a *groupApp) dropAt(s exthost.DragSession, at ui.Offset, drop bool) (group, index int, opened bool) {
	a.t.Helper()
	group, index = -1, -1
	s.Move(at)
	a.pump()
	opened, err := s.End(at, drop, func(g, i int) error { group, index = g, i; return nil })
	if err != nil {
		a.t.Fatal(err)
	}
	a.pump()
	return group, index, opened
}

func TestADragOfAnExtensionIsShownAndDroppedOnAStripOrAnArea(t *testing.T) {
	tests := []struct {
		name string
		// at is the place of the drop in the window, from a group's strip and area
		at func(a *groupApp) ui.Offset
		// drop says that the pointer is released; wantGroup and wantIndex where the tab is asked for, -1 for nowhere; groups is the
		// number of groups after the drop
		drop      bool
		wantGroup int
		wantIndex int
		groups    int
	}{
		{"over the strip, between the first two tabs", func(a *groupApp) ui.Offset {
			r := a.tabRect(1, "a.go")
			return ui.Offset{Dx: r.Right - 4, Dy: r.Top + 15}
		}, true, 1, 1, 1},
		{"over the strip, after the last tab", func(a *groupApp) ui.Offset {
			r := a.tabRect(1, "b.go")
			return ui.Offset{Dx: r.Right - 4, Dy: r.Top + 15}
		}, true, 1, 2, 1},
		{"over the middle of the area", func(a *groupApp) ui.Offset { return center(a.editorArea(1)) }, true, 1, -1, 1},
		{"over the right edge of the area splits it", func(a *groupApp) ui.Offset {
			r := a.editorArea(1)
			return ui.Offset{Dx: r.Right - 8, Dy: (r.Top + r.Bottom) / 2}
		}, true, 2, -1, 2},
		{"over the left edge of the area splits it", func(a *groupApp) ui.Offset {
			r := a.editorArea(1)
			return ui.Offset{Dx: r.Left + 8, Dy: (r.Top + r.Bottom) / 2}
		}, true, 2, -1, 2},
		{"over nothing a tab can go to", func(*groupApp) ui.Offset { return ui.Offset{Dx: 2, Dy: 2} }, true, -1, -1, 1},
		{"released over the area, but the drag was cancelled", func(a *groupApp) ui.Offset { return center(a.editorArea(1)) }, false, -1, -1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
			s := a.beginDrag("Dragged terminal")
			if a.shell().group.drag == nil || !a.shell().group.drag.external || a.texts("Dragged terminal") != 0 {
				t.Fatalf("the drag is not there before the pointer moves: %+v", a.shell().group.drag)
			}
			at := tt.at(a)
			s.Move(at)
			a.pump()
			if got := a.texts("Dragged terminal"); got != 1 {
				t.Errorf("%d labels follow the pointer, want 1", got)
			}
			group, index, opened := a.dropAt(s, at, tt.drop)
			if wantOpened := tt.wantGroup >= 0; opened != wantOpened {
				t.Errorf("opened = %v, want %v", opened, wantOpened)
			}
			if tt.wantGroup > 0 && (group == -1 || (tt.wantGroup == 1 && group != a.group(1).ID())) || tt.wantGroup < 0 && group != -1 {
				t.Errorf("the tab was asked for in group %d, want %d", group, tt.wantGroup)
			}
			if index != tt.wantIndex {
				t.Errorf("the tab was asked for at index %d, want %d", index, tt.wantIndex)
			}
			if got := len(a.grid().Groups(0)); got != tt.groups {
				t.Errorf("%d groups after the drop, want %d", got, tt.groups)
			}
			if a.shell().group.drag != nil || a.texts("Dragged terminal") != 0 {
				t.Error("the drag is still shown after it ended")
			}
		})
	}
}

func TestOnlyOneDragGoesOnAndItsErrorsHaveRoutes(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	s := a.beginDrag("One")
	_, err := extEditorHost{w: a.win}.BeginDrag("Two")
	routeIs(t, err, "extEditorHost.BeginDrag.ExtEditorsDragBusy")
	// a drag of a tab does not start while an extension's goes on
	if a.shell().dragStart(a.group(1), 0) {
		t.Error("a tab began to be dragged during the drag of an extension")
	}
	if _, _, opened := a.dropAt(s, ui.Offset{Dx: 1, Dy: 1}, false); opened {
		t.Error("a cancelled drag opened a tab")
	}
	// the window that is not on screen
	_, err = extEditorHost{w: &Workspace{}}.BeginDrag("Three")
	routeIs(t, err, "extEditorHost.shellOrErr.ExtEditorsUnavailable")
}

// routeIs asserts that the error is from the errors package and that its route has the text.
func routeIs(t *testing.T, err error, want string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}
