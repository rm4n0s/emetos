package git

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// Clone and Init against real git, local repositories and the local test server (step 24.16). Nothing here touches a network.

func TestCloneFolderName(t *testing.T) {
	tests := []struct {
		name, url, want string
	}{
		{"https with .git", "https://example.com/org/project.git", "project"},
		{"https without .git", "https://example.com/org/project", "project"},
		{"trailing slashes", "https://example.com/org/project.git///", "project"},
		{"scp-like with a path", "git@example.com:org/project.git", "project"},
		{"scp-like without a slash", "example.com:project.git", "project"},
		{"ssh", "ssh://git@example.com:2222/srv/project.git", "project"},
		{"a file URL", "file:///srv/git/project.git", "project"},
		{"a path", "/srv/git/project.git", "project"},
		{"a relative path", "../project", "project"},
		{"escaped characters", "https://example.com/org/my%20project.git", "my project"},
		{"an escaped slash cannot leave the folder", "https://example.com/..%2F..%2Fescape.git", "escape"},
		{"an escaped dot dot", "https://example.com/org/%2E%2E", "repository"},
		{"a dot dot", "https://example.com/org/..", "repository"},
		{"a name that is only .git", "https://example.com/.git", "repository"},
		{"a host only", "https://example.com/", "example.com"},
		{"an escaped control character", "https://example.com/a%00b.git", "repository"},
		{"spaces around", "  https://example.com/org/project.git  ", "project"},
		{"empty", "", "repository"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CloneFolderName(tc.url); got != tc.want {
				t.Errorf("CloneFolderName(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

// cloneWorld is a bare remote with two commits and a runner for the world.
type cloneWorld struct {
	env    *gittest.Env
	remote *gittest.Repo
	runner *Runner
	logs   *lines
}

// lines collects the Git channel's lines.
type lines struct {
	mu   sync.Mutex
	list []string
}

func (l *lines) add(s string) {
	l.mu.Lock()
	l.list = append(l.list, s)
	l.mu.Unlock()
}

func (l *lines) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.list, "\n")
}

func newCloneWorld(t *testing.T) *cloneWorld {
	t.Helper()
	e := gittest.NewEnv(t)
	work := e.Init("origin-work")
	work.Write("a.txt", "one\n")
	work.Commit("one")
	bare := work.AddRemote("origin")
	work.Git("push", "-q", "origin", "main")
	work.Write("b.txt", "two\n")
	work.Commit("two")
	work.Git("push", "-q", "origin", "main")
	logs := &lines{}
	return &cloneWorld{env: e, remote: bare, logs: logs, runner: NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: e.Vars(), Logger: logs.add})}
}

func TestCloneNamesTheFolderAfterTheRepositoryAndTakesTheNextFreeName(t *testing.T) {
	w := newCloneWorld(t)
	parent := filepath.Join(w.env.Root, "clones")
	var got []string
	for i := 0; i < 3; i++ {
		path, err := w.runner.Clone(context.Background(), w.remote.Dir, parent, CloneOptions{})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.Base(path))
		if data, err := os.ReadFile(filepath.Join(path, "b.txt")); err != nil || string(data) != "two\n" {
			t.Errorf("the clone has no checkout: %q, %v", data, err)
		}
	}
	if want := []string{"origin", "origin-1", "origin-2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("folders %v, want %v (the parent folder was made on the way)", got, want)
	}
}

func TestCloneChecksOutTheRefAndLeavesTheFolderForAFailedRef(t *testing.T) {
	w := newCloneWorld(t)
	work := &gittest.Repo{Env: w.env, Dir: filepath.Join(w.env.Work, "origin-work")}
	work.Branch("feature", "main")
	work.Checkout("feature")
	work.Write("f.txt", "feature\n")
	work.Commit("feature work")
	work.Git("push", "-q", "origin", "feature")
	work.Tag("v1", "first")
	work.Git("push", "-q", "origin", "v1")
	parent := t.TempDir()
	path, err := w.runner.Clone(context.Background(), w.remote.Dir, parent, CloneOptions{Ref: "feature", TargetName: "on-feature"})
	if err != nil {
		t.Fatal(err)
	}
	if got := w.env.Git(path, "rev-parse", "--abbrev-ref", "HEAD"); got != "feature" {
		t.Errorf("HEAD is %q", got)
	}
	tagged, err := w.runner.Clone(context.Background(), w.remote.Dir, parent, CloneOptions{Ref: "v1", TargetName: "at-tag"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tagged, "f.txt")); err != nil {
		t.Errorf("the tag's file is missing: %v", err)
	}
	_, err = w.runner.Clone(context.Background(), w.remote.Dir, parent, CloneOptions{Ref: "no-such-branch", TargetName: "bad-ref"})
	if err == nil {
		t.Fatal("a branch the remote lacks was cloned")
	}
	if _, statErr := os.Stat(filepath.Join(parent, "bad-ref")); statErr == nil {
		t.Error("a failed clone left its folder")
	}
	wantRoute(t, err, "Runner.Run")
	_, err = w.runner.Clone(context.Background(), w.remote.Dir, parent, CloneOptions{Ref: "--upload-pack=x"})
	wantRoute(t, err, "Runner.Clone->refName.GitRefInvalid")
}

func TestCloneRecursiveCheckoutsTheSubmodules(t *testing.T) {
	w := newCloneWorld(t)
	sub := w.env.Init("lib")
	sub.Write("lib.go", "package lib\n")
	sub.Commit("lib")
	main := w.env.Init("app")
	main.Write("main.go", "package main\n")
	main.Commit("app")
	main.Submodule("vendor/lib", sub)
	for _, recursive := range []bool{false, true} {
		path, err := w.runner.Clone(context.Background(), main.Dir, t.TempDir(), CloneOptions{Recursive: recursive})
		if err != nil {
			t.Fatal(err)
		}
		_, statErr := os.Stat(filepath.Join(path, "vendor", "lib", "lib.go"))
		if (statErr == nil) != recursive {
			t.Errorf("recursive %v: the submodule's file exists: %v", recursive, statErr == nil)
		}
	}
}

func TestCloneReportsTheProgressOfTheTransfer(t *testing.T) {
	w := newCloneWorld(t)
	var mu sync.Mutex
	var got []Progress
	_, err := w.runner.Clone(context.Background(), "file://"+w.remote.Dir, t.TempDir(), CloneOptions{Progress: func(p Progress) {
		mu.Lock()
		got = append(got, p)
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	phases := map[string]bool{}
	for _, p := range got {
		phases[p.Phase] = true
	}
	if !phases["Receiving objects"] {
		t.Errorf("no progress of the transfer was reported: %+v", got)
	}
	if strings.Contains(w.logs.text(), "Receiving objects:") {
		t.Errorf("the progress lines reached the Git channel:\n%s", w.logs.text())
	}
}

func TestCloneRefusesWhatIsNotACloneTargetBeforeStartingGit(t *testing.T) {
	w := newCloneWorld(t)
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "taken", "keep.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(parent, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		source string
		parent string
		opts   CloneOptions
		route  string
	}{
		{"a parent that cannot be made", w.remote.Dir, filepath.Join(file, "inside"), CloneOptions{}, "Runner.Clone.GitCloneParentFailed"},
		{"a folder with files", w.remote.Dir, parent, CloneOptions{TargetName: "taken"}, "Runner.Clone.GitCloneTargetExists"},
		{"an empty source", "  ", parent, CloneOptions{}, "Runner.Clone.GitCloneURLInvalid"},
		{"a source that is an option", "--upload-pack=touch x", parent, CloneOptions{}, "Runner.Clone.GitCloneURLInvalid"},
		{"a source with a control character", "https://example.com/a\nb", parent, CloneOptions{}, "Runner.Clone.GitCloneURLInvalid"},
		{"a scheme git cannot clone from", "ftp://example.com/x.git", parent, CloneOptions{}, "Runner.Clone.GitCloneURLInvalid"},
		{"a relative parent", w.remote.Dir, "relative/dir", CloneOptions{}, "Runner.Clone.GitCloneParentInvalid"},
		{"a target that is a path", w.remote.Dir, parent, CloneOptions{TargetName: "../escape"}, "Runner.Clone.GitCloneTargetInvalid"},
		{"a target that is dot dot", w.remote.Dir, parent, CloneOptions{TargetName: ".."}, "Runner.Clone.GitCloneTargetInvalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.runner.Clone(context.Background(), tc.source, tc.parent, tc.opts)
			wantRoute(t, err, tc.route)
		})
	}
	if data, err := os.ReadFile(filepath.Join(parent, "taken", "keep.txt")); err != nil || string(data) != "keep\n" {
		t.Errorf("the folder with files was touched: %q, %v", data, err)
	}
	// an empty folder named by the caller is used
	if path, err := w.runner.Clone(context.Background(), w.remote.Dir, parent, CloneOptions{TargetName: "empty"}); err != nil || path != filepath.Join(parent, "empty") {
		t.Errorf("an empty target: %q, %v", path, err)
	}
}

func TestCloneOfARepositoryThatIsNotThereFailsAndLeavesNoFolder(t *testing.T) {
	w := newCloneWorld(t)
	parent := t.TempDir()
	_, err := w.runner.Clone(context.Background(), filepath.Join(w.env.Root, "no-such.git"), parent, CloneOptions{})
	if err == nil {
		t.Fatal("a repository that is not there was cloned")
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		t.Errorf("the parent folder holds %v", entries)
	}
}

func TestCloneReadsAPathFromTheParentFolderAndTheHomeFolder(t *testing.T) {
	w := newCloneWorld(t)
	parent := filepath.Join(w.env.Root, "dst")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(w.remote.Dir, filepath.Join(w.env.Root, "src.git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(w.env.Root, "src.git"), filepath.Join(w.env.Home, "home.git")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, source string }{
		{"relative to the parent folder", "../src.git"},
		{"a name in the parent folder's parent", "./../src.git"},
		{"the home folder", "~/home.git"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, err := w.runner.Clone(context.Background(), tc.source, parent, CloneOptions{TargetName: strings.NewReplacer("/", "_", ".", "_", "~", "h").Replace(tc.source)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(path, "b.txt")); err != nil {
				t.Errorf("no checkout: %v", err)
			}
		})
	}
}

// slowGit writes a git that makes the target folder with a file in it, as a clone that has begun does, and then waits. It records its
// pid.
func slowGit(t *testing.T) (program, pidFile string) {
	t.Helper()
	dir := t.TempDir()
	program, pidFile = filepath.Join(dir, "git"), filepath.Join(dir, "pid")
	script := `#!/bin/sh
for a in "$@"; do last=$a; done
mkdir -p "$last/.git"
echo partial > "$last/partial.txt"
sleep 30 &
echo $! > "` + pidFile + `"
wait
`
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return program, pidFile
}

func TestCancellingACloneRemovesTheFolderAndStopsGit(t *testing.T) {
	for _, tc := range []struct {
		name         string
		existedEmpty bool
	}{
		{"a folder the clone made", false},
		{"an empty folder that was there", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			program, pidFile := slowGit(t)
			parent := t.TempDir()
			opts := CloneOptions{}
			if tc.existedEmpty {
				opts.TargetName = "mine"
				if err := os.MkdirAll(filepath.Join(parent, "mine"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			r := NewRunner(Config{Trust: trustAll, Path: program, Env: []string{"PATH=" + os.Getenv("PATH")}, KillGrace: 300 * time.Millisecond})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := r.Clone(ctx, "https://example.com/repo.git", parent, opts)
				done <- err
			}()
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(pidFile); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the fake git did not start")
				}
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
			var err error
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("Clone did not return after the cancel")
			}
			wantRoute(t, err, "Runner.Clone->Runner.Run->Runner.execute.GitCancelled")
			entries, _ := os.ReadDir(parent)
			if tc.existedEmpty {
				inner, _ := os.ReadDir(filepath.Join(parent, "mine"))
				if len(entries) != 1 || len(inner) != 0 {
					t.Errorf("the empty folder holds %v, the parent %v", inner, entries)
				}
			} else if len(entries) != 0 {
				t.Errorf("the cancelled clone left %v", entries)
			}
			data, _ := os.ReadFile(pidFile)
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr != nil {
				t.Fatalf("pid file %q: %v", data, convErr)
			}
			gone := false
			for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
				if syscall.Kill(pid, 0) == syscall.ESRCH {
					gone = true
					break
				}
			}
			if !gone {
				t.Errorf("the process %d of the clone is still running", pid)
			}
		})
	}
}

func TestACloneOverHTTPAsksForThePasswordAndNeverLogsIt(t *testing.T) {
	w := newCloneWorld(t)
	const secret = "s3cret-in-the-url"
	srv := w.env.HTTPBackend(filepath.Dir(w.remote.Dir), "ada", "right-password")
	url := strings.Replace(srv.URL, "http://", "http://ada:"+secret+"@", 1) + "/origin.git"
	parent := t.TempDir()
	_, err := w.runner.Clone(context.Background(), url, parent, CloneOptions{})
	if err == nil {
		t.Fatal("a wrong password cloned")
	}
	appErr := routeOf(t, err, "Runner.Run")
	if appErr.Tag != "AuthenticationFailed" {
		t.Errorf("tag %s, want AuthenticationFailed", appErr.Tag)
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		t.Errorf("the failed clone left %v", entries)
	}
	for _, text := range []string{w.logs.text(), appErr.Error(), strings.Join(metaStrings(appErr.Metadata), "\n")} {
		if strings.Contains(text, secret) {
			t.Errorf("the password is in %q", text)
		}
	}
	good := strings.Replace(srv.URL, "http://", "http://ada:right-password@", 1) + "/origin.git"
	path, err := w.runner.Clone(context.Background(), good, parent, CloneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "b.txt")); err != nil {
		t.Error(err)
	}
	if strings.Contains(w.logs.text(), "right-password") {
		t.Errorf("the password is in the log:\n%s", w.logs.text())
	}
}

// metaStrings are the values of error metadata that are strings or lists of them.
func metaStrings(m map[string]any) []string {
	var out []string
	for _, v := range m {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case []string:
			out = append(out, x...)
		}
	}
	return out
}

func TestInitMakesARepositoryOnTheDefaultBranch(t *testing.T) {
	e := gittest.NewEnv(t)
	tests := []struct {
		name    string
		env     []string
		opts    InitOptions
		want    string
		useBare bool
	}{
		{"the branch named", e.Vars(), InitOptions{DefaultBranch: "trunk"}, "trunk", false},
		{"git's own default when none is given and init.defaultBranch is not set", withVars(e.Vars(), "GIT_CONFIG_PARAMETERS="), InitOptions{}, "master", false},
		{"init.defaultBranch when none is given", e.Vars(), InitOptions{}, "main", false},
		{"a bare repository", e.Vars(), InitOptions{DefaultBranch: "trunk", Bare: true}, "trunk", true},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(e.Work, "init-"+string(rune('a'+i)), "nested")
			r := NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: tc.env})
			if err := r.Init(context.Background(), dir, tc.opts); err != nil {
				t.Fatal(err)
			}
			head, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
			if tc.useBare {
				head, err = os.ReadFile(filepath.Join(dir, "HEAD"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(head)); got != "ref: refs/heads/"+tc.want {
				t.Errorf("HEAD is %q, want the branch %s", got, tc.want)
			}
		})
	}
}
