package app

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The tests of dragging a tab: to reorder, to another strip, into another group and to split.

var primaryDown = gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.PrimaryButton)}

// tabRect is the place of a tab of a group on the screen.
func (a *groupApp) tabRect(id int, name string) ui.Rect {
	a.t.Helper()
	v := a.shell().view(a.group(id))
	for i, n := range a.names(id) {
		if n == name {
			r, ok := v.tabs.TabRect(i)
			if !ok {
				a.t.Fatalf("tab %s of group %d is not on screen", name, id)
			}
			return r
		}
	}
	a.t.Fatalf("no tab %s in group %d (%v)", name, id, a.names(id))
	return ui.Rect{}
}

func center(r ui.Rect) ui.Offset {
	return ui.Offset{Dx: (r.Left + r.Right) / 2, Dy: (r.Top + r.Bottom) / 2}
}

// strip is the place of the tab strip of a group.
func (a *groupApp) strip(id int) ui.Rect {
	a.t.Helper()
	r, ok := a.shell().view(a.group(id)).tabs.Bounds()
	if !ok {
		a.t.Fatalf("group %d has no tab strip", id)
	}
	return r
}

// area is the editor area of a group: what is below its tab strip.
func (a *groupApp) editorArea(id int) ui.Rect {
	a.t.Helper()
	g, ok := a.shell().view(a.group(id)).area.Rect()
	if !ok {
		a.t.Fatalf("group %d is not on screen", id)
	}
	if a.group(id).Model.Count() == 0 {
		return g
	}
	return ui.NewRectFromLTRB(g.Left, a.strip(id).Bottom, g.Right, g.Bottom)
}

// texts is how many texts show a name: the tab, the breadcrumbs, the label of a drag.
func (a *groupApp) texts(name string) int { return a.ts.Count(gluttertest.ByText(name)) }

// dragTab presses a tab, moves the pointer past the slop and on to a position, and gives the gesture,
// with the pointer still down. The tab is active after the press: a.base is how many texts show its
// name then, before the label of the drag adds one.
func (a *groupApp) dragTab(id int, name string, to ui.Offset) *gluttertest.TestGesture {
	a.t.Helper()
	from := center(a.tabRect(id, name))
	g := a.ts.StartGesture(from, primaryDown)
	a.pump()
	a.base = a.texts(name)
	g.MoveTo(ui.Offset{Dx: from.Dx + 6, Dy: from.Dy})
	g.MoveTo(to)
	a.pump()
	return g
}

func (a *groupApp) wantTabs(id int, want ...string) {
	a.t.Helper()
	if got := a.names(id); !slices.Equal(got, want) {
		a.t.Errorf("group %d has the tabs %v, want %v", id, got, want)
	}
}

// twoGroups is a window with group 1 showing a, b, c and group 2, on its right, showing a (the copy that
// the split makes) and d.
func twoGroups(t *testing.T) *groupApp {
	t.Helper()
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go", "d.go"}, "a.go", "b.go", "c.go")
	a.run("workbench.action.splitEditorRight")
	a.show("d.go", false)
	if a.shape() != "H(1 2)" {
		t.Fatalf("the grid is %s", a.shape())
	}
	// the keyboard focus of the new group arrives a few frames after the split: group 2 is the active
	// group, as the user left it
	if err := a.grid().Activate(a.group(2)); err != nil {
		t.Fatal(err)
	}
	a.pump()
	a.wantTabs(1, "a.go", "b.go", "c.go")
	a.wantTabs(2, "a.go", "d.go")
	return a
}

func TestDraggingATabToReorderTheStrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		tab  string
		// over is the tab the pointer ends over, and right says in its right half
		over  string
		right bool
		want  []string
		line  int // the place of the line before the drop
	}{
		{"to the end", "a.go", "c.go", true, []string{"b.go", "c.go", "a.go"}, 3},
		{"to the start", "c.go", "a.go", false, []string{"c.go", "a.go", "b.go"}, 0},
		{"between two tabs", "a.go", "b.go", false, []string{"a.go", "b.go", "c.go"}, 1},
		{"after the next tab", "a.go", "b.go", true, []string{"b.go", "a.go", "c.go"}, 2},
		{"on itself nothing changes", "b.go", "b.go", false, []string{"a.go", "b.go", "c.go"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newGroupApp(t, []string{"a.go", "b.go", "c.go"}, "a.go", "b.go", "c.go")
			over := a.tabRect(1, tc.over)
			x := over.Left + over.Width()/4
			if tc.right {
				x = over.Left + over.Width()*3/4
			}
			g := a.dragTab(1, tc.tab, ui.Offset{Dx: x, Dy: over.Top + 15})
			if got := a.shell().view(a.group(1)).feedback.Index(); got != tc.line {
				t.Errorf("the line is at %d, want %d", got, tc.line)
			}
			// the label of the drag follows the pointer
			if n := a.texts(tc.tab); n != a.base+1 {
				t.Errorf("%d texts %s, want %d: the label of the drag too", n, tc.tab, a.base+1)
			}
			g.Up()
			a.pump()
			a.wantTabs(1, tc.want...)
			if got := a.shell().view(a.group(1)).feedback.Index(); got != -1 {
				t.Errorf("the line stays at %d after the drop", got)
			}
			if n := a.texts(tc.tab); n != a.base {
				t.Errorf("%d texts %s after the drop, want %d: the label of the drag is gone", n, tc.tab, a.base)
			}
			// a drag activates the tab it started on (the press does), and the order is kept
			if a.shell().drag() != nil {
				t.Error("the drag is not over")
			}
			a.noErrors()
		})
	}
}

// drag is the drag that goes on, for the tests.
func (s *shellState) drag() *tabDrag { return s.group.drag }

func TestDraggingATabToAnotherStrip(t *testing.T) {
	a := twoGroups(t)
	d := a.tabRect(2, "d.go")
	// the left half of d.go: between the two tabs of group 2
	g := a.dragTab(1, "c.go", ui.Offset{Dx: d.Left + 10, Dy: d.Top + 15})
	if got := a.shell().view(a.group(2)).feedback.Index(); got != 1 {
		t.Errorf("the line of group 2 is at %d, want 1", got)
	}
	if got := a.shell().view(a.group(1)).feedback.Index(); got != -1 {
		t.Errorf("group 1 shows a line at %d", got)
	}
	g.Up()
	a.pump()
	a.wantTabs(1, "a.go", "b.go")
	a.wantTabs(2, "a.go", "c.go", "d.go")
	if a.activeID() != 2 {
		t.Errorf("group %d is active, want the one the tab was dropped in", a.activeID())
	}
	if got := a.names(2)[a.group(2).Model.IndexOf(a.group(2).Model.ActiveEditor())]; got != "c.go" {
		t.Errorf("the dropped tab is not the active one: %s", got)
	}
	// the editor moved with its view: it is the same editor, not a new one
	if a.editorOf(2).doc != a.doc("c.go") {
		t.Error("the dropped tab shows another document")
	}
	if got := a.win.Documents.Refs(a.doc("c.go")); got != 1 {
		t.Errorf("c.go has %d users after it moved, want 1", got)
	}
	a.noErrors()
}

func TestDraggingATabPastTheLastTabOfAStrip(t *testing.T) {
	a := twoGroups(t)
	end := a.strip(2)
	g := a.dragTab(1, "b.go", ui.Offset{Dx: end.Right - 5, Dy: end.Top + 15})
	if got := a.shell().view(a.group(2)).feedback.Index(); got != 2 {
		t.Errorf("the line is at %d, want after the last tab", got)
	}
	g.Up()
	a.pump()
	a.wantTabs(1, "a.go", "c.go")
	a.wantTabs(2, "a.go", "d.go", "b.go")
}

func TestControlCopiesADraggedTab(t *testing.T) {
	a := twoGroups(t)
	d := a.tabRect(2, "d.go")
	a.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	g := a.dragTab(1, "c.go", ui.Offset{Dx: d.Left + 10, Dy: d.Top + 15})
	g.Up()
	a.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	a.pump()
	a.wantTabs(1, "a.go", "b.go", "c.go")
	a.wantTabs(2, "a.go", "c.go", "d.go")
	// the two groups show c.go in editors of their own
	if a.shell().group.editors[a.group(1).Model.EditorAt(2)] == a.shell().group.editors[a.group(2).Model.EditorAt(1)] {
		t.Error("a copy shares the editor of the original")
	}
	if got := a.win.Documents.Refs(a.doc("c.go")); got != 2 {
		t.Errorf("c.go has %d users, want one for each editor", got)
	}
	a.noErrors()
}

func TestEscapeCancelsTheDrag(t *testing.T) {
	a := twoGroups(t)
	d := a.tabRect(2, "d.go")
	g := a.dragTab(1, "c.go", ui.Offset{Dx: d.Left + 10, Dy: d.Top + 15})
	if a.texts("c.go") != a.base+1 {
		t.Error("the label of the drag is not shown")
	}
	if a.shell().drag() == nil {
		t.Fatal("no drag goes on")
	}
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyEscape)
	a.pump()
	if a.shell().drag() != nil {
		t.Error("the drag goes on after Escape")
	}
	if got := a.shell().view(a.group(2)).feedback.Index(); got != -1 {
		t.Errorf("the line stays at %d", got)
	}
	g.MoveTo(ui.Offset{Dx: d.Left + 30, Dy: d.Top + 15})
	g.Up()
	a.pump()
	a.wantTabs(1, "a.go", "b.go", "c.go")
	a.wantTabs(2, "a.go", "d.go")
	if n := a.texts("c.go"); n != a.base {
		t.Errorf("%d texts c.go, want %d: the label of the drag stays", n, a.base)
	}
	a.noErrors()
}

func TestDraggingATabIntoTheEditorAreaOfAnotherGroup(t *testing.T) {
	a := twoGroups(t)
	area := a.editorArea(2)
	g := a.dragTab(1, "b.go", center(area))
	target := a.shell().group.drag.target
	if target.kind != dropArea || target.group != a.group(2) || target.zone != editorgroup.DropCenter {
		t.Fatalf("the target is %+v", target)
	}
	// the whole area is highlighted
	if target.rect != area {
		t.Errorf("the highlight is %v, want the area %v", target.rect, area)
	}
	g.Up()
	a.pump()
	a.wantTabs(1, "a.go", "c.go")
	a.wantTabs(2, "a.go", "d.go", "b.go")
	if a.activeID() != 2 {
		t.Errorf("group %d is active", a.activeID())
	}
	a.noErrors()
}

func TestDraggingATabOntoAnEdgeSplits(t *testing.T) {
	for _, tc := range []struct {
		name string
		// at is the pointer in the area, as shares of its width and height
		x, y  float64
		zone  editorgroup.DropZone
		shape string
		// the tabs of the groups afterwards, by the order of the grid
		want [][]string
	}{
		{"left", 0.03, 0.5, editorgroup.DropLeft, "H(1 3 2)", [][]string{{"a.go", "c.go"}, {"b.go"}, {"a.go", "d.go"}}},
		{"right", 0.97, 0.5, editorgroup.DropRight, "H(1 2 3)", [][]string{{"a.go", "c.go"}, {"a.go", "d.go"}, {"b.go"}}},
		{"up", 0.5, 0.03, editorgroup.DropUp, "H(1 V(3 2))", [][]string{{"a.go", "c.go"}, {"b.go"}, {"a.go", "d.go"}}},
		{"down", 0.5, 0.97, editorgroup.DropDown, "H(1 V(2 3))", [][]string{{"a.go", "c.go"}, {"a.go", "d.go"}, {"b.go"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := twoGroups(t)
			area := a.editorArea(2)
			at := ui.Offset{Dx: area.Left + area.Width()*tc.x, Dy: area.Top + area.Height()*tc.y}
			g := a.dragTab(1, "b.go", at)
			target := a.shell().group.drag.target
			if target.kind != dropArea || target.zone != tc.zone {
				t.Fatalf("the target is %+v, want the zone %v", target, tc.zone)
			}
			// the highlight is the half of the area on that side
			wantRect := map[editorgroup.DropZone]ui.Rect{
				editorgroup.DropLeft:  ui.NewRectFromLTWH(area.Left, area.Top, area.Width()/2, area.Height()),
				editorgroup.DropRight: ui.NewRectFromLTWH(area.Left+area.Width()/2, area.Top, area.Width()/2, area.Height()),
				editorgroup.DropUp:    ui.NewRectFromLTWH(area.Left, area.Top, area.Width(), area.Height()/2),
				editorgroup.DropDown:  ui.NewRectFromLTWH(area.Left, area.Top+area.Height()/2, area.Width(), area.Height()/2),
			}[tc.zone]
			if target.rect != wantRect {
				t.Errorf("the highlight is %v, want %v", target.rect, wantRect)
			}
			g.Up()
			a.pump()
			if got := a.shape(); got != tc.shape {
				t.Errorf("the grid is %s, want %s", got, tc.shape)
			}
			var got [][]string
			for _, id := range a.ids() {
				got = append(got, a.names(id))
			}
			if !slices.EqualFunc(got, tc.want, slices.Equal[[]string]) {
				t.Errorf("the groups show %v, want %v", got, tc.want)
			}
			if a.names(a.activeID())[0] != "b.go" {
				t.Errorf("the new group is not the active one: %v", a.names(a.activeID()))
			}
			a.noErrors()
		})
	}
}

func TestSplitOnDragAndDropCanBeTurnedOff(t *testing.T) {
	a := twoGroups(t)
	a.setting("workbench.editor.splitOnDragAndDrop", false)
	area := a.editorArea(2)
	g := a.dragTab(1, "b.go", ui.Offset{Dx: area.Right - 3, Dy: area.Top + area.Height()/2})
	if got := a.shell().group.drag.target.zone; got != editorgroup.DropCenter {
		t.Errorf("the zone at the edge is %v, want the center", got)
	}
	// Alt turns it on for one drag
	a.ts.SendKeyDownEvent(services.LogicalKeyboardKeyAlt)
	g.MoveTo(ui.Offset{Dx: area.Right - 4, Dy: area.Top + area.Height()/2})
	if got := a.shell().group.drag.target.zone; got != editorgroup.DropRight {
		t.Errorf("with Alt the zone is %v, want right", got)
	}
	a.ts.SendKeyUpEvent(services.LogicalKeyboardKeyAlt)
	g.Up()
	a.pump()
}

func TestAGroupWithOneTabCannotBeDroppedOnItself(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.run("workbench.action.splitEditorRight")
	a.run("workbench.action.closeActiveEditor") // group 2 closes with its only editor
	a.wantTabs(1, "a.go")
	area := a.editorArea(1)
	g := a.dragTab(1, "a.go", center(area))
	if a.shell().group.drag.target.kind != dropNone {
		t.Errorf("the only tab of a group over its own area: %+v", a.shell().group.drag.target)
	}
	g.Up()
	a.pump()
	a.wantTabs(1, "a.go")
	if a.grid().Count() != 1 {
		t.Errorf("the grid is %s", a.shape())
	}
}

func TestDraggingTheLastTabOfAGroupMovesTheGroup(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go")
	a.run("workbench.action.newGroupRight")
	a.show("b.go", false)
	a.wantTabs(2, "b.go")
	area := a.editorArea(1)
	g := a.dragTab(2, "b.go", ui.Offset{Dx: area.Left + 3, Dy: area.Top + area.Height()/2})
	g.Up()
	a.pump()
	// the group of b.go is now on the left of the group of a.go, with its editor
	if got := a.shape(); got != "H(2 1)" {
		t.Errorf("the grid is %s, want H(2 1)", got)
	}
	a.wantTabs(2, "b.go")
	a.wantTabs(1, "a.go")
	a.noErrors()
}

func TestDraggingOverAnEmptyGroup(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go"}, "a.go", "b.go")
	a.run("workbench.action.newGroupRight")
	if !a.group(2).Empty() {
		t.Fatal("group 2 is not empty")
	}
	// an empty group has no strip: its whole area takes the drop
	g := a.dragTab(1, "b.go", center(a.editorArea(2)))
	if target := a.shell().group.drag.target; target.kind != dropArea || target.group != a.group(2) || target.zone != editorgroup.DropCenter {
		t.Fatalf("the target is %+v", target)
	}
	g.Up()
	a.pump()
	a.wantTabs(1, "a.go")
	a.wantTabs(2, "b.go")
	a.noErrors()
}

func TestADragOverNothingDropsNothing(t *testing.T) {
	a := twoGroups(t)
	// the pointer ends over the title bar, which is no group
	g := a.dragTab(1, "c.go", ui.Offset{Dx: 400, Dy: 2})
	if a.shell().group.drag.target.kind != dropNone {
		t.Errorf("the target is %+v", a.shell().group.drag.target)
	}
	g.Up()
	a.pump()
	a.wantTabs(1, "a.go", "b.go", "c.go")
	a.wantTabs(2, "a.go", "d.go")
}

func TestTheStripScrollsWhileADragRestsAtItsEnd(t *testing.T) {
	names := manyNames(16)
	a := newGroupApp(t, names, names...)
	tabs := a.shell().view(a.group(1)).tabs
	strip := a.strip(1)
	last, _ := tabs.TabRect(len(names) - 1)
	if last.Right <= strip.Right+600 {
		t.Fatalf("the tabs fit: the last ends at %v, the strip at %v", last.Right, strip.Right)
	}
	// a drag that starts with the first tab and rests near the end of the strip; a tab at the end that is
	// cut is shown whole first, as that is the tab under the pointer
	g := a.dragTab(1, names[0], ui.Offset{Dx: strip.Right - 5, Dy: strip.Top + 15})
	a.ts.Pump(tabDragScrollInterval)
	// the pointer rests near the end: the strip scrolls by a step in each frame
	first := tabs.Scrolled()
	for range 5 {
		a.ts.Pump(tabDragScrollInterval)
	}
	if got := tabs.Scrolled(); got <= first {
		t.Fatalf("the strip did not scroll to the right: %v after %v", got, first)
	}
	first = tabs.Scrolled()
	for range 5 {
		a.ts.Pump(tabDragScrollInterval)
	}
	if got := tabs.Scrolled(); got <= first {
		t.Errorf("the strip stopped at %v after %v", got, first)
	}
	// it stops at the end of the content
	for range 200 {
		a.ts.Pump(tabDragScrollInterval)
	}
	last, _ = tabs.TabRect(len(names) - 1)
	if last.Right != strip.Right {
		t.Errorf("the last tab ends at %v, the strip at %v", last.Right, strip.Right)
	}
	// and back to the start near the other end, and the line follows what is under the pointer
	g.MoveTo(ui.Offset{Dx: strip.Left + 2, Dy: strip.Top + 15})
	for range 200 {
		a.ts.Pump(tabDragScrollInterval)
	}
	if got := tabs.Scrolled(); got != 0 {
		t.Errorf("the strip is scrolled by %v at its start", got)
	}
	if got := a.shell().view(a.group(1)).feedback.Index(); got != 0 {
		t.Errorf("the line is at %d, want before the first tab", got)
	}
	// away from the ends the strip stays
	g.MoveTo(center(strip))
	before := tabs.Scrolled()
	for range 10 {
		a.ts.Pump(tabDragScrollInterval)
	}
	if got := tabs.Scrolled(); got != before {
		t.Errorf("the strip scrolls with the pointer in its middle: %v to %v", before, got)
	}
	g.Up()
	a.pump()
	a.noErrors()
}

// manyNames are names of n files that make a tab strip of the window overflow.
func manyNames(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("file%02d.go", i+1)
	}
	return out
}

func TestADraggedOverTabThatIsCutIsShownWhole(t *testing.T) {
	names := manyNames(16)
	a := newGroupApp(t, names, names...)
	tabs := a.shell().view(a.group(1)).tabs
	strip := a.strip(1)
	// a tab that is cut by the end of the strip
	var cut string
	for i, n := range names {
		if r, ok := tabs.TabRect(i); ok && r.Left < strip.Right-10 && r.Right > strip.Right+10 {
			cut = n
			break
		}
	}
	if cut == "" {
		t.Fatal("no tab is cut by the end of the strip")
	}
	r := a.tabRect(1, cut)
	// the pointer over the visible part of the cut tab, away from the scrolling zone
	g := a.dragTab(1, names[0], ui.Offset{Dx: r.Left + 5, Dy: strip.Top + 15})
	after := a.tabRect(1, cut)
	if after.Right > strip.Right+0.5 {
		t.Errorf("the tab is still cut: %v in the strip %v", after, strip)
	}
	g.Up()
	a.pump()
}

func TestADragLabelAndTheHighlightAreDrawn(t *testing.T) {
	a := twoGroups(t)
	colors := kit.ThemeOf(a.shell().Context())
	area := a.editorArea(2)
	g := a.dragTab(1, "b.go", ui.Offset{Dx: area.Left + 3, Dy: area.Top + 100})
	// the left half of the area is highlighted over the editor, the right half is not
	inside := a.ts.Screenshot()
	px := func(x, y float64) [4]uint8 {
		r, gr, b, al := inside.At(int(x), int(y)).RGBA()
		return [4]uint8{uint8(r >> 8), uint8(gr >> 8), uint8(b >> 8), uint8(al >> 8)}
	}
	left, right := px(area.Left+area.Width()/4, area.Top+200), px(area.Left+area.Width()*3/4, area.Top+200)
	if left == right {
		t.Errorf("the left half %v looks like the right half %v", left, right)
	}
	if colors.EditorGroupDropBackground.Alpha() == 0 {
		t.Error("the theme has no drop background")
	}
	g.Up()
	a.pump()
}

func TestTheDragIsOverWhenTheWindowIsGone(t *testing.T) {
	a := twoGroups(t)
	g := a.dragTab(1, "c.go", center(a.editorArea(2)))
	g.Cancel()
	a.pump()
	if a.shell().drag() != nil {
		t.Error("the drag goes on after the pointer was cancelled")
	}
	a.wantTabs(1, "a.go", "b.go", "c.go")
	a.ts.Pump(100 * time.Millisecond)
}

func TestPlaceDragLabel(t *testing.T) {
	window := ui.Size{Width: 800, Height: 600}
	label := ui.Size{Width: 100, Height: 30}
	for _, tc := range []struct {
		name string
		at   ui.Offset
		want ui.Offset
	}{
		{"below and to the right of the pointer", ui.Offset{Dx: 100, Dy: 100}, ui.Offset{Dx: 112, Dy: 112}},
		{"no room on the right: to its left", ui.Offset{Dx: 780, Dy: 100}, ui.Offset{Dx: 668, Dy: 112}},
		{"no room below: above it", ui.Offset{Dx: 100, Dy: 590}, ui.Offset{Dx: 112, Dy: 548}},
		{"in the corner", ui.Offset{Dx: 799, Dy: 599}, ui.Offset{Dx: 687, Dy: 557}},
		{"a pointer outside the window", ui.Offset{Dx: -300, Dy: -300}, ui.Offset{Dx: 0, Dy: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := placeDragLabel(tc.at, label, window); got != tc.want {
				t.Errorf("placeDragLabel = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDevOpenFileCanOpenAPreviewInAGroup(t *testing.T) {
	a := twoGroups(t)
	open := func(name string, args map[string]any) {
		t.Helper()
		args["path"] = a.files[name]
		if err := a.runWith("dev.openFile", args); err != nil {
			t.Fatal(err)
		}
	}
	open("b.go", map[string]any{"group": 2.0, "preview": true})
	a.wantTabs(2, "a.go", "d.go", "b.go")
	if it := a.shown(2); !slices.Equal(it, []string{"a.go", "d.go", "b.go"}) {
		t.Fatalf("group 2 shows %v", it)
	}
	// b.go is the preview of group 2: the next preview replaces it
	open("c.go", map[string]any{"group": 2.0, "preview": true})
	a.wantTabs(2, "a.go", "d.go", "c.go")
	// a file opened without the argument is kept
	open("b.go", map[string]any{"group": 2.0})
	a.wantTabs(2, "a.go", "d.go", "c.go", "b.go")
	m := a.group(2).Model
	for name, preview := range map[string]bool{"a.go": false, "d.go": false, "c.go": true, "b.go": false} {
		if got := !m.IsPinned(m.EditorAt(slices.Index(a.names(2), name))); got != preview {
			t.Errorf("%s preview = %v, want %v", name, got, preview)
		}
	}
	a.noErrors()
}

func TestTheDragEndsWhenItsTabIsClosedUnderIt(t *testing.T) {
	a := twoGroups(t)
	g := a.dragTab(1, "c.go", center(a.editorArea(2)))
	if a.shell().drag() == nil {
		t.Fatal("no drag goes on")
	}
	if err := a.runWith("workbench.action.closeActiveEditor", tabContext(a.group(1), 2)); err != nil {
		t.Fatal(err)
	}
	if a.shell().drag() != nil {
		t.Error("the drag goes on after its tab was closed")
	}
	g.Up()
	a.pump()
	a.wantTabs(1, "a.go", "b.go")
	a.wantTabs(2, "a.go", "d.go")
	// no tab, no breadcrumbs and no label of the drag shows c.go any more
	if n := a.texts("c.go"); n != 0 {
		t.Errorf("%d texts c.go after the tab was closed", n)
	}
	a.noErrors()
}

func TestAnEditorReplacedInTheActiveGroupGivesItsFocusToTheNewOne(t *testing.T) {
	a := newGroupApp(t, []string{"a.go", "b.go", "c.go", "d.go"}, "a.go", "b.go", "c.go")
	a.run("workbench.action.splitEditorRight")
	if a.activeID() != 2 {
		t.Fatalf("group %d is active after the split", a.activeID())
	}
	// a file opens over the active tab of group 2: the focus must not fall to the editor of group 1
	a.show("d.go", false)
	a.pump()
	if a.activeID() != 2 {
		t.Errorf("group %d is active after a file opened in group 2", a.activeID())
	}
	if !a.shell().group.focused[a.shell().group.editors[a.group(2).Model.ActiveEditor()]] {
		t.Error("the editor of the new tab of group 2 has not the keyboard focus")
	}
	// the same for a tab that is clicked
	a.ts.TapAt(center(a.tabRect(2, "a.go")), mouseButton)
	a.pump()
	if a.activeID() != 2 {
		t.Errorf("group %d is active after a tab of group 2 was clicked", a.activeID())
	}
	a.noErrors()
}
