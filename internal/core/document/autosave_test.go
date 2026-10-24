package document

import (
	"context"
	"math"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/fileio"
)

// fakeClock is a clock that moves when a test says so.
type fakeClock struct {
	now    time.Duration
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Duration
	f       func()
	stopped bool
	fired   bool
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) func() {
	t := &fakeTimer{at: c.now + d, f: f}
	c.timers = append(c.timers, t)
	return func() { t.stopped = true }
}

// pending is the number of timers that have neither fired nor been stopped.
func (c *fakeClock) pending() int {
	n := 0
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

// advance moves the clock and fires the timers that fall due, in order.
func (c *fakeClock) advance(d time.Duration) {
	target := c.now + d
	for {
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.stopped && !t.fired && t.at <= target && (next == nil || t.at < next.at) {
				next = t
			}
		}
		if next == nil {
			break
		}
		c.now = next.at
		next.fired = true
		next.f()
	}
	c.now = target
}

// fakeHost is a window: it writes the documents for real, or fails.
type fakeHost struct {
	t         *testing.T
	auto      *AutoSave
	cfg       AutoSaveConfig
	cfgFor    map[*Document]AutoSaveConfig
	inside    map[*Document]bool
	errors    map[*Document]bool
	fail      bool
	saves     []SaveReason
	saved     []*Document
	reasons   []SaveReason
	participa func(*Document, SaveReason) error
}

func (h *fakeHost) Config(d *Document) AutoSaveConfig {
	if c, ok := h.cfgFor[d]; ok {
		return c
	}
	return h.cfg
}

func (h *fakeHost) InWorkspace(d *Document) bool {
	in, ok := h.inside[d]
	return !ok || in
}

func (h *fakeHost) HasErrors(d *Document) bool { return h.errors[d] }

func (h *fakeHost) Save(d *Document, reason SaveReason) {
	h.t.Helper()
	h.saves = append(h.saves, reason)
	h.saved = append(h.saved, d)
	if h.fail {
		h.auto.SaveFailed(d)
		return
	}
	opts := SaveOptions{Reason: reason}
	if h.participa != nil {
		opts.Participate = func(d *Document, r SaveReason) error {
			h.reasons = append(h.reasons, r)
			return h.participa(d, r)
		}
	}
	if _, err := d.Save(context.Background(), opts); err != nil {
		h.t.Fatalf("Save: %v", err)
	}
}

type autoFixture struct {
	t     *testing.T
	clock *fakeClock
	host  *fakeHost
	auto  *AutoSave
	dir   string
}

func newAutoFixture(t *testing.T, cfg AutoSaveConfig) *autoFixture {
	t.Helper()
	f := &autoFixture{t: t, clock: &fakeClock{}, dir: t.TempDir()}
	f.host = &fakeHost{t: t, cfg: cfg, inside: map[*Document]bool{}, errors: map[*Document]bool{}}
	f.auto = NewAutoSave(f.clock, f.host)
	f.host.auto = f.auto
	t.Cleanup(f.auto.Dispose)
	return f
}

// file makes a document of a file on disk and tracks it.
func (f *autoFixture) file(name, text string) *Document {
	f.t.Helper()
	path := filepath.Join(f.dir, name)
	writeFile(f.t, path, text, 0o644)
	d := openFile(f.t, path)
	f.auto.Track(d)
	return d
}

func after(delay time.Duration) AutoSaveConfig {
	return AutoSaveConfig{Mode: AutoSaveAfterDelay, Delay: delay}
}

func TestAutoSaveAfterDelayWaitsForTheLastEdit(t *testing.T) {
	f := newAutoFixture(t, after(time.Second))
	d := f.file("a.txt", "one\n")
	edits := []time.Duration{0, 500 * time.Millisecond, 900 * time.Millisecond}
	for _, at := range edits {
		f.clock.advance(at - f.clock.now)
		appendText(t, d, "x")
	}
	// the last edit was at 900 ms: nothing is saved before 1900 ms
	f.clock.advance(999 * time.Millisecond)
	if len(f.host.saves) != 0 || !d.Dirty() {
		t.Fatalf("saved too early: %v", f.host.saves)
	}
	f.clock.advance(time.Millisecond)
	if !slices.Equal(f.host.saves, []SaveReason{SaveAuto}) || d.Dirty() {
		t.Fatalf("saves = %v, dirty = %v", f.host.saves, d.Dirty())
	}
	if got := string(readDisk(t, d.URI().Path)); got != "one\nxxx" {
		t.Errorf("file = %q", got)
	}
	if f.clock.pending() != 0 {
		t.Errorf("%d timers are left", f.clock.pending())
	}
}

func TestAutoSaveTimerIsCancelledWhenTheDocumentStopsBeingDirty(t *testing.T) {
	tests := []struct {
		name string
		stop func(*testing.T, *autoFixture, *Document)
	}{
		{"a manual save", func(t *testing.T, _ *autoFixture, d *Document) {
			if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
				t.Fatal(err)
			}
		}},
		{"an undo back to the saved text", func(t *testing.T, _ *autoFixture, d *Document) { undo(t, d) }},
		{"a revert", func(t *testing.T, _ *autoFixture, d *Document) {
			if err := d.Revert(context.Background(), fileio.ReadOptions{}); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAutoFixture(t, after(time.Second))
			d := f.file("a.txt", "one\n")
			appendText(t, d, "x")
			if f.clock.pending() != 1 {
				t.Fatalf("%d timers after an edit", f.clock.pending())
			}
			f.clock.advance(400 * time.Millisecond)
			tt.stop(t, f, d)
			if f.clock.pending() != 0 {
				t.Errorf("%d timers are left", f.clock.pending())
			}
			f.clock.advance(time.Hour)
			if len(f.host.saves) != 0 {
				t.Errorf("auto save ran: %v", f.host.saves)
			}
		})
	}
}

func TestAutoSaveDelay(t *testing.T) {
	tests := []struct {
		in   float64
		want time.Duration
	}{
		{0, 0}, {1, time.Millisecond}, {1000, time.Second}, {2500.5, 2500500 * time.Microsecond},
		{-1, DefaultAutoSaveDelay}, {-0.5, DefaultAutoSaveDelay}, {1e30, DefaultAutoSaveDelay},
	}
	for _, tt := range tests {
		if got := AutoSaveDelay(tt.in); got != tt.want {
			t.Errorf("AutoSaveDelay(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
	if got := AutoSaveDelay(math.NaN()); got != DefaultAutoSaveDelay {
		t.Errorf("AutoSaveDelay(NaN) = %v", got)
	}
}

func TestAutoSaveWithADelayOfZeroSavesOnTheNextTick(t *testing.T) {
	f := newAutoFixture(t, after(0))
	d := f.file("a.txt", "one\n")
	appendText(t, d, "x")
	f.clock.advance(0)
	if len(f.host.saves) != 1 {
		t.Fatalf("saves = %v", f.host.saves)
	}
}

func TestParseAutoSaveMode(t *testing.T) {
	tests := []struct {
		in   string
		want AutoSaveMode
		ok   bool
	}{
		{"off", AutoSaveOff, true}, {"afterDelay", AutoSaveAfterDelay, true},
		{"onFocusChange", AutoSaveOnFocusChange, true}, {"onWindowChange", AutoSaveOnWindowChange, true},
		{"", AutoSaveOff, false}, {"AfterDelay", AutoSaveOff, false}, {"always", AutoSaveOff, false},
	}
	for _, tt := range tests {
		got, ok := ParseAutoSaveMode(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseAutoSaveMode(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
		if ok && got.String() != tt.in {
			t.Errorf("%v.String() = %q, want %q", got, got.String(), tt.in)
		}
	}
}

func TestAutoSaveEventsPerMode(t *testing.T) {
	type event string
	const (
		timer  event = "the timer ends"
		focus  event = "the editor loses the focus"
		active event = "another editor becomes active"
		window event = "the window loses the focus"
	)
	tests := []struct {
		mode AutoSaveMode
		ev   event
		want []SaveReason
	}{
		{AutoSaveOff, timer, nil}, {AutoSaveOff, focus, nil}, {AutoSaveOff, active, nil}, {AutoSaveOff, window, nil},
		{AutoSaveAfterDelay, timer, []SaveReason{SaveAuto}}, {AutoSaveAfterDelay, focus, nil},
		{AutoSaveAfterDelay, active, nil}, {AutoSaveAfterDelay, window, nil},
		{AutoSaveOnFocusChange, timer, nil}, {AutoSaveOnFocusChange, focus, []SaveReason{SaveFocusChange}},
		{AutoSaveOnFocusChange, active, []SaveReason{SaveFocusChange}}, {AutoSaveOnFocusChange, window, []SaveReason{SaveWindowChange}},
		{AutoSaveOnWindowChange, timer, nil}, {AutoSaveOnWindowChange, focus, nil},
		{AutoSaveOnWindowChange, active, nil}, {AutoSaveOnWindowChange, window, []SaveReason{SaveWindowChange}},
	}
	for _, tt := range tests {
		t.Run(tt.mode.String()+"/"+string(tt.ev), func(t *testing.T) {
			f := newAutoFixture(t, AutoSaveConfig{Mode: tt.mode, Delay: time.Second})
			d := f.file("a.txt", "one\n")
			appendText(t, d, "x")
			switch tt.ev {
			case timer:
				f.clock.advance(time.Hour)
			case focus, active: // the same call: the group tells the controller which editor went
				f.auto.FocusLost(d)
			case window:
				f.auto.WindowFocusLost()
			}
			if !slices.Equal(f.host.saves, tt.want) {
				t.Fatalf("saves = %v, want %v", f.host.saves, tt.want)
			}
			if d.Dirty() != (len(tt.want) == 0) {
				t.Errorf("dirty = %v", d.Dirty())
			}
			// a clean document is not saved again by the next event
			f.auto.WindowFocusLost()
			f.auto.FocusLost(d)
			f.clock.advance(time.Hour)
			if len(tt.want) > 0 && len(f.host.saves) != len(tt.want) {
				t.Errorf("%d saves for one change", len(f.host.saves))
			}
		})
	}
}

func TestAutoSaveSavesEveryDirtyDocumentWhenTheWindowLosesTheFocus(t *testing.T) {
	f := newAutoFixture(t, AutoSaveConfig{Mode: AutoSaveOnWindowChange})
	a, b, c := f.file("a.txt", "a\n"), f.file("b.txt", "b\n"), f.file("c.txt", "c\n")
	appendText(t, a, "1")
	appendText(t, c, "3")
	f.auto.WindowFocusLost()
	if !slices.Equal(f.host.saved, []*Document{a, c}) {
		t.Errorf("saved %d documents, in the wrong order or the wrong ones", len(f.host.saved))
	}
	if a.Dirty() || b.Dirty() || c.Dirty() {
		t.Error("a document is dirty")
	}
}

func TestAutoSaveEligibility(t *testing.T) {
	tests := []struct {
		name  string
		cfg   AutoSaveConfig
		setup func(f *autoFixture, d *Document)
		want  AutoSaveBlock
	}{
		{"an ordinary file", AutoSaveConfig{Mode: AutoSaveOnWindowChange}, func(*autoFixture, *Document) {}, AutoSaveNotBlocked},
		{"read-only", AutoSaveConfig{Mode: AutoSaveOnWindowChange}, func(_ *autoFixture, d *Document) { d.SetReadOnly(true) }, AutoSaveBlockedReadOnly},
		{"mode off", AutoSaveConfig{}, func(*autoFixture, *Document) {}, AutoSaveBlockedBySettings},
		{"a mode for another event", after(time.Second), func(*autoFixture, *Document) {}, AutoSaveBlockedBySettings},
		{"outside the workspace, only workspace files", AutoSaveConfig{Mode: AutoSaveOnWindowChange, WorkspaceFilesOnly: true},
			func(f *autoFixture, d *Document) { f.host.inside[d] = false }, AutoSaveBlockedOutOfWorkspace},
		{"outside the workspace, any file", AutoSaveConfig{Mode: AutoSaveOnWindowChange},
			func(f *autoFixture, d *Document) { f.host.inside[d] = false }, AutoSaveNotBlocked},
		{"inside the workspace, only workspace files", AutoSaveConfig{Mode: AutoSaveOnWindowChange, WorkspaceFilesOnly: true},
			func(f *autoFixture, d *Document) { f.host.inside[d] = true }, AutoSaveNotBlocked},
		{"errors, when no errors", AutoSaveConfig{Mode: AutoSaveOnWindowChange, WhenNoErrors: true},
			func(f *autoFixture, d *Document) { f.host.errors[d] = true }, AutoSaveBlockedByErrors},
		{"errors, any", AutoSaveConfig{Mode: AutoSaveOnWindowChange},
			func(f *autoFixture, d *Document) { f.host.errors[d] = true }, AutoSaveNotBlocked},
		{"paused", AutoSaveConfig{Mode: AutoSaveOnWindowChange}, func(f *autoFixture, d *Document) { f.auto.SaveFailed(d) }, AutoSaveBlockedPaused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAutoFixture(t, tt.cfg)
			d := f.file("a.txt", "one\n")
			appendText(t, d, "x")
			tt.setup(f, d)
			if got := f.auto.Check(d, SaveWindowChange); got != tt.want {
				t.Fatalf("Check = %v, want %v", got, tt.want)
			}
			f.auto.WindowFocusLost()
			if saved := len(f.host.saves) == 1; saved != (tt.want == AutoSaveNotBlocked) {
				t.Errorf("saves = %v", f.host.saves)
			}
		})
	}
}

func TestAutoSaveNeverSavesAnUntitledOrVirtualDocument(t *testing.T) {
	for name, uri := range map[string]URI{"untitled": UntitledURI(1), "virtual": VirtualURI("settings")} {
		t.Run(name, func(t *testing.T) {
			for _, mode := range []AutoSaveMode{AutoSaveAfterDelay, AutoSaveOnFocusChange, AutoSaveOnWindowChange} {
				f := newAutoFixture(t, AutoSaveConfig{Mode: mode, Delay: time.Millisecond})
				d := New(uri, "", NewOptions())
				f.auto.Track(d)
				edit(t, d, []Edit{ins(1, 0, "text")}, EditOptions{})
				f.clock.advance(time.Hour)
				f.auto.FocusLost(d)
				f.auto.WindowFocusLost()
				if len(f.host.saves) != 0 || f.clock.pending() != 0 {
					t.Errorf("%v: saves = %v, timers = %d", mode, f.host.saves, f.clock.pending())
				}
				if _, ok := f.auto.OnClose(d); ok || f.auto.OnShutdown(d) {
					t.Errorf("%v: an untitled document is saved on close", mode)
				}
			}
		})
	}
}

func TestAutoSaveWaitsForTheErrorsToGo(t *testing.T) {
	f := newAutoFixture(t, AutoSaveConfig{Mode: AutoSaveAfterDelay, Delay: time.Second, WhenNoErrors: true})
	d := f.file("a.txt", "one\n")
	f.host.errors[d] = true
	appendText(t, d, "x")
	f.clock.advance(time.Second)
	if len(f.host.saves) != 0 {
		t.Fatalf("saved a document with errors: %v", f.host.saves)
	}
	// markers change, but the error is still there
	f.auto.MarkersChanged()
	if len(f.host.saves) != 0 {
		t.Fatal("saved with the error still there")
	}
	f.host.errors[d] = false
	f.auto.MarkersChanged()
	if !slices.Equal(f.host.saves, []SaveReason{SaveAuto}) || d.Dirty() {
		t.Fatalf("saves = %v, dirty = %v", f.host.saves, d.Dirty())
	}
	f.auto.MarkersChanged()
	if len(f.host.saves) != 1 {
		t.Error("saved twice")
	}
}

func TestAutoSaveWaitingForErrorsEndsWithANewEditOrTheDocument(t *testing.T) {
	f := newAutoFixture(t, AutoSaveConfig{Mode: AutoSaveOnWindowChange, WhenNoErrors: true})
	d := f.file("a.txt", "one\n")
	f.host.errors[d] = true
	appendText(t, d, "x")
	f.auto.WindowFocusLost()
	f.auto.Untrack(d)
	f.host.errors[d] = false
	f.auto.MarkersChanged()
	if len(f.host.saves) != 0 {
		t.Errorf("a closed document was saved: %v", f.host.saves)
	}
}

func TestAutoSavePausesAfterAFailedSaveAndResumes(t *testing.T) {
	tests := []struct {
		name   string
		resume func(*testing.T, *Document)
	}{
		{"a manual save", func(t *testing.T, d *Document) {
			if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
				t.Fatal(err)
			}
		}},
		{"a revert", func(t *testing.T, d *Document) {
			if err := d.Revert(context.Background(), fileio.ReadOptions{}); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAutoFixture(t, after(time.Second))
			d := f.file("a.txt", "one\n")
			f.host.fail = true
			appendText(t, d, "x")
			f.clock.advance(time.Second)
			if len(f.host.saves) != 1 || !f.auto.Paused(d) {
				t.Fatalf("saves = %v, paused = %v", f.host.saves, f.auto.Paused(d))
			}
			// typing on does not try again: the failure is not repeated on every key
			for range 5 {
				appendText(t, d, "y")
				f.clock.advance(10 * time.Second)
			}
			f.auto.FocusLost(d)
			f.auto.WindowFocusLost()
			if len(f.host.saves) != 1 || f.clock.pending() != 0 {
				t.Fatalf("saves = %v, timers = %d", f.host.saves, f.clock.pending())
			}
			f.host.fail = false
			tt.resume(t, d)
			if f.auto.Paused(d) {
				t.Fatal("still paused")
			}
			appendText(t, d, "z")
			f.clock.advance(time.Second)
			if len(f.host.saves) != 2 || d.Dirty() {
				t.Errorf("saves = %v, dirty = %v", f.host.saves, d.Dirty())
			}
		})
	}
}

func TestAutoSaveConfigChangedAtRuntime(t *testing.T) {
	tests := []struct {
		name     string
		from, to AutoSaveConfig
		saves    []SaveReason
		timers   int
	}{
		{"off to afterDelay saves the dirty document", AutoSaveConfig{}, after(time.Second), []SaveReason{SaveAuto}, 0},
		{"off to onFocusChange", AutoSaveConfig{}, AutoSaveConfig{Mode: AutoSaveOnFocusChange}, []SaveReason{SaveFocusChange}, 0},
		{"off to onWindowChange", AutoSaveConfig{}, AutoSaveConfig{Mode: AutoSaveOnWindowChange}, []SaveReason{SaveWindowChange}, 0},
		{"afterDelay to off cancels the timer", after(time.Second), AutoSaveConfig{}, nil, 0},
		{"afterDelay to onWindowChange cancels the timer", after(time.Second), AutoSaveConfig{Mode: AutoSaveOnWindowChange}, []SaveReason{SaveWindowChange}, 0},
		{"another delay saves too", after(time.Hour), after(time.Second), []SaveReason{SaveAuto}, 0},
		{"the same configuration does nothing", after(time.Second), after(time.Second), nil, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAutoFixture(t, tt.from)
			d := f.file("a.txt", "one\n")
			appendText(t, d, "x")
			f.host.cfg = tt.to
			f.auto.ConfigChanged()
			if !slices.Equal(f.host.saves, tt.saves) {
				t.Errorf("saves = %v, want %v", f.host.saves, tt.saves)
			}
			if f.clock.pending() != tt.timers {
				t.Errorf("%d timers, want %d", f.clock.pending(), tt.timers)
			}
			f.clock.advance(time.Hour * 2)
			if tt.timers == 0 && len(f.host.saves) != len(tt.saves) {
				t.Errorf("a cancelled timer saved: %v", f.host.saves)
			}
		})
	}
}

func TestAutoSaveSwitchingOnDoesNotSaveACleanDocument(t *testing.T) {
	f := newAutoFixture(t, AutoSaveConfig{})
	f.file("a.txt", "one\n")
	f.host.cfg = after(time.Second)
	f.auto.ConfigChanged()
	if len(f.host.saves) != 0 {
		t.Errorf("saves = %v", f.host.saves)
	}
}

func TestAutoSaveFollowsTheConfigurationOfEachDocument(t *testing.T) {
	// files.autoSave can be set for a language
	f := newAutoFixture(t, AutoSaveConfig{})
	a, b := f.file("a.txt", "a\n"), f.file("b.txt", "b\n")
	f.host.cfgFor = map[*Document]AutoSaveConfig{b: after(time.Second)}
	f.host.cfg = AutoSaveConfig{}
	f.auto.ConfigChanged()
	appendText(t, a, "1")
	appendText(t, b, "2")
	f.clock.advance(time.Hour)
	if !slices.Equal(f.host.saved, []*Document{b}) {
		t.Errorf("saved %d documents", len(f.host.saved))
	}
}

func TestAutoSaveUntrackLeavesNothingBehind(t *testing.T) {
	f := newAutoFixture(t, after(time.Second))
	a, b := f.file("a.txt", "a\n"), f.file("b.txt", "b\n")
	appendText(t, a, "1")
	appendText(t, b, "2")
	if f.clock.pending() != 2 {
		t.Fatalf("%d timers", f.clock.pending())
	}
	f.auto.Untrack(a)
	if f.auto.Tracked(a) || f.clock.pending() != 1 {
		t.Errorf("tracked = %v, timers = %d", f.auto.Tracked(a), f.clock.pending())
	}
	// the document is not followed any more
	appendText(t, a, "again")
	if f.clock.pending() != 1 {
		t.Errorf("an untracked document started a timer")
	}
	f.auto.Dispose()
	if f.clock.pending() != 0 || f.auto.Tracked(b) {
		t.Errorf("timers = %d, tracked = %v", f.clock.pending(), f.auto.Tracked(b))
	}
	f.clock.advance(time.Hour)
	if len(f.host.saves) != 0 {
		t.Errorf("saves = %v", f.host.saves)
	}
	f.auto.Untrack(a) // twice is fine
}

func TestAutoSaveTracksADirtyDocumentAtOnceAndOnce(t *testing.T) {
	f := newAutoFixture(t, after(time.Second))
	path := filepath.Join(f.dir, "a.txt")
	writeFile(t, path, "one\n", 0o644)
	d := openFile(t, path)
	appendText(t, d, "x")
	f.auto.Track(d)
	f.auto.Track(d)
	if f.clock.pending() != 1 {
		t.Fatalf("%d timers", f.clock.pending())
	}
	f.clock.advance(time.Second)
	if len(f.host.saves) != 1 {
		t.Errorf("saves = %v", f.host.saves)
	}
}

func TestAutoSaveReasonReachesTheParticipants(t *testing.T) {
	tests := []struct {
		mode AutoSaveMode
		run  func(f *autoFixture, d *Document)
		want SaveReason
	}{
		{AutoSaveAfterDelay, func(f *autoFixture, _ *Document) { f.clock.advance(time.Second) }, SaveAuto},
		{AutoSaveOnFocusChange, func(f *autoFixture, d *Document) { f.auto.FocusLost(d) }, SaveFocusChange},
		{AutoSaveOnFocusChange, func(f *autoFixture, _ *Document) { f.auto.WindowFocusLost() }, SaveWindowChange},
		{AutoSaveOnWindowChange, func(f *autoFixture, _ *Document) { f.auto.WindowFocusLost() }, SaveWindowChange},
	}
	for _, tt := range tests {
		t.Run(tt.mode.String()+"/"+tt.want.String(), func(t *testing.T) {
			f := newAutoFixture(t, AutoSaveConfig{Mode: tt.mode, Delay: time.Second})
			f.host.participa = func(*Document, SaveReason) error { return nil }
			d := f.file("a.txt", "one\n")
			appendText(t, d, "x")
			tt.run(f, d)
			if !slices.Equal(f.host.reasons, []SaveReason{tt.want}) {
				t.Errorf("participants saw %v, want %v", f.host.reasons, tt.want)
			}
		})
	}
}

func TestAutoSaveOnCloseAndOnShutdown(t *testing.T) {
	tests := []struct {
		name         string
		cfg          AutoSaveConfig
		setup        func(f *autoFixture, d *Document)
		closeReason  SaveReason
		closeSaves   bool
		shutdownSave bool
	}{
		{"off", AutoSaveConfig{}, nil, SaveExplicit, false, false},
		{"afterDelay asks on close but saves on shutdown", after(time.Second), nil, SaveExplicit, false, true},
		{"onFocusChange saves on close", AutoSaveConfig{Mode: AutoSaveOnFocusChange}, nil, SaveFocusChange, true, true},
		{"onWindowChange saves on close", AutoSaveConfig{Mode: AutoSaveOnWindowChange}, nil, SaveWindowChange, true, true},
		{"onFocusChange outside the workspace", AutoSaveConfig{Mode: AutoSaveOnFocusChange, WorkspaceFilesOnly: true},
			func(f *autoFixture, d *Document) { f.host.inside[d] = false }, SaveExplicit, false, false},
		{"onFocusChange with errors", AutoSaveConfig{Mode: AutoSaveOnFocusChange, WhenNoErrors: true},
			func(f *autoFixture, d *Document) { f.host.errors[d] = true }, SaveExplicit, false, false},
		{"onFocusChange after a failed save", AutoSaveConfig{Mode: AutoSaveOnFocusChange},
			func(f *autoFixture, d *Document) { f.auto.SaveFailed(d) }, SaveExplicit, false, false},
		{"onFocusChange read-only", AutoSaveConfig{Mode: AutoSaveOnFocusChange},
			func(_ *autoFixture, d *Document) { d.SetReadOnly(true) }, SaveExplicit, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAutoFixture(t, tt.cfg)
			d := f.file("a.txt", "one\n")
			if tt.setup != nil {
				tt.setup(f, d)
			}
			reason, ok := f.auto.OnClose(d)
			if ok != tt.closeSaves || (ok && reason != tt.closeReason) {
				t.Errorf("OnClose = %v, %v; want %v, %v", reason, ok, tt.closeReason, tt.closeSaves)
			}
			if got := f.auto.OnShutdown(d); got != tt.shutdownSave {
				t.Errorf("OnShutdown = %v, want %v", got, tt.shutdownSave)
			}
		})
	}
}

func TestSaveReasonString(t *testing.T) {
	for r, want := range map[SaveReason]string{SaveExplicit: "explicit", SaveAuto: "auto", SaveFocusChange: "focusChange", SaveWindowChange: "windowChange", SaveReason(99): "unknown"} {
		if got := r.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int(r), got, want)
		}
	}
}
