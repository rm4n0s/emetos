package keybinding

import (
	"strings"
	"time"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// ChordTimeout is how long the dispatcher waits for the next chord of a sequence before it
// forgets the chords pressed so far.
const ChordTimeout = 5 * time.Second

// Recorder receives every key press while the dispatcher is in recording mode, modifier-only
// presses included (the keybindings UI uses it to record a key to search or to bind).
type Recorder interface {
	Record(press Chord)
}

// Outcome is what the dispatcher decided for one key press.
type Outcome struct {
	// Handled tells that the key was used (a command runs, a chord is pending, the press
	// was recorded, or a chord was cancelled), so the widget must not see it. When false the
	// key is left alone and Glutter may type its character.
	Handled bool
	// Command is the command to run, empty when there is none.
	Command string
	// Args are the arguments of the command.
	Args any
	// Bubble is set when the keybinding's command id started with "^": run the command but
	// let other handlers see the key too.
	Bubble bool
	// Status is a message for the status bar ("(Ctrl+K) was pressed. Waiting for second key of
	// chord..."); empty when the press says nothing.
	Status string
	// ClearStatus is set when a status message of an earlier press is to be removed (the
	// chord ended).
	ClearStatus bool
}

// pendingChord is a chord pressed that waits for the rest of its sequence.
type pendingChord struct {
	dispatch string
	label    string
}

// Dispatcher turns key presses into commands: it keeps the chords pressed so far, asks the
// resolver what the sequence means, and tells the caller what to do. It does not run
// commands itself and is not safe for concurrent use: call it from the UI thread.
//
// Ported from VS Code: src/vs/platform/keybinding/common/abstractKeybindingService.ts
type Dispatcher struct {
	resolver    *Resolver
	now         func() time.Time
	pending     []pendingChord
	lastChordAt time.Time
	recorder    Recorder
}

// NewDispatcher makes a dispatcher over a resolver. now is the clock for the chord timeout;
// nil means time.Now.
func NewDispatcher(r *Resolver, now func() time.Time) *Dispatcher {
	if now == nil {
		now = time.Now
	}
	return &Dispatcher{resolver: r, now: now}
}

// SetResolver replaces the keymap (after keybindings.json changed) and forgets pending
// chords.
func (d *Dispatcher) SetResolver(r *Resolver) {
	d.resolver = r
	d.pending = nil
}

// InChordMode tells whether a chord was pressed and the dispatcher waits for the next.
func (d *Dispatcher) InChordMode() bool { return len(d.pending) > 0 }

// PendingLabel is the chords pressed so far as the UI shows them, "Ctrl+K, Ctrl+B"; empty
// when not in chord mode.
func (d *Dispatcher) PendingLabel() string {
	labels := make([]string, len(d.pending))
	for i, p := range d.pending {
		labels[i] = p.label
	}
	return strings.Join(labels, ", ")
}

// LeaveChordMode forgets the chords pressed so far.
func (d *Dispatcher) LeaveChordMode() { d.pending = nil }

// Expire leaves chord mode when no chord was pressed for ChordTimeout. The UI calls it
// from a timer (VS Code checks every 500 ms). It reports whether it left chord mode.
func (d *Dispatcher) Expire() bool {
	if len(d.pending) > 0 && d.now().Sub(d.lastChordAt) > ChordTimeout {
		d.pending = nil
		return true
	}
	return false
}

// SetRecorder starts recording mode: every press goes to r, and nothing is resolved. A nil
// recorder ends it. Starting it forgets pending chords.
func (d *Dispatcher) SetRecorder(r Recorder) {
	d.recorder = r
	d.pending = nil
}

// Recording tells whether the dispatcher is in recording mode.
func (d *Dispatcher) Recording() bool { return d.recorder != nil }

// Dispatch resolves a key press against the keymap in the context ctx of the focused
// widget. A press of a modifier key alone does nothing and keeps a pending chord.
func (d *Dispatcher) Dispatch(ctx contextkey.Context, press Chord) Outcome {
	if d.recorder != nil {
		d.recorder.Record(press)
		return Outcome{Handled: true}
	}
	dispatch := press.Dispatch()
	if dispatch == "" {
		return Outcome{}
	}
	label := press.Label()
	currentChords := make([]string, len(d.pending))
	for i, p := range d.pending {
		currentChords[i] = p.dispatch
	}
	res := d.resolver.Resolve(ctx, currentChords, dispatch)

	switch res.Kind {
	case MoreChordsNeeded:
		d.pending = append(d.pending, pendingChord{dispatch: dispatch, label: label})
		d.lastChordAt = d.now()
		status := "(" + label + ") was pressed. Waiting for second key of chord..."
		if len(d.pending) > 1 {
			status = "(" + d.PendingLabel() + ") was pressed. Waiting for next key of chord..."
		}
		return Outcome{Handled: true, Status: status}
	case KbFound:
		if res.Command == "" {
			return d.noCommand(label)
		}
		out := Outcome{Handled: !res.Bubble, Command: res.Command, Args: res.Args, Bubble: res.Bubble}
		if d.InChordMode() {
			d.pending = nil
			out.ClearStatus = true
		}
		return out
	default:
		return d.noCommand(label)
	}
}

// noCommand ends chord mode when the press does not complete a keybinding. Outside chord
// mode the key is left alone.
func (d *Dispatcher) noCommand(label string) Outcome {
	if !d.InChordMode() {
		return Outcome{}
	}
	status := "The key combination (" + d.PendingLabel() + ", " + label + ") is not a command."
	d.pending = nil
	return Outcome{Handled: true, Status: status, ClearStatus: true}
}

// KeyRecorder is a Recorder that collects the chords of a keybinding: each press of a key
// with its modifiers adds a chord, up to Max chords, after which the next press starts over.
// Modifier-only presses are not chords; Modifiers shows them while the user holds the keys.
type KeyRecorder struct {
	// Max is the number of chords a keybinding can have; zero means 2.
	Max       int
	chords    Keybinding
	modifiers Chord
}

// Record adds a press.
func (r *KeyRecorder) Record(press Chord) {
	limit := r.Max
	if limit <= 0 {
		limit = 2
	}
	if press.Key == Unknown {
		return
	}
	if press.Key.IsModifier() {
		r.modifiers = Chord{Ctrl: press.Ctrl, Shift: press.Shift, Alt: press.Alt, Meta: press.Meta}
		return
	}
	r.modifiers = Chord{}
	if len(r.chords) >= limit {
		r.chords = nil
	}
	r.chords = append(r.chords, press)
}

// Keybinding is the chords recorded so far.
func (r *KeyRecorder) Keybinding() Keybinding { return append(Keybinding(nil), r.chords...) }

// Modifiers are the modifier keys held at the last modifier-only press, as a chord with no
// key; it is cleared by the next key.
func (r *KeyRecorder) Modifiers() Chord { return r.modifiers }

// Reset forgets everything recorded.
func (r *KeyRecorder) Reset() {
	r.chords = nil
	r.modifiers = Chord{}
}
