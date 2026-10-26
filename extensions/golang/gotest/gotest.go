// Package gotest builds the fake go program (internal/fakego) for the tests of the Go extension (SPEC §10.17, step 14.1): a
// program that answers `go env` and `go install` from the environment without touching the user's GOPATH, module cache or Go
// environment. The tests put it first in an isolated PATH.
package gotest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/rm4n0s/errors"
)

// The environment variables the fake program reads (see its package comment).
const (
	// EnvLog is the path of a JSONL file the fake appends a record of each run to.
	EnvLog = "FAKEGO_LOG"
	// EnvVersion is the answer to `go env GOVERSION`.
	EnvVersion = "FAKEGO_VERSION"
	// EnvEnvFail makes `go env` fail with this text.
	EnvEnvFail = "FAKEGO_ENVFAIL"
	// EnvDelayMS makes `go env` wait this many milliseconds before it answers.
	EnvDelayMS = "FAKEGO_DELAY_MS"
	// EnvFail and EnvHang are lists of texts, separated by commas, of the packages `go install` fails on and hangs on.
	EnvFail = "FAKEGO_FAIL"
	EnvHang = "FAKEGO_HANG"
	// EnvPidfile is the file a hanging install writes its process id and its child's to.
	EnvPidfile = "FAKEGO_PIDFILE"
	// EnvRunStdout and EnvRunStderr are what `go run` prints; EnvRunGate is a file it waits for after the standard output, EnvRunExit
	// the status it ends with and EnvRunHang makes it hang after the output (EnvPidfile gets the process ids).
	EnvRunStdout = "FAKEGO_RUN_STDOUT"
	EnvRunStderr = "FAKEGO_RUN_STDERR"
	EnvRunGate   = "FAKEGO_RUN_GATE"
	EnvRunExit   = "FAKEGO_RUN_EXIT"
	EnvRunHang   = "FAKEGO_RUN_HANG"
	// EnvTestFile is the file `go test` prints the lines of (the lines of `go test -json`; "#gate", "#hang" and "#stderr text" are
	// instructions), and EnvTestExit the status it ends with. EnvVetOut is what `go vet` prints to the standard error and EnvVetExit
	// its status.
	EnvTestFile = "FAKEGO_TEST_FILE"
	EnvTestExit = "FAKEGO_TEST_EXIT"
	EnvVetOut   = "FAKEGO_VET_OUT"
	EnvVetExit  = "FAKEGO_VET_EXIT"
)

// The environment variables the fake lint tool reads (see its package comment).
const (
	// EnvLintLog is the path of a JSONL file the fake appends a record of each run to (the tool's name, arguments and working
	// folder), EnvLintOut what it prints to the standard output and EnvLintExit the status it ends with (1 when it printed
	// something, else 0). EnvLintGate is a file it waits for before it prints, and EnvLintHang makes it hang after it started.
	EnvLintLog  = "FAKELINT_LOG"
	EnvLintOut  = "FAKELINT_OUT"
	EnvLintExit = "FAKELINT_EXIT"
	EnvLintGate = "FAKELINT_GATE"
	EnvLintHang = "FAKELINT_HANG"
)

// Build builds the fake program into dir, as the file "go", and returns its path. It runs the real Go tool (once per test
// run, before the tests isolate their PATH) and fails with the tag FakeGoBuildFailed, with the output of the tool in the
// metadata.
func Build(dir string) (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("FakeGoBuildFailed", "cannot find the source of gotest")
	}
	out := filepath.Join(dir, "go")
	cmd := exec.Command("go", "build", "-o", out, "./internal/fakego")
	cmd.Dir = filepath.Dir(file)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := cmd.CombinedOutput(); err != nil {
		return "", errors.NewErr("FakeGoBuildFailed", err).SetMetadata("output", string(msg))
	}
	return out, nil
}

// BuildLint builds the fake lint tool (internal/fakelint) into dir under the name of the tool it stands for (staticcheck,
// golangci-lint, revive), and returns its path. It fails with the tag FakeLintBuildFailed, with the output of the Go tool in the
// metadata.
func BuildLint(dir, name string) (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("FakeLintBuildFailed", "cannot find the source of gotest")
	}
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", out, "./internal/fakelint")
	cmd.Dir = filepath.Dir(file)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := cmd.CombinedOutput(); err != nil {
		return "", errors.NewErr("FakeLintBuildFailed", err).SetMetadata("output", string(msg))
	}
	return out, nil
}
