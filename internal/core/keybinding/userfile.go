package keybinding

import (
	"path/filepath"
	"strconv"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/jsonc"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/errors"
)

// userFileName is the name of the user's keybindings file.
const userFileName = "keybindings.json"

// UserKeybindingsPath is the keybindings.json of a profile (SPEC §11.3), next to its settings.json.
func UserKeybindingsPath(paths storage.Paths, profile string) (string, error) {
	dir, err := paths.ConfigDir(profile)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, userFileName), nil
}

// Problem is something wrong with keybindings.json. The entry it concerns is skipped, the
// others still apply.
type Problem struct {
	// Index is the position of the entry in the array, or -1 for the file as a whole.
	Index int
	// Err says what is wrong; it is an *errors.Error with a tag of this package.
	Err error
}

// ParseUserFile reads the text of keybindings.json: an array of objects with "key" (chords
// such as "ctrl+k ctrl+c"), "command" (a leading "-" removes a built-in keybinding of that
// command), "when" and "args". The result are the user's items, in file order, to pass as the
// overrides of NewResolver. An entry whose key, command or when-clause cannot be read is
// reported in the problems and skipped; VS Code would keep an entry with an unknown key as an
// unbound command, which makes a removal with a typo remove every key of the command, so
// Emetos does not.
//
// Ported from VS Code: src/vs/workbench/services/keybinding/common/keybindingIO.ts
func ParseUserFile(text string) ([]Item, []Problem) {
	value, syntax := jsonc.Parse(text, jsonc.ParseOptions{AllowEmptyContent: true})
	var problems []Problem
	for _, e := range syntax {
		problems = append(problems, Problem{Index: -1, Err: errors.New("KeybindingsFileInvalid", "syntax error in keybindings.json: "+e.Message(), "offset", e.Offset)})
	}
	if value == nil {
		return nil, problems
	}
	entries, ok := value.([]any)
	if !ok {
		problems = append(problems, Problem{Index: -1, Err: errors.New("KeybindingsFileInvalid", "keybindings.json must hold an array of keybindings")})
		return nil, problems
	}
	var items []Item
	for i, entry := range entries {
		item, err := readUserEntry(entry)
		if err != nil {
			problems = append(problems, Problem{Index: i, Err: err})
			continue
		}
		item.FileEntry = i + 1
		items = append(items, item)
	}
	return items, problems
}

// readUserEntry reads one object of keybindings.json.
func readUserEntry(entry any) (Item, error) {
	obj, ok := entry.(map[string]any)
	if !ok {
		return Item{}, errors.New("KeybindingEntryInvalid", "an entry of keybindings.json is not an object")
	}
	var kb Keybinding
	if key, present := obj["key"]; present {
		s, ok := key.(string)
		if !ok {
			return Item{}, errors.New("KeybindingEntryInvalid", "the key of a keybinding is not a string")
		}
		var err error
		if kb, err = ParseKeybinding(s); err != nil {
			return Item{}, err
		}
	}
	command, _ := obj["command"].(string)
	if command == "" && kb == nil {
		return Item{}, errors.New("KeybindingEntryInvalid", "a keybinding has neither a key nor a command")
	}
	var when contextkey.Expr
	if w, ok := obj["when"].(string); ok && w != "" {
		var err error
		if when, err = contextkey.Parse(w); err != nil {
			return Item{}, err
		}
	}
	return NewItem(kb, command, obj["args"], when, false), nil
}

// Describe gives a one-line description of a problem for the log and for notifications.
func (p Problem) Describe() string {
	appErr, ok := errors.FromError(p.Err)
	msg := p.Err.Error()
	if ok {
		msg = appErr.Message
	}
	if p.Index < 0 {
		return msg
	}
	return "keybinding " + strconv.Itoa(p.Index+1) + ": " + msg
}
