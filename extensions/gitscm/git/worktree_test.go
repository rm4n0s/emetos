package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

func TestParseWorktrees(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Worktree
	}{
		{"nothing", "", nil},
		{"the main work tree on a branch", "worktree /srv/app\nHEAD 1111\nbranch refs/heads/main\n\n",
			[]Worktree{{Name: "app", Path: "/srv/app", Head: "1111", Ref: "refs/heads/main", Main: true}}},
		{"a linked one on a branch and one detached", "worktree /srv/app\nHEAD 1111\nbranch refs/heads/main\n\nworktree /srv/app.worktrees/feature-a\nHEAD 2222\nbranch refs/heads/feature/a\n\nworktree /srv/app.worktrees/old\nHEAD 3333\ndetached\n\n",
			[]Worktree{
				{Name: "app", Path: "/srv/app", Head: "1111", Ref: "refs/heads/main", Main: true},
				{Name: "feature-a", Path: "/srv/app.worktrees/feature-a", Head: "2222", Ref: "refs/heads/feature/a"},
				{Name: "old", Path: "/srv/app.worktrees/old", Head: "3333", Detached: true},
			}},
		{"locked with and without a reason, and prunable", "worktree /srv/app\nHEAD 1111\nbranch refs/heads/main\n\nworktree /w/a\nHEAD 2222\ndetached\nlocked on a USB stick\n\nworktree /w/b\nHEAD 3333\ndetached\nlocked\nprunable gitdir file points to non-existent location\n",
			[]Worktree{
				{Name: "app", Path: "/srv/app", Head: "1111", Ref: "refs/heads/main", Main: true},
				{Name: "a", Path: "/w/a", Head: "2222", Detached: true, Locked: true, LockReason: "on a USB stick"},
				{Name: "b", Path: "/w/b", Head: "3333", Detached: true, Locked: true, Prunable: true},
			}},
		{"a bare repository is left out", "worktree /srv/app.git\nbare\n\nworktree /w/a\nHEAD 2222\nbranch refs/heads/x\n\n",
			[]Worktree{{Name: "a", Path: "/w/a", Head: "2222", Ref: "refs/heads/x"}}},
		{"a quoted path", "worktree \"/w/with\\nnewline\"\nHEAD 2222\ndetached\n", []Worktree{{Name: "with\nnewline", Path: "/w/with\nnewline", Head: "2222", Detached: true, Main: true}}},
		{"carriage returns", "worktree /w/a\r\nHEAD 2222\r\nbranch refs/heads/x\r\n", []Worktree{{Name: "a", Path: "/w/a", Head: "2222", Ref: "refs/heads/x", Main: true}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseWorktrees([]byte(tc.in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseWorktrees = %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestWorktreeBranchIsTheShortName(t *testing.T) {
	tests := []struct {
		w    Worktree
		want string
	}{
		{Worktree{Ref: "refs/heads/feature/a"}, "feature/a"},
		{Worktree{Detached: true}, ""},
	}
	for _, tc := range tests {
		if got := tc.w.Branch(); got != tc.want {
			t.Errorf("Branch(%+v) = %q, want %q", tc.w, got, tc.want)
		}
	}
}

// worktreeBase makes a repository with two commits, a tag, a branch and a file that is ignored, and returns it open.
func worktreeBase(t *testing.T) (*gittest.Env, *gittest.Repo, *Repo) {
	t.Helper()
	e := gittest.NewEnv(t)
	r := e.Init("app")
	r.Write("a.txt", "one\n")
	r.Write(".gitignore", "node_modules/\n*.log\n.env\n")
	r.Commit("first commit")
	r.Write("a.txt", "one\ntwo\n")
	r.Commit("second commit")
	r.Git("tag", "v1")
	r.Branch("topic", "")
	return e, r, open(t, e, r.Dir)
}

func TestWorktreesListsTheMainAndTheLinkedOnes(t *testing.T) {
	e, r, repo := worktreeBase(t)
	ctx := context.Background()
	wt := filepath.Join(filepath.Dir(r.Dir), "app.worktrees", "topic")
	r.Git("worktree", "add", "-q", wt, "topic")
	det := filepath.Join(filepath.Dir(r.Dir), "app.worktrees", "det")
	r.Git("worktree", "add", "-q", "--detach", det, "v1")

	list, err := repo.Worktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("worktrees = %+v", list)
	}
	if !list[0].Main || list[0].Path != r.Dir || list[0].Branch() != "main" || list[0].Name != "app" {
		t.Errorf("main = %+v", list[0])
	}
	// git lists the linked work trees in the order of their names
	if list[2].Main || list[2].Path != wt || list[2].Branch() != "topic" || list[2].Name != "topic" || list[2].Detached {
		t.Errorf("linked = %+v", list[2])
	}
	if !list[1].Detached || list[1].Branch() != "" || list[1].Head != strings.TrimSpace(r.Git("rev-parse", "v1^{commit}")) {
		t.Errorf("detached = %+v", list[1])
	}
	// the list is the same from a linked worktree
	again, err := open(t, e, wt).Worktrees(ctx)
	if err != nil || !reflect.DeepEqual(again, list) {
		t.Errorf("from the linked worktree: %+v, %v", again, err)
	}
	// the details name the commit of each
	details, err := repo.WorktreeDetails(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if details[0].Subject != "second commit" || details[1].Subject != "second commit" {
		t.Errorf("subjects = %q, %q", details[0].Subject, details[1].Subject)
	}
	if got, ok := FindWorktree(list, wt+"/"); !ok || got.Name != "topic" {
		t.Errorf("FindWorktree = %+v, %v", got, ok)
	}
	if _, ok := FindWorktree(list, filepath.Join(wt, "x")); ok {
		t.Error("FindWorktree found a folder inside a worktree")
	}
}

func TestAddWorktree(t *testing.T) {
	tests := []struct {
		name string
		opts AddWorktreeOptions
		// branch is the branch the new work tree has checked out, "" for a detached HEAD; head the commit it is at (a rev of the repository)
		branch, head string
		// tracking is the upstream the new branch has
		tracking string
	}{
		{"an existing branch", AddWorktreeOptions{Commitish: "topic"}, "topic", "topic", ""},
		{"a tag, detached", AddWorktreeOptions{Commitish: "v1"}, "", "v1^{commit}", ""},
		{"HEAD, detached", AddWorktreeOptions{}, "", "HEAD", ""},
		{"a new branch at HEAD", AddWorktreeOptions{Branch: "feature/new"}, "feature/new", "HEAD", ""},
		{"a new branch at a tag", AddWorktreeOptions{Branch: "from-tag", Commitish: "v1"}, "from-tag", "v1^{commit}", ""},
		{"a new branch that tracks nothing", AddWorktreeOptions{Branch: "plain", Commitish: "origin/main", NoTrack: true}, "plain", "origin/main", ""},
		{"a new branch that tracks its start", AddWorktreeOptions{Branch: "tracked", Commitish: "origin/main"}, "tracked", "origin/main", "origin/main"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, r, repo := worktreeBase(t)
			r.AddRemote("origin")
			r.Git("push", "-q", "origin", "main")
			r.Git("fetch", "-q", "origin")
			tc.opts.Path = filepath.Join(filepath.Dir(r.Dir), "app.worktrees", "new")
			if err := repo.AddWorktree(context.Background(), tc.opts); err != nil {
				t.Fatal(err)
			}
			wt := tc.opts.Path
			branch := strings.TrimSpace(r.Git("-C", wt, "branch", "--show-current"))
			if branch != tc.branch {
				t.Errorf("branch = %q, want %q", branch, tc.branch)
			}
			if got, want := strings.TrimSpace(r.Git("-C", wt, "rev-parse", "HEAD")), strings.TrimSpace(r.Git("rev-parse", tc.head)); got != want {
				t.Errorf("HEAD = %s, want %s", got, want)
			}
			if tc.branch != "" {
				up, _, _ := r.GitErr("-C", wt, "rev-parse", "--abbrev-ref", "@{upstream}")
				if strings.TrimSpace(up) != tc.tracking {
					t.Errorf("upstream = %q, want %q", strings.TrimSpace(up), tc.tracking)
				}
			}
			if _, err := os.Stat(filepath.Join(wt, "a.txt")); err != nil {
				t.Errorf("the work tree has no files: %v", err)
			}
		})
	}
}

func TestAddWorktreeARelativePathIsRelativeToTheRoot(t *testing.T) {
	_, r, repo := worktreeBase(t)
	if err := repo.AddWorktree(context.Background(), AddWorktreeOptions{Path: "../rel-wt", Commitish: "topic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.Dir), "rel-wt", "a.txt")); err != nil {
		t.Error(err)
	}
}

func TestAddWorktreeFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("no folder", func(t *testing.T) {
		_, _, repo := worktreeBase(t)
		wantRoute(t, repo.AddWorktree(ctx, AddWorktreeOptions{}), "Repo.AddWorktree.GitWorktreeAddPathEmpty")
	})
	t.Run("a start that is not a ref name", func(t *testing.T) {
		_, r, repo := worktreeBase(t)
		wantRoute(t, repo.AddWorktree(ctx, AddWorktreeOptions{Path: filepath.Join(r.Dir, "..", "x"), Commitish: "--force"}), "Repo.AddWorktree->refName.GitRefInvalid")
		wantRoute(t, repo.AddWorktree(ctx, AddWorktreeOptions{Path: filepath.Join(r.Dir, "..", "x"), Branch: "-b"}), "Repo.AddWorktree->refName.GitRefInvalid")
	})
	t.Run("the folder is taken", func(t *testing.T) {
		_, r, repo := worktreeBase(t)
		dir := filepath.Join(filepath.Dir(r.Dir), "taken")
		r.Write("../taken/file.txt", "x\n")
		err := repo.AddWorktree(ctx, AddWorktreeOptions{Path: dir, Commitish: "topic"})
		appErr := routeOf(t, err, viaWrite+"classify.WorktreeAlreadyExists")
		if appErr != nil && !strings.Contains(appErr.Message, "already exists") {
			t.Errorf("message = %q", appErr.Message)
		}
	})
	t.Run("the branch is checked out in another work tree", func(t *testing.T) {
		_, r, repo := worktreeBase(t)
		first := filepath.Join(filepath.Dir(r.Dir), "first")
		if err := repo.AddWorktree(ctx, AddWorktreeOptions{Path: first, Commitish: "topic"}); err != nil {
			t.Fatal(err)
		}
		err := repo.AddWorktree(ctx, AddWorktreeOptions{Path: filepath.Join(filepath.Dir(r.Dir), "second"), Commitish: "topic"})
		appErr := routeOf(t, err, viaWrite+"classify.WorktreeBranchAlreadyUsed")
		if appErr != nil {
			if stderr, _ := appErr.Metadata["stderr"].(string); !strings.Contains(stderr, first) {
				t.Errorf("stderr = %q does not name %s", stderr, first)
			}
		}
		// and the main one
		err = repo.AddWorktree(ctx, AddWorktreeOptions{Path: filepath.Join(filepath.Dir(r.Dir), "third"), Commitish: "main"})
		wantRoute(t, err, viaWrite+"classify.WorktreeBranchAlreadyUsed")
	})
	t.Run("a new branch with a taken name", func(t *testing.T) {
		_, r, repo := worktreeBase(t)
		err := repo.AddWorktree(ctx, AddWorktreeOptions{Path: filepath.Join(filepath.Dir(r.Dir), "x"), Branch: "topic"})
		wantRoute(t, err, viaWrite+"classify.BranchAlreadyExists")
	})
}

func TestRemoveWorktree(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		// prepare changes the new work tree; force is what RemoveWorktree is given; want the tag of the failure, "" for none
		prepare func(t *testing.T, wt string)
		force   bool
		want    string
	}{
		{"a clean one", func(*testing.T, string) {}, false, ""},
		{"one with a changed file", func(t *testing.T, wt string) { writeTestFile(t, filepath.Join(wt, "a.txt"), "changed\n") }, false, "WorktreeContainsChanges"},
		{"one with an untracked file", func(t *testing.T, wt string) { writeTestFile(t, filepath.Join(wt, "new.txt"), "new\n") }, false, "WorktreeContainsChanges"},
		{"one with changes, forced", func(t *testing.T, wt string) { writeTestFile(t, filepath.Join(wt, "a.txt"), "changed\n") }, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, r, repo := worktreeBase(t)
			wt := filepath.Join(filepath.Dir(r.Dir), "app.worktrees", "topic")
			if err := repo.AddWorktree(ctx, AddWorktreeOptions{Path: wt, Commitish: "topic"}); err != nil {
				t.Fatal(err)
			}
			tc.prepare(t, wt)
			err := repo.RemoveWorktree(ctx, wt, tc.force)
			_, statErr := os.Stat(wt)
			if tc.want != "" {
				wantRoute(t, err, viaWrite+"classify."+tc.want)
				if statErr != nil {
					t.Errorf("the work tree was removed: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !os.IsNotExist(statErr) {
				t.Errorf("the work tree is still there: %v", statErr)
			}
			list, err := repo.Worktrees(ctx)
			if err != nil || len(list) != 1 {
				t.Errorf("worktrees = %+v, %v", list, err)
			}
		})
	}
	t.Run("a locked one is refused whatever the force", func(t *testing.T) {
		_, r, repo := worktreeBase(t)
		wt := filepath.Join(filepath.Dir(r.Dir), "app.worktrees", "topic")
		if err := repo.AddWorktree(ctx, AddWorktreeOptions{Path: wt, Commitish: "topic"}); err != nil {
			t.Fatal(err)
		}
		r.Git("worktree", "lock", "--reason", "in use", wt)
		wantRoute(t, repo.RemoveWorktree(ctx, wt, false), viaWrite+"classify.GitCommandFailed")
		wantRoute(t, repo.RemoveWorktree(ctx, wt, true), viaWrite+"classify.GitCommandFailed")
		list, _ := repo.Worktrees(ctx)
		if len(list) != 2 || !list[1].Locked || list[1].LockReason != "in use" {
			t.Errorf("worktrees = %+v", list)
		}
	})
	t.Run("no folder", func(t *testing.T) {
		_, _, repo := worktreeBase(t)
		wantRoute(t, repo.RemoveWorktree(ctx, "", false), "Repo.RemoveWorktree.GitWorktreeRemovePathEmpty")
	})
}

// writeTestFile writes a file and the folders above it.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
