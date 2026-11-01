package gitscm

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// Detection, open, delete, migrate and compare of the worktrees (step 24.19).

// linked adds a linked worktree on a new branch with the git program, as a user in a terminal does, and returns its folder.
func (w *wtWorld) linked(name, branch string) string {
	w.t.Helper()
	dir := filepath.Join(w.root, name)
	w.repo.Git("worktree", "add", "-q", "-b", branch, dir)
	return dir
}

func TestWorktreesAreRepositoriesOfTheViewOnlyWhenDetectionIsOn(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		want     int // the repositories of the view
	}{
		{"off, as VS Code has it", nil, 1},
		{"on", map[string]any{"git.detectWorktrees": true}, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := e.Init("app")
			repo.Write("a.txt", "one\n")
			repo.Commit("first commit")
			wt1, wt2 := filepath.Join(e.Work, "app.worktrees", "one"), filepath.Join(e.Work, "app.worktrees", "two")
			repo.Git("worktree", "add", "-q", "-b", "one", wt1)
			repo.Git("worktree", "add", "-q", "-b", "two", wt2)
			r := newRig(t, e, rigOptions{folder: repo.Dir, settings: tc.settings})
			r.settle()
			if got := len(r.x.model.order); got != tc.want {
				t.Fatalf("%d repositories: %q", got, r.roots())
			}
			if tc.want == 1 {
				return
			}
			for _, wt := range []string{wt1, wt2} {
				rp := r.repo(wt)
				src := r.source(wt)
				if rp.kind() != git.KindWorktree || src.ContextValue() != "worktree" || src.ActiveRepositoryName() != "app" || !rp.trusted {
					t.Errorf("%s: kind %v, context %q, name %q, trusted %v", wt, rp.kind(), src.ContextValue(), src.ActiveRepositoryName(), rp.trusted)
				}
			}
			if src := r.source(repo.Dir); src.ContextValue() != "repository" || src.ActiveRepositoryName() != "" {
				t.Errorf("the main repository: context %q, name %q", src.ContextValue(), src.ActiveRepositoryName())
			}
			if len(r.api.OpenDialogs()) != 0 {
				t.Error("a worktree of a trusted repository asked the trust question")
			}
			if r.api.ContextKey("gitOpenRepositoryCount") != 3 {
				t.Errorf("gitOpenRepositoryCount = %v", r.api.ContextKey("gitOpenRepositoryCount"))
			}
		})
	}
}

func TestDetectingWorktreesStopsAtTheLimitAndWarnsOnce(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int // the repositories of the view, the main one included
		warns bool
	}{
		{"room for all", 50, 4, false},
		{"as many as there are", 4, 4, false},
		{"one too few", 3, 3, true},
		{"only the main one fits", 1, 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := e.Init("app")
			repo.Write("a.txt", "one\n")
			repo.Commit("first commit")
			for _, n := range []string{"one", "two", "three"} {
				repo.Git("worktree", "add", "-q", "-b", n, filepath.Join(e.Work, "app.worktrees", n))
			}
			r := newRig(t, e, rigOptions{folder: repo.Dir, settings: map[string]any{"git.detectWorktrees": true, "git.detectWorktreesLimit": tc.limit}})
			r.settle()
			rp := r.repo(repo.Dir)
			r.refresh(rp)
			r.refresh(rp)
			if got := len(r.x.model.order); got != tc.want {
				t.Errorf("%d repositories: %q", got, r.roots())
			}
			warned := notesWith(r, "worktrees which won't be opened automatically")
			if tc.warns {
				if len(warned) != 1 {
					t.Fatalf("%d warnings: %q", len(warned), r.noteTexts())
				}
				if got, want := r.api.Notes()[warned[0]].Message, `The "app" repository has 4 worktrees which won't be opened automatically. You can still open each one individually by opening a folder.`; got != want || r.api.Notes()[warned[0]].Severity != "warn" {
					t.Errorf("warning %q", got)
				}
			} else if len(warned) != 0 {
				t.Errorf("a warning without a reason: %q", r.noteTexts())
			}
			// the worktrees that open are the first of the list: git lists the linked ones by name
			if tc.want == 3 {
				if got := r.roots(); !strings.HasSuffix(got[1], "/one") || !strings.HasSuffix(got[2], "/three") {
					t.Errorf("repositories %q", got)
				}
			}
		})
	}
}

func TestAWorktreeThatIsTheFolderOfTheTabIsARepositoryWhateverTheSettingSays(t *testing.T) {
	w := newWtWorld(t, rigOptions{})
	wt := w.linked("one", "one")
	e := w.env
	r := newRig(t, e, rigOptions{folder: wt})
	r.settle()
	if got := r.roots(); !slices.Equal(got, []string{wt}) {
		t.Fatalf("repositories %q", got)
	}
	rp := r.repo(wt)
	if rp.kind() != git.KindWorktree || !rp.trusted || rp.repo == nil {
		t.Errorf("kind %v trusted %v", rp.kind(), rp.trusted)
	}
	// no second trust question: the repository it belongs to is trusted
	if len(r.api.OpenDialogs()) != 0 {
		t.Error("the worktree of a trusted repository asked the trust question")
	}
	if r.source(wt).ActiveRepositoryName() != "app" {
		t.Errorf("name %q", r.source(wt).ActiveRepositoryName())
	}
}

func TestAWorktreeOfARepositoryThatIsNotTrustedAsksAbout(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("app")
	repo.Write("a.txt", "one\n")
	repo.Commit("first commit")
	wt := filepath.Join(e.Work, "app.worktrees", "one")
	repo.Git("worktree", "add", "-q", "-b", "one", wt)
	if err := os.Remove(e.TrustFile()); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, e, rigOptions{folder: wt})
	r.settle()
	if rp := r.repo(wt); rp.trusted || rp.repo != nil {
		t.Errorf("the worktree of a repository nobody trusted is trusted: %v", rp.trusted)
	}
	if len(r.api.OpenDialogs()) != 1 {
		t.Errorf("%d questions", len(r.api.OpenDialogs()))
	}
}

func TestOpenWorktreeReplacesTheFolderOfTheTabAndTheOtherOpensANewTab(t *testing.T) {
	w := newWtWorld(t, rigOptions{settings: map[string]any{"git.detectWorktrees": true}})
	wt := w.linked("one", "one")
	w.r.refresh(w.rp)
	w.r.settle()
	rp := w.r.repo(wt)
	for _, tc := range []struct {
		cmd    string
		newTab bool
	}{{"git.openWorktree", false}, {"git.openWorktreeInNewWindow", true}} {
		before := len(w.r.api.OpenedFolders())
		if err := w.r.api.Run(tc.cmd, []any{sourceArg(rp)}); err != nil {
			t.Fatal(err)
		}
		got := w.r.api.OpenedFolders()[before:]
		if len(got) != 1 || got[0] != (fakeapi.OpenedFolder{Path: wt, NewTab: tc.newTab}) {
			t.Errorf("%s opened %v", tc.cmd, got)
		}
	}
	// without an argument the repository is picked
	p := w.r.startPick("git.openWorktree", nil)
	if p.Options().Placeholder != "Choose a repository" || len(p.Labels()) != 2 {
		t.Errorf("pick %+v", p.Options())
	}
	p.Choose(pickIndex(t, p, "one"))
	if got := w.r.api.OpenedFolders(); got[len(got)-1] != (fakeapi.OpenedFolder{Path: wt, NewTab: false}) {
		t.Errorf("opened %v", got)
	}
}

func TestDeleteWorktreeFromThePalette(t *testing.T) {
	w := newWtWorld(t, rigOptions{settings: map[string]any{"git.detectWorktrees": true}})
	one := w.linked("one", "one")
	two := w.linked("two", "two")
	w.r.refresh(w.rp)
	w.r.settle()
	p := w.r.startPick("git.deleteWorktree", menuArg(w.rp))
	if p.Options().Placeholder != "Select a worktree to delete" {
		t.Errorf("placeholder %q", p.Options().Placeholder)
	}
	items := p.Options().Items
	if len(items) != 2 || items[0].Label != "$(list-tree) one" || items[1].Label != "$(list-tree) two" {
		t.Fatalf("items %+v", items)
	}
	hash := w.repo.Git("rev-parse", "--short=7", "HEAD")
	if items[0].Description != "one • "+hash+" • second commit" || items[0].Detail != one {
		t.Errorf("item %+v", items[0])
	}
	p.Choose(1)
	d := w.r.theOnlyDialog()
	if d.Spec().Message != `Are you sure you want to delete the worktree "two"?` || d.Spec().Detail != "Worktree: "+two || strings.Join(buttons(d), ",") != "Delete Worktree,Cancel" {
		t.Fatalf("dialog %+v", d.Spec())
	}
	d.Press("Delete Worktree")
	w.r.idle(w.rp)
	if got := w.worktreeList(); !slices.Equal(got, []string{w.repo.Dir + " main", one + " one"}) {
		t.Errorf("worktrees %q", got)
	}
	if _, err := os.Stat(two); !os.IsNotExist(err) {
		t.Errorf("the folder is still there: %v", err)
	}
	if w.r.x.model.byRoot(two) != nil {
		t.Error("the repository of the deleted worktree is still open")
	}
	if w.r.x.model.byRoot(one) == nil {
		t.Error("the repository of the other worktree was closed")
	}
}

func TestDeleteWorktreeCancelledOrWithNoWorktrees(t *testing.T) {
	t.Run("Cancel keeps the worktree", func(t *testing.T) {
		w := newWtWorld(t, rigOptions{})
		one := w.linked("one", "one")
		p := w.r.startPick("git.deleteWorktree", menuArg(w.rp))
		p.Choose(0)
		w.r.theOnlyDialog().Cancel()
		w.r.api.Pump()
		if _, err := os.Stat(one); err != nil {
			t.Error(err)
		}
	})
	t.Run("a repository with none says so", func(t *testing.T) {
		w := newWtWorld(t, rigOptions{})
		p := w.r.startPick("git.deleteWorktree", menuArg(w.rp))
		if labels := p.Labels(); len(labels) != 1 || labels[0] != "$(info) This repository has no worktrees." {
			t.Errorf("labels %q", labels)
		}
		p.Choose(0)
		w.r.api.Pump()
		if len(w.r.api.OpenDialogs()) != 0 {
			t.Error("a question about nothing")
		}
	})
}

func TestDeleteWorktreeWithChangesAsksToForceIt(t *testing.T) {
	tests := []struct {
		name    string
		change  func(dir string)
		answer  string
		removed bool
	}{
		{"a changed file, forced", func(dir string) { writeFile(t, filepath.Join(dir, "a.txt"), "changed\n") }, "Force Delete", true},
		{"an untracked file, forced", func(dir string) { writeFile(t, filepath.Join(dir, "new.txt"), "new\n") }, "Force Delete", true},
		{"a changed file, not forced", func(dir string) { writeFile(t, filepath.Join(dir, "a.txt"), "changed\n") }, "Cancel", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWtWorld(t, rigOptions{})
			one := w.linked("one", "one")
			tc.change(one)
			p := w.r.startPick("git.deleteWorktree", menuArg(w.rp))
			p.Choose(0)
			w.r.theOnlyDialog().Press("Delete Worktree")
			d := w.r.nextDialog(0)
			if d.Spec().Message != "The worktree contains modified or untracked files. Do you want to force delete?" || d.Spec().Detail != "Worktree: "+one ||
				strings.Join(buttons(d), ",") != "Force Delete,Cancel" {
				t.Fatalf("dialog %+v", d.Spec())
			}
			if tc.answer == "Cancel" {
				d.Cancel()
			} else {
				d.Press(tc.answer)
			}
			w.r.idle(w.rp)
			_, err := os.Stat(one)
			if tc.removed != os.IsNotExist(err) {
				t.Errorf("removed = %v, want %v (%v)", os.IsNotExist(err), tc.removed, err)
			}
			if n := len(w.r.api.OpenDialogs()); n != 0 {
				t.Errorf("%d questions are open", n)
			}
		})
	}
}

func TestDeleteWorktreeOfALockedOneFailsWithGitsMessage(t *testing.T) {
	w := newWtWorld(t, rigOptions{})
	one := w.linked("one", "one")
	w.repo.Git("worktree", "lock", "--reason", "in use", one)
	p := w.r.startPick("git.deleteWorktree", menuArg(w.rp))
	p.Choose(0)
	w.r.theOnlyDialog().Press("Delete Worktree")
	w.r.idle(w.rp)
	w.r.api.WaitFor("the failure", func() bool { w.r.api.Pump(); return len(notesWith(w.r, "unlock first")) > 0 })
	if _, err := os.Stat(one); err != nil {
		t.Errorf("the locked worktree is gone: %v", err)
	}
	note := w.r.api.Notes()[notesWith(w.r, "unlock first")[0]]
	if note.Severity != "error" || !strings.HasPrefix(note.Message, "Git: ") {
		t.Errorf("notification %+v", note)
	}
}

func TestDeleteWorktreeFromItsOwnMenu(t *testing.T) {
	w := newWtWorld(t, rigOptions{settings: map[string]any{"git.detectWorktrees": true}})
	one := w.linked("one", "one")
	w.r.refresh(w.rp)
	w.r.settle()
	rp := w.r.repo(one)
	if err := w.r.api.Run("git.deleteWorktree2", []any{sourceArg(rp)}); err != nil {
		t.Fatal(err)
	}
	d := w.r.theOnlyDialog()
	if d.Spec().Message != `Are you sure you want to delete the worktree "one"?` {
		t.Fatalf("dialog %+v", d.Spec())
	}
	d.Press("Delete Worktree")
	w.r.idle(w.rp)
	if _, err := os.Stat(one); !os.IsNotExist(err) {
		t.Errorf("the folder is still there: %v", err)
	}
	if w.r.x.model.byRoot(one) != nil {
		t.Error("the repository of the deleted worktree is still open")
	}
}

func TestTheWorktreeYouAreInCannotBeDeleted(t *testing.T) {
	const refusal = "You cannot delete the worktree you are currently in. Please switch to the main repository first."
	t.Run("from its own menu while the main repository is not open", func(t *testing.T) {
		w := newWtWorld(t, rigOptions{})
		one := w.linked("one", "one")
		r := newRig(t, w.env, rigOptions{folder: one})
		r.settle()
		rp := r.repo(one)
		if err := r.api.Run("git.deleteWorktree2", []any{sourceArg(rp)}); err != nil {
			t.Fatal(err)
		}
		d := r.theOnlyDialog()
		if d.Spec().Message != refusal || d.Spec().Type != extension.DialogError {
			t.Errorf("dialog %+v", d.Spec())
		}
		d.Press("OK")
		r.api.Pump()
		if _, err := os.Stat(one); err != nil {
			t.Error(err)
		}
	})
	t.Run("whatever repository is open: the folder of the tab is protected", func(t *testing.T) {
		w := newWtWorld(t, rigOptions{})
		one := w.linked("one", "one")
		// the window is on the worktree, and the main repository is open as well (git.openRepository)
		r := newRig(t, w.env, rigOptions{folder: one})
		r.settle()
		main := r.x.openRepositoryAt(w.repo.Dir)
		if main == nil {
			t.Fatal("the main repository did not open")
		}
		r.settle()
		if err := r.api.Run("git.deleteWorktree2", []any{sourceArg(r.repo(one))}); err != nil {
			t.Fatal(err)
		}
		d := r.theOnlyDialog()
		if d.Spec().Message != refusal {
			t.Errorf("dialog %+v", d.Spec())
		}
		d.Press("OK")
		// the same from the main repository's picker
		p := r.startPick("git.deleteWorktree", []any{sourceArg(main)})
		p.Choose(0)
		d = r.theOnlyDialog()
		if d.Spec().Message != refusal {
			t.Errorf("dialog %+v", d.Spec())
		}
		d.Press("OK")
		r.api.Pump()
		if _, err := os.Stat(one); err != nil {
			t.Error(err)
		}
	})
}

func TestTheWorktreeCommandsPickTheRepositoryFromTheRightKinds(t *testing.T) {
	w := newWtWorld(t, rigOptions{settings: map[string]any{"git.detectWorktrees": true}})
	one := w.linked("one", "one")
	w.r.refresh(w.rp)
	w.r.settle()
	// only the main repository can make, delete and migrate: it is the only one, so nothing is asked
	p := w.r.startPick("git.createWorktree", nil)
	if p.Options().Placeholder != "Select a branch or tag to create the new worktree from" {
		t.Errorf("pick %q", p.Options().Placeholder)
	}
	p.Close()
	// nothing but a worktree is open: there is no repository matching
	r := newRig(t, w.env, rigOptions{folder: one})
	r.settle()
	if err := r.api.Run("git.createWorktree", nil); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	if len(r.api.Picks()) != 0 || !slices.Contains(r.noteTexts(), "info: There are no available repositories matching the filter.") {
		t.Errorf("picks %d, notes %q", len(r.api.Picks()), r.noteTexts())
	}
}
