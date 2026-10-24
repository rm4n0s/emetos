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
// shows as "recently opened". It is kept in memory; step 7.5 (UI state persistence) saves it. It is
// safe for concurrent use.
//
// TODO(step 7.5): save and restore the list with the UI state of the window.
type RecentFiles struct {
	mu    sync.Mutex
	limit int
	paths []string
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
	defer r.mu.Unlock()
	if i := slices.Index(r.paths, path); i >= 0 {
		r.paths = slices.Delete(r.paths, i, i+1)
	}
	r.paths = slices.Insert(r.paths, 0, path)
	if len(r.paths) > r.limit {
		r.paths = r.paths[:r.limit]
	}
}

// Remove takes a path out of the list.
func (r *RecentFiles) Remove(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := slices.Index(r.paths, path); i >= 0 {
		r.paths = slices.Delete(r.paths, i, i+1)
	}
}

// Rename replaces a path by its new name, keeping its place: the files below a renamed folder
// follow it too.
func (r *RecentFiles) Rename(oldPath, newPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.paths {
		switch {
		case p == oldPath:
			r.paths[i] = newPath
		case len(p) > len(oldPath) && p[:len(oldPath)] == oldPath && p[len(oldPath)] == '/':
			r.paths[i] = newPath + p[len(oldPath):]
		}
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
	defer r.mu.Unlock()
	r.paths = nil
}
