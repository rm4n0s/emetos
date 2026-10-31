package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// subjects are the first lines of the messages of the commits, newest first.
func subjects(commits []Commit) []string {
	out := make([]string, len(commits))
	for i, c := range commits {
		out[i], _, _ = strings.Cut(c.Message, "\n")
	}
	return out
}

// openVersioned is open with the version of the git program, which LogFile reads to ask for the stat of merge commits (the helper of the
// other tests leaves it out).
func openVersioned(t *testing.T, e *gittest.Env, dir string) *Repo {
	t.Helper()
	v, err := ParseVersion(e.Git(dir, "--version"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := Open(context.Background(), NewRunner(Config{Trust: trustAll, Path: e.GitPath, Env: e.Vars(), Version: v}), nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLogFileFollowsRenamesAndPagesAcrossTheBoundary(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("history")
	ctx := context.Background()
	body := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\n"
	r.Write("old.txt", body)
	r.Write("other.txt", "unrelated\n")
	first := r.Commit("add old.txt")
	r.Write("old.txt", strings.Replace(body, "line 2", "line two", 1))
	r.Commit("edit old.txt", "old.txt")
	r.Write("other.txt", "unrelated again\n")
	r.Commit("edit other.txt", "other.txt")
	r.Git("mv", "old.txt", "dir-new.txt")
	rename := r.Commit("rename old.txt to dir-new.txt")
	r.Write("dir-new.txt", strings.Replace(body, "line 2", "line two", 1)+"line 9\n")
	r.Commit("edit dir-new.txt", "dir-new.txt")
	repo := openVersioned(t, e, r.Dir)

	all, err := repo.LogFile(ctx, LogFileOptions{Path: "dir-new.txt", Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"edit dir-new.txt", "rename old.txt to dir-new.txt", "edit old.txt", "add old.txt"}
	if got := subjects(all); !equalStrings(got, want) {
		t.Fatalf("history = %v, want %v", got, want)
	}
	// the path the file had in each commit, which is what a diff of that commit needs
	paths := []string{all[0].FilePath, all[1].FilePath, all[2].FilePath, all[3].FilePath}
	if wantPaths := []string{"dir-new.txt", "dir-new.txt", "old.txt", "old.txt"}; !equalStrings(paths, wantPaths) {
		t.Errorf("paths = %v, want %v", paths, wantPaths)
	}
	if all[1].Hash != rename || all[3].Hash != first {
		t.Errorf("hashes = %s %s", all[1].Hash, all[3].Hash)
	}
	// without --follow the history stops at the rename
	plain, err := repo.LogFile(ctx, LogFileOptions{Path: "dir-new.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := subjects(plain); !equalStrings(got, want[:2]) {
		t.Errorf("history without Follow = %v", got)
	}
	// the first page asks for one more than it shows; the next page starts at that commit with the path it had there
	page, err := repo.LogFile(ctx, LogFileOptions{Path: "dir-new.txt", Follow: true, MaxEntries: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := subjects(page); !equalStrings(got, want[:3]) {
		t.Fatalf("first page = %v", got)
	}
	cursor := page[2]
	next, err := repo.LogFile(ctx, LogFileOptions{Path: cursor.FilePath, Hash: cursor.Hash, Follow: true, MaxEntries: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := subjects(next); !equalStrings(got, want[2:]) {
		t.Errorf("the page after the rename boundary = %v, want %v", got, want[2:])
	}
	// the new name of the file at the old commit is not there, which is why the cursor carries the path
	lost, err := repo.LogFile(ctx, LogFileOptions{Path: "dir-new.txt", Hash: cursor.Hash, Follow: true, MaxEntries: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(lost) != 0 {
		t.Errorf("a log from before the rename with the new name = %v", subjects(lost))
	}
}

func TestLogFileOfAMergeAnUntrackedFileAndAnEmptyRepository(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	ctx := context.Background()
	repo := openVersioned(t, e, r.Dir)
	got, err := repo.LogFile(ctx, LogFileOptions{Path: "a.txt", Follow: true})
	if err != nil || len(got) != 0 {
		t.Fatalf("a repository with no commit: %v, %v", got, err)
	}
	r.Write("a.txt", "one\ntwo\nthree\n")
	r.Commit("add a.txt")
	trunk := strings.TrimSpace(r.Git("rev-parse", "--abbrev-ref", "HEAD"))
	r.Branch("side", "")
	r.Checkout("side")
	r.Write("a.txt", "one\ntwo\nthree\nside\n")
	r.Commit("side edits a.txt", "a.txt")
	r.Checkout(trunk)
	r.Write("a.txt", "main\none\ntwo\nthree\n")
	r.Commit("main edits a.txt", "a.txt")
	if err := r.Merge("side"); err != nil {
		// both sides changed the file: resolve by taking both
		r.Write("a.txt", "main\none\ntwo\nthree\nside\n")
		r.Git("add", "a.txt")
		r.Git("commit", "-q", "--no-edit")
	}
	r.Write("untracked.txt", "x\n")
	// with --diff-merges=first-parent (git 2.31) a log that follows renames lists the merge commit too
	got, err = repo.LogFile(ctx, LogFileOptions{Path: "a.txt", Follow: true, ShortStats: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := subjects(got); len(s) != 4 || !strings.HasPrefix(s[0], "Merge") || !equalStrings(s[1:], []string{"main edits a.txt", "side edits a.txt", "add a.txt"}) {
		t.Fatalf("history that follows = %v", s)
	}
	// the merge is there with the path of the file and its stat against the first parent
	got, err = repo.LogFile(ctx, LogFileOptions{Path: "a.txt", ShortStats: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := subjects(got); len(s) != 4 || !strings.HasPrefix(s[0], "Merge") {
		t.Fatalf("history = %v", s)
	}
	if len(got[0].Parents) != 2 || got[0].FilePath != "a.txt" {
		t.Errorf("the merge has %d parents and the path %q", len(got[0].Parents), got[0].FilePath)
	}
	if got[0].ShortStat == nil || got[0].ShortStat.Files != 1 {
		t.Errorf("the stat of the merge = %+v", got[0].ShortStat)
	}
	got, err = repo.LogFile(ctx, LogFileOptions{Path: "untracked.txt", Follow: true})
	if err != nil || len(got) != 0 {
		t.Errorf("an untracked file: %v, %v", got, err)
	}
	got, err = repo.LogFile(ctx, LogFileOptions{Path: r.Path("a.txt"), MaxEntries: 1})
	if err != nil || len(got) != 1 {
		t.Errorf("an absolute path: %v, %v", got, err)
	}
}

func TestLogFileTakesAPathAsItIsAndRefusesWhatItCannotUse(t *testing.T) {
	e := gittest.NewEnv(t)
	r := e.Init("repo")
	ctx := context.Background()
	r.Write("a[1].txt", "bracket\n")
	r.Write("a1.txt", "one\n")
	r.Commit("add both")
	r.Write("a[1].txt", "bracket two\n")
	r.Commit("edit the bracket file", "a[1].txt")
	repo := openVersioned(t, e, r.Dir)
	got, err := repo.LogFile(ctx, LogFileOptions{Path: "a[1].txt", Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	if s := subjects(got); !equalStrings(s, []string{"edit the bracket file", "add both"}) {
		t.Errorf("a[1].txt is a pattern: %v", s)
	}
	out := filepath.Join(t.TempDir(), "out")
	_, err = repo.LogFile(ctx, LogFileOptions{Path: "a.txt", Hash: "--output=" + out})
	wantRoute(t, err, "Repo.LogFile.GitLogFileRevisionInvalid")
	_, err = repo.LogFile(ctx, LogFileOptions{Path: "../outside.txt"})
	wantRoute(t, err, "GitPathOutsideRepository")
	if _, err := os.Stat(out); err == nil {
		t.Error("an option was run")
	}
}
