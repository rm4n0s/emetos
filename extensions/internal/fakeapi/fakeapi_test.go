package fakeapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
)

func manifest() extension.Manifest {
	m := extension.Manifest{ID: "fk", Name: "Fake"}
	m.Contributes.Commands = []extension.CommandSpec{{ID: "fk.run", Title: "Run"}}
	m.Contributes.Views = []extension.ViewSpec{{ID: "fk.view", Name: "V", Container: "fk.box"}}
	return m
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

func TestTheFakeRefusesWhatTheHostRefuses(t *testing.T) {
	folder := t.TempDir()
	a := New(t, manifest(), extension.ScopeInfo{Folder: folder})
	noFolder := New(t, manifest(), extension.ScopeInfo{})
	if err := a.Commands().Register(extension.CommandSpec{ID: "fk.dyn"}, func(context.Context, any) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := a.StatusBar().Add(extension.StatusItem{ID: "s"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Output().Channel("c"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		err   error
		route string
	}{
		{"a command the manifest does not declare", a.Commands().Handle("fk.other", nil), "commands.Handle.FakeCommandNotDeclared"},
		{"a command that is taken", a.Commands().Register(extension.CommandSpec{ID: "fk.dyn"}, nil), "commands.Register.FakeCommandTaken"},
		{"a setting that is not registered", func() error { _, e := a.Settings().Get("fk.missing"); return e }(), "settings.Get.FakeSettingUnknown"},
		{"a setting of another id", a.Settings().Register(extension.SettingSpec{Key: "other.x"}), "settings.Register.FakeSettingKeyForeign"},
		{"updating a setting that is not the extension's", a.Settings().Update("editor.tabSize", 2), "settings.Update.FakeSettingNotOwned"},
		{"a status item twice", func() error { _, e := a.StatusBar().Add(extension.StatusItem{ID: "s"}); return e }(), "statusBar.Add.FakeStatusItemTaken"},
		{"a request with a topic that is not valid", a.Events().Request(context.Background(), "t..x", nil, nil), "events.Request->CheckTopic.EventTopicInvalid"},
		{"a view that is not declared", func() error {
			_, e := a.Views().Provide("fk.other", extension.ViewContent{Build: func(widgets.BuildContext) widgets.Widget { return nil }})
			return e
		}(), "views.Provide.FakeViewNotDeclared"},
		{"a view with no Build", func() error { _, e := a.Views().Provide("fk.view", extension.ViewContent{}); return e }(), "views.Provide.FakeViewContentInvalid"},
		{"a channel that is open", func() error { _, e := a.Output().Channel("c"); return e }(), "output.Channel.FakeChannelOpen"},
		{"no folder", func() error { _, e := noFolder.Workspace().Files(); return e }(), "workspace.Files.FakeNoFolder"},
		{"a path that leaves the folder", func() error { _, e := a.Workspace().ReadFile(context.Background(), "../x"); return e }(), "workspace.ReadFile.FakePathInvalid"},
		{"a file that is too large", func() error {
			if err := os.WriteFile(filepath.Join(folder, "big"), []byte(strings.Repeat("x", extension.MaxReadFileSize+1)), 0o644); err != nil {
				t.Fatal(err)
			}
			_, e := a.Workspace().ReadFile(context.Background(), "big")
			return e
		}(), "workspace.ReadFile.FakeFileTooLarge"},
		{"a context key of another id", a.Context().Set("other.k", 1), "contextKeys.Set.FakeContextKeyForeign"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { route(t, tt.err, tt.route) })
	}
}

func TestTheFakeDoesWhatTheHostDoes(t *testing.T) {
	folder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(folder, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(folder, "sub", "a.txt"), []byte("hi"), 0o644)
	_ = os.MkdirAll(filepath.Join(folder, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(folder, ".git", "config"), nil, 0o644)
	a := New(t, manifest(), extension.ScopeInfo{Folder: folder})
	if list, err := a.Workspace().Files(); err != nil || !slices.Equal(list.Files, []string{"sub/a.txt"}) || !list.Complete {
		t.Errorf("files = %+v, %v", list, err)
	}
	if text, err := a.Workspace().ReadFile(context.Background(), "sub/a.txt"); err != nil || text != "hi" {
		t.Errorf("ReadFile = %q, %v", text, err)
	}
	a.SetComplete(false)
	if list, _ := a.Workspace().Files(); list.Complete {
		t.Error("the walk is complete")
	}
	// posted functions wait for the pump
	ran := 0
	a.Post(func() { ran++; a.Post(func() { ran++ }) })
	if ran != 0 || !a.Pump() || ran != 2 || a.Pump() {
		t.Errorf("ran %d", ran)
	}
	// settings: the default, then the user's
	if err := a.Settings().Register(extension.SettingSpec{Key: "fk.list", Type: "array", Default: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if v, _ := extension.SettingValue[[]string](a.Settings(), "fk.list"); !slices.Equal(v, []string{"a"}) {
		t.Errorf("default = %v", v)
	}
	changes := 0
	a.Settings().OnChange(func() { changes++ })
	a.SetSetting("fk.list", []string{"b", "c"})
	if v, _ := extension.SettingValue[[]string](a.Settings(), "fk.list"); !slices.Equal(v, []string{"b", "c"}) || changes != 1 {
		t.Errorf("value = %v, %d changes", v, changes)
	}
	if err := a.Settings().Update("fk.list", []string{"d"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := extension.SettingValue[[]string](a.Settings(), "fk.list"); !slices.Equal(v, []string{"d"}) || changes != 2 {
		t.Errorf("value after Update = %v, %d changes", v, changes)
	}
	if ups := a.SettingUpdates(); len(ups) != 1 || ups[0].Key != "fk.list" {
		t.Errorf("updates = %+v", ups)
	}
	// events
	var got []string
	cancel, err := a.Events().Subscribe("emetos.files.*", func(e extension.Event) { got = append(got, e.Topic) })
	if err != nil {
		t.Fatal(err)
	}
	a.Emit("emetos.files.changed", 1)
	a.Emit("emetos.document.saved", 1)
	cancel()
	a.Emit("emetos.files.changed", 2)
	if !slices.Equal(got, []string{"emetos.files.changed"}) {
		t.Errorf("events = %v", got)
	}
	// markers, notes, channels, status
	_ = a.Problems().Set("o", "file:///a", []extension.Marker{{Message: "m"}})
	if a.MarkerCount() != 1 || len(a.Markers("o")["file:///a"]) != 1 {
		t.Error("markers are missing")
	}
	a.Problems().Clear("o")
	if a.MarkerCount() != 0 {
		t.Error("markers stayed")
	}
	a.Notifications().Info("i")
	a.Notifications().Warn("w")
	a.Notifications().Error("e")
	if n := a.Notes(); len(n) != 3 || n[1].Severity != "warn" {
		t.Errorf("notes = %+v", n)
	}
	ch, _ := a.Output().Channel("log")
	ch.AppendLine("a\nb")
	ch.Show(false)
	if lines := a.Channel("log").Lines(); !slices.Equal(lines, []string{"a", "b"}) || a.Channel("log").Shown() != 1 {
		t.Errorf("channel = %v", lines)
	}
	ch.Clear()
	h, _ := a.StatusBar().Add(extension.StatusItem{ID: "s", Text: "x"})
	h.Update(extension.StatusItem{ID: "s", Text: "y"})
	h.SetVisible(false)
	if it := a.StatusItem("s"); it.Text != "y" || !it.Hidden {
		t.Errorf("status = %+v", it)
	}
	h.Dispose()
	// commands
	_ = a.Commands().Handle("fk.run", func(_ context.Context, args any) error { got = append(got, "run"); return nil })
	if err := a.Run("fk.run", nil); err != nil || !a.HasHandler("fk.run") {
		t.Errorf("Run: %v", err)
	}
	// a view and its handle
	v, _ := a.Views().Provide("fk.view", extension.ViewContent{Build: func(widgets.BuildContext) widgets.Widget { return nil }})
	v.Refresh()
	v.SetBadge(2, "two")
	v.Reveal(true)
	view := a.View("fk.view")
	if r, b, tip := view.Snapshot(); r != 1 || b != 2 || tip != "two" || !slices.Equal(view.Reveals, []bool{true}) {
		t.Errorf("view = %d %d %q %v", r, b, tip, view.Reveals)
	}
	v.Dispose()
	_ = a.Context().Set("fk.key", 1)
	if a.ContextKey("fk.key") != 1 || !slices.Equal(a.ViewIDs(), []string{"fk.view"}) {
		t.Error("context key or view ids")
	}
	a.Context().Reset("fk.key")
	if a.ContextKey("fk.key") != nil {
		t.Error("the key stayed")
	}
	_ = a.Documents().Show("file:///a", 1, 2, true)
	if s := a.ShownDocuments(); len(s) != 1 || s[0].Column != 2 {
		t.Errorf("shown = %+v", s)
	}
	if _, err := a.Documents().Read(context.Background(), "file://"+filepath.Join(folder, "sub", "a.txt")); err != nil {
		t.Error(err)
	}
	_ = a.Documents().Open()
	_ = a.Scope()
	_ = a.Log()
	_ = a.Keybindings().Add(extension.Keybinding{})
	_ = a.Menus().Add(extension.MenuItem{})
	_ = a.Events().Publish("x", 1)
	_ = a.Events().Handle("x", nil)
	_ = a.Events().HandleAsync("x", nil)
	_ = a.RunCtx
}

func TestTheFakeEditorsRefuseWhatTheHostRefuses(t *testing.T) {
	m := manifest()
	m.Contributes.EditorKinds = []extension.EditorKindSpec{{ID: "fk.card", Name: "Card"}}
	m.Contributes.FileViews = []extension.FileViewSpec{{ID: "fk.preview", Name: "Preview", Languages: []string{"markdown"}}}
	a := New(t, m, extension.ScopeInfo{})
	route(t, a.Editors().Provide("fk.nope", extension.EditorKind{}), "editors.Provide.FakeEditorKindNotDeclared")
	route(t, a.Editors().Provide("fk.card", extension.EditorKind{}), "editors.Provide.FakeEditorKindInvalid")
	_, err := a.Editors().Open("fk.card", nil, extension.EditorOpenOptions{})
	route(t, err, "API.makeTab.FakeEditorKindNotProvided")
	create := func(extension.EditorTab, json.RawMessage) (extension.EditorTabContent, error) {
		return extension.EditorTabContent{SaveState: func() (any, error) { return "s", nil }, OnClose: func() {}}, nil
	}
	if err := a.Editors().Provide("fk.card", extension.EditorKind{Create: create}); err != nil {
		t.Fatal(err)
	}
	_, err = a.Editors().Open("fk.card", make(chan int), extension.EditorOpenOptions{})
	route(t, err, "editors.Open.FakeEditorStateInvalid")
	tab, err := a.Editors().Open("fk.card", "x", extension.EditorOpenOptions{Group: 2})
	if err != nil {
		t.Fatal(err)
	}
	tab.SetTitle("T")
	tab.SetHint("H")
	tab.SetDirty(true)
	tab.SetBusy(true)
	tab.Refresh()
	tab.Reveal(true)
	ft := a.EditorTabs()[0]
	if ft.Title() != "T" || ft.Hint() != "H" || !ft.Dirty() || !ft.Busy() || ft.Refreshs != 1 || !ft.Revealed || tab.Group() != 2 || string(ft.State()) != `"s"` {
		t.Errorf("the tab = %q %q %v %v", ft.Title(), ft.Hint(), ft.Dirty(), ft.Busy())
	}
	if got := a.Editors().Tabs("fk.card"); len(got) != 1 {
		t.Errorf("Tabs = %d", len(got))
	}
	tab.Close()
	if got := a.Editors().Tabs("fk.card"); len(got) != 0 {
		t.Errorf("Tabs after Close = %d", len(got))
	}
	restored := a.RestoreTab("fk.card", "r1", json.RawMessage(`"old"`))
	if restored.ID() != "r1" || restored.Kind() != "fk.card" {
		t.Errorf("restored = %s %s", restored.ID(), restored.Kind())
	}

	route(t, a.FileViews().Provide("fk.nope", extension.FileViewProvider{}), "fileViews.Provide.FakeFileViewNotDeclared")
	route(t, a.FileViews().Provide("fk.preview", extension.FileViewProvider{}), "fileViews.Provide.FakeFileViewInvalid")
	_, err = a.OpenFileView("fk.preview", nil)
	route(t, err, "API.OpenFileView.FakeFileViewNotProvided")
	if err := a.FileViews().Provide("fk.preview", extension.FileViewProvider{Open: func(extension.DocumentHandle, extension.FileViewTab) (extension.FileViewContent, error) {
		return extension.FileViewContent{}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.OpenFileView("fk.preview", nil); err != nil {
		t.Fatal(err)
	}
	if err := a.FileViews().Open("file:///a.md", "fk.preview", extension.FileViewOpenOptions{Side: true}); err != nil || len(a.FileViewOpens()) != 1 {
		t.Errorf("FileViews.Open: %v, %v", err, a.FileViewOpens())
	}
	if id, ok := a.Languages().Resolve("Go"); !ok || id != "go" {
		t.Errorf("Resolve = %q, %v", id, ok)
	}
	lines, _ := a.Languages().Tokenize("go", "a\nb")
	if len(lines) != 2 || lines[0][0].Scopes[0] != "source.go" {
		t.Errorf("Tokenize = %+v", lines)
	}
	a.SetTokenizer(func(string, string) ([][]extension.Token, error) { return nil, nil })
	if lines, _ := a.Languages().Tokenize("go", "a"); lines != nil {
		t.Error("SetTokenizer did not replace the tokenizer")
	}
	if err := a.Workspace().OpenURI(context.Background(), "https://x.dev"); err != nil || len(a.OpenedURIs()) != 1 {
		t.Errorf("OpenURI: %v %v", err, a.OpenedURIs())
	}
}

func TestTheFakeWindowTabRefusesWhatTheHostRefusesAndKeepsTheTab(t *testing.T) {
	m := manifest()
	m.Contributes.WindowTabKinds = []extension.WindowTabKindSpec{{ID: "fk.tab", Title: "Tab"}}
	a := New(t, m, extension.ScopeInfo{Kind: extension.ScopeWindowTab, Name: "Tab", WindowTab: "fk.tab", ID: "fk:t1"})
	create := func(tab extension.WindowTab, state json.RawMessage) (extension.WindowTabContent, error) {
		tab.SetTitle("T " + string(state))
		tab.SetHint("hint")
		tab.StateChanged()
		tab.Refresh()
		return extension.WindowTabContent{
			Build:     func(widgets.BuildContext) widgets.Widget { return widgets.NewSizedBox(nil) },
			SaveState: func() (any, error) { return "saved", nil },
			OnClose:   func() { tab.Close() },
		}, nil
	}
	route(t, a.WindowTabs().Provide("fk.other", extension.WindowTabKind{Create: create}), "windowTabs.Provide.FakeWindowTabKindNotDeclared")
	route(t, a.WindowTabs().Provide("fk.tab", extension.WindowTabKind{}), "windowTabs.Provide.FakeWindowTabKindInvalid")
	if err := a.WindowTabs().Provide("fk.tab", extension.WindowTabKind{Create: create}); err != nil {
		t.Fatal(err)
	}
	tab := a.StartWindowTab("t1", json.RawMessage(`"old"`))
	if tab.ID() != "t1" || tab.Kind() != "fk.tab" || tab.Title() != `T "old"` || tab.Hint() != "hint" || tab.Changes != 1 || tab.Refreshs != 1 {
		t.Errorf("the tab = %q %q %q %q, %d changes, %d refreshs", tab.ID(), tab.Kind(), tab.Title(), tab.Hint(), tab.Changes, tab.Refreshs)
	}
	if got := string(tab.State()); got != `"saved"` {
		t.Errorf("the state = %s", got)
	}
	a.CloseWindowTab()
	if tab.Closes != 1 {
		t.Errorf("Close was called %d times by OnClose", tab.Closes)
	}
}

func TestTheFakeDeliversTheEventsOfOtherTabsToTheSubscriptionsThatListenToThem(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Name: "me", ID: "fk:t1"})
	var own, others []string
	if _, err := a.Events().Subscribe("fk.ping", func(e extension.Event) { own = append(own, e.Scope.Name) }); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Events().Subscribe("fk.ping", func(e extension.Event) { others = append(others, e.Scope.Name) }, extension.FromOtherScopes()); err != nil {
		t.Fatal(err)
	}
	a.Emit("fk.ping", 1)
	a.EmitFrom(extension.ScopeInfo{Name: "elsewhere", ID: "/work/elsewhere"}, "fk.ping", 2)
	if !slices.Equal(own, []string{"me"}) || !slices.Equal(others, []string{"me", "elsewhere"}) {
		t.Errorf("own %q, others %q", own, others)
	}
}

func TestTheFakeRecordsProviders(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Name: "w"})
	l := a.Languages()
	_, err := l.RegisterHoverProvider(extension.DocumentSelector{}, nil)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("register.FakeProviderInvalid") {
		t.Errorf("a nil provider: %v", err)
	}
	dispose, err := l.RegisterCompletionProvider(extension.DocumentSelector{Languages: []string{"go"}}, fakeCompletion{}, ".", ":")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.RegisterRenameProvider(extension.DocumentSelector{Pattern: "**/*.go"}, fakeRename{}); err != nil {
		t.Fatal(err)
	}
	ps := a.Providers()
	if len(ps) != 2 || ps[0].Feature != "completion" || ps[0].Selector.Languages[0] != "go" || len(ps[0].Triggers) != 2 || ps[1].Feature != "rename" || ps[0].Disposed {
		t.Fatalf("providers = %+v", ps)
	}
	dispose()
	if !a.Providers()[0].Disposed {
		t.Error("the provider was not marked disposed")
	}
}

type fakeCompletion struct{}

func (fakeCompletion) ProvideCompletion(context.Context, extension.TextDocument, extension.Position, extension.CompletionTrigger) (extension.CompletionList, error) {
	return extension.CompletionList{}, nil
}

type fakeRename struct{}

func (fakeRename) PrepareRename(context.Context, extension.TextDocument, extension.Position) (*extension.PrepareRenameResult, error) {
	return nil, nil
}

func (fakeRename) ProvideRename(context.Context, extension.TextDocument, extension.Position, string) (*extension.WorkspaceEdit, error) {
	return nil, nil
}

func TestTheFakeShowsProposedChangesAndAnswersThemAsTheUserDoes(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "w", Folder: t.TempDir()})
	_, err := a.Editors().OpenProposedChange(context.Background(), extension.ProposedChange{Proposed: "x"})
	route(t, err, "editors.OpenProposedChange.FakeProposedChangeInvalid")

	tests := []struct {
		name   string
		answer func(p *Proposal)
		want   extension.ProposedResult
	}{
		{"accept", func(p *Proposal) { p.Accept("new\n") }, extension.ProposedResult{Accepted: true, Text: "new\n"}},
		{"accept with an edit", func(p *Proposal) { p.Accept("newer\n") }, extension.ProposedResult{Accepted: true, Text: "newer\n", Edited: true}},
		{"reject", func(p *Proposal) { p.Reject() }, extension.ProposedResult{}},
		{"close", func(p *Proposal) { p.Close(); p.Accept("late") }, extension.ProposedResult{}},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan extension.ProposedResult, 1)
			go func() {
				res, err := a.Editors().OpenProposedChange(context.Background(), extension.ProposedChange{URI: "file:///w/a.go", Original: "old\n", Proposed: "new\n"})
				if err != nil {
					t.Error(err)
				}
				done <- res
			}()
			a.WaitFor("the proposed change", func() bool { return len(a.Proposals()) == i+1 })
			tc.answer(a.Proposals()[i])
			if got := <-done; got != tc.want {
				t.Errorf("the call returned %+v, want %+v", got, tc.want)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := a.Editors().OpenProposedChange(ctx, extension.ProposedChange{URI: "file:///w/b.go"})
		errc <- err
	}()
	a.WaitFor("the fifth proposed change", func() bool { return len(a.Proposals()) == len(tests)+1 })
	cancel()
	route(t, <-errc, "editors.OpenProposedChange.FakeProposedChangeCancelled")
	if p := a.Proposals()[len(tests)]; !p.Cancelled() {
		t.Error("a cancelled call is not marked")
	}
}

func TestTheFakeKeepsTheButtonsOfNotifications(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "w", Folder: t.TempDir()})
	pressed := 0
	a.Notifications().Info("done", extension.NotificationAction{Label: "Show", Run: func() { pressed++ }})
	if notes := a.Notes(); len(notes) != 1 || !slices.Equal(notes[0].Actions, []string{"Show"}) {
		t.Fatalf("notes = %+v", notes)
	}
	a.RunNoteAction(0, "Show")
	if pressed != 1 {
		t.Errorf("the button was pressed %d times", pressed)
	}
}
