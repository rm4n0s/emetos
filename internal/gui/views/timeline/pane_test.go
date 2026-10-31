package timeline

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/timeline"
	rerrors "github.com/rm4n0s/errors"
)

const fileA = "file:///w/a.go"

func TestLoadsTheTimelineOfTheActiveFile(t *testing.T) {
	r := newRig(t)
	f := &fakeProvider{answer: pager(items("P", 0, 2))}
	r.provider("p", f)
	r.run()
	r.pane.SetActive(fileA)
	if got := r.pane.Message(); got != "Loading timeline for a.go..." {
		t.Errorf("message while loading = %q", got)
	}
	r.settle()
	if got, want := r.labels(), []string{"P0", "P1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if r.pane.Message() != "" {
		t.Errorf("message = %q", r.pane.Message())
	}
	if got := r.pane.Description(); got != "a.go" {
		t.Errorf("description = %q", got)
	}
	// the ages: 1000-1000 and 1000-990 minutes before the clock
	rows := r.pane.Rows()
	if rows[0].Relative != "now" || rows[1].Relative != "10 mins" || rows[1].RelativeFull != "10 minutes" {
		t.Errorf("ages = %q, %q (%q)", rows[0].Relative, rows[1].Relative, rows[1].RelativeFull)
	}
	calls := f.Calls()
	if len(calls) != 1 || calls[0].URI != fileA || calls[0].Opts != (timeline.Options{Limit: 3}) {
		t.Errorf("calls = %+v", calls)
	}
}

func TestAnAgeThatRepeatsIsHidden(t *testing.T) {
	r := newRig(t)
	r.provider("p", &fakeProvider{answer: func(string, timeline.Options) (timeline.Page, error) {
		return timeline.Page{Items: []timeline.Item{
			{ID: "1", Label: "one", Timestamp: at(995)},
			{ID: "2", Label: "two", Timestamp: at(995).Add(-20 * time.Second)},
			{ID: "3", Label: "three", Timestamp: at(940)},
		}}, nil
	}})
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	var got []string
	for _, row := range r.pane.Rows() {
		got = append(got, row.Relative+"/"+map[bool]string{true: "hidden", false: "shown"}[row.HideRelative])
	}
	if want := []string{"5 mins/shown", "5 mins/hidden", "1 hr/shown"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ages = %v, want %v", got, want)
	}
}

func TestPagingAcrossTheBoundary(t *testing.T) {
	r := newRig(t)
	f := &fakeProvider{answer: pager(items("P", 0, 7))}
	r.provider("p", f)
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	if got, want := r.labels(), []string{"P0", "P1", "P2", LoadMore}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first page = %v, want %v", got, want)
	}
	r.pane.Rows()[3].Loading = true
	r.pane.LoadMore()
	if last := r.pane.Rows()[3]; !last.Loading || last.Text() != Loading {
		t.Errorf("the row while loading = %+v", last)
	}
	r.settle()
	if got, want := r.labels(), []string{"P0", "P1", "P2", "P3", "P4", "P5", LoadMore}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second page = %v, want %v", got, want)
	}
	r.pane.ScrolledToEnd()
	r.settle()
	if got, want := r.labels(), []string{"P0", "P1", "P2", "P3", "P4", "P5", "P6"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("third page = %v, want %v", got, want)
	}
	wantCalls := []timeline.Options{{Limit: 3}, {Cursor: "3", Limit: 3}, {Cursor: "6", Limit: 3}}
	var gotCalls []timeline.Options
	for _, c := range f.Calls() {
		gotCalls = append(gotCalls, c.Opts)
	}
	if !reflect.DeepEqual(gotCalls, wantCalls) {
		t.Errorf("asked for %+v, want %+v", gotCalls, wantCalls)
	}
}

func TestPagingOnScrollCanBeOff(t *testing.T) {
	r := newRig(t)
	r.provider("p", &fakeProvider{answer: pager(items("P", 0, 7))})
	r.pane.opts.PageOnScroll = func() bool { return false }
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	r.pane.ScrolledToEnd()
	r.settle()
	if got := len(r.pane.Rows()); got != 4 {
		t.Errorf("rows = %d after scrolling with pageOnScroll off", got)
	}
	r.pane.Open(r.pane.Rows()[3]) // the row still loads when it is opened
	r.settle()
	if got := len(r.pane.Rows()); got != 7 {
		t.Errorf("rows = %d after opening Load more", got)
	}
}

func TestAPageThatRepeatsAnItemReplacesIt(t *testing.T) {
	page := 0
	r := newRig(t)
	r.provider("p", &fakeProvider{answer: func(string, timeline.Options) (timeline.Page, error) {
		page++
		if page == 1 {
			return timeline.Page{Items: []timeline.Item{{ID: "a", Label: "a", Timestamp: at(990)}, {ID: "b", Label: "b", Timestamp: at(980)}}, Cursor: "x"}, nil
		}
		return timeline.Page{Items: []timeline.Item{{ID: "b", Label: "b again", Timestamp: at(980)}, {ID: "c", Label: "c", Timestamp: at(970)}}}, nil
	}})
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	r.pane.LoadMore()
	r.settle()
	if got, want := r.labels(), []string{"a", "b again", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestAPageOfNewerItemsGoesFirst(t *testing.T) {
	a := &aggregate{items: []timeline.Item{{ID: "old", Timestamp: at(5)}}}
	if !a.add(timeline.Page{Items: []timeline.Item{{ID: "new", Timestamp: at(9)}}, Cursor: "c"}) {
		t.Error("add did not report the change")
	}
	if a.items[0].ID != "new" || a.items[1].ID != "old" || a.cursor != "c" {
		t.Errorf("items = %+v cursor %q", a.items, a.cursor)
	}
	if a.add(timeline.Page{}) {
		t.Error("an empty page is a change")
	}
}

func TestSeveralSourcesAreMergedNewestFirst(t *testing.T) {
	r := newRig(t)
	r.pageSz = 10
	mk := func(prefix string, mins ...int) *fakeProvider {
		var its []timeline.Item
		for i, m := range mins {
			its = append(its, timeline.Item{ID: prefix + itoa(i), Label: prefix + itoa(i), Timestamp: at(m)})
		}
		return &fakeProvider{answer: pager(its)}
	}
	r.provider("a", mk("A", 990, 970))
	r.provider("b", mk("B", 980, 960))
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	if got, want := r.labels(), []string{"A0", "B0", "A1", "B1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestTheFilterHidesASourceAndKeepsItsItems(t *testing.T) {
	r := newRig(t)
	r.pageSz = 10
	r.provider("a", &fakeProvider{answer: pager([]timeline.Item{{ID: "a1", Label: "A1", Timestamp: at(990)}})})
	r.provider("b", &fakeProvider{answer: pager([]timeline.Item{{ID: "b1", Label: "B1", Timestamp: at(980)}})})
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	r.pane.ToggleSource("a")
	r.settle()
	if got, want := r.labels(), []string{"B1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows with a filtered out = %v, want %v", got, want)
	}
	if want := [][]string{{"a"}}; !reflect.DeepEqual(r.excl, want) {
		t.Errorf("stored filter = %v, want %v", r.excl, want)
	}
	r.pane.ToggleSource("a")
	r.settle()
	if got, want := r.labels(), []string{"A1", "B1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows with a back in = %v, want %v", got, want)
	}
	// a source that was filtered out when the view loaded is asked when it comes back
	r.pane.SetExcluded([]string{"b"})
	r.pane.Reset()
	r.settle()
	r.pane.SetExcluded(nil)
	r.settle()
	if got, want := r.labels(), []string{"A1", "B1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows after the filter was cleared = %v, want %v", got, want)
	}
}

func TestFollowsTheActiveEditorUntilPinned(t *testing.T) {
	r := newRig(t)
	f := &fakeProvider{answer: pager(items("P", 0, 1))}
	r.provider("p", f)
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	r.pane.SetActive("file:///w/b.go")
	r.settle()
	if r.pane.URI() != "file:///w/b.go" || r.pane.Description() != "b.go" || !r.pane.Following() {
		t.Fatalf("following: uri %q description %q", r.pane.URI(), r.pane.Description())
	}
	r.pane.SetFollowing(false)
	if got := r.pane.Description(); got != "b.go (pinned)" {
		t.Errorf("pinned description = %q", got)
	}
	r.pane.SetActive("file:///w/c.go")
	r.settle()
	if r.pane.URI() != "file:///w/b.go" {
		t.Errorf("a pinned view moved to %q", r.pane.URI())
	}
	r.pane.SetFollowing(true)
	r.settle()
	if r.pane.URI() != "file:///w/c.go" || r.pane.Description() != "c.go" {
		t.Errorf("following again: %q %q", r.pane.URI(), r.pane.Description())
	}
	// Show (files.openTimeline) pins the view to the file
	r.svc.Show("file:///w/d.go")
	r.run()
	r.settle()
	if r.pane.URI() != "file:///w/d.go" || r.pane.Following() || r.pane.Description() != "d.go (pinned)" {
		t.Errorf("after Show: %q following %v %q", r.pane.URI(), r.pane.Following(), r.pane.Description())
	}
	// an active editor with no file leaves a following view with nothing to show
	r.pane.SetFollowing(true)
	r.pane.SetActive("")
	r.settle()
	if r.pane.Message() != MessageNoEditor || len(r.pane.Rows()) != 0 {
		t.Errorf("no file: message %q rows %d", r.pane.Message(), len(r.pane.Rows()))
	}
}

func TestAFoldedViewLoadsNothingUntilItIsShown(t *testing.T) {
	r := newRig(t)
	r.visible = false
	r.pane.VisibilityChanged()
	f := &fakeProvider{answer: pager(items("P", 0, 2))}
	r.provider("p", f)
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	if len(f.Calls()) != 0 {
		t.Fatalf("a folded view asked: %+v", f.Calls())
	}
	r.visible = true
	r.pane.VisibilityChanged()
	r.settle()
	if got := r.labels(); len(got) != 2 {
		t.Errorf("rows after it was shown = %v", got)
	}
}

func TestAChangeWhileFoldedMakesTheTimelineStale(t *testing.T) {
	r := newRig(t)
	f := &fakeProvider{answer: pager(items("P", 0, 2))}
	reg := r.provider("p", f)
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	r.visible = false
	r.pane.VisibilityChanged()
	reg.Fire()
	r.run()
	if n := len(f.Calls()); n != 1 {
		t.Fatalf("a folded view reloaded: %d calls", n)
	}
	r.visible = true
	r.pane.VisibilityChanged()
	r.settle()
	if n := len(f.Calls()); n != 2 {
		t.Errorf("the view did not reload when shown: %d calls", n)
	}
}

func TestRefreshAfterTheProviderSaysItChanged(t *testing.T) {
	r := newRig(t)
	var all = items("P", 0, 2)
	f := &fakeProvider{answer: func(u string, o timeline.Options) (timeline.Page, error) { return pager(all)(u, o) }}
	reg := r.provider("p", f)
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	all = append([]timeline.Item{{ID: "N", Label: "N", Timestamp: at(1005)}}, all...)
	reg.Fire(fileA)
	r.run()
	r.settle()
	if got, want := r.labels(), []string{"N", "P0", "P1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows after a commit = %v, want %v", got, want)
	}
	r.pane.Reset()
	r.settle()
	if len(f.Calls()) != 3 {
		t.Errorf("calls = %d", len(f.Calls()))
	}
}

func TestAProviderThatFailsShowsTheEmptyMessageAndLogsOnce(t *testing.T) {
	var buf bytes.Buffer
	r := newRig(t)
	r.pane.log = slog.New(slog.NewTextHandler(&buf, nil))
	r.provider("p", &fakeProvider{answer: func(string, timeline.Options) (timeline.Page, error) {
		return timeline.Page{}, rerrors.New("Boom", "boom")
	}})
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	if r.pane.Message() != MessageNone {
		t.Errorf("message = %q", r.pane.Message())
	}
	if n := strings.Count(buf.String(), "a timeline provider failed"); n != 1 {
		t.Errorf("logged %d times: %s", n, buf.String())
	}
}

func TestAFileWithAnotherSchemeHasNoRequest(t *testing.T) {
	r := newRig(t)
	f := &fakeProvider{}
	r.provider("p", f)
	r.run()
	r.pane.SetActive("emetos-virtual:/git/abc/HEAD/a.go")
	r.settle()
	if len(f.Calls()) != 0 {
		t.Errorf("asked for a file of another scheme: %+v", f.Calls())
	}
	if r.pane.Message() != MessageNone {
		t.Errorf("message = %q", r.pane.Message())
	}
}

// Ported from VS Code: timelinePane.test.ts ('removing a pending provider releases pagination for a surviving provider')
func TestRemovingAPendingProviderReleasesPagination(t *testing.T) {
	r := newRig(t)
	b := &fakeProvider{answer: func(string, timeline.Options) (timeline.Page, error) {
		return timeline.Page{Items: []timeline.Item{{ID: "first", Label: "First item", Timestamp: at(1)}}, Cursor: "next"}, nil
	}}
	r.provider("B", b)
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	a := &fakeProvider{gate: make(chan struct{})}
	regA := r.provider("A", a)
	r.run() // A is added: its timeline is asked for, and waits
	if got := r.pending(); !reflect.DeepEqual(got, []string{"A"}) {
		t.Fatalf("pending = %v", got)
	}
	r.pane.refresh()
	if !r.pane.Rows()[1].Loading {
		t.Error("the row does not say it loads while A is pending")
	}
	for deadline := time.Now().Add(5 * time.Second); len(a.Calls()) == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("A was not asked")
		}
	}
	removed := a.Calls()[0].Ctx
	regA.Dispose()
	r.run()
	if last := r.pane.Rows()[len(r.pane.Rows())-1]; last.Loading || last.Text() != LoadMore {
		t.Errorf("after A was removed the last row is %q (loading %v)", last.Text(), last.Loading)
	}
	// the extension host discards results from a disposed provider; a late one changes nothing
	close(a.gate)
	r.run()
	if removed.Err() == nil {
		t.Error("the request of the removed provider was not cancelled")
	}
	if last := r.pane.Rows()[len(r.pane.Rows())-1]; last.Loading {
		t.Error("the last row loads after the late answer")
	}
	r.pane.LoadMore()
	r.settle()
	var got []string
	for _, c := range b.Calls() {
		got = append(got, c.Opts.Cursor)
	}
	if want := []string{"", "next"}; !reflect.DeepEqual(got, want) {
		t.Errorf("B was asked with cursors %q, want %q", got, want)
	}
}

// Ported from VS Code: timelinePane.test.ts ('late nonempty result cannot affect a replacement provider request' and the same with an
// undefined result)
func TestALateResultCannotAffectAReplacementProviderRequest(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "a late empty result"
		if stale {
			name = "a late nonempty result"
		}
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.provider("B", &fakeProvider{answer: func(string, timeline.Options) (timeline.Page, error) {
				return timeline.Page{Items: []timeline.Item{{ID: "first", Label: "First item", Timestamp: at(1)}}, Cursor: "next"}, nil
			}})
			r.run()
			r.pane.SetActive(fileA)
			r.settle()
			old := &fakeProvider{gate: make(chan struct{}), answer: func(string, timeline.Options) (timeline.Page, error) {
				if !stale {
					return timeline.Page{}, nil
				}
				return timeline.Page{Items: []timeline.Item{{ID: "stale", Label: "Stale item", Timestamp: at(3)}}}, nil
			}}
			regOld := r.provider("A", old)
			r.run()
			regOld.Dispose()
			r.run()
			replacement := &fakeProvider{gate: make(chan struct{}), answer: func(string, timeline.Options) (timeline.Page, error) {
				return timeline.Page{Items: []timeline.Item{{ID: "replacement", Label: "Replacement item", Timestamp: at(2)}}}, nil
			}}
			r.provider("A", replacement)
			r.run()
			replacementRequest := r.pane.pending["A"]
			r.pane.refresh()
			close(old.gate)
			time.Sleep(20 * time.Millisecond)
			r.run()
			if r.pane.pending["A"] != replacementRequest || replacementRequest == nil {
				t.Error("the late answer replaced the pending request of the new provider")
			}
			if r.pane.timelines["A"] != nil {
				t.Error("the late answer made a timeline for A")
			}
			if last := r.pane.Rows()[len(r.pane.Rows())-1]; !last.Loading {
				t.Error("the row does not load while the replacement is pending")
			}
			close(replacement.gate)
			r.settle()
			if got := r.pane.timelines["A"]; got == nil || len(got.items) != 1 || got.items[0].ID != "replacement" {
				t.Errorf("timeline of A = %+v", got)
			}
			if last := r.pane.Rows()[len(r.pane.Rows())-1]; last.Loading {
				t.Error("the row loads after the replacement answered")
			}
		})
	}
}

// Ported from VS Code: timelinePane.test.ts ('explains why the timeline is empty')
func TestExplainsWhyTheTimelineIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		sources  []string
		excluded []string
		want     string
	}{
		{"no providers", nil, nil, "No timeline information was provided. Source Control has not been configured."},
		{"no providers with a stale filter", nil, []string{"git"}, "No timeline information was provided. Source Control has not been configured."},
		{"a stale filter with another provider", []string{"other"}, []string{"git"}, "No timeline information was provided. Source Control has not been configured."},
		{"all providers filtered", []string{"git"}, []string{"git"}, "All timeline sources have been filtered out. Source Control has not been configured."},
		{"some providers filtered", []string{"git", "other"}, []string{"git"}, "No filtered timeline information was provided. Source Control has not been configured."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			r.scm = 0
			for _, s := range tt.sources {
				r.provider(s, &fakeProvider{})
			}
			r.pane.SetExcluded(tt.excluded)
			r.run()
			r.pane.SetActive(fileA)
			r.settle()
			if got := r.pane.Message(); got != tt.want {
				t.Errorf("message = %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("with a source control provider", func(t *testing.T) {
		r := newRig(t)
		r.provider("git", &fakeProvider{})
		r.run()
		r.pane.SetActive(fileA)
		r.settle()
		if got := r.pane.Message(); got != MessageNone {
			t.Errorf("message = %q", got)
		}
	})
}

// Ported from VS Code: timelinePane.test.ts ('reloads a changed timeline with a limit covering the loaded items'; the workspace
// timeline is not built)
func TestReloadsAChangedTimelineWithALimitCoveringTheLoadedItems(t *testing.T) {
	r := newRig(t)
	f := &fakeProvider{answer: func(string, timeline.Options) (timeline.Page, error) {
		return timeline.Page{Items: []timeline.Item{{ID: "first", Label: "First item", Timestamp: at(1)}}, Cursor: "next"}, nil
	}}
	reg := r.provider("B", f)
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	f.mu.Lock()
	f.gate = make(chan struct{})
	f.mu.Unlock()
	reg.Fire("file:///other.txt")
	reg.Fire()
	reg.Fire(fileA)
	r.run()
	waitCalls(t, f, 2)
	var got []timeline.Options
	for _, c := range f.Calls() {
		got = append(got, c.Opts)
	}
	want := []timeline.Options{{Limit: 3}, {Limit: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("requests = %+v, want %+v", got, want)
	}
	close(f.gate)
}

// Ported from VS Code: timelinePane.test.ts ('reloads a source whose providers changed')
func TestReloadsASourceWhoseProvidersChanged(t *testing.T) {
	r := newRig(t)
	f := &fakeProvider{answer: func(string, timeline.Options) (timeline.Page, error) {
		return timeline.Page{Items: []timeline.Item{{ID: "first", Label: "First item", Timestamp: at(1)}}, Cursor: "next"}, nil
	}}
	r.provider("B", f)
	r.run()
	r.pane.SetActive(fileA)
	r.settle()
	r.pane.onProvidersChanged(timeline.ProvidersChange{Removed: []string{"B"}, Added: []string{"B"}})
	r.settle()
	var got []string
	for _, c := range f.Calls() {
		got = append(got, c.Opts.Cursor)
	}
	if want := []string{"", ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("cursors = %q, want %q", got, want)
	}
}

// Ported from VS Code: timelinePane.test.ts ('inline and context menus of an item get the same selection context'; a view with one
// selected row has no multi-selection)
func TestTheContextOfTheMenusOfAnItem(t *testing.T) {
	r := newRig(t)
	row := Row{Entry: &Entry{Item: timeline.Item{ID: "1", Source: "B", Label: "one", ContextValue: "commit", Args: map[string]any{"ref": "abc"}}}}
	want := map[string]any{"view": "timeline", "timelineItem": "commit", "listMultiSelection": false, "listDoubleSelection": false, "timelineCanCompareSelection": false}
	if got := r.pane.ItemContext(row); !reflect.DeepEqual(got, want) {
		t.Errorf("context = %v, want %v", got, want)
	}
	if got := r.pane.ItemContext(Row{})["timelineItem"]; got != nil {
		t.Errorf("the Load more row has the item %v", got)
	}
	r.pane.uri = fileA
	args := r.pane.ItemArgs(row)
	wantArgs := []any{map[string]any{"source": "B", "id": "1", "uri": fileA, "contextValue": "commit", "label": "one", "args": map[string]any{"ref": "abc"}}}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestOpeningAnItemRunsItsCommand(t *testing.T) {
	r := newRig(t)
	r.pane.uri = fileA
	plain := Row{Entry: &Entry{Item: timeline.Item{ID: "1", Source: "p", Label: "one", Command: timeline.Command{ID: "x.open"}}}}
	withArgs := Row{Entry: &Entry{Item: timeline.Item{ID: "2", Source: "p", Command: timeline.Command{ID: "x.open", Args: "mine"}}}}
	none := Row{Entry: &Entry{Item: timeline.Item{ID: "3", Source: "p"}}}
	r.pane.Open(plain)
	r.pane.Open(withArgs)
	r.pane.Open(none)
	if len(r.runs) != 2 || r.runs[0].ID != "x.open" || !reflect.DeepEqual(r.runs[0].Args, r.pane.ItemArgs(plain)) || r.runs[1].Args != "mine" {
		t.Errorf("runs = %+v", r.runs)
	}
}

func TestDisposeCancelsTheRequests(t *testing.T) {
	r := newRig(t)
	f := &fakeProvider{gate: make(chan struct{})}
	r.provider("p", f)
	r.run()
	r.pane.SetActive(fileA)
	deadline := time.Now().Add(2 * time.Second)
	for len(f.Calls()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	ctx := f.Calls()[0].Ctx
	r.pane.Dispose()
	deadline = time.Now().Add(2 * time.Second)
	for ctx.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !rerrors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("context = %v", ctx.Err())
	}
}

func TestThePageSizeOfAnAutomaticSetting(t *testing.T) {
	tests := []struct {
		name   string
		size   int
		height float64
		scroll bool
		want   int
	}{
		{"a number", 50, 0, true, 50},
		{"a small view", 0, 100, true, 20},
		{"a tall view with paging on scroll takes one more row", 0, 22 * 40, true, 41},
		{"a tall view without paging on scroll takes one less", 0, 22 * 40, false, 39},
		{"no height", 0, 0, true, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPane(PaneOpts{
				PageSize: func() int { return tt.size }, RenderHeight: func() float64 { return tt.height }, PageOnScroll: func() bool { return tt.scroll },
			})
			if got := p.pageSize(); got != tt.want {
				t.Errorf("pageSize = %d, want %d", got, tt.want)
			}
		})
	}
}

// waitCalls waits until a provider was called n times.
func waitCalls(t *testing.T, f *fakeProvider, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(f.Calls()) < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}
