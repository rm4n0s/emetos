package terminal

import (
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// special maps the logical keys that are not characters to the keys of the emulator's encoder.
var special = map[services.LogicalKeyboardKey]emulator.Key{
	services.LogicalKeyboardKeyEnter: emulator.KeyEnter, services.LogicalKeyboardKeyTab: emulator.KeyTab,
	services.LogicalKeyboardKeyBackspace: emulator.KeyBackspace, services.LogicalKeyboardKeyEscape: emulator.KeyEscape,
	services.LogicalKeyboardKeyArrowUp: emulator.KeyUp, services.LogicalKeyboardKeyArrowDown: emulator.KeyDown,
	services.LogicalKeyboardKeyArrowRight: emulator.KeyRight, services.LogicalKeyboardKeyArrowLeft: emulator.KeyLeft,
	services.LogicalKeyboardKeyHome: emulator.KeyHome, services.LogicalKeyboardKeyEnd: emulator.KeyEnd,
	services.LogicalKeyboardKeyPageUp: emulator.KeyPageUp, services.LogicalKeyboardKeyPageDown: emulator.KeyPageDown,
	services.LogicalKeyboardKeyInsert: emulator.KeyInsert, services.LogicalKeyboardKeyDelete: emulator.KeyDelete,
	services.LogicalKeyboardKeyNumpadEnter:    emulator.KeyKPEnter,
	services.LogicalKeyboardKeyNumpadAdd:      emulator.KeyKPAdd,
	services.LogicalKeyboardKeyNumpadSubtract: emulator.KeyKPSubtract,
	services.LogicalKeyboardKeyNumpadMultiply: emulator.KeyKPMultiply,
	services.LogicalKeyboardKeyNumpadDivide:   emulator.KeyKPDivide,
	services.LogicalKeyboardKeyNumpadDecimal:  emulator.KeyKPDecimal,
	services.LogicalKeyboardKeyNumpadComma:    emulator.KeyKPComma,
	services.LogicalKeyboardKeyNumpadEqual:    emulator.KeyKPEqual,
}

// functionKeys are F1 to F20 in order.
var functionKeys = [20]services.LogicalKeyboardKey{
	services.LogicalKeyboardKeyF1, services.LogicalKeyboardKeyF2, services.LogicalKeyboardKeyF3, services.LogicalKeyboardKeyF4,
	services.LogicalKeyboardKeyF5, services.LogicalKeyboardKeyF6, services.LogicalKeyboardKeyF7, services.LogicalKeyboardKeyF8,
	services.LogicalKeyboardKeyF9, services.LogicalKeyboardKeyF10, services.LogicalKeyboardKeyF11, services.LogicalKeyboardKeyF12,
	services.LogicalKeyboardKeyF13, services.LogicalKeyboardKeyF14, services.LogicalKeyboardKeyF15, services.LogicalKeyboardKeyF16,
	services.LogicalKeyboardKeyF17, services.LogicalKeyboardKeyF18, services.LogicalKeyboardKeyF19, services.LogicalKeyboardKeyF20,
}

// keypad are Numpad0 to Numpad9 in order.
var keypad = [10]services.LogicalKeyboardKey{
	services.LogicalKeyboardKeyNumpad0, services.LogicalKeyboardKeyNumpad1, services.LogicalKeyboardKeyNumpad2,
	services.LogicalKeyboardKeyNumpad3, services.LogicalKeyboardKeyNumpad4, services.LogicalKeyboardKeyNumpad5,
	services.LogicalKeyboardKeyNumpad6, services.LogicalKeyboardKeyNumpad7, services.LogicalKeyboardKeyNumpad8,
	services.LogicalKeyboardKeyNumpad9,
}

// keyEventOf turns a key press into the key the emulator encodes. A character key with no Ctrl and no Alt is left to the text
// input (viaText true): the input method delivers the text, as it does to the editor, unless there is no input method
// connection, in which case the character of the key is used. A key that sends nothing (a modifier alone) is not ok.
func keyEventOf(e services.KeyEvent, mods emulator.Mod, textInput bool) (ev emulator.KeyEvent, viaText, ok bool) {
	logical := e.LogicalKey()
	if k, found := special[logical]; found {
		return emulator.KeyEvent{Key: k, Mods: mods}, false, true
	}
	for i, k := range functionKeys {
		if k == logical {
			return emulator.KeyEvent{Key: emulator.KeyF1 + emulator.Key(i), Mods: mods}, false, true
		}
	}
	for i, k := range keypad {
		if k == logical {
			return emulator.KeyEvent{Key: emulator.KeyKP0 + emulator.Key(i), Mods: mods}, false, true
		}
	}
	id := logical.KeyID()
	if id == 0 || id >= unicode.MaxRune {
		return emulator.KeyEvent{}, false, false // a modifier, a media key, ...
	}
	r := rune(id)
	if !unicode.IsPrint(r) && r != ' ' {
		return emulator.KeyEvent{}, false, false
	}
	if mods&(emulator.ModCtrl|emulator.ModAlt) == 0 {
		if textInput {
			return emulator.KeyEvent{}, true, true
		}
		if ch := e.Character(); ch != "" {
			c, _ := utf8.DecodeRuneInString(ch)
			return emulator.KeyEvent{Key: emulator.KeyRune, Rune: c, Mods: mods &^ emulator.ModShift}, false, true
		}
		return emulator.KeyEvent{}, false, false
	}
	// Ctrl or Alt with a character: the character of the layout, in upper case with Shift held
	if mods&emulator.ModCtrl == 0 {
		if ch := e.Character(); ch != "" {
			c, _ := utf8.DecodeRuneInString(ch)
			if c >= ' ' {
				r = c
			}
		}
	}
	if mods&emulator.ModShift != 0 && mods&emulator.ModCtrl == 0 {
		r = unicode.ToUpper(r)
	}
	return emulator.KeyEvent{Key: emulator.KeyRune, Rune: r, Mods: mods &^ emulator.ModShift}, false, true
}
