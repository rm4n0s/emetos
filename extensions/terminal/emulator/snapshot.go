package emulator

import "strings"

// Cursor is the state of the cursor in a Frame.
type Cursor struct {
	X, Y        int
	Visible     bool
	Shape       CursorShape
	Blink       bool
	WrapPending bool
}

// Frame is a copy of the screen (or of some lines) for the user interface. Snapshot and ReadLines fill it, reusing its
// slices, so a Frame that is kept between frames allocates nothing in the common case.
type Frame struct {
	Rows, Cols int
	// Cells are Rows*Cols cells, row-major. Only the rows with Dirty set were copied by this Snapshot; the others still hold
	// what the Frame had (the picture of the last frame).
	Cells []Cell
	// Wrapped says for each row that its text goes on in the next one; Dirty that the row changed since the last Snapshot.
	Wrapped, Dirty []bool
	// Clusters are, for each row that has some, the texts of its CellCluster cells (Rune is the index).
	Clusters [][]string
	// Full says that everything changed (the first frame, a resize, the other screen, a scroll of the whole screen, a new
	// palette); every row is then Dirty.
	Full bool
	// Changed says that anything changed since the last Snapshot: rows, the cursor, the modes, the palette. A Snapshot that
	// returns false has not touched the Frame.
	Changed bool
	Cursor  Cursor
	Modes   Modes
	// Styles and Links are the tables the cells' Style indexes point into (Style.Link points into Links).
	Styles []Style
	Links  []Link
	// Palette are the 256 colors as 0xRRGGBB and Fg, Bg and CursorColor the default colors, with the program's changes.
	Palette             [256]uint32
	Fg, Bg, CursorColor uint32
	// Scrollback is the number of lines above the screen, and Top the absolute number of row 0 (see OSCEvent.Line).
	Scrollback int
	Top        int64
	// Synchronized says that the program began a synchronized update (DECSET 2026) and the frame was not updated.
	Synchronized bool

	styleGen   uint32
	paletteGen uint32
	valid      bool
}

// Cell returns the cell at row y, column x.
func (f *Frame) Cell(y, x int) Cell { return f.Cells[y*f.Cols+x] }

// Row returns the cells of row y.
func (f *Frame) Row(y int) []Cell { return f.Cells[y*f.Cols : (y+1)*f.Cols] }

// Text returns the text of the cell at row y, column x (a space for an empty one, "" for the right half of a wide
// character).
func (f *Frame) Text(y, x int) string {
	c := f.Cells[y*f.Cols+x]
	switch {
	case c.Flags&CellTail != 0:
		return ""
	case c.Flags&CellCluster != 0:
		if y < len(f.Clusters) && int(c.Rune) < len(f.Clusters[y]) {
			return f.Clusters[y][c.Rune]
		}
		return " "
	case c.Rune == 0:
		return " "
	}
	return string(rune(c.Rune))
}

// RowText returns the text of row y with the trailing spaces removed.
func (f *Frame) RowText(y int) string {
	var sb strings.Builder
	for x := 0; x < f.Cols; x++ {
		if f.Cells[y*f.Cols+x].Flags&CellWideFill != 0 {
			continue
		}
		sb.WriteString(f.Text(y, x))
	}
	return strings.TrimRight(sb.String(), " ")
}

// StyleOf returns the style of cell c.
func (f *Frame) StyleOf(c Cell) Style {
	if int(c.Style) < len(f.Styles) {
		return f.Styles[c.Style]
	}
	return Style{}
}

// LinkOf returns the hyperlink of style s (the zero Link when it has none).
func (f *Frame) LinkOf(s Style) Link {
	if s.Link != 0 && int(s.Link) < len(f.Links) {
		return f.Links[s.Link]
	}
	return Link{}
}

func (f *Frame) size(rows, cols int) {
	n := rows * cols
	if cap(f.Cells) < n {
		f.Cells = make([]Cell, n)
	} else {
		f.Cells = f.Cells[:n]
	}
	f.Wrapped = growBools(f.Wrapped, rows)
	f.Dirty = growBools(f.Dirty, rows)
	if cap(f.Clusters) < rows {
		f.Clusters = make([][]string, rows)
	} else {
		f.Clusters = f.Clusters[:rows]
	}
	f.Rows, f.Cols = rows, cols
}

func growBools(b []bool, n int) []bool {
	if cap(b) < n {
		return make([]bool, n)
	}
	return b[:n]
}

// copyRow copies line l into row y of the frame.
func (f *Frame) copyRow(y int, l *line) {
	copy(f.Cells[y*f.Cols:(y+1)*f.Cols], l.cells)
	f.Wrapped[y] = l.wrapped
	if l.ext != nil && len(l.ext.clusters) > 0 {
		f.Clusters[y] = append(f.Clusters[y][:0], l.ext.clusters...)
	} else {
		f.Clusters[y] = f.Clusters[y][:0]
	}
	f.Dirty[y] = true
}

// copyTables brings the style and link tables of the frame up to date; it reports whether they were rebuilt (a compaction
// changed the indexes, so every row has to be copied again).
func (e *Emulator) copyTables(f *Frame) (rebuilt bool) {
	t := e.st
	if !f.valid || f.styleGen != t.gen {
		f.Styles = append(f.Styles[:0], t.styles...)
		f.Links = append(f.Links[:0], t.links...)
		f.styleGen = t.gen
		return true
	}
	if len(f.Styles) < len(t.styles) {
		f.Styles = append(f.Styles, t.styles[len(f.Styles):]...)
	}
	if len(f.Links) < len(t.links) {
		f.Links = append(f.Links, t.links[len(f.Links):]...)
	}
	return false
}

// Snapshot copies what changed on the screen since the last call into f, under a short lock, and returns whether anything
// changed. When nothing did it returns false at once and allocates nothing. While the program has a synchronized update
// open (DECSET 2026) it leaves f as it is and sets f.Synchronized, so the user interface keeps its last picture; the
// changes are delivered by the first Snapshot after the update ends (see EndSynchronizedUpdate).
func (e *Emulator) Snapshot(f *Frame) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.modes.SyncOutput {
		f.Synchronized = true
		f.Changed = false
		return false
	}
	f.Synchronized = false
	if f.valid && !e.changed && f.Rows == e.rows && f.Cols == e.cols {
		f.Changed = false
		return false
	}
	s := e.scr
	full := e.allDirty || !f.valid || f.Rows != s.rows || f.Cols != s.cols
	if f.Rows != s.rows || f.Cols != s.cols {
		f.size(s.rows, s.cols)
	}
	if e.copyTables(f) {
		full = true
	}
	for y := 0; y < s.rows; y++ {
		l := s.row(y)
		if full || l.dirty {
			f.copyRow(y, l)
		} else {
			f.Dirty[y] = false
		}
		l.dirty = false
	}
	f.Full = full
	f.Changed = true
	f.Cursor = Cursor{X: s.x, Y: s.y, Visible: e.modes.CursorVisible, Shape: e.modes.CursorShape, Blink: e.modes.CursorBlink, WrapPending: s.wrapNext}
	f.Modes = e.modes
	if full || f.paletteGen != e.paletteGen {
		f.Palette = e.palette
		f.Fg, f.Bg, f.CursorColor = e.fg, e.bg, e.cur
		f.paletteGen = e.paletteGen
	}
	f.Scrollback = s.scrollbackLen()
	f.Top = s.absLine(0)
	f.valid = true
	e.changed, e.allDirty = false, false
	return true
}

// EndSynchronizedUpdate ends a synchronized update (DECSET 2026) the program left open; the host calls it when the update
// lasts too long (xterm and VS Code give up after a second).
func (e *Emulator) EndSynchronizedUpdate() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.modes.SyncOutput {
		e.modes.SyncOutput = false
		e.changed = true
	}
}

// ReadLines copies lines [first, first+n) of the current buffer into f, counting from the oldest line of the scrollback
// (0) to the last row of the screen (Scrollback + Rows - 1); lines outside are not copied. Every row of f is Dirty. It
// does not clear the changes Snapshot tracks. Use it to draw the scrollback or to search it.
func (e *Emulator) ReadLines(first, n int, f *Frame) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.scr
	first = max(first, 0)
	if first+n > s.ring.n {
		n = s.ring.n - first
	}
	if n < 0 {
		n = 0
	}
	e.copyTables(f)
	f.size(n, s.cols)
	for i := 0; i < n; i++ {
		f.copyRow(i, s.ring.at(first+i))
	}
	f.Full, f.Changed = true, true
	f.Cursor = Cursor{X: s.x, Y: s.ring.n - s.rows + s.y - first, Visible: e.modes.CursorVisible}
	f.Modes = e.modes
	f.Scrollback = s.scrollbackLen()
	f.Top = s.ring.trimmed + int64(first)
	f.Palette = e.palette
	f.Fg, f.Bg, f.CursorColor = e.fg, e.bg, e.cur
	f.valid = false
}

// LineCount is the number of lines of the current buffer: the scrollback and the screen.
func (e *Emulator) LineCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.scr.ring.n
}

// LineText returns the text of line i (counted as in ReadLines) with trailing spaces removed, and whether the line goes on
// in the next one. It is what a search over the scrollback reads.
func (e *Emulator) LineText(i int) (text string, wrapped bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.scr
	if i < 0 || i >= s.ring.n {
		return "", false
	}
	l := s.ring.at(i)
	b := l.text(nil, 0, len(l.cells))
	return strings.TrimRight(string(b), " "), l.wrapped
}

// Text returns the text of the whole buffer from line first to line last (inclusive, as in ReadLines): rows that autowrap
// joined are joined, the others are separated by a newline, trailing spaces are removed.
func (e *Emulator) Text(first, last int) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.scr
	first = max(first, 0)
	last = min(last, s.ring.n-1)
	var out []byte
	for i := first; i <= last; i++ {
		l := s.ring.at(i)
		row := l.text(nil, 0, len(l.cells))
		if !l.wrapped {
			row = trimSpaceRight(row)
		}
		out = append(out, row...)
		if !l.wrapped && i < last {
			out = append(out, '\n')
		}
	}
	return string(out)
}

func trimSpaceRight(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == ' ' {
		b = b[:len(b)-1]
	}
	return b
}
