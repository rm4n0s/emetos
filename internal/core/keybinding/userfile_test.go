package keybinding

import "testing"

func TestParseUserFile(t *testing.T) {
	text := `// my keys
[
	{ "key": "ctrl+shift+k", "command": "editor.action.deleteLines", "when": "editorTextFocus && !editorReadonly" },
	{ "key": "ctrl+k ctrl+t", "command": "someCommand", "args": { "text": "hi", "n": 3 } },
	/* removal */ { "key": "ctrl+l", "command": "-expandLineSelection" },
	{ "command": "-undo" },
	{ "key": "", "command": "cursorLeft" },
	{ "key": "ctrl+q", "command": "^bubble" },
]`
	items, problems := ParseUserFile(text)
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if len(items) != 6 {
		t.Fatalf("got %d items", len(items))
	}
	if items[0].Command != "editor.action.deleteLines" || items[0].When == nil ||
		items[0].When.Serialize() != "editorTextFocus && !editorReadonly" || items[0].IsDefault {
		t.Errorf("item 0: %+v", items[0])
	}
	args, _ := items[1].Args.(map[string]any)
	if items[1].Keybinding.UserSettingsLabel() != "ctrl+k ctrl+t" || args["text"] != "hi" || args["n"] != 3.0 || len(items[1].Chords) != 2 {
		t.Errorf("item 1: %+v", items[1])
	}
	if items[2].Command != "-expandLineSelection" || items[3].Command != "-undo" || items[3].Keybinding != nil {
		t.Errorf("removals: %+v %+v", items[2], items[3])
	}
	if items[4].Command != "cursorLeft" || len(items[4].Chords) != 0 {
		t.Errorf("an empty key unbinds: %+v", items[4])
	}
	if !items[5].Bubble || items[5].Command != "bubble" {
		t.Errorf("bubble: %+v", items[5])
	}
}

func TestParseUserFileEmpty(t *testing.T) {
	for _, text := range []string{"", "  \n", "// only a comment\n", "[]"} {
		items, problems := ParseUserFile(text)
		if len(items) != 0 || len(problems) != 0 {
			t.Errorf("%q: %v %v", text, items, problems)
		}
	}
}

func TestParseUserFileProblems(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		items int
		index int
		route string
	}{
		{"not an array", `{ "key": "ctrl+k" }`, 0, -1, "ParseUserFile.KeybindingsFileInvalid"},
		{"a number", `3`, 0, -1, "ParseUserFile.KeybindingsFileInvalid"},
		{"syntax error", `[ { "key": "ctrl+k", "command": "a" } `, 1, -1, "ParseUserFile.KeybindingsFileInvalid"},
		{"entry not an object", `[ 3, { "key": "a", "command": "ok" } ]`, 1, 0, "ParseUserFile->readUserEntry.KeybindingEntryInvalid"},
		{"key not a string", `[ { "key": 3, "command": "x" } ]`, 0, 0, "ParseUserFile->readUserEntry.KeybindingEntryInvalid"},
		{"unknown key", `[ { "key": "ctrl+nonsense", "command": "x" } ]`, 0, 0, "ParseUserFile->readUserEntry->ParseKeybinding.KeybindingKeyUnknown"},
		{"scan code key", `[ { "key": "[KeyA]", "command": "x" } ]`, 0, 0, "ParseUserFile->readUserEntry->ParseKeybinding.KeybindingScanCodeUnsupported"},
		{"neither key nor command", `[ { "when": "a" } ]`, 0, 0, "ParseUserFile->readUserEntry.KeybindingEntryInvalid"},
		{"bad when", `[ { "key": "a", "command": "x", "when": "a &&" } ]`, 0, 0, "ParseUserFile->readUserEntry->Parse.WhenClauseInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, problems := ParseUserFile(tc.text)
			if len(items) != tc.items {
				t.Errorf("got %d items, want %d", len(items), tc.items)
			}
			if len(problems) == 0 {
				t.Fatal("no problem reported")
			}
			p := problems[len(problems)-1]
			if p.Index != tc.index {
				t.Errorf("index %d, want %d", p.Index, tc.index)
			}
			assertRoute(t, p.Err, tc.route)
			if p.Describe() == "" {
				t.Error("empty description")
			}
		})
	}
}

func TestProblemDescribe(t *testing.T) {
	_, problems := ParseUserFile(`[ { "key": "ctrl+nonsense", "command": "x" } ]`)
	if len(problems) != 1 {
		t.Fatalf("problems: %v", problems)
	}
	if got := problems[0].Describe(); got != "keybinding 1: the keybinding has a key that does not exist" {
		t.Errorf("Describe = %q", got)
	}
}
