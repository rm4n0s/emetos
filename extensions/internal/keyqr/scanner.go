package keyqr

import (
	"context"
	"image"
	"time"
)

// DefaultScanInterval is how often a frame is looked at: about 10 frames a second.
const DefaultScanInterval = 100 * time.Millisecond

// ScanOptions are the options of Scan.
type ScanOptions struct {
	// Interval is the time between the start of one frame and the start of the next that is looked at; DefaultScanInterval when
	// zero. A frame that takes longer to read than the interval is followed at once by the newest one; the frames in between are
	// dropped, and the camera never queues up.
	Interval time.Duration
	// OnFrame is called with every frame that is looked at, on the goroutine of Scan, before it is searched for a code: the
	// preview. It must not keep the image past what it needs (nothing here does) and must return quickly, handing work to the UI
	// thread rather than doing it.
	OnFrame func(frame image.Image)
	// OnCode is called with the text of every QR code that is read, on the goroutine of Scan. The same code is read again in the
	// next frames while it stays in front of the camera, so the caller decides what is news.
	OnCode func(text string)
}

// Scan reads frames from the stream, about ten a second, and looks for a QR code in each. It does the work on the calling
// goroutine, so a caller that must not block (the UI thread) runs it on a goroutine of its own. It returns nil when ctx ends and
// the error of the stream when the camera fails (the tags of Stream.Next); a frame with no code is the usual case and is not an
// error. It does not close the stream.
func Scan(ctx context.Context, stream Stream, opts ScanOptions) error {
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultScanInterval
	}
	for {
		frame, err := stream.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		started := time.Now()
		if opts.OnFrame != nil {
			opts.OnFrame(frame)
		}
		if text, err := DecodeQR(frame); err == nil && opts.OnCode != nil {
			opts.OnCode(text)
		}
		if wait := interval - time.Since(started); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		}
	}
}
