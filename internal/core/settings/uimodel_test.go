package settings

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestWordifyKey(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"editor", "Editor"},
		{"fontSize", "Font Size"},
		{"editor.minimap", "Editor › Minimap"},
		{"workbench.editor", "Workbench › Editor"},
		{"tabSize", "Tab Size"},
		{"zoomLevel", "Zoom Level"},
		{"AISearch", "AI Search"},
		{"json.format", "JSON › Format"},
		{"openFoldersInNewWindow", "Open Folders In New Window"},
		{"typescript", "TypeScript"},
		{"githubIssues", "GitHub Issues"},
		{"files", "Files"},
		{"", ""},
	} {
		if got := WordifyKey(c.in); got != c.want {
			t.Errorf("WordifyKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestKeyToDisplayFormat(t *testing.T) {
	for _, c := range []struct{ key, group, category, label string }{
		{"editor.fontSize", "", "Editor", "Font Size"},
		{"files.autoSave", "", "Files", "Auto Save"},
		{"workbench.editor.showTabs", "", "Workbench › Editor", "Show Tabs"},
		{"editor.tabSize", "", "Editor", "Tab Size"},
		{"window.zoomLevel", "", "Window", "Zoom Level"},
		{"editor.minimap.maxColumn", "", "Editor › Minimap", "Max Column"},
		{"emmet.triggerExpansionOnTab", "", "Emmet", "Trigger Expansion On Tab"},
		{"zoom", "", "", "Zoom"},
		// the group says part of the category already
		{"editor.fontSize", "editor/font", "", "Font Size"},
		{"editor.fontSize", "editor", "", "Font Size"},
		{"editor.minimap.maxColumn", "editor/minimap", "", "Max Column"},
		{"workbench.editor.showTabs", "workbench/editor", "", "Show Tabs"},
		{"workbench.editor.showTabs", "workbench", "Editor", "Show Tabs"},
		{"files.autoSave", "editor/files", "", "Auto Save"},
		{"explorer.openEditors.visible", "features/explorer", "Open Editors", "Visible"},
		{"editor.fontSize", "commonlyUsed", "Editor", "Font Size"},
	} {
		category, label := KeyToDisplayFormat(c.key, c.group)
		if category != c.category || label != c.label {
			t.Errorf("KeyToDisplayFormat(%q, %q) = %q, %q; want %q, %q", c.key, c.group, category, label, c.category, c.label)
		}
	}
	if Title("Editor", "Font Size") != "Editor: Font Size" || Title("", "Font Size") != "Font Size" {
		t.Error("Title")
	}
}

func TestBuildCategories(t *testing.T) {
	settings := []Setting{
		{Key: "editor.fontSize"}, {Key: "editor.fontFamily"}, {Key: "editor.cursorStyle"}, {Key: "editor.tabSize"},
		{Key: "editor.minimap.enabled"}, {Key: "editor.quickSuggestions"}, {Key: "editor.find.seedSearchStringFromSelection"},
		{Key: "files.autoSave"}, {Key: "files.exclude"}, {Key: "workbench.colorTheme"}, {Key: "workbench.editor.showTabs"},
		{Key: "workbench.settings.editor"}, {Key: "window.zoomLevel"}, {Key: "window.openFoldersInNewWindow"},
		{Key: "explorer.autoReveal"}, {Key: "search.exclude"}, {Key: "emmet.variables"}, {Key: "breadcrumbs.enabled"},
	}
	tree := BuildCategories(DefaultTOC(), settings)
	type node struct {
		ID   string
		Keys []string
		Kids []node
	}
	var conv func(c *Category) node
	conv = func(c *Category) node {
		n := node{ID: c.ID, Keys: c.Keys}
		for _, ch := range c.Children {
			n.Kids = append(n.Kids, conv(ch))
		}
		return n
	}
	var got []node
	for _, c := range tree {
		got = append(got, conv(c))
	}
	want := []node{
		{ID: "commonlyUsed", Keys: []string{"editor.fontSize", "files.autoSave", "editor.fontFamily", "files.exclude", "workbench.colorTheme", "editor.tabSize"}},
		{ID: "editor", Keys: []string{"editor.tabSize"}, Kids: []node{
			{ID: "editor/cursor", Keys: []string{"editor.cursorStyle"}},
			{ID: "editor/find", Keys: []string{"editor.find.seedSearchStringFromSelection"}},
			{ID: "editor/font", Keys: []string{"editor.fontFamily", "editor.fontSize"}},
			{ID: "editor/minimap", Keys: []string{"editor.minimap.enabled"}},
			{ID: "editor/suggestions", Keys: []string{"editor.quickSuggestions"}},
			{ID: "editor/files", Keys: []string{"files.autoSave", "files.exclude"}},
		}},
		{ID: "workbench", Kids: []node{
			{ID: "workbench/appearance", Keys: []string{"workbench.colorTheme"}},
			{ID: "workbench/breadcrumbs", Keys: []string{"breadcrumbs.enabled"}},
			{ID: "workbench/editor", Keys: []string{"workbench.editor.showTabs"}},
			{ID: "workbench/settings", Keys: []string{"workbench.settings.editor"}},
		}},
		{ID: "window", Keys: []string{"window.zoomLevel"}, Kids: []node{
			{ID: "window/newWindow", Keys: []string{"window.openFoldersInNewWindow"}},
		}},
		{ID: "features", Kids: []node{
			{ID: "features/explorer", Keys: []string{"explorer.autoReveal"}},
			{ID: "features/search", Keys: []string{"search.exclude"}},
		}},
		{ID: "extensions", Kids: []node{{ID: "extensions/emmet", Keys: []string{"emmet.variables"}}}},
	}
	// workbench.colorTheme is in Commonly Used and in Appearance: the first group is a copy
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tree\n got %+v\nwant %+v", got, want)
	}
	if tree[1].Count() != 9 || tree[1].Find("editor/font") == nil || tree[1].Find("nope") != nil {
		t.Errorf("Count %d", tree[1].Count())
	}
	var ids []string
	tree[1].Walk(func(c *Category) { ids = append(ids, c.ID) })
	if ids[0] != "editor" || ids[1] != "editor/cursor" || len(ids) != 7 {
		t.Errorf("walk %v", ids)
	}
	if tree[1].Children[0].Level != 2 || tree[1].Level != 1 {
		t.Error("levels")
	}
}

func TestDefaultRegistryIsPlaced(t *testing.T) {
	r := DefaultRegistry()
	tree := BuildCategories(DefaultTOC(), r.All())
	placed := map[string]int{}
	for _, c := range tree {
		if c.ID == CommonlyUsedID {
			continue
		}
		c.Walk(func(c *Category) {
			for _, k := range c.Keys {
				placed[k]++
			}
		})
	}
	for _, s := range r.All() {
		if placed[s.Key] != 1 {
			t.Errorf("%s is placed %d times", s.Key, placed[s.Key])
		}
	}
	for _, c := range tree {
		if c.Count() == 0 {
			t.Errorf("empty group %s", c.ID)
		}
	}
}

func TestDiffEditorSettingsAreInTheirGroup(t *testing.T) {
	r := DefaultRegistry()
	var found []string
	for _, c := range BuildCategories(DefaultTOC(), r.All()) {
		c.Walk(func(c *Category) {
			if c.ID == "editor/diffEditor" {
				found = append(found, c.Keys...)
			}
		})
	}
	if !slices.Equal(found, []string{"diffEditor.ignoreTrimWhitespace", "diffEditor.maxComputationTime", "diffEditor.renderSideBySide", "diffEditor.renderSideBySideInlineBreakpoint", "diffEditor.useInlineViewWhenSpaceIsLimited"}) {
		t.Errorf("the Diff Editor group holds %v", found)
	}
}

func uiFixture(t *testing.T) (*fixture, *UIModel) {
	t.Helper()
	f := newFixture(t)
	return f, NewUIModel(f.store)
}

func TestModelVisibility(t *testing.T) {
	f, m := uiFixture(t)
	keys := func(target Target, match func(Row) bool) []string {
		var out []string
		for _, c := range m.Categories(target, match) {
			c.Walk(func(c *Category) { out = append(out, c.Keys...) })
		}
		return out
	}
	user := keys(TargetUser, nil)
	ws := keys(TargetWorkspace, nil)
	if !slices.Contains(user, "test.app") || slices.Contains(ws, "test.app") {
		t.Error("an application setting is in the User tab only")
	}
	if !slices.Contains(user, "window.restoreWindows") || slices.Contains(ws, "window.restoreWindows") {
		t.Error("window.restoreWindows is application scoped")
	}
	if !slices.Contains(ws, "editor.tabSize") || !slices.Contains(ws, "files.exclude") {
		t.Error("window and resource settings are in the Workspace tab")
	}
	// the search
	found := keys(TargetUser, func(r Row) bool { return r.Key == "editor.fontSize" })
	if !slices.Contains(found, "editor.fontSize") || slices.Contains(found, "editor.tabSize") {
		t.Errorf("found %v", found)
	}
	// a new registration appears
	f.store.Registry().MustRegister(Setting{Key: "zzz.new", Types: []Type{Boolean}})
	if !slices.Contains(keys(TargetUser, nil), "zzz.new") {
		t.Error("the new setting is missing")
	}
}

func TestModelRows(t *testing.T) {
	ctx := context.Background()
	f, m := uiFixture(t)
	f.write(f.userPath, "{\n  // mine\n  \"editor.tabSize\": 2,\n  \"files.eol\": \"bogus\"\n}\n")
	f.write(f.wsPath, "{\n  \"editor.tabSize\": 6\n}\n")
	f.reload()
	kinds := []struct {
		key  string
		kind RowKind
	}{
		{"editor.insertSpaces", KindBool},
		{"editor.tabSize", KindNumber},
		{"test.size", KindInteger},
		{"files.encoding", KindEnum},
		{"editor.fontFamily", KindString},
		{"test.list", KindComplex},
		{"test.obj", KindComplex},
		{"files.associations", KindComplex},
		{"explorer.autoReveal", KindEnum},
		{"workbench.iconTheme", KindComplex},
		{"test.mode", KindEnum},
	}
	for _, c := range kinds {
		row, ok := m.Row(c.key, TargetUser, "")
		if !ok || row.Kind != c.kind {
			t.Errorf("%s: kind %v ok %v, want %v", c.key, row.Kind, ok, c.kind)
		}
	}
	if _, ok := m.Row("nope.nope", TargetUser, ""); ok {
		t.Error("an unregistered key has a row")
	}
	user, _ := m.Row("editor.tabSize", TargetUser, "")
	ws, _ := m.Row("editor.tabSize", TargetWorkspace, "")
	if !user.Modified || user.Value != 2.0 || user.Default != 4.0 || !reflect.DeepEqual(user.AlsoModifiedIn, []string{"Workspace"}) {
		t.Errorf("user row %+v", user)
	}
	if !ws.Modified || ws.Value != 6.0 || !reflect.DeepEqual(ws.AlsoModifiedIn, []string{"User"}) {
		t.Errorf("workspace row %+v", ws)
	}
	if user.Title() != "Editor: Tab Size" {
		t.Errorf("title %q", user.Title())
	}
	plain, _ := m.Row("editor.insertSpaces", TargetUser, "")
	if plain.Modified || plain.Value != true || len(plain.AlsoModifiedIn) != 0 || !plain.Checked() {
		t.Errorf("plain %+v", plain)
	}
	// a value the setting does not allow shows the default and the reason
	bad, _ := m.Row("files.eol", TargetUser, "")
	if !bad.Modified || bad.Invalid == "" || bad.Value != bad.Default {
		t.Errorf("invalid row %+v", bad)
	}
	// enum choices and their descriptions
	eol, _ := m.Row("files.eol", TargetUser, "")
	if len(eol.Choices) != 3 || eol.Choices[0].Label != "\n" || eol.Choices[0].Description != "LF" || eol.ChoiceIndex() != 2 {
		t.Errorf("eol choices %+v idx %d", eol.Choices, eol.ChoiceIndex())
	}
	auto, _ := m.Row("explorer.autoReveal", TargetUser, "")
	if got := []string{auto.Choices[0].Label, auto.Choices[1].Label}; got[0] != "true" || got[1] != "false" {
		t.Errorf("labels %v", got)
	}
	// the group trims the title
	font, _ := m.Row("editor.fontSize", TargetUser, "editor/font")
	if font.Title() != "Font Size" {
		t.Errorf("title %q", font.Title())
	}
	_ = ctx
}

func TestRowParse(t *testing.T) {
	_, m := uiFixture(t)
	for _, c := range []struct {
		name, key, text string
		want            any
		msg             string
	}{
		{"number", "editor.tabSize", "8", 8.0, ""},
		{"number with spaces", "editor.tabSize", " 3 ", 3.0, ""},
		{"fraction", "editor.tabSize", "2.5", 2.5, ""},
		{"not a number", "editor.tabSize", "abc", nil, "Value must be a number."},
		{"empty", "editor.tabSize", "", nil, "Value must be a number."},
		{"too small", "editor.tabSize", "0", nil, "Value must be greater than or equal to 1."},
		{"too big", "editor.tabSize", "101", nil, "Value must be less than or equal to 100."},
		{"integer", "test.size", "4", 4.0, ""},
		{"not an integer", "test.size", "4.5", nil, "Value must be an integer."},
		{"integer range", "test.size", "10", nil, "Value must be less than or equal to 9."},
		{"nan", "editor.tabSize", "NaN", nil, "Value must be a number."},
		{"string", "editor.fontFamily", "Fira Code", "Fira Code", ""},
		{"empty string", "editor.fontFamily", "", "", ""},
		{"complex", "test.obj", "x", nil, "This setting is edited in settings.json."},
	} {
		t.Run(c.name, func(t *testing.T) {
			row, _ := m.Row(c.key, TargetUser, "")
			got, msg := row.Parse(c.text)
			if msg != c.msg || !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, %q; want %v, %q", got, msg, c.want, c.msg)
			}
		})
	}
	row, _ := m.Row("editor.tabSize", TargetUser, "")
	if row.ValueText() != "4" {
		t.Errorf("text %q", row.ValueText())
	}
	row, _ = m.Row("editor.fontFamily", TargetUser, "")
	if row.ValueText() != "" {
		t.Errorf("text %q", row.ValueText())
	}
}

func TestModelEdits(t *testing.T) {
	ctx := context.Background()
	f, m := uiFixture(t)
	f.write(f.userPath, "// my settings\n{\n  // tabs\n  \"editor.tabSize\": 2\n}\n")
	f.reload()
	if err := m.SetText(ctx, "editor.fontFamily", TargetUser, "Fira Code"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetValue(ctx, "editor.insertSpaces", TargetUser, false); err != nil {
		t.Fatal(err)
	}
	want := "// my settings\n{\n  // tabs\n  \"editor.tabSize\": 2,\n  \"editor.fontFamily\": \"Fira Code\",\n  \"editor.insertSpaces\": false\n}\n"
	if got := f.read(f.userPath); got != want {
		t.Errorf("file %q, want %q", got, want)
	}
	row, _ := m.Row("editor.fontFamily", TargetUser, "")
	if !row.Modified || row.Value != "Fira Code" {
		t.Errorf("row %+v", row)
	}
	if err := m.Reset(ctx, "editor.fontFamily", TargetUser); err != nil {
		t.Fatal(err)
	}
	row, _ = m.Row("editor.fontFamily", TargetUser, "")
	if row.Modified || row.Value != "" {
		t.Errorf("row after reset %+v", row)
	}
	if got := f.read(f.userPath); got != "// my settings\n{\n  // tabs\n  \"editor.tabSize\": 2,\n  \"editor.insertSpaces\": false\n}\n" {
		t.Errorf("file %q", got)
	}
	// the workspace file
	if err := m.SetText(ctx, "editor.tabSize", TargetWorkspace, "3"); err != nil {
		t.Fatal(err)
	}
	if got := f.read(f.wsPath); got != "{\n\t\"editor.tabSize\": 3\n}" { // editor.insertSpaces is false in the user file
		t.Errorf("workspace file %q", got)
	}
}

func TestModelErrors(t *testing.T) {
	ctx := context.Background()
	f, m := uiFixture(t)
	f.write(f.userPath, "{}")
	cases := []struct {
		name  string
		do    func() error
		route string
	}{
		{"invalid input", func() error { return m.SetText(ctx, "editor.tabSize", TargetUser, "abc") }, "UIModel.SetText->UIModel.SetTextFor.SettingInputInvalid"},
		{"out of range", func() error { return m.SetText(ctx, "editor.tabSize", TargetUser, "0") }, "UIModel.SetText->UIModel.SetTextFor.SettingInputInvalid"},
		{"unregistered", func() error { return m.SetText(ctx, "nope.nope", TargetUser, "1") }, "UIModel.SetText->UIModel.SetTextFor.SettingUnregistered"},
		{"application setting in the workspace", func() error { return m.SetValue(ctx, "test.app", TargetWorkspace, true) }, "Store.write.SettingScopeInvalid"},
		{"no workspace", func() error {
			f.store.opts.WorkspacePath = ""
			return m.SetValue(ctx, "editor.tabSize", TargetWorkspace, 3)
		}, "Store.pathOf.SettingsNoWorkspace"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.do()
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(c.route) {
				t.Fatalf("got %v, want route %s", err, c.route)
			}
		})
	}
	if f.read(f.userPath) != "{}" {
		t.Error("a file was written")
	}
}

func TestRenderDescription(t *testing.T) {
	for _, c := range []struct {
		in   string
		want [][]Span
	}{
		{"plain", [][]Span{{{Text: "plain"}}}},
		{"Use `editor.tabSize` now", [][]Span{{{Text: "Use "}, {Text: "editor.tabSize", Code: true}, {Text: " now"}}}},
		{"See [the docs](https://x.y/z \"t\") for more", [][]Span{{{Text: "See "}, {Text: "the docs", Link: "https://x.y/z"}, {Text: " for more"}}}},
		{"a **bold** word", [][]Span{{{Text: "a bold word"}}}},
		{"one\ntwo `c`", [][]Span{{{Text: "one"}}, {{Text: "two "}, {Text: "c", Code: true}}}},
		{"[`x`](command:y)", [][]Span{{{Text: "`x`", Link: "command:y"}}}},
		{"as #files.exclude# says, and # not a ref", [][]Span{{{Text: "as "}, {Text: "files.exclude", Link: "#files.exclude"}, {Text: " says, and # not a ref"}}}},
		{"", [][]Span{nil}},
	} {
		got := RenderDescription(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("RenderDescription(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}
