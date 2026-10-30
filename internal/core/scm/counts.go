package scm

import "strconv"

// ActivityBadge is the number on the Source Control icon of the activity bar (scm.countBadge, VS Code's activity.ts): the sum of
// the badge counts of the repositories that are shown ("all"), the count of the active repository ("focused"), or none ("off").
// The count of a repository is what the provider gave, else the number of its resources. The badge is not drawn for 0.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/activity.ts (SCMActiveRepositoryController, _countBadge)
func (m *Model) ActivityBadge() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.cfg.CountBadge {
	case CountAll:
		total := 0
		for _, s := range m.visibleLocked() {
			total += s.badgeLocked()
		}
		return total
	case CountFocused:
		if s, _ := m.activeLocked(); s != nil {
			return s.badgeLocked()
		}
	}
	return 0
}

// PendingChangesLabel is the tooltip of the badge on the Source Control icon.
func PendingChangesLabel(count int) string { return strconv.Itoa(count) + " pending changes" }

// ProviderCountVisible tells whether the count badge of a repository is drawn in its header (scm.providerCountBadge): never for
// "hidden", when it is not 0 for "auto", always for "visible".
func (m *Model) ProviderCountVisible(s *Source) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.cfg.ProviderCountBadge {
	case ProviderCountVisible:
		return true
	case ProviderCountAuto:
		return s.badgeLocked() != 0
	}
	return false
}
