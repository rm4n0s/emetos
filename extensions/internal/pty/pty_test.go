package pty

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// route checks the route of an error that came from this package.
func route(t *testing.T, err error, want string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}

// shell starts /bin/sh with a script, or skips the test when there is none.
func shell(t *testing.T, opts Options, script string) *Process {
	t.Helper()
	opts.Program, opts.Args = "/bin/sh", []string{"-c", script}
	if opts.Env == nil {
		opts.Env = os.Environ()
	}
	if opts.Grace == 0 {
		opts.Grace = 200 * time.Millisecond
	}
	p, err := Start(t.Context(), opts)
	if err != nil {
		t.Skipf("no /bin/sh with a pseudo-terminal: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// readAll reads the terminal until it ends.
func readAll(t *testing.T, p *Process) string {
	t.Helper()
	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(&buf, p)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the terminal did not end")
	}
	return buf.String()
}

func TestTheErrorsHaveTheirOwnRoutes(t *testing.T) {
	// a file that is not a terminal master cannot be unlocked
	f, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	_, err = openSlave(f, 24, 80, false)
	route(t, err, "openSlave.PtyOpenFailed")

	// a program that does not exist
	_, err = Start(t.Context(), Options{Program: filepath.Join(t.TempDir(), "nope")})
	route(t, err, "Start.ProcessStartFailed")

	// a program that ended: writing, resizing and reading its closed terminal
	p := shell(t, Options{}, "exit 3")
	<-p.Done()
	readAll(t, p)
	p.Close()
	_, err = p.Write([]byte("x"))
	route(t, err, "Process.Write.PtyWriteFailed")
	route(t, p.Resize(30, 100), "Process.Resize.PtyResizeFailed")
	route(t, p.Resize(0, 100), "Process.Resize.PtyResizeFailed")
	if got := p.ExitCode(); got != 3 {
		t.Errorf("exit = %d, want 3", got)
	}

	// a read that fails for another reason than the end of the terminal
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	_, err = (&Process{master: dir}).Read(make([]byte, 8))
	route(t, err, "Process.Read.PtyReadFailed")
}

func TestAProgramSeesATerminalAndItsOutputAndInputReachUs(t *testing.T) {
	for _, tt := range []struct {
		name   string
		opts   Options
		script string
		input  string
		want   string
	}{
		{"no echo", Options{NoEcho: true}, `test -t 0 && test -t 1 && test -t 2 && echo tty; read line; echo "got:$line"`, "hello\n", "tty\r\ngot:hello\r\n"},
		{"the terminal echoes what is typed by default", Options{}, `read line; echo "got:$line"`, "hello\n", "hello\r\ngot:hello\r\n"},
		{"the default size", Options{NoEcho: true}, `stty size`, "", "24 80\r\n"},
		{"a size", Options{NoEcho: true, Rows: 30, Cols: 100}, `stty size`, "", "30 100\r\n"},
		{"the working folder and the environment", Options{NoEcho: true, Dir: "/", Env: []string{"PATH=" + os.Getenv("PATH"), "EMETOS_PTY=yes"}}, `pwd; echo $EMETOS_PTY`, "", "/\r\nyes\r\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			if opts.Env == nil {
				opts.Env = os.Environ()
			}
			p := shell(t, opts, tt.script)
			if tt.input != "" {
				if _, err := p.Write([]byte(tt.input)); err != nil {
					t.Fatal(err)
				}
			}
			if got := readAll(t, p); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
			<-p.Done()
			if p.ExitCode() != 0 {
				t.Errorf("exit = %d", p.ExitCode())
			}
		})
	}
}

func TestResizeChangesTheSizeTheProgramSees(t *testing.T) {
	// the program waits for a line, then asks for the size of its terminal
	p := shell(t, Options{NoEcho: true, Rows: 10, Cols: 40}, `read x; stty size`)
	if err := p.Resize(33, 123); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write([]byte("go\n")); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, p); got != "33 123\r\n" {
		t.Errorf("size = %q", got)
	}
}

func TestTheProgramIsTheLeaderOfItsSessionAndGroup(t *testing.T) {
	p := shell(t, Options{NoEcho: true}, `ps -o pid=,pgid=,sid= -p $$ | tr -s ' '`)
	got := strings.Fields(readAll(t, p))
	if len(got) != 3 || got[0] != got[1] || got[1] != got[2] {
		t.Skipf("ps said %q (a missing ps is not a failure of the package)", got)
	}
	if got[0] != strconv.Itoa(p.Pid()) {
		t.Errorf("pid = %s, Pid() = %d", got[0], p.Pid())
	}
}

// gone waits for the init process to reap what was killed, and tells whether no process of the group is left.
func gone(pgid int) bool {
	for range 100 {
		if syscall.Kill(-pgid, 0) == syscall.ESRCH {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestEndingTheProgramEndsItsWholeGroup(t *testing.T) {
	for _, tt := range []struct {
		name   string
		script string
		end    func(*Process)
	}{
		{"Stop ends a program and what it started", `sleep 60 & wait`, func(p *Process) { p.Stop() }},
		{"Close hangs up a program and what it started", `sleep 60 & wait`, func(p *Process) { p.Close() }},
		{"a program that ignores the signal is killed after the grace time", `trap '' TERM HUP; sleep 60 & wait`, func(p *Process) { p.Close() }},
		{"the end of a program kills what it left behind", `sleep 60 &`, func(*Process) {}},
		{"Signal reaches the group", `sleep 60 & wait`, func(p *Process) { p.Signal(syscall.SIGKILL) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := shell(t, Options{NoEcho: true, Grace: 300 * time.Millisecond}, tt.script)
			time.Sleep(100 * time.Millisecond) // the shell has started its child
			tt.end(p)
			select {
			case <-p.Done():
			case <-time.After(10 * time.Second):
				t.Fatal("the program did not end")
			}
			if !gone(p.Pid()) {
				t.Error("a process of the group is still there")
			}
			p.Stop() // ending twice, or after the end, is fine
			p.Close()
		})
	}
}

func TestTheContextEndsTheProgram(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	p, err := Start(ctx, Options{Program: "/bin/sh", Args: []string{"-c", "sleep 60 & wait"}, Env: os.Environ(), Grace: 300 * time.Millisecond})
	if err != nil {
		t.Skipf("no /bin/sh with a pseudo-terminal: %v", err)
	}
	t.Cleanup(p.Close)
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the program did not end with its context")
	}
	if !gone(p.Pid()) {
		t.Error("a process of the group is still there")
	}
	if p.ExitCode() == 0 {
		t.Error("a program that a signal ended has exit status 0")
	}
}

func TestCloseMakesAWaitingReadReturnEOF(t *testing.T) {
	p := shell(t, Options{NoEcho: true}, `sleep 60 & wait`)
	got := make(chan error, 1)
	go func() {
		_, err := p.Read(make([]byte, 16))
		got <- err
	}()
	time.Sleep(100 * time.Millisecond)
	p.Close()
	select {
	case err := <-got:
		if err != io.EOF {
			t.Errorf("Read = %v, want io.EOF", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not end the read")
	}
}

func TestForegroundPidIsTheProgramAndThenWhatItRuns(t *testing.T) {
	// a shell with job control would make a group per command; this one runs the program as the leader and then sleeps in it
	p := shell(t, Options{NoEcho: true}, `sleep 60`)
	time.Sleep(100 * time.Millisecond)
	pid, err := p.ForegroundPid()
	if err != nil {
		t.Fatal(err)
	}
	if pid != p.Pid() {
		t.Errorf("foreground group = %d, want the leader %d", pid, p.Pid())
	}
	p.Close()
	<-p.Done()
	_, err = p.ForegroundPid()
	route(t, err, "Process.ForegroundPid.PtyForegroundFailed")
}

func TestEchoOnFollowsTheEchoFlagOfTheTerminal(t *testing.T) {
	// the terminal echoes by default; stty -echo, as a program that asks for a password does, turns it off
	p := shell(t, Options{}, `printf ready; stty -echo; printf off; sleep 60`)
	if !p.EchoOn() {
		t.Error("EchoOn = false for a terminal with the default settings")
	}
	buf := make([]byte, 64)
	var got string
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(got, "readyoff") && time.Now().Before(deadline) {
		n, err := p.Read(buf)
		got += string(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(got, "readyoff") {
		t.Fatalf("the script printed %q", got)
	}
	if p.EchoOn() {
		t.Error("EchoOn = true after stty -echo")
	}
	p.Close()
	<-p.Done()
	if p.EchoOn() {
		t.Error("EchoOn = true for a terminal that is closed; what is typed there must count as secret")
	}
}
