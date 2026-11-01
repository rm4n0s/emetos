package git

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Ported from VS Code: extensions/git/src/test/worktreeSymlink.test.ts (5 cases of 6; skipped: "symlinking worktree directories is best
// effort", which drives a private method of the TypeScript Repository with a harness, the Go counterpart is a test of the extension)

func TestWorktreeSymlinkFoldersMatchTheConfiguredAndTheRepositoryPatterns(t *testing.T) {
	ignored := nul("node_modules/a/index.js", "node_modules/b/index.js", "packages/a/cache/data", "unignored/cache/data", "tracked-cache/ignored", "partial-cache/drop/data")
	matched := nul("node_modules/a/index.js", "node_modules/b/index.js", "packages/a/cache/data", "unignored/cache/data", "tracked-cache/ignored",
		"partial-cache/drop/data", "partial-cache/keep/data", "other/file")
	candidates := WorktreeSymlinkFolderCandidates(ignored, matched)
	wantCandidates := []string{"node_modules/a", "node_modules", "node_modules/b", "packages/a/cache", "packages/a", "packages", "unignored/cache", "unignored",
		"tracked-cache", "partial-cache/drop", "partial-cache"}
	if !reflect.DeepEqual(candidates, wantCandidates) {
		t.Errorf("candidates = %q\nwant %q", candidates, wantCandidates)
	}
	got := FilterWorktreeSymlinkFolders(candidates,
		nul("node_modules/", "node_modules/a/", "node_modules/b/", "packages/a/cache/", "unignored/cache/", "tracked-cache/", "partial-cache/", "partial-cache/drop/"),
		nul("node_modules/", "node_modules/a/", "node_modules/b/", "packages/", "packages/a/", "packages/a/cache/", "unignored/", "tracked-cache/", "partial-cache/", "partial-cache/drop/"),
		nul("node_modules/", "packages/", "unignored/", "partial-cache/drop/"))
	if want := []string{"node_modules", "packages/a/cache", "partial-cache/drop"}; !reflect.DeepEqual(got, want) {
		t.Errorf("folders = %q, want %q", got, want)
	}
}

// symlinkWorld makes a repository folder and a worktree folder side by side.
func symlinkWorld(t *testing.T) (repo, wt string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo, wt = filepath.Join(root, "repository"), filepath.Join(root, "worktree")
	for _, d := range []string{repo, wt} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return repo, wt
}

func TestCreateWorktreeSymlinkMakesADirectoryLinkToTheRepository(t *testing.T) {
	repo, wt := symlinkWorld(t)
	writeTestFile(t, filepath.Join(repo, "node_modules", "package.txt"), "contents")
	status, err := CreateWorktreeSymlink(repo, wt, "node_modules")
	if err != nil || status != SymlinkCreated {
		t.Fatalf("status = %q, err = %v", status, err)
	}
	target := filepath.Join(wt, "node_modules")
	if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("not a link: %v, %v", info, err)
	}
	if real, _ := filepath.EvalSymlinks(target); real != filepath.Join(repo, "node_modules") {
		t.Errorf("target = %s", real)
	}
	if data, err := os.ReadFile(filepath.Join(target, "package.txt")); err != nil || string(data) != "contents" {
		t.Errorf("contents = %q, %v", data, err)
	}
}

func TestCreateWorktreeSymlinkMakesTheFoldersAboveTheLink(t *testing.T) {
	repo, wt := symlinkWorld(t)
	writeTestFile(t, filepath.Join(repo, "packages", "a", "cache", "data"), "x")
	status, err := CreateWorktreeSymlink(repo, wt, "packages/a/cache")
	if err != nil || status != SymlinkCreated {
		t.Fatalf("status = %q, err = %v", status, err)
	}
	if _, err := os.Stat(filepath.Join(wt, "packages", "a", "cache", "data")); err != nil {
		t.Error(err)
	}
}

func TestCreateWorktreeSymlinkSkipsExistingTargets(t *testing.T) {
	repo, wt := symlinkWorld(t)
	for _, d := range []string{repo, wt} {
		if err := os.Mkdir(filepath.Join(d, "existing"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if status, err := CreateWorktreeSymlink(repo, wt, "existing"); err != nil || status != SymlinkTargetExists {
		t.Errorf("status = %q, err = %v", status, err)
	}
}

func TestCreateWorktreeSymlinkSkipsFoldersContainingTheWorktree(t *testing.T) {
	repo, _ := symlinkWorld(t)
	nested := filepath.Join(repo, ".worktrees", "feature")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if status, err := CreateWorktreeSymlink(repo, nested, ".worktrees"); err != nil || status != SymlinkSourceContainsWorktree {
		t.Errorf("status = %q, err = %v", status, err)
	}
}

func TestCreateWorktreeSymlinkFailures(t *testing.T) {
	t.Run("a folder above the link in the worktree is a link", func(t *testing.T) {
		repo, wt := symlinkWorld(t)
		external := filepath.Join(filepath.Dir(repo), "external")
		for _, d := range []string{filepath.Join(repo, "parent", "child"), external} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(external, filepath.Join(wt, "parent")); err != nil {
			t.Fatal(err)
		}
		_, err := CreateWorktreeSymlink(repo, wt, "parent/child")
		wantRoute(t, err, "CreateWorktreeSymlink.GitWorktreeSymlinkParentLink")
	})
	t.Run("a folder above the link in the worktree is a file", func(t *testing.T) {
		repo, wt := symlinkWorld(t)
		if err := os.MkdirAll(filepath.Join(repo, "parent", "child"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(wt, "parent"), "a file")
		_, err := CreateWorktreeSymlink(repo, wt, "parent/child")
		wantRoute(t, err, "CreateWorktreeSymlink.GitWorktreeSymlinkParentNotDir")
	})
	t.Run("the folder to link is not there", func(t *testing.T) {
		repo, wt := symlinkWorld(t)
		_, err := CreateWorktreeSymlink(repo, wt, "missing")
		appErr := routeOf(t, err, "CreateWorktreeSymlink.GitWorktreeSymlinkFailed")
		if appErr != nil && appErr.Metadata["step"] != "realpath-source" {
			t.Errorf("step = %v", appErr.Metadata["step"])
		}
	})
	t.Run("the worktree is not there", func(t *testing.T) {
		repo, wt := symlinkWorld(t)
		if err := os.Mkdir(filepath.Join(repo, "node_modules"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(wt); err != nil {
			t.Fatal(err)
		}
		_, err := CreateWorktreeSymlink(repo, wt, "node_modules")
		appErr := routeOf(t, err, "CreateWorktreeSymlink.GitWorktreeSymlinkFailed")
		if appErr != nil && appErr.Metadata["step"] != "realpath-worktree" {
			t.Errorf("step = %v", appErr.Metadata["step"])
		}
	})
	t.Run("the link cannot be made", func(t *testing.T) {
		repo, wt := symlinkWorld(t)
		if err := os.Mkdir(filepath.Join(repo, "node_modules"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(wt, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(wt, 0o755) })
		if os.Geteuid() == 0 {
			t.Skip("root can write anywhere")
		}
		_, err := CreateWorktreeSymlink(repo, wt, "node_modules")
		appErr := routeOf(t, err, "CreateWorktreeSymlink.GitWorktreeSymlinkFailed")
		if appErr != nil && appErr.Metadata["step"] != "symlink" {
			t.Errorf("step = %v", appErr.Metadata["step"])
		}
	})
}
