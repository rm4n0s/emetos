package gitscm

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// Stage and unstage against real repositories (step 24.8): each command runs through the registry of the fake API, the way a menu or a
// key runs it, and the test looks at what git says afterwards.

// idle waits until the commands of the extension in the repository are done and its status is the confirming one.
func (r *rig) idle(rp *repository) {
	r.t.Helper()
	r.api.WaitFor("the commands and the status", func() bool { return rp.own == 0 && !rp.refreshing && !rp.again })
}

// run runs a command and waits for the repository to be idle.
func (r *rig) run(rp *repository, cmd string, args any) {
	r.t.Helper()
	if err := r.api.Run(cmd, args); err != nil {
		r.t.Fatalf("%s: %v", cmd, err)
	}
	r.idle(rp)
}

// porcelain lists "git status --porcelain" of a repository, sorted, with the untracked files one by one.
func porcelain(repo *gittest.Repo) []string {
	out := repo.Git("status", "--porcelain", "-z", "-uall")
	var lines []string
	toks := strings.Split(out, "\x00")
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		if len(tok) < 4 {
			continue
		}
		lines = append(lines, tok)
		if tok[0] == 'R' || tok[0] == 'C' || tok[1] == 'R' || tok[1] == 'C' {
			i++
		}
	}
	sort.Strings(lines)
	return lines
}

// groupArg is the argument a menu gives a command for a group.
func groupArg(r *repository, group string) map[string]any {
	return map[string]any{extension.SCMArgSourceControl: r.handle, extension.SCMArgGroup: group}
}

// sourceArg is the argument a menu gives a command for a repository.
func sourceArg(r *repository) map[string]any {
	return map[string]any{extension.SCMArgSourceControl: r.handle}
}

// resourceArgs are the arguments of the resources with these paths in a group.
func resourceArgs(r *repository, group string, rels ...string) []any {
	var out []any
	for _, rel := range rels {
		out = append(out, resourceArg(r, group, rel))
	}
	return out
}

// stageRepo builds a repository with changes of every kind in the work tree and the index.
func stageRepo(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	r := e.Init("stage")
	for _, f := range []string{"a.txt", "b.txt", "c.txt", "del.txt", "dir/x.txt", "dir/y.txt", "dir/z.txt", "sp ace/ünï.txt", "a[1].txt", "old.txt", "keep.txt"} {
		r.Write(f, f+"\none\ntwo\nthree\nfour\n")
	}
	r.Commit("Add the files")
	r.Write("a.txt", "a.txt\nchanged\n")
	r.Write("b.txt", "b.txt\nstaged\n")
	r.Git("add", "b.txt")
	r.Write("c.txt", "c.txt\nstaged\n")
	r.Git("add", "c.txt")
	r.Write("c.txt", "c.txt\nstaged and more\n")
	r.Remove("del.txt")
	r.Write("dir/x.txt", "changed x\n")
	r.Write("dir/y.txt", "changed y\n")
	r.Write("sp ace/ünï.txt", "changed\n")
	r.Write("a[1].txt", "changed\n")
	r.Write("u1.txt", "u\n")
	r.Write("newdir/u2.txt", "u\n")
	r.Write("intent.txt", "i\n")
	r.Git("add", "-N", "intent.txt")
	return r
}

// stageRig activates the extension over a stageRepo.
func stageRig(t *testing.T, o rigOptions) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := stageRepo(t, e)
	o.folder = repo.Dir
	r := newRig(t, e, o)
	r.settle()
	return r, repo, r.repo(repo.Dir)
}

func TestStageCommands(t *testing.T) {
	const initial = " A intent.txt| D del.txt| M a.txt| M a[1].txt| M dir/x.txt| M dir/y.txt| M sp ace/ünï.txt|?? newdir/u2.txt|?? u1.txt|M  b.txt|MM c.txt"
	tests := []struct {
		name string
		cmd  string
		args func(rp *repository) any
		want string // the lines of git status that change, " | " apart
	}{
		{"one modified file", "git.stage", func(rp *repository) any { return resourceArgs(rp, groupWorking, "a.txt") }, "M  a.txt"},
		{"a selection of several", "git.stage", func(rp *repository) any {
			return resourceArgs(rp, groupWorking, "a.txt", "dir/x.txt", "u1.txt", "del.txt")
		}, "M  a.txt|M  dir/x.txt|A  u1.txt|D  del.txt"},
		{"the files below a folder", "git.stage", func(rp *repository) any { return resourceArgs(rp, groupWorking, "dir/x.txt", "dir/y.txt") }, "M  dir/x.txt|M  dir/y.txt"},
		{"a deleted file", "git.stage", func(rp *repository) any { return resourceArgs(rp, groupWorking, "del.txt") }, "D  del.txt"},
		{"an untracked file in a folder", "git.stage", func(rp *repository) any { return resourceArgs(rp, groupWorking, "newdir/u2.txt") }, "A  newdir/u2.txt"},
		{"a path with spaces and non-ASCII characters", "git.stage", func(rp *repository) any { return resourceArgs(rp, groupWorking, "sp ace/ünï.txt") }, "M  sp ace/ünï.txt"},
		{"a path with glob characters is that file only", "git.stage", func(rp *repository) any { return resourceArgs(rp, groupWorking, "a[1].txt") }, "M  a[1].txt"},
		{"an intent to add file", "git.stage", func(rp *repository) any { return resourceArgs(rp, groupWorking, "intent.txt") }, "A  intent.txt"},
		{"a file with staged and unstaged changes", "git.stage", func(rp *repository) any { return resourceArgs(rp, groupWorking, "c.txt") }, "M  c.txt"},
		{"a staged file is left alone", "git.stage", func(rp *repository) any { return resourceArgs(rp, groupIndex, "b.txt") }, ""},
		{"stage all", "git.stageAll", func(rp *repository) any { return []any{sourceArg(rp)} }, "M  a.txt|M  a[1].txt|M  c.txt|M  dir/x.txt|M  dir/y.txt|M  sp ace/ünï.txt|A  intent.txt|A  newdir/u2.txt|A  u1.txt|D  del.txt"},
		{"stage all from the menu of the group", "git.stageAll", func(rp *repository) any { return []any{groupArg(rp, groupWorking)} }, "M  a.txt|M  a[1].txt|M  c.txt|M  dir/x.txt|M  dir/y.txt|M  sp ace/ünï.txt|A  intent.txt|A  newdir/u2.txt|A  u1.txt|D  del.txt"},
		{"stage all tracked", "git.stageAllTracked", func(rp *repository) any { return []any{sourceArg(rp)} }, "M  a.txt|M  a[1].txt|M  c.txt|M  dir/x.txt|M  dir/y.txt|M  sp ace/ünï.txt|A  intent.txt|D  del.txt"},
		{"stage all untracked", "git.stageAllUntracked", func(rp *repository) any { return []any{sourceArg(rp)} }, "A  newdir/u2.txt|A  u1.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := stageRig(t, rigOptions{})
			if got := strings.Join(porcelain(repo), "|"); got != initial {
				t.Fatalf("the repository starts as\n%s\nwant\n%s", got, initial)
			}
			r.run(rp, tc.cmd, tc.args(rp))
			got := diffLines(strings.Split(initial, "|"), porcelain(repo))
			if want := sortedJoin(strings.Split(tc.want, "|")); got != want {
				t.Errorf("new lines of the status\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// diffLines lists the lines of the new status that the old one did not have, sorted and joined with "|".
func diffLines(old, now []string) string {
	have := map[string]bool{}
	for _, l := range old {
		have[l] = true
	}
	var out []string
	for _, l := range now {
		if !have[l] {
			out = append(out, l)
		}
	}
	return sortedJoin(out)
}

func sortedJoin(lines []string) string {
	lines = append([]string(nil), lines...)
	sort.Strings(lines)
	return strings.Join(lines, "|")
}

func TestStageAllFollowsGitUntrackedChanges(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{"mixed", "A  newdir/u2.txt|A  u1.txt"},
		{"separate", "?? newdir/u2.txt|?? u1.txt"}, // git add -u: the untracked files are left
		{"hidden", ""},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			r, repo, rp := stageRig(t, rigOptions{settings: map[string]any{"git.untrackedChanges": tc.mode}})
			r.run(rp, "git.stageAll", []any{sourceArg(rp)})
			got := strings.Join(porcelain(repo), "|")
			for _, l := range strings.Split(tc.want, "|") {
				if l != "" && !strings.Contains(got, l) {
					t.Errorf("status lacks %q:\n%s", l, got)
				}
			}
			if !strings.Contains(got, "M  a.txt") || !strings.Contains(got, "D  del.txt") {
				t.Errorf("the tracked files were not staged:\n%s", got)
			}
			if tc.mode != "mixed" && strings.Contains(got, "A  u1.txt") {
				t.Errorf("an untracked file was staged:\n%s", got)
			}
		})
	}
}

func TestStageAllUntrackedAndTrackedInTheSeparateMode(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{settings: map[string]any{"git.untrackedChanges": "separate"}})
	if got := r.paths(repo.Dir, groupUntracked); !reflect.DeepEqual(got, []string{"newdir/u2.txt", "u1.txt"}) {
		t.Fatalf("untracked group = %v", got)
	}
	r.run(rp, "git.stageAllUntracked", []any{groupArg(rp, groupUntracked)})
	if got := strings.Join(porcelain(repo), "|"); !strings.Contains(got, "A  u1.txt") || !strings.Contains(got, "A  newdir/u2.txt") || !strings.Contains(got, " M a.txt") {
		t.Errorf("after stageAllUntracked:\n%s", got)
	}
	r.run(rp, "git.stageAllTracked", []any{groupArg(rp, groupWorking)})
	if got := strings.Join(porcelain(repo), "|"); strings.Contains(got, " M ") || strings.Contains(got, " D ") {
		t.Errorf("after stageAllTracked:\n%s", got)
	}
}

func TestUnstageCommands(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		args func(rp *repository) any
		want string
	}{
		{"one staged file", "git.unstage", func(rp *repository) any { return resourceArgs(rp, groupIndex, "b.txt") }, " M b.txt"},
		{"a file staged and changed again", "git.unstage", func(rp *repository) any { return resourceArgs(rp, groupIndex, "c.txt") }, " M c.txt"},
		{"a working tree file is left alone", "git.unstage", func(rp *repository) any { return resourceArgs(rp, groupWorking, "a.txt") }, ""},
		{"unstage all", "git.unstageAll", func(rp *repository) any { return []any{groupArg(rp, groupIndex)} }, " M b.txt| M c.txt|?? intent.txt"}, // an intent to add file is not kept by git reset
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := stageRig(t, rigOptions{})
			before := porcelain(repo)
			r.run(rp, tc.cmd, tc.args(rp))
			got := diffLines(before, porcelain(repo))
			// the unstaged file now shows once, in the work tree
			if got != tc.want {
				t.Errorf("changed lines %q, want %q\nstatus:\n%s", got, tc.want, strings.Join(porcelain(repo), "\n"))
			}
		})
	}
}

func TestRenamesAreStagedAndUnstagedAsPairs(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("rename")
	repo.Write("old.txt", "one\ntwo\nthree\nfour\n")
	repo.Write("keep.txt", "k\n")
	repo.Commit("base")
	if err := os.Rename(repo.Path("old.txt"), repo.Path("new.txt")); err != nil {
		t.Fatal(err)
	}
	repo.Git("add", "-N", "new.txt")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	if got := r.letters(repo.Dir, groupWorking); !reflect.DeepEqual(got, []string{"new.txt A"}) && !reflect.DeepEqual(got, []string{"new.txt R"}) {
		t.Logf("the working group shows %v", got)
	}
	arg := resourceArgs(rp, groupWorking, "new.txt")
	r.run(rp, "git.stage", arg)
	if got := strings.Join(porcelain(repo), "|"); got != "R  new.txt" {
		t.Fatalf("staged: %s", got)
	}
	r.run(rp, "git.unstage", resourceArgs(rp, groupIndex, "new.txt"))
	// both paths were unstaged: the deletion and the new file are in the work tree, nothing is staged
	if got := strings.Join(porcelain(repo), "|"); got != " D old.txt|?? new.txt" {
		t.Errorf("unstaged: %s", got)
	}
}

func TestStageAndUnstageBeforeTheFirstCommit(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("fresh")
	repo.Write("a.txt", "a\n")
	repo.Write("dir/b.txt", "b\n")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	r.run(rp, "git.stageAll", []any{sourceArg(rp)})
	if got := strings.Join(porcelain(repo), "|"); got != "A  a.txt|A  dir/b.txt" {
		t.Fatalf("staged: %s", got)
	}
	r.run(rp, "git.unstage", resourceArgs(rp, groupIndex, "a.txt"))
	if got := strings.Join(porcelain(repo), "|"); got != "?? a.txt|A  dir/b.txt" {
		t.Errorf("one unstaged: %s", got)
	}
	r.run(rp, "git.unstageAll", []any{groupArg(rp, groupIndex)})
	if got := strings.Join(porcelain(repo), "|"); got != "?? a.txt|?? dir/b.txt" {
		t.Errorf("all unstaged: %s", got)
	}
	if repo.Read("a.txt") != "a\n" {
		t.Error("the file changed")
	}
}

func TestStageTheFileOfTheEditorAndOfAURI(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{})
	file := func(rel string) string { return fileURI(repo.Path(rel)) }
	// nothing is open and nothing is given
	r.run(rp, "git.stage", nil)
	if before := len(porcelain(repo)); before != 11 {
		t.Fatalf("a stage with no file changed the status: %d lines", before)
	}
	r.api.SetActive(&extension.ActiveDocument{URI: file("a.txt")})
	r.run(rp, "git.stage", nil)
	if !strings.Contains(strings.Join(porcelain(repo), "|"), "M  a.txt") {
		t.Errorf("the file of the editor was not staged:\n%s", strings.Join(porcelain(repo), "\n"))
	}
	// the title of a file in the multi-file diff editor
	r.run(rp, "git.stageFile", file("dir/x.txt"))
	r.run(rp, "git.unstageFile", file("b.txt"))
	got := strings.Join(porcelain(repo), "|")
	if !strings.Contains(got, "M  dir/x.txt") || !strings.Contains(got, " M b.txt") {
		t.Errorf("stageFile and unstageFile:\n%s", got)
	}
	// the editor shows the staged version of a file: unstage works from the git document
	r.api.SetActive(&extension.ActiveDocument{URI: toGitURI(rp.id, rp.root, repo.Path("a.txt"), "~")})
	r.run(rp, "git.unstage", nil)
	if got := strings.Join(porcelain(repo), "|"); !strings.Contains(got, " M a.txt") {
		t.Errorf("unstage from the version of the index:\n%s", got)
	}
	// the file of a path in the arguments, relative to the folder
	r.run(rp, "git.stage", map[string]any{"path": "sp ace/ünï.txt"})
	if got := strings.Join(porcelain(repo), "|"); !strings.Contains(got, "M  sp ace/ünï.txt") {
		t.Errorf("stage by path:\n%s", got)
	}
}

func TestTheOptimisticUpdateMovesTheFilesAtOnceAndIsUndoneWhenGitFails(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{})
	// the command returns before git ran: the groups already show the result
	if err := r.api.Run("git.stage", resourceArgs(rp, groupWorking, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if got := r.paths(repo.Dir, groupIndex); !contains(got, "a.txt") {
		t.Errorf("the index group does not show the file at once: %v", got)
	}
	r.idle(rp)
	if got := r.paths(repo.Dir, groupIndex); !contains(got, "a.txt") {
		t.Errorf("the confirming status lost the file: %v", got)
	}
	// git fails: the index is locked
	lock := filepath.Join(repo.Dir, ".git", "index.lock")
	writeFile(t, lock, "")
	if err := r.api.Run("git.stage", resourceArgs(rp, groupWorking, "dir/x.txt")); err != nil {
		t.Fatal(err)
	}
	if got := r.paths(repo.Dir, groupIndex); !contains(got, "dir/x.txt") {
		t.Errorf("the optimistic update was not applied: %v", got)
	}
	r.idle(rp)
	if got := r.paths(repo.Dir, groupIndex); contains(got, "dir/x.txt") {
		t.Errorf("the optimistic update was not undone: %v", got)
	}
	if got := r.paths(repo.Dir, groupWorking); !contains(got, "dir/x.txt") {
		t.Errorf("the file is not back in Changes: %v", got)
	}
	notes := r.api.Notes()
	if len(notes) == 0 || notes[len(notes)-1].Severity != "error" || !strings.HasPrefix(notes[len(notes)-1].Message, "Git: ") {
		t.Fatalf("notes = %+v", notes)
	}
	if last := notes[len(notes)-1]; len(last.Actions) != 1 || last.Actions[0] != "Show Git Output" {
		t.Errorf("actions = %v", last.Actions)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func TestWithTheOptimisticUpdateOffTheGroupsWaitForTheStatus(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{settings: map[string]any{"git.optimisticUpdate": false}})
	if err := r.api.Run("git.stage", resourceArgs(rp, groupWorking, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if got := r.paths(repo.Dir, groupIndex); contains(got, "a.txt") {
		t.Errorf("the file moved before git was done: %v", got)
	}
	r.idle(rp)
	if got := r.paths(repo.Dir, groupIndex); !contains(got, "a.txt") {
		t.Errorf("the file did not move: %v", got)
	}
}

func TestTheDiffsOfStagedFilesCloseWithCloseDiffOnOperation(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprintf("closeDiffOnOperation %v", on), func(t *testing.T) {
			r, repo, rp := stageRig(t, rigOptions{settings: map[string]any{"git.closeDiffOnOperation": on}})
			wt := func(rel string) extension.DocumentTab {
				return extension.DocumentTab{ID: "wt:" + rel, Kind: "diff",
					Original: toGitURI(rp.id, rp.root, repo.Path(rel), "~"), Modified: fileURI(repo.Path(rel))}
			}
			idx := func(rel string) extension.DocumentTab {
				return extension.DocumentTab{ID: "idx:" + rel, Kind: "diff",
					Original: toGitURI(rp.id, rp.root, repo.Path(rel), "HEAD"), Modified: toGitURI(rp.id, rp.root, repo.Path(rel), "~")}
			}
			r.api.SetTabs(wt("a.txt"), wt("dir/x.txt"), idx("b.txt"), idx("c.txt"))
			r.run(rp, "git.stage", resourceArgs(rp, groupWorking, "a.txt"))
			wantClosed := []string(nil)
			if on {
				wantClosed = []string{"wt:a.txt"}
			}
			if got := r.api.ClosedTabs(); !reflect.DeepEqual(got, wantClosed) {
				t.Errorf("after the stage closed %v, want %v", got, wantClosed)
			}
			r.run(rp, "git.unstage", resourceArgs(rp, groupIndex, "b.txt"))
			if on {
				wantClosed = append(wantClosed, "idx:b.txt")
			}
			if got := r.api.ClosedTabs(); !reflect.DeepEqual(got, wantClosed) {
				t.Errorf("after the unstage closed %v, want %v", got, wantClosed)
			}
			r.run(rp, "git.unstageAll", []any{groupArg(rp, groupIndex)})
			if on {
				// the diffs of every staged file go, those of the work tree stay
				wantClosed = append(wantClosed, "idx:c.txt")
			}
			if got := r.api.ClosedTabs(); !reflect.DeepEqual(got, wantClosed) {
				t.Errorf("after the unstage of all closed %v, want %v", got, wantClosed)
			}
		})
	}
}

func TestFiveThousandChangesAreStagedUnstagedAndDiscardedThroughTheCommands(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("big")
	const n = 5000
	for i := 0; i < n; i++ {
		repo.Write(fmt.Sprintf("d%02d/file-%04d.txt", i%50, i), "one\n")
	}
	repo.Commit("many")
	for i := 0; i < n; i++ {
		repo.Write(fmt.Sprintf("d%02d/file-%04d.txt", i%50, i), "two\n")
	}
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	r.run(rp, "git.stageAll", []any{sourceArg(rp)})
	if got := len(r.paths(repo.Dir, groupIndex)); got != n {
		t.Fatalf("%d staged", got)
	}
	r.run(rp, "git.unstageAll", []any{groupArg(rp, groupIndex)})
	if got := len(r.paths(repo.Dir, groupWorking)); got != n {
		t.Fatalf("%d unstaged", got)
	}
	if err := r.api.Run("git.cleanAll", []any{groupArg(rp, groupWorking)}); err != nil {
		t.Fatal(err)
	}
	dialogs := r.api.OpenDialogs()
	if len(dialogs) != 1 || !strings.Contains(dialogs[0].Spec().Message, "5000 files") {
		t.Fatalf("dialogs = %+v", dialogs)
	}
	dialogs[0].Press("Discard All 5000 Files")
	r.idle(rp)
	if got := porcelain(repo); len(got) != 0 {
		t.Errorf("%d changes are left", len(got))
	}
}

func TestMergeChangesAreStagedAfterTheQuestionsAboutConflicts(t *testing.T) {
	setup := func(t *testing.T) (*rig, *gittest.Repo, *repository) {
		e := gittest.NewEnv(t)
		repo := e.Init("merge")
		repo.Write("both.txt", "base\n")
		repo.Write("marked.txt", "base\n")
		repo.Write("gone.txt", "base\n")
		repo.Write("gone2.txt", "base\n")
		repo.Commit("base")
		repo.Branch("other", "HEAD")
		repo.Write("both.txt", "main\n")
		repo.Write("marked.txt", "main\n")
		repo.Write("gone.txt", "main\n")
		repo.Git("rm", "-q", "gone2.txt")
		repo.Commit("main")
		repo.Checkout("other")
		repo.Write("both.txt", "other\n")
		repo.Write("marked.txt", "other\n")
		repo.Git("rm", "-q", "gone.txt")
		repo.Write("gone2.txt", "other\n")
		repo.Commit("other")
		repo.Checkout("main")
		if err := repo.Merge("other"); err == nil {
			t.Fatal("the merge did not conflict")
		}
		repo.Write("both.txt", "resolved\n") // no markers left
		r := newRig(t, e, rigOptions{folder: repo.Dir})
		r.settle()
		return r, repo, r.repo(repo.Dir)
	}
	t.Run("a resolved file is staged without a question", func(t *testing.T) {
		r, repo, rp := setup(t)
		r.run(rp, "git.stage", resourceArgs(rp, groupMerge, "both.txt"))
		if d := r.api.OpenDialogs(); len(d) != 0 {
			t.Fatalf("a question was asked: %q", d[0].Spec().Message)
		}
		if got := strings.Join(porcelain(repo), "|"); !strings.Contains(got, "M  both.txt") {
			t.Errorf("status:\n%s", got)
		}
	})
	t.Run("a file with markers asks, Cancel changes nothing", func(t *testing.T) {
		r, repo, rp := setup(t)
		before := porcelain(repo)
		if err := r.api.Run("git.stage", resourceArgs(rp, groupMerge, "marked.txt")); err != nil {
			t.Fatal(err)
		}
		r.api.WaitFor("the question", func() bool { return len(r.api.OpenDialogs()) == 1 })
		d := r.api.OpenDialogs()[0]
		if d.Spec().Message != "Are you sure you want to stage marked.txt with merge conflicts?" {
			t.Errorf("message %q", d.Spec().Message)
		}
		d.Cancel()
		r.idle(rp)
		if !reflect.DeepEqual(porcelain(repo), before) {
			t.Errorf("Cancel changed the status:\n%s", strings.Join(porcelain(repo), "\n"))
		}
	})
	t.Run("Yes stages it", func(t *testing.T) {
		r, repo, rp := setup(t)
		if err := r.api.Run("git.stage", resourceArgs(rp, groupMerge, "marked.txt", "both.txt")); err != nil {
			t.Fatal(err)
		}
		r.api.WaitFor("the question", func() bool { return len(r.api.OpenDialogs()) == 1 })
		d := r.api.OpenDialogs()[0]
		if d.Spec().Message != "Are you sure you want to stage marked.txt with merge conflicts?" {
			t.Errorf("only the file with markers is asked about: %q", d.Spec().Message)
		}
		d.Press("Yes")
		r.idle(rp)
		got := strings.Join(porcelain(repo), "|")
		if !strings.Contains(got, "M  marked.txt") || !strings.Contains(got, "M  both.txt") {
			t.Errorf("status:\n%s", got)
		}
	})
	t.Run("a file deleted by them and modified by us", func(t *testing.T) {
		for _, tc := range []struct{ button, want string }{{"Keep Our Version", ""}, {"Delete File", "D  gone.txt"}} {
			t.Run(tc.button, func(t *testing.T) {
				r, repo, rp := setup(t)
				if err := r.api.Run("git.stage", resourceArgs(rp, groupMerge, "gone.txt")); err != nil {
					t.Fatal(err)
				}
				r.api.WaitFor("the question", func() bool { return len(r.api.OpenDialogs()) == 1 })
				d := r.api.OpenDialogs()[0]
				if d.Spec().Message != "File \"gone.txt\" was deleted by them and modified by us." {
					t.Errorf("message %q", d.Spec().Message)
				}
				d.Press(tc.button)
				r.idle(rp)
				got := strings.Join(porcelain(repo), "|")
				if strings.Contains(got, "gone.txt") != (tc.want != "") || (tc.want != "" && !strings.Contains(got, tc.want)) {
					t.Errorf("status:\n%s\nwant %q", got, tc.want)
				}
				if strings.Contains(got, "UD gone.txt") {
					t.Errorf("the conflict is still there: %s", got)
				}
			})
		}
	})
	t.Run("a file deleted by us and modified by them", func(t *testing.T) {
		r, repo, rp := setup(t)
		if err := r.api.Run("git.stage", resourceArgs(rp, groupMerge, "gone2.txt")); err != nil {
			t.Fatal(err)
		}
		r.api.WaitFor("the question", func() bool { return len(r.api.OpenDialogs()) == 1 })
		d := r.api.OpenDialogs()[0]
		if d.Spec().Message != "File \"gone2.txt\" was deleted by us and modified by them." {
			t.Errorf("message %q", d.Spec().Message)
		}
		before := porcelain(repo)
		d.Cancel()
		r.idle(rp)
		if !reflect.DeepEqual(porcelain(repo), before) {
			t.Errorf("Cancel changed the status")
		}
	})
	t.Run("stage all merge changes", func(t *testing.T) {
		r, repo, rp := setup(t)
		if err := r.api.Run("git.stageAllMerge", []any{groupArg(rp, groupMerge)}); err != nil {
			t.Fatal(err)
		}
		// the questions come one after the other: the deletions first, then the files with markers
		for i := 0; i < 3; i++ {
			r.api.WaitFor("a question", func() bool { return len(r.api.OpenDialogs()) > 0 })
			d := r.api.OpenDialogs()[0]
			msg := d.Spec().Message
			switch {
			case strings.Contains(msg, "deleted by"):
				d.Press(d.Spec().Buttons[0].Label)
			default:
				d.Press("Yes")
			}
			r.api.Pump()
			if len(r.api.OpenDialogs()) == 0 {
				break
			}
		}
		r.idle(rp)
		if got := r.paths(repo.Dir, groupMerge); len(got) != 0 {
			t.Errorf("merge changes left: %v (status:\n%s)", got, strings.Join(porcelain(repo), "\n"))
		}
	})
}

func TestCommandsOfAHostWithoutDialogsAskNothingAndLogNothing(t *testing.T) {
	// the fake always has dialogs; the commands that need none never ask
	r, repo, rp := stageRig(t, rigOptions{})
	r.run(rp, "git.stage", resourceArgs(rp, groupWorking, "a.txt"))
	if d := r.api.OpenDialogs(); len(d) != 0 {
		t.Errorf("a stage asked %q", d[0].Spec().Message)
	}
	_ = repo
}
