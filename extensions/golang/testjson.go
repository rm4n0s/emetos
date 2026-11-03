package golang

import (
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"
)

// `go test -json` (SPEC §10.17 "As built (step 14.3)", "(step 26.5)"): the events of test2json, read line by line; the run of the
// controller (testexec.go) applies them to the items of the tree. Nothing here starts a program.

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

// maxTestOutputLines is how many lines of output a test or a package keeps; a test that prints more loses the oldest.
const maxTestOutputLines = 2000

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
