package remotework

import (
	"context"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
	"github.com/rm4n0s/emetos/extensions/remotework/login"
	"github.com/rm4n0s/emetos/extensions/remotework/loginui"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
	"github.com/rm4n0s/emetos/extensions/remotework/transport"
)

// ---- starting and stopping (the UI thread) ----

// spawn runs f on a goroutine that the tab waits for when it closes; it does nothing once the tab is closing.
func (t *remoteTab) spawn(f func()) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closing {
		return false
	}
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		f()
	}()
	return true
}

// isClosing tells whether the tab is closing.
func (t *remoteTab) isClosing() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closing
}

// connect reads the form and starts a session with it.
func (t *remoteTab) connect() {
	if t.phase == phaseConnecting || t.phase == phasePicker || t.phase == phaseConnected || t.phase == phaseReconnecting {
		return
	}
	p, err := t.form.state().params()
	if err != nil {
		t.phase = phaseForm
		t.failForm(err)
		return
	}
	p.Workspace = t.workspac
	t.stateChanged()
	t.begin(p)
}

// begin starts the goroutine that makes the session for p.
func (t *remoteTab) begin(p connectParams) {
	t.mu.Lock()
	if t.closing {
		t.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(t.ctxOrBackground())
	t.cancel = cancel
	t.wg.Add(1)
	t.mu.Unlock()
	t.gen++
	gen := t.gen
	t.phase, t.note, t.live, t.attempt = phaseConnecting, note{}, nil, 0
	t.picture.SetValue(nil)
	t.size.sentW, t.size.sentH = 0, 0
	go func() {
		defer t.wg.Done()
		t.run(ctx, gen, p)
	}()
	t.tab.Refresh()
}

// disconnect is the user ending the connection or giving up the connecting: the session stops and the form is back.
func (t *remoteTab) disconnect() {
	t.stopSession()
	t.gen++
	t.phase, t.note, t.live = phaseForm, note{"Disconnected.", false}, nil
	t.picture.SetValue(nil)
	t.tab.CaptureKeys(false)
	t.in.release()
	t.tab.Refresh()
}

// backToForm shows the form again, with what the user typed, after a session ended.
func (t *remoteTab) backToForm() {
	t.phase, t.note = phaseForm, note{}
	t.picture.SetValue(nil)
	t.tab.Refresh()
}

// stopSession stops the goroutine of the session and closes its connection.
func (t *remoteTab) stopSession() {
	t.mu.Lock()
	cancel, conn := t.cancel, t.conn
	t.cancel, t.conn = nil, nil
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close(protocol.ReasonClosed, "") // the connection only closes
	}
}

// post runs f on the UI thread if the session it belongs to is still the one the tab has.
func (t *remoteTab) post(gen int, f func()) {
	t.api.Post(func() {
		if t.gen == gen && !t.isClosing() {
			f()
		}
	})
}

// ---- the goroutine of a session ----

// run makes the session and keeps it: it logs in, opens the transport, serves the messages until the connection ends, and
// connects again, with a growing wait, when the connection dropped and the key can still be used. It returns when the host
// ended the session, the connection cannot be made again, or ctx ended.
func (t *remoteTab) run(ctx context.Context, gen int, p connectParams) {
	hosts, err := t.hostStore()
	if err != nil {
		t.post(gen, func() { t.fail(err) })
		return
	}
	var unlocked *sshlogin.Auth // a key file's key, kept in memory after the passphrase unlocked it, for the next connection
	workspace := p.Workspace
	connected := false
	for attempt := 0; ctx.Err() == nil; {
		auth, release, err := t.authenticate(ctx, p, &unlocked)
		if err != nil {
			t.post(gen, func() { t.fail(err) })
			return
		}
		conn, grant, err := t.dial(ctx, gen, p, auth, hosts, &workspace)
		release()
		if err != nil {
			switch {
			case ctx.Err() != nil:
				return
			case connected && retryable(err):
				attempt++
				if !t.wait(ctx, gen, attempt) {
					return
				}
				continue
			}
			t.post(gen, func() { t.fail(err) })
			return
		}
		connected, attempt = true, 0
		peer := t.serve(ctx, gen, conn, grant)
		if ctx.Err() != nil {
			return
		}
		if peer != nil {
			t.post(gen, func() { t.endedBy(peer) })
			return
		}
		attempt++
		if !t.wait(ctx, gen, attempt) {
			return
		}
	}
}

// hostStore is the store of pinned host keys of this computer.
func (t *remoteTab) hostStore() (*sshlogin.HostStore, error) {
	path, err := t.hub.HostsPath()
	if err != nil {
		return nil, err
	}
	return sshlogin.NewHostStore(path, nil), nil
}

// authenticate gives the key to log in with. A key file is unlocked once (the passphrase is asked in a dialog, used and wiped,
// never kept) and the unlocked key stays in memory for the reconnections of this session; the ssh-agent is asked each time.
// release must be called when the connection is made.
func (t *remoteTab) authenticate(ctx context.Context, p connectParams, unlocked **sshlogin.Auth) (*sshlogin.Auth, func(), error) {
	if p.KeyFile != "" {
		if *unlocked == nil {
			a, err := sshlogin.KeyFileAuth(p.KeyFile, t.prompter.Passphrase(p.KeyFile))
			if err != nil {
				return nil, nil, err
			}
			*unlocked = a
		}
		return *unlocked, func() {}, nil
	}
	a, err := sshlogin.AgentAuth(ctx, t.opts.agentSocket(), p.Fingerprint)
	if err != nil {
		return nil, nil, err
	}
	return a, func() { _ = a.Close() }, nil // the agent's socket is only needed for the login
}

// dial logs in and opens the transport. A host with several workspaces that has none named is asked about, and the answer
// is remembered in workspace.
func (t *remoteTab) dial(ctx context.Context, gen int, p connectParams, auth *sshlogin.Auth, hosts *sshlogin.HostStore, workspace *string) (*transport.ClientConn, login.Grant, error) {
	for {
		lc, err := login.Connect(ctx, login.ConnectOptions{
			Address: p.Addr, Workspace: *workspace, Auth: auth, Hosts: hosts, ConfirmHost: t.prompter.ConfirmHost(),
			HandshakeTimeout: t.opts.HandshakeTimeout,
		})
		if err != nil {
			names, ask := workspacesToChoose(err)
			if !ask {
				return nil, login.Grant{}, err
			}
			name, ok := t.choose(ctx, gen, names)
			if !ok {
				return nil, login.Grant{}, errors.New("RemoteWorkChoiceCancelled", "no workspace was chosen")
			}
			*workspace = name
			continue
		}
		o := transport.FromLogin(lc, p.Addr)
		if t.opts.Transport != nil {
			t.opts.Transport(&o)
		}
		conn, err := transport.Connect(ctx, o)
		if err != nil {
			return nil, login.Grant{}, err // Connect closed the login
		}
		return conn, lc.Grant, nil
	}
}

// workspacesToChoose reads the names a host offered when it has several workspaces on (or the one asked for is not), from the
// denial of the login; false for any other error and for a denial with nothing to choose from.
func workspacesToChoose(err error) ([]string, bool) {
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "RemoteWorkLoginDenied" {
		return nil, false
	}
	if s, _ := appErr.Metadata["status"].(string); s != login.StatusAmbiguous && s != login.StatusUnknown {
		return nil, false
	}
	names, _ := appErr.Metadata["workspaces"].([]string)
	return names, len(names) > 0
}

// choose shows the workspaces and waits for the user to pick one. ok is false when the user cancelled or the session ended.
func (t *remoteTab) choose(ctx context.Context, gen int, names []string) (string, bool) {
	select { // an answer left over
	case <-t.pickCh:
	default:
	}
	t.post(gen, func() {
		t.picker, t.phase = names, phasePicker
		t.tab.Refresh()
	})
	select {
	case a := <-t.pickCh:
		if a.ok {
			t.post(gen, func() {
				t.phase = phaseConnecting
				t.tab.Refresh()
			})
		}
		return a.name, a.ok
	case <-ctx.Done():
		return "", false
	}
}

// retryable tells whether a failure to connect is the network's (the host may be back soon) and not an answer (the key is
// refused, Remote Work is off, the key of the host changed).
func retryable(err error) bool {
	appErr, ok := errors.FromError(err)
	if !ok {
		return false
	}
	switch appErr.Tag {
	case "SSHLoginConnectFailed", "RemoteWorkGrantReadFailed", "SSHLoginChannelRefused", "TransportDialFailed", "TransportClosed", "TransportLinkFailed", "TransportHelloInvalid":
		return true
	}
	return false
}

// wait shows that the connection will be made again and sleeps for the backoff of the attempt. It returns false when the session
// ended meanwhile.
func (t *remoteTab) wait(ctx context.Context, gen, attempt int) bool {
	delay := t.opts.backoff(attempt)
	t.post(gen, func() { t.reconnecting(attempt, delay) })
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// serve shows the connection and serves its messages until it ends. It returns the farewell of the host, or nil when the
// connection dropped.
func (t *remoteTab) serve(ctx context.Context, gen int, conn *transport.ClientConn, grant login.Grant) *protocol.Disconnect {
	t.mu.Lock()
	if t.closing {
		t.mu.Unlock()
		_ = conn.Close(protocol.ReasonClosed, "") // the connection only closes
		return nil
	}
	t.conn = conn
	t.mu.Unlock()
	live := &liveSession{conn: conn, screen: transport.NewScreen(conn, t.log, t.pictureChanged), workspace: grant.Workspace, gen: gen}
	t.post(gen, func() { t.connected(live) })
	var peer *protocol.Disconnect
	for {
		m, err := conn.Next(ctx)
		if err != nil {
			break
		}
		switch m := m.(type) {
		case *protocol.Tile:
			live.screen.Handle(m)
		case *protocol.Notification:
			t.post(gen, func() { t.showNotification(m) })
		case *protocol.Clipboard:
			t.post(gen, func() { t.setClipboard(m.Text) })
		case *protocol.Disconnect:
			// ReasonClosed from the host is no choice of its user: its SSH connection ended or the hub stopped, as when the
			// network failed, so the tab connects again instead of showing a farewell.
			if m.Reason != protocol.ReasonClosed {
				peer = m
			}
		case *protocol.Ping:
			// a reply to a ping; the tab sends none
		default:
			t.log.Debug("Remote Work: a message the tab does not take", "type", m.Type().String())
		}
	}
	t.mu.Lock()
	if t.conn == conn {
		t.conn = nil
	}
	t.mu.Unlock()
	_ = conn.Close(protocol.ReasonClosed, "") // ends the goroutines of the connection; it does nothing when it ended
	return peer
}

// ---- what the session does to the tab (the UI thread) ----

// connected shows the picture of the workspace the host granted.
func (t *remoteTab) connected(live *liveSession) {
	t.live = live
	t.phase, t.attempt, t.retryIn = phaseConnected, 0, 0
	t.workspac = live.workspace
	address := t.form.address()
	t.tab.SetTitle(t.form.state().Host + ": " + live.workspace)
	t.tab.SetHint("Remote Work: " + live.workspace + " on " + address)
	t.stateChanged()
	t.size.sentW, t.size.sentH = 0, 0
	t.sendSize()
	t.in.reset()
	t.tab.CaptureKeys(true)
	t.tab.Refresh()
	t.in.focus()
}

// reconnecting shows that the connection dropped and will be made again.
func (t *remoteTab) reconnecting(attempt int, delay time.Duration) {
	t.phase, t.attempt, t.retryIn, t.live = phaseReconnecting, attempt, delay, nil
	t.tab.CaptureKeys(false)
	t.in.release()
	t.tab.Refresh()
}

// fail shows why a session could not be made or kept. A host key that changed also gets its warning.
func (t *remoteTab) fail(err error) {
	t.live, t.failure = nil, err
	t.phase, t.ended = phaseEnded, describeError(t.form.address(), err)
	t.tab.CaptureKeys(false)
	t.in.release()
	t.prompter.ShowChanged(err, func(host loginui.ChangedHost) {
		path, perr := t.hub.HostsPath()
		if perr != nil {
			t.log.Warn("Remote Work: the old key of a host cannot be forgotten", "err", perr)
			return
		}
		if ferr := sshlogin.NewHostStore(path, nil).Forget(host.Address); ferr != nil {
			t.log.Warn("Remote Work: the old key of a host cannot be forgotten", "err", ferr)
		}
	})
	t.tab.Refresh()
}

// endedBy shows the reason the host gave for ending the session.
func (t *remoteTab) endedBy(d *protocol.Disconnect) {
	t.live = nil
	t.phase, t.ended = phaseEnded, describeDisconnect(t.form.address(), d)
	t.tab.CaptureKeys(false)
	t.in.release()
	t.tab.Refresh()
}

// send puts a message of the user on the connection to the host. The UI thread.
func (t *remoteTab) send(m protocol.Message) {
	if t.live == nil {
		return
	}
	if err := t.live.conn.Send(m); err != nil {
		t.log.Debug("Remote Work: a message was not sent", "type", m.Type().String(), "err", err)
	}
}

// showNotification shows a notification of the host's workspace, whose buttons are pressed on the host.
func (t *remoteTab) showNotification(n *protocol.Notification) {
	actions := make([]extension.NotificationAction, len(n.Actions))
	for i, label := range n.Actions {
		i := i
		actions[i] = extension.NotificationAction{Label: label, Run: func() { t.send(&protocol.NotificationAction{ID: n.ID, Action: uint8(i)}) }}
	}
	msg := n.Message
	if n.Source != "" {
		msg = n.Source + ": " + msg
	}
	msg = t.form.state().Host + ": " + msg
	switch n.Severity {
	case protocol.SeverityWarning:
		t.api.Notifications().Warn(msg, actions...)
	case protocol.SeverityError:
		t.api.Notifications().Error(msg, actions...)
	default:
		t.api.Notifications().Info(msg, actions...)
	}
}

// setClipboard puts the text the user copied on the host on the clipboard of this computer.
func (t *remoteTab) setClipboard(text string) {
	if t.binding == nil {
		return
	}
	t.binding.Clipboard().SetData(t.ctxOrBackground(), services.NewClipboardData(text), func(err error) {
		if err != nil {
			t.log.Warn("Remote Work: the copied text cannot be put on the clipboard", "err", err)
		}
	})
}
