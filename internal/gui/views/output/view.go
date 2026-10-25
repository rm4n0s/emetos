package output

import (
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// ViewOpts configure a View.
type ViewOpts struct {
	// Store holds the channels. Required.
	Store *logs.Store
	// Channel is the channel that is shown first; logs.MainChannel when empty.
	Channel string
	// Post hands a function to the UI thread (the binding's Post); lines that other goroutines log
	// are shown there, many in one go. Without it they are shown at once, which is for tests.
	Post func(func())
	// AfterFrame runs a function after the next frame, which the view asks for with it; the tail follows
	// the text once the editor has been laid out with the new lines. Without it the tail is not followed.
	AfterFrame func(func())
	// Visible tells whether the body of the panel is on screen. While it is not, the view does not ask
	// for frames to follow the tail (a log line would draw a frame of the whole window for a panel nobody
	// sees; Shown goes to the end when the panel opens). Without it the panel counts as visible.
	Visible func() bool
	// Options gives the options of the editor (the settings of the editor for plain text); the Output
	// panel turns off what an output text does not need. Without it the editor has the zero Options.
	Options func() editor.Options
	// SmartScroll tells whether output.smartScroll.enabled is on; without it the setting counts as on.
	SmartScroll func() bool
	// Keys is the scope of the context keys of the window: the view sets outputView.scrollLock and
	// activeOutputChannel there.
	Keys *contextkey.Scope
	// LogFile gives the path of the log file of the Emetos channel, for "Open Output in Editor"; nil or
	// "" when there is none.
	LogFile func() string
	// Logger gets the failures that have nobody to tell; slog.Default() when nil.
	Logger *slog.Logger
}

// pendingEvent is an event of the store that waits for the UI thread, with the watch it came from.
type pendingEvent struct {
	gen int
	ev  logs.Event
}

// View is the Output panel without its widgets: the channel that is shown, the read-only document that
// holds its text and the editor view over it, the scroll lock, and the following of the channel while
// lines are logged. The window makes one; NewBody and NewActions are the widgets that show it, and the
// commands of the panel call its methods. It is used on the UI thread; the store it watches may be
// written from any goroutine.
//
// Ported from VS Code: src/vs/workbench/contrib/output/browser/outputView.ts (OutputViewPane)
type View struct {
	opts   ViewOpts
	doc    *document.Document
	vm     *editor.ViewModel
	focus  *editor.EditorFocus
	active string
	// lineCount is how many lines of the channel the document holds.
	lineCount int
	lock      bool
	// gen numbers the watches of the store; the events of an older watch are dropped.
	gen   int
	stop  func()
	names []string
	// programmatic is set while the view moves the cursor itself, so that the smart scroll does not take
	// it for the user's click.
	programmatic bool
	// cursor is where the cursor was when the view last looked: the selection listeners also hear of a
	// change of the focus, which is not the user moving the cursor.
	cursor buffer.Position

	pendingMu sync.Mutex
	pending   []pendingEvent
	scheduled bool
	listeners []*func()
	cursorOff func()
}

// NewView makes the view of a store, showing a channel (the Emetos channel by default).
func NewView(opts ViewOpts) *View {
	if opts.Post == nil {
		opts.Post = func(f func()) { f() }
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	active := opts.Channel
	if active == "" {
		active = logs.MainChannel
	}
	v := &View{opts: opts, active: active, focus: &editor.EditorFocus{}}
	v.doc = document.New(document.VirtualURI("output"), "", document.Options{ReadOnly: true})
	var eo editor.Options
	if opts.Options != nil {
		eo = opts.Options()
	}
	v.vm = editor.NewViewModel(v.doc, outputOptions(eo), nil)
	v.vm.SetPoster(opts.Post)
	h := v.vm.AddSelectionListener(v.cursorMoved)
	v.cursorOff = func() { v.vm.RemoveSelectionListener(h) }
	v.names = slices.Clone(opts.Store.Names())
	v.watch()
	v.syncKeys()
	return v
}

// outputOptions are the options of the editor of an output text: wrapped lines, no line numbers,
// glyph margin, folding, minimap, sticky scroll, rulers or highlighting of the current line, and
// room for 20 px of line decorations, as VS Code makes its output editors.
//
// Ported from VS Code: src/vs/workbench/contrib/output/browser/outputView.ts (getConfigurationOverrides)
func outputOptions(o editor.Options) editor.Options {
	o.WordWrap = "on"
	o.LineNumbers = editor.LineNumbersOff
	o.GlyphMargin = false
	o.LineDecorationsWidth = 20
	o.Rulers = nil
	o.Folding = false
	o.ScrollBeyondLastLine = false
	o.RenderLineHighlight = "none"
	o.Minimap.Enabled = false
	o.StickyScroll.Enabled = false
	o.RoundedSelection = false
	o.OccurrencesHighlight = "off"
	o.SelectionHighlight = false
	o.MatchBrackets = "never"
	o.BracketPairColorization = false
	o.Guides = indent.GuideOptions{}
	o.RenderWhitespace = "none"
	return o
}

// Dispose stops watching the store.
func (v *View) Dispose() {
	if v.stop != nil {
		v.stop()
		v.stop = nil
	}
	v.cursorOff()
}

// Document is the read-only document that holds the text of the channel.
func (v *View) Document() *document.Document { return v.doc }

// ViewModel is the view model of the editor that shows the text.
func (v *View) ViewModel() *editor.ViewModel { return v.vm }

// Focus gives the keyboard focus to the editor while it is on screen.
func (v *View) Focus() *editor.EditorFocus { return v.focus }

// Channels lists the channels in the order they were created.
func (v *View) Channels() []string { return v.names }

// Active is the channel that is shown.
func (v *View) Active() string { return v.active }

// ScrollLock tells whether the view stays where it is when lines are added (outputView.scrollLock).
func (v *View) ScrollLock() bool { return v.lock }

// OnChange calls fn after the list of channels, the channel that is shown or the scroll lock changed,
// and returns the function that stops it. It is not called for lines.
func (v *View) OnChange(fn func()) (unsubscribe func()) {
	p := &fn
	v.listeners = append(v.listeners, p)
	return func() {
		if i := slices.Index(v.listeners, p); i >= 0 {
			v.listeners = slices.Delete(v.listeners, i, i+1)
		}
	}
}

func (v *View) notify() {
	for _, f := range slices.Clone(v.listeners) {
		(*f)()
	}
}

// LogFile is the path of the log file behind the active channel, or "" when it has none: only the
// Emetos channel has one.
func (v *View) LogFile() string {
	if v.active != logs.MainChannel || v.opts.LogFile == nil {
		return ""
	}
	return v.opts.LogFile()
}

// syncKeys sets the context keys of the view.
func (v *View) syncKeys() {
	if v.opts.Keys == nil {
		return
	}
	v.opts.Keys.Batch(func() {
		v.opts.Keys.Set("outputView.scrollLock", v.lock)
		v.opts.Keys.Set("activeOutputChannel", v.active)
	})
}

// watch shows the lines of the active channel and follows the lines that come after them.
func (v *View) watch() {
	if v.stop != nil {
		v.stop()
	}
	v.gen++
	gen := v.gen
	lines, stop := v.opts.Store.Watch(v.active, func(ev logs.Event) { v.enqueue(gen, ev) })
	v.stop = stop
	v.pendingMu.Lock()
	v.pending = nil
	v.pendingMu.Unlock()
	v.reset(lines)
}

// reset replaces the text with the lines of a channel and goes to the end of it.
func (v *View) reset(lines []string) {
	lines = splitLines(lines)
	v.programmatic = true
	v.doc.SetText(strings.Join(lines, "\n"))
	v.vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{})})
	v.vm.DocumentChanged()
	v.programmatic = false
	v.lineCount = len(lines)
	v.followIfNeeded(true)
}

// SetActive shows another channel. A name the store does not have yet shows an empty text, which the
// channel's first line fills.
func (v *View) SetActive(channel string) {
	if channel == v.active {
		return
	}
	v.active = channel
	v.lock = false
	v.watch()
	v.syncKeys()
	v.notify()
}

// SetScrollLock locks the view where it is, or lets it follow the lines again (and goes to the end).
func (v *View) SetScrollLock(lock bool) {
	if lock == v.lock {
		return
	}
	v.lock = lock
	v.syncKeys()
	if !lock {
		v.followIfNeeded(true)
	}
	v.notify()
}

// ToggleScrollLock flips the scroll lock (workbench.output.action.toggleAutoScroll).
func (v *View) ToggleScrollLock() { v.SetScrollLock(!v.lock) }

// Clear empties the active channel (workbench.output.action.clearOutput).
func (v *View) Clear() { v.opts.Store.Clear(v.active) }

// cursorMoved is the smart scroll: the user's click on a line other than the last locks the view, and a
// click on the last line unlocks it, when output.smartScroll.enabled is on.
//
// Ported from VS Code: src/vs/workbench/contrib/output/browser/outputView.ts (onDidChangeCursorPosition)
func (v *View) cursorMoved() {
	at := v.vm.Cursors().Primary().Position()
	if v.programmatic || at == v.cursor {
		v.cursor = at
		return
	}
	v.cursor = at
	if v.opts.SmartScroll != nil && !v.opts.SmartScroll() {
		return
	}
	last := v.doc.Snapshot().LineCount() - 1
	v.SetScrollLock(at.Line != last)
}

// enqueue is called by the store, on any goroutine: it queues the event and has the UI thread read the
// queue, once for any number of events.
func (v *View) enqueue(gen int, ev logs.Event) {
	v.pendingMu.Lock()
	v.pending = append(v.pending, pendingEvent{gen: gen, ev: ev})
	if v.scheduled {
		v.pendingMu.Unlock()
		return
	}
	v.scheduled = true
	v.pendingMu.Unlock()
	v.opts.Post(v.flush)
}

// flush applies the queued events on the UI thread.
func (v *View) flush() {
	v.pendingMu.Lock()
	events := v.pending
	v.pending = nil
	v.scheduled = false
	v.pendingMu.Unlock()

	listChanged := false
	var added []string
	for _, p := range events {
		if p.gen != v.gen {
			continue
		}
		switch ev := p.ev; ev.Kind {
		case logs.EventChannelAdded:
			listChanged = true
		case logs.EventLine:
			if ev.Channel == v.active {
				added = append(added, ev.Line)
			}
		case logs.EventCleared:
			if ev.Channel == v.active {
				added = nil
				v.apply(nil, true)
			}
		}
	}
	if len(added) > 0 {
		v.apply(added, false)
	}
	if listChanged {
		v.names = slices.Clone(v.opts.Store.Names())
		v.notify()
	}
}

// apply puts lines at the end of the text (or, with replace, makes them the text) and keeps the tail:
// the view follows the text when it was at the end before, unless it is locked.
func (v *View) apply(lines []string, replace bool) {
	if replace {
		v.reset(lines)
		return
	}
	lines = splitLines(lines)
	follow := v.shouldFollow()
	snap := v.doc.Snapshot()
	end := snap.PositionAt(snap.Len())
	text := strings.Join(lines, "\n")
	if v.lineCount > 0 {
		text = "\n" + text
	}
	v.doc.SetReadOnly(false)
	_, err := v.doc.Edit([]document.Edit{{Range: buffer.Range{Start: end, End: end}, Text: text}}, document.EditOptions{})
	v.doc.SetReadOnly(true)
	if err != nil {
		v.opts.Logger.Error("output: cannot add lines", "err", err)
		return
	}
	// the text is never edited by anyone: it is not unsaved work
	v.doc.MarkSaved(v.doc.Stat())
	v.lineCount += len(lines)
	if over := v.lineCount - v.opts.Store.MaxLines(); over > 0 {
		v.trim(over)
	}
	v.doc.ClearHistory()
	v.programmatic = true
	v.vm.DocumentChanged()
	v.programmatic = false
	v.followIfNeeded(follow)
}

// trim drops the first lines, which the store has dropped too.
func (v *View) trim(n int) {
	v.doc.SetReadOnly(false)
	_, err := v.doc.Edit([]document.Edit{{Range: buffer.Range{End: buffer.Position{Line: n}}}}, document.EditOptions{})
	v.doc.SetReadOnly(true)
	if err != nil {
		v.opts.Logger.Error("output: cannot drop old lines", "err", err)
		return
	}
	v.lineCount -= n
	v.programmatic = true
	v.vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{})})
	v.programmatic = false
}

// shouldFollow tells whether lines that are added now are to be followed: the view is not locked and
// shows the end of the text (a view that the user scrolled up stays there). With smart scrolling off the
// position does not matter.
func (v *View) shouldFollow() bool {
	if v.lock {
		return false
	}
	if v.opts.SmartScroll != nil && !v.opts.SmartScroll() {
		return true
	}
	return v.vm.AtBottom()
}

// followIfNeeded scrolls to the end of the text, once the editor has been laid out with the lines that
// were just added.
func (v *View) followIfNeeded(follow bool) {
	if !follow || v.lock || v.opts.AfterFrame == nil {
		return
	}
	v.revealEnd(3, 0)
}

// revealEnd scrolls to the end after the next frame, and again (up to attempts times) while the
// editor, whose layout lags a frame behind the text, is still short of it. It lets wait frames go by
// first.
func (v *View) revealEnd(attempts, wait int) {
	if v.opts.Visible != nil && !v.opts.Visible() {
		return
	}
	v.opts.AfterFrame(func() {
		if v.lock {
			return
		}
		if wait > 0 {
			v.revealEnd(attempts, wait-1)
			return
		}
		v.vm.RevealEnd()
		if attempts > 1 && !v.vm.AtBottom() {
			v.revealEnd(attempts-1, 0)
		}
	})
}

// Shown is told that the editor of the view is on screen again (the tab of the panel was opened): it
// goes to the end of the text unless the view is locked. A scroll made in the first frame of a new
// editor is lost (the position goes back to the offset the editor started with), so the scroll waits
// two frames.
func (v *View) Shown() {
	if v.lock || v.opts.AfterFrame == nil {
		return
	}
	v.revealEnd(3, 2)
}

// splitLines makes every entry one line: a message with line breaks becomes several lines.
func splitLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		l = strings.ReplaceAll(strings.ReplaceAll(l, "\r\n", "\n"), "\r", "\n")
		if !strings.Contains(l, "\n") {
			out = append(out, l)
			continue
		}
		out = append(out, strings.Split(l, "\n")...)
	}
	return out
}
