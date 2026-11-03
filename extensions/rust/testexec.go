package rust

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

// Running the tests of the controller (SPEC §10.32 "The providers", Rust; "As built (step 26.7)"): a request becomes one `cargo test` for
// each target it reaches (`-p <package> --lib|--bin|--test|--doc`), with `--exact` and the names of the tests when it does not take the
// whole target. The output of libtest is read as it comes (testlibtest.go) into the TestRun; Debug starts gdb or lldb-dap on the test
// binary that `cargo test --no-run --message-format=json` names (step 25.9's way of building a program before it is debugged).

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

// testGroup are the tests of a request that are in one target.
type testGroup struct {
	target testTarget
	// tid is the id of the item of the target.
	tid string
	// leaves are the tests, names their names in libtest's words, and whole says that the request takes every test of the target.
	leaves []*extension.TestItem
	names  []string
	whole  bool
}

// plan turns a request into the tests to run, grouped by target in the order they come: Include (everything with none) without Exclude.
func (tc *testCtl) plan(req *extension.TestRunRequest) []*testGroup {
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
	var groups []*testGroup
	byTarget := map[string]*testGroup{}
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
		if seen[it] {
			return
		}
		seen[it] = true
		tid := "cargo:" + info.target.pkg + ":" + info.target.key()
		g := byTarget[tid]
		if g == nil {
			g = &testGroup{target: info.target, tid: tid}
			byTarget[tid] = g
			groups = append(groups, g)
		}
		g.leaves = append(g.leaves, it)
		g.names = append(g.names, info.name)
	}
	for _, r := range roots {
		add(r)
	}
	for _, g := range groups {
		g.whole = len(g.leaves) == tc.countLeaves(tc.byID[g.tid])
	}
	return groups
}

// countLeaves counts the tests below an item. tc.mu is held.
func (tc *testCtl) countLeaves(it *extension.TestItem) int {
	if it == nil {
		return 0
	}
	n := 0
	for _, k := range it.Children().All() {
		if tc.info[k].role == roleTest {
			n++
		} else {
			n += tc.countLeaves(k)
		}
	}
	return n
}

// docFilters are the filters that select documentation tests. rustdoc splits a filter at its spaces and has no --exact for names that
// have them, so a test is selected by the longest word of the path of its item ("add" in "src/lib.rs - add (line 5)"), which also
// selects the tests of other items whose names contain it; the results of those are not reported.
func docFilters(names []string) []string {
	var out []string
	for _, n := range names {
		subject := n
		if m := docNameRe.FindStringSubmatch(n); m != nil {
			subject = m[2]
		}
		best := ""
		for _, f := range strings.Fields(subject) {
			if f != "-" && len(f) > len(best) && !strings.HasPrefix(f, "(line") {
				best = f
			}
		}
		if best != "" && !slices.Contains(out, best) {
			out = append(out, best)
		}
	}
	return out
}

// runArgs are the arguments of cargo for the group.
func (tc *testCtl) runArgs(g *testGroup) []string {
	args := append([]string{"test", "-p", g.target.pkg}, g.target.flags()...)
	args = append(args, tc.extraArgs()...)
	args = append(args, "--no-fail-fast", "--", "--show-output")
	switch {
	case g.whole:
	case g.target.kind == kindDoc:
		args = append(args, docFilters(g.names)...)
	default:
		args = append(args, "--exact")
		args = append(args, g.names...)
	}
	return args
}

// runRequest runs the request and reports it. It returns when the run has ended; ctx stops it.
func (tc *testCtl) runRequest(ctx context.Context, req *extension.TestRunRequest) {
	r := tc.r
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(r.ctx, cancel)()
	groups := tc.plan(req)
	if len(groups) == 0 {
		r.api.Notifications().Info("There are no Rust tests to run.")
		return
	}
	cargo, err := findCargo()
	if err != nil {
		r.api.Notifications().Error("The Rust tests cannot be run: " + describe(err))
		return
	}
	tr, err := tc.ctl.CreateTestRun(req, "cargo test", true)
	if err != nil {
		r.api.Log().Warn("the test run cannot be made", "err", err)
		return
	}
	defer tr.End()
	for _, g := range groups {
		for _, it := range g.leaves {
			tr.Enqueued(it)
		}
	}
	folder := r.api.Scope().Folder
	for _, g := range groups {
		if ctx.Err() != nil {
			return
		}
		args := tc.runArgs(g)
		tr.AppendOutput("$ cargo "+shellJoin(args)+"\r\n", nil, nil)
		run := &groupRun{tc: tc, tr: tr, g: g, pending: map[string]bool{}, reported: map[*extension.TestItem]bool{}, files: map[string][]string{}}
		err := r.runner(ctx, cmdSpec{Program: cargo, Args: args, Dir: folder, Env: cargoEnv()}, run.line)
		run.end(ctx, err)
	}
}

// groupRun is the state of the run of one target while its output comes. Its methods run on the goroutine that reads the output.
type groupRun struct {
	tc *testCtl
	tr extension.TestRun
	g  *testGroup
	// parser reads the output; sawRunning is true once libtest said "running N tests".
	parser     libtestParser
	sawRunning bool
	// pending are the tests that failed and wait for their block of output, reported are the items that have a result.
	pending  map[string]bool
	reported map[*extension.TestItem]bool
	// lines are the first lines of the output, kept for the message of a target that did not build; files caches the lines of files that
	// messages point into.
	lines []string
	files map[string][]string
}

// maxKeptLines bounds the lines kept for the message of a build that failed.
const maxKeptLines = 400

// crlf makes the line breaks of the text those of a terminal, which is what the output of a run has.
func crlf(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
}

// line reads a line of the output of cargo.
func (r *groupRun) line(l string) {
	if len(r.lines) < maxKeptLines {
		r.lines = append(r.lines, l)
	}
	r.events(r.parser.Feed(l))
}

// events reports what the parser found.
func (r *groupRun) events(evs []libtestEvent) {
	for _, ev := range evs {
		switch ev.Kind {
		case evLine:
			r.tr.AppendOutput(crlf(ev.Text)+"\r\n", nil, nil)
		case evRunning:
			r.sawRunning = true
		case evResult:
			r.result(ev)
		case evBlock:
			r.block(ev)
		}
	}
}

// item is the item of the test of this target with the name; nil with a note in the log when the tree has none (a documentation test
// that the filter of a run selected along with the one asked for, a test the discovery did not see).
func (r *groupRun) item(name string) *extension.TestItem {
	it := r.tc.itemForRun(r.g.tid, name)
	if it == nil || !slices.Contains(r.g.leaves, it) {
		r.tc.r.api.Log().Debug("a result is for a test that was not asked for", "target", r.g.tid, "name", name)
		return nil
	}
	return it
}

// result reports the line of a test. A failure waits for its block, which has the message.
func (r *groupRun) result(ev libtestEvent) {
	switch ev.Outcome {
	case outcomeFailed:
		r.pending[ev.Name] = true
		return
	}
	it := r.item(ev.Name)
	if it == nil || r.reported[it] {
		return
	}
	r.reported[it] = true
	if ev.Outcome == outcomeIgnored {
		r.tr.Skipped(it)
		return
	}
	r.tr.Passed(it, extension.TestNoDuration)
}

// block reports the output of a test: the message of a failure, or the output of a test that passed.
func (r *groupRun) block(ev libtestEvent) {
	it := r.item(ev.Name)
	if ev.Success {
		if it != nil && strings.TrimSpace(ev.Text) != "" {
			r.tr.AppendOutput(crlf(ev.Text)+"\r\n", nil, it)
		}
		return
	}
	delete(r.pending, ev.Name)
	if it == nil || r.reported[it] {
		return
	}
	r.reported[it] = true
	r.tr.Failed(it, r.messages(ev.Name, ev.Text), extension.TestNoDuration)
	if strings.TrimSpace(ev.Text) != "" {
		r.tr.AppendOutput(crlf(ev.Text)+"\r\n", nil, it)
	}
}

// messages are the TestMessages of the output of the failed test with the name: one for each panic, at the line it names (for a
// documentation test, at the test), with the two sides of a failed
// assert_eq! and the frames of the user's code; else the text of the output.
func (r *groupRun) messages(name, text string) []extension.TestMessage {
	panics := parsePanics(text)
	if len(panics) == 0 {
		msg := extension.TestMessage{Message: failureText(text)}
		if file, line, _, ok := compilerPlace(text); ok {
			msg.Location = r.location(file, line, 1)
		}
		return []extension.TestMessage{msg}
	}
	var out []extension.TestMessage
	for _, p := range panics {
		body := p.Text
		if body == "" {
			body = "The test panicked."
		}
		if p.Thread != "main" && !r.isTestThread(p.Thread) {
			body = "thread '" + p.Thread + "' panicked: " + body
		}
		msg := extension.TestMessage{Message: body}
		if p.Cmp {
			msg.ActualOutput, msg.ExpectedOutput = p.Left, p.Right
		}
		msg.Location = r.location(p.File, p.Line, p.Col)
		if r.g.target.kind == kindDoc {
			msg.Location = r.docLocation(name)
		}
		for _, f := range p.Frames {
			if fr, ok := r.frame(f); ok {
				msg.StackTrace = append(msg.StackTrace, fr)
			}
		}
		out = append(out, msg)
	}
	return out
}

// isTestThread tells whether the thread is the one of a test: libtest names it after the test.
func (r *groupRun) isTestThread(name string) bool {
	return slices.Contains(r.g.names, name)
}

// failureText is the text of a failure that is not a panic: the output of the test, shortened.
func failureText(text string) string {
	t := strings.TrimSpace(text)
	if t == "" {
		return "The test failed."
	}
	return clip(t, 2000)
}

// location is the place of a message: from the column of the panic to the end of its line.
func (r *groupRun) location(file string, line, col int) *extension.Location {
	if file == "" || line < 1 {
		return nil
	}
	path := r.tc.resolvePath(file, r.g.target.dir)
	if !fileExists(path) {
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
		rng.End.Col = len(strings.TrimRight(l, " \t\r"))
		if rng.End.Col < rng.Start.Col {
			rng.End.Col = rng.Start.Col
		}
	}
	return &extension.Location{URI: fileURI(path), Range: rng}
}

// frame is a frame of a backtrace as a TestStackFrame, when it is in the user's workspace.
func (r *groupRun) frame(f frame) (extension.TestStackFrame, bool) {
	if f.File == "" || f.Line < 1 {
		return extension.TestStackFrame{}, false
	}
	path := r.tc.resolvePath(strings.TrimPrefix(f.File, "./"), r.g.target.dir)
	if !fileExists(path) || !within(path, r.tc.r.api.Scope().Folder) {
		return extension.TestStackFrame{}, false
	}
	pos := extension.Position{Line: f.Line - 1, Col: max(f.Col-1, 0)}
	return extension.TestStackFrame{Label: f.Name, URI: fileURI(path), Position: &pos}, true
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

// end ends the run of the target: a failure whose block never came is reported with what it has, and a target that cargo could not
// build (no "running N tests") makes its tests errored with the compiler's output.
func (r *groupRun) end(ctx context.Context, err error) {
	r.events(r.parser.Flush())
	for name := range r.pending {
		if it := r.item(name); it != nil && !r.reported[it] {
			r.reported[it] = true
			r.tr.Failed(it, []extension.TestMessage{{Message: "The test failed."}}, extension.TestNoDuration)
		}
	}
	if err == nil || ctx.Err() != nil || r.sawRunning {
		return
	}
	text := buildErrorText(r.lines)
	r.tc.r.api.Log().Warn("cargo test ended without running the tests", "target", r.g.tid, "err", err)
	msg := extension.TestMessage{Message: failureText(text)}
	if file, line, col, ok := compilerPlace(text); ok {
		msg.Location = r.location(file, line, col)
	}
	for _, it := range r.g.leaves {
		if !r.reported[it] {
			r.reported[it] = true
			r.tr.Errored(it, []extension.TestMessage{msg}, extension.TestNoDuration)
		}
	}
}

// buildErrorText is the output of a cargo that did not build, without the lines that say what it was doing.
func buildErrorText(lines []string) string {
	i := 0
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "Compiling ") || strings.HasPrefix(t, "Blocking ") || strings.HasPrefix(t, "Updating ") ||
			strings.HasPrefix(t, "Downloading ") || strings.HasPrefix(t, "Locking ") {
			i++
			continue
		}
		break
	}
	return strings.Join(lines[i:], "\n")
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

// debugRequest starts the debugger on the tests the request selects. A session debugs one test binary, so with tests in several targets
// the first target is debugged; documentation tests are built and run by rustdoc and cannot be. It makes no result (the session is the
// debugger's).
func (tc *testCtl) debugRequest(_ context.Context, req *extension.TestRunRequest) {
	r := tc.r
	groups := tc.plan(req)
	if len(groups) == 0 {
		r.api.Notifications().Info("There are no Rust tests to debug.")
		return
	}
	g := groups[0]
	if g.target.kind == kindDoc {
		r.api.Notifications().Info("The documentation tests are built by rustdoc and cannot be debugged.")
		return
	}
	if len(groups) > 1 {
		r.api.Notifications().Info("A debug session runs one test binary; debugging the tests of " + g.target.label() + " of " + g.target.pkg + ".")
	}
	cfg := debugConfiguration(g, tc.extraArgs())
	r.api.Post(func() { r.startDebugConfig(cfg, "the Rust tests of "+g.target.label()) })
}

// debugConfiguration is the configuration that builds the test binary of the group's target and debugs the tests in it: the attribute
// `cargo` makes step 25.9's provider run `cargo test --no-run --message-format=json` and take the executable it names.
func debugConfiguration(g *testGroup, extra []string) extension.DebugConfiguration {
	flags := map[targetKind]string{kindLib: "--lib", kindBin: "--bin=" + g.target.name, kindTest: "--test=" + g.target.name, kindExample: "--example=" + g.target.name}
	cargoArgs := append([]string{"test", "--no-run", "--package=" + g.target.pkg, flags[g.target.kind]}, extra...)
	progArgs := []string{"--nocapture"}
	if !g.whole {
		progArgs = append([]string{"--exact"}, g.names...)
		progArgs = append(progArgs, "--nocapture")
	}
	return extension.DebugConfiguration{
		"name": "Debug tests", "type": debuggerType(), "request": "launch", "args": stringsToAny(progArgs), "cwd": g.target.dir,
		nativedebug.CargoAttribute: map[string]any{
			"args":   stringsToAny(cargoArgs),
			"filter": map[string]any{"name": g.target.name, "kind": g.target.artifactKind},
		},
	}
}

// startDebugConfig starts the session of the configuration; what names it for the message of a failure.
func (r *rust) startDebugConfig(cfg extension.DebugConfiguration, what string) {
	folder := r.api.Scope().Folder
	origin := r.api.Origins().Current()
	r.work.Add(1)
	go func() {
		defer r.work.Done()
		_, err := r.api.Debug().StartDebugging(r.ctx, folder, extension.DebugTarget{Config: cfg}, extension.DebugStartOptions{})
		if err == nil {
			return
		}
		r.api.Log().Warn("the tests cannot be debugged", "tests", what, "err", err)
		r.api.Post(func() {
			r.api.Origins().Run(origin, func() { r.api.Notifications().Error("Debug " + what + ": " + describe(err)) })
		})
	}()
}

// docLocation is the place of a documentation test, which its name gives ("src/lib.rs - add (line 5)"): the line a panic names is a
// line of the program rustdoc generates, not of the file.
func (r *groupRun) docLocation(name string) *extension.Location {
	m := docNameRe.FindStringSubmatch(name)
	if m == nil {
		return nil
	}
	return r.location(m[1], atoi(m[3]), 1)
}
