package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// open makes a Runner for the world and opens the repository at dir.
func open(t *testing.T, e *gittest.Env, dir string) *Repo {
	t.Helper()
	repo, err := Open(context.Background(), NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: e.Vars()}), nil, dir)
	if err != nil {
		t.Fatalf("Open(%s): %v", dir, err)
	}
	return repo
}

func fixtureRepo(t *testing.T, kind string, opts gittest.FixtureOptions) (*gittest.Env, *Repo, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), kind)
	gittest.Fixture(t, kind, dir, opts)
	e := gittest.NewEnv(t)
	return e, open(t, e, dir), dir
}

func TestOpen(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	r.Write("src/a.txt", "a\n")
	r.Commit("base")
	bare := e.InitBare("bare.git")
	r.Git("worktree", "add", "-q", filepath.Join(e.Work, "linked"), "-b", "linked")
	sub := e.Init("lib")
	sub.Write("lib.go", "package lib\n")
	sub.Commit("lib")
	r.Submodule("vendor/lib", sub)
	runner := NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: e.Vars()})
	ctx := context.Background()

	t.Run("the top level of a repository", func(t *testing.T) {
		repo, err := Open(ctx, runner, nil, r.Dir)
		if err != nil {
			t.Fatal(err)
		}
		want := RepoInfo{WorkTree: r.Dir, GitDir: filepath.Join(r.Dir, ".git"), CommonDir: filepath.Join(r.Dir, ".git"), InsideWorkTree: true}
		if repo.Root != r.Dir || repo.Info != want {
			t.Errorf("root %s info %+v\nwant %+v", repo.Root, repo.Info, want)
		}
	})
	t.Run("a folder inside the repository is not its root", func(t *testing.T) {
		_, err := Open(ctx, runner, nil, filepath.Join(r.Dir, "src"))
		appErr := routeOf(t, err, "Open.NotAtRepositoryRoot")
		if appErr != nil && appErr.Metadata["topLevel"] != r.Dir {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
	})
	t.Run("Discover finds the repository from a subfolder", func(t *testing.T) {
		repo, err := Discover(ctx, runner, nil, filepath.Join(r.Dir, "src"))
		if err != nil || repo.Root != r.Dir || !repo.Info.InsideWorkTree {
			t.Errorf("got %+v, %v", repo, err)
		}
	})
	t.Run("a folder that is no repository", func(t *testing.T) {
		dir := filepath.Join(e.Work, "plain")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Open(ctx, runner, nil, dir)
		wantRoute(t, err, "Open->revParse->Queue.Do->revParse.func1->Runner.Run->Runner.execute->classify.NotAGitRepository")
		_, err = Discover(ctx, runner, nil, dir)
		wantRoute(t, err, "Discover->revParse->Queue.Do->revParse.func1->Runner.Run->Runner.execute->classify.NotAGitRepository")
	})
	t.Run("a bare repository", func(t *testing.T) {
		repo, err := Open(ctx, runner, nil, bare.Dir)
		if err != nil || !repo.Info.Bare || repo.Info.WorkTree != "" || repo.Root != bare.Dir || repo.Info.GitDir != bare.Dir {
			t.Errorf("got %+v, %v", repo, err)
		}
	})
	t.Run("a linked worktree has its own git directory and the common one", func(t *testing.T) {
		repo := open(t, e, filepath.Join(e.Work, "linked"))
		if repo.Info.CommonDir != filepath.Join(r.Dir, ".git") || !strings.HasPrefix(repo.Info.GitDir, filepath.Join(r.Dir, ".git", "worktrees")) {
			t.Errorf("info = %+v", repo.Info)
		}
	})
	t.Run("a submodule knows its super project", func(t *testing.T) {
		repo := open(t, e, filepath.Join(r.Dir, "vendor", "lib"))
		if repo.Info.SuperProject != r.Dir || repo.Info.WorkTree != filepath.Join(r.Dir, "vendor", "lib") {
			t.Errorf("info = %+v", repo.Info)
		}
	})
	t.Run("a repository the user does not own", func(t *testing.T) {
		unsafe := NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: withVars(e.Vars(), "GIT_TEST_ASSUME_DIFFERENT_OWNER=1")})
		_, err := Open(ctx, unsafe, nil, r.Dir)
		wantRoute(t, err, "Open->revParse->Queue.Do->revParse.func1->Runner.Run->Runner.execute->classify.UnsafeRepository")
	})
	t.Run("a shallow clone", func(t *testing.T) {
		shallow := filepath.Join(e.Work, "shallow")
		e.Git(e.Work, "clone", "-q", "--depth", "1", "file://"+r.Dir, shallow)
		e.Trust(shallow)
		if repo := open(t, e, shallow); !repo.Info.Shallow {
			t.Errorf("info = %+v", repo.Info)
		}
	})
}

func sortedByPath(es []StatusEntry) []StatusEntry {
	out := append([]StatusEntry(nil), es...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func TestStatusOfTheSmallFixture(t *testing.T) {
	_, repo, _ := fixtureRepo(t, "small", gittest.FixtureOptions{})
	ctx := context.Background()
	res, err := repo.Status(ctx, StatusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []StatusEntry{
		{X: 'M', Y: ' ', Path: "README.md"},
		{X: ' ', Y: 'D', Path: "docs/notes.md"},
		{X: '?', Y: '?', Path: "docs/todo.md"},
		{X: ' ', Y: 'M', Path: "src/loader.go"},
		{X: 'R', Y: ' ', Path: "src/loader_check_test.go", OrigPath: "src/loader_test.go"},
	}
	if got := sortedByPath(res.Entries); !reflect.DeepEqual(got, want) || res.Total != 5 || res.HitLimit {
		t.Errorf("status = %+v (total %d, hit %v)\nwant %+v", got, res.Total, res.HitLimit, want)
	}
	// The groups VS Code makes of them.
	var merge, index, work int
	for _, e := range res.Entries {
		m, i, w := e.Statuses()
		if m != StatusNone {
			merge++
		}
		if i != StatusNone {
			index++
		}
		if w != StatusNone {
			work++
		}
	}
	if merge != 0 || index != 2 || work != 3 {
		t.Errorf("groups: merge %d index %d work %d", merge, index, work)
	}

	t.Run("hidden untracked files", func(t *testing.T) {
		res, err := repo.Status(ctx, StatusOptions{Untracked: UntrackedNone})
		if err != nil || len(res.Entries) != 4 {
			t.Errorf("got %d entries, %v", len(res.Entries), err)
		}
		for _, e := range res.Entries {
			if e.X == '?' {
				t.Errorf("untracked file listed: %+v", e)
			}
		}
	})
	t.Run("the similarity threshold changes what is a rename", func(t *testing.T) {
		res, err := repo.Status(ctx, StatusOptions{SimilarityThreshold: 100})
		if err != nil || res.Total != 5 {
			t.Errorf("got %d, %v", res.Total, err)
		}
	})
	t.Run("ignored files", func(t *testing.T) {
		e2 := gittest.NewEnv(t)
		r := e2.Init("ig")
		r.Write(".gitignore", "*.log\nbuild/\n")
		r.Write("a.txt", "a\n")
		r.Commit("base")
		r.Write("x.log", "x\n")
		r.Write("build/out.o", "o\n")
		repo2 := open(t, e2, r.Dir)
		with, err := repo2.Status(ctx, StatusOptions{Ignored: true})
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, e := range sortedByPath(with.Entries) {
			paths = append(paths, string(e.X)+string(e.Y)+" "+e.Path)
		}
		if !reflect.DeepEqual(paths, []string{"!! build/out.o", "!! x.log"}) { // -uall lists the files of an ignored folder
			t.Errorf("ignored entries: %q", paths)
		}
		without, err := repo2.Status(ctx, StatusOptions{})
		if err != nil || len(without.Entries) != 0 {
			t.Errorf("without --ignored: %+v, %v", without.Entries, err)
		}
	})
}

func TestStatusOfAConflict(t *testing.T) {
	_, repo, _ := fixtureRepo(t, "conflict", gittest.FixtureOptions{})
	res, err := repo.Status(context.Background(), StatusOptions{})
	if err != nil || len(res.Entries) != 1 || res.Entries[0] != (StatusEntry{X: 'U', Y: 'U', Path: "src/loader.go"}) {
		t.Fatalf("got %+v, %v", res, err)
	}
	if m, i, w := res.Entries[0].Statuses(); m != StatusBothModified || i != StatusNone || w != StatusNone {
		t.Errorf("statuses = %d %d %d", m, i, w)
	}
}

// All seven forms of a conflict, made by real merges: UU, AA, DU, UD, and DD, AU, UA from two renames of one file.
func TestStatusOfEveryFormOfConflict(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	body := func(f, first string) string { // files that differ from each other, so that git finds no rename among them
		return first + "\nthe file " + f + " has its own second line\nand a third line of " + f + "\nand a fourth line of " + f + "\n"
	}
	for _, f := range []string{"uu", "du", "ud", "rn"} {
		r.Write(f+".txt", body(f, "line one"))
	}
	r.Commit("base")
	r.Branch("theirs", "")
	r.Checkout("theirs")
	r.Write("uu.txt", body("uu", "theirs"))
	r.Write("aa.txt", "theirs added\n")
	r.Git("rm", "-q", "ud.txt")
	r.Write("du.txt", body("du", "theirs"))
	r.Git("mv", "rn.txt", "rn-theirs.txt")
	r.Git("add", "-A")
	r.Commit("theirs")
	r.Checkout("main")
	r.Write("uu.txt", body("uu", "ours"))
	r.Write("aa.txt", "ours added\n")
	r.Write("ud.txt", body("ud", "ours"))
	r.Git("rm", "-q", "du.txt")
	r.Git("mv", "rn.txt", "rn-ours.txt")
	r.Git("add", "-A")
	r.Commit("ours")
	if err := r.Merge("theirs"); err == nil {
		t.Fatal("the merge did not conflict")
	}
	res, err := open(t, e, r.Dir).Status(context.Background(), StatusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Status{}
	for _, en := range res.Entries {
		if merge, _, _ := en.Statuses(); merge != StatusNone {
			got[en.Path] = merge
		}
	}
	want := map[string]Status{
		"uu.txt": StatusBothModified, "aa.txt": StatusBothAdded, "du.txt": StatusDeletedByUs, "ud.txt": StatusDeletedByThem,
		"rn.txt": StatusBothDeleted, "rn-ours.txt": StatusAddedByUs, "rn-theirs.txt": StatusAddedByThem,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merge statuses = %v\nwant %v\nentries %+v", got, want, res.Entries)
	}
}

func TestStatusStopsAtTheLimit(t *testing.T) {
	_, repo, _ := fixtureRepo(t, "many", gittest.FixtureOptions{Files: 3000})
	ctx := context.Background()
	res, err := repo.Status(ctx, StatusOptions{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !res.HitLimit || len(res.Entries) != 1000 || res.Total <= 1000 {
		t.Errorf("hit %v, %d entries, total %d", res.HitLimit, len(res.Entries), res.Total)
	}
	full, err := repo.Status(ctx, StatusOptions{})
	if err != nil || full.HitLimit || len(full.Entries) != 3000 || full.Total != 3000 {
		t.Errorf("without a limit: hit %v, %d entries, %v", full.HitLimit, len(full.Entries), err)
	}
	if !reflect.DeepEqual(full.Entries[:1000], res.Entries) {
		t.Error("the limited answer is not the start of the whole")
	}
	exact, err := repo.Status(ctx, StatusOptions{Limit: 3000})
	if err != nil || exact.HitLimit || len(exact.Entries) != 3000 {
		t.Errorf("a limit equal to the number of entries: hit %v, %d, %v", exact.HitLimit, len(exact.Entries), err)
	}
}

func TestLogOfTheHistoryFixture(t *testing.T) {
	_, repo, _ := fixtureRepo(t, "history", gittest.FixtureOptions{})
	ctx := context.Background()

	t.Run("the default is 32 commits, newest first", func(t *testing.T) {
		commits, err := repo.Log(ctx, LogOptions{})
		if err != nil || len(commits) != 32 {
			t.Fatalf("%d commits, %v", len(commits), err)
		}
		if commits[0].Message != "Update the loader here, revision 303" || commits[0].AuthorName != "Ada Example" || commits[0].AuthorEmail != "ada@example.com" {
			t.Errorf("first = %+v", commits[0])
		}
		if !contains(commits[0].RefNames, "HEAD -> main") {
			t.Errorf("ref names of HEAD: %q", commits[0].RefNames)
		}
		if len(commits[0].Parents) != 1 {
			t.Errorf("parents = %q", commits[0].Parents)
		}
	})
	t.Run("a number of commits, a skip and a path", func(t *testing.T) {
		all, err := repo.Log(ctx, LogOptions{MaxEntries: -1})
		if err != nil {
			t.Fatal(err)
		}
		// 303 on main, 20 and 18 on the feature branches (all reachable from the branches, not from HEAD): the log from HEAD
		// has the commits of main and of the merged parts of the branches.
		if len(all) < 303 {
			t.Fatalf("%d commits", len(all))
		}
		page, err := repo.Log(ctx, LogOptions{MaxEntries: 5, Skip: 10})
		if err != nil || len(page) != 5 || page[0].Hash != all[10].Hash || page[4].Hash != all[14].Hash {
			t.Errorf("page = %+v, %v", page, err)
		}
		forFile, err := repo.Log(ctx, LogOptions{MaxEntries: 3, Path: "src/loader.go"})
		if err != nil || len(forFile) != 3 {
			t.Fatalf("for file: %d, %v", len(forFile), err)
		}
		authors := map[string]bool{}
		fileAll, _ := repo.Log(ctx, LogOptions{MaxEntries: -1, Path: "src/loader.go"})
		for _, c := range fileAll {
			authors[c.AuthorName] = true
		}
		if len(authors) != 3 {
			t.Errorf("authors of src/loader.go: %v", authors)
		}
	})
	t.Run("a range, an author and a grep", func(t *testing.T) {
		ahead, err := repo.Log(ctx, LogOptions{Range: "origin/main..main", MaxEntries: -1})
		if err != nil || len(ahead) != 1 || ahead[0].Message != "Update the loader here, revision 303" {
			t.Errorf("ahead = %+v, %v", ahead, err)
		}
		behind, err := repo.Log(ctx, LogOptions{Range: "main..origin/main", MaxEntries: -1})
		if err != nil || len(behind) != 2 {
			t.Errorf("behind = %d, %v", len(behind), err)
		}
		bo, err := repo.Log(ctx, LogOptions{Author: "Bo Example", MaxEntries: 4})
		if err != nil || len(bo) != 4 {
			t.Fatalf("bo: %d, %v", len(bo), err)
		}
		for _, c := range bo {
			if c.AuthorName != "Bo Example" {
				t.Errorf("author %s", c.AuthorName)
			}
		}
		g, err := repo.Log(ctx, LogOptions{Grep: "feature a, STEP 1[0-9]$", MaxEntries: -1, Range: "feature/a"})
		if err != nil || len(g) != 10 {
			t.Errorf("grep: %d, %v", len(g), err)
		}
	})
	t.Run("merges, tags and short stats", func(t *testing.T) {
		merges, err := repo.Log(ctx, LogOptions{Grep: "^Merge feature", MaxEntries: -1})
		if err != nil || len(merges) != 2 {
			t.Fatalf("merges: %d, %v", len(merges), err)
		}
		for _, m := range merges {
			if len(m.Parents) != 2 {
				t.Errorf("merge %s has parents %q", m.Message, m.Parents)
			}
		}
		withStats, err := repo.Log(ctx, LogOptions{MaxEntries: 4, ShortStats: true})
		if err != nil || len(withStats) != 4 {
			t.Fatalf("stats: %d, %v", len(withStats), err)
		}
		if withStats[0].ShortStat == nil || withStats[0].ShortStat.Files != 1 || withStats[0].ShortStat.Insertions != 1 || withStats[0].ShortStat.Deletions != 1 {
			t.Errorf("stat = %+v", withStats[0].ShortStat)
		}
		reversed, err := repo.Log(ctx, LogOptions{Range: "v0.1.0..main", MaxEntries: 3, Reverse: true})
		if err != nil || len(reversed) != 3 || !reversed[0].AuthorDate.Before(reversed[2].AuthorDate) {
			// git applies -n before --reverse: the three newest, then reversed (the oldest of the three first).
			t.Errorf("reverse: %d, %v", len(reversed), err)
		}
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestLogOfOddCommits(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	r.Write("a.txt", "a\n")
	r.Commit("Subject ☃ \"quoted\" 'single'\n\nBody line one\n\n  indented line\nCo-authored-by: Bo Example <bo@example.com>\n")
	r.Write("a.txt", "b\n")
	r.CommitAs("Zoë Ünïcode", "zoe@example.com", "Another: 3 files changed, 1 insertion(+)", "-a")
	repo := open(t, e, r.Dir)
	commits, err := repo.Log(context.Background(), LogOptions{ShortStats: true})
	if err != nil || len(commits) != 2 {
		t.Fatalf("%d commits, %v", len(commits), err)
	}
	if commits[0].AuthorName != "Zoë Ünïcode" || commits[0].Message != "Another: 3 files changed, 1 insertion(+)" {
		t.Errorf("first = %+v", commits[0])
	}
	if commits[1].Message != "Subject ☃ \"quoted\" 'single'\n\nBody line one\n\n  indented line\nCo-authored-by: Bo Example <bo@example.com>" {
		t.Errorf("second message = %q", commits[1].Message)
	}
	if !reflect.DeepEqual(commits[1].CoAuthors, []CoAuthor{{"Bo Example", "bo@example.com"}}) {
		t.Errorf("co-authors = %+v", commits[1].CoAuthors)
	}
	if commits[0].ShortStat == nil || commits[1].ShortStat == nil {
		t.Errorf("stats = %+v %+v", commits[0].ShortStat, commits[1].ShortStat)
	}

	t.Run("a repository with no commit has no log", func(t *testing.T) {
		empty := e.Init("empty")
		got, err := open(t, e, empty.Dir).Log(context.Background(), LogOptions{})
		if err != nil || len(got) != 0 {
			t.Errorf("got %+v, %v", got, err)
		}
	})
}

func TestRefsOfTheHistoryFixture(t *testing.T) {
	_, repo, dir := fixtureRepo(t, "history", gittest.FixtureOptions{})
	ctx := context.Background()
	refs, err := repo.Refs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Ref{}
	for _, r := range refs {
		byName[r.FullName] = r
	}
	main := byName["refs/heads/main"]
	if !main.Head || main.Ahead != 1 || main.Behind != 2 || main.Upstream == nil || *main.Upstream != (Upstream{"origin", "main", "refs/remotes/origin/main"}) {
		t.Errorf("main = %+v upstream %+v", main, main.Upstream)
	}
	if fa := byName["refs/heads/feature/a"]; fa.Head || fa.Upstream == nil || fa.Upstream.Name != "feature/a" || fa.Ahead != 0 || fa.Behind != 0 {
		t.Errorf("feature/a = %+v", fa)
	}
	if rm := byName["refs/remotes/origin/main"]; rm.Type != RefRemoteHead || rm.Remote != "origin" || rm.Name != "origin/main" {
		t.Errorf("origin/main = %+v", rm)
	}
	if rb := byName["refs/remotes/origin/feature/b"]; rb.Remote != "origin" || rb.Name != "origin/feature/b" {
		t.Errorf("origin/feature/b = %+v", rb)
	}
	light, annotated := byName["refs/tags/v0.1.0"], byName["refs/tags/v1.0.0"]
	if light.Type != RefTag || light.Object != light.Commit {
		t.Errorf("v0.1.0 = %+v", light)
	}
	if annotated.Type != RefTag || annotated.Object == annotated.Commit || annotated.CommitDate.IsZero() {
		t.Errorf("v1.0.0 = %+v", annotated)
	}
	if main.CommitDate.IsZero() {
		t.Error("no commit date")
	}
	// The commit of the annotated tag is the commit of the merge of feature/a, which is in the log.
	log, _ := repo.Log(ctx, LogOptions{Grep: "^Merge feature/a", MaxEntries: 1})
	if len(log) != 1 || log[0].Hash != annotated.Commit {
		t.Errorf("tag commit %s, merge %v", annotated.Commit, log)
	}
	// Patterns select.
	heads, err := repo.Refs(ctx, "refs/heads")
	if err != nil || len(heads) != 3 {
		t.Errorf("heads = %d, %v", len(heads), err)
	}
	tags, err := repo.Refs(ctx, "refs/tags")
	if err != nil || len(tags) != 2 {
		t.Errorf("tags = %d, %v", len(tags), err)
	}
	_ = dir
}

func TestRefsWithAGoneUpstream(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	r.Write("a.txt", "a\n")
	r.Commit("base")
	r.AddRemote("origin")
	r.Git("push", "-q", "-u", "origin", "main")
	r.Branch("topic", "")
	r.Git("push", "-q", "-u", "origin", "topic")
	r.Git("push", "-q", "origin", "--delete", "topic")
	r.Git("fetch", "-q", "--prune", "origin")
	refs, err := open(t, e, r.Dir).Refs(context.Background(), "refs/heads")
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if ref.Name == "topic" && (!ref.Gone || ref.Upstream != nil) {
			t.Errorf("topic = %+v", ref)
		}
	}
}

func TestConfigRemotesAndStashes(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	r.Write("a.txt", "a\n")
	r.Commit("base")
	bare := r.AddRemote("origin")
	r.Git("remote", "add", "fork", "https://example.com/fork.git")
	r.Git("remote", "set-url", "--push", "fork", "no_push")
	r.Git("config", "alias.odd", "!echo \"two\nlines\"")
	r.Git("config", "branch.main.description", "a description; with = signs")
	r.Write("a.txt", "stash me\n")
	r.Stash("first stash", false)
	r.Write("b.txt", "b\n")
	r.Stash("second: with colon", true)
	repo := open(t, e, r.Dir)
	ctx := context.Background()

	cfg, err := repo.Config(ctx, "local")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ConfigEntry{}
	for _, c := range cfg {
		got[c.Key] = c
	}
	if got["remote.origin.url"].Value != bare.Dir || got["alias.odd"].Value != "!echo \"two\nlines\"" || got["branch.main.description"].Value != "a description; with = signs" ||
		got["core.bare"].Value != "false" {
		t.Errorf("config = %+v", got)
	}
	if _, ok := got["user.name"]; ok {
		t.Error("the world's identity (set outside the repository) is in the local configuration")
	}
	if global, err := repo.Config(ctx, "global"); err != nil || len(global) != 0 {
		t.Errorf("global = %+v, %v", global, err)
	}

	remotes, err := repo.Remotes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []Remote{
		{Name: "fork", FetchURL: "https://example.com/fork.git", PushURL: "no_push", ReadOnly: true},
		{Name: "origin", FetchURL: bare.Dir, PushURL: bare.Dir},
	}
	if !reflect.DeepEqual(remotes, want) {
		t.Errorf("remotes = %+v", remotes)
	}
	// The same remotes read from the text of .git/config by the other parser.
	text, err := os.ReadFile(filepath.Join(r.Dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if fromText := ParseRemotesConfig(string(text)); len(fromText) != 2 {
		t.Errorf("ParseRemotesConfig = %+v", fromText)
	}

	stashes, err := repo.Stashes(ctx)
	if err != nil || len(stashes) != 2 {
		t.Fatalf("stashes = %+v, %v", stashes, err)
	}
	if stashes[0].Index != 0 || stashes[0].Branch != "main" || stashes[0].Description != "second: with colon" || len(stashes[0].Parents) < 2 {
		t.Errorf("stash 0 = %+v", stashes[0])
	}
	if stashes[1].Index != 1 || stashes[1].Description != "first stash" || stashes[1].AuthorDate.IsZero() {
		t.Errorf("stash 1 = %+v", stashes[1])
	}
	none, err := open(t, e, e.Init("fresh").Dir).Stashes(ctx)
	if err != nil || len(none) != 0 {
		t.Errorf("no stash: %+v, %v", none, err)
	}
}

func TestBlameOfTheHistoryFixture(t *testing.T) {
	_, repo, _ := fixtureRepo(t, "history", gittest.FixtureOptions{})
	ctx := context.Background()
	commits, err := repo.Blame(ctx, BlameOptions{Path: "src/loader.go"})
	if err != nil {
		t.Fatal(err)
	}
	covered := make([]bool, 41)
	for _, c := range commits {
		if len(c.Hash) != 40 || c.Author == "" || c.AuthorEmail == "" || c.Summary == "" || c.AuthorTime.IsZero() {
			t.Errorf("incomplete commit %+v", c)
		}
		for _, rg := range c.Ranges {
			for l := rg.Start; l <= rg.End; l++ {
				if covered[l] {
					t.Errorf("line %d is blamed twice", l)
				}
				covered[l] = true
			}
		}
	}
	for l := 1; l <= 40; l++ {
		if !covered[l] {
			t.Errorf("line %d is not blamed", l)
		}
	}
	if len(commits) < 2 {
		t.Errorf("only %d commits are blamed", len(commits))
	}
	// A revision, and ignoring whitespace.
	if _, err := repo.Blame(ctx, BlameOptions{Path: "src/loader.go", Rev: "v1.0.0", IgnoreWhitespace: true}); err != nil {
		t.Errorf("blame at a tag: %v", err)
	}
	_, err = repo.Blame(ctx, BlameOptions{Path: "no/such/file.go"})
	wantRoute(t, err, "Repo.Blame->Repo.stream->Queue.Do->Repo.stream.func1->Runner.RunStream->Runner.execute->classify.NoPathFound")
}

func TestNameStatusRevListAndMergeBase(t *testing.T) {
	_, repo, _ := fixtureRepo(t, "small", gittest.FixtureOptions{})
	ctx := context.Background()
	work, err := repo.NameStatus(ctx, DiffSpec{})
	if err != nil || !reflect.DeepEqual(work, []NameStatus{{'D', 0, "docs/notes.md", ""}, {'M', 0, "src/loader.go", ""}}) {
		t.Errorf("work tree: %+v, %v", work, err)
	}
	staged, err := repo.NameStatus(ctx, DiffSpec{Cached: true, SimilarityThreshold: 50})
	if err != nil || !reflect.DeepEqual(staged, []NameStatus{{'M', 0, "README.md", ""}, {'R', 100, "src/loader_check_test.go", "src/loader_test.go"}}) {
		t.Errorf("index: %+v, %v", staged, err)
	}
	between, err := repo.NameStatus(ctx, DiffSpec{Revs: []string{"HEAD~1", "HEAD"}, Filter: "AMDR"})
	if err != nil || !reflect.DeepEqual(between, []NameStatus{{'M', 0, "docs/notes.md", ""}}) {
		t.Errorf("between: %+v, %v", between, err)
	}
	paths, err := repo.NameStatus(ctx, DiffSpec{Paths: []string{"src"}})
	if err != nil || len(paths) != 1 || paths[0].Path != "src/loader.go" {
		t.Errorf("paths: %+v, %v", paths, err)
	}

	if n, err := repo.RevListCount(ctx, "HEAD"); err != nil || n != 2 {
		t.Errorf("count = %d, %v", n, err)
	}
	if _, err := repo.RevListCount(ctx, "nonexistent-ref"); err == nil {
		t.Error("count of a missing ref")
	}
	base, ok, err := repo.MergeBase(ctx, "HEAD", "HEAD~1")
	if err != nil || !ok || len(base) != 40 {
		t.Errorf("merge base = %q %v %v", base, ok, err)
	}
	e := gittest.NewEnv(t)
	r := e.Init("two")
	r.Write("a", "a\n")
	r.Commit("a")
	r.Git("checkout", "-q", "--orphan", "other")
	r.Write("b", "b\n")
	r.Commit("b")
	if base, ok, err := open(t, e, r.Dir).MergeBase(ctx, "main", "other"); err != nil || ok || base != "" {
		t.Errorf("unrelated histories: %q %v %v", base, ok, err)
	}
}

func TestAheadBehindOfTheHistoryFixture(t *testing.T) {
	_, repo, _ := fixtureRepo(t, "history", gittest.FixtureOptions{})
	ahead, behind, err := repo.AheadBehind(context.Background(), "main", "origin/main")
	if err != nil || ahead != 1 || behind != 2 {
		t.Errorf("ahead %d behind %d, %v", ahead, behind, err)
	}
	if _, _, err := repo.AheadBehind(context.Background(), "main", "nonexistent"); err == nil {
		t.Error("a missing ref")
	}
}

func TestBlobsObjectsAndTheIndex(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	r.Write("a.txt", "committed\n")
	r.Write("dir/b.txt", "bee\n")
	r.Commit("base")
	r.Write("a.txt", "staged\n")
	r.Git("add", "a.txt")
	r.Write("a.txt", "work tree\n")
	repo := open(t, e, r.Dir)
	ctx := context.Background()

	for _, tc := range []struct{ object, want string }{
		{"HEAD:a.txt", "committed\n"},
		{":a.txt", "staged\n"},
		{"HEAD:dir/b.txt", "bee\n"},
	} {
		got, err := repo.ReadBlob(ctx, tc.object)
		if err != nil || string(got) != tc.want {
			t.Errorf("ReadBlob(%s) = %q, %v", tc.object, got, err)
		}
	}
	_, err := repo.ReadBlob(ctx, "HEAD:nope.txt")
	wantRoute(t, err, "classify.CantOpenResource")

	t.Run("a file of exactly 16 MiB is read, one byte more is too large", func(t *testing.T) {
		r.Write("big.bin", strings.Repeat("x", MaxBlobSize))
		r.Write("bigger.bin", strings.Repeat("x", MaxBlobSize+1))
		r.Git("add", "big.bin", "bigger.bin")
		got, err := repo.ReadBlob(ctx, ":big.bin")
		if err != nil || len(got) != MaxBlobSize {
			t.Errorf("exactly the limit: %d bytes, %v", len(got), err)
		}
		_, err = repo.ReadBlob(ctx, ":bigger.bin")
		if appErr := routeOf(t, err, "Repo.ReadBlob.func1.GitContentTooLarge"); appErr != nil && appErr.Metadata["limit"] != MaxBlobSize {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
	})

	t.Run("the details of a path", func(t *testing.T) {
		d, err := repo.ObjectDetails(ctx, "HEAD", "dir/b.txt")
		if err != nil || d.Mode != "100644" || d.Size != 4 || len(d.Object) != 40 {
			t.Errorf("at HEAD: %+v, %v", d, err)
		}
		d, err = repo.ObjectDetails(ctx, "", "a.txt")
		if err != nil || d.Size != int64(len("staged\n")) {
			t.Errorf("in the index: %+v, %v", d, err)
		}
		_, err = repo.ObjectDetails(ctx, "HEAD", "no/such")
		wantRoute(t, err, "Repo.ObjectDetails.UnknownPath")
		_, err = repo.ObjectDetails(ctx, "", "no/such")
		wantRoute(t, err, "Repo.ObjectDetails.UnknownPath")
	})

	t.Run("the stages of a conflict", func(t *testing.T) {
		c := e.Init("conflict")
		ours, theirs := c.ConflictingBranches()
		_, _ = ours, c.Merge(theirs)
		cr := open(t, e, c.Dir)
		entries, err := cr.LsFiles(ctx, "src/loader.go")
		if err != nil || len(entries) != 3 {
			t.Fatalf("entries = %+v, %v", entries, err)
		}
		for i, e := range entries {
			if e.Stage != []string{"1", "2", "3"}[i] {
				t.Errorf("stage of entry %d is %s", i, e.Stage)
			}
		}
		d, err := cr.ObjectDetails(ctx, ":2", "src/loader.go")
		if err != nil || d.Object != entries[1].Object {
			t.Errorf("stage 2: %+v, %v", d, err)
		}
	})

	t.Run("a tree", func(t *testing.T) {
		tree, err := repo.LsTree(ctx, "HEAD", "", false)
		if err != nil || len(tree) != 2 {
			t.Fatalf("tree = %+v, %v", tree, err)
		}
		if tree[0].Path != "a.txt" || tree[0].Type != "blob" || tree[1].Path != "dir" || tree[1].Type != "tree" || tree[1].Size != "-" {
			t.Errorf("tree = %+v", tree)
		}
		rec, err := repo.LsTree(ctx, "HEAD", "", true)
		if err != nil || len(rec) != 2 || rec[1].Path != "dir/b.txt" {
			t.Errorf("recursive = %+v, %v", rec, err)
		}
	})
}

func TestOddFileNamesRoundTripThroughEveryParser(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	names := []string{"my file.txt", "quote\"d.txt", "back\\slash.txt", "tab\there.txt", "new\nline.txt", "ünï ☃.txt", "bad\xff\xfe.txt", "-dash.txt", "[brackets].txt", "dir with space/x y.txt"}
	for _, n := range names {
		r.Write(n, "content of "+n+"\n")
	}
	r.Commit("odd names")
	repo := open(t, e, r.Dir)
	ctx := context.Background()

	files, err := repo.LsFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Path)
	}
	sort.Strings(got)
	want := append([]string(nil), names...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ls-files: %q\nwant %q", got, want)
	}

	// Change them all, rename one, and read the status and the diff.
	for _, n := range names {
		r.Write(n, "changed "+n+"\n")
	}
	st, err := repo.Status(ctx, StatusOptions{})
	if err != nil || len(st.Entries) != len(names) {
		t.Fatalf("status: %d entries, %v", len(st.Entries), err)
	}
	var statusPaths []string
	for _, en := range st.Entries {
		if en.X != ' ' || en.Y != 'M' {
			t.Errorf("entry %+v", en)
		}
		statusPaths = append(statusPaths, en.Path)
	}
	sort.Strings(statusPaths)
	if !reflect.DeepEqual(statusPaths, want) {
		t.Errorf("status paths: %q", statusPaths)
	}
	ns, err := repo.NameStatus(ctx, DiffSpec{})
	if err != nil || len(ns) != len(names) {
		t.Fatalf("name-status: %d, %v", len(ns), err)
	}
	r.Git("mv", "my file.txt", "moved\nname ☃.txt")
	st, err = repo.Status(ctx, StatusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	foundRename := false
	for _, en := range st.Entries {
		if en.X == 'R' && en.Path == "moved\nname ☃.txt" && en.OrigPath == "my file.txt" {
			foundRename = true
		}
	}
	if !foundRename {
		t.Errorf("no rename in %+v", st.Entries)
	}

	// Blame and the blobs.
	if c, err := repo.Blame(ctx, BlameOptions{Path: "new\nline.txt", Rev: "HEAD"}); err != nil || len(c) != 1 || c[0].Ranges[0] != (LineRange{1, 2}) {
		t.Errorf("blame: %+v, %v", c, err)
	}
	if b, err := repo.ReadBlob(ctx, "HEAD:tab\there.txt"); err != nil || string(b) != "content of tab\there.txt\n" {
		t.Errorf("blob: %q, %v", b, err)
	}
	// Ignore rules for the odd names.
	r.Write(".gitignore", "tab*\nbad*\n!-dash.txt\n*.log\n")
	r.Write("x\ny.log", "log\n")
	r.Write("tab\tnew.txt", "untracked\n")
	r.Write("bad\xffnew.txt", "untracked\n")
	ignored, err := repo.CheckIgnore(ctx, []string{"tab\tnew.txt", "bad\xffnew.txt", "-dash.txt", "my file.txt", "x\ny.log"})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(ignored)
	if !reflect.DeepEqual(ignored, []string{"bad\xffnew.txt", "tab\tnew.txt", "x\ny.log"}) {
		t.Errorf("ignored = %q", ignored)
	}
}

func TestCheckIgnore(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	r.Write(".gitignore", "*.log\nbuild/\n!keep.log\n")
	r.Write("a.txt", "a\n")
	r.Commit("base")
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	got, err := repo.CheckIgnore(ctx, []string{"a.txt", "x.log", "keep.log", "build/out.o", "src/y.log"})
	if err != nil || !reflect.DeepEqual(got, []string{"x.log", "build/out.o", "src/y.log"}) {
		t.Errorf("ignored = %q, %v", got, err)
	}
	none, err := repo.CheckIgnore(ctx, []string{"a.txt"})
	if err != nil || len(none) != 0 {
		t.Errorf("none = %q, %v", none, err)
	}
	if got, err := repo.CheckIgnore(ctx, nil); err != nil || got != nil {
		t.Errorf("no paths = %q, %v", got, err)
	}
	_, err = repo.CheckIgnore(ctx, []string{"../outside.txt"})
	if err == nil {
		t.Error("a path outside the work tree")
	}
}

func TestInitAndCloneForTheHarness(t *testing.T) {
	e := gittest.NewEnv(t)
	runner := NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: e.Vars()})
	ctx := context.Background()
	dir := filepath.Join(e.Work, "fresh")
	if err := runner.Init(ctx, dir, InitOptions{DefaultBranch: "trunk"}); err != nil {
		t.Fatal(err)
	}
	e.Trust(dir)
	if got := e.Git(dir, "symbolic-ref", "--short", "HEAD"); got != "trunk" {
		t.Errorf("branch = %q", got)
	}
	bare := filepath.Join(e.Work, "bare.git")
	if err := runner.Init(ctx, bare, InitOptions{Bare: true}); err != nil {
		t.Fatal(err)
	}
	if got := e.Git(bare, "rev-parse", "--is-bare-repository"); got != "true" {
		t.Errorf("bare = %q", got)
	}

	src := e.Init("src")
	src.Write("a.txt", "a\n")
	src.Commit("one")
	sub := e.Init("lib")
	sub.Write("lib.go", "package lib\n")
	sub.Commit("lib")
	src.Submodule("vendor/lib", sub)
	for _, tc := range []struct {
		name string
		opts CloneOptions
		path string
	}{
		{"a clone", CloneOptions{}, "a.txt"},
		{"a recursive clone", CloneOptions{Recursive: true}, "vendor/lib/lib.go"},
		{"a clone of a branch", CloneOptions{Branch: "main"}, "a.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := filepath.Join(e.Work, strings.ReplaceAll(tc.name, " ", "-"))
			if err := runner.Clone(ctx, src.Dir, target, tc.opts); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(tc.path))); err != nil {
				t.Errorf("%s: %v", tc.path, err)
			}
		})
	}
	t.Run("a bare clone", func(t *testing.T) {
		target := filepath.Join(e.Work, "bare-clone.git")
		if err := runner.Clone(ctx, src.Dir, target, CloneOptions{Bare: true}); err != nil {
			t.Fatal(err)
		}
		if got := e.Git(target, "rev-parse", "--is-bare-repository"); got != "true" {
			t.Errorf("bare = %q", got)
		}
	})
	t.Run("a clone of a repository that is not there", func(t *testing.T) {
		err := runner.Clone(ctx, filepath.Join(e.Root, "nope"), filepath.Join(e.Work, "nope-clone"), CloneOptions{})
		wantRoute(t, err, "Runner.Clone->Runner.Run->Runner.execute->classify.RepositoryNotFound")
	})
	t.Run("an init that fails", func(t *testing.T) {
		file := filepath.Join(e.Work, "a-file")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := runner.Init(ctx, filepath.Join(file, "inside"), InitOptions{})
		wantRoute(t, err, "Runner.Init->Runner.Run->Runner.execute->classify.GitCommandFailed")
	})
}

// Reads from many goroutines at the same time go through the queue and the race detector sees nothing.
func TestConcurrentReadsOfOneRepository(t *testing.T) {
	_, repo, _ := fixtureRepo(t, "history", gittest.FixtureOptions{})
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch i % 6 {
			case 0:
				_, _ = repo.Status(ctx, StatusOptions{})
			case 1:
				_, _ = repo.Log(ctx, LogOptions{MaxEntries: 20})
			case 2:
				_, _ = repo.Refs(ctx)
			case 3:
				_, _ = repo.Blame(ctx, BlameOptions{Path: "src/loader.go"})
			case 4:
				_, _ = repo.Remotes(ctx)
			case 5:
				_, _, _ = repo.AheadBehind(ctx, "main", "origin/main")
			}
		}()
	}
	wg.Wait()
	if w, a := repo.Queue.counts(); w != 0 || a != 0 {
		t.Errorf("queue: %d waiting, %d running", w, a)
	}
}

// After the tests of the package no git process is left (CLAUDE.md "Don't leave processes running").
func TestNoGitProcessOutlivesTheCommands(t *testing.T) {
	_, repo, _ := fixtureRepo(t, "many", gittest.FixtureOptions{Files: 2000})
	if _, err := repo.Status(context.Background(), StatusOptions{Limit: 100}); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("pgrep", "-af", "git status").Output()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.Contains(line, repo.Root) {
			t.Errorf("a git status of this test is still running: %s", line)
		}
	}
}

// A git that prints something unexpected makes the reads that count fail with GitOutputUnexpected.
func TestReadsThatCountRefuseUnexpectedOutput(t *testing.T) {
	r := fakeRunner(t, "stdout \"not a number\\n\"", nil)
	repo := &Repo{Runner: r, Queue: NewQueue(0), Root: t.TempDir()}
	ctx := context.Background()
	_, err := repo.RevListCount(ctx, "HEAD")
	wantRoute(t, err, "Repo.RevListCount.GitOutputUnexpected")
	_, _, err = repo.AheadBehind(ctx, "a", "b")
	wantRoute(t, err, "Repo.AheadBehind.GitOutputUnexpected")
}

func TestConfigValueReadsTheSettingAsGitSeesIt(t *testing.T) {
	_, repo, _ := fixtureRepo(t, "small", gittest.FixtureOptions{})
	ctx := context.Background()
	tests := []struct {
		key, want string
		found     bool
	}{
		{"user.name", gittest.AuthorName, true},
		{"user.email", gittest.AuthorEmail, true},
		{"no.such.key", "", false},
	}
	for _, tc := range tests {
		got, found, err := repo.ConfigValue(ctx, tc.key)
		if err != nil || got != tc.want || found != tc.found {
			t.Errorf("%s: %q, %v, %v; want %q, %v", tc.key, got, found, err, tc.want, tc.found)
		}
	}
}

// TestBlameOfAFileOfOneCommit is the assertion of VS Code's smoke test "blame2": a tracked file that one commit made has one blame entry,
// and its subject is the message of the commit. The same file blamed at a revision names the lines of that revision, and a file with
// uncommitted lines (the work tree, no revision) has the commit that does not exist for them.
func TestBlameOfAFileOfOneCommit(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	r.Write("a.txt", "one\ntwo\n")
	r.Commit("Add a.txt")
	repo := open(t, e, r.Dir)
	ctx := context.Background()
	got, err := repo.Blame(ctx, BlameOptions{Path: "a.txt", Rev: "HEAD"})
	if err != nil || len(got) != 1 || got[0].Summary != "Add a.txt" || len(got[0].Ranges) != 1 || got[0].Ranges[0] != (LineRange{1, 2}) {
		t.Fatalf("blame %+v, %v", got, err)
	}
	r.Write("a.txt", "one\nchanged\n")
	got, err = repo.Blame(ctx, BlameOptions{Path: "a.txt"})
	if err != nil || len(got) != 2 {
		t.Fatalf("blame of the work tree %+v, %v", got, err)
	}
	var notCommitted bool
	for _, c := range got {
		notCommitted = notCommitted || c.Hash == "0000000000000000000000000000000000000000"
	}
	if !notCommitted {
		t.Errorf("no entry for the uncommitted line: %+v", got)
	}
	// the revision wins over the work tree
	got, err = repo.Blame(ctx, BlameOptions{Path: "a.txt", Rev: "HEAD"})
	if err != nil || len(got) != 1 {
		t.Errorf("blame at HEAD of a changed file: %+v, %v", got, err)
	}
}
