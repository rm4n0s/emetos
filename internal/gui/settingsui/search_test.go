package settingsui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// found is the keys the model finds for a query, which the rows of the list must be.
func (r *rig) found(target settings.Target, query string) settings.SearchResult {
	return r.model.Search(target, query, testLanguages)
}

// rowsShown tells whether the list has a row for each key (in any group).
func (r *rig) hasRow(key string) bool {
	for _, c := range r.ctl.state.cats {
		var ok bool
		c.Walk(func(c *settings.Category) {
			for _, k := range c.Keys {
				if k == key && r.ts.Exists(rowOf(c.ID, k)) {
					ok = true
				}
			}
		})
		if ok {
			return true
		}
	}
	return false
}

func TestSearchShowsTheResultsAndTheCount(t *testing.T) {
	r := newRig(t, rigOpts{userText: "{\n  \"editor.fontSize\": 16\n}\n"})
	if r.ts.Exists(gluttertest.ByTextContaining("Found")) {
		t.Error("the count shows without a search")
	}
	r.show("font")
	res := r.found(settings.TargetUser, "font")
	if res.Count < 3 {
		t.Fatalf("only %d settings match font", res.Count)
	}
	if !r.ts.Exists(gluttertest.ByText(strconv.Itoa(res.Count)+" Settings Found")) || r.ts.Exists(gluttertest.ByText(NoResultsText)) {
		t.Errorf("no count of %d", res.Count)
	}
	if !r.hasRow("editor.fontSize") || !r.hasRow("editor.fontFamily") || r.hasRow("editor.tabSize") {
		t.Error("the rows are not those of the search")
	}
	// words in any order and in any case
	r.show("SIZE Font")
	if !r.hasRow("editor.fontSize") || r.hasRow("editor.fontFamily") {
		t.Error("two words")
	}
	r.show("@id:editor.fontSize")
	if !r.ts.Exists(gluttertest.ByText("1 Setting Found")) || !r.hasRow("editor.fontSize") {
		t.Error("one id")
	}
	r.show("zzzzqqq")
	if !r.ts.Exists(gluttertest.ByText("No Settings Found")) || !r.ts.Exists(gluttertest.ByText(NoResultsText)) {
		t.Error("the empty result is not said")
	}
	// the tree shows the counts of the groups
	r.show("font")
	font := res.Categories
	var n int
	for _, c := range font {
		if f := c.Find("editor/font"); f != nil {
			n = f.Count()
		}
	}
	if n == 0 || !r.ts.Exists(gluttertest.ByText("Font ("+strconv.Itoa(n)+")")) {
		t.Errorf("the tree has no \"Font (%d)\"", n)
	}
	r.show("")
	if r.ts.Exists(gluttertest.ByTextContaining("Font (")) || r.ts.Exists(gluttertest.ByTextContaining("Found")) {
		t.Error("the counts stay after the search is cleared")
	}
	r.noErrors()
}

func TestSearchModifiedAndIDs(t *testing.T) {
	r := newRig(t, rigOpts{hasFolder: true,
		userText: "{\n  // mine\n  \"editor.fontSize\": 16,\n  \"editor.tabSize\": 2\n}\n", wsText: "{\n  \"editor.wordWrap\": \"on\"\n}\n"})
	r.show("@modified")
	if !r.ts.Exists(gluttertest.ByText("2 Settings Found")) || !r.hasRow("editor.fontSize") || !r.hasRow("editor.tabSize") || r.hasRow("editor.wordWrap") || r.hasRow("editor.insertSpaces") {
		t.Error("@modified in the user tab")
	}
	for _, key := range []string{"editor.fontSize", "editor.tabSize"} {
		var row gluttertest.Finder
		r.ctl.state.eachCategory(func(c *settings.Category) {
			for _, k := range c.Keys {
				if k == key {
					row = rowOf(c.ID, k)
				}
			}
		})
		if !r.ts.Exists(gluttertest.Descendant(row, r.indicator())) {
			t.Errorf("%s has no mark", key)
		}
	}
	// the workspace tab has its own
	r.ctl.SetTarget(editorgroup.SettingsWorkspace)
	r.ts.Pump(pump)
	r.ts.Pump(pump)
	if !r.ts.Exists(gluttertest.ByText("1 Setting Found")) || !r.hasRow("editor.wordWrap") || r.hasRow("editor.fontSize") {
		t.Error("@modified in the workspace tab")
	}
	r.ctl.SetTarget(editorgroup.SettingsUser)
	r.show("@id:editor.tabSize,editor.wordWrap")
	if !r.hasRow("editor.tabSize") || !r.hasRow("editor.wordWrap") || r.hasRow("editor.fontSize") {
		t.Error("two ids")
	}
	r.show("@modified @id:editor.wordWrap")
	if !r.ts.Exists(gluttertest.ByText("No Settings Found")) {
		t.Error("an id that is not modified")
	}
	r.show("@tag:accessibility @id:editor.tabSize")
	if !r.ts.Exists(gluttertest.ByText("@tag:accessibility is not supported and was ignored.")) || !r.hasRow("editor.tabSize") {
		t.Error("the unsupported filter has no hint")
	}
	r.noErrors()
}

func TestSearchBoxClearsWithTheButtonAndEscape(t *testing.T) {
	r := newRig(t, rigOpts{})
	if r.ts.Exists(clearButton()) {
		t.Error("a clear button over an empty box")
	}
	r.ts.EnterText(gluttertest.ByType[*kit.InputBox](), "tab size")
	r.ts.Pump(pump)
	if !r.hasRow("editor.tabSize") || r.hasRow("editor.insertSpaces") || r.ctl.ViewState().Query != "tab size" {
		t.Fatal("typing does not search")
	}
	// the button clears it
	r.ts.Tap(clearButton())
	r.ts.Pump(pump)
	r.ts.Pump(pump)
	if r.ctl.ViewState().Query != "" || !r.ts.Exists(rowOf("commonlyUsed", "editor.fontSize")) {
		t.Errorf("the button leaves %q", r.ctl.ViewState().Query)
	}
	// so does Escape in the box
	r.show("font")
	r.ctl.FocusSearch()
	r.ts.Pump(pump)
	r.press(services.LogicalKeyboardKeyEscape)
	r.ts.Pump(pump)
	if r.ctl.ViewState().Query != "" || !r.ts.Exists(rowOf("commonlyUsed", "editor.fontSize")) {
		t.Errorf("Escape leaves %q", r.ctl.ViewState().Query)
	}
	// Escape in an empty box is left to others
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyEscape)
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyEscape)
	r.noErrors()
}

func TestSearchQueryIsRestoredFromTheState(t *testing.T) {
	for _, query := range []string{"@modified", "@lang:go tab", "@id:editor.tabSize"} {
		r := newRig(t, rigOpts{userText: "{\n  \"editor.tabSize\": 2\n}\n", state: editorgroup.SettingsViewState{Query: query}})
		r.ts.Pump(pump)
		if got := r.ctl.ViewState().Query; got != query {
			t.Errorf("the query is %q, want %q", got, query)
		}
		if !r.ts.Exists(gluttertest.ByTextContaining("Found")) {
			t.Errorf("%q: no count", query)
		}
		if got := r.ctl.state.search.Value(); got != query {
			t.Errorf("the box has %q", got)
		}
	}
}

func TestSearchInTheBlockOfALanguage(t *testing.T) {
	const userText = "// my settings\n{\n  \"editor.tabSize\": 2,\n  \"[go]\": {\n    // go is special\n    \"editor.tabSize\": 8\n  }\n}\n"
	r := newRig(t, rigOpts{userText: userText})
	r.show("@lang:go tab size")
	if !r.ts.Exists(gluttertest.ByText(LanguageHeaderPrefix + "Go")) {
		t.Fatal("no language header")
	}
	var rowCat string
	r.ctl.state.eachCategory(func(c *settings.Category) {
		for _, k := range c.Keys {
			if k == "editor.tabSize" {
				rowCat = c.ID
			}
		}
	})
	row := rowOf(rowCat, "editor.tabSize")
	if !r.ts.Exists(row) {
		t.Fatal("no row for the tab size")
	}
	// the block's value shows, with the mark; the plain value is not what the row says
	box := r.ts.Widgets(gluttertest.Descendant(row, gluttertest.ByType[*kit.InputBox]()))
	if len(box) != 1 || r.inputText(row) != "8" || !r.ts.Exists(gluttertest.Descendant(row, r.indicator())) {
		t.Errorf("the row shows %q", r.inputText(row))
	}
	// editing writes the block, and only the block
	r.ts.EnterText(in[*kit.InputBox](row), "3")
	r.press(services.LogicalKeyboardKeyEnter)
	got := r.read(r.userPath)
	want := "// my settings\n{\n  \"editor.tabSize\": 2,\n  \"[go]\": {\n    // go is special\n    \"editor.tabSize\": 3\n  }\n}\n"
	if got != want {
		t.Errorf("file %q, want %q", got, want)
	}
	// a setting the block does not have: a check box writes it into the block
	r.show("@lang:go @id:editor.insertSpaces")
	var cat string
	r.ctl.state.eachCategory(func(c *settings.Category) { cat = c.ID })
	insert := rowOf(cat, "editor.insertSpaces")
	if r.ts.Exists(gluttertest.Descendant(insert, r.indicator())) {
		t.Error("an unset setting has a mark")
	}
	r.ts.Tap(in[*kit.Checkbox](insert))
	r.ts.Pump(pump)
	got = r.read(r.userPath)
	if !strings.Contains(got, "// go is special") || !strings.Contains(got, `"editor.insertSpaces": false`) || !strings.Contains(got, `"editor.tabSize": 2,`) {
		t.Errorf("file %q", got)
	}
	if v := settings.Get[bool](r.store, "editor.insertSpaces", ""); !v {
		t.Error("the plain setting changed")
	}
	if v := settings.Get[bool](r.store, "editor.insertSpaces", "go"); v {
		t.Error("the setting of go did not change")
	}
	// Reset removes the key from the block, and the block with its last key
	for _, key := range []string{"editor.insertSpaces", "editor.tabSize"} {
		r.show("@lang:go @id:" + key)
		r.ctl.state.eachCategory(func(c *settings.Category) { cat = c.ID })
		row := rowOf(cat, key)
		rect := r.rect(row)
		r.ts.Hover(ui.Offset{Dx: rect.Left + 100, Dy: rect.Top + 10})
		r.ts.Pump(pump)
		r.ts.Tap(in[*kit.IconButton](row))
		r.ts.Pump(pump)
		r.ts.Tap(gluttertest.ByText(menuResetLabel))
		r.ts.Pump(pump)
	}
	got = r.read(r.userPath)
	if strings.Contains(got, "[go]") || !strings.Contains(got, "// my settings") || !strings.Contains(got, `"editor.tabSize": 2`) {
		t.Errorf("after the resets: %q", got)
	}
	r.noErrors()
}

func TestSearchForALanguageThatDoesNotExist(t *testing.T) {
	r := newRig(t, rigOpts{})
	r.show("@lang:zzz")
	if !r.ts.Exists(gluttertest.ByText("No Settings Found")) || !r.ts.Exists(gluttertest.ByText("There is no language \"zzz\".")) {
		t.Error("an unknown language is not said")
	}
	if r.ts.Exists(gluttertest.ByTextContaining(LanguageHeaderPrefix)) {
		t.Error("a language header for no language")
	}
	// the workspace tab has no block to show yet, but the language filter works
	r2 := newRig(t, rigOpts{hasFolder: true, state: editorgroup.SettingsViewState{Target: editorgroup.SettingsWorkspace, Query: "@lang:markdown"}})
	r2.ts.Pump(pump)
	if !r2.ts.Exists(gluttertest.ByText(LanguageHeaderPrefix + "Markdown")) {
		t.Error("no header in the workspace tab")
	}
}

func (r *rig) inputText(row gluttertest.Finder) string {
	w := r.ts.Widgets(gluttertest.Descendant(row, gluttertest.ByType[*widgets.EditableText]()))
	if len(w) == 0 {
		return ""
	}
	return w[0].(*widgets.EditableText).Controller().Text()
}

// clearButton finds the button that clears the search box.
func clearButton() gluttertest.Finder {
	return gluttertest.ByPredicate("the clear button", func(w widgets.Widget) bool {
		b, ok := w.(*kit.IconButton)
		return ok && b.Tooltip() == ClearSearchTooltip
	})
}
