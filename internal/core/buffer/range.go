package buffer

// Range is a span of text from Start up to End (SPEC §6.2). A Range made by
// NewRange never has End before Start, as in VS Code's Range. A Range with Start
// equal to End is empty.
type Range struct {
	Start Position
	End   Position
}

// NewRange makes a range from two positions and swaps them when end is before
// start, as VS Code's Range constructor does.
func NewRange(start, end Position) Range {
	if end.IsBefore(start) {
		return Range{Start: end, End: start}
	}
	return Range{Start: start, End: end}
}

// IsEmpty reports whether the range covers no text.
func (r Range) IsEmpty() bool {
	return r.Start == r.End
}

// SpansMultipleLines reports whether the range starts and ends on different lines.
func (r Range) SpansMultipleLines() bool {
	return r.Start.Line != r.End.Line
}

// ContainsPosition reports whether p is inside the range; both ends count as inside.
func (r Range) ContainsPosition(p Position) bool {
	return r.Start.IsBeforeOrEqual(p) && p.IsBeforeOrEqual(r.End)
}

// StrictContainsPosition reports whether p is inside the range and is neither of
// its ends.
func (r Range) StrictContainsPosition(p Position) bool {
	return r.Start.IsBefore(p) && p.IsBefore(r.End)
}

// ContainsRange reports whether o lies inside r; o may share r's ends.
func (r Range) ContainsRange(o Range) bool {
	return r.Start.IsBeforeOrEqual(o.Start) && o.End.IsBeforeOrEqual(r.End)
}

// StrictContainsRange reports whether o lies inside r without sharing either of
// r's ends.
func (r Range) StrictContainsRange(o Range) bool {
	return r.Start.IsBefore(o.Start) && o.End.IsBefore(r.End)
}

// Union is the smallest range that contains both r and o (VS Code's plusRange).
func (r Range) Union(o Range) Range {
	return Range{Start: MinPosition(r.Start, o.Start), End: MaxPosition(r.End, o.End)}
}

// Intersect is the part r and o have in common. The second result is false when
// they have none; ranges that only touch give an empty range. This is VS Code's
// intersectRanges.
func (r Range) Intersect(o Range) (Range, bool) {
	start := MaxPosition(r.Start, o.Start)
	end := MinPosition(r.End, o.End)
	if end.IsBefore(start) {
		return Range{}, false
	}
	return Range{Start: start, End: end}, true
}

// CollapseToStart is the empty range at r's start.
func (r Range) CollapseToStart() Range {
	return Range{Start: r.Start, End: r.Start}
}

// CollapseToEnd is the empty range at r's end.
func (r Range) CollapseToEnd() Range {
	return Range{Start: r.End, End: r.End}
}

// WithStart replaces the start. The result is swapped when the new start is
// after the end, as in VS Code's setStartPosition.
func (r Range) WithStart(p Position) Range {
	return NewRange(p, r.End)
}

// WithEnd replaces the end. The result is swapped when the new end is before the
// start.
func (r Range) WithEnd(p Position) Range {
	return NewRange(r.Start, p)
}

// RangeFromPositions makes the range between two positions, in either order.
func RangeFromPositions(a, b Position) Range {
	return NewRange(a, b)
}

// AreIntersecting reports whether two ranges share text. Ranges that only touch
// do not: the doc comment of VS Code's Range.areIntersecting says they do, but its code
// and its tests say they don't.
func AreIntersecting(a, b Range) bool {
	return !a.End.IsBeforeOrEqual(b.Start) && !b.End.IsBeforeOrEqual(a.Start)
}

// AreIntersectingOrTouching reports whether two ranges overlap or share an end.
func AreIntersectingOrTouching(a, b Range) bool {
	return b.Start.IsBeforeOrEqual(a.End) && a.Start.IsBeforeOrEqual(b.End)
}

// CompareUsingStarts orders ranges by start, then by end.
func CompareUsingStarts(a, b Range) int {
	if c := a.Start.Compare(b.Start); c != 0 {
		return c
	}
	return a.End.Compare(b.End)
}

// CompareUsingEnds orders ranges by end, then by start.
func CompareUsingEnds(a, b Range) int {
	if c := a.End.Compare(b.End); c != 0 {
		return c
	}
	return a.Start.Compare(b.Start)
}
