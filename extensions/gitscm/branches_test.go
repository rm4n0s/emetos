package gitscm

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// The commands of branches and tags against real repositories (step 24.13).

// refExists says whether a ref is in the repository.
func refExists(repo *gittest.Repo, ref string) bool {
	_, _, err := repo.GitErr("rev-parse", "--verify", "-q", ref)
	return err == nil
}

func TestCreateBranch(t *testing.T) {
	tests := []struct {
		name     string
		cmd      string
		settings map[string]any
		args     any
		typed    string // answers the box with this text, "" when no box is expected
		pick     string // the ref chosen for a branch from a ref
		onBranch string
		at       string
	}{
		{"asks the name, starts at HEAD", "git.branch", nil, nil, "topic", "", "topic", "main"},
		{"whitespace becomes the character of the setting", "git.branch", map[string]any{"git.branchWhitespaceChar": "_"}, nil, "my new  topic", "", "my_new__topic", "main"},
		{"the prefix starts the box", "git.branch", map[string]any{"git.branchPrefix": "ada/"}, nil, "ada/topic", "", "ada/topic", "main"},
		{"a name from the caller asks nothing", "git.branch", nil, map[string]any{"name": "given"}, "", "", "given", "main"},
		{"from a branch", "git.branchFrom", nil, nil, "from-old", "$(git-branch) old", "from-old", "old"},
		{"from a remote branch", "git.branchFrom", nil, nil, "from-topic", "$(cloud) origin/topic", "from-topic", "origin/topic"},
		{"from a tag", "git.branchFrom", nil, nil, "from-v1", "$(tag) v1", "from-v1", "v1"},
		{"from HEAD", "git.branchFrom", nil, nil, "from-head", "HEAD", "from-head", "main"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := refsRig(t, rigOptions{settings: tc.settings})
			r.start(rp, tc.cmd, tc.args)
			if tc.pick != "" {
				p := r.waitPick()
				p.Choose(indexOf(t, p, tc.pick))
			}
			if tc.typed != "" {
				r.nextInput(0).Accept(tc.typed)
			} else if len(r.api.Inputs()) != 0 {
				t.Error("a box was shown")
			}
			r.idle(rp)
			if got := branchOf(repo); got != tc.onBranch {
				t.Errorf("on %q, want %q", got, tc.onBranch)
			}
			if got, want := repo.Git("rev-parse", "HEAD"), repo.Git("rev-parse", tc.at); got != want {
				t.Errorf("HEAD = %s, want %s (%s)", got, want, tc.at)
			}
			if len(r.api.Notes()) != 0 {
				t.Errorf("notifications: %+v", r.api.Notes())
			}
		})
	}
}

func TestTheBranchNameBoxChecksTheName(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		text     string
		want     string // the message under the box
	}{
		{"a good name", nil, "topic", ""},
		{"nothing", nil, "   ", "Please provide a new branch name"},
		{"a name that is taken", nil, "feature", `Branch "feature" already exists`},
		{"a name that is taken once it is made safe", nil, "feature ", `Branch "feature" already exists`},
		{"a name that does not match the expression", map[string]any{"git.branchValidationRegex": `^(feat|fix)/.+`}, "topic", "Branch name needs to match regex: ^(feat|fix)/.+"},
		{"a name that matches the expression", map[string]any{"git.branchValidationRegex": `^(feat|fix)/.+`}, "fix/loader", ""},
		{"the check sees the name after the whitespace is replaced", map[string]any{"git.branchValidationRegex": `^[a-z-]+$`}, "my topic", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _, rp := refsRig(t, rigOptions{settings: tc.settings})
			r.start(rp, "git.branch", nil)
			in := r.nextInput(0)
			if got := in.Validate(tc.text); got != tc.want {
				t.Errorf("Validate(%q) = %q, want %q", tc.text, got, tc.want)
			}
			in.Cancel()
			r.idle(rp)
		})
	}
}

func TestCreateBranchFailures(t *testing.T) {
	t.Run("a name that is taken, given by the caller", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		if err := r.api.Run("git.branch", map[string]any{"name": "feature"}); err != nil {
			t.Fatal(err)
		}
		r.api.WaitFor("the failure", func() bool { return len(notesWith(r, "already exists")) > 0 })
		r.idle(rp)
		if got := branchOf(repo); got != "main" {
			t.Errorf("on %q", got)
		}
	})
	t.Run("closing the box makes nothing", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.branch", nil)
		r.nextInput(0).Cancel()
		r.idle(rp)
		if got := branchOf(repo); got != "main" || len(r.api.Notes()) != 0 {
			t.Errorf("on %q, notes %+v", got, r.api.Notes())
		}
	})
}

func TestRenameBranch(t *testing.T) {
	r, repo, rp := refsRig(t, rigOptions{})
	r.start(rp, "git.renameBranch", nil)
	in := r.nextInput(0)
	if in.Options().Value != "main" {
		t.Errorf("the box starts with %q", in.Options().Value)
	}
	if got := in.Validate("main"); got != `Branch "main" already exists` {
		t.Errorf("Validate(main) = %q", got)
	}
	in.Accept("trunk")
	r.idle(rp)
	if got := branchOf(repo); got != "trunk" {
		t.Errorf("on %q", got)
	}
	if refExists(repo, "refs/heads/main") {
		t.Error("main still exists")
	}
	if items := r.source(repo.Dir).StatusBarCommands(); items[0].Text != "$(git-branch) trunk" {
		t.Errorf("status bar = %+v", items[0])
	}
	t.Run("a name from the caller that git refuses", func(t *testing.T) {
		r, _, rp := refsRig(t, rigOptions{})
		if err := r.api.Run("git.renameBranch", map[string]any{"name": "HEAD"}); err != nil {
			t.Fatal(err)
		}
		r.api.WaitFor("the failure", func() bool { return len(notesWith(r, "Invalid branch name")) > 0 })
		r.idle(rp)
	})
	t.Run("a name that is taken, from the caller", func(t *testing.T) {
		r, _, rp := refsRig(t, rigOptions{})
		if err := r.api.Run("git.renameBranch", map[string]any{"name": "old"}); err != nil {
			t.Fatal(err)
		}
		r.api.WaitFor("the failure", func() bool { return len(notesWith(r, `A branch named "old" already exists`)) > 0 })
		r.idle(rp)
	})
	t.Run("a detached HEAD has no branch to rename", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		repo.Checkout("v1")
		r.refresh(rp)
		r.start(rp, "git.renameBranch", nil)
		if len(r.api.Inputs()) != 0 || len(notesWith(r, "check out a branch")) != 1 {
			t.Errorf("inputs %d, notes %+v", len(r.api.Inputs()), r.api.Notes())
		}
	})
}

func TestDeleteBranch(t *testing.T) {
	t.Run("the list leaves out the current branch and has the details", func(t *testing.T) {
		r, _, rp := refsRig(t, rigOptions{settings: map[string]any{"git.branchSortOrder": "alphabetically"}})
		r.start(rp, "git.deleteBranch", nil)
		p := r.waitPick()
		if got := pickLabels(p); !equalLists(got, []string{"-- branches --", "$(git-branch) feature", "$(git-branch) old"}) {
			t.Errorf("items = %v", got)
		}
		if p.Options().Placeholder != "Select a branch to delete" {
			t.Errorf("placeholder = %q", p.Options().Placeholder)
		}
		p.Close()
	})
	t.Run("a merged branch is deleted", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.deleteBranch", nil)
		p := r.waitPick()
		p.Choose(indexOf(t, p, "$(git-branch) old"))
		r.idle(rp)
		if refExists(repo, "refs/heads/old") {
			t.Error("old is still there")
		}
		if len(r.api.OpenDialogs()) != 0 || len(r.api.Notes()) != 0 {
			t.Errorf("questions %d, notes %+v", len(r.api.OpenDialogs()), r.api.Notes())
		}
	})
	t.Run("a branch that is not merged asks, and Delete Branch forces it", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		repo.Git("push", "-q", "origin", "--delete", "feature") // nothing else has its commit now
		repo.Git("branch", "--unset-upstream", "feature")
		repo.Git("update-ref", "-d", "refs/remotes/origin/feature")
		repo.Git("update-ref", "-d", "refs/remotes/origin/topic")
		r.start(rp, "git.deleteBranch", nil)
		p := r.waitPick()
		p.Choose(indexOf(t, p, "$(git-branch) feature"))
		d := r.theOnlyDialog()
		if d.Spec().Message != `The branch "feature" is not fully merged. Delete anyway?` {
			t.Errorf("message = %q", d.Spec().Message)
		}
		if got := buttons(d); !equalLists(got, []string{"Delete Branch", "Cancel"}) {
			t.Errorf("buttons = %v", got)
		}
		d.Press("Delete Branch")
		r.idle(rp)
		if refExists(repo, "refs/heads/feature") {
			t.Error("feature is still there")
		}
	})
	t.Run("Cancel keeps the branch that is not merged", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		repo.Git("update-ref", "-d", "refs/remotes/origin/feature")
		repo.Git("update-ref", "-d", "refs/remotes/origin/topic")
		repo.Git("branch", "--unset-upstream", "feature")
		if err := r.api.Run("git.deleteBranch", map[string]any{"name": "feature"}); err != nil {
			t.Fatal(err)
		}
		r.theOnlyDialog().Cancel()
		r.idle(rp)
		if !refExists(repo, "refs/heads/feature") {
			t.Error("feature was deleted")
		}
	})
	t.Run("the current branch is refused by git", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		if err := r.api.Run("git.deleteBranch", map[string]any{"name": "main"}); err != nil {
			t.Fatal(err)
		}
		r.api.WaitFor("the failure", func() bool { return len(notesWith(r, "git branch failed")) > 0 })
		r.idle(rp)
		if !refExists(repo, "refs/heads/main") {
			t.Error("main was deleted")
		}
	})
	t.Run("a repository with only the current branch has nothing to delete", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("single")
		repo.Write("a.txt", "x\n")
		repo.Commit("Add a")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		rp := r.repo(repo.Dir)
		if err := r.api.Run("git.deleteBranch", nil); err != nil {
			t.Fatal(err)
		}
		r.api.WaitFor("the notice", func() bool { return len(notesWith(r, "There are no branches to delete.")) > 0 })
		r.idle(rp)
		if len(r.api.Picks()) != 0 {
			t.Error("an empty list was shown")
		}
	})
}

func TestDeleteRemoteBranch(t *testing.T) {
	t.Run("the list has the remote branches, not the one the current branch tracks", func(t *testing.T) {
		r, _, rp := refsRig(t, rigOptions{settings: map[string]any{"git.branchSortOrder": "alphabetically"}})
		r.start(rp, "git.deleteRemoteBranch", nil)
		p := r.waitPick()
		if got := pickLabels(p); !equalLists(got, []string{"-- remote branches --", "$(cloud) origin/feature", "$(cloud) origin/old", "$(cloud) origin/topic"}) {
			t.Errorf("items = %v", got)
		}
		if p.Options().Placeholder != "Select a remote branch to delete" {
			t.Errorf("placeholder = %q", p.Options().Placeholder)
		}
		p.Close()
	})
	t.Run("the branch is deleted at the remote after a question", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		remote := repo.Git("remote", "get-url", "origin")
		r.start(rp, "git.deleteRemoteBranch", nil)
		p := r.waitPick()
		p.Choose(indexOf(t, p, "$(cloud) origin/topic"))
		d := r.theOnlyDialog()
		if d.Spec().Message != `Are you sure you want to delete the branch "topic" from the remote "origin"?` || !equalLists(buttons(d), []string{"Delete Remote Branch", "Cancel"}) {
			t.Errorf("question = %q %v", d.Spec().Message, buttons(d))
		}
		if !refExistsAt(repo, remote, "refs/heads/topic") {
			t.Fatal("topic is not at the remote")
		}
		d.Press("Delete Remote Branch")
		r.idle(rp)
		if refExistsAt(repo, remote, "refs/heads/topic") {
			t.Error("topic is still at the remote")
		}
	})
	t.Run("Cancel leaves it", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		remote := repo.Git("remote", "get-url", "origin")
		if err := r.api.Run("git.deleteRemoteBranch", map[string]any{"name": "origin/topic"}); err != nil {
			t.Fatal(err)
		}
		r.theOnlyDialog().Cancel()
		r.idle(rp)
		if !refExistsAt(repo, remote, "refs/heads/topic") {
			t.Error("topic was deleted")
		}
	})
}

// refExistsAt says whether a ref is in the repository at a path (the remote).
func refExistsAt(repo *gittest.Repo, path, ref string) bool {
	out := repo.Git("ls-remote", path, ref)
	return strings.TrimSpace(out) != ""
}

func TestTags(t *testing.T) {
	t.Run("a lightweight tag", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.createTag", nil)
		in := r.nextInput(0)
		if o := in.Options(); o.Prompt != "Tag name" || o.Message != "Please provide a tag name" || !o.IgnoreFocusOut {
			t.Errorf("name box = %+v", o)
		}
		in.Accept("v3")
		msg := r.nextInput(1)
		if o := msg.Options(); o.Prompt != "Message" || o.Message != "Please provide a message to annotate the tag" {
			t.Errorf("message box = %+v", o)
		}
		msg.Accept("")
		r.idle(rp)
		if got := repo.Git("cat-file", "-t", "v3"); got != "commit" {
			t.Errorf("v3 is a %s", got)
		}
		if got, want := repo.Git("rev-parse", "v3"), repo.Git("rev-parse", "HEAD"); got != want {
			t.Errorf("v3 names %s, want HEAD", got)
		}
	})
	t.Run("an annotated tag", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.createTag", nil)
		r.nextInput(0).Accept("v3")
		r.nextInput(1).Accept("Third release")
		r.idle(rp)
		if got := repo.Git("cat-file", "-t", "v3"); got != "tag" {
			t.Errorf("v3 is a %s", got)
		}
		if got := repo.Git("tag", "-l", "--format=%(contents:subject)", "v3"); got != "Third release" {
			t.Errorf("message = %q", got)
		}
	})
	t.Run("closing the message box still makes a lightweight tag", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.createTag", map[string]any{"name": "v4", "ref": "old"})
		r.nextInput(0).Cancel()
		r.idle(rp)
		if got := repo.Git("cat-file", "-t", "v4"); got != "commit" {
			t.Errorf("v4 is a %s", got)
		}
		if got, want := repo.Git("rev-parse", "v4"), repo.Git("rev-parse", "old"); got != want {
			t.Errorf("v4 names %s, want old", got)
		}
	})
	t.Run("the name is made safe", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.createTag", nil)
		r.nextInput(0).Accept("release 1:final.")
		r.nextInput(1).Accept("")
		r.idle(rp)
		if !refExists(repo, "refs/tags/release-1-final-") {
			t.Errorf("tags = %q", repo.Git("tag", "-l"))
		}
	})
	t.Run("a name that is taken fails with the message of git", func(t *testing.T) {
		r, _, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.createTag", nil)
		r.nextInput(0).Accept("v1")
		r.nextInput(1).Accept("")
		r.api.WaitFor("the failure", func() bool { return len(notesWith(r, "git tag failed")) > 0 })
		r.idle(rp)
	})
	t.Run("closing the name box makes nothing", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.createTag", nil)
		r.nextInput(0).Cancel()
		r.idle(rp)
		if got := repo.Git("tag", "-l"); got != "v1\nv2" {
			t.Errorf("tags = %q", got)
		}
	})
	t.Run("delete a tag", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.deleteTag", nil)
		p := r.waitPick()
		if p.Options().Placeholder != "Select a tag to delete" || !equalLists(pickLabels(p), []string{"-- tags --", "$(tag) v1", "$(tag) v2"}) {
			t.Errorf("pick = %q %v", p.Options().Placeholder, pickLabels(p))
		}
		p.Choose(indexOf(t, p, "$(tag) v1"))
		r.idle(rp)
		if got := repo.Git("tag", "-l"); got != "v2" {
			t.Errorf("tags = %q", got)
		}
	})
	t.Run("a repository without tags says so", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		repo.Git("tag", "-d", "v1", "v2")
		r.start(rp, "git.deleteTag", nil)
		p := r.waitPick()
		if got := pickLabels(p); !equalLists(got, []string{"$(info) This repository has no tags."}) {
			t.Errorf("items = %v", got)
		}
		p.Choose(0)
		r.idle(rp)
		if len(r.api.Notes()) != 0 {
			t.Errorf("notes %+v", r.api.Notes())
		}
	})
}

func TestRemoteTags(t *testing.T) {
	t.Run("push the tags", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		remote := repo.Git("remote", "get-url", "origin")
		if refExistsAt(repo, remote, "refs/tags/v1") {
			t.Fatal("the remote has v1 already")
		}
		r.run(rp, "git.pushTags", nil)
		for _, tag := range []string{"refs/tags/v1", "refs/tags/v2"} {
			if !refExistsAt(repo, remote, tag) {
				t.Errorf("%s is not at the remote", tag)
			}
		}
		if len(r.api.Notes()) != 0 {
			t.Errorf("notes %+v", r.api.Notes())
		}
	})
	t.Run("a repository with no remote says so", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("local")
		repo.Write("a.txt", "x\n")
		repo.Commit("Add a")
		repo.Tag("v1", "")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		rp := r.repo(repo.Dir)
		r.run(rp, "git.pushTags", nil)
		notes := r.api.Notes()
		if len(notes) != 1 || notes[0].Severity != "warn" || notes[0].Message != "Your repository has no remotes configured to push to." {
			t.Errorf("notes %+v", notes)
		}
		r.run(rp, "git.deleteRemoteTag", nil)
		notes = r.api.Notes()
		if len(notes) != 2 || notes[1].Severity != "error" || notes[1].Message != "Your repository has no remotes configured to push to." {
			t.Errorf("notes %+v", notes)
		}
	})
	t.Run("delete a tag at the remote", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		remote := repo.Git("remote", "get-url", "origin")
		repo.Git("push", "-q", "origin", "--tags")
		r.start(rp, "git.deleteRemoteTag", nil) // one remote: no list of remotes
		p := r.waitPick()
		if p.Options().Placeholder != "Select a remote tag to delete" || !equalLists(pickLabels(p), []string{"$(tag) v1", "$(tag) v2"}) {
			t.Errorf("pick = %q %v", p.Options().Placeholder, pickLabels(p))
		}
		if d := p.Options().Items[0].Description; !strings.HasPrefix(d, "Remote tag at ") {
			t.Errorf("description = %q", d)
		}
		p.Choose(0)
		r.idle(rp)
		if refExistsAt(repo, remote, "refs/tags/v1") || !refExistsAt(repo, remote, "refs/tags/v2") {
			t.Errorf("remote tags = %q", repo.Git("ls-remote", "--tags", remote))
		}
		if !refExists(repo, "refs/tags/v1") {
			t.Error("the local tag was deleted too")
		}
	})
	t.Run("a remote without tags says so", func(t *testing.T) {
		r, _, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.deleteRemoteTag", nil)
		p := r.waitPick()
		if got := pickLabels(p); !equalLists(got, []string{`$(info) Remote "origin" has no tags.`}) {
			t.Errorf("items = %v", got)
		}
		p.Close()
	})
	t.Run("two remotes ask which", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := refsRepo(t, e)
		other := repo.AddRemote("backup")
		repo.Git("push", "-q", "backup", "main")
		repo.Git("push", "-q", "backup", "--tags")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		rp := r.repo(repo.Dir)
		r.start(rp, "git.deleteRemoteTag", nil)
		first := r.nextPick(0)
		if first.Options().Placeholder != "Select a remote to delete a tag from" || len(first.Options().Items) != 2 {
			t.Fatalf("pick = %q %v", first.Options().Placeholder, pickLabels(first))
		}
		first.Choose(indexOf(t, first, "$(cloud) backup"))
		second := r.nextPick(1)
		second.Choose(indexOf(t, second, "$(tag) v2"))
		r.idle(rp)
		if refExistsAt(repo, other.Dir, "refs/tags/v2") || !refExistsAt(repo, other.Dir, "refs/tags/v1") {
			t.Errorf("backup tags = %q", repo.Git("ls-remote", "--tags", other.Dir))
		}
	})
}
