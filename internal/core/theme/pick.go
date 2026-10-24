package theme

import (
	"slices"
	"strings"
)

// The lists the theme pickers show (SPEC §10.8).
//
// Ported from VS Code: src/vs/workbench/contrib/themes/browser/themes.contribution.ts (toEntry, toEntries and
// the groups of SelectColorThemeAction and SelectFileIconThemeAction)

// Titles of the groups of the color theme picker.
const (
	// PickLightThemes is the title of the group of the light themes.
	PickLightThemes = "light themes"
	// PickDarkThemes is the title of the group of the dark themes.
	PickDarkThemes = "dark themes"
	// PickHighContrastThemes is the title of the group of the high contrast themes.
	PickHighContrastThemes = "high contrast themes"
	// PickFileIconThemes is the title of the group of the file icon theme picker.
	PickFileIconThemes = "file icon themes"
)

// PickerEntry is a theme in the color theme picker.
type PickerEntry struct {
	// Info describes the theme; its ID is the id of the item.
	Info Info
	// Value is what the setting workbench.colorTheme holds for the theme: its label.
	Value string
	// Description is the text after the label: "Default Dark" for the default dark theme, the id of the
	// others when it adds something.
	Description string
	// Current tells that the theme is the one in use.
	Current bool
}

// PickerGroup is a group of themes under a title.
type PickerGroup struct {
	// Title is the text of the separator above the group.
	Title string
	// Entries are the themes of the group.
	Entries []PickerEntry
}

// PickDescription is the description of a theme in the picker (VS Code's toEntry): "Default Dark" and "Default
// Light" for Dark Modern and Light Modern, the id of a theme whose id is not its label (the "Default High
// Contrast" of Dark High Contrast), "Default " and the id for the other bundled themes, and nothing for a user
// theme whose label is its id.
func PickDescription(info Info) string {
	switch {
	case info.Bundled && info.ID == DefaultDark:
		return "Default Dark"
	case info.Bundled && info.ID == DefaultLight:
		return "Default Light"
	case info.ID == info.Label && info.Bundled:
		return "Default " + info.ID
	case info.ID == info.Label:
		return ""
	}
	return info.ID
}

// PickerGroups lists the themes that can be chosen in the groups of the picker, in its order: the light
// themes, the dark themes and the high contrast ones (both kinds), a group that has no theme left out. In each
// group the default theme comes first and the others by label. The error is that of Themes.
func (s *Service) PickerGroups() ([]PickerGroup, error) {
	list, err := s.Themes()
	if err != nil {
		return nil, err
	}
	current := s.CurrentInfo()
	build := func(title string, keep func(Type) bool) PickerGroup {
		g := PickerGroup{Title: title}
		for _, info := range list {
			if keep(info.Type) {
				g.Entries = append(g.Entries, PickerEntry{Info: info, Value: info.Label, Description: PickDescription(info), Current: info == current})
			}
		}
		pinned := func(e PickerEntry) bool {
			return e.Info.Bundled && (e.Info.ID == DefaultDark || e.Info.ID == DefaultLight)
		}
		slices.SortStableFunc(g.Entries, func(a, b PickerEntry) int {
			if pa, pb := pinned(a), pinned(b); pa != pb {
				if pa {
					return -1
				}
				return 1
			}
			return strings.Compare(strings.ToLower(a.Info.Label), strings.ToLower(b.Info.Label))
		})
		return g
	}
	groups := []PickerGroup{
		build(PickLightThemes, func(t Type) bool { return t == TypeLight }),
		build(PickDarkThemes, func(t Type) bool { return t == TypeDark }),
		build(PickHighContrastThemes, func(t Type) bool { return t == TypeHCDark || t == TypeHCLight }),
	}
	return slices.DeleteFunc(groups, func(g PickerGroup) bool { return len(g.Entries) == 0 }), nil
}

// FileIconChoice is a choice of the file icon theme picker.
type FileIconChoice struct {
	// ID is the id of the theme, "" for none.
	ID string
	// Label and Description are the texts of the item.
	Label, Description string
}

// SettingValue is what the setting workbench.iconTheme holds for the choice: the id, or nil (null) for none.
func (c FileIconChoice) SettingValue() any {
	if c.ID == "" {
		return nil
	}
	return c.ID
}

// FileIconChoices lists what the file icon theme picker offers: None (no icons) and the bundled themes.
func FileIconChoices() []FileIconChoice {
	return []FileIconChoice{
		{ID: "", Label: "None", Description: "Disable File Icons"},
		{ID: DefaultFileIconTheme, Label: "Seti (Visual Studio Code)", Description: DefaultFileIconTheme},
	}
}
