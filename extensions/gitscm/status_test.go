package gitscm

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// statesRepo builds a repository with a file in every state of git status that VS Code's groups tell apart.
func statesRepo(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	r := e.Init("states")
	for _, f := range []string{"staged_mod", "unstaged_mod", "both_mod", "add_then_mod", "del_unstaged", "del_staged", "renamed_old", "rename_mod_old", "typechange", "keep"} {
		r.Write(f+".txt", f+"\nline two\nline three\nline four\nline five\n")
	}
	r.Git("add", "-A")
	r.Commit("Add the files")
	r.Write("staged_mod.txt", "staged_mod\nline two changed\nline three\nline four\nline five\n")
	r.Git("add", "staged_mod.txt")
	r.Write("unstaged_mod.txt", "unstaged_mod\nline two changed\nline three\nline four\nline five\n")
	r.Write("both_mod.txt", "both_mod\nline two changed\nline three\nline four\nline five\n")
	r.Git("add", "both_mod.txt")
	r.Write("both_mod.txt", "both_mod\nline two changed\nline three changed\nline four\nline five\n")
	r.Write("staged_add.txt", "new\n")
	r.Git("add", "staged_add.txt")
	r.Write("add_then_mod.txt2", "x")
	r.Write("add_then_mod2.txt", "new\n")
	r.Git("add", "add_then_mod2.txt")
	r.Write("add_then_mod2.txt", "new\nchanged\n")
	r.Write("intent.txt", "intent\n")
	r.Git("add", "-N", "intent.txt")
	r.Remove("del_unstaged.txt")
	r.Git("rm", "-q", "del_staged.txt")
	r.Git("mv", "renamed_old.txt", "renamed_new.txt")
	r.Git("mv", "rename_mod_old.txt", "rename_mod_new.txt")
	r.Write("rename_mod_new.txt", "rename_mod\nline two\nline three\nline four\nline five\nmore\n")
	if err := os.Remove(r.Path("typechange.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("keep.txt", r.Path("typechange.txt")); err != nil {
		t.Fatal(err)
	}
	r.Write("untracked.txt", "u\n")
	if err := os.Remove(r.Path("add_then_mod.txt2")); err != nil {
		t.Fatal(err)
	}
	return r
}

// unmerge puts a file of the index in an unmerged state by its stages (1 base, 2 ours, 3 theirs), as a merge would leave it.
func unmerge(t *testing.T, r *gittest.Repo, path string, stages ...int) {
	t.Helper()
	r.Git("update-index", "--force-remove", path)
	var lines []string
	for _, s := range stages {
		blob, _, err := r.Env.GitInput(r.Dir, path+" stage "+string(rune('0'+s))+"\n", "hash-object", "-w", "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		_ = blob
	}
	for _, s := range stages {
		out, _, err := r.Env.GitInput(r.Dir, path+" stage "+string(rune('0'+s))+"\n", "hash-object", "-w", "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, "100644 "+strings.TrimSpace(out)+" "+string(rune('0'+s))+"\t"+path)
	}
	if _, _, err := r.Env.GitInput(r.Dir, strings.Join(lines, "\n")+"\n", "update-index", "--index-info"); err != nil {
		t.Fatal(err)
	}
}

func TestStatusFillsTheGroupsWithTheLettersOfVSCode(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := statesRepo(t, e)
	// the seven forms of an unmerged path
	for path, stages := range map[string][]int{"dd.txt": {1}, "au.txt": {2}, "ud.txt": {1, 2}, "ua.txt": {3}, "du.txt": {1, 3}, "aa.txt": {2, 3}, "uu.txt": {1, 2, 3}} {
		unmerge(t, repo, path, stages...)
	}
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	root := repo.Dir
	tests := []struct {
		group string
		want  []string
	}{
		{groupMerge, []string{"aa.txt !", "au.txt !", "dd.txt !", "du.txt !", "ua.txt !", "ud.txt !", "uu.txt !"}},
		{groupIndex, []string{"add_then_mod2.txt A", "both_mod.txt M", "del_staged.txt D", "rename_mod_new.txt R", "renamed_new.txt R", "staged_add.txt A", "staged_mod.txt M"}},
		{groupWorking, []string{"add_then_mod2.txt M", "both_mod.txt M", "del_unstaged.txt D", "intent.txt A", "rename_mod_new.txt M", "typechange.txt T", "unstaged_mod.txt M", "untracked.txt U"}},
		{groupUntracked, nil},
	}
	for _, tc := range tests {
		if got := r.letters(root, tc.group); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %v\nwant %v", tc.group, got, tc.want)
		}
	}
	// tooltips, colors and the strike-through of a deleted file
	g := r.source(root).Group(groupIndex)
	for _, res := range g.Resources() {
		switch filepath.Base(res.URI) {
		case "del_staged.txt":
			d := res.Decorations
			if !d.StrikeThrough || d.Tooltip != "Index Deleted" || d.Color != "gitDecoration.stageDeletedResourceForeground" {
				t.Errorf("del_staged.txt: %+v", d)
			}
		case "staged_mod.txt":
			if d := res.Decorations; d.StrikeThrough || d.Tooltip != "Index Modified" || d.Color != "gitDecoration.stageModifiedResourceForeground" {
				t.Errorf("staged_mod.txt: %+v", d)
			}
		}
	}
	// the groups, their labels and whether they hide when empty, as VS Code makes them
	src := r.source(root)
	var ids, labels []string
	for _, grp := range src.Groups() {
		ids = append(ids, grp.ID())
		labels = append(labels, grp.Label())
	}
	if !reflect.DeepEqual(ids, []string{"merge", "index", "workingTree", "untracked"}) || !reflect.DeepEqual(labels, []string{"Merge Changes", "Staged Changes", "Changes", "Untracked Changes"}) {
		t.Errorf("groups %v %v", ids, labels)
	}
	if src.Count() != 7+7+8 {
		t.Errorf("count = %d", src.Count())
	}
}

func TestUntrackedChangesModes(t *testing.T) {
	tests := []struct {
		mode               string
		working, untracked []string
		count              int
	}{
		{"mixed", []string{"mod.txt M", "new.txt U"}, nil, 2},
		{"separate", []string{"mod.txt M"}, []string{"new.txt U"}, 2},
		{"hidden", []string{"mod.txt M"}, nil, 1},
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := e.Init("repo")
			repo.Write("mod.txt", "a\n")
			repo.Commit("Add mod")
			repo.Write("mod.txt", "b\n")
			repo.Write("new.txt", "n\n")
			r := newRig(t, e, rigOptions{folder: repo.Dir, settings: map[string]any{"git.untrackedChanges": tc.mode}})
			r.settle()
			if got := r.letters(repo.Dir, groupWorking); !reflect.DeepEqual(got, tc.working) {
				t.Errorf("working %v want %v", got, tc.working)
			}
			if got := r.letters(repo.Dir, groupUntracked); !reflect.DeepEqual(got, tc.untracked) {
				t.Errorf("untracked %v want %v", got, tc.untracked)
			}
			if c := r.source(repo.Dir).Count(); c != tc.count {
				t.Errorf("count %d want %d", c, tc.count)
			}
		})
	}
}

func TestCountBadgeModes(t *testing.T) {
	for mode, want := range map[string]int{"all": 3, "tracked": 1, "off": 0} {
		t.Run(mode, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := e.Init("repo")
			repo.Write("mod.txt", "a\n")
			repo.Commit("Add mod")
			repo.Write("mod.txt", "b\n")
			repo.Write("n1.txt", "n\n")
			repo.Write("n2.txt", "n\n")
			r := newRig(t, e, rigOptions{folder: repo.Dir, settings: map[string]any{"git.countBadge": mode}})
			r.settle()
			if c := r.source(repo.Dir).Count(); c != want {
				t.Errorf("count %d want %d", c, want)
			}
		})
	}
}

func TestRenamesAndTheSimilarityThreshold(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("repo")
	body := strings.Repeat("line of the file\n", 20)
	repo.Write("old.txt", body)
	repo.Commit("Add old")
	repo.Git("mv", "old.txt", "new.txt")
	repo.Write("new.txt", body+"one more line\n")
	repo.Git("add", "new.txt")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	if got := r.letters(repo.Dir, groupIndex); !reflect.DeepEqual(got, []string{"new.txt R"}) {
		t.Errorf("index %v", got)
	}
	// with a threshold of 100 % the rename is a delete and an add
	r.api.SetSetting("git.similarityThreshold", 100)
	r.api.WaitFor("the status with the new threshold", func() bool {
		r.api.Pump()
		return reflect.DeepEqual(r.letters(repo.Dir, groupIndex), []string{"new.txt A", "old.txt D"})
	})
}
