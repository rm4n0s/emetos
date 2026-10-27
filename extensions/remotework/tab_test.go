package remotework

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
	"github.com/rm4n0s/emetos/extensions/remotework/transport"
)

// shownSize is the size of the tab in the tests: small, so that a picture is a few tiles and the race detector keeps up.
var shownSize = gui.Size{Width: 640, Height: 480}

// tabRig is machine A (a hub with a workspace on a fake API, serving over QUIC on the loopback) and machine B (the window tab of
// the extension on a fake API, shown in a window of the tester), with a key that A lists and B has in a file.
type tabRig struct {
	*rig
	a      *fakeapi.API
	folder string
	b      *fakeapi.API
	ext    *remoteWork
	wt     *fakeapi.WindowTab
	tr     *remoteTab
	ts     *gluttertest.Tester
	key    testKey
	file   string
	// size is the size of the window of open; shownSize when it is zero.
	size gui.Size

	built   int
	rebuild func()

	mu sync.Mutex
	// dropSSH closes the SSH connection of the newest session on B's side, as a network that fails does.
	dropSSH func()
}

// newTabRig makes the two machines and opens the tab on B, not yet connected. pinned says that B trusts A's host key already (the
// first-use question is tested on its own). tweak changes the options of the tab.
func newTabRig(t *testing.T, pinned bool, tweak ...func(*TabOptions)) *tabRig {
	t.Helper()
	opts := TabOptions{Backoff: func(int) time.Duration { return 30 * time.Millisecond }, ResizeDelay: 20 * time.Millisecond, AgentSocket: func() string { return "" }}
	for _, f := range tweak {
		f(&opts)
	}
	tr := &tabRig{key: newKey(t)}
	user := opts.Transport
	opts.Transport = func(o *transport.ClientOptions) {
		tr.mu.Lock()
		tr.dropSSH = o.CloseSSH
		tr.mu.Unlock()
		if user != nil {
			user(o)
		}
	}
	r := newRig(t, rigOptions{transport: true, view: true, hubTweak: func(h *HubOptions) { h.Tab = opts }})
	tr.rig = r
	tr.a, tr.folder = r.workspace("project")
	r.on(tr.folder, tr.key)
	tr.file = writeKeyFile(t, tr.key, nil)
	if pinned {
		pin(t, r)
	}

	tr.ext = &remoteWork{hub: r.hub}
	tr.b = fakeapi.New(t, tr.ext.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWindowTab, Name: "Remote Work", WindowTab: TabKindID, ID: ID + ":t1"})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := tr.ext.Activate(ctx, tr.b); err != nil {
		t.Fatal(err)
	}
	return tr
}

// writeKeyFile writes the private key to a file in OpenSSH format, with a passphrase when one is given.
func writeKeyFile(t *testing.T, k testKey, passphrase []byte) string {
	t.Helper()
	var block *pem.Block
	var err error
	if passphrase == nil {
		block, err = ssh.MarshalPrivateKey(k.priv, "test")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(k.priv, "test", passphrase)
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// pin makes B trust the host key of A's hub for its address, as a user who compared the fingerprints does.
func pin(t *testing.T, r *rig) {
	t.Helper()
	signer, err := r.hub.HostKey()
	if err != nil {
		t.Fatal(err)
	}
	path, err := r.hub.HostsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := sshlogin.NewHostStore(path, nil).Pin(r.addr(), signer.PublicKey()); err != nil {
		t.Fatal(err)
	}
}

// open starts the tab with a state and shows it in a window.
func (r *tabRig) open(th uitest.Theme, state any) {
	r.t.Helper()
	var raw json.RawMessage
	if state != nil {
		data, err := json.Marshal(state)
		if err != nil {
			r.t.Fatal(err)
		}
		raw = data
	}
	r.wt = r.b.StartWindowTab("t1", raw)
	if len(r.ext.tabs) != 1 {
		r.t.Fatalf("%d tabs made", len(r.ext.tabs))
	}
	r.tr = r.ext.tabs[0]
	host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		r.rebuild = func() { set(func() {}) }
		return r.wt.Content.Build(bc)
	})
	size := shownSize
	if r.size.Width > 0 {
		size = r.size
	}
	r.ts = uitest.Pump(r.t, th, size, host)
	r.built = r.wt.Refreshs
	r.t.Cleanup(func() {
		if r.wt.Content.OnClose != nil {
			r.wt.Content.OnClose()
		}
	})
}

// connectState is the state of a tab that points at A with the key file.
func (r *tabRig) connectState() tabState {
	_, port, _ := strings.Cut(r.addr(), ":")
	n, _ := strconv.Atoi(port)
	return tabState{Host: "127.0.0.1", Port: n, Key: keyFile, KeyFile: r.file}
}

// settle runs what both machines posted and builds B's tab again when it asked for it, until nothing is left.
func (r *tabRig) settle() {
	r.t.Helper()
	for i := 0; i < 100; i++ {
		ran := r.a.Pump()
		if r.b.Pump() {
			ran = true
		}
		if r.wt.Refreshs != r.built {
			r.built = r.wt.Refreshs
			r.rebuild()
			ran = true
		}
		r.ts.Pump()
		if !ran {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// until settles until the condition holds.
func (r *tabRig) until(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		r.settle()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s (phase %d, ended %q: %q)", what, r.tr.phase, r.tr.ended.title, r.tr.ended.detail)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (r *tabRig) has(text string) bool { return r.ts.Exists(gluttertest.ByTextContaining(text)) }

// connect presses Connect and waits for the picture to be there.
func (r *tabRig) connect() *fakeapi.HeadlessView {
	r.t.Helper()
	r.tr.connect()
	r.until("the tab to be connected", func() bool { return r.tr.phase == phaseConnected && len(r.a.HeadlessViews()) == 1 })
	return r.a.HeadlessViews()[0]
}

// frame is a picture of a color with a mark, as the view of A would draw.
func frame(w, h int, c color.RGBA, mark int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	for y := 10; y < 20; y++ {
		for x := 10 + mark; x < 20+mark; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	return img
}

// shows waits until the picture on B is the frame.
func (r *tabRig) shows(what string, want *image.RGBA) {
	r.t.Helper()
	r.until(what, func() bool {
		pic := r.tr.picture.Value()
		if pic == nil || pic.Width() != want.Rect.Dx() || pic.Height() != want.Rect.Dy() {
			return false
		}
		got := pic.Pixels()
		for i := range want.Pix {
			if got[i] != want.Pix[i] {
				return false
			}
		}
		return true
	})
}

func wantTag(t *testing.T, err error, tag string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %s", tag)
	}
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != tag {
		t.Fatalf("error = %v, want tag %s", err, tag)
	}
}

func TestTheTabKeepsAndReadsItsStateWithoutAnythingSecret(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    tabState
		wantTag string
	}{
		{"a new tab", ``, tabState{Port: DefaultPort, Key: keyFile}, ""},
		{"a key file", `{"host":"laptop","port":2222,"key":"file","keyFile":"/k/id"}`, tabState{Host: "laptop", Port: 2222, Key: keyFile, KeyFile: "/k/id"}, ""},
		{"the agent and a fingerprint", `{"host":"laptop","port":47123,"key":"agent","fingerprint":"SHA256:abc","workspace":"api"}`, tabState{Host: "laptop", Port: 47123, Key: keyAgent, Fingerprint: "SHA256:abc", Workspace: "api"}, ""},
		{"no port is the default", `{"host":"laptop","key":"file"}`, tabState{Host: "laptop", Port: DefaultPort, Key: keyFile}, ""},
		{"a port that is not one", `{"host":"laptop","port":70000,"key":"file"}`, tabState{}, "RemoteWorkTabStateInvalid"},
		{"a way to choose the key that is not one", `{"host":"laptop","port":1,"key":"password"}`, tabState{}, "RemoteWorkTabStateInvalid"},
		{"an unknown field (a passphrase)", `{"host":"laptop","port":1,"key":"file","passphrase":"x"}`, tabState{}, "RemoteWorkTabStateInvalid"},
		{"not JSON", `{`, tabState{}, "RemoteWorkTabStateInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTabState(json.RawMessage(tc.raw))
			if tc.wantTag != "" {
				wantTag(t, err, tc.wantTag)
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("state = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
	// what the tab saves is the form, and has no field for a passphrase, a token or a key
	r := newTabRig(t, true)
	r.open(uitest.Themes()[0], tabState{Host: "laptop", Port: 2222, Key: keyAgent, Fingerprint: "SHA256:abc"})
	saved := r.wt.State()
	var m map[string]any
	if err := json.Unmarshal(saved, &m); err != nil {
		t.Fatal(err)
	}
	for k := range m {
		switch k {
		case "host", "port", "key", "keyFile", "fingerprint", "workspace":
		default:
			t.Errorf("the saved state has a field %q", k)
		}
	}
	if m["host"] != "laptop" || m["key"] != "agent" || m["fingerprint"] != "SHA256:abc" {
		t.Errorf("saved state = %s", saved)
	}
	// a restored tab is the form again: it does not connect on its own
	if r.tr.phase != phaseForm || r.tr.live != nil {
		t.Errorf("a restored tab is in phase %d with a session %v, want the form", r.tr.phase, r.tr.live)
	}
}

func TestTheFormIsReadAsAConnectionOrRefusedWithItsOwnReason(t *testing.T) {
	cases := []struct {
		name    string
		st      tabState
		want    connectParams
		wantTag string
	}{
		{"a host and a key file", tabState{Host: " Laptop.Local ", Port: 47123, Key: keyFile, KeyFile: " /k/id "}, connectParams{Host: "Laptop.Local", Port: 47123, Addr: "laptop.local:47123", KeyFile: "/k/id"}, ""},
		{"an IPv6 address in brackets", tabState{Host: "[::1]", Port: 5, Key: keyFile, KeyFile: "/k"}, connectParams{Host: "::1", Port: 5, Addr: "[::1]:5", KeyFile: "/k"}, ""},
		{"the agent", tabState{Host: "h", Port: 5, Key: keyAgent, Fingerprint: "SHA256:x"}, connectParams{Host: "h", Port: 5, Addr: "h:5", Fingerprint: "SHA256:x"}, ""},
		{"no host", tabState{Port: 5, Key: keyFile, KeyFile: "/k"}, connectParams{}, "RemoteWorkHostInvalid"},
		{"a host with a space", tabState{Host: "a b", Port: 5, Key: keyFile, KeyFile: "/k"}, connectParams{}, "RemoteWorkHostInvalid"},
		{"a host that is a user and a host", tabState{Host: "me@laptop", Port: 5, Key: keyFile, KeyFile: "/k"}, connectParams{}, "RemoteWorkHostInvalid"},
		{"a port of 0", tabState{Host: "h", Port: 0, Key: keyFile, KeyFile: "/k"}, connectParams{}, "RemoteWorkPortInvalid"},
		{"a port that is too large", tabState{Host: "h", Port: 65536, Key: keyFile, KeyFile: "/k"}, connectParams{}, "RemoteWorkPortInvalid"},
		{"no key file", tabState{Host: "h", Port: 5, Key: keyFile}, connectParams{}, "RemoteWorkKeyChoiceInvalid"},
		{"a way to choose that is not one", tabState{Host: "h", Port: 5, Key: "x"}, connectParams{}, "RemoteWorkKeyChoiceInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.st.params()
			if tc.wantTag != "" {
				wantTag(t, err, tc.wantTag)
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("params = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestFitSizeKeepsAPictureWithinTheProtocol(t *testing.T) {
	cases := []struct{ w, h, wantW, wantH int }{
		{400, 300, 400, 300},
		{0, 0, 1, 1},
		{-5, 20, 1, 20},
		{9000, 100, protocol.MaxDimension, 100},
		{7000, 7000, 0, 0}, // scaled down until it is at most MaxPixels
	}
	for _, tc := range cases {
		w, h := fitSize(tc.w, tc.h)
		if tc.wantW != 0 && (w != tc.wantW || h != tc.wantH) {
			t.Errorf("fitSize(%d, %d) = %d, %d; want %d, %d", tc.w, tc.h, w, h, tc.wantW, tc.wantH)
		}
		if !protocol.ValidSize(w, h) {
			t.Errorf("fitSize(%d, %d) = %d, %d is not a size the protocol allows", tc.w, tc.h, w, h)
		}
	}
}

func TestTheTabConnectsAndShowsThePictureOfTheWorkspaceAtItsSize(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	r.open(uitest.Themes()[0], r.connectState())
	r.settle()
	view := r.connect()

	if got := r.wt.Title(); got != "127.0.0.1: project" {
		t.Errorf("the title = %q", got)
	}
	if !strings.Contains(r.wt.Hint(), "project") {
		t.Errorf("the hint = %q", r.wt.Hint())
	}
	if !r.wt.Capturing {
		t.Error("the tab does not capture the keys while it shows the picture")
	}
	if size := view.Size(); size != (extension.Size{Width: 640, Height: 480}) {
		t.Errorf("the view of A is %v, the tab is 640x480", size)
	}
	want := frame(640, 480, color.RGBA{R: 20, G: 60, B: 120}, 0)
	if !view.Emit(want) {
		t.Fatal("the view has nobody to give its pictures to")
	}
	r.shows("the picture", want)
	// A's next picture replaces it
	next := frame(640, 480, color.RGBA{R: 20, G: 60, B: 120}, 100)
	view.Emit(next)
	r.shows("A's edit", next)
	if !r.ts.Exists(gluttertest.ByType[*widgets.RawImage]()) {
		t.Error("the picture is not on screen")
	}
	// the state keeps the workspace used
	var st tabState
	if err := json.Unmarshal(r.wt.State(), &st); err != nil || st.Workspace != "project" {
		t.Errorf("the saved state = %s (%v)", r.wt.State(), err)
	}
}

func TestTheSizeOfTheTabIsSentToTheHostOnceItHasSettled(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	r.open(uitest.Themes()[0], r.connectState())
	view := r.connect()
	// a window that is dragged to its size goes through many: only the last is sent
	for _, w := range []int{410, 420, 430, 440, 450} {
		r.tr.noteSize(w, 300)
	}
	r.until("the new size", func() bool { return view.Size().Width == 450 })
	if view.Size() != (extension.Size{Width: 450, Height: 300}) {
		t.Errorf("the view is %v", view.Size())
	}
	// a size that the protocol does not allow is brought within it
	r.tr.noteSize(9000, 8000)
	r.until("the largest size", func() bool { return view.Size().Width*view.Size().Height > 3_000_000 })
	if s := view.Size(); !protocol.ValidSize(s.Width, s.Height) {
		t.Errorf("the view is %v", s)
	}
}

func TestTheKeysTheMouseAndTheScrollOfTheTabGoToTheViewOfTheHost(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	r.open(uitest.Themes()[0], r.connectState())
	view := r.connect()
	r.settle()
	fake := view
	typed := func() []extension.KeyEvent { r.settle(); return fake.Keys() }

	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyKeyA, gluttertest.KeyOpts{Character: "a"})
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyKeyA)
	r.until("the keys", func() bool { return len(typed()) == 2 })
	k := fake.Keys()
	if k[0].Kind != extension.KeyDown || k[0].Text != "a" || k[0].Logical != services.LogicalKeyboardKeyKeyA.KeyID() || k[0].Physical == 0 {
		t.Errorf("the key down = %+v", k[0])
	}
	if k[1].Kind != extension.KeyUp || k[1].Text != "" || k[1].Physical != k[0].Physical {
		t.Errorf("the key up = %+v", k[1])
	}
	// a key up whose down never went (a shortcut of this window took it) is not sent
	before := len(fake.Keys())
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyKeyQ)
	r.settle()
	time.Sleep(50 * time.Millisecond)
	r.settle()
	if len(fake.Keys()) != before {
		t.Errorf("a key up with no key down was sent: %+v", fake.Keys()[before:])
	}

	// text from an input method
	r.ts.SendTextInputEvent(gui.TextInputEvent{Commit: "é", PreeditCursorBegin: -1, PreeditCursorEnd: -1})
	r.until("the committed text", func() bool { return len(typed()) >= before+2 })
	if k := fake.Keys()[before]; k.Kind != extension.KeyDown || k.Text != "é" {
		t.Errorf("the committed text = %+v", k)
	}

	// the mouse and the scroll, in the pixels of the picture
	r.ts.Hover(gui.Offset{Dx: 50, Dy: 60})
	g := r.ts.StartGesture(gui.Offset{Dx: 70, Dy: 80})
	g.Up()
	r.ts.ScrollAt(gui.Offset{Dx: 90, Dy: 100}, gui.Offset{Dx: 0, Dy: 40})
	r.until("the pointer events", func() bool { r.settle(); return len(fake.Pointers()) >= 3 && len(fake.Scrolls()) >= 1 })
	var kinds []extension.PointerKind
	for _, p := range fake.Pointers() {
		kinds = append(kinds, p.Kind)
	}
	var down, up *extension.PointerEvent
	for i, p := range fake.Pointers() {
		switch p.Kind {
		case extension.PointerDown:
			down = &fake.Pointers()[i]
		case extension.PointerUp:
			up = &fake.Pointers()[i]
		}
	}
	if down == nil || up == nil || down.Buttons&extension.ButtonPrimary == 0 || down.X != 70 || down.Y != 80 {
		t.Errorf("the pointer events = %+v (kinds %v)", fake.Pointers(), kinds)
	}
	if s := fake.Scrolls()[0]; s.DY != 40 || s.X != 90 || s.Y != 100 {
		t.Errorf("the scroll = %+v", s)
	}
}

func TestTheClipboardTravelsOnlyWhenTheUserCopiesOrPastes(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	r.open(uitest.Themes()[0], r.connectState())
	view := r.connect()
	r.settle()

	// nothing of B's clipboard goes to A before a paste
	r.ts.SetClipboardText("secret of B")
	time.Sleep(60 * time.Millisecond)
	r.settle()
	if view.Clipboard() != "" {
		t.Fatalf("the clipboard of B reached A without a paste: %q", view.Clipboard())
	}
	// A's copy goes to B's clipboard
	view.Copy("copied on A")
	r.until("the copy on B", func() bool {
		text, _ := r.ts.ClipboardText()
		return text == "copied on A"
	})
	// B's paste sends the clipboard before the key
	r.ts.SetClipboardText("pasted from B")
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyKeyV, gluttertest.KeyOpts{Character: "v"})
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyKeyV)
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	r.until("the paste", func() bool {
		for _, k := range view.Keys() {
			if k.Logical == services.LogicalKeyboardKeyKeyV.KeyID() && k.Kind == extension.KeyDown {
				return true
			}
		}
		return false
	})
	if view.Clipboard() != "pasted from B" {
		t.Errorf("A's clipboard = %q", view.Clipboard())
	}
	// and the key up of the paste follows its key down
	r.until("the whole paste", func() bool { return len(view.Keys()) >= 4 })
	keys := view.Keys()
	last := keys[len(keys)-1]
	if last.Kind != extension.KeyUp {
		t.Errorf("the keys after the paste = %+v", keys)
	}
}

func TestANotificationOfTheHostShowsOnBAndItsButtonRunsOnA(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	r.open(uitest.Themes()[0], r.connectState())
	view := r.connect()
	r.settle()

	view.Notify(extension.ViewNotification{ID: 41, Severity: extension.NotificationWarning, Source: "Git", Message: "pull failed", Actions: []string{"Retry", "Open Log"}})
	// the messages of Remote Work itself stay on A
	view.Notify(extension.ViewNotification{ID: 42, Source: "Remote Work", Message: "a key logged in"})
	r.until("the notification", func() bool { return len(r.b.Notes()) == 1 })
	n := r.b.Notes()[0]
	if n.Severity != "warn" || n.Message != "127.0.0.1: Git: pull failed" || len(n.Actions) != 2 || n.Actions[1] != "Open Log" {
		t.Errorf("the notification on B = %+v", n)
	}
	r.b.RunNoteAction(0, "Open Log")
	r.until("the press on A", func() bool { return len(view.Presses()) == 1 })
	if p := view.Presses()[0]; p.ID != 41 || p.Action != 1 {
		t.Errorf("A was told to press %+v", p)
	}
	if len(r.b.Notes()) != 1 {
		t.Errorf("B shows %d notifications", len(r.b.Notes()))
	}
}
