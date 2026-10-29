package containerapi

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
)

// ServiceOptions say how StartService runs "podman system service".
type ServiceOptions struct {
	// Program is the podman program (found on PATH when it has no slash); empty is "podman".
	Program string
	// Getenv reads the variables PATH and XDG_RUNTIME_DIR; nil is os.Getenv.
	Getenv func(string) string
	// Env is the whole environment of the service; nil is the environment of this process.
	Env []string
	// SocketDir is the folder of the socket; empty is $XDG_RUNTIME_DIR/emetos, and a folder made in the temporary folder
	// when there is no XDG_RUNTIME_DIR.
	SocketDir string
	// PID names the socket (podman-<PID>.sock); 0 is the process id of this process.
	PID int
	// ReadyTimeout is how long to wait for the socket to answer; 0 is 15 seconds.
	ReadyTimeout time.Duration
	// Grace is how long Stop waits after SIGTERM before it kills the process group; 0 is five seconds.
	Grace time.Duration
	// Client tunes the Client that probes the socket.
	Client Options
}

// Service is a "podman system service" that Emetos started on a socket of its own (SPEC §10.20 "Which engine"), so that
// nothing has to be enabled in systemd. It runs as a child in its own process group and ends with Stop, or when this
// process dies (the child gets SIGTERM then).
type Service struct {
	// Socket is the path of the Unix socket the service listens on.
	Socket string

	cmd     *exec.Cmd
	pgid    int
	grace   time.Duration
	exited  chan struct{}
	release chan struct{}
	stderr  *tailBuffer
	once    sync.Once
}

// tailBuffer keeps the last bytes written to it, for the message of a service that failed to start.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

const tailSize = 4 << 10

// Write keeps the last tailSize bytes.
func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailSize {
		t.buf = t.buf[len(t.buf)-tailSize:]
	}
	return len(p), nil
}

// String returns the text kept.
func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// lookPath finds a program in the directories of path (a PATH value); a program with a slash is taken as it is.
func lookPath(program, path string) (string, bool) {
	isExec := func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
	}
	if strings.Contains(program, "/") {
		return program, isExec(program)
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, program); isExec(p) {
			return p, true
		}
	}
	return "", false
}

// StartService starts "podman system service --time=0 unix://<socket>" and waits until the socket answers. The service is a
// child in a process group of its own. The errors have the tags ContainerAPIPodmanNotFound (no podman program),
// ContainerAPIPodmanStartFailed (it did not start, or ended at once; the message holds the end of its error output) and
// ContainerAPIPodmanTimeout (the socket did not answer in time).
func StartService(ctx context.Context, opts ServiceOptions) (*Service, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	program := opts.Program
	if program == "" {
		program = "podman"
	}
	path, ok := lookPath(program, getenv("PATH"))
	if !ok {
		return nil, errors.New("ContainerAPIPodmanNotFound", "the program "+program+" is not on PATH", "program", program)
	}
	dir := opts.SocketDir
	if dir == "" {
		if rt := getenv("XDG_RUNTIME_DIR"); rt != "" {
			dir = filepath.Join(rt, "emetos")
		} else {
			made, err := os.MkdirTemp("", "emetos-podman-")
			if err != nil {
				return nil, errors.NewErr("ContainerAPIPodmanStartFailed", err).SetMetadata("step", "socket folder")
			}
			dir = made
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, errors.NewErr("ContainerAPIPodmanStartFailed", err).SetMetadata("step", "socket folder", "dir", dir)
	}
	pid := opts.PID
	if pid == 0 {
		pid = os.Getpid()
	}
	socket := filepath.Join(dir, "podman-"+strconv.Itoa(pid)+".sock")
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return nil, errors.NewErr("ContainerAPIPodmanStartFailed", err).SetMetadata("step", "old socket", "socket", socket)
	}

	svc := &Service{Socket: socket, grace: opts.Grace, exited: make(chan struct{}), release: make(chan struct{}), stderr: &tailBuffer{}}
	if svc.grace <= 0 {
		svc.grace = 5 * time.Second
	}
	svc.cmd = exec.Command(path, "system", "service", "--time=0", "unix://"+socket) //nolint:gosec // the program is the user's podman
	svc.cmd.Env = opts.Env
	svc.cmd.Stderr = svc.stderr
	svc.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}

	// Pdeathsig fires when the thread that started the child ends, so a goroutine that holds its thread starts it and
	// lives until Stop: the thread ends with the goroutine (it is never unlocked), and so does the child if this process
	// is gone before Stop.
	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		started <- svc.cmd.Start()
		<-svc.release
	}()
	if err := <-started; err != nil {
		close(svc.release)
		return nil, errors.NewErr("ContainerAPIPodmanStartFailed", err).SetMetadata("program", path)
	}
	svc.pgid = svc.cmd.Process.Pid
	go func() {
		_ = svc.cmd.Wait() // the exit status is of no use: a service that ends is reported by the probe below, or by its next call
		close(svc.exited)
	}()

	timeout := opts.ReadyTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		_, err := New(KindPodman, UnixEndpoint(socket), opts.Client).Negotiate(probeCtx)
		cancel()
		if err == nil {
			return svc, nil
		}
		select {
		case <-svc.exited:
			svc.Stop()
			return nil, errors.New("ContainerAPIPodmanStartFailed", "podman system service ended before its socket answered: "+svc.stderr.String(), "program", path, "socket", socket)
		case <-ctx.Done():
			svc.Stop()
			return nil, errors.NewErr("ContainerAPIPodmanStartFailed", ctx.Err()).SetMetadata("program", path, "socket", socket)
		case <-deadline.C:
			svc.Stop()
			return nil, errors.New("ContainerAPIPodmanTimeout", "podman system service did not answer on its socket within "+timeout.String()+": "+svc.stderr.String(), "program", path, "socket", socket)
		case <-tick.C:
		}
	}
}

// Stop ends the service: SIGTERM to its process group, SIGKILL after the grace time, and the socket is removed. It is safe
// to call more than once and from several goroutines.
func (s *Service) Stop() {
	s.once.Do(func() {
		// an error of Kill says only that the group is gone already (ESRCH), which is what is wanted
		_ = syscall.Kill(-s.pgid, syscall.SIGTERM)
		select {
		case <-s.exited:
		case <-time.After(s.grace):
			_ = syscall.Kill(-s.pgid, syscall.SIGKILL)
			select {
			case <-s.exited:
			case <-time.After(5 * time.Second): // a process that a kill cannot end is beyond what Stop can do
			}
		}
		_ = os.Remove(s.Socket) // a socket the service removed itself is not there
		close(s.release)
	})
}

// Done is closed when the service has ended, whether by Stop or by itself.
func (s *Service) Done() <-chan struct{} { return s.exited }
