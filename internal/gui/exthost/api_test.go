package exthost

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// withAPI activates an extension named id in a new tab and gives the test its API.
func withAPI(t *testing.T, id string, extra ...extension.Factory) (extension.API, *rig, *tab) {
	t.Helper()
	var saved extension.API
	f, _ := factory(manifest(id), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error { saved = api; return nil }
	})
	r := newRig(t, append([]extension.Factory{f}, extra...)...)
	tb := r.newTab("one")
	tb.scope.Start()
	if saved == nil {
		t.Fatalf("the extension was not activated: %v", tb.scope.Failure(id))
	}
	return saved, r, tb
}

func TestAPIMisuseHasTagsWithRoutes(t *testing.T) {
	ok := func(context.Context, any) error { return nil }
	api, _, _ := withAPI(t, "emetos.api")
	// a setting that the extension registers first, to be duplicated
	if err := api.Settings().Register(extension.SettingSpec{Key: "emetos.api.once", Type: "string"}); err != nil {
		t.Fatal(err)
	}
	if _, err := api.StatusBar().Add(extension.StatusItem{ID: "dup"}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		call  func() error
		route string
	}{
		{"Handle of a command the manifest does not declare", func() error { return api.Commands().Handle("emetos.api.x", ok) }, "ExtensionCommandNotDeclared"},
		{"Register of a command of the editor", func() error {
			return api.Commands().Register(extension.CommandSpec{ID: "workbench.action.files.save"}, ok)
		}, "Register.CommandDuplicate"},
		{"Register of a command with a bad when-clause", func() error {
			return api.Commands().Register(extension.CommandSpec{ID: "emetos.api.w", When: "((("}, ok)
		}, "WhenClauseInvalid"},
		{"a keybinding with no key", func() error { return api.Keybindings().Add(extension.Keybinding{Command: "a"}) }, "addKeybinding.ExtensionKeybindingInvalid"},
		{"a menu item with no menu", func() error { return api.Menus().Add(extension.MenuItem{Command: "a"}) }, "MenuItemMenuMissing"},
		{"a setting outside the id of the extension", func() error {
			return api.Settings().Register(extension.SettingSpec{Key: "editor.other", Type: "string"})
		}, "ExtensionSettingKeyForeign"},
		{"a setting of another extension that is a prefix", func() error {
			return api.Settings().Register(extension.SettingSpec{Key: "emetos.apix.k", Type: "string"})
		}, "ExtensionSettingKeyForeign"},
		{"a setting with an unknown type", func() error {
			return api.Settings().Register(extension.SettingSpec{Key: "emetos.api.k", Type: "color"})
		}, "ExtensionSettingTypeInvalid"},
		{"a setting registered twice", func() error {
			return api.Settings().Register(extension.SettingSpec{Key: "emetos.api.once", Type: "string"})
		}, "ExtensionSettingDuplicate"},
		{"a setting whose default does not fit", func() error {
			return api.Settings().Register(extension.SettingSpec{Key: "emetos.api.bad", Type: "number", Default: "text"})
		}, "SettingDefaultInvalid"},
		{"a status bar item with no ID", func() error { _, err := api.StatusBar().Add(extension.StatusItem{}); return err }, "ExtensionStatusItemInvalid"},
		{"a status bar item with an ID that is taken", func() error { _, err := api.StatusBar().Add(extension.StatusItem{ID: "dup"}); return err }, "ExtensionStatusItemDuplicate"},
		{"reading a setting that is not registered", func() error { _, err := api.Settings().Get("emetos.api.none"); return err }, "ExtensionSettingUnknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appErr, ok := errors.FromError(tt.call())
			if !ok {
				t.Fatalf("expected *errors.Error")
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestASettingOfAnotherExtensionIsTaken(t *testing.T) {
	// "emetos" owns the prefix "emetos." and so may name a key that "emetos.api" would also be allowed to name
	var inner, outer extension.API
	a, _ := factory(manifest("emetos.api"), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error { inner = api; return nil }
	})
	b, _ := factory(manifest("emetos"), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error { outer = api; return nil }
	})
	r := newRig(t, a, b)
	r.newTab("one").scope.Start()
	if err := inner.Settings().Register(extension.SettingSpec{Key: "emetos.api.k", Type: "string"}); err != nil {
		t.Fatal(err)
	}
	appErr, ok := errors.FromError(outer.Settings().Register(extension.SettingSpec{Key: "emetos.api.k", Type: "string"}))
	if !ok || !appErr.HasRoute("Host.holdSetting.ExtensionSettingTaken") {
		t.Errorf("error = %+v", appErr)
	}
}

func TestSettingsAreRegisteredAndRead(t *testing.T) {
	api, r, tb := withAPI(t, "emetos.cfg")
	err := api.Settings().Register(extension.SettingSpec{
		Key: "emetos.cfg.size", Type: "integer", Default: 4, Description: "Size.", Minimum: settingsFloat(1), Maximum: settingsFloat(9),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := api.Settings().Register(extension.SettingSpec{Key: "emetos.cfg.names", Type: "array", Default: []any{"a"}}); err != nil {
		t.Fatal(err)
	}
	s, ok := r.registry.Lookup("emetos.cfg.size")
	if !ok || s.Category != "Ext emetos.cfg" || s.Description != "Size." {
		t.Fatalf("registered setting = %+v, %v", s, ok)
	}
	if v, _ := api.Settings().Get("emetos.cfg.size"); v != float64(4) {
		t.Errorf("default = %v (%T)", v, v)
	}
	// the value of the editor can be read too
	if v, err := api.Settings().Get("editor.tabSize"); err != nil || v != float64(4) {
		t.Errorf("editor.tabSize = %v, %v", v, err)
	}
	tb.changeSetting("emetos.cfg.size", 6)
	if n, err := extension.SettingValue[int](api.Settings(), "emetos.cfg.size"); err != nil || n != 6 {
		t.Errorf("SettingValue[int] = %v, %v", n, err)
	}
	if names, err := extension.SettingValue[[]string](api.Settings(), "emetos.cfg.names"); err != nil || !slices.Equal(names, []string{"a"}) {
		t.Errorf("SettingValue[[]string] = %v, %v", names, err)
	}
	if _, err := extension.SettingValue[bool](api.Settings(), "emetos.cfg.size"); err == nil {
		t.Errorf("a number was read as a bool")
	}
	// OnChange: called for a change, stops with cancel
	calls := 0
	cancel := api.Settings().OnChange(func() { calls++ })
	tb.changeSetting("emetos.cfg.size", 7)
	if calls != 1 {
		t.Errorf("%d calls after a change, want 1", calls)
	}
	cancel()
	tb.changeSetting("emetos.cfg.size", 8)
	if calls != 1 {
		t.Errorf("%d calls after cancel, want 1", calls)
	}
	if len(tb.posted) == 0 {
		t.Errorf("the listener was not handed over with Post")
	}
}

func settingsFloat(v float64) *float64 { return &v }

func TestMenusKeybindingsAndCommandsAreWorkingContributions(t *testing.T) {
	var ran []any
	api, _, tb := withAPI(t, "emetos.w")
	err := api.Commands().Register(extension.CommandSpec{ID: "w.go", Title: "Go", Category: "W", When: "editorFocus"}, func(_ context.Context, args any) error {
		ran = append(ran, args)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := api.Menus().Add(extension.MenuItem{Menu: extension.MenuEditorContext, Command: "w.go", Group: "9_w", Args: "menu", When: "editorTextFocus"}); err != nil {
		t.Fatal(err)
	}
	if err := api.Keybindings().Add(extension.Keybinding{Command: "w.go", Key: "ctrl+alt+w", When: "editorFocus", Args: "key"}); err != nil {
		t.Fatal(err)
	}
	meta, ok := tb.commands.Lookup("w.go")
	if !ok || meta.Title != "Go" || meta.Category != "W" || meta.Precondition == nil {
		t.Fatalf("meta = %+v", meta)
	}
	keys := contextkey.NewRoot()
	keys.Set("editorTextFocus", true)
	var entries []command.MenuEntry
	for _, e := range tb.menus.Items(extension.MenuEditorContext, keys) {
		if e.Command == "w.go" {
			entries = append(entries, e)
		}
	}
	if len(entries) != 1 || entries[0].Args != "menu" {
		t.Errorf("menu entries = %+v", entries)
	}
	if tb.keysCount() != 1 {
		t.Errorf("%d keybindings", tb.keysCount())
	}
	if err := tb.commands.Run(commandContext(), "w.go", "x"); err != nil || !slices.Equal(ran, []any{"x"}) {
		t.Errorf("run: %v, %v", err, ran)
	}
}

func (tb *tab) keysCount() int { return len(tb.keys.commands()) }

func TestStatusBarItemsUpdateAndDispose(t *testing.T) {
	api, _, tb := withAPI(t, "emetos.sb")
	clicks := 0
	h, err := api.StatusBar().Add(extension.StatusItem{ID: "n", Text: "one", Tooltip: "tip", Alignment: extension.StatusRight, Priority: 7, OnClick: func() { clicks++ }})
	if err != nil {
		t.Fatal(err)
	}
	items := tb.bar.Items(1)
	if len(items) != 1 || items[0].ID != "extension.emetos.sb.n" || items[0].Entry.Text != "one" || items[0].Entry.Tooltip != "tip" || items[0].Priority != 7 || items[0].Entry.Name != "Ext emetos.sb" {
		t.Fatalf("items = %+v", items)
	}
	click := items[0].Entry.Command
	if click == "" {
		t.Fatal("an item with OnClick has no command")
	}
	if err := tb.run(click); err != nil || clicks != 1 {
		t.Errorf("click: %v, %d", err, clicks)
	}
	if meta, ok := tb.commands.Lookup(click); !ok || meta.Title != "" {
		t.Errorf("the click command %+v must be hidden from the palette", meta)
	}
	// an update changes the text, and an item with a command instead of OnClick
	h.Update(extension.StatusItem{ID: "n", Text: "two", Command: "workbench.action.showCommands"})
	e := tb.bar.Items(1)[0].Entry
	if e.Text != "two" || e.Command != "workbench.action.showCommands" {
		t.Errorf("after Update: %+v", e)
	}
	if _, ok := tb.commands.Lookup(click); ok {
		t.Errorf("the click command stayed after the item lost its OnClick")
	}
	h.SetVisible(false)
	if got := len(tb.bar.Items(1)); got != 0 {
		t.Errorf("a hidden item is listed: %d", got)
	}
	h.SetVisible(true)
	h.Dispose()
	if got := tb.statusIDs(); len(got) != 0 {
		t.Errorf("a disposed item is still there: %v", got)
	}
	h.Update(extension.StatusItem{ID: "n", Text: "late", OnClick: func() {}})
	if got := tb.statusIDs(); len(got) != 0 {
		t.Errorf("a disposed item came back: %v", got)
	}
	// the id can be used again
	if _, err := api.StatusBar().Add(extension.StatusItem{ID: "n", Text: "again"}); err != nil {
		t.Errorf("an id that was disposed cannot be added again: %v", err)
	}
}

func TestNotificationsHaveTheNameOfTheExtensionAsSource(t *testing.T) {
	api, _, tb := withAPI(t, "emetos.nt")
	ran := 0
	api.Notifications().Info("an info", extension.NotificationAction{Label: "Do", Run: func() { ran++ }}, extension.NotificationAction{Label: "Plain"})
	api.Notifications().Warn("a warning")
	api.Notifications().Error("an error")
	items := tb.notify.Items()
	if len(items) != 3 {
		t.Fatalf("%d notifications", len(items))
	}
	for _, it := range items {
		if it.Source != "Ext emetos.nt" {
			t.Errorf("source = %q", it.Source)
		}
	}
	if len(items[0].Actions) != 2 || items[0].Actions[0].Label != "Do" || items[0].Actions[1].Run != nil {
		t.Fatalf("actions = %+v", items[0].Actions)
	}
	items[0].Actions[0].Run()
	if ran != 1 {
		t.Errorf("the action ran %d times", ran)
	}
}

func TestDocumentsAreReadThroughTheScope(t *testing.T) {
	api, _, tb := withAPI(t, "emetos.doc")
	tb.docs.texts["file:///work/one/a.txt"] = "hello"
	tb.docs.order = []string{"file:///work/one/a.txt"}
	text, err := api.Documents().Read(context.Background(), "file:///work/one/a.txt")
	if err != nil || text != "hello" {
		t.Errorf("Read = %q, %v", text, err)
	}
	if !slices.Equal(api.Documents().Open(), []string{"file:///work/one/a.txt"}) {
		t.Errorf("Open = %v", api.Documents().Open())
	}
	if _, err := api.Documents().Read(context.Background(), "file:///elsewhere"); err == nil {
		t.Errorf("a document the scope does not have was read")
	}
	if api.Scope().Name != "one" || api.Scope().Folder != "/work/one" || api.Scope().Kind != extension.ScopeWorkspace {
		t.Errorf("scope = %+v", api.Scope())
	}
}

func TestADocumentSourceIsOptional(t *testing.T) {
	var saved extension.API
	f, _ := factory(manifest("emetos.nodoc"), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error { saved = api; return nil }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.opts.Documents = nil
	tb.scope.Start()
	_, err := saved.Documents().Read(context.Background(), "file:///a")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("documentsAPI.Read.ExtensionDocumentUnavailable") {
		t.Errorf("error = %+v", appErr)
	}
	if saved.Documents().Open() != nil {
		t.Errorf("Open = %v", saved.Documents().Open())
	}
}

func TestTheLoggerNamesTheExtensionAndTheScope(t *testing.T) {
	api, _, tb := withAPI(t, "emetos.lg")
	api.Log().Info("hello from the extension")
	got := tb.log.String()
	for _, want := range []string{"hello from the extension", "extension=emetos.lg", "scope=one"} {
		if !strings.Contains(got, want) {
			t.Errorf("the log lacks %q:\n%s", want, got)
		}
	}
}

func TestPostRunsOnTheUIThreadUnlessTheExtensionIsGone(t *testing.T) {
	api, _, tb := withAPI(t, "emetos.post")
	ran := 0
	api.Post(func() { ran++ })
	api.Post(nil)
	if ran != 1 || len(tb.posted) != 1 {
		t.Errorf("ran %d times, %d handed to the UI thread", ran, len(tb.posted))
	}
	// a function that is still waiting for its turn when the extension goes is dropped
	var pending func()
	tb.scope.opts.Post = func(f func()) { pending = f }
	api.Post(func() { ran++ })
	tb.scope.Close()
	pending()
	if ran != 1 {
		t.Errorf("a posted function ran after the extension was deactivated")
	}
	api.Post(func() { ran++ })
	if ran != 1 {
		t.Errorf("Post ran a function after the extension was deactivated")
	}
}
