package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// trustSet is a TrustStore of exact roots.
type trustSet map[string]bool

func (s trustSet) IsTrusted(root string) bool { return s[root] }

// TestRunnerRefusesAnUntrustedRepositoryBeforeAnyProcess runs every way of putting a command in a repository against a fake git that
// records each call: nothing is recorded, and the error is GitRepositoryNotTrusted, until the repository is trusted.
func TestRunnerRefusesAnUntrustedRepositoryBeforeAnyProcess(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("src/a.go", "package a\n")
	repo.Commit("Add a")
	other := e.Init("other")
	outside := filepath.Join(e.Work, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(repo.Dir, ".git")
	tests := []struct {
		name    string
		cmd     Command
		extra   []string
		refused bool
	}{
		{"the root", Command{Dir: repo.Dir, Args: []string{"status"}}, nil, true},
		{"a folder inside", Command{Dir: filepath.Join(repo.Dir, "src"), Args: []string{"status"}}, nil, true},
		{"-C with the other repository as the folder", Command{Dir: other.Dir, Args: []string{"-C", repo.Dir, "status"}}, nil, true},
		{"-C relative to the folder", Command{Dir: outside, Args: []string{"-C", "../repo", "status"}}, nil, true},
		{"--git-dir", Command{Dir: outside, Args: []string{"--git-dir", gitDir, "status"}}, nil, true},
		{"--git-dir=", Command{Dir: outside, Args: []string{"--git-dir=" + gitDir, "status"}}, nil, true},
		{"--work-tree=", Command{Dir: outside, Args: []string{"--work-tree=" + repo.Dir, "status"}}, nil, true},
		{"GIT_DIR of the command", Command{Dir: outside, Args: []string{"status"}, Env: []string{"GIT_DIR=" + gitDir}}, nil, true},
		{"GIT_WORK_TREE of the command", Command{Dir: outside, Args: []string{"status"}, Env: []string{"GIT_WORK_TREE=" + repo.Dir}}, nil, true},
		{"GIT_DIR of the runner", Command{Dir: outside, Args: []string{"status"}}, []string{"GIT_DIR=" + gitDir}, true},
		{"GIT_DIR pointing into .git/modules or worktrees", Command{Dir: outside, Args: []string{"status"}, Env: []string{"GIT_DIR=" + filepath.Join(gitDir, "worktrees", "w")}}, nil, true},
		{"outside every repository", Command{Dir: outside, Args: []string{"--version"}}, nil, false},
		{"no folder (the root of the file system)", Command{Args: []string{"--version"}}, nil, false},
		{"a folder that does not exist", Command{Dir: filepath.Join(e.Work, "gone"), Args: []string{"status"}}, nil, false},
		{"the trusted repository", Command{Dir: other.Dir, Args: []string{"status"}}, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := filepath.Join(t.TempDir(), "calls.json")
			path, fakeEnv := gittest.FakeGit(t, "record "+record+"\nstdout \"ok\\n\"")
			r := NewRunner(Config{Path: path, Env: []string{"PATH=" + os.Getenv("PATH"), fakeEnv}, Extra: tc.extra,
				Trust: TrustedBy(trustSet{other.Dir: true})})
			_, err := r.Run(context.Background(), tc.cmd)
			_, statErr := os.Stat(record)
			started := statErr == nil
			switch {
			case tc.refused:
				appErr, ok := errors.FromError(err)
				if !ok || !appErr.HasRoute("Runner.Run->Runner.execute.GitRepositoryNotTrusted") {
					t.Fatalf("got %v, want GitRepositoryNotTrusted", err)
				}
				if appErr.Metadata["root"] != repo.Dir {
					t.Errorf("root = %v, want %s", appErr.Metadata["root"], repo.Dir)
				}
				if started {
					t.Error("a process was started")
				}
			case tc.name == "a folder that does not exist":
				wantRoute(t, err, "Runner.Run->Runner.execute.GitDirMissing")
			default:
				if err != nil {
					t.Fatal(err)
				}
				if !started {
					t.Error("the command did not run")
				}
			}
		})
	}
}

func TestRunnerWithoutATrustFunctionTrustsNoRepository(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	r := NewRunner(Config{Path: e.GitPath, Env: e.Vars()})
	_, err := r.Run(context.Background(), Command{Dir: repo.Dir, Args: []string{"status"}})
	wantRoute(t, err, "Runner.Run->Runner.execute.GitRepositoryNotTrusted")
	if _, err := r.Run(context.Background(), Command{Dir: e.Work, Args: []string{"--version"}}); err != nil {
		t.Errorf("a command outside every repository: %v", err)
	}
}

func TestTrustedByInheritsFromTheParentOfASubmoduleAndAWorktree(t *testing.T) {
	e := gittest.NewEnv(t)
	main := e.Init("main")
	main.Write("a.txt", "a\n")
	main.Commit("Add a")
	sub := e.Init("sub")
	sub.Write("s.txt", "s\n")
	sub.Commit("Add s")
	main.Submodule("vendor/sub", sub)
	wt := filepath.Join(e.Work, "feature")
	main.Git("worktree", "add", "-q", wt, "-b", "feature")
	subLoc, _ := FindRepository(filepath.Join(main.Dir, "vendor", "sub"))
	wtLoc, _ := FindRepository(wt)
	mainLoc, _ := FindRepository(main.Dir)
	for _, tc := range []struct {
		name  string
		store trustSet
		loc   Location
		want  bool
	}{
		{"the submodule of a trusted repository", trustSet{main.Dir: true}, subLoc, true},
		{"the worktree of a trusted repository", trustSet{main.Dir: true}, wtLoc, true},
		{"the submodule alone", trustSet{subLoc.Root: true}, subLoc, true},
		{"the parent is not trusted because the submodule is", trustSet{subLoc.Root: true}, mainLoc, false},
		{"the worktree of an untrusted repository", trustSet{}, wtLoc, false},
	} {
		if got := TrustedBy(tc.store)(tc.loc); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}
