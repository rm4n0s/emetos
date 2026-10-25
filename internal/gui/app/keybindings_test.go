package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/keybindingsui"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The tests of the keybindings editor in the window (step 8.3): the editor input and its commands, what the
// editor writes to keybindings.json, the rule of a keybindings file that is open in a text editor, and the keys,
// which go through the real dispatcher.

// newKeybindingsApp is a window (over a folder, none when empty) whose keybindings.json has the text, with a.txt
// open.
func newKeybindingsApp(t *testing.T, folder, userText string) *groupApp {
	t.Helper()
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"editor.fontSize": 14, "files.hotExit": "off", "window.menuBarVisibility": "classic"})
	if userText != "" {
		path, err := keybinding.UserKeybindingsPath(paths, storage.DefaultProfile)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(userText), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	win, err := NewWorkspace(context.Background(), newTestProcess(ProcessOptions{Paths: paths}), folder)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &groupApp{t: t, win: win, files: map[string]string{"a.txt": path}, docs: map[string]*document.Document{}}
	doc := a.doc("a.txt")
	a.ts = testutil.New(t, testWindowSize)
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a.ts.PumpWidget(newAppDocs(context.Background(), win, []*document.Document{doc}, nil, ""))
	a.pump()
	return a
}

// pressUnbound sends keys that nothing is bound to and checks that the dispatcher did not use them.
func (a *groupApp) pressUnbound(keys string) {
	a.t.Helper()
	kb, err := keybinding.ParseKeybinding(keys)
	if err != nil {
		a.t.Fatal(err)
	}
	for i, chord := range kb {
		if sendChord(a.ts, chord) && i == len(kb)-1 {
			a.t.Errorf("the keys %s were used", keys)
		}
	}
	a.pump()
}

func (a *groupApp) keybindingsFile() string {
	a.t.Helper()
	return a.win.userKeys.path
}

// keybindingsInputs are the keybindings inputs of all the groups.
func (a *groupApp) keybindingsInputs() []*editorgroup.KeybindingsInput {
	var out []*editorgroup.KeybindingsInput
	for _, g := range a.shell().group.grid.Groups(editorgroup.GridAppearance) {
		for _, e := range g.Model.Editors(editorgroup.Sequential, false) {
			if in, ok := e.(*editorgroup.KeybindingsInput); ok {
				out = append(out, in)
			}
		}
	}
	return out
}

// keysPane is the pane of the keybindings editor.
func (a *groupApp) keysPane() *keybindingsPane {
	a.t.Helper()
	p := a.shell().keybindingsPane()
	if p == nil {
		a.t.Fatal("no keybindings editor")
	}
	return p
}

func (a *groupApp) keysRow(command, key string) gluttertest.Finder {
	a.t.Helper()
	for _, r := range a.win.KeybindingsModel().Rows() {
		if r.Command == command && (key == "" || r.Key == key) {
			return gluttertest.ByKey(keybindingsui.RowKey(r.ID))
		}
	}
	a.t.Fatalf("no row %s %s", command, key)
	return nil
}

func (a *groupApp) keysRowExists(command, key string) bool {
	for _, r := range a.win.KeybindingsModel().Rows() {
		if r.Command == command && r.Key == key {
			return a.ts.Exists(gluttertest.ByKey(keybindingsui.RowKey(r.ID)))
		}
	}
	return false
}

// tapKeysRow selects a row of the editor with a click.
func (a *groupApp) tapKeysRow(command, key string) {
	a.t.Helper()
	a.ts.TapAt(a.ts.Center(a.keysRow(command, key)), mouseButton)
	a.pump()
}

// fileEntries are the entries of keybindings.json as "key|command|when".
func (a *groupApp) fileEntries() []string {
	a.t.Helper()
	if _, err := os.Stat(a.keybindingsFile()); err != nil {
		return nil
	}
	items, problems := keybinding.ParseUserFile(readFile(a.t, a.keybindingsFile()))
	if len(problems) != 0 {
		a.t.Fatalf("problems %v", problems)
	}
	var out []string
	for _, it := range items {
		when := ""
		if it.When != nil {
			when = it.When.Serialize()
		}
		cmd := it.Command
		if it.Bubble {
			cmd = "^" + cmd
		}
		out = append(out, it.Keybinding.UserSettingsLabel()+"|"+cmd+"|"+when)
	}
	return out
}

// searchKeybindings filters the keybindings editor by a text.
func (a *groupApp) searchKeybindings(query string) {
	a.t.Helper()
	a.run2("workbench.action.openGlobalKeybindings", query)
}

func TestCtrlKCtrlSOpensTheKeybindingsEditor(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.press("ctrl+k ctrl+s")
	wantNames(t, a.tabNames(), "a.txt", "Keyboard Shortcuts")
	if got := a.active(); got != "Keyboard Shortcuts" {
		t.Errorf("active tab %q", got)
	}
	if got := len(a.keybindingsInputs()); got != 1 {
		t.Fatalf("%d keybindings inputs", got)
	}
	if !a.ts.Exists(gluttertest.ByType[*keybindingsui.Editor]()) {
		t.Error("the editor is not shown")
	}
	item := a.item("Keyboard Shortcuts")
	if item.Dirty || item.Preview || item.Icon == nil {
		t.Errorf("tab %+v: dirty, preview or without the icon", item)
	}
	if v := a.win.Keys.Get("activeEditor"); v != editorgroup.KeybindingsInputTypeID {
		t.Errorf("activeEditor %v", v)
	}
	// the search box has the focus
	if !a.keysPane().hasFocus() {
		t.Error("the keybindings editor has not the focus")
	}
	// the keys again reveal it, they do not make another
	a.clickTab("a.txt")
	a.press("ctrl+k ctrl+s")
	wantNames(t, a.tabNames(), "a.txt", "Keyboard Shortcuts")
	if got := a.active(); got != "Keyboard Shortcuts" {
		t.Errorf("active tab %q after the second Ctrl+K Ctrl+S", got)
	}
	// closing it asks nothing
	a.run("workbench.action.closeActiveEditor")
	wantNames(t, a.tabNames(), "a.txt")
	if len(a.keybindingsInputs()) != 0 || len(a.shell().group.panes) != 0 {
		t.Error("the input or its pane is left")
	}
	if a.win.Dispatcher.Recording() {
		t.Error("the dispatcher records")
	}
	a.noErrors()
}

func TestTheKeybindingsCommandTakesTheSearchText(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.run2("workbench.action.openGlobalKeybindings", "copy line")
	if got := a.keybindingsInputs()[0].ViewState().Query; got != "copy line" {
		t.Errorf("query %q", got)
	}
	if !a.keysRowExists("editor.action.copyLinesDownAction", "ctrl+shift+alt+down") || a.keysRowExists("workbench.action.openSettings", "ctrl+,") {
		t.Error("the table is not filtered")
	}
	a.run2("workbench.action.openGlobalKeybindings", map[string]any{"query": "settings"})
	if got := len(a.keybindingsInputs()); got != 1 {
		t.Errorf("%d inputs", got)
	}
	if got := a.keysPane().ctl.Query(); got != "settings" {
		t.Errorf("query %q", got)
	}
	// an argument that is neither keeps the search
	a.run2("workbench.action.openGlobalKeybindings", 42)
	if got := a.keysPane().ctl.Query(); got != "settings" {
		t.Errorf("query %q", got)
	}
	a.noErrors()
}

func TestTheKeybindingsEditorIsOnlyInOneGroup(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.run("workbench.action.openGlobalKeybindings")
	a.run("workbench.action.splitEditorRight")
	if got := len(a.keybindingsInputs()); got != 1 {
		t.Errorf("%d inputs after a split", got)
	}
	a.run("workbench.action.openGlobalKeybindings")
	if got := len(a.keybindingsInputs()); got != 1 {
		t.Errorf("%d inputs after opening again", got)
	}
	a.noErrors()
}

func TestKeybindingsInTheManageAndTheFileMenus(t *testing.T) {
	t.Run("the Manage menu", func(t *testing.T) {
		a := newKeybindingsApp(t, "", "")
		a.run2("dev.openMenu", map[string]any{"id": "GlobalActivity"})
		rows := menuRows(a.openMenu())
		found := false
		for _, r := range rows {
			if r == "Keyboard Shortcuts" {
				found = true
			}
		}
		if !found {
			t.Fatalf("rows %v", rows)
		}
		for _, it := range a.openMenu() {
			if kit.MenuLabelText(it.Label) == "Keyboard Shortcuts" && it.Keybinding != "Ctrl+K Ctrl+S" {
				t.Errorf("the key of the row %q", it.Keybinding)
			}
		}
		a.ts.Tap(gluttertest.ByText("Keyboard Shortcuts"))
		a.pump()
		wantNames(t, a.tabNames(), "a.txt", "Keyboard Shortcuts")
		a.noErrors()
	})
	t.Run("the Preferences submenu of the File menu", func(t *testing.T) {
		a := newKeybindingsApp(t, "", "")
		a.run2("dev.openMenu", map[string]any{"id": "MenubarFileMenu"})
		a.ts.Tap(gluttertest.ByText("Preferences"))
		a.pump()
		a.ts.Tap(gluttertest.ByText("Keyboard Shortcuts"))
		a.pump()
		wantNames(t, a.tabNames(), "a.txt", "Keyboard Shortcuts")
		a.noErrors()
	})
}

func TestTheUserKeybindingsWorkAtOnce(t *testing.T) {
	a := newKeybindingsApp(t, "", `// mine
[
	{ "key": "ctrl+alt+9", "command": "workbench.action.openGlobalKeybindings" },
	{ "key": "ctrl+,", "command": "-workbench.action.openSettings" }
]`)
	// the file at the start of the window: a new key works, a removed default does not
	a.pressUnbound("ctrl+,")
	wantNames(t, a.tabNames(), "a.txt")
	a.press("ctrl+alt+9")
	wantNames(t, a.tabNames(), "a.txt", "Keyboard Shortcuts")
	// the menus and the status bar follow: the label of the key of a command is the new one
	if got := a.win.keyLabelOf("workbench.action.openGlobalKeybindings"); got != "Ctrl+Alt+9" {
		t.Errorf("label %q", got)
	}
	if got := a.win.keyLabelOf("workbench.action.openSettings"); got != "" {
		t.Errorf("label of the removed key %q", got)
	}
	// the file changes: the keys follow
	if err := os.WriteFile(a.keybindingsFile(), []byte(`[{ "key": "ctrl+alt+8", "command": "workbench.action.openSettings" }]`), 0o644); err != nil {
		t.Fatal(err)
	}
	a.win.userKeys.reload(context.Background(), false)
	a.press("ctrl+alt+8")
	wantNames(t, a.tabNames(), "a.txt", "Keyboard Shortcuts", "Settings")
	a.pressUnbound("ctrl+alt+9") // gone
	wantNames(t, a.tabNames(), "a.txt", "Keyboard Shortcuts", "Settings")
	a.noErrors()
}

func TestAKeybindingsFileWithProblemsStillWorks(t *testing.T) {
	a := newKeybindingsApp(t, "", `[
	{ "key": "ctrl+alt+9", "command": "workbench.action.openGlobalKeybindings" },
	{ "key": "nokey", "command": "x" },
	{ "key": "ctrl+alt+7", "command": "y", "when": "a &&" }
]`)
	if got := len(a.win.userKeys.Problems()); got != 2 {
		t.Errorf("%d problems", got)
	}
	a.press("ctrl+alt+9")
	wantNames(t, a.tabNames(), "a.txt", "Keyboard Shortcuts")
	a.noErrors()
}

func TestADefineKeybindingChangesAndWorks(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.searchKeybindings(`"ctrl+,"`)
	a.tapKeysRow("workbench.action.openSettings", "ctrl+,")
	// Enter on the row opens the box that records the keys
	a.press("enter")
	if !a.win.Dispatcher.Recording() || !a.ts.Exists(gluttertest.ByText(keybindingsui.DefineMessage)) {
		t.Fatal("the box is not shown")
	}
	a.press("ctrl+shift+9")
	if !a.ts.Exists(gluttertest.ByText("ctrl+shift+9")) {
		t.Error("the keys are not shown")
	}
	a.press("enter")
	if a.win.Dispatcher.Recording() || a.ts.Exists(gluttertest.ByText(keybindingsui.DefineMessage)) {
		t.Error("the box stays")
	}
	want := []string{"ctrl+shift+9|workbench.action.openSettings|", "ctrl+,|-workbench.action.openSettings|"}
	if got := a.fileEntries(); !reflect.DeepEqual(got, want) {
		t.Errorf("entries %q, want %q\n%s", got, want, readFile(t, a.keybindingsFile()))
	}
	// the new key works, the old one does not
	a.run("workbench.action.closeActiveEditor")
	a.pressUnbound("ctrl+,")
	wantNames(t, a.tabNames(), "a.txt")
	a.press("ctrl+shift+9")
	wantNames(t, a.tabNames(), "a.txt", "Settings")
	a.noErrors()
}

func TestTheKeybindingsEditorFollowsTheEdit(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.searchKeybindings("workbench.action.openSettings")
	a.tapKeysRow("workbench.action.openSettings", "ctrl+,")
	a.press("enter")
	a.press("ctrl+shift+9")
	a.press("enter")
	if !a.keysRowExists("workbench.action.openSettings", "ctrl+shift+9") {
		t.Error("the new row is not shown")
	}
	if a.keysRowExists("workbench.action.openSettings", "ctrl+,") {
		t.Error("the old row is shown")
	}
	// the row of the new key is the selected one
	if row, ok := a.keysPane().ctl.Selected(); !ok || row.Key != "ctrl+shift+9" || row.Source != keybinding.SourceUser {
		t.Errorf("selected %+v", row)
	}
	if got := a.keysPane().input.ViewState(); got.Command != "workbench.action.openSettings" || got.Key != "ctrl+shift+9" {
		t.Errorf("view state %+v", got)
	}
	a.noErrors()
}

// defineKeys defines the keys of the row of a command with the editor open on it.
func (a *groupApp) defineKeys(command, key, keys string, add bool) {
	a.t.Helper()
	a.searchKeybindings(command)
	a.tapKeysRow(command, key)
	if add {
		a.press("ctrl+k ctrl+a")
	} else {
		a.press("enter")
	}
	a.press(keys)
	a.press("enter")
}

func TestAddAKeybinding(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.defineKeys("workbench.action.openSettings", "ctrl+,", "ctrl+alt+5", true)
	if got, want := a.fileEntries(), []string{"ctrl+alt+5|workbench.action.openSettings|"}; !reflect.DeepEqual(got, want) {
		t.Errorf("entries %q, want %q", got, want)
	}
	// both keys work
	a.press("ctrl+,")
	wantNames(t, a.tabNames(), "a.txt", "Keyboard Shortcuts", "Settings")
	a.run("workbench.action.closeActiveEditor")
	a.press("ctrl+alt+5")
	wantNames(t, a.tabNames(), "a.txt", "Keyboard Shortcuts", "Settings")
	a.noErrors()
}

func TestChangeAUserKeybindingInPlace(t *testing.T) {
	a := newKeybindingsApp(t, "", "// keep me\n[\n\t{ \"key\": \"ctrl+alt+5\", \"command\": \"workbench.action.openSettings\" }\n]\n")
	a.defineKeys("workbench.action.openSettings", "ctrl+alt+5", "ctrl+alt+6", false)
	text := readFile(t, a.keybindingsFile())
	if !strings.HasPrefix(text, "// keep me\n") {
		t.Errorf("the comment went: %q", text)
	}
	if got, want := a.fileEntries(), []string{"ctrl+alt+6|workbench.action.openSettings|"}; !reflect.DeepEqual(got, want) {
		t.Errorf("entries %q, want %q", got, want)
	}
	a.noErrors()
}

func TestConflictsAskWhatToDo(t *testing.T) {
	// ctrl+b toggles the side bar; the row to define is Settings
	for _, tc := range []struct {
		name, button string
		wantEntries  []string
		opens        bool
		sidebar      bool
	}{
		{"keep both", keybindingsui.KeepBoth, []string{"ctrl+b|workbench.action.openSettings|"}, true, true},
		{"overwrite", keybindingsui.Overwrite, []string{"ctrl+b|workbench.action.openSettings|", "ctrl+b|-workbench.action.toggleSidebarVisibility|"}, true, true},
		{"cancel", keybindingsui.Cancel, nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newKeybindingsApp(t, "", "")
			a.searchKeybindings("workbench.action.openSettings")
			a.tapKeysRow("workbench.action.openSettings", "ctrl+,")
			a.press("ctrl+k ctrl+a")
			a.press("ctrl+b")
			if !a.ts.Exists(gluttertest.ByText(keybindingsui.ExistingText(1))) {
				t.Fatal("the box does not warn")
			}
			a.press("enter")
			if !a.win.Dialogs.IsShowing() {
				t.Fatal("no dialog")
			}
			if got := a.fileEntries(); len(got) != 0 {
				t.Errorf("the file has %q before the answer", got)
			}
			a.ts.Tap(gluttertest.ByText(tc.button))
			a.pump()
			if got := a.fileEntries(); !reflect.DeepEqual(got, tc.wantEntries) && (len(got) != 0 || len(tc.wantEntries) != 0) {
				t.Errorf("entries %q, want %q", got, tc.wantEntries)
			}
			// the key does what the choice made it do
			a.run("workbench.action.closeActiveEditor")
			a.win.Layout.SetSideBarVisible(true)
			a.press("ctrl+b")
			opened := len(a.tabNames()) == 2
			if opened != tc.opens {
				t.Errorf("ctrl+b opened the settings: %v, want %v (%v)", opened, tc.opens, a.tabNames())
			}
			if got := a.win.Layout.State().SideBarVisible; got != tc.sidebar {
				t.Errorf("the side bar is visible: %v, want %v", got, tc.sidebar)
			}
			a.noErrors()
		})
	}
}

func TestRemoveAKeybinding(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.searchKeybindings(`"ctrl+,"`)
	a.tapKeysRow("workbench.action.openSettings", "ctrl+,")
	a.press("delete")
	if got, want := a.fileEntries(), []string{"ctrl+,|-workbench.action.openSettings|"}; !reflect.DeepEqual(got, want) {
		t.Errorf("entries %q, want %q", got, want)
	}
	// the key does nothing now, and the row has no key
	a.run("workbench.action.closeActiveEditor")
	a.pressUnbound("ctrl+,")
	wantNames(t, a.tabNames(), "a.txt")
	a.searchKeybindings("workbench.action.openSettings")
	if a.keysRowExists("workbench.action.openSettings", "ctrl+,") || !a.keysRowExists("workbench.action.openSettings", "") {
		t.Error("the rows are not what they should be")
	}
	a.noErrors()
}

func TestResetAKeybinding(t *testing.T) {
	a := newKeybindingsApp(t, "", `[
	{ "key": "ctrl+alt+5", "command": "workbench.action.openSettings" },
	{ "key": "ctrl+,", "command": "-workbench.action.openSettings" },
	{ "key": "ctrl+alt+1", "command": "workbench.action.openGlobalKeybindings" }
]`)
	a.searchKeybindings("workbench.action.openSettings")
	a.tapKeysRow("workbench.action.openSettings", "ctrl+alt+5")
	a.run("keybindings.editor.resetKeybinding")
	if got, want := a.fileEntries(), []string{"ctrl+alt+1|workbench.action.openGlobalKeybindings|"}; !reflect.DeepEqual(got, want) {
		t.Errorf("entries %q, want %q", got, want)
	}
	// the built-in key is back
	a.run("workbench.action.closeActiveEditor")
	a.press("ctrl+,")
	wantNames(t, a.tabNames(), "a.txt", "Settings")
	a.noErrors()
}

func TestChangeTheWhenClause(t *testing.T) {
	a := newKeybindingsApp(t, "", `[{ "key": "ctrl+alt+5", "command": "workbench.action.openSettings" }]`)
	a.searchKeybindings("workbench.action.openSettings")
	a.tapKeysRow("workbench.action.openSettings", "ctrl+alt+5")
	a.press("ctrl+k ctrl+e")
	input := gluttertest.ByKey(foundation.NewValueKey("keybindingsui-when"))
	if !a.ts.Exists(input) {
		t.Fatal("no box")
	}
	// a text that is not a clause is not written
	a.ts.EnterText(input, "isLinux &&")
	a.press("enter")
	if got := a.fileEntries(); !reflect.DeepEqual(got, []string{"ctrl+alt+5|workbench.action.openSettings|"}) {
		t.Errorf("entries %q", got)
	}
	if !a.ts.Exists(gluttertest.ByTextContaining("Not a when-clause")) {
		t.Error("no message")
	}
	a.ts.EnterText(input, "editorTextFocus")
	a.press("enter")
	if got := a.fileEntries(); !reflect.DeepEqual(got, []string{"ctrl+alt+5|workbench.action.openSettings|editorTextFocus"}) {
		t.Errorf("entries %q", got)
	}
	a.noErrors()
}

func TestCopyARow(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.searchKeybindings(`"ctrl+,"`)
	a.tapKeysRow("workbench.action.openSettings", "ctrl+,")
	a.press("ctrl+c")
	text, ok := a.ts.ClipboardText()
	if !ok || text != "{\n  \"key\": \"ctrl+,\",\n  \"command\": \"workbench.action.openSettings\"\n}" {
		t.Errorf("clipboard %q %v", text, ok)
	}
	a.run("keybindings.editor.copyCommandKeybindingEntry")
	if text, _ := a.ts.ClipboardText(); text != "workbench.action.openSettings" {
		t.Errorf("clipboard %q", text)
	}
	a.run("keybindings.editor.copyCommandTitle")
	if text, _ := a.ts.ClipboardText(); text != "Settings" {
		t.Errorf("clipboard %q", text)
	}
	a.noErrors()
}

func TestTheSearchKeysOfTheEditor(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.run("workbench.action.openGlobalKeybindings")
	// Ctrl+F focuses the search from the rows
	a.run("keybindings.editor.focusKeybindings")
	a.press("ctrl+f")
	// Alt+K records the keys that are pressed, as a quoted search; Escape ends it
	a.press("alt+k")
	if !a.keysPane().ctl.Recording() || !a.win.Dispatcher.Recording() {
		t.Fatal("not recording")
	}
	a.press("ctrl+shift+p")
	if got := a.keysPane().ctl.Query(); got != `"ctrl+shift+p"` {
		t.Errorf("query %q", got)
	}
	if !a.keysRowExists("workbench.action.showCommands", "ctrl+shift+p") {
		t.Error("the row of the keys is not found")
	}
	a.press("escape")
	if a.keysPane().ctl.Recording() || a.win.Dispatcher.Recording() {
		t.Error("still recording")
	}
	// Escape in the search box clears the search
	a.press("escape")
	if got := a.keysPane().ctl.Query(); got != "" {
		t.Errorf("query %q", got)
	}
	a.noErrors()
}

func TestTheDefaultKeyboardShortcutsDocument(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.run("workbench.action.openDefaultKeybindingsFile")
	wantNames(t, a.tabNames(), "a.txt", DefaultKeybindingsLabel)
	doc := a.shell().activeGroup().Model.ActiveEditor().(*editorgroup.TextInput).Document()
	if !doc.ReadOnly() {
		t.Error("the document is not read-only")
	}
	text := doc.Text()
	if !strings.HasPrefix(text, keybinding.DefaultKeybindingsHeader) || !strings.Contains(text, `"command": "workbench.action.openGlobalKeybindings"`) {
		t.Errorf("document %.200q", text)
	}
	if _, problems := keybinding.ParseUserFile(text); len(problems) != 0 {
		t.Errorf("problems %v", problems)
	}
	if doc.Dirty() {
		t.Error("dirty")
	}
	a.noErrors()
}

func TestOpenKeyboardShortcutsJSON(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	if _, err := os.Stat(a.keybindingsFile()); err == nil {
		t.Fatal("the file is there already")
	}
	a.run("workbench.action.openGlobalKeybindingsFile")
	a.waitFor("keybindings.json to open", func() bool { return a.active() == "keybindings.json" })
	if got := readFile(t, a.keybindingsFile()); got != keybinding.EmptyUserFile {
		t.Errorf("file %q", got)
	}
	// the button of the editor opens it too
	a.run("workbench.action.openGlobalKeybindings")
	a.ts.Tap(gluttertest.ByKey(foundation.NewValueKey("keybindingsui-open-json")))
	a.waitFor("keybindings.json to be shown", func() bool { return a.active() == "keybindings.json" })
	// a file that is there is left alone
	if err := os.WriteFile(a.keybindingsFile(), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.action.openGlobalKeybindingsFile")
	if got := readFile(t, a.keybindingsFile()); got != "[]" {
		t.Errorf("file %q", got)
	}
	a.noErrors()
}

// keybindingsDoc opens the keybindings file of the user in an editor.
func (a *groupApp) keybindingsDoc() *document.Document {
	a.t.Helper()
	if err := a.win.userKeys.ensure(context.Background()); err != nil {
		a.t.Fatal(err)
	}
	doc, err := a.win.Documents.Acquire(context.Background(), a.keybindingsFile(), document.NewOptions())
	if err != nil {
		a.t.Fatal(err)
	}
	s := a.shell()
	s.show(widgets.WidgetsBindingOf(s.Context()), doc, openOptions{})
	a.pump()
	return doc
}

func TestADirtyKeybindingsDocumentGetsTheEdit(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	doc := a.keybindingsDoc()
	if _, err := doc.Edit([]document.Edit{{Text: "// typed, not saved\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	onDisk := readFile(t, a.keybindingsFile())
	a.defineKeys("workbench.action.openSettings", "ctrl+,", "ctrl+alt+5", true)
	if got := readFile(t, a.keybindingsFile()); got != onDisk {
		t.Errorf("the file was written: %q", got)
	}
	text := doc.Text()
	if !strings.HasPrefix(text, "// typed, not saved\n") || !strings.Contains(text, `"key": "ctrl+alt+5"`) {
		t.Errorf("document %q", text)
	}
	if !doc.Dirty() {
		t.Error("the document is not dirty after the edit")
	}
	// the keys follow the document
	a.run("workbench.action.closeActiveEditor")
	a.press("ctrl+alt+5")
	if got := a.tabNames(); got[len(got)-1] != "Settings" {
		t.Errorf("tabs %v", got)
	}
	a.noErrors()
}

func TestACleanKeybindingsDocumentGetsTheEditAndIsSaved(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	doc := a.keybindingsDoc()
	if doc.Dirty() {
		t.Fatal("the document is dirty")
	}
	a.defineKeys("workbench.action.openSettings", "ctrl+,", "ctrl+alt+5", true)
	a.waitFor("the file to be written", func() bool { return strings.Contains(readFile(t, a.keybindingsFile()), `"ctrl+alt+5"`) })
	a.waitFor("the document to be saved", func() bool { return !doc.Dirty() })
	if got, want := doc.Text(), readFile(t, a.keybindingsFile()); got != want {
		t.Errorf("document %q, file %q", got, want)
	}
	a.noErrors()
}

func TestAClosedKeybindingsDocumentIsNotUsed(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.keybindingsDoc()
	a.run("workbench.action.closeActiveEditor")
	if _, ok := a.win.settingsDocs.OpenText(a.keybindingsFile()); ok {
		t.Fatal("the closed document is still used")
	}
	a.defineKeys("workbench.action.openSettings", "ctrl+,", "ctrl+alt+5", true)
	if got := readFile(t, a.keybindingsFile()); !strings.Contains(got, `"ctrl+alt+5"`) {
		t.Errorf("file %q", got)
	}
	a.noErrors()
}

func TestSavingTheKeybindingsDocumentChangesTheKeys(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	doc := a.keybindingsDoc()
	text := "[{ \"key\": \"ctrl+alt+4\", \"command\": \"workbench.action.openSettings\" }]"
	if _, err := doc.Edit([]document.Edit{{Range: buffer0(doc), Text: text}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	// typed, not saved: the keys are not changed yet
	a.pressUnbound("ctrl+alt+4")
	if len(a.tabNames()) != 2 {
		t.Fatalf("tabs %v", a.tabNames())
	}
	a.run("workbench.action.files.save")
	a.waitFor("the keys to follow the file", func() bool { return a.win.keyLabelOf("workbench.action.openSettings") == "Ctrl+Alt+4" })
	a.noErrors()
}

func TestAKeybindingsFileWithErrorsIsNotEdited(t *testing.T) {
	a := newKeybindingsApp(t, "", `[ { "key": "ctrl+9" `)
	err := a.win.userKeys.Edit(context.Background(), func(text string, e keybinding.Editing) (string, error) {
		return e.Add(text, "x", "ctrl+x", "", nil)
	})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Editing.Add->open.KeybindingsEditFileInvalid") {
		t.Fatalf("error %v", err)
	}
	if got := readFile(t, a.keybindingsFile()); got != `[ { "key": "ctrl+9" ` {
		t.Errorf("the file changed: %q", got)
	}
	// the editor reports it
	a.run("workbench.action.openGlobalKeybindings")
	a.keysPane().s.keybindingEditFailed(err)
	a.noErrors()
}

func TestTheKeybindingsFileCannotBeWritten(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	if os.Geteuid() == 0 {
		t.Skip("a folder that cannot be written is written by root")
	}
	// a folder of the configuration that cannot be written
	dir := filepath.Dir(a.keybindingsFile())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := a.win.userKeys.Edit(context.Background(), func(text string, e keybinding.Editing) (string, error) {
		return e.Add(text, "x", "ctrl+x", "", nil)
	})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("userKeys.write->Write->writeInPlace.FilePermissionDenied") {
		t.Fatalf("error %v", err)
	}
	if err := a.win.userKeys.ensure(context.Background()); err == nil {
		t.Error("the file was made")
	} else if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("userKeys.ensure->Write->writeInPlace.FilePermissionDenied") {
		t.Errorf("error %v", err)
	}
}

// A folder of the configuration that cannot be made (a file is where it should be) is the tags
// KeybindingsFileWriteFailed and KeybindingsFileCreateFailed.
func TestTheFolderOfTheKeybindingsFileCannotBeMade(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	dir := filepath.Dir(a.keybindingsFile())
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := a.win.userKeys.write(context.Background(), "[]")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("userKeys.write.KeybindingsFileWriteFailed") || appErr.Metadata["path"] != a.keybindingsFile() {
		t.Errorf("write: %v", err)
	}
	err = a.win.userKeys.ensure(context.Background())
	appErr, ok = errors.FromError(err)
	if !ok || !appErr.HasRoute("userKeys.ensure.KeybindingsFileCreateFailed") || appErr.Metadata["path"] != a.keybindingsFile() {
		t.Errorf("ensure: %v", err)
	}
}

func TestTheKeybindingsEditorComesBackAfterARestart(t *testing.T) {
	env := newStateEnv(t, nil)
	first := env.start(env.root)
	first.open(env.path("main.go"), 0)
	first.command("workbench.action.openGlobalKeybindings", "copy line")
	input := func(r *stateRun) *editorgroup.KeybindingsInput {
		for _, in := range r.shell().activeGroup().Model.Editors(editorgroup.Sequential, false) {
			if ki, ok := in.(*editorgroup.KeybindingsInput); ok {
				return ki
			}
		}
		return nil
	}
	if in := input(first); in == nil || in.ViewState().Query != "copy line" {
		t.Fatalf("view state %+v", in)
	}
	wantLayout := first.layout()
	first.close()

	second := env.start(env.root)
	in := input(second)
	if in == nil {
		t.Fatalf("the keybindings editor did not come back: %v", second.layout())
	}
	if got := in.ViewState(); got.Query != "copy line" {
		t.Errorf("view state %+v", got)
	}
	if got := second.layout(); strings.Join(got, "|") != strings.Join(wantLayout, "|") {
		t.Errorf("layout %v, want %v", got, wantLayout)
	}
	if !second.ts.Exists(gluttertest.ByType[*keybindingsui.Editor]()) {
		t.Error("the editor is not shown")
	}
	if got := second.shell().group.panes[in].(*keybindingsPane).ctl.ViewState(); got.Query != "copy line" {
		t.Errorf("pane view state %+v", got)
	}
}

func TestTheKeybindingsEditorKind(t *testing.T) {
	if k, ok := kindOf(editorgroup.KeybindingsInputTypeID); !ok || k.restore == nil || k.newPane == nil || k.icon == 0 {
		t.Fatalf("keybindings kind %+v", k)
	}
	if _, ok := keybindingsKind().restore("{nope"); ok {
		t.Error("a broken saved value came back")
	}
	in, ok := keybindingsKind().restore(`{"query":"x","command":"c","key":"ctrl+a"}`)
	if !ok || in.(*editorgroup.KeybindingsInput).ViewState() != (editorgroup.KeybindingsViewState{Query: "x", Command: "c", Key: "ctrl+a"}) {
		t.Errorf("restored %v %v", in, ok)
	}
}

func TestTheContextMenuOfARowInTheWindow(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.searchKeybindings(`"ctrl+,"`)
	right := gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)}
	a.ts.TapAt(a.ts.Center(a.keysRow("workbench.action.openSettings", "ctrl+,")), right)
	a.pump()
	rows := menuRows(a.openMenu())
	want := []string{"Copy", "Copy Command ID", "Copy Command Title", "-", "Change Keybinding...", "Add Keybinding...", "-", "Remove Keybinding", "Reset Keybinding~", "-", "Change When Expression", "-", "Show Same Keybindings"}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows %q, want %q", rows, want)
	}
	// the keys of the rows are those of the commands
	for _, it := range a.openMenu() {
		switch kit.MenuLabelText(it.Label) {
		case "Copy":
			if it.Keybinding != "Ctrl+C" {
				t.Errorf("Copy has %q", it.Keybinding)
			}
		case "Remove Keybinding":
			if it.Keybinding != "Del" {
				t.Errorf("Remove has %q", it.Keybinding)
			}
		case "Change Keybinding...":
			if it.Keybinding != "Enter" {
				t.Errorf("Change has %q", it.Keybinding)
			}
		}
	}
	a.noErrors()
}

func TestTheKeybindingCommandsNeedAnEditor(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	for _, id := range []string{"keybindings.editor.searchKeybindings", "keybindings.editor.clearSearchResults", "keybindings.editor.recordSearchKeys",
		"keybindings.editor.focusKeybindings", "keybindings.editor.defineKeybinding", "keybindings.editor.addKeybinding",
		"keybindings.editor.defineWhenExpression", "keybindings.editor.removeKeybinding", "keybindings.editor.resetKeybinding",
		"keybindings.editor.showConflicts", "keybindings.editor.copyKeybindingEntry", "keybindings.editor.copyCommandKeybindingEntry",
		"keybindings.editor.copyCommandTitle"} {
		if err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys, Source: command.SourceAPI}, id, nil); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	a.noErrors()
}

func TestTheRecordingModeEndsWithTheEditor(t *testing.T) {
	a := newKeybindingsApp(t, "", "")
	a.run("workbench.action.openGlobalKeybindings")
	a.run("keybindings.editor.recordSearchKeys")
	if !a.win.Dispatcher.Recording() {
		t.Fatal("not recording")
	}
	a.run("workbench.action.closeActiveEditor")
	if a.win.Dispatcher.Recording() || a.win.recorder != nil {
		t.Error("the dispatcher still records")
	}
	// a recorder that was replaced does not end the mode of the other
	stop1 := a.win.startRecording(&keybindingsui.Recorder{})
	stop2 := a.win.startRecording(&keybindingsui.Recorder{})
	stop1()
	if !a.win.Dispatcher.Recording() {
		t.Error("the first stop ended the second recording")
	}
	stop2()
	if a.win.Dispatcher.Recording() {
		t.Error("the dispatcher records")
	}
	a.noErrors()
}

// buffer0 is the whole text of a document, as a range to replace.
func buffer0(doc *document.Document) buffer.Range {
	snap := doc.Snapshot()
	return buffer.Range{Start: snap.PositionAt(0), End: snap.PositionAt(len(doc.Text()))}
}
