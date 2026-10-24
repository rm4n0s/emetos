package keybinding

import (
	"slices"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/jsonc"
)

// The edits of the keybindings editor to keybindings.json (SPEC §11.3, step 8.3), as functions from the text of
// the file to its new text. They edit the text as the settings do, so the comments and the layout of the file
// stay. The file is read and written, or the open document edited, by the caller.
//
// Ported from VS Code: src/vs/workbench/services/keybinding/common/keybindingEditing.ts
//
// Where this differs from VS Code: an entry of the file is found by its position (Row.FileEntry), not by its
// command and when-clause, so a default keybinding that is changed never edits an unrelated entry of the same
// command; Reset removes every entry of the file for the command (its keybindings and its "-command" entries),
// where VS Code removes the entry of the row and the "-command" entries; a default keybinding that has
// arguments keeps them in the entry that replaces it.

// EmptyUserFile is the text of a keybindings.json that Emetos makes.
const EmptyUserFile = "// Place your key bindings in this file to override the defaults\n[\n]\n"

// Editing edits the text of keybindings.json. Format says how the lines it adds are indented.
type Editing struct {
	Format jsonc.FormattingOptions
}

// userEntry is an entry of keybindings.json as it is written: the order of the properties is VS Code's.
type userEntry struct {
	Key     string `json:"key"`
	Command string `json:"command,omitempty"`
	When    string `json:"when,omitempty"`
	Args    any    `json:"args,omitempty"`
}

// ValidateWhen checks a when-clause that is going into the file; an empty text is no clause. It fails with the tag
// WhenClauseInvalid of package contextkey.
func ValidateWhen(when string) error {
	if strings.TrimSpace(when) == "" {
		return nil
	}
	_, err := contextkey.Parse(when)
	return err
}

// validKey parses the key of an entry that is going into the file. It fails with the tag
// KeybindingEditKeyMissing, or one of ParseKeybinding.
func validKey(key string) (Keybinding, error) {
	kb, err := ParseKeybinding(key)
	if err != nil {
		return nil, err
	}
	if len(kb) == 0 {
		return nil, errors.New("KeybindingEditKeyMissing", "the keybinding has no keys")
	}
	return kb, nil
}

// document is the parsed text of the file.
type document struct {
	text string
	root *jsonc.Node
}

// open parses the text. An empty file is the file Emetos makes, and one with comments alone gets an empty array
// after them. It fails with the tags KeybindingsEditFileInvalid (a syntax error) and KeybindingsEditNotArray.
func open(text string) (*document, error) {
	if strings.TrimSpace(text) == "" {
		text = EmptyUserFile
	}
	root, errs := jsonc.ParseTree(text, jsonc.ParseOptions{AllowEmptyContent: true})
	if len(errs) > 0 {
		return nil, errors.New("KeybindingsEditFileInvalid", "keybindings.json has errors: fix them first", "offset", errs[0].Offset)
	}
	if root == nil {
		text += "\n[]\n"
		root, _ = jsonc.ParseTree(text, jsonc.ParseOptions{})
	}
	if root == nil || root.Type != jsonc.ArrayNode {
		return nil, errors.New("KeybindingsEditNotArray", "keybindings.json must hold an array of keybindings: fix it first")
	}
	return &document{text: text, root: root}, nil
}

// entry reads the entry at an index of the array.
func (d *document) entry(index int) (key, command, when string, ok bool) {
	if index < 0 || index >= len(d.root.Children) || d.root.Children[index].Type != jsonc.ObjectNode {
		return "", "", "", false
	}
	for _, prop := range d.root.Children[index].Children {
		if prop.Type != jsonc.PropertyNode || len(prop.Children) != 2 || prop.Children[1].Type != jsonc.StringNode {
			continue
		}
		name, _ := prop.Children[0].Value.(string)
		value, _ := prop.Children[1].Value.(string)
		switch name {
		case "key":
			key = value
		case "command":
			command = value
		case "when":
			when = value
		}
	}
	return key, command, when, true
}

// apply makes the edits to the text and parses it again.
func (e Editing) apply(d *document, edits []jsonc.Edit, err error) (*document, error) {
	if err != nil {
		return nil, errors.NewErr("KeybindingsEditFailed", err)
	}
	text, err := jsonc.ApplyEdits(d.text, edits)
	if err != nil {
		return nil, errors.NewErr("KeybindingsEditFailed", err)
	}
	return open(text)
}

// append adds an entry at the end of the array.
func (e Editing) append(d *document, entry userEntry) (*document, error) {
	edits, err := jsonc.SetValue(d.text, jsonc.Path{-1}, entry, e.Format)
	return e.apply(d, edits, err)
}

// removeAt takes the entry at an index out of the array.
func (e Editing) removeAt(d *document, index int) (*document, error) {
	edits, err := jsonc.Remove(d.text, jsonc.Path{index}, e.Format)
	return e.apply(d, edits, err)
}

// setProperty sets a property of the entry at an index; an empty value takes it out.
func (e Editing) setProperty(d *document, index int, name, value string) (*document, error) {
	var edits []jsonc.Edit
	var err error
	if value == "" {
		edits, err = jsonc.Remove(d.text, jsonc.Path{index, name}, e.Format)
	} else {
		edits, err = jsonc.SetValue(d.text, jsonc.Path{index, name}, value, e.Format)
	}
	return e.apply(d, edits, err)
}

// userRow finds the entry of a row that is a keybinding of the file. It fails with the tag
// KeybindingEntryNotFound when the file does not have it any more (it changed since the table was made).
func (d *document) userRow(r Row) (int, error) {
	index := r.FileEntry - 1
	_, command, _, ok := d.entry(index)
	if r.FileEntry <= 0 || !ok || command != r.Command {
		return 0, errors.New("KeybindingEntryNotFound", "keybindings.json does not have the keybinding any more: it changed since the list was made",
			"command", r.Command, "entry", r.FileEntry)
	}
	return index, nil
}

// Add adds the keybinding of a command to the file: an entry with the key, the command, the when-clause and the
// arguments (nil for none). It fails with the tags KeybindingEditKeyMissing, those of ParseKeybinding and
// WhenClauseInvalid, KeybindingEditCommandMissing, KeybindingsEditFileInvalid and KeybindingsEditNotArray.
func (e Editing) Add(text, command, key, when string, args any) (string, error) {
	if command == "" {
		return "", errors.New("KeybindingEditCommandMissing", "a keybinding needs a command")
	}
	if _, err := validKey(key); err != nil {
		return "", err
	}
	when = strings.TrimSpace(when)
	if err := ValidateWhen(when); err != nil {
		return "", err
	}
	d, err := open(text)
	if err != nil {
		return "", err
	}
	if d, err = e.append(d, userEntry{Key: key, Command: command, When: when, Args: args}); err != nil {
		return "", err
	}
	return d.text, nil
}

// Edit gives a row another key and when-clause. A keybinding of the file is changed in place; a built-in one is
// removed with a "-command" entry and replaced by an entry of the new key; a command without a keybinding gets
// one. It fails with the tags of Add, and KeybindingEntryNotFound.
func (e Editing) Edit(text string, row Row, key, when string) (string, error) {
	if _, err := validKey(key); err != nil {
		return "", err
	}
	when = strings.TrimSpace(when)
	if err := ValidateWhen(when); err != nil {
		return "", err
	}
	d, err := open(text)
	if err != nil {
		return "", err
	}
	if row.Source == SourceUser && row.Bound() && row.FileEntry > 0 {
		index, err := d.userRow(row)
		if err != nil {
			return "", err
		}
		if d, err = e.setProperty(d, index, "key", key); err != nil {
			return "", err
		}
		if d, err = e.setProperty(d, index, "when", when); err != nil {
			return "", err
		}
		return d.text, nil
	}
	if d, err = e.append(d, userEntry{Key: key, Command: row.Command, When: when, Args: row.Args}); err != nil {
		return "", err
	}
	if row.Source == SourceDefault && row.Bound() {
		if d, err = e.removeDefault(d, row); err != nil {
			return "", err
		}
	}
	return d.text, nil
}

// UpdateWhen gives a row another when-clause and keeps its key. It fails with the tags of Edit, and
// KeybindingEditUnbound for a row without a keybinding.
func (e Editing) UpdateWhen(text string, row Row, when string) (string, error) {
	if !row.Bound() {
		return "", errors.New("KeybindingEditUnbound", "a command without a keybinding has no when-clause to change", "command", row.Command)
	}
	return e.Edit(text, row, row.Key, when)
}

// Remove takes the keybinding of a row away: the entry of the file is deleted; a built-in keybinding gets a
// "-command" entry (unless the file has it already). It fails with the tags KeybindingEditUnbound,
// KeybindingEntryNotFound, KeybindingsEditFileInvalid and KeybindingsEditNotArray.
func (e Editing) Remove(text string, row Row) (string, error) {
	if !row.Bound() {
		return "", errors.New("KeybindingEditUnbound", "a command without a keybinding has nothing to remove", "command", row.Command)
	}
	d, err := open(text)
	if err != nil {
		return "", err
	}
	if row.Source == SourceDefault {
		if d, err = e.removeDefault(d, row); err != nil {
			return "", err
		}
		return d.text, nil
	}
	index, err := d.userRow(row)
	if err != nil {
		return "", err
	}
	if d, err = e.removeAt(d, index); err != nil {
		return "", err
	}
	return d.text, nil
}

// removeDefault adds the entry "-command" for a built-in keybinding, when the file does not have it.
func (e Editing) removeDefault(d *document, row Row) (*document, error) {
	removal := "-" + row.Command
	for i := range d.root.Children {
		key, command, when, ok := d.entry(i)
		if ok && command == removal && sameKey(key, row.Key) && sameWhen(when, row.When) {
			return d, nil
		}
	}
	return e.append(d, userEntry{Key: row.Key, Command: removal, When: row.When})
}

func sameKey(a, b string) bool {
	ka, errA := ParseKeybinding(a)
	kb, errB := ParseKeybinding(b)
	return errA == nil && errB == nil && ka.Equal(kb)
}

func sameWhen(a, b string) bool {
	if a == b {
		return true
	}
	ea, errA := contextkey.Parse(a)
	eb, errB := contextkey.Parse(b)
	if a == "" || b == "" || errA != nil || errB != nil {
		return false
	}
	return ea.Equals(eb)
}

// Reset gives a command its built-in keybindings back: every entry of the file for the command, its own
// keybindings and its "-command" entries, is deleted. A row that is a built-in keybinding changes nothing.
// It fails with the tags KeybindingsEditFileInvalid and KeybindingsEditNotArray.
func (e Editing) Reset(text string, row Row) (string, error) {
	d, err := open(text)
	if err != nil {
		return "", err
	}
	if row.Source == SourceDefault && row.Bound() {
		return text, nil
	}
	for i := len(d.root.Children) - 1; i >= 0; i-- {
		_, command, _, ok := d.entry(i)
		if ok && (command == row.Command || command == "-"+row.Command) {
			if d, err = e.removeAt(d, i); err != nil {
				return "", err
			}
		}
	}
	if d.text == EmptyUserFile && strings.TrimSpace(text) == "" {
		return text, nil
	}
	return d.text, nil
}

// Overwrite takes the keybindings of the rows away together, as Remove does for each: the user's entries are
// deleted (the last first, so the positions of the others hold) and the built-in ones get their "-command"
// entries. It fails with the tags of Remove.
func (e Editing) Overwrite(text string, rows []Row) (string, error) {
	user := slices.DeleteFunc(slices.Clone(rows), func(r Row) bool { return r.Source != SourceUser || !r.Bound() })
	sort.SliceStable(user, func(i, j int) bool { return user[i].FileEntry > user[j].FileEntry })
	var rest []Row
	for _, r := range rows {
		if r.Source == SourceDefault && r.Bound() {
			rest = append(rest, r)
		}
	}
	var err error
	for _, r := range slices.Concat(user, rest) {
		if text, err = e.Remove(text, r); err != nil {
			return "", err
		}
	}
	return text, nil
}
