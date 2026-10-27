package remotework

import (
	"context"
	"image"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
	"github.com/rm4n0s/emetos/extensions/remotework/transport"
)

// extensionName is the name the notifications of this extension carry as their source (the Name of its manifest). They are
// about Remote Work on the host, so they stay there.
const extensionName = "Remote Work"

// hostView is the host's side of one session (SPEC §10.18 "As built (step 15.4)"): the headless view of the workspace that the
// client's size asked for, the picture flow from its frames to the client, and the client's input, clipboard and answers to
// notifications going the other way. It lives as long as the session. The messages are handled on the UI thread, in the order
// they arrived, because the view is made and used there; the goroutine that reads them only posts.
type hostView struct {
	hub  *Hub
	w    *workspaceHost
	s    *Session
	conn transport.Conn
	pres *transport.Presenter

	mu     sync.Mutex
	view   extension.HeadlessView
	closed bool
}

// runView serves the client of a session until the session ends: it reads the messages of the transport, and ends the
// session when the client breaks the protocol. It returns when the transport is gone and the view is closed. The hub calls it on
// the goroutine of the session, after the grant was sent.
func (h *Hub) runView(w *workspaceHost, s *Session) {
	conn := s.conn
	if conn == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.endCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	pres, err := transport.NewPresenter(ctx, conn, transport.PresenterOptions{
		Clock: transport.FuncClock{NowFunc: h.opts.Now, After: h.opts.After}, Logger: h.log,
	})
	if err != nil {
		h.log.Error("Remote Work: the picture flow cannot start", "err", err)
		s.End(reasonProtocol, "the host cannot send pictures")
		return
	}
	hv := &hostView{hub: h, w: w, s: s, conn: conn, pres: pres}
	defer hv.close()
	for {
		m, err := conn.Next(ctx)
		if err != nil {
			return
		}
		hv.handle(m)
	}
}

// close ends the view and the picture flow. A view that is being made on the UI thread at this moment sees closed and closes
// itself (open).
func (hv *hostView) close() {
	hv.mu.Lock()
	hv.closed = true
	v := hv.view
	hv.view = nil
	hv.mu.Unlock()
	hv.pres.Close()
	if v != nil {
		v.Close()
	}
}

// handle takes one message of the client.
func (hv *hostView) handle(m protocol.Message) {
	switch m := m.(type) {
	case *protocol.FrameConfirm, *protocol.Refresh:
		hv.pres.Handle(m)
	case *protocol.Disconnect:
		// the client said goodbye: the transport ends the session
	case *protocol.Ping:
		if !m.Reply {
			if err := hv.conn.Send(&protocol.Ping{Seq: m.Seq, Reply: true}); err != nil {
				hv.hub.log.Debug("Remote Work: a ping was not answered", "err", err)
			}
		}
	case *protocol.Resize, *protocol.Key, *protocol.Mouse, *protocol.Scroll, *protocol.Clipboard, *protocol.NotificationAction:
		// the view is made and used on the UI thread, and the messages keep their order
		hv.w.api.Post(func() { hv.apply(m) })
	default:
		hv.hub.log.Warn("Remote Work: the client sent a message a host does not take", "type", m.Type().String())
		hv.s.End(reasonProtocol, "the host does not take a "+m.Type().String()+" message")
	}
}

// apply does what a message of the client asks of the view. It runs on the UI thread.
func (hv *hostView) apply(m protocol.Message) {
	hv.mu.Lock()
	v, closed := hv.view, hv.closed
	hv.mu.Unlock()
	if closed {
		return
	}
	if r, ok := m.(*protocol.Resize); ok {
		size := extension.Size{Width: int(r.Width), Height: int(r.Height)}
		var err error
		if v == nil {
			err = hv.open(size)
		} else {
			err = v.Resize(size)
		}
		if err != nil {
			hv.hub.log.Warn("Remote Work: the view cannot be shown", "err", err)
			hv.s.End(reasonProtocol, "the workspace cannot be shown at that size")
		}
		return
	}
	if v == nil {
		return // input before the first size: the client sends its size first, so there is nothing to give it to
	}
	switch m := m.(type) {
	case *protocol.Key:
		v.SendKey(extension.KeyEvent{Kind: extension.KeyKind(m.Kind), Physical: m.Physical, Logical: m.Logical, Text: m.Text})
	case *protocol.Mouse:
		v.SendPointer(extension.PointerEvent{Kind: extension.PointerKind(m.Kind), Buttons: m.Buttons, X: float64(m.X), Y: float64(m.Y)})
	case *protocol.Scroll:
		v.SendScroll(extension.ScrollEvent{X: float64(m.X), Y: float64(m.Y), DX: float64(m.DX), DY: float64(m.DY)})
	case *protocol.Clipboard:
		v.SetClipboard(m.Text)
	case *protocol.NotificationAction:
		v.RunNotificationAction(m.ID, int(m.Action))
	}
}

// open makes the view at the size the client asked for and wires it to the session: its pictures to the picture flow, what
// the user copies in it to the client's clipboard and the notifications of the workspace to the client. It runs on the UI
// thread. It fails with RemoteWorkViewFailed.
func (hv *hostView) open(size extension.Size) error {
	v, err := hv.w.api.Workspace().NewHeadlessView(size)
	if err != nil {
		return errors.NewErr("RemoteWorkViewFailed", err).SetMetadata("width", size.Width, "height", size.Height)
	}
	hv.mu.Lock()
	if hv.closed {
		hv.mu.Unlock()
		v.Close()
		return nil
	}
	hv.view = v
	hv.mu.Unlock()
	v.OnFrame(func(img *image.RGBA) { hv.pres.Present(img) })
	v.OnCopy(hv.copied)
	v.OnNotification(hv.notified)
	// the view goes with its workspace: the folder closed or changed
	go func() {
		select {
		case <-v.Done():
			hv.s.End(reasonWorkspaceClosed, "the workspace was closed")
		case <-hv.s.endCh:
		}
	}()
	return nil
}

// copied sends the text the user copied or cut in the view to the client's clipboard. It runs on the UI thread.
func (hv *hostView) copied(text string) {
	if text == "" || len(text) > protocol.MaxClipboard {
		hv.hub.log.Debug("Remote Work: a copy was not sent to the client", "bytes", len(text))
		return
	}
	if err := hv.conn.Send(&protocol.Clipboard{Text: text}); err != nil {
		hv.hub.log.Debug("Remote Work: the copied text was not sent", "err", err)
	}
}

// notified sends a notification of the workspace to the client, with the id it is answered with. The messages of this
// extension about Remote Work stay on the host. It runs on the UI thread.
func (hv *hostView) notified(n extension.ViewNotification) {
	if n.Source == extensionName {
		return
	}
	m := &protocol.Notification{
		ID: n.ID, Severity: protocol.Severity(n.Severity), Source: cut(n.Source, protocol.MaxSource),
		Message: cut(n.Message, protocol.MaxMessageText),
	}
	for i, a := range n.Actions {
		if i == protocol.MaxActions {
			break
		}
		m.Actions = append(m.Actions, cut(a, protocol.MaxActionLabel))
	}
	if err := hv.conn.Send(m); err != nil {
		hv.hub.log.Debug("Remote Work: a notification was not sent to the client", "err", err)
	}
}

// cut shortens a text to at most n bytes without splitting a character; a byte that is not UTF-8 is replaced, as the protocol takes
// UTF-8 only.
func cut(s string, n int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
