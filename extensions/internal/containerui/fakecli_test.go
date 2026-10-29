package containerui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeCLI is a fake docker program on a temporary PATH: a shell script that records the folder it runs in and its arguments in
// calls.log, one run to a line (a tab before each argument), and answers as the test said for the verb of the command (build, push,
// run, exec, compose_up, compose_down, compose_config, compose_logs, compose_start, compose_version ...): the lines of
// <verb>.out on stdout, those of <verb>.err on stderr, the status in <verb>.code, and a wait of <verb>.sleep seconds (to be stopped).
// It is what the extension runs, through exec.LookPath, as the real docker.
type fakeCLI struct {
	t   *testing.T
	dir string
}

const fakeCLIScript = `#!/bin/sh
d="$FAKE_CLI_DIR"
name="$(basename "$0")"
{ printf 'cwd=%s' "$(pwd)"; [ "$name" = docker-compose ] && printf '\t%s' "(docker-compose)"; for a in "$@"; do printf '\t%s' "$a"; done; printf '\n'; } >> "$d/calls.log"
key="$1"
case "$name" in docker-compose|podman-compose) set -- compose "$@";; esac
if [ "$1" = compose ]; then
  key=compose
  for a in "$@"; do
    case "$a" in up|down|config|logs|start|stop|restart|version) key="compose_$a"; break;; esac
  done
fi
case " $* " in *" --profiles "*) key="${key}_profiles";; esac
[ -f "$d/$key.out" ] && cat "$d/$key.out"
[ -f "$d/$key.err" ] && cat "$d/$key.err" >&2
[ -f "$d/$key.sleep" ] && { echo "sleeping" >> "$d/$key.started"; sleep "$(cat "$d/$key.sleep")"; }
code=0
[ -f "$d/$key.code" ] && code="$(cat "$d/$key.code")"
exit "$code"
`

// alias adds another program that is the same script (docker-compose).
func (f *fakeCLI) alias(name string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, "bin", name), []byte(fakeCLIScript), 0o755); err != nil {
		f.t.Fatal(err)
	}
}

// newFakeCLI makes the fake program "docker" in a temporary folder, puts the folder alone on PATH for the test and answers every verb
// with success and no output.
func newFakeCLI(t *testing.T, name string) *fakeCLI {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte(fakeCLIScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_CLI_DIR", dir)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	return &fakeCLI{t: t, dir: dir}
}

// answer sets what the verb prints and its exit status.
func (f *fakeCLI) answer(verb, out, errText string, code int) {
	f.t.Helper()
	write := func(ext, text string) {
		if text == "" {
			_ = os.Remove(filepath.Join(f.dir, verb+"."+ext))
			return
		}
		if err := os.WriteFile(filepath.Join(f.dir, verb+"."+ext), []byte(text), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	write("out", out)
	write("err", errText)
	if code != 0 {
		write("code", strconv.Itoa(code))
	} else {
		write("code", "")
	}
}

// sleep makes the verb wait for seconds before it ends.
func (f *fakeCLI) sleep(verb string, seconds int) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, verb+".sleep"), []byte(strconv.Itoa(seconds)), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// started tells whether the verb began to wait.
func (f *fakeCLI) started(verb string) bool {
	_, err := os.Stat(filepath.Join(f.dir, verb+".started"))
	return err == nil
}

// call is a run of the program.
type fakeCall struct {
	cwd  string
	args []string
}

// calls lists the runs, the first first.
func (f *fakeCLI) calls() []fakeCall {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dir, "calls.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	var out []fakeCall
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		parts := strings.Split(line, "\t")
		out = append(out, fakeCall{cwd: strings.TrimPrefix(parts[0], "cwd="), args: parts[1:]})
	}
	return out
}

// lines lists the runs as lines of arguments ("build --pull --rm ..."), without the folder.
func (f *fakeCLI) lines() []string {
	var out []string
	for _, c := range f.calls() {
		out = append(out, strings.Join(c.args, " "))
	}
	return out
}

// without lists the runs except the ones that ask whether Compose exists (compose version).
func (f *fakeCLI) without(prefix string) []string {
	var out []string
	for _, l := range f.lines() {
		if !strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}
