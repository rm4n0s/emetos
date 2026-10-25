package keybinding

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/jsonc"
)

// edit of a test: the editing, its format and the model the rows come from.
func testEditing() Editing {
	return Editing{Format: jsonc.FormattingOptions{TabSize: 4, InsertSpaces: false}}
}

// rowOfModel finds a row of the model of a keybindings.json by command and source.
func rowOfModel(t *testing.T, text, command string, source Source, key string) Row {
	t.Helper()
	m := testModel(t, text)
	for _, r := range m.Rows() {
		if r.Command == command && r.Source == source && (key == "" || r.Key == key) {
			return r
		}
	}
	t.Fatalf("no row %s %v %s in %q", command, source, key, summaries(m.Rows()))
	return Row{}
}

// userEntries reads the entries of a keybindings.json as "key|command|when".
func userEntries(t *testing.T, text string) []string {
	t.Helper()
	d, err := open(text)
	if err != nil {
		t.Fatalf("the result does not open: %v\n%s", err, text)
	}
	var out []string
	for i := range d.root.Children {
		key, command, when, _ := d.entry(i)
		out = append(out, key+"|"+command+"|"+when)
	}
	return out
}

func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestEditingAdd(t *testing.T) {
	e := testEditing()
	tests := []struct {
		name, text, command, key, when string
		args                           any
		want                           []string
		wantText                       string
	}{
		{name: "an empty file gets the header and the entry", text: "", command: "a.b", key: "ctrl+x", when: "editorFocus",
			want:     []string{"ctrl+x|a.b|editorFocus"},
			wantText: EmptyUserFile[:strings.Index(EmptyUserFile, "[")] + "[\n\t{\n\t\t\"key\": \"ctrl+x\",\n\t\t\"command\": \"a.b\",\n\t\t\"when\": \"editorFocus\"\n\t}\n]\n"},
		{name: "an entry without a when-clause", text: "[]", command: "a.b", key: "ctrl+shift+9",
			want: []string{"ctrl+shift+9|a.b|"}},
		{name: "after the entries and their comments", text: "// mine\n[\n\t// first\n\t{ \"key\": \"ctrl+1\", \"command\": \"one\" },\n\t/* second */\n\t{ \"key\": \"ctrl+2\", \"command\": \"two\" }\n]\n",
			command: "a.b", key: "ctrl+3",
			want: []string{"ctrl+1|one|", "ctrl+2|two|", "ctrl+3|a.b|"}},
		{name: "a file of comments only", text: "// nothing yet\n", command: "a.b", key: "ctrl+3", want: []string{"ctrl+3|a.b|"}},
		{name: "a sequence and arguments", text: "[]", command: "a.b", key: "Ctrl+K Ctrl+C", args: map[string]any{"n": 1.0}, want: []string{"Ctrl+K Ctrl+C|a.b|"}},
		{name: "a trailing comma is fine", text: "[\n\t{ \"key\": \"ctrl+1\", \"command\": \"one\" },\n]", command: "a.b", key: "ctrl+3", want: []string{"ctrl+1|one|", "ctrl+3|a.b|"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.Add(tc.text, tc.command, tc.key, tc.when, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if entries := userEntries(t, got); !sameList(entries, tc.want) {
				t.Errorf("entries %q, want %q\n%s", entries, tc.want, got)
			}
			if tc.wantText != "" && got != tc.wantText {
				t.Errorf("text:\n%q\nwant\n%q", got, tc.wantText)
			}
			if strings.HasPrefix(tc.text, "// mine") && !strings.Contains(got, "// first") || strings.Contains(tc.text, "/* second */") && !strings.Contains(got, "/* second */") {
				t.Errorf("a comment was lost:\n%s", got)
			}
			if tc.args != nil && !strings.Contains(got, `"args"`) {
				t.Errorf("the arguments are not in\n%s", got)
			}
			// the entries read back as keybindings
			if _, problems := ParseUserFile(got); len(problems) != 0 {
				t.Errorf("problems %v", problems)
			}
		})
	}
}

func TestEditingEdit(t *testing.T) {
	e := testEditing()
	user := "// my keys\n[\n\t{ \"key\": \"ctrl+alt+d\", \"command\": \"editor.action.copyLinesDownAction\", \"when\": \"editorFocus\" }\n]\n"
	tests := []struct {
		name     string
		text     string
		row      func(t *testing.T) Row
		key      string
		when     string
		want     []string
		wantKeep string
	}{
		{name: "a user entry changes its key in place", text: user,
			row: func(t *testing.T) Row {
				return rowOfModel(t, user, "editor.action.copyLinesDownAction", SourceUser, "")
			},
			key: "ctrl+alt+e", when: "editorFocus", want: []string{"ctrl+alt+e|editor.action.copyLinesDownAction|editorFocus"}, wantKeep: "// my keys"},
		{name: "a user entry changes its when-clause", text: user,
			row: func(t *testing.T) Row {
				return rowOfModel(t, user, "editor.action.copyLinesDownAction", SourceUser, "")
			},
			key: "ctrl+alt+d", when: "editorTextFocus", want: []string{"ctrl+alt+d|editor.action.copyLinesDownAction|editorTextFocus"}},
		{name: "a user entry loses its when-clause", text: user,
			row: func(t *testing.T) Row {
				return rowOfModel(t, user, "editor.action.copyLinesDownAction", SourceUser, "")
			},
			key: "ctrl+alt+d", when: "", want: []string{"ctrl+alt+d|editor.action.copyLinesDownAction|"}},
		{name: "a user entry gets a when-clause", text: "[{ \"key\": \"ctrl+alt+d\", \"command\": \"editor.action.copyLinesDownAction\" }]",
			row: func(t *testing.T) Row {
				return rowOfModel(t, `[{ "key": "ctrl+alt+d", "command": "editor.action.copyLinesDownAction" }]`, "editor.action.copyLinesDownAction", SourceUser, "")
			},
			key: "ctrl+alt+d", when: "editorFocus", want: []string{"ctrl+alt+d|editor.action.copyLinesDownAction|editorFocus"}},
		{name: "a default keybinding is removed and replaced", text: "",
			row: func(t *testing.T) Row {
				return rowOfModel(t, "", "workbench.action.showCommands", SourceDefault, "")
			},
			key: "ctrl+alt+p", want: []string{"ctrl+alt+p|workbench.action.showCommands|", "ctrl+shift+p|-workbench.action.showCommands|"}},
		{name: "a default keybinding with a when-clause is removed under it", text: "[]",
			row: func(t *testing.T) Row {
				return rowOfModel(t, "", "editor.action.copyLinesUpAction", SourceDefault, "")
			},
			key: "ctrl+alt+u", when: "editorFocus", want: []string{
				"ctrl+alt+u|editor.action.copyLinesUpAction|editorFocus",
				"shift+alt+up|-editor.action.copyLinesUpAction|editorTextFocus && !editorReadonly"}},
		{name: "a default keybinding of two keys changes one", text: "[]",
			row: func(t *testing.T) Row {
				return rowOfModel(t, "", "editor.action.clipboardCopyAction", SourceDefault, "ctrl+insert")
			},
			key: "ctrl+alt+c", want: []string{"ctrl+alt+c|editor.action.clipboardCopyAction|", "ctrl+insert|-editor.action.clipboardCopyAction|"}},
		{name: "a command without a keybinding gets one", text: "[]",
			row: func(t *testing.T) Row {
				return rowOfModel(t, "", "cursorLeft", SourceDefault, "")
			},
			key: "ctrl+alt+l", want: []string{"ctrl+alt+l|cursorLeft|"}},
		{name: "a default keybinding that is removed already is not removed twice", text: `[{ "key": "ctrl+shift+p", "command": "-workbench.action.showCommands" }]`,
			row: func(t *testing.T) Row {
				// the row of the model before the removal
				return rowOfModel(t, "", "workbench.action.showCommands", SourceDefault, "")
			},
			key: "ctrl+alt+p", want: []string{"ctrl+shift+p|-workbench.action.showCommands|", "ctrl+alt+p|workbench.action.showCommands|"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.Edit(tc.text, tc.row(t), tc.key, tc.when)
			if err != nil {
				t.Fatal(err)
			}
			if entries := userEntries(t, got); !sameList(entries, tc.want) {
				t.Errorf("entries %q, want %q\n%s", entries, tc.want, got)
			}
			if tc.wantKeep != "" && !strings.Contains(got, tc.wantKeep) {
				t.Errorf("%q is lost from\n%s", tc.wantKeep, got)
			}
		})
	}
}

func TestEditingUpdateWhen(t *testing.T) {
	e := testEditing()
	t.Run("a default keybinding keeps its key under another when-clause", func(t *testing.T) {
		row := rowOfModel(t, "", "workbench.action.showCommands", SourceDefault, "")
		got, err := e.UpdateWhen("", row, "editorFocus")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"ctrl+shift+p|workbench.action.showCommands|editorFocus", "ctrl+shift+p|-workbench.action.showCommands|"}
		if entries := userEntries(t, got); !sameList(entries, want) {
			t.Errorf("entries %q, want %q", entries, want)
		}
	})
	t.Run("a user keybinding", func(t *testing.T) {
		text := `[{ "key": "ctrl+9", "command": "workbench.action.showCommands" }]`
		row := rowOfModel(t, text, "workbench.action.showCommands", SourceUser, "")
		got, err := e.UpdateWhen(text, row, "editorFocus")
		if err != nil {
			t.Fatal(err)
		}
		if entries := userEntries(t, got); !sameList(entries, []string{"ctrl+9|workbench.action.showCommands|editorFocus"}) {
			t.Errorf("entries %q", entries)
		}
	})
	t.Run("a command without a keybinding has no when-clause to change", func(t *testing.T) {
		_, err := e.UpdateWhen("", rowOfModel(t, "", "cursorLeft", SourceDefault, ""), "editorFocus")
		wantRoute(t, err, "Editing.UpdateWhen.KeybindingEditUnbound")
	})
}

func TestEditingRemove(t *testing.T) {
	e := testEditing()
	user := "[\n\t// first\n\t{ \"key\": \"ctrl+1\", \"command\": \"one\" },\n\t{ \"key\": \"ctrl+alt+d\", \"command\": \"editor.action.copyLinesDownAction\" },\n\t{ \"key\": \"ctrl+3\", \"command\": \"three\" }\n]"
	tests := []struct {
		name string
		text string
		row  func(t *testing.T) Row
		want []string
	}{
		{name: "a user entry is deleted", text: user,
			row: func(t *testing.T) Row {
				return rowOfModel(t, user, "editor.action.copyLinesDownAction", SourceUser, "")
			},
			want: []string{"ctrl+1|one|", "ctrl+3|three|"}},
		{name: "the only entry", text: `[{ "key": "ctrl+alt+d", "command": "editor.action.copyLinesDownAction" }]`,
			row: func(t *testing.T) Row {
				return rowOfModel(t, `[{ "key": "ctrl+alt+d", "command": "editor.action.copyLinesDownAction" }]`, "editor.action.copyLinesDownAction", SourceUser, "")
			},
			want: nil},
		{name: "a default keybinding gets a removal", text: "",
			row: func(t *testing.T) Row {
				return rowOfModel(t, "", "workbench.action.showCommands", SourceDefault, "")
			},
			want: []string{"ctrl+shift+p|-workbench.action.showCommands|"}},
		{name: "a default keybinding with a when-clause", text: "[]",
			row: func(t *testing.T) Row {
				return rowOfModel(t, "", "editor.action.addCommentLine", SourceDefault, "")
			},
			want: []string{"ctrl+k ctrl+c|-editor.action.addCommentLine|editorTextFocus && !editorReadonly"}},
		{name: "a removal that the file has already is not added again", text: `[{ "key": "Ctrl+Shift+P", "command": "-workbench.action.showCommands" }]`,
			row: func(t *testing.T) Row {
				return rowOfModel(t, "", "workbench.action.showCommands", SourceDefault, "")
			},
			want: []string{"Ctrl+Shift+P|-workbench.action.showCommands|"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.Remove(tc.text, tc.row(t))
			if err != nil {
				t.Fatal(err)
			}
			if entries := userEntries(t, got); !sameList(entries, tc.want) {
				t.Errorf("entries %q, want %q\n%s", entries, tc.want, got)
			}
			if strings.Contains(tc.text, "// first") && !strings.Contains(got, "// first") {
				t.Errorf("a comment was lost:\n%s", got)
			}
		})
	}
	t.Run("a removal makes the model hide the default row", func(t *testing.T) {
		row := rowOfModel(t, "", "workbench.action.showCommands", SourceDefault, "")
		text, err := e.Remove("", row)
		if err != nil {
			t.Fatal(err)
		}
		m := testModel(t, text)
		for _, r := range m.ForCommand("workbench.action.showCommands") {
			if r.Bound() {
				t.Errorf("the row is still bound: %+v", r)
			}
		}
	})
}

func TestEditingReset(t *testing.T) {
	e := testEditing()
	text := `[
	{ "key": "ctrl+alt+d", "command": "editor.action.copyLinesDownAction" },
	{ "key": "ctrl+1", "command": "one" },
	{ "key": "shift+alt+down", "command": "-editor.action.copyLinesDownAction", "when": "editorTextFocus && !editorReadonly" },
	{ "key": "ctrl+alt+e", "command": "editor.action.copyLinesDownAction", "when": "editorFocus" }
]`
	t.Run("every entry of the command goes", func(t *testing.T) {
		row := rowOfModel(t, text, "editor.action.copyLinesDownAction", SourceUser, "ctrl+alt+d")
		got, err := e.Reset(text, row)
		if err != nil {
			t.Fatal(err)
		}
		if entries := userEntries(t, got); !sameList(entries, []string{"ctrl+1|one|"}) {
			t.Errorf("entries %q", entries)
		}
		// the default keybinding is back in the model
		rows := testModel(t, got).ForCommand("editor.action.copyLinesDownAction")
		if len(rows) != 1 || rows[0].Key != "shift+alt+down" || rows[0].Source != SourceDefault {
			t.Errorf("rows %v", summaries(rows))
		}
	})
	t.Run("a command whose keys were all removed", func(t *testing.T) {
		removed := `[{ "key": "ctrl+shift+p", "command": "-workbench.action.showCommands" }]`
		row := rowOfModel(t, removed, "workbench.action.showCommands", SourceUser, "")
		got, err := e.Reset(removed, row)
		if err != nil {
			t.Fatal(err)
		}
		if entries := userEntries(t, got); len(entries) != 0 {
			t.Errorf("entries %q", entries)
		}
	})
	t.Run("a default keybinding is left alone", func(t *testing.T) {
		row := rowOfModel(t, "", "workbench.action.showCommands", SourceDefault, "")
		got, err := e.Reset(text, row)
		if err != nil || got != text {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("an empty file stays empty", func(t *testing.T) {
		row := rowOfModel(t, `[{ "key": "ctrl+9", "command": "workbench.action.showCommands" }]`, "workbench.action.showCommands", SourceUser, "")
		got, err := e.Reset("", row)
		if err != nil || got != "" {
			t.Errorf("got %q, %v", got, err)
		}
	})
}

func TestEditingOverwrite(t *testing.T) {
	e := testEditing()
	text := `[
	{ "key": "ctrl+x", "command": "mine.one" },
	{ "key": "ctrl+9", "command": "mine.two" },
	{ "key": "ctrl+x", "command": "mine.three", "when": "editorFocus" }
]`
	m := testModel(t, text)
	kb, _ := ParseKeybinding("ctrl+x")
	conflicts := m.FindConflicts(kb, "", "new.command")
	if len(conflicts) != 4 {
		t.Fatalf("conflicts %v", summaries(conflicts))
	}
	got, err := e.Overwrite(text, conflicts)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ctrl+9|mine.two|", "ctrl+x|-editor.action.clipboardCutAction|textInputFocus", "ctrl+x|-other.cut|editorFocus"}
	if entries := userEntries(t, got); !sameList(entries, want) {
		t.Errorf("entries %q, want %q\n%s", entries, want, got)
	}
	// then the new command is the only one on the key
	got, err = e.Add(got, "new.command", "ctrl+x", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	after := testModel(t, got)
	var onKey []string
	for _, r := range after.Rows() {
		if r.Key == "ctrl+x" {
			onKey = append(onKey, r.Command)
		}
	}
	if len(onKey) != 1 || onKey[0] != "new.command" {
		t.Errorf("commands on the key: %v", onKey)
	}
}

// wantRoute checks that err is an *errors.Error whose route has the part.
func wantRoute(t *testing.T, err error, part string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %s", part)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute(part) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), part)
	}
}

func TestEditingFailures(t *testing.T) {
	e := testEditing()
	row := rowOfModel(t, "", "workbench.action.showCommands", SourceDefault, "")
	user := rowOfModel(t, `[{ "key": "ctrl+9", "command": "workbench.action.showCommands" }]`, "workbench.action.showCommands", SourceUser, "")
	tests := []struct {
		name  string
		run   func() error
		route string
	}{
		{"a file with a syntax error", func() error { _, err := e.Add("[ { ", "a", "ctrl+x", "", nil); return err }, "Editing.Add->open.KeybindingsEditFileInvalid"},
		{"a file that is an object", func() error { _, err := e.Add("{}", "a", "ctrl+x", "", nil); return err }, "Editing.Add->open.KeybindingsEditNotArray"},
		{"remove in a bad file", func() error { _, err := e.Remove("{}", row); return err }, "Editing.Remove->open.KeybindingsEditNotArray"},
		{"reset in a bad file", func() error { _, err := e.Reset("{", user); return err }, "Editing.Reset->open.KeybindingsEditFileInvalid"},
		{"edit in a bad file", func() error { _, err := e.Edit("[", row, "ctrl+x", ""); return err }, "Editing.Edit->open.KeybindingsEditFileInvalid"},
		{"no key", func() error { _, err := e.Add("[]", "a", "", "", nil); return err }, "Editing.Add->validKey.KeybindingEditKeyMissing"},
		{"a key that does not exist", func() error { _, err := e.Add("[]", "a", "ctrl+nokey", "", nil); return err }, "KeybindingKeyUnknown"},
		{"a key of only modifiers", func() error { _, err := e.Edit("[]", row, "ctrl+", ""); return err }, "KeybindingKeyUnknown"},
		{"no command", func() error { _, err := e.Add("[]", "", "ctrl+x", "", nil); return err }, "Editing.Add.KeybindingEditCommandMissing"},
		{"a when-clause that is not one", func() error { _, err := e.Add("[]", "a", "ctrl+x", "a &&", nil); return err }, "WhenClauseInvalid"},
		{"a when-clause that is not one, in an edit", func() error { _, err := e.Edit("[]", row, "ctrl+x", "(("); return err }, "WhenClauseInvalid"},
		{"an entry that is gone: edit", func() error { _, err := e.Edit("[]", user, "ctrl+x", ""); return err }, "Editing.Edit->document.userRow.KeybindingEntryNotFound"},
		{"an entry that is gone: remove", func() error { _, err := e.Remove("[]", user); return err }, "Editing.Remove->document.userRow.KeybindingEntryNotFound"},
		{"an entry of another command", func() error {
			_, err := e.Remove(`[{ "key": "ctrl+9", "command": "other" }]`, user)
			return err
		}, "KeybindingEntryNotFound"},
		{"arguments that cannot be written", func() error {
			_, err := e.Add("[]", "a", "ctrl+x", "", make(chan int))
			return err
		}, "Editing.Add->Editing.append->SetValue->setValue->setInArray->stringify.JSONCEncodeFailed"},
		{"nothing to remove from an unbound command", func() error {
			_, err := e.Remove("[]", Row{Command: "cursorLeft"})
			return err
		}, "Editing.Remove.KeybindingEditUnbound"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantRoute(t, tc.run(), tc.route)
		})
	}
}

func TestEditingTabsAndSpaces(t *testing.T) {
	spaces := Editing{Format: jsonc.FormattingOptions{TabSize: 2, InsertSpaces: true}}
	got, err := spaces.Add("[]", "a", "ctrl+x", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "\n    \"key\": \"ctrl+x\"") {
		t.Errorf("text %q", got)
	}
}

func TestValidateWhen(t *testing.T) {
	for _, ok := range []string{"", "  ", "a", "a && !b", "editorTextFocus && !editorReadonly"} {
		if err := ValidateWhen(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"&&", "a ||", "(a", "a &&", "!"} {
		wantRoute(t, ValidateWhen(bad), "WhenClauseInvalid")
	}
}

func TestParseUserFileKeepsTheEntryPosition(t *testing.T) {
	items, problems := ParseUserFile(`[ { "key": "nokey1", "command": "z" }, { "key": "ctrl+1", "command": "one" }, { "key": "ctrl+2", "command": "two" } ]`)
	if len(problems) != 1 || problems[0].Index != 0 || len(items) != 2 {
		t.Fatalf("items %v problems %v", items, problems)
	}
	if items[0].FileEntry != 2 || items[1].FileEntry != 3 {
		t.Errorf("entries %d %d", items[0].FileEntry, items[1].FileEntry)
	}
}
