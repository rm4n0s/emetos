package dap

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

// A debug adapter that is a program (SPEC §10.31 "The session") runs in the folder the configuration names, with the environment
// of Emetos and the variables of the descriptor, in a process group of its own, so that it and everything it started can be
// stopped together and none outlives the session. Its standard output and input are the stream of the Client (or, for a server
// program, the place it prints its port); its standard error is read line by line for the log.

// killGrace is how long a program that was told to terminate has before the whole group is killed.
const killGrace = 3 * time.Second

// maxLine is the longest line of the standard error (or output, for a server program) that is kept whole; the rest of a longer
// line is dropped.
const maxLine = 64 * 1024

// ProcessSpec says how to start an adapter program.
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
	// Stdout, when set, takes the lines of the standard output, from a goroutine of its own; the Process then has no stream to
	// read (Read gives the end of file). It is for server programs (StartServer); the standard output of an adapter that speaks
	// DAP on it must be left to the client.
	Stdout func(line string)
}

// Process is a running adapter program. It is the stream of a Client: Read reads its standard output and Write writes to its
// standard input; Close closes both and ends the program (SIGTERM to its group, SIGKILL after three seconds).
type Process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser // nil when ProcessSpec.Stdout took the output
	pgid   int
	done   chan struct{}

	closeOnce sync.Once
	mu        sync.Mutex
	code      int
	err       error
}

// StartProcess starts the program in a process group of its own. When ctx ends the group is killed, so ctx is the life of the
// process, not a limit for its start. It fails with the tags DapAdapterNotFound and DapAdapterStartFailed.
func StartProcess(ctx context.Context, spec ProcessSpec) (*Process, error) {
	path, err := exec.LookPath(spec.Program)
	if err != nil {
		return nil, errors.NewErr("DapAdapterNotFound", err).SetMetadata("program", spec.Program)
	}
	pctx, cancel := context.WithCancel(ctx)
	cmd := exec.Command(path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, errors.NewErr("DapAdapterStartFailed", err).SetMetadata("program", path)
	}
	var stdout io.ReadCloser
	var outW io.WriteCloser
	var outLines *io.PipeReader
	if spec.Stdout != nil {
		var w *io.PipeWriter
		outLines, w = io.Pipe()
		outW = w
	} else {
		// Not StdoutPipe: Wait closes that pipe when the program ends, and the client may not have read the last answer yet.
		// This pipe ends with end of file when the program is gone and everything was read.
		var r, w *os.File
		r, w, err = os.Pipe()
		if err != nil {
			cancel()
			return nil, errors.NewErr("DapAdapterStartFailed", err).SetMetadata("program", path)
		}
		stdout, outW = r, w
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
		if stdout != nil {
			closeQuietly(stdout)
		}
		if errW != nil {
			closeQuietly(errW)
		}
		return nil, errors.NewErr("DapAdapterStartFailed", err).SetMetadata("program", path)
	}
	if stdout != nil {
		closeQuietly(outW) // the child has its own copy
	}
	p := &Process{cmd: cmd, stdin: stdin, stdout: stdout, pgid: cmd.Process.Pid, done: make(chan struct{})}
	var lines sync.WaitGroup
	if errR != nil {
		lines.Add(1)
		go func() {
			defer lines.Done()
			readLines(errR, spec.Stderr)
		}()
	}
	if outLines != nil {
		lines.Add(1)
		go func() {
			defer lines.Done()
			readLines(outLines, spec.Stdout)
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
		if stdout == nil {
			closeQuietly(outW)
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

// closeQuietly closes something whose failure to close cannot matter: a pipe end of a process that ended or failed to start.
func closeQuietly(c io.Closer) { _ = c.Close() }

// readLines calls fn with each line of r; a line longer than maxLine is cut and the rest of it dropped.
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
			if len(line) > maxLine {
				line, cut = line[:maxLine], true
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
func (p *Process) Read(b []byte) (int, error) {
	if p.stdout == nil {
		return 0, io.EOF
	}
	return p.stdout.Read(b)
}

// Write writes to the standard input of the program.
func (p *Process) Write(b []byte) (int, error) { return p.stdin.Write(b) }

// Close closes the standard input and output of the program and ends it: SIGTERM to its group now, SIGKILL three seconds later
// when it is still there. It does not wait; Stop does. Closing twice is harmless.
func (p *Process) Close() error {
	err := p.stdin.Close()
	if p.stdout != nil {
		if oerr := p.stdout.Close(); err == nil {
			err = oerr
		}
	}
	p.closeOnce.Do(func() { go p.terminate() })
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

// terminate tells the group to end and kills it when it does not.
func (p *Process) terminate() {
	select {
	case <-p.done:
		return
	default:
	}
	p.killGroup(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(killGrace):
		p.killGroup(syscall.SIGKILL)
	}
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

// Stop ends the program: it closes the standard input, waits up to grace for the program to end by itself, then tells the whole
// process group to terminate and, after three seconds more, kills it. It returns when the program has ended. Stopping a program
// that ended is harmless.
func (p *Process) Stop(grace time.Duration) {
	closeQuietly(p.stdin) // the end of its input is how an adapter learns it is done
	select {
	case <-p.done:
		return
	case <-time.After(grace):
	}
	p.terminate()
	<-p.done
	if p.stdout != nil {
		closeQuietly(p.stdout)
	}
}
