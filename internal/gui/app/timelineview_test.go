package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/timeline"
	timelineview "github.com/rm4n0s/emetos/internal/gui/views/timeline"
)

// The tests of the Timeline view in the window (step 24.21): it is folded and loads nothing until it is shown, it follows the active
// editor or is pinned, Open Timeline pins it to a file, its context keys and its saved filter.

// timelineProvider answers for any file with two items named after the file, and counts its calls.
type timelineProvider struct {
	mu    sync.Mutex
	calls []string
}

func (p *timelineProvider) Provide(_ context.Context, uri string, _ timeline.Options) (timeline.Page, error) {
	p.mu.Lock()
	p.calls = append(p.calls, uri)
	p.mu.Unlock()
	name := uri[strings.LastIndex(uri, "/")+1:]
	return timeline.Page{Items: []timeline.Item{
		{ID: "n2", Label: "Second change of " + name, Description: "Ada Example", Timestamp: time.Now().Add(-3 * time.Hour), Icon: "git-commit", ContextValue: "git:file:commit"},
		{ID: "n1", Label: "First change of " + name, Description: "Ada Example", Timestamp: time.Now().Add(-48 * time.Hour), Icon: "git-commit", ContextValue: "git:file:commit"},
	}}, nil
}

func (p *timelineProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func (p *timelineProvider) last() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.calls) == 0 {
		return ""
	}
	return p.calls[len(p.calls)-1]
}

// timelineApp is a window with files, the Explorer in the side bar and a timeline provider registered.
func timelineApp(t *testing.T, open ...string) (*saveApp, *timelineProvider, *timeline.Registration) {
	t.Helper()
	a := newSaveApp(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n"}, open...)
	p := &timelineProvider{}
	reg, err := a.win.Timeline.Register("test.ext", timeline.Descriptor{ID: "test-history", Label: "Test History", Schemes: []string{"file"}}, p)
	if err != nil {
		t.Fatal(err)
	}
	a.run("workbench.view.explorer")
	return a, p, reg
}

func (a *saveApp) pane() *timelineview.Pane { return a.shell().timeline.pane }

// waitPane pumps until the pane shows rows.
func (a *saveApp) waitPane(what string, cond func() bool) {
	a.t.Helper()
	a.waitFor(what, cond)
}

func TestTheTimelineViewIsFoldedAndLoadsNothingUntilItIsShown(t *testing.T) {
	a, p, _ := timelineApp(t, "a.go")
	a.pump()
	if !a.win.Views.IsCollapsed(command.ViewTimeline) {
		t.Fatal("the Timeline view starts open")
	}
	if p.count() != 0 {
		t.Fatalf("a folded view asked the provider: %v", p.calls)
	}
	a.run("timeline.focus")
	if a.win.Views.IsCollapsed(command.ViewTimeline) {
		t.Fatal("timeline.focus left the view folded")
	}
	a.waitPane("the items of a.go", func() bool { return len(a.pane().Rows()) == 2 })
	if got := p.last(); got != "file://"+a.files["a.go"] {
		t.Errorf("the provider was asked about %q", got)
	}
	if !a.ts.Exists(gluttertest.ByKey(timelineview.ItemKey("test-history", "n2"))) {
		t.Error("the view does not show the items")
	}
	if got := a.pane().Description(); got != "a.go" {
		t.Errorf("description = %q", got)
	}
	// folding it again stops it asking
	a.win.Views.SetCollapsed(command.ViewTimeline, true)
	before := p.count()
	a.show("b.go", false)
	a.pump()
	if p.count() != before {
		t.Errorf("a folded view asked for b.go")
	}
	a.noErrors()
}

func TestTheTimelineFollowsTheActiveEditorUntilItIsPinned(t *testing.T) {
	a, p, _ := timelineApp(t, "a.go")
	a.run("timeline.focus")
	a.waitPane("a.go", func() bool { return len(a.pane().Rows()) == 2 })
	a.show("b.go", false)
	a.waitPane("b.go", func() bool { return a.pane().URI() == "file://"+a.files["b.go"] && len(a.pane().Rows()) == 2 })
	if got := a.win.Keys.Get("timelineFollowActiveEditor"); got != true {
		t.Errorf("timelineFollowActiveEditor = %v", got)
	}
	// Pin Current Timeline: another editor does not move the view
	a.run("timeline.toggleFollowActiveEditor")
	if got := a.win.Keys.Get("timelineFollowActiveEditor"); got != false {
		t.Errorf("timelineFollowActiveEditor after pinning = %v", got)
	}
	if got := a.pane().Description(); got != "b.go (pinned)" {
		t.Errorf("description = %q", got)
	}
	a.show("a.go", false)
	a.pump()
	if a.pane().URI() != "file://"+a.files["b.go"] {
		t.Errorf("a pinned view moved to %s (%d calls)", a.pane().URI(), p.count())
	}
	// Unpin: it shows the file in front again
	a.run("timeline.toggleFollowActiveEditor")
	a.waitPane("a.go again", func() bool { return a.pane().URI() == "file://"+a.files["a.go"] })
	a.noErrors()
}

// Ported from VS Code: timeline.contribution.test.ts ('resolves the resource from the argument or the active editor')
func TestOpenTimelineTakesTheFileOfTheArgumentTheActiveEditorOrNone(t *testing.T) {
	t.Run("the argument", func(t *testing.T) {
		a, _, _ := timelineApp(t, "a.go")
		if err := a.runWith("files.openTimeline", a.files["b.go"]); err != nil {
			t.Fatal(err)
		}
		a.waitPane("b.go", func() bool { return len(a.pane().Rows()) == 2 })
		if a.pane().URI() != "file://"+a.files["b.go"] || a.pane().Following() {
			t.Errorf("uri %q following %v", a.pane().URI(), a.pane().Following())
		}
		if a.win.Views.IsCollapsed(command.ViewTimeline) || a.win.Views.Active() != command.ViewContainerExplorer {
			t.Error("the view was not shown")
		}
		if got := a.pane().Description(); got != "b.go (pinned)" {
			t.Errorf("description = %q", got)
		}
	})
	t.Run("the active editor", func(t *testing.T) {
		a, _, _ := timelineApp(t, "a.go")
		a.run("files.openTimeline")
		a.waitPane("a.go", func() bool { return len(a.pane().Rows()) == 2 })
		if a.pane().URI() != "file://"+a.files["a.go"] || a.pane().Following() {
			t.Errorf("uri %q following %v", a.pane().URI(), a.pane().Following())
		}
	})
	t.Run("no resource", func(t *testing.T) {
		a, p, _ := timelineApp(t)
		a.run("files.openTimeline")
		a.pump()
		if a.win.Views.IsCollapsed(command.ViewTimeline) {
			t.Error("the view was not opened")
		}
		if a.pane().URI() != "" || p.count() != 0 {
			t.Errorf("uri %q, %d calls", a.pane().URI(), p.count())
		}
		if !a.ts.Exists(gluttertest.ByText(timelineview.MessageNoEditor)) {
			t.Error("the view does not say that the editor has no timeline")
		}
	})
}

func TestRefreshAsksAgainAndTheKeysFollowTheProviders(t *testing.T) {
	a, p, reg := timelineApp(t, "a.go")
	if got := a.win.Keys.Get("timelineHasProvider"); got != true {
		t.Errorf("timelineHasProvider = %v", got)
	}
	a.run("timeline.focus")
	a.waitPane("a.go", func() bool { return len(a.pane().Rows()) == 2 })
	n := p.count()
	a.run("timeline.refresh")
	a.waitFor("a second call", func() bool { return p.count() == n+1 })
	// a change of the provider asks again for the file in front
	reg.Fire()
	a.waitFor("a third call", func() bool { return p.count() == n+2 })
	reg.Dispose()
	a.pump()
	if got := a.win.Keys.Get("timelineHasProvider"); got != false {
		t.Errorf("timelineHasProvider after the provider went = %v", got)
	}
	a.waitPane("an empty view", func() bool { return len(a.pane().Rows()) == 0 })
	a.noErrors()
}

func TestTheFilterOfTheSourcesIsSavedWithTheWindow(t *testing.T) {
	a, _, _ := timelineApp(t, "a.go")
	a.run("timeline.focus")
	a.waitPane("a.go", func() bool { return len(a.pane().Rows()) == 2 })
	if raw := a.shell().timelineWindowState(); raw != nil {
		t.Errorf("state with no filter = %s", raw)
	}
	a.pane().ToggleSource("test-history")
	a.pump()
	if got := a.win.Keys.Get("timelineExcludeSources"); got != `["test-history"]` {
		t.Errorf("timelineExcludeSources = %v", got)
	}
	raw := a.shell().timelineWindowState()
	var st timelineWindowState
	if err := json.Unmarshal(raw, &st); err != nil || !reflect.DeepEqual(st.Excluded, []string{"test-history"}) {
		t.Fatalf("state = %s (%v)", raw, err)
	}
	// a window that comes back with it
	b, _, _ := timelineApp(t, "a.go")
	b.win.pendingTimeline = raw
	b.shell().disposeTimeline()
	b.shell().initTimeline(b.shell().binding)
	if !b.pane().IsExcluded("test-history") {
		t.Error("the filter was not restored")
	}
}

func TestTheSavedStateOfTheTimelineMustBeJSON(t *testing.T) {
	_, err := decodeTimelineState(json.RawMessage("{"))
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("decodeTimelineState.TimelineStateInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestTheTimelinePageSizeAndPagingComeFromTheSettings(t *testing.T) {
	a, _, _ := timelineApp(t, "a.go")
	pane := a.pane()
	_ = pane
	a.setting("timeline.pageSize", 7)
	a.setting("timeline.pageOnScroll", false)
	// the pane reads them when it asks: the first page of a.go is 7 items
	var asked []timeline.Options
	var mu sync.Mutex
	if _, err := a.win.Timeline.Register("test.ext", timeline.Descriptor{ID: "sized", Label: "Sized", Schemes: []string{"file"}},
		sizedProvider{func(o timeline.Options) { mu.Lock(); asked = append(asked, o); mu.Unlock() }}); err != nil {
		t.Fatal(err)
	}
	a.run("timeline.focus")
	a.waitFor("a request with the page size", func() bool { mu.Lock(); defer mu.Unlock(); return len(asked) > 0 })
	mu.Lock()
	defer mu.Unlock()
	if asked[0].Limit != 7 {
		t.Errorf("limit = %d, want the setting 7", asked[0].Limit)
	}
}

type sizedProvider struct{ f func(timeline.Options) }

func (s sizedProvider) Provide(_ context.Context, _ string, o timeline.Options) (timeline.Page, error) {
	s.f(o)
	return timeline.Page{}, nil
}
