package gittest

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runFakeGit starts the fake git with the script and returns what it printed and how it ended.
func runFakeGit(t *testing.T, script string, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	path, env := FakeGit(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), env)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

func TestFakeGitDirectives(t *testing.T) {
	recordFile := filepath.Join(t.TempDir(), "calls.jsonl")
	for _, tc := range []struct {
		name       string
		script     string
		stdin      string
		args       []string
		wantOut    string
		wantErr    string
		wantStatus int
	}{
		{name: "an old git", script: "version 2.29.1", args: []string{"--version"}, wantOut: "git version 2.29.1\n"},
		{name: "the version only answers --version", script: "version 2.43.0\nstdout \"other\\n\"", args: []string{"status"}, wantOut: "other\n"},
		{name: "a failing git", script: "stderr \"fatal: not a git repository\\n\"\nexit 128", args: []string{"status"}, wantErr: "fatal: not a git repository\n", wantStatus: 128},
		{name: "standard input is counted", script: "stdin", stdin: "hello", args: []string{"x"}, wantOut: "stdin 5\n"},
		{name: "a huge output", script: "huge 100000", args: []string{"log"}, wantOut: strings.Repeat("x", 100000)},
		{name: "a slow git", script: "sleep 50ms\nstdout \"done\"", args: []string{"x"}, wantOut: "done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, err := runFakeGit(t, tc.script, tc.stdin, tc.args...)
			status := 0
			if exit, ok := err.(*exec.ExitError); ok {
				status = exit.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if out != tc.wantOut || errOut != tc.wantErr || status != tc.wantStatus {
				t.Errorf("got %d bytes of output %.40q, error output %q, status %d; want %.40q, %q, %d", len(out), out, errOut, status, tc.wantOut, tc.wantErr, tc.wantStatus)
			}
		})
	}

	t.Run("record and dump", func(t *testing.T) {
		out, _, err := runFakeGit(t, "record "+recordFile+"\ndump", "", "status", "-z")
		if err != nil {
			t.Fatal(err)
		}
		var call FakeCall
		if err := json.Unmarshal([]byte(out), &call); err != nil {
			t.Fatalf("dump: %v\n%s", err, out)
		}
		if strings.Join(call.Args, " ") != "status -z" || call.PID == 0 {
			t.Errorf("call = %+v", call)
		}
		data, err := os.ReadFile(recordFile)
		if err != nil || !strings.Contains(string(data), `"args":["status","-z"]`) {
			t.Errorf("record file = %q, %v", data, err)
		}
	})

	t.Run("a missing git does not exist", func(t *testing.T) {
		if _, err := os.Stat(MissingGit(t)); !os.IsNotExist(err) {
			t.Errorf("the missing git exists: %v", err)
		}
	})
}

func TestFakeGitHangsUntilKilledAndLeavesAChildInTheGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	path, env := FakeGit(t, "child "+pidFile+"\nhang")
	cmd := exec.Command(path, "fetch")
	cmd.Env = append(os.Environ(), env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	})
	var childPID int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil && len(data) > 0 {
			childPID, _ = strconv.Atoi(string(data))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatal("the fake did not start its child")
	}
	if err := syscall.Kill(childPID, 0); err != nil {
		t.Fatalf("the child %d is not running: %v", childPID, err)
	}
	if pgid, err := syscall.Getpgid(childPID); err != nil || pgid != cmd.Process.Pid {
		t.Errorf("the child is in group %d (%v), want the group %d of the fake", pgid, err, cmd.Process.Pid)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	<-done
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(childPID) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("the child %d survived the signal to the group", childPID)
}

// alive says whether the process runs (a zombie that nobody has reaped yet does not count).
func alive(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// The state is the field after the command name, which is in parentheses.
	s := string(data)
	if i := strings.LastIndex(s, ")"); i >= 0 && len(s) > i+2 {
		return s[i+2] != 'Z'
	}
	return true
}
