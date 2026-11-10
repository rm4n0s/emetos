package helper

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// Handler answers a request. ctx ends when the client goes away (git was cancelled) or the server closes; the Handler may stop
// waiting for the user then. It is called from the goroutine of the connection, so it may take as long as the user does, and several
// Handlers run at the same time.
type Handler func(ctx context.Context, req Request) Reply

// ServerOptions configure a Server.
type ServerOptions struct {
	// Root is the folder that holds the folders of the processes (Root(runtimeDir, stateDir)). Required.
	Root string
	// Binary is the program the scripts start; os.Executable() when empty.
	Binary string
	// Pid is the id of the process the folder is named after; os.Getpid() when zero.
	Pid int
	// Handler answers the requests. Required.
	Handler Handler
	// Logger gets the refusals; slog.Default() when nil. It never gets the arguments of a request.
	Logger *slog.Logger
	// RequestTimeout is how long a connection has to send its request; DefaultRequestTimeout when zero.
	RequestTimeout time.Duration

	// random and executable are the sources of the token and of the path of the binary; the tests replace them.
	random     io.Reader
	executable func() (string, error)
	// uid, when set, is the user id the peers must have instead of the user's own.
	uid *uint32
}

// Server listens on the socket of one window tab.
type Server struct {
	opts   ServerOptions
	log    *slog.Logger
	dir    string
	socket string
	token  string
	ln     *net.UnixListener
	// uid is the user id the peers must have.
	uid uint32

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	slots  chan struct{}
	once   sync.Once
}

// Listen makes the folder of the process and its scripts, removes the folders of dead processes, makes a token and starts serving on a
// new socket (mode 0600). The error has the tags of the folder (GitHelperFolderCreateFailed, GitHelperFolderUnusable,
// GitHelperScriptWriteFailed), GitHelperExecutableUnknown, GitHelperTokenFailed, GitHelperSocketPathTooLong and GitHelperListenFailed.
func Listen(opts ServerOptions) (*Server, error) {
	if opts.Handler == nil || opts.Root == "" {
		return nil, errors.New("GitHelperOptionsInvalid", "a server needs a root folder and a handler", "root", opts.Root)
	}
	if opts.Pid == 0 {
		opts.Pid = os.Getpid()
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	if opts.random == nil {
		opts.random = rand.Reader
	}
	if opts.executable == nil {
		opts.executable = os.Executable
	}
	if opts.Binary == "" {
		exe, err := opts.executable()
		if err != nil {
			return nil, errors.NewErr("GitHelperExecutableUnknown", err)
		}
		opts.Binary = runningBinary(exe)
	}
	if removed, failed := RemoveStale(opts.Root, opts.Pid); len(removed) > 0 || failed > 0 {
		log.Info("removed the folders of Git helpers that are gone", "removed", len(removed), "failed", failed)
	}
	dir, err := prepareFolder(opts.Root, opts.Pid, opts.Binary)
	if err != nil {
		return nil, err
	}
	var raw [32]byte
	if _, err := io.ReadFull(opts.random, raw[:]); err != nil {
		return nil, errors.NewErr("GitHelperTokenFailed", err)
	}
	var id [4]byte
	if _, err := io.ReadFull(opts.random, id[:]); err != nil {
		return nil, errors.NewErr("GitHelperTokenFailed", err)
	}
	socket := filepath.Join(dir, hex.EncodeToString(id[:])+".sock")
	if len(socket) > maxSocketPath {
		releaseFolder(dir, socket)
		return nil, errors.New("GitHelperSocketPathTooLong", "the path of the socket is longer than a Unix socket path may be", "length", len(socket), "max", maxSocketPath)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		releaseFolder(dir, socket)
		return nil, errors.NewErr("GitHelperListenFailed", err).SetMetadata("socket", socket)
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = ln.Close()
		releaseFolder(dir, socket)
		return nil, errors.NewErr("GitHelperListenFailed", err).SetMetadata("socket", socket)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		opts: opts, log: log, dir: dir, socket: socket, token: hex.EncodeToString(raw[:]), ln: ln, uid: uint32(os.Getuid()),
		ctx: ctx, cancel: cancel, slots: make(chan struct{}, MaxConnections),
	}
	if opts.uid != nil {
		s.uid = *opts.uid
	}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

// runningBinary is the path of the running program: a binary that was replaced on disk reads "(deleted)" at the end of the path of
// /proc/self/exe, and the path without it is the new file.
func runningBinary(exe string) string {
	const suffix = " (deleted)"
	if trimmed, ok := strings.CutSuffix(exe, suffix); ok {
		if _, err := os.Stat(trimmed); err == nil {
			return trimmed
		}
	}
	return exe
}

// Socket is the path of the socket.
func (s *Server) Socket() string { return s.socket }

// Folder is the folder of the process, which holds the scripts.
func (s *Server) Folder() string { return s.dir }

// Token is the token the requests must carry. It is a secret of the user's session: it goes into the environment of the git commands
// and nowhere else.
func (s *Server) Token() string { return s.token }

// Script is the path of a script of the folder (ScriptAskpass, ScriptSSHAskpass or ScriptEditor).
func (s *Server) Script(name string) string { return filepath.Join(s.dir, name) }

// Environment is what a git command needs to reach the server. The variables of the askpass programs are included when askpass is
// true (HTTPS: GIT_ASKPASS; ssh: SSH_ASKPASS with SSH_ASKPASS_REQUIRE=force) and GIT_EDITOR when editor is true, with GIT_SEQUENCE_EDITOR
// (the editor of the list of commits of "git rebase -i", which would otherwise be the one of the user's configuration, a program that
// needs a terminal); the socket and the token come with either.
func (s *Server) Environment(askpass, editor bool) []string {
	if !askpass && !editor {
		return nil
	}
	env := []string{EnvSocket + "=" + s.socket, EnvToken + "=" + s.token}
	if askpass {
		// git and ssh run the program without a shell: the path is the value as it is, with no quotes
		env = append(env, "GIT_ASKPASS="+s.Script(ScriptAskpass), "SSH_ASKPASS="+s.Script(ScriptSSHAskpass), "SSH_ASKPASS_REQUIRE=force")
	}
	if editor {
		// git runs the editor through a shell
		script := shellQuote(s.Script(ScriptEditor))
		env = append(env, "GIT_EDITOR="+script, "GIT_SEQUENCE_EDITOR="+script)
	}
	return env
}

// TerminalEnvironment is what a shell in a terminal of the window needs so that the git typed there asks the window (VS Code's
// getTerminalEnv of askpass.ts and gitEditor.ts): GIT_ASKPASS when askpass is true and GIT_EDITOR when editor is true, with the socket and
// the token. It is not Environment: there is no SSH_ASKPASS (ssh in a terminal asks in the terminal, which is where the person is typing),
// no GIT_SEQUENCE_EDITOR (the person's own configuration decides the editor of a todo list), and nothing at all when both are false.
func (s *Server) TerminalEnvironment(askpass, editor bool) []string {
	if !askpass && !editor {
		return nil
	}
	env := []string{EnvSocket + "=" + s.socket, EnvToken + "=" + s.token}
	if askpass {
		env = append(env, "GIT_ASKPASS="+s.Script(ScriptAskpass))
	}
	if editor {
		env = append(env, "GIT_EDITOR="+shellQuote(s.Script(ScriptEditor)))
	}
	return env
}

// Close stops serving, ends the requests that wait, and removes the socket (and the folder of the process when it was the last socket
// in it).
func (s *Server) Close() error {
	s.once.Do(func() {
		s.cancel()
		_ = s.ln.Close()
		s.wg.Wait()
		releaseFolder(s.dir, s.socket)
	})
	return nil
}

// serve accepts connections until the listener closes.
func (s *Server) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.AcceptUnix()
		if err != nil {
			if s.ctx.Err() == nil {
				s.log.Warn("the Git helper stopped accepting connections", "err", err)
			}
			return
		}
		select {
		case s.slots <- struct{}{}:
		default:
			s.log.Warn("a connection to the Git helper was refused: too many are open")
			_ = conn.Close()
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-s.slots }()
			s.serveConn(conn)
		}()
	}
}

// serveConn serves one connection: one request, one reply.
func (s *Server) serveConn(conn *net.UnixConn) {
	defer func() { _ = conn.Close() }()
	req, err := s.accept(conn)
	if err != nil {
		s.log.Warn("a connection to the Git helper was refused", "err", err)
		return
	}
	// the client that goes away (git was cancelled) ends the request: it sends nothing more, so a read returns when it closes
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	go func() {
		var b [1]byte
		_, _ = conn.Read(b[:])
		cancel()
	}()
	reply := s.opts.Handler(ctx, req)
	if ctx.Err() != nil {
		return
	}
	if err := conn.SetWriteDeadline(time.Now().Add(DefaultReplyTimeout)); err != nil {
		s.log.Warn("the Git helper cannot answer", "err", errors.NewErr("GitHelperWriteFailed", err))
		return
	}
	if err := writeJSON(conn, reply); err != nil {
		s.log.Warn("the Git helper cannot answer", "err", err)
	}
}

// accept checks the peer and reads the request of a connection. The error has the tags GitHelperPeerUnknown, GitHelperPeerWrong,
// GitHelperLineTooLong, GitHelperLineUnreadable, GitHelperRequestInvalid and GitHelperTokenWrong.
func (s *Server) accept(conn *net.UnixConn) (Request, error) {
	uid, err := peerUID(conn)
	if err != nil {
		return Request{}, err
	}
	if uid != s.uid {
		return Request{}, errors.New("GitHelperPeerWrong", "the peer of the connection is not the user that runs Emetos", "uid", uid)
	}
	if err := conn.SetReadDeadline(time.Now().Add(s.opts.RequestTimeout)); err != nil {
		return Request{}, errors.NewErr("GitHelperLineUnreadable", err)
	}
	line, err := readLine(conn, MaxRequestBytes)
	if err != nil {
		return Request{}, err
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Request{}, errors.NewErr("GitHelperRequestInvalid", err)
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(s.token)) != 1 {
		return Request{}, errors.New("GitHelperTokenWrong", "the request has the wrong token")
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return Request{}, errors.NewErr("GitHelperLineUnreadable", err)
	}
	return req, nil
}
