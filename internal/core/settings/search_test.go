package settings

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestParseQuery(t *testing.T) {
	for _, c := range []struct {
		name, in string
		want     Query
	}{
		{"empty", "", Query{}},
		{"blanks", "   ", Query{}},
		{"words", "Font  Size", Query{Text: "Font Size", Words: []string{"font", "size"}}},
		{"modified", "@modified", Query{Modified: true}},
		{"modified and words", "tab @modified size", Query{Modified: true, Text: "tab size", Words: []string{"tab", "size"}}},
		{"modified inside a word is text", "x@modified", Query{Text: "x@modified", Words: []string{"x@modified"}}},
		{"id", "@id:editor.fontSize", Query{IDs: []string{"editor.fontSize"}}},
		{"ids with commas", "@id:editor.fontSize,editor.tabSize,,files.eol", Query{IDs: []string{"editor.fontSize", "editor.tabSize", "files.eol"}}},
		{"ids repeated", "@id:a.b @id:c.d", Query{IDs: []string{"a.b", "c.d"}}},
		{"id quoted", `@id:"a.b,c.d" tab`, Query{IDs: []string{"a.b", "c.d"}, Text: "tab", Words: []string{"tab"}}},
		{"id with nothing", "@id:", Query{}},
		{"id prefix", "@id:editor.font*", Query{IDs: []string{"editor.font*"}}},
		{"lang", "@lang:go", Query{Language: "go"}},
		{"lang with words", "@lang:go tab size", Query{Language: "go", Text: "tab size", Words: []string{"tab", "size"}}},
		{"only the first lang", "@lang:go @lang:json", Query{Language: "go"}},
		{"lang list takes the first", "@lang:json,go", Query{Language: "json"}},
		{"lang with nothing", "@lang:", Query{}},
		{"all together", "@modified @lang:go @id:editor.tabSize tab", Query{Modified: true, Language: "go", IDs: []string{"editor.tabSize"}, Text: "tab", Words: []string{"tab"}}},
		{"tag", "@tag:accessibility", Query{Unsupported: []string{"@tag:accessibility"}}},
		{"feature and ext", "@feature:scm @ext:foo.bar font", Query{Unsupported: []string{"@feature:scm", "@ext:foo.bar"}, Text: "font", Words: []string{"font"}}},
		{"policy and stable", "@hasPolicy @stable", Query{Unsupported: []string{"@hasPolicy", "@stable"}}},
		{"quotes and colons are cleaned", `"editor.fontSize":`, Query{Text: "editor.fontSize", Words: []string{"editor.fontsize"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := ParseQuery(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseQuery(%q)\n got %+v\nwant %+v", c.in, got, c.want)
			}
		})
	}
}

func TestQueryEmpty(t *testing.T) {
	for in, want := range map[string]bool{
		"": true, "  ": true, "@id:": true, "@lang:": true, "@tag:x": true, "@modified": false, "font": false, "@id:a": false, "@lang:go": false,
	} {
		if got := ParseQuery(in).Empty(); got != want {
			t.Errorf("ParseQuery(%q).Empty() = %v", in, got)
		}
	}
}

// languages is the resolver of the tests: go and json, found by id or by name.
func languages(name string) (id, display string, ok bool) {
	switch strings.ToLower(name) {
	case "go":
		return "go", "Go", true
	case "json":
		return "json", "JSON", true
	case "markdown":
		return "markdown", "Markdown", true
	}
	return "", "", false
}

// found is the keys of a result in the order the editor lists them.
func found(r SearchResult) []string {
	var out []string
	for _, c := range r.Categories {
		c.Walk(func(c *Category) { out = append(out, c.Keys...) })
	}
	return out
}

func TestSearch(t *testing.T) {
	f, m := uiFixture(t)
	f.write(f.userPath, "{\n  // mine\n  \"editor.fontSize\": 16,\n  \"editor.tabSize\": 2,\n  \"files.eol\": \"\\n\",\n  \"test.app\": true,\n  \"[go]\": {\n    \"editor.tabSize\": 8,\n    \"editor.insertSpaces\": false\n  }\n}\n")
	f.write(f.wsPath, "{\n  \"editor.tabSize\": 6,\n  \"editor.wordWrap\": \"on\"\n}\n")
	f.reload()
	type tc struct {
		name   string
		target Target
		query  string
		// in and out are keys that are, and are not, among the results
		in, out []string
		// exactly, when not nil, are all the results, in order
		exactly []string
		count   int // when not 0
		hints   int
		summary string
	}
	cases := []tc{
		{name: "a word in the key", query: "font", in: []string{"editor.fontSize", "editor.fontFamily"}, out: []string{"editor.tabSize", "files.eol"}},
		{name: "upper case", query: "FONT", in: []string{"editor.fontSize"}, out: []string{"editor.tabSize"}},
		{name: "two words, any order", query: "size font", in: []string{"editor.fontSize"}, out: []string{"editor.fontFamily", "editor.tabSize"}},
		{name: "camel case words", query: "font size", in: []string{"editor.fontSize"}, out: []string{"editor.fontFamily"}},
		{name: "the key without dots and case", query: "editorfontsize", exactly: []string{"editor.fontSize"}},
		{name: "the whole key", query: "editor.fontSize", in: []string{"editor.fontSize"}},
		{name: "a word of the title", query: "auto save", in: []string{"files.autoSave"}},
		{name: "a word of an enum value", query: "afterDelay", in: []string{"files.autoSave"}, out: []string{"editor.fontSize"}},
		{name: "a word of the description", query: "pixels", in: []string{"editor.fontSize"}, out: []string{"editor.fontFamily"}},
		{name: "no setting", query: "zzzzqqq", exactly: []string{}, count: 0, summary: "No Settings Found"},
		{name: "one word missing", query: "font zzzzqqq", exactly: []string{}},
		{name: "quotes and a colon are ignored", query: `"editor.fontSize":`, in: []string{"editor.fontSize"}},
		{name: "modified in the user file", query: "@modified", exactly: []string{"editor.fontSize", "editor.tabSize", "files.eol", "test.app"}, count: 4, summary: "4 Settings Found"},
		{name: "modified in the workspace file", target: TargetWorkspace, query: "@modified", exactly: []string{"editor.tabSize", "editor.wordWrap"}},
		{name: "modified and a word", query: "@modified tab", exactly: []string{"editor.tabSize"}, count: 1, summary: "1 Setting Found"},
		{name: "modified and a word that is not modified", query: "@modified word", exactly: []string{}},
		{name: "one id", query: "@id:editor.fontSize", exactly: []string{"editor.fontSize"}},
		{name: "ids", query: "@id:files.eol,editor.fontSize", exactly: []string{"editor.fontSize", "files.eol"}},
		{name: "two id filters", query: "@id:files.eol @id:editor.tabSize", exactly: []string{"editor.tabSize", "files.eol"}},
		{name: "an id is exact", query: "@id:editor.font", exactly: []string{}},
		{name: "an id is case sensitive", query: "@id:editor.fontsize", exactly: []string{}},
		{name: "a prefix id", query: "@id:editor.font*", in: []string{"editor.fontSize", "editor.fontFamily"}, out: []string{"editor.tabSize"}},
		{name: "an id of the other tab only", target: TargetWorkspace, query: "@id:test.app", exactly: []string{}},
		{name: "an id in the user tab", query: "@id:test.app", exactly: []string{"test.app"}},
		{name: "id and modified", query: "@id:editor.fontSize,editor.wordWrap @modified", exactly: []string{"editor.fontSize"}},
		{name: "id and a word", query: "@id:editor.fontSize,editor.fontFamily family", exactly: []string{"editor.fontFamily"}},
		{name: "a filter alone lists the rest", query: "@id:editor.tabSize @id:", exactly: []string{"editor.tabSize"}},
		{name: "unsupported filters are ignored with a hint", query: "@tag:accessibility font", in: []string{"editor.fontSize"}, hints: 1},
		{name: "an unsupported filter alone finds every setting", query: "@feature:scm", in: []string{"editor.fontSize", "files.eol"}, hints: 1},
		{name: "the language settings", query: "@lang:go", in: []string{"editor.tabSize", "editor.insertSpaces", "files.eol"}, out: []string{"files.associations", "test.app", "window.zoomLevel"}},
		{name: "a language by name", query: "@lang:JSON", in: []string{"editor.tabSize"}, out: []string{"files.associations"}},
		{name: "a language that does not exist", query: "@lang:zzz", exactly: []string{}, hints: 1, summary: "No Settings Found"},
		{name: "a language that does not exist and words", query: "@lang:zzz tab", exactly: []string{}, hints: 1},
		{name: "the modified settings of a language", query: "@lang:go @modified", exactly: []string{"editor.insertSpaces", "editor.tabSize"}},
		{name: "a language without a block", query: "@lang:markdown @modified", exactly: []string{}},
		{name: "a language and a word", query: "@lang:go tab", in: []string{"editor.tabSize"}, out: []string{"editor.fontSize", "workbench.editor.showTabs"}},
		{name: "a language and an id", query: "@lang:go @id:editor.tabSize", exactly: []string{"editor.tabSize"}},
		{name: "a language and an id that cannot be set per language", query: "@lang:go @id:files.associations", exactly: []string{}},
		{name: "the workspace has no block of go", target: TargetWorkspace, query: "@lang:go @modified", exactly: []string{}},
		{name: "the workspace tab leaves out application settings", target: TargetWorkspace, query: "restore", out: []string{"window.restoreWindows"}},
		{name: "the user tab has them", query: "restore windows", in: []string{"window.restoreWindows"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			target := c.target
			if target == 0 {
				target = TargetUser
			}
			res := m.Search(target, c.query, languages)
			got := found(res)
			if c.exactly != nil {
				sorted := slices.Clone(got)
				slices.Sort(sorted)
				want := slices.Clone(c.exactly)
				slices.Sort(want)
				if len(sorted) != len(want) || (len(want) > 0 && !reflect.DeepEqual(sorted, want)) {
					t.Errorf("found %v, want %v", got, c.exactly)
				}
			}
			for _, key := range c.in {
				if !slices.Contains(got, key) {
					t.Errorf("%s is not among the %d results", key, len(got))
				}
			}
			for _, key := range c.out {
				if slices.Contains(got, key) {
					t.Errorf("%s is among the results", key)
				}
			}
			if res.Count != len(got) {
				t.Errorf("Count = %d, %d settings found", res.Count, len(got))
			}
			if c.count != 0 && res.Count != c.count {
				t.Errorf("Count = %d, want %d", res.Count, c.count)
			}
			if len(res.Hints) != c.hints {
				t.Errorf("hints %v", res.Hints)
			}
			if c.summary != "" && res.Summary() != c.summary {
				t.Errorf("Summary = %q, want %q", res.Summary(), c.summary)
			}
			seen := map[string]bool{}
			for _, key := range got {
				if seen[key] {
					t.Errorf("%s is listed twice", key)
				}
				seen[key] = true
			}
		})
	}
}

func TestSearchWithoutAQueryIsTheWholeEditor(t *testing.T) {
	_, m := uiFixture(t)
	for _, q := range []string{"", "   "} {
		res := m.Search(TargetUser, q, languages)
		if res.Active || res.Count != 0 || len(res.Hints) != 0 {
			t.Errorf("%q: %+v", q, res)
		}
		if len(res.Categories) == 0 || res.Categories[0].ID != CommonlyUsedID {
			t.Errorf("%q: the Commonly Used group should come first", q)
		}
	}
	// a search leaves it out: each setting is listed once
	res := m.Search(TargetUser, "font", languages)
	if !res.Active {
		t.Error("not active")
	}
	for _, c := range res.Categories {
		if c.ID == CommonlyUsedID {
			t.Error("Commonly Used is in the results")
		}
	}
}

func TestSearchGroupsKeepTheLayoutOrder(t *testing.T) {
	_, m := uiFixture(t)
	res := m.Search(TargetUser, "font", languages)
	var ids []string
	for _, c := range res.Categories {
		c.Walk(func(c *Category) { ids = append(ids, c.ID) })
	}
	order := map[string]int{}
	for i, c := range m.Categories(TargetUser, nil) {
		c.Walk(func(c *Category) { order[c.ID] = i*1000 + len(order) })
	}
	for i := 1; i < len(ids); i++ {
		if order[ids[i]] < order[ids[i-1]] {
			t.Errorf("group %s comes after %s", ids[i], ids[i-1])
		}
	}
	font := findCategory(res.Categories, "editor/font")
	if font == nil || font.Count() < 3 {
		t.Fatalf("the Font group: %+v", font)
	}
	// the count of a group counts the groups below it
	if top := findCategory(res.Categories, "editor"); top == nil || top.Count() < font.Count() {
		t.Errorf("Text Editor has %d, Font %d", top.Count(), font.Count())
	}
	total := 0
	for _, c := range res.Categories {
		total += c.Count()
	}
	if total != res.Count {
		t.Errorf("the groups hold %d settings, Count says %d", total, res.Count)
	}
}

func findCategory(cats []*Category, id string) *Category {
	for _, c := range cats {
		if f := c.Find(id); f != nil {
			return f
		}
	}
	return nil
}

func TestSearchRanking(t *testing.T) {
	_, m := uiFixture(t)
	// the key typed whole is first of its group
	res := m.Search(TargetUser, "editor.insertSpaces", languages)
	if c := findCategory(res.Categories, "editor"); c == nil || c.Keys[0] != "editor.insertSpaces" {
		t.Errorf("exact id: %v", found(res))
	}
	// a word of the key before a word of the description, in one group
	res = m.Search(TargetUser, "tab", languages)
	editor := findCategory(res.Categories, "editor")
	if editor == nil || !slices.Contains(editor.Keys, "editor.tabSize") {
		t.Fatalf("tab: %v", found(res))
	}
	// a setting that only mentions the word in its description comes after the ones that have it in the key
	prev := -1
	for _, k := range editor.Keys {
		s, _ := m.Store().Registry().Lookup(k)
		sc, ok := scoreSetting(s, []string{"tab"})
		if !ok {
			t.Fatalf("%s does not match tab", k)
		}
		if prev != -1 && sc > prev {
			t.Errorf("%s (%d) is after a lower score (%d)", k, sc, prev)
		}
		prev = sc
	}
	// equal scores keep the layout order
	res = m.Search(TargetUser, "tab", languages)
	for _, c := range res.Categories {
		c.Walk(func(c *Category) {
			var layout []string
			for _, lc := range m.Categories(TargetUser, nil) {
				if x := lc.Find(c.ID); x != nil {
					layout = x.Keys
				}
			}
			last := -1
			prevScore := 1 << 30
			for _, k := range c.Keys {
				s, _ := m.Store().Registry().Lookup(k)
				sc, _ := scoreSetting(s, []string{"tab"})
				at := slices.Index(layout, k)
				if sc == prevScore && at < last {
					t.Errorf("%s: equal scores out of the layout order", k)
				}
				prevScore, last = sc, at
			}
		})
	}
}

func TestSearchScores(t *testing.T) {
	setting := Setting{Key: "editor.fontSize", Types: []Type{Number}, Description: "Controls the font size in pixels.", Enum: []any{"small", "large", 3}}
	for _, c := range []struct {
		words []string
		want  int
		ok    bool
	}{
		{[]string{"font"}, scoreKeyWhole, true},
		{[]string{"fon"}, scoreKey, true},
		{[]string{"controls"}, scoreDescriptionWhole, true},
		{[]string{"control"}, scoreDescription, true},
		{[]string{"small"}, scoreValue, true},
		{[]string{"font", "size"}, 2 * scoreKeyWhole, true},
		{[]string{"font", "pixels"}, scoreKeyWhole + scoreDescriptionWhole, true},
		{[]string{"font", "zzz"}, 0, false},
		{[]string{"editorfont"}, scoreKey, true},
	} {
		got, ok := scoreSetting(setting, c.words)
		if got != c.want || ok != c.ok {
			t.Errorf("scoreSetting(%v) = %d, %v; want %d, %v", c.words, got, ok, c.want, c.ok)
		}
	}
}

func TestSearchInTheBlockOfALanguage(t *testing.T) {
	ctx := context.Background()
	f, m := uiFixture(t)
	f.write(f.userPath, "{\n  // mine\n  \"editor.tabSize\": 2,\n  \"[go]\": {\n    // go wants this\n    \"editor.tabSize\": 8\n  }\n}\n")
	f.reload()

	row, ok := m.RowFor("editor.tabSize", TargetUser, "go", "")
	if !ok || !row.Modified || row.Value != 8.0 || row.Language != "go" {
		t.Fatalf("row of [go]: %+v", row)
	}
	// the plain row is not modified by the block
	plain, _ := m.RowFor("editor.tabSize", TargetUser, "", "")
	if plain.Value != 2.0 || !plain.Modified {
		t.Errorf("plain row: %+v", plain)
	}
	// a setting the block does not set shows the default, unmodified
	insert, _ := m.RowFor("editor.insertSpaces", TargetUser, "go", "")
	if insert.Modified || insert.Value != true || len(insert.AlsoModifiedIn) != 0 {
		t.Errorf("insertSpaces of go: %+v", insert)
	}
	// another language is untouched
	other, _ := m.RowFor("editor.tabSize", TargetUser, "json", "")
	if other.Modified || other.Value != 4.0 {
		t.Errorf("tabSize of json: %+v", other)
	}
	// the workspace tab has no block
	ws, _ := m.RowFor("editor.tabSize", TargetWorkspace, "go", "")
	if ws.Modified {
		t.Errorf("workspace row of go: %+v", ws)
	}

	// editing writes the block, keeping the comments
	if err := m.SetValueFor(ctx, "editor.insertSpaces", TargetUser, "go", false); err != nil {
		t.Fatal(err)
	}
	if err := m.SetTextFor(ctx, "editor.tabSize", TargetUser, "go", "3"); err != nil {
		t.Fatal(err)
	}
	text := f.read(f.userPath)
	for _, want := range []string{"// mine", "// go wants this", `"[go]"`, `"editor.tabSize": 3`, `"editor.insertSpaces": false`} {
		if !strings.Contains(text, want) {
			t.Errorf("the file lacks %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, `"editor.tabSize": 2`) {
		t.Errorf("the plain value changed:\n%s", text)
	}
	if got := Get[int](f.store, "editor.tabSize", "go"); got != 3 {
		t.Errorf("tab size for go = %d", got)
	}
	if err := m.SetTextFor(ctx, "editor.tabSize", TargetUser, "go", "0"); err == nil {
		t.Error("an invalid text was written")
	} else {
		assertRoute(t, err, "UIModel.SetTextFor.SettingInputInvalid")
	}

	// resetting removes the key from the block, and the block with its last key
	if err := m.ResetFor(ctx, "editor.tabSize", TargetUser, "go"); err != nil {
		t.Fatal(err)
	}
	text = f.read(f.userPath)
	if strings.Contains(text, `"editor.tabSize": 3`) || !strings.Contains(text, `"[go]"`) || !strings.Contains(text, `"editor.tabSize": 2`) {
		t.Errorf("after the first reset:\n%s", text)
	}
	if err := m.ResetFor(ctx, "editor.insertSpaces", TargetUser, "go"); err != nil {
		t.Fatal(err)
	}
	text = f.read(f.userPath)
	if strings.Contains(text, "[go]") || strings.Contains(text, "insertSpaces") {
		t.Errorf("the empty block is still there:\n%s", text)
	}
	if !strings.Contains(text, "// mine") || !strings.Contains(text, `"editor.tabSize": 2`) {
		t.Errorf("the rest of the file changed:\n%s", text)
	}
	// ResetFor with no language is Reset
	if err := m.ResetFor(ctx, "editor.tabSize", TargetUser, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.read(f.userPath), "tabSize") {
		t.Error("the plain setting is still there")
	}
}

func TestResettingTheOnlySettingOfABlockRemovesTheBlock(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name, before, after string
	}{
		{"only block", "{\n  \"[go]\": {\n    \"editor.tabSize\": 8\n  }\n}\n", "{}\n"},
		{"block between settings", "{\n  \"a.b\": 1,\n  \"[go]\": {\n    \"editor.tabSize\": 8\n  },\n  \"c.d\": 2\n}\n", ""},
		{"a block with a comment inside keeps it", "{\n  \"[go]\": {\n    // keep\n    \"editor.tabSize\": 8\n  }\n}\n", ""},
		{"a block with another setting stays", "{\n  \"[go]\": {\n    \"editor.tabSize\": 8,\n    \"editor.insertSpaces\": false\n  }\n}\n", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.write(f.userPath, c.before)
			f.reload()
			if err := f.store.RemoveForLanguage(ctx, "editor.tabSize", "go", TargetUser); err != nil {
				t.Fatal(err)
			}
			text := f.read(f.userPath)
			if strings.Contains(text, "tabSize") {
				t.Errorf("the setting is still there:\n%s", text)
			}
			emptyBlock := strings.Contains(strings.Join(strings.Fields(text), ""), `"[go]":{}`)
			if emptyBlock {
				t.Errorf("an empty block is left:\n%s", text)
			}
			if c.name == "a block with another setting stays" && !strings.Contains(text, "insertSpaces") {
				t.Errorf("the other setting went:\n%s", text)
			}
			if c.after != "" && strings.Join(strings.Fields(text), "") != strings.Join(strings.Fields(c.after), "") {
				t.Errorf("text %q, want %q", text, c.after)
			}
		})
	}
}
