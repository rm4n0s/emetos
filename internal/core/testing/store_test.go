package testing_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	ct "github.com/rm4n0s/emetos/internal/core/testing"
)

// finished runs a result of the controller with some messages, output and durations.
func (r *rig) finished(persist bool, output string) *ct.Result {
	r.t.Helper()
	res := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupRun, Exclude: []string{id(other)}, Targets: []ct.RunTarget{{ControllerID: "c", ProfileID: 1, TestIDs: []string{"c"}}}}, Persist: persist})
	run, err := res.AddTask(ct.TaskSpec{Name: "task", ControllerID: "c"})
	if err != nil {
		r.t.Fatal(err)
	}
	run.SetState(id(pkg, "a"), ct.StatePassed, 3*time.Millisecond)
	run.AddMessage(id(pkg, "b"), ct.Message{Text: "boom", Expected: "1", Actual: "2", HasDiff: true,
		Location: &ct.Location{URI: "file:///w/pkg/b_test.go", Range: ct.Range{Start: ct.Pos{Line: 4, Col: 2}}}, StackTrace: []ct.StackFrame{{Label: "f", URI: "file:///w/f.go", Pos: &ct.Pos{Line: 1}}}})
	run.SetState(id(pkg, "b"), ct.StateFailed, 4*time.Millisecond)
	run.SetState(id(pkg, sub, "x"), ct.StateQueued, ct.NoDuration)
	run.AppendOutput([]byte(output), &ct.Location{URI: "file:///w/pkg/b_test.go"}, id(pkg, "b"))
	run.End()
	return res
}

func TestStoredResultsRoundTrip(t *testing.T) {
	r := newRig(t)
	kept := r.finished(true, "--- FAIL: b\n")
	r.finished(false, "not saved\n") // persist false: not kept
	running := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupRun}, Persist: true})
	if _, err := running.AddTask(ct.TaskSpec{ControllerID: "c"}); err != nil { // a run that goes on is not saved either
		t.Fatal(err)
	}
	pid := r.profile(ct.ProfileSpec{Label: "dflt", IsDefault: true, Run: func(context.Context, ct.ProfileRequest) error { return nil }})
	r.ctl.UpdateProfile(pid, nil, ptr(false))

	data, err := r.svc.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	var generic struct{ Results []json.RawMessage }
	if err := json.Unmarshal(data, &generic); err != nil || len(generic.Results) != 1 {
		t.Fatalf("saved %d results (%v)", len(generic.Results), err)
	}

	r2 := newRig(t)
	pid2 := r2.profile(ct.ProfileSpec{Label: "dflt", IsDefault: true, Run: func(context.Context, ct.ProfileRequest) error { return nil }})
	if err := r2.svc.RestoreState(data); err != nil {
		t.Fatal(err)
	}
	got := r2.svc.Results()
	if len(got) != 1 {
		t.Fatalf("%d results restored", len(got))
	}
	back := got[0]
	if !back.Hydrated() || !back.IsComplete() || back.ID() != kept.ID() || back.Name() != kept.Name() {
		t.Errorf("restored %v %v %v", back.Hydrated(), back.ID(), back.Name())
	}
	if !back.CompletedAt().Equal(kept.CompletedAt().Truncate(time.Millisecond)) {
		t.Errorf("completed at %v, want %v", back.CompletedAt(), kept.CompletedAt())
	}
	if !reflect.DeepEqual(back.Tests(), kept.Tests()) || back.Counts() != kept.Counts() || !reflect.DeepEqual(back.Request(), kept.Request()) {
		t.Errorf("tests, counts or request differ:\n%+v\n%+v", back.Tests(), kept.Tests())
	}
	ko, _ := kept.Output(kept.Tasks()[0].ID)
	bo, _ := back.Output(back.Tasks()[0].ID)
	if string(bo) != string(ko) || back.Tasks()[0].Running {
		t.Errorf("output %q vs %q", bo, ko)
	}
	if _, st, ok := r2.svc.StateOf(id(pkg, "b")); !ok || st.State != ct.StateFailed {
		t.Errorf("the state of the failed test did not come back: %v %v", st.State, ok)
	}
	// the user's choice of a default came back, not the profile's own default
	for _, p := range r2.svc.Profiles("c") {
		if p.ID == pid2 && p.IsDefault {
			t.Error("the choice that the profile is not a default was not restored")
		}
	}
	// a restore replaces the results it restored before
	if err := r2.svc.RestoreState(data); err != nil || len(r2.svc.Results()) != 1 {
		t.Errorf("second restore: %v, %d results", err, len(r2.svc.Results()))
	}
	// and it saves again as it was
	again, err := r2.svc.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Errorf("a restored state saves differently:\n%s\n%s", data, again)
	}
}

func ptr[T any](v T) *T { return &v }

func TestSavedOutputIsTrimmedToOneMebibyte(t *testing.T) {
	r := newRig(t)
	res := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupRun}, Persist: true})
	var ids []string
	var runs []*ct.Run
	for i := 0; i < 2; i++ {
		run, _ := res.AddTask(ct.TaskSpec{ControllerID: "c"})
		run.AppendOutput([]byte(strings.Repeat("a", 700<<10)), nil, id(pkg, "a")) // 700 KiB each: 1.4 MiB in all
		run.AppendOutput([]byte(strings.Repeat("b", 100)), &ct.Location{URI: "file:///x"}, "")
		ids = append(ids, run.TaskID())
		runs = append(runs, run)
	}
	for _, run := range runs {
		run.End()
	}
	data, err := r.svc.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	r2 := newRig(t)
	if err := r2.svc.RestoreState(data); err != nil {
		t.Fatal(err)
	}
	back := r2.svc.Results()[0]
	o1, s1 := back.Output(ids[0])
	o2, s2 := back.Output(ids[1])
	if len(o1)+len(o2) != ct.MaxSavedOutput {
		t.Errorf("saved %d + %d bytes, want %d in all", len(o1), len(o2), ct.MaxSavedOutput)
	}
	if len(o2) != 700<<10+100 || s2 != 0 {
		t.Errorf("the last task lost bytes: %d from %d", len(o2), s2)
	}
	if s1 != int64(700<<10+100-len(o1)) {
		t.Errorf("the first task starts at %d with %d bytes", s1, len(o1))
	}
	// the live result still has everything
	if o, _ := res.Output(ids[0]); len(o) != 700<<10+100 {
		t.Errorf("the live output was trimmed: %d", len(o))
	}
	// the message of the output that was cut off (the first 700 KiB of the first task) is gone, the others stay
	if len(back.OtherMessages(ids[0])) != 1 || len(back.OtherMessages(ids[1])) != 1 {
		t.Errorf("messages %d %d", len(back.OtherMessages(ids[0])), len(back.OtherMessages(ids[1])))
	}
	if got, _ := back.Test(id(pkg, "a")); len(got.Tasks[0].Messages) != 0 || len(got.Tasks[1].Messages) != 1 {
		t.Errorf("test messages %d %d", len(got.Tasks[0].Messages), len(got.Tasks[1].Messages))
	}
}

func TestOnlyTheLast16ResultsAreSaved(t *testing.T) {
	r := newRig(t)
	for i := 0; i < ct.MaxResults; i++ {
		r.finished(true, "x")
	}
	data, err := r.svc.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	r2 := newRig(t)
	if err := r2.svc.RestoreState(data); err != nil {
		t.Fatal(err)
	}
	got, want := r2.svc.Results(), r.svc.Results()
	if len(got) != ct.MaxResults {
		t.Fatalf("%d restored", len(got))
	}
	for i := range got {
		if got[i].ID() != want[i].ID() {
			t.Errorf("result %d out of order", i)
		}
	}
}

func TestRestoreStateErrorsAndDamagedData(t *testing.T) {
	r := newRig(t)
	kept := r.finished(true, "x")
	routeOf(t, r.svc.RestoreState([]byte(`{broken`)), "Service.RestoreState.TestingStateInvalid")
	routeOf(t, r.svc.RestoreState([]byte(`{"version": 99}`)), "Service.RestoreState.TestingStateUnsupported")
	if got := r.svc.Results(); len(got) != 1 || got[0] != kept {
		t.Error("a bad state changed the results")
	}
	if err := r.svc.RestoreState(nil); err != nil {
		t.Errorf("an empty state is no error: %v", err)
	}
	cases := []struct {
		name string
		json string
		want int
	}{
		{"a result with no id", `{"version":1,"results":[{"id":"","tests":[]}]}`, 0},
		{"a test with an unknown state", `{"version":1,"results":[{"id":"r","tests":[{"item":{"ID":"c"},"own":99,"shown":0,"tasks":[]}]}]}`, 0},
		{"a test whose parent is not above it", `{"version":1,"results":[{"id":"r","tests":[{"item":{"ID":"c\u0000a"},"parent":"zz","own":0,"shown":0,"tasks":[]}]}]}`, 0},
		{"a test with the wrong number of task states", `{"version":1,"results":[{"id":"r","tests":[{"item":{"ID":"c\u0000a"},"own":0,"shown":0,"tasks":[{"State":1}]}]}]}`, 0},
		{"the same test twice", `{"version":1,"results":[{"id":"r","tests":[{"item":{"ID":"c\u0000a"},"own":0,"shown":0},{"item":{"ID":"c\u0000a"},"own":0,"shown":0}]}]}`, 0},
		{"a good one next to a bad one", `{"version":1,"results":[{"id":"","tests":[]},{"id":"ok","completedAt":5,"tests":[{"item":{"ID":"c\u0000a"},"own":3,"shown":3}]}]}`, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r2 := newRig(t)
			if err := r2.svc.RestoreState([]byte(c.json)); err != nil {
				t.Fatal(err)
			}
			if n := len(r2.svc.Results()); n != c.want {
				t.Errorf("%d results, want %d", n, c.want)
			}
		})
	}
}

func TestSaveStateEncodeFailure(t *testing.T) {
	svc := ct.NewService(ct.Options{Marshal: func(any) ([]byte, error) { return nil, plainError("no encoder") }})
	_, err := svc.SaveState()
	routeOf(t, err, "Service.SaveState.TestingStateEncodeFailed")
}
