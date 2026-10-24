package output

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// rig is a view over a store with a queue for the UI thread and for the frames, which the test runs
// itself, and an editor that has been "laid out" with a viewport of 100 px and rows of 20 px.
type rig struct {
	t      *testing.T
	store  *logs.Store
	view   *View
	posted []func()
	frames []func()
	// revealed are the scroll positions the editor was asked for.
	revealed []float64
	smart    bool
}

func newRig(t *testing.T, maxLines int, channel string) *rig {
	t.Helper()
	r := &rig{t: t, store: logs.NewStore(maxLines), smart: true}
	r.view = NewView(ViewOpts{
		Store: r.store, Channel: channel,
		Post:        func(f func()) { r.posted = append(r.posted, f) },
		AfterFrame:  func(f func()) { r.frames = append(r.frames, f) },
		Options:     func() editor.Options { return editor.Options{FontSize: 14, LineHeight: 20, TabSize: 4} },
		SmartScroll: func() bool { return r.smart },
	})
	r.view.ViewModel().SetScroller(func(top float64) { r.revealed = append(r.revealed, top) })
	t.Cleanup(r.view.Dispose)
	return r
}

// layout tells the view model what the render object would: a viewport of 100 px, and as much room
// to scroll as there are lines of 20 px beyond it.
func (r *rig) layout() {
	vm := r.view.ViewModel()
	lines := vm.Document().Snapshot().LineCount()
	vm.SetTextMetrics(300, 100, max(float64(lines)*20-100, 0), 100, 8)
}

// ui runs what was posted to the UI thread, and the frames after it, until nothing is left.
func (r *rig) ui() {
	for len(r.posted) > 0 || len(r.frames) > 0 {
		posted, frames := r.posted, r.frames
		r.posted, r.frames = nil, nil
		for _, f := range posted {
			f()
		}
		r.layout()
		for _, f := range frames {
			f()
		}
	}
}

func (r *rig) text() string { return r.view.Document().Text() }

func TestViewShowsAChannelAndSwitches(t *testing.T) {
	tests := []struct {
		name     string
		channel  string
		prepare  func(s *logs.Store)
		switchTo string
		want     string
		channels []string
	}{
		{"the Emetos channel is shown first", "", func(s *logs.Store) { s.Append("Emetos", "a"); s.Append("Emetos", "b") }, "", "a\nb", []string{"Emetos"}},
		{"another channel from the start", "other", func(s *logs.Store) { s.Append("Emetos", "a"); s.Append("other", "x") }, "", "x", []string{"Emetos", "other"}},
		{"a channel that does not exist yet is empty", "", func(*logs.Store) {}, "", "", nil},
		{"switching shows the other text", "", func(s *logs.Store) { s.Append("Emetos", "a"); s.Append("other", "x"); s.Append("other", "y") }, "other", "x\ny", []string{"Emetos", "other"}},
		{"switching to an empty name shows nothing", "", func(s *logs.Store) { s.Append("Emetos", "a") }, "never", "", []string{"Emetos"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := logs.NewStore(0)
			tt.prepare(store)
			v := NewView(ViewOpts{Store: store, Channel: tt.channel})
			defer v.Dispose()
			if tt.switchTo != "" {
				v.SetActive(tt.switchTo)
				if v.Active() != tt.switchTo {
					t.Errorf("Active = %q, want %q", v.Active(), tt.switchTo)
				}
			}
			if got := v.Document().Text(); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
			if got := v.Channels(); !reflect.DeepEqual(got, tt.channels) && (len(got) != 0 || len(tt.channels) != 0) {
				t.Errorf("channels = %v, want %v", got, tt.channels)
			}
		})
	}
}

func TestViewTailsTheActiveChannelOnly(t *testing.T) {
	r := newRig(t, 0, "")
	r.store.Append("Emetos", "one")
	r.ui()
	r.store.Append("Emetos", "two")
	r.store.Append("other", "elsewhere")
	r.store.Append("Emetos", "three")
	if got := r.text(); got != "one" {
		t.Fatalf("lines are shown before the UI thread runs: %q", got)
	}
	if len(r.posted) != 1 {
		t.Errorf("%d posts for a burst of lines, want 1", len(r.posted))
	}
	r.ui()
	if got, want := r.text(), "one\ntwo\nthree"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if got, want := r.view.Channels(), []string{"Emetos", "other"}; !reflect.DeepEqual(got, want) {
		t.Errorf("channels = %v, want %v", got, want)
	}
	// switch, and the lines of the old channel that were not shown yet do not come in
	r.store.Append("Emetos", "four")
	r.view.SetActive("other")
	r.ui()
	if got := r.text(); got != "elsewhere" {
		t.Errorf("text = %q, want only the other channel", got)
	}
	r.store.Append("other", "more")
	r.ui()
	if got, want := r.text(), "elsewhere\nmore"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	// and back: all the lines the channel has
	r.view.SetActive("Emetos")
	if got, want := r.text(), "one\ntwo\nthree\nfour"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestViewMessagesWithLineBreaksAreLines(t *testing.T) {
	r := newRig(t, 0, "")
	r.store.Append("Emetos", "a\nb")
	r.store.Append("Emetos", "c\r\nd")
	r.ui()
	if got, want := r.text(), "a\nb\nc\nd"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestViewClear(t *testing.T) {
	r := newRig(t, 0, "")
	r.store.Append("Emetos", "a")
	r.store.Append("other", "x")
	r.ui()
	r.view.Clear()
	r.ui()
	if got := r.text(); got != "" {
		t.Errorf("text after Clear = %q", got)
	}
	if got := r.store.Lines("Emetos"); len(got) != 0 {
		t.Errorf("the channel still has %v", got)
	}
	if got := r.store.Lines("other"); len(got) != 1 {
		t.Errorf("Clear touched another channel: %v", got)
	}
	// the first line after a clear starts the text, with no line break before it
	r.store.Append("Emetos", "fresh")
	r.ui()
	if got := r.text(); got != "fresh" {
		t.Errorf("text = %q, want %q", got, "fresh")
	}
}

func TestViewKeepsTheLastLinesOfTheStore(t *testing.T) {
	r := newRig(t, 5, "")
	for i := 1; i <= 12; i++ {
		r.store.Append("Emetos", fmt.Sprint(i))
		r.ui()
	}
	lines := strings.Split(r.text(), "\n")
	if len(lines) != 5 || lines[0] != "8" || lines[4] != "12" {
		t.Errorf("lines = %v, want 8..12", lines)
	}
	// a burst of more lines than fit in one go
	for i := 13; i <= 40; i++ {
		r.store.Append("Emetos", fmt.Sprint(i))
	}
	r.ui()
	lines = strings.Split(r.text(), "\n")
	if len(lines) != 5 || lines[0] != "36" || lines[4] != "40" {
		t.Errorf("lines after the burst = %v, want 36..40", lines)
	}
}

func TestViewIsReadOnly(t *testing.T) {
	r := newRig(t, 0, "")
	r.store.Append("Emetos", "text")
	r.ui()
	doc := r.view.Document()
	if !doc.ReadOnly() {
		t.Fatal("the document is not read-only")
	}
	_, err := doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "typed"}}, document.EditOptions{})
	if err == nil {
		t.Error("an edit of the document went through")
	}
	if got := r.text(); got != "text" {
		t.Errorf("text = %q", got)
	}
	// adding lines does not leave the document writable
	r.store.Append("Emetos", "more")
	r.ui()
	if !doc.ReadOnly() {
		t.Error("the document is writable after lines were added")
	}
	if doc.Dirty() {
		t.Error("the document is dirty")
	}
}

func TestViewOptionsOfAnOutputText(t *testing.T) {
	v := NewView(ViewOpts{
		Store: logs.NewStore(0),
		Options: func() editor.Options {
			return editor.Options{FontSize: 14, LineHeight: 20, LineNumbers: editor.LineNumbersOn, Folding: true, GlyphMargin: true}
		},
	})
	defer v.Dispose()
	o := v.ViewModel().Options()
	if o.WordWrap != "on" || o.LineNumbers != editor.LineNumbersOff || o.Folding || o.GlyphMargin || o.Minimap.Enabled ||
		o.StickyScroll.Enabled || o.RenderLineHighlight != "none" || o.LineDecorationsWidth != 20 || o.ScrollBeyondLastLine {
		t.Errorf("options of an output text: %+v", o)
	}
}

func TestViewFollowsTheTail(t *testing.T) {
	tests := []struct {
		name string
		// arrange runs after the first lines were shown and laid out.
		arrange func(r *rig)
		// follows says whether the lines that come next scroll to the end.
		follows bool
	}{
		{"at the end it follows", func(r *rig) {}, true},
		{"scrolled up it does not", func(r *rig) { r.view.ViewModel().SetScrollTop(0) }, false},
		{"scrolled up with smart scroll off it follows anyway", func(r *rig) { r.smart = false; r.view.ViewModel().SetScrollTop(0) }, true},
		{"locked it does not", func(r *rig) { r.view.SetScrollLock(true) }, false},
		{"locked with smart scroll off it does not either", func(r *rig) { r.smart = false; r.view.SetScrollLock(true) }, false},
		{"unlocked at the end it follows", func(r *rig) { r.view.SetScrollLock(true); r.view.SetScrollLock(false) }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, 0, "")
			for i := range 20 {
				r.store.Append("Emetos", fmt.Sprint(i))
			}
			r.ui()
			// the end: 20 lines of 20 px in a viewport of 100
			r.view.ViewModel().SetScrollTop(300)
			tt.arrange(r)
			r.ui()
			r.revealed = nil
			r.store.Append("Emetos", "new line")
			r.ui()
			if got := len(r.revealed) > 0; got != tt.follows {
				t.Errorf("scrolled to the end = %v (%v), want %v", got, r.revealed, tt.follows)
			}
			if r.view.Document().Text()[len(r.view.Document().Text())-8:] != "new line" {
				t.Errorf("the line is not in the text")
			}
		})
	}
}

func TestViewScrollLock(t *testing.T) {
	r := newRig(t, 0, "")
	changes := 0
	r.view.OnChange(func() { changes++ })
	r.view.ToggleScrollLock()
	if !r.view.ScrollLock() || changes != 1 {
		t.Errorf("lock %v, changes %d after the toggle", r.view.ScrollLock(), changes)
	}
	r.view.SetScrollLock(true)
	if changes != 1 {
		t.Errorf("setting the lock it has told listeners")
	}
	r.view.ToggleScrollLock()
	if r.view.ScrollLock() || changes != 2 {
		t.Errorf("lock %v, changes %d after the second toggle", r.view.ScrollLock(), changes)
	}
	// another channel is shown from its end
	r.view.SetScrollLock(true)
	r.view.SetActive("other")
	if r.view.ScrollLock() {
		t.Error("the lock stays when another channel is shown")
	}
}

func TestViewSmartScrollFollowsTheClick(t *testing.T) {
	tests := []struct {
		name     string
		smart    bool
		line     int
		wantLock bool
	}{
		{"a click above the last line locks", true, 3, true},
		{"a click on the last line unlocks", true, 9, false},
		{"with smart scroll off a click does nothing", false, 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, 0, "")
			r.smart = tt.smart
			for i := range 10 {
				r.store.Append("Emetos", fmt.Sprint(i))
			}
			r.ui()
			if tt.line == 9 {
				r.view.SetScrollLock(true)
			}
			r.view.ViewModel().Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: tt.line})})
			r.view.ViewModel().SelectionsChanged()
			if r.view.ScrollLock() != (tt.wantLock || (tt.line == 9 && !tt.smart)) {
				t.Errorf("lock = %v, want %v", r.view.ScrollLock(), tt.wantLock)
			}
		})
	}
}

func TestViewSetsTheContextKeys(t *testing.T) {
	keys := contextkey.NewRoot()
	store := logs.NewStore(0)
	store.Append("other", "x")
	v := NewView(ViewOpts{Store: store, Keys: keys})
	defer v.Dispose()
	if keys.Get("outputView.scrollLock") != false || keys.Get("activeOutputChannel") != "Emetos" {
		t.Errorf("keys at the start: lock %v, channel %v", keys.Get("outputView.scrollLock"), keys.Get("activeOutputChannel"))
	}
	v.SetScrollLock(true)
	if keys.Get("outputView.scrollLock") != true {
		t.Error("outputView.scrollLock is not set by the lock")
	}
	v.SetActive("other")
	if keys.Get("outputView.scrollLock") != false || keys.Get("activeOutputChannel") != "other" {
		t.Errorf("keys after switching: lock %v, channel %v", keys.Get("outputView.scrollLock"), keys.Get("activeOutputChannel"))
	}
}

func TestViewLogFile(t *testing.T) {
	store := logs.NewStore(0)
	store.Append("other", "x")
	v := NewView(ViewOpts{Store: store, LogFile: func() string { return "/logs/main.log" }})
	defer v.Dispose()
	if got := v.LogFile(); got != "/logs/main.log" {
		t.Errorf("LogFile of the Emetos channel = %q", got)
	}
	v.SetActive("other")
	if got := v.LogFile(); got != "" {
		t.Errorf("LogFile of another channel = %q, want none", got)
	}
	bare := NewView(ViewOpts{Store: store})
	defer bare.Dispose()
	if got := bare.LogFile(); got != "" {
		t.Errorf("LogFile with no log = %q", got)
	}
}

func TestViewIsSafeForConcurrentAppends(t *testing.T) {
	store := logs.NewStore(100000)
	var mu sync.Mutex
	var queue []func()
	post := func(f func()) {
		mu.Lock()
		queue = append(queue, f)
		mu.Unlock()
	}
	v := NewView(ViewOpts{Store: store, Post: post})
	defer v.Dispose()
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 100 {
				store.Append("Emetos", fmt.Sprintf("%d-%d", g, i))
			}
		}()
	}
	wg.Wait()
	for {
		mu.Lock()
		q := queue
		queue = nil
		mu.Unlock()
		if len(q) == 0 {
			break
		}
		for _, f := range q {
			f()
		}
	}
	if n := len(strings.Split(v.Document().Text(), "\n")); n != 400 {
		t.Errorf("the view shows %d lines, want 400", n)
	}
}
