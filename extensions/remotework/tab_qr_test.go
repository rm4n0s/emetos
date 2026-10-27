package remotework

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
)

const showButton = "Show My Public Key as QR Code"

// shownCode reads the QR code on screen back: the line of the key the dialog draws.
func (r *tabRig) shownCode() string {
	r.t.Helper()
	pic := r.ts.Widget(gluttertest.ByType[*widgets.RawImage]()).(*widgets.RawImage).Image()
	rgba := &image.RGBA{Pix: pic.Pixels(), Stride: 4 * pic.Width(), Rect: image.Rect(0, 0, pic.Width(), pic.Height())}
	text, err := keyqr.DecodeQR(rgba)
	if err != nil {
		r.t.Fatalf("the code on screen cannot be read: %v", err)
	}
	return text
}

func TestShowMyPublicKeyDrawsTheKeyOfThePubFileBesideTheKeyFile(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	line := r.key.line("bob@desk")
	if err := os.WriteFile(r.file+".pub", []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyFile, KeyFile: r.file})
	r.settle()
	if !r.has(showButton) {
		t.Fatal("the dialog has no Show My Public Key as QR Code button")
	}
	r.ts.Tap(gluttertest.ByText(showButton))
	r.until("the dialog", func() bool { return r.tr.qr != nil })
	for _, want := range []string{"My Public Key", "ssh-ed25519", r.key.fingerprint(), "bob@desk", "never leaves this computer"} {
		if !r.has(want) {
			t.Errorf("the dialog does not show %q", want)
		}
	}
	if got := r.shownCode(); got != line {
		t.Errorf("the code on screen is %q, want the line of the .pub file %q", got, line)
	}
	// the private key is nowhere in it
	if priv, err := os.ReadFile(r.file); err == nil {
		for _, l := range strings.Split(string(priv), "\n") {
			if len(l) > 30 && !strings.HasPrefix(l, "-----") && r.has(l[:30]) {
				t.Error("a line of the private key is on screen")
			}
		}
	}
	r.ts.Tap(gluttertest.ByText("Close"))
	r.settle()
	if r.tr.qr != nil || r.ts.Exists(gluttertest.ByText("My Public Key")) {
		t.Error("the dialog did not close")
	}
}

func TestShowMyPublicKeyReadsThePublicHalfOfAnOpenSSHKeyWithoutAskingForThePassphrase(t *testing.T) {
	noLeaks(t)
	for name, pass := range map[string][]byte{"no passphrase": nil, "a passphrase": []byte("a long secret")} {
		t.Run(name, func(t *testing.T) {
			r := newTabRig(t, true)
			path := writeKeyFile(t, r.key, pass) // no .pub beside it
			r.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyFile, KeyFile: path})
			r.settle()
			r.ts.Tap(gluttertest.ByText(showButton))
			r.until("the dialog", func() bool { return r.tr.qr != nil })
			if r.tr.prompter.Showing() {
				t.Error("the passphrase was asked for: the public half is in the clear")
			}
			if got, want := r.shownCode(), r.key.line(""); got != want {
				t.Errorf("the code is %q, want %q", got, want)
			}
			if !r.has("(none)") {
				t.Error("a key read from the key file has no comment, and the dialog says so")
			}
		})
	}
}

func TestShowMyPublicKeyAsksForThePassphraseOfAPEMKey(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	//nolint:staticcheck // the legacy PEM encryption is what old key files have
	enc, err := x509.EncryptPEMBlock(rand.Reader, "EC PRIVATE KEY", der, []byte("pw"), x509.PEMCipherAES128)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ecdsa")
	if err := os.WriteFile(path, pem.EncodeToMemory(enc), 0o600); err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	r.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyFile, KeyFile: path})
	r.settle()
	r.ts.Tap(gluttertest.ByText(showButton))
	r.until("the question", func() bool { return r.tr.prompter.Showing() })
	if !r.has("Passphrase for the key") {
		t.Error("the passphrase is not asked for")
	}
	r.ts.EnterText(gluttertest.At(gluttertest.ByType[*ui.InputBox](), r.ts.Count(gluttertest.ByType[*ui.InputBox]())-1), "pw")
	r.ts.Tap(gluttertest.ByText("OK"))
	r.until("the dialog", func() bool { return r.tr.qr != nil })
	if got, want := r.shownCode(), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))); got != want {
		t.Errorf("the code is %q, want %q", got, want)
	}
	// cancelling the question shows nothing and says why
	r2 := newTabRig(t, true)
	r2.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyFile, KeyFile: path})
	r2.settle()
	r2.ts.Tap(gluttertest.ByText(showButton))
	r2.until("the question", func() bool { return r2.tr.prompter.Showing() })
	r2.ts.Tap(gluttertest.ByText("Cancel"))
	r2.until("the note", func() bool { return r2.has("passphrase was not given") })
	if r2.tr.qr != nil {
		t.Error("a dialog opened though the passphrase was not given")
	}
}

func TestShowMyPublicKeyOfTheAgent(t *testing.T) {
	noLeaks(t)
	k1, k2 := newKey(t), newKey(t)
	open := func(t *testing.T, keys ...testKey) *tabRig {
		r := newTabRig(t, true)
		socket := fakeAgent(t, keys...)
		r.hub.opts.Tab.AgentSocket = func() string { return socket }
		r.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyAgent})
		r.until("the keys of the agent", func() bool { return !r.tr.form.agentLoading && len(r.tr.form.agent) == len(keys) })
		return r
	}
	t.Run("one key", func(t *testing.T) {
		r := open(t, k1)
		r.ts.Tap(gluttertest.ByText(showButton))
		r.until("the dialog", func() bool { return r.tr.qr != nil })
		if got := r.shownCode(); got != k1.line("") {
			t.Errorf("the code is %q, want %q", got, k1.line(""))
		}
	})
	t.Run("several keys and none chosen", func(t *testing.T) {
		r := open(t, k1, k2)
		r.ts.Tap(gluttertest.ByText(showButton))
		r.until("the note", func() bool { return r.has("several keys") })
		if r.tr.qr != nil {
			t.Error("a dialog opened though the agent has several keys")
		}
		r.tr.form.fingerprint = k2.fingerprint()
		r.ts.Tap(gluttertest.ByText(showButton))
		r.until("the dialog", func() bool { return r.tr.qr != nil })
		if got := r.shownCode(); got != k2.line("") {
			t.Errorf("the code is %q, want the chosen key %q", got, k2.line(""))
		}
	})
	t.Run("a chosen key the agent no longer has", func(t *testing.T) {
		r := open(t, k1)
		r.tr.form.fingerprint = k2.fingerprint()
		r.ts.Tap(gluttertest.ByText(showButton))
		r.until("the note", func() bool { return r.has("not in the ssh-agent any more") })
	})
	t.Run("an agent with no key", func(t *testing.T) {
		r := newTabRig(t, true)
		socket := fakeAgent(t)
		r.hub.opts.Tab.AgentSocket = func() string { return socket }
		r.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyAgent})
		r.until("the note", func() bool { return !r.tr.form.agentLoading && r.tr.form.agentNote != "" })
		r.ts.Tap(gluttertest.ByText(showButton))
		r.until("the note", func() bool { return r.has("no key") })
	})
	t.Run("no agent", func(t *testing.T) {
		r := newTabRig(t, true) // SSH_AUTH_SOCK is empty
		r.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyAgent})
		r.until("the note", func() bool { return !r.tr.form.agentLoading && r.tr.form.agentNote != "" })
		r.ts.Tap(gluttertest.ByText(showButton))
		r.until("the note", func() bool { return r.has("no ssh-agent") })
	})
	t.Run("the keys are still being read", func(t *testing.T) {
		r := newTabRig(t, true)
		r.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyAgent})
		r.tr.form.agentLoading = true
		r.ts.Tap(gluttertest.ByText(showButton))
		r.until("the note", func() bool { return r.has("still being read") })
	})
}

func TestShowMyPublicKeySaysWhatIsInTheWay(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	r.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyFile})
	r.settle()
	// no key file chosen
	r.ts.Tap(gluttertest.ByText(showButton))
	r.until("the note", func() bool { return r.has("Choose the private key file first") })
	// a key file that is not there, and no .pub beside it
	r.tr.form.keyFile.SetValue(filepath.Join(t.TempDir(), "missing"))
	r.ts.Tap(gluttertest.ByText(showButton))
	r.until("the note", func() bool { return r.has("no such file") })
	if r.tr.qr != nil {
		t.Error("a dialog opened for a key file that is not there")
	}
	// a .pub file that is not a public key is not drawn
	dir := t.TempDir()
	path := filepath.Join(dir, "id_x")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".pub", []byte("this is not a public key"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.tr.form.keyFile.SetValue(path)
	r.ts.Tap(gluttertest.ByText(showButton))
	r.until("the note", func() bool { return r.has("not a public key") })
	if r.tr.qr != nil {
		t.Error("a dialog opened for text that is not a public key")
	}
	// the private key in the .pub place is refused: only a public key is ever drawn
	priv, err := os.ReadFile(r.file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".pub", priv, 0o644); err != nil {
		t.Fatal(err)
	}
	r.ts.Tap(gluttertest.ByText(showButton))
	r.until("the note", func() bool { return r.has("not a public key") })
	if r.tr.qr != nil {
		t.Error("the private key was drawn")
	}
}

func TestTheButtonDoesNothingWhileTheTabConnects(t *testing.T) {
	noLeaks(t)
	r := newTabRig(t, true)
	r.open(uitest.Themes()[0], tabState{Host: "h", Port: DefaultPort, Key: keyFile, KeyFile: r.file})
	r.tr.phase = phaseConnecting
	r.wt.Refresh()
	r.settle()
	r.ts.Tap(gluttertest.ByText(showButton))
	r.settle()
	if r.tr.qr != nil || r.tr.prompter.Showing() {
		t.Error("the button worked while the tab was connecting")
	}
}

func TestThePublicLineOfAKeyFileHasTheTagsOfItsParts(t *testing.T) {
	dir := t.TempDir()
	// a .pub that is a folder: it exists, so the key is not read from the key file, and it cannot be used
	asDir := filepath.Join(dir, "id_dir")
	if err := os.MkdirAll(asDir+".pub", 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := publicLineOfKeyFile(asDir, nil)
	wantRoute(t, err, "publicLineOfKeyFile->ReadPublicKeyFile.SSHLoginPubFileInvalid")
	// nothing there at all
	_, err = publicLineOfKeyFile(filepath.Join(dir, "missing"), nil)
	wantRoute(t, err, "publicLineOfKeyFile->PublicKeyOfKeyFile.SSHLoginKeyFileUnreadable")
	// a key file that is not a key
	junk := filepath.Join(dir, "junk")
	if err := os.WriteFile(junk, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = publicLineOfKeyFile(junk, nil)
	wantRoute(t, err, "publicLineOfKeyFile->PublicKeyOfKeyFile->KeyFileAuth.SSHLoginKeyFileInvalid")
}

func TestGoldensOfShowMyPublicKey(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			noLeaks(t)
			r := newTabRig(t, true)
			// the key of the agent is the same in every run, and no path of the machine is on screen
			socket := fakeAgent(t, seededKey(t, 3))
			r.hub.opts.Tab.AgentSocket = func() string { return socket }
			r.size = gui.Size{Width: 640, Height: 900}
			r.open(th, tabState{Host: "laptop.local", Port: DefaultPort, Key: keyAgent})
			r.until("the keys of the agent", func() bool { return !r.tr.form.agentLoading && len(r.tr.form.agent) == 1 })
			r.ts.Tap(gluttertest.ByText(showButton))
			r.until("the dialog", func() bool { return r.tr.qr != nil })
			uitest.MatchesGolden(r.ts, "tab_show_key_"+th.Name)
		})
	}
}
