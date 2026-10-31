// Package githttp serves bare git repositories over HTTP on 127.0.0.1 with git's own "git http-backend", with Basic authentication when a
// user is given. It is the remote of the tests of the Git extension (extensions/gitscm/gittest.HTTPBackend) and of the dev command
// dev.git.testserver that makes the proof image of the password prompt (SPEC §15.2). It listens on the loopback interface only; nothing
// leaves the machine, and nothing is stored: the repositories are the ones in the folder it is given.
package githttp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cgi"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// Options configure Start.
type Options struct {
	// GitPath is the git program ("git http-backend" is run as a CGI program). Required.
	GitPath string
	// Root is the folder that holds the bare repositories; a repository "name.git" of it is at URL + "/name.git". Required.
	Root string
	// User and Password, when User is not empty, are the credentials every request must carry in Basic authentication: the server
	// answers 401 until it gets them.
	User, Password string
	// Env is the environment of git http-backend ("KEY=VALUE"); GIT_PROJECT_ROOT and GIT_HTTP_EXPORT_ALL are added.
	Env []string

	// network is the address the server listens on, "127.0.0.1:0" when empty; a test gives a bad one.
	network string
}

// Server is a running server.
type Server struct {
	// URL is the address of the server, "http://127.0.0.1:<port>".
	URL string

	srv  *http.Server
	mu   sync.Mutex
	log  []string
	once sync.Once
}

// Requests returns one line for each request the server has answered, "GET /x.git/info/refs?service=git-upload-pack 401".
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

// statusRecorder remembers the status a handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Start starts the server on a free port of 127.0.0.1. The error has the tags GitHTTPOptionsInvalid and GitHTTPListenFailed.
func Start(o Options) (*Server, error) {
	if o.GitPath == "" || o.Root == "" {
		return nil, errors.New("GitHTTPOptionsInvalid", "the server needs the git program and the folder of the repositories", "git", o.GitPath, "root", o.Root)
	}
	addr := o.network
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, errors.NewErr("GitHTTPListenFailed", err).SetMetadata("address", addr)
	}
	s := &Server{URL: "http://" + ln.Addr().String()}
	cgiHandler := &cgi.Handler{
		Path: o.GitPath,
		Args: []string{"http-backend"},
		Dir:  o.Root,
		// The error output of git http-backend is dropped: a client sees git's own messages.
		Stderr: io.Discard,
		Env:    append(append([]string(nil), o.Env...), "GIT_PROJECT_ROOT="+o.Root, "GIT_HTTP_EXPORT_ALL=1"),
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			s.mu.Lock()
			s.log = append(s.log, fmt.Sprintf("%s %s %d", r.Method, r.URL.RequestURI(), rec.status))
			s.mu.Unlock()
		}()
		if o.User != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != o.User || p != o.Password {
				rec.Header().Set("WWW-Authenticate", `Basic realm="git"`)
				http.Error(rec, "authentication required", http.StatusUnauthorized)
				return
			}
		}
		h := *cgiHandler
		if o.User != "" {
			h.Env = append(append([]string(nil), cgiHandler.Env...), "REMOTE_USER="+o.User)
		}
		h.ServeHTTP(rec, r)
	})
	s.srv = &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		// Serve returns ErrServerClosed after Close; any other end of the loop leaves a server that answers nothing, which the client sees
		_ = s.srv.Serve(ln)
	}()
	return s, nil
}

// Close stops the server and the requests it is serving. It may be called more than once.
func (s *Server) Close() {
	s.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(ctx); err != nil {
			_ = s.srv.Close()
		}
	})
}

// Local says whether a URL is one of this package's servers: http on a loopback address.
func Local(url string) bool {
	return strings.HasPrefix(url, "http://127.0.0.1:")
}
