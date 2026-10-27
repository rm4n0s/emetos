//go:build linux && (amd64 || arm64)

package keyqr

import (
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The structures and constants of linux/videodev2.h, for 64-bit Linux (amd64 and arm64 have the same layout: little endian,
// 8-byte pointers and longs). Go lays a struct out as C does, with the same alignment, so the fields below are the kernel's own
// and the sizes are checked against the kernel's in v4l2_linux_test.go (a v4l2_format is 208 bytes, a v4l2_buffer 88).

// The capabilities of VIDIOC_QUERYCAP.
const (
	capVideoCapture = 0x00000001
	capStreaming    = 0x04000000
	capDeviceCaps   = 0x80000000
)

// bufTypeVideoCapture is V4L2_BUF_TYPE_VIDEO_CAPTURE, memoryMmap V4L2_MEMORY_MMAP, fieldNone V4L2_FIELD_NONE and
// bufFlagError V4L2_BUF_FLAG_ERROR.
const (
	bufTypeVideoCapture = 1
	memoryMmap          = 1
	fieldNone           = 1
	bufFlagError        = 0x00000040
)

// v4l2Capability is struct v4l2_capability (104 bytes).
type v4l2Capability struct {
	Driver       [16]byte
	Card         [32]byte
	BusInfo      [32]byte
	Version      uint32
	Capabilities uint32
	DeviceCaps   uint32
	Reserved     [3]uint32
}

// v4l2Fmtdesc is struct v4l2_fmtdesc (64 bytes).
type v4l2Fmtdesc struct {
	Index       uint32
	Type        uint32
	Flags       uint32
	Description [32]byte
	PixelFormat uint32
	MbusCode    uint32
	Reserved    [3]uint32
}

// v4l2PixFormat is struct v4l2_pix_format (48 bytes).
type v4l2PixFormat struct {
	Width        uint32
	Height       uint32
	PixelFormat  uint32
	Field        uint32
	BytesPerLine uint32
	SizeImage    uint32
	Colorspace   uint32
	Priv         uint32
	Flags        uint32
	Enc          uint32 // the union of ycbcr_enc and hsv_enc
	Quantization uint32
	XferFunc     uint32
}

// v4l2Format is struct v4l2_format (208 bytes on 64-bit): the type and a union of 200 bytes aligned to 8 (some of its members
// hold pointers), of which a video capture uses the first 48 as a v4l2_pix_format.
type v4l2Format struct {
	Type uint32
	_    [4]byte
	Pix  v4l2PixFormat
	_    [152]byte
}

// v4l2Requestbuffers is struct v4l2_requestbuffers (20 bytes).
type v4l2Requestbuffers struct {
	Count        uint32
	Type         uint32
	Memory       uint32
	Capabilities uint32
	Flags        uint8
	Reserved     [3]uint8
}

// v4l2Timecode is struct v4l2_timecode (16 bytes).
type v4l2Timecode struct {
	Type     uint32
	Flags    uint32
	Frames   uint8
	Seconds  uint8
	Minutes  uint8
	Hours    uint8
	Userbits [4]uint8
}

// v4l2Buffer is struct v4l2_buffer (88 bytes on 64-bit). M is the union of offset (u32, the low half on a little endian machine),
// userptr, planes and fd.
type v4l2Buffer struct {
	Index     uint32
	Type      uint32
	BytesUsed uint32
	Flags     uint32
	Field     uint32
	Sec       int64
	Usec      int64
	Timecode  v4l2Timecode
	Sequence  uint32
	Memory    uint32
	M         uint64
	Length    uint32
	Reserved2 uint32
	RequestFD int32
}

// The ioctl numbers are _IOR/_IOW/_IOWR('V', nr, struct) of <linux/ioctl.h>: the direction in the top two bits, the size of the
// struct in 14 bits, the type 'V' in 8 and the number in 8.
const (
	iocWrite = 1
	iocRead  = 2
)

const (
	vidiocQuerycap  = iocRead<<30 | unsafe.Sizeof(v4l2Capability{})<<16 | 'V'<<8 // number 0
	vidiocEnumFmt   = (iocRead|iocWrite)<<30 | unsafe.Sizeof(v4l2Fmtdesc{})<<16 | 'V'<<8 | 2
	vidiocSFmt      = (iocRead|iocWrite)<<30 | unsafe.Sizeof(v4l2Format{})<<16 | 'V'<<8 | 5
	vidiocReqbufs   = (iocRead|iocWrite)<<30 | unsafe.Sizeof(v4l2Requestbuffers{})<<16 | 'V'<<8 | 8
	vidiocQuerybuf  = (iocRead|iocWrite)<<30 | unsafe.Sizeof(v4l2Buffer{})<<16 | 'V'<<8 | 9
	vidiocQbuf      = (iocRead|iocWrite)<<30 | unsafe.Sizeof(v4l2Buffer{})<<16 | 'V'<<8 | 15
	vidiocDqbuf     = (iocRead|iocWrite)<<30 | unsafe.Sizeof(v4l2Buffer{})<<16 | 'V'<<8 | 17
	vidiocStreamon  = iocWrite<<30 | unsafe.Sizeof(int32(0))<<16 | 'V'<<8 | 18
	vidiocStreamoff = iocWrite<<30 | unsafe.Sizeof(int32(0))<<16 | 'V'<<8 | 19
)

// maxFormatsListed bounds the loop that lists the pixel formats, so that a device that never says it is done cannot hold it.
const maxFormatsListed = 256

// ioctlDriver is the driver of a device file: the ioctls, mmap and poll, with nothing decided.
type ioctlDriver struct {
	fd   int
	mu   sync.Mutex
	maps [][]byte
}

// globDevices lists /dev/video*.
func globDevices() ([]string, error) { return filepath.Glob("/dev/video*") }

// openDriver opens a device file for reading and writing, non blocking (the dequeue says unix.EAGAIN when no frame is there, and
// poll waits). It is called by the Source of the real cameras only.
func openDriver(path string) (driver, error) {
	var fd int
	var err error
	for {
		fd, err = unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != unix.EINTR {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	return &ioctlDriver{fd: fd}, nil
}

// ioctl makes one call, again when a signal interrupted it. arg points to the struct the request is made for.
func (d *ioctlDriver) ioctl(req uintptr, arg unsafe.Pointer) error {
	for {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd), req, uintptr(arg))
		switch errno {
		case 0:
			return nil
		case unix.EINTR:
			continue
		}
		return errno
	}
}

// cString is the text of a fixed array that ends at its first zero byte.
func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// QueryCap implements driver.
func (d *ioctlDriver) QueryCap() (capability, error) {
	var c v4l2Capability
	if err := d.ioctl(vidiocQuerycap, unsafe.Pointer(&c)); err != nil {
		return capability{}, err
	}
	caps := c.Capabilities
	if caps&capDeviceCaps != 0 {
		caps = c.DeviceCaps // the capabilities of this node, not of the whole device (a metadata node of a camera is not a camera)
	}
	return capability{Card: cString(c.Card[:]), Capture: caps&capVideoCapture != 0, Streaming: caps&capStreaming != 0}, nil
}

// Formats implements driver.
func (d *ioctlDriver) Formats() ([]uint32, error) {
	var out []uint32
	for i := uint32(0); i < maxFormatsListed; i++ {
		f := v4l2Fmtdesc{Index: i, Type: bufTypeVideoCapture}
		if err := d.ioctl(vidiocEnumFmt, unsafe.Pointer(&f)); err != nil {
			if err == unix.EINVAL { // the index is past the last format
				return out, nil
			}
			return nil, err
		}
		out = append(out, f.PixelFormat)
	}
	return out, nil
}

// SetFormat implements driver.
func (d *ioctlDriver) SetFormat(pixelFormat uint32, width, height int) (format, error) {
	f := v4l2Format{Type: bufTypeVideoCapture}
	f.Pix = v4l2PixFormat{Width: uint32(width), Height: uint32(height), PixelFormat: pixelFormat, Field: fieldNone}
	if err := d.ioctl(vidiocSFmt, unsafe.Pointer(&f)); err != nil {
		return format{}, err
	}
	return format{PixelFormat: f.Pix.PixelFormat, Width: int(f.Pix.Width), Height: int(f.Pix.Height), BytesPerLine: int(f.Pix.BytesPerLine), SizeImage: int(f.Pix.SizeImage)}, nil
}

// RequestBuffers implements driver.
func (d *ioctlDriver) RequestBuffers(n int) (int, error) {
	r := v4l2Requestbuffers{Count: uint32(n), Type: bufTypeVideoCapture, Memory: memoryMmap}
	if err := d.ioctl(vidiocReqbufs, unsafe.Pointer(&r)); err != nil {
		return 0, err
	}
	return int(r.Count), nil
}

// MapBuffer implements driver.
func (d *ioctlDriver) MapBuffer(i int) ([]byte, error) {
	b := v4l2Buffer{Index: uint32(i), Type: bufTypeVideoCapture, Memory: memoryMmap}
	if err := d.ioctl(vidiocQuerybuf, unsafe.Pointer(&b)); err != nil {
		return nil, err
	}
	if b.Length == 0 || b.Length > MaxFrameBytes {
		return make([]byte, 0), nil // the layer above refuses a buffer of a size it does not map
	}
	mem, err := unix.Mmap(d.fd, int64(uint32(b.M)), int(b.Length), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.maps = append(d.maps, mem)
	d.mu.Unlock()
	return mem, nil
}

// Queue implements driver.
func (d *ioctlDriver) Queue(i int) error {
	b := v4l2Buffer{Index: uint32(i), Type: bufTypeVideoCapture, Memory: memoryMmap}
	return d.ioctl(vidiocQbuf, unsafe.Pointer(&b))
}

// Dequeue implements driver.
func (d *ioctlDriver) Dequeue() (dequeued, error) {
	b := v4l2Buffer{Type: bufTypeVideoCapture, Memory: memoryMmap}
	if err := d.ioctl(vidiocDqbuf, unsafe.Pointer(&b)); err != nil {
		return dequeued{}, err
	}
	return dequeued{Index: int(b.Index), BytesUsed: int(b.BytesUsed), Damaged: b.Flags&bufFlagError != 0}, nil
}

// StreamOn implements driver.
func (d *ioctlDriver) StreamOn() error {
	t := int32(bufTypeVideoCapture)
	return d.ioctl(vidiocStreamon, unsafe.Pointer(&t))
}

// StreamOff implements driver.
func (d *ioctlDriver) StreamOff() error {
	t := int32(bufTypeVideoCapture)
	return d.ioctl(vidiocStreamoff, unsafe.Pointer(&t))
}

// Wait implements driver: poll for a frame (POLLIN), at most the time. A device that went away shows as POLLERR or POLLHUP,
// which is an error here.
func (d *ioctlDriver) Wait(timeout time.Duration) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(d.fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, int(timeout/time.Millisecond))
	if err == unix.EINTR {
		return false, nil // a signal: the caller looks at its context and waits again
	}
	if err != nil {
		return false, err
	}
	if n > 0 && fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
		return false, unix.ENODEV
	}
	return n > 0 && fds[0].Revents&unix.POLLIN != 0, nil
}

// Close implements driver.
func (d *ioctlDriver) Close() error {
	d.mu.Lock()
	maps := d.maps
	d.maps = nil
	d.mu.Unlock()
	var first error
	for _, m := range maps {
		if err := unix.Munmap(m); err != nil && first == nil {
			first = err
		}
	}
	if d.fd >= 0 {
		if err := unix.Close(d.fd); err != nil && first == nil {
			first = err
		}
		d.fd = -1
	}
	return first
}
