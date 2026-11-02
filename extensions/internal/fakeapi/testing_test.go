package fakeapi

import (
	"context"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
)

// The Testing API of the fake (step 26.1) has the rules of the host: the same errors, the controllers going with the activation, a
// panic in a handler taking the extension off.

func TestFakeTestingRegistrationsFailLikeTheHost(t *testing.T) {
	noop := func(context.Context, *extension.TestRunRequest) error { return nil }
	tests := []struct {
		name string
		do   func(a *API) error
		prep func(a *API)
		tag  string
	}{
		{"no id", func(a *API) error { _, err := a.Tests().CreateTestController("", "x"); return err }, nil, "ExtensionTestingInvalid"},
		{"no label", func(a *API) error { _, err := a.Tests().CreateTestController("x", ""); return err }, nil, "ExtensionTestingInvalid"},
		{"a NUL in the id", func(a *API) error { _, err := a.Tests().CreateTestController("a\x00b", "x"); return err }, nil, "ExtensionTestingInvalid"},
		{"the id is taken", func(a *API) error { _, err := a.Tests().CreateTestController("x", "x"); return err },
			func(a *API) {
				if _, err := a.Tests().CreateTestController("x", "x"); err != nil {
					panic(err)
				}
			}, "ExtensionTestingControllerTaken"},
		{"no service", func(a *API) error { _, err := a.Tests().CreateTestController("x", "x"); return err },
			func(a *API) { a.SetTestingUnavailable(true) }, "ExtensionTestingUnavailable"},
		{"a deactivated extension", func(a *API) error { _, err := a.Tests().CreateTestController("x", "x"); return err },
			func(a *API) { a.Deactivate() }, "ExtensionDeactivated"},
		{"a profile with no label", func(a *API) error {
			c, _ := a.Tests().CreateTestController("x", "x")
			_, err := c.CreateRunProfile("", extension.TestKindRun, noop, extension.TestRunProfileOptions{})
			return err
		}, nil, "ExtensionTestingInvalid"},
		{"a profile with no handler", func(a *API) error {
			c, _ := a.Tests().CreateTestController("x", "x")
			_, err := c.CreateRunProfile("p", extension.TestKindRun, nil, extension.TestRunProfileOptions{})
			return err
		}, nil, "ExtensionTestingInvalid"},
		{"a profile of an unknown kind", func(a *API) error {
			c, _ := a.Tests().CreateTestController("x", "x")
			_, err := c.CreateRunProfile("p", 7, noop, extension.TestRunProfileOptions{})
			return err
		}, nil, "ExtensionTestingInvalid"},
		{"a run with no request", func(a *API) error {
			c, _ := a.Tests().CreateTestController("x", "x")
			_, err := c.CreateTestRun(nil, "r", false)
			return err
		}, nil, "ExtensionTestingInvalid"},
		{"a run of a disposed controller", func(a *API) error {
			c, _ := a.Tests().CreateTestController("x", "x")
			c.Dispose()
			_, err := c.CreateTestRun(&extension.TestRunRequest{}, "r", false)
			return err
		}, nil, "ExtensionDeactivated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newDebugFake(t)
			if tt.prep != nil {
				tt.prep(a)
			}
			if got := routeOf(t, tt.do(a)); got != tt.tag {
				t.Errorf("tag = %s, want %s", got, tt.tag)
			}
		})
	}
}

func TestFakeTestControllerRecordsTheTreeAndTheRuns(t *testing.T) {
	a := newDebugFake(t)
	ctl, err := a.Tests().CreateTestController("fake", "Fake")
	if err != nil {
		t.Fatal(err)
	}
	fc := a.TestController("fake")
	if fc == nil || fc != ctl || len(a.TestControllers()) != 1 || fc.ID() != "fake" || fc.Label() != "Fake" {
		t.Fatal("the controller is not found")
	}
	pkg := ctl.CreateTestItem("pkg", "pkg", "file:///w/pkg")
	one := ctl.CreateTestItem("one", "one", "file:///w/pkg/one_test.go")
	if err := pkg.Children().Add(one); err != nil {
		t.Fatal(err)
	}
	if err := ctl.Items().Add(pkg); err != nil {
		t.Fatal(err)
	}
	one.Update(func(d *extension.TestItemData) { d.Label = "One" })
	if ch := fc.Changes(); len(ch) != 3 || ch[0].Kind != extension.TestItemAdded || ch[1].Path[1] != "one" || ch[2].Kind != extension.TestItemChanged || ch[2].Data.Label != "One" {
		t.Errorf("changes %+v", ch)
	}

	var handled *extension.TestRunRequest
	prof, err := ctl.CreateRunProfile("Run", extension.TestKindRun, func(_ context.Context, req *extension.TestRunRequest) error {
		handled = req
		run, err := ctl.CreateTestRun(req, "a run", true)
		if err != nil {
			return err
		}
		run.Enqueued(one)
		run.Started(one)
		run.Failed(one, []extension.TestMessage{{Message: "no"}}, time.Second)
		run.AppendOutput("out", nil, one)
		run.AddCoverage(extension.FileCoverage{URI: "file:///w/pkg/one.go"})
		run.End()
		run.Passed(one, 0) // after the end: nothing
		return nil
	}, extension.TestRunProfileOptions{IsDefault: true, Tag: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if !prof.IsDefault() || prof.Tag() != "t" || prof.Label() != "Run" || len(fc.Profiles()) != 1 {
		t.Error("the profile")
	}
	req, err := fc.Profiles()[0].Run(context.Background(), []*extension.TestItem{pkg}, nil, false)
	if err != nil || req != handled || handled.Profile != prof {
		t.Fatalf("Run: %v", err)
	}
	runs := fc.Runs()
	if len(runs) != 1 || runs[0].Name() != "a run" || !runs[0].Persist() || runs[0].Request() != req || !runs[0].Ended() {
		t.Fatalf("runs %+v", runs)
	}
	var kinds []TestRunEventKind
	for _, e := range runs[0].Events() {
		kinds = append(kinds, e.Kind)
	}
	want := []TestRunEventKind{RunEnqueued, RunStarted, RunFailed, RunOutput, RunCoverage, RunEnded}
	if len(kinds) != len(want) {
		t.Fatalf("events %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("events %v, want %v", kinds, want)
		}
	}
	if e := runs[0].Events()[2]; e.Item != one || e.Duration != time.Second || e.Messages[0].Message != "no" {
		t.Errorf("failed event %+v", e)
	}

	prof.SetIsDefault(false)
	prof.SetLabel("Run 2")
	if prof.IsDefault() || prof.Label() != "Run 2" {
		t.Error("SetIsDefault or SetLabel")
	}
	prof.Dispose()
	if len(fc.Profiles()) != 0 {
		t.Error("the disposed profile is listed")
	}
	ctl.InvalidateTestResults(one)
	if inv := fc.Invalidated(); len(inv) != 1 || inv[0][0] != one {
		t.Errorf("invalidated %+v", inv)
	}
}

func TestFakeTestHandlersAndTheirPanics(t *testing.T) {
	a := newDebugFake(t)
	ctl, _ := a.Tests().CreateTestController("fake", "Fake")
	fc := a.TestController("fake")
	item := ctl.CreateTestItem("x", "x", "file:///w/x_test.go")
	if err := ctl.Items().Add(item); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := fc.Resolve(ctx, nil); err != nil {
		t.Errorf("Resolve with no handler: %v", err)
	}
	if err := fc.Refresh(ctx); err != nil {
		t.Errorf("Refresh with no handler: %v", err)
	}
	var resolved *extension.TestItem
	ctl.SetResolveHandler(func(_ context.Context, it *extension.TestItem) error { resolved = it; return nil })
	ctl.SetRefreshHandler(func(context.Context) error { return plainError("refresh failed") })
	if err := fc.Resolve(ctx, item); err != nil || resolved != item {
		t.Errorf("Resolve %v", err)
	}
	if err := fc.Refresh(ctx); err == nil || err.Error() != "refresh failed" {
		t.Errorf("Refresh %v", err)
	}

	configured := false
	loads := 0
	prof, _ := ctl.CreateRunProfile("p", extension.TestKindCoverage, func(context.Context, *extension.TestRunRequest) error { return nil }, extension.TestRunProfileOptions{
		ConfigureHandler: func() { configured = true },
		LoadDetailedCoverage: func(context.Context, extension.FileCoverage) (extension.CoverageDetails, error) {
			loads++
			return extension.CoverageDetails{Declarations: []extension.DeclarationCoverage{{Name: "F"}}}, nil
		},
		ContinuousTrigger: func(it *extension.TestItem, uri string) bool { return uri == "file:///w/lib.go" },
	})
	fp := fc.Profiles()[0]
	if ok, err := fp.Configure(); !ok || err != nil || !configured {
		t.Errorf("Configure %v %v %v", ok, err, configured)
	}
	if d, err := fp.LoadCoverage(ctx, extension.FileCoverage{}); err != nil || d.Declarations[0].Name != "F" || loads != 1 {
		t.Errorf("LoadCoverage %+v %v", d, err)
	}
	if hit, err := fp.Triggers(item, "file:///w/lib.go"); !hit || err != nil {
		t.Errorf("Triggers %v %v", hit, err)
	}
	prof.Dispose()

	// a profile with no options answers as the model does by default
	_, _ = ctl.CreateRunProfile("plain", extension.TestKindRun, func(context.Context, *extension.TestRunRequest) error { return nil }, extension.TestRunProfileOptions{})
	pp := fc.Profiles()[0]
	if ok, _ := pp.Configure(); ok {
		t.Error("Configure with no handler reports ok")
	}
	if hit, _ := pp.Triggers(item, "file:///w/x_test.go"); !hit {
		t.Error("the default trigger is the item's own file")
	}
	if _, err := pp.LoadCoverage(ctx, extension.FileCoverage{}); routeOf(t, err) != "ExtensionTestingInvalid" {
		t.Errorf("LoadCoverage with no loader: %v", err)
	}
}

func TestDeactivateEndsTheControllersAndTheirRuns(t *testing.T) {
	a := newDebugFake(t)
	ctl, _ := a.Tests().CreateTestController("fake", "Fake")
	fc := a.TestController("fake")
	a.Deactivate()
	if !fc.Disposed() || len(a.TestControllers()) != 0 || a.TestController("fake") != nil {
		t.Error("the controller survived the deactivation")
	}
	ctl.Dispose() // twice: nothing happens
	_, err := ctl.CreateTestRun(&extension.TestRunRequest{}, "r", false)
	if routeOf(t, err) != "ExtensionDeactivated" {
		t.Errorf("err = %v", err)
	}
}

// A panic in a function of the extension takes the extension off, whichever function it is.
func TestAPanicInATestHandlerTakesTheExtensionOff(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		do   func(a *API, ctl extension.TestController) error
	}{
		{"resolve", func(a *API, ctl extension.TestController) error {
			ctl.SetResolveHandler(func(context.Context, *extension.TestItem) error { panic("boom") })
			return a.TestController("f").Resolve(ctx, nil)
		}},
		{"refresh", func(a *API, ctl extension.TestController) error {
			ctl.SetRefreshHandler(func(context.Context) error { panic("boom") })
			return a.TestController("f").Refresh(ctx)
		}},
		{"run", func(a *API, ctl extension.TestController) error {
			_, err := ctl.CreateRunProfile("p", extension.TestKindRun, func(context.Context, *extension.TestRunRequest) error { panic("boom") }, extension.TestRunProfileOptions{})
			if err != nil {
				return err
			}
			_, err = a.TestController("f").Profiles()[0].Run(ctx, nil, nil, false)
			return err
		}},
		{"configure", func(a *API, ctl extension.TestController) error {
			_, err := ctl.CreateRunProfile("p", extension.TestKindRun, func(context.Context, *extension.TestRunRequest) error { return nil }, extension.TestRunProfileOptions{ConfigureHandler: func() { panic("boom") }})
			if err != nil {
				return err
			}
			_, err = a.TestController("f").Profiles()[0].Configure()
			return err
		}},
		{"coverage loader", func(a *API, ctl extension.TestController) error {
			_, err := ctl.CreateRunProfile("p", extension.TestKindCoverage, func(context.Context, *extension.TestRunRequest) error { return nil }, extension.TestRunProfileOptions{
				LoadDetailedCoverage: func(context.Context, extension.FileCoverage) (extension.CoverageDetails, error) { panic("boom") },
			})
			if err != nil {
				return err
			}
			_, err = a.TestController("f").Profiles()[0].LoadCoverage(ctx, extension.FileCoverage{})
			return err
		}},
		{"continuous trigger", func(a *API, ctl extension.TestController) error {
			_, err := ctl.CreateRunProfile("p", extension.TestKindRun, func(context.Context, *extension.TestRunRequest) error { return nil }, extension.TestRunProfileOptions{
				ContinuousTrigger: func(*extension.TestItem, string) bool { panic("boom") },
			})
			if err != nil {
				return err
			}
			_, err = a.TestController("f").Profiles()[0].Triggers(ctl.CreateTestItem("i", "i", ""), "file:///x")
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newDebugFake(t)
			ctl, err := a.Tests().CreateTestController("f", "F")
			if err != nil {
				t.Fatal(err)
			}
			if got := routeOf(t, c.do(a, ctl)); got != "ExtensionTestingPanicked" {
				t.Errorf("tag = %s", got)
			}
			if !a.Failed() || len(a.TestControllers()) != 0 {
				t.Errorf("failed %v, controllers %d", a.Failed(), len(a.TestControllers()))
			}
		})
	}
}

// plainError is an error that is not an *errors.Error.
type plainError string

func (e plainError) Error() string { return string(e) }
