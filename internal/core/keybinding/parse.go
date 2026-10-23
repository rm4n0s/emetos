package keybinding

import (
	"strings"

	"github.com/rm4n0s/errors"
)

// modifierPrefixes are the modifier words keybindings.json accepts, each followed by "+" or
// "-". win and cmd are other names of meta.
var modifierPrefixes = []struct {
	word string
	set  func(*Chord)
}{
	{"ctrl", func(c *Chord) { c.Ctrl = true }},
	{"shift", func(c *Chord) { c.Shift = true }},
	{"alt", func(c *Chord) { c.Alt = true }},
	{"meta", func(c *Chord) { c.Meta = true }},
	{"win", func(c *Chord) { c.Meta = true }},
	{"cmd", func(c *Chord) { c.Meta = true }},
}

// readModifiers reads the modifiers at the start of input and returns the chord so far, the
// key word that follows, and the rest of the input.
func readModifiers(input string) (c Chord, key, rest string) {
	input = strings.ToLower(strings.TrimSpace(input))
	for matched := true; matched; {
		matched = false
		for _, m := range modifierPrefixes {
			if len(input) > len(m.word) && strings.HasPrefix(input, m.word) && (input[len(m.word)] == '+' || input[len(m.word)] == '-') {
				m.set(&c)
				input = input[len(m.word)+1:]
				matched = true
			}
		}
	}
	if i := strings.IndexByte(input, ' '); i > 0 {
		return c, input[:i], input[i:]
	}
	return c, input, ""
}

// ParseKeybinding reads a keybinding as keybindings.json spells it: chords separated by
// white space, each made of modifiers (ctrl, shift, alt, meta, win, cmd) and a key joined
// with "+", such as "ctrl+shift+k ctrl+c". Case is ignored. An empty input is no
// keybinding (nil, nil). The [ScanCode] spelling of VS Code is not supported.
//
// Ported from VS Code: src/vs/base/common/keybindingParser.ts
func ParseKeybinding(input string) (Keybinding, error) {
	var out Keybinding
	rest := input
	for strings.TrimSpace(rest) != "" {
		var c Chord
		var key string
		c, key, rest = readModifiers(rest)
		if key == "" {
			return nil, errors.New("KeybindingKeyUnknown", "a chord of the keybinding has no key", "keybinding", input)
		}
		if strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]") && len(key) > 2 {
			return nil, errors.New("KeybindingScanCodeUnsupported", "keys written as [ScanCode] are not supported", "keybinding", input)
		}
		c.Key = KeyFromUserSettings(key)
		if c.Key == Unknown {
			return nil, errors.New("KeybindingKeyUnknown", "the keybinding has a key that does not exist", "keybinding", input, "key", key)
		}
		out = append(out, c)
	}
	return out, nil
}
