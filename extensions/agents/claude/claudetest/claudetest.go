// Package claudetest builds and drives the fake claude program (internal/fakeclaude) for the tests of the Agents extension
// and for its proofs (SPEC §10.16, step 13.1): a program that speaks the stream-json protocol from a JSONL script.
package claudetest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/rm4n0s/errors"
)

// The environment variables the fake program reads (see its package comment).
const (
	// EnvScript is the path of the JSONL script the fake replays.
	EnvScript = "FAKECLAUDE_SCRIPT"
	// EnvLog is the path of a JSONL file the fake appends its command line, environment and input to.
	EnvLog = "FAKECLAUDE_LOG"
	// EnvPidfile is the file the directive spawn_child writes the PID of its child to.
	EnvPidfile = "FAKECLAUDE_PIDFILE"
	// EnvAuth selects what `auth status` does: in, out, garbage, crash or hang.
	EnvAuth = "FAKECLAUDE_AUTH"
	// EnvAuthFile names a file that `auth login` writes "in" to on success, and that makes `auth status` say it is logged in.
	EnvAuthFile = "FAKECLAUDE_AUTHFILE"
)

// Build builds the fake program into dir, as the file "claude", and returns its path. It runs the Go tool and fails with
// the tag FakeBuildFailed, with the output of the tool in the metadata.
func Build(dir string) (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("FakeBuildFailed", "cannot find the source of claudetest")
	}
	out := filepath.Join(dir, "claude")
	cmd := exec.Command("go", "build", "-o", out, "./internal/fakeclaude")
	cmd.Dir = filepath.Dir(filepath.Dir(file))
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := cmd.CombinedOutput(); err != nil {
		return "", errors.NewErr("FakeBuildFailed", err).SetMetadata("output", string(msg))
	}
	return out, nil
}
