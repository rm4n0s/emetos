package dap_test

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/errors"
)

// lines collects the lines a program prints.
type lines struct {
	mu   sync.Mutex
	list []string
}

func (l *lines) add(s string) {
	l.mu.Lock()
	l.list = append(l.list, s)
	l.mu.Unlock()
}

func (l *lines) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.list...)
}

// waitFor waits until cond holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func sh(script string) dap.ProcessSpec {
	return dap.ProcessSpec{Program: "sh", Args: []string{"-c", script}}
}

// gone reports whether the process no longer exists.
func gone(pid int) bool { return syscall.Kill(pid, 0) == syscall.ESRCH }

// stopAtEnd makes sure a process of a test is gone when the test ends.
func stopAtEnd(t *testing.T, p *dap.Process) {
	t.Helper()
	t.Cleanup(func() {
		p.Stop(0)
		// a member killed just now may be a zombie for a moment, until init reaps it
		for deadline := time.Now().Add(2 * time.Second); syscall.Kill(-p.PID(), 0) == nil && time.Now().Before(deadline); {
			time.Sleep(10 * time.Millisecond)
		}
		if syscall.Kill(-p.PID(), 0) == nil {
			t.Errorf("process group %d still has members", p.PID())
		}
	})
}

func TestStartProcessFailures(t *testing.T) {
	tests := []struct {
		name  string
		spec  dap.ProcessSpec
		route string
	}{
		{"a program that does not exist", dap.ProcessSpec{Program: "emetos-no-such-adapter"}, "StartProcess.DapAdapterNotFound"},
		{"a folder that does not exist", dap.ProcessSpec{Program: "sh", Args: []string{"-c", "true"}, Dir: "/emetos/no/such/folder"}, "StartProcess.DapAdapterStartFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := dap.StartProcess(ctxT(t), tt.spec)
			if err == nil {
				p.Stop(0)
				t.Fatal("no error")
			}
			appErr := wantRouteError(t, err, tt.route)
			if appErr.Metadata["program"] == nil {
				t.Errorf("metadata = %v", appErr.Metadata)
			}
		})
	}
}

func TestProcessLinesAndEnvironment(t *testing.T) {
	var out, errs lines
	spec := sh(`echo "a:$EMETOS_DAP_TEST" ; echo oops >&2; pwd; echo last-without-newline >&2 | cat; printf tail >&2`)
	spec.Env = []string{"EMETOS_DAP_TEST=yes"}
	spec.Dir = t.TempDir()
	spec.Stdout, spec.Stderr = out.add, errs.add
	p, err := dap.StartProcess(ctxT(t), spec)
	if err != nil {
		t.Fatal(err)
	}
	stopAtEnd(t, p)
	select {
	case <-p.Done():
	case <-time.After(wait):
		t.Fatal("the program did not end")
	}
	if p.ExitCode() != 0 {
		t.Errorf("exit status %d", p.ExitCode())
	}
	gotOut, gotErr := out.get(), errs.get()
	if len(gotOut) != 2 || gotOut[0] != "a:yes" || !strings.HasSuffix(gotOut[1], filepath.Base(spec.Dir)) {
		t.Errorf("stdout lines = %q", gotOut)
	}
	if strings.Join(gotErr, "|") != "oops|last-without-newline|tail" {
		t.Errorf("stderr lines = %q (the last has no line break)", gotErr)
	}
	var b [8]byte
	if n, err := p.Read(b[:]); n != 0 || err == nil {
		t.Errorf("Read of a program whose output went to a callback = %d, %v, want the end of file", n, err)
	}
}

func TestProcessLongLinesAreCut(t *testing.T) {
	var errs lines
	spec := sh(`head -c 200000 /dev/zero | tr '\0' x >&2; echo >&2; echo short >&2`)
	spec.Stderr = errs.add
	p, err := dap.StartProcess(ctxT(t), spec)
	if err != nil {
		t.Fatal(err)
	}
	stopAtEnd(t, p)
	<-p.Done()
	got := errs.get()
	if len(got) != 2 || len(got[0]) != 64*1024 || got[1] != "short" {
		var sizes []int
		for _, l := range got {
			sizes = append(sizes, len(l))
		}
		t.Errorf("line sizes = %v, want [65536 5]", sizes)
	}
}

func TestProcessCloseTerminatesTheWholeGroup(t *testing.T) {
	var out lines
	spec := sh(`sleep 300 & echo $!; wait`)
	spec.Stdout = out.add
	p, err := dap.StartProcess(ctxT(t), spec)
	if err != nil {
		t.Fatal(err)
	}
	stopAtEnd(t, p)
	waitFor(t, "the pid of the child", func() bool { return len(out.get()) == 1 })
	child, err := strconv.Atoi(out.get()[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("closing twice: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(wait):
		t.Fatal("Close did not end the program")
	}
	waitFor(t, "the child to be gone", func() bool { return gone(child) })
}

func TestProcessThatIgnoresTerminateIsKilledAfterThreeSeconds(t *testing.T) {
	var out lines
	spec := sh(`trap "" TERM; echo ready; while :; do sleep 0.1; done`)
	spec.Stdout = out.add
	p, err := dap.StartProcess(ctxT(t), spec)
	if err != nil {
		t.Fatal(err)
	}
	stopAtEnd(t, p)
	waitFor(t, "the program to be ready", func() bool { return len(out.get()) == 1 })
	start := time.Now()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(wait):
		t.Fatal("the program was not killed")
	}
	if d := time.Since(start); d < 2500*time.Millisecond || d > 6*time.Second {
		t.Errorf("the program lasted %v after Close, want about 3 s (SIGTERM, then SIGKILL)", d)
	}
	if p.ExitCode() != -1 {
		t.Errorf("exit status = %d, want -1 (killed by a signal)", p.ExitCode())
	}
}

func TestProcessEndsWhenItsContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := dap.StartProcess(ctx, sh(`sleep 300`))
	if err != nil {
		t.Fatal(err)
	}
	stopAtEnd(t, p)
	cancel()
	select {
	case <-p.Done():
	case <-time.After(wait):
		t.Fatal("the program outlived its context")
	}
}

func TestProcessStopLetsAProgramEndOnItsOwn(t *testing.T) {
	p, err := dap.StartProcess(ctxT(t), dap.ProcessSpec{Program: "cat"}) // cat ends at the end of its input
	if err != nil {
		t.Fatal(err)
	}
	stopAtEnd(t, p)
	start := time.Now()
	p.Stop(10 * time.Second)
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Stop took %v: the program should have ended with its input", d)
	}
	if p.ExitCode() != 0 {
		t.Errorf("exit status = %d", p.ExitCode())
	}
	p.Stop(0) // a program that ended: harmless
}

func TestProcessStopTerminatesAProgramThatKeepsRunning(t *testing.T) {
	p, err := dap.StartProcess(ctxT(t), sh(`sleep 300`))
	if err != nil {
		t.Fatal(err)
	}
	stopAtEnd(t, p)
	start := time.Now()
	p.Stop(50 * time.Millisecond)
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Stop took %v", d)
	}
	select {
	case <-p.Done():
	default:
		t.Error("Stop returned before the program ended")
	}
}

func TestStartServerFailures(t *testing.T) {
	tests := []struct {
		name   string
		spec   dap.ProcessSpec
		server dap.ServerSpec
		route  string
		check  func(t *testing.T, e *errors.Error)
	}{
		{"a pattern that does not compile", sh("true"), dap.ServerSpec{PortPattern: "("}, "StartServer.DapPortPatternInvalid", nil},
		{"a pattern with no port group", sh("true"), dap.ServerSpec{PortPattern: `(\d+)`}, "StartServer.DapPortPatternInvalid", nil},
		{"a program that does not exist", dap.ProcessSpec{Program: "emetos-no-such-adapter"}, dap.ServerSpec{}, "StartProcess.DapAdapterNotFound", nil},
		{"a program that ends before it prints a port", sh(`echo "starting"; exit 3`), dap.ServerSpec{}, "StartServer.DapAdapterExited",
			func(t *testing.T, e *errors.Error) {
				if e.Metadata["exitCode"] != 3 {
					t.Errorf("metadata = %v", e.Metadata)
				}
			}},
		{"a program that prints no port in time", sh(`echo "starting"; sleep 300`), dap.ServerSpec{Timeout: 150 * time.Millisecond}, "StartServer.DapTimeout", nil},
		{"a port that is not one", sh(`echo "listening at 127.0.0.1:99999"; sleep 300`), dap.ServerSpec{}, "StartServer.DapAdapterConnectFailed", nil},
		{"a port nobody listens on", sh(`echo "listening at 127.0.0.1:1"; sleep 300`), dap.ServerSpec{}, "DialTCP.DapAdapterConnectFailed", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := dap.StartServer(ctxT(t), tt.spec, tt.server)
			if err == nil {
				_ = s.Close()
				t.Fatal("no error")
			}
			appErr := wantRouteError(t, err, tt.route)
			if tt.check != nil {
				tt.check(t, appErr)
			}
		})
	}
}

func TestStartServerStopsTheProgramThatFailed(t *testing.T) {
	var out lines
	spec := sh(`echo $$; echo "listening at 127.0.0.1:1"; sleep 300`)
	spec.Stdout = out.add
	if _, err := dap.StartServer(ctxT(t), spec, dap.ServerSpec{}); err == nil {
		t.Fatal("no error")
	}
	waitFor(t, "the program's pid", func() bool { return len(out.get()) >= 1 })
	pid, err := strconv.Atoi(out.get()[0])
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the program to be gone", func() bool { return gone(pid) })
}

func TestStartServerStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err := dap.StartServer(ctx, sh(`sleep 300`), dap.ServerSpec{Timeout: wait})
	wantRoute(t, err, "StartServer.DapAdapterStartFailed")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the context's error is lost: %v", err)
	}
}

func TestStartServerReadsTheAddressTheProgramPrints(t *testing.T) {
	tests := []struct {
		name, format, pattern string
	}{
		{"what dlv prints", "DAP server listening at: 127.0.0.1:%d", ""},
		{"what js-debug prints", "Debug server listening at 127.0.0.1:%d", ""},
		{"a name for the host", "listening on localhost:%d", ""},
		{"only a port", "port %d", ""},
		{"an address that means every interface", "listening at 0.0.0.0:%d", ""},
		{"an IPv6 address that means every interface", "listening at [::]:%d", ""},
		{"a pattern of the debugger", "Server ready :: %d :: now", `ready :: (?P<port>\d+) ::`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := daptest.Start(t, daptest.Options{Transport: daptest.Announce, AnnounceFormat: tt.format, PortPattern: tt.pattern, Script: daptest.Script{
				daptest.Reply("threads", dap.ThreadsResponseBody{Threads: []dap.Thread{{ID: 1, Name: "x"}}}),
			}})
			if _, err := h.Client.Request(ctxT(t), dap.CmdThreads, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestServerProcessIsTheClientsTransport(t *testing.T) {
	h := daptest.Start(t, daptest.Options{Transport: daptest.Announce, Script: daptest.Script{daptest.Reply("threads", nil)}})
	sp, ok := h.Client.Transport().(*dap.ServerProcess)
	if !ok {
		t.Fatalf("transport = %T", h.Client.Transport())
	}
	if !strings.HasPrefix(sp.Addr(), "127.0.0.1:") || sp.PID() == 0 {
		t.Errorf("addr %q pid %d", sp.Addr(), sp.PID())
	}
	if _, err := h.Client.Request(ctxT(t), dap.CmdThreads, nil); err != nil {
		t.Fatal(err)
	}
	sp.Stop(0)
	select {
	case <-sp.Done():
	default:
		t.Error("Stop returned before the program ended")
	}
	if sp.ExitCode() == 0 {
		t.Error("a program that was stopped before its script ended exited with 0")
	}
}

func TestDialFailures(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = dap.DialTCP(ctxT(t), "", port)
	if appErr := wantRouteError(t, err, "DialTCP.DapAdapterConnectFailed"); appErr.Metadata["address"] != "127.0.0.1:"+strconv.Itoa(port) {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	_, err = dap.DialUnix(ctxT(t), filepath.Join(t.TempDir(), "none.sock"))
	wantRoute(t, err, "DialUnix.DapAdapterConnectFailed")
}

func TestConnectNeedsAKnownKind(t *testing.T) {
	_, err := dap.Connect(ctxT(t), dap.Adapter{}, dap.ClientOptions{})
	wantRoute(t, err, "Connect.DapAdapterInvalid")
	for _, a := range []dap.Adapter{
		{Kind: dap.AdapterExecutable, Process: dap.ProcessSpec{Program: "emetos-no-such-adapter"}},
		{Kind: dap.AdapterServer, Port: 1},
		{Kind: dap.AdapterPipe, Path: filepath.Join(t.TempDir(), "none.sock")},
		{Kind: dap.AdapterExecutableServer, Process: dap.ProcessSpec{Program: "emetos-no-such-adapter"}},
	} {
		if _, err := dap.Connect(ctxT(t), a, dap.ClientOptions{}); err == nil {
			t.Errorf("Connect(%+v) worked", a)
		}
	}
}
