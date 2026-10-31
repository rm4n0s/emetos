package gitscm

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// smokeRig is the setup of the smoke test of VS Code: a repository with app.js and index.pug committed.
func smokeRig(t *testing.T) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("app.js", "hello")
	repo.Write("index.pug", "hello")
	repo.Commit("initial commit")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	return r, repo, r.repo(repo.Dir)
}

// Ported from VS Code: extensions/git/src/test/smoke.test.ts ('reflects working tree changes'): the part that reads the status. The
// edits go through the file system here (the editor does the save in the VS Code test).
func TestSmokeReflectsWorkingTreeChanges(t *testing.T) {
	r, repo, rep := smokeRig(t)
	repo.Write("app.js", "hello world")
	r.refresh(rep)
	if got, want := r.letters(repo.Dir, groupWorking), []string{"app.js M"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("working = %v, want %v", got, want)
	}
	repo.Write("newfile.txt", "hey there")
	r.refresh(rep)
	if got, want := r.letters(repo.Dir, groupWorking), []string{"app.js M", "newfile.txt U"}; !reflect.DeepEqual(got, want) {
		t.Errorf("working = %v, want %v", got, want)
	}
}

// Ported from VS Code: extensions/git/src/test/smoke.test.ts ('stages correctly', 'stages, commits changes and verifies outgoing
// change'): the part that reads the status after the index changed. Staging and committing through the extension are steps 24.7 and
// 24.8, so git itself does them here.
func TestSmokeTheStatusFollowsTheIndexAndTheCommits(t *testing.T) {
	r, repo, rep := smokeRig(t)
	repo.Write("app.js", "hello world")
	repo.Write("newfile.txt", "hey there")
	repo.Git("add", "app.js")
	r.refresh(rep)
	if got, want := r.letters(repo.Dir, groupIndex), []string{"app.js M"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("index = %v, want %v", got, want)
	}
	if got, want := r.letters(repo.Dir, groupWorking), []string{"newfile.txt U"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("working = %v, want %v", got, want)
	}
	repo.Git("reset", "-q", "--", "app.js")
	r.refresh(rep)
	if got := r.letters(repo.Dir, groupIndex); len(got) != 0 {
		t.Fatalf("index = %v after the revert", got)
	}
	if got, want := r.letters(repo.Dir, groupWorking), []string{"app.js M", "newfile.txt U"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("working = %v, want %v", got, want)
	}
	repo.Git("add", "app.js")
	repo.Git("commit", "-q", "-m", "second commit")
	r.refresh(rep)
	if got, want := r.letters(repo.Dir, groupWorking), []string{"newfile.txt U"}; !reflect.DeepEqual(got, want) || len(r.letters(repo.Dir, groupIndex)) != 0 {
		t.Fatalf("working = %v", got)
	}
	repo.Write("index.pug", "hello pug")
	repo.Git("commit", "-q", "-a", "-m", "third commit")
	r.refresh(rep)
	if len(r.letters(repo.Dir, groupWorking)) != 1 || len(r.letters(repo.Dir, groupIndex)) != 0 {
		t.Errorf("working = %v (the untracked file is not committed by -a)", r.letters(repo.Dir, groupWorking))
	}
}
