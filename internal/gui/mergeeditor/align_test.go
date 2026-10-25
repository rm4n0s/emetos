package mergeeditor

import (
	"context"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/diff"
)

func lines(s string) []string { return strings.Split(s, " ") }

// ranges are the modified base ranges of a merge of three texts written as words separated by spaces.
func ranges(t *testing.T, base, in1, in2 string) []diff.ModifiedBaseRange {
	t.Helper()
	m, err := diff.NewMergeModel(context.Background(), diff.MergeInputsFromFiles(lines(base), lines(in1), lines(in2)), diff.MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out []diff.ModifiedBaseRange
	for _, r := range m.Regions() {
		out = append(out, r.Range)
	}
	return out
}

func TestInputSpacers(t *testing.T) {
	for _, tc := range []struct {
		name         string
		base         string
		in1, in2     string
		want1, want2 []Spacer
	}{
		{"nothing changed", "a b c", "a b c", "a b c", nil, nil},
		// the lines x and y have nothing in the other input: it gets two empty rows before c
		{"one input adds lines", "a b c", "a b x y c", "a b c", nil, []Spacer{{Before: 3, Rows: 2}}},
		{"the other input adds lines", "a b c", "a b c", "a b x y c", []Spacer{{Before: 3, Rows: 2}}, nil},
		// b1 and b2 against B: B lines up with b2 (the end of the range), with one empty row above it
		{"both change a line, one with more lines", "a b c", "a b1 b2 c", "a B c", nil, []Spacer{{Before: 2, Rows: 1}}},
		{"a line removed on one side", "a b c", "a c", "a b c", []Spacer{{Before: 2, Rows: 1}}, nil},
		// c and d are on the same rows in both (rows 3 and 4), and so is e (row 7): input 2 gets a row above b to line it up
		// with y, and input 1 two rows before e, which z and w are next to
		{"two separate changes", "a b c d e", "a x y c d e", "a b c d z w e", []Spacer{{Before: 6, Rows: 2}}, []Spacer{{Before: 2, Rows: 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got1, got2 := InputSpacers(ranges(t, tc.base, tc.in1, tc.in2))
			if !slices.Equal(got1, tc.want1) || !slices.Equal(got2, tc.want2) {
				t.Errorf("spacers %v %v, want %v %v", got1, got2, tc.want1, tc.want2)
			}
		})
	}
}

// rowOf is the row a line (counted from 1) of an input is at once the spacers are in.
func rowOf(line int, spacers []Spacer) int {
	row := line - 1
	for _, s := range spacers {
		if s.Before <= line {
			row += s.Rows
		}
	}
	return row
}

// TestInputSpacersLineUpTheEnds checks, on random merges, that the line after every range is on the same row in both
// inputs, and that so is every line outside the ranges: what the spacers are for.
func TestInputSpacersLineUpTheEnds(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	words := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	mutate := func(base []string) []string {
		var out []string
		for _, w := range base {
			switch rnd.Intn(8) {
			case 0: // removed
			case 1:
				out = append(out, w, words[rnd.Intn(len(words))]+"+")
			case 2:
				out = append(out, w+"~")
			default:
				out = append(out, w)
			}
		}
		return out
	}
	for i := 0; i < 200; i++ {
		var base []string
		for n := 3 + rnd.Intn(10); n > 0; n-- {
			base = append(base, words[rnd.Intn(len(words))]+string(rune('0'+rnd.Intn(10))))
		}
		in1, in2 := mutate(base), mutate(base)
		m, err := diff.NewMergeModel(context.Background(), diff.MergeInputsFromFiles(base, in1, in2), diff.MergeOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var rs []diff.ModifiedBaseRange
		for _, r := range m.Regions() {
			rs = append(rs, r.Range)
		}
		s1, s2 := InputSpacers(rs)
		for _, r := range rs {
			if a, b := rowOf(r.Input1.EndLine, s1), rowOf(r.Input2.EndLine, s2); a != b {
				t.Fatalf("case %d: the end of a range is at row %d in input 1 and %d in input 2\nbase %q\nin1 %q\nin2 %q\nspacers %v %v", i, a, b, base, in1, in2, s1, s2)
			}
		}
		// lines before the first range
		if len(rs) > 0 {
			for line := 1; line < min(rs[0].Input1.StartLine, rs[0].Input2.StartLine); line++ {
				if rowOf(line, s1) != rowOf(line, s2) {
					t.Fatalf("case %d: line %d is not aligned", i, line)
				}
			}
		}
	}
}

func TestAlignmentsKeepTheEnds(t *testing.T) {
	rs := ranges(t, "a b c", "a b1 b2 c", "a B c")
	if len(rs) != 1 {
		t.Fatalf("%d ranges", len(rs))
	}
	got := Alignments(rs[0])
	first, final := got[0], got[len(got)-1]
	if first != (LineAlignment{1, 1, 1}) {
		t.Errorf("first alignment %v", first)
	}
	if final != (LineAlignment{4, 3, 3}) {
		t.Errorf("final alignment %v", final)
	}
}

func TestAlignmentsInsideALine(t *testing.T) {
	// both inputs change the middle of a line: the text before the change lines the two up at the line, and the text after
	// it too
	base := []string{"head", "alpha beta gamma", "tail"}
	in1 := []string{"head", "alpha BETA gamma", "x", "tail"}
	in2 := []string{"head", "alpha beta GAMMA", "tail"}
	m, err := diff.NewMergeModel(context.Background(), diff.MergeInputsFromFiles(base, in1, in2), diff.MergeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rs := m.Regions()
	if len(rs) != 1 {
		t.Fatalf("%d regions", len(rs))
	}
	for _, a := range Alignments(rs[0].Range) {
		if a[0] != undef && a[2] != undef && a[0] > a[2]+1 {
			t.Errorf("alignment %v puts input 1 further down than input 2 can be", a)
		}
	}
	s1, s2 := InputSpacers([]diff.ModifiedBaseRange{rs[0].Range})
	if rowOf(rs[0].Range.Input1.EndLine, s1) != rowOf(rs[0].Range.Input2.EndLine, s2) {
		t.Errorf("the ends are not aligned: %v %v", s1, s2)
	}
}
