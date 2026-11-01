package gitscm

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// Fetch, pull, push, sync, publish and the remotes against real git and local bare repositories (step 24.15): each command runs through
// the registry of the fake API, the test answers the questions as the user does, and looks at what git says afterwards. Nothing here uses
// a network.

// syncWorld is a clone of a bare origin on main with the upstream set, and a second clone that plays another developer.
type syncWorld struct {
	env               *gittest.Env
	work, bare, other *gittest.Repo
}

func newSyncWorld(t *testing.T) *syncWorld {
	t.Helper()
	e := gittest.NewEnv(t)
	work := e.Init("work")
	work.Write("a.txt", "one\n")
	work.Commit("one")
	bare := work.AddRemote("origin")
	work.Git("push", "-q", "-u", "origin", "main")
	other := bare.Clone("other")
	work.Git("config", "pull.rebase", "false")
	other.Git("config", "pull.rebase", "false")
	return &syncWorld{env: e, work: work, bare: bare, other: other}
}

// open activates the extension on the work repository and waits for its first status.
func (w *syncWorld) open(t *testing.T, o rigOptions) (*rig, *repository) {
	t.Helper()
	o.folder = w.work.Dir
	r := newRig(t, w.env, o)
	r.settle()
	return r, r.repo(w.work.Dir)
}

// remoteCommit makes a commit in the other clone, pushes it to origin and returns its hash.
func (w *syncWorld) remoteCommit(file string) string {
	w.other.Git("pull", "-q", "--no-rebase", "origin", "main")
	w.other.Write(file, file+"\n")
	head := w.other.Commit("other " + file)
	w.other.Git("push", "-q", "origin", "main")
	return head
}

// localCommit makes a commit in the work repository.
func (w *syncWorld) localCommit(file string) string {
	w.work.Write(file, file+"\n")
	return w.work.Commit("mine " + file)
}

// stateOf waits for the status and returns how far the branch is from its upstream.
func stateOf(r *rig, rp *repository) (ahead, behind int) {
	r.t.Helper()
	r.refresh(rp)
	return rp.head.ahead, rp.head.behind
}

// startPick runs a command that shows a quick pick at once and returns the pick.
func (r *rig) startPick(cmd string, args any) *fakeapi.Pick {
	r.t.Helper()
	before := len(r.api.Picks())
	if err := r.api.Run(cmd, args); err != nil {
		r.t.Fatalf("%s: %v", cmd, err)
	}
	r.api.WaitFor("the pick of "+cmd, func() bool { return len(r.api.Picks()) > before })
	return r.api.LastPick()
}

func (r *rig) noteTexts() []string {
	var out []string
	for _, n := range r.api.Notes() {
		out = append(out, n.Severity+": "+n.Message)
	}
	return out
}

func TestFetch(t *testing.T) {
	t.Run("git.fetch fetches the only remote and the status shows it", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		head := w.remoteCommit("b.txt")
		w.other.Tag("v1", "")
		w.other.Git("push", "-q", "origin", "v1")
		r.run(rp, "git.fetch", nil)
		if got := w.work.Git("rev-parse", "origin/main"); got != head {
			t.Errorf("origin/main = %s, want %s", got, head)
		}
		if got := w.work.Git("tag", "--list", "v1"); got != "v1" {
			t.Errorf("tags %q", got)
		}
		if ahead, behind := stateOf(r, rp); ahead != 0 || behind != 1 {
			t.Errorf("ahead %d behind %d", ahead, behind)
		}
		items := r.source(rp.root).StatusBarCommands()
		if len(items) != 2 || items[1].Text != "$(sync) 1↓ 0↑" || items[1].Command != "git.sync" {
			t.Errorf("status bar %+v", items)
		}
		if notes := r.noteTexts(); len(notes) != 0 {
			t.Errorf("notifications %v", notes)
		}
	})
	t.Run("a repository with no remote says so", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("lonely")
		repo.Write("a.txt", "a\n")
		repo.Commit("one")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		rp := r.repo(repo.Dir)
		for cmd, want := range map[string]string{
			"git.fetch":        "This repository has no remotes configured to fetch from.",
			"git.fetchPrune":   "This repository has no remotes configured to fetch from.",
			"git.fetchAll":     "This repository has no remotes configured to fetch from.",
			"git.pull":         "Your repository has no remotes configured to pull from.",
			"git.pullRebase":   "Your repository has no remotes configured to pull from.",
			"git.pullFrom":     "Your repository has no remotes configured to pull from.",
			"git.removeRemote": "Your repository has no remotes.",
		} {
			n := len(r.api.Notes())
			r.run(rp, cmd, nil)
			notes := r.api.Notes()
			if len(notes) != n+1 || !strings.HasSuffix(notes[n].Message, want) && notes[n].Message != want {
				t.Errorf("%s: notifications %v, want %q", cmd, r.noteTexts()[n:], want)
			}
		}
	})
	t.Run("git.pruneOnFetch and Fetch (Prune)", func(t *testing.T) {
		w := newSyncWorld(t)
		w.other.Git("push", "-q", "origin", "main:topic")
		w.work.Git("fetch", "-q", "origin")
		w.other.Git("push", "-q", "origin", "--delete", "topic")
		r, rp := w.open(t, rigOptions{})
		r.run(rp, "git.fetch", nil)
		if got := w.work.Git("branch", "-r", "--list", "origin/topic"); got == "" {
			t.Fatal("a fetch pruned without being asked")
		}
		r.run(rp, "git.fetchPrune", nil)
		if got := w.work.Git("branch", "-r", "--list", "origin/topic"); got != "" {
			t.Errorf("Fetch (Prune) left %q", got)
		}
		// the setting prunes on every fetch
		w.other.Git("push", "-q", "origin", "main:topic2")
		w.work.Git("fetch", "-q", "origin")
		w.other.Git("push", "-q", "origin", "--delete", "topic2")
		r.api.SetSetting("git.pruneOnFetch", true)
		r.run(rp, "git.fetch", nil)
		if got := w.work.Git("branch", "-r", "--list", "origin/topic2"); got != "" {
			t.Errorf("git.pruneOnFetch left %q", got)
		}
	})
	t.Run("several remotes: a pick with the upstream's first and all remotes last", func(t *testing.T) {
		w := newSyncWorld(t)
		second := w.work.AddRemote("second")
		w.work.Git("push", "-q", "second", "main")
		w.work.Git("branch", "-u", "second/main")
		r, rp := w.open(t, rigOptions{})
		secondClone := second.Clone("second-clone")
		secondClone.Write("s.txt", "s\n")
		secondHead := secondClone.Commit("on second")
		secondClone.Git("push", "-q", "origin", "main")
		originHead := w.remoteCommit("b.txt")

		p := r.startPick("git.fetch", nil)
		items := p.Options().Items
		if len(items) != 4 || items[0].Label != "$(cloud) second" || items[1].Label != "$(cloud) origin" || !items[2].Separator || items[3].Label != "$(cloud-download) Fetch all remotes" {
			t.Fatalf("items %+v", items)
		}
		if items[1].Description != w.bare.Dir {
			t.Errorf("description %q", items[1].Description)
		}
		if p.Options().Placeholder != "Select a remote to fetch" {
			t.Errorf("placeholder %q", p.Options().Placeholder)
		}
		p.Choose(1) // origin only
		r.idle(rp)
		if got := w.work.Git("rev-parse", "origin/main"); got != originHead {
			t.Errorf("origin/main = %s", got)
		}
		if got := w.work.Git("rev-parse", "second/main"); got == secondHead {
			t.Error("the other remote was fetched too")
		}
		r.startPick("git.fetch", nil).Choose(3) // all
		r.idle(rp)
		if got := w.work.Git("rev-parse", "second/main"); got != secondHead {
			t.Errorf("second/main = %s, want %s", got, secondHead)
		}
	})
	t.Run("Fetch From All Remotes", func(t *testing.T) {
		w := newSyncWorld(t)
		second := w.work.AddRemote("second")
		w.work.Git("push", "-q", "second", "main")
		clone := second.Clone("second-clone")
		clone.Write("s.txt", "s\n")
		secondHead := clone.Commit("on second")
		clone.Git("push", "-q", "origin", "main")
		r, rp := w.open(t, rigOptions{})
		r.run(rp, "git.fetchAll", nil)
		if got := w.work.Git("rev-parse", "second/main"); got != secondHead {
			t.Errorf("second/main = %s", got)
		}
	})
	t.Run("git.fetchRef fetches the branch of a remote ref", func(t *testing.T) {
		w := newSyncWorld(t)
		w.other.Git("push", "-q", "origin", "main:topic")
		head := w.remoteCommit("b.txt")
		r, rp := w.open(t, rigOptions{})
		r.run(rp, "git.fetchRef", map[string]any{"ref": "refs/remotes/origin/main"})
		if got := w.work.Git("rev-parse", "origin/main"); got != head {
			t.Errorf("origin/main = %s", got)
		}
		if got := w.work.Git("branch", "-r", "--list", "origin/topic"); got != "" {
			t.Errorf("a ref fetched another branch: %q", got)
		}
		// with no ref, or a ref no remote has, nothing is done
		n := len(r.api.Notes())
		r.run(rp, "git.fetchRef", nil)
		r.run(rp, "git.fetchRef", map[string]any{"ref": "nowhere/main"})
		if len(r.api.Notes()) != n {
			t.Errorf("notifications %v", r.noteTexts())
		}
	})
	t.Run("a remote that cannot be reached is a failure with the output of git", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		w.work.Git("remote", "set-url", "origin", w.env.Root+"/nowhere.git")
		r.run(rp, "git.fetch", nil)
		note := r.lastNote()
		if note.Severity != "error" || !strings.HasPrefix(note.Message, "Git: ") || strings.Join(note.Actions, ",") != "Show Git Output" {
			t.Errorf("notification %+v", note)
		}
		if strings.Contains(note.Message, "Could not read from remote repository") {
			t.Errorf("the message is the last line of git, not the first: %q", note.Message)
		}
	})
}

func TestPull(t *testing.T) {
	t.Run("git.pull fast-forwards and the counts go to zero", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		head := w.remoteCommit("b.txt")
		r.run(rp, "git.fetch", nil)
		if _, behind := stateOf(r, rp); behind != 1 {
			t.Fatalf("behind %d", behind)
		}
		r.run(rp, "git.pull", nil)
		if got := w.work.Head(); got != head {
			t.Errorf("HEAD = %s, want %s", got, head)
		}
		if ahead, behind := stateOf(r, rp); ahead != 0 || behind != 0 {
			t.Errorf("ahead %d behind %d", ahead, behind)
		}
		if items := r.source(rp.root).StatusBarCommands(); items[1].Text != "$(sync)" {
			t.Errorf("sync item %q", items[1].Text)
		}
	})
	t.Run("git.pullRebase rebases the local commits", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		head := w.remoteCommit("b.txt")
		w.localCommit("c.txt")
		r.run(rp, "git.pullRebase", nil)
		if got := w.work.Git("rev-parse", "HEAD~1"); got != head {
			t.Errorf("HEAD~1 = %s, want %s", got, head)
		}
		if got := w.work.Git("rev-list", "--merges", "--count", "HEAD"); got != "0" {
			t.Errorf("%s merges", got)
		}
	})
	t.Run("git.pull merges when git is told to", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		w.remoteCommit("b.txt")
		w.localCommit("c.txt")
		r.run(rp, "git.pull", nil)
		if got := w.work.Git("rev-list", "--merges", "--count", "HEAD"); got != "1" {
			t.Errorf("%s merges", got)
		}
		if ahead, behind := stateOf(r, rp); ahead != 2 || behind != 0 {
			t.Errorf("ahead %d behind %d", ahead, behind)
		}
	})
	t.Run("a conflict is a warning and leaves the merge to resolve", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		w.remoteCommit("a.txt")
		w.work.Write("a.txt", "mine\n")
		w.work.Commit("mine")
		r.run(rp, "git.pull", nil)
		note := r.lastNote()
		if note.Severity != "warn" || note.Message != "There are merge conflicts. Please resolve them before committing your changes." {
			t.Errorf("notification %+v", note)
		}
		if !rp.ops.merge {
			t.Error("no merge in progress")
		}
		if got := r.paths(rp.root, groupMerge); strings.Join(got, ",") != "a.txt" {
			t.Errorf("Merge Changes %v", got)
		}
	})
	t.Run("local changes in the way are a failure", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		w.remoteCommit("a.txt")
		w.work.Write("a.txt", "edited\n")
		r.refresh(rp)
		r.run(rp, "git.pull", nil)
		note := r.lastNote()
		if note.Severity != "error" || !strings.HasPrefix(note.Message, "Git: ") {
			t.Fatalf("notification %+v", note)
		}
		if got := w.work.Read("a.txt"); got != "edited\n" {
			t.Errorf("a.txt = %q", got)
		}
	})
	t.Run("git.autoStash stashes them for the pull and brings them back", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{settings: map[string]any{"git.autoStash": true}})
		w.remoteCommit("b.txt")
		w.work.Write("a.txt", "edited\n")
		r.refresh(rp)
		r.run(rp, "git.pull", nil)
		if got := w.work.Read("a.txt"); got != "edited\n" {
			t.Errorf("a.txt = %q, the change did not come back", got)
		}
		if got := w.work.Read("b.txt"); got != "b.txt\n" {
			t.Errorf("b.txt = %q, the pull did not happen", got)
		}
		if got := w.work.Git("stash", "list"); got != "" {
			t.Errorf("stash left: %q", got)
		}
		if notes := r.noteTexts(); len(notes) != 0 {
			t.Errorf("notifications %v", notes)
		}
	})
	t.Run("git.pullFrom: a pick of the branches of the remote", func(t *testing.T) {
		w := newSyncWorld(t)
		w.other.Git("checkout", "-q", "-b", "topic")
		w.other.Write("t.txt", "t\n")
		topic := w.other.Commit("topic")
		w.other.Git("push", "-q", "origin", "topic")
		r, rp := w.open(t, rigOptions{})
		w.work.Git("fetch", "-q", "origin")
		r.start(rp, "git.pullFrom", nil)
		p := r.waitPick()
		if got := p.Labels(); strings.Join(got, ",") != "$(cloud) origin/topic,$(cloud) origin/main" && strings.Join(got, ",") != "$(cloud) origin/main,$(cloud) origin/topic" {
			t.Fatalf("items %v", got)
		}
		if p.Options().Placeholder != "Pick a branch to pull from" {
			t.Errorf("placeholder %q", p.Options().Placeholder)
		}
		for i, label := range p.Labels() {
			if label == "$(cloud) origin/topic" {
				p.Choose(i)
			}
		}
		r.idle(rp)
		if got := w.work.Head(); got != topic {
			t.Errorf("HEAD = %s, want %s", got, topic)
		}
		if got := w.work.Read("t.txt"); got != "t\n" {
			t.Errorf("t.txt = %q", got)
		}
	})
	t.Run("git.pullRef pulls the branch a ref names", func(t *testing.T) {
		w := newSyncWorld(t)
		head := w.remoteCommit("b.txt")
		r, rp := w.open(t, rigOptions{})
		r.run(rp, "git.pullRef", map[string]any{"ref": "origin/main"})
		if got := w.work.Head(); got != head {
			t.Errorf("HEAD = %s, want %s", got, head)
		}
	})
	t.Run("git.fetchOnPull fetches every branch", func(t *testing.T) {
		for _, on := range []bool{false, true} {
			w := newSyncWorld(t)
			w.other.Git("push", "-q", "origin", "main:topic")
			r, rp := w.open(t, rigOptions{settings: map[string]any{"git.fetchOnPull": on}})
			w.remoteCommit("b.txt")
			r.run(rp, "git.pull", nil)
			got := w.work.Git("branch", "-r", "--list", "origin/topic") != ""
			if got != on {
				t.Errorf("git.fetchOnPull %v: origin/topic fetched %v", on, got)
			}
		}
	})
	t.Run("git.pullTags", func(t *testing.T) {
		for _, on := range []bool{true, false} {
			w := newSyncWorld(t)
			// a tag on a commit that main does not have is not followed by a plain pull
			w.other.Git("checkout", "-q", "-b", "side")
			w.other.Write("side.txt", "s\n")
			w.other.Commit("side")
			w.other.Tag("side-tag", "")
			w.other.Git("push", "-q", "origin", "side-tag")
			r, rp := w.open(t, rigOptions{settings: map[string]any{"git.pullTags": on}})
			w.remoteCommit("b.txt")
			r.run(rp, "git.pull", nil)
			got := w.work.Git("tag", "--list", "side-tag") != ""
			if got != on {
				t.Errorf("git.pullTags %v: tag fetched %v", on, got)
			}
		}
	})
}

// rebasedWorld makes a branch whose upstream has a commit with the same change as one of its own: the branch was rebased.
func rebasedWorld(t *testing.T) *syncWorld {
	t.Helper()
	w := newSyncWorld(t)
	w.work.Write("b.txt", "bee\n")
	w.work.Commit("add b")
	w.work.Git("push", "-q", "origin", "main")
	w.work.Git("reset", "-q", "--hard", "HEAD~1")
	w.work.Write("z.txt", "zed\n")
	w.work.Commit("new base")
	w.work.Write("b.txt", "bee\n")
	w.work.Commit("add b")
	w.work.Git("fetch", "-q", "origin")
	w.remoteCommit("later.txt")
	return w
}

func TestPullIntoABranchThatMightHaveBeenRebased(t *testing.T) {
	tests := []struct {
		name       string
		button     string
		pulled     bool
		settingKey string
	}{
		{"Pull", "Pull", true, ""},
		{"Always Pull", "Always Pull", true, "git.ignoreRebaseWarning"},
		{"Don't Pull", "Don't Pull", false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := rebasedWorld(t)
			r, rp := w.open(t, rigOptions{})
			before := w.work.Head()
			r.start(rp, "git.pull", nil)
			d := r.theOnlyDialog()
			want := `It looks like the current branch "main" might have been rebased. Are you sure you still want to pull into it?`
			if d.Spec().Message != want || d.Spec().Type != extension.DialogWarning {
				t.Errorf("the question is %+v", d.Spec())
			}
			if labels := dialogLabels(d.Spec()); strings.Join(labels, ",") != "Always Pull,Pull,Don't Pull" {
				t.Errorf("buttons %v", labels)
			}
			d.Press(tc.button)
			r.idle(rp)
			if pulled := w.work.Head() != before; pulled != tc.pulled {
				t.Errorf("pulled %v, want %v", pulled, tc.pulled)
			}
			var updates []string
			for _, u := range r.api.SettingUpdates() {
				updates = append(updates, u.Key)
			}
			if tc.settingKey == "" && len(updates) != 0 || tc.settingKey != "" && strings.Join(updates, ",") != tc.settingKey {
				t.Errorf("setting updates %v", updates)
			}
		})
	}
	t.Run("git.ignoreRebaseWarning asks nothing", func(t *testing.T) {
		w := rebasedWorld(t)
		r, rp := w.open(t, rigOptions{settings: map[string]any{"git.ignoreRebaseWarning": true}})
		before := w.work.Head()
		r.run(rp, "git.pull", nil)
		if w.work.Head() == before {
			t.Error("no pull")
		}
	})
	t.Run("Escape does not pull", func(t *testing.T) {
		w := rebasedWorld(t)
		r, rp := w.open(t, rigOptions{})
		before := w.work.Head()
		r.start(rp, "git.pull", nil)
		r.theOnlyDialog().Cancel()
		r.idle(rp)
		if w.work.Head() != before {
			t.Error("a pull after Escape")
		}
	})
}

func dialogLabels(spec extension.DialogSpec) []string {
	var out []string
	for _, b := range spec.Buttons {
		out = append(out, b.Label)
	}
	return out
}

// tagConflictWorld makes a pull stop on a tag that the remote moved.
func tagConflictWorld(t *testing.T) *syncWorld {
	t.Helper()
	w := newSyncWorld(t)
	w.work.Tag("v1", "")
	w.other.Git("pull", "-q")
	w.other.Git("tag", "v1")
	w.other.Write("b.txt", "bee\n")
	w.other.Commit("later")
	w.other.Git("tag", "-f", "v1")
	w.other.Git("push", "-q", "-f", "origin", "main", "v1")
	return w
}

func TestAPullThatMergesNeedsTheNameOfTheUser(t *testing.T) {
	w := newSyncWorld(t)
	w.remoteCommit("b.txt")
	w.localCommit("c.txt")
	w.env.Identity = false // git does not know who the author of the merge commit is
	r, rp := w.open(t, rigOptions{})
	r.run(rp, "git.pull", nil)
	note := r.lastNote()
	if note.Severity != "error" || note.Message != `Make sure you configure your "user.name" and "user.email" in git.` || strings.Join(note.Actions, ",") != "Set Name and E-mail...,Show Git Output" {
		t.Fatalf("notification %+v", note)
	}
}

func TestPullStopsOnTagsThatDiffer(t *testing.T) {
	t.Run("Replace Local Tag(s)", func(t *testing.T) {
		w := tagConflictWorld(t)
		r, rp := w.open(t, rigOptions{})
		r.start(rp, "git.pull", nil)
		d := r.theOnlyDialog()
		if want := "Unable to pull from remote repository due to conflicting tag(s): v1. Would you like to resolve the conflict by replacing the local tag(s)?"; d.Spec().Message != want {
			t.Errorf("question %q", d.Spec().Message)
		}
		if labels := dialogLabels(d.Spec()); strings.Join(labels, ",") != "Replace Local Tag(s),Always Replace Local Tag(s),Cancel" {
			t.Errorf("buttons %v", labels)
		}
		d.Press("Replace Local Tag(s)")
		r.idle(rp)
		if got, want := w.work.Git("rev-parse", "v1^{commit}"), w.other.Git("rev-parse", "v1^{commit}"); got != want {
			t.Errorf("v1 = %s, want %s", got, want)
		}
		if got := w.work.Head(); got != w.other.Head() {
			t.Errorf("HEAD = %s: the pull did not run again", got)
		}
		if len(r.api.SettingUpdates()) != 0 {
			t.Errorf("updates %v", r.api.SettingUpdates())
		}
	})
	t.Run("Always Replace Local Tag(s) is kept in the settings", func(t *testing.T) {
		w := tagConflictWorld(t)
		r, rp := w.open(t, rigOptions{})
		r.start(rp, "git.pull", nil)
		r.theOnlyDialog().Press("Always Replace Local Tag(s)")
		r.idle(rp)
		ups := r.api.SettingUpdates()
		if len(ups) != 1 || ups[0].Key != "git.replaceTagsWhenPull" || ups[0].Value != true {
			t.Errorf("updates %+v", ups)
		}
	})
	t.Run("git.replaceTagsWhenPull asks nothing", func(t *testing.T) {
		w := tagConflictWorld(t)
		r, rp := w.open(t, rigOptions{settings: map[string]any{"git.replaceTagsWhenPull": true}})
		r.run(rp, "git.pull", nil)
		if got, want := w.work.Git("rev-parse", "v1^{commit}"), w.other.Git("rev-parse", "v1^{commit}"); got != want {
			t.Errorf("v1 = %s, want %s", got, want)
		}
	})
	t.Run("Cancel leaves the tag and the branch", func(t *testing.T) {
		w := tagConflictWorld(t)
		r, rp := w.open(t, rigOptions{})
		before, tag := w.work.Head(), w.work.Git("rev-parse", "v1^{commit}")
		r.start(rp, "git.pull", nil)
		r.theOnlyDialog().Cancel()
		r.idle(rp)
		if w.work.Head() != before || w.work.Git("rev-parse", "v1^{commit}") != tag {
			t.Error("something moved")
		}
		if notes := r.noteTexts(); len(notes) != 0 {
			t.Errorf("notifications %v", notes)
		}
	})
}

func TestPush(t *testing.T) {
	t.Run("git.push sends the commits and the status follows", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		head := w.localCommit("b.txt")
		r.refresh(rp)
		r.run(rp, "git.push", nil)
		if got := w.bare.Git("rev-parse", "main"); got != head {
			t.Errorf("origin/main = %s, want %s", got, head)
		}
		if ahead, behind := stateOf(r, rp); ahead != 0 || behind != 0 {
			t.Errorf("ahead %d behind %d", ahead, behind)
		}
		if notes := r.noteTexts(); len(notes) != 0 {
			t.Errorf("notifications %v", notes)
		}
	})
	t.Run("git.showPushSuccessNotification", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{settings: map[string]any{"git.showPushSuccessNotification": true}})
		w.localCommit("b.txt")
		r.run(rp, "git.push", nil)
		if notes := r.noteTexts(); len(notes) != 1 || notes[0] != "info: Successfully pushed." {
			t.Errorf("notifications %v", notes)
		}
	})
	t.Run("a repository with no remote offers to add one", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("lonely")
		repo.Write("a.txt", "a\n")
		repo.Commit("one")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		rp := r.repo(repo.Dir)
		r.run(rp, "git.push", nil)
		note := r.lastNote()
		if note.Severity != "warn" || note.Message != "Your repository has no remotes configured to push to." || strings.Join(note.Actions, ",") != "Add Remote" {
			t.Fatalf("notification %+v", note)
		}
		r.api.RunNoteAction(len(r.api.Notes())-1, "Add Remote")
		if r.nextInput(0).Options().Prompt != "Provide repository URL" {
			t.Error("Add Remote did not ask for the URL")
		}
	})
	t.Run("a rejected push says so and offers Pull", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		remote := w.remoteCommit("b.txt")
		w.localCommit("c.txt")
		r.run(rp, "git.push", nil)
		note := r.lastNote()
		if note.Severity != "error" || note.Message != `Can't push refs to remote. Try running "Pull" first to integrate your changes.` || strings.Join(note.Actions, ",") != "Pull,Show Git Output" {
			t.Fatalf("notification %+v", note)
		}
		if got := w.bare.Git("rev-parse", "main"); got != remote {
			t.Error("the remote moved")
		}
		r.api.RunNoteAction(len(r.api.Notes())-1, "Pull")
		r.idle(rp)
		if got := w.work.Git("merge-base", "--is-ancestor", remote, "HEAD"); got != "" {
			t.Errorf("the pull did not bring %s", remote)
		}
		r.run(rp, "git.push", nil)
		if got := w.bare.Git("rev-parse", "main"); got != w.work.Head() {
			t.Error("the push after the pull did not go through")
		}
	})
	t.Run("git.push with no upstream asks to publish the branch", func(t *testing.T) {
		w := newSyncWorld(t)
		w.work.Git("checkout", "-q", "-b", "topic")
		w.work.Git("config", "push.autoSetupRemote", "false")
		head := w.localCommit("t.txt")
		r, rp := w.open(t, rigOptions{})
		r.start(rp, "git.push", nil)
		d := r.theOnlyDialog()
		if d.Spec().Message != `The branch "topic" has no remote branch. Would you like to publish this branch?` || strings.Join(dialogLabels(d.Spec()), ",") != "OK,OK, Don't Ask Again,Cancel" {
			t.Errorf("question %+v", d.Spec())
		}
		d.Press("OK")
		r.idle(rp)
		if got := w.bare.Git("rev-parse", "topic"); got != head {
			t.Errorf("topic at the remote = %s", got)
		}
		if got := w.work.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); got != "origin/topic" {
			t.Errorf("upstream = %q", got)
		}
	})
	t.Run("OK, Don't Ask Again is kept in the state of the profile", func(t *testing.T) {
		w := newSyncWorld(t)
		w.work.Git("checkout", "-q", "-b", "topic")
		w.localCommit("t.txt")
		r, rp := w.open(t, rigOptions{})
		r.start(rp, "git.push", nil)
		r.theOnlyDialog().Press("OK, Don't Ask Again")
		r.idle(rp)
		if got := w.bare.Git("branch", "--list", "topic"); !strings.Contains(got, "topic") {
			t.Fatalf("not published: %q", got)
		}
		// another branch, another window: no question
		w.work.Git("checkout", "-q", "-b", "second")
		w.localCommit("s.txt")
		r2 := newRig(t, w.env, rigOptions{folder: w.work.Dir})
		r2.settle()
		rp2 := r2.repo(w.work.Dir)
		r2.run(rp2, "git.push", nil)
		if !r2.quiet() {
			t.Error("the question was asked again")
		}
		if got := w.bare.Git("branch", "--list", "second"); !strings.Contains(got, "second") {
			t.Errorf("second not published: %q", got)
		}
	})
	t.Run("Cancel publishes nothing", func(t *testing.T) {
		w := newSyncWorld(t)
		w.work.Git("checkout", "-q", "-b", "topic")
		w.work.Git("config", "push.autoSetupRemote", "false")
		w.localCommit("t.txt")
		r, rp := w.open(t, rigOptions{})
		r.start(rp, "git.push", nil)
		r.theOnlyDialog().Press("Cancel")
		r.idle(rp)
		if got := w.bare.Git("branch", "--list", "topic"); got != "" {
			t.Errorf("published %q", got)
		}
	})
	t.Run("a detached HEAD has no branch to push", func(t *testing.T) {
		w := newSyncWorld(t)
		w.work.Git("checkout", "-q", "--detach")
		r, rp := w.open(t, rigOptions{})
		r.run(rp, "git.push", nil)
		if note := r.lastNote(); note.Message != "Please check out a branch to push to a remote." {
			t.Errorf("notification %+v", note)
		}
	})
	t.Run("git.pushTo asks for the remote", func(t *testing.T) {
		w := newSyncWorld(t)
		second := w.work.AddRemote("second")
		head := w.localCommit("b.txt")
		r, rp := w.open(t, rigOptions{})
		p := r.startPick("git.pushTo", nil)
		if want := `Pick a remote to publish the branch "main" to:`; p.Options().Placeholder != want {
			t.Errorf("placeholder %q", p.Options().Placeholder)
		}
		labels := p.Labels()
		if len(labels) != 3 || labels[0] != "origin" || labels[1] != "second" || labels[2] != "$(plus) Add a new remote..." {
			t.Fatalf("items %v", labels)
		}
		p.Choose(1)
		r.idle(rp)
		if got := second.Git("rev-parse", "main"); got != head {
			t.Errorf("second/main = %s", got)
		}
		if got := w.bare.Git("rev-parse", "main"); got == head {
			t.Error("origin got the commit too")
		}
	})
	t.Run("git.pushTo with a remote, a refspec and -u as arguments asks nothing", func(t *testing.T) {
		w := newSyncWorld(t)
		second := w.work.AddRemote("second")
		head := w.localCommit("b.txt")
		r, rp := w.open(t, rigOptions{})
		r.run(rp, "git.pushTo", map[string]any{"remote": "second", "refspec": "main:trunk", "setUpstream": true})
		if got := second.Git("rev-parse", "trunk"); got != head {
			t.Errorf("trunk = %s", got)
		}
		if got := w.work.Git("rev-parse", "--abbrev-ref", "main@{upstream}"); got != "second/trunk" {
			t.Errorf("upstream = %q", got)
		}
	})
	t.Run("git.pushWithTags pushes the annotated tags of the commits", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		w.localCommit("b.txt")
		w.work.Tag("light", "")
		w.work.Tag("annotated", "release")
		r.run(rp, "git.pushWithTags", nil)
		if got := w.bare.Git("tag", "--list"); got != "annotated" {
			t.Errorf("tags at the remote %q", got)
		}
		if got := w.bare.Git("rev-parse", "main"); got != w.work.Head() {
			t.Error("the branch was not pushed")
		}
	})
	t.Run("git.pushTags pushes every tag and stops", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		w.work.Tag("light", "")
		w.work.Tag("annotated", "release")
		r.run(rp, "git.pushTags", nil)
		if got := w.bare.Git("tag", "--list"); got != "annotated\nlight" {
			t.Errorf("tags at the remote %q", got)
		}
		if len(r.api.Picks()) != 0 || len(r.api.Notes()) != 0 {
			t.Errorf("picks %d notifications %v", len(r.api.Picks()), r.noteTexts())
		}
	})
}

func TestForcePush(t *testing.T) {
	amended := func(t *testing.T, o rigOptions) (*syncWorld, *rig, *repository) {
		w := newSyncWorld(t)
		w.work.Git("commit", "-q", "--amend", "-m", "one, amended")
		r, rp := w.open(t, o)
		return w, r, rp
	}
	t.Run("not allowed by default", func(t *testing.T) {
		w, r, rp := amended(t, rigOptions{})
		before := w.bare.Git("rev-parse", "main")
		for _, cmd := range []string{"git.pushForce", "git.pushToForce", "git.pushWithTagsForce"} {
			r.run(rp, cmd, nil)
			if note := r.lastNote(); note.Severity != "error" || note.Message != `Force push is not allowed, please enable it with the "git.allowForcePush" setting.` {
				t.Errorf("%s: notification %+v", cmd, note)
			}
		}
		if w.bare.Git("rev-parse", "main") != before {
			t.Error("the remote moved")
		}
	})
	t.Run("confirmed, with lease and if-includes", func(t *testing.T) {
		w, r, rp := amended(t, rigOptions{settings: map[string]any{"git.allowForcePush": true}})
		r.start(rp, "git.pushForce", nil)
		d := r.theOnlyDialog()
		if d.Spec().Message != "You are about to force push your changes, this can be destructive and could inadvertently overwrite changes made by others." || d.Spec().Detail != "Are you sure to continue?" {
			t.Errorf("question %+v", d.Spec())
		}
		d.Press("OK")
		r.idle(rp)
		if got := w.bare.Git("rev-parse", "main"); got != w.work.Head() {
			t.Errorf("origin/main = %s", got)
		}
		if lines := r.api.Channel(ChannelName).Lines(); !hasLine(lines, "--force-with-lease --force-if-includes") {
			t.Errorf("the command line is not in the Git channel: %v", lines)
		}
	})
	t.Run("Cancel pushes nothing", func(t *testing.T) {
		w, r, rp := amended(t, rigOptions{settings: map[string]any{"git.allowForcePush": true}})
		before := w.bare.Git("rev-parse", "main")
		r.start(rp, "git.pushForce", nil)
		r.theOnlyDialog().Press("Cancel")
		r.idle(rp)
		if w.bare.Git("rev-parse", "main") != before {
			t.Error("the remote moved")
		}
	})
	t.Run("OK, Don't Ask Again turns the confirmation off", func(t *testing.T) {
		_, r, rp := amended(t, rigOptions{settings: map[string]any{"git.allowForcePush": true}})
		r.start(rp, "git.pushForce", nil)
		r.theOnlyDialog().Press("OK, Don't Ask Again")
		r.idle(rp)
		ups := r.api.SettingUpdates()
		if len(ups) != 1 || ups[0].Key != "git.confirmForcePush" || ups[0].Value != false {
			t.Errorf("updates %+v", ups)
		}
	})
	t.Run("git.confirmForcePush off, lease off: a plain force", func(t *testing.T) {
		w, r, rp := amended(t, rigOptions{settings: map[string]any{"git.allowForcePush": true, "git.confirmForcePush": false, "git.useForcePushWithLease": false}})
		w.remoteCommit("b.txt") // the remote moved: only a plain force goes through
		r.run(rp, "git.pushForce", nil)
		if got := w.bare.Git("rev-parse", "main"); got != w.work.Head() {
			t.Errorf("origin/main = %s", got)
		}
		if lines := r.api.Channel(ChannelName).Lines(); !hasLine(lines, "push --force -- origin main:main") {
			t.Errorf("channel %v", lines)
		}
	})
	t.Run("with lease, a remote that moved refuses it and says why", func(t *testing.T) {
		w, r, rp := amended(t, rigOptions{settings: map[string]any{"git.allowForcePush": true, "git.confirmForcePush": false, "git.useForcePushIfIncludes": false}})
		moved := w.remoteCommit("b.txt")
		r.run(rp, "git.pushForce", nil)
		note := r.lastNote()
		if note.Severity != "error" || !strings.HasPrefix(note.Message, "Can't force push refs to remote. The tip of the remote-tracking branch has been updated since the last checkout.") || strings.Join(note.Actions, ",") != "Pull,Show Git Output" {
			t.Errorf("notification %+v", note)
		}
		if got := w.bare.Git("rev-parse", "main"); got != moved {
			t.Error("the remote was overwritten")
		}
	})
	t.Run("git.pushToForce with arguments", func(t *testing.T) {
		w, r, rp := amended(t, rigOptions{settings: map[string]any{"git.allowForcePush": true, "git.confirmForcePush": false}})
		r.run(rp, "git.pushToForce", map[string]any{"remote": "origin", "refspec": "main"})
		if got := w.bare.Git("rev-parse", "main"); got != w.work.Head() {
			t.Errorf("origin/main = %s", got)
		}
	})
}

func hasLine(lines []string, part string) bool {
	for _, l := range lines {
		if strings.Contains(l, part) {
			return true
		}
	}
	return false
}

func TestSync(t *testing.T) {
	t.Run("pulls then pushes after the confirmation", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		remote := w.remoteCommit("b.txt")
		w.localCommit("c.txt")
		r.start(rp, "git.sync", nil)
		d := r.theOnlyDialog()
		if want := `This action will pull and push commits from and to "origin/main".`; d.Spec().Message != want || strings.Join(dialogLabels(d.Spec()), ",") != "OK,OK, Don't Show Again,Cancel" {
			t.Errorf("question %+v", d.Spec())
		}
		d.Press("OK")
		r.idle(rp)
		if got := w.bare.Git("rev-parse", "main"); got != w.work.Head() {
			t.Errorf("origin/main = %s, HEAD %s", got, w.work.Head())
		}
		if w.work.Git("merge-base", "--is-ancestor", remote, "HEAD") != "" {
			t.Error("the remote's commit is not in HEAD")
		}
		if ahead, behind := stateOf(r, rp); ahead != 0 || behind != 0 {
			t.Errorf("ahead %d behind %d", ahead, behind)
		}
	})
	t.Run("Cancel does nothing", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		w.localCommit("c.txt")
		before := w.bare.Git("rev-parse", "main")
		r.start(rp, "git.sync", nil)
		r.theOnlyDialog().Press("Cancel")
		r.idle(rp)
		if w.bare.Git("rev-parse", "main") != before {
			t.Error("pushed")
		}
	})
	t.Run("OK, Don't Show Again turns git.confirmSync off", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		w.localCommit("c.txt")
		r.start(rp, "git.sync", nil)
		r.theOnlyDialog().Press("OK, Don't Show Again")
		r.idle(rp)
		ups := r.api.SettingUpdates()
		if len(ups) != 1 || ups[0].Key != "git.confirmSync" || ups[0].Value != false {
			t.Errorf("updates %+v", ups)
		}
		if got := w.bare.Git("rev-parse", "main"); got != w.work.Head() {
			t.Error("not pushed")
		}
	})
	t.Run("git.confirmSync off asks nothing, and a branch that is not ahead pushes nothing", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{settings: map[string]any{"git.confirmSync": false}})
		head := w.remoteCommit("b.txt")
		r.run(rp, "git.sync", nil)
		if w.work.Head() != head {
			t.Error("not pulled")
		}
		if lines := r.api.Channel(ChannelName).Lines(); hasLine(lines, "> git push") {
			t.Errorf("a push ran with nothing to push: %v", lines)
		}
	})
	t.Run("git.syncRebase and git.rebaseWhenSync rebase", func(t *testing.T) {
		for _, tc := range []struct {
			cmd      string
			settings map[string]any
		}{
			{"git.syncRebase", map[string]any{"git.confirmSync": false}},
			{"git.sync", map[string]any{"git.confirmSync": false, "git.rebaseWhenSync": true}},
		} {
			w := newSyncWorld(t)
			r, rp := w.open(t, rigOptions{settings: tc.settings})
			remote := w.remoteCommit("b.txt")
			w.localCommit("c.txt")
			r.run(rp, tc.cmd, nil)
			if got := w.work.Git("rev-parse", "HEAD~1"); got != remote {
				t.Errorf("%s: HEAD~1 = %s, want %s", tc.cmd, got, remote)
			}
			if got := w.bare.Git("rev-parse", "main"); got != w.work.Head() {
				t.Errorf("%s: not pushed", tc.cmd)
			}
		}
	})
	t.Run("git.followTagsWhenSync", func(t *testing.T) {
		for _, on := range []bool{false, true} {
			w := newSyncWorld(t)
			r, rp := w.open(t, rigOptions{settings: map[string]any{"git.confirmSync": false, "git.followTagsWhenSync": on}})
			w.localCommit("c.txt")
			w.work.Tag("rel", "release")
			r.run(rp, "git.sync", nil)
			got := w.bare.Git("tag", "--list") == "rel"
			if got != on {
				t.Errorf("git.followTagsWhenSync %v: tag pushed %v", on, got)
			}
		}
	})
	t.Run("a read-only remote is pulled from and not pushed to", func(t *testing.T) {
		w := newSyncWorld(t)
		w.work.Git("remote", "set-url", "--push", "origin", "no_push")
		r, rp := w.open(t, rigOptions{})
		head := w.remoteCommit("b.txt")
		w.localCommit("c.txt")
		before := w.bare.Git("rev-parse", "main")
		r.run(rp, "git.sync", nil) // no question: there is nothing to confirm about a push
		if w.work.Git("merge-base", "--is-ancestor", head, "HEAD") != "" {
			t.Error("not pulled")
		}
		if w.bare.Git("rev-parse", "main") != before {
			t.Error("pushed to a read-only remote")
		}
		if items := r.source(rp.root).StatusBarCommands(); !strings.HasSuffix(items[1].Text, "↓") && !strings.HasPrefix(items[1].Text, "$(sync)") {
			t.Errorf("sync item %q", items[1].Text)
		}
	})
	t.Run("a branch with no upstream is published", func(t *testing.T) {
		w := newSyncWorld(t)
		w.work.Git("checkout", "-q", "-b", "topic")
		w.work.Git("config", "push.autoSetupRemote", "false")
		head := w.localCommit("t.txt")
		r, rp := w.open(t, rigOptions{})
		r.start(rp, "git.sync", nil)
		r.theOnlyDialog().Press("OK") // the question about publishing
		r.idle(rp)
		if got := w.bare.Git("rev-parse", "topic"); got != head {
			t.Errorf("topic = %s", got)
		}
	})
	t.Run("a conflict stops the sync before the push", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{settings: map[string]any{"git.confirmSync": false}})
		remote := w.remoteCommit("a.txt")
		w.work.Write("a.txt", "mine\n")
		w.work.Commit("mine")
		r.run(rp, "git.sync", nil)
		if note := r.lastNote(); note.Severity != "warn" || !strings.HasPrefix(note.Message, "There are merge conflicts.") {
			t.Errorf("notification %+v", note)
		}
		if got := w.bare.Git("rev-parse", "main"); got != remote {
			t.Error("pushed in the middle of a conflict")
		}
	})
	t.Run("a push the remote refuses after the pull", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{settings: map[string]any{"git.confirmSync": false}})
		w.localCommit("c.txt")
		// a hook of the remote refuses everything, with its own message
		w.bare.Hook("pre-receive", "#!/bin/sh\necho 'protected branch: pushes to main are not allowed' >&2\nexit 1\n")
		r.run(rp, "git.sync", nil)
		note := r.lastNote()
		if note.Severity != "error" || note.Message != "Git: protected branch: pushes to main are not allowed" || strings.Join(note.Actions, ",") != "Show Git Output" {
			t.Fatalf("notification %+v", note)
		}
		var out string
		for _, l := range r.api.Channel(ChannelName).Lines() {
			out += l + "\n"
		}
		if !strings.Contains(out, "protected branch: pushes to main are not allowed") {
			t.Errorf("the message of the remote is not in the Git channel:\n%s", out)
		}
	})
}

func TestPublish(t *testing.T) {
	t.Run("with one remote the branch is pushed with -u", func(t *testing.T) {
		w := newSyncWorld(t)
		w.work.Git("checkout", "-q", "-b", "topic")
		head := w.localCommit("t.txt")
		r, rp := w.open(t, rigOptions{})
		r.run(rp, "git.publish", nil)
		if got := w.bare.Git("rev-parse", "topic"); got != head {
			t.Errorf("topic = %s", got)
		}
		if got := w.work.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); got != "origin/topic" {
			t.Errorf("upstream = %q", got)
		}
		items := r.source(rp.root).StatusBarCommands()
		if items[1].Command != "git.sync" {
			t.Errorf("sync item %+v after publishing", items[1])
		}
	})
	t.Run("with several remotes it asks which", func(t *testing.T) {
		w := newSyncWorld(t)
		second := w.work.AddRemote("second")
		w.work.Git("checkout", "-q", "-b", "topic")
		head := w.localCommit("t.txt")
		r, rp := w.open(t, rigOptions{})
		p := r.startPick("git.publish", nil)
		if want := `Pick a remote to publish the branch "topic" to:`; p.Options().Placeholder != want {
			t.Errorf("placeholder %q", p.Options().Placeholder)
		}
		p.Choose(1)
		r.idle(rp)
		if got := second.Git("rev-parse", "topic"); got != head {
			t.Errorf("second/topic = %s", got)
		}
		if got := w.work.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); got != "second/topic" {
			t.Errorf("upstream = %q", got)
		}
	})
	t.Run("with no remote it adds one first and creates no service repository", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("lonely")
		repo.Write("a.txt", "a\n")
		head := repo.Commit("one")
		target := e.InitBare("target.git")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		rp := r.repo(repo.Dir)
		items := r.source(rp.root).StatusBarCommands()
		if len(items) != 2 || items[1].Text != "$(cloud-upload)" || items[1].Tooltip != "Publish Branch" || items[1].Command != "git.publish" {
			t.Fatalf("status bar %+v", items)
		}
		r.start(rp, "git.publish", nil)
		r.nextInput(0).Accept(target.Dir)
		r.nextInput(1).Accept("origin")
		r.idle(rp)
		if got := target.Git("rev-parse", "main"); got != head {
			t.Errorf("target main = %s", got)
		}
		if got := repo.Git("rev-parse", "--abbrev-ref", "main@{upstream}"); got != "origin/main" {
			t.Errorf("upstream = %q", got)
		}
		if notes := r.noteTexts(); len(notes) != 0 {
			t.Errorf("notifications %v", notes)
		}
	})
	t.Run("a detached HEAD cannot be published", func(t *testing.T) {
		w := newSyncWorld(t)
		w.work.Git("checkout", "-q", "--detach")
		r, rp := w.open(t, rigOptions{})
		r.run(rp, "git.publish", nil)
		if note := r.lastNote(); note.Message != "Please check out a branch to push to a remote." {
			t.Errorf("notification %+v", note)
		}
	})
}

func TestAddAndRemoveARemote(t *testing.T) {
	t.Run("the URL is validated, the name sanitized, and the remote is fetched", func(t *testing.T) {
		w := newSyncWorld(t)
		second := w.env.InitBare("second.git")
		seed := second.Clone("seed")
		seed.Write("s.txt", "s\n")
		seedHead := seed.Commit("seed")
		seed.Git("push", "-q", "origin", "main")
		r, rp := w.open(t, rigOptions{})
		r.start(rp, "git.addRemote", nil)
		url := r.nextInput(0)
		if o := url.Options(); o.Prompt != "Provide repository URL" || o.Message != "Add remote from URL" || !o.IgnoreFocusOut {
			t.Errorf("url box %+v", o)
		}
		for bad, want := range map[string]bool{"": true, "-u": true, "https://": true, "ftp://x/y": true, "https://example.com/r.git": false, second.Dir: false} {
			if got := url.Validate(bad) != ""; got != want {
				t.Errorf("Validate(%q) = %q", bad, url.Validate(bad))
			}
		}
		url.Accept("  " + second.Dir + "  ")
		name := r.nextInput(1)
		if o := name.Options(); o.Prompt != "Remote name" || o.Message != "Please provide a remote name" {
			t.Errorf("name box %+v", o)
		}
		if name.Validate("origin") != `Remote "origin" already exists.` || name.Validate("   ") != "Remote name format invalid" || name.Validate("fork") != "" {
			t.Errorf("validation: %q %q %q", name.Validate("origin"), name.Validate("   "), name.Validate("fork"))
		}
		name.Accept("my fork..x")
		r.idle(rp)
		if got := w.work.Git("remote", "get-url", "my-fork-x"); got != second.Dir {
			t.Errorf("url = %q", got)
		}
		if got := w.work.Git("rev-parse", "my-fork-x/main"); got != seedHead {
			t.Errorf("the new remote was not fetched: %s", got)
		}
		if remotes := rp.remotes; len(remotes) != 2 {
			t.Errorf("remotes of the repository %+v", remotes)
		}
	})
	t.Run("Escape in either box adds nothing", func(t *testing.T) {
		w := newSyncWorld(t)
		r, rp := w.open(t, rigOptions{})
		r.start(rp, "git.addRemote", nil)
		r.nextInput(0).Cancel()
		r.start(rp, "git.addRemote", nil)
		r.nextInput(1).Accept(w.bare.Dir)
		r.nextInput(2).Cancel()
		r.idle(rp)
		if got := w.work.Git("remote"); got != "origin" {
			t.Errorf("remotes %q", got)
		}
	})
	t.Run("git.removeRemote", func(t *testing.T) {
		w := newSyncWorld(t)
		w.work.AddRemote("second")
		r, rp := w.open(t, rigOptions{})
		p := r.startPick("git.removeRemote", nil)
		if p.Options().Placeholder != "Pick a remote to remove" || strings.Join(p.Labels(), ",") != "$(cloud) origin,$(cloud) second" {
			t.Errorf("pick %q %v", p.Options().Placeholder, p.Labels())
		}
		p.Choose(1)
		r.idle(rp)
		if got := w.work.Git("remote"); got != "origin" {
			t.Errorf("remotes %q", got)
		}
		if len(rp.remotes) != 1 {
			t.Errorf("remotes of the repository %+v", rp.remotes)
		}
	})
	t.Run("a remote added in a terminal shows in the sync item at the next status", func(t *testing.T) {
		e := gittest.NewEnv(t)
		repo := e.Init("lonely")
		repo.Write("a.txt", "a\n")
		repo.Commit("one")
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		rp := r.repo(repo.Dir)
		repo.AddRemote("origin")
		r.refresh(rp)
		if len(rp.remotes) != 1 {
			t.Errorf("remotes %+v", rp.remotes)
		}
	})
}

func TestRemoteCommandsShowTheirProgress(t *testing.T) {
	w := newHTTPWorld(t)
	r := newWorldRig(t, w, nil)
	rp := r.repo(w.work.Dir)
	w.publish("two.txt")
	src := r.source(rp.root)

	if err := r.api.Run("git.pull", nil); err != nil {
		t.Fatal(err)
	}
	in := r.nextInput(0) // git waits for the password: the pull is running
	if !rp.syncing || !src.Busy() {
		t.Errorf("syncing %v busy %v while the pull runs", rp.syncing, src.Busy())
	}
	items := src.StatusBarCommands()
	if items[1].Text != "$(sync~spin)" || items[1].Command != "" || items[1].Tooltip != "Synchronizing Changes..." {
		t.Errorf("sync item while pulling %+v", items[1])
	}
	if !strings.Contains(items[0].Tooltip, "Synchronizing Changes...") {
		t.Errorf("branch item tooltip %q", items[0].Tooltip)
	}
	// a second pull while one runs is the same run
	if err := r.api.Run("git.pull", nil); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	if n := len(r.api.Inputs()); n != 1 {
		t.Errorf("%d boxes: the second pull ran", n)
	}
	in.Accept(typed)
	r.idle(rp)
	if rp.syncing || src.Busy() {
		t.Errorf("syncing %v busy %v after it ended", rp.syncing, src.Busy())
	}
	if items := src.StatusBarCommands(); items[1].Text == "$(sync~spin)" || items[1].Command == "" {
		t.Errorf("sync item after the pull %+v", items[1])
	}
	t.Run("git.showProgress off", func(t *testing.T) {
		w := newHTTPWorld(t)
		r := newWorldRig(t, w, map[string]any{"git.showProgress": false})
		rp := r.repo(w.work.Dir)
		w.publish("two.txt")
		if err := r.api.Run("git.pull", nil); err != nil {
			t.Fatal(err)
		}
		in := r.nextInput(0)
		if r.source(rp.root).Busy() {
			t.Error("the progress bar is shown")
		}
		if !rp.syncing {
			t.Error("the sync item does not spin")
		}
		in.Accept(typed)
		r.idle(rp)
	})
}

// The sync item and the action button in every state (VS Code's statusbar.ts and the action button of the SPEC), as a table.
func TestTheSyncItemAndTheButtonInEveryState(t *testing.T) {
	type want struct {
		text, tooltip, command string
		button                 string // the title of the action button, "" for none
		enabled                bool
	}
	tests := []struct {
		name     string
		setup    func(w *syncWorld)
		settings map[string]any
		want     want
		none     bool // no sync item
	}{
		{"in step", nil, nil, want{"$(sync)", "Synchronize Changes", "git.sync", "$(check) Commit", false}, false},
		{"behind", func(w *syncWorld) { w.remoteCommit("b.txt"); w.work.Git("fetch", "-q") }, nil,
			want{"$(sync) 1↓ 0↑", "Pull 1 commits from origin/main", "git.sync", "$(sync) Sync Changes 1$(arrow-down)", true}, false},
		{"ahead", func(w *syncWorld) { w.localCommit("c.txt") }, nil,
			want{"$(sync) 0↓ 1↑", "Push 1 commits to origin/main", "git.sync", "$(sync) Sync Changes 1$(arrow-up)", true}, false},
		{"both", func(w *syncWorld) { w.remoteCommit("b.txt"); w.localCommit("c.txt"); w.work.Git("fetch", "-q") }, nil,
			want{"$(sync) 1↓ 1↑", "Pull 1 and push 1 commits between origin/main", "git.sync", "$(sync) Sync Changes 1$(arrow-down) 1$(arrow-up)", true}, false},
		{"no upstream", func(w *syncWorld) { w.work.Git("checkout", "-q", "-b", "topic") }, nil,
			want{"$(cloud-upload)", "Publish Branch", "git.publish", "$(cloud-upload) Publish Branch", true}, false},
		{"read-only remote shows the pull side only", func(w *syncWorld) {
			w.remoteCommit("b.txt")
			w.work.Git("fetch", "-q")
			w.work.Git("remote", "set-url", "--push", "origin", "no_push")
		}, nil, want{"$(sync) 1↓", "Pull 1 commits from origin/main", "git.sync", "$(sync) Sync Changes 1$(arrow-down)", true}, false},
		{"git.enableStatusBarSync off", func(w *syncWorld) { w.localCommit("c.txt") }, map[string]any{"git.enableStatusBarSync": false},
			want{button: "$(sync) Sync Changes 1$(arrow-up)", enabled: true}, true},
		{"git.showActionButton without sync", func(w *syncWorld) { w.localCommit("c.txt") }, map[string]any{"git.showActionButton": map[string]any{"sync": false}},
			want{text: "$(sync) 0↓ 1↑", tooltip: "Push 1 commits to origin/main", command: "git.sync", button: "$(check) Commit"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newSyncWorld(t)
			if tc.setup != nil {
				tc.setup(w)
			}
			r, rp := w.open(t, rigOptions{settings: tc.settings})
			src := r.source(rp.root)
			items := src.StatusBarCommands()
			if tc.none {
				if len(items) != 1 {
					t.Errorf("items %+v", items)
				}
			} else if len(items) != 2 || items[1].Text != tc.want.text || items[1].Tooltip != tc.want.tooltip || items[1].Command != tc.want.command {
				t.Errorf("sync item %+v, want %+v", items, tc.want)
			}
			b := src.ActionButton()
			if b == nil || b.Command.Title != tc.want.button || b.Enabled != tc.want.enabled {
				t.Errorf("button %+v, want %q enabled %v", b, tc.want.button, tc.want.enabled)
			}
		})
	}
}

func TestTheActionButtonRunsTheCommands(t *testing.T) {
	w := newSyncWorld(t)
	head := w.localCommit("c.txt")
	r, rp := w.open(t, rigOptions{settings: map[string]any{"git.confirmSync": false}})
	b := r.source(rp.root).ActionButton()
	if b == nil || b.Command.ID != "git.sync" {
		t.Fatalf("button %+v", b)
	}
	if err := r.api.Run(b.Command.ID, b.Command.Args); err != nil {
		t.Fatal(err)
	}
	r.idle(rp)
	if got := w.bare.Git("rev-parse", "main"); got != head {
		t.Errorf("origin/main = %s, want %s", got, head)
	}
	// Publish Branch
	w.work.Git("checkout", "-q", "-b", "topic")
	w.work.Git("config", "push.autoSetupRemote", "false")
	topic := w.localCommit("t.txt")
	r.refresh(rp)
	b = r.source(rp.root).ActionButton()
	if b == nil || b.Command.ID != "git.publish" {
		t.Fatalf("button %+v", b)
	}
	if err := r.api.Run(b.Command.ID, b.Command.Args); err != nil {
		t.Fatal(err)
	}
	r.idle(rp)
	if got := w.bare.Git("rev-parse", "topic"); got != topic {
		t.Errorf("topic = %s, want %s", got, topic)
	}
}

func TestCheckoutPullsFirstWithPullBeforeCheckout(t *testing.T) {
	for _, on := range []bool{false, true} {
		w := newSyncWorld(t)
		w.work.Git("branch", "--track", "topic", "origin/main")
		r, rp := w.open(t, rigOptions{settings: map[string]any{"git.pullBeforeCheckout": on}})
		head := w.remoteCommit("b.txt")
		r.run(rp, "git.checkout", map[string]any{"treeish": "topic"})
		if got := w.work.Git("rev-parse", "HEAD"); (got == head) != on {
			t.Errorf("git.pullBeforeCheckout %v: topic at %s, remote at %s", on, got, head)
		}
	}
	t.Run("a branch with commits of its own is checked out as it is", func(t *testing.T) {
		w := newSyncWorld(t)
		w.work.Git("checkout", "-q", "-b", "topic", "--track", "origin/main")
		mine := w.localCommit("mine.txt")
		w.work.Checkout("main")
		r, rp := w.open(t, rigOptions{settings: map[string]any{"git.pullBeforeCheckout": true}})
		w.remoteCommit("b.txt")
		r.run(rp, "git.checkout", map[string]any{"treeish": "topic"})
		if got := w.work.Head(); got != mine {
			t.Errorf("topic at %s, want %s", got, mine)
		}
		if note := r.noteTexts(); len(note) != 0 {
			t.Errorf("notifications %v", note)
		}
	})
}

func TestCommitAndPushRunsThePushAndHoldsTheNextCommit(t *testing.T) {
	w := newSyncWorld(t)
	r, rp := w.open(t, rigOptions{})
	w.work.Write("c.txt", "c\n")
	w.work.Git("add", "c.txt")
	r.refresh(rp)
	in := inputOf(r, rp)
	in.SetValue("add c")
	r.run(rp, "git.commit", []any{sourceArg(rp), "git.push"})
	r.idle(rp)
	if got := w.bare.Git("rev-parse", "main"); got != w.work.Head() || w.work.Git("log", "-1", "--format=%s") != "add c" {
		t.Errorf("origin/main = %s, HEAD %s: the post-commit push did not run", got, w.work.Head())
	}
	// Commit & Sync pulls and pushes
	remote := w.remoteCommit("b.txt")
	w.work.Write("d.txt", "d\n")
	w.work.Git("add", "d.txt")
	r.refresh(rp)
	r.api.SetSetting("git.confirmSync", false)
	in.SetValue("add d")
	r.run(rp, "git.commit", []any{sourceArg(rp), "git.sync"})
	r.idle(rp)
	if w.work.Git("merge-base", "--is-ancestor", remote, "HEAD") != "" {
		t.Error("Commit & Sync did not pull")
	}
	if got := w.bare.Git("rev-parse", "main"); got != w.work.Head() {
		t.Errorf("origin/main = %s, HEAD %s", got, w.work.Head())
	}
}

// errorWithStderr is the error of a failed command whose error output is the text.
func errorWithStderr(stderr string) error {
	return errors.New("GitCommandFailed", "git push failed", "stderr", stderr)
}

func TestRemoteNames(t *testing.T) {
	tests := []struct{ in, want string }{
		{"origin", "origin"},
		{"  upstream  ", "upstream"},
		{"my fork", "my-fork"},
		{"a..b", "a-b"},
		{"~tilde^caret:colon", "-tilde-caret-colon"},
		{"x.lock", "x-"},
		{"trailing.", "trailing-"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range tests {
		if got := remoteNameFor(tc.in); got != tc.want {
			t.Errorf("remoteNameFor(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWhatTheRemoteSaidAboutARefusedPush(t *testing.T) {
	mk := func(stderr string) error {
		return errorWithStderr(stderr)
	}
	tests := []struct{ name, stderr, want string }{
		{"the lines of the remote", "remote: error: GH006: Protected branch update failed.\nremote: \nremote: more\nTo x\n ! [remote rejected] main -> main (protected branch hook declined)\n", "error: GH006: Protected branch update failed. more"},
		{"the reason after the ref", "To x\n ! [remote rejected] main -> main (pre-receive hook declined)\nerror: failed to push some refs to 'x'\n", "pre-receive hook declined"},
		{"nothing known", "fatal: nothing\n", "fatal: nothing"},
		{"the user information is masked", "remote: see https://ada:secret@host/x\n ! [remote rejected] a -> a (x)\n", "see https://***@host/x"},
	}
	for _, tc := range tests {
		if got := remoteMessage(mk(tc.stderr)); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestThePushArgumentsOfACommand(t *testing.T) {
	tests := []struct {
		name  string
		args  any
		force bool
		want  pushRequest
	}{
		{"none", nil, false, pushRequest{typ: pushToRemote}},
		{"a map", map[string]any{"remote": "o", "refspec": "a:b", "setUpstream": true}, true, pushRequest{typ: pushToRemote, force: true, remote: "o", refspec: "a:b", setUpstream: true}},
		{"a list with the repository first", []any{map[string]any{"sourceControl": "scm1"}, map[string]any{"remote": "o"}}, false, pushRequest{typ: pushToRemote, remote: "o"}},
		{"values of the wrong type", map[string]any{"remote": 3, "setUpstream": "yes"}, false, pushRequest{typ: pushToRemote}},
	}
	for _, tc := range tests {
		if got := pushToArgs(tc.args, tc.force); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
