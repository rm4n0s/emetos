package testing

import (
	"slices"
	"strings"
	"testing"
	"time"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
)

// Test Results (step 26.3): the nodes of the runs, their tests and their messages, and the text beside the tree. testResultsTree.ts needs the
// workbench's tree and has no unit test that runs without it: 0 ported.

// failingRun makes a result with a passing test, a failing one with a message of every kind and output that belongs to it.
func (m *model) failingRun() *coretesting.Result {
	m.t.Helper()
	res := m.svc.StartExternalRun(coretesting.ExternalRun{Request: coretesting.RunRequest{Group: coretesting.GroupRun}, Persist: true})
	run, err := res.AddTask(coretesting.TaskSpec{Name: "t", ControllerID: "c"})
	if err != nil {
		m.t.Fatal(err)
	}
	add := cid("a_test.go", "Add")
	sub := cid("a_test.go", "Sub")
	run.Ensure(add)
	run.SetState(add, coretesting.StatePassed, 3*time.Millisecond)
	run.AppendOutput([]byte("before\n"), nil, "")
	run.Ensure(sub)
	run.AppendOutput([]byte("\x1b[31mFAIL\x1b[0m Sub\nsecond line\n"), nil, sub)
	run.AddMessage(sub, coretesting.Message{
		Type: coretesting.MessageError, Text: "Sub(3, 1) = 3, want 2",
		Expected: "2", Actual: "3", HasDiff: true,
		StackTrace: []coretesting.StackFrame{{Label: "calc.TestSub", URI: "file:///w/a_test.go", Pos: &coretesting.Pos{Line: 11, Col: 1}}, {Label: "testing.tRunner"}},
	})
	run.SetState(sub, coretesting.StateFailed, 12*time.Millisecond)
	run.End()
	return res
}

func (m *model) results(t *testing.T) *Results {
	t.Helper()
	r := NewResults(ResultsOpts{Service: m.svc, Post: func(f func()) { f() }})
	t.Cleanup(r.Dispose)
	return r
}

func rowLabels(r *Results) []string {
	var out []string
	for _, n := range r.Model().Rows() {
		out = append(out, strings.Repeat(" ", n.Depth)+n.Label)
	}
	return out
}

func TestResultsHasTheRunsTheirTestsAndTheMessages(t *testing.T) {
	m := newModel(t)
	r := m.results(t)
	if got := len(r.Model().Rows()); got != 0 {
		t.Fatalf("%d rows before any run", got)
	}
	m.failingRun()
	got := rowLabels(r)
	// the run, the tests that took part (a group above them), and the message of the failure under its test
	if len(got) != 5 || !strings.HasPrefix(got[0], "Test run at ") || got[1] != " a_test.go" || got[2] != "  Add" || got[3] != "  Sub" || got[4] != "   Sub(3, 1) = 3, want 2" {
		t.Errorf("rows %q", got)
	}
}

func TestResultsShowsTheNewestRunFirstAndOpensTheFailures(t *testing.T) {
	m := newModel(t)
	r := m.results(t)
	first := m.failingRun()
	second := m.result(map[string]coretesting.State{"a_test.go/Add": coretesting.StatePassed}, nil)
	rows := r.Model().Rows()
	if rows[0].Key != ResultKey(second) {
		t.Errorf("the first row is %q, want the newest run", rows[0].Key)
	}
	if !r.Model().IsExpanded(TestKey(first, cid("a_test.go", "Sub"))) {
		t.Error("the failed test is not open to show its message")
	}
	if n, ok := r.Selected(); !ok || n.Result != second {
		t.Errorf("selected %+v, want the newest run", n)
	}
	r.ShowLatest()
	if r.selected != ResultKey(second) {
		t.Errorf("ShowLatest selected %q", r.selected)
	}
	if !r.Select(ResultKey(first)) || r.selected != ResultKey(first) {
		t.Error("the older run cannot be selected")
	}
	if r.Select("r:none") {
		t.Error("a node that is not there was selected")
	}
}

func TestTheTextOfANode(t *testing.T) {
	m := newModel(t)
	r := m.results(t)
	res := m.failingRun()
	sub := cid("a_test.go", "Sub")
	tests := []struct {
		name string
		key  string
		want []string
	}{
		{"the run: all its output", ResultKey(res), []string{"before\n", "\x1b[31mFAIL\x1b[0m Sub"}},
		{"a test: its messages and its own output", TestKey(res, sub), []string{"Sub(3, 1) = 3, want 2", "\x1b[31mFAIL\x1b[0m Sub\nsecond line\n"}},
		{"a test that passed prints nothing of its own", TestKey(res, cid("a_test.go", "Add")), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := r.Node(tc.key)
			if !ok {
				t.Fatalf("no node %q in %q", tc.key, rowLabels(r))
			}
			text := NodeText(n, nil)
			for _, w := range tc.want {
				if !strings.Contains(text, w) {
					t.Errorf("the text %q has no %q", text, w)
				}
			}
			if tc.want == nil && strings.TrimSpace(text) != "" {
				t.Errorf("the text is %q, want none", text)
			}
		})
	}
	// the run's output is the whole of it, and the test's only what its messages name
	n, _ := r.Node(TestKey(res, sub))
	if strings.Contains(NodeText(n, nil), "before") {
		t.Error("the output of another test is in the text of Sub")
	}
	var msg *ResultNode
	for _, k := range []string{MessageNodeKey(res, sub, 0), MessageNodeKey(res, sub, 1)} {
		if x, ok := r.Node(k); ok && x.Kind == NodeMessage {
			msg = x
		}
	}
	if msg == nil {
		t.Fatalf("no message node in %q", rowLabels(r))
	}
	text := NodeText(msg, func(p string) string { return "w" + strings.TrimPrefix(p, "/w") })
	for _, want := range []string{"Sub(3, 1) = 3, want 2", "Expected", "Actual", "Stack", "calc.TestSub  w/a_test.go:12:2", "testing.tRunner"} {
		if !strings.Contains(text, want) {
			t.Errorf("the text of the message has no %q: %q", want, text)
		}
	}
}

func TestTestOutputCutsTheOutputByTheOffsetsOfTheMessages(t *testing.T) {
	m := newModel(t)
	res := m.failingRun()
	tr, ok := res.Test(cid("a_test.go", "Sub"))
	if !ok {
		t.Fatal("no test")
	}
	if got := TestOutput(res, tr); got != "\x1b[31mFAIL\x1b[0m Sub\nsecond line\n" {
		t.Errorf("the output of Sub is %q", got)
	}
	if got := RunOutput(res); got != "before\n\x1b[31mFAIL\x1b[0m Sub\nsecond line\n" {
		t.Errorf("the output of the run is %q", got)
	}
}

func TestResultsFollowsTheServiceAndTellsTheListeners(t *testing.T) {
	m := newModel(t)
	r := m.results(t)
	n := 0
	stop := r.OnChange(func() { n++ })
	m.failingRun()
	if n == 0 {
		t.Error("the listener was not told of a new run")
	}
	seen := n
	stop()
	m.svc.ClearResults()
	if n != seen {
		t.Error("a listener that was stopped was told")
	}
	if got := len(r.Model().Rows()); got != 0 {
		t.Errorf("%d rows after the results were cleared", got)
	}
	if !slices.Equal(rowLabels(r), nil) {
		t.Errorf("rows %q", rowLabels(r))
	}
}
