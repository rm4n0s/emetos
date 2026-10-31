package gitscm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// Add to .gitignore, Rename and Delete (step 24.8).

func TestIgnoreLineEscapesWhatGitReadsAsAPattern(t *testing.T) {
	tests := []struct{ in, want string }{
		{"src/loader.go", "src/loader.go"},
		{"a[1].txt", `a\[1].txt`},
		{"what?.txt", `what\?.txt`},
		{"star*.txt", `star\*.txt`},
		{`back\slash.txt`, `back\\slash.txt`},
		{"#hash.txt", `\#hash.txt`},
		{"!bang.txt", `\!bang.txt`},
		{"a#b", "a#b"},
		{"trailing ", `trailing\ `},
		{"two  ", `two\ \ `},
		{"sp ace/ünï.txt", "sp ace/ünï.txt"},
	}
	for _, tc := range tests {
		if got := ignoreLine(tc.in); got != tc.want {
			t.Errorf("ignoreLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAppendToTheIgnoreFile(t *testing.T) {
	tests := []struct {
		name     string
		existing *string
		add      []string
		want     string
	}{
		{"a file that is not there is made", nil, []string{"a.txt", "b/c.txt"}, "a.txt\nb/c.txt\n"},
		{"after the lines it has", ptr("x\n"), []string{"a.txt"}, "x\na.txt\n"},
		{"after a last line with no break", ptr("x"), []string{"a.txt"}, "x\na.txt\n"},
		{"a pattern it has is not added twice", ptr("a.txt\n"), []string{"a.txt", "b.txt", "b.txt"}, "a.txt\nb.txt\n"},
		{"nothing new leaves the file as it is", ptr("a.txt"), []string{"a.txt"}, "a.txt"},
		{"an empty file", ptr(""), []string{"a.txt"}, "a.txt\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), ".gitignore")
			if tc.existing != nil {
				writeFile(t, file, *tc.existing)
			}
			if err := appendToIgnoreFile(file, tc.add); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(file)
			if err != nil || string(data) != tc.want {
				t.Errorf("%q, %v; want %q", data, err, tc.want)
			}
		})
	}
	t.Run("a file that cannot be written", func(t *testing.T) {
		if err := appendToIgnoreFile(filepath.Join(t.TempDir(), "no", "such", ".gitignore"), []string{"a"}); err == nil {
			t.Error("no error")
		}
	})
}

func ptr(s string) *string { return &s }

func TestIgnoreAddsThePathsAndStagesNothing(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{})
	before := porcelain(repo)
	r.run(rp, "git.ignore", resourceArgs(rp, groupWorking, "u1.txt", "a[1].txt", "newdir/u2.txt"))
	if got := repo.Read(".gitignore"); got != "u1.txt\na\\[1].txt\nnewdir/u2.txt\n" {
		t.Errorf(".gitignore = %q", got)
	}
	// the untracked files are ignored now; the tracked one is still changed; nothing was staged
	got := strings.Join(porcelain(repo), "|")
	if strings.Contains(got, "u1.txt") || strings.Contains(got, "u2.txt") || !strings.Contains(got, " M a[1].txt") || !strings.Contains(got, "?? .gitignore") {
		t.Errorf("status before\n%s\nafter\n%s", strings.Join(before, "|"), got)
	}
	for _, l := range porcelain(repo) {
		if l[0] != ' ' && l[0] != '?' && !strings.HasPrefix(l, "M  b.txt") && !strings.HasPrefix(l, "MM c.txt") {
			t.Errorf("%q is staged", l)
		}
	}
	// a second time adds nothing
	r.run(rp, "git.ignore", resourceArgs(rp, groupWorking, "u1.txt"))
	if got := repo.Read(".gitignore"); strings.Count(got, "u1.txt") != 1 {
		t.Errorf("twice: %q", got)
	}
	// the file of the editor
	r.api.SetActive(&extension.ActiveDocument{URI: fileURI(repo.Path("dir/x.txt"))})
	r.run(rp, "git.ignore", nil)
	if got := repo.Read(".gitignore"); !strings.HasSuffix(got, "dir/x.txt\n") {
		t.Errorf("the file of the editor: %q", got)
	}
}

func TestRenameAsksForThePathMovesTheFileAndShowsTheNewName(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{})
	old := fileURI(repo.Path("keep.txt"))
	r.api.SetTabs(extension.DocumentTab{ID: "1:keep", Kind: "text", URI: old})
	r.api.SetActive(&extension.ActiveDocument{URI: old})
	if err := r.api.Run("git.rename", nil); err != nil {
		t.Fatal(err)
	}
	in := r.api.LastInput()
	if in == nil || in.Options().Value != "keep.txt" {
		t.Fatalf("input %+v", in)
	}
	in.Accept("moved dir/kept ünï.txt")
	r.api.Pump()
	r.idle(rp)
	// the folder does not exist: git refuses
	if exists(repo, "keep.txt") != true || len(r.api.Notes()) != 1 {
		t.Fatalf("the file moved to a missing folder: notes %+v", r.api.Notes())
	}
	r.api.LastInput() // the failure showed a notification, no second box
	if err := r.api.Run("git.rename", nil); err != nil {
		t.Fatal(err)
	}
	r.api.LastInput().Accept("dir/kept ünï.txt")
	r.api.Pump()
	r.idle(rp)
	if exists(repo, "keep.txt") || !exists(repo, "dir/kept ünï.txt") {
		t.Fatalf("keep.txt %v, kept %v", exists(repo, "keep.txt"), exists(repo, "dir/kept ünï.txt"))
	}
	if got := strings.Join(porcelain(repo), "|"); !strings.Contains(got, "R  dir/kept ünï.txt") {
		t.Errorf("status:\n%s", got)
	}
	if got := r.api.ClosedTabs(); len(got) != 1 || got[0] != "1:keep" {
		t.Errorf("closed %v", got)
	}
	opened := r.api.OpenedDocuments()
	if len(opened) != 1 || opened[0].URI != fileURI(repo.Path("dir/kept ünï.txt")) {
		t.Errorf("opened %+v", opened)
	}
}

func TestRenameDoesNothingForAnEmptyOrUnchangedName(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{})
	r.api.SetActive(&extension.ActiveDocument{URI: fileURI(repo.Path("keep.txt"))})
	for _, answer := range []string{"", "   ", "keep.txt"} {
		if err := r.api.Run("git.rename", nil); err != nil {
			t.Fatal(err)
		}
		r.api.LastInput().Accept(answer)
		r.api.Pump()
	}
	r.idle(rp)
	if !exists(repo, "keep.txt") || len(r.api.Notes()) != 0 {
		t.Errorf("something happened: %+v", r.api.Notes())
	}
	// an editor with a document that is no file of a repository
	r.api.SetActive(&extension.ActiveDocument{URI: "untitled:Untitled-1"})
	n := len(r.api.Inputs())
	if err := r.api.Run("git.rename", nil); err != nil || len(r.api.Inputs()) != n {
		t.Errorf("asked for the name of an untitled document: %v", err)
	}
}

func TestDeleteRemovesACommittedFileAfterAQuestion(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{})
	file := fileURI(repo.Path("keep.txt"))
	r.api.SetTabs(extension.DocumentTab{ID: "1:keep", Kind: "text", URI: file}, extension.DocumentTab{ID: "2:keep", Kind: "text", URI: file, Dirty: true})
	if err := r.api.Run("git.delete", file); err != nil {
		t.Fatal(err)
	}
	d := r.theOnlyDialog()
	if d.Spec().Message != "Are you sure you want to delete 'keep.txt'?" || buttons(d)[0] != "Delete File" {
		t.Errorf("%q %v", d.Spec().Message, buttons(d))
	}
	d.Cancel()
	r.idle(rp)
	if !exists(repo, "keep.txt") {
		t.Fatal("Cancel deleted the file")
	}
	if err := r.api.Run("git.delete", file); err != nil {
		t.Fatal(err)
	}
	r.theOnlyDialog().Press("Delete File")
	r.idle(rp)
	if exists(repo, "keep.txt") {
		t.Error("the file is still there")
	}
	if got := strings.Join(porcelain(repo), "|"); !strings.Contains(got, "D  keep.txt") {
		t.Errorf("status:\n%s", got)
	}
	if got := r.api.ClosedTabs(); len(got) != 1 || got[0] != "1:keep" {
		t.Errorf("closed %v (a tab with unsaved changes stays)", got)
	}
}

func TestDeleteWithoutTheQuestionAndRefusalsForFilesWithChanges(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{settings: map[string]any{"git.confirmCommittedDelete": false}})
	r.run(rp, "git.delete", map[string]any{"path": "keep.txt"})
	if exists(repo, "keep.txt") || len(r.api.OpenDialogs()) != 0 {
		t.Errorf("keep.txt %v, dialogs %d", exists(repo, "keep.txt"), len(r.api.OpenDialogs()))
	}
	for _, rel := range []string{"a.txt", "b.txt", "u1.txt"} {
		r.run(rp, "git.delete", fileURI(repo.Path(rel)))
		if !exists(repo, rel) {
			t.Errorf("%s was deleted", rel)
		}
	}
	notes := r.api.Notes()
	if len(notes) != 3 || notes[0].Message != "Git: Delete can only be performed on committed files without uncommitted changes." {
		t.Errorf("notes %+v", notes)
	}
	// a file git does not track at all and that has no change in the groups (ignored) is a failure of git
	repo.Write(".gitignore", "ign.txt\n")
	repo.Write("ign.txt", "i\n")
	r.refresh(rp)
	r.run(rp, "git.delete", fileURI(repo.Path("ign.txt")))
	if last := r.api.Notes()[len(r.api.Notes())-1]; last.Severity != "error" {
		t.Errorf("the failure of git was not shown: %+v", last)
	}
	if !exists(repo, "ign.txt") {
		t.Error("the file was deleted")
	}
}
