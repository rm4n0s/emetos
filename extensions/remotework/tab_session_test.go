package remotework

import (
	"encoding/json"
	"image/color"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// connectedRig is a rig whose tab is connected and shows a first picture.
func connectedRig(t *testing.T, tweak ...func(*TabOptions)) (*tabRig, *fakeapi.HeadlessView) {
	t.Helper()
	r := newTabRig(t, true, tweak...)
	r.open(uitest.Themes()[0], r.connectState())
	view := r.connect()
	want := frame(640, 480, color.RGBA{R: 20, G: 60, B: 120}, 0)
	view.Emit(want)
	r.shows("the first picture", want)
	return r, view
}

func TestTheHostEndingTheSessionShowsItsReasonOnTheTab(t *testing.T) {
	cases := []struct {
		name   string
		end    func(r *tabRig)
		title  string
		detail string
	}{
		{"the user of A presses Disconnect", func(r *tabRig) {
			r.a.Pump()
			b := sessionBanners(r.a)
			if len(b) != 1 || b[0].Banner().Text != "Remote Work: laptop B connected" {
				r.t.Fatalf("the banners on A = %+v", b)
			}
			b[0].Press("Disconnect")
		}, "Disconnected by 127.0.0.1", "pressed Disconnect"},
		{"the idle timeout", func(r *tabRig) {
			r.hub.Sessions()[0].End(reasonIdle, "no activity")
		}, "The session was idle for too long", "idle timeout"},
		{"Remote Work is turned off", func(r *tabRig) {
			if err := r.hub.SetWorkspace(r.folder, false); err != nil {
				r.t.Fatal(err)
			}
		}, "The host stopped sharing this workspace.", "Remote Work was turned off on 127.0.0.1"},
		{"another client takes the session", func(r *tabRig) {
			c := r.client(r.key)
			c.pin(r.rig)
			conn, err := c.connect(r.addr(), "")
			if err != nil {
				r.t.Fatal(err)
			}
			r.t.Cleanup(func() { _ = conn.Close() })
		}, "Another computer took the session", ""},
		{"the workspace is closed on A", func(r *tabRig) {
			r.unregister[r.folder]()
		}, "The workspace was closed on 127.0.0.1", ""},
		{"the host breaks the protocol", func(r *tabRig) {
			r.hub.Sessions()[0].End(reasonProtocol, "that was not a message")
		}, "Protocol error", "that was not a message"},
		{"the version is not the same", func(r *tabRig) {
			r.hub.Sessions()[0].End(protocol.ReasonVersion, "")
		}, "Version mismatch", "another version"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			noLeaks(t)
			r, _ := connectedRig(t)
			tc.end(r)
			r.until("the reason", func() bool { return r.tr.phase == phaseEnded })
			if !r.has(tc.title) {
				t.Errorf("the card does not say %q; it says %q", tc.title, r.tr.ended.title)
			}
			if tc.detail != "" && !strings.Contains(r.tr.ended.detail, tc.detail) {
				t.Errorf("the detail = %q, want %q in it", r.tr.ended.detail, tc.detail)
			}
			if r.wt.Capturing {
				t.Error("the tab still captures the keys under the reason")
			}
			// the host ended it on purpose: the tab does not connect again by itself
			time.Sleep(150 * time.Millisecond)
			r.settle()
			if r.tr.phase != phaseEnded {
				t.Errorf("the tab went on to phase %d", r.tr.phase)
			}
			// and the user can connect again
			if !r.has("Connect Again") || !r.has("Close Tab") || !r.has("Edit Connection") {
				t.Error("the card lacks its buttons")
			}
		})
	}
}

func TestTheTabConnectsAgainAfterTheConnectionDropsWithoutAskingForThePassphrase(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	r.file = writeKeyFile(t, r.key, []byte("correct horse"))
	st := r.connectState()
	r.open(uitest.Themes()[0], st)
	r.tr.connect()
	r.until("the passphrase question", r.tr.prompter.Showing)
	r.settle()
	if !r.has("Passphrase for the key") {
		t.Fatal("the passphrase is not asked for")
	}
	r.ts.EnterText(gluttertest.At(gluttertest.ByType[*ui.InputBox](), r.ts.Count(gluttertest.ByType[*ui.InputBox]())-1), "correct horse")
	r.ts.Tap(gluttertest.ByText("OK"))
	r.until("the connection", func() bool { return r.tr.phase == phaseConnected && len(r.a.HeadlessViews()) == 1 })
	first := r.a.HeadlessViews()[0]
	want := frame(640, 480, color.RGBA{R: 1, G: 2, B: 3}, 0)
	first.Emit(want)
	r.shows("the picture", want)

	// the network fails: the SSH connection of B breaks, and A is not heard to say goodbye
	r.mu.Lock()
	drop := r.dropSSH
	r.mu.Unlock()
	drop()
	r.until("the reconnection to be shown", func() bool { return r.tr.phase == phaseReconnecting || r.tr.attempt > 0 })
	if r.tr.phase == phaseReconnecting {
		if !r.has("Reconnecting...") {
			t.Error("the tab does not say it is reconnecting")
		}
		if r.wt.Capturing {
			t.Error("the tab captures the keys while it reconnects")
		}
		if r.tr.picture.Value() == nil {
			t.Error("the last picture is gone")
		}
	}
	r.until("the tab to be connected again", func() bool {
		return r.tr.phase == phaseConnected && len(r.a.HeadlessViews()) == 2
	})
	if r.tr.prompter.Showing() {
		t.Error("the passphrase was asked for again")
	}
	if !first.Closed() {
		t.Error("the view of the dropped session was not closed on A")
	}
	second := r.a.HeadlessViews()[1]
	if second.Size() != (extension.Size{Width: 640, Height: 480}) {
		t.Errorf("the new view is %v", second.Size())
	}
	want2 := frame(640, 480, color.RGBA{R: 9, G: 8, B: 7}, 30)
	second.Emit(want2)
	r.shows("the picture of the new session", want2)
	if got := r.wt.Title(); got != "127.0.0.1: project" {
		t.Errorf("the title = %q", got)
	}
}

func TestTheTabKeepsTryingWhileTheHostIsGoneAndGivesUpWhenItIsTold(t *testing.T) {
	noLeaks(t)
	var attempts atomic.Int64
	r, _ := connectedRig(t, func(o *TabOptions) {
		o.Backoff = func(n int) time.Duration {
			attempts.Store(int64(n))
			return 20 * time.Millisecond
		}
		o.HandshakeTimeout = time.Second
	})
	// the host goes away altogether: the connection breaks and nothing answers any more
	r.mu.Lock()
	drop := r.dropSSH
	r.mu.Unlock()
	drop()
	r.hub.Close()
	r.until("several attempts", func() bool { return attempts.Load() >= 3 })
	if r.tr.phase != phaseReconnecting {
		t.Fatalf("phase %d, want reconnecting", r.tr.phase)
	}
	if !r.has("Attempt ") {
		t.Error("the card does not say the attempt")
	}
	// the user gives up
	r.ts.Tap(gluttertest.ByText("Disconnect"))
	r.settle()
	if r.tr.phase != phaseForm || !r.has("Connect") {
		t.Errorf("phase %d after Disconnect", r.tr.phase)
	}
}

func TestAKeyThatIsNotListedAndAHostThatIsOffAreToldOnTheCard(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *tabRig)
		title string
	}{
		{"the key is not in the list", func(r *tabRig) {
			other := newKey(t)
			r.file = writeKeyFile(t, other, nil)
		}, "refused the key"},
		{"Remote Work is not on for any workspace", func(r *tabRig) {
			if err := r.hub.SetWorkspace(r.folder, false); err != nil {
				t.Fatal(err)
			}
			// the listener is gone with the last workspace: start it again for another that is off
			_, other := r.workspace("other")
			if err := r.hub.SetWorkspace(other, true); err != nil {
				t.Fatal(err)
			}
			if err := r.hub.SetWorkspace(other, false); err != nil {
				t.Fatal(err)
			}
		}, "cannot connect"},
		{"nothing listens", func(r *tabRig) { r.hub.Close() }, "Cannot connect"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			noLeaks(t)
			r := newTabRig(t, true)
			st := r.connectState()
			tc.setup(r)
			st.KeyFile = r.file
			r.open(uitest.Themes()[0], st)
			r.tr.connect()
			r.until("the card", func() bool { return r.tr.phase == phaseEnded })
			if !strings.Contains(strings.ToLower(r.tr.ended.title), strings.ToLower(tc.title)) {
				t.Errorf("the card says %q, want %q in it", r.tr.ended.title, tc.title)
			}
			if !r.tr.ended.bad {
				t.Error("a failure has no error icon")
			}
		})
	}
}

func TestTheFirstConnectionAsksToCompareTheFingerprintAndAChangedKeyIsRefused(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, false)
	r.open(uitest.Themes()[0], r.connectState())
	// the user does not trust it
	r.tr.connect()
	r.until("the question", r.tr.prompter.Showing)
	r.settle()
	signer, _ := r.hub.HostKey()
	if !r.has("Trust this computer?") || !r.has("SHA256:") {
		t.Fatal("the first-use question is not on screen")
	}
	if !r.has(ssh.FingerprintSHA256(signer.PublicKey())) {
		t.Errorf("the fingerprint of the host is not shown")
	}
	r.ts.Tap(gluttertest.ByText("Cancel"))
	r.until("the refusal", func() bool { return r.tr.phase == phaseEnded })
	if !strings.Contains(r.tr.ended.title, "You did not trust") {
		t.Errorf("the card says %q", r.tr.ended.title)
	}
	// trusting it connects, and pins it
	r.tr.backToForm()
	r.tr.connect()
	r.until("the question", r.tr.prompter.Showing)
	r.settle()
	r.ts.Tap(gluttertest.ByText("Trust"))
	r.until("the connection", func() bool { return r.tr.phase == phaseConnected })
	// a host whose key changed is refused before anything is sent, with the warning
	r.tr.stopSession()
	r.until("the end", func() bool { return len(r.hub.Sessions()) == 0 })
	other := newKey(t)
	path, err := r.hub.HostsPath()
	if err != nil {
		t.Fatal(err)
	}
	store := sshlogin.NewHostStore(path, nil)
	if err := store.Forget(r.addr()); err != nil {
		t.Fatal(err)
	}
	if err := store.Pin(r.addr(), other.signer.PublicKey()); err != nil {
		t.Fatal(err)
	}
	r.tr.gen++
	r.tr.phase = phaseForm
	r.tr.connect()
	r.until("the warning", func() bool { return r.tr.phase == phaseEnded && r.tr.prompter.Showing() })
	r.settle()
	if !r.has("The key of this computer changed") || !r.has("Forget the Old Key") {
		t.Error("the warning of a changed key is not on screen")
	}
	if !strings.Contains(r.tr.ended.title, "changed") {
		t.Errorf("the card says %q", r.tr.ended.title)
	}
	if len(r.hub.Sessions()) != 0 {
		t.Error("a session was granted to a host whose key changed")
	}
}

func TestAHostWithSeveralWorkspacesAsksWhichOne(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	_, second := r.workspace("second")
	if err := r.hub.SetWorkspace(second, true); err != nil {
		t.Fatal(err)
	}
	r.open(uitest.Themes()[0], r.connectState())
	r.tr.connect()
	r.until("the question", func() bool { return r.tr.phase == phasePicker })
	r.settle()
	if !r.has("Which workspace?") || !r.has("project") || !r.has("second") {
		t.Fatal("the choice is not on screen")
	}
	r.ts.Tap(gluttertest.ByText("second"))
	r.until("the connection", func() bool { return r.tr.phase == phaseConnected })
	if r.tr.live.workspace != "second" || r.wt.Title() != "127.0.0.1: second" {
		t.Errorf("workspace %q, title %q", r.tr.live.workspace, r.wt.Title())
	}
	// it is remembered for the next connection
	var st tabState
	if err := json.Unmarshal(r.wt.State(), &st); err != nil || st.Workspace != "second" {
		t.Errorf("the state = %s", r.wt.State())
	}
	// and cancelling the choice ends the attempt
	r.tr.stopSession()
	r.tr.gen++
	r.tr.phase, r.tr.workspac = phaseForm, ""
	r.until("the end", func() bool { return len(r.hub.Sessions()) == 0 })
	r.tr.connect()
	r.until("the question", func() bool { return r.tr.phase == phasePicker })
	r.settle()
	r.ts.Tap(gluttertest.ByText("Cancel"))
	r.until("the end", func() bool { return r.tr.phase == phaseEnded })
	if r.tr.ended.title != "Cancelled" {
		t.Errorf("the card says %q", r.tr.ended.title)
	}
	wantRoute(t, r.tr.failure, "remoteTab.dial.RemoteWorkChoiceCancelled")
}

func TestClosingTheTabEndsTheSessionAndEveryGoroutine(t *testing.T) {
	noLeaks(t)
	r, view := connectedRig(t)
	r.wt.Content.OnClose()
	r.until("the session to end on A", func() bool { return len(r.hub.Sessions()) == 0 })
	r.a.Pump()
	if !view.Closed() {
		t.Error("the view of A was not closed")
	}
	if got := sessionBanners(r.a); len(got) != 0 {
		t.Errorf("the banner of the session is still on A: %d", len(got))
	}
}

func TestDescribeErrorAndDescribeDisconnectSayWhyInWords(t *testing.T) {
	errs := []struct {
		tag   string
		meta  []any
		title string
	}{
		{"SSHLoginHostKeyChanged", nil, "changed"},
		{"SSHLoginHostKeyRejected", nil, "did not trust"},
		{"SSHLoginAuthFailed", nil, "refused the key"},
		{"SSHLoginConnectFailed", nil, "Cannot connect"},
		{"SSHLoginPassphraseCancelled", nil, "passphrase was not given"},
		{"SSHLoginWrongPassphrase", nil, "passphrase is wrong"},
		{"SSHLoginKeyFileUnreadable", nil, "key file cannot be used"},
		{"SSHLoginAgentUnavailable", nil, "no ssh-agent"},
		{"SSHLoginAgentNoKey", nil, "no such key"},
		{"RemoteWorkLoginDenied", []any{"status", "none"}, "is off"},
		{"RemoteWorkLoginDenied", []any{"status", "unknown"}, "not shared"},
		{"RemoteWorkVersion", nil, "Version mismatch"},
		{"TransportCertMismatch", nil, "another certificate"},
		{"TransportRefused", nil, "refused the session"},
		{"LoginUICancelled", nil, "Cancelled"},
		{"SomethingElse", nil, "failed"},
	}
	for _, tc := range errs {
		info := describeError("laptop", errors.New(tc.tag, "message of "+tc.tag, tc.meta...))
		if !strings.Contains(strings.ToLower(info.title), strings.ToLower(tc.title)) {
			t.Errorf("%s: title %q, want %q in it", tc.tag, info.title, tc.title)
		}
	}
	if info := describeError("laptop", errors.New("X", "a plain error")); info.title == "" {
		t.Error("an error with no tag has no title")
	}
	for r := protocol.ReasonClosed; r <= protocol.ReasonWorkspaceClosed; r++ {
		info := describeDisconnect("laptop", &protocol.Disconnect{Reason: r, Text: "because"})
		if info.title == "" {
			t.Errorf("reason %v has no words", r)
		}
	}
}
