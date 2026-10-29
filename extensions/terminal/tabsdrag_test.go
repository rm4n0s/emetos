package terminal

import (
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// The drag of a tab of the list (step 17d.7): inside the list it moves the tab, out of it it moves the terminal to an editor tab.

// mouse presses the primary button of a mouse.
func mouse() gluttertest.GestureOpts {
	return gluttertest.GestureOpts{Kind: gui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.PrimaryButton)}
}

// tabRowCenter is the middle of the row i of the tabs list in a window of the width.
func tabRowCenter(width float64, i int) gui.Offset {
	return gui.Offset{Dx: width - tabsWidth/2, Dy: float64(i)*tabRowH + tabRowH/2}
}

func TestDraggingATabInTheListMovesIt(t *testing.T) {
	const width = 900
	r := newPanelRig(t, uitest.Themes()[0], gui.Size{Width: width, Height: 300})
	r.waitShows(t, "ready")
	for range 2 {
		r.run(CmdNew, nil)
	}
	r.waitShows(t, "ready")
	es := append([]*entry(nil), r.ext.panelEntries()...)
	if len(es) != 3 {
		t.Fatalf("%d terminals", len(es))
	}
	tests := []struct {
		name     string
		from, to gui.Offset
		want     []int // the indexes of es in the order of the list after the drag
	}{
		{"a tab onto the last row goes after it", tabRowCenter(width, 0), tabRowCenter(width, 2), []int{1, 2, 0}},
		{"a tab onto the first row goes before it", tabRowCenter(width, 2), tabRowCenter(width, 0), []int{2, 0, 1}},
		{"below the last row is the end", tabRowCenter(width, 0), gui.Offset{Dx: width - tabsWidth/2, Dy: 3*tabRowH + 20}, []int{1, 2, 0}},
		{"a press that does not move is a click", tabRowCenter(width, 1), tabRowCenter(width, 1).Add(gui.Offset{Dx: 1}), []int{0, 1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// start from the order a, b, c every time
			r.ext.moveTab(es[0], nil)
			r.ext.moveTab(es[1], nil)
			r.ext.moveTab(es[2], nil)
			r.settle()
			g := r.ts.StartGesture(tt.from, mouse())
			g.MoveTo(tt.from.Add(gui.Offset{Dy: 2}))
			g.MoveTo(tt.to)
			r.settle()
			if tt.to != tt.from && r.ext.tabDrag != nil && r.ext.tabDrag.dragging && r.ext.dragOver == nil && !r.ext.dragEnd {
				t.Error("a drag over the list marks no place")
			}
			g.Up()
			r.settle()
			var got []int
			for _, e := range r.ext.panelEntries() {
				for i, x := range es {
					if x == e {
						got = append(got, i)
					}
				}
			}
			if !equalInts(got, tt.want) {
				t.Errorf("the order is %v, want %v", got, tt.want)
			}
			if r.ext.tabDrag != nil || r.ext.dragOver != nil || r.ext.dragEnd {
				t.Error("the drag left its state behind")
			}
			if len(r.api.Drags()) != 0 {
				t.Error("a drag inside the list began a drag of the window")
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDraggingATabOutOfTheListToTheEditorArea(t *testing.T) {
	const width = 900
	tests := []struct {
		name       string
		refuse     bool
		cancel     bool
		wantEditor bool
	}{
		{"dropped where a tab can go", false, false, true},
		{"dropped where nothing takes it", true, false, false},
		{"the pointer is taken away", false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newPanelRig(t, uitest.Themes()[0], gui.Size{Width: width, Height: 300})
			r.waitShows(t, "ready")
			r.run(CmdNew, nil)
			r.waitShows(t, "ready")
			e := r.ext.panelEntries()[0]
			pid, before := e.term.Pid(), r.ext.panelCount()
			from := tabRowCenter(width, 0)
			g := r.ts.StartGesture(from, mouse())
			g.MoveTo(from.Add(gui.Offset{Dy: 2}))
			out := gui.Offset{Dx: 300, Dy: 100}
			g.MoveTo(out)
			drags := r.api.Drags()
			if len(drags) != 1 || drags[0].Kind != EditorKindID || len(drags[0].Moves) == 0 || drags[0].Moves[len(drags[0].Moves)-1] != out {
				t.Fatalf("drags = %+v", drags)
			}
			drags[0].Refuse = tt.refuse
			if r.ext.dragOver != nil || r.ext.dragEnd {
				t.Error("a drag outside the list marks a place in it")
			}
			if tt.cancel {
				g.Cancel()
			} else {
				g.Up()
			}
			r.settle()
			if !drags[0].Ended || drags[0].Dropped != tt.wantEditor {
				t.Errorf("ended %v dropped %v, want dropped %v", drags[0].Ended, drags[0].Dropped, tt.wantEditor)
			}
			if e.inEditor() != tt.wantEditor || (r.ext.panelCount() == before-1) != tt.wantEditor {
				t.Fatalf("in editor %v, %d in the panel", e.inEditor(), r.ext.panelCount())
			}
			if e.term.Pid() != pid {
				t.Error("the process changed")
			}
			if _, exited := e.term.ExitCode(); exited {
				t.Error("the process ended")
			}
			if tt.wantEditor {
				if tab := drags[0].Tab; tab == nil || tab.Closed || !strings.Contains(string(drags[0].State), `"adopt"`) {
					t.Errorf("tab %+v, state %s", tab, drags[0].State)
				}
			}
		})
	}
}

func TestDraggingAfterTheTerminalEndedDropsNothing(t *testing.T) {
	const width = 900
	r := newPanelRig(t, uitest.Themes()[0], gui.Size{Width: width, Height: 300})
	r.waitShows(t, "ready")
	r.run(CmdNew, nil)
	r.waitShows(t, "ready")
	from := tabRowCenter(width, 1)
	g := r.ts.StartGesture(from, mouse())
	g.MoveTo(from.Add(gui.Offset{Dy: 2}))
	g.MoveTo(gui.Offset{Dx: 300, Dy: 100})
	// the terminal of the dragged tab is killed while the drag goes on
	r.ext.kill(r.ext.panelEntries()[1])
	g.Up()
	r.settle()
	if r.ext.panelCount() != 1 || len(r.ext.editors) != 0 {
		t.Errorf("%d in the panel, %d in editor tabs after the terminal ended during a drag", r.ext.panelCount(), len(r.ext.editors))
	}
}
