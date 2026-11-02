package exthost

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
)

// The Testing API of the extensions (step 26.1): each call through the host, against a fake controller, with the model
// (internal/core/testing) as the one that sees the results.

const testingWait = 10 * time.Second

func waitTesting(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testingWait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// testingRig is a tab with the extensions "tester" and "other", both active from the start.
type testingRig struct {
	*tab
	r     *rig
	t     *testing.T
	api   extension.API
	other extension.API
	svc   *coretesting.Service
	ctl   extension.TestController
}

func newTestingRig(t *testing.T, noHost bool) *testingRig {
	t.Helper()
	g := &testingRig{t: t}
	f1, _ := factory(manifest("tester"), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error { g.api = api; return nil }
	})
	f2, _ := factory(manifest("other"), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error { g.other = api; return nil }
	})
	g.r = newRig(t, f1, f2)
	g.r.noTesting["w"] = noHost
	g.tab = g.r.newTab("w")
	g.scope.Start()
	if g.testing != nil {
		g.svc = g.testing.Service()
	}
	return g
}

// withController makes the controller "fake" with the tree
//
//	pkg: a, b, sub: x
//	other
func (g *testingRig) withController() *testingRig {
	g.t.Helper()
	ctl, err := g.api.Tests().CreateTestController("fake", "Fake")
	if err != nil {
		g.t.Fatal(err)
	}
	g.ctl = ctl
	add := func(parent *extension.TestItem, id string, uri string) *extension.TestItem {
		it := ctl.CreateTestItem(id, id, uri)
		coll := ctl.Items()
		if parent != nil {
			coll = parent.Children()
		}
		if err := coll.Add(it); err != nil {
			g.t.Fatal(err)
		}
		return it
	}
	pkg := add(nil, "pkg", "file:///w/pkg")
	add(pkg, "a", "file:///w/pkg/a_test.go")
	add(pkg, "b", "file:///w/pkg/b_test.go")
	sub := add(pkg, "sub", "file:///w/pkg/sub")
	add(sub, "x", "file:///w/pkg/sub/x_test.go")
	add(nil, "other", "file:///w/other_test.go")
	return g
}

func fid(path ...string) string { return coretesting.JoinID("fake", path...) }

// item finds an item of the controller by its path.
func (g *testingRig) item(path ...string) *extension.TestItem {
	g.t.Helper()
	coll := g.ctl.Items()
	var it *extension.TestItem
	for _, p := range path {
		if it = coll.Get(p); it == nil {
			g.t.Fatalf("no item %v", path)
		}
		coll = it.Children()
	}
	return it
}

func (g *testingRig) runGroup(ctx context.Context, group coretesting.Group, tests []string, exclude ...string) (*coretesting.Result, error) {
	req := g.svc.ResolveGroup(coretesting.AmbiguousRequest{Group: group, Tests: tests, Exclude: exclude})
	return g.svc.RunTests(ctx, req)
}

func tagOf(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func TestTheTreeOfAControllerReachesTheModel(t *testing.T) {
	g := newTestingRig(t, false).withController()
	svc := g.svc
	if got := svc.Controllers(); len(got) != 1 || got[0].ID != "fake" || got[0].Label != "Fake" {
		t.Fatalf("controllers %+v", got)
	}
	top := svc.Children("fake")
	if len(top) != 2 || top[0].ID != fid("pkg") || top[1].ID != fid("other") || top[0].URI != "file:///w/pkg" || top[0].ParentID != "fake" {
		t.Fatalf("top items %+v", top)
	}
	if kids := svc.Children(fid("pkg")); len(kids) != 3 || kids[2].ID != fid("pkg", "sub") {
		t.Errorf("children of pkg %+v", kids)
	}
	if _, ok := svc.Item(fid("pkg", "sub", "x")); !ok {
		t.Error("the grandchild is not in the model")
	}

	// a change of the properties of an item
	g.item("pkg", "a").Update(func(d *extension.TestItemData) {
		d.Label, d.Description, d.SortText = "A", "the a test", "0"
		d.Tags = []string{"slow"}
		d.Range = &extension.Range{Start: extension.Position{Line: 3}, End: extension.Position{Line: 9, Col: 1}}
		d.Error, d.Busy, d.CanResolveChildren = "oops", true, true
	})
	a, _ := svc.Item(fid("pkg", "a"))
	if a.Label != "A" || a.Description != "the a test" || a.SortText != "0" || !a.HasTag("slow") || a.Range == nil || a.Range.End.Line != 9 || a.Error != "oops" || !a.Busy || !a.CanResolveChildren {
		t.Errorf("the item in the model: %+v", a)
	}

	// a subtree made before it is added arrives whole, parents first
	grp := g.ctl.CreateTestItem("grp", "grp", "")
	for _, id := range []string{"1", "2"} {
		if err := grp.Children().Add(g.ctl.CreateTestItem(id, id, "")); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.ctl.Items().Add(grp); err != nil {
		t.Fatal(err)
	}
	if len(svc.Children(fid("grp"))) != 2 {
		t.Error("the subtree was not added with its children")
	}
	// an item added below an item of the tree
	if err := grp.Children().Add(g.ctl.CreateTestItem("3", "3", "")); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.Item(fid("grp", "3")); !ok {
		t.Error("a child added later is not in the model")
	}
	// deleting takes the subtree out
	g.ctl.Items().Delete("grp")
	if _, ok := svc.Item(fid("grp", "1")); ok || svc.ItemCount("fake") != 6 {
		t.Errorf("the deleted subtree is in the model: %d items", svc.ItemCount("fake"))
	}
	// replace
	if err := g.item("pkg").Children().Replace([]*extension.TestItem{g.item("pkg", "b"), g.ctl.CreateTestItem("c", "c", "")}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range svc.Children(fid("pkg")) {
		ids = append(ids, it.ID)
	}
	if strings.Join(ids, ",") != strings.Join([]string{fid("pkg", "b"), fid("pkg", "c")}, ",") {
		t.Errorf("after the replacement %q", ids)
	}
	g.ctl.SetLabel("Renamed")
	if svc.Controllers()[0].Label != "Renamed" || g.ctl.Label() != "Renamed" {
		t.Error("SetLabel")
	}
}

func TestARunThroughTheHostReportsToTheResults(t *testing.T) {
	g := newTestingRig(t, false).withController()
	var gotReq *extension.TestRunRequest
	prof, err := g.ctl.CreateRunProfile("Run", extension.TestKindRun, func(ctx context.Context, req *extension.TestRunRequest) error {
		gotReq = req
		run, err := g.ctl.CreateTestRun(req, "fake run", true)
		if err != nil {
			return err
		}
		defer run.End()
		a, b, x := g.item("pkg", "a"), g.item("pkg", "b"), g.item("pkg", "sub", "x")
		run.Enqueued(a)
		run.Enqueued(b)
		run.Enqueued(x)
		run.Started(a)
		run.Passed(a, 5*time.Millisecond)
		run.Started(b)
		run.AppendOutput("--- FAIL: b\r\n", &extension.Location{URI: "file:///w/pkg/b_test.go"}, b)
		run.Failed(b, []extension.TestMessage{{
			Message: "expected 1", ExpectedOutput: "1", ActualOutput: "2",
			Location:   &extension.Location{URI: "file:///w/pkg/b_test.go", Range: extension.Range{Start: extension.Position{Line: 7}}},
			StackTrace: []extension.TestStackFrame{{Label: "TestB", URI: "file:///w/pkg/b_test.go", Position: &extension.Position{Line: 7, Col: 1}}},
		}}, 7*time.Millisecond)
		run.Skipped(x)
		return nil
	}, extension.TestRunProfileOptions{IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	if prof.Label() != "Run" || prof.Kind() != extension.TestKindRun || !prof.IsDefault() {
		t.Errorf("profile %q %v %v", prof.Label(), prof.Kind(), prof.IsDefault())
	}
	res, err := g.runGroup(context.Background(), coretesting.GroupRun, []string{"fake"})
	if err != nil {
		t.Fatal(err)
	}
	if gotReq.Include != nil || len(gotReq.Exclude) != 0 || gotReq.Profile != prof || gotReq.Continuous {
		t.Errorf("a run of the controller's id is a request with no include: %+v", gotReq)
	}
	if !res.IsComplete() || len(res.Tasks()) != 1 || res.Tasks()[0].Name != "fake run" {
		t.Fatalf("result %v %+v", res.IsComplete(), res.Tasks())
	}
	want := map[string]struct {
		state coretesting.State
		dur   time.Duration
	}{
		fid("pkg", "a"): {coretesting.StatePassed, 5 * time.Millisecond}, fid("pkg", "b"): {coretesting.StateFailed, 7 * time.Millisecond},
		fid("pkg", "sub", "x"): {coretesting.StateSkipped, coretesting.NoDuration}, fid("pkg", "sub"): {coretesting.StateSkipped, coretesting.NoDuration},
		fid("pkg"): {coretesting.StateFailed, 12 * time.Millisecond},
	}
	for id, w := range want {
		got, ok := res.Test(id)
		if !ok || got.State != w.state || got.Duration != w.dur {
			t.Errorf("%q: %+v (%v), want %v %v", id, got.State, got.Duration, w.state, w.dur)
		}
	}
	if _, ok := res.Test(fid("other")); ok {
		t.Error("a test that was not reported is in the result")
	}
	b, _ := res.Test(fid("pkg", "b"))
	msgs := b.Tasks[0].Messages
	if len(msgs) != 2 || msgs[0].Type != coretesting.MessageOutput || msgs[1].Type != coretesting.MessageError || !msgs[1].HasDiff || msgs[1].Expected != "1" ||
		msgs[1].Location == nil || msgs[1].Location.Range.Start.Line != 7 || len(msgs[1].StackTrace) != 1 || msgs[1].StackTrace[0].Pos.Col != 1 {
		t.Errorf("messages of b: %+v", msgs)
	}
	if out, _ := res.Output(res.Tasks()[0].ID); string(out) != "--- FAIL: b\r\n" {
		t.Errorf("output %q", out)
	}
	if c := res.Counts(); c[coretesting.StatePassed] != 1 || c[coretesting.StateFailed] != 1 || c[coretesting.StateSkipped] != 1 {
		t.Errorf("counts %v", c)
	}
}

func TestIncludeAndExcludeReachTheHandler(t *testing.T) {
	g := newTestingRig(t, false).withController()
	var gotReq *extension.TestRunRequest
	if _, err := g.ctl.CreateRunProfile("Run", extension.TestKindRun, func(_ context.Context, req *extension.TestRunRequest) error {
		gotReq = req
		return nil
	}, extension.TestRunProfileOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.runGroup(context.Background(), coretesting.GroupRun, []string{fid("pkg"), fid("other")}, fid("pkg", "b"), fid("pkg", "sub", "x")); err != nil {
		t.Fatal(err)
	}
	ids := func(items []*extension.TestItem) string {
		var out []string
		for _, it := range items {
			out = append(out, it.ID())
		}
		return strings.Join(out, ",")
	}
	if ids(gotReq.Include) != "pkg,other" || ids(gotReq.Exclude) != "b,x" || gotReq.Include[0] != g.item("pkg") {
		t.Errorf("include %q exclude %q", ids(gotReq.Include), ids(gotReq.Exclude))
	}
	if _, err := g.runGroup(context.Background(), coretesting.GroupRun, []string{fid("other")}, fid("pkg", "b")); err != nil {
		t.Fatal(err)
	}
	if ids(gotReq.Include) != "other" || len(gotReq.Exclude) != 0 {
		t.Errorf("an exclusion that is not below an inclusion reached the handler: %q %q", ids(gotReq.Include), ids(gotReq.Exclude))
	}
}

func TestARunWaitsForTheRunsTheHandlerLeftGoingAndCancelIsSeenByTheHandler(t *testing.T) {
	g := newTestingRig(t, false).withController()
	started := make(chan struct{})
	var ctxSeen atomic.Pointer[context.Context]
	finish := make(chan struct{})
	if _, err := g.ctl.CreateRunProfile("Run", extension.TestKindRun, func(ctx context.Context, req *extension.TestRunRequest) error {
		ctxSeen.Store(&ctx)
		run, err := g.ctl.CreateTestRun(req, "", false)
		if err != nil {
			return err
		}
		run.Started(g.item("other"))
		close(started)
		go func() { // the handler returns; the run goes on
			<-finish
			run.Passed(g.item("other"), time.Millisecond)
			run.End()
		}()
		return nil
	}, extension.TestRunProfileOptions{}); err != nil {
		t.Fatal(err)
	}
	done := make(chan *coretesting.Result)
	go func() {
		res, _ := g.runGroup(context.Background(), coretesting.GroupRun, []string{"fake"})
		done <- res
	}()
	<-started
	select {
	case <-done:
		t.Fatal("the run ended with a task going")
	case <-time.After(50 * time.Millisecond):
	}
	if !g.svc.IsRunning() {
		t.Error("not running")
	}
	close(finish)
	res := <-done
	if st, _ := res.Test(fid("other")); st.State != coretesting.StatePassed {
		t.Errorf("state %v", st.State)
	}
	if res.Tasks()[0].Name != "Fake" {
		t.Errorf("the default name of a run is the controller's label, got %q", res.Tasks()[0].Name)
	}

	// a cancelled run: the handler's context ends
	started = make(chan struct{})
	finish = make(chan struct{}) // never closed: the run is ended by force
	go func() {
		res, _ := g.runGroup(context.Background(), coretesting.GroupRun, []string{"fake"})
		done <- res
	}()
	<-started
	g.svc.CancelRun(g.svc.Results()[0].ID())
	if res = <-done; res == nil {
		t.Fatal("no result")
	}
	if ctx := *ctxSeen.Load(); ctx.Err() == nil {
		t.Error("the context of the handler did not end with the cancel")
	}
	if st, _ := res.Test(fid("other")); st.State != coretesting.StateSkipped {
		t.Errorf("after the cancel the running test is %v, want skipped", st.State)
	}
	close(finish)
}

func TestAnExtensionMayStartARunOnItsOwn(t *testing.T) {
	g := newTestingRig(t, false).withController()
	prof, err := g.ctl.CreateRunProfile("Debug", extension.TestKindDebug, func(context.Context, *extension.TestRunRequest) error { return nil }, extension.TestRunProfileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	req := &extension.TestRunRequest{Include: []*extension.TestItem{g.item("pkg", "a")}, Exclude: nil, Profile: prof}
	run, err := g.ctl.CreateTestRun(req, "watcher", false)
	if err != nil {
		t.Fatal(err)
	}
	if !g.svc.IsRunning() {
		t.Error("the run is not running")
	}
	run.Passed(g.item("pkg", "a"), time.Millisecond)
	run.End()
	results := g.svc.Results()
	if len(results) != 1 || !results[0].IsComplete() || results[0].Persist() {
		t.Fatalf("results %d", len(results))
	}
	rq := results[0].Request()
	if rq.Group != coretesting.GroupDebug || len(rq.Targets) != 1 || rq.Targets[0].TestIDs[0] != fid("pkg", "a") || rq.Targets[0].ProfileID == 0 {
		t.Errorf("request %+v", rq)
	}
	data, err := g.svc.SaveState()
	if err != nil {
		t.Fatal(err)
	}
	var st struct{ Results []json.RawMessage }
	if err := json.Unmarshal(data, &st); err != nil || len(st.Results) != 0 {
		t.Errorf("a run with persist false was saved: %s", data)
	}
	// persist true is saved
	run2, _ := g.ctl.CreateTestRun(&extension.TestRunRequest{}, "kept", true)
	run2.Passed(g.item("other"), 0)
	run2.End()
	data, _ = g.svc.SaveState()
	if err := json.Unmarshal(data, &st); err != nil || len(st.Results) != 1 {
		t.Errorf("a run with persist true was not saved: %s", data)
	}
	// after End a run takes nothing
	run2.Passed(g.item("pkg", "b"), 0)
	if _, ok := g.svc.Results()[0].Test(fid("pkg", "b")); ok {
		t.Error("a run took a report after it ended")
	}
}

func TestRunProfiles(t *testing.T) {
	g := newTestingRig(t, false).withController()
	noop := func(context.Context, *extension.TestRunRequest) error { return nil }
	configured := 0
	run, _ := g.ctl.CreateRunProfile("Run", extension.TestKindRun, noop, extension.TestRunProfileOptions{ConfigureHandler: func() { configured++ }})
	dbg, _ := g.ctl.CreateRunProfile("Debug", extension.TestKindDebug, noop, extension.TestRunProfileOptions{IsDefault: true, Tag: "unit", SupportsContinuousRun: true})
	cov, _ := g.ctl.CreateRunProfile("Coverage", extension.TestKindCoverage, noop, extension.TestRunProfileOptions{})
	infos := g.svc.Profiles("fake")
	groups := map[string]coretesting.Group{}
	for _, p := range infos {
		groups[p.Label] = p.Group
	}
	if groups["Run"] != coretesting.GroupRun || groups["Debug"] != coretesting.GroupDebug || groups["Coverage"] != coretesting.GroupCoverage {
		t.Errorf("groups %v", groups)
	}
	if !dbg.IsDefault() || dbg.Tag() != "unit" || !dbg.SupportsContinuousRun() || run.IsDefault() {
		t.Error("the options of the profiles")
	}
	cov.SetIsDefault(true)
	if !cov.IsDefault() {
		t.Error("SetIsDefault")
	}
	// the user chooses another default for the group: the profile reads it from the model
	g.svc.SetGroupDefaults(coretesting.GroupCoverage, nil)
	if cov.IsDefault() {
		t.Error("the user's choice is not seen by the profile")
	}
	run.SetLabel("Run all")
	if run.Label() != "Run all" || g.svc.Profiles("fake")[0].Label == "" {
		t.Error("SetLabel")
	}
	found := false
	for _, p := range g.svc.Profiles("fake") {
		found = found || p.Label == "Run all"
	}
	if !found {
		t.Error("the model has the old label")
	}
	for _, p := range g.svc.Profiles("fake") {
		if p.Label == "Run all" {
			if err := g.svc.ConfigureProfile(p.Ref()); err != nil || configured != 1 {
				t.Errorf("configure %v %d", err, configured)
			}
		}
	}
	dbg.Dispose()
	cov.Dispose()
	if n := len(g.svc.Profiles("fake")); n != 1 {
		t.Errorf("%d profiles after disposing two", n)
	}

	for name, fn := range map[string]func() error{
		"no label": func() error {
			_, err := g.ctl.CreateRunProfile("", extension.TestKindRun, noop, extension.TestRunProfileOptions{})
			return err
		},
		"no handler": func() error {
			_, err := g.ctl.CreateRunProfile("x", extension.TestKindRun, nil, extension.TestRunProfileOptions{})
			return err
		},
		"no kind": func() error {
			_, err := g.ctl.CreateRunProfile("x", 0, noop, extension.TestRunProfileOptions{})
			return err
		},
		"bad kind": func() error {
			_, err := g.ctl.CreateRunProfile("x", 9, noop, extension.TestRunProfileOptions{})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) { tagOf(t, fn(), "hostController.CreateRunProfile.ExtensionTestingInvalid") })
	}
	_, err := g.ctl.CreateTestRun(nil, "x", false)
	tagOf(t, err, "hostController.CreateTestRun.ExtensionTestingInvalid")
}

func TestResolveAndRefreshHandlers(t *testing.T) {
	g := newTestingRig(t, false).withController()
	var mu sync.Mutex
	var resolved []string
	g.ctl.SetResolveHandler(func(_ context.Context, item *extension.TestItem) error {
		mu.Lock()
		defer mu.Unlock()
		if item == nil {
			resolved = append(resolved, "<root>")
		} else {
			resolved = append(resolved, item.ID())
		}
		return nil
	})
	refreshed := 0
	g.ctl.SetRefreshHandler(func(context.Context) error { refreshed++; return nil })
	ctx := context.Background()
	for _, id := range []string{"fake", fid("pkg", "sub"), fid("pkg", "gone")} {
		if err := g.svc.ResolveItem(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(resolved, ","); got != "<root>,sub" {
		t.Errorf("resolved %q: the root as nil, an item as itself, an item that went away not at all", got)
	}
	if err := g.svc.RefreshTests(ctx, ""); err != nil || refreshed != 1 {
		t.Errorf("refresh %v %d", err, refreshed)
	}
	g.ctl.SetResolveHandler(nil)
	g.ctl.SetRefreshHandler(func(context.Context) error { return errors.New("TestRefreshFailed", "no") })
	if err := g.svc.ResolveItem(ctx, "fake"); err != nil {
		t.Errorf("a controller with no resolve handler: %v", err)
	}
	tagOf(t, g.svc.RefreshTests(ctx, "fake"), "TestRefreshFailed")
}

func TestARegistrationFailsLikeTheModelAndTheHost(t *testing.T) {
	t.Run("the id is taken, also by another extension", func(t *testing.T) {
		g := newTestingRig(t, false)
		first, err := g.api.Tests().CreateTestController("same", "Same")
		if err != nil {
			t.Fatal(err)
		}
		_, err = g.other.Tests().CreateTestController("same", "Other")
		tagOf(t, err, "testsAPI.CreateTestController.ExtensionTestingControllerTaken")
		first.Dispose() // once it is gone the id is free
		if _, err := g.other.Tests().CreateTestController("same", "Other"); err != nil {
			t.Errorf("the id stayed taken after the controller was disposed: %v", err)
		}
	})
	t.Run("invalid", func(t *testing.T) {
		g := newTestingRig(t, false)
		for _, c := range [][2]string{{"", "x"}, {"x", ""}, {"a\x00b", "x"}} {
			_, err := g.api.Tests().CreateTestController(c[0], c[1])
			tagOf(t, err, "testsAPI.CreateTestController.ExtensionTestingInvalid")
		}
	})
	t.Run("no service", func(t *testing.T) {
		g := newTestingRig(t, true)
		_, err := g.api.Tests().CreateTestController("x", "x")
		tagOf(t, err, "testsAPI.CreateTestController.ExtensionTestingUnavailable")
	})
	t.Run("after the activation ended", func(t *testing.T) {
		g := newTestingRig(t, false).withController()
		api, ctl := g.api, g.ctl
		g.scope.Close()
		_, err := api.Tests().CreateTestController("late", "Late")
		tagOf(t, err, "testsAPI.CreateTestController.ExtensionDeactivated")
		_, err = ctl.CreateRunProfile("x", extension.TestKindRun, func(context.Context, *extension.TestRunRequest) error { return nil }, extension.TestRunProfileOptions{})
		tagOf(t, err, "hostController.CreateRunProfile.ExtensionDeactivated")
		_, err = ctl.CreateTestRun(&extension.TestRunRequest{}, "x", false)
		tagOf(t, err, "hostController.CreateTestRun.ExtensionDeactivated")
	})
}

func TestTurningTheExtensionOffRemovesItsControllersAndEndsItsRuns(t *testing.T) {
	g := newTestingRig(t, false).withController()
	if _, err := g.ctl.CreateRunProfile("Run", extension.TestKindRun, func(context.Context, *extension.TestRunRequest) error { return nil }, extension.TestRunProfileOptions{}); err != nil {
		t.Fatal(err)
	}
	run, err := g.ctl.CreateTestRun(&extension.TestRunRequest{}, "going", false)
	if err != nil {
		t.Fatal(err)
	}
	run.Started(g.item("other"))
	if !g.svc.IsRunning() {
		t.Fatal("not running")
	}
	g.disabled = []string{"tester"}
	g.scope.Refresh()
	waitTesting(t, "the controller to go", func() bool { return len(g.svc.Controllers()) == 0 })
	if g.svc.IsRunning() {
		t.Error("the run of the extension that went is still running")
	}
	if len(g.svc.Profiles("")) != 0 || g.svc.ItemCount("fake") != 0 {
		t.Error("the profiles or the items stayed")
	}
	res := g.svc.Results()[0]
	if st, _ := res.Test(fid("other")); st.State != coretesting.StateSkipped {
		t.Errorf("the running test is %v, want skipped", st.State)
	}
}

func TestAPanicInAHandlerTakesTheExtensionOff(t *testing.T) {
	cases := []struct {
		name  string
		do    func(g *testingRig) error
		where string
	}{
		{"the run handler", func(g *testingRig) error {
			if _, err := g.ctl.CreateRunProfile("Boom", extension.TestKindRun, func(context.Context, *extension.TestRunRequest) error { panic("boom") }, extension.TestRunProfileOptions{}); err != nil {
				return err
			}
			_, err := g.runGroup(context.Background(), coretesting.GroupRun, []string{"fake"})
			return err
		}, "the run handler"},
		{"the resolve handler", func(g *testingRig) error {
			g.ctl.SetResolveHandler(func(context.Context, *extension.TestItem) error { panic("boom") })
			return g.svc.ResolveItem(context.Background(), "fake")
		}, "the resolve handler"},
		{"the refresh handler", func(g *testingRig) error {
			g.ctl.SetRefreshHandler(func(context.Context) error { panic("boom") })
			return g.svc.RefreshTests(context.Background(), "")
		}, "the refresh handler"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newTestingRig(t, false).withController()
			err := c.do(g)
			tagOf(t, err, "TestingHost.guard.func1.ExtensionTestingPanicked")
			waitTesting(t, "the extension to fail", func() bool { return g.scope.State("tester") == StateFailed })
			f := g.scope.Failure("tester")
			if f == nil || !strings.Contains(f.Where, c.where) {
				t.Errorf("failure %+v", f)
			}
			waitTesting(t, "the controller to go", func() bool { return len(g.svc.Controllers()) == 0 })
		})
	}
}

func TestContinuousRunsThroughTheHost(t *testing.T) {
	g := newTestingRig(t, false).withController()
	runs := make(chan *extension.TestRunRequest, 8)
	prof, err := g.ctl.CreateRunProfile("Run", extension.TestKindRun, func(_ context.Context, req *extension.TestRunRequest) error {
		runs <- req
		return nil
	}, extension.TestRunProfileOptions{SupportsContinuousRun: true, ContinuousTrigger: func(item *extension.TestItem, uri string) bool {
		return item.ID() == "pkg" && strings.HasSuffix(uri, ".go") && strings.HasPrefix(uri, "file:///w/pkg/")
	}})
	if err != nil {
		t.Fatal(err)
	}
	var ref coretesting.ProfileRef
	for _, p := range g.svc.Profiles("fake") {
		ref = p.Ref()
	}
	if err := g.svc.StartContinuous([]coretesting.ProfileRef{ref}, ""); err != nil {
		t.Fatal(err)
	}
	next := func() *extension.TestRunRequest {
		select {
		case r := <-runs:
			return r
		case <-time.After(testingWait):
			t.Fatal("no run")
			return nil
		}
	}
	first := next()
	if !first.Continuous || first.Include != nil || first.Profile != prof {
		t.Errorf("first run %+v", first)
	}
	if n := g.svc.FilesChanged([]string{"file:///w/pkg/lib.go"}); n != 1 {
		t.Fatalf("FilesChanged = %d", n)
	}
	second := next()
	if !second.Continuous || len(second.Include) != 1 || second.Include[0] != g.item("pkg") {
		t.Errorf("second run %+v", second)
	}
	if n := g.svc.FilesChanged([]string{"file:///w/other_test.go"}); n != 0 {
		t.Errorf("the provider said the file is not for any item, but %d runs started", n)
	}
	g.svc.StopContinuous("")
}

func TestCoverageThroughTheHost(t *testing.T) {
	g := newTestingRig(t, false).withController()
	type handle struct{ n int }
	var loads atomic.Int32
	_, err := g.ctl.CreateRunProfile("Coverage", extension.TestKindCoverage, func(ctx context.Context, req *extension.TestRunRequest) error {
		run, err := g.ctl.CreateTestRun(req, "cov", true)
		if err != nil {
			return err
		}
		defer run.End()
		run.Passed(g.item("pkg", "a"), 0)
		run.AddCoverage(extension.FileCoverage{URI: "file:///w/pkg/a.go", Statement: extension.CoverageCount{Covered: 3, Total: 4}, Branch: &extension.CoverageCount{Covered: 1, Total: 2}, Data: &handle{7}})
		run.AddCoverage(extension.FileCoverage{URI: "file:///w/pkg/b.go", Statement: extension.CoverageCount{Covered: 1, Total: 6}, Declaration: &extension.CoverageCount{Covered: 0, Total: 1}})
		return nil
	}, extension.TestRunProfileOptions{LoadDetailedCoverage: func(_ context.Context, f extension.FileCoverage) (extension.CoverageDetails, error) {
		loads.Add(1)
		h, _ := f.Data.(*handle)
		return extension.CoverageDetails{
			Statements:   []extension.StatementCoverage{{Executed: h.n, Location: extension.Range{Start: extension.Position{Line: 2}}, Branches: []extension.BranchCoverage{{Executed: 1, Label: "if"}}}},
			Declarations: []extension.DeclarationCoverage{{Name: "F", Executed: 2}},
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := g.runGroup(context.Background(), coretesting.GroupCoverage, []string{"fake"})
	if err != nil {
		t.Fatal(err)
	}
	cov := res.LatestCoverage()
	if cov == nil {
		t.Fatal("no coverage")
	}
	dir, ok := cov.Computed("file:///w/pkg")
	if !ok || dir.Statement.Covered != 4 || dir.Statement.Total != 10 || dir.Branch == nil || dir.Branch.Total != 2 || dir.Declaration == nil || dir.Declaration.Total != 1 {
		t.Errorf("sum of the folder %+v", dir)
	}
	d, err := cov.Details(context.Background(), "file:///w/pkg/a.go")
	if err != nil || len(d.Statements) != 1 || d.Statements[0].Executed != 7 || d.Statements[0].Branches[0].Label != "if" || d.Declarations[0].Name != "F" || d.Statements[0].Location.Start.Line != 2 {
		t.Errorf("details %+v %v", d, err)
	}
	if _, err := cov.Details(context.Background(), "file:///w/pkg/a.go"); err != nil || loads.Load() != 1 {
		t.Errorf("the details were loaded %d times", loads.Load())
	}
}

func TestInvalidateTestResultsRetiresTheTests(t *testing.T) {
	g := newTestingRig(t, false).withController()
	_, err := g.ctl.CreateRunProfile("Run", extension.TestKindRun, func(_ context.Context, req *extension.TestRunRequest) error {
		run, err := g.ctl.CreateTestRun(req, "r", true)
		if err != nil {
			return err
		}
		defer run.End()
		run.Passed(g.item("pkg", "a"), 0)
		run.Passed(g.item("other"), 0)
		return nil
	}, extension.TestRunProfileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := g.runGroup(context.Background(), coretesting.GroupRun, []string{"fake"})
	if err != nil {
		t.Fatal(err)
	}
	g.ctl.InvalidateTestResults(g.item("pkg"))
	retired := func(path ...string) bool { r, _ := res.Test(fid(path...)); return r.Retired }
	if !retired("pkg") || !retired("pkg", "a") || retired("other") {
		t.Error("invalidating pkg")
	}
	g.ctl.InvalidateTestResults()
	if !retired("other") {
		t.Error("invalidating all")
	}
	g.ctl.InvalidateTestResults(g.ctl.CreateTestItem("loose", "loose", "")) // an item in no tree: nothing happens
}

func TestAReportAboutAnItemThatIsNotInTheTreeIsIgnored(t *testing.T) {
	g := newTestingRig(t, false).withController()
	run, err := g.ctl.CreateTestRun(&extension.TestRunRequest{}, "r", false)
	if err != nil {
		t.Fatal(err)
	}
	loose := g.ctl.CreateTestItem("loose", "loose", "")
	run.Passed(loose, 0)
	run.AppendOutput("x", nil, loose)
	child := g.ctl.CreateTestItem("c", "c", "")
	if err := loose.Children().Add(child); err != nil {
		t.Fatal(err)
	}
	run.Failed(child, []extension.TestMessage{{Message: "m"}}, 0)
	run.End()
	res := g.svc.Results()[0]
	if len(res.Tests()) != 0 {
		t.Errorf("tests %+v", res.Tests())
	}
}

func TestTheHostServesItsScopeOnly(t *testing.T) {
	g := newTestingRig(t, false)
	g.r.host.NewScope(ScopeOptions{Testing: g.testing, Info: extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "second"}, Ctx: context.Background()}).Close()
	if _, err := g.api.Tests().CreateTestController("ok", "ok"); err != nil {
		t.Errorf("the first scope lost its host to the second: %v", err)
	}
}
