package lsp

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
)

// A language server is a program the host starts (SPEC §9.12 "Servers and their lifetime"): it runs in the folder of the
// workspace, with the environment of Emetos, in a process group of its own, so that it and everything it started can be
// stopped together and none outlives the window tab. Its standard input and output are the stream of the Client; its
// standard error is read line by line for the Output channel.

// killGrace is how long a process that was told to terminate has before the whole group is killed.
const killGrace = 2 * time.Second

// maxStderrLine is the longest line of the standard error that is kept whole; the rest of a longer line is dropped.
const maxStderrLine = 64 * 1024

// ProcessSpec says how to start a server.
type ProcessSpec struct {
	// Program is the path of the program, or a name that is looked up in PATH.
	Program string
	// Args are its arguments.
	Args []string
	// Dir is the folder it runs in; empty is the folder of Emetos.
	Dir string
	// Env are variables ("NAME=value") added to the environment of Emetos.
	Env []string
	// Stderr, when set, is called with each line the program writes to its standard error, from a goroutine of its own.
	Stderr func(line string)
}

// Process is a running server program. It is the stream of a Client: Read reads its standard output and Write writes to
// its standard input; Close closes both (the program ends when it reads the end of its input, as servers do).
type Process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	pgid   int
	done   chan struct{}
	cancel context.CancelFunc

	mu   sync.Mutex
	code int
	err  error
}

// FindProgram looks a program up the way StartProcess will: a name with no slash in PATH, a path as it is. It fails with
// the tag ProgramNotFound, with the program in the metadata.
func FindProgram(program string) (string, error) {
	path, err := exec.LookPath(program)
	if err != nil {
		return "", errors.NewErr("ProgramNotFound", err).SetMetadata("program", program)
	}
	return path, nil
}

// StartProcess starts the program in a process group of its own. When ctx ends the group is killed. It fails with the
// tags ProgramNotFound and ProcessStartFailed.
func StartProcess(ctx context.Context, spec ProcessSpec) (*Process, error) {
	path, err := exec.LookPath(spec.Program)
	if err != nil {
		return nil, errors.NewErr("ProgramNotFound", err).SetMetadata("program", spec.Program)
	}
	pctx, cancel := context.WithCancel(ctx)
	cmd := exec.Command(path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, errors.NewErr("ProcessStartFailed", err).SetMetadata("program", path)
	}
	// Not StdoutPipe: Wait closes that pipe when the program ends, and the client may not have read the last answer yet.
	// This pipe ends with end of file when the program is gone and everything was read.
	outR, outW, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, errors.NewErr("ProcessStartFailed", err).SetMetadata("program", path)
	}
	cmd.Stdout = outW
	var errR *io.PipeReader
	var errW *io.PipeWriter
	if spec.Stderr != nil {
		errR, errW = io.Pipe()
		cmd.Stderr = errW
	}
	if err := cmd.Start(); err != nil {
		cancel()
		closeQuietly(outW)
		closeQuietly(outR)
		if errW != nil {
			closeQuietly(errW)
		}
		return nil, errors.NewErr("ProcessStartFailed", err).SetMetadata("program", path)
	}
	closeQuietly(outW) // the child has its own copy
	p := &Process{cmd: cmd, stdin: stdin, stdout: outR, pgid: cmd.Process.Pid, done: make(chan struct{}), cancel: cancel}
	var lines sync.WaitGroup
	if errR != nil {
		lines.Add(1)
		go func() {
			defer lines.Done()
			readLines(errR, spec.Stderr)
		}()
	}
	go func() {
		select {
		case <-pctx.Done():
			p.killGroup(syscall.SIGKILL)
		case <-p.done:
		}
	}()
	go func() {
		werr := cmd.Wait()
		if errW != nil {
			closeQuietly(errW)
		}
		lines.Wait()
		// whatever the program left behind in its group goes with it
		p.killGroup(syscall.SIGKILL)
		p.mu.Lock()
		p.code, p.err = cmd.ProcessState.ExitCode(), werr
		p.mu.Unlock()
		close(p.done)
		cancel()
	}()
	return p, nil
}

// closeQuietly closes something whose failure to close cannot matter: a pipe end of a process that failed to start.
func closeQuietly(c io.Closer) { _ = c.Close() }

// readLines calls fn with each line of r; a line longer than maxStderrLine is cut and the rest of it dropped.
func readLines(r io.Reader, fn func(string)) {
	br := bufio.NewReaderSize(r, 4096)
	var line []byte
	cut := false
	for {
		chunk, isPrefix, err := br.ReadLine()
		if err != nil {
			if len(line) > 0 {
				fn(string(line))
			}
			_, _ = io.Copy(io.Discard, r)
			return
		}
		if !cut {
			line = append(line, chunk...)
			if len(line) > maxStderrLine {
				line, cut = line[:maxStderrLine], true
			}
		}
		if !isPrefix {
			fn(string(line))
			line, cut = line[:0], false
		}
	}
}

// killGroup sends a signal to the process group; a group that is gone is not an error.
func (p *Process) killGroup(sig syscall.Signal) { _ = syscall.Kill(-p.pgid, sig) }

// Read reads the standard output of the program.
func (p *Process) Read(b []byte) (int, error) { return p.stdout.Read(b) }

// Write writes to the standard input of the program.
func (p *Process) Write(b []byte) (int, error) { return p.stdin.Write(b) }

// Close closes the standard input and output of the program, which makes a server end. It does not wait; Stop does.
func (p *Process) Close() error {
	err := p.stdin.Close()
	if oerr := p.stdout.Close(); err == nil {
		err = oerr
	}
	return err
}

// PID is the process id of the program, which is also the id of its process group.
func (p *Process) PID() int { return p.pgid }

// Done is closed when the program has ended and its standard error was read.
func (p *Process) Done() <-chan struct{} { return p.done }

// ExitCode is the exit status of the program (-1 when a signal ended it); only meaningful after Done.
func (p *Process) ExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code
}

// Stop ends the program: it closes the standard input, waits up to grace for the program to end by itself, then tells the
// whole process group to terminate and, after two seconds more, kills it. It returns when the program has ended. Stopping a
// program that ended is harmless.
func (p *Process) Stop(grace time.Duration) {
	closeQuietly(p.stdin) // the end of its input is how a server learns it is done
	select {
	case <-p.done:
		return
	case <-time.After(grace):
	}
	p.killGroup(syscall.SIGTERM)
	select {
	case <-p.done:
		return
	case <-time.After(killGrace):
	}
	p.killGroup(syscall.SIGKILL)
	<-p.done
	closeQuietly(p.stdout)
}
