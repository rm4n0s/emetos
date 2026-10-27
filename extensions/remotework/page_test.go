package remotework

import (
	"context"
	"crypto/ed25519"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
)

// seededKey is a key that is the same in every run, for goldens.
func seededKey(t testing.TB, seed byte) testKey {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(append(make([]byte, 31), seed))
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return testKey{priv: priv, signer: signer}
}

// pageRig is the extension activated in a workspace, with its page open on a fake API.
type pageRig struct {
	*rig
	api    *fakeapi.API
	ext    *remoteWork
	folder string
	tab    *fakeapi.Tab
}

// newPageRig activates the extension over the fake API of a workspace and runs Manage. With seeded the host key and the two keys
// listed are the same in every run.
func newPageRig(t *testing.T, seeded bool) *pageRig { return newPageRigWith(t, seeded, nil) }

// newPageRigWith is newPageRig with the cameras of the hub: the Source that Scan QR Code reads, or none when it is nil. No test reads
// a real camera.
func newPageRigWith(t *testing.T, seeded bool, cameras keyqr.Source, tweak ...func(*HubOptions)) *pageRig {
	t.Helper()
	r := newRig(t, rigOptions{hubTweak: func(h *HubOptions) {
		h.Camera = cameras
		for _, f := range tweak {
			f(h)
		}
	}})
	if seeded {
		block, err := ssh.MarshalPrivateKey(seededKey(t, 1).priv, "host")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(r.dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.dir, hostKeyFileName), pem.EncodeToMemory(block), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// the folder is a name and nothing is read from it; the same in every run, for the goldens
	const folder = "/home/user/project"
	e := &remoteWork{hub: r.hub}
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: folder, ID: folder})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	if err := api.Run(ManageCommand, nil); err != nil {
		t.Fatal(err)
	}
	tabs := api.EditorTabs()
	if len(tabs) != 1 {
		t.Fatalf("Manage made %d tabs", len(tabs))
	}
	return &pageRig{rig: r, api: api, ext: e, folder: folder, tab: tabs[0]}
}

// shownPage is the page on screen.
type shownPage struct {
	t       *testing.T
	p       *pageRig
	ts      *gluttertest.Tester
	rebuild func()
	built   int
}

func (p *pageRig) show(t *testing.T, th uitest.Theme, size gui.Size) *shownPage {
	t.Helper()
	s := &shownPage{t: t, p: p}
	host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		s.rebuild = func() { set(func() {}) }
		return p.tab.Content.Build(bc)
	})
	s.ts = uitest.Pump(t, th, size, host)
	s.built = p.tab.Refreshs
	return s
}

// settle runs what the extension posted and builds the page again when it asked for it, until nothing is left.
func (s *shownPage) settle() {
	s.t.Helper()
	for i := 0; i < 50; i++ {
		ran := s.p.api.Pump()
		if s.p.tab.Refreshs != s.built {
			s.built = s.p.tab.Refreshs
			s.rebuild()
			ran = true
		}
		s.ts.Pump()
		if !ran {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// until settles until the condition holds.
func (s *shownPage) until(what string, cond func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.settle()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (s *shownPage) tap(text string) {
	s.t.Helper()
	s.ts.Tap(gluttertest.ByText(text))
	s.settle()
}

func (s *shownPage) has(text string) bool { return s.ts.Exists(gluttertest.ByTextContaining(text)) }

// box is the input box with the placeholder or the index.
func (s *shownPage) enter(index int, text string) {
	s.t.Helper()
	s.ts.EnterText(gluttertest.At(gluttertest.ByType[*ui.InputBox](), index), text)
	s.ts.Pump()
}

var pageSize = gui.Size{Width: 760, Height: 960}

func TestGoldenThePage(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run("off "+th.Name, func(t *testing.T) {
			p := newPageRig(t, true)
			for _, k := range []struct {
				seed    byte
				comment string
			}{{2, "laptop B (work)"}, {3, "tablet"}} {
				if _, err := p.hub.AddKey(seededKey(t, k.seed).line(k.comment)); err != nil {
					t.Fatal(err)
				}
			}
			s := p.show(t, th, pageSize)
			s.settle()
			uitest.MatchesGolden(s.ts, "page_off_"+th.Name)
		})
		t.Run("on but the port is taken "+th.Name, func(t *testing.T) {
			p := newPageRig(t, true)
			p.failTo = errors.New("FakeListenFailed", "address already in use")
			if _, err := p.hub.AddKey(seededKey(t, 2).line("laptop B (work)")); err != nil {
				t.Fatal(err)
			}
			if err := p.hub.SetWorkspace(p.folder, true); err != nil {
				t.Fatal(err)
			}
			s := p.show(t, th, pageSize)
			s.settle()
			uitest.MatchesGolden(s.ts, "page_on_error_"+th.Name)
		})
	}
}

func TestThePageShowsTheHostFingerprint(t *testing.T) {
	p := newPageRig(t, true)
	s := p.show(t, uitest.Themes()[0], pageSize)
	s.settle()
	want := seededKey(t, 1).fingerprint()
	if !s.has(want) {
		t.Errorf("the page does not show the host fingerprint %s", want)
	}
	// the fingerprint is the one a client is shown on first use
	if got := p.hub.Snapshot(p.folder).HostFingerprint; got != want {
		t.Errorf("Snapshot fingerprint = %s", got)
	}
	for _, note := range securityNotes {
		if !s.has(note[:40]) {
			t.Errorf("the security note %q is not on the page", note[:40])
		}
	}
}

func TestThePageMakesTheHostKeyWhenItOpens(t *testing.T) {
	p := newPageRig(t, false)
	info, err := os.Stat(filepath.Join(p.dir, hostKeyFileName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("host key: %v, %v", info, err)
	}
	s := p.show(t, uitest.Themes()[0], pageSize)
	s.settle()
	if !s.has("SHA256:") {
		t.Error("the fingerprint is not shown")
	}
}

func TestTheSwitchTurnsRemoteWorkOnAndOff(t *testing.T) {
	p := newPageRig(t, true)
	s := p.show(t, uitest.Themes()[0], pageSize)
	s.settle()
	if !s.has("Off. Nothing listens") {
		t.Error("the page does not say it is off")
	}
	s.tap("Allow other computers to use project")
	s.until("it to be on", func() bool { return p.hub.Snapshot(p.folder).On && s.has("Listening on port") })
	if p.hub.Addr() == nil {
		t.Error("nothing listens")
	}
	if !s.has("Listening on port " + portOf(p.hub.Addr().String(), 0)[5:]) {
		t.Error("the page does not show the port")
	}
	s.tap("Allow other computers to use project")
	s.until("it to be off", func() bool { return !p.hub.Snapshot(p.folder).On && s.has("Off. Nothing listens") })
	if p.hub.Addr() != nil {
		t.Error("it still listens")
	}
}

func TestAddingAndRemovingKeys(t *testing.T) {
	p := newPageRig(t, true)
	s := p.show(t, uitest.Themes()[0], pageSize)
	s.settle()
	if !s.has("No key is listed") {
		t.Error("the empty list is not explained")
	}
	line := seededKey(t, 2).line("laptop B (work)")
	// the key boxes are, in order: the port, the idle timeout and the line of the key
	s.enter(2, line)
	s.tap("Add")
	if !s.has("laptop B (work)") || !s.has("Added laptop B (work).") {
		t.Fatal("the key is not on the page")
	}
	if got := p.hub.Snapshot(p.folder).Keys; len(got) != 1 || got[0].Comment != "laptop B (work)" {
		t.Errorf("keys = %+v", got)
	}
	// a key twice, a line that is not a key, a line with options
	for _, tc := range []struct{ line, want string }{
		{line, "in the list already"},
		{"hello world", "not a public key"},
		{`command="x" ` + line, "options"},
	} {
		s.enter(2, tc.line)
		s.tap("Add")
		if !s.has(tc.want) {
			t.Errorf("no answer containing %q for %q", tc.want, tc.line)
		}
	}
	s.tap("Remove")
	if !s.has("Removed laptop B (work).") || len(p.hub.Snapshot(p.folder).Keys) != 0 {
		t.Error("the key was not removed")
	}
	// the file of the keys holds nothing but the lines
	data, _ := os.ReadFile(filepath.Join(p.dir, configFileName))
	if strings.Contains(string(data), "laptop B") {
		t.Error("a removed key is still in the file")
	}
}

func TestChoosingAPublicKeyFile(t *testing.T) {
	p := newPageRig(t, true)
	s := p.show(t, uitest.Themes()[0], pageSize)
	s.settle()
	pub := filepath.Join(t.TempDir(), "id_ed25519.pub")
	if err := os.WriteFile(pub, []byte(seededKey(t, 4).line("from a file")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.api.AnswerFileChooser(pub, nil)
	s.tap("Choose .pub File...")
	s.until("the key to be added", func() bool { return len(p.hub.Snapshot(p.folder).Keys) == 1 })
	calls := p.api.FileChooserCalls()
	if len(calls) != 1 || calls[0].Options.Filters[0].Patterns[0] != "*.pub" {
		t.Errorf("chooser calls = %+v", calls)
	}
	// a dialog that is closed adds nothing; a file that is not a key says so; a dialog that fails says so
	p.api.AnswerFileChooser("", nil)
	s.tap("Choose .pub File...")
	junk := filepath.Join(t.TempDir(), "junk.pub")
	if err := os.WriteFile(junk, []byte("not a key at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.api.AnswerFileChooser(junk, nil)
	s.tap("Choose .pub File...")
	s.until("the refusal", func() bool { return s.has("not a public key") })
	p.api.AnswerFileChooser("", errors.New("FakePortal", "no portal"))
	s.tap("Choose .pub File...")
	s.until("the error of the dialog", func() bool { return s.has("no portal") })
	if len(p.hub.Snapshot(p.folder).Keys) != 1 {
		t.Error("something else was added")
	}
}

func TestTheConnectionSettings(t *testing.T) {
	p := newPageRig(t, true)
	s := p.show(t, uitest.Themes()[0], pageSize)
	s.settle()
	for _, tc := range []struct{ port, idle, want string }{
		{"abc", "30", "The port is a number"},
		{"0", "30", "The port is a number"},
		{"70000", "30", "The port is a number"},
		{"47123", "x", "number of minutes"},
		{"47123", "0", "from 1 minute to 24 hours"},
		{"47123", "5000", "from 1 minute to 24 hours"},
	} {
		s.enter(0, tc.port)
		s.enter(1, tc.idle)
		s.tap("Apply")
		if !s.has(tc.want) {
			t.Errorf("port %q idle %q: no answer containing %q", tc.port, tc.idle, tc.want)
		}
	}
	s.enter(0, "50123")
	s.enter(1, "45")
	s.tap("Apply")
	s.until("it to be saved", func() bool { return s.has("Saved.") })
	if c := p.hub.Snapshot(p.folder).Config; c.Port != 50123 || c.IdleMinutes != 45 {
		t.Errorf("config = %+v", c)
	}
}

func TestDisconnectOnThePage(t *testing.T) {
	p := newPageRig(t, true)
	b := newKey(t)
	p.on(p.folder, b)
	s := p.show(t, uitest.Themes()[0], pageSize)
	conn, err := p.client(b).connect(p.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	sess := p.nextSession()
	s.until("the session on the page", func() bool { return s.has("laptop B connected from 127.0.0.1 since 12:00") })
	s.tap("Disconnect")
	<-sess.Done()
	s.until("the session to leave the page", func() bool { return !s.has("connected from") })
}

func TestAWorkspaceWithoutAFolderCannotBeShared(t *testing.T) {
	r := newRig(t)
	e := &remoteWork{hub: r.hub}
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "No Folder"})
	if err := e.Activate(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	if err := api.Run(ManageCommand, nil); err != nil {
		t.Fatal(err)
	}
	pr := &pageRig{rig: r, api: api, ext: e, tab: api.EditorTabs()[0]}
	s := pr.show(t, uitest.Themes()[0], pageSize)
	s.settle()
	if !s.has("no folder") || s.ts.Exists(gluttertest.ByType[*ui.Checkbox]()) {
		t.Error("the page offers the switch to a workspace without a folder")
	}
}

func TestAFileThatCannotBeUsedLocksThePage(t *testing.T) {
	r := newRig(t)
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// a file that others can write is not trusted: it decides who may log in
	if err := os.WriteFile(filepath.Join(r.dir, configFileName), []byte(`{"version":1}`), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(r.dir, configFileName), 0o666); err != nil {
		t.Fatal(err)
	}
	e := &remoteWork{hub: r.hub}
	folder := t.TempDir()
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: folder, ID: folder})
	if err := e.Activate(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	if err := api.Run(ManageCommand, nil); err != nil {
		t.Fatal(err)
	}
	pr := &pageRig{rig: r, api: api, ext: e, folder: folder, tab: api.EditorTabs()[0]}
	s := pr.show(t, uitest.Themes()[0], pageSize)
	s.settle()
	if !s.has("remote-work.json cannot be used") || !s.has("can be changed or read by other users") {
		t.Error("the page does not explain the file")
	}
	_, err := r.hub.AddKey(newKey(t).line("x"))
	wantRoute(t, err, "Hub.mutate->Hub.unusable.RemoteWorkConfigUnusable")
	wantRoute(t, r.hub.SetWorkspace(folder, true), "Hub.EnsureHostKey->Hub.unusable.RemoteWorkConfigUnusable")
	if r.hub.Addr() != nil {
		t.Error("it listens although its file is not trusted")
	}
}

func TestTheManageCommandShowsTheOpenPage(t *testing.T) {
	p := newPageRig(t, true)
	if err := p.api.Run(ManageCommand, nil); err != nil {
		t.Fatal(err)
	}
	if len(p.api.EditorTabs()) != 1 || !p.tab.Revealed {
		t.Errorf("tabs = %d, revealed = %v", len(p.api.EditorTabs()), p.tab.Revealed)
	}
}

func TestManifest(t *testing.T) {
	m := (&remoteWork{}).Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "emetos.remotework" || m.Name != "Remote Work" || len(m.Contributes.Commands) != 1 || m.Contributes.Commands[0].ID != "emetos.remotework.manage" {
		t.Errorf("manifest = %+v", m)
	}
	f := NewFactory(HubOptions{})
	a, b := f(), f()
	if a == b || a.(*remoteWork).hub != b.(*remoteWork).hub {
		t.Error("the instances must be two and share one hub")
	}
}

func TestReadPublicKeyFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.pub")
	if err := os.WriteFile(good, []byte("ssh-ed25519 AAAA x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.pub")
	if err := os.WriteFile(big, make([]byte, maxPublicKeyFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readPublicKeyFile(good); err != nil || got != "ssh-ed25519 AAAA x\n" {
		t.Errorf("readPublicKeyFile = %q, %v", got, err)
	}
	_, err := readPublicKeyFile(filepath.Join(dir, "none"))
	wantRoute(t, err, "readPublicKeyFile.RemoteWorkKeyFileUnreadable")
	_, err = readPublicKeyFile(big)
	wantRoute(t, err, "readPublicKeyFile.RemoteWorkKeyFileInvalid")
	_, err = readPublicKeyFile(dir)
	wantRoute(t, err, "readPublicKeyFile.RemoteWorkKeyFileInvalid")
}
