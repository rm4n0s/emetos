package cpp

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

// Running the tests of the controller (SPEC §10.32 "The providers", C and C++; "As built (step 26.9)"): a request becomes one
// `ctest --output-junit <file> [-R <the names>]` in the build folder. The progress lines of ctest start and pass the tests as they
// come; the JUnit file, read when ctest ends, has the rest: the failures with what the tests printed, the skipped and the tests that
// could not be run. Debug starts gdb or lldb-dap on the command of the test, as CTest would run it.

// handler is the handler of a run profile.
func (tc *testCtl) handler(kind extension.TestRunKind) extension.TestRunHandler {
	return func(ctx context.Context, req *extension.TestRunRequest) error {
		if kind == extension.TestKindDebug {
			tc.debugRequest(ctx, req)
			return nil
		}
		tc.runRequest(ctx, req)
		return nil
	}
}

// plan turns a request into the tests to run, in the order they come: Include (everything with none) without Exclude. whole says that
// the request takes every test.
func (tc *testCtl) plan(req *extension.TestRunRequest) (leaves []*extension.TestItem, whole bool) {
	excluded := map[*extension.TestItem]bool{}
	for _, e := range req.Exclude {
		excluded[e] = true
	}
	roots := req.Include
	if len(roots) == 0 {
		roots = tc.ctl.Items().All()
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	seen := map[*extension.TestItem]bool{}
	var add func(it *extension.TestItem)
	add = func(it *extension.TestItem) {
		info, known := tc.info[it]
		if !known || excluded[it] {
			return
		}
		if info.role != roleTest {
			for _, k := range it.Children().All() {
				add(k)
			}
			return
		}
		if !seen[it] {
			seen[it] = true
			leaves = append(leaves, it)
		}
	}
	for _, r := range roots {
		add(r)
	}
	return leaves, len(leaves) == len(tc.byName)
}

// regexMeta are the characters that mean something in CMake's regular expressions.
const regexMeta = `^$.[]*+?()|\`

// escapeRegex makes a test name a regular expression that matches that name only.
func escapeRegex(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if strings.ContainsRune(regexMeta, r) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// selectRegex is the regular expression of -R that selects the tests with the names.
func selectRegex(names []string) string {
	esc := make([]string, len(names))
	for i, n := range names {
		esc[i] = escapeRegex(n)
	}
	return "^(" + strings.Join(esc, "|") + ")$"
}

// runArgs are the arguments of ctest for the leaves; whole takes every test and needs no -R.
func (tc *testCtl) runArgs(junit string, leaves []*extension.TestItem, whole bool) []string {
	args := []string{"--output-junit", junit}
	if whole {
		return args
	}
	tc.mu.Lock()
	names := make([]string, len(leaves))
	for i, it := range leaves {
		names[i] = tc.info[it].test.Name
	}
	tc.mu.Unlock()
	return append(args, "-R", selectRegex(names))
}

var (
	// startRe is the line ctest prints when a test starts and doneRe the one it prints when it ends: "1/6 Test #1: adds ....   Passed    0.00 sec".
	startRe = regexp.MustCompile(`^\s*Start\s+\d+:\s+(.+?)\s*$`)
	doneRe  = regexp.MustCompile(`^\s*\d+/\d+ Test\s+#\d+: (.+?) \.+\s*(.*?)\s+([0-9.]+) sec\s*$`)
)

// runRequest runs the request and reports it. It returns when the run has ended; ctx stops it.
func (tc *testCtl) runRequest(ctx context.Context, req *extension.TestRunRequest) {
	c := tc.c
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(c.ctx, cancel)()
	leaves, whole := tc.plan(req)
	if len(leaves) == 0 {
		c.api.Notifications().Info("There are no CTest tests to run.")
		return
	}
	ctest, err := findCTest()
	if err != nil {
		c.api.Notifications().Error("The CTest tests cannot be run: " + describe(err))
		return
	}
	tmp, err := os.MkdirTemp("", "emetos-ctest-")
	if err != nil {
		c.api.Notifications().Error("The CTest tests cannot be run: " + err.Error())
		return
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	junit := filepath.Join(tmp, "junit.xml")
	tr, err := tc.ctl.CreateTestRun(req, "ctest", true)
	if err != nil {
		c.api.Log().Warn("the test run cannot be made", "err", err)
		return
	}
	defer tr.End()
	for _, it := range leaves {
		tr.Enqueued(it)
	}
	run := &ctestRun{tc: tc, tr: tr, leaves: map[*extension.TestItem]bool{}, reported: map[*extension.TestItem]bool{}, files: map[string][]string{}}
	for _, it := range leaves {
		run.leaves[it] = true
	}
	args := tc.runArgs(junit, leaves, whole)
	tr.AppendOutput("$ ctest "+shellJoin(args)+"\r\n", nil, nil)
	runErr := c.runner(ctx, cmdSpec{Program: ctest, Args: args, Dir: c.buildDirectory(), Env: os.Environ()}, run.line)
	if ctx.Err() != nil {
		return
	}
	run.end(junit, runErr)
}

// ctestRun is the state of one run while its output comes. Its methods run on the goroutine that reads the output.
type ctestRun struct {
	tc *testCtl
	tr extension.TestRun
	// leaves are the tests the run was asked for and reported those that have a result.
	leaves   map[*extension.TestItem]bool
	reported map[*extension.TestItem]bool
	// lines are the first lines of the output, kept for the message of a run that made no report; files caches the lines of files that
	// messages point into.
	lines []string
	files map[string][]string
}

// maxKeptLines bounds the lines kept for the message of a run that made no report.
const maxKeptLines = 400

// line reads a line of the output of ctest.
func (r *ctestRun) line(l string) {
	if len(r.lines) < maxKeptLines {
		r.lines = append(r.lines, l)
	}
	r.tr.AppendOutput(l+"\r\n", nil, nil)
	if m := startRe.FindStringSubmatch(l); m != nil {
		if it := r.item(m[1]); it != nil {
			r.tr.Started(it)
		}
		return
	}
	if m := doneRe.FindStringSubmatch(l); m != nil && m[2] == "Passed" {
		if it := r.item(m[1]); it != nil && !r.reported[it] {
			r.reported[it] = true
			secs, _ := strconv.ParseFloat(m[3], 64)
			r.tr.Passed(it, time.Duration(secs*float64(time.Second)))
		}
	}
}

// item is the item of the test of this run with the name; nil with a note in the log when it is not one the run was asked for.
func (r *ctestRun) item(name string) *extension.TestItem {
	it := r.tc.itemForName(name)
	if it == nil || !r.leaves[it] {
		r.tc.c.api.Log().Debug("a result is for a test that was not asked for", "name", name)
		return nil
	}
	return it
}

// end reads the JUnit file ctest wrote and reports the tests with it. Without the file (ctest did not get to run) the tests are errored
// with ctest's output.
func (r *ctestRun) end(junit string, runErr error) {
	data, err := os.ReadFile(junit)
	var results []junitResult
	if err == nil {
		results, err = readJUnit(data)
	}
	if err != nil {
		r.tc.c.api.Log().Warn("ctest wrote no report", "err", err, "ctest", runErr)
		msg := extension.TestMessage{Message: failureText(strings.Join(r.lines, "\n"))}
		for it := range r.leaves {
			if !r.reported[it] {
				r.reported[it] = true
				r.tr.Errored(it, []extension.TestMessage{msg}, extension.TestNoDuration)
			}
		}
		return
	}
	for _, res := range results {
		r.result(res)
	}
}

// result reports a test of the JUnit file.
func (r *ctestRun) result(res junitResult) {
	it := r.item(res.Name)
	if it == nil {
		return
	}
	if out := strings.TrimSpace(res.Output); out != "" {
		r.tr.AppendOutput(crlf(strings.TrimRight(res.Output, "\r\n"))+"\r\n", nil, it)
	}
	if r.reported[it] {
		return
	}
	r.reported[it] = true
	switch res.Outcome {
	case outcomePassed:
		r.tr.Passed(it, res.Duration)
	case outcomeSkipped:
		r.tr.Skipped(it)
	case outcomeFailed:
		r.tr.Failed(it, []extension.TestMessage{r.message(it, failureHeading(res.Message), res.Output)}, res.Duration)
	default:
		r.tr.Errored(it, []extension.TestMessage{r.message(it, notRunHeading(res.Message), res.Output)}, res.Duration)
	}
}

// failureHeading and notRunHeading are the first line of the message of a failure and of a test that could not be run.
func failureHeading(reason string) string {
	switch reason {
	case "", "Failed":
		return "The test failed."
	case "Timeout":
		return "The test timed out."
	}
	return "The test failed: " + reason + "."
}

func notRunHeading(reason string) string {
	if reason == "" {
		return "CTest did not run the test."
	}
	return "CTest did not run the test: " + reason + "."
}

// message is the TestMessage of a failure: the heading, then what the test printed; it is placed at the first source line of the C or
// C++ code that the output names.
func (r *ctestRun) message(it *extension.TestItem, heading, output string) extension.TestMessage {
	msg := extension.TestMessage{Message: heading}
	if out := strings.TrimSpace(output); out != "" {
		msg.Message += "\n\n" + failureText(out)
		msg.Location = r.placeIn(it, out)
	}
	return msg
}

// failureText is the output of a test, shortened.
func failureText(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return "ctest ended without running the tests."
	}
	return clip(t, 4000)
}

// crlf makes the line breaks of the text those of a terminal, which is what the output of a run has.
func crlf(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
}

// sourcePlaceRe finds "file.cpp:12" (also "file.cpp:12:5") in the output of a test: the form of assert(), GoogleTest, Catch2, doctest and
// compilers.
var sourcePlaceRe = regexp.MustCompile("([^\\s:'\"`()<>]+\\.(?:c|cc|cpp|cxx|c\\+\\+|cu|h|hh|hpp|hxx|inl)):(\\d+)(?::(\\d+))?")

// placeIn is the place of the first source file in the workspace that the output names; nil when it names none.
func (r *ctestRun) placeIn(it *extension.TestItem, output string) *extension.Location {
	info := r.tc.infoOf(it)
	folder := r.tc.c.api.Scope().Folder
	for _, m := range sourcePlaceRe.FindAllStringSubmatch(output, 20) {
		path := m[1]
		var candidates []string
		if filepath.IsAbs(path) {
			candidates = []string{path}
		} else {
			candidates = []string{filepath.Join(info.test.Dir, path), filepath.Join(folder, path), filepath.Join(r.tc.c.buildDirectory(), path)}
		}
		for _, p := range candidates {
			p = filepath.Clean(p)
			if !fileExists(p) || !within(p, folder) {
				continue
			}
			line, _ := strconv.Atoi(m[2])
			col, _ := strconv.Atoi(m[3])
			return r.location(p, line, col)
		}
	}
	return nil
}

// location is the place from the column (the start of the line without one) to the end of the line.
func (r *ctestRun) location(path string, line, col int) *extension.Location {
	if line < 1 {
		return nil
	}
	lines, ok := r.files[path]
	if !ok {
		lines = readLines(path)
		r.files[path] = lines
	}
	rng := extension.Range{Start: extension.Position{Line: line - 1}, End: extension.Position{Line: line - 1}}
	if line-1 < len(lines) {
		l := lines[line-1]
		rng.Start.Col = min(max(col-1, 0), len(l))
		rng.End.Col = max(len(strings.TrimRight(l, " \t\r")), rng.Start.Col)
	}
	return &extension.Location{URI: fileURI(path), Range: rng}
}

// infoOf is what the tree knows of the item.
func (tc *testCtl) infoOf(it *extension.TestItem) testItemInfo {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.info[it]
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

// shellJoin writes words as a shell command line, quoting what needs it.
func shellJoin(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		if w != "" && strings.Trim(w, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
			quoted[i] = w
		} else {
			quoted[i] = "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
		}
	}
	return strings.Join(quoted, " ")
}

// ---- Debug ----

// debuggerType is the debugger that debugs the tests: gdb when it is installed, else lldb-dap; gdb when neither is (its adapter tells
// the user how to get it).
func debuggerType() string {
	if _, err := exec.LookPath("gdb"); err == nil {
		return nativedebug.TypeGDB
	}
	if _, err := exec.LookPath("lldb-dap"); err == nil {
		return nativedebug.TypeLLDB
	}
	return nativedebug.TypeGDB
}

// debugRequest starts the debugger on the first test the request selects (a session debugs one program). It makes no result (the
// session is the debugger's).
func (tc *testCtl) debugRequest(_ context.Context, req *extension.TestRunRequest) {
	c := tc.c
	leaves, _ := tc.plan(req)
	if len(leaves) == 0 {
		c.api.Notifications().Info("There are no CTest tests to debug.")
		return
	}
	test := tc.infoOf(leaves[0]).test
	if len(test.Command) == 0 {
		c.api.Notifications().Info("The test " + test.Name + " has no command to debug.")
		return
	}
	if len(leaves) > 1 {
		c.api.Notifications().Info("A debug session runs one test; debugging " + test.Name + ".")
	}
	cfg := debugConfiguration(test, c.buildDirectory())
	c.api.Post(func() { c.startDebugConfig(cfg, "the CTest test "+test.Name) })
}

// debugConfiguration is the configuration that runs the command of the test under the debugger, in the folder and with the
// environment CTest gives it.
func debugConfiguration(t ctestTest, build string) extension.DebugConfiguration {
	program := t.Command[0]
	if !strings.Contains(program, "/") {
		if p, err := exec.LookPath(program); err == nil {
			program = p
		}
	}
	args := make([]any, 0, len(t.Command)-1)
	for _, a := range t.Command[1:] {
		args = append(args, a)
	}
	cwd := t.Dir
	if cwd == "" {
		cwd = build
	}
	cfg := extension.DebugConfiguration{"name": "Debug " + t.Name, "type": debuggerType(), "request": "launch", "program": program, "args": args, "cwd": cwd}
	if len(t.Env) > 0 {
		env := make([]any, len(t.Env))
		for i, e := range t.Env {
			env[i] = e
		}
		cfg["env"] = env
	}
	return cfg
}

// startDebugConfig starts the session of the configuration; what names it for the message of a failure.
func (c *cpp) startDebugConfig(cfg extension.DebugConfiguration, what string) {
	folder := c.api.Scope().Folder
	origin := c.api.Origins().Current()
	c.work.Add(1)
	go func() {
		defer c.work.Done()
		_, err := c.api.Debug().StartDebugging(c.ctx, folder, extension.DebugTarget{Config: cfg}, extension.DebugStartOptions{})
		if err == nil {
			return
		}
		c.api.Log().Warn("the tests cannot be debugged", "tests", what, "err", err)
		c.api.Post(func() {
			c.api.Origins().Run(origin, func() { c.api.Notifications().Error("Debug " + what + ": " + describe(err)) })
		})
	}()
}
