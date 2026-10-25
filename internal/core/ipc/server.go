package ipc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// Handler carries out the request of a client and gives the reply. ctx ends when the server stops or when the
// client goes away (killed, or it closed the connection), so a handler that waits (--wait) stops waiting. It
// runs on a goroutine of its own, concurrently with the handlers of other clients, and must not touch what
// belongs to the UI thread directly.
type Handler func(ctx context.Context, req Request) Reply

// Server is the instance's end of the socket. Make one with Join.
type Server struct {
	opts     Options
	listener *net.UnixListener
	// dev and ino identify the socket file this server made, so that Close removes only that one.
	dev, ino uint64

	// base ends when Close is called: the handlers of the clients in flight are told to stop.
	base     context.Context
	stopBase context.CancelFunc
	mu       sync.Mutex
	closing  bool
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
	// closeOnce makes Close run once; a second caller waits until the first has finished.
	closeOnce sync.Once
	closeErr  error
}

// listen makes the socket (the caller holds the lock): mode 0600, and not removed by the listener itself, so that
// Close can check that the file is still the one this server made.
func listen(opts Options) (*Server, error) {
	path := opts.Location.Socket
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, errors.NewErr("IPCListenFailed", err).SetMetadata("path", path)
	}
	l.SetUnlinkOnClose(false)
	made := false
	defer func() {
		if !made { // a failure below leaves no socket behind
			_ = l.Close()
			_ = os.Remove(path)
		}
	}()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, errors.NewErr("IPCListenFailed", err).SetMetadata("path", path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.NewErr("IPCListenFailed", err).SetMetadata("path", path)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, errors.NewErr("IPCListenFailed", syscall.ENOTSUP).SetMetadata("path", path)
	}
	base, stop := context.WithCancel(context.Background())
	made = true
	return &Server{opts: opts, listener: l, base: base, stopBase: stop, dev: uint64(st.Dev), ino: st.Ino, conns: map[net.Conn]struct{}{}}, nil //nolint:gosec // dev is an unsigned device number
}

// Path is the path of the socket.
func (s *Server) Path() string { return s.opts.Location.Socket }

// shutdownGrace is how long Close lets the clients in flight finish after their handlers were told to stop,
// and then again how long it waits for them once their connections were closed.
const shutdownGrace = 2 * time.Second

// Serve accepts clients and gives their requests to h until ctx ends or Close is called. It then closes the server
// (Close) and returns its result: the handlers in flight are told to stop through their context and may still
// give a reply, which is written when it comes in time. Each client is served on a goroutine of its own.
// The tag of a failure of the listener is IPCAcceptFailed.
func (s *Server) Serve(parent context.Context, h Handler) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	stopBase := context.AfterFunc(s.base, cancel)
	defer stopBase()
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			s.mu.Lock()
			closing := s.closing
			s.mu.Unlock()
			cancel()
			// the socket file goes before Serve returns
			cerr := s.Close()
			if closing || parent.Err() != nil {
				return cerr
			}
			return errors.NewErr("IPCAcceptFailed", err)
		}
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		s.conns[conn] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			defer func() {
				s.mu.Lock()
				delete(s.conns, conn)
				s.mu.Unlock()
				_ = conn.Close()
			}()
			if err := s.handleConn(ctx, conn, h); err != nil {
				s.report(err)
			}
		}()
	}
}

// report tells the owner of the server of the failure of one connection.
func (s *Server) report(err error) {
	s.opts.logger().Warn("a client of the socket failed", "err", err)
	if s.opts.OnError != nil {
		s.opts.OnError(err)
	}
}

// handleConn serves one client: it checks who the peer is, reads the request, runs the handler while it watches
// the connection (a client that goes away cancels the context of the handler) and writes the reply.
// The tags are IPCPeerCredentialsFailed, IPCPeerRejected, IPCMessageTooLarge, IPCMessageInvalid,
// IPCVersionUnsupported, IPCTypeUnknown, IPCHandlerPanicked and IPCWriteFailed.
func (s *Server) handleConn(ctx context.Context, conn *net.UnixConn, h Handler) error {
	timeout := s.opts.ioTimeout()
	creds := s.opts.Credentials
	if creds == nil {
		creds = peerUID
	}
	uid, err := creds(conn)
	if err != nil {
		return errors.NewErr("IPCPeerCredentialsFailed", err)
	}
	if uid != s.opts.uid() {
		s.replyBestEffort(conn, Failed("not allowed"))
		return errors.New("IPCPeerRejected", "the client belongs to another user", "uid", uid, "want", s.opts.uid())
	}
	br := bufio.NewReader(conn)
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return errors.NewErr("IPCReadFailed", err)
	}
	var req Request
	if err := readMessage(br, &req); err != nil {
		if appErr, ok := errors.FromError(err); ok && (appErr.Tag == "IPCMessageTooLarge" || appErr.Tag == "IPCMessageInvalid") {
			s.replyBestEffort(conn, Failed("the message cannot be used: "+appErr.Message))
		}
		return err
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return errors.NewErr("IPCReadFailed", err)
	}
	if req.Version != Version {
		s.replyBestEffort(conn, Failed(fmt.Sprintf("the protocol version %d is not supported (this Emetos speaks %d)", req.Version, Version)))
		return errors.New("IPCVersionUnsupported", "the request is of another version of the protocol", "version", req.Version, "supported", Version)
	}
	if req.Type != TypeOpen || req.Open == nil {
		s.replyBestEffort(conn, Failed("unknown request"))
		return errors.New("IPCTypeUnknown", "the request has an unknown type or no arguments", "type", req.Type)
	}

	// the client sends nothing more: a read that ends means it has gone
	hctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_, _ = io.Copy(io.Discard, br)
		cancel()
	}()
	reply, err := s.run(hctx, h, req)
	if err != nil {
		s.replyBestEffort(conn, Failed("the request failed"))
		return err
	}
	if reply.Version == 0 {
		reply.Version = Version
	}
	if hctx.Err() != nil && ctx.Err() == nil {
		return nil // the client went away: there is nobody to tell
	}
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return errors.NewErr("IPCWriteFailed", err)
	}
	return writeMessage(conn, reply)
}

// run calls the handler and turns its panic into an error: one request must not end the instance.
func (s *Server) run(ctx context.Context, h Handler, req Request) (reply Reply, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("IPCHandlerPanicked", "the handler of the request panicked", "panic", fmt.Sprint(r))
		}
	}()
	return h(ctx, req), nil
}

// replyBestEffort writes a reply to a client that is being turned away; a failure to write is of no use to anyone.
func (s *Server) replyBestEffort(conn *net.UnixConn, reply Reply) {
	if err := conn.SetWriteDeadline(time.Now().Add(s.opts.ioTimeout())); err != nil {
		return
	}
	_ = writeMessage(conn, reply)
}

// peerUID is the user id of the process at the other end of a Unix connection (SO_PEERCRED).
func peerUID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, serr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, serr
	}
	return int(cred.Uid), nil
}

// Close stops listening, ends the connections that are open and removes the socket file, but only if the file is
// still the one this server made (a later instance may have replaced it). It is safe to call more than once and
// from the goroutine of Serve's context; the error is IPCCloseFailed.
func (s *Server) Close() error {
	s.closeOnce.Do(func() { s.closeErr = s.close() })
	return s.closeErr
}

func (s *Server) close() error {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()

	s.stopBase()
	cerr := s.listener.Close()
	// the clients in flight get their handlers' last word; those that do not finish in time are cut off
	idle := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(idle)
	}()
	select {
	case <-idle:
	case <-time.After(shutdownGrace):
		s.mu.Lock()
		for c := range s.conns {
			_ = c.Close()
		}
		s.mu.Unlock()
		select {
		case <-idle:
		case <-time.After(shutdownGrace):
			s.opts.logger().Warn("a handler did not end when the server closed")
		}
	}
	// under the lock of the launches, so that no launch is between finding the file stale and replacing it
	release, lerr := lockFile(context.Background(), s.opts.Location.Lock, s.opts.lockTimeout())
	if lerr == nil {
		defer release()
	}
	if err := s.removeSocket(); err != nil {
		return err
	}
	if cerr != nil {
		return errors.NewErr("IPCCloseFailed", cerr)
	}
	return nil
}

// removeSocket removes the socket file when it is the one this server made.
func (s *Server) removeSocket() error {
	info, err := os.Lstat(s.opts.Location.Socket)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.NewErr("IPCCloseFailed", err).SetMetadata("path", s.opts.Location.Socket)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(st.Dev) != s.dev || st.Ino != s.ino { //nolint:gosec // dev is an unsigned device number
		return nil // another instance owns the path now
	}
	if err := os.Remove(s.opts.Location.Socket); err != nil && !os.IsNotExist(err) {
		return errors.NewErr("IPCCloseFailed", err).SetMetadata("path", s.opts.Location.Socket)
	}
	return nil
}
