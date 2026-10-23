package keybinding

import (
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// press makes the chord of a one-chord keybinding.
func press(t *testing.T, spec string) Chord {
	t.Helper()
	kb := mustKB(t, spec)
	if len(kb) != 1 {
		t.Fatalf("%q is not one chord", spec)
	}
	return kb[0]
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func newTestDispatcher(t *testing.T) (*Dispatcher, *fakeClock) {
	t.Helper()
	items := []Item{
		kbItem(t, "ctrl+s", "save", nil, nil, true),
		kbItem(t, "ctrl+k ctrl+c", "comment", nil, nil, true),
		kbItem(t, "ctrl+k ctrl+u", "uncomment", map[string]any{"a": 1.0}, nil, true),
		kbItem(t, "ctrl+k a b", "long", nil, nil, true),
		kbItem(t, "ctrl+q", "^bubble", nil, nil, true),
		kbItem(t, "ctrl+e", "edit", nil, contextkey.Has("editing"), true),
		kbItem(t, "ctrl+w", "", nil, nil, true),
	}
	clock := &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	return NewDispatcher(NewResolver(items, nil, nil), clock.Now), clock
}

func TestDispatchSingleChord(t *testing.T) {
	d, _ := newTestDispatcher(t)
	keys := contextkey.MapContext{}

	out := d.Dispatch(keys, press(t, "ctrl+s"))
	if !out.Handled || out.Command != "save" || out.Bubble || out.Status != "" {
		t.Errorf("ctrl+s: %+v", out)
	}
	if out := d.Dispatch(keys, press(t, "ctrl+x")); out.Handled || out.Command != "" {
		t.Errorf("an unbound key is left alone: %+v", out)
	}
	if out := d.Dispatch(keys, press(t, "ctrl+e")); out.Handled {
		t.Errorf("the condition does not hold: %+v", out)
	}
	if out := d.Dispatch(contextkey.MapContext{"editing": true}, press(t, "ctrl+e")); out.Command != "edit" {
		t.Errorf("the condition holds: %+v", out)
	}
	if out := d.Dispatch(keys, press(t, "ctrl+q")); out.Command != "bubble" || !out.Bubble || out.Handled {
		t.Errorf("a ^ command runs but the key is not consumed: %+v", out)
	}
	if out := d.Dispatch(keys, press(t, "ctrl+w")); out.Handled || out.Command != "" {
		t.Errorf("a binding without a command leaves the key: %+v", out)
	}
}

func TestDispatchChords(t *testing.T) {
	d, _ := newTestDispatcher(t)
	keys := contextkey.MapContext{}

	out := d.Dispatch(keys, press(t, "ctrl+k"))
	if !out.Handled || out.Command != "" || out.Status != "(Ctrl+K) was pressed. Waiting for second key of chord..." {
		t.Fatalf("first chord: %+v", out)
	}
	if !d.InChordMode() || d.PendingLabel() != "Ctrl+K" {
		t.Errorf("pending: %v %q", d.InChordMode(), d.PendingLabel())
	}

	// A modifier key alone neither cancels the chord nor does anything.
	for _, m := range []string{"ctrl", "shift", "alt", "meta"} {
		if out := d.Dispatch(keys, Chord{Key: press(t, m).Key}); out.Handled || out.Status != "" || out.ClearStatus {
			t.Errorf("%s alone: %+v", m, out)
		}
		if out := d.Dispatch(keys, Chord{Ctrl: true, Key: press(t, m).Key}); out.Handled {
			t.Errorf("ctrl+%s alone: %+v", m, out)
		}
	}
	if !d.InChordMode() {
		t.Fatal("modifier keys cancelled the chord")
	}

	// The chord goes on after the modifier presses.
	out = d.Dispatch(keys, press(t, "ctrl+c"))
	if out.Command != "comment" || d.InChordMode() {
		t.Fatalf("ctrl+k ctrl+c: %+v", out)
	}
}

func TestDispatchSecondChord(t *testing.T) {
	d, _ := newTestDispatcher(t)
	keys := contextkey.MapContext{}

	d.Dispatch(keys, press(t, "ctrl+k"))
	out := d.Dispatch(keys, press(t, "ctrl+u"))
	if !out.Handled || out.Command != "uncomment" || !out.ClearStatus || d.InChordMode() {
		t.Errorf("ctrl+k ctrl+u: %+v", out)
	}
	if m, _ := out.Args.(map[string]any); m["a"] != 1.0 {
		t.Errorf("arguments: %v", out.Args)
	}

	d.Dispatch(keys, press(t, "ctrl+k"))
	out = d.Dispatch(keys, press(t, "x"))
	if !out.Handled || out.Command != "" || !out.ClearStatus || d.InChordMode() ||
		out.Status != "The key combination (Ctrl+K, X) is not a command." {
		t.Errorf("a key that completes nothing: %+v", out)
	}
	// After that, the first chord is a first chord again.
	if out := d.Dispatch(keys, press(t, "ctrl+k")); out.Status == "" {
		t.Errorf("chord mode did not start again: %+v", out)
	}
}

func TestDispatchLongChord(t *testing.T) {
	d, _ := newTestDispatcher(t)
	keys := contextkey.MapContext{}

	d.Dispatch(keys, press(t, "ctrl+k"))
	out := d.Dispatch(keys, press(t, "a"))
	if out.Status != "(Ctrl+K, A) was pressed. Waiting for next key of chord..." || !out.Handled {
		t.Errorf("second chord of three: %+v", out)
	}
	out = d.Dispatch(keys, press(t, "b"))
	if out.Command != "long" || d.InChordMode() {
		t.Errorf("third chord: %+v", out)
	}

	d.Dispatch(keys, press(t, "ctrl+k"))
	d.Dispatch(keys, press(t, "a"))
	out = d.Dispatch(keys, press(t, "c"))
	if out.Status != "The key combination (Ctrl+K, A, C) is not a command." {
		t.Errorf("wrong third chord: %+v", out)
	}
}

func TestDispatchChordTimeout(t *testing.T) {
	d, clock := newTestDispatcher(t)
	keys := contextkey.MapContext{}

	if d.Expire() {
		t.Error("nothing to expire")
	}
	d.Dispatch(keys, press(t, "ctrl+k"))
	clock.now = clock.now.Add(4 * time.Second)
	if d.Expire() || !d.InChordMode() {
		t.Error("the chord must survive 4 s")
	}
	clock.now = clock.now.Add(2 * time.Second)
	if !d.Expire() || d.InChordMode() {
		t.Error("the chord must expire after 5 s")
	}
	d.Dispatch(keys, press(t, "ctrl+k"))
	d.LeaveChordMode()
	if d.InChordMode() || d.PendingLabel() != "" {
		t.Error("LeaveChordMode")
	}
}

func TestDispatchSetResolver(t *testing.T) {
	d, _ := newTestDispatcher(t)
	keys := contextkey.MapContext{}
	d.Dispatch(keys, press(t, "ctrl+k"))
	d.SetResolver(NewResolver([]Item{kbItem(t, "ctrl+s", "other", nil, nil, true)}, nil, nil))
	if d.InChordMode() {
		t.Error("a new keymap forgets pending chords")
	}
	if out := d.Dispatch(keys, press(t, "ctrl+s")); out.Command != "other" {
		t.Errorf("new keymap: %+v", out)
	}
}

type sliceRecorder struct{ got []Chord }

func (r *sliceRecorder) Record(c Chord) { r.got = append(r.got, c) }

func TestDispatchRecordingMode(t *testing.T) {
	d, _ := newTestDispatcher(t)
	keys := contextkey.MapContext{}
	d.Dispatch(keys, press(t, "ctrl+k"))

	rec := &sliceRecorder{}
	d.SetRecorder(rec)
	if !d.Recording() || d.InChordMode() {
		t.Fatalf("recording: %v, chord mode %v", d.Recording(), d.InChordMode())
	}
	for _, spec := range []string{"ctrl+s", "ctrl+k", "shift", "x"} {
		out := d.Dispatch(keys, press(t, spec))
		if !out.Handled || out.Command != "" || out.Status != "" {
			t.Errorf("recording %s: %+v", spec, out)
		}
	}
	if len(rec.got) != 4 || rec.got[0] != press(t, "ctrl+s") || rec.got[2].Key != Shift {
		t.Errorf("recorded %v", rec.got)
	}
	if d.InChordMode() {
		t.Error("recording must not start a chord")
	}

	d.SetRecorder(nil)
	if d.Recording() {
		t.Error("recording stopped")
	}
	if out := d.Dispatch(keys, press(t, "ctrl+s")); out.Command != "save" {
		t.Errorf("after recording: %+v", out)
	}
}

func TestKeyRecorder(t *testing.T) {
	var r KeyRecorder
	r.Record(Chord{Ctrl: true, Key: Ctrl})
	if r.Modifiers() != (Chord{Ctrl: true}) || len(r.Keybinding()) != 0 {
		t.Errorf("a modifier alone is not a chord: %v %+v", r.Keybinding(), r.Modifiers())
	}
	r.Record(Chord{Key: Unknown})
	r.Record(press(t, "ctrl+k"))
	if r.Modifiers() != (Chord{}) {
		t.Error("a key clears the held modifiers")
	}
	r.Record(press(t, "ctrl+c"))
	if got := r.Keybinding().UserSettingsLabel(); got != "ctrl+k ctrl+c" {
		t.Errorf("two chords: %q", got)
	}
	r.Record(press(t, "x"))
	if got := r.Keybinding().UserSettingsLabel(); got != "x" {
		t.Errorf("a third chord starts over: %q", got)
	}
	r.Reset()
	if len(r.Keybinding()) != 0 {
		t.Error("Reset")
	}
	three := KeyRecorder{Max: 3}
	for _, s := range []string{"a", "b", "c", "d"} {
		three.Record(press(t, s))
	}
	if got := three.Keybinding().UserSettingsLabel(); got != "d" {
		t.Errorf("Max 3: %q", got)
	}
}
