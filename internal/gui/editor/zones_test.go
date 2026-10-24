package editor

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/folding"
	"github.com/rm4n0s/emetos/internal/core/wrap"
)

// zoneText is the lines the projection tables use: line 2 is two rows when wrapped at 10 columns.
const zoneText = "aaa\nbbb\nalpha beta gamma\nccc\nddd\neee\nfff\nggg\nhhh\niii"

// zoneBreaks are the breaks of the lines of zoneText wrapped at ten columns.
func zoneBreaks(text string) []*wrap.Breaks {
	opts := wrap.Options{TabSize: 4, Column: 10, Indent: wrap.ParseIndent("none"), BreakBefore: wrap.DefaultBreakBefore, BreakAfter: wrap.DefaultBreakAfter}
	lines := strings.Split(text, "\n")
	out := make([]*wrap.Breaks, len(lines))
	for i, l := range lines {
		out[i] = wrap.Compute(l, opts)
	}
	return out
}

// zoneRowsOf lists what every row of a projection shows: "L2.1" is the second row of line 2 (from 0)
// and "Z3.0" the first row of zone 3.
func zoneRowsOf(p Projection) []string {
	var out []string
	for row := range p.RowCount() {
		if id, index, ok := p.RowZone(row); ok {
			out = append(out, fmt.Sprintf("Z%d.%d", id, index))
			continue
		}
		line, _, _ := p.RowSpan(row)
		first, _ := p.LineRows(line)
		out = append(out, fmt.Sprintf("L%d.%d", line, row-first))
	}
	return out
}

func zoneItems(specs ...ZoneSpec) []zoneItem {
	out := make([]zoneItem, len(specs))
	for i, s := range specs {
		out[i] = zoneItem{id: ZoneID(i + 1), spec: s}
	}
	return out
}

func TestZonedProjectionLayouts(t *testing.T) {
	const h = 19
	lines := strings.Count(zoneText, "\n") + 1
	// lines 5 to 7 (from 0) are hidden: the fold is on line 4
	hidden := []folding.LineSpan{{First: 5, Last: 7}}
	plain := newIdentityProjection(lines, h)
	folded := newFoldedProjection(lines, h, hidden)
	wrapped := newWrappedProjection(lines, h, nil, zoneBreaks(zoneText))
	both := newWrappedProjection(lines, h, folded, zoneBreaks(zoneText))
	cases := []struct {
		name  string
		base  Projection
		zones []ZoneSpec
		want  string
	}{
		{"no zones", plain, nil, "L0.0 L1.0 L2.0 L3.0 L4.0 L5.0 L6.0 L7.0 L8.0 L9.0"},
		{"a zone after a line", plain, []ZoneSpec{{AfterLine: 3, Rows: 2}},
			"L0.0 L1.0 L2.0 Z1.0 Z1.1 L3.0 L4.0 L5.0 L6.0 L7.0 L8.0 L9.0"},
		{"above the first line and after the last", plain, []ZoneSpec{{AfterLine: 10, Rows: 3}, {AfterLine: 0, Rows: 1}},
			"Z2.0 L0.0 L1.0 L2.0 L3.0 L4.0 L5.0 L6.0 L7.0 L8.0 L9.0 Z1.0 Z1.1 Z1.2"},
		{"several zones after one line go by ordinal, then by when they were added", plain,
			[]ZoneSpec{{AfterLine: 1, Rows: 1, Ordinal: 5}, {AfterLine: 1, Rows: 2, Ordinal: 1}, {AfterLine: 1, Rows: 1, Ordinal: 5}},
			"L0.0 Z2.0 Z2.1 Z1.0 Z3.0 L1.0 L2.0 L3.0 L4.0 L5.0 L6.0 L7.0 L8.0 L9.0"},
		{"zones follow the last row of a wrapped line", wrapped, []ZoneSpec{{AfterLine: 3, Rows: 2}},
			"L0.0 L1.0 L2.0 L2.1 Z1.0 Z1.1 L3.0 L4.0 L5.0 L6.0 L7.0 L8.0 L9.0"},
		{"a zone after the line before a wrapped one", wrapped, []ZoneSpec{{AfterLine: 2, Rows: 1}},
			"L0.0 L1.0 Z1.0 L2.0 L2.1 L3.0 L4.0 L5.0 L6.0 L7.0 L8.0 L9.0"},
		{"a zone before a hidden line is hidden, even when it follows the line that holds the fold", folded,
			[]ZoneSpec{{AfterLine: 5, Rows: 1}},
			"L0.0 L1.0 L2.0 L3.0 L4.0 L8.0 L9.0"},
		{"a zone in a hidden area is hidden", folded, []ZoneSpec{{AfterLine: 6, Rows: 1}, {AfterLine: 7, Rows: 1}},
			"L0.0 L1.0 L2.0 L3.0 L4.0 L8.0 L9.0"},
		{"a zone after the last hidden line follows the line that holds the fold", folded, []ZoneSpec{{AfterLine: 8, Rows: 1}},
			"L0.0 L1.0 L2.0 L3.0 L4.0 Z1.0 L8.0 L9.0"},
		{"ShowInHiddenAreas shows a zone of a hidden area after the fold", folded,
			[]ZoneSpec{{AfterLine: 6, Rows: 2, ShowInHiddenAreas: true}},
			"L0.0 L1.0 L2.0 L3.0 L4.0 Z1.0 Z1.1 L8.0 L9.0"},
		{"zones with folds and wraps together", both, []ZoneSpec{
			{AfterLine: 3, Rows: 2},                          // 1: after the wrapped line
			{AfterLine: 5, Rows: 1},                          // 2: line 5 is hidden after it: hidden
			{AfterLine: 6, Rows: 1},                          // 3: in the hidden area
			{AfterLine: 8, Rows: 1},                          // 4: after the hidden area: follows the fold
			{AfterLine: 6, Rows: 1, ShowInHiddenAreas: true}, // 5: follows the fold too
			{AfterLine: 0, Rows: 1},                          // 6
			{AfterLine: 10, Rows: 3},                         // 7
			{AfterLine: 1, Rows: 2, Ordinal: 2},              // 8
			{AfterLine: 1, Rows: 1, Ordinal: 1},              // 9
		}, "Z6.0 L0.0 Z9.0 Z8.0 Z8.1 L1.0 L2.0 L2.1 Z1.0 Z1.1 L3.0 L4.0 Z4.0 Z5.0 L8.0 L9.0 Z7.0 Z7.1 Z7.2"},
		{"the zones of one place are by ordinal across lines, then by id", both, []ZoneSpec{
			{AfterLine: 6, Rows: 1, ShowInHiddenAreas: true, Ordinal: 9}, // 1
			{AfterLine: 8, Rows: 1, Ordinal: 3},                          // 2
			{AfterLine: 7, Rows: 1, ShowInHiddenAreas: true, Ordinal: 3}, // 3
		}, "L0.0 L1.0 L2.0 L2.1 L3.0 L4.0 Z2.0 Z3.0 Z1.0 L8.0 L9.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.base
			if len(tc.zones) > 0 {
				p = newZonedProjection(tc.base, lines, zoneItems(tc.zones...))
			}
			if got := strings.Join(zoneRowsOf(p), " "); got != tc.want {
				t.Fatalf("rows\n got %s\nwant %s", got, tc.want)
			}
			checkProjection(t, p, tc.base)
		})
	}
}

// checkProjection checks that the answers of a projection agree with each other, and that the rows of
// text are those of the base in order.
func checkProjection(t *testing.T, p, base Projection) {
	t.Helper()
	count := p.RowCount()
	if p.Height() != float64(count)*base.RowHeight(0) {
		t.Errorf("height %v for %d rows", p.Height(), count)
	}
	text := 0
	zoneRows := map[ZoneID][]int{}
	for row := range count {
		if p.RowAt(p.RowTop(row)) != row || p.RowAt(p.RowTop(row)+p.RowHeight(row)-0.01) != row {
			t.Errorf("RowAt(RowTop(%d)) = %d", row, p.RowAt(p.RowTop(row)))
		}
		id, index, ok := p.RowZone(row)
		line, start, end := p.RowSpan(row)
		if ok {
			zoneRows[id] = append(zoneRows[id], row)
			first, n := p.ZoneRows(id)
			if row != first+index || index >= n {
				t.Errorf("row %d is row %d of zone %d, which has rows %d+%d", row, index, id, first, n)
			}
			if start != 0 || end != 0 {
				t.Errorf("row %d of a zone shows the range %d-%d", row, start, end)
			}
			if prev := row - index - 1; prev >= 0 {
				l, _, _ := p.RowSpan(prev)
				if l != line {
					t.Errorf("zone row %d follows line %d but row %d is of line %d", row, line, prev, l)
				}
			}
			continue
		}
		wantLine, wantStart, wantEnd := base.RowSpan(text)
		if line != wantLine || start != wantStart || end != wantEnd {
			t.Errorf("row %d shows %d %d-%d, the base's row %d shows %d %d-%d", row, line, start, end, text, wantLine, wantStart, wantEnd)
		}
		first, n := p.LineRows(line)
		if row < first || row >= first+n {
			t.Errorf("row %d is of line %d, which has rows %d+%d", row, line, first, n)
		}
		text++
	}
	if text != base.RowCount() {
		t.Errorf("%d rows of text, the base has %d", text, base.RowCount())
	}
	for id, rows := range zoneRows {
		first, n := p.ZoneRows(id)
		if n != len(rows) || first != rows[0] || rows[len(rows)-1] != first+n-1 {
			t.Errorf("zone %d has rows %v, ZoneRows says %d+%d", id, rows, first, n)
		}
	}
	zones := 0
	for row := range count {
		if _, _, ok := p.RowZone(row); ok {
			zones++
		}
		if got := p.ZoneRowsBefore(row + 1); got != zones {
			t.Errorf("ZoneRowsBefore(%d) = %d, want %d", row+1, got, zones)
		}
	}
	if count-zones != base.RowCount() {
		t.Errorf("rows %d, zone rows %d, base %d", count, zones, base.RowCount())
	}
	// the rows of text of every line are together, and the zones of a line come right after them
	for line := range 64 {
		first, n := p.LineRows(line)
		if n == 0 {
			continue
		}
		zf, zn := p.LineZoneRows(line)
		if zn > 0 && zf != first+n {
			t.Errorf("zones of line %d start at row %d, its text ends at %d", line, zf, first+n)
		}
		for r := range zn {
			if _, _, ok := p.RowZone(zf + r); !ok {
				t.Errorf("row %d after line %d is not a zone row", zf+r, line)
			}
		}
	}
	if _, _, ok := p.RowZone(-1); ok {
		t.Error("row -1 is in a zone")
	}
	if _, _, ok := p.RowZone(count); ok {
		t.Error("the row after the last is in a zone")
	}
}

func TestZonedProjectionLookups(t *testing.T) {
	lines := strings.Count(zoneText, "\n") + 1
	base := newWrappedProjection(lines, 19, nil, zoneBreaks(zoneText))
	p := newZonedProjection(base, lines, zoneItems(
		ZoneSpec{AfterLine: 3, Rows: 2}, ZoneSpec{AfterLine: 3, Rows: 1, Ordinal: 1}, ZoneSpec{AfterLine: 1, Rows: 1}))
	// L0 Z3 L1 L2.0 L2.1 Z1 Z1 Z2 L3 ...
	if first, n := p.ZoneRows(1); first != 5 || n != 2 {
		t.Errorf("zone 1: rows %d+%d", first, n)
	}
	if first, n := p.ZoneRows(2); first != 7 || n != 1 {
		t.Errorf("zone 2: rows %d+%d", first, n)
	}
	if first, n := p.ZoneRows(99); first != 0 || n != 0 {
		t.Errorf("an unknown zone: rows %d+%d", first, n)
	}
	if first, n := p.LineZoneRows(2); first != 5 || n != 3 {
		t.Errorf("zones of line 2: rows %d+%d", first, n)
	}
	if first, n := p.LineZoneRows(0); first != 1 || n != 1 {
		t.Errorf("zones of line 0: rows %d+%d", first, n)
	}
	if first, n := p.LineZoneRows(3); first != 0 || n != 0 {
		t.Errorf("zones of line 3: rows %d+%d", first, n)
	}
	if first, n := p.LineRows(3); first != 8 || n != 1 {
		t.Errorf("rows of line 3: %d+%d", first, n)
	}
	if got := p.ZonesIn(0, 100); !reflect.DeepEqual(got, []ZoneID{3, 1, 2}) {
		t.Errorf("zones in all rows: %v", got)
	}
	if got := p.ZonesIn(6, 6); !reflect.DeepEqual(got, []ZoneID{1}) {
		t.Errorf("zones in row 6: %v", got)
	}
	if got := p.ZonesIn(8, 20); len(got) != 0 {
		t.Errorf("zones in the rows after the last zone: %v", got)
	}
	if got := p.ZoneRowsBefore(6); got != 2 {
		t.Errorf("zone rows before row 6: %d", got)
	}
	if got := p.RowAt(-5); got != 0 {
		t.Errorf("RowAt above the top: %d", got)
	}
	if got := p.RowAt(1e9); got != p.RowCount()-1 {
		t.Errorf("RowAt below the bottom: %d", got)
	}
	// a hidden line has no zone rows, and the first row it reports is that of the line that holds the fold
	folded := newFoldedProjection(lines, 19, []folding.LineSpan{{First: 5, Last: 7}})
	zp := newZonedProjection(folded, lines, zoneItems(ZoneSpec{AfterLine: 1, Rows: 3}))
	if first, n := zp.LineRows(6); first != 7 || n != 0 {
		t.Errorf("a hidden line: rows %d+%d", first, n)
	}
	if first, n := zp.LineZoneRows(6); first != 0 || n != 0 {
		t.Errorf("a hidden line has zone rows %d+%d", first, n)
	}
}

// naiveLayout lists the rows of a base projection with zones put in by plain loops: the oracle of the
// randomized test.
func naiveLayout(base Projection, lines int, specs []ZoneSpec) []string {
	var out []string
	zonesAt := func(at int) {
		for _, ord := range sortedOrdinals(specs) {
			for i, s := range specs {
				if s.Ordinal != ord {
					continue
				}
				if placeOf(base, lines, s) == at && shown(base, lines, s) {
					for r := range s.Rows {
						out = append(out, fmt.Sprintf("Z%d.%d", i+1, r))
					}
				}
			}
		}
	}
	zonesAt(-1)
	for row := range base.RowCount() {
		line, _, _ := base.RowSpan(row)
		first, _ := base.LineRows(line)
		out = append(out, fmt.Sprintf("L%d.%d", line, row-first))
		zonesAt(row)
	}
	return out
}

func sortedOrdinals(specs []ZoneSpec) []int {
	seen := map[int]bool{}
	var out []int
	for _, s := range specs {
		seen[s.Ordinal] = true
	}
	for o := range seen {
		out = append(out, o)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// placeOf is the base row a zone follows (-1 for the top).
func placeOf(base Projection, lines int, s ZoneSpec) int {
	if s.AfterLine == 0 {
		return -1
	}
	first, count := base.LineRows(min(s.AfterLine, lines) - 1)
	if count == 0 {
		return first
	}
	return first + count - 1
}

// shown tells whether the line after a zone has rows, or the zone is for hidden areas.
func shown(base Projection, lines int, s ZoneSpec) bool {
	if s.AfterLine == 0 || s.ShowInHiddenAreas {
		return true
	}
	_, count := base.LineRows(min(s.AfterLine, lines-1))
	return count > 0
}

func TestZonedProjectionRandomized(t *testing.T) {
	r := rand.New(rand.NewSource(94))
	for round := range 300 {
		lines := 1 + r.Intn(25)
		var sb []string
		for range lines {
			sb = append(sb, strings.Repeat("word ", r.Intn(8)))
		}
		text := strings.Join(sb, "\n")
		var hidden []folding.LineSpan
		for l := 1; l < lines; {
			if r.Intn(4) == 0 {
				last := min(l+r.Intn(4), lines-1)
				hidden = append(hidden, folding.LineSpan{First: l, Last: last})
				l = last + 2
				continue
			}
			l++
		}
		var folded *foldedProjection
		if len(hidden) > 0 {
			folded = newFoldedProjection(lines, 19, hidden)
		}
		var base Projection
		switch r.Intn(3) {
		case 0:
			if folded != nil {
				base = folded
			} else {
				base = newIdentityProjection(lines, 19)
			}
		default:
			base = newWrappedProjection(lines, 19, folded, zoneBreaks(text))
		}
		specs := make([]ZoneSpec, r.Intn(12))
		for i := range specs {
			specs[i] = ZoneSpec{AfterLine: r.Intn(lines + 1), Rows: 1 + r.Intn(4), Ordinal: r.Intn(3), ShowInHiddenAreas: r.Intn(4) == 0}
		}
		// the projection takes the zones sorted by line, then ordinal, then when they were added
		sorted := make([]zoneItem, len(specs))
		for i, s := range specs {
			sorted[i] = zoneItem{id: ZoneID(i + 1), spec: s}
		}
		sortItems(sorted)
		p := newZonedProjection(base, lines, sorted)
		want := naiveLayout(base, lines, specs)
		if got := zoneRowsOf(p); !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d: lines %d hidden %v specs %+v\n got %v\nwant %v", round, lines, hidden, specs, got, want)
		}
		checkProjection(t, p, base)
	}
}

// sortItems orders zones as ViewModel gives them to the projection.
func sortItems(items []zoneItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0; j-- {
			a, b := items[j-1], items[j]
			if a.spec.AfterLine < b.spec.AfterLine || (a.spec.AfterLine == b.spec.AfterLine && (a.spec.Ordinal < b.spec.Ordinal || (a.spec.Ordinal == b.spec.Ordinal && a.id < b.id))) {
				break
			}
			items[j-1], items[j] = items[j], items[j-1]
		}
	}
}

func TestFollowLines(t *testing.T) {
	change := func(old buffer.Range, added int) document.Change {
		return document.Change{OldRange: old, NewRange: rg(old.Start.Line, 0, old.Start.Line+added, 0)}
	}
	cases := []struct {
		name    string
		after   int
		changes []document.Change
		want    int
	}{
		{"an edit inside a line", 5, []document.Change{change(rg(2, 1, 2, 3), 0)}, 5},
		{"a line inserted below the zone", 3, []document.Change{change(rg(5, 0, 5, 0), 1)}, 3},
		{"a line inserted above the zone", 5, []document.Change{change(rg(1, 3, 1, 3), 1)}, 6},
		{"three lines inserted above the zone", 5, []document.Change{change(rg(1, 3, 1, 3), 3)}, 8},
		{"a line break at the end of the line the zone is after leaves it above the new line", 4, []document.Change{change(rg(3, 3, 3, 3), 1)}, 4},
		{"a line break in the line before the zone's line pushes the zone down", 4, []document.Change{change(rg(2, 3, 2, 3), 1)}, 5},
		{"lines deleted above the zone", 8, []document.Change{change(rg(2, 0, 4, 0), 0)}, 6},
		{"the line the zone is after is deleted: it goes after the line before them", 4, []document.Change{change(rg(2, 3, 5, 0), 0)}, 3},
		{"the zone is after the first of the lines that are joined", 3, []document.Change{change(rg(2, 3, 5, 0), 0)}, 3},
		{"the zone is after the last deleted line", 6, []document.Change{change(rg(2, 3, 5, 3), 0)}, 3},
		{"above the first line it stays", 0, []document.Change{change(rg(0, 0, 3, 0), 5)}, 0},
		{"a replacement of lines by more lines", 7, []document.Change{change(rg(1, 0, 3, 0), 5)}, 7 - 2 + 5},
		{"several changes of one edit, the last first", 9, []document.Change{change(rg(1, 0, 1, 0), 2), change(rg(5, 0, 6, 0), 0)}, 9 - 1 + 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := followLines(tc.after, tc.changes); got != tc.want {
				t.Errorf("followLines(%d) = %d, want %d", tc.after, got, tc.want)
			}
		})
	}
}

// zoneVM is a view model over the lines of zoneText.
func zoneVM(t *testing.T) *ViewModel {
	t.Helper()
	vm := NewViewModel(newTestDoc(zoneText), goOptions(), nil)
	t.Cleanup(vm.DisposeZones)
	return vm
}

func rowStrings(vm *ViewModel) string { return strings.Join(zoneRowsOf(vm.Projection()), " ") }

func TestViewModelZonesAddChangeRemove(t *testing.T) {
	vm := zoneVM(t)
	notified := 0
	vm.AddListener(func() { notified++ })
	base := rowStrings(vm)

	id, err := vm.AddZone(ZoneSpec{AfterLine: 2, Rows: 2})
	if err != nil || id == 0 {
		t.Fatalf("AddZone: %d %v", id, err)
	}
	if notified != 1 {
		t.Errorf("notified %d times by an add", notified)
	}
	if got := rowStrings(vm); got != "L0.0 L1.0 Z1.0 Z1.1 L2.0 L3.0 L4.0 L5.0 L6.0 L7.0 L8.0 L9.0" {
		t.Errorf("after the add: %s", got)
	}
	if vm.Projection().RowCount() != 12 || vm.Projection().Height() != 12*19 {
		t.Errorf("rows %d height %v", vm.Projection().RowCount(), vm.Projection().Height())
	}

	if err := vm.SetZoneRows(id, 1); err != nil || notified != 2 {
		t.Fatalf("SetZoneRows: %v, notified %d", err, notified)
	}
	if vm.Projection().RowCount() != 11 {
		t.Errorf("rows after a shorter zone: %d", vm.Projection().RowCount())
	}
	if err := vm.SetZoneAfterLine(id, 0); err != nil {
		t.Fatal(err)
	}
	if got := rowStrings(vm); !strings.HasPrefix(got, "Z1.0 L0.0") {
		t.Errorf("after moving the zone to the top: %s", got)
	}
	if err := vm.ChangeZone(id, func(s *ZoneSpec) { s.AfterLine, s.Ordinal = 10, 4 }); err != nil {
		t.Fatal(err)
	}
	if spec, ok := vm.Zone(id); !ok || spec.AfterLine != 10 || spec.Ordinal != 4 || spec.Rows != 1 {
		t.Errorf("zone %+v %v", spec, ok)
	}
	if got := rowStrings(vm); !strings.HasSuffix(got, "L9.0 Z1.0") {
		t.Errorf("after moving the zone to the end: %s", got)
	}
	if infos := vm.Zones(); len(infos) != 1 || infos[0].ID != id || infos[0].FirstRow != 10 || infos[0].ShownRows != 1 {
		t.Errorf("Zones: %+v", infos)
	}

	if err := vm.RemoveZone(id); err != nil {
		t.Fatal(err)
	}
	if got := rowStrings(vm); got != base {
		t.Errorf("after the removal: %s", got)
	}
	if len(vm.Zones()) != 0 {
		t.Errorf("Zones after the removal: %+v", vm.Zones())
	}
	if _, ok := vm.Zone(id); ok {
		t.Error("the removed zone is still there")
	}
}

func TestChangeZonesNotifiesOnce(t *testing.T) {
	vm := zoneVM(t)
	notified := 0
	vm.AddListener(func() { notified++ })
	var ids []ZoneID
	err := vm.ChangeZones(func(z ZoneChanges) error {
		for i := range 50 {
			id, err := z.Add(ZoneSpec{AfterLine: i % 10, Rows: 1})
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		if err := z.Remove(ids[0]); err != nil {
			return err
		}
		return z.Change(ids[1], func(s *ZoneSpec) { s.Rows = 3 })
	})
	if err != nil || notified != 1 {
		t.Fatalf("err %v notified %d", err, notified)
	}
	if got := vm.Projection().RowCount(); got != 10+49+2 {
		t.Errorf("rows %d", got)
	}
	// a batch that changes nothing tells nobody
	if err := vm.ChangeZones(func(ZoneChanges) error { return nil }); err != nil || notified != 1 {
		t.Errorf("err %v notified %d", err, notified)
	}
}

func TestZoneErrors(t *testing.T) {
	vm := zoneVM(t)
	id, err := vm.AddZone(ZoneSpec{AfterLine: 1, Rows: 2})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		do   func() error
		// entry is the method the call came through, route the end of the failure path
		entry, route string
	}{
		{"no rows", func() error { _, err := vm.AddZone(ZoneSpec{AfterLine: 1}); return err },
			"ViewModel.AddZone->ViewModel.ChangeZones", "ZoneChanges.Add->ViewModel.validateZone.EditorZoneInvalid"},
		{"a line before the top", func() error { _, err := vm.AddZone(ZoneSpec{AfterLine: -1, Rows: 1}); return err },
			"ViewModel.AddZone", "ViewModel.validateZone.EditorZoneInvalid"},
		{"a line past the end", func() error { _, err := vm.AddZone(ZoneSpec{AfterLine: 11, Rows: 1}); return err },
			"ViewModel.AddZone", "ViewModel.validateZone.EditorZoneInvalid"},
		{"a height that is changed to none", func() error { return vm.SetZoneRows(id, 0) },
			"ViewModel.SetZoneRows->ViewModel.ChangeZone", "ZoneChanges.Change->ViewModel.validateZone.EditorZoneInvalid"},
		{"a line that is changed to a bad one", func() error { return vm.SetZoneAfterLine(id, 99) },
			"ViewModel.SetZoneAfterLine->ViewModel.ChangeZone", "ZoneChanges.Change->ViewModel.validateZone.EditorZoneInvalid"},
		{"changing an unknown zone", func() error { return vm.ChangeZone(77, func(*ZoneSpec) {}) },
			"ViewModel.ChangeZone", "ZoneChanges.Change->ViewModel.findZone.EditorZoneNotFound"},
		{"changing a zone that was removed", func() error {
			gone, _ := vm.AddZone(ZoneSpec{AfterLine: 2, Rows: 1})
			if err := vm.RemoveZone(gone); err != nil {
				return err
			}
			return vm.SetZoneRows(gone, 2)
		}, "ViewModel.SetZoneRows", "ZoneChanges.Change->ViewModel.findZone.EditorZoneNotFound"},
		{"removing an unknown zone", func() error { return vm.RemoveZone(0) },
			"ViewModel.RemoveZone->ViewModel.ChangeZones", "ZoneChanges.Remove->ViewModel.findZone.EditorZoneNotFound"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.do()
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.entry) || !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	// nothing that failed changed the zone
	if spec, _ := vm.Zone(id); spec.Rows != 2 || spec.AfterLine != 1 {
		t.Errorf("the zone was changed by the failures: %+v", spec)
	}
	if len(vm.Zones()) != 1 {
		t.Errorf("zones: %+v", vm.Zones())
	}
}

func TestZonesFollowTheEdits(t *testing.T) {
	cases := []struct {
		name string
		edit buffer.Range
		text string
		zone int // AfterLine at the start
		want int
	}{
		{"a line inserted above", rg(0, 3, 0, 3), "\nnew", 4, 5},
		{"lines inserted above", rg(0, 3, 0, 3), "\nx\ny\nz", 4, 7},
		{"a line inserted below", rg(8, 3, 8, 3), "\nnew", 4, 4},
		{"text typed on the line of the zone", rg(3, 0, 3, 0), "text", 4, 4},
		{"a line break at the end of the line of the zone", rg(3, 3, 3, 3), "\n", 4, 4},
		{"the lines above are deleted", rg(0, 0, 2, 0), "", 6, 4},
		{"the line of the zone is deleted", rg(2, 3, 4, 3), "", 4, 3},
		{"the lines before and with the line of the zone are deleted", rg(1, 3, 6, 3), "", 5, 2},
		{"the document is replaced by a shorter one", rg(0, 0, 9, 3), "x\ny", 8, 1},
		{"a zone above the first line stays", rg(0, 0, 4, 0), "", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := zoneVM(t)
			id, err := vm.AddZone(ZoneSpec{AfterLine: tc.zone, Rows: 2})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := vm.Document().Edit([]document.Edit{{Range: tc.edit, Text: tc.text}}, document.EditOptions{}); err != nil {
				t.Fatal(err)
			}
			spec, _ := vm.Zone(id)
			if spec.AfterLine != tc.want {
				t.Errorf("the zone is after line %d, want %d", spec.AfterLine, tc.want)
			}
			// the projection has the zone after the line it names
			first, _ := vm.Projection().ZoneRows(id)
			lines := vm.Document().Snapshot().LineCount()
			if spec.AfterLine > lines {
				t.Fatalf("after line %d of %d", spec.AfterLine, lines)
			}
			if wantFirst := spec.AfterLine; first != wantFirst {
				t.Errorf("the zone starts at row %d, want %d", first, wantFirst)
			}
		})
	}
}

func TestZonesShrinkWithTheDocument(t *testing.T) {
	vm := zoneVM(t)
	id, err := vm.AddZone(ZoneSpec{AfterLine: 10, Rows: 1})
	if err != nil {
		t.Fatal(err)
	}
	// the lines the zone is after are replaced: it comes after the first of them, as in VS Code
	vm.Document().SetText("a\nb")
	if spec, _ := vm.Zone(id); spec.AfterLine != 1 {
		t.Errorf("after the document was replaced by a shorter one: %d", spec.AfterLine)
	}
	if got := rowStrings(vm); got != "L0.0 Z1.0 L1.0" {
		t.Errorf("rows %s", got)
	}
	// an event that says the document has fewer lines than the zone's place clamps it
	vm.zones.byID[id].spec.AfterLine = 7
	vm.zonesDocumentChanged(document.ChangeEvent{After: newTestDoc("a\nb").Snapshot()})
	if spec, _ := vm.Zone(id); spec.AfterLine != 2 {
		t.Errorf("clamped to the last line: %d", spec.AfterLine)
	}
	if got := rowStrings(vm); got != "L0.0 L1.0 Z1.0" {
		t.Errorf("rows %s", got)
	}
	// after DisposeZones the zones stay but stop following the document
	vm.DisposeZones()
	if _, err := vm.Document().Edit([]document.Edit{{Range: rg(0, 0, 0, 0), Text: "x\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if spec, _ := vm.Zone(id); spec.AfterLine != 2 {
		t.Errorf("a disposed view model still follows the edits: %d", spec.AfterLine)
	}
}

func TestZonesWithFoldsAndWrapsInAViewModel(t *testing.T) {
	vm := wrapVM(t, "short\nalpha beta gamma delta epsilon\nlong enough to wrap twice here ok\nend\nlast\nmore", "on", 12, nil)
	// line 1 and line 2 wrap; a zone after each, and one before the first line
	a, _ := vm.AddZone(ZoneSpec{AfterLine: 2, Rows: 2})
	b, _ := vm.AddZone(ZoneSpec{AfterLine: 3, Rows: 1})
	c, _ := vm.AddZone(ZoneSpec{AfterLine: 0, Rows: 1})
	proj := vm.Projection()
	checkProjection(t, proj, vm.baseProj)
	if first, n := proj.LineRows(1); n < 2 {
		t.Fatalf("line 1 is %d rows from %d", n, first)
	}
	firstA, nA := proj.ZoneRows(a)
	firstL1, nL1 := proj.LineRows(1)
	if firstA != firstL1+nL1 || nA != 2 {
		t.Errorf("zone a: rows %d+%d, line 1 rows %d+%d", firstA, nA, firstL1, nL1)
	}
	if first, _ := proj.ZoneRows(c); first != 0 {
		t.Errorf("zone c at row %d", first)
	}
	firstB, _ := proj.ZoneRows(b)
	if firstL2, nL2 := proj.LineRows(2); firstB != firstL2+nL2 {
		t.Errorf("zone b at %d, line 2 ends at %d", firstB, firstL2+nL2)
	}
	// a change of the wrap width changes the rows of the lines, and the zones stay with their lines
	setWrapViewport(vm, 40)
	proj = vm.Projection()
	checkProjection(t, proj, vm.baseProj)
	firstA2, _ := proj.ZoneRows(a)
	firstL1b, nL1b := proj.LineRows(1)
	if firstA2 != firstL1b+nL1b {
		t.Errorf("after the wrap changed, zone a at %d, line 1 ends at %d", firstA2, firstL1b+nL1b)
	}
	if nL1b >= nL1 {
		t.Errorf("a wider editor did not change the rows: %d then %d", nL1, nL1b)
	}
}
