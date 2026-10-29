package terminal

import (
	"math"
	"slices"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// Dragging a tab of the tabs list (SPEC §10.25 "The rest of the tabs list", "Terminals in the editor area"): inside the list the
// tab goes to another place (VS Code's TerminalTabsDragAndDrop: a tab of another group moves its group, a tab of the same group
// moves inside it); out of the list the drag becomes the drag of an editor tab (Editors().BeginDrag), and dropped on a tab
// strip or the editor area it moves the terminal into an editor tab.

// dragSlop is how far the pointer moves before a press on a tab is a drag.
const dragSlop = 4.0

// tabDrag is a press on a tab of the list that may become a drag.
type tabDrag struct {
	e       *entry
	pointer int
	start   gui.Offset
	// list is the tabs list on the screen, and rows the top of each row from the top of the list (with the end as the last).
	list gui.Rect
	rows []float64
	// dragging says that the pointer moved past the slop; drag is the drag of the window once the pointer left the list.
	dragging bool
	drag     extension.EditorDrag
	last     gui.Offset
}

// tabPointerDown is a press on a tab: with the primary button it may start a drag.
func (t *terminals) tabPointerDown(ev *gestures.PointerDownEvent, e *entry, index int) {
	if ev.Buttons()&gestures.PrimaryButton == 0 || t.tabDrag != nil {
		return
	}
	origin := gui.Offset{Dx: ev.Position().Dx - ev.LocalPosition().Dx, Dy: ev.Position().Dy - ev.LocalPosition().Dy}
	rows := []float64{0}
	for _, x := range t.panelEntries() {
		rows = append(rows, rows[len(rows)-1]+tabRowH+t.markHeight(x))
	}
	top := origin.Dy - rows[min(index, len(rows)-1)]
	t.tabDrag = &tabDrag{
		e: e, pointer: ev.Pointer(), start: ev.Position(), last: ev.Position(), rows: rows,
		list: gui.NewRectFromLTWH(origin.Dx, top, tabsWidth, rows[len(rows)-1]),
	}
}

// markHeight is the extra height of the row of a terminal that Relay clients typed in.
func (t *terminals) markHeight(e *entry) float64 {
	if clientMark(e) != "" {
		return markH
	}
	return 0
}

// tabPointerMove follows the pointer of a press on a tab.
func (t *terminals) tabPointerMove(ev *gestures.PointerMoveEvent) {
	d := t.tabDrag
	if d == nil || ev.Pointer() != d.pointer {
		return
	}
	pos := ev.Position()
	d.last = pos
	if !d.dragging {
		if math.Hypot(pos.Dx-d.start.Dx, pos.Dy-d.start.Dy) < dragSlop {
			return
		}
		d.dragging = true
	}
	inside := pos.Dx >= d.list.Left-tabsWidth/2 && pos.Dx <= d.list.Right+tabsWidth/2 && pos.Dy >= d.list.Top-tabRowH && pos.Dy <= d.list.Bottom+tabRowH
	if inside {
		if d.drag != nil {
			t.endWindowDrag(d, pos, false)
		}
		target := t.rowAt(d, pos.Dy)
		t.setDragOver(target, target == nil)
		return
	}
	t.setDragOver(nil, false)
	if d.drag == nil {
		drag, err := t.api.Editors().BeginDrag(EditorKindID, t.title(d.e), tabState{Adopt: d.e.term.ID()})
		if err != nil {
			t.api.Log().Warn("the terminal cannot be dragged to the editor area", "err", err)
			return
		}
		d.drag = drag
	}
	d.drag.Move(pos)
}

// rowAt is the terminal whose row is at a height of the window, nil below the last row.
func (t *terminals) rowAt(d *tabDrag, y float64) *entry {
	rel := y - d.list.Top
	all := t.panelEntries()
	for i := range all {
		if rel < d.rows[i+1] {
			return all[i]
		}
	}
	return nil
}

// setDragOver shows the place the tab would go to: a line over the row of the terminal it would go before, none at the end.
func (t *terminals) setDragOver(e *entry, end bool) {
	if t.dragOver == e && t.dragEnd == end {
		return
	}
	t.dragOver, t.dragEnd = e, end
	t.handle.Refresh()
}

// endWindowDrag ends the drag of the window, dropping it or not; it reports whether the terminal was opened in a tab.
func (t *terminals) endWindowDrag(d *tabDrag, pos gui.Offset, drop bool) bool {
	drag := d.drag
	d.drag = nil
	if drag == nil {
		return false
	}
	e := d.e
	e.adopting = true
	opened, err := drag.End(pos, drop)
	e.adopting = false
	if err != nil {
		t.api.Log().Warn("the terminal cannot be dropped in the editor area", "err", err)
		t.api.Notifications().Warn("The terminal cannot move to the editor area: " + err.Error())
	}
	t.rescue(e)
	if opened && e.inEditor() {
		t.finishLeaving()
		t.focusEntry(e, true)
		return true
	}
	return false
}

// tabPointerUp is the release of the press on a tab: it drops the drag where the pointer is.
func (t *terminals) tabPointerUp(ev *gestures.PointerUpEvent) {
	d := t.tabDrag
	if d == nil || ev.Pointer() != d.pointer {
		return
	}
	t.tabDrag = nil
	pos := ev.Position()
	switch {
	case !d.dragging:
		return
	case d.drag != nil:
		t.endWindowDrag(d, pos, true)
	default:
		target := t.rowAt(d, pos.Dy)
		t.setDragOver(nil, false)
		t.moveTab(d.e, target)
	}
}

// tabPointerCancel is the system taking the pointer away: the drag ends and nothing moves.
func (t *terminals) tabPointerCancel(ev *gestures.PointerCancelEvent) {
	d := t.tabDrag
	if d == nil || ev.Pointer() != d.pointer {
		return
	}
	t.tabDrag = nil
	t.setDragOver(nil, false)
	if d.drag != nil {
		t.endWindowDrag(d, d.last, false)
	}
}

// dropMark draws the line of a drag over a row: at its top when the dragged tab would go before it.
func (t *terminals) dropMark(colors ui.Colors, row widgets.Widget, e *entry) widgets.Widget {
	isLast := false
	if all := t.panelEntries(); len(all) > 0 {
		isLast = all[len(all)-1] == e
	}
	top := t.dragOver == e && t.dragOver != nil
	bottom := t.dragEnd && isLast
	if !top && !bottom {
		return row
	}
	line := widgets.NewColoredBox(colors.TerminalTabActiveBorder, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(2.0)}))
	opts := widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0)}
	if top {
		opts.Top = foundation.Some(0.0)
	} else {
		opts.Bottom = foundation.Some(0.0)
	}
	return widgets.NewStack([]widgets.Widget{row, widgets.NewPositioned(line, opts)}, widgets.StackOpts{})
}

// moveTab moves a terminal of the tabs list before another one (VS Code's moveGroup): inside a tab of a split it moves inside it;
// a terminal of another tab moves its whole tab, after the tab of the target when it came from above and before it when it came
// from below; no target is the end of the list.
func (t *terminals) moveTab(e, target *entry) {
	gs := groupOf(t, e)
	if gs == nil || target == e {
		return
	}
	var keep *group
	if t.cur >= 0 && t.cur < len(t.groups) {
		keep = t.groups[t.cur]
	}
	if target == nil {
		t.groups = slices.DeleteFunc(t.groups, func(g *group) bool { return g == gs })
		t.groups = append(t.groups, gs)
	} else {
		gt := groupOf(t, target)
		if gt == nil {
			return
		}
		if gs == gt {
			// the tab moves to the place of the target: after it when it came from above, before it when it came from below
			i, j := slices.Index(gs.entries, e), slices.Index(gs.entries, target)
			size := gs.sizes[i]
			active := gs.entries[min(gs.active, len(gs.entries)-1)]
			gs.entries = slices.Insert(slices.Delete(gs.entries, i, i+1), j, e)
			gs.sizes = slices.Insert(slices.Delete(gs.sizes, i, i+1), j, size)
			gs.active = slices.Index(gs.entries, active)
		} else {
			si, ti := slices.Index(t.groups, gs), slices.Index(t.groups, gt)
			t.groups = slices.Insert(slices.Delete(t.groups, si, si+1), ti, gs)
		}
	}
	if i := slices.Index(t.groups, keep); i >= 0 {
		t.cur = i
	}
	t.syncKeys()
	t.handle.Refresh()
	t.focusEntry(e, false)
}

// rescue puts a terminal that is in no place back in the panel; it happens when a drop failed after the terminal left it.
func (t *terminals) rescue(e *entry) {
	if e.gone || t.alive(e) {
		return
	}
	t.placeInPanel(e, openOpts{})
	t.syncKeys()
	t.handle.Refresh()
}

// ansiColor is the color of the theme with the id of one of the terminal.ansi* colors; false for any other id.
func ansiColor(colors ui.Colors, id string) (gui.Color, bool) {
	if i := ansiIndex(id); i >= 0 {
		return colors.TerminalAnsi[i], true
	}
	return gui.Color{}, false
}
