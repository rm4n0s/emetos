package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

// wait is how long a test waits for something that must happen; nothing sleeps for it.
const wait = 10 * time.Second

// testOptions gives the options of an isolated socket: the runtime folder is a short folder of the test.
func testOptions(t *testing.T) Options {
	t.Helper()
	paths := storagetest.Isolate(t)
	loc, err := Locations(paths, os.Getuid())
	if err != nil {
		t.Fatalf("Locations: %v", err)
	}
	return Options{Location: loc, LockTimeout: 5 * time.Second}
}

// wantRoute checks that err is an *errors.Error whose route contains route.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

// errorSink collects the errors a server reports.
type errorSink struct {
	mu   sync.Mutex
	errs []error
	ch   chan error
}

func newErrorSink() *errorSink { return &errorSink{ch: make(chan error, 16)} }

func (s *errorSink) add(err error) {
	s.mu.Lock()
	s.errs = append(s.errs, err)
	s.mu.Unlock()
	s.ch <- err
}

// next waits for the next error.
func (s *errorSink) next(t *testing.T) error {
	t.Helper()
	select {
	case err := <-s.ch:
		return err
	case <-time.After(wait):
		t.Fatal("no error was reported")
		return nil
	}
}

// startServer joins as the first instance and serves h until the test ends.
func startServer(t *testing.T, opts Options, h Handler) (*Server, <-chan error) {
	t.Helper()
	srv, client, err := Join(context.Background(), opts)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if client != nil {
		_ = client.Close()
		t.Fatal("Join found an instance, want none")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, h) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(wait):
			t.Error("Serve did not return")
		}
		if err := srv.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return srv, done
}

func openRequest(cwd string, files ...string) Request {
	open := &OpenArgs{}
	for _, f := range files {
		open.Files = append(open.Files, FileTarget{Path: f})
	}
	return Request{Cwd: cwd, Open: open}
}

func TestLocations(t *testing.T) {
	long := "/" + strings.Repeat("d", 120)
	tests := []struct {
		name    string
		paths   storage.Paths
		socket  string
		lock    string
		tag     string
		useTemp bool
	}{
		{name: "runtime folder", paths: storage.Paths{RuntimeDir: "/run/user/1000"}, socket: "/run/user/1000/emetos-1000.sock", lock: "/run/user/1000/emetos-1000.lock"},
		{name: "path too long", paths: storage.Paths{RuntimeDir: long}, tag: "IPCSocketPathTooLong"},
		{name: "no runtime folder", paths: storage.Paths{}, useTemp: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantSocket, wantLock := tt.socket, tt.lock
			if tt.useTemp {
				tmp, err := os.MkdirTemp("", "emt")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(tmp) })
				t.Setenv("TMPDIR", tmp)
				wantSocket = filepath.Join(tmp, "emetos-1000", "emetos.sock")
				wantLock = filepath.Join(tmp, "emetos-1000", "emetos.lock")
			}
			loc, err := Locations(tt.paths, 1000)
			if tt.tag != "" {
				appErr, ok := errors.FromError(err)
				if !ok || appErr.Tag != tt.tag {
					t.Fatalf("error %v, want tag %s", err, tt.tag)
				}
				wantRoute(t, err, "Locations."+tt.tag)
				return
			}
			if err != nil {
				t.Fatalf("Locations: %v", err)
			}
			if loc.Socket != wantSocket || loc.Lock != wantLock {
				t.Errorf("Locations = %+v, want %s and %s", loc, wantSocket, wantLock)
			}
		})
	}
}

func TestLocationsFallbackFolder(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		tag   string
	}{
		{"made when missing", func(*testing.T, string) {}, ""},
		{"kept when private", func(t *testing.T, dir string) {
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}, ""},
		{"open to others", func(t *testing.T, dir string) {
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}, "IPCRuntimeDirInvalid"},
		{"a file", func(t *testing.T, dir string) {
			if err := os.WriteFile(dir, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "IPCRuntimeDirInvalid"},
		{"owned by another user", nil, "IPCRuntimeDirInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp, err := os.MkdirTemp("", "emt")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(tmp) })
			t.Setenv("TMPDIR", tmp)
			uid := os.Getuid()
			dir := filepath.Join(tmp, "emetos-"+itoa(uid))
			if tt.setup != nil {
				tt.setup(t, dir)
			} else {
				uid++ // the folder made for the user is not the folder of uid+1
				dir = filepath.Join(tmp, "emetos-"+itoa(uid))
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			_, err = Locations(storage.Paths{}, uid)
			if tt.tag == "" {
				if err != nil {
					t.Fatalf("Locations: %v", err)
				}
				if info, serr := os.Stat(dir); serr != nil || info.Mode().Perm() != 0o700 {
					t.Errorf("folder %v %v, want mode 0700", info, serr)
				}
				return
			}
			wantRoute(t, err, "ensurePrivateDir."+tt.tag)
		})
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestJoinRejectsBadLocations(t *testing.T) {
	long := "/" + strings.Repeat("d", 120)
	tests := []struct {
		name string
		loc  Location
		tag  string
	}{
		{"relative socket", Location{Socket: "s.sock", Lock: "/tmp/l"}, "IPCSocketPathInvalid"},
		{"relative lock", Location{Socket: "/tmp/s.sock", Lock: "l"}, "IPCSocketPathInvalid"},
		{"empty", Location{}, "IPCSocketPathInvalid"},
		{"too long", Location{Socket: long + "/s.sock", Lock: "/tmp/l"}, "IPCSocketPathTooLong"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Join(context.Background(), Options{Location: tt.loc})
			wantRoute(t, err, "Join->validLocation."+tt.tag)
		})
	}
}

func TestRoundTrip(t *testing.T) {
	opts := testOptions(t)
	var got Request
	var mu sync.Mutex
	startServer(t, opts, func(_ context.Context, req Request) Reply {
		mu.Lock()
		got = req
		mu.Unlock()
		return OK()
	})
	// the second launch finds the instance
	srv, client, err := Join(context.Background(), opts)
	if err != nil || srv != nil || client == nil {
		t.Fatalf("Join = %v, %v, %v; want a client", srv, client, err)
	}
	defer func() { _ = client.Close() }()
	req := Request{Cwd: "/work", Open: &OpenArgs{
		Folder: "/work/p", Files: []FileTarget{{Path: "/work/a.go", Line: 3, Column: 4}, {Path: "/work/b.go"}},
		NewWindow: true, Profile: "p",
	}}
	reply, err := client.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply != OK() {
		t.Errorf("reply = %+v, want %+v", reply, OK())
	}
	mu.Lock()
	defer mu.Unlock()
	if got.Version != Version || got.Type != TypeOpen || got.PID != os.Getpid() || got.Cwd != "/work" {
		t.Errorf("request = %+v", got)
	}
	if got.Open == nil || got.Open.Folder != "/work/p" || len(got.Open.Files) != 2 || got.Open.Files[0] != (FileTarget{Path: "/work/a.go", Line: 3, Column: 4}) ||
		!got.Open.NewWindow || got.Open.Profile != "p" {
		t.Errorf("open = %+v", got.Open)
	}
	// the socket is private
	info, err := os.Stat(opts.Location.Socket)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("socket %v %v, want mode 0600", info, err)
	}
}

func TestErrorReplyIsAReply(t *testing.T) {
	opts := testOptions(t)
	startServer(t, opts, func(context.Context, Request) Reply { return Failed("no") })
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	reply, err := c.Send(context.Background(), openRequest("/"))
	if err != nil || reply.Type != TypeError || reply.ExitCode != 1 || reply.Message != "no" {
		t.Errorf("reply %+v, err %v", reply, err)
	}
}

// rawExchange sends the bytes to the server and reads one reply.
func rawExchange(t *testing.T, opts Options, payload []byte) Reply {
	t.Helper()
	conn, err := net.Dial("unix", opts.Location.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(wait))
	go func() { _, _ = conn.Write(payload) }()
	var reply Reply
	if err := readMessage(bufio.NewReader(conn), &reply); err != nil {
		t.Fatalf("no reply: %v", err)
	}
	return reply
}

func TestServerRefusesBadMessages(t *testing.T) {
	oversize := []byte(strings.Repeat("a", MaxMessageSize+10) + "\n")
	tests := []struct {
		name    string
		payload []byte
		route   string
	}{
		{"version", []byte(`{"version":2,"type":"open","open":{}}` + "\n"), "handleConn.IPCVersionUnsupported"},
		{"no version", []byte(`{"type":"open","open":{}}` + "\n"), "handleConn.IPCVersionUnsupported"},
		{"type", []byte(`{"version":1,"type":"close","open":{}}` + "\n"), "handleConn.IPCTypeUnknown"},
		{"no arguments", []byte(`{"version":1,"type":"open"}` + "\n"), "handleConn.IPCTypeUnknown"},
		{"not json", []byte("hello\n"), "readMessage.IPCMessageInvalid"},
		{"too large", oversize, "readMessage.IPCMessageTooLarge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			sink := newErrorSink()
			opts.OnError = sink.add
			var called atomic.Bool
			startServer(t, opts, func(context.Context, Request) Reply { called.Store(true); return OK() })
			reply := rawExchange(t, opts, tt.payload)
			if reply.Type != TypeError || reply.ExitCode == 0 || reply.Message == "" {
				t.Errorf("reply %+v, want an error", reply)
			}
			wantRoute(t, sink.next(t), tt.route)
			if called.Load() {
				t.Error("the handler ran")
			}
		})
	}
}

func TestServerKeepsServingAfterABadClient(t *testing.T) {
	opts := testOptions(t)
	sink := newErrorSink()
	opts.OnError = sink.add
	startServer(t, opts, func(context.Context, Request) Reply { return OK() })
	rawExchange(t, opts, []byte("garbage\n"))
	_ = sink.next(t)
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if reply, err := c.Send(context.Background(), openRequest("/")); err != nil || reply.Type != TypeOK {
		t.Errorf("reply %+v, err %v", reply, err)
	}
}

func TestPeerOfAnotherUserIsRefused(t *testing.T) {
	tests := []struct {
		name  string
		creds func(*net.UnixConn) (int, error)
		tag   string
		ok    bool
	}{
		{"same user", func(*net.UnixConn) (int, error) { return os.Getuid(), nil }, "", true},
		{"another user", func(*net.UnixConn) (int, error) { return os.Getuid() + 1, nil }, "IPCPeerRejected", false},
		{"credentials unavailable", func(*net.UnixConn) (int, error) { return 0, syscall.ENOTSUP }, "IPCPeerCredentialsFailed", false},
		{"real credentials", nil, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			sink := newErrorSink()
			opts.OnError = sink.add
			opts.Credentials = tt.creds
			var called atomic.Bool
			startServer(t, opts, func(context.Context, Request) Reply { called.Store(true); return OK() })
			c, err := Dial(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close() }()
			reply, err := c.Send(context.Background(), openRequest("/"))
			if tt.ok {
				if err != nil || reply.Type != TypeOK || !called.Load() {
					t.Errorf("reply %+v, err %v, called %v", reply, err, called.Load())
				}
				return
			}
			wantRoute(t, sink.next(t), "handleConn."+tt.tag)
			if called.Load() {
				t.Error("the handler ran for a refused peer")
			}
			// the client heard why, or the connection ended
			if err == nil && reply.Type != TypeError {
				t.Errorf("reply %+v", reply)
			}
		})
	}
}

func TestHandlerPanicDoesNotEndTheServer(t *testing.T) {
	opts := testOptions(t)
	sink := newErrorSink()
	opts.OnError = sink.add
	var first atomic.Bool
	startServer(t, opts, func(context.Context, Request) Reply {
		if first.CompareAndSwap(false, true) {
			panic("boom")
		}
		return OK()
	})
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := c.Send(context.Background(), openRequest("/"))
	_ = c.Close()
	if err != nil || reply.Type != TypeError {
		t.Errorf("reply %+v, err %v", reply, err)
	}
	wantRoute(t, sink.next(t), "Server.run.func1.IPCHandlerPanicked")
	c, err = Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if reply, err := c.Send(context.Background(), openRequest("/")); err != nil || reply.Type != TypeOK {
		t.Errorf("reply %+v, err %v", reply, err)
	}
}

func TestStaleSocketIsTakenOver(t *testing.T) {
	opts := testOptions(t)
	// an instance that died: its socket file is there and nobody listens
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: opts.Location.Socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(opts.Location.Socket); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("no stale socket: %v %v", info, err)
	}
	startServer(t, opts, func(context.Context, Request) Reply { return OK() })
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatalf("the new instance does not listen: %v", err)
	}
	defer func() { _ = c.Close() }()
	if reply, err := c.Send(context.Background(), openRequest("/")); err != nil || reply.Type != TypeOK {
		t.Errorf("reply %+v, err %v", reply, err)
	}
}

func TestJoinLeavesWhatIsNotASocket(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
		tag   string
	}{
		{"a file", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "IPCSocketBusy"},
		{"a folder", func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}, "IPCSocketBusy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			tt.setup(t, opts.Location.Socket)
			_, _, err := Join(context.Background(), opts)
			wantRoute(t, err, "Join->removeStale."+tt.tag)
			if _, serr := os.Lstat(opts.Location.Socket); serr != nil {
				t.Errorf("the path was removed: %v", serr)
			}
		})
	}
}

func TestRemoveStaleLeavesForeignPaths(t *testing.T) {
	dir, err := os.MkdirTemp("", "emt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	wantRoute(t, removeStale(file, os.Getuid()), "removeStale.IPCSocketBusy")
	// a socket of another user is not ours either
	sock := filepath.Join(dir, "s")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	_ = l.Close()
	wantRoute(t, removeStale(sock, os.Getuid()+1), "removeStale.IPCSocketBusy")
	if err := removeStale(filepath.Join(dir, "missing"), os.Getuid()); err != nil {
		t.Errorf("a missing path: %v", err)
	}
	if err := removeStale(sock, os.Getuid()); err != nil {
		t.Errorf("a stale socket of the user: %v", err)
	}
	if _, err := os.Lstat(sock); !os.IsNotExist(err) {
		t.Errorf("the stale socket is still there: %v", err)
	}
}

func TestCloseKeepsASocketAnotherInstanceOwns(t *testing.T) {
	opts := testOptions(t)
	a, _, err := Join(context.Background(), opts)
	if err != nil || a == nil {
		t.Fatalf("Join: %v", err)
	}
	// the file of a is taken away and another instance listens at the path
	if err := os.Remove(opts.Location.Socket); err != nil {
		t.Fatal(err)
	}
	b, _ := startServer(t, opts, func(context.Context, Request) Reply { return OK() })
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatalf("the socket of the other instance is gone: %v", err)
	}
	defer func() { _ = c.Close() }()
	if reply, err := c.Send(context.Background(), openRequest("/")); err != nil || reply.Type != TypeOK {
		t.Errorf("reply %+v, err %v", reply, err)
	}
	_ = b
}

func TestConcurrentClients(t *testing.T) {
	opts := testOptions(t)
	var mu sync.Mutex
	seen := map[string]bool{}
	startServer(t, opts, func(_ context.Context, req Request) Reply {
		mu.Lock()
		seen[req.Cwd] = true
		mu.Unlock()
		return OK()
	})
	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := Dial(context.Background(), opts)
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = c.Close() }()
			cwd := "/c" + itoa(i)
			reply, err := c.Send(context.Background(), openRequest(cwd))
			if err == nil && reply.Type != TypeOK {
				err = errors.New("Test", "unexpected reply", "type", reply.Type)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != n {
		t.Errorf("%d requests seen, want %d", len(seen), n)
	}
}

func TestWaitReplyComesWhenTheHandlerSays(t *testing.T) {
	opts := testOptions(t)
	release := make(chan struct{})
	started := make(chan struct{})
	startServer(t, opts, func(ctx context.Context, req Request) Reply {
		close(started)
		select {
		case <-release:
			return Done(4)
		case <-ctx.Done():
			return Failed("cancelled")
		}
	})
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	req := openRequest("/")
	req.Open.Wait = true
	replies := make(chan Reply, 1)
	go func() {
		reply, err := c.Send(context.Background(), req)
		if err != nil {
			t.Errorf("Send: %v", err)
		}
		replies <- reply
	}()
	<-started
	select {
	case r := <-replies:
		t.Fatalf("the reply came before the release: %+v", r)
	default:
	}
	close(release)
	select {
	case r := <-replies:
		if r.Type != TypeDone || r.ExitCode != 4 {
			t.Errorf("reply %+v, want done with 4", r)
		}
	case <-time.After(wait):
		t.Fatal("no reply")
	}
}

func TestADroppedClientCancelsTheWait(t *testing.T) {
	opts := testOptions(t)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	startServer(t, opts, func(ctx context.Context, req Request) Reply {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return Failed("gone")
	})
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	req := openRequest("/")
	req.Open.Wait = true
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := c.Send(ctx, req)
		errc <- err
	}()
	<-started
	cancel() // the client gives up: its connection closes
	select {
	case <-cancelled:
	case <-time.After(wait):
		t.Fatal("the handler was not cancelled")
	}
	err = <-errc
	wantRoute(t, err, "Client.Send.IPCReplyFailed")
}

func TestShutdownRemovesTheSocket(t *testing.T) {
	opts := testOptions(t)
	srv, client, err := Join(context.Background(), opts)
	if err != nil || client != nil {
		t.Fatalf("Join: %v %v", client, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	started := make(chan struct{})
	handlerEnded := make(chan struct{})
	go func() {
		served <- srv.Serve(ctx, func(ctx context.Context, _ Request) Reply {
			close(started)
			<-ctx.Done()
			close(handlerEnded)
			return Failed("shutting down")
		})
	}()
	// a client is being served when the instance stops
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	req := openRequest("/")
	req.Open.Wait = true
	go func() { _, _ = c.Send(context.Background(), req) }()
	<-started
	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve = %v", err)
		}
	case <-time.After(wait):
		t.Fatal("Serve did not return")
	}
	<-handlerEnded
	if _, err := os.Lstat(opts.Location.Socket); !os.IsNotExist(err) {
		t.Errorf("the socket is still there: %v", err)
	}
	if _, err := Dial(context.Background(), opts); err == nil {
		t.Error("Dial succeeded after the shutdown")
	} else {
		wantRoute(t, err, "Dial.IPCDialFailed")
	}
	if err := srv.Close(); err != nil {
		t.Errorf("a second Close = %v", err)
	}
}

func TestLockTimeout(t *testing.T) {
	opts := testOptions(t)
	opts.LockTimeout = 50 * time.Millisecond
	release, err := lockFile(context.Background(), opts.Location.Lock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, _, err = Join(context.Background(), opts)
	wantRoute(t, err, "Join->lockFile.IPCLockTimeout")
	// and a context that ends
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opts.LockTimeout = time.Minute
	_, _, err = Join(ctx, opts)
	wantRoute(t, err, "Join->lockFile.IPCLockFailed")
}

func TestJoinListenFailure(t *testing.T) {
	opts := testOptions(t)
	// the folder of the socket is gone
	opts.Location.Socket = filepath.Join(filepath.Dir(opts.Location.Socket), "missing", "s.sock")
	_, _, err := Join(context.Background(), opts)
	wantRoute(t, err, "Join->listen.IPCListenFailed")
}

func TestLockFileFailure(t *testing.T) {
	opts := testOptions(t)
	opts.Location.Lock = filepath.Join(opts.Location.Lock, "x") // the lock file's parent is not a folder
	_, _, err := Join(context.Background(), opts)
	wantRoute(t, err, "Join->lockFile.IPCLockFailed")
}

func TestClientRefusesABadReply(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		route string
	}{
		{"other version", `{"version":9,"type":"ok"}`, "Client.Send.IPCVersionUnsupported"},
		{"unknown type", `{"version":1,"type":"maybe"}`, "Client.Send.IPCReplyInvalid"},
		{"not json", `nonsense`, "Client.Send.IPCReplyFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			l, err := net.ListenUnix("unix", &net.UnixAddr{Name: opts.Location.Socket, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = l.Close() }()
			go func() {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				defer func() { _ = conn.Close() }()
				_, _ = bufio.NewReader(conn).ReadString('\n')
				_, _ = conn.Write([]byte(tt.reply + "\n"))
			}()
			c, err := Dial(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close() }()
			_, err = c.Send(context.Background(), openRequest("/"))
			wantRoute(t, err, tt.route)
		})
	}
}

func TestClientSendFailures(t *testing.T) {
	opts := testOptions(t)
	startServer(t, opts, func(context.Context, Request) Reply { return OK() })
	// a request over the limit is not sent
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	big := openRequest("/", strings.Repeat("x", MaxMessageSize))
	_, err = c.Send(context.Background(), big)
	wantRoute(t, err, "Client.Send->writeMessage.IPCMessageTooLarge")
	_ = c.Close()
	// a closed connection
	_, err = c.Send(context.Background(), openRequest("/"))
	wantRoute(t, err, "Client.Send.IPCSendFailed")
	if err := c.Close(); err != nil {
		t.Errorf("a second Close = %v", err)
	}
}

func TestJoinInProcessRaceHasOneServer(t *testing.T) {
	opts := testOptions(t)
	const n = 6
	type result struct {
		srv    *Server
		client *Client
		err    error
	}
	results := make(chan result, n)
	start := make(chan struct{})
	for range n {
		go func() {
			<-start
			s, c, err := Join(context.Background(), opts)
			results <- result{s, c, err}
		}()
	}
	close(start)
	servers, clients := 0, 0
	var srv *Server
	for range n {
		r := <-results
		if r.err != nil {
			t.Errorf("Join: %v", r.err)
			continue
		}
		if r.srv != nil {
			servers++
			srv = r.srv
		}
		if r.client != nil {
			clients++
			_ = r.client.Close()
		}
	}
	if servers != 1 || clients != n-1 {
		t.Errorf("%d servers and %d clients, want 1 and %d", servers, clients, n-1)
	}
	if srv != nil {
		if err := srv.Close(); err != nil {
			t.Error(err)
		}
	}
}

// isolatePaths is storagetest.Isolate.
func isolatePaths(t *testing.T) storage.Paths { return storagetest.Isolate(t) }
