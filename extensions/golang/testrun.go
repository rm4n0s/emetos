package golang

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Running the tests (SPEC §10.17 "As built (step 14.3)"): the commands, the program `go test -json` in a process group of its own,
// the events handed to the UI thread as they come, the panel view "Go Tests" and the markers of the failures.

// Ported from VS Code Go: extension/src/goTest.ts and extension/src/testUtils.ts (the arguments of go test: -timeout, the flags
// of go.testFlags, -run with ^(A|B)$, -bench with -run=^$ and -benchmem; the environment: go.testEnvFile, then go.testEnvVars).

// The ids of the commands and of the panel view, and the owner of the markers of the failures.
const (
	// TestCursorCommand runs the test function at the cursor, BenchmarkCursorCommand the benchmark at the cursor, TestFileCommand the
	// tests of the file, TestPackageCommand the tests of the package, TestPreviousCommand the last run again and TestCancelCommand
	// stops the run that goes on.
	TestCursorCommand      = ID + ".testCursor"
	BenchmarkCursorCommand = ID + ".benchmarkCursor"
	TestFileCommand        = ID + ".testFile"
	TestPackageCommand     = ID + ".testPackage"
	TestPreviousCommand    = ID + ".testPrevious"
	TestCancelCommand      = ID + ".testCancel"
	// TestsViewID is the panel view "Go Tests".
	TestsViewID = "emetos.golang.tests"
	// TestsMarkerOwner is the owner of the markers of the failed tests; the name is also the source the Problems panel shows.
	TestsMarkerOwner = "go test"
)

// testRequest is what a run is asked to run; Run Again and Test Previous run the same request again.
type testRequest struct {
	// dir is the folder go test runs in (the package) and label how the panel names it.
	dir, label string
	// what says what is run, for the panel: "TestAdd", "tests of add_test.go", "the package".
	what string
	// pattern is the value of -run (or of -bench for a benchmark); empty runs everything.
	pattern string
	bench   bool
}

// identRe is the name of a Go function.
var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// testJob is one run of go test. The fields before mu belong to the UI thread; the lines the program printed that have not been
// read yet are filled by the goroutine of the run and guarded by mu.
type testJob struct {
	req     testRequest
	command string
	state   int
	// status is the text of the end: "exit status 1", "stopped", why it did not start.
	status   string
	cancel   context.CancelFunc
	stopping bool
	results  *testResults
	// seen are the nodes of the tree that were shown once: a node is open the first time it is seen when a test below it failed.
	seen map[string]bool

	mu          sync.Mutex
	pending     []string
	flushPosted bool
}

// push hands a line the program printed to the UI thread (once for a burst of lines). It may be called from any goroutine.
func (g *golang) push(j *testJob, line string) {
	j.mu.Lock()
	j.pending = append(j.pending, line)
	post := !j.flushPosted
	j.flushPosted = true
	j.mu.Unlock()
	if post {
		g.api.Post(func() { g.flush(j) })
	}
}

// flush reads the lines that came into the results and shows them.
func (g *golang) flush(j *testJob) {
	j.mu.Lock()
	lines := j.pending
	j.pending, j.flushPosted = nil, false
	j.mu.Unlock()
	for _, l := range lines {
		j.results.addLine(l)
	}
	if len(lines) > 0 && g.tests.job == j {
		g.tests.refresh()
	}
}

// ---- the settings ----

// testFlags are the flags of go.testFlags.
func (g *golang) testFlags() []string {
	flags, err := extension.SettingValue[[]string](g.api.Settings(), TestFlagsSetting)
	if err != nil {
		g.api.Log().Warn("go.testFlags is not a list of strings", "err", err)
		return nil
	}
	return flags
}

// testTimeout is go.testTimeout; a value that is not a duration is replaced by the default.
func (g *golang) testTimeout() string {
	v, err := extension.SettingValue[string](g.api.Settings(), TestTimeoutSetting)
	v = strings.TrimSpace(v)
	if err != nil || v == "" {
		return defaultTestTimeout
	}
	if _, err := time.ParseDuration(v); err != nil {
		g.api.Log().Warn("go.testTimeout is not a duration; the default is used", "value", v, "default", defaultTestTimeout)
		return defaultTestTimeout
	}
	return v
}

// hasFlag tells whether the flags have the flag, as -timeout, -timeout=1s or --timeout.
func hasFlag(flags []string, name string) bool {
	for _, f := range flags {
		f = strings.TrimLeft(f, "-")
		if f == name || strings.HasPrefix(f, name+"=") {
			return true
		}
	}
	return false
}

// testArgs are the arguments of go test for the request: the JSON output, the timeout unless the flags have one (and a benchmark
// has none), the flags, then -run or the benchmark.
func (g *golang) testArgs(req testRequest) []string {
	flags := g.testFlags()
	args := []string{"test", "-json"}
	if !req.bench && !hasFlag(flags, "timeout") {
		args = append(args, "-timeout", g.testTimeout())
	}
	args = append(args, flags...)
	switch {
	case req.bench:
		args = append(args, "-run=^$", "-bench", req.pattern, "-benchmem")
	case req.pattern != "":
		args = append(args, "-run", req.pattern)
	}
	return args
}

// parseEnvFile reads the lines NAME=value of an environment file: blank lines and lines that start with # are skipped, `export `
// before a name is dropped, and a value in single or double quotes loses them (\n in double quotes is a line break).
func parseEnvFile(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		name, value, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" || strings.ContainsAny(name, " \t") {
			continue
		}
		value = strings.TrimSpace(value)
		if n := len(value); n >= 2 && value[0] == '"' && value[n-1] == '"' {
			value = strings.ReplaceAll(value[1:n-1], `\n`, "\n")
		} else if n >= 2 && value[0] == '\'' && value[n-1] == '\'' {
			value = value[1 : n-1]
		}
		out = append(out, name+"="+value)
	}
	return out
}

// maxEnvFileBytes is the largest environment file that is read.
const maxEnvFileBytes = 1 << 20

// readEnvFile reads go.testEnvFile: a path inside the folder of the workspace (a relative one is relative to the folder; a link
// that leaves the folder does not count). It fails with TestEnvFileOutsideWorkspace for a file outside the folder, or in a scope
// with none, and TestEnvFileReadFailed when the file cannot be read.
func readEnvFile(folder, path string) ([]string, error) {
	if folder == "" {
		return nil, errors.New("TestEnvFileOutsideWorkspace", "go.testEnvFile needs a workspace folder", "path", path)
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(folder, abs)
	}
	abs = filepath.Clean(abs)
	root, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return nil, errors.NewErr("TestEnvFileReadFailed", err).SetMetadata("path", path)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !within(folder, abs) {
			return nil, errors.New("TestEnvFileOutsideWorkspace", "go.testEnvFile must be inside the workspace", "path", path)
		}
		return nil, errors.NewErr("TestEnvFileReadFailed", err).SetMetadata("path", path)
	}
	if !within(root, real) {
		return nil, errors.New("TestEnvFileOutsideWorkspace", "go.testEnvFile must be inside the workspace", "path", path)
	}
	if st, err := os.Stat(real); err == nil && st.Size() > maxEnvFileBytes {
		return nil, errors.New("TestEnvFileReadFailed", "the environment file is larger than 1 MiB", "path", path)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return nil, errors.NewErr("TestEnvFileReadFailed", err).SetMetadata("path", path)
	}
	return parseEnvFile(string(data)), nil
}

// testEnv is the environment of go test: that of Emetos, go.toolsEnvVars, the file go.testEnvFile, then go.testEnvVars (a later
// one wins).
func (g *golang) testEnv() []string {
	var extra []string
	if file, _ := extension.SettingValue[string](g.api.Settings(), TestEnvFileSetting); strings.TrimSpace(file) != "" {
		vars, err := readEnvFile(g.api.Scope().Folder, strings.TrimSpace(file))
		if err != nil {
			g.api.Log().Warn("go.testEnvFile is ignored", "err", err)
			g.api.Notifications().Warn("go.testEnvFile is ignored: " + describe(err))
		}
		extra = append(extra, vars...)
	}
	if m, err := extension.SettingValue[map[string]any](g.api.Settings(), TestEnvVarsSetting); err == nil {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if s, ok := m[k].(string); ok && k != "" {
				extra = append(extra, k+"="+s)
			}
		}
	}
	return g.programEnv(extra...)
}

// ---- the commands ----

// testTarget is the Go file a test command acts on: the file of the argument {"uri": ...} (what the lenses pass), else the editor
// in front. The selection is the editor's, and only when the file is the one in front.
func (g *golang) testTarget(ctx context.Context, args any, what string) (goFile, bool) {
	if m, ok := args.(map[string]any); ok {
		if uri, _ := m["uri"].(string); uri != "" {
			path, isFile := filePath(uri)
			if !isFile || !strings.HasSuffix(path, ".go") {
				g.api.Notifications().Info(what + " needs a Go file.")
				return goFile{}, false
			}
			if g.editor.uri == uri {
				return g.activeGoFile(ctx, what)
			}
			text, err := g.api.Documents().Read(ctx, uri)
			if err != nil {
				g.api.Log().Warn("the Go file cannot be read", "uri", uri, "err", err)
				g.api.Notifications().Error(what + ": " + filepath.Base(path) + " cannot be read: " + err.Error())
				return goFile{}, false
			}
			return goFile{uri: uri, path: path, text: text}, true
		}
	}
	return g.activeGoFile(ctx, what)
}

// requestFor makes the request for a package folder.
func (g *golang) requestFor(dir, what, pattern string, bench bool) testRequest {
	return testRequest{dir: dir, label: g.runLabel(dir), what: what, pattern: pattern, bench: bench}
}

// testCursorCommand is the command "Go: Test Function At Cursor".
func (g *golang) testCursorCommand(ctx context.Context, args any) error {
	g.testAtCursor(ctx, args, false)
	return nil
}

// benchmarkCursorCommand is the command "Go: Benchmark Function At Cursor".
func (g *golang) benchmarkCursorCommand(ctx context.Context, args any) error {
	g.testAtCursor(ctx, args, true)
	return nil
}

func (g *golang) testAtCursor(ctx context.Context, args any, bench bool) {
	what, noun := "Test Function At Cursor", "test"
	if bench {
		what, noun = "Benchmark Function At Cursor", "benchmark"
	}
	file, ok := g.testTarget(ctx, args, what)
	if !ok {
		return
	}
	base := filepath.Base(file.path)
	if !strings.HasSuffix(file.path, "_test.go") {
		g.api.Notifications().Info(what + ": " + base + " is not a test file (its name does not end in _test.go).")
		return
	}
	file = g.refreshed(ctx, file)
	scan := scanTestFile(file.text)
	line := offsetLine(file.text, offsetOf(file.text, file.sel.Active))
	fn, found := scan.at(line)
	if !found || (fn.kind == kindBenchmark) != bench {
		g.api.Notifications().Info("No " + noun + " function found at the cursor in " + base + ".")
		return
	}
	g.startTests(g.requestFor(filepath.Dir(file.path), fn.name, runPattern([]string{fn.name}), fn.kind == kindBenchmark))
}

// testFunctionCommand is the command the lenses run: {"uri": ..., "name": "TestAdd"}. A name that starts with Benchmark is run as a
// benchmark.
func (g *golang) testFunctionCommand(_ context.Context, args any) error {
	m, _ := args.(map[string]any)
	uri, _ := m["uri"].(string)
	name, _ := m["name"].(string)
	path, isFile := filePath(uri)
	if !isFile || !strings.HasSuffix(path, "_test.go") || !identRe.MatchString(name) {
		return errors.New("TestFunctionInvalid", "the command needs the uri of a _test.go file and the name of a function", "uri", uri, "name", name)
	}
	g.startTests(g.requestFor(filepath.Dir(path), name, runPattern([]string{name}), strings.HasPrefix(name, "Benchmark")))
	return nil
}

// testFileCommand is the command "Go: Test File".
func (g *golang) testFileCommand(ctx context.Context, args any) error {
	const what = "Test File"
	file, ok := g.testTarget(ctx, args, what)
	if !ok {
		return nil
	}
	base := filepath.Base(file.path)
	if !strings.HasSuffix(file.path, "_test.go") {
		g.api.Notifications().Info(what + ": " + base + " is not a test file (its name does not end in _test.go).")
		return nil
	}
	file = g.refreshed(ctx, file)
	tests := scanTestFile(file.text).tests()
	if len(tests) == 0 {
		g.api.Notifications().Info("No tests found in " + base + ".")
		return nil
	}
	g.startTests(g.requestFor(filepath.Dir(file.path), "the tests of "+base, runPattern(tests), false))
	return nil
}

// testPackageCommand is the command "Go: Test Package": the package of the Go file in front, else the folder of the workspace.
func (g *golang) testPackageCommand(ctx context.Context, args any) error {
	dir := ""
	if m, ok := args.(map[string]any); ok {
		if uri, _ := m["uri"].(string); uri != "" {
			if path, isFile := filePath(uri); isFile {
				dir = filepath.Dir(path)
			}
		}
	}
	if dir == "" {
		var ok bool
		if dir, ok = g.runDir(); !ok {
			g.api.Notifications().Info("Test Package needs a Go file or a folder: open one first.")
			return nil
		}
	}
	g.startTests(g.requestFor(dir, "the package", "", false))
	return nil
}

// testPreviousCommand is the command "Go: Test Previous": the last request again.
func (g *golang) testPreviousCommand(context.Context, any) error {
	if g.lastTest == nil {
		g.api.Notifications().Info("No tests have been run yet.")
		return nil
	}
	g.startTests(*g.lastTest)
	return nil
}

// testCancelCommand is the command "Go: Cancel Running Tests".
func (g *golang) testCancelCommand(context.Context, any) error {
	g.stopTests()
	return nil
}

// stopTests stops the run that goes on.
func (g *golang) stopTests() {
	if j := g.tests.job; j != nil && j.state == runRunning {
		j.stopping = true
		j.cancel()
	}
}

// offsetLine is the line (from 0) of a byte offset of the text.
func offsetLine(text string, off int) int {
	off = min(max(off, 0), len(text))
	return strings.Count(text[:off], "\n")
}

// ---- the run ----

// startTests runs the request and shows the panel. A run that goes on is stopped first.
func (g *golang) startTests(req testRequest) {
	p := g.tests
	program, err := g.goProgram()
	if err != nil {
		g.api.Log().Warn("the go program was not found", "err", err)
		g.api.Notifications().Error("The go command was not found. Install Go, or set go.alternateTools in the settings.")
		return
	}
	g.stopTests()
	args := g.testArgs(req)
	j := &testJob{req: req, results: newTestResults(), seen: map[string]bool{}, command: "go " + strings.Join(args, " ")}
	ctx, cancel := context.WithCancel(g.ctx)
	j.cancel = cancel
	p.job, p.selected = j, ""
	g.lastTest = &req
	g.api.Problems().Clear(TestsMarkerOwner)
	spec := cmdSpec{Program: program, Args: args, Dir: req.dir, Env: g.testEnv()}
	g.closers.Add(1)
	go func() {
		defer g.closers.Done()
		defer cancel()
		err := runProgram(ctx, spec, func(l string) { g.push(j, l) })
		g.api.Post(func() { g.finishTests(j, err) })
	}()
	p.refresh()
	p.handle.Reveal(true)
}

// finishTests is the end of a run, on the UI thread: the lines that are left are read, the state is told, and the failures go
// into the Problems panel.
func (g *golang) finishTests(j *testJob, err error) {
	g.flush(j)
	j.results.finish()
	var exit *exec.ExitError
	switch {
	case err == nil:
		j.state, j.status = runExited, "exit status 0"
	case j.stopping || g.ctx.Err() != nil:
		j.state, j.status = runStopped, "stopped"
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		j.state, j.status = runFailed, "exit status "+strconv.Itoa(exit.ExitCode())
	case errors.As(err, &exit):
		j.state, j.status = runFailed, "killed ("+exit.String()+")"
	default:
		j.state, j.status = runNotStarted, describe(err)
	}
	p := g.tests
	if p.job != j {
		return
	}
	if !j.stopping {
		g.setTestMarkers(j)
	}
	p.selectFirstFailure()
	p.refresh()
}

// setTestMarkers puts the places named in the output of the failed tests into the Problems panel (the owner "go test"; the old
// markers went when the run started).
func (g *golang) setTestMarkers(j *testJob) {
	root := g.api.Scope().Folder
	if root == "" {
		root = j.req.dir
	}
	byURI := map[string][]extension.Marker{}
	seen := map[string]bool{}
	add := func(lines []string, code string) {
		for _, f := range failureFindings(lines, j.req.dir, root, code) {
			key := f.Path + ":" + strconv.Itoa(f.Line) + ":" + f.Message
			if seen[key] {
				continue
			}
			seen[key] = true
			uri := fileURI(f.Path)
			byURI[uri] = append(byURI[uri], markerOf(f, TestsMarkerOwner))
		}
	}
	for _, p := range j.results.packages {
		add(p.output, "")
		names := make([]string, 0, len(p.byName))
		for name, n := range p.byName {
			if n.status == statusFail {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		for _, name := range names {
			add(p.byName[name].output, name)
		}
	}
	for uri, markers := range byURI {
		if err := g.api.Problems().Set(TestsMarkerOwner, uri, markers); err != nil {
			g.api.Log().Warn("the markers of the failed tests cannot be set", "uri", uri, "err", err)
		}
	}
}
