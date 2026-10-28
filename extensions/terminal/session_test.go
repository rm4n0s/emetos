package terminal

import (
	"bytes"
	"context"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/pty"
)

func TestTerminalShowsTheOutputOfItsProgramAndEnds(t *testing.T) {
	tests := []struct {
		name, script string
		want         string
		code         int
	}{
		{"output and status", `echo hello; exit 3`, "hello", 3},
		{"stderr is the terminal too", `echo oops >&2; exit 0`, "oops", 0},
		{"colors reach the emulator", `printf '\033[31mred\033[0m done'`, "red done", 0},
		{"a program that sees a terminal", `test -t 0 && test -t 1 && echo tty`, "tty", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term := startSh(t, tt.script, 24, 80)
			select {
			case <-term.Exited():
			case <-time.After(10 * time.Second):
				t.Fatal("the terminal did not end")
			}
			waitText(t, term, tt.want)
			code, exited := term.ExitCode()
			if !exited || code != tt.code {
				t.Errorf("exit = %d, %v; want %d, true", code, exited, tt.code)
			}
		})
	}
}

func TestTerminalInputReachesTheProgramAndTheProgramSeesTheSize(t *testing.T) {
	term := startTestShell(t, 30, 100)
	waitText(t, term, "ready")
	term.Send([]byte("size\r"))
	waitText(t, term, "size 30 100")
	term.Resize(120, 40)
	term.Send([]byte("size\r"))
	waitText(t, term, "size 40 120")
	if cols, rows := term.Size(); cols != 120 || rows != 40 {
		t.Errorf("Size = %d x %d, want 120 x 40", cols, rows)
	}
	term.Send([]byte("echo typed\r"))
	waitText(t, term, "typed")
}

func TestTerminalRepliesGoToTheProgram(t *testing.T) {
	// the program asks where the cursor is (CSI 6 n) and the emulator answers on the terminal's input
	term := startSh(t, `printf '\033[6n'; IFS= read -r -s -n 6 -t 5 reply; printf 'reply=%s' "$(printf %s "$reply" | od -An -c | tr -d ' ')"`, 24, 80)
	waitText(t, term, "[1;1R")
}

func TestTerminalTitleBellAndFolder(t *testing.T) {
	term := startTestShell(t, 24, 80)
	waitText(t, term, "ready")
	var metas atomic.Int32
	term.SetOnMeta(func() { metas.Add(1) })
	term.Send([]byte("title my title\rbell\r"))
	waitText(t, term, "$")
	deadline := time.Now().Add(5 * time.Second)
	for term.Meta().title != "my title" || term.Meta().bellAt.IsZero() {
		if time.Now().After(deadline) {
			t.Fatalf("meta = %+v", term.Meta())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if metas.Load() == 0 {
		t.Error("the owner was not told")
	}
	if got := term.Cwd(); got == "" {
		t.Error("the terminal has no folder")
	}
}

// A flood of output never has more than highWater plus one chunk waiting to be parsed, and all of it arrives.
func TestFlowControlBoundsTheUnparsedOutputAndKeepsAllOfIt(t *testing.T) {
	const lines = 200_000
	term := startTestShell(t, 24, 80)
	waitText(t, term, "ready")
	var peak int
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			term.flowMu.Lock()
			peak = max(peak, term.unparsed)
			term.flowMu.Unlock()
			time.Sleep(50 * time.Microsecond)
		}
	}()
	term.Send([]byte("flood " + itoa(lines) + "\r"))
	waitText(t, term, "line "+itoa(lines))
	close(stop)
	<-done
	if peak > highWater+readChunk {
		t.Errorf("up to %d bytes waited to be parsed, want at most %d", peak, highWater+readChunk)
	}
	if got := term.Emulator().Scrollback(); got != 1000 {
		t.Errorf("scrollback = %d lines, want the 1000 the terminal keeps", got)
	}
}

func itoa(n int) string { return intString(n) }

func intString(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Kill ends a program that ignores SIGHUP after the grace time, and its whole group.
func TestKillEndsTheProgramAndItsGroup(t *testing.T) {
	term := startSh(t, `trap '' HUP; sleep 60 & echo started; wait`, 24, 80)
	waitText(t, term, "started")
	start := time.Now()
	term.Kill()
	select {
	case <-term.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("Kill did not end the terminal")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Kill took %v", d)
	}
}

// fakeProcess is a program that is not one: it writes what the test gives it and never reads its input.
type fakeProcess struct {
	out    chan []byte
	done   chan struct{}
	closed atomic.Bool
	inMu   sync.Mutex
	in     bytes.Buffer
	size   [2]int
}

// input is what the terminal wrote to the program so far.
func (f *fakeProcess) input() string {
	f.inMu.Lock()
	defer f.inMu.Unlock()
	return f.in.String()
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{out: make(chan []byte, 16), done: make(chan struct{})}
}

func (f *fakeProcess) Read(p []byte) (int, error) {
	b, ok := <-f.out
	if !ok {
		return 0, io.EOF
	}
	return copy(p, b), nil
}
func (f *fakeProcess) Write(p []byte) (int, error) {
	f.inMu.Lock()
	defer f.inMu.Unlock()
	return f.in.Write(p)
}
func (f *fakeProcess) Resize(rows, cols int) error { f.size = [2]int{rows, cols}; return nil }
func (f *fakeProcess) Close() {
	if f.closed.CompareAndSwap(false, true) {
		close(f.out)
		close(f.done)
	}
}
func (f *fakeProcess) Signal(syscall.Signal) {}
func (f *fakeProcess) Done() <-chan struct{} { return f.done }
func (f *fakeProcess) ExitCode() int         { return 0 }
func (f *fakeProcess) Pid() int              { return 1 }

func TestInputIsQueuedForAProgramThatDoesNotRead(t *testing.T) {
	f := newFakeProcess()
	term, err := newTerminal(t.Context(), terminalOpts{id: 1, profile: profile{name: "fake", path: "fake"}, scrollback: 10,
		start: func(context.Context, pty.Options) (process, error) { return f, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Kill(); term.Wait() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		big := bytes.Repeat([]byte("x"), 1<<20)
		term.Send(big) // does not block
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked")
	}
	f.out <- []byte("still parsing")
	waitText(t, term, "still parsing")
}

func TestStartFailureKeepsThePtyError(t *testing.T) {
	_, err := newTerminal(t.Context(), terminalOpts{id: 1, profile: profile{name: "nope", path: "/nonexistent/shell"}, env: os.Environ()})
	if err == nil {
		t.Fatal("a terminal started a program that does not exist")
	}
	routeHas(t, err, "ProcessStartFailed")
}
