package fileio

import (
	"os"
	"syscall"
)

// Device returns the id of the filesystem a file is on.
func (osFS) Device(info os.FileInfo) (uint64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true //nolint:unconvert // the type of Dev differs between architectures
}
