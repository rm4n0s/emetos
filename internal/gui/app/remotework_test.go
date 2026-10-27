package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/remotework"
	"github.com/rm4n0s/emetos/extensions/remotework/transport"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/notify"
)

// Remote Work on two Emetos in one process, on the loopback (step 15.4, SPEC §10.18): A is a window with a folder, whose Remote Work
// is on; B is a window with a Remote Work tab that logs in to A with a key, shows a second view of A's workspace, and sends its
// keys and mouse. Both are real windows of the tester with the real extension.

// rwSize is the size of both windows: small, so that a picture of the view is quick to draw.
var rwSize = ui.Size{Width: 640, Height: 480}

// rwPair is the two machines.
type rwPair struct {
	t       *testing.T
	a, b    *multiApp
	hubA    *remotework.Hub
	hubB    *remotework.Hub
	folder  string
	file    string
	doc     *document.Document
	wsA     *Workspace
	keyFile string

	mu sync.Mutex
	// dropSSH closes the SSH connection of B's newest session, as a network that fails does.
	dropSSH func()
}

// newRWPair makes the two machines with Remote Work on for the workspace of A and a key file of B that A lists, and opens the tab on
// B with the form filled in.
func newRWPair(t *testing.T) *rwPair {
	t.Helper()
	p := &rwPair{t: t}
	before := liveGoroutines()
	t.Cleanup(func() { settleGoroutines(t, before) })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hubOptions := func(stateDir string) remotework.HubOptions {
		return remotework.HubOptions{
			StateDir: func() (string, error) { return stateDir, nil },
			Listen:   func(int) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") },
			ListenUDP: func(port int) (net.PacketConn, error) {
				return net.ListenPacket("udp", "127.0.0.1:"+strconv.Itoa(port))
			},
			Logger: logger,
			Tab: remotework.TabOptions{
				Backoff: func(int) time.Duration { return 50 * time.Millisecond }, ResizeDelay: 20 * time.Millisecond,
				AgentSocket: func() string { return "" },
			},
		}
	}
	optsA := hubOptions(filepath.Join(t.TempDir(), "state-a"))
	p.hubA = remotework.NewHub(optsA)
	t.Cleanup(p.hubA.Close)
	optsB := hubOptions(filepath.Join(t.TempDir(), "state-b"))
	optsB.Tab.Transport = func(o *transport.ClientOptions) {
		p.mu.Lock()
		p.dropSSH = o.CloseSSH
		p.mu.Unlock()
	}
	p.hubB = remotework.NewHub(optsB)
	t.Cleanup(p.hubB.Close)

	// A: a folder with a file, in a window whose Remote Work is on for the folder, listing the key of B
	envA := newMultiEnv(t, nil)
	p.folder = envA.folder("project", "main.go", "notes.txt")
	p.file = filepath.Join(p.folder, "main.go")
	p.a = envA.runWith(Options{Folder: p.folder}, ProcessOptions{Extensions: []extension.Factory{remotework.FactoryOf(p.hubA)}})
	p.a.ts.SetViewSize(rwSize)
	p.a.settle()
	p.wsA = p.a.m.Active()
	if err := p.a.command(p.wsA, "dev.openFile", map[string]any{"path": p.file}); err != nil {
		t.Fatal(err)
	}
	doc, ok := p.wsA.Documents.Get(document.FileURI(p.file))
	if !ok {
		t.Fatal("main.go is not open on A")
	}
	p.doc = doc
	_, priv := newRWKey(t)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(signer.PublicKey())), "\n") + " laptop B"
	if _, err := p.hubA.AddKey(line); err != nil {
		t.Fatal(err)
	}
	if err := p.hubA.SetWorkspace(p.folder, true); err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "test")
	if err != nil {
		t.Fatal(err)
	}
	p.keyFile = filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(p.keyFile, pemEncode(block), 0o600); err != nil {
		t.Fatal(err)
	}

	// B: a window of its own, with a folder of its own, and the tab
	envB := newMultiEnv(t, nil)
	folderB := envB.folder("local", "local.go")
	p.b = envB.runWith(Options{Folder: folderB}, ProcessOptions{Extensions: []extension.Factory{remotework.FactoryOf(p.hubB)}})
	p.b.ts.SetViewSize(rwSize)
	p.b.settle()
	return p
}

// both pumps both windows until the condition holds.
func (p *rwPair) both(what string, cond func() bool) {
	p.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			p.t.Fatalf("timed out waiting for %s", what)
		}
		p.a.ts.Pump(10 * time.Millisecond)
		p.b.ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
}

// settle pumps both windows for a moment.
func (p *rwPair) settle() {
	for range 6 {
		p.a.ts.Pump(10 * time.Millisecond)
		p.b.ts.Pump(10 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
}

// openTab opens the Remote Work tab on B, pointing at A.
func (p *rwPair) openTab() {
	p.t.Helper()
	addr := p.hubA.Addr()
	if addr == nil {
		p.t.Fatal("A does not listen")
	}
	_, port, _ := net.SplitHostPort(addr.String())
	n, _ := strconv.Atoi(port)
	state, err := json.Marshal(map[string]any{"host": "127.0.0.1", "port": n, "key": "file", "keyFile": p.keyFile})
	if err != nil {
		p.t.Fatal(err)
	}
	if _, err := p.b.m.OpenExtensionTab(remotework.TabKindID, state); err != nil {
		p.t.Fatal(err)
	}
	p.settle()
}

// connected is whether the tab on B is titled for the workspace of A.
func (p *rwPair) connected() bool {
	for _, n := range p.b.tabNames() {
		if n == "127.0.0.1: project" {
			return true
		}
	}
	return false
}

// picture is the pixels of the picture the tab on B shows, nil while there is none.
func (p *rwPair) picture() []byte {
	if !p.b.ts.Exists(gluttertest.ByType[*widgets.RawImage]()) {
		return nil
	}
	img := p.b.ts.Widget(gluttertest.ByType[*widgets.RawImage]()).(*widgets.RawImage).Image()
	if img == nil {
		return nil
	}
	return append([]byte(nil), img.Pixels()...)
}

// connect presses Connect on B, trusts the host key of A and waits for the picture.
func (p *rwPair) connect() {
	p.t.Helper()
	p.b.ts.Tap(gluttertest.ByText("Connect"))
	p.both("the first-use question", func() bool { return p.b.text("Trust") })
	p.b.ts.Tap(gluttertest.ByText("Trust"))
	p.both("the connection", func() bool { return p.connected() && p.picture() != nil })
}

// differs waits until the picture on B is not the one given.
func (p *rwPair) changesFrom(what string, was []byte) {
	p.t.Helper()
	p.both(what, func() bool {
		now := p.picture()
		return now != nil && string(now) != string(was)
	})
}

func (p *rwPair) type_(text string) {
	keys := map[rune]services.LogicalKeyboardKey{
		'h': services.LogicalKeyboardKeyKeyH, 'e': services.LogicalKeyboardKeyKeyE, 'l': services.LogicalKeyboardKeyKeyL,
		'o': services.LogicalKeyboardKey(services.LogicalKeyboardKeyKeyO), 'x': services.LogicalKeyboardKeyKeyX,
	}
	for _, r := range text {
		k := keys[r]
		p.b.ts.SendKeyDownEvent(k, gluttertest.KeyOpts{Character: string(r)})
		p.b.ts.SendKeyUpEvent(k)
		p.settle()
	}
}

// chord presses a key with Ctrl on B.
func (p *rwPair) chord(k services.LogicalKeyboardKey) {
	p.b.ts.SendKeyDownEvent(services.LogicalKeyboardKeyControl)
	p.b.ts.SendKeyDownEvent(k)
	p.b.ts.SendKeyUpEvent(k)
	p.b.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	p.settle()
}

func TestThePlusMenuOfBHasRemoteWorkAndChoosingItOpensTheDialog(t *testing.T) {
	p := newRWPair(t)
	w := p.b.m.Active()
	var labels []string
	for _, it := range w.shell.titleBarOptions(w).NewItems() {
		labels = append(labels, it.Label)
	}
	found := false
	for _, l := range labels {
		found = found || l == "Remote Work..."
	}
	if !found {
		t.Fatalf("the entries of the menu = %q", labels)
	}
	if !w.shell.newMenu.Open() {
		t.Fatal("the menu cannot be opened")
	}
	p.settle()
	p.b.ts.Tap(gluttertest.ByText("Remote Work..."))
	p.settle()
	p.settle()
	if p.b.m.Count() != 2 || p.b.m.ActiveTab().KindID() != remotework.TabKindID {
		t.Fatalf("%d tabs; the active one is %q", p.b.m.Count(), p.b.m.ActiveTab().KindID())
	}
	if !p.b.text("Connect") || !p.b.ts.Exists(gluttertest.ByTextContaining("Use a workspace of another computer")) {
		t.Error("the connect dialog is not on screen")
	}
	if got := p.b.tabNames(); got[1] != "Remote Work" {
		t.Errorf("the strip = %q", got)
	}
}

func TestBOpensTheWorkspaceOfAInATabAndTypesIntoItAndAEditsAppearOnB(t *testing.T) {
	p := newRWPair(t)
	p.openTab()
	if !p.b.text("Connect") || !p.b.text("Remote Work") {
		t.Fatal("the connect dialog is not on screen")
	}
	p.connect()
	if got := p.b.tabNames(); len(got) != 2 || got[0] != "local" || got[1] != "127.0.0.1: project" {
		t.Errorf("the strip of B = %q", got)
	}
	// A shows the banner with its Disconnect button, and a view of its own for B
	if !p.a.text("Remote Work: laptop B connected") {
		t.Error("A does not show the banner")
	}
	if n := len(p.wsA.views); n != 1 {
		t.Errorf("A has %d views", n)
	}
	// the view is as large as the tab: the window without its title bar
	size := p.wsA.views[0].Size()
	if size.Width != int(rwSize.Width) || size.Height >= int(rwSize.Height) || size.Height < int(rwSize.Height)-100 {
		t.Errorf("the view of A is %v for a window of %v", size, rwSize)
	}

	// B clicks into the editor of the picture and types: the file of A changes
	was := p.picture()
	p.b.ts.TapAt(ui.Offset{Dx: 350, Dy: 200})
	p.settle()
	p.type_("hello")
	p.both("the typed text on A", func() bool { return strings.Contains(p.doc.Text(), "hello") })
	p.changesFrom("the typed text on B's picture", was)

	// A edits: it appears in the picture on B
	was = p.picture()
	if _, err := p.doc.Edit([]document.Edit{{Range: buffer.Range{}, Text: "// edited on A\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	p.changesFrom("A's edit on B's picture", was)

	// B copies: the copy is on the clipboard of B
	p.chord(services.LogicalKeyboardKeyKeyA)
	p.b.ts.ClearClipboard()
	p.chord(services.LogicalKeyboardKeyKeyC)
	p.both("the copy on B's clipboard", func() bool {
		text, ok := p.b.ts.ClipboardText()
		return ok && text == p.doc.Text()
	})
	// B pastes what is on its clipboard: A's file takes it
	p.b.ts.SetClipboardText("pasted from B\n")
	p.chord(services.LogicalKeyboardKeyKeyV)
	p.both("the paste on A", func() bool { return p.doc.Text() == "pasted from B\n" })
}

func TestOnlyTheShortcutsOfBsWindowStayOnBAndEveryOtherKeyGoesToA(t *testing.T) {
	p := newRWPair(t)
	p.openTab()
	p.connect()
	p.b.ts.TapAt(ui.Offset{Dx: 350, Dy: 200}) // the picture has the keyboard
	p.settle()
	wsB := p.b.m.ActiveTab().Workspace()
	view := p.wsA.views[0]
	// Ctrl+Shift+P is a shortcut of the editor: it opens the palette in the view of A, not on B
	for _, k := range []services.LogicalKeyboardKey{services.LogicalKeyboardKeyControl, services.LogicalKeyboardKeyShift} {
		p.b.ts.SendKeyDownEvent(k)
	}
	p.b.ts.SendKeyDownEvent(services.LogicalKeyboardKeyKeyP, gluttertest.KeyOpts{Character: "P"})
	p.b.ts.SendKeyUpEvent(services.LogicalKeyboardKeyKeyP)
	p.b.ts.SendKeyUpEvent(services.LogicalKeyboardKeyShift)
	p.b.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	p.both("the palette on A", func() bool { return view.ws.QuickInput.Visible() })
	if wsB.QuickInput.Visible() || p.b.m.Active().QuickInput.Visible() {
		t.Error("the palette opened on B")
	}
	// Escape closes it; the palette takes the keyboard a few frames after it opens, so the key is sent again until it does
	p.both("the palette to close", func() bool {
		if !view.ws.QuickInput.Visible() {
			return true
		}
		p.b.ts.SendKeyEvent(services.LogicalKeyboardKeyEscape)
		p.settle()
		return false
	})
	// Ctrl+Alt+PageUp switches the tabs of B's window, and nothing of it reaches A
	keys := len(p.wsA.views)
	for _, k := range []services.LogicalKeyboardKey{services.LogicalKeyboardKeyControl, services.LogicalKeyboardKeyAlt} {
		p.b.ts.SendKeyDownEvent(k)
	}
	p.b.ts.SendKeyDownEvent(services.LogicalKeyboardKeyPageUp)
	p.b.ts.SendKeyUpEvent(services.LogicalKeyboardKeyPageUp)
	p.b.ts.SendKeyUpEvent(services.LogicalKeyboardKeyAlt)
	p.b.ts.SendKeyUpEvent(services.LogicalKeyboardKeyControl)
	p.settle()
	if got := p.b.m.IndexOf(p.b.m.ActiveTab()); got != 0 {
		t.Errorf("the active tab of B is %d, want the first", got)
	}
	if len(p.wsA.views) != keys {
		t.Error("the view of A changed")
	}
	// back on the tab, the keys are A's again
	p.b.m.ActivateIndex(1)
	p.settle()
	if p.b.m.IndexOf(p.b.m.ActiveTab()) != 1 {
		t.Fatal("the remote tab is not active")
	}
}

func TestANotificationOfAShowsOnBAndItsButtonRunsOnA(t *testing.T) {
	p := newRWPair(t)
	p.openTab()
	p.connect()
	ran := make(chan struct{}, 1)
	p.wsA.Notify.Warn("Git", "pull failed", notify.Action{Label: "Retry", Run: func() { ran <- struct{}{} }})
	p.both("the notification on B", func() bool { return p.b.text("Retry") })
	// A's own view draws no toast of it: B's does
	p.b.ts.Tap(gluttertest.ByText("Retry"))
	select {
	case <-ran:
	case <-time.After(0):
		p.both("the action on A", func() bool {
			select {
			case <-ran:
				return true
			default:
				return false
			}
		})
	}
	if _, ok := p.wsA.Notify.Get(1); ok && len(p.wsA.Notify.Items()) > 1 {
		t.Errorf("A still has %d notifications", len(p.wsA.Notify.Items()))
	}
}

func TestDisconnectOnAEndsBsTabWithItsReasonAndBConnectsAgain(t *testing.T) {
	p := newRWPair(t)
	p.openTab()
	p.connect()
	// the user of A presses Disconnect on its banner
	p.a.ts.Tap(gluttertest.ByText("Disconnect"))
	p.both("the reason on B", func() bool { return p.b.ts.Exists(gluttertest.ByTextContaining("Disconnected by 127.0.0.1")) })
	if p.b.text("Reconnecting...") {
		t.Error("B reconnects after the user of A disconnected it")
	}
	p.both("A to let go", func() bool { return len(p.wsA.views) == 0 })
	// the user of B connects again
	p.b.ts.Tap(gluttertest.ByText("Connect Again"))
	p.both("the connection again", func() bool {
		return p.picture() != nil && len(p.wsA.views) == 1 && !p.b.ts.Exists(gluttertest.ByTextContaining("Disconnected by 127.0.0.1"))
	})
}

func TestBConnectsAgainAfterTheConnectionDrops(t *testing.T) {
	p := newRWPair(t)
	p.openTab()
	p.connect()
	first := p.wsA.views[0]
	p.mu.Lock()
	drop := p.dropSSH
	p.mu.Unlock()
	drop()
	p.both("the new session", func() bool {
		return len(p.wsA.views) == 1 && p.wsA.views[0] != first && p.picture() != nil && !p.b.text("Reconnecting...")
	})
	select {
	case <-first.Done():
	default:
		t.Error("the view of the dropped session was not closed on A")
	}
	if got := p.b.tabNames(); got[len(got)-1] != "127.0.0.1: project" {
		t.Errorf("the strip of B = %q", got)
	}
}

func TestClosingTheTabOnBEndsTheSessionAndTheViewOnA(t *testing.T) {
	p := newRWPair(t)
	p.openTab()
	p.connect()
	tab := p.b.m.ActiveTab()
	p.b.m.Close(tab, nil)
	p.both("A to let go", func() bool { return len(p.wsA.views) == 0 && !p.a.text("Remote Work: laptop B connected") })
	if p.b.m.Count() != 1 {
		t.Errorf("B has %d tabs", p.b.m.Count())
	}
}

// liveGoroutines counts the goroutines of the process, not those that live as long as it does (the clock of regexp2, which the
// first regular expression starts).
func liveGoroutines() int {
	buf := make([]byte, 4<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if g != "" && !strings.Contains(g, "regexp2.runClock") {
			n++
		}
	}
	return n
}

// settleGoroutines fails the test when it leaves goroutines behind (the ones of its windows and sessions end with it).
func settleGoroutines(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for liveGoroutines() > before {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			t.Errorf("goroutines left: %d before, %d now\n%s", before, liveGoroutines(), buf[:n])
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// newRWKey makes a key for B.
func newRWKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func pemEncode(b *pem.Block) []byte { return pem.EncodeToMemory(b) }
