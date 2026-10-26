package golang

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
)

// The bounds of the programs the extension runs.
const (
	// versionTimeout and envTimeout end `go env GOVERSION` and `go env` when the program does not answer.
	versionTimeout = 20 * time.Second
	envTimeout     = 60 * time.Second
	// installTimeout ends the install of one tool.
	installTimeout = 15 * time.Minute
	// maxLineBytes is the longest line of output read; a longer one ends the output that is read (the program is not blocked: the
	// rest is drained).
	maxLineBytes = 1 << 20
)

// cmdSpec is a program to run: its path, arguments, working folder and environment.
type cmdSpec struct {
	Program string
	Args    []string
	Dir     string
	Env     []string
}

// runProgram runs the program in a process group of its own and calls line, from the calling goroutine, for each line of its
// standard output and standard error (they are one stream). When ctx ends the whole group is killed; whatever the program leaves
// in the group when it ends is killed too, so no process outlives the call. A program that did not start fails with the tag
// ProgramStartFailed, one that ended with a status other than 0 or was killed with ProgramFailed.
func runProgram(ctx context.Context, spec cmdSpec, line func(string)) error {
	cmd := exec.Command(spec.Program, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// one pipe for both streams, made here so that Wait does not wait for the processes that hold its other end: when the
	// program ends, what it left in its group is killed, which closes them
	pr, pw, err := os.Pipe()
	if err != nil {
		return errors.NewErr("ProgramStartFailed", err).SetMetadata("program", spec.Program)
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return errors.NewErr("ProgramStartFailed", err).SetMetadata("program", spec.Program)
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
		return errors.NewErr("ProgramFailed", err).SetMetadata("program", spec.Program, "args", strings.Join(spec.Args, " "))
	}
	return nil
}

// lookProgram finds a program: a name is looked up in PATH, a path with a slash is used as it is (a relative one is relative to
// the folder).
func lookProgram(name, folder string) (string, error) {
	if strings.Contains(name, "/") && !filepath.IsAbs(name) && folder != "" {
		name = filepath.Join(folder, name)
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", errors.NewErr("ProgramNotFound", err).SetMetadata("program", name)
	}
	return path, nil
}

// environment is the environment of Emetos with the variables ("NAME=value") added; a later entry wins.
func environment(extra ...string) []string {
	return append(os.Environ(), extra...)
}
