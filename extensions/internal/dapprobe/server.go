package dapprobe

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
)

// The adapters that are servers (js-debug; the tag emetos_jsdebug): a program that listens on a port and prints it, which the client
// then connects to, as the host does for DebugAdapterExecutableServer; and further connections to the same server, which are the
// sessions its adapter starts with startDebugging.

// listenLine finds the address in the line a server program prints: "Debug server listening at 127.0.0.1:8123", "listening at: [::]:4711".
var listenLine = regexp.MustCompile(`listening (?:at|on)(?: port)?:? \[?([0-9A-Za-z.:-]*?)\]?:?(\d+)\s*$`)

// serverWait is how long the server has to print its port.
const serverWait = 30 * time.Second

// StartServer starts the program a DebugAdapterExecutableServer descriptor describes, as Start does (under `timeout -k`, in a process
// group of its own, in a home folder and a temporary folder of the test), waits for the line with its port and connects to it. The
// client's Addr is the address of the server, which Connect reaches again for the sessions of children. The test fails when the program
// prints no port.
func StartServer(t *testing.T, a *extension.DebugAdapter, adapterID string) *Client {
	t.Helper()
	if a.Kind != extension.DebugAdapterExecutableServer {
		t.Fatalf("adapter = %+v, want an executable server", a)
	}
	timeout, err := exec.LookPath("timeout")
	if err != nil {
		t.Skip("timeout is not installed")
	}
	cmd := exec.Command(timeout, append([]string{"-k", "5", "600", a.Command}, a.Args...)...)
	cmd.Dir = a.Cwd
	home := t.TempDir()
	tmp := filepath.Join(home, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, "config"), "XDG_CACHE_HOME="+filepath.Join(home, "cache"),
		"XDG_DATA_HOME="+filepath.Join(home, "data"), "TMPDIR="+tmp)
	for k, v := range a.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out := &lockedBuilder{}
	found := make(chan string, 1)
	cmd.Stdout = &lineWriter{out: out, found: found}
	cmd.Stderr = &lineWriter{out: out, found: found}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	var addr string
	select {
	case addr = <-found:
	case <-time.After(serverWait):
		t.Fatalf("the server printed no port in %v (output: %q)", serverWait, out.String())
	}
	c := Connect(t, addr, adapterID)
	c.stderr = out
	return c
}

// Connect connects to a server that listens at addr and returns the client of the session on that connection. The session is ended
// (disconnect, with terminateDebuggee) and the connection closed when the test ends.
func Connect(t *testing.T, addr, adapterID string) *Client {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{t: t, w: conn, msgs: make(chan map[string]any, 256), responses: map[int]map[string]any{}, stderr: &lockedBuilder{}, adapterID: adapterID, Addr: addr}
	go c.read(conn)
	t.Cleanup(func() { _ = conn.Close() })
	t.Cleanup(c.terminate) // runs before the connection is closed
	return c
}

// lineWriter keeps what a program prints and reports the address in the first line that has one.
type lineWriter struct {
	out   *lockedBuilder
	found chan string
	buf   []byte
}

// Write implements io.Writer.
func (l *lineWriter) Write(p []byte) (int, error) {
	_, _ = l.out.Write(p)
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(string(l.buf[:i]))
		l.buf = l.buf[i+1:]
		if m := listenLine.FindStringSubmatch(line); m != nil {
			host := m[1]
			if host == "" || host == "0.0.0.0" || host == "::" {
				host = "127.0.0.1"
			}
			select {
			case l.found <- net.JoinHostPort(host, m[2]):
			default:
			}
		}
	}
	return len(p), nil
}
