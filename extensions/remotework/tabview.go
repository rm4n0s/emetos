package remotework

import (
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// The picture is drawn in logical pixels (SPEC §10.18 "As built (step 15.4)"): the view of the host is made as large as the tab is in
// logical pixels, and Glutter draws it at a scale of 1, so on a display with a pixel ratio above 1 it is stretched, with the filter
// of the image. The pointer positions are the tab's logical positions, which are the pixels of the picture.

const (
	// moveEvery is how often a moving pointer is told to the host: the moves between two sends are one.
	moveEvery = 8 * time.Millisecond
	// clipboardWait is how long a paste waits for the clipboard of this computer before the key goes on without it.
	clipboardWait = time.Second
)

// pictureView is the picture of the other computer's workspace, with the keyboard, the pointer and the scroll of the tab going
// to it.
func (t *remoteTab) pictureView(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	in := t.in
	pic := widgets.NewValueListenableBuilder(t.picture, func(_ widgets.BuildContext, img *gui.Image, _ widgets.Widget) widgets.Widget {
		if img == nil {
			return widgets.NewSizedBox(nil)
		}
		// the picture is drawn at its own size, from the top left corner, whatever the size of the tab is: a tab that was resized
		// shows the old picture for a moment, not a stretched one
		w, h := float64(img.Width()), float64(img.Height())
		return widgets.NewClipRect(widgets.NewOverflowBox(
			widgets.NewRawImage(img, widgets.RawImageOpts{Scale: 1, Fit: painting.BoxFitFill, FilterQuality: gui.FilterQualityLow}),
			widgets.OverflowBoxOpts{Alignment: painting.AlignmentTopLeft, MinWidth: foundation.Some(w), MaxWidth: foundation.Some(w), MinHeight: foundation.Some(h), MaxHeight: foundation.Some(h)}))
	}, nil)
	surface := widgets.NewListener(widgets.NewColoredBox(colors.EditorBackground, pic), widgets.ListenerOpts{
		Behavior:        rendering.HitTestBehaviorOpaque,
		OnPointerDown:   in.onDown,
		OnPointerMove:   in.onMove,
		OnPointerUp:     in.onUp,
		OnPointerHover:  in.onHover,
		OnPointerSignal: in.onSignal,
	})
	return widgets.NewFocus(surface, widgets.FocusOpts{
		FocusNode: in.node, Autofocus: true, OnKeyEvent: in.onKey, OnFocusChange: in.focusChanged, DebugLabel: "RemoteWorkView",
	})
}

// inputSurface turns what the user does in the picture into the messages of the protocol, in order: keys with their text,
// the text an input method committed, the pointer, the scroll, and the clipboard before a paste. It lives on the UI thread.
type inputSurface struct {
	t    *remoteTab
	node *widgets.FocusNode
	ime  *imeClient

	// pressed are the keys that went down and not up yet (physical to logical), so that none is stuck on the host when the focus
	// goes or the connection is lost, and a key up whose down was a window shortcut is not sent.
	pressed map[uint64]uint64
	// move is the pointer move that waits for its turn, and moveTimer the timer that sends it.
	move      *protocol.Mouse
	moveTimer *time.Timer
	// busy is true while a paste waits for the clipboard; the messages after it wait in queue, so that the text arrives before
	// the key and the order of the keys is kept.
	busy  bool
	queue []func()
	// focused is whether the picture has the keyboard focus.
	focused bool
}

func newInputSurface(t *remoteTab) *inputSurface {
	in := &inputSurface{t: t, node: widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "RemoteWorkView"}), pressed: map[uint64]uint64{}}
	in.ime = &imeClient{in: in}
	return in
}

// stop lets go of the focus node and the input method. The UI thread.
func (in *inputSurface) stop() {
	if in.moveTimer != nil {
		in.moveTimer.Stop()
	}
	in.ime.detach()
	in.node.Dispose()
}

// focus gives the picture the keyboard focus.
func (in *inputSurface) focus() {
	in.t.api.Post(func() {
		if in.t.phase == phaseConnected {
			in.node.RequestFocus()
		}
	})
}

// reset forgets what a session left: the keys, the pointer and the queue.
func (in *inputSurface) reset() {
	clear(in.pressed)
	in.move, in.busy, in.queue = nil, false, nil
}

// release sends the release of the keys that are down (the focus went, or the connection did) and forgets the state.
func (in *inputSurface) release() {
	in.ime.detach()
	t := in.t
	if t.live != nil {
		for phys, logical := range in.pressed {
			t.send(&protocol.Key{Kind: protocol.KeyUp, Physical: phys, Logical: logical})
		}
	}
	in.reset()
}

// focusChanged is the picture getting or losing the keyboard focus.
func (in *inputSurface) focusChanged(has bool) {
	in.focused = has
	if in.t.phase != phaseConnected {
		return
	}
	if has {
		if in.t.binding != nil {
			in.ime.attach(in.t.binding)
		}
		return
	}
	// the keys the user holds when the focus goes are let go on the host
	in.ime.detach()
	for phys, logical := range in.pressed {
		in.emit(&protocol.Key{Kind: protocol.KeyUp, Physical: phys, Logical: logical})
	}
	clear(in.pressed)
}

// ---- order ----

// emit sends a message after the ones before it; while a paste waits for the clipboard it waits too.
func (in *inputSurface) emit(m protocol.Message) { in.run(func() { in.t.send(m) }) }

// run does f now, or after the paste that is waiting.
func (in *inputSurface) run(f func()) {
	if in.busy {
		in.queue = append(in.queue, f)
		return
	}
	f()
}

// drain does what waited for a paste, until the next paste.
func (in *inputSurface) drain() {
	for !in.busy && len(in.queue) > 0 {
		f := in.queue[0]
		in.queue[0] = nil
		in.queue = in.queue[1:]
		f()
	}
}

// ---- keys ----

// onKey is a key event of the keyboard. In a picture of another computer every key is that computer's: the shortcuts of this
// window that act on the window itself were taken before it got here (WindowTab.CaptureKeys), and the rest are sent.
func (in *inputSurface) onKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	t := in.t
	if t.phase != phaseConnected || t.live == nil {
		return widgets.KeyEventResultIgnored
	}
	var kind protocol.KeyKind
	switch e.(type) {
	case *services.KeyDownEvent:
		kind = protocol.KeyDown
	case *services.KeyRepeatEvent:
		kind = protocol.KeyRepeat
	case *services.KeyUpEvent:
		kind = protocol.KeyUp
	default:
		return widgets.KeyEventResultIgnored
	}
	phys, logical := e.PhysicalKey().USBHidUsage(), e.LogicalKey().KeyID()
	switch kind {
	case protocol.KeyUp:
		if _, down := in.pressed[phys]; !down {
			return widgets.KeyEventResultHandled // its down was a shortcut of this window
		}
		delete(in.pressed, phys)
	default:
		in.pressed[phys] = logical
	}
	m := &protocol.Key{Kind: kind, Physical: phys, Logical: logical}
	if kind != protocol.KeyUp {
		if text := e.Character(); text != "" && len(text) <= protocol.MaxKeyText && utf8.ValidString(text) {
			m.Text = text
		}
	}
	if kind != protocol.KeyUp && in.isPaste(e) {
		in.paste(m)
	} else {
		in.emit(m)
	}
	return widgets.KeyEventResultHandled
}

// isPaste tells whether the key event is the paste of the desktop: Ctrl+V, Ctrl+Shift+V or Shift+Insert.
func (in *inputSurface) isPaste(e services.KeyEvent) bool {
	if in.t.binding == nil {
		return false
	}
	kb := in.t.binding.Keyboard()
	switch e.PhysicalKey() {
	case services.PhysicalKeyboardKeyKeyV:
		return kb.IsControlPressed() && !kb.IsAltPressed()
	case services.PhysicalKeyboardKeyInsert:
		return kb.IsShiftPressed() && !kb.IsControlPressed() && !kb.IsAltPressed()
	}
	return false
}

// paste sends the text of the clipboard of this computer to the host, and then the key that pastes. The clipboard is read only
// here, when the user pastes; the host never asks for it. The messages after the key wait for it.
func (in *inputSurface) paste(key *protocol.Key) {
	t := in.t
	in.run(func() {
		in.busy = true
		done := false
		var timer *time.Timer
		finish := func(text string) {
			if done {
				return
			}
			done = true
			if timer != nil {
				timer.Stop()
			}
			switch {
			case text == "":
			case len(text) > protocol.MaxClipboard:
				t.log.Info("Remote Work: the clipboard is too large to paste into the other computer", "bytes", len(text))
			default:
				t.send(&protocol.Clipboard{Text: text})
			}
			t.send(key)
			in.busy = false
			in.drain()
		}
		timer = time.AfterFunc(clipboardWait, func() { t.api.Post(func() { finish("") }) })
		t.binding.Clipboard().GetData(t.ctxOrBackground(), services.ClipboardKTextPlain, func(d *services.ClipboardData, err error) {
			text := ""
			if err == nil && d != nil {
				text = d.Text
			} else if err != nil {
				t.log.Debug("Remote Work: the clipboard cannot be read", "err", err)
			}
			t.api.Post(func() { finish(text) })
		})
	})
}

// ---- the pointer ----

func buttonsOf(b int) uint8 {
	return uint8(b) & (protocol.ButtonPrimary | protocol.ButtonSecondary | protocol.ButtonMiddle)
}

func (in *inputSurface) mouse(kind protocol.MouseKind, buttons int, p gui.Offset) {
	in.emit(&protocol.Mouse{Kind: kind, Buttons: buttonsOf(buttons), X: float32(p.Dx), Y: float32(p.Dy)})
}

func (in *inputSurface) onDown(e *gestures.PointerDownEvent) {
	if in.t.phase != phaseConnected {
		return
	}
	in.node.RequestFocus()
	in.flushMove()
	in.mouse(protocol.MouseDown, e.Buttons(), e.LocalPosition())
}

func (in *inputSurface) onUp(e *gestures.PointerUpEvent) {
	if in.t.phase != phaseConnected {
		return
	}
	in.flushMove()
	in.mouse(protocol.MouseUp, e.Buttons(), e.LocalPosition())
}

func (in *inputSurface) onMove(e *gestures.PointerMoveEvent) {
	in.moved(e.Buttons(), e.LocalPosition())
}

func (in *inputSurface) onHover(e *gestures.PointerHoverEvent) {
	in.moved(e.Buttons(), e.LocalPosition())
}

// moved remembers the newest position of the pointer, and has it sent soon: the moves between two sends are one.
func (in *inputSurface) moved(buttons int, p gui.Offset) {
	if in.t.phase != phaseConnected {
		return
	}
	in.move = &protocol.Mouse{Kind: protocol.MouseMove, Buttons: buttonsOf(buttons), X: float32(p.Dx), Y: float32(p.Dy)}
	if in.moveTimer == nil {
		in.moveTimer = time.AfterFunc(moveEvery, func() { in.t.api.Post(in.flushMove) })
	} else {
		in.moveTimer.Reset(moveEvery)
	}
}

// flushMove sends the pointer move that waits.
func (in *inputSurface) flushMove() {
	if in.move == nil {
		return
	}
	m := in.move
	in.move = nil
	if in.moveTimer != nil {
		in.moveTimer.Stop()
	}
	in.emit(m)
}

// onSignal is a signal of the pointer: the wheel or the trackpad scrolling.
func (in *inputSurface) onSignal(e gestures.PointerSignalEvent) {
	s, ok := e.(*gestures.PointerScrollEvent)
	if !ok || in.t.phase != phaseConnected {
		return
	}
	in.flushMove()
	p, d := s.LocalPosition(), s.ScrollDelta()
	in.emit(&protocol.Scroll{X: float32(p.Dx), Y: float32(p.Dy), DX: float32(d.Dx), DY: float32(d.Dy)})
}

// ---- the input method ----

// imeClient is the connection to the input method of the desktop while the picture has the focus: it gets the text the method
// committed (a composed character, an emoji) and sends it as the text of a key. The preedit, the text still being composed, is
// the method's own and is not shown in the picture.
type imeClient struct {
	services.TextInputClientDefaults
	in   *inputSurface
	conn *services.TextInputConnection
}

// attach connects to the input method.
func (c *imeClient) attach(binding *widgets.WidgetsBinding) {
	if c.conn != nil && c.conn.Attached() {
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
	if c.conn != nil && c.conn.Attached() {
		c.conn.Close()
	}
	c.conn = nil
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

// UpdateEditingValueWithDeltas implements services.DeltaTextInputClient: the text a change commits (an insertion or a replacement
// that leaves nothing composing) is sent; the field stays empty.
func (c *imeClient) UpdateEditingValueWithDeltas(deltas []services.TextEditingDelta) {
	for _, d := range deltas {
		if comp := d.Composing(); comp.Start >= 0 && comp.End > comp.Start {
			continue // still composing
		}
		text := ""
		switch d := d.(type) {
		case services.TextEditingDeltaInsertion:
			text = d.TextInserted()
		case services.TextEditingDeltaReplacement:
			text = d.ReplacementText()
		}
		if text != "" {
			c.in.commit(text)
		}
	}
	if c.conn != nil && c.conn.Attached() {
		c.conn.SetEditingState(services.TextEditingValue{Composing: gui.TextRangeEmpty})
	}
}

// commit sends text the input method committed as a key that types it.
func (in *inputSurface) commit(text string) {
	if in.t.phase != phaseConnected || len(text) > protocol.MaxKeyText || !utf8.ValidString(text) {
		return
	}
	r, _ := utf8.DecodeRuneInString(text)
	logical := uint64(r)
	in.emit(&protocol.Key{Kind: protocol.KeyDown, Logical: logical, Text: text})
	in.emit(&protocol.Key{Kind: protocol.KeyUp, Logical: logical})
}
