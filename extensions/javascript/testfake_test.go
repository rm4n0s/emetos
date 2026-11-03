package javascript

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The rig of the Testing provider's tests: the pack over a copy of a project of testdata/tests, with a fake runner that answers the programs
// the pack starts with the output recorded from the real runners (testdata/recorded; vitest's by hand) and keeps what it was asked.

// fakeRunner is the testRunner of the tests.
type fakeRunner struct {
	t *testing.T

	mu    sync.Mutex
	calls []string
	// fail makes the call whose command line contains the key end with an error; empty lines make it print nothing.
	fail map[string]bool
	// text replaces what the call whose command line contains the key prints and writes (a recorded file name) .
	reports map[string]string
	// block makes the calls that contain the key wait for their context to end.
	block string
	// started is closed when a blocked call starts.
	started chan struct{}
}

func newFakeRunner(t *testing.T) *fakeRunner {
	return &fakeRunner{t: t, fail: map[string]bool{}, reports: map[string]string{}, started: make(chan struct{}, 8)}
}

// argValue is the value of the first argument that starts with the prefix.
func argValue(args []string, prefix string) string {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return strings.TrimPrefix(a, prefix)
		}
	}
	return ""
}

func (f *fakeRunner) write(path, name, dir string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Error(err)
		return
	}
	if err := os.WriteFile(path, []byte(recorded(f.t, name, dir)), 0o644); err != nil {
		f.t.Error(err)
	}
}

func (f *fakeRunner) emit(name, dir string, line func(string)) {
	for _, l := range strings.Split(strings.TrimRight(recorded(f.t, name, dir), "\n"), "\n") {
		line(l)
	}
}

// run implements testRunner.
func (f *fakeRunner) run(ctx context.Context, spec cmdSpec, line func(string)) error {
	cmdline := filepath.Base(spec.Program) + " " + strings.Join(spec.Args, " ")
	f.mu.Lock()
	f.calls = append(f.calls, cmdline)
	block := f.block != "" && strings.Contains(cmdline, f.block)
	var fail bool
	for k := range f.fail {
		fail = fail || strings.Contains(cmdline, k)
	}
	override := ""
	for k, v := range f.reports {
		if strings.Contains(cmdline, k) {
			override = v
		}
	}
	f.mu.Unlock()
	dir := spec.Dir
	if block {
		f.started <- struct{}{}
		<-ctx.Done()
		return errors.New("FakeRunnerCancelled", "cancelled")
	}
	args := spec.Args
	pick := func(def string) string {
		if override != "" {
			return override
		}
		return def
	}
	switch {
	case fail:
		line("the fake program fails")
		return errors.New("FakeRunnerFailed", "the fake program failed")
	case strings.HasSuffix(cmdline, "--listTests"):
		f.emit("jest_list.txt", dir, line)
	case strings.Contains(cmdline, " list --filesOnly --json"):
		f.emit("vitest_list.json", dir, line)
	case argValue(args, "--outputFile=") != "": // jest
		f.write(argValue(args, "--outputFile="), pick("jest_report.json"), dir)
		if cov := argValue(args, "--coverageDirectory="); cov != "" {
			f.write(filepath.Join(cov, "coverage-final.json"), "jest_coverage.json", dir)
		}
		f.emit("jest_run.txt", dir, line)
	case argValue(args, "--outputFile.json=") != "": // vitest
		f.write(argValue(args, "--outputFile.json="), pick("vitest_report.json"), dir)
		if cov := argValue(args, "--coverage.reportsDirectory="); cov != "" {
			f.write(filepath.Join(cov, "coverage-final.json"), "vitest_coverage.json", dir)
		}
		line(" RUN  v5.0.5")
	case argValue(args, "--test-reporter=") != "": // node --test, one file
		file := args[len(args)-1]
		name := map[string]string{"test/math.test.js": "node_math.tap", "test/other.test.js": "node_other.tap", "test/broken.test.js": "node_broken.tap"}[file]
		if argValue(args, "--test-name-pattern=") != "" && name == "node_math.tap" {
			name = "node_filtered.tap"
		}
		if override != "" {
			name = override
		}
		for _, a := range args {
			if d, ok := strings.CutPrefix(a, "--test-reporter-destination="); ok && d != "stdout" {
				f.write(d, "node_coverage.lcov", dir)
			}
		}
		if name != "" {
			f.emit(name, dir, line)
		}
		if name == "node_broken.tap" {
			return errors.New("FakeRunnerFailed", "exit status 1")
		}
	default:
		f.t.Errorf("an unexpected program: %s", cmdline)
		return errors.New("FakeRunnerUnexpected", "unexpected program")
	}
	return nil
}

// called lists what the programs were called with.
func (f *fakeRunner) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// jsRig is the pack activated over a project with a fake runner.
type jsRig struct {
	*rig
	fr *fakeRunner
}

// newJSRig activates the pack over a copy of the project.
func newJSRig(t *testing.T, project string, o rigOptions) *jsRig {
	t.Helper()
	fr := newFakeRunner(t)
	o.project, o.runner = project, fr.run
	r := newRig(t, o)
	t.Cleanup(r.ext.wait)
	jr := &jsRig{rig: r, fr: fr}
	jr.settled()
	return jr
}

// settled waits until nothing is being found.
func (r *jsRig) settled() {
	r.t.Helper()
	r.api.WaitFor("the discovery of the tests", func() bool { return r.ext.testsIdle() })
}

// ctl is the controller of the runner the pack made.
func (r *jsRig) ctl(runner string) *fakeapi.FakeTestController {
	r.t.Helper()
	c := r.api.TestController(runner)
	if c == nil {
		r.t.Fatalf("the pack made no controller %s (it made %d)", runner, len(r.api.TestControllers()))
	}
	return c
}

// profile is the profile of the kind of the controller.
func (r *jsRig) profile(runner string, kind extension.TestRunKind) *fakeapi.FakeTestProfile {
	r.t.Helper()
	for _, p := range r.ctl(runner).Profiles() {
		if p.Kind() == kind {
			return p
		}
	}
	r.t.Fatalf("no profile of kind %d", kind)
	return nil
}

// runWith runs the include with the profile of the kind and returns the run the handler made.
func (r *jsRig) runWith(runner string, kind extension.TestRunKind, include, exclude []*extension.TestItem) *fakeapi.FakeTestRun {
	r.t.Helper()
	c := r.ctl(runner)
	before := len(c.Runs())
	if _, err := r.profile(runner, kind).Run(context.Background(), include, exclude, false); err != nil {
		r.t.Fatal(err)
	}
	runs := c.Runs()
	if len(runs) != before+1 {
		r.t.Fatalf("%d runs, want %d", len(runs), before+1)
	}
	return runs[before]
}

// run runs the include with the Run profile.
func (r *jsRig) run(runner string, include, exclude []*extension.TestItem) *fakeapi.FakeTestRun {
	return r.runWith(runner, extension.TestKindRun, include, exclude)
}

// item finds an item of the controller by its labels.
func (r *jsRig) item(runner, path string) *extension.TestItem {
	r.t.Helper()
	return treeFind(r.t, r.ctl(runner).Items(), path)
}

// items finds several.
func (r *jsRig) items(runner string, paths ...string) []*extension.TestItem {
	r.t.Helper()
	var out []*extension.TestItem
	for _, p := range paths {
		out = append(out, r.item(runner, p))
	}
	return out
}

// note is the text of the last notification, empty when there is none.
func (r *jsRig) note() string {
	n := r.api.Notes()
	if len(n) == 0 {
		return ""
	}
	return n[len(n)-1].Message
}

// debugOf runs the Debug profile on the items and returns the configurations the pack started.
func (r *jsRig) debugOf(runner string, include, exclude []*extension.TestItem) []extension.DebugConfiguration {
	r.t.Helper()
	n := len(r.api.DebugStarts())
	if _, err := r.profile(runner, extension.TestKindDebug).Run(context.Background(), include, exclude, false); err != nil {
		r.t.Fatal(err)
	}
	r.api.Pump()
	r.ext.wait()
	var out []extension.DebugConfiguration
	for _, s := range r.api.DebugStarts()[n:] {
		out = append(out, s.Target.Config)
	}
	return out
}
