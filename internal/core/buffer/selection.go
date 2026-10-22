package buffer

// Direction tells which end of a selection is the active one.
type Direction int

const (
	// LTR is a selection whose anchor is at its start and whose active end is at
	// its end: made by moving the cursor forward.
	LTR Direction = iota
	// RTL is a selection whose anchor is at its end and whose active end is at its
	// start: made by moving the cursor backward.
	RTL
)

// Selection is a range with a direction (SPEC §6.2): the anchor stays where the
// selection began and the active end is where the cursor is. An empty selection
// is a cursor.
type Selection struct {
	Anchor Position
	Active Position
}

// NewCursor is the empty selection at p.
func NewCursor(p Position) Selection {
	return Selection{Anchor: p, Active: p}
}

// SelectionFromRange makes the selection that covers r and has the direction d.
func SelectionFromRange(r Range, d Direction) Selection {
	if d == LTR {
		return Selection{Anchor: r.Start, Active: r.End}
	}
	return Selection{Anchor: r.End, Active: r.Start}
}

// Range is the text the selection covers, start before end.
func (s Selection) Range() Range {
	return NewRange(s.Anchor, s.Active)
}

// Start is the earlier end.
func (s Selection) Start() Position {
	return MinPosition(s.Anchor, s.Active)
}

// End is the later end.
func (s Selection) End() Position {
	return MaxPosition(s.Anchor, s.Active)
}

// IsEmpty reports whether the selection is a cursor.
func (s Selection) IsEmpty() bool {
	return s.Anchor == s.Active
}

// Direction is LTR when the active end is at the end of the selection. An empty
// selection is LTR, as in VS Code.
func (s Selection) Direction() Direction {
	if s.Anchor == s.Start() {
		return LTR
	}
	return RTL
}

// WithStart replaces the start and keeps the direction, as VS Code's
// setStartPosition does: the anchor is the start of an LTR selection, the active
// end of an RTL one.
func (s Selection) WithStart(p Position) Selection {
	if s.Direction() == LTR {
		return Selection{Anchor: p, Active: s.Active}
	}
	return Selection{Anchor: s.Anchor, Active: p}
}

// WithEnd replaces the end and keeps the direction, as VS Code's setEndPosition does.
func (s Selection) WithEnd(p Position) Selection {
	if s.Direction() == LTR {
		return Selection{Anchor: s.Anchor, Active: p}
	}
	return Selection{Anchor: p, Active: s.Active}
}

// Equal reports whether two selections have the same anchor and active end
// (VS Code's selectionsEqual).
func (s Selection) Equal(o Selection) bool {
	return s == o
}

// SelectionsEqual reports whether two lists hold equal selections in the same order.
func SelectionsEqual(a, b []Selection) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
