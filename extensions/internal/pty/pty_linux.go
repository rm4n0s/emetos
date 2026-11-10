//go:build linux

package pty

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// The requests that read and set the terminal's settings.
const (
	termiosGet = unix.TCGETS
	termiosSet = unix.TCSETS
)

// unlockSlave unlocks the slave of the master fd and returns its path (/dev/pts/N). The error is the system's.
func unlockSlave(fd int) (string, error) {
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		return "", err
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		return "", err
	}
	return "/dev/pts/" + strconv.Itoa(number), nil
}
