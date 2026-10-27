package keyqr

import (
	"context"
	"image"
)

// CameraInfo is a camera that can be opened.
type CameraInfo struct {
	// ID names the camera to Source.Open (the device file, /dev/video0).
	ID string
	// Name is what the camera calls itself ("Integrated Camera"); the picker shows it with the ID.
	Name string
}

// Source finds the cameras of the computer and opens them. The real one is V4L2; FakeSource plays frames from memory. The
// methods may be called from any goroutine.
type Source interface {
	// Cameras lists the cameras that can capture video. It returns an empty list when there is none, and an error (tagged
	// KeyQRCameraDenied) when the computer has devices that this user may not open.
	Cameras(ctx context.Context) ([]CameraInfo, error)
	// Open turns a camera on. The camera is released when ctx ends or the Stream is closed, whichever is first.
	Open(ctx context.Context, id string) (Stream, error)
}

// Stream is an open camera.
type Stream interface {
	// Next waits for the newest frame and returns it: the frames that came while the caller was busy are dropped (they are put
	// back for the camera to fill), so a slow caller shows what the camera sees now and never falls behind. The image is the
	// caller's, which no later call touches. It fails with the error of the context when it ends, and with the tags of the
	// camera layer when the camera fails (KeyQRCameraGone, KeyQRNoFrames, KeyQRStreamClosed, ...).
	Next(ctx context.Context) (image.Image, error)
	// Close turns the camera off and gives it back. It may be called from any goroutine, more than once; a Next that is waiting
	// returns.
	Close() error
}
