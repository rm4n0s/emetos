package realinput

import (
	"strings"

	"github.com/rm4n0s/errors"
)

// keymapText is the xkb keymap the virtual keyboard gives the compositor: the US layout, as the evdev codes of the
// tables below assume.
const keymapText = `xkb_keymap {
 xkb_keycodes { include "evdev+aliases(qwerty)" };
 xkb_types { include "complete" };
 xkb_compat { include "complete" };
 xkb_symbols { include "pc+us+inet(evdev)" };
};
`

// Modifier masks of the keymap (the mods_depressed of zwp_virtual_keyboard_v1.modifiers).
const (
	modShift = 1 << 0
	modCtrl  = 1 << 2
	modAlt   = 1 << 3
	modSuper = 1 << 6
)

// evdev codes of the modifier keys and of the keys the combos name.
const (
	keyLeftShift = 42
	keyLeftCtrl  = 29
	keyLeftAlt   = 56
	keyLeftMeta  = 125
)

// keyStroke is one key of the US layout: its evdev code and whether it needs Shift.
type keyStroke struct {
	code  int
	shift bool
}

// usKeys maps a character to the key that types it on the US layout. It is built once and never changed.
var usKeys = buildUSKeys()

func buildUSKeys() map[rune]keyStroke {
	m := map[rune]keyStroke{}
	rows := []struct {
		plain, shifted string
		first          int
	}{
		{"1234567890-=", "!@#$%^&*()_+", 2},
		{"qwertyuiop[]", "QWERTYUIOP{}", 16},
		{"asdfghjkl;'`", "ASDFGHJKL:\"~", 30},
		{"\\zxcvbnm,./", "|ZXCVBNM<>?", 43},
	}
	for _, row := range rows {
		for i, c := range row.plain {
			m[c] = keyStroke{code: row.first + i}
		}
		for i, c := range row.shifted {
			m[c] = keyStroke{code: row.first + i, shift: true}
		}
	}
	m[' '] = keyStroke{code: 57}
	m['\n'] = keyStroke{code: 28}
	m['\t'] = keyStroke{code: 15}
	return m
}

// namedKeys are the keys that combos name (lower case).
var namedKeys = map[string]int{
	"return": 28, "enter": 28, "escape": 1, "esc": 1, "tab": 15, "backspace": 14, "delete": 111, "del": 111, "space": 57,
	"up": 103, "down": 108, "left": 105, "right": 106, "home": 102, "end": 107, "pageup": 104, "pagedown": 109,
	"insert": 110, "f1": 59, "f2": 60, "f3": 61, "f4": 62, "f5": 63, "f6": 64, "f7": 65, "f8": 66, "f9": 67, "f10": 68,
	"f11": 87, "f12": 88, "grave": 41, "backquote": 41, "minus": 12, "equal": 13, "comma": 51, "period": 52, "slash": 53,
	"semicolon": 39, "quote": 40, "bracketleft": 26, "bracketright": 27, "backslash": 43,
}

// modifierKeys maps the name of a modifier to its mask and its key.
var modifierKeys = map[string]struct{ mask, code int }{
	"shift": {modShift, keyLeftShift}, "ctrl": {modCtrl, keyLeftCtrl}, "control": {modCtrl, keyLeftCtrl},
	"alt": {modAlt, keyLeftAlt}, "super": {modSuper, keyLeftMeta}, "meta": {modSuper, keyLeftMeta}, "cmd": {modSuper, keyLeftMeta},
}

// combo is a key with the modifiers held while it is pressed.
type combo struct {
	// mods are the modifier keys, in the order they are pressed.
	mods []int
	// mask is the modifier mask the compositor is told while the key is down.
	mask int
	// code is the evdev code of the key.
	code int
}

// parseCombo reads a key combination such as "ctrl+shift+p", "Escape", "alt+Left", "ctrl++" or "f2". Names are not case
// sensitive; a single character is the key that types it, and a character that needs Shift adds it.
func parseCombo(text string) (combo, error) {
	var c combo
	parts := strings.Split(strings.ToLower(text), "+")
	// "ctrl++" is Ctrl and the plus key, and "+" alone is the plus key: the empty parts around the last plus are the key
	if n := len(parts); n >= 2 && parts[n-1] == "" {
		parts = append(parts[:n-2], "+")
	}
	for _, p := range parts[:len(parts)-1] {
		m, ok := modifierKeys[p]
		if !ok {
			return c, errors.New("RealInputComboInvalid", "a combination names a modifier that does not exist", "combo", text, "modifier", p)
		}
		c.mods = append(c.mods, m.code)
		c.mask |= m.mask
	}
	last := parts[len(parts)-1]
	if code, ok := namedKeys[last]; ok {
		c.code = code
		return c, nil
	}
	r := []rune(last)
	if len(r) != 1 {
		return c, errors.New("RealInputComboInvalid", "a combination names a key that does not exist", "combo", text, "key", last)
	}
	// the combo of a capital letter is written in lower case above; the key is the same, Shift is explicit in the combo
	k, ok := usKeys[r[0]]
	if !ok {
		return c, errors.New("RealInputComboInvalid", "a combination names a key that is not on the US layout", "combo", text, "key", last)
	}
	c.code = k.code
	if k.shift && c.mask&modShift == 0 {
		c.mods = append(c.mods, keyLeftShift)
		c.mask |= modShift
	}
	return c, nil
}
