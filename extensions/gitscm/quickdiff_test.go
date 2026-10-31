package gitscm

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The quick diff providers against real repositories (step 24.11): each provider is asked through the fake API as the window asks it, for
// every kind of file, and the text of the original it names is read through the provider of the documents.

// quickDiffRepo has a file of every kind a rule of quickDiffProvider.ts looks at.
func quickDiffRepo(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	r := e.Init("qd")
	r.Write(".gitignore", "*.log\n")
	for _, f := range []string{"tracked.txt", "staged.txt", "both.txt", "clean.txt", "gone.txt", "sub dir/ünï.txt"} {
		r.Write(f, f+"\none\ntwo\nthree\n")
	}
	r.Commit("Add the files")
	r.Write("tracked.txt", "tracked.txt\nchanged in the work tree\ntwo\nthree\n")
	r.Write("staged.txt", "staged.txt\nstaged\ntwo\nthree\n")
	r.Git("add", "staged.txt")
	r.Write("both.txt", "both.txt\nstaged\ntwo\nthree\n")
	r.Git("add", "both.txt")
	r.Write("both.txt", "both.txt\nstaged and more\ntwo\nthree\n")
	r.Write("sub dir/ünï.txt", "changed\n")
	r.Write("untracked.txt", "u\n")
	r.Write("ignored.log", "i\n")
	r.Write("newly staged.txt", "n\n")
	r.Git("add", "newly staged.txt")
	if err := os.Symlink("tracked.txt", r.Path("link.txt")); err != nil {
		t.Fatal(err)
	}
	r.Git("add", "link.txt")
	r.Remove("gone.txt")
	return r
}

// quickDiffRig activates the extension over a quickDiffRepo.
func quickDiffRig(t *testing.T, o rigOptions) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := quickDiffRepo(t, e)
	o.folder = repo.Dir
	r := newRig(t, e, o)
	r.settle()
	return r, repo, r.repo(repo.Dir)
}

// providersOf are the two providers the extension added to a repository, by label.
func providersOf(t *testing.T, r *rig, rp *repository) (working, index *fakeapi.FakeQuickDiff) {
	t.Helper()
	for _, q := range r.api.SourceControlByHandle(rp.handle).QuickDiffProviders() {
		switch q.Label() {
		case "Git Local Changes (Working Tree)":
			working = q
		case "Git Local Changes (Index)":
			index = q
		}
	}
	if working == nil || index == nil {
		t.Fatalf("the providers of the repository are %v", r.api.SourceControlByHandle(rp.handle).QuickDiffProviders())
	}
	return working, index
}

func TestTheQuickDiffProvidersAreVSCodesTwo(t *testing.T) {
	r, _, rp := quickDiffRig(t, rigOptions{})
	working, index := providersOf(t, r, rp)
	if working.Kind() != extension.QuickDiffPrimary || index.Kind() != extension.QuickDiffSecondary {
		t.Errorf("kinds = %v and %v, want primary and secondary", working.Kind(), index.Kind())
	}
	if working.Root() != fileURI(rp.root) {
		t.Errorf("root = %s, want %s", working.Root(), fileURI(rp.root))
	}
}

func TestTheQuickDiffOriginalOfEveryKindOfFile(t *testing.T) {
	r, repo, rp := quickDiffRig(t, rigOptions{})
	working, index := providersOf(t, r, rp)
	ctx := context.Background()
	index0 := func(rel string) string { return toGitURI(rp.id, rp.root, repo.Path(rel), refIndex) }
	head := func(rel string) string { return toGitURI(rp.id, rp.root, repo.Path(rel), refHEAD) }
	tests := []struct {
		name         string
		file         string // relative to the repository, or an absolute path when it starts with "/"
		wantWorking  string
		wantIndex    string
		wantNoReason string
	}{
		{"a file changed in the work tree", "tracked.txt", index0("tracked.txt"), "", ""},
		{"a file that is staged and has more changes", "both.txt", index0("both.txt"), head("both.txt"), ""},
		{"a file that is staged only", "staged.txt", index0("staged.txt"), head("staged.txt"), ""},
		{"a file added to the index", "newly staged.txt", index0("newly staged.txt"), head("newly staged.txt"), ""},
		{"a file that has not changed", "clean.txt", index0("clean.txt"), "", ""},
		{"a file with a space and non-ASCII letters", "sub dir/ünï.txt", index0("sub dir/ünï.txt"), "", ""},
		{"an untracked file", "untracked.txt", "", "", "it is in no commit and no index"},
		{"a file that git ignores", "ignored.log", "", "", "ignored"},
		{"a symbolic link", "link.txt", "", "", "a link"},
		{"a file that was deleted", "gone.txt", "", "", "it is not there"},
		{"a file inside the git directory", ".git/config", "", "", "inside .git"},
		{"a file of no repository", "/etc/hostname", "", "", "outside"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := repo.Path(tt.file)
			if strings.HasPrefix(tt.file, "/") {
				path = tt.file
			}
			uri := fileURI(path)
			if got, err := working.Original(ctx, uri); err != nil || got != tt.wantWorking {
				t.Errorf("working tree original = %q, %v; want %q", got, err, tt.wantWorking)
			}
			if got, err := index.Original(ctx, uri); err != nil || got != tt.wantIndex {
				t.Errorf("index original = %q, %v; want %q", got, err, tt.wantIndex)
			}
		})
	}
	// a URI that is not a file of the disk
	for _, uri := range []string{"untitled:1", "emetos-virtual:/git/x//a.txt", "https://example.com/a.txt"} {
		if got, err := working.Original(ctx, uri); err != nil || got != "" {
			t.Errorf("working tree original of %s = %q, %v; want none", uri, got, err)
		}
	}
}

func TestTheQuickDiffOriginalIsTheTextOfTheIndexAndOfHEAD(t *testing.T) {
	r, repo, rp := quickDiffRig(t, rigOptions{})
	working, index := providersOf(t, r, rp)
	ctx := context.Background()
	uri := fileURI(repo.Path("both.txt"))
	read := func(q *fakeapi.FakeQuickDiff) string {
		t.Helper()
		orig, err := q.Original(ctx, uri)
		if err != nil || orig == "" {
			t.Fatalf("no original: %q, %v", orig, err)
		}
		text, err := r.api.ContentText(ctx, orig)
		if err != nil {
			t.Fatal(err)
		}
		return text
	}
	if got, want := read(working), "both.txt\nstaged\ntwo\nthree\n"; got != want {
		t.Errorf("the original of the working tree provider is %q, want the staged text %q", got, want)
	}
	if got, want := read(index), "both.txt\none\ntwo\nthree\n"; got != want {
		t.Errorf("the original of the index provider is %q, want the committed text %q", got, want)
	}
	// the API of the documents says the same, for the primary provider
	text, ok, err := r.api.Documents().QuickDiffOriginal(ctx, uri)
	if err != nil || !ok || text != "both.txt\nstaged\ntwo\nthree\n" {
		t.Errorf("QuickDiffOriginal = %q, %v, %v", text, ok, err)
	}
	if _, ok, _ := r.api.Documents().QuickDiffOriginal(ctx, fileURI(repo.Path("untracked.txt"))); ok {
		t.Error("QuickDiffOriginal has an original for an untracked file")
	}
}

func TestTheQuickDiffOfAFileInConflict(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("conflict")
	ours, theirs := repo.ConflictingBranches()
	_ = ours
	if err := repo.Merge(theirs); err == nil {
		t.Fatal("the merge did not conflict")
	}
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	working, index := providersOf(t, r, rp)
	uri := fileURI(repo.Path("src/loader.go"))
	if got, err := working.Original(context.Background(), uri); err != nil || got != "" {
		t.Errorf("the working tree provider has an original for a file in conflict: %q, %v", got, err)
	}
	if got, err := index.Original(context.Background(), uri); err != nil || got != "" {
		t.Errorf("the index provider has an original for a file in conflict: %q, %v", got, err)
	}
}

func TestTheQuickDiffIsToldOnlyWhenWhatTheProvidersAnswerChanged(t *testing.T) {
	r, repo, rp := quickDiffRig(t, rigOptions{})
	s := r.api.SourceControlByHandle(rp.handle)
	count := s.QuickDiffRefreshes()
	if count == 0 {
		t.Fatal("the host was not told after the first status")
	}
	same := func(label string) {
		t.Helper()
		r.refresh(rp)
		if got := s.QuickDiffRefreshes(); got != count {
			t.Errorf("%s: refreshes = %d, want %d", label, got, count)
		}
	}
	changed := func(label string) {
		t.Helper()
		if got := s.QuickDiffRefreshes(); got <= count {
			t.Errorf("%s: refreshes = %d, want more than %d", label, got, count)
		}
		count = s.QuickDiffRefreshes()
	}
	same("a refresh that finds the same state")
	// editing a file that is changed already, without staging: nothing the providers answer
	repo.Write("tracked.txt", "tracked.txt\nchanged again\ntwo\nthree\n")
	same("another edit of a file in the work tree")
	// staging a file: the index changed, and the file is in the Staged group
	r.run(rp, "git.stage", resourceArgs(rp, groupWorking, "tracked.txt"))
	changed("after a stage")
	// staging more of a file that is staged already: the statuses are the same, the index is not
	repo.Write("both.txt", "both.txt\nstaged and more and more\ntwo\nthree\n")
	r.refresh(rp)
	count = s.QuickDiffRefreshes()
	r.run(rp, "git.stage", resourceArgs(rp, groupWorking, "both.txt"))
	changed("after staging more of a file that was staged")
	// a commit moves HEAD
	repo.Commit("Commit what is staged")
	r.refresh(rp)
	changed("after a commit")
	// a file that becomes untracked
	repo.Write("brand-new.txt", "n\n")
	r.refresh(rp)
	changed("after a new untracked file")
}

func TestTheQuickDiffProvidersGoWithTheUntrustedRepository(t *testing.T) {
	r, _, rp := quickDiffRig(t, rigOptions{})
	s := r.api.SourceControlByHandle(rp.handle)
	if len(s.QuickDiffProviders()) != 2 {
		t.Fatalf("%d providers, want 2", len(s.QuickDiffProviders()))
	}
	rp.clearGroups()
	if len(s.QuickDiffProviders()) != 0 {
		t.Errorf("%d providers left after the groups were cleared", len(s.QuickDiffProviders()))
	}
	before := s.QuickDiffRefreshes()
	rp.quickDiffChanged() // does nothing without providers
	if s.QuickDiffRefreshes() != before {
		t.Error("a repository without providers told the host")
	}
}

func TestTheOwnerOfAFileIsTheDeepestOpenRepository(t *testing.T) {
	x := &gitscm{}
	x.owners.set([]string{"/work/outer", "/work/outer/inner", "/work/other"})
	for path, want := range map[string]string{
		"/work/outer/a.go":             "/work/outer",
		"/work/outer/inner/a.go":       "/work/outer/inner",
		"/work/outer/inner/deep/a.go":  "/work/outer/inner",
		"/work/outer/innerer/a.go":     "/work/outer",
		"/work/other/a.go":             "/work/other",
		"/work/elsewhere/a.go":         "",
		"/work/outer/inner":            "/work/outer/inner",
		filepath.Join("/work", "x.go"): "",
	} {
		if got := x.ownerOf(path); got != want {
			t.Errorf("owner of %s = %q, want %q", path, got, want)
		}
	}
}

func TestAScopeWithoutEditorsHasNoQuickDiffAndNoLogLine(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := quickDiffRepo(t, e)
	var logged bytes.Buffer
	r := newRig(t, e, rigOptions{folder: repo.Dir, noStart: true, logger: slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	r.api.SetQuickDiffUnavailable(true)
	if err := r.x.Activate(context.Background(), r.api); err != nil {
		t.Fatal(err)
	}
	r.settle()
	rp := r.repo(repo.Dir)
	if got := r.api.SourceControlByHandle(rp.handle).QuickDiffProviders(); len(got) != 0 {
		t.Errorf("providers = %v, want none", got)
	}
	if strings.Contains(strings.ToLower(logged.String()), "quick diff") {
		t.Errorf("the extension logged about the quick diff in a scope that has none:\n%s", logged.String())
	}
	// and the repository still works
	if len(rp.res.working) == 0 {
		t.Error("the repository has no changes")
	}
}
