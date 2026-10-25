package editor

import (
	"sort"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// ZoneSpec is a view zone: whole rows inserted into the editor after a line of the document, as
// VS Code's view zones (whitespace) are (SPEC §9.1). The diff editor fills the side that has fewer
// lines with them, and shows deleted lines in them.
type ZoneSpec struct {
	// AfterLine is the number of lines above the zone: the zone comes after the line with that
	// number, counted from 1. 0 puts it above the first line, and the number of lines of the
	// document after the last line.
	AfterLine int
	// Rows is the height of the zone in rows of the editor (at least 1).
	Rows int
	// Ordinal orders the zones that come after the same line: the lower first, and zones with the
	// same ordinal in the order they were added.
	Ordinal int
	// ShowInHiddenAreas shows the zone also when the line after it is hidden by a fold; a zone
	// is hidden then otherwise (VS Code's showInHiddenAreas).
	ShowInHiddenAreas bool
	// Widget is drawn in the rows of the zone, as wide as the text area; nil leaves the rows empty.
	// A zone's widget does not scroll sideways with the text.
	Widget widgets.Widget
	// Gutter, when it is not transparent, fills the gutter along the rows of the zone (VS Code's margin of a
	// view zone: the diff editor colors the margin of the deleted lines of its inline view).
	Gutter ui.Color
	// GutterGlyph, when it is not 0, is the Codicon (its code point) drawn at the right of the line
	// decorations column on every row of the zone, in the color of the line numbers: the minus sign of the
	// deleted lines of the inline diff.
	GutterGlyph int
}

// ZoneInfo is a zone and where the editor shows it now.
type ZoneInfo struct {
	// ID is the id of the zone.
	ID ZoneID
	// ZoneSpec is the zone as it was asked for; its AfterLine follows the edits of the document.
	ZoneSpec
	// FirstRow and ShownRows are where the zone is in the rows of the editor: ShownRows is 0 when
	// the zone is hidden by a fold.
	FirstRow, ShownRows int
}

// ZoneHit is a row of a zone, as the hit test finds it.
type ZoneHit struct {
	// ID is the zone.
	ID ZoneID
	// Index is the row inside the zone, from 0.
	Index int
	// FirstRow and Rows are the rows of the zone.
	FirstRow, Rows int
}

// zoneEntry is a zone of a view model.
type zoneEntry struct {
	id   ZoneID
	spec ZoneSpec
}

// zoneLink is the view zones of a view model.
type zoneLink struct {
	// byID holds the zones; order lists them by the line they come after, then by ordinal, then by
	// when they were added (the ids grow); it is made again when needed.
	byID  map[ZoneID]*zoneEntry
	order []*zoneEntry
	dirty bool
	next  ZoneID
	// epoch counts the changes; the projection was made for syncedEpoch.
	epoch, syncedEpoch int
	// unsubscribe stops following the edits of the document (made with the first zone).
	unsubscribe func()
	// press is told of a press on a row of a zone.
	press func(ZoneHit)
	// batch is nonzero while ChangeZones runs, and changed says that it changed something.
	batch   int
	changed bool
}

// sorted is the zones in the order of the projection.
func (z *zoneLink) sorted() []*zoneEntry {
	if z.dirty || len(z.order) != len(z.byID) {
		z.order = z.order[:0]
		for _, e := range z.byID {
			z.order = append(z.order, e)
		}
		sort.Slice(z.order, func(i, j int) bool {
			a, b := z.order[i], z.order[j]
			if a.spec.AfterLine != b.spec.AfterLine {
				return a.spec.AfterLine < b.spec.AfterLine
			}
			if a.spec.Ordinal != b.spec.Ordinal {
				return a.spec.Ordinal < b.spec.Ordinal
			}
			return a.id < b.id
		})
		z.dirty = false
	}
	return z.order
}

// items are the zones as the projection takes them.
func (z *zoneLink) items() []zoneItem {
	sorted := z.sorted()
	items := make([]zoneItem, len(sorted))
	for i, e := range sorted {
		items[i] = zoneItem{id: e.id, spec: e.spec}
	}
	return items
}

// zonesTouched records a change of the zones: the projection is made again.
func (vm *ViewModel) zonesTouched() {
	vm.zones.epoch++
	vm.zones.dirty = true
	vm.zones.changed = true
	if vm.zones.batch == 0 {
		vm.zones.changed = false
		vm.NotifyListeners()
	}
}

// validateZone says whether a zone can be had in this document.
func (vm *ViewModel) validateZone(spec ZoneSpec) error {
	if spec.Rows < 1 {
		return errors.New("EditorZoneInvalid", "a view zone has at least one row", "rows", spec.Rows)
	}
	if lines := vm.doc.Snapshot().LineCount(); spec.AfterLine < 0 || spec.AfterLine > lines {
		return errors.New("EditorZoneInvalid", "a view zone comes after a line of the document, or above the first",
			"afterLine", spec.AfterLine, "lines", lines)
	}
	return nil
}

// findZone is the zone with an id.
func (vm *ViewModel) findZone(id ZoneID) (*zoneEntry, error) {
	e, ok := vm.zones.byID[id]
	if !ok {
		return nil, errors.New("EditorZoneNotFound", "the editor has no view zone with this id", "id", int(id))
	}
	return e, nil
}

// AddZone adds a view zone and gives its id. The rows of the zone are inserted after the line it
// comes after (unless a fold hides the line after it), and move with that line when the document
// is edited: lines inserted above push it down, and when the line it comes after is deleted it
// goes after the line before the deleted ones.
func (vm *ViewModel) AddZone(spec ZoneSpec) (ZoneID, error) {
	var id ZoneID
	err := vm.ChangeZones(func(z ZoneChanges) error {
		var err error
		id, err = z.Add(spec)
		return err
	})
	return id, err
}

// ChangeZone changes a zone: fn is given a copy of it. A zone that would be invalid is not changed.
func (vm *ViewModel) ChangeZone(id ZoneID, fn func(*ZoneSpec)) error {
	return vm.ChangeZones(func(z ZoneChanges) error { return z.Change(id, fn) })
}

// SetZoneRows changes the height of a zone.
func (vm *ViewModel) SetZoneRows(id ZoneID, rows int) error {
	return vm.ChangeZone(id, func(s *ZoneSpec) { s.Rows = rows })
}

// SetZoneAfterLine moves a zone to come after another line.
func (vm *ViewModel) SetZoneAfterLine(id ZoneID, afterLine int) error {
	return vm.ChangeZone(id, func(s *ZoneSpec) { s.AfterLine = afterLine })
}

// RemoveZone removes a zone.
func (vm *ViewModel) RemoveZone(id ZoneID) error {
	return vm.ChangeZones(func(z ZoneChanges) error { return z.Remove(id) })
}

// ZoneChanges adds, changes and removes the zones inside ChangeZones.
type ZoneChanges struct{ vm *ViewModel }

// Add adds a zone; see ViewModel.AddZone.
func (z ZoneChanges) Add(spec ZoneSpec) (ZoneID, error) {
	vm := z.vm
	if err := vm.validateZone(spec); err != nil {
		return 0, err
	}
	if vm.zones.byID == nil {
		vm.zones.byID = map[ZoneID]*zoneEntry{}
	}
	if len(vm.zones.byID) == 0 && vm.zones.unsubscribe == nil {
		vm.zones.unsubscribe = vm.doc.Subscribe(vm.zonesDocumentChanged)
	}
	vm.zones.next++
	id := vm.zones.next
	vm.zones.byID[id] = &zoneEntry{id: id, spec: spec}
	vm.zonesTouched()
	return id, nil
}

// Change changes a zone; see ViewModel.ChangeZone.
func (z ZoneChanges) Change(id ZoneID, fn func(*ZoneSpec)) error {
	vm := z.vm
	e, err := vm.findZone(id)
	if err != nil {
		return err
	}
	spec := e.spec
	fn(&spec)
	if err := vm.validateZone(spec); err != nil {
		return err
	}
	e.spec = spec
	vm.zonesTouched()
	return nil
}

// Remove removes a zone; see ViewModel.RemoveZone.
func (z ZoneChanges) Remove(id ZoneID) error {
	vm := z.vm
	if _, err := vm.findZone(id); err != nil {
		return err
	}
	delete(vm.zones.byID, id)
	vm.zonesTouched()
	return nil
}

// ChangeZones makes several changes of the zones at once: the editor is told of them, and lays out
// again, when fn returns (VS Code's changeViewZones). The changes that were made before fn returns
// an error stay.
func (vm *ViewModel) ChangeZones(fn func(ZoneChanges) error) error {
	vm.zones.batch++
	err := fn(ZoneChanges{vm: vm})
	vm.zones.batch--
	if vm.zones.batch == 0 && vm.zones.changed {
		vm.zones.changed = false
		vm.NotifyListeners()
	}
	return err
}

// Zone is a zone by its id.
func (vm *ViewModel) Zone(id ZoneID) (ZoneSpec, bool) {
	e, ok := vm.zones.byID[id]
	if !ok {
		return ZoneSpec{}, false
	}
	return e.spec, true
}

// Zones lists the zones in the order of the projection (by the line they come after, then by
// ordinal, then in the order they were added) with the rows they have now.
func (vm *ViewModel) Zones() []ZoneInfo {
	proj := vm.Projection()
	sorted := vm.zones.sorted()
	out := make([]ZoneInfo, len(sorted))
	for i, e := range sorted {
		first, count := proj.ZoneRows(e.id)
		out[i] = ZoneInfo{ID: e.id, ZoneSpec: e.spec, FirstRow: first, ShownRows: count}
	}
	return out
}

// ZoneAtRow tells which zone has a row, if one does.
func (vm *ViewModel) ZoneAtRow(row int) (ZoneHit, bool) {
	proj := vm.Projection()
	id, index, ok := proj.RowZone(row)
	if !ok {
		return ZoneHit{}, false
	}
	first, count := proj.ZoneRows(id)
	return ZoneHit{ID: id, Index: index, FirstRow: first, Rows: count}, true
}

// ZoneAtY tells which zone is at a height from the top of the document, if one is. Above the first
// row and below the last there is none.
func (vm *ViewModel) ZoneAtY(y float64) (ZoneHit, bool) {
	proj := vm.Projection()
	if y < 0 || y >= proj.Height() {
		return ZoneHit{}, false
	}
	return vm.ZoneAtRow(proj.RowAt(y))
}

// SetZonePressHandler sets the function that is told when the primary button is pressed on a row
// of a zone: such a press does not move the cursor (VS Code's CONTENT_VIEW_ZONE target).
func (vm *ViewModel) SetZonePressHandler(fn func(ZoneHit)) { vm.zones.press = fn }

// DisposeZones stops following the document. The zones stay.
func (vm *ViewModel) DisposeZones() {
	if vm.zones.unsubscribe != nil {
		vm.zones.unsubscribe()
		vm.zones.unsubscribe = nil
	}
}

// zonesDocumentChanged moves the zones with the lines they come after.
func (vm *ViewModel) zonesDocumentChanged(e document.ChangeEvent) {
	changed := false
	lines := e.After.LineCount()
	for _, z := range vm.zones.byID {
		after := followLines(z.spec.AfterLine, e.Changes)
		after = min(after, lines)
		if after != z.spec.AfterLine {
			z.spec.AfterLine, changed = after, true
		}
	}
	if changed {
		vm.zones.dirty = true
		vm.zones.epoch++
	}
}

// followLines is the line a zone comes after (the number of lines above it) once the changes of
// an edit were made. Each change replaces the text from line s to line e by a text of n lines: the
// lines s+1 to e (from 0) are gone, and n-1 new ones follow line s. A zone after one of the lines
// that are gone comes after line s, one after a line below moves up or down by the difference.
//
// Ported from VS Code: src/vs/editor/common/viewLayout/linesLayout.ts (onLinesDeleted, onLinesInserted)
func followLines(after int, changes []document.Change) int {
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		s, e := c.OldRange.Start.Line, c.OldRange.End.Line
		n := c.NewRange.End.Line - c.NewRange.Start.Line + 1
		if e > s {
			switch {
			case after >= s+2 && after <= e+1:
				after = s + 1
			case after > e+1:
				after -= e - s
			}
		}
		if n > 1 && after >= s+2 {
			after += n - 1
		}
	}
	return after
}
