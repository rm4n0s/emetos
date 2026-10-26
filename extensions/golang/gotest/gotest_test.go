package gotest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestBuildAndRun(t *testing.T) {
	dir := t.TempDir()
	path, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(path); err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("the built program %s is not an executable file: %v", path, err)
	}
	tests := []struct {
		name string
		env  []string
		args []string
		want string
		fail bool
	}{
		{"the version", []string{EnvVersion + "=go1.2.3"}, []string{"env", "GOVERSION"}, "go1.2.3\n", false},
		{"the default version", nil, []string{"env", "GOVERSION"}, "go1.99.1\n", false},
		{"the whole environment", []string{"GOPATH=/x"}, []string{"env"}, "GOPATH='/x'", false},
		{"an environment that fails", []string{EnvEnvFail + "=no go here"}, []string{"env"}, "no go here", true},
		{"an install", nil, []string{"install", "a.b/c@latest"}, "go: installed a.b/c@latest", false},
		{"an install that fails", []string{EnvFail + "=b/c"}, []string{"install", "a.b/c@latest"}, "module lookup disabled", true},
		{"a run that prints", []string{EnvRunStdout + "=hello from main"}, []string{"run", "."}, "hello from main\n", false},
		{"a run that fails", []string{EnvRunStderr + "=panic: boom", EnvRunExit + "=3"}, []string{"run", "."}, "panic: boom", true},
		{"an unknown command", nil, []string{"build"}, "unsupported", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(path, tt.args...)
			cmd.Env = tt.env
			out, err := cmd.CombinedOutput()
			if (err != nil) != tt.fail {
				t.Errorf("error = %v, want failure %v", err, tt.fail)
			}
			if !strings.Contains(string(out), tt.want) {
				t.Errorf("output %q does not contain %q", out, tt.want)
			}
		})
	}
	// the log records the arguments, the folder and the variables
	log := filepath.Join(t.TempDir(), "log")
	cmd := exec.Command(path, "install", "x@v1")
	cmd.Env = []string{EnvLog + "=" + log, "GOFLAGS=-mod=mod"}
	cmd.Dir = t.TempDir()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(data), `"args":["install","x@v1"]`) || !strings.Contains(string(data), `"GOFLAGS":"-mod=mod"`) {
		t.Errorf("log = %q, %v", data, err)
	}
}

func TestBuildFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plain-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// a plain file where the folder of the output should be
	_, err := Build(file)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute("Build.FakeGoBuildFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestTheFakeGoTestAndVet(t *testing.T) {
	dir := t.TempDir()
	path, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "events.jsonl")
	lines := "{\"Action\":\"pass\",\"Package\":\"p\",\"Test\":\"TestA\"}\n#stderr go: note\n{\"Action\":\"fail\",\"Package\":\"p\"}\n"
	if err := os.WriteFile(fixture, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		env  []string
		args []string
		want string
		fail bool
	}{
		{"go test prints the fixture and fails when an action failed", []string{EnvTestFile + "=" + fixture}, []string{"test", "-json"}, "\"Test\":\"TestA\"}\n", true},
		{"the stderr instruction", []string{EnvTestFile + "=" + fixture}, []string{"test"}, "go: note", true},
		{"a status of the environment", []string{EnvTestFile + "=" + fixture, EnvTestExit + "=0"}, []string{"test"}, "TestA", false},
		{"go test with no fixture passes", nil, []string{"test"}, "", false},
		{"go vet prints to the standard error and fails", []string{EnvVetOut + "=a.go:1:1: bad"}, []string{"vet", "."}, "a.go:1:1: bad", true},
		{"go vet with no output passes", nil, []string{"vet", "."}, "", false},
		{"go vet with a status", []string{EnvVetOut + "=x", EnvVetExit + "=0"}, []string{"vet"}, "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(path, tt.args...)
			cmd.Env = tt.env
			out, err := cmd.CombinedOutput()
			if (err != nil) != tt.fail {
				t.Errorf("error = %v, want failure %v", err, tt.fail)
			}
			if !strings.Contains(string(out), tt.want) {
				t.Errorf("output %q does not contain %q", out, tt.want)
			}
		})
	}
}

func TestTheFakeLintTool(t *testing.T) {
	dir := t.TempDir()
	path, err := BuildLint(dir, "staticcheck")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "staticcheck" {
		t.Errorf("path = %s", path)
	}
	log := filepath.Join(t.TempDir(), "lint.log")
	tests := []struct {
		name string
		env  []string
		want string
		fail bool
	}{
		{"it prints and fails", []string{EnvLintOut + "=a.go:1:1: x (SA1000)"}, "a.go:1:1: x (SA1000)", true},
		{"it prints and passes when told to", []string{EnvLintOut + "=a.go:1:1: x", EnvLintExit + "=0"}, "a.go:1:1: x", false},
		{"it prints nothing and passes", nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(path, "-f", "text", "./...")
			cmd.Env = append([]string{EnvLintLog + "=" + log}, tt.env...)
			out, err := cmd.CombinedOutput()
			if (err != nil) != tt.fail || !strings.Contains(string(out), tt.want) {
				t.Errorf("output %q, error %v", out, err)
			}
		})
	}
	data, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(data), `"tool":"staticcheck"`) || !strings.Contains(string(data), `"args":["-f","text","./..."]`) {
		t.Errorf("log = %q, %v", data, err)
	}
}

func TestBuildLintFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plain-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := BuildLint(file, "staticcheck")
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute("BuildLint.FakeLintBuildFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
