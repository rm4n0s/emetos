//go:build emetos_gopls

package golang

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
