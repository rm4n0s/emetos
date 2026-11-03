package testing_test

import (
	"testing"

	ct "github.com/rm4n0s/emetos/internal/core/testing"
)

// finish runs a result with the coverage of one file (or none) and ends it.
func finish(t *testing.T, r *rig, withCoverage bool) *ct.Result {
	t.Helper()
	res := r.svc.StartExternalRun(ct.ExternalRun{Request: ct.RunRequest{Group: ct.GroupCoverage}})
	run, err := res.AddTask(ct.TaskSpec{ControllerID: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if withCoverage {
		run.AddCoverage(ct.FileCoverage{URI: "file:///w/a.go", Statement: ct.Count{Covered: 1, Total: 2}})
	}
	run.End()
	return res
}

// The open coverage: a result that ends with coverage opens it, one that ends without closes it, and removing the result of the open
// coverage closes it (VS Code's TestCoverageService).
func TestCoverageServiceFollowsTheResults(t *testing.T) {
	r := newRig(t)
	cs := ct.NewCoverageService(r.svc)
	defer cs.Dispose()
	changes := 0
	cs.OnChange(func() { changes++ })

	if _, ok := cs.Selected(); ok {
		t.Fatal("coverage is open before any run")
	}
	with := finish(t, r, true)
	sel, ok := cs.Selected()
	if !ok || sel.ResultID != with.ID() || sel.Coverage == nil || len(sel.Coverage.Files()) != 1 {
		t.Fatalf("a result with coverage did not open it: %+v %v", sel, ok)
	}
	if changes != 1 {
		t.Errorf("%d changes, want 1", changes)
	}
	finish(t, r, false)
	if _, ok := cs.Selected(); ok {
		t.Error("a result without coverage left the old coverage open")
	}
	finish(t, r, true)
	if _, ok := cs.Selected(); !ok {
		t.Fatal("coverage did not open again")
	}
	r.svc.ClearResults()
	if _, ok := cs.Selected(); ok {
		t.Error("clearing the results left their coverage open")
	}
}

// Open and Close, and the inline setting, tell the listeners only when something changed.
func TestCoverageServiceOpenCloseAndInline(t *testing.T) {
	r := newRig(t)
	cs := ct.NewCoverageService(r.svc)
	defer cs.Dispose()
	res := finish(t, r, true)
	cs.Close()
	if _, ok := cs.Selected(); ok {
		t.Fatal("Close left it open")
	}
	changes := 0
	stop := cs.OnChange(func() { changes++ })
	cs.Close() // already closed
	cs.SetShowInline(false)
	if changes != 0 {
		t.Errorf("%d changes for nothing", changes)
	}
	cs.Open(res, res.Tasks()[0].ID)
	cs.SetShowInline(true)
	cs.SetShowInline(true)
	if _, ok := cs.Selected(); !ok || !cs.ShowInline() || changes != 2 {
		t.Errorf("open %v, inline %v, %d changes (want true, true, 2)", ok, cs.ShowInline(), changes)
	}
	cs.Open(res, "no-such-task") // a task with no coverage opens nothing and changes nothing
	if _, ok := cs.Selected(); !ok || changes != 2 {
		t.Errorf("opening a task without coverage changed the state (%d changes)", changes)
	}
	stop()
	cs.Close()
	if changes != 2 {
		t.Errorf("a stopped listener was called (%d)", changes)
	}
}
