package gitscm

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// The commands by lines against real repositories (step 24.12): each runs through the registry of the fake API, the way a menu or a key
// runs it, with the changes the host's diff finds, and the test looks at the index, the work tree and the document afterwards.

// linesText is the lines "line 1".."line n" with the numbers in edits replaced, ending with a line break.
func linesText(n int, edits map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if e, ok := edits[i]; ok {
			if e == "\x00" {
				continue // the line is deleted
			}
			b.WriteString(e + "\n")
			continue
		}
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// linesRig has a file of fourteen lines with three changes in the work tree (line 2 modified, a line added after line 6, line 12 deleted),
// open in an editor with its unsaved text equal to the file.
func linesRig(t *testing.T) (r *rig, repo *gittest.Repo, rp *repository, uri, committed, work string) {
	t.Helper()
	e := gittest.NewEnv(t)
	repo = e.Init("lines")
	committed = linesText(14, nil)
	repo.Write("src/f.txt", committed)
	repo.Write("other.txt", "other\n")
	repo.Commit("Add the files")
	work = linesText(14, map[int]string{2: "line 2 changed", 6: "line 6\nline 6.5", 12: "\x00"})
	repo.Write("src/f.txt", work)
	r = newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp = r.repo(repo.Dir)
	uri = fileURI(repo.Path("src/f.txt"))
	r.api.SetDocument(uri, work)
	return r, repo, rp, uri, committed, work
}

// hostChanges are the changes the host finds for the file against the index version.
func hostChanges(t *testing.T, r *rig, original, modified string) []extension.LineChange {
	t.Helper()
	changes, err := r.api.Documents().LineChanges(context.Background(), original, modified)
	if err != nil {
		t.Fatal(err)
	}
	return changes
}

func TestStageChangeStagesOneChangeAndLeavesTheWorkTreeAlone(t *testing.T) {
	for index := 0; index < 3; index++ {
		t.Run(fmt.Sprintf("change %d of 3", index+1), func(t *testing.T) {
			r, repo, rp, uri, committed, work := linesRig(t)
			changes := hostChanges(t, r, committed, work)
			if len(changes) != 3 {
				t.Fatalf("the file has %d changes: %v", len(changes), changes)
			}
			before, _ := os.Stat(repo.Path("src/f.txt"))
			r.run(rp, "git.stageChange", extension.QuickDiffArgs(uri, changes, index))
			if got, want := repo.Git("show", ":src/f.txt")+"\n", modelApply(committed, work, []extension.LineChange{changes[index]}); got != want {
				t.Errorf("the index has %q, want %q", got, want)
			}
			if got := repo.Read("src/f.txt"); got != work {
				t.Errorf("the work tree file changed to %q", got)
			}
			after, _ := os.Stat(repo.Path("src/f.txt"))
			if !after.ModTime().Equal(before.ModTime()) {
				t.Error("the file in the work tree was written")
			}
			// the other two changes are still unstaged
			rest := hostChanges(t, r, repo.Git("show", ":src/f.txt")+"\n", work)
			if len(rest) != 2 {
				t.Errorf("%d changes are left unstaged: %v", len(rest), rest)
			}
			if got := strings.Join(porcelain(repo), "|"); got != "MM src/f.txt" {
				t.Errorf("git status = %q", got)
			}
		})
	}
}

func TestStageChangeOfAStaleChangeIsRefused(t *testing.T) {
	r, repo, rp, uri, committed, work := linesRig(t)
	changes := hostChanges(t, r, committed, work)
	// the text was edited after the peek showed its changes: line 2 changed again
	r.api.SetDocument(uri, strings.Replace(work, "line 2 changed", "line 2 changed again", 1))
	r.run(rp, "git.stageChange", extension.QuickDiffArgs(uri, changes, 0))
	// the change at line 2 is still a change at line 2 with other text; the shape is the same, so it is staged as the file is now
	if got := repo.Git("show", ":src/f.txt") + "\n"; !strings.Contains(got, "line 2 changed again") {
		t.Errorf("a change that has the same lines in the file now is staged as it is: %q", got)
	}
	// text typed above moved the change: it is not where the peek said
	repo.Git("reset", "-q")
	r.api.SetDocument(uri, "a new first line\n"+work)
	r.run(rp, "git.stageChange", extension.QuickDiffArgs(uri, changes, 0))
	if got := repo.Git("diff", "--cached", "--stat"); got != "" {
		t.Errorf("a stale change was staged: %s", got)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "info" || !strings.Contains(notes[0].Message, "does not match the file any more") {
		t.Errorf("notes = %+v", notes)
	}
}

func TestStageChangeOfAFileNotInTheIndexSaysSo(t *testing.T) {
	r, repo, rp, _, _, _ := linesRig(t)
	repo.Write("new.txt", "a\nb\n")
	uri := fileURI(repo.Path("new.txt"))
	r.api.SetDocument(uri, "a\nb\nc\n")
	r.run(rp, "git.stageChange", extension.QuickDiffArgs(uri, []extension.LineChange{lineChange(2, 0, 3, 3)}, 0))
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Message != "The file is not in the index: stage it first." {
		t.Errorf("notes = %+v", notes)
	}
	if got := repo.Git("ls-files", "--", "new.txt"); got != "" {
		t.Errorf("the file was added to the index: %q", got)
	}
}

func TestStageSelectedRangesStagesTheChangesTheSelectionsTouch(t *testing.T) {
	tests := []struct {
		name string
		sels []extension.Selection
		want []int // the indexes of the changes of the file that are staged
		note string
	}{
		{"a cursor in the first change", []extension.Selection{{Anchor: extension.Position{Line: 1}, Active: extension.Position{Line: 1}}}, []int{0}, ""},
		{"a selection over the second", []extension.Selection{{Anchor: extension.Position{Line: 5}, Active: extension.Position{Line: 6, Col: 3}}}, []int{1}, ""},
		{"two cursors", []extension.Selection{{Anchor: extension.Position{Line: 1}, Active: extension.Position{Line: 1}}, {Anchor: extension.Position{Line: 11}, Active: extension.Position{Line: 11}}}, []int{0, 2}, ""},
		{"a selection over everything", []extension.Selection{{Anchor: extension.Position{Line: 0}, Active: extension.Position{Line: 13}}}, []int{0, 1, 2}, ""},
		{"a cursor where nothing changed", []extension.Selection{{Anchor: extension.Position{Line: 9}, Active: extension.Position{Line: 9}}}, nil, "The selection range does not contain any changes."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, repo, rp, uri, committed, work := linesRig(t)
			changes := hostChanges(t, r, committed, work)
			r.api.SetActive(&extension.ActiveDocument{URI: uri, LanguageID: "plaintext", Version: 1, Selections: tc.sels})
			r.run(rp, "git.stageSelectedRanges", nil)
			var want []extension.LineChange
			for _, i := range tc.want {
				want = append(want, changes[i])
			}
			// a selection cuts a block of as many lines on both sides to the lines it touches, so compare with the staged changes' effect
			got := repo.Git("show", ":src/f.txt") + "\n"
			if len(tc.want) == 0 {
				if got != committed {
					t.Errorf("the index changed: %q", got)
				}
				if notes := r.api.Notes(); len(notes) != 1 || notes[0].Message != tc.note {
					t.Errorf("notes = %+v, want %q", notes, tc.note)
				}
				return
			}
			if got == committed {
				t.Fatal("nothing was staged")
			}
			rest := hostChanges(t, r, got, work)
			if len(rest) != 3-len(tc.want) {
				t.Errorf("%d changes are left unstaged (%v), want %d", len(rest), rest, 3-len(tc.want))
			}
			if wantText := modelApply(committed, work, want); wantText != got {
				t.Errorf("the index has %q, want %q", got, wantText)
			}
		})
	}
}

func TestStageSelectedRangesCutsABlockToTheSelectedLines(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("cut")
	committed := linesText(8, nil)
	repo.Write("f.txt", committed)
	repo.Commit("Add the file")
	work := linesText(8, map[int]string{3: "three", 4: "four", 5: "five"})
	repo.Write("f.txt", work)
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	uri := fileURI(repo.Path("f.txt"))
	r.api.SetDocument(uri, work)
	r.api.SetActive(&extension.ActiveDocument{URI: uri, Selections: []extension.Selection{{Anchor: extension.Position{Line: 3}, Active: extension.Position{Line: 3}}}})
	r.run(rp, "git.stageSelectedRanges", nil)
	want := linesText(8, map[int]string{4: "four"})
	if got := repo.Git("show", ":f.txt") + "\n"; got != want {
		t.Errorf("the index has %q, want only line 4 staged: %q", got, want)
	}
	if got := repo.Read("f.txt"); got != work {
		t.Errorf("the file changed to %q", got)
	}
}

func TestRevertChangePutsTheOriginalLinesBackAsOneEdit(t *testing.T) {
	for index := 0; index < 3; index++ {
		t.Run(fmt.Sprintf("change %d of 3", index+1), func(t *testing.T) {
			r, repo, rp, uri, committed, work := linesRig(t)
			changes := hostChanges(t, r, committed, work)
			r.api.SetDirty(uri)
			r.run(rp, "git.revertChange", extension.QuickDiffArgs(uri, changes, index))
			var kept []extension.LineChange
			for i, c := range changes {
				if i != index {
					kept = append(kept, c)
				}
			}
			want := modelApply(committed, work, kept)
			if got, _ := r.api.DocumentText(uri); got != want {
				t.Errorf("the document is %q, want %q", got, want)
			}
			if applied := r.api.Applied(); len(applied) != 1 || len(applied[0].Changes) != 1 || len(applied[0].Changes[0].Edits) != 1 {
				t.Errorf("the revert was %v, want one edit of one document", applied)
			}
			if saved := r.api.SavedAll(); len(saved) != 1 || !slices.Equal(saved[0], []string{uri}) {
				t.Errorf("saved %v, want the document", saved)
			}
			if got := repo.Git("diff", "--cached", "--stat"); got != "" {
				t.Errorf("a revert touched the index: %s", got)
			}
		})
	}
}

func TestRevertSelectedRangesRevertsTheChangesTheSelectionsTouch(t *testing.T) {
	r, _, rp, uri, committed, work := linesRig(t)
	changes := hostChanges(t, r, committed, work)
	r.api.SetActive(&extension.ActiveDocument{URI: uri, Selections: []extension.Selection{
		{Anchor: extension.Position{Line: 1, Col: 2}, Active: extension.Position{Line: 1, Col: 2}},
		{Anchor: extension.Position{Line: 11, Col: 7}, Active: extension.Position{Line: 11, Col: 7}},
	}})
	r.api.SetDirty(uri)
	r.run(rp, "git.revertSelectedRanges", nil)
	if got, _ := r.api.DocumentText(uri); got != modelApply(committed, work, changes[1:2]) {
		t.Errorf("the document is %q", got)
	}
	// a cursor on a line nothing changed reverts nothing
	r.api.SetDocument(uri, work)
	r.api.SetActive(&extension.ActiveDocument{URI: uri, Selections: []extension.Selection{{Anchor: extension.Position{Line: 9}, Active: extension.Position{Line: 9}}}})
	before := len(r.api.Applied())
	r.run(rp, "git.revertSelectedRanges", nil)
	if len(r.api.Applied()) != before {
		t.Error("a selection with no change reverted something")
	}
	if notes := r.api.Notes(); len(notes) != 1 || notes[0].Message != "The selection range does not contain any changes." {
		t.Errorf("notes = %+v", notes)
	}
}

func TestRevertOfATextEditedMeanwhileDoesNothing(t *testing.T) {
	r, _, rp, uri, committed, work := linesRig(t)
	changes := hostChanges(t, r, committed, work)
	if err := r.api.Run("git.revertChange", extension.QuickDiffArgs(uri, changes, 0)); err != nil {
		t.Fatal(err)
	}
	// the user types before the git process has answered
	r.api.SetDocument(uri, "typed\n"+work)
	r.idle(rp)
	if len(r.api.Applied()) != 0 {
		t.Errorf("a revert was applied to a text that changed: %v", r.api.Applied())
	}
	if notes := r.api.Notes(); len(notes) != 1 || !strings.Contains(notes[0].Message, "nothing was reverted") {
		t.Errorf("notes = %+v", notes)
	}
}

func TestUnstageSelectedRangesPutsTheLinesBackToHEADInTheIndex(t *testing.T) {
	r, repo, rp, _, committed, work := linesRig(t)
	repo.Git("add", "src/f.txt")
	r.refresh(rp)
	staged := toGitURI(rp.id, rp.root, repo.Path("src/f.txt"), refIndex)
	r.api.SetDocument(staged, work)
	changes := hostChanges(t, r, committed, work)
	r.api.SetActive(&extension.ActiveDocument{URI: staged, Selections: []extension.Selection{{Anchor: extension.Position{Line: 11}, Active: extension.Position{Line: 11}}}})
	r.run(rp, "git.unstageSelectedRanges", nil)
	want := modelApply(committed, work, []extension.LineChange{changes[0], changes[1]})
	if got := repo.Git("show", ":src/f.txt") + "\n"; got != want {
		t.Errorf("the index has %q, want the deletion unstaged: %q", got, want)
	}
	if got := repo.Read("src/f.txt"); got != work {
		t.Errorf("the file changed to %q", got)
	}
}

func TestUnstageChangeNeedsAFileWithoutUnstagedChanges(t *testing.T) {
	r, repo, rp, uri, committed, work := linesRig(t)
	repo.Git("add", "src/f.txt")
	r.refresh(rp)
	changes := hostChanges(t, r, committed, work)
	r.run(rp, "git.unstageChange", extension.QuickDiffArgs(uri, changes, 1))
	want := modelApply(committed, work, []extension.LineChange{changes[0], changes[2]})
	if got := repo.Git("show", ":src/f.txt") + "\n"; got != want {
		t.Errorf("the index has %q, want the second change unstaged: %q", got, want)
	}
	// the file now has changes that are not staged: the same command is refused
	r.run(rp, "git.unstageChange", extension.QuickDiffArgs(uri, changes, 0))
	if notes := r.api.Notes(); len(notes) != 1 || !strings.Contains(notes[0].Message, "changes that are not staged") {
		t.Errorf("notes = %+v", notes)
	}
	if got := repo.Git("show", ":src/f.txt") + "\n"; got != want {
		t.Errorf("a refused command changed the index to %q", got)
	}
}

func TestDiffStageHunkAndSelectionStageTheTextOfTheToolbar(t *testing.T) {
	r, repo, rp, uri, committed, work := linesRig(t)
	changes := hostChanges(t, r, committed, work)
	text := modelApply(committed, work, changes[:1])
	for _, cmd := range []string{"git.diff.stageHunk", "git.diff.stageSelection"} {
		repo.Git("reset", "-q")
		r.run(rp, cmd, extension.DiffEditorContext{
			OriginalURI: toGitURI(rp.id, rp.root, repo.Path("src/f.txt"), refWorkTree), ModifiedURI: uri, OriginalWithModifiedChanges: text,
		}.Args())
		if got := repo.Git("show", ":src/f.txt") + "\n"; got != text {
			t.Errorf("%s: the index has %q, want %q", cmd, got, text)
		}
	}
	// a modified side that is not a file is left alone
	repo.Git("reset", "-q")
	r.run(rp, "git.diff.stageHunk", extension.DiffEditorContext{OriginalURI: "x", ModifiedURI: "untitled:1", OriginalWithModifiedChanges: "z"}.Args())
	if got := repo.Git("diff", "--cached", "--stat"); got != "" {
		t.Errorf("a diff of an untitled document staged %s", got)
	}
}

func TestDiffStageHunkWithoutArgumentsStagesTheChangeAtTheCursor(t *testing.T) {
	r, repo, rp, uri, committed, work := linesRig(t)
	changes := hostChanges(t, r, committed, work)
	r.api.SetActive(&extension.ActiveDocument{URI: uri, Selections: []extension.Selection{{Anchor: extension.Position{Line: 6, Col: 2}, Active: extension.Position{Line: 6, Col: 2}}}})
	r.run(rp, "git.diff.stageHunk", nil)
	if got, want := repo.Git("show", ":src/f.txt")+"\n", modelApply(committed, work, changes[1:2]); got != want {
		t.Errorf("the index has %q, want %q", got, want)
	}
	repo.Git("reset", "-q")
	r.api.SetActive(&extension.ActiveDocument{URI: uri, Selections: []extension.Selection{{Anchor: extension.Position{Line: 9}, Active: extension.Position{Line: 9}}}})
	r.run(rp, "git.diff.stageHunk", nil)
	if notes := r.api.Notes(); len(notes) != 1 || notes[0].Message != "No hunk found at cursor position." {
		t.Errorf("notes = %+v", notes)
	}
}

func TestStagingKeepsTheLineBreaksOfAFileWithCRLF(t *testing.T) {
	e := gittest.NewEnv(t)
	repo := e.Init("crlf")
	repo.Git("config", "core.autocrlf", "false")
	committed := "one\r\ntwo\r\nthree\r\nfour\r\n"
	repo.Write("f.txt", committed)
	repo.Commit("Add the file")
	// the document holds "\n" line breaks whatever the file has
	work := "ONE\ntwo\nthree\nFOUR\n"
	repo.Write("f.txt", "ONE\r\ntwo\r\nthree\r\nFOUR\r\n")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	rp := r.repo(repo.Dir)
	uri := fileURI(repo.Path("f.txt"))
	r.api.SetDocument(uri, work)
	changes := hostChanges(t, r, committed, work)
	if len(changes) != 2 {
		t.Fatalf("changes = %v", changes)
	}
	r.run(rp, "git.stageChange", extension.QuickDiffArgs(uri, changes, 1))
	if got := repo.Git("show", ":f.txt"); got != "one\r\ntwo\r\nthree\r\nFOUR\r" { // the harness trims the last line break
		t.Errorf("the index has %q", got)
	}
	// the revert gives the document "\n" line breaks too
	r.api.SetDirty(uri)
	r.run(rp, "git.revertChange", extension.QuickDiffArgs(uri, changes, 0))
	if got, _ := r.api.DocumentText(uri); got != "one\ntwo\nthree\nFOUR\n" {
		t.Errorf("the document is %q", got)
	}
}

func TestStagingRefusesWhatItCannotDo(t *testing.T) {
	t.Run("an untrusted or unknown file is left alone", func(t *testing.T) {
		r, repo, rp, _, committed, work := linesRig(t)
		changes := hostChanges(t, r, committed, work)
		outside := "file:///tmp/emetos-not-a-repository-24-12/a.txt"
		r.api.SetDocument(outside, work)
		for _, cmd := range []string{"git.stageChange", "git.revertChange", "git.unstageChange"} {
			r.run(rp, cmd, extension.QuickDiffArgs(outside, changes, 0))
		}
		if len(r.api.Applied()) != 0 || len(r.api.Notes()) != 0 || repo.Git("diff", "--cached", "--stat") != "" {
			t.Errorf("something happened: %v %v", r.api.Applied(), r.api.Notes())
		}
	})
	t.Run("arguments of another shape", func(t *testing.T) {
		r, _, rp, _, _, _ := linesRig(t)
		for _, args := range []any{nil, "x", []any{"a"}, []any{"file:///a", "no", 0}, []any{"file:///a", []any{}, 0}, []any{"file:///a", []any{map[string]any{"x": 1.0}}, 0}} {
			for _, cmd := range []string{"git.stageChange", "git.revertChange", "git.unstageChange"} {
				r.run(rp, cmd, args)
			}
		}
		if len(r.api.Applied()) != 0 || len(r.api.Notes()) != 0 {
			t.Errorf("something happened: %v %v", r.api.Applied(), r.api.Notes())
		}
	})
	t.Run("no editor in front", func(t *testing.T) {
		r, _, rp, _, _, _ := linesRig(t)
		for _, cmd := range []string{"git.stageSelectedRanges", "git.revertSelectedRanges", "git.unstageSelectedRanges", "git.diff.stageHunk", "git.diff.stageSelection"} {
			r.run(rp, cmd, nil)
		}
		r.api.SetActive(&extension.ActiveDocument{URI: "untitled:1"})
		for _, cmd := range []string{"git.stageSelectedRanges", "git.revertSelectedRanges", "git.unstageSelectedRanges", "git.diff.stageHunk"} {
			r.run(rp, cmd, nil)
		}
		if len(r.api.Applied()) != 0 || len(r.api.Notes()) != 0 {
			t.Errorf("something happened: %v %v", r.api.Applied(), r.api.Notes())
		}
	})
}

// routeOf asserts the error is an *errors.Error whose route has the part.
func routeOf(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestTheWaysAStagingCommandIsRefusedHaveTheirRoutes(t *testing.T) {
	changes := []extension.LineChange{lineChange(2, 2, 2, 2), lineChange(5, 0, 6, 6)}
	doc := newTextDoc("a\nb\nc\nd\ne\nf\n")
	_, err := pickChange(changes, lineChange(9, 9, 9, 9))
	routeOf(t, err, "pickChange.GitStageStale")
	if got, err := pickChange(changes, changes[1]); err != nil || !slices.Equal(got, changes[1:]) {
		t.Errorf("pickChange of a change the file has = %v, %v", got, err)
	}
	_, err = pickSelected(changes, doc, []extension.Selection{{Anchor: extension.Position{Line: 3}, Active: extension.Position{Line: 3}}})
	routeOf(t, err, "pickSelected.GitStageNoChanges")
	_, err = pickHunkAt(changes, doc, textPos{line: 3})
	routeOf(t, err, "pickHunkAt.GitStageNoHunk")
	if got, err := pickHunkAt(changes, doc, textPos{line: 1, col: 0}); err != nil || got[0] != changes[0] {
		t.Errorf("pickHunkAt on a change = %v, %v", got, err)
	}
	_, err = keepAllBut(changes, lineChange(9, 9, 9, 9))
	routeOf(t, err, "keepAllBut.GitStageStale")
	if got, err := keepAllBut(changes, changes[0]); err != nil || !slices.Equal(got, changes[1:]) {
		t.Errorf("keepAllBut = %v, %v", got, err)
	}
	_, err = keepUntouched(changes, doc, []extension.Selection{{Anchor: extension.Position{Line: 3}, Active: extension.Position{Line: 3}}})
	routeOf(t, err, "keepUntouched.GitStageNoChanges")
	routeOf(t, requireStaged("f.txt", changes), "requireStaged.GitStageUnstageBlocked")
	if err := requireStaged("f.txt", nil); err != nil {
		t.Errorf("requireStaged with nothing unstaged = %v", err)
	}

	e := gittest.NewEnv(t)
	repo := e.Init("route")
	repo.Write("a.txt", "a\n")
	repo.Commit("Add a file")
	repo.Write("u.txt", "u\n")
	r := newRig(t, e, rigOptions{folder: repo.Dir})
	r.settle()
	g := r.repo(repo.Dir).repo
	if text, err := indexVersion(context.Background(), g, "a.txt"); err != nil || text != "a\n" {
		t.Errorf("indexVersion of a tracked file = %q, %v", text, err)
	}
	_, err = indexVersion(context.Background(), g, "u.txt")
	routeOf(t, err, "indexVersion.GitStageNotInIndex")
}

func TestEveryStagingMessageIsSaidInWords(t *testing.T) {
	for _, tag := range []string{"GitStageStale", "GitStageNoChanges", "GitStageNotInIndex", "GitStageUnstageBlocked", "GitStageNoHunk"} {
		if msg, ok := stageRefused(&errors.Error{Tag: tag}); !ok || msg == "" {
			t.Errorf("%s has no message", tag)
		}
	}
	if _, ok := stageRefused(&errors.Error{Tag: "GitSomethingElse"}); ok {
		t.Error("another failure is explained as a refusal")
	}
}
