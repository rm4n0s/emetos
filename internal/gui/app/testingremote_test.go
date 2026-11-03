package app

import (
	"context"
	"image"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang"
	"github.com/rm4n0s/emetos/internal/core/command"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	testview "github.com/rm4n0s/emetos/internal/gui/views/testing"
)

// Remote Work and testing (D46, SPEC §10.32 "Remote Work", step 26.10): a client of a hosted workspace runs the tests with the commands of
// its own view, and the host's window and the client's both show the one result of the workspace: the states in the Test Explorer, the
// run in Test Results, the summary and the badge. The first test uses the fake controller of extensions/devtesting; the second the Go
// extension with the real go on the Go fixture of the proofs (it is skipped when go is not installed).

// testingClient makes the headless view of a client of the window and waits until it draws its Testing views.
func testingClient(t *testing.T, win *Workspace, wait func(string, func() bool)) *Workspace {
	t.Helper()
	v, err := win.makeHeadlessView(extension.Size{Width: 900, Height: 560}, rigFrameRate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	var frames atomic.Int64
	v.OnFrame(func(*image.RGBA) { frames.Add(1) })
	wait("the client's view to draw", func() bool {
		return frames.Load() > 0 && v.ws.shell != nil && v.ws.shell.testing.explorer != nil && v.ws.shell.testing.results != nil
	})
	if v.ws.TestingService() != win.TestingService() {
		t.Fatal("the client's view has a testing service of its own")
	}
	return v.ws
}

// explorerRow finds the row of the test with the label in the Test Explorer of a window.
func explorerRow(w *Workspace, label string) *testview.Element {
	var find func(es []*testview.Element) *testview.Element
	find = func(es []*testview.Element) *testview.Element {
		for _, e := range es {
			if e.Label == label && e.Kind != testview.KindError {
				return e
			}
			if f := find(e.Children); f != nil {
				return f
			}
		}
		return nil
	}
	return find(w.shell.testing.explorer.Roots())
}

func TestAClientRunsTheTestsAndTheHostAndTheClientSeeTheSameResult(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	client := testingClient(t, r.win, r.waitFor)
	for name, w := range map[string]*Workspace{"host": r.win, "client": client} {
		if e := explorerRow(w, "TestDiv"); e == nil || e.State != coretesting.StateUnset {
			t.Fatalf("the %s's Test Explorer shows %+v before the run", name, e)
		}
	}

	// the client runs everything with its own command; the run is the workspace's
	runOn(t, client, command.TestingRunAll)
	r.finished()
	results := r.svc().Results()
	if len(results) != 1 {
		t.Fatalf("%d results after the client's run, want 1", len(results))
	}
	want := map[string]coretesting.State{
		"TestAdd": coretesting.StatePassed, "TestSub": coretesting.StatePassed, "TestDiv": coretesting.StateFailed,
		"TestRoot": coretesting.StateSkipped, "TestNumber": coretesting.StatePassed, "TestNested": coretesting.StateFailed,
	}
	for name, w := range map[string]*Workspace{"host": r.win, "client": client} {
		r.waitFor("the states in the "+name+"'s Test Explorer", func() bool {
			for label, state := range want {
				if e := explorerRow(w, label); e == nil || e.State != state {
					return false
				}
			}
			return true
		})
		r.waitFor("the run in the "+name+"'s Test Results", func() bool { return len(w.shell.testing.results.Model().Rows()) > 0 })
	}
	// both screens read the same summary and the same badge
	hostSum := testview.SummaryOf(r.win.TestingService(), r.win.Process.testingElapsed)
	clientSum := testview.SummaryOf(client.TestingService(), client.Process.testingElapsed)
	if hostSum.Text == "" || hostSum.Text != clientSum.Text || hostSum.Counts != clientSum.Counts {
		t.Errorf("the summaries differ: host %q, client %q", hostSum.Text, clientSum.Text)
	}
	for name, w := range map[string]*Workspace{"host": r.win, "client": client} {
		if got, _ := w.Views.Badge(command.ViewContainerTesting); got.Count != 2 {
			t.Errorf("the %s's badge is %+v, want 2 failed tests", name, got)
		}
	}

	// the host reruns the failed tests: the client sees the second result
	runOn(t, r.win, command.TestingReRunFailTests)
	r.waitFor("the second run", func() bool {
		rs := r.svc().Results()
		return len(rs) == 2 && rs[0].IsComplete() && !r.svc().IsRunning()
	})
	if got := client.TestingService().Results(); len(got) != 2 {
		t.Errorf("the client's service has %d results after the host's rerun", len(got))
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

// realGoRig is a window over a folder of the Go fixture with the Go extension and no fake controller, the real go in PATH.
type realGoRig struct {
	wait time.Duration
	// activated is when the Go extension was started.
	activated time.Time
	t         *testing.T
	win       *Workspace
	ts        *gluttertest.Tester
	folder    string
}

func (r *realGoRig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(r.wait)
	for !cond() {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
		r.ts.FlushPosted()
		r.ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	for range 4 {
		r.ts.Pump(20 * time.Millisecond)
	}
}

func newRealGoRig(t *testing.T) *realGoRig {
	t.Helper()
	return newRealGoRigIn(t, func(folder string) {
		copyTree(t, filepath.Join(testutil.RepoRoot(t), "testdata", "proof", "26.5", "project"), folder)
	}, 120*time.Second, nil)
}

// newRealGoRigIn is newRealGoRig over a folder that fill makes; waits is how long it waits for a condition and more are settings.
func newRealGoRigIn(t *testing.T, fill func(folder string), waits time.Duration, more map[string]any) *realGoRig {
	t.Helper()
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not installed")
	}
	// go runs as it does in the tests of the Go extension: a build cache of the test, no network
	for k, v := range map[string]string{
		"PATH": filepath.Dir(goPath), "GOCACHE": t.TempDir(), "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOFLAGS": "", "GOENV": "off",
		"GOWORK": "off", "CGO_ENABLED": "0", "TMPDIR": t.TempDir(),
	} {
		t.Setenv(k, v)
	}
	folder := t.TempDir()
	fill(folder)
	paths := testutil.Isolate(t)
	values := map[string]any{"files.hotExit": "off", "editor.minimap.enabled": false, "breadcrumbs.enabled": false,
		"editor.fontSize": 14, "window.menuBarVisibility": "classic"}
	for k, v := range more {
		values[k] = v
	}
	testutil.Settings(t, paths, values)
	p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{golang.New}})
	win, err := NewWorkspace(context.Background(), p, folder)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(win.release)
	ts := testutil.New(t, testWindowSize)
	ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	r := &realGoRig{t: t, win: win, ts: ts, folder: folder, wait: waits}
	r.activated = time.Now()
	win.Extensions().Activate(golang.ID)
	r.waitFor("the Go controller's tests", func() bool {
		svc := win.TestingService()
		return svc != nil && svc.ItemCount("go") > 0
	})
	return r
}

func TestAClientRunsTheGoFixtureWithTheRealGoAndTheHostSeesItsResult(t *testing.T) {
	r := newRealGoRig(t)
	client := testingClient(t, r.win, r.waitFor)
	svc := r.win.TestingService()

	runOn(t, client, command.TestingRunAll)
	r.waitFor("the run to end", func() bool {
		rs := svc.Results()
		return len(rs) == 1 && rs[0].IsComplete() && !svc.IsRunning()
	})
	// the subtests of TestTotal that are not named by a literal are added when go reports them, on both screens
	want := map[string]coretesting.State{
		"TestTotal": coretesting.StatePassed, "TestDiscount": coretesting.StateFailed, "TestLabel": coretesting.StatePassed,
		"one_item": coretesting.StatePassed, "three_items": coretesting.StatePassed,
	}
	for name, w := range map[string]*Workspace{"host": r.win, "client": client} {
		r.waitFor("the states in the "+name+"'s Test Explorer", func() bool {
			for label, state := range want {
				if e := explorerRow(w, label); e == nil || e.State != state {
					return false
				}
			}
			return true
		})
	}
	// the failure's message is the host's result, read by the client's service
	found := false
	for _, tr := range client.TestingService().Results()[0].Tests() {
		for _, task := range tr.Tasks {
			for _, m := range task.Messages {
				if strings.Contains(m.Text, "Discount(200, 15)") && m.HasDiff && m.Expected == "175" && m.Actual == "170" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("the failure message of TestDiscount is not in the result the client reads")
	}
	if got, _ := client.Views.Badge(command.ViewContainerTesting); got.Count != 1 {
		t.Errorf("the client's badge is %+v, want 1 failed test", got)
	}
}
