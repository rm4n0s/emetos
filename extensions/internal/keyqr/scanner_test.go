package keyqr

import (
	"context"
	"image"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// blank is a frame with nothing in it.
func blank() image.Image { return frameWith(image.NewGray(image.Rect(0, 0, 8, 8)), 640, 480, 120) }

// codeFrame is a frame of a camera with the code of a line in it.
func codeFrame(t testing.TB, line string) image.Image {
	t.Helper()
	code, _, err := EncodeKey(line, 300)
	if err != nil {
		t.Fatal(err)
	}
	return frameWith(scaledTo(code, 320), 640, 480, 140)
}

func TestScanShowsEveryFrameAndReadsTheCode(t *testing.T) {
	line := testLines(t)["ed25519"]
	src := NewFakeSource(FakeCamera{Name: "Cam", Frames: []image.Image{blank(), blank(), codeFrame(t, line), blank()}})
	stream, err := src.Open(t.Context(), "fake:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var mu sync.Mutex
	var frames int
	var texts []string
	err = Scan(ctx, stream, ScanOptions{
		Interval: time.Millisecond,
		OnFrame: func(img image.Image) {
			mu.Lock()
			frames++
			mu.Unlock()
		},
		OnCode: func(text string) {
			mu.Lock()
			texts = append(texts, text)
			n := frames
			mu.Unlock()
			if n != 3 {
				t.Errorf("the code was read at frame %d, not 3", n)
			}
			cancel()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(texts) != 1 || texts[0] != line || frames != 3 {
		t.Errorf("texts %q after %d frames", texts, frames)
	}
	if src.Active() != 1 {
		t.Error("Scan closed the stream, which belongs to the caller")
	}
}

func TestScanLooksAtAboutTenFramesASecond(t *testing.T) {
	src := NewFakeSource(FakeCamera{Name: "Cam", Frames: []image.Image{blank()}})
	stream, err := src.Open(t.Context(), "fake:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 1050*time.Millisecond)
	defer cancel()
	var frames atomic.Int32
	if err := Scan(ctx, stream, ScanOptions{OnFrame: func(image.Image) { frames.Add(1) }}); err != nil {
		t.Fatal(err)
	}
	// one at once, then one every 100 ms: 11 in a second, with the slack of a loaded machine
	if n := frames.Load(); n < 8 || n > 12 {
		t.Errorf("%d frames in a second, want about 10", n)
	}
}

func TestAFrameThatIsSlowToReadDropsTheOnesBehindIt(t *testing.T) {
	// a camera at about 200 frames a second, a reader that takes 40 ms for a frame, an interval of 10 ms: the reader is the limit,
	// and the frames it takes are the newest, so the numbers it sees skip
	var frames []image.Image
	for i := 0; i < 400; i++ {
		frames = append(frames, image.NewGray(image.Rect(0, 0, 1+i%400, 1)))
	}
	src := NewFakeSource(FakeCamera{Name: "Cam", Frames: frames, Delay: 5 * time.Millisecond})
	stream, err := src.Open(t.Context(), "fake:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 400*time.Millisecond)
	defer cancel()
	var seen int
	err = Scan(ctx, stream, ScanOptions{Interval: 10 * time.Millisecond, OnFrame: func(image.Image) { seen++; time.Sleep(40 * time.Millisecond) }})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 4 || seen > 11 {
		t.Errorf("a reader that takes 40 ms looked at %d frames in 400 ms", seen)
	}
}

// failingStream is a camera that gives frames and then fails.
type failingStream struct {
	frames int
	err    error
}

func (s *failingStream) Next(context.Context) (image.Image, error) {
	if s.frames == 0 {
		return nil, s.err
	}
	s.frames--
	return blank(), nil
}
func (s *failingStream) Close() error { return nil }

func TestScanReturnsTheErrorOfACameraThatFailsAndNilWhenItIsCancelled(t *testing.T) {
	// a camera that goes away while scanning
	f := newFakeDriver(PixFmtYUYV)
	f.give(yuyvFrame(1), yuyvFrame(2))
	stream, err := openStream(t.Context(), func() (driver, error) { return f, nil }, fastOptions)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	var frames int
	err = Scan(t.Context(), stream, ScanOptions{Interval: time.Millisecond, OnFrame: func(image.Image) {
		frames++
		if frames == 2 {
			f.mu.Lock()
			f.errs["Wait"] = unix.ENODEV
			f.mu.Unlock()
		}
	}})
	assertRoute(t, err, "captureStream.Next.KeyQRCameraGone")
	if frames != 2 {
		t.Errorf("%d frames before the failure", frames)
	}
	// a plain failing stream
	if err := Scan(t.Context(), &failingStream{frames: 1, err: errors.New("KeyQRCaptureFailed", "x")}, ScanOptions{Interval: time.Millisecond}); err == nil {
		t.Error("the failure was lost")
	}
	// cancelled while waiting for the interval, and while waiting for a frame
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Scan(ctx, &failingStream{frames: 1000}, ScanOptions{Interval: time.Hour}) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("cancelled: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Scan did not return after the cancel")
	}
	src := NewFakeSource(FakeCamera{Name: "Silent"})
	silent, err := src.Open(t.Context(), "fake:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel2()
	if err := Scan(ctx2, silent, ScanOptions{}); err != nil {
		t.Errorf("a silent camera, cancelled: %v", err)
	}
	_ = silent.Close()
}

func TestAFakeCameraPlaysItsFramesAndRepeatsTheLast(t *testing.T) {
	a, b := image.NewGray(image.Rect(0, 0, 1, 1)), image.NewGray(image.Rect(0, 0, 2, 2))
	src := NewFakeSource(FakeCamera{Name: "One", Frames: []image.Image{a, b}}, FakeCamera{Name: "Two"}, FakeCamera{Name: "Busy", OpenErr: unix.EBUSY})
	cams, err := src.Cameras(t.Context())
	if err != nil || len(cams) != 3 || cams[0] != (CameraInfo{ID: "fake:0", Name: "One"}) || cams[2].Name != "Busy" {
		t.Fatalf("cameras %+v, %v", cams, err)
	}
	s, err := src.Open(t.Context(), "fake:0")
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []image.Image{a, b, b, b} {
		got, err := s.Next(t.Context())
		if err != nil || got != want {
			t.Fatalf("frame %d: %v, %v", i, got, err)
		}
	}
	if src.Opened() != 1 || src.Active() != 1 || src.Released() != 0 || src.LastOpened() != "fake:0" {
		t.Errorf("opened %d, active %d, released %d, last %q", src.Opened(), src.Active(), src.Released(), src.LastOpened())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil || src.Active() != 0 || src.Released() != 1 {
		t.Errorf("after Close twice: %v, active %d, released %d", err, src.Active(), src.Released())
	}
	_, err = s.Next(t.Context())
	assertRoute(t, err, "fakeStream.Next.KeyQRStreamClosed")
	// an unknown camera, a camera that is in use, a list that fails
	_, err = src.Open(t.Context(), "fake:9")
	assertRoute(t, err, "FakeSource.Open.KeyQRCameraGone")
	if _, err := src.Open(t.Context(), "fake:2"); !errors.Is(err, unix.EBUSY) {
		t.Errorf("a camera that is in use: %v", err)
	}
	src.SetListError(unix.EIO)
	if _, err := src.Cameras(t.Context()); !errors.Is(err, unix.EIO) {
		t.Errorf("a list that fails: %v", err)
	}
	src.SetListError(nil)
	if cams, err := src.Cameras(t.Context()); err != nil || len(cams) != 3 {
		t.Errorf("a list that works again: %v, %v", cams, err)
	}
	// a cancelled context opens nothing
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := src.Open(ctx, "fake:0"); !errors.Is(err, context.Canceled) || src.Opened() != 1 {
		t.Errorf("a cancelled open: %v", err)
	}
}

func TestAFakeCameraIsReleasedWithItsContextAndCancelsItsReader(t *testing.T) {
	src := NewFakeSource(FakeCamera{Name: "Slow", Frames: []image.Image{blank()}, Delay: time.Hour}, FakeCamera{Name: "Silent"})
	ctx, cancel := context.WithCancel(t.Context())
	s, err := src.Open(ctx, "fake:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Next(t.Context()); done <- err }()
	time.Sleep(10 * time.Millisecond)
	cancel() // the camera goes with the context, and a reader that waits is woken
	select {
	case err := <-done:
		assertRoute(t, err, "fakeStream.Next.KeyQRStreamClosed")
	case <-time.After(2 * time.Second):
		t.Fatal("the reader was not woken")
	}
	deadline := time.Now().Add(2 * time.Second)
	for src.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if src.Active() != 0 || src.Released() != 1 {
		t.Errorf("active %d, released %d", src.Active(), src.Released())
	}
	// a camera with no frames waits for the context of its reader
	silent, err := src.Open(t.Context(), "fake:1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = silent.Close() }()
	rctx, rcancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer rcancel()
	_, err = silent.Next(rctx)
	assertRoute(t, err, "fakeStream.Next.KeyQRCancelled")
	// ...and is woken by Close
	go func() { _, err := silent.Next(t.Context()); done <- err }()
	time.Sleep(10 * time.Millisecond)
	_ = silent.Close()
	select {
	case err := <-done:
		assertRoute(t, err, "fakeStream.Next.KeyQRStreamClosed")
	case <-time.After(2 * time.Second):
		t.Fatal("the reader of the silent camera was not woken")
	}
}
