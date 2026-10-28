package terminal

import (
	"context"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// The suggest list of a terminal (SPEC §10.25 "Terminal IntelliSense"): when it opens, how it follows what is typed, its keys,
// and what accepting an item writes to the shell. suggest_view.go draws it.

// suggestRowsMax is the most rows the list shows at once (VS Code's suggest widget shows 12), and suggestPageRows how far
// PageUp and PageDown move.
const (
	suggestRowsMax  = 12
	suggestPageRows = 12
	// typingWindow is how long after a key the line is looked at for a change the key made.
	typingWindow = time.Second
	// maxInputBytes is the most text that counts as typing; more is a paste.
	maxInputBytes = 16
)

// sugSession is an open list.
type sugSession struct {
	// raw are the items the providers gave, items the ones the typed word matched, in order; lc and in are the line they were
	// made for.
	raw   []rawItem
	items []sugItem
	lc    lineContext
	in    promptInput
	// focus is the row that is selected (-1 for none), top the first row shown, rows how many are shown.
	focus, top, rows int
	// navigated says that the user moved in the list; explicit that it was opened by the user (Ctrl+Space).
	navigated, explicit bool
	// details says that the details pane is open.
	details bool
	// x and y are where the list was put in the pane's widget, in logical pixels (for the tests).
	x, y float64
}

// suggestState is the IntelliSense state of a pane. It lives on the UI thread.
type suggestState struct {
	sess *sugSession
	// typing says that printable text was typed and the list may open when the shell echoes it, edited that a key that changes
	// the line (or moves in it) was sent; typedAt is when. retrigger says that the list opens again after an item was accepted.
	typing, edited, retrigger bool
	typedAt                   time.Time
	typedRune                 rune
	// retriggerExplicit says that the list that is opened again was opened by the user, so it has everything and no quick
	// suggestion setting is asked.
	retriggerExplicit bool
	// last is the line as it was last read, and haveLast whether it was read.
	last     promptInput
	haveLast bool
	// frame is the scratch frame the line is read into.
	frame emulator.Frame
	// observing says that the line will be looked at after the next frame.
	observing bool
	// gen numbers the requests, so that the answer of an old one is dropped; cancel stops the one that runs.
	gen    uint64
	cancel context.CancelFunc
}

// suggestOn tells whether terminal IntelliSense works in this pane: it is on in the settings, the shell sends the sequences of
// shell integration, and the program is not on the alternate screen.
func (pv *paneView) suggestOn() bool {
	cfg := pv.env.cfg()
	return cfg.sugEnabled && cfg.shellInteg && pv.env.suggester != nil && pv.shellActive() && !pv.term.Emulator().Modes().AltScreen
}

// suggestVisible tells whether the list is shown.
func (pv *paneView) suggestVisible() bool {
	return pv.sug.sess != nil && len(pv.sug.sess.items) > 0
}

// suggestTyped is told what the user sends to the shell. Typing text lets the list open, a key that changes the line lets the
// open list follow it, and Enter or a control key closes it.
func (pv *paneView) suggestTyped(b []byte) {
	st := &pv.sug
	if len(b) == 0 || !pv.suggestOn() {
		return
	}
	switch {
	case printableInput(b):
		r, _ := utf8.DecodeLastRuneInString(string(b))
		st.typing, st.edited, st.typedAt, st.typedRune = true, true, time.Now(), r
	case strings.ContainsAny(string(b), "\r\n"):
		st.haveLast = false
		pv.suggestHide()
	default:
		// Backspace, the arrows, Ctrl+U and the like: the line changes or the cursor moves, and the list follows
		st.edited, st.typedAt = true, time.Now()
	}
}

// printableInput tells whether bytes are a few printable characters, as typing makes them.
func printableInput(b []byte) bool {
	if len(b) > maxInputBytes || !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r < 0x20 || r == 0x7f || !unicode.IsPrint(r) && r != ' ' {
			return false
		}
	}
	return true
}

// suggestFrame is told that the screen changed. The line is looked at after the frame that draws the change: the terminal tells
// the pane once a frame, so what arrives between the notice and the frame's snapshot would not be told again.
func (pv *paneView) suggestFrame() {
	st := &pv.sug
	if st.sess == nil && !st.typing && !st.edited && !st.retrigger || st.observing {
		return
	}
	st.observing = true
	pv.afterFrame(func() {
		st.observing = false
		pv.suggestObserve()
	})
}

// suggestObserve reads the line after the screen changed. When the user just typed, or the list is open, the line is read again and the
// list opens, follows it or closes.
func (pv *paneView) suggestObserve() {
	st := &pv.sug
	if st.sess == nil && !st.typing && !st.edited && !st.retrigger {
		return
	}
	if st.sess == nil && time.Since(st.typedAt) > typingWindow {
		st.typing, st.edited, st.retrigger = false, false, false
		return
	}
	if !pv.suggestOn() || !pv.focused {
		pv.suggestHide()
		return
	}
	in := readPromptInput(pv.term, &st.frame)
	if !in.ok {
		pv.suggestHide()
		st.typing, st.edited, st.retrigger = false, false, false
		return
	}
	if st.haveLast && sameInput(in, st.last) {
		return // the shell has not echoed the key yet
	}
	st.last, st.haveLast = in, true
	pv.suggestFollow(in)
}

func sameInput(a, b promptInput) bool {
	return a.value == b.value && a.cursor == b.cursor && a.startLine == b.startLine && a.startX == b.startX && a.cursorLine == b.cursorLine
}

// folderOf is the part of the word up to its last slash: the folder whose files the path provider lists.
func folderOf(lc lineContext) string {
	if i := strings.LastIndexByte(lc.cur.text, '/'); i >= 0 {
		return lc.cur.text[:i+1]
	}
	return ""
}

// suggestFollow takes a new reading of the line: the open list follows it, a closed one may open.
func (pv *paneView) suggestFollow(in promptInput) {
	st := &pv.sug
	lc := newLineContext(in.value, in.cursor)
	typing, retrigger := st.typing, st.retrigger
	st.typing, st.edited, st.retrigger = false, false, false
	if s := st.sess; s != nil {
		if !s.explicit && lc.cur.text == "" {
			pv.suggestHide() // the word was deleted: a list that opened by itself has nothing to follow
			return
		}
		sameWord := s.in.startLine == in.startLine && s.lc.cmdStart == lc.cmdStart && len(s.lc.words) == len(lc.words) &&
			s.lc.cur.start == lc.cur.start && in.cursor >= lc.cur.start
		switch {
		case sameWord && folderOf(lc) == folderOf(s.lc):
			pv.suggestRun(in, lc, s.raw, s.explicit)
		case sameWord:
			pv.suggestRun(in, lc, nil, s.explicit)
		default:
			pv.suggestHide()
			if typing && pv.suggestAutoOK(lc) {
				pv.suggestRun(in, lc, nil, false)
			}
		}
		return
	}
	switch {
	case retrigger:
		pv.suggestRun(in, lc, nil, st.retriggerExplicit)
	case typing && pv.suggestAutoOK(lc):
		pv.suggestRun(in, lc, nil, false)
	}
}

// suggestAutoOK tells whether the list opens by itself for the word at the cursor (terminal.integrated.suggest.quickSuggestions
// and suggestOnTriggerCharacters).
func (pv *paneView) suggestAutoOK(lc lineContext) bool {
	cfg := pv.env.cfg()
	if pv.sug.typedRune == '/' && cfg.sugTriggerChars {
		return true
	}
	if lc.cur.text == "" {
		return false
	}
	if lc.command {
		return cfg.sugQuick.commands
	}
	return cfg.sugQuick.arguments
}

// suggestTrigger opens the list at the cursor (Ctrl+Space).
func (pv *paneView) suggestTrigger() {
	st := &pv.sug
	if !pv.suggestOn() {
		return
	}
	in := readPromptInput(pv.term, &st.frame)
	if !in.ok {
		return
	}
	st.last, st.haveLast = in, true
	pv.suggestRun(in, newLineContext(in.value, in.cursor), nil, true)
}

// suggestRun computes the list for a line on a goroutine and shows it when it is done. With items from an earlier run of the same
// word they are only filtered again.
func (pv *paneView) suggestRun(in promptInput, lc lineContext, reuse []rawItem, explicit bool) {
	st := &pv.sug
	if st.cancel != nil {
		st.cancel()
	}
	st.gen++
	gen := st.gen
	env := pv.env.suggestEnv(pv)
	quick := pv.env.cfg().sugQuick
	parent := pv.env.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	st.cancel = cancel
	sg := pv.env.suggester
	post := pv.env.post
	go func() {
		defer cancel()
		raw := reuse
		if raw == nil {
			if env.loadHistory != nil && env.enabled(providerHistory) {
				env.history = env.loadHistory()
			}
			raw = sg.complete(ctx, env, lc, explicit, quick).items
		}
		var m ui.SuggestMatcher
		items := filterItems(&m, raw, lc)
		if ctx.Err() != nil {
			return
		}
		post(func() {
			if gen != pv.sug.gen {
				return
			}
			pv.suggestShow(raw, items, lc, in, explicit)
		})
	}()
}

// suggestShow shows the items that a run found.
func (pv *paneView) suggestShow(raw []rawItem, items []sugItem, lc lineContext, in promptInput, explicit bool) {
	st := &pv.sug
	if !pv.focused || !pv.suggestOn() || len(items) == 0 {
		pv.suggestHide()
		return
	}
	cfg := pv.env.cfg()
	s := &sugSession{raw: raw, items: items, lc: lc, in: in, explicit: explicit, rows: min(len(items), suggestRowsMax)}
	if old := st.sess; old != nil {
		s.details = old.details
	}
	if cfg.sugSelection == "never" && !explicit {
		s.focus = -1
	}
	st.sess = s
	pv.rebuild()
	pv.suggestChanged()
}

// suggestChanged tells the extension that the list was shown or closed (the context key follows).
func (pv *paneView) suggestChanged() {
	if pv.env.suggestChanged != nil {
		pv.env.suggestChanged(pv)
	}
}

// suggestHide closes the list and stops the request that runs.
func (pv *paneView) suggestHide() {
	st := &pv.sug
	if st.cancel != nil {
		st.cancel()
		st.cancel = nil
	}
	st.gen++
	if st.sess == nil {
		return
	}
	st.sess = nil
	pv.rebuild()
	pv.suggestChanged()
}

// suggestMove moves the selection by n rows (a step wraps around the list, a page stops at its ends).
func (pv *paneView) suggestMove(n int) {
	s := pv.sug.sess
	if s == nil || len(s.items) == 0 {
		return
	}
	s.navigated = true
	next := s.focus + n
	switch {
	case s.focus < 0 && n < 0:
		next = len(s.items) - 1
	case n == 1 || n == -1:
		next = (next + len(s.items)) % len(s.items)
	default:
		next = min(max(next, 0), len(s.items)-1)
	}
	s.focus = next
	if s.focus < s.top {
		s.top = s.focus
	}
	if s.focus >= s.top+s.rows {
		s.top = s.focus - s.rows + 1
	}
	pv.rebuild()
}

// suggestScroll scrolls the rows with the wheel.
func (pv *paneView) suggestScroll(n int) {
	s := pv.sug.sess
	if s == nil {
		return
	}
	s.top = min(max(s.top+n, 0), max(len(s.items)-s.rows, 0))
	pv.rebuild()
}

// suggestUp is the Up key with the list open: on the first row, before the user moved, it is the shell's own Up (the history)
// unless terminal.integrated.suggest.upArrowNavigatesHistory is off.
func (pv *paneView) suggestUp() {
	s := pv.sug.sess
	if s == nil {
		return
	}
	if s.focus <= 0 && !s.navigated && pv.env.cfg().sugUpHistory {
		pv.suggestHide()
		if b := pv.term.Emulator().EncodeKey(emulator.KeyEvent{Key: emulator.KeyUp}, nil); len(b) > 0 {
			pv.userInput(b)
		}
		return
	}
	pv.suggestMove(-1)
}

// suggestToggleDetails opens or closes the details pane.
func (pv *paneView) suggestToggleDetails() {
	if s := pv.sug.sess; s != nil {
		s.details = !s.details
		pv.rebuild()
	}
}

// suggestAcceptFocused is Tab or Enter with the list open. Tab accepts the selected row (the first one when nothing was
// selected, unless terminal.integrated.suggest.selectionMode is never and the user did not move); Enter accepts only a row the
// user chose, or any when the mode is always, and else is the shell's Enter.
func (pv *paneView) suggestAcceptFocused(enter bool) {
	s := pv.sug.sess
	if s == nil {
		return
	}
	mode := pv.env.cfg().sugSelection
	canEnter := s.explicit || mode == "always" || s.navigated
	canTab := mode != "never" || s.explicit || s.navigated
	if enter && !canEnter || !enter && !canTab {
		pv.sug.haveLast = false
		pv.suggestHide()
		key := []byte("\t")
		if enter {
			key = []byte("\r")
		}
		pv.userInput(key)
		return
	}
	pv.suggestAccept(max(s.focus, 0), enter)
}

// suggestAccept writes the rest of the item to the shell (see acceptText) and closes the list. A folder, or any item when a
// trailing space is added, opens the list again for what comes next; Enter may also run the line (runOnEnter).
func (pv *paneView) suggestAccept(i int, enter bool) {
	s := pv.sug.sess
	if s == nil || i < 0 || i >= len(s.items) {
		return
	}
	cfg := pv.env.cfg()
	it := s.items[i]
	text := acceptText(it, s.lc, cfg.sugTrailingSpace)
	run := false
	if enter {
		typed := s.lc.line[min(s.lc.cur.start, s.lc.cursor):s.lc.cursor]
		switch cfg.sugRunOnEnter {
		case "always":
			run = true
		case "exactMatch":
			run = typed == it.label
		case "exactMatchIgnoreExtension":
			run = typed == it.label || typed == strings.TrimSuffix(it.label, path.Ext(it.label))
		}
	}
	explicit := s.explicit
	pv.sug.haveLast = false
	pv.suggestHide()
	if text != "" {
		pv.term.Type([]byte(text), ByUser)
		pv.scrollTo(0)
	}
	switch {
	case run:
		pv.userInput([]byte("\r"))
	case strings.HasSuffix(it.label, "/") || cfg.sugTrailingSpace:
		st := &pv.sug
		st.retrigger, st.retriggerExplicit, st.edited, st.typedAt = true, explicit, true, time.Now()
	}
}
