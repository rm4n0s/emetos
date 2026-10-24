package keybinding

import (
	"path/filepath"

	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
)

// testModel makes the model of a few built-in keybindings, the text of a keybindings.json and some commands.
func testModel(t testing.TB, userText string) *Model {
	t.Helper()
	defaults := []Item{
		kbItem(t, "ctrl+c", "editor.action.clipboardCopyAction", nil, nil, true),
		kbItem(t, "ctrl+insert", "editor.action.clipboardCopyAction", nil, nil, true),
		kbItem(t, "shift+alt+down", "editor.action.copyLinesDownAction", nil, mustParseWhen(t, "editorTextFocus && !editorReadonly"), true),
		kbItem(t, "shift+alt+up", "editor.action.copyLinesUpAction", nil, mustParseWhen(t, "editorTextFocus && !editorReadonly"), true),
		kbItem(t, "ctrl+k ctrl+c", "editor.action.addCommentLine", nil, mustParseWhen(t, "editorTextFocus && !editorReadonly"), true),
		kbItem(t, "ctrl+shift+p", "workbench.action.showCommands", nil, nil, true),
		kbItem(t, "ctrl+x", "editor.action.clipboardCutAction", nil, mustParseWhen(t, "textInputFocus"), true),
		kbItem(t, "ctrl+x", "other.cut", nil, mustParseWhen(t, "editorFocus"), true),
		kbItem(t, "ctrl+k", "prefix.only", nil, nil, true),
	}
	user, problems := ParseUserFile(userText)
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	commands := []CommandInfo{
		{ID: "editor.action.clipboardCopyAction", Title: "Copy", Category: ""},
		{ID: "editor.action.copyLinesDownAction", Title: "Copy Line Down", Category: ""},
		{ID: "editor.action.copyLinesUpAction", Title: "Copy Line Up"},
		{ID: "editor.action.addCommentLine", Title: "Add Line Comment"},
		{ID: "workbench.action.showCommands", Title: "Show All Commands", Category: "View"},
		{ID: "editor.action.clipboardCutAction", Title: "Cut"},
		{ID: "other.cut", Title: "Cut Other"},
		{ID: "prefix.only"},
		{ID: "workbench.action.openSettings", Title: "Settings"},
		{ID: "cursorLeft"},
		{ID: "_private"},
	}
	return NewModel(defaults, user, commands)
}

// summary describes a row as "command|key|when|source".
func summary(r Row) string { return r.Command + "|" + r.Key + "|" + r.When + "|" + r.Source.String() }

func summaries(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = summary(r)
	}
	return out
}

func resultRows(rs []Result) []Row {
	out := make([]Row, len(rs))
	for i, r := range rs {
		out[i] = r.Row
	}
	return out
}

func TestModelRows(t *testing.T) {
	tests := []struct {
		name string
		user string
		want []string
	}{
		{
			name: "built-in keybindings, then the commands without one",
			want: []string{
				"editor.action.addCommentLine|ctrl+k ctrl+c|editorTextFocus && !editorReadonly|Default",
				"editor.action.clipboardCopyAction|ctrl+c||Default",
				"editor.action.clipboardCopyAction|ctrl+insert||Default",
				"editor.action.copyLinesDownAction|shift+alt+down|editorTextFocus && !editorReadonly|Default",
				"editor.action.copyLinesUpAction|shift+alt+up|editorTextFocus && !editorReadonly|Default",
				"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default",
				"other.cut|ctrl+x|editorFocus|Default",
				"workbench.action.showCommands|ctrl+shift+p||Default",
				"prefix.only|ctrl+k||Default",
				"workbench.action.openSettings|||Default",
				"cursorLeft|||Default",
			},
		},
		{
			name: "a user keybinding sits before the built-in one of its command",
			user: `[{ "key": "ctrl+alt+d", "command": "editor.action.copyLinesDownAction" }]`,
			want: []string{
				"editor.action.addCommentLine|ctrl+k ctrl+c|editorTextFocus && !editorReadonly|Default",
				"editor.action.clipboardCopyAction|ctrl+c||Default",
				"editor.action.clipboardCopyAction|ctrl+insert||Default",
				"editor.action.copyLinesDownAction|ctrl+alt+d||User",
				"editor.action.copyLinesDownAction|shift+alt+down|editorTextFocus && !editorReadonly|Default",
				"editor.action.copyLinesUpAction|shift+alt+up|editorTextFocus && !editorReadonly|Default",
				"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default",
				"other.cut|ctrl+x|editorFocus|Default",
				"workbench.action.showCommands|ctrl+shift+p||Default",
				"prefix.only|ctrl+k||Default",
				"workbench.action.openSettings|||Default",
				"cursorLeft|||Default",
			},
		},
		{
			name: "a removal hides the built-in keybinding and leaves the command unbound, as the user's",
			user: `[{ "key": "ctrl+shift+p", "command": "-workbench.action.showCommands" }]`,
			want: []string{
				"editor.action.addCommentLine|ctrl+k ctrl+c|editorTextFocus && !editorReadonly|Default",
				"editor.action.clipboardCopyAction|ctrl+c||Default",
				"editor.action.clipboardCopyAction|ctrl+insert||Default",
				"editor.action.copyLinesDownAction|shift+alt+down|editorTextFocus && !editorReadonly|Default",
				"editor.action.copyLinesUpAction|shift+alt+up|editorTextFocus && !editorReadonly|Default",
				"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default",
				"other.cut|ctrl+x|editorFocus|Default",
				"prefix.only|ctrl+k||Default",
				"workbench.action.openSettings|||Default",
				"workbench.action.showCommands|||User",
				"cursorLeft|||Default",
			},
		},
		{
			name: "a removal of one of two keys leaves the other",
			user: `[{ "key": "ctrl+insert", "command": "-editor.action.clipboardCopyAction" }]`,
			want: []string{
				"editor.action.addCommentLine|ctrl+k ctrl+c|editorTextFocus && !editorReadonly|Default",
				"editor.action.clipboardCopyAction|ctrl+c||Default",
				"editor.action.copyLinesDownAction|shift+alt+down|editorTextFocus && !editorReadonly|Default",
				"editor.action.copyLinesUpAction|shift+alt+up|editorTextFocus && !editorReadonly|Default",
				"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default",
				"other.cut|ctrl+x|editorFocus|Default",
				"workbench.action.showCommands|ctrl+shift+p||Default",
				"prefix.only|ctrl+k||Default",
				"workbench.action.openSettings|||Default",
				"cursorLeft|||Default",
			},
		},
		{
			name: "a user keybinding for a command without a title is listed by its id",
			user: `[{ "key": "ctrl+9", "command": "myext.run", "when": "editorFocus" }]`,
			want: []string{
				"editor.action.addCommentLine|ctrl+k ctrl+c|editorTextFocus && !editorReadonly|Default",
				"editor.action.clipboardCopyAction|ctrl+c||Default",
				"editor.action.clipboardCopyAction|ctrl+insert||Default",
				"editor.action.copyLinesDownAction|shift+alt+down|editorTextFocus && !editorReadonly|Default",
				"editor.action.copyLinesUpAction|shift+alt+up|editorTextFocus && !editorReadonly|Default",
				"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default",
				"other.cut|ctrl+x|editorFocus|Default",
				"workbench.action.showCommands|ctrl+shift+p||Default",
				"myext.run|ctrl+9|editorFocus|User",
				"prefix.only|ctrl+k||Default",
				"workbench.action.openSettings|||Default",
				"cursorLeft|||Default",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := summaries(testModel(t, tc.user).Rows())
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rows:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestModelRowLabelsAndFileEntries(t *testing.T) {
	m := testModel(t, `[{ "key": "ctrl+9", "command": "x" }, { "key": "ctrl+alt+d", "command": "editor.action.copyLinesDownAction" }]`)
	var down, show Row
	for _, r := range m.Rows() {
		switch {
		case r.Command == "editor.action.copyLinesDownAction" && r.Source == SourceUser:
			down = r
		case r.Command == "workbench.action.showCommands":
			show = r
		}
	}
	if down.FileEntry != 2 || down.Label != "Copy Line Down" || down.KeyLabel != "Ctrl+Alt+D" || down.Key != "ctrl+alt+d" || !down.Bound() {
		t.Errorf("user row %+v", down)
	}
	if show.Label != "View: Show All Commands" || show.FileEntry != 0 || show.Source != SourceDefault {
		t.Errorf("default row %+v", show)
	}
	if rows := m.ForCommand("editor.action.clipboardCopyAction"); len(rows) != 2 {
		t.Errorf("ForCommand %v", summaries(rows))
	}
	if r, ok := m.Row(down.ID); !ok || r.FileEntry != 2 {
		t.Errorf("Row(%q) = %+v, %v", down.ID, r, ok)
	}
	if _, ok := m.Row("none"); ok {
		t.Error("Row found a row that is not there")
	}
}

func TestFetch(t *testing.T) {
	m := testModel(t, `[{ "key": "ctrl+alt+d", "command": "editor.action.copyLinesDownAction" }, { "key": "ctrl+k ctrl+x", "command": "workbench.action.openSettings" }]`)
	tests := []struct {
		name   string
		search string
		want   []string
	}{
		{"empty search finds every row", "", nil},
		{"words of the title", "copy line", []string{
			"editor.action.copyLinesDownAction|ctrl+alt+d||User",
			"editor.action.copyLinesDownAction|shift+alt+down|editorTextFocus && !editorReadonly|Default",
			"editor.action.copyLinesUpAction|shift+alt+up|editorTextFocus && !editorReadonly|Default",
		}},
		{"the id", "clipboardCut", []string{"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default"}},
		{"camel case of the id", "cLUA", []string{"editor.action.copyLinesUpAction|shift+alt+up|editorTextFocus && !editorReadonly|Default"}},
		{"the when-clause", "textInputFocus", []string{"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default"}},
		{"the source", "user", []string{
			"editor.action.copyLinesDownAction|ctrl+alt+d||User",
			"workbench.action.openSettings|ctrl+k ctrl+x||User",
		}},
		{"@source:user", "@source:user", []string{
			"editor.action.copyLinesDownAction|ctrl+alt+d||User",
			"workbench.action.openSettings|ctrl+k ctrl+x||User",
		}},
		{"@source:user with text", "@source:user settings", []string{"workbench.action.openSettings|ctrl+k ctrl+x||User"}},
		{"@source:default with text", "@source:default copy line up", []string{"editor.action.copyLinesUpAction|shift+alt+up|editorTextFocus && !editorReadonly|Default"}},
		{"@source:system is @source:default", "@source:system cursorLeft", []string{"cursorLeft|||Default"}},
		{"@command", "@command:editor.action.clipboardCopyAction", []string{
			"editor.action.clipboardCopyAction|ctrl+c||Default",
			"editor.action.clipboardCopyAction|ctrl+insert||Default",
		}},
		{"@command with a when-clause the command has", "@command:editor.action.copyLinesUpAction +when:editorTextFocus && !editorReadonly", []string{
			"editor.action.copyLinesUpAction|shift+alt+up|editorTextFocus && !editorReadonly|Default",
		}},
		{"@command with a when-clause it has not gets a row of its own", "@command:editor.action.copyLinesUpAction +when:isLinux", []string{
			"editor.action.copyLinesUpAction||true|User",
		}},
		{"@command of an unknown command", "@command:nothing.here", []string{}},
		{"exact keys in quotes", `"ctrl+x"`, []string{
			"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default",
			"other.cut|ctrl+x|editorFocus|Default",
		}},
		{"exact keys in the label of the UI", `"Ctrl+C"`, []string{"editor.action.clipboardCopyAction|ctrl+c||Default"}},
		{"exact keys match the sequences that begin with them (VS Code)", `"ctrl+k"`, []string{
			"editor.action.addCommentLine|ctrl+k ctrl+c|editorTextFocus && !editorReadonly|Default",
			"workbench.action.openSettings|ctrl+k ctrl+x||User",
			"prefix.only|ctrl+k||Default",
		}},
		{"two chords in quotes", `"ctrl+k ctrl+c"`, []string{"editor.action.addCommentLine|ctrl+k ctrl+c|editorTextFocus && !editorReadonly|Default"}},
		{"two chords are not found by their first", `"ctrl+k ctrl+x"`, []string{"workbench.action.openSettings|ctrl+k ctrl+x||User"}},
		{"quotes are not text", `"copy line"`, []string{}},
		{"@keybinding", "@keybinding:ctrl+x", []string{
			"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default",
			"other.cut|ctrl+x|editorFocus|Default",
		}},
		{"@keybinding in quotes", `@keybinding:"ctrl+k ctrl+c"`, []string{"editor.action.addCommentLine|ctrl+k ctrl+c|editorTextFocus && !editorReadonly|Default"}},
		{"keys as words, a modifier and a key", "ctrl insert", []string{"editor.action.clipboardCopyAction|ctrl+insert||Default"}},
		{"nothing found", "zzzzzz", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := summaries(resultRows(m.Fetch(tc.search)))
			want := tc.want
			if want == nil {
				want = summaries(m.Rows())
			}
			if len(got) == 0 && len(want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Fetch(%q):\n got %q\nwant %q", tc.search, got, want)
			}
		})
	}
}

func TestFetchMatches(t *testing.T) {
	m := testModel(t, "")
	t.Run("the label and the id", func(t *testing.T) {
		rs := m.Fetch("copy line up")
		if len(rs) != 1 {
			t.Fatalf("%d results", len(rs))
		}
		r := rs[0]
		if want := []fuzzy.Match{{Start: 0, End: 12}}; !reflect.DeepEqual(r.Label, want) {
			t.Errorf("label matches %v, want %v", r.Label, want)
		}
		if r.Keybinding != nil || r.ID == nil && r.Label == nil {
			t.Errorf("result %+v", r)
		}
	})
	t.Run("the id alone", func(t *testing.T) {
		rs := m.Fetch("clipboardCut")
		if len(rs) != 1 || rs[0].ID == nil || rs[0].Label != nil {
			t.Fatalf("results %+v", rs)
		}
		id := rs[0].Row.Command
		got := rs[0].ID[0]
		if id[got.Start:got.End] != "clipboardCut" {
			t.Errorf("id match %v in %q", got, id)
		}
	})
	t.Run("keys", func(t *testing.T) {
		rs := m.Fetch(`"ctrl+shift+p"`)
		if len(rs) != 1 || rs[0].Keybinding == nil {
			t.Fatalf("results %+v", rs)
		}
		want := &KeyMatches{First: ChordMatch{Ctrl: true, Shift: true, Key: true}}
		if !reflect.DeepEqual(rs[0].Keybinding, want) {
			t.Errorf("key matches %+v, want %+v", rs[0].Keybinding, want)
		}
		if rs[0].Label != nil || rs[0].ID != nil {
			t.Error("a quoted search matched text")
		}
	})
	t.Run("two chords", func(t *testing.T) {
		rs := m.Fetch(`"ctrl+k ctrl+c"`)
		if len(rs) != 1 {
			t.Fatalf("results %+v", rs)
		}
		want := &KeyMatches{First: ChordMatch{Ctrl: true, Key: true}, Chord: ChordMatch{Ctrl: true, Key: true}}
		if !reflect.DeepEqual(rs[0].Keybinding, want) {
			t.Errorf("key matches %+v, want %+v", rs[0].Keybinding, want)
		}
	})
	t.Run("the source", func(t *testing.T) {
		rs := m.Fetch("default")
		found := false
		for _, r := range rs {
			if r.Row.Command == "cursorLeft" && r.Source != nil {
				found = true
			}
		}
		if !found {
			t.Errorf("results %+v", rs)
		}
	})
}

func TestFindConflicts(t *testing.T) {
	m := testModel(t, `[{ "key": "ctrl+alt+d", "command": "editor.action.copyLinesDownAction", "when": "editorFocus" }]`)
	tests := []struct {
		name    string
		key     string
		when    string
		command string
		want    []string
	}{
		{"the same key and the same when", "ctrl+x", "textInputFocus", "new.command", []string{
			"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default",
			"other.cut|ctrl+x|editorFocus|Default",
		}},
		{"the same key, a when that excludes the other", "ctrl+x", "textInputFocus && !editorFocus", "new.command", []string{"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default"}},
		{"the same key, an empty when overlaps all", "ctrl+x", "", "new.command", []string{
			"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default",
			"other.cut|ctrl+x|editorFocus|Default",
		}},
		{"a binding with no when overlaps any", "ctrl+c", "editorFocus", "new.command", []string{"editor.action.clipboardCopyAction|ctrl+c||Default"}},
		{"a when that includes the other", "ctrl+x", "editorFocus && textInputFocus", "new.command", []string{
			"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default",
			"other.cut|ctrl+x|editorFocus|Default",
		}},
		{"whens that do not exclude each other conflict", "ctrl+x", "listFocus", "new.command", []string{
			"editor.action.clipboardCutAction|ctrl+x|textInputFocus|Default",
			"other.cut|ctrl+x|editorFocus|Default",
		}},
		{"whens that exclude each other do not", "ctrl+x", "!editorFocus && !textInputFocus", "new.command", nil},
		{"the command's own rows are not conflicts", "ctrl+c", "", "editor.action.clipboardCopyAction", nil},
		{"another key", "ctrl+7", "", "new.command", nil},
		{"only the same sequence conflicts, not the longer ones that begin with it", "ctrl+k", "", "new.command", []string{"prefix.only|ctrl+k||Default"}},
		{"a sequence is not found by its first chord", "ctrl+k ctrl+x", "", "new.command", nil},
		{"two chords", "ctrl+k ctrl+c", "", "new.command", []string{"editor.action.addCommentLine|ctrl+k ctrl+c|editorTextFocus && !editorReadonly|Default"}},
		{"a user keybinding", "ctrl+alt+d", "editorFocus", "new.command", []string{"editor.action.copyLinesDownAction|ctrl+alt+d|editorFocus|User"}},
		{"no key", "", "", "new.command", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kb, err := ParseKeybinding(tc.key)
			if err != nil {
				t.Fatal(err)
			}
			got := summaries(m.FindConflicts(kb, tc.when, tc.command))
			if !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("conflicts:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestWhensOverlap(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"a", "", true},
		{"", "a", true},
		{"a", "a", true},
		{"a && b", "a", true},
		{"a", "a && b", true},
		{"a", "b", true},
		{"a", "!a", false},
		{"!a", "a && b", false},
		{"a == 1", "a == 2", true}, // Implies does not prove that two values exclude each other
		{"a == 1", "b == 2", true},
		{"a ||", "b", false}, // not a clause: compared as text
		{"a ||", "a ||", true},
	}
	for _, tc := range tests {
		if got := WhensOverlap(tc.a, tc.b); got != tc.want {
			t.Errorf("WhensOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestDefaultKeybindingsJSON(t *testing.T) {
	reg := DefaultRegistry()
	text := DefaultKeybindingsJSON(reg)
	if !strings.HasPrefix(text, DefaultKeybindingsHeader+"\n[") || !strings.HasSuffix(text, "\n]\n") {
		t.Fatalf("text starts %q ends %q", text[:60], text[len(text)-10:])
	}
	// it is valid JSONC whose items are the defaults, each once, in the order of the keymap
	items, problems := ParseUserFile(text)
	if len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
	defaults := reg.Defaults()
	var want []Item
	for _, d := range defaults {
		if len(d.Keybinding) > 0 {
			want = append(want, d)
		}
	}
	if len(items) != len(want) {
		t.Fatalf("%d items, want %d", len(items), len(want))
	}
	for i := range items {
		if items[i].Command != want[i].Command || !items[i].Keybinding.Equal(want[i].Keybinding) || (items[i].When == nil) != (want[i].When == nil) {
			t.Fatalf("item %d: %s, want %s", i, describe(items[i]), describe(want[i]))
		}
		if items[i].When != nil && items[i].When.Serialize() != want[i].When.Serialize() {
			t.Fatalf("item %d when %q, want %q", i, items[i].When.Serialize(), want[i].When.Serialize())
		}
	}
	if !strings.Contains(text, `{ "key": "ctrl+shift+p",       "command": "workbench.action.showCommands"`) &&
		!strings.Contains(text, `"key": "ctrl+shift+p",`) {
		t.Error("the layout of a line is not VS Code's")
	}
}

func TestDefaultKeybindingsJSONLayout(t *testing.T) {
	reg := NewRegistry()
	reg.MustRegister(
		Rule{Command: "a.b", Primary: "ctrl+x", When: mustParseWhen(t, "editorFocus")},
		Rule{Command: "c.d", Primary: "ctrl+k ctrl+c", Args: map[string]any{"n": 1}},
	)
	want := DefaultKeybindingsHeader + `
[
{ "key": "ctrl+x",                 "command": "a.b",
                                     "when": "editorFocus" },
{ "key": "ctrl+k ctrl+c",          "command": "c.d",
                                     "args": {"n":1} }
]
`
	if got := DefaultKeybindingsJSON(reg); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestResolverReplace(t *testing.T) {
	before := NewResolver([]Item{kbItem(t, "ctrl+a", "one", nil, nil, true)}, nil, nil)
	holder := before
	after := NewResolver([]Item{kbItem(t, "ctrl+a", "two", nil, nil, true)}, nil, nil)
	before.Replace(after)
	if res := holder.Resolve(nil, nil, "ctrl+A"); res.Command != "two" {
		t.Errorf("the holder resolves %q", res.Command)
	}
}

func TestUserKeybindingsPath(t *testing.T) {
	paths := storagetest.Isolate(t)
	got, err := UserKeybindingsPath(paths, storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := paths.ConfigDir(storage.DefaultProfile)
	if got != filepath.Join(dir, "keybindings.json") {
		t.Errorf("path %q", got)
	}
	if _, err := UserKeybindingsPath(paths, "../bad"); err == nil {
		t.Error("a bad profile was accepted")
	}
}
