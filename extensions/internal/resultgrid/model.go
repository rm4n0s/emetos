package resultgrid

import (
	"slices"
	"unicode/utf8"
)

// SortDir is the direction of the sort of a column.
type SortDir int

// The directions.
const (
	// SortNone shows the rows in the order the source has them.
	SortNone SortDir = iota
	// SortAsc sorts from the smallest to the largest, NULL first.
	SortAsc
	// SortDesc sorts from the largest to the smallest, NULL last.
	SortDesc
)

// The limits and the defaults of the columns.
const (
	// MinColumnWidth and MaxColumnWidth bound the width of a column, in pixels.
	MinColumnWidth = 40.0
	MaxColumnWidth = 1200.0
	// autoMinWidth and autoMaxWidth bound the width a column gets from its content.
	autoMinWidth = 64.0
	autoMaxWidth = 320.0
	// charWidth is the width of a character of the UI font, in pixels, for the guess of the width of a column; cellPadding is the
	// room the cell adds around its text.
	charWidth   = 7.4
	cellPadding = 20.0
	// autoSample is how many rows are looked at to size a column.
	autoSample = 200
	// sortIconWidth is the room the title of a sorted column leaves for its arrow.
	sortIconWidth = 18.0
)

// ModelOptions configure a Model.
type ModelOptions struct {
	// NullText is how NULL is written in the cells and in a copy; "NULL" when empty.
	NullText string
	// MaxRunes is where a long text is cut in a cell; DefaultMaxRunes when 0.
	MaxRunes int
}

// Model is the grid without its widgets: the source, the order of the rows after the sort, the selection, the widths of the columns,
// the text of a copy and the export. It is used on the UI thread; Export gives a view that another goroutine may read.
type Model struct {
	opts ModelOptions
	src  Source
	cols []Column
	// order maps a row as shown to a row of the source; nil is the order of the source. It is replaced by a new slice when the
	// sort changes and never changed in place, so that an export that holds it is not disturbed.
	order []int32

	sortCol int
	sortDir SortDir
	sel     Selection
	widths  []float64

	scratch []Value
	// version counts the changes of what the grid shows, for the widget to build again.
	version int
}

// NewModel makes a model with no source.
func NewModel(opts ModelOptions) *Model { return &Model{opts: opts, sortCol: -1} }

// touch records a change.
func (m *Model) touch() { m.version++ }

// Version counts the changes of the model; a widget builds again when it moves.
func (m *Model) Version() int { return m.version }

// Display are the options that write a value in a cell.
func (m *Model) Display() DisplayOptions {
	return DisplayOptions{NullText: m.opts.NullText, MaxRunes: m.opts.MaxRunes}
}

// SetSource shows another result: the sort and the selection are cleared and the columns get widths from their content.
func (m *Model) SetSource(src Source) {
	m.src = src
	m.order, m.sortCol, m.sortDir, m.sel = nil, -1, SortNone, Selection{}
	m.cols = nil
	if src != nil {
		m.cols = src.Columns()
	}
	m.scratch = make([]Value, len(m.cols))
	m.widths = make([]float64, len(m.cols))
	for c := range m.cols {
		m.widths[c] = m.contentWidth(c)
	}
	m.touch()
}

// Appended tells the model that the source has more rows (a fetch): the sort is applied to all of them, the selection stays.
func (m *Model) Appended() {
	if m.sortDir != SortNone {
		m.applySort()
	}
	m.touch()
}

// Columns are the columns of the source.
func (m *Model) Columns() []Column { return m.cols }

// Len is the number of rows.
func (m *Model) Len() int {
	if m.src == nil {
		return 0
	}
	return m.src.Len()
}

// SourceRow is the row of the source that is shown at row r.
func (m *Model) SourceRow(r int) int {
	if m.order == nil {
		return r
	}
	return int(m.order[r])
}

// Row writes the values of the row r as shown into dst, which has a place for each column.
func (m *Model) Row(r int, dst []Value) { m.src.Row(m.SourceRow(r), dst) }

// Value is the value of a cell. It reads a whole row of the source, so a caller that wants several cells of a row uses Row.
func (m *Model) Value(r, c int) Value {
	m.Row(r, m.scratch)
	return m.scratch[c]
}

// Widths.

// Width is the width of a column in pixels.
func (m *Model) Width(c int) float64 { return m.widths[c] }

// TotalWidth is the width of all the columns.
func (m *Model) TotalWidth() float64 {
	var w float64
	for _, x := range m.widths {
		w += x
	}
	return w
}

// SetWidth sets the width of a column, within the limits.
func (m *Model) SetWidth(c int, w float64) {
	if c < 0 || c >= len(m.widths) {
		return
	}
	w = min(max(w, MinColumnWidth), MaxColumnWidth)
	if m.widths[c] != w {
		m.widths[c] = w
		m.touch()
	}
}

// FitWidth sets the width of a column to what its content needs (a double click on the edge of its title).
func (m *Model) FitWidth(c int) {
	if c >= 0 && c < len(m.widths) {
		m.SetWidth(c, m.contentWidth(c))
	}
}

// contentWidth guesses the width a column needs from its title and its first rows: the width of that many characters of the UI
// font, in the limits of the auto width.
func (m *Model) contentWidth(c int) float64 {
	chars := utf8.RuneCountInString(m.cols[c].Name)
	opts := m.Display()
	buf := make([]Value, len(m.cols))
	n := min(m.Len(), autoSample)
	for r := range n {
		m.src.Row(m.SourceRow(r), buf)
		text, _ := buf[c].Display(opts)
		chars = max(chars, utf8.RuneCountInString(text))
	}
	w := float64(chars)*charWidth + cellPadding + sortIconWidth
	return min(max(w, autoMinWidth), autoMaxWidth)
}

// Sort.

// SortState is the column that is sorted and its direction; the column is -1 when none is.
func (m *Model) SortState() (col int, dir SortDir) {
	if m.sortDir == SortNone {
		return -1, SortNone
	}
	return m.sortCol, m.sortDir
}

// SortBy sorts by a column, or, with SortNone, shows the rows in the order of the source. The selection is cleared: it names rows
// as they are shown.
func (m *Model) SortBy(c int, d SortDir) {
	if c < 0 || c >= len(m.cols) {
		d = SortNone
	}
	m.sortCol, m.sortDir = c, d
	if d == SortNone {
		m.sortCol = -1
		m.order = nil
	} else {
		m.applySort()
	}
	m.sel = Selection{}
	m.touch()
}

// CycleSort is the click on the title of a column: ascending, then descending, then the order of the source.
func (m *Model) CycleSort(c int) {
	switch {
	case m.sortCol != c || m.sortDir == SortNone:
		m.SortBy(c, SortAsc)
	case m.sortDir == SortAsc:
		m.SortBy(c, SortDesc)
	default:
		m.SortBy(c, SortNone)
	}
}

// applySort puts the rows in the order of the sorted column. It reads that column of every row of the source once.
func (m *Model) applySort() {
	n := m.Len()
	keys := make([]Value, n)
	buf := make([]Value, len(m.cols))
	for i := range n {
		clear(buf)
		m.src.Row(i, buf)
		keys[i] = buf[m.sortCol]
	}
	idx := make([]int32, n)
	for i := range idx {
		idx[i] = int32(i)
	}
	sign := 1
	if m.sortDir == SortDesc {
		sign = -1
	}
	slices.SortStableFunc(idx, func(a, b int32) int { return sign * Compare(keys[a], keys[b]) })
	m.order = idx
}
