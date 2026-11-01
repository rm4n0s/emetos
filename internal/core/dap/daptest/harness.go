package daptest

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap"
)

// Timeout bounds every wait of the harness, so that a test that hangs fails instead.
const Timeout = 20 * time.Second

// ProgramEnv is the environment variable in which Main leaves the path of the fake adapter program for Program.
const ProgramEnv = "EMETOS_DAPTEST_PROGRAM"

// Transport is how the harness reaches the fake adapter.
type Transport int

const (
	// InProcess runs the adapter in the test's process over an in-memory pipe: fast, and the race detector sees both sides.
	InProcess Transport = iota
	// Stdio starts the program and talks on its standard input and output (dap.AdapterExecutable).
	Stdio
	// TCP starts the program listening on a port and connects to it (dap.AdapterServer).
	TCP
	// Unix starts the program listening on a Unix socket and connects to it (dap.AdapterPipe).
	Unix
	// Announce starts the program, reads the port it prints and connects (dap.AdapterExecutableServer).
	Announce
)

// Options say how the harness starts the adapter and its client.
type Options struct {
	// Script is what the adapter does.
	Script Script
	// Transport is how the client reaches it; the zero value is InProcess.
	Transport Transport
	// Client are the options of the dap.Client.
	Client dap.ClientOptions
	// Program is the fake adapter program; empty uses Program(t).
	Program string
	// AnnounceFormat is the line an Announce program prints, with %d for the port; empty is the program's default.
	AnnounceFormat string
	// PortPattern is the dap.ServerSpec pattern of an Announce transport; empty is the default one.
	PortPattern string
}

// Harness is a client connected to a fake adapter.
type Harness struct {
	t testing.TB
	// Client is the client; the harness has not sent anything, so the test starts with initialize.
	Client *dap.Client
	// Server is the adapter of an in-process harness; nil for a program.
	Server *Server

	mu     sync.Mutex
	stderr []string
	progs  []program // the programs behind the client; the first is the adapter
}

// program is a running adapter program: a *dap.Process or a *dap.ServerProcess.
type program interface {
	Done() <-chan struct{}
	ExitCode() int
	PID() int
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

// Pipe makes the two ends of a connection in memory. A write blocks until the other end reads it, as a full pipe does.
func Pipe() (client, server io.ReadWriteCloser) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	return pipeEnd{r: cr, w: cw}, pipeEnd{r: sr, w: sw}
}

// Start connects a client to a fake adapter. For InProcess the adapter runs in this process; for the other transports it is
// the program, started in its own process group, which is stopped when the test ends (the harness checks that no process of its
// group is left). The client is closed when the test ends.
func Start(t testing.TB, o Options) *Harness {
	t.Helper()
	h := &Harness{t: t}
	var c *dap.Client
	switch o.Transport {
	case InProcess:
		clientEnd, serverEnd := Pipe()
		h.Server = NewServer(o.Script, ServerOptions{Stderr: io.Discard})
		done := make(chan struct{})
		go func() {
			defer close(done)
			if err := h.Server.Serve(serverEnd); err != nil {
				t.Logf("the fake adapter stopped: %v", err)
			}
		}()
		c = dap.NewClient(clientEnd, o.Client)
		t.Cleanup(func() {
			if err := c.Close(); err != nil {
				t.Logf("closing the client: %v", err)
			}
			c.Wait()
			select {
			case <-done:
			case <-time.After(Timeout):
				t.Errorf("the fake adapter did not stop")
			}
		})
	default:
		c = h.startProgram(o)
	}
	h.Client = c
	return h
}

// startProgram starts the program for a transport and connects to it.
func (h *Harness) startProgram(o Options) *dap.Client {
	t := h.t
	t.Helper()
	prog := o.Program
	if prog == "" {
		prog = Program(t)
	}
	scriptPath := filepath.Join(t.TempDir(), "script.jsonl")
	lines, err := o.Script.Lines()
	if err != nil {
		t.Fatalf("encoding the script: %v", err)
	}
	if err := os.WriteFile(scriptPath, lines, 0o600); err != nil {
		t.Fatalf("writing the script: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	spec := dap.ProcessSpec{Program: prog, Args: []string{"-script", scriptPath}, Stderr: h.addStderr}
	var c *dap.Client
	switch o.Transport {
	case Stdio:
		c, err = dap.Connect(ctx, dap.Adapter{Kind: dap.AdapterExecutable, Process: spec}, o.Client)
	case Announce:
		if o.AnnounceFormat != "" {
			spec.Args = append(spec.Args, "-announce-format", o.AnnounceFormat)
		}
		spec.Args = append(spec.Args, "-listen", "tcp", "-announce")
		c, err = dap.Connect(ctx, dap.Adapter{Kind: dap.AdapterExecutableServer, Process: spec, Server: dap.ServerSpec{PortPattern: o.PortPattern, Timeout: Timeout}}, o.Client)
	case TCP, Unix:
		c = h.connectListening(ctx, spec, o)
	default:
		t.Fatalf("unknown transport %d", o.Transport)
	}
	if err != nil {
		t.Fatalf("starting the fake adapter: %v\nstderr: %v", err, h.Stderr())
	}
	if p, ok := c.Transport().(program); ok {
		h.progs = append(h.progs, p)
	}
	t.Cleanup(func() { h.stop(c) })
	return c
}

// connectListening starts a program that listens, and connects to it by TCP or by the Unix socket. It fails the test when it
// cannot.
func (h *Harness) connectListening(ctx context.Context, spec dap.ProcessSpec, o Options) *dap.Client {
	t := h.t
	t.Helper()
	tcp := o.Transport == TCP
	port := make(chan int, 1)
	var sock string
	if tcp {
		spec.Args = append(spec.Args, "-listen", "tcp", "-announce")
		re := regexp.MustCompile(`:(\d+)\s*$`)
		spec.Stdout = func(line string) {
			if m := re.FindStringSubmatch(line); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil {
					select {
					case port <- n:
					default:
					}
				}
			}
		}
	} else {
		sock = filepath.Join(t.TempDir(), "dap.sock")
		spec.Args = append(spec.Args, "-listen", "unix", "-unix", sock)
		spec.Stdout = func(string) {}
	}
	p, err := dap.StartProcess(ctx, spec)
	if err != nil {
		t.Fatalf("starting the fake adapter: %v", err)
	}
	h.progs = append(h.progs, p)
	dctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	if tcp {
		select {
		case n := <-port:
			c, err := dap.Connect(dctx, dap.Adapter{Kind: dap.AdapterServer, Port: n}, o.Client)
			if err != nil {
				t.Fatalf("connecting to the fake adapter: %v", err)
			}
			return c
		case <-p.Done():
			t.Fatalf("the fake adapter ended (status %d) before it printed its port; stderr: %v", p.ExitCode(), h.Stderr())
		case <-dctx.Done():
			t.Fatalf("the fake adapter printed no port; stderr: %v", h.Stderr())
		}
	}
	for {
		c, err := dap.Connect(dctx, dap.Adapter{Kind: dap.AdapterPipe, Path: sock}, o.Client)
		if err == nil {
			return c
		}
		select {
		case <-p.Done():
			t.Fatalf("the fake adapter ended (status %d) before it listened; stderr: %v", p.ExitCode(), h.Stderr())
		case <-dctx.Done():
			t.Fatalf("connecting to the fake adapter: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// stop closes the client, waits for the program to end and checks that nothing of its process group is left.
func (h *Harness) stop(c *dap.Client) {
	t := h.t
	if err := c.Close(); err != nil {
		t.Logf("closing the client: %v", err)
	}
	c.Wait()
	for _, p := range h.progs {
		if closer, ok := p.(io.Closer); ok {
			_ = closer.Close() // the program of a TCP or Unix harness is not the client's transport
		}
		select {
		case <-p.Done():
		case <-time.After(Timeout):
			t.Errorf("the fake adapter program did not end")
		}
		// The group is killed when the program is reaped; a signal 0 tells whether any member is left.
		deadline := time.Now().Add(2 * time.Second)
		for syscall.Kill(-p.PID(), 0) == nil && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if syscall.Kill(-p.PID(), 0) == nil {
			t.Errorf("process group %d still has members", p.PID())
			_ = syscall.Kill(-p.PID(), syscall.SIGKILL)
		}
	}
	if s := h.Stderr(); len(s) > 0 && testing.Verbose() {
		t.Logf("stderr of the fake adapter: %v", s)
	}
}

func (h *Harness) addStderr(line string) {
	h.mu.Lock()
	h.stderr = append(h.stderr, line)
	h.mu.Unlock()
}

// Stderr is the lines the fake adapter program wrote to its standard error so far (including its violation notes).
func (h *Harness) Stderr() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.stderr...)
}

// Exited is closed when the fake adapter program has ended; nil for an in-process harness.
func (h *Harness) Exited() <-chan struct{} {
	if len(h.progs) == 0 {
		return nil
	}
	return h.progs[0].Done()
}

// WaitExit waits for the fake adapter program to end and returns its exit status; it fails the test when it is still running
// after Timeout.
func (h *Harness) WaitExit() int {
	h.t.Helper()
	if len(h.progs) == 0 {
		h.t.Fatal("the harness has no process")
	}
	select {
	case <-h.progs[0].Done():
		return h.progs[0].ExitCode()
	case <-time.After(Timeout):
		h.t.Fatal("the fake adapter program did not end")
		return 0
	}
}

// PID is the process id (and process group) of the fake adapter program; zero for an in-process harness.
func (h *Harness) PID() int {
	if len(h.progs) == 0 {
		return 0
	}
	return h.progs[0].PID()
}

// Traffic is the messages the fake adapter recorded, asked through the protocol so that it works for a program too. The request is
// queued behind everything the client sent before.
func (h *Harness) Traffic() []Message {
	h.t.Helper()
	return ask[[]Message](h, MethodTraffic)
}

// Violations are the protocol violations the fake adapter noticed.
func (h *Harness) Violations() []string {
	h.t.Helper()
	return ask[[]string](h, MethodViolations)
}

// Sent is how many events of a burst the fake adapter has written.
func (h *Harness) Sent() int {
	h.t.Helper()
	return ask[int](h, MethodSent)
}

func ask[R any](h *Harness, method string) R {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	r, err := dap.Call[R](ctx, h.Client, method, nil)
	if err != nil {
		h.t.Fatalf("%s: %v", method, err)
	}
	return r
}

// Requests returns the arguments of the requests the fake adapter got for command, in order.
func (h *Harness) Requests(command string) []json.RawMessage {
	h.t.Helper()
	var out []json.RawMessage
	for _, m := range h.Traffic() {
		if m.Direction == "in" && m.Type == dap.TypeRequest && m.Name == command {
			out = append(out, m.Body)
		}
	}
	return out
}

// Main builds the fake adapter program into a folder of its own, puts its path in the environment (ProgramEnv), runs the tests
// and removes the folder. A package whose tests use a program calls it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(daptest.Main(m)) }
func Main(m *testing.M) int {
	dir, err := os.MkdirTemp("", "daptest-")
	if err != nil {
		_, _ = os.Stderr.WriteString("daptest: " + err.Error() + "\n")
		return 1
	}
	code := runMain(m, dir)
	if err := os.RemoveAll(dir); err != nil {
		_, _ = os.Stderr.WriteString("daptest: " + err.Error() + "\n")
	}
	return code
}

func runMain(m *testing.M, dir string) int {
	out := filepath.Join(dir, "daptest-fakeadapter")
	if problem, ok := build(out); !ok {
		_, _ = os.Stderr.WriteString("daptest: building the fake adapter: " + problem + "\n")
		return 1
	}
	if err := os.Setenv(ProgramEnv, out); err != nil {
		_, _ = os.Stderr.WriteString("daptest: " + err.Error() + "\n")
		return 1
	}
	return m.Run()
}

// build compiles the program to out; the build cache of Go makes it quick after the first time. When it fails it says why.
func build(out string) (problem string, ok bool) {
	_, file, _, found := runtime.Caller(0)
	if !found {
		return "cannot find the source of daptest", false
	}
	cmd := exec.Command("go", "build", "-o", out, "./fakeadapter")
	cmd.Dir = filepath.Dir(file)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := cmd.CombinedOutput(); err != nil {
		return err.Error() + "\n" + string(msg), false
	}
	return "", true
}

// Program is the path of the fake adapter program: the one Main built, or, when the package has no TestMain that calls Main, one
// built into the folder of the test.
func Program(t testing.TB) string {
	t.Helper()
	if p := os.Getenv(ProgramEnv); p != "" {
		return p
	}
	out := filepath.Join(t.TempDir(), "daptest-fakeadapter")
	if problem, ok := build(out); !ok {
		t.Fatalf("building the fake adapter: %s", problem)
	}
	return out
}
