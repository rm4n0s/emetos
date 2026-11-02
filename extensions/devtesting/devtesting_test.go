package devtesting

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

const script = `{
  "label": "Mine", "delayMs": 1,
  "tests": [{"id": "f", "label": "f.go", "file": "f.go", "children": [
    {"id": "A", "label": "A", "file": "f.go", "line": 3, "endLine": 5, "result": "passed", "ms": 2.5},
    {"id": "B", "label": "B", "result": "failed", "ms": 7, "message": "nope", "expected": "1", "actual": "2"},
    {"id": "C", "label": "C", "result": "skipped"},
    {"id": "D", "label": "D", "result": "errored", "ms": 1, "message": "crash"}
  ]}]
}`

func activate(t *testing.T, text string) (*fakeapi.API, error) {
	t.Helper()
	folder := t.TempDir()
	if text != "" {
		if err := os.WriteFile(filepath.Join(folder, ScriptFile), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ext := New()
	a := fakeapi.New(t, ext.Manifest(), extension.ScopeInfo{Folder: folder})
	return a, ext.Activate(context.Background(), a)
}

func TestTheManifestIsValid(t *testing.T) {
	if err := extension.CheckManifest(New().Manifest()); err != nil {
		t.Fatal(err)
	}
}

func TestTheScriptMakesTheControllerItsTreeAndItsProfiles(t *testing.T) {
	a, err := activate(t, script)
	if err != nil {
		t.Fatal(err)
	}
	ctl := a.TestController(ControllerID)
	if ctl == nil || ctl.Label() != "Mine" {
		t.Fatalf("controller %v", ctl)
	}
	top := ctl.Items().All()
	if len(top) != 1 || top[0].ID() != "f" || top[0].Data().Label != "f.go" || len(top[0].Children().All()) != 4 {
		t.Fatalf("the tree is wrong: %+v", top)
	}
	d := top[0].Children().Get("A").Data()
	if want := "file://" + filepath.ToSlash(filepath.Join(a.Scope().Folder, "f.go")); d.URI != want || d.Range == nil || d.Range.Start.Line != 2 || d.Range.End.Line != 4 {
		t.Errorf("A: uri %q range %+v", d.URI, d.Range)
	}
	if got := top[0].Children().Get("B").Data(); got.URI != "" || got.Range != nil {
		t.Errorf("B has no file but %+v", got)
	}
	var kinds []extension.TestRunKind
	for _, p := range ctl.Profiles() {
		kinds = append(kinds, p.Kind())
		if !p.IsDefault() {
			t.Errorf("%s is not a default", p.Label())
		}
	}
	if len(kinds) != 3 || kinds[0] != extension.TestKindRun || kinds[1] != extension.TestKindDebug || kinds[2] != extension.TestKindCoverage {
		t.Errorf("the profiles are %v", kinds)
	}
}

func TestARunReportsTheScriptsResultsInOrder(t *testing.T) {
	a, err := activate(t, script)
	if err != nil {
		t.Fatal(err)
	}
	ctl := a.TestController(ControllerID)
	if _, err := ctl.Profiles()[0].Run(context.Background(), nil, nil, false); err != nil {
		t.Fatal(err)
	}
	runs := ctl.Runs()
	if len(runs) != 1 {
		t.Fatalf("%d runs", len(runs))
	}
	type ev struct {
		kind fakeapi.TestRunEventKind
		id   string
		d    time.Duration
		msg  string
	}
	var got []ev
	for _, e := range runs[0].Events() {
		x := ev{kind: e.Kind, d: e.Duration}
		if e.Item != nil {
			x.id = e.Item.ID()
		}
		if len(e.Messages) > 0 {
			x.msg = e.Messages[0].Message
		}
		got = append(got, x)
	}
	us := func(f float64) time.Duration { return time.Duration(f * float64(time.Millisecond)) }
	no := extension.TestNoDuration
	want := []ev{
		{fakeapi.RunEnqueued, "A", no, ""}, {fakeapi.RunEnqueued, "B", no, ""}, {fakeapi.RunEnqueued, "C", no, ""}, {fakeapi.RunEnqueued, "D", no, ""},
		{fakeapi.RunStarted, "A", no, ""}, {fakeapi.RunPassed, "A", us(2.5), ""},
		{fakeapi.RunStarted, "B", no, ""}, {fakeapi.RunFailed, "B", us(7), "nope"},
		{fakeapi.RunStarted, "C", no, ""}, {fakeapi.RunSkipped, "C", no, ""},
		{fakeapi.RunStarted, "D", no, ""}, {fakeapi.RunErrored, "D", us(1), "crash"},
		{fakeapi.RunEnded, "", no, ""},
	}
	if len(got) != len(want) {
		t.Fatalf("events %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestARunOfOneTestRunsOnlyThatTestAndCancelEndsItEarly(t *testing.T) {
	a, err := activate(t, script)
	if err != nil {
		t.Fatal(err)
	}
	ctl := a.TestController(ControllerID)
	b := ctl.Items().All()[0].Children().Get("B")
	if _, err := ctl.Profiles()[0].Run(context.Background(), []*extension.TestItem{b}, nil, false); err != nil {
		t.Fatal(err)
	}
	var started int
	for _, e := range ctl.Runs()[0].Events() {
		if e.Kind == fakeapi.RunStarted {
			started++
		}
	}
	if started != 1 {
		t.Errorf("%d tests started, want 1", started)
	}
	// a run whose context is done reports nothing past the first start and still ends
	slow, err := activate(t, `{"delayMs": 600000, "tests": [{"id": "x", "label": "x", "result": "passed"}, {"id": "y", "label": "y", "result": "passed"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	c2 := slow.TestController(ControllerID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c2.Profiles()[0].Run(ctx, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	evs := c2.Runs()[0].Events()
	if last := evs[len(evs)-1]; last.Kind != fakeapi.RunEnded {
		t.Errorf("the run did not end: %+v", last)
	}
	for _, e := range evs {
		if e.Kind == fakeapi.RunPassed {
			t.Errorf("a test passed after the cancel")
		}
	}
}

func TestAFolderWithNoScriptGetsNoController(t *testing.T) {
	a, err := activate(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := a.TestControllers(); len(got) != 0 {
		t.Errorf("%d controllers with no script", len(got))
	}
}

func TestABrokenScriptFailsTheActivation(t *testing.T) {
	_, err := activate(t, `{"tests": [`)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("devTesting.Activate.DevTestingScriptInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
