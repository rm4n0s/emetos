//go:build !linux

package workspace

import "github.com/rm4n0s/errors"

// inotify does not exist outside Linux (Emetos targets Linux only); the watcher polls.
type inotify struct{}

func newInotify(func(int) (int, error), func(int, string, uint32) (int, error)) (*inotify, error) {
	return nil, errors.New("WatcherInitFailed", "inotify is only available on Linux")
}

func (n *inotify) close()                        {}
func (n *inotify) dropRoot(r *root)              {}
func (n *inotify) add(string, *root, bool) error { return nil }

func (w *Watcher) addRootLocked(r *root, out *[]rawEvent) error { return nil }
func (w *Watcher) readLoop()                                    { w.wg.Done() }
