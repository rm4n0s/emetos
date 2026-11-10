package helper

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// recorder is a slog handler that keeps the errors it is given.
type recorder struct {
	mu   sync.Mutex
	errs []*errors.Error
	text []string
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }
func (r *recorder) WithAttrs([]slog.Attr) slog.Handler       { return r }
func (r *recorder) WithGroup(string) slog.Handler            { return r }
func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.text = append(r.text, rec.Message)
	rec.Attrs(func(a slog.Attr) bool {
		if err, ok := a.Value.Any().(error); ok {
			if appErr, ok := errors.FromError(err); ok {
				r.errs = append(r.errs, appErr)
			}
		}
		r.text = append(r.text, a.Key+"="+a.Value.String())
		return true
	})
	return nil
}

// refusal waits for the server to log a refused connection and returns its error.
func (r *recorder) refusal(t *testing.T) *errors.Error {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.errs) > 0 {
			e := r.errs[0]
			r.mu.Unlock()
			return e
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the server logged no refusal")
	return nil
}

func (r *recorder) logged() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.text, "\n")
}

// calls records the requests a Handler got.
type calls struct {
	mu   sync.Mutex
	reqs []Request
}

func (c *calls) add(r Request) {
	c.mu.Lock()
	c.reqs = append(c.reqs, r)
	c.mu.Unlock()
}

func (c *calls) all() []Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Request(nil), c.reqs...)
}

// serverIn starts a server under its own root in a temporary folder.
func serverIn(t *testing.T, root string, h Handler) (*Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	s, err := Listen(ServerOptions{Root: root, Handler: h, Logger: slog.New(rec), RequestTimeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, rec
}

// shortDir makes a temporary folder with a short path: the path of a Unix socket is limited to about 100 bytes, and the names of the
// subtests of t.TempDir are long.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func newServer(t *testing.T, h Handler) (*Server, *recorder) {
	t.Helper()
	return serverIn(t, Root(shortDir(t), ""), h)
}

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func envOf(s *Server, more ...string) []string {
	return append([]string{EnvSocket + "=" + s.Socket(), EnvToken + "=" + s.Token()}, more...)
}

func run(args, env []string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = Main(args, env, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestMainRequests(t *testing.T) {
	const secret = "hunter2-correct-horse"
	tests := []struct {
		name       string
		args       []string
		env        []string
		reply      Reply
		wantCode   int
		wantStdout string
		wantKind   Kind
		wantCalled bool
	}{
		{name: "an answer is printed", args: []string{FlagAskpass, "Username for 'https://h': "}, reply: Reply{OK: true, Value: "ada"},
			wantStdout: "ada\n", wantKind: KindAskpass, wantCalled: true},
		{name: "a secret is printed to stdout only", args: []string{FlagAskpass, "Password for 'https://ada@h': "}, reply: Reply{OK: true, Value: secret},
			wantStdout: secret + "\n", wantKind: KindAskpass, wantCalled: true},
		{name: "ssh", args: []string{FlagSSHAskpass, "Enter passphrase for key '/k': "}, env: []string{EnvSSHPrompt + "=confirm"}, reply: Reply{OK: true, Value: "yes"},
			wantStdout: "yes\n", wantKind: KindSSHAskpass, wantCalled: true},
		{name: "a cancel prints nothing", args: []string{FlagAskpass, "Password for 'https://ada@h': "}, reply: Reply{},
			wantCode: 1, wantKind: KindAskpass, wantCalled: true},
		{name: "a refusal is explained", args: []string{FlagAskpass, "Username for 'https://h': "}, reply: Reply{Error: "Git is disabled"},
			wantCode: 1, wantKind: KindAskpass, wantCalled: true},
		{name: "the editor is answered with the status only", args: []string{FlagEditor, "--wait", ".git/COMMIT_EDITMSG"}, reply: Reply{OK: true, Value: "ignored"},
			wantKind: KindEditor, wantCalled: true},
		{name: "the editor is aborted", args: []string{FlagEditor, ".git/COMMIT_EDITMSG"}, reply: Reply{},
			wantCode: 1, wantKind: KindEditor, wantCalled: true},
		{name: "a silent fetch never asks", args: []string{FlagAskpass, "Username for 'https://h': "}, env: []string{EnvCommand + "=fetch", EnvFetchSilent + "=1"},
			wantCode: 1},
		{name: "a fetch the user asked for asks", args: []string{FlagAskpass, "Username for 'https://h': "}, env: []string{EnvCommand + "=fetch"}, reply: Reply{OK: true, Value: "u"},
			wantStdout: "u\n", wantKind: KindAskpass, wantCalled: true},
		{name: "no flag", args: []string{"--version"}, wantCode: 1},
		{name: "no arguments", args: nil, wantCode: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got calls
			s, _ := newServer(t, func(_ context.Context, r Request) Reply { got.add(r); return tc.reply })
			code, stdout, stderr := run(tc.args, envOf(s, tc.env...))
			if code != tc.wantCode || stdout != tc.wantStdout {
				t.Errorf("exit %d, stdout %q; want %d, %q (stderr %q)", code, stdout, tc.wantCode, tc.wantStdout, stderr)
			}
			if strings.Contains(stderr, secret) {
				t.Errorf("the secret is on stderr: %q", stderr)
			}
			if tc.reply.Error != "" && !strings.Contains(stderr, tc.reply.Error) {
				t.Errorf("stderr %q does not say %q", stderr, tc.reply.Error)
			}
			reqs := got.all()
			if (len(reqs) == 1) != tc.wantCalled || len(reqs) > 1 {
				t.Fatalf("the handler got %d requests, want called=%v", len(reqs), tc.wantCalled)
			}
			if tc.wantCalled {
				r := reqs[0]
				if r.Kind != tc.wantKind || r.Token != s.Token() || len(r.Argv) != len(tc.args)-1 {
					t.Errorf("request %+v, want kind %s and %d arguments", r, tc.wantKind, len(tc.args)-1)
				}
				if wd, err := os.Getwd(); err == nil && r.Dir != wd {
					t.Errorf("dir %q, want %q", r.Dir, wd)
				}
			}
		})
	}
}

func TestMainCannotReachTheServer(t *testing.T) {
	// a stale socket: the file is there and nothing listens
	dir := shortTempDir(t)
	stale := filepath.Join(dir, "s.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: stale, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	_ = ln.Close()

	tests := []struct {
		name string
		env  []string
		want string
	}{
		{name: "no socket in the environment", env: []string{EnvToken + "=t"}, want: EnvSocket},
		{name: "a stale socket", env: []string{EnvSocket + "=" + stale, EnvToken + "=t"}, want: "missing or invalid credentials"},
		{name: "no file", env: []string{EnvSocket + "=" + filepath.Join(dir, "none.sock"), EnvToken + "=t"}, want: "missing or invalid credentials"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := run([]string{FlagAskpass, "Password for 'https://h': "}, tc.env)
			if code != 1 || stdout != "" || !strings.Contains(stderr, tc.want) {
				t.Errorf("exit %d, stdout %q, stderr %q; want 1, nothing, %q", code, stdout, stderr, tc.want)
			}
		})
	}
	_, err = Call(context.Background(), "", Request{})
	wantRoute(t, err, "Call.GitHelperSocketMissing")
	_, err = Call(context.Background(), stale, Request{})
	wantRoute(t, err, "Call.GitHelperDialFailed")
}

func TestMainWithAWrongToken(t *testing.T) {
	var got calls
	s, rec := newServer(t, func(_ context.Context, r Request) Reply { got.add(r); return Reply{OK: true, Value: "x"} })
	code, stdout, _ := run([]string{FlagAskpass, "Password for 'https://h': "}, []string{EnvSocket + "=" + s.Socket(), EnvToken + "=" + strings.Repeat("0", 64)})
	if code != 1 || stdout != "" {
		t.Errorf("exit %d, stdout %q; want 1 and nothing", code, stdout)
	}
	wantRoute(t, rec.refusal(t), "accept.GitHelperTokenWrong")
	if len(got.all()) != 0 {
		t.Error("the handler was called with a wrong token")
	}
	if strings.Contains(rec.logged(), strings.Repeat("0", 64)) || strings.Contains(rec.logged(), s.Token()) {
		t.Errorf("a token is in the log: %s", rec.logged())
	}
}

// send writes raw bytes to the socket and returns what the server says before it closes the connection.
func send(t *testing.T, socket string, payload func(io.Writer)) []byte {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	go func() { payload(conn) }()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	data, _ := io.ReadAll(conn)
	return data
}

func TestRefusedRequests(t *testing.T) {
	tests := []struct {
		name    string
		payload func(token string) func(io.Writer)
		route   string
	}{
		{name: "10 MiB", route: "readLine.GitHelperLineTooLong", payload: func(string) func(io.Writer) {
			return func(w io.Writer) {
				// the server stops reading at the bound and closes: the write fails then, which is the point
				_, _ = w.Write([]byte(`{"token":"`))
				_, _ = w.Write(bytes.Repeat([]byte("a"), 10<<20))
			}
		}},
		{name: "a line over the bound with a newline", route: "readLine.GitHelperLineTooLong", payload: func(string) func(io.Writer) {
			return func(w io.Writer) { _, _ = w.Write(append(bytes.Repeat([]byte("a"), MaxRequestBytes+10), '\n')) }
		}},
		{name: "not JSON", route: "accept.GitHelperRequestInvalid", payload: func(string) func(io.Writer) {
			return func(w io.Writer) { _, _ = w.Write([]byte("hello\n")) }
		}},
		{name: "JSON of the wrong shape", route: "accept.GitHelperRequestInvalid", payload: func(string) func(io.Writer) {
			return func(w io.Writer) { _, _ = w.Write([]byte(`{"token": 5}` + "\n")) }
		}},
		{name: "no token", route: "accept.GitHelperTokenWrong", payload: func(string) func(io.Writer) {
			return func(w io.Writer) { _ = writeJSON(w, Request{Kind: KindAskpass, Argv: []string{"Password"}}) }
		}},
		{name: "a token that is a prefix", route: "accept.GitHelperTokenWrong", payload: func(token string) func(io.Writer) {
			return func(w io.Writer) { _ = writeJSON(w, Request{Token: token[:63], Kind: KindAskpass}) }
		}},
		{name: "a token with a suffix", route: "accept.GitHelperTokenWrong", payload: func(token string) func(io.Writer) {
			return func(w io.Writer) { _ = writeJSON(w, Request{Token: token + "0", Kind: KindAskpass}) }
		}},
		{name: "a peer that never sends", route: "readLine.GitHelperLineUnreadable", payload: func(string) func(io.Writer) {
			return func(io.Writer) { time.Sleep(2 * time.Second) }
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got calls
			s, rec := newServer(t, func(_ context.Context, r Request) Reply { got.add(r); return Reply{OK: true, Value: "x"} })
			reply := send(t, s.Socket(), tc.payload(s.Token()))
			if len(reply) != 0 {
				t.Errorf("the server answered %q", reply)
			}
			wantRoute(t, rec.refusal(t), tc.route)
			if len(got.all()) != 0 {
				t.Error("the handler was called")
			}
		})
	}
}

func TestAPeerOfAnotherUserIsRefused(t *testing.T) {
	var got calls
	other := uint32(os.Getuid()) + 1 // the peer is the user that runs the test, which this server takes for someone else
	rec := &recorder{}
	s, err := Listen(ServerOptions{Root: Root(shortDir(t), ""), Logger: slog.New(rec), uid: &other,
		Handler: func(_ context.Context, r Request) Reply { got.add(r); return Reply{OK: true, Value: "x"} }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	code, stdout, _ := run([]string{FlagAskpass, "Password for 'https://h': "}, envOf(s))
	if code != 1 || stdout != "" {
		t.Errorf("exit %d, stdout %q; want 1 and nothing", code, stdout)
	}
	wantRoute(t, rec.refusal(t), "accept.GitHelperPeerWrong")
	if len(got.all()) != 0 {
		t.Error("the handler was called")
	}
}

func TestPeerOfAClosedConnectionIsUnknown(t *testing.T) {
	s, _ := newServer(t, func(context.Context, Request) Reply { return Reply{} })
	a, b := unixPair(t)
	_ = a.Close()
	_ = b.Close()
	_, err := s.accept(a)
	wantRoute(t, err, "accept->peerUID.GitHelperPeerUnknown")
}

// unixPair makes two connected Unix sockets.
func unixPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	path := filepath.Join(shortTempDir(t), "p.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	type result struct {
		c   *net.UnixConn
		err error
	}
	ch := make(chan result, 1)
	go func() { c, err := ln.AcceptUnix(); ch <- result{c, err} }()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	r := <-ch
	if r.err != nil {
		t.Fatal(r.err)
	}
	return client, r.c
}

func TestEncodeAndWriteFailures(t *testing.T) {
	wantRoute(t, writeJSON(io.Discard, make(chan int)), "writeJSON.GitHelperEncodeFailed")
	wantRoute(t, writeJSON(failingWriter{}, Reply{}), "writeJSON.GitHelperWriteFailed")
	_, err := readLine(failingReader{}, 10)
	wantRoute(t, err, "readLine.GitHelperLineUnreadable")
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestAnInvalidReplyIsAnError(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "bad.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_, _ = readLine(c, MaxRequestBytes)
		_, _ = c.Write([]byte("this is not JSON\n"))
	}()
	_, err = Call(context.Background(), path, Request{Token: "t", Kind: KindAskpass})
	wantRoute(t, err, "Call.GitHelperReplyInvalid")
}

func TestAClientThatGoesAwayEndsTheRequest(t *testing.T) {
	started := make(chan struct{})
	ended := make(chan struct{})
	s, _ := newServer(t, func(ctx context.Context, _ Request) Reply {
		close(started)
		select {
		case <-ctx.Done():
			close(ended)
		case <-time.After(5 * time.Second):
		}
		return Reply{}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Call(ctx, s.Socket(), Request{Token: s.Token(), Kind: KindAskpass})
		done <- err
	}()
	<-started
	cancel()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the handler was not told the client is gone")
	}
	t.Logf("the call ended: %v", <-done)
}

func TestModesAndLayout(t *testing.T) {
	runtime := shortDir(t)
	s, _ := serverIn(t, Root(runtime, ""), func(context.Context, Request) Reply { return Reply{} })
	wantDir := filepath.Join(runtime, "emetos", "git-"+strconv.Itoa(os.Getpid()))
	if s.Folder() != wantDir {
		t.Errorf("folder %s, want %s", s.Folder(), wantDir)
	}
	mode := func(path string) os.FileMode {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode()
	}
	if m := mode(s.Folder()); m.Perm() != 0o700 || !m.IsDir() {
		t.Errorf("folder mode %v", m)
	}
	if m := mode(s.Socket()); m.Perm() != 0o600 || m.Type() != os.ModeSocket {
		t.Errorf("socket mode %v", m)
	}
	for _, name := range []string{ScriptAskpass, ScriptSSHAskpass, ScriptEditor} {
		if m := mode(s.Script(name)); m.Perm() != 0o700 {
			t.Errorf("%s mode %v", name, m)
		}
	}
	if len(s.Token()) != 64 {
		t.Errorf("token %q is not 32 bytes of hex", s.Token())
	}
	// without a runtime folder the state folder holds it
	if got := Root("", "/state/emetos"); got != "/state/emetos" {
		t.Errorf("Root without a runtime folder = %s", got)
	}
	if got := Root("relative", "/state/emetos"); got != "/state/emetos" {
		t.Errorf("Root with a relative runtime folder = %s", got)
	}
}

func TestTwoWindowTabs(t *testing.T) {
	root := Root(shortDir(t), "")
	var a, b calls
	first, _ := serverIn(t, root, func(_ context.Context, r Request) Reply { a.add(r); return Reply{OK: true, Value: "first"} })
	second, _ := serverIn(t, root, func(_ context.Context, r Request) Reply { b.add(r); return Reply{OK: true, Value: "second"} })
	if first.Socket() == second.Socket() || first.Token() == second.Token() || first.Folder() != second.Folder() {
		t.Fatalf("two tabs: sockets %s %s, folders %s %s", first.Socket(), second.Socket(), first.Folder(), second.Folder())
	}
	args := []string{FlagAskpass, "Username for 'https://h': "}
	if _, out, _ := run(args, envOf(first)); out != "first\n" {
		t.Errorf("first answered %q", out)
	}
	if _, out, _ := run(args, envOf(second)); out != "second\n" {
		t.Errorf("second answered %q", out)
	}
	// the token of one is no good at the other
	if code, out, _ := run(args, []string{EnvSocket + "=" + second.Socket(), EnvToken + "=" + first.Token()}); code != 1 || out != "" {
		t.Errorf("the token of the other tab was taken: exit %d, %q", code, out)
	}
	if len(a.all()) != 1 || len(b.all()) != 1 {
		t.Errorf("requests: %d and %d, want 1 and 1", len(a.all()), len(b.all()))
	}

	// closing one leaves the other, and the scripts it needs
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.Socket()); !os.IsNotExist(err) {
		t.Errorf("the socket of the closed tab is still there: %v", err)
	}
	if _, err := os.Stat(second.Script(ScriptAskpass)); err != nil {
		t.Errorf("the scripts went with the first tab: %v", err)
	}
	if _, out, _ := run(args, envOf(second)); out != "second\n" {
		t.Errorf("second answered %q after the first closed", out)
	}
	// closing the last removes the folder
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(second.Folder()); !os.IsNotExist(err) {
		t.Errorf("the folder of the process is still there: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Errorf("a second Close: %v", err)
	}
}

func TestStaleFoldersAreRemovedAtTheStart(t *testing.T) {
	root := Root(shortDir(t), "")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	// a process that is gone: it ran and ended
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot run true: %v", err)
	}
	dead := cmd.Process.Pid
	mk := func(name string) string {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "x.sock"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stale := mk("git-" + strconv.Itoa(dead))
	alive := mk("git-" + strconv.Itoa(os.Getppid()))
	other := mk("git-notapid")
	target := t.TempDir()
	link := filepath.Join(root, "git-"+strconv.Itoa(dead+1))
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if processExists(dead + 1) {
		t.Skip("the next pid is in use")
	}

	serverIn(t, root, func(context.Context, Request) Reply { return Reply{} })
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the folder of a dead process is still there: %v", err)
	}
	for _, p := range []string{alive, other, link, target} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s was removed: %v", p, err)
		}
	}
	// and nothing of the process itself is removed by itself
	removed, failed := RemoveStale(root, os.Getpid())
	if len(removed) != 0 || failed != 0 {
		t.Errorf("RemoveStale removed %v (failed %d) the second time", removed, failed)
	}
}

func TestScripts(t *testing.T) {
	// a binary whose path has a space and a quote prints how it was called
	home := filepath.Join(t.TempDir(), "it's a folder")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(home, "emetos x")
	fake := "#!/bin/sh\nfor a in \"$@\"; do printf '[%s]\\n' \"$a\"; done\n"
	if err := os.WriteFile(binary, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	root := Root(shortDir(t), "")
	rec := &recorder{}
	s, err := Listen(ServerOptions{Root: root, Binary: binary, Handler: func(context.Context, Request) Reply { return Reply{} }, Logger: slog.New(rec)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	tests := []struct {
		script, flag string
		args         []string
	}{
		{ScriptAskpass, FlagAskpass, []string{"Username for 'https://h': "}},
		{ScriptSSHAskpass, FlagSSHAskpass, []string{"Enter passphrase for key '/a b/k': "}},
		{ScriptEditor, FlagEditor, []string{"--wait", "a file with spaces", `"quoted"`, "$HOME", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.script, func(t *testing.T) {
			path := s.Script(tc.script)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != ScriptText(binary, tc.flag) {
				t.Errorf("script text %q", data)
			}
			if strings.Contains(string(data), s.Token()) || strings.Contains(string(data), s.Socket()) {
				t.Errorf("the script holds a secret or the socket: %q", data)
			}
			if out, err := exec.Command("sh", "-n", path).CombinedOutput(); err != nil {
				t.Errorf("sh -n: %v: %s", err, out)
			}
			out, err := exec.Command(path, tc.args...).CombinedOutput()
			if err != nil {
				t.Fatalf("running the script: %v: %s", err, out)
			}
			want := "[" + tc.flag + "]\n"
			for _, a := range tc.args {
				want += "[" + a + "]\n"
			}
			if string(out) != want {
				t.Errorf("the binary was called with\n%s\nwant\n%s", out, want)
			}
		})
	}
}

func TestEnvironment(t *testing.T) {
	s, _ := newServer(t, func(context.Context, Request) Reply { return Reply{} })
	if env := s.Environment(false, false); env != nil {
		t.Errorf("nothing asked, got %v", env)
	}
	has := func(env []string, want string) bool {
		for _, kv := range env {
			if kv == want {
				return true
			}
		}
		return false
	}
	both := s.Environment(true, true)
	for _, want := range []string{
		EnvSocket + "=" + s.Socket(), EnvToken + "=" + s.Token(), "GIT_ASKPASS=" + s.Script(ScriptAskpass),
		"SSH_ASKPASS=" + s.Script(ScriptSSHAskpass), "SSH_ASKPASS_REQUIRE=force", "GIT_EDITOR='" + s.Script(ScriptEditor) + "'",
		"GIT_SEQUENCE_EDITOR='" + s.Script(ScriptEditor) + "'", // the list of commits of git rebase -i goes to the same editor
	} {
		if !has(both, want) {
			t.Errorf("the environment lacks %s: %v", want, both)
		}
	}
	askpassOnly := s.Environment(true, false)
	for _, kv := range askpassOnly {
		if strings.HasPrefix(kv, "GIT_EDITOR=") || strings.HasPrefix(kv, "GIT_SEQUENCE_EDITOR=") {
			t.Errorf("the editor is set without being asked: %v", askpassOnly)
		}
	}
	editorOnly := s.Environment(false, true)
	for _, kv := range editorOnly {
		if strings.HasPrefix(kv, "GIT_ASKPASS=") || strings.HasPrefix(kv, "SSH_ASKPASS") {
			t.Errorf("askpass is set without being asked: %v", editorOnly)
		}
	}
	if !has(editorOnly, EnvToken+"="+s.Token()) {
		t.Errorf("the editor has no token: %v", editorOnly)
	}
}

func TestListenFailures(t *testing.T) {
	handler := func(context.Context, Request) Reply { return Reply{} }
	t.Run("no handler", func(t *testing.T) {
		_, err := Listen(ServerOptions{Root: shortDir(t)})
		wantRoute(t, err, "Listen.GitHelperOptionsInvalid")
	})
	t.Run("the root is a file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Listen(ServerOptions{Root: file, Handler: handler})
		wantRoute(t, err, "Listen->prepareFolder.GitHelperFolderCreateFailed")
	})
	t.Run("the folder of the process is open to others", func(t *testing.T) {
		root := Root(shortDir(t), "")
		if err := os.MkdirAll(FolderOf(root, os.Getpid()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(FolderOf(root, os.Getpid()), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Listen(ServerOptions{Root: root, Handler: handler})
		wantRoute(t, err, "Listen->prepareFolder.GitHelperFolderUnusable")
	})
	t.Run("the folder of the process is a link", func(t *testing.T) {
		root := Root(shortDir(t), "")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), FolderOf(root, os.Getpid())); err != nil {
			t.Fatal(err)
		}
		_, err := Listen(ServerOptions{Root: root, Handler: handler})
		wantRoute(t, err, "Listen->prepareFolder.GitHelperFolderUnusable")
	})
	t.Run("a script cannot be written", func(t *testing.T) {
		root := Root(shortDir(t), "")
		if err := os.MkdirAll(filepath.Join(FolderOf(root, os.Getpid()), ScriptAskpass), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := Listen(ServerOptions{Root: root, Handler: handler, Binary: "/bin/true"})
		wantRoute(t, err, "writeScript.GitHelperScriptWriteFailed")
	})
	t.Run("the path of the socket is too long", func(t *testing.T) {
		root := filepath.Join(shortDir(t), strings.Repeat("d", 90))
		_, err := Listen(ServerOptions{Root: root, Handler: handler, Binary: "/bin/true"})
		wantRoute(t, err, "Listen.GitHelperSocketPathTooLong")
		if _, statErr := os.Stat(FolderOf(root, os.Getpid())); !os.IsNotExist(statErr) {
			t.Errorf("the folder of the failed server stays: %v", statErr)
		}
	})
	t.Run("the folder cannot take a socket", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root can write anywhere")
		}
		root := Root(shortDir(t), "")
		dir := FolderOf(root, os.Getpid())
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, s := range []struct{ name, flag string }{{ScriptAskpass, FlagAskpass}, {ScriptSSHAskpass, FlagSSHAskpass}, {ScriptEditor, FlagEditor}} {
			if err := os.WriteFile(filepath.Join(dir, s.name), []byte(ScriptText("/bin/true", s.flag)), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		_, err := Listen(ServerOptions{Root: root, Handler: handler, Binary: "/bin/true"})
		wantRoute(t, err, "Listen.GitHelperListenFailed")
	})
	t.Run("no random bytes", func(t *testing.T) {
		_, err := Listen(ServerOptions{Root: Root(shortDir(t), ""), Handler: handler, Binary: "/bin/true", random: failingReader{}})
		wantRoute(t, err, "Listen.GitHelperTokenFailed")
	})
	t.Run("the binary is unknown", func(t *testing.T) {
		_, err := Listen(ServerOptions{Root: Root(shortDir(t), ""), Handler: handler,
			executable: func() (string, error) { return "", io.ErrUnexpectedEOF }})
		wantRoute(t, err, "Listen.GitHelperExecutableUnknown")
	})
}

func TestRunningBinaryOfAReplacedFile(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "emetos")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := runningBinary(exe + " (deleted)"); got != exe {
		t.Errorf("runningBinary = %q, want %q", got, exe)
	}
	if got := runningBinary(filepath.Join(dir, "gone") + " (deleted)"); !strings.HasSuffix(got, "(deleted)") {
		t.Errorf("runningBinary of a file that is gone = %q", got)
	}
	if got := runningBinary(exe); got != exe {
		t.Errorf("runningBinary = %q", got)
	}
}

func TestTooManyConnectionsAreRefused(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, MaxConnections)
	s, rec := newServer(t, func(ctx context.Context, _ Request) Reply {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return Reply{OK: true, Value: "x"}
	})
	conns := make([]net.Conn, 0, MaxConnections)
	t.Cleanup(func() {
		close(release)
		for _, c := range conns {
			_ = c.Close()
		}
	})
	for i := 0; i < MaxConnections; i++ {
		c, err := net.Dial("unix", s.Socket())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		if err := writeJSON(c, Request{Token: s.Token(), Kind: KindAskpass}); err != nil {
			t.Fatal(err)
		}
		<-entered
	}
	extra, err := net.Dial("unix", s.Socket())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = extra.Close() }()
	_ = extra.SetReadDeadline(time.Now().Add(3 * time.Second))
	if n, err := extra.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Errorf("the connection over the limit was served: %d bytes, %v", n, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(rec.logged(), "too many") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(rec.logged(), "too many") {
		t.Errorf("no log line about the limit: %s", rec.logged())
	}
}
