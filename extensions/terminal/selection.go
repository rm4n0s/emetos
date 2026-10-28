package terminal

// pos is a place in the buffer: a line by its absolute number (which does not change when the scrollback drops lines) and a
// column.
type pos struct {
	line int64
	col  int
}

func (p pos) before(q pos) bool { return p.line < q.line || p.line == q.line && p.col < q.col }

// selection is what the user selected with the mouse or a command: from the anchor to the head, both cells included.
type selection struct {
	active       bool
	anchor, head pos
}

// normalized returns the selection with its start before its end, and whether there is one.
func (s selection) normalized() (selection, bool) {
	if !s.active {
		return s, false
	}
	if s.head.before(s.anchor) {
		s.anchor, s.head = s.head, s.anchor
	}
	return s, true
}

// rangeOn is the cells [x0, x1) of the line the selection covers, for a normalized selection and a line of cols cells.
func (s selection) rangeOn(line int64, cols int) (x0, x1 int, ok bool) {
	if line < s.anchor.line || line > s.head.line {
		return 0, 0, false
	}
	x0, x1 = 0, cols
	if line == s.anchor.line {
		x0 = s.anchor.col
	}
	if line == s.head.line {
		x1 = s.head.col + 1
	}
	x0, x1 = max(x0, 0), min(x1, cols)
	return x0, x1, x1 > x0
}
