package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/devtesting"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	testview "github.com/rm4n0s/emetos/internal/gui/views/testing"
)

// The Test Explorer in the window (step 26.2, SPEC §10.32): the container, its view and menus, the commands and keys, the badge, against the
// fake test controller of extensions/devtesting (the host and the model are the real ones).

// testingScript is the script of the fake controller most tests use: three files, with passed, failed and skipped tests.
const testingScript = `{
  "label": "Fake Tests (dev)",
  "delayMs": 1,
  "tests": [
    {"id": "calc_test.go", "label": "calc_test.go", "file": "calc_test.go", "children": [
      {"id": "TestAdd", "label": "TestAdd", "file": "calc_test.go", "line": 3, "endLine": 5, "result": "passed", "ms": 3.2},
      {"id": "TestSub", "label": "TestSub", "file": "calc_test.go", "line": 7, "endLine": 9, "result": "passed", "ms": 2.1},
      {"id": "TestDiv", "label": "TestDiv", "file": "calc_test.go", "line": 11, "endLine": 13, "result": "failed", "ms": 12, "message": "expected 2.5, got 2"},
      {"id": "TestRoot", "label": "TestRoot", "file": "calc_test.go", "line": 15, "endLine": 17, "result": "skipped"}
    ]},
    {"id": "parser_test.go", "label": "parser_test.go", "file": "parser_test.go", "children": [
      {"id": "TestNumber", "label": "TestNumber", "file": "parser_test.go", "line": 3, "endLine": 5, "result": "passed", "ms": 4},
      {"id": "TestNested", "label": "TestNested", "file": "parser_test.go", "line": 7, "endLine": 9, "result": "failed", "ms": 7.5, "message": "unexpected token"}
    ]}
  ]
}`

// testingSource is the text of the test files of the folder: the tests are at the lines the script says.
const testingSource = "package p\n\nfunc TestA(t *testing.T) {\n\t_ = 1\n}\n\nfunc TestB(t *testing.T) {\n\t_ = 2\n}\n\nfunc TestC(t *testing.T) {\n\t_ = 3\n}\n\nfunc TestD(t *testing.T) {\n\t_ = 4\n}\n"

// testingRig is a window over a folder with the script, and the Test Explorer's model.
type testingRig struct {
	t      *testing.T
	win    *Workspace
	ts     *gluttertest.Tester
	folder string
}

// testingOpts tune a testingRig.
type testingOpts struct {
	theme  string
	size   ui.Size
	script string
	files  map[string]string
	open   []string
	// now is the clock of the model (the goldens fix it, so that the duration in the summary does not change).
	now      func() time.Time
	settings map[string]any
}

// newTestingRig makes the window and waits until the controller has its tree.
func newTestingRig(t *testing.T, o testingOpts) *testingRig {
	t.Helper()
	folder := t.TempDir()
	script := o.script
	if script == "" {
		script = testingScript
	}
	files := map[string]string{devtesting.ScriptFile: script, "calc_test.go": testingSource, "parser_test.go": testingSource}
	for name, text := range o.files {
		files[name] = text
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paths := testutil.Isolate(t)
	values := map[string]any{"files.hotExit": "off", "editor.minimap.enabled": false, "breadcrumbs.enabled": false, "editor.fontSize": 14,
		"window.menuBarVisibility": "classic"}
	if o.theme != "" {
		values["workbench.colorTheme"] = o.theme
	}
	for k, v := range o.settings {
		values[k] = v
	}
	testutil.Settings(t, paths, values)
	p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{devtesting.New}, TestingNow: o.now})
	win, err := NewWorkspace(context.Background(), p, folder)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(win.release)
	var docs []*document.Document
	for _, name := range o.open {
		doc, err := win.Documents.OpenFile(context.Background(), filepath.Join(folder, name), document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	size := o.size
	if size.Width == 0 {
		size = testWindowSize
	}
	ts := testutil.New(t, size)
	ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	ts.PumpWidget(newAppDocs(context.Background(), win, docs, nil, ""))
	r := &testingRig{t: t, win: win, ts: ts, folder: folder}
	r.waitFor("the controller's tests", func() bool {
		svc := win.TestingService()
		return svc != nil && svc.ItemCount(devtesting.ControllerID) > 0
	})
	return r
}

// pump runs the window for a few frames.
func (r *testingRig) pump() {
	for range 4 {
		r.ts.Pump(20 * time.Millisecond)
	}
}

// waitFor pumps the window until the condition holds.
func (r *testingRig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
		r.ts.FlushPosted()
		r.ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	r.pump()
}

// run runs a command in the window.
func (r *testingRig) run(id string, args any) {
	r.t.Helper()
	if err := r.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: r.win.Keys, Source: command.SourceAPI}, id, args); err != nil {
		r.t.Fatalf("%s: %v", id, err)
	}
	r.pump()
}

// svc is the test service of the window.
func (r *testingRig) svc() *coretesting.Service { return r.win.TestingService() }

// explorer is the model of the Test Explorer of the window.
func (r *testingRig) explorer() *testview.Explorer { return r.win.shell.testing.explorer }

// finished waits until a run ended.
func (r *testingRig) finished() {
	r.t.Helper()
	r.waitFor("a run to end", func() bool {
		rs := r.svc().Results()
		return len(rs) > 0 && rs[0].IsComplete() && !r.svc().IsRunning()
	})
}

// id is the id of a test of the fake controller.
func id(path ...string) string { return coretesting.JoinID(devtesting.ControllerID, path...) }

// state is the state a test shows in the newest result.
func (r *testingRig) state(path ...string) coretesting.State {
	_, tr, ok := r.svc().StateOf(id(path...))
	if !ok {
		return coretesting.StateUnset
	}
	return tr.State
}

func TestTheContainerOfTestingIsInTheActivityBarAndShowsTheTests(t *testing.T) {
	r := newTestingRig(t, testingOpts{})
	var ids []string
	for _, c := range r.win.Views.Registry().Containers(command.ViewLocationSideBar) {
		ids = append(ids, c.ID)
	}
	if !contains(ids, command.ViewContainerTesting) {
		t.Fatalf("the activity bar has %v, want %s in it", ids, command.ViewContainerTesting)
	}
	r.run(command.ViewContainerTesting, nil)
	if got := r.win.Views.Active(); got != command.ViewContainerTesting {
		t.Errorf("the active container is %q", got)
	}
	if n := r.explorer().Model().Len(); n != 8 {
		t.Errorf("the explorer has %d rows, want 8 (two files expanded)", n)
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}
