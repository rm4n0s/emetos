package storage

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
)

// StateVersion is the version of the state files (SPEC §10.10). A file with another version is ignored
// (StateVersionUnsupported) and written again with this one.
const StateVersion = 1

// Geometry is the size and the state of a window. The size is the content size in logical pixels; it is the
// size the window has when it is neither maximized nor in full screen. The position is not kept: Wayland
// compositors place windows.
type Geometry struct {
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
	Maximized  bool    `json:"maximized,omitempty"`
	Fullscreen bool    `json:"fullscreen,omitempty"`
}

// Valid tells whether the size is one a window can be opened with.
func (g Geometry) Valid() bool {
	ok := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 1 && v <= 1<<16 }
	return ok(g.Width) && ok(g.Height)
}

// ExplorerState is what is kept of the Explorer: the folders that are expanded, as paths relative to the
// folder of the window, with slashes, sorted.
type ExplorerState struct {
	Expanded []string `json:"expanded,omitempty"`
}

// WindowState is the UI state of one window (SPEC §10.10), in windows/<id>.json.
type WindowState struct {
	// Version is StateVersion; Save sets it.
	Version int `json:"version"`
	// Folder is the folder of the window, or empty for the window without one.
	Folder string `json:"folder,omitempty"`
	// Geometry is the size and state of the window; nil when there is none.
	Geometry *Geometry `json:"geometry,omitempty"`
	// Layout is the layout of the workbench (workbench.LayoutState as JSON); the store does not look into it.
	Layout json.RawMessage `json:"layout,omitempty"`
	// Grid is the editor groups with their editors; nil when none are kept.
	Grid *editorgroup.SerializedGrid `json:"grid,omitempty"`
	// Explorer is the state of the Explorer.
	Explorer *ExplorerState `json:"explorer,omitempty"`
	// Recent are the files the quick open lists first, the most recent first.
	Recent []string `json:"recent,omitempty"`
}

// LastWindow is the window the user last had open, which an empty command line opens again.
type LastWindow struct {
	// Folder is the folder of the window, or empty for the window without one.
	Folder string `json:"folder,omitempty"`
}

// OpenWindow is a window that is open: the id of its state (see WindowID and NewEmptyWindowID) and its folder.
// The global state keeps the list of them in the order the windows were made (SPEC §10.10).
type OpenWindow struct {
	// ID is the id of the state file and the backups of the window.
	ID string `json:"id"`
	// Folder is the folder of the window, or empty for a window without one.
	Folder string `json:"folder,omitempty"`
}

// GlobalState is the UI state that belongs to no window (SPEC §10.10), in state.json.
type GlobalState struct {
	// Version is StateVersion; the store sets it.
	Version int `json:"version"`
	// EmptyWindowID is the id of the window without a folder (see NewEmptyWindowID).
	EmptyWindowID string `json:"emptyWindowId,omitempty"`
	// LastWindow is the window that was open last; nil before any was.
	LastWindow *LastWindow `json:"lastWindow,omitempty"`
	// OpenWindows are the windows that are open, in the order they were made. A window that the user closes
	// leaves the list; the windows that are open when the application quits stay in it, so the next start
	// opens them again (window.restoreWindows).
	OpenWindows []OpenWindow `json:"openWindows,omitempty"`
	// Geometry is the size and state the last window had, for a window that has none of its own.
	Geometry *Geometry `json:"geometry,omitempty"`
	// Recent are the folders and files of File > Open Recent.
	Recent RecentlyOpened `json:"recent"`
}

// sanitize drops what a window state read from a file holds that cannot be used: a geometry that is not a
// size, expanded folders that are not below the folder, and relative paths of recent files.
func (w *WindowState) sanitize() {
	if w.Geometry != nil && !w.Geometry.Valid() {
		w.Geometry = nil
	}
	if w.Explorer != nil {
		var kept []string
		for _, rel := range w.Explorer.Expanded {
			if clean, ok := cleanRelative(rel); ok {
				kept = append(kept, clean)
			}
		}
		w.Explorer.Expanded = kept
	}
	var recent []string
	for _, p := range w.Recent {
		if filepath.IsAbs(p) {
			recent = append(recent, filepath.Clean(p))
		}
	}
	w.Recent = recent
}

// cleanRelative cleans a slash-separated path relative to a folder; it reports false for an empty path, an
// absolute one and one that leaves the folder.
func cleanRelative(rel string) (string, bool) {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

// ExpandedPaths turns the relative paths of the expanded folders into absolute ones below folder.
func (e *ExplorerState) ExpandedPaths(folder string) []string {
	if e == nil {
		return nil
	}
	out := make([]string, 0, len(e.Expanded))
	for _, rel := range e.Expanded {
		if clean, ok := cleanRelative(rel); ok {
			out = append(out, filepath.Join(folder, filepath.FromSlash(clean)))
		}
	}
	return out
}

// ExpandedRelative turns absolute paths of expanded folders into the relative, slash-separated ones that
// ExplorerState keeps, sorted; a path that is not below folder is left out.
func ExpandedRelative(folder string, paths []string) []string {
	var out []string
	for _, p := range paths {
		rel, err := filepath.Rel(folder, p)
		if err != nil {
			continue
		}
		if clean, ok := cleanRelative(filepath.ToSlash(rel)); ok {
			out = append(out, clean)
		}
	}
	slices.Sort(out)
	return out
}

// encodeState gives the JSON of a state file.
func encodeState(path string, v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, errors.NewErr("StateEncodeFailed", err).SetMetadata("path", path)
	}
	return data, nil
}

// readStateFile reads a state file into v and checks its version. A file that is not there is not an
// error: found is false. A file that cannot be read has the tag StateFileUnreadable, one that is not JSON of
// the shape of v StateFileCorrupt, and one with another version StateVersionUnsupported.
func readStateFile(path string, v any, version func() int) (found bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.NewErr("StateFileUnreadable", err).SetMetadata("path", path)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return false, errors.NewErr("StateFileCorrupt", err).SetMetadata("path", path)
	}
	if got := version(); got != StateVersion {
		return false, errors.New("StateVersionUnsupported", "the state file has another version than this program reads",
			"path", path, "version", got, "supported", StateVersion)
	}
	return true, nil
}
