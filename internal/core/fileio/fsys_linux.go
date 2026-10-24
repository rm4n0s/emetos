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

// keepOwner gives the file at path the owner and group of orig, best effort: a process
// that may not do it (it is not root and does not own the group) leaves the new file with its
// own owner, as the editors that save by renaming do.
func keepOwner(path string, orig os.FileInfo) {
	if orig == nil {
		return
	}
	st, ok := orig.Sys().(*syscall.Stat_t)
	if !ok || (int(st.Uid) == os.Getuid() && int(st.Gid) == os.Getgid()) {
		return
	}
	// A failure leaves the file with the owner of the process; nothing more can be done about it.
	_ = os.Chown(path, int(st.Uid), int(st.Gid))
}
