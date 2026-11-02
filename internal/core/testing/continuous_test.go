package testing_test

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	ct "github.com/rm4n0s/emetos/internal/core/testing"
)

// contProfile is a continuous profile whose runs are announced on a channel.
type contProfile struct {
	runs chan ct.ProfileRequest
	mu   sync.Mutex
	ctxs []context.Context
}

func (c *contProfile) run(r *rig) func(context.Context, ct.ProfileRequest) error {
	return func(ctx context.Context, req ct.ProfileRequest) error {
		c.mu.Lock()
		c.ctxs = append(c.ctxs, ctx)
		c.mu.Unlock()
		run, err := r.svc.Result(req.ResultID).AddTask(ct.TaskSpec{ControllerID: "c"})
		if err != nil {
			return err
		}
		for _, in := range req.Include {
			run.SetState(in, ct.StatePassed, ct.NoDuration)
		}
		run.End()
		c.runs <- req
		return nil
	}
}

func next(t *testing.T, ch chan ct.ProfileRequest) ct.ProfileRequest {
	t.Helper()
	select {
	case req := <-ch:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("no run started")
		return ct.ProfileRequest{}
	}
}

func TestContinuousRunsAreRunAgainWhenFilesChange(t *testing.T) {
	cases := []struct {
		name    string
		testID  string
		trigger func(it ct.Item, uri string) bool
		changed []string
		want    []string // the include of the second run; nil: no second run
		started int
	}{
		{"a changed test file runs its item", "", nil, []string{"file:///w/pkg/a_test.go"}, []string{id(pkg, "a")}, 1},
		{"two files give two items", "", nil, []string{"file:///w/pkg/a_test.go", "file:///w/other_test.go"}, []string{id(pkg, "a"), id(other)}, 1},
		{"a folder and a file below it give the folder", "", nil, []string{"file:///w/pkg/sub", "file:///w/pkg/sub/x_test.go"}, []string{id(pkg, sub)}, 1},
		{"a file of no item runs nothing", "", nil, []string{"file:///w/readme.md"}, nil, 0},
		{"the provider says which files belong to an item", "", func(it ct.Item, uri string) bool { return it.ID == id(pkg) && uri == "file:///w/pkg/lib.go" },
			[]string{"file:///w/pkg/lib.go"}, []string{id(pkg)}, 1},
		{"only the items below the test the run is for", id(pkg), nil, []string{"file:///w/other_test.go"}, nil, 0},
		{"the item the run is for", id(pkg), nil, []string{"file:///w/pkg/b_test.go"}, []string{id(pkg, "b")}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			cp := &contProfile{runs: make(chan ct.ProfileRequest, 10)}
			pid := r.profile(ct.ProfileSpec{Run: cp.run(r), SupportsContinuous: true, Trigger: c.trigger})
			if err := r.svc.StartContinuous([]ct.ProfileRef{{ControllerID: "c", ID: pid}}, c.testID); err != nil {
				t.Fatal(err)
			}
			first := next(t, cp.runs)
			root := c.testID
			if root == "" {
				root = "c"
			}
			if !first.Continuous || !reflect.DeepEqual(first.Include, []string{root}) {
				t.Errorf("first run %+v", first)
			}
			if !r.svc.IsContinuousEnabled() {
				t.Error("not enabled")
			}
			if got := r.svc.FilesChanged(c.changed); got != c.started {
				t.Errorf("FilesChanged started %d runs, want %d", got, c.started)
			}
			if c.want == nil {
				select {
				case req := <-cp.runs:
					t.Fatalf("a run started: %+v", req)
				case <-time.After(100 * time.Millisecond):
				}
				return
			}
			second := next(t, cp.runs)
			slices.Sort(second.Include)
			want := slices.Clone(c.want)
			slices.Sort(want)
			if !reflect.DeepEqual(second.Include, want) || !second.Continuous {
				t.Errorf("second run %+v, want include %q", second, want)
			}
		})
	}
}

func TestAChangeDuringAContinuousRunCancelsItAndStartsAgain(t *testing.T) {
	r := newRig(t)
	started := make(chan context.Context, 4)
	var calls int
	var mu sync.Mutex
	pid := r.profile(ct.ProfileSpec{SupportsContinuous: true, Run: func(ctx context.Context, req ct.ProfileRequest) error {
		run, err := r.svc.Result(req.ResultID).AddTask(ct.TaskSpec{ControllerID: "c"})
		if err != nil {
			return err
		}
		defer run.End()
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		started <- ctx
		if n == 1 {
			<-ctx.Done() // the first run is slow: it ends only when it is cancelled
		}
		return nil
	}})
	if err := r.svc.StartContinuous([]ct.ProfileRef{{ControllerID: "c", ID: pid}}, ""); err != nil {
		t.Fatal(err)
	}
	firstCtx := <-started
	if r.svc.FilesChanged([]string{"file:///w/other_test.go"}) != 1 {
		t.Fatal("not started again")
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("no second run")
	}
	if firstCtx.Err() == nil {
		t.Error("the first run was not cancelled")
	}
}

func TestStoppingContinuousRuns(t *testing.T) {
	r := newRig(t)
	cp := &contProfile{runs: make(chan ct.ProfileRequest, 10)}
	p1 := r.profile(ct.ProfileSpec{Label: "one", Run: cp.run(r), SupportsContinuous: true})
	p2 := r.profile(ct.ProfileSpec{Label: "two", Run: cp.run(r), SupportsContinuous: true})
	ref1, ref2 := ct.ProfileRef{ControllerID: "c", ID: p1}, ct.ProfileRef{ControllerID: "c", ID: p2}
	if err := r.svc.StartContinuous([]ct.ProfileRef{ref1, ref2}, id(pkg)); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.StartContinuous([]ct.ProfileRef{ref1}, id(other)); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.StartContinuous([]ct.ProfileRef{ref1}, id(other)); err != nil { // again: left alone
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		next(t, cp.runs)
	}
	checks := []struct {
		name string
		got  bool
		want bool
	}{
		{"specifically pkg", r.svc.ContinuousSpecificallyFor(id(pkg)), true},
		{"specifically pkg/a", r.svc.ContinuousSpecificallyFor(id(pkg, "a")), false},
		{"a parent of pkg/a", r.svc.ContinuousForAParentOf(id(pkg, "a")), true},
		{"a parent of other/..", r.svc.ContinuousForAParentOf(id("zzz")), false},
		{"a child of the controller", r.svc.ContinuousForAChildOf("c"), true},
		{"a child of pkg/a", r.svc.ContinuousForAChildOf(id(pkg, "a")), false},
		{"for profile one", r.svc.ContinuousForProfile(ref1), true},
		{"for profile two", r.svc.ContinuousForProfile(ref2), true},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	r.svc.StopContinuousProfile(ref2)
	if r.svc.ContinuousForProfile(ref2) || !r.svc.ContinuousSpecificallyFor(id(pkg)) {
		t.Error("stopping profile two did the wrong thing")
	}
	r.svc.StopContinuous(id(pkg))
	if r.svc.ContinuousSpecificallyFor(id(pkg)) || !r.svc.ContinuousSpecificallyFor(id(other)) {
		t.Error("stopping pkg did the wrong thing")
	}
	r.svc.StopContinuous("")
	if r.svc.IsContinuousEnabled() {
		t.Error("still enabled")
	}
	if r.svc.FilesChanged([]string{"file:///w/other_test.go"}) != 0 {
		t.Error("a stopped run started again")
	}
	if n := len(r.events(ct.EventContinuous)); n < 4 {
		t.Errorf("%d continuous events", n)
	}
}

func TestStartContinuousGroupUsesTheDefaultsThatSupportIt(t *testing.T) {
	r := newRig(t)
	cp := &contProfile{runs: make(chan ct.ProfileRequest, 10)}
	r.profile(ct.ProfileSpec{Label: "no", Run: cp.run(r), IsDefault: true})
	pid := r.profile(ct.ProfileSpec{Label: "yes", Run: cp.run(r), IsDefault: true, SupportsContinuous: true})
	if err := r.svc.StartContinuousGroup(ct.GroupRun, ""); err != nil {
		t.Fatal(err)
	}
	next(t, cp.runs)
	if !r.svc.ContinuousForProfile(ct.ProfileRef{ControllerID: "c", ID: pid}) {
		t.Error("the profile that supports it did not start")
	}
	err := r.svc.StartContinuousGroup(ct.GroupDebug, "")
	routeOf(t, err, "Service.StartContinuousGroup.TestingContinuousUnsupported")
}

func TestStartContinuousErrors(t *testing.T) {
	r := newRig(t)
	noop := func(context.Context, ct.ProfileRequest) error { return nil }
	plain := r.profile(ct.ProfileSpec{Label: "plain", Run: noop})
	cont := r.profile(ct.ProfileSpec{Label: "cont", Run: noop, SupportsContinuous: true})
	cases := []struct {
		name   string
		refs   []ct.ProfileRef
		testID string
		route  string
	}{
		{"unknown profile", []ct.ProfileRef{{ControllerID: "c", ID: 77}}, "", "Service.StartContinuous.TestingProfileUnknown"},
		{"no support", []ct.ProfileRef{{ControllerID: "c", ID: plain}}, "", "Service.StartContinuous.TestingContinuousUnsupported"},
		{"another controller", []ct.ProfileRef{{ControllerID: "c", ID: cont}}, "d" + ct.IDSeparator + "x", "Service.StartContinuous.TestingContinuousUnsupported"},
		{"unknown item", []ct.ProfileRef{{ControllerID: "c", ID: cont}}, id("gone"), "Service.StartContinuous.TestingItemUnknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			routeOf(t, r.svc.StartContinuous(c.refs, c.testID), c.route)
		})
	}
	if r.svc.IsContinuousEnabled() {
		t.Error("a failed start left a run")
	}
}

func TestRemovingAProfileOrAControllerStopsItsContinuousRuns(t *testing.T) {
	r := newRig(t)
	cp := &contProfile{runs: make(chan ct.ProfileRequest, 10)}
	pid := r.profile(ct.ProfileSpec{Run: cp.run(r), SupportsContinuous: true})
	ref := ct.ProfileRef{ControllerID: "c", ID: pid}
	if err := r.svc.StartContinuous([]ct.ProfileRef{ref}, ""); err != nil {
		t.Fatal(err)
	}
	next(t, cp.runs)
	r.ctl.RemoveProfile(pid)
	if r.svc.IsContinuousEnabled() {
		t.Error("removing the profile left the run")
	}
	pid = r.profile(ct.ProfileSpec{Label: "again", Run: cp.run(r), SupportsContinuous: true})
	if err := r.svc.StartContinuous([]ct.ProfileRef{{ControllerID: "c", ID: pid}}, ""); err != nil {
		t.Fatal(err)
	}
	next(t, cp.runs)
	r.ctl.Dispose()
	if r.svc.IsContinuousEnabled() {
		t.Error("disposing the controller left the run")
	}
}
