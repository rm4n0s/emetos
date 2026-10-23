package settings

import (
	"slices"
	"sort"
)

// Target is a settings file that can be written.
type Target int

const (
	// TargetUser is the user's settings.json of the profile.
	TargetUser Target = iota + 1
	// TargetWorkspace is <folder>/.emetos/settings.json.
	TargetWorkspace
)

func (t Target) String() string {
	switch t {
	case TargetUser:
		return "user"
	case TargetWorkspace:
		return "workspace"
	}
	return "unknown"
}

// ChangeEvent tells which settings a reload or an update changed.
type ChangeEvent struct {
	// Targets are the files whose settings changed.
	Targets []Target
	// Keys are the dotted keys whose value changed, in no language, sorted.
	Keys []string
	// Languages are, for each language that has blocks of its own, the keys whose value
	// changed for that language.
	Languages map[string][]string
}

// IsEmpty tells whether nothing changed.
func (e ChangeEvent) IsEmpty() bool { return len(e.Keys) == 0 && len(e.Languages) == 0 }

// Affects tells whether the change touches a section, such as "editor" or
// "editor.tabSize": the section is a changed key, or contains one, or is inside one
// (a changed "files.exclude" affects "files.exclude.node_modules"). With a language it
// looks at the value for that language; with "" at the value in any language.
//
// Ported from VS Code: src/vs/platform/configuration/common/configurationModels.ts (ConfigurationChangeEvent.affectsConfiguration).
func (e ChangeEvent) Affects(section, languageID string) bool {
	if section == "" {
		return false
	}
	touches := func(keys []string) bool {
		return slices.ContainsFunc(keys, func(k string) bool { return inSection(k, section) || inSection(section, k) })
	}
	if languageID == "" {
		if touches(e.Keys) {
			return true
		}
		for _, keys := range e.Languages {
			if touches(keys) {
				return true
			}
		}
		return false
	}
	if keys, ok := e.Languages[languageID]; ok {
		return touches(keys)
	}
	return touches(e.Keys)
}

// diff compares two consolidated models.
func (r *Registry) diff(old, updated *Model) ChangeEvent {
	var ev ChangeEvent
	changedKeys := func(a, b *Model) []string {
		set := map[string]bool{}
		for _, k := range r.flatten(a.contents) {
			set[k] = true
		}
		for _, k := range r.flatten(b.contents) {
			set[k] = true
		}
		var out []string
		for k := range set {
			av, aok := a.GetValue(k)
			bv, bok := b.GetValue(k)
			if aok != bok || !equalValues(av, bv) {
				out = append(out, k)
			}
		}
		sort.Strings(out)
		return out
	}
	ev.Keys = changedKeys(old, updated)
	ids := old.OverrideIdentifiers()
	for _, id := range updated.OverrideIdentifiers() {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		keys := changedKeys(old.Override(id), updated.Override(id))
		if len(keys) == 0 {
			continue
		}
		if ev.Languages == nil {
			ev.Languages = map[string][]string{}
		}
		ev.Languages[id] = keys
	}
	return ev
}
