package golang

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/golang/gotest"
)

func TestRunProgramReadsBothStreamsInOrderAndEndsItsGroup(t *testing.T) {
	script := filepath.Join(t.TempDir(), "prog.sh")
	pidfile := filepath.Join(t.TempDir(), "child")
	text := "#!/bin/sh\necho out1\necho err1 >&2\n(sleep 60 & echo $! > " + pidfile + "; wait) &\nsleep 0.2\necho " + strings.Repeat("x", 70000) + "\necho last\n"
	if err := os.WriteFile(script, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	// the shell needs sleep, and PATH is empty in the tests
	err := runProgram(context.Background(), cmdSpec{Program: script, Dir: t.TempDir(), Env: environment("PATH=/usr/bin:/bin")}, func(l string) { lines = append(lines, l) })
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 4 || lines[0] != "out1" || lines[1] != "err1" || len(lines[2]) != 70000 || lines[3] != "last" {
		t.Fatalf("lines = %d: %.30q", len(lines), lines)
	}
	// the program ended, and what it left in its group went with it
	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; !gone(child) && i < 1500; i++ {
		sleepMS(10)
	}
	if !gone(child) {
		t.Errorf("the child %d of the program is still there", child)
	}
}

func TestRunProgramDrainsALineThatIsTooLong(t *testing.T) {
	script := filepath.Join(t.TempDir(), "long.sh")
	text := "#!/bin/sh\necho before\nhead -c " + strconv.Itoa(maxLineBytes+4096) + " /dev/zero | tr '\\0' x\necho\necho after\n"
	if err := os.WriteFile(script, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
	var lines []string
	err := runProgram(context.Background(), cmdSpec{Program: script, Env: environment("PATH=/usr/bin:/bin")}, func(l string) { lines = append(lines, l) })
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "before" {
		t.Errorf("lines = %d: %.30q", len(lines), lines)
	}
}

func TestRunProgramStopsWhenItsContextEnds(t *testing.T) {
	t.Setenv(gotest.EnvHang, "x")
	t.Setenv(gotest.EnvPidfile, filepath.Join(t.TempDir(), "pids"))
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var lines []string
	done := make(chan error, 1)
	go func() {
		done <- runProgram(ctx, cmdSpec{Program: filepath.Join(fakeGoDir, "go"), Args: []string{"install", "x@v1"}, Dir: t.TempDir(), Env: environment()}, func(l string) {
			mu.Lock()
			lines = append(lines, l)
			mu.Unlock()
		})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		n := len(lines)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no output")
		}
		sleepMS(5)
	}
	cancel()
	err := <-done
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("runProgram.ProgramFailed") {
		t.Errorf("err = %v", err)
	}
}

func TestRunProgramErrors(t *testing.T) {
	notExecutable := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(notExecutable, []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	fails := filepath.Join(t.TempDir(), "fails.sh")
	if err := os.WriteFile(fails, []byte("#!/bin/sh\necho no good\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		program string
		route   string
	}{
		{"a program that cannot start", notExecutable, "runProgram.ProgramStartFailed"},
		{"a program that fails", fails, "runProgram.ProgramFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lines []string
			err := runProgram(context.Background(), cmdSpec{Program: tt.program, Env: environment()}, func(l string) { lines = append(lines, l) })
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Fatalf("err = %v, want route %s", err, tt.route)
			}
			if tt.program == fails && (len(lines) != 1 || failureText(err, lines) != "no good") {
				t.Errorf("lines %v, text %q", lines, failureText(err, lines))
			}
		})
	}
	// with no output the text is that of the exit
	err := runProgram(context.Background(), cmdSpec{Program: fails + "x"}, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := failureText(err, nil); got == "" {
		t.Error("no text")
	}
}

func TestLookProgram(t *testing.T) {
	folder := t.TempDir()
	script := filepath.Join(folder, "tools", "mygo")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(folder, "tools"))
	tests := []struct {
		name, program, want string
	}{
		{"a name in PATH", "mygo", script},
		{"an absolute path", script, script},
		{"a relative path is relative to the folder", "tools/mygo", script},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lookProgram(tt.program, folder)
			if err != nil || got != tt.want {
				t.Errorf("lookProgram = %q, %v", got, err)
			}
		})
	}
	_, err := lookProgram("no-such-program", folder)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("lookProgram.ProgramNotFound") {
		t.Errorf("err = %v", err)
	}
}
