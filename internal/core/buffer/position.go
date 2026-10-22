package buffer

// Position is a place in a text: a zero-based line and a zero-based byte column
// (SPEC §6.2, D6). It is the counterpart of VS Code's IPosition, which counts
// lines and UTF-16 columns from 1.
type Position struct {
	Line int
	Col  int
}

// Compare orders two positions: negative when p is before q, zero when they are
// the same place, positive when p is after q.
func (p Position) Compare(q Position) int {
	if p.Line != q.Line {
		return cmpInt(p.Line, q.Line)
	}
	return cmpInt(p.Col, q.Col)
}

// Equal reports whether p and q are the same place.
func (p Position) Equal(q Position) bool {
	return p == q
}

// IsBefore reports whether p is strictly before q.
func (p Position) IsBefore(q Position) bool {
	return p.Compare(q) < 0
}

// IsBeforeOrEqual reports whether p is before q or the same place.
func (p Position) IsBeforeOrEqual(q Position) bool {
	return p.Compare(q) <= 0
}

// Delta moves p by a number of lines and byte columns.
func (p Position) Delta(lines, cols int) Position {
	return Position{Line: p.Line + lines, Col: p.Col + cols}
}

// With replaces the line and the column.
func (p Position) With(line, col int) Position {
	return Position{Line: line, Col: col}
}

// MinPosition is the earlier of two positions.
func MinPosition(a, b Position) Position {
	if b.IsBefore(a) {
		return b
	}
	return a
}

// MaxPosition is the later of two positions.
func MaxPosition(a, b Position) Position {
	if a.IsBefore(b) {
		return b
	}
	return a
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
