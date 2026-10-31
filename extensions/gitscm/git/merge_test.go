package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// Merge, rebase, cherry-pick and the commands around them against real repositories (step 24.14).

// mergeBase makes a repository on main with a.txt of three lines. The branch "clean" adds b.txt; the branch "topic" changes line 2 of
// a.txt in one commit and adds c.txt in a second; main changes line 2 of a.txt too (a conflict with topic) and has a third commit that
// only adds d.txt; "later" is main plus a change of d.txt.
func mergeBase(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	r := e.Init("merge")
	r.Write("a.txt", "one\ntwo\nthree\n")
	r.Commit("Add a")
	r.Branch("clean", "")
	r.Branch("topic", "")
	r.Checkout("clean")
	r.Write("b.txt", "bee\n")
	r.Commit("Add b")
	r.Checkout("topic")
	r.Write("a.txt", "one\ntopic\nthree\n")
	r.Commit("Change a in topic")
	r.Write("c.txt", "sea\n")
	r.Commit("Add c")
	r.Checkout("main")
	r.Write("a.txt", "one\nmain\nthree\n")
	r.Commit("Change a in main")
	r.Write("d.txt", "dee\n")
	r.Commit("Add d")
	r.Branch("later", "")
	r.Checkout("later")
	r.Write("d.txt", "dee later\n")
	r.Commit("Change d later")
	r.Checkout("main")
	return r
}

// state tells which operation files the git directory has.
func state(r *gittest.Repo) string {
	var found []string
	for _, f := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(r.Dir, ".git", f)); err == nil {
			found = append(found, f)
		}
	}
	return strings.Join(found, ",")
}

func TestMerge(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		setup     func(r *gittest.Repo)
		ref       string
		route     string // the route of the error, "" for none
		wantState string
		wantFile  string // a file that must exist afterwards
		parents   int    // the parents of HEAD afterwards, 0 to skip
	}{
		{name: "a merge without conflicts", ref: "clean", wantFile: "b.txt", parents: 2},
		{name: "a fast-forward", setup: func(r *gittest.Repo) { r.Git("reset", "-q", "--hard", "clean~1") }, ref: "clean", wantFile: "b.txt", parents: 1},
		{name: "a conflict stays in progress", ref: "topic", route: "Repo.Merge" + writeRoute + "Conflict", wantState: "MERGE_HEAD"},
		{name: "a ref that does not exist", ref: "nope", route: "Repo.Merge" + writeRoute + "GitCommandFailed"},
		{name: "a ref that looks like an option", ref: "--abort", route: "refName.GitRefInvalid"},
		{name: "local changes the merge would overwrite", setup: func(r *gittest.Repo) { r.Write("d.txt", "mine\n") }, ref: "later",
			route: "Repo.Merge" + writeRoute + "DirtyWorkTree"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			r := mergeBase(t, e)
			if tc.setup != nil {
				tc.setup(r)
			}
			repo := open(t, e, r.Dir)
			err := repo.Merge(ctx, tc.ref)
			if tc.route != "" {
				wantRoute(t, err, tc.route)
			} else if err != nil {
				t.Fatal(err)
			}
			if got := state(r); got != tc.wantState {
				t.Errorf("state = %q, want %q", got, tc.wantState)
			}
			if tc.wantFile != "" {
				if _, err := os.Stat(r.Path(tc.wantFile)); err != nil {
					t.Errorf("%s is missing: %v", tc.wantFile, err)
				}
			}
			if tc.parents != 0 {
				if n := len(strings.Fields(r.Git("log", "-1", "--format=%P"))); n != tc.parents {
					t.Errorf("HEAD has %d parents, want %d", n, tc.parents)
				}
			}
		})
	}
}

func TestMergeAbort(t *testing.T) {
	ctx := context.Background()
	e := gittest.NewEnv(t)
	r := mergeBase(t, e)
	repo := open(t, e, r.Dir)
	before := r.Head()
	wantRoute(t, repo.MergeAbort(ctx), "Repo.MergeAbort"+writeRoute+"GitCommandFailed") // no merge to abort
	wantRoute(t, repo.Merge(ctx, "topic"), "Repo.Merge"+writeRoute+"Conflict")
	if err := repo.MergeAbort(ctx); err != nil {
		t.Fatal(err)
	}
	if state(r) != "" || r.Head() != before || r.Read("a.txt") != "one\nmain\nthree\n" {
		t.Errorf("the abort left state %q, HEAD %s, a.txt %q", state(r), r.Head(), r.Read("a.txt"))
	}
}

func TestContinueAMergeAfterTheConflictsAreResolved(t *testing.T) {
	ctx := context.Background()
	e := gittest.NewEnv(t)
	r := mergeBase(t, e)
	repo := open(t, e, r.Dir)
	wantRoute(t, repo.Merge(ctx, "topic"), "Repo.Merge"+writeRoute+"Conflict")
	// the files are still unmerged
	wantRoute(t, repo.Continue(ctx, OperationMerge), "Repo.Continue"+writeRoute+"UnmergedChanges")
	r.Write("a.txt", "one\nboth\nthree\n")
	r.Git("add", "a.txt")
	if err := repo.Continue(ctx, OperationMerge); err != nil {
		t.Fatal(err)
	}
	if state(r) != "" {
		t.Errorf("the merge is still in progress: %s", state(r))
	}
	if got := r.Git("log", "-1", "--format=%s"); !strings.HasPrefix(got, "Merge branch 'topic'") {
		t.Errorf("the merge commit says %q", got)
	}
	if n := len(strings.Fields(r.Git("log", "-1", "--format=%P"))); n != 2 {
		t.Errorf("HEAD has %d parents", n)
	}
}

func TestRebase(t *testing.T) {
	ctx := context.Background()
	t.Run("two commits, a conflict in the first, continue", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		r.Checkout("topic")
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Rebase(ctx, "main", RebaseOptions{}), "Repo.Rebase"+writeRoute+"Conflict")
		if got := state(r); got != "rebase-merge" {
			t.Fatalf("state = %q", got)
		}
		wantRoute(t, repo.Continue(ctx, OperationRebase), "Repo.Continue"+writeRoute+"UnmergedChanges")
		r.Write("a.txt", "one\nresolved\nthree\n")
		r.Git("add", "a.txt")
		if err := repo.Continue(ctx, OperationRebase); err != nil {
			t.Fatal(err)
		}
		if state(r) != "" {
			t.Errorf("the rebase is still in progress: %s", state(r))
		}
		if got := r.Git("log", "--format=%s", "main..topic"); got != "Add c\nChange a in topic" {
			t.Errorf("topic has the commits %q", got)
		}
		if got := r.Read("a.txt"); got != "one\nresolved\nthree\n" {
			t.Errorf("a.txt = %q", got)
		}
	})
	t.Run("a conflict in the second commit", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		// topic gets a first commit that does not conflict, then the one that does
		r.Checkout("topic")
		r.Git("reset", "-q", "--hard", "main~2")
		r.Write("c.txt", "sea\n")
		r.Commit("Add c first")
		r.Write("a.txt", "one\ntopic\nthree\n")
		r.Commit("Change a in topic")
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Rebase(ctx, "main", RebaseOptions{}), "Repo.Rebase"+writeRoute+"Conflict")
		if got := r.Git("log", "--format=%s", "main..HEAD"); got != "Add c first" {
			t.Errorf("the commits already put on main: %q", got)
		}
		commit, err := repo.CommitAt(ctx, "REBASE_HEAD")
		if err != nil {
			t.Fatal(err)
		}
		if commit.Message != "Change a in topic" {
			t.Errorf("the stopped commit is %q", commit.Message)
		}
		r.Write("a.txt", "one\nresolved\nthree\n")
		r.Git("add", "a.txt")
		if err := repo.Continue(ctx, OperationRebase); err != nil {
			t.Fatal(err)
		}
		if got := r.Git("log", "--format=%s", "main..topic"); got != "Change a in topic\nAdd c first" {
			t.Errorf("topic has the commits %q", got)
		}
	})
	t.Run("abort puts the branch back", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		r.Checkout("topic")
		before := r.Head()
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Rebase(ctx, "main", RebaseOptions{}), "Repo.Rebase"+writeRoute+"Conflict")
		if err := repo.RebaseAbort(ctx); err != nil {
			t.Fatal(err)
		}
		if state(r) != "" || r.Head() != before {
			t.Errorf("state %q, HEAD %s, want %s", state(r), r.Head(), before)
		}
		wantRoute(t, repo.RebaseAbort(ctx), "Repo.RebaseAbort"+writeRoute+"GitCommandFailed")
	})
	t.Run("skip leaves the commit out", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		r.Checkout("topic")
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Rebase(ctx, "main", RebaseOptions{}), "Repo.Rebase"+writeRoute+"Conflict")
		if err := repo.RebaseSkip(ctx); err != nil {
			t.Fatal(err)
		}
		if state(r) != "" {
			t.Errorf("the rebase is still in progress: %s", state(r))
		}
		if got := r.Git("log", "--format=%s", "main..topic"); got != "Add c" {
			t.Errorf("topic has the commits %q", got)
		}
	})
	t.Run("onto", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		r.Checkout("clean")
		repo := open(t, e, r.Dir)
		// the commits of clean after main~2 (the first commit) are put on main
		if err := repo.Rebase(ctx, "main~2", RebaseOptions{Onto: "main"}); err != nil {
			t.Fatal(err)
		}
		if got := r.Git("log", "--format=%s", "main..HEAD"); got != "Add b" {
			t.Errorf("clean has the commits %q", got)
		}
	})
	t.Run("with local changes", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		r.Checkout("topic")
		r.Write("a.txt", "dirty\n")
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Rebase(ctx, "main", RebaseOptions{}), "Repo.Rebase"+writeRoute+"DirtyWorkTree")
	})
	t.Run("a name that looks like an option", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Rebase(ctx, "--root", RebaseOptions{}), "refName.GitRefInvalid")
		wantRoute(t, repo.Rebase(ctx, "main", RebaseOptions{Onto: "--x"}), "refName.GitRefInvalid")
	})
}

func TestRebaseInteractiveRunsTheSequenceEditor(t *testing.T) {
	ctx := context.Background()
	e := gittest.NewEnv(t)
	r := mergeBase(t, e)
	r.Checkout("topic")
	r.Git("reset", "-q", "--hard", "main~2")
	r.Write("c.txt", "sea\n")
	r.Commit("Add c first")
	r.Write("e.txt", "ee\n")
	r.Commit("Add e second")
	// the editor of the todo list reverses the two commits
	editor := filepath.Join(e.BinDir, "reverse-todo")
	script := "#!/bin/sh\nawk '{ lines[NR] = $0 } END { for (i = NR; i >= 1; i--) print lines[i] }' \"$1\" | grep -v '^#' > \"$1.new\"\nmv \"$1.new\" \"$1\"\n"
	if err := os.WriteFile(editor, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := open(t, e.With("GIT_SEQUENCE_EDITOR="+editor), r.Dir)
	if err := repo.Rebase(ctx, "main~2", RebaseOptions{Interactive: true}); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("log", "--format=%s", "main~2..HEAD"); got != "Add c first\nAdd e second" {
		t.Errorf("the commits after the todo list was reversed: %q", got)
	}
}

func TestCherryPick(t *testing.T) {
	ctx := context.Background()
	t.Run("without conflicts", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		repo := open(t, e, r.Dir)
		if err := repo.CherryPick(ctx, "clean"); err != nil {
			t.Fatal(err)
		}
		if got := r.Git("log", "-1", "--format=%s"); got != "Add b" || state(r) != "" {
			t.Errorf("HEAD is %q, state %q", got, state(r))
		}
	})
	t.Run("a conflict, continue", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.CherryPick(ctx, "topic~1"), "Repo.CherryPick"+writeRoute+"CherryPickConflict")
		if got := state(r); got != "CHERRY_PICK_HEAD" {
			t.Fatalf("state = %q", got)
		}
		wantRoute(t, repo.Continue(ctx, OperationCherryPick), "Repo.Continue"+writeRoute+"UnmergedChanges")
		r.Write("a.txt", "one\nresolved\nthree\n")
		r.Git("add", "a.txt")
		if err := repo.Continue(ctx, OperationCherryPick); err != nil {
			t.Fatal(err)
		}
		if state(r) != "" || r.Git("log", "-1", "--format=%s") != "Change a in topic" {
			t.Errorf("state %q, HEAD %q", state(r), r.Git("log", "-1", "--format=%s"))
		}
	})
	t.Run("a conflict, abort", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		repo := open(t, e, r.Dir)
		before := r.Head()
		wantRoute(t, repo.CherryPick(ctx, "topic~1"), "Repo.CherryPick"+writeRoute+"CherryPickConflict")
		if err := repo.CherryPickAbort(ctx); err != nil {
			t.Fatal(err)
		}
		if state(r) != "" || r.Head() != before {
			t.Errorf("state %q, HEAD %s", state(r), r.Head())
		}
		wantRoute(t, repo.CherryPickAbort(ctx), "Repo.CherryPickAbort"+writeRoute+"GitCommandFailed")
	})
	t.Run("changes that are in the branch already end the pick", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		// the same change as main~1, made on another branch
		r.Checkout("clean")
		r.Write("a.txt", "one\nmain\nthree\n")
		r.Commit("The same change as main")
		r.Checkout("main")
		repo := open(t, e, r.Dir)
		before := r.Head()
		wantRoute(t, repo.CherryPick(ctx, "clean"), "Repo.CherryPick"+writeRoute+"CherryPickEmpty")
		if state(r) != "" || r.Head() != before {
			t.Errorf("the empty pick was not ended: state %q", state(r))
		}
	})
	t.Run("a commit that does not exist is no conflict", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.CherryPick(ctx, "nope"), "Repo.CherryPick"+writeRoute+"GitCommandFailed")
		wantRoute(t, repo.CherryPick(ctx, "-n"), "refName.GitRefInvalid")
	})
}

func TestCommitAt(t *testing.T) {
	ctx := context.Background()
	e := gittest.NewEnv(t)
	r := mergeBase(t, e)
	repo := open(t, e, r.Dir)
	c, err := repo.CommitAt(ctx, "topic")
	if err != nil {
		t.Fatal(err)
	}
	if c.Message != "Add c" || len(c.Parents) != 1 {
		t.Errorf("topic is %+v", c)
	}
	_, err = repo.CommitAt(ctx, "nope")
	wantRoute(t, err, "Repo.CommitAt->Repo.Log->Repo.stream->Queue.Do->Repo.stream.func1->Runner.RunStream")
	_, err = repo.CommitAt(ctx, "-1")
	wantRoute(t, err, "refName.GitRefInvalid")
}

func TestMergeFile(t *testing.T) {
	ctx := context.Background()
	t.Run("the stages of a merge", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Merge(ctx, "topic"), "Repo.Merge"+writeRoute+"Conflict")
		tests := []struct {
			name string
			o    MergeFileOptions
			want string
		}{
			{"plain", MergeFileOptions{}, "one\n<<<<<<< HEAD\nmain\n=======\ntopic\n>>>>>>> theirs\nthree\n"},
			{"labels", MergeFileOptions{Ours: "ours", Theirs: "MERGE_HEAD"}, "one\n<<<<<<< ours\nmain\n=======\ntopic\n>>>>>>> MERGE_HEAD\nthree\n"},
			{"diff3", MergeFileOptions{Diff3: true}, "one\n<<<<<<< HEAD\nmain\n||||||| base\ntwo\n=======\ntopic\n>>>>>>> theirs\nthree\n"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				// the work tree file is changed by the user meanwhile; the stages are what counts
				r.Write("a.txt", "something else\n")
				res, err := repo.MergeFile(ctx, filepath.Join(r.Dir, "a.txt"), tc.o)
				if err != nil {
					t.Fatal(err)
				}
				if res.Text != tc.want || res.Conflicts != 1 {
					t.Errorf("text %q, conflicts %d, want %q, 1", res.Text, res.Conflicts, tc.want)
				}
			})
		}
	})
	t.Run("both sides added", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := e.Init("addadd")
		r.Write("keep.txt", "k\n")
		r.Commit("Start")
		r.Branch("other", "")
		r.Write("n.txt", "ours\n")
		r.Commit("Add n here")
		r.Checkout("other")
		r.Write("n.txt", "theirs\n")
		r.Commit("Add n there")
		r.Checkout("main")
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Merge(ctx, "other"), "Repo.Merge"+writeRoute+"Conflict")
		res, err := repo.MergeFile(ctx, "n.txt", MergeFileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if want := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> theirs\n"; res.Text != want || res.Conflicts != 1 {
			t.Errorf("text %q, conflicts %d", res.Text, res.Conflicts)
		}
	})
	t.Run("a file deleted on one side", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		r.Checkout("clean")
		r.Git("rm", "-q", "a.txt")
		r.Commit("Delete a")
		r.Checkout("main")
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Merge(ctx, "clean"), "Repo.Merge"+writeRoute+"Conflict")
		_, err := repo.MergeFile(ctx, "a.txt", MergeFileOptions{})
		wantRoute(t, err, "Repo.MergeFile.MergeStageMissing")
	})
	t.Run("a file that is not in conflict", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		repo := open(t, e, r.Dir)
		_, err := repo.MergeFile(ctx, "a.txt", MergeFileOptions{})
		wantRoute(t, err, "Repo.MergeFile.MergeStageMissing")
		_, err = repo.MergeFile(ctx, "/elsewhere/a.txt", MergeFileOptions{})
		wantRoute(t, err, "Repo.MergeFile->Repo.relPaths.GitPathOutsideRepository")
	})
	t.Run("a temporary folder that cannot be made", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := mergeBase(t, e)
		repo := open(t, e, r.Dir)
		wantRoute(t, repo.Merge(ctx, "topic"), "Repo.Merge"+writeRoute+"Conflict")
		t.Setenv("TMPDIR", filepath.Join(e.Root, "no", "such", "folder"))
		_, err := repo.MergeFile(ctx, "a.txt", MergeFileOptions{})
		wantRoute(t, err, "Repo.MergeFile.MergeFileTempFailed")
	})
}
