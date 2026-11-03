package python

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/rm4n0s/errors"
)

// cmdSpec is a program to run: its path, arguments, working folder and environment.
type cmdSpec struct {
	Program string
	Args    []string
	Dir     string
	Env     []string
}

// testRunner runs a program for the Testing provider and calls line for each line of its output. The pack uses runProcess; the tests
// give it a fake that prints recorded lines.
type testRunner func(ctx context.Context, spec cmdSpec, line func(string)) error

// maxLineBytes is the longest line of output read; a longer one ends the output that is read (the program is not blocked: the rest is
// drained).
const maxLineBytes = 1 << 20

// runProcess runs the program in a process group of its own and calls line, from the calling goroutine's reader, for each line of its
// standard output and standard error (they are one stream). When ctx ends the whole group is killed; whatever the program leaves in the
// group when it ends is killed too, so no process outlives the call. A program that did not start fails with the tag
// PythonProgramStartFailed, one that ended with a status other than 0 or was killed with PythonProgramFailed.
func runProcess(ctx context.Context, spec cmdSpec, line func(string)) error {
	cmd := exec.Command(spec.Program, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pr, pw, err := os.Pipe()
	if err != nil {
		return errors.NewErr("PythonProgramStartFailed", err).SetMetadata("program", spec.Program)
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return errors.NewErr("PythonProgramStartFailed", err).SetMetadata("program", spec.Program)
	}
	_ = pw.Close() // the program has its own copy
	pgid := cmd.Process.Pid
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
		for sc.Scan() {
			if line != nil {
				line(strings.ToValidUTF8(sc.Text(), "?"))
			}
		}
		_, _ = io.Copy(io.Discard, pr) // a line that was too long: drain the rest so the program is not blocked
	}()
	waited := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		case <-waited:
		}
	}()
	err = cmd.Wait()
	close(waited)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	readers.Wait()
	_ = pr.Close()
	if err != nil {
		return errors.NewErr("PythonProgramFailed", err).SetMetadata("program", spec.Program, "args", strings.Join(spec.Args, " "))
	}
	return nil
}
