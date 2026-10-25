package fakeapi

import (
	"context"
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
		{"a status item twice", func() error { _, e := a.StatusBar().Add(extension.StatusItem{ID: "s"}); return e }(), "statusBar.Add.FakeStatusItemTaken"},
		{"a request", a.Events().Request(context.Background(), "t", nil, nil), "events.Request.FakeRequestUnsupported"},
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
