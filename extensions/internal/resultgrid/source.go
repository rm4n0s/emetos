package resultgrid

import "sync"

// Column describes a column of a result.
type Column struct {
	// Name is the title of the column.
	Name string
	// Type is the type the database gave the column ("integer", "varchar(40)"); it is shown in the hover of the title.
	Type string
}

// Source gives the rows of a result. The grid reads only the rows in view (and the whole of a column when it is sorted or
// exported), so a Source may make a row when it is asked for it. Its methods are called on the UI thread, and on the goroutine
// of an export; a Source that changes while it is shown (Table.Append) tells the grid with Grid.Appended.
type Source interface {
	// Columns are the columns; they do not change.
	Columns() []Column
	// Len is the number of rows now.
	Len() int
	// Row writes the values of row i into dst, which has one place for each column. i is in [0, Len).
	Row(i int, dst []Value)
}

// Table is a Source that holds its rows in memory: the result of a query. It is safe for use from several goroutines: a fetch that
// appends rows and an export that reads them do not race.
type Table struct {
	cols []Column

	mu   sync.RWMutex
	rows [][]Value
}

var _ Source = (*Table)(nil)

// NewTable makes an empty table with the columns.
func NewTable(cols []Column) *Table { return &Table{cols: append([]Column(nil), cols...)} }

// Append adds rows; a row shorter than the columns is filled with NULL and a longer one is cut. The rows are copied.
func (t *Table) Append(rows ...[]Value) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range rows {
		row := make([]Value, len(t.cols))
		copy(row, r)
		t.rows = append(t.rows, row)
	}
}

// Columns implements Source.
func (t *Table) Columns() []Column { return t.cols }

// Len implements Source.
func (t *Table) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.rows)
}

// Row implements Source.
func (t *Table) Row(i int, dst []Value) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	copy(dst, t.rows[i])
}

// FuncSource is a Source whose rows are made when they are asked for: a result of 100,000 rows takes no memory until it is looked
// at, and only the rows in view are made.
type FuncSource struct {
	cols []Column
	n    int
	fn   func(i int, dst []Value)
}

var _ Source = (*FuncSource)(nil)

// NewFuncSource makes a source of n rows; fn writes the values of row i into dst (one place for each column, all NULL when fn
// is called).
func NewFuncSource(cols []Column, n int, fn func(i int, dst []Value)) *FuncSource {
	return &FuncSource{cols: append([]Column(nil), cols...), n: n, fn: fn}
}

// Columns implements Source.
func (s *FuncSource) Columns() []Column { return s.cols }

// Len implements Source.
func (s *FuncSource) Len() int { return s.n }

// SetLen changes the number of rows (more rows were fetched).
func (s *FuncSource) SetLen(n int) { s.n = n }

// Row implements Source.
func (s *FuncSource) Row(i int, dst []Value) {
	clear(dst)
	s.fn(i, dst)
}
