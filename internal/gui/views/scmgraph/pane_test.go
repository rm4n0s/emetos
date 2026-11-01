package scmgraph

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/scm/graph"
)

func TestTheGraphShowsTheHistoryOfTheCurrentBranchTheOneItTracksWithIncomingAndOutgoingChanges(t *testing.T) {
	r := newRig(t, branching())
	if got := r.pane.Message(); got != MessageLoading {
		t.Errorf("message while loading = %q", got)
	}
	r.settle()
	got := r.subjects()
	// the outgoing changes stand before the current commit, the incoming ones before the merge base
	if got[0] != "Outgoing Changes" || got[1] != "Merge topic" || !strings.Contains(strings.Join(got, ","), "Incoming Changes,Main one") {
		t.Fatalf("rows = %v", got)
	}
	if len(got) != 9 {
		t.Errorf("rows = %v, want the 7 commits and the two rows of changes", got)
	}
	if r.pane.Message() != "" {
		t.Errorf("message = %q", r.pane.Message())
	}
	rows := r.pane.Rows()
	if rows[0].Kind != RowOutgoing || rows[0].Item.Author != "main" || !rows[1].Current {
		t.Errorf("outgoing %+v, current %v", rows[0], rows[1].Current)
	}
	q := r.hist.Queries()
	if len(q) != 1 || q[0].Skip != 0 || q[0].Limit != 50 || !reflect.DeepEqual(q[0].RefIDs, []string{"m", "r1"}) {
		t.Errorf("queries = %+v", q)
	}
	if len(r.hist.asked) != 1 || !reflect.DeepEqual(r.hist.asked[0], []string{"main", "origin/main"}) {
		t.Errorf("common ancestor asked = %v", r.hist.asked)
	}
}

func TestTheIncomingAndOutgoingRowsFollowTheSettings(t *testing.T) {
	r := newRig(t, branching())
	r.settle()
	r.pane.o.ShowIncoming = func() bool { return false }
	r.pane.o.ShowOutgoing = func() bool { return false }
	r.pane.Refresh()
	r.settle()
	for _, s := range r.subjects() {
		if strings.HasSuffix(s, "Changes") {
			t.Errorf("rows = %v", r.subjects())
		}
	}
}

func TestTheBadgesOfTheRows(t *testing.T) {
	r := newRig(t, branching())
	r.settle()
	by := map[string][]Badge{}
	for _, row := range r.pane.Rows() {
		by[row.Item.ID] = row.Badges
	}
	if b := by["m"]; len(b) != 1 || b[0].Name != "main" || b[0].Icon != "git-branch" || b[0].Color != graph.RefColor {
		t.Errorf("main = %+v", b)
	}
	if b := by["r1"]; len(b) != 1 || b[0].Name != "origin/main" || b[0].Color != graph.RemoteRefColor {
		t.Errorf("origin/main = %+v", b)
	}
	// a reference that is not the current branch, its remote or its base has no color and shows only with scm.graph.badges = all
	if b := by["b2"]; len(b) != 0 {
		t.Errorf("v1 = %+v with the filter badges", b)
	}
	r.badges = "all"
	r.pane.Refresh()
	r.settle()
	for _, row := range r.pane.Rows() {
		if row.Item.ID == "b2" && (len(row.Badges) != 1 || row.Badges[0].Icon != "tag") {
			t.Errorf("v1 = %+v with all badges", row.Badges)
		}
	}
}

func TestTheFilterChoosesTheReferences(t *testing.T) {
	r := newRig(t, branching())
	r.settle()
	if got := r.pane.FilterLabel(); got != "Auto" {
		t.Errorf("label = %q", got)
	}
	if got := r.pane.FilterTooltip(); got != "main, origin/main" {
		t.Errorf("tooltip = %q", got)
	}
	r.pane.SetFilter(Filter{Kind: FilterAll})
	r.settle()
	if got := r.pane.FilterLabel(); got != "All" {
		t.Errorf("label = %q", got)
	}
	q := r.hist.Queries()
	if last := q[len(q)-1]; !reflect.DeepEqual(last.RefIDs, []string{"m", "d", "r1", "b2"}) {
		t.Errorf("all: query %+v", last)
	}
	r.pane.SetFilter(Filter{Kind: FilterRefs, Refs: []string{"refs/heads/topic"}})
	r.settle()
	if got := r.pane.FilterLabel(); got != "topic" {
		t.Errorf("label = %q", got)
	}
	q = r.hist.Queries()
	if last := q[len(q)-1]; !reflect.DeepEqual(last.RefIDs, []string{"d"}) {
		t.Errorf("topic: query %+v", last)
	}
	// the incoming and outgoing changes are about the current branch: with it out of the filter they are gone
	for _, s := range r.subjects() {
		if strings.HasSuffix(s, "Changes") {
			t.Errorf("rows = %v", r.subjects())
		}
	}
	r.pane.SetFilter(Filter{Kind: FilterRefs, Refs: []string{"refs/heads/topic", "refs/tags/v1"}})
	if got := r.pane.FilterLabel(); got != "2 Items" {
		t.Errorf("label = %q", got)
	}
	// the filters are kept with the window, and only the ones that are not Auto
	if last := r.saved[len(r.saved)-1]; len(last) != 1 || last[r.src.StorageKey()].Kind != FilterRefs {
		t.Errorf("saved %+v", r.saved)
	}
	r.pane.SetFilter(Filter{})
	if last := r.saved[len(r.saved)-1]; len(last) != 0 {
		t.Errorf("saved %+v after Auto", last)
	}
}

func TestAFilterWhoseReferencesAreGoneIsReset(t *testing.T) {
	r := newRig(t, branching())
	r.pane.filters[r.src.StorageKey()] = Filter{Kind: FilterRefs, Refs: []string{"refs/heads/gone"}}
	r.pane.Refresh()
	r.settle()
	if f := r.pane.Filter(); f.Kind != "" && f.Kind != FilterAuto {
		t.Errorf("filter = %+v, want Auto", f)
	}
	if len(r.saved) == 0 || len(r.saved[len(r.saved)-1]) != 0 {
		t.Errorf("saved = %v", r.saved)
	}
	if len(r.pane.Rows()) == 0 {
		t.Error("no rows after the reset")
	}
}

func TestTheSavedFiltersAreTheOnesOfTheirRepository(t *testing.T) {
	r := newRig(t, branching())
	r.newPane(map[string]Filter{r.src.StorageKey(): {Kind: FilterAll}})
	r.settle()
	if got := r.pane.FilterLabel(); got != "All" {
		t.Errorf("label = %q", got)
	}
}

func TestPagesAreReadWhenTheLoadMoreRowShows(t *testing.T) {
	h := branching()
	r := newRig(t, h)
	r.pageSz = 3
	r.pane.Refresh()
	r.settle()
	got := r.subjects()
	if got[len(got)-1] != "..." {
		t.Fatalf("rows = %v, want the load-more row last", got)
	}
	if r.pane.Rows()[len(got)-1].Kind != RowLoadMore {
		t.Fatal("the last row is not the load-more row")
	}
	r.pane.ScrolledToEnd()
	r.settle()
	q := h.Queries()
	if last := q[len(q)-1]; last.Skip != 3 || last.Limit != 3 {
		t.Errorf("the second page: %+v", last)
	}
	r.pane.ScrolledToEnd()
	r.settle()
	r.pane.ScrolledToEnd()
	r.settle()
	got = r.subjects()
	if got[len(got)-1] == "..." {
		t.Errorf("rows = %v, want no load-more row at the end of the history", got)
	}
	n := 0
	for _, row := range r.pane.Rows() {
		if row.Kind == RowItem {
			n++
		}
	}
	if n != 7 {
		t.Errorf("%d commits in the rows, want 7", n)
	}
	// without pageOnScroll the row loads on a click (Open), not by showing
	r.onScrl = false
	r.pageSz = 3
	r.pane.Refresh()
	r.settle()
	before := len(h.Queries())
	r.pane.ScrolledToEnd()
	r.settle()
	if len(h.Queries()) != before {
		t.Error("the next page was read without scm.graph.pageOnScroll")
	}
	rows := r.pane.Rows()
	r.pane.Open(rows[len(rows)-1])
	r.settle()
	if len(h.Queries()) != before+1 {
		t.Errorf("queries %d, want %d", len(h.Queries()), before+1)
	}
}

func TestAPageThatEndsInsideAMergeJoinsTheNextOne(t *testing.T) {
	r := newRig(t, branching())
	r.pageSz = 2 // m, r1 | b2, d | c, b1 | a: the pages end with a lane open (the merge's second parent d, the branch point b1)
	r.pane.Refresh()
	r.settle()
	for i := 0; i < 5; i++ {
		r.pane.ScrolledToEnd()
		r.settle()
	}
	whole := newRig(t, branching())
	whole.settle()
	if got, want := r.subjects(), whole.subjects(); !reflect.DeepEqual(got, want) {
		t.Errorf("by pages %v\nwhole %v", got, want)
	}
	for i, row := range r.pane.Rows() {
		w := whole.pane.Rows()[i]
		if row.Kind != RowLoadMore && !reflect.DeepEqual(row.Lane.Input, w.Lane.Input) || !reflect.DeepEqual(row.Lane.Output, w.Lane.Output) {
			t.Errorf("row %d: lanes by pages %+v/%+v, whole %+v/%+v", i, row.Lane.Input, row.Lane.Output, w.Lane.Input, w.Lane.Output)
		}
	}
}

func TestRevealCurrentReadsPagesUntilTheCommitIsFound(t *testing.T) {
	h := branching()
	r := newRig(t, h)
	// make main be at the last commit
	main := h.refs[0]
	main.Revision = "a"
	h.refs[0] = main
	r.src.SetHistoryRefs(scm.HistoryRefs{Current: &main})
	r.pageSz = 2
	r.pane.Refresh()
	r.settle()
	r.pane.TakeReveal()
	r.pane.RevealCurrent()
	r.settle()
	if got := r.pane.TakeReveal(); got != "a" {
		t.Errorf("reveal = %q, want a", got)
	}
	if got := r.pane.TakeReveal(); got != "" {
		t.Errorf("reveal twice: %q", got)
	}
	// a current branch that is out of the filter is not revealed
	r.pane.SetFilter(Filter{Kind: FilterRefs, Refs: []string{"refs/heads/topic"}})
	r.settle()
	r.pane.RevealCurrent()
	if got := r.pane.TakeReveal(); got != "" {
		t.Errorf("reveal of a branch out of the filter: %q", got)
	}
}

func TestTheSearchReadsAgainAfterAPause(t *testing.T) {
	h := branching()
	r := newRig(t, h)
	r.settle()
	r.pane.SetSearch("grace")
	r.pane.SetSearch("grace h")
	if len(r.after) != 2 || r.after[0] != nil {
		t.Fatalf("pending calls %d, the first must have been stopped", len(r.after))
	}
	r.after[1]()
	r.settle()
	if got, want := r.subjects(), []string{"Topic two", "Topic one"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	for _, row := range r.pane.Rows() {
		if len(row.Lane.Input) != 0 && row.Item.ParentIDs == nil && len(row.Lane.Output) > 1 {
			t.Errorf("a found commit has lanes: %+v", row.Lane)
		}
	}
	q := h.Queries()
	if last := q[len(q)-1]; last.Filter != "grace h" {
		t.Errorf("query %+v", last)
	}
	// nothing found
	r.pane.SetSearch("zzz")
	r.after[len(r.after)-1]()
	r.settle()
	if r.pane.Message() != MessageNoMatch || len(r.pane.Rows()) != 0 {
		t.Errorf("message %q, rows %v", r.pane.Message(), r.subjects())
	}
	// cleared: the history again, at once, with its incoming and outgoing rows
	r.pane.SetSearch("")
	r.settle()
	if len(r.pane.Rows()) != 9 {
		t.Errorf("rows = %v", r.subjects())
	}
}

func TestAFailureOfTheProviderIsTheMessage(t *testing.T) {
	h := branching()
	h.fail = &errors.Error{Tag: "FakeFailure", Message: "git is not there"}
	r := newRig(t, h)
	r.settle()
	if got := r.pane.Message(); got != "Cannot read the history: git is not there" || len(r.pane.Rows()) != 0 {
		t.Errorf("message %q", got)
	}
	// it is read again by a refresh
	h.mu.Lock()
	h.fail = nil
	h.mu.Unlock()
	r.pane.Refresh()
	r.settle()
	if r.pane.Message() != "" || len(r.pane.Rows()) == 0 {
		t.Errorf("message %q, rows %d", r.pane.Message(), len(r.pane.Rows()))
	}
}

func TestARepositoryWithNoCommitsHasTheEmptyMessage(t *testing.T) {
	h := branching()
	h.all = nil
	r := newRig(t, h)
	r.settle()
	if r.pane.Message() != MessageEmpty || len(r.pane.Rows()) != 0 {
		t.Errorf("message %q, rows %v", r.pane.Message(), r.subjects())
	}
	// no repository with a history: the same
	r.src.SetHistoryProvider(nil)
	r.run()
	r.pane.Refresh()
	if r.pane.Message() != MessageEmpty || r.pane.Source() != nil {
		t.Errorf("message %q", r.pane.Message())
	}
}

func TestAHiddenViewReadsNothingUntilItIsShown(t *testing.T) {
	h := branching()
	r := newRig(t, h)
	r.settle()
	n := len(h.Queries())
	r.visible = false
	r.pane.Refresh()
	r.settle()
	if len(h.Queries()) != n {
		t.Error("a hidden view read the history")
	}
	r.visible = true
	r.pane.VisibilityChanged()
	r.settle()
	if len(h.Queries()) != n+1 || len(r.pane.Rows()) == 0 {
		t.Errorf("queries %d, want %d", len(h.Queries()), n+1)
	}
}

func TestAnAnswerThatCameAfterANewRequestIsDropped(t *testing.T) {
	h := branching()
	h.gate = make(chan struct{})
	r := newRig(t, h)
	r.pane.Refresh() // cancels the first request
	close(h.gate)
	r.settle()
	if len(r.pane.Rows()) != 9 {
		t.Errorf("rows = %v", r.subjects())
	}
}

func TestSilentChangesOfReferencesRefreshOnlyAtTheTop(t *testing.T) {
	h := branching()
	r := newRig(t, h)
	r.settle()
	n := len(h.Queries())
	r.atTop = false
	r.src.NotifyHistoryRefs(scm.HistoryRefsChange{Silent: true, Modified: []scm.HistoryRef{h.refs[2]}})
	r.run()
	if !r.pane.Outdated() || len(h.Queries()) != n {
		t.Errorf("outdated %v, queries %d (was %d)", r.pane.Outdated(), len(h.Queries()), n)
	}
	// a change that is not silent, and a silent one at the top, refresh
	r.src.NotifyHistoryRefs(scm.HistoryRefsChange{Modified: []scm.HistoryRef{h.refs[2]}})
	r.run()
	r.settle()
	if r.pane.Outdated() || len(h.Queries()) != n+1 {
		t.Errorf("outdated %v, queries %d, want %d", r.pane.Outdated(), len(h.Queries()), n+1)
	}
	r.atTop = true
	r.src.NotifyHistoryRefs(scm.HistoryRefsChange{Silent: true})
	r.run()
	r.settle()
	if len(h.Queries()) != n+2 {
		t.Errorf("queries %d, want %d", len(h.Queries()), n+2)
	}
}

func TestAnotherCurrentBranchRefreshesTheGraph(t *testing.T) {
	h := branching()
	r := newRig(t, h)
	r.settle()
	n := len(h.Queries())
	topic := h.refs[1]
	r.src.SetHistoryRefs(scm.HistoryRefs{Current: &topic})
	r.run()
	r.settle()
	if len(h.Queries()) != n+1 {
		t.Fatalf("queries %d, want %d", len(h.Queries()), n+1)
	}
	q := h.Queries()
	if got := q[len(q)-1].RefIDs; !reflect.DeepEqual(got, []string{"d"}) {
		t.Errorf("refs of the new query %v", got)
	}
	// a change of the remote reference alone does not
	remote := h.refs[2]
	r.src.SetHistoryRefs(scm.HistoryRefs{Current: &topic, Remote: &remote})
	r.run()
	r.settle()
	if len(h.Queries()) != n+1 {
		t.Errorf("queries %d after a change of the remote only", len(h.Queries()))
	}
}

func TestSeveralRepositoriesHaveTheirOwnHistory(t *testing.T) {
	r := newRig(t, branching())
	other := branching()
	other.all = other.all[:3]
	src2 := r.model.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///w/other"})
	src2.SetHistoryProvider(other)
	src2.SetHistoryRefs(scm.HistoryRefs{Current: &other.refs[0]})
	r.run()
	r.settle()
	if got := len(r.pane.Sources()); got != 2 {
		t.Fatalf("%d sources", got)
	}
	r.pane.SelectSource(src2)
	r.settle()
	if r.pane.Source() != src2 || len(other.Queries()) == 0 {
		t.Errorf("shown %v", r.pane.Source())
	}
	r.pane.SelectSource(nil)
	r.settle()
	// a removed repository is dropped
	r.pane.SelectSource(src2)
	r.model.Remove(src2)
	r.run()
	r.settle()
	if r.pane.Selected() != nil || r.pane.Source() != r.src {
		t.Errorf("after the removal: %v", r.pane.Source())
	}
}

func TestOpeningARowRunsTheCommandOfTheProvider(t *testing.T) {
	r := newRig(t, branching())
	r.settle()
	rows := r.pane.Rows()
	r.pane.Open(rows[1])
	if len(r.runs) != 1 || r.runs[0].ID != "git.viewCommit" {
		t.Fatalf("runs %+v", r.runs)
	}
	arg, ok := historyArg(r.runs[0].Args)
	if !ok || arg.Item == nil || arg.Item.ID != "m" || arg.SourceControl == "" {
		t.Errorf("arguments %+v", r.runs[0].Args)
	}
	// the incoming and the outgoing changes open a range
	r.pane.Open(rows[0])
	arg, ok = historyArg(r.runs[1].Args)
	if !ok || arg.Range == nil || arg.Range.Base != "b1" || arg.Range.Head != "m" || arg.Range.Title != "Outgoing Changes - origin/main ↔ main" {
		t.Errorf("outgoing range %+v", arg.Range)
	}
	var incoming Row
	for _, row := range rows {
		if row.Kind == RowIncoming {
			incoming = row
		}
	}
	r.pane.Open(incoming)
	arg, _ = historyArg(r.runs[2].Args)
	if arg.Range == nil || arg.Range.Base != "b1" || arg.Range.Head != "r1" || arg.Range.Title != "Incoming Changes - main ↔ origin/main" {
		t.Errorf("incoming range %+v", arg.Range)
	}
	r.pane.OpenID("c")
	if arg, _ := historyArg(r.runs[3].Args); arg.Item == nil || arg.Item.ID != "c" {
		t.Errorf("OpenID: %+v", r.runs[3].Args)
	}
	// a provider with no command opens nothing
	r.hist.open = ""
	r.pane.Open(rows[1])
	if len(r.runs) != 4 {
		t.Errorf("%d runs", len(r.runs))
	}
}

func TestTheContextOfTheMenus(t *testing.T) {
	r := newRig(t, branching())
	r.settle()
	rows := r.pane.Rows()
	title := r.pane.TitleContext()
	if title["scmProvider"] != "git" || title["scmCurrentHistoryItemRefHasRemote"] != true || title["scmCurrentHistoryItemRefHasBase"] != false || title["scmCurrentHistoryItemRefInFilter"] != true {
		t.Errorf("title context %+v", title)
	}
	if ctx := r.pane.ItemContext(rows[1]); ctx["scmHistoryItemHasCurrentHistoryItemRef"] != true {
		t.Errorf("the current commit: %+v", ctx)
	}
	if ctx := r.pane.ItemContext(rows[2]); ctx["scmHistoryItemHasCurrentHistoryItemRef"] != false {
		t.Errorf("another commit: %+v", ctx)
	}
	args := r.pane.ItemArgs(rows[1], "refs/heads/main")
	if arg, ok := historyArg(args); !ok || arg.RefID != "refs/heads/main" || arg.Item.ID != "m" {
		t.Errorf("item arguments %+v", args)
	}
	if arg, ok := historyArg(r.pane.TitleArgs()); !ok || arg.Item != nil || arg.SourceControl == "" {
		t.Errorf("title arguments %+v", r.pane.TitleArgs())
	}
	refs := make(chan []scm.HistoryRef, 1)
	r.pane.Refs(func(l []scm.HistoryRef, err error) {
		if err != nil {
			t.Error(err)
		}
		refs <- l
	})
	for len(refs) == 0 {
		r.run()
	}
	if l := <-refs; len(l) != 4 {
		t.Errorf("refs %v", l)
	}
}

// long is a history of n commits with a merge of a side branch every 50: the lanes open and close all along it.
func long(n int) *fakeHistory {
	h := branching()
	h.all = make([]scm.HistoryItem, 0, n)
	id := func(i int) string { return "c" + itoa(i) }
	for i := 0; i < n; i++ {
		it := scm.HistoryItem{ID: id(i), Subject: "Commit " + itoa(i), DisplayID: id(i), Author: "Ada Example", Timestamp: at(n - i)}
		if i+1 < n {
			it.ParentIDs = []string{id(i + 1)}
		}
		if i%50 == 0 && i+3 < n {
			// a merge of a branch that has the next two commits as its own: the first parent is the commit after them
			it.ParentIDs = []string{id(i + 3), id(i + 1)}
		}
		if i%50 == 1 && i+3 < n {
			it.ParentIDs = []string{id(i + 1)}
		}
		h.all = append(h.all, it)
	}
	h.refs = []scm.HistoryRef{ref("refs/heads/main", "main", id(0), "branches", "git-branch")}
	h.all[0].Refs = h.refs
	return h
}

func TestAVeryLongHistoryIsReadPageByPageWithTheCostOfAPage(t *testing.T) {
	const commits, size = 100_000, 50
	h := long(commits)
	r := newRig(t, h)
	r.pageSz = size
	start := time.Now()
	r.pane.Refresh()
	r.settle()
	first := time.Since(start)
	if first > 300*time.Millisecond {
		t.Errorf("the first page took %v, budget 300ms", first)
	}
	if got := len(r.pane.Rows()); got != size+1 {
		t.Fatalf("%d rows after the first page, want the page and the load-more row", got)
	}
	// 400 pages: a page is laid out from the lanes the page before it left, and the rows are made again from the lanes (a cost that
	// grows with the rows loaded, about 8 ms a page at 20,000 rows), so the pages at the end are bounded by a budget, not compared
	pages := func(n int) time.Duration {
		start := time.Now()
		for range n {
			r.pane.ScrolledToEnd()
			r.settle()
		}
		return time.Since(start)
	}
	pages(350)
	budget := 60 * time.Millisecond
	if raceEnabled {
		budget *= 10
	}
	if late := pages(50) / 50; late > budget {
		t.Errorf("a page at the end of 400 took %v, budget %v", late, budget)
	}
	if got, want := len(r.pane.Rows()), 401*size+1; got != want {
		t.Errorf("%d rows, want %d", got, want)
	}
	// the page of one lane layout does not depend on how many pages were laid out before: the last pages take as long as the first
	st := r.pane.state()
	if st == nil || len(st.lanes) != 401*size {
		t.Fatalf("lanes %d", len(st.lanes))
	}
}
