package scm

import "slices"

// The state of the Source Control view that outlives the window (SPEC §10.30): which repositories are shown and in what order,
// the order of the repositories, and the view mode and sort key the user chose. The window keeps it in its UI state file; it is
// read before the extensions create their source controls, and a source control that is added later finds its place in it by
// its provider and root.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewService.ts (the storage key scm:view:visibleRepositories,
// ISCMViewServiceState) and src/vs/workbench/contrib/scm/browser/scmViewPane.ts (scm.viewMode, scm.viewSortKey)

// State is the saved state of a Model.
type State struct {
	// All are the keys of the source controls the model had when the state was taken, in the order of the repositories, and
	// Visible the places in All of those that were shown, in the order they were selected (ISCMViewServiceState).
	All     []string `json:"all,omitempty"`
	Visible []int    `json:"visible,omitempty"`
	// RepositoriesSortKey is the order of the repositories the user chose.
	RepositoriesSortKey RepositorySortKey `json:"repositoriesSortKey,omitempty"`
	// ViewMode is the view mode the user chose, and ViewSortKey the sort key the user chose for the list; both empty when the
	// user chose none.
	ViewMode    ViewMode `json:"viewMode,omitempty"`
	ViewSortKey SortKey  `json:"viewSortKey,omitempty"`
}

// StorageKey is how the saved state names a source control: its provider and its root folder (VS Code's
// getProviderStorageKey).
func (s *Source) StorageKey() string { return s.providerID + "\x00" + s.rootURI }

// State is the state to save. A model that has no source control yet gives the state it was given, so that a window that is
// closed before its extensions started does not lose it.
func (m *Model) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := State{RepositoriesSortKey: m.sortKey}
	if m.viewChosen {
		st.ViewMode = m.viewMode
	}
	if m.hasStoredKey {
		st.ViewSortKey = m.storedSort
	}
	if len(m.sources) == 0 && m.restored != nil {
		st.All, st.Visible = m.restored.All, m.restored.Visible
		if st.ViewMode == "" {
			st.ViewMode = m.restored.ViewMode
		}
		return st
	}
	for _, s := range m.sources {
		st.All = append(st.All, s.StorageKey())
	}
	for _, s := range m.visibleLocked() {
		st.Visible = append(st.Visible, slices.Index(st.All, s.StorageKey()))
	}
	return st
}

// Restore gives the model the saved state, before the source controls are added: the view mode and the sort keys apply at once,
// and Add shows a source control that the state knows when the state shows it, and one it does not know as a new repository is
// shown. With the single selection mode only the first of the repositories that were shown is restored.
func (m *Model) Restore(st State) {
	m.mu.Lock()
	if m.cfg.SelectionMode == SelectSingle && len(st.Visible) > 1 {
		st.Visible = st.Visible[:1]
	}
	m.restored, m.restoreOpen, m.didSelect = &st, true, false
	if st.RepositoriesSortKey != "" {
		m.sortKey = st.RepositoriesSortKey
	}
	if st.ViewMode == ViewList || st.ViewMode == ViewTree {
		m.viewMode, m.viewChosen = st.ViewMode, true
	}
	if st.ViewSortKey != "" {
		m.storedSort, m.hasStoredKey = ParseSortKey(string(st.ViewSortKey)), true
	}
	m.viewSortKey = m.computeSortKey()
	m.mu.Unlock()
	m.notify(Event{Kind: ViewChanged})
}

// FinishRestore ends the time in which source controls that are added are matched with the saved state (VS Code waits 5 seconds
// after the last repository came, for the extensions to finish finding them). The host calls it after that time.
func (m *Model) FinishRestore() {
	m.mu.Lock()
	m.restoreOpen = false
	m.mu.Unlock()
}
