package keybinding

import (
	"slices"
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
)

// The model of the keybindings editor (SPEC §11.3, step 8.3): the rows of its table, made from the built-in
// keybindings, the user's and the commands, and the search over them. It is plain Go and knows nothing of the
// UI.
//
// Ported from VS Code: src/vs/workbench/services/preferences/browser/keybindingsEditorModel.ts

// Source says where the keybinding of a row comes from.
type Source int

// The sources of a row.
const (
	// SourceDefault is a built-in keybinding, or a command that has none ("System" in VS Code's older UI).
	SourceDefault Source = iota
	// SourceUser is a keybinding of keybindings.json, or a command whose built-in keybindings the user removed.
	SourceUser
)

// String is the text of the Source column: "Default" or "User".
func (s Source) String() string {
	if s == SourceUser {
		return "User"
	}
	return "Default"
}

// CommandInfo is what the model needs to know of a command: its id and the words the UI calls it.
type CommandInfo struct {
	// ID is the command id.
	ID string
	// Title and Category are those of the command; both empty for a command the palette does not list.
	Title, Category string
}

// Label is the name of the command in the table, "Category: Title", "Title", or "" when it has none.
func (c CommandInfo) Label() string {
	if c.Title == "" {
		return ""
	}
	if c.Category != "" {
		return c.Category + ": " + c.Title
	}
	return c.Title
}

// Row is a row of the table: a command with one keybinding (or none) and the condition it holds under.
type Row struct {
	// ID names the row: the command, the keybinding, the when-clause and the source.
	ID string
	// Command is the id of the command.
	Command string
	// Label is the name of the command ("Category: Title"), empty for a command without a title.
	Label string
	// Keybinding is the key sequence; nil for a command that has none.
	Keybinding Keybinding
	// Key is the keybinding as keybindings.json writes it ("ctrl+k ctrl+c"), and KeyLabel as the UI shows it
	// ("Ctrl+K Ctrl+C"); both empty without a keybinding.
	Key, KeyLabel string
	// When is the when-clause in its normal form, or "".
	When string
	// Source says where the row comes from.
	Source Source
	// Args are the arguments of the command.
	Args any
	// FileEntry is the position of the entry of keybindings.json that made the row, counted from 1; 0 for a
	// built-in keybinding or a command without one.
	FileEntry int
}

// Bound tells whether the row has a keybinding.
func (r Row) Bound() bool { return len(r.Keybinding) > 0 }

// Model is the list of rows of the keybindings editor. Make one with NewModel; it does not change.
type Model struct {
	rows []Row
}

// NewModel makes the rows from the built-in keybindings, the user's (as ParseUserFile gives them) and the
// commands. The built-in keybindings that a user's "-command" entry removes are not listed, a command that has
// no keybinding is listed once without one, and rows are ordered as the editor shows them: those with a
// keybinding first, then by the name of the command, then the user's before the built-in ones.
func NewModel(defaults, user []Item, commands []CommandInfo) *Model {
	labels := make(map[string]string, len(commands))
	for _, c := range commands {
		labels[c.ID] = c.Label()
	}
	hasDefault := map[string]bool{}
	for _, d := range defaults {
		if d.Command != "" && !strings.HasPrefix(d.Command, "-") {
			hasDefault[d.Command] = true
		}
	}
	kept := HandleRemovals(slices.Concat(defaults, user))
	bound := map[string]bool{}
	var rows []Row
	for _, it := range kept {
		if it.Command == "" || strings.HasPrefix(it.Command, "-") {
			continue
		}
		bound[it.Command] = true
		rows = append(rows, rowOf(it, labels[it.Command]))
	}
	for _, c := range commands {
		if bound[c.ID] || strings.HasPrefix(c.ID, "_") || strings.HasPrefix(c.ID, "vscode.") {
			continue
		}
		bound[c.ID] = true
		row := Row{Command: c.ID, Label: c.Label(), Source: SourceDefault}
		if hasDefault[c.ID] {
			// the built-in keybindings were all removed: the user's file changed this command
			row.Source = SourceUser
		}
		row.ID = rowID(row)
		rows = append(rows, row)
	}
	seen := map[string]bool{}
	out := rows[:0]
	for _, r := range rows {
		if !seen[r.ID] {
			seen[r.ID] = true
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return compareRows(out[i], out[j]) < 0 })
	return &Model{rows: out}
}

// NewModelOfRows makes a model of rows as they are given: their ids are not made again, and rows with the same
// id are not left out, which NewModel never produces. It lets a test show the editor a model it cannot make.
func NewModelOfRows(rows []Row) *Model { return &Model{rows: slices.Clone(rows)} }

// rowOf makes the row of a resolved item.
func rowOf(it Item, label string) Row {
	r := Row{Command: it.Command, Label: label, Keybinding: it.Keybinding, Args: it.Args, FileEntry: it.FileEntry}
	if len(it.Keybinding) > 0 {
		r.Key, r.KeyLabel = it.Keybinding.UserSettingsLabel(), it.Keybinding.Label()
	}
	if it.When != nil {
		r.When = it.When.Serialize()
	}
	if !it.IsDefault {
		r.Source = SourceUser
	}
	r.ID = rowID(r)
	return r
}

func rowID(r Row) string {
	return r.Command + "\x00" + r.Keybinding.AriaLabel() + "\x00" + r.When + "\x00" + r.Source.String()
}

// compareRows orders rows as the table shows them (KeybindingsEditorModel.compareKeybindingData).
func compareRows(a, b Row) int {
	switch {
	case a.Bound() && !b.Bound():
		return -1
	case b.Bound() && !a.Bound():
		return 1
	case a.Label != "" && b.Label == "":
		return -1
	case b.Label != "" && a.Label == "":
		return 1
	}
	if a.Label != "" && b.Label != "" && a.Label != b.Label {
		return fuzzy.Collate(a.Label, b.Label)
	}
	if a.Command == b.Command {
		if a.Source == SourceDefault && b.Source != SourceDefault {
			return 1
		}
		if a.Source != SourceDefault && b.Source == SourceDefault {
			return -1
		}
		return 0
	}
	return fuzzy.Collate(a.Command, b.Command)
}

// Rows are all the rows, in the order of the table. The caller must not change the slice.
func (m *Model) Rows() []Row { return m.rows }

// Row finds the row with an id.
func (m *Model) Row(id string) (Row, bool) {
	for _, r := range m.rows {
		if r.ID == id {
			return r, true
		}
	}
	return Row{}, false
}

// ForCommand lists the rows of a command.
func (m *Model) ForCommand(command string) []Row {
	var out []Row
	for _, r := range m.rows {
		if r.Command == command {
			out = append(out, r)
		}
	}
	return out
}
