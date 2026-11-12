package gitscm

import (
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// The dev command that makes the proof of the password prompt (step 24.9, SPEC §15.2).
func TestTheDevTestServerMakesOriginAskForAPassword(t *testing.T) {
	if !foundation.DebugMode && !devBuild {
		t.Skip("the dev test server exists only in builds that run dev commands")
	}
	e := gittest.NewEnv(t)
	repo := e.Init("small")
	repo.Write("a.txt", "one\n")
	repo.Commit("one")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)

	if err := r.api.Run(CommandDevTestServer, map[string]any{"user": "ada", "password": typed}); err != nil {
		t.Fatal(err)
	}
	url := repo.Git("remote", "get-url", "origin")
	if !strings.HasPrefix(url, "http://ada@127.0.0.1:") || !strings.HasSuffix(url, "/small.git") {
		t.Fatalf("origin is %s", url)
	}
	// a second call replaces the remote
	if err := r.api.Run(CommandDevTestServer, map[string]any{"password": typed}); err != nil {
		t.Fatal(err)
	}
	if again := repo.Git("remote", "get-url", "origin"); again == url {
		t.Errorf("origin is the same server %s after a second call", again)
	}

	if err := r.api.Run("git.fetch", nil); err != nil {
		t.Fatal(err)
	}
	in := r.nextInput(0)
	host := strings.TrimSuffix(strings.TrimPrefix(repo.Git("remote", "get-url", "origin"), "http://ada@"), "/small.git")
	if o := in.Options(); o.Prompt != "Password for 'http://ada@"+host+"'" || o.Message != "Git: http://"+host || !o.Password {
		t.Errorf("the box is %+v", o)
	}
	in.Accept(typed)
	r.idle(rp)
	if notes := r.api.Notes(); len(notes) != 0 {
		t.Errorf("notifications %+v", notes)
	}
	if got := repo.Git("rev-parse", "origin/main"); got != repo.Head() {
		t.Errorf("origin/main is %s, want %s", got, repo.Head())
	}
}

func TestTheDevTestServerFailures(t *testing.T) {
	if !foundation.DebugMode && !devBuild {
		t.Skip("the dev test server exists only in builds that run dev commands")
	}
	t.Run("no repository", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := newRig(t, e, rigOptions{})
		r.settle()
		wantRoute(t, r.api.Run(CommandDevTestServer, nil), "cmdDevTestServer.DevGitTestServerNoRepository")
	})
	t.Run("no temporary folder", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("small")
		repo.Write("a.txt", "one\n")
		repo.Commit("one")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		t.Setenv("TMPDIR", e.Root+"/does/not/exist")
		wantRoute(t, r.api.Run(CommandDevTestServer, nil), "cmdDevTestServer.DevGitTestServerFailed")
	})
}
