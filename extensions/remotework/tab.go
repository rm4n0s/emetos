package remotework

import (
	"context"
	"encoding/json"
	"image"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/remotework/loginui"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
	"github.com/rm4n0s/emetos/extensions/remotework/transport"
)

// DevConnectCommand is the dev command of the proofs (debug builds and builds with the emetos_dev tag only): in a Remote Work
// tab it fills the form and presses Connect. Arguments: {"host", "port", "keyFile", "workspace"}, or {"host", "port",
// "agent": true, "fingerprint"}.
const DevConnectCommand = "dev.remotework.connect"

// phase is what the tab shows.
type phase int

const (
	// phaseForm is the connect dialog.
	phaseForm phase = iota
	// phaseConnecting is the login and the transport being made, with Cancel.
	phaseConnecting
	// phasePicker is the host's list of workspaces, when several have Remote Work on.
	phasePicker
	// phaseConnected is the picture of the other computer's workspace.
	phaseConnected
	// phaseReconnecting is the last picture under "Reconnecting...".
	phaseReconnecting
	// phaseEnded is the last picture (or the form) under the reason the session ended.
	phaseEnded
)

// remoteTab is the window tab "Remote Work...": the connect dialog, and after it the picture of a workspace of another Emetos
// and the input that goes back to it (SPEC §10.18 "As built (step 15.4)"). Everything in it runs on the UI thread, except the
// goroutine of the session (tabsession.go) and the transport's callbacks, which hand their results over with Post.
type remoteTab struct {
	hub  *Hub
	api  extension.API
	tab  extension.WindowTab
	opts TabOptions
	log  *slog.Logger
	ctx  context.Context

	prompter *loginui.Prompter
	form     *tabForm
	in       *inputSurface

	// UI thread only.
	phase    phase
	gen      int
	note     note
	ended    endInfo
	picker   []string
	pickCh   chan pickAnswer
	attempt  int
	retryIn  time.Duration
	live     *liveSession
	size     tabSize
	scroll   ui.ScrollHandle
	binding  *widgets.WidgetsBinding
	workspac string
	disposed bool
	// failure is the error that ended the last attempt, for the log and the tests.
	failure error

	// picture is the latest picture, an image made from the canvas of the screen; dirty and queued keep the pictures from piling up
	// on the UI thread: the transport goroutine marks the screen changed, and one function that makes the image is queued at a time.
	picture *foundation.ValueNotifier[*gui.Image]
	dirty   atomic.Bool
	queued  atomic.Bool

	mu      sync.Mutex
	cancel  context.CancelFunc
	conn    *transport.ClientConn
	wg      sync.WaitGroup
	closing bool
}

// pickAnswer is the user's choice of a workspace: its name, or ok false for Cancel.
type pickAnswer struct {
	name string
	ok   bool
}

// liveSession is what the UI thread knows of the session that shows: its connection and its picture.
type liveSession struct {
	conn   *transport.ClientConn
	screen *transport.Screen
	// host is the workspace the host granted.
	workspace string
	gen       int
}

// createTab is the Create of the window tab kind: it makes the tab with the state it saved.
func (r *remoteWork) createTab(tab extension.WindowTab, raw json.RawMessage) (extension.WindowTabContent, error) {
	st, err := parseTabState(raw)
	if err != nil {
		return extension.WindowTabContent{}, err
	}
	t := &remoteTab{hub: r.hub, api: r.api, tab: tab, opts: r.hub.opts.Tab, log: r.api.Log(), ctx: r.ctx, phase: phaseForm,
		picture: foundation.NewValueNotifier[*gui.Image](nil), workspac: st.Workspace}
	t.prompter = loginui.NewPrompter(r.api.Post, tab.Refresh)
	t.form = newTabForm(t, st)
	t.in = newInputSurface(t)
	t.pickCh = make(chan pickAnswer, 1)
	tab.SetTitle("Remote Work")
	tab.SetHint("Use a workspace of another computer that runs Emetos")
	if r.ctx != nil {
		context.AfterFunc(r.ctx, t.shutdown)
	}
	r.tabs = append(r.tabs, t)
	return extension.WindowTabContent{Build: t.build, SaveState: t.saveState, OnClose: t.close}, nil
}

// saveState is the state kept across restarts: the form as it is, and the workspace used last.
func (t *remoteTab) saveState() (any, error) {
	st := t.form.state()
	st.Workspace = t.workspac
	return st, nil
}

// stateChanged tells the window the state to save changed.
func (t *remoteTab) stateChanged() { t.tab.StateChanged() }

// close is the tab closing for good: the session ends and the boxes are let go.
func (t *remoteTab) close() {
	t.shutdown()
	if t.disposed {
		return
	}
	t.disposed = true
	t.in.stop()
	t.form.dispose()
}

// shutdown ends the session and its goroutines; it may be called from any goroutine, more than once.
func (t *remoteTab) shutdown() {
	t.mu.Lock()
	if t.closing {
		t.mu.Unlock()
		t.wg.Wait()
		return
	}
	t.closing = true
	cancel, conn := t.cancel, t.conn
	t.cancel, t.conn = nil, nil
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close(protocol.ReasonClosed, "") // the connection only closes; the peer may be gone already
	}
	t.prompter.Close()
	// the goroutines only post to the UI thread, so waiting for them here cannot deadlock
	done := make(chan struct{})
	go func() { t.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.log.Warn("Remote Work: the session of a tab did not stop in time")
	}
}

// ---- what the tab shows ----

// build is the widget of the tab.
func (t *remoteTab) build(bc widgets.BuildContext) widgets.Widget {
	t.binding = widgets.WidgetsBindingOf(bc)
	colors := ui.Theme(bc)
	return widgets.NewLayoutBuilder(func(bc widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		t.noteSize(int(c.MaxWidth), int(c.MaxHeight))
		var body widgets.Widget
		switch t.phase {
		case phaseConnected:
			body = t.pictureView(bc, colors)
		case phaseReconnecting:
			body = widgets.NewStack([]widgets.Widget{t.pictureView(bc, colors), t.reconnectingCard(bc, colors)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
		case phaseEnded:
			under := t.formView(bc, colors)
			if t.picture.Value() != nil {
				under = t.pictureView(bc, colors)
			}
			body = widgets.NewStack([]widgets.Widget{under, t.endedCard(bc, colors)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
		case phasePicker:
			body = widgets.NewStack([]widgets.Widget{t.formView(bc, colors), t.pickerCard(bc, colors)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
		case phaseConnecting:
			body = t.formView(bc, colors)
			if !t.prompter.Showing() { // a question (the passphrase, the fingerprint) takes the place of the card
				body = widgets.NewStack([]widgets.Widget{body, t.connectingCard(bc, colors)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
			}
		default:
			body = t.formView(bc, colors)
		}
		children := []widgets.Widget{widgets.NewColoredBox(colors.EditorBackground, widgets.NewSizedBox(nil)), body}
		if d := t.prompter.Build(bc); d != nil {
			children = append(children, d)
		}
		return widgets.NewStack(children, widgets.StackOpts{Fit: rendering.StackFitExpand})
	})
}

// card is a message in a box over the middle of the tab, on a veil that keeps the pointer from the picture under it.
func (t *remoteTab) card(bc widgets.BuildContext, colors ui.Colors, content widgets.Widget) widgets.Widget {
	veil := colors.QuickInputBackground.WithAlpha(0x99)
	return widgets.NewListener(widgets.NewColoredBox(veil, widgets.NewCenter(
		widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(520.0)}),
			widgets.NewPadding(painting.NewEdgeInsetsAll(16), widgets.NewColoredBox(colors.EditorBackground,
				widgets.NewPadding(painting.NewEdgeInsetsAll(18), content)))))),
		widgets.ListenerOpts{Behavior: rendering.HitTestBehaviorOpaque})
}

func cardColumn(children ...widgets.Widget) widgets.Widget {
	return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
}

func cardTitle(bc widgets.BuildContext, colors ui.Colors, icon string, iconColor gui.Color, text string) widgets.Widget {
	return widgets.NewRow([]widgets.Widget{
		ui.Icon(icon, 18, iconColor), hgap(8),
		widgets.NewExpanded(widgets.NewText(text, widgets.TextOpts{Style: bold(ui.TextStyle(bc, 16, colors.Foreground))})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

func cardButtons(buttons ...widgets.Widget) widgets.Widget {
	return widgets.NewWrap(buttons, widgets.WrapOpts{Spacing: 8, RunSpacing: 8, Alignment: rendering.WrapAlignmentEnd})
}

// connectingCard says the connection is being made.
func (t *remoteTab) connectingCard(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	return t.card(bc, colors, cardColumn(
		cardTitle(bc, colors, "remote", colors.NotificationsInfoIconForeground, "Connecting to "+t.form.address()),
		gap(8),
		widgets.NewText("Logging in with the key, and opening the picture of the workspace.", widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.DescriptionForeground)}),
		gap(14),
		cardButtons(ui.NewButton("Cancel", ui.ButtonOpts{Secondary: true, OnPressed: t.disconnect})),
	))
}

// reconnectingCard says the connection dropped and is being made again.
func (t *remoteTab) reconnectingCard(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	detail := "Trying again"
	if t.retryIn > 0 {
		detail += " in " + t.retryIn.Round(time.Second).String()
	}
	detail += ". The picture below is the last one that arrived. Attempt " + strconv.Itoa(t.attempt) + "."
	return t.card(bc, colors, cardColumn(
		cardTitle(bc, colors, "sync", colors.NotificationsWarningIconForeground, "Reconnecting..."),
		gap(8),
		widgets.NewText(detail, widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.DescriptionForeground)}),
		gap(14),
		cardButtons(ui.NewButton("Disconnect", ui.ButtonOpts{Secondary: true, OnPressed: t.disconnect})),
	))
}

// endedCard says why the session ended.
func (t *remoteTab) endedCard(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	icon, color := "info", colors.NotificationsInfoIconForeground
	if t.ended.bad {
		icon, color = "error", colors.NotificationsErrorIconForeground
	}
	items := []widgets.Widget{cardTitle(bc, colors, icon, color, t.ended.title)}
	if t.ended.detail != "" {
		items = append(items, gap(8), widgets.NewText(t.ended.detail, widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.Foreground)}))
	}
	items = append(items, gap(14), cardButtons(
		ui.NewButton("Close Tab", ui.ButtonOpts{Secondary: true, OnPressed: t.tab.Close}),
		ui.NewButton("Edit Connection", ui.ButtonOpts{Secondary: true, OnPressed: t.backToForm}),
		ui.NewButton("Connect Again", ui.ButtonOpts{OnPressed: t.connect, Autofocus: true}),
	))
	return t.card(bc, colors, cardColumn(items...))
}

// pickerCard lists the workspaces of the host that have Remote Work on, to choose one.
func (t *remoteTab) pickerCard(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	items := []widgets.Widget{
		cardTitle(bc, colors, "remote", colors.NotificationsInfoIconForeground, "Which workspace?"),
		gap(8),
		widgets.NewText(t.form.address()+" has Remote Work on for several workspaces. Choose the one to use.", widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.DescriptionForeground)}),
		gap(12),
	}
	for _, name := range t.picker {
		name := name
		items = append(items, ui.NewButton(name, ui.ButtonOpts{Expand: true, Icon: iconCode("root-folder"), OnPressed: func() { t.answerPick(pickAnswer{name: name, ok: true}) }}), gap(6))
	}
	items = append(items, gap(8), cardButtons(ui.NewButton("Cancel", ui.ButtonOpts{Secondary: true, OnPressed: func() { t.answerPick(pickAnswer{}) }})))
	return t.card(bc, colors, cardColumn(items...))
}

// iconCode is the code point of a Codicon, 0 for an unknown name.
func iconCode(name string) int {
	code, _ := ui.CodiconCode(name)
	return code
}

// answerPick gives the session goroutine the choice of a workspace.
func (t *remoteTab) answerPick(a pickAnswer) {
	select {
	case t.pickCh <- a:
	default:
	}
}

// ---- size ----

// tabSize follows the size of the tab and sends it to the host once it has stayed as it is for a moment.
type tabSize struct {
	w, h int
	// sent is the last size the host was told.
	sentW, sentH int
	timer        *time.Timer
}

// noteSize records the size of the tab, as the layout reports it, and arranges for the host to be told (debounced).
func (t *remoteTab) noteSize(w, h int) {
	if w <= 0 || h <= 0 || (w == t.size.w && h == t.size.h) {
		return
	}
	t.size.w, t.size.h = w, h
	if t.phase != phaseConnected || t.live == nil {
		return
	}
	if t.size.timer != nil {
		t.size.timer.Stop()
	}
	gen := t.gen
	t.size.timer = time.AfterFunc(t.opts.resizeDelay(), func() {
		t.api.Post(func() {
			if t.gen == gen {
				t.sendSize()
			}
		})
	})
}

// sendSize tells the host the size of the tab, when it is not the one it knows.
func (t *remoteTab) sendSize() {
	w, h := fitSize(t.size.w, t.size.h)
	if t.live == nil || t.size.w <= 0 || (w == t.size.sentW && h == t.size.sentH) {
		return
	}
	if err := t.live.conn.Send(&protocol.Resize{Width: uint16(w), Height: uint16(h)}); err != nil {
		t.log.Debug("Remote Work: the size was not sent", "err", err)
		return
	}
	t.size.sentW, t.size.sentH = w, h
}

// ---- the picture ----

// pictureChanged is the transport goroutine telling that tiles were put on the picture: one function that makes the image is
// queued at a time, so pictures never pile up on the UI thread.
func (t *remoteTab) pictureChanged() {
	t.dirty.Store(true)
	if t.queued.Swap(true) {
		return
	}
	t.api.Post(t.refreshPicture)
}

// refreshPicture makes the image of the newest picture of the screen. It runs on the UI thread.
func (t *remoteTab) refreshPicture() {
	t.queued.Store(false)
	if !t.dirty.Swap(false) || t.live == nil {
		return
	}
	t.live.screen.Draw(func(img *image.RGBA) {
		if img == nil {
			return
		}
		pic, err := gui.DecodeImageFromPixels(context.Background(), img.Pix, img.Rect.Dx(), img.Rect.Dy(), gui.PixelFormatRGBA8888, gui.DecodeImageFromPixelsOpts{RowBytes: img.Stride})
		if err != nil {
			t.log.Warn("Remote Work: the picture cannot be drawn", "err", err)
			return
		}
		t.picture.SetValue(pic)
	})
}

// ---- errors ----

// endInfo is what the card of an ended session says.
type endInfo struct {
	// title is the reason in a line, and detail what the user can do or what the host said.
	title, detail string
	// bad tells whether it is a failure (an error icon), not a decision of somebody.
	bad bool
}

// describeError says in words why a login or a transport did not work. host is the address typed.
func describeError(host string, err error) endInfo {
	appErr, ok := errors.FromError(err)
	if !ok {
		return endInfo{title: "The connection failed", detail: err.Error(), bad: true}
	}
	meta := func(k string) string { s, _ := appErr.Metadata[k].(string); return s }
	switch appErr.Tag {
	case "SSHLoginHostKeyChanged":
		return endInfo{title: "The key of " + host + " changed", detail: "The connection was refused and nothing was sent. Somebody may be answering in place of that computer, or Remote Work was set up again there.", bad: true}
	case "SSHLoginHostKeyRejected":
		return endInfo{title: "You did not trust " + host, detail: "The first connection needs the fingerprint to be compared with the one on the Remote Work page of that computer.", bad: false}
	case "SSHLoginAuthFailed":
		return endInfo{title: host + " refused the key", detail: "Its public key is not in the list of the Remote Work page on that computer, or it was taken out of it.", bad: true}
	case "SSHLoginConnectFailed":
		return endInfo{title: "Cannot connect to " + host, detail: appErr.Message + ". Check the address and the port, and that Remote Work is on there.", bad: true}
	case "SSHLoginPassphraseCancelled":
		return endInfo{title: "The passphrase was not given", detail: "The key file is protected by a passphrase.", bad: false}
	case "SSHLoginWrongPassphrase":
		return endInfo{title: "The passphrase is wrong", detail: "It was asked for three times.", bad: true}
	case "SSHLoginKeyFileUnreadable", "SSHLoginKeyFileInvalid", "SSHLoginPassphraseRequired":
		return endInfo{title: "The key file cannot be used", detail: appErr.Message, bad: true}
	case "SSHLoginAgentUnavailable":
		return endInfo{title: "There is no ssh-agent", detail: "SSH_AUTH_SOCK does not name a running agent. Start one, or choose a key file.", bad: true}
	case "SSHLoginAgentNoKey", "SSHLoginAgentFailed":
		return endInfo{title: "The ssh-agent has no such key", detail: appErr.Message, bad: true}
	case "RemoteWorkLoginDenied":
		switch meta("status") {
		case "none":
			return endInfo{title: "Remote Work is off on " + host, detail: "No workspace there has Remote Work turned on. Turn it on in the Remote Work page of the workspace.", bad: true}
		case "unknown":
			return endInfo{title: "That workspace is not shared", detail: "It does not have Remote Work on any more.", bad: true}
		}
		return endInfo{title: host + " did not grant a session", detail: appErr.Message, bad: true}
	case "RemoteWorkVersion":
		return endInfo{title: "Version mismatch", detail: host + " speaks another version of Remote Work than this Emetos. Update the older one.", bad: true}
	case "TransportCertMismatch":
		return endInfo{title: "The picture channel of " + host + " showed another certificate", detail: "Something other than that Emetos answered on the UDP port, so nothing was sent to it.", bad: true}
	case "TransportRefused":
		return endInfo{title: host + " refused the session", detail: appErr.Message, bad: true}
	case "LoginUICancelled", "RemoteWorkChoiceCancelled":
		return endInfo{title: "Cancelled", bad: false}
	}
	return endInfo{title: "The connection failed", detail: appErr.Message, bad: true}
}

// describeDisconnect says what the host told when it ended the session.
func describeDisconnect(host string, d *protocol.Disconnect) endInfo {
	info := endInfo{}
	switch d.Reason {
	case protocol.ReasonHost:
		info.title = "Disconnected by " + host
		info.detail = "The user of that computer pressed Disconnect."
	case protocol.ReasonIdle:
		info.title = "The session was idle for too long"
		info.detail = host + " ends a session that sends nothing for its idle timeout."
	case protocol.ReasonOff:
		info.title = "Remote Work was turned off on " + host
	case protocol.ReasonProtocol:
		info.title, info.bad = "Protocol error", true
	case protocol.ReasonVersion:
		info.title, info.bad = "Version mismatch", true
		info.detail = host + " speaks another version of Remote Work than this Emetos. Update the older one."
	case protocol.ReasonReplaced:
		info.title = "Another computer took the session"
		info.detail = "Somebody logged in to this workspace of " + host + ", and a workspace has one session at a time."
	case protocol.ReasonWorkspaceClosed:
		info.title = "The workspace was closed on " + host
	default:
		info.title = host + " ended the session"
	}
	if d.Text != "" && info.detail == "" {
		info.detail = d.Text
	} else if d.Text != "" && d.Reason == protocol.ReasonProtocol {
		info.detail = d.Text
	}
	return info
}
