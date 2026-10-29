package resultgrid

import (
	"slices"
	"strings"
	"testing"
)

func TestSortCyclesAndOrdersTheRows(t *testing.T) {
	m := NewModel(ModelOptions{})
	m.SetSource(people())
	if got := column(m, 1); !slices.Equal(got, []string{"carol", "Alice", "bob", "Bob"}) {
		t.Fatalf("unsorted = %v", got)
	}
	tests := []struct {
		name string
		do   func()
		col  int
		want []string
		dir  SortDir
	}{
		{"ascending by name", func() { m.CycleSort(1) }, 1, []string{"Alice", "Bob", "bob", "carol"}, SortAsc},
		{"descending by name", func() { m.CycleSort(1) }, 1, []string{"carol", "bob", "Bob", "Alice"}, SortDesc},
		{"back to the order of the source", func() { m.CycleSort(1) }, 1, []string{"carol", "Alice", "bob", "Bob"}, SortNone},
		{"ints ascending", func() { m.CycleSort(0) }, 0, []string{"1", "2", "3", "10"}, SortAsc},
		{"another column starts ascending", func() { m.CycleSort(2) }, 2, []string{"NULL", "-1", "7.5", "9.25"}, SortAsc},
		{"null last when descending", func() { m.CycleSort(2) }, 2, []string{"9.25", "7.5", "-1", "NULL"}, SortDesc},
		{"times", func() { m.SortBy(3, SortAsc) }, 3, []string{"NULL", "1985-01-02T00:00:00Z", "1990-05-17T00:00:00Z", "2001-12-31T00:00:00Z"}, SortAsc},
	}
	for _, tt := range tests {
		tt.do()
		col, dir := m.SortState()
		if dir != tt.dir || (dir != SortNone && col != tt.col) {
			t.Errorf("%s: SortState = %d, %v", tt.name, col, dir)
		}
		if got := column(m, tt.col); !slices.Equal(got, tt.want) {
			t.Errorf("%s: column = %v, want %v", tt.name, got, tt.want)
		}
	}
	// a sort is stable: the rows that are equal keep the order of the source
	m.SortBy(1, SortAsc)
	m.SortBy(4, SortAsc) // the note column: one NULL, then text
	if got := column(m, 1); got[0] != "carol" {
		t.Errorf("a NULL note first: %v", got)
	}
	// the source row of a displayed row
	m.SortBy(0, SortAsc)
	if m.SourceRow(0) != 1 || m.SourceRow(3) != 3 {
		t.Errorf("SourceRow = %d, %d", m.SourceRow(0), m.SourceRow(3))
	}
	// a column that is not there sorts nothing
	m.SortBy(9, SortAsc)
	if _, dir := m.SortState(); dir != SortNone {
		t.Error("a sort by a column that does not exist")
	}
}

func TestSortClearsTheSelectionAndAppendKeepsIt(t *testing.T) {
	tbl := people()
	m := NewModel(ModelOptions{})
	m.SetSource(tbl)
	m.Press(1, 1, Mods{})
	m.SortBy(0, SortAsc)
	if m.Selection().Kind != SelNone {
		t.Error("the selection survived a sort, but it names rows as they were shown")
	}
	m.Press(0, 0, Mods{})
	tbl.Append([]Value{Int(0), Text("zed")})
	m.Appended()
	if m.Len() != 5 || m.Selection().Kind == SelNone {
		t.Errorf("after Appended: %d rows, selection %+v", m.Len(), m.Selection())
	}
	// the new row is sorted in with the others
	if got := column(m, 0); !slices.Equal(got, []string{"0", "1", "2", "3", "10"}) {
		t.Errorf("column = %v", got)
	}
}

func TestSelectionRules(t *testing.T) {
	m := NewModel(ModelOptions{})
	m.SetSource(people())
	type want struct {
		kind           SelKind
		r0, c0, r1, c1 int
		cells, rows    int
	}
	tests := []struct {
		name string
		do   func()
		want want
	}{
		{"a click selects the cell", func() { m.Press(1, 2, Mods{}) }, want{SelCells, 1, 2, 1, 2, 1, 1}},
		{"shift extends the rectangle from the anchor", func() { m.Press(3, 4, Mods{Shift: true}) }, want{SelCells, 1, 2, 3, 4, 9, 3}},
		{"shift to the other side", func() { m.Press(0, 0, Mods{Shift: true}) }, want{SelCells, 0, 0, 1, 2, 6, 2}},
		{"a click on a number selects the row", func() { m.PressRow(2, Mods{}) }, want{SelRows, 2, 0, 2, 4, 5, 1}},
		{"shift on a number extends the rows", func() { m.PressRow(0, Mods{Shift: true}) }, want{SelRows, 0, 0, 2, 4, 15, 3}},
		{"a column", func() { m.PressColumn(1, Mods{}) }, want{SelColumns, 0, 1, 3, 1, 4, 4}},
		{"shift on a column extends", func() { m.PressColumn(3, Mods{Shift: true}) }, want{SelColumns, 0, 1, 3, 3, 12, 4}},
		{"select all", func() { m.SelectAll() }, want{SelAll, 0, 0, 3, 4, 20, 4}},
		{"arrows start at the first cell", func() { m.ClearSelection(); m.Move(1, 0, false) }, want{SelCells, 0, 0, 0, 0, 1, 1}},
		{"down", func() { m.Move(1, 0, false) }, want{SelCells, 1, 0, 1, 0, 1, 1}},
		{"right extending", func() { m.Move(0, 1, true) }, want{SelCells, 1, 0, 1, 1, 2, 1}},
		{"the focus stops at the edge", func() { m.MoveTo(99, 99, false) }, want{SelCells, 3, 4, 3, 4, 1, 1}},
		{"up past the first row", func() { m.MoveTo(-5, 0, false) }, want{SelCells, 0, 0, 0, 0, 1, 1}},
		{"a click outside is ignored", func() { m.Press(99, 0, Mods{}) }, want{SelCells, 0, 0, 0, 0, 1, 1}},
		{"escape clears", func() { m.ClearSelection() }, want{SelNone, 0, 0, 0, 0, 0, 0}},
	}
	for _, tt := range tests {
		tt.do()
		sel := m.Selection()
		r0, c0, r1, c1, ok := sel.Bounds(m.Len(), len(m.Columns()))
		got := want{sel.Kind, r0, c0, r1, c1, m.SelectedCells(), m.SelectedRows()}
		if !ok {
			got = want{sel.Kind, 0, 0, 0, 0, 0, 0}
		}
		if got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
	empty := NewModel(ModelOptions{})
	empty.SelectAll()
	empty.MoveTo(0, 0, false)
	empty.Press(0, 0, Mods{})
	if empty.Selection().Kind != SelNone {
		t.Error("a selection in an empty grid")
	}
}

func TestCopyFormats(t *testing.T) {
	m := NewModel(ModelOptions{})
	m.SetSource(people())
	tests := []struct {
		name string
		got  func() string
		want string
	}{
		{"a cell", func() string { return m.CopyCell(1, 4) }, "line one\nline two"},
		{"a null cell", func() string { return m.CopyCell(0, 4) }, "NULL"},
		{"a time", func() string { return m.CopyCell(0, 3) }, "1990-05-17T00:00:00Z"},
		{"a cell out of the grid", func() string { return m.CopyCell(9, 9) }, ""},
		{"a row", func() string { return m.CopyRow(0, false) }, "3\tcarol\t7.5\t1990-05-17T00:00:00Z\tNULL"},
		{"a row with headers", func() string { return m.CopyRow(0, true) }, "id\tname\tscore\tborn\tnote\n3\tcarol\t7.5\t1990-05-17T00:00:00Z\tNULL"},
		{"a row with quoting", func() string { return m.CopyRow(3, false) }, "10\tBob\t-1\tNULL\t\"say \"\"hi\"\"\""},
		{"a row with a line break", func() string { return m.CopyRow(1, false) }, "1\tAlice\t9.25\t1985-01-02T00:00:00Z\t\"line one\nline two\""},
		{"a column", func() string { return m.CopyColumn(1, false) }, "carol\nAlice\nbob\nBob"},
		{"a column with its header", func() string { return m.CopyColumn(0, true) }, "id\n3\n1\n2\n10"},
		{"a column that is not there", func() string { return m.CopyColumn(9, false) }, ""},
		{"nothing selected", func() string { return m.CopySelection(false) }, ""},
		{"everything", func() string { return m.CopyAll(true) }, "id\tname\tscore\tborn\tnote\n" +
			"3\tcarol\t7.5\t1990-05-17T00:00:00Z\tNULL\n" +
			"1\tAlice\t9.25\t1985-01-02T00:00:00Z\t\"line one\nline two\"\n" +
			"2\tbob\tNULL\t2001-12-31T00:00:00Z\t\"tab\there\"\n" +
			"10\tBob\t-1\tNULL\t\"say \"\"hi\"\"\""},
	}
	for _, tt := range tests {
		if got := tt.got(); got != tt.want {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, got, tt.want)
		}
	}
	m.Press(0, 1, Mods{})
	if got := m.CopySelection(false); got != "carol" {
		t.Errorf("a selected cell is its text as it is: %q", got)
	}
	m.Press(1, 2, Mods{Shift: true})
	if got := m.CopySelection(true); got != "name\tscore\ncarol\t7.5\nAlice\t9.25" {
		t.Errorf("a rectangle: %q", got)
	}
	m.PressRow(2, Mods{})
	if got := m.CopySelection(false); got != "2\tbob\tNULL\t2001-12-31T00:00:00Z\t\"tab\there\"" {
		t.Errorf("a row: %q", got)
	}
	// the copy follows the sort
	m.SortBy(0, SortDesc)
	if got := m.CopyColumn(0, false); got != "10\n3\n2\n1" {
		t.Errorf("a sorted column: %q", got)
	}
	nulls := NewModel(ModelOptions{NullText: "∅"})
	nulls.SetSource(people())
	if got := nulls.CopyCell(0, 4); got != "∅" {
		t.Errorf("NullText: %q", got)
	}
	empty := NewModel(ModelOptions{})
	if empty.CopyAll(true) != "" || empty.CopyRow(0, false) != "" {
		t.Error("a copy from nothing")
	}
}

func TestColumnWidths(t *testing.T) {
	m := NewModel(ModelOptions{})
	m.SetSource(NewFuncSource([]Column{{Name: "id"}, {Name: "a_much_longer_title_than_any_value"}, {Name: "text"}}, 5, func(i int, dst []Value) {
		dst[0] = Int(int64(i))
		dst[1] = Text("x")
		dst[2] = Text(strings.Repeat("w", 500))
	}))
	w0, w1, w2 := m.Width(0), m.Width(1), m.Width(2)
	if w0 != autoMinWidth {
		t.Errorf("a short column is %v wide, want the minimum %v", w0, autoMinWidth)
	}
	if w1 <= w0 || w1 >= autoMaxWidth {
		t.Errorf("a long title is %v wide", w1)
	}
	if w2 != autoMaxWidth {
		t.Errorf("a long value is %v wide, want the maximum %v", w2, autoMaxWidth)
	}
	m.SetWidth(0, 5)
	if m.Width(0) != MinColumnWidth {
		t.Errorf("width = %v", m.Width(0))
	}
	m.SetWidth(0, 99999)
	if m.Width(0) != MaxColumnWidth {
		t.Errorf("width = %v", m.Width(0))
	}
	m.SetWidth(7, 100) // not a column
	m.FitWidth(0)
	if m.Width(0) != w0 {
		t.Errorf("FitWidth = %v, want %v", m.Width(0), w0)
	}
	if m.TotalWidth() != m.Width(0)+m.Width(1)+m.Width(2) {
		t.Error("TotalWidth")
	}
	v := m.Version()
	m.SetWidth(0, m.Width(0))
	if m.Version() != v {
		t.Error("an unchanged width counts as a change")
	}
}
