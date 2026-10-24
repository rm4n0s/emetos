package diff

import (
	"context"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

func TestComputeLines(t *testing.T) {
	tests := []struct {
		name     string
		original []string
		modified []string
		ignoreWS bool
		want     string
	}{
		{"both empty", nil, nil, false, ""},
		{"both one empty line", fields("_"), fields("_"), false, ""},
		{"same single line", fields("a"), fields("a"), false, ""},
		{"identical", fields("a b c"), fields("a b c"), false, ""},
		{"empty to text", nil, fields("a b"), false, "[1,1)->[1,3)"},
		{"text to empty", fields("a b"), nil, false, "[1,3)->[1,1)"},
		{"empty line to text", fields("_"), fields("a b"), false, "[1,2)->[1,3)"},
		{"text to empty line", fields("a b"), fields("_"), false, "[1,3)->[1,2)"},
		{"one line changed", fields("a b c"), fields("a x c"), false, "[2,3)->[2,3)"},
		{"first line changed", fields("a b c"), fields("x b c"), false, "[1,2)->[1,2)"},
		{"last line changed", fields("a b c"), fields("a b x"), false, "[3,4)->[3,4)"},
		{"inserted block", fields("a b c"), fields("a x y b c"), false, "[2,2)->[2,4)"},
		{"deleted block", fields("a x y b c"), fields("a b c"), false, "[2,4)->[2,2)"},
		{"inserted at start", fields("a b"), fields("x a b"), false, "[1,1)->[1,2)"},
		{"inserted at end", fields("a b"), fields("a b x"), false, "[3,3)->[3,4)"},
		{"two separate changes", fields("a b c d e"), fields("a x c y e"), false, "[2,3)->[2,3) [4,5)->[4,5)"},
		{"everything replaced", fields("a b c"), fields("x y z"), false, "[1,4)->[1,4)"},
		// There is no move detection: a moved block is a deletion and an insertion.
		{"moved block", fields("a b c d e"), fields("c d a b e"), false, "[1,1)->[1,3) [3,5)->[5,5)"},
		{"trailing line break added", fields("a"), fields("a _"), false, "[2,2)->[2,3)"},
		{"trailing line break removed", fields("a _"), fields("a"), false, "[2,3)->[2,2)"},
		{"empty line removed", fields("a _ b"), fields("a b"), false, "[2,3)->[2,2)"},
		{"repeated line added", fields("a a a"), fields("a a a a"), false, "[1,1)->[1,2)"},
		{"repeated line removed", fields("a a a a"), fields("a a a"), false, "[1,2)->[1,1)"},
		{"white space change reported", []string{"a", "b  ", "c"}, []string{"a", "b", "c"}, false, "[2,3)->[2,3)"},
		{"white space change ignored", []string{"a", "b  ", "c"}, []string{"a", "b", "c"}, true, ""},
		{"indentation change reported", []string{"a", "b"}, []string{"a", "\tb"}, false, "[2,3)->[2,3)"},
		{"indentation change ignored", []string{"a", "b"}, []string{"a", "\tb"}, true, ""},
		{"no-break space is trimmed", []string{"a ", "b"}, []string{"a", "b"}, true, ""},
		{"white space change next to a change is one change", []string{"a ", "x"}, []string{"a", "y"}, false, "[1,3)->[1,3)"},
		{"white space change apart from a change", []string{"a ", "m", "x"}, []string{"a", "m", "y"}, false, "[1,2)->[1,2) [3,4)->[3,4)"},
		{"change next to ignored white space", []string{"a ", "x"}, []string{"a", "y"}, true, "[2,3)->[2,3)"},
		{"interior white space is a change", []string{"a  b"}, []string{"a b"}, true, "[1,2)->[1,2)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComputeLines(context.Background(), tc.original, tc.modified, Options{IgnoreTrimWhitespace: tc.ignoreWS})
			if err != nil {
				t.Fatalf("ComputeLines: %v", err)
			}
			if f := format(got.Changes); f != tc.want {
				t.Errorf("changes = %q, want %q", f, tc.want)
			}
			if got.HitTimeout {
				t.Error("HitTimeout is set")
			}
			if err := Validate(got.Changes, len(tc.original), len(tc.modified)); err != nil {
				t.Errorf("Validate: %v", err)
			}
			if !tc.ignoreWS {
				if applied := Apply(tc.original, tc.modified, got.Changes); !equalLines(applied, tc.modified) {
					t.Errorf("Apply = %q, want %q", applied, tc.modified)
				}
			}
		})
	}
}

// equalLines compares two texts, an empty text being the same as a nil one.
func equalLines(a, b []string) bool {
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

func TestComputeLinesShortcutInner(t *testing.T) {
	// VS Code returns the whole texts as the one character change when a text
	// is one empty line.
	tests := []struct {
		name     string
		original []string
		modified []string
		want     RangeMapping
	}{
		{"empty line to text", []string{""}, []string{"ab", "cde"}, RangeMapping{
			Original: Range{Position{1, 1}, Position{1, 1}},
			Modified: Range{Position{1, 1}, Position{2, 4}},
		}},
		{"text to empty line", []string{"ab", "cde"}, []string{""}, RangeMapping{
			Original: Range{Position{1, 1}, Position{2, 4}},
			Modified: Range{Position{1, 1}, Position{1, 1}},
		}},
		{"multibyte columns count bytes", []string{""}, []string{"é"}, RangeMapping{
			Original: Range{Position{1, 1}, Position{1, 1}},
			Modified: Range{Position{1, 1}, Position{1, 3}},
		}},
		{"no lines", nil, []string{""}, RangeMapping{
			Original: Range{Position{1, 1}, Position{1, 1}},
			Modified: Range{Position{1, 1}, Position{1, 1}},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComputeLines(context.Background(), tc.original, tc.modified, Options{})
			if err != nil {
				t.Fatalf("ComputeLines: %v", err)
			}
			if len(got.Changes) != 1 || len(got.Changes[0].Inner) != 1 || got.Changes[0].Inner[0] != tc.want {
				t.Errorf("changes = %+v, want one change with inner %+v", got.Changes, tc.want)
			}
		})
	}
}

func TestComputeLinesInnerIsEmptyOtherwise(t *testing.T) {
	got, err := ComputeLines(context.Background(), fields("a b c"), fields("a x c"), Options{})
	if err != nil {
		t.Fatalf("ComputeLines: %v", err)
	}
	if len(got.Changes) != 1 || got.Changes[0].Inner != nil {
		t.Errorf("changes = %+v, want one change without character changes", got.Changes)
	}
}

func TestComputeLinesBoundaryShift(t *testing.T) {
	// A block that is inserted next to a copy of itself could be put at either
	// place; the one with the best boundary (least indentation) wins.
	tests := []struct {
		name     string
		original []string
		modified []string
		want     string
	}{
		{
			"function inserted between functions",
			[]string{"f() {", "\ta", "}", "g() {", "\tb", "}"},
			[]string{"f() {", "\ta", "}", "h() {", "\tc", "}", "g() {", "\tb", "}"},
			"[4,4)->[4,7)",
		},
		{
			"function inserted at the end",
			[]string{"f() {", "\ta", "}"},
			[]string{"f() {", "\ta", "}", "g() {", "\tb", "}"},
			"[4,4)->[4,7)",
		},
		{
			"function removed from the middle",
			[]string{"f() {", "\ta", "}", "h() {", "\tc", "}", "g() {", "\tb", "}"},
			[]string{"f() {", "\ta", "}", "g() {", "\tb", "}"},
			"[4,7)->[4,4)",
		},
		{
			"indented block inserted between siblings",
			[]string{"f {", "  a", "  b", "}"},
			[]string{"f {", "  a", "  b", "  a", "  b", "}"},
			"[2,2)->[2,4)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComputeLines(context.Background(), tc.original, tc.modified, Options{})
			if err != nil {
				t.Fatalf("ComputeLines: %v", err)
			}
			if f := format(got.Changes); f != tc.want {
				t.Errorf("changes = %q, want %q", f, tc.want)
			}
		})
	}
}

func TestComputeLinesTimeout(t *testing.T) {
	big := func(n int, with string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = with + string(rune('a'+i%7)) + strings.Repeat("x", i%5)
		}
		return out
	}
	tests := []struct {
		name     string
		original []string
		modified []string
	}{
		{"dynamic programming", fields("a b c d e"), fields("a x c d e f")},
		{"myers", big(1200, "p"), big(1000, "q")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The clock is a second further at every look at it, so the limit of
			// a millisecond is over at the first one.
			got, err := ComputeLines(context.Background(), tc.original, tc.modified,
				Options{MaxComputationTime: time.Millisecond, Now: steppingClock(time.Second)})
			if err != nil {
				t.Fatalf("ComputeLines: %v", err)
			}
			if !got.HitTimeout {
				t.Error("HitTimeout is not set")
			}
			want := LineRangeMapping{
				Original: LineRange{1, len(tc.original) + 1},
				Modified: LineRange{1, len(tc.modified) + 1},
			}
			if len(got.Changes) != 1 || got.Changes[0].Original != want.Original || got.Changes[0].Modified != want.Modified {
				t.Errorf("changes = %s, want one change covering both texts", format(got.Changes))
			}
			if err := Validate(got.Changes, len(tc.original), len(tc.modified)); err != nil {
				t.Errorf("Validate: %v", err)
			}

			// A clock that stands still never reaches the limit.
			frozen := time.Unix(1000, 0)
			got, err = ComputeLines(context.Background(), tc.original, tc.modified,
				Options{MaxComputationTime: time.Millisecond, Now: func() time.Time { return frozen }})
			if err != nil {
				t.Fatalf("ComputeLines: %v", err)
			}
			if got.HitTimeout {
				t.Error("HitTimeout is set with a clock that stands still")
			}
			// Zero is no limit, whatever the clock says.
			got, err = ComputeLines(context.Background(), tc.original, tc.modified,
				Options{Now: steppingClock(time.Hour)})
			if err != nil {
				t.Fatalf("ComputeLines: %v", err)
			}
			if got.HitTimeout {
				t.Error("HitTimeout is set without a limit")
			}
		})
	}
}

func TestComputeLinesRealClockLimit(t *testing.T) {
	// With the real clock a generous limit is not reached by a small diff.
	got, err := ComputeLines(context.Background(), fields("a b c"), fields("a x c"), Options{MaxComputationTime: time.Minute})
	if err != nil {
		t.Fatalf("ComputeLines: %v", err)
	}
	if got.HitTimeout || format(got.Changes) != "[2,3)->[2,3)" {
		t.Errorf("got %+v", got)
	}
}

// cancelAfter is a context whose Err reports cancellation after n calls.
type cancelAfter struct {
	context.Context
	n int
}

func (c *cancelAfter) Err() error {
	c.n--
	if c.n < 0 {
		return context.Canceled
	}
	return nil
}

func TestComputeLinesCancelled(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	bigText := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = string(rune('a' + i%26))
		}
		return out
	}
	tests := []struct {
		name     string
		ctx      context.Context
		original []string
		modified []string
	}{
		{"before the start", cancelled, fields("a b"), fields("a c")},
		{"before the shortcuts", cancelled, nil, nil},
		{"in dynamic programming", &cancelAfter{Context: context.Background(), n: 2}, bigText(800), append(bigText(600), "z")},
		{"in myers", &cancelAfter{Context: context.Background(), n: 1}, bigText(2000), reversed(bigText(2000))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ComputeLines(tc.ctx, tc.original, tc.modified, Options{})
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("ComputeLines") || !appErr.HasRoute("DiffCancelled") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("the error does not wrap context.Canceled: %v", err)
			}
		})
	}
}

func reversed(s []string) []string {
	out := make([]string, len(s))
	for i, l := range s {
		out[len(s)-1-i] = l
	}
	return out
}

func TestComputeLinesOptionsInvalid(t *testing.T) {
	_, err := ComputeLines(context.Background(), fields("a"), fields("b"), Options{MaxComputationTime: -time.Second})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("ComputeLines->computeDiff.DiffTimeoutInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestBuildChangesAlignmentBroken(t *testing.T) {
	// The unchanged lines before a change must be as many in both texts.
	_, err := buildChanges(fields("a b c"), fields("a b c"),
		[]sequenceDiff{{offsetRange{2, 3}, offsetRange{1, 2}}}, false)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("buildChanges.DiffAlignmentBroken") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestComputeLinesAroundTheThreshold(t *testing.T) {
	// 849 + 850 lines are aligned by dynamic programming, 850 + 850 by Myers.
	mk := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "l" + string(rune('a'+i%26)) + string(rune('a'+i/26%26))
		}
		return out
	}
	for _, sizes := range [][2]int{{849, 850}, {850, 850}} {
		orig, mod := mk(sizes[0]), mk(sizes[1])
		mod[100] = "changed"
		got, err := ComputeLines(context.Background(), orig, mod, Options{})
		if err != nil {
			t.Fatalf("ComputeLines: %v", err)
		}
		if err := Validate(got.Changes, len(orig), len(mod)); err != nil {
			t.Errorf("sizes %v: Validate: %v", sizes, err)
		}
		if applied := Apply(orig, mod, got.Changes); !equalLines(applied, mod) {
			t.Errorf("sizes %v: Apply does not give the modified text", sizes)
		}
	}
}

// randomLines returns n lines drawn from the alphabet.
func randomLines(r *rand.Rand, alphabet []string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = alphabet[r.IntN(len(alphabet))]
	}
	return out
}

// mutate returns a copy of lines with some lines changed, inserted or removed.
func mutate(r *rand.Rand, alphabet, lines []string, edits int) []string {
	out := append([]string(nil), lines...)
	for range edits {
		switch r.IntN(3) {
		case 0:
			if len(out) > 0 {
				out[r.IntN(len(out))] = alphabet[r.IntN(len(alphabet))]
			}
		case 1:
			i := r.IntN(len(out) + 1)
			block := randomLines(r, alphabet, 1+r.IntN(4))
			out = append(out[:i], append(block, out[i:]...)...)
		default:
			if len(out) > 0 {
				i := r.IntN(len(out))
				j := min(len(out), i+1+r.IntN(4))
				out = append(out[:i], out[j:]...)
			}
		}
	}
	return out
}

func TestComputeLinesRandomApply(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 1))
	words := []string{"a", "b", "c", "d", "e", "f"}
	// White space variants test the trimming.
	spaced := []string{"a", " a", "a ", "", "\tb", "b", "{", "  }"}
	for i := range 600 {
		alphabet := words[:3+r.IntN(4)]
		if i%3 == 0 {
			alphabet = spaced[:3+r.IntN(6)]
		}
		original := randomLines(r, alphabet, r.IntN(61))
		var modified []string
		if i%2 == 0 {
			modified = randomLines(r, alphabet, r.IntN(61))
		} else {
			modified = mutate(r, alphabet, original, 1+r.IntN(5))
		}
		for _, ignore := range []bool{false, true} {
			got, err := ComputeLines(context.Background(), original, modified, Options{IgnoreTrimWhitespace: ignore})
			if err != nil {
				t.Fatalf("case %d: ComputeLines: %v", i, err)
			}
			checkDiff(t, i, original, modified, got, ignore)
		}
	}
}

func TestComputeLinesBigRandomApply(t *testing.T) {
	// Texts of up to 4000 lines, which cross VS Code's limit for dynamic
	// programming.
	r := rand.New(rand.NewPCG(5, 8))
	alphabet := make([]string, 300)
	for i := range alphabet {
		alphabet[i] = strings.Repeat(" ", i%4) + "line " + string(rune('A'+i%26)) + string(rune('a'+i/26))
	}
	sizes := []int{900, 1700, 2500, 4000}
	for i, n := range sizes {
		original := randomLines(r, alphabet, n)
		modified := mutate(r, alphabet, original, 10+i*20)
		got, err := ComputeLines(context.Background(), original, modified, Options{})
		if err != nil {
			t.Fatalf("size %d: ComputeLines: %v", n, err)
		}
		checkDiff(t, n, original, modified, got, false)
		if len(got.Changes) == 0 {
			t.Errorf("size %d: no changes for texts that differ", n)
		}
	}
	// Mostly different texts of 2000 lines (the worst case for Myers).
	original := randomLines(r, alphabet[:4], 2000)
	modified := randomLines(r, alphabet[:4], 2000)
	got, err := ComputeLines(context.Background(), original, modified, Options{})
	if err != nil {
		t.Fatalf("ComputeLines: %v", err)
	}
	checkDiff(t, 2000, original, modified, got, false)
}

// checkDiff checks that a diff is valid and, when white space is not ignored,
// that it turns the original into the modified text.
func checkDiff(t *testing.T, id int, original, modified []string, got LinesDiff, ignoreWS bool) {
	t.Helper()
	if got.HitTimeout {
		t.Errorf("case %d: HitTimeout is set", id)
	}
	if err := Validate(got.Changes, len(original), len(modified)); err != nil {
		t.Fatalf("case %d: Validate: %v\noriginal %q\nmodified %q\nchanges %s", id, err, original, modified, format(got.Changes))
	}
	applied := Apply(original, modified, got.Changes)
	if len(applied) != len(modified) {
		t.Fatalf("case %d: Apply gives %d lines, want %d", id, len(applied), len(modified))
	}
	for i := range applied {
		if ignoreWS {
			if jsTrim(applied[i]) != jsTrim(modified[i]) {
				t.Fatalf("case %d: line %d is %q, want %q (ignoring white space)", id, i+1, applied[i], modified[i])
			}
		} else if applied[i] != modified[i] {
			t.Fatalf("case %d: line %d is %q, want %q", id, i+1, applied[i], modified[i])
		}
	}
}
