package agents

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// ptyProcess is a program that runs with a pseudo-terminal as its terminal, in a session and a process group of its own. It
// is how the login of the claude program runs (SPEC §10.16): the program's own flow, which opens a browser or waits for a code
// to be pasted, needs a terminal. The pseudo-terminal is made with /dev/ptmx and golang.org/x/sys/unix, so Emetos stays
// cgo-free.
type ptyProcess struct {
	cmd    *exec.Cmd
	master *os.File
	pgid   int
	// done is closed when the program and its group are gone and the terminal is closed.
	done chan struct{}
	// exit is the exit status once done is closed (-1 for a signal).
	exit      int
	closeOnce sync.Once
}

// ptyGrace is how long a program has to end after SIGTERM before its group is killed.
const ptyGrace = 2 * time.Second

// startPty starts the program with the pseudo-terminal as its standard input, output and error and as its controlling
// terminal. The terminal does not echo what is written to it (a code that is pasted is not shown again). When ctx ends, the
// group gets SIGTERM and, after ptyGrace, SIGKILL. It fails with the tags PtyOpenFailed and ProcessStartFailed.
func startPty(ctx context.Context, program string, args []string, dir string, env []string) (*ptyProcess, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, errors.NewErr("PtyOpenFailed", err).SetMetadata("step", "open /dev/ptmx")
	}
	slave, err := openSlave(master)
	if err != nil {
		_ = master.Close() // already failing
		return nil, err
	}
	defer func() { _ = slave.Close() }() // the child has its own copies
	cmd := exec.Command(program, args...)
	cmd.Dir, cmd.Env = dir, env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = master.Close() // already failing
		return nil, errors.NewErr("ProcessStartFailed", err).SetMetadata("program", program)
	}
	p := &ptyProcess{cmd: cmd, master: master, pgid: cmd.Process.Pid, done: make(chan struct{})}
	go p.wait()
	go func() {
		select {
		case <-ctx.Done():
			p.stop()
		case <-p.done:
		}
	}()
	return p, nil
}

// openSlave unlocks the master, finds its slave and opens it. The terminal is set to a window of 24 rows and 100 columns and
// not to echo.
func openSlave(master *os.File) (*os.File, error) {
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
		if ioErr = unix.IoctlSetWinsize(f, unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 100}); ioErr != nil {
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

// wait reaps the program, kills what it left in its group, and closes the terminal.
func (p *ptyProcess) wait() {
	_ = p.cmd.Wait() // the status is in ProcessState
	p.exit = p.cmd.ProcessState.ExitCode()
	_ = syscall.Kill(-p.pgid, syscall.SIGKILL) // whatever it started and left running; ESRCH when nothing is left
	close(p.done)
}

// pump reads what the program writes to its terminal and gives it to fn, until the terminal is closed (the program and
// everything it started are gone: Linux then fails the read), and closes the master. It must be called once.
func (p *ptyProcess) pump(fn func([]byte)) {
	defer p.closeOnce.Do(func() { _ = p.master.Close() })
	buf := make([]byte, 4096)
	for {
		n, err := p.master.Read(buf)
		if n > 0 {
			fn(append([]byte(nil), buf[:n]...))
		}
		if err != nil {
			return // EIO when the other side is closed: the end of the output
		}
	}
}

// stop ends the program: SIGTERM to its group, SIGKILL after ptyGrace. It returns at once.
func (p *ptyProcess) stop() {
	select {
	case <-p.done:
		return
	default:
	}
	_ = syscall.Kill(-p.pgid, syscall.SIGTERM) // ESRCH when it ended already
	go func() {
		select {
		case <-p.done:
		case <-time.After(ptyGrace):
			_ = syscall.Kill(-p.pgid, syscall.SIGKILL)
		}
	}()
}

// write sends bytes to the program, as typed on its terminal.
func (p *ptyProcess) write(b []byte) error {
	if _, err := p.master.Write(b); err != nil {
		return errors.NewErr("PtyWriteFailed", err)
	}
	return nil
}
