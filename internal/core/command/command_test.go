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

func TestNotificationCommands(t *testing.T) {
	r := DefaultRegistry()
	cases := []struct{ id, title, category string }{
		{"notifications.showList", "Show Notifications", "Notifications"},
		{"notifications.hideList", "Hide Notifications", "Notifications"},
		{"notifications.toggleList", "", ""},
		{"notifications.hideToasts", "", ""},
		{"notifications.focusToasts", "Focus Notification Toast", "Notifications"},
		{"notifications.clearAll", "Clear All Notifications", "Notifications"},
		{"notifications.toggleDoNotDisturbMode", "Toggle Do Not Disturb Mode", "Notifications"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			m, ok := r.Lookup(tc.id)
			if !ok {
				t.Fatal("not registered")
			}
			if m.Title != tc.title || m.Category != tc.category || m.Precondition != nil {
				t.Errorf("title %q category %q precondition %v, want %q %q", m.Title, m.Category, m.Precondition, tc.title, tc.category)
			}
		})
	}
}

func TestPanelViewCommands(t *testing.T) {
	r := DefaultRegistry()
	cases := []struct{ id, title, category string }{
		{"workbench.actions.view.problems", "Toggle Problems", "View"},
		{"workbench.action.output.toggleOutput", "Toggle Output", "View"},
		{"workbench.panel.markers.view.focus", "Focus on Problems View", "View"},
		{"workbench.panel.output.focus", "Focus on Output View", "View"},
		{"workbench.action.problems.focus", "Focus Problems (Errors, Warnings, Infos)", "View"},
		{"workbench.actions.view.toggleProblems", "Toggle Problems (Errors, Warnings, Infos)", ""},
		{"workbench.action.showErrorsWarnings", "", ""},
		{"problems.action.focusFilter", "Focus problems filter", ""},
		{"problems.action.focusProblemsFromFilter", "Focus problems view", ""},
		{"problems.action.clearFilterText", "Clear filters text", "Problems"},
		{"workbench.actions.treeView.workbench.panel.markers.view.collapseAll", "Collapse All", ""},
		{"workbench.actions.workbench.panel.markers.view.toggleErrors", "Show Errors", "Problems"},
		{"workbench.actions.workbench.panel.markers.view.toggleWarnings", "Show Warnings", "Problems"},
		{"workbench.actions.workbench.panel.markers.view.toggleInfos", "Show Infos", "Problems"},
		{"workbench.actions.workbench.panel.markers.view.toggleActiveFile", "Show Active File Only", "Problems"},
		{"workbench.output.action.clearOutput", "Clear Output", "View"},
		{"workbench.output.action.toggleAutoScroll", "Toggle Auto Scrolling", ""},
		{"workbench.action.openActiveLogOutputFile", "Open Output in Editor", ""},
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
		})
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

func TestForkCopiesTheCommandsAndTheirHandlers(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Meta{ID: "a", Title: "A"}, Meta{ID: "b"})
	var ran []string
	handler := func(name string) Handler {
		return func(Context, any) error { ran = append(ran, name); return nil }
	}
	if _, err := r.SetHandler("a", handler("process")); err != nil {
		t.Fatal(err)
	}
	f := r.Fork()
	if meta, ok := f.Lookup("a"); !ok || meta.Title != "A" || len(f.All()) != 2 {
		t.Fatalf("the fork lost the commands: %v %v", meta, f.All())
	}
	run := func(reg *Registry, id string) {
		t.Helper()
		if err := reg.Run(Context{Ctx: context.Background()}, id, nil); err != nil {
			t.Fatal(err)
		}
	}
	// the handler the registry had is the fork's too
	run(f, "a")
	// a handler set in the fork is the fork's only, and so is its removal
	remove, err := f.SetHandler("a", handler("fork"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.SetHandler("b", handler("fork b")); err != nil {
		t.Fatal(err)
	}
	run(f, "a")
	run(r, "a")
	if r.HasHandler("b") {
		t.Error("a handler of the fork reached the registry")
	}
	remove()
	run(f, "a")
	// and what is set in the registry later does not reach the fork
	if _, err := r.SetHandler("a", handler("process again")); err != nil {
		t.Fatal(err)
	}
	run(f, "a")
	want := []string{"process", "fork", "process", "process", "process"}
	if len(ran) != len(want) {
		t.Fatalf("ran %v, want %v", ran, want)
	}
	for i := range want {
		if ran[i] != want[i] {
			t.Errorf("ran %v, want %v", ran, want)
			break
		}
	}
}

func TestDiffEditorCommands(t *testing.T) {
	r := DefaultRegistry()
	for id, want := range map[string]Meta{
		"workbench.action.compareEditor.nextChange":         {Title: "Go to Next Change"},
		"workbench.action.compareEditor.previousChange":     {Title: "Go to Previous Change"},
		"workbench.action.compareEditor.focusPrimarySide":   {},
		"workbench.action.compareEditor.focusSecondarySide": {},
		"workbench.action.compareEditor.focusOtherSide":     {},
		"diffEditor.switchSide":                             {Title: "Switch Side", Category: "Diff Editor"},
	} {
		got, ok := r.Lookup(id)
		if !ok {
			t.Errorf("%s is not registered", id)
			continue
		}
		if got.Title != want.Title || got.Category != want.Category {
			t.Errorf("%s: title %q category %q, want %q %q", id, got.Title, got.Category, want.Title, want.Category)
		}
	}
	// Switch Side needs the cursor to be in a diff editor
	meta, _ := r.Lookup("diffEditor.switchSide")
	if meta.Precondition == nil || !meta.Precondition.Evaluate(contextkey.MapContext{"isInDiffEditor": true}) || meta.Precondition.Evaluate(contextkey.MapContext{}) {
		t.Errorf("precondition %v", meta.Precondition)
	}
}

func TestInlineViewAndCompareCommands(t *testing.T) {
	r := DefaultRegistry()
	for id, want := range map[string]Meta{
		"toggle.diff.renderSideBySide":                     {Title: "Toggle Inline View", Category: "Compare"},
		"diffEditor.toggleUseInlineViewWhenSpaceIsLimited": {Title: "Toggle Use Inline View When Space Is Limited"},
		"diffEditor.setViewMode.inline":                    {},
		"diffEditor.setViewMode.sideBySide":                {},
		"diffEditor.setViewMode.automatic":                 {},
		"workbench.files.action.compareWithSaved":          {Title: "Compare Active File with Saved", Category: "File"},
		"workbench.files.action.compareFileWith":           {Title: "Compare Active File With...", Category: "File"},
		"workbench.files.action.compareWithClipboard":      {Title: "Compare Active File with Clipboard", Category: "File"},
		"selectForCompare":                                 {},
		"compareFiles":                                     {},
		"compareSelected":                                  {},
	} {
		got, ok := r.Lookup(id)
		if !ok {
			t.Errorf("%s is not registered", id)
			continue
		}
		if got.Title != want.Title || got.Category != want.Category {
			t.Errorf("%s: title %q category %q, want %q %q", id, got.Title, got.Category, want.Title, want.Category)
		}
	}
	// Compare Active File With... needs an editor
	meta, _ := r.Lookup("workbench.files.action.compareFileWith")
	if meta.Precondition == nil || !meta.Precondition.Evaluate(contextkey.MapContext{"activeEditor": "emetos.textEditor"}) || meta.Precondition.Evaluate(contextkey.MapContext{}) {
		t.Errorf("precondition %v", meta.Precondition)
	}
}

func TestPaletteRulesOfTheInlineView(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	listed := func(id string, ctx contextkey.MapContext) bool {
		for _, it := range menus.Items(CommandPalette, ctx) {
			if it.Command == id {
				return true
			}
		}
		return false
	}
	// a row of the CommandPalette menu says when the palette hides its command: Items lists the rows that hold
	for _, tc := range []struct {
		name string
		id   string
		ctx  contextkey.MapContext
		want bool
	}{
		{"Toggle Inline View with a compare editor", "toggle.diff.renderSideBySide", contextkey.MapContext{"textCompareEditorActive": true}, true},
		{"Toggle Inline View without one", "toggle.diff.renderSideBySide", contextkey.MapContext{}, false},
		{"the toggle of the limited space is never listed", "diffEditor.toggleUseInlineViewWhenSpaceIsLimited", contextkey.MapContext{"isInDiffEditor": true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := listed(tc.id, tc.ctx); got != tc.want {
				t.Errorf("listed = %v", got)
			}
		})
	}
}

func TestMergeEditorActionCommands(t *testing.T) {
	r := DefaultRegistry()
	for id, title := range map[string]string{
		"merge.acceptAllInput1":                     "Accept All Incoming Changes from Left",
		"merge.acceptAllInput2":                     "Accept All Current Changes from Right",
		"merge.toggleActiveConflictInput1":          "Toggle Current Conflict from Left",
		"merge.toggleActiveConflictInput2":          "Toggle Current Conflict from Right",
		"mergeEditor.resetResultToBaseAndAutoMerge": "Reset Result",
		"mergeEditor.acceptAllCombination":          "Accept All Combination",
		"mergeEditor.acceptMerge":                   "Complete Merge",
	} {
		got, ok := r.Lookup(id)
		if !ok {
			t.Errorf("%s is not registered", id)
			continue
		}
		if got.Title != title || got.Category != "Merge Editor" {
			t.Errorf("%s: title %q category %q, want %q in Merge Editor", id, got.Title, got.Category, title)
		}
		// they need a merge editor
		if got.Precondition == nil || !got.Precondition.Evaluate(contextkey.MapContext{"isMergeEditor": true}) || got.Precondition.Evaluate(contextkey.MapContext{}) {
			t.Errorf("%s: precondition %v", id, got.Precondition)
		}
	}
}

func TestUnregister(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(Meta{ID: "a", Title: "A"}, Meta{ID: "b", Title: "B"})
	if _, err := r.SetHandler("a", func(Context, any) error { return nil }); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"a command with a handler", "a", true},
		{"a command twice", "a", false},
		{"a command without a handler", "b", true},
		{"an unknown command", "zzz", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Unregister(tt.id); got != tt.want {
				t.Errorf("Unregister(%q) = %v, want %v", tt.id, got, tt.want)
			}
			if _, ok := r.Lookup(tt.id); ok {
				t.Errorf("%q is still registered", tt.id)
			}
		})
	}
	assertRoute(t, r.Run(Context{Ctx: context.Background()}, "a", nil), "Run.CommandNotFound")
	if err := r.Register(Meta{ID: "a", Title: "Again"}); err != nil {
		t.Errorf("an id that was unregistered cannot be registered again: %v", err)
	}
	// the fork of a window keeps its own set
	fork := r.Fork()
	fork.Unregister("a")
	if _, ok := r.Lookup("a"); !ok {
		t.Errorf("unregistering in the fork removed the command from the original")
	}
}
