package claude

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
)

// process is the program, running in a process group of its own (the program leads it), so that it and everything it
// started can be stopped together and none outlives the session.
type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *os.File
	stderr *os.File
	pgid   int
	exited chan struct{}

	mu   sync.Mutex
	code int
}

// startProcess starts the program with the arguments in the folder with the environment. Its standard output and error
// are pipes this package reads; they end when the program and the processes of its group are gone. It fails with the tag
// ProcessStartFailed.
func startProcess(path string, args []string, dir string, env []string, killGrace time.Duration) (*process, error) {
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.NewErr("ProcessStartFailed", err).SetMetadata("program", path)
	}
	// Not StdoutPipe: Wait closes that pipe when the program ends, and the reader may not have read the last line yet.
	// These pipes end with end of file when the group is gone and everything was read.
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, errors.NewErr("ProcessStartFailed", err).SetMetadata("program", path)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		closeFiles(outR, outW)
		return nil, errors.NewErr("ProcessStartFailed", err).SetMetadata("program", path)
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	if err := cmd.Start(); err != nil {
		closeFiles(outR, outW, errR, errW)
		return nil, errors.NewErr("ProcessStartFailed", err).SetMetadata("program", path, "dir", dir)
	}
	closeFiles(outW, errW) // the child has its own copies
	p := &process{cmd: cmd, stdin: stdin, stdout: outR, stderr: errR, pgid: cmd.Process.Pid, exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait() // the exit status is read from ProcessState
		// whatever the program left behind in its group goes with it
		p.killGroup(syscall.SIGKILL)
		p.mu.Lock()
		p.code = cmd.ProcessState.ExitCode()
		p.mu.Unlock()
		close(p.exited)
		// a process that left the group may still hold the pipes: do not wait for it forever
		time.AfterFunc(killGrace, func() { closeFiles(outR, errR) })
	}()
	return p, nil
}

// closeFiles closes files whose close cannot fail in a way that matters: pipe ends of a process that is gone.
func closeFiles(files ...*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}

// killGroup sends the signal to the whole process group.
func (p *process) killGroup(sig syscall.Signal) {
	_ = syscall.Kill(-p.pgid, sig) // ESRCH: the group is already gone
}

// exitCode is the exit status of the program, -1 when a signal ended it. It is valid after exited is closed.
func (p *process) exitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code
}

// terminate asks the group to end and kills it when it has not ended after the grace.
func (p *process) terminate(grace time.Duration) {
	p.killGroup(syscall.SIGTERM)
	select {
	case <-p.exited:
	case <-time.After(grace):
		p.killGroup(syscall.SIGKILL)
	}
}
