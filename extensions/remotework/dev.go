package remotework

import (
	"context"
	"crypto/ed25519"
	"image"
	"math"
	"os"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
	"github.com/rm4n0s/emetos/extensions/internal/sharing"
)

// DevBindEnv is the environment variable that makes the listeners of a debug build (or a build with the emetos_dev tag) listen on one
// address instead of every interface: the real-input tests of step 17e.4 set it to 127.0.0.1 so that nothing is exposed on the
// network (SPEC §15.2). A release build never reads it: its listeners are always on every interface.
const DevBindEnv = "EMETOS_DEV_REMOTEWORK_BIND"

// DevInterfacesEnv is the environment variable that replaces the network interfaces of this computer by a list (debug and emetos_dev
// builds only; see sharing.DevInterfacesEnv): the real-input tests set it so that no address of the machine they run on is on a
// screenshot.
const DevInterfacesEnv = sharing.DevInterfacesEnv

// bindHost is the host the listeners bind: the value of DevBindEnv in a debug or emetos_dev build, empty (every interface) in any
// other and when the variable is not set.
func bindHost() string {
	if !foundation.DebugMode && !devBuild {
		return ""
	}
	return os.Getenv(DevBindEnv)
}

// The dev commands of step 15.5, registered in a workspace in debug builds and builds with the emetos_dev tag (SPEC §15.2).
const (
	// DevFakeCameraCommand is dev.keyqr.fakeCamera: it puts a camera that plays pictures from memory in place of the real ones, so
	// that a proof or a test of the Scan QR Code dialog never opens a real device. Arguments: {"comment", "text", "nothing",
	// "cameras"}. Run it before the page is opened: the page looks for cameras when it is made.
	DevFakeCameraCommand = "dev.keyqr.fakeCamera"
	// DevScanQRCommand is dev.remotework.scanQR: it opens the Scan QR Code dialog of the Remote Work page that is open.
	DevScanQRCommand = "dev.remotework.scanQR"
)

// fakeCameraFrames is how many pictures of the room the fake camera shows before the code is held up (at 10 a second).
const fakeCameraFrames = 6

// registerDevWorkspace registers the dev commands of the Scan QR Code proof in a workspace.
func (r *remoteWork) registerDevWorkspace() error {
	if !foundation.DebugMode && !devBuild {
		return nil
	}
	if err := r.api.Commands().Register(extension.CommandSpec{
		ID: DevFakeCameraCommand,
		Description: `Use a camera that plays pictures from memory for Scan QR Code. Arguments: {"comment": "name of the key shown", ` +
			`"text": "text of the QR code (default: a new Ed25519 public key made now)", "nothing": true for a camera that sees no code, "cameras": 1 or 2}.`,
	}, r.devFakeCamera); err != nil {
		return err
	}
	return r.api.Commands().Register(extension.CommandSpec{
		ID: DevScanQRCommand, Description: "Open the Scan QR Code dialog of the Remote Work page that is open.",
	}, r.devScanQR)
}

// devFakeCamera is dev.keyqr.fakeCamera. The key of the code is made now and its private half is dropped: the proof shows the
// public key only, and nothing secret is made or kept. It fails with RemoteWorkDevFakeCameraInvalid when the key cannot be made, and
// with KeyQREncodeFailed for a text that does not fit in a QR code.
func (r *remoteWork) devFakeCamera(_ context.Context, args any) error {
	m, _ := args.(map[string]any)
	text, _ := m["text"].(string)
	comment, _ := m["comment"].(string)
	if comment == "" {
		comment = "alice@laptop"
	}
	if text == "" {
		line, err := func() (string, error) {
			pub, _, err := ed25519.GenerateKey(r.hub.opts.Random)
			if err != nil {
				return "", err
			}
			key, err := ssh.NewPublicKey(pub)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " " + comment, nil
		}()
		if err != nil {
			return errors.NewErr("RemoteWorkDevFakeCameraInvalid", err)
		}
		text = line
	}
	scene := keyqr.Scene(1280, 720)
	frames := make([]image.Image, fakeCameraFrames)
	for i := range frames {
		frames[i] = scene
	}
	if nothing, _ := m["nothing"].(bool); !nothing {
		held, err := keyqr.SceneWithCode(text, 1280, 720)
		if err != nil {
			return err // a text that does not fit in a QR code: KeyQREncodeFailed
		}
		frames = append(frames, held)
	}
	cams := []keyqr.FakeCamera{{Name: "Integrated Camera", Frames: frames, Delay: keyqr.DefaultScanInterval}}
	if n, _ := m["cameras"].(float64); n >= 2 {
		cams = append(cams, keyqr.FakeCamera{Name: "USB Camera", Frames: []image.Image{scene}, Delay: keyqr.DefaultScanInterval / 2})
	}
	r.hub.SetCameras(keyqr.NewFakeSource(cams...))
	return nil
}

// devScanQR is dev.remotework.scanQR. It fails with RemoteWorkDevScanInvalid when no Remote Work page is open in the workspace.
func (r *remoteWork) devScanQR(context.Context, any) error {
	if len(r.pages) == 0 {
		return errors.New("RemoteWorkDevScanInvalid", "dev.remotework.scanQR needs the Remote Work page to be open: run emetos.remotework.manage first")
	}
	p := r.pages[len(r.pages)-1]
	p.openScan()
	return nil
}

// registerDev registers the dev command of the proofs in the scope of a tab, in debug builds and builds with the emetos_dev tag
// only (SPEC §15.2).
func (r *remoteWork) registerDev() error {
	if !foundation.DebugMode && !devBuild {
		return nil
	}
	return r.api.Commands().Register(extension.CommandSpec{
		ID: DevConnectCommand,
		Description: `Fill the connect dialog of the Remote Work tab and press Connect. Arguments: {"host", "port", "keyFile", "workspace"}, ` +
			`or {"host", "port", "agent": true, "fingerprint"}.`,
	}, r.devConnect)
}

// devConnect is dev.remotework.connect: it puts the arguments in the form of the tab and connects, as the user does. It fails with
// RemoteWorkDevConnectInvalid for arguments that are not a host with a key file or the agent.
func (r *remoteWork) devConnect(_ context.Context, args any) error {
	m, _ := args.(map[string]any)
	host, _ := m["host"].(string)
	st := tabState{Host: host, Port: DefaultPort, Key: keyFile}
	if f, ok := m["port"].(float64); ok && f == math.Trunc(f) {
		st.Port = int(f)
	}
	st.KeyFile, _ = m["keyFile"].(string)
	st.Workspace, _ = m["workspace"].(string)
	if agent, _ := m["agent"].(bool); agent {
		st.Key = keyAgent
		st.Fingerprint, _ = m["fingerprint"].(string)
	}
	if host == "" || (st.Key == keyFile && st.KeyFile == "") || len(r.tabs) == 0 {
		return errors.New("RemoteWorkDevConnectInvalid", `dev.remotework.connect needs a Remote Work tab in front and {"host", "keyFile"} or {"host", "agent": true}`, "arguments", args)
	}
	t := r.tabs[len(r.tabs)-1]
	t.form.set(st)
	t.workspac = st.Workspace
	t.connect()
	return nil
}
