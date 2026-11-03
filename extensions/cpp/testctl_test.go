package cpp

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The tree of the tests and the runs of the controller (step 26.9) over a fake ctest: a script in the PATH that answers the commands the
// pack runs with the recorded output of the real ctest (testdata/ctest, recorded from the project testdata/tests with CMake 4.3.0) and
// writes down the arguments it was called with. The real ctest runs under the tag emetos_ctest (testreal_test.go).

// fakeCase is an answer of the fake ctest: the arguments (a shell pattern; the --output-junit file is left out of them), the recorded
// output, the recorded JUnit file, the exit status and the seconds it takes.
type fakeCase struct {
	args, stdout, junit string
	code                int
	sleep               int
}

// ctestRig is the pack activated over a copy of the project testdata/tests with a build folder and a fake ctest.
type ctestRig struct {
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

// listCase and runCases are the answers of the fake ctest.
var (
	listCase = fakeCase{args: "--show-only=json-v1", stdout: "show.json"}
	runCases = []fakeCase{
		{args: "--output-junit", stdout: "run_all.txt", junit: "junit_all.xml", code: 8},
		{args: `--output-junit -R ^(adds|divides)$`, stdout: "run_two.txt", junit: "junit_two.xml", code: 8},
		{args: `--output-junit -R ^(odd\[1\]\+x\(2\))$`, stdout: "run_odd.txt", junit: "junit_odd.xml"},
	}
)

// fakeCTestScript is the program: it answers by the arguments, logs them and writes the JUnit file it was asked for.
func fakeCTestScript(cases []fakeCase) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\njunit=\"\"; key=\"\"\n")
	sb.WriteString("while [ $# -gt 0 ]; do\n  if [ \"$1\" = \"--output-junit\" ]; then junit=\"$2\"; key=\"$key --output-junit\"; shift 2; continue; fi\n  key=\"$key $1\"; shift\ndone\n")
	sb.WriteString("key=\"${key# }\"\necho \"$key\" >> \"$CALLS\"\n")
	sb.WriteString("sub() { sed -e \"s#{{dir}}#$FOLDER#g\" \"$FIX/$1\"; }\ncase \"$key\" in\n")
	for _, c := range cases {
		sb.WriteString("'" + c.args + "') ")
		if c.sleep > 0 {
			sb.WriteString("echo $$ > \"$FOLDER/../pid\"; sleep " + strconv.Itoa(c.sleep) + "; ")
		}
		if c.stdout != "" {
			sb.WriteString("sub " + c.stdout + "; ")
		}
		if c.junit != "" {
			sb.WriteString("sub " + c.junit + " > \"$junit\"; ")
		}
		sb.WriteString("exit " + strconv.Itoa(c.code) + ";;\n")
	}
	sb.WriteString("*) echo \"unexpected ctest: $key\" >&2; exit 2;;\nesac\n")
	return sb.String()
}

// ctestOptions say what a test rig has.
type ctestOptions struct {
	// cases are the answers of the fake ctest; nil is the list and runCases. noCTest leaves the PATH with no ctest.
	cases   []fakeCase
	noCTest bool
	// noBuild leaves the build folder out.
	noBuild  bool
	settings map[string]any
	// files are written over the copy of the project (a name to text; "" removes the file); fixtures over the recorded outputs.
	files    map[string]string
	fixtures map[string]string
	// noTesting makes the window have no Testing view.
	noTesting bool
	// real runs the ctest of the machine (under the tag emetos_ctest) over a project that was configured and built.
	real bool
}

// newCTestRig activates the pack over the project.
func newCTestRig(t *testing.T, o ctestOptions) *ctestRig {
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
	if !o.noBuild && !o.real {
		writeFile(t, r.folder, "build/CTestTestfile.cmake", "# written by CMake\n", 0o644)
	}
	if err := os.MkdirAll(r.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := o.cases
	if cases == nil {
		cases = append([]fakeCase{listCase}, runCases...)
	}
	cr := &ctestRig{rig: r, calls: filepath.Join(base, "calls.log")}
	fix := filepath.Join(base, "fixtures")
	copyDir(t, filepath.Join("testdata", "ctest"), fix)
	for name, text := range o.fixtures {
		writeFile(t, fix, name, text, 0o644)
	}
	if !o.noCTest && !o.real {
		writeFile(t, r.bin, "ctest", fakeCTestScript(cases), 0o755)
	}
	// the PATH is the folder of the fake ctest alone, with links to the programs its script uses, so that no program of the machine (gdb,
	// lldb-dap, a real ctest) is found
	for _, tool := range []string{"sed", "cat", "sleep"} {
		if err := os.Symlink(filepath.Join("/usr/bin", tool), filepath.Join(r.bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if !o.real {
		t.Setenv("PATH", r.bin)
	}
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("FOLDER", r.folder)
	t.Setenv("CALLS", cr.calls)
	t.Setenv("FIX", fix)
	ext := New()
	ext.(*cpp).testDebounce = 5 * time.Millisecond
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
	if o.real {
		cr.configureAndBuild()
	}
	if err := ext.Activate(context.Background(), r.api); err != nil {
		t.Fatal(err)
	}
	r.ext = ext.(*cpp)
	t.Cleanup(r.ext.wait)
	if r.ext.tests != nil {
		cr.settled() // the tests are found when the pack starts
	}
	return cr
}

// configureAndBuild configures and builds the project with the machine's cmake; the real tests use it.
func (r *ctestRig) configureAndBuild() {
	r.t.Helper()
	r.buildProject(r.folder)
}

// ctl is the controller the pack made.
func (r *ctestRig) ctl() *fakeapi.FakeTestController {
	r.t.Helper()
	c := r.api.TestController(TestControllerID)
	if c == nil {
		r.t.Fatal("the pack made no test controller")
	}
	return c
}

// settled waits until nothing is being found.
func (r *ctestRig) settled() {
	r.t.Helper()
	r.api.WaitFor("the discovery of the tests", func() bool { return r.ext.tests.idle() })
}

// called lists what ctest was called with, one entry for each call.
func (r *ctestRig) called() []string {
	data, err := os.ReadFile(r.calls)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// profile is the profile of the kind.
func (r *ctestRig) profile(kind extension.TestRunKind) *fakeapi.FakeTestProfile {
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
func (r *ctestRig) run(include, exclude []*extension.TestItem) *fakeapi.FakeTestRun {
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

// items finds the items by their labels from the top.
func (r *ctestRig) items(paths ...string) []*extension.TestItem {
	r.t.Helper()
	var out []*extension.TestItem
	for _, p := range paths {
		out = append(out, find(r.t, r.ctl().Items(), p))
	}
	return out
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

// buildProject configures and builds the project in dir with the machine's cmake into dir/build (the real tests use it).
func (r *ctestRig) buildProject(dir string) {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, args := range [][]string{{"-S", dir, "-B", filepath.Join(dir, "build"), "-DCMAKE_BUILD_TYPE=Debug"}, {"--build", filepath.Join(dir, "build")}} {
		cmd := exec.CommandContext(ctx, "cmake", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			r.t.Fatalf("cmake %v: %v\n%s", args, err, out)
		}
	}
}
