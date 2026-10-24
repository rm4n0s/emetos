package ipc

import (
	"bufio"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

// shortDir makes a folder with a short name, where a socket path fits.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "emt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o700)
		_ = os.RemoveAll(dir)
	})
	return dir
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
}

func TestMessageErrors(t *testing.T) {
	t.Run("write to a broken connection", func(t *testing.T) {
		err := writeMessage(failingWriter{}, OK())
		wantRoute(t, err, "writeMessage.IPCWriteFailed")
	})
	t.Run("write what cannot be encoded", func(t *testing.T) {
		wantRoute(t, writeMessage(io.Discard, make(chan int)), "writeMessage.IPCMessageInvalid")
	})
	t.Run("write what is too large", func(t *testing.T) {
		wantRoute(t, writeMessage(io.Discard, strings.Repeat("a", MaxMessageSize)), "writeMessage.IPCMessageTooLarge")
	})
	t.Run("read from a closed connection", func(t *testing.T) {
		var r Request
		wantRoute(t, readMessage(bufio.NewReader(strings.NewReader("")), &r), "readMessage.IPCConnectionClosed")
	})
	t.Run("read a connection that fails", func(t *testing.T) {
		var r Request
		wantRoute(t, readMessage(bufio.NewReader(iotest.ErrReader(os.ErrDeadlineExceeded)), &r), "readMessage.IPCReadFailed")
	})
	t.Run("read half a line", func(t *testing.T) {
		var r Request
		wantRoute(t, readMessage(bufio.NewReader(strings.NewReader(`{"version":`)), &r), "readMessage.IPCReadFailed")
	})
	t.Run("a line over the buffer but under the limit", func(t *testing.T) {
		line := `{"version":1,"type":"open","cwd":"` + strings.Repeat("a", 100000) + `","open":{}}` + "\n"
		var r Request
		if err := readMessage(bufio.NewReader(strings.NewReader(line)), &r); err != nil || len(r.Cwd) != 100000 {
			t.Errorf("err %v, cwd %d bytes", err, len(r.Cwd))
		}
	})
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestStaleSocketThatCannotBeRemoved(t *testing.T) {
	skipIfRoot(t)
	base := shortDir(t)
	sub := filepath.Join(base, "s")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(sub, "x.sock")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	_ = l.Close()
	// the folder can be searched but not written to: the file can be found and not removed
	if err := os.Chmod(sub, 0o500); err != nil {
		t.Fatal(err)
	}
	_, _, err = Join(context.Background(), Options{Location: Location{Socket: sock, Lock: filepath.Join(base, "l")}})
	wantRoute(t, err, "removeStale.IPCStaleSocketRemoveFailed")
	// a path that cannot even be looked at
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	wantRoute(t, removeStale(sock, os.Getuid()), "removeStale.IPCStaleSocketRemoveFailed")
}

func TestCloseCannotLookAtTheSocket(t *testing.T) {
	skipIfRoot(t)
	base := shortDir(t)
	sub := filepath.Join(base, "s")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	opts := Options{Location: Location{Socket: filepath.Join(sub, "x.sock"), Lock: filepath.Join(base, "l")}}
	srv, _, err := Join(context.Background(), opts)
	if err != nil || srv == nil {
		t.Fatalf("Join: %v", err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	wantRoute(t, srv.Close(), "removeSocket.IPCCloseFailed")
	if err := os.Chmod(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(opts.Location.Socket); err != nil {
		t.Fatal(err)
	}
}

func TestServeWhenTheListenerFails(t *testing.T) {
	opts := testOptions(t)
	srv, _, err := Join(context.Background(), opts)
	if err != nil || srv == nil {
		t.Fatalf("Join: %v", err)
	}
	// the listener is closed under the server, which has not been told to stop
	if err := srv.listener.Close(); err != nil {
		t.Fatal(err)
	}
	err = srv.Serve(context.Background(), func(context.Context, Request) Reply { return OK() })
	wantRoute(t, err, "Serve.IPCAcceptFailed")
	if _, serr := os.Lstat(opts.Location.Socket); !os.IsNotExist(serr) {
		t.Errorf("the socket is still there: %v", serr)
	}
}
