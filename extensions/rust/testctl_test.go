package rust

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The tree of the tests and the runs of the controller (step 26.7) over a fake cargo: a script in the PATH that answers the commands the
// pack runs with the recorded output of the real cargo (testdata/cargo, recorded from the crate testdata/tests) and writes down the
// arguments it was called with. The real cargo runs under the tag emetos_cargo (testreal_test.go).

// fakeCase is an answer of the fake cargo: the arguments (a shell pattern), the recorded file, the exit status and the seconds it takes.
type fakeCase struct {
	args, file string
	code       int
	sleep      int
}

// cargoRig is the pack activated over a copy of the crate testdata/tests with a fake cargo.
type cargoRig struct {
	*rig
	calls string
}

// copyDir copies a folder.
func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// listCases and runCases are the answers of the fake cargo for the targets of the crate.
var listCases = []fakeCase{
	{args: "metadata --no-deps --format-version 1", file: "metadata.json"},
	{args: "test -p calc --lib -- --list --format terse", file: "list_lib.txt"},
	{args: "test -p calc --bin calc -- --list --format terse", file: "list_bin.txt"},
	{args: "test -p calc --test integration -- --list --format terse", file: "list_test.txt"},
	{args: "test -p calc --doc -- --list --format terse", file: "list_doc.txt"},
}

var runCases = []fakeCase{
	{args: "test -p calc --lib --no-fail-fast -- --show-output", file: "run_lib.txt", code: 101},
	{args: "test -p calc --lib --no-fail-fast -- --show-output --exact tests::adds tests::nested::deep tests::divides", file: "run_lib_exact.txt", code: 101},
	{args: "test -p calc --bin calc --no-fail-fast -- --show-output", file: "run_bin.txt"},
	{args: "test -p calc --test integration --no-fail-fast -- --show-output", file: "run_test.txt", code: 101},
	{args: "test -p calc --doc --no-fail-fast -- --show-output", file: "run_doc.txt", code: 101},
	{args: "test -p calc --doc --no-fail-fast -- --show-output divide", file: "run_doc_one.txt", code: 101},
}

// fakeCargoScript is the program: it answers by the arguments and logs them.
func fakeCargoScript(cases []fakeCase) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\necho \"$*\" >> \"$CALLS\"\n")
	sb.WriteString("sub() { sed -e \"s#{{dir}}#$FOLDER#g\" -e \"s#{{target}}#$FOLDER/target#g\" \"$FIX/$1\"; }\ncase \"$*\" in\n")
	for _, c := range cases {
		sb.WriteString("'" + c.args + "') ")
		if c.sleep > 0 {
			sb.WriteString("echo $$ > \"$FOLDER/../pid\"; sleep " + strconv.Itoa(c.sleep) + "; ")
		}
		if c.file != "" {
			sb.WriteString("sub " + c.file + "; ")
		}
		sb.WriteString("exit " + strconv.Itoa(c.code) + ";;\n")
	}
	sb.WriteString("*) echo \"unexpected cargo: $*\" >&2; exit 2;;\nesac\n")
	return sb.String()
}

// cargoOptions say what a test rig has.
type cargoOptions struct {
	// cases are the answers of the fake cargo; nil is listCases and runCases. noCargo leaves the PATH with no cargo.
	cases    []fakeCase
	noCargo  bool
	settings map[string]any
	// files are written over the copy of the crate (a name to text; "" removes the file).
	files map[string]string
	// noTesting makes the window have no Testing view.
	noTesting bool
	// real runs the cargo of the machine, offline, with its home and target folders in the temporary folder (under the tag emetos_cargo).
	real bool
}

// newCargoRig activates the pack over the crate.
func newCargoRig(t *testing.T, o cargoOptions) *cargoRig {
	t.Helper()
	base := t.TempDir()
	r := &rig{t: t, folder: filepath.Join(base, "work"), bin: filepath.Join(base, "bin")}
	copyDir(t, filepath.Join("testdata", "tests"), r.folder)
	for name, text := range o.files {
		if text == "" {
			if err := os.Remove(filepath.Join(r.folder, name)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		writeFile(t, r.folder, name, text, 0o644)
	}
	if err := os.MkdirAll(r.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := o.cases
	if cases == nil {
		cases = append(slices.Clone(listCases), runCases...)
	}
	cr := &cargoRig{rig: r, calls: filepath.Join(base, "calls.log")}
	if !o.noCargo && !o.real {
		writeFile(t, r.bin, "cargo", fakeCargoScript(cases), 0o755)
	}
	fix, err := filepath.Abs(filepath.Join("testdata", "cargo"))
	if err != nil {
		t.Fatal(err)
	}
	// the PATH is the folder of the fake cargo alone, with links to the programs its script uses, so that no program of the machine (gdb,
	// lldb-dap, a real cargo) is found
	for _, tool := range []string{"sed", "cat", "sleep"} {
		if err := os.Symlink(filepath.Join("/usr/bin", tool), filepath.Join(r.bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if o.real {
		// the cargo of the machine, with nothing of the user's: its home and its target folder are temporary, and it does not touch the network
		t.Setenv("CARGO_HOME", filepath.Join(base, "cargo-home"))
		t.Setenv("CARGO_TARGET_DIR", filepath.Join(base, "target"))
		t.Setenv("CARGO_NET_OFFLINE", "true")
	} else {
		t.Setenv("PATH", r.bin)
	}
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("FOLDER", r.folder)
	t.Setenv("CALLS", cr.calls)
	t.Setenv("FIX", fix)
	ext := New()
	ext.(*rust).testDebounce = 5 * time.Millisecond
	if err := extension.CheckManifest(ext.Manifest()); err != nil {
		t.Fatal(err)
	}
	r.api = fakeapi.New(t, ext.Manifest(), extension.ScopeInfo{Name: "tab", Folder: r.folder})
	for k, v := range o.settings {
		r.api.SetSetting(k, v)
	}
	if o.noTesting {
		r.api.SetTestingUnavailable(true)
	}
	if err := ext.Activate(context.Background(), r.api); err != nil {
		t.Fatal(err)
	}
	r.ext = ext.(*rust)
	t.Cleanup(r.ext.wait)
	if r.ext.tests != nil {
		cr.settled() // the tests are found when the pack starts
	}
	return cr
}

// ctl is the controller the pack made.
func (r *cargoRig) ctl() *fakeapi.FakeTestController {
	r.t.Helper()
	c := r.api.TestController(TestControllerID)
	if c == nil {
		r.t.Fatal("the pack made no test controller")
	}
	return c
}

// found waits for the discovery the pack makes when it starts.
func (r *cargoRig) found() {
	r.t.Helper()
	r.settled()
}

// settled waits until nothing is being found.
func (r *cargoRig) settled() {
	r.t.Helper()
	r.api.WaitFor("the discovery of the tests", func() bool { return r.ext.tests.idle() })
}

// called lists what cargo was called with, one entry for each call.
func (r *cargoRig) called() []string {
	data, err := os.ReadFile(r.calls)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// profile is the profile of the kind.
func (r *cargoRig) profile(kind extension.TestRunKind) *fakeapi.FakeTestProfile {
	r.t.Helper()
	for _, p := range r.ctl().Profiles() {
		if p.Kind() == kind {
			return p
		}
	}
	r.t.Fatalf("no profile of kind %d", kind)
	return nil
}

// run runs the include with the Run profile and returns the run the handler made.
func (r *cargoRig) run(include, exclude []*extension.TestItem) *fakeapi.FakeTestRun {
	r.t.Helper()
	before := len(r.ctl().Runs())
	if _, err := r.profile(extension.TestKindRun).Run(context.Background(), include, exclude, false); err != nil {
		r.t.Fatal(err)
	}
	runs := r.ctl().Runs()
	if len(runs) != before+1 {
		r.t.Fatalf("%d runs, want %d", len(runs), before+1)
	}
	return runs[before]
}

// dump writes the items as indented lines: the label, "@line" (from 1) for an item with a place and "!text" for one with an error.
func dump(c *extension.TestItemCollection) string {
	var sb strings.Builder
	var walk func(items []*extension.TestItem, depth int)
	walk = func(items []*extension.TestItem, depth int) {
		for _, it := range items {
			d := it.Data()
			sb.WriteString(strings.Repeat("  ", depth) + d.Label)
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

// find finds an item by the labels from the top, joined by slashes (no label has a slash).
func find(t *testing.T, c *extension.TestItemCollection, path string) *extension.TestItem {
	t.Helper()
	cur := c
	var it *extension.TestItem
	for _, label := range strings.Split(path, "/") {
		it = nil
		for _, x := range cur.All() {
			if x.Data().Label == label {
				it = x
				break
			}
		}
		if it == nil {
			t.Fatalf("no item %q (at %q) in\n%s", path, label, dump(c))
		}
		cur = it.Children()
	}
	return it
}

// labelOf names an item for the tests: its labels from the top joined by slashes.
func labelOf(it *extension.TestItem) string {
	var parts []string
	for x := it; x != nil; x = x.Parent() {
		parts = append(parts, x.Data().Label)
	}
	slices.Reverse(parts)
	return strings.Join(parts, "/")
}

// outcomes are the last state of each item the run reported, by the labels from the top.
func outcomes(run *fakeapi.FakeTestRun) map[string]string {
	got := map[string]string{}
	for _, e := range run.Events() {
		switch e.Kind {
		case fakeapi.RunEnqueued, fakeapi.RunStarted, fakeapi.RunPassed, fakeapi.RunFailed, fakeapi.RunErrored, fakeapi.RunSkipped:
			got[labelOf(e.Item)] = string(e.Kind)
		}
	}
	return got
}

// messagesOf are the messages of the failure of the item.
func messagesOf(run *fakeapi.FakeTestRun, label string) []extension.TestMessage {
	var msgs []extension.TestMessage
	for _, e := range run.Events() {
		if e.Item != nil && labelOf(e.Item) == label && (e.Kind == fakeapi.RunFailed || e.Kind == fakeapi.RunErrored) {
			msgs = e.Messages
		}
	}
	return msgs
}

// outputOf is the text of the output of the run, or only of the item.
func outputOf(run *fakeapi.FakeTestRun, item string) string {
	var sb strings.Builder
	for _, e := range run.Events() {
		if e.Kind == fakeapi.RunOutput && (item == "" || (e.Item != nil && labelOf(e.Item) == item)) {
			sb.WriteString(e.Text)
		}
	}
	return sb.String()
}
