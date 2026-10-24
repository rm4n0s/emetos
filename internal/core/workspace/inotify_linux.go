//go:build linux

package workspace

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// watchMask is what Emetos asks inotify for on each folder. IN_MODIFY comes for every write of
// a file, so a long write gives many events, which the coalescer joins.
const watchMask = unix.IN_CREATE | unix.IN_DELETE | unix.IN_MODIFY | unix.IN_CLOSE_WRITE | unix.IN_ATTRIB |
	unix.IN_MOVED_FROM | unix.IN_MOVED_TO | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_ONLYDIR

// inotify is the inotify instance of a watcher. Its maps are guarded by Watcher.mu.
type inotify struct {
	f  *os.File // the descriptor, in non-blocking mode so that closing it ends a Read
	fd int
	// addWatch is unix.InotifyAddWatch; tests replace it to reach the limit of watches.
	addWatch func(fd int, path string, mask uint32) (int, error)
	wds      map[int]*watchedDir
	byPath   map[string]*watchedDir
}

// watchedDir is a folder with an inotify watch.
type watchedDir struct {
	wd    int
	path  string
	roots map[*root]struct{}
}

// newInotify makes an inotify instance. init is unix.InotifyInit1 and add unix.InotifyAddWatch
// unless tests replace them.
func newInotify(init func(flags int) (int, error), add func(fd int, path string, mask uint32) (int, error)) (*inotify, error) {
	if init == nil {
		init = unix.InotifyInit1
	}
	if add == nil {
		add = unix.InotifyAddWatch
	}
	fd, err := init(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, errors.NewErr("WatcherInitFailed", err)
	}
	// os.NewFile puts a non-blocking descriptor in Go's poller. The descriptor is never asked
	// for with f.Fd(), which would make it blocking again.
	return &inotify{
		f: os.NewFile(uintptr(fd), "inotify"), fd: fd, addWatch: add,
		wds: map[int]*watchedDir{}, byPath: map[string]*watchedDir{},
	}, nil
}

// close closes the descriptor, which ends the read loop.
func (n *inotify) close() { _ = n.f.Close() }

// add watches a folder for a root. follow lets the watch go through a link (only the root of a
// target does).
func (n *inotify) add(path string, r *root, follow bool) error {
	mask := uint32(watchMask)
	if !follow {
		mask |= unix.IN_DONT_FOLLOW
	}
	wd, err := n.addWatch(n.fd, path, mask)
	if err != nil {
		return err
	}
	d := n.wds[wd]
	if d == nil {
		d = &watchedDir{wd: wd, path: path, roots: map[*root]struct{}{}}
		n.wds[wd] = d
	}
	if d.path != path {
		delete(n.byPath, d.path)
		d.path = path
	}
	n.byPath[path] = d
	d.roots[r] = struct{}{}
	return nil
}

// dropRoot takes a root out of every watched folder and removes the watches nobody needs.
func (n *inotify) dropRoot(r *root) {
	for wd, d := range n.wds {
		if _, ok := d.roots[r]; !ok {
			continue
		}
		delete(d.roots, r)
		if len(d.roots) == 0 {
			// the error says the folder is gone already, which is what we want
			_, _ = unix.InotifyRmWatch(n.fd, uint32(wd))
			delete(n.wds, wd)
			if n.byPath[d.path] == d {
				delete(n.byPath, d.path)
			}
		}
	}
}

// renameTree makes the watched folders at and below oldPath follow a rename.
func (n *inotify) renameTree(oldPath, newPath string) {
	for _, d := range n.wds {
		switch {
		case d.path == oldPath:
			delete(n.byPath, d.path)
			d.path = newPath
			n.byPath[newPath] = d
		case isParentPath(d.path, oldPath):
			delete(n.byPath, d.path)
			d.path = newPath + d.path[len(oldPath):]
			n.byPath[d.path] = d
		}
	}
}

// dropTree removes the watches at and below a path (the folder left the watched tree).
func (n *inotify) dropTree(path string) {
	for wd, d := range n.wds {
		if d.path == path || isParentPath(d.path, path) {
			_, _ = unix.InotifyRmWatch(n.fd, uint32(wd))
			delete(n.wds, wd)
			if n.byPath[d.path] == d {
				delete(n.byPath, d.path)
			}
		}
	}
}

// addRootLocked places the watches of a new root. The caller holds w.mu.
func (w *Watcher) addRootLocked(r *root, out *[]rawEvent) error {
	if err := w.ino.add(r.path, r, true); err != nil {
		if isNoSpace(err) {
			w.fallbackLocked([]*root{r}, r.path, err, nil)
			return nil
		}
		return errors.NewErr("WatcherAddFailed", err).SetMetadata("path", r.path)
	}
	if r.recursive {
		w.walkLocked(r.path, []*root{r}, false, out)
	}
	return nil
}

// walkLocked watches the folders below dir for the roots rs, which are recursive. With emit,
// the entries found are reported as created (a folder that appeared may already hold files).
func (w *Watcher) walkLocked(dir string, rs []*root, emit bool, out *[]rawEvent) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		w.log.Debug("watcher: cannot read a folder", "path", dir, "err", err)
		return
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		isDir := e.IsDir() // a link is not followed
		var sub []*root
		for _, r := range rs {
			if !r.excluded(p) {
				sub = append(sub, r)
			}
		}
		if len(sub) == 0 {
			continue
		}
		if emit {
			*out = append(*out, rawEvent{kind: rawCreated, path: p, isDir: isDir})
		}
		if !isDir {
			continue
		}
		var added []*root
		for _, r := range sub {
			if err := w.ino.add(p, r, false); err != nil {
				if isNoSpace(err) {
					w.fallbackLocked([]*root{r}, p, err, out)
					continue
				}
				w.log.Debug("watcher: cannot watch a folder", "path", p, "err", err)
				continue
			}
			added = append(added, r)
		}
		if len(added) > 0 {
			w.walkLocked(p, added, emit, out)
		}
	}
}

// fallbackLocked moves roots to polling because inotify has no watches left. The caller holds
// w.mu. The roots are scanned afresh by the poller; out (when not nil) gets a rescan event so
// that the consumers read everything again.
func (w *Watcher) fallbackLocked(rs []*root, path string, err error, out *[]rawEvent) {
	for _, r := range rs {
		if r.polling {
			continue
		}
		r.polling = true
		w.ino.dropRoot(r)
		if out != nil {
			*out = append(*out, rawEvent{kind: rawRescan, path: r.path})
		}
	}
	w.log.Warn("the limit of inotify watches is reached, polling instead", "path", path, "err", err)
	go w.sendNotice(Notice{Kind: NoticeWatchLimit, Path: path, Err: err,
		Message: "Emetos reached the limit of file watches of the system (fs.inotify.max_user_watches), so it checks " +
			"some folders for changes every few seconds instead. Raising the limit makes changes show at once."})
}

// readLoop reads the events of inotify until the descriptor is closed.
func (w *Watcher) readLoop() {
	defer w.wg.Done()
	buf := make([]byte, 64*1024)
	for {
		n, err := w.ino.f.Read(buf)
		if err != nil {
			if w.ctx.Err() == nil && !errors.Is(err, os.ErrClosed) {
				w.log.Error("reading inotify failed", "err", err)
			}
			return
		}
		w.handle(buf[:n])
	}
}

// inotifyHeader is the size of struct inotify_event without the name.
const inotifyHeader = 16

// handle turns the events of one read into raw events.
func (w *Watcher) handle(buf []byte) {
	var out []rawEvent
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	moved := map[uint32]string{} // folders moved from, by cookie, that wait for their move to
	for len(buf) >= inotifyHeader {
		wd := int32(binary.NativeEndian.Uint32(buf[0:4]))
		mask := binary.NativeEndian.Uint32(buf[4:8])
		cookie := binary.NativeEndian.Uint32(buf[8:12])
		nameLen := int(binary.NativeEndian.Uint32(buf[12:16]))
		if inotifyHeader+nameLen > len(buf) {
			break
		}
		name := strings.TrimRight(string(buf[inotifyHeader:inotifyHeader+nameLen]), "\x00")
		buf = buf[inotifyHeader+nameLen:]
		w.handleOne(int(wd), mask, cookie, name, moved, &out)
	}
	// a folder moved from a watched folder and not moved into another one has left the tree
	for _, p := range moved {
		w.ino.dropTree(p)
	}
	w.mu.Unlock()
	w.send(out)
}

// handleOne handles one inotify event. The caller holds w.mu.
func (w *Watcher) handleOne(wd int, mask, cookie uint32, name string, moved map[uint32]string, out *[]rawEvent) {
	if mask&unix.IN_Q_OVERFLOW != 0 {
		w.log.Warn("the inotify queue overflowed, some events were lost")
		for r := range w.roots {
			if !r.polling {
				*out = append(*out, rawEvent{kind: rawRescan, path: r.path})
			}
		}
		return
	}
	d := w.ino.wds[wd]
	if d == nil {
		return
	}
	if mask&unix.IN_IGNORED != 0 {
		delete(w.ino.wds, wd)
		if w.ino.byPath[d.path] == d {
			delete(w.ino.byPath, d.path)
		}
		return
	}
	path := d.path
	if name != "" {
		path = filepath.Join(d.path, name)
	}
	isDir := mask&unix.IN_ISDIR != 0
	accepted := func(p string) bool {
		for r := range d.roots {
			if r.accepts(p) {
				return true
			}
		}
		return false
	}
	var recursive []*root
	for r := range d.roots {
		if r.recursive && !r.excluded(path) {
			recursive = append(recursive, r)
		}
	}
	switch {
	case mask&unix.IN_CREATE != 0:
		if accepted(path) {
			*out = append(*out, rawEvent{kind: rawCreated, path: path, isDir: isDir})
		}
		if isDir && len(recursive) > 0 {
			w.watchNewDir(path, recursive, true, out)
		}
	case mask&unix.IN_MOVED_FROM != 0:
		*out = append(*out, rawEvent{kind: rawMovedFrom, path: path, isDir: isDir, cookie: cookie, skip: !accepted(path)})
		if isDir {
			moved[cookie] = path
		}
	case mask&unix.IN_MOVED_TO != 0:
		*out = append(*out, rawEvent{kind: rawMovedTo, path: path, isDir: isDir, cookie: cookie, skip: !accepted(path)})
		if isDir {
			if from, ok := moved[cookie]; ok {
				delete(moved, cookie)
				w.ino.renameTree(from, path)
			} else if len(recursive) > 0 {
				w.watchNewDir(path, recursive, false, out)
			}
		}
	case mask&unix.IN_DELETE != 0:
		if accepted(path) {
			*out = append(*out, rawEvent{kind: rawDeleted, path: path, isDir: isDir})
		}
	case mask&unix.IN_DELETE_SELF != 0:
		for r := range d.roots {
			if r.path == d.path {
				*out = append(*out, rawEvent{kind: rawDeleted, path: d.path, isDir: true})
				break
			}
		}
	case mask&unix.IN_MOVE_SELF != 0:
		for r := range d.roots {
			if r.path == d.path {
				// the watched folder itself moved: it is not where it was
				*out = append(*out, rawEvent{kind: rawDeleted, path: d.path, isDir: true})
				w.ino.dropTree(d.path)
				break
			}
		}
	case mask&(unix.IN_MODIFY|unix.IN_CLOSE_WRITE|unix.IN_ATTRIB) != 0:
		if name != "" && accepted(path) {
			*out = append(*out, rawEvent{kind: rawChanged, path: path, isDir: isDir})
		}
	}
}

// watchNewDir watches a folder that appeared in a recursive root, and what is already in it.
func (w *Watcher) watchNewDir(path string, rs []*root, emit bool, out *[]rawEvent) {
	var added []*root
	for _, r := range rs {
		if err := w.ino.add(path, r, false); err != nil {
			if isNoSpace(err) {
				w.fallbackLocked([]*root{r}, path, err, out)
			} else {
				w.log.Debug("watcher: cannot watch a new folder", "path", path, "err", err)
			}
			continue
		}
		added = append(added, r)
	}
	if len(added) > 0 {
		w.walkLocked(path, added, emit, out)
	}
}
