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
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Running the tests of the controller (SPEC §10.32 "The providers", Go; §10.17 "As built (step 26.5)"): the request is turned into
// invocations of `go test -json`, each in a process group of its own, whose events are read as they come into the TestRun; a run with
// coverage reads the profiles the invocations wrote; Debug starts the Go debugger on the tests (step 25.7).

// testSel is what a request selects in one package.
type testSel struct {
	pkg *testPkg
	// whole says that the whole package is run.
	whole bool
	// names are the test, example and fuzz functions, benches the benchmarks and subs the subtests (by their whole name).
	names, benches, subs []string
	// items are the items selected, whose tests are queued.
	items []*extension.TestItem
}

// testInv is one invocation of go test.
type testInv struct {
	dir string
	// run is the value of -run, bench that of -bench (the benchmarks only), pkgs the packages ("./..." for a module).
	run, bench string
	pkgs       []string
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

// plan turns a request into what it selects, by package in the order of the tree: Include (all the items with none), without Exclude,
// where an item with an excluded item below it is replaced by its children.
func (tc *testCtl) plan(req *extension.TestRunRequest) []*testSel {
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
	sels := map[*testPkg]*testSel{}
	var order []*testSel
	var add func(it *extension.TestItem, viaParent bool)
	add = func(it *extension.TestItem, viaParent bool) {
		info, known := tc.info[it]
		if !known || excluded[it] {
			return
		}
		if kids := it.Children().All(); len(kids) > 0 && hasExcludedBelow(it, excluded) {
			for _, k := range kids {
				add(k, true)
			}
			return
		}
		if viaParent && info.role == roleFunc && info.kind == kindBenchmark {
			return // a package or a file run does not run the benchmarks
		}
		sel := sels[info.pkg]
		if sel == nil {
			sel = &testSel{pkg: info.pkg}
			sels[info.pkg] = sel
			order = append(order, sel)
		}
		sel.items = append(sel.items, it)
		switch info.role {
		case rolePackage:
			sel.whole = true
		case roleFile:
			for _, c := range it.Children().All() {
				if ci := tc.info[c]; ci.kind != kindBenchmark {
					sel.names = append(sel.names, ci.name)
				}
			}
		default:
			switch {
			case strings.Contains(info.name, "/"):
				sel.subs = append(sel.subs, info.name)
			case info.kind == kindBenchmark:
				sel.benches = append(sel.benches, info.name)
			default:
				sel.names = append(sel.names, info.name)
			}
		}
	}
	for _, r := range roots {
		add(r, false)
	}
	for _, sel := range order {
		sel.names = uniq(sel.names)
		sel.benches = uniq(sel.benches)
		sel.subs = uniq(sel.subs)
		sel.subs = slices.DeleteFunc(sel.subs, func(s string) bool {
			top, _, _ := strings.Cut(s, "/")
			return slices.Contains(sel.names, top)
		})
	}
	return order
}

// subPattern is the value of -run for a subtest: a level of the name for each element.
func subPattern(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = "^" + regexp.QuoteMeta(p) + "$"
	}
	return strings.Join(parts, "/")
}

// invocations are the runs of go test the selections need: a module whose packages are all selected whole runs as ./..., any other
// package by itself with a -run for its functions, one for its benchmarks and one for each subtest.
func (tc *testCtl) invocations(sels []*testSel) []testInv {
	tc.mu.Lock()
	perModule := map[string]int{}
	for _, p := range tc.pkgs {
		perModule[p.modRoot]++
	}
	tc.mu.Unlock()
	whole := map[string][]*testSel{}
	for _, s := range sels {
		if s.whole && s.pkg.modRoot != "" {
			whole[s.pkg.modRoot] = append(whole[s.pkg.modRoot], s)
		}
	}
	var out []testInv
	inModule := map[string]bool{}
	for root, ws := range whole {
		if len(ws) > 1 && len(ws) == perModule[root] {
			inModule[root] = true
		}
	}
	roots := make([]string, 0, len(inModule))
	for r := range inModule {
		roots = append(roots, r)
	}
	sort.Strings(roots)
	for _, r := range roots {
		out = append(out, testInv{dir: r, pkgs: []string{"./..."}})
	}
	for _, s := range sels {
		dir := s.pkg.dir
		switch {
		case s.whole && inModule[s.pkg.modRoot]:
		case s.whole:
			out = append(out, testInv{dir: dir})
		default:
			if len(s.names) > 0 {
				out = append(out, testInv{dir: dir, run: runPattern(s.names)})
			}
			if len(s.benches) > 0 {
				out = append(out, testInv{dir: dir, bench: runPattern(s.benches)})
			}
			for _, sub := range s.subs {
				out = append(out, testInv{dir: dir, run: subPattern(sub)})
			}
		}
	}
	return out
}

// handler is the handler of a run profile.
func (tc *testCtl) handler(kind extension.TestRunKind) extension.TestRunHandler {
	return func(ctx context.Context, req *extension.TestRunRequest) error {
		if kind == extension.TestKindDebug {
			tc.debugRequest(req)
			return nil
		}
		tc.runRequest(ctx, req, kind == extension.TestKindCoverage)
		return nil
	}
}

// start runs the items with the profile of the kind for a command: a new run stops the one the commands started before.
func (tc *testCtl) start(kind extension.TestRunKind, items []*extension.TestItem) {
	g := tc.g
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID()
	}
	tc.mu.Lock()
	tc.last = &lastTest{ids: ids, kind: kind}
	tc.mu.Unlock()
	tc.cancelRuns()
	ctx, cancel := context.WithCancel(g.ctx)
	tc.mu.Lock()
	tc.nrun++
	n := tc.nrun
	tc.runs[n] = cancel
	tc.mu.Unlock()
	req := &extension.TestRunRequest{Include: items, Profile: tc.profiles[kind]}
	g.closers.Add(1)
	go func() {
		defer g.closers.Done()
		defer func() {
			cancel()
			tc.mu.Lock()
			delete(tc.runs, n)
			tc.mu.Unlock()
		}()
		if err := tc.handler(kind)(ctx, req); err != nil {
			g.api.Log().Warn("the tests cannot be run", "err", err)
		}
	}()
}

// startLast runs the last run of a command again, with the items the tree has now; it reports whether there was one.
func (tc *testCtl) startLast() bool {
	tc.mu.Lock()
	last := tc.last
	var items []*extension.TestItem
	if last != nil {
		for _, id := range last.ids {
			if it := tc.byID[id]; it != nil {
				items = append(items, it)
			}
		}
	}
	tc.mu.Unlock()
	if last == nil {
		return false
	}
	if len(items) == 0 {
		tc.g.api.Notifications().Info("The tests of the last run are gone.")
		return true
	}
	tc.start(last.kind, items)
	return true
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

// enqueue queues the tests of the item and returns them: its leaves (the benchmarks only when asked for).
func (tc *testCtl) enqueue(tr extension.TestRun, it *extension.TestItem, benchmarks bool) []*extension.TestItem {
	tc.mu.Lock()
	info := tc.info[it]
	tc.mu.Unlock()
	if info.role == roleFunc && info.kind == kindBenchmark && !benchmarks {
		return nil
	}
	kids := it.Children().All()
	if len(kids) == 0 {
		tr.Enqueued(it)
		return []*extension.TestItem{it}
	}
	var out []*extension.TestItem
	for _, k := range kids {
		out = append(out, tc.enqueue(tr, k, benchmarks && info.role == roleFunc)...)
	}
	return out
}

// runRequest runs the request with go test and reports it. It returns when the run has ended; ctx stops it.
func (tc *testCtl) runRequest(ctx context.Context, req *extension.TestRunRequest, cover bool) {
	g := tc.g
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(g.ctx, cancel)()
	program, err := g.goProgram()
	if err != nil {
		g.api.Log().Warn("the go program was not found", "err", err)
		g.api.Notifications().Error("The go command was not found. Install Go, or set go.alternateTools in the settings.")
		return
	}
	sels := tc.plan(req)
	invs := tc.invocations(sels)
	if len(invs) == 0 {
		g.api.Notifications().Info("There are no Go tests to run.")
		return
	}
	tr, err := tc.ctl.CreateTestRun(req, "go test", true)
	if err != nil {
		g.api.Log().Warn("the test run cannot be made", "err", err)
		return
	}
	defer tr.End()
	queued := map[*testPkg][]*extension.TestItem{}
	for _, s := range sels {
		for _, it := range s.items {
			leaves := tc.enqueue(tr, it, true)
			queued[s.pkg] = append(queued[s.pkg], leaves...)
		}
	}
	coverDir := ""
	if cover {
		if coverDir, err = os.MkdirTemp("", "emetos-gocover-"); err != nil {
			g.api.Log().Warn("the folder for the coverage profile cannot be made", "err", err)
			g.api.Notifications().Warn("The coverage cannot be measured: " + describe(err))
			cover = false
		} else {
			defer func() {
				if err := os.RemoveAll(coverDir); err != nil {
					g.api.Log().Warn("the coverage folder cannot be removed", "dir", coverDir, "err", err)
				}
			}()
		}
	}
	g.api.Post(func() { g.api.Problems().Clear(TestsMarkerOwner) })
	run := &goTestRun{tc: tc, tr: tr, workspace: g.api.Scope().Folder, outputs: map[*extension.TestItem][]string{}, running: map[*extension.TestItem]*testPkg{},
		failed: map[*extension.TestItem]bool{}, queued: queued, pkgOut: map[string][]string{}, builds: map[string]bool{}, failedIn: map[*testPkg]bool{}}
	env := g.testEnv()
	var profiles []string
	for i, inv := range invs {
		if ctx.Err() != nil {
			break
		}
		profile := ""
		if cover {
			profile = filepath.Join(coverDir, "cover"+strconv.Itoa(i)+".out")
			profiles = append(profiles, profile)
		}
		args := g.goTestArgs(inv, profile)
		tr.AppendOutput("$ go "+strings.Join(args, " ")+"\r\n", nil, nil)
		run.events, run.stray, run.cwd = 0, nil, inv.dir
		err := runProgram(ctx, cmdSpec{Program: program, Args: args, Dir: inv.dir, Env: env}, run.line)
		run.endInvocation(ctx, err)
	}
	if cover && ctx.Err() == nil {
		tc.reportCoverage(tr, profiles)
	}
	run.postMarkers()
}

// reportCoverage reads the profiles the invocations wrote and reports the files they cover.
func (tc *testCtl) reportCoverage(tr extension.TestRun, profiles []string) {
	g := tc.g
	all := map[string][]coverBlock{}
	for _, p := range profiles {
		data, err := os.ReadFile(p)
		if err != nil {
			g.api.Log().Warn("the coverage profile cannot be read", "profile", p, "err", err)
			continue
		}
		blocks, err := parseCoverProfile(string(data))
		if err != nil {
			g.api.Log().Warn("the coverage profile is not valid", "profile", p, "err", err)
			g.api.Notifications().Warn("The coverage profile cannot be read: " + describe(err))
			continue
		}
		mergeBlocks(all, blocks)
	}
	files := make([]string, 0, len(all))
	for f := range all {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		path, ok := tc.coveredFile(f)
		if !ok {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			g.api.Log().Warn("a covered file cannot be read", "path", path, "err", err)
		}
		tr.AddCoverage(fileCoverage(fileURI(path), string(src), all[f]))
	}
}

// coveredFile is the path of the file the profile names by import path ("example.com/p/add.go"): in the folder of the package, else in
// the module that holds it.
func (tc *testCtl) coveredFile(importFile string) (string, bool) {
	dir, base := filepath.Split(importFile)
	importPath := strings.TrimSuffix(filepath.ToSlash(dir), "/")
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if p := tc.byImport[importPath]; p != nil {
		return filepath.Join(p.dir, base), true
	}
	for _, m := range tc.mods {
		if !m.ok {
			continue
		}
		if rest, ok := strings.CutPrefix(importPath, m.path); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
			return filepath.Join(m.root, filepath.FromSlash(strings.TrimPrefix(rest, "/")), base), true
		}
	}
	return "", false
}

// ---- the events ----

// goTestRun is the state of one run while the events of its invocations come. Its methods run on the goroutine that reads the output of
// the program, one invocation after the other.
type goTestRun struct {
	tc        *testCtl
	tr        extension.TestRun
	workspace string
	// outputs are the lines each test printed, running the tests that started and have no result, with their package, failed the tests
	// that failed, and failedIn the packages with a failed test.
	outputs  map[*extension.TestItem][]string
	running  map[*extension.TestItem]*testPkg
	failed   map[*extension.TestItem]bool
	queued   map[*testPkg][]*extension.TestItem
	failedIn map[*testPkg]bool
	// pkgOut is the output of each package that belongs to no test, by import path; builds the packages whose build failure was told.
	pkgOut map[string][]string
	builds map[string]bool
	// findings are the places named by the failures, for the Problems panel.
	findings []finding
	// cwd is the folder go test runs in: the compiler names the files relative to it.
	cwd string
	// events counts the events of the invocation and stray holds the lines that were not events.
	events int
	stray  []string
}

// crlf makes the line breaks of the text those of a terminal, which is what the output of a run has.
func crlf(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
}

// line reads a line of the output of go test.
func (r *goTestRun) line(l string) {
	ev, err := parseTestEvent(l)
	if err != nil {
		if strings.TrimSpace(l) != "" {
			r.stray = append(r.stray, l)
		}
		r.tr.AppendOutput(l+"\r\n", nil, nil)
		return
	}
	r.events++
	path := ev.Package
	if path == "" {
		path = stripImportPath(ev.ImportPath)
	}
	if path == "" {
		return
	}
	pkg := r.tc.packageByImport(path)
	switch ev.Action {
	case "build-output":
		r.pkgOut[path] = addOutput(r.pkgOut[path], ev.Output)
		r.tr.AppendOutput(crlf(ev.Output), nil, nil)
		return
	case "build-fail":
		r.buildFailed(path, pkg)
		return
	case "start":
		return
	}
	if ev.Test == "" || pkg == nil {
		r.packageEvent(ev, path, pkg)
		return
	}
	item := r.tc.testItem(pkg, ev.Test)
	// go test -json rounds Elapsed to 0.01 s, so a fast test reports 0, which is "not measured" and not a duration of 0.0 ms
	d := extension.TestNoDuration
	if ev.Elapsed > 0 {
		d = time.Duration(ev.Elapsed * float64(time.Second))
	}
	switch ev.Action {
	case "run":
		r.tr.Started(item)
		r.running[item] = pkg
	case "output":
		r.outputs[item] = addOutput(r.outputs[item], ev.Output)
		r.tr.AppendOutput(crlf(ev.Output), nil, item)
	case "pass":
		delete(r.running, item)
		r.tr.Passed(item, d)
	case "skip":
		delete(r.running, item)
		r.tr.Skipped(item)
	case "fail":
		delete(r.running, item)
		r.failed[item] = true
		r.failedIn[pkg] = true
		r.tr.Failed(item, r.messagesFor(item, pkg), d)
	}
}

// packageEvent applies an event of a package: its output, and its end.
func (r *goTestRun) packageEvent(ev testEvent, path string, pkg *testPkg) {
	switch ev.Action {
	case "output":
		r.pkgOut[path] = addOutput(r.pkgOut[path], ev.Output)
		var item *extension.TestItem
		if pkg != nil {
			item = pkg.item
		}
		r.tr.AppendOutput(crlf(ev.Output), nil, item)
	case "pass":
		// a benchmark has no result of its own in the events: it passed with its package
		for it, p := range r.running {
			if p == pkg {
				r.tr.Passed(it, extension.TestNoDuration)
				delete(r.running, it)
			}
		}
	case "fail":
		if ev.FailedBuild != "" || r.builds[path] {
			r.buildFailed(path, pkg)
			return
		}
		r.packageFailed(path, pkg)
	}
}

// buildFailed tells that the package did not build, once: the package and the tests queued in it are errored, with the places of the
// compiler's messages.
func (r *goTestRun) buildFailed(path string, pkg *testPkg) {
	if r.builds[path] {
		return
	}
	r.builds[path] = true
	if pkg == nil {
		return
	}
	lines := cleanLines(r.pkgOut[path])
	r.findings = append(r.findings, failureFindings(lines, r.cwd, r.workspace, "")...)
	msgs := r.placeMessages(lines, pkg, r.cwd)
	if len(msgs) == 0 {
		msgs = []extension.TestMessage{{Message: textOf(lines, "The package did not build.")}}
	}
	r.tr.Errored(pkg.item, msgs, extension.TestNoDuration)
	for _, it := range r.queued[pkg] {
		r.tr.Errored(it, []extension.TestMessage{{Message: "The package did not build."}}, extension.TestNoDuration)
	}
}

// packageFailed is the end of a package that failed without a build failure: a test that was running did not end (a panic outside
// it, a timeout), or the package failed on its own (TestMain, an init). The tests that were running, else the package, are errored
// with what the package printed.
func (r *goTestRun) packageFailed(path string, pkg *testPkg) {
	if pkg == nil {
		return
	}
	lines := cleanLines(r.pkgOut[path])
	r.findings = append(r.findings, failureFindings(lines, pkg.dir, r.workspace, "")...)
	msgs := r.placeMessages(lines, pkg, pkg.dir)
	if len(msgs) == 0 {
		msgs = []extension.TestMessage{{Message: textOf(r.pkgOut[path], "The package failed.")}}
	}
	stuck := false
	for it, p := range r.running {
		if p != pkg {
			continue
		}
		stuck = true
		r.failed[it] = true
		own := cleanLines(r.outputs[it])
		itemMsgs := r.placeMessages(own, pkg, pkg.dir)
		if len(itemMsgs) == 0 {
			if text := textOf(own, ""); text != "" {
				itemMsgs = []extension.TestMessage{{Message: text}}
			} else {
				itemMsgs = msgs
			}
		}
		r.tr.Errored(it, itemMsgs, extension.TestNoDuration)
		delete(r.running, it)
	}
	if !stuck && !r.failedIn[pkg] {
		r.tr.Errored(pkg.item, msgs, extension.TestNoDuration)
	}
}

// endInvocation is the end of a run of go test: a program that could not start, or that failed without telling anything (an unknown
// flag, no module), is told to the user with its first line.
func (r *goTestRun) endInvocation(ctx context.Context, err error) {
	if err == nil || ctx.Err() != nil || r.events > 0 {
		return
	}
	var exit *exec.ExitError
	what := describe(err)
	if errors.As(err, &exit) && len(r.stray) > 0 {
		what = r.stray[0]
	}
	r.tc.g.api.Log().Warn("go test failed without results", "err", err)
	r.tc.g.api.Notifications().Error("go test failed: " + what)
}

// postMarkers puts the places named by the failures into the Problems panel, on the UI thread.
func (r *goTestRun) postMarkers() {
	g := r.tc.g
	byURI := map[string][]extension.Marker{}
	seen := map[string]bool{}
	for _, f := range r.findings {
		key := f.Path + ":" + strconv.Itoa(f.Line) + ":" + f.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		uri := fileURI(f.Path)
		byURI[uri] = append(byURI[uri], markerOf(f, TestsMarkerOwner))
	}
	if len(byURI) == 0 {
		return
	}
	g.api.Post(func() {
		for uri, markers := range byURI {
			if err := g.api.Problems().Set(TestsMarkerOwner, uri, markers); err != nil {
				g.api.Log().Warn("the markers of the failed tests cannot be set", "uri", uri, "err", err)
			}
		}
	})
}

// ---- the messages of a failure ----

// frameLine tells whether the line of the output only repeats what the tree shows: the markers of go test -v and its --- lines.
func frameLine(l string) bool {
	t := strings.TrimSpace(l)
	return strings.HasPrefix(t, "=== ") || strings.HasPrefix(t, "--- FAIL") || strings.HasPrefix(t, "--- PASS") || strings.HasPrefix(t, "--- SKIP") ||
		t == "FAIL" || t == "PASS" || strings.HasPrefix(t, "FAIL\t") || strings.HasPrefix(t, "ok  \t") || strings.HasPrefix(t, "exit status ")
}

// cleanLines drops the lines that only repeat what the tree shows.
func cleanLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if !frameLine(l) {
			out = append(out, l)
		}
	}
	return out
}

// textOf joins the first lines that are not blank, or is the fallback when there are none.
func textOf(lines []string, fallback string) string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimRight(l, " \t"))
		}
		if len(out) >= 30 {
			break
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return strings.Join(out, "\n")
}

// wantRes read the messages of the form `got X, want Y` and `F(args) = X, want Y`, which tests write by habit.
var wantRes = []*regexp.Regexp{
	regexp.MustCompile(`^(?:.*?\b)?got:?\s+(.+?),?\s+want:?\s+(.+)$`),
	regexp.MustCompile(`^.*?\s=\s(.+?),\s+want\s+(.+)$`),
}

// splitWant finds the actual and the expected value in a message of the form `got X, want Y`; both are empty when it has none.
func splitWant(msg string) (expected, actual string) {
	if strings.Contains(msg, "\n") {
		return "", ""
	}
	for _, re := range wantRes {
		if m := re.FindStringSubmatch(msg); m != nil {
			return strings.TrimSpace(m[2]), strings.TrimSpace(m[1])
		}
	}
	return "", ""
}

// stackFrames are the frames of a panic that are in the code of the module (root), innermost first, each with the function printed
// above it.
func stackFrames(lines []string, root string) []extension.TestStackFrame {
	var out []extension.TestStackFrame
	for i, l := range lines {
		m := frameRe.FindStringSubmatch(l)
		if m == nil || !within(root, m[1]) {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil || n < 1 {
			continue
		}
		label := ""
		if i > 0 && frameRe.FindStringSubmatch(lines[i-1]) == nil {
			label = strings.TrimSpace(lines[i-1])
			if j := strings.IndexByte(label, '('); j > 0 {
				label = label[:j]
			}
		}
		out = append(out, extension.TestStackFrame{Label: label, URI: fileURI(filepath.Clean(m[1])), Position: &extension.Position{Line: n - 1}})
		if len(out) >= 30 {
			break
		}
	}
	return out
}

// placeMessages are the messages for the places the lines name (`file_test.go:12: message`, a compiler error, the first frame of a
// panic in the module); a panic has the frames of the module as its stack trace, and a `got X, want Y` message its two values.
func (r *goTestRun) placeMessages(lines []string, pkg *testPkg, dir string) []extension.TestMessage {
	root := pkg.modRoot
	if root == "" {
		root = pkg.dir
	}
	var msgs []extension.TestMessage
	frames := stackFrames(lines, root)
	for _, f := range failureFindings(lines, dir, root, "") {
		at := extension.Position{Line: f.Line - 1, Col: max(f.Col-1, 0)}
		m := extension.TestMessage{
			Message:  f.Message,
			Location: &extension.Location{URI: fileURI(f.Path), Range: extension.Range{Start: at, End: extension.Position{Line: at.Line, Col: at.Col + 1}}},
		}
		if strings.HasPrefix(f.Message, "panic: ") {
			m.StackTrace = frames
		} else {
			m.ExpectedOutput, m.ActualOutput = splitWant(f.Message)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

// messagesFor are the messages of the failure of a test: none for a test that failed only because a test below it did, else the places
// its output names, else what it printed. The places also go to the Problems panel.
func (r *goTestRun) messagesFor(item *extension.TestItem, pkg *testPkg) []extension.TestMessage {
	for _, c := range item.Children().All() {
		if r.failed[c] {
			return nil
		}
	}
	lines := cleanLines(r.outputs[item])
	r.tc.mu.Lock()
	name := r.tc.info[item].name
	r.tc.mu.Unlock()
	r.findings = append(r.findings, failureFindings(lines, pkg.dir, r.workspace, name)...)
	msgs := r.placeMessages(lines, pkg, pkg.dir)
	if len(msgs) == 0 {
		msgs = []extension.TestMessage{{Message: textOf(lines, "The test failed.")}}
	}
	return msgs
}

// ---- Debug ----

// debugRequest starts the Go debugger on the tests the request selects: one package, with one -test.run (or -test.bench) for what is
// selected in it (the whole package when it is selected whole, else the functions, the benchmarks or the first subtest); the rest is
// told to the user.
func (tc *testCtl) debugRequest(req *extension.TestRunRequest) {
	g := tc.g
	sels := tc.plan(req)
	if len(sels) == 0 {
		g.api.Notifications().Info("There are no Go tests to debug.")
		return
	}
	sel := sels[0]
	run, bench := "", ""
	switch {
	case sel.whole:
	case len(sel.names) > 0:
		run = runPattern(sel.names)
	case len(sel.benches) > 0:
		bench = runPattern(sel.benches)
	case len(sel.subs) > 0:
		run = subPattern(sel.subs[0])
	}
	if len(sels) > 1 || (len(sel.names) > 0 && len(sel.benches)+len(sel.subs) > 0) || len(sel.subs) > 1 {
		g.api.Notifications().Info("The debugger runs the tests of one package at a time: debugging " + g.runLabel(sel.pkg.dir) + ".")
	}
	cfg := g.debugTestConfigFor(sel.pkg.dir, run, bench)
	g.api.Post(func() { g.startDebugConfig(cfg, "the tests of "+g.runLabel(sel.pkg.dir)) })
}

// uniq drops the repeated names, keeping the order of the first ones.
func uniq(names []string) []string {
	seen := map[string]bool{}
	out := names[:0]
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}
