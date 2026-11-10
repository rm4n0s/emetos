//go:build darwin

package pty

import (
	"bytes"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The requests that read and set the terminal's settings.
const (
	termiosGet = unix.TIOCGETA
	termiosSet = unix.TIOCSETA
)

// The requests of macOS's /dev/ptmx that x/sys/unix does not name (<sys/ttycom.h>): TIOCPTYGRANT is _IO('t', 0x54),
// TIOCPTYGNAME is _IOC(IOC_OUT, 't', 0x53, 128) and TIOCPTYUNLK is _IO('t', 0x52).
const (
	tiocPtyGrant = 0x20007454
	tiocPtyGname = 0x40807453
	tiocPtyUnlk  = 0x20007452
)

// unlockSlave grants and unlocks the slave of the master fd and returns its path (/dev/ttysNNN). The error is the system's.
func unlockSlave(fd int) (string, error) {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), tiocPtyGrant, 0); e != 0 {
		return "", e
	}
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), tiocPtyUnlk, 0); e != 0 {
		return "", e
	}
	var name [128]byte
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), tiocPtyGname, uintptr(unsafe.Pointer(&name[0]))); e != 0 {
		return "", e
	}
	n := bytes.IndexByte(name[:], 0)
	if n < 0 {
		n = len(name)
	}
	return string(name[:n]), nil
}
