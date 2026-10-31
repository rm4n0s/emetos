package githttp

import (
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestStartFailures(t *testing.T) {
	_, err := Start(Options{})
	wantRoute(t, err, "Start.GitHTTPOptionsInvalid")
	_, err = Start(Options{GitPath: "git", Root: t.TempDir(), network: "127.0.0.1:99999"})
	wantRoute(t, err, "Start.GitHTTPListenFailed")
}

func TestTheServerAsksForCredentialsOnTheLoopbackOnly(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	s, err := Start(Options{GitPath: git, Root: t.TempDir(), User: "ada", Password: "right", Env: []string{"PATH=" + os.Getenv("PATH")}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !Local(s.URL) {
		t.Fatalf("the server is not on the loopback interface: %s", s.URL)
	}
	for _, tc := range []struct {
		user, password string
		want           int
	}{{"", "", 401}, {"ada", "wrong", 401}, {"bob", "right", 401}, {"ada", "right", 404}} {
		req, _ := http.NewRequest("GET", s.URL+"/none.git/info/refs?service=git-upload-pack", nil)
		if tc.user != "" {
			req.SetBasicAuth(tc.user, tc.password)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%q/%q: status %d, want %d", tc.user, tc.password, resp.StatusCode, tc.want)
		}
	}
	if got := s.Requests(); len(got) != 4 || !strings.HasSuffix(got[0], " 401") {
		t.Errorf("requests %v", got)
	}
	s.Close()
	s.Close()
	if Local("http://example.com") || !Local("http://127.0.0.1:1") {
		t.Error("Local")
	}
}
