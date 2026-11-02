package app

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/core/workspace"
)

// What the window does with the test model besides the view (step 26.2, decision 1533): it keeps the results with the state of the window, feeds
// the changes of files to the continuous runs, makes one testing host for a workspace that its clients share, and gives the extensions the API.

func TestTheResultsCameBackWithTheStateOfTheWindow(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	r.runCommand(command.TestingRunAll, nil)
	state := r.win.testingWindowState()
	if len(state) == 0 {
		t.Fatal("no state to save after a run")
	}
	// a window of the same folder that restores it shows the states of the tests before any run
	r2 := newTestingRig(t, testingOpts{})
	if r2.svc().HasAnyResults() {
		t.Fatal("a new window has results")
	}
	r2.win.restoreTesting(state)
	r2.run(command.ViewContainerTesting, nil)
	r2.waitFor("the results to show", func() bool { return r2.win.Keys.Get("testing.hasAnyResults") == true })
	if got := r2.state("calc_test.go", "TestDiv"); got != coretesting.StateFailed {
		t.Errorf("TestDiv is %v after the restore", got)
	}
	if got, _ := r2.win.Views.Badge(command.ViewContainerTesting); got.Count != 2 {
		t.Errorf("the badge is %+v after the restore", got)
	}
	// a state that cannot be used is logged and changes nothing
	r2.win.restoreTesting([]byte(`{"version": 99}`))
	if !r2.svc().HasAnyResults() {
		t.Error("a damaged state took the results away")
	}
}

func TestAChangeOfAFileRunsTheContinuousTestsAgain(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	test := id("calc_test.go", "TestAdd")
	r.run(command.TestingToggleContinuous, test)
	r.waitFor("the first run", func() bool { return len(r.svc().Results()) > 0 && r.svc().Results()[0].IsComplete() })
	n := len(r.svc().Results())
	file := filepath.Join(r.folder, "calc_test.go")
	if err := os.WriteFile(file, []byte(testingSource+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.win.onFileEvents(context.Background(), []workspace.Event{{Kind: workspace.EventChanged, Path: file}})
	r.waitFor("the run after the change", func() bool { return len(r.svc().Results()) > n })
	r.run(command.TestingStopContinuous, nil)
}

func TestAClientsViewShowsTheHostsTestsAndRunsThemOnTheHost(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	v, err := r.win.makeHeadlessView(extension.Size{Width: 900, Height: 560}, rigFrameRate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	var frames atomic.Int64
	v.OnFrame(func(*image.RGBA) { frames.Add(1) })
	r.waitFor("the view to draw", func() bool { return frames.Load() > 0 && v.ws.shell != nil && v.ws.shell.testing.explorer != nil })
	client := v.ws
	if client.TestingService() != r.svc() {
		t.Fatal("the client's view has a test service of its own")
	}
	// the client runs the tests with the commands of its own view; the host's window shows the one result
	runOn(t, client, command.ViewContainerTesting)
	runOn(t, client, command.TestingRunAll)
	r.waitFor("the run on both screens", func() bool {
		rs := r.svc().Results()
		return len(rs) == 1 && rs[0].IsComplete() && client.Keys.Get("testing.hasAnyResults") == true && r.win.Keys.Get("testing.hasAnyResults") == true
	})
	for _, w := range []*Workspace{r.win, client} {
		ex := w.shell.testing.explorer
		var withDuration int
		for _, n := range ex.Model().Rows() {
			if e, ok := n.Data.(interface{ HasDuration() bool }); ok && e.HasDuration() {
				withDuration++
			}
		}
		if withDuration == 0 {
			t.Errorf("a screen shows no durations")
		}
		if got, _ := w.Views.Badge(command.ViewContainerTesting); got.Count != 2 {
			t.Errorf("a screen has the badge %+v", got)
		}
	}
	// what one screen hides in its explorer is its own
	runOn(t, client, command.TestingClearResults)
	r.waitFor("the results to go from both screens", func() bool {
		return r.win.Keys.Get("testing.hasAnyResults") == false && client.Keys.Get("testing.hasAnyResults") == false
	})
}

func TestTheTestingAPIIsThereForAnExtensionOfTheWindow(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	if r.win.Testing == nil || r.win.TestingService() == nil {
		t.Fatal("the window has no testing host")
	}
	ctrls := r.svc().Controllers()
	if len(ctrls) != 1 || ctrls[0].ID != "dev.testing.fake" || ctrls[0].Label != "Fake Tests (dev)" {
		t.Errorf("controllers: %+v", ctrls)
	}
	if got := len(r.svc().Profiles("dev.testing.fake")); got != 3 {
		t.Errorf("%d profiles, want Run, Debug and Coverage", got)
	}
	// the state of a window that has no results saves as nothing worth keeping
	if r.win.Testing.Service() != r.svc() {
		t.Error("the service of the host is not the window's")
	}
}
