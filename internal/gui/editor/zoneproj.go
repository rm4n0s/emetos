package editor

import (
	"math"
	"sort"
)

// Ported from VS Code: src/vs/editor/common/viewLayout/linesLayout.ts (whitespaces: their order by line and
// ordinal, and the rows they add before a line) and
// src/vs/editor/browser/viewParts/viewZones/viewZones.ts (_computeWhitespaceProps: where a zone sits and when it
// is in a hidden area)

// ZoneID names a view zone of a view model. The ids of a view model are never 0.
type ZoneID int

// noZones is embedded in the projections that have no view zones.
type noZones struct{}

// RowZone implements Projection: no row belongs to a zone.
func (noZones) RowZone(int) (ZoneID, int, bool) { return 0, 0, false }

// LineZoneRows implements Projection: no zone follows a line.
func (noZones) LineZoneRows(int) (first, count int) { return 0, 0 }

// ZoneRows implements Projection: no zone has rows.
func (noZones) ZoneRows(ZoneID) (first, count int) { return 0, 0 }

// ZonesIn implements Projection: no zone has a row.
func (noZones) ZonesIn(int, int) []ZoneID { return nil }

// ZoneRowsBefore implements Projection: no zone row comes before a row.
func (noZones) ZoneRowsBefore(int) int { return 0 }

// zoneItem is a zone as the projection needs it.
type zoneItem struct {
	id   ZoneID
	spec ZoneSpec
}

// zoneSlot is a zone that is shown: the base row it follows (-1 for the top of the document) and
// its rows.
type zoneSlot struct {
	id   ZoneID
	at   int
	rows int
	// ord is the ordinal of the zone, which orders the zones of one place.
	ord int
	// line is the line the zone follows on the screen: that of row at, or 0.
	line int
}

// zonedProjection puts the rows of view zones between the rows of another projection (SPEC §9.1).
// Every row has the height of the base's rows; a zone is a whole number of rows. The rows of a
// zone follow the last row of the line they come after (a hidden line has none: the zone follows
// the row of the line that holds the fold), the zones of one place by ordinal and then in the order
// they were added.
//
// The base row r is the row r + (the rows of the zones that follow a row before r). A row of a zone
// shows no text: RowSpan gives it the line it follows and an empty range.
type zonedProjection struct {
	base   Projection
	height float64
	zones  []zoneSlot
	// cum[i] is the number of rows of zones[:i+1]; start[i] the first row of zones[i].
	cum, start []int
	index      map[ZoneID]int
	total      int
}

// newZonedProjection puts the zones that are shown on a base projection of lines lines. The items
// come sorted by the line they follow, then by ordinal, then by id (the order they were added); the
// projection orders the rows by the place they follow, which differs only for zones of hidden lines.
// A zone is shown when the line after it is not hidden (VS Code), or when it says it is to be shown in
// hidden areas; a zone after the last line follows the last line.
func newZonedProjection(base Projection, lines int, items []zoneItem) *zonedProjection {
	p := &zonedProjection{base: base, height: base.RowHeight(0), index: map[ZoneID]int{}}
	slots := make([]zoneSlot, 0, len(items))
	for _, it := range items {
		s := it.spec
		slot := zoneSlot{id: it.id, rows: s.Rows, ord: s.Ordinal}
		if s.AfterLine <= 0 {
			slot.at = -1
		} else {
			after := min(s.AfterLine, max(lines, 1)) - 1
			next := min(s.AfterLine, max(lines, 1)-1)
			if _, count := base.LineRows(next); count == 0 && !s.ShowInHiddenAreas {
				continue // the line after the zone is in a hidden area
			}
			first, count := base.LineRows(after)
			slot.at = first
			if count > 0 {
				slot.at = first + count - 1
			}
			slot.line, _, _ = base.RowSpan(slot.at)
		}
		slots = append(slots, slot)
	}
	byPlace := func(i, j int) bool {
		if slots[i].at != slots[j].at {
			return slots[i].at < slots[j].at
		}
		if slots[i].ord != slots[j].ord {
			return slots[i].ord < slots[j].ord
		}
		return slots[i].id < slots[j].id
	}
	// the items come sorted by the line they follow: only zones that follow a hidden line, or the
	// last of several that follow the same row, can be out of order
	if !sort.SliceIsSorted(slots, byPlace) {
		sort.Slice(slots, byPlace)
	}
	p.zones = slots
	p.cum, p.start = make([]int, len(slots)), make([]int, len(slots))
	sum := 0
	for i, z := range slots {
		p.start[i] = z.at + 1 + sum
		sum += z.rows
		p.cum[i] = sum
		p.index[z.id] = i
	}
	p.total = sum
	return p
}

// before is the number of rows of the zones that follow a row before the base row r.
func (p *zonedProjection) before(r int) int {
	k := sort.Search(len(p.zones), func(i int) bool { return p.zones[i].at >= r })
	if k == 0 {
		return 0
	}
	return p.cum[k-1]
}

// zoneOf is the index of the zone that has a row, or -1; otherwise baseRow is the base row it shows.
func (p *zonedProjection) zoneOf(row int) (zone, baseRow int) {
	k := sort.Search(len(p.zones), func(i int) bool { return p.start[i] > row }) - 1
	if k < 0 {
		return -1, row
	}
	if row < p.start[k]+p.zones[k].rows {
		return k, 0
	}
	return -1, row - p.cum[k]
}

// RowCount implements Projection.
func (p *zonedProjection) RowCount() int { return p.base.RowCount() + p.total }

// LineRows implements Projection: the rows of the line's text, not those of the zones after it.
func (p *zonedProjection) LineRows(line int) (first, count int) {
	first, count = p.base.LineRows(line)
	return first + p.before(first), count
}

// RowSpan implements Projection.
func (p *zonedProjection) RowSpan(row int) (line, start, end int) {
	row = min(max(row, 0), p.RowCount()-1)
	z, r := p.zoneOf(row)
	if z >= 0 {
		return p.zones[z].line, 0, 0
	}
	return p.base.RowSpan(r)
}

// RowTop implements Projection.
func (p *zonedProjection) RowTop(row int) float64 { return float64(row) * p.height }

// RowHeight implements Projection.
func (p *zonedProjection) RowHeight(int) float64 { return p.height }

// RowAt implements Projection.
func (p *zonedProjection) RowAt(y float64) int {
	if p.height <= 0 || math.IsNaN(y) {
		return 0
	}
	return min(max(int(math.Floor(y/p.height)), 0), p.RowCount()-1)
}

// Height implements Projection.
func (p *zonedProjection) Height() float64 { return float64(p.RowCount()) * p.height }

// RowZone implements Projection.
func (p *zonedProjection) RowZone(row int) (id ZoneID, index int, ok bool) {
	if row < 0 || row >= p.RowCount() {
		return 0, 0, false
	}
	z, _ := p.zoneOf(row)
	if z < 0 {
		return 0, 0, false
	}
	return p.zones[z].id, row - p.start[z], true
}

// LineZoneRows implements Projection: the rows of the zones that follow the last row of a line.
func (p *zonedProjection) LineZoneRows(line int) (first, count int) {
	f, c := p.base.LineRows(line)
	if c == 0 {
		return 0, 0
	}
	at := f + c - 1
	lo := sort.Search(len(p.zones), func(i int) bool { return p.zones[i].at >= at })
	hi := sort.Search(len(p.zones), func(i int) bool { return p.zones[i].at > at })
	if lo == hi {
		return 0, 0
	}
	return p.start[lo], p.cum[hi-1] - p.cum[lo] + p.zones[lo].rows
}

// ZoneRows implements Projection.
func (p *zonedProjection) ZoneRows(id ZoneID) (first, count int) {
	i, ok := p.index[id]
	if !ok {
		return 0, 0
	}
	return p.start[i], p.zones[i].rows
}

// ZonesIn implements Projection.
func (p *zonedProjection) ZonesIn(first, last int) []ZoneID {
	lo := max(sort.Search(len(p.zones), func(i int) bool { return p.start[i]+p.zones[i].rows > first }), 0)
	var ids []ZoneID
	for i := lo; i < len(p.zones) && p.start[i] <= last; i++ {
		ids = append(ids, p.zones[i].id)
	}
	return ids
}

// ZoneRowsBefore implements Projection: the rows of zones among the rows 0 to row-1.
func (p *zonedProjection) ZoneRowsBefore(row int) int {
	if row <= 0 {
		return 0
	}
	k := sort.Search(len(p.zones), func(i int) bool { return p.start[i] >= row })
	// the zones before k lie wholly before row; the zone k-1 may be cut by it
	if k == 0 {
		return 0
	}
	whole := p.cum[k-1]
	if over := p.start[k-1] + p.zones[k-1].rows - row; over > 0 {
		whole -= over
	}
	return whole
}

// RowIndentColumns is the indentation of the base's row; a zone's rows have none.
func (p *zonedProjection) RowIndentColumns(row int) int {
	z, r := p.zoneOf(min(max(row, 0), p.RowCount()-1))
	if z >= 0 {
		return 0
	}
	if ri, ok := p.base.(rowIndenter); ok {
		return ri.RowIndentColumns(r)
	}
	return 0
}
