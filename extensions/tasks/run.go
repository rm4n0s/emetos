package tasks

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// killGrace is how long a task has to end after it was asked to, before its whole process group is killed.
const killGrace = 2 * time.Second

// maxLineBytes is the longest line of output read whole; a longer one is cut into pieces.
const maxLineBytes = 1 << 20

// process is one run of a command: a shell in a process group of its own, which ends with its context.
type process struct {
	cmd  *exec.Cmd
	done chan struct{}

	mu   sync.Mutex
	exit int
	err  error
}

// start runs the command line in the folder under ctx and calls line for each line of its output, from a goroutine of its
// own, and finish once, after the process ended and its output was read, from the same goroutine. The shell leads a process
// group; when ctx ends the group is told to terminate and, after killGrace, killed, so no child of the command outlives it.
func start(ctx context.Context, folder, cmdline string, line func(string), finish func(exit int, err error)) (*process, error) {
	cmd := exec.Command("sh", "-c", cmdline)
	cmd.Dir = folder
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	cmd.WaitDelay = killGrace
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		return nil, err
	}
	p := &process{cmd: cmd, done: make(chan struct{})}
	pgid := cmd.Process.Pid
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
		for sc.Scan() {
			line(sc.Text())
		}
		_, _ = io.Copy(io.Discard, pr) // a line that was too long: drain the rest so the process is not blocked
	}()
	waited := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			select {
			case <-waited:
			case <-time.After(killGrace):
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
		case <-waited:
		}
	}()
	go func() {
		err := cmd.Wait()
		close(waited)
		_ = pw.Close()
		wg.Wait()
		// whatever the command left behind in its group goes with it
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		p.mu.Lock()
		p.exit, p.err = cmd.ProcessState.ExitCode(), err
		p.mu.Unlock()
		close(p.done)
		finish(p.exit, err)
	}()
	return p, nil
}
