package folding

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/regex"
)

// lines is a Text of lines.
type lines []string

func (l lines) LineCount() int { return len(l) }

func (l lines) LineContent(i int) string { return l[i] }

func ecma(t testing.TB, pattern string, ignoreCase bool) *regex.Regexp {
	t.Helper()
	re, err := regex.Compile(pattern, regex.Options{Syntax: regex.ECMAScript, IgnoreCase: ignoreCase})
	if err != nil {
		t.Fatal(err)
	}
	return re
}

// regionMarkers are the markers of FoldingRanges' tests: `#region` and `#endregion` lines.
func regionMarkers(t testing.TB) *Markers {
	return &Markers{Start: ecma(t, `^#region$`, false), End: ecma(t, `^#endregion$`, false)}
}

// Ported from VS Code: foldingRanges.test.ts ('test max folding regions').
func TestMaxFoldingRegions(t *testing.T) {
	var text lines
	collector := NewRangesCollector(Limit{Max: MaxRegions})
	for i := 0; i < MaxRegions; i++ {
		start := len(text)
		text = append(text, "#region")
		collector.InsertFirst(start, len(text), 0)
		text = append(text, "#endregion")
	}
	regions, err := collector.ToRegions(text, 4)
	if err != nil {
		t.Fatal(err)
	}
	if regions.Len() != MaxRegions {
		t.Errorf("len = %d, want %d", regions.Len(), MaxRegions)
	}
}

// Ported from VS Code: foldingRanges.test.ts ('findRange', with the lines made zero-based).
func TestFindRange(t *testing.T) {
	text := lines{
		"#region", "#endregion", "class A {", "  void foo() {", "    if (true) {", "        return;", "    }", "",
		"    if (true) {", "      return;", "    }", "  }", "}",
	}
	regions, err := ComputeRanges(text, 4, false, regionMarkers(t), Limit{})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 0, 1, 2, 3, 3, 2, 2, 4, 4, 2, 1, -1}
	for i, w := range want {
		if got := regions.FindRange(i); got != w {
			t.Errorf("FindRange(%d) = %d, want %d", i+1, got, w)
		}
	}
}

// Ported from VS Code: foldingRanges.test.ts ('setCollapsed').
func TestSetCollapsed(t *testing.T) {
	const n = 500
	text := make(lines, 0, 2*n)
	for i := 0; i < n; i++ {
		text = append(text, "#region")
	}
	for i := 0; i < n; i++ {
		text = append(text, "#endregion")
	}
	regions, err := ComputeRanges(text, 4, false, regionMarkers(t), Limit{})
	if err != nil {
		t.Fatal(err)
	}
	if regions.Len() != n {
		t.Fatalf("len = %d, want %d", regions.Len(), n)
	}
	for i := 0; i < n; i++ {
		regions.SetCollapsed(i, i%3 == 0)
	}
	for i := 0; i < n; i++ {
		if regions.Collapsed(i) != (i%3 == 0) {
			t.Errorf("region %d collapsed = %v", i, regions.Collapsed(i))
		}
	}
}

// vsRange is a FoldRange as VS Code's tests write it: one-based lines.
func vsRange(from, to int, collapsed bool, source Source, typ string) Range {
	return Range{Start: from, End: to, Collapsed: collapsed, Source: source, Type: typ}
}

// mergeVS runs SanitizeAndMerge on ranges written with VS Code's one-based lines and gives the
// result in one-based lines.
func mergeVS(a, b []Range, maxLine int) []Range {
	shift := func(in []Range, d int) []Range {
		out := make([]Range, len(in))
		for i, r := range in {
			r.Start += d
			r.End += d
			out[i] = r
		}
		return out
	}
	return shift(SanitizeAndMerge(shift(a, -1), shift(b, -1), maxLine-1, nil), 1)
}

func TestSanitizeAndMerge(t *testing.T) {
	tests := []struct {
		name string
		a, b []Range
		want []Range
	}{
		{"sanitizeAndMerge1",
			[]Range{
				vsRange(0, 100, false, FromProvider, ""),   // invalid, should be removed
				vsRange(1, 100, false, FromProvider, "A"),  // valid
				vsRange(1, 100, false, FromProvider, "Z"),  // invalid, duplicate start
				vsRange(10, 10, false, FromProvider, ""),   // invalid, should be removed
				vsRange(20, 80, false, FromProvider, "C1"), // valid inside 'B'
				vsRange(22, 80, true, FromProvider, "D1"),  // valid inside 'C1'
				vsRange(90, 101, false, FromProvider, ""),  // invalid, should be removed
			},
			[]Range{
				vsRange(20, 80, true, FromProvider, ""),   // should merge with C1
				vsRange(18, 80, true, FromProvider, ""),   // invalid, out of order
				vsRange(21, 81, true, FromProvider, "Z"),  // invalid, overlapping
				vsRange(22, 80, true, FromProvider, "D2"), // should merge with D1
			},
			[]Range{
				vsRange(1, 100, false, FromProvider, "A"),
				vsRange(20, 80, true, FromProvider, "C1"),
				vsRange(22, 80, true, FromProvider, "D1"),
			}},
		{"sanitizeAndMerge2",
			[]Range{
				vsRange(1, 100, false, FromProvider, "a1"), // valid
				vsRange(2, 100, false, FromProvider, "a2"), // valid
				vsRange(3, 19, false, FromProvider, "a3"),  // valid
				vsRange(20, 71, false, FromProvider, "a4"), // overlaps b3
				vsRange(21, 29, false, FromProvider, "a5"), // valid
				vsRange(81, 91, false, FromProvider, "a6"), // overlaps b4
			},
			[]Range{
				vsRange(30, 39, true, FromProvider, "b1"), // valid, will be recovered
				vsRange(40, 49, true, UserDefined, "b2"),  // valid
				vsRange(50, 100, true, UserDefined, "b3"), // overlaps a4
				vsRange(80, 90, true, UserDefined, "b4"),  // overlaps a6
				vsRange(92, 100, true, UserDefined, "b5"), // valid
			},
			[]Range{
				vsRange(1, 100, false, FromProvider, "a1"),
				vsRange(2, 100, false, FromProvider, "a2"),
				vsRange(3, 19, false, FromProvider, "a3"),
				vsRange(21, 29, false, FromProvider, "a5"),
				vsRange(30, 39, true, Recovered, "b1"),
				vsRange(40, 49, true, UserDefined, "b2"),
				vsRange(50, 100, true, UserDefined, "b3"),
				vsRange(80, 90, true, UserDefined, "b4"),
				vsRange(92, 100, true, UserDefined, "b5"),
			}},
		{"sanitizeAndMerge3",
			[]Range{
				vsRange(1, 100, false, FromProvider, "a1"), // valid
				vsRange(10, 29, false, FromProvider, "a2"), // matches manual hidden
				vsRange(35, 39, true, Recovered, "a3"),     // valid
			},
			[]Range{
				vsRange(10, 29, true, Recovered, "b1"),    // matches a
				vsRange(20, 28, true, FromProvider, "b2"), // should remain
				vsRange(30, 39, true, Recovered, "b3"),    // should remain
			},
			[]Range{
				vsRange(1, 100, false, FromProvider, "a1"),
				vsRange(10, 29, true, FromProvider, "a2"),
				vsRange(20, 28, true, Recovered, "b2"),
				vsRange(30, 39, true, Recovered, "b3"),
				vsRange(35, 39, true, Recovered, "a3"),
			}},
		{"sanitizeAndMerge4",
			[]Range{vsRange(1, 100, false, FromProvider, "a1")},
			[]Range{
				vsRange(20, 28, true, FromProvider, "b1"), // hidden
				vsRange(30, 38, true, FromProvider, "b2"), // hidden
			},
			[]Range{
				vsRange(1, 100, false, FromProvider, "a1"),
				vsRange(20, 28, true, Recovered, "b1"),
				vsRange(30, 38, true, Recovered, "b2"),
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeVS(tc.a, tc.b, 100)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, w := range tc.want {
				g := got[i]
				// The types of the ranges are not kept by the merge (only the ranges of one list win); compare the rest.
				if g.Start != w.Start || g.End != w.End || g.Collapsed != w.Collapsed || g.Source != w.Source {
					t.Errorf("result[%d] = %+v, want %+v", i, g, w)
				}
				if g.Type != w.Type {
					t.Errorf("result[%d] type = %q, want %q", i, g.Type, w.Type)
				}
			}
		})
	}
}

func TestSanitizeAndMergeKeepsAFoldedRegionUnlessACursorIsInside(t *testing.T) {
	a := []Range{{Start: 1, End: 5}}
	b := []Range{{Start: 1, End: 4, Collapsed: true}} // the region grew
	if got := SanitizeAndMerge(a, b, -1, nil); !got[0].Collapsed {
		t.Error("a folded region of the same first line stays folded when there is no cursor")
	}
	if got := SanitizeAndMerge(a, b, -1, LinesOf([]int{3})); got[0].Collapsed {
		t.Error("a region that changed size with a cursor inside is unfolded")
	}
	b[0].End = 5
	if got := SanitizeAndMerge(a, b, -1, LinesOf([]int{3})); !got[0].Collapsed {
		t.Error("a region of the same size stays folded with a cursor inside")
	}
}

func TestRegionsText(t *testing.T) {
	r, err := FromRanges([]Range{{Start: 0, End: 5, Collapsed: true, Source: UserDefined}, {Start: 1, End: 2, Source: Recovered}, {Start: 3, End: 4, Type: "comment"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.String(), "[u+] 0/5, [r-] 1/2, [ -] 3/4"; got != want {
		t.Errorf("String = %q, want %q", got, want)
	}
	if !r.HasTypes() || r.Type(2) != "comment" || r.Type(0) != "" {
		t.Error("types are wrong")
	}
	if r.Parent(0) != -1 || r.Parent(1) != 0 || r.Parent(2) != 0 {
		t.Errorf("parents = %d %d %d", r.Parent(0), r.Parent(1), r.Parent(2))
	}
	if !r.SetCollapsedAllOfType("comment", true) || !r.Collapsed(2) || r.SetCollapsedAllOfType("none", true) {
		t.Error("SetCollapsedAllOfType is wrong")
	}
	plain, _ := NewRegions([]int{0}, []int{1}, nil)
	if plain.HasTypes() || plain.Type(0) != "" || plain.SetCollapsedAllOfType("x", true) {
		t.Error("regions with no types")
	}
	reg := r.Region(1)
	if !reg.ContainedBy(0, 5) || reg.ContainedBy(2, 5) || !reg.ContainsLine(2) || reg.HidesLine(1) || !reg.HidesLine(2) {
		t.Error("Region helpers are wrong")
	}
}

func TestInvalidRegions(t *testing.T) {
	tests := []struct {
		name         string
		starts, ends []int
		types        []string
		route        string
	}{
		{"different lengths", []int{1, 2}, []int{3}, nil, "NewRegions.FoldingInvalidRegions"},
		{"types of another length", []int{1}, []int{2}, []string{"a", "b"}, "NewRegions.FoldingInvalidRegions"},
		{"too many", make([]int, MaxRegions+1), make([]int, MaxRegions+1), nil, "NewRegions.FoldingInvalidRegions"},
		{"a line past the limit", []int{0}, []int{MaxLine + 1}, nil, "NewRegions.FoldingLineOutOfRange"},
		{"a negative line", []int{-1}, []int{2}, nil, "NewRegions.FoldingLineOutOfRange"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRegions(tc.starts, tc.ends, tc.types)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	// FromRanges passes the failure on.
	_, err := FromRanges([]Range{{Start: 0, End: MaxLine + 1}})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("NewRegions.FoldingLineOutOfRange") {
		t.Errorf("FromRanges error = %v", err)
	}
}

func TestLimitOfRegions(t *testing.T) {
	// Ten regions at three indentations; a limit of 6 keeps the least indented ones that fit.
	var text lines
	for i := 0; i < 4; i++ {
		text = append(text, "a", "  b")
	}
	for i := 0; i < 3; i++ {
		text = append(text, "  c", "    d")
	}
	text = append(text, "      e", "        f", "      g", "        h")
	var computed, limited int
	regions, err := ComputeRanges(text, 4, false, nil, Limit{Max: 6, Update: func(c, l int) { computed, limited = c, l }})
	if err != nil {
		t.Fatal(err)
	}
	if regions.Len() != 6 || computed <= 6 || limited != 6 {
		t.Errorf("len = %d, computed = %d, limited = %d", regions.Len(), computed, limited)
	}
	for i := 0; i < regions.Len(); i++ {
		if indent := len(text[regions.Start(i)]) - len(strings.TrimLeft(text[regions.Start(i)], " ")); indent > 2 {
			t.Errorf("region %d starts on a line indented by %d", i, indent)
		}
	}
	// Under the limit nothing is dropped and Update says so.
	regions, err = ComputeRanges(text, 4, false, nil, Limit{Max: 100, Update: func(c, l int) { computed, limited = c, l }})
	if err != nil || regions.Len() != computed || limited != 0 {
		t.Errorf("len = %d, computed = %d, limited = %d, err = %v", regions.Len(), computed, limited, err)
	}
}
