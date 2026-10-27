//go:build !(linux && (amd64 || arm64))

package keyqr

import "syscall"

// globDevices finds no cameras: the V4L2 structures are laid out here for 64-bit Linux on amd64 and arm64 only.
func globDevices() ([]string, error) { return nil, nil }

// openDriver fails with ENOSYS: there is no V4L2 layer for this platform (Emetos targets the Linux desktop).
func openDriver(string) (driver, error) { return nil, syscall.ENOSYS }
