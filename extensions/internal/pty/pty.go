// Package pty runs a program with a pseudo-terminal as its terminal (SPEC §10.25 "The process"): the program is the leader
// of a new session, the terminal is its controlling terminal, and Emetos reads and writes the other end, resizes it and
// ends the program's whole process group. The pseudo-terminal is made with /dev/ptmx and golang.org/x/sys/unix, so Emetos
// stays cgo-free. Nothing uses the package before the terminal extension (milestone M17d); the Agents extension, whose
// login ran in a pseudo-terminal, was removed in step 17c.1 (D26).
package pty

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// Options say what Start runs and how its terminal looks.
type Options struct {
	// Program and Args are the program to run (found on PATH when Program has no slash) and its arguments (without the
	// program's own name).
	Program string
	Args    []string
	// Dir is the working folder; empty is the folder of Emetos.
	Dir string
	// Env is the whole environment of the program, as "NAME=value"; nil is the environment of Emetos.
	Env []string
	// Rows and Cols are the size of the terminal in cells; 0 is DefaultRows and DefaultCols.
	Rows, Cols int
	// NoEcho turns the echo of the terminal off, so that what is written to it is not read back (the program of a login
	// that reads a pasted code). A shell in a terminal leaves it on and echoes by itself.
	NoEcho bool
	// Grace is how long a program has to end after SIGTERM or SIGHUP before its group is killed; 0 is DefaultGrace.
	Grace time.Duration
}

// The sizes and times that Options uses when it leaves a field at 0.
const (
	// DefaultRows and DefaultCols are the size of a terminal that Options does not size.
	DefaultRows = 24
	DefaultCols = 80
	// DefaultGrace is how long Stop and Close wait before they kill the process group (VS Code waits 5 seconds too).
	DefaultGrace = 5 * time.Second
)

// Process is a program that runs with a pseudo-terminal as its terminal, in a session and a process group of its own. The
// pseudo-terminal is made with /dev/ptmx and golang.org/x/sys/unix, so Emetos stays cgo-free. Reading it, writing it,
// resizing it and stopping it may be done from any goroutine.
type Process struct {
	cmd    *exec.Cmd
	master *os.File
	pgid   int
	grace  time.Duration
	// done is closed when the program and its group are gone.
	done chan struct{}
	// exit is the exit status once done is closed (-1 for a signal).
	exit      int
	closeOnce sync.Once
}

// Start starts the program with the pseudo-terminal as its standard input, output and error and as its controlling
// terminal, as the leader of a new session. When ctx ends, the group gets SIGTERM and, after the grace time, SIGKILL. It
// fails with the tags PtyOpenFailed and ProcessStartFailed.
func Start(ctx context.Context, opts Options) (*Process, error) {
	rows, cols, grace := opts.Rows, opts.Cols, opts.Grace
	if rows <= 0 {
		rows = DefaultRows
	}
	if cols <= 0 {
		cols = DefaultCols
	}
	if grace <= 0 {
		grace = DefaultGrace
	}
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, errors.NewErr("PtyOpenFailed", err).SetMetadata("step", "open /dev/ptmx")
	}
	slave, err := openSlave(master, rows, cols, opts.NoEcho)
	if err != nil {
		_ = master.Close() // already failing
		return nil, err
	}
	defer func() { _ = slave.Close() }() // the child has its own copies
	cmd := exec.Command(opts.Program, opts.Args...)
	cmd.Dir, cmd.Env = opts.Dir, opts.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = master.Close() // already failing
		return nil, errors.NewErr("ProcessStartFailed", err).SetMetadata("program", opts.Program)
	}
	p := &Process{cmd: cmd, master: master, pgid: cmd.Process.Pid, grace: grace, done: make(chan struct{})}
	go p.wait()
	go func() {
		select {
		case <-ctx.Done():
			p.Stop()
		case <-p.done:
		}
	}()
	return p, nil
}

// openSlave unlocks the master, finds its slave and opens it, with the window size and, when noEcho is set, with the echo
// turned off.
func openSlave(master *os.File, rows, cols int, noEcho bool) (*os.File, error) {
	sc, err := master.SyscallConn()
	if err != nil {
		return nil, errors.NewErr("PtyOpenFailed", err).SetMetadata("step", "syscall conn")
	}
	var number int
	var ioErr error
	if err := sc.Control(func(fd uintptr) {
		f := int(fd)
		if ioErr = unix.IoctlSetPointerInt(f, unix.TIOCSPTLCK, 0); ioErr != nil {
			return
		}
		if number, ioErr = unix.IoctlGetInt(f, unix.TIOCGPTN); ioErr != nil {
			return
		}
		if ioErr = unix.IoctlSetWinsize(f, unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(rows), Col: uint16(cols)}); ioErr != nil {
			return
		}
		if !noEcho {
			return
		}
		var tio *unix.Termios
		if tio, ioErr = unix.IoctlGetTermios(f, unix.TCGETS); ioErr != nil {
			return
		}
		tio.Lflag &^= unix.ECHO
		ioErr = unix.IoctlSetTermios(f, unix.TCSETS, tio)
	}); err != nil {
		return nil, errors.NewErr("PtyOpenFailed", err).SetMetadata("step", "control")
	}
	if ioErr != nil {
		return nil, errors.NewErr("PtyOpenFailed", ioErr).SetMetadata("step", "ioctl")
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, errors.NewErr("PtyOpenFailed", err).SetMetadata("step", "open the slave", "number", number)
	}
	return slave, nil
}

// wait reaps the program, kills what it left in its group, and closes the channel done.
func (p *Process) wait() {
	_ = p.cmd.Wait() // the status is in ProcessState
	p.exit = p.cmd.ProcessState.ExitCode()
	_ = syscall.Kill(-p.pgid, syscall.SIGKILL) // whatever it started and left running; ESRCH when nothing is left
	close(p.done)
}

// Pid is the process ID of the program, which is also the ID of its process group and of its session.
func (p *Process) Pid() int { return p.pgid }

// Done is closed when the program and everything it started in its group are gone. The output it wrote can still be read
// after that, until Read reports io.EOF.
func (p *Process) Done() <-chan struct{} { return p.done }

// ExitCode is the exit status of the program (-1 when a signal ended it). It is valid once Done is closed.
func (p *Process) ExitCode() int { return p.exit }

// Read reads what the program writes to its terminal. It implements io.Reader and reports io.EOF when the terminal is
// closed: the program and everything it started in its session are gone (Linux then fails the read with EIO), or Close
// was called. Any other failure has the tag PtyReadFailed.
func (p *Process) Read(b []byte) (int, error) {
	n, err := p.master.Read(b)
	switch {
	case err == nil:
		return n, nil
	case errors.Is(err, syscall.EIO), errors.Is(err, os.ErrClosed), errors.Is(err, io.EOF):
		return n, io.EOF
	}
	return n, errors.NewErr("PtyReadFailed", err)
}

// Write sends bytes to the program, as typed on its terminal. It implements io.Writer. A failure has the tag
// PtyWriteFailed.
func (p *Process) Write(b []byte) (int, error) {
	n, err := p.master.Write(b)
	if err != nil {
		return n, errors.NewErr("PtyWriteFailed", err)
	}
	return n, nil
}

// Resize sets the size of the terminal in cells; the foreground group of the terminal gets SIGWINCH. A size that is not
// positive, or a failure, has the tag PtyResizeFailed.
func (p *Process) Resize(rows, cols int) error {
	if rows <= 0 || cols <= 0 || rows > 0xffff || cols > 0xffff {
		return errors.New("PtyResizeFailed", "the size of a terminal is positive", "rows", rows, "cols", cols)
	}
	sc, err := p.master.SyscallConn()
	if err != nil {
		return errors.NewErr("PtyResizeFailed", err).SetMetadata("step", "syscall conn")
	}
	var ioErr error
	if err := sc.Control(func(fd uintptr) {
		ioErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(rows), Col: uint16(cols)})
	}); err != nil {
		return errors.NewErr("PtyResizeFailed", err).SetMetadata("step", "control")
	}
	if ioErr != nil {
		return errors.NewErr("PtyResizeFailed", ioErr).SetMetadata("step", "ioctl")
	}
	return nil
}

// Signal sends a signal to the process group of the program. A group that is gone is not an error.
func (p *Process) Signal(sig syscall.Signal) {
	_ = syscall.Kill(-p.pgid, sig) // ESRCH when it ended already
}

// Stop ends the program: SIGTERM to its group, SIGKILL after the grace time. It returns at once.
func (p *Process) Stop() { p.end(syscall.SIGTERM) }

// Close ends the program the way closing the terminal of a shell does: SIGHUP to its group, SIGKILL after the grace
// time, and the terminal is closed, so that a Read that waits returns io.EOF. It returns at once and may be called more
// than once.
func (p *Process) Close() {
	p.end(syscall.SIGHUP)
	p.closeOnce.Do(func() { _ = p.master.Close() })
}

// end sends the signal to the group and kills the group when it is still there after the grace time.
func (p *Process) end(sig syscall.Signal) {
	select {
	case <-p.done:
		return
	default:
	}
	p.Signal(sig)
	go func() {
		select {
		case <-p.done:
		case <-time.After(p.grace):
			p.Signal(syscall.SIGKILL)
		}
	}()
}

// ForegroundPid is the process ID of the foreground process group of the terminal: the shell at its prompt, the program it
// runs while one runs (tcgetpgrp on the master). It fails with the tag PtyForegroundFailed when the terminal is closed.
func (p *Process) ForegroundPid() (int, error) {
	sc, err := p.master.SyscallConn()
	if err != nil {
		return 0, errors.NewErr("PtyForegroundFailed", err).SetMetadata("step", "syscall conn")
	}
	var pgrp int
	var ioErr error
	if err := sc.Control(func(fd uintptr) { pgrp, ioErr = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP) }); err != nil {
		return 0, errors.NewErr("PtyForegroundFailed", err).SetMetadata("step", "control")
	}
	if ioErr != nil {
		return 0, errors.NewErr("PtyForegroundFailed", ioErr).SetMetadata("step", "ioctl")
	}
	return pgrp, nil
}

// LineMode reads the ECHO and ICANON flags of the terminal's settings with one TCGETS. A shell with a line editor (bash's
// readline, zsh, fish) and a full-screen program (vim, less, htop) leave ICANON off and echo by themselves, whatever ECHO is. A
// program that asks for a password (getpass, sudo, ssh, passwd, read -s) turns ECHO off and keeps ICANON on. It fails with the
// tag PtyLineModeFailed when the terminal cannot be asked, because it is closed.
func (p *Process) LineMode() (echo, canonical bool, err error) {
	sc, err := p.master.SyscallConn()
	if err != nil {
		return false, false, errors.NewErr("PtyLineModeFailed", err).SetMetadata("step", "syscall conn")
	}
	var ioErr error
	if err := sc.Control(func(fd uintptr) {
		var t *unix.Termios
		t, ioErr = unix.IoctlGetTermios(int(fd), unix.TCGETS)
		if ioErr == nil {
			echo, canonical = t.Lflag&unix.ECHO != 0, t.Lflag&unix.ICANON != 0
		}
	}); err != nil {
		return false, false, errors.NewErr("PtyLineModeFailed", err).SetMetadata("step", "control")
	}
	if ioErr != nil {
		return false, false, errors.NewErr("PtyLineModeFailed", ioErr).SetMetadata("step", "ioctl")
	}
	return echo, canonical, nil
}
