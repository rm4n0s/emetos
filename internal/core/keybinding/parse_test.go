package keybinding

import (
	"slices"
	"testing"
)

func TestParseKeybinding(t *testing.T) {
	cases := []struct {
		input    string
		label    string
		aria     string
		settings string
		dispatch []string
	}{
		{"ctrl+shift+k", "Ctrl+Shift+K", "Control+Shift+K", "ctrl+shift+k", []string{"ctrl+shift+K"}},
		{"Ctrl+Shift+Alt+Meta+K", "Ctrl+Shift+Alt+Super+K", "Control+Shift+Alt+Super+K", "ctrl+shift+alt+meta+k", []string{"ctrl+shift+alt+meta+K"}},
		{"alt+ctrl+k", "Ctrl+Alt+K", "Control+Alt+K", "ctrl+alt+k", []string{"ctrl+alt+K"}},
		{"ctrl-k", "Ctrl+K", "Control+K", "ctrl+k", []string{"ctrl+K"}},
		{"cmd+k", "Super+K", "Super+K", "meta+k", []string{"meta+K"}},
		{"win+k", "Super+K", "Super+K", "meta+k", []string{"meta+K"}},
		{"  ctrl+k   ctrl+c  ", "Ctrl+K Ctrl+C", "Control+K Control+C", "ctrl+k ctrl+c", []string{"ctrl+K", "ctrl+C"}},
		{"ctrl+k a b", "Ctrl+K A B", "Control+K A B", "ctrl+k a b", []string{"ctrl+K", "A", "B"}},
		{"left", "LeftArrow", "LeftArrow", "left", []string{"LeftArrow"}},
		{"shift+delete", "Shift+Del", "Shift+Del", "shift+delete", []string{"shift+Del"}},
		{"ctrl+[", "Ctrl+[", "Control+[", "ctrl+[", []string{"ctrl+["}},
		{"ctrl+]", "Ctrl+]", "Control+]", "ctrl+]", []string{"ctrl+]"}},
		{"ctrl+-", "Ctrl+-", "Control+-", "ctrl+-", []string{"ctrl+-"}},
		{"ctrl+=", "Ctrl+=", "Control+=", "ctrl+=", []string{"ctrl+="}},
		{"ctrl+\\", "Ctrl+\\", "Control+\\", "ctrl+\\", []string{"ctrl+\\"}},
		{"ctrl+oem_minus", "Ctrl+-", "Control+-", "ctrl+-", []string{"ctrl+-"}},
		{"ctrl+numpad_add", "Ctrl+NumPad_Add", "Control+NumPad_Add", "ctrl+numpad_add", []string{"ctrl+NumPad_Add"}},
		{"f5", "F5", "F5", "f5", []string{"F5"}},
		{"ctrl+ctrl", "Ctrl", "Control", "ctrl", nil},
		{"shift", "Shift", "Shift", "shift", nil},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			kb, err := ParseKeybinding(tc.input)
			if err != nil {
				t.Fatalf("ParseKeybinding: %v", err)
			}
			if got := kb.Label(); got != tc.label {
				t.Errorf("Label = %q, want %q", got, tc.label)
			}
			if got := kb.AriaLabel(); got != tc.aria {
				t.Errorf("AriaLabel = %q, want %q", got, tc.aria)
			}
			if got := kb.UserSettingsLabel(); got != tc.settings {
				t.Errorf("UserSettingsLabel = %q, want %q", got, tc.settings)
			}
			if got := kb.DispatchChords(); !slices.Equal(got, tc.dispatch) {
				t.Errorf("DispatchChords = %q, want %q", got, tc.dispatch)
			}
			// The user settings label reads back as the same keybinding, except for a modifier
			// that is also its own flag ("ctrl+ctrl" is printed "ctrl").
			again, err := ParseKeybinding(kb.UserSettingsLabel())
			if tc.input != "ctrl+ctrl" && (err != nil || !again.Equal(kb)) {
				t.Errorf("round trip: %v %v", again, err)
			}
		})
	}
}

func TestParseKeybindingEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\t"} {
		kb, err := ParseKeybinding(in)
		if err != nil || kb != nil {
			t.Errorf("ParseKeybinding(%q) = %v, %v; want nil, nil", in, kb, err)
		}
	}
}

func TestParseKeybindingErrors(t *testing.T) {
	cases := []struct {
		input string
		route string
	}{
		{"ctrl+", "ParseKeybinding.KeybindingKeyUnknown"},
		{"ctrl+foo", "ParseKeybinding.KeybindingKeyUnknown"},
		{"ctrl+k ctrl+", "ParseKeybinding.KeybindingKeyUnknown"},
		{"ctrl+k nonsense", "ParseKeybinding.KeybindingKeyUnknown"},
		{"[KeyA]", "ParseKeybinding.KeybindingScanCodeUnsupported"},
		{"ctrl+[KeyA]", "ParseKeybinding.KeybindingScanCodeUnsupported"},
		{"ctrl+k [KeyB]", "ParseKeybinding.KeybindingScanCodeUnsupported"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			kb, err := ParseKeybinding(tc.input)
			if err == nil {
				t.Fatalf("ParseKeybinding(%q) = %v, want an error", tc.input, kb)
			}
			assertRoute(t, err, tc.route)
		})
	}
}

func TestKeybindingEqual(t *testing.T) {
	a := mustKB(t, "ctrl+k ctrl+c")
	if !a.Equal(mustKB(t, "Ctrl+K Ctrl+C")) {
		t.Error("same sequences are equal")
	}
	for _, other := range []string{"ctrl+k", "ctrl+k ctrl+u", "ctrl+k ctrl+c ctrl+c", "shift+k ctrl+c", ""} {
		if a.Equal(mustKB(t, other)) {
			t.Errorf("%q must differ", other)
		}
	}
}

func TestSingleModifierChords(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"shift shift", []string{"shift", "shift"}},
		{"ctrl", []string{"ctrl"}},
		{"alt alt", []string{"alt", "alt"}},
		{"meta meta", []string{"meta", "meta"}},
		{"ctrl+k", nil},
		{"shift k", nil},
		{"ctrl+shift", nil},
	}
	for _, tc := range cases {
		kb := mustKB(t, tc.input)
		if got := kb.SingleModifierChords(); !slices.Equal(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.input, got, tc.want)
		}
	}
	if (Keybinding(nil)).SingleModifierChords() != nil || (Keybinding(nil)).DispatchChords() != nil {
		t.Error("no chords, no dispatch")
	}
}
