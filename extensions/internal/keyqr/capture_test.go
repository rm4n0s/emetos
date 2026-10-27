package keyqr

import (
	"context"
	"image"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// fastOptions make a capture that notices a cancel in a few milliseconds and gives up on a silent camera in a few more.
var fastOptions = captureOptions{pollEvery: 2 * time.Millisecond, noFramesAfter: 60 * time.Millisecond}

// openFake opens a stream over a fake driver.
func openFake(t *testing.T, ctx context.Context, f *fakeDriver) Stream {
	t.Helper()
	s, err := openStream(ctx, func() (driver, error) { return f, nil }, fastOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func assertRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

func TestTheFormatIsMJPEGWhenTheCameraHasItAndYUYVOtherwise(t *testing.T) {
	const h264 = uint32('H') | '2'<<8 | '6'<<16 | '4'<<24
	cases := []struct {
		name          string
		formats       []uint32
		want          uint32
		width, height int
	}{
		{"both, MJPEG last", []uint32{PixFmtYUYV, PixFmtMJPEG}, PixFmtMJPEG, 1280, 720},
		{"both, MJPEG first", []uint32{PixFmtMJPEG, PixFmtYUYV}, PixFmtMJPEG, 1280, 720},
		{"with others", []uint32{h264, PixFmtYUYV, PixFmtMJPEG}, PixFmtMJPEG, 1280, 720},
		{"YUYV only", []uint32{PixFmtYUYV}, PixFmtYUYV, 640, 480},
		{"YUYV and another", []uint32{h264, PixFmtYUYV}, PixFmtYUYV, 640, 480},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDriver(tc.formats...)
			openFake(t, t.Context(), f)
			if len(f.asked) != 1 || f.asked[0] != (format{PixelFormat: tc.want, Width: tc.width, Height: tc.height}) {
				t.Errorf("the camera was asked for %+v", f.asked)
			}
		})
	}
	// no format that can be used
	f := newFakeDriver(h264, 0x20363532 /* '256 ' */)
	_, err := openStream(t.Context(), func() (driver, error) { return f, nil }, fastOptions)
	assertRoute(t, err, "prepare.KeyQRNoFormat")
	if f.closed != 1 {
		t.Errorf("the device was closed %d times after the failure", f.closed)
	}
}

func TestTheCameraIsSetUpInTheOrderThatStartsIt(t *testing.T) {
	f := newFakeDriver(PixFmtMJPEG)
	s := openFake(t, t.Context(), f)
	want := []string{"QueryCap", "Formats", "SetFormat", "RequestBuffers", "MapBuffer", "MapBuffer", "MapBuffer", "MapBuffer",
		"Queue", "Queue", "Queue", "Queue", "StreamOn"}
	if got := f.callLog(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls %v, want %v", got, want)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	got := f.callLog()
	if tail := got[len(got)-2:]; !reflect.DeepEqual(tail, []string{"StreamOff", "Close"}) {
		t.Errorf("a stream is turned off before the device is closed: %v", got)
	}
	// closing again changes nothing
	n := len(f.callLog())
	if err := s.Close(); err != nil || len(f.callLog()) != n {
		t.Errorf("a second Close: %v, %d calls more", err, len(f.callLog())-n)
	}
}

func TestACameraThatSettlesOnAnotherSizeIsTakenUnlessItIsUnusable(t *testing.T) {
	good := func(pf uint32, w, h int) format {
		return format{PixelFormat: pf, Width: w, Height: h, BytesPerLine: 2 * w, SizeImage: 2 * w * h}
	}
	cases := []struct {
		name   string
		settle func(pf uint32, w, h int) format
		route  string
	}{
		{"the nearest size", func(pf uint32, _, _ int) format { return good(pf, 800, 600) }, ""},
		{"the largest allowed", func(pf uint32, _, _ int) format { return good(pf, 1920, 1080) }, ""},
		{"a size above 1920x1080", func(pf uint32, _, _ int) format { return good(pf, 3840, 2160) }, "prepare.KeyQRFrameTooLarge"},
		{"a width above 1920", func(pf uint32, _, _ int) format { return good(pf, 1922, 480) }, "prepare.KeyQRFrameTooLarge"},
		{"another pixel format", func(_ uint32, w, h int) format { return good(PixFmtYUYV, w, h) }, "prepare.KeyQRFormatRejected"},
		{"no size", func(pf uint32, _, _ int) format { return good(pf, 0, 0) }, "prepare.KeyQRFormatRejected"},
		{"no row length", func(pf uint32, w, h int) format { f := good(pf, w, h); f.BytesPerLine = 0; return f }, "prepare.KeyQRFormatRejected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDriver(PixFmtMJPEG)
			f.settle = tc.settle
			s, err := openStream(t.Context(), func() (driver, error) { return f, nil }, fastOptions)
			if tc.route == "" {
				if err != nil {
					t.Fatal(err)
				}
				_ = s.Close()
				return
			}
			assertRoute(t, err, tc.route)
			if f.closed != 1 || f.streamOn {
				t.Errorf("after the failure the device was closed %d times, streaming %v", f.closed, f.streamOn)
			}
		})
	}
	// a YUYV size that cannot be unpacked: an odd width, a row shorter than its pixels
	for name, fm := range map[string]format{
		"odd width":   {PixelFormat: PixFmtYUYV, Width: 639, Height: 480, BytesPerLine: 1278, SizeImage: 1278 * 480},
		"short rows":  {PixelFormat: PixFmtYUYV, Width: 640, Height: 480, BytesPerLine: 600, SizeImage: 600 * 480},
		"no image":    {PixelFormat: PixFmtYUYV, Width: 640, Height: 480, BytesPerLine: 1280, SizeImage: 0},
		"short mjpeg": {PixelFormat: PixFmtYUYV, Width: 640, Height: 480, BytesPerLine: 0, SizeImage: 1},
	} {
		f := newFakeDriver(PixFmtYUYV)
		f.settle = func(uint32, int, int) format { return fm }
		_, err := openStream(t.Context(), func() (driver, error) { return f, nil }, fastOptions)
		if err == nil {
			t.Errorf("%s: a stream was opened", name)
			continue
		}
		assertRoute(t, err, "prepare.KeyQRFormatRejected")
	}
}

func TestTheBuffersAreCheckedBeforeTheyAreUsed(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *fakeDriver)
	}{
		{"one buffer only", func(f *fakeDriver) { f.buffers = 1 }},
		{"no buffer", func(f *fakeDriver) { f.buffers = -1 }},
		{"more than asked for", func(f *fakeDriver) { f.buffers = 40 }},
		{"a buffer larger than any frame", func(f *fakeDriver) { f.bufLen = MaxFrameBytes + 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDriver(PixFmtMJPEG)
			tc.setup(f)
			_, err := openStream(t.Context(), func() (driver, error) { return f, nil }, fastOptions)
			assertRoute(t, err, "prepare.KeyQRBuffersFailed")
			if f.closed != 1 {
				t.Errorf("the device was closed %d times", f.closed)
			}
		})
	}
}

func TestADeviceThatIsNotACameraIsRefused(t *testing.T) {
	for name, caps := range map[string]capability{
		"metadata":  {Card: "Cam", Capture: false, Streaming: true},
		"no stream": {Card: "Cam", Capture: true, Streaming: false},
	} {
		f := newFakeDriver(PixFmtMJPEG)
		f.caps = caps
		_, err := openStream(t.Context(), func() (driver, error) { return f, nil }, fastOptions)
		assertRoute(t, err, "prepare.KeyQRCameraNotCapture")
		if f.count("SetFormat") != 0 || f.closed != 1 {
			t.Errorf("%s: it went on after the check (%v)", name, f.callLog())
		}
	}
}

func TestWhatTheSystemSaysAtOpenBecomesATag(t *testing.T) {
	cases := []struct {
		name string
		at   string // the call that fails; "open" is the device file
		err  error
		tag  string
	}{
		{"busy at the device file", "open", unix.EBUSY, "KeyQRCameraBusy"},
		{"no permission", "open", unix.EACCES, "KeyQRCameraDenied"},
		{"not permitted", "open", unix.EPERM, "KeyQRCameraDenied"},
		{"no such device", "open", unix.ENODEV, "KeyQRCameraGone"},
		{"the file went", "open", unix.ENOENT, "KeyQRCameraGone"},
		{"busy at the format", "SetFormat", unix.EBUSY, "KeyQRCameraBusy"},
		{"unplugged at the query", "QueryCap", unix.ENODEV, "KeyQRCameraGone"},
		{"an i/o error at the buffers", "RequestBuffers", unix.EIO, "KeyQRCameraGone"},
		{"no memory for the buffers", "RequestBuffers", unix.ENOMEM, "KeyQRCameraOpenFailed"},
		{"an ioctl that is refused", "Formats", unix.EINVAL, "KeyQRCameraOpenFailed"},
		{"a mapping that fails", "MapBuffer", unix.ENOMEM, "KeyQRCameraOpenFailed"},
		{"a queue that fails", "Queue", unix.EINVAL, "KeyQRCameraOpenFailed"},
		{"a stream that does not start", "StreamOn", unix.EPIPE, "KeyQRCameraOpenFailed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDriver(PixFmtMJPEG)
			open := func() (driver, error) { return f, nil }
			if tc.at == "open" {
				open = func() (driver, error) { return nil, tc.err }
			} else {
				f.errs[tc.at] = tc.err
			}
			_, err := openStream(t.Context(), open, fastOptions)
			assertRoute(t, err, "openStream."+tc.tag)
			if tc.at != "open" && (f.closed != 1 || f.streamOn) {
				t.Errorf("the device was closed %d times, streaming %v", f.closed, f.streamOn)
			}
			if appErr, _ := errors.FromError(err); appErr != nil && !errors.Is(err, tc.err) {
				t.Errorf("the cause %v was lost from %v", tc.err, appErr.Message)
			}
		})
	}
}

func TestAnOpenThatIsCancelledOpensNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	opened := false
	_, err := openStream(ctx, func() (driver, error) { opened = true; return nil, nil }, fastOptions)
	if !errors.Is(err, context.Canceled) || opened {
		t.Errorf("err %v, opened %v", err, opened)
	}
	// cancelled in the middle of the setup: the device is let go
	ctx, cancel = context.WithCancel(t.Context())
	f := newFakeDriver(PixFmtMJPEG)
	f.settle = func(pf uint32, w, h int) format {
		cancel()
		return format{PixelFormat: pf, Width: w, Height: h, BytesPerLine: 2 * w, SizeImage: 2 * w * h}
	}
	if _, err = openStream(ctx, func() (driver, error) { return f, nil }, fastOptions); !errors.Is(err, context.Canceled) {
		t.Errorf("err %v", err)
	}
	if f.closed != 1 || f.streamOn {
		t.Errorf("the device was closed %d times, streaming %v", f.closed, f.streamOn)
	}
}

// yuyvFrame is a flat frame of a gray level in the 640x480 the fake settles on.
func yuyvFrame(level byte) []byte {
	return yuyvOf(640, 480, 1280, func(int, int) byte { return level }, func(int, int) byte { return 128 }, func(int, int) byte { return 128 })
}

func levelOf(t *testing.T, img image.Image) byte {
	t.Helper()
	if img.Bounds() != image.Rect(0, 0, 640, 480) {
		t.Fatalf("frame of %v", img.Bounds())
	}
	return img.(*image.YCbCr).Y[0]
}

func TestFramesComeOutAsTheCameraMadeThemAndTheBuffersCycle(t *testing.T) {
	f := newFakeDriver(PixFmtYUYV)
	for i := 0; i < 20; i++ {
		f.give(yuyvFrame(byte(10 + i)))
	}
	s := openFake(t, t.Context(), f)
	for i := 0; i < 20; i++ {
		img, err := s.Next(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if got := levelOf(t, img); got != byte(10+i) {
			t.Fatalf("frame %d has level %d", i, got)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// every buffer that was taken was given back: 4 queued at the start, one back for each of the 20 taken
	if f.maxQueue != 4 || len(f.queued) != 4 || f.dequeues != 20 {
		t.Errorf("at most %d buffers were queued, %d are now, %d were taken", f.maxQueue, len(f.queued), f.dequeues)
	}
	seen := map[int]bool{}
	for _, i := range f.queued {
		seen[i] = true
	}
	if len(seen) != 4 {
		t.Errorf("the buffers queued are %v: a buffer was lost or queued twice", f.queued)
	}
}

func TestOnlyTheNewestFrameIsGivenAndTheOlderOnesGoBack(t *testing.T) {
	f := newFakeDriver(PixFmtYUYV)
	f.perWait = 3 // three frames came while the reader was busy
	f.give(yuyvFrame(1), yuyvFrame(2), yuyvFrame(3), yuyvFrame(4))
	s := openFake(t, t.Context(), f)
	img, err := s.Next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := levelOf(t, img); got != 3 {
		t.Errorf("level %d: the newest of the three was not given", got)
	}
	f.mu.Lock()
	n := len(f.queued)
	f.mu.Unlock()
	if n != 4 { // the 2 older frames went back at once, the newest after it was converted, and one was never filled
		t.Errorf("%d buffers are queued after the frames were taken", n)
	}
	img, err = s.Next(t.Context())
	if err != nil || levelOf(t, img) != 4 {
		t.Errorf("the next frame: %v, %v", img, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queued) != 4 {
		t.Errorf("%d buffers are queued at the end: one is lost", len(f.queued))
	}
}

func TestMJPEGFramesAreDecodedWithOrWithoutTables(t *testing.T) {
	f := newFakeDriver(PixFmtMJPEG)
	f.settle = func(pf uint32, _, _ int) format {
		return format{PixelFormat: pf, Width: 1280, Height: 720, BytesPerLine: 0 + 1, SizeImage: 1 << 20}
	}
	whole := jpegOf(t, testPicture(64, 48))
	f.give(whole, withoutTables(t, whole))
	s := openFake(t, t.Context(), f)
	for i := 0; i < 2; i++ {
		img, err := s.Next(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds() != image.Rect(0, 0, 64, 48) {
			t.Errorf("frame %d is %v", i, img.Bounds())
		}
	}
}

func TestADamagedFrameIsSkippedAndTooManyEndTheCapture(t *testing.T) {
	f := newFakeDriver(PixFmtYUYV)
	f.damage = 2
	f.give(yuyvFrame(1), yuyvFrame(2), yuyvFrame(3)) // the first two carry the error flag of the camera
	s := openFake(t, t.Context(), f)
	img, err := s.Next(t.Context())
	if err != nil || levelOf(t, img) != 3 {
		t.Fatalf("the first good frame: %v, %v", img, err)
	}
	// a frame that is too short for its size is damaged too
	f.give([]byte{1, 2, 3}, yuyvFrame(9))
	if img, err = s.Next(t.Context()); err != nil || levelOf(t, img) != 9 {
		t.Fatalf("after a short frame: %v, %v", img, err)
	}
	// nothing but damaged frames
	for i := 0; i <= maxBadFrames+1; i++ {
		f.give([]byte{1})
	}
	_, err = s.Next(t.Context())
	assertRoute(t, err, "captureStream.next.KeyQRTooManyBadFrames")
}

func TestACancelledReaderLeavesAtOnceAndTheCameraIsLetGoWhenClosed(t *testing.T) {
	f := newFakeDriver(PixFmtYUYV) // no frames: Next waits
	s := openFake(t, t.Context(), f)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := s.Next(ctx); done <- err }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		assertRoute(t, err, "captureStream.next.KeyQRCancelled")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the cause is lost from %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not return after the cancel")
	}
	if f.closed != 0 {
		t.Error("the camera was released by a reader that gave up: it belongs to the stream")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if f.closed != 1 || f.streamOn {
		t.Errorf("closed %d times, streaming %v", f.closed, f.streamOn)
	}
	_, err := s.Next(t.Context())
	assertRoute(t, err, "captureStream.next.KeyQRStreamClosed")
}

func TestClosingWakesAReaderThatWaits(t *testing.T) {
	f := newFakeDriver(PixFmtYUYV)
	f.waitTime = time.Millisecond
	s, err := openStream(t.Context(), func() (driver, error) { return f, nil }, captureOptions{pollEvery: time.Millisecond, noFramesAfter: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Next(t.Context()); done <- err }()
	time.Sleep(20 * time.Millisecond)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		assertRoute(t, err, "captureStream.next.KeyQRStreamClosed")
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not return after Close")
	}
	if f.closed != 1 {
		t.Errorf("closed %d times", f.closed)
	}
}

func TestTheCameraIsReleasedWhenTheContextOfTheStreamEnds(t *testing.T) {
	f := newFakeDriver(PixFmtYUYV)
	ctx, cancel := context.WithCancel(t.Context())
	s := openFake(t, ctx, f)
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		closed, on := f.closed, f.streamOn
		f.mu.Unlock()
		if closed == 1 && !on {
			_, err := s.Next(t.Context())
			assertRoute(t, err, "captureStream.next.KeyQRStreamClosed")
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the camera was not released when the context ended")
}

func TestACameraThatGivesNoPicturesIsReported(t *testing.T) {
	f := newFakeDriver(PixFmtYUYV)
	s := openFake(t, t.Context(), f)
	start := time.Now()
	_, err := s.Next(t.Context())
	assertRoute(t, err, "captureStream.next.KeyQRNoFrames")
	if took := time.Since(start); took < fastOptions.noFramesAfter || took > 2*time.Second {
		t.Errorf("gave up after %v", took)
	}
}

func TestAFailureOfTheCameraDuringTheCaptureHasItsTag(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *fakeDriver)
		route string
	}{
		{"unplugged while waiting", func(f *fakeDriver) { f.errs["Wait"] = unix.ENODEV }, "captureStream.Next.KeyQRCameraGone"},
		{"an i/o error while taking a frame", func(f *fakeDriver) { f.errs["Dequeue"] = unix.EIO; f.give(yuyvFrame(1)) }, "captureStream.Next.KeyQRCameraGone"},
		{"an ioctl that fails", func(f *fakeDriver) { f.errs["Dequeue"] = unix.EINVAL; f.give(yuyvFrame(1)) }, "captureStream.Next.KeyQRCaptureFailed"},
		{"poll that fails", func(f *fakeDriver) { f.errs["Wait"] = unix.EBADF }, "captureStream.Next.KeyQRCaptureFailed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDriver(PixFmtYUYV)
			tc.setup(f)
			s := openFake(t, t.Context(), f)
			_, err := s.Next(t.Context())
			assertRoute(t, err, tc.route)
		})
	}
}

func TestACameraThatGivesBackAnUnknownBufferIsAFailure(t *testing.T) {
	f := newFakeDriver(PixFmtYUYV)
	s := openFake(t, t.Context(), f)
	f.mu.Lock()
	f.filled = append(f.filled, dequeued{Index: 7, BytesUsed: 10})
	f.mu.Unlock()
	_, err := s.Next(t.Context())
	assertRoute(t, err, "captureStream.next->captureStream.newest.KeyQRCaptureFailed")
}

func TestAFailureToGiveABufferBackIsAFailure(t *testing.T) {
	f := newFakeDriver(PixFmtYUYV)
	f.give(yuyvFrame(1))
	s := openFake(t, t.Context(), f)
	f.mu.Lock()
	f.errs["Queue"] = unix.EINVAL
	f.mu.Unlock()
	_, err := s.Next(t.Context())
	assertRoute(t, err, "captureStream.Next.KeyQRCaptureFailed")
}

func TestAFailureToCloseTheDeviceIsReported(t *testing.T) {
	f := newFakeDriver(PixFmtYUYV)
	f.errs["Close"] = unix.EIO
	s, err := openStream(t.Context(), func() (driver, error) { return f, nil }, fastOptions)
	if err != nil {
		t.Fatal(err)
	}
	assertRoute(t, s.Close(), "captureStream.Close.KeyQRCaptureFailed")
	// the stream going off at a camera that is gone is not an error
	g := newFakeDriver(PixFmtYUYV)
	g.errs["StreamOff"] = unix.ENODEV
	s, err = openStream(t.Context(), func() (driver, error) { return g, nil }, fastOptions)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// but another failure of StreamOff is
	h := newFakeDriver(PixFmtYUYV)
	h.errs["StreamOff"] = unix.EINVAL
	s, err = openStream(t.Context(), func() (driver, error) { return h, nil }, fastOptions)
	if err != nil {
		t.Fatal(err)
	}
	assertRoute(t, s.Close(), "captureStream.Close.KeyQRCaptureFailed")
}

func TestNextAndCloseMayRaceWithoutHarm(t *testing.T) {
	f := newFakeDriver(PixFmtYUYV)
	for i := 0; i < 200; i++ {
		f.give(yuyvFrame(byte(i)))
	}
	s, err := openStream(t.Context(), func() (driver, error) { return f, nil }, captureOptions{pollEvery: time.Millisecond, noFramesAfter: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			if _, err := s.Next(t.Context()); err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		_ = s.Close()
	}()
	wg.Wait()
	if f.closed != 1 {
		t.Errorf("closed %d times", f.closed)
	}
}
