package settings

import (
	"context"
	"slices"
	"strings"
)

// Turning extensions on and off in the settings editor (step 31.1, SPEC §10.15): one check box per extension in the
// group Extensions > Installed, over the setting extensions.disabled.

// DisabledExtensionsKey is the setting that lists the extensions the user turned off.
const DisabledExtensionsKey = "extensions.disabled"

// InstalledExtensionsID is the id of the group of the extension rows, the first group under Extensions.
const InstalledExtensionsID = extensionsID + "/installed"

// ExtensionInfo is an extension as the settings editor lists it.
type ExtensionInfo struct {
	// ID is the id of the extension ("emetos.git").
	ID string
	// Name is its name ("Git").
	Name string
	// Description is what it does, in one sentence.
	Description string
}

// ExtensionRow is the row of an extension: the extension, and whether it is on.
type ExtensionRow struct {
	ExtensionInfo
	// Enabled is false while the id is in extensions.disabled. A disabled extension's row is modified.
	Enabled bool
}

// SetExtensions gives the model the extensions to list, in the order to list them (the order of the Extensions page).
func (m *UIModel) SetExtensions(list []ExtensionInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exts = slices.Clone(list)
}

// ExtensionRow is the row of an extension of SetExtensions; false for an id the model does not list.
func (m *UIModel) ExtensionRow(id string) (ExtensionRow, bool) {
	m.mu.Lock()
	i := slices.IndexFunc(m.exts, func(e ExtensionInfo) bool { return e.ID == id })
	var info ExtensionInfo
	if i >= 0 {
		info = m.exts[i]
	}
	m.mu.Unlock()
	if i < 0 {
		return ExtensionRow{}, false
	}
	disabled := Get[[]string](m.store, DisabledExtensionsKey, "")
	return ExtensionRow{ExtensionInfo: info, Enabled: !slices.Contains(disabled, id)}, true
}

// extensionRows are the rows of the extensions that keep accepts, in order.
func (m *UIModel) extensionRows(keep func(ExtensionRow) bool) []string {
	m.mu.Lock()
	exts := slices.Clone(m.exts)
	m.mu.Unlock()
	disabled := Get[[]string](m.store, DisabledExtensionsKey, "")
	var out []string
	for _, e := range exts {
		row := ExtensionRow{ExtensionInfo: e, Enabled: !slices.Contains(disabled, e.ID)}
		if keep == nil || keep(row) {
			out = append(out, e.ID)
		}
	}
	return out
}

// withExtensions puts the group Installed, with the ids, first under the group Extensions of cats (made at the end
// when there is none). Nothing changes when ids is empty.
func withExtensions(cats []*Category, ids []string) []*Category {
	if len(ids) == 0 {
		return cats
	}
	installed := &Category{ID: InstalledExtensionsID, Label: "Installed", Level: 2, Extensions: ids}
	for _, c := range cats {
		if c.ID == extensionsID {
			c.Children = append([]*Category{installed}, c.Children...)
			return cats
		}
	}
	return append(cats, &Category{ID: extensionsID, Label: "Extensions", Level: 1, Children: []*Category{installed}})
}

// matchesExtension tells whether every word is in the name, the id or the description of an extension.
func matchesExtension(e ExtensionInfo, words []string) bool {
	text := strings.ToLower(e.Name + " " + e.ID + " " + e.Description)
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// ExtensionRowPrefix starts the key of the Row of an extension, "@ext:emetos.git": the key is no setting's, and it is
// also the search that finds the row (Copy Setting ID gives it).
const ExtensionRowPrefix = ExtensionFilter

// ExtensionOfRowKey is the id of the extension of a row key, and whether the key is the row of an extension.
func ExtensionOfRowKey(key string) (string, bool) { return strings.CutPrefix(key, ExtensionRowPrefix) }

// ExtensionSettingRow is the row of an extension as a check box: the name as the category, "Enabled" as the label,
// the description with the id, checked while the extension is on and modified while it is off. It reports false
// for an id the model does not list.
func (m *UIModel) ExtensionSettingRow(id string) (Row, bool) {
	e, ok := m.ExtensionRow(id)
	if !ok {
		return Row{}, false
	}
	desc := "`" + id + "`"
	if e.Description != "" {
		desc = e.Description + " " + desc
	}
	return Row{
		Key: ExtensionRowPrefix + id, Category: e.Name, Label: "Enabled", Description: desc, Kind: KindBool,
		Value: e.Enabled, Default: true, Modified: !e.Enabled,
	}, true
}

// SetExtensionEnabled turns an extension on or off by writing extensions.disabled in the user's file: the id is taken
// out of the list or added to it, the list is kept sorted, and an empty list removes the setting. The Extensions page
// and the rows of the settings editor both write through it, so they always agree.
func SetExtensionEnabled(ctx context.Context, store *Store, id string, enable bool) error {
	list := slices.Clone(Get[[]string](store, DisabledExtensionsKey, ""))
	has := slices.Contains(list, id)
	switch {
	case enable && has:
		list = slices.DeleteFunc(list, func(s string) bool { return s == id })
	case !enable && !has:
		list = append(list, id)
	default:
		return nil
	}
	slices.Sort(list)
	if len(list) == 0 {
		return store.Remove(ctx, DisabledExtensionsKey, TargetUser)
	}
	values := make([]any, len(list))
	for i, s := range list {
		values[i] = s
	}
	return store.Update(ctx, DisabledExtensionsKey, values, TargetUser)
}
