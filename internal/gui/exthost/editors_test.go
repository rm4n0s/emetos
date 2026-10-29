package exthost

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	apperrors "github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
)

// The contributions to the editor area (step 11.6): editor tab kinds, editor title actions and file views.

// route asserts the route of an error.
func route(t *testing.T, err error, want string) {
	t.Helper()
	appErr, ok := apperrors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}

// edManifest declares an editor kind, two title actions and two file views.
func edManifest() extension.Manifest {
	m := extension.Manifest{ID: "ed", Name: "Ed", Version: "1.0.0", ActivationEvents: nil}
	m.Contributes.Commands = []extension.CommandSpec{{ID: "ed.new", Title: "New Thing"}, {ID: "ed.other", Title: "Other"}, {ID: "ed.more", Title: "More"}}
	m.Contributes.EditorKinds = []extension.EditorKindSpec{{ID: "ed.thing", Name: "Thing", Icon: "note"}}
	m.Contributes.EditorActions = []extension.EditorActionSpec{
		{Command: "ed.new", Icon: "new-file", When: "activeEditor != ed.never", Order: 2},
		{Command: "ed.other", Icon: "add", Tooltip: "Other thing", When: "resourceLangId == markdown", Order: 1},
		{Command: "ed.more", Icon: "refresh"},
	}
	m.Contributes.FileViews = []extension.FileViewSpec{
		{ID: "ed.preview", Name: "Preview", Icon: "preview", Languages: []string{"markdown"}, Order: 1},
		{ID: "ed.grid", Name: "Grid", Globs: []string{"*.csv", "data/**/*.tsv"}},
	}
	return m
}

// thing is the content of a tab that shows a text and keeps a state.
type thing struct {
	tab    extension.EditorTab
	state  string
	closed int
}

// thingKind is the kind the test extension provides; created are the tabs it made with the states they got.
type thingKind struct {
	created []*thing
	states  []string
}

func (k *thingKind) kind() extension.EditorKind {
	return extension.EditorKind{Create: func(tab extension.EditorTab, state json.RawMessage) (extension.EditorTabContent, error) {
		th := &thing{tab: tab}
		if len(state) > 0 {
			if err := json.Unmarshal(state, &th.state); err != nil {
				return extension.EditorTabContent{}, err
			}
		}
		k.created = append(k.created, th)
		k.states = append(k.states, th.state)
		tab.SetTitle("Thing " + th.state)
		return extension.EditorTabContent{
			Build: func(bc widgets.BuildContext) widgets.Widget {
				return widgets.NewText("thing:"+th.state, widgets.TextOpts{Style: ui.TextStyle(bc, 13, ui.Theme(bc).Foreground)})
			},
			SaveState: func() (any, error) { return th.state, nil },
			OnClose:   func() { th.closed++ },
		}, nil
	}}
}

// edTab is a deferred tab with the extension ed, started, over the kind k.
func edTab(t *testing.T, k *thingKind, setup func(api extension.API) error) *tab {
	t.Helper()
	f, _ := factory(edManifest(), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			if k != nil {
				if err := api.Editors().Provide("ed.thing", k.kind()); err != nil {
					return err
				}
			}
			if err := api.Commands().Handle("ed.new", func(context.Context, any) error { return nil }); err != nil {
				return err
			}
			if setup != nil {
				return setup(api)
			}
			return nil
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.deferred = true
	tb.scope.Start()
	tb.pump()
	return tb
}

// activate activates the extension ed by the event of its kind.
func (tb *tab) activate() {
	tb.t.Helper()
	tb.scope.Fire(extension.OnEditor("ed.thing"))
	tb.pump()
	if st := tb.scope.State("ed"); st != StateActive {
		tb.t.Fatalf("ed is %v: %v", st, tb.scope.Failure("ed"))
	}
}

// editorsOf is the API of the activation of ed, for calls that a test makes as the extension does.
func apiOf(tb *tab) extension.API {
	tb.scope.mu.Lock()
	defer tb.scope.mu.Unlock()
	for _, sl := range tb.scope.slots {
		if sl.info.Manifest.ID == "ed" && sl.inst != nil {
			return &api{inst: sl.inst}
		}
	}
	return nil
}

func TestAnEditorTabOpensWithItsStateAndTheMarksReachTheInput(t *testing.T) {
	k := &thingKind{}
	tb := edTab(t, k, nil)
	tb.activate()
	a := apiOf(tb)
	opened, err := a.Editors().Open("ed.thing", "hello", extension.EditorOpenOptions{Group: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.editors.opened) != 1 {
		t.Fatalf("the window was asked to open %d tabs", len(tb.editors.opened))
	}
	in := tb.editors.opened[0]
	if in.Extension() != "ed" || in.Kind() != "ed.thing" || in.Name() != "Thing hello" || in.Icon() != "note" || in.ID() != opened.ID() {
		t.Errorf("the input = %q %q %q %q", in.Extension(), in.Kind(), in.Name(), in.Icon())
	}
	if tb.editors.groups[in] != 3 || opened.Group() != 3 {
		t.Errorf("the group of the tab is %d, %d, want 3", tb.editors.groups[in], opened.Group())
	}
	if !slices.Equal(k.states, []string{"hello"}) {
		t.Errorf("Create got the states %v", k.states)
	}
	opened.SetTitle("Renamed")
	opened.SetHint("tooltip")
	opened.SetDirty(true)
	opened.SetBusy(true)
	if in.Name() != "Renamed" || in.Title() != "tooltip" || !in.Dirty() || !in.Busy() {
		t.Errorf("after the setters: %q %q dirty %v busy %v", in.Name(), in.Title(), in.Dirty(), in.Busy())
	}
	opened.Reveal(true)
	tb.pump()
	opened.Close()
	tb.pump()
	if len(tb.editors.revealed) != 1 || len(tb.editors.closed) != 1 {
		t.Errorf("revealed %d, closed %d", len(tb.editors.revealed), len(tb.editors.closed))
	}
	// the tab draws its content in a box
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 100}, tb.scope.TabBody(in))
	ts.Pump()
	if !ts.Exists(gluttertest.ByText("thing:hello")) {
		t.Error("the content of the tab is not on screen")
	}
}

func TestSeveralInstancesAreTabsOfTheirOwnAndCopiesKeepTheState(t *testing.T) {
	k := &thingKind{}
	tb := edTab(t, k, nil)
	tb.activate()
	a := apiOf(tb)
	for _, s := range []string{"a", "b"} {
		if _, err := a.Editors().Open("ed.thing", s, extension.EditorOpenOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	first, second := tb.editors.opened[0], tb.editors.opened[1]
	if first.ID() == second.ID() || first.Matches(second) || first.TabKey() == second.TabKey() {
		t.Error("two instances are the same tab")
	}
	if got := a.Editors().Tabs("ed.thing"); len(got) != 2 {
		t.Errorf("Tabs lists %d tabs", len(got))
	}
	// a split makes another instance, with the state the first one saves now
	k.created[0].state = "a2"
	copied, ok := first.CopyInput().(*editorgroup.ExtensionTabInput)
	if !ok || copied.ID() == first.ID() || copied.Kind() != "ed.thing" {
		t.Fatalf("the copy = %v", copied)
	}
	if got := k.states[len(k.states)-1]; got != "a2" {
		t.Errorf("the copy was made with the state %q, want a2", got)
	}
	if got := a.Editors().Tabs("ed.thing"); len(got) != 3 {
		t.Errorf("Tabs lists %d tabs after the copy", len(got))
	}
}

func TestTheStateOfATabIsKeptWithTheWindowAndGivenBackOnRestore(t *testing.T) {
	k := &thingKind{}
	tb := edTab(t, k, nil)
	tb.activate()
	a := apiOf(tb)
	if _, err := a.Editors().Open("ed.thing", "kept", extension.EditorOpenOptions{}); err != nil {
		t.Fatal(err)
	}
	in := tb.editors.opened[0]
	k.created[0].state = "kept-later"
	states := tb.scope.ViewStates()
	raw, ok := states[tabKey(in.ID())]
	if !ok || string(raw) != `"kept-later"` {
		t.Fatalf("the saved states are %v", states)
	}
	// a new session: the input comes back from its saved value, the state from the window's, and the extension is
	// activated when the tab is on screen
	value, _ := in.Serialize()
	restored, err := editorgroup.ParseExtensionTabInput(value)
	if err != nil {
		t.Fatal(err)
	}
	k2 := &thingKind{}
	tb2 := edTab(t, k2, nil)
	tb2.scope.SetViewStates(states)
	tb2.scope.RegisterTab(restored)
	if len(k2.created) != 0 || tb2.scope.State("ed") != StateInactive {
		t.Fatal("the extension ran before the tab was shown")
	}
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 100}, tb2.scope.TabBody(restored))
	for i := 0; i < 3; i++ {
		tb2.pump()
		ts.Pump()
	}
	if tb2.scope.State("ed") != StateActive || !slices.Equal(k2.states, []string{"kept-later"}) {
		t.Fatalf("ed is %v, Create got %v", tb2.scope.State("ed"), k2.states)
	}
	if !ts.Exists(gluttertest.ByText("thing:kept-later")) {
		t.Error("the restored tab shows nothing")
	}
}

func TestClosingATabTellsTheExtensionAndDropsItsState(t *testing.T) {
	k := &thingKind{}
	tb := edTab(t, k, nil)
	tb.activate()
	if _, err := apiOf(tb).Editors().Open("ed.thing", "x", extension.EditorOpenOptions{}); err != nil {
		t.Fatal(err)
	}
	in := tb.editors.opened[0]
	tb.scope.ViewStates()
	tb.scope.TabClosed(in)
	if k.created[0].closed != 1 {
		t.Errorf("OnClose ran %d times", k.created[0].closed)
	}
	if _, ok := tb.scope.ViewStates()[tabKey(in.ID())]; ok {
		t.Error("the state of a closed tab is kept")
	}
	tb.scope.TabClosed(in)
	if k.created[0].closed != 1 {
		t.Error("OnClose ran again for the same tab")
	}
}

func TestATabOfAnExtensionThatIsOffShowsWhy(t *testing.T) {
	k := &thingKind{}
	tb := edTab(t, k, nil)
	tb.activate()
	if _, err := apiOf(tb).Editors().Open("ed.thing", "x", extension.EditorOpenOptions{}); err != nil {
		t.Fatal(err)
	}
	in := tb.editors.opened[0]
	tb.disabled = []string{"ed"}
	tb.scope.Refresh()
	tb.pump()
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 100}, tb.scope.TabBody(in))
	ts.Pump()
	if !ts.Exists(gluttertest.ByText("Ed is turned off. Turn it on in the Extensions page to use this tab.")) {
		t.Error("the tab of a disabled extension does not say so")
	}
	// the extension that was never part of this Emetos
	stranger := editorgroup.NewExtensionTabInput("nobody", "nobody.kind", "zz", "Stranger", "")
	ts = uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 100}, tb.scope.TabBody(stranger))
	ts.Pump()
	if !ts.Exists(gluttertest.ByText("This tab needs the extension nobody, which this Emetos does not have.")) {
		t.Error("the tab of an unknown extension does not say so")
	}
	// turned on again, the content comes back with the state it had
	tb.disabled = nil
	tb.scope.Refresh()
	tb.pump()
	ts = uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 400, Height: 100}, tb.scope.TabBody(in))
	for i := 0; i < 3; i++ {
		tb.pump()
		ts.Pump()
	}
	if !ts.Exists(gluttertest.ByText("thing:x")) {
		t.Error("the tab did not come back when the extension was turned on")
	}
}

func TestEditorKindErrorsHaveRoutes(t *testing.T) {
	k := &thingKind{}
	tb := edTab(t, k, nil)
	tb.activate()
	a := apiOf(tb)
	cases := []struct {
		name  string
		err   func() error
		route string
	}{
		{"not declared", func() error { return a.Editors().Provide("ed.nope", k.kind()) }, "editorsAPI.Provide.ExtensionEditorKindNotDeclared"},
		{"provided twice", func() error { return a.Editors().Provide("ed.thing", k.kind()) }, "editorsAPI.Provide.ExtensionEditorKindProvided"},
		{"no Create", func() error { return a.Editors().Provide("ed.thing", extension.EditorKind{}) }, "editorsAPI.Provide.ExtensionEditorKindInvalid"},
		{"not provided", func() error {
			_, err := a.Editors().Open("ed.nope", nil, extension.EditorOpenOptions{})
			return err
		}, "editorsAPI.Open.ExtensionEditorKindNotProvided"},
		{"state that is not JSON", func() error {
			_, err := a.Editors().Open("ed.thing", make(chan int), extension.EditorOpenOptions{})
			return err
		}, "editorsAPI.Open.ExtensionEditorStateInvalid"},
		{"no editor area", func() error {
			tb.scope.opts.Editors = nil
			defer func() { tb.scope.opts.Editors = tb.editors }()
			_, err := a.Editors().Open("ed.thing", nil, extension.EditorOpenOptions{})
			return err
		}, "editorsAPI.Open.ExtensionEditorsUnavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { route(t, tc.err(), tc.route) })
	}
	// the tab the window refused to open is forgotten
	tb.editors.failOpen = apperrors.New("NoRoom", "no room")
	if _, err := a.Editors().Open("ed.thing", nil, extension.EditorOpenOptions{}); err == nil {
		t.Error("a refused tab was opened")
	}
	if got := len(a.Editors().Tabs("ed.thing")); got != 0 {
		t.Errorf("%d tabs are known after a refusal", got)
	}
	// a tab whose Create has no widget
	noBuild := extension.EditorKind{Create: func(extension.EditorTab, json.RawMessage) (extension.EditorTabContent, error) {
		return extension.EditorTabContent{}, nil
	}}
	tb2 := edTab(t, nil, func(api extension.API) error { return api.Editors().Provide("ed.thing", noBuild) })
	tb2.activate()
	_, err := apiOf(tb2).Editors().Open("ed.thing", nil, extension.EditorOpenOptions{})
	route(t, err, "Scope.makeContent.ExtensionEditorContentInvalid")
}

func TestAPanicInCreateOrInOnCloseFailsTheExtension(t *testing.T) {
	boom := extension.EditorKind{Create: func(extension.EditorTab, json.RawMessage) (extension.EditorTabContent, error) { panic("create") }}
	tb := edTab(t, nil, func(api extension.API) error { return api.Editors().Provide("ed.thing", boom) })
	tb.activate()
	_, err := apiOf(tb).Editors().Open("ed.thing", nil, extension.EditorOpenOptions{})
	route(t, err, "Scope.makeContent.func1.1.ExtensionEditorCreatePanicked")
	tb.pump()
	if tb.scope.State("ed") != StateFailed {
		t.Errorf("ed is %v after the panic", tb.scope.State("ed"))
	}
	route(t, tb.scope.Failure("ed").Err, "ExtensionEditorCreatePanicked")

	onClose := extension.EditorKind{Create: func(extension.EditorTab, json.RawMessage) (extension.EditorTabContent, error) {
		return extension.EditorTabContent{Build: func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) }, OnClose: func() { panic("close") }}, nil
	}}
	tb2 := edTab(t, nil, func(api extension.API) error { return api.Editors().Provide("ed.thing", onClose) })
	tb2.activate()
	if _, err := apiOf(tb2).Editors().Open("ed.thing", nil, extension.EditorOpenOptions{}); err != nil {
		t.Fatal(err)
	}
	tb2.scope.TabClosed(tb2.editors.opened[0])
	tb2.pump()
	if tb2.scope.State("ed") != StateFailed {
		t.Errorf("ed is %v after the panic in OnClose", tb2.scope.State("ed"))
	}
	route(t, tb2.scope.Failure("ed").Err, "Scope.runTabCallback.func1.ExtensionEditorCallbackPanicked")
}

// keysFor is the context a group with an editor of a language has.
func keysFor(lang string) contextkey.Context {
	root := contextkey.NewRoot()
	root.Set("resourceLangId", lang)
	root.Set("activeEditor", "emetos.textEditor")
	return root
}

func TestEditorTitleActionsAppearByTheirConditionInTheOrderOfTheirOrder(t *testing.T) {
	tb := edTab(t, nil, nil)
	got := func(lang string) []string {
		var out []string
		for _, a := range tb.scope.EditorActions(keysFor(lang)) {
			out = append(out, a.Command)
		}
		return out
	}
	// ed.more has no order (0), ed.other order 1, ed.new order 2 as the manifest gives them: the lowest first, in the order
	// they are declared among the equal ones
	if want := []string{"ed.more", "ed.other", "ed.new"}; !slices.Equal(got("markdown"), want) {
		t.Errorf("the actions for a Markdown file are %v, want %v", got("markdown"), want)
	}
	if want := []string{"ed.more", "ed.new"}; !slices.Equal(got("go"), want) {
		t.Errorf("the actions for a Go file are %v, want %v", got("go"), want)
	}
	actions := tb.scope.EditorActions(keysFor("markdown"))
	if actions[1].Tooltip != "Other thing" || actions[2].Tooltip != "New Thing" || actions[0].Icon == 0 {
		t.Errorf("the tooltips and icons are %+v", actions)
	}
	// the extension is off: no icon, and the window is told
	before := tb.changed
	tb.disabled = []string{"ed"}
	tb.scope.Refresh()
	if len(tb.scope.EditorActions(keysFor("markdown"))) != 0 || tb.changed == before {
		t.Errorf("a disabled extension still has actions, or the window was not told (%d, %d)", tb.changed, before)
	}
}

func TestEditorTitleActionsOfTwoExtensionsAreOrderedByExtension(t *testing.T) {
	mk := func(id string) extension.Factory {
		m := extension.Manifest{ID: id, Name: id}
		m.Contributes.Commands = []extension.CommandSpec{{ID: id + ".go", Title: id}}
		m.Contributes.EditorActions = []extension.EditorActionSpec{{Command: id + ".go", Icon: "add", Order: 1}}
		f, _ := factory(m, nil)
		return f
	}
	r := newRig(t, mk("first"), mk("second"))
	tb := r.newTab("one")
	var got []string
	for _, a := range tb.scope.EditorActions(nil) {
		got = append(got, a.Command)
	}
	if want := []string{"first.go", "second.go"}; !slices.Equal(got, want) {
		t.Errorf("the actions are %v, want %v", got, want)
	}
}

// fakeDoc is a document handle for the file views.
type fakeDoc struct {
	uri, lang string
	text      string
	onChange  []func(extension.ViewDocumentChange)
	top       int
}

func (d *fakeDoc) URI() string                     { return d.uri }
func (d *fakeDoc) LanguageID() string              { return d.lang }
func (d *fakeDoc) Version() int                    { return 1 }
func (d *fakeDoc) Text() string                    { return d.text }
func (d *fakeDoc) ReadOnly() bool                  { return false }
func (d *fakeDoc) Folder() string                  { return "/work/one" }
func (d *fakeDoc) Edit([]extension.TextEdit) error { return nil }
func (d *fakeDoc) OnChange(fn func(extension.ViewDocumentChange)) func() {
	d.onChange = append(d.onChange, fn)
	return func() {}
}
func (d *fakeDoc) TopLine() int              { return d.top }
func (d *fakeDoc) OnScroll(func(int)) func() { return func() {} }
func (d *fakeDoc) OpenFile(string) error     { return nil }

func TestFileViewsAreSelectedByLanguageAndGlobAndOrdered(t *testing.T) {
	tb := edTab(t, nil, nil)
	ids := func(file, lang string) []string {
		var out []string
		for _, v := range tb.scope.FileViewsFor(file, lang, "/work/one") {
			out = append(out, v.ID)
		}
		return out
	}
	cases := []struct {
		name, file, lang string
		want             []string
	}{
		{"markdown by language", "/work/one/a.txt", "markdown", []string{"ed.preview"}},
		{"csv by glob", "/work/one/sales.csv", "plaintext", []string{"ed.grid"}},
		{"tsv by a path glob", "/work/one/data/2026/q1.tsv", "plaintext", []string{"ed.grid"}},
		{"tsv outside the folder of the glob", "/work/one/other/q1.tsv", "plaintext", nil},
		{"a file that is no one's", "/work/one/main.go", "go", nil},
		{"both", "/work/one/notes.csv", "markdown", []string{"ed.preview", "ed.grid"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(tc.file, tc.lang); !slices.Equal(got, tc.want) {
				t.Errorf("the views are %v, want %v", got, tc.want)
			}
		})
	}
	// they go with the extension
	tb.disabled = []string{"ed"}
	tb.scope.Refresh()
	if got := ids("/work/one/notes.csv", "markdown"); len(got) != 0 {
		t.Errorf("a disabled extension still has views: %v", got)
	}
}

// previewProvider provides the file view "ed.preview" with a text of the document.
func previewProvider(opened *int, opens *[]*fakeDoc) func(api extension.API) error {
	return func(api extension.API) error {
		return api.FileViews().Provide("ed.preview", extension.FileViewProvider{Open: func(doc extension.DocumentHandle, tab extension.FileViewTab) (extension.FileViewContent, error) {
			*opened++
			*opens = append(*opens, doc.(guardedDoc).DocumentHandle.(*fakeDoc))
			doc.OnChange(func(extension.ViewDocumentChange) { tab.Refresh() })
			return extension.FileViewContent{Build: func(bc widgets.BuildContext) widgets.Widget {
				return widgets.NewText("preview:"+doc.Text(), widgets.TextOpts{Style: ui.TextStyle(bc, 13, ui.Theme(bc).Foreground)})
			}}, nil
		}})
	}
}

func TestAFileViewIsBuiltInABoxOverTheDocumentAndKeptWhileTheTabLives(t *testing.T) {
	var opened int
	var docs []*fakeDoc
	tb := edTab(t, nil, previewProvider(&opened, &docs))
	doc := &fakeDoc{uri: "file:///work/one/a.md", lang: "markdown", text: "# hi"}
	key := new(int)
	body, ok := tb.scope.FileViewBody(key, "ed.preview", doc)
	if !ok {
		t.Fatal("the view of an extension that is not active yet is not available")
	}
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 300, Height: 100}, body)
	for i := 0; i < 3; i++ {
		tb.pump()
		ts.Pump()
	}
	if tb.scope.State("ed") != StateActive || opened != 1 {
		t.Fatalf("ed is %v, Open ran %d times", tb.scope.State("ed"), opened)
	}
	if !ts.Exists(gluttertest.ByText("preview:# hi")) {
		t.Error("the view is not on screen")
	}
	// an edit reaches the view through the handle
	doc.text = "# changed"
	for _, fn := range doc.onChange {
		fn(extension.ViewDocumentChange{Version: 2, Cause: "edit"})
	}
	for i := 0; i < 3; i++ {
		tb.pump()
		ts.Pump()
	}
	if !ts.Exists(gluttertest.ByText("preview:# changed")) {
		t.Error("the view did not follow the edit")
	}
	// the same tab asks again: the same instance
	if _, ok := tb.scope.FileViewBody(key, "ed.preview", doc); !ok || opened != 1 {
		t.Errorf("a second ask made another instance (Open ran %d times)", opened)
	}
	// another tab has an instance of its own
	other, ok := tb.scope.FileViewBody(new(int), "ed.preview", &fakeDoc{uri: "file:///work/one/b.md", lang: "markdown", text: "b"})
	if !ok || other == nil || opened != 2 {
		t.Errorf("another tab: %v, Open ran %d times", ok, opened)
	}
	tb.scope.ReleaseFileViews(key, "")
	tb.scope.ReleaseFileViews(new(int), "ed.nope")
	// the view of a view that no extension selects is not available
	if _, ok := tb.scope.FileViewBody(key, "ed.unknown", doc); ok {
		t.Error("an unknown view is available")
	}
}

func TestAFileViewThatFailsToOpenOrPanicsIsNotAvailable(t *testing.T) {
	cases := []struct {
		name  string
		open  func(extension.DocumentHandle, extension.FileViewTab) (extension.FileViewContent, error)
		route string
	}{
		{"an error", func(extension.DocumentHandle, extension.FileViewTab) (extension.FileViewContent, error) {
			return extension.FileViewContent{}, apperrors.New("No", "no")
		}, ""},
		{"no Build", func(extension.DocumentHandle, extension.FileViewTab) (extension.FileViewContent, error) {
			return extension.FileViewContent{}, nil
		}, ""},
		{"a panic", func(extension.DocumentHandle, extension.FileViewTab) (extension.FileViewContent, error) {
			panic("open")
		},
			"Scope.openFileView.func1.1.ExtensionFileViewPanicked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tb := edTab(t, nil, func(api extension.API) error {
				return api.FileViews().Provide("ed.preview", extension.FileViewProvider{Open: tc.open})
			})
			tb.activate2()
			doc := &fakeDoc{uri: "file:///work/one/a.md", lang: "markdown"}
			key := new(int)
			tb.scope.FileViewBody(key, "ed.preview", doc)
			tb.pump()
			if _, ok := tb.scope.FileViewBody(key, "ed.preview", doc); ok {
				t.Error("a view that failed to open is available")
			}
			if tc.route != "" {
				route(t, tb.scope.Failure("ed").Err, tc.route)
			}
			if tc.name == "no Build" {
				// the error of the second open is the one the log gets
				inst := tb.scope.fileViewInstances[0]
				inst.opened, inst.failed = false, false
				route(t, tb.scope.openFileView(inst), "Scope.openFileView.ExtensionFileViewContentInvalid")
			}
		})
	}
}

// activate2 activates ed by the event of its file view.
func (tb *tab) activate2() {
	tb.t.Helper()
	tb.scope.Fire(extension.OnView("ed.preview"))
	tb.pump()
}

func TestAPanicInACallbackOfAFileViewFailsTheExtensionAndTheViewGoes(t *testing.T) {
	var opened int
	var docs []*fakeDoc
	tb := edTab(t, nil, func(api extension.API) error {
		return api.FileViews().Provide("ed.preview", extension.FileViewProvider{Open: func(doc extension.DocumentHandle, _ extension.FileViewTab) (extension.FileViewContent, error) {
			doc.OnChange(func(extension.ViewDocumentChange) { panic("change") })
			opened++
			return extension.FileViewContent{Build: func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) }}, nil
		}})
	})
	_ = docs
	tb.activate2()
	doc := &fakeDoc{uri: "file:///work/one/a.md", lang: "markdown"}
	if _, ok := tb.scope.FileViewBody(new(int), "ed.preview", doc); !ok {
		t.Fatal("the view is not available")
	}
	before := tb.changed
	doc.onChange[0](extension.ViewDocumentChange{})
	tb.pump()
	if tb.scope.State("ed") != StateFailed {
		t.Fatalf("ed is %v after the panic", tb.scope.State("ed"))
	}
	route(t, tb.scope.Failure("ed").Err, "guardedDoc.fail.ExtensionFileViewPanicked")
	if got := tb.scope.FileViewsFor("/work/one/a.md", "markdown", "/work/one"); len(got) != 0 {
		t.Errorf("a failed extension still has views: %v", got)
	}
	if tb.changed == before {
		t.Error("the window was not told that the view is gone, so the tab would not fall back to Text")
	}
}

func TestFileViewAndEditorAPIErrorsHaveRoutes(t *testing.T) {
	var opened int
	var docs []*fakeDoc
	tb := edTab(t, nil, previewProvider(&opened, &docs))
	tb.activate2()
	a := apiOf(tb)
	ok := extension.FileViewProvider{Open: func(extension.DocumentHandle, extension.FileViewTab) (extension.FileViewContent, error) {
		return extension.FileViewContent{}, nil
	}}
	route(t, a.FileViews().Provide("ed.nope", ok), "fileViewsAPI.Provide.ExtensionFileViewNotDeclared")
	route(t, a.FileViews().Provide("ed.grid", extension.FileViewProvider{}), "fileViewsAPI.Provide.ExtensionFileViewInvalid")
	route(t, a.FileViews().Provide("ed.preview", ok), "fileViewsAPI.Provide.ExtensionFileViewProvided")
	if err := a.FileViews().Open("file:///work/one/a.md", "ed.preview", extension.FileViewOpenOptions{Side: true, Group: 2}); err != nil {
		t.Fatal(err)
	}
	if want := (fileOpen{"file:///work/one/a.md", "ed.preview", true, 2}); len(tb.editors.fileOpen) != 1 || tb.editors.fileOpen[0] != want {
		t.Errorf("the window was asked %+v, want %v", tb.editors.fileOpen, want)
	}
	tb.scope.opts.Editors = nil
	route(t, a.FileViews().Open("", "ed.preview", extension.FileViewOpenOptions{}), "fileViewsAPI.Open.ExtensionEditorsUnavailable")
	tb.scope.opts.Editors = tb.editors
}

func TestLanguagesAndOpenURIGoToTheWindow(t *testing.T) {
	tb := edTab(t, nil, nil)
	tb.activate2()
	a := apiOf(tb)
	if id, ok := a.Languages().Resolve("go"); !ok || id != "go" {
		t.Errorf("Resolve(go) = %q, %v", id, ok)
	}
	if _, ok := a.Languages().Resolve("klingon"); ok {
		t.Error("Resolve found a language that is not there")
	}
	lines, err := a.Languages().Tokenize("go", "a\nbc")
	if err != nil || len(lines) != 2 || lines[1][0].End != 2 {
		t.Errorf("Tokenize = %v, %v", lines, err)
	}
	if err := a.Workspace().OpenURI(context.Background(), "https://example.com"); err != nil || !slices.Equal(tb.uris, []string{"https://example.com"}) {
		t.Errorf("OpenURI: %v, %v", err, tb.uris)
	}
	tb.scope.opts.OpenURI = nil
	route(t, a.Workspace().OpenURI(context.Background(), "https://example.com"), "workspaceAPI.OpenURI.ExtensionOpenURIUnavailable")
	tb.scope.opts.Languages = nil
	_, err = a.Languages().Tokenize("go", "x")
	route(t, err, "languagesAPI.Tokenize.ExtensionLanguageUnknown")
	if _, ok := a.Languages().Resolve("go"); ok {
		t.Error("a scope with no languages resolved one")
	}
}

func TestOpenWindowTabGoesToTheWindow(t *testing.T) {
	tb := edTab(t, nil, nil)
	tb.activate2()
	a := apiOf(tb)
	if err := a.Workspace().OpenWindowTab("emetos.relay.tab"); err != nil || !slices.Equal(tb.tabs, []string{"emetos.relay.tab"}) {
		t.Errorf("OpenWindowTab: %v, %v", err, tb.tabs)
	}
	tb.scope.opts.OpenWindowTab = nil
	route(t, a.Workspace().OpenWindowTab("emetos.relay.tab"), "workspaceAPI.OpenWindowTab.ExtensionOpenWindowTabUnavailable")
}

func TestTheScopeSaysWhichExtensionDeclaresAnEditorKind(t *testing.T) {
	tb := edTab(t, &thingKind{}, nil)
	for _, tt := range []struct {
		name, ext, kind string
		off             bool
		want            bool
	}{
		{"a declared kind", "ed", "ed.thing", false, true},
		{"an extension that is switched off still declares it", "ed", "ed.thing", true, true},
		{"a kind the extension does not declare", "ed", "ed.gone", false, false},
		{"an extension Emetos does not have", "agents", "agents.session", false, false},
		{"a kind of another extension", "other", "ed.thing", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tb.disabled = nil
			if tt.off {
				tb.disabled = []string{"ed"}
			}
			tb.scope.Refresh()
			tb.pump()
			if got := tb.scope.DeclaresEditorKind(tt.ext, tt.kind); got != tt.want {
				t.Errorf("DeclaresEditorKind(%q, %q) = %v, want %v", tt.ext, tt.kind, got, tt.want)
			}
		})
	}
}

func TestIsExtensionCommandKnowsTheCommandsOfLiveExtensionsOnly(t *testing.T) {
	tb := edTab(t, nil, nil)
	for id, want := range map[string]bool{"ed.new": true, "ed.other": true, "ed.nothing": false, "explorer.newFile": false, "": false} {
		if got := tb.scope.IsExtensionCommand(id); got != want {
			t.Errorf("IsExtensionCommand(%q) = %v, want %v", id, got, want)
		}
	}
	tb.disabled = []string{"ed"}
	tb.scope.Refresh()
	if tb.scope.IsExtensionCommand("ed.new") {
		t.Error("a command of an extension that is turned off counts")
	}
}
