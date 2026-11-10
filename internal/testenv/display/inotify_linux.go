//go:build linux

package display

import (
	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// inotifyAvailable tells whether this process can create an inotify instance. The limit of instances is per user
// (fs.inotify.max_user_instances): the programs of the user's desktop can use it up, and then every editor of a test falls back
// to polling and shows a warning, whose toast takes the next Escape (the key binding notifications.hideToasts) away from the
// test. EMFILE and ENFILE mean it is used up; any other answer counts as available, so that nothing is waited for.
func inotifyAvailable() bool {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		return !errors.Is(err, unix.EMFILE) && !errors.Is(err, unix.ENFILE)
	}
	_ = unix.Close(fd)
	return true
}
