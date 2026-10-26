package scratchpad

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// clock is a clock that moves a second at every reading, from 12:00:00.
type clock struct{ t time.Time }

func (c *clock) now() time.Time {
	c.t = c.t.Add(time.Second)
	return c.t
}

// rig is the extension activated over the fake API as the window tab "t1".
type rig struct {
	t   *testing.T
	api *fakeapi.API
	ext *scratchpad
	tab *fakeapi.WindowTab
}

// newRig activates the extension and starts its tab with the state; the tab is the one tab of the scope.
func newRig(t *testing.T, state json.RawMessage) *rig {
	t.Helper()
	c := &clock{t: time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)}
	e := NewWithClock(c.now)()
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWindowTab, Name: "Scratchpad", WindowTab: KindID, ID: ID + ":t1"})
	if err := e.Activate(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, api: api, ext: e.(*scratchpad), tab: api.StartWindowTab("t1", state)}
}

// other is a window tab the edits come from.
func other(name string) extension.ScopeInfo {
	return extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: name, Folder: "/work/" + name, ID: "/work/" + name}
}

// change is the payload of emetos.document.changed for a document.
func change(uri string) extension.DocumentChange {
	return extension.DocumentChange{URI: uri, Version: 2, Cause: "edit", Changes: []extension.TextChange{{Text: "x"}}}
}

func route(t *testing.T, err error, want string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}

func TestTheManifestDeclaresTheWindowTabAndTheTopicsOfTheFeed(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	k := m.Contributes.WindowTabKinds
	if len(k) != 1 || k[0].ID != "emetos.scratchpad.tab" || k[0].Title != "Scratchpad" || k[0].PlusMenuLabel != "Scratchpad" {
		t.Errorf("the kinds = %+v", k)
	}
	if !slices.Contains(m.Events.CrossTab, extension.TopicDocumentChanged) || !slices.Contains(m.Events.CrossTab, extension.TopicSharingRevoked) {
		t.Errorf("the cross-tab topics = %v", m.Events.CrossTab)
	}
}

func TestTheNotesAreKeptInTheStateOfTheTabAndComeBack(t *testing.T) {
	r := newRig(t, nil)
	if got := string(r.tab.State()); got != `{"notes":""}` {
		t.Errorf("the state of a new tab = %s", got)
	}
	r.ext.notes.SetValue("buy milk")
	if got := string(r.tab.State()); got != `{"notes":"buy milk"}` {
		t.Errorf("the state = %s", got)
	}
	back := newRig(t, r.tab.State())
	if got := back.ext.notes.Value(); got != "buy milk" {
		t.Errorf("the notes after the restart = %q", got)
	}
	// a state that is not JSON of the notes is the error of the tab
	_, err := r.ext.create(r.tab, json.RawMessage(`[1]`))
	route(t, err, "scratchpad.create.ScratchpadStateInvalid")
}

func TestTheFeedListsTheEditsOfOtherTabsNewestFirstAndCountsTheChangesOfADocument(t *testing.T) {
	r := newRig(t, nil)
	r.api.EmitFrom(other("api"), extension.TopicDocumentChanged, change("file:///work/api/main.go"))
	r.api.EmitFrom(other("web"), extension.TopicDocumentChanged, change("file:///work/web/app.ts"))
	r.api.EmitFrom(other("api"), extension.TopicDocumentChanged, change("file:///work/api/main.go"))
	r.api.EmitFrom(other("api"), extension.TopicDocumentChanged, change("untitled:1"))
	var got []string
	for _, x := range r.ext.recent {
		got = append(got, nameOf(x.uri)+" "+x.detail())
	}
	want := []string{
		"untitled:1 api · 1 change · 12:00:04",
		"main.go api · 2 changes · 12:00:03",
		"app.ts web · 1 change · 12:00:02",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the feed =\n%q\nwant\n%q", got, want)
	}
	// every change asks the tab to be built again
	if r.tab.Refreshs != 4 {
		t.Errorf("the tab was built again %d times, want 4", r.tab.Refreshs)
	}
	// the feed keeps the newest entries only
	for i := range maxRecent + 5 {
		r.api.EmitFrom(other("many"), extension.TopicDocumentChanged, change("file:///work/many/f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".go"))
	}
	if len(r.ext.recent) != maxRecent {
		t.Errorf("the feed has %d entries, want %d", len(r.ext.recent), maxRecent)
	}
}

func TestTheFeedSaysWhenTheEventsOfATabNoLongerReachIt(t *testing.T) {
	r := newRig(t, nil)
	r.api.Emit(extension.TopicSharingRevoked, extension.SharingRevoked{From: "/work/api", FromName: "api", Reason: extension.RevokedLinkRemoved})
	if r.ext.stopped != "api" {
		t.Errorf("stopped = %q", r.ext.stopped)
	}
	r.api.Emit(extension.TopicSharingRevoked, extension.SharingRevoked{From: "/work/web", Reason: extension.RevokedPrivate})
	if r.ext.stopped != "/work/web" {
		t.Errorf("stopped = %q", r.ext.stopped)
	}
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 900, Height: 200}, widgetOf(r))
	ts.Pump()
	if !ts.Exists(gluttertest.ByText("The edits of /work/web no longer reach this tab.")) {
		t.Error("the notice is not on screen")
	}
}

func TestEditingTheNotesAsksForTheStateToBeWritten(t *testing.T) {
	r := newRig(t, nil)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 700, Height: 300}, widgetOf(r))
	ts.Pump()
	r.ext.notes.SetValue("a")
	r.ext.notes.SetValue("ab")
	if r.tab.Changes < 2 {
		t.Errorf("StateChanged was called %d times for two edits", r.tab.Changes)
	}
	r.api.CloseWindowTab()
}

func TestGoldenTheScratchpadTab(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, json.RawMessage(`{"notes":"Ideas for the release\n- the window tabs\n- the remote control"}`))
			r.api.EmitFrom(other("api"), extension.TopicDocumentChanged, change("file:///work/api/server.go"))
			r.api.EmitFrom(other("api"), extension.TopicDocumentChanged, change("file:///work/api/server.go"))
			r.api.EmitFrom(other("web"), extension.TopicDocumentChanged, change("file:///work/web/index.html"))
			ts := uitest.Pump(t, th, gui.Size{Width: 760, Height: 300}, widgetOf(r))
			ts.Pump()
			if !ts.Exists(gluttertest.ByText("RECENT EDITS")) || !ts.Exists(gluttertest.ByText("server.go")) {
				t.Fatal("the feed is not on screen")
			}
			uitest.MatchesGolden(ts, "scratchpad_tab_"+th.Name)
		})
	}
}

func TestGoldenTheScratchpadTabBeforeAnyEdit(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, nil)
			ts := uitest.Pump(t, th, gui.Size{Width: 760, Height: 200}, widgetOf(r))
			ts.Pump()
			if !ts.Exists(gluttertest.ByText("NOTES")) {
				t.Fatal("the tab is not on screen")
			}
			uitest.MatchesGolden(ts, "scratchpad_tab_empty_"+th.Name)
		})
	}
}

// widgetOf is the widget of the tab of the rig.
func widgetOf(r *rig) widgets.Widget { return widgets.NewBuilder(r.tab.Content.Build) }
