package testext

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// These tests keep what the tests of the Notes and Scratchpad extensions held of the contract, since those were removed
// (step 17e.1, SPEC §10.26): a tab kind of which several are open at once, the editor title action, the state of a tab and
// its restore, and a window tab that keeps its state and hears the events of other tabs.

// tabsRig is the extension activated over the fake API.
type tabsRig struct {
	api *fakeapi.API
	ext *tabsExt
}

func newTabsRig(t *testing.T, info extension.ScopeInfo) *tabsRig {
	t.Helper()
	e := NewTabs()
	api := fakeapi.New(t, e.Manifest(), info)
	if err := e.Activate(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	return &tabsRig{api: api, ext: e.(*tabsExt)}
}

func workspaceRig(t *testing.T) *tabsRig {
	return newTabsRig(t, extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: t.TempDir()})
}

// panelRig is the extension as the window tab "t1", started with the state.
func panelRig(t *testing.T, state json.RawMessage) (*tabsRig, *fakeapi.WindowTab) {
	t.Helper()
	r := newTabsRig(t, extension.ScopeInfo{Kind: extension.ScopeWindowTab, Name: "Test Panel", WindowTab: PanelKindID, ID: TabsID + ":t1"})
	return r, r.api.StartWindowTab("t1", state)
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
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

func pageStateJSON(text string) json.RawMessage { return mustJSON(pageState{Text: &text}) }

func TestTheTabsManifestIsValidAndDeclaresTheKindsTheActionAndTheTopics(t *testing.T) {
	m := NewTabs().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	c := m.Contributes
	if len(c.EditorKinds) != 1 || c.EditorKinds[0].ID != PageKindID || len(c.EditorActions) != 1 || c.EditorActions[0].Command != NewPageCommand {
		t.Errorf("the editor contributions are %+v", c)
	}
	if k := c.WindowTabKinds; len(k) != 1 || k[0].ID != PanelKindID || k[0].PlusMenuLabel != "Test Panel" {
		t.Errorf("the window tab kinds are %+v", k)
	}
	if !slices.Contains(m.Events.CrossTab, extension.TopicDocumentChanged) || !slices.Contains(m.Events.CrossTab, extension.TopicSharingRevoked) {
		t.Errorf("the cross-tab topics are %v", m.Events.CrossTab)
	}
	if a, b := NewTabs(), NewTabs(); a == b {
		t.Error("two calls of the factory made one extension")
	}
}

func TestNewPageOpensATabInTheGroupOfTheIconAndInTheActiveGroupFromThePalette(t *testing.T) {
	r := workspaceRig(t)
	if err := r.api.Run(NewPageCommand, map[string]any{"groupId": float64(3)}); err != nil {
		t.Fatal(err)
	}
	tabs := r.api.EditorTabs()
	if len(tabs) != 1 || tabs[0].Group_ != 3 || tabs[0].Title() != untitledPage {
		t.Fatalf("tabs = %+v", tabs)
	}
	if err := r.api.Run(NewPageCommand, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.api.EditorTabs(); len(got) != 2 || got[1].Group_ != 0 || len(r.ext.pages) != 2 {
		t.Errorf("the second page: %+v", got)
	}
}

func TestEditingAPageTitlesTheTabAndKeepsTheTextInItsState(t *testing.T) {
	r := workspaceRig(t)
	if err := r.api.Run(NewPageCommand, nil); err != nil {
		t.Fatal(err)
	}
	tab := r.api.EditorTabs()[0]
	r.ext.pages[0].text.SetValue("Groceries\nmilk")
	r.ext.pages[0].changed()
	if tab.Title() != "Groceries" {
		t.Errorf("the tab is %q", tab.Title())
	}
	var st pageState
	if err := json.Unmarshal(tab.State(), &st); err != nil || st.Text == nil || *st.Text != "Groceries\nmilk" {
		t.Errorf("the state of the tab is %s", tab.State())
	}
}

func TestAClosedTabIsForgottenAndATabComesBackFromItsState(t *testing.T) {
	r := workspaceRig(t)
	if err := r.api.Run(NewPageCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.ext.pages[0].text.SetValue("Ideas")
	r.ext.pages[0].changed()
	state := r.api.EditorTabs()[0].State()
	r.api.EditorTabs()[0].Close()
	if len(r.ext.pages) != 0 {
		t.Errorf("%d pages after the close", len(r.ext.pages))
	}
	// a new run: the tab that was open gets its own state
	second := workspaceRig(t)
	back := second.api.RestoreTab(PageKindID, "t1", state)
	if back.Title() != "Ideas" || len(second.ext.pages) != 1 || second.ext.pages[0].text.Value() != "Ideas" {
		t.Errorf("the restored tab is %q", back.Title())
	}
}

func TestBadPageStatesAreRefusedWithARoute(t *testing.T) {
	r := workspaceRig(t)
	tab := r.api.RestoreTab(PageKindID, "ok", pageStateJSON("fine"))
	for _, tc := range []struct {
		name  string
		state json.RawMessage
	}{
		{"not JSON", json.RawMessage(`nope`)},
		{"no state", nil},
		{"no text", json.RawMessage(`{}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.ext.createPage(tab, tc.state)
			route(t, err, "tabsExt.createPage.TestTabsPageStateInvalid")
		})
	}
}

func TestThePanelKeepsItsTextInTheStateAndItComesBack(t *testing.T) {
	r, tab := panelRig(t, nil)
	if got := string(tab.State()); got != `{"text":""}` {
		t.Errorf("the state of a new tab = %s", got)
	}
	r.ext.panelText.SetValue("buy milk")
	if got := string(tab.State()); got != `{"text":"buy milk"}` {
		t.Errorf("the state = %s", got)
	}
	back, _ := panelRig(t, tab.State())
	if got := back.ext.panelText.Value(); got != "buy milk" {
		t.Errorf("the text after the restart = %q", got)
	}
	// a state that is not JSON of the panel is the error of the tab
	_, err := r.ext.createPanel(tab, json.RawMessage(`[1]`))
	route(t, err, "tabsExt.createPanel.TestTabsPanelStateInvalid")
}

func TestEditingThePanelAsksForTheStateToBeWritten(t *testing.T) {
	r, tab := panelRig(t, nil)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 200}, widgets.NewBuilder(tab.Content.Build))
	ts.Pump()
	r.ext.panelText.SetValue("a")
	r.ext.panelText.SetValue("ab")
	if tab.Changes < 2 {
		t.Errorf("StateChanged was called %d times for two edits", tab.Changes)
	}
	r.api.CloseWindowTab()
}

// other is a tab the events come from.
func other(name string) extension.ScopeInfo {
	return extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: name, Folder: "/work/" + name, ID: "/work/" + name}
}

func change(uri string) extension.DocumentChange {
	return extension.DocumentChange{URI: uri, Version: 2, Cause: "edit", Changes: []extension.TextChange{{Text: "x"}}}
}

func TestThePanelHearsTheEditsOfOtherTabsNewestFirstAndEachAsksForABuild(t *testing.T) {
	r, tab := panelRig(t, nil)
	r.api.EmitFrom(other("api"), extension.TopicDocumentChanged, change("file:///work/api/main.go"))
	r.api.EmitFrom(other("web"), extension.TopicDocumentChanged, change("file:///work/web/app.ts"))
	r.api.EmitFrom(other("api"), extension.TopicDocumentChanged, change("untitled:1"))
	want := []string{"api untitled:1", "web file:///work/web/app.ts", "api file:///work/api/main.go"}
	if !slices.Equal(r.ext.changes, want) {
		t.Errorf("the changes are\n%q\nwant\n%q", r.ext.changes, want)
	}
	if tab.Refreshs != 3 {
		t.Errorf("the tab was built again %d times, want 3", tab.Refreshs)
	}
}

func TestThePanelSaysWhenTheEventsOfATabNoLongerReachIt(t *testing.T) {
	r, tab := panelRig(t, nil)
	r.api.Emit(extension.TopicSharingRevoked, extension.SharingRevoked{From: "/work/api", FromName: "api", Reason: extension.RevokedLinkRemoved})
	if r.ext.revoked != "api" {
		t.Errorf("revoked = %q", r.ext.revoked)
	}
	r.api.Emit(extension.TopicSharingRevoked, extension.SharingRevoked{From: "/work/web", Reason: extension.RevokedPrivate})
	if r.ext.revoked != "/work/web" {
		t.Errorf("revoked = %q", r.ext.revoked)
	}
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 500, Height: 200}, widgets.NewBuilder(tab.Content.Build))
	ts.Pump()
	if !ts.Exists(gluttertest.ByText("The edits of /work/web no longer reach this tab.")) {
		t.Error("the notice is not on screen")
	}
}
