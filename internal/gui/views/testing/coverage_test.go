package testing

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
)

// The Test Coverage view (step 26.4): the tree of files and folders from the open coverage, its sort orders, and the declarations of a file
// with the coverage of the statements inside each. testCoverageView.ts needs the workbench's tree: 0 ported.

// coverageModel is a service with a coverage service over it and a view that follows it, with calls run at once.
type coverageModel struct {
	t    *testing.T
	svc  *coretesting.Service
	cs   *coretesting.CoverageService
	view *Coverage

	mu     sync.Mutex
	posted []func()
}

// post queues a function for the UI thread, which is the goroutine of the test: drain runs the queue.
func (c *coverageModel) post(f func()) {
	c.mu.Lock()
	c.posted = append(c.posted, f)
	c.mu.Unlock()
}

// drain runs what was posted, and what that posts, until the queue stays empty for a moment.
func (c *coverageModel) drain() {
	for idle := 0; idle < 20; {
		c.mu.Lock()
		q := c.posted
		c.posted = nil
		c.mu.Unlock()
		if len(q) == 0 {
			idle++
			time.Sleep(time.Millisecond)
			continue
		}
		idle = 0
		for _, f := range q {
			f()
		}
	}
}

func rg(l1, c1, l2, c2 int) coretesting.Range {
	return coretesting.Range{Start: coretesting.Pos{Line: l1, Col: c1}, End: coretesting.Pos{Line: l2, Col: c2}}
}

func newCoverageModel(t *testing.T) *coverageModel {
	t.Helper()
	m := newModel(t)
	cs := coretesting.NewCoverageService(m.svc)
	t.Cleanup(cs.Dispose)
	c := &coverageModel{t: t, svc: m.svc, cs: cs}
	c.view = NewCoverage(CoverageOpts{Ctx: context.Background(), Service: cs, Post: c.post})
	t.Cleanup(c.view.Dispose)
	return c
}

// report completes a result that reports the files; the loader gives the details by the URI of the file.
func (c *coverageModel) report(details map[string]coretesting.Details, files ...coretesting.FileCoverage) {
	c.t.Helper()
	loader := func(_ context.Context, f coretesting.FileCoverage) (coretesting.Details, error) {
		return details[f.URI], nil
	}
	res := c.svc.StartExternalRun(coretesting.ExternalRun{Request: coretesting.RunRequest{Group: coretesting.GroupCoverage}})
	run, err := res.AddTask(coretesting.TaskSpec{ControllerID: "c", LoadCoverage: loader})
	if err != nil {
		c.t.Fatal(err)
	}
	for _, f := range files {
		run.AddCoverage(f)
	}
	run.End()
	c.drain()
}

func file(uri string, covered, total int) coretesting.FileCoverage {
	return coretesting.FileCoverage{URI: uri, Statement: coretesting.Count{Covered: covered, Total: total}}
}

func labelsOf(v *Coverage) []string {
	var out []string
	for _, n := range v.Model().Rows() {
		out = append(out, strings.Repeat(" ", n.Depth)+n.Label)
	}
	return out
}

func TestCoverageViewStartsAtTheFirstFolderWithSeveralEntriesAndCompressesChains(t *testing.T) {
	c := newCoverageModel(t)
	if len(c.view.Model().Rows()) != 0 {
		t.Fatal("rows before any coverage")
	}
	c.report(nil,
		file("file:///w/proj/a/x.go", 1, 2), file("file:///w/proj/a/y.go", 2, 2),
		file("file:///w/proj/b/c/d/z.go", 0, 4), file("file:///w/proj/top.go", 3, 4))
	got := labelsOf(c.view)
	// the top is the folder of the project (w/proj has the entries a, b/c/d and top.go), the chain b/c/d is one row
	want := []string{"proj", " a", "  x.go", "  y.go", " b/c/d", "  z.go", " top.go"}
	if !slices.Equal(got, want) {
		t.Errorf("rows %q, want %q", got, want)
	}
	top := c.view.Model().Rows()[0]
	if n, ok := c.view.Node(top.Key); !ok || n.Kind != CoverageFolder || n.Coverage.Statement != (coretesting.Count{Covered: 6, Total: 12}) {
		t.Errorf("the top is %+v, want the folder with the sum of the files", n)
	}
}

func TestCoverageViewTopLabelFollowsTheWindow(t *testing.T) {
	m := newModel(t)
	cs := coretesting.NewCoverageService(m.svc)
	defer cs.Dispose()
	c := &coverageModel{t: t, svc: m.svc, cs: cs}
	v := NewCoverage(CoverageOpts{Service: cs, Post: c.post, Top: func(p string) string { return "<" + p + ">" }})
	defer v.Dispose()
	c.view = v
	c.report(nil, file("file:///w/a.go", 1, 1), file("file:///w/b.go", 1, 1))
	if got := labelsOf(v); got[0] != "</w>" {
		t.Errorf("rows %q", got)
	}
}

func TestCoverageViewSortOrders(t *testing.T) {
	c := newCoverageModel(t)
	c.report(nil, file("file:///w/p/b.go", 1, 4), file("file:///w/p/a.go", 2, 4), file("file:///w/p/c.go", 4, 4))
	cases := []struct {
		sort CoverageSort
		want []string
	}{
		{CoverageByLocation, []string{"p", " a.go", " b.go", " c.go"}},
		{CoverageByName, []string{"p", " a.go", " b.go", " c.go"}},
		{CoverageByCoverage, []string{"p", " c.go", " a.go", " b.go"}}, // the best first
	}
	for _, tc := range cases {
		t.Run(string(tc.sort), func(t *testing.T) {
			c.view.SetSort(tc.sort)
			if got := labelsOf(c.view); !slices.Equal(got, tc.want) {
				t.Errorf("rows %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCoverageViewDeclarationsAreNestedAndHaveTheCoverageOfTheirStatements(t *testing.T) {
	c := newCoverageModel(t)
	uri := "file:///w/p/a.go"
	details := coretesting.Details{
		Declarations: []coretesting.DeclarationDetail{
			{Name: "outer", Executed: 2, Location: rg(0, 0, 9, 1)},
			{Name: "inner", Executed: 0, Location: rg(2, 1, 4, 2)},
			{Name: "other", Executed: 5, Location: rg(11, 0, 14, 1)},
		},
		Statements: []coretesting.StatementDetail{
			{Executed: 2, Location: rg(1, 1, 1, 9)},
			{Executed: 0, Location: rg(3, 2, 3, 9), Branches: []coretesting.BranchDetail{{Executed: 1}, {Executed: 0}}},
			{Executed: 5, Location: rg(12, 1, 12, 9)},
		},
	}
	fc := file(uri, 2, 3)
	fc.Declaration = &coretesting.Count{Covered: 2, Total: 3}
	c.report(map[string]coretesting.Details{uri: details}, fc, file("file:///w/p/b.go", 1, 1))
	rows := c.view.Model().Rows()
	var fileRow string
	for _, r := range rows {
		if r.Label == "a.go" {
			fileRow = r.Key
			if !r.Expandable {
				t.Fatal("a file with declarations cannot be expanded")
			}
		}
		if r.Label == "b.go" && r.Expandable {
			t.Error("a file with no declarations can be expanded")
		}
	}
	c.view.Model().Expand(fileRow)
	c.drain() // the load runs in the background and posts its answer
	c.view.Model().ExpandAll()
	got := labelsOf(c.view)
	want := []string{"p", " a.go", "  outer", "   inner", "  other", " b.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("rows %q, want %q", got, want)
	}
	outer, _ := c.view.Node(fileRow + "#0")
	inner, _ := c.view.Node(fileRow + "#1")
	other, _ := c.view.Node(fileRow + "#2")
	// outer holds the statements inside it (inner's too, not other's): 1 of 2 statements, 1 of 2 branches
	if outer.Attr == nil || outer.Attr.Statement != (coretesting.Count{Covered: 1, Total: 2}) || outer.Attr.Branch == nil || *outer.Attr.Branch != (coretesting.Count{Covered: 1, Total: 2}) {
		t.Errorf("outer: %+v", outer.Attr)
	}
	if inner.Attr != nil {
		t.Errorf("a declaration that did not run has the coverage %+v, want none", inner.Attr)
	}
	if other.Attr == nil || other.Attr.Statement != (coretesting.Count{Covered: 1, Total: 1}) || other.TotalPercent() != 1 {
		t.Errorf("other: %+v", other.Attr)
	}
	if inner.TotalPercent() != -1 {
		t.Errorf("inner has the percent %v, want none", inner.TotalPercent())
	}
	// by name
	c.view.SetSort(CoverageByName)
	c.drain()
	c.view.Model().ExpandAll()
	if got := labelsOf(c.view); !slices.Contains(got, "  other") {
		t.Errorf("by name: %q", got)
	}
}

func TestCoverageViewFollowsClosingTheCoverage(t *testing.T) {
	c := newCoverageModel(t)
	c.report(nil, file("file:///w/p/a.go", 1, 2), file("file:///w/p/b.go", 1, 2))
	if len(c.view.Model().Rows()) == 0 {
		t.Fatal("no rows")
	}
	changes := 0
	stop := c.view.OnChange(func() { changes++ })
	defer stop()
	c.cs.Close()
	c.drain()
	if len(c.view.Model().Rows()) != 0 || changes == 0 {
		t.Errorf("%d rows and %d changes after Close", len(c.view.Model().Rows()), changes)
	}
}

func TestBarsTooltipAndTheBarsSource(t *testing.T) {
	src := BarSource{Statement: coretesting.Count{Covered: 1234, Total: 2000}, Branch: &coretesting.Count{Covered: 1, Total: 3}, Declaration: &coretesting.Count{Covered: 0, Total: 0}}
	want := "1,234/2,000 statements covered (61.70%)\n\n0/0 functions covered (100.00%)\n\n1/3 branches covered (33.33%)"
	if got := BarsTooltip(src); got != want {
		t.Errorf("tooltip %q, want %q", got, want)
	}
	if got := BarsTooltip(BarSource{Statement: coretesting.Count{Covered: 1, Total: 2}}); got != "1/2 statements covered (50.00%)" {
		t.Errorf("tooltip %q", got)
	}
	f := coretesting.FileCoverage{Statement: coretesting.Count{Covered: 1, Total: 2}, Branch: &coretesting.Count{Covered: 1, Total: 1}}
	if b := BarsOf(f); b.Statement != f.Statement || b.Branch != f.Branch || b.Declaration != nil {
		t.Errorf("bars %+v", b)
	}
}
