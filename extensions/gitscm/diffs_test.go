package gitscm

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The content provider and the commands that open changes, against real repositories (step 24.7).

// contentOf asks the provider of the rig for the text of a version of a file.
func (r *rig) contentOf(repo *repository, ref, rel string) (string, error) {
	r.t.Helper()
	return r.api.ContentText(context.Background(), toGitURI(repo.id, repo.root, filepath.Join(repo.root, rel), ref))
}

func (r *rig) mustContent(repo *repository, ref, rel string) string {
	r.t.Helper()
	text, err := r.contentOf(repo, ref, rel)
	if err != nil {
		r.t.Fatalf("%s %q: %v", ref, rel, err)
	}
	return text
}

// conflicted makes a repository with the three stages of a conflicted file, and the committed history behind them.
func conflicted(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	repo := e.Init("conflict")
	repo.Write("c.txt", "base\n")
	repo.Commit("base")
	unmerge(t, repo, "c.txt", 1, 2, 3)
	return repo
}

func TestTheContentOfEveryVersionComesFromGit(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := statesRepo(t, e)
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	head := repo.Head()
	tests := []struct {
		name, ref, path string
		want            string
	}{
		{"HEAD", "HEAD", "staged_mod.txt", "staged_mod\nline two\nline three\nline four\nline five\n"},
		{"the index of a staged file", "", "staged_mod.txt", "staged_mod\nline two changed\nline three\nline four\nline five\n"},
		{"~ of a file with staged changes is the index", "~", "both_mod.txt", "both_mod\nline two changed\nline three\nline four\nline five\n"},
		{"~ of a file with no staged changes is the index, which is HEAD", "~", "unstaged_mod.txt", "unstaged_mod\nline two\nline three\nline four\nline five\n"},
		{"a commit by its id", head, "keep.txt", "keep\nline two\nline three\nline four\nline five\n"},
		{"a commit by a revision", "HEAD~0", "keep.txt", "keep\nline two\nline three\nline four\nline five\n"},
		{"a file deleted in the work tree is still in HEAD", "HEAD", "del_unstaged.txt", "del_unstaged\nline two\nline three\nline four\nline five\n"},
		{"a file deleted in the index is not in the index", "", "del_staged.txt", ""},
		{"a new file is not in HEAD", "HEAD", "staged_add.txt", ""},
		{"a new file is in the index", "", "staged_add.txt", "new\n"},
		{"an untracked file is nowhere in git", "", "untracked.txt", ""},
		{"a path that never existed", "HEAD", "no/such/file.txt", ""},
		{"a commit that never existed", strings.Repeat("0", 40), "keep.txt", ""},
		{"the rename in the index has the new name", "", "renamed_new.txt", "renamed_old\nline two\nline three\nline four\nline five\n"},
		{"the old name of a rename is in HEAD", "HEAD", "renamed_old.txt", "renamed_old\nline two\nline three\nline four\nline five\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.mustContent(rp, tc.ref, tc.path); got != tc.want {
				t.Errorf("%q", got)
			}
		})
	}
}

func TestTheStagesOfAConflictedFileAreTheStagesOfTheIndex(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := conflicted(t, e)
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	for ref, want := range map[string]string{"~1": "c.txt stage 1\n", "~2": "c.txt stage 2\n", "~3": "c.txt stage 3\n", "HEAD": "base\n"} {
		if got := r.mustContent(rp, ref, "c.txt"); got != want {
			t.Errorf("%s: %q, want %q", ref, got, want)
		}
	}
	// a stage the file does not have is an empty text
	unmerge(t, repo, "d.txt", 2)
	if got := r.mustContent(rp, "~3", "d.txt"); got != "" {
		t.Errorf("a missing stage: %q", got)
	}
}

func TestASymbolicLinkInTheCommitIsTheTextOfItsTarget(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("links")
	repo.Write("target.txt", "t\n")
	if err := os.Symlink("target.txt", repo.Path("link")); err != nil {
		t.Fatal(err)
	}
	repo.Git("add", "-A")
	repo.Commit("with a link")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	if got := r.mustContent(r.repo(repo.Dir), "HEAD", "link"); got != "target.txt" {
		t.Errorf("link %q", got)
	}
}

func TestTextThatIsNotUTF8IsShownWithoutBreakingAndTheByteOrderMarkIsDropped(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("encodings")
	if err := os.WriteFile(repo.Path("bom.txt"), []byte("\xef\xbb\xbfhello\r\nworld\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repo.Path("latin.txt"), []byte("caf\xe9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Git("add", "-A")
	repo.Commit("encodings")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	if got := r.mustContent(rp, "HEAD", "bom.txt"); got != "hello\r\nworld\r\n" {
		t.Errorf("bom %q", got)
	}
	if got := r.mustContent(rp, "HEAD", "latin.txt"); got != "caf�\n" {
		t.Errorf("latin %q", got)
	}
}

func TestABinaryFileIsRefusedWithTheTagTheWindowShowsTheNoticeFor(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("binary")
	if err := os.WriteFile(repo.Path("img.bin"), []byte("GIF89a\x00\x01\x02binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Git("add", "-A")
	repo.Commit("a binary file")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	_, err := r.contentOf(r.repo(repo.Dir), "HEAD", "img.bin")
	appErr, ok := errors.FromError(err)
	// the window finds the tag the contract names
	if !ok || !appErr.HasRoute("gitscm.contentText.ContentBinary") || !errors.Is(err, &errors.Error{Tag: extension.TagContentBinary}) {
		t.Fatalf("got %v", err)
	}
}

func TestAFileOverSixteenMebibytesIsRefusedWithItsOwnMessage(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("big")
	if err := os.WriteFile(repo.Path("big.txt"), bytes.Repeat([]byte("0123456789abcdef\n"), (git.MaxBlobSize/17)+10), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.Write("small.txt", "s\n")
	repo.Git("add", "-A")
	repo.Commit("a big file")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	_, err := r.contentOf(rp, "HEAD", "big.txt")
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "GitContentTooLarge" || !strings.Contains(appErr.Message, "too large") {
		t.Fatalf("got %v", err)
	}
	if got := r.mustContent(rp, "HEAD", "small.txt"); got != "s\n" {
		t.Errorf("small %q", got)
	}
}

func TestTheProviderRefusesURIsThatAreNotWhatItMakes(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("refusals")
	repo.Write("a.txt", "a\n")
	repo.Commit("a")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	tests := []struct {
		name, uri, route string
	}{
		{"not a version", "emetos-virtual:/git/nothing", "gitscm.contentText.GitContentURIInvalid"},
		{"a ref git would read as an option", extension.ContentURI("git", rp.id+"/--output=x/a.txt"), "gitscm.contentText.GitContentRefInvalid"},
		{"a ref with a space", extension.ContentURI("git", rp.id+"/a b/a.txt"), "gitscm.contentText.GitContentRefInvalid"},
		{"a repository that is not open", extension.ContentURI("git", "000000000000/HEAD/a.txt"), "gitscm.contentText.GitContentRepositoryGone"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.api.ContentText(context.Background(), tc.uri)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tc.route) {
				t.Fatalf("got %v, want the route %s", err, tc.route)
			}
		})
	}
	// the repository that is closed no longer answers
	r.x.model.close(rp)
	if _, err := r.contentOf(rp, "HEAD", "a.txt"); err == nil {
		t.Error("a closed repository answered")
	}
}

func TestTheLabelAndTheLanguageOfAVersion(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("labels")
	repo.Write("src/a.go", "package a\n")
	repo.Commit("a")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	uri := toGitURI(rp.id, rp.root, filepath.Join(rp.root, "src/a.go"), "HEAD")
	if got := r.api.ContentLabel(uri); got != "a.go" {
		t.Errorf("label %q", got)
	}
	sub := toSubmoduleURI(rp.id, rp.root, filepath.Join(rp.root, "libs/sub"), refSubIndex)
	if got := r.api.ContentLabel(sub); got != "sub.diff" {
		t.Errorf("label %q", got)
	}
	if got := r.api.ContentLanguage(uri); got != "" {
		t.Errorf("language %q: the window detects it from the label", got)
	}
}

func TestTheDiffOfASubmoduleIsGitsDiffOfIt(t *testing.T) {
	e := gittest.NewEnv(t)
	sub := e.Init("sub")
	sub.Write("s.txt", "one\n")
	sub.Commit("one")
	repo := e.Init("super")
	repo.Write("a.txt", "a\n")
	repo.Commit("a")
	repo.Submodule("libs/sub", sub)
	subWork := filepath.Join(repo.Dir, "libs", "sub")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	got, err := r.api.ContentText(context.Background(), toSubmoduleURI(rp.id, rp.root, filepath.Join(rp.root, "libs/sub"), refSubIndex))
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("nothing staged: %q", got)
	}
	// the work tree diff names the commits
	runGit(t, e, subWork, "commit", "--allow-empty", "-m", "two")
	got, err = r.api.ContentText(context.Background(), toSubmoduleURI(rp.id, rp.root, filepath.Join(rp.root, "libs/sub"), refSubWorkTre))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Subproject commit") || !strings.Contains(got, "-Subproject commit") {
		t.Errorf("work tree diff:\n%s", got)
	}
}

// runGit runs git in a folder in the world of a test.
func runGit(t *testing.T, e *gittest.Env, dir string, args ...string) string {
	t.Helper()
	out, errOut, err := e.GitInput(dir, "", args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, errOut)
	}
	return out
}

// diffRig is a rig over the states repository, settled.
func diffRig(t *testing.T) (*rig, *gittest.Repo, *repository) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo := statesRepo(t, e)
	unmerge(t, repo, "uu.txt", 1, 2, 3)
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	return r, repo, r.repo(repo.Dir)
}

// resourceArg is the argument a menu gives a command for a resource.
func resourceArg(r *repository, group, rel string) map[string]any {
	return map[string]any{
		extension.SCMArgSourceControl: r.handle, extension.SCMArgGroup: group, extension.SCMArgURI: fileURI(filepath.Join(r.root, rel)),
	}
}

func TestOpenChangesOpensTheDiffOfEachResourceOfTheMenu(t *testing.T) {
	r, _, rp := diffRig(t)
	id := rp.id
	vers := func(ref, rel string) string { return extension.ContentURI("git", id+"/"+ref+"/"+rel) }
	file := func(rel string) string { return fileURI(filepath.Join(rp.root, rel)) }
	tests := []struct {
		name, group, rel string
		diff             *extension.DiffSpec
		doc              *extension.OpenSpec
	}{
		{"staged modification", groupIndex, "staged_mod.txt", &extension.DiffSpec{Left: vers("HEAD", "staged_mod.txt"), Right: vers("", "staged_mod.txt"), Title: "staged_mod.txt (Index)", Preview: true}, nil},
		{"modification in the work tree", groupWorking, "unstaged_mod.txt", &extension.DiffSpec{Left: vers("~", "unstaged_mod.txt"), Right: file("unstaged_mod.txt"), Title: "unstaged_mod.txt (Working Tree)", Preview: true}, nil},
		{"staged rename", groupIndex, "renamed_new.txt", &extension.DiffSpec{Left: vers("HEAD", "renamed_old.txt"), Right: vers("", "renamed_new.txt"), Title: "renamed_new.txt (Index)", Preview: true}, nil},
		{"deleted file", groupWorking, "del_unstaged.txt", nil, &extension.OpenSpec{URI: vers("HEAD", "del_unstaged.txt"), Title: "del_unstaged.txt (Deleted)", Preview: true}},
		{"untracked file", groupWorking, "untracked.txt", nil, &extension.OpenSpec{URI: file("untracked.txt"), Title: "untracked.txt (Untracked)", Preview: true}},
		{"staged addition", groupIndex, "staged_add.txt", nil, &extension.OpenSpec{URI: vers("", "staged_add.txt"), Title: "staged_add.txt (Index)", Preview: true}},
		{"conflict both modified", groupMerge, "uu.txt", nil, &extension.OpenSpec{URI: file("uu.txt"), Title: "uu.txt (Working Tree)", Preview: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diffs, docs := len(r.api.OpenedDiffs()), len(r.api.OpenedDocuments())
			if err := r.api.Run("git.openChange", []any{resourceArg(rp, tc.group, tc.rel)}); err != nil {
				t.Fatal(err)
			}
			gotD, gotO := r.api.OpenedDiffs()[diffs:], r.api.OpenedDocuments()[docs:]
			switch {
			case tc.diff != nil && (len(gotD) != 1 || len(gotO) != 0 || gotD[0] != *tc.diff):
				t.Errorf("diffs %+v docs %+v, want %+v", gotD, gotO, *tc.diff)
			case tc.doc != nil && (len(gotO) != 1 || len(gotD) != 0 || !sameOpen(gotO[0], *tc.doc)):
				t.Errorf("diffs %+v docs %+v, want %+v", gotD, gotO, *tc.doc)
			}
		})
	}
}

func sameOpen(a, b extension.OpenSpec) bool {
	return a.URI == b.URI && a.Title == b.Title && a.Preview == b.Preview && a.PreserveFocus == b.PreserveFocus
}

func TestOpenChangesWithAFileOrNothingFindsTheResourceOfTheEditor(t *testing.T) {
	r, _, rp := diffRig(t)
	uri := fileURI(filepath.Join(rp.root, "unstaged_mod.txt"))
	// the argument of an editor title action: the group and the resource
	if err := r.api.Run("git.openChange", map[string]any{"groupId": 1.0, "uri": uri}); err != nil {
		t.Fatal(err)
	}
	// a file URI, a git URI of a version of the file, the active editor, and nothing at all
	versionURI := toGitURI(rp.id, rp.root, filepath.Join(rp.root, "unstaged_mod.txt"), "HEAD")
	if err := r.api.Run("git.openChange", uri); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run("git.openChange", versionURI); err != nil {
		t.Fatal(err)
	}
	r.api.SetActive(&extension.ActiveDocument{URI: uri})
	if err := r.api.Run("git.openChange", nil); err != nil {
		t.Fatal(err)
	}
	r.api.SetActive(nil)
	if err := r.api.Run("git.openChange", nil); err != nil {
		t.Fatal(err)
	}
	// a path, relative to the folder of the window tab or absolute
	if err := r.api.Run("git.openChange", map[string]any{"path": "unstaged_mod.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run("git.openChange", map[string]any{"path": filepath.Join(rp.root, "unstaged_mod.txt")}); err != nil {
		t.Fatal(err)
	}
	// a file with no changes
	if err := r.api.Run("git.openChange", fileURI(filepath.Join(rp.root, "keep.txt"))); err != nil {
		t.Fatal(err)
	}
	diffs := r.api.OpenedDiffs()
	if len(diffs) != 6 {
		t.Fatalf("%d diffs, want 6: %+v", len(diffs), diffs)
	}
	for _, d := range diffs {
		if d.Title != "unstaged_mod.txt (Working Tree)" || d.Right != uri {
			t.Errorf("diff %+v", d)
		}
	}
}

func TestOpenFileOpensTheFileNotTheDiffAndSkipsDeletedFiles(t *testing.T) {
	r, _, rp := diffRig(t)
	file := func(rel string) string { return fileURI(filepath.Join(rp.root, rel)) }
	if err := r.api.Run("git.openFile", []any{resourceArg(rp, groupWorking, "unstaged_mod.txt"), resourceArg(rp, groupWorking, "del_unstaged.txt"), resourceArg(rp, groupIndex, "del_staged.txt"), resourceArg(rp, groupIndex, "staged_add.txt")}); err != nil {
		t.Fatal(err)
	}
	got := r.api.OpenedDocuments()
	if len(got) != 2 || got[0].URI != file("unstaged_mod.txt") || got[1].URI != file("staged_add.txt") {
		t.Fatalf("opened %+v", got)
	}
	for _, o := range got {
		if o.Preview || !o.PreserveFocus || o.Title != "" {
			t.Errorf("%+v: kept, behind the focus (a resource), with no title expected", o)
		}
	}
	// from a version of the file, the file opens with the selection of the editor it comes from
	versionURI := toGitURI(rp.id, rp.root, filepath.Join(rp.root, "unstaged_mod.txt"), "HEAD")
	sel := extension.Selection{Anchor: extension.Position{Line: 2, Col: 1}, Active: extension.Position{Line: 3, Col: 4}}
	r.api.SetActive(&extension.ActiveDocument{URI: versionURI, Selections: []extension.Selection{sel}})
	if err := r.api.Run("git.openFile", versionURI); err != nil {
		t.Fatal(err)
	}
	last := r.api.OpenedDocuments()[len(got)]
	if last.URI != file("unstaged_mod.txt") || last.Selection == nil || *last.Selection != sel || last.PreserveFocus {
		t.Errorf("from a version: %+v", last)
	}
	// a file that is not in the repository, and an editor with no changes: opened as it is
	r.api.SetActive(&extension.ActiveDocument{URI: file("keep.txt")})
	if err := r.api.Run("git.openFile", nil); err != nil {
		t.Fatal(err)
	}
	if last := r.api.OpenedDocuments()[len(got)+1]; last.URI != file("keep.txt") {
		t.Errorf("no changes: %+v", last)
	}
	// Open File (the inline one) is the same command
	before := len(r.api.OpenedDocuments())
	if err := r.api.Run("git.openFile2", []any{resourceArg(rp, groupWorking, "unstaged_mod.txt")}); err != nil {
		t.Fatal(err)
	}
	if n := len(r.api.OpenedDocuments()); n != before+1 {
		t.Errorf("%d opened by openFile2", n-before)
	}
}

func TestOpenHEADFileOpensTheLeftSideReadOnlyWithItsTitle(t *testing.T) {
	r, _, rp := diffRig(t)
	vers := func(ref, rel string) string { return extension.ContentURI("git", rp.id+"/"+ref+"/"+rel) }
	// from the menu: kept; from the palette with the file of the editor: a preview
	if err := r.api.Run("git.openHEADFile", []any{resourceArg(rp, groupIndex, "staged_mod.txt")}); err != nil {
		t.Fatal(err)
	}
	r.api.SetActive(&extension.ActiveDocument{URI: fileURI(filepath.Join(rp.root, "unstaged_mod.txt"))})
	if err := r.api.Run("git.openHEADFile", nil); err != nil {
		t.Fatal(err)
	}
	got := r.api.OpenedDocuments()
	want := []extension.OpenSpec{
		{URI: vers("HEAD", "staged_mod.txt"), Title: "staged_mod.txt (HEAD)", Preview: false},
		{URI: vers("~", "unstaged_mod.txt"), Title: "unstaged_mod.txt (HEAD)", Preview: true},
	}
	if len(got) != 2 || !sameOpen(got[0], want[0]) || !sameOpen(got[1], want[1]) {
		t.Fatalf("opened %+v, want %+v", got, want)
	}
	// a file with no left side has no HEAD version
	if err := r.api.Run("git.openHEADFile", []any{resourceArg(rp, groupIndex, "staged_add.txt")}); err != nil {
		t.Fatal(err)
	}
	if n := len(r.api.OpenedDocuments()); n != 2 {
		t.Errorf("%d documents", n)
	}
	notes := r.api.Notes()
	if len(notes) == 0 || notes[len(notes)-1].Message != `HEAD version of "staged_add.txt" is not available.` {
		t.Errorf("notes %+v", notes)
	}
}

func TestTheClickOnARowOpensTheDiffAsAPreviewAndAKeptTabOnDoubleClick(t *testing.T) {
	r, _, rp := diffRig(t)
	cmd := r.source(rp.root).Group(groupWorking).Resources()[0].Command
	if cmd == nil || cmd.ID != CommandOpenResource {
		t.Fatalf("command %+v", cmd)
	}
	arg := resourceArg(rp, groupWorking, "unstaged_mod.txt")
	run := func(extra map[string]any) {
		t.Helper()
		a := map[string]any{}
		for k, v := range arg {
			a[k] = v
		}
		for k, v := range extra {
			a[k] = v
		}
		if err := r.api.Run(CommandOpenResource, a); err != nil {
			t.Fatal(err)
		}
	}
	run(map[string]any{"preserveFocus": true, "pinned": false})
	run(map[string]any{"pinned": true})
	diffs := r.api.OpenedDiffs()
	if len(diffs) != 2 || !diffs[0].Preview || !diffs[0].PreserveFocus || diffs[1].Preview || diffs[1].PreserveFocus {
		t.Fatalf("diffs %+v", diffs)
	}
	// with git.openDiffOnClick off the click opens the file
	r.api.SetSetting("git.openDiffOnClick", false)
	run(map[string]any{"preserveFocus": true})
	docs := r.api.OpenedDocuments()
	if len(docs) != 1 || docs[0].URI != fileURI(filepath.Join(rp.root, "unstaged_mod.txt")) || !docs[0].Preview || !docs[0].PreserveFocus {
		t.Fatalf("docs %+v", docs)
	}
	// a resource that is gone, and an argument that is no resource, do nothing
	run(map[string]any{extension.SCMArgURI: fileURI(filepath.Join(rp.root, "gone.txt"))})
	if err := r.api.Run(CommandOpenResource, "x"); err != nil {
		t.Fatal(err)
	}
	if len(r.api.OpenedDiffs()) != 2 || len(r.api.OpenedDocuments()) != 1 {
		t.Error("something opened")
	}
}

func TestEveryResourceOfTheViewRunsTheOpenResourceCommand(t *testing.T) {
	r, _, rp := diffRig(t)
	for _, g := range []string{groupMerge, groupIndex, groupWorking} {
		res := r.source(rp.root).Group(g).Resources()
		if len(res) == 0 {
			t.Fatalf("group %s is empty", g)
		}
		for _, s := range res {
			if s.Command == nil || s.Command.ID != CommandOpenResource {
				t.Errorf("%s %s: %+v", g, s.URI, s.Command)
			}
		}
	}
}

func TestRevealOpensTheExplorerAndTheFileManager(t *testing.T) {
	r, _, rp := diffRig(t)
	arg := []any{resourceArg(rp, groupWorking, "unstaged_mod.txt")}
	if err := r.api.Run("git.revealInExplorer", arg); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run("git.revealFileInOS.linux", arg); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run("git.revealInExplorer", nil); err != nil {
		t.Fatal(err)
	}
	uri := fileURI(filepath.Join(rp.root, "unstaged_mod.txt"))
	got := r.api.RevealedFiles()
	if len(got) != 2 || got[0].URI != uri || got[0].InOS || got[1].URI != uri || !got[1].InOS {
		t.Errorf("revealed %+v", got)
	}
}

func TestTheContextKeysFollowTheFileOfTheActiveEditor(t *testing.T) {
	r, _, rp := diffRig(t)
	key := func(k string) bool { v, _ := r.api.ContextKey(k).(bool); return v }
	set := func(rel string) {
		r.api.SetActive(&extension.ActiveDocument{URI: fileURI(filepath.Join(rp.root, rel))})
		r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: fileURI(filepath.Join(rp.root, rel))})
		r.api.Pump()
	}
	for _, tc := range []struct {
		rel              string
		staged, unstaged bool
	}{
		{"staged_mod.txt", true, false},
		{"unstaged_mod.txt", false, true},
		{"both_mod.txt", true, true},
		{"keep.txt", false, false},
		{"untracked.txt", false, true}, // git.untrackedChanges is mixed: it is a change of the work tree
	} {
		set(tc.rel)
		if key("git.activeResourceHasStagedChanges") != tc.staged || key("git.activeResourceHasUnstagedChanges") != tc.unstaged {
			t.Errorf("%s: staged %v unstaged %v", tc.rel, key("git.activeResourceHasStagedChanges"), key("git.activeResourceHasUnstagedChanges"))
		}
	}
	r.api.SetActive(nil)
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{})
	r.api.Pump()
	if key("git.activeResourceHasStagedChanges") || key("git.activeResourceHasUnstagedChanges") {
		t.Error("keys set with no editor")
	}
}

func TestOpenDocumentsAreMadeAgainWhenTheRepositoryChanges(t *testing.T) {
	r, repo, rp := diffRig(t)
	indexURI := toGitURI(rp.id, rp.root, filepath.Join(rp.root, "unstaged_mod.txt"), refIndex)
	headURI := toGitURI(rp.id, rp.root, filepath.Join(rp.root, "unstaged_mod.txt"), refHEAD)
	otherURI := toGitURI("ffffffffffff", "/other", "/other/x.txt", refIndex)
	for _, u := range []string{indexURI, headURI, otherURI} {
		r.api.SetDocument(u, "x")
	}
	reg := r.api.ContentProviderFor("git")
	if reg == nil {
		t.Fatal("no provider")
	}
	before := len(reg.ContentChanges())
	repo.Git("add", "unstaged_mod.txt")
	r.refresh(rp)
	got := reg.ContentChanges()[before:]
	if len(got) == 0 || !slices.Contains(got[len(got)-1], indexURI) || slices.Contains(got[len(got)-1], headURI) || slices.Contains(got[len(got)-1], otherURI) {
		t.Fatalf("after a refresh: %v", got)
	}
	// HEAD moves with a commit
	before = len(reg.ContentChanges())
	repo.Commit("commit the staged change")
	r.refresh(rp)
	got = reg.ContentChanges()[before:]
	if len(got) == 0 || !slices.Contains(got[len(got)-1], headURI) {
		t.Fatalf("after a commit: %v", got)
	}
}

func TestCloseAllDiffEditorsAndCloseDiffOnOperation(t *testing.T) {
	r, _, rp := diffRig(t)
	id := rp.id
	file := func(rel string) string { return fileURI(filepath.Join(rp.root, rel)) }
	vers := func(ref, rel string) string { return extension.ContentURI("git", id+"/"+ref+"/"+rel) }
	tab := func(n int, kind, uri, orig, mod string) extension.DocumentTab {
		return extension.DocumentTab{ID: "1:" + string(rune('a'+n)), Group: 1, Kind: kind, URI: uri, Original: orig, Modified: mod}
	}
	tabs := []extension.DocumentTab{
		tab(0, "diff", "", vers("HEAD", "a.txt"), vers("", "a.txt")),                                                                   // index
		tab(1, "diff", "", vers("HEAD", "b.txt"), vers("", "b.txt")),                                                                   // index
		tab(2, "diff", "", vers("~", "a.txt"), file("a.txt")),                                                                          // work tree
		tab(3, "diff", "", vers("~", "b.txt"), file("b.txt")),                                                                          // work tree
		tab(4, "diff", "", "emetos-virtual:/saved/x", file("x.txt")),                                                                   // a comparison with the saved file
		tab(5, "diff", "", extension.ContentURI("git", "ffffffffffff/HEAD/a.txt"), extension.ContentURI("git", "ffffffffffff//a.txt")), // another repository
		tab(6, "text", file("a.txt"), "", ""),
	}
	closed := func(indexPaths, workPaths []string, force bool) []string {
		r.api.SetTabs(tabs...)
		rp.closeDiffEditors(indexPaths, workPaths, force)
		got := r.api.ClosedTabs()
		return got
	}
	abs := func(rel string) string { return filepath.Join(rp.root, rel) }
	// the setting off: nothing, unless forced
	if got := closed(nil, nil, false); len(got) != 0 {
		t.Errorf("closed %v with git.closeDiffOnOperation off", got)
	}
	if got := closed(nil, nil, true); !slices.Equal(got, []string{"1:a", "1:b", "1:c", "1:d"}) {
		t.Errorf("forced: closed %v", got)
	}
	// the setting on: the paths decide
	r.api.SetSetting("git.closeDiffOnOperation", true)
	r.api.Pump()
	before := len(r.api.ClosedTabs())
	r.api.SetTabs(tabs...)
	rp.closeDiffEditors([]string{abs("a.txt")}, []string{}, false)
	if got := r.api.ClosedTabs()[before:]; !slices.Equal(got, []string{"1:a"}) {
		t.Errorf("one index path: closed %v", got)
	}
	before = len(r.api.ClosedTabs())
	r.api.SetTabs(tabs...)
	rp.closeDiffEditors([]string{}, []string{abs("b.txt")}, false)
	if got := r.api.ClosedTabs()[before:]; !slices.Equal(got, []string{"1:d"}) {
		t.Errorf("one work tree path: closed %v", got)
	}
}

func TestCloseAllUnmodifiedEditorsKeepsTheFilesWithChangesAndTheDirtyOnes(t *testing.T) {
	r, _, rp := diffRig(t)
	file := func(rel string) string { return fileURI(filepath.Join(rp.root, rel)) }
	r.api.SetTabs(
		extension.DocumentTab{ID: "1:a", Kind: "text", URI: file("staged_mod.txt")},
		extension.DocumentTab{ID: "1:b", Kind: "text", URI: file("keep.txt")},
		extension.DocumentTab{ID: "1:c", Kind: "text", URI: file("keep.txt"), Dirty: true},
		extension.DocumentTab{ID: "1:d", Kind: "text", URI: file("untracked.txt")},
		extension.DocumentTab{ID: "1:e", Kind: "text", URI: file("uu.txt")},
		extension.DocumentTab{ID: "1:f", Kind: "text", URI: file("docs/readme.md")},
		extension.DocumentTab{ID: "1:g", Kind: "diff", Original: file("keep.txt"), Modified: file("keep.txt")},
	)
	if err := r.api.Run("git.closeAllUnmodifiedEditors", nil); err != nil {
		t.Fatal(err)
	}
	if got := r.api.ClosedTabs(); !slices.Equal(got, []string{"1:b", "1:f"}) {
		t.Errorf("closed %v", got)
	}
	r.api.SetTabs(extension.DocumentTab{ID: "1:z", Kind: "diff", Original: extension.ContentURI("git", rp.id+"/HEAD/x.txt"), Modified: file("x.txt")})
	if err := r.api.Run("git.closeAllDiffEditors", []any{map[string]any{extension.SCMArgSourceControl: rp.handle}}); err != nil {
		t.Fatal(err)
	}
	if got := r.api.ClosedTabs(); !slices.Equal(got, []string{"1:b", "1:f", "1:z"}) {
		t.Errorf("closed %v", got)
	}
}

// failingContentAPI is the fake API whose documents refuse a provider with an error of the tag.
type failingContentAPI struct {
	*fakeapi.API
	tag    string
	logged *bytes.Buffer
}

func (a failingContentAPI) Documents() extension.Documents { return failingContentDocs(a) }
func (a failingContentAPI) Log() *slog.Logger              { return slog.New(slog.NewTextHandler(a.logged, nil)) }

type failingContentDocs failingContentAPI

func (d failingContentDocs) ProvideContent(extension.ContentProvider) (extension.ContentRegistration, error) {
	return nil, errors.New(d.tag, "the provider is refused")
}
func (d failingContentDocs) Read(ctx context.Context, uri string, o ...extension.ReadOption) (string, error) {
	return d.API.Documents().Read(ctx, uri, o...)
}
func (d failingContentDocs) Open() []string { return d.API.Documents().Open() }
func (d failingContentDocs) Active() (extension.ActiveDocument, bool) {
	return d.API.Documents().Active()
}
func (d failingContentDocs) Show(uri string, line, column int, preview bool) error {
	return d.API.Documents().Show(uri, line, column, preview)
}
func (d failingContentDocs) OpenUntitled(l, t string) (string, error) {
	return d.API.Documents().OpenUntitled(l, t)
}
func (d failingContentDocs) Apply(ctx context.Context, e extension.WorkspaceEdit) error {
	return d.API.Documents().Apply(ctx, e)
}
func (d failingContentDocs) OpenDiff(s extension.DiffSpec) error {
	return d.API.Documents().OpenDiff(s)
}
func (d failingContentDocs) OpenDocument(s extension.OpenSpec) error {
	return d.API.Documents().OpenDocument(s)
}
func (d failingContentDocs) ProvideMultiDiff(extension.MultiDiffProvider) (extension.MultiDiffRegistration, error) {
	return nil, errors.New(d.tag, "the multi-file provider is refused")
}
func (d failingContentDocs) OpenMultiDiff(s extension.MultiDiffSpec) error {
	return d.API.Documents().OpenMultiDiff(s)
}
func (d failingContentDocs) Tabs() []extension.DocumentTab { return d.API.Documents().Tabs() }
func (d failingContentDocs) Dirty() []string               { return d.API.Documents().Dirty() }
func (d failingContentDocs) SaveAll(ctx context.Context, uris []string) error {
	return d.API.Documents().SaveAll(ctx, uris)
}
func (d failingContentDocs) QuickDiffOriginal(ctx context.Context, uri string) (string, bool, error) {
	return d.API.Documents().QuickDiffOriginal(ctx, uri)
}
func (d failingContentDocs) LineChanges(ctx context.Context, original, modified string) ([]extension.LineChange, error) {
	return d.API.Documents().LineChanges(ctx, original, modified)
}
func (d failingContentDocs) CloseTabs(t ...extension.DocumentTab) int {
	return d.API.Documents().CloseTabs(t...)
}
func (d failingContentDocs) CloseTabsAs(how extension.TabClose, t ...extension.DocumentTab) int {
	return d.API.Documents().CloseTabsAs(how, t...)
}

// TestAScopeThatCannotShowDocumentsIsNotWarnedAbout keeps a scope that cannot show documents (a host without a workbench) out of the log,
// and warns about every other failure of ProvideContent.
func TestAScopeThatCannotShowDocumentsIsNotWarnedAbout(t *testing.T) {
	for _, tc := range []struct {
		tag      string
		wantWarn bool
	}{
		{"ExtensionDocumentUnavailable", false},
		{"ExtensionDeactivated", true},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			e := gittest.NewEnv(t)
			repo := e.Init("repo")
			repo.Write("a.go", "one")
			repo.Commit("initial commit")
			r := newRig(t, e, rigOptions{folder: repo.Dir, noStart: true})
			var logged bytes.Buffer
			if err := r.x.Activate(t.Context(), failingContentAPI{API: r.api, tag: tc.tag, logged: &logged}); err != nil {
				t.Fatal(err)
			}
			r.settle()
			if warned := strings.Contains(logged.String(), "cannot be provided"); warned != tc.wantWarn {
				t.Errorf("warned %v, want %v; the log:\n%s", warned, tc.wantWarn, logged.String())
			}
		})
	}
}
