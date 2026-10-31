package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// writeTree writes files (relative path to content) below dir.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindRepositoryWithoutRunningGit(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitDir := map[string]string{"HEAD": "ref: refs/heads/main\n", "objects/.keep": "", "refs/.keep": ""}
	with := func(prefix string, files map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range files {
			out[prefix+k] = v
		}
		return out
	}
	layout := map[string]string{}
	for k, v := range with("plain/.git/", gitDir) {
		layout[k] = v
	}
	layout["plain/src/a.go"] = "package a\n"
	for k, v := range with("bare.git/", gitDir) {
		layout[k] = v
	}
	for k, v := range with("super/.git/", gitDir) {
		layout[k] = v
	}
	for k, v := range with("super/.git/modules/sub/", gitDir) {
		layout[k] = v
	}
	layout["super/sub/.git"] = "gitdir: ../.git/modules/sub\n"
	for k, v := range with("main/.git/", gitDir) {
		layout[k] = v
	}
	for k, v := range with("main/.git/worktrees/feature/", gitDir) {
		layout[k] = v
	}
	layout["wt/feature/.git"] = "gitdir: " + filepath.Join(root, "main/.git/worktrees/feature") + "\n"
	layout["broken/.git"] = "not a git file\n"
	layout["emptygit/.git/readme"] = "a .git folder that is no git directory\n"
	layout["none/a.txt"] = "x"
	writeTree(t, root, layout)

	tests := []struct {
		name       string
		dir        string
		ok         bool
		root, gdir string
		bare       bool
		kind       Kind
		parent     string
	}{
		{"the work tree", "plain", true, "plain", "plain/.git", false, KindPlain, ""},
		{"a folder inside it", "plain/src", true, "plain", "plain/.git", false, KindPlain, ""},
		{"inside the git directory", "plain/.git", true, "plain", "plain/.git", false, KindPlain, ""},
		{"a bare repository", "bare.git", true, "bare.git", "bare.git", true, KindPlain, ""},
		{"a submodule", "super/sub", true, "super/sub", "super/.git/modules/sub", false, KindSubmodule, "super"},
		{"a linked worktree", "wt/feature", true, "wt/feature", "main/.git/worktrees/feature", false, KindWorktree, "main"},
		{"a .git file that says nothing", "broken", false, "", "", false, KindPlain, ""},
		{"a .git folder that is no git directory", "emptygit", false, "", "", false, KindPlain, ""},
		{"no repository", "none", false, "", "", false, KindPlain, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loc, ok := FindRepository(filepath.Join(root, tc.dir))
			// a repository above the temporary folder would be found for the folders that have none: the tests are in /tmp or /var/tmp
			if ok != tc.ok {
				t.Fatalf("FindRepository(%s) = %+v, %v", tc.dir, loc, ok)
			}
			if !ok {
				return
			}
			abs := func(rel string) string {
				if rel == "" {
					return ""
				}
				return filepath.Join(root, rel)
			}
			if tc.name == "inside the git directory" {
				// a folder named .git inside the work tree: the git directory is a repository of its own to FindRepository (it
				// has HEAD and objects) unless the work tree above is found first; the work tree is what the Runner's check
				// maps it to (ownerOf)
				loc, ok = ownerOf(filepath.Join(root, tc.dir))
				if !ok {
					t.Fatal("ownerOf found nothing")
				}
			}
			if loc.Root != abs(tc.root) || loc.GitDir != abs(tc.gdir) || loc.Bare != tc.bare || loc.Kind != tc.kind || loc.Parent != abs(tc.parent) {
				t.Errorf("got %+v", loc)
			}
		})
	}
}

func TestFindRepositoryResolvesSymbolicLinks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"real/.git/HEAD": "ref: refs/heads/main\n", "real/.git/objects/.keep": ""})
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	loc, ok := FindRepository(filepath.Join(root, "link"))
	if !ok || loc.Root != filepath.Join(root, "real") || loc.GitDir != filepath.Join(root, "real", ".git") {
		t.Errorf("got %+v, %v", loc, ok)
	}
}

func TestFindRepositoryAgreesWithGit(t *testing.T) {
	e := gittest.NewEnv(t)
	main := e.Init("main")
	main.Write("a.txt", "a\n")
	main.Commit("Add a")
	sub := e.Init("sub")
	sub.Write("s.txt", "s\n")
	sub.Commit("Add s")
	main.Submodule("vendor/sub", sub)
	main.Git("worktree", "add", "-q", filepath.Join(e.Work, "feature"), "-b", "feature")
	runner := NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: e.Vars()})
	for _, dir := range []string{main.Dir, filepath.Join(main.Dir, "vendor", "sub"), filepath.Join(e.Work, "feature")} {
		loc, ok := FindRepository(dir)
		if !ok {
			t.Fatalf("no repository found at %s", dir)
		}
		repo, err := Open(context.Background(), runner, nil, loc.Root)
		if err != nil {
			t.Fatalf("git does not open %s: %v", loc.Root, err)
		}
		if repo.Info.GitDir != loc.GitDir {
			t.Errorf("%s: git says the git directory is %s, the file system says %s", dir, repo.Info.GitDir, loc.GitDir)
		}
	}
	if loc, _ := FindRepository(filepath.Join(main.Dir, "vendor", "sub")); loc.Kind != KindSubmodule || loc.Parent != main.Dir {
		t.Errorf("the submodule: %+v", loc)
	}
	if loc, _ := FindRepository(filepath.Join(e.Work, "feature")); loc.Kind != KindWorktree || loc.Parent != main.Dir {
		t.Errorf("the worktree: %+v", loc)
	}
}

func TestWithin(t *testing.T) {
	tests := []struct {
		path, dir string
		want      bool
	}{
		{"/a/b", "/a/b", true},
		{"/a/b/c", "/a/b", true},
		{"/a/bc", "/a/b", false},
		{"/a", "/a/b", false},
		{"/a/b/", "/a/b", true},
		{"/x", "/", true},
	}
	for _, tc := range tests {
		if got := Within(tc.path, tc.dir); got != tc.want {
			t.Errorf("Within(%q, %q) = %v", tc.path, tc.dir, got)
		}
	}
}
