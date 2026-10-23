package keybinding

import "testing"

func TestKeyNamesAreComplete(t *testing.T) {
	for i := range keyCount {
		k := Key(i)
		if k.String() == "" {
			t.Errorf("key %d has no UI name", i)
		}
		if k != Unknown && k.UserSettings() == "" {
			t.Errorf("key %d (%s) has no user settings name", i, k)
		}
	}
	if Key(-1).String() != "unknown" || Key(keyCount).String() != "unknown" || Key(keyCount).UserSettings() != "" {
		t.Error("out of range keys must read as unknown")
	}
}

// TestKeyNameRoundTrips checks that every name the UI or keybindings.json prints is read back
// as the same key, except for the few keys VS Code gives the same name.
func TestKeyNameRoundTrips(t *testing.T) {
	for i := 1; i < keyCount; i++ {
		k := Key(i)
		if got := KeyFromString(k.String()); got != k {
			t.Errorf("KeyFromString(%q) = %s, want %s", k.String(), got, k)
		}
		if got := KeyFromUserSettings(k.UserSettings()); got != k {
			t.Errorf("KeyFromUserSettings(%q) = %s, want %s", k.UserSettings(), got, k)
		}
	}
}

func TestKeyNames(t *testing.T) {
	cases := []struct {
		key      Key
		ui       string
		settings string
	}{
		{KeyK, "K", "k"},
		{Digit7, "7", "7"},
		{LeftArrow, "LeftArrow", "left"},
		{Delete, "Del", "delete"},
		{Escape, "Escape", "escape"},
		{PageDown, "PageDown", "pagedown"},
		{BracketLeft, "[", "["},
		{Backslash, "\\", "\\"},
		{Quote, "'", "'"},
		{Backquote, "`", "`"},
		{NumpadAdd, "NumPad_Add", "numpad_add"},
		{IntlBackslash, "OEM_102", "oem_102"},
		{F12, "F12", "f12"},
		{Space, "Space", "space"},
	}
	for _, tc := range cases {
		if tc.key.String() != tc.ui {
			t.Errorf("%d String = %q, want %q", tc.key, tc.key.String(), tc.ui)
		}
		if tc.key.UserSettings() != tc.settings {
			t.Errorf("%d UserSettings = %q, want %q", tc.key, tc.key.UserSettings(), tc.settings)
		}
	}
}

func TestKeyFromNames(t *testing.T) {
	cases := []struct {
		name string
		want Key
	}{
		{"K", KeyK}, {"k", KeyK}, {"Left", LeftArrow}, {"Delete", Delete}, {"leftarrow", Unknown}, {"Del", Unknown},
		{"OEM_MINUS", Minus}, {"oem_plus", Equal}, {"OEM_4", BracketLeft}, {"-", Minus},
		{"nonsense", Unknown}, {"", Unknown}, {"esc", Unknown},
	}
	for _, tc := range cases {
		if got := KeyFromUserSettings(tc.name); got != tc.want {
			t.Errorf("KeyFromUserSettings(%q) = %s, want %s", tc.name, got, tc.want)
		}
	}
	if KeyFromString("Del") != Delete || KeyFromString("delete") != Unknown {
		t.Error("KeyFromString reads the UI names only")
	}
}

func TestIsModifier(t *testing.T) {
	for _, k := range []Key{Ctrl, Shift, Alt, Meta} {
		if !k.IsModifier() || !(Chord{Key: k}).IsModifierKey() {
			t.Errorf("%s is a modifier", k)
		}
	}
	if KeyA.IsModifier() || (Chord{Key: KeyA}).IsModifierKey() {
		t.Error("A is not a modifier")
	}
	if !(Chord{}).IsModifierKey() {
		t.Error("a chord without a key can not be dispatched")
	}
}
