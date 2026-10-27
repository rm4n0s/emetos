package keyqr

import (
	"context"
	"image"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// The pixel formats of V4L2 (four letters, little endian) the layer takes, in the order it prefers them.
const (
	// PixFmtMJPEG is V4L2_PIX_FMT_MJPEG, 'MJPG': one JPEG for every frame.
	PixFmtMJPEG uint32 = 'M' | 'J'<<8 | 'P'<<16 | 'G'<<24
	// PixFmtYUYV is V4L2_PIX_FMT_YUYV, 'YUYV': packed 4:2:2.
	PixFmtYUYV uint32 = 'Y' | 'U'<<8 | 'Y'<<16 | 'V'<<24
)

// The sizes asked of a camera: MJPEG is compressed, so a camera gives 1280x720 at 30 frames a second over USB 2, and a code of
// an RSA 4096 key (113 modules a side) needs it; YUYV is not, so 640x480 is what USB 2 carries at a good rate. A camera that has
// not the size gives the nearest it has.
const (
	mjpegWidth, mjpegHeight = 1280, 720
	yuyvWidth, yuyvHeight   = 640, 480
)

// The knobs of a capture, with their defaults.
const (
	// wantBuffers is how many buffers are asked for: with 4 the camera always has one to fill while a frame is converted.
	wantBuffers = 4
	// minBuffers is the fewest the capture works with.
	minBuffers = 2
	// pollEvery is how long the capture waits for a frame before it looks at the context and the camera again, so that a
	// cancel is answered within this time.
	pollEvery = 50 * time.Millisecond
	// noFramesAfter is how long a camera may give no frame before it counts as broken (the first frame of a camera that is
	// starting can take a second).
	noFramesAfter = 5 * time.Second
	// maxBadFrames is how many damaged frames in a row end the capture; a damaged frame (a USB hiccup) is skipped.
	maxBadFrames = 30
)

// capability is what a device says it is.
type capability struct {
	// Card is the name of the camera.
	Card string
	// Capture is true for a single-plane video capture device and Streaming for one that streams with buffers.
	Capture, Streaming bool
}

// format is what the camera agreed to.
type format struct {
	PixelFormat   uint32
	Width, Height int
	// BytesPerLine and SizeImage are the camera's numbers for a frame (the rows may be padded).
	BytesPerLine, SizeImage int
}

// dequeued is a buffer the camera filled.
type dequeued struct {
	Index, BytesUsed int
	// Damaged is true when the camera marked the frame as having an error.
	Damaged bool
}

// driver is the kernel's side of a camera: what V4L2 does with ioctls, mmap and poll. The real one (v4l2_linux.go) is the thin
// shell over those calls, and its methods return the errors of the system (unix.Errno) as they are; everything that decides
// something (the format, the number of buffers, what an error means) is above it, in this file, and is tested with a fake.
type driver interface {
	// QueryCap says what the device is (VIDIOC_QUERYCAP).
	QueryCap() (capability, error)
	// Formats lists the pixel formats the device captures (VIDIOC_ENUM_FMT until it says there are no more).
	Formats() ([]uint32, error)
	// SetFormat asks for a pixel format and a size and returns what the device settled on (VIDIOC_S_FMT).
	SetFormat(pixelFormat uint32, width, height int) (format, error)
	// RequestBuffers asks for n buffers the device and the program share and returns how many there are (VIDIOC_REQBUFS).
	RequestBuffers(n int) (int, error)
	// MapBuffer maps buffer i into memory (VIDIOC_QUERYBUF and mmap); the driver unmaps it when it is closed.
	MapBuffer(i int) ([]byte, error)
	// Queue gives buffer i to the device to fill (VIDIOC_QBUF).
	Queue(i int) error
	// Dequeue takes a buffer the device filled (VIDIOC_DQBUF); it returns unix.EAGAIN when there is none.
	Dequeue() (dequeued, error)
	// StreamOn and StreamOff start and stop the capture.
	StreamOn() error
	StreamOff() error
	// Wait blocks until a frame is ready or the time passes (poll); it reports whether one is.
	Wait(timeout time.Duration) (bool, error)
	// Close unmaps the buffers and closes the device.
	Close() error
}

// captureOptions are the knobs of a capture, which the tests shorten.
type captureOptions struct {
	pollEvery, noFramesAfter time.Duration
}

func (o captureOptions) withDefaults() captureOptions {
	if o.pollEvery <= 0 {
		o.pollEvery = pollEvery
	}
	if o.noFramesAfter <= 0 {
		o.noFramesAfter = noFramesAfter
	}
	return o
}

// captureStream is a camera that streams through a driver. It implements Stream.
type captureStream struct {
	drv  driver
	opts captureOptions
	fmt  format
	bufs [][]byte

	// mu is held by Next while it works with the buffers and by Close while it releases them.
	mu     sync.Mutex
	closed bool
	// stop is closed by Close, to wake a Next that waits.
	stop     chan struct{}
	stopOnce sync.Once
	closeErr error
	unwatch  func() bool
}

// openStream opens the device (open gives the driver of it), makes it ready and starts it: it checks the device, chooses the
// format (MJPEG when the camera has it, else YUYV), sets the size, maps the buffers, queues them and turns the capture on. On any
// failure the driver is closed. The camera is released when ctx ends. It fails with KeyQRCameraNotCapture, KeyQRNoFormat,
// KeyQRFormatRejected, KeyQRFrameTooLarge and KeyQRBuffersFailed for what the camera is, and with KeyQRCameraBusy,
// KeyQRCameraDenied, KeyQRCameraGone and KeyQRCameraOpenFailed for what the system said.
func openStream(ctx context.Context, open func() (driver, error), opts captureOptions) (Stream, error) {
	s, drv, err := prepare(ctx, open, opts.withDefaults())
	if err == nil {
		return s, nil
	}
	if drv != nil {
		_ = drv.Close() // the failure is the one that matters; a device that failed to close has nothing more to tell
	}
	if _, tagged := errors.FromError(err); tagged || ctx.Err() != nil {
		return nil, err
	}
	switch {
	case errors.Is(err, unix.EBUSY):
		return nil, errors.NewErr("KeyQRCameraBusy", err).SetMessage("the camera is in use by another program")
	case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM):
		return nil, errors.NewErr("KeyQRCameraDenied", err).SetMessage("this user may not use the camera (is it in the video group?)")
	case errors.Is(err, unix.ENODEV), errors.Is(err, unix.ENOENT), errors.Is(err, unix.ENXIO), errors.Is(err, unix.EIO):
		return nil, errors.NewErr("KeyQRCameraGone", err).SetMessage("the camera is not there any more")
	}
	return nil, errors.NewErr("KeyQRCameraOpenFailed", err)
}

// prepare does the steps of openStream. The errors of the driver come back as they are, and the ones that say what the camera is
// come tagged. It returns the driver as soon as there is one, so that the caller can close it.
func prepare(ctx context.Context, open func() (driver, error), opts captureOptions) (*captureStream, driver, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	drv, err := open()
	if err != nil {
		return nil, nil, err
	}
	caps, err := drv.QueryCap()
	if err != nil {
		return nil, drv, err
	}
	if !caps.Capture || !caps.Streaming {
		return nil, drv, errors.New("KeyQRCameraNotCapture", "the device is not a camera that streams video", "name", caps.Card)
	}
	have, err := drv.Formats()
	if err != nil {
		return nil, drv, err
	}
	want, w, h := chooseFormat(have)
	if want == 0 {
		return nil, drv, errors.New("KeyQRNoFormat", "the camera gives neither MJPEG nor YUYV", "formats", have)
	}
	got, err := drv.SetFormat(want, w, h)
	if err != nil {
		return nil, drv, err
	}
	switch {
	case got.PixelFormat != want || got.Width <= 0 || got.Height <= 0 || got.BytesPerLine <= 0 || got.SizeImage <= 0:
		return nil, drv, errors.New("KeyQRFormatRejected", "the camera did not take the format that was asked for", "asked", want, "got", got.PixelFormat)
	case got.Width > MaxFrameWidth || got.Height > MaxFrameHeight:
		return nil, drv, errors.New("KeyQRFrameTooLarge", "the camera's frames are larger than 1920x1080", "width", got.Width, "height", got.Height)
	case want == PixFmtYUYV && (got.Width%2 != 0 || got.BytesPerLine < 2*got.Width):
		return nil, drv, errors.New("KeyQRFormatRejected", "the camera's YUYV frames have a size that does not fit", "width", got.Width, "bytesPerLine", got.BytesPerLine)
	}
	if err := ctx.Err(); err != nil {
		return nil, drv, err
	}
	n, err := drv.RequestBuffers(wantBuffers)
	if err != nil {
		return nil, drv, err
	}
	if n < minBuffers || n > wantBuffers {
		return nil, drv, errors.New("KeyQRBuffersFailed", "the camera has too few buffers to stream", "buffers", n)
	}
	s := &captureStream{drv: drv, opts: opts, fmt: got, stop: make(chan struct{})}
	for i := 0; i < n; i++ {
		b, err := drv.MapBuffer(i)
		if err != nil {
			return nil, drv, err
		}
		if len(b) == 0 || len(b) > MaxFrameBytes {
			return nil, drv, errors.New("KeyQRBuffersFailed", "a buffer of the camera has a size that is not usable", "buffer", i, "bytes", len(b))
		}
		s.bufs = append(s.bufs, b)
	}
	for i := range s.bufs {
		if err := drv.Queue(i); err != nil {
			return nil, drv, err
		}
	}
	if err := drv.StreamOn(); err != nil {
		return nil, drv, err
	}
	// the camera is released when the context ends, whoever else holds the stream
	s.unwatch = context.AfterFunc(ctx, func() { _ = s.Close() })
	return s, drv, nil
}

// chooseFormat picks the pixel format and the size to ask for from the formats the camera has: MJPEG, else YUYV, else none (0).
func chooseFormat(have []uint32) (format uint32, width, height int) {
	for _, f := range have {
		if f == PixFmtMJPEG {
			return PixFmtMJPEG, mjpegWidth, mjpegHeight
		}
	}
	for _, f := range have {
		if f == PixFmtYUYV {
			return PixFmtYUYV, yuyvWidth, yuyvHeight
		}
	}
	return 0, 0, 0
}

// Next implements Stream. What the driver says when it fails comes back tagged: KeyQRCameraGone for a camera that is not there any
// more, KeyQRCaptureFailed for the rest; the other tags are those of Stream.Next.
func (s *captureStream) Next(ctx context.Context) (image.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	img, err := s.next(ctx)
	if err == nil {
		return img, nil
	}
	if _, tagged := errors.FromError(err); tagged {
		return nil, err
	}
	switch {
	case errors.Is(err, unix.ENODEV), errors.Is(err, unix.ENXIO), errors.Is(err, unix.EIO), errors.Is(err, unix.EPIPE):
		return nil, errors.NewErr("KeyQRCameraGone", err).SetMessage("the camera is not there any more")
	}
	return nil, errors.NewErr("KeyQRCaptureFailed", err)
}

// next waits for a frame and converts it. The errors of the driver come back as they are. The caller holds s.mu.
func (s *captureStream) next(ctx context.Context) (image.Image, error) {
	bad := 0
	var lastBad error
	waited := time.Now()
	for {
		select {
		case <-s.stop:
			return nil, errors.New("KeyQRStreamClosed", "the camera was turned off")
		default:
		}
		if err := ctx.Err(); err != nil {
			return nil, errors.NewErr("KeyQRCancelled", err)
		}
		ready, err := s.drv.Wait(s.opts.pollEvery)
		if err != nil {
			return nil, err
		}
		if !ready {
			if time.Since(waited) > s.opts.noFramesAfter {
				return nil, errors.New("KeyQRNoFrames", "the camera gives no pictures", "seconds", s.opts.noFramesAfter.Seconds())
			}
			continue
		}
		waited = time.Now()
		buf, err := s.newest()
		if err != nil {
			return nil, err
		}
		if buf.Index < 0 {
			continue // the frame was taken by another wake-up
		}
		var img image.Image
		if buf.Damaged {
			err = errors.New("KeyQRFrameInvalid", "the camera marked the frame as damaged")
		} else {
			img, err = s.convert(s.bufs[buf.Index][:min(buf.BytesUsed, len(s.bufs[buf.Index]))])
		}
		// the image is the caller's own memory, so the buffer goes back to the camera at once
		if qerr := s.drv.Queue(buf.Index); qerr != nil {
			return nil, qerr
		}
		if err != nil {
			if bad++; bad > maxBadFrames {
				return nil, errors.New("KeyQRTooManyBadFrames", "the camera gave nothing but damaged frames", "frames", bad, "last", lastBad)
			}
			lastBad = err
			continue
		}
		return img, nil
	}
}

// newest takes every frame the camera has ready, gives back all but the last and returns the last (Index -1 when there was none).
func (s *captureStream) newest() (dequeued, error) {
	newest := dequeued{Index: -1}
	for {
		d, err := s.drv.Dequeue()
		if errors.Is(err, unix.EAGAIN) {
			return newest, nil
		}
		if err != nil {
			return dequeued{Index: -1}, err
		}
		if d.Index < 0 || d.Index >= len(s.bufs) {
			return dequeued{Index: -1}, errors.New("KeyQRCaptureFailed", "the camera returned a buffer that does not exist", "index", d.Index)
		}
		if newest.Index >= 0 {
			if err := s.drv.Queue(newest.Index); err != nil { // an older frame that nobody wanted: dropped
				return dequeued{Index: -1}, err
			}
		}
		newest = d
	}
}

// convert makes an image of the bytes of a frame in the format of the capture.
func (s *captureStream) convert(data []byte) (image.Image, error) {
	if s.fmt.PixelFormat == PixFmtYUYV {
		return YUYVToImage(data, s.fmt.Width, s.fmt.Height, s.fmt.BytesPerLine)
	}
	return DecodeMJPEG(data)
}

// Close implements Stream: it turns the capture off, unmaps the buffers and closes the device.
func (s *captureStream) Close() error {
	s.stopOnce.Do(func() { close(s.stop) })
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	if s.unwatch != nil {
		s.unwatch()
	}
	offErr := s.drv.StreamOff()
	closeErr := s.drv.Close()
	switch {
	case closeErr != nil:
		s.closeErr = errors.NewErr("KeyQRCaptureFailed", closeErr)
	case offErr != nil && !errors.Is(offErr, unix.ENODEV):
		s.closeErr = errors.NewErr("KeyQRCaptureFailed", offErr)
	}
	s.bufs = nil
	return s.closeErr
}
