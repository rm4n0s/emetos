//go:build !linux

package workspace

import "github.com/rm4n0s/errors"

// nativeWatching tells whether the system tells the watcher about changes: inotify does on Linux. Everywhere else the watcher
// polls, which is not a failure and sends no notice (a kqueue or FSEvents watcher would replace it).
const nativeWatching = false

// inotify does not exist outside Linux; the watcher polls.
type inotify struct {
	// wds is never filled; it lets the tests name the folders that inotify watches, which are none here.
	wds map[int]*watchedDir
}

type watchedDir struct{ path string }

func newInotify(func(int) (int, error), func(int, string, uint32) (int, error)) (*inotify, error) {
	return nil, errors.New("WatcherInitFailed", "inotify is only available on Linux")
}

func (n *inotify) close()                        {}
func (n *inotify) dropRoot(r *root)              {}
func (n *inotify) add(string, *root, bool) error { return nil }

func (w *Watcher) addRootLocked(r *root, out *[]rawEvent) error { return nil }
func (w *Watcher) readLoop()                                    { w.wg.Done() }
