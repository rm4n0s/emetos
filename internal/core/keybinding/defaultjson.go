package keybinding

import (
	"encoding/json"
	"strings"
)

// DefaultKeybindingsHeader is the comment the default keybindings document starts with.
const DefaultKeybindingsHeader = "// Override key bindings by placing them into your key bindings file."

// DefaultKeybindingsJSON is the text of the default keybindings document ("Open Default Keyboard Shortcuts
// (JSON)", SPEC §11.3): a JSONC array with a line for each built-in keybinding, in the order of the keymap, in the
// layout VS Code writes. Lines are separated by "\n".
//
// Ported from VS Code: src/vs/workbench/services/keybinding/common/keybindingIO.ts (writeKeybindingItem) and
// src/vs/workbench/services/keybinding/browser/keybindingService.ts (getDefaultKeybindingsContent)
func DefaultKeybindingsJSON(reg *Registry) string {
	var b strings.Builder
	b.WriteString(DefaultKeybindingsHeader + "\n[")
	first := true
	for _, it := range reg.Defaults() {
		if len(it.Keybinding) == 0 {
			continue
		}
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString("\n")
		writeKeybindingItem(&b, it)
	}
	b.WriteString("\n]\n")
	return b.String()
}

// jsonString is the JSON text of a string.
func jsonString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return `""`
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func writeKeybindingItem(b *strings.Builder, it Item) {
	const indent = "                                     "
	key := jsonString(it.Keybinding.UserSettingsLabel()) + ","
	for len(key) < 25 {
		key += " "
	}
	command := it.Command
	if it.Bubble {
		command = "^" + command
	}
	b.WriteString(`{ "key": ` + key + ` "command": `)
	if it.When != nil {
		b.WriteString(jsonString(command) + ",\n" + indent + `"when": ` + jsonString(it.When.Serialize()))
	} else {
		b.WriteString(jsonString(command))
	}
	if it.Args != nil {
		if data, err := json.Marshal(it.Args); err == nil {
			b.WriteString(",\n" + indent + `"args": ` + string(data))
		}
	}
	b.WriteString(" }")
}
