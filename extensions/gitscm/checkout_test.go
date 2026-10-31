package gitscm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The checkout and its picker against real repositories (step 24.13): each command runs through the registry of the fake API, the test
// answers the picker as the user does, and looks at what git says afterwards.

// aLines is a.txt with the given first line.
func aLines(first string) string { return first + "\nl2\nl3\nl4\nl5\nl6\nl7\nl8\n" }

// refsRepo makes a repository on main with a remote: main is one commit ahead of origin/main; "feature" is one ahead of main and "old" at
// the first commit, both also at the remote ("topic" only there); v1 is a lightweight tag and v2 an annotated one.
func refsRepo(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	r := e.Init("refs")
	r.Write("a.txt", aLines("l1"))
	r.Commit("Add a")
	r.Tag("v1", "")
	r.Branch("old", "")
	r.Write("a.txt", aLines("L1"))
	r.Commit("Change a")
	r.Tag("v2", "Second release")
	r.Branch("feature", "")
	r.Checkout("feature")
	r.Write("b.txt", "bee\n")
	r.Commit("Add b")
	r.Checkout("main")
	r.AddRemote("origin")
	r.Git("push", "-q", "-u", "origin", "main", "old", "feature")
	r.Git("push", "-q", "origin", "feature:topic")
	r.Git("fetch", "-q", "origin")
	r.Write("c.txt", "see\n")
	r.Commit("Add c")
	return r
}

func refsRig(t *testing.T, o rigOptions) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := refsRepo(t, e)
	o.folder = repo.Dir
	r := newRig(t, e, o)
	r.settle()
	return r, repo, r.repo(repo.Dir)
}

// branchOf is the branch HEAD is on, "HEAD" when it is detached.
func branchOf(repo *gittest.Repo) string { return repo.Git("rev-parse", "--abbrev-ref", "HEAD") }

// labelsOf are the labels of the items of a pick, a separator as "-- name --".
func pickLabels(p *fakeapi.Pick) []string {
	var out []string
	for _, it := range p.Options().Items {
		if it.Separator {
			out = append(out, "-- "+it.Label+" --")
			continue
		}
		out = append(out, it.Label)
	}
	return out
}

// indexOf is the position of the item with this label in a pick.
func indexOf(t *testing.T, p *fakeapi.Pick, label string) int {
	t.Helper()
	for i, it := range p.Options().Items {
		if it.Label == label {
			return i
		}
	}
	t.Fatalf("the pick has no %q: %v", label, pickLabels(p))
	return -1
}

func equalLists(a, b []string) bool { return strings.Join(a, "\n") == strings.Join(b, "\n") }

func TestTheCheckoutPickerListsTheRefs(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		detached bool
		want     []string
	}{
		{"by date, with the three kinds", nil, false, []string{
			"$(plus) Create new branch...", "$(plus) Create new branch from...", "$(debug-disconnect) Checkout detached...",
			"-- branches --", "$(git-branch) main", "$(git-branch) feature", "$(git-branch) old",
			"-- remote branches --", "$(cloud) origin/feature", "$(cloud) origin/topic", "$(cloud) origin/main", "$(cloud) origin/old",
			"-- tags --", "$(tag) v1", "$(tag) v2"}},
		{"alphabetical", map[string]any{"git.branchSortOrder": "alphabetically"}, false, []string{
			"$(plus) Create new branch...", "$(plus) Create new branch from...", "$(debug-disconnect) Checkout detached...",
			"-- branches --", "$(git-branch) feature", "$(git-branch) main", "$(git-branch) old",
			"-- remote branches --", "$(cloud) origin/feature", "$(cloud) origin/main", "$(cloud) origin/old", "$(cloud) origin/topic",
			"-- tags --", "$(tag) v1", "$(tag) v2"}},
		{"only tags", map[string]any{"git.checkoutType": []any{"tags"}}, false, []string{
			"$(plus) Create new branch...", "$(plus) Create new branch from...", "$(debug-disconnect) Checkout detached...", "-- tags --", "$(tag) v1", "$(tag) v2"}},
		{"remote branches before the local ones", map[string]any{"git.checkoutType": []any{"remote", "local"}, "git.branchSortOrder": "alphabetically"}, false, []string{
			"$(plus) Create new branch...", "$(plus) Create new branch from...", "$(debug-disconnect) Checkout detached...",
			"-- remote branches --", "$(cloud) origin/feature", "$(cloud) origin/main", "$(cloud) origin/old", "$(cloud) origin/topic",
			"-- branches --", "$(git-branch) feature", "$(git-branch) main", "$(git-branch) old"}},
		{"a protected branch has a lock", map[string]any{"git.branchProtection": []any{"old", "feat*"}, "git.branchSortOrder": "alphabetically"}, false, []string{
			"$(plus) Create new branch...", "$(plus) Create new branch from...", "$(debug-disconnect) Checkout detached...",
			"-- branches --", "$(lock) feature", "$(git-branch) main", "$(lock) old",
			"-- remote branches --", "$(cloud) origin/feature", "$(cloud) origin/main", "$(cloud) origin/old", "$(cloud) origin/topic",
			"-- tags --", "$(tag) v1", "$(tag) v2"}},
		{"detached: no commands and no tags, origin/HEAD kept", map[string]any{"git.branchSortOrder": "alphabetically"}, true, []string{
			"-- branches --", "$(git-branch) feature", "$(git-branch) main", "$(git-branch) old",
			"-- remote branches --", "$(cloud) origin/HEAD", "$(cloud) origin/feature", "$(cloud) origin/main", "$(cloud) origin/old", "$(cloud) origin/topic"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := refsRig(t, rigOptions{settings: tc.settings})
			repo.Git("remote", "set-head", "origin", "main") // origin/HEAD is listed for a detached checkout only
			cmd := "git.checkout"
			if tc.detached {
				cmd = "git.checkoutDetached"
			}
			r.start(rp, cmd, nil)
			p := r.waitPick()
			got := pickLabels(p)
			if !equalLists(got, tc.want) {
				t.Errorf("items:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
			for _, it := range p.Options().Items {
				if strings.HasPrefix(it.Label, "$(plus)") || strings.HasPrefix(it.Label, "$(debug") {
					if !it.AlwaysShow {
						t.Errorf("%q is not always shown", it.Label)
					}
				}
			}
			wantHolder := "Select a branch or tag to checkout"
			if tc.detached {
				wantHolder = "Select a branch to checkout in detached mode"
			}
			if p.Options().Placeholder != wantHolder {
				t.Errorf("placeholder = %q", p.Options().Placeholder)
			}
			p.Close()
		})
	}
}

func TestTheCheckoutPickerShowsTheDetailsOfTheRefs(t *testing.T) {
	r, repo, rp := refsRig(t, rigOptions{})
	short := func(rev string) string { return repo.Git("rev-parse", "--short=7", rev) }
	r.start(rp, "git.checkout", nil)
	p := r.waitPick()
	item := func(label string) extension.QuickPickItem { return p.Options().Items[indexOf(t, p, label)] }

	main := item("$(git-branch) main") // one commit ahead of origin/main
	if !strings.HasPrefix(main.Description, "0↓ 1↑ • ") || !strings.HasSuffix(main.Description, " ago") {
		t.Errorf("main: description %q", main.Description)
	}
	if want := "Ada Example • " + short("main") + " • Add c"; main.Detail != want {
		t.Errorf("main: detail %q, want %q", main.Detail, want)
	}
	old := item("$(git-branch) old") // in step with its upstream: only how long ago
	if strings.Contains(old.Description, "↓") || !strings.HasSuffix(old.Description, " ago") {
		t.Errorf("old: description %q", old.Description)
	}
	if want := "Ada Example • " + short("old") + " • Add a"; old.Detail != want {
		t.Errorf("old: detail %q, want %q", old.Detail, want)
	}
	if d := item("$(cloud) origin/topic").Description; !strings.HasSuffix(d, " ago") {
		t.Errorf("origin/topic: description %q", d)
	}
	// the annotated tag shows the commit it points at
	v2 := item("$(tag) v2")
	if want := "Ada Example • " + short("v2^{commit}") + " • Change a"; v2.Detail != want {
		t.Errorf("v2: detail %q, want %q", v2.Detail, want)
	}
	p.Close()

	r.api.SetSetting("git.showReferenceDetails", false)
	r.start(rp, "git.checkout", nil)
	p = r.waitPick()
	if got := item("$(git-branch) old"); got.Detail != "" || got.Description != short("old") {
		t.Errorf("without details: old = %+v", got)
	}
	if got := item("$(cloud) origin/topic"); got.Detail != "" || got.Description != "Remote branch at "+short("origin/topic") {
		t.Errorf("without details: origin/topic = %+v", got)
	}
	if got := item("$(tag) v1"); got.Description != "Tag at "+short("v1") {
		t.Errorf("without details: v1 = %+v", got)
	}
	p.Close()
}

func TestCheckingOutWhatThePickerListsSwitches(t *testing.T) {
	tests := []struct {
		name      string
		detached  bool
		label     string
		wantRef   string // the branch HEAD is on afterwards, "HEAD" when detached
		wantAt    string // the revision HEAD names
		trackedBy string // the upstream of the branch HEAD is on
	}{
		{"a local branch", false, "$(git-branch) feature", "feature", "feature", "origin/feature"},
		{"a local branch, detached", true, "$(git-branch) feature", "HEAD", "feature", ""},
		{"a remote branch that has a local branch tracking it", false, "$(cloud) origin/feature", "feature", "origin/feature", "origin/feature"},
		{"a remote branch with no local branch gets one that tracks it", false, "$(cloud) origin/topic", "topic", "origin/topic", "origin/topic"},
		{"a remote branch, detached", true, "$(cloud) origin/topic", "HEAD", "origin/topic", ""},
		{"a lightweight tag", false, "$(tag) v1", "HEAD", "v1", ""},
		{"an annotated tag", false, "$(tag) v2", "HEAD", "v2^{commit}", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := refsRig(t, rigOptions{})
			cmd := "git.checkout"
			if tc.detached {
				cmd = "git.checkoutDetached"
			}
			r.start(rp, cmd, nil)
			p := r.waitPick()
			p.Choose(indexOf(t, p, tc.label))
			r.idle(rp)
			if got := branchOf(repo); got != tc.wantRef {
				t.Errorf("on %q, want %q", got, tc.wantRef)
			}
			if got, want := repo.Git("rev-parse", "HEAD"), repo.Git("rev-parse", tc.wantAt); got != want {
				t.Errorf("HEAD = %s, want %s", got, want)
			}
			if tc.trackedBy != "" {
				if got := repo.Git("rev-parse", "--abbrev-ref", "@{upstream}"); got != tc.trackedBy {
					t.Errorf("upstream = %q, want %q", got, tc.trackedBy)
				}
			}
			if n := len(r.api.Notes()); n != 0 {
				t.Errorf("notifications: %+v", r.api.Notes())
			}
			// the status bar shows the new place
			items := r.source(repo.Dir).StatusBarCommands()
			if len(items) == 0 || items[0].Command != "git.checkout" {
				t.Fatalf("status bar = %+v", items)
			}
		})
	}
}

func TestCheckoutByTheArgumentOfACaller(t *testing.T) {
	r, repo, rp := refsRig(t, rigOptions{})
	r.run(rp, "git.checkout", map[string]any{"treeish": "feature"})
	if got := branchOf(repo); got != "feature" {
		t.Errorf("on %q", got)
	}
	if len(r.api.Picks()) != 0 {
		t.Error("a picker was shown for a ref that was given")
	}
	r.run(rp, "git.checkoutDetached", map[string]any{"treeish": "main"})
	if got := branchOf(repo); got != "HEAD" {
		t.Errorf("on %q after a detached checkout", got)
	}
	// a ref that is not there fails with the message of git, and the branch item is back to what it was
	if err := r.api.Run("git.checkout", map[string]any{"treeish": "nope"}); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the failure", func() bool { return len(notesWith(r, "git checkout failed")) > 0 })
	r.idle(rp)
	if items := r.source(repo.Dir).StatusBarCommands(); items[0].Command != "git.checkout" || strings.Contains(items[0].Text, "nope") {
		t.Errorf("status bar = %+v after a failed checkout", items[0])
	}
}

func TestCreatingABranchFromThePicker(t *testing.T) {
	t.Run("the text typed is the name and nothing is asked", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.checkout", nil)
		p := r.waitPick()
		p.ChooseTyped(indexOf(t, p, "$(plus) Create new branch..."), "my topic")
		r.idle(rp)
		if got := branchOf(repo); got != "my-topic" {
			t.Errorf("on %q, want my-topic (the space replaced)", got)
		}
		if len(r.api.Inputs()) != 0 {
			t.Error("the name box was shown for a name that was typed")
		}
		if got, want := repo.Git("rev-parse", "my-topic"), repo.Git("rev-parse", "main"); got != want {
			t.Errorf("my-topic names %s, want %s", got, want)
		}
	})
	t.Run("nothing typed asks for the name", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{settings: map[string]any{"git.branchPrefix": "ada/"}})
		r.start(rp, "git.checkout", nil)
		p := r.waitPick()
		p.Choose(indexOf(t, p, "$(plus) Create new branch..."))
		in := r.nextInput(0)
		if in.Options().Value != "ada/" || in.Options().Prompt != "Branch name" || in.Options().Message != "Please provide a new branch name" || !in.Options().IgnoreFocusOut {
			t.Errorf("box = %+v", in.Options())
		}
		in.Accept("ada/loader cache")
		r.idle(rp)
		if got := branchOf(repo); got != "ada/loader-cache" {
			t.Errorf("on %q", got)
		}
	})
	t.Run("from a ref", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.checkout", nil)
		p := r.waitPick()
		p.ChooseTyped(indexOf(t, p, "$(plus) Create new branch from..."), "from-old")
		second := r.nextPick(1)
		if second.Options().Placeholder != "Select a ref to create the branch from" {
			t.Errorf("placeholder = %q", second.Options().Placeholder)
		}
		got := pickLabels(second)
		if got[0] != "HEAD" || got[1] != "-- branches --" || got[len(got)-1] != "$(tag) v2" {
			t.Errorf("items = %v", got)
		}
		second.Choose(indexOf(t, second, "$(git-branch) old"))
		r.idle(rp)
		if got := branchOf(repo); got != "from-old" {
			t.Errorf("on %q", got)
		}
		if got, want := repo.Git("rev-parse", "HEAD"), repo.Git("rev-parse", "old"); got != want {
			t.Errorf("HEAD = %s, want old %s", got, want)
		}
	})
	t.Run("checkout detached from the picker", func(t *testing.T) {
		r, repo, rp := refsRig(t, rigOptions{})
		r.start(rp, "git.checkout", nil)
		p := r.waitPick()
		p.Choose(indexOf(t, p, "$(debug-disconnect) Checkout detached..."))
		second := r.nextPick(1)
		if second.Options().Placeholder != "Select a branch to checkout in detached mode" {
			t.Errorf("placeholder = %q", second.Options().Placeholder)
		}
		second.Choose(indexOf(t, second, "$(git-branch) old"))
		r.idle(rp)
		if got := branchOf(repo); got != "HEAD" {
			t.Errorf("on %q", got)
		}
		if got, want := repo.Git("rev-parse", "HEAD"), repo.Git("rev-parse", "old"); got != want {
			t.Errorf("HEAD = %s, want old %s", got, want)
		}
	})
}

// dirtyRig makes a repository on "old" with a change to the last line of a.txt and an untracked file; main and feature changed the first
// line, so git refuses to switch to them although the changes do not overlap.
func dirtyRig(t *testing.T, o rigOptions) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	r, repo, rp := refsRig(t, o)
	repo.Checkout("old")
	repo.Write("a.txt", strings.Replace(aLines("l1"), "l8", "edited8", 1))
	repo.Write("u.txt", "untracked\n")
	r.refresh(rp)
	return r, repo, rp
}

func TestACheckoutThatWouldOverwriteLocalChanges(t *testing.T) {
	pickMain := func(t *testing.T, r *rig, rp *repository) {
		t.Helper()
		r.start(rp, "git.checkout", nil)
		p := r.waitPick()
		p.Choose(indexOf(t, p, "$(git-branch) main"))
	}
	merged := strings.Replace(aLines("L1"), "l8", "edited8", 1) // main's file with the change on top
	t.Run("the question has its three answers, and Cancel changes nothing", func(t *testing.T) {
		r, repo, rp := dirtyRig(t, rigOptions{})
		pickMain(t, r, rp)
		d := r.theOnlyDialog()
		if d.Spec().Message != "Your local changes would be overwritten by checkout." {
			t.Errorf("message = %q", d.Spec().Message)
		}
		if got := buttons(d); !equalLists(got, []string{"Stash & Checkout", "Migrate Changes", "Force Checkout", "Cancel"}) {
			t.Errorf("buttons = %v", got)
		}
		d.Cancel()
		r.idle(rp)
		if got := branchOf(repo); got != "old" {
			t.Errorf("on %q after Cancel", got)
		}
		if got := repo.Read("a.txt"); !strings.Contains(got, "edited8") {
			t.Errorf("a.txt = %q, the change was lost", got)
		}
		if len(r.api.Notes()) != 0 {
			t.Errorf("notifications: %+v", r.api.Notes())
		}
	})
	t.Run("Stash & Checkout keeps the changes in the stash", func(t *testing.T) {
		r, repo, rp := dirtyRig(t, rigOptions{})
		pickMain(t, r, rp)
		r.theOnlyDialog().Press("Stash & Checkout")
		r.nextInput(0).Accept("before the checkout") // the message of the stash
		r.idle(rp)
		if got := branchOf(repo); got != "main" {
			t.Errorf("on %q", got)
		}
		if got := repo.Read("a.txt"); got != aLines("L1") {
			t.Errorf("a.txt = %q, want the one of main", got)
		}
		if got := stashSubjects(repo); len(got) != 1 || !strings.HasSuffix(got[0], "before the checkout") {
			t.Errorf("stashes = %q", got)
		}
		if exists(repo, "u.txt") {
			t.Error("the untracked file was not stashed")
		}
	})
	t.Run("Stash & Checkout stops when the message box is closed", func(t *testing.T) {
		r, repo, rp := dirtyRig(t, rigOptions{})
		pickMain(t, r, rp)
		r.theOnlyDialog().Press("Stash & Checkout")
		r.nextInput(0).Cancel()
		r.idle(rp)
		if got := branchOf(repo); got != "old" || len(stashSubjects(repo)) != 0 {
			t.Errorf("on %q with stashes %q", got, stashSubjects(repo))
		}
	})
	t.Run("Migrate Changes brings them to the new branch", func(t *testing.T) {
		r, repo, rp := dirtyRig(t, rigOptions{})
		pickMain(t, r, rp)
		r.theOnlyDialog().Press("Migrate Changes")
		r.nextInput(0).Accept("")
		r.api.WaitFor("the migrated file", func() bool { r.api.Pump(); return readOr(repo, "a.txt") == merged })
		r.idle(rp)
		if got := branchOf(repo); got != "main" {
			t.Errorf("on %q", got)
		}
		if got := readOr(repo, "u.txt"); got != "untracked\n" {
			t.Errorf("u.txt = %q", got)
		}
		if len(stashSubjects(repo)) != 0 {
			t.Errorf("the stash was kept: %q", stashSubjects(repo))
		}
	})
	t.Run("Force Checkout throws the changes away", func(t *testing.T) {
		r, repo, rp := dirtyRig(t, rigOptions{})
		pickMain(t, r, rp)
		r.theOnlyDialog().Press("Force Checkout")
		r.idle(rp)
		if got := branchOf(repo); got != "main" {
			t.Errorf("on %q", got)
		}
		if got := repo.Read("a.txt"); got != aLines("L1") {
			t.Errorf("a.txt = %q", got)
		}
		if len(stashSubjects(repo)) != 0 {
			t.Error("a stash was made")
		}
		if got := readOr(repo, "u.txt"); got != "untracked\n" {
			t.Errorf("the untracked file was removed: %q", got)
		}
	})
	t.Run("git.autoStash stashes, switches and brings the changes back without asking", func(t *testing.T) {
		r, repo, rp := dirtyRig(t, rigOptions{settings: map[string]any{"git.autoStash": true}})
		pickMain(t, r, rp)
		r.api.WaitFor("the migrated file", func() bool { r.api.Pump(); return readOr(repo, "a.txt") == merged })
		r.idle(rp)
		if got := branchOf(repo); got != "main" {
			t.Errorf("on %q", got)
		}
		if len(r.api.OpenDialogs()) != 0 || len(r.api.Inputs()) != 0 {
			t.Error("a question was asked")
		}
		if len(stashSubjects(repo)) != 0 {
			t.Errorf("the stash was kept: %q", stashSubjects(repo))
		}
	})
	t.Run("a checkout given by a caller fails with the message of git", func(t *testing.T) {
		r, repo, rp := dirtyRig(t, rigOptions{})
		if err := r.api.Run("git.checkout", map[string]any{"treeish": "main"}); err != nil {
			t.Fatal(err)
		}
		r.api.WaitFor("the failure", func() bool { return len(notesWith(r, "git checkout failed")) > 0 })
		r.idle(rp)
		if len(r.api.OpenDialogs()) != 0 {
			t.Error("the question was asked for a ref that was given")
		}
		if got := branchOf(repo); got != "old" {
			t.Errorf("on %q", got)
		}
	})
}

func TestTheStatusBarShowsARunningCheckout(t *testing.T) {
	r, repo, rp := refsRig(t, rigOptions{})
	marker := filepath.Join(t.TempDir(), "go")
	repo.Hook("post-checkout", "#!/bin/sh\nwhile [ ! -f '"+marker+"' ]; do sleep 0.02; done\n")
	r.start(rp, "git.checkout", map[string]any{"treeish": "feature"})
	var item extension.StatusCommand
	r.api.WaitFor("the running state", func() bool {
		r.api.Pump()
		item = r.source(repo.Dir).StatusBarCommands()[0]
		return strings.Contains(item.Text, "loading~spin")
	})
	if item.Text != "$(loading~spin) feature" || item.Command != "" || item.Tooltip != "feature, Checking Out Branch/Tag..." {
		t.Errorf("while it runs: %+v", item)
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.idle(rp)
	item = r.source(repo.Dir).StatusBarCommands()[0]
	if item.Text != "$(git-branch) feature" || item.Command != "git.checkout" || item.Tooltip != "feature, Checkout Branch/Tag..." {
		t.Errorf("afterwards: %+v", item)
	}
}

func TestTheBranchItemForEachStateOfTheRepository(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
		setup    func(repo *gittest.Repo)
		flags    func(rp *repository)
		text     string
		tooltip  string
		command  string
	}{
		{"a branch", nil, nil, nil, "$(git-branch) main", "main, Checkout Branch/Tag...", "git.checkout"},
		{"a protected branch", map[string]any{"git.branchProtection": []any{"main"}}, nil, nil, "$(lock) main", "main, Checkout Branch/Tag...", "git.checkout"},
		{"a protected branch with changes is still locked", map[string]any{"git.branchProtection": []any{"ma*"}}, func(repo *gittest.Repo) { repo.Write("c.txt", "changed\n") }, nil,
			"$(lock) main*", "main*, Checkout Branch/Tag...", "git.checkout"},
		{"conflicts", nil, func(repo *gittest.Repo) {
			repo.Write("a.txt", "main side\n")
			repo.Commit("Edit a on main", "a.txt")
			repo.Checkout("old")
			repo.Write("a.txt", "old side\n")
			repo.Commit("Edit a on old", "a.txt")
			_ = repo.Merge("main")
		}, nil, "$(git-branch-conflicts) old+!", "old+!, Checkout Branch/Tag...", "git.checkout"},
		{"staged changes", nil, func(repo *gittest.Repo) { repo.Write("c.txt", "changed\n"); repo.Git("add", "c.txt") }, nil,
			"$(git-branch-staged-changes) main+", "main+, Checkout Branch/Tag...", "git.checkout"},
		{"changes", nil, func(repo *gittest.Repo) { repo.Write("c.txt", "changed\n") }, nil, "$(git-branch-changes) main*", "main*, Checkout Branch/Tag...", "git.checkout"},
		{"a tag", nil, func(repo *gittest.Repo) { repo.Checkout("v1") }, nil, "$(tag) v1", "v1, Checkout Branch/Tag...", "git.checkout"},
		{"a detached commit", nil, func(repo *gittest.Repo) { repo.Git("checkout", "-q", "--detach", "feature") }, nil, "", "", "git.checkout"},
		{"a commit that is running", nil, nil, func(rp *repository) { rp.committing = true }, "$(git-branch) main", "main, Committing Changes...", ""},
		{"a sync that is running", nil, nil, func(rp *repository) { rp.syncing = true }, "$(git-branch) main", "main, Synchronizing Changes...", ""},
		{"a checkout that is running", nil, nil, func(rp *repository) { rp.checkingOut, rp.checkoutLabel = true, "feature" },
			"$(loading~spin) feature", "feature, Checking Out Branch/Tag...", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := refsRepo(t, e)
			if tc.setup != nil {
				tc.setup(repo)
			}
			r := newRig(t, e, rigOptions{folder: repo.Dir, settings: tc.settings})
			r.settle()
			rp := r.repo(repo.Dir)
			if tc.flags != nil {
				tc.flags(rp)
				rp.updateStatusBar()
			}
			item := r.source(repo.Dir).StatusBarCommands()[0]
			if tc.text == "" { // a detached commit: the short hash
				tc.text = "$(git-commit) " + repo.Git("rev-parse", "--short=7", "HEAD")
				tc.tooltip = tc.text[len("$(git-commit) "):] + ", Checkout Branch/Tag..."
			}
			if item.Text != tc.text || item.Tooltip != tc.tooltip || item.Command != tc.command {
				t.Errorf("item = %+v\nwant text %q, tooltip %q, command %q", item, tc.text, tc.tooltip, tc.command)
			}
		})
	}
}

func TestTheBranchItemFollowsARunningCommit(t *testing.T) {
	r, repo, rp := refsRig(t, rigOptions{})
	rp.setCommitting(true)
	item := r.source(repo.Dir).StatusBarCommands()[0]
	if item.Command != "" || item.Tooltip != "main, Committing Changes..." {
		t.Errorf("while a commit runs: %+v", item)
	}
	rp.setCommitting(false)
	item = r.source(repo.Dir).StatusBarCommands()[0]
	if item.Command != "git.checkout" || item.Tooltip != "main, Checkout Branch/Tag..." {
		t.Errorf("afterwards: %+v", item)
	}
}
