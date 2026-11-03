package javascript

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// Running the tests of a controller (SPEC §10.32 "The providers", JavaScript and TypeScript; "As built (step 26.8)"): a request becomes
// one process for each project (Jest, Vitest: the files asked for, with a name filter for the files of which only some tests were asked
// for) or one for each file (Node's runner, whose TAP does not say which file a test is in). Jest and Vitest write their JSON report
// to a file that is read when the run ends; Node's TAP is read as it comes. Coverage is read from the files the runners write.

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

// fileSel is what a request asks of one test file.
type fileSel struct {
	project, rel, abs string
	// whole says that every test of the file is asked for. Otherwise picks are the suites and tests asked for (without those below
	// another pick) and leaves the tests of them.
	whole  bool
	picks  []*extension.TestItem
	leaves []*extension.TestItem
	// chosenFile says that the request reached the file itself, hasExcluded that something below it is excluded.
	chosenFile, hasExcluded bool
}

// covers tells whether the titles are a test the request asked for: the file as a whole, or below one of the picks.
func (s *fileSel) covers(tc *testCtl, path []string) bool {
	if s.whole {
		return true
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	for _, p := range s.picks {
		pp := tc.info[p].path
		if len(path) >= len(pp) && slices.Equal(path[:len(pp)], pp) {
			return true
		}
	}
	return false
}

// plan turns a request into the files to run, in the order of the tree: Include (everything with none) without Exclude.
func (tc *testCtl) plan(req *extension.TestRunRequest) []*fileSel {
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
	var order []*fileSel
	byFile := map[string]*fileSel{}
	get := func(info testItemInfo) *fileSel {
		key := fileKey(info.project, info.file)
		s := byFile[key]
		if s == nil {
			s = &fileSel{project: info.project, rel: info.file, abs: filepath.Join(info.project, filepath.FromSlash(info.file))}
			byFile[key] = s
			order = append(order, s)
		}
		return s
	}
	var collect func(it *extension.TestItem, s *fileSel)
	collect = func(it *extension.TestItem, s *fileSel) {
		for _, k := range it.Children().All() {
			if excluded[k] {
				s.hasExcluded = true
				continue
			}
			if tc.info[k].role == roleTest {
				s.leaves = append(s.leaves, k)
			}
			collect(k, s)
		}
	}
	var visit func(it *extension.TestItem)
	visit = func(it *extension.TestItem) {
		info, known := tc.info[it]
		if !known || excluded[it] {
			return
		}
		switch info.role {
		case roleProject:
			for _, k := range it.Children().All() {
				visit(k)
			}
		case roleFile:
			s := get(info)
			s.chosenFile = true
			collect(it, s)
		default:
			s := get(info)
			s.picks = append(s.picks, it)
			if info.role == roleTest {
				s.leaves = append(s.leaves, it)
			}
			collect(it, s)
		}
	}
	for _, r := range roots {
		visit(r)
	}
	for _, s := range order {
		s.leaves = dedupe(s.leaves)
		switch {
		case s.chosenFile && !s.hasExcluded:
			s.whole, s.picks = true, nil
		case s.chosenFile:
			// something is excluded: the tests that remain are asked for one by one
			s.picks = slices.Clone(s.leaves)
		default:
			s.picks = dedupe(slices.DeleteFunc(s.picks, func(p *extension.TestItem) bool {
				for a := p.Parent(); a != nil; a = a.Parent() {
					if slices.Contains(s.picks, a) {
						return true
					}
				}
				return false
			}))
		}
	}
	return order
}

func dedupe(items []*extension.TestItem) []*extension.TestItem {
	var out []*extension.TestItem
	for _, it := range items {
		if !slices.Contains(out, it) {
			out = append(out, it)
		}
	}
	return out
}

// namePatterns are the regular expressions that select the picks of the files by their full names (the titles joined with spaces, with " > " for Vitest): a test
// by its whole name, a suite by its name as the start of the names below it.
func (tc *testCtl) namePatterns(sels []*fileSel) []string {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	var out []string
	for _, s := range sels {
		if s.whole {
			continue
		}
		for _, p := range s.picks {
			info := tc.info[p]
			sep := " "
			if tc.runner == RunnerVitest {
				sep = " > " // Vitest matches the names joined with " > "
			}
			name := regexp.QuoteMeta(strings.Join(info.path, sep))
			if info.role == roleTest {
				out = append(out, "^"+name+"$")
			} else {
				out = append(out, "^"+name+"("+regexp.QuoteMeta(sep)+"|$)")
			}
		}
	}
	return slices.Compact(out)
}

// anyPartial tells whether some file is asked for only in part.
func anyPartial(sels []*fileSel) bool {
	return slices.ContainsFunc(sels, func(s *fileSel) bool { return !s.whole })
}

// ---- the run ----

// runState is the state of one run while the reports come.
type runState struct {
	tc       *testCtl
	tr       extension.TestRun
	sels     map[string]*fileSel
	leafSet  map[*extension.TestItem]bool
	reported map[*extension.TestItem]bool
	// files caches the lines of the files that messages point into.
	files map[string][]string
}

// runRequest runs the request and reports it. It returns when the run has ended; ctx stops it.
func (tc *testCtl) runRequest(ctx context.Context, req *extension.TestRunRequest, cover bool) {
	j := tc.j
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(j.ctx, cancel)()
	sels := tc.plan(req)
	if len(sels) == 0 {
		j.api.Notifications().Info("There are no " + runnerLabels[tc.runner] + " tests to run.")
		return
	}
	folder := j.api.Scope().Folder
	node, err := j.findNode(j.env(folder))
	if err != nil {
		return
	}
	tr, err := tc.ctl.CreateTestRun(req, runnerLabels[tc.runner], true)
	if err != nil {
		j.api.Log().Warn("the test run cannot be made", "err", err)
		return
	}
	defer tr.End()
	rs := &runState{tc: tc, tr: tr, sels: map[string]*fileSel{}, leafSet: map[*extension.TestItem]bool{}, reported: map[*extension.TestItem]bool{}, files: map[string][]string{}}
	for _, s := range sels {
		rs.sels[s.abs] = s
		for _, it := range s.leaves {
			rs.leafSet[it] = true
			tr.Enqueued(it)
		}
	}
	covDir, err := os.MkdirTemp("", "emetos-jstest-")
	if err != nil {
		j.api.Log().Warn("the folder for the reports cannot be made", "err", err)
		j.api.Notifications().Error("The " + runnerLabels[tc.runner] + " tests cannot be run: " + describe(err))
		return
	}
	defer func() {
		if err := os.RemoveAll(covDir); err != nil {
			j.api.Log().Warn("the folder of the reports cannot be removed", "dir", covDir, "err", err)
		}
	}()
	var projects []string
	byProject := map[string][]*fileSel{}
	for _, s := range sels {
		if _, ok := byProject[s.project]; !ok {
			projects = append(projects, s.project)
		}
		byProject[s.project] = append(byProject[s.project], s)
	}
	for i, dir := range projects {
		if ctx.Err() != nil {
			return
		}
		dirs := &runDirs{base: filepath.Join(covDir, strconv.Itoa(i)), cover: cover}
		switch tc.runner {
		case RunnerNode:
			tc.runNode(ctx, rs, node, dir, byProject[dir], dirs)
		default:
			tc.runJestLike(ctx, rs, node, dir, byProject[dir], dirs)
		}
	}
}

// runDirs are the places a run of a project writes its reports to.
type runDirs struct {
	base  string
	cover bool
	n     int
}

// next gives a fresh sub-folder for one process of the run.
func (d *runDirs) next() string {
	d.n++
	return filepath.Join(d.base, strconv.Itoa(d.n))
}

// toolSpec is the program and arguments that start the runner's own tool (jest, vitest) in the project: node on its script, or the
// program the link names.
func (tc *testCtl) toolSpec(node string, p string, tool string) (cmdSpec, string, error) {
	script, program, ok := findTool(p, tool, os.Getenv("PATH"))
	switch {
	case !ok:
		return cmdSpec{}, "", errors.New("JsTestToolNotFound", tool+" was not found in node_modules/.bin of the project or in PATH", "tool", tool, "project", p)
	case script != "":
		return cmdSpec{Program: node, Args: []string{script}, Dir: p, Env: testEnv()}, tool, nil
	}
	return cmdSpec{Program: program, Dir: p, Env: testEnv()}, tool, nil
}

// testEnv is the environment of a program of the provider: Emetos's own.
func testEnv() []string { return os.Environ() }

// with returns the spec with more arguments.
func (c cmdSpec) with(args ...string) cmdSpec {
	c.Args = append(slices.Clone(c.Args), args...)
	return c
}

// shown writes the command line the output of the run starts with: the tool's name and the arguments the user would recognize.
func shown(tool string, args []string) string { return "$ " + tool + " " + shellJoin(args) + "\r\n" }

// runJestLike runs Jest or Vitest for the files of a project: the files asked for as a whole in one process and the files of which
// only some tests are asked for in another, with the names as the filter.
func (tc *testCtl) runJestLike(ctx context.Context, rs *runState, node, dir string, sels []*fileSel, dirs *runDirs) {
	j := tc.j
	tool := tc.runner
	spec, _, err := tc.toolSpec(node, dir, tool)
	if err != nil {
		j.api.Log().Warn("the test tool cannot be found", "tool", tool, "err", err)
		j.api.Notifications().Error(runnerLabels[tc.runner] + " was not found in " + dir + ": install it in the project (npm install).")
		return
	}
	cover := dirs.cover
	if cover && tool == RunnerVitest && !hasVitestCoverage(dir) {
		j.api.Notifications().Warn("Vitest has no coverage package in this project, so the tests run without coverage. Install it with: npm install -D @vitest/coverage-v8")
		cover = false
	}
	var whole, part []*fileSel
	for _, s := range sels {
		if s.whole {
			whole = append(whole, s)
		} else {
			part = append(part, s)
		}
	}
	for _, group := range [][]*fileSel{whole, part} {
		if len(group) == 0 || ctx.Err() != nil {
			continue
		}
		out := dirs.next()
		if err := os.MkdirAll(out, 0o755); err != nil {
			j.api.Log().Warn("the folder for the reports cannot be made", "err", err)
			continue
		}
		report := filepath.Join(out, "report.json")
		covOut := filepath.Join(out, "coverage")
		var plumbing, user, lead, coverFlag []string // coverFlag is only for the line the output starts with
		if tool == RunnerJest {
			plumbing = []string{"--json", "--outputFile=" + report, "--testLocationInResults"}
			if cover {
				plumbing = append(plumbing, "--coverage", "--coverageReporters=json", "--coverageDirectory="+covOut)
				coverFlag = []string{"--coverage"}
			}
		} else {
			lead = []string{"run"}
			plumbing = []string{"--reporter=default", "--reporter=json", "--outputFile.json=" + report}
			if cover {
				plumbing = append(plumbing, "--coverage.enabled", "--coverage.reportOnFailure", "--coverage.reporter=json", "--coverage.reportsDirectory="+covOut)
				coverFlag = []string{"--coverage"}
			}
		}
		var files []string
		for _, s := range group {
			files = append(files, s.rel)
		}
		if !group[0].whole {
			pats := tc.namePatterns(group)
			if len(pats) > 0 {
				user = append(user, "-t", "("+strings.Join(pats, "|")+")")
			}
		}
		if tool == RunnerJest {
			user = append(user, "--runTestsByPath")
		}
		user = append(user, files...)
		args := slices.Concat(lead, plumbing, user)
		rs.tr.AppendOutput(shown(tool, slices.Concat(lead, coverFlag, user)), nil, nil)
		lines := newTail(60)
		run := spec.with(args...)
		err := j.runner(ctx, run, func(l string) {
			lines.add(l)
			rs.tr.AppendOutput(crlf(l)+"\r\n", nil, nil)
		})
		if ctx.Err() != nil {
			return
		}
		data, rerr := os.ReadFile(report)
		if rerr != nil {
			tc.noReport(rs, group, lines, err, rerr)
			continue
		}
		rep, perr := parseJestReport(data)
		if perr != nil {
			tc.noReport(rs, group, lines, err, perr)
			continue
		}
		rs.apply(dir, rep)
		if cover {
			tc.reportIstanbul(rs, dir, filepath.Join(covOut, "coverage-final.json"))
		}
	}
}

// noReport tells that a run left no report: the leaves of the group that have no result are errored with the end of the output.
func (tc *testCtl) noReport(rs *runState, group []*fileSel, lines *tail, runErr, readErr error) {
	j := tc.j
	j.api.Log().Warn("the run left no report", "runner", tc.runner, "err", readErr, "run", runErr)
	text := strings.TrimSpace(strings.Join(lines.all(), "\n"))
	if text == "" {
		text = "The run left no report: " + describe(readErr)
	}
	for _, s := range group {
		rs.fileFailed(s, text)
	}
}

// hasVitestCoverage tells whether a coverage provider of Vitest is installed for the project.
func hasVitestCoverage(dir string) bool {
	for d := dir; ; d = filepath.Dir(d) {
		for _, pkg := range []string{"coverage-v8", "coverage-istanbul"} {
			if fileExists(filepath.Join(d, "node_modules", "@vitest", pkg)) {
				return true
			}
		}
		if filepath.Dir(d) == d {
			return false
		}
	}
}

// reportIstanbul reads the coverage a runner wrote and reports its files.
func (tc *testCtl) reportIstanbul(rs *runState, dir, path string) {
	j := tc.j
	data, err := os.ReadFile(path)
	if err != nil {
		j.api.Log().Warn("the coverage report cannot be read", "path", path, "err", err)
		j.api.Notifications().Warn("The coverage report cannot be read: " + describe(err))
		return
	}
	files, err := parseIstanbul(data, dir, nil)
	if err != nil {
		j.api.Log().Warn("the coverage report is not valid", "err", err)
		j.api.Notifications().Warn("The coverage report cannot be read: " + describe(err))
		return
	}
	for _, f := range files {
		rs.tr.AddCoverage(f)
	}
}

// tail keeps the last lines of an output.
type tail struct {
	max   int
	lines []string
}

func newTail(n int) *tail { return &tail{max: n} }

func (t *tail) add(l string) {
	if len(t.lines) >= t.max {
		t.lines = t.lines[1:]
	}
	t.lines = append(t.lines, l)
}

func (t *tail) all() []string { return t.lines }

// ---- Node's runner ----

// runNode runs the files of a project one by one with node --test and reads the TAP as it comes.
func (tc *testCtl) runNode(ctx context.Context, rs *runState, node, dir string, sels []*fileSel, dirs *runDirs) {
	j := tc.j
	for _, s := range sels {
		if ctx.Err() != nil {
			return
		}
		out := dirs.next()
		if err := os.MkdirAll(out, 0o755); err != nil {
			j.api.Log().Warn("the folder for the reports cannot be made", "err", err)
			continue
		}
		lcov := filepath.Join(out, "lcov.info")
		args := []string{"--test", "--test-reporter=tap", "--test-reporter-destination=stdout"}
		var user []string
		if dirs.cover {
			args = append(args, "--experimental-test-coverage", "--test-reporter=lcov", "--test-reporter-destination="+lcov)
			user = append(user, "--experimental-test-coverage")
		}
		if !s.whole {
			for _, p := range tc.namePatterns([]*fileSel{s}) {
				args = append(args, "--test-name-pattern="+p)
				user = append(user, "--test-name-pattern="+p)
			}
		}
		args = append(args, s.rel)
		user = append(user, s.rel)
		rs.tr.AppendOutput(shown("node --test", user), nil, nil)
		run := &nodeRun{rs: rs, sel: s, dir: dir, lines: newTail(80)}
		err := j.runner(ctx, cmdSpec{Program: node, Args: args, Dir: dir, Env: testEnv()}, run.line)
		run.end(ctx, err)
		if dirs.cover && ctx.Err() == nil {
			tc.reportLcov(rs, dir, lcov)
		}
	}
}

// reportLcov reads the lcov file Node's runner wrote and reports its files, those of the tests themselves left out.
func (tc *testCtl) reportLcov(rs *runState, dir, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		tc.j.api.Log().Warn("the coverage report cannot be read", "path", path, "err", err)
		tc.j.api.Notifications().Warn("The coverage report cannot be read: " + describe(err))
		return
	}
	isTest := func(p string) bool {
		rel, err := filepath.Rel(dir, p)
		return err == nil && (matchNode(filepath.ToSlash(rel)) || strings.Contains(p, "/node_modules/"))
	}
	for _, f := range parseLcov(data, dir, isTest) {
		rs.tr.AddCoverage(f)
	}
}

// nodeRun reads the TAP of one process.
type nodeRun struct {
	rs     *runState
	sel    *fileSel
	dir    string
	parser tapParser
	lines  *tail
	// suiteErr are the suites that failed for a reason other than their tests, by their titles joined, with the error.
	suiteErr map[string]string
	stray    []string
}

// line reads a line of the output of node.
func (n *nodeRun) line(l string) {
	n.events(n.parser.Feed(l))
}

func (n *nodeRun) events(evs []tapEvent) {
	for _, ev := range evs {
		if ev.IsOutput {
			n.lines.add(ev.Output)
			n.stray = append(n.stray, ev.Output)
			n.rs.tr.AppendOutput(crlf(ev.Output)+"\r\n", nil, nil)
			continue
		}
		t := ev.Result
		if t.Type == "suite" {
			if !t.OK && t.FailureType != "subtestsFailed" {
				if n.suiteErr == nil {
					n.suiteErr = map[string]string{}
				}
				n.suiteErr[strings.Join(t.Path, "\x1f")] = t.failure().Text
			}
			continue
		}
		r, ok := t.toResult(n.sel.abs)
		if !ok {
			continue
		}
		if n.rs.tc.itemForRun(n.sel.project, n.sel.rel, r.Path) == nil {
			if len(r.Path) == 1 && !t.OK && n.isFile(r.Path[0]) {
				// a file that cannot be loaded is reported as a test named by its path
				n.rs.fileFailed(n.sel, strings.TrimSpace(strings.Join(n.stray, "\n")))
				continue
			}
			if !n.sel.covers(n.rs.tc, r.Path) {
				continue
			}
		}
		n.rs.result(n.dir, r)
	}
}

// isFile tells whether a name is the file of the run: a file that cannot be loaded is reported as a test with the file's path.
func (n *nodeRun) isFile(name string) bool {
	return name == n.sel.abs || name == n.sel.rel || filepath.Base(name) == filepath.Base(n.sel.abs)
}

// end finishes the run of the file: suites that failed for themselves make their unreported tests errored.
func (n *nodeRun) end(ctx context.Context, err error) {
	n.events(n.parser.Flush())
	if ctx.Err() != nil {
		return
	}
	if len(n.suiteErr) > 0 {
		for path, msg := range n.suiteErr {
			titles := strings.Split(path, "\x1f")
			for _, it := range n.sel.leaves {
				info := n.rs.tc.infoOf(it)
				if len(info.path) > len(titles) && slices.Equal(info.path[:len(titles)], titles) && !n.rs.reported[it] {
					n.rs.reported[it] = true
					n.rs.tr.Errored(it, []extension.TestMessage{{Message: msg}}, extension.TestNoDuration)
				}
			}
		}
	}
	if err != nil && !slices.ContainsFunc(n.sel.leaves, func(it *extension.TestItem) bool { return n.rs.reported[it] }) {
		var tapSeen bool
		for _, l := range n.stray {
			if strings.TrimSpace(l) != "" {
				tapSeen = true
				break
			}
		}
		if tapSeen {
			n.rs.fileFailed(n.sel, strings.TrimSpace(strings.Join(n.lines.all(), "\n")))
		}
	}
}

func (tc *testCtl) infoOf(it *extension.TestItem) testItemInfo {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.info[it]
}

// ---- reporting ----

// apply reports a runner's report: every result of a file the request asked for, and the files that could not run.
func (rs *runState) apply(dir string, rep report) {
	for _, fe := range rep.FileErrors {
		if s := rs.sels[fe.File]; s != nil {
			rs.fileFailed(s, strings.TrimSpace(stripANSI(fe.Message)))
		}
	}
	for _, r := range rep.Results {
		s := rs.sels[r.File]
		if s == nil {
			rs.tc.j.api.Log().Debug("a result is for a file that was not asked for", "file", r.File)
			continue
		}
		if rs.tc.itemForRun(s.project, s.rel, r.Path) == nil && !s.covers(rs.tc, r.Path) {
			continue
		}
		rs.result(dir, r)
	}
}

// fileFailed errors the tests of a file that could not run, with the message.
func (rs *runState) fileFailed(s *fileSel, text string) {
	if text == "" {
		text = "The file could not be run."
	}
	msg := extension.TestMessage{Message: clip(text, 4000)}
	for _, it := range s.leaves {
		if !rs.reported[it] {
			rs.reported[it] = true
			rs.tr.Errored(it, []extension.TestMessage{msg}, extension.TestNoDuration)
		}
	}
}

// result reports a test: its item is found by its titles, or added to the tree when only the run knows it.
func (rs *runState) result(dir string, r testResult) {
	tc := rs.tc
	rel, err := filepath.Rel(dir, r.File)
	if err != nil {
		return
	}
	rel = filepath.ToSlash(rel)
	it := tc.itemForRun(dir, rel, r.Path)
	if it == nil {
		if it = tc.addReported(dir, rel, r.Path, r.Line); it == nil {
			return
		}
		rs.leafSet[it] = true
		rs.tr.Enqueued(it)
	}
	if !rs.leafSet[it] || rs.reported[it] {
		return
	}
	rs.reported[it] = true
	switch r.State {
	case statePassed:
		rs.tr.Passed(it, r.Duration)
	case stateSkipped:
		rs.tr.Skipped(it)
	case stateErrored:
		rs.tr.Errored(it, rs.messages(dir, r), r.Duration)
	default:
		rs.tr.Failed(it, rs.messages(dir, r), r.Duration)
	}
}

// messages are the TestMessages of the failures of a result: the text, the two sides of a comparison, the place of the failure (the first
// frame in the project's own files) and the frames of the project.
func (rs *runState) messages(dir string, r testResult) []extension.TestMessage {
	var out []extension.TestMessage
	for _, f := range r.Failures {
		msg := extension.TestMessage{Message: clip(f.Text, 4000), ExpectedOutput: f.Expected, ActualOutput: f.Actual}
		for _, fr := range f.Frames {
			path := fr.File
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			if !within(dir, path) || strings.Contains(path, "/node_modules/") || !fileExists(path) {
				continue
			}
			pos := extension.Position{Line: max(fr.Line-1, 0), Col: max(fr.Col-1, 0)}
			msg.StackTrace = append(msg.StackTrace, extension.TestStackFrame{Label: fr.Func, URI: fileURI(path), Position: &pos})
			if msg.Location == nil {
				msg.Location = rs.location(path, fr.Line, fr.Col)
			}
		}
		out = append(out, msg)
	}
	if len(out) == 0 {
		out = []extension.TestMessage{{Message: "The test failed."}}
	}
	return out
}

// location is the place of a failure: from its column to the end of its line.
func (rs *runState) location(path string, line, col int) *extension.Location {
	if line < 1 {
		return nil
	}
	lines, ok := rs.files[path]
	if !ok {
		lines = readLines(path)
		rs.files[path] = lines
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

// clip shortens a text to n bytes.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "..."
}

// crlf makes the line breaks of the text those of a terminal, which is what the output of a run has.
func crlf(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
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

// ---- listing the files with the runner ----

// quietNode finds the node that runs the tool without telling the user when there is none.
func (j *javascript) quietNode(env langserver.Env) (string, bool) {
	if setting := j.settingString(NodePathSetting); setting != "" {
		if langserver.CheckPath(env, setting) != "" {
			return "", false
		}
		expanded := env.Expand(setting)
		if strings.Contains(expanded, "/") {
			return env.Abs(setting), true
		}
		return langserver.FindExecutable(expanded, filepath.SplitList(env.Path))
	}
	return langserver.FindExecutable("node", filepath.SplitList(env.Path))
}

// listWithRunner runs the tool of the project with the arguments and returns the files in its output: the lines that are absolute paths
// of files (or what parse makes of the output).
func (j *javascript) listWithRunner(ctx context.Context, p jsProject, tool string, args []string, parse func(lines []string) []string) ([]string, error) {
	env := j.env(p.dir)
	node, ok := j.quietNode(env)
	if !ok {
		return nil, errors.New("JsTestToolNotFound", "node was not found", "tool", tool)
	}
	script, program, found := findTool(p.dir, tool, env.Path)
	if !found {
		return nil, errors.New("JsTestToolNotFound", tool+" was not found in node_modules/.bin of the project or in PATH", "tool", tool, "project", p.dir)
	}
	spec := cmdSpec{Program: program, Args: args, Dir: p.dir, Env: testEnv()}
	if script != "" {
		spec = cmdSpec{Program: node, Args: append([]string{script}, args...), Dir: p.dir, Env: testEnv()}
	}
	var lines []string
	err := j.runner(ctx, spec, func(l string) { lines = append(lines, l) })
	if ctx.Err() != nil {
		return nil, nil
	}
	var files []string
	if parse != nil {
		files = parse(lines)
	} else {
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if filepath.IsAbs(l) && fileExists(l) {
				files = append(files, l)
			}
		}
	}
	if len(files) == 0 && err != nil {
		return nil, err
	}
	return files, nil
}
