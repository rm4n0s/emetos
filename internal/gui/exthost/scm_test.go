package exthost

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/scmtest"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/scm"
)

// ---- the conversions between the model and the contract, for the conformance suite ----

func extCommand(c scm.Command) extension.Command {
	return extension.Command{ID: c.ID, Title: c.Title, Tooltip: c.Tooltip, Args: c.Args}
}

func observeSources(m *scm.Model) []scmtest.Source {
	var out []scmtest.Source
	// the model lists the repositories in its own order; the suite wants the order of creation, which the handles number
	list := m.Sources()
	sort.Slice(list, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.TrimPrefix(list[i].Key(), "scm"))
		b, _ := strconv.Atoi(strings.TrimPrefix(list[j].Key(), "scm"))
		return a < b
	})
	for _, s := range list {
		in := s.Input()
		accept, _ := s.AcceptInputCommand()
		src := scmtest.Source{
			Handle: s.Key(), ID: s.ProviderID(), Label: s.Label(), RootURI: s.RootURI(), ContextValue: s.ContextValue(),
			Count: s.Count(), BadgeCount: s.BadgeCount(), Busy: s.Busy(), Accept: extCommand(accept),
			Input: scmtest.Input{Value: in.Value(), Placeholder: in.Placeholder(), Visible: in.Visible(), Enabled: in.Enabled(), FocusRequests: int(in.FocusRequests())},
		}
		if name := s.ActiveRepositoryName(); name != s.Name() {
			src.ActiveName = name
		}
		for _, c := range s.StatusBarCommands() {
			src.Status = append(src.Status, extension.StatusCommand{Text: c.Text, Tooltip: c.Tooltip, Command: c.Command, Args: c.Args})
		}
		if b := s.ActionButton(); b != nil {
			ab := &extension.ActionButton{Command: extCommand(b.Command), Enabled: b.Enabled, Description: b.Description}
			for _, g := range b.Secondary {
				var list []extension.Command
				for _, c := range g {
					list = append(list, extCommand(c))
				}
				ab.Secondary = append(ab.Secondary, list)
			}
			src.Button = ab
		}
		for _, g := range s.Groups() {
			sg := scmtest.Group{ID: g.ID(), Label: g.Label(), HideWhenEmpty: g.HideWhenEmpty(), ContextValue: g.ContextValue()}
			for _, r := range g.Resources() {
				rs := extension.ResourceState{
					URI: r.URI, ContextValue: r.ContextValue,
					Decorations:                    extension.ResourceDecorations{Icon: r.Decorations.Icon, Tooltip: r.Decorations.Tooltip, StrikeThrough: r.Decorations.StrikeThrough, Faded: r.Decorations.Faded, Color: r.Decorations.Color},
					MultiFileDiffEditorOriginalURI: r.MultiDiffOriginalURI, MultiFileDiffEditorModifiedURI: r.MultiDiffModifiedURI,
				}
				if r.Command != nil {
					c := extCommand(*r.Command)
					rs.Command = &c
				}
				sg.Resources = append(sg.Resources, rs)
			}
			src.Groups = append(src.Groups, sg)
		}
		out = append(out, src)
	}
	return out
}

// rawJSON makes the arguments of a command from JSON texts.
func rawJSON(texts ...string) []json.RawMessage {
	var out []json.RawMessage
	for _, t := range texts {
		out = append(out, json.RawMessage(t))
	}
	return out
}

// scmHarness makes the harness of the conformance suite over a tab of the host.
func scmHarness(t *testing.T) *scmtest.Harness {
	t.Helper()
	_, tb, api := grabAPI(t, "emetos.scmtest", nil)
	return hostHarness(tb, api, "emetos.scmtest")
}

func hostHarness(tb *tab, api extension.API, id string) *scmtest.Harness {
	return &scmtest.Harness{
		API:     api,
		Observe: func() []scmtest.Source { return observeSources(tb.scm) },
		Type:    func(handle, text string) { tb.scm.Source(handle).Input().SetValue(text) },
		Validate: func(handle, text string) *extension.Validation {
			v := tb.scm.Source(handle).Input().Validate(text)
			if v == nil {
				return nil
			}
			return &extension.Validation{Message: v.Message, Level: extension.ValidationLevel(v.Level)}
		},
		Pump:       tb.pump,
		Deactivate: func() { tb.disabled = []string{id}; tb.scope.Refresh() },
		Failed:     func() bool { return tb.scope.State(id) == StateFailed },
	}
}

// TestSourceControlConformance runs the suite that the fake of the tests of the extensions passes too.
func TestSourceControlConformance(t *testing.T) { scmtest.Run(t, scmHarness) }

// ---- what only the host does ----

func TestOnlyTheOwnerChangesASourceControl(t *testing.T) {
	var apiA, apiB extension.API
	fa, _ := factory(manifest("a.ext"), func(f *fake) { f.activate = func(_ context.Context, a extension.API) error { apiA = a; return nil } })
	fb, _ := factory(manifest("b.ext"), func(f *fake) { f.activate = func(_ context.Context, a extension.API) error { apiB = a; return nil } })
	r := newRig(t, fa, fb)
	tb := r.newTab("one")
	tb.scope.Start()
	sc, err := apiA.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/repo"})
	if err != nil {
		t.Fatal(err)
	}
	// the other extension makes its own, with the same id and root: ids are the extension's own
	other, err := apiB.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/repo"})
	if err != nil {
		t.Fatalf("the same id in another extension: %v", err)
	}
	if sc.Handle() == other.Handle() {
		t.Fatal("two source controls have one handle")
	}
	// a handle that B built over the source control of A is refused, whatever it calls
	src := tb.scm.Source(sc.Handle())
	instB := tb.scope.slots[1].inst
	foreign := &scmSource{inst: instB, src: src, key: "x", model: tb.scm}
	foreign.SetCount(99)
	foreign.SetBusy(true)
	foreign.Input().SetValue("hijacked")
	if src.Count() != -1 || src.Busy() || src.Input().Value() != "" {
		t.Errorf("a source control of another extension was changed: count %d busy %v input %q", src.Count(), src.Busy(), src.Input().Value())
	}
	_, err = foreign.CreateGroup(extension.ResourceGroupSpec{ID: "g", Label: "G"})
	wantRoute(t, err, "scmSource.CreateGroup->Scope.scmOwned.ExtensionSCMForeign")
	wantRoute(t, tb.scope.scmOwned(instB, src), "Scope.scmOwned.ExtensionSCMForeign")
	if err := tb.scope.scmOwned(tb.scope.slots[0].inst, src); err != nil {
		t.Errorf("the owner was refused: %v", err)
	}
	// it is the owner that changes it
	sc.SetCount(3)
	if src.Count() != 3 {
		t.Errorf("count = %d", src.Count())
	}
	// the source controls of an extension go with it, and the other's stay
	tb.disabled = []string{"a.ext"}
	tb.scope.Refresh()
	if got := tb.scm.Sources(); len(got) != 1 || got[0].Owner() != "b.ext" {
		t.Errorf("source controls = %v", names(got))
	}
}

func names(ss []*scm.Source) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Owner()+":"+s.ProviderID())
	}
	return out
}

func TestAPanicInACallbackFailsOnlyThatExtension(t *testing.T) {
	var apiA, apiB extension.API
	fa, _ := factory(manifest("a.ext"), func(f *fake) { f.activate = func(_ context.Context, a extension.API) error { apiA = a; return nil } })
	fb, _ := factory(manifest("b.ext"), func(f *fake) { f.activate = func(_ context.Context, a extension.API) error { apiB = a; return nil } })
	r := newRig(t, fa, fb)
	tb := r.newTab("one")
	tb.scope.Start()
	scA, _ := apiA.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git"})
	scB, _ := apiB.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git"})
	scA.Input().SetValidation(func(string) *extension.Validation { panic("boom") })
	if v := tb.scm.Source(scA.Handle()).Input().Validate("x"); v != nil {
		t.Errorf("validation = %v", v)
	}
	if tb.scope.State("a.ext") != StateFailed || tb.scope.State("b.ext") != StateActive {
		t.Fatalf("states: a %v b %v", tb.scope.State("a.ext"), tb.scope.State("b.ext"))
	}
	wantRoute(t, tb.scope.Failure("a.ext").Err, "Scope.runSCMValidate.func1.ExtensionSCMValidatePanicked")
	if got := tb.scm.Sources(); len(got) != 1 || got[0].Key() != scB.Handle() {
		t.Errorf("the source controls left = %v", names(got))
	}
	// a panic in OnChange
	scB.Input().OnChange(func(string) { panic("boom") })
	tb.scm.Source(scB.Handle()).Input().SetValue("x")
	tb.pump()
	if tb.scope.State("b.ext") != StateFailed {
		t.Fatal("b is not failed")
	}
	wantRoute(t, tb.scope.Failure("b.ext").Err, "Scope.runSCMChange.func1.ExtensionSCMChangePanicked")
	if tb.scm.Count() != 0 {
		t.Errorf("%d source controls left", tb.scm.Count())
	}
}

func TestOnChangeRunsOnTheUIThread(t *testing.T) {
	_, tb, api := grabAPI(t, "emetos.scmui", nil)
	tb.deferred = true // Post queues, as the UI thread does
	sc, err := api.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git"})
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	sc.Input().OnChange(func(v string) { seen = append(seen, v) })
	go tb.scm.Source(sc.Handle()).Input().SetValue("typed on another goroutine")
	waitFor(t, tb, "the change", func() bool { return len(seen) == 1 })
	if seen[0] != "typed on another goroutine" {
		t.Errorf("seen %q", seen)
	}
	// SetValue by the extension calls its own listeners as well, later
	sc.Input().SetValue("own")
	if len(seen) != 1 {
		t.Errorf("the listener ran before the UI thread: %q", seen)
	}
	tb.pump()
	if !reflect.DeepEqual(seen, []string{"typed on another goroutine", "own"}) {
		t.Errorf("seen %q", seen)
	}
}

func TestSourceControlUnavailableWithoutAModel(t *testing.T) {
	_, _, api := grabAPI(t, "emetos.noscm", func(r *rig) { r.noSCM["one"] = true })
	_, err := api.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git"})
	wantRoute(t, err, "scmAPI.Create.ExtensionSCMUnavailable")
}

func TestRoutesOfTheSourceControlFailures(t *testing.T) {
	r, tb, api := grabAPI(t, "emetos.scmroutes", nil)
	_ = r
	spec := extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/a"}
	sc, err := api.SourceControl().Create(spec)
	if err != nil {
		t.Fatal(err)
	}
	_, err = api.SourceControl().Create(spec)
	wantRoute(t, err, "scmAPI.Create.ExtensionSCMDuplicate")
	_, err = api.SourceControl().Create(extension.SourceControlSpec{Label: "Git"})
	wantRoute(t, err, "scmAPI.Create.ExtensionSCMInvalid")
	if _, err = sc.CreateGroup(extension.ResourceGroupSpec{ID: "g", Label: "G"}); err != nil {
		t.Fatal(err)
	}
	_, err = sc.CreateGroup(extension.ResourceGroupSpec{ID: "g", Label: "Again"})
	wantRoute(t, err, "scmSource.CreateGroup.ExtensionSCMGroupDuplicate")
	_, err = sc.CreateGroup(extension.ResourceGroupSpec{})
	wantRoute(t, err, "scmSource.CreateGroup.ExtensionSCMGroupInvalid")
	sc.Dispose()
	_, err = sc.CreateGroup(extension.ResourceGroupSpec{ID: "late", Label: "L"})
	wantRoute(t, err, "scmSource.CreateGroup.ExtensionSCMDisposed")
	tb.disabled = []string{"emetos.scmroutes"}
	tb.scope.Refresh()
	_, err = api.SourceControl().Create(spec)
	wantRoute(t, err, "scmAPI.Create.ExtensionDeactivated")
	_, err = sc.CreateGroup(extension.ResourceGroupSpec{ID: "late", Label: "L"})
	wantRoute(t, err, "scmSource.CreateGroup.ExtensionDeactivated")
}

func TestACommandOfASourceControlGetsItsArguments(t *testing.T) {
	tests := []struct {
		name string
		c    extension.Command
		want any
	}{
		{"none", extension.Command{ID: "x"}, nil},
		{"Args", extension.Command{ID: "x", Args: map[string]any{"a": 1}}, map[string]any{"a": 1}},
		{"one JSON argument", extension.Command{ID: "x", Arguments: rawJSON(`"file:///a"`)}, "file:///a"},
		{"several JSON arguments", extension.Command{ID: "x", Arguments: rawJSON(`"a"`, `2`, `{"k":true}`)}, []any{"a", 2.0, map[string]any{"k": true}}},
		{"Args wins", extension.Command{ID: "x", Args: "args", Arguments: rawJSON(`"json"`)}, "args"},
		{"an argument that is not JSON is left out", extension.Command{ID: "x", Arguments: rawJSON(`{bad`, `1`)}, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scmCommand(tt.c).Args; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("args = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestTheSCMMenuIDsAreTheMenusOfTheEditor checks that the constants of the contract and those of the registry of menus name the
// same menus, and that the arguments of a menu command are read by extension.SCMArgs.
func TestTheSCMMenuIDsAreTheMenusOfTheEditor(t *testing.T) {
	pairs := map[string][2]string{
		"SCMTitle": {extension.MenuSCMTitle, command.SCMTitle}, "SCMRepository": {extension.MenuSCMRepository, command.SCMSourceControlInline},
		"SCMSourceControl":             {extension.MenuSCMSourceControl, command.SCMSourceControl},
		"SCMResourceGroupContext":      {extension.MenuSCMResourceGroupContext, command.SCMResourceGroupContext},
		"SCMResourceStateContext":      {extension.MenuSCMResourceStateContext, command.SCMResourceContext},
		"SCMResourceFolderContext":     {extension.MenuSCMResourceFolderContext, command.SCMResourceFolderContext},
		"SCMChangeTitle":               {extension.MenuSCMChangeTitle, command.SCMChangeContext},
		"SCMHistoryTitle":              {extension.MenuSCMHistoryTitle, command.SCMHistoryTitle},
		"SCMHistoryItemContext":        {extension.MenuSCMHistoryItemContext, command.SCMHistoryItemContext},
		"SCMHistoryItemRefContext":     {extension.MenuSCMHistoryItemRefContext, command.SCMHistoryItemRefContext},
		"TimelineItemContext":          {extension.MenuTimelineItemContext, command.TimelineItemContext},
		"EditorContent":                {extension.MenuEditorContent, command.EditorContent},
		"EditorTitle":                  {extension.MenuEditorTitle, command.EditorTitle},
		"DiffEditorGutterHunk":         {extension.MenuDiffEditorGutterHunk, command.DiffEditorHunkToolbar},
		"DiffEditorGutterSelection":    {extension.MenuDiffEditorGutterSelection, command.DiffEditorSelectionToolbar},
		"CommandPalette (existing)":    {extension.MenuCommandPalette, command.CommandPalette},
		"Explorer context (existing)":  {extension.MenuExplorerContext, command.ExplorerContext},
		"Manage menu (existing)":       {extension.MenuManage, command.GlobalActivity},
		"Editor context (existing)":    {extension.MenuEditorContext, command.EditorContext},
		"The Terminal menu (existing)": {extension.MenuTerminal, command.MenubarTerminalMenu},
	}
	for name, p := range pairs {
		if p[0] != p[1] || p[0] == "" {
			t.Errorf("%s: the contract says %q, the registry %q", name, p[0], p[1])
		}
	}
	// the members of the arguments are the model's
	m := scm.NewModel(scm.DefaultConfig())
	s := m.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///repo"})
	g := s.AddGroup("index", "Staged", false, "staged")
	g.SetResources([]scm.Resource{{URI: "file:///repo/a.go", ContextValue: "m"}})
	args := scm.SelectionMenuArgs(scm.Row{Resource: &g.Resources()[0]}, nil)
	args = append(args, scm.SelectionMenuArgs(scm.Row{Group: g}, nil)...)
	args = append(args, scm.SourceMenuArgs(s)...)
	got := extension.SCMArgs(args)
	want := []extension.SCMArg{
		{SourceControl: s.Key(), Group: "index", URI: "file:///repo/a.go", ContextValue: "m"},
		{SourceControl: s.Key(), Group: "index", ContextValue: "staged"},
		{SourceControl: s.Key()},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SCMArgs = %+v\nwant %+v", got, want)
	}
	if !got[0].IsResource() || !got[1].IsGroup() || !got[2].IsSourceControl() || got[0].IsGroup() || got[1].IsResource() {
		t.Error("IsResource, IsGroup and IsSourceControl")
	}
}

func TestSubmenusAreInstalledAsRows(t *testing.T) {
	m := manifest("emetos.sub")
	m.Contributes.Commands = []extension.CommandSpec{{ID: "git.commit", Title: "Commit"}, {ID: "git.commitAmend", Title: "Commit (Amend)"}}
	m.Contributes.Submenus = []extension.SubmenuSpec{{ID: "git.commit", Label: "Commit"}}
	m.Contributes.Menus = []extension.MenuItem{
		{Menu: extension.MenuSCMTitle, Submenu: "git.commit", Group: "1_commit", Order: 1},
		{Menu: "git.commit", Command: "git.commit", Group: "1_commit"},
		{Menu: "git.commit", Command: "git.commitAmend", Group: "2_amend"},
	}
	f, _ := factory(m, nil)
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	entries := tb.menus.Items(extension.MenuSCMTitle, contextkey.NewRoot())
	if len(entries) != 1 || entries[0].Submenu != "git.commit" || entries[0].Title != "Commit" || len(entries[0].Items) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	var cmds []string
	for _, e := range entries[0].Items {
		if !e.Separator {
			cmds = append(cmds, e.Command)
		}
	}
	if !reflect.DeepEqual(cmds, []string{"git.commit", "git.commitAmend"}) {
		t.Errorf("submenu commands = %v", cmds)
	}
	// the rows go with the extension
	tb.disabled = []string{"emetos.sub"}
	tb.scope.Refresh()
	if got := tb.menus.Items(extension.MenuSCMTitle, contextkey.NewRoot()); len(got) != 0 {
		t.Errorf("rows after turning the extension off: %+v", got)
	}
}

func TestMenusAddOpensASubmenuOfTheManifest(t *testing.T) {
	m := manifest("emetos.sub2")
	m.Contributes.Submenus = []extension.SubmenuSpec{{ID: "git.more", Label: "More"}}
	_, tb, api := grabAPIWith(t, m)
	if err := api.Menus().Add(extension.MenuItem{Menu: extension.MenuSCMTitle, Submenu: "git.more", Title: "Even More"}); err != nil {
		t.Fatal(err)
	}
	if err := api.Menus().Add(extension.MenuItem{Menu: extension.MenuSCMTitle, Command: "git.x", Submenu: "git.more"}); err == nil {
		t.Error("an item with a command and a submenu was added")
	}
	err := api.Menus().Add(extension.MenuItem{Menu: extension.MenuSCMTitle, Submenu: "git.unknown"})
	wantRoute(t, err, "Scope.addMenu.ExtensionSubmenuUnknown")
	_ = tb
}

// grabAPIWith is grabAPI for a manifest.
func grabAPIWith(t *testing.T, m extension.Manifest) (*rig, *tab, extension.API) {
	t.Helper()
	var api extension.API
	f, _ := factory(m, func(f *fake) { f.activate = func(_ context.Context, a extension.API) error { api = a; return nil } })
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	if api == nil {
		t.Fatalf("the extension %s was not activated", m.ID)
	}
	return r, tb, api
}

func TestDottedContextKeysAndSubmenusAreTheExtensionsOwn(t *testing.T) {
	decl := func(id string, keys []string, submenus ...string) extension.Factory {
		m := manifest(id)
		m.Contributes.ContextKeys = keys
		for _, s := range submenus {
			m.Contributes.Submenus = append(m.Contributes.Submenus, extension.SubmenuSpec{ID: s, Label: s})
		}
		f, _ := factory(m, nil)
		return f
	}
	tests := []struct {
		name  string
		a     extension.Factory
		left  int
		route string
	}{
		{"dotted keys of VS Code's names", decl("emetos.gitscm", []string{"git.state", "git.missing"}, "git.commit"), 2, ""},
		{"a key under the own id", decl("emetos.gitscm", []string{"emetos.gitscm.busy"}), 2, ""},
		{"a key under another extension's id", decl("emetos.gitscm", []string{"other.count"}), 1, "checkDeclOwnership.ExtensionContextKeyDeclTaken"},
		{"a submenu under another extension's id", decl("emetos.gitscm", nil, "other.menu"), 1, "checkDeclOwnership.ExtensionSubmenuTaken"},
		{"a name that only starts like another id is not that extension's", decl("emetos.gitscm", []string{"others.count", "other"}, "otherwise.menu"), 2, ""},
		{"a key under emetos. and another id is the contract's to refuse", decl("emetos.gitscm", []string{"emetos.other.count"}), 1, "validateManifest->CheckManifest->checkContextKeys.ExtensionContextKeyDeclForeign"},
		{"a submenu under emetos. and another id is the contract's to refuse", decl("emetos.gitscm", nil, "emetos.other.menu"), 1, "validateManifest->CheckManifest->checkSubmenus.ExtensionSubmenuInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other, _ := factory(manifest("other"), nil)
			host := NewHost(HostOptions{Factories: []extension.Factory{tt.a, other}})
			if got := len(host.Manifests()); got != tt.left {
				t.Fatalf("%d extensions kept, want %d (problems %v)", got, tt.left, host.Problems())
			}
			problems := host.Problems()
			if tt.route == "" {
				if len(problems) != 0 {
					t.Errorf("problems %v", problems)
				}
				return
			}
			if len(problems) != 1 {
				t.Fatalf("problems %v", problems)
			}
			wantRoute(t, problems[0], tt.route)
		})
	}
}
