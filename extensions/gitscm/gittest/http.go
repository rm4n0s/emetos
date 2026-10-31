package gittest

import (
	"github.com/rm4n0s/emetos/extensions/gitscm/githttp"
)

// HTTPServer is the local test server of HTTPBackend: git's own "git http-backend" behind a Go handler on 127.0.0.1.
type HTTPServer struct {
	// URL is the address of the server, "http://127.0.0.1:<port>"; a repository "name.git" of the served folder is at
	// URL + "/name.git".
	URL string

	server *githttp.Server
}

// Requests returns one line for each request the server has answered, "GET /x.git/info/refs?service=git-upload-pack 401".
func (s *HTTPServer) Requests() []string { return s.server.Requests() }

// HTTPBackend serves the bare repositories under root over HTTP on 127.0.0.1, with git http-backend run as a CGI program.
// When user is not empty the server answers 401 until a request carries that user and password in Basic authentication
// (HTTP authentication tests). Pushes work for the repositories made by Env.InitBare. The server is closed when the test
// ends. It listens on the loopback interface only; nothing leaves the machine.
func (e *Env) HTTPBackend(root, user, password string) *HTTPServer {
	e.t.Helper()
	srv, err := githttp.Start(githttp.Options{GitPath: e.GitPath, Root: root, User: user, Password: password, Env: e.Vars()})
	if err != nil {
		e.t.Fatalf("gittest: %v", err)
	}
	e.t.Cleanup(srv.Close)
	return &HTTPServer{URL: srv.URL, server: srv}
}
