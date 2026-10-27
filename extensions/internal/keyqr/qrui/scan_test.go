package qrui

import (
	"context"
	"image"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"
	"golang.org/x/sys/unix"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
)

func TestTheDialogOpensTheFirstCameraAndShowsItsPicture(t *testing.T) {
	src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Integrated Camera", Frames: []image.Image{scene(1280, 720)}})
	r := newRig(t, uitest.Themes()[0], src, nil)
	r.pump()
	if r.d.State() == "listing" && !r.has("Looking for cameras") {
		t.Error("the dialog does not say it is looking for cameras")
	}
	r.until("the picture", func() bool { return r.d.State() == "scanning" && r.hasPicture() })
	if src.Opened() != 1 || src.LastOpened() != "fake:0" || src.Active() != 1 {
		t.Errorf("opened %d, last %q, active %d", src.Opened(), src.LastOpened(), src.Active())
	}
	if !r.has("Scan QR Code") || !r.has("Hold the QR code") || r.has("Add Key") {
		t.Error("the dialog does not say what to do")
	}
	// the picture is the frame made smaller: a frame of 1280x720 is not kept at its size
	pic := r.ts.Widget(findPicture(r)).(*widgets.RawImage).Image()
	if pic.Width() != previewWidth || pic.Height() != previewWidth*720/1280 {
		t.Errorf("the preview is %dx%d", pic.Width(), pic.Height())
	}
	// one camera: no picker
	if r.ts.Exists(byDropdown()) {
		t.Error("a picker for one camera")
	}
}

func TestTheDialogClosesOnTheFirstPublicKeyAndAsksBeforeAddingIt(t *testing.T) {
	src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam", Delay: 3 * time.Millisecond, Frames: []image.Image{
		scene(640, 480), withCode(t, "https://example.com/not-a-key", 640, 480), withCode(t, edLine, 640, 480), withCode(t, "ssh-ed25519 AAAA this one never comes", 640, 480),
	}})
	r := newRig(t, uitest.Themes()[0], src, nil)
	r.until("the confirmation", func() bool { return r.d.State() == "confirm" })
	// the camera is let go as soon as the key is seen, before the user answers
	r.until("the camera to be released", released(src, 1))
	if r.hasPicture() || r.d.picture != nil {
		t.Error("a picture of the camera is still there")
	}
	for _, want := range []string{"Add this key?", "ssh-ed25519", "SHA256:", "laptop@emetos", "Compare the fingerprint"} {
		if !r.has(want) {
			t.Errorf("the confirmation does not show %q", want)
		}
	}
	if len(r.added) != 0 || r.closes != 0 {
		t.Fatal("the key was added before the user confirmed")
	}
	if r.d.Key().Comment != "laptop@emetos" || r.d.Key().Fingerprint() == "" {
		t.Errorf("the key is %+v", r.d.Key())
	}
	r.tap("Add Key")
	if len(r.added) != 1 || r.added[0].Line() != edLine || r.closes != 1 {
		t.Fatalf("added %d keys, closed %d times", len(r.added), r.closes)
	}
	if r.has("Add this key?") {
		t.Error("the dialog is still on screen")
	}
}

func TestOtherQRCodesAreIgnoredAndSaidToBeNotAKey(t *testing.T) {
	src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam", Delay: 3 * time.Millisecond, Frames: []image.Image{withCode(t, "https://example.com/pair?code=7", 640, 480)}})
	r := newRig(t, uitest.Themes()[0], src, nil)
	r.until("the note", func() bool { return r.d.State() == "scanning" && r.has("not a public key") })
	// it stays scanning, with the camera on, however long the code is in front of it
	time.Sleep(40 * time.Millisecond)
	r.pump()
	if r.d.State() != "scanning" || src.Active() != 1 || len(r.added) != 0 || r.closes != 0 {
		t.Errorf("state %s, active %d, added %d, closed %d", r.d.State(), src.Active(), len(r.added), r.closes)
	}
}

func TestTheNoteGoesWhenTheCodeDoes(t *testing.T) {
	// a code that is not a key for a few frames, then a scene with nothing: the note stays for a moment and goes
	frames := []image.Image{withCode(t, "WIFI:T:WPA;S:home;P:x;;", 640, 480), withCode(t, "WIFI:T:WPA;S:home;P:x;;", 640, 480)}
	src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam", Delay: 3 * time.Millisecond, Frames: append(frames, scene(640, 480))})
	r := newRig(t, uitest.Themes()[0], src, nil)
	r.until("the note", func() bool { return r.has("not a public key") })
	r.until("the note to go", func() bool { return !r.has("not a public key") && r.has("Hold the QR code") })
}

func TestCancelLetsTheCameraGoAndKeepsNoFrame(t *testing.T) {
	src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam", Delay: 3 * time.Millisecond, Frames: []image.Image{scene(640, 480)}})
	r := newRig(t, uitest.Themes()[0], src, nil)
	r.until("the picture", func() bool { return r.hasPicture() })
	r.tap("Cancel")
	if r.closes != 1 || !r.d.closed {
		t.Fatalf("closed %d times", r.closes)
	}
	if r.d.picture != nil || r.d.latest.Load() != nil {
		t.Error("a frame is kept after the dialog closed")
	}
	r.until("the camera to be released", released(src, 1))
	// what the goroutine of the camera still posts is not shown, and nothing opens the camera again
	r.pump()
	time.Sleep(20 * time.Millisecond)
	r.pump()
	if src.Opened() != 1 || r.hasPicture() || r.d.picture != nil {
		t.Errorf("opened %d times; a picture came back", src.Opened())
	}
	r.d.Close() // a second close does nothing
	if r.closes != 1 {
		t.Errorf("the owner was told %d times", r.closes)
	}
}

func TestTheCameraIsOnlyOpenWhileTheDialogIs(t *testing.T) {
	src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam", Frames: []image.Image{scene(640, 480)}})
	// nothing opened before the dialog is made, and a dialog closed at once opens none or lets it go
	if src.Opened() != 0 {
		t.Fatal("the source was opened before it was asked")
	}
	ctx, cancel := context.WithCancel(t.Context())
	r := newRig(t, uitest.Themes()[0], src, ctx)
	r.until("the camera", func() bool { return src.Active() == 1 })
	// the context of the extension ends: the camera is let go with it
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for src.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if src.Active() != 0 || src.Released() != 1 {
		t.Errorf("active %d, released %d after the context ended", src.Active(), src.Released())
	}
}

func TestSeveralCamerasAreChosenFromAPicker(t *testing.T) {
	src := keyqr.NewFakeSource(
		keyqr.FakeCamera{Name: "Integrated Camera", Delay: 2 * time.Millisecond, Frames: []image.Image{scene(640, 480)}},
		keyqr.FakeCamera{Name: "USB Camera", Delay: 2 * time.Millisecond, Frames: []image.Image{withCode(t, edLine, 640, 480)}},
	)
	r := newRig(t, uitest.Themes()[0], src, nil)
	r.until("the first camera", func() bool { return r.d.State() == "scanning" && r.hasPicture() })
	if !r.ts.Exists(byDropdown()) || !r.has("Integrated Camera") {
		t.Fatal("the picker does not show the camera that is open")
	}
	r.ts.Tap(byDropdown())
	r.pump()
	if !r.has("USB Camera") {
		t.Fatal("the list of cameras did not open")
	}
	r.tap("USB Camera  fake:1")
	// the first camera is let go, the second opened, and its code read
	r.until("the second camera's key", func() bool { return r.d.State() == "confirm" })
	if src.Opened() != 2 || src.LastOpened() != "fake:1" {
		t.Errorf("opened %d, last %q", src.Opened(), src.LastOpened())
	}
	r.until("both cameras to be released", released(src, 2))
}

func TestScanAgainGoesBackToTheCamera(t *testing.T) {
	src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam", Delay: 2 * time.Millisecond, Frames: []image.Image{withCode(t, edLine, 640, 480)}})
	r := newRig(t, uitest.Themes()[0], src, nil)
	r.until("the confirmation", func() bool { return r.d.State() == "confirm" })
	r.until("the camera to be released", released(src, 1))
	r.tap("Scan Again")
	if r.d.State() == "confirm" && src.Opened() < 2 {
		r.until("the camera again", func() bool { return src.Opened() == 2 })
	}
	r.until("the key again", func() bool { return src.Opened() == 2 && r.d.State() == "confirm" })
	r.tap("Cancel")
	r.until("the camera to be released", released(src, 2))
	if len(r.added) != 0 || r.closes != 1 {
		t.Errorf("added %d, closed %d", len(r.added), r.closes)
	}
}

// brokenSource wraps a source: its first open fails, and its stream can be made to fail.
type brokenSource struct {
	*keyqr.FakeSource
	mu       sync.Mutex
	openErrs []error
	failAt   int // the stream fails on this frame (1 is the first) when above 0
}

func (b *brokenSource) Open(ctx context.Context, id string) (keyqr.Stream, error) {
	b.mu.Lock()
	if len(b.openErrs) > 0 {
		err := b.openErrs[0]
		b.openErrs = b.openErrs[1:]
		b.mu.Unlock()
		return nil, err
	}
	failAt := b.failAt
	b.mu.Unlock()
	s, err := b.FakeSource.Open(ctx, id)
	if err != nil || failAt == 0 {
		return s, err
	}
	return &failingStream{Stream: s, left: failAt}, nil
}

type failingStream struct {
	keyqr.Stream
	left int
}

func (s *failingStream) Next(ctx context.Context) (image.Image, error) {
	if s.left--; s.left <= 0 {
		return nil, errors.New("KeyQRCameraGone", "the camera is not there any more")
	}
	return s.Stream.Next(ctx)
}

func TestACameraThatCannotBeOpenedOrStopsIsSaidSoAndCanBeTriedAgain(t *testing.T) {
	fake := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam", Delay: 2 * time.Millisecond, Frames: []image.Image{scene(640, 480)}})
	src := &brokenSource{FakeSource: fake, openErrs: []error{errors.New("KeyQRCameraBusy", "the camera is in use by another program")}}
	r := newRig(t, uitest.Themes()[0], src, nil)
	r.until("the failure", func() bool { return r.d.State() == "failed" })
	if !r.has("in use by another program") || !r.has("Try Again") || r.has("Add Key") {
		t.Error("the failure is not said")
	}
	r.tap("Try Again")
	r.until("the camera", func() bool { return r.d.State() == "scanning" && r.hasPicture() })
	// the camera goes away while it scans
	src.mu.Lock()
	src.failAt = 3
	src.mu.Unlock()
	r.tap("Cancel")
	r2 := newRig(t, uitest.Themes()[0], src, nil)
	r2.until("the failure", func() bool { return r2.d.State() == "failed" })
	if !r2.has("not there any more") {
		t.Error("a camera that went is not said")
	}
	r2.until("the camera to be released", func() bool { return fake.Active() == 0 })
	// a failure with a plain error says it as it is
	plain := &brokenSource{FakeSource: fake, openErrs: []error{unix.EIO}}
	r3 := newRig(t, uitest.Themes()[0], plain, nil)
	r3.until("the failure", func() bool { return r3.d.State() == "failed" })
	if !r3.has(unix.EIO.Error()) {
		t.Errorf("the plain error is not shown")
	}
}

func TestNoCameraIsSaidWithItsReasonAndNothingIsOpened(t *testing.T) {
	cases := []struct {
		name   string
		source func() keyqr.Source
		want   string
	}{
		{"no camera", func() keyqr.Source { return keyqr.NewFakeSource() }, "No camera was found"},
		{"no source", func() keyqr.Source { return nil }, "no camera that Emetos can read"},
		{"no permission", func() keyqr.Source {
			f := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam"})
			f.SetListError(errors.New("KeyQRCameraDenied", "this user may not open the cameras (is it in the video group?)"))
			return f
		}, "video group"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.source()
			r := newRig(t, uitest.Themes()[0], src, nil)
			r.until("the reason", func() bool { return r.d.State() == "no camera" })
			if !r.has(tc.want) || !r.has("Close") || r.has("Hold the QR code") {
				t.Errorf("the dialog does not say %q", tc.want)
			}
			if f, ok := src.(*keyqr.FakeSource); ok && f.Opened() != 0 {
				t.Error("a camera was opened")
			}
			r.tap("Close")
			if r.closes != 1 {
				t.Errorf("closed %d times", r.closes)
			}
		})
	}
}

func TestAnOpenInTheMiddleOfAListIsLetGoWhenTheDialogCloses(t *testing.T) {
	// closing while the list is still being read opens nothing afterwards
	src := keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Cam", Frames: []image.Image{scene(640, 480)}})
	r := newRig(t, uitest.Themes()[0], src, nil)
	r.d.Close()
	for i := 0; i < 20; i++ {
		r.pump()
		time.Sleep(2 * time.Millisecond)
	}
	if src.Active() != 0 {
		t.Errorf("%d cameras are open after the dialog closed", src.Active())
	}
}

func TestThePreviewIsMadeSmallAndKeepsTheProportions(t *testing.T) {
	cases := []struct {
		name         string
		img          image.Image
		wantW, wantH int
	}{
		{"a frame of an MJPEG camera", scene(1280, 720), 480, 270},
		{"a frame of 1920x1080", scene(1920, 1080), 480, 270},
		{"a small frame is not enlarged", scene(320, 240), 320, 240},
		{"a YUYV frame", image.NewYCbCr(image.Rect(0, 0, 640, 480), image.YCbCrSubsampleRatio422), 480, 360},
		{"a frame that starts away from the origin", scene(800, 600).SubImage(image.Rect(100, 100, 740, 580)), 480, 360},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pic, err := previewPicture(tc.img)
			if err != nil {
				t.Fatal(err)
			}
			if pic.Width() != tc.wantW || pic.Height() != tc.wantH {
				t.Errorf("%dx%d, want %dx%d", pic.Width(), pic.Height(), tc.wantW, tc.wantH)
			}
		})
	}
}

func TestADialogWithNoOptionsDoesNotPanic(t *testing.T) {
	// no Close, no Add: the owner may not care
	d := NewScanDialog(ScanOptions{Post: func(f func()) { f() }, Refresh: func() {}})
	d.Close()
	d.add()
	if d.State() != "no camera" {
		t.Errorf("state %s", d.State())
	}
}

// findPicture and byDropdown are finders.
func findPicture(*rig) gluttertest.Finder { return gluttertest.ByType[*widgets.RawImage]() }
func byDropdown() gluttertest.Finder      { return gluttertest.ByType[*ui.Dropdown]() }

func TestNoCameraReasonIsEmptyOnlyWhenThereIsACamera(t *testing.T) {
	cams := []keyqr.CameraInfo{{ID: "fake:0", Name: "Cam"}}
	cases := []struct {
		name string
		cams []keyqr.CameraInfo
		err  error
		want string
	}{
		{"a camera", cams, nil, ""},
		{"none", nil, nil, "No camera was found on this computer."},
		{"an error with its own words", nil, errors.New("KeyQRCameraDenied", "this user may not open the cameras"), "this user may not open the cameras"},
		{"a plain error", cams, unix.EIO, unix.EIO.Error()},
	}
	for _, tc := range cases {
		if got := NoCameraReason(tc.cams, tc.err); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
