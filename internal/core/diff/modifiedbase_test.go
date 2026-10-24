package diff

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// ranges computes the modified base ranges of three texts written as the
// tests write lines (see fields).
func ranges(t *testing.T, base, in1, in2 string) ([]ModifiedBaseRange, []string, []string, []string) {
	t.Helper()
	b, i1, i2 := normalizeText(fields(base)), normalizeText(fields(in1)), normalizeText(fields(in2))
	d1, err := Compute(context.Background(), b, i1, Options{})
	if err != nil {
		t.Fatal(err)
	}
	d2, err := Compute(context.Background(), b, i2, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rs, err := ModifiedBaseRangesFromDiffs(d1.Changes, d2.Changes, b, i1, i2)
	if err != nil {
		t.Fatal(err)
	}
	return rs, b, i1, i2
}

// describeRanges writes ranges as "base[2,3) in1[2,4) in2[2,3) d1=1 d2=0 eq".
func describeRanges(rs []ModifiedBaseRange) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		s := fmt.Sprintf("base[%d,%d) in1[%d,%d) in2[%d,%d) d1=%d d2=%d", r.Base.StartLine, r.Base.EndLine,
			r.Input1.StartLine, r.Input1.EndLine, r.Input2.StartLine, r.Input2.EndLine, len(r.Input1Diffs), len(r.Input2Diffs))
		if r.IsEqualChange {
			s += " eq"
		}
		if r.IsConflicting() {
			s += " conflicting"
		}
		parts[i] = s
	}
	return strings.Join(parts, "; ")
}

// checkRanges checks the invariants of ranges: the base ranges are sorted and
// do not touch, and each input is the base with its ranges replaced.
func checkRanges(t *testing.T, rs []ModifiedBaseRange, base, in1, in2 []string) {
	t.Helper()
	for i, r := range rs {
		if r.Base.StartLine < 1 || r.Base.EndLine > len(base)+1 || r.Base.StartLine > r.Base.EndLine {
			t.Errorf("range %d: base range %+v outside the base", i, r.Base)
		}
		if i > 0 && rs[i-1].Base.EndLine >= r.Base.StartLine {
			t.Errorf("range %d touches or overlaps the one before: %+v %+v", i, rs[i-1].Base, r.Base)
		}
		for _, d := range r.Input1Diffs {
			if !r.Base.intersectsOrTouches(d.Original) || d.Original.StartLine < r.Base.StartLine || d.Original.EndLine > r.Base.EndLine {
				t.Errorf("range %d: change %+v of input 1 is outside %+v", i, d.Original, r.Base)
			}
		}
		for _, d := range r.Input2Diffs {
			if d.Original.StartLine < r.Base.StartLine || d.Original.EndLine > r.Base.EndLine {
				t.Errorf("range %d: change %+v of input 2 is outside %+v", i, d.Original, r.Base)
			}
		}
		if len(r.Input1Diffs) == 0 && len(r.Input2Diffs) == 0 {
			t.Errorf("range %d has no change", i)
		}
	}
	for k, in := range [][]string{in1, in2} {
		input := Input(k + 1)
		var got []string
		next := 1
		for _, r := range rs {
			got = append(got, lines(base, next, r.Base.StartLine)...)
			got = append(got, r.InputLines(input)...)
			next = r.Base.EndLine
		}
		got = append(got, lines(base, next, len(base)+1)...)
		if !slices.Equal(got, in) {
			t.Errorf("input %d rebuilt from the ranges is %q, want %q", input, got, in)
		}
	}
}

func TestModifiedBaseRanges(t *testing.T) {
	tests := []struct {
		name         string
		base         string
		in1, in2     string
		want         string
		wantAutoText string // the result the model starts with
	}{
		{"nothing changed", "a b c", "a b c", "a b c", "", "a b c"},
		{"only input 1 changed", "a b c", "a X c", "a b c", "base[2,3) in1[2,3) in2[2,3) d1=1 d2=0", "a X c"},
		{"only input 2 changed", "a b c", "a b c", "a b Y", "base[3,4) in1[3,4) in2[3,4) d1=0 d2=1", "a b Y"},
		{"both made the same change", "a b c", "a X c", "a X c", "base[2,3) in1[2,3) in2[2,3) d1=1 d2=1 eq conflicting", "a X c"},
		{"both changed differently", "a b c", "a X c", "a Y c", "base[2,3) in1[2,3) in2[2,3) d1=1 d2=1 conflicting", "a b c"},
		{"far apart changes", "a b c d e", "a X c d e", "a b c Y e", "base[2,3) in1[2,3) in2[2,3) d1=1 d2=0; base[4,5) in1[4,5) in2[4,5) d1=0 d2=1", "a X c Y e"},
		// A change of input 1 that ends where a change of input 2 begins
		// touches it, and they are one range, which is a conflict
		// (MappingAlignment.compute joins with intersectsOrTouches).
		{"adjacent changes touch", "a b c d", "a X c d", "a b Y d", "base[2,4) in1[2,4) in2[2,4) d1=1 d2=1 conflicting", "a b c d"},
		{"one unchanged line between does not touch", "a b c d e", "a X c d e", "a b c Y e", "base[2,3) in1[2,3) in2[2,3) d1=1 d2=0; base[4,5) in1[4,5) in2[4,5) d1=0 d2=1", "a X c Y e"},
		{"insertions at the same place", "a b", "a X b", "a Y b", "base[2,2) in1[2,3) in2[2,3) d1=1 d2=1 conflicting", "a b"},
		{"identical insertions", "a b", "a X b", "a X b", "base[2,2) in1[2,3) in2[2,3) d1=1 d2=1 eq conflicting", "a X b"},
		{"insertions at different places", "a b c d", "a X b c d", "a b c Y d", "base[2,2) in1[2,3) in2[2,2) d1=1 d2=0; base[4,4) in1[5,5) in2[4,5) d1=0 d2=1", "a X b c Y d"},
		{"insertion right after a modification touches", "a b c", "a X c", "a b Y c", "base[2,3) in1[2,3) in2[2,4) d1=1 d2=1 conflicting", "a b c"},
		{"deletion against modification", "a b c", "a c", "a Y c", "base[2,3) in1[2,2) in2[2,3) d1=1 d2=1 conflicting", "a b c"},
		{"same deletion", "a b c", "a c", "a c", "base[2,3) in1[2,2) in2[2,2) d1=1 d2=1 eq conflicting", "a c"},
		{"changes at the start and the end", "a b c", "X b c", "a b Z", "base[1,2) in1[1,2) in2[1,2) d1=1 d2=0; base[3,4) in1[3,4) in2[3,4) d1=0 d2=1", "X b Z"},
		{"partial overlap", "a b c d e", "a X Y Z e", "a b c W V", "base[2,6) in1[2,6) in2[2,6) d1=1 d2=1 conflicting", "a b c d e"},
		{"input 1 inside a larger change of input 2", "a b c d e f", "a b X d e f", "a P Q R S f", "base[2,6) in1[2,6) in2[2,6) d1=1 d2=1 conflicting", "a b c d e f"},
		{"empty base", "", "x", "y", "base[1,2) in1[1,2) in2[1,2) d1=1 d2=1 conflicting", ""},
		{"empty inputs", "a b", "", "a b", "base[1,3) in1[1,2) in2[1,3) d1=1 d2=0", ""},
		{"both inputs empty", "a b", "", "", "base[1,3) in1[1,2) in2[1,2) d1=1 d2=1 eq conflicting", ""},
		{"empty everything", "", "", "", "", ""},
		// The example of git's documentation (git-merge, "How conflicts are
		// presented"): both changed line 3; line 5 only ours, line 7 only theirs.
		{"git documentation example", "1 2 3 4 5 6 7 8 9", "1 2 three-theirs 4 5 6 seven 8 9", "1 2 three-ours 4 five 6 7 8 9",
			"base[3,4) in1[3,4) in2[3,4) d1=1 d2=1 conflicting; base[5,6) in1[5,6) in2[5,6) d1=0 d2=1; base[7,8) in1[7,8) in2[7,8) d1=1 d2=0",
			"1 2 3 4 five 6 seven 8 9"},
		{"several changes in one range", "a b c d e f g", "a X c Y e f g", "a b c d E F g", "base[2,3) in1[2,3) in2[2,3) d1=1 d2=0; base[4,7) in1[4,7) in2[4,7) d1=1 d2=1 conflicting", "a X c d e f g"},
		{"input 2 adds lines after a change of input 1", "a b c d", "a X c d", "a b c d Y Z", "base[2,3) in1[2,3) in2[2,3) d1=1 d2=0; base[5,5) in1[5,5) in2[5,7) d1=0 d2=1", "a X c d Y Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs, base, in1, in2 := ranges(t, tt.base, tt.in1, tt.in2)
			// The lines of the texts as the model sees them: a text with no
			// line is one empty line.
			if got := describeRanges(rs); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
			checkRanges(t, rs, base, in1, in2)

			m, err := NewMergeModel(context.Background(), MergeInputs{Base: base, Input1: in1, Input2: in2}, MergeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(m.Result(), " "); got != strings.Join(normalizeText(fields(tt.wantAutoText)), " ") {
				t.Errorf("initial result %q, want %q", got, tt.wantAutoText)
			}
		})
	}
}

func TestModifiedBaseRangeEqualChangeNeedsEqualDiffs(t *testing.T) {
	// Both inputs insert the same lines at the same place but input 2 also
	// changes a line next to them: not an equal change.
	rs, _, _, _ := ranges(t, "a b c", "a X b c", "a X Y c")
	if len(rs) != 1 || rs[0].IsEqualChange {
		t.Errorf("got %s", describeRanges(rs))
	}
}

func TestModifiedBaseRangeAccessors(t *testing.T) {
	rs, _, _, _ := ranges(t, "a b c d e", "a X Y Z e", "a b c W V")
	r := rs[0]
	if got := r.InputRange(Input1); got != r.Input1 {
		t.Errorf("InputRange(1) = %+v", got)
	}
	if got := r.InputRange(Input2); got != r.Input2 {
		t.Errorf("InputRange(2) = %+v", got)
	}
	if got := r.InputLines(Input1); !slices.Equal(got, fields("X Y Z e")) {
		t.Errorf("InputLines(1) = %q", got)
	}
	if got := r.InputLines(Input2); !slices.Equal(got, fields("b c W V")) {
		t.Errorf("InputLines(2) = %q", got)
	}
	if got := r.BaseLines(); !slices.Equal(got, fields("b c d e")) {
		t.Errorf("BaseLines = %q", got)
	}
	c1, ok := r.InputCombinedDiff(Input1)
	if !ok || c1.Original != (LineRange{StartLine: 2, EndLine: 5}) || c1.Modified != (LineRange{StartLine: 2, EndLine: 5}) {
		t.Errorf("InputCombinedDiff(1) = %+v, %v", c1, ok)
	}
	one, _, _, _ := ranges(t, "a b c", "a X c", "a b c")
	if _, ok := one[0].InputCombinedDiff(Input2); ok {
		t.Error("InputCombinedDiff of an input without changes should report false")
	}
}

func TestTextForState(t *testing.T) {
	both, _, _, _ := ranges(t, "a b c", "a X c", "a Y c")
	only1, _, _, _ := ranges(t, "a b c", "a X c", "a b c")
	tests := []struct {
		name     string
		r        ModifiedBaseRange
		state    RangeState
		want     string
		wantKind RangeState
	}{
		{"base", both[0], RangeState{}, "b", RangeState{}},
		{"input 1", both[0], RangeState{Kind: StateInput1}, "X", RangeState{Kind: StateInput1}},
		{"input 2", both[0], RangeState{Kind: StateInput2}, "Y", RangeState{Kind: StateInput2}},
		{"both, input 1 first", both[0], BothState(Input1), "X Y", BothState(Input1)},
		{"both, input 2 first", both[0], BothState(Input2), "Y X", BothState(Input2)},
		{"input without changes is base", only1[0], RangeState{Kind: StateInput2}, "b", RangeState{}},
		{"both with one changing input is that input", only1[0], BothState(Input2), "X", RangeState{Kind: StateInput1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, eff := tt.r.TextForState(tt.state)
			if !slices.Equal(text, fields(tt.want)) || eff != tt.wantKind {
				t.Errorf("got %q, %v; want %q, %v", text, eff, tt.want, tt.wantKind)
			}
		})
	}
}

func TestRangeStateTransitions(t *testing.T) {
	base := RangeState{}
	in1 := RangeState{Kind: StateInput1}
	in2 := RangeState{Kind: StateInput2}
	un := RangeState{Kind: StateUnrecognized}
	tests := []struct {
		name  string
		start RangeState
		in    Input
		value bool
		want  RangeState
	}{
		{"base add 1", base, Input1, true, in1},
		{"base add 2", base, Input2, true, in2},
		{"base remove", base, Input1, false, base},
		{"in1 remove 1", in1, Input1, false, base},
		{"in1 keep 1", in1, Input1, true, in1},
		{"in1 add 2", in1, Input2, true, BothState(Input1)},
		{"in1 remove 2 (already out)", in1, Input2, false, in1},
		{"in2 add 1", in2, Input1, true, BothState(Input2)},
		{"in2 remove 2", in2, Input2, false, base},
		{"in2 remove 1 (already out)", in2, Input1, false, in2},
		{"both remove 1", BothState(Input2), Input1, false, in2},
		{"both remove 2", BothState(Input2), Input2, false, in1},
		{"both add", BothState(Input2), Input1, true, BothState(Input2)},
		{"unrecognized add 2", un, Input2, true, in2},
		{"unrecognized remove", un, Input2, false, un},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.start.WithInputValue(tt.in, tt.value); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	toggles := []struct {
		start RangeState
		in    Input
		want  RangeState
	}{
		{base, Input1, in1}, {in1, Input1, base}, {in1, Input2, BothState(Input1)}, {BothState(Input1), Input1, in2}, {un, Input1, in1},
	}
	for _, tt := range toggles {
		if got := tt.start.Toggle(tt.in); got != tt.want {
			t.Errorf("%v.Toggle(%d) = %v, want %v", tt.start, tt.in, got, tt.want)
		}
	}

	if got := in1.Swap(); got != in2 {
		t.Errorf("Swap of input 1 = %v", got)
	}
	if got := BothState(Input1).Swap(); got != BothState(Input2) {
		t.Errorf("Swap of both = %v", got)
	}
	if got := base.Swap(); got != base {
		t.Errorf("Swap of base = %v", got)
	}

	roles := []struct {
		s      RangeState
		in     Input
		want   InputState
		string string
	}{
		{base, Input1, InputExcluded, "base"},
		{in1, Input1, InputFirst, "input1"},
		{in1, Input2, InputExcluded, "input1"},
		{BothState(Input2), Input2, InputFirst, "both(input2 first)"},
		{BothState(Input2), Input1, InputSecond, "both(input2 first)"},
		{un, Input1, InputExcluded, "unrecognized"},
	}
	for _, tt := range roles {
		if got := tt.s.InputState(tt.in); got != tt.want {
			t.Errorf("%v.InputState(%d) = %v, want %v", tt.s, tt.in, got, tt.want)
		}
		if tt.s.String() != tt.string {
			t.Errorf("String() = %q, want %q", tt.s.String(), tt.string)
		}
	}
	if StateKind(99).String() != "invalid" {
		t.Error("an unknown kind should print as invalid")
	}
}

func TestNewModifiedBaseRangeWithoutChange(t *testing.T) {
	_, err := newModifiedBaseRange(LineRange{StartLine: 1, EndLine: 2}, LineRange{}, nil, LineRange{}, nil, nil, nil, nil)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("newModifiedBaseRange.MergeRangeWithoutChange") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
