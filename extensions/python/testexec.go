package python

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Running the tests of the controller (SPEC §10.32 "The providers", Python; "As built (step 26.6)"): the request is turned into one run of
// pytest (with the plugin helpers/emetos_pytest.py) or of the unittest helper, in a process group of its own, whose events are read as
// they come into the TestRun; a run with coverage goes through coverage.py; Debug starts the Python debugger on the tests (step 25.8).

// handler is the handler of a run profile.
func (tc *testCtl) handler(kind extension.TestRunKind) extension.TestRunHandler {
	return func(ctx context.Context, req *extension.TestRunRequest) error {
		if kind == extension.TestKindDebug {
			tc.debugRequest(ctx, req)
			return nil
		}
		tc.runRequest(ctx, req, kind == extension.TestKindCoverage)
		return nil
	}
}

// hasExcludedBelow tells whether an item below the item is excluded.
func hasExcludedBelow(it *extension.TestItem, excluded map[*extension.TestItem]bool) bool {
	for _, c := range it.Children().All() {
		if excluded[c] || hasExcludedBelow(c, excluded) {
			return true
		}
	}
	return false
}

// plan turns a request into the items to run: Include (everything with none), without Exclude, where an item with an excluded item below
// it is replaced by its children, and where an item below another one that is run is left out. whole says that the request names nothing,
// so the run is the arguments of the settings as they are.
func (tc *testCtl) plan(req *extension.TestRunRequest, framework string) (items []*extension.TestItem, whole bool) {
	excluded := map[*extension.TestItem]bool{}
	for _, e := range req.Exclude {
		excluded[e] = true
	}
	roots := req.Include
	whole = len(roots) == 0 && len(excluded) == 0
	if len(roots) == 0 {
		roots = tc.ctl.Items().All()
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	var add func(it *extension.TestItem)
	add = func(it *extension.TestItem) {
		info, known := tc.info[it]
		if !known || excluded[it] {
			return
		}
		// unittest has no name for a folder: its files are run
		if (info.role == roleFolder && framework == frameworkUnittest) || hasExcludedBelow(it, excluded) {
			for _, k := range it.Children().All() {
				add(k)
			}
			return
		}
		items = append(items, it)
	}
	for _, r := range roots {
		add(r)
	}
	set := map[*extension.TestItem]bool{}
	for _, it := range items {
		set[it] = true
	}
	items = slices.DeleteFunc(items, func(it *extension.TestItem) bool {
		for a := it.Parent(); a != nil; a = a.Parent() {
			if set[a] {
				return true
			}
		}
		return false
	})
	return items, whole
}

// argsOf are the names of the items for the framework's command line.
func (tc *testCtl) argsOf(items []*extension.TestItem) []string {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if a := tc.info[it].arg; a != "" && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

// start runs the items with the profile of the kind for a command or a rerun: a new run stops the one the commands started before.
func (tc *testCtl) start(kind extension.TestRunKind, items []*extension.TestItem) {
	p := tc.p
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID()
	}
	tc.mu.Lock()
	tc.last = &lastTest{ids: ids, kind: kind}
	tc.mu.Unlock()
	tc.cancelRuns()
	ctx, cancel := context.WithCancel(p.ctx)
	tc.mu.Lock()
	tc.nrun++
	n := tc.nrun
	tc.runs[n] = cancel
	tc.mu.Unlock()
	req := &extension.TestRunRequest{Include: items, Profile: tc.profiles[kind]}
	p.closers.Add(1)
	go func() {
		defer p.closers.Done()
		defer func() {
			cancel()
			tc.mu.Lock()
			delete(tc.runs, n)
			tc.mu.Unlock()
		}()
		_ = tc.handler(kind)(ctx, req)
	}()
}

// cancelRuns stops the runs the commands started.
func (tc *testCtl) cancelRuns() {
	tc.mu.Lock()
	fns := make([]context.CancelFunc, 0, len(tc.runs))
	for _, c := range tc.runs {
		fns = append(fns, c)
	}
	tc.mu.Unlock()
	for _, c := range fns {
		c()
	}
}

// enqueue queues the tests of the item and returns them: its leaves.
func (tc *testCtl) enqueue(tr extension.TestRun, it *extension.TestItem) []*extension.TestItem {
	kids := it.Children().All()
	if len(kids) == 0 {
		tc.mu.Lock()
		role := tc.info[it].role
		tc.mu.Unlock()
		if role == roleFunc || role == roleParam {
			tr.Enqueued(it)
			return []*extension.TestItem{it}
		}
		return nil
	}
	var out []*extension.TestItem
	for _, k := range kids {
		out = append(out, tc.enqueue(tr, k)...)
	}
	return out
}

// hasCoveragePy asks the interpreter whether it can import coverage.
func (tc *testCtl) hasCoveragePy(ctx context.Context, python string, cfg testConfig) bool {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return tc.p.runner(ctx, cmdSpec{Program: python, Args: []string{"-c", "import coverage"}, Dir: cfg.cwd, Env: tc.env(cfg, "")}, nil) == nil
}

// hasCoverageConfig tells whether the folder has a configuration of coverage.py, which then decides what is measured.
func hasCoverageConfig(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".coveragerc")); err == nil {
		return true
	}
	for name, marker := range map[string]string{"pyproject.toml": "[tool.coverage", "setup.cfg": "[coverage:", "tox.ini": "[coverage:"} {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil && strings.Contains(string(data), marker) {
			return true
		}
	}
	return false
}

// runRequest runs the request and reports it. It returns when the run has ended; ctx stops it.
func (tc *testCtl) runRequest(ctx context.Context, req *extension.TestRunRequest, cover bool) {
	p := tc.p
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(p.ctx, cancel)()
	cfg := p.readTestConfig()
	python, err := p.testInterpreter(true)
	if err != nil {
		return
	}
	helpers, err := helperDir()
	if err != nil {
		p.api.Log().Warn("the helper scripts of the tests cannot be written", "err", err)
		p.api.Notifications().Error("The Python tests cannot be run: " + describe(err))
		return
	}
	fw := tc.frameworkFor(ctx, cfg, python)
	items, whole := tc.plan(req, fw)
	if len(items) == 0 {
		p.api.Notifications().Info("There are no Python tests to run.")
		return
	}
	if cover && !tc.hasCoveragePy(ctx, python, cfg) {
		p.api.Notifications().Warn("coverage.py is not installed in " + python + ", so the tests run without coverage. Install it with: " + shellJoin([]string{python, "-m", "pip", "install", "coverage"}))
		cover = false
	}
	tr, err := tc.ctl.CreateTestRun(req, fw, true)
	if err != nil {
		p.api.Log().Warn("the test run cannot be made", "err", err)
		return
	}
	defer tr.End()
	for _, it := range items {
		tc.enqueue(tr, it)
	}
	spec := cmdSpec{Program: python, Dir: cfg.cwd, Env: tc.env(cfg, helpers)}
	covDir := ""
	if cover {
		if covDir, err = os.MkdirTemp("", "emetos-pycover-"); err != nil {
			p.api.Log().Warn("the folder for the coverage data cannot be made", "err", err)
			p.api.Notifications().Warn("The coverage cannot be measured: " + describe(err))
			cover = false
		} else {
			defer func() {
				if err := os.RemoveAll(covDir); err != nil {
					p.api.Log().Warn("the coverage folder cannot be removed", "dir", covDir, "err", err)
				}
			}()
			spec.Env = append(spec.Env, "COVERAGE_FILE="+filepath.Join(covDir, ".coverage"))
		}
	}
	spec.Args = tc.commandArgs(fw, cfg, helpers, tc.argsOf(items), whole, cover)
	tr.AppendOutput("$ "+shellJoin(append([]string{filepath.Base(python)}, spec.Args...))+"\r\n", nil, nil)
	run := &pyRun{tc: tc, tr: tr, files: map[string][]string{}, workspace: p.api.Scope().Folder}
	err = p.runner(ctx, spec, run.line)
	run.end(ctx, err)
	if cover && ctx.Err() == nil {
		tc.reportCoverage(ctx, tr, python, cfg, spec.Env, filepath.Join(covDir, "coverage.json"))
	}
}

// commandArgs are the arguments of the interpreter for a run of the framework: the plugin or the helper, the settings' arguments and the
// names of the items (the arguments of the settings that name files or folders are left out of a run of chosen tests), behind coverage.py
// when asked for.
func (tc *testCtl) commandArgs(fw string, cfg testConfig, helpers string, names []string, whole, cover bool) []string {
	var args []string
	if cover {
		args = []string{"-m", "coverage", "run"}
		if !hasCoverageConfig(cfg.cwd) {
			args = append(args, "--source="+cfg.cwd, "--omit=*/site-packages/*,*/dist-packages/*")
		}
	}
	if fw == frameworkPytest {
		pargs := cfg.pytestArgs
		if !whole {
			pargs = withoutTestPaths(pargs, func(path string) bool {
				if !filepath.IsAbs(path) {
					path = filepath.Join(cfg.cwd, path)
				}
				_, err := os.Stat(path)
				return err == nil
			})
		}
		args = append(args, "-m", "pytest", "-p", pytestPlugin)
		args = append(args, pargs...)
		if whole {
			return args
		}
		return append(args, names...)
	}
	start, pattern, top := unittestDiscovery(cfg.unittestArgs)
	if cover {
		args = append(args, filepath.Join(helpers, unittestScript))
	} else {
		args = []string{filepath.Join(helpers, unittestScript)}
	}
	args = append(args, "run")
	args = append(args, unittestFlags(start, pattern, top)...)
	if !whole {
		args = append(args, names...)
	}
	return args
}

// ---- the events ----

// pyRun is the state of one run while the output of the program comes. Its methods run on the goroutine that reads the output.
type pyRun struct {
	tc        *testCtl
	tr        extension.TestRun
	workspace string
	// files caches the lines of the files that messages point into.
	files map[string][]string
	// events counts the events and stray holds the lines of output that were not events.
	events int
	stray  []string
}

// crlf makes the line breaks of the text those of a terminal, which is what the output of a run has.
func crlf(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
}

// line reads a line of the output of the program.
func (r *pyRun) line(l string) {
	text, ev, ok, err := parseEvent(l)
	if text != "" || !ok {
		r.tr.AppendOutput(crlf(text)+"\r\n", nil, nil)
		if !ok && err == nil && strings.TrimSpace(l) != "" {
			r.stray = append(r.stray, l)
		}
	}
	if err != nil {
		r.tc.p.api.Log().Debug("a line of the test run is not an event", "err", err)
		return
	}
	if !ok {
		return
	}
	r.events++
	switch ev.Type {
	case "start":
		if it := r.tc.itemForRun(ev.ID); it != nil {
			r.tr.Started(it)
		}
	case "result":
		r.result(ev)
	case "collect_error":
		it := r.tc.fileItemFor(ev.File)
		msgs := []extension.TestMessage{r.message(eventMessage{Message: ev.Message, Text: ev.Text, File: ev.File, Line: ev.Line})}
		r.tr.Errored(it, msgs, extension.TestNoDuration)
		r.tr.AppendOutput(crlf(ev.Text)+"\r\n", nil, it)
	}
}

// result reports the result of a test.
func (r *pyRun) result(ev testEvent) {
	var it *extension.TestItem
	if ev.Holder {
		it = r.tc.holderItem(ev.ID)
	} else {
		it = r.tc.itemForRun(ev.ID)
	}
	if it == nil {
		r.tc.p.api.Log().Debug("a result is for a test that is not in the tree", "id", ev.ID)
		return
	}
	d := extension.TestNoDuration
	if ev.Duration > 0 {
		d = time.Duration(ev.Duration * float64(time.Second))
	}
	msgs := make([]extension.TestMessage, 0, len(ev.Messages))
	for _, m := range ev.Messages {
		msgs = append(msgs, r.message(m))
	}
	if strings.TrimSpace(ev.Output) != "" {
		r.tr.AppendOutput(crlf(ev.Output)+"\r\n", nil, it)
	}
	switch ev.Outcome {
	case "passed":
		r.tr.Passed(it, d)
	case "skipped":
		r.tr.Skipped(it)
	case "failed":
		r.tr.Failed(it, msgs, d)
	default:
		r.tr.Errored(it, msgs, d)
	}
}

// message is the TestMessage of a failure: its text, the place it points at, the two sides of a comparison and the frames of the user's
// code.
func (r *pyRun) message(m eventMessage) extension.TestMessage {
	text := strings.TrimSpace(m.Message)
	if text == "" {
		text = strings.TrimSpace(clip(m.Text, 2000))
	}
	if text == "" {
		text = "The test failed."
	}
	msg := extension.TestMessage{Message: text, ExpectedOutput: m.Expected, ActualOutput: m.Actual}
	if m.File != "" && m.Line > 0 {
		msg.Location = &extension.Location{URI: fileURI(m.File), Range: r.lineRange(m.File, m.Line)}
	}
	for _, f := range m.Frames {
		if f.File == "" || f.Line < 1 {
			continue
		}
		msg.StackTrace = append(msg.StackTrace, extension.TestStackFrame{Label: f.Name, URI: fileURI(f.File), Position: &extension.Position{Line: f.Line - 1}})
	}
	return msg
}

// lineRange is the range of the code of a line (from the first character that is not blank to the end), 0-based; the line start when the
// file cannot be read.
func (r *pyRun) lineRange(path string, line int) extension.Range {
	lines, ok := r.files[path]
	if !ok {
		lines = readLines(path)
		r.files[path] = lines
	}
	rng := extension.Range{Start: extension.Position{Line: line - 1}, End: extension.Position{Line: line - 1}}
	if line-1 < len(lines) {
		l := lines[line-1]
		trimmed := strings.TrimLeft(l, " \t")
		rng.Start.Col = len(l) - len(trimmed)
		rng.End.Col = len(strings.TrimRight(l, " \t\r"))
	}
	return rng
}

// readLines reads the lines of a file of at most 4 MiB, none when it cannot be read.
func readLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() && len(out) < 200000 {
		out = append(out, sc.Text())
	}
	return out
}

// end is the end of the run of the program: a program that could not start, or that failed without telling anything (pytest is not
// installed, an unknown option), is told to the user with its last line.
func (r *pyRun) end(ctx context.Context, err error) {
	if err == nil || ctx.Err() != nil || r.events > 0 {
		return
	}
	what := describe(err)
	if len(r.stray) > 0 {
		what = r.stray[len(r.stray)-1]
	}
	r.tc.p.api.Log().Warn("the Python tests failed without results", "err", err)
	r.tc.p.api.Notifications().Error("The Python tests failed: " + what)
}

// ---- Debug ----

// debugRequest starts the Python debugger on the tests the request selects: pytest or unittest as the module, with the names of the items.
// It makes no result (the session is the debugger's).
func (tc *testCtl) debugRequest(ctx context.Context, req *extension.TestRunRequest) {
	p := tc.p
	cfg := p.readTestConfig()
	python, err := p.testInterpreter(true)
	if err != nil {
		return
	}
	fw := tc.frameworkFor(ctx, cfg, python)
	items, whole := tc.plan(req, fw)
	if len(items) == 0 {
		p.api.Notifications().Info("There are no Python tests to debug.")
		return
	}
	names := tc.argsOf(items)
	args := []any{}
	module := fw
	if fw == frameworkPytest {
		pargs := cfg.pytestArgs
		if !whole {
			pargs = withoutTestPaths(pargs, func(path string) bool {
				if !filepath.IsAbs(path) {
					path = filepath.Join(cfg.cwd, path)
				}
				_, err := os.Stat(path)
				return err == nil
			})
		}
		for _, a := range append(slices.Clone(pargs), wanted(names, whole)...) {
			args = append(args, a)
		}
	} else if whole {
		start, pattern, top := unittestDiscovery(cfg.unittestArgs)
		for _, a := range append([]string{"discover"}, unittestFlags(start, pattern, top)...) {
			args = append(args, a)
		}
	} else {
		for _, a := range names {
			args = append(args, a)
		}
	}
	cfgDebug := extension.DebugConfiguration{
		"name": "Debug Tests", "type": DebugType, "request": "launch", "module": module, "args": args, "cwd": cfg.cwd,
		"console": "internalConsole", "purpose": []any{"debug-test"},
	}
	p.api.Post(func() { p.startDebugConfig(cfgDebug, "the Python tests") })
}

// startDebugConfig starts the session of the configuration; what names it for the message of a failure.
func (p *python) startDebugConfig(cfg extension.DebugConfiguration, what string) {
	folder := p.api.Scope().Folder
	origin := p.api.Origins().Current()
	p.closers.Add(1)
	go func() {
		defer p.closers.Done()
		_, err := p.api.Debug().StartDebugging(p.ctx, folder, extension.DebugTarget{Config: cfg}, extension.DebugStartOptions{})
		if err == nil {
			return
		}
		p.api.Log().Warn("the tests cannot be debugged", "tests", what, "err", err)
		p.api.Post(func() {
			p.api.Origins().Run(origin, func() { p.api.Notifications().Error("Debug " + what + ": " + describe(err)) })
		})
	}()
}

// readFile reads a file; a file that cannot be read fails with PythonTestFileReadFailed.
func readFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.NewErr("PythonTestFileReadFailed", err).SetMetadata("path", path)
	}
	return data, nil
}

// wanted are the names to put on the command line: none for a run of everything.
func wanted(names []string, whole bool) []string {
	if whole {
		return nil
	}
	return names
}
