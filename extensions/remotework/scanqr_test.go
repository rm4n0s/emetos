package remotework

import (
	"context"
	"image"
	"io"
	"testing"
	"time"

	rerrors "github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
)

var scanSize = gui.Size{Width: 800, Height: 1100}

// codeCamera is a camera that sees the room for two pictures and then a sheet with the QR code of the text.
func codeCamera(t testing.TB, name, text string) keyqr.FakeCamera {
	t.Helper()
	held, err := keyqr.SceneWithCode(text, 640, 480)
	if err != nil {
		t.Fatal(err)
	}
	room := keyqr.Scene(640, 480)
	return keyqr.FakeCamera{Name: name, Delay: 5 * time.Millisecond, Frames: []image.Image{room, room, held}}
}

// released waits until the fake source has no camera open.
func (s *shownPage) released(src *keyqr.FakeSource) {
	s.t.Helper()
	s.until("the camera to be released", func() bool { return src.Active() == 0 })
}

// close takes the page off the screen and closes its tab, as the window does, and runs what the page posted.
func (s *shownPage) close() {
	s.t.Helper()
	s.ts.PumpWidget(widgets.NewSizedBox(nil))
	s.p.tab.Content.OnClose()
	for i := 0; i < 20; i++ {
		s.p.api.Pump()
		time.Sleep(time.Millisecond)
	}
}

func (p *pageRig) thePage() *page { return p.ext.pages[len(p.ext.pages)-1] }

func TestScanQRCodeAddsTheKeyOfTheCameraAfterTheUserConfirmsIt(t *testing.T) {
	noLeaks(t)
	k := newKey(t)
	line := k.line("alice@laptop")
	src := keyqr.NewFakeSource(codeCamera(t, "Integrated Camera", line))
	p := newPageRigWith(t, false, src)
	s := p.show(t, uitest.Themes()[0], scanSize)
	pg := p.thePage()
	s.until("the look for cameras", func() bool { return !pg.cameras.looking })
	if pg.cameras.reason != "" || s.has("Scan QR Code is not available") {
		t.Fatalf("a camera is there and the page says %q", pg.cameras.reason)
	}
	if src.Opened() != 0 {
		t.Fatal("the camera was opened before the user asked to scan")
	}
	s.tap("Scan QR Code...")
	s.until("the dialog", func() bool { return pg.scan != nil && s.has("Hold the QR code") })
	s.until("the key", func() bool { return pg.scan != nil && pg.scan.State() == "confirm" })
	// the camera is let go as soon as the key was seen
	s.released(src)
	for _, want := range []string{"Add this key?", "ssh-ed25519", k.fingerprint(), "alice@laptop"} {
		if !s.has(want) {
			t.Errorf("the confirmation does not show %q", want)
		}
	}
	if keys := p.hub.Snapshot(p.folder).Keys; len(keys) != 0 {
		t.Fatalf("the key was added before the user confirmed: %v", keys)
	}
	s.tap("Add Key")
	keys := p.hub.Snapshot(p.folder).Keys
	if len(keys) != 1 || keys[0].Line() != line {
		t.Fatalf("the keys are %v, want %q", keys, line)
	}
	if pg.scan != nil || s.has("Add this key?") || !s.has("Added alice@laptop.") {
		t.Errorf("the dialog is %v, and the page does not say the key was added", pg.scan)
	}
	if src.Opened() != 1 || src.Active() != 0 {
		t.Errorf("opened %d times, %d still open", src.Opened(), src.Active())
	}
}

func TestScanQRCodeOfAKeyThatIsListedAlreadySaysSo(t *testing.T) {
	noLeaks(t)
	k := newKey(t)
	src := keyqr.NewFakeSource(codeCamera(t, "Cam", k.line("alice@laptop")))
	p := newPageRigWith(t, false, src)
	if _, err := p.hub.AddKey(k.line("")); err != nil {
		t.Fatal(err)
	}
	s := p.show(t, uitest.Themes()[0], scanSize)
	pg := p.thePage()
	s.until("the look for cameras", func() bool { return !pg.cameras.looking })
	s.tap("Scan QR Code...")
	s.until("the key", func() bool { return pg.scan != nil && pg.scan.State() == "confirm" })
	s.tap("Add Key")
	if !s.has("in the list already") || len(p.hub.Snapshot(p.folder).Keys) != 1 {
		t.Error("a key that is listed was not refused with the reason")
	}
	s.released(src)
}

func TestCancellingScanQRCodeAddsNothingAndLetsTheCameraGo(t *testing.T) {
	noLeaks(t)
	k := newKey(t)
	cam := codeCamera(t, "Cam", k.line(""))
	cam.Frames = cam.Frames[:2] // no code in sight
	src := keyqr.NewFakeSource(cam)
	p := newPageRigWith(t, false, src)
	s := p.show(t, uitest.Themes()[0], scanSize)
	pg := p.thePage()
	s.until("the look for cameras", func() bool { return !pg.cameras.looking })
	s.tap("Scan QR Code...")
	s.until("the camera", func() bool { return src.Active() == 1 })
	s.tap("Cancel")
	s.released(src)
	if pg.scan != nil || s.has("Hold the QR code") || len(p.hub.Snapshot(p.folder).Keys) != 0 {
		t.Error("cancelling did not close the dialog, or added a key")
	}
	// and the button works again
	s.tap("Scan QR Code...")
	s.until("the camera again", func() bool { return src.Opened() == 2 })
	s.tap("Cancel")
	s.released(src)
}

func TestClosingThePageWhileScanningLetsTheCameraGo(t *testing.T) {
	noLeaks(t)
	cam := codeCamera(t, "Cam", newKey(t).line(""))
	cam.Frames = cam.Frames[:2]
	src := keyqr.NewFakeSource(cam)
	p := newPageRigWith(t, false, src)
	s := p.show(t, uitest.Themes()[0], scanSize)
	pg := p.thePage()
	s.until("the look for cameras", func() bool { return !pg.cameras.looking })
	s.tap("Scan QR Code...")
	s.until("the camera", func() bool { return src.Active() == 1 })
	s.close()
	deadline := time.Now().Add(5 * time.Second)
	for src.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if src.Active() != 0 {
		t.Error("the camera was not released when the page closed")
	}
	if len(p.ext.pages) != 0 {
		t.Error("the page that closed is still known")
	}
}

// slowSource is a Source whose list of cameras waits for the test.
type slowSource struct {
	keyqr.Source
	release chan struct{}
}

func (s slowSource) Cameras(ctx context.Context) ([]keyqr.CameraInfo, error) {
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.Source.Cameras(ctx)
}

func TestTheButtonIsDisabledWithTheReasonWhenThereIsNoCamera(t *testing.T) {
	noLeaks(t)
	denied := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam"})
	denied.SetListError(rerrors.New("KeyQRCameraDenied", "this user may not open the cameras (is it in the video group?)"))
	cases := []struct {
		name string
		src  keyqr.Source
		want string
	}{
		{"no way to read a camera", nil, "no camera that Emetos can read"},
		{"no camera", keyqr.NewFakeSource(), "No camera was found on this computer."},
		{"no permission", denied, "video group"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPageRigWith(t, false, tc.src)
			s := p.show(t, uitest.Themes()[0], scanSize)
			pg := p.thePage()
			s.until("the reason", func() bool { return !pg.cameras.looking && pg.cameras.reason != "" })
			if !s.has("Scan QR Code is not available: ") || !s.has(tc.want) {
				t.Errorf("the page does not say why: %q", pg.cameras.reason)
			}
			s.ts.Tap(gluttertest.ByText("Scan QR Code..."))
			s.settle()
			if pg.scan != nil {
				t.Error("a disabled button opened the dialog")
			}
		})
	}
}

func TestTheButtonWaitsForTheLookForCameras(t *testing.T) {
	noLeaks(t)
	release := make(chan struct{})
	src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam"})
	p := newPageRigWith(t, false, slowSource{Source: src, release: release})
	s := p.show(t, uitest.Themes()[0], scanSize)
	pg := p.thePage()
	s.settle()
	if !pg.cameras.looking || !s.has("Looking for cameras...") {
		t.Fatal("the page does not say it is looking for cameras")
	}
	s.ts.Tap(gluttertest.ByText("Scan QR Code..."))
	s.settle()
	if pg.scan != nil {
		t.Error("the dialog opened before the cameras were known")
	}
	close(release)
	s.until("the cameras", func() bool { return !pg.cameras.looking })
	if s.has("Scan QR Code is not available") {
		t.Error("the page still says Scan QR Code is not available")
	}
}

func TestALookForCamerasThatTheClosingPageEndsPostsNothing(t *testing.T) {
	noLeaks(t)
	release := make(chan struct{})
	src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam"})
	p := newPageRigWith(t, false, slowSource{Source: src, release: release})
	s := p.show(t, uitest.Themes()[0], scanSize)
	s.settle()
	s.close() // the look is waiting for the cameras when the page goes
	if src.Opened() != 0 {
		t.Error("a camera was opened")
	}
}

func TestAPageWithAnUnreadableConfigurationKeepsScanQRCodeOff(t *testing.T) {
	noLeaks(t)
	src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam"})
	p := newPageRigWith(t, false, src)
	s := p.show(t, uitest.Themes()[0], scanSize)
	pg := p.thePage()
	s.until("the look for cameras", func() bool { return !pg.cameras.looking })
	// the button is the one of the keys: with the file locked nothing can be added
	if got := pg.scanQRButton(true); got == nil {
		t.Fatal("no button")
	}
}

func TestGoldensOfScanQRCodeOnThePage(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run("a camera "+th.Name, func(t *testing.T) {
			noLeaks(t)
			cam := keyqr.FakeCamera{Name: "Integrated Camera", Delay: 5 * time.Millisecond, Frames: []image.Image{keyqr.Scene(1280, 720)}}
			p := newPageRigWith(t, true, keyqr.NewFakeSource(cam))
			s := p.show(t, th, scanSize)
			pg := p.thePage()
			s.until("the look for cameras", func() bool { return !pg.cameras.looking })
			uitest.MatchesGolden(s.ts, "page_scan_button_"+th.Name)
		})
		t.Run("the dialog "+th.Name, func(t *testing.T) {
			noLeaks(t)
			cam := keyqr.FakeCamera{Name: "Integrated Camera", Delay: 5 * time.Millisecond, Frames: []image.Image{keyqr.Scene(1280, 720)}}
			src := keyqr.NewFakeSource(cam)
			p := newPageRigWith(t, true, src)
			s := p.show(t, th, scanSize)
			pg := p.thePage()
			s.until("the look for cameras", func() bool { return !pg.cameras.looking })
			s.tap("Scan QR Code...")
			s.until("the picture", func() bool {
				return pg.scan != nil && pg.scan.State() == "scanning" && s.ts.Exists(gluttertest.ByType[*widgets.RawImage]())
			})
			uitest.MatchesGolden(s.ts, "page_scan_dialog_"+th.Name)
			s.tap("Cancel")
			s.released(src)
		})
	}
}

// ---- the dev commands ----

func TestTheDevCommandsPutAFakeCameraInPlaceOfTheRealOnes(t *testing.T) {
	noLeaks(t)
	cases := []struct {
		name  string
		args  any
		check func(t *testing.T, s *shownPage, pg *page, src keyqr.Source)
	}{
		{"a key made now", map[string]any{"comment": "bob@desk"}, func(t *testing.T, s *shownPage, pg *page, src keyqr.Source) {
			s.until("the key", func() bool { return pg.scan != nil && pg.scan.State() == "confirm" })
			if k := pg.scan.Key(); k.Comment != "bob@desk" || k.Type() != "ssh-ed25519" {
				t.Errorf("the key is %q of type %s", k.Comment, k.Type())
			}
		}},
		{"the text given", map[string]any{"text": "https://example.com/not-a-key"}, func(t *testing.T, s *shownPage, pg *page, src keyqr.Source) {
			s.until("the note", func() bool { return s.has("not a public key") })
			if pg.scan.State() != "scanning" {
				t.Errorf("the dialog is %s", pg.scan.State())
			}
		}},
		{"nothing in sight", map[string]any{"nothing": true}, func(t *testing.T, s *shownPage, pg *page, src keyqr.Source) {
			time.Sleep(800 * time.Millisecond)
			s.settle()
			if pg.scan.State() != "scanning" || s.has("not a public key") {
				t.Errorf("the dialog is %s", pg.scan.State())
			}
		}},
		{"two cameras", map[string]any{"cameras": float64(2), "nothing": true}, func(t *testing.T, s *shownPage, pg *page, src keyqr.Source) {
			cams, err := src.Cameras(t.Context())
			if err != nil || len(cams) != 2 || cams[1].Name != "USB Camera" {
				t.Errorf("cameras %+v, %v", cams, err)
			}
			s.until("the picture", func() bool { return pg.scan.State() == "scanning" })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPageRigWith(t, false, nil) // the real cameras are not even offered
			s := p.show(t, uitest.Themes()[0], scanSize)
			pg := p.thePage()
			s.settle()
			if err := p.api.Run(DevFakeCameraCommand, tc.args); err != nil {
				t.Fatal(err)
			}
			src := p.hub.Cameras()
			if _, ok := src.(*keyqr.FakeSource); !ok {
				t.Fatalf("the cameras are %T", src)
			}
			if err := p.api.Run(DevScanQRCommand, nil); err != nil {
				t.Fatal(err)
			}
			s.until("the dialog", func() bool { return pg.scan != nil })
			tc.check(t, s, pg, src)
			pg.scan.Close()
			s.released(src.(*keyqr.FakeSource))
		})
	}
}

// failingReader is a source of randomness that fails.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrNoProgress }

func TestTheDevCommandsRefuseWhatTheyCannotDo(t *testing.T) {
	noLeaks(t)
	p := newPageRigWith(t, false, nil, func(h *HubOptions) { h.Random = failingReader{} })
	s := p.show(t, uitest.Themes()[0], scanSize)
	s.settle()
	wantRoute(t, p.api.Run(DevFakeCameraCommand, nil), "remoteWork.devFakeCamera.RemoteWorkDevFakeCameraInvalid")
	wantRoute(t, p.api.Run(DevFakeCameraCommand, map[string]any{"text": string(make([]byte, keyqr.MaxQRBytes+1))}),
		"remoteWork.devFakeCamera->SceneWithCode->EncodeQR->encodeQR.KeyQREncodeFailed")
	if _, ok := p.hub.Cameras().(*keyqr.FakeSource); ok {
		t.Error("a camera was put in after the command failed")
	}
	// no page is open: the dialog has nowhere to be
	s.close()
	wantRoute(t, p.api.Run(DevScanQRCommand, nil), "remoteWork.devScanQR.RemoteWorkDevScanInvalid")
}
