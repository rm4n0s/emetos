package workspace

import (
	"slices"
	"sync"
)

// DefaultRecentLimit is the number of files a RecentFiles keeps: VS Code's MAX_HISTORY_ITEMS.
//
// Ported from VS Code: src/vs/workbench/services/history/browser/historyService.ts
const DefaultRecentLimit = 200

// RecentFiles is the list of files the user opened, the most recent first, that the quick open
// shows as "recently opened". The window keeps it in its UI state (SPEC §10.10): OnChange tells when to
// save it and Replace puts a saved list back. It is safe for concurrent use.
type RecentFiles struct {
	mu        sync.Mutex
	limit     int
	paths     []string
	listeners []*func()
}

// OnChange calls fn after the list changed, on the goroutine that changed it, and returns the function that
// stops it.
func (r *RecentFiles) OnChange(fn func()) (unsubscribe func()) {
	p := &fn
	r.mu.Lock()
	r.listeners = append(r.listeners, p)
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if i := slices.Index(r.listeners, p); i >= 0 {
			r.listeners = slices.Delete(r.listeners, i, i+1)
		}
	}
}

// notify tells the listeners; the caller has let go of the lock.
func (r *RecentFiles) notify() {
	r.mu.Lock()
	listeners := slices.Clone(r.listeners)
	r.mu.Unlock()
	for _, f := range listeners {
		(*f)()
	}
}

// Replace makes the list the paths, the most recent first (the order List gives); the paths past the limit
// and the ones that are empty or repeated are left out.
func (r *RecentFiles) Replace(paths []string) {
	r.mu.Lock()
	r.paths = nil
	for _, p := range paths {
		if p != "" && !slices.Contains(r.paths, p) && len(r.paths) < r.limit {
			r.paths = append(r.paths, p)
		}
	}
	r.mu.Unlock()
	r.notify()
}

// NewRecentFiles makes an empty list that keeps at most limit files (DefaultRecentLimit when limit
// is not positive).
func NewRecentFiles(limit int) *RecentFiles {
	if limit <= 0 {
		limit = DefaultRecentLimit
	}
	return &RecentFiles{limit: limit}
}

// Add puts the absolute path of a file first; it moves there when it is in the list already, and
// the oldest file goes when the list is full.
func (r *RecentFiles) Add(path string) {
	if path == "" {
		return
	}
	r.mu.Lock()
	if len(r.paths) > 0 && r.paths[0] == path {
		r.mu.Unlock()
		return
	}
	if i := slices.Index(r.paths, path); i >= 0 {
		r.paths = slices.Delete(r.paths, i, i+1)
	}
	r.paths = slices.Insert(r.paths, 0, path)
	if len(r.paths) > r.limit {
		r.paths = r.paths[:r.limit]
	}
	r.mu.Unlock()
	r.notify()
}

// Remove takes a path out of the list.
func (r *RecentFiles) Remove(path string) {
	r.mu.Lock()
	i := slices.Index(r.paths, path)
	if i >= 0 {
		r.paths = slices.Delete(r.paths, i, i+1)
	}
	r.mu.Unlock()
	if i >= 0 {
		r.notify()
	}
}

// Rename replaces a path by its new name, keeping its place: the files below a renamed folder
// follow it too.
func (r *RecentFiles) Rename(oldPath, newPath string) {
	r.mu.Lock()
	changed := false
	for i, p := range r.paths {
		switch {
		case p == oldPath:
			r.paths[i] = newPath
			changed = true
		case len(p) > len(oldPath) && p[:len(oldPath)] == oldPath && p[len(oldPath)] == '/':
			r.paths[i] = newPath + p[len(oldPath):]
			changed = true
		}
	}
	r.mu.Unlock()
	if changed {
		r.notify()
	}
}

// List is the paths, the most recent first. The caller owns the slice.
func (r *RecentFiles) List() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.paths)
}

// Len is the number of files in the list.
func (r *RecentFiles) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.paths)
}

// Clear empties the list.
func (r *RecentFiles) Clear() {
	r.mu.Lock()
	had := len(r.paths) > 0
	r.paths = nil
	r.mu.Unlock()
	if had {
		r.notify()
	}
}
