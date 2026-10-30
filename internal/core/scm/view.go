package scm

import (
	"slices"
	"sort"
)

// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewService.ts (SCMViewService) and
// src/vs/workbench/contrib/scm/browser/scmViewPane.ts (the view mode and the sort key of the Changes view)

// compareLocked orders two repositories by the repositories' sort key (SCMViewService.compareRepositories).
func (m *Model) compareLocked(a, b *Source) int {
	if m.sortKey == SortDiscoveryTime {
		switch {
		case a.discovery < b.discovery:
			return -1
		case a.discovery > b.discovery:
			return 1
		}
		return 0
	}
	if m.sortKey == SortRepositoryPath && a.rootPath != "" && b.rootPath != "" {
		return ComparePaths(a.rootPath, b.rootPath)
	}
	c := compareFileNames(a.Name(), b.Name())
	if c == 0 && a.rootPath != "" && b.rootPath != "" {
		return ComparePaths(a.rootPath, b.rootPath)
	}
	return c
}

// insertLocked puts a source control in its place among the repositories.
func (m *Model) insertLocked(s *Source) {
	i := sort.Search(len(m.sources), func(i int) bool { return m.compareLocked(m.sources[i], s) >= 0 })
	m.sources = slices.Insert(m.sources, i, s)
}

func (m *Model) anySelectedLocked() bool {
	return slices.ContainsFunc(m.sources, func(s *Source) bool { return s.selection != -1 })
}

func (m *Model) maxSelectionLocked() int {
	max := -1
	for _, s := range m.sources {
		if s.selection > max {
			max = s.selection
		}
	}
	return max
}

func (m *Model) focusedLocked() *Source {
	for _, s := range m.sources {
		if s.focused {
			return s
		}
	}
	return nil
}

// setFocusLocked focuses a repository that is shown; the focus is what the active repository follows when it was set last.
func (m *Model) setFocusLocked(s *Source) bool {
	if s != nil && s.selection == -1 {
		return false
	}
	found := false
	for _, o := range m.sources {
		o.focused = o == s
		found = found || o.focused
	}
	if found {
		m.editorLatest = false
	}
	return found
}

// visibleLocked are the repositories that are shown: in the order they were selected when the repositories are sorted by
// discovery time (VS Code keeps that behavior), else in the order of the repositories.
func (m *Model) visibleLocked() []*Source {
	var out []*Source
	for _, s := range m.sources {
		if s.selection != -1 {
			out = append(out, s)
		}
	}
	if m.sortKey == SortDiscoveryTime {
		slices.SortStableFunc(out, func(a, b *Source) int { return a.selection - b.selection })
	}
	return out
}

// Visible are the repositories the Changes view shows (VS Code's visibleRepositories).
func (m *Model) Visible() []*Source {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.visibleLocked()
}

// IsVisible tells whether a repository is shown.
func (m *Model) IsVisible(s *Source) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !s.removed && s.selection != -1
}

// setVisibleLocked is the setter of visibleRepositories: the repositories of the list are shown, in that order, the others not.
// It moves the focus when the focused repository is no longer shown.
func (m *Model) setVisibleLocked(list []*Source) []Event {
	changed := false
	for _, s := range m.sources {
		i := slices.Index(list, s)
		switch {
		case i < 0 && s.selection != -1:
			s.selection = -1
			changed = true
		case i >= 0:
			if s.selection == -1 {
				changed = true
			}
			s.selection = i
		}
	}
	if !changed {
		return nil
	}
	events := []Event{{Kind: ViewChanged}}
	if f := m.focusedLocked(); f != nil && f.selection == -1 {
		var first *Source
		for _, s := range m.sources {
			if s.selection != -1 {
				first = s
				break
			}
		}
		m.setFocusLocked(first)
	}
	return events
}

// SetVisible shows the repositories of the list, in that order, and no others.
func (m *Model) SetVisible(list []*Source) {
	m.mu.Lock()
	events := m.setVisibleLocked(slices.DeleteFunc(slices.Clone(list), func(s *Source) bool { return s.removed }))
	m.mu.Unlock()
	m.notify(events...)
}

// ToggleVisibility shows or hides a repository: with visible nil it flips, else it sets (VS Code's toggleVisibility). With the
// single selection mode, showing one shows it alone.
func (m *Model) ToggleVisibility(s *Source, visible *bool) {
	m.mu.Lock()
	if s.removed {
		m.mu.Unlock()
		return
	}
	now := s.selection != -1
	want := !now
	if visible != nil {
		want = *visible
		if want == now {
			m.mu.Unlock()
			return
		}
	}
	vis := m.visibleLocked()
	var events []Event
	if want {
		if m.cfg.SelectionMode == SelectSingle {
			events = m.setVisibleLocked([]*Source{s})
		} else {
			events = m.setVisibleLocked(append(slices.Clone(vis), s))
		}
	} else if i := slices.Index(vis, s); i >= 0 {
		events = m.setVisibleLocked(slices.Delete(slices.Clone(vis), i, i+1))
	}
	m.mu.Unlock()
	m.notify(events...)
}

// applySelectionModeLocked applies a change of the selection mode: to single keeps the first repository shown, to multiple shows
// every repository.
func (m *Model) applySelectionModeLocked() []Event {
	vis := m.visibleLocked()
	switch {
	case m.cfg.SelectionMode == SelectSingle && len(vis) > 1:
		return m.setVisibleLocked(vis[:1])
	case m.cfg.SelectionMode == SelectMultiple && len(m.sources) > 1:
		return m.setVisibleLocked(slices.Clone(m.sources))
	}
	return nil
}

// SetSelectionMode sets the selection mode as a change of scm.repositories.selectionMode does.
func (m *Model) SetSelectionMode(mode SelectionMode) {
	c := m.Config()
	c.SelectionMode = mode
	m.SetConfig(c)
}

// SelectionMode is the selection mode (scm.repositories.selectionMode).
func (m *Model) SelectionMode() SelectionMode {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.SelectionMode
}

// RepositorySortKey is the order of the repositories.
func (m *Model) RepositorySortKey() RepositorySortKey {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sortKey
}

// SetRepositorySortKey sets the order of the repositories and sorts them (VS Code's toggleSortKey).
func (m *Model) SetRepositorySortKey(key RepositorySortKey) {
	m.mu.Lock()
	m.sortKey = key
	slices.SortStableFunc(m.sources, m.compareLocked)
	m.mu.Unlock()
	m.notify(Event{Kind: ViewChanged})
}

// Focus makes a repository that is shown the focused one, as the user does by clicking its header or its input; the active
// repository follows the focus unless the active editor changed after it. A repository that is not shown is not focused.
func (m *Model) Focus(s *Source) {
	m.mu.Lock()
	ok := !s.removed && m.setFocusLocked(s)
	m.mu.Unlock()
	if ok {
		m.notify(Event{Kind: ViewChanged, Source: s})
	}
}

// Focused is the focused repository, or nil.
func (m *Model) Focused() *Source {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.focusedLocked()
}

// Pin makes a repository the active one whatever the focus and the active editor say; nil unpins (Auto).
func (m *Model) Pin(s *Source) {
	m.mu.Lock()
	if s != nil && s.removed {
		m.mu.Unlock()
		return
	}
	m.pinned = s
	m.mu.Unlock()
	m.notify(Event{Kind: ViewChanged, Source: s})
}

// SetActiveResource tells the model which file the active editor shows ("" for none, or a file that is not a file:// URI). The
// repository of the file becomes the one the active repository follows, until the focus changes; a file in no repository leaves
// the last one in place (VS Code's derivedObservableWithCache).
func (m *Model) SetActiveResource(uri string) {
	m.mu.Lock()
	m.activeURI = uri
	var repo *Source
	if uri != "" && len(uri) >= 7 && uri[:7] == "file://" {
		repo = m.repositoryLocked(PathOfURI(uri))
	}
	if repo != nil {
		m.editorRepo = repo
		m.editorLatest = true
	}
	m.mu.Unlock()
	m.notify(Event{Kind: ViewChanged})
}

// Active is the active repository and whether it is pinned: the pinned one, else the one that was last made the focused one or
// the repository of the active editor's file (SCMViewService.activeRepository). It is the one the status bar shows.
func (m *Model) Active() (src *Source, pinned bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeLocked()
}

func (m *Model) activeLocked() (*Source, bool) {
	if m.pinned != nil {
		return m.pinned, true
	}
	focused := m.focusedLocked()
	first, second := focused, m.editorRepo
	if m.editorLatest {
		first, second = m.editorRepo, focused
	}
	if first != nil {
		return first, false
	}
	return second, false
}

// ViewMode is how the Changes view shows the resources.
func (m *Model) ViewMode() ViewMode {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.viewMode
}

// SetViewMode is the choice of the user (workbench.scm.action.setListViewMode and setTreeViewMode). The sort key follows: path
// in the tree, the one chosen for the list or the default in the list.
func (m *Model) SetViewMode(mode ViewMode) {
	m.mu.Lock()
	if mode != ViewTree {
		mode = ViewList
	}
	m.viewChosen = true
	if m.viewMode == mode {
		m.mu.Unlock()
		return
	}
	m.viewMode = mode
	m.viewSortKey = m.computeSortKey()
	m.mu.Unlock()
	m.notify(Event{Kind: ViewChanged})
}

// computeSortKey is the sort key the view mode implies (getViewSortKey).
func (m *Model) computeSortKey() SortKey {
	if m.viewMode == ViewTree {
		return SortByPath
	}
	if m.hasStoredKey {
		return m.storedSort
	}
	return m.cfg.DefaultViewSortKey
}

// SortKey is the order of the resources in the list view (always path in the tree view).
func (m *Model) SortKey() SortKey {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.viewSortKey
}

// SetSortKey is the choice of the user (workbench.scm.action.setSortKey.*); it is remembered for the list view.
func (m *Model) SetSortKey(key SortKey) {
	m.mu.Lock()
	key = ParseSortKey(string(key))
	if m.viewSortKey == key {
		m.mu.Unlock()
		return
	}
	m.viewSortKey = key
	if m.viewMode == ViewList {
		m.storedSort, m.hasStoredKey = key, true
	}
	m.mu.Unlock()
	m.notify(Event{Kind: ViewChanged})
}

// CompactFolders is scm.compactFolders.
func (m *Model) CompactFolders() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.CompactFolders
}
