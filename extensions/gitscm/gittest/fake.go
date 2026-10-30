package gittest

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// FakeEnv is the variable that makes the test binary act as git: its value is the script of the fake (see FakeGit).
const FakeEnv = "EMETOS_FAKE_GIT"

// FakeCall is one line of the file a fake git records (the directive "record"): the call as the fake saw it.
type FakeCall struct {
	// Args are the arguments, without the program name.
	Args []string `json:"args"`
	// Dir is the working directory.
	Dir string `json:"dir"`
	// Env is the whole environment of the call.
	Env []string `json:"env"`
	// PID is the process id of the fake.
	PID int `json:"pid"`
}

// MaybeRunFakeGit must be the first statement of the TestMain of a package that uses FakeGit. When the variable
// EMETOS_FAKE_GIT is set the test binary was started as the fake git: it follows the script, exits and never returns;
// otherwise it returns at once.
func MaybeRunFakeGit() {
	script := os.Getenv(FakeEnv)
	if script == "" {
		return
	}
	os.Exit(runFake(script, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// FakeGit makes a folder with a program called git that is the test binary itself, and returns its path and the
// variable ("EMETOS_FAKE_GIT=<script>") that the test gives the process it starts with it. The test binary must call
// MaybeRunFakeGit first in its TestMain. The script has one directive a line, run in this order for every call:
//
//	record <file>      append a FakeCall (JSON) to the file
//	version <x.y.z>    answer "git --version" with "git version x.y.z" and stop
//	stdin              read the whole standard input; print "stdin <bytes>" first on standard output
//	child <file>       start a child process that hangs, in the same process group, and write its pid to the file
//	sleep <duration>   wait
//	dump               print the call (a FakeCall) as JSON on standard output
//	stdout "<text>"    print the Go-quoted text on standard output
//	stderr "<text>"    print the Go-quoted text on standard error
//	huge <bytes>       print that many bytes of "x" on standard output
//	ignoreterm         ignore SIGTERM
//	hang               wait until killed
//	exit <n>           exit with that status (the default is 0)
//
// A missing git is simply a path that does not exist (see MissingGit), an old one the directive "version 2.29.0", a slow one
// "sleep", a failing one "stderr" and "exit 128", a huge output "huge", and one that hangs until cancelled "hang".
func FakeGit(t TB, script string) (path string, env string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("gittest: %v", err)
	}
	dir := t.TempDir()
	path = filepath.Join(dir, "git")
	if err := os.Symlink(exe, path); err != nil {
		t.Fatalf("gittest: %v", err)
	}
	return path, FakeEnv + "=" + script
}

// MissingGit returns the path of a git program that does not exist.
func MissingGit(t TB) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "no-such-git")
}

// runFake follows the script and returns the exit status.
func runFake(script string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	type directive struct{ name, rest string }
	var ds []directive
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, rest, _ := strings.Cut(line, " ")
		ds = append(ds, directive{name, strings.TrimSpace(rest)})
	}
	dir, _ := os.Getwd()
	call := FakeCall{Args: args, Dir: dir, Env: os.Environ(), PID: os.Getpid()}
	for _, d := range ds {
		if d.name == "record" {
			f, err := os.OpenFile(d.rest, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				_, _ = io.WriteString(stderr, "fake git: "+err.Error()+"\n")
				return 99
			}
			line, _ := json.Marshal(call)
			_, _ = f.Write(append(line, '\n'))
			_ = f.Close()
		}
	}
	for _, d := range ds {
		if d.name == "version" && len(args) == 1 && args[0] == "--version" {
			_, _ = io.WriteString(stdout, "git version "+d.rest+"\n")
			return 0
		}
	}
	exit := 0
	for _, d := range ds {
		switch d.name {
		case "stdin":
			n, _ := io.Copy(io.Discard, bufio.NewReader(stdin))
			_, _ = io.WriteString(stdout, "stdin "+strconv.FormatInt(n, 10)+"\n")
		case "child":
			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(), FakeEnv+"=hang")
			if err := cmd.Start(); err != nil {
				_, _ = io.WriteString(stderr, "fake git: "+err.Error()+"\n")
				return 99
			}
			if err := os.WriteFile(d.rest, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
				_, _ = io.WriteString(stderr, "fake git: "+err.Error()+"\n")
				return 99
			}
		case "sleep":
			dur, err := time.ParseDuration(d.rest)
			if err != nil {
				_, _ = io.WriteString(stderr, "fake git: "+err.Error()+"\n")
				return 99
			}
			time.Sleep(dur)
		case "dump":
			line, _ := json.Marshal(call)
			_, _ = stdout.Write(append(line, '\n'))
		case "stdout":
			text, err := strconv.Unquote(d.rest)
			if err != nil {
				text = d.rest
			}
			_, _ = io.WriteString(stdout, text)
		case "stderr":
			text, err := strconv.Unquote(d.rest)
			if err != nil {
				text = d.rest
			}
			_, _ = io.WriteString(stderr, text)
		case "huge":
			n, _ := strconv.ParseInt(d.rest, 10, 64)
			chunk := []byte(strings.Repeat("x", 64*1024))
			for n > 0 {
				k := int64(len(chunk))
				if n < k {
					k = n
				}
				if _, err := stdout.Write(chunk[:k]); err != nil {
					return 141
				}
				n -= k
			}
		case "ignoreterm":
			signal.Ignore(syscall.SIGTERM)
		case "hang":
			time.Sleep(24 * time.Hour)
		case "exit":
			exit, _ = strconv.Atoi(d.rest)
		}
	}
	return exit
}
