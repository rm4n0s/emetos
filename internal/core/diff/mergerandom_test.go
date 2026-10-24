package diff

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"testing"
)

// areaEdit replaces the base lines [start, end) (0-based, end exclusive) by
// lines.
type areaEdit struct {
	start, end int
	lines      []string
}

// area is a place of the base that one or both inputs change.
type area struct {
	lo, hi int // the base lines [lo, hi) the region covers (0-based)
	e1, e2 *areaEdit
}

// conflict reports that both inputs change the area, differently.
func (a area) conflict() bool {
	return a.e1 != nil && a.e2 != nil && (a.e1.start != a.e2.start || a.e1.end != a.e2.end || !slices.Equal(a.e1.lines, a.e2.lines))
}

// version returns the lines of the area in an input that made the edit (the
// base lines when it made none).
func (a area) version(base []string, e *areaEdit) []string {
	if e == nil {
		return slices.Clone(base[a.lo:a.hi])
	}
	out := slices.Clone(base[a.lo:e.start])
	out = append(out, e.lines...)
	return append(out, base[e.end:a.hi]...)
}

// randomMerge makes a base, the areas the inputs change, and the inputs.
func randomMerge(rng *rand.Rand, withConflicts bool) (base []string, areas []area, in1, in2 []string) {
	n := 8 + rng.Intn(23)
	for i := 0; i < n; i++ {
		base = append(base, fmt.Sprintf("base line number %d", i))
	}
	fresh := 0
	newLines := func(tag string, m int) []string {
		out := make([]string, m)
		for i := range out {
			fresh++
			out[i] = fmt.Sprintf("%s new line %d", tag, fresh)
		}
		return out
	}
	p := rng.Intn(3)
	for p < n {
		var a area
		kinds := 3
		if withConflicts {
			kinds = 5
		}
		k := rng.Intn(kinds)
		switch k {
		case 0, 1: // only one input
			count, m := rng.Intn(4), rng.Intn(4)
			if count == 0 && m == 0 {
				m = 1
			}
			count = min(count, n-p)
			e := &areaEdit{start: p, end: p + count, lines: newLines(fmt.Sprintf("in%d", k+1), m)}
			a = area{lo: e.start, hi: e.end, e1: e}
			if k == 1 {
				a.e1, a.e2 = nil, e
			}
		case 2: // the same change in both
			count, m := rng.Intn(4), rng.Intn(4)
			if count == 0 && m == 0 {
				m = 1
			}
			count = min(count, n-p)
			lines := newLines("same", m)
			a = area{lo: p, hi: p + count, e1: &areaEdit{p, p + count, lines}, e2: &areaEdit{p, p + count, slices.Clone(lines)}}
		default: // a conflict
			c1 := min(rng.Intn(4), n-p)
			m1 := rng.Intn(4)
			if c1 == 0 && m1 == 0 {
				m1 = 1
			}
			b := 0
			if c1 > 0 {
				b = rng.Intn(c1 + 1)
			}
			c2 := min(rng.Intn(4), n-p-b)
			m2 := rng.Intn(4)
			if c2 == 0 && m2 == 0 {
				m2 = 1
			}
			e1 := &areaEdit{p, p + c1, newLines("in1", m1)}
			e2 := &areaEdit{p + b, p + b + c2, newLines("in2", m2)}
			a = area{lo: p, hi: max(e1.end, e2.end), e1: e1, e2: e2}
		}
		areas = append(areas, a)
		p = a.hi + 1 + rng.Intn(3) // at least one unchanged line between areas
	}
	build := func(pick func(a area) *areaEdit) []string {
		var out []string
		next := 0
		for _, a := range areas {
			out = append(out, base[next:a.lo]...)
			out = append(out, a.version(base, pick(a))...)
			next = a.hi
		}
		return append(out, base[next:]...)
	}
	in1 = build(func(a area) *areaEdit { return a.e1 })
	in2 = build(func(a area) *areaEdit { return a.e2 })
	return base, areas, in1, in2
}

// expectedResult returns the result of a model with the accepted choice for
// the conflicts (none: the base lines) and the other areas merged.
func expectedResult(base []string, areas []area, accept Choice) []string {
	var out []string
	next := 0
	for _, a := range areas {
		out = append(out, base[next:a.lo]...)
		switch {
		case a.conflict():
			switch accept {
			case AcceptInput1:
				out = append(out, a.version(base, a.e1)...)
			case AcceptInput2:
				out = append(out, a.version(base, a.e2)...)
			case AcceptBoth:
				out = append(out, a.version(base, a.e2)...)
				out = append(out, a.version(base, a.e1)...)
			default:
				out = append(out, base[a.lo:a.hi]...)
			}
		case a.e1 != nil:
			out = append(out, a.version(base, a.e1)...)
		default:
			out = append(out, a.version(base, a.e2)...)
		}
		next = a.hi
	}
	return append(out, base[next:]...)
}

func countConflicts(areas []area) int {
	n := 0
	for _, a := range areas {
		if a.conflict() {
			n++
		}
	}
	return n
}

func TestMergeRandomNonConflicting(t *testing.T) {
	for seed := int64(0); seed < 300; seed++ {
		rng := rand.New(rand.NewSource(seed))
		base, areas, in1, in2 := randomMerge(rng, false)
		m, err := NewMergeModel(context.Background(), MergeInputs{Base: base, Input1: in1, Input2: in2}, MergeOptions{})
		if err != nil {
			t.Fatal(err)
		}
		checkModel(t, m, true)
		want := expectedResult(base, areas, AcceptBase)
		if !slices.Equal(m.Result(), want) {
			t.Fatalf("seed %d: result\n%q\nwant\n%q", seed, m.Result(), want)
		}
		if m.ConflictCount() != 0 || m.UnhandledCount() != 0 {
			t.Fatalf("seed %d: %d conflicts, %d unhandled", seed, m.ConflictCount(), m.UnhandledCount())
		}
		if len(m.Regions()) != len(areas) {
			t.Fatalf("seed %d: %d regions, want %d", seed, len(m.Regions()), len(areas))
		}
		rs := make([]ModifiedBaseRange, 0, len(areas))
		for _, g := range m.Regions() {
			rs = append(rs, g.Range)
		}
		checkRanges(t, rs, base, in1, in2)
	}
}

func TestMergeRandomConflicts(t *testing.T) {
	for seed := int64(1000); seed < 1300; seed++ {
		rng := rand.New(rand.NewSource(seed))
		base, areas, in1, in2 := randomMerge(rng, true)
		inputs := MergeInputs{Base: base, Input1: in1, Input2: in2}
		m, err := NewMergeModel(context.Background(), inputs, MergeOptions{})
		if err != nil {
			t.Fatal(err)
		}
		checkModel(t, m, true)
		if want := expectedResult(base, areas, AcceptBase); !slices.Equal(m.Result(), want) {
			t.Fatalf("seed %d: initial result\n%q\nwant\n%q", seed, m.Result(), want)
		}
		if got, want := m.ConflictCount(), countConflicts(areas); got != want || m.UnhandledCount() != want {
			t.Fatalf("seed %d: %d conflicts, %d unhandled, want %d", seed, got, m.UnhandledCount(), want)
		}
		rs := make([]ModifiedBaseRange, 0, len(areas))
		for _, g := range m.Regions() {
			rs = append(rs, g.Range)
		}
		checkRanges(t, rs, base, in1, in2)

		// Every choice for all conflicts gives the text built independently.
		for _, c := range []Choice{AcceptBase, AcceptInput1, AcceptInput2, AcceptBoth} {
			m2, err := NewMergeModel(context.Background(), inputs, MergeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m2.AcceptAll(c); err != nil {
				t.Fatal(err)
			}
			checkModel(t, m2, true)
			if want := expectedResult(base, areas, c); !slices.Equal(m2.Result(), want) {
				t.Fatalf("seed %d: AcceptAll(%d)\n%q\nwant\n%q", seed, c, m2.Result(), want)
			}
			if m2.UnhandledCount() != 0 || m2.HandledCount() != m2.ConflictCount() {
				t.Fatalf("seed %d: %d unhandled after AcceptAll", seed, m2.UnhandledCount())
			}
		}
	}
}

// TestMergeRandomOperations does random accepts, resets, toggles and manual
// edits and checks the invariants after each.
func TestMergeRandomOperations(t *testing.T) {
	choices := []Choice{AcceptBase, AcceptInput1, AcceptInput2, AcceptBoth}
	for seed := int64(5000); seed < 5300; seed++ {
		rng := rand.New(rand.NewSource(seed))
		base, _, in1, in2 := randomMerge(rng, true)
		m, err := NewMergeModel(context.Background(), MergeInputs{Base: base, Input1: in1, Input2: in2}, MergeOptions{})
		if err != nil {
			t.Fatal(err)
		}
		pure := true
		for op := 0; op < 12; op++ {
			n := m.ConflictCount()
			switch k := rng.Intn(6); {
			case k <= 2 && n > 0:
				i, c := rng.Intn(n), choices[rng.Intn(len(choices))]
				if _, err := m.Accept(i, c); err != nil {
					t.Fatal(err)
				}
			case k == 3 && n > 0:
				i := rng.Intn(n)
				if _, err := m.Reset(i); err != nil {
					t.Fatal(err)
				}
			case k == 4:
				if _, err := m.AcceptAll(choices[rng.Intn(len(choices))]); err != nil {
					t.Fatal(err)
				}
			default:
				// A manual edit of a random range of the result.
				pure = false
				size := len(m.Result())
				s := 1 + rng.Intn(size+1)
				e := s + rng.Intn(min(3, size+1-s)+1)
				var text []string
				for j := rng.Intn(3); j > 0; j-- {
					text = append(text, fmt.Sprintf("typed %d", rng.Intn(1000)))
				}
				if err := m.ApplyResultEdit(s, e, text); err != nil {
					t.Fatalf("seed %d: ApplyResultEdit(%d, %d): %v", seed, s, e, err)
				}
			}
			if k := m.HandledCount() + m.UnhandledCount(); k != m.ConflictCount() {
				t.Fatalf("seed %d: counts %d != %d", seed, k, m.ConflictCount())
			}
			checkModel(t, m, pure)
			// The regions stay in order, and Conflicts is Regions without the non-conflicts.
			var inOrder []Region
			for _, g := range m.Regions() {
				if g.Conflict {
					inOrder = append(inOrder, g)
				}
			}
			cs := m.Conflicts()
			if len(cs) != len(inOrder) {
				t.Fatalf("seed %d: %d conflicts, %d regions that are conflicts", seed, len(cs), len(inOrder))
			}
			for j := range cs {
				if cs[j].Index != inOrder[j].Index || cs[j].ConflictIndex != j {
					t.Fatalf("seed %d: conflict %d is %+v", seed, j, cs[j].ConflictIndex)
				}
			}
		}
	}
}
