package folding

import (
	"testing"
)

// Ported from VS Code: reference/vscode/src/vs/editor/contrib/folding/test/browser/indentRangeProvider.test.ts

// wantRange is a region as VS Code's test writes it: one-based lines, the index of the parent, and
// whether it comes from markers.
type wantRange struct {
	start, end, parent int
	marker             bool
}

type indentCase struct {
	name    string
	markers bool // the markers `#region` and `#endregion` with leading white space
	offSide bool
	lines   []string
	want    []wantRange
}

var indentCases = []indentCase{
	{name: "Indentation Folding/Fold one level", markers: false, offSide: true,
		lines: []string{"A", "  A", "  A", "  A"},
		want:  []wantRange{{1, 4, -1, false}}},
	{name: "Indentation Folding/Fold one level", markers: false, offSide: false,
		lines: []string{"A", "  A", "  A", "  A"},
		want:  []wantRange{{1, 4, -1, false}}},
	{name: "Indentation Folding/Fold two levels", markers: false, offSide: true,
		lines: []string{"A", "  A", "  A", "    A", "    A"},
		want:  []wantRange{{1, 5, -1, false}, {3, 5, 0, false}}},
	{name: "Indentation Folding/Fold two levels", markers: false, offSide: false,
		lines: []string{"A", "  A", "  A", "    A", "    A"},
		want:  []wantRange{{1, 5, -1, false}, {3, 5, 0, false}}},
	{name: "Indentation Folding/Fold three levels", markers: false, offSide: true,
		lines: []string{"A", "  A", "    A", "      A", "A"},
		want:  []wantRange{{1, 4, -1, false}, {2, 4, 0, false}, {3, 4, 1, false}}},
	{name: "Indentation Folding/Fold three levels", markers: false, offSide: false,
		lines: []string{"A", "  A", "    A", "      A", "A"},
		want:  []wantRange{{1, 4, -1, false}, {2, 4, 0, false}, {3, 4, 1, false}}},
	{name: "Indentation Folding/Fold decreasing indent", markers: false, offSide: true,
		lines: []string{"    A", "  A", "A"},
		want:  []wantRange{}},
	{name: "Indentation Folding/Fold decreasing indent", markers: false, offSide: false,
		lines: []string{"    A", "  A", "A"},
		want:  []wantRange{}},
	{name: "Indentation Folding/Fold Java", markers: false, offSide: false,
		lines: []string{"class A {", "  void foo() {", "    console.log();", "    console.log();", "  }", "", "  void bar() {", "    console.log();", "  }", "}", "interface B {", "  void bar();", "}"},
		want:  []wantRange{{1, 9, -1, false}, {2, 4, 0, false}, {7, 8, 0, false}, {11, 12, -1, false}}},
	{name: "Indentation Folding/Fold Javadoc", markers: false, offSide: false,
		lines: []string{"/**", " * Comment", " */", "class A {", "  void foo() {", "  }", "}"},
		want:  []wantRange{{1, 3, -1, false}, {4, 6, -1, false}}},
	{name: "Indentation Folding/Fold Whitespace Java", markers: false, offSide: false,
		lines: []string{"class A {", "", "  void foo() {", "     ", "     return 0;", "  }", "      ", "}"},
		want:  []wantRange{{1, 7, -1, false}, {3, 5, 0, false}}},
	{name: "Indentation Folding/Fold Whitespace Python", markers: false, offSide: true,
		lines: []string{"def a:", "  pass", "   ", "  def b:", "    pass", "  ", "      ", "def c: # since there was a deintent here"},
		want:  []wantRange{{1, 5, -1, false}, {4, 5, 0, false}}},
	{name: "Indentation Folding/Fold Tabs", markers: false, offSide: false,
		lines: []string{"class A {", "\t\t", "\tvoid foo() {", "\t \t//hello", "\t    return 0;", "  \t}", "      ", "}"},
		want:  []wantRange{{1, 7, -1, false}, {3, 5, 0, false}}},
	{name: "Folding with regions/Inside region, indented", markers: true, offSide: false,
		lines: []string{"class A {", "  #region", "  void foo() {", "     ", "     return 0;", "  }", "  #endregion", "}"},
		want:  []wantRange{{1, 7, -1, false}, {2, 7, 0, true}, {3, 5, 1, false}}},
	{name: "Folding with regions/Inside region, not indented", markers: true, offSide: false,
		lines: []string{"var x;", "#region", "void foo() {", "     ", "     return 0;", "  }", "#endregion", ""},
		want:  []wantRange{{2, 7, -1, true}, {3, 6, 0, false}}},
	{name: "Folding with regions/Empty Regions", markers: true, offSide: false,
		lines: []string{"var x;", "#region", "#endregion", "#region", "", "#endregion", "var y;"},
		want:  []wantRange{{2, 3, -1, true}, {4, 6, -1, true}}},
	{name: "Folding with regions/Nested Regions", markers: true, offSide: false,
		lines: []string{"var x;", "#region", "#region", "", "#endregion", "#endregion", "var y;"},
		want:  []wantRange{{2, 6, -1, true}, {3, 5, 0, true}}},
	{name: "Folding with regions/Nested Regions 2", markers: true, offSide: false,
		lines: []string{"class A {", "  #region", "", "  #region", "", "  #endregion", "  // comment", "  #endregion", "}"},
		want:  []wantRange{{1, 8, -1, false}, {2, 8, 0, true}, {4, 6, 1, true}}},
	{name: "Folding with regions/Incomplete Regions", markers: true, offSide: false,
		lines: []string{"class A {", "#region", "  // comment", "}"},
		want:  []wantRange{{2, 3, -1, false}}},
	{name: "Folding with regions/Incomplete Regions 2", markers: true, offSide: false,
		lines: []string{"", "#region", "#region", "#region", "  // comment", "#endregion", "#endregion", " // hello"},
		want:  []wantRange{{3, 7, -1, true}, {4, 6, 0, true}}},
	{name: "Folding with regions/Indented region before", markers: true, offSide: false,
		lines: []string{"if (x)", "  return;", "", "#region", "  // comment", "#endregion"},
		want:  []wantRange{{1, 3, -1, false}, {4, 6, -1, true}}},
	{name: "Folding with regions/Indented region before 2", markers: true, offSide: false,
		lines: []string{"if (x)", "  log();", "", "    #region", "      // comment", "    #endregion"},
		want:  []wantRange{{1, 6, -1, false}, {2, 6, 0, false}, {4, 6, 1, true}}},
	{name: "Folding with regions/Indented region in-between", markers: true, offSide: false,
		lines: []string{"#region", "  // comment", "  if (x)", "    return;", "", "#endregion"},
		want:  []wantRange{{1, 6, -1, true}, {3, 5, 0, false}}},
	{name: "Folding with regions/Indented region after", markers: true, offSide: false,
		lines: []string{"#region", "  // comment", "", "#endregion", "  if (x)", "    return;"},
		want:  []wantRange{{1, 4, -1, true}, {5, 6, -1, false}}},
	{name: "Folding with regions/With off-side", markers: true, offSide: true,
		lines: []string{"#region", "  ", "", "#endregion", ""},
		want:  []wantRange{{1, 4, -1, true}}},
	{name: "Folding with regions/Nested with off-side", markers: true, offSide: true,
		lines: []string{"#region", "  ", "#region", "", "#endregion", "", "#endregion", ""},
		want:  []wantRange{{1, 7, -1, true}, {3, 5, 0, true}}},
	{name: "Folding with regions/Issue 35981", markers: true, offSide: false,
		lines: []string{"function thisFoldsToEndOfPage() {", "  const variable = []", "    // #region", "    .reduce((a, b) => a,[]);", "}", "", "function thisFoldsProperly() {", "  const foo = \"bar\"", "}"},
		want:  []wantRange{{1, 4, -1, false}, {2, 4, 0, false}, {7, 8, -1, false}}},
	{name: "Folding with regions/Misspelled Markers", markers: true, offSide: true,
		lines: []string{"#Region", "#endregion", "#regionsandmore", "#endregion", "#region", "#end region", "#region", "#endregionff"},
		want:  []wantRange{}},
	{name: "Folding with regions/Issue 79359", markers: true, offSide: true,
		lines: []string{"#region", "", "class A", "  foo", "", "class A", "  foo", "", "#endregion"},
		want:  []wantRange{{1, 9, -1, true}, {3, 4, 0, false}, {6, 7, 0, false}}},
}

func assertRanges(t *testing.T, text []string, want []wantRange, offSide bool, markers *Markers) {
	t.Helper()
	regions, err := ComputeRanges(lines(text), 4, offSide, markers, Limit{})
	if err != nil {
		t.Fatal(err)
	}
	var got []wantRange
	for i := 0; i < regions.Len(); i++ {
		got = append(got, wantRange{regions.Start(i) + 1, regions.End(i) + 1, regions.Parent(i), false})
	}
	if len(got) != len(want) {
		t.Fatalf("regions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].start != want[i].start || got[i].end != want[i].end || got[i].parent != want[i].parent {
			t.Fatalf("regions = %v, want %v", got, want)
		}
	}
}

func TestVSCodeIndentRanges(t *testing.T) {
	for _, tc := range indentCases {
		t.Run(tc.name, func(t *testing.T) {
			var markers *Markers
			if tc.markers {
				markers = &Markers{Start: ecma(t, `^\s*#region\b`, false), End: ecma(t, `^\s*#endregion\b`, false)}
			}
			assertRanges(t, tc.lines, tc.want, tc.offSide, markers)
		})
	}
}

func TestVSCodeMarkersWithStatefulFlags(t *testing.T) {
	// The flags of the two patterns may differ; each is used as it is.
	tests := []struct {
		name         string
		lines        []string
		start, end   bool // ignore case
		want         []wantRange
		startPattern string
	}{
		{"both global", []string{"#region", "content", "#endregion"}, false, false, []wantRange{{1, 3, -1, true}}, `^\s*#region\b`},
		{"start ignores case", []string{"#REGION", "content", "#endregion"}, true, false, []wantRange{{1, 3, -1, true}}, `^\s*#region\b`},
		{"end does not match", []string{"#REGION", "content", "#ENDREGION"}, true, false, nil, `^\s*#region\b`},
		{"start does not match", []string{"#REGION", "content", "#ENDREGION"}, false, true, nil, `^\s*#region\b`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			markers := &Markers{Start: ecma(t, tc.startPattern, tc.start), End: ecma(t, `^\s*#endregion\b`, tc.end)}
			assertRanges(t, tc.lines, tc.want, false, markers)
		})
	}
}
