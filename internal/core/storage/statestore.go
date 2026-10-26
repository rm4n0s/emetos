package storage

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// StateStoreOptions say how a StateStore writes.
type StateStoreOptions struct {
	// Delay is how long after a change a file is written; DefaultStateDelay when zero.
	Delay time.Duration
	// After sets the timers of the delay; the system clock when nil.
	After AfterFunc
	// WriteFile replaces a file; WriteFileAtomic when nil.
	WriteFile WriteFileFunc
	// Logger gets the failures of writes that nobody waits for; slog.Default() when nil.
	Logger *slog.Logger
}

// StateStore reads and writes the UI state files of a profile (SPEC §10.10): the global state, state.json, and
// the state of each window, windows/<id>.json. Reads happen when asked for; writes are debounced per file (see
// Delay), atomic, and flushed with Flush. A file that cannot be used (unreadable, not JSON, another version)
// is reported by the read and then written over by the next save. It is safe for concurrent use.
type StateStore struct {
	paths   Paths
	profile string
	opts    StateStoreOptions

	mu      sync.Mutex
	global  *GlobalState
	writers map[string]*writer
}

// NewStateStore makes the store of a profile. It reads and writes nothing yet. The error has the tag
// ProfilePathUnsafe.
func NewStateStore(paths Paths, profile string, opts StateStoreOptions) (*StateStore, error) {
	if _, err := paths.StateDir(profile); err != nil {
		return nil, err
	}
	if opts.Delay <= 0 {
		opts.Delay = DefaultStateDelay
	}
	if opts.After == nil {
		opts.After = realAfter
	}
	if opts.WriteFile == nil {
		opts.WriteFile = WriteFileAtomic
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &StateStore{paths: paths, profile: profile, opts: opts, writers: map[string]*writer{}}, nil
}

// writerFor is the writer of a file, made the first time. The caller holds mu.
func (s *StateStore) writerFor(path string) *writer {
	w := s.writers[path]
	if w == nil {
		w = newWriter(path, s.opts)
		s.writers[path] = w
	}
	return w
}

// LoadGlobal reads the global state once and keeps it: later calls give the kept state, as changed by
// UpdateGlobal. A file that is not there is an empty state. A file that cannot be used also gives an empty
// state, together with the error (tags StateFileUnreadable, StateFileCorrupt, StateVersionUnsupported); the
// next UpdateGlobal replaces the file.
func (s *StateStore) LoadGlobal(ctx context.Context) (GlobalState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.global != nil {
		return cloneGlobal(*s.global), nil
	}
	if err := ctx.Err(); err != nil {
		// nothing was read, so nothing is kept: an update must not write an empty state over the file
		return GlobalState{}, errors.NewErr("StateFileUnreadable", err)
	}
	g, err := s.readGlobal(ctx)
	s.global = &g
	return cloneGlobal(g), err
}

func (s *StateStore) readGlobal(ctx context.Context) (GlobalState, error) {
	var g GlobalState
	path, err := s.paths.GlobalStatePath(s.profile)
	if err != nil {
		return g, err
	}
	var read GlobalState
	found, err := readStateFile(path, &read, func() int { return read.Version })
	if err != nil || !found {
		return g, err
	}
	read.Recent.Folders = cleanAbsolute(read.Recent.Folders)
	read.Recent.Files = cleanAbsolute(read.Recent.Files)
	if read.Geometry != nil && !read.Geometry.Valid() {
		read.Geometry = nil
	}
	if read.WindowGeometry != nil && !read.WindowGeometry.Valid() {
		read.WindowGeometry = nil
	}
	if read.EmptyWindowID != "" && !validWindowID(read.EmptyWindowID) {
		read.EmptyWindowID = ""
	}
	if read.LastWindow != nil && read.LastWindow.Folder != "" && !filepath.IsAbs(read.LastWindow.Folder) {
		read.LastWindow = nil
	}
	read.OpenWindows = cleanOpenWindows(read.OpenWindows)
	read.WorkspaceTabs = cleanWorkspaceTabs(read.WorkspaceTabs)
	migrateWindows(&read)
	read.LastProfiles = cleanLastProfiles(read.LastProfiles)
	return read, nil
}

// migrateWindows reads what an Emetos older than step 9b.1 wrote as what it is now (SPEC §10.14): the openWindows list
// is the list of the tabs, in the same order, when the file has no workspaceTabs, and the geometry of the last window
// is the geometry of the window when the file has no windowGeometry. The old fields are emptied, so that they are not
// written again.
func migrateWindows(g *GlobalState) {
	if len(g.WorkspaceTabs) == 0 {
		for _, w := range g.OpenWindows {
			g.WorkspaceTabs = append(g.WorkspaceTabs, WorkspaceTab{ID: w.ID, StateID: w.ID, Folder: w.Folder, Profile: w.Profile})
		}
	}
	g.OpenWindows = nil
	if g.WindowGeometry == nil {
		g.WindowGeometry = g.Geometry
	}
	g.Geometry = nil
	if g.ActiveWorkspaceTab != "" && !slices.ContainsFunc(g.WorkspaceTabs, func(t WorkspaceTab) bool { return t.ID == g.ActiveWorkspaceTab }) {
		g.ActiveWorkspaceTab = ""
	}
}

// cleanWorkspaceTabs drops the tabs of a list whose id is not an id or is there already, whose kind is not known, and
// the folders that are not absolute paths; a tab with no state id gets the one its folder makes.
func cleanWorkspaceTabs(list []WorkspaceTab) []WorkspaceTab {
	var out []WorkspaceTab
	seen := map[string]bool{}
	for _, t := range list {
		extensionTab := t.Kind != "" && t.Kind != TabKindWorkspace
		if !validWindowID(t.ID) || seen[t.ID] || (extensionTab && !ValidTabKind(t.Kind)) {
			continue
		}
		seen[t.ID] = true
		if extensionTab {
			// the tab of an extension has no folder, state id or profile; its state must be JSON
			t.Folder, t.StateID, t.Profile = "", "", ""
			if len(t.State) > 0 && !json.Valid(t.State) {
				t.State = nil
			}
			out = append(out, t)
			continue
		}
		t.Kind, t.Title, t.State = "", "", nil
		if t.Folder != "" {
			if !filepath.IsAbs(t.Folder) {
				continue
			}
			t.Folder = filepath.Clean(t.Folder)
			if id, err := WindowID(t.Folder); err == nil {
				t.StateID = id
			}
		}
		if t.StateID != "" && !validWindowID(t.StateID) {
			t.StateID = ""
		}
		if t.Profile != "" && !ValidProfileName(t.Profile) {
			t.Profile = ""
		}
		out = append(out, t)
	}
	return out
}

// cleanOpenWindows drops the windows of a list whose id is not an id or is there already, and the folders that
// are not absolute paths.
func cleanOpenWindows(list []OpenWindow) []OpenWindow {
	var out []OpenWindow
	seen := map[string]bool{}
	for _, w := range list {
		if !validWindowID(w.ID) || seen[w.ID] {
			continue
		}
		seen[w.ID] = true
		if w.Folder != "" {
			if !filepath.IsAbs(w.Folder) {
				continue
			}
			w.Folder = filepath.Clean(w.Folder)
		}
		if w.Profile != "" && !ValidProfileName(w.Profile) {
			w.Profile = ""
		}
		out = append(out, w)
	}
	return out
}

// cleanLastProfiles drops the entries of the profile of each folder whose folder is not an absolute path or whose
// profile is not a valid name, and cleans the paths.
func cleanLastProfiles(m map[string]string) map[string]string {
	var out map[string]string
	for folder, profile := range m {
		if !filepath.IsAbs(folder) || !ValidProfileName(profile) {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[filepath.Clean(folder)] = profile
	}
	return out
}

// cleanAbsolute cleans the paths and drops the ones that are not absolute and the repeated ones, and keeps
// at most MaxRecentEntries.
func cleanAbsolute(list []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range list {
		if !filepath.IsAbs(p) {
			continue
		}
		p = filepath.Clean(p)
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) == MaxRecentEntries {
			break
		}
	}
	return out
}

func cloneGlobal(g GlobalState) GlobalState {
	g.Recent = g.Recent.Clone()
	if g.Geometry != nil {
		geo := *g.Geometry
		g.Geometry = &geo
	}
	if g.WindowGeometry != nil {
		geo := *g.WindowGeometry
		g.WindowGeometry = &geo
	}
	if g.LastWindow != nil {
		last := *g.LastWindow
		g.LastWindow = &last
	}
	g.WorkspaceTabs = slices.Clone(g.WorkspaceTabs)
	g.OpenWindows = slices.Clone(g.OpenWindows)
	g.LastProfiles = maps.Clone(g.LastProfiles)
	return g
}

// UpdateGlobal changes the global state with fn, which gets the kept state (read first when it was not) and
// tells whether it changed it; a change is written after the delay, and nothing is written when fn says there
// was none. fn runs with the store locked: it must not call the store. The error has the tags of LoadGlobal,
// which are not fatal here (the update goes on from an empty state), and StateEncodeFailed.
func (s *StateStore) UpdateGlobal(ctx context.Context, fn func(*GlobalState) (changed bool)) error {
	_, loadErr := s.LoadGlobal(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.global == nil {
		return loadErr // the state could not be read at all (ctx is done): it is not written over
	}
	if !fn(s.global) {
		return loadErr
	}
	s.global.Version = StateVersion
	path, err := s.paths.GlobalStatePath(s.profile)
	if err != nil {
		return err
	}
	data, err := encodeState(path, s.global)
	if err != nil {
		return err
	}
	// set under the lock, so the updates reach the writer in the order they were made
	s.writerFor(path).Set(data)
	return loadErr
}

// EmptyWindowID is the id of the window without a folder: the one the global state keeps, made and kept the
// first time. The error has the tags of NewEmptyWindowID and of UpdateGlobal.
func (s *StateStore) EmptyWindowID(ctx context.Context) (string, error) {
	g, loadErr := s.LoadGlobal(ctx)
	if g.EmptyWindowID != "" {
		return g.EmptyWindowID, nil
	}
	id, err := NewEmptyWindowID()
	if err != nil {
		return "", err
	}
	if err := s.UpdateGlobal(ctx, func(g *GlobalState) bool { g.EmptyWindowID = id; return true }); err != nil {
		return id, err
	}
	return id, loadErr
}

// LoadWindow reads the state of the window with the id. found is false when the window has no state yet. A file
// that cannot be used gives an empty state and the error (see LoadGlobal); the next SaveWindow replaces it.
func (s *StateStore) LoadWindow(ctx context.Context, id string) (st WindowState, found bool, err error) {
	if err := ctx.Err(); err != nil {
		return st, false, errors.NewErr("StateFileUnreadable", err)
	}
	path, err := s.paths.WindowStatePath(s.profile, id)
	if err != nil {
		return st, false, err
	}
	var read WindowState
	found, err = readStateFile(path, &read, func() int { return read.Version })
	if err != nil || !found {
		return st, false, err
	}
	read.sanitize()
	return read, true, nil
}

// SaveWindow encodes the state of the window with the id and has it written after the delay. The caller may go
// on using st. The error has the tags WindowIDInvalid and StateEncodeFailed.
func (s *StateStore) SaveWindow(id string, st WindowState) error {
	path, err := s.paths.WindowStatePath(s.profile, id)
	if err != nil {
		return err
	}
	st.Version = StateVersion
	data, err := encodeState(path, st)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writerFor(path).Set(data)
	return nil
}

// DeleteWindow removes the state file of the window with the id, and what waits to be written to it: the window is
// gone for good (an extra window without a folder that the user closed). A file that is not there is not an error.
// The error has the tags WindowIDInvalid and StateDeleteFailed.
func (s *StateStore) DeleteWindow(id string) error {
	path, err := s.paths.WindowStatePath(s.profile, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if w := s.writers[path]; w != nil {
		w.Discard()
	}
	s.mu.Unlock()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return errors.NewErr("StateDeleteFailed", err).SetMetadata("path", path)
	}
	return nil
}

// Flush writes every file that waits, now. It tries them all and returns the first error (tag
// StateWriteFailed, or StateFlushCancelled).
func (s *StateStore) Flush(ctx context.Context) error {
	s.mu.Lock()
	writers := make([]*writer, 0, len(s.writers))
	for _, w := range s.writers {
		writers = append(writers, w)
	}
	s.mu.Unlock()
	var first error
	for _, w := range writers {
		if err := w.Flush(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Pending tells whether any file waits to be written.
func (s *StateStore) Pending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.writers {
		if w.Pending() {
			return true
		}
	}
	return false
}
