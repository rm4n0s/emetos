package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/snippet"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// canonicalPhysical is the physical key of each key the dispatcher names: the lowest HID code
// of the keys that map to it (Enter, not the numpad's; the left modifiers).
var canonicalPhysical = func() map[keybinding.Key]services.PhysicalKeyboardKey {
	out := map[keybinding.Key]services.PhysicalKeyboardKey{}
	for p, k := range physicalKeys {
		if cur, ok := out[k]; !ok || p < cur {
			out[k] = p
		}
	}
	return out
}()

// namedLogical are the logical keys of the keys that are not letters or digits.
var namedLogical = map[keybinding.Key]services.LogicalKeyboardKey{
	keybinding.Backspace: services.LogicalKeyboardKeyBackspace, keybinding.Tab: services.LogicalKeyboardKeyTab,
	keybinding.Enter: services.LogicalKeyboardKeyEnter, keybinding.Escape: services.LogicalKeyboardKeyEscape,
	keybinding.Space: services.LogicalKeyboardKeySpace, keybinding.PageUp: services.LogicalKeyboardKeyPageUp,
	keybinding.PageDown: services.LogicalKeyboardKeyPageDown, keybinding.End: services.LogicalKeyboardKeyEnd,
	keybinding.Home: services.LogicalKeyboardKeyHome, keybinding.LeftArrow: services.LogicalKeyboardKeyArrowLeft,
	keybinding.UpArrow: services.LogicalKeyboardKeyArrowUp, keybinding.RightArrow: services.LogicalKeyboardKeyArrowRight,
	keybinding.DownArrow: services.LogicalKeyboardKeyArrowDown, keybinding.Insert: services.LogicalKeyboardKeyInsert,
	keybinding.Delete: services.LogicalKeyboardKeyDelete,
	keybinding.F1:     services.LogicalKeyboardKeyF1, keybinding.F2: services.LogicalKeyboardKeyF2,
	keybinding.F3: services.LogicalKeyboardKeyF3, keybinding.F4: services.LogicalKeyboardKeyF4,
	keybinding.F5: services.LogicalKeyboardKeyF5, keybinding.F6: services.LogicalKeyboardKeyF6,
	keybinding.F7: services.LogicalKeyboardKeyF7, keybinding.F8: services.LogicalKeyboardKeyF8,
	keybinding.F9: services.LogicalKeyboardKeyF9, keybinding.F10: services.LogicalKeyboardKeyF10,
	keybinding.F11: services.LogicalKeyboardKeyF11, keybinding.F12: services.LogicalKeyboardKeyF12,
	keybinding.Semicolon: ';', keybinding.Equal: '=', keybinding.Comma: ',', keybinding.Minus: '-',
	keybinding.Period: '.', keybinding.Slash: '/', keybinding.Backquote: '`', keybinding.BracketLeft: '[',
	keybinding.Backslash: '\\', keybinding.BracketRight: ']', keybinding.Quote: '\'',
}

// logicalOf is the logical key a US layout gives a key.
func logicalOf(k keybinding.Key) services.LogicalKeyboardKey {
	switch {
	case k >= keybinding.KeyA && k <= keybinding.KeyZ:
		return services.LogicalKeyboardKey('a' + rune(k-keybinding.KeyA))
	case k >= keybinding.Digit0 && k <= keybinding.Digit9:
		return services.LogicalKeyboardKey('0' + rune(k-keybinding.Digit0))
	}
	return namedLogical[k]
}

// sendChord presses the modifiers of a chord, then its key, and lets go; it reports whether
// the press of the key was handled.
func sendChord(ts *gluttertest.Tester, c keybinding.Chord) bool {
	mods := []struct {
		on  bool
		key services.LogicalKeyboardKey
	}{
		{c.Ctrl, services.LogicalKeyboardKeyControl}, {c.Shift, services.LogicalKeyboardKeyShift},
		{c.Alt, services.LogicalKeyboardKeyAlt}, {c.Meta, services.LogicalKeyboardKeyMeta},
	}
	for _, m := range mods {
		if m.on {
			ts.SendKeyDownEvent(m.key)
		}
	}
	handled := ts.SendKeyEvent(logicalOf(c.Key), gluttertest.KeyOpts{Physical: canonicalPhysical[c.Key]})
	for i := len(mods) - 1; i >= 0; i-- {
		if mods[i].on {
			ts.SendKeyUpEvent(mods[i].key)
		}
	}
	return handled
}

// keyApp is an app that shows a text in an editor that has the focus.
type keyApp struct {
	win *Workspace
	vm  *editor.ViewModel
	ts  *gluttertest.Tester
	// snippets has one snippet, with the prefix "alp", for the state with snippets before the cursor.
	snippets *snippet.Service
}

func newKeyApp(t *testing.T, text string) *keyApp {
	t.Helper()
	win := newTestWorkspace(t, "")
	path := filepath.Join(t.TempDir(), "text.txt")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := win.Documents.OpenFile(context.Background(), path, document.NewOptions())
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, doc, nil))
	ts.Pump(10 * time.Millisecond)
	vm := ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plaintext.json"), []byte(`{"a": {"prefix": "alp", "body": "ALP"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	snippets := snippet.NewService(dir)
	if err := snippets.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	vm.SetSnippets(snippets)
	return &keyApp{win: win, vm: vm, ts: ts, snippets: snippets}
}

// focusEditor gives the editor the keyboard focus, as it has when the window opens.
func (a *keyApp) focusEditor() {
	if e := a.win.shell.activeEditor(); e != nil && e.focus.Request() {
		a.ts.Pump(time.Millisecond)
	}
}

func (a *keyApp) text() string { return a.vm.Document().Text() }

func (a *keyApp) selections() []buffer.Selection { return a.vm.Cursors().Selections() }

// spyOnCommands replaces the handler of every command by one that records its id and returns
// the list of ids that ran. The handlers of the editor stay below, for the test to restore.
func (a *keyApp) spyOnCommands(t *testing.T) (ran *[]string, restore func()) {
	t.Helper()
	ran = new([]string)
	var removes []func()
	for _, meta := range a.win.Commands.All() {
		id := meta.ID
		remove, err := a.win.Commands.SetHandler(id, func(command.Context, any) error {
			*ran = append(*ran, id)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		removes = append(removes, remove)
	}
	return ran, func() {
		for _, r := range removes {
			r()
		}
	}
}

func cursorAt(line, col int) buffer.Selection {
	return buffer.NewCursor(buffer.Position{Line: line, Col: col})
}

func sel(l1, c1, l2, c2 int) buffer.Selection {
	return buffer.Selection{Anchor: buffer.Position{Line: l1, Col: c1}, Active: buffer.Position{Line: l2, Col: c2}}
}

func TestKeyOfUsesTheLogicalKeyForLettersAndTheNameOfThePhysicalKeyForTheRest(t *testing.T) {
	cases := []struct {
		name     string
		logical  services.LogicalKeyboardKey
		physical services.PhysicalKeyboardKey
		want     keybinding.Key
	}{
		{"a", 'a', services.PhysicalKeyboardKeyKeyA, keybinding.KeyA},
		{"an upper case letter", 'A', services.PhysicalKeyboardKeyKeyA, keybinding.KeyA},
		{"dvorak: the key of a is q", 'a', services.PhysicalKeyboardKeyKeyQ, keybinding.KeyA},
		{"azerty digit row gives a symbol: the physical digit", '&', services.PhysicalKeyboardKeyDigit1, keybinding.Digit1},
		{"a digit", '7', services.PhysicalKeyboardKeyDigit7, keybinding.Digit7},
		{"greek letter on the key of a", 0x3b1, services.PhysicalKeyboardKeyKeyA, keybinding.KeyA},
		{"cyrillic letter on the key of z", 0x44f, services.PhysicalKeyboardKeyKeyZ, keybinding.KeyZ},
		{"greek on the key of ]", 0x3c8, services.PhysicalKeyboardKeyBracketRight, keybinding.BracketRight},
		{"punctuation is the physical key", ']', services.PhysicalKeyboardKeyBracketRight, keybinding.BracketRight},
		{"punctuation on another layout", '*', services.PhysicalKeyboardKeyBracketRight, keybinding.BracketRight},
		{"tab", services.LogicalKeyboardKeyTab, services.PhysicalKeyboardKeyTab, keybinding.Tab},
		{"the numpad's enter is enter", services.LogicalKeyboardKeyEnter, services.PhysicalKeyboardKeyNumpadEnter, keybinding.Enter},
		{"an arrow", services.LogicalKeyboardKeyArrowLeft, services.PhysicalKeyboardKeyArrowLeft, keybinding.LeftArrow},
		{"the right control key", services.LogicalKeyboardKeyControlRight, services.PhysicalKeyboardKeyControlRight, keybinding.Ctrl},
		{"a key without a name", 0x100000001, services.PhysicalKeyboardKeyLang1, keybinding.Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := services.NewKeyDownEvent(tc.physical, tc.logical, 0)
			if got := KeyOf(e); got != tc.want {
				t.Errorf("KeyOf = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChordOfReadsTheModifiersAndIgnoresKeyUps(t *testing.T) {
	kb := services.NewHardwareKeyboard()
	press := func(logical services.LogicalKeyboardKey, physical services.PhysicalKeyboardKey) services.KeyEvent {
		e := services.NewKeyDownEvent(physical, logical, 0)
		kb.HandleKeyEvent(e)
		return e
	}
	press(services.LogicalKeyboardKeyControlLeft, services.PhysicalKeyboardKeyControlLeft)
	press(services.LogicalKeyboardKeyShiftLeft, services.PhysicalKeyboardKeyShiftLeft)
	e := press('k', services.PhysicalKeyboardKeyKeyK)
	got, ok := ChordOf(e, kb)
	if !ok || got != (keybinding.Chord{Ctrl: true, Shift: true, Key: keybinding.KeyK}) {
		t.Errorf("chord %+v, %v", got, ok)
	}
	repeat := services.NewKeyRepeatEvent(services.PhysicalKeyboardKeyKeyK, 'k', 0)
	if _, ok := ChordOf(repeat, kb); !ok {
		t.Error("a repeated key is pressed again")
	}
	up := services.NewKeyUpEvent(services.PhysicalKeyboardKeyKeyK, 'k', 0)
	if _, ok := ChordOf(up, kb); ok {
		t.Error("a key up dispatches nothing")
	}
}

// leaveFind closes the find widget, which gives the focus back to the editor.
func (a *keyApp) leaveFind() {
	if a.vm.FindRevealed() {
		if err := a.vm.RunCommand("closeFindWidget", nil); err != nil {
			panic(err)
		}
	}
	a.ts.Pump(time.Millisecond)
}

// endSnippet leaves the snippet that is being filled in and turns the tab completion of
// snippets off.
func (a *keyApp) endSnippet() {
	a.resetSuggest()
	if err := a.vm.RunCommand("leaveSnippet", nil); err != nil {
		panic(err)
	}
	a.setTabCompletion("off")
	a.setEmmetOnTab(false)
}

// resetSuggest closes the suggest widget and gives the editor the host of the window and its default selection mode back.
func (a *keyApp) resetSuggest() {
	if err := a.vm.RunCommand("hideSuggestWidget", nil); err != nil {
		panic(err)
	}
	a.vm.SetSuggest(a.win.suggestHost())
	o := a.vm.Options()
	o.Suggest.SelectionMode = "always"
	a.vm.SetOptions(o)
}

// suggestState is the state of an editor whose suggest widget shows two items that change the text, with a row focused or,
// when unfocused, with none (a widget that opened by typing and has selectionMode never).
func suggestState(a *keyApp, focused bool) func() {
	return func() {
		a.leaveFind()
		a.endSnippet()
		a.focusEditor() // the widget closes when the editor loses the focus: it must have it when the widget opens
		reg := langfeatures.NewRegistry()
		reg.RegisterCompletion("keys", langfeatures.Selector{}, keysCompletions{})
		a.vm.SetSuggest(&editor.SuggestHost{Ctx: context.Background(), Features: reg, Shared: editor.NewSuggestShared()})
		o := a.vm.Options()
		args := map[string]any{}
		if !focused {
			o.Suggest.SelectionMode = "never"
			args["auto"] = true
		}
		a.vm.SetOptions(o)
		a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 5)})
		if err := a.vm.RunCommand("editor.action.triggerSuggest", args); err != nil {
			panic(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for !a.vm.SuggestVisible() && time.Now().Before(deadline) {
			time.Sleep(2 * time.Millisecond)
			a.ts.FlushPosted()
		}
		if !a.vm.SuggestVisible() {
			panic("the suggest widget did not show")
		}
	}
}

// keysCompletions answers every request with two words that continue "gamma".
type keysCompletions struct{}

func (keysCompletions) ProvideCompletion(context.Context, langfeatures.Document, langfeatures.Position, langfeatures.CompletionTrigger) (langfeatures.CompletionList, error) {
	return langfeatures.CompletionList{Items: []langfeatures.CompletionItem{{Label: "gammaOne", Kind: 3, Detail: "one", Documentation: &langfeatures.Markup{Value: "the first"}}, {Label: "gammaTwo", Kind: 3}}}, nil
}

func (a *keyApp) setEmmetOnTab(on bool) {
	o := a.vm.Options()
	o.Emmet.TriggerExpansionOnTab = on
	a.vm.SetOptions(o)
}

func (a *keyApp) setTabCompletion(value string) {
	o := a.vm.Options()
	o.TabCompletion = value
	a.vm.SetOptions(o)
}

// openFind opens the find widget with a command of the editor (the registry's handlers are the
// spy's) and waits for its input to have the focus.
func (a *keyApp) openFind(id string) {
	if err := a.vm.RunCommand(id, nil); err != nil {
		panic(err)
	}
	a.ts.Pump(time.Millisecond)
	a.ts.Pump(time.Millisecond)
}

// states are the situations the default keybindings are tried in: each when-clause of the
// keymap holds in one of them.
func (a *keyApp) states() []func() {
	text := func(f func()) func() {
		return func() {
			a.leaveFind()
			a.endSnippet()
			f()
		}
	}
	// a snippet is being filled in, at its first tab stop or at the second one
	snippetAt := func(jumps int) func() {
		return func() {
			a.leaveFind()
			a.endSnippet()
			a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 3)})
			if err := a.vm.RunCommand("editor.action.insertSnippet", map[string]any{"snippet": "<${1:a}> ${2:b}$0"}); err != nil {
				panic(err)
			}
			for range jumps {
				if err := a.vm.RunCommand("jumpToNextSnippetPlaceholder", nil); err != nil {
					panic(err)
				}
			}
		}
	}
	return []func(){
		text(func() { a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 3)}) }),
		text(func() { a.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 2, 1, 2)}) }),
		text(func() { a.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 2, 1, 2), cursorAt(2, 1)}) }),
		text(func() {
			a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 2)})
			if err := a.vm.RunCommand("cursorColumnSelectDown", nil); err != nil {
				panic(err)
			}
		}),
		snippetAt(0),
		snippetAt(1),
		// Emmet expands with Tab
		func() {
			a.leaveFind()
			a.endSnippet()
			a.setEmmetOnTab(true)
			a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 3)})
		},
		// the text before the cursor is the prefix of a snippet
		func() {
			a.leaveFind()
			a.endSnippet()
			a.setTabCompletion("onlySnippets")
			a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(2, 0)})
			if err := a.vm.RunCommand("editor.action.insertSnippet", map[string]any{"snippet": "alp"}); err != nil {
				panic(err)
			}
		},
		// the find widget is open and its find input has the focus
		func() {
			a.leaveFind()
			a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 3)})
			a.openFind("actions.find")
		},
		// ... and its replace input
		func() {
			a.leaveFind()
			a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 3)})
			a.openFind("editor.action.startFindReplaceAction")
			a.openFind("editor.action.startFindReplaceAction") // from the find input, it goes to the replace input
		},
		// the hover of a problem that a command opened is showing (step 12.4), for the key that closes it
		func() {
			a.leaveFind()
			a.endSnippet()
			a.vm.SetDiagnostics([]editor.Diagnostic{{
				Range: buffer.Range{Start: buffer.Position{Line: 1, Col: 0}, End: buffer.Position{Line: 1, Col: 5}}, Severity: editor.DiagnosticError, Message: "a problem",
			}})
			a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 1)})
			a.vm.ShowHoverAt(buffer.Position{Line: 1, Col: 1})
		},
	}
}

// pinnedState is the state of an editor whose tab is pinned, where the key of pin unpins.
func pinnedState(a *keyApp) func() {
	return func() {
		a.leaveFind()
		a.win.Keys.Set("activeEditorIsPinned", true)
	}
}

// unfocusedState is the state of a window in which no editor has the focus, for the keys that are not
// for the editor (the paths of a file: Ctrl+Alt+C).
func unfocusedState(a *keyApp) func() {
	return func() {
		a.leaveFind()
		if focus := a.ts.Binding.FocusManager().PrimaryFocus(); focus != nil {
			focus.Unfocus()
		}
	}
}

// zenModeState is the state of a window in zen mode, for the keys that leave it (Escape Escape).
func zenModeState(a *keyApp) func() {
	return func() {
		a.leaveFind()
		a.win.Keys.Set("inZenMode", true)
	}
}

// diffEditorState is the state of a window that shows a diff editor, for the keys that go to its changes (step 9.5).
func diffEditorState(a *keyApp) func() {
	return func() {
		a.leaveFind()
		a.win.Keys.Set("textCompareEditorVisible", true)
	}
}

// mergeEditorState is the state of a window that shows a merge editor, for the key that completes the merge (step 9.8).
func mergeEditorState(a *keyApp) func() {
	return func() {
		a.leaveFind()
		a.win.Keys.Set("isMergeEditor", true)
	}
}

// groupsState is the state of a window with several groups, the active one empty, for the keys that act
// on groups.
func groupsState(a *keyApp) func() {
	return func() {
		a.leaveFind()
		a.win.Keys.Set("activeEditorGroupEmpty", true)
		a.win.Keys.Set("multipleEditorGroups", true)
		a.win.Keys.Set("editorPartMultipleEditorGroups", true)
	}
}

// notificationsState is the state of a window with the notification center open, toasts showing, or a
// toast with the focus, for the keys of the notifications.
func notificationsState(a *keyApp, center, toasts, focus bool) func() {
	return func() {
		a.leaveFind()
		a.win.Keys.Set("notificationCenterVisible", center)
		a.win.Keys.Set("notificationToastsVisible", toasts)
		a.win.Keys.Set("notificationFocus", focus)
	}
}

// problemsState is the state of a window in which a view of the panel has the focus (the one the id
// names, or none) and the filter box of the Problems has it or not, for the keys of the Problems.
func problemsState(a *keyApp, view string, filter bool) func() {
	return func() {
		a.leaveFind()
		if view != "" {
			a.win.Keys.Set("focusedView", view)
		}
		a.win.Keys.Set("problemsFilterFocus", filter)
	}
}

// folderState is the state of a window that has a folder open, for the keys that need one.
func folderState(a *keyApp) func() {
	return func() { a.win.Keys.Set("workbenchState", "folder") }
}

// explorerKeys are the context keys the keys of the file operations of the Explorer look at.
var explorerKeys = []string{"foldersViewVisible", "filesExplorerFocus", "explorerViewletFocus", "inputFocus",
	"explorerResourceMoveableToTrash", "explorerResourceCut", "explorerResourceIsRoot", "explorerResourceReadonly", "explorerResourceIsFolder"}

// filesExplorerState is the state of a window in which the folder view has the focus, on an item that can be moved
// to the trash or not, with a cut item or not, for the keys of the file operations.
func filesExplorerState(a *keyApp, trash, cut bool) func() {
	return func() {
		unfocusedState(a)()
		a.win.Keys.Batch(func() {
			a.win.Keys.Set("foldersViewVisible", true)
			a.win.Keys.Set("filesExplorerFocus", true)
			a.win.Keys.Set("explorerViewletFocus", true)
			a.win.Keys.Set("explorerResourceMoveableToTrash", trash)
			a.win.Keys.Set("explorerResourceCut", cut)
			a.win.Keys.Set("explorerResourceIsFolder", true)
		})
	}
}

// quickInputState is the state of a window whose quick input has the focus, for the keys that close it.
func quickInputState(a *keyApp) func() {
	return func() {
		unfocusedState(a)()
		a.win.Keys.Set("inQuickOpen", true)
	}
}

// historyState is the state of a window that can go back and forward in its history of locations.
func historyState(a *keyApp) func() {
	return func() {
		a.leaveFind()
		a.win.Keys.Set("canNavigateBack", true)
		a.win.Keys.Set("canNavigateForward", true)
	}
}

// editorPickerState is the state of a window whose quick input shows the open editors, for the keys that move
// in the picker.
func editorPickerState(a *keyApp) func() {
	return func() {
		quickInputState(a)()
		a.win.Keys.Set("inEditorsPicker", true)
	}
}

// keybindingsKeys are the context keys the keys of the keybindings editor look at.
var keybindingsKeys = []string{"inKeybindings", "inKeybindingsSearch", "keybindingsSearchHasValue", "keybindingFocus", "whenFocus"}

// keybindingsEditorState is the state of a window whose keybindings editor has the focus: in the search box (with
// a search or without) or on a row of the table.
func keybindingsEditorState(a *keyApp, search, hasValue bool) func() {
	return func() {
		unfocusedState(a)()
		a.win.Keys.Batch(func() {
			a.win.Keys.Set("inKeybindings", true)
			a.win.Keys.Set("inKeybindingsSearch", search)
			a.win.Keys.Set("keybindingsSearchHasValue", hasValue)
			a.win.Keys.Set("keybindingFocus", !search)
		})
	}
}

func TestEveryDefaultKeybindingRunsItsCommand(t *testing.T) {
	a := newKeyApp(t, "alpha beta\ngamma delta\nepsilon zeta\n")
	ran, restore := a.spyOnCommands(t)
	defer func() { restore() }()
	var tried int
	for _, item := range a.win.Process.Keymap.Defaults() {
		if item.Command == "" || strings.HasPrefix(item.Command, "-") || len(item.Keybinding) == 0 {
			continue
		}
		tried++
		t.Run(item.Command+" "+item.Keybinding.UserSettingsLabel(), func(t *testing.T) {
			// a command tried before may have left the editor without the focus
			a.focusEditor()
			a.resetSuggest()
			var reached []string
			states := append(a.states(), pinnedState(a), groupsState(a), unfocusedState(a),
				notificationsState(a, true, false, false), notificationsState(a, false, true, false), notificationsState(a, false, true, true),
				problemsState(a, command.ViewMarkers, false), problemsState(a, "", true), folderState(a),
				filesExplorerState(a, true, false), filesExplorerState(a, false, false), filesExplorerState(a, true, true),
				quickInputState(a), historyState(a), editorPickerState(a), suggestState(a, true), suggestState(a, false),
				codeActionsState(a, ""), codeActionsState(a, "list"), codeActionsState(a, "rename"), codeActionsState(a, "preview"),
				hoverState(a, false, false), hoverState(a, true, false), hoverState(a, true, true), signatureState(a, 1), signatureState(a, 3), navigationState(a), peekState(a),
				keybindingsEditorState(a, true, true), keybindingsEditorState(a, true, false), keybindingsEditorState(a, false, false),
				zenModeState(a), diffEditorState(a), mergeEditorState(a))
			for _, state := range states {
				notificationsState(a, false, false, false)()
				for _, key := range explorerKeys {
					a.win.Keys.Reset(key)
				}
				for _, key := range keybindingsKeys {
					a.win.Keys.Reset(key)
				}
				a.vm.HideHover() // only the state of the hover of a problem shows one
				a.vm.SignatureCancel()
				a.vm.PeekClose()
				a.vm.CodeActionHide()
				a.vm.RenameCancel()
				a.vm.PreviewDiscard()
				a.win.Keys.Set("inZenMode", false)
				a.win.Keys.Set("textCompareEditorVisible", false)
				a.win.Keys.Reset("isMergeEditor")
				a.win.Keys.Set("activeEditorIsPinned", false)
				a.win.Keys.Set("activeEditorGroupEmpty", false)
				a.win.Keys.Set("multipleEditorGroups", false)
				a.win.Keys.Set("editorPartMultipleEditorGroups", false)
				a.win.Keys.Reset("focusedView")
				a.win.Keys.Reset("inQuickOpen")
				a.win.Keys.Reset("inEditorsPicker")
				a.win.Keys.Set("canNavigateBack", false)
				a.win.Keys.Set("canNavigateForward", false)
				a.win.Keys.Set("problemsFilterFocus", false)
				a.win.Keys.Set("workbenchState", "empty")
				state()
				a.vm.SelectionsChanged()
				a.ts.Pump(time.Millisecond)
				// A state that moves the focus into the find widget makes the editor bind its commands
				// again, above the spies: the spies go on top again.
				restore()
				ran, restore = a.spyOnCommands(t)
				*ran = nil
				for _, chord := range item.Keybinding {
					sendChord(a.ts, chord)
				}
				reached = append(reached, *ran...)
				if slices.Contains(*ran, item.Command) {
					return
				}
			}
			t.Errorf("the keys never ran the command; they ran %v", reached)
		})
	}
	if tried < 60 {
		t.Errorf("only %d default keybindings were tried", tried)
	}
}

func TestEditorCommandsRunFromTheirKeys(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		sels  []buffer.Selection
		chord string
		want  string
		wantS []buffer.Selection
	}{
		{"typing a tab", "ab", []buffer.Selection{cursorAt(0, 1)}, "tab", "a   b", []buffer.Selection{cursorAt(0, 4)}},
		{"backspace", "abc", []buffer.Selection{cursorAt(0, 2)}, "backspace", "ac", []buffer.Selection{cursorAt(0, 1)}},
		{"delete", "abc", []buffer.Selection{cursorAt(0, 1)}, "delete", "ac", []buffer.Selection{cursorAt(0, 1)}},
		{"ctrl+a selects all", "ab\ncd", []buffer.Selection{cursorAt(0, 0)}, "ctrl+a", "ab\ncd", []buffer.Selection{sel(0, 0, 1, 2)}},
		{"left", "abc", []buffer.Selection{cursorAt(0, 2)}, "left", "abc", []buffer.Selection{cursorAt(0, 1)}},
		{"shift+right selects", "abc", []buffer.Selection{cursorAt(0, 1)}, "shift+right", "abc", []buffer.Selection{sel(0, 1, 0, 2)}},
		{"down", "ab\ncd", []buffer.Selection{cursorAt(0, 1)}, "down", "ab\ncd", []buffer.Selection{cursorAt(1, 1)}},
		{"end", "abc\nd", []buffer.Selection{cursorAt(0, 1)}, "end", "abc\nd", []buffer.Selection{cursorAt(0, 3)}},
		{"ctrl+end", "abc\nde", []buffer.Selection{cursorAt(0, 1)}, "ctrl+end", "abc\nde", []buffer.Selection{cursorAt(1, 2)}},
		{"ctrl+shift+k deletes the line", "a\nb\nc", []buffer.Selection{cursorAt(1, 0)}, "ctrl+shift+k", "a\nc", []buffer.Selection{cursorAt(1, 0)}},
		{"alt+down moves the line", "a\nb\nc", []buffer.Selection{cursorAt(0, 0)}, "alt+down", "b\na\nc", []buffer.Selection{cursorAt(1, 0)}},
		{"ctrl+] indents", "a", []buffer.Selection{cursorAt(0, 0)}, "ctrl+]", "    a", []buffer.Selection{cursorAt(0, 4)}},
		{"ctrl+enter inserts a line below", "a\nb", []buffer.Selection{cursorAt(0, 0)}, "ctrl+enter", "a\n\nb", []buffer.Selection{cursorAt(1, 0)}},
		{"shift+alt+down adds a cursor", "ab\ncd", []buffer.Selection{cursorAt(0, 1)}, "shift+alt+down", "ab\ncd", []buffer.Selection{cursorAt(0, 1), cursorAt(1, 1)}},
		{"escape with a selection", "abc", []buffer.Selection{sel(0, 0, 0, 2)}, "escape", "abc", []buffer.Selection{cursorAt(0, 2)}},
		{"escape with several cursors", "abc", []buffer.Selection{cursorAt(0, 0), cursorAt(0, 2)}, "escape", "abc", []buffer.Selection{cursorAt(0, 0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newKeyApp(t, tc.text)
			a.vm.Cursors().SetSelections(tc.sels)
			a.vm.SelectionsChanged()
			a.ts.Pump(time.Millisecond)
			chord, err := keybinding.ParseKeybinding(tc.chord)
			if err != nil {
				t.Fatal(err)
			}
			if !sendChord(a.ts, chord[0]) {
				t.Error("the key was not handled")
			}
			if a.text() != tc.want || !buffer.SelectionsEqual(a.selections(), tc.wantS) {
				t.Errorf("text %q selections %v, want %q %v", a.text(), a.selections(), tc.want, tc.wantS)
			}
		})
	}
}

func TestChordsAndModifierKeys(t *testing.T) {
	a := newKeyApp(t, "alpha\nbeta\n")
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 2)})
	var statuses []string
	a.win.OnChordStatus = func(status string, cleared bool) {
		statuses = append(statuses, status+"|"+map[bool]string{true: "cleared", false: ""}[cleared])
	}
	ran, restore := a.spyOnCommands(t)
	defer restore()

	// ctrl+k ctrl+x is trim trailing whitespace: the first chord is held back.
	if !sendChord(a.ts, keybinding.Chord{Ctrl: true, Key: keybinding.KeyK}) {
		t.Error("the first key of a chord is held")
	}
	if !a.win.Dispatcher.InChordMode() || len(*ran) != 0 {
		t.Fatalf("pending: %v, ran %v", a.win.Dispatcher.InChordMode(), *ran)
	}
	// A modifier alone does not cancel the chord: press and release Shift, then Meta. (Alt is not
	// used: a tap of Alt on its own gives the focus to the menu bar, as it does in VS Code, and the
	// editor does not have its commands then.)
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyShift)
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyMeta)
	if !a.win.Dispatcher.InChordMode() {
		t.Fatal("a modifier key alone cancelled the chord")
	}
	sendChord(a.ts, keybinding.Chord{Ctrl: true, Key: keybinding.KeyX})
	if !slices.Equal(*ran, []string{"editor.action.trimTrailingWhitespace"}) {
		t.Errorf("ran %v", *ran)
	}
	if a.win.Dispatcher.InChordMode() {
		t.Error("the chord is done")
	}
	if len(statuses) != 2 || !strings.Contains(statuses[0], "Waiting for second key of chord") || !strings.HasSuffix(statuses[1], "|cleared") {
		t.Errorf("statuses %q", statuses)
	}
	// A key that does not complete a chord is dropped with a message and not typed.
	*ran = nil
	sendChord(a.ts, keybinding.Chord{Ctrl: true, Key: keybinding.KeyK})
	if !sendChord(a.ts, keybinding.Chord{Key: keybinding.KeyY}) { // Ctrl+K Z is zen mode since step 8.5
		t.Error("the key that ends a failed chord is used up")
	}
	if len(*ran) != 0 || a.win.Dispatcher.InChordMode() {
		t.Errorf("ran %v, pending %v", *ran, a.win.Dispatcher.InChordMode())
	}
	if last := statuses[len(statuses)-1]; !strings.Contains(last, "is not a command") {
		t.Errorf("last status %q", last)
	}
}

func TestKeysOnAnotherLayout(t *testing.T) {
	a := newKeyApp(t, "x")
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 0)})
	// ctrl+] indents: on a Greek layout the key of ] gives ψ, and the shortcut still works.
	a.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	handled := a.ts.SendKeyEvent(services.LogicalKeyboardKey(0x3c8), gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyBracketRight, Character: "ψ"})
	a.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	if !handled || a.text() != "    x" {
		t.Errorf("handled %v, text %q", handled, a.text())
	}
	// ctrl+a on a layout without Latin letters: the key of a gives α.
	a.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	handled = a.ts.SendKeyEvent(services.LogicalKeyboardKey(0x3b1), gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyKeyA, Character: "α"})
	a.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	if !handled || !buffer.SelectionsEqual(a.selections(), []buffer.Selection{sel(0, 0, 0, 5)}) {
		t.Errorf("handled %v, selections %v", handled, a.selections())
	}
}

func TestUnboundKeysAreLeftForTheTextInput(t *testing.T) {
	a := newKeyApp(t, "abc")
	ran, restore := a.spyOnCommands(t)
	defer restore()
	for _, tc := range []struct {
		name    string
		logical services.LogicalKeyboardKey
		phys    services.PhysicalKeyboardKey
		char    string
	}{
		{"a letter", 'x', services.PhysicalKeyboardKeyKeyX, "x"},
		{"a digit", '5', services.PhysicalKeyboardKeyDigit5, "5"},
		{"a symbol", '$', services.PhysicalKeyboardKeyDigit4, "$"},
		{"a greek letter", 0x3b2, services.PhysicalKeyboardKeyKeyB, "β"},
		{"space", services.LogicalKeyboardKeySpace, services.PhysicalKeyboardKeySpace, " "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// (SendKeyEvent says whether anything used the key: the text input does, by typing it.)
			a.ts.SendKeyEvent(tc.logical, gluttertest.KeyOpts{Physical: tc.phys, Character: tc.char})
			if len(*ran) != 0 {
				t.Errorf("the dispatcher ran %v for an unbound key", *ran)
			}
			if got := a.win.Dispatcher.InChordMode(); got {
				t.Error("an unbound key started a chord")
			}
		})
	}
	// The editor's text input typed them, in order, at the cursor.
	if a.text() != "x5$β abc" {
		t.Errorf("the keys were not typed: %q", a.text())
	}
	// A key with a binding is not left for the text input.
	if !a.ts.SendKeyEvent(services.LogicalKeyboardKeyArrowRight, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyArrowRight}) {
		t.Error("an arrow is bound")
	}
}

func TestBoundKeysBeatFocusTraversal(t *testing.T) {
	a := newKeyApp(t, "ab")
	editorNode := a.ts.Binding.FocusManager().PrimaryFocus()
	if editorNode == nil {
		t.Fatal("the editor has the focus")
	}
	if !a.ts.SendKeyEvent(services.LogicalKeyboardKeyTab, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyTab}) {
		t.Error("tab is bound in the editor")
	}
	if got := a.ts.Binding.FocusManager().PrimaryFocus(); got != editorNode {
		t.Error("the focus moved: traversal saw the tab")
	}
	if a.text() != "    ab" {
		t.Errorf("text %q", a.text())
	}
}

func TestKeysAreResolvedInTheContextOfTheFocus(t *testing.T) {
	a := newKeyApp(t, "ab")
	manager := a.ts.Binding.FocusManager()
	editorNode := manager.PrimaryFocus()
	// Take the focus away from the editor: its keys are not its any more.
	manager.PrimaryFocus().Unfocus()
	a.ts.Pump(time.Millisecond)
	if manager.PrimaryFocus() == editorNode {
		t.Fatal("the editor still has the focus")
	}
	// (An arrow key would be handled by Glutter's own focus traversal: use Ctrl+Shift+K,
	// which deletes the line in the editor and means nothing elsewhere.)
	if sendChord(a.ts, keybinding.Chord{Ctrl: true, Shift: true, Key: keybinding.KeyK}) {
		t.Error("an editor key was handled while the focus is elsewhere")
	}
	if a.text() != "ab" {
		t.Errorf("the line was deleted: %q", a.text())
	}
	editorNode.RequestFocus()
	a.ts.Pump(time.Millisecond)
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyArrowRight, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyArrowRight})
	if !buffer.SelectionsEqual(a.selections(), []buffer.Selection{cursorAt(0, 1)}) {
		t.Errorf("with the focus back: %v", a.selections())
	}
}

func TestContextKeysOfTheEditor(t *testing.T) {
	a := newKeyApp(t, "one\ntwo\nthree")
	scope := a.win.Scopes
	_ = scope
	// The window's root scope has no editor keys; the editor's own scope has them.
	if a.win.Keys.Get("editorTextFocus") != nil {
		t.Error("the root scope has the editor's keys")
	}
	manager := a.ts.Binding.FocusManager()
	keys := a.win.Scopes.ScopeOf(manager.PrimaryFocus(), a.win.Keys)
	if keys == a.win.Keys {
		t.Fatal("the focused editor has no scope of its own")
	}
	get := func(key string) any { return keys.Get(key) }
	if get("editorTextFocus") != true || get("editorFocus") != true || get("textInputFocus") != true {
		t.Errorf("focus keys: %v %v %v", get("editorTextFocus"), get("editorFocus"), get("textInputFocus"))
	}
	if get("editorHasSelection") != false || get("editorHasMultipleSelections") != false || get("editorReadonly") != false {
		t.Errorf("selection keys: %v %v %v", get("editorHasSelection"), get("editorHasMultipleSelections"), get("editorReadonly"))
	}
	if get("editorLangId") != a.vm.Document().LanguageID() {
		t.Errorf("language %v", get("editorLangId"))
	}
	a.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 0, 0, 2), cursorAt(1, 1)})
	a.vm.SelectionsChanged()
	if get("editorHasSelection") != true || get("editorHasMultipleSelections") != true {
		t.Errorf("after selecting: %v %v", get("editorHasSelection"), get("editorHasMultipleSelections"))
	}
	if err := a.vm.RunCommand("cursorColumnSelectDown", nil); err != nil {
		t.Fatal(err)
	}
	if get("editorColumnSelection") != true {
		t.Error("a column selection is on")
	}
	a.vm.Document().SetReadOnly(true)
	a.vm.SelectionsChanged()
	if get("editorReadonly") != true {
		t.Error("the document is read only")
	}
	manager.PrimaryFocus().Unfocus()
	a.ts.Pump(time.Millisecond)
	if get("editorTextFocus") != false {
		t.Error("the editor lost the focus")
	}
}

func TestReadOnlyDocumentKeepsItsText(t *testing.T) {
	a := newKeyApp(t, "abc")
	a.vm.Document().SetReadOnly(true)
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 1)})
	a.vm.SelectionsChanged()
	a.ts.Pump(time.Millisecond)
	a.ts.SendKeyEvent(services.LogicalKeyboardKeyBackspace, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyBackspace})
	if a.text() != "abc" {
		t.Errorf("a read-only document changed: %q", a.text())
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestEditorsHandOverTheirCommandsWithTheFocus(t *testing.T) {
	a := newKeyApp(t, "ab")
	reg := a.win.Commands
	if !reg.HasHandler("cursorLeft") {
		t.Fatal("the focused editor handles the commands")
	}
	manager := a.ts.Binding.FocusManager()
	manager.PrimaryFocus().Unfocus()
	a.ts.Pump(time.Millisecond)
	if reg.HasHandler("cursorLeft") {
		t.Error("an editor without the focus keeps its handlers")
	}
	ids := editor.CommandIDs()
	sort.Strings(ids)
	if len(ids) < 100 {
		t.Errorf("%d commands", len(ids))
	}
	_ = ui.Size{}
}

func TestTypingThroughTheApp(t *testing.T) {
	a := newKeyApp(t, "")
	typeKey := func(logical services.LogicalKeyboardKey, physical services.PhysicalKeyboardKey, char string) bool {
		return a.ts.SendKeyEvent(logical, gluttertest.KeyOpts{Physical: physical, Character: char})
	}
	for _, k := range []struct {
		logical  services.LogicalKeyboardKey
		physical services.PhysicalKeyboardKey
		char     string
	}{
		{'f', services.PhysicalKeyboardKeyKeyF, "f"},
		{'o', services.PhysicalKeyboardKeyKeyO, "o"},
		{'o', services.PhysicalKeyboardKeyKeyO, "o"},
	} {
		typeKey(k.logical, k.physical, k.char)
	}
	if a.text() != "foo" {
		t.Fatalf("text %q", a.text())
	}
	// Enter is typed as a new line.
	typeKey(services.LogicalKeyboardKeyEnter, services.PhysicalKeyboardKeyEnter, "")
	typeKey('b', services.PhysicalKeyboardKeyKeyB, "b")
	if a.text() != "foo\nb" {
		t.Errorf("after Enter: %q", a.text())
	}
	// Tab is a command of the editor, and a Control key is not typed.
	if !typeKey(services.LogicalKeyboardKeyTab, services.PhysicalKeyboardKeyTab, "\t") {
		t.Error("tab was not used")
	}
	if a.text() != "foo\nb   " {
		t.Errorf("after Tab: %q", a.text())
	}
	a.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	typeKey('z', services.PhysicalKeyboardKeyKeyZ, "z") // undo
	a.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	if strings.Contains(a.text(), "z") {
		t.Errorf("ctrl+z typed a z: %q", a.text())
	}
	// Backspace is a command too.
	typeKey(services.LogicalKeyboardKeyBackspace, services.PhysicalKeyboardKeyBackspace, "")
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestClipboardKeys(t *testing.T) {
	a := newKeyApp(t, "alpha beta\ngamma\n")
	press := func(chord string) {
		t.Helper()
		parsed, err := keybinding.ParseKeybinding(chord)
		if err != nil {
			t.Fatal(err)
		}
		if !sendChord(a.ts, parsed[0]) {
			t.Errorf("%s was not handled", chord)
		}
		a.ts.Pump(time.Millisecond)
	}
	a.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 0, 0, 5)})
	a.vm.SelectionsChanged()
	a.ts.Pump(time.Millisecond)

	press("ctrl+c")
	if got, ok := a.ts.ClipboardText(); !ok || got != "alpha" {
		t.Fatalf("ctrl+c put %q on the clipboard", got)
	}
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 5)})
	a.vm.SelectionsChanged()
	press("ctrl+v")
	if a.text() != "alpha beta\ngammaalpha\n" {
		t.Errorf("ctrl+v: %q", a.text())
	}
	a.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 5, 0, 10)})
	a.vm.SelectionsChanged()
	press("ctrl+x")
	if got, _ := a.ts.ClipboardText(); got != " beta" || a.text() != "alpha\ngammaalpha\n" {
		t.Errorf("ctrl+x: clipboard %q, text %q", got, a.text())
	}
	a.ts.SetClipboardText("Z")
	press("shift+insert")
	if a.text() != "alphaZ\ngammaalpha\n" {
		t.Errorf("shift+insert: %q", a.text())
	}
}

func TestTabExpandsAnEmmetAbbreviationAndReportsAFailure(t *testing.T) {
	a := newKeyApp(t, "ul>li\n")
	a.vm.Document().SetLanguageID("html")
	o := a.vm.Options()
	o.Emmet.TriggerExpansionOnTab = true
	o.Emmet.IncludeLanguages = map[string]any{}
	o.Emmet.ExcludeLanguages = []any{}
	o.Emmet.Variables = map[string]any{}
	a.vm.SetOptions(o)
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 5)})
	a.vm.SelectionsChanged()
	a.ts.Pump(time.Millisecond)
	tab := keybinding.Chord{Key: keybinding.Tab}
	sendChord(a.ts, tab)
	if got := a.text(); !strings.HasPrefix(got, "<ul>") {
		t.Fatalf("Tab did not expand: %q", got)
	}

	// a failing expansion is reported and the key is a plain Tab
	a.vm.Document().SetText("a")
	o.Emmet.Variables = []any{1}
	a.vm.SetOptions(o)
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 1)})
	a.vm.SelectionsChanged()
	a.ts.Pump(time.Millisecond)
	sendChord(a.ts, tab)
	var reported bool
	for _, it := range a.win.Notify.Items() {
		if strings.Contains(it.Message, "Failed to expand abbreviation") {
			reported = true
		}
	}
	if !reported {
		t.Error("the failure was not shown as a notification")
	}
}
