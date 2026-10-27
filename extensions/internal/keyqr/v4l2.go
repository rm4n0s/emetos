package keyqr

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// V4L2 is the Source of the cameras of a Linux computer, read through the V4L2 interface of the kernel: the devices /dev/video*
// that are video capture devices that stream. Make it with NewV4L2.
type V4L2 struct {
	// glob lists the device files, and open opens one; the real ones are in v4l2_linux.go and the tests replace them. Nothing
	// outside this package opens a device.
	glob func() ([]string, error)
	open func(path string) (driver, error)
	opts captureOptions
}

// NewV4L2 makes the Source of the real cameras. Making it opens nothing: the devices are looked at when Cameras is called, and
// one is opened when Open is.
func NewV4L2() *V4L2 { return &V4L2{glob: globDevices, open: openDriver} }

// Cameras implements Source: it opens every /dev/video* for a moment to ask what it is (this starts nothing and does not light the
// camera) and lists the ones that capture video. The metadata nodes that most cameras have next to their video node, and the
// devices that are not cameras, are left out. It fails with KeyQRCameraListFailed when the devices cannot be listed, and with
// KeyQRCameraDenied when there is no camera this user may open but there are devices that it may not.
func (v *V4L2) Cameras(ctx context.Context) ([]CameraInfo, error) {
	paths, err := v.glob()
	if err != nil {
		return nil, errors.NewErr("KeyQRCameraListFailed", err)
	}
	sort.Slice(paths, func(i, j int) bool { return deviceNumber(paths[i]) < deviceNumber(paths[j]) })
	var cams []CameraInfo
	denied := ""
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, errors.NewErr("KeyQRCancelled", err)
		}
		drv, err := v.open(path)
		if err != nil {
			if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
				denied = path
			}
			continue // busy, gone or not a device of this kind
		}
		caps, err := drv.QueryCap()
		_ = drv.Close() // it was opened only to ask; nothing is lost if closing fails
		if err != nil || !caps.Capture || !caps.Streaming {
			continue
		}
		cams = append(cams, CameraInfo{ID: path, Name: caps.Card})
	}
	if len(cams) == 0 && denied != "" {
		return nil, errors.New("KeyQRCameraDenied", "this user may not open the cameras (is it in the video group?)", "device", denied)
	}
	return cams, nil
}

// Open implements Source. It fails like openStream (KeyQRCameraBusy, KeyQRCameraDenied, KeyQRCameraGone, KeyQRCameraOpenFailed,
// KeyQRCameraNotCapture, KeyQRNoFormat, KeyQRFormatRejected, KeyQRFrameTooLarge, KeyQRBuffersFailed).
func (v *V4L2) Open(ctx context.Context, id string) (Stream, error) {
	return openStream(ctx, func() (driver, error) { return v.open(id) }, v.opts)
}

// deviceNumber is the number at the end of /dev/video12 (video2 comes before video10), or a large one for a name that has none.
func deviceNumber(path string) int {
	i := len(path)
	for i > 0 && path[i-1] >= '0' && path[i-1] <= '9' {
		i--
	}
	n, err := strconv.Atoi(path[i:])
	if err != nil || !strings.Contains(path[:i], "video") {
		return 1 << 30
	}
	return n
}
