package gitscm

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// fakeDialog is a question the extension asked.
type fakeDialog = fakeapi.FakeDialog

// Discard against real repositories (step 24.8).

// theOnlyDialog waits for the one question the extension asked and returns it.
func (r *rig) theOnlyDialog() *fakeDialog {
	r.t.Helper()
	r.api.WaitFor("the question", func() bool { return len(r.api.OpenDialogs()) > 0 })
	d := r.api.OpenDialogs()
	if len(d) != 1 {
		r.t.Fatalf("%d questions are open", len(d))
	}
	return d[0]
}

// exists says whether a file of the repository is there.
func exists(repo *gittest.Repo, rel string) bool {
	_, err := os.Lstat(repo.Path(rel))
	return err == nil
}

func buttons(d *fakeDialog) []string {
	var out []string
	for _, b := range d.Spec().Buttons {
		out = append(out, b.Label)
	}
	return out
}

func TestDiscardTexts(t *testing.T) {
	tests := []struct {
		name    string
		names   []string
		kind    string
		toTrash bool
		message string
		detail  string
		buttons []string
	}{
		{"one changed file", []string{"a.go"}, "modified", true,
			"Are you sure you want to discard changes in 'a.go'?", "This action is irreversible!", []string{"Discard Changes", "Cancel"}},
		{"many changed files", []string{"a.go", "b.go", "c.go"}, "modified", true,
			"Are you sure you want to discard ALL changes in 3 files?",
			"a.go, b.go, c.go\n\nThis action is irreversible!\nYour current working set will be FOREVER LOST if you proceed.", []string{"Discard All 3 Files", "Cancel"}},
		{"more names than are listed", []string{"a", "b", "c", "d", "e", "f", "g"}, "modified", true,
			"Are you sure you want to discard ALL changes in 7 files?",
			"a, b, c, d, e and 2 more\n\nThis action is irreversible!\nYour current working set will be FOREVER LOST if you proceed.", []string{"Discard All 7 Files", "Cancel"}},
		{"one deleted file", []string{"a.go"}, "deleted", true, "Are you sure you want to restore 'a.go'?", "", []string{"Restore File", "Cancel"}},
		{"many deleted files", []string{"a.go", "b.go"}, "deleted", true, "Are you sure you want to restore ALL 2 files?", "a.go, b.go", []string{"Restore All 2 Files", "Cancel"}},
		{"one untracked file to the trash", []string{"u.txt"}, "untracked", true,
			"Are you sure you want to DELETE the following untracked file: 'u.txt'?", "You can restore this file from the Trash.", []string{"Move to Trash", "Cancel"}},
		{"many untracked files to the trash", []string{"u.txt", "v.txt"}, "untracked", true,
			"Are you sure you want to DELETE the 2 untracked files?", "u.txt, v.txt\n\nYou can restore these files from the Trash.", []string{"Move to Trash", "Cancel"}},
		{"one untracked file for good", []string{"u.txt"}, "untracked", false,
			"Are you sure you want to DELETE the following untracked file: 'u.txt'?", "This action is irreversible!\nThis file will be FOREVER LOST if you proceed.", []string{"Delete File", "Cancel"}},
		{"many untracked files for good", []string{"u.txt", "v.txt"}, "untracked", false,
			"Are you sure you want to DELETE the 2 untracked files?", "u.txt, v.txt\n\nThis action is irreversible!\nThese files will be FOREVER LOST if you proceed.", []string{"Delete All 2 Files", "Cancel"}},
		{"tracked and untracked files", []string{"u.txt"}, "mixed", true,
			"Are you sure you want to DELETE the following untracked file: 'u.txt'?",
			"You can restore this file from the Trash.\n\nAre you sure you want to discard changes in 'tracked.go'?\n\nThis action is irreversible!\nYour current working set will be FOREVER LOST if you proceed.",
			[]string{"Discard 1 Tracked File", "Discard All 2 Files", "Cancel"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := DiscardDialogs(tc.names, tc.kind, tc.toTrash)
			if spec.Type != extension.DialogWarning || spec.Message != tc.message || spec.Detail != tc.detail {
				t.Errorf("message %q\ndetail %q\nwant %q\n     %q", spec.Message, spec.Detail, tc.message, tc.detail)
			}
			var got []string
			for _, b := range spec.Buttons {
				got = append(got, b.Label)
			}
			if !reflect.DeepEqual(got, tc.buttons) {
				t.Errorf("buttons %v, want %v", got, tc.buttons)
			}
			if last := spec.Buttons[len(spec.Buttons)-1]; !last.Cancel {
				t.Error("the last button is not the cancel one")
			}
		})
	}
}

func TestDiscardingTrackedFiles(t *testing.T) {
	tests := []struct {
		name    string
		select_ func(rp *repository) any
		button  string
		message string
		gone    []string // changed files that are back as committed afterwards
	}{
		{"one changed file", func(rp *repository) any { return resourceArgs(rp, groupWorking, "a.txt") }, "Discard Changes",
			"Are you sure you want to discard changes in 'a.txt'?", []string{"a.txt"}},
		{"a deleted file comes back", func(rp *repository) any { return resourceArgs(rp, groupWorking, "del.txt") }, "Restore File",
			"Are you sure you want to restore 'del.txt'?", []string{"del.txt"}},
		{"several files", func(rp *repository) any { return resourceArgs(rp, groupWorking, "a.txt", "dir/x.txt", "dir/y.txt") }, "Discard All 3 Files",
			"Are you sure you want to discard ALL changes in 3 files?", []string{"a.txt", "dir/x.txt", "dir/y.txt"}},
		{"a path with spaces and non-ASCII characters", func(rp *repository) any { return resourceArgs(rp, groupWorking, "sp ace/ünï.txt") }, "Discard Changes",
			"Are you sure you want to discard changes in 'ünï.txt'?", []string{"sp ace/ünï.txt"}},
		{"a path with glob characters is that file only", func(rp *repository) any { return resourceArgs(rp, groupWorking, "a[1].txt") }, "Discard Changes",
			"Are you sure you want to discard changes in 'a[1].txt'?", []string{"a[1].txt"}},
		{"the same file twice is once", func(rp *repository) any {
			return append(resourceArgs(rp, groupWorking, "a.txt"), resourceArgs(rp, groupWorking, "a.txt")...)
		}, "Discard Changes", "Are you sure you want to discard changes in 'a.txt'?", []string{"a.txt"}},
		{"a staged file is not discarded", func(rp *repository) any { return resourceArgs(rp, groupIndex, "b.txt") }, "", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := stageRig(t, rigOptions{})
			before := porcelain(repo)
			if err := r.api.Run("git.clean", tc.select_(rp)); err != nil {
				t.Fatal(err)
			}
			if tc.button == "" {
				if d := r.api.OpenDialogs(); len(d) != 0 {
					t.Fatalf("a question for nothing: %q", d[0].Spec().Message)
				}
				return
			}
			d := r.theOnlyDialog()
			if d.Spec().Message != tc.message {
				t.Errorf("message %q, want %q", d.Spec().Message, tc.message)
			}
			// Cancel changes nothing
			d.Cancel()
			r.idle(rp)
			if !reflect.DeepEqual(porcelain(repo), before) {
				t.Fatalf("Cancel changed the status:\n%s", strings.Join(porcelain(repo), "\n"))
			}
			if err := r.api.Run("git.clean", tc.select_(rp)); err != nil {
				t.Fatal(err)
			}
			r.theOnlyDialog().Press(tc.button)
			r.idle(rp)
			after := strings.Join(porcelain(repo), "|")
			for _, rel := range tc.gone {
				if strings.Contains(after, " "+rel) && !strings.Contains(after, "?? "+rel) {
					t.Errorf("%s is still changed:\n%s", rel, after)
				}
				if !exists(repo, rel) {
					t.Errorf("%s is not there", rel)
				}
			}
			if got := repo.Read(tc.gone[0]); !strings.HasPrefix(got, tc.gone[0]+"\none\n") {
				t.Errorf("%s = %q, not the committed text", tc.gone[0], got)
			}
		})
	}
}

func TestDiscardKeepsTheStagedVersionOfAFileChangedAgain(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{})
	if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "c.txt")); err != nil {
		t.Fatal(err)
	}
	r.theOnlyDialog().Press("Discard Changes")
	r.idle(rp)
	if got := repo.Read("c.txt"); got != "c.txt\nstaged\n" {
		t.Errorf("c.txt = %q", got)
	}
	if got := strings.Join(porcelain(repo), "|"); !strings.Contains(got, "M  c.txt") || strings.Contains(got, "MM c.txt") {
		t.Errorf("the staged change was lost:\n%s", got)
	}
}

func TestDiscardingUntrackedFilesMovesThemToTheTrashAndOffersAnUndo(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{})
	if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "u1.txt")); err != nil {
		t.Fatal(err)
	}
	d := r.theOnlyDialog()
	if d.Spec().Message != "Are you sure you want to DELETE the following untracked file: 'u1.txt'?" || d.Spec().Detail != "You can restore this file from the Trash." {
		t.Errorf("%q %q", d.Spec().Message, d.Spec().Detail)
	}
	d.Cancel()
	r.idle(rp)
	if len(r.api.TrashedFiles()) != 0 || !exists(repo, "u1.txt") {
		t.Fatal("Cancel moved the file")
	}
	if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "u1.txt", "newdir/u2.txt")); err != nil {
		t.Fatal(err)
	}
	r.theOnlyDialog().Press("Move to Trash")
	r.idle(rp)
	trashed := r.api.TrashedFiles()
	if len(trashed) != 1 || !reflect.DeepEqual(trashed[0].URIs, []string{fileURI(repo.Path("u1.txt")), fileURI(repo.Path("newdir/u2.txt"))}) {
		t.Fatalf("trashed %+v", trashed)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Message != "Moved to Trash: 2 files" || !reflect.DeepEqual(notes[0].Actions, []string{"Undo"}) {
		t.Fatalf("notes %+v", notes)
	}
	// the groups no longer show them (the fake did not move the files, so the confirming status still lists them: it is the host that moves)
	r.api.RunNoteAction(0, "Undo")
	r.api.WaitFor("the undo", func() bool { r.api.Pump(); return trashed[0].Undone() })
}

func TestTheUndoOfAMoveToTheTrashFailsWithAMessage(t *testing.T) {
	r, _, rp := stageRig(t, rigOptions{})
	r.api.FailUndoTrash(errors.New("FileOpDestinationExists", "something is at the old path now"))
	if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "u1.txt")); err != nil {
		t.Fatal(err)
	}
	r.theOnlyDialog().Press("Move to Trash")
	r.idle(rp)
	r.api.RunNoteAction(0, "Undo")
	r.api.WaitFor("the message", func() bool { r.api.Pump(); return len(r.api.Notes()) == 2 })
	if got := r.api.Notes()[1]; got.Severity != "error" || !strings.HasPrefix(got.Message, "Cannot restore u1.txt from the Trash: ") {
		t.Errorf("%+v", got)
	}
}

func TestDiscardingUntrackedFilesForGoodWhenTheSettingIsOff(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{settings: map[string]any{"git.discardUntrackedChangesToTrash": false}})
	repo.Write(".gitignore", "ignored.txt\n")
	repo.Write("ignored.txt", "i\n")
	r.refresh(rp)
	if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "u1.txt")); err != nil {
		t.Fatal(err)
	}
	d := r.theOnlyDialog()
	if d.Spec().Detail != "This action is irreversible!\nThis file will be FOREVER LOST if you proceed." || buttons(d)[0] != "Delete File" {
		t.Errorf("%q %v", d.Spec().Detail, buttons(d))
	}
	d.Press("Delete File")
	r.idle(rp)
	if exists(repo, "u1.txt") || !exists(repo, "newdir/u2.txt") || !exists(repo, "ignored.txt") {
		t.Errorf("u1 %v, u2 %v, ignored %v", exists(repo, "u1.txt"), exists(repo, "newdir/u2.txt"), exists(repo, "ignored.txt"))
	}
	if len(r.api.TrashedFiles()) != 0 || len(r.api.Notes()) != 0 {
		t.Errorf("the trash was used or a message shown: %v %v", r.api.TrashedFiles(), r.api.Notes())
	}
}

func TestATrashThatCannotTakeTheFilesAsksBeforeDeletingForGood(t *testing.T) {
	for _, button := range []string{"Delete All 2 Files", ""} {
		t.Run("answer "+button, func(t *testing.T) {
			r, repo, rp := stageRig(t, rigOptions{})
			r.api.FailTrash(errors.New("TrashCrossDevice", "the item is on another file system"))
			if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "u1.txt", "newdir/u2.txt")); err != nil {
				t.Fatal(err)
			}
			r.theOnlyDialog().Press("Move to Trash")
			r.api.WaitFor("the second question", func() bool { return len(r.api.OpenDialogs()) == 1 })
			d := r.theOnlyDialog()
			if d.Spec().Message != "Failed to delete using the Trash. Do you want to permanently delete instead?" || buttons(d)[0] != "Delete All 2 Files" {
				t.Errorf("%q %v", d.Spec().Message, buttons(d))
			}
			if button == "" {
				d.Cancel()
			} else {
				d.Press(button)
			}
			r.idle(rp)
			if gone := !exists(repo, "u1.txt") && !exists(repo, "newdir/u2.txt"); gone != (button != "") {
				t.Errorf("files gone: %v with the answer %q", gone, button)
			}
		})
	}
}

func TestAScopeWithoutATrashDeletesLikeTheSettingOff(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{})
	r.api.FailTrash(errors.New("ExtensionTrashUnavailable", "the scope has no trash"))
	if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "u1.txt")); err != nil {
		t.Fatal(err)
	}
	r.theOnlyDialog().Press("Move to Trash")
	r.idle(rp)
	if exists(repo, "u1.txt") {
		t.Error("the file is still there")
	}
	if d := r.api.OpenDialogs(); len(d) != 0 {
		t.Errorf("a second question: %q", d[0].Spec().Message)
	}
}

func TestDiscardingTrackedAndUntrackedFilesTogether(t *testing.T) {
	tests := []struct {
		button         string
		trackedBack    bool
		untrackedMoved bool
	}{
		{"Discard 1 Tracked File", true, false},
		{"Discard All 2 Files", true, true},
		{"", false, false},
	}
	for _, tc := range tests {
		t.Run("answer "+tc.button, func(t *testing.T) {
			r, repo, rp := stageRig(t, rigOptions{})
			if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "a.txt", "u1.txt")); err != nil {
				t.Fatal(err)
			}
			d := r.theOnlyDialog()
			if !reflect.DeepEqual(buttons(d), []string{"Discard 1 Tracked File", "Discard All 2 Files", "Cancel"}) {
				t.Fatalf("buttons %v", buttons(d))
			}
			if tc.button == "" {
				d.Cancel()
			} else {
				d.Press(tc.button)
			}
			r.idle(rp)
			if back := strings.HasPrefix(repo.Read("a.txt"), "a.txt\none\n"); back != tc.trackedBack {
				t.Errorf("a.txt back: %v", back)
			}
			if moved := len(r.api.TrashedFiles()) == 1; moved != tc.untrackedMoved {
				t.Errorf("u1.txt moved: %v", moved)
			}
		})
	}
}

func TestDiscardAllCommands(t *testing.T) {
	// the new files of the repository are u1.txt, newdir/u2.txt and intent.txt (an intent to add file is deleted like an untracked one:
	// git checkout would empty it); seven files are tracked
	noChangesLeft := func(t *testing.T, repo *gittest.Repo) {
		t.Helper()
		got := strings.Join(porcelain(repo), "|")
		if strings.Contains(got, " M ") || strings.Contains(got, " D ") || strings.Contains(got, "MM ") {
			t.Errorf("tracked changes are left:\n%s", got)
		}
	}
	tests := []struct {
		name     string
		settings map[string]any
		cmd      string
		arg      func(rp *repository) any
		message  string
		button   string
		check    func(t *testing.T, r *rig, repo *gittest.Repo)
	}{
		{"cleanAll in the mixed mode asks about everything", nil, "git.cleanAll", func(rp *repository) any { return []any{groupArg(rp, groupWorking)} },
			"Are you sure you want to DELETE the 3 untracked files?", "Discard All 10 Files", func(t *testing.T, r *rig, repo *gittest.Repo) {
				noChangesLeft(t, repo)
				if len(r.api.TrashedFiles()) != 1 || len(r.api.TrashedFiles()[0].URIs) != 3 {
					t.Errorf("trashed %+v", r.api.TrashedFiles())
				}
			}},
		{"cleanAllTracked leaves the new files", nil, "git.cleanAllTracked", func(rp *repository) any { return []any{groupArg(rp, groupWorking)} },
			"Are you sure you want to discard ALL changes in 7 files?", "Discard All 7 Files", func(t *testing.T, r *rig, repo *gittest.Repo) {
				got := strings.Join(porcelain(repo), "|")
				if strings.Contains(got, " M ") || strings.Contains(got, " D ") || !strings.Contains(got, "?? u1.txt") || !strings.Contains(got, " A intent.txt") {
					t.Errorf("status %s", got)
				}
				if len(r.api.TrashedFiles()) != 0 {
					t.Errorf("trashed %+v", r.api.TrashedFiles())
				}
			}},
		{"cleanAllUntracked leaves the tracked files", nil, "git.cleanAllUntracked", func(rp *repository) any { return []any{groupArg(rp, groupWorking)} },
			"Are you sure you want to DELETE the 3 untracked files?", "Move to Trash", func(t *testing.T, r *rig, repo *gittest.Repo) {
				if len(r.api.TrashedFiles()) != 1 || strings.Contains(strings.Join(r.api.TrashedFiles()[0].URIs, "|"), "a.txt") {
					t.Errorf("trashed %+v", r.api.TrashedFiles())
				}
				if got := strings.Join(porcelain(repo), "|"); !strings.Contains(got, " D del.txt| M a.txt") {
					t.Errorf("status %s", got)
				}
			}},
		{"cleanAllUntracked from the untracked group", map[string]any{"git.untrackedChanges": "separate"}, "git.cleanAllUntracked", func(rp *repository) any { return []any{groupArg(rp, groupUntracked)} },
			"Are you sure you want to DELETE the 3 untracked files?", "Move to Trash", func(t *testing.T, r *rig, repo *gittest.Repo) {
				if len(r.api.TrashedFiles()) != 1 || len(r.api.TrashedFiles()[0].URIs) != 3 {
					t.Errorf("trashed %+v", r.api.TrashedFiles())
				}
			}},
		{"cleanAllTracked in the separate mode", map[string]any{"git.untrackedChanges": "separate"}, "git.cleanAllTracked", func(rp *repository) any { return []any{groupArg(rp, groupWorking)} },
			"Are you sure you want to discard ALL changes in 7 files?", "Discard All 7 Files", func(t *testing.T, r *rig, repo *gittest.Repo) {
				if got := strings.Join(porcelain(repo), "|"); strings.Contains(got, " M ") {
					t.Errorf("status %s", got)
				}
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp := stageRig(t, rigOptions{settings: tc.settings})
			if err := r.api.Run(tc.cmd, tc.arg(rp)); err != nil {
				t.Fatal(err)
			}
			d := r.theOnlyDialog()
			if d.Spec().Message != tc.message {
				t.Errorf("message %q, want %q", d.Spec().Message, tc.message)
			}
			d.Press(tc.button)
			r.idle(rp)
			tc.check(t, r, repo)
		})
	}
}

func TestDiscardingAnIntentToAddFileDeletesItInsteadOfEmptyingIt(t *testing.T) {
	for _, toTrash := range []bool{false, true} {
		t.Run(map[bool]string{false: "for good", true: "to the trash"}[toTrash], func(t *testing.T) {
			r, repo, rp := stageRig(t, rigOptions{settings: map[string]any{"git.discardUntrackedChangesToTrash": toTrash}})
			if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "intent.txt")); err != nil {
				t.Fatal(err)
			}
			d := r.theOnlyDialog()
			if d.Spec().Message != "Are you sure you want to DELETE the following untracked file: 'intent.txt'?" {
				t.Errorf("%q", d.Spec().Message)
			}
			d.Press(buttons(d)[0])
			r.idle(rp)
			got := strings.Join(porcelain(repo), "|")
			if strings.Contains(got, " A intent.txt") || strings.Contains(got, " D intent.txt") {
				t.Errorf("the index still knows the file:\n%s", got)
			}
			if !toTrash && exists(repo, "intent.txt") {
				t.Error("the file is still there")
			}
		})
	}
}

func TestDiscardingTheFileOfTheEditor(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{})
	r.api.SetActive(&extension.ActiveDocument{URI: fileURI(repo.Path("dir/x.txt"))})
	if err := r.api.Run("git.clean", nil); err != nil {
		t.Fatal(err)
	}
	d := r.theOnlyDialog()
	if d.Spec().Message != "Are you sure you want to discard changes in 'x.txt'?" {
		t.Errorf("%q", d.Spec().Message)
	}
	d.Press("Discard Changes")
	r.idle(rp)
	if got := repo.Read("dir/x.txt"); !strings.HasPrefix(got, "dir/x.txt\none") {
		t.Errorf("x.txt = %q", got)
	}
}

func TestDiscardingBeforeTheFirstCommit(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("fresh")
	repo.Write("a.txt", "a\n")
	r := newRig(t, e, rigOptions{folder: repo.Dir, settings: map[string]any{"git.discardUntrackedChangesToTrash": false}})
	r.settle()
	rp := r.repo(repo.Dir)
	if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "a.txt")); err != nil {
		t.Fatal(err)
	}
	r.theOnlyDialog().Press("Delete File")
	r.idle(rp)
	if exists(repo, "a.txt") {
		t.Error("the file is still there")
	}
}

func TestDiscardingAChangeOfASubmodulePutsItBackOnTheCommitOfTheIndex(t *testing.T) {
	e := gittest.NewEnv(t)
	sub := e.Init("lib")
	sub.Write("lib.go", "package lib\n")
	sub.Commit("lib")
	repo := e.Init("main")
	repo.Write("a.txt", "a\n")
	repo.Commit("a")
	repo.Submodule("vendor/lib", sub)
	inner := &gittest.Repo{Env: e, Dir: repo.Path("vendor/lib")}
	committed := inner.Head()
	inner.Write("more.go", "package lib\n")
	inner.Commit("more")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	if got := r.paths(repo.Dir, groupWorking); !reflect.DeepEqual(got, []string{"vendor/lib"}) {
		t.Fatalf("working = %v", got)
	}
	if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "vendor/lib")); err != nil {
		t.Fatal(err)
	}
	r.theOnlyDialog().Press("Discard Changes")
	r.idle(rp)
	if got := inner.Head(); got != committed {
		t.Errorf("the submodule is at %s, want %s", got, committed)
	}
	if got := porcelain(repo); len(got) != 0 {
		t.Errorf("status %v", got)
	}
}

func TestDiscardingDoesNotRunWhenGitFailsAndTheGroupsComeBack(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{})
	writeFile(t, filepath.Join(repo.Dir, ".git", "index.lock"), "")
	if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "a.txt")); err != nil {
		t.Fatal(err)
	}
	r.theOnlyDialog().Press("Discard Changes")
	r.api.Pump()
	if got := r.paths(repo.Dir, groupWorking); contains(got, "a.txt") {
		t.Errorf("the optimistic update was not applied: %v", got)
	}
	r.idle(rp)
	if got := r.paths(repo.Dir, groupWorking); !contains(got, "a.txt") {
		t.Errorf("the file is not back in Changes: %v", got)
	}
	if notes := r.api.Notes(); len(notes) != 1 || notes[0].Severity != "error" {
		t.Errorf("notes %+v", notes)
	}
}

func TestTheDiffsOfDiscardedFilesCloseWithCloseDiffOnOperation(t *testing.T) {
	r, repo, rp := stageRig(t, rigOptions{settings: map[string]any{"git.closeDiffOnOperation": true}})
	tab := func(rel string) extension.DocumentTab {
		return extension.DocumentTab{ID: "wt:" + rel, Kind: "diff", Original: toGitURI(rp.id, rp.root, repo.Path(rel), "~"), Modified: fileURI(repo.Path(rel))}
	}
	r.api.SetTabs(tab("a.txt"), tab("dir/x.txt"))
	if err := r.api.Run("git.clean", resourceArgs(rp, groupWorking, "a.txt")); err != nil {
		t.Fatal(err)
	}
	r.theOnlyDialog().Press("Discard Changes")
	r.idle(rp)
	if got := r.api.ClosedTabs(); !reflect.DeepEqual(got, []string{"wt:a.txt"}) {
		t.Errorf("closed %v", got)
	}
}
