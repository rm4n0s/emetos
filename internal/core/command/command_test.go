package command

import (
	"context"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/errors"
)

func assertRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func TestRegisterErrors(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Meta{ID: "ok"}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		meta  Meta
		route string
	}{
		{"empty", Meta{}, "Register.CommandIDInvalid"},
		{"white space", Meta{ID: "a b"}, "Register.CommandIDInvalid"},
		{"removal prefix", Meta{ID: "-a"}, "Register.CommandIDInvalid"},
		{"bubble prefix", Meta{ID: "^a"}, "Register.CommandIDInvalid"},
		{"duplicate", Meta{ID: "ok"}, "Register.CommandDuplicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertRoute(t, r.Register(tc.meta), tc.route)
		})
	}
	defer func() {
		if recover() == nil {
			t.Error("MustRegister must panic on a duplicate")
		}
	}()
	r.MustRegister(Meta{ID: "ok"})
}

func TestRunErrors(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Meta{ID: "bare"})
	ctx := Context{Ctx: context.Background()}

	assertRoute(t, r.Run(ctx, "missing", nil), "Run.CommandNotFound")
	assertRoute(t, r.Run(ctx, "bare", nil), "Run.CommandHandlerMissing")

	_, err := r.SetHandler("missing", func(Context, any) error { return nil })
	assertRoute(t, err, "SetHandler.CommandNotFound")
	_, err = r.SetHandler("bare", nil)
	assertRoute(t, err, "SetHandler.CommandHandlerInvalid")
	if r.HasHandler("bare") || r.HasHandler("missing") {
		t.Error("no handler was set")
	}
}

func TestRunPassesContextAndArguments(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Meta{ID: "cmd", Title: "A Command"})
	var gotCtx Context
	var gotArgs any
	remove, err := r.SetHandler("cmd", func(ctx Context, args any) error {
		gotCtx, gotArgs = ctx, args
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.HasHandler("cmd") {
		t.Error("HasHandler")
	}
	keys := contextkey.MapContext{"a": true}
	args := map[string]any{"text": "hi"}
	if err := r.Run(Context{Ctx: context.Background(), Keys: keys, Source: SourceKeybinding}, "cmd", args); err != nil {
		t.Fatal(err)
	}
	if gotCtx.Source != SourceKeybinding || gotCtx.Keys == nil || gotArgs.(map[string]any)["text"] != "hi" {
		t.Errorf("got %+v %v", gotCtx, gotArgs)
	}
	remove()
	remove() // a second call does nothing
	if r.HasHandler("cmd") {
		t.Error("the handler was removed")
	}
}

func TestHandlerStack(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Meta{ID: "cmd"})
	var calls []string
	handler := func(name string) Handler {
		return func(Context, any) error {
			calls = append(calls, name)
			return nil
		}
	}
	removeA, _ := r.SetHandler("cmd", handler("a"))
	removeB, _ := r.SetHandler("cmd", handler("b"))
	ctx := Context{Ctx: context.Background()}
	for range 2 {
		if err := r.Run(ctx, "cmd", nil); err != nil {
			t.Fatal(err)
		}
	}
	removeB()
	if err := r.Run(ctx, "cmd", nil); err != nil {
		t.Fatal(err)
	}
	removeA()
	assertRoute(t, r.Run(ctx, "cmd", nil), "Run.CommandHandlerMissing")
	if got := len(calls); got != 3 || calls[0] != "b" || calls[1] != "b" || calls[2] != "a" {
		t.Errorf("calls: %v", calls)
	}
}

func TestHandlerError(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Meta{ID: "cmd"})
	boom := errors.New("Boom", "it failed")
	if _, err := r.SetHandler("cmd", func(Context, any) error { return boom }); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(Context{Ctx: context.Background()}, "cmd", nil); err != boom {
		t.Errorf("the handler's error must come back untouched, got %v", err)
	}
}

func TestEnabledAndLookup(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(
		Meta{ID: "b", Title: "B", Category: "Cat", Precondition: contextkey.Has("flag"), Description: "does b"},
		Meta{ID: "a"},
	)
	if m, ok := r.Lookup("b"); !ok || m.Title != "B" || m.Category != "Cat" || m.Description != "does b" {
		t.Errorf("Lookup: %+v %v", m, ok)
	}
	if _, ok := r.Lookup("zzz"); ok {
		t.Error("unknown command found")
	}
	if !r.Enabled("b", contextkey.MapContext{"flag": true}) || r.Enabled("b", contextkey.MapContext{}) {
		t.Error("Enabled follows the precondition")
	}
	if !r.Enabled("a", contextkey.MapContext{}) || r.Enabled("zzz", contextkey.MapContext{}) {
		t.Error("no precondition means enabled; unknown means not")
	}
	all := r.All()
	if len(all) != 2 || all[0].ID != "a" || all[1].ID != "b" {
		t.Errorf("All: %+v", all)
	}
}

// TestDefaultRegistry checks the table of built-in commands: VS Code's ids, no duplicates, and
// the titles and preconditions of a few.
func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	all := r.All()
	if len(all) < 100 {
		t.Fatalf("only %d commands", len(all))
	}
	for _, m := range all {
		if m.Precondition != nil && m.Precondition.Serialize() == "" {
			t.Errorf("%s: empty precondition", m.ID)
		}
	}
	cases := []struct {
		id    string
		title string
		pre   string
	}{
		{"cursorLeft", "", ""},
		{"cancelSelection", "", "editorHasSelection"},
		{"removeSecondaryCursors", "", "editorHasMultipleSelections"},
		{"deleteLeft", "", "!editorReadonly"},
		{"tab", "", "!editorReadonly"},
		{"undo", "Undo", ""},
		{"redo", "Redo", ""},
		{"editor.action.selectAll", "Select All", ""},
		{"editor.action.moveLinesUpAction", "Move Line Up", "!editorReadonly"},
		{"editor.action.copyLinesDownAction", "Copy Line Down", "!editorReadonly"},
		{"editor.action.deleteLines", "Delete Line", "!editorReadonly"},
		{"editor.action.transformToTitlecase", "Transform to Title Case", "!editorReadonly"},
		{"editor.action.insertCursorAbove", "Add Cursor Above", ""},
		{"editor.action.addSelectionToNextFindMatch", "Add Selection to Next Find Match", ""},
		{"editor.action.smartSelect.expand", "Expand Selection", ""},
		{"deleteInsideWord", "Delete Word", "!editorReadonly"},
		{"cursorWordPartLeftSelect", "", ""},
		{"expandLineSelection", "Expand Line Selection", ""},
	}
	for _, tc := range cases {
		m, ok := r.Lookup(tc.id)
		if !ok {
			t.Errorf("%s is not registered", tc.id)
			continue
		}
		pre := ""
		if m.Precondition != nil {
			pre = m.Precondition.Serialize()
		}
		if m.Title != tc.title || pre != tc.pre {
			t.Errorf("%s: title %q precondition %q, want %q %q", tc.id, m.Title, pre, tc.title, tc.pre)
		}
	}
}

func TestLayoutCommands(t *testing.T) {
	r := DefaultRegistry()
	maximizable := "panelAlignment == 'center' || panelPosition != 'bottom' && panelPosition != 'top'"
	cases := []struct {
		id, title, category, pre string
	}{
		{"workbench.action.toggleSidebarVisibility", "Toggle Primary Side Bar Visibility", "View", ""},
		{"workbench.action.toggleActivityBarVisibility", "Toggle Activity Bar Visibility", "", ""},
		{"workbench.action.toggleStatusbarVisibility", "Toggle Status Bar Visibility", "View", ""},
		{"workbench.action.toggleMenuBar", "Toggle Menu Bar", "View", ""},
		{"workbench.action.togglePanel", "Toggle Panel Visibility", "View", ""},
		{"workbench.action.toggleMaximizedPanel", "Toggle Maximized Panel", "View", maximizable},
		{"workbench.action.positionPanelLeft", "Move Panel Left", "View", ""},
		{"workbench.action.positionPanelRight", "Move Panel Right", "View", ""},
		{"workbench.action.positionPanelBottom", "Move Panel To Bottom", "View", ""},
		{"workbench.action.positionPanelTop", "Move Panel To Top", "View", ""},
		{"workbench.action.focusSideBar", "Focus into Primary Side Bar", "View", ""},
		{"workbench.action.focusActiveEditorGroup", "Focus Active Editor Group", "View", ""},
		{"workbench.action.focusPanel", "Focus into Panel", "View", ""},
		{"workbench.action.focusStatusBar", "Focus Status Bar", "View", ""},
		{"workbench.action.focusActivityBar", "Focus Activity Bar", "View", ""},
		{"workbench.actions.menubar.focus", "Focus Application Menu", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			m, ok := r.Lookup(tc.id)
			if !ok {
				t.Fatal("not registered")
			}
			if m.Title != tc.title || m.Category != tc.category {
				t.Errorf("title %q category %q, want %q %q", m.Title, m.Category, tc.title, tc.category)
			}
			pre := ""
			if m.Precondition != nil {
				pre = m.Precondition.Serialize()
			}
			if pre != tc.pre {
				t.Errorf("precondition %q, want %q", pre, tc.pre)
			}
		})
	}
	// the panel can be maximized at the bottom, since its alignment is centered, and not when the alignment is something else
	keys := func(alignment, position string) contextkey.Context {
		return contextkey.MapContext{"panelAlignment": alignment, "panelPosition": position}
	}
	for _, tc := range []struct {
		alignment, position string
		want                bool
	}{
		{"center", "bottom", true}, {"center", "right", true}, {"left", "bottom", false}, {"justify", "top", false}, {"left", "left", true},
	} {
		if got := r.Enabled("workbench.action.toggleMaximizedPanel", keys(tc.alignment, tc.position)); got != tc.want {
			t.Errorf("toggleMaximizedPanel with alignment %s, position %s: enabled = %v, want %v", tc.alignment, tc.position, got, tc.want)
		}
	}
}
