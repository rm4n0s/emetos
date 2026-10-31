package app

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// A discard of the Git extension (step 24.8) puts the file of the index back on disk with git checkout; the window follows it as it
// follows any change from outside (SPEC §6.3 "External changes"): a clean editor reloads, a dirty one asks. The file operation here is the
// real git of the harness, the event the one the watcher gives.

func TestAGitDiscardIsAnExternalChangeTheOpenEditorFollows(t *testing.T) {
	const committed = "committed\nline two\n"
	setup := func(t *testing.T) (*extApp, *gittest.Repo) {
		a := newExtApp(t, map[string]string{"a.txt": committed}, "a.txt")
		a.settle("a.txt")
		repo := gittest.NewEnv(t).InitAt(filepath.Dir(a.files["a.txt"]))
		repo.Commit("Add the file", "a.txt")
		return a, repo
	}
	t.Run("a clean editor reloads", func(t *testing.T) {
		a, repo := setup(t)
		doc := a.doc("a.txt")
		a.put("a.txt", "edited in another program\n", 100)
		a.deliver(changed(a.files["a.txt"]))
		if doc.Text() != "edited in another program\n" || doc.Dirty() {
			t.Fatalf("the edit was not followed: %q", doc.Text())
		}
		repo.Git("checkout", "-q", "--", "a.txt") // what the discard does
		a.deliver(changed(a.files["a.txt"]))
		if doc.Text() != committed || doc.Dirty() || doc.Conflicted() {
			t.Errorf("text %q, dirty %v, conflicted %v", doc.Text(), doc.Dirty(), doc.Conflicted())
		}
		if n := len(a.notes()); n != 0 {
			t.Errorf("notifications: %v", a.notes())
		}
		a.noErrors()
	})
	t.Run("a dirty editor asks", func(t *testing.T) {
		a, repo := setup(t)
		a.typeText("mine ")
		a.put("a.txt", "edited in another program\n", 100)
		repo.Git("checkout", "-q", "--", "a.txt")
		a.deliver(changed(a.files["a.txt"]))
		doc := a.doc("a.txt")
		if doc.Text() != "mine "+committed || !doc.Dirty() || !doc.Conflicted() {
			t.Fatalf("text %q, dirty %v, conflicted %v", doc.Text(), doc.Dirty(), doc.Conflicted())
		}
		warnings := a.warnings()
		if len(warnings) != 1 {
			t.Fatalf("notifications: %v", a.notes())
		}
		if got := actionLabels(warnings[0]); !slices.Equal(got, []string{"Compare", "Overwrite", "Revert"}) {
			t.Errorf("buttons %v", got)
		}
		a.noErrors()
	})
}
