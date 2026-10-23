package kit

import (
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
)

// tapWith taps a row with modifier keys held.
func (r *treeRig) tapWith(i int, x float64, mods ...services.LogicalKeyboardKey) {
	r.t.Helper()
	for _, k := range mods {
		r.ts.SendKeyDownEvent(k)
	}
	r.tapRow(i, x)
	for _, k := range slices.Backward(mods) {
		r.ts.SendKeyUpEvent(k)
	}
}

func (r *treeRig) typeText(text string) {
	r.t.Helper()
	for _, c := range text {
		logical := services.LogicalKeyboardKey(c)
		if c == ' ' {
			logical = services.LogicalKeyboardKeySpace
		}
		r.ts.SendKeyDownEvent(logical, gluttertest.KeyOpts{Character: string(c)})
		r.ts.SendKeyUpEvent(logical)
	}
	r.settle()
}

// firstRowTop is where the first row is: below the find box when there is one.
func (r *treeRig) firstRowTop() float64 {
	return r.ts.TopLeft(gluttertest.ByType[*ScrollableElement]()).Dy
}

func TestTreeMultiSelectClicks(t *testing.T) {
	r := newTreeRig(t, treeRigOpts{expanded: []string{"a", "a2"}, tree: TreeOpts{MultiSelect: true}})
	// rows: a a1 a2 a2x a3 b c d
	r.tapRow(1, 150) // a1
	r.tapWith(5, 150, keyControl)
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"a1", "b"}) {
		t.Errorf("ctrl: %q", got)
	}
	r.tapWith(3, 150, keyShift) // from the anchor (b) up to a2x
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"a1", "a2x", "a3", "b"}) {
		t.Errorf("shift: %q", got)
	}
	r.tapWith(0, twistieX(0), keyControl) // the twistie of a: only the selection changes
	if r.m.IsExpanded("a") != true {
		t.Error("ctrl+click on a twistie does not collapse the folder")
	}
	if !r.m.IsSelected("a") {
		t.Error("it selects the node")
	}
	r.tapRow(7, 150) // d, plain: alone
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"d"}) {
		t.Errorf("plain click: %q", got)
	}
	r.noErrors()
}

func TestTreeOnSelectionChange(t *testing.T) {
	var events [][]string
	r := newTreeRig(t, treeRigOpts{expanded: []string{"a"}, tree: TreeOpts{MultiSelect: true, OnSelectionChange: func(keys []string) { events = append(events, keys) }}})
	r.tapRow(1, 150)
	r.tapWith(3, 150, keyControl)
	r.tapWith(3, 150, keyControl)
	r.tapRow(1, 150) // already selected alone: no change
	r.focusKeys()
	r.keyWith(keyShift, keyArrowDown)
	want := [][]string{{"a1"}, {"a1", "a3"}, {"a1"}, {"a1", "a2"}}
	if len(events) != len(want) {
		t.Fatalf("events %q", events)
	}
	for i := range want {
		if !slices.Equal(events[i], want[i]) {
			t.Errorf("event %d: %q, want %q", i, events[i], want[i])
		}
	}
	r.noErrors()
}

// focusKeys gives the tree the keyboard focus.
func (r *treeRig) focusKeys() {
	r.ctrl.Focus()
	r.settle()
}

func TestTreeMultiSelectKeysInTheWidget(t *testing.T) {
	r := newTreeRig(t, treeRigOpts{expanded: []string{"a"}, tree: TreeOpts{MultiSelect: true}})
	r.tapRow(1, 150)
	r.keyWith(keyShift, keyArrowDown)
	r.keyWith(keyShift, keyArrowDown)
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"a1", "a2", "a3"}) {
		t.Errorf("shift+down: %q", got)
	}
	r.keyWith(keyShift, keyArrowUp)
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"a1", "a2"}) {
		t.Errorf("shift+up: %q", got)
	}
	r.keyWith(keyControl, services.LogicalKeyboardKeyKeyA)
	if r.m.SelectionCount() != 7 {
		t.Errorf("ctrl+a: %q", r.m.SelectedKeys())
	}
	r.key(keyEscape)
	if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"a2"}) {
		t.Errorf("escape: %q", got)
	}
	r.keyWith(keyControl, keyArrowLeft) // collapse all is still the tree's
	if r.m.IsExpanded("a") {
		t.Error("ctrl+left")
	}
	// the keys are not used without MultiSelect
	single := newTreeRig(t, treeRigOpts{expanded: []string{"a"}})
	single.tapRow(1, 150)
	single.keyWith(keyShift, keyArrowDown)
	if got := single.m.SelectedKeys(); !slices.Equal(got, []string{"a1"}) || focusedKey(single.m) != "a1" {
		t.Errorf("shift+down needs MultiSelect: %q %q", got, focusedKey(single.m))
	}
	r.noErrors()
	single.noErrors()
}

func findTreeRig(t *testing.T, mode FindMode, expanded ...string) *treeRig {
	t.Helper()
	return newTreeRig(t, treeRigOpts{
		items: findItems(), expanded: expanded, size: ui.Size{Width: 300, Height: 220},
		model: TreeModelOpts{FindMode: mode},
		tree:  TreeOpts{Render: nil},
	})
}

func TestTreeTypeToFind(t *testing.T) {
	t.Run("highlight keeps the rows and moves the focus to the matches", func(t *testing.T) {
		r := findTreeRig(t, FindModeHighlight, "src", "src/util")
		r.focusKeys()
		r.typeText("go")
		st := r.m.Find()
		if !st.Open || st.Pattern != "go" || st.MatchCount != 3 {
			t.Errorf("find %+v", st)
		}
		if got := focusedKey(r.m); got != "src/main.go" {
			t.Errorf("focus %q", got)
		}
		r.key(keyArrowDown)
		r.key(keyArrowDown)
		if got := focusedKey(r.m); got != "src/util/maps.go" {
			t.Errorf("down: %q", got)
		}
		r.key(keyEscape)
		if r.m.Find().Open {
			t.Error("escape closes the find")
		}
		if got := focusedKey(r.m); got != "src/util/maps.go" {
			t.Errorf("the focus stays: %q", got)
		}
		r.noErrors()
	})
	t.Run("filter shows the matches and the nodes above them, expanded", func(t *testing.T) {
		r := findTreeRig(t, FindModeFilter)
		r.focusKeys()
		clear(r.rendered)
		r.typeText("maps")
		if got := r.renderedKeys(); !slices.Equal(got, []string{"src", "src/util", "src/util/maps.go"}) {
			t.Errorf("rows %q", got)
		}
		if got := shape(r.m); !slices.Equal(got, []string{"src-", "  src/util-", "    src/util/maps.go"}) {
			t.Errorf("shape %q", got)
		}
		// a click on the match: the rows are below the find box
		top := r.firstRowTop()
		r.ts.TapAt(ui.Offset{Dx: 150, Dy: top + r.y(2)}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
		r.settle()
		if got := r.m.SelectedKeys(); !slices.Equal(got, []string{"src/util/maps.go"}) {
			t.Errorf("selected %q", got)
		}
		r.key(keyEscape)
		if got := shape(r.m); !slices.Equal(got, []string{"src+", "docs+", "LICENSE", "vendor+"}) {
			t.Errorf("after the find: %q", got)
		}
		r.noErrors()
	})
	t.Run("no results", func(t *testing.T) {
		r := findTreeRig(t, FindModeFilter)
		r.focusKeys()
		r.typeText("qqq")
		if !r.ts.Exists(gluttertest.ByText("No results found.")) {
			t.Error("no message")
		}
		if r.m.Len() != 0 {
			t.Errorf("rows %d", r.m.Len())
		}
		r.noErrors()
	})
	t.Run("a letter on a tree with TypeToFind off is not used", func(t *testing.T) {
		r := newTreeRig(t, treeRigOpts{tree: TreeOpts{TypeToFind: foundation.Some(false)}})
		r.focusKeys()
		r.typeText("a")
		if r.m.Find().Open {
			t.Error("find is off")
		}
	})
}

// dropLog records the drops of a tree.
type dropLog struct{ calls []dropCall }

func dndTreeRig(t *testing.T, log *dropLog, refuse string) *treeRig {
	t.Helper()
	return newTreeRig(t, treeRigOpts{
		items: sampleItems(), expanded: []string{"a"}, size: ui.Size{Width: 300, Height: 220},
		tree: TreeOpts{
			MultiSelect: true,
			DragAndDrop: &DragAndDrop{
				CanDrop: func(target string, _ DropPosition, _ []string) DropEffect {
					if target == refuse && refuse != "" {
						return DropEffectNone
					}
					return DropEffectMove
				},
				OnDrop: func(target string, pos DropPosition, keys []string) {
					log.calls = append(log.calls, dropCall{target, pos, slices.Clone(keys)})
				},
			},
		},
	})
}

func (r *treeRig) drag(row int, to ui.Offset) *gluttertest.TestGesture {
	r.t.Helper()
	g := r.ts.StartGesture(ui.Offset{Dx: 150, Dy: r.y(row)}, mouseDown)
	g.MoveTo(ui.Offset{Dx: 150, Dy: r.y(row) + dragSlop + 2})
	g.MoveTo(to)
	r.settle()
	return g
}

func TestTreeDragAndDrop(t *testing.T) {
	t.Run("a drop inside, before and after a row", func(t *testing.T) {
		// rows: a a1 a2 a3 b c d (b is row 4)
		for _, tc := range []struct {
			name string
			dy   float64
			want DropPosition
		}{{"inside", 0, DropInside}, {"before", -8, DropBefore}, {"after", 8, DropAfter}} {
			log := &dropLog{}
			r := dndTreeRig(t, log, "")
			g := r.drag(1, ui.Offset{Dx: 150, Dy: r.y(4) + tc.dy}) // b
			switch tc.want {
			case DropInside:
				if got := r.pixel(250, int(r.y(4))); got != nrgba(r.colors.ListDropBackground) {
					t.Errorf("%s: the row is %v", tc.name, got)
				}
			case DropBefore:
				if got := r.pixel(250, 4*22); got != nrgba(r.colors.ListDropBetweenBackground) {
					t.Errorf("%s: the line is %v", tc.name, got)
				}
			case DropAfter:
				if got := r.pixel(250, 5*22-1); got != nrgba(r.colors.ListDropBetweenBackground) {
					t.Errorf("%s: the line is %v", tc.name, got)
				}
			}
			g.Up()
			r.settle()
			if len(log.calls) != 1 || log.calls[0].target != "b" || log.calls[0].pos != tc.want || !slices.Equal(log.calls[0].keys, []string{"a1"}) {
				t.Errorf("%s: drops %+v", tc.name, log.calls)
			}
			if len(r.selected) != 0 {
				t.Errorf("a drag is not a click: %q", r.selected)
			}
			r.noErrors()
		}
	})
	t.Run("a folder expands after the drag rests on it", func(t *testing.T) {
		log := &dropLog{}
		r := dndTreeRig(t, log, "")
		r.m.Collapse("a")
		r.settle()
		// rows: a b c d
		g := r.drag(3, ui.Offset{Dx: 150, Dy: r.y(0)}) // d onto a
		r.ts.Pump(DefaultAutoExpandDelay - 100*time.Millisecond)
		if r.m.IsExpanded("a") {
			t.Error("not yet")
		}
		r.ts.Pump(200 * time.Millisecond)
		if !r.m.IsExpanded("a") {
			t.Error("the folder expands after 500 ms")
		}
		// the drag goes on: the target is the folder still
		g.Up()
		r.settle()
		if len(log.calls) != 1 || log.calls[0].target != "a" || log.calls[0].pos != DropInside {
			t.Errorf("drops %+v", log.calls)
		}
		r.noErrors()
	})
	t.Run("moving away stops the expansion, and a refusing folder does not expand", func(t *testing.T) {
		log := &dropLog{}
		r := dndTreeRig(t, log, "a")
		r.m.Collapse("a")
		r.settle()
		g := r.drag(3, ui.Offset{Dx: 150, Dy: r.y(0)})
		r.ts.Pump(DefaultAutoExpandDelay + 100*time.Millisecond)
		if r.m.IsExpanded("a") {
			t.Error("a folder that refuses the drop is not expanded")
		}
		g.Up()
		r.settle()

		log2 := &dropLog{}
		r = dndTreeRig(t, log2, "")
		r.m.Collapse("a")
		r.settle()
		g = r.drag(3, ui.Offset{Dx: 150, Dy: r.y(0)})
		r.ts.Pump(300 * time.Millisecond)
		g.MoveTo(ui.Offset{Dx: 150, Dy: r.y(1)}) // b
		r.ts.Pump(DefaultAutoExpandDelay)
		if r.m.IsExpanded("a") {
			t.Error("the pointer left the folder")
		}
		// the edge of the folder (before it) does not expand either
		g.MoveTo(ui.Offset{Dx: 150, Dy: r.y(0) - 8})
		r.ts.Pump(DefaultAutoExpandDelay + 100*time.Millisecond)
		if r.m.IsExpanded("a") {
			t.Error("a drop before the folder does not open it")
		}
		g.Up()
		r.settle()
		r.noErrors()
	})
	t.Run("the delay can be changed", func(t *testing.T) {
		log := &dropLog{}
		r := dndTreeRig(t, log, "")
		r.m.Collapse("a")
		r.settle()
		_ = r
		r2 := newTreeRig(t, treeRigOpts{items: sampleItems(), tree: TreeOpts{DragAndDrop: &DragAndDrop{
			CanDrop:         func(string, DropPosition, []string) DropEffect { return DropEffectMove },
			AutoExpandDelay: foundation.Some(50 * time.Millisecond),
		}}})
		g := r2.drag(3, ui.Offset{Dx: 150, Dy: r2.y(0)})
		r2.ts.Pump(80 * time.Millisecond)
		if !r2.m.IsExpanded("a") {
			t.Error("expanded after 50 ms")
		}
		g.Up()
		r2.settle()
	})
	t.Run("escape cancels", func(t *testing.T) {
		log := &dropLog{}
		r := dndTreeRig(t, log, "")
		g := r.drag(1, ui.Offset{Dx: 150, Dy: r.y(5)})
		r.key(keyEscape)
		if got := r.pixel(250, int(r.y(5))); got == nrgba(r.colors.ListDropBackground) {
			t.Error("feedback after Escape")
		}
		g.Up()
		r.settle()
		if len(log.calls) != 0 {
			t.Errorf("drops %+v", log.calls)
		}
		// a selection is cleared by Escape only when no drag goes on: nothing was selected here
		r.noErrors()
	})
	t.Run("the selection is dragged", func(t *testing.T) {
		log := &dropLog{}
		r := dndTreeRig(t, log, "")
		r.tapRow(1, 150)
		r.tapWith(3, 150, keyControl)
		g := r.drag(3, ui.Offset{Dx: 150, Dy: r.y(5)})
		g.Up()
		r.settle()
		if len(log.calls) != 1 || !slices.Equal(log.calls[0].keys, []string{"a1", "a3"}) {
			t.Errorf("drops %+v", log.calls)
		}
		r.noErrors()
	})
	t.Run("the empty part below the rows is the root", func(t *testing.T) {
		log := &dropLog{}
		r := dndTreeRig(t, log, "")
		g := r.drag(1, ui.Offset{Dx: 150, Dy: 200})
		g.Up()
		r.settle()
		if len(log.calls) != 1 || log.calls[0].target != "" || log.calls[0].pos != DropInside {
			t.Errorf("drops %+v", log.calls)
		}
	})
}

func TestTreeDragScrollsNearTheEdges(t *testing.T) {
	log := &dropLog{}
	items := make([]TreeItem, 100)
	for i := range items {
		items[i] = TreeItem{Key: "n" + string(rune('A'+i/26)) + string(rune('a'+i%26)), Data: "x"}
	}
	r := newTreeRig(t, treeRigOpts{items: items, tree: TreeOpts{DragAndDrop: &DragAndDrop{
		CanDrop: func(string, DropPosition, []string) DropEffect { return DropEffectMove },
		OnDrop: func(target string, pos DropPosition, keys []string) {
			log.calls = append(log.calls, dropCall{target, pos, keys})
		},
	}}})
	g := r.drag(1, ui.Offset{Dx: 150, Dy: 215})
	for range 5 {
		r.ts.Pump(dragScrollInterval)
	}
	if got := r.ctrl.ScrollOffset(); got < 40 {
		t.Errorf("scrolled %v", got)
	}
	g.Up()
	r.settle()
	if len(log.calls) != 1 {
		t.Errorf("drops %+v", log.calls)
	}
	r.noErrors()
}
