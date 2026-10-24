package app

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests of the items of the status bar and their pickers (step 6.20).

// status is the entry of an item of the bar.
func (a *groupApp) status(id string) workbench.StatusItem {
	a.t.Helper()
	it, ok := a.win.StatusBar.Get(id)
	if !ok {
		a.t.Fatalf("no status bar item %s", id)
	}
	return it
}

// statusText is the text of an item, or "" when it is hidden.
func (a *groupApp) statusText(id string) string {
	it := a.status(id)
	if !it.Visible {
		return ""
	}
	return it.Entry.Text
}

// editorStatus are the texts of the items of the editor, left to right.
func (a *groupApp) editorStatus() []string {
	var out []string
	for _, it := range a.win.StatusBar.Items(workbench.StatusRight) {
		if strings.HasPrefix(it.ID, "status.editor.") {
			out = append(out, it.Entry.Text)
		}
	}
	return out
}

// clickStatus clicks the item of the bar the way the user does.
func (a *groupApp) clickStatus(id string) {
	a.t.Helper()
	f := gluttertest.ByKey(workbench.StatusItemKey(id))
	if !a.ts.Exists(f) {
		a.t.Fatalf("status bar item %s is not on screen", id)
	}
	a.ts.TapAt(a.ts.Center(f), mouseButton)
	a.pump()
}

// pickRow chooses a row of the quick input by its label, as a click on it does.
func (a *groupApp) pickRow(label string) {
	a.t.Helper()
	ctl := a.win.QuickInput
	if !ctl.Visible() {
		a.t.Fatalf("the quick input is not open (choosing %q)", label)
	}
	for _, r := range ctl.Picker().Rows() {
		if r.Item.Label == label && !r.Item.Separator {
			ctl.AcceptRow(r, quickinput.KeyMods{})
			a.pump()
			return
		}
	}
	a.t.Fatalf("no row %q in %v", label, a.pickerLabels())
}

func TestTheEditorItemsShowTheActiveEditor(t *testing.T) {
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.go", "b.txt"},
		map[string]string{"a.go": "package a\n\nfunc f() {\n\tx := 1\n}\n", "b.txt": "one\r\ntwo\r\n"}, "a.go", "b.txt")
	// the first file is shown, with its cursor at the start
	if got := a.editorStatus(); !slices.Equal(got, []string{"Ln 1, Col 1", "Tab Size: 4", "UTF-8", "LF", "Go"}) {
		t.Errorf("a.go: %v", got)
	}
	a.clickTab("b.txt")
	if got := a.editorStatus(); !slices.Equal(got, []string{"Ln 1, Col 1", "Spaces: 4", "UTF-8", "CRLF", "Plain Text"}) {
		t.Errorf("b.txt: %v", got)
	}
	a.clickTab("a.go")
	a.vm().GoToPosition(buffer.Position{Line: 3, Col: 7}) // after "\tx := 1" the tab counts to the tab stop
	a.pump()
	if got := a.statusText("status.editor.selection"); got != "Ln 4, Col 11" {
		t.Errorf("the cursor after a tab: %q", got)
	}
	// every item has its command and tooltip
	for id, want := range map[string][2]string{
		"status.editor.selection":   {"workbench.action.gotoLine", "Go to Line/Column"},
		"status.editor.indentation": {"changeEditorIndentation", "Select Indentation"},
		"status.editor.encoding":    {"workbench.action.editor.changeEncoding", "Select Encoding"},
		"status.editor.eol":         {"workbench.action.editor.changeEOL", "Select End of Line Sequence"},
		"status.editor.mode":        {"workbench.action.editor.changeLanguageMode", "Select Language Mode"},
	} {
		if e := a.status(id).Entry; e.Command != want[0] || e.Tooltip != want[1] {
			t.Errorf("%s: command %q tooltip %q", id, e.Command, e.Tooltip)
		}
	}
	a.noErrors()
}

func TestTheSelectionItemCountsWhatIsSelected(t *testing.T) {
	text := "hello world\nsecond line\nüñí\n"
	tests := []struct {
		name string
		sels []buffer.Selection
		want string
	}{
		{"a cursor", []buffer.Selection{cursorAt(1, 3)}, "Ln 2, Col 4"},
		{"a selection on a line", []buffer.Selection{{Anchor: buffer.Position{Line: 0, Col: 0}, Active: buffer.Position{Line: 0, Col: 5}}}, "Ln 1, Col 6 (5 selected)"},
		{"over two lines counts the break", []buffer.Selection{{Anchor: buffer.Position{Line: 0, Col: 6}, Active: buffer.Position{Line: 1, Col: 3}}}, "Ln 2, Col 4 (9 selected)"},
		{"accents are one character and one column", []buffer.Selection{{Anchor: buffer.Position{Line: 2, Col: 0}, Active: buffer.Position{Line: 2, Col: 6}}}, "Ln 3, Col 4 (3 selected)"},
		{"several cursors", []buffer.Selection{cursorAt(0, 1), cursorAt(1, 1)}, "2 selections"},
		{"several selections", []buffer.Selection{
			{Anchor: buffer.Position{Line: 0, Col: 0}, Active: buffer.Position{Line: 0, Col: 5}},
			{Anchor: buffer.Position{Line: 1, Col: 0}, Active: buffer.Position{Line: 1, Col: 6}}}, "2 selections (11 characters selected)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.txt"}, map[string]string{"a.txt": text}, "a.txt")
			a.vm().Cursors().SetSelections(tt.sels)
			a.vm().SelectionsChanged()
			a.pump()
			if got := a.statusText("status.editor.selection"); got != tt.want {
				t.Errorf("selection item %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTheSelectionItemFollowsEditsAndCRLF(t *testing.T) {
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.txt"}, map[string]string{"a.txt": "ab\r\ncd\r\n"}, "a.txt")
	a.vm().Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 0, Col: 1}, Active: buffer.Position{Line: 1, Col: 1}}})
	a.vm().SelectionsChanged()
	a.pump()
	if got := a.statusText("status.editor.selection"); got != "Ln 2, Col 2 (4 selected)" {
		t.Errorf("a break of a CRLF file counts two: %q", got)
	}
	if err := a.vm().RunCommand("type", map[string]any{"text": "X"}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if got := a.statusText("status.editor.selection"); got != "Ln 1, Col 3" {
		t.Errorf("after typing over the selection: %q", got)
	}
}

func TestTheEditorItemsAreHiddenWithoutAnEditor(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	if len(a.editorStatus()) != 5 {
		t.Fatalf("items %v", a.editorStatus())
	}
	a.run("workbench.action.closeAllEditors")
	if got := a.editorStatus(); len(got) != 0 {
		t.Errorf("the items of the editor stay with no editor: %v", got)
	}
	// the problems and the bell are still there
	if a.statusText("status.problems") == "" || a.statusText("status.notifications") == "" {
		t.Error("the problems or the bell went away")
	}
	a.show("a.go", false)
	if len(a.editorStatus()) != 5 {
		t.Errorf("the items did not come back: %v", a.editorStatus())
	}
	a.noErrors()
}

func TestTheIndentationItemFollowsTheCommands(t *testing.T) {
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.txt"}, map[string]string{"a.txt": "x\n"}, "a.txt")
	if got := a.statusText("status.editor.indentation"); got != "Spaces: 4" {
		t.Fatalf("at first %q", got)
	}
	if err := a.vm().RunCommand("editor.action.indentUsingTabs", 8.0); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if got := a.statusText("status.editor.indentation"); got != "Tab Size: 8" {
		t.Errorf("after Indent Using Tabs: %q", got)
	}
	if err := a.vm().RunCommand("editor.action.indentUsingSpaces", 2.0); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if got := a.statusText("status.editor.indentation"); got != "Spaces: 2" {
		t.Errorf("after Indent Using Spaces: %q", got)
	}
}

func TestTheProblemsItemCountsTheMarkers(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	if got := a.statusText("status.problems"); got != "$(error) 0 $(warning) 0" {
		t.Fatalf("no problems: %q", got)
	}
	if e := a.status("status.problems").Entry; e.Tooltip != "No Problems" || e.Command != "workbench.actions.view.toggleProblems" {
		t.Errorf("entry %+v", e)
	}
	file := a.files["a.go"]
	a.win.Markers.Set("test", file, []problems.Marker{
		{Severity: problems.SeverityError, Message: "e1"}, {Severity: problems.SeverityError, Message: "e2"},
		{Severity: problems.SeverityWarning, Message: "w"}, {Severity: problems.SeverityInfo, Message: "i"},
	})
	a.pump()
	e := a.status("status.problems").Entry
	if e.Text != "$(error) 2 $(warning) 1 $(info) 1" || e.Tooltip != "Errors: 2, Warnings: 1, Infos: 1" {
		t.Errorf("with markers: %q, %q", e.Text, e.Tooltip)
	}
	a.win.Markers.Remove("test", file)
	a.pump()
	if got := a.statusText("status.problems"); got != "$(error) 0 $(warning) 0" {
		t.Errorf("markers removed: %q", got)
	}
	// the left side starts with it, the right side ends with the bell
	left := a.win.StatusBar.Items(workbench.StatusLeft)
	right := a.win.StatusBar.Items(workbench.StatusRight)
	if len(left) == 0 || left[0].ID != "status.problems" || right[len(right)-1].ID != "status.notifications" {
		t.Errorf("order: left %v right %v", left, right)
	}
}

func TestClickingTheProblemsItemTogglesThePanel(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	if a.win.Layout.State().PanelVisible {
		t.Fatal("the panel is shown at first")
	}
	a.clickStatus("status.problems")
	if !a.win.Layout.State().PanelVisible || a.win.Views.ActivePanel() != command.ViewContainerMarkers {
		t.Error("the click did not open the Problems")
	}
	a.noErrors()
}

func TestTheBellShowsNewNotificationsAndOpensTheCenter(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	bell := func() workbench.StatusEntry { return a.status("status.notifications").Entry }
	if bell().Text != "$(bell)" || bell().Tooltip != "No Notifications" {
		t.Fatalf("at first %+v", bell())
	}
	a.win.Notify.Info("Test", "hello")
	a.pump()
	if bell().Text != "$(bell-dot)" || bell().Tooltip != "1 New Notification" {
		t.Errorf("one notification: %+v", bell())
	}
	a.clickStatus("status.notifications")
	if !a.win.Notifications.CenterVisible() {
		t.Fatal("the click did not open the center")
	}
	if bell().Text != "$(bell)" || bell().Tooltip != "Hide Notifications" || bell().Command != "notifications.hideList" {
		t.Errorf("with the center open: %+v", bell())
	}
	a.clickStatus("status.notifications")
	if a.win.Notifications.CenterVisible() {
		t.Error("the second click did not close the center")
	}
	if bell().Tooltip != "No New Notifications" {
		t.Errorf("read: %+v", bell())
	}
	a.win.Notifications.ToggleDoNotDisturb()
	a.pump()
	if bell().Text != "$(bell-slash)" {
		t.Errorf("do not disturb: %+v", bell())
	}
	a.noErrors()
}

func TestTheChordMessageShowsAndClears(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	msg := func() string {
		if it, ok := a.win.StatusBar.Get("status.message"); ok {
			return it.Entry.Text
		}
		return ""
	}
	sendChord(a.ts, keybinding.Chord{Ctrl: true, Key: keybinding.KeyK})
	a.pump()
	if got := msg(); got != "(Ctrl+K) was pressed. Waiting for second key of chord..." {
		t.Fatalf("after the first key: %q", got)
	}
	if left := a.win.StatusBar.Items(workbench.StatusLeft); left[len(left)-1].ID != "status.message" {
		t.Errorf("the message is the last on the left: %v", left)
	}
	// the chord is completed: the message goes
	sendChord(a.ts, keybinding.Chord{Ctrl: true, Key: keybinding.KeyX})
	a.pump()
	if got := msg(); got != "" {
		t.Errorf("after the chord was completed: %q", got)
	}
	// a key that is not a command: a message for ten seconds
	sendChord(a.ts, keybinding.Chord{Ctrl: true, Key: keybinding.KeyK})
	sendChord(a.ts, keybinding.Chord{Key: keybinding.KeyZ})
	a.pump()
	if got := msg(); got != "The key combination (Ctrl+K, Z) is not a command." {
		t.Fatalf("after a failed chord: %q", got)
	}
	a.ts.Pump(9 * time.Second)
	if msg() == "" {
		t.Error("the message went before ten seconds")
	}
	a.ts.Pump(2 * time.Second)
	if got := msg(); got != "" {
		t.Errorf("the message stayed after ten seconds: %q", got)
	}
	// a chord that nobody finishes is forgotten by the dispatcher and the message goes
	sendChord(a.ts, keybinding.Chord{Ctrl: true, Key: keybinding.KeyK})
	a.pump()
	if msg() == "" || !a.win.Dispatcher.InChordMode() {
		t.Fatal("the chord is pending")
	}
	a.ts.Pump(keybinding.ChordTimeout + time.Second)
	if msg() != "" || a.win.Dispatcher.InChordMode() {
		t.Errorf("after the timeout: message %q, pending %v", msg(), a.win.Dispatcher.InChordMode())
	}
	a.noErrors()
}

func TestClickingTheSelectionItemOpensGoToLine(t *testing.T) {
	a := newGroupApp(t, []string{"long.go"}, "long.go")
	a.clickStatus("status.editor.selection")
	ctl := a.win.QuickInput
	if !ctl.Visible() || ctl.Provider() == nil || ctl.Provider().Info().Prefix != ":" {
		t.Fatalf("go to line is not open: visible %v", ctl.Visible())
	}
	a.noErrors()
}

func TestTheTooltipsShowTheKeyOfTheCommand(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	f := gluttertest.ByKey(workbench.StatusItemKey("status.editor.selection"))
	a.ts.Hover(a.ts.Center(f))
	a.ts.Pump(600 * time.Millisecond)
	if !a.ts.Exists(gluttertest.ByText("Go to Line/Column (Ctrl+G)")) {
		t.Error("the tooltip of the selection item shows no key")
	}
	g := gluttertest.ByKey(workbench.StatusItemKey("status.editor.mode"))
	a.ts.Hover(a.ts.Center(g))
	a.ts.Pump(600 * time.Millisecond)
	if !a.ts.Exists(gluttertest.ByText("Select Language Mode (Ctrl+K M)")) {
		t.Error("the tooltip of the language item shows no key")
	}
	// an item without a key shows its tooltip alone
	h := gluttertest.ByKey(workbench.StatusItemKey("status.editor.eol"))
	a.ts.Hover(a.ts.Center(h))
	a.ts.Pump(600 * time.Millisecond)
	if !a.ts.Exists(gluttertest.ByText("Select End of Line Sequence")) {
		t.Error("the tooltip of the end of line item")
	}
	a.noErrors()
}

func TestTheLanguagePicker(t *testing.T) {
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.go", "n.txt"},
		map[string]string{"a.go": "package a\n", "n.txt": ""}, "a.go", "n.txt")
	a.clickTab("a.go")
	a.clickStatus("status.editor.mode")
	ctl := a.win.QuickInput
	if !ctl.Visible() || ctl.Picker().Placeholder() != "Select Language Mode" {
		t.Fatalf("the picker is not open")
	}
	labels := a.pickerLabels()
	if len(labels) < 10 || labels[0] != "Auto Detect" || labels[1] != "Configure File Association for '.go'..." {
		t.Fatalf("first rows %v", labels[:min(3, len(labels))])
	}
	var goRow quickinput.Row
	var plain quickinput.Row
	for _, r := range ctl.Picker().Rows() {
		switch r.Item.ID {
		case "go":
			goRow = r
		case "plaintext":
			plain = r
		}
	}
	if goRow.Item.Label != "Go" || goRow.Item.Description != "(go) - Configured Language" {
		t.Errorf("the current language: %+v", goRow.Item)
	}
	if plain.Item.Description != "(plaintext)" {
		t.Errorf("another language: %+v", plain.Item)
	}
	if goRow.Separator != "languages (identifier)" && ctl.Picker().Rows()[2].Separator != "languages (identifier)" {
		t.Errorf("the languages have a heading")
	}
	// the names are in order
	var names []string
	for _, r := range ctl.Picker().Rows() {
		if r.Item.ID != "" && r.Item.ID != "autoDetect" && r.Item.ID != "configureAssociation" {
			names = append(names, strings.ToLower(r.Item.Label))
		}
	}
	if !slices.IsSorted(names) {
		t.Errorf("the languages are not sorted: %v", names)
	}
	// choosing one changes the language of the document and the item
	a.pickRow("Plain Text")
	if a.doc("a.go").LanguageID() != "plaintext" || a.statusText("status.editor.mode") != "Plain Text" {
		t.Errorf("language %q, item %q", a.doc("a.go").LanguageID(), a.statusText("status.editor.mode"))
	}
	// Auto Detect goes back to the language of the file name
	a.run("workbench.action.editor.changeLanguageMode")
	a.pickRow("Auto Detect")
	if a.doc("a.go").LanguageID() != "go" || a.statusText("status.editor.mode") != "Go" {
		t.Errorf("auto detect: %q, item %q", a.doc("a.go").LanguageID(), a.statusText("status.editor.mode"))
	}
	// an empty document has no Auto Detect
	a.clickTab("n.txt")
	a.run("workbench.action.editor.changeLanguageMode")
	if slices.Contains(a.pickerLabels(), "Auto Detect") {
		t.Error("an empty document offers Auto Detect")
	}
	a.win.QuickInput.Hide()
	a.noErrors()
}

func TestTheLanguageCommandTakesAName(t *testing.T) {
	a := newGroupApp(t, []string{"a.txt"}, "a.txt")
	if err := a.runWith("workbench.action.editor.changeLanguageMode", "Go"); err != nil {
		t.Fatal(err)
	}
	if a.doc("a.txt").LanguageID() != "go" || a.statusText("status.editor.mode") != "Go" {
		t.Errorf("language %q", a.doc("a.txt").LanguageID())
	}
	if err := a.runWith("workbench.action.editor.changeLanguageMode", "No Such Language"); err != nil {
		t.Fatal(err)
	}
	a.pump()
	found := false
	for _, it := range a.win.Notify.Items() {
		found = found || strings.Contains(it.Message, "no language named")
	}
	if !found {
		t.Error("an unknown language is not reported")
	}
}

func TestTheLanguageChangeReachesTheHighlighting(t *testing.T) {
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.txt"}, map[string]string{"a.txt": "func main() {}\n"}, "a.txt")
	e := a.shell().activeEditor()
	if e.tokenizer != nil && e.tokenizer.Active() {
		t.Fatal("plain text is not tokenized")
	}
	if err := a.runWith("workbench.action.editor.changeLanguageMode", "go"); err != nil {
		t.Fatal(err)
	}
	if e.tokenizer == nil || !e.tokenizer.Active() {
		t.Error("the editor did not take the grammar of the new language")
	}
}

func TestConfiguringAFileAssociation(t *testing.T) {
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.foo", "b.foo", "Dockerfile.x"},
		map[string]string{"a.foo": "x\n", "b.foo": "y\n", "Dockerfile.x": "FROM a\n"}, "a.foo", "b.foo", "Dockerfile.x")
	a.clickTab("a.foo")
	if a.doc("a.foo").LanguageID() != "plaintext" {
		t.Fatalf("a .foo file is plain text")
	}
	a.run("workbench.action.editor.changeLanguageMode")
	a.pickRow("Configure File Association for '.foo'...")
	if !a.win.QuickInput.Visible() || a.win.QuickInput.Picker().Placeholder() != "Select Language Mode to Associate with '.foo'" {
		t.Fatalf("the second picker: %q", a.win.QuickInput.Picker().Placeholder())
	}
	var current []string
	for _, r := range a.win.QuickInput.Picker().Rows() {
		if r.Item.Description == "Current Association" {
			current = append(current, r.Item.Label)
		}
	}
	if !slices.Equal(current, []string{"Plain Text"}) {
		t.Errorf("current association %v", current)
	}
	a.pickRow("Go")
	// the setting is written to the user settings
	got, _ := a.win.Settings.Inspect("files.associations", "").User.Value.(map[string]any)
	if got["*.foo"] != "go" {
		t.Errorf("files.associations = %v", got)
	}
	userFile, err := settings.UserSettingsPath(a.win.Process.Paths, storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(userFile)
	if err != nil || !strings.Contains(string(data), `"*.foo": "go"`) {
		t.Errorf("settings.json: %v %s", err, data)
	}
	// both open .foo files take the language, and the item follows
	if a.doc("a.foo").LanguageID() != "go" || a.doc("b.foo").LanguageID() != "go" || a.statusText("status.editor.mode") != "Go" {
		t.Errorf("languages %q %q, item %q", a.doc("a.foo").LanguageID(), a.doc("b.foo").LanguageID(), a.statusText("status.editor.mode"))
	}
	// a file whose name starts with a dot, or has no extension, is associated by its whole name
	a.clickTab("Dockerfile.x")
	a.run("workbench.action.editor.changeLanguageMode")
	a.pickRow("Configure File Association for '.x'...")
	a.pickRow("Go")
	got, _ = a.win.Settings.Inspect("files.associations", "").User.Value.(map[string]any)
	if got["*.x"] != "go" || got["*.foo"] != "go" {
		t.Errorf("the earlier association was lost: %v", got)
	}
	a.noErrors()
}

func TestEOLPicker(t *testing.T) {
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.txt"}, map[string]string{"a.txt": "one\ntwo\n"}, "a.txt")
	doc := a.doc("a.txt")
	a.clickStatus("status.editor.eol")
	ctl := a.win.QuickInput
	if !ctl.Visible() || ctl.Picker().Placeholder() != "Select End of Line Sequence" {
		t.Fatal("the picker is not open")
	}
	if got := a.pickerLabels(); !slices.Equal(got, []string{"LF", "CRLF"}) {
		t.Errorf("rows %v", got)
	}
	if got := a.pickerActive(); got != "LF" {
		t.Errorf("the current one is active: %s", got)
	}
	a.pickRow("CRLF")
	if doc.LineEnding() != fileio.CRLF || a.statusText("status.editor.eol") != "CRLF" {
		t.Errorf("line ending %q, item %q", doc.LineEnding(), a.statusText("status.editor.eol"))
	}
	if !doc.Dirty() {
		t.Error("the change is an edit")
	}
	// it can be undone
	a.run("undo")
	if doc.LineEnding() != fileio.LF || a.statusText("status.editor.eol") != "LF" {
		t.Errorf("after undo: line ending %q, item %q", doc.LineEnding(), a.statusText("status.editor.eol"))
	}
	// the other way round: the picker opens on the current one
	a.run("redo")
	a.run("workbench.action.editor.changeEOL")
	if got := a.pickerActive(); got != "CRLF" {
		t.Errorf("the active one is the current: %s", got)
	}
	a.win.QuickInput.Hide()
	a.noErrors()
}

func TestAReadOnlyEditorHasNoEOLItemAndNoPickers(t *testing.T) {
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.txt"}, map[string]string{"a.txt": "one\n"}, "a.txt")
	a.doc("a.txt").SetReadOnly(true)
	a.shell().refreshStatus()
	a.pump()
	if a.status("status.editor.eol").Visible {
		t.Error("a read-only file shows the end of line item")
	}
	a.run("workbench.action.editor.changeEOL")
	if got := a.pickerLabels(); !slices.Equal(got, []string{"The active code editor is read-only."}) {
		t.Errorf("rows %v", got)
	}
	a.win.QuickInput.Hide()
	a.run("changeEditorIndentation")
	if got := a.pickerLabels(); !slices.Equal(got, []string{"The active code editor is read-only."}) {
		t.Errorf("indentation rows %v", got)
	}
	a.win.QuickInput.Hide()
}

func TestThePickersNeedAnEditor(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.run("workbench.action.closeAllEditors")
	for _, id := range []string{
		"workbench.action.editor.changeLanguageMode", "workbench.action.editor.changeEOL",
		"workbench.action.editor.changeEncoding", "changeEditorIndentation",
	} {
		a.run(id)
		if got := a.pickerLabels(); !slices.Equal(got, []string{"No text editor active at this time"}) {
			t.Errorf("%s: rows %v", id, got)
		}
		// choosing the line closes the picker
		a.pickRow("No text editor active at this time")
		if a.win.QuickInput.Visible() {
			t.Errorf("%s: the picker stays open", id)
		}
	}
}

func TestTheIndentationPicker(t *testing.T) {
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.txt"}, map[string]string{"a.txt": "x\n    y\n"}, "a.txt")
	a.clickStatus("status.editor.indentation")
	ctl := a.win.QuickInput
	if !ctl.Visible() || ctl.Picker().Placeholder() != "Select Action" {
		t.Fatal("the picker is not open")
	}
	want := []string{"Indent Using Spaces", "Indent Using Tabs", "Change Tab Display Size", "Detect Indentation from Content",
		"Convert Indentation to Spaces", "Convert Indentation to Tabs", "Trim Trailing Whitespace"}
	var got []string
	var headings []string
	for _, r := range ctl.Picker().Rows() {
		got = append(got, r.Item.Label)
		if r.Separator != "" {
			headings = append(headings, r.Separator)
		}
	}
	if !slices.Equal(got, want) || !slices.Equal(headings, []string{"change view", "convert file"}) {
		t.Errorf("rows %v headings %v", got, headings)
	}
	// Indent Using Tabs asks for the tab size, and then applies it
	a.pickRow("Indent Using Tabs")
	if !a.win.QuickInput.Visible() || a.win.QuickInput.Picker().Placeholder() != "Select Tab Size for Current File" {
		t.Fatalf("the tab size picker: visible %v", a.win.QuickInput.Visible())
	}
	if got := a.pickerActive(); got != "4" {
		t.Errorf("the current size is active: %s", got)
	}
	a.pickRow("2")
	if a.statusText("status.editor.indentation") != "Tab Size: 2" {
		t.Errorf("item %q", a.statusText("status.editor.indentation"))
	}
	// the conversion of the text
	a.run("changeEditorIndentation")
	a.pickRow("Convert Indentation to Spaces")
	if a.statusText("status.editor.indentation") != "Spaces: 2" {
		t.Errorf("after the conversion: %q", a.statusText("status.editor.indentation"))
	}
	a.run("changeEditorIndentation")
	a.pickRow("Detect Indentation from Content")
	if a.statusText("status.editor.indentation") != "Spaces: 4" {
		t.Errorf("after detecting: %q", a.statusText("status.editor.indentation"))
	}
	a.noErrors()
}

// latin1File is a file in Windows 1252 with an e acute.
const latin1File = "caf\xe9\nna\xefve\n"

func TestReopenWithEncoding(t *testing.T) {
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.txt"}, map[string]string{"a.txt": latin1File}, "a.txt")
	doc := a.doc("a.txt")
	_ = doc
	a.clickStatus("status.editor.encoding")
	ctl := a.win.QuickInput
	if !ctl.Visible() || ctl.Picker().Placeholder() != "Select Action" {
		t.Fatalf("the action picker: visible %v", ctl.Visible())
	}
	if got := a.pickerLabels(); !slices.Equal(got, []string{"Reopen with Encoding", "Save with Encoding"}) {
		t.Errorf("actions %v", got)
	}
	a.pickRow("Reopen with Encoding")
	a.waitFor("the encodings", func() bool {
		return a.win.QuickInput.Visible() && a.win.QuickInput.Picker().Placeholder() == "Select File Encoding to Reopen File"
	})
	labels := a.pickerLabels()
	if len(labels) < 20 || labels[0] != "UTF-8" {
		t.Fatalf("encodings %v", labels[:min(5, len(labels))])
	}
	if slices.Contains(labels, "UTF-8 with BOM") {
		t.Error("an encoding that can only be saved in is offered to reopen with")
	}
	if !slices.Contains(labels, "Western (Windows 1252)") || !slices.Contains(labels, "Japanese (Shift JIS)") {
		t.Errorf("encodings %v", labels)
	}
	// the current encoding is active; this file is read as UTF-8 with the bad byte replaced
	if got := a.pickerActive(); got != "UTF-8" {
		t.Errorf("active %q", got)
	}
	a.pickRow("Western (Windows 1252)")
	a.waitFor("the text read again", func() bool { return a.doc("a.txt").Encoding() == "windows1252" })
	if got := a.doc("a.txt").Text(); got != "café\nnaïve\n" {
		t.Errorf("text %q", got)
	}
	if a.statusText("status.editor.encoding") != "Windows 1252" {
		t.Errorf("item %q", a.statusText("status.editor.encoding"))
	}
	if a.doc("a.txt").Dirty() {
		t.Error("a reopened file is not dirty")
	}
	a.noErrors()
}

func TestReopenWithEncodingOfADirtyFileAsks(t *testing.T) {
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"a.txt"}, map[string]string{"a.txt": latin1File}, "a.txt")
	a.edit("a.txt")
	if !a.doc("a.txt").Dirty() {
		t.Fatal("the edit did not make the file dirty")
	}
	choose := func() {
		a.run("workbench.action.editor.changeEncoding")
		a.pickRow("Reopen with Encoding")
		a.waitFor("the encodings", func() bool { return a.win.QuickInput.Visible() && len(a.pickerLabels()) > 5 })
		a.pickRow("Western (Windows 1252)")
	}
	// cancelling keeps the changes
	choose()
	a.waitFor("the question", func() bool { return a.win.Dialogs.IsShowing() })
	spec, _ := a.win.Dialogs.Current()
	if !strings.Contains(spec.Message, "revert the active text editor and reopen with a different encoding") ||
		!slices.Contains(spec.Buttons, "Discard Changes and Reopen") {
		t.Errorf("dialog %+v", spec)
	}
	a.win.Dialogs.Dismiss()
	a.pump()
	if a.doc("a.txt").Encoding() == "windows1252" || !a.doc("a.txt").Dirty() {
		t.Error("cancelling changed the file")
	}
	// confirming discards them
	choose()
	a.waitFor("the question", func() bool { return a.win.Dialogs.IsShowing() })
	a.ts.Tap(gluttertest.ByText("Discard Changes and Reopen"))
	a.waitFor("the file read again", func() bool { return a.doc("a.txt").Encoding() == "windows1252" })
	if a.doc("a.txt").Dirty() || a.doc("a.txt").Text() != "café\nnaïve\n" {
		t.Errorf("dirty %v text %q", a.doc("a.txt").Dirty(), a.doc("a.txt").Text())
	}
	a.noErrors()
}

func TestTheEncodingPickerOffersTheGuessedEncoding(t *testing.T) {
	// a UTF-16 file with a byte order mark: the guess is UTF-16 LE, and the setting says UTF-8
	utf16 := "\xff\xfeh\x00i\x00\n\x00"
	a := newGroupAppWith(t, newTestWindow(t, ""), testWindowSize, []string{"u.txt"}, map[string]string{"u.txt": utf16}, "u.txt")
	if a.doc("u.txt").Encoding() != "utf16le" || a.statusText("status.editor.encoding") != "UTF-16 LE" {
		t.Fatalf("encoding %q, item %q", a.doc("u.txt").Encoding(), a.statusText("status.editor.encoding"))
	}
	a.run("workbench.action.editor.changeEncoding")
	a.pickRow("Reopen with Encoding")
	a.waitFor("the encodings", func() bool { return a.win.QuickInput.Visible() && len(a.pickerLabels()) > 5 })
	rows := a.win.QuickInput.Picker().Rows()
	if rows[0].Item.Label != "UTF-16 LE" || rows[0].Item.Description != "Guessed from content" {
		t.Errorf("first row %+v", rows[0].Item)
	}
	// the guess is not listed twice
	n := 0
	for _, r := range rows {
		if r.Item.Label == "UTF-16 LE" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("UTF-16 LE is listed %d times", n)
	}
	a.win.QuickInput.Hide()
}

func TestEncodingPicks(t *testing.T) {
	labels := func(items []quickinput.Item) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.Label)
		}
		return out
	}
	noop := func(fileio.Encoding) {}
	tests := []struct {
		name                string
		configured, guessed string
		reopen              bool
		first               []string
		absent              []string
	}{
		{"the configured one first", "utf8", "", true, []string{"UTF-8", "UTF-16 LE", "UTF-16 BE"}, []string{"UTF-8 with BOM"}},
		{"to save in, the BOM one is there", "utf8", "", false, []string{"UTF-8", "UTF-8 with BOM", "UTF-16 LE"}, nil},
		{"another configured one moves up", "windows1252", "", true, []string{"Western (Windows 1252)", "UTF-8", "UTF-16 LE"}, nil},
		{"the guess first and not again", "utf8", "utf16le", true, []string{"UTF-16 LE", "", "UTF-8", "UTF-16 BE"}, nil},
		{"a guess that is the configured one is not repeated", "utf8", "utf8", true, []string{"UTF-8", "UTF-16 LE"}, nil},
		{"a guess that can only be saved in is not offered to reopen", "utf8", "utf8bom", true, []string{"UTF-8", "UTF-16 LE"}, []string{"UTF-8 with BOM"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := labels(encodingPicks(tt.configured, tt.guessed, tt.reopen, noop))
			if len(got) < len(tt.first) || !slices.Equal(got[:len(tt.first)], tt.first) {
				t.Errorf("first rows %v, want %v", got[:min(len(got), len(tt.first)+1)], tt.first)
			}
			for _, a := range tt.absent {
				if slices.Contains(got, a) {
					t.Errorf("%q is listed", a)
				}
			}
		})
	}
}

func TestEveryStatusBarCommandHasAHandler(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	for _, id := range []string{
		"workbench.action.editor.changeLanguageMode", "workbench.action.editor.changeEOL",
		"workbench.action.editor.changeEncoding", "changeEditorIndentation",
	} {
		if !a.win.Commands.HasHandler(id) {
			t.Errorf("%s has no handler", id)
		}
	}
}

func TestAFailingStatusCommandIsReported(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	a.win.StatusBar.Add("test.item", workbench.StatusRight, 1, workbench.StatusEntry{Text: "boom", Command: "no.such.command"})
	a.pump()
	a.clickStatus("test.item")
	found := false
	for _, it := range a.win.Notify.Items() {
		if it.Severity == notify.Error {
			found = true
		}
	}
	if !found {
		t.Error("a command that fails is not reported")
	}
}

func TestAnItemOfAnotherStepShowsAndRuns(t *testing.T) {
	a := newGroupApp(t, []string{"a.go"}, "a.go")
	acc := a.win.StatusBar.Add("test.item", workbench.StatusLeft, 10, workbench.StatusEntry{
		Text: "$(sync~spin) Syncing", Tooltip: "Syncing", Command: "workbench.action.gotoLine",
	})
	a.pump()
	if !a.ts.Exists(gluttertest.ByKey(workbench.StatusItemKey("test.item"))) {
		t.Fatal("the item is not on screen")
	}
	a.clickStatus("test.item")
	if !a.win.QuickInput.Visible() {
		t.Error("the command of the item did not run")
	}
	a.win.QuickInput.Hide()
	acc.Dispose()
	a.pump()
	if a.ts.Exists(gluttertest.ByKey(workbench.StatusItemKey("test.item"))) {
		t.Error("the item stays after it was disposed")
	}
}
