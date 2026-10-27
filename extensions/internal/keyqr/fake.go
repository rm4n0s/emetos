package keyqr

import (
	"context"
	"image"
	"strconv"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// FakeCamera is a camera that plays pictures from memory.
type FakeCamera struct {
	// Name is what the camera calls itself.
	Name string
	// Frames are given one for each call of Next, in order; the last one is given again and again once they are all played. A
	// camera with no frames gives nothing and Next waits for its context.
	Frames []image.Image
	// Delay is how long each Next takes, as a camera at a frame rate does; none when zero.
	Delay time.Duration
	// OpenErr makes Open fail with this error, as a camera that is in use does.
	OpenErr error
}

// FakeSource is a Source of cameras that play frames from memory, for the tests of what uses a camera and for the proofs (the dev
// command dev.keyqr.fakeCamera). Its methods may be called from any goroutine.
type FakeSource struct {
	mu      sync.Mutex
	cameras []FakeCamera
	listErr error
	opened  int
	active  int
	closed  int
	last    string
}

// NewFakeSource makes a Source with the cameras; the ID of the n-th (from 0) is "fake:n".
func NewFakeSource(cameras ...FakeCamera) *FakeSource { return &FakeSource{cameras: cameras} }

// SetListError makes Cameras fail with err (nil makes it work again).
func (f *FakeSource) SetListError(err error) {
	f.mu.Lock()
	f.listErr = err
	f.mu.Unlock()
}

// Opened is how many times a camera was opened.
func (f *FakeSource) Opened() int { f.mu.Lock(); defer f.mu.Unlock(); return f.opened }

// Active is how many of the cameras that were opened are open now.
func (f *FakeSource) Active() int { f.mu.Lock(); defer f.mu.Unlock(); return f.active }

// Released is how many of the cameras that were opened were let go.
func (f *FakeSource) Released() int { f.mu.Lock(); defer f.mu.Unlock(); return f.closed }

// LastOpened is the ID of the camera that was opened last, or "".
func (f *FakeSource) LastOpened() string { f.mu.Lock(); defer f.mu.Unlock(); return f.last }

// Cameras implements Source.
func (f *FakeSource) Cameras(context.Context) ([]CameraInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	cams := make([]CameraInfo, len(f.cameras))
	for i, c := range f.cameras {
		cams[i] = CameraInfo{ID: fakeID(i), Name: c.Name}
	}
	return cams, nil
}

func fakeID(i int) string { return "fake:" + strconv.Itoa(i) }

// Open implements Source. It fails with KeyQRCameraGone for an ID that is not one of the cameras, and with the camera's OpenErr.
func (f *FakeSource) Open(ctx context.Context, id string) (Stream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.cameras {
		if fakeID(i) != id {
			continue
		}
		if c.OpenErr != nil {
			return nil, c.OpenErr
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f.opened++
		f.active++
		f.last = id
		s := &fakeStream{src: f, cam: c, stop: make(chan struct{})}
		s.unwatch = context.AfterFunc(ctx, func() { _ = s.Close() })
		return s, nil
	}
	return nil, errors.New("KeyQRCameraGone", "there is no such camera", "camera", id)
}

// fakeStream is an open FakeCamera.
type fakeStream struct {
	src     *FakeSource
	cam     FakeCamera
	stop    chan struct{}
	unwatch func() bool

	mu     sync.Mutex
	next   int
	closed bool
}

// Next implements Stream.
func (s *fakeStream) Next(ctx context.Context) (image.Image, error) {
	if s.cam.Delay > 0 {
		timer := time.NewTimer(s.cam.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-s.stop:
		case <-timer.C:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("KeyQRStreamClosed", "the camera was turned off")
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("KeyQRCancelled", err)
	}
	if len(s.cam.Frames) == 0 {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-s.stop:
		}
		s.mu.Lock()
		if s.closed {
			return nil, errors.New("KeyQRStreamClosed", "the camera was turned off")
		}
		return nil, errors.NewErr("KeyQRCancelled", ctx.Err())
	}
	frame := s.cam.Frames[min(s.next, len(s.cam.Frames)-1)]
	s.next++
	return frame, nil
}

// Close implements Stream.
func (s *fakeStream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.stop)
	s.mu.Unlock()
	if s.unwatch != nil {
		s.unwatch()
	}
	s.src.mu.Lock()
	s.src.active--
	s.src.closed++
	s.src.mu.Unlock()
	return nil
}

// Scene is a picture a camera could show of a room with nothing held up to it: a gradient with a window. It is the same in every
// call, for the tests and the proofs that need a camera's picture. It is at most MaxFrameWidth by MaxFrameHeight.
func Scene(width, height int) *image.RGBA {
	width, height = min(max(width, 16), MaxFrameWidth), min(max(height, 16), MaxFrameHeight)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			o := img.PixOffset(x, y)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = uint8(60+100*y/height), uint8(70+80*x/width), uint8(110+60*(width-x)/width), 255
		}
	}
	for y := height / 8; y < height/2; y++ {
		for x := width * 5 / 8; x < width*7/8; x++ {
			o := img.PixOffset(x, y)
			img.Pix[o], img.Pix[o+1], img.Pix[o+2] = 235, 240, 245
		}
	}
	return img
}

// SceneWithCode is Scene with a sheet of paper in the middle that has the QR code of the text on it, as a person holds one up to
// a camera. It fails with KeyQREncodeFailed for text that does not fit in a code.
func SceneWithCode(text string, width, height int) (image.Image, error) {
	frame := Scene(width, height)
	side := frame.Bounds().Dy() * 3 / 4
	code, err := EncodeQR(text, side)
	if err != nil {
		return nil, err
	}
	b := code.Bounds()
	left, top := (frame.Bounds().Dx()-b.Dx())/2, (frame.Bounds().Dy()-b.Dy())/2
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			v := code.GrayAt(x, y).Y
			o := frame.PixOffset(left+x, top+y)
			frame.Pix[o], frame.Pix[o+1], frame.Pix[o+2] = v, v, v
		}
	}
	return frame, nil
}
