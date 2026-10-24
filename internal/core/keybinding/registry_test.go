package keybinding

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

func TestRegistryOrder(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(
		Rule{Command: "b.contrib", Weight: WeightEditorContrib, Primary: "ctrl+1"},
		Rule{Command: "z.core", Weight: WeightEditorCore, Primary: "ctrl+2", Secondary: []string{"ctrl+3", "ctrl+4"}},
		Rule{Command: "a.core", Weight: WeightEditorCore, Primary: "ctrl+5"},
		Rule{Command: "a.core", Weight: WeightEditorCore, Primary: "ctrl+6"},
		Rule{Command: "none", Weight: WeightEditorCore},
	)
	var got []string
	for _, it := range r.Defaults() {
		got = append(got, it.Command+" "+it.Chords[0])
	}
	// By weight, then command, then the second weight (the secondary keys come first so
	// that the primary key is the last, and so the one shown); equal ones keep their order.
	want := []string{"a.core ctrl+5", "a.core ctrl+6", "z.core ctrl+4", "z.core ctrl+3", "z.core ctrl+2", "b.contrib ctrl+1"}
	if !slices.Equal(got, want) {
		t.Errorf("order:\n got %q\nwant %q", got, want)
	}
	for _, it := range r.Defaults() {
		if !it.IsDefault {
			t.Errorf("%s must be a default", it.Command)
		}
	}
}

func TestRegisterErrors(t *testing.T) {
	r := NewRegistry()
	assertRoute(t, r.Register(Rule{Command: "x", Primary: "ctrl+nonsense"}), "Register->ParseKeybinding.KeybindingKeyUnknown")
	assertRoute(t, r.Register(Rule{Command: "x", Primary: "a", Secondary: []string{"ctrl+"}}), "Register->ParseKeybinding.KeybindingKeyUnknown")
	if got := r.Defaults(); len(got) != 1 {
		t.Errorf("the valid primary key stays registered: %v", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("MustRegister must panic on an invalid rule")
		}
	}()
	r.MustRegister(Rule{Command: "y", Primary: "[KeyA]"})
}

// defaultContext makes the context keys of a focused, writable editor with the given extra
// keys.
func defaultContext(extra map[string]any) contextkey.MapContext {
	ctx := contextkey.MapContext{
		"editorTextFocus": true, "textInputFocus": true, "editorFocus": true,
	}
	for k, v := range extra {
		ctx[k] = v
	}
	return ctx
}

func TestDefaultKeymap(t *testing.T) {
	resolver := NewResolver(DefaultRegistry().Defaults(), nil, nil)
	cases := []struct {
		name  string
		extra map[string]any
		press string
		want  string
	}{
		{"left", nil, "left", "cursorLeft"},
		{"shift+left", nil, "shift+left", "cursorLeftSelect"},
		{"shift+up on Linux", nil, "shift+up", "cursorUpSelect"},
		{"ctrl+shift+up inserts a cursor on Linux", nil, "ctrl+shift+up", "editor.action.insertCursorAbove"},
		{"shift+alt+up", nil, "shift+alt+up", "editor.action.insertCursorAbove"},
		{"shift+alt+down", nil, "shift+alt+down", "editor.action.insertCursorBelow"},
		{"column selection", map[string]any{"editorColumnSelection": true}, "shift+left", "cursorColumnSelectLeft"},
		{"no column selection", nil, "shift+left", "cursorLeftSelect"},
		{"ctrl+left", nil, "ctrl+left", "cursorWordLeft"},
		{"ctrl+right", nil, "ctrl+right", "cursorWordEndRight"},
		{"ctrl+shift+right", nil, "ctrl+shift+right", "cursorWordEndRightSelect"},
		{"home", nil, "home", "cursorHome"},
		{"ctrl+end", nil, "ctrl+end", "cursorBottom"},
		{"escape with a selection", map[string]any{"editorHasSelection": true}, "escape", "cancelSelection"},
		{"escape with several selections", map[string]any{"editorHasMultipleSelections": true}, "escape", "removeSecondaryCursors"},
		{"escape with both", map[string]any{"editorHasSelection": true, "editorHasMultipleSelections": true}, "escape", "removeSecondaryCursors"},
		{"escape with nothing", nil, "escape", ""},
		{"backspace", nil, "backspace", "deleteLeft"},
		{"shift+backspace", nil, "shift+backspace", "deleteLeft"},
		{"backspace read-only", map[string]any{"editorReadonly": true}, "backspace", ""},
		{"delete", nil, "delete", "deleteRight"},
		{"ctrl+backspace", nil, "ctrl+backspace", "deleteWordLeft"},
		{"ctrl+delete", nil, "ctrl+delete", "deleteWordRight"},
		{"tab", nil, "tab", "tab"},
		{"tab moves focus", map[string]any{"editorTabMovesFocus": true}, "tab", ""},
		{"shift+tab", nil, "shift+tab", "outdent"},
		{"tab with Emmet on", map[string]any{"config.emmet.triggerExpansionOnTab": true}, "tab", "editor.emmet.action.expandAbbreviation"},
		{"tab with Emmet on and several cursors", map[string]any{"config.emmet.triggerExpansionOnTab": true, "editorHasMultipleSelections": true}, "tab", "editor.emmet.action.expandAbbreviation"},
		{"tab with Emmet on and a selection", map[string]any{"config.emmet.triggerExpansionOnTab": true, "editorHasSelection": true}, "tab", "tab"},
		{"tab with Emmet on, read-only", map[string]any{"config.emmet.triggerExpansionOnTab": true, "editorReadonly": true}, "tab", ""},
		{"tab with Emmet on in a snippet", map[string]any{"config.emmet.triggerExpansionOnTab": true, "inSnippetMode": true, "hasNextTabstop": true}, "tab", "jumpToNextSnippetPlaceholder"},
		{"tab with Emmet on and a snippet prefix", map[string]any{"config.emmet.triggerExpansionOnTab": true, "hasSnippetCompletions": true}, "tab", "insertSnippet"},
		{"tab with snippets before the cursor", map[string]any{"hasSnippetCompletions": true}, "tab", "insertSnippet"},
		{"tab with snippets before the cursor, tab moves focus", map[string]any{"hasSnippetCompletions": true, "editorTabMovesFocus": true}, "tab", ""},
		{"tab in a snippet", map[string]any{"inSnippetMode": true, "hasNextTabstop": true, "hasSnippetCompletions": true}, "tab", "jumpToNextSnippetPlaceholder"},
		{"tab at the last tab stop", map[string]any{"inSnippetMode": true, "hasSnippetCompletions": true}, "tab", "tab"},
		{"tab in a snippet, read-only", map[string]any{"inSnippetMode": true, "hasNextTabstop": true, "editorReadonly": true}, "tab", "jumpToNextSnippetPlaceholder"},
		{"shift+tab in a snippet", map[string]any{"inSnippetMode": true, "hasPrevTabstop": true}, "shift+tab", "jumpToPrevSnippetPlaceholder"},
		{"shift+tab at the first tab stop", map[string]any{"inSnippetMode": true}, "shift+tab", "outdent"},
		{"escape in a snippet", map[string]any{"inSnippetMode": true}, "escape", "leaveSnippet"},
		{"shift+escape in a snippet", map[string]any{"inSnippetMode": true}, "shift+escape", "leaveSnippet"},
		{"escape in a snippet with several cursors", map[string]any{"inSnippetMode": true, "editorHasMultipleSelections": true}, "escape", "leaveSnippet"},
		{"ctrl+z", nil, "ctrl+z", "undo"},
		{"ctrl+y", nil, "ctrl+y", "redo"},
		{"ctrl+shift+z", nil, "ctrl+shift+z", "redo"},
		{"ctrl+a", nil, "ctrl+a", "editor.action.selectAll"},
		{"ctrl+u", nil, "ctrl+u", "cursorUndo"},
		{"ctrl+l", nil, "ctrl+l", "expandLineSelection"},
		{"alt+up", nil, "alt+up", "editor.action.moveLinesUpAction"},
		{"alt+up read-only", map[string]any{"editorReadonly": true}, "alt+up", ""},
		{"ctrl+alt+shift+down", nil, "ctrl+alt+shift+down", "editor.action.copyLinesDownAction"},
		{"ctrl+shift+k", nil, "ctrl+shift+k", "editor.action.deleteLines"},
		{"ctrl+]", nil, "ctrl+]", "editor.action.indentLines"},
		{"ctrl+[", nil, "ctrl+[", "editor.action.outdentLines"},
		{"ctrl+enter", nil, "ctrl+enter", "editor.action.insertLineAfter"},
		{"ctrl+shift+enter", nil, "ctrl+shift+enter", "editor.action.insertLineBefore"},
		{"ctrl+d", nil, "ctrl+d", "editor.action.addSelectionToNextFindMatch"},
		{"ctrl+shift+l", nil, "ctrl+shift+l", "editor.action.selectHighlights"},
		{"shift+alt+i", nil, "shift+alt+i", "editor.action.insertCursorAtEndOfEachLineSelected"},
		{"shift+alt+right", nil, "shift+alt+right", "editor.action.smartSelect.expand"},
		{"shift+alt+left", nil, "shift+alt+left", "editor.action.smartSelect.shrink"},
		{"ctrl+j toggles the panel (join lines has no key on Linux)", nil, "ctrl+j", "workbench.action.togglePanel"},
		{"ctrl+b toggles the side bar", nil, "ctrl+b", "workbench.action.toggleSidebarVisibility"},
		{"ctrl+0 focuses the side bar", nil, "ctrl+0", "workbench.action.focusSideBar"},
		{"ctrl+w closes the editor", nil, "ctrl+w", "workbench.action.closeActiveEditor"},
		{"alt+f4 closes the window", nil, "alt+f4", "workbench.action.closeWindow"},
		{"ctrl+shift+w closes the window", nil, "ctrl+shift+w", "workbench.action.closeWindow"},
		{"ctrl+q quits", nil, "ctrl+q", "workbench.action.quit"},
		{"ctrl+pagedown opens the next editor", nil, "ctrl+pagedown", "workbench.action.nextEditor"},
		{"ctrl+pageup opens the previous editor", nil, "ctrl+pageup", "workbench.action.previousEditor"},
		{"ctrl+tab opens the editor picker", nil, "ctrl+tab", "workbench.action.quickOpenPreviousRecentlyUsedEditorInGroup"},
		{"ctrl+shift+tab opens the editor picker at the last editor", nil, "ctrl+shift+tab", "workbench.action.quickOpenLeastRecentlyUsedEditorInGroup"},
		{"ctrl+tab does nothing in an empty group", map[string]any{"activeEditorGroupEmpty": true}, "ctrl+tab", ""},
		{"ctrl+tab in the editor picker goes to the next editor", map[string]any{"inQuickOpen": true, "inEditorsPicker": true}, "ctrl+tab", "workbench.action.quickOpenNavigateNextInEditorPicker"},
		{"ctrl+shift+tab in the editor picker goes to the previous editor", map[string]any{"inQuickOpen": true, "inEditorsPicker": true}, "ctrl+shift+tab", "workbench.action.quickOpenNavigatePreviousInEditorPicker"},
		{"ctrl+tab in another picker opens the editor picker", map[string]any{"inQuickOpen": true}, "ctrl+tab", "workbench.action.quickOpenPreviousRecentlyUsedEditorInGroup"},
		{"ctrl+alt+- goes back", map[string]any{"canNavigateBack": true}, "ctrl+alt+-", "workbench.action.navigateBack"},
		{"ctrl+alt+- does nothing when it cannot go back", nil, "ctrl+alt+-", ""},
		{"ctrl+shift+- goes forward", map[string]any{"canNavigateForward": true}, "ctrl+shift+-", "workbench.action.navigateForward"},
		{"ctrl+shift+- does nothing when it cannot go forward", nil, "ctrl+shift+-", ""},
		{"ctrl+shift+t reopens the closed editor", nil, "ctrl+shift+t", "workbench.action.reopenClosedEditor"},
		{"ctrl+w closes an empty group when there are several", map[string]any{"activeEditorGroupEmpty": true, "multipleEditorGroups": true}, "ctrl+w", "workbench.action.closeGroup"},
		{"ctrl+w closes the editor of a group that is not empty", map[string]any{"multipleEditorGroups": true}, "ctrl+w", "workbench.action.closeActiveEditor"},
		{"ctrl+w closes the editor when the only group is empty", map[string]any{"activeEditorGroupEmpty": true}, "ctrl+w", "workbench.action.closeActiveEditor"},
		{"ctrl+backslash splits the editor", nil, "ctrl+\\", "workbench.action.splitEditor"},
		{"ctrl+1 focuses the first group", nil, "ctrl+1", "workbench.action.focusFirstEditorGroup"},
		{"ctrl+8 focuses the eighth group", nil, "ctrl+8", "workbench.action.focusEighthEditorGroup"},
		{"ctrl+alt+right moves the editor to the next group", nil, "ctrl+alt+right", "workbench.action.moveEditorToNextGroup"},
		{"ctrl+alt+left moves the editor to the previous group", nil, "ctrl+alt+left", "workbench.action.moveEditorToPreviousGroup"},
		{"shift+alt+1 moves the editor to the first group", nil, "shift+alt+1", "workbench.action.moveEditorToFirstGroup"},
		{"ctrl+b in the find input still toggles the side bar", map[string]any{"findInputFocussed": true}, "ctrl+b", "workbench.action.toggleSidebarVisibility"},
		{"ctrl+shift+m toggles the Problems", nil, "ctrl+shift+m", "workbench.actions.view.problems"},
		{"ctrl+f in the Problems view focuses its filter", map[string]any{"focusedView": "workbench.panel.markers.view"}, "ctrl+f", "problems.action.focusFilter"},
		{"ctrl+f in another view finds", map[string]any{"focusedView": "workbench.explorer.fileView"}, "ctrl+f", "actions.find"},
		{"ctrl+down in the filter focuses the problems", map[string]any{"problemsFilterFocus": true}, "ctrl+down", "problems.action.focusProblemsFromFilter"},
		{"escape in the filter clears it", map[string]any{"problemsFilterFocus": true}, "escape", "problems.action.clearFilterText"},
		{"escape with no filter focus", nil, "escape", ""},
		{"escape with the notification center open", map[string]any{"notificationCenterVisible": true}, "escape", "notifications.hideList"},
		{"escape with the center open and a selection", map[string]any{"notificationCenterVisible": true, "editorHasSelection": true}, "escape", "notifications.hideList"},
		{"escape with a toast", map[string]any{"notificationToastsVisible": true}, "escape", "notifications.hideToasts"},
		{"escape with a toast and a selection", map[string]any{"notificationToastsVisible": true, "editorHasSelection": true}, "escape", "notifications.hideToasts"},
		{"escape with a toast that has the focus", map[string]any{"notificationToastsVisible": true, "notificationFocus": true}, "escape", "notifications.hideToasts"},
		{"escape in a snippet with a toast", map[string]any{"notificationToastsVisible": true, "inSnippetMode": true}, "escape", "notifications.hideToasts"},
		{"escape with the center and a toast", map[string]any{"notificationCenterVisible": true, "notificationToastsVisible": true}, "escape", "notifications.hideList"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := resolver.Resolve(defaultContext(tc.extra), nil, dispatchOf(t, tc.press))
			got := ""
			if res.Kind == KbFound {
				got = res.Command
			}
			if got != tc.want {
				t.Errorf("got %q, want %q (%+v)", got, tc.want, res)
			}
		})
	}

	// pin and unpin share a chord and tell apart by whether the active editor is pinned
	kb := mustKB(t, "ctrl+k shift+enter")
	pinned := defaultContext(map[string]any{"activeEditorIsPinned": true})
	if res := resolver.Resolve(pinned, []string{kb[0].Dispatch()}, kb[1].Dispatch()); res.Kind != KbFound || res.Command != "workbench.action.unpinEditor" {
		t.Errorf("ctrl+k shift+enter with a pinned editor: %+v", res)
	}

	// Chords.
	for _, tc := range []struct{ chords, want string }{
		{"ctrl+k ctrl+x", "editor.action.trimTrailingWhitespace"},
		{"ctrl+k ctrl+d", "editor.action.moveSelectionToNextFindMatch"},
		{"ctrl+k ctrl+w", "workbench.action.closeAllEditors"},
		{"ctrl+k ctrl+p", "workbench.action.showAllEditors"},
		{"ctrl+k ctrl+pagedown", "workbench.action.nextEditorInGroup"},
		{"ctrl+k ctrl+pageup", "workbench.action.previousEditorInGroup"},
		{"ctrl+k ctrl+q", "workbench.action.navigateToLastEditLocation"},
		{"ctrl+k w", "workbench.action.closeEditorsInGroup"},
		{"ctrl+k u", "workbench.action.closeUnmodifiedEditors"},
		{"ctrl+k enter", "workbench.action.keepEditor"},
		{"ctrl+k shift+enter", "workbench.action.pinEditor"},
		{"ctrl+k ctrl+\\", "workbench.action.splitEditorOrthogonal"},
		{"ctrl+k ctrl+left", "workbench.action.focusLeftGroup"},
		{"ctrl+k ctrl+right", "workbench.action.focusRightGroup"},
		{"ctrl+k ctrl+up", "workbench.action.focusAboveGroup"},
		{"ctrl+k ctrl+down", "workbench.action.focusBelowGroup"},
		{"ctrl+k left", "workbench.action.moveActiveEditorGroupLeft"},
		{"ctrl+k right", "workbench.action.moveActiveEditorGroupRight"},
		{"ctrl+k up", "workbench.action.moveActiveEditorGroupUp"},
		{"ctrl+k down", "workbench.action.moveActiveEditorGroupDown"},
		{"ctrl+k ctrl+shift+w", "workbench.action.closeAllGroups"},
		{"ctrl+k p", "workbench.action.files.copyPathOfActiveFile"},
		{"ctrl+k ctrl+shift+n", "notifications.showList"},
		{"ctrl+k ctrl+h", "workbench.action.output.toggleOutput"},
	} {
		kb := mustKB(t, tc.chords)
		res := resolver.Resolve(defaultContext(nil), []string{kb[0].Dispatch()}, kb[1].Dispatch())
		if res.Kind != KbFound || res.Command != tc.want {
			t.Errorf("%s: %+v", tc.chords, res)
		}
	}
}

// TestDefaultKeymapMatchesCommands checks the default keymap against the metadata of the
// commands: every bound command exists, and a keybinding only applies where the command's
// precondition holds, as VS Code builds its when-clause.
func TestDefaultKeymapMatchesCommands(t *testing.T) {
	commands := command.DefaultRegistry()
	for _, it := range DefaultRegistry().Defaults() {
		meta, ok := commands.Lookup(it.Command)
		if !ok {
			t.Errorf("%s has a key but no command", it.Command)
			continue
		}
		if meta.Precondition != nil && !WhenIsEntirelyIncluded(it.When, meta.Precondition) {
			t.Errorf("%s %v: when %v does not imply the precondition %v", it.Command, it.Chords, it.When, meta.Precondition.Serialize())
		}
	}
}

func TestDefaultKeymapHasNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, it := range DefaultRegistry().Defaults() {
		when := ""
		if it.When != nil {
			when = it.When.Serialize()
		}
		id := it.Command + "|" + it.Keybinding.UserSettingsLabel() + "|" + when
		if seen[id] {
			t.Errorf("registered twice: %s", id)
		}
		seen[id] = true
	}
}

// TestUserOverridesDefaults checks the removal and override of default keys the way a
// keybindings.json does it.
func TestUserOverridesDefaults(t *testing.T) {
	user, problems := ParseUserFile(`[
		{ "key": "ctrl+shift+k", "command": "-editor.action.deleteLines" },
		{ "key": "ctrl+shift+d", "command": "editor.action.deleteLines", "when": "editorTextFocus" },
		{ "key": "ctrl+l", "command": "-expandLineSelection" },
	]`)
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	resolver := NewResolver(DefaultRegistry().Defaults(), user, nil)
	ctx := defaultContext(nil)
	if res := resolver.Resolve(ctx, nil, dispatchOf(t, "ctrl+shift+k")); res.Kind == KbFound {
		t.Errorf("the default key must be gone: %+v", res)
	}
	if res := resolver.Resolve(ctx, nil, dispatchOf(t, "ctrl+shift+d")); res.Command != "editor.action.deleteLines" {
		t.Errorf("the user key: %+v", res)
	}
	if res := resolver.Resolve(ctx, nil, dispatchOf(t, "ctrl+l")); res.Kind == KbFound {
		t.Errorf("ctrl+l must be unbound: %+v", res)
	}
	got := resolver.LookupKeybindings("editor.action.deleteLines")
	if len(got) != 1 || got[0].IsDefault {
		t.Errorf("lookup: %+v", got)
	}
}

// TestExplorerFileKeys checks the keys of the file operations of the Explorer against the context keys
// that the folder view sets.
func TestExplorerFileKeys(t *testing.T) {
	resolver := NewResolver(DefaultRegistry().Defaults(), nil, nil)
	focus := func(extra map[string]any) contextkey.MapContext {
		ctx := contextkey.MapContext{
			"foldersViewVisible": true, "filesExplorerFocus": true, "explorerViewletFocus": true,
			"explorerResourceMoveableToTrash": true,
		}
		for k, v := range extra {
			ctx[k] = v
		}
		return ctx
	}
	cases := []struct {
		name  string
		ctx   contextkey.MapContext
		press string
		want  string
	}{
		{"F2 renames", focus(nil), "f2", "renameFile"},
		{"F2 on the root does nothing", focus(map[string]any{"explorerResourceIsRoot": true}), "f2", ""},
		{"F2 on a read-only item does nothing", focus(map[string]any{"explorerResourceReadonly": true}), "f2", ""},
		{"Delete trashes", focus(nil), "delete", "moveFileToTrash"},
		{"Delete deletes for good where the trash cannot be used", focus(map[string]any{"explorerResourceMoveableToTrash": false}), "delete", "deleteFile"},
		{"Shift+Delete deletes for good", focus(nil), "shift+delete", "deleteFile"},
		{"Ctrl+X cuts", focus(nil), "ctrl+x", "filesExplorer.cut"},
		{"Ctrl+X on the root does nothing", focus(map[string]any{"explorerResourceIsRoot": true}), "ctrl+x", ""},
		{"Ctrl+C copies", focus(nil), "ctrl+c", "filesExplorer.copy"},
		{"Ctrl+V pastes", focus(nil), "ctrl+v", "filesExplorer.paste"},
		{"Ctrl+V into a read-only folder does nothing", focus(map[string]any{"explorerResourceReadonly": true}), "ctrl+v", ""},
		{"Escape forgets a cut", focus(map[string]any{"explorerResourceCut": true}), "escape", "filesExplorer.cancelCut"},
		{"Escape with nothing cut", focus(nil), "escape", ""},
		{"an input has the keys", focus(map[string]any{"inputFocus": true}), "f2", ""},
		{"another view has the focus", contextkey.MapContext{"foldersViewVisible": true}, "delete", ""},
		{"Ctrl+Enter opens to the side", focus(nil), "ctrl+enter", "explorer.openToSide"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := resolver.Resolve(tc.ctx, nil, dispatchOf(t, tc.press))
			got := ""
			if res.Kind == KbFound {
				got = res.Command
			}
			if got != tc.want {
				t.Errorf("%s: got %q, want %q (%+v)", tc.press, got, tc.want, res)
			}
		})
	}
}

func TestQuickInputKeys(t *testing.T) {
	resolver := NewResolver(DefaultRegistry().Defaults(), nil, nil)
	cases := []struct {
		name  string
		ctx   contextkey.MapContext
		press string
		want  string
	}{
		{"Ctrl+Shift+P opens the palette", contextkey.MapContext{}, "ctrl+shift+p", "workbench.action.showCommands"},
		{"F1 opens the palette", contextkey.MapContext{}, "f1", "workbench.action.showCommands"},
		{"Ctrl+G goes to a line", contextkey.MapContext{}, "ctrl+g", "workbench.action.gotoLine"},
		{"Ctrl+P opens quick open", contextkey.MapContext{}, "ctrl+p", "workbench.action.quickOpen"},
		{"Ctrl+E opens quick open", contextkey.MapContext{}, "ctrl+e", "workbench.action.quickOpen"},
		{"Escape closes it", contextkey.MapContext{"inQuickOpen": true}, "escape", "workbench.action.closeQuickOpen"},
		{"Shift+Escape closes it", contextkey.MapContext{"inQuickOpen": true}, "shift+escape", "workbench.action.closeQuickOpen"},
		{"Escape does nothing when it is closed", contextkey.MapContext{}, "escape", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := resolver.Resolve(tc.ctx, nil, dispatchOf(t, tc.press))
			got := ""
			if res.Kind == KbFound {
				got = res.Command
			}
			if got != tc.want {
				t.Errorf("%s: got %q, want %q (%+v)", tc.press, got, tc.want, res)
			}
		})
	}
}
