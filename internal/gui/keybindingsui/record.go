package keybindingsui

import (
	"strings"

	"github.com/rm4n0s/emetos/internal/core/keybinding"
)

// MaxChords is the number of chords a keybinding recorded here can have (VS Code's limit for this widget).
const MaxChords = 2

// Recorder collects the chords of a keybinding from the key presses that the dispatcher hands over while it is in
// recording mode (keybinding.Dispatcher.SetRecorder). It is VS Code's KeybindingsSearchWidget: a press of a key
// with its modifiers adds a chord, up to MaxChords, and the next one starts over; a press of modifiers alone is a
// chord that is not complete, and the next key replaces it. Enter (when not recorded) and Escape, pressed with no
// modifier, are the end of the recording and not chords.
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/keybindingWidgets.ts (KeybindingsSearchWidget)
type Recorder struct {
	chords keybinding.Keybinding
	// RecordEnter makes Enter a key like the others; without it, Enter calls OnEnter.
	RecordEnter bool
	// OnChange is called after the chords changed.
	OnChange func()
	// OnEnter is called for an Enter that is not recorded, OnEscape for an Escape with no modifiers.
	OnEnter, OnEscape func()
}

// Record implements keybinding.Recorder.
func (r *Recorder) Record(press keybinding.Chord) {
	plain := !press.Ctrl && !press.Shift && !press.Alt && !press.Meta
	if plain && press.Key == keybinding.Enter && !r.RecordEnter {
		if r.OnEnter != nil {
			r.OnEnter()
		}
		return
	}
	if plain && press.Key == keybinding.Escape {
		if r.OnEscape != nil {
			r.OnEscape()
		}
		return
	}
	if press.Key == keybinding.Unknown {
		return
	}
	// a press of modifiers alone is a chord that is not complete: the next press replaces it
	if n := len(r.chords); n > 0 && r.chords[n-1].IsModifierKey() {
		r.chords[n-1] = press
	} else {
		if len(r.chords) >= MaxChords {
			r.chords = nil
		}
		r.chords = append(r.chords, press)
	}
	if r.OnChange != nil {
		r.OnChange()
	}
}

// Chords are the chords recorded so far, the last of which may be a press of modifiers alone.
func (r *Recorder) Chords() keybinding.Keybinding {
	return append(keybinding.Keybinding(nil), r.chords...)
}

// Keybinding is the chords that are complete: those with a key besides the modifiers.
func (r *Recorder) Keybinding() keybinding.Keybinding {
	var out keybinding.Keybinding
	for _, c := range r.chords {
		if !c.IsModifierKey() {
			out = append(out, c)
		}
	}
	return out
}

// Clear forgets the chords.
func (r *Recorder) Clear() {
	r.chords = nil
	if r.OnChange != nil {
		r.OnChange()
	}
}

// Empty tells whether nothing was recorded.
func (r *Recorder) Empty() bool { return len(r.chords) == 0 }

// Text is the chords as keybindings.json writes them ("ctrl+k ctrl+c"), a press of modifiers alone as the
// modifiers ("ctrl+shift").
func (r *Recorder) Text() string { return r.chords.UserSettingsLabel() }

// Quoted is Text in double quotes, what the search box shows to look for exactly those keys; empty while nothing
// was recorded.
func (r *Recorder) Quoted() string {
	text := r.Text()
	if text == "" {
		return ""
	}
	return `"` + strings.TrimSpace(text) + `"`
}
