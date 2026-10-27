package keyqr

import (
	"context"
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// devices is a set of fake device files: the drivers they open, and the error that opening one gives.
type devices struct {
	drivers map[string]*fakeDriver
	openErr map[string]error
	opened  []string
}

func (d *devices) source(paths ...string) *V4L2 {
	return &V4L2{
		glob: func() ([]string, error) { return append([]string(nil), paths...), nil },
		open: func(path string) (driver, error) {
			d.opened = append(d.opened, path)
			if err := d.openErr[path]; err != nil {
				return nil, err
			}
			drv, ok := d.drivers[path]
			if !ok {
				return nil, unix.ENOENT
			}
			return drv, nil
		},
		opts: fastOptions,
	}
}

func camera(card string, formats ...uint32) *fakeDriver {
	f := newFakeDriver(formats...)
	f.caps.Card = card
	return f
}

func TestTheCamerasAreTheCaptureDevicesInTheOrderOfTheirNumbers(t *testing.T) {
	metadata := camera("Integrated Camera", PixFmtMJPEG)
	metadata.caps = capability{Card: "Integrated Camera", Capture: false, Streaming: true}
	d := &devices{
		drivers: map[string]*fakeDriver{
			"/dev/video10": camera("External", PixFmtYUYV),
			"/dev/video2":  camera("Integrated Camera", PixFmtMJPEG),
			"/dev/video3":  metadata,
			"/dev/video1":  func() *fakeDriver { f := camera("Output only", PixFmtMJPEG); f.caps.Streaming = false; return f }(),
			"/dev/video4":  func() *fakeDriver { f := camera("Broken", PixFmtMJPEG); f.errs["QueryCap"] = unix.EINVAL; return f }(),
		},
		openErr: map[string]error{"/dev/video5": unix.EBUSY, "/dev/video6": unix.ENODEV},
	}
	src := d.source("/dev/video10", "/dev/video2", "/dev/video3", "/dev/video1", "/dev/video4", "/dev/video5", "/dev/video6")
	cams, err := src.Cameras(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []CameraInfo{{ID: "/dev/video2", Name: "Integrated Camera"}, {ID: "/dev/video10", Name: "External"}}
	if !reflect.DeepEqual(cams, want) {
		t.Errorf("cameras %+v, want %+v", cams, want)
	}
	// every device that was opened to be asked was let go, and nothing was started
	for path, drv := range d.drivers {
		if drv.closed != 1 || drv.streamOn || drv.count("StreamOn") != 0 || drv.count("SetFormat") != 0 {
			t.Errorf("%s: closed %d times, calls %v", path, drv.closed, drv.callLog())
		}
	}
}

func TestNoCameraIsNothingAndNoPermissionIsSaid(t *testing.T) {
	none := (&devices{}).source()
	if cams, err := none.Cameras(t.Context()); err != nil || len(cams) != 0 {
		t.Errorf("no device: %v, %v", cams, err)
	}
	denied := (&devices{openErr: map[string]error{"/dev/video0": unix.EACCES}}).source("/dev/video0", "/dev/video1")
	_, err := denied.Cameras(t.Context())
	assertRoute(t, err, "V4L2.Cameras.KeyQRCameraDenied")
	// a device that is in use while another is denied is still only a denial: there is nothing to list
	// a camera that can be used among devices that are denied is listed
	d := &devices{drivers: map[string]*fakeDriver{"/dev/video1": camera("Cam", PixFmtMJPEG)}, openErr: map[string]error{"/dev/video0": unix.EPERM}}
	cams, err := d.source("/dev/video0", "/dev/video1").Cameras(t.Context())
	if err != nil || len(cams) != 1 {
		t.Errorf("one camera and one denied: %v, %v", cams, err)
	}
	// glob that fails
	broken := &V4L2{glob: func() ([]string, error) { return nil, unix.EIO }}
	_, err = broken.Cameras(t.Context())
	assertRoute(t, err, "V4L2.Cameras.KeyQRCameraListFailed")
	// a cancelled list
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d2 := &devices{drivers: map[string]*fakeDriver{"/dev/video0": camera("Cam", PixFmtMJPEG)}}
	_, err = d2.source("/dev/video0").Cameras(ctx)
	assertRoute(t, err, "V4L2.Cameras.KeyQRCancelled")
	if len(d2.opened) != 0 {
		t.Errorf("a cancelled list opened %v", d2.opened)
	}
}

func TestACameraOfTheListIsOpenedAndStreams(t *testing.T) {
	drv := camera("Integrated Camera", PixFmtYUYV)
	drv.give(yuyvFrame(77))
	d := &devices{drivers: map[string]*fakeDriver{"/dev/video0": drv}}
	s, err := d.source("/dev/video0").Open(t.Context(), "/dev/video0")
	if err != nil {
		t.Fatal(err)
	}
	img, err := s.Next(t.Context())
	if err != nil || levelOf(t, img) != 77 {
		t.Fatalf("frame %v, %v", img, err)
	}
	if err := s.Close(); err != nil || drv.closed != 1 {
		t.Errorf("Close: %v, closed %d times", err, drv.closed)
	}
}

func TestAnOpenThatTheSystemRefusesHasItsTag(t *testing.T) {
	cases := []struct {
		name string
		err  error
		tag  string
	}{
		{"in use", unix.EBUSY, "KeyQRCameraBusy"},
		{"no permission", unix.EACCES, "KeyQRCameraDenied"},
		{"no such device", unix.ENODEV, "KeyQRCameraGone"},
		{"another failure", unix.EMFILE, "KeyQRCameraOpenFailed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &devices{openErr: map[string]error{"/dev/video0": tc.err}}
			_, err := d.source("/dev/video0").Open(t.Context(), "/dev/video0")
			assertRoute(t, err, "V4L2.Open->openStream."+tc.tag)
			if !errors.Is(err, tc.err) {
				t.Errorf("the cause is lost from %v", err)
			}
		})
	}
}

func TestDeviceNumbersOrderTheNamesAsNumbers(t *testing.T) {
	cases := map[string]int{"/dev/video0": 0, "/dev/video2": 2, "/dev/video10": 10, "/dev/video": 1 << 30, "/dev/other3": 1 << 30, "": 1 << 30}
	for path, want := range cases {
		if got := deviceNumber(path); got != want {
			t.Errorf("deviceNumber(%q) = %d, want %d", path, got, want)
		}
	}
}

func TestTheRealSourceOpensNothingUntilAskedTo(t *testing.T) {
	// Making the Source of the real cameras touches no device (no test of this package calls Cameras or Open on it).
	if src := NewV4L2(); src == nil || src.glob == nil || src.open == nil {
		t.Fatal("the real source is not made")
	}
}
