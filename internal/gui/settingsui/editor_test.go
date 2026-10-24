package settingsui

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

const pump = time.Millisecond

// show filters the list by a key, so that the row is on screen.
func (r *rig) show(query string) {
	r.t.Helper()
	r.ctl.SetQuery(query)
	r.ts.Pump(pump)
	r.ts.Pump(pump)
}

func rowOf(cat, key string) gluttertest.Finder { return gluttertest.ByKey(RowKey(cat, key)) }

// in is a finder of the widgets of a type below the row.
func in[T widgets.Widget](row gluttertest.Finder) gluttertest.Finder {
	return gluttertest.Descendant(row, gluttertest.ByType[T]())
}

func (r *rig) press(k services.LogicalKeyboardKey) {
	r.t.Helper()
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	r.ts.Pump(pump)
}

func (r *rig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("errors: %v", errs)
	}
	if len(r.errs) != 0 {
		r.t.Errorf("reported write errors: %v", r.errs)
	}
}

func TestEditEachKind(t *testing.T) {
	const userText = "// my settings\n{\n  // tabs\n  \"editor.tabSize\": 2\n}\n"
	t.Run("a check box", func(t *testing.T) {
		r := newRig(t, rigOpts{userText: userText})
		r.show("editor.insertSpaces")
		row := rowOf("editor", "editor.insertSpaces")
		if !r.ts.Exists(row) {
			t.Fatal("no row")
		}
		r.ts.Tap(in[*kit.Checkbox](row))
		r.ts.Pump(pump)
		want := "// my settings\n{\n  // tabs\n  \"editor.tabSize\": 2,\n  \"editor.insertSpaces\": false\n}\n"
		if got := r.read(r.userPath); got != want {
			t.Errorf("file %q, want %q", got, want)
		}
		if cb := r.ts.Widget(in[*kit.Checkbox](row)).(*kit.Checkbox); cb.Checked() {
			t.Error("the check box is still checked")
		}
		// the text of the description flips it back
		r.ts.Tap(gluttertest.ByTextContaining("Insert spaces when pressing", gluttertest.FindOpts{FindRichText: true}))
		r.ts.Pump(pump)
		if got := r.read(r.userPath); !strings.Contains(got, `"editor.insertSpaces": true`) {
			t.Errorf("file %q", got)
		}
		r.noErrors()
	})
	t.Run("a number", func(t *testing.T) {
		r := newRig(t, rigOpts{userText: userText})
		r.show("editor.tabSize")
		row := rowOf("editor", "editor.tabSize")
		box := in[*kit.InputBox](row)
		r.ts.EnterText(box, "8")
		if got := r.read(r.userPath); !strings.Contains(got, `"editor.tabSize": 2`) {
			t.Errorf("the file changed before Enter: %q", got)
		}
		r.press(services.LogicalKeyboardKeyEnter)
		want := "// my settings\n{\n  // tabs\n  \"editor.tabSize\": 8\n}\n"
		if got := r.read(r.userPath); got != want {
			t.Errorf("file %q, want %q", got, want)
		}
		r.noErrors()
	})
	t.Run("a number commits when the box loses the focus", func(t *testing.T) {
		r := newRig(t, rigOpts{userText: userText})
		r.show("editor.tabSize")
		row := rowOf("editor", "editor.tabSize")
		r.ts.EnterText(in[*kit.InputBox](row), "6")
		// the focus goes to the search box
		r.ctl.FocusSearch()
		r.ts.Pump(pump)
		if got := r.read(r.userPath); !strings.Contains(got, `"editor.tabSize": 6`) {
			t.Errorf("file %q", got)
		}
		r.noErrors()
	})
	t.Run("a number that is not allowed writes nothing and says why", func(t *testing.T) {
		r := newRig(t, rigOpts{userText: userText})
		r.show("editor.tabSize")
		row := rowOf("editor", "editor.tabSize")
		for _, c := range []struct{ text, message string }{
			{"abc", "Value must be a number."},
			{"0", "Value must be greater than or equal to 1."},
			{"101", "Value must be less than or equal to 100."},
		} {
			r.ts.EnterText(in[*kit.InputBox](row), c.text)
			r.press(services.LogicalKeyboardKeyEnter)
			if !r.ts.Exists(gluttertest.Descendant(row, gluttertest.ByText(c.message))) {
				t.Errorf("%q: no message %q", c.text, c.message)
			}
			if got := r.read(r.userPath); got != userText {
				t.Errorf("%q: file %q", c.text, got)
			}
		}
		// a good value clears the message
		r.ts.EnterText(in[*kit.InputBox](row), "4")
		r.press(services.LogicalKeyboardKeyEnter)
		if r.ts.Exists(gluttertest.Descendant(row, gluttertest.ByText("Value must be a number."))) {
			t.Error("the message stays")
		}
		if got := r.read(r.userPath); !strings.Contains(got, `"editor.tabSize": 4`) {
			t.Errorf("file %q", got)
		}
		r.noErrors()
	})
	t.Run("a string", func(t *testing.T) {
		r := newRig(t, rigOpts{userText: userText})
		r.show("editor.fontFamily")
		row := rowOf("editor/font", "editor.fontFamily")
		r.ts.EnterText(in[*kit.InputBox](row), "Fira Code, monospace")
		r.press(services.LogicalKeyboardKeyEnter)
		want := "// my settings\n{\n  // tabs\n  \"editor.tabSize\": 2,\n  \"editor.fontFamily\": \"Fira Code, monospace\"\n}\n"
		if got := r.read(r.userPath); got != want {
			t.Errorf("file %q, want %q", got, want)
		}
		r.noErrors()
	})
	t.Run("an enum", func(t *testing.T) {
		r := newRig(t, rigOpts{userText: userText})
		r.show("files.autoSave")
		row := rowOf("editor/files", "files.autoSave")
		r.ts.Tap(in[*kit.Dropdown](row))
		r.ts.Pump(pump)
		if !r.ts.Exists(gluttertest.ByType[*kit.ListOverlay]()) {
			t.Fatal("the list did not open")
		}
		r.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.ListOverlay](), gluttertest.ByText("afterDelay")))
		r.ts.Pump(pump)
		want := "// my settings\n{\n  // tabs\n  \"editor.tabSize\": 2,\n  \"files.autoSave\": \"afterDelay\"\n}\n"
		if got := r.read(r.userPath); got != want {
			t.Errorf("file %q, want %q", got, want)
		}
		if d := r.ts.Widget(in[*kit.Dropdown](row)).(*kit.Dropdown); d.Options()[d.Selected()].Label != "afterDelay" {
			t.Errorf("selected %d", d.Selected())
		}
		r.noErrors()
	})
	t.Run("an array and an object link to the JSON", func(t *testing.T) {
		r := newRig(t, rigOpts{userText: userText})
		r.show("files.associations")
		row := rowOf("editor/files", "files.associations")
		r.ts.Tap(gluttertest.Descendant(row, gluttertest.ByText(editInJSONLabel)))
		r.ts.Pump(pump)
		if len(r.opened) != 1 || r.opened[0] != (openCall{settings.TargetUser, "files.associations"}) {
			t.Errorf("opened %v", r.opened)
		}
		if got := r.read(r.userPath); got != userText {
			t.Errorf("file %q", got)
		}
		r.noErrors()
	})
}

func TestModifiedMarkAndReset(t *testing.T) {
	const userText = "// header\n{\n  \"files.eol\": \"\\n\",\n  \"editor.tabSize\": 2,\n  // keep this\n  \"editor.insertSpaces\": false\n}\n"
	r := newRig(t, rigOpts{userText: userText})
	r.show("editor.tabSize")
	row := rowOf("editor", "editor.tabSize")
	other := rowOf("editor", "editor.detectIndentation")
	modified := func(f gluttertest.Finder) bool { return r.ts.Exists(gluttertest.Descendant(f, r.indicator())) }
	if !modified(row) || modified(other) {
		t.Fatalf("modified marks: tabSize %v, detectIndentation %v", modified(row), modified(other))
	}
	// the gear shows while the pointer is over the row, and its menu resets the setting
	if r.ts.Exists(in[*kit.IconButton](row)) {
		t.Error("the gear shows with no pointer over the row")
	}
	rect := r.rect(row)
	r.ts.Hover(ui.Offset{Dx: rect.Left + 100, Dy: rect.Top + 10})
	r.ts.Pump(pump)
	if !r.ts.Exists(in[*kit.IconButton](row)) {
		t.Fatal("no gear on the hovered row")
	}
	r.ts.Tap(in[*kit.IconButton](row))
	r.ts.Pump(pump)
	for _, label := range []string{menuResetLabel, menuCopyIDLabel, menuCopyJSONLabel} {
		if !r.ts.Exists(gluttertest.ByText(label)) {
			t.Errorf("no menu item %q", label)
		}
	}
	r.ts.Tap(gluttertest.ByText(menuCopyIDLabel))
	r.ts.Pump(pump)
	r.ts.Hover(ui.Offset{Dx: rect.Left + 100, Dy: rect.Top + 10})
	r.ts.Pump(pump)
	r.ts.Tap(in[*kit.IconButton](row))
	r.ts.Pump(pump)
	r.ts.Tap(gluttertest.ByText(menuCopyJSONLabel))
	r.ts.Pump(pump)
	if len(r.copied) != 2 || r.copied[0] != "editor.tabSize" || r.copied[1] != `"editor.tabSize": 2` {
		t.Errorf("copied %q", r.copied)
	}
	r.ts.Hover(ui.Offset{Dx: rect.Left + 100, Dy: rect.Top + 10})
	r.ts.Pump(pump)
	r.ts.Tap(in[*kit.IconButton](row))
	r.ts.Pump(pump)
	r.ts.Tap(gluttertest.ByText(menuResetLabel))
	r.ts.Pump(pump)
	want := "// header\n{\n  \"files.eol\": \"\\n\",\n  // keep this\n  \"editor.insertSpaces\": false\n}\n"
	if got := r.read(r.userPath); got != want {
		t.Errorf("file %q, want %q", got, want)
	}
	if modified(row) {
		t.Error("the mark stays after the reset")
	}
	if got := r.ts.Widget(in[*kit.InputBox](row)); got == nil {
		t.Fatal("no input")
	}
	r.noErrors()
}

func TestWorkspaceTab(t *testing.T) {
	t.Run("it writes the workspace file", func(t *testing.T) {
		r := newRig(t, rigOpts{hasFolder: true, userText: "{\n  \"editor.tabSize\": 2\n}\n", wsText: "{\n  \"editor.insertSpaces\": false\n}\n"})
		r.ts.Tap(gluttertest.ByText(WorkspaceTabLabel))
		r.ts.Pump(pump)
		if got := r.ctl.ViewState().Target; got != editorgroup.SettingsWorkspace {
			t.Fatalf("target %q", got)
		}
		r.show("editor.tabSize")
		row := rowOf("editor", "editor.tabSize")
		// the workspace does not have it: the default shows, and the user's value is named
		if !r.hasText(row, "Also modified in: User") {
			t.Error("the row does not say that the user file sets it")
		}
		r.ts.EnterText(in[*kit.InputBox](row), "3")
		r.press(services.LogicalKeyboardKeyEnter)
		if got := r.read(r.wsPath); got != "{\n  \"editor.insertSpaces\": false,\n  \"editor.tabSize\": 3\n}\n" {
			t.Errorf("workspace file %q", got)
		}
		if got := r.read(r.userPath); got != "{\n  \"editor.tabSize\": 2\n}\n" {
			t.Errorf("user file %q", got)
		}
		// an application setting is not listed
		r.show("window.restoreWindows")
		if r.ts.Exists(rowOf("features/../window", "window.restoreWindows")) || r.ts.Exists(gluttertest.ByTextContaining("Restore Windows", gluttertest.FindOpts{FindRichText: true})) {
			t.Error("an application setting is in the Workspace tab")
		}
		r.ts.Tap(gluttertest.ByText(UserTabLabel))
		r.ts.Pump(pump)
		if !r.ts.Exists(gluttertest.ByTextContaining("Restore Windows", gluttertest.FindOpts{FindRichText: true})) {
			t.Error("the application setting is not in the User tab")
		}
		r.noErrors()
	})
	t.Run("it is disabled without a folder", func(t *testing.T) {
		r := newRig(t, rigOpts{})
		r.ts.Tap(gluttertest.ByText(WorkspaceTabLabel))
		r.ts.Pump(pump)
		if got := r.ctl.ViewState().Target; got != editorgroup.SettingsUser {
			t.Errorf("target %q", got)
		}
		r.ctl.SetTarget(editorgroup.SettingsWorkspace)
		if got := r.ctl.ViewState().Target; got != editorgroup.SettingsUser {
			t.Errorf("the controller switched to the workspace: %q", got)
		}
		// the tooltip tells why
		rect := r.rect(gluttertest.ByText(WorkspaceTabLabel))
		r.ts.Hover(ui.Offset{Dx: rect.Left + 5, Dy: rect.Top + 5})
		r.ts.Pump(2 * time.Second)
		if !r.ts.Exists(gluttertest.ByText(NoWorkspaceTooltip)) {
			t.Error("no tooltip")
		}
		r.noErrors()
	})
	t.Run("a saved view of the workspace without a folder shows the user tab", func(t *testing.T) {
		r := newRig(t, rigOpts{state: editorgroup.SettingsViewState{Target: editorgroup.SettingsWorkspace}})
		if got := r.ctl.ViewState().Target; got != editorgroup.SettingsUser {
			t.Errorf("target %q", got)
		}
	})
}

// hasText tells whether a RichText below the row has the text in its spans.
func (r *rig) hasText(row gluttertest.Finder, text string) bool {
	for _, w := range r.ts.Widgets(in[*widgets.RichText](row)) {
		if strings.Contains(w.(*widgets.RichText).PlainText(), text) {
			return true
		}
	}
	return false
}

func TestCategoriesAndScroll(t *testing.T) {
	r := newRig(t, rigOpts{})
	// the tree has the groups of the layout, in order
	var labels []string
	for _, id := range []string{"Commonly Used", "Text Editor", "Workbench", "Window", "Features", "Extensions"} {
		if !r.ts.Exists(gluttertest.Descendant(gluttertest.ByType[*kit.Tree](), gluttertest.ByText(id))) {
			labels = append(labels, id)
		}
	}
	if len(labels) != 0 {
		t.Errorf("the tree lacks %v", labels)
	}
	if got := r.ctl.ViewState().Category; got != "" {
		t.Errorf("category %q before a choice", got)
	}
	// a click on a group scrolls the list to its header
	r.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Tree](), gluttertest.ByText("Window")))
	r.ts.Pump(pump)
	r.ts.Pump(pump)
	st := r.ctl.ViewState()
	if st.Category != "window" || st.Scroll <= 0 {
		t.Fatalf("view after the click: %+v", st)
	}
	header := r.rect(gluttertest.ByKey(groupKey("window")))
	if header.Top < 0 || header.Top > 200 {
		t.Errorf("the header of Window is at %v", header.Top)
	}
	if len(r.states) == 0 || r.states[len(r.states)-1].Category != "window" {
		t.Errorf("states %v", r.states)
	}
	// scrolling up with the wheel moves the selection of the tree to the group at the top
	r.ts.ScrollAt(ui.Offset{Dx: 600, Dy: 400}, ui.Offset{Dx: 0, Dy: -1500})
	r.ts.Pump(time.Second)
	r.ts.Pump(pump)
	r.ts.Pump(pump)
	after := r.ctl.ViewState()
	if after.Category == "window" || after.Category == "" || after.Scroll >= st.Scroll {
		t.Errorf("view after scrolling: %+v (was %+v)", after, st)
	}
	if sel, _ := r.selectedGroup(); sel != after.Category {
		t.Errorf("the tree selects %q, the list is at %q", sel, after.Category)
	}
	r.noErrors()
}

func TestSearch(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.show("tab size")
	if !r.ts.Exists(rowOf("editor", "editor.tabSize")) || r.ts.Exists(rowOf("editor", "editor.insertSpaces")) {
		t.Error("the list is not filtered to the settings that match")
	}
	// a search lists a setting once: not in Commonly Used too
	if r.ts.Exists(rowOf("commonlyUsed", "editor.tabSize")) {
		t.Error("Commonly Used is listed in a search")
	}
	if r.ctl.ViewState().Query != "tab size" {
		t.Errorf("state %+v", r.ctl.ViewState())
	}
	r.show("zzzzzzzz")
	if !r.ts.Exists(gluttertest.ByText(NoResultsText)) {
		t.Error("no message for an empty result")
	}
	r.show("")
	if !r.ts.Exists(rowOf("commonlyUsed", "editor.fontSize")) {
		t.Error("the list is not back")
	}
	// the box has the typed text
	r.ts.EnterText(gluttertest.ByType[*kit.InputBox](), "font size")
	r.ts.Pump(pump)
	if !r.ts.Exists(rowOf("editor/font", "editor.fontSize")) {
		t.Error("typing in the box does not filter")
	}
	r.noErrors()
}

func TestFollowsTheStore(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.show("editor.tabSize")
	row := rowOf("editor", "editor.tabSize")
	value := func() string {
		w := r.ts.Widgets(gluttertest.Descendant(row, gluttertest.ByType[*widgets.EditableText]()))
		if len(w) == 0 {
			t.Fatal("no text")
		}
		return w[0].(*widgets.EditableText).Controller().Text()
	}
	if value() != "4" {
		t.Fatalf("value %q", value())
	}
	// something else changes the setting: the row follows, with the mark
	if err := r.store.Update(t.Context(), "editor.tabSize", 7, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	r.ts.FlushPosted()
	r.ts.Pump(pump)
	if value() != "7" {
		t.Errorf("value %q after an update", value())
	}
	if !r.ts.Exists(gluttertest.Descendant(row, r.indicator())) {
		t.Error("no mark after the update")
	}
	// and the file edited by hand
	r.write(r.userPath, "{\n  \"editor.tabSize\": 3\n}\n")
	if _, err := r.store.Reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.ts.FlushPosted()
	r.ts.Pump(pump)
	if value() != "3" {
		t.Errorf("value %q after a reload", value())
	}
	r.noErrors()
}

func TestViewStateIsRestored(t *testing.T) {
	r := newRig(t, rigOpts{state: editorgroup.SettingsViewState{Category: "window", Query: ""}})
	r.ts.Pump(pump)
	r.ts.Pump(pump)
	r.ts.Pump(pump)
	if got := r.ctl.ViewState(); got.Category != "window" {
		t.Errorf("state %+v", got)
	}
	header := r.rect(gluttertest.ByKey(groupKey("window")))
	if header.Top < 0 || header.Top > 200 {
		t.Errorf("the list is not scrolled to Window: the header is at %v", header.Top)
	}
	r2 := newRig(t, rigOpts{state: editorgroup.SettingsViewState{Query: "tab size"}})
	if !r2.ts.Exists(rowOf("editor", "editor.tabSize")) || r2.ts.Exists(rowOf("editor", "editor.insertSpaces")) {
		t.Error("the search is not restored")
	}
}

func TestOpenJSONButton(t *testing.T) {
	r := newRig(t, rigOpts{hasFolder: true})
	r.ts.Tap(gluttertest.ByType[*kit.IconButton]())
	r.ts.Pump(pump)
	r.ctl.SetTarget(editorgroup.SettingsWorkspace)
	r.ts.Pump(pump)
	r.ts.Tap(gluttertest.First(gluttertest.ByType[*kit.IconButton]()))
	r.ts.Pump(pump)
	if len(r.opened) != 2 || r.opened[0] != (openCall{settings.TargetUser, ""}) || r.opened[1] != (openCall{settings.TargetWorkspace, ""}) {
		t.Errorf("opened %v", r.opened)
	}
}

func TestWriteErrorsAreReported(t *testing.T) {
	r := newRig(t, rigOpts{userText: "{ \"editor.tabSize\": "})
	r.show("editor.insertSpaces")
	r.ts.Tap(in[*kit.Checkbox](rowOf("editor", "editor.insertSpaces")))
	r.ts.Pump(pump)
	if len(r.errs) != 1 {
		t.Fatalf("errors %v", r.errs)
	}
	if got := r.read(r.userPath); got != "{ \"editor.tabSize\": " {
		t.Errorf("the file was written: %q", got)
	}
}

// rect is where the widget of a finder is, in the window.
func (r *rig) rect(f gluttertest.Finder) ui.Rect {
	at, size := r.ts.TopLeft(f), r.ts.Size(f)
	return ui.NewRectFromLTWH(at.Dx, at.Dy, size.Width, size.Height)
}

// indicator finds the mark of a modified setting: a thin box in the color of the indicator.
func (r *rig) indicator() gluttertest.Finder {
	want := kit.DarkModern().SettingsModifiedItemIndicator
	return gluttertest.ByPredicate("the modified mark", func(w widgets.Widget) bool {
		b, ok := w.(*widgets.ColoredBox)
		return ok && b.Color() == want
	})
}

// selectedGroup is the key of the group the tree has selected.
func (r *rig) selectedGroup() (string, bool) {
	return r.ctl.state.tree.Selected()
}
