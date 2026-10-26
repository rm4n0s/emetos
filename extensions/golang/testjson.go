package golang

import (
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"
)

// `go test -json` (SPEC §10.17 "As built (step 14.3)"): the events of test2json are read as they come into a tree of packages, tests
// and subtests, with the status, the time and the output of each. The model belongs to the UI thread; nothing here starts a program.

// Ported from VS Code Go: extension/src/testUtils.ts (the events of go test -json and the state of the tests they give).

// testEvent is one line of `go test -json`: the TestEvent of cmd/test2json, and the build events of Go 1.24 (`ImportPath` instead
// of `Package`, the actions build-output and build-fail).
type testEvent struct {
	Action      string  `json:"Action"`
	Package     string  `json:"Package"`
	ImportPath  string  `json:"ImportPath"`
	Test        string  `json:"Test"`
	Output      string  `json:"Output"`
	Elapsed     float64 `json:"Elapsed"`
	FailedBuild string  `json:"FailedBuild"`
}

// parseTestEvent reads a line. A line that is not an event of go test (the text of go itself: "go: downloading ...") fails with the
// tag TestEventInvalid, and the caller keeps it as the output of the run.
func parseTestEvent(line string) (testEvent, error) {
	var ev testEvent
	if !strings.HasPrefix(strings.TrimSpace(line), "{") {
		return ev, errors.New("TestEventInvalid", "the line is not a JSON object", "line", line)
	}
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return testEvent{}, errors.NewErr("TestEventInvalid", err).SetMetadata("line", line)
	}
	if ev.Action == "" {
		return testEvent{}, errors.New("TestEventInvalid", "the event has no action", "line", line)
	}
	return ev, nil
}

// The states of a package and of a test.
const (
	// statusRunning is a test that has started and has no result yet.
	statusRunning = iota
	statusPass
	statusFail
	statusSkip
)

// maxTestOutputLines is how many lines of output a test or a package keeps; a test that prints more loses the oldest.
const maxTestOutputLines = 2000

// testNode is a test or a subtest.
type testNode struct {
	// name is the whole name ("TestAdd/negative") and short its last element ("negative").
	name, short string
	status      int
	// elapsed is the time in seconds the result said; hasElapsed says there was one.
	elapsed    float64
	hasElapsed bool
	output     []string
	children   []*testNode
	parent     *testNode
}

// pkgNode is a package of the run.
type pkgNode struct {
	path   string
	status int
	// elapsed is the time of the package in seconds.
	elapsed    float64
	hasElapsed bool
	// output is the output that belongs to no test: the build errors, a panic outside a test, the line "FAIL path 0.1s".
	output []string
	// buildFailed says that the package did not build (go test said FailedBuild, or a build-fail event came).
	buildFailed bool
	tests       []*testNode
	byName      map[string]*testNode
}

// testResults is the tree of a run.
type testResults struct {
	packages []*pkgNode
	byPath   map[string]*pkgNode
	// stray are the lines of the run that are not events (the text of go itself).
	stray []string
}

func newTestResults() *testResults { return &testResults{byPath: map[string]*pkgNode{}} }

func (r *testResults) pkg(path string) *pkgNode {
	if p := r.byPath[path]; p != nil {
		return p
	}
	p := &pkgNode{path: path, byName: map[string]*testNode{}}
	r.byPath[path] = p
	r.packages = append(r.packages, p)
	return p
}

// node finds or makes the test of the name, and the tests above it.
func (p *pkgNode) node(name string) *testNode {
	if n := p.byName[name]; n != nil {
		return n
	}
	n := &testNode{name: name, short: name}
	if i := strings.LastIndex(name, "/"); i >= 0 {
		n.short = name[i+1:]
		n.parent = p.node(name[:i])
		n.parent.children = append(n.parent.children, n)
	} else {
		p.tests = append(p.tests, n)
	}
	p.byName[name] = n
	return n
}

func appendLine(lines []string, text string) []string {
	lines = append(lines, text)
	if len(lines) > maxTestOutputLines {
		lines = append([]string(nil), lines[len(lines)-maxTestOutputLines:]...)
	}
	return lines
}

// noise tells that a line of the output of a test only repeats what the tree shows: the markers of go test -v.
func noise(line string) bool {
	return strings.HasPrefix(line, "=== RUN") || strings.HasPrefix(line, "=== PAUSE") || strings.HasPrefix(line, "=== CONT") ||
		strings.HasPrefix(line, "=== NAME") || strings.HasPrefix(line, "=== FAILED") || strings.HasPrefix(line, "=== SKIP")
}

// addOutput adds the text of an output event (it may hold several lines, and ends with a line break that is not a line).
func addOutput(lines []string, text string) []string {
	for _, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		l = strings.TrimSuffix(l, "\r")
		if noise(l) {
			continue
		}
		lines = appendLine(lines, l)
	}
	return lines
}

// stripImportPath is the package of the import path of a build event: "example.com/p [example.com/p.test]" is "example.com/p".
func stripImportPath(importPath string) string {
	if i := strings.Index(importPath, " ["); i >= 0 {
		return importPath[:i]
	}
	return importPath
}

// addLine reads a line of the run: an event is applied, any other line is kept as the output of the run.
func (r *testResults) addLine(line string) {
	ev, err := parseTestEvent(line)
	if err != nil {
		if strings.TrimSpace(line) != "" {
			r.stray = appendLine(r.stray, line)
		}
		return
	}
	r.apply(ev)
}

// apply applies one event.
func (r *testResults) apply(ev testEvent) {
	path := ev.Package
	if path == "" {
		path = stripImportPath(ev.ImportPath)
	}
	if path == "" {
		return
	}
	p := r.pkg(path)
	switch ev.Action {
	case "build-output":
		p.output = addOutput(p.output, ev.Output)
		return
	case "build-fail":
		p.buildFailed = true
		p.status = statusFail
		return
	case "start":
		return
	}
	if ev.Test == "" {
		r.applyPackage(p, ev)
		return
	}
	n := p.node(ev.Test)
	switch ev.Action {
	case "output":
		n.output = addOutput(n.output, ev.Output)
	case "pass", "fail", "skip":
		n.status = map[string]int{"pass": statusPass, "fail": statusFail, "skip": statusSkip}[ev.Action]
		n.elapsed, n.hasElapsed = ev.Elapsed, true
	}
}

// applyPackage applies an event of the package itself: its output and its result.
func (r *testResults) applyPackage(p *pkgNode, ev testEvent) {
	switch ev.Action {
	case "output":
		p.output = addOutput(p.output, ev.Output)
	case "pass", "fail", "skip":
		p.status = map[string]int{"pass": statusPass, "fail": statusFail, "skip": statusSkip}[ev.Action]
		p.elapsed, p.hasElapsed = ev.Elapsed, true
		if ev.FailedBuild != "" {
			p.buildFailed = true
		}
		if ev.Action == "fail" {
			// a test that was running when the package failed (a panic, a timeout) did not pass
			for _, n := range p.byName {
				if n.status == statusRunning {
					n.status = statusFail
				}
			}
		}
	}
}

// finish ends the run: whatever has no result (the run was stopped, or the program was killed) is marked as not run.
func (r *testResults) finish() {
	for _, p := range r.packages {
		if p.status == statusRunning {
			p.status = statusSkip
		}
		for _, n := range p.byName {
			if n.status == statusRunning {
				n.status = statusSkip
			}
		}
	}
}

// testCounts are the numbers of the tests of a run (the subtests count, a parent test counts as a test).
type testCounts struct {
	Pass, Fail, Skip, Running int
	// Packages and FailedPackages count the packages; BuildFailed those that did not build.
	Packages, FailedPackages, BuildFailed int
}

// counts counts the tests and packages.
func (r *testResults) counts() testCounts {
	var c testCounts
	for _, p := range r.packages {
		c.Packages++
		if p.status == statusFail {
			c.FailedPackages++
		}
		if p.buildFailed {
			c.BuildFailed++
		}
		for _, n := range p.byName {
			switch n.status {
			case statusPass:
				c.Pass++
			case statusFail:
				c.Fail++
			case statusSkip:
				c.Skip++
			default:
				c.Running++
			}
		}
	}
	return c
}

// failing tells that the node or a test below it failed.
func (n *testNode) failing() bool {
	if n.status == statusFail {
		return true
	}
	for _, c := range n.children {
		if c.failing() {
			return true
		}
	}
	return false
}
