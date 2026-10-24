package diff

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// format writes changes as "[1,3)->[1,4) [7,7)->[8,9)".
func format(changes []LineRangeMapping) string {
	parts := make([]string, len(changes))
	for i, c := range changes {
		parts[i] = fmt.Sprintf("[%d,%d)->[%d,%d)", c.Original.StartLine, c.Original.EndLine, c.Modified.StartLine, c.Modified.EndLine)
	}
	return strings.Join(parts, " ")
}

// fields splits a text into lines the way the tests write them: "a b c" is
// three lines and "_" is an empty line. The empty string is no line at all.
func fields(s string) []string {
	if s == "" {
		return nil
	}
	out := strings.Split(s, " ")
	for i, l := range out {
		if l == "_" {
			out[i] = ""
		}
	}
	return out
}

// steppingClock returns a clock that moves forward by step at every call.
func steppingClock(step time.Duration) func() time.Time {
	t := time.Unix(1000, 0)
	return func() time.Time {
		now := t
		t = t.Add(step)
		return now
	}
}

// numbers are the elements of a test sequence: equal numbers are equal
// elements.
type numbers []int

func (n numbers) length() int                  { return len(n) }
func (n numbers) element(offset int) int       { return n[offset] }
func (n numbers) boundaryScore(offset int) int { return 1 }
func (n numbers) stronglyEqual(a, b int) bool  { return n[a] == n[b] }

// offsetIn returns the byte offset of a position in the lines joined by "\n";
// ok is false when the position is not inside the lines (a column must be on
// a character boundary).
func offsetIn(lines []string, p Position) (int, bool) {
	if p.Line < 1 || p.Line > len(lines) {
		return 0, false
	}
	line := lines[p.Line-1]
	if p.Column < 1 || p.Column > len(line)+1 {
		return 0, false
	}
	if p.Column <= len(line) && !utf8.RuneStart(line[p.Column-1]) {
		return 0, false
	}
	off := 0
	for _, l := range lines[:p.Line-1] {
		off += len(l) + 1
	}
	return off + p.Column - 1, true
}

// applyInner replaces the original range of every character change by the text
// of its modified range and returns the resulting text.
func applyInner(original, modified []string, changes []LineRangeMapping) string {
	text := strings.Join(original, "\n")
	target := strings.Join(modified, "\n")
	var out strings.Builder
	last := 0
	for _, c := range changes {
		for _, in := range c.Inner {
			o1, _ := offsetIn(original, in.Original.Start)
			o2, _ := offsetIn(original, in.Original.End)
			m1, _ := offsetIn(modified, in.Modified.Start)
			m2, _ := offsetIn(modified, in.Modified.End)
			out.WriteString(text[last:o1])
			out.WriteString(target[m1:m2])
			last = o2
		}
	}
	out.WriteString(text[last:])
	return out.String()
}

// checkInner checks the character changes of the blocks against the texts:
// every block has some, every position is inside its text on a character
// boundary, the ranges are not reversed, sorted and do not overlap, and each
// lies in its block (VS Code allows a range to start at the end of the line
// before the block and to end at the start of the line after it).
func checkInner(t *testing.T, changes []LineRangeMapping, original, modified []string) {
	t.Helper()
	within := func(r Range, b LineRange, lines []string) bool {
		startOK := r.Start.Line >= b.StartLine ||
			(r.Start.Line == b.StartLine-1 && r.Start.Line >= 1 && r.Start.Column == len(lines[r.Start.Line-1])+1)
		endOK := r.End.Line <= b.EndLine-1 || (r.End.Line == b.EndLine && r.End.Column == 1)
		return startOK && endOK
	}
	for i, c := range changes {
		if len(c.Inner) == 0 {
			t.Errorf("change %d has no character changes", i)
		}
		for j, in := range c.Inner {
			for _, side := range []struct {
				name  string
				r     Range
				lines []string
				block LineRange
			}{
				{"original", in.Original, original, c.Original},
				{"modified", in.Modified, modified, c.Modified},
			} {
				_, ok1 := offsetIn(side.lines, side.r.Start)
				_, ok2 := offsetIn(side.lines, side.r.End)
				if !ok1 || !ok2 {
					t.Errorf("change %d, inner %d: %s range %+v is outside its text", i, j, side.name, side.r)
					continue
				}
				if side.r.End.isBefore(side.r.Start) {
					t.Errorf("change %d, inner %d: %s range %+v is reversed", i, j, side.name, side.r)
				}
				if !within(side.r, side.block, side.lines) {
					t.Errorf("change %d, inner %d: %s range %+v is outside its block %+v", i, j, side.name, side.r, side.block)
				}
			}
			if j > 0 {
				prev := c.Inner[j-1]
				if in.Original.Start.isBefore(prev.Original.End) || in.Modified.Start.isBefore(prev.Modified.End) {
					t.Errorf("change %d: inner %d overlaps inner %d", i, j, j-1)
				}
			}
		}
	}
}

// formatDetailed writes changes with their character changes, as
// "[2,3)->[2,3){2:1-2:2=>2:1-2:2}": the line blocks and then, for each
// character change, line:column of its start and end in the original text and
// in the modified one.
func formatDetailed(changes []LineRangeMapping) string {
	pos := func(p Position) string { return fmt.Sprintf("%d:%d", p.Line, p.Column) }
	parts := make([]string, len(changes))
	for i, c := range changes {
		inner := make([]string, len(c.Inner))
		for j, in := range c.Inner {
			inner[j] = fmt.Sprintf("%s-%s=>%s-%s", pos(in.Original.Start), pos(in.Original.End), pos(in.Modified.Start), pos(in.Modified.End))
		}
		parts[i] = fmt.Sprintf("[%d,%d)->[%d,%d){%s}", c.Original.StartLine, c.Original.EndLine, c.Modified.StartLine, c.Modified.EndLine, strings.Join(inner, " "))
	}
	return strings.Join(parts, " ")
}
