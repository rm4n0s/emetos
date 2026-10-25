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
	// ZoomLevel is the zoom level the window had when it was saved, when it was not the one of window.zoomLevel; nil
	// when there is none. A state written before step 8.5 has none.
	ZoomLevel *float64 `json:"zoomLevel,omitempty"`
}

// LastWindow is the window the user last had open, which an empty command line opens again.
type LastWindow struct {
	// Folder is the folder of the window, or empty for the window without one.
	Folder string `json:"folder,omitempty"`
}

// TabKindWorkspace is the kind of a window tab that holds a workspace (a folder, or none). Step 11.6 adds the kinds
// of the extension tabs; a tab with no kind is a workspace.
const TabKindWorkspace = "workspace"

// WorkspaceTab is a window tab that is open (SPEC §10.14): the stable id of the tab, the id of the UI state of its
// workspace (see WindowID and NewEmptyWindowID) and its folder. The global state keeps the list of them in the order
// of the strip.
type WorkspaceTab struct {
	// ID is the stable id of the tab (NewTabID); it stays the same when the tab opens another folder. A tab read from
	// the openWindows of a state written before step 9b.1 has the id of its window.
	ID string `json:"id"`
	// Kind is the kind of the tab: TabKindWorkspace, or empty (the same). A tab of a kind that is not known is dropped
	// when the state is read.
	Kind string `json:"kind,omitempty"`
	// StateID is the id of the state file and the backups of the workspace of the tab.
	StateID string `json:"stateId,omitempty"`
	// Folder is the folder of the workspace, or empty for a tab without one.
	Folder string `json:"folder,omitempty"`
	// Profile is the profile the workspace used (SPEC §10.12), so that the next start opens it with the same
	// profile and reads its UI state from that profile's folder; empty is the default profile.
	Profile string `json:"profile,omitempty"`
}

// OpenWindow is a window of the list openWindows that an Emetos older than step 9b.1 wrote (one Glutter window per
// folder). It is read as a WorkspaceTab and not written any more.
type OpenWindow struct {
	// ID is the id of the state file and the backups of the window.
	ID string `json:"id"`
	// Folder is the folder of the window, or empty for a window without one.
	Folder string `json:"folder,omitempty"`
	// Profile is the profile the window used (SPEC §10.12).
	Profile string `json:"profile,omitempty"`
}

// GlobalState is the UI state that belongs to no window (SPEC §10.10), in state.json.
type GlobalState struct {
	// Version is StateVersion; the store sets it.
	Version int `json:"version"`
	// EmptyWindowID is the id of the window without a folder (see NewEmptyWindowID).
	EmptyWindowID string `json:"emptyWindowId,omitempty"`
	// LastWindow is the workspace that was active last; nil before any was.
	LastWindow *LastWindow `json:"lastWindow,omitempty"`
	// WorkspaceTabs are the window tabs that are open, in the order of the strip. A tab that the user closes leaves
	// the list; the tabs that are open when the application quits stay in it, so the next start opens them again
	// (window.restoreWindows, SPEC §10.14).
	WorkspaceTabs []WorkspaceTab `json:"workspaceTabs,omitempty"`
	// ActiveWorkspaceTab is the id of the tab that was active.
	ActiveWorkspaceTab string `json:"activeWorkspaceTab,omitempty"`
	// WindowGeometry is the size and state of the one window.
	WindowGeometry *Geometry `json:"windowGeometry,omitempty"`
	// OpenWindows and Geometry are what a state written before step 9b.1 holds: the store moves them to WorkspaceTabs
	// and WindowGeometry when it reads the file (when those are not there) and leaves them empty.
	OpenWindows []OpenWindow `json:"openWindows,omitempty"`
	Geometry    *Geometry    `json:"geometry,omitempty"`
	// Recent are the folders and files of File > Open Recent.
	Recent RecentlyOpened `json:"recent"`
	// LastProfiles is the profile last used with each folder, by the cleaned absolute path of the folder (SPEC
	// §10.12). A folder with no entry uses the default profile. A state written before step 8.6 has none.
	LastProfiles map[string]string `json:"lastProfiles,omitempty"`
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
