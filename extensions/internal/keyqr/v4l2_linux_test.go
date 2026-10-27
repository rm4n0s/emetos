//go:build linux && (amd64 || arm64)

package keyqr

import (
	"testing"
	"unsafe"
)

// The kernel's sizes and numbers for 64-bit Linux (amd64 and arm64), from linux/videodev2.h and the values every V4L2 program of
// those machines uses (v4l2-ctl --verbose prints VIDIOC_S_FMT as 0xc0d05605). No device is opened here.
func TestTheStructuresHaveTheSizesOfTheKernel(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"v4l2_capability", unsafe.Sizeof(v4l2Capability{}), 104},
		{"v4l2_fmtdesc", unsafe.Sizeof(v4l2Fmtdesc{}), 64},
		{"v4l2_pix_format", unsafe.Sizeof(v4l2PixFormat{}), 48},
		{"v4l2_format", unsafe.Sizeof(v4l2Format{}), 208},
		{"v4l2_requestbuffers", unsafe.Sizeof(v4l2Requestbuffers{}), 20},
		{"v4l2_timecode", unsafe.Sizeof(v4l2Timecode{}), 16},
		{"v4l2_buffer", unsafe.Sizeof(v4l2Buffer{}), 88},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s is %d bytes, the kernel's is %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestTheFieldsAreAtTheOffsetsOfTheKernel(t *testing.T) {
	var b v4l2Buffer
	var f v4l2Format
	var c v4l2Capability
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"v4l2_buffer.bytesused", unsafe.Offsetof(b.BytesUsed), 8},
		{"v4l2_buffer.flags", unsafe.Offsetof(b.Flags), 12},
		{"v4l2_buffer.timestamp", unsafe.Offsetof(b.Sec), 24},
		{"v4l2_buffer.timecode", unsafe.Offsetof(b.Timecode), 40},
		{"v4l2_buffer.sequence", unsafe.Offsetof(b.Sequence), 56},
		{"v4l2_buffer.memory", unsafe.Offsetof(b.Memory), 60},
		{"v4l2_buffer.m", unsafe.Offsetof(b.M), 64},
		{"v4l2_buffer.length", unsafe.Offsetof(b.Length), 72},
		{"v4l2_buffer.request_fd", unsafe.Offsetof(b.RequestFD), 80},
		{"v4l2_format.fmt", unsafe.Offsetof(f.Pix), 8},
		{"v4l2_capability.capabilities", unsafe.Offsetof(c.Capabilities), 84},
		{"v4l2_capability.device_caps", unsafe.Offsetof(c.DeviceCaps), 88},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s is at %d, the kernel's is %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestTheIoctlNumbersAreTheKernels(t *testing.T) {
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"VIDIOC_QUERYCAP", vidiocQuerycap, 0x80685600},
		{"VIDIOC_ENUM_FMT", vidiocEnumFmt, 0xc0405602},
		{"VIDIOC_S_FMT", vidiocSFmt, 0xc0d05605},
		{"VIDIOC_REQBUFS", vidiocReqbufs, 0xc0145608},
		{"VIDIOC_QUERYBUF", vidiocQuerybuf, 0xc0585609},
		{"VIDIOC_QBUF", vidiocQbuf, 0xc058560f},
		{"VIDIOC_DQBUF", vidiocDqbuf, 0xc0585611},
		{"VIDIOC_STREAMON", vidiocStreamon, 0x40045612},
		{"VIDIOC_STREAMOFF", vidiocStreamoff, 0x40045613},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s is %#x, the kernel's is %#x", tc.name, tc.got, tc.want)
		}
	}
}

func TestThePixelFormatCodesAreFourLetters(t *testing.T) {
	if PixFmtMJPEG != 0x47504a4d || PixFmtYUYV != 0x56595559 {
		t.Errorf("MJPG is %#x and YUYV %#x", PixFmtMJPEG, PixFmtYUYV)
	}
}

func TestACStringEndsAtItsFirstZero(t *testing.T) {
	if got := cString([]byte("Integrated\x00garbage")); got != "Integrated" {
		t.Errorf("got %q", got)
	}
	if got := cString([]byte("full")); got != "full" {
		t.Errorf("got %q", got)
	}
}
