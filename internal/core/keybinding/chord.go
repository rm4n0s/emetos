package keybinding

import "strings"

// Chord is a key pressed together with modifiers. Ctrl is the Ctrl key and Meta the Super
// (Windows) key; Emetos runs on Linux only, so there is no CtrlCmd.
//
// Ported from VS Code: src/vs/base/common/keybindings.ts (KeyCodeChord)
type Chord struct {
	Ctrl  bool
	Shift bool
	Alt   bool
	Meta  bool
	Key   Key
}

// IsModifierKey tells whether the chord is a modifier key alone (or has no key at all).
func (c Chord) IsModifierKey() bool {
	return c.Key == Unknown || c.Key.IsModifier()
}

// isDuplicateModifierCase tells whether the key is a modifier that is also a modifier flag
// of the chord, as in "ctrl+ctrl".
func (c Chord) isDuplicateModifierCase() bool {
	return (c.Ctrl && c.Key == Ctrl) || (c.Shift && c.Key == Shift) ||
		(c.Alt && c.Key == Alt) || (c.Meta && c.Key == Meta)
}

// Dispatch is the string the resolver matches a pressed chord with: the modifiers in the
// order ctrl, shift, alt, meta, then the name of the key in the UI, such as "ctrl+shift+K".
// It is empty for a modifier key alone.
//
// Ported from VS Code: src/vs/platform/keybinding/common/usLayoutResolvedKeybinding.ts
func (c Chord) Dispatch() string {
	if c.IsModifierKey() {
		return ""
	}
	var b strings.Builder
	if c.Ctrl {
		b.WriteString("ctrl+")
	}
	if c.Shift {
		b.WriteString("shift+")
	}
	if c.Alt {
		b.WriteString("alt+")
	}
	if c.Meta {
		b.WriteString("meta+")
	}
	b.WriteString(c.Key.String())
	return b.String()
}

// singleModifier is "ctrl", "shift", "alt" or "meta" when the chord is that modifier key
// alone, and empty otherwise.
func (c Chord) singleModifier() string {
	switch {
	case c.Key == Ctrl && !c.Shift && !c.Alt && !c.Meta:
		return "ctrl"
	case c.Key == Shift && !c.Ctrl && !c.Alt && !c.Meta:
		return "shift"
	case c.Key == Alt && !c.Ctrl && !c.Shift && !c.Meta:
		return "alt"
	case c.Key == Meta && !c.Ctrl && !c.Shift && !c.Alt:
		return "meta"
	}
	return ""
}

// modifierLabels are the words a label provider prints for the modifiers.
type modifierLabels struct {
	ctrl, shift, alt, meta string
	separator              string
}

// Linux label providers.
//
// Ported from VS Code: src/vs/base/common/keybindingLabels.ts
var (
	uiLabels       = modifierLabels{ctrl: "Ctrl", shift: "Shift", alt: "Alt", meta: "Super", separator: "+"}
	ariaLabels     = modifierLabels{ctrl: "Control", shift: "Shift", alt: "Alt", meta: "Super", separator: "+"}
	settingsLabels = modifierLabels{ctrl: "ctrl", shift: "shift", alt: "alt", meta: "meta", separator: "+"}
)

// label prints the chord with the modifiers of labels and the given name of the key.
func (c Chord) label(key string, labels modifierLabels) string {
	var parts []string
	if c.Ctrl {
		parts = append(parts, labels.ctrl)
	}
	if c.Shift {
		parts = append(parts, labels.shift)
	}
	if c.Alt {
		parts = append(parts, labels.alt)
	}
	if c.Meta {
		parts = append(parts, labels.meta)
	}
	if key != "" {
		parts = append(parts, key)
	}
	return strings.Join(parts, labels.separator)
}

// Label is the chord as the UI shows it: "Ctrl+Shift+K".
func (c Chord) Label() string {
	key := c.Key.String()
	if c.isDuplicateModifierCase() {
		key = ""
	}
	return c.label(key, uiLabels)
}

// AriaLabel is the chord as a screen reader should read it: "Control+Shift+K".
func (c Chord) AriaLabel() string {
	key := c.Key.String()
	if c.isDuplicateModifierCase() {
		key = ""
	}
	return c.label(key, ariaLabels)
}

// UserSettingsLabel is the chord as keybindings.json spells it: "ctrl+shift+k".
func (c Chord) UserSettingsLabel() string {
	key := c.Key.UserSettings()
	if c.isDuplicateModifierCase() {
		key = ""
	}
	return c.label(key, settingsLabels)
}

// Keybinding is a sequence of chords: one chord, or several pressed one after the other
// (such as ctrl+k ctrl+c). A nil Keybinding is no key at all.
//
// Ported from VS Code: src/vs/base/common/keybindings.ts (Keybinding)
type Keybinding []Chord

// Equal tells whether two keybindings are the same sequence.
func (k Keybinding) Equal(o Keybinding) bool {
	if len(k) != len(o) {
		return false
	}
	for i := range k {
		if k[i] != o[i] {
			return false
		}
	}
	return true
}

// join prints every chord with f and joins them with a space.
func (k Keybinding) join(f func(Chord) string) string {
	parts := make([]string, len(k))
	for i, c := range k {
		parts[i] = f(c)
	}
	return strings.Join(parts, " ")
}

// Label is the keybinding as the UI shows it: "Ctrl+K Ctrl+C".
func (k Keybinding) Label() string { return k.join(Chord.Label) }

// AriaLabel is the keybinding as a screen reader should read it.
func (k Keybinding) AriaLabel() string { return k.join(Chord.AriaLabel) }

// UserSettingsLabel is the keybinding as keybindings.json spells it: "ctrl+k ctrl+c".
func (k Keybinding) UserSettingsLabel() string { return k.join(Chord.UserSettingsLabel) }

// DispatchChords are the strings the resolver matches, one per chord. It is nil when the
// keybinding has no chords or one of them is a modifier key alone.
func (k Keybinding) DispatchChords() []string {
	out := make([]string, len(k))
	for i, c := range k {
		d := c.Dispatch()
		if d == "" {
			return nil
		}
		out[i] = d
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SingleModifierChords are the strings for a keybinding made of modifier keys alone
// ("ctrl", "shift", "alt", "meta"), such as the double press "shift shift". It is nil when
// some chord is not a lone modifier.
func (k Keybinding) SingleModifierChords() []string {
	out := make([]string, len(k))
	for i, c := range k {
		m := c.singleModifier()
		if m == "" {
			return nil
		}
		out[i] = m
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
