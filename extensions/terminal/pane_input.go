package terminal

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// modsOf are the modifier keys the keyboard holds, as the emulator names them.
func (pv *paneView) modsOf() emulator.Mod {
	if pv.binding == nil {
		return 0
	}
	kb := pv.binding.Keyboard()
	var m emulator.Mod
	if kb.IsShiftPressed() {
		m |= emulator.ModShift
	}
	if kb.IsAltPressed() {
		m |= emulator.ModAlt
	}
	if kb.IsControlPressed() {
		m |= emulator.ModCtrl
	}
	if kb.IsMetaPressed() {
		m |= emulator.ModMeta
	}
	return m
}

// onKey is a key the window did not use: a key with no binding, or the key of a command that does not skip the shell. It is
// sent to the program through the emulator's key encoder; a character with no Ctrl or Alt is left to the input method.
func (pv *paneView) onKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	ev, viaText, ok := keyEventOf(e, pv.modsOf(), pv.ime.attached())
	if !ok || viaText {
		return widgets.KeyEventResultIgnored
	}
	buf := pv.term.Emulator().EncodeKey(ev, nil)
	if len(buf) == 0 {
		return widgets.KeyEventResultIgnored
	}
	pv.userInput(buf)
	return widgets.KeyEventResultHandled
}

// userInput sends what the user typed: the view goes to the bottom and the selection ends.
func (pv *paneView) userInput(b []byte) {
	pv.term.Send(b)
	pv.scrollTo(0)
	if pv.sel.active {
		pv.setSelection(selection{})
	}
	if pv.blink != nil {
		pv.blinkOn = true
	}
}

// typeText sends text an input method committed.
func (pv *paneView) typeText(text string) {
	if text == "" || !utf8.ValidString(text) {
		return
	}
	pv.userInput([]byte(text))
}

// ---- the input method ----

// imeClient is the connection to the input method of the desktop while the pane has the focus (SPEC §9.3): the delta model, an
// editing value that is always empty, so that what the method commits is typed and what it composes is shown at the cursor.
type imeClient struct {
	services.TextInputClientDefaults
	pv   *paneView
	conn *services.TextInputConnection
}

func (c *imeClient) attached() bool { return c.conn != nil && c.conn.Attached() }

// attach connects to the input method.
func (c *imeClient) attach(binding *widgets.WidgetsBinding) {
	if c.attached() {
		return
	}
	c.conn = binding.TextInput().Attach(c, services.NewTextInputConfiguration(services.TextInputConfigurationOpts{
		InputType: services.TextInputTypeMultiline, InputAction: services.TextInputActionNewline, EnableDeltaModel: true, DisableAutocorrect: true,
	}))
	c.conn.Show()
	c.conn.SetEditingState(services.TextEditingValue{Composing: gui.TextRangeEmpty})
}

// detach closes the connection.
func (c *imeClient) detach() {
	if c.attached() {
		c.conn.Close()
	}
	c.conn = nil
	if c.pv.preedit != "" {
		c.pv.preedit = ""
	}
}

// CurrentTextEditingValue implements services.TextInputClient: the field is empty.
func (c *imeClient) CurrentTextEditingValue() (services.TextEditingValue, bool) {
	return services.TextEditingValue{Composing: gui.TextRangeEmpty}, true
}

// CurrentAutofillScope implements services.TextInputClient.
func (c *imeClient) CurrentAutofillScope() services.AutofillScope { return nil }

// UpdateEditingValue implements services.TextInputClient for a method that sends whole values (the deltas are asked for).
func (c *imeClient) UpdateEditingValue(services.TextEditingValue) {}

// PerformAction implements services.TextInputClient.
func (c *imeClient) PerformAction(services.TextInputAction) {}

// PerformPrivateCommand implements services.TextInputClient.
func (c *imeClient) PerformPrivateCommand(string, map[string]any) {}

// UpdateFloatingCursor implements services.TextInputClient.
func (c *imeClient) UpdateFloatingCursor(services.RawFloatingCursorPoint) {}

// ShowAutocorrectionPromptRect implements services.TextInputClient.
func (c *imeClient) ShowAutocorrectionPromptRect(int, int) {}

// ConnectionClosed implements services.TextInputClient.
func (c *imeClient) ConnectionClosed() { c.conn = nil }

// UpdateEditingValueWithDeltas implements services.DeltaTextInputClient: the text a change commits is typed, the text still
// being composed is shown at the cursor, and the field stays empty.
func (c *imeClient) UpdateEditingValueWithDeltas(deltas []services.TextEditingDelta) {
	preedit := ""
	for _, d := range deltas {
		text := ""
		switch d := d.(type) {
		case services.TextEditingDeltaInsertion:
			text = d.TextInserted()
		case services.TextEditingDeltaReplacement:
			text = d.ReplacementText()
		}
		if comp := d.Composing(); comp.Start >= 0 && comp.End > comp.Start {
			preedit = text
			continue
		}
		if text != "" {
			c.pv.typeText(text)
		}
	}
	if preedit != c.pv.preedit {
		c.pv.preedit = preedit
		if c.pv.render != nil {
			c.pv.render.MarkNeedsPaint()
		}
	}
	if c.attached() && preedit == "" {
		c.conn.SetEditingState(services.TextEditingValue{Composing: gui.TextRangeEmpty})
	}
}

// scheduleInputGeometry tells the input method, after the frame, where the cursor is, so that its window opens there.
func (pv *paneView) scheduleInputGeometry(a *atlas, off gui.Offset) {
	if !pv.ime.attached() || pv.render == nil || pv.shown == nil {
		return
	}
	r := pv.render
	conn := pv.ime.conn
	cur := pv.frame.Cursor
	rect := gui.NewRectFromLTWH(padLeft+float64(cur.X)*a.m.w, float64(cur.Y)*a.m.h, a.m.w, a.m.h)
	scheduler := pv.binding.Scheduler()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if !conn.Attached() || !r.Attached() {
			return
		}
		conn.SetEditableSizeAndTransform(r.Size(), r.GetTransformTo(nil))
		conn.SetCaretRect(rect)
	})
}

// ---- the cursor blink ----

func (pv *paneView) stopBlink() {
	if pv.blink != nil {
		pv.blink.Cancel()
		pv.blink = nil
	}
	pv.blinkOn = true
}

// scheduleBlink starts the timer that toggles the cursor while the pane has the focus and the cursor blinks, and stops it
// otherwise.
func (pv *paneView) scheduleBlink(cfg *config, f *emulator.Frame) {
	want := pv.focused && pv.scroll == 0 && f.Cursor.Visible && (cfg.cursorBlink || f.Cursor.Blink)
	switch {
	case want && pv.blink == nil:
		pv.blink = pv.binding.Gestures().NewTimer(600*time.Millisecond, pv.blinkTick)
	case !want && pv.blink != nil:
		pv.stopBlink()
	}
}

func (pv *paneView) blinkTick() {
	pv.blink = nil
	pv.blinkOn = !pv.blinkOn
	if pv.render != nil {
		pv.render.MarkNeedsPaint()
	}
}

// ---- the pointer ----

// cellAt is the cell under a point of the pane, clamped to the grid.
func (pv *paneView) cellAt(p gui.Offset) (col, row int) {
	a, _ := pv.atlases()
	col = int((p.Dx - padLeft) / a.m.w)
	row = int(p.Dy / a.m.h)
	return min(max(col, 0), max(pv.cols-1, 0)), min(max(row, 0), max(pv.rows-1, 0))
}

// posOf is the place in the buffer of a cell of the view.
func (pv *paneView) posOf(col, row int) pos {
	top := int64(0)
	if pv.shown != nil {
		top = pv.shown.Top
	}
	return pos{line: top + int64(row), col: col}
}

// reportsMouse tells whether the program asked for mouse events and the user is not forcing a selection (Alt or Shift).
func (pv *paneView) reportsMouse() bool {
	if pv.term.Emulator().Modes().Mouse == emulator.MouseNone {
		return false
	}
	return pv.modsOf()&(emulator.ModAlt|emulator.ModShift) == 0
}

// buttonOf is the emulator's mouse button of the buttons of a pointer event.
func buttonOf(buttons int) (emulator.MouseButton, bool) {
	switch {
	case buttons&gestures.PrimaryButton != 0:
		return emulator.MouseLeft, true
	case buttons&gestures.TertiaryButton != 0:
		return emulator.MouseMiddle, true
	case buttons&gestures.SecondaryButton != 0:
		return emulator.MouseRight, true
	}
	return 0, false
}

// onPointerDown is a press in the pane.
func (pv *paneView) onPointerDown(e *gestures.PointerDownEvent) {
	pv.node.RequestFocus()
	local := e.LocalPosition()
	if pv.hitBar(local) {
		pv.beginBarDrag(local)
		return
	}
	col, row := pv.cellAt(local)
	btn, ok := buttonOf(e.Buttons())
	if !ok {
		return
	}
	if pv.reportsMouse() {
		pv.reporting = true
		pv.report(emulator.MouseEvent{Button: btn, Action: emulator.MousePress, Mods: pv.modsOf(), X: col, Y: row})
		return
	}
	cfg := pv.env.cfg()
	switch btn {
	case emulator.MouseRight:
		pv.rightClick(e, cfg, col, row)
	case emulator.MouseMiddle:
		pv.middleClick(cfg)
	default:
		pv.leftDown(e, col, row)
	}
}

// leftDown starts a selection: a click places the anchor, a double click selects a word and a triple click a line; Shift
// extends the selection that is there.
func (pv *paneView) leftDown(e *gestures.PointerDownEvent, col, row int) {
	local := e.LocalPosition()
	if pv.clicks > 0 && e.TimeStamp()-pv.lastDown < 400*time.Millisecond &&
		abs(local.Dx-pv.lastAt.Dx) <= 5 && abs(local.Dy-pv.lastAt.Dy) <= 5 {
		pv.clicks = min(pv.clicks+1, 3)
	} else {
		pv.clicks = 1
	}
	pv.lastDown, pv.lastAt = e.TimeStamp(), local
	p := pv.posOf(col, row)
	pv.pressed = true
	switch {
	case pv.clicks == 1 && pv.modsOf()&emulator.ModShift != 0 && pv.sel.active:
		s := pv.sel
		s.head = p
		pv.setSelection(s)
	case pv.clicks == 1:
		pv.setSelection(selection{anchor: p, head: p})
	case pv.clicks == 2:
		start, end := pv.wordAt(col, row)
		pv.setSelection(selection{active: true, anchor: pos{p.line, start}, head: pos{p.line, end}})
	default:
		first, last := pv.lineAt(row)
		pv.setSelection(selection{active: true, anchor: pv.posOf(0, first), head: pos{pv.posOf(0, last).line, pv.cols - 1}})
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// rightClick does what terminal.integrated.rightClickBehavior says.
func (pv *paneView) rightClick(e *gestures.PointerDownEvent, cfg *config, col, row int) {
	switch cfg.rightClick {
	case "nothing":
	case "paste":
		pv.pasteClipboard()
	case "copyPaste":
		if pv.sel.active {
			pv.copySelection()
			pv.setSelection(selection{})
		} else {
			pv.pasteClipboard()
		}
	case "selectWord":
		if !pv.sel.active {
			start, end := pv.wordAt(col, row)
			p := pv.posOf(col, row)
			pv.setSelection(selection{active: true, anchor: pos{p.line, start}, head: pos{p.line, end}})
		}
		pv.openMenu(e)
	default:
		pv.openMenu(e)
	}
}

func (pv *paneView) openMenu(e *gestures.PointerDownEvent) {
	if pv.env.menu != nil {
		pv.env.menu(pv, e.Position())
	}
}

// middleClick pastes the primary selection of the desktop (VS Code on Linux), or the clipboard with middleClickBehavior paste.
func (pv *paneView) middleClick(cfg *config) {
	if cfg.middleClick == "paste" {
		pv.pasteClipboard()
		return
	}
	pv.pastePrimary()
}

// onPointerMove is the pointer moving with a button down.
func (pv *paneView) onPointerMove(e *gestures.PointerMoveEvent) {
	local := e.LocalPosition()
	if pv.dragBar {
		pv.dragBarTo(local)
		return
	}
	col, row := pv.cellAt(local)
	if pv.reporting {
		btn, ok := buttonOf(e.Buttons())
		if !ok {
			btn = emulator.MouseNoButton
		}
		pv.report(emulator.MouseEvent{Button: btn, Action: emulator.MouseMove, Mods: pv.modsOf(), X: col, Y: row})
		return
	}
	if !pv.pressed || pv.clicks != 1 {
		return
	}
	// the selection follows the pointer; past the top or bottom edge it scrolls
	size := pv.render.Size()
	switch {
	case local.Dy < 0:
		pv.scrollBy(1)
	case local.Dy > size.Height:
		pv.scrollBy(-1)
	}
	col, row = pv.cellAt(local)
	s := pv.sel
	s.head = pv.posOf(col, row)
	s.active = s.head != s.anchor
	pv.setSelection(s)
}

// onPointerHover is the pointer moving with no button down.
func (pv *paneView) onPointerHover(e *gestures.PointerHoverEvent) {
	local := e.LocalPosition()
	over := pv.hitBar(local)
	if over != pv.hoverBar {
		pv.hoverBar = over
		if pv.render != nil {
			pv.render.MarkNeedsPaint()
		}
	}
	if pv.term.Emulator().Modes().Mouse == emulator.MouseAny && pv.reportsMouse() {
		col, row := pv.cellAt(local)
		pv.report(emulator.MouseEvent{Button: emulator.MouseNoButton, Action: emulator.MouseMove, Mods: pv.modsOf(), X: col, Y: row})
	}
}

// onPointerUp is the end of a press.
func (pv *paneView) onPointerUp(e *gestures.PointerUpEvent) {
	if pv.dragBar {
		pv.dragBar = false
		if pv.render != nil {
			pv.render.MarkNeedsPaint()
		}
		return
	}
	if pv.reporting {
		pv.reporting = false
		col, row := pv.cellAt(e.LocalPosition())
		btn, ok := buttonOf(e.Buttons())
		if !ok {
			btn = emulator.MouseLeft
		}
		pv.report(emulator.MouseEvent{Button: btn, Action: emulator.MouseRelease, Mods: pv.modsOf(), X: col, Y: row})
		return
	}
	if !pv.pressed {
		return
	}
	pv.pressed = false
	if pv.sel.active {
		cfg := pv.env.cfg()
		text := pv.selectedText()
		if text != "" {
			pv.setPrimary(text)
			if cfg.copyOnSelection {
				pv.copySelection()
			}
		}
	}
}

// report sends a mouse event the program asked for.
func (pv *paneView) report(ev emulator.MouseEvent) {
	if b := pv.term.Emulator().EncodeMouse(ev, nil); len(b) > 0 {
		pv.term.Send(b)
	}
}

// onPointerSignal is the wheel. The program gets it when it tracks the mouse, or on the alternate screen when it asked for the
// wheel to move the cursor; else the view scrolls (Alt for the fast sensitivity).
func (pv *paneView) onPointerSignal(e gestures.PointerSignalEvent) {
	scroll, ok := e.(*gestures.PointerScrollEvent)
	if !ok {
		return
	}
	dy := scroll.ScrollDelta().Dy
	if dy == 0 {
		return
	}
	pv.binding.Gestures().PointerSignalResolver.Register(e, func(gestures.PointerSignalEvent) { pv.wheel(scroll, dy) })
}

func (pv *paneView) wheel(scroll *gestures.PointerScrollEvent, dy float64) {
	mods := pv.modsOf()
	em := pv.term.Emulator()
	modes := em.Modes()
	if modes.Mouse != emulator.MouseNone && mods&emulator.ModShift == 0 || modes.AltScreen && modes.AltScroll && modes.Mouse == emulator.MouseNone {
		col, row := pv.cellAt(scroll.LocalPosition())
		btn := emulator.MouseWheelDown
		if dy < 0 {
			btn = emulator.MouseWheelUp
		}
		n := max(int(abs(dy)/notch+0.5), 1)
		for i := 0; i < n; i++ {
			pv.report(emulator.MouseEvent{Button: btn, Action: emulator.MousePress, Mods: mods, X: col, Y: row})
		}
		return
	}
	cfg := pv.env.cfg()
	a, _ := pv.atlases()
	k := cfg.wheelSens
	if mods&emulator.ModAlt != 0 {
		k *= cfg.fastSens
	}
	pv.wheelRest += -dy * k / a.m.h
	lines := int(pv.wheelRest)
	pv.wheelRest -= float64(lines)
	if lines != 0 {
		pv.scrollBy(lines)
		pv.barUntil = 0
	}
}

// hitBar tells whether a point is on the scrollbar.
func (pv *paneView) hitBar(p gui.Offset) bool {
	if pv.render == nil {
		return false
	}
	size := pv.render.Size()
	_, _, ok := pv.thumb(size)
	return ok && p.Dx >= size.Width-scrollbarW
}

func (pv *paneView) beginBarDrag(local gui.Offset) {
	size := pv.render.Size()
	top, h, _ := pv.thumb(size)
	pv.dragBar = true
	if local.Dy >= top && local.Dy <= top+h {
		pv.barGrab = local.Dy - top
	} else {
		pv.barGrab = h / 2
		pv.dragBarTo(local)
	}
}

func (pv *paneView) dragBarTo(local gui.Offset) {
	size := pv.render.Size()
	_, h, ok := pv.thumb(size)
	if !ok {
		return
	}
	top := min(max(local.Dy-pv.barGrab, 0), size.Height-h)
	frac := top / (size.Height - h)
	sb := pv.frame.Scrollback
	pv.scrollTo(sb - int(frac*float64(sb)+0.5))
}

// ---- the selection and the clipboard ----

// setSelection changes the selection.
func (pv *paneView) setSelection(s selection) {
	if s == pv.sel {
		return
	}
	pv.sel = s
	if pv.render != nil {
		pv.render.MarkNeedsPaint()
	}
	if pv.env.selectionChanged != nil {
		pv.env.selectionChanged(pv)
	}
}

// hasSelection tells whether text is selected.
func (pv *paneView) hasSelection() bool { return pv.sel.active }

// selectAll selects the whole buffer: the scrollback and the screen.
func (pv *paneView) selectAll() {
	em := pv.term.Emulator()
	first := em.FirstLine()
	last := first + int64(em.LineCount()) - 1
	cols, _ := pv.term.Size()
	pv.setSelection(selection{active: true, anchor: pos{first, 0}, head: pos{last, cols - 1}})
}

// selectedText is the text of the selection.
func (pv *paneView) selectedText() string {
	s, ok := pv.sel.normalized()
	if !ok {
		return ""
	}
	return pv.term.Emulator().TextRange(s.anchor.line, s.anchor.col, s.head.line, s.head.col+1)
}

// copySelection puts the selected text on the clipboard.
func (pv *paneView) copySelection() {
	text := pv.selectedText()
	if text == "" || pv.binding == nil {
		return
	}
	pv.binding.Clipboard().SetData(pv.env.clipboardCtx, services.NewClipboardData(text), func(error) {})
}

// setPrimary puts text on the primary selection of the desktop (Linux).
func (pv *paneView) setPrimary(text string) {
	if pv.binding == nil {
		return
	}
	pv.binding.Clipboard().SetPrimaryData(pv.env.clipboardCtx, services.NewClipboardData(text), func(error) {})
}

// pasteClipboard pastes the text of the clipboard.
func (pv *paneView) pasteClipboard() {
	if pv.binding == nil {
		return
	}
	pv.binding.Clipboard().GetData(pv.env.clipboardCtx, services.ClipboardKTextPlain, func(d *services.ClipboardData, err error) {
		if err == nil && d != nil {
			pv.paste(d.Text)
		}
	})
}

// pastePrimary pastes the primary selection of the desktop.
func (pv *paneView) pastePrimary() {
	if pv.binding == nil {
		return
	}
	pv.binding.Clipboard().GetPrimaryData(pv.env.clipboardCtx, services.ClipboardKTextPlain, func(d *services.ClipboardData, err error) {
		if err == nil && d != nil {
			pv.paste(d.Text)
		}
	})
}

// paste sends a text as a paste: as bracketed paste when the program asked for it, and after VS Code's warning about several
// lines (terminal.integrated.enableMultiLinePasteWarning).
func (pv *paneView) paste(text string) {
	if text == "" {
		return
	}
	cfg := pv.env.cfg()
	bracketed := pv.term.Emulator().Modes().BracketedPaste && !cfg.ignoreBracket
	lines := strings.Count(strings.TrimRight(text, "\r\n"), "\n") + 1
	warn := lines > 1 && (cfg.multiLinePaste == "always" || cfg.multiLinePaste == "auto" && !bracketed)
	send := func() {
		var b []byte
		if cfg.ignoreBracket {
			b = []byte(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\r"), "\n", "\r"))
		} else {
			b = pv.term.Emulator().EncodePaste(text, nil)
		}
		pv.userInput(b)
	}
	if warn && pv.env.confirmPaste != nil {
		pv.env.confirmPaste(lines, send)
		return
	}
	send()
}

// ---- words and lines ----

// cellText is the text of a cell of the view, "" for the right half of a wide character.
func (pv *paneView) cellText(row, col int) string {
	f := pv.shown
	if f == nil || row < 0 || row >= f.Rows || col < 0 || col >= f.Cols {
		return " "
	}
	return f.Text(row, col)
}

// wordAt is the first and last column of the word at a cell: the characters that are not separators
// (terminal.integrated.wordSeparators) or blank around it.
func (pv *paneView) wordAt(col, row int) (start, end int) {
	seps := pv.env.cfg().wordSeparators
	is := func(c int) bool {
		t := pv.cellText(row, c)
		if t == "" {
			return true // the right half of a wide character is part of its word
		}
		r, _ := utf8.DecodeRuneInString(t)
		return t != " " && !strings.ContainsRune(seps, r)
	}
	if !is(col) {
		return col, col
	}
	start, end = col, col
	for start > 0 && is(start-1) {
		start--
	}
	for end < pv.cols-1 && is(end+1) {
		end++
	}
	return start, end
}

// lineAt is the first and last row of the line at a row: the rows that autowrap joined.
func (pv *paneView) lineAt(row int) (first, last int) {
	f := pv.shown
	first, last = row, row
	if f == nil {
		return
	}
	for first > 0 && first-1 < len(f.Wrapped) && f.Wrapped[first-1] {
		first--
	}
	for last < f.Rows-1 && last < len(f.Wrapped) && f.Wrapped[last] {
		last++
	}
	return first, last
}

// ctx is the context of the clipboard requests of the extension.
func clipboardContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
