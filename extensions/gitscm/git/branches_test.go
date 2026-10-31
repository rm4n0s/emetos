package git

import (
	"context"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// The commands of branches and tags against real repositories (step 24.13).

// branchBase makes a repository on main with two commits, a branch "feature" one commit ahead, and a branch "old" at the first commit.
func branchBase(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	r := e.Init("branches")
	r.Write("a.txt", "one\n")
	r.Commit("Add a")
	r.Branch("old", "")
	r.Write("a.txt", "two\n")
	r.Commit("Change a")
	r.Branch("feature", "")
	r.Checkout("feature")
	r.Write("b.txt", "bee\n")
	r.Commit("Add b")
	r.Checkout("main")
	return r
}

func current(r *gittest.Repo) string { return r.Git("rev-parse", "--abbrev-ref", "HEAD") }

const writeRoute = "->Repo.write->Queue.Do->Repo.write.func1->Runner.Run->Runner.execute->classify."

func TestCheckoutRef(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		setup func(r *gittest.Repo)
		ref   string
		opts  CheckoutOptions
		want  string // the current branch, "HEAD" when detached
	}{
		{"a local branch", nil, "feature", CheckoutOptions{}, "feature"},
		{"a tag leaves HEAD detached", func(r *gittest.Repo) { r.Tag("v1", "") }, "v1", CheckoutOptions{}, "HEAD"},
		{"a branch in detached mode", nil, "feature", CheckoutOptions{Detached: true}, "HEAD"},
		{"a commit", func(r *gittest.Repo) {}, "main~1", CheckoutOptions{}, "HEAD"},
		{"a remote branch makes a tracking branch", func(r *gittest.Repo) {
			r.AddRemote("origin")
			r.Git("push", "-q", "origin", "feature:topic")
			r.Git("fetch", "-q", "origin")
		}, "origin/topic", CheckoutOptions{Track: true}, "topic"},
		{"changes that do not conflict are kept", func(r *gittest.Repo) { r.Write("c.txt", "new\n") }, "feature", CheckoutOptions{}, "feature"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			r := branchBase(t, e)
			if tc.setup != nil {
				tc.setup(r)
			}
			repo := open(t, e, r.Dir)
			if err := repo.CheckoutRef(ctx, tc.ref, tc.opts); err != nil {
				t.Fatal(err)
			}
			if got := current(r); got != tc.want {
				t.Errorf("on %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("the tracking branch follows the remote branch", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := branchBase(t, e)
		r.AddRemote("origin")
		r.Git("push", "-q", "origin", "feature:topic")
		r.Git("fetch", "-q", "origin")
		if err := open(t, e, r.Dir).CheckoutRef(ctx, "origin/topic", CheckoutOptions{Track: true}); err != nil {
			t.Fatal(err)
		}
		if got := r.Git("rev-parse", "--abbrev-ref", "topic@{upstream}"); got != "origin/topic" {
			t.Errorf("upstream = %q", got)
		}
	})
}

func TestCheckoutRefWithLocalChanges(t *testing.T) {
	ctx := context.Background()
	dirty := func(t *testing.T) (*gittest.Repo, *Repo) {
		e := gittest.NewEnv(t)
		r := branchBase(t, e)
		r.Checkout("old")
		r.Write("a.txt", "edited\n") // main and feature changed a.txt after old
		return r, open(t, e, r.Dir)
	}
	t.Run("a switch that would overwrite them fails with DirtyWorkTree", func(t *testing.T) {
		r, repo := dirty(t)
		err := repo.CheckoutRef(ctx, "main", CheckoutOptions{})
		wantRoute(t, err, "Repo.CheckoutRef"+writeRoute+"DirtyWorkTree")
		if got := current(r); got != "old" {
			t.Errorf("on %q after the refusal", got)
		}
		if got := r.Read("a.txt"); got != "edited\n" {
			t.Errorf("a.txt = %q, the change was lost", got)
		}
	})
	t.Run("a forced switch throws them away", func(t *testing.T) {
		r, repo := dirty(t)
		if err := repo.CheckoutRef(ctx, "main", CheckoutOptions{Force: true}); err != nil {
			t.Fatal(err)
		}
		if got := current(r); got != "main" {
			t.Errorf("on %q", got)
		}
		if got := r.Read("a.txt"); got != "two\n" {
			t.Errorf("a.txt = %q", got)
		}
	})
}

func TestCheckoutRefFailures(t *testing.T) {
	ctx := context.Background()
	e := gittest.NewEnv(t)
	r := branchBase(t, e)
	repo := open(t, e, r.Dir)
	t.Run("a ref that is not there", func(t *testing.T) {
		wantRoute(t, repo.CheckoutRef(ctx, "nope", CheckoutOptions{}), "Repo.CheckoutRef"+writeRoute+"GitCommandFailed")
	})
	t.Run("a name that starts with a dash", func(t *testing.T) {
		wantRoute(t, repo.CheckoutRef(ctx, "--orphan", CheckoutOptions{}), "refName.GitRefInvalid")
		wantRoute(t, repo.CheckoutRef(ctx, "", CheckoutOptions{}), "refName.GitRefInvalid")
	})
}

func TestBranch(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		branch   string
		checkout bool
		from     string
		onBranch string
		at       string // the revision the new branch names
	}{
		{"from HEAD, switching", "topic", true, "", "topic", "main"},
		{"from HEAD, staying", "topic", false, "", "main", "main"},
		{"from another branch, switching", "topic", true, "old", "topic", "old"},
		{"from a tag, staying", "topic", false, "v1", "main", "v1"},
		{"a name with a slash", "feat/topic", true, "", "feat/topic", "main"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			r := branchBase(t, e)
			r.Tag("v1", "")
			repo := open(t, e, r.Dir)
			if err := repo.Branch(ctx, tc.branch, tc.checkout, tc.from); err != nil {
				t.Fatal(err)
			}
			if got := current(r); got != tc.onBranch {
				t.Errorf("on %q, want %q", got, tc.onBranch)
			}
			if got, want := r.Git("rev-parse", tc.branch), r.Git("rev-parse", tc.at); got != want {
				t.Errorf("%s names %s, want %s", tc.branch, got, want)
			}
			if _, _, err := r.GitErr("rev-parse", "--abbrev-ref", tc.branch+"@{upstream}"); err == nil {
				t.Error("the new branch tracks something")
			}
		})
	}
	t.Run("changes go with the new branch", func(t *testing.T) {
		e := gittest.NewEnv(t)
		r := branchBase(t, e)
		r.Write("a.txt", "edited\n")
		if err := open(t, e, r.Dir).Branch(ctx, "topic", true, ""); err != nil {
			t.Fatal(err)
		}
		if got := r.Read("a.txt"); got != "edited\n" {
			t.Errorf("a.txt = %q", got)
		}
	})
	failures := []struct {
		name   string
		branch string
		from   string
		route  string
	}{
		{"a name that is taken", "feature", "", "Repo.Branch" + writeRoute + "BranchAlreadyExists"},
		{"a name git refuses", "a..b", "", "Repo.Branch" + writeRoute + "InvalidBranchName"},
		{"a start that is not there", "topic", "nope", "Repo.Branch" + writeRoute + "GitCommandFailed"},
		{"a name that starts with a dash", "-x", "", "refName.GitRefInvalid"},
		{"a start that starts with a dash", "topic", "--detach", "refName.GitRefInvalid"},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			r := branchBase(t, e)
			wantRoute(t, open(t, e, r.Dir).Branch(ctx, tc.branch, true, tc.from), tc.route)
		})
	}
}

func TestRenameBranch(t *testing.T) {
	ctx := context.Background()
	e := gittest.NewEnv(t)
	r := branchBase(t, e)
	repo := open(t, e, r.Dir)
	wantRoute(t, repo.RenameBranch(ctx, "feature"), "Repo.RenameBranch"+writeRoute+"BranchAlreadyExists")
	wantRoute(t, repo.RenameBranch(ctx, "a..b"), "Repo.RenameBranch"+writeRoute+"InvalidBranchName")
	wantRoute(t, repo.RenameBranch(ctx, "-m"), "refName.GitRefInvalid")
	if err := repo.RenameBranch(ctx, "trunk"); err != nil {
		t.Fatal(err)
	}
	if got := current(r); got != "trunk" {
		t.Errorf("on %q", got)
	}
	if _, _, err := r.GitErr("rev-parse", "--verify", "-q", "main"); err == nil {
		t.Error("main still exists")
	}
}

func TestDeleteBranch(t *testing.T) {
	ctx := context.Background()
	e := gittest.NewEnv(t)
	r := branchBase(t, e)
	repo := open(t, e, r.Dir)
	exists := func(name string) bool {
		_, _, err := r.GitErr("rev-parse", "--verify", "-q", "refs/heads/"+name)
		return err == nil
	}
	if err := repo.DeleteBranch(ctx, "old", false); err != nil {
		t.Fatal(err)
	}
	if exists("old") {
		t.Error("old is still there")
	}
	err := repo.DeleteBranch(ctx, "feature", false)
	wantRoute(t, err, "Repo.DeleteBranch"+writeRoute+"BranchNotFullyMerged")
	if !exists("feature") {
		t.Fatal("feature was deleted without force")
	}
	if err := repo.DeleteBranch(ctx, "feature", true); err != nil {
		t.Fatal(err)
	}
	if exists("feature") {
		t.Error("feature is still there after a forced delete")
	}
	wantRoute(t, repo.DeleteBranch(ctx, "main", true), "Repo.DeleteBranch"+writeRoute+"GitCommandFailed")
	wantRoute(t, repo.DeleteBranch(ctx, "nope", false), "Repo.DeleteBranch"+writeRoute+"GitCommandFailed")
	wantRoute(t, repo.DeleteBranch(ctx, "--all", false), "refName.GitRefInvalid")
}

func TestTags(t *testing.T) {
	ctx := context.Background()
	e := gittest.NewEnv(t)
	r := branchBase(t, e)
	repo := open(t, e, r.Dir)
	if err := repo.Tag(ctx, "v1", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("cat-file", "-t", "v1"); got != "commit" {
		t.Errorf("v1 is a %s, want a lightweight tag", got)
	}
	if err := repo.Tag(ctx, "v2", "Second release", "old"); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("cat-file", "-t", "v2"); got != "tag" {
		t.Errorf("v2 is a %s, want an annotated tag", got)
	}
	if got := r.Git("tag", "-l", "--format=%(contents:subject)", "v2"); got != "Second release" {
		t.Errorf("message = %q", got)
	}
	if got, want := r.Git("rev-parse", "v2^{commit}"), r.Git("rev-parse", "old"); got != want {
		t.Errorf("v2 names %s, want %s", got, want)
	}
	wantRoute(t, repo.Tag(ctx, "v1", "", ""), "Repo.Tag"+writeRoute+"GitCommandFailed")
	wantRoute(t, repo.Tag(ctx, "-d", "", ""), "refName.GitRefInvalid")
	wantRoute(t, repo.Tag(ctx, "v3", "", "--force"), "refName.GitRefInvalid")
	if err := repo.DeleteTag(ctx, "v1"); err != nil {
		t.Fatal(err)
	}
	if got := r.Git("tag", "-l"); got != "v2" {
		t.Errorf("tags = %q", got)
	}
	wantRoute(t, repo.DeleteTag(ctx, "v1"), "Repo.DeleteTag"+writeRoute+"GitCommandFailed")
	wantRoute(t, repo.DeleteTag(ctx, "-x"), "refName.GitRefInvalid")
}

func TestTagsAtARemote(t *testing.T) {
	ctx := context.Background()
	e := gittest.NewEnv(t)
	r := branchBase(t, e)
	bare := r.AddRemote("origin")
	r.Git("push", "-q", "origin", "main", "feature")
	repo := open(t, e, r.Dir)
	if err := repo.Tag(ctx, "v1", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tag(ctx, "v2", "Annotated", ""); err != nil {
		t.Fatal(err)
	}
	if got := bare.Git("tag", "-l"); got != "" {
		t.Fatalf("the remote has tags already: %q", got)
	}
	if err := repo.PushTags(ctx, "origin"); err != nil {
		t.Fatal(err)
	}
	if got := bare.Git("tag", "-l"); got != "v1\nv2" {
		t.Errorf("the remote's tags = %q", got)
	}
	tags, err := repo.RemoteTags(ctx, "origin")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tag := range tags {
		names = append(names, tag.Name)
		if len(tag.Commit) < 40 {
			t.Errorf("tag %s has the object %q", tag.Name, tag.Commit)
		}
	}
	if strings.Join(names, ",") != "v1,v2" {
		t.Errorf("RemoteTags = %v", names)
	}
	// pushing again has nothing to do and is no failure; the default remote is the one of the branch
	r.Git("push", "-q", "-u", "origin", "main")
	if err := repo.PushTags(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteRemoteRef(ctx, "origin", "v1", false); err != nil {
		t.Fatal(err)
	}
	if got := bare.Git("tag", "-l"); got != "v2" {
		t.Errorf("the remote's tags after the delete = %q", got)
	}
	if err := repo.DeleteRemoteRef(ctx, "origin", "feature", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bare.GitErr("rev-parse", "--verify", "-q", "refs/heads/feature"); err == nil {
		t.Error("feature is still at the remote")
	}
	wantRoute(t, repo.DeleteRemoteRef(ctx, "origin", "nope", false), "Repo.DeleteRemoteRef"+writeRoute+"PushRejected")
	wantRoute(t, repo.DeleteRemoteRef(ctx, "-x", "v2", false), "refName.GitRefInvalid")
	wantRoute(t, repo.DeleteRemoteRef(ctx, "origin", "--all", false), "refName.GitRefInvalid")
	wantRoute(t, repo.PushTags(ctx, "--all"), "refName.GitRefInvalid")
	_, err = repo.RemoteTags(ctx, "-x")
	wantRoute(t, err, "refName.GitRefInvalid")
	_, err = repo.RemoteTags(ctx, "nowhere")
	wantRoute(t, err, "Repo.RemoteTags->Queue.Do->Repo.RemoteTags.func1->Runner.Run->Runner.execute->classify.")
}

func TestRefsQuery(t *testing.T) {
	ctx := context.Background()
	e := gittest.NewEnv(t)
	r := branchBase(t, e)
	r.Tag("v1", "")
	r.Tag("v2", "Annotated")
	r.AddRemote("origin")
	r.Git("push", "-q", "origin", "main", "feature")
	r.Git("fetch", "-q", "origin")
	repo := open(t, e, r.Dir)

	names := func(refs []Ref) string {
		var out []string
		for _, ref := range refs {
			out = append(out, ref.Name)
		}
		return strings.Join(out, ",")
	}
	refs, err := repo.RefsQuery(ctx, RefQuery{Sort: "alphabetically", Patterns: []string{"refs/heads"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(refs); got != "feature,main,old" {
		t.Errorf("alphabetical branches = %s", got)
	}
	if refs[0].Subject != "" {
		t.Errorf("a query without details has a subject: %q", refs[0].Subject)
	}
	refs, err = repo.RefsQuery(ctx, RefQuery{Sort: "committerdate", Details: true, Patterns: []string{"refs/heads"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(refs); got != "feature,main,old" { // the newest commit first: feature, main, old
		t.Errorf("branches by date = %s", got)
	}
	if refs[0].Subject != "Add b" || refs[0].AuthorName != "Ada Example" || refs[2].Subject != "Add a" {
		t.Errorf("details = %q by %q, %q", refs[0].Subject, refs[0].AuthorName, refs[2].Subject)
	}
	refs, err = repo.RefsQuery(ctx, RefQuery{Details: true, Patterns: []string{"refs/tags"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(refs); got != "v1,v2" {
		t.Fatalf("tags = %s", got)
	}
	for _, ref := range refs { // the annotated tag shows the commit it points at
		if ref.Subject != "Change a" || ref.AuthorName != "Ada Example" {
			t.Errorf("tag %s: %q by %q", ref.Name, ref.Subject, ref.AuthorName)
		}
	}
	refs, err = repo.RefsQuery(ctx, RefQuery{Patterns: []string{"refs/remotes"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(refs); got != "origin/HEAD,origin/feature,origin/main" || refs[0].Remote != "origin" {
		t.Errorf("remote branches = %s", got)
	}
	all, err := repo.RefsQuery(ctx, RefQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3+3+2 {
		t.Errorf("all refs = %s", names(all))
	}

	tracking, err := repo.FindTrackingBranches(ctx, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracking) != 0 {
		t.Errorf("tracking = %v before any branch tracks", tracking)
	}
	r.Git("branch", "--set-upstream-to=origin/main", "main")
	r.Git("branch", "--track", "again", "origin/main")
	tracking, err = repo.FindTrackingBranches(ctx, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tracking, ",") != "again,main" {
		t.Errorf("tracking = %v", tracking)
	}
}

func TestParseRefsDetailsAndRemoteTags(t *testing.T) {
	line := func(fields ...string) []byte { return []byte(strings.Join(fields, "\x00") + "\n") }
	refs := ParseRefs(append(
		line("refs/heads/main", strings.Repeat("a", 40), "", "", "", "", "", "1700000000", "", "*", "Ada Example", "", "Fix the loader", ""),
		line("refs/tags/v1", strings.Repeat("b", 40), strings.Repeat("c", 40), "", "", "", "", "1700000000", "1700000100", " ", "Tagger", "Ada Example", "Release", "Fix the loader")...))
	if len(refs) != 2 {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0].AuthorName != "Ada Example" || refs[0].Subject != "Fix the loader" || !refs[0].Head {
		t.Errorf("branch = %+v", refs[0])
	}
	if refs[1].AuthorName != "Ada Example" || refs[1].Subject != "Fix the loader" || refs[1].Commit != strings.Repeat("c", 40) {
		t.Errorf("tag = %+v", refs[1])
	}
	tags := ParseRemoteTags([]byte("aaa\trefs/tags/v1\nbbb\trefs/tags/v2\nccc\trefs/tags/v2^{}\nddd\trefs/heads/main\nnot a line\neee\trefs/tags/with space\n"))
	if len(tags) != 2 || tags[0] != (RemoteRef{Name: "v1", Commit: "aaa"}) || tags[1] != (RemoteRef{Name: "v2", Commit: "bbb"}) {
		t.Errorf("tags = %+v", tags)
	}
}
