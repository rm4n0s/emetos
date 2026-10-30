package gittest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"strings"
	"sync"
)

// HTTPServer is the local test server of HTTPBackend: git's own "git http-backend" behind a Go handler on 127.0.0.1.
type HTTPServer struct {
	// URL is the address of the server, "http://127.0.0.1:<port>"; a repository "name.git" of the served folder is at
	// URL + "/name.git".
	URL string

	server *httptest.Server
	mu     sync.Mutex
	log    []string
}

// Requests returns one line for each request the server has answered, "GET /x.git/info/refs?service=git-upload-pack 401".
func (s *HTTPServer) Requests() []string {
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

// HTTPBackend serves the bare repositories under root over HTTP on 127.0.0.1, with git http-backend run as a CGI program.
// When user is not empty the server answers 401 until a request carries that user and password in Basic authentication
// (HTTP authentication tests). Pushes work for the repositories made by Env.InitBare. The server is closed when the test
// ends. It listens on the loopback interface only; nothing leaves the machine.
func (e *Env) HTTPBackend(root, user, password string) *HTTPServer {
	e.t.Helper()
	s := &HTTPServer{}
	cgiHandler := &cgi.Handler{
		Path: e.GitPath,
		Args: []string{"http-backend"},
		Dir:  root,
		// The error output of git http-backend is dropped: a test sees git's own messages on the client side.
		Stderr: io.Discard,
		Env: append(e.Vars(),
			"GIT_PROJECT_ROOT="+root,
			"GIT_HTTP_EXPORT_ALL=1",
		),
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			s.mu.Lock()
			s.log = append(s.log, fmt.Sprintf("%s %s %d", r.Method, r.URL.RequestURI(), rec.status))
			s.mu.Unlock()
		}()
		if user != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != password {
				rec.Header().Set("WWW-Authenticate", `Basic realm="git"`)
				http.Error(rec, "authentication required", http.StatusUnauthorized)
				return
			}
		}
		h := *cgiHandler
		if user != "" {
			h.Env = append(append([]string(nil), cgiHandler.Env...), "REMOTE_USER="+user)
		}
		h.ServeHTTP(rec, r)
	})
	s.server = httptest.NewServer(handler)
	s.URL = strings.TrimRight(s.server.URL, "/")
	e.t.Cleanup(s.server.Close)
	return s
}
