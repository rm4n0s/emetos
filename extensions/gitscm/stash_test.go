package gitscm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The stash against real repositories (step 24.18): each command runs through the registry of the fake API, and the test looks at what
// git says afterwards.

// stashRepo builds a repository with a staged change, a change of the work tree and an untracked file.
func stashRepo(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	r := e.Init("stash")
	for _, f := range []string{"a.txt", "b.txt", "c.txt", "dir/d.txt", "old.txt"} {
		r.Write(f, f+"\none\ntwo\nthree\nfour\n")
	}
	r.Commit("Add the files")
	r.Write("a.txt", "a.txt\nchanged\n")
	r.Write("b.txt", "b.txt\nstaged\n")
	r.Git("add", "b.txt")
	r.Write("u.txt", "untracked\n")
	return r
}

func stashRig(t *testing.T, o rigOptions) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := stashRepo(t, e)
	o.folder = repo.Dir
	r := newRig(t, e, o)
	r.settle()
	return r, repo, r.repo(repo.Dir)
}

// stashSubjects are the subjects of the stashes of the repository, the newest first.
func stashSubjects(repo *gittest.Repo) []string {
	out := strings.TrimSpace(repo.Git("stash", "list", "--format=%gs"))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// readOr reads a file of the repository, "" while it is not there (git is replacing it).
func readOr(repo *gittest.Repo, rel string) string {
	data, err := os.ReadFile(repo.Path(rel))
	if err != nil {
		return ""
	}
	return string(data)
}

// state is "git status --porcelain" of the repository as one line.
func state(repo *gittest.Repo) string { return strings.Join(porcelain(repo), "|") }

func skipBefore235(t *testing.T, repo *gittest.Repo) {
	t.Helper()
	v, err := git.ParseVersion(repo.Git("--version"))
	if err != nil {
		t.Fatal(err)
	}
	if !v.AtLeast(2, 35) {
		t.Skipf("git %s is older than 2.35, which git stash --staged needs", v)
	}
}

func TestStashCommandsMakeTheStash(t *testing.T) {
	const initial = " M a.txt|?? u.txt|M  b.txt"
	tests := []struct {
		name     string
		cmd      string
		args     any
		staged   bool
		want     string // the status afterwards
		subject  string // the end of the subject of the stash
		hasUntr  bool   // the stash holds u.txt
		skipOld  bool
		wantNote string
	}{
		{"the changes of tracked files", "git.stash", map[string]any{"message": "WIP loader cache"}, false, "?? u.txt", "On main: WIP loader cache", false, false, ""},
		{"with no message", "git.stash", map[string]any{"message": ""}, false, "?? u.txt", "WIP on main", false, false, ""},
		{"with the untracked files", "git.stashIncludeUntracked", map[string]any{"message": "all of it"}, false, "", "On main: all of it", true, false, ""},
		{"only what is staged", "git.stashStaged", map[string]any{"message": "the index"}, true, " M a.txt|?? u.txt", "On main: the index", false, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := stashRig(t, rigOptions{})
			if tc.skipOld {
				skipBefore235(t, repo)
			}
			if got := state(repo); got != initial {
				t.Fatalf("starts as %q", got)
			}
			r.run(rp, tc.cmd, tc.args)
			if got := state(repo); got != tc.want {
				t.Errorf("status = %q, want %q", got, tc.want)
			}
			subjects := stashSubjects(repo)
			if len(subjects) != 1 || !strings.HasSuffix(subjects[0], tc.subject) && !strings.Contains(subjects[0], tc.subject) {
				t.Fatalf("stashes = %q, want one with %q", subjects, tc.subject)
			}
			if got := strings.Contains(repo.Git("stash", "show", "--include-untracked", "--name-only", "stash@{0}"), "u.txt"); got != tc.hasUntr {
				t.Errorf("the stash holds u.txt = %v, want %v", got, tc.hasUntr)
			}
			if len(r.api.Inputs()) != 0 {
				t.Error("the input box was shown for a message that was given")
			}
			if rp.stashes != 1 {
				t.Errorf("the repository counts %d stashes", rp.stashes)
			}
		})
	}
}

func TestStashAsksForTheMessage(t *testing.T) {
	tests := []struct {
		name    string
		answer  func(in *fakeapi.Input)
		commit  string
		setting bool
		value   string // the text the box starts with
		stashes int
		subject string
	}{
		{"the text of the box is the message", func(in *fakeapi.Input) { in.Accept("from the box") }, "", false, "", 1, "On main: from the box"},
		{"an empty text is no message", func(in *fakeapi.Input) { in.Accept("") }, "", false, "", 1, "WIP on main"},
		{"escape stashes nothing", func(in *fakeapi.Input) { in.Cancel() }, "", false, "", 0, ""},
		{"the commit box is the first text when the setting is on", func(in *fakeapi.Input) { in.Accept("Fix the loader") }, "Fix the loader", true, "Fix the loader", 1, "On main: Fix the loader"},
		{"the commit box is not used when the setting is off", func(in *fakeapi.Input) { in.Accept("x") }, "Fix the loader", false, "", 1, "On main: x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := stashRig(t, rigOptions{settings: map[string]any{"git.useCommitInputAsStashMessage": tc.setting}})
			rp.src.Input().SetValue(tc.commit)
			if err := r.api.Run("git.stash", nil); err != nil {
				t.Fatal(err)
			}
			in := r.api.LastInput()
			if in == nil {
				t.Fatal("no input box")
			}
			if in.Options().Prompt != "Optionally provide a stash message" || in.Options().Value != tc.value {
				t.Errorf("box = %+v", in.Options())
			}
			tc.answer(in)
			r.idle(rp)
			got := stashSubjects(repo)
			if len(got) != tc.stashes || (tc.stashes == 1 && !strings.Contains(got[0], tc.subject)) {
				t.Errorf("stashes = %q, want %d with %q", got, tc.stashes, tc.subject)
			}
		})
	}
}

func TestStashHasNothingToStash(t *testing.T) {
	tests := []struct {
		name  string
		setup func(repo *gittest.Repo)
		cmd   string
		note  string
	}{
		{"a clean repository", func(repo *gittest.Repo) { repo.Git("reset", "-q", "--hard"); repo.Remove("u.txt") }, "git.stash", "There are no changes to stash."},
		{"nothing staged for the staged stash", func(repo *gittest.Repo) { repo.Git("reset", "-q") }, "git.stashStaged", "There are no staged changes to stash."},
		{"an untracked file is no change for the plain stash", func(repo *gittest.Repo) { repo.Git("reset", "-q", "--hard") }, "git.stash", "Git: there are no local changes to save"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := stashRig(t, rigOptions{})
			if tc.cmd == "git.stashStaged" {
				skipBefore235(t, repo)
			}
			tc.setup(repo)
			rp.refresh()
			r.settle()
			r.idle(rp)
			r.run(rp, tc.cmd, map[string]any{"message": ""})
			if n := notesWith(r, tc.note); len(n) != 1 {
				t.Errorf("notes %+v, want one with %q", r.api.Notes(), tc.note)
			}
			if got := stashSubjects(repo); got != nil {
				t.Errorf("stashes %q", got)
			}
		})
	}
}

func TestStashStagedNeedsGit235(t *testing.T) {
	r, repo, rp := stashRig(t, rigOptions{})
	r.x.install.Version = git.Version{Major: 2, Minor: 34}
	r.run(rp, "git.stashStaged", map[string]any{"message": ""})
	if n := notesWith(r, "Needs git 2.35 or newer"); len(n) != 1 || r.api.Notes()[n[0]].Severity != "error" {
		t.Errorf("notes %+v", r.api.Notes())
	}
	if stashSubjects(repo) != nil {
		t.Error("a stash was made")
	}
}

func TestStashBeforeTheFirstCommit(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("fresh")
	repo.Write("a.txt", "a\n")
	repo.Git("add", "a.txt")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	err := stashNeedsACommit(context.Background(), rp.repo)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("stashNeedsACommit.StashNoInitialCommit") {
		t.Errorf("error %v", err)
	}
	r.run(rp, "git.stash", map[string]any{"message": ""})
	if n := notesWith(r, "The repository does not have any commits. Please make an initial commit before creating a stash."); len(n) != 1 || r.api.Notes()[n[0]].Severity != "info" {
		t.Errorf("notes %+v", r.api.Notes())
	}
}

func TestStashFailureIsShownWithTheOutputButton(t *testing.T) {
	r, repo, rp := stashRig(t, rigOptions{})
	// a pre-existing lock makes git refuse the stash
	if err := os.WriteFile(filepath.Join(repo.Dir, ".git", "index.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.run(rp, "git.stash", map[string]any{"message": ""})
	n := notesWith(r, "Git: ")
	if len(n) != 1 || r.api.Notes()[n[0]].Severity != "error" || r.api.Notes()[n[0]].Actions[0] != "Show Git Output" {
		t.Errorf("notes %+v", r.api.Notes())
	}
}

func TestStashClosesTheDiffEditorsOfTheStashedFiles(t *testing.T) {
	tests := []struct {
		name      string
		untracked string
		cmd       string
		closed    []string
	}{
		// with untracked files mixed into the changes, the work tree group holds them and the plain stash closes their editors too
		{"the plain stash closes the index and the work tree", "mixed", "git.stash", []string{"index-b", "work-a", "work-u"}},
		{"the plain stash leaves the untracked files of their own group", "separate", "git.stash", []string{"index-b", "work-a"}},
		{"with the untracked files", "separate", "git.stashIncludeUntracked", []string{"index-b", "work-a", "work-u"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _, rp := stashRig(t, rigOptions{settings: map[string]any{"git.closeDiffOnOperation": true, "git.untrackedChanges": tc.untracked}})
			vers := func(ref, rel string) string { return extension.ContentURI("git", rp.id+"/"+ref+"/"+rel) }
			file := func(rel string) string { return fileURI(filepath.Join(rp.root, rel)) }
			r.api.SetTabs(
				extension.DocumentTab{ID: "index-b", Kind: "diff", Original: vers("HEAD", "b.txt"), Modified: vers("", "b.txt")},
				extension.DocumentTab{ID: "work-a", Kind: "diff", Original: vers("~", "a.txt"), Modified: file("a.txt")},
				extension.DocumentTab{ID: "work-u", Kind: "diff", Original: vers("~", "u.txt"), Modified: file("u.txt")},
				extension.DocumentTab{ID: "work-c", Kind: "diff", Original: vers("~", "c.txt"), Modified: file("c.txt")},
			)
			r.run(rp, tc.cmd, map[string]any{"message": ""})
			got := r.api.ClosedTabs()
			if strings.Join(got, ",") != strings.Join(tc.closed, ",") {
				t.Errorf("closed %v, want %v", got, tc.closed)
			}
		})
	}
	t.Run("the staged stash leaves the work tree alone", func(t *testing.T) {
		r, repo, rp := stashRig(t, rigOptions{settings: map[string]any{"git.closeDiffOnOperation": true}})
		skipBefore235(t, repo)
		file := func(rel string) string { return fileURI(filepath.Join(rp.root, rel)) }
		r.api.SetTabs(
			extension.DocumentTab{ID: "index-b", Kind: "diff", Original: extension.ContentURI("git", rp.id+"/HEAD/b.txt"), Modified: extension.ContentURI("git", rp.id+"//b.txt")},
			extension.DocumentTab{ID: "work-a", Kind: "diff", Original: extension.ContentURI("git", rp.id+"/~/a.txt"), Modified: file("a.txt")},
		)
		r.run(rp, "git.stashStaged", map[string]any{"message": ""})
		if got := r.api.ClosedTabs(); strings.Join(got, ",") != "index-b" {
			t.Errorf("closed %v", got)
		}
	})
	t.Run("nothing closes with the setting off", func(t *testing.T) {
		r, _, rp := stashRig(t, rigOptions{})
		r.api.SetTabs(extension.DocumentTab{ID: "work-a", Kind: "diff", Original: extension.ContentURI("git", rp.id+"/~/a.txt"), Modified: fileURI(filepath.Join(rp.root, "a.txt"))})
		r.run(rp, "git.stash", map[string]any{"message": ""})
		if got := r.api.ClosedTabs(); len(got) != 0 {
			t.Errorf("closed %v", got)
		}
	})
}

func TestStashPromptsToSaveTheUnsavedFiles(t *testing.T) {
	const (
		saveAndStash = "Save All & Stash"
		stashAnyway  = "Stash Anyway"
	)
	tests := []struct {
		name    string
		setting string
		staged  bool // something is staged (b.txt)
		dirty   []string
		prompt  string // "" for no question
		press   string
		saved   []string
		stashed bool
	}{
		{"never asks", "never", true, []string{"a.txt", "b.txt"}, "", "", nil, true},
		{"nothing unsaved", "always", true, nil, "", "", nil, true},
		{"always, with staged changes only the staged file counts", "always", true, []string{"a.txt", "b.txt"},
			"The following file has unsaved changes which won't be included in the stash if you proceed: b.txt.\n\nWould you like to save it before stashing?", saveAndStash, []string{"b.txt"}, true},
		{"always, nothing staged: every unsaved file counts", "always", false, []string{"a.txt", "c.txt"},
			"There are 2 unsaved files.\n\nWould you like to save them before stashing?", saveAndStash, []string{"a.txt", "c.txt"}, true},
		{"always, an unsaved file of another folder is left alone", "always", false, []string{"outside"}, "", "", nil, true},
		{"staged, only the staged files count even with nothing staged", "staged", false, []string{"a.txt"}, "", "", nil, true},
		{"staged, a staged file", "staged", true, []string{"b.txt"},
			"The following file has unsaved changes which won't be included in the stash if you proceed: b.txt.\n\nWould you like to save it before stashing?", stashAnyway, nil, true},
		{"cancel stashes nothing", "always", true, []string{"b.txt"},
			"The following file has unsaved changes which won't be included in the stash if you proceed: b.txt.\n\nWould you like to save it before stashing?", "", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := stashRig(t, rigOptions{settings: map[string]any{"git.promptToSaveFilesBeforeStash": tc.setting}})
			if !tc.staged {
				repo.Git("reset", "-q")
				rp.refresh()
				r.idle(rp)
			}
			var dirty []string
			for _, d := range tc.dirty {
				if d == "outside" {
					dirty = append(dirty, "file:///elsewhere/x.txt")
					continue
				}
				dirty = append(dirty, fileURI(filepath.Join(repo.Dir, d)))
			}
			r.api.SetDirty(dirty...)
			if err := r.api.Run("git.stash", map[string]any{"message": "m"}); err != nil {
				t.Fatal(err)
			}
			if tc.prompt != "" {
				d := r.theOnlyDialog()
				spec := d.Spec()
				if spec.Message != tc.prompt || spec.Type != extension.DialogWarning {
					t.Errorf("question %q (%v), want %q", spec.Message, spec.Type, tc.prompt)
				}
				if got := buttons(d); strings.Join(got, "|") != "Save All & Stash|Stash Anyway|Cancel" {
					t.Errorf("buttons %v", got)
				}
				if tc.press == "" {
					d.Cancel()
				} else {
					d.Press(tc.press)
				}
			}
			if len(tc.saved) > 0 {
				r.api.WaitFor("the save", func() bool { return len(r.api.SavedAll()) > 0 })
			}
			r.api.WaitFor("the stash", func() bool { return !tc.stashed || len(stashSubjects(repo)) == 1 })
			r.idle(rp)
			var saved []string
			for _, call := range r.api.SavedAll() {
				for _, u := range call {
					saved = append(saved, baseName(u))
				}
			}
			if strings.Join(saved, ",") != strings.Join(tc.saved, ",") {
				t.Errorf("saved %v, want %v", saved, tc.saved)
			}
			if got := len(stashSubjects(repo)) == 1; got != tc.stashed {
				t.Errorf("stashed = %v, want %v", got, tc.stashed)
			}
		})
	}
}

func TestStashStopsWhenTheFilesCannotBeSaved(t *testing.T) {
	r, repo, rp := stashRig(t, rigOptions{})
	r.api.SetDirty(fileURI(filepath.Join(repo.Dir, "b.txt")))
	r.api.FailSave(errors.New("ExtDocumentNotSaved", "the document was not saved"))
	if err := r.api.Run("git.stash", map[string]any{"message": "m"}); err != nil {
		t.Fatal(err)
	}
	r.theOnlyDialog().Press("Save All & Stash")
	r.api.WaitFor("the failure", func() bool { return len(notesWith(r, "could not be saved")) > 0 })
	r.idle(rp)
	if stashSubjects(repo) != nil {
		t.Error("a stash was made over files that were not saved")
	}
}

// pickLabels are the labels of the last quick pick, after waiting for it.
func (r *rig) waitPick() *fakeapi.Pick {
	r.t.Helper()
	before := len(r.api.Picks())
	r.api.WaitFor("the picker", func() bool { return len(r.api.Picks()) > before })
	return r.api.LastPick()
}

// twoStashes makes two stashes in the repository: "first" of a.txt and an untracked file, "second" of b.txt.
func twoStashes(t *testing.T, repo *gittest.Repo) {
	t.Helper()
	repo.Git("reset", "-q", "--hard")
	repo.Remove("u.txt")
	repo.Write("a.txt", "first\n")
	repo.Write("u1.txt", "untracked in the first\n")
	repo.Stash("first", true)
	repo.Write("b.txt", "second\n")
	repo.Stash("second", false)
}

func TestStashPickersListTheStashes(t *testing.T) {
	for _, cmd := range []string{"git.stashPop", "git.stashApply", "git.stashDrop", "git.stashView"} {
		t.Run(cmd, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := stashRepo(t, e)
			twoStashes(t, repo)
			r := newRig(t, e, rigOptions{folder: repo.Dir})
			r.settle()
			if err := r.api.Run(cmd, nil); err != nil {
				t.Fatal(err)
			}
			p := r.waitPick()
			if got := strings.Join(p.Labels(), "|"); got != "#0: second|#1: first" {
				t.Errorf("labels %q", got)
			}
			for _, it := range p.Options().Items {
				// the dates of the test repositories are made up, so the age is whatever they say
				if !strings.HasSuffix(it.Description, " \u2022 main") || len(it.Description) < len(" \u2022 main")+3 {
					t.Errorf("description %q", it.Description)
				}
			}
			want := map[string]string{
				"git.stashPop": "Pick a stash to pop", "git.stashApply": "Pick a stash to apply", "git.stashDrop": "Pick a stash to drop", "git.stashView": "Pick a stash to view",
			}[cmd]
			if p.Options().Placeholder != want {
				t.Errorf("placeholder %q, want %q", p.Options().Placeholder, want)
			}
		})
	}
}

func TestStashPickerOfARepositoryWithoutStashes(t *testing.T) {
	r, _, _ := stashRig(t, rigOptions{})
	if err := r.api.Run("git.stashPop", nil); err != nil {
		t.Fatal(err)
	}
	p := r.waitPick()
	if got := p.Labels(); len(got) != 1 || got[0] != "$(info) This repository has no stashes." {
		t.Errorf("labels %q", got)
	}
	p.Choose(0) // the row is a message: choosing it does nothing, and it must not run a stash
}

func TestStashPopAndApplyFromThePicker(t *testing.T) {
	tests := []struct {
		name   string
		cmd    string
		choose int
		left   int    // stashes left
		a, b   string // the files afterwards
	}{
		{"pop the newest", "git.stashPop", 0, 1, "a.txt\none\ntwo\nthree\nfour\n", "second\n"},
		{"pop the older", "git.stashPop", 1, 1, "first\n", "b.txt\none\ntwo\nthree\nfour\n"},
		{"apply the newest", "git.stashApply", 0, 2, "a.txt\none\ntwo\nthree\nfour\n", "second\n"},
		{"apply the older", "git.stashApply", 1, 2, "first\n", "b.txt\none\ntwo\nthree\nfour\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := stashRepo(t, e)
			twoStashes(t, repo)
			r := newRig(t, e, rigOptions{folder: repo.Dir})
			r.settle()
			rp := r.repo(repo.Dir)
			if err := r.api.Run(tc.cmd, nil); err != nil {
				t.Fatal(err)
			}
			r.waitPick().Choose(tc.choose)
			r.idle(rp)
			if got := len(stashSubjects(repo)); got != tc.left {
				t.Errorf("%d stashes left, want %d", got, tc.left)
			}
			if repo.Read("a.txt") != tc.a || repo.Read("b.txt") != tc.b {
				t.Errorf("a.txt %q b.txt %q", repo.Read("a.txt"), repo.Read("b.txt"))
			}
			if tc.choose == 1 && !exists(repo, "u1.txt") && tc.name != "" {
				t.Error("the untracked file of the stash did not come back")
			}
		})
	}
}

func TestStashLatestCommands(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		left int
	}{
		{"pop the latest", "git.stashPopLatest", 1},
		{"apply the latest", "git.stashApplyLatest", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := stashRepo(t, e)
			twoStashes(t, repo)
			r := newRig(t, e, rigOptions{folder: repo.Dir})
			r.settle()
			rp := r.repo(repo.Dir)
			if err := r.api.Run(tc.cmd, nil); err != nil {
				t.Fatal(err)
			}
			r.api.WaitFor("the stash", func() bool { return rp.own == 0 && readOr(repo, "b.txt") == "second\n" })
			r.idle(rp)
			if got := len(stashSubjects(repo)); got != tc.left {
				t.Errorf("%d stashes left, want %d", got, tc.left)
			}
		})
	}
	t.Run("none says so", func(t *testing.T) {
		for _, cmd := range []string{"git.stashPopLatest", "git.stashApplyLatest", "git.stashDropAll"} {
			r, _, _ := stashRig(t, rigOptions{})
			if err := r.api.Run(cmd, nil); err != nil {
				t.Fatal(err)
			}
			r.api.WaitFor("the message", func() bool { return len(notesWith(r, "There are no stashes in the repository.")) == 1 })
		}
	})
}

func TestStashPopAndApplyNameTheStashByIndex(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := stashRepo(t, e)
	twoStashes(t, repo)
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	r.run(rp, "git.stashApply", map[string]any{"index": 1})
	r.api.WaitFor("the apply", func() bool { return readOr(repo, "a.txt") == "first\n" })
	r.idle(rp)
	if len(r.api.Picks()) != 0 {
		t.Error("a picker was shown for a stash that was named")
	}
	if err := r.api.Run("git.stashPop", map[string]any{"index": 7}); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the message", func() bool { return len(notesWith(r, "There is no stash #7 in the repository.")) == 1 })
}

func TestStashDropAsksFirst(t *testing.T) {
	for _, tc := range []struct {
		name   string
		press  string
		left   []string
		picked int
	}{
		{"yes drops it", "Yes", []string{"On main: second"}, 1},
		{"cancel keeps it", "", []string{"On main: second", "On main: first"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := stashRepo(t, e)
			twoStashes(t, repo)
			r := newRig(t, e, rigOptions{folder: repo.Dir})
			r.settle()
			rp := r.repo(repo.Dir)
			if err := r.api.Run("git.stashDrop", nil); err != nil {
				t.Fatal(err)
			}
			r.waitPick().Choose(tc.picked)
			d := r.theOnlyDialog()
			if d.Spec().Message != "Are you sure you want to drop the stash: first?" || d.Spec().Type != extension.DialogWarning {
				t.Errorf("question %q", d.Spec().Message)
			}
			if got := buttons(d); strings.Join(got, "|") != "Yes|Cancel" {
				t.Errorf("buttons %v", got)
			}
			if tc.press == "" {
				d.Cancel()
			} else {
				d.Press(tc.press)
			}
			r.idle(rp)
			got := stashSubjects(repo)
			if len(got) != len(tc.left) {
				t.Fatalf("stashes %q, want %q", got, tc.left)
			}
			for i := range got {
				if !strings.HasSuffix(got[i], tc.left[i]) {
					t.Errorf("stash %d = %q, want %q", i, got[i], tc.left[i])
				}
			}
		})
	}
}

func TestStashDropAllAsksFirst(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stashes  int
		question string
		press    string
		left     int
	}{
		{"one stash", 1, "Are you sure you want to drop ALL stashes? There is 1 stash that will be subject to pruning, and MAY BE IMPOSSIBLE TO RECOVER.", "Yes", 0},
		{"three stashes", 3, "Are you sure you want to drop ALL stashes? There are 3 stashes that will be subject to pruning, and MAY BE IMPOSSIBLE TO RECOVER.", "Yes", 0},
		{"cancel", 2, "Are you sure you want to drop ALL stashes? There are 2 stashes that will be subject to pruning, and MAY BE IMPOSSIBLE TO RECOVER.", "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := e.Init("many")
			repo.Write("a.txt", "a\n")
			repo.Commit("base")
			for i := 0; i < tc.stashes; i++ {
				repo.Write("a.txt", strings.Repeat("x", i+1)+"\n")
				repo.Stash("s", false)
			}
			r := newRig(t, e, rigOptions{folder: repo.Dir})
			r.settle()
			rp := r.repo(repo.Dir)
			if err := r.api.Run("git.stashDropAll", nil); err != nil {
				t.Fatal(err)
			}
			d := r.theOnlyDialog()
			if d.Spec().Message != tc.question {
				t.Errorf("question %q", d.Spec().Message)
			}
			if tc.press == "" {
				d.Cancel()
			} else {
				d.Press(tc.press)
			}
			r.idle(rp)
			if got := len(stashSubjects(repo)); got != tc.left {
				t.Errorf("%d stashes left, want %d", got, tc.left)
			}
		})
	}
}

func TestStashConflictKeepsTheStashAndShowsTheMergeChanges(t *testing.T) {
	for _, cmd := range []string{"git.stashPop", "git.stashApply"} {
		t.Run(cmd, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := e.Init("conflict")
			repo.Write("a.txt", "one\ntwo\nthree\n")
			repo.Commit("base")
			repo.Write("a.txt", "stashed\ntwo\nthree\n")
			repo.Stash("conflicting", false)
			repo.Write("a.txt", "committed\ntwo\nthree\n")
			repo.Commit("mine", "a.txt")
			r := newRig(t, e, rigOptions{folder: repo.Dir})
			r.settle()
			rp := r.repo(repo.Dir)
			r.run(rp, cmd, map[string]any{"index": 0})
			r.api.WaitFor("the conflict", func() bool { return len(r.paths(repo.Dir, "merge")) == 1 })
			r.idle(rp)
			n := notesWith(r, "There are merge conflicts while applying the stash. Please resolve them before committing your changes.")
			if len(n) != 1 || r.api.Notes()[n[0]].Severity != "warn" {
				t.Fatalf("notes %+v", r.api.Notes())
			}
			if len(notesWith(r, "Git: ")) != 0 {
				t.Error("the generic failure was shown as well")
			}
			if got := len(stashSubjects(repo)); got != 1 {
				t.Errorf("%d stashes, the stash must be kept", got)
			}
			if got := r.paths(repo.Dir, "merge"); len(got) != 1 || got[0] != "a.txt" {
				t.Errorf("merge changes %v", got)
			}
		})
	}
}

func TestStashApplyOverChangesItWouldOverwrite(t *testing.T) {
	r, repo, rp := stashRig(t, rigOptions{})
	repo.Git("reset", "-q", "--hard")
	repo.Remove("u.txt")
	repo.Write("a.txt", "stashed\n")
	repo.Stash("s", false)
	repo.Write("a.txt", "dirty\n")
	r.run(rp, "git.stashPop", map[string]any{"index": 0})
	r.api.WaitFor("the failure", func() bool { return len(notesWith(r, "Git: ")) == 1 })
	if len(stashSubjects(repo)) != 1 || repo.Read("a.txt") != "dirty\n" {
		t.Error("the stash was applied over changes")
	}
}

func TestFromNowShort(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		ago  time.Duration
		want string
	}{
		{0, "now"}, {29 * time.Second, "now"}, {30 * time.Second, "30 secs"}, {59 * time.Second, "59 secs"}, {time.Minute, "1 min"},
		{5 * time.Minute, "5 mins"}, {time.Hour, "1 hr"}, {23*time.Hour + 59*time.Minute, "23 hrs"}, {24 * time.Hour, "1 day"},
		{6 * 24 * time.Hour, "6 days"}, {7 * 24 * time.Hour, "1 wk"}, {15 * 24 * time.Hour, "2 wks"}, {30 * 24 * time.Hour, "1 mo"},
		{200 * 24 * time.Hour, "6 mos"}, {365 * 24 * time.Hour, "1 yr"}, {800 * 24 * time.Hour, "2 yrs"}, {-90 * time.Second, "in 1 min"},
	}
	for _, tc := range tests {
		if got := fromNowShort(now.Add(-tc.ago), now); got != tc.want {
			t.Errorf("%v ago = %q, want %q", tc.ago, got, tc.want)
		}
	}
}

func TestStashItemDescription(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		s    git.Stash
		want string
	}{
		{"date and branch", git.Stash{CommitDate: now.Add(-2 * time.Hour), Branch: "main"}, "2 hrs • main"},
		{"date only", git.Stash{CommitDate: now.Add(-3 * 24 * time.Hour)}, "3 days"},
		{"branch only", git.Stash{Branch: "feature/x"}, "feature/x"},
		{"neither", git.Stash{}, ""},
	}
	for _, tc := range tests {
		if got := stashItemDescription(tc.s, now); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := stashItemLabel(git.Stash{Index: 3, Description: "WIP (abc Subject)"}); got != "#3: WIP (abc Subject)" {
		t.Errorf("label %q", got)
	}
}

// stashEditorRig makes two stashes and opens the editor of the older one.
func stashEditorRig(t *testing.T) (*rig, *gittest.Repo, *repository, extension.MultiDiffSpec) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := stashRepo(t, e)
	twoStashes(t, repo)
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	if err := r.api.Run("git.stashView", map[string]any{"index": 1}); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the editor", func() bool { return len(r.api.OpenedMultiDiffs()) == 1 })
	return r, repo, rp, r.api.OpenedMultiDiffs()[0]
}

func TestStashViewOpensTheChangesOfTheStash(t *testing.T) {
	r, repo, rp, spec := stashEditorRig(t)
	hash := strings.TrimSpace(repo.Git("rev-parse", "stash@{1}"))
	if spec.Kind != StashMultiDiffKind || spec.Key != rp.id+"/"+hash || spec.Title != "Git Stash #1: first" || spec.Preview || spec.PreserveFocus {
		t.Errorf("spec %+v", spec)
	}
	files := r.mustStashFiles(spec.Key)
	untracked := strings.TrimSpace(repo.Git("rev-parse", "stash@{1}^3"))
	got := lines(rp, files)
	// the original of a tracked file is the commit the stash was made on, by its hash; an untracked file is read from its own commit
	head := strings.TrimSpace(repo.Git("rev-parse", "stash@{1}^1"))
	equalStrings(t, got, []string{
		"a.txt M " + head + "/a.txt > " + hash + "/a.txt",
		"u1.txt U - > " + untracked + "/u1.txt",
	})
	for _, f := range files {
		if f.Context["scmProvider"] != "git" || f.Context["scmResourceGroup"] != "stash" {
			t.Errorf("context of %s: %v", f.Path, f.Context)
		}
	}
	if files[1].Color != "gitDecoration.untrackedResourceForeground" || files[1].Tooltip != "Untracked" || files[1].File == "" {
		t.Errorf("the untracked file: %+v", files[1])
	}
	// the documents of the stash are readable through the provider of the versions
	text, err := r.api.ContentText(context.Background(), files[1].Modified)
	if err != nil || text != "untracked in the first\n" {
		t.Errorf("the untracked file reads %q, %v", text, err)
	}
	text, err = r.api.ContentText(context.Background(), files[0].Original)
	if err != nil || text != "a.txt\none\ntwo\nthree\nfour\n" {
		t.Errorf("the base of a.txt reads %q, %v", text, err)
	}
}

func TestStashViewOfAStashWithRenamesDeletionsAndAdditions(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := stashRepo(t, e)
	repo.Git("reset", "-q", "--hard")
	repo.Remove("u.txt")
	repo.Write("old.txt", "old.txt\none\ntwo\nthree\nfour\nfive\n")
	repo.Git("mv", "old.txt", "new.txt")
	repo.Remove("c.txt")
	repo.Write("added.txt", "added\n")
	repo.Git("add", "added.txt")
	repo.Write("dir/d.txt", "changed\n")
	repo.Write("n/u.txt", "n\n")
	repo.Stash("mixed", true)
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	r.run(rp, "git.stashView", map[string]any{"index": 0})
	r.api.WaitFor("the editor", func() bool { return len(r.api.OpenedMultiDiffs()) == 1 })
	files := r.mustStashFiles(r.api.OpenedMultiDiffs()[0].Key)
	var got []string
	for _, f := range files {
		got = append(got, f.Letter+" "+f.Path+" "+boolWord(f.Original != "", "orig")+" "+boolWord(f.Modified != "", "mod"))
	}
	equalStrings(t, got, []string{
		"A added.txt  mod", "D c.txt orig ", "M dir/d.txt orig mod", "R new.txt orig mod", "U n/u.txt  mod",
	})
	if !strings.HasSuffix(files[3].Original, "/old.txt") {
		t.Errorf("a rename starts from the old path: %s", files[3].Original)
	}
}

func boolWord(b bool, w string) string {
	if b {
		return w
	}
	return ""
}

func (r *rig) mustStashFiles(key string) []extension.MultiDiffResource {
	r.t.Helper()
	files, err := r.api.MultiDiffFiles(context.Background(), StashMultiDiffKind, key)
	if err != nil {
		r.t.Fatalf("%s: %v", key, err)
	}
	return files
}

func TestStashViewWithNothingInTheStash(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := stashRepo(t, e)
	repo.Git("reset", "-q", "--hard")
	repo.Remove("u.txt")
	// git makes no stash of a clean tree, but a stash is only a commit on top of HEAD: this one has HEAD's own tree
	index := strings.TrimSpace(repo.Git("commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "index"))
	empty := strings.TrimSpace(repo.Git("commit-tree", "HEAD^{tree}", "-p", "HEAD", "-p", index, "-m", "empty"))
	repo.Git("stash", "store", "-m", "nothing in it", empty)
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	if err := r.api.Run("git.stashView", map[string]any{"index": 0}); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the message", func() bool { return len(notesWith(r, "The stash has no changes.")) == 1 })
	if len(r.api.OpenedMultiDiffs()) != 0 {
		t.Error("an editor was opened")
	}
}

func TestTheStashEditorStaysAfterThePopAndRefusesAGoneRepository(t *testing.T) {
	r, repo, rp, spec := stashEditorRig(t)
	r.run(rp, "git.stashPop", map[string]any{"index": 1})
	r.api.WaitFor("the pop", func() bool { return len(stashSubjects(repo)) == 1 })
	r.idle(rp)
	if files := r.mustStashFiles(spec.Key); len(files) != 2 {
		t.Errorf("after the pop the editor shows %d files", len(files))
	}
	// the repository is closed: the editor is gone
	reg := r.api.MultiDiffProviderFor(StashMultiDiffKind)
	r.x.model.close(rp)
	r.api.Pump()
	changes := reg.MultiDiffChanges()
	if len(changes) == 0 || !contains(changes[len(changes)-1], spec.Key) {
		t.Errorf("the window was not told: %v", changes)
	}
	_, err := r.api.MultiDiffFiles(context.Background(), StashMultiDiffKind, spec.Key)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("gitscm.stashResources.MultiDiffGone") {
		t.Errorf("error %v", err)
	}
}

func TestTheStashKeyIsChecked(t *testing.T) {
	r, _, _ := stashRig(t, rigOptions{})
	for _, key := range []string{"", "nohash", "/abc", "abc/"} {
		_, err := r.api.MultiDiffFiles(context.Background(), StashMultiDiffKind, key)
		if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("gitscm.stashResources.GitStashKeyInvalid") {
			t.Errorf("%q: %v", key, err)
		}
	}
	_, err := r.api.MultiDiffFiles(context.Background(), StashMultiDiffKind, "unknownrepo/"+strings.Repeat("a", 40))
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("gitscm.stashResources.MultiDiffGone") {
		t.Errorf("an unknown repository: %v", err)
	}
	rp := r.repo(r.folder)
	_, err = r.api.MultiDiffFiles(context.Background(), StashMultiDiffKind, rp.id+"/"+strings.Repeat("a", 40))
	if err == nil {
		t.Error("a commit git does not have was read")
	}
	_, err = r.api.MultiDiffFiles(context.Background(), StashMultiDiffKind, rp.id+"/--output=x")
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("Repo.StashChangesAt.GitStashHashInvalid") {
		t.Errorf("a key that is an option: %v", err)
	}
}

// editorArgs are the arguments the editor title gives a command of a stash editor.
func editorArgs(spec extension.MultiDiffSpec) map[string]any {
	return map[string]any{"groupId": float64(1), "kind": spec.Kind, "key": spec.Key}
}

func TestTheTitleActionsOfTheStashEditor(t *testing.T) {
	t.Run("apply keeps the stash", func(t *testing.T) {
		r, repo, rp, spec := stashEditorRig(t)
		r.run(rp, "git.stashApplyEditor", editorArgs(spec))
		r.api.WaitFor("the apply", func() bool { return readOr(repo, "a.txt") == "first\n" })
		r.idle(rp)
		if len(stashSubjects(repo)) != 2 || !exists(repo, "u1.txt") {
			t.Errorf("stashes %q", stashSubjects(repo))
		}
	})
	t.Run("pop drops it", func(t *testing.T) {
		r, repo, rp, spec := stashEditorRig(t)
		r.run(rp, "git.stashPopEditor", editorArgs(spec))
		r.api.WaitFor("the pop", func() bool { return len(stashSubjects(repo)) == 1 })
		r.idle(rp)
		if repo.Read("a.txt") != "first\n" {
			t.Errorf("a.txt %q", repo.Read("a.txt"))
		}
	})
	t.Run("the stash is found by its hash even when its index moved", func(t *testing.T) {
		r, repo, rp, spec := stashEditorRig(t)
		repo.Git("reset", "-q", "--hard")
		repo.Write("c.txt", "newest\n")
		repo.Stash("newest", false) // "first" is stash@{2} now
		r.run(rp, "git.stashApplyEditor", editorArgs(spec))
		r.api.WaitFor("the apply", func() bool { return readOr(repo, "a.txt") == "first\n" })
		r.idle(rp)
		if repo.Read("c.txt") == "newest\n" {
			t.Error("the newest stash was applied")
		}
	})
	t.Run("drop asks, drops it and closes the editor", func(t *testing.T) {
		r, repo, rp, spec := stashEditorRig(t)
		reg := r.api.MultiDiffProviderFor(StashMultiDiffKind)
		before := len(reg.MultiDiffChanges())
		if err := r.api.Run("git.stashDropEditor", editorArgs(spec)); err != nil {
			t.Fatal(err)
		}
		d := r.theOnlyDialog()
		if d.Spec().Message != "Are you sure you want to drop the stash: first?" {
			t.Errorf("question %q", d.Spec().Message)
		}
		d.Press("Yes")
		r.idle(rp)
		if got := stashSubjects(repo); len(got) != 1 || !strings.HasSuffix(got[0], "second") {
			t.Errorf("stashes %q", got)
		}
		changes := reg.MultiDiffChanges()
		if len(changes) != before+1 || !contains(changes[len(changes)-1], spec.Key) {
			t.Errorf("the editor was not told to ask again: %v", changes)
		}
		_, err := r.api.MultiDiffFiles(context.Background(), StashMultiDiffKind, spec.Key)
		if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("gitscm.stashResources.MultiDiffGone") {
			t.Errorf("after the drop: %v", err)
		}
	})
	t.Run("drop that is not confirmed keeps the editor", func(t *testing.T) {
		r, repo, _, spec := stashEditorRig(t)
		if err := r.api.Run("git.stashDropEditor", editorArgs(spec)); err != nil {
			t.Fatal(err)
		}
		r.theOnlyDialog().Cancel()
		r.api.Pump()
		if len(stashSubjects(repo)) != 2 {
			t.Error("the stash was dropped")
		}
		if _, err := r.api.MultiDiffFiles(context.Background(), StashMultiDiffKind, spec.Key); err != nil {
			t.Errorf("the editor is gone: %v", err)
		}
	})
	t.Run("a stash that is not in the list any more", func(t *testing.T) {
		r, repo, rp, spec := stashEditorRig(t)
		repo.Git("stash", "clear")
		r.run(rp, "git.stashApplyEditor", editorArgs(spec))
		r.api.WaitFor("the message", func() bool { return len(notesWith(r, "This stash is not in the stash list any more.")) == 1 })
	})
	t.Run("the arguments of another editor do nothing", func(t *testing.T) {
		r, repo, rp, _ := stashEditorRig(t)
		for _, args := range []any{nil, map[string]any{"kind": "git.changes", "key": "x/y"}, map[string]any{"kind": StashMultiDiffKind, "key": "nokey"}} {
			r.run(rp, "git.stashPopEditor", args)
		}
		if len(stashSubjects(repo)) != 2 {
			t.Error("a stash was popped")
		}
	})
	t.Run("a repository that is closed", func(t *testing.T) {
		r, _, rp, spec := stashEditorRig(t)
		r.x.model.close(rp)
		r.run(rp, "git.stashApplyEditor", editorArgs(spec))
		if len(notesWith(r, "The repository of this stash is not open.")) != 1 {
			t.Errorf("notes %+v", r.api.Notes())
		}
	})
}

func TestTheStashMenus(t *testing.T) {
	m := (&gitscm{}).Manifest()
	if len(m.Contributes.Submenus) != 1 || m.Contributes.Submenus[0].ID != "git.stash" || m.Contributes.Submenus[0].Label != "Stash" {
		t.Errorf("submenus %+v", m.Contributes.Submenus)
	}
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	var sub, title []string
	for _, it := range m.Contributes.Menus {
		switch it.Menu {
		case "git.stash":
			sub = append(sub, it.Group+" "+it.Command+" "+it.When)
		case extension.MenuEditorTitle:
			if strings.HasPrefix(it.Command, "git.stash") {
				title = append(title, it.Group+" "+it.Command+" "+it.When)
			}
		}
	}
	equalStrings(t, sub, []string{
		"1_stash git.stash ", "1_stash git.stashIncludeUntracked ", "1_stash git.stashStaged gitVersion235", "2_apply git.stashApplyLatest ", "2_apply git.stashApply ",
		"3_pop git.stashPopLatest ", "3_pop git.stashPop ", "4_drop git.stashDrop ", "4_drop git.stashDropAll ", "5_preview git.stashView ",
	})
	equalStrings(t, title, []string{
		"navigation git.stashApplyEditor config.git.enabled && !git.missing && resourceScheme == git-stash",
		"navigation git.stashPopEditor config.git.enabled && !git.missing && resourceScheme == git-stash",
		"navigation git.stashDropEditor config.git.enabled && !git.missing && resourceScheme == git-stash",
	})
	var in []string
	for _, it := range m.Contributes.Menus {
		if it.Submenu == "git.stash" {
			in = append(in, it.Menu+" "+it.Group)
		}
	}
	equalStrings(t, in, []string{extension.MenuSCMTitle + " 2_main", extension.MenuSCMRepository + " 2_main"})
}

// A saved stash editor asks for its files when the extension starts, before the repository is found: the answer waits for it.
func TestTheStashEditorWaitsForARepositoryThatIsStillComing(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := stashRepo(t, e)
	repo.Git("stash", "push", "-m", "saved")
	hash := strings.TrimSpace(repo.Git("rev-parse", "stash@{0}"))
	r := newRig(t, e, rigOptions{folder: repo.Dir, noStart: true})
	if err := r.x.Activate(context.Background(), r.api); err != nil {
		t.Fatal(err)
	}
	type answer struct {
		files []extension.MultiDiffResource
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		files, err := r.api.MultiDiffFiles(context.Background(), StashMultiDiffKind, repoID(repo.Dir)+"/"+hash)
		done <- answer{files, err}
	}()
	r.settle()
	var a answer
	r.api.WaitFor("the answer", func() bool {
		select {
		case a = <-done:
			return true
		default:
			return false
		}
	})
	if a.err != nil || len(a.files) == 0 {
		t.Errorf("files %v, error %v", a.files, a.err)
	}
}
