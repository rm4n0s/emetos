package diff

import (
	"context"
	"math/rand/v2"
	"strconv"
	"testing"
)

// lcsLength returns the length of the longest common subsequence of a and b.
func lcsLength(a, b numbers) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else {
				cur[j] = max(prev[j], cur[j-1])
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// checkAlignment checks that the diffs are sorted, and that what is between
// them is equal in both sequences. It returns the number of elements the
// diffs replace.
func checkAlignment(t *testing.T, a, b numbers, diffs []sequenceDiff) int {
	t.Helper()
	cost := 0
	p1, p2 := 0, 0
	for _, d := range diffs {
		if d.seq1.start < p1 || d.seq2.start < p2 || d.seq1.start > d.seq1.end || d.seq2.start > d.seq2.end {
			t.Fatalf("diffs %v are not sorted or not ranges", diffs)
		}
		if d.seq1.start-p1 != d.seq2.start-p2 {
			t.Fatalf("diffs %v: unchanged parts differ in length", diffs)
		}
		for i := 0; i < d.seq1.start-p1; i++ {
			if a[p1+i] != b[p2+i] {
				t.Fatalf("diffs %v: %d and %d are not equal", diffs, p1+i, p2+i)
			}
		}
		if d.seq1.isEmpty() && d.seq2.isEmpty() {
			t.Fatalf("diffs %v: an empty diff", diffs)
		}
		cost += d.seq1.length() + d.seq2.length()
		p1, p2 = d.seq1.end, d.seq2.end
	}
	if len(a)-p1 != len(b)-p2 {
		t.Fatalf("diffs %v: unchanged tails differ in length", diffs)
	}
	for i := 0; i < len(a)-p1; i++ {
		if a[p1+i] != b[p2+i] {
			t.Fatalf("diffs %v: tail elements are not equal", diffs)
		}
	}
	return cost
}

func newBudget() *budget {
	return &budget{ctx: context.Background(), timeout: infiniteTimeout{}}
}

func TestAlgorithms(t *testing.T) {
	tests := []struct {
		name   string
		a, b   numbers
		myers  string
		dp     string
		minLen int // the number of elements that must change
	}{
		{"equal", numbers{1, 2, 3}, numbers{1, 2, 3}, "", "", 0},
		{"first empty", nil, numbers{1, 2}, "[0,0)->[0,2)", "[0,0)->[0,2)", 2},
		{"second empty", numbers{1, 2}, nil, "[0,2)->[0,0)", "[0,2)->[0,0)", 2},
		{"both empty", nil, nil, "[0,0)->[0,0)", "[0,0)->[0,0)", 0},
		{"replace middle", numbers{1, 2, 3}, numbers{1, 9, 3}, "[1,2)->[1,2)", "[1,2)->[1,2)", 2},
		{"insert", numbers{1, 3}, numbers{1, 2, 3}, "[1,1)->[1,2)", "[1,1)->[1,2)", 1},
		{"delete", numbers{1, 2, 3}, numbers{1, 3}, "[1,2)->[1,1)", "[1,2)->[1,1)", 1},
		{"disjoint", numbers{1, 2}, numbers{3, 4}, "[0,2)->[0,2)", "[0,2)->[0,2)", 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, hit, err := myers(newBudget(), tc.a, tc.b)
			if err != nil || hit {
				t.Fatalf("myers: hit %v, err %v", hit, err)
			}
			d, hit, err := dynamicProgramming(newBudget(), tc.a, tc.b, nil)
			if err != nil || hit {
				t.Fatalf("dynamicProgramming: hit %v, err %v", hit, err)
			}
			if got := formatDiffs(m); got != tc.myers {
				t.Errorf("myers = %q, want %q", got, tc.myers)
			}
			if got := formatDiffs(d); got != tc.dp {
				t.Errorf("dynamicProgramming = %q, want %q", got, tc.dp)
			}
		})
	}
}

func formatDiffs(diffs []sequenceDiff) string {
	out := ""
	for i, d := range diffs {
		if i > 0 {
			out += " "
		}
		out += "[" + strconv.Itoa(d.seq1.start) + "," + strconv.Itoa(d.seq1.end) + ")->[" + strconv.Itoa(d.seq2.start) + "," + strconv.Itoa(d.seq2.end) + ")"
	}
	return out
}

func TestMyersAndDynamicProgrammingAgree(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for i := range 500 {
		alphabet := 2 + r.IntN(6)
		a := make(numbers, r.IntN(70))
		for j := range a {
			a[j] = r.IntN(alphabet)
		}
		b := make(numbers, r.IntN(70))
		for j := range b {
			b[j] = r.IntN(alphabet)
		}
		optimal := len(a) + len(b) - 2*lcsLength(a, b)

		m, _, err := myers(newBudget(), a, b)
		if err != nil {
			t.Fatal(err)
		}
		// Myers' algorithm gives a minimal diff, whatever the sequences.
		if cost := checkAlignment(t, a, b, m); cost != optimal {
			t.Fatalf("case %d: myers replaces %d elements, the minimum is %d (%v %v)", i, cost, optimal, a, b)
		}

		d, _, err := dynamicProgramming(newBudget(), a, b, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Dynamic programming prefers runs of equal elements over fewer
		// changes, so it is valid but not always minimal.
		if cost := checkAlignment(t, a, b, d); cost < optimal {
			t.Fatalf("case %d: dynamic programming replaces %d elements, below the minimum %d", i, cost, optimal)
		}
	}
}

func TestMyersBigInputsAreMinimal(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 7))
	a := make(numbers, 1500)
	for i := range a {
		a[i] = r.IntN(40)
	}
	b := append(numbers(nil), a...)
	for range 60 {
		b[r.IntN(len(b))] = r.IntN(40)
	}
	m, _, err := myers(newBudget(), a, b)
	if err != nil {
		t.Fatal(err)
	}
	if cost, optimal := checkAlignment(t, a, b, m), len(a)+len(b)-2*lcsLength(a, b); cost != optimal {
		t.Errorf("myers replaces %d elements, the minimum is %d", cost, optimal)
	}
}

func TestDynamicProgrammingPrefersConsecutiveRuns(t *testing.T) {
	// a b c x versus x a b c: the run a b c is kept, x is moved.
	a, b := numbers{1, 2, 3, 9}, numbers{9, 1, 2, 3}
	d, _, err := dynamicProgramming(newBudget(), a, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := formatDiffs(d), "[0,0)->[0,1) [3,4)->[4,4)"; got != want {
		t.Errorf("diffs = %q, want %q", got, want)
	}
}

func TestDynamicProgrammingEqualityScore(t *testing.T) {
	// 1 2 versus 2 1: one of the two can be kept; the score decides which.
	a, b := numbers{1, 2}, numbers{2, 1}
	for _, tc := range []struct {
		name  string
		score func(o1, o2 int) float64
		want  string
	}{
		{"keep the second", func(o1, o2 int) float64 { return float64(o1) + 1 }, "[0,1)->[0,0) [2,2)->[1,2)"},
		{"keep the first", func(o1, o2 int) float64 { return float64(2 - o1) }, "[0,0)->[0,1) [1,2)->[2,2)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, err := dynamicProgramming(newBudget(), a, b, tc.score)
			if err != nil {
				t.Fatal(err)
			}
			if got := formatDiffs(d); got != tc.want {
				t.Errorf("diffs = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAlgorithmsTimeout(t *testing.T) {
	a, b := numbers{1, 2, 3, 4}, numbers{4, 3, 2, 1}
	expired := func() *budget {
		return &budget{ctx: context.Background(), timeout: newDateTimeout(1, steppingClock(1000))}
	}
	m, hit, err := myers(expired(), a, b)
	if err != nil || !hit || formatDiffs(m) != "[0,4)->[0,4)" {
		t.Errorf("myers = %v, hit %v, err %v", formatDiffs(m), hit, err)
	}
	d, hit, err := dynamicProgramming(expired(), a, b, nil)
	if err != nil || !hit || formatDiffs(d) != "[0,4)->[0,4)" {
		t.Errorf("dynamicProgramming = %v, hit %v, err %v", formatDiffs(d), hit, err)
	}
}

func TestDateTimeoutStaysExpired(t *testing.T) {
	now := steppingClock(10)
	tm := newDateTimeout(25, now) // the clock is read once here (0), then 10, 20, 30...
	got := []bool{tm.isValid(), tm.isValid(), tm.isValid(), tm.isValid()}
	want := []bool{true, true, false, false}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("isValid #%d = %v, want %v", i, got[i], want[i])
		}
	}
	if !(infiniteTimeout{}).isValid() {
		t.Error("infiniteTimeout is not valid")
	}
}

func TestUseDynamicProgramming(t *testing.T) {
	// VS Code: original.length + modified.length < 1700.
	tests := []struct {
		n1, n2 int
		want   bool
	}{
		{0, 0, true},
		{849, 850, true},
		{850, 850, false},
		{1699, 0, true},
		{1700, 0, false},
		{3000, 3000, false},
	}
	for _, tc := range tests {
		if got := useDynamicProgramming(tc.n1, tc.n2); got != tc.want {
			t.Errorf("useDynamicProgramming(%d, %d) = %v, want %v", tc.n1, tc.n2, got, tc.want)
		}
	}
}
