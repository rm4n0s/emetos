package testing

import "slices"

// The details of a file's coverage laid out for the editor: statements, branches and declarations can contain each other (a function and
// the statements in it), and the editor needs ranges that do not overlap, each with the detail that applies.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/codeCoverageDecorations.ts (CoverageDetailsModel)

// DetailKind says what a DetailRange is a range of. The order is the order a tie of two ranges is broken in: a declaration comes first,
// so that a function of one statement still gets the range of its statement.
type DetailKind int

const (
	// DetailDeclaration is a function or other declaration.
	DetailDeclaration DetailKind = iota
	// DetailStatement is a statement.
	DetailStatement
	// DetailBranch is a branch of a statement.
	DetailBranch
)

// DetailRange is a stretch of the text of a file and the detail that covers it.
type DetailRange struct {
	Range Range
	// Primary says that the range is the start of its detail (the first piece, when the detail was cut by one inside it); the count of
	// times it ran is written there.
	Primary bool
	Kind    DetailKind
	// Executed is how many times the statement, branch or declaration ran.
	Executed int
	// Statement is the index in Details.Statements of the statement (and of the statement a branch belongs to) and Branch the index of the
	// branch in it; Declaration is the index in Details.Declarations. The ones that do not apply are -1.
	Statement, Branch, Declaration int
}

// Hit says whether the range ran.
func (d DetailRange) Hit() bool { return d.Executed > 0 }

func posLess(a, b Pos) bool { return a.Line < b.Line || a.Line == b.Line && a.Col < b.Col }

func normRange(a, b Pos) Range {
	if posLess(b, a) {
		a, b = b, a
	}
	return Range{Start: a, End: b}
}

func (r Range) isEmpty() bool { return r.Start == r.End }

// containsPos says that a position is inside a range, the end not included: a detail that starts where another ends is next to it, not in
// it (VS Code includes the end, which makes ranges that touch overlap).
func (r Range) containsPos(p Pos) bool { return !posLess(p, r.Start) && posLess(p, r.End) }

func comparePos(a, b Pos) int {
	switch {
	case posLess(a, b):
		return -1
	case posLess(b, a):
		return 1
	}
	return 0
}

// BuildDetailRanges cuts the details of a file into ranges that do not overlap, in the order of the text: where a detail holds another
// (a function with its statements, a statement with a nested one) the outer one keeps the pieces around the inner one. A branch with no
// location is the empty range at the end of its statement.
func BuildDetailRanges(d Details) []DetailRange {
	var all []*DetailRange
	for i, de := range d.Declarations {
		all = append(all, &DetailRange{Range: normRange(de.Location.Start, de.Location.End), Primary: true, Kind: DetailDeclaration,
			Executed: de.Executed, Statement: -1, Branch: -1, Declaration: i})
	}
	for i, st := range d.Statements {
		all = append(all, &DetailRange{Range: normRange(st.Location.Start, st.Location.End), Primary: true, Kind: DetailStatement,
			Executed: st.Executed, Statement: i, Branch: -1, Declaration: -1})
	}
	for i, st := range d.Statements {
		for j, br := range st.Branches {
			at := normRange(st.Location.End, st.Location.End)
			if br.Location != nil {
				at = normRange(br.Location.Start, br.Location.End)
			}
			all = append(all, &DetailRange{Range: at, Primary: true, Kind: DetailBranch, Executed: br.Executed, Statement: i, Branch: j, Declaration: -1})
		}
	}
	slices.SortStableFunc(all, func(a, b *DetailRange) int {
		if c := comparePos(a.Range.Start, b.Range.Start); c != 0 {
			return c
		}
		if c := comparePos(a.Range.End, b.Range.End); c != 0 {
			return c
		}
		return int(a.Kind) - int(b.Kind)
	})

	var stack []*DetailRange
	var result []DetailRange
	pop := func() {
		next := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if len(stack) > 0 {
			prev := stack[len(stack)-1]
			prev.Range = normRange(next.Range.End, prev.Range.End)
		}
		result = append(result, *next)
	}
	for _, item := range all {
		start := item.Range.Start
		for len(stack) > 0 && !stack[len(stack)-1].Range.containsPos(start) {
			pop()
		}
		if item.Range.isEmpty() {
			result = append(result, *item)
			continue
		}
		if len(stack) > 0 {
			prev := stack[len(stack)-1]
			primary := prev.Primary
			before := normRange(prev.Range.Start, start)
			prev.Range = normRange(item.Range.End, prev.Range.End)
			prev.Primary = false
			if prev.Range.isEmpty() {
				stack = stack[:len(stack)-1]
			}
			piece := *prev
			piece.Range, piece.Primary = before, primary
			result = append(result, piece)
		}
		stack = append(stack, item)
	}
	for len(stack) > 0 {
		pop()
	}
	return result
}
