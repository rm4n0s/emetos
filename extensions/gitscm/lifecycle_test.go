package gitscm

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

func notesWith(r *rig, part string) []int {
	var out []int
	for i, n := range r.api.Notes() {
		if strings.Contains(n.Message, part) {
			out = append(out, i)
		}
	}
	return out
}

func TestGitThatIsMissingOrTooOld(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("repo")
		r := newRig(t, e, rigOptions{folder: repo.Dir, noGit: true})
		r.settle()
		if r.api.ContextKey("git.missing") != true || r.api.ContextKey("git.state") != "initialized" {
			t.Errorf("git.missing = %v, git.state = %v", r.api.ContextKey("git.missing"), r.api.ContextKey("git.state"))
		}
		if len(r.x.model.order) != 0 || len(r.api.SourceControls()) != 0 {
			t.Error("a repository was opened with no git")
		}
		n := notesWith(r, "Git is not installed")
		if len(n) != 1 {
			t.Fatalf("%d notifications", len(n))
		}
		if got := r.api.Notes()[n[0]].Actions; len(got) != 2 || got[0] != "Don't Show Again" || got[1] != "Show Git Output" {
			t.Errorf("actions = %v", got)
		}
		r.api.RunNoteAction(n[0], "Don't Show Again")
		if ups := r.api.SettingUpdates(); len(ups) != 1 || ups[0].Key != "git.ignoreMissingGitWarning" || ups[0].Value != true {
			t.Errorf("updates = %+v", ups)
		}
	})
	t.Run("missing and the warning ignored", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("repo")
		r := newRig(t, e, rigOptions{folder: repo.Dir, noGit: true, settings: map[string]any{"git.ignoreMissingGitWarning": true}})
		r.settle()
		if len(r.api.Notes()) != 0 || r.api.ContextKey("git.missing") != true {
			t.Errorf("notes = %+v", r.api.Notes())
		}
	})
	t.Run("too old", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("repo")
		path, env := gittest.FakeGit(t, "version 2.29.0")
		r := newRig(t, e, rigOptions{folder: repo.Dir, gitPath: path, extraEnv: []string{env}})
		r.settle()
		if r.api.ContextKey("git.missing") != true || len(r.x.model.order) != 0 {
			t.Errorf("git.missing = %v, repositories = %d", r.api.ContextKey("git.missing"), len(r.x.model.order))
		}
		if len(notesWith(r, "older than 2.30")) != 1 {
			t.Errorf("notes = %+v", r.api.Notes())
		}
	})
	t.Run("a version of 2.35 sets the context key", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("repo")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		if r.api.ContextKey("gitVersion235") != true || r.api.ContextKey("git.missing") != false {
			t.Errorf("gitVersion235 = %v", r.api.ContextKey("gitVersion235"))
		}
	})
}

func TestGitDisabledAndEnabled(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("a.txt", "a\n")
	r := newRig(t, e, rigOptions{folder: repo.Dir, settings: map[string]any{"git.enabled": false}})
	r.api.Pump()
	if len(r.x.model.order) != 0 || r.x.started {
		t.Fatal("a disabled extension scanned")
	}
	r.api.SetSetting("git.enabled", true)
	r.api.WaitFor("the repository", func() bool { r.api.Pump(); return len(r.api.SourceControls()) == 1 })
	r.settle()
	if got := r.paths(repo.Dir, groupWorking); len(got) != 0 {
		t.Logf("working = %v", got)
	}
	r.api.SetSetting("git.enabled", false)
	r.api.Pump()
	if len(r.api.SourceControls()) != 0 || len(r.api.Watches()) != 0 {
		t.Error("turning Git off left the repository and its watch")
	}
}

func TestAnUnsafeRepositoryIsListedAndNeverMadeSafeByItself(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("a.txt", "a\n")
	r := newRig(t, e, rigOptions{folder: repo.Dir, extraEnv: []string{"GIT_TEST_ASSUME_DIFFERENT_OWNER=1"}})
	r.api.WaitFor("the unsafe repository", func() bool { r.api.Pump(); return len(r.x.model.unsafe) == 1 })
	r.settle()
	if len(r.api.SourceControls()) != 0 || len(r.x.model.order) != 0 {
		t.Errorf("an unsafe repository is open: %d", len(r.x.model.order))
	}
	if r.api.ContextKey("git.unsafeRepositoryCount") != 1 {
		t.Errorf("git.unsafeRepositoryCount = %v", r.api.ContextKey("git.unsafeRepositoryCount"))
	}
	if u := r.x.model.unsafe[0]; u.root != repo.Dir || u.safeDir == "" {
		t.Errorf("unsafe = %+v", u)
	}
	// Manage Unsafe Repositories: the pick, then the dialog that names the folder; Cancel changes nothing
	if err := r.api.Run("git.manageUnsafeRepositories", nil); err != nil {
		t.Fatal(err)
	}
	picks := r.api.Picks()
	if len(picks) != 1 || len(picks[0].Labels()) != 1 {
		t.Fatalf("picks = %+v", picks)
	}
	picks[0].Choose(0)
	r.api.Pump()
	dialogs := r.api.OpenDialogs()
	if len(dialogs) != 1 || !strings.Contains(dialogs[0].Spec().Message, repo.Dir) || !strings.Contains(dialogs[0].Spec().Detail, "git config --global --add safe.directory") {
		t.Fatalf("dialogs = %+v", dialogs)
	}
	dialogs[0].Cancel()
	r.api.Pump()
	if len(r.x.model.unsafe) != 1 {
		t.Error("Cancel changed the list")
	}
	// the user's global configuration was not touched: the harness' one is empty of safe.directory
	if out, _, _ := repo.GitErr("config", "--global", "--get-all", "safe.directory"); out != "" {
		t.Errorf("safe.directory = %q", out)
	}
}

func TestClosingAndReopeningARepository(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("a.txt", "a\n")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	r.x.model.close(r.repo(repo.Dir))
	if len(r.api.SourceControls()) != 0 || r.api.ContextKey("git.closedRepositoryCount") != 1 || r.api.ContextKey("gitOpenRepositoryCount") != 0 {
		t.Fatal("the repository was not closed")
	}
	r.x.model.scan()
	r.settle()
	if len(r.x.model.order) != 0 {
		t.Error("a scan brought the closed repository back")
	}
	if err := r.api.Run("git.reopenClosedRepositories", nil); err != nil {
		t.Fatal(err)
	}
	picks := r.api.Picks()
	if len(picks) != 1 {
		t.Fatalf("picks = %d", len(picks))
	}
	picks[0].Choose(0)
	r.settle()
	if len(r.api.SourceControls()) != 1 || r.api.ContextKey("git.closedRepositoryCount") != 0 {
		t.Error("the repository did not come back")
	}
	if got := r.paths(repo.Dir, groupWorking); len(got) != 1 && len(r.paths(repo.Dir, groupUntracked)) != 1 {
		t.Errorf("status = %v", got)
	}
}

func TestAnewGitFolderIsFoundByARescan(t *testing.T) {
	e := gittest.NewEnv(t)
	dir := filepath.Join(e.Work, "plain")
	writeFile(t, filepath.Join(dir, "a.txt"), "a\n")
	r := newRig(t, e, rigOptions{folder: dir})
	r.settle()
	if len(r.api.SourceControls()) != 0 {
		t.Fatal("a repository in a folder with none")
	}
	// git init inside the folder
	e.InitAt(dir)
	r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileCreated, Path: ".git"}}})
	r.api.Pump()
	if r.clock.Pending() != 1 {
		t.Fatalf("%d timers pending", r.clock.Pending())
	}
	r.clock.Advance(DebounceDelay)
	r.api.WaitFor("the repository", func() bool { r.api.Pump(); return len(r.x.model.order) == 1 })
	// a change inside the .git of an open repository does not scan again
	r.api.Emit(extension.TopicFilesChanged, extension.FilesChanged{Changes: []extension.FileChange{{Kind: extension.FileChanged, Path: ".git"}}})
	if r.clock.Pending() != 0 {
		t.Error("a change of an open repository's .git scanned again")
	}
}

func TestSettingsThatChangeTheStatusRefreshTheRepositories(t *testing.T) {
	r, repo, rep := repoWithChange(t, rigOptions{})
	repo.Write("new.txt", "n\n")
	r.refresh(rep)
	if got := r.paths(repo.Dir, groupWorking); len(got) != 2 {
		t.Fatalf("working = %v", got)
	}
	before := rep.refreshes
	r.api.SetSetting("git.untrackedChanges", "hidden")
	r.api.WaitFor("the status", func() bool { r.api.Pump(); return rep.refreshes > before && !rep.refreshing })
	if got := r.paths(repo.Dir, groupWorking); len(got) != 1 {
		t.Errorf("working = %v with the mode hidden", got)
	}
	r.api.SetSetting("git.untrackedChanges", "separate")
	r.api.WaitFor("the status again", func() bool { r.api.Pump(); return len(r.paths(repo.Dir, groupUntracked)) == 1 })
	if got := r.paths(repo.Dir, groupWorking); len(got) != 1 {
		t.Errorf("working = %v with the mode separate", got)
	}
}

func TestAConfigurationWithProgramsIsMentionedOnceWhenTheParentIsTrusted(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.Write("a.txt", "a\n")
	repo.Git("config", "core.fsmonitor", "/bin/true")
	// the harness trusts the repository by itself: no notification, the user saw the list
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	if len(notesWith(r, "names programs")) != 0 {
		t.Errorf("notes = %+v", r.api.Notes())
	}
	// trusted through the parent folder, with a sibling: the question was about the folder, not about the list of programs
	e2 := gittest.NewEnv(t)
	parent := filepath.Join(e2.Work, "projects")
	a := untrustedRepo(t, e2, filepath.Join(parent, "a"))
	b := untrustedRepo(t, e2, filepath.Join(parent, "b"))
	e2.Git(a, "config", "core.fsmonitor", "/bin/true")
	r2 := newRig(t, e2, rigOptions{folder: parent, settings: map[string]any{"git.repositoryScanMaxDepth": 1}})
	r2.settle()
	dialogs := r2.api.OpenDialogs()
	if len(dialogs) != 1 {
		t.Fatalf("dialogs = %d", len(dialogs))
	}
	dialogs[0].Press("Trust All Repositories in projects")
	r2.api.WaitFor("the repositories to open", func() bool {
		r2.api.Pump()
		ra, rb := r2.x.model.byRoot(a), r2.x.model.byRoot(b)
		return ra.repo != nil && rb.repo != nil && !ra.refreshing && !rb.refreshing && ra.refreshed && rb.refreshed
	})
	n := notesWith(r2, "names programs")
	if len(n) != 2 {
		t.Fatalf("notes = %+v", r2.api.Notes())
	}
	for _, i := range n {
		msg := r2.api.Notes()[i].Message
		if !strings.Contains(msg, "you trusted all the repositories in "+parent) || !strings.Contains(msg, "core.hooksPath") {
			t.Errorf("message = %q", msg)
		}
	}
	if m := r2.api.Notes()[n[0]].Message; !strings.Contains(m, "configuration of a ") || !strings.Contains(m, "core.fsmonitor") {
		t.Errorf("message of a = %q", m)
	}
	r2.refresh(r2.repo(a))
	if len(notesWith(r2, "names programs")) != 2 {
		t.Error("a notification came again")
	}
}
