package git

import (
	"context"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// stashBase makes a repository with a few committed files and returns it open.
func stashBase(t *testing.T) (*gittest.Env, *gittest.Repo, *Repo) {
	t.Helper()
	e := gittest.NewEnv(t)
	r := e.Init("stash")
	r.Write("a.txt", "a\none\ntwo\n")
	r.Write("b.txt", "b\none\ntwo\n")
	r.Write("old.txt", "an old name\nwith several lines\nthat git can match\nas a rename\n")
	r.Commit("base")
	return e, r, open(t, e, r.Dir)
}

// gitAtLeast skips the test when the installed git is older than major.minor, saying so.
func gitAtLeast(t *testing.T, e *gittest.Env, dir string, major, minor int) {
	t.Helper()
	v, err := ParseVersion(e.Git(dir, "--version"))
	if err != nil {
		t.Fatal(err)
	}
	if !v.AtLeast(major, minor) {
		t.Skipf("git %s is older than %d.%d, which this case needs", v, major, minor)
	}
}

// viaWrite is the part of a route between a method that writes and the classification of what git said.
const viaWrite = "Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->"

func stashList(r *gittest.Repo) []string {
	out := strings.TrimSpace(r.Git("stash", "list", "--format=%gs"))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func TestStash(t *testing.T) {
	tests := []struct {
		name  string
		setup func(r *gittest.Repo)
		opts  StashOptions
		// left is the status after the stash, " | " apart; held are the files the stash holds in its tree against the base
		left string
		// subject is the end of the reflog subject of the stash
		subject string
		need235 bool
	}{
		{"tracked changes only", func(r *gittest.Repo) { r.Write("a.txt", "changed\n"); r.Write("u.txt", "untracked\n") },
			StashOptions{}, "?? u.txt", "WIP on", false},
		{"with the untracked files", func(r *gittest.Repo) { r.Write("a.txt", "changed\n"); r.Write("u.txt", "untracked\n") },
			StashOptions{IncludeUntracked: true}, "", "WIP on", false},
		{"with a message", func(r *gittest.Repo) { r.Write("a.txt", "changed\n") },
			StashOptions{Message: "WIP loader cache"}, "", "On main: WIP loader cache", false},
		{"a message that starts with a dash and has quotes", func(r *gittest.Repo) { r.Write("a.txt", "changed\n") },
			StashOptions{Message: `-m "quoted" --all`}, "", `On main: -m "quoted" --all`, false},
		{"only what is staged", func(r *gittest.Repo) {
			r.Write("a.txt", "staged\n")
			r.Git("add", "a.txt")
			r.Write("b.txt", "not staged\n")
		}, StashOptions{Staged: true, Message: "just the index"}, " M b.txt", "On main: just the index", true},
		{"staged and unstaged changes of one file", func(r *gittest.Repo) {
			r.Write("a.txt", "staged\n")
			r.Git("add", "a.txt")
			r.Write("a.txt", "staged and more\n")
		}, StashOptions{}, "", "WIP on", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, r, repo := stashBase(t)
			if tc.need235 {
				gitAtLeast(t, e, r.Dir, 2, 35)
			}
			tc.setup(r)
			if err := repo.Stash(context.Background(), tc.opts); err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(strings.Split(strings.TrimSpace(r.Git("status", "--porcelain")), "\n"), " | "); got != strings.TrimSpace(tc.left) {
				t.Errorf("status after the stash = %q, want %q", got, tc.left)
			}
			list := stashList(r)
			if len(list) != 1 || !strings.Contains(list[0], tc.subject) {
				t.Errorf("stash list = %q, want one ending in %q", list, tc.subject)
			}
		})
	}
}

func TestStashWithNothingToStashFailsWithNoLocalChanges(t *testing.T) {
	_, r, repo := stashBase(t)
	r.Write("u.txt", "untracked only\n")
	err := repo.Stash(context.Background(), StashOptions{})
	wantRoute(t, err, "Repo.Stash->"+viaWrite+"classifySuccess.NoLocalChanges")
	if len(stashList(r)) != 0 {
		t.Error("a stash was made")
	}
}

func TestStashApplyAndPop(t *testing.T) {
	tests := []struct {
		name  string
		run   func(ctx context.Context, repo *Repo) error
		kept  int    // stashes left
		want  string // contents of a.txt
		wantB string // contents of b.txt
	}{
		{"pop the latest", func(ctx context.Context, repo *Repo) error {
			return repo.StashPop(ctx, StashLatest, StashApplyOptions{})
		}, 1, "second\n", "b\none\ntwo\n"},
		{"apply the latest", func(ctx context.Context, repo *Repo) error {
			return repo.StashApply(ctx, StashLatest, StashApplyOptions{})
		}, 2, "second\n", "b\none\ntwo\n"},
		{"pop the one with an index", func(ctx context.Context, repo *Repo) error { return repo.StashPop(ctx, 1, StashApplyOptions{}) }, 1, "a\none\ntwo\n", "first b\n"},
		{"apply the one with an index", func(ctx context.Context, repo *Repo) error { return repo.StashApply(ctx, 1, StashApplyOptions{}) }, 2, "a\none\ntwo\n", "first b\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, r, repo := stashBase(t)
			r.Write("b.txt", "first b\n")
			r.Stash("first", false)
			r.Write("a.txt", "second\n")
			r.Stash("second", false)
			if err := tc.run(context.Background(), repo); err != nil {
				t.Fatal(err)
			}
			if got := len(stashList(r)); got != tc.kept {
				t.Errorf("%d stashes left, want %d", got, tc.kept)
			}
			if got := r.Read("a.txt"); got != tc.want {
				t.Errorf("a.txt = %q, want %q", got, tc.want)
			}
			if got := r.Read("b.txt"); got != tc.wantB {
				t.Errorf("b.txt = %q, want %q", got, tc.wantB)
			}
		})
	}
}

func TestStashApplyReinstatesTheIndexOnRequest(t *testing.T) {
	for _, reinstate := range []bool{false, true} {
		t.Run(map[bool]string{false: "without", true: "with"}[reinstate], func(t *testing.T) {
			_, r, repo := stashBase(t)
			r.Write("a.txt", "staged\n")
			r.Git("add", "a.txt")
			r.Stash("with an index", false)
			if err := repo.StashApply(context.Background(), 0, StashApplyOptions{ReinstateIndex: reinstate}); err != nil {
				t.Fatal(err)
			}
			want := " M a.txt"
			if reinstate {
				want = "M  a.txt"
			}
			if got := strings.TrimRight(r.Git("status", "--porcelain"), "\n"); got != want {
				t.Errorf("status = %q, want %q", got, want)
			}
		})
	}
}

func TestStashPopOntoAChangedTree(t *testing.T) {
	tests := []struct {
		name      string
		change    func(r *gittest.Repo)
		route     string
		keepsIt   bool
		wantMerge bool
	}{
		{"a change of another file applies", func(r *gittest.Repo) { r.Write("b.txt", "other\n") }, "", false, false},
		{"a committed change of the same file in another line merges", func(r *gittest.Repo) { r.Write("a.txt", "a\none\ntwo\nthree\n"); r.Commit("another line", "a.txt") }, "", false, false},
		{"a change of the same line conflicts", func(r *gittest.Repo) { r.Write("a.txt", "mine\none\ntwo\n"); r.Git("add", "a.txt"); r.Commit("mine") },
			"Repo.StashPop->Repo.popOrApply->" + viaWrite + "classify.StashConflict", true, true},
		{"an uncommitted change that the stash would overwrite", func(r *gittest.Repo) { r.Write("a.txt", "dirty\n") },
			"Repo.StashPop->Repo.popOrApply->" + viaWrite + "classify.LocalChangesOverwritten", true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, r, repo := stashBase(t)
			r.Write("a.txt", "stashed\none\ntwo\n")
			r.Stash("to pop", false)
			tc.change(r)
			err := repo.StashPop(context.Background(), StashLatest, StashApplyOptions{})
			if tc.route == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				wantRoute(t, err, tc.route)
			}
			if kept := len(stashList(r)) == 1; kept != tc.keepsIt {
				t.Errorf("the stash is kept = %v, want %v", kept, tc.keepsIt)
			}
			if merge := strings.Contains(r.Git("status", "--porcelain"), "UU a.txt"); merge != tc.wantMerge {
				t.Errorf("a.txt is in conflict = %v, want %v", merge, tc.wantMerge)
			}
		})
	}
}

func TestStashApplyOfAnApplyThatConflictsKeepsTheStash(t *testing.T) {
	_, r, repo := stashBase(t)
	r.Write("a.txt", "stashed\none\ntwo\n")
	r.Stash("to apply", false)
	r.Write("a.txt", "mine\none\ntwo\n")
	r.Commit("mine", "a.txt")
	wantRoute(t, repo.StashApply(context.Background(), 0, StashApplyOptions{}), "Repo.StashApply->Repo.popOrApply->"+viaWrite+"classify.StashConflict")
	if len(stashList(r)) != 1 {
		t.Error("the stash is gone")
	}
}

func TestStashWithoutStashesFailsWithNoStashFound(t *testing.T) {
	_, _, repo := stashBase(t)
	ctx := context.Background()
	wantRoute(t, repo.StashPop(ctx, StashLatest, StashApplyOptions{}), "Repo.StashPop->Repo.popOrApply->"+viaWrite+"classify.NoStashFound")
	wantRoute(t, repo.StashApply(ctx, 3, StashApplyOptions{}), "Repo.StashApply->Repo.popOrApply->"+viaWrite+"classify.NoStashFound")
	wantRoute(t, repo.StashDrop(ctx, 0), "Repo.StashDrop->"+viaWrite+"classify.NoStashFound")
}

func TestStashDropAndClear(t *testing.T) {
	_, r, repo := stashBase(t)
	ctx := context.Background()
	for _, name := range []string{"one", "two", "three"} {
		r.Write("a.txt", name+"\n")
		r.Stash(name, false)
	}
	if err := repo.StashDrop(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got := stashList(r); len(got) != 2 || !strings.HasSuffix(got[0], "three") || !strings.HasSuffix(got[1], "one") {
		t.Errorf("after dropping stash@{1}: %q", got)
	}
	if err := repo.StashDrop(ctx, StashLatest); err != nil {
		t.Fatal(err)
	}
	if got := stashList(r); len(got) != 1 || !strings.HasSuffix(got[0], "one") {
		t.Errorf("after dropping the latest: %q", got)
	}
	r.Write("a.txt", "again\n")
	r.Stash("again", false)
	if err := repo.StashClear(ctx); err != nil {
		t.Fatal(err)
	}
	if got := stashList(r); got != nil {
		t.Errorf("after clearing: %q", got)
	}
	if err := repo.StashClear(ctx); err != nil {
		t.Errorf("clearing an empty stash: %v", err)
	}
}

func TestStashChanges(t *testing.T) {
	_, r, repo := stashBase(t)
	ctx := context.Background()
	base := r.Head()
	r.Write("a.txt", "changed\n")
	r.Remove("b.txt")
	r.Git("mv", "old.txt", "new.txt")
	r.Write("added.txt", "added\n")
	r.Git("add", "added.txt")
	r.Write("u/one.txt", "u1\n")
	r.Write("two.txt", "u2\n")
	r.Stash("everything", true)
	r.Write("a.txt", "only a\n")
	r.Stash("tracked only", false)

	got, err := repo.StashChanges(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Base != base || got.Stash.Index != 1 || got.Stash.Description != "everything" || got.Untracked == "" || len(got.Stash.Parents) != 3 {
		t.Errorf("stash 1 = base %s untracked %q stash %+v", got.Base, got.Untracked, got.Stash)
	}
	var lines []string
	for _, c := range got.Changes {
		s := string(c.Status) + " " + c.Path
		if c.OrigPath != "" {
			s = string(c.Status) + " " + c.OrigPath + ">" + c.Path
		}
		if c.Untracked {
			s += " (untracked)"
		}
		lines = append(lines, s)
	}
	want := "M a.txt|A added.txt|D b.txt|R old.txt>new.txt|A two.txt (untracked)|A u/one.txt (untracked)"
	if g := strings.Join(lines, "|"); g != want {
		t.Errorf("changes of stash 1:\n got %s\nwant %s", g, want)
	}

	tracked, err := repo.StashChanges(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracked.Changes) != 1 || tracked.Changes[0].Path != "a.txt" || tracked.Untracked != "" || tracked.Changes[0].Untracked {
		t.Errorf("changes of stash 0 = %+v", tracked)
	}

	// the stash is still read by its hash after it was popped
	hash := got.Stash.Hash
	if err := repo.StashPop(ctx, 0, StashApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	r.Git("checkout", "--", "a.txt")
	if err := repo.StashDrop(ctx, 0); err != nil {
		t.Fatal(err)
	}
	again, err := repo.StashChangesAt(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Changes) != len(got.Changes) || again.Base != base || again.Untracked != got.Untracked {
		t.Errorf("a dropped stash read by its hash = %+v", again)
	}
}

func TestStashChangesFailures(t *testing.T) {
	_, _, repo := stashBase(t)
	ctx := context.Background()
	_, err := repo.StashChanges(ctx, 0)
	wantRoute(t, err, "Repo.StashChanges.GitStashMissing")
	_, err = repo.StashChangesAt(ctx, "--output=/tmp/x")
	wantRoute(t, err, "Repo.StashChangesAt.GitStashHashInvalid")
	_, err = repo.StashChangesAt(ctx, strings.Repeat("a", 40))
	if err == nil {
		t.Error("a hash git does not have was read")
	}
}
