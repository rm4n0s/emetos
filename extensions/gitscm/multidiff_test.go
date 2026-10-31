package gitscm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// The multi-file diff editor of the changes of a group and of a commit, against real repositories (step 24.17).

// multiFiles asks the provider for the files of a set, as the window does.
func (r *rig) multiFiles(key string) ([]extension.MultiDiffResource, error) {
	r.t.Helper()
	return r.api.MultiDiffFiles(context.Background(), MultiDiffKind, key)
}

func (r *rig) mustMultiFiles(key string) []extension.MultiDiffResource {
	r.t.Helper()
	files, err := r.multiFiles(key)
	if err != nil {
		r.t.Fatalf("%s: %v", key, err)
	}
	return files
}

// line is a file of a set as "path letter original>modified" with the repository id and root cut out of the URIs.
func line(rp *repository, f extension.MultiDiffResource) string {
	cut := func(u string) string {
		if u == "" {
			return "-"
		}
		u = strings.TrimPrefix(u, extension.ContentURI("git", rp.id+"/"))
		return strings.TrimPrefix(u, "file://"+rp.root+"/")
	}
	return f.Path + " " + f.Letter + " " + cut(f.Original) + " > " + cut(f.Modified)
}

func lines(rp *repository, files []extension.MultiDiffResource) []string {
	var out []string
	for _, f := range files {
		out = append(out, line(rp, f))
	}
	return out
}

func equalStrings(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestTheFilesOfAGroupAreSortedWithTheirTwoDocuments(t *testing.T) {
	r, _, rp := diffRig(t)
	// the staged changes: HEAD against the index, a deletion has no modified side, an addition no original
	index := lines(rp, r.mustMultiFiles(groupKey(rp.id, groupIndex)))
	for _, want := range []string{
		"add_then_mod2.txt A - > /add_then_mod2.txt",
		"del_staged.txt D HEAD/del_staged.txt > -",
		"renamed_new.txt R HEAD/renamed_old.txt > /renamed_new.txt",
		"staged_add.txt A - > /staged_add.txt",
		"staged_mod.txt M HEAD/staged_mod.txt > /staged_mod.txt",
	} {
		if !contains(index, want) {
			t.Errorf("the staged changes lack %q:\n%s", want, strings.Join(index, "\n"))
		}
	}
	// sorted: a file before the folders at its level, names without case
	if !sortedByPath(r.mustMultiFiles(groupKey(rp.id, groupIndex))) {
		t.Errorf("the staged changes are not sorted: %v", index)
	}
	// the work tree: the index against the file, an untracked file is all added, a deleted one has no modified side
	work := lines(rp, r.mustMultiFiles(groupKey(rp.id, groupWorking)))
	for _, want := range []string{
		"unstaged_mod.txt M ~/unstaged_mod.txt > unstaged_mod.txt",
		"both_mod.txt M ~/both_mod.txt > both_mod.txt",
		"del_unstaged.txt D HEAD/del_unstaged.txt > -",
		"untracked.txt U - > untracked.txt",
	} {
		if !contains(work, want) {
			t.Errorf("the changes lack %q:\n%s", want, strings.Join(work, "\n"))
		}
	}
	// the conflicts: the file with its markers
	merge := r.mustMultiFiles(groupKey(rp.id, groupMerge))
	if len(merge) != 1 || merge[0].Path != "uu.txt" || merge[0].Letter != "!" {
		t.Errorf("merge changes %v", lines(rp, merge))
	}
	// "all" is the work tree and the untracked files, as Open All Changes of a repository opens
	all := r.mustMultiFiles(groupKey(rp.id, groupAll))
	if len(all) < len(r.mustMultiFiles(groupKey(rp.id, groupWorking))) {
		t.Errorf("all has %d files", len(all))
	}
}

func sortedByPath(files []extension.MultiDiffResource) bool {
	for i := 1; i < len(files); i++ {
		if comparePaths(files[i-1].Path, files[i].Path) > 0 {
			return false
		}
	}
	return true
}

func TestEveryFileOfAGroupCarriesWhatItsActionsNeed(t *testing.T) {
	r, _, rp := diffRig(t)
	for _, g := range []string{groupIndex, groupWorking, groupUntracked, groupAll, groupMerge} {
		for _, f := range r.mustMultiFiles(groupKey(rp.id, g)) {
			ctx := f.Context
			if ctx["scmProvider"] != "git" || ctx["scmResourceGroup"] == nil || (g != groupAll && ctx["scmResourceGroup"] != g) {
				t.Errorf("%s %s: context %v", g, f.Path, ctx)
			}
			// the arguments of the file are the arguments of a menu of the Source Control view: they find the resource
			targets := r.x.resourceTargets(f.Args)
			if len(targets) != 1 || targets[0].res.path != filepath.Join(rp.root, filepath.FromSlash(f.Path)) || targets[0].group != ctx["scmResourceGroup"] {
				t.Errorf("%s %s: the arguments %v find %v", g, f.Path, f.Args, targets)
			}
			if f.Original == "" && f.Modified == "" || f.Letter == "" || f.Color == "" || f.Tooltip == "" {
				t.Errorf("%s %s: %+v", g, f.Path, f)
			}
		}
	}
}

// The menu of the header of a file has Stage for the work tree and the untracked files and Unstage for the staged ones: the items the
// manifest declares match the context of the files the provider makes (the when-clause is `scmProvider == git && scmResourceGroup == <group>`).
func TestTheStageActionsOfTheHeaderMatchTheGroupsOfTheFiles(t *testing.T) {
	r, _, rp := diffRig(t)
	var items []extension.MenuItem
	for _, m := range r.x.Manifest().Contributes.Menus {
		if m.Menu == extension.MenuMultiDiffFileToolbar {
			items = append(items, m)
		}
	}
	commandOf := func(group string) string {
		for _, m := range items {
			if strings.Contains(m.When, "scmProvider == git") && strings.HasSuffix(m.When, "scmResourceGroup == "+group) {
				return m.Command
			}
		}
		return ""
	}
	want := map[string]string{groupWorking: "git.stageFile", groupUntracked: "git.stageFile", groupIndex: "git.unstageFile", groupMerge: ""}
	for g, cmd := range want {
		if got := commandOf(g); got != cmd {
			t.Errorf("the action of %s is %q, want %q", g, got, cmd)
		}
	}
	// a file's own group says which of them its header shows
	for _, f := range r.mustMultiFiles(groupKey(rp.id, groupAll)) {
		if f.Context["scmResourceGroup"] != groupWorking && f.Context["scmResourceGroup"] != groupUntracked {
			t.Errorf("%s is in %v", f.Path, f.Context["scmResourceGroup"])
		}
	}
}

// The action of a header runs its command with the arguments of the file the provider made: with them git.stageFile stages that file
// and git.unstageFile unstages it, checked against the index of a real repository.
func TestTheStageActionsOfTheHeaderStageAndUnstageTheirFile(t *testing.T) {
	r, repo, rp := diffRig(t)
	indexOf := func() string { return strings.TrimSpace(repo.Git("diff", "--cached", "--name-only")) }
	fileOf := func(group, rel string) extension.MultiDiffResource {
		t.Helper()
		for _, f := range r.mustMultiFiles(groupKey(rp.id, group)) {
			if f.Path == rel {
				return f
			}
		}
		t.Fatalf("%s is not in %s", rel, group)
		return extension.MultiDiffResource{}
	}
	if strings.Contains(indexOf(), "unstaged_mod.txt") {
		t.Fatalf("the file is staged before the test:\n%s", indexOf())
	}
	other := indexOf()
	// the header of a file of the work tree: Stage
	f := fileOf(groupWorking, "unstaged_mod.txt")
	r.run(rp, "git.stageFile", f.Args)
	got := indexOf()
	if !strings.Contains(got, "unstaged_mod.txt") {
		t.Fatalf("the file was not staged:\n%s", got)
	}
	if strings.Contains(got, "both_mod.txt") != strings.Contains(other, "both_mod.txt") {
		t.Errorf("another file changed in the index:\n%s", got)
	}
	// it is in the staged group now, whose header says Unstage
	f = fileOf(groupIndex, "unstaged_mod.txt")
	if f.Context["scmResourceGroup"] != groupIndex {
		t.Errorf("context %v", f.Context)
	}
	r.run(rp, "git.unstageFile", f.Args)
	if strings.Contains(indexOf(), "unstaged_mod.txt") {
		t.Errorf("the file is still staged:\n%s", indexOf())
	}
	if indexOf() != other {
		t.Errorf("the index is %q, want %q", indexOf(), other)
	}
	// an untracked file is staged by the same action
	r.run(rp, "git.stageFile", fileOf(groupWorking, "untracked.txt").Args)
	if !strings.Contains(indexOf(), "untracked.txt") {
		t.Errorf("the untracked file was not staged:\n%s", indexOf())
	}
}

func TestTheGroupCommandsOpenTheEditorOfTheirGroup(t *testing.T) {
	r, _, rp := diffRig(t)
	arg := []any{map[string]any{extension.SCMArgSourceControl: rp.handle}}
	tests := []struct {
		command, key, title string
	}{
		{"git.viewChanges", groupKey(rp.id, groupWorking), "Git: Changes"},
		{"git.viewStagedChanges", groupKey(rp.id, groupIndex), "Git: Staged Changes"},
		{"git.openAllChanges", groupKey(rp.id, groupAll), "Git: Changes"},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			before := len(r.api.OpenedMultiDiffs())
			if err := r.api.Run(tc.command, arg); err != nil {
				t.Fatal(err)
			}
			got := r.api.OpenedMultiDiffs()
			if len(got) != before+1 {
				t.Fatalf("%d editors opened", len(got)-before)
			}
			spec := got[before]
			if spec.Kind != MultiDiffKind || spec.Key != tc.key || spec.Title != tc.title || spec.Preview {
				t.Errorf("spec %+v", spec)
			}
		})
	}
	// Open All Changes of a group: the group of the argument
	if err := r.api.Run("git.openAllChanges", []any{map[string]any{extension.SCMArgSourceControl: rp.handle, extension.SCMArgGroup: groupIndex}}); err != nil {
		t.Fatal(err)
	}
	got := r.api.OpenedMultiDiffs()
	if last := got[len(got)-1]; last.Key != groupKey(rp.id, groupIndex) || last.Title != "Git: Staged Changes" {
		t.Errorf("Open All Changes of the staged group opened %+v", last)
	}
	// no diff editor was opened for any of them
	if n := len(r.api.OpenedDiffs()) + len(r.api.OpenedDocuments()); n != 0 {
		t.Errorf("%d diffs and documents were opened", n)
	}
	if n := len(r.api.OpenDialogs()); n != 0 {
		t.Errorf("%d dialogs: there is nothing to ask when one editor opens", n)
	}
}

func TestAGroupWithNoChangeOpensNothingAndSaysSo(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("clean")
	repo.Write("a.txt", "a\n")
	repo.Commit("a")
	r := newRig(t, e, rigOptions{folder: repo.Dir, settings: map[string]any{"git.untrackedChanges": "separate"}})
	r.settle()
	rp := r.repo(repo.Dir)
	arg := []any{map[string]any{extension.SCMArgSourceControl: rp.handle}}
	for command, want := range map[string]string{
		"git.viewChanges":          "The repository does not have any changes.",
		"git.viewStagedChanges":    "The repository does not have any staged changes.",
		"git.viewUntrackedChanges": "The repository does not have any untracked changes.",
	} {
		before := len(r.api.Notes())
		if err := r.api.Run(command, arg); err != nil {
			t.Fatal(err)
		}
		notes := r.api.Notes()
		if len(notes) != before+1 || notes[before].Message != want || notes[before].Severity != "info" {
			t.Errorf("%s: notes %+v", command, notes[before:])
		}
	}
	// Open All Changes of a clean repository opens nothing and says nothing
	if err := r.api.Run("git.openAllChanges", arg); err != nil {
		t.Fatal(err)
	}
	if n := len(r.api.OpenedMultiDiffs()); n != 0 {
		t.Errorf("%d editors opened", n)
	}
	// the untracked group has files when there is one
	repo.Write("new.txt", "new\n")
	r.refreshAndSettle(rp)
	if err := r.api.Run("git.viewUntrackedChanges", arg); err != nil {
		t.Fatal(err)
	}
	got := r.api.OpenedMultiDiffs()
	if len(got) != 1 || got[0].Key != groupKey(rp.id, groupUntracked) || got[0].Title != "Git: Untracked Changes" {
		t.Errorf("opened %+v", got)
	}
}

// refreshAndSettle runs a status of the repository and waits for it.
func (r *rig) refreshAndSettle(rp *repository) {
	r.t.Helper()
	rp.refresh()
	r.settle()
}

func TestTheProviderIsToldWhenAGroupChanges(t *testing.T) {
	r, repo, rp := diffRig(t)
	reg := r.api.MultiDiffProviderFor(MultiDiffKind)
	if reg == nil {
		t.Fatal("no provider")
	}
	before := len(reg.MultiDiffChanges())
	repo.Git("add", "unstaged_mod.txt")
	r.refreshAndSettle(rp)
	changes := reg.MultiDiffChanges()
	if len(changes) <= before {
		t.Fatal("the status told nothing")
	}
	last := changes[len(changes)-1]
	for _, g := range []string{groupMerge, groupIndex, groupWorking, groupUntracked, groupAll} {
		if !contains(last, groupKey(rp.id, g)) {
			t.Errorf("the keys %v lack the group %s", last, g)
		}
	}
	// the file moved from the work tree to the staged changes
	if got := lines(rp, r.mustMultiFiles(groupKey(rp.id, groupIndex))); !contains(got, "unstaged_mod.txt M HEAD/unstaged_mod.txt > /unstaged_mod.txt") {
		t.Errorf("staged %v", got)
	}
	for _, f := range r.mustMultiFiles(groupKey(rp.id, groupWorking)) {
		if f.Path == "unstaged_mod.txt" {
			t.Error("the staged file is still among the changes")
		}
	}
}

func TestAClosedRepositoryIsGoneForItsSets(t *testing.T) {
	r, repo, rp := diffRig(t)
	reg := r.api.MultiDiffProviderFor(MultiDiffKind)
	key := commitKey(rp.id, repo.Head())
	if err := r.api.Run("git.viewCommit", map[string]any{"id": "HEAD"}); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	r.settleUntil("the commit set is opened", func() bool { return len(r.api.OpenedMultiDiffs()) == 1 })
	r.x.model.close(rp)
	r.settle()
	for _, k := range []string{groupKey(rp.id, groupWorking), key} {
		_, err := r.multiFiles(k)
		appErr, ok := errors.FromError(err)
		if !ok || appErr.Tag != "MultiDiffGone" || !appErr.HasRoute("gitscm.multiDiffResources.MultiDiffGone") {
			t.Errorf("%s: %v", k, err)
		}
	}
	// the window is told, for the groups and for the commits that were opened
	last := reg.MultiDiffChanges()[len(reg.MultiDiffChanges())-1]
	if !contains(last, key) || !contains(last, groupKey(rp.id, groupWorking)) {
		t.Errorf("changed %v", last)
	}
}

func TestAKeyThatIsNotOneIsRefused(t *testing.T) {
	r, _, _ := diffRig(t)
	for _, key := range []string{"", "x", "group/", "group/abc", "group//x", "other/abc/x"} {
		_, err := r.multiFiles(key)
		if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "GitMultiDiffKeyInvalid" {
			t.Errorf("%q: %v", key, err)
		}
	}
}

// settleUntil pumps the extension until the condition holds.
func (r *rig) settleUntil(what string, cond func() bool) {
	r.t.Helper()
	r.api.WaitFor(what, cond)
}

// history makes a repository with a root commit, a normal commit with every kind of change, and a merge.
func history(t *testing.T) (*rig, *gittest.Repo, *repository, map[string]string) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := e.Init("history")
	repo.Write("a.txt", "one\ntwo\nthree\nfour\nfive\nsix\n")
	repo.Write("b.txt", "bee\nbee\nbee\nbee\n")
	repo.Write("run.sh", "echo hi\n")
	repo.Write("gone.txt", "to be deleted\n")
	if err := os.WriteFile(repo.Path("blob.bin"), []byte{0, 1, 2, 3, 4}, 0o644); err != nil {
		t.Fatal(err)
	}
	commits := map[string]string{"root": repo.Commit("The root commit")}
	repo.Write("a.txt", "one\n2\nthree\nfour\nfive\nsix\n")
	repo.Remove("gone.txt")
	repo.Write("new.txt", "new\n")
	repo.Git("mv", "b.txt", "bb.txt")
	if err := os.Chmod(repo.Path("run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repo.Path("blob.bin"), []byte{0, 9, 9, 9}, 0o644); err != nil {
		t.Fatal(err)
	}
	commits["normal"] = repo.Commit("A normal commit\n\nwith a body")
	trunk := strings.TrimSpace(repo.Git("rev-parse", "--abbrev-ref", "HEAD"))
	repo.Branch("side", "")
	repo.Checkout("side")
	repo.Write("side.txt", "side\n")
	repo.Commit("side commit")
	repo.Checkout(trunk)
	repo.Write("main.txt", "main\n")
	repo.Commit("main commit")
	if err := repo.Merge("side"); err != nil {
		t.Fatal(err)
	}
	commits["merge"] = repo.Head()
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	return r, repo, r.repo(repo.Dir), commits
}

func TestTheChangesOfACommitAreAgainstItsFirstParent(t *testing.T) {
	r, repo, rp, commits := history(t)
	parent := strings.TrimSpace(repo.Git("rev-parse", commits["normal"]+"^"))
	tests := []struct {
		name, hash string
		want       []string
	}{
		{"a root commit is against nothing: every file is added", commits["root"], []string{
			"a.txt A - > " + commits["root"] + "/a.txt",
			"b.txt A - > " + commits["root"] + "/b.txt",
			"blob.bin A - > " + commits["root"] + "/blob.bin",
			"gone.txt A - > " + commits["root"] + "/gone.txt",
			"run.sh A - > " + commits["root"] + "/run.sh",
		}},
		{"a normal commit: a modification, a rename, a deletion, an addition, a mode change and a binary file", commits["normal"], []string{
			"a.txt M " + parent + "/a.txt > " + commits["normal"] + "/a.txt",
			"bb.txt R " + parent + "/b.txt > " + commits["normal"] + "/bb.txt",
			"blob.bin M " + parent + "/blob.bin > " + commits["normal"] + "/blob.bin",
			"gone.txt D " + parent + "/gone.txt > -",
			"new.txt A - > " + commits["normal"] + "/new.txt",
			"run.sh M " + parent + "/run.sh > " + commits["normal"] + "/run.sh",
		}},
		{"a merge commit is against its first parent", commits["merge"], []string{
			"side.txt A - > " + commits["merge"] + "/side.txt",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := r.mustMultiFiles(commitKey(rp.id, tc.hash))
			equalStrings(t, lines(rp, files), tc.want)
			for _, f := range files {
				if f.Context["scmResourceGroup"] != "commit" || f.Letter == "" || f.Color == "" || f.Args != nil {
					t.Errorf("%s: %+v", f.Path, f)
				}
				// Open File opens the file of the work tree, unless the commit deleted it
				if (f.Letter == "D") != (f.File == "") {
					t.Errorf("%s: file %q for %s", f.Path, f.File, f.Letter)
				}
			}
		})
	}
	// the text of the two sides comes from git, with the provider of the documents
	a := r.mustMultiFiles(commitKey(rp.id, commits["normal"]))[0]
	if got, _ := r.api.ContentText(context.Background(), a.Original); got != "one\ntwo\nthree\nfour\nfive\nsix\n" {
		t.Errorf("original %q", got)
	}
	if got, _ := r.api.ContentText(context.Background(), a.Modified); got != "one\n2\nthree\nfour\nfive\nsix\n" {
		t.Errorf("modified %q", got)
	}
	// a binary file is refused with the notice of the editor
	for _, f := range r.mustMultiFiles(commitKey(rp.id, commits["normal"])) {
		if f.Path == "blob.bin" {
			_, err := r.api.ContentText(context.Background(), f.Modified)
			if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ContentBinary" {
				t.Errorf("a binary file: %v", err)
			}
		}
	}
}

func TestViewCommitOpensTheEditorOfACommit(t *testing.T) {
	r, repo, rp, commits := history(t)
	open := func(args any) {
		t.Helper()
		before := len(r.api.OpenedMultiDiffs())
		if err := r.api.Run("git.viewCommit", args); err != nil {
			t.Fatal(err)
		}
		r.settleUntil("the editor", func() bool { return len(r.api.OpenedMultiDiffs()) > before })
	}
	open(map[string]any{"id": "HEAD~1"})
	spec := r.api.OpenedMultiDiffs()[0]
	main := strings.TrimSpace(repo.Git("rev-parse", "HEAD~1"))
	if spec.Kind != MultiDiffKind || spec.Key != commitKey(rp.id, main) || spec.Title != main[:7]+" - main commit" || spec.Preview || spec.Reveal != "" {
		t.Errorf("spec %+v", spec)
	}
	// the hash length is the setting's, the subject is the first line of the message, a revision is resolved to a hash
	r.api.SetSetting("git.commitShortHashLength", 10)
	open(map[string]any{"id": commits["normal"]})
	spec = r.api.OpenedMultiDiffs()[1]
	if spec.Title != commits["normal"][:10]+" - A normal commit" || spec.Key != commitKey(rp.id, commits["normal"]) {
		t.Errorf("spec %+v", spec)
	}
	// a file to reveal is the version of the file in the commit
	open(map[string]any{"id": commits["normal"], "revealUri": fileURI(filepath.Join(rp.root, "a.txt"))})
	if spec = r.api.OpenedMultiDiffs()[2]; spec.Reveal != toGitURI(rp.id, rp.root, filepath.Join(rp.root, "a.txt"), commits["normal"]) {
		t.Errorf("reveal %q", spec.Reveal)
	}
	// the Graph's name for the id, and a bare revision
	open(map[string]any{"historyItemId": commits["root"]})
	open(commits["root"])
	if got := r.api.OpenedMultiDiffs(); got[3].Key != got[4].Key {
		t.Errorf("the same commit with two spellings: %v %v", got[3].Key, got[4].Key)
	}
}

func TestViewCommitFailuresAreReported(t *testing.T) {
	r, _, _, _ := history(t)
	for _, id := range []string{"no-such-revision", "--output=/tmp/x"} {
		before := len(r.api.Notes())
		if err := r.api.Run("git.viewCommit", map[string]any{"id": id}); err != nil {
			t.Fatal(err)
		}
		r.settleUntil("the notification", func() bool { return len(r.api.Notes()) > before })
		note := r.api.Notes()[before]
		if note.Severity != "error" || !strings.Contains(note.Message, "Cannot open the commit "+id) {
			t.Errorf("%s: %+v", id, note)
		}
	}
	if n := len(r.api.OpenedMultiDiffs()); n != 0 {
		t.Errorf("%d editors opened", n)
	}
	// no id: nothing
	if err := r.api.Run("git.viewCommit", nil); err != nil {
		t.Fatal(err)
	}
}

func TestACommitSetOfARepositoryThatIsGoneIsNotAnEditorTheWindowKeeps(t *testing.T) {
	r, _, rp, commits := history(t)
	r.x.model.close(rp)
	r.settle()
	_, err := r.multiFiles(commitKey(rp.id, commits["normal"]))
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "MultiDiffGone" {
		t.Errorf("%v", err)
	}
}

func TestTheExtensionRegistersTheMultiDiffProvider(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("quiet")
	repo.Write("a.txt", "a\n")
	repo.Commit("a")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	if r.api.MultiDiffProviderFor(MultiDiffKind) == nil {
		t.Error("the provider is not registered")
	}
}
