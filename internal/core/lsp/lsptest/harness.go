package lsptest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/errors"
)

// Timeout bounds every wait of the harness, so that a test that hangs fails instead.
const Timeout = 20 * time.Second

// Options say how the harness starts a server and its client.
type Options struct {
	// Script is what the fake server does.
	Script Script
	// Client are the options of the lsp.Client; the harness sets nothing in them.
	Client lsp.ClientOptions
	// Program is the fake server program for StartProcess (BuildProgram); empty builds it.
	Program string
}

// Harness is a client connected to a fake server.
type Harness struct {
	t testing.TB
	// Client is the initialized client.
	Client *lsp.Client
	// Server is the fake server of an in-process harness; nil for a process.
	Server *Server

	proc *process
}

// pipeEnd is one end of a pair of in-memory pipes.
type pipeEnd struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p pipeEnd) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipeEnd) Write(b []byte) (int, error) { return p.w.Write(b) }

// Close closes both directions of the end: the other end reads end of file and its writes fail.
func (p pipeEnd) Close() error {
	werr := p.w.Close()
	rerr := p.r.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

// Pipe makes the two ends of a connection in memory. A write blocks until the other end reads it, as a
// full pipe does.
func Pipe() (client, server io.ReadWriteCloser) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	return pipeEnd{r: cr, w: cw}, pipeEnd{r: sr, w: sw}
}

// Connect starts a fake server in this process and returns the client's end of the connection, without a
// client: for tests of the handshake itself. The server stops with the test.
func Connect(t testing.TB, script Script) (io.ReadWriteCloser, *Server) {
	t.Helper()
	clientEnd, serverEnd := Pipe()
	srv := NewServer(script, func() {
		if err := serverEnd.Close(); err != nil {
			t.Logf("closing the server end: %v", err)
		}
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(serverEnd); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			t.Logf("the fake server stopped: %v", err)
		}
	}()
	t.Cleanup(func() {
		if err := clientEnd.Close(); err != nil {
			t.Logf("closing the client end: %v", err)
		}
		if err := serverEnd.Close(); err != nil {
			t.Logf("closing the server end: %v", err)
		}
		select {
		case <-done:
		case <-time.After(Timeout):
			t.Errorf("the fake server did not stop")
		}
	})
	return clientEnd, srv
}

// Start connects a client to a fake server in this process and performs the handshake. The client is closed
// when the test ends.
func Start(t testing.TB, o Options) *Harness {
	t.Helper()
	rwc, srv := Connect(t, o.Script)
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	c, err := lsp.NewClient(ctx, rwc, o.Client)
	if err != nil {
		t.Fatalf("the handshake with the fake server failed: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Logf("closing the client: %v", err)
		}
		c.Wait()
	})
	return &Harness{t: t, Client: c, Server: srv}
}

// BuildProgram builds the fake server program into a folder of the test and returns its path. Building
// again is quick: the build cache of Go holds the compiled packages.
func BuildProgram(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot find the source of lsptest")
	}
	out := filepath.Join(t.TempDir(), "lsptest-fakeserver")
	cmd := exec.Command("go", "build", "-o", out, "./fakeserver")
	cmd.Dir = filepath.Dir(file)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the fake server: %v\n%s", err, msg)
	}
	return out
}

// process is the fake server program running in its own process group.
type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *syncBuffer
	exited chan struct{}
	code   int
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (p *process) Read(b []byte) (int, error)  { return p.stdout.Read(b) }
func (p *process) Write(b []byte) (int, error) { return p.stdin.Write(b) }

// Close closes the standard input of the program, which makes it end, and its output.
func (p *process) Close() error {
	err := p.stdin.Close()
	if oerr := p.stdout.Close(); err == nil {
		err = oerr
	}
	return err
}

// StartProcess starts the fake server program in its own process group, connects a client to it and
// performs the handshake. When the test ends the client is closed and the whole group is killed.
func StartProcess(t testing.TB, o Options) *Harness {
	t.Helper()
	prog := o.Program
	if prog == "" {
		prog = BuildProgram(t)
	}
	script, err := json.Marshal(o.Script)
	if err != nil {
		t.Fatalf("encoding the script: %v", err)
	}
	scriptPath := filepath.Join(t.TempDir(), "script.json")
	if err := os.WriteFile(scriptPath, script, 0o600); err != nil {
		t.Fatalf("writing the script: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*Timeout)
	cmd := exec.CommandContext(ctx, prog, "-script", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	p := &process{cmd: cmd, stderr: &syncBuffer{}, exited: make(chan struct{})}
	cmd.Stderr = p.stderr
	if p.stdin, err = cmd.StdinPipe(); err != nil {
		cancel()
		t.Fatalf("stdin of the fake server: %v", err)
	}
	// Not StdoutPipe: Wait closes that pipe when the program ends, and the client may not have read the
	// last answer yet. This pipe ends with end of file when the program is gone and the client has read
	// everything.
	outR, outW, err := os.Pipe()
	if err != nil {
		cancel()
		t.Fatalf("stdout of the fake server: %v", err)
	}
	p.stdout = outR
	cmd.Stdout = outW
	err = cmd.Start()
	if cerr := outW.Close(); cerr != nil {
		t.Logf("closing the write end of the output pipe: %v", cerr)
	}
	if err != nil {
		cancel()
		t.Fatalf("starting the fake server: %v", err)
	}
	go func() {
		defer close(p.exited)
		if err := cmd.Wait(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				p.code = ee.ExitCode()
				return
			}
			p.code = -1
		}
	}()
	t.Cleanup(func() {
		if cmd.Process != nil {
			if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				t.Logf("killing the fake server: %v", err)
			}
		}
		select {
		case <-p.exited:
		case <-time.After(Timeout):
			t.Errorf("the fake server process did not end")
		}
		cancel()
		if s := p.stderr.String(); s != "" && testing.Verbose() {
			t.Logf("stderr of the fake server:\n%s", s)
		}
	})

	hctx, hcancel := context.WithTimeout(context.Background(), Timeout)
	defer hcancel()
	c, err := lsp.NewClient(hctx, p, o.Client)
	if err != nil {
		t.Fatalf("the handshake with the fake server process failed: %v\nstderr: %s", err, p.stderr.String())
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Logf("closing the client: %v", err)
		}
		c.Wait()
	})
	return &Harness{t: t, Client: c, proc: p}
}

// WaitExit waits for the fake server program to end and returns its exit status; it fails the test when it
// is still running after Timeout.
func (h *Harness) WaitExit() int {
	h.t.Helper()
	if h.proc == nil {
		h.t.Fatal("the harness has no process")
	}
	select {
	case <-h.proc.exited:
		return h.proc.code
	case <-time.After(Timeout):
		h.t.Fatal("the fake server process did not end")
		return 0
	}
}

// Pid is the process id of the fake server program; zero for a harness in this process.
func (h *Harness) Pid() int {
	if h.proc == nil || h.proc.cmd.Process == nil {
		return 0
	}
	return h.proc.cmd.Process.Pid
}

// Stderr is what the fake server program wrote to its standard error so far.
func (h *Harness) Stderr() string {
	if h.proc == nil {
		return ""
	}
	return h.proc.stderr.String()
}

// call asks the fake server something through the protocol, which works for a process too.
func call[R any](h *Harness, method string, params any) R {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	r, err := lsp.Request[R](ctx, h.Client, method, params)
	if err != nil {
		h.t.Fatalf("%s: %v", method, err)
	}
	return r
}

// Document is the fake server's copy of a document. The request is queued behind everything the client
// sent before, and the server handles the notifications in order, so the copy includes all of it.
func (h *Harness) Document(uri string) Doc {
	h.t.Helper()
	return call[Doc](h, MethodDocument, map[string]string{"uri": uri})
}

// Traffic is the messages the fake server recorded.
func (h *Harness) Traffic() []Message {
	h.t.Helper()
	return call[[]Message](h, MethodTraffic, nil)
}

// Violations are the protocol violations the fake server noticed.
func (h *Harness) Violations() []string {
	h.t.Helper()
	return call[[]string](h, MethodViolations, nil)
}
