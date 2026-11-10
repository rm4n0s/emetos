package git

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/errors"
)

// fakeRunner makes a Runner whose git is the fake with the script. base is the environment the commands start from.
func fakeRunner(t *testing.T, script string, mod func(*Config)) *Runner {
	t.Helper()
	path, fakeEnv := gittest.FakeGit(t, script)
	cfg := Config{Path: path, Env: []string{"PATH=" + os.Getenv("PATH"), fakeEnv}, KillGrace: 500 * time.Millisecond}
	if mod != nil {
		mod(&cfg)
	}
	return NewRunner(cfg)
}

// dumpOf runs the command against the fake that dumps its call, and returns the call.
func dumpOf(t *testing.T, base []string, cfg func(*Config), c Command) gittest.FakeCall {
	t.Helper()
	r := fakeRunner(t, "dump", func(cf *Config) {
		cf.Env = append(cf.Env, base...)
		if cfg != nil {
			cfg(cf)
		}
	})
	res, err := r.Run(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	var call gittest.FakeCall
	if err := json.Unmarshal(res.Stdout, &call); err != nil {
		t.Fatalf("%v: %s", err, res.Stdout)
	}
	return call
}

func envOf(call gittest.FakeCall) map[string]string {
	m := map[string]string{}
	for _, kv := range call.Env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

// The environment of a command (SPEC §10.30 "Environment"): a table of what is set and what is removed.
func TestTheEnvironmentOfACommand(t *testing.T) {
	userEnv := []string{"LC_ALL=de_DE.UTF-8", "LC_CTYPE=de_DE.UTF-8", "LANG=de_DE.UTF-8", "LANGUAGE=de", "LC_MESSAGES=de_DE.UTF-8",
		"GIT_TERMINAL_PROMPT=1", "GIT_PAGER=less", "GIT_OPTIONAL_LOCKS=1", "EMETOS_GIT_COMMAND=wrong"}
	extra := []string{"GIT_ASKPASS=/run/emetos/askpass.sh", "SSH_ASKPASS=/run/emetos/ssh-askpass.sh", "SSH_ASKPASS_REQUIRE=force", "GIT_EDITOR=/run/emetos/editor.sh", "EMETOS_GIT_TOKEN=t0k"}
	for _, tc := range []struct {
		name    string
		command Command
		set     map[string]string
		unset   []string
	}{
		{"a foreground command", Command{Args: []string{"commit", "-m", "x"}},
			map[string]string{"LC_CTYPE": "de_DE.UTF-8", "LANG": "de_DE.UTF-8", "LANGUAGE": "en", "LC_MESSAGES": "C", "GIT_TERMINAL_PROMPT": "0", "GIT_PAGER": "cat",
				"EMETOS_GIT_COMMAND": "commit", "GIT_OPTIONAL_LOCKS": "1", "GIT_ASKPASS": "/run/emetos/askpass.sh", "SSH_ASKPASS_REQUIRE": "force", "EMETOS_GIT_TOKEN": "t0k"},
			[]string{"LC_ALL"}},
		{"a background command", Command{Args: []string{"status", "-z"}, Background: true},
			map[string]string{"GIT_OPTIONAL_LOCKS": "0", "EMETOS_GIT_COMMAND": "status", "LC_CTYPE": "de_DE.UTF-8", "LANGUAGE": "en"},
			[]string{"LC_ALL"}},
		{"the command is the one after the -c options", Command{Args: []string{"-c", "user.name=x", "log", "-n1"}},
			map[string]string{"EMETOS_GIT_COMMAND": "log"}, nil},
		{"the variables of the command come last", Command{Args: []string{"fetch"}, Env: []string{"GIT_EDITOR=true", "GIT_ASKPASS=/other"}},
			map[string]string{"GIT_EDITOR": "true", "GIT_ASKPASS": "/other", "SSH_ASKPASS": "/run/emetos/ssh-askpass.sh"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := dumpOf(t, userEnv, func(c *Config) { c.Extra = extra }, tc.command)
			env := envOf(call)
			for k, want := range tc.set {
				if env[k] != want {
					t.Errorf("%s = %q, want %q", k, env[k], want)
				}
			}
			for _, k := range tc.unset {
				if v, ok := env[k]; ok {
					t.Errorf("%s = %q is set, want it removed", k, v)
				}
			}
		})
	}

	t.Run("a foreground command has no GIT_OPTIONAL_LOCKS of its own", func(t *testing.T) {
		call := dumpOf(t, nil, nil, Command{Args: []string{"fetch"}})
		if v, ok := envOf(call)["GIT_OPTIONAL_LOCKS"]; ok {
			t.Errorf("GIT_OPTIONAL_LOCKS = %q", v)
		}
	})
}

// The argument list of a command: the background flags only on background commands, --no-ext-diff and --no-textconv only
// where the command has them, UTF-8 log encoding, the paths after "--".
func TestTheArgumentsOfACommand(t *testing.T) {
	bg := []string{"-c", "core.fsmonitor=false", "-c", "core.pager=cat"}
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	for _, tc := range []struct {
		name string
		c    Command
		want []string
	}{
		{"status in the foreground", Command{Args: []string{"status", "-z"}}, []string{"status", "-z"}},
		{"status in the background", Command{Args: []string{"status", "-z"}, Background: true}, cat(bg, []string{"status", "-z"})},
		{"diff in the background", Command{Args: []string{"diff", "--name-status"}, Background: true}, cat(bg, []string{"diff", "--no-ext-diff", "--no-textconv", "--name-status"})},
		{"diff in the foreground", Command{Args: []string{"diff", "--name-status"}}, []string{"diff", "--name-status"}},
		{"log in the background", Command{Args: []string{"log", "-z"}, Background: true},
			cat(bg, []string{"-c", "i18n.logOutputEncoding=UTF-8", "log", "--no-ext-diff", "--no-textconv", "-z"})},
		{"log in the foreground", Command{Args: []string{"log", "-z"}}, []string{"-c", "i18n.logOutputEncoding=UTF-8", "log", "-z"}},
		{"show in the background", Command{Args: []string{"show", "-s", "HEAD"}, Background: true},
			cat(bg, []string{"-c", "i18n.logOutputEncoding=UTF-8", "show", "--no-ext-diff", "--no-textconv", "-s", "HEAD"})},
		{"blame in the background", Command{Args: []string{"blame", "--incremental"}, Background: true},
			cat(bg, []string{"-c", "i18n.logOutputEncoding=UTF-8", "blame", "--no-ext-diff", "--no-textconv", "--incremental"})},
		{"diff-tree in the background", Command{Args: []string{"diff-tree", "-r"}, Background: true}, cat(bg, []string{"diff-tree", "--no-ext-diff", "--no-textconv", "-r"})},
		{"cat-file takes neither option", Command{Args: []string{"cat-file", "blob", "HEAD:a"}, Background: true}, cat(bg, []string{"cat-file", "blob", "HEAD:a"})},
		{"the user's -c options come before the command", Command{Args: []string{"-c", "user.name=x", "diff"}, Background: true},
			cat(bg, []string{"-c", "user.name=x", "diff", "--no-ext-diff", "--no-textconv"})},
		{"paths go after --", Command{Args: []string{"add"}, Paths: []string{"a.txt", "-weird name", "dir/b"}}, []string{"add", "--", "a.txt", "-weird name", "dir/b"}},
		{"200 paths stay on the command line", Command{Args: []string{"add"}, Paths: numbered(200)}, append([]string{"add", "--"}, numbered(200)...)},
		{"diff with 201 paths cannot read a file", Command{Args: []string{"diff"}, Paths: numbered(201)}, append([]string{"diff", "--"}, numbered(201)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name, idx := subcommand(tc.c.Args)
			got, input := arguments(name, idx, tc.c)
			if !reflect.DeepEqual(got, tc.want) || input != nil {
				t.Errorf("arguments = %q (input %d bytes)\nwant        %q", got, len(input), tc.want)
			}
		})
	}

	t.Run("201 paths of add are read from the standard input, NUL separated", func(t *testing.T) {
		c := Command{Args: []string{"add", "-v"}, Paths: numbered(201)}
		name, idx := subcommand(c.Args)
		got, input := arguments(name, idx, c)
		if !reflect.DeepEqual(got, []string{"add", "-v", "--pathspec-from-file=-", "--pathspec-file-nul"}) {
			t.Errorf("arguments = %q", got)
		}
		if want := strings.Join(numbered(201), "\x00") + "\x00"; string(input) != want {
			t.Errorf("input = %.60q...", input)
		}
	})
}

func numbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "dir/file-" + strconv.Itoa(i) + ".txt"
	}
	return out
}

func TestSubcommand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		name string
		idx  int
	}{
		{[]string{"status"}, "status", 0},
		{[]string{"-c", "a=b", "-c", "c=d", "log", "-n1"}, "log", 4},
		{[]string{"-C", "/x", "diff"}, "diff", 2},
		{[]string{"--no-pager", "show"}, "show", 1},
		{[]string{"--version"}, "", -1},
		{nil, "", -1},
	} {
		if name, idx := subcommand(tc.args); name != tc.name || idx != tc.idx {
			t.Errorf("subcommand(%q) = %q, %d; want %q, %d", tc.args, name, idx, tc.name, tc.idx)
		}
	}
}

// 5,000 paths go through the pathspec file: with the fake (the arguments and the bytes on the input) and with the real git.
func TestThePathspecFileForFiveThousandPaths(t *testing.T) {
	paths := numbered(5000)
	r := fakeRunner(t, "stdin\ndump", nil)
	res, err := r.Run(context.Background(), Command{Args: []string{"add"}, Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	first, rest, _ := strings.Cut(string(res.Stdout), "\n")
	want := 0
	for _, p := range paths {
		want += len(p) + 1
	}
	if first != "stdin "+strconv.Itoa(want) {
		t.Errorf("input = %q, want %d bytes", first, want)
	}
	var call gittest.FakeCall
	if err := json.Unmarshal([]byte(rest), &call); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(call.Args, []string{"add", "--pathspec-from-file=-", "--pathspec-file-nul"}) {
		t.Errorf("args = %q", call.Args)
	}

	t.Run("with the real git", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("repo")
		for i, p := range paths {
			repo.Write(p, strconv.Itoa(i)+"\n")
		}
		runner := NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: e.Vars()})
		if _, err := runner.Run(context.Background(), Command{Dir: repo.Dir, Args: []string{"add"}, Paths: paths}); err != nil {
			t.Fatal(err)
		}
		if n := len(repo.Files()); n != 5000 {
			t.Errorf("%d files were added, want 5000", n)
		}
	})

	t.Run("the input of the command and the paths cannot both be on the standard input", func(t *testing.T) {
		_, err := r.Run(context.Background(), Command{Args: []string{"add"}, Paths: paths, Stdin: strings.NewReader("x")})
		wantRoute(t, err, "Runner.Run->Runner.execute.GitStdinConflict")
	})
}

func TestRunReturnsTheOutputAndTheResult(t *testing.T) {
	r := fakeRunner(t, "stdout \"out\\n\"\nstderr \"warning: x\\n\"", nil)
	res, err := r.Run(context.Background(), Command{Args: []string{"status"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "out\n" || res.Stderr != "warning: x\n" || res.ExitCode != 0 {
		t.Errorf("result = %+v", res)
	}
}

func TestAnExitStatusThatTheCommandAllows(t *testing.T) {
	r := fakeRunner(t, "exit 1", nil)
	if res, err := r.Run(context.Background(), Command{Args: []string{"merge-base", "a", "b"}, AllowExit: []int{1}}); err != nil || res.ExitCode != 1 {
		t.Errorf("allowed: %+v, %v", res, err)
	}
	_, err := r.Run(context.Background(), Command{Args: []string{"merge-base", "a", "b"}})
	appErr := routeOf(t, err, "Runner.Run->Runner.execute->classify.GitCommandFailed")
	if appErr != nil && appErr.Metadata["exitCode"] != 1 {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
}

func TestAFailureCarriesTheArgumentsTheStatusAndTheLastErrorOutput(t *testing.T) {
	long := strings.Repeat("progress line that is long enough to fill the buffer\\n", 100)
	r := fakeRunner(t, "stderr \""+long+"fatal: unable to access 'https://user:secret@example.com/x.git/': boom\\n\"\nexit 128", nil)
	_, err := r.Run(context.Background(), Command{Args: []string{"fetch", "https://user:secret@example.com/x.git"}})
	appErr := routeOf(t, err, "Runner.Run->Runner.execute->classify.CantAccessRemote")
	if appErr == nil {
		return
	}
	if appErr.Metadata["exitCode"] != 128 {
		t.Errorf("exitCode = %v", appErr.Metadata["exitCode"])
	}
	stderr, _ := appErr.Metadata["stderr"].(string)
	if len(stderr) > StderrKept || !strings.HasSuffix(strings.TrimSpace(stderr), "boom") || strings.Contains(stderr, "secret") {
		t.Errorf("stderr = %d bytes %q", len(stderr), stderr)
	}
	args, _ := appErr.Metadata["args"].([]string)
	if strings.Contains(strings.Join(args, " "), "secret") || !strings.Contains(strings.Join(args, " "), "https://***@example.com/x.git") {
		t.Errorf("args = %q", args)
	}
	if strings.Contains(appErr.Message, "secret") {
		t.Errorf("message = %q", appErr.Message)
	}
}

func TestStashPushWithNothingToStash(t *testing.T) {
	r := fakeRunner(t, "stdout \"No local changes to save\\n\"", nil)
	_, err := r.Run(context.Background(), Command{Args: []string{"stash", "push", "-m", "x"}})
	wantRoute(t, err, "Runner.Run->Runner.execute->classifySuccess.NoLocalChanges")
	if _, err := r.Run(context.Background(), Command{Args: []string{"stash", "list"}}); err != nil {
		// The same output for another command is not an error.
		t.Logf("stash list: %v", err)
	}
}

func TestStartFailures(t *testing.T) {
	t.Run("git is not there", func(t *testing.T) {
		r := NewRunner(Config{Path: gittest.MissingGit(t)})
		_, err := r.Run(context.Background(), Command{Args: []string{"status"}})
		appErr := routeOf(t, err, "Runner.Run->Runner.execute.GitNotFound")
		if appErr != nil && appErr.Metadata["path"] != r.Path() {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
	})
	t.Run("the working directory is not there", func(t *testing.T) {
		r := fakeRunner(t, "", nil)
		_, err := r.Run(context.Background(), Command{Dir: filepath.Join(t.TempDir(), "gone"), Args: []string{"status"}})
		wantRoute(t, err, "Runner.Run->Runner.execute.GitDirMissing")
	})
	t.Run("the program cannot be started", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "git")
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o644); err != nil { // not executable
			t.Fatal(err)
		}
		r := NewRunner(Config{Path: path})
		_, err := r.Run(context.Background(), Command{Args: []string{"status"}})
		wantRoute(t, err, "Runner.Run->Runner.execute.GitStartFailed")
	})
}

// A command that was cancelled is ended by SIGTERM to its process group: the child that the fake git started dies with it.
func TestCancellationKillsTheWholeProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	r := fakeRunner(t, "child "+pidFile+"\nhang", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		res Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := r.Run(ctx, Command{Args: []string{"fetch"}})
		done <- outcome{res, err}
	}()
	childPID := waitForPID(t, pidFile)
	if !alive(childPID) {
		t.Fatalf("the child %d is not running", childPID)
	}
	cancel()
	select {
	case o := <-done:
		wantRoute(t, o.err, "Runner.Run->Runner.execute.GitCancelled")
		if !errors.Is(o.err, context.Canceled) {
			t.Errorf("the cancellation is not the cause: %v", o.err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not return after the cancellation")
	}
	waitForDeath(t, childPID)
}

// A command that ignores SIGTERM is ended by SIGKILL after the grace period, and so is everything in its group.
func TestACommandThatIgnoresSIGTERMGetsSIGKILL(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	r := fakeRunner(t, "ignoreterm\nchild "+pidFile+"\nhang", func(c *Config) { c.KillGrace = 400 * time.Millisecond })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.Run(ctx, Command{Args: []string{"push"}})
		done <- err
	}()
	childPID := waitForPID(t, pidFile)
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		wantRoute(t, err, "Runner.Run->Runner.execute.GitCancelled")
		if took := time.Since(start); took < 300*time.Millisecond {
			t.Errorf("Run returned after %v, before the grace period of 400ms", took)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not return after the cancellation")
	}
	waitForDeath(t, childPID)
}

func waitForPID(t *testing.T, file string) int {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(file); err == nil && len(data) > 0 {
			if pid, err := strconv.Atoi(string(data)); err == nil {
				return pid
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the fake git did not start its child")
	return 0
}

func waitForDeath(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("the process %d is still running", pid)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// alive says whether the process runs; a zombie nobody reaped yet does not count.
func alive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	return !isZombie(pid)
}

func TestTheOutputIsBounded(t *testing.T) {
	if got := NewRunner(Config{}).cfg.MaxOutput; got != 64<<20 {
		t.Errorf("the default bound is %d, want 64 MiB", got)
	}
	r := fakeRunner(t, "huge 5000000", func(c *Config) { c.MaxOutput = 100000 })
	res, err := r.Run(context.Background(), Command{Args: []string{"log"}})
	appErr := routeOf(t, err, "Runner.Run->Runner.execute.GitOutputTooLarge")
	if appErr != nil && appErr.Metadata["limit"] != int64(100000) {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	if len(res.Stdout) > 100000 {
		t.Errorf("%d bytes were kept", len(res.Stdout))
	}
	// Output under the bound is kept whole.
	r = fakeRunner(t, "huge 90000", func(c *Config) { c.MaxOutput = 100000 })
	if res, err := r.Run(context.Background(), Command{Args: []string{"log"}}); err != nil || len(res.Stdout) != 90000 {
		t.Errorf("%d bytes, %v", len(res.Stdout), err)
	}
}

func TestRunStream(t *testing.T) {
	t.Run("a consumer that reads everything", func(t *testing.T) {
		r := fakeRunner(t, "huge 1000000", nil)
		var n int64
		res, err := r.RunStream(context.Background(), Command{Args: []string{"log"}}, func(stdout io.Reader) error {
			var err error
			n, err = io.Copy(io.Discard, stdout)
			return err
		})
		if err != nil || n != 1000000 || len(res.Stdout) != 0 {
			t.Errorf("read %d bytes, %v, kept %d", n, err, len(res.Stdout))
		}
	})
	t.Run("output larger than the bound of Run is no problem for a stream", func(t *testing.T) {
		r := fakeRunner(t, "huge 3000000", func(c *Config) { c.MaxOutput = 1000 })
		var n int64
		if _, err := r.RunStream(context.Background(), Command{Args: []string{"log"}}, func(stdout io.Reader) error {
			var err error
			n, err = io.Copy(io.Discard, stdout)
			return err
		}); err != nil || n != 3000000 {
			t.Errorf("read %d bytes, %v", n, err)
		}
	})
	t.Run("a consumer that stops the command", func(t *testing.T) {
		r := fakeRunner(t, "huge 2000000000", nil)
		start := time.Now()
		_, err := r.RunStream(context.Background(), Command{Args: []string{"log"}}, func(stdout io.Reader) error {
			buf := make([]byte, 1024)
			if _, err := io.ReadFull(stdout, buf); err != nil {
				return err
			}
			return ErrStopStream
		})
		if err != nil {
			t.Fatalf("stopped stream: %v", err)
		}
		if took := time.Since(start); took > 15*time.Second {
			t.Errorf("took %v", took)
		}
	})
	t.Run("a consumer that fails: its error comes back untouched", func(t *testing.T) {
		r := fakeRunner(t, "huge 2000000", nil)
		sentinel := &errors.Error{Tag: "TestConsumerFailed"}
		_, err := r.RunStream(context.Background(), Command{Args: []string{"log"}}, func(io.Reader) error { return sentinel })
		if err != error(sentinel) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a failure of git after the stream", func(t *testing.T) {
		r := fakeRunner(t, "stdout \"abc\"\nstderr \"fatal: not a git repository\\n\"\nexit 128", nil)
		_, err := r.RunStream(context.Background(), Command{Args: []string{"status"}}, func(stdout io.Reader) error {
			_, err := io.Copy(io.Discard, stdout)
			return err
		})
		wantRoute(t, err, "Runner.RunStream->Runner.execute->classify.NotAGitRepository")
	})
	t.Run("a consumer that fails because the stream was cancelled", func(t *testing.T) {
		r := fakeRunner(t, "stdout \"x\"\nhang", nil)
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(200 * time.Millisecond); cancel() }()
		_, err := r.RunStream(ctx, Command{Args: []string{"log"}}, func(stdout io.Reader) error {
			_, err := io.Copy(io.Discard, stdout)
			if err == nil {
				err = &errors.Error{Tag: "TestPipeClosed"}
			}
			return err
		})
		wantRoute(t, err, "Runner.RunStream->Runner.execute.GitCancelled")
	})
	t.Run("cancelling a stream", func(t *testing.T) {
		r := fakeRunner(t, "stdout \"x\"\nhang", nil)
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(200 * time.Millisecond); cancel() }()
		_, err := r.RunStream(ctx, Command{Args: []string{"log"}}, func(stdout io.Reader) error {
			_, err := io.Copy(io.Discard, stdout)
			return err
		})
		wantRoute(t, err, "Runner.RunStream->Runner.execute.GitCancelled")
	})
}

// Git exiting before it read its input must not fail the command (VS Code's "Git process input" test: 8 MiB for --version).
func TestGitThatExitsBeforeReadingItsInput(t *testing.T) {
	e := gittest.NewEnv(t)
	r := NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: e.Vars()})
	res, err := r.Run(context.Background(), Command{Args: []string{"--version"}, Stdin: strings.NewReader(strings.Repeat("x", 8*1024*1024))})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(res.Stdout), "git version ") {
		t.Errorf("stdout = %q", res.Stdout)
	}
}

// fakeClock returns times that advance by the given steps, one per call.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	steps []time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.now
	if len(c.steps) > 0 {
		c.now = c.now.Add(c.steps[0])
		c.steps = c.steps[1:]
	}
	return t
}

func TestTheGitChannelLog(t *testing.T) {
	const secret = "https://user:s3cret@example.com/x.git"
	for _, tc := range []struct {
		name      string
		script    string
		command   Command
		toLog     []string
		want      []string
		wantError bool
	}{
		{"a command line with its duration", "stdout \"M a\\n\"", Command{Args: []string{"status", "-z", "-uall"}}, nil,
			[]string{"> git status -z -uall [34ms]"}, false},
		{"the stdout of a command in commandsToLog", "stdout \"line one\\nline two\\n\"", Command{Args: []string{"show", "HEAD"}}, []string{"show"},
			[]string{"> git show HEAD [34ms]", "line one\nline two"}, false},
		{"stdout of another command is not logged", "stdout \"line one\\n\"", Command{Args: []string{"log"}}, []string{"show"},
			[]string{"> git log [34ms]"}, false},
		{"stderr is always logged", "stderr \"warning: something\\n\"", Command{Args: []string{"fetch"}}, nil,
			[]string{"> git fetch [34ms]", "warning: something"}, false},
		{"the userinfo of a URL is masked in the command and in stderr", "stderr \"fatal: unable to access '" + secret + "': nope\\n\"\nexit 128",
			Command{Args: []string{"clone", secret, "/tmp/x"}}, nil,
			[]string{"> git clone https://***@example.com/x.git /tmp/x [34ms]", "fatal: unable to access 'https://***@example.com/x.git': nope"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var lines []string
			clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), steps: []time.Duration{34 * time.Millisecond}}
			r := fakeRunner(t, tc.script, func(c *Config) {
				c.Logger = func(line string) { mu.Lock(); lines = append(lines, line); mu.Unlock() }
				c.CommandsToLog = tc.toLog
				c.Now = clock.Now
			})
			_, err := r.Run(context.Background(), tc.command)
			if (err != nil) != tc.wantError {
				t.Fatalf("err = %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(lines, tc.want) {
				t.Errorf("log = %q\nwant  %q", lines, tc.want)
			}
		})
	}

	t.Run("a cancelled command says so", func(t *testing.T) {
		var mu sync.Mutex
		var lines []string
		r := fakeRunner(t, "hang", func(c *Config) { c.Logger = func(l string) { mu.Lock(); lines = append(lines, l); mu.Unlock() } })
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, _ = r.Run(ctx, Command{Args: []string{"pull"}})
		mu.Lock()
		defer mu.Unlock()
		if len(lines) != 1 || !strings.HasPrefix(lines[0], "> git pull [") || !strings.HasSuffix(lines[0], "(cancelled)") {
			t.Errorf("log = %q", lines)
		}
	})
}

func TestMaskURLs(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://user:secret@host/x", "https://***@host/x"},
		{"https://token@github.com/a/b.git", "https://***@github.com/a/b.git"},
		{"clone https://u:p@h/x and ssh://git@h:22/y", "clone https://***@h/x and ssh://***@h:22/y"},
		{"https://host/no/userinfo", "https://host/no/userinfo"},
		{"https://host/path@with-at", "https://host/path@with-at"},
		{"git@github.com:a/b.git", "git@github.com:a/b.git"},
		{"HTTP://U:P@H", "HTTP://***@H"},
		{"", ""},
	} {
		if got := MaskURLs(tc.in); got != tc.want {
			t.Errorf("MaskURLs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLastBytesKeepsWholeCharacters(t *testing.T) {
	s := strings.Repeat("é", 10) // 20 bytes
	if got := lastBytes(s, 5); got != "éé" {
		t.Errorf("lastBytes = %q", got)
	}
	if got := lastBytes("abc", 10); got != "abc" {
		t.Errorf("lastBytes = %q", got)
	}
}

// The background flags and variables do what they are for, with the real git: a program that a repository's configuration names
// is started by a foreground command and not by a background one (D37).
func TestBackgroundCommandsStartNoProgramOfTheRepositoryConfiguration(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("a.txt", "one\n")
	repo.Write("b.bin", "binary-ish\n")
	repo.Write(".gitattributes", "*.bin diff=conv\n")
	repo.Commit("base")
	repo.Write("a.txt", "two\n")
	repo.Write("b.bin", "changed\n")

	marker := func(name string) string { return filepath.Join(e.Root, "marker-"+name) }
	script := func(name, body string) string {
		p := filepath.Join(e.Root, name+".sh")
		if err := os.WriteFile(p, []byte("#!/bin/sh\n: > "+marker(name)+"\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	repo.Git("config", "diff.external", script("external", "exit 0\n"))
	repo.Git("config", "diff.conv.textconv", script("textconv", "cat \"$1\"\n"))
	repo.Git("config", "core.fsmonitor", script("fsmonitor", "exit 0\n"))
	// The harness keeps core.fsmonitor off with a setting of the command; the test needs the configuration to apply.
	env := withVars(e.Vars(), "GIT_CONFIG_PARAMETERS=")
	env = removeVars(env, "GIT_OPTIONAL_LOCKS")
	runner := NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: env})

	ran := func(name string) bool { _, err := os.Stat(marker(name)); return err == nil }
	clear := func() {
		for _, n := range []string{"external", "textconv", "fsmonitor"} {
			_ = os.Remove(marker(n))
		}
	}
	for _, tc := range []struct {
		name       string
		command    Command
		background bool
		wantRan    []string
	}{
		{"diff in the foreground", Command{Args: []string{"diff"}}, false, []string{"external", "fsmonitor"}},
		{"diff in the background", Command{Args: []string{"diff"}}, true, nil},
		{"show of a blob in the foreground", Command{Args: []string{"show", "HEAD:b.bin"}}, false, nil}, // show prints a blob raw
		{"log -p in the foreground", Command{Args: []string{"log", "-p", "-n1", "--format=%H"}}, false, []string{"textconv"}},
		{"log -p in the background", Command{Args: []string{"log", "-p", "-n1", "--format=%H"}}, true, nil},
		{"status in the foreground", Command{Args: []string{"status", "-z"}}, false, []string{"fsmonitor"}},
		{"status in the background", Command{Args: []string{"status", "-z"}}, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clear()
			tc.command.Dir = repo.Dir
			tc.command.Background = tc.background
			if _, err := runner.Run(context.Background(), tc.command); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, n := range []string{"external", "textconv", "fsmonitor"} {
				if ran(n) {
					got = append(got, n)
				}
			}
			if !reflect.DeepEqual(got, tc.wantRan) {
				t.Errorf("programs started: %v, want %v", got, tc.wantRan)
			}
		})
	}
}
