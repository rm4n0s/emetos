//go:build emetos_display

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The commit as a user makes it (step 24.10, SPEC §10.30 "Staging, discarding and committing", §15.1): a real repository in a temporary
// folder, the Source Control view of the real window, a message typed with the virtual keyboard, Ctrl+Enter, and then the button and the
// question of smart commit with the pointer. The repository, its configuration and its author are those of the test; the editor runs
// with a HOME of its own.

func TestRealInputGitCommit(t *testing.T) {
	e := gittest.NewEnv(t)
	var repo *gittest.Repo
	a := display.Start(t, display.Options{
		Args: []string{"."},
		Prepare: func(dir string) {
			repo = e.InitAt(dir)
			repo.Git("config", "user.name", gittest.AuthorName)
			repo.Git("config", "user.email", gittest.AuthorEmail)
			repo.Write("loader.go", "package main\n\nfunc load() int { return 1 }\n")
			repo.Write("notes.md", "# Notes\n")
			repo.Commit("Add the loader")
			repo.Write("loader.go", "package main\n\nfunc load() int { return 2 }\n")
			repo.Git("add", "loader.go")
			repo.Write("notes.md", "# Notes\n\n- retries\n")
		},
		// the repository is the test's own: trusted for the session (SPEC §15.2); no system configuration
		Env: []string{"EMETOS_DEV_GIT_TRUST=" + os.TempDir(), "GIT_CONFIG_NOSYSTEM=1"},
	})
	a.Settle()
	subject := func() string { return repo.Git("log", "-1", "--format=%s") }
	count := func() string { return repo.Git("rev-list", "--count", "HEAD") }

	a.Press("ctrl+shift+g")
	a.WaitText("Staged Changes")
	a.WaitText("Changes")
	var box app.DevInput
	a.Eventually("the commit box", func() (bool, string) {
		in, ok := commitBox(a)
		box = in
		return ok, a.Dump()
	})

	// the message is typed and Ctrl+Enter makes the commit: the staged file only
	a.ClickInput(box)
	a.Eventually("the click to give the box the keyboard", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.X < 350, a.FocusedText()
	})
	const message = "Fix the cache key of the loader"
	a.Type(message)
	a.WaitFocusedText(message)
	a.Press("ctrl+Enter")
	a.Eventually("the commit", func() (bool, string) { return count() == "2", a.Dump() })
	if got := subject(); got != message {
		t.Errorf("the subject is %q", got)
	}
	if got := repo.Git("show", "--format=", "--name-only", "HEAD"); got != "loader.go" {
		t.Errorf("the commit has %q: only the staged file belongs in it", got)
	}
	if got := repo.Git("log", "-1", "--format=%an <%ae>"); got != "Ada Example <ada@example.com>" {
		t.Errorf("the author is %q", got)
	}
	a.WaitNoText("Staged Changes")
	a.Eventually("the box to be empty", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Text == "", a.FocusedText()
	})

	// nothing is staged and notes.md is changed: the button commits it after the question of smart commit
	a.Type("Note the retries")
	a.WaitFocusedText("Note the retries")
	a.ClickText("Commit")
	a.WaitText("There are no staged changes to commit.")
	a.ClickText("Yes")
	a.Eventually("the second commit", func() (bool, string) { return count() == "3", a.Dump() })
	if got := subject(); got != "Note the retries" {
		t.Errorf("the subject is %q", got)
	}
	if got := strings.TrimSpace(repo.Git("status", "--porcelain")); got != "" {
		t.Errorf("the work tree is not clean: %q", got)
	}
	// nothing is left to commit and the branch has no remote: the button offers to publish it
	a.WaitText("Publish Branch")
	a.WaitNoText("There are no staged changes to commit.")
	if got := repo.Git("rev-list", "--count", "--merges", "HEAD"); got != "0" {
		t.Errorf("%s merge commits", got)
	}
}
