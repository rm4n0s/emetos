package app

import (
	"github.com/rm4n0s/glutter/gluttertest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/settings"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	testview "github.com/rm4n0s/emetos/internal/gui/views/testing"
)

// Coverage (step 26.4, SPEC §10.32 "Test Coverage"): the layer the editor shows, and the whole path against the fake controller of
// extensions/devtesting through the real host and model: the view, the editor's marks and toolbar, the Explorer's percentages and the
// commands and settings.

// coverageSource is a file with coverage: the lines are those coverageScript says (counted from 1).
const coverageSource = `package calc

func Add(a, b int) int {
	return a + b
}

func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
`

// coverageUtilSource is a second file, in a folder, that no test reached.
const coverageUtilSource = `package pkg

func Unused() int {
	return 1
}
`

// coverageScript reports the coverage of both files for a run with coverage: Clamp's second if never took its branch.
const coverageScript = `{
  "label": "Fake Tests (dev)",
  "delayMs": 1,
  "tests": [
    {"id": "TestAdd", "label": "TestAdd", "file": "calc_test.go", "line": 5, "endLine": 9, "result": "passed", "ms": 2},
    {"id": "TestClamp", "label": "TestClamp", "file": "calc_test.go", "line": 11, "endLine": 15, "result": "passed", "ms": 2}
  ],
  "coverage": [
    {"file": "calc.go",
      "statements": [
        {"line": 4, "hits": 3},
        {"line": 8, "hits": 6, "branches": [{"hits": 2, "label": "v < lo"}, {"hits": 4, "label": "else"}]},
        {"line": 9, "hits": 2},
        {"line": 11, "hits": 4, "branches": [{"hits": 0, "label": "v > hi"}, {"hits": 4, "label": "else"}]},
        {"line": 12, "hits": 0},
        {"line": 14, "hits": 4}
      ],
      "declarations": [{"name": "Add", "line": 3, "endLine": 5, "hits": 3}, {"name": "Clamp", "line": 7, "endLine": 15, "hits": 6}]},
    {"file": "pkg/util.go",
      "statements": [{"line": 4, "hits": 0}],
      "declarations": [{"name": "Unused", "line": 3, "endLine": 5, "hits": 0}]}
  ]
}`

// coverageRig is a window with calc.go open, after a run with coverage.
func coverageRig(t *testing.T, o testingOpts) (*testingRig, *textEditor) {
	t.Helper()
	if o.script == "" {
		o.script = coverageScript
	}
	if o.files == nil {
		o.files = map[string]string{}
	}
	o.files["calc.go"] = coverageSource
	if o.open == nil {
		o.open = []string{"calc.go"}
	}
	if o.settings == nil {
		o.settings = map[string]any{}
	}
	if _, ok := o.settings["testing.openTesting"]; !ok {
		o.settings["testing.openTesting"] = "neverOpen"
	}
	r := newTestingRig(t, o)
	if err := os.MkdirAll(filepath.Join(r.folder, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.folder, "pkg", "util.go"), []byte(coverageUtilSource), 0o644); err != nil {
		t.Fatal(err)
	}
	e := r.win.shell.activeEditor()
	if e == nil || e.cov == nil {
		t.Fatalf("the editor has no coverage: %+v", e)
	}
	return r, e
}

// covered runs the tests with coverage and waits until the editor shows it.
func (r *testingRig) covered(e *textEditor) {
	r.t.Helper()
	r.run(command.TestingCoverageAll, nil)
	r.finished()
	r.waitFor("the coverage in the editor", func() bool { return len(e.view.CoverageMarks()) > 0 })
}

func TestBuildCoverageLayer(t *testing.T) {
	r := func(l1, c1, l2, c2 int) coretesting.Range {
		return coretesting.Range{Start: coretesting.Pos{Line: l1, Col: c1}, End: coretesting.Pos{Line: l2, Col: c2}}
	}
	text := func(coretesting.Range) string { return "x := 1" }
	details := coretesting.Details{
		Statements: []coretesting.StatementDetail{
			{Executed: 4, Location: r(0, 0, 1, 0)},
			{Executed: 0, Location: r(1, 0, 2, 0)},
			{Executed: 0, Location: r(2, 0, 3, 0)},
			{Executed: 7, Location: r(5, 0, 6, 0), Branches: []coretesting.BranchDetail{{Executed: 7}, {Executed: 0}}},
			{Executed: 150, Location: r(8, 0, 9, 0)},
		},
	}
	ranges := coretesting.BuildDetailRanges(details)
	type mark struct {
		first, last int
		id          string
	}
	lineMarks := func(l coverageLayer) (out []mark) {
		for _, m := range l.marks {
			if m.WholeLine {
				out = append(out, mark{m.Range.Start.Line, m.Range.End.Line, m.GutterID})
			}
		}
		return out
	}
	t.Run("the gutter without the inline coverage", func(t *testing.T) {
		l := buildCoverageLayer(details, ranges, false, text)
		want := []mark{{0, 0, covGutterHit}, {1, 2, covGutterMiss}, {5, 5, covGutterHit}, {8, 8, covGutterHit}}
		if got := lineMarks(l); !slices.Equal(got, want) {
			t.Errorf("gutter marks %+v, want %+v", got, want)
		}
		if len(l.marks) != len(want) || len(l.notes) != 0 {
			t.Errorf("%d marks and %d notes without the inline coverage, want %d and 0", len(l.marks), len(l.notes), len(want))
		}
		if !slices.Equal(l.missed, []int{1, 2}) {
			t.Errorf("missed lines %v, want [1 2]", l.missed)
		}
	})
	t.Run("the inline coverage adds backgrounds, counts and branches", func(t *testing.T) {
		l := buildCoverageLayer(details, ranges, true, text)
		var hit, miss int
		for _, m := range l.marks {
			switch m.BackgroundID {
			case covInlineHit:
				hit++
			case covInlineMiss:
				miss++
			}
		}
		if hit != 3 || miss != 2 {
			t.Errorf("%d covered and %d uncovered backgrounds, want 3 and 2", hit, miss)
		}
		notes := map[int][]string{}
		for _, n := range l.notes {
			notes[n.Line] = append(notes[n.Line], n.Text)
		}
		want := map[int][]string{0: {" 4x "}, 5: {" 7x ", " 1/2 branches "}, 8: {" 99+x "}}
		for line, texts := range want {
			if !slices.Equal(notes[line], texts) {
				t.Errorf("line %d has the notes %q, want %q", line, notes[line], texts)
			}
		}
		if len(notes) != len(want) {
			t.Errorf("notes %v, want only the lines of %v", notes, want)
		}
	})
	t.Run("no details", func(t *testing.T) {
		l := buildCoverageLayer(coretesting.Details{}, nil, true, text)
		if len(l.marks) != 0 || len(l.notes) != 0 || len(l.missed) != 0 {
			t.Errorf("a layer %+v for nothing", l)
		}
	})
}

func TestDescribeDetail(t *testing.T) {
	text := func(coretesting.Range) string { return "  if v > hi {  " }
	d := coretesting.Details{
		Declarations: []coretesting.DeclarationDetail{{Name: "Clamp", Executed: 6}, {Name: "Unused"}},
		Statements: []coretesting.StatementDetail{
			{Executed: 4},
			{Executed: 4, Branches: []coretesting.BranchDetail{{Executed: 0, Label: "v > hi"}, {Executed: 4}, {Executed: 1}}},
		},
	}
	cases := []struct {
		name string
		r    coretesting.DetailRange
		want string
	}{
		{"a declaration that ran", coretesting.DetailRange{Kind: coretesting.DetailDeclaration, Declaration: 0, Executed: 6}, "`Clamp` was executed 6 time(s)."},
		{"a declaration that did not", coretesting.DetailRange{Kind: coretesting.DetailDeclaration, Declaration: 1}, "`Unused` was not executed."},
		{"a statement", coretesting.DetailRange{Kind: coretesting.DetailStatement, Statement: 0, Executed: 4}, "`if v > hi {` was executed 4 time(s)."},
		{"a statement with branches", coretesting.DetailRange{Kind: coretesting.DetailStatement, Statement: 1, Executed: 4}, "2 of 3 of branches in `if v > hi {` were covered."},
		{"a branch that did not run", coretesting.DetailRange{Kind: coretesting.DetailBranch, Statement: 1, Branch: 0}, "Branch `v > hi` in `if v > hi {` was not covered."},
		{"a branch that ran", coretesting.DetailRange{Kind: coretesting.DetailBranch, Statement: 1, Branch: 2, Executed: 1}, "Branch #3 in `if v > hi {` was executed 1 time(s)."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := describeDetail(d, c.r, text); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestNextMissed(t *testing.T) {
	cases := []struct {
		name    string
		missed  []int
		from    int
		forward bool
		want    int
	}{
		{"none", nil, 3, true, -1},
		{"the next one after the cursor", []int{2, 8, 12}, 3, true, 8},
		{"not the line the cursor is on", []int{2, 8, 12}, 8, true, 12},
		{"going round at the end", []int{2, 8, 12}, 12, true, 2},
		{"the previous one", []int{2, 8, 12}, 9, false, 8},
		{"not the line the cursor is on, backwards", []int{2, 8, 12}, 8, false, 2},
		{"going round at the start", []int{2, 8, 12}, 2, false, 12},
		{"one line", []int{4}, 4, true, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nextMissed(c.missed, c.from, c.forward); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestACoverageRunOpensTheCoverageInTheViewAndTheEditor(t *testing.T) {
	r, e := coverageRig(t, testingOpts{})
	if _, ok := r.win.Coverage.Selected(); ok {
		t.Fatal("coverage is open before a run")
	}
	if r.win.Keys.Get("testing.isTestCoverageOpen") == true {
		t.Error("testing.isTestCoverageOpen is set before a run")
	}
	r.covered(e)
	sel, ok := r.win.Coverage.Selected()
	if !ok || len(sel.Coverage.Files()) != 2 {
		t.Fatalf("the open coverage is %+v", sel)
	}
	r.waitFor("the keys", func() bool {
		k := r.win.Keys
		return k.Get("testing.isTestCoverageOpen") == true && k.Get("testing.hasCoverageInFile") == true && k.Get("testing.hasInlineCoverageDetails") == true
	})
	if got := r.win.Keys.Get("testing.inlineCoverageEnabled"); got == true {
		t.Error("the inline coverage is on by default")
	}
	// the Testing container has the Test Coverage view now
	var ids []string
	for _, v := range r.win.Views.Registry().Views(command.ViewContainerTesting, r.win.Keys) {
		ids = append(ids, v.ID)
	}
	if !slices.Contains(ids, command.ViewTestCoverage) {
		t.Errorf("the views of the Testing container are %v, want %s in them", ids, command.ViewTestCoverage)
	}
	// the gutter has the lines: 4 covered, 8 and 9 covered, 11 covered by its branch, 12 not, 14 covered
	type mark struct {
		first, last int
		gutter      string
	}
	var got []mark
	for _, m := range e.view.CoverageMarks() {
		got = append(got, mark{m.Range.Start.Line, m.Range.End.Line, m.GutterID})
	}
	want := []mark{{3, 3, covGutterHit}, {7, 8, covGutterHit}, {10, 10, covGutterHit}, {11, 11, covGutterMiss}, {13, 13, covGutterHit}}
	if !slices.Equal(got, want) {
		t.Errorf("gutter marks %+v, want %+v", got, want)
	}
	if n := len(e.view.CoverageAnnotations()); n != 0 {
		t.Errorf("%d counts without the inline coverage", n)
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestTheInlineCoverageToggleAndItsKeys(t *testing.T) {
	r, e := coverageRig(t, testingOpts{})
	r.covered(e)
	before := len(e.view.CoverageMarks())
	r.run(command.TestingToggleInlineCoverage, nil)
	r.waitFor("the inline coverage", func() bool { return len(e.view.CoverageAnnotations()) > 0 })
	if !r.win.Coverage.ShowInline() || r.win.Keys.Get("testing.inlineCoverageEnabled") != true {
		t.Error("the toggle did not turn the inline coverage on")
	}
	if len(e.view.CoverageMarks()) <= before {
		t.Errorf("%d marks inline, want more than the %d of the gutter", len(e.view.CoverageMarks()), before)
	}
	notes := e.view.CoverageAnnotations()
	if got := notes[7]; !slices.Equal(got, []string{"6x", "1/2 branches"}) && !slices.Equal(got, []string{"6x"}) {
		t.Errorf("line 8 has the counts %q", got)
	}
	if got := notes[10]; !slices.Contains(got, "1/2 branches") {
		t.Errorf("line 11 has the counts %q, want its branches", got)
	}
	r.run(command.TestingToggleInlineCoverage, nil)
	r.waitFor("the gutter alone", func() bool { return len(e.view.CoverageAnnotations()) == 0 })
	if len(e.view.CoverageMarks()) != before {
		t.Errorf("%d marks after toggling back, want %d", len(e.view.CoverageMarks()), before)
	}
}

func TestClearCoverageTakesItOffEveryScreen(t *testing.T) {
	r, e := coverageRig(t, testingOpts{})
	r.covered(e)
	r.run(command.TestingCoverageClose, nil)
	r.waitFor("the editor without coverage", func() bool { return len(e.view.CoverageMarks()) == 0 })
	if _, ok := r.win.Coverage.Selected(); ok {
		t.Error("the coverage is still open")
	}
	r.waitFor("the keys", func() bool {
		return r.win.Keys.Get("testing.isTestCoverageOpen") != true && r.win.Keys.Get("testing.hasCoverageInFile") != true
	})
	if r.win.shell.testing.coverage.Model().Len() != 0 {
		t.Errorf("the view still has %d rows", r.win.shell.testing.coverage.Model().Len())
	}
	// Open Coverage brings the report of the last run back
	r.run(command.TestingOpenCoverage, nil)
	r.waitFor("the coverage again", func() bool { return len(e.view.CoverageMarks()) > 0 })
}

func TestGoToTheNextAndThePreviousUncoveredLine(t *testing.T) {
	r, e := coverageRig(t, testingOpts{})
	r.covered(e)
	line := func() int { return e.view.Cursors().Primary().Selection.Active.Line }
	e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{})})
	r.run(command.TestingCoverageNextMissed, nil)
	if line() != 11 {
		t.Errorf("Next Uncovered Line went to line %d, want 11 (counted from 0)", line())
	}
	r.run(command.TestingCoverageNextMissed, nil) // the only one: it goes round to itself
	if line() != 11 {
		t.Errorf("the next one went to %d", line())
	}
	e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 14})})
	r.run(command.TestingCoveragePreviousMissed, nil)
	if line() != 11 {
		t.Errorf("Previous Uncovered Line went to line %d, want 11", line())
	}
}

func TestTheCoverageToolbarShowsWithItsSetting(t *testing.T) {
	r, e := coverageRig(t, testingOpts{})
	r.covered(e)
	has := func() bool { return r.ts.Exists(gluttertest.ByKey(CoverageToolbarKey)) }
	if has() {
		t.Error("the toolbar shows with testing.coverageToolbarEnabled off, its default")
	}
	if r.win.Keys.Get("testing.coverageToolbarEnabled") == true {
		t.Error("testing.coverageToolbarEnabled is set by default")
	}
	r.run(command.TestingToggleCoverageToolbar, nil)
	r.waitFor("the toolbar", has)
	if !settings.Get[bool](r.win.Settings, "testing.coverageToolbarEnabled", "") {
		t.Error("the command did not set the setting")
	}
	r.run(command.TestingToggleCoverageToolbar, nil)
	r.waitFor("no toolbar", func() bool { return !has() })
}

func TestTheExplorerShowsThePercentagesAsDecorations(t *testing.T) {
	r, e := coverageRig(t, testingOpts{})
	uri := func(rel string) string { return explorerURI(filepath.Join(r.folder, rel)) }
	if _, ok := r.win.Decorations.Get(uri("calc.go"), false); ok {
		t.Fatal("a file has a decoration before the run")
	}
	r.covered(e)
	cases := []struct {
		name, rel, badge, color string
	}{
		// calc.go: 5 of 6 statements, 5 of 6 branches... worked out by the model: (4+2+2)/(6+4+2)? see the sums below
		{"a file", "pkg/util.go", "0%", "charts.red"},
	}
	for _, c := range cases {
		d, ok := r.win.Decorations.Get(uri(c.rel), false)
		if !ok || d.Badge != c.badge || !slices.Contains(d.Colors, c.color) {
			t.Errorf("%s: decoration %+v (%v), want badge %s in %s", c.name, d, ok, c.badge, c.color)
		}
	}
	sel, _ := r.win.Coverage.Selected()
	calc, _ := sel.Coverage.Computed(uri("calc.go"))
	want := coretesting.DisplayPercent(calc.TotalPercent(), 0)
	if d, ok := r.win.Decorations.Get(uri("calc.go"), false); !ok || d.Badge != want {
		t.Errorf("calc.go has %+v (%v), want the badge %s", d, ok, want)
	}
	// the folder is the sum of its files
	if d, ok := r.win.Decorations.Get(explorerURI(r.folder), false); !ok || d.Badge == "" {
		t.Errorf("the folder has %+v (%v), want its percentage", d, ok)
	}
	// testing.showCoverageInExplorer takes them away, and testing.displayedCoveragePercent changes them
	r.run(command.TestingToggleCoverageInExplorer, nil)
	r.waitFor("no decorations", func() bool { _, ok := r.win.Decorations.Get(uri("pkg/util.go"), false); return !ok })
	r.run(command.TestingToggleCoverageInExplorer, nil)
	r.waitFor("decorations again", func() bool { _, ok := r.win.Decorations.Get(uri("calc.go"), false); return ok })
	if err := r.win.Settings.Update(r.win.ctx, "testing.displayedCoveragePercent", "statement", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	stmt := coretesting.DisplayPercent(coretesting.ClampedPercent(calc.Statement), 0)
	r.waitFor("the statement percentage", func() bool { d, ok := r.win.Decorations.Get(uri("calc.go"), false); return ok && d.Badge == stmt })
	// the color follows testing.coverageBarThresholds
	if err := r.win.Settings.Update(r.win.ctx, "testing.coverageBarThresholds", map[string]any{"red": 0.0, "yellow": 10.0, "green": 20.0}, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the green", func() bool {
		d, _ := r.win.Decorations.Get(uri("calc.go"), false)
		return slices.Contains(d.Colors, "charts.green")
	})
}

func TestTheCoverageViewListsFilesAndDeclarationsAndSorts(t *testing.T) {
	r, e := coverageRig(t, testingOpts{})
	r.covered(e)
	cv := r.win.shell.testing.coverage
	r.waitFor("the rows", func() bool { return cv.Model().Len() > 0 })
	labels := func() []string {
		var out []string
		for _, n := range cv.Model().Rows() {
			out = append(out, n.Label)
		}
		return out
	}
	// the folder of the workspace, its file and the folder of the other, expanded
	if got := labels(); len(got) != 4 {
		t.Fatalf("rows %q, want the folder, calc.go, pkg and util.go", got)
	}
	file := cv.Model().Rows()[1]
	if file.Label != "calc.go" || !file.Expandable {
		t.Fatalf("the second row is %+v, want calc.go with declarations", file)
	}
	cv.Model().Expand(file.Key)
	r.waitFor("the declarations", func() bool { return len(labels()) == 6 })
	if got := labels(); got[2] != "Add" || got[3] != "Clamp" {
		t.Errorf("declarations by location: %q", got)
	}
	r.run(command.TestingCoverageCollapseAll, nil)
	if got := labels(); len(got) != 1 {
		t.Errorf("after Collapse All the rows are %q", got)
	}
	cv.Model().Expand(cv.Model().Rows()[0].Key)
	cv.SetSort(testview.CoverageByCoverage)
	rows := cv.Model().Rows()
	if len(rows) < 3 || rows[1].Label != "calc.go" {
		t.Errorf("by coverage the better file comes first: %+v", rows)
	}
	cv.SetSort(testview.CoverageByName)
	if cv.Sort() != testview.CoverageByName {
		t.Errorf("sort %s", cv.Sort())
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestOpeningARowOfTheCoverageViewOpensTheFileAtTheDeclaration(t *testing.T) {
	r, e := coverageRig(t, testingOpts{open: []string{"calc.go"}})
	r.covered(e)
	cv := r.win.shell.testing.coverage
	r.waitFor("the rows", func() bool { return cv.Model().Len() > 0 })
	cv.Model().Expand(cv.Model().Rows()[1].Key)
	r.waitFor("the declarations", func() bool { return cv.Model().Len() == 6 })
	var key string
	for _, n := range cv.Model().Rows() {
		if n.Label == "Clamp" {
			key = n.Key
		}
	}
	node, ok := cv.Node(key)
	if !ok || node.Range.Start.Line != 6 {
		t.Fatalf("the Clamp node is %+v (%v)", node, ok)
	}
	r.win.shell.coverageOpenNode(node)
	r.waitFor("the cursor at Clamp", func() bool {
		ed := r.win.shell.activeEditor()
		return ed != nil && ed.view.Cursors().Primary().Selection.Active.Line == 6
	})
}
