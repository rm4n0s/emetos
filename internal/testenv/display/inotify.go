package display

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"

	"github.com/rm4n0s/emetos/internal/gui/app"
)

// watchWarningPrefix starts the message of the warning that the workspace posts when it cannot create an inotify watcher
// (internal/core/workspace/watcher.go, shown by internal/gui/app/workspace.go).
const watchWarningPrefix = "Emetos cannot watch files for changes"

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

// devRequester sends a command to the dev server of an editor.
type devRequester interface {
	Request(command string, args any) (app.DevResponse, error)
}

// dismissWatchWarning waits up to wait for the toast of the warning about watching files, and dismisses that notification (and
// no other) through dev.dismissNotification. It reports whether it dismissed it; a toast that never comes is not an error.
func dismissWatchWarning(r devRequester, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		resp, err := r.Request("dev.toasts", nil)
		if err == nil && resp.OK {
			var toasts []app.DevToast
			if json.Unmarshal(resp.Result, &toasts) == nil {
				for _, toast := range toasts {
					if !strings.HasPrefix(toast.Message, watchWarningPrefix) {
						continue
					}
					resp, err := r.Request("dev.dismissNotification", toast.ID)
					return err == nil && resp.OK
				}
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}
