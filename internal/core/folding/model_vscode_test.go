package folding

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Ported from VS Code: reference/vscode/src/vs/editor/contrib/folding/test/browser/foldingModel.test.ts
// The tests keep VS Code's one-based line numbers; the harness converts them.

const testOwner = 7

// expected is a region as the tests write it.
type expected struct {
	start, end int
	collapsed  bool
}

func r(start, end int, collapsed ...bool) expected {
	return expected{start, end, len(collapsed) > 0 && collapsed[0]}
}

type expectedDecoration struct {
	line int
	kind string
}

// testDecorations is VS Code's TestDecorationProvider: the class name says what the decoration is.
func testDecorations(collapsed, hidden, _ bool) document.DecorationOptions {
	opts := document.DecorationOptions{Stickiness: document.NeverGrowsWhenTypingAtEdges}
	switch {
	case hidden:
		opts.ClassName = "hidden"
	case collapsed:
		opts.ClassName = "collapsed"
	default:
		opts.ClassName = "expanded"
	}
	return opts
}

// harness is a folding model over a document of lines.
type harness struct {
	t     *testing.T
	doc   *document.Document
	model *Model
}

func newHarness(t *testing.T, text ...string) *harness {
	t.Helper()
	doc := document.New(document.UntitledURI(1), strings.Join(text, "\n"), document.NewOptions())
	return &harness{t: t, doc: doc, model: NewModel(doc, testOwner, testDecorations)}
}

type docText struct{ doc *document.Document }

func (d docText) LineCount() int { return d.doc.Snapshot().LineCount() }

func (d docText) LineContent(i int) string {
	s, _ := d.doc.Snapshot().LineContent(i)
	return s
}

func (h *harness) compute(offSide bool, markers *Markers) *Regions {
	h.t.Helper()
	regions, err := ComputeRanges(docText{h.doc}, 4, offSide, markers, Limit{})
	if err != nil {
		h.t.Fatal(err)
	}
	return regions
}

func (h *harness) update(regions *Regions, sel SelectedLines) {
	h.t.Helper()
	if err := h.model.Update(regions, sel); err != nil {
		h.t.Fatal(err)
	}
}

// at is the region at a one-based line.
func (h *harness) at(line int) Region {
	h.t.Helper()
	region, ok := h.model.RegionAtLine(line - 1)
	if !ok {
		h.t.Fatalf("no region at line %d", line)
	}
	return region
}

func (h *harness) toggle(lines ...int) {
	h.t.Helper()
	var regions []Region
	for _, l := range lines {
		regions = append(regions, h.at(l))
	}
	h.model.ToggleCollapseState(regions)
}

func (h *harness) assertRanges(want []expected, msg ...string) {
	h.t.Helper()
	var got []expected
	regions := h.model.Regions()
	for i := 0; i < regions.Len(); i++ {
		got = append(got, expected{regions.Start(i) + 1, regions.End(i) + 1, regions.Collapsed(i)})
	}
	if !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
		h.t.Errorf("%v: ranges = %v, want %v", msg, got, want)
	}
}

func (h *harness) assertFolded(want []expected, msg ...string) {
	h.t.Helper()
	var got []expected
	regions := h.model.Regions()
	for i := 0; i < regions.Len(); i++ {
		if regions.Collapsed(i) {
			got = append(got, expected{regions.Start(i) + 1, regions.End(i) + 1, false})
		}
	}
	var wantPlain []expected
	for _, w := range want {
		wantPlain = append(wantPlain, expected{w.start, w.end, false})
	}
	if !reflect.DeepEqual(got, wantPlain) && (len(got) != 0 || len(wantPlain) != 0) {
		h.t.Errorf("%v: folded = %v, want %v", msg, got, wantPlain)
	}
}

func (h *harness) assertDecorations(want []expectedDecoration) {
	h.t.Helper()
	var got []expectedDecoration
	for _, d := range h.doc.DecorationsOf(testOwner) {
		got = append(got, expectedDecoration{d.Range.Start.Line + 1, d.Options.ClassName})
	}
	if !reflect.DeepEqual(got, want) {
		h.t.Errorf("decorations = %v, want %v", got, want)
	}
}

func assertRegions(t *testing.T, got []Region, want []expected, msg string) {
	t.Helper()
	var plain []expected
	for _, g := range got {
		plain = append(plain, expected{g.Start() + 1, g.End() + 1, g.Collapsed()})
	}
	if !reflect.DeepEqual(plain, want) && (len(plain) != 0 || len(want) != 0) {
		t.Errorf("%s: regions = %v, want %v", msg, plain, want)
	}
}

func assertRegion(t *testing.T, region Region, ok bool, want *expected, msg string) {
	t.Helper()
	if ok != (want != nil) {
		t.Errorf("%s: found = %v, want %v", msg, ok, want != nil)
		return
	}
	if ok {
		got := expected{region.Start() + 1, region.End() + 1, region.Collapsed()}
		if got != *want {
			t.Errorf("%s: region = %v, want %v", msg, got, *want)
		}
	}
}

// zero converts the one-based lines of a test to the lines of the model.
func zero(lines []int) []int {
	out := make([]int, len(lines))
	for i, l := range lines {
		out[i] = l - 1
	}
	return out
}

// edit applies an edit written with zero-based positions.
func (h *harness) edit(r buffer.Range, text string) {
	h.t.Helper()
	if _, err := h.doc.Edit([]document.Edit{{Range: r, Text: text}}, document.EditOptions{Kind: document.KindOther}); err != nil {
		h.t.Fatal(err)
	}
}

func javaDoc() []string {
	return []string{"/**", " * Comment", " */", "class A {", "  void foo() {", "    // comment {", "  }", "}"}
}

func TestGetRegionAtLine(t *testing.T) {
	h := newHarness(t, javaDoc()...)
	h.update(h.compute(false, nil), nil)
	r1, r2, r3 := r(1, 3), r(4, 7), r(5, 6)
	h.assertRanges([]expected{r1, r2, r3})
	for line, want := range map[int]*expected{1: &r1, 2: &r1, 3: &r1, 4: &r2, 5: &r3, 6: &r3, 7: &r2, 8: nil} {
		region, ok := h.model.RegionAtLine(line - 1)
		assertRegion(t, region, ok, want, "line "+strconv.Itoa(line))
	}
}

func TestCollapse(t *testing.T) {
	h := newHarness(t, javaDoc()...)
	ranges := h.compute(false, nil)
	h.update(ranges, nil)
	r1, r2, r3 := r(1, 3), r(4, 7), r(5, 6)
	h.assertRanges([]expected{r1, r2, r3})

	h.toggle(1)
	h.update(ranges, nil)
	h.assertRanges([]expected{r(1, 3, true), r2, r3})

	h.toggle(5)
	h.update(ranges, nil)
	h.assertRanges([]expected{r(1, 3, true), r2, r(5, 6, true)})

	h.toggle(7)
	h.update(ranges, nil)
	h.assertRanges([]expected{r(1, 3, true), r(4, 7, true), r(5, 6, true)})
}

func TestUpdate(t *testing.T) {
	h := newHarness(t, javaDoc()...)
	h.update(h.compute(false, nil), nil)
	h.assertRanges([]expected{r(1, 3), r(4, 7), r(5, 6)})
	h.toggle(2, 5)
	h.edit(buffer.Range{Start: buffer.Position{Line: 3}, End: buffer.Position{Line: 3}}, "//hello\n")
	h.update(h.compute(false, nil), nil)
	h.assertRanges([]expected{r(1, 3, true), r(5, 8), r(6, 7, true)})
}

func TestDelete(t *testing.T) {
	h := newHarness(t,
		"function foo() {", "  switch (x) {", "    case 1:", "      //hello1", "      break;", "    case 2:",
		"      //hello2", "      break;", "    case 3:", "      //hello3", "      break;", "  }", "}")
	h.update(h.compute(false, nil), nil)
	h.assertRanges([]expected{r(1, 12), r(2, 11), r(3, 5), r(6, 8), r(9, 11)})
	h.toggle(6)
	h.edit(buffer.Range{Start: buffer.Position{Line: 5, Col: 10}, End: buffer.Position{Line: 8}}, "")
	h.update(h.compute(true, nil), LinesOf(zero([]int{7})))
	h.assertRanges([]expected{r(1, 9), r(2, 8), r(3, 5), r(6, 8)})
}

func TestGetRegionsInside(t *testing.T) {
	h := newHarness(t, javaDoc()...)
	h.update(h.compute(false, nil), nil)
	r1, r2, r3 := r(1, 3), r(4, 7), r(5, 6)
	h.assertRanges([]expected{r1, r2, r3})
	region1, region2, region3 := h.at(1), h.at(4), h.at(5)
	assertRegions(t, h.model.RegionsInside(Region{}, false, nil), []expected{r1, r2, r3}, "1")
	assertRegions(t, h.model.RegionsInside(region1, true, nil), nil, "2")
	assertRegions(t, h.model.RegionsInside(region2, true, nil), []expected{r3}, "3")
	assertRegions(t, h.model.RegionsInside(region3, true, nil), nil, "4")
}

func TestGetRegionsInsideWithLevel(t *testing.T) {
	h := newHarness(t,
		"//#region", "//#endregion", "class A {", "  void foo() {", "    if (true) {", "        return;", "    }",
		"    if (true) {", "      return;", "    }", "  }", "}")
	h.update(h.compute(false, &Markers{Start: ecma(t, `^\/\/#region$`, false), End: ecma(t, `^\/\/#endregion$`, false)}), nil)
	r1, r2, r3, r4, r5 := r(1, 2), r(3, 11), r(4, 10), r(5, 6), r(8, 9)
	region1, region2, region3 := h.at(1), h.at(3), h.at(4)
	h.assertRanges([]expected{r1, r2, r3, r4, r5})
	level := func(n int) func(Region, int) bool { return func(_ Region, l int) bool { return l == n } }
	assertRegions(t, h.model.RegionsInside(Region{}, false, level(1)), []expected{r1, r2}, "1")
	assertRegions(t, h.model.RegionsInside(Region{}, false, level(2)), []expected{r3}, "2")
	assertRegions(t, h.model.RegionsInside(Region{}, false, level(3)), []expected{r4, r5}, "3")
	assertRegions(t, h.model.RegionsInside(region2, true, level(1)), []expected{r3}, "4")
	assertRegions(t, h.model.RegionsInside(region2, true, level(2)), []expected{r4, r5}, "5")
	assertRegions(t, h.model.RegionsInside(region3, true, level(1)), []expected{r4, r5}, "6")
	assertRegions(t, h.model.RegionsInside(region2, true, func(r Region, _ int) bool { return r.HidesLine(8) }), []expected{r3, r5}, "7")
	assertRegions(t, h.model.RegionsInside(region1, true, level(1)), nil, "8")
}

func TestGetRegionAtLine2(t *testing.T) {
	h := newHarness(t,
		"//#region", "class A {", "  void foo() {", "    if (true) {", "      //hello", "    }", "", "  }", "}", "//#endregion", "")
	h.update(h.compute(false, &Markers{Start: ecma(t, `^\/\/#region$`, false), End: ecma(t, `^\/\/#endregion$`, false)}), nil)
	r1, r2, r3, r4 := r(1, 10), r(2, 8), r(3, 7), r(4, 5)
	h.assertRanges([]expected{r1, r2, r3, r4})
	at := func(line int) []Region { return h.model.AllRegionsAtLine(line-1, nil) }
	assertRegions(t, at(1), []expected{r1}, "1")
	assertRegions(t, at(2), []expected{r2, r1}, "2")
	assertRegions(t, at(3), []expected{r3, r2, r1}, "3")
	assertRegions(t, at(4), []expected{r4, r3, r2, r1}, "4")
	assertRegions(t, at(5), []expected{r4, r3, r2, r1}, "5")
	assertRegions(t, at(6), []expected{r3, r2, r1}, "6")
	assertRegions(t, at(7), []expected{r3, r2, r1}, "7")
	assertRegions(t, at(8), []expected{r2, r1}, "8")
	assertRegions(t, at(9), []expected{r1}, "9")
	assertRegions(t, at(10), []expected{r1}, "10")
	assertRegions(t, at(11), nil, "11")
}

// nested is the text of the tests that fold by level.
func nested(extra ...string) []string {
	return append([]string{
		"//#region", "//#endregion", "class A {", "  void foo() {", "    if (true) {", "        return;", "    }", "",
		"    if (true) {", "      return;", "    }", "  }",
	}, extra...)
}

func regionMarkersWithSpace(t testing.TB) *Markers {
	return &Markers{Start: ecma(t, `^\s*\/\/#region$`, false), End: ecma(t, `^\s*\/\/#endregion$`, false)}
}

func TestSetCollapseStateRecursively(t *testing.T) {
	h := newHarness(t, nested("}")...)
	h.update(h.compute(false, &Markers{Start: ecma(t, `^\/\/#region$`, false), End: ecma(t, `^\/\/#endregion$`, false)}), nil)
	r1, r2, r3, r4, r5 := r(1, 2), r(3, 12), r(4, 11), r(5, 6), r(9, 10)
	h.assertRanges([]expected{r1, r2, r3, r4, r5})

	SetCollapseStateLevelsDown(h.model, true, AllLevels, zero([]int{4}))
	h.assertFolded([]expected{r3, r4, r5}, "1")
	SetCollapseStateLevelsDown(h.model, false, AllLevels, zero([]int{8}))
	h.assertFolded(nil, "2")
	SetCollapseStateLevelsDown(h.model, true, AllLevels, zero([]int{12}))
	h.assertFolded([]expected{r2, r3, r4, r5}, "3")
	SetCollapseStateLevelsDown(h.model, false, AllLevels, zero([]int{7}))
	h.assertFolded([]expected{r2}, "4")
	SetCollapseStateLevelsDown(h.model, false, AllLevels, nil)
	h.assertFolded(nil, "5")
	SetCollapseStateLevelsDown(h.model, true, AllLevels, nil)
	h.assertFolded([]expected{r1, r2, r3, r4, r5}, "6")
}

func TestSetCollapseStateAtLevel(t *testing.T) {
	h := newHarness(t, nested("  //#region", "  const bar = 9;", "  //#endregion", "}")...)
	h.update(h.compute(false, regionMarkersWithSpace(t)), nil)
	r1, r2, r3, r4, r5, r6 := r(1, 2), r(3, 15), r(4, 11), r(5, 6), r(9, 10), r(13, 15)
	h.assertRanges([]expected{r1, r2, r3, r4, r5, r6})

	SetCollapseStateAtLevel(h.model, 1, true, nil)
	h.assertFolded([]expected{r1, r2}, "1")
	SetCollapseStateAtLevel(h.model, 1, false, zero([]int{5}))
	h.assertFolded([]expected{r2}, "2")
	SetCollapseStateAtLevel(h.model, 1, false, zero([]int{1}))
	h.assertFolded(nil, "3")
	SetCollapseStateAtLevel(h.model, 2, true, nil)
	h.assertFolded([]expected{r3, r6}, "4")
	SetCollapseStateAtLevel(h.model, 2, false, zero([]int{5, 6}))
	h.assertFolded([]expected{r3}, "5")
	SetCollapseStateAtLevel(h.model, 3, true, zero([]int{4, 9}))
	h.assertFolded([]expected{r3, r4}, "6")
	SetCollapseStateAtLevel(h.model, 3, false, zero([]int{4, 9}))
	h.assertFolded([]expected{r3}, "7")
}

func TestSetCollapseStateLevelsDown(t *testing.T) {
	h := newHarness(t, nested("}")...)
	h.update(h.compute(false, &Markers{Start: ecma(t, `^\/\/#region$`, false), End: ecma(t, `^\/\/#endregion$`, false)}), nil)
	r1, r2, r3, r4, r5 := r(1, 2), r(3, 12), r(4, 11), r(5, 6), r(9, 10)
	h.assertRanges([]expected{r1, r2, r3, r4, r5})

	SetCollapseStateLevelsDown(h.model, true, 1, zero([]int{4}))
	h.assertFolded([]expected{r3}, "1")
	SetCollapseStateLevelsDown(h.model, true, 2, zero([]int{4}))
	h.assertFolded([]expected{r3, r4, r5}, "2")
	SetCollapseStateLevelsDown(h.model, false, 2, zero([]int{3}))
	h.assertFolded([]expected{r4, r5}, "3")
	SetCollapseStateLevelsDown(h.model, false, 2, zero([]int{2}))
	h.assertFolded([]expected{r4, r5}, "4")
	SetCollapseStateLevelsDown(h.model, true, 4, zero([]int{2}))
	h.assertFolded([]expected{r1, r4, r5}, "5")
	SetCollapseStateLevelsDown(h.model, false, 4, zero([]int{2, 3}))
	h.assertFolded(nil, "6")
}

func TestSetCollapseStateLevelsUp(t *testing.T) {
	h := newHarness(t, nested("}")...)
	h.update(h.compute(false, &Markers{Start: ecma(t, `^\/\/#region$`, false), End: ecma(t, `^\/\/#endregion$`, false)}), nil)
	r1, r2, r3, r4, r5 := r(1, 2), r(3, 12), r(4, 11), r(5, 6), r(9, 10)
	h.assertRanges([]expected{r1, r2, r3, r4, r5})
	_ = r1
	_ = r4

	SetCollapseStateLevelsUp(h.model, true, 1, zero([]int{4}))
	h.assertFolded([]expected{r3}, "1")
	SetCollapseStateLevelsUp(h.model, true, 2, zero([]int{4}))
	h.assertFolded([]expected{r2, r3}, "2")
	SetCollapseStateLevelsUp(h.model, false, 4, zero([]int{1, 3, 4}))
	h.assertFolded(nil, "3")
	SetCollapseStateLevelsUp(h.model, true, 2, zero([]int{10}))
	h.assertFolded([]expected{r3, r5}, "4")
}

func TestSetCollapseStateUp(t *testing.T) {
	h := newHarness(t, nested("}")...)
	h.update(h.compute(false, &Markers{Start: ecma(t, `^\/\/#region$`, false), End: ecma(t, `^\/\/#endregion$`, false)}), nil)
	r1, r2, r3, r4, r5 := r(1, 2), r(3, 12), r(4, 11), r(5, 6), r(9, 10)
	h.assertRanges([]expected{r1, r2, r3, r4, r5})

	SetCollapseStateUp(h.model, true, zero([]int{5}))
	h.assertFolded([]expected{r4}, "1")
	SetCollapseStateUp(h.model, true, zero([]int{5}))
	h.assertFolded([]expected{r3, r4}, "2")
	SetCollapseStateUp(h.model, true, zero([]int{4}))
	h.assertFolded([]expected{r2, r3, r4}, "3")
}

func TestSetCollapseStateForMatchingLines(t *testing.T) {
	h := newHarness(t,
		"/**", " * the class", " */", "class A {", "  /**", "   * the foo", "   */", "  void foo() {", "    /*",
		"     * the comment", "     */", "  }", "}")
	h.update(h.compute(false, &Markers{Start: ecma(t, `^\/\/#region$`, false), End: ecma(t, `^\/\/#endregion$`, false)}), nil)
	r1, r2, r3, r4, r5 := r(1, 3), r(4, 12), r(5, 7), r(8, 11), r(9, 11)
	h.assertRanges([]expected{r1, r2, r3, r4, r5})
	SetCollapseStateForMatchingLines(h.model, MatchesRegex(ecma(t, `^\s*\/\*`, false)), true)
	h.assertFolded([]expected{r1, r3, r5}, "1")
}

func TestSetCollapseStateForRest(t *testing.T) {
	h := newHarness(t, nested("}")...)
	h.update(h.compute(false, &Markers{Start: ecma(t, `^\/\/#region$`, false), End: ecma(t, `^\/\/#endregion$`, false)}), nil)
	r1, r2, r3, r4, r5 := r(1, 2), r(3, 12), r(4, 11), r(5, 6), r(9, 10)
	h.assertRanges([]expected{r1, r2, r3, r4, r5})

	SetCollapseStateForRest(h.model, true, zero([]int{5}))
	h.assertFolded([]expected{r1, r5}, "1")
	SetCollapseStateForRest(h.model, false, zero([]int{5}))
	h.assertFolded(nil, "2")
	SetCollapseStateForRest(h.model, true, zero([]int{1}))
	h.assertFolded([]expected{r2, r3, r4, r5}, "3")
	SetCollapseStateForRest(h.model, true, zero([]int{3}))
	h.assertFolded([]expected{r1, r2, r3, r4, r5}, "4")
}

func TestFoldingDecoration(t *testing.T) {
	h := newHarness(t, "class A {", "  void foo() {", "    if (true) {", "      hoo();", "    }", "  }", "}")
	ranges := h.compute(false, nil)
	h.update(ranges, nil)
	r1, r2, r3 := r(1, 6), r(2, 5), r(3, 4)
	h.assertRanges([]expected{r1, r2, r3})
	h.assertDecorations([]expectedDecoration{{1, "expanded"}, {2, "expanded"}, {3, "expanded"}})

	h.toggle(2)
	h.assertRanges([]expected{r1, r(2, 5, true), r3})
	h.assertDecorations([]expectedDecoration{{1, "expanded"}, {2, "collapsed"}, {3, "hidden"}})
	h.update(ranges, nil)
	h.assertRanges([]expected{r1, r(2, 5, true), r3})
	h.assertDecorations([]expectedDecoration{{1, "expanded"}, {2, "collapsed"}, {3, "hidden"}})

	h.toggle(1)
	h.assertRanges([]expected{r(1, 6, true), r(2, 5, true), r3})
	h.assertDecorations([]expectedDecoration{{1, "collapsed"}, {2, "hidden"}, {3, "hidden"}})
	h.update(ranges, nil)
	h.assertRanges([]expected{r(1, 6, true), r(2, 5, true), r3})
	h.assertDecorations([]expectedDecoration{{1, "collapsed"}, {2, "hidden"}, {3, "hidden"}})

	h.toggle(1, 3)
	h.assertRanges([]expected{r1, r(2, 5, true), r(3, 4, true)})
	h.assertDecorations([]expectedDecoration{{1, "expanded"}, {2, "collapsed"}, {3, "hidden"}})
	h.update(ranges, nil)
	h.assertRanges([]expected{r1, r(2, 5, true), r(3, 4, true)})
	h.assertDecorations([]expectedDecoration{{1, "expanded"}, {2, "collapsed"}, {3, "hidden"}})
}

// jump gives the result of a fold jump as VS Code's tests write it: a one-based line, or 0 for none.
func jump(fn func(int, *Model) (int, bool), line int, m *Model) int {
	got, ok := fn(line-1, m)
	if !ok {
		return 0
	}
	return got + 1
}

func TestFoldJumping(t *testing.T) {
	h := newHarness(t,
		"class A {", "  void foo() {", "    if (1) {", "      a();", "    } else if (2) {", "      if (true) {", "        b();",
		"      }", "    } else {", "      c();", "    }", "  }", "}")
	h.update(h.compute(false, nil), nil)
	h.assertRanges([]expected{r(1, 12), r(2, 11), r(3, 4), r(5, 8), r(6, 7), r(9, 10)})

	for _, tc := range []struct {
		name string
		fn   func(int, *Model) (int, bool)
		line int
		want int
	}{
		{"parent", ParentFoldLine, 7, 6}, {"parent", ParentFoldLine, 6, 5}, {"parent", ParentFoldLine, 5, 2},
		{"parent", ParentFoldLine, 2, 1}, {"parent", ParentFoldLine, 1, 0},
		{"previous", PreviousFoldLine, 10, 9}, {"previous", PreviousFoldLine, 9, 5}, {"previous", PreviousFoldLine, 5, 3},
		{"previous", PreviousFoldLine, 3, 0},
		{"previous off a start line", PreviousFoldLine, 4, 3}, {"previous off a start line", PreviousFoldLine, 7, 6},
		{"previous off a start line", PreviousFoldLine, 8, 6},
		{"next", NextFoldLine, 3, 5}, {"next", NextFoldLine, 5, 9}, {"next", NextFoldLine, 9, 0},
		{"next off a start line", NextFoldLine, 4, 5}, {"next off a start line", NextFoldLine, 7, 9},
		{"next off a start line", NextFoldLine, 8, 9},
	} {
		if got := jump(tc.fn, tc.line, h.model); got != tc.want {
			t.Errorf("%s from line %d = %d, want %d", tc.name, tc.line, got, tc.want)
		}
	}
}

func TestFoldJumpingIssue129503(t *testing.T) {
	h := newHarness(t, "", "if True:", "  print(1)", "if True:", "  print(1)", "")
	h.update(h.compute(false, nil), nil)
	h.assertRanges([]expected{r(2, 3), r(4, 6)})
	for line, want := range []int{2, 4, 4, 0, 0, 0} {
		if got := jump(NextFoldLine, line+1, h.model); got != want {
			t.Errorf("next from line %d = %d, want %d", line+1, got, want)
		}
	}
	for line, want := range []int{0, 0, 2, 2, 4, 4} {
		if got := jump(PreviousFoldLine, line+1, h.model); got != want {
			t.Errorf("previous from line %d = %d, want %d", line+1, got, want)
		}
	}
}

// manualRegions updates the model with ranges written with one-based lines.
func (h *harness) manualRegions(ranges ...Range) {
	h.t.Helper()
	for i := range ranges {
		ranges[i].Start--
		ranges[i].End--
	}
	regions, err := FromRanges(ranges)
	if err != nil {
		h.t.Fatal(err)
	}
	h.update(regions, nil)
}

func provider(from, to int) Range { return Range{Start: from, End: to} }

func user(from, to int) Range { return Range{Start: from, End: to, Source: UserDefined} }

func cursor(line int) buffer.Range {
	return buffer.Range{Start: buffer.Position{Line: line - 1}, End: buffer.Position{Line: line - 1}}
}

func span(l1, c1, l2, c2 int) buffer.Range {
	return buffer.Range{Start: buffer.Position{Line: l1 - 1, Col: c1 - 1}, End: buffer.Position{Line: l2 - 1, Col: c2 - 1}}
}

func (h *harness) removeManual(ranges ...buffer.Range) {
	h.t.Helper()
	if err := h.model.RemoveManualRanges(ranges); err != nil {
		h.t.Fatal(err)
	}
}

func TestRemoveManualRanges(t *testing.T) {
	t.Run("cursor on a manual range removes the innermost only", func(t *testing.T) {
		h := newHarness(t, "class A {", "  void foo() {", "    if (true) {", "      return;", "    }", "  }", "}")
		h.manualRegions(provider(1, 6), user(2, 5), user(3, 4))
		h.assertRanges([]expected{r(1, 6), r(2, 5), r(3, 4)})
		h.removeManual(cursor(4))
		h.assertRanges([]expected{r(1, 6), r(2, 5)})
		h.removeManual(cursor(3))
		h.assertRanges([]expected{r(1, 6)})
	})
	t.Run("cursor skips provider ranges to remove the nearest manual range", func(t *testing.T) {
		h := newHarness(t, "class A {", "  void foo() {", "    return;", "  }", "}")
		h.manualRegions(user(1, 5), provider(2, 4))
		h.removeManual(cursor(3))
		h.assertRanges([]expected{r(2, 4)})
	})
	t.Run("cursor not on a manual range removes all manual ranges", func(t *testing.T) {
		h := newHarness(t, "// header", "class A {", "  void foo() {", "  }", "}", "// footer")
		h.manualRegions(provider(2, 4), user(3, 4))
		h.assertRanges([]expected{r(2, 4), r(3, 4)})
		h.removeManual(span(6, 1, 6, 2))
		h.assertRanges([]expected{r(2, 4), r(3, 4)})
		h.removeManual(cursor(6))
		h.assertRanges([]expected{r(2, 4)})
	})
	t.Run("a single-line selection removes all intersecting manual ranges", func(t *testing.T) {
		h := newHarness(t, "class A {", "  void foo() {", "    if (true) {", "      return;", "    }", "  }", "}")
		h.manualRegions(provider(1, 6), user(2, 5), user(3, 4))
		h.removeManual(span(4, 1, 4, 2))
		h.assertRanges([]expected{r(1, 6)})
	})
	t.Run("a selection removes the intersecting manual ranges", func(t *testing.T) {
		h := newHarness(t, "class A {", "  void foo() {", "  }", "  void bar() {", "  }", "}")
		h.manualRegions(provider(1, 5), user(2, 3), user(4, 5))
		h.assertRanges([]expected{r(1, 5), r(2, 3), r(4, 5)})
		h.removeManual(span(2, 1, 3, 1))
		h.assertRanges([]expected{r(1, 5), r(4, 5)})
	})
}
