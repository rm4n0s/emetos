package gitscm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// Commit against real repositories (step 24.10): each command runs through the registry of the fake API, the way a key, a menu or the
// action button runs it, and the test looks at what git made.

// commitRepo is a repository with a commit and a staged change to a.txt; b.txt is changed and not staged, u.txt is untracked.
func commitRepo(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	r := e.Init("commit")
	r.Write("a.txt", "one\n")
	r.Write("b.txt", "bee\n")
	r.Commit("Add the files")
	r.Write("a.txt", "one\ntwo\n")
	r.Git("add", "a.txt")
	r.Write("b.txt", "bee\nsee\n")
	r.Write("u.txt", "untracked\n")
	return r
}

// commitRig activates the extension over a commitRepo.
func commitRig(t *testing.T, o rigOptions) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := commitRepo(t, e)
	return commitRigOf(t, e, repo, o)
}

// commitRigOf activates the extension over a repository of the world e.
func commitRigOf(t *testing.T, e *gittest.Env, repo *gittest.Repo, o rigOptions) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	o.folder = repo.Dir
	if o.settings == nil {
		o.settings = map[string]any{}
	}
	if _, ok := o.settings["git.useEditorAsCommitInput"]; !ok {
		o.settings["git.useEditorAsCommitInput"] = false
	}
	r := newRig(t, e, o)
	r.settle()
	return r, repo, r.repo(repo.Dir)
}

// typeMessage plays the user typing in the box of the repository.
func (r *rig) typeMessage(rp *repository, text string) {
	r.t.Helper()
	r.source(rp.root).Input().(*fakeapi.FakeInput).Type(text)
	r.api.Pump()
}

// message is the text of the box.
func (r *rig) message(rp *repository) string { return r.source(rp.root).Input().Value() }

// start runs a command and returns without waiting: the questions that it asks are open.
func (r *rig) start(rp *repository, cmd string, args any) {
	r.t.Helper()
	if err := r.api.Run(cmd, args); err != nil {
		r.t.Fatalf("%s: %v", cmd, err)
	}
	r.api.Pump()
}

// answer presses a button of the only question that is open.
func (r *rig) answer(label string) {
	r.t.Helper()
	r.theOnlyDialog().Press(label)
	r.api.Pump()
}

// quiet says that no question is open (after the commands ended).
func (r *rig) quiet() bool { return len(r.api.OpenDialogs()) == 0 }

// commitBox runs git.commit with the source control as its argument, as Ctrl+Enter in the box does.
func (r *rig) commitBox(rp *repository) {
	r.t.Helper()
	r.run(rp, "git.commit", []any{sourceArg(rp)})
}

// lastNote is the last notification, or fails the test.
func (r *rig) lastNote() fakeapi.Note {
	r.t.Helper()
	notes := r.api.Notes()
	if len(notes) == 0 {
		r.t.Fatal("no notification was shown")
	}
	return notes[len(notes)-1]
}

// count is the number of commits on HEAD.
func count(repo *gittest.Repo) string { return repo.Git("rev-list", "--count", "HEAD") }

func TestCommitFromTheBox(t *testing.T) {
	r, repo, rp := commitRig(t, rigOptions{})
	before := repo.Head()
	r.typeMessage(rp, "Fix the cache key of the loader\n\nThe key ignored the scheme.")
	r.commitBox(rp)
	if repo.Head() == before || count(repo) != "2" {
		t.Fatalf("no commit was made: %s commits", count(repo))
	}
	if got := repo.Git("log", "-1", "--format=%B"); strings.TrimSpace(got) != "Fix the cache key of the loader\n\nThe key ignored the scheme." {
		t.Errorf("message = %q", got)
	}
	if got := repo.Git("log", "-1", "--format=%an <%ae> %P"); got != "Ada Example <ada@example.com> "+before {
		t.Errorf("author and parent = %q", got)
	}
	if got := repo.Git("show", "--format=", "--name-only", "HEAD"); got != "a.txt" {
		t.Errorf("the commit has %q (only the staged file belongs in it)", got)
	}
	if got := repo.Git("show", "HEAD:a.txt"); got != "one\ntwo" {
		t.Errorf("a.txt in the commit = %q", got)
	}
	if got := r.message(rp); got != "" {
		t.Errorf("the box still holds %q", got)
	}
	if got := r.paths(rp.root, groupIndex); len(got) != 0 {
		t.Errorf("Staged Changes still holds %v", got)
	}
	if got := r.paths(rp.root, groupWorking); strings.Join(got, ",") != "b.txt,u.txt" {
		t.Errorf("Changes holds %v", got)
	}
}

func TestEveryCommitCommand(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		settings map[string]any
		message  string   // the box
		count    string   // commits afterwards
		subject  string   // the subject of HEAD
		files    []string // what the last commit changed
		trailer  string
		answer   string // the button of the question that is asked, if any
	}{
		{name: "commit", cmd: "git.commit", message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt"}},
		{name: "commit staged", cmd: "git.commitStaged", message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt"}},
		{name: "commit all", cmd: "git.commitAll", message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt", "b.txt", "u.txt"}},
		{name: "commit signed off", cmd: "git.commitSigned", message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt"}, trailer: "Signed-off-by: Ada Example <ada@example.com>"},
		{name: "commit staged signed off", cmd: "git.commitStagedSigned", message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt"}, trailer: "Signed-off-by: Ada Example <ada@example.com>"},
		{name: "commit all signed off", cmd: "git.commitAllSigned", message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt", "b.txt", "u.txt"}, trailer: "Signed-off-by: Ada Example <ada@example.com>"},
		{name: "commit amend", cmd: "git.commitAmend", message: "Better", count: "1", subject: "Better", files: []string{"a.txt", "b.txt"}},
		{name: "commit amend keeps the message when the box is empty", cmd: "git.commitAmend", message: "", count: "1", subject: "Add the files", files: []string{"a.txt", "b.txt"}},
		{name: "commit staged amend", cmd: "git.commitStagedAmend", message: "Better", count: "1", subject: "Better", files: []string{"a.txt", "b.txt"}},
		{name: "commit all amend", cmd: "git.commitAllAmend", message: "Better", count: "1", subject: "Better", files: []string{"a.txt", "b.txt", "u.txt"}},
		{name: "commit with no verify", cmd: "git.commitNoVerify", settings: map[string]any{"git.allowNoVerifyCommit": true}, message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt"}, answer: "OK"},
		{name: "commit staged with no verify", cmd: "git.commitStagedNoVerify", settings: map[string]any{"git.allowNoVerifyCommit": true}, message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt"}, answer: "OK"},
		{name: "commit all with no verify", cmd: "git.commitAllNoVerify", settings: map[string]any{"git.allowNoVerifyCommit": true}, message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt", "b.txt", "u.txt"}, answer: "OK"},
		{name: "commit signed off with no verify", cmd: "git.commitSignedNoVerify", settings: map[string]any{"git.allowNoVerifyCommit": true}, message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt"}, trailer: "Signed-off-by:", answer: "OK"},
		{name: "commit staged signed off with no verify", cmd: "git.commitStagedSignedNoVerify", settings: map[string]any{"git.allowNoVerifyCommit": true}, message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt"}, trailer: "Signed-off-by:", answer: "OK"},
		{name: "commit all signed off with no verify", cmd: "git.commitAllSignedNoVerify", settings: map[string]any{"git.allowNoVerifyCommit": true}, message: "Fix", count: "2", subject: "Fix", files: []string{"a.txt", "b.txt", "u.txt"}, trailer: "Signed-off-by:", answer: "OK"},
		{name: "commit amend with no verify", cmd: "git.commitAmendNoVerify", settings: map[string]any{"git.allowNoVerifyCommit": true}, message: "Better", count: "1", subject: "Better", files: []string{"a.txt", "b.txt"}, answer: "OK"},
		{name: "commit staged amend with no verify", cmd: "git.commitStagedAmendNoVerify", settings: map[string]any{"git.allowNoVerifyCommit": true}, message: "Better", count: "1", subject: "Better", files: []string{"a.txt", "b.txt"}, answer: "OK"},
		{name: "commit all amend with no verify", cmd: "git.commitAllAmendNoVerify", settings: map[string]any{"git.allowNoVerifyCommit": true}, message: "Better", count: "1", subject: "Better", files: []string{"a.txt", "b.txt", "u.txt"}, answer: "OK"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := commitRig(t, rigOptions{settings: tc.settings})
			// a hook that stops every commit proves that the no-verify variants skip it
			if strings.Contains(tc.cmd, "NoVerify") {
				repo.Hook("pre-commit", "#!/bin/sh\necho blocked >&2\nexit 1\n")
			}
			r.typeMessage(rp, tc.message)
			r.start(rp, tc.cmd, []any{sourceArg(rp)})
			if tc.answer != "" {
				r.answer(tc.answer)
			}
			r.idle(rp)
			if !r.quiet() {
				t.Fatalf("a question is open: %q", r.api.OpenDialogs()[0].Spec().Message)
			}
			if got := count(repo); got != tc.count {
				t.Fatalf("%s commits, want %s; notes %+v", got, tc.count, r.api.Notes())
			}
			if got := repo.Git("log", "-1", "--format=%s"); got != tc.subject {
				t.Errorf("subject = %q, want %q", got, tc.subject)
			}
			files := strings.Split(repo.Git("show", "--format=", "--name-only", "HEAD"), "\n")
			if strings.Join(files, ",") != strings.Join(tc.files, ",") {
				t.Errorf("files = %v, want %v", files, tc.files)
			}
			if tc.trailer != "" && !strings.Contains(repo.Git("log", "-1", "--format=%B"), tc.trailer) {
				t.Errorf("message %q lacks %q", repo.Git("log", "-1", "--format=%B"), tc.trailer)
			}
		})
	}
}

func TestACommitFromThePaletteWithAnEmptyBoxAsksForAMessage(t *testing.T) {
	r, repo, rp := commitRig(t, rigOptions{})
	r.start(rp, "git.commit", nil)
	in := r.nextInput(0)
	o := in.Options()
	if o.Message != "Please provide a commit message" || o.Prompt != `Message (commit on "main")` || !o.IgnoreFocusOut {
		t.Errorf("input box %+v", o)
	}
	in.Accept("Typed in the box that pops up")
	r.idle(rp)
	if got := repo.Git("log", "-1", "--format=%s"); got != "Typed in the box that pops up" || count(repo) != "2" {
		t.Errorf("last commit %q, %s commits", got, count(repo))
	}
	t.Run("closing the box makes no commit", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{})
		r.start(rp, "git.commit", nil)
		r.nextInput(0).Cancel()
		r.idle(rp)
		if count(repo) != "1" || !r.quiet() {
			t.Errorf("%s commits", count(repo))
		}
	})
	t.Run("an amend keeps the message and asks nothing", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{})
		r.run(rp, "git.commitAmend", nil)
		if len(r.api.Inputs()) != 0 || count(repo) != "1" || repo.Git("log", "-1", "--format=%s") != "Add the files" {
			t.Errorf("inputs %d, %s commits", len(r.api.Inputs()), count(repo))
		}
	})
}

func TestACommitWithoutAMessageUsesTheEditorOfGit(t *testing.T) {
	// git.useEditorAsCommitInput is on: git starts the helper, which opens COMMIT_EDITMSG in a tab
	editor := func(t *testing.T) (*rig, *gittest.Repo, *repository) {
		t.Helper()
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		return commitRigOf(t, e, repo, rigOptions{settings: map[string]any{"git.useEditorAsCommitInput": true}})
	}
	t.Run("saved and closed", func(t *testing.T) {
		r, repo, rp := editor(t)
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		path, uri := r.editorOpened(0)
		writeFile(t, path, "Written in the editor\n")
		r.userSaves(uri)
		r.userCloses(uri)
		r.idle(rp)
		if count(repo) != "2" || repo.Git("log", "-1", "--format=%s") != "Written in the editor" {
			t.Errorf("%s commits, last %q; notes %+v", count(repo), repo.Git("log", "-1", "--format=%s"), r.api.Notes())
		}
	})
	t.Run("saved with an empty message", func(t *testing.T) {
		r, repo, rp := editor(t)
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		path, uri := r.editorOpened(0)
		writeFile(t, path, "# only a comment\n")
		r.userSaves(uri)
		r.userCloses(uri)
		r.idle(rp)
		if count(repo) != "1" {
			t.Errorf("%s commits", count(repo))
		}
		if n := r.lastNote(); n.Severity != "info" || n.Message != "Commit operation was cancelled due to empty commit message." {
			t.Errorf("note %+v", n)
		}
	})
	t.Run("closed without saving", func(t *testing.T) {
		r, repo, rp := editor(t)
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		_, uri := r.editorOpened(0)
		r.userCloses(uri)
		r.idle(rp)
		if count(repo) != "1" {
			t.Errorf("%s commits", count(repo))
		}
		if n := r.lastNote(); n.Severity != "info" || n.Message != "Commit operation was cancelled." {
			t.Errorf("note %+v", n)
		}
	})
	t.Run("amend shows the last message", func(t *testing.T) {
		r, repo, rp := editor(t)
		r.start(rp, "git.commitAmend", []any{sourceArg(rp)})
		path, uri := r.editorOpened(0)
		if got := repo.Read(".git/COMMIT_EDITMSG"); !strings.HasPrefix(got, "Add the files\n") {
			t.Errorf("the editor shows %q", got)
		}
		writeFile(t, path, "Add the files, reworded\n")
		r.userSaves(uri)
		r.userCloses(uri)
		r.idle(rp)
		if count(repo) != "1" || repo.Git("log", "-1", "--format=%s") != "Add the files, reworded" {
			t.Errorf("%s commits, last %q", count(repo), repo.Git("log", "-1", "--format=%s"))
		}
	})
	t.Run("verbose shows the diff", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := commitRepo(t, e)
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{settings: map[string]any{"git.useEditorAsCommitInput": true, "git.verboseCommit": true}})
		r.start(rp, "git.commit", []any{sourceArg(rp)})
		_, uri := r.editorOpened(0)
		if got := repo.Read(".git/COMMIT_EDITMSG"); !strings.Contains(got, "diff --git a/a.txt b/a.txt") {
			t.Errorf("the editor shows %q", got)
		}
		r.userCloses(uri)
		r.idle(rp)
	})
	t.Run("a message in the box is used and no editor opens", func(t *testing.T) {
		r, repo, rp := editor(t)
		r.typeMessage(rp, "From the box")
		r.run(rp, "git.commit", []any{sourceArg(rp)})
		if len(r.api.ShownDocuments()) != 0 || repo.Git("log", "-1", "--format=%s") != "From the box" {
			t.Errorf("%d documents shown", len(r.api.ShownDocuments()))
		}
	})
}

func TestACommitThatFailsKeepsTheMessageAndSaysWhy(t *testing.T) {
	r, repo, rp := commitRig(t, rigOptions{})
	repo.Hook("pre-commit", "#!/bin/sh\necho 'lint: trailing spaces in a.txt' >&2\nexit 1\n")
	r.typeMessage(rp, "Will not be committed")
	r.commitBox(rp)
	if count(repo) != "1" {
		t.Fatal("the commit went through")
	}
	if got := r.message(rp); got != "Will not be committed" {
		t.Errorf("the box holds %q", got)
	}
	n := r.lastNote()
	if n.Severity != "error" || !strings.Contains(n.Message, "lint: trailing spaces in a.txt") || !strings.HasPrefix(n.Message, "Git: ") {
		t.Errorf("note %+v", n)
	}
	if len(n.Actions) != 1 || n.Actions[0] != "Show Git Output" {
		t.Errorf("actions %v", n.Actions)
	}
	if got := r.paths(rp.root, groupIndex); strings.Join(got, ",") != "a.txt" {
		t.Errorf("Staged Changes holds %v after the failure", got)
	}
	if !r.source(rp.root).Input().(*fakeapi.FakeInput).Enabled() {
		t.Error("the box stayed disabled")
	}
	if b := r.source(rp.root).ActionButton(); b == nil || !b.Enabled {
		t.Errorf("button %+v", b)
	}
	// the hook that passes lets it through, and the box is empty afterwards
	repo.Hook("pre-commit", "#!/bin/sh\nexit 0\n")
	r.commitBox(rp)
	if count(repo) != "2" || r.message(rp) != "" {
		t.Errorf("%s commits, box %q", count(repo), r.message(rp))
	}
}

func TestACommitIsASingleOperationOfTheQueue(t *testing.T) {
	r, repo, rp := commitRig(t, rigOptions{})
	marker := filepath.Join(repo.Env.Root, "release")
	gate := filepath.Join(repo.Env.Root, "hook-started")
	// the hook waits for the test, so the commit is running while the second command arrives
	repo.Hook("pre-commit", "#!/bin/sh\n: > '"+gate+"'\nwhile [ ! -e '"+marker+"' ]; do sleep 0.01; done\nexit 0\n")
	r.typeMessage(rp, "First")
	r.start(rp, "git.commit", []any{sourceArg(rp)})
	r.api.WaitFor("the hook to start", func() bool { _, err := os.Stat(gate); return err == nil })
	// while it runs: the box and the button are off, and git.commitInProgress is on
	if r.source(rp.root).Input().(*fakeapi.FakeInput).Enabled() {
		t.Error("the box is enabled during the commit")
	}
	if b := r.source(rp.root).ActionButton(); b == nil || b.Enabled {
		t.Errorf("the button is enabled during the commit: %+v", b)
	}
	if v, _ := r.api.ContextKey("git.commitInProgress").(bool); !v {
		t.Error("git.commitInProgress is off during the commit")
	}
	// a second commit waits for the first and the status after it: nothing is left to commit
	r.typeMessage(rp, "Second")
	r.start(rp, "git.commit", []any{sourceArg(rp)})
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the first commit to end", func() bool { return !rp.committing && count(repo) == "2" })
	r.idle(rp)
	// git.commit with nothing staged and the suggestion on asks about smart commit
	d := r.theOnlyDialog()
	if d.Spec().Message != "There are no staged changes to commit." {
		t.Errorf("the second command asked %q", d.Spec().Message)
	}
	d.Cancel()
	r.api.Pump()
	r.idle(rp)
	if count(repo) != "2" || repo.Git("log", "-1", "--format=%s") != "First" {
		t.Errorf("%s commits, last %q", count(repo), repo.Git("log", "-1", "--format=%s"))
	}
	if v, _ := r.api.ContextKey("git.commitInProgress").(bool); v {
		t.Error("git.commitInProgress is still on")
	}
}

func TestSmartCommit(t *testing.T) {
	// nothing staged: a.txt is unstaged here, b.txt changed, u.txt untracked
	unstage := func(repo *gittest.Repo) { repo.Git("reset", "-q") }
	tests := []struct {
		name     string
		settings map[string]any
		prepare  func(repo *gittest.Repo)
		cmd      string
		answer   string // the button of the question
		asks     bool
		files    []string // the files of the new commit; nil for none
		after    map[string]any
	}{
		{name: "off, with something staged, commits what is staged", settings: map[string]any{"git.enableSmartCommit": false}, cmd: "git.commit",
			files: []string{"a.txt"}},
		{name: "off, nothing staged, asks and Yes commits everything", prepare: unstage, cmd: "git.commit", asks: true, answer: "Yes",
			files: []string{"a.txt", "b.txt", "u.txt"}},
		{name: "off, nothing staged, Always turns it on and commits", prepare: unstage, cmd: "git.commit", asks: true, answer: "Always",
			files: []string{"a.txt", "b.txt", "u.txt"}, after: map[string]any{"git.enableSmartCommit": true}},
		{name: "off, nothing staged, Never turns the suggestion off and commits nothing", prepare: unstage, cmd: "git.commit", asks: true, answer: "Never",
			after: map[string]any{"git.suggestSmartCommit": false}},
		{name: "off, nothing staged, Cancel commits nothing", prepare: unstage, cmd: "git.commit", asks: true, answer: "Cancel"},
		{name: "off, nothing staged, the suggestion off, does nothing at all", settings: map[string]any{"git.suggestSmartCommit": false}, prepare: unstage, cmd: "git.commit"},
		{name: "on, nothing staged, commits everything without asking", settings: map[string]any{"git.enableSmartCommit": true}, prepare: unstage, cmd: "git.commit",
			files: []string{"a.txt", "b.txt", "u.txt"}},
		{name: "on, something staged, commits only that", settings: map[string]any{"git.enableSmartCommit": true}, cmd: "git.commit", files: []string{"a.txt"}},
		{name: "on with tracked changes, the untracked file stays", settings: map[string]any{"git.enableSmartCommit": true, "git.smartCommitChanges": "tracked"}, prepare: unstage, cmd: "git.commit",
			files: []string{"a.txt", "b.txt"}},
		{name: "commit all with tracked changes only", settings: map[string]any{"git.smartCommitChanges": "tracked"}, cmd: "git.commitAll", files: []string{"a.txt", "b.txt"}},
		{name: "commit all with the untracked files hidden", settings: map[string]any{"git.untrackedChanges": "hidden"}, cmd: "git.commitAll", files: []string{"a.txt", "b.txt"}},
		{name: "commit staged never stages anything with smart commit off", prepare: unstage, cmd: "git.commitStaged", asks: true, answer: "Cancel"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := commitRepo(t, e)
			if tc.prepare != nil {
				tc.prepare(repo)
			}
			r, repo, rp := commitRigOf(t, e, repo, rigOptions{settings: tc.settings})
			before := count(repo)
			r.typeMessage(rp, "Smart")
			r.start(rp, tc.cmd, []any{sourceArg(rp)})
			if tc.asks {
				d := r.theOnlyDialog()
				if d.Spec().Message != "There are no staged changes to commit." || d.Spec().Detail != "Would you like to stage all your changes and commit them directly?" {
					t.Errorf("question %q / %q", d.Spec().Message, d.Spec().Detail)
				}
				if got := strings.Join(buttons(d), ","); got != "Yes,Always,Never,Cancel" {
					t.Errorf("buttons %s", got)
				}
				r.answer(tc.answer)
			}
			r.idle(rp)
			if !r.quiet() && !tc.asks {
				t.Fatalf("a question is open: %q", r.api.OpenDialogs()[0].Spec().Message)
			}
			if tc.files == nil {
				if count(repo) != before {
					t.Errorf("a commit was made")
				}
			} else {
				if count(repo) == before {
					t.Fatalf("no commit; notes %+v", r.api.Notes())
				}
				files := strings.Split(repo.Git("show", "--format=", "--name-only", "HEAD"), "\n")
				if strings.Join(files, ",") != strings.Join(tc.files, ",") {
					t.Errorf("files = %v, want %v", files, tc.files)
				}
			}
			for key, want := range tc.after {
				var got any
				for _, u := range r.api.SettingUpdates() {
					if u.Key == key {
						got = u.Value
					}
				}
				if got != want {
					t.Errorf("setting %s = %v, want %v", key, got, want)
				}
			}
		})
	}
}

func TestACommitWithNothingToCommit(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("clean")
	repo.Write("a.txt", "a\n")
	repo.Commit("first")
	r, repo, rp := commitRigOf(t, e, repo, rigOptions{})
	r.typeMessage(rp, "Nothing")
	r.start(rp, "git.commit", []any{sourceArg(rp)})
	n := r.lastNote()
	if n.Message != "There are no changes to commit." || len(n.Actions) != 1 || n.Actions[0] != "Create Empty Commit" {
		t.Fatalf("note %+v", n)
	}
	r.api.RunNoteAction(len(r.api.Notes())-1, "Create Empty Commit")
	r.idle(rp)
	if count(repo) != "2" || repo.Git("log", "-1", "--format=%s") != "Nothing" || repo.Git("show", "--format=", "--name-only", "HEAD") != "" {
		t.Errorf("%s commits, last %q", count(repo), repo.Git("log", "-1", "--format=%s"))
	}
	t.Run("an amend with nothing to commit changes the message only", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("clean")
		repo.Write("a.txt", "a\n")
		repo.Commit("first")
		r, repo, rp := commitRigOf(t, e, repo, rigOptions{})
		r.typeMessage(rp, "Reworded")
		r.run(rp, "git.commitAmend", []any{sourceArg(rp)})
		if count(repo) != "1" || repo.Git("log", "-1", "--format=%s") != "Reworded" || !r.quiet() {
			t.Errorf("%s commits, last %q", count(repo), repo.Git("log", "-1", "--format=%s"))
		}
	})
}

func TestCommitEmpty(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		settings map[string]any
		answer   string
		asks     bool
		commits  string
		noVerify bool
		after    map[string]any
	}{
		{name: "asked, Yes", cmd: "git.commitEmpty", asks: true, answer: "Yes", commits: "2"},
		{name: "asked, Yes and do not ask again", cmd: "git.commitEmpty", asks: true, answer: "Yes, Don't Show Again", commits: "2", after: map[string]any{"git.confirmEmptyCommits": false}},
		{name: "asked, Cancel", cmd: "git.commitEmpty", asks: true, answer: "Cancel", commits: "1"},
		{name: "not asked when turned off", cmd: "git.commitEmpty", settings: map[string]any{"git.confirmEmptyCommits": false}, commits: "2"},
		{name: "no verify", cmd: "git.commitEmptyNoVerify", settings: map[string]any{"git.allowNoVerifyCommit": true, "git.confirmEmptyCommits": false, "git.confirmNoVerifyCommit": false}, commits: "2", noVerify: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := commitRepo(t, e)
			repo.Git("reset", "-q") // nothing is staged: the commit has no change to take
			r, repo, rp := commitRigOf(t, e, repo, rigOptions{settings: tc.settings})
			if tc.noVerify {
				repo.Hook("pre-commit", "#!/bin/sh\nexit 1\n")
			}
			r.typeMessage(rp, "An empty commit")
			r.start(rp, tc.cmd, []any{sourceArg(rp)})
			if tc.asks {
				d := r.theOnlyDialog()
				if d.Spec().Message != "Are you sure you want to create an empty commit?" || strings.Join(buttons(d), ",") != "Yes,Yes, Don't Show Again,Cancel" {
					t.Errorf("question %q %v", d.Spec().Message, buttons(d))
				}
				r.answer(tc.answer)
			}
			r.idle(rp)
			if got := count(repo); got != tc.commits {
				t.Fatalf("%s commits, want %s", got, tc.commits)
			}
			if tc.commits == "2" {
				// an empty commit has no changes, and the changes of the work tree are still there
				if got := repo.Git("show", "--format=", "--name-only", "HEAD"); got != "" {
					t.Errorf("the empty commit has %q", got)
				}
				if got := strings.Join(porcelain(repo), "|"); got != " M a.txt| M b.txt|?? u.txt" {
					t.Errorf("the work tree changed: %q", got)
				}
			}
			for key, want := range tc.after {
				var got any
				for _, u := range r.api.SettingUpdates() {
					if u.Key == key {
						got = u.Value
					}
				}
				if got != want {
					t.Errorf("setting %s = %v, want %v", key, got, want)
				}
			}
		})
	}
}

func TestCommitWithoutVerification(t *testing.T) {
	t.Run("not allowed by the setting", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{})
		r.typeMessage(rp, "x")
		r.run(rp, "git.commitNoVerify", []any{sourceArg(rp)})
		n := r.lastNote()
		if n.Severity != "error" || !strings.Contains(n.Message, `"git.allowNoVerifyCommit"`) || count(repo) != "1" {
			t.Errorf("note %+v, %s commits", n, count(repo))
		}
	})
	tests := []struct {
		name    string
		answer  string
		commits string
		after   any
	}{
		{"OK", "OK", "2", nil},
		{"OK and do not ask again", "OK, Don't Ask Again", "2", false},
		{"Cancel", "Cancel", "1", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := commitRig(t, rigOptions{settings: map[string]any{"git.allowNoVerifyCommit": true}})
			repo.Hook("pre-commit", "#!/bin/sh\nexit 1\n")
			r.typeMessage(rp, "x")
			r.start(rp, "git.commitNoVerify", []any{sourceArg(rp)})
			d := r.theOnlyDialog()
			if !strings.HasPrefix(d.Spec().Message, "You are about to commit your changes without verification") || d.Spec().Detail != "Are you sure to continue?" {
				t.Errorf("question %q / %q", d.Spec().Message, d.Spec().Detail)
			}
			r.answer(tc.answer)
			r.idle(rp)
			if count(repo) != tc.commits {
				t.Errorf("%s commits", count(repo))
			}
			var got any
			for _, u := range r.api.SettingUpdates() {
				if u.Key == "git.confirmNoVerifyCommit" {
					got = u.Value
				}
			}
			if got != tc.after {
				t.Errorf("git.confirmNoVerifyCommit updated to %v, want %v", got, tc.after)
			}
		})
	}
	t.Run("not asked when turned off", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: map[string]any{"git.allowNoVerifyCommit": true, "git.confirmNoVerifyCommit": false}})
		repo.Hook("pre-commit", "#!/bin/sh\nexit 1\n")
		r.typeMessage(rp, "x")
		r.run(rp, "git.commitNoVerify", []any{sourceArg(rp)})
		if count(repo) != "2" || !r.quiet() {
			t.Errorf("%s commits", count(repo))
		}
	})
}

func TestTheSettingsOfTheCommit(t *testing.T) {
	t.Run("always sign off", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: map[string]any{"git.alwaysSignOff": true}})
		r.typeMessage(rp, "Fix")
		r.commitBox(rp)
		if got := repo.Git("log", "-1", "--format=%B"); !strings.Contains(got, "Signed-off-by: Ada Example <ada@example.com>") {
			t.Errorf("message %q", got)
		}
	})
	// signing is handed to git: the flag is passed, no key is made (the signing program of the repository fails)
	t.Run("commit signing passes -S", func(t *testing.T) {
		r, repo, rp := commitRig(t, rigOptions{settings: map[string]any{"git.enableCommitSigning": true}})
		repo.Git("config", "gpg.program", "false")
		r.typeMessage(rp, "Signed")
		r.commitBox(rp)
		if count(repo) != "1" {
			t.Error("a commit was made with a signing program that fails")
		}
		if line := lastCommitLine(r); !strings.Contains(line, " -S") || strings.Contains(line, "--no-gpg-sign") {
			t.Errorf("the command was %q", line)
		}
	})
	t.Run("commit signing off leaves it to the configuration", func(t *testing.T) {
		r, _, rp := commitRig(t, rigOptions{})
		r.typeMessage(rp, "Not signed")
		r.commitBox(rp)
		if line := lastCommitLine(r); strings.Contains(line, " -S") || strings.Contains(line, "--no-gpg-sign") {
			t.Errorf("the command was %q", line)
		}
	})
	t.Run("the user configuration is required", func(t *testing.T) {
		r, _, rp := commitRig(t, rigOptions{})
		r.typeMessage(rp, "x")
		r.commitBox(rp)
		if line := lastCommitLine(r); !strings.Contains(line, "-c user.useConfigOnly=true commit") {
			t.Errorf("the command was %q", line)
		}
	})
	t.Run("the user configuration is not required", func(t *testing.T) {
		r, _, rp := commitRig(t, rigOptions{settings: map[string]any{"git.requireGitUserConfig": false}})
		r.typeMessage(rp, "x")
		r.commitBox(rp)
		if line := lastCommitLine(r); strings.Contains(line, "useConfigOnly") {
			t.Errorf("the command was %q", line)
		}
	})
}

// lastCommitLine is the last "> git ... commit ..." line of the Git channel.
func lastCommitLine(r *rig) string {
	var line string
	for _, l := range r.api.Channel(ChannelName).Lines() {
		if strings.HasPrefix(l, "> git") && strings.Contains(l, " commit ") {
			line = l
		}
	}
	return line
}
