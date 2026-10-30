package gittest

import (
	"strings"
	"testing"
)

func TestHTTPBackendServesGitBehindBasicAuthentication(t *testing.T) {
	e := NewEnv(t)
	src := e.Init("src")
	src.Write("a.txt", "a\n")
	src.Commit("one")
	bare := src.AddRemote("origin")
	src.Git("push", "-q", "origin", "main")
	srv := e.HTTPBackend(e.Work, "ada", "s3cret")
	if !strings.HasPrefix(srv.URL, "http://127.0.0.1:") {
		t.Fatalf("the server is not on the loopback interface: %s", srv.URL)
	}
	repoURL := func(userinfo string) string {
		return strings.Replace(srv.URL, "http://", "http://"+userinfo, 1) + "/origin.git"
	}

	t.Run("the right credentials clone", func(t *testing.T) {
		dir := e.Work + "/over-http"
		out, errOut, err := e.GitErr(e.Work, "clone", "-q", repoURL("ada:s3cret@"), dir)
		if err != nil {
			t.Fatalf("clone: %v\n%s%s", err, out, errOut)
		}
		if got := e.Git(dir, "rev-parse", "HEAD"); got != src.Head() {
			t.Errorf("cloned %s, want %s", got, src.Head())
		}
	})

	t.Run("the wrong password is refused", func(t *testing.T) {
		_, errOut, err := e.GitErr(e.Work, "clone", "-q", repoURL("ada:wrong@"), e.Work+"/refused")
		if err == nil || !strings.Contains(errOut, "Authentication failed") {
			t.Errorf("clone with a wrong password: %v\n%s", err, errOut)
		}
	})

	t.Run("no credentials are refused without a prompt", func(t *testing.T) {
		_, errOut, err := e.GitErr(e.Work, "clone", "-q", repoURL(""), e.Work+"/anonymous")
		if err == nil {
			t.Fatalf("clone without credentials worked")
		}
		if !strings.Contains(errOut, "terminal prompts disabled") && !strings.Contains(errOut, "Authentication failed") {
			t.Errorf("unexpected message: %s", errOut)
		}
	})

	t.Run("a push over HTTP reaches the bare repository", func(t *testing.T) {
		dir := e.Work + "/pusher"
		e.Git(e.Work, "clone", "-q", repoURL("ada:s3cret@"), dir)
		e.Trust(dir)
		pusher := &Repo{Env: e, Dir: dir}
		pusher.Write("b.txt", "b\n")
		head := pusher.Commit("two")
		pusher.Git("push", "-q", "origin", "main")
		if got := bare.Git("rev-parse", "main"); got != head {
			t.Errorf("the bare repository is at %s, want %s", got, head)
		}
	})

	if requests := srv.Requests(); len(requests) == 0 || !containsPrefix(requests, "GET /origin.git/info/refs?service=git-upload-pack 401") {
		t.Errorf("the server did not log the 401 of the first request: %v", requests)
	}
}

func containsPrefix(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func TestHTTPBackendWithoutCredentialsServesAnyone(t *testing.T) {
	e := NewEnv(t)
	src := e.Init("src")
	src.Write("a.txt", "a\n")
	src.Commit("one")
	src.AddRemote("origin")
	src.Git("push", "-q", "origin", "main")
	srv := e.HTTPBackend(e.Work, "", "")
	e.Git(e.Work, "clone", "-q", srv.URL+"/origin.git", e.Work+"/copy")
}
