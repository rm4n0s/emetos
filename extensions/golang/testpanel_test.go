package golang

import (
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// The panel view "Go Tests" (step 14.3).

// loadJob shows a job made from the lines of a fixture in the panel of the rig, in the state, as if a run were there.
func loadJob(r *rig, fixtureName string, state int, status string, upTo string) *testJob {
	j := &testJob{
		req: testRequest{label: "internal/calc", what: "the package"}, command: "go test -json -timeout 30s", state: state, status: status,
		results: newTestResults(), seen: map[string]bool{}, cancel: func() {},
	}
	for _, l := range fixture(r.t, fixtureName, "/work/calc") {
		j.results.addLine(l)
		if upTo != "" && strings.Contains(l, upTo) {
			break
		}
	}
	if state != runRunning {
		j.results.finish()
	}
	r.ext.tests.job = j
	r.ext.tests.selectFirstFailure()
	r.ext.tests.refresh()
	return j
}

func TestGoldenTheTestsPanel(t *testing.T) {
	states := []struct {
		name  string
		setup func(r *rig)
	}{
		{"idle", func(*rig) {}},
		{"running", func(r *rig) { loadJob(r, "mixed.jsonl", runRunning, "", `"Test":"TestSkipped","Elapsed"`) }},
		{"passed", func(r *rig) { loadJob(r, "passing.jsonl", runExited, "exit status 0", "") }},
		{"failed", func(r *rig) { loadJob(r, "mixed.jsonl", runFailed, "exit status 1", "") }},
		{"build", func(r *rig) { loadJob(r, "build.jsonl", runFailed, "exit status 1", "") }},
	}
	for _, th := range uitest.Themes() {
		for _, st := range states {
			t.Run(th.Name+"/"+st.name, func(t *testing.T) {
				r := newRig(t, rigOptions{})
				st.setup(r)
				panel := r.ext.tests
				host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget { return panel.build(bc) })
				ts := uitest.Pump(t, th, gui.Size{Width: 760, Height: 260}, host)
				uitest.MatchesGolden(ts, "tests_"+st.name+"_"+th.Name)
			})
		}
	}
}

func TestTheTreeAndTheOutputOfThePanel(t *testing.T) {
	r := newRig(t, rigOptions{})
	loadJob(r, "mixed.jsonl", runFailed, "exit status 1", "")
	p := r.ext.tests
	rows := map[string]bool{}
	for _, n := range p.model.Rows() {
		rows[n.Key] = true
	}
	// the package is open and so is the table test that has a failure below it; the first failure is selected
	for _, key := range []string{"p:example.com/mixed", "t:example.com/mixed:TestAddPasses", "t:example.com/mixed:TestTable", "t:example.com/mixed:TestTable/wrong", "t:example.com/mixed:TestTable/small"} {
		if !rows[key] {
			t.Errorf("no row %s in %v", key, rows)
		}
	}
	if p.selected != "t:example.com/mixed:TestAddFails" {
		t.Errorf("selected = %q", p.selected)
	}
	if got := strings.Join(p.outputOf(), "\n"); !strings.Contains(got, "mixed_test.go:14: Add(2, 2) = 4, want 5") {
		t.Errorf("output = %q", got)
	}
	// a click on another test shows its output; on the package, the output of the package
	for key, want := range map[string]string{
		"t:example.com/mixed:TestTable/wrong": "mixed_test.go:29: Add(2, 3) = 5, want 6",
		"t:example.com/mixed:TestSkipped":     "needs a network",
		"p:example.com/mixed":                 "FAIL\texample.com/mixed",
	} {
		node, ok := p.model.Node(key)
		if !ok {
			t.Fatalf("no node %s", key)
		}
		p.onSelect(node)
		if got := strings.Join(p.outputOf(), "\n"); !strings.Contains(got, want) {
			t.Errorf("output of %s = %q, want %q", key, got, want)
		}
	}
	// nothing selected: the command and the lines go itself printed
	p.selected = ""
	p.job.results.stray = []string{"go: downloading x"}
	if got := strings.Join(p.outputOf(), "\n"); got != "$ go test -json -timeout 30s\ngo: downloading x" {
		t.Errorf("output = %q", got)
	}
	if s := p.job.summary(); s != "the package in internal/calc: 2 passed, 3 failed, 1 skipped" {
		t.Errorf("summary = %q", s)
	}
}

func TestExpandTabs(t *testing.T) {
	got := expandTabs([]string{"FAIL\texample.com/p\t0.002s", "ab\tc", "\tindent", "plain"})
	want := "FAIL    example.com/p   0.002s\nab      c\n        indent\nplain"
	if got != want {
		t.Errorf("expandTabs = %q, want %q", got, want)
	}
}

func TestTheSummaryOfARun(t *testing.T) {
	tests := []struct {
		fixture string
		state   int
		status  string
		want    string
	}{
		{"passing.jsonl", runExited, "", "the package in internal/calc: 2 passed"},
		{"build.jsonl", runFailed, "", "the package in internal/calc: build failed"},
		{"nofiles.jsonl", runExited, "", "the package in internal/calc: no tests"},
		{"passing.jsonl", runStopped, "", "Stopped the package in internal/calc: 2 passed"},
		{"passing.jsonl", runNotStarted, "boom", "Could not run the package: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.fixture+" "+tt.want, func(t *testing.T) {
			r := newRig(t, rigOptions{})
			j := loadJob(r, tt.fixture, tt.state, tt.status, "")
			if got := j.summary(); got != tt.want {
				t.Errorf("summary = %q, want %q", got, tt.want)
			}
		})
	}
	if got := formatElapsed(0.0123); got != "12 ms" {
		t.Errorf("elapsed = %q", got)
	}
	if got := formatElapsed(1.256); got != "1.26 s" {
		t.Errorf("elapsed = %q", got)
	}
}

func TestTheButtonsOfTheTestsPanel(t *testing.T) {
	r := testRig(t, "passing.jsonl")
	r.focus("sample_test.go", "§"+sampleTests)
	panel := r.ext.tests
	var rebuild widgets.StateSetter
	host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		rebuild = set
		return panel.build(bc)
	})
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 760, Height: 260}, host)
	// Test Package, before any run
	ts.Tap(gluttertest.ByText("Test Package"))
	first := r.testsEnded()
	if len(r.goTests()) != 1 {
		t.Fatalf("runs = %d", len(r.goTests()))
	}
	// Run Again after the run
	rebuild(func() {})
	ts.Pump()
	ts.Tap(gluttertest.ByText("Run Again"))
	r.api.WaitFor("the second run", func() bool { return len(r.goTests()) == 2 && panel.job != first && panel.job.state != runRunning })
	// Stop while one runs
	stopped := false
	panel.job = &testJob{req: testRequest{label: "."}, state: runRunning, cancel: func() { stopped = true }, results: newTestResults(), seen: map[string]bool{}}
	rebuild(func() {})
	ts.Pump()
	ts.Tap(gluttertest.ByText("Stop"))
	if !stopped || !panel.job.stopping {
		t.Errorf("stopped %v, stopping %v", stopped, panel.job.stopping)
	}
}
