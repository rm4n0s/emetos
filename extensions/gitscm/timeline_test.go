package gitscm

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

// The Timeline of a file against real repositories (step 24.21).

// timelinePage asks the provider for a page of the timeline of a file, as the view does.
func (r *rig) timelinePage(path string, o extension.TimelineOptions) extension.Timeline {
	r.t.Helper()
	r.api.WaitFor("the timeline provider", func() bool { r.api.Pump(); return r.api.TimelineProvider(TimelineProviderID) != nil })
	f := r.api.TimelineProvider(TimelineProviderID)
	var tl extension.Timeline
	r.api.WaitFor("the repository for the timeline", func() bool {
		r.api.Pump()
		return r.x.tl.forPath(path) != nil
	})
	tl, err := f.Page(context.Background(), "file://"+path, o)
	if err != nil {
		r.t.Fatalf("Page(%s): %v", path, err)
	}
	return tl
}

func labelsOf(tl extension.Timeline) []string {
	var out []string
	for _, it := range tl.Items {
		out = append(out, it.Label)
	}
	return out
}

// itemArgs is the argument the view gives a command of an item.
func itemArgs(it extension.TimelineItem, uri string) []any {
	return []any{map[string]any{"source": TimelineProviderID, "id": it.ID, "uri": uri, "contextValue": it.ContextValue, "label": it.Label, "args": it.Args}}
}

// historyRepo is a repository with a file that was edited, renamed and edited, by two authors.
func historyRepo(t *testing.T, e *gittest.Env) *gittest.Repo {
	t.Helper()
	r := e.Init("history")
	body := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n"
	r.Write("old.txt", body)
	r.Commit("Add old.txt")
	r.Write("old.txt", strings.Replace(body, "two", "TWO", 1))
	r.CommitAs("Grace Hopper", "grace@example.com", "Edit old.txt\n\nWith a body.", "-a")
	r.Git("mv", "old.txt", "new.txt")
	r.Commit("Rename old.txt to new.txt")
	r.Write("new.txt", strings.Replace(body, "two", "TWO", 1)+"nine\n")
	r.Commit("Edit new.txt", "new.txt")
	return r
}

func TestTheTimelineOfAFilePagesAcrossARenameAndComparesWithThePreviousCommit(t *testing.T) {
	e := gittest.NewEnv(t)
	hr := historyRepo(t, e)
	r := newRig(t, e, rigOptions{})
	r.settle()
	file := hr.Path("new.txt")
	empty := strings.TrimSpace(hr.Git("hash-object", "-t", "tree", "--stdin"))
	hashes := strings.Fields(hr.Git("log", "--format=%H"))

	first := r.timelinePage(file, extension.TimelineOptions{Limit: 2})
	if got, want := labelsOf(first), []string{"Edit new.txt", "Rename old.txt to new.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first page = %v, want %v", got, want)
	}
	if first.Paging.Cursor == "" {
		t.Fatal("the first page has no cursor")
	}
	second := r.timelinePage(file, extension.TimelineOptions{Limit: 2, Cursor: first.Paging.Cursor})
	if got, want := labelsOf(second), []string{"Edit old.txt", "Add old.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second page, across the rename = %v, want %v", got, want)
	}
	if second.Paging.Cursor != "" {
		t.Errorf("the last page has the cursor %q", second.Paging.Cursor)
	}
	all := r.timelinePage(file, extension.TimelineOptions{})
	if len(all.Items) != 4 || all.Paging.Cursor != "" {
		t.Fatalf("all of it = %v (cursor %q)", labelsOf(all), all.Paging.Cursor)
	}
	for i, it := range all.Items {
		d, ok := timelineDataOf(it.Args)
		if !ok || it.ID != hashes[i] || d.Ref != hashes[i] || it.ContextValue != "git:file:commit" || it.Icon != "git-commit" || it.Command == nil || it.Command.ID != "git.timeline.openDiff" {
			t.Errorf("item %d = %+v (%+v)", i, it, d)
		}
		// each item is compared with the commit before it in the history of the file, also the last of a page
		wantPrev := empty
		if i+1 < len(hashes) {
			wantPrev = hashes[i+1]
		}
		if d.PreviousRef != wantPrev {
			t.Errorf("item %d is compared with %s, want %s", i, d.PreviousRef, wantPrev)
		}
	}
	// the page that ends at the boundary is compared with the commit the next page starts with
	if d, _ := timelineDataOf(first.Items[1].Args); d.PreviousRef != hashes[2] {
		t.Errorf("the last item of the first page is compared with %s, want %s", d.PreviousRef, hashes[2])
	}
	// the paths the file had: the new name after the rename, the old one before
	var paths []string
	for _, it := range all.Items {
		d, _ := timelineDataOf(it.Args)
		paths = append(paths, filepath.Base(d.Path)+">"+filepath.Base(d.PreviousPath))
	}
	if want := []string{"new.txt>new.txt", "new.txt>old.txt", "old.txt>old.txt", "old.txt>old.txt"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	// the author is the description, the date the committer's, the tooltip the commit
	if all.Items[1].Description != "Ada Example" || all.Items[2].Description != "Grace Hopper" {
		t.Errorf("descriptions = %q %q", all.Items[1].Description, all.Items[2].Description)
	}
	for _, want := range []string{"Grace Hopper", "grace@example.com", "Edit old.txt", "With a body.", "1 file changed"} {
		if !strings.Contains(all.Items[2].Detail, want) {
			t.Errorf("the tooltip lacks %q: %q", want, all.Items[2].Detail)
		}
	}
	for i := 1; i < len(all.Items); i++ {
		if !all.Items[i].Timestamp.Before(all.Items[i-1].Timestamp) {
			t.Errorf("item %d is not older than item %d", i, i-1)
		}
	}
}

func TestTheTimelineOfAFileThatIsNotTrackedAFolderOrAFileOutsideARepositoryIsEmpty(t *testing.T) {
	e := gittest.NewEnv(t)
	hr := historyRepo(t, e)
	hr.Write("untracked.txt", "x\n")
	if err := os.MkdirAll(hr.Path("dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, e, rigOptions{})
	r.settle()
	r.timelinePage(hr.Path("new.txt"), extension.TimelineOptions{Limit: 1}) // the provider and the repository are there
	for name, p := range map[string]string{
		"untracked": hr.Path("untracked.txt"), "a folder": hr.Path("dir"), "outside": filepath.Join(e.Root, "elsewhere.txt"), "not there": hr.Path("gone.txt"),
	} {
		tl, err := r.api.TimelineProvider(TimelineProviderID).Page(context.Background(), "file://"+p, extension.TimelineOptions{Limit: 5})
		if err != nil || len(tl.Items) != 0 {
			t.Errorf("%s: %v, %v", name, labelsOf(tl), err)
		}
	}
	// another scheme has no history
	tl, err := r.api.TimelineProvider(TimelineProviderID).Page(context.Background(), "untitled:Untitled-1", extension.TimelineOptions{})
	if err != nil || len(tl.Items) != 0 {
		t.Errorf("untitled: %v, %v", labelsOf(tl), err)
	}
}

func TestTheStagedAndUncommittedChangesAreTheFirstItems(t *testing.T) {
	for _, showUncommitted := range []bool{false, true} {
		name := "git.timeline.showUncommitted off"
		if showUncommitted {
			name = "git.timeline.showUncommitted on"
		}
		t.Run(name, func(t *testing.T) {
			e := gittest.NewEnv(t)
			hr := historyRepo(t, e)
			file := hr.Path("new.txt")
			past := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
			hr.Write("new.txt", "staged\n")
			hr.Git("add", "new.txt")
			hr.Write("new.txt", "staged\nand then changed\n")
			if err := os.Chtimes(filepath.Join(hr.Dir, ".git", "index"), past, past); err != nil {
				t.Fatal(err)
			}
			r := newRig(t, e, rigOptions{settings: map[string]any{"git.timeline.showUncommitted": showUncommitted}})
			r.settle()
			tl := r.timelinePage(file, extension.TimelineOptions{Limit: 10})
			want := []string{"Staged Changes", "Edit new.txt", "Rename old.txt to new.txt", "Edit old.txt", "Add old.txt"}
			if showUncommitted {
				want = append([]string{"Uncommitted Changes"}, want...)
			}
			if got := labelsOf(tl); !reflect.DeepEqual(got, want) {
				t.Fatalf("items = %v, want %v", got, want)
			}
			staged := tl.Items[len(tl.Items)-5]
			if staged.ID != "index" || staged.ContextValue != "git:file:index" || !staged.Timestamp.Equal(past) {
				t.Errorf("the staged item = %+v", staged)
			}
			d, _ := timelineDataOf(staged.Args)
			if d.Ref != "~" || d.PreviousRef != "HEAD" {
				t.Errorf("staged: %+v", d)
			}
			if !strings.Contains(staged.Detail, "Index Modified") {
				t.Errorf("staged tooltip = %q", staged.Detail)
			}
			if showUncommitted {
				u := tl.Items[0]
				d, _ := timelineDataOf(u.Args)
				if u.ID != "working" || u.ContextValue != "git:file:working" || u.Icon != "circle-outline" || d.Ref != "" || d.PreviousRef != "~" {
					t.Errorf("the uncommitted item = %+v (%+v)", u, d)
				}
			}
		})
	}
}

func TestOnlyTheFirstPageHasTheChanges(t *testing.T) {
	e := gittest.NewEnv(t)
	hr := historyRepo(t, e)
	hr.Write("new.txt", "staged\n")
	hr.Git("add", "new.txt")
	r := newRig(t, e, rigOptions{})
	r.settle()
	first := r.timelinePage(hr.Path("new.txt"), extension.TimelineOptions{Limit: 1})
	if got := labelsOf(first); !reflect.DeepEqual(got, []string{"Staged Changes", "Edit new.txt"}) || first.Paging.Cursor == "" {
		t.Fatalf("first page = %v (cursor %q)", got, first.Paging.Cursor)
	}
	next := r.timelinePage(hr.Path("new.txt"), extension.TimelineOptions{Limit: 1, Cursor: first.Paging.Cursor})
	if got := labelsOf(next); !reflect.DeepEqual(got, []string{"Rename old.txt to new.txt"}) {
		t.Errorf("next page = %v", got)
	}
}

func TestTheTimelineUsesTheAuthorDateAndHidesTheAuthorOnRequest(t *testing.T) {
	e := gittest.NewEnv(t)
	hr := e.Init("dates")
	hr.Write("a.txt", "one\n")
	hr.Commit("First")
	hr.Write("a.txt", "two\n")
	e.With("GIT_COMMITTER_DATE=2024-01-01T12:00:00Z", "GIT_AUTHOR_DATE=2001-02-03T04:05:06Z", "GIT_AUTHOR_NAME=Grace Hopper", "GIT_AUTHOR_EMAIL=grace@example.com").Git(hr.Dir, "commit", "-q", "-a", "-m", "Second")
	for _, tt := range []struct {
		name     string
		settings map[string]any
		author   bool
		hasName  bool
	}{
		{"the committer date and the author", nil, false, true},
		{"the author date, without the author", map[string]any{"git.timeline.date": "authored", "git.timeline.showAuthor": false}, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, e, rigOptions{settings: tt.settings})
			r.settle()
			tl := r.timelinePage(hr.Path("a.txt"), extension.TimelineOptions{})
			if len(tl.Items) != 2 {
				t.Fatalf("items = %v", labelsOf(tl))
			}
			second := tl.Items[0]
			for _, it := range tl.Items {
				if it.Label == "Second" {
					second = it
				}
			}
			isAuthorDate := second.Timestamp.Equal(time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC))
			if isAuthorDate != tt.author {
				t.Errorf("timestamp %v, author date %v", second.Timestamp, tt.author)
			}
			if (second.Description == "Grace Hopper") != tt.hasName || (!tt.hasName && second.Description != "") {
				t.Errorf("description = %q", second.Description)
			}
		})
	}
}

func TestTheTimelineOfAFileChangedOnTwoBranchesListsBothSidesAndTheMerge(t *testing.T) {
	e := gittest.NewEnv(t)
	hr := e.Init("merged")
	hr.Write("a.txt", "one\ntwo\nthree\n")
	hr.Commit("Add a.txt")
	trunk := strings.TrimSpace(hr.Git("rev-parse", "--abbrev-ref", "HEAD"))
	hr.Branch("side", "")
	hr.Checkout("side")
	hr.Write("a.txt", "one\ntwo\nthree\nside\n")
	hr.Commit("Side edits a.txt", "a.txt")
	hr.Checkout(trunk)
	hr.Write("a.txt", "main\none\ntwo\nthree\n")
	hr.Commit("Main edits a.txt", "a.txt")
	if err := hr.Merge("side"); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, e, rigOptions{})
	r.settle()
	tl := r.timelinePage(hr.Path("a.txt"), extension.TimelineOptions{})
	got := labelsOf(tl)
	if len(got) != 4 || !strings.HasPrefix(got[0], "Merge") {
		t.Fatalf("items = %v", got)
	}
	d, _ := timelineDataOf(tl.Items[0].Args)
	if filepath.Base(d.Path) != "a.txt" {
		t.Errorf("the merge has the path %q", d.Path)
	}
}

func TestTheTimelineCommandsOpenTheVersionsOfTheFile(t *testing.T) {
	e := gittest.NewEnv(t)
	hr := historyRepo(t, e)
	file := hr.Path("new.txt")
	hr.Write("new.txt", "staged\n")
	hr.Git("add", "new.txt")
	hr.Write("new.txt", "staged\nmore\n")
	r := newRig(t, e, rigOptions{settings: map[string]any{"git.timeline.showUncommitted": true, "git.commitShortHashLength": 7}})
	r.settle()
	rp := r.repo(hr.Dir)
	tl := r.timelinePage(file, extension.TimelineOptions{})
	uri := "file://" + file
	hashes := strings.Fields(hr.Git("log", "--format=%H"))
	short := func(i int) string { return hashes[i][:7] }
	byLabel := map[string]extension.TimelineItem{}
	for _, it := range tl.Items {
		byLabel[it.Label] = it
	}
	git := func(ref, rel string) string { return toGitURI(rp.id, rp.root, filepath.Join(rp.root, rel), ref) }

	run := func(id string, it extension.TimelineItem) {
		t.Helper()
		if err := r.api.Run(id, itemArgs(it, uri)); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	tests := []struct {
		label string
		left  string
		right string
		title string
	}{
		{"Uncommitted Changes", git("~", "new.txt"), "file://" + file, "new.txt (Working Tree)"},
		{"Staged Changes", git("HEAD", "new.txt"), git("~", "new.txt"), "new.txt (Index)"},
		{"Edit new.txt", git(hashes[1], "new.txt"), git(hashes[0], "new.txt"), "new.txt (" + short(1) + ") ↔ new.txt (" + short(0) + ")"},
		// across the rename the left side is the file under its old name
		{"Rename old.txt to new.txt", git(hashes[2], "old.txt"), git(hashes[1], "new.txt"), "new.txt (" + short(2) + ") ↔ new.txt (" + short(1) + ")"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			before := len(r.api.OpenedDiffs())
			run("git.timeline.openDiff", byLabel[tt.label])
			diffs := r.api.OpenedDiffs()
			if len(diffs) != before+1 {
				t.Fatalf("%d diffs opened", len(diffs)-before)
			}
			got := diffs[len(diffs)-1]
			if got.Left != tt.left || got.Right != tt.right || got.Title != tt.title || !got.Preview || !got.PreserveFocus {
				t.Errorf("diff = %+v\nwant %s > %s %q", got, tt.left, tt.right, tt.title)
			}
		})
	}
	// the first commit is compared with the empty tree, which has no file
	empty := strings.TrimSpace(hr.Git("hash-object", "-t", "tree", "--stdin"))
	run("git.timeline.openDiff", byLabel["Add old.txt"])
	if d := r.api.OpenedDiffs(); d[len(d)-1].Left != git(empty, "old.txt") || d[len(d)-1].Right != git(hashes[3], "old.txt") {
		t.Errorf("root diff = %+v", d[len(d)-1])
	}
	if text, err := r.contentOf(rp, empty, "old.txt"); err != nil || text != "" {
		t.Errorf("the empty tree has the text %q (%v)", text, err)
	}

	// Open Commit opens the commit with the file revealed
	run("git.timeline.viewCommit", byLabel["Edit new.txt"])
	r.settleUntil("the commit", func() bool { return len(r.api.OpenedMultiDiffs()) == 1 })
	spec := r.api.OpenedMultiDiffs()[0]
	if !strings.HasPrefix(spec.Title, short(0)+" - Edit new.txt") || spec.Reveal != git(hashes[0], "new.txt") {
		t.Errorf("commit = %+v", spec)
	}

	// Copy Commit Hash and Copy Commit Message
	run("git.timeline.copyCommitId", byLabel["Edit old.txt"])
	run("git.timeline.copyCommitMessage", byLabel["Edit old.txt"])
	if got := r.api.ClipboardTexts(); !reflect.DeepEqual(got, []string{hashes[2], "Edit old.txt\n\nWith a body."}) {
		t.Errorf("clipboard = %q", got)
	}
	// they are for commits only
	run("git.timeline.copyCommitId", byLabel["Staged Changes"])
	if n := len(r.api.ClipboardTexts()); n != 2 {
		t.Errorf("a staged item was copied: %d texts", n)
	}
}

func TestSelectForCompareAndCompareWithSelected(t *testing.T) {
	e := gittest.NewEnv(t)
	hr := historyRepo(t, e)
	file := hr.Path("new.txt")
	hr.Write("new.txt", "staged\n")
	hr.Git("add", "new.txt")
	r := newRig(t, e, rigOptions{})
	r.settle()
	rp := r.repo(hr.Dir)
	tl := r.timelinePage(file, extension.TimelineOptions{})
	uri := "file://" + file
	hashes := strings.Fields(hr.Git("log", "--format=%H"))
	byLabel := map[string]extension.TimelineItem{}
	for _, it := range tl.Items {
		byLabel[it.Label] = it
	}
	if got := r.api.ContextKey("git.timeline.selectedForCompare"); got == true {
		t.Fatalf("selectedForCompare = %v before a selection", got)
	}
	// without a selection Compare with Selected does nothing
	if err := r.api.Run("git.timeline.compareWithSelected", itemArgs(byLabel["Edit new.txt"], uri)); err != nil || len(r.api.OpenedDiffs()) != 0 {
		t.Fatalf("compare without a selection: %v, %d diffs", err, len(r.api.OpenedDiffs()))
	}
	if err := r.api.Run("git.timeline.selectForCompare", itemArgs(byLabel["Add old.txt"], uri)); err != nil {
		t.Fatal(err)
	}
	if got := r.api.ContextKey("git.timeline.selectedForCompare"); got != true {
		t.Errorf("selectedForCompare = %v", got)
	}
	// another file's timeline does not compare with it
	if err := r.api.Run("git.timeline.compareWithSelected", itemArgs(byLabel["Edit new.txt"], "file://"+hr.Path("other.txt"))); err != nil || len(r.api.OpenedDiffs()) != 0 {
		t.Fatalf("compare with another file: %v, %d diffs", err, len(r.api.OpenedDiffs()))
	}
	for _, tt := range []struct {
		with  string
		right string
		title string
	}{
		{"Edit new.txt", toGitURI(rp.id, rp.root, hr.Path("new.txt"), hashes[0]), "new.txt (" + hashes[3][:7] + ") ↔ new.txt (" + hashes[0][:7] + ")"},
		{"Staged Changes", toGitURI(rp.id, rp.root, hr.Path("new.txt"), "~"), "new.txt (" + hashes[3][:7] + ") ↔ new.txt (Index)"},
	} {
		if err := r.api.Run("git.timeline.compareWithSelected", itemArgs(byLabel[tt.with], uri)); err != nil {
			t.Fatal(err)
		}
		diffs := r.api.OpenedDiffs()
		got := diffs[len(diffs)-1]
		if got.Left != toGitURI(rp.id, rp.root, hr.Path("old.txt"), hashes[3]) || got.Right != tt.right || got.Title != tt.title {
			t.Errorf("compare with %s: %+v", tt.with, got)
		}
	}
}

func TestTheTimelineIsToldWhenTheHistoryChanges(t *testing.T) {
	e := gittest.NewEnv(t)
	hr := historyRepo(t, e)
	r := newRig(t, e, rigOptions{})
	r.settle()
	r.timelinePage(hr.Path("new.txt"), extension.TimelineOptions{Limit: 1})
	reg := r.api.TimelineProvider(TimelineProviderID)
	r.clock.Advance(timelineDebounce)
	r.api.Pump()
	before := len(reg.Changes())
	// a commit moves HEAD: the Timeline is told once, after the pause
	hr.Write("new.txt", "changed\n")
	hr.Commit("Another edit", "new.txt")
	rp := r.repo(hr.Dir)
	r.refresh(rp)
	r.settle()
	if got := len(reg.Changes()); got != before {
		t.Fatalf("told before the pause: %d", got-before)
	}
	r.clock.Advance(timelineDebounce)
	r.api.Pump()
	if got := reg.Changes(); len(got) != before+1 || len(got[before]) != 0 {
		t.Errorf("changes = %v", got)
	}
	// a status that changes nothing about the history does not tell it
	r.refresh(rp)
	r.settle()
	r.clock.Advance(timelineDebounce)
	r.api.Pump()
	if got := len(reg.Changes()); got != before+1 {
		t.Errorf("an unchanged status told the Timeline: %d", got-before)
	}
	// staging the file changes the first page
	hr.Write("new.txt", "staged\n")
	hr.Git("add", "new.txt")
	r.refresh(rp)
	r.settle()
	r.clock.Advance(timelineDebounce)
	r.api.Pump()
	if got := len(reg.Changes()); got != before+2 {
		t.Errorf("staging did not tell the Timeline: %d", got-before)
	}
}

func TestThereIsNoTimelineProviderUntilARepositoryIsOpenAndNoneWithoutAView(t *testing.T) {
	e := gittest.NewEnv(t)
	folder := filepath.Join(e.Root, "no-repos")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, e, rigOptions{folder: folder})
	r.settle()
	if r.api.TimelineProvider(TimelineProviderID) != nil {
		t.Error("a provider with no repository")
	}
	e2 := gittest.NewEnv(t)
	historyRepo(t, e2)
	hostless := newRig(t, e2, rigOptions{noStart: true})
	hostless.api.SetTimelineUnavailable(true)
	if err := hostless.x.Activate(context.Background(), hostless.api); err != nil {
		t.Fatal(err)
	}
	hostless.settle()
	if hostless.api.TimelineProvider(TimelineProviderID) != nil {
		t.Error("a provider in a host with no Timeline view")
	}
}

func TestTheTimelineMenusAndCommands(t *testing.T) {
	m := (&gitscm{}).Manifest()
	var rows []string
	for _, it := range m.Contributes.Menus {
		if it.Menu == extension.MenuTimelineItemContext {
			rows = append(rows, it.Group+" "+it.Command)
		}
	}
	want := []string{"inline git.timeline.viewCommit", "1_actions git.timeline.openDiff", "1_actions git.timeline.viewCommit", "3_compare git.timeline.compareWithSelected",
		"3_compare git.timeline.selectForCompare", "5_copy git.timeline.copyCommitId", "5_copy git.timeline.copyCommitMessage"}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %v, want %v", rows, want)
	}
}
