//go:build emetos_gopls

package golang

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
)

// The tests against the real Go and gopls (SPEC §10.17, step 14.1): they only read (go env GOVERSION, go env, the path of gopls)
// and are skipped when the programs are not installed. Nothing here installs a tool.

func TestGoplsStatusItemShowsTheVersionOfTheRealGo(t *testing.T) {
	path, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not installed")
	}
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	out, err := exec.Command(path, "env", "GOVERSION").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := versionLabel(strings.TrimSpace(string(out)))
	r := newRig(t, rigOptions{realGo: true})
	r.goEditor("go")
	r.api.WaitFor("the version", func() bool { return r.item().Text == want })
	if !r.item().Visible {
		t.Error("the item is hidden for a Go file")
	}
	if _, err := lookProgram("gopls", r.folder); err != nil {
		t.Errorf("gopls is not found: %v", err)
	}
}

func TestGoplsShowGoEnvironmentRunsTheRealGoEnv(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	r := newRig(t, rigOptions{realGo: true})
	if err := r.api.Run(ShowEnvironmentCommand, nil); err != nil {
		t.Fatal(err)
	}
	ch := r.api.Channel(EnvironmentChannel)
	r.api.WaitFor("go env", func() bool {
		for _, l := range ch.Lines() {
			if strings.HasPrefix(l, "GOROOT=") {
				return true
			}
		}
		return false
	})
	r.ext.wait()
}

func TestRealRunRunsTheRealGoRun(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	tests := []struct {
		name   string
		main   string
		state  int
		status string
		out    string
	}{
		{"a program that prints", "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hello from the real go\") }\n", runExited, "exit status 0", "hello from the real go"},
		{"a program that exits with a status", "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() { fmt.Fprintln(os.Stderr, \"bye\"); os.Exit(3) }\n", runFailed, "exit status 1", "exit status 3"},
		{"a program that does not build", "package main\n\nfunc main() { undefined() }\n", runFailed, "exit status 1", "undefined: undefined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// go only builds into its temporary folder and the build cache; the toolchain is never downloaded
			r := newRig(t, rigOptions{realGo: true, settings: map[string]any{ToolsEnvVarsSetting: map[string]any{"GOTOOLCHAIN": "local", "GOFLAGS": "-mod=mod"}}})
			if err := os.WriteFile(filepath.Join(r.folder, "main.go"), []byte(tt.main), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := r.api.Run(RunCommand, nil); err != nil {
				t.Fatal(err)
			}
			run := r.ended()
			if run.state != tt.state || run.status != tt.status || !strings.Contains(run.text(), tt.out) {
				t.Errorf("state %d, status %q, output:\n%s", run.state, run.status, run.text())
			}
		})
	}
}

// The real go test, go vet and lint tools (step 14.3), on small packages made in a temporary folder. go builds into its build cache
// only; the toolchain is never downloaded; no tool is installed, and a lint tool that is not installed skips its test.

const realModule = "module example.com/real\n\ngo 1.21\n"

const realSource = `package real

import "fmt"

// Add adds.
func Add(a, b int) int { return a + b }

// Noisy has a result that is not used, which go vet finds.
func Noisy() { fmt.Sprintf("unused") }
`

const realTests = `package real

import "testing"

func TestPasses(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("no")
	}
}

func TestFails(t *testing.T) {
	if got := Add(2, 2); got != 5 {
		t.Errorf("Add(2, 2) = %d, want 5", got)
	}
}

func TestTable(t *testing.T) {
	t.Run("ok", func(t *testing.T) {})
	t.Run("skipped", func(t *testing.T) { t.Skip("later") })
}
`

// realRig makes a rig over the real tools with the module in its folder.
func realRig(t *testing.T, extra map[string]any) *rig {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	env := map[string]any{"GOTOOLCHAIN": "local", "GOFLAGS": "-mod=mod", "GOLANGCI_LINT_CACHE": filepath.Join(t.TempDir(), "lint-cache")}
	settings := map[string]any{ToolsEnvVarsSetting: env}
	for k, v := range extra {
		settings[k] = v
	}
	r := newRig(t, rigOptions{realGo: true, settings: settings})
	r.ext.debounce = 10 * time.Millisecond
	for name, text := range map[string]string{"go.mod": realModule, "real.go": realSource, "real_test.go": realTests} {
		if err := os.WriteFile(filepath.Join(r.folder, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// the rig made a main.go; the module is the package real
	if err := os.Remove(filepath.Join(r.folder, "main.go")); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRealGoTestRunsThePackageAndFillsTheTreeAndProblems(t *testing.T) {
	r := realRig(t, nil)
	path := filepath.Join(r.folder, "real_test.go")
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: fileURI(path), LanguageID: "go"})
	r.api.Pump()
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	job := r.testsEnded()
	if job.state != runFailed || job.status != "exit status 1" {
		t.Errorf("state %d, status %q:\n%s", job.state, job.status, strings.Join(r.ext.tests.outputOf(), "\n"))
	}
	got := show(job.results)
	for _, want := range []string{"example.com/real fail", "TestPasses pass", "TestFails fail", "TestTable pass", "ok pass", "skipped skip"} {
		if !strings.Contains(got, want) {
			t.Errorf("the tree has no %q:\n%s", want, got)
		}
	}
	// the failure is in Problems at the line of t.Errorf
	var lines []string
	for uri, ms := range r.api.Markers(TestsMarkerOwner) {
		for _, m := range ms {
			lines = append(lines, filepath.Base(strings.TrimPrefix(uri, "file://"))+":"+itoa(m.StartLine)+" "+m.Message)
		}
	}
	if !slices.Equal(lines, []string{"real_test.go:13 Add(2, 2) = 4, want 5"}) {
		t.Errorf("markers = %q", lines)
	}
	// a single test, by a lens
	id, arg := lensCommandOf(t, r, fileURI(path), realTests, "run test", "TestPasses")
	if err := r.api.Run(id, arg); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the second run", func() bool { return r.ext.tests.job != job && r.ext.tests.job.state != runRunning })
	if j := r.ext.tests.job; j.state != runExited || j.results.counts().Pass != 1 || r.api.MarkerCount() != 0 {
		t.Errorf("state %d, counts %+v, markers %d", j.state, j.results.counts(), r.api.MarkerCount())
	}
}

func TestRealGoTestShowsABuildFailure(t *testing.T) {
	r := realRig(t, nil)
	if err := os.WriteFile(filepath.Join(r.folder, "real_test.go"), []byte("package real\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) { _ = undefinedName }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: fileURI(filepath.Join(r.folder, "real_test.go")), LanguageID: "go"})
	r.api.Pump()
	if err := r.api.Run(TestPackageCommand, nil); err != nil {
		t.Fatal(err)
	}
	job := r.testsEnded()
	p := job.results.byPath["example.com/real"]
	if p == nil || !p.buildFailed || p.status != statusFail || !strings.Contains(strings.Join(p.output, "\n"), "undefined: undefinedName") {
		t.Errorf("package = %+v", p)
	}
	var found bool
	for uri, ms := range r.api.Markers(TestsMarkerOwner) {
		for _, m := range ms {
			found = found || (strings.HasSuffix(uri, "/real_test.go") && m.StartLine == 5 && strings.Contains(m.Message, "undefined: undefinedName"))
		}
	}
	if !found {
		t.Errorf("markers = %v", r.api.Markers(TestsMarkerOwner))
	}
}

func TestRealGoVetOnSaveFindsTheUnusedResult(t *testing.T) {
	r := realRig(t, map[string]any{LintOnSaveSetting: "off"})
	r.save(filepath.Join(r.folder, "real.go"))
	r.settled()
	got := r.markerList(VetMarkerOwner)
	if len(got) != 1 || !strings.HasPrefix(got[0], "real.go:9:") || !strings.Contains(got[0], "result of fmt.Sprintf call not used") || !strings.HasSuffix(got[0], "(go vet, warning)") {
		t.Errorf("markers = %q", got)
	}
}

func TestRealGolangciLintOnSave(t *testing.T) {
	path, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Skip("golangci-lint is not installed")
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil || !strings.Contains(string(out), "version 2.") {
		t.Skip("the installed golangci-lint is not version 2")
	}
	r := realRig(t, map[string]any{VetOnSaveSetting: "off", LintToolSetting: "golangci-lint-v2"})
	if err := os.WriteFile(filepath.Join(r.folder, "real.go"), []byte("package real\n\nimport \"os\"\n\n// Add adds.\nfunc Add(a, b int) int { return a + b }\n\n// F removes.\nfunc F() {\n\tos.Remove(\"x\")\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.save(filepath.Join(r.folder, "real.go"))
	r.settled()
	got := r.markerList(LintMarkerOwner)
	if len(got) != 1 || !strings.HasPrefix(got[0], "real.go:10:") || !strings.Contains(got[0], "[errcheck]") || !strings.HasSuffix(got[0], "(golangci-lint-v2, warning)") {
		t.Errorf("markers = %q", got)
	}
}

func TestRealStaticcheckOnSave(t *testing.T) {
	if _, err := exec.LookPath("staticcheck"); err != nil {
		t.Skip("staticcheck is not installed")
	}
	r := realRig(t, map[string]any{VetOnSaveSetting: "off"})
	if err := os.WriteFile(filepath.Join(r.folder, "real.go"), []byte("package real\n\n// F has a value that is never used.\nfunc F() int {\n\tx := 1\n\tx = 2\n\treturn 3\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.save(filepath.Join(r.folder, "real.go"))
	r.settled()
	got := r.markerList(LintMarkerOwner)
	if len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), "[SA4006]") {
		t.Errorf("markers = %q", got)
	}
}
