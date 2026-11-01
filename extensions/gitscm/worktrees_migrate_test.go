package gitscm

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Migrate Worktree Changes, Compare with Workspace and the merge editor of a migration (step 24.19).

// migrateWorld is the world with the worktree "one" on the branch one, with a change of a tracked file, a staged change and an untracked file.
func migrateWorld(t *testing.T, o rigOptions) (*wtWorld, string) {
	t.Helper()
	w := newWtWorld(t, o)
	one := w.linked("one", "one")
	writeFile(t, filepath.Join(one, "a.txt"), "one\ntwo\nthree from the worktree\n")
	writeFile(t, filepath.Join(one, "src", "b.txt"), "b\nstaged in the worktree\n")
	w.repo.Git("-C", one, "add", "src/b.txt")
	writeFile(t, filepath.Join(one, "new.txt"), "new in the worktree\n")
	return w, one
}

// gitLines are the lines of the Git channel.
func (r *rig) gitLines() []string { return r.api.Channel(ChannelName).Lines() }

func TestMigrateWorktreeChangesMovesTheChangesToTheRepository(t *testing.T) {
	w, one := migrateWorld(t, rigOptions{})
	if err := w.r.api.Run("git.migrateWorktreeChanges", []any{sourceArg(w.rp)}); err != nil {
		t.Fatal(err)
	}
	// the only worktree is taken without a question about which; the repository of it is opened in the view for the changes
	d := w.r.theOnlyDialog()
	if d.Spec().Message != "Proceed with migrating changes to the current repository?" ||
		d.Spec().Detail != "This will apply the worktree's changes to this repository and discard changes in the worktree.\nThis is IRREVERSIBLE!" ||
		strings.Join(buttons(d), ",") != "Proceed,Cancel" || d.Spec().Type != extension.DialogWarning {
		t.Fatalf("dialog %+v", d.Spec())
	}
	if len(w.r.api.Picks()) != 0 {
		t.Error("the only worktree was picked from a list")
	}
	d.Press("Proceed")
	w.r.idle(w.rp)
	w.r.idle(w.r.repo(one))
	if got, want := porcelain(w.repo), []string{" M a.txt", " M src/b.txt", "?? new.txt"}; !slices.Equal(got, want) {
		t.Errorf("the repository is %q, want %q", got, want)
	}
	if got := w.repo.Read("a.txt"); got != "one\ntwo\nthree from the worktree\n" {
		t.Errorf("a.txt %q", got)
	}
	if got := w.repo.Git("-C", one, "status", "--porcelain"); got != "" {
		t.Errorf("the worktree still has changes: %q", got)
	}
	if got := strings.TrimSpace(w.repo.Git("stash", "list")); got != "" {
		t.Errorf("a stash is left: %q", got)
	}
	// the stash is named as VS Code names it
	if !slices.Contains(w.r.gitLines(), "> git stash push -u -m migration:one-main") && !slices.ContainsFunc(w.r.gitLines(), func(l string) bool {
		return strings.Contains(l, "stash push -u -m migration:one-main")
	}) {
		t.Errorf("the Git channel: %q", w.r.gitLines())
	}
	if w.rp.worktreeMigrating {
		t.Error("the migration is in conflict")
	}
}

func TestMigrateWorktreeChangesWithSeveralWorktreesAsksWhich(t *testing.T) {
	w, _ := migrateWorld(t, rigOptions{})
	two := w.linked("two", "two")
	writeFile(t, filepath.Join(two, "from-two.txt"), "two\n")
	p := w.r.startPick("git.migrateWorktreeChanges", menuArg(w.rp))
	if p.Options().Placeholder != "Select a worktree to migrate changes from" {
		t.Errorf("placeholder %q", p.Options().Placeholder)
	}
	if got := p.Labels(); !slices.Equal(got, []string{"$(list-tree) one", "$(list-tree) two"}) {
		t.Fatalf("labels %q", got)
	}
	p.Choose(1)
	w.r.theOnlyDialog().Press("Proceed")
	w.r.idle(w.rp)
	w.r.idle(w.r.repo(two))
	if got := porcelain(w.repo); !slices.Equal(got, []string{"?? from-two.txt"}) {
		t.Errorf("the repository is %q", got)
	}
}

func TestMigrateWorktreeChangesRefusals(t *testing.T) {
	t.Run("no worktrees", func(t *testing.T) {
		w := newWtWorld(t, rigOptions{})
		p := w.r.startPick("git.migrateWorktreeChanges", menuArg(w.rp))
		if got := p.Labels(); !slices.Equal(got, []string{"$(info) This repository has no worktrees."}) {
			t.Errorf("labels %q", got)
		}
	})
	t.Run("no changes in the worktree", func(t *testing.T) {
		w := newWtWorld(t, rigOptions{})
		w.linked("one", "one")
		if err := w.r.api.Run("git.migrateWorktreeChanges", []any{sourceArg(w.rp)}); err != nil {
			t.Fatal(err)
		}
		w.r.api.WaitFor("the answer", func() bool { w.r.api.Pump(); return len(notesWith(w.r, "no changes in the selected worktree")) > 0 })
		if n := w.r.api.Notes()[notesWith(w.r, "no changes in the selected worktree")[0]]; n.Severity != "info" || n.Message != "There are no changes in the selected worktree to migrate." {
			t.Errorf("notification %+v", n)
		}
		if len(w.r.api.OpenDialogs()) != 0 {
			t.Error("a question about nothing")
		}
	})
	t.Run("Cancel moves nothing", func(t *testing.T) {
		w, one := migrateWorld(t, rigOptions{})
		if err := w.r.api.Run("git.migrateWorktreeChanges", []any{sourceArg(w.rp)}); err != nil {
			t.Fatal(err)
		}
		w.r.theOnlyDialog().Cancel()
		w.r.api.Pump()
		if got := porcelain(w.repo); len(got) != 0 {
			t.Errorf("the repository changed: %q", got)
		}
		if got := w.repo.Git("-C", one, "status", "--porcelain"); got == "" {
			t.Error("the worktree lost its changes")
		}
	})
	t.Run("files both have changed refuse it, with the list", func(t *testing.T) {
		for _, n := range []int{1, 7} {
			w, one := migrateWorld(t, rigOptions{})
			names := []string{"a.txt", "new.txt", "x1.txt", "x2.txt", "x3.txt", "x4.txt", "x5.txt"}[:n]
			for _, name := range names {
				writeFile(t, filepath.Join(one, name), "worktree "+name+"\n")
				writeFile(t, filepath.Join(w.repo.Dir, name), "repository "+name+"\n")
			}
			if err := w.r.api.Run("git.migrateWorktreeChanges", []any{sourceArg(w.rp)}); err != nil {
				t.Fatal(err)
			}
			d := w.r.theOnlyDialog()
			list := strings.Join(names[:min(n, 5)], "\n ")
			if n > 5 {
				list += "\n and 2 more files..."
			}
			want := "Your local changes to the following files would be overwritten by merge:\n " + list +
				"\n\nPlease stage, commit, or stash your changes in the repository before migrating changes."
			if d.Spec().Message != want || d.Spec().Type != extension.DialogError {
				t.Errorf("%d files: dialog %q", n, d.Spec().Message)
			}
			d.Press("OK")
			w.r.api.Pump()
			if got := strings.TrimSpace(w.repo.Git("stash", "list")); got != "" {
				t.Errorf("a stash was made: %q", got)
			}
			if got := w.repo.Read("a.txt"); got != "repository a.txt\n" {
				t.Errorf("a.txt %q", got)
			}
		}
	})
	t.Run("one more file past the fifth is not a plural", func(t *testing.T) {
		w, one := migrateWorld(t, rigOptions{})
		names := []string{"a.txt", "new.txt", "x1.txt", "x2.txt", "x3.txt", "x4.txt"}
		for _, name := range names {
			writeFile(t, filepath.Join(one, name), "worktree\n")
			writeFile(t, filepath.Join(w.repo.Dir, name), "repository\n")
		}
		if err := w.r.api.Run("git.migrateWorktreeChanges", []any{sourceArg(w.rp)}); err != nil {
			t.Fatal(err)
		}
		d := w.r.theOnlyDialog()
		if !strings.Contains(d.Spec().Message, "\n and 1 more file...\n") {
			t.Errorf("dialog %q", d.Spec().Message)
		}
	})
}

func TestMigrateWorktreeChangesWithConflictsLeavesThemInTheMergeGroup(t *testing.T) {
	w := newWtWorld(t, rigOptions{settings: map[string]any{"git.mergeEditor": true}})
	one := w.linked("one", "one")
	writeFile(t, filepath.Join(one, "a.txt"), "one\ntwo changed by the worktree\n")
	// the repository commits a different change of the same line after the worktree was made
	w.repo.Write("a.txt", "one\ntwo changed by the repository\n")
	w.repo.Commit("change a.txt here")
	w.r.refresh(w.rp)
	if err := w.r.api.Run("git.migrateWorktreeChanges", []any{sourceArg(w.rp)}); err != nil {
		t.Fatal(err)
	}
	w.r.theOnlyDialog().Press("Proceed")
	w.r.api.WaitFor("the conflicts", func() bool {
		w.r.api.Pump()
		return len(notesWith(w.r, "merge conflicts from migrating changes")) > 0 && !w.rp.refreshing && w.rp.own == 0 && len(w.rp.res.merge) == 1
	})
	n := w.r.api.Notes()[notesWith(w.r, "merge conflicts from migrating changes")[0]]
	if n.Severity != "warn" || n.Message != "There are merge conflicts from migrating changes. Please resolve them before committing." {
		t.Errorf("notification %+v", n)
	}
	if got := w.r.paths(w.repo.Dir, groupMerge); !slices.Equal(got, []string{"a.txt"}) {
		t.Errorf("merge group %q", got)
	}
	if !w.rp.worktreeMigrating {
		t.Error("the migration is not marked")
	}
	// the worktree got its changes back, and no stash is left
	if got := w.repo.Read("../app.worktrees/one/a.txt"); got != "one\ntwo changed by the worktree\n" {
		t.Errorf("the worktree's a.txt %q", got)
	}
	if got := strings.TrimSpace(w.repo.Git("stash", "list")); got != "" {
		t.Errorf("a stash is left: %q", got)
	}
	// a click on the conflicted file opens the merge editor with the sides "Workspace" and "Worktree"
	docs := len(w.r.api.OpenedDocuments())
	if err := w.r.api.Run("git.openChange", []any{resourceArg(w.rp, groupMerge, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	got := w.r.api.OpenedDocuments()[docs:]
	if len(got) != 1 || !got[0].MergeEditor || got[0].MergeLabels == nil || *got[0].MergeLabels != (extension.MergeLabels{Current: "Workspace", Incoming: "Worktree"}) {
		t.Errorf("opened %+v", got)
	}
	// the conflicts resolved, the next status ends the migration
	w.repo.Write("a.txt", "one\ntwo resolved\n")
	w.repo.Git("add", "a.txt")
	w.r.refresh(w.rp)
	if w.rp.worktreeMigrating {
		t.Error("the migration is still marked after the conflicts are gone")
	}
	docs = len(w.r.api.OpenedDocuments())
	w.repo.Git("reset", "-q", "--hard")
	w.r.refresh(w.rp)
	if len(w.r.api.OpenedDocuments()) != docs {
		t.Error("something opened")
	}
}

func TestAConflictOutsideAMigrationOpensTheMergeEditorWithTheNamesOfTheMarkers(t *testing.T) {
	e := fixtureConflictWorld(t)
	rp := e.rp
	docs := len(e.r.api.OpenedDocuments())
	if err := e.r.api.Run("git.openChange", []any{resourceArg(rp, groupMerge, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	got := e.r.api.OpenedDocuments()[docs:]
	if len(got) != 1 || !got[0].MergeEditor || got[0].MergeLabels != nil {
		t.Errorf("opened %+v", got)
	}
}

func TestOpenWorktreeMergeEditorOpensTheFileWithTheNamesOfTheSides(t *testing.T) {
	w := newWtWorld(t, rigOptions{})
	uri := fileURI(filepath.Join(w.repo.Dir, "a.txt"))
	for _, args := range []any{map[string]any{"uri": uri}, uri} {
		docs := len(w.r.api.OpenedDocuments())
		if err := w.r.api.Run("git.openWorktreeMergeEditor", args); err != nil {
			t.Fatal(err)
		}
		got := w.r.api.OpenedDocuments()[docs:]
		if len(got) != 1 || got[0].URI != uri || !got[0].MergeEditor || got[0].MergeLabels == nil ||
			*got[0].MergeLabels != (extension.MergeLabels{Current: "Workspace", Incoming: "Worktree"}) {
			t.Errorf("%v opened %+v", args, got)
		}
	}
	docs := len(w.r.api.OpenedDocuments())
	if err := w.r.api.Run("git.openWorktreeMergeEditor", nil); err != nil || len(w.r.api.OpenedDocuments()) != docs {
		t.Errorf("no argument opened something: %v", err)
	}
}

// conflictWorld is a repository in a merge with a conflict in a.txt.
type conflictWorld struct {
	r  *rig
	rp *repository
}

func fixtureConflictWorld(t *testing.T) conflictWorld {
	t.Helper()
	w := newWtWorld(t, rigOptions{settings: map[string]any{"git.mergeEditor": true}})
	w.repo.Git("checkout", "-q", "topic")
	w.repo.Write("a.txt", "one\ntwo on topic\n")
	w.repo.Commit("topic change")
	w.repo.Git("checkout", "-q", "main")
	w.repo.Write("a.txt", "one\ntwo on main\n")
	w.repo.Commit("main change")
	_, _, _ = w.repo.GitErr("merge", "topic")
	w.r.refresh(w.rp)
	if len(w.rp.res.merge) != 1 {
		t.Fatalf("merge group %v", w.rp.res.merge)
	}
	return conflictWorld{r: w.r, rp: w.rp}
}

func TestCompareWithWorkspace(t *testing.T) {
	w := newWtWorld(t, rigOptions{settings: map[string]any{"git.detectWorktrees": true}})
	one := w.linked("one", "one")
	writeFile(t, filepath.Join(one, "a.txt"), "one\ntwo\nchanged in the worktree\n")
	writeFile(t, filepath.Join(one, "only-here.txt"), "not in the main work tree\n")
	writeFile(t, filepath.Join(one, "src", "b.txt"), "b\nstaged\n")
	w.repo.Git("-C", one, "add", "src/b.txt")
	w.r.refresh(w.rp)
	w.r.settle()
	wt := w.r.repo(one)
	w.r.refresh(wt)
	main := func(rel string) string { return fileURI(filepath.Join(w.repo.Dir, rel)) }
	vers := func(ref, rel string) string { return extension.ContentURI("git", wt.id+"/"+ref+"/"+rel) }
	tests := []struct {
		name, group, rel string
		diff             *extension.DiffSpec
		doc              *extension.OpenSpec
	}{
		{"a changed file against the same file of the main work tree", groupWorking, "a.txt",
			&extension.DiffSpec{Left: main("a.txt"), Right: fileURI(filepath.Join(one, "a.txt")), Title: "a.txt (Working Tree)", Preview: true}, nil},
		{"a staged file against the same file of the main work tree", groupIndex, "src/b.txt",
			&extension.DiffSpec{Left: main("src/b.txt"), Right: vers("", "src/b.txt"), Title: "b.txt (Index)", Preview: true}, nil},
		{"a file the main work tree does not have is opened as it is", groupWorking, "only-here.txt",
			nil, &extension.OpenSpec{URI: fileURI(filepath.Join(one, "only-here.txt")), Title: "only-here.txt (Untracked)", Preview: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diffs, docs := len(w.r.api.OpenedDiffs()), len(w.r.api.OpenedDocuments())
			if err := w.r.api.Run("git.compareWithWorkspace", []any{resourceArg(wt, tc.group, tc.rel)}); err != nil {
				t.Fatal(err)
			}
			gotD, gotO := w.r.api.OpenedDiffs()[diffs:], w.r.api.OpenedDocuments()[docs:]
			switch {
			case tc.diff != nil && (len(gotD) != 1 || len(gotO) != 0 || gotD[0] != *tc.diff):
				t.Errorf("diffs %+v docs %+v, want %+v", gotD, gotO, *tc.diff)
			case tc.doc != nil && (len(gotO) != 1 || len(gotD) != 0 || !sameOpen(gotO[0], *tc.doc)):
				t.Errorf("diffs %+v docs %+v, want %+v", gotD, gotO, *tc.doc)
			}
		})
	}
	t.Run("a repository that is not a worktree opens the change", func(t *testing.T) {
		w.repo.Write("a.txt", "one\ntwo\nchanged in the main work tree\n")
		w.r.refresh(w.rp)
		diffs := len(w.r.api.OpenedDiffs())
		if err := w.r.api.Run("git.compareWithWorkspace", []any{resourceArg(w.rp, groupWorking, "a.txt")}); err != nil {
			t.Fatal(err)
		}
		if got := w.r.api.OpenedDiffs()[diffs:]; len(got) != 1 || got[0].Title != "a.txt (Working Tree)" || got[0].Left == main("a.txt") {
			t.Errorf("diffs %+v", got)
		}
	})
	t.Run("no resource opens nothing", func(t *testing.T) {
		diffs, docs := len(w.r.api.OpenedDiffs()), len(w.r.api.OpenedDocuments())
		if err := w.r.api.Run("git.compareWithWorkspace", nil); err != nil {
			t.Fatal(err)
		}
		if len(w.r.api.OpenedDiffs()) != diffs || len(w.r.api.OpenedDocuments()) != docs {
			t.Error("something opened")
		}
	})
}

func TestCheckingOutABranchAnotherWorktreeHasOffersToOpenThatWorktree(t *testing.T) {
	w := newWtWorld(t, rigOptions{})
	one := w.linked("one", "one")
	p := w.r.startPick("git.checkout", menuArg(w.rp))
	p.Choose(pickIndex(t, p, "$(git-branch) one"))
	d := w.r.theOnlyDialog()
	if want := `Branch "one" is already checked out in the worktree at "` + one + `".`; d.Spec().Message != want ||
		strings.Join(buttons(d), ",") != "Open Worktree in Current Window,Open Worktree in New Window,Cancel" {
		t.Fatalf("dialog %+v", d.Spec())
	}
	d.Press("Open Worktree in New Window")
	w.r.idle(w.rp)
	if got := w.r.api.OpenedFolders(); len(got) != 1 || got[0].Path != one || !got[0].NewTab {
		t.Errorf("opened %v", got)
	}
	if got := strings.TrimSpace(w.repo.Git("branch", "--show-current")); got != "main" {
		t.Errorf("the branch is %q", got)
	}
	if len(notesWith(w.r, "Git:")) != 0 {
		t.Errorf("the failure was also reported: %q", w.r.noteTexts())
	}
}

func TestCheckingOutTheBranchOfTheMainWorkTreeFromAWorktree(t *testing.T) {
	w := newWtWorld(t, rigOptions{settings: map[string]any{"git.detectWorktrees": true}})
	one := w.linked("one", "one")
	w.r.refresh(w.rp)
	w.r.settle()
	wt := w.r.repo(one)
	p := w.r.startPick("git.checkout", menuArg(wt))
	p.Choose(pickIndex(t, p, "$(git-branch) main"))
	d := w.r.theOnlyDialog()
	if d.Spec().Message != `Branch "main" is already checked out in the current window.` || d.Spec().Type != extension.DialogError {
		t.Errorf("dialog %+v", d.Spec())
	}
	d.Press("OK")
	// another worktree's branch is offered to open
	two := w.linked("two", "two")
	w.r.refresh(w.rp)
	p = w.r.startPick("git.checkout", menuArg(wt))
	p.Choose(pickIndex(t, p, "$(git-branch) two"))
	d = w.r.theOnlyDialog()
	if want := `Branch "two" is already checked out in the worktree at "` + two + `".`; d.Spec().Message != want {
		t.Errorf("dialog %+v", d.Spec())
	}
	d.Cancel()
}

func TestWorktreeCommandsWhenTheFolderIsGone(t *testing.T) {
	w := newWtWorld(t, rigOptions{settings: map[string]any{"git.detectWorktrees": true}})
	one := w.linked("one", "one")
	w.r.refresh(w.rp)
	w.r.settle()
	if err := os.RemoveAll(one); err != nil {
		t.Fatal(err)
	}
	// the record is prunable, and the repository of it cannot be found any more: nothing opens and nothing is logged as a failure
	list, err := w.rp.repo.Worktrees(w.rp.ctx)
	if err != nil || len(list) != 2 || !list[1].Prunable {
		t.Fatalf("worktrees %+v, %v", list, err)
	}
	if _, ok := git.RepositoryAt(one); ok {
		t.Error("a repository was found in a folder that is gone")
	}
}
