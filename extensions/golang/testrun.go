package golang

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Running the tests (SPEC §10.17 "As built (step 14.3)" and "(step 26.5)"): the commands and the settings of go test. The runs are those
// of the controller of testctl.go and testexec.go, in the Testing view.

// Ported from VS Code Go: extension/src/goTest.ts and extension/src/testUtils.ts (the arguments of go test: -timeout, the flags
// of go.testFlags, -run with ^(A|B)$, -bench with -run=^$ and -benchmem; the environment: go.testEnvFile, then go.testEnvVars).

// The ids of the commands.
const (
	// TestCursorCommand runs the test function at the cursor, BenchmarkCursorCommand the benchmark at the cursor, TestFileCommand the
	// tests of the file, TestPackageCommand the tests of the package, TestPreviousCommand the last run again and TestCancelCommand
	// stops the runs the commands started.
	TestCursorCommand      = ID + ".testCursor"
	BenchmarkCursorCommand = ID + ".benchmarkCursor"
	TestFileCommand        = ID + ".testFile"
	TestPackageCommand     = ID + ".testPackage"
	TestPreviousCommand    = ID + ".testPrevious"
	TestCancelCommand      = ID + ".testCancel"
	// TestsMarkerOwner is the owner of the markers of the failed tests; the name is also the source the Problems panel shows.
	TestsMarkerOwner = "go test"
)

// identRe is the name of a Go function.
var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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

// goTestArgs are the arguments of go test for the invocation: the JSON output, the timeout unless the flags have one (and a benchmark
// has none), the flags, the coverage profile when there is one, then -run or the benchmark, then the packages.
func (g *golang) goTestArgs(inv testInv, coverFile string) []string {
	flags := g.testFlags()
	args := []string{"test", "-json"}
	if inv.bench == "" && !hasFlag(flags, "timeout") {
		args = append(args, "-timeout", g.testTimeout())
	}
	args = append(args, flags...)
	if coverFile != "" {
		args = append(args, "-coverprofile="+coverFile)
		if !hasFlag(flags, "covermode") {
			// the race detector wants atomic counters
			mode := "count"
			if hasFlag(flags, "race") {
				mode = "atomic"
			}
			args = append(args, "-covermode="+mode)
		}
	}
	switch {
	case inv.bench != "":
		args = append(args, "-run=^$", "-bench", inv.bench, "-benchmem")
	case inv.run != "":
		args = append(args, "-run", inv.run)
	}
	return append(args, inv.pkgs...)
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

// testing is the controller, or nil with a notification to the user when the window has no Testing view.
func (g *golang) testing(what string) *testCtl {
	if g.tests == nil {
		g.api.Notifications().Info(what + " needs the Testing view, which this window does not have.")
	}
	return g.tests
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
	tc := g.testing(what)
	if tc == nil {
		return
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
	tc.loadText(file.path, file.text)
	if item := tc.itemFor(filepath.Dir(file.path), fn.name); item != nil {
		tc.start(extension.TestKindRun, []*extension.TestItem{item})
	}
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
	tc := g.testing("Run Test")
	if tc == nil {
		return nil
	}
	tc.loadFile(path)
	item := tc.itemFor(filepath.Dir(path), name)
	if item == nil {
		g.api.Notifications().Info(name + " was not found in " + filepath.Base(path) + ".")
		return nil
	}
	tc.start(extension.TestKindRun, []*extension.TestItem{item})
	return nil
}

// testFileCommand is the command "Go: Test File".
func (g *golang) testFileCommand(ctx context.Context, args any) error {
	const what = "Test File"
	tc := g.testing(what)
	if tc == nil {
		return nil
	}
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
	if len(scanTestFile(file.text).tests()) == 0 {
		g.api.Notifications().Info("No tests found in " + base + ".")
		return nil
	}
	tc.loadText(file.path, file.text)
	if item := tc.fileItemOf(file.path); item != nil {
		tc.start(extension.TestKindRun, []*extension.TestItem{item})
	}
	return nil
}

// testPackageCommand is the command "Go: Test Package": the package of the Go file in front, else the folder of the workspace.
func (g *golang) testPackageCommand(ctx context.Context, args any) error {
	tc := g.testing("Test Package")
	if tc == nil {
		return nil
	}
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
	tc.loadDir(dir)
	item := tc.packageItemOf(dir)
	if item == nil {
		where := g.runLabel(dir)
		if where == "." {
			where = "the workspace folder"
		}
		g.api.Notifications().Info("No tests found in " + where + ".")
		return nil
	}
	tc.start(extension.TestKindRun, []*extension.TestItem{item})
	return nil
}

// testPreviousCommand is the command "Go: Test Previous": the last run of a command again.
func (g *golang) testPreviousCommand(context.Context, any) error {
	if tc := g.testing("Test Previous"); tc != nil && !tc.startLast() {
		g.api.Notifications().Info("No tests have been run yet.")
	}
	return nil
}

// testCancelCommand is the command "Go: Cancel Running Tests".
func (g *golang) testCancelCommand(context.Context, any) error {
	if g.tests != nil {
		g.tests.cancelRuns()
	}
	return nil
}

// offsetLine is the line (from 0) of a byte offset of the text.
func offsetLine(text string, off int) int {
	off = min(max(off, 0), len(text))
	return strings.Count(text[:off], "\n")
}
