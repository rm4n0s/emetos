package quickinput

import (
	"slices"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorQuickAccess.ts and
// src/vs/workbench/browser/parts/editor/media/editorquickaccess.css

// The prefixes of the modes that list the open editors, the context key that is true while one of them is
// shown, and the labels of the picker.
const (
	// EditorsPrefix lists the editors of all groups in the order of the groups and the tabs: "edt ".
	EditorsPrefix = "edt "
	// EditorsMRUPrefix lists them in the order they were used: "edt mru ".
	EditorsMRUPrefix = "edt mru "
	// EditorsActiveGroupPrefix lists the editors of the active group in the order they were used: "edt active ".
	EditorsActiveGroupPrefix = "edt active "
	// EditorsContextKey is true while one of the editor modes is shown (VS Code's inEditorsPicker).
	EditorsContextKey = "inEditorsPicker"
	// EditorsPlaceholder is the hint of the input in these modes.
	EditorsPlaceholder = "Type the name of an editor to open it."
	// EditorsNoResults is the row shown when no editor matches.
	EditorsNoResults = "No matching editors"
)

// The ids of the commands that open the editor modes.
const (
	// ShowAllEditorsCommand is Show All Editors By Appearance (Ctrl+K Ctrl+P).
	ShowAllEditorsCommand = "workbench.action.showAllEditors"
	// ShowAllEditorsByMRUCommand is Show All Editors By Most Recently Used.
	ShowAllEditorsByMRUCommand = "workbench.action.showAllEditorsByMostRecentlyUsed"
	// ShowEditorsInActiveGroupCommand is Show Editors in Active Group By Most Recently Used.
	ShowEditorsInActiveGroupCommand = "workbench.action.showEditorsInActiveGroup"
)

// EditorsMode is which editors a provider lists, in which order.
type EditorsMode int

// The modes.
const (
	// EditorsByAppearance lists the editors of all groups in the order of the groups and their tabs.
	EditorsByAppearance EditorsMode = iota
	// EditorsByRecentlyUsed lists the editors of all groups, the most recently used first.
	EditorsByRecentlyUsed
	// EditorsInActiveGroup lists the editors of the active group, the most recently used first.
	EditorsInActiveGroup
)

// EditorEntry is an open editor as the editor picker lists it.
type EditorEntry struct {
	// ID tells the entry from the others; it is not shown.
	ID string
	// GroupID is the group the editor is in.
	GroupID int
	// Label is the name of the editor and Description its folder; Path is the path of its file, which the
	// filter matches too.
	Label, Description, Path string
	// Dirty tells that the editor has changes that are not saved: its close button is a dot.
	Dirty bool
	// Preview tells that the editor is a preview editor, which is in italics.
	Preview bool
	// FileIcon gives the icon of the editor for the type of the color theme; false draws none.
	FileIcon func(theme.Type) (theme.FileIcon, bool)
	// Open shows the editor in its group.
	Open func()
	// Close closes the editor and tells whether it is gone (it stays when it has unsaved changes and the
	// user cancels).
	Close func() bool
}

// EditorsOptions connect the editor modes to the window.
type EditorsOptions struct {
	// Entries lists the editors of a mode, in the order the mode shows them when nothing is typed.
	Entries func(mode EditorsMode) []EditorEntry
	// Groups lists the ids of the groups in the order of the grid; GroupLabel names one ("Group 1").
	Groups     func() []int
	GroupLabel func(id int) string
}

// editorAccessor gives the scorer the texts of an editor entry.
type editorAccessor struct{}

func (editorAccessor) ItemLabel(e EditorEntry) string       { return e.Label }
func (editorAccessor) ItemDescription(e EditorEntry) string { return e.Description }
func (editorAccessor) ItemPath(e EditorEntry) string        { return e.Path }

// NewEditorsProvider makes the provider of one of the modes that list the open editors: the label of an item is
// the name of the editor and its description the folder, a dirty editor has a dot at the right that turns into a
// cross under the pointer (the cross closes the editor and takes the item out of the list, the picker stays),
// a preview editor is in italics. With more than one group the items are grouped under the name of their group.
// Choosing an item shows the editor.
func NewEditorsProvider(mode EditorsMode, o EditorsOptions) *ListProvider {
	prefix, command := EditorsPrefix, ShowAllEditorsCommand
	help := "Show All Opened Editors By Appearance"
	switch mode {
	case EditorsByRecentlyUsed:
		prefix, command, help = EditorsMRUPrefix, ShowAllEditorsByMRUCommand, "Show All Opened Editors By Most Recently Used"
	case EditorsInActiveGroup:
		prefix, command, help = EditorsActiveGroupPrefix, ShowEditorsInActiveGroupCommand, "Show Editors in Active Group by Most Recently Used"
	}
	scorer := fuzzy.NewScorer()
	// The scores are kept while the filter does not change: moving in a switcher asks for the items again.
	var cache *fuzzy.Cache
	p := &ListProvider{
		ProviderInfo: ProviderInfo{
			Prefix: prefix, Placeholder: EditorsPlaceholder, ContextKey: EditorsContextKey,
			Help: []HelpEntry{{Description: help, Command: command}},
		},
		NoResults: EditorsNoResults,
		Start: func(*Session) func() {
			cache = fuzzy.NewCache()
			return nil
		},
	}
	p.Picks = func(filter string) []Item { return editorPicks(mode, o, filter, scorer, cache) }
	return p
}

// editorPicks makes the items of an editor mode for a filter.
func editorPicks(mode EditorsMode, o EditorsOptions, filter string, scorer *fuzzy.Scorer, cache *fuzzy.Cache) []Item {
	type scored struct {
		entry EditorEntry
		score fuzzy.ItemScore
	}
	query := fuzzy.PrepareQuery(filter)
	var picks []scored
	for _, e := range o.Entries(mode) {
		if query.Normalized == "" {
			picks = append(picks, scored{entry: e})
			continue
		}
		s := fuzzy.ScoreItemFuzzy(scorer, e, query, true, editorAccessor{}, cache)
		if s.Score == 0 {
			continue
		}
		picks = append(picks, scored{entry: e, score: s})
	}
	if query.Normalized != "" {
		groups := o.Groups()
		slices.SortStableFunc(picks, func(a, b scored) int {
			if a.entry.GroupID != b.entry.GroupID {
				return slices.Index(groups, a.entry.GroupID) - slices.Index(groups, b.entry.GroupID) // older groups first
			}
			return fuzzy.CompareItemsByFuzzyScore(scorer, a.entry, b.entry, query, true, editorAccessor{}, cache)
		})
	}
	multiple := len(o.Groups()) > 1
	var items []Item
	last, has := 0, false
	for _, pick := range picks {
		e := pick.entry
		if multiple && (!has || last != e.GroupID) {
			items = append(items, Item{Separator: true, Label: o.GroupLabel(e.GroupID)})
			last, has = e.GroupID, true
		}
		items = append(items, editorItem(e, pick.score))
	}
	return items
}

// editorItem is the item of an editor.
func editorItem(e EditorEntry, score fuzzy.ItemScore) Item {
	btn := Button{Icon: fonts.CodiconClose, Tooltip: "Close Editor"}
	if e.Dirty {
		btn = Button{Icon: fonts.CodiconCircleFilled, Tooltip: "Close Editor", AlwaysVisible: true, HoverIcon: fonts.CodiconClose}
	}
	return Item{
		ID: e.ID, Label: e.Label, Description: e.Description, Italic: e.Preview, FileIcon: e.FileIcon,
		Highlights: Highlights{Label: score.LabelMatch, Description: score.DescriptionMatch},
		Buttons:    []Button{btn},
		Data:       e,
		Accept:     func(KeyMods) { e.Open() },
		Trigger: func(int, KeyMods) TriggerAction {
			if e.Close != nil && e.Close() {
				return TriggerRemove
			}
			return TriggerNone
		},
	}
}
