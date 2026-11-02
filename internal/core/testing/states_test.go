package testing_test

import (
	"context"
	"testing"
	"time"

	ct "github.com/rm4n0s/emetos/internal/core/testing"
)

func TestMaxPriority(t *testing.T) {
	cases := []struct {
		name   string
		states []ct.State
		want   ct.State
	}{
		{"none", nil, ct.StateUnset},
		{"one", []ct.State{ct.StatePassed}, ct.StatePassed},
		{"running over everything", []ct.State{ct.StateErrored, ct.StateRunning, ct.StateFailed}, ct.StateRunning},
		{"errored over failed", []ct.State{ct.StateFailed, ct.StateErrored, ct.StatePassed}, ct.StateErrored},
		{"failed over queued", []ct.State{ct.StateQueued, ct.StateFailed}, ct.StateFailed},
		{"queued over passed", []ct.State{ct.StatePassed, ct.StateQueued}, ct.StateQueued},
		{"passed over skipped", []ct.State{ct.StateSkipped, ct.StatePassed}, ct.StatePassed},
		{"skipped over unset", []ct.State{ct.StateUnset, ct.StateSkipped}, ct.StateSkipped},
		{"unset alone", []ct.State{ct.StateUnset, ct.StateUnset}, ct.StateUnset},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ct.MaxPriority(c.states...); got != c.want {
				t.Errorf("MaxPriority(%v) = %v, want %v", c.states, got, c.want)
			}
		})
	}
}

func TestStateValuesAreVSCodes(t *testing.T) {
	want := map[ct.State]int{ct.StateUnset: 0, ct.StateQueued: 1, ct.StateRunning: 2, ct.StatePassed: 3, ct.StateFailed: 4, ct.StateSkipped: 5, ct.StateErrored: 6}
	for s, n := range want {
		if int(s) != n {
			t.Errorf("%v = %d, want %d", s, int(s), n)
		}
	}
	if int(ct.GroupRun) != 2 || int(ct.GroupDebug) != 4 || int(ct.GroupCoverage) != 8 {
		t.Error("the groups are not VS Code's TestRunProfileBitset values")
	}
}

type step struct {
	id    string
	state ct.State
	dur   time.Duration
	task  int // the task that reports (0 or 1)
}

type want struct {
	state, own ct.State
	dur        time.Duration
}

// TestStatesOfATree is a table of reports to the states a tree shows: the precedence, the parents, the terminal states that stay and
// the durations.
func TestStatesOfATree(t *testing.T) {
	none := ct.NoDuration
	ms := time.Millisecond
	cases := []struct {
		name   string
		steps  []step
		end    bool // end the tasks at the end
		want   map[string]want
		counts map[ct.State]int
	}{
		{
			name:  "a failed child fails its parents, a passed one does not hide it",
			steps: []step{{id(pkg, "a"), ct.StatePassed, 5 * ms, 0}, {id(pkg, "b"), ct.StateFailed, 7 * ms, 0}},
			want: map[string]want{
				id(pkg, "a"): {ct.StatePassed, ct.StatePassed, 5 * ms}, id(pkg, "b"): {ct.StateFailed, ct.StateFailed, 7 * ms},
				id(pkg): {ct.StateFailed, ct.StateUnset, 12 * ms},
			},
			counts: map[ct.State]int{ct.StatePassed: 1, ct.StateFailed: 1, ct.StateUnset: 1},
		},
		{
			name:  "running beats an error, an error beats a failure",
			steps: []step{{id(pkg, "a"), ct.StateErrored, none, 0}, {id(pkg, "b"), ct.StateRunning, none, 0}, {id(pkg, sub, "x"), ct.StateFailed, none, 0}},
			want: map[string]want{
				id(pkg): {ct.StateRunning, ct.StateUnset, none}, id(pkg, sub): {ct.StateFailed, ct.StateUnset, none},
			},
		},
		{
			name:  "queued beats passed",
			steps: []step{{id(pkg, "a"), ct.StatePassed, none, 0}, {id(pkg, "b"), ct.StateQueued, none, 0}},
			want:  map[string]want{id(pkg): {ct.StateQueued, ct.StateUnset, none}},
		},
		{
			name:  "children that are all skipped make the parent skipped",
			steps: []step{{id(pkg, "a"), ct.StateSkipped, none, 0}, {id(pkg, "b"), ct.StateSkipped, none, 0}},
			want:  map[string]want{id(pkg): {ct.StateSkipped, ct.StateUnset, none}},
		},
		{
			name:  "a skipped child does not hide a passed one",
			steps: []step{{id(pkg, "a"), ct.StateSkipped, none, 0}, {id(pkg, "b"), ct.StatePassed, none, 0}},
			want:  map[string]want{id(pkg): {ct.StatePassed, ct.StateUnset, none}},
		},
		{
			name:  "the parent follows the child down: failed, then queued is refused, so the failure stays",
			steps: []step{{id(pkg, "a"), ct.StateFailed, none, 0}, {id(pkg, "a"), ct.StateQueued, none, 0}, {id(pkg, "a"), ct.StateRunning, none, 0}},
			want:  map[string]want{id(pkg, "a"): {ct.StateFailed, ct.StateFailed, none}, id(pkg): {ct.StateFailed, ct.StateUnset, none}},
		},
		{
			name:  "a passed test may fail later, a failed one may not pass",
			steps: []step{{id(pkg, "a"), ct.StatePassed, none, 0}, {id(pkg, "a"), ct.StateFailed, none, 0}, {id(pkg, "a"), ct.StatePassed, none, 0}},
			want:  map[string]want{id(pkg, "a"): {ct.StateFailed, ct.StateFailed, none}},
		},
		{
			name:  "a skipped test does not become passed",
			steps: []step{{id(pkg, "a"), ct.StateSkipped, none, 0}, {id(pkg, "a"), ct.StatePassed, none, 0}},
			want:  map[string]want{id(pkg, "a"): {ct.StateSkipped, ct.StateSkipped, none}},
		},
		{
			name:  "an errored test stays errored",
			steps: []step{{id(pkg, "a"), ct.StateErrored, none, 0}, {id(pkg, "a"), ct.StateFailed, none, 0}},
			want:  map[string]want{id(pkg, "a"): {ct.StateErrored, ct.StateErrored, none}},
		},
		{
			name:  "two tasks: the test shows the worse of its states and the longest duration",
			steps: []step{{id(pkg, "a"), ct.StatePassed, 3 * ms, 0}, {id(pkg, "a"), ct.StateFailed, 9 * ms, 1}},
			want:  map[string]want{id(pkg, "a"): {ct.StateFailed, ct.StateFailed, 9 * ms}, id(pkg): {ct.StateFailed, ct.StateUnset, 9 * ms}},
		},
		{
			name:  "the duration of a parent that has its own is its own",
			steps: []step{{id(pkg, "a"), ct.StatePassed, 4 * ms, 0}, {id(pkg), ct.StatePassed, 100 * ms, 0}},
			want:  map[string]want{id(pkg): {ct.StatePassed, ct.StatePassed, 100 * ms}},
		},
		{
			name:  "durations add up through the levels",
			steps: []step{{id(pkg, "a"), ct.StatePassed, 1 * ms, 0}, {id(pkg, sub, "x"), ct.StatePassed, 2 * ms, 0}, {id(pkg, "b"), ct.StatePassed, none, 0}},
			want:  map[string]want{id(pkg, sub): {ct.StatePassed, ct.StateUnset, 2 * ms}, id(pkg): {ct.StatePassed, ct.StateUnset, 3 * ms}},
		},
		{
			name:  "a later duration replaces the sum of a parent",
			steps: []step{{id(pkg, "a"), ct.StatePassed, 1 * ms, 0}, {id(pkg, "a"), ct.StatePassed, 6 * ms, 0}},
			want:  map[string]want{id(pkg, "a"): {ct.StatePassed, ct.StatePassed, 6 * ms}, id(pkg): {ct.StatePassed, ct.StateUnset, 6 * ms}},
		},
		{
			name:  "ending the tasks skips what is queued or running",
			steps: []step{{id(pkg, "a"), ct.StateQueued, none, 0}, {id(pkg, "b"), ct.StateRunning, none, 0}, {id(other), ct.StatePassed, none, 0}},
			end:   true,
			want: map[string]want{
				id(pkg, "a"): {ct.StateSkipped, ct.StateSkipped, none}, id(pkg, "b"): {ct.StateSkipped, ct.StateSkipped, none},
				id(pkg): {ct.StateSkipped, ct.StateUnset, none}, id(other): {ct.StatePassed, ct.StatePassed, none},
			},
			counts: map[ct.State]int{ct.StateSkipped: 2, ct.StatePassed: 1, ct.StateUnset: 1},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			res := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupRun}, Persist: true})
			var runs []*ct.Run
			for i := 0; i < 2; i++ {
				run, err := res.AddTask(ct.TaskSpec{Name: "task", ControllerID: "c"})
				if err != nil {
					t.Fatal(err)
				}
				runs = append(runs, run)
			}
			for _, s := range c.steps {
				runs[s.task].SetState(s.id, s.state, s.dur)
			}
			if c.end {
				runs[0].End()
				runs[1].End()
			}
			for tid, w := range c.want {
				got, ok := res.Test(tid)
				if !ok {
					t.Errorf("%q is not in the result", tid)
					continue
				}
				if got.State != w.state || got.OwnState != w.own || got.Duration != w.dur {
					t.Errorf("%q: state %v own %v duration %v, want %v %v %v", tid, got.State, got.OwnState, got.Duration, w.state, w.own, w.dur)
				}
			}
			for st, n := range c.counts {
				if got := res.Counts()[st]; got != n {
					t.Errorf("count of %v = %d, want %d (%v)", st, got, n, res.Counts())
				}
			}
		})
	}
}

// pkg and the other names are the ids of the rig's tree below the controller.
const (
	pkg   = "pkg"
	sub   = "sub"
	other = "other"
)

func TestRunToTheEndCompletesTheResultAndOrdersTheList(t *testing.T) {
	r := newRig(t)
	pid := r.profile(ct.ProfileSpec{Run: func(ctx context.Context, req ct.ProfileRequest) error {
		run, err := r.svc.Result(req.ResultID).AddTask(ct.TaskSpec{Name: "go test", ControllerID: "c"})
		if err != nil {
			return err
		}
		run.SetState(id(pkg, "a"), ct.StatePassed, time.Millisecond)
		run.End()
		return nil
	}})
	first, err := r.runAll(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.runAll(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	if !first.IsComplete() || !second.IsComplete() {
		t.Fatal("a result did not complete")
	}
	got := r.svc.Results()
	if len(got) != 2 || got[0].ID() != second.ID() || got[1].ID() != first.ID() {
		t.Errorf("the results are not newest first")
	}
	if r.svc.IsRunning() {
		t.Error("IsRunning after the runs")
	}
	if _, st, ok := r.svc.StateOf(id(pkg, "a")); !ok || st.State != ct.StatePassed {
		t.Errorf("StateOf = %v %v", st.State, ok)
	}
	r.svc.ClearResults()
	if r.svc.HasAnyResults() {
		t.Error("ClearResults left results")
	}
	if n := len(r.events(ct.EventRunning)); n != 4 {
		t.Errorf("EventRunning fired %d times, want 4 (two runs, start and end)", n)
	}
}

func TestOnlyTheLast16ResultsAreKept(t *testing.T) {
	r := newRig(t)
	for i := 0; i < ct.MaxResults+3; i++ {
		res := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupRun}, Persist: true})
		run, err := res.AddTask(ct.TaskSpec{ControllerID: "c"})
		if err != nil {
			t.Fatal(err)
		}
		run.End()
	}
	if n := len(r.svc.Results()); n != ct.MaxResults {
		t.Errorf("%d results, want %d", n, ct.MaxResults)
	}
	removed := 0
	for _, e := range r.events(ct.EventResult) {
		if e.Change == ct.ResultRemoved {
			removed += len(e.Removals)
		}
	}
	if removed != 3 {
		t.Errorf("%d results reported removed, want 3", removed)
	}
}

func TestInvalidateRetiresTheTestsAndTheOnesBelow(t *testing.T) {
	r := newRig(t)
	res := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupRun}, Persist: true})
	run, err := res.AddTask(ct.TaskSpec{ControllerID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{id(pkg, "a"), id(pkg, sub, "x"), id(other)} {
		run.SetState(p, ct.StatePassed, ct.NoDuration)
	}
	r.svc.InvalidateResults(id(pkg, sub))
	for tid, retired := range map[string]bool{id(pkg): false, id(pkg, "a"): false, id(pkg, sub): true, id(pkg, sub, "x"): true, id(other): false} {
		if got, _ := res.Test(tid); got.Retired != retired {
			t.Errorf("%q retired = %v, want %v", tid, got.Retired, retired)
		}
	}
	r.svc.InvalidateResults()
	if got, _ := res.Test(id(other)); !got.Retired {
		t.Error("InvalidateResults() with no ids left a test alone")
	}
}
