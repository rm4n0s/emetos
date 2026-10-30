package gittest

import (
	"os"
	"strings"
	"testing"
)

func TestRepoHelpersBuildRepositories(t *testing.T) {
	t.Run("branches, merge and tags", func(t *testing.T) {
		r := New(t)
		r.Write("a.txt", "a\n")
		base := r.Commit("base")
		r.Branch("topic", "")
		r.Checkout("topic")
		r.Write("b.txt", "b\n")
		r.Commit("topic change")
		r.Checkout("main")
		r.Write("c.txt", "c\n")
		r.Commit("main change")
		mergeBase := r.Git("merge-base", "main", "topic")
		if err := r.Merge("topic"); err != nil {
			t.Fatalf("merge: %v", err)
		}
		r.Tag("light", "")
		r.Tag("annotated", "Release")
		if got := r.Git("cat-file", "-t", "annotated"); got != "tag" {
			t.Errorf("annotated tag type = %q", got)
		}
		if got := r.Git("cat-file", "-t", "light"); got != "commit" {
			t.Errorf("lightweight tag type = %q", got)
		}
		if got := r.Git("rev-list", "--parents", "-n1", "HEAD"); len(strings.Fields(got)) != 3 {
			t.Errorf("HEAD is not a merge: %q", got)
		}
		if got := strings.Join(r.Files(), ","); got != "a.txt,b.txt,c.txt" {
			t.Errorf("files = %s", got)
		}
		if mergeBase != base {
			t.Error("the merge base is not the base commit")
		}
	})

	t.Run("conflicting branches", func(t *testing.T) {
		r := New(t)
		ours, theirs := r.ConflictingBranches()
		if ours != "ours" || theirs != "theirs" || r.Git("rev-parse", "--abbrev-ref", "HEAD") != "ours" {
			t.Fatalf("branches %q %q", ours, theirs)
		}
		if err := r.Merge(theirs); err == nil {
			t.Fatal("the merge did not conflict")
		}
		if got := r.Git("diff", "--name-only", "--diff-filter=U"); got != "src/loader.go" {
			t.Errorf("unmerged files = %q", got)
		}
		if got := r.Git("status", "--porcelain"); got != "UU src/loader.go" {
			t.Errorf("status = %q", got)
		}
	})

	t.Run("stash", func(t *testing.T) {
		r := New(t)
		r.Write("a.txt", "a\n")
		r.Commit("base")
		r.Write("a.txt", "changed\n")
		r.Write("u.txt", "untracked\n")
		r.Stash("keep it", true)
		if got := r.Git("stash", "list", "--format=%gs"); !strings.HasSuffix(got, "keep it") {
			t.Errorf("stash list = %q", got)
		}
		if got := r.Git("status", "--porcelain"); got != "" {
			t.Errorf("status after the stash = %q", got)
		}
	})

	t.Run("a remote, a clone and a push", func(t *testing.T) {
		r := New(t)
		r.Write("a.txt", "a\n")
		r.Commit("base")
		bare := r.AddRemote("origin")
		r.Git("push", "-q", "-u", "origin", "main")
		clone := bare.Clone("copy")
		if clone.Head() != r.Head() {
			t.Error("the clone differs from the repository")
		}
		if !bare.Bare {
			t.Error("the remote is not bare")
		}
		if got := r.Git("rev-parse", "--abbrev-ref", "main@{upstream}"); got != "origin/main" {
			t.Errorf("upstream = %q", got)
		}
	})

	t.Run("a submodule", func(t *testing.T) {
		r := New(t)
		sub := r.Env.Init("lib")
		sub.Write("lib.go", "package lib\n")
		sub.Commit("lib")
		r.Write("a.txt", "a\n")
		r.Commit("base")
		r.Submodule("vendor/lib", sub)
		if got := r.Git("submodule", "status"); !strings.Contains(got, "vendor/lib") {
			t.Errorf("submodule status = %q", got)
		}
	})

	t.Run("a hook runs", func(t *testing.T) {
		r := New(t)
		r.Hook("pre-commit", "#!/bin/sh\necho blocked >&2\nexit 1\n")
		r.Write("a.txt", "a\n")
		r.Git("add", "-A")
		if _, errOut, err := r.GitErr("commit", "-q", "-m", "x"); err == nil || !strings.Contains(errOut, "blocked") {
			t.Errorf("the hook did not stop the commit: %v %q", err, errOut)
		}
	})

	t.Run("no hook runs by default", func(t *testing.T) {
		r := New(t)
		// A hook in the repository's own folder is not run, because core.hooksPath is an empty folder.
		r.Write(".git/hooks/pre-commit", "#!/bin/sh\nexit 1\n")
		if err := chmodExec(r.Path(".git/hooks/pre-commit")); err != nil {
			t.Fatal(err)
		}
		r.Write("a.txt", "a\n")
		r.Commit("works")
	})
}

func chmodExec(path string) error { return os.Chmod(path, 0o755) }
