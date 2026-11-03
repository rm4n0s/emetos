package javascript

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// readFixture reads a file of testdata.
func readFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// recorded reads a file of testdata/recorded with {{dir}} replaced by the folder of the project.
func recorded(t *testing.T, name, dir string) string {
	t.Helper()
	return strings.ReplaceAll(readFixture(t, "testdata/recorded/"+name), "{{dir}}", dir)
}

// treeDump writes the items as indented lines: the label, "@line" (from 1) for an item with a place and "!text" for one with an error.
func treeDump(c *extension.TestItemCollection) string {
	var sb strings.Builder
	var walk func(items []*extension.TestItem, depth int)
	walk = func(items []*extension.TestItem, depth int) {
		for _, it := range items {
			d := it.Data()
			sb.WriteString(strings.Repeat("  ", depth) + strings.ReplaceAll(d.Label, "/", "|"))
			if d.Range != nil {
				sb.WriteString(" @" + strconv.Itoa(d.Range.Start.Line+1))
			}
			if d.Error != "" {
				sb.WriteString(" !" + d.Error)
			}
			sb.WriteString("\n")
			walk(it.Children().All(), depth+1)
		}
	}
	walk(c.All(), 0)
	return sb.String()
}

// treeFind finds an item by the labels from the top, joined by "/" (no label of the fixtures has a slash except a file's path, so the
// path of a file is written with its slashes replaced by "|").
func treeFind(t *testing.T, c *extension.TestItemCollection, path string) *extension.TestItem {
	t.Helper()
	cur := c
	var it *extension.TestItem
	for _, label := range strings.Split(path, "/") {
		it = nil
		for _, x := range cur.All() {
			if strings.ReplaceAll(x.Data().Label, "/", "|") == label {
				it = x
				break
			}
		}
		if it == nil {
			t.Fatalf("no item %q (at %q) in\n%s", path, label, treeDump(c))
		}
		cur = it.Children()
	}
	return it
}

// itemLabel names an item for the tests: its labels from the top joined by slashes (a file's slashes are "|").
func itemLabel(it *extension.TestItem) string {
	var parts []string
	for x := it; x != nil; x = x.Parent() {
		parts = append(parts, strings.ReplaceAll(x.Data().Label, "/", "|"))
	}
	slices.Reverse(parts)
	return strings.Join(parts, "/")
}

// runOutcomes are the last state of each item the run reported, by the labels from the top.
func runOutcomes(run *fakeapi.FakeTestRun) map[string]string {
	got := map[string]string{}
	for _, e := range run.Events() {
		switch e.Kind {
		case fakeapi.RunEnqueued, fakeapi.RunStarted, fakeapi.RunPassed, fakeapi.RunFailed, fakeapi.RunErrored, fakeapi.RunSkipped:
			got[itemLabel(e.Item)] = string(e.Kind)
		}
	}
	return got
}

// runMessages are the messages of the failure of the item.
func runMessages(run *fakeapi.FakeTestRun, label string) []extension.TestMessage {
	var msgs []extension.TestMessage
	for _, e := range run.Events() {
		if e.Item != nil && itemLabel(e.Item) == label && (e.Kind == fakeapi.RunFailed || e.Kind == fakeapi.RunErrored) {
			msgs = e.Messages
		}
	}
	return msgs
}

// runOutput is the text of the output of the run, or only of the item.
func runOutput(run *fakeapi.FakeTestRun, item string) string {
	var sb strings.Builder
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunOutput && (item == "" || (e.Item != nil && itemLabel(e.Item) == item)) {
			sb.WriteString(e.Text)
		}
	}
	return sb.String()
}

// runCoverage are the files the run reported coverage for.
func runCoverage(run *fakeapi.FakeTestRun) []extension.FileCoverage {
	var out []extension.FileCoverage
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunCoverage {
			out = append(out, e.Coverage)
		}
	}
	return out
}

// wantRoute checks that the error is an *errors.Error whose route has the text.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), route)
	}
}
