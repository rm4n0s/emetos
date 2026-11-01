package scm

// Ported from VS Code: src/vs/workbench/contrib/scm/browser/menus.ts (the overlays of the context key service),
// src/vs/workbench/contrib/scm/browser/activity.ts (scmActiveRepositoryName, scmActiveResourceHasChanges,
// scmActiveResourceRepository), src/vs/workbench/contrib/scm/common/scmService.ts (scm.providerCount) and
// src/vs/workbench/contrib/scm/browser/scmViewPane.ts, scmViewService.ts (the keys of the view)

// Context holds the values of the context keys that belong to the whole model, for the window's scope: scm.providerCount,
// scm.historyProviderCount, scmRepositoryCount, scmRepositoryVisibleCount, scmRepositorySortKey, scmRepositorySelectionMode,
// scmViewMode, scmViewSortKey, scmActiveRepositoryName, scmActiveRepositoryBranchName, scmActiveResourceHasChanges and
// scmActiveResourceRepository. The host sets them again after each event.
func (m *Model) Context() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	active, _ := m.activeLocked()
	name := ""
	if active != nil {
		name = active.Name()
		if active.activeName != "" {
			name = active.activeName
		}
	}
	hasChanges, repo := false, ""
	if m.activeURI != "" && len(m.activeURI) >= 7 && m.activeURI[:7] == "file://" {
		p := PathOfURI(m.activeURI)
		if s := m.repositoryLocked(p); s != nil {
			repo = s.key
			for _, g := range s.groups {
				for i := range g.resources {
					if g.resources[i].Path == p {
						hasChanges = true
						break
					}
				}
				if hasChanges {
					break
				}
			}
		}
	}
	return map[string]any{
		"scm.providerCount":             len(m.sources),
		"scm.historyProviderCount":      m.historyCountLocked(),
		"scmRepositoryCount":            len(m.sources),
		"scmRepositoryVisibleCount":     len(m.visibleLocked()),
		"scmRepositorySortKey":          string(m.sortKey),
		"scmRepositorySelectionMode":    string(m.cfg.SelectionMode),
		"scmViewMode":                   string(m.viewMode),
		"scmViewSortKey":                string(m.viewSortKey),
		"scmActiveRepositoryName":       name,
		"scmActiveRepositoryBranchName": m.activeBranchLocked(),
		"scmActiveResourceHasChanges":   hasChanges,
		"scmActiveResourceRepository":   repo,
	}
}

// Context holds the values of the context keys for the menus of the source control: scmProvider (the id of the provider),
// scmProviderRootUri and scmProviderHasRootUri, and scmProviderContext when the provider gave a context value.
func (s *Source) Context() map[string]any {
	c := map[string]any{
		"scmProvider":           s.providerID,
		"scmProviderRootUri":    s.rootURI,
		"scmProviderHasRootUri": s.rootURI != "",
	}
	if s.contextValue != "" {
		c["scmProviderContext"] = s.contextValue
	}
	return c
}

// Context holds the values of the context keys for the menus of the group: those of its source control, scmResourceGroup (the
// id), scmResourceGroupState (the context value, when it has one) and multiDiffEditorEnableViewChanges.
func (g *Group) Context() map[string]any {
	c := g.src.Context()
	c["scmResourceGroup"] = g.id
	if v := g.ContextValue(); v != "" {
		c["scmResourceGroupState"] = v
	}
	c["multiDiffEditorEnableViewChanges"] = false
	return c
}

// Context holds the values of the context keys for the menus of the resource: those of its group and scmResourceState (the
// context value, when it has one).
func (r *Resource) Context() map[string]any {
	c := r.Group.Context()
	if r.ContextValue != "" {
		c["scmResourceState"] = r.ContextValue
	}
	return c
}
