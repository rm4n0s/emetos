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
