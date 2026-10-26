package exthost

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// The window tabs of the extensions (step 11.7): the kinds, the scope of a tab, its content and state, and its place in the
// links between tabs.

// fakeHost is the strip's view of a test tab: it records what the extension did to the tab.
type fakeHost struct {
	title, hint string
	changed     int
	closed      int
}

func (f *fakeHost) SetTitle(title string) { f.title = title }
func (f *fakeHost) SetHint(hint string)   { f.hint = hint }
func (f *fakeHost) StateChanged()         { f.changed++ }
func (f *fakeHost) Close()                { f.closed++ }

// wtManifest declares the window tab kind wt.tab.
func wtManifest() extension.Manifest {
	m := manifest("wt", extension.OnStartup)
	m.ActivationEvents = nil
	m.Name = "Wt"
	m.Contributes.WindowTabKinds = []extension.WindowTabKindSpec{{ID: "wt.tab", Title: "Wt Tab", Icon: "note", PlusMenuLabel: "Wt Tab..."}}
	m.Contributes.Commands = []extension.CommandSpec{{ID: "wt.hello", Title: "Hello"}}
	m.Events.CrossTab = []string{"watch.**"}
	return m
}

// wtContent is what the test extension puts in its tab; every activation makes one.
type wtContent struct {
	tab     extension.WindowTab
	state   string
	got     json.RawMessage
	built   int
	closed  int
	saveErr error
}

// wtSetup makes the factory of the extension wt, which provides the kind with a content; made records the contents.
func wtSetup(made *[]*wtContent, extra func(api extension.API, c *wtContent) error) extension.Factory {
	f, _ := factory(wtManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			return api.WindowTabs().Provide("wt.tab", extension.WindowTabKind{Create: func(tab extension.WindowTab, state json.RawMessage) (extension.WindowTabContent, error) {
				c := &wtContent{tab: tab, got: state}
				if len(state) > 0 {
					if err := json.Unmarshal(state, &c.state); err != nil {
						return extension.WindowTabContent{}, err
					}
				}
				*made = append(*made, c)
				tab.SetTitle("Wt " + c.state)
				tab.SetHint("hint " + c.state)
				if extra != nil {
					if err := extra(api, c); err != nil {
						return extension.WindowTabContent{}, err
					}
				}
				return extension.WindowTabContent{
					Build: func(bc widgets.BuildContext) widgets.Widget {
						c.built++
						return widgets.NewText("wt:"+c.state, widgets.TextOpts{Style: ui.TextStyle(bc, 13, ui.Theme(bc).Foreground)})
					},
					SaveState: func() (any, error) { return c.state, c.saveErr },
					OnClose:   func() { c.closed++ },
				}, nil
			}})
		}
	})
	return f
}

// newWindowTab makes the scope of a window tab of the kind wt.tab over the host of the rig, as a window makes it: only the
// owner of the kind, no folder, no editor area, no status bar. It does not start it.
func (r *rig) newWindowTab(tabID string, host *fakeHost, disabled ...string) *tab {
	r.t.Helper()
	commands := command.DefaultRegistry()
	tb := &tab{
		t: r.t, commands: commands, menus: command.DefaultMenus(commands),
		keys: &keySink{}, docs: &docSource{texts: map[string]string{}}, disabled: disabled, log: &bytes.Buffer{},
	}
	tb.clock = &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	tb.store = settings.NewStore(r.registry, settings.StoreOptions{UserPath: filepath.Join(r.t.TempDir(), "settings.json")})
	tb.notify = notify.NewService(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), nil)
	tb.scope = r.host.NewScope(ScopeOptions{
		Info: extension.ScopeInfo{Kind: extension.ScopeWindowTab, Name: "Wt Tab", WindowTab: "wt.tab", ID: "wt:" + tabID},
		Ctx:  context.Background(), Commands: commands, Menus: tb.menus, Keybindings: tb.keys, Settings: tb.store,
		Notify: tb.notify, Disabled: func() []string { return tb.disabled },
		Post: tb.post, AskSharing: r.askFunc(tb, "wt:"+tabID), Only: "wt", WindowTab: host,
		EditorsChanged: func() { tb.changed++ },
		Frame:          func(f func()) { tb.evMu.Lock(); tb.frames = append(tb.frames, f); tb.evMu.Unlock() },
		After:          tb.clock.after, Now: tb.clock.nowFunc,
		Logger: slog.New(slog.NewTextHandler(tb.log, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	r.t.Cleanup(tb.scope.Close)
	r.tabs = append(r.tabs, tb)
	return tb
}

// wtRig is a host with the window tab extension and an extension that starts with every scope.
func wtRig(t *testing.T, made *[]*wtContent) (*rig, *made) {
	t.Helper()
	other, rec := factory(manifest("other"), nil)
	return newRig(t, wtSetup(made, nil), other), rec
}

func TestTheKindsListInTheOrderOfTheExtensionsAndGoWithTheExtension(t *testing.T) {
	var contents []*wtContent
	r, _ := wtRig(t, &contents)
	kinds := r.host.WindowTabKinds(nil)
	if len(kinds) != 1 || kinds[0].Extension != "wt" || kinds[0].ExtensionName != "Wt" || kinds[0].Spec.ID != "wt.tab" || kinds[0].Label() != "Wt Tab..." {
		t.Fatalf("the kinds = %+v", kinds)
	}
	if got := r.host.WindowTabKinds([]string{"wt"}); len(got) != 0 {
		t.Errorf("a kind of a disabled extension is listed: %+v", got)
	}
	if got := r.host.WindowTabKinds([]string{"other"}); len(got) != 1 {
		t.Errorf("the kinds with another extension disabled = %+v", got)
	}
	k, ok := r.host.WindowTabKind("wt.tab")
	if !ok || k.Extension != "wt" {
		t.Errorf("WindowTabKind = %+v, %v", k, ok)
	}
	if _, ok := r.host.WindowTabKind("wt.nothing"); ok {
		t.Error("an unknown kind was found")
	}
	if got := (WindowTabKindInfo{Spec: extension.WindowTabKindSpec{ID: "a.b", Title: "Title"}}).Label(); got != "Title" {
		t.Errorf("the label without a PlusMenuLabel = %q", got)
	}
}

func TestAWindowTabScopeHoldsItsOwnerOnlyAndMakesTheContentFromTheSavedState(t *testing.T) {
	var contents []*wtContent
	r, other := wtRig(t, &contents)
	host := &fakeHost{}
	tb := r.newWindowTab("t1", host)
	tb.deferred = true
	tb.scope.SetWindowTabState(json.RawMessage(`"saved"`))
	tb.scope.Start()
	tb.pump()
	if len(contents) != 1 || contents[0].state != "saved" {
		t.Fatalf("Create made %d contents, the state it got is %q", len(contents), string(contents[0].got))
	}
	if st := tb.scope.State("wt"); st != StateActive {
		t.Fatalf("the owner is %v: %v", st, tb.scope.Failure("wt"))
	}
	if len(other.all()) != 0 {
		t.Error("an extension that is not the owner was activated in the scope of a window tab")
	}
	if st := tb.scope.State("other"); st != StateInactive {
		t.Errorf("the state of an extension the scope does not have = %v", st)
	}
	rows := r.host.Rows(nil)
	if len(rows) != 2 || len(rows[0].Scopes) != 1 || rows[0].Scopes[0].Scope != "Wt Tab" || len(rows[1].Scopes) != 0 {
		t.Errorf("the rows = %+v", rows)
	}
	if host.title != "Wt saved" || host.hint != "hint saved" {
		t.Errorf("the title and the hint the extension set: %q %q", host.title, host.hint)
	}
	if info := tb.scope.Info(); info.Kind != extension.ScopeWindowTab || info.WindowTab != "wt.tab" || info.ID != "wt:t1" {
		t.Errorf("the scope = %+v", info)
	}
	if contents[0].tab.ID() != "t1" || contents[0].tab.Kind() != "wt.tab" {
		t.Errorf("the tab = %q %q", contents[0].tab.ID(), contents[0].tab.Kind())
	}
	// the content is drawn in a box
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 100}, tb.scope.WindowTabBody())
	ts.Pump()
	if !ts.Exists(gluttertest.ByText("wt:saved")) {
		t.Error("the content of the tab is not on screen")
	}
	// the declared command is in the registry of the tab
	if _, ok := tb.commands.Lookup("wt.hello"); !ok {
		t.Error("the command the owner declares is not in the tab")
	}
	// a request for a refresh, the state and the change notice reach the host
	contents[0].tab.Refresh()
	contents[0].tab.StateChanged()
	contents[0].tab.SetTitle("Renamed")
	contents[0].tab.Close()
	tb.pump()
	if host.changed != 1 || host.closed != 1 || host.title != "Renamed" {
		t.Errorf("the host heard: changed %d, closed %d, title %q", host.changed, host.closed, host.title)
	}
}

func TestTheStateOfAWindowTabIsKeptWhenItsExtensionIsTurnedOffAndComesBack(t *testing.T) {
	var contents []*wtContent
	r, _ := wtRig(t, &contents)
	tb := r.newWindowTab("t1", &fakeHost{})
	tb.deferred = true
	tb.scope.Start()
	tb.pump()
	contents[0].state = "typed"
	if got := string(tb.scope.WindowTabState()); got != `"typed"` {
		t.Fatalf("the state of the tab = %q", got)
	}
	// turn the owner off: the content goes, the tab keeps its state and says so
	tb.changeSetting("extensions.disabled", []any{"wt"})
	tb.disabled = []string{"wt"}
	tb.scope.Refresh()
	tb.pump()
	if st := tb.scope.State("wt"); st != StateDisabled {
		t.Fatalf("the owner is %v", st)
	}
	if got := string(tb.scope.WindowTabState()); got != `"typed"` {
		t.Errorf("the state after the owner went = %q", got)
	}
	if tb.changed == 0 {
		t.Error("the window was not told that the tab changed")
	}
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 100}, tb.scope.WindowTabBody())
	ts.Pump()
	if !ts.Exists(gluttertest.ByText("Wt is turned off. Turn it on in the Extensions page to use this tab.")) {
		t.Error("a tab of a turned off extension says nothing")
	}
	// turn it on again: the content is made with the state
	tb.disabled = nil
	tb.scope.Refresh()
	tb.pump()
	if st := tb.scope.State("wt"); st != StateActive {
		t.Fatalf("the owner is %v after it was turned on", st)
	}
	if len(contents) != 2 || contents[1].state != "typed" {
		t.Errorf("the contents = %d, the state of the second = %q", len(contents), contents[len(contents)-1].state)
	}
}

func TestClosingAWindowTabTellsTheContentAndDropsTheState(t *testing.T) {
	var contents []*wtContent
	r, _ := wtRig(t, &contents)
	tb := r.newWindowTab("t1", &fakeHost{})
	tb.deferred = true
	tb.scope.SetWindowTabState(json.RawMessage(`"x"`))
	tb.scope.Start()
	tb.pump()
	tb.scope.WindowTabClosed()
	if contents[0].closed != 1 {
		t.Errorf("OnClose was called %d times", contents[0].closed)
	}
	if got := tb.scope.WindowTabState(); got != nil {
		t.Errorf("the state after the close = %q", string(got))
	}
	tb.scope.WindowTabClosed() // twice does nothing more
	if contents[0].closed != 1 {
		t.Errorf("OnClose was called %d times after a second close", contents[0].closed)
	}
	// a scope that is not a window tab has nothing to close
	ws := r.newTab("one")
	ws.scope.WindowTabClosed()
	if ws.scope.WindowTabState() != nil {
		t.Error("a workspace scope has a window tab state")
	}
	ws.scope.SetWindowTabState(json.RawMessage(`"x"`))
}

func TestWindowTabsFailInsideTheirOwnScope(t *testing.T) {
	tests := []struct {
		name  string
		setup func(api extension.API) extension.WindowTabKind
		route string
		tag   string
		where string
	}{
		{"Create panics", func(extension.API) extension.WindowTabKind {
			return extension.WindowTabKind{Create: func(extension.WindowTab, json.RawMessage) (extension.WindowTabContent, error) { panic("boom") }}
		}, "makeWindowTabContent.func1.1.ExtensionWindowTabCreatePanicked", "ExtensionWindowTabCreatePanicked", "window tab kind wt.tab"},
		{"OnClose panics", func(extension.API) extension.WindowTabKind {
			return extension.WindowTabKind{Create: func(extension.WindowTab, json.RawMessage) (extension.WindowTabContent, error) {
				return extension.WindowTabContent{
					Build:   func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) },
					OnClose: func() { panic("close") },
				}, nil
			}}
		}, "runWindowTabCallback.func1.ExtensionWindowTabCallbackPanicked", "ExtensionWindowTabCallbackPanicked", "window tab OnClose"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := factory(wtManifest(), func(f *fake) {
				f.activate = func(_ context.Context, api extension.API) error {
					return api.WindowTabs().Provide("wt.tab", tt.setup(api))
				}
			})
			r := newRig(t, f)
			tb := r.newWindowTab("t1", &fakeHost{})
			tb.deferred = true
			tb.scope.Start()
			tb.pump()
			tb.scope.WindowTabClosed()
			tb.pump()
			if st := tb.scope.State("wt"); st != StateFailed {
				t.Fatalf("the owner is %v", st)
			}
			fail := tb.scope.Failure("wt")
			if fail == nil || fail.Where != tt.where || fail.BoxText != "Wt failed" {
				t.Fatalf("the failure = %+v", fail)
			}
			route(t, fail.Err, tt.route)
			route(t, fail.Err, tt.tag)
			// the other scopes are untouched, and the tab shows the failure
			ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 100}, tb.scope.WindowTabBody())
			ts.Pump()
			if !ts.Exists(gluttertest.ByText("Wt failed")) {
				t.Error("the tab of a failed extension does not say so")
			}
		})
	}
}

func TestAPanicInTheBuildOfAWindowTabFailsTheExtensionAndTheBoxShowsIt(t *testing.T) {
	f, _ := factory(wtManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			return api.WindowTabs().Provide("wt.tab", extension.WindowTabKind{Create: func(extension.WindowTab, json.RawMessage) (extension.WindowTabContent, error) {
				return extension.WindowTabContent{Build: func(widgets.BuildContext) widgets.Widget { panic("build") }}, nil
			}})
		}
	})
	r := newRig(t, f)
	tb := r.newWindowTab("t1", &fakeHost{})
	tb.scope.Start()
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 100}, tb.scope.WindowTabBody())
	ts.Pump()
	ts.Pump()
	if st := tb.scope.State("wt"); st != StateFailed {
		t.Fatalf("the owner is %v", st)
	}
	route(t, tb.scope.Failure("wt").Err, "ExtensionViewBuildPanicked")
	if !ts.Exists(gluttertest.ByText("Wt failed")) {
		t.Error("the box does not say that the extension failed")
	}
}

func TestACreateThatReturnsAnErrorLeavesTheTabWithTheError(t *testing.T) {
	cases := []struct {
		name    string
		content extension.WindowTabContent
		err     error
		want    string
	}{
		{"an error", extension.WindowTabContent{}, context.DeadlineExceeded, "This tab could not be made: context deadline exceeded"},
		{"no Build", extension.WindowTabContent{}, nil, "This tab could not be made: the content of a window tab needs a Build function"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := factory(wtManifest(), func(f *fake) {
				f.activate = func(_ context.Context, api extension.API) error {
					return api.WindowTabs().Provide("wt.tab", extension.WindowTabKind{Create: func(extension.WindowTab, json.RawMessage) (extension.WindowTabContent, error) {
						return tt.content, tt.err
					}})
				}
			})
			r := newRig(t, f)
			tb := r.newWindowTab("t1", &fakeHost{})
			tb.scope.Start()
			if st := tb.scope.State("wt"); st != StateActive {
				t.Fatalf("a Create that fails with an error fails the extension: %v", st)
			}
			ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 500, Height: 100}, tb.scope.WindowTabBody())
			ts.Pump()
			if !ts.Exists(gluttertest.ByText(tt.want)) {
				t.Errorf("the tab does not say %q", tt.want)
			}
		})
	}
}

func TestProvideAWindowTabKindChecksWhatItIsGiven(t *testing.T) {
	create := func(extension.WindowTab, json.RawMessage) (extension.WindowTabContent, error) {
		return extension.WindowTabContent{Build: func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) }}, nil
	}
	tests := []struct {
		name  string
		kind  string
		in    func(r *rig) *tab
		call  func(api extension.API, kind string) error
		route string
	}{
		{"a kind the manifest does not declare", "wt.other", func(r *rig) *tab { return r.newWindowTab("t1", &fakeHost{}) },
			func(api extension.API, kind string) error {
				return api.WindowTabs().Provide(kind, extension.WindowTabKind{Create: create})
			}, "windowTabsAPI.Provide.ExtensionWindowTabKindNotDeclared"},
		{"a scope that is a workspace", "wt.tab", func(r *rig) *tab { return r.newTab("one") },
			func(api extension.API, kind string) error {
				return api.WindowTabs().Provide(kind, extension.WindowTabKind{Create: create})
			}, "windowTabsAPI.Provide.ExtensionWindowTabUnavailable"},
		{"no Create", "wt.tab", func(r *rig) *tab { return r.newWindowTab("t1", &fakeHost{}) },
			func(api extension.API, kind string) error {
				return api.WindowTabs().Provide(kind, extension.WindowTabKind{})
			}, "windowTabsAPI.Provide.ExtensionWindowTabKindInvalid"},
		{"the same kind twice", "wt.tab", func(r *rig) *tab { return r.newWindowTab("t1", &fakeHost{}) },
			func(api extension.API, kind string) error {
				if err := api.WindowTabs().Provide(kind, extension.WindowTabKind{Create: create}); err != nil {
					return err
				}
				return api.WindowTabs().Provide(kind, extension.WindowTabKind{Create: create})
			}, "windowTabsAPI.Provide.ExtensionWindowTabKindProvided"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got error
			f, _ := factory(wtManifest(), func(f *fake) {
				f.activate = func(_ context.Context, api extension.API) error {
					got = tt.call(api, tt.kind)
					return nil
				}
			})
			r := newRig(t, f)
			tb := tt.in(r)
			if tb.scope.Info().Kind == extension.ScopeWorkspace {
				tb.scope.Fire(extension.OnWindowTab("wt.tab"))
			} else {
				tb.scope.Start()
			}
			if got == nil {
				t.Fatal("Provide did not fail")
			}
			route(t, got, tt.route)
		})
	}
}

func TestAWindowTabScopeHasNoStatusBar(t *testing.T) {
	var got error
	f, _ := factory(wtManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			_, got = api.StatusBar().Add(extension.StatusItem{ID: "x", Text: "x"})
			return nil
		}
	})
	r := newRig(t, f)
	r.newWindowTab("t1", &fakeHost{}).scope.Start()
	if got == nil {
		t.Fatal("a status bar item was added in a window tab")
	}
	route(t, got, "statusBarAPI.Add.ExtensionStatusBarUnavailable")
}

func TestAWindowTabOfAnUnknownKindShowsANotice(t *testing.T) {
	var contents []*wtContent
	r, _ := wtRig(t, &contents)
	// the scope names a kind the owner does not declare: it has no tab, and the body says so
	s := r.host.NewScope(ScopeOptions{Info: extension.ScopeInfo{Kind: extension.ScopeWindowTab, Name: "x", WindowTab: "wt.gone", ID: "wt:t9"}, Only: "wt"})
	t.Cleanup(s.Close)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 500, Height: 100}, s.WindowTabBody())
	ts.Pump()
	if !ts.Exists(gluttertest.ByText("This tab needs the extension that made it, which this Emetos does not have.")) {
		t.Error("the notice is not there")
	}
}

func TestAWindowTabScopeGetsNoEventsWithoutALinkAndGetsThemAfterAllow(t *testing.T) {
	var contents []*wtContent
	var received []string
	// the window tab extension listens to the events of the other tabs
	f, _ := factory(wtManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			if _, err := api.Events().Subscribe("watch.ping", func(e extension.Event) {
				received = append(received, e.Scope.Name+":"+e.Topic)
			}, extension.FromOtherScopes()); err != nil {
				return err
			}
			return api.WindowTabs().Provide("wt.tab", extension.WindowTabKind{Create: func(extension.WindowTab, json.RawMessage) (extension.WindowTabContent, error) {
				contents = append(contents, &wtContent{})
				return extension.WindowTabContent{Build: func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) }}, nil
			}})
		}
	})
	// the other tab is a workspace with an extension that publishes watch.ping
	pm := manifest("watch")
	pm.Events = extension.EventDecl{CrossTab: []string{"watch.**"}}
	var pub extension.API
	p, _ := factory(pm, func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error { pub = api; return nil }
	})
	r := newRig(t, f, p)
	notes := r.newTab("notes")
	notes.scope.Start()
	notes.deferred = true
	win := r.newWindowTab("t1", &fakeHost{})
	win.scope.Start()
	win.deferred = true
	notes.pump()
	win.pump()
	publish := func() {
		t.Helper()
		if err := pub.Events().Publish("watch.ping", map[string]string{"n": "1"}); err != nil {
			t.Fatal(err)
		}
		notes.pump()
		win.pump()
	}
	// the question comes with the first event, and that event is lost
	publish()
	if len(win.asked) != 1 {
		t.Fatalf("the window tab was asked %d questions, want 1", len(win.asked))
	}
	q := win.asked[0]
	if q.To != "Wt Tab" || q.ToID != "wt:t1" || q.From != "notes" || q.FromID != "/work/notes" || !slices.Equal(q.Topics, []string{"watch.ping"}) {
		t.Errorf("the question = %+v", q)
	}
	if len(received) != 0 {
		t.Fatalf("the window tab heard %v without a link", received)
	}
	win.answers[0](SharingAllow)
	win.pump()
	publish()
	if !slices.Equal(received, []string{"notes:watch.ping"}) {
		t.Errorf("the window tab heard %v after Allow", received)
	}
	if links := r.sharing.Snapshot().Links; len(links) != 1 || links[0].From != "/work/notes" || links[0].To != "wt:t1" {
		t.Errorf("the saved links = %+v", links)
	}
}

func TestAWindowTabContentWithoutABuildIsRefusedWithItsOwnTag(t *testing.T) {
	f, _ := factory(wtManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			return api.WindowTabs().Provide("wt.tab", extension.WindowTabKind{Create: func(extension.WindowTab, json.RawMessage) (extension.WindowTabContent, error) {
				return extension.WindowTabContent{}, nil
			}})
		}
	})
	r := newRig(t, f)
	tb := r.newWindowTab("t1", &fakeHost{})
	tb.scope.Start()
	wt := tb.scope.wtab
	if wt == nil || wt.inst == nil {
		t.Fatal("the tab was not given to the extension")
	}
	kind := extension.WindowTabKind{Create: func(extension.WindowTab, json.RawMessage) (extension.WindowTabContent, error) {
		return extension.WindowTabContent{}, nil
	}}
	err := tb.scope.makeWindowTabContent(wt, wt.inst, kind, nil)
	route(t, err, "Scope.makeWindowTabContent.ExtensionWindowTabContentInvalid")
}
