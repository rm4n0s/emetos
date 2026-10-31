package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// The commands that change the index and the work tree, against real repositories (step 24.8).

// porcelain is "git status --porcelain -z" with the untracked files listed one by one, as lines "XY path" (the old path of a rename is
// left out).
func porcelain(r *gittest.Repo) []string {
	out := r.Git("status", "--porcelain", "-z", "-uall")
	var lines []string
	toks := strings.Split(out, "\x00")
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		if len(tok) < 4 {
			continue
		}
		lines = append(lines, tok)
		if tok[0] == 'R' || tok[0] == 'C' || tok[1] == 'R' || tok[1] == 'C' {
			i++ // the old path
		}
	}
	sort.Strings(lines)
	return lines
}

// statesBase makes the repository most of the tests start from: committed files of every kind of path.
func statesBase(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	r := e.Init("states")
	for _, f := range []string{"a.txt", "b.txt", "dir/c.txt", "dir with space/ünï cödé.txt", "a[1].txt", "star.txt", "old.txt"} {
		r.Write(f, f+"\none\ntwo\n")
	}
	r.Commit("Add the files")
	return r
}

func TestAdd(t *testing.T) {
	tests := []struct {
		name  string
		setup func(r *gittest.Repo)
		paths []string
		opts  AddOptions
		want  []string
	}{
		{"a modified file", func(r *gittest.Repo) { r.Write("a.txt", "changed\n") }, []string{"a.txt"}, AddOptions{}, []string{"M  a.txt"}},
		{"a deleted file", func(r *gittest.Repo) { r.Remove("a.txt") }, []string{"a.txt"}, AddOptions{}, []string{"D  a.txt"}},
		{"an untracked file", func(r *gittest.Repo) { r.Write("new.txt", "n\n") }, []string{"new.txt"}, AddOptions{}, []string{"A  new.txt"}},
		{"a folder with a new file and a modified one", func(r *gittest.Repo) {
			r.Write("dir/c.txt", "changed\n")
			r.Write("dir/new.txt", "n\n")
		}, []string{"dir"}, AddOptions{}, []string{"M  dir/c.txt", "A  dir/new.txt"}},
		{"update stages the tracked files and leaves the untracked ones", func(r *gittest.Repo) {
			r.Write("a.txt", "changed\n")
			r.Write("new.txt", "n\n")
		}, nil, AddOptions{Update: true}, []string{"M  a.txt", "?? new.txt"}},
		{"an intent to add file", func(r *gittest.Repo) {
			r.Write("new.txt", "n\n")
			r.Git("add", "-N", "new.txt")
		}, []string{"new.txt"}, AddOptions{}, []string{"A  new.txt"}},
		{"a path with spaces and non-ASCII characters", func(r *gittest.Repo) { r.Write("dir with space/ünï cödé.txt", "changed\n") },
			[]string{"dir with space/ünï cödé.txt"}, AddOptions{}, []string{"M  dir with space/ünï cödé.txt"}},
		{"a path with glob characters is that file only", func(r *gittest.Repo) {
			r.Write("a[1].txt", "changed\n")
			r.Write("a.txt", "changed\n")
			r.Write("1.txt", "x\n")
		}, []string{"a[1].txt"}, AddOptions{}, []string{" M a.txt", "M  a[1].txt", "?? 1.txt"}},
		{"an untracked symbolic link", func(r *gittest.Repo) {
			if err := os.Symlink("a.txt", r.Path("link")); err != nil {
				t.Fatal(err)
			}
		}, []string{"link"}, AddOptions{}, []string{"A  link"}},
		{"an absolute path", func(r *gittest.Repo) { r.Write("b.txt", "changed\n") }, []string{"@abs:b.txt"}, AddOptions{}, []string{"M  b.txt"}},
		{"no path is the whole tree", func(r *gittest.Repo) {
			r.Write("b.txt", "changed\n")
			r.Write("new.txt", "n\n")
			r.Remove("a.txt")
		}, nil, AddOptions{}, []string{"D  a.txt", "M  b.txt", "A  new.txt"}},
		{"a conflict that was resolved", func(r *gittest.Repo) {
			r.Branch("other", "HEAD")
			r.Write("b.txt", "main\n")
			r.Commit("main")
			r.Checkout("other")
			r.Write("b.txt", "other\n")
			r.Commit("other")
			r.Checkout("main")
			if err := r.Merge("other"); err == nil {
				t.Fatal("the merge did not conflict")
			}
			r.Write("b.txt", "resolved\n")
		}, []string{"b.txt"}, AddOptions{}, []string{"M  b.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			r := statesBase(t, e)
			tc.setup(r)
			repo := open(t, e, r.Dir)
			paths := absolutize(r, tc.paths)
			if err := repo.Add(context.Background(), paths, tc.opts); err != nil {
				t.Fatal(err)
			}
			got := porcelain(r)
			want := tc.want
			sort.Strings(got)
			sort.Strings(want)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("status:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

// absolutize turns the paths marked "@abs:" into absolute ones.
func absolutize(r *gittest.Repo, paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		if rest, ok := strings.CutPrefix(p, "@abs:"); ok {
			p = r.Path(rest)
		}
		out[i] = p
	}
	return out
}

func TestAddInAFreshRepositoryAndWithASubmodule(t *testing.T) {
	t.Run("before the first commit", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := e.Init("fresh")
		r.Write("a.txt", "a\n")
		repo := open(t, e, r.Dir)
		if err := repo.Add(context.Background(), []string{"a.txt"}, AddOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := porcelain(r); strings.Join(got, "|") != "A  a.txt" {
			t.Errorf("%v", got)
		}
	})
	t.Run("a submodule with a new commit", func(t *testing.T) {
		e := gittest.NewEnv(t)
		sub := e.Init("lib")
		sub.Write("lib.go", "package lib\n")
		sub.Commit("lib")
		r := e.Init("main")
		r.Write("a.txt", "a\n")
		r.Commit("a")
		r.Submodule("vendor/lib", sub)
		inner := &gittest.Repo{Env: e, Dir: r.Path("vendor/lib")}
		inner.Write("more.go", "package lib\n")
		inner.Commit("more")
		repo := open(t, e, r.Dir)
		if got := porcelain(r); strings.Join(got, "|") != " M vendor/lib" {
			t.Fatalf("before: %v", got)
		}
		if err := repo.Add(context.Background(), []string{"vendor/lib"}, AddOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := porcelain(r); strings.Join(got, "|") != "M  vendor/lib" {
			t.Errorf("after: %v", got)
		}
	})
}

func TestAStarIsAFileNameNotAPattern(t *testing.T) {
	e := gittest.NewEnv(t)
	r := statesBase(t, e)
	r.Write("star.txt", "changed\n")
	r.Write("a.txt", "changed\n")
	err := open(t, e, r.Dir).Add(context.Background(), []string{"*.txt"}, AddOptions{})
	wantRoute(t, err, "Repo.Add->Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->classify.GitCommandFailed")
	if got := porcelain(r); strings.Join(got, "|") != " M a.txt| M star.txt" {
		t.Errorf("%v", got)
	}
}

func TestAddIntentToAdd(t *testing.T) {
	e := gittest.NewEnv(t)
	r := statesBase(t, e)
	r.Write("new.txt", "n\n")
	r.Write("dir/other.txt", "o\n")
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	if err := repo.AddIntentToAdd(ctx, []string{"new.txt", "dir/other.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := porcelain(r); strings.Join(got, "|") != " A dir/other.txt| A new.txt" {
		t.Errorf("%v", got)
	}
	if err := repo.AddIntentToAdd(ctx, nil); err != nil {
		t.Errorf("no path: %v", err)
	}
}

func TestReset(t *testing.T) {
	tests := []struct {
		name  string
		setup func(r *gittest.Repo)
		paths []string
		want  []string
	}{
		{"a staged change", func(r *gittest.Repo) { r.Write("a.txt", "c\n"); r.Git("add", "a.txt") }, []string{"a.txt"}, []string{" M a.txt"}},
		{"a staged new file becomes untracked", func(r *gittest.Repo) { r.Write("new.txt", "n\n"); r.Git("add", "new.txt") }, []string{"new.txt"}, []string{"?? new.txt"}},
		{"a staged deletion", func(r *gittest.Repo) { r.Git("rm", "-q", "a.txt") }, []string{"a.txt"}, []string{" D a.txt"}},
		{"a staged rename, both paths", func(r *gittest.Repo) { r.Git("mv", "old.txt", "new.txt") }, []string{"old.txt", "new.txt"}, []string{" D old.txt", "?? new.txt"}},
		{"only the path asked for", func(r *gittest.Repo) {
			r.Write("a.txt", "c\n")
			r.Write("b.txt", "c\n")
			r.Git("add", "-A")
		}, []string{"a.txt"}, []string{" M a.txt", "M  b.txt"}},
		{"everything when there is no path", func(r *gittest.Repo) {
			r.Write("a.txt", "c\n")
			r.Write("b.txt", "c\n")
			r.Git("add", "-A")
		}, nil, []string{" M a.txt", " M b.txt"}},
		{"a path with glob characters is that file only", func(r *gittest.Repo) {
			r.Write("a[1].txt", "c\n")
			r.Write("a.txt", "c\n")
			r.Git("add", "-A")
		}, []string{"a[1].txt"}, []string{"M  a.txt", " M a[1].txt"}},
		{"an intent to add file", func(r *gittest.Repo) { r.Write("new.txt", "n\n"); r.Git("add", "-N", "new.txt") }, []string{"new.txt"}, []string{"?? new.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			r := statesBase(t, e)
			tc.setup(r)
			repo := open(t, e, r.Dir)
			if err := repo.Reset(context.Background(), tc.paths); err != nil {
				t.Fatal(err)
			}
			got, want := porcelain(r), tc.want
			sort.Strings(got)
			sort.Strings(want)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("status:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
	t.Run("before the first commit the entries leave the index", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := e.Init("fresh")
		r.Write("a.txt", "a\n")
		r.Write("dir/b.txt", "b\n")
		r.Git("add", "-A")
		repo := open(t, e, r.Dir)
		ctx := context.Background()
		if has, err := repo.HasCommit(ctx); err != nil || has {
			t.Fatalf("HasCommit = %v, %v", has, err)
		}
		if err := repo.Reset(ctx, []string{"a.txt"}); err != nil {
			t.Fatal(err)
		}
		if got := porcelain(r); strings.Join(got, "|") != "?? a.txt|A  dir/b.txt" {
			t.Errorf("one file: %v", got)
		}
		if err := repo.Reset(ctx, nil); err != nil {
			t.Fatal(err)
		}
		if got := porcelain(r); strings.Join(got, "|") != "?? a.txt|?? dir/b.txt" {
			t.Errorf("all: %v", got)
		}
		if _, err := os.Stat(r.Path("a.txt")); err != nil {
			t.Errorf("the file was touched: %v", err)
		}
	})
}

func TestHasCommit(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	repo := open(t, e, r.Dir)
	if has, err := repo.HasCommit(context.Background()); err != nil || has {
		t.Errorf("a fresh repository: %v, %v", has, err)
	}
	r.Write("a", "a")
	r.Commit("a")
	if has, err := repo.HasCommit(context.Background()); err != nil || !has {
		t.Errorf("a repository with a commit: %v, %v", has, err)
	}
}

func TestCheckout(t *testing.T) {
	tests := []struct {
		name  string
		setup func(r *gittest.Repo)
		paths []string
		want  []string
		check func(t *testing.T, r *gittest.Repo)
	}{
		{"a modified file", func(r *gittest.Repo) { r.Write("a.txt", "c\n") }, []string{"a.txt"}, nil, func(t *testing.T, r *gittest.Repo) {
			if got := r.Read("a.txt"); got != "a.txt\none\ntwo\n" {
				t.Errorf("a.txt = %q", got)
			}
		}},
		{"a deleted file comes back", func(r *gittest.Repo) { r.Remove("a.txt") }, []string{"a.txt"}, nil, nil},
		{"the staged version is kept", func(r *gittest.Repo) {
			r.Write("a.txt", "staged\n")
			r.Git("add", "a.txt")
			r.Write("a.txt", "worktree\n")
		}, []string{"a.txt"}, []string{"M  a.txt"}, func(t *testing.T, r *gittest.Repo) {
			if got := r.Read("a.txt"); got != "staged\n" {
				t.Errorf("a.txt = %q", got)
			}
		}},
		{"a path with spaces and non-ASCII characters", func(r *gittest.Repo) { r.Write("dir with space/ünï cödé.txt", "c\n") },
			[]string{"dir with space/ünï cödé.txt"}, nil, nil},
		{"a path with glob characters is that file only", func(r *gittest.Repo) {
			r.Write("a[1].txt", "c\n")
			r.Write("a.txt", "c\n")
		}, []string{"a[1].txt"}, []string{" M a.txt"}, nil},
		{"a folder", func(r *gittest.Repo) { r.Write("dir/c.txt", "c\n") }, []string{"dir"}, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			r := statesBase(t, e)
			tc.setup(r)
			repo := open(t, e, r.Dir)
			if err := repo.Checkout(context.Background(), tc.paths); err != nil {
				t.Fatal(err)
			}
			if got := porcelain(r); strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("status = %v, want %v", got, tc.want)
			}
			if tc.check != nil {
				tc.check(t, r)
			}
		})
	}
	t.Run("no path does nothing", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := statesBase(t, e)
		r.Write("a.txt", "c\n")
		if err := open(t, e, r.Dir).Checkout(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		if got := porcelain(r); strings.Join(got, "|") != " M a.txt" {
			t.Errorf("%v", got)
		}
	})
	t.Run("git refuses a path it does not track before the first commit", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := e.Init("fresh")
		r.Write("a.txt", "a\n")
		err := open(t, e, r.Dir).Checkout(context.Background(), []string{"a.txt"})
		if err == nil {
			t.Fatal("git checkout of an untracked file did not fail")
		}
		wantRoute(t, err, "Repo.Checkout->Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->classify.GitCommandFailed")
	})
}

func TestClean(t *testing.T) {
	e := gittest.NewEnv(t)
	r := statesBase(t, e)
	r.Write(".gitignore", "ignored.txt\n")
	r.Commit("ignore")
	r.Write("new.txt", "n\n")
	r.Write("ignored.txt", "i\n")
	r.Write("untracked dir/ünï.txt", "u\n")
	r.Write("[x].txt", "x\n")
	r.Write("x.txt", "x\n")
	r.Write("a.txt", "modified\n")
	if err := os.Symlink("a.txt", r.Path("link")); err != nil {
		t.Fatal(err)
	}
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	if err := repo.Clean(ctx, []string{"new.txt", "untracked dir/ünï.txt", "[x].txt", "a.txt", "link"}); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"new.txt", "untracked dir/ünï.txt", "[x].txt", "link"} {
		if _, err := os.Stat(r.Path(gone)); !os.IsNotExist(err) {
			t.Errorf("%s is still there (%v)", gone, err)
		}
	}
	for _, kept := range []string{"a.txt", "x.txt", "ignored.txt"} {
		if _, err := os.Stat(r.Path(kept)); err != nil {
			t.Errorf("%s was removed", kept)
		}
	}
	if got := r.Read("a.txt"); got != "modified\n" {
		t.Errorf("a tracked file was touched: %q", got)
	}
	if err := repo.Clean(ctx, nil); err != nil {
		t.Errorf("no path: %v", err)
	}
}

func TestCleanSplitsLongListsOfPaths(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("many")
	r.Write("keep.txt", "k\n")
	r.Commit("keep")
	var paths []string
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("untracked-directory-with-a-long-name/file-with-a-long-name-%04d.txt", i)
		r.Write(name, "u\n")
		paths = append(paths, name)
	}
	if n := len(strings.Join(paths, " ")); n <= MaxCommandLineLength {
		t.Fatalf("the paths are %d characters, not past the limit", n)
	}
	if err := open(t, e, r.Dir).Clean(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	if got := porcelain(r); len(got) != 0 {
		t.Errorf("%d files are left: %v", len(got), got[:min(3, len(got))])
	}
}

func TestRm(t *testing.T) {
	ctx := context.Background()
	t.Run("a committed file", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := statesBase(t, e)
		if err := open(t, e, r.Dir).Rm(ctx, []string{"a.txt", "dir with space/ünï cödé.txt"}, RmOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(r.Path("a.txt")); !os.IsNotExist(err) {
			t.Errorf("a.txt is there: %v", err)
		}
		got := porcelain(r)
		if len(got) != 2 || got[0] != "D  a.txt" {
			t.Errorf("%v", got)
		}
	})
	t.Run("cached keeps the file", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := statesBase(t, e)
		if err := open(t, e, r.Dir).Rm(ctx, []string{"a.txt"}, RmOptions{Cached: true}); err != nil {
			t.Fatal(err)
		}
		if got := porcelain(r); strings.Join(got, "|") != "?? a.txt|D  a.txt" {
			t.Errorf("%v", got)
		}
	})
	t.Run("a folder needs the recursive option", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := statesBase(t, e)
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Rm(ctx, []string{"dir"}, RmOptions{}), "Repo.Rm->Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->classify.GitCommandFailed")
		if err := repo.Rm(ctx, []string{"dir"}, RmOptions{Recursive: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(r.Path("dir")); !os.IsNotExist(err) {
			t.Errorf("dir is there: %v", err)
		}
	})
	t.Run("a file with changes is refused", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := statesBase(t, e)
		r.Write("a.txt", "changed\n")
		err := open(t, e, r.Dir).Rm(ctx, []string{"a.txt"}, RmOptions{})
		appErr := routeOf(t, err, "Repo.Rm->Repo.write")
		if appErr != nil && !strings.Contains(fmt.Sprint(appErr.Metadata["stderr"]), "local modifications") {
			t.Errorf("stderr = %v", appErr.Metadata["stderr"])
		}
		if got := r.Read("a.txt"); got != "changed\n" {
			t.Errorf("the file was touched: %q", got)
		}
	})
	t.Run("force removes a file with changes, and an intent to add file", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := statesBase(t, e)
		r.Write("a.txt", "changed\n")
		r.Write("new.txt", "n\n")
		r.Git("add", "-N", "new.txt")
		repo := open(t, e, r.Dir)
		if err := repo.Rm(ctx, []string{"a.txt", "new.txt"}, RmOptions{Force: true}); err != nil {
			t.Fatal(err)
		}
		if got := porcelain(r); strings.Join(got, "|") != "D  a.txt" {
			t.Errorf("%v", got)
		}
		if exists := func(rel string) bool { _, err := os.Stat(r.Path(rel)); return err == nil }; exists("a.txt") || exists("new.txt") {
			t.Error("the files are still there")
		}
	})
	t.Run("cached forgets an intent to add file whose file is gone", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := statesBase(t, e)
		r.Write("new.txt", "n\n")
		r.Git("add", "-N", "new.txt")
		r.Remove("new.txt")
		if err := open(t, e, r.Dir).Rm(ctx, []string{"new.txt"}, RmOptions{Cached: true, Force: true}); err != nil {
			t.Fatal(err)
		}
		if got := porcelain(r); len(got) != 0 {
			t.Errorf("%v", got)
		}
	})
	t.Run("no path does nothing", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := statesBase(t, e)
		if err := open(t, e, r.Dir).Rm(ctx, nil, RmOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := porcelain(r); len(got) != 0 {
			t.Errorf("%v", got)
		}
	})
}

func TestMv(t *testing.T) {
	e := gittest.NewEnv(t)
	r := statesBase(t, e)
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	if err := repo.Mv(ctx, "dir with space/ünï cödé.txt", "dir/moved file.txt"); err != nil {
		t.Fatal(err)
	}
	got := porcelain(r)
	if len(got) != 1 || !strings.HasPrefix(got[0], "R  ") {
		t.Errorf("%v", got)
	}
	if _, err := os.Stat(r.Path("dir/moved file.txt")); err != nil {
		t.Errorf("the file did not move: %v", err)
	}
	wantRoute(t, repo.Mv(ctx, "no-such.txt", "x.txt"), "Repo.Mv->Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->classify.GitCommandFailed")
	wantRoute(t, repo.Mv(ctx, "a.txt", filepath.Join("..", "outside.txt")), "Repo.Mv->Repo.relPaths.GitPathOutsideRepository")
}

func TestUpdateSubmodules(t *testing.T) {
	e := gittest.NewEnv(t)
	sub := e.Init("lib")
	sub.Write("lib.go", "package lib\n")
	sub.Commit("lib")
	r := e.Init("main")
	r.Write("a.txt", "a\n")
	r.Commit("a")
	r.Submodule("vendor/lib", sub)
	inner := &gittest.Repo{Env: e, Dir: r.Path("vendor/lib")}
	committed := inner.Head()
	inner.Write("more.go", "package lib\n")
	inner.Commit("more")
	repo := open(t, e, r.Dir)
	if got := porcelain(r); strings.Join(got, "|") != " M vendor/lib" {
		t.Fatalf("before: %v", got)
	}
	if err := repo.UpdateSubmodules(context.Background(), []string{"vendor/lib"}); err != nil {
		t.Fatal(err)
	}
	if got := porcelain(r); len(got) != 0 {
		t.Errorf("after: %v", got)
	}
	if got := inner.Head(); got != committed {
		t.Errorf("the submodule is at %s, want %s", got, committed)
	}
}

func TestFiveThousandPathsAreStagedUnstagedAndDiscardedInOneCommandEach(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("big")
	const n = 5000
	var paths []string
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("d%02d/file-%04d.txt", i%50, i)
		r.Write(name, "one\n")
		paths = append(paths, name)
	}
	r.Commit("many")
	for _, p := range paths {
		r.Write(p, "two\n")
	}
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	count := func(prefix string) int {
		c := 0
		for _, l := range porcelain(r) {
			if strings.HasPrefix(l, prefix) {
				c++
			}
		}
		return c
	}
	if err := repo.Add(ctx, paths, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := count("M  "); got != n {
		t.Fatalf("staged %d files, want %d", got, n)
	}
	if err := repo.Reset(ctx, paths); err != nil {
		t.Fatal(err)
	}
	if got := count(" M "); got != n {
		t.Fatalf("unstaged %d files, want %d", got, n)
	}
	if err := repo.Checkout(ctx, paths); err != nil {
		t.Fatal(err)
	}
	if got := porcelain(r); len(got) != 0 {
		t.Errorf("%d files still changed", len(got))
	}
}

func TestThePathsOfTheWritesAreChecked(t *testing.T) {
	e := gittest.NewEnv(t)
	r := statesBase(t, e)
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	outside := filepath.Join(filepath.Dir(r.Dir), "other", "x.txt")
	tests := []struct {
		name  string
		err   error
		route string
	}{
		{"a relative path that leaves the repository", repo.Add(ctx, []string{"../x"}, AddOptions{}), "Repo.Add->Repo.relPaths.GitPathOutsideRepository"},
		{"an absolute path outside", repo.Reset(ctx, []string{outside}), "Repo.Reset->Repo.relPaths.GitPathOutsideRepository"},
		{"an empty path", repo.Checkout(ctx, []string{""}), "Repo.Checkout->Repo.relPaths.GitPathEmpty"},
		{"clean", repo.Clean(ctx, []string{"a/../../x"}), "Repo.Clean->Repo.relPaths.GitPathOutsideRepository"},
		{"rm", repo.Rm(ctx, []string{"../x"}, RmOptions{}), "Repo.Rm->Repo.relPaths.GitPathOutsideRepository"},
		{"intent to add", repo.AddIntentToAdd(ctx, []string{outside}), "Repo.AddIntentToAdd->Repo.relPaths.GitPathOutsideRepository"},
		{"submodules", repo.UpdateSubmodules(ctx, []string{".."}), "Repo.UpdateSubmodules->Repo.relPaths.GitPathOutsideRepository"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { wantRoute(t, tc.err, tc.route) })
	}
	t.Run("git refuses a path it does not know", func(t *testing.T) {
		wantRoute(t, repo.Add(ctx, []string{"no-such-file.txt"}, AddOptions{}), "Repo.Add->Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->classify.GitCommandFailed")
	})
}

func TestAWriteWaitsForTheReadsBeforeIt(t *testing.T) {
	// the status that was asked for before the write sees the files as they were; the write is queued behind it
	e := gittest.NewEnv(t)
	r := statesBase(t, e)
	r.Write("a.txt", "c\n")
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	res, err := repo.Status(ctx, StatusOptions{})
	if err != nil || len(res.Entries) != 1 {
		t.Fatalf("%v %v", res, err)
	}
	if err := repo.Add(ctx, []string{"a.txt"}, AddOptions{}); err != nil {
		t.Fatal(err)
	}
	res, err = repo.Status(ctx, StatusOptions{})
	if err != nil || len(res.Entries) != 1 || res.Entries[0].X != 'M' {
		t.Errorf("after the add: %+v %v", res, err)
	}
}
