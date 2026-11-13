package app

import (
	"encoding/json"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// What the extensions need from the system, for the window (SPEC §10.36 "Checking", step 34.4): one checker of the requirements per
// window, made when a page first needs it, whose folder and settings follow the active workspace tab, and the actions of the buttons
// of the requirements on the pages.

// windowRequirements is the checker of the window and what it follows. It is used on the UI thread only.
type windowRequirements struct {
	checker *exthost.Requirements
	// source is the workspace whose folder and settings the checks use, and stopSettings stops following its settings.
	source       *Workspace
	stopSettings func()
	stopTabs     func()
}

// requirements is the checker of the window, made at the first call; post is the binding's Post, through which it tells the
// pages of new results. It runs on the UI thread.
func (m *Workspaces) requirements(post func(func())) *exthost.Requirements {
	if m.reqs != nil {
		return m.reqs.checker
	}
	r := &windowRequirements{}
	r.checker = exthost.NewRequirements(exthost.RequirementsOptions{
		Extensions: m.p.Extensions.Manifests(), Logger: m.p.Logger, Post: post,
		State: func(extensionID, folder, key string) (json.RawMessage, bool) {
			if w := m.launchTarget(); w != nil {
				return w.extensionStateGet(extensionID, folder, key)
			}
			return nil, false
		},
	})
	m.reqs = r
	m.followRequirementSource()
	r.stopTabs = m.OnChange(m.followRequirementSource)
	return r.checker
}

// followRequirementSource points the checker at the workspace tab the user works in (the active one, or the workspace tab used
// last when the tab in front is an extension's) and follows the changes of its settings: a path setting that changes is checked
// again.
func (m *Workspaces) followRequirementSource() {
	r := m.reqs
	if r == nil {
		return
	}
	w := m.launchTarget()
	if w != r.source {
		if r.stopSettings != nil {
			r.stopSettings()
			r.stopSettings = nil
		}
		r.source = w
		if w != nil && w.Settings != nil {
			checker := r.checker
			r.stopSettings = w.Settings.OnChange(func(e settings.ChangeEvent) { checker.SettingsChanged(e.Keys) })
		}
	}
	if w == nil {
		r.checker.SetSource("", nil)
		return
	}
	r.checker.SetSource(w.Folder, exthost.SettingReader(w.Settings))
}

// closeRequirements stops the checks of the window and what the checker follows.
func (m *Workspaces) closeRequirements() {
	r := m.reqs
	if r == nil {
		return
	}
	m.reqs = nil
	if r.stopTabs != nil {
		r.stopTabs()
	}
	if r.stopSettings != nil {
		r.stopSettings()
	}
	r.checker.Close()
}

// setRequirementPath opens the Settings editor of the user on the key of a requirement's path setting (Set Path...).
func (s *shellState) setRequirementPath(key string) {
	s.openSettingsEditor(editorgroup.SettingsUser, settings.IDFilter+key)
}
