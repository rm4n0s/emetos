package httpclient

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/checks"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/rundata"
)

// showRunner shows the runner tab in a window of the theme.
func (r *rig) showRunner(th uitest.Theme, t *runTab, w, h float64) *gluttertest.Tester {
	r.t.Helper()
	changed := foundation.NewChangeNotifier()
	t.tab = refreshTab{t.tab, changed}
	body := widgets.NewListenableBuilder(changed, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget { return t.build(bc) }, nil)
	return uitest.Pump(r.t, th, gui.Size{Width: w, Height: h}, body)
}

func TestTheRunButtonRunsAndTheTableShowsEachResult(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow")
	ts := r.showRunner(uitest.Themes()[0], tab, 980, 560)
	ts.Pump(time.Millisecond)
	for _, want := range []string{"Run Flow", "Login", "Profile", "Logout", "Admin / Audit", "Requests (4 of 4)", "Run sends 4 requests in order"} {
		if !ts.Exists(gluttertest.ByTextContaining(want, rich)) {
			t.Errorf("the tab before the run does not show %q", want)
		}
	}
	ts.Tap(gluttertest.First(gluttertest.ByText("Run", rich)))
	r.api.WaitFor("the run to end", func() bool { return !tab.running && r.ext.pending.Load() == 0 })
	r.settle()
	ts.Pump(time.Millisecond)
	for _, want := range []string{"Completed", "4 requests", "3 checks passed, 0 failed", "200 OK", "204 No Content", "2/2"} {
		if !ts.Exists(gluttertest.ByTextContaining(want, rich)) {
			t.Errorf("the results do not show %q", want)
		}
	}
	if len(tab.results) != 4 {
		t.Fatalf("%d results", len(tab.results))
	}
	// a row opens to its details and closes again
	if ts.Exists(gluttertest.ByTextContaining("Extracted token", rich)) {
		t.Error("a row is open before it was asked for")
	}
	ts.Tap(gluttertest.First(iconButton("Show the details")))
	ts.Pump(time.Millisecond)
	for _, want := range []string{"Extracted token = tok-nobody", "POST http://127.0.0.1:", "/login"} {
		if !ts.Exists(gluttertest.ByTextContaining(want, rich)) {
			t.Errorf("the details do not show %q", want)
		}
	}
	ts.Tap(gluttertest.First(iconButton("Show the details")))
	ts.Pump(time.Millisecond)
	if ts.Exists(gluttertest.ByTextContaining("Extracted token", rich)) {
		t.Error("the row did not close")
	}
}

func TestTheCancelButtonStopsARunThatIsShown(t *testing.T) {
	srv := newFlowServer(t)
	srv.hold = "/profile/"
	t.Cleanup(func() { close(srv.release) })
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow")
	ts := r.showRunner(uitest.Themes()[0], tab, 980, 560)
	ts.Tap(gluttertest.First(gluttertest.ByText("Run", rich)))
	select {
	case <-srv.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("the profile never arrived")
	}
	r.api.WaitFor("the first result", func() bool { return len(tab.results) == 1 })
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByTextContaining("Running...", rich)) || !tab.running {
		t.Error("the tab does not say it is running")
	}
	ts.Tap(gluttertest.First(gluttertest.ByText("Cancel", rich)))
	r.api.WaitFor("the run to end", func() bool { return !tab.running && r.ext.pending.Load() == 0 })
	ts.Pump(time.Millisecond)
	if tab.end != runCanceled || !ts.Exists(gluttertest.ByText("Canceled", rich)) {
		t.Errorf("end %v", tab.end)
	}
	if got := strings.Join(srv.paths(), " "); got != "/login /profile/tok-nobody" {
		t.Errorf("the server got %s", got)
	}
}

func TestOneIterationAtATimeCanBeShown(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	writeFile(t, filepath.Join(r.folder, "rows.csv"), "user\nAda\nGrace\nLinus\n")
	tab := r.openRunner("flow/admin")
	tab.data.SetValue("rows.csv")
	r.runAndWait(tab)
	ts := r.showRunner(uitest.Themes()[0], tab, 980, 560)
	ts.Pump(time.Millisecond)
	for _, want := range []string{"Iteration 1 of 3", "Iteration 2 of 3", "Iteration 3 of 3", "user=Grace"} {
		if !ts.Exists(gluttertest.ByTextContaining(want, rich)) {
			t.Errorf("the table does not show %q", want)
		}
	}
	tab.filter = 2
	tab.tab.Refresh()
	ts.Pump(time.Millisecond)
	if ts.Exists(gluttertest.ByTextContaining("Iteration 1 of 3", rich)) || !ts.Exists(gluttertest.ByTextContaining("Iteration 2 of 3", rich)) {
		t.Error("the picker did not narrow the table to the second iteration")
	}
	if got := len(tab.lines()); got != 2 {
		t.Errorf("%d lines", got)
	}
	// the iterations box shows the rows of the file and cannot be edited
	if !ts.Exists(gluttertest.ByTextContaining("3 rows: user", rich)) {
		t.Error("the data file's rows are not shown")
	}
}

func TestTheUrlOfAResultHasItsSecretsMasked(t *testing.T) {
	srv := newFlowServer(t)
	r := flowRig(t, srv, map[string]string{"admin/audit.request.jsonc": strings.Replace(flowAudit, "/audit", "/audit?api_key=hunter2", 1)})
	tab := r.openRunner("flow/admin")
	r.runAndWait(tab)
	if len(tab.results) != 1 || strings.Contains(tab.results[0].URL, "hunter2") || !strings.Contains(tab.results[0].URL, "api_key=") {
		t.Errorf("url = %q", tab.results[0].URL)
	}
}

// ---- goldens ----

const runW, runH = 980.0, 520.0

func passedCheck(what, expected, actual string) checks.Result {
	return checks.Result{Passed: true, What: what, Expected: expected, Actual: actual}
}

// demoResult is a result for the goldens.
func demoResult(it, item int, name, method, url string, status int, text string, ms int, size int64, list ...checks.Result) runResult {
	return runResult{
		Iteration: it, Item: item, Name: name, Method: method, URL: url, Status: status, StatusText: text,
		Duration: time.Duration(ms) * time.Millisecond, Size: size, Checks: list,
	}
}

// runnerRig is the flow collection with a data file of three rows loaded into the tab.
func runnerRig(t *testing.T, prepare func(tab *runTab)) (*rig, *runTab) {
	t.Helper()
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	tab := r.openRunner("flow")
	tab.data.SetValue("data/users.csv")
	tab.table = &rundata.Table{Columns: []string{"user", "role"}, Rows: []map[string]string{
		{"user": "Ada", "role": "admin"}, {"user": "Grace", "role": "support"}, {"user": "Linus", "role": "dev"},
	}}
	tab.dataFor = tab.dataPath()
	tab.stop = true
	if prepare != nil {
		prepare(tab)
	}
	return r, tab
}

const demoBase = "http://localhost:8080"

func demoRun(tab *runTab, iterations int) {
	tab.total, tab.name, tab.envName = iterations, "Flow", "Dev"
	tokenRule := format.ExtractRule{From: "body", Path: "$.token", To: "token"}
	for it := 1; it <= iterations; it++ {
		login := demoResult(it, 0, "Login", "POST", demoBase+"/login", 200, "OK", 84, 312, passedCheck("status", "equals 200", "200"))
		login.Extracted = []extractOutcome{{Scope: scopeEnvironment, Stored: true, Extracted: checks.Extracted{Rule: tokenRule, Value: "tok-Ada"}}}
		profile := demoResult(it, 1, "Profile", "GET", demoBase+"/profile/tok-Ada", 200, "OK", 41, 1536, passedCheck("status", "equals 200", "200"), passedCheck("json $.name", "exists", "Ada"))
		logout := demoResult(it, 2, "Logout", "DELETE", demoBase+"/logout/tok-Ada", 204, "No Content", 27, 0)
		tab.results = append(tab.results, login, profile, logout)
	}
}

func goldenRunner(t *testing.T, name string, prepare func(tab *runTab)) {
	t.Helper()
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r, tab := runnerRig(t, prepare)
			ts := r.showRunner(th, tab, runW, runH)
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, "runner_"+name+"_"+th.Name)
		})
	}
}

func TestGoldenTheRunnerBeforeARun(t *testing.T) { goldenRunner(t, "before", nil) }

func TestGoldenTheRunnerDuringARun(t *testing.T) {
	goldenRunner(t, "during", func(tab *runTab) {
		demoRun(tab, 3)
		tab.results = tab.results[:4] // the second iteration is on its first request
		tab.running = true
	})
}

func TestGoldenTheRunnerAfterARun(t *testing.T) {
	goldenRunner(t, "after", func(tab *runTab) {
		demoRun(tab, 2)
		bad := &tab.results[4] // the profile of the second iteration
		bad.Status, bad.StatusText, bad.Size = 403, "Forbidden", 24
		bad.Checks = []checks.Result{
			{Passed: false, What: "status", Expected: "equals 200", Actual: "403"},
			{Passed: false, What: "json $.name", Expected: "exists", Actual: "", Problem: "the path $.name matches nothing"},
		}
		bad.Notes = []string{"No value for locale"}
		tab.end = runCompleted
		tab.open[4] = true
		tab.open[0] = true
	})
}

func TestGoldenTheRunnerStoppedOnAFailure(t *testing.T) {
	goldenRunner(t, "stopped", func(tab *runTab) {
		demoRun(tab, 3)
		tab.results = tab.results[:5]
		bad := &tab.results[4]
		bad.Status, bad.StatusText = 500, "Internal Server Error"
		bad.Checks = []checks.Result{{Passed: false, What: "status", Expected: "equals 200", Actual: "500"}}
		tab.end = runStopped
		tab.filter = 0
	})
}

func TestGoldenTheRunnerWithABadDataFile(t *testing.T) {
	goldenRunner(t, "bad_data", func(tab *runTab) {
		tab.table = nil
		tab.dataErr = "line 3 of the CSV file is not valid: wrong number of fields"
		tab.skip[tab.requests()[1].node.Path] = true
	})
}

func TestTheContextMenuOfACollectionAndAFolderRunsThem(t *testing.T) {
	for _, tc := range []struct{ rel, entry string }{{"flow", "Run Collection..."}, {"flow/admin", "Run Folder..."}} {
		t.Run(tc.entry, func(t *testing.T) {
			srv := newFlowServer(t)
			r := flowRig(t, srv, nil)
			r.ext.collections.tree.Expand(r.col("flow"))
			r.ext.collections.rebuild()
			ts := r.show(uitest.Themes()[0], CollectionsViewID, 360, 360)
			r.ext.collections.onContextMenu(r.col(tc.rel), gui.Offset{Dx: 60, Dy: 40})
			ts.Pump(time.Millisecond)
			ts.Pump(time.Millisecond)
			if !ts.Exists(gluttertest.ByText(tc.entry)) {
				t.Fatalf("the menu has no %q", tc.entry)
			}
			ts.Tap(gluttertest.ByText(tc.entry))
			ts.Pump(time.Millisecond)
			if len(r.ext.runTabs) != 1 || r.ext.runTabs[0].path != r.col(tc.rel) {
				t.Errorf("runner tabs = %d", len(r.ext.runTabs))
			}
		})
	}
	// a request has no such entry
	srv := newFlowServer(t)
	r := flowRig(t, srv, nil)
	ts := r.show(uitest.Themes()[0], CollectionsViewID, 360, 360)
	r.ext.collections.tree.Expand(r.col("flow"))
	r.ext.collections.rebuild()
	ts.Pump(time.Millisecond)
	r.ext.collections.onContextMenu(r.col("flow/login.request.jsonc"), gui.Offset{Dx: 60, Dy: 40})
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if ts.Exists(gluttertest.ByTextContaining("Run ", rich)) {
		t.Error("a request has a Run entry")
	}
}
