package testing_test

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ct "github.com/rm4n0s/emetos/internal/core/testing"
)

// recorder is a profile Run function that remembers the requests it got and reports every included test as passed.
type recorder struct {
	r   *rig
	got []ct.ProfileRequest
}

func (rc *recorder) run(ctx context.Context, req ct.ProfileRequest) error {
	rc.got = append(rc.got, req)
	run, err := rc.r.svc.Result(req.ResultID).AddTask(ct.TaskSpec{Name: "t", ControllerID: "c"})
	if err != nil {
		return err
	}
	for _, in := range req.Include {
		run.SetState(in, ct.StatePassed, ct.NoDuration)
	}
	run.End()
	return nil
}

func TestRunRequestsWithIncludeAndExclude(t *testing.T) {
	cases := []struct {
		name        string
		include     []string
		exclude     []string
		wantInclude []string
		wantExclude []string
		wantRun     bool
	}{
		{"the controller runs everything", []string{"c"}, nil, []string{"c"}, nil, true},
		{"an item and its sibling", []string{id(pkg, "a"), id(other)}, nil, []string{id(pkg, "a"), id(other)}, nil, true},
		{"a duplicate is run once", []string{id(other), id(other)}, nil, []string{id(other)}, nil, true},
		{"an item that is not in the tree is left out", []string{id("gone"), id(other)}, nil, []string{id(other)}, nil, true},
		{"nothing in the tree: the profile is not called", []string{id("gone")}, nil, nil, nil, false},
		{"an exclusion below an inclusion reaches the profile", []string{id(pkg)}, []string{id(pkg, "b")}, []string{id(pkg)}, []string{id(pkg, "b")}, true},
		{"an exclusion deep below the inclusion", []string{"c"}, []string{id(pkg, sub, "x")}, []string{"c"}, []string{id(pkg, sub, "x")}, true},
		{"an exclusion that is not below any inclusion is dropped", []string{id(pkg)}, []string{id(other)}, []string{id(pkg)}, nil, true},
		{"an item is not excluded by being included", []string{id(pkg)}, []string{id(pkg)}, []string{id(pkg)}, nil, true},
		{"an exclusion that is not in the tree is dropped", []string{"c"}, []string{id("gone")}, []string{"c"}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			rc := &recorder{r: r}
			pid := r.profile(ct.ProfileSpec{Run: rc.run})
			res, err := r.svc.RunTests(context.Background(), ct.RunRequest{
				Group: ct.GroupRun, Exclude: c.exclude, PreserveFocus: true,
				Targets: []ct.RunTarget{{ControllerID: "c", ProfileID: pid, TestIDs: c.include}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !res.IsComplete() {
				t.Error("the result did not complete")
			}
			if !c.wantRun {
				if len(rc.got) != 0 {
					t.Fatalf("the profile was called: %+v", rc.got)
				}
				return
			}
			if len(rc.got) != 1 {
				t.Fatalf("the profile was called %d times", len(rc.got))
			}
			got := rc.got[0]
			if !reflect.DeepEqual(got.Include, c.wantInclude) || !reflect.DeepEqual(got.Exclude, c.wantExclude) {
				t.Errorf("include %q exclude %q, want %q %q", got.Include, got.Exclude, c.wantInclude, c.wantExclude)
			}
			if got.ResultID != res.ID() || got.ProfileID != pid || got.Continuous || !got.PreserveFocus {
				t.Errorf("request %+v", got)
			}
		})
	}
}

func TestRunRequestErrors(t *testing.T) {
	r := newRig(t)
	pid := r.profile(ct.ProfileSpec{Run: func(context.Context, ct.ProfileRequest) error { return nil }})
	ok := ct.RunTarget{ControllerID: "c", ProfileID: pid, TestIDs: []string{"c"}}
	cases := []struct {
		name  string
		req   ct.RunRequest
		route string
	}{
		{"no targets", ct.RunRequest{Group: ct.GroupRun}, "Service.RunTests.TestingRunInvalid"},
		{"no group", ct.RunRequest{Targets: []ct.RunTarget{ok}}, "Service.RunTests.TestingRunInvalid"},
		{"unknown profile", ct.RunRequest{Group: ct.GroupRun, Targets: []ct.RunTarget{{ControllerID: "c", ProfileID: 99, TestIDs: []string{"c"}}}}, "Service.RunTests.TestingProfileUnknown"},
		{"unknown controller", ct.RunRequest{Group: ct.GroupRun, Targets: []ct.RunTarget{{ControllerID: "zz", ProfileID: 1}}}, "Service.RunTests.TestingProfileUnknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := r.svc.RunTests(context.Background(), c.req)
			if res != nil {
				t.Error("a result was made")
			}
			routeOf(t, err, c.route)
		})
	}
	if len(r.svc.Results()) != 0 {
		t.Error("a failed request left a result")
	}
}

func TestAProfileThatFailsKeepsItsResult(t *testing.T) {
	r := newRig(t)
	pid := r.profile(ct.ProfileSpec{Run: func(context.Context, ct.ProfileRequest) error { return plainError("the runner crashed") }})
	res, err := r.runAll(context.Background(), pid)
	routeOf(t, err, "Service.runTarget.TestingRunFailed")
	if res == nil || !res.IsComplete() {
		t.Fatal("no completed result")
	}
}

func TestRunsWaitForTheirTasksAndCancelEndsThemByForce(t *testing.T) {
	r := newRig(t)
	started := make(chan struct{})
	release := make(chan struct{})
	// a profile that starts a task and returns: the run goes on until the task ends
	pid := r.profile(ct.ProfileSpec{Label: "late", Run: func(ctx context.Context, req ct.ProfileRequest) error {
		run, err := r.svc.Result(req.ResultID).AddTask(ct.TaskSpec{ControllerID: "c"})
		if err != nil {
			return err
		}
		run.SetState(id(other), ct.StateRunning, ct.NoDuration)
		close(started)
		go func() {
			select {
			case <-release:
				run.SetState(id(other), ct.StatePassed, time.Millisecond)
				run.End()
			case <-time.After(30 * time.Second):
			}
		}()
		return nil
	}})
	done := make(chan *ct.Result)
	go func() {
		res, err := r.runAll(context.Background(), pid)
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	<-started
	select {
	case <-done:
		t.Fatal("the run returned while its task was running")
	case <-time.After(50 * time.Millisecond):
	}
	if !r.svc.IsRunning() {
		t.Error("not running while a task runs")
	}
	close(release)
	res := <-done
	if got, _ := res.Test(id(other)); got.State != ct.StatePassed {
		t.Errorf("state %v", got.State)
	}

	// cancelled: after the deadline the task that never ends is ended by force and its running test is skipped
	started = make(chan struct{})
	release = make(chan struct{})
	go func() {
		res, _ := r.runAll(context.Background(), pid)
		done <- res
	}()
	<-started
	r.svc.CancelRun(r.svc.Results()[0].ID())
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled run did not end")
	}
	if got, _ := res.Test(id(other)); got.State != ct.StateSkipped {
		t.Errorf("after the cancel the test is %v, want skipped", got.State)
	}
	close(release)
}

func TestCancelAllEndsTheContextOfEveryRun(t *testing.T) {
	r := newRig(t)
	running := make(chan struct{})
	pid := r.profile(ct.ProfileSpec{Run: func(ctx context.Context, req ct.ProfileRequest) error {
		run, err := r.svc.Result(req.ResultID).AddTask(ct.TaskSpec{ControllerID: "c"})
		if err != nil {
			return err
		}
		defer run.End()
		close(running)
		<-ctx.Done()
		return nil
	}})
	done := make(chan struct{})
	go func() {
		_, _ = r.runAll(context.Background(), pid)
		close(done)
	}()
	<-running
	r.svc.CancelAll()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CancelAll did not end the run")
	}
}

func TestSeveralTargetsMakeOneResultWithATaskEach(t *testing.T) {
	r := newRig(t)
	var calls atomic.Int32
	run := func(ctx context.Context, req ct.ProfileRequest) error {
		calls.Add(1)
		u, err := r.svc.Result(req.ResultID).AddTask(ct.TaskSpec{Name: strings.Join(req.Include, ","), ControllerID: "c"})
		if err != nil {
			return err
		}
		for _, in := range req.Include {
			u.SetState(in, ct.StatePassed, ct.NoDuration)
		}
		u.End()
		return nil
	}
	p1 := r.profile(ct.ProfileSpec{Label: "one", Run: run})
	p2 := r.profile(ct.ProfileSpec{Label: "two", Run: run})
	res, err := r.svc.RunTests(context.Background(), ct.RunRequest{Group: ct.GroupRun, Targets: []ct.RunTarget{
		{ControllerID: "c", ProfileID: p1, TestIDs: []string{id(pkg, "a")}}, {ControllerID: "c", ProfileID: p2, TestIDs: []string{id(other)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(res.Tasks()) != 2 || len(r.svc.Results()) != 1 {
		t.Errorf("%d calls, %d tasks, %d results", calls.Load(), len(res.Tasks()), len(r.svc.Results()))
	}
	if res.Counts()[ct.StatePassed] != 2 {
		t.Errorf("counts %v", res.Counts())
	}
}

func TestResolveGroupChoosesProfiles(t *testing.T) {
	r := newRig(t)
	noop := func(context.Context, ct.ProfileRequest) error { return nil }
	r.put(ct.Item{ID: id("tagged"), Label: "tagged", Tags: []string{"slow"}})
	plain := r.profile(ct.ProfileSpec{Label: "plain", Run: noop})
	slow := r.profile(ct.ProfileSpec{Label: "slow only", Tag: "slow", IsDefault: true, Run: noop})
	dbg := r.profile(ct.ProfileSpec{Label: "debug", Group: ct.GroupDebug, Run: noop})
	cases := []struct {
		name  string
		req   ct.AmbiguousRequest
		want  []ct.RunTarget
		wantG ct.Group
	}{
		{"the default profile takes the tests it can use", ct.AmbiguousRequest{Group: ct.GroupRun, Tests: []string{id("tagged"), id(other)}},
			[]ct.RunTarget{{ControllerID: "c", ProfileID: slow, TestIDs: []string{id("tagged")}}}, ct.GroupRun},
		{"the controller's id has no tags, so a profile with a tag does not take it and the fallback runs", ct.AmbiguousRequest{Group: ct.GroupRun, Tests: []string{"c"}},
			[]ct.RunTarget{{ControllerID: "c", ProfileID: plain, TestIDs: []string{"c"}}}, ct.GroupRun},
		{"the first profile of the group when there is no default", ct.AmbiguousRequest{Group: ct.GroupDebug, Tests: []string{id(pkg, "a"), id(other)}},
			[]ct.RunTarget{{ControllerID: "c", ProfileID: dbg, TestIDs: []string{id(pkg, "a"), id(other)}}}, ct.GroupDebug},
		{"no profile for the group", ct.AmbiguousRequest{Group: ct.GroupCoverage, Tests: []string{"c"}}, nil, ct.GroupCoverage},
		{"no tests", ct.AmbiguousRequest{Group: ct.GroupRun}, nil, ct.GroupRun},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := r.svc.ResolveGroup(c.req)
			if !reflect.DeepEqual(got.Targets, c.want) || got.Group != c.wantG {
				t.Errorf("targets %+v group %v, want %+v %v", got.Targets, got.Group, c.want, c.wantG)
			}
		})
	}
}

func TestOutputMessagesAndCoverageOfATask(t *testing.T) {
	r := newRig(t)
	res := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupRun}, Persist: true})
	run, err := res.AddTask(ct.TaskSpec{Name: "out", ControllerID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	loc := &ct.Location{URI: "file:///w/pkg/a_test.go", Range: ct.Range{Start: ct.Pos{Line: 3}, End: ct.Pos{Line: 3, Col: 9}}}
	run.AppendOutput([]byte("plain line\r\n"), nil, "")
	run.AppendOutput([]byte("\x1b[31mred\x1b[0m failure\r\n"), loc, id(pkg, "a"))
	run.AppendOutput([]byte("elsewhere\r\n"), loc, "")
	run.AddMessage(id(pkg, "a"), ct.Message{Text: "expected 1", Expected: "1", Actual: "2", HasDiff: true})
	run.SetState(id(pkg, "a"), ct.StateFailed, time.Millisecond)

	out, start := res.Output(run.TaskID())
	if string(out) != "plain line\r\n\x1b[31mred\x1b[0m failure\r\nelsewhere\r\n" || start != 0 {
		t.Errorf("output %q from %d", out, start)
	}
	got, _ := res.Test(id(pkg, "a"))
	msgs := got.Tasks[0].Messages
	if len(msgs) != 2 || msgs[0].Type != ct.MessageOutput || msgs[0].Text != "red failure\r\n" || msgs[0].Offset != 12 || msgs[0].Length != 22 ||
		msgs[1].Type != ct.MessageError || !msgs[1].HasDiff || msgs[1].TestID != id(pkg, "a") {
		t.Errorf("messages %+v", msgs)
	}
	others := res.OtherMessages(run.TaskID())
	if len(others) != 1 || others[0].Location == nil || others[0].Offset != 34 {
		t.Errorf("other messages %+v", others)
	}
	// a message about a test that is not in the tree is kept with the others
	run.AppendOutput([]byte("x"), nil, id("gone"))
	if len(res.OtherMessages(run.TaskID())) != 2 {
		t.Error("the output of an unknown test was lost")
	}
	// the events say what changed
	var newMsgs int
	for _, e := range r.events(ct.EventTest) {
		if e.Test.Reason == ct.ChangeNewMessage {
			newMsgs++
		}
	}
	if newMsgs != 2 {
		t.Errorf("%d message events, want 2", newMsgs)
	}
	run.End()
	run.SetState(id(other), ct.StatePassed, 0)
	if _, ok := res.Test(id(other)); ok {
		t.Error("a report after the end added a test")
	}
}

func TestAResultThatEndedTakesNoTask(t *testing.T) {
	r := newRig(t)
	res := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupRun}})
	run, err := res.AddTask(ct.TaskSpec{ControllerID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	run.End()
	if !res.IsComplete() {
		t.Fatal("the result of an external run completes with its last task")
	}
	_, err = res.AddTask(ct.TaskSpec{})
	routeOf(t, err, "Result.AddTask.TestingResultComplete")
}

func TestLiveOutputKeepsTheLastBytes(t *testing.T) {
	r := newRig(t)
	res := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupRun}})
	run, _ := res.AddTask(ct.TaskSpec{ControllerID: "c"})
	chunk := []byte(strings.Repeat("x", 1<<20))
	for i := 0; i < ct.MaxLiveOutput/len(chunk)+6; i++ {
		run.AppendOutput(chunk, nil, "")
	}
	out, start := res.Output(run.TaskID())
	total := int64(len(chunk)) * int64(ct.MaxLiveOutput/len(chunk)+6)
	if int64(len(out))+start != total || len(out) > ct.MaxLiveOutput+ct.MaxLiveOutput/4 || start == 0 {
		t.Errorf("kept %d bytes from %d of %d", len(out), start, total)
	}
}
