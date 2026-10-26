package lsp_test

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

// gone tells whether no process of the group (or the process) exists any more.
func gone(pid int) bool { return syscall.Kill(pid, 0) == syscall.ESRCH }

func waitGone(t *testing.T, what string, pid int) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !gone(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("%s (pid %d) is still running", what, pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStartProcessErrors(t *testing.T) {
	dir := t.TempDir()
	badInterpreter := filepath.Join(dir, "bad.sh")
	if err := os.WriteFile(badInterpreter, []byte("#!/no/such/interpreter\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, program, route string
	}{
		{"a program that is not in PATH", "emetos-no-such-program", "StartProcess.ProgramNotFound"},
		{"a program that cannot be run", badInterpreter, "StartProcess.ProcessStartFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := lsp.StartProcess(context.Background(), lsp.ProcessSpec{Program: tt.program})
			wantRoute(t, err, tt.route)
		})
	}
	_, err := lsp.FindProgram("emetos-no-such-program")
	wantRoute(t, err, "FindProgram.ProgramNotFound")
	if p, err := lsp.FindProgram("sh"); err != nil || !filepath.IsAbs(p) {
		t.Errorf("FindProgram(sh) = %q, %v", p, err)
	}
}

func TestProcessIsAServerStream(t *testing.T) {
	prog := lsptest.BuildProgram(t)
	script := filepath.Join(t.TempDir(), "script.json")
	if err := os.WriteFile(script, []byte(`{"capabilities":{"hoverProvider":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var lines []string
	proc, err := lsp.StartProcess(context.Background(), lsp.ProcessSpec{
		Program: "sh", Args: []string{"-c", `echo "to stderr" >&2; exec "$0" "$@"`, prog, "-script", script},
		Dir: t.TempDir(), Env: []string{"EMETOS_TEST=1"},
		Stderr: func(l string) { mu.Lock(); lines = append(lines, l); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { proc.Stop(0) })
	if proc.PID() <= 0 {
		t.Fatalf("PID = %d", proc.PID())
	}
	c, err := lsp.NewClient(ctxT(t), proc, lsp.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !c.Capabilities().HoverProvider.Enabled {
		t.Errorf("capabilities = %+v", c.Capabilities())
	}
	if err := c.Shutdown(ctxT(t)); err != nil {
		t.Fatal(err)
	}
	proc.Stop(5 * time.Second)
	select {
	case <-proc.Done():
	default:
		t.Fatal("Stop returned before the program ended")
	}
	if proc.ExitCode() != 0 {
		t.Errorf("exit code = %d, want 0 after shutdown and exit", proc.ExitCode())
	}
	waitGone(t, "the server", proc.PID())
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 1 || lines[0] != "to stderr" {
		t.Errorf("stderr lines = %q", lines)
	}
}

func TestStopKillsTheWholeGroup(t *testing.T) {
	tests := []struct {
		name  string
		shell string
		grace time.Duration
	}{
		{"a child outlives a program that ends", `sleep 300 & echo $!`, time.Second},
		{"a program that ignores the end of its input", `trap '' TERM; sleep 300 & echo $!; while :; do sleep 1; done`, 50 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc, err := lsp.StartProcess(context.Background(), lsp.ProcessSpec{Program: "sh", Args: []string{"-c", tt.shell}})
			if err != nil {
				t.Fatal(err)
			}
			line, err := bufio.NewReader(proc).ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			child, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil {
				t.Fatal(err)
			}
			if gone(child) {
				t.Fatalf("the child %d is not running", child)
			}
			start := time.Now()
			proc.Stop(tt.grace)
			if d := time.Since(start); d > 10*time.Second {
				t.Errorf("Stop took %v", d)
			}
			waitGone(t, "the child", child)
			waitGone(t, "the program", proc.PID())
		})
	}
}

func TestStartProcessEndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	proc, err := lsp.StartProcess(ctx, lsp.ProcessSpec{Program: "sh", Args: []string{"-c", "sleep 300"}})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-proc.Done():
	case <-time.After(wait):
		t.Fatal("the program did not end with its context")
	}
	waitGone(t, "the program", proc.PID())
}

func TestLongStderrLinesAreCut(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	proc, err := lsp.StartProcess(context.Background(), lsp.ProcessSpec{
		Program: "sh", Args: []string{"-c", `head -c 200000 /dev/zero | tr '\0' x >&2; echo >&2; echo after >&2`},
		Stderr: func(l string) { mu.Lock(); lines = append(lines, l); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-proc.Done():
	case <-time.After(wait):
		t.Fatal("the program did not end")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 2 || len(lines[0]) != 64*1024 || lines[1] != "after" {
		var sizes []int
		for _, l := range lines {
			sizes = append(sizes, len(l))
		}
		t.Errorf("lines = %v", sizes)
	}
}
