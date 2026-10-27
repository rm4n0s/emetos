//go:build emetos_camera

package keyqr

import (
	"context"
	"image"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// TestRealCamera is the manual check of step 15.5 (docs/MANUAL-CHECKS.md): it opens the first real camera of this computer
// (or the one in EMETOS_CAMERA, a device such as /dev/video2), shows what it found, and waits up to 60 seconds for a QR code
// that holds a public key line, which the user holds up in front of it (the QR code of the Show My Public Key as QR Code dialog
// of another Emetos, or any QR code of a line of an id_ed25519.pub). It prints the type, the fingerprint and the comment of the
// key, and the frame size and rate of the camera. Nothing is written or kept.
//
//	go test -tags emetos_camera -run TestRealCamera -v ./extensions/internal/keyqr
func TestRealCamera(t *testing.T) {
	src := NewV4L2()
	cams, err := src.Cameras(t.Context())
	if err != nil {
		t.Fatalf("cannot list the cameras: %v", err)
	}
	if len(cams) == 0 {
		t.Skip("this computer has no camera")
	}
	for _, c := range cams {
		t.Logf("camera %s: %s", c.ID, c.Name)
	}
	id := cams[0].ID
	if env := os.Getenv("EMETOS_CAMERA"); env != "" {
		id = env
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	stream, err := src.Open(ctx, id)
	if err != nil {
		t.Fatalf("cannot open %s: %v", id, err)
	}
	defer func() {
		if err := stream.Close(); err != nil {
			t.Errorf("closing the camera: %v", err)
		}
	}()
	t.Logf("opened %s: hold a QR code of a public key in front of the camera (60 seconds)", id)
	var frames atomic.Int64
	var size atomic.Value
	started := time.Now()
	found := make(chan string, 1)
	scanCtx, stop := context.WithCancel(ctx)
	scanned := make(chan struct{})
	defer func() { stop(); <-scanned }() // the scan goroutine logs through t, so it ends before the test does
	go func() {
		defer close(scanned)
		_ = Scan(scanCtx, stream, ScanOptions{
			OnFrame: func(img image.Image) { frames.Add(1); size.Store(img.Bounds().Size()) },
			OnCode: func(text string) {
				if _, err := ParseKey(text); err == nil {
					select {
					case found <- text:
					default:
					}
				} else {
					t.Logf("a QR code that is not a public key: %.40q", text)
				}
			},
		})
	}()
	select {
	case text := <-found:
		stop()
		key, err := ParseKey(text)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("read a public key: type %s, fingerprint %s, comment %q", key.Type(), key.Fingerprint(), key.Comment)
		t.Logf("%d frames of %v in %v", frames.Load(), size.Load(), time.Since(started).Round(time.Millisecond))
	case <-ctx.Done():
		t.Fatalf("no public key was read in 60 seconds (%d frames of %v)", frames.Load(), size.Load())
	}
}
