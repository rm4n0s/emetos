package gitscm

import (
	"os"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// Merge, rebase, cherry-pick and the conflicts (step 24.14) with the real git: the commands, the states they leave, the box and the
// button in each, the Merge Changes group, and the merge editor's completion.

const loaderPath = "src/loader.go"

// conflictRig makes a repository on "ours", with the branches "ours" and "theirs" changing the same two lines of src/loader.go, and
// the extension over it. When rigOptions.settings is nil the defaults of the extension apply.
func conflictRig(t *testing.T, o rigOptions) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.ConflictingBranches()
	o.folder = repo.Dir
	o.settings = withoutEditor(o.settings)
	r := newRig(t, e, o)
	r.settle()
	return r, repo, r.repo(repo.Dir)
}

// withoutEditor turns the editor of the commit message off unless the test says otherwise: a commit with an empty box would wait for a tab.
func withoutEditor(settings map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range settings {
		out[k] = v
	}
	if _, ok := out["git.useEditorAsCommitInput"]; !ok {
		out["git.useEditorAsCommitInput"] = false
	}
	return out
}

// resolveFile writes the file without its markers and stages it, as the user does with the merge editor.
func resolveFile(repo *gittest.Repo, rel string) {
	repo.Write(rel, gittest.LoaderSource(0)+"// resolved\n")
	repo.Git("add", rel)
}

func hasNote(r *rig, part string) bool { return len(notesWith(r, part)) > 0 }

func TestMergeCommand(t *testing.T) {
	t.Run("the picker lists the other refs", func(t *testing.T) {
		r, _, rp := conflictRig(t, rigOptions{})
		r.start(rp, "git.merge", nil)
		p := r.waitPick()
		if got := p.Options().Placeholder; got != "Select a branch or tag to merge from" {
			t.Errorf("placeholder = %q", got)
		}
		labels := pickLabels(p)
		for _, want := range []string{"-- branches --", "$(git-branch) theirs", "$(git-branch) main"} {
			if indexOfLabel(labels, want) < 0 {
				t.Errorf("the picker has no %q: %v", want, labels)
			}
		}
		if indexOfLabel(labels, "$(git-branch) ours") >= 0 {
			t.Errorf("the current branch is listed: %v", labels)
		}
		p.Close()
		r.idle(rp)
	})
	t.Run("a merge with conflicts stops and says so", func(t *testing.T) {
		r, repo, rp := conflictRig(t, rigOptions{})
		r.start(rp, "git.merge", nil)
		p := r.waitPick()
		p.Choose(indexOf(t, p, "$(git-branch) theirs"))
		r.idle(rp)
		if !hasNote(r, "There are merge conflicts. Please resolve them before committing your changes.") {
			t.Errorf("notes: %+v", r.api.Notes())
		}
		if _, err := os.Stat(repo.Path(".git/MERGE_HEAD")); err != nil {
			t.Error("the merge is not in progress")
		}
		if got := r.letters(repo.Dir, groupMerge); len(got) != 1 || got[0] != loaderPath+" !" {
			t.Errorf("merge group = %v", got)
		}
		if r.api.ContextKey("gitMergeInProgress") != true {
			t.Error("gitMergeInProgress is not set")
		}
		in := inputOf(r, rp)
		if got := in.Placeholder(); got != "Message ({0} to continue the merge)" {
			t.Errorf("placeholder = %q", got)
		}
		if got := in.Value(); !strings.HasPrefix(got, "Merge branch 'theirs'") {
			t.Errorf("the box holds %q", got)
		}
		b := r.source(rp.root).ActionButton()
		if b == nil || b.Enabled || b.Command.Title != "$(check) Continue" || b.Command.Tooltip != "Continue Merge" {
			t.Errorf("button = %+v", b)
		}
		mc, _ := r.api.ContextKey("git.mergeChanges").([]any)
		if len(mc) != 1 || mc[0] != fileURI(repo.Path(loaderPath)) {
			t.Errorf("git.mergeChanges = %v", mc)
		}
		// the conflicts are resolved and staged: Continue is on, and it makes the merge commit with the message of the box
		resolveFile(repo, loaderPath)
		r.refresh(rp)
		if b := r.source(rp.root).ActionButton(); b == nil || !b.Enabled || b.Command.Title != "$(check) Continue" {
			t.Errorf("button after the resolution = %+v", b)
		}
		r.commitBox(rp)
		if _, err := os.Stat(repo.Path(".git/MERGE_HEAD")); err == nil {
			t.Error("the merge is still in progress")
		}
		if n := len(strings.Fields(repo.Git("log", "-1", "--format=%P"))); n != 2 {
			t.Errorf("HEAD has %d parents", n)
		}
		if got := repo.Git("log", "-1", "--format=%s"); !strings.HasPrefix(got, "Merge branch 'theirs'") {
			t.Errorf("the merge commit says %q", got)
		}
		if got := in.Placeholder(); got != `Message ({0} to commit on "ours")` {
			t.Errorf("placeholder after the merge = %q", got)
		}
		if r.api.ContextKey("gitMergeInProgress") != false {
			t.Error("gitMergeInProgress is still set")
		}
	})
	t.Run("Continue with an empty box uses the message of the merge", func(t *testing.T) {
		r, repo, rp := conflictRig(t, rigOptions{})
		r.run(rp, "git.merge", map[string]any{"ref": "theirs"})
		resolveFile(repo, loaderPath)
		r.refresh(rp)
		inputOf(r, rp).SetValue("")
		r.commitBox(rp)
		if _, err := os.Stat(repo.Path(".git/MERGE_HEAD")); err == nil {
			t.Fatal("the merge is still in progress")
		}
		if got := repo.Git("log", "-1", "--format=%s"); !strings.HasPrefix(got, "Merge branch 'theirs'") || len(r.api.OpenDialogs()) != 0 || len(r.api.Inputs()) != 0 {
			t.Errorf("subject %q, dialogs %d, boxes %d", got, len(r.api.OpenDialogs()), len(r.api.Inputs()))
		}
	})
	t.Run("abort puts the work tree back", func(t *testing.T) {
		r, repo, rp := conflictRig(t, rigOptions{})
		before := repo.Head()
		r.run(rp, "git.merge", map[string]any{"ref": "theirs"})
		r.run(rp, "git.mergeAbort", nil)
		if _, err := os.Stat(repo.Path(".git/MERGE_HEAD")); err == nil {
			t.Error("the merge is still in progress")
		}
		if repo.Head() != before || len(r.letters(repo.Dir, groupMerge)) != 0 {
			t.Errorf("HEAD %s, merge group %v", repo.Head(), r.letters(repo.Dir, groupMerge))
		}
		if r.api.ContextKey("gitMergeInProgress") != false {
			t.Error("gitMergeInProgress is still set")
		}
	})
	t.Run("a merge without conflicts makes the commit", func(t *testing.T) {
		r, repo, rp := conflictRig(t, rigOptions{})
		repo.Git("branch", "-q", "other", "main")
		repo.Git("checkout", "-q", "other")
		repo.Write("o.txt", "other\n")
		repo.Commit("Add o")
		repo.Git("checkout", "-q", "ours")
		r.refresh(rp)
		r.run(rp, "git.merge", map[string]any{"ref": "other"})
		if _, err := os.Stat(repo.Path("o.txt")); err != nil {
			t.Error("o.txt was not merged")
		}
		if len(r.api.Notes()) != 0 {
			t.Errorf("notes: %+v", r.api.Notes())
		}
	})
	t.Run("a ref that does not exist fails with the message of git", func(t *testing.T) {
		r, _, rp := conflictRig(t, rigOptions{})
		r.run(rp, "git.merge", map[string]any{"ref": "nope"})
		if !hasNote(r, "git merge failed") {
			t.Errorf("notes: %+v", r.api.Notes())
		}
	})
}

func indexOfLabel(labels []string, want string) int {
	for i, l := range labels {
		if l == want {
			return i
		}
	}
	return -1
}

// rebaseRig is conflictRig with two commits on "theirs" and the extension over the work tree on "theirs", ready to rebase onto "ours".
func rebaseRig(t *testing.T, o rigOptions) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	repo.ConflictingBranches()
	repo.Git("checkout", "-q", "theirs")
	repo.Write("extra.txt", "extra\n")
	repo.Commit("Add extra after the retries")
	o.folder = repo.Dir
	o.settings = withoutEditor(o.settings)
	r := newRig(t, e, o)
	r.settle()
	return r, repo, r.repo(repo.Dir)
}

func TestRebaseCommand(t *testing.T) {
	t.Run("the picker lists the branches and the upstream first", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("repo")
		repo.ConflictingBranches()
		bare := repo.AddRemote("origin")
		_ = bare
		repo.Git("push", "-q", "-u", "origin", "ours")
		repo.Git("fetch", "-q", "origin")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		rp := r.repo(repo.Dir)
		r.start(rp, "git.rebase", nil)
		p := r.waitPick()
		if got := p.Options().Placeholder; got != "Select a branch to rebase onto" {
			t.Errorf("placeholder = %q", got)
		}
		items := p.Options().Items
		if len(items) == 0 || items[0].Label != "$(cloud) origin/ours" || items[0].Description != "(upstream)" {
			t.Errorf("first item = %+v", items)
		}
		labels := pickLabels(p)
		if indexOfLabel(labels, "$(git-branch) theirs") < 0 || indexOfLabel(labels, "$(git-branch) ours") >= 0 {
			t.Errorf("labels = %v", labels)
		}
		p.Close()
		r.idle(rp)
	})
	t.Run("a conflict stops the rebase, the box shows the message of the commit", func(t *testing.T) {
		r, repo, rp := rebaseRig(t, rigOptions{})
		r.start(rp, "git.rebase", nil)
		p := r.waitPick()
		p.Choose(indexOf(t, p, "$(git-branch) ours"))
		r.idle(rp)
		if !hasNote(r, "There are merge conflicts") {
			t.Errorf("notes: %+v", r.api.Notes())
		}
		if r.api.ContextKey("gitRebaseInProgress") != true {
			t.Error("gitRebaseInProgress is not set")
		}
		items := r.source(rp.root).StatusBarCommands()
		if len(items) == 0 || !strings.HasPrefix(items[0].Text, "$(git-branch-conflicts) ") || !strings.Contains(items[0].Text, "(Rebasing)") {
			t.Fatalf("branch item = %+v", items)
		}
		in := inputOf(r, rp)
		if got := in.Placeholder(); got != "Message ({0} to continue the rebase)" {
			t.Errorf("placeholder = %q", got)
		}
		if got := in.Value(); got != "Retry five times" {
			t.Errorf("the box holds %q", got)
		}
		if v := in.Validate(in.Value()); v != nil {
			t.Errorf("the message of the commit is warned about: %+v", v)
		}
		v := in.Validate("Another message")
		if v == nil || v.Level != extension.ValidationWarning || !strings.HasPrefix(v.Message, "It's not possible to change the commit message in the middle of a rebase.") {
			t.Errorf("validation = %+v", v)
		}
		b := r.source(rp.root).ActionButton()
		if b == nil || b.Enabled || b.Command.Tooltip != "Continue Rebase" {
			t.Errorf("button = %+v", b)
		}
		// resolved: Continue goes on to the next commit and the rebase ends; the box is empty again
		resolveFile(repo, loaderPath)
		r.refresh(rp)
		if b := r.source(rp.root).ActionButton(); b == nil || !b.Enabled {
			t.Errorf("button after the resolution = %+v", b)
		}
		in.SetValue("A message the rebase ignores")
		r.commitBox(rp)
		if _, err := os.Stat(repo.Path(".git/rebase-merge")); err == nil {
			t.Error("the rebase is still in progress")
		}
		if got := repo.Git("log", "--format=%s", "ours..theirs"); got != "Add extra after the retries\nRetry five times" {
			t.Errorf("theirs has the commits %q", got)
		}
		if in.Value() != "" || in.Placeholder() != `Message ({0} to commit on "theirs")` {
			t.Errorf("the box holds %q with the placeholder %q", in.Value(), in.Placeholder())
		}
		if v := in.Validate("x"); v != nil {
			t.Errorf("the box is warned about after the rebase: %+v", v)
		}
	})
	t.Run("a second conflict while continuing is a warning too", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("repo")
		repo.ConflictingBranches()
		repo.Git("checkout", "-q", "theirs")
		repo.Write(loaderPath, strings.Replace(repo.Read(loaderPath), "retries := 5 // theirs", "retries := 6 // theirs again", 1))
		repo.Commit("Retry six times")
		r := newRig(t, e, rigOptions{folder: repo.Dir, settings: withoutEditor(nil)})
		r.settle()
		rp := r.repo(repo.Dir)
		r.run(rp, "git.rebase", map[string]any{"ref": "ours"})
		resolveFile(repo, loaderPath)
		r.refresh(rp)
		r.commitBox(rp)
		if got := len(notesWith(r, "There are merge conflicts")); got != 2 {
			t.Errorf("%d warnings: %+v", got, r.api.Notes())
		}
		if got := inputOf(r, rp).Value(); got != "Retry six times" {
			t.Errorf("the box holds %q", got)
		}
		if r.api.ContextKey("gitRebaseInProgress") != true || hasNote(r, "failed") {
			t.Errorf("notes: %+v", r.api.Notes())
		}
	})
	t.Run("abort and skip", func(t *testing.T) {
		r, repo, rp := rebaseRig(t, rigOptions{})
		before := repo.Head()
		r.run(rp, "git.rebase", map[string]any{"ref": "ours"})
		r.run(rp, "git.rebaseAbort", nil)
		if repo.Head() != before || r.api.ContextKey("gitRebaseInProgress") != false {
			t.Errorf("HEAD %s, in progress %v", repo.Head(), r.api.ContextKey("gitRebaseInProgress"))
		}
		if got := inputOf(r, rp).Value(); got != "" {
			t.Errorf("the box holds %q", got)
		}
		r.run(rp, "git.rebase", map[string]any{"ref": "ours"})
		r.run(rp, "git.rebaseSkip", nil)
		if got := repo.Git("log", "--format=%s", "ours..theirs"); got != "Add extra after the retries" {
			t.Errorf("theirs has the commits %q", got)
		}
	})
	t.Run("abort and skip without a rebase say so", func(t *testing.T) {
		r, _, rp := rebaseRig(t, rigOptions{})
		r.run(rp, "git.rebaseAbort", nil)
		r.run(rp, "git.rebaseSkip", nil)
		if got := len(notesWith(r, "No rebase in progress.")); got != 2 {
			t.Errorf("%d notes: %+v", got, r.api.Notes())
		}
	})
	t.Run("the rebase of a restarted extension is found again", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("repo")
		repo.ConflictingBranches()
		repo.Git("checkout", "-q", "theirs")
		_, _, _ = repo.GitErr("rebase", "ours")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		rp := r.repo(repo.Dir)
		if got := inputOf(r, rp).Value(); got != "Retry five times" {
			t.Errorf("the box holds %q", got)
		}
		if got := inputOf(r, rp).Placeholder(); got != "Message ({0} to continue the rebase)" {
			t.Errorf("placeholder = %q", got)
		}
		if r.api.ContextKey("gitRebaseInProgress") != true {
			t.Error("gitRebaseInProgress is not set")
		}
	})
}

func TestRebaseInteractiveEditsTheListInATab(t *testing.T) {
	t.Run("the list opens in a tab and the rebase goes on when it is closed", func(t *testing.T) {
		r, repo, rp := rebaseRig(t, rigOptions{settings: map[string]any{"git.useEditorAsCommitInput": true}})
		base := repo.Git("merge-base", "theirs", "ours")
		r.start(rp, "git.rebaseInteractive", map[string]any{"ref": base})
		path, uri := r.editorOpened(0)
		if !strings.HasSuffix(path, "git-rebase-todo") {
			t.Fatalf("the editor opened %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "pick ") || !strings.Contains(string(data), "Add extra after the retries") {
			t.Errorf("the list is %q", data)
		}
		// the user turns the first commit into a drop
		lines := strings.Split(string(data), "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, "pick ") && strings.Contains(l, "Retry five times") {
				lines[i] = "drop" + strings.TrimPrefix(l, "pick")
			}
		}
		writeFile(t, path, strings.Join(lines, "\n"))
		r.userSaves(uri)
		r.userCloses(uri)
		r.idle(rp)
		if got := repo.Git("log", "--format=%s", base+"..theirs"); got != "Add extra after the retries" {
			t.Errorf("theirs has the commits %q", got)
		}
	})
	t.Run("closing the tab without a save stops the rebase and changes nothing", func(t *testing.T) {
		r, repo, rp := rebaseRig(t, rigOptions{settings: map[string]any{"git.useEditorAsCommitInput": true}})
		before := repo.Head()
		base := repo.Git("merge-base", "theirs", "ours")
		r.start(rp, "git.rebaseInteractive", map[string]any{"ref": base})
		_, uri := r.editorOpened(0)
		r.userCloses(uri)
		r.idle(rp)
		if repo.Head() != before {
			t.Errorf("HEAD moved to %s", repo.Head())
		}
	})
	t.Run("without the editor setting it says what to turn on", func(t *testing.T) {
		r, _, rp := rebaseRig(t, rigOptions{settings: map[string]any{"git.useEditorAsCommitInput": false}})
		r.run(rp, "git.rebaseInteractive", map[string]any{"ref": "ours"})
		if !hasNote(r, `Turn on "git.useEditorAsCommitInput"`) {
			t.Errorf("notes: %+v", r.api.Notes())
		}
	})
}

func TestCherryPickCommand(t *testing.T) {
	t.Run("the hash is asked for, a conflict stops, Continue finishes", func(t *testing.T) {
		r, repo, rp := conflictRig(t, rigOptions{})
		theirs := repo.Git("rev-parse", "theirs")
		r.start(rp, "git.cherryPick", nil)
		in := r.nextInput(0)
		if got := in.Options().Prompt; got != "Commit Hash" {
			t.Errorf("prompt = %q", got)
		}
		in.Accept(theirs)
		r.idle(rp)
		if !hasNote(r, "There were merge conflicts while cherry picking the changes. Resolve the conflicts before committing them.") {
			t.Errorf("notes: %+v", r.api.Notes())
		}
		if r.api.ContextKey("gitCherryPickInProgress") != true {
			t.Error("gitCherryPickInProgress is not set")
		}
		items := r.source(rp.root).StatusBarCommands()
		if len(items) == 0 || !strings.HasPrefix(items[0].Text, "$(git-branch-conflicts) ") {
			t.Errorf("branch item = %+v", items)
		}
		box := inputOf(r, rp)
		if got := box.Placeholder(); got != "Message ({0} to continue the cherry-pick)" {
			t.Errorf("placeholder = %q", got)
		}
		if got := box.Value(); got != "Retry five times" {
			t.Errorf("the box holds %q", got)
		}
		b := r.source(rp.root).ActionButton()
		if b == nil || b.Enabled || b.Command.Tooltip != "Continue Cherry Pick" {
			t.Errorf("button = %+v", b)
		}
		resolveFile(repo, loaderPath)
		r.refresh(rp)
		r.commitBox(rp)
		if _, err := os.Stat(repo.Path(".git/CHERRY_PICK_HEAD")); err == nil {
			t.Error("the cherry-pick is still in progress")
		}
		if got := repo.Git("log", "-1", "--format=%s"); got != "Retry five times" {
			t.Errorf("HEAD is %q", got)
		}
	})
	t.Run("abort", func(t *testing.T) {
		r, repo, rp := conflictRig(t, rigOptions{})
		before := repo.Head()
		r.run(rp, "git.cherryPick", map[string]any{"ref": "theirs"})
		r.run(rp, "git.cherryPickAbort", nil)
		if repo.Head() != before || r.api.ContextKey("gitCherryPickInProgress") != false {
			t.Errorf("HEAD %s, in progress %v", repo.Head(), r.api.ContextKey("gitCherryPickInProgress"))
		}
	})
	t.Run("changes that are in the branch already", func(t *testing.T) {
		r, repo, rp := conflictRig(t, rigOptions{})
		before := repo.Head()
		// picking the commit HEAD is leaves nothing to commit
		r.run(rp, "git.cherryPick", map[string]any{"ref": "ours"})
		if !hasNote(r, "The changes are already present in the current branch.") || repo.Head() != before {
			t.Errorf("HEAD %s, notes: %+v", repo.Head(), r.api.Notes())
		}
		if _, err := os.Stat(repo.Path(".git/CHERRY_PICK_HEAD")); err == nil {
			t.Error("the empty pick is still in progress")
		}
	})
	t.Run("closing the box picks nothing", func(t *testing.T) {
		r, repo, rp := conflictRig(t, rigOptions{})
		before := repo.Head()
		r.start(rp, "git.cherryPick", nil)
		r.nextInput(0).Cancel()
		r.idle(rp)
		if repo.Head() != before || len(r.api.Notes()) != 0 {
			t.Errorf("HEAD %s, notes %+v", repo.Head(), r.api.Notes())
		}
	})
}

func TestRevertInProgressIsContinued(t *testing.T) {
	r, repo, rp := conflictRig(t, rigOptions{})
	// the revert of the first commit of the branch, which the later change of the same lines depends on, stops on a conflict
	if _, _, err := repo.GitErr("revert", "--no-edit", "ours~1"); err == nil {
		t.Fatal("the revert did not stop")
	}
	r.refresh(rp)
	if _, err := os.Stat(repo.Path(".git/REVERT_HEAD")); err != nil {
		t.Fatalf("the revert is not in progress: %v", err)
	}
	if got := inputOf(r, rp).Placeholder(); got != "Message ({0} to continue the revert)" {
		t.Errorf("placeholder = %q", got)
	}
	if b := r.source(rp.root).ActionButton(); b == nil || b.Enabled || b.Command.Tooltip != "Continue Revert" {
		t.Errorf("button = %+v", b)
	}
	items := r.source(rp.root).StatusBarCommands()
	if len(items) == 0 || !strings.HasPrefix(items[0].Text, "$(git-branch-conflicts) ") {
		t.Errorf("branch item = %+v", items)
	}
	resolveFile(repo, loaderPath)
	r.refresh(rp)
	inputOf(r, rp).SetValue("")
	r.commitBox(rp)
	if _, err := os.Stat(repo.Path(".git/REVERT_HEAD")); err == nil {
		t.Error("the revert is still in progress")
	}
	if got := repo.Git("log", "-1", "--format=%s"); !strings.HasPrefix(got, "Revert ") {
		t.Errorf("HEAD is %q", got)
	}
}

func TestEveryConflictFormShowsAndOpens(t *testing.T) {
	// the seven forms of the unmerged status: four made with real merges, three with stages put in the index
	type form struct {
		name    string
		setup   func(e *gittest.Env) *gittest.Repo
		tooltip string
		file    string
		opens   string // "file", "merge" (a document that opens in the merge editor with git.mergeEditor on), "diff" or "nothing"
	}
	merge := func(e *gittest.Env, ours, theirs func(r *gittest.Repo)) *gittest.Repo {
		r := e.Init("repo")
		r.Write("f.txt", "one\ntwo\nthree\n")
		r.Commit("Add f")
		r.Branch("theirs", "")
		r.Branch("ours", "")
		r.Checkout("theirs")
		theirs(r)
		r.Checkout("ours")
		ours(r)
		if err := r.Merge("theirs"); err == nil {
			t.Fatal("the merge did not stop")
		}
		return r
	}
	edit := func(text string) func(r *gittest.Repo) {
		return func(r *gittest.Repo) { r.Write("f.txt", text); r.Commit("Edit f") }
	}
	remove := func(r *gittest.Repo) { r.Git("rm", "-q", "f.txt"); r.Commit("Remove f") }
	add := func(text string) func(r *gittest.Repo) {
		return func(r *gittest.Repo) { r.Write("n.txt", text); r.Commit("Add n") }
	}
	// stages puts a file in the index at the given stages only, which is how git leaves the forms a merge does not easily make
	stages := func(stage ...string) func(e *gittest.Env) *gittest.Repo {
		return func(e *gittest.Env) *gittest.Repo {
			r := e.Init("repo")
			r.Write("keep.txt", "k\n")
			r.Commit("Start")
			r.Write("p.txt", "p\n")
			blob := r.Git("hash-object", "-w", "p.txt")
			var in strings.Builder
			for _, st := range stage {
				in.WriteString("100644 " + blob + " " + st + "\tp.txt\n")
			}
			if _, errOut, err := e.GitInput(r.Dir, in.String(), "update-index", "--index-info"); err != nil {
				t.Fatalf("update-index: %v\n%s", err, errOut)
			}
			return r
		}
	}
	forms := []form{
		{"both modified", func(e *gittest.Env) *gittest.Repo {
			return merge(e, edit("one\nours\nthree\n"), edit("one\ntheirs\nthree\n"))
		}, "Conflict: Both Modified", "f.txt", "merge"},
		{"both added", func(e *gittest.Env) *gittest.Repo { return merge(e, add("ours\n"), add("theirs\n")) }, "Conflict: Both Added", "n.txt", "merge"},
		{"deleted by us", func(e *gittest.Env) *gittest.Repo { return merge(e, remove, edit("one\ntheirs\nthree\n")) }, "Conflict: Deleted By Us", "f.txt", "diff"},
		{"deleted by them", func(e *gittest.Env) *gittest.Repo { return merge(e, edit("one\nours\nthree\n"), remove) }, "Conflict: Deleted By Them", "f.txt", "diff"},
		{"added by us", stages("2"), "Conflict: Added By Us", "p.txt", "file"},
		{"added by them", stages("3"), "Conflict: Added By Them", "p.txt", "file"},
		{"both deleted", stages("1"), "Conflict: Both Deleted", "p.txt", "nothing"},
	}
	for _, f := range forms {
		for _, on := range []bool{false, true} {
			name := f.name + " with git.mergeEditor off"
			if on {
				name = f.name + " with git.mergeEditor on"
			}
			t.Run(name, func(t *testing.T) {
				e := gittest.NewEnv(t)
				repo := f.setup(e)
				r := newRig(t, e, rigOptions{folder: repo.Dir, settings: map[string]any{"git.mergeEditor": on}})
				r.settle()
				rp := r.repo(repo.Dir)
				if got := r.letters(repo.Dir, groupMerge); len(got) != 1 || got[0] != f.file+" !" {
					t.Fatalf("merge group = %v", got)
				}
				states := r.source(rp.root).Group(groupMerge).Resources()
				if len(states) != 1 || states[0].Decorations.Tooltip != f.tooltip {
					t.Fatalf("states = %+v", states)
				}
				// a click on the row
				if err := r.api.Run(CommandOpenResource, resourceArg(rp, groupMerge, f.file)); err != nil {
					t.Fatal(err)
				}
				opened, diffs := r.api.OpenedDocuments(), r.api.OpenedDiffs()
				switch f.opens {
				case "nothing":
					if len(opened) != 0 || len(diffs) != 0 {
						t.Errorf("opened %+v, diffs %+v", opened, diffs)
					}
				case "diff":
					if len(opened) != 0 || len(diffs) != 1 {
						t.Errorf("opened %+v, diffs %+v", opened, diffs)
					}
				default:
					if len(opened) != 1 || len(diffs) != 0 {
						t.Fatalf("opened %+v, diffs %+v", opened, diffs)
					}
					if want := on && f.opens == "merge"; opened[0].MergeEditor != want {
						t.Errorf("MergeEditor = %v, want %v", opened[0].MergeEditor, want)
					}
					if p, _ := uriPath(opened[0].URI); p != repo.Path(f.file) {
						t.Errorf("opened %s", opened[0].URI)
					}
				}
			})
		}
	}
}

func TestTheMergeEditorCompletingAFileStagesIt(t *testing.T) {
	r, repo, rp := conflictRig(t, rigOptions{})
	r.run(rp, "git.merge", map[string]any{"ref": "theirs"})
	file := repo.Path(loaderPath)
	// the merge editor saved the resolved file and publishes the event; the file is staged, the group is empty, Continue is on
	repo.Write(loaderPath, gittest.LoaderSource(0)+"// resolved\n")
	r.api.Emit(extension.TopicMergeEditorCompleted, extension.MergeEditorCompleted{URI: fileURI(file)})
	r.idle(rp)
	if got := r.letters(repo.Dir, groupMerge); len(got) != 0 {
		t.Errorf("merge group = %v", got)
	}
	if got := repo.Git("diff", "--cached", "--name-only"); got != loaderPath {
		t.Errorf("staged: %q", got)
	}
	if mc, _ := r.api.ContextKey("git.mergeChanges").([]any); len(mc) != 0 {
		t.Errorf("git.mergeChanges = %v", mc)
	}
	if b := r.source(rp.root).ActionButton(); b == nil || !b.Enabled || b.Command.Title != "$(check) Continue" {
		t.Errorf("button = %+v", b)
	}
	r.commitBox(rp)
	if n := len(strings.Fields(repo.Git("log", "-1", "--format=%P"))); n != 2 {
		t.Errorf("HEAD has %d parents", n)
	}
	t.Run("a file that is not in Merge Changes stays as it is", func(t *testing.T) {
		repo.Write("other.txt", "other\n")
		r.refresh(rp)
		r.api.Emit(extension.TopicMergeEditorCompleted, extension.MergeEditorCompleted{URI: fileURI(repo.Path("other.txt"))})
		r.api.Emit(extension.TopicMergeEditorCompleted, extension.MergeEditorCompleted{URI: "untitled:1"})
		r.idle(rp)
		if got := repo.Git("diff", "--cached", "--name-only"); got != "" {
			t.Errorf("staged: %q", got)
		}
	})
	t.Run("the command stages the file of its argument", func(t *testing.T) {
		r, repo, rp := conflictRig(t, rigOptions{})
		r.run(rp, "git.merge", map[string]any{"ref": "theirs"})
		repo.Write(loaderPath, "resolved\n")
		r.run(rp, "git.acceptMerge", map[string]any{"uri": fileURI(repo.Path(loaderPath))})
		if got := repo.Git("diff", "--cached", "--name-only"); got != loaderPath {
			t.Errorf("staged: %q", got)
		}
	})
}

func TestContextKeysOfTheConflicts(t *testing.T) {
	r, repo, rp := conflictRig(t, rigOptions{})
	if mc, _ := r.api.ContextKey("git.mergeChanges").([]any); len(mc) != 0 {
		t.Errorf("git.mergeChanges = %v before a merge", mc)
	}
	r.run(rp, "git.merge", map[string]any{"ref": "theirs"})
	file := fileURI(repo.Path(loaderPath))
	r.api.SetActive(&extension.ActiveDocument{URI: file, LanguageID: "go"})
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: file, LanguageID: "go"})
	r.api.Pump()
	if r.api.ContextKey("git.activeResourceHasMergeConflicts") != true {
		t.Error("git.activeResourceHasMergeConflicts is not set for the file in conflict")
	}
	other := fileURI(repo.Path("README.md"))
	r.api.SetActive(&extension.ActiveDocument{URI: other, LanguageID: "markdown"})
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: other, LanguageID: "markdown"})
	r.api.Pump()
	if r.api.ContextKey("git.activeResourceHasMergeConflicts") != false {
		t.Error("git.activeResourceHasMergeConflicts is set for a file that is not in conflict")
	}
}

func TestComputeConflictsWithGit(t *testing.T) {
	tests := []struct {
		name  string
		cmd   string
		check func(t *testing.T, text string)
	}{
		{"plain", "git.runGitMerge", func(t *testing.T, text string) {
			if !strings.Contains(text, "<<<<<<< HEAD\n") || !strings.Contains(text, ">>>>>>> theirs\n") || strings.Contains(text, "|||||||") {
				t.Errorf("text = %q", text)
			}
		}},
		{"diff3", "git.runGitMergeDiff3", func(t *testing.T, text string) {
			if !strings.Contains(text, "<<<<<<< HEAD\n") || !strings.Contains(text, "||||||| base\n") || !strings.Contains(text, ">>>>>>> theirs\n") {
				t.Errorf("text = %q", text)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := conflictRig(t, rigOptions{})
			r.run(rp, "git.merge", map[string]any{"ref": "theirs"})
			file := fileURI(repo.Path(loaderPath))
			// the user has changed the result in the merge editor meanwhile
			r.api.SetDocument(file, "the result as the user has it\n")
			r.api.SetActiveMergeEditor(&extension.ActiveMerge{Result: file, Markers: true})
			if err := r.api.Run(tc.cmd, nil); err != nil {
				t.Fatal(err)
			}
			r.api.WaitFor("the new result", func() bool {
				r.api.Pump()
				text, _ := r.api.DocumentText(file)
				return text != "the result as the user has it\n"
			})
			text, _ := r.api.DocumentText(file)
			tc.check(t, text)
			// what git merge-file makes of the three stages
			want := mergeFileOf(t, repo, tc.name == "diff3")
			if text != want {
				t.Errorf("text = %q, want what git merge-file makes: %q", text, want)
			}
		})
	}
	t.Run("without a merge editor in front nothing happens", func(t *testing.T) {
		r, repo, rp := conflictRig(t, rigOptions{})
		r.run(rp, "git.merge", map[string]any{"ref": "theirs"})
		file := fileURI(repo.Path(loaderPath))
		r.api.SetDocument(file, "unchanged\n")
		r.run(rp, "git.runGitMerge", nil)
		if text, _ := r.api.DocumentText(file); text != "unchanged\n" {
			t.Errorf("text = %q", text)
		}
	})
	t.Run("a file that is not in conflict", func(t *testing.T) {
		r, repo, rp := conflictRig(t, rigOptions{})
		file := fileURI(repo.Path(loaderPath))
		r.api.SetDocument(file, "unchanged\n")
		r.api.SetActiveMergeEditor(&extension.ActiveMerge{Result: file, Markers: true})
		r.run(rp, "git.runGitMerge", nil)
		r.api.WaitFor("the failure", func() bool { r.api.Pump(); return hasNote(r, "git:") || hasNote(r, "Git:") })
		if text, _ := r.api.DocumentText(file); text != "unchanged\n" {
			t.Errorf("text = %q", text)
		}
	})
}

// mergeFileOf is what git merge-file makes of the stages of src/loader.go, with the labels the command uses.
func mergeFileOf(t *testing.T, repo *gittest.Repo, diff3 bool) string {
	t.Helper()
	dir := t.TempDir()
	for n, name := range map[string]string{"2": "ours", "1": "base", "3": "theirs"} {
		writeFile(t, dir+"/"+name, repo.Git("show", ":"+n+":"+loaderPath)+"\n")
	}
	args := []string{"merge-file", "-p", "-L", "HEAD", "-L", "base", "-L", "theirs"}
	if diff3 {
		args = append(args, "--diff3")
	}
	args = append(args, dir+"/ours", dir+"/base", dir+"/theirs")
	out, _, _ := repo.GitErr(args...)
	return out + "\n"
}

func TestTheCommandsOfTheStatesAreInTheMenus(t *testing.T) {
	menus := map[string][]string{}
	for _, m := range (&gitscm{}).Manifest().Contributes.Menus {
		if m.Menu == branchSubmenu || m.Menu == commitSubmenu || m.Menu == extension.MenuEditorContent {
			menus[m.Menu] = append(menus[m.Menu], m.Command)
		}
	}
	for menu, want := range map[string][]string{
		branchSubmenu:               {"git.merge", "git.rebase", "git.rebaseInteractive"},
		commitSubmenu:               {"git.rebaseAbort", "git.mergeAbort", "git.cherryPickAbort"},
		extension.MenuEditorContent: {"git.acceptMerge", "git.openMergeEditor"},
	} {
		for _, id := range want {
			if indexOfLabel(menus[menu], id) < 0 {
				t.Errorf("%s has no %s: %v", menu, id, menus[menu])
			}
		}
	}
}
