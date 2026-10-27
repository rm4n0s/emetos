package remotework

import (
	"encoding/json"
	"image"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"golang.org/x/crypto/ssh/agent"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// fakeAgent serves an ssh-agent with the keys on a socket of the test and returns the path of the socket.
func fakeAgent(t *testing.T, keys ...testKey) string {
	t.Helper()
	keyring := agent.NewKeyring()
	for _, k := range keys {
		if err := keyring.Add(agent.AddedKey{PrivateKey: k.priv, Comment: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := os.MkdirTemp("", "agent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				_ = agent.ServeAgent(keyring, c)
			}()
		}
	}()
	return path
}

func TestTheConnectDialogTakesTheHostThePortAndTheKeyFileAndConnects(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	st := r.connectState()
	r.open(uitest.Themes()[0], tabState{Port: DefaultPort, Key: keyFile})
	r.settle()
	if !r.has("Remote Work") || !r.has("Host") || !r.has("Port") || !r.has("Private key file") {
		t.Fatal("the dialog lacks its fields")
	}
	// Connect with nothing typed says what is missing
	r.ts.Tap(gluttertest.ByText("Connect"))
	r.settle()
	if !r.has("name or the address") || r.tr.phase != phaseForm {
		t.Errorf("phase %d; the dialog does not say the host is missing", r.tr.phase)
	}
	// a key file that is not there is told by the login
	r.ts.EnterText(gluttertest.At(gluttertest.ByType[*ui.InputBox](), 0), st.Host)
	r.ts.EnterText(gluttertest.At(gluttertest.ByType[*ui.InputBox](), 1), strconv.Itoa(st.Port))
	r.ts.EnterText(gluttertest.At(gluttertest.ByType[*ui.InputBox](), 2), filepath.Join(t.TempDir(), "missing"))
	r.ts.Tap(gluttertest.ByText("Connect"))
	r.until("the failure", func() bool { return r.tr.phase == phaseEnded })
	if r.tr.ended.title != "The key file cannot be used" {
		t.Errorf("the card says %q", r.tr.ended.title)
	}
	// back to the form with what was typed, and the real file
	r.ts.Tap(gluttertest.ByText("Edit Connection"))
	r.settle()
	r.ts.EnterText(gluttertest.At(gluttertest.ByType[*ui.InputBox](), 2), r.file)
	r.ts.Tap(gluttertest.ByText("Connect"))
	r.until("the connection", func() bool { return r.tr.phase == phaseConnected })
	if got := r.wt.Title(); got != "127.0.0.1: project" {
		t.Errorf("the title = %q", got)
	}
	if r.wt.Changes == 0 {
		t.Error("the window was not told that the state of the tab changed")
	}
}

func TestTheKeyFileCanBeChosenWithTheDialogOfTheDesktop(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	r.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyFile})
	r.settle()
	r.b.AnswerFileChooser(r.file, nil)
	r.ts.Tap(gluttertest.ByText("Choose..."))
	r.until("the key file", func() bool { return r.tr.form.keyFile.Value() == r.file })
	if calls := r.b.FileChooserCalls(); len(calls) != 1 || calls[0].Options.Title == "" {
		t.Errorf("chooser calls = %+v", calls)
	}
}

func TestTheAgentCanBeChosenAndItsKeysAreListed(t *testing.T) {
	noLeaks(t)
	other := newKey(t)
	r := newTabRig(t, true)
	socket := fakeAgent(t, other, r.key)
	r.hub.opts.Tab.AgentSocket = func() string { return socket }
	r.tr = nil
	st := r.connectState()
	r.open(uitest.Themes()[0], tabState{Host: st.Host, Port: st.Port, Key: keyAgent})
	r.until("the keys of the agent", func() bool { return !r.tr.form.agentLoading && len(r.tr.form.agent) == 2 })
	if r.tr.form.agent[0].Fingerprint != other.fingerprint() || r.tr.form.agent[1].Fingerprint != r.key.fingerprint() {
		t.Errorf("the agent's keys = %+v", r.tr.form.agent)
	}
	if !r.has("Any key of the agent") {
		t.Error("the choice of the agent's key is not on screen")
	}
	// pick the listed key; the choice is the state the tab saves
	r.tr.form.fingerprint = r.key.fingerprint()
	r.tr.stateChanged()
	r.tr.connect()
	r.until("the connection through the agent", func() bool { return r.tr.phase == phaseConnected })
	var saved tabState
	if err := json.Unmarshal(r.wt.State(), &saved); err != nil || saved.Key != keyAgent || saved.Fingerprint != r.key.fingerprint() || saved.KeyFile != "" {
		t.Errorf("the saved state = %s (%v)", r.wt.State(), err)
	}
	// with no agent behind the socket the dialog says so
	r2 := newTabRig(t, true)
	r2.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyAgent})
	r2.until("the note", func() bool { return r2.tr.form.agentNote != "" })
	if !r2.has("no ssh-agent") && !r2.has("There is no ssh-agent") {
		t.Errorf("the dialog does not say there is no agent (note %q)", r2.tr.form.agentNote)
	}
}

func TestADevCommandFillsTheDialogAndConnects(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	r.open(uitest.Themes()[0], nil)
	st := r.connectState()
	wantRoute(t, r.b.Run(DevConnectCommand, map[string]any{"host": ""}), "remoteWork.devConnect.RemoteWorkDevConnectInvalid")
	wantRoute(t, r.b.Run(DevConnectCommand, map[string]any{"host": "h"}), "remoteWork.devConnect.RemoteWorkDevConnectInvalid")
	if err := r.b.Run(DevConnectCommand, map[string]any{"host": st.Host, "port": float64(st.Port), "keyFile": r.file}); err != nil {
		t.Fatal(err)
	}
	r.until("the connection", func() bool { return r.tr.phase == phaseConnected })
}

// ---- pictures of the tab ----

// loadPicture reads a picture of a view of the host from the testdata of the tiles.
func loadPicture(t testing.TB, name string) *gui.Image {
	t.Helper()
	f, err := os.Open(filepath.Join("tiles", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	src, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	rgba := image.NewRGBA(src.Bounds())
	for y := 0; y < src.Bounds().Dy(); y++ {
		for x := 0; x < src.Bounds().Dx(); x++ {
			rgba.Set(x, y, src.At(x, y))
		}
	}
	img, err := gui.DecodeImageFromPixels(t.Context(), rgba.Pix, rgba.Rect.Dx(), rgba.Rect.Dy(), gui.PixelFormatRGBA8888, gui.DecodeImageFromPixelsOpts{RowBytes: rgba.Stride})
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestGoldensOfTheTab(t *testing.T) {
	for _, th := range uitest.Themes() {
		open := func(t *testing.T) *tabRig {
			r := newTabRig(t, true)
			r.open(th, tabState{Host: "laptop.local", Port: DefaultPort, Key: keyFile, KeyFile: "/home/user/.ssh/id_ed25519"})
			r.settle()
			return r
		}
		t.Run("the connect dialog "+th.Name, func(t *testing.T) {
			r := open(t)
			uitest.MatchesGolden(r.ts, "tab_form_"+th.Name)
		})
		t.Run("the connect dialog with the agent "+th.Name, func(t *testing.T) {
			r := newTabRig(t, true)
			socket := fakeAgent(t, seededKey(t, 5))
			r.hub.opts.Tab.AgentSocket = func() string { return socket }
			r.open(th, tabState{Host: "laptop.local", Port: DefaultPort, Key: keyAgent})
			r.until("the keys", func() bool { return !r.tr.form.agentLoading && len(r.tr.form.agent) == 1 })
			uitest.MatchesGolden(r.ts, "tab_form_agent_"+th.Name)
		})
		t.Run("connecting "+th.Name, func(t *testing.T) {
			r := open(t)
			r.tr.phase = phaseConnecting
			r.wt.Refresh()
			r.settle()
			uitest.MatchesGolden(r.ts, "tab_connecting_"+th.Name)
		})
		t.Run("connected "+th.Name, func(t *testing.T) {
			r := open(t)
			r.tr.phase = phaseConnected
			r.tr.picture.SetValue(loadPicture(t, "typed.png"))
			r.wt.Refresh()
			r.settle()
			uitest.MatchesGolden(r.ts, "tab_connected_"+th.Name)
		})
		t.Run("reconnecting "+th.Name, func(t *testing.T) {
			r := open(t)
			r.tr.picture.SetValue(loadPicture(t, "typed.png"))
			r.tr.phase, r.tr.attempt, r.tr.retryIn = phaseReconnecting, 3, 4e9
			r.wt.Refresh()
			r.settle()
			uitest.MatchesGolden(r.ts, "tab_reconnecting_"+th.Name)
		})
		t.Run("a disconnect reason "+th.Name, func(t *testing.T) {
			r := open(t)
			r.tr.picture.SetValue(loadPicture(t, "typed.png"))
			r.tr.phase, r.tr.ended = phaseEnded, describeDisconnect("laptop.local", &protocol.Disconnect{Reason: protocol.ReasonHost, Text: "the user of the host pressed Disconnect"})
			r.wt.Refresh()
			r.settle()
			uitest.MatchesGolden(r.ts, "tab_ended_host_"+th.Name)
		})
		t.Run("a failure "+th.Name, func(t *testing.T) {
			r := open(t)
			r.tr.phase, r.tr.ended = phaseEnded, describeError("laptop.local:47123", errors.New("SSHLoginAuthFailed", "ssh: unable to authenticate"))
			r.wt.Refresh()
			r.settle()
			uitest.MatchesGolden(r.ts, "tab_ended_refused_"+th.Name)
		})
		t.Run("the choice of a workspace "+th.Name, func(t *testing.T) {
			r := open(t)
			r.tr.phase, r.tr.picker = phasePicker, []string{"api", "emetos", "notes"}
			r.wt.Refresh()
			r.settle()
			uitest.MatchesGolden(r.ts, "tab_picker_"+th.Name)
		})
	}
}
