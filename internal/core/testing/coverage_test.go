package testing_test

import (
	"context"
	"math"
	"testing"

	ct "github.com/rm4n0s/emetos/internal/core/testing"
)

func cnt(c, t int) *ct.Count { return &ct.Count{Covered: c, Total: t} }

func TestCoverageSums(t *testing.T) {
	file := func(uri string, st ct.Count, br, de *ct.Count) ct.FileCoverage {
		return ct.FileCoverage{URI: uri, Statement: st, Branch: br, Declaration: de}
	}
	cases := []struct {
		name   string
		add    []ct.FileCoverage
		uri    string // the node to look at
		want   ct.FileCoverage
		files  int
		total  ct.FileCoverage
		tpc    float64
		wantOK bool
	}{
		{
			name:  "a folder is the sum of its files",
			add:   []ct.FileCoverage{file("file:///w/a/x.go", ct.Count{Covered: 3, Total: 4}, nil, nil), file("file:///w/a/y.go", ct.Count{Covered: 1, Total: 6}, nil, nil)},
			uri:   "file:///w/a",
			want:  ct.FileCoverage{Statement: ct.Count{Covered: 4, Total: 10}},
			files: 2, total: ct.FileCoverage{Statement: ct.Count{Covered: 4, Total: 10}}, tpc: 0.4, wantOK: true,
		},
		{
			name: "branches and declarations are summed where they exist",
			add: []ct.FileCoverage{
				file("file:///w/a/x.go", ct.Count{Covered: 2, Total: 2}, cnt(1, 2), nil),
				file("file:///w/a/y.go", ct.Count{Covered: 0, Total: 2}, nil, cnt(1, 3)),
				file("file:///w/b/z.go", ct.Count{Covered: 1, Total: 1}, cnt(2, 2), cnt(1, 1)),
			},
			uri:   "file:///w/a",
			want:  ct.FileCoverage{Statement: ct.Count{Covered: 2, Total: 4}, Branch: cnt(1, 2), Declaration: cnt(1, 3)},
			files: 3,
			total: ct.FileCoverage{Statement: ct.Count{Covered: 3, Total: 5}, Branch: cnt(3, 4), Declaration: cnt(2, 4)},
			tpc:   float64(3+3+2) / float64(5+4+4), wantOK: true,
		},
		{
			name: "a file reported again replaces its numbers in the sums",
			add: []ct.FileCoverage{
				file("file:///w/a/x.go", ct.Count{Covered: 1, Total: 4}, cnt(0, 2), nil),
				file("file:///w/a/y.go", ct.Count{Covered: 2, Total: 2}, nil, nil),
				file("file:///w/a/x.go", ct.Count{Covered: 4, Total: 4}, cnt(2, 2), nil),
			},
			uri:   "file:///w/a",
			want:  ct.FileCoverage{Statement: ct.Count{Covered: 6, Total: 6}, Branch: cnt(2, 2)},
			files: 2, total: ct.FileCoverage{Statement: ct.Count{Covered: 6, Total: 6}, Branch: cnt(2, 2)}, tpc: 1, wantOK: true,
		},
		{
			name:  "a file is its own numbers",
			add:   []ct.FileCoverage{file("file:///w/a/x.go", ct.Count{Covered: 1, Total: 4}, nil, nil)},
			uri:   "file:///w/a/x.go",
			want:  ct.FileCoverage{Statement: ct.Count{Covered: 1, Total: 4}},
			files: 1, total: ct.FileCoverage{Statement: ct.Count{Covered: 1, Total: 4}}, tpc: 0.25, wantOK: true,
		},
		{name: "a file that is not there", add: []ct.FileCoverage{file("file:///w/a/x.go", ct.Count{Covered: 1, Total: 4}, nil, nil)}, uri: "file:///w/zzz", files: 1,
			total: ct.FileCoverage{Statement: ct.Count{Covered: 1, Total: 4}}},
		{name: "a file with nothing to cover is fully covered", add: []ct.FileCoverage{file("file:///w/e.go", ct.Count{}, nil, nil)}, uri: "file:///w/e.go",
			files: 1, tpc: 1, wantOK: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			res := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupCoverage}})
			run, _ := res.AddTask(ct.TaskSpec{ControllerID: "c"})
			for _, f := range c.add {
				run.AddCoverage(f)
			}
			cov := res.Coverage(run.TaskID())
			if cov == nil || res.LatestCoverage() != cov {
				t.Fatal("no coverage")
			}
			got, ok := cov.Computed(c.uri)
			if ok != c.wantOK {
				t.Fatalf("Computed ok = %v", ok)
			}
			if ok {
				got.ID, got.URI = "", ""
				if got.Statement != c.want.Statement || !sameCount(got.Branch, c.want.Branch) || !sameCount(got.Declaration, c.want.Declaration) {
					t.Errorf("computed %+v %v %v, want %+v %v %v", got.Statement, got.Branch, got.Declaration, c.want.Statement, c.want.Branch, c.want.Declaration)
				}
				if c.tpc != 0 && math.Abs(got.TotalPercent()-c.tpc) > 1e-9 && c.uri != "file:///w/a" {
					t.Errorf("total percent %v, want %v", got.TotalPercent(), c.tpc)
				}
			}
			if n := len(cov.Files()); n != c.files {
				t.Errorf("%d files, want %d", n, c.files)
			}
			total, any := cov.Total()
			if any != (c.files > 0) || total.Statement != c.total.Statement || !sameCount(total.Branch, c.total.Branch) || !sameCount(total.Declaration, c.total.Declaration) {
				t.Errorf("total %+v %v %v", total.Statement, total.Branch, total.Declaration)
			}
			if c.files > 0 && c.tpc != 0 && c.uri == "file:///w/a" {
				if p := total.TotalPercent(); math.Abs(p-c.tpc) > 1e-9 {
					t.Errorf("total percent %v, want %v", p, c.tpc)
				}
			}
		})
	}
}

func sameCount(a, b *ct.Count) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func TestCountPercentAndTotals(t *testing.T) {
	if (ct.Count{}).Percent() != 1 || (ct.Count{Covered: 1, Total: 4}).Percent() != 0.25 {
		t.Error("Count.Percent")
	}
	d := ct.Details{
		Statements: []ct.StatementDetail{
			{Executed: 2, Branches: []ct.BranchDetail{{Executed: 1}, {Executed: 0}}},
			{Executed: 0}, {Executed: 5},
		},
		Declarations: []ct.DeclarationDetail{{Name: "f", Executed: 1}, {Name: "g"}},
	}
	got := ct.TotalsFromDetails("file:///w/a.go", d)
	if got.Statement != (ct.Count{Covered: 2, Total: 3}) || !sameCount(got.Branch, cnt(1, 2)) || !sameCount(got.Declaration, cnt(1, 2)) {
		t.Errorf("totals %+v %v %v", got.Statement, got.Branch, got.Declaration)
	}
}

func TestCoverageTreeShape(t *testing.T) {
	c := ct.NewTestCoverage(nil)
	c.Append(ct.FileCoverage{URI: "file:///w/a/x.go", Statement: ct.Count{Covered: 1, Total: 2}})
	c.Append(ct.FileCoverage{URI: "file:///w/b.go", Statement: ct.Count{Covered: 2, Total: 2}})
	tree := c.Tree()
	if len(tree) != 1 || tree[0].Name != "file" {
		t.Fatalf("tree %+v", tree)
	}
	var names []string
	var walk func(n ct.CoverageNode, depth int)
	walk = func(n ct.CoverageNode, depth int) {
		names = append(names, n.Name)
		for _, ch := range n.Children {
			walk(ch, depth+1)
		}
	}
	walk(tree[0], 0)
	want := []string{"file", "", "", "w", "a", "x.go", "b.go"}
	if len(names) != len(want) {
		t.Fatalf("names %q", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names %q, want %q", names, want)
			break
		}
	}
}

func TestCoverageDetailsAreLoadedOnceAndErrors(t *testing.T) {
	r := newRig(t)
	loads := 0
	fail := false
	loader := func(ctx context.Context, f ct.FileCoverage) (ct.Details, error) {
		loads++
		if fail {
			return ct.Details{}, plainError("no")
		}
		line, _ := f.Data.(int)
		return ct.Details{Statements: []ct.StatementDetail{{Executed: 1, Location: ct.Range{Start: ct.Pos{Line: line}}}}}, nil
	}
	res := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupCoverage}})
	run, _ := res.AddTask(ct.TaskSpec{ControllerID: "c", LoadCoverage: loader})
	run.AddCoverage(ct.FileCoverage{URI: "file:///w/a.go", Statement: ct.Count{Covered: 1, Total: 1}, Data: 7})
	cov := res.LatestCoverage()
	for i := 0; i < 2; i++ {
		d, err := cov.Details(context.Background(), "file:///w/a.go")
		if err != nil || len(d.Statements) != 1 || d.Statements[0].Location.Start.Line != 7 {
			t.Fatalf("details %+v %v", d, err)
		}
	}
	if loads != 1 {
		t.Errorf("%d loads, want 1", loads)
	}
	run.AddCoverage(ct.FileCoverage{URI: "file:///w/a.go", Statement: ct.Count{Covered: 1, Total: 1}, Data: 8})
	d, err := cov.Details(context.Background(), "file:///w/a.go")
	if err != nil || d.Statements[0].Location.Start.Line != 8 || loads != 2 {
		t.Errorf("a file reported again keeps its old details: %+v %v %d", d, err, loads)
	}
	_, err = cov.Details(context.Background(), "file:///w")
	routeOf(t, err, "Coverage.Details.TestingCoverageUnknownFile")
	_, err = cov.Details(context.Background(), "file:///w/zzz.go")
	routeOf(t, err, "Coverage.Details.TestingCoverageUnknownFile")

	fail = true
	run.AddCoverage(ct.FileCoverage{URI: "file:///w/b.go", Statement: ct.Count{Covered: 1, Total: 1}})
	if _, err = cov.Details(context.Background(), "file:///w/b.go"); err == nil || err.Error() != "no" {
		t.Errorf("the loader's error was not returned: %v", err)
	}
	fail = false
	if _, err = cov.Details(context.Background(), "file:///w/b.go"); err != nil {
		t.Errorf("a failed load was kept: %v", err)
	}

	res2 := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupCoverage}})
	run2, _ := res2.AddTask(ct.TaskSpec{ControllerID: "c"})
	run2.AddCoverage(ct.FileCoverage{URI: "file:///w/a.go"})
	_, err = res2.LatestCoverage().Details(context.Background(), "file:///w/a.go")
	routeOf(t, err, "Coverage.Details.TestingCoverageNoLoader")
}
