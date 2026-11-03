package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// What the editor shows for the coverage of a file (SPEC §10.32 "The editor", step 26.4), worked out from the details the controller gave:
// the gutter marks of the covered and uncovered lines, with the inline backgrounds and the counts when the inline coverage is on. This
// part is plain data (no widget), so a table tests it.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/codeCoverageDecorations.ts (apply, describe)

// The theme colors of the coverage marks.
const (
	covGutterHit   = "testing.coveredGutterBackground"
	covGutterMiss  = "testing.uncoveredGutterBackground"
	covInlineHit   = "testing.coveredBackground"
	covInlineMiss  = "testing.uncoveredBackground"
	covCountFg     = "testing.coverCountBadgeForeground"
	covCountBg     = "testing.coverCountBadgeBackground"
	covBranchBg    = "testing.uncoveredBranchBackground"
	covCountCutoff = 99
)

// coverageLayer is what the editor shows.
type coverageLayer struct {
	// marks are the gutter marks (and, inline, the backgrounds of the text) and notes the counts after the lines.
	marks []editor.BackgroundMark
	notes []editor.LineAnnotation
	// missed are the lines (counted from 0) that have uncovered code and none that ran, in order.
	missed []int
}

// buildCoverageLayer works the layer out. textOf gives the text of a range of the file, for the hovers.
//
// A line is covered when a statement or a branch on it ran, uncovered when only ones that did not run are on it; a line that has both is
// covered in the gutter, and with the inline coverage on the parts that did not run are marked on their own.
func buildCoverageLayer(d coretesting.Details, ranges []coretesting.DetailRange, inline bool, textOf func(coretesting.Range) string) coverageLayer {
	var out coverageLayer
	type state struct{ hit, miss bool }
	lines := map[int]*state{}
	for _, r := range ranges {
		if r.Kind == coretesting.DetailDeclaration || r.Range.Start == r.Range.End {
			continue
		}
		first, last := r.Range.Start.Line, r.Range.End.Line
		if r.Range.End.Col == 0 && last > first {
			last--
		}
		for l := first; l <= last; l++ {
			st := lines[l]
			if st == nil {
				st = &state{}
				lines[l] = st
			}
			if r.Hit() {
				st.hit = true
			} else {
				st.miss = true
			}
		}
	}
	numbers := make([]int, 0, len(lines))
	for l := range lines {
		numbers = append(numbers, l)
	}
	sort.Ints(numbers)
	// consecutive lines of one kind are one mark
	for i := 0; i < len(numbers); {
		j := i
		covered := lines[numbers[i]].hit
		for j+1 < len(numbers) && numbers[j+1] == numbers[j]+1 && lines[numbers[j+1]].hit == covered {
			j++
		}
		id := covGutterMiss
		if covered {
			id = covGutterHit
		}
		out.marks = append(out.marks, editor.BackgroundMark{
			Range:     buffer.Range{Start: buffer.Position{Line: numbers[i]}, End: buffer.Position{Line: numbers[j]}},
			WholeLine: true, GutterID: id,
		})
		i = j + 1
	}
	for _, l := range numbers {
		if st := lines[l]; st.miss && !st.hit {
			out.missed = append(out.missed, l)
		}
	}
	if !inline {
		return out
	}
	counted := map[int]bool{}
	for _, r := range ranges {
		if r.Kind == coretesting.DetailDeclaration || r.Range.Start == r.Range.End {
			continue
		}
		id := covInlineMiss
		if r.Hit() {
			id = covInlineHit
		}
		out.marks = append(out.marks, editor.BackgroundMark{
			Range:        buffer.Range{Start: buffer.Position{Line: r.Range.Start.Line, Col: r.Range.Start.Col}, End: buffer.Position{Line: r.Range.End.Line, Col: r.Range.End.Col}},
			BackgroundID: id,
		})
		if r.Primary && r.Hit() && !counted[r.Range.Start.Line] {
			counted[r.Range.Start.Line] = true
			out.notes = append(out.notes, editor.LineAnnotation{
				Line: r.Range.Start.Line, Text: " " + countText(r.Executed) + " ", Color: covCountFg, Background: covCountBg, Tight: true,
				Hover: describeDetail(d, r, textOf),
			})
		}
	}
	// a statement that ran with some of its branches not run says how many were
	for i, st := range d.Statements {
		if st.Executed == 0 || len(st.Branches) == 0 {
			continue
		}
		covered := 0
		for _, b := range st.Branches {
			if b.Executed > 0 {
				covered++
			}
		}
		if covered == len(st.Branches) {
			continue
		}
		out.notes = append(out.notes, editor.LineAnnotation{
			Line: st.Location.Start.Line, Text: fmt.Sprintf(" %d/%d branches ", covered, len(st.Branches)), Color: covCountFg, Background: covBranchBg, Tight: true,
			Hover: describeDetail(d, coretesting.DetailRange{Kind: coretesting.DetailStatement, Statement: i, Branch: -1, Declaration: -1, Executed: st.Executed}, textOf),
		})
	}
	return out
}

// countText is the badge of how many times something ran: "3x", "99+x" from a hundred.
func countText(n int) string {
	if n > covCountCutoff {
		return "99+x"
	}
	return fmt.Sprintf("%dx", n)
}

// wrapName writes a name or a piece of code in backticks, shortened, for the Markdown of a hover.
func wrapName(s string) string {
	if len(s) > 50 {
		s = s[:40] + "..."
	}
	return "`" + strings.NewReplacer("\n", "", "\r", "", "`", "").Replace(s) + "`"
}

// executedText says how often something named ran.
func executedText(name string, n int) string {
	switch {
	case n <= 0:
		return fmt.Sprintf("`%s` was not executed.", name)
	}
	return fmt.Sprintf("`%s` was executed %d time(s).", name, n)
}

// describeDetail is the Markdown of the hover of a detail: what ran, how often.
func describeDetail(d coretesting.Details, r coretesting.DetailRange, textOf func(coretesting.Range) string) string {
	stmtText := func(i int) string {
		t := strings.TrimSpace(textOf(d.Statements[i].Location))
		if t == "" {
			t = "<empty statement>"
		}
		return wrapName(t)
	}
	switch r.Kind {
	case coretesting.DetailDeclaration:
		return executedText(d.Declarations[r.Declaration].Name, r.Executed)
	case coretesting.DetailStatement:
		st := d.Statements[r.Statement]
		if len(st.Branches) > 0 {
			covered := 0
			for _, b := range st.Branches {
				if b.Executed > 0 {
					covered++
				}
			}
			return fmt.Sprintf("%d of %d of branches in %s were covered.", covered, len(st.Branches), stmtText(r.Statement))
		}
		name := strings.Trim(stmtText(r.Statement), "`")
		return executedText(name, r.Executed)
	}
	br := d.Statements[r.Statement].Branches[r.Branch]
	label := fmt.Sprintf("#%d", r.Branch+1)
	if br.Label != "" {
		label = "`" + strings.NewReplacer("\n", "", "\r", "", "`", "").Replace(br.Label) + "`"
	}
	if br.Executed <= 0 {
		return fmt.Sprintf("Branch %s in %s was not covered.", label, stmtText(r.Statement))
	}
	return fmt.Sprintf("Branch %s in %s was executed %d time(s).", label, stmtText(r.Statement), br.Executed)
}

// nextMissed is the line to go to from a line: the first uncovered one after it, else (the list wraps) the first; the previous one for
// forward false. -1 when there is none.
func nextMissed(missed []int, from int, forward bool) int {
	if len(missed) == 0 {
		return -1
	}
	if forward {
		for _, l := range missed {
			if l > from {
				return l
			}
		}
		return missed[0]
	}
	for i := len(missed) - 1; i >= 0; i-- {
		if missed[i] < from {
			return missed[i]
		}
	}
	return missed[len(missed)-1]
}
