package debug

import (
	"context"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The completions of the input of the Debug Console: the adapter's (the request completions, when it supports it) and the lines typed
// before (debug.console.historySuggestions). The list opens while the user types, Up and Down move in it, Tab (and Enter, with
// debug.console.acceptSuggestionOnEnter) take the focused item, Escape closes it.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/repl.ts (the completion provider registered in onDidFocusSession)

// MaxSuggestRows is how many rows the list shows at once.
const MaxSuggestRows = 8

type suggestItem struct {
	label, detail string
	// insert replaces the text between from and to (byte offsets in the input as it was when the list was asked for).
	insert   string
	from, to int
	// selStart and selLen select a part of the inserted text afterwards (bytes), when selLen > 0.
	selStart, selLen int
	kind             int
	history          bool
}

type suggestions struct {
	c      *Console
	items  []suggestItem
	focus  int
	top    int
	text   string // the input the items were made for
	gen    int
	cancel context.CancelFunc
	timer  *time.Timer
	// silent is set while the console changes the input itself.
	silent bool
}

// Open tells whether the list shows.
func (s *suggestions) open() bool { return len(s.items) > 0 }

func (s *suggestions) close() {
	s.gen++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if len(s.items) > 0 {
		s.items, s.focus, s.top = nil, 0, 0
		s.c.notify()
	}
}

func (s *suggestions) setInput(text string) {
	s.silent = true
	s.c.input.SetValue(text)
	s.silent = false
}

// changed is told of every change of the input: it asks for completions after the delay.
func (s *suggestions) changed(text string) {
	if s.silent {
		return
	}
	s.close()
	if strings.TrimSpace(text) == "" || s.c.ReadOnly() {
		return
	}
	gen := s.gen
	s.timer = time.AfterFunc(s.c.opts.CompletionDelay, func() {
		s.c.opts.Post(func() {
			if gen == s.gen {
				s.request()
			}
		})
	})
}

// Trigger asks for completions now (Ctrl+Space).
func (s *suggestions) trigger() {
	s.close()
	if !s.c.ReadOnly() {
		s.request()
	}
}

// cursorOf is the offset of the cursor in the text of the input.
func (s *suggestions) cursorOf(text string) int {
	if _, end, ok := s.c.input.Selection(); ok {
		return min(max(end, 0), len(text))
	}
	return len(text)
}

// request asks the adapter and adds the history; the answer is handed over to the UI thread.
func (s *suggestions) request() {
	c := s.c
	sess := c.shown
	if sess == nil || sess.State() == debug.Inactive {
		return
	}
	text := c.input.Value()
	cursor := s.cursorOf(text)
	s.gen++
	gen := s.gen
	s.text = text
	history := s.historyItems(text, cursor)
	if !sess.Capabilities().SupportsCompletionsRequest {
		s.set(gen, nil, history)
		return
	}
	before := text[:cursor]
	line := 1 + strings.Count(before, "\n")
	lineText := before[strings.LastIndex(before, "\n")+1:]
	column := len(utf16.Encode([]rune(lineText))) + 1
	frameID := 0
	if f := c.opts.Service.Model().Focus(); f.Frame != nil && f.Frame.Thread.Session().ConsoleRoot() == sess {
		frameID = f.Frame.ID
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	s.cancel = cancel
	go func() {
		defer cancel()
		targets, err := sess.CompletionsAt(ctx, frameID, text, line, column)
		if err != nil {
			targets = nil // an adapter that cannot answer leaves the history
		}
		c.opts.Post(func() { s.set(gen, targets, history) })
	}()
}

// set shows the items of an answer, unless the input changed since the question.
func (s *suggestions) set(gen int, targets []dap.CompletionItem, history []suggestItem) {
	if gen != s.gen || s.c.input.Value() != s.text {
		return
	}
	cursor := s.cursorOf(s.text)
	var items []suggestItem
	for _, t := range targets {
		if t.Label == "" {
			continue
		}
		insert := t.Text
		if insert == "" {
			insert = t.Label
		}
		from := cursor - unitsToBytes(s.text[:cursor], t.Length)
		it := suggestItem{label: t.Label, detail: t.Detail, insert: insert, from: from, to: cursor, kind: completionKind(t.Type)}
		if t.SelectionStart > 0 || t.SelectionLength > 0 {
			it.selStart = utf16PrefixBytes(insert, t.SelectionStart)
			it.selLen = utf16PrefixBytes(insert, t.SelectionStart+t.SelectionLength) - it.selStart
		}
		items = append(items, it)
	}
	items = append(items, history...)
	s.items, s.focus, s.top = items, 0, 0
	s.c.notify()
}

// historyItems are the lines typed before that continue the input.
func (s *suggestions) historyItems(text string, cursor int) []suggestItem {
	if !s.c.config().HistorySuggestions || text == "" {
		return nil
	}
	var out []suggestItem
	lower := strings.ToLower(text)
	h := s.c.history.Items()
	for i := len(h) - 1; i >= 0; i-- {
		if h[i] != text && strings.HasPrefix(strings.ToLower(h[i]), lower) {
			out = append(out, suggestItem{label: h[i], insert: h[i], from: 0, to: len(text), kind: 1, history: true})
		}
	}
	_ = cursor
	return out
}

// unitsToBytes is how many bytes the last n UTF-16 units of a text take.
func unitsToBytes(text string, n int) int {
	bytes := 0
	for n > 0 && bytes < len(text) {
		r, size := utf8.DecodeLastRuneInString(text[:len(text)-bytes])
		bytes += size
		n -= utf16.RuneLen(r)
	}
	return bytes
}

// utf16PrefixBytes is the number of bytes of the first n UTF-16 units of a text.
func utf16PrefixBytes(text string, n int) int {
	bytes := 0
	for n > 0 && bytes < len(text) {
		r, size := utf8.DecodeRuneInString(text[bytes:])
		bytes += size
		n -= utf16.RuneLen(r)
	}
	return bytes
}

// move moves the focus, round the ends, and keeps it in the window.
func (s *suggestions) move(delta int) {
	n := len(s.items)
	if n == 0 {
		return
	}
	s.focus = ((s.focus+delta)%n + n) % n
	if s.focus < s.top {
		s.top = s.focus
	}
	if s.focus >= s.top+MaxSuggestRows {
		s.top = s.focus - MaxSuggestRows + 1
	}
	s.c.notify()
}

// accept puts the focused item in the input.
func (s *suggestions) accept() bool {
	if !s.open() || s.c.input.Value() != s.text {
		return false
	}
	it := s.items[s.focus]
	text := s.text
	from, to := min(it.from, len(text)), min(it.to, len(text))
	next := text[:from] + it.insert + text[to:]
	s.close()
	s.setInput(next)
	start := from + len(it.insert)
	end := start
	if it.selLen > 0 {
		start = from + it.selStart
		end = start + it.selLen
	}
	s.c.input.Select(start, end)
	return true
}

// rows are the rows of the list that show.
func (s *suggestions) rows() (rows []kit.SuggestRow, focus, top int) {
	top = min(s.top, max(0, len(s.items)-1))
	end := min(len(s.items), top+MaxSuggestRows)
	for _, it := range s.items[top:end] {
		rows = append(rows, kit.SuggestRow{Label: it.label, LabelDescription: it.detail, Kind: it.kind})
	}
	return rows, s.focus - top, top
}

// key handles the keys of the list for the input; it is the KeyHook of the input box.
func (s *suggestions) key(e services.KeyEvent, kb *services.HardwareKeyboard) widgets.KeyEventResult {
	if _, ok := e.(*services.KeyDownEvent); !ok {
		if _, ok := e.(*services.KeyRepeatEvent); !ok {
			return widgets.KeyEventResultIgnored
		}
	}
	key := e.LogicalKey()
	if kb != nil && kb.IsControlPressed() && key == services.LogicalKeyboardKeySpace {
		s.trigger()
		return widgets.KeyEventResultHandled
	}
	if !s.open() {
		return widgets.KeyEventResultIgnored
	}
	if kb != nil && (kb.IsShiftPressed() || kb.IsControlPressed() || kb.IsAltPressed()) {
		return widgets.KeyEventResultIgnored
	}
	switch key {
	case services.LogicalKeyboardKeyArrowDown:
		s.move(1)
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyArrowUp:
		s.move(-1)
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyPageDown:
		s.move(MaxSuggestRows)
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyPageUp:
		s.move(-MaxSuggestRows)
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyTab:
		s.accept()
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter:
		if s.c.config().AcceptSuggestionOnEnter {
			s.accept()
			return widgets.KeyEventResultHandled
		}
		s.close() // Enter evaluates what is typed
	case services.LogicalKeyboardKeyEscape:
		s.close()
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

// completionKind maps the type of a completion item of the adapter to the kinds of the suggest list (LSP's CompletionItemKind);
// VS Code's CompletionItemKinds.fromString with the default property.
func completionKind(typ string) int {
	switch strings.ToLower(typ) {
	case "method":
		return 2
	case "function":
		return 3
	case "constructor":
		return 4
	case "field":
		return 5
	case "variable":
		return 6
	case "class":
		return 7
	case "interface":
		return 8
	case "module":
		return 9
	case "unit":
		return 11
	case "value":
		return 12
	case "enum":
		return 13
	case "keyword":
		return 14
	case "snippet":
		return 15
	case "color", "customcolor":
		return 16
	case "file":
		return 17
	case "reference":
		return 18
	case "text":
		return 1
	}
	return 10
}
