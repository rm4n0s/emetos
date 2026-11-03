package cpp

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runProcess (step 26.7): the order of the lines, the exit status, a program that does not start and a cancel that leaves nothing.

func TestRunProcessReadsOutputAndErrorInOrderAndReportsTheStatus(t *testing.T) {
	var got []string
	err := runProcess(context.Background(), cmdSpec{Program: "/bin/sh", Args: []string{"-c", "echo one; echo two >&2; echo three; exit 0"}, Env: os.Environ()}, func(l string) { got = append(got, l) })
	if err != nil || strings.Join(got, ",") != "one,two,three" {
		t.Errorf("lines %v, err %v", got, err)
	}
	got = nil
	err = runProcess(context.Background(), cmdSpec{Program: "/bin/sh", Args: []string{"-c", "echo x; exit 3"}, Env: os.Environ()}, func(l string) { got = append(got, l) })
	wantRoute(t, err, "runProcess.CppTestProgramFailed")
	if strings.Join(got, ",") != "x" {
		t.Errorf("lines %v", got)
	}
}

func TestRunProcessFailsToStartAMissingProgram(t *testing.T) {
	err := runProcess(context.Background(), cmdSpec{Program: filepath.Join(t.TempDir(), "nope"), Env: os.Environ()}, nil)
	wantRoute(t, err, "runProcess.CppTestProgramStartFailed")
}

func TestRunProcessKillsTheWholeGroupWhenTheContextEnds(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runProcess(ctx, cmdSpec{Program: "/bin/sh", Args: []string{"-c", "sleep 60 & echo $! > " + pidFile + "; wait"}, Env: os.Environ()}, nil)
	}()
	deadline := time.Now().Add(10 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil && strings.TrimSpace(string(data)) != "" {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("the program did not start")
	}
	cancel()
	select {
	case err := <-done:
		wantRoute(t, err, "runProcess.CppTestProgramFailed")
	case <-time.After(10 * time.Second):
		t.Fatal("runProcess did not return")
	}
	for i := 0; i < 200 && syscall.Kill(pid, 0) == nil; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Error("the child of the program is still running")
	}
}
