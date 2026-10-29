// Package keyqr is what Remote Work uses to carry a public key by QR code (SPEC §10.18 "Public keys by QR code",
// step 15.5): the camera of the computer, read straight through Linux's V4L2 interface (golang.org/x/sys/unix, no cgo); the
// frames it gives (MJPEG and YUYV) turned into images; the QR codes in them (github.com/makiuchi-d/gozxing), read off the UI
// thread; the text of a code parsed as the line of a public key under the rules of the login (extensions/internal/sshlogin);
// and the other direction, a public key written as the image of a QR code.
//
// # The camera
//
// A Source lists the cameras and opens one; a Stream is an open camera that gives its newest frame on request, so a slow reader
// drops frames and never falls behind. V4L2 is the Source of the real computer; FakeSource plays frames from memory for the
// tests and the proofs. Everything that touches /dev/video* is in v4l2_linux.go, behind a small driver interface that is the
// kernel's own (ioctls, mmap, poll), so the rest of the layer (the choice of the format, the buffers, the conversion, the
// stop on cancel) is tested against a fake driver; the ioctl numbers and the struct layouts are checked against the kernel's
// sizes. No test opens a real device: the one that does is the manual check with the build tag emetos_camera.
//
// # Bounds
//
// A frame is at most 1920x1080 pixels (MaxFrameWidth, MaxFrameHeight) and its compressed form at most MaxFrameBytes; the size
// of an MJPEG frame is read with image.DecodeConfig before it is decoded, so a malicious frame costs nothing. A QR code holds at
// most 2953 bytes, and the text of a key line is at most sshlogin.MaxKeyLine. The scanner decodes about 10 frames a second and
// drops the ones that arrive while it is busy.
//
// No frame is kept: the kernel's buffers are put back as soon as a frame is converted, the scanner keeps no picture, and
// nothing is written or sent anywhere.
package keyqr
