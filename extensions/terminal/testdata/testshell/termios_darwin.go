//go:build darwin

package main

import "golang.org/x/sys/unix"

// The requests that read and set the terminal's settings.
const (
	termiosGet = unix.TIOCGETA
	termiosSet = unix.TIOCSETA
)
