package ipc

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// Default timeouts.
const (
	// DefaultIOTimeout is how long a message may take to be read or written (a request has to arrive
	// at once; the wait of --wait has no limit).
	DefaultIOTimeout = 5 * time.Second
	// DefaultLockTimeout is how long a launch waits for the lock that another launch holds while it decides.
	DefaultLockTimeout = 10 * time.Second
	// dialTimeout is how long connecting to a socket may take.
	dialTimeout = 2 * time.Second
)

// Options say where the instance listens and how a server and a client behave. The zero value of every field
// but Location is the default.
type Options struct {
	// Location is the socket and the lock file (Locations).
	Location Location
	// UID is the user that owns the socket: only a peer of this user is served. It is os.Getuid() when zero
	// and UIDSet is false.
	UID    int
	UIDSet bool
	// Logger gets the debug and warning lines of the server; nothing when nil.
	Logger *slog.Logger
	// OnError is told of the failures of one connection (a refused peer, a bad message), which do not stop the
	// server. Optional.
	OnError func(err error)
	// Credentials gives the user id of the peer of a connection; the SO_PEERCRED of the socket when nil.
	// Tests replace it.
	Credentials func(conn *net.UnixConn) (uid int, err error)
	// IOTimeout is DefaultIOTimeout when zero, and LockTimeout is DefaultLockTimeout.
	IOTimeout, LockTimeout time.Duration
}

func (o Options) uid() int {
	if o.UIDSet {
		return o.UID
	}
	return os.Getuid()
}

func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func (o Options) ioTimeout() time.Duration {
	if o.IOTimeout > 0 {
		return o.IOTimeout
	}
	return DefaultIOTimeout
}

func (o Options) lockTimeout() time.Duration {
	if o.LockTimeout > 0 {
		return o.LockTimeout
	}
	return DefaultLockTimeout
}

// Join decides what this process is. Under the lock file, so that two launches at once cannot both decide
// the same, it tries the socket: when an instance answers (accepts the connection), the result is a Client
// for it and no Server; when nobody does it removes a stale socket file (the file of an instance that died),
// listens, and the result is a Server and no Client. The lock is released before Join returns, and as soon as
// the server listens a launch that connects is queued until Serve accepts it, so there is no window in which
// the second launch finds nothing. The caller closes the Server (which removes the socket) or the Client.
//
// The tags are those of Locations, IPCSocketPathInvalid, IPCSocketPathTooLong, IPCLockFailed, IPCLockTimeout,
// IPCDialFailed, IPCSocketBusy (the path exists and is not a socket, or not the user's), IPCStaleSocketRemoveFailed
// and IPCListenFailed.
func Join(ctx context.Context, opts Options) (*Server, *Client, error) {
	if err := validLocation(opts.Location); err != nil {
		return nil, nil, err
	}
	release, err := lockFile(ctx, opts.Location.Lock, opts.lockTimeout())
	if err != nil {
		return nil, nil, err
	}
	defer release()

	conn, err := dialSocket(ctx, opts.Location.Socket)
	switch {
	case err == nil:
		return nil, newClient(conn, opts), nil
	case errors.Is(err, syscall.ENOENT):
		// nobody has made the socket
	case errors.Is(err, syscall.ECONNREFUSED):
		if err := removeStale(opts.Location.Socket, opts.uid()); err != nil {
			return nil, nil, err
		}
		opts.logger().Info("removed a stale socket", "path", opts.Location.Socket)
	default:
		return nil, nil, errors.NewErr("IPCDialFailed", err).SetMetadata("path", opts.Location.Socket)
	}
	srv, err := listen(opts)
	if err != nil {
		return nil, nil, err
	}
	return srv, nil, nil
}

// dialSocket connects to a Unix socket.
func dialSocket(ctx context.Context, path string) (*net.UnixConn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		conn.Close() //nolint:errcheck // not a Unix connection: nothing was sent
		return nil, syscall.ENOTSOCK
	}
	return uc, nil
}

// removeStale removes the file of a socket nobody listens on. A path that is not a socket of the user is left
// alone: it is not ours to remove.
func removeStale(path string, uid int) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.NewErr("IPCStaleSocketRemoveFailed", err).SetMetadata("path", path)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if info.Mode()&os.ModeSocket == 0 || !ok || int(st.Uid) != uid {
		return errors.New("IPCSocketBusy", "the path of the socket is in use by something that is not a socket of the user", "path", path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return errors.NewErr("IPCStaleSocketRemoveFailed", err).SetMetadata("path", path)
	}
	return nil
}

// lockFile takes an exclusive flock on the file at path (made with mode 0600) and returns the function that
// releases it. It waits for another holder for at most timeout or until ctx ends.
func lockFile(ctx context.Context, path string, timeout time.Duration) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, errors.NewErr("IPCLockFailed", err).SetMetadata("path", path)
	}
	deadline := time.Now().Add(timeout)
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			f.Close() //nolint:errcheck // the lock failed: the file is of no use
			return nil, errors.NewErr("IPCLockFailed", err).SetMetadata("path", path)
		}
		if time.Now().After(deadline) {
			f.Close() //nolint:errcheck // the lock was not taken
			return nil, errors.New("IPCLockTimeout", "another launch holds the lock of the socket", "path", path, "timeout", timeout.String())
		}
		select {
		case <-ctx.Done():
			f.Close() //nolint:errcheck // the lock was not taken
			return nil, errors.NewErr("IPCLockFailed", ctx.Err()).SetMetadata("path", path)
		case <-time.After(5 * time.Millisecond):
		}
	}
	return func() {
		// closing the file releases the lock
		_ = f.Close()
	}, nil
}
