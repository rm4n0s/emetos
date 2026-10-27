package remotework

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// hostRig is A with a workspace and a client that logged in over the transport and sends the messages of a tab by hand.
type hostRig struct {
	*rig
	api    *fakeapi.API
	folder string
	s      *session
}

func newHostRig(t *testing.T) *hostRig {
	t.Helper()
	noLeaks(t)
	r := newRig(t, rigOptions{transport: true, view: true})
	h := &hostRig{rig: r}
	h.api, h.folder = r.workspace("project")
	b := newKey(t)
	r.on(h.folder, b)
	h.s = r.transportSession(r.client(b))
	return h
}

// view waits for the view the first Resize makes.
func (h *hostRig) view() *fakeapi.HeadlessView {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		h.api.Pump()
		if v := h.api.HeadlessViews(); len(v) > 0 {
			return v[0]
		}
		if time.Now().After(deadline) {
			h.t.Fatal("the host made no view")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (h *hostRig) pumpUntil(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		h.api.Pump()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (h *hostRig) send(m protocol.Message) {
	h.t.Helper()
	if err := h.s.client.Send(m); err != nil {
		h.t.Fatal(err)
	}
}

func TestTheHostMakesTheViewAtTheSizeTheClientSaidAndResizesIt(t *testing.T) {
	h := newHostRig(t)
	h.send(&protocol.Resize{Width: 500, Height: 300})
	v := h.view()
	if v.Size() != (extension.Size{Width: 500, Height: 300}) {
		t.Errorf("the view is %v", v.Size())
	}
	h.send(&protocol.Resize{Width: 700, Height: 420})
	h.pumpUntil("the resize", func() bool { return v.Size().Width == 700 })
	if len(h.api.HeadlessViews()) != 1 {
		t.Errorf("%d views", len(h.api.HeadlessViews()))
	}
}

func TestTheInputOfTheClientReachesTheViewInTheOrderItWasSent(t *testing.T) {
	h := newHostRig(t)
	// input before the first size has no view to go to, and is dropped
	h.send(&protocol.Key{Kind: protocol.KeyDown, Logical: 1, Text: "x"})
	h.send(&protocol.Resize{Width: 500, Height: 300})
	v := h.view()
	h.send(&protocol.Clipboard{Text: "from the client"})
	h.send(&protocol.Key{Kind: protocol.KeyDown, Physical: 7, Logical: 9, Text: "é"})
	h.send(&protocol.Key{Kind: protocol.KeyRepeat, Physical: 7, Logical: 9, Text: "é"})
	h.send(&protocol.Key{Kind: protocol.KeyUp, Physical: 7, Logical: 9})
	h.send(&protocol.Mouse{Kind: protocol.MouseDown, Buttons: protocol.ButtonPrimary | protocol.ButtonSecondary, X: 12.5, Y: 40})
	h.send(&protocol.Mouse{Kind: protocol.MouseUp, X: 12.5, Y: 40})
	h.send(&protocol.Scroll{X: 3, Y: 4, DX: -1, DY: 25})
	h.pumpUntil("the input", func() bool { return len(v.Keys()) == 3 && len(v.Pointers()) == 2 && len(v.Scrolls()) == 1 })
	if v.Clipboard() != "from the client" {
		t.Errorf("the clipboard = %q", v.Clipboard())
	}
	keys := v.Keys()
	if keys[0].Kind != extension.KeyDown || keys[0].Text != "é" || keys[0].Physical != 7 || keys[0].Logical != 9 || keys[1].Kind != extension.KeyRepeat || keys[2].Kind != extension.KeyUp {
		t.Errorf("keys = %+v", keys)
	}
	p := v.Pointers()
	if p[0].Kind != extension.PointerDown || p[0].Buttons != extension.ButtonPrimary|extension.ButtonSecondary || p[0].X != 12.5 || p[0].Y != 40 || p[1].Kind != extension.PointerUp {
		t.Errorf("pointers = %+v", p)
	}
	if s := v.Scrolls()[0]; s != (extension.ScrollEvent{X: 3, Y: 4, DX: -1, DY: 25}) {
		t.Errorf("scroll = %+v", s)
	}
}

func TestTheHostAnswersAPingAndTellsTheClientWhatTheUserCopied(t *testing.T) {
	h := newHostRig(t)
	h.send(&protocol.Resize{Width: 500, Height: 300})
	v := h.view()
	h.send(&protocol.Ping{Seq: 7})
	if p, ok := nextMsg(t, h.s.client).(*protocol.Ping); !ok || p.Seq != 7 || !p.Reply {
		t.Errorf("answer = %+v", p)
	}
	h.pumpUntil("the copy to be wired", func() bool { return v.Copy("a line") })
	if c, ok := nextMsg(t, h.s.client).(*protocol.Clipboard); !ok || c.Text != "a line" {
		t.Errorf("clipboard = %+v", c)
	}
	// text that is empty or larger than the protocol allows is not sent
	v.Copy("")
	v.Copy(strings.Repeat("x", protocol.MaxClipboard+1))
	v.Copy("after")
	if c, ok := nextMsg(t, h.s.client).(*protocol.Clipboard); !ok || c.Text != "after" {
		t.Errorf("clipboard = %+v", c)
	}
}

func TestTheNotificationsOfTheWorkspaceGoToTheClientCutToTheLimitsAndItsAnswerPressesTheButton(t *testing.T) {
	h := newHostRig(t)
	h.send(&protocol.Resize{Width: 500, Height: 300})
	v := h.view()
	h.pumpUntil("the notifications to be wired", func() bool {
		return v.Notify(extension.ViewNotification{ID: 1, Source: extensionName, Message: "a key logged in"})
	})
	long := strings.Repeat("é", 4000) // 8000 bytes
	v.Notify(extension.ViewNotification{ID: 2, Severity: extension.NotificationError, Source: "Git", Message: long, Actions: []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", strings.Repeat("l", 300)}})
	n, ok := nextMsg(t, h.s.client).(*protocol.Notification)
	if !ok {
		t.Fatalf("got %T", n)
	}
	// the message of Remote Work itself (ID 1) did not come
	if n.ID != 2 || n.Severity != protocol.SeverityError || n.Source != "Git" || len(n.Message) > protocol.MaxMessageText || len(n.Message) < protocol.MaxMessageText-1 || len(n.Actions) != protocol.MaxActions {
		t.Errorf("notification = id %d sev %d source %q message %d bytes, %d actions", n.ID, n.Severity, n.Source, len(n.Message), len(n.Actions))
	}
	h.send(&protocol.NotificationAction{ID: 2, Action: 3})
	h.pumpUntil("the press", func() bool { return len(v.Presses()) == 1 })
	if p := v.Presses()[0]; p.ID != 2 || p.Action != 3 {
		t.Errorf("press = %+v", p)
	}
}

func TestAClientThatSendsWhatAHostDoesNotTakeIsDisconnected(t *testing.T) {
	h := newHostRig(t)
	h.send(&protocol.Resize{Width: 500, Height: 300})
	h.view()
	h.send(&protocol.Notification{ID: 1, Message: "I am the client"})
	for {
		m := nextMsg(t, h.s.client)
		if d, ok := m.(*protocol.Disconnect); ok {
			if d.Reason != protocol.ReasonProtocol || !strings.Contains(d.Text, "Notification") {
				t.Errorf("disconnect = %+v", d)
			}
			break
		}
	}
	h.pumpUntil("the view to close", func() bool { return h.api.HeadlessViews()[0].Closed() })
}

func TestAViewThatCannotBeMadeEndsTheSession(t *testing.T) {
	h := newHostRig(t)
	h.api.FailHeadlessViews(errors.New("FakeNoView", "no view for you"))
	h.send(&protocol.Resize{Width: 500, Height: 300})
	h.pumpUntil("the end", func() bool { _, _, ended := h.s.host.Reason(); return ended })
	reason, _, _ := h.s.host.Reason()
	if reason != protocol.ReasonProtocol {
		t.Errorf("reason = %v", reason)
	}
	if len(h.api.HeadlessViews()) != 0 {
		t.Error("a view was made")
	}

	// the error itself says where it came from
	hv := &hostView{hub: h.hub, w: h.hub.workspaceFor(h.folder), s: h.s.host}
	wantRoute(t, hv.open(extension.Size{Width: 1, Height: 1}), "hostView.open.RemoteWorkViewFailed")
}

func TestTheSessionEndsWithItsViewAndTheViewEndingEndsTheSession(t *testing.T) {
	h := newHostRig(t)
	h.send(&protocol.Resize{Width: 500, Height: 300})
	v := h.view()
	// the view goes (the workspace closed its folder): the client is told why
	v.Close()
	for {
		if d, ok := nextMsg(t, h.s.client).(*protocol.Disconnect); ok {
			if d.Reason != protocol.ReasonWorkspaceClosed {
				t.Errorf("disconnect = %+v", d)
			}
			break
		}
	}

	// a session that ends closes its view
	h2 := newHostRig(t)
	h2.send(&protocol.Resize{Width: 500, Height: 300})
	v2 := h2.view()
	h2.s.host.End(reasonHost, "bye")
	h2.pumpUntil("the view to close", v2.Closed)
}

func TestCutShortensAtACharacter(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 3, "hel"},
		{"héllo", 2, "h"},
		{"héllo", 3, "hé"},
		{"a\xffb", 10, "a�b"},
		{"", 5, ""},
	}
	for _, tc := range cases {
		if got := cut(tc.in, tc.n); got != tc.want {
			t.Errorf("cut(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}
