package gitscm

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// TestTheIgnoreMatcherAgreesWithGit asks git (check-ignore) about every path and the matcher the same.
func TestTheIgnoreMatcherAgreesWithGit(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write(".gitignore", strings.Join([]string{
		"# a comment", "", "*.log", "!keep.log", "build/", "/rooted.txt", "doc/*.tmp", "**/cache", "a/**/z.o", "\\#hash", "trailing\\ ", "[ab]x.c", "?.q", "",
	}, "\n"))
	repo.Write("sub/.gitignore", "*.out\n!/important.out\nnested/\n")
	repo.Write(".git/info/exclude", "excluded-by-info\n")
	repo.Write("README.md", "r\n")
	repo.Commit("start")
	paths := []struct {
		rel   string
		isDir bool
	}{
		{"a.log", false}, {"keep.log", false}, {"x/keep.log", false}, {"x/y/z.log", false}, {"build", true}, {"build/out.o", false},
		{"src/build", true}, {"src/build/out.o", false}, {"rooted.txt", false}, {"x/rooted.txt", false},
		{"doc/a.tmp", false}, {"doc/x/a.tmp", false}, {"cache", true}, {"p/q/cache", true}, {"p/q/cache/f", false},
		{"a/z.o", false}, {"a/b/c/z.o", false}, {"b/z.o", false}, {"#hash", false}, {"ax.c", false}, {"cx.c", false}, {"1.q", false}, {"12.q", false},
		{"sub/a.out", false}, {"sub/important.out", false}, {"sub/d/important.out", false}, {"sub/nested", true}, {"sub/nested/f", false},
		{"nested/f", false}, {"excluded-by-info", false}, {"d/excluded-by-info", false}, {"README.md", false}, {"src/main.go", false},
	}
	m := newIgnoreMatcher(repo.Dir, repo.Path(".git"))
	for _, p := range paths {
		t.Run(p.rel, func(t *testing.T) {
			// git needs the file to exist to know whether it is a folder
			if p.isDir {
				repo.Write(p.rel+"/.keep", "")
			} else {
				repo.Write(p.rel, "")
			}
			out, _, err := repo.GitErr("check-ignore", "-q", "--no-index", p.rel+map[bool]string{true: "/", false: ""}[p.isDir])
			want := err == nil
			_ = out
			if got := m.Ignored(repo.Path(p.rel), p.isDir); got != want {
				t.Errorf("Ignored(%q, dir=%v) = %v, git says %v", p.rel, p.isDir, got, want)
			}
		})
	}
}

func TestTheIgnoreMatcherReadsAChangedFileAgain(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	m := newIgnoreMatcher(repo.Dir, repo.Path(".git"))
	if m.Ignored(repo.Path("a.tmp"), false) {
		t.Fatal("ignored before there is a rule")
	}
	repo.Write(".gitignore", "*.tmp\n")
	if !m.Ignored(repo.Path("a.tmp"), false) {
		t.Error("the new rule is not read")
	}
	repo.Write(".gitignore", "*.tmp\n*.bak\n")
	if !m.Ignored(repo.Path("a.bak"), false) {
		t.Error("the changed file is not read again")
	}
	repo.Remove(".gitignore")
	if m.Ignored(repo.Path("a.tmp"), false) {
		t.Error("a deleted file still ignores")
	}
	// paths that are not below the root, and the root, are never ignored
	repo.Write(".gitignore", "*\n")
	if m.Ignored(repo.Dir, true) || m.Ignored("/elsewhere/x", false) || m.Ignored(repo.Path("../x"), false) {
		t.Error("a path that is not below the root was ignored")
	}
}

func TestAnIgnoreFileBeyondTheSizeLimitIsNotRead(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	m := newIgnoreMatcher(repo.Dir, repo.Path(".git"))
	m.maxBytes = 10
	repo.Write(".gitignore", "*.tmp\n# "+strings.Repeat("x", 50)+"\n")
	if m.Ignored(repo.Path("a.tmp"), false) {
		t.Error("a file over the limit was read (a path it does not know about triggers a status: the safe side)")
	}
}
