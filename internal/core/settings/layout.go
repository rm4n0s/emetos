package settings

import (
	"regexp"
	"strings"
)

// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/settingsLayout.ts (tocData,
// COMMONLY_USED_SETTINGS) and src/vs/workbench/contrib/preferences/browser/settingsTree.ts
// (resolveSettingsTree, getMatchingSettings, sortSettings)

// TOCEntry is a group of the table of contents of the settings editor: a label, the key patterns of
// the settings it holds (`*` matches anything, case does not matter) and the groups below it.
type TOCEntry struct {
	// ID names the group, such as "editor/font"; it is VS Code's id.
	ID string
	// Label is the text of the group in the tree and as the header of its settings.
	Label string
	// Patterns are the key patterns of the settings of the group.
	Patterns []string
	// Children are the groups below.
	Children []TOCEntry
}

// CommonlyUsedID is the id of the group of the settings most people change.
const CommonlyUsedID = "commonlyUsed"

// commonlyUsed are the settings of the Commonly Used group, in order; those that are not registered
// are left out.
var commonlyUsed = []string{
	"editor.fontSize",
	"editor.formatOnSave",
	"files.autoSave",
	"editor.defaultFormatter",
	"editor.fontFamily",
	"editor.wordWrap",
	"files.exclude",
	"workbench.colorTheme",
	"editor.tabSize",
	"editor.mouseWheelZoom",
	"editor.formatOnPaste",
}

// extensionsID is the id of the group of the settings no group claims. VS Code lists the settings of
// extensions there, one group each; a setting of Emetos that the table does not place is shown in a
// group named after its first key segment ("emmet.*" is "Emmet", as the Emmet extension of VS Code
// shows it).
const extensionsID = "extensions"

// DefaultTOC is VS Code's table of contents for the groups Emetos can have settings in. The groups of
// VS Code that no setting of Emetos can belong to (Chat, Notebook, Debug and so on) are
// left out until a setting needs one.
func DefaultTOC() []TOCEntry {
	return []TOCEntry{
		{
			ID: "editor", Label: "Text Editor", Patterns: []string{"editor.*"},
			Children: []TOCEntry{
				{ID: "editor/cursor", Label: "Cursor", Patterns: []string{"editor.cursor*"}},
				{ID: "editor/find", Label: "Find", Patterns: []string{"editor.find.*"}},
				{ID: "editor/font", Label: "Font", Patterns: []string{"editor.font*"}},
				{ID: "editor/format", Label: "Formatting", Patterns: []string{"editor.format*"}},
				{ID: "editor/diffEditor", Label: "Diff Editor", Patterns: []string{"diffEditor.*"}},
				{ID: "editor/multiDiffEditor", Label: "Multi-File Diff Editor", Patterns: []string{"multiDiffEditor.*"}},
				{ID: "editor/minimap", Label: "Minimap", Patterns: []string{"editor.minimap.*"}},
				{ID: "editor/suggestions", Label: "Suggestions", Patterns: []string{"editor.*suggest*"}},
				{ID: "editor/files", Label: "Files", Patterns: []string{"files.*"}},
			},
		},
		{
			ID: "workbench", Label: "Workbench", Patterns: []string{"workbench.*"},
			Children: []TOCEntry{
				{ID: "workbench/appearance", Label: "Appearance", Patterns: []string{"workbench.activityBar.*", "workbench.*color*", "workbench.fontAliasing", "workbench.iconTheme", "workbench.sidebar.location", "workbench.*.visible", "workbench.tips.enabled", "workbench.tree.*", "workbench.view.*"}},
				{ID: "workbench/breadcrumbs", Label: "Breadcrumbs", Patterns: []string{"breadcrumbs.*"}},
				{ID: "workbench/editor", Label: "Editor Management", Patterns: []string{"workbench.editor.*"}},
				{ID: "workbench/settings", Label: "Settings Editor", Patterns: []string{"workbench.settings.*"}},
				{ID: "workbench/zenmode", Label: "Zen Mode", Patterns: []string{"zenmode.*"}},
				{ID: "workbench/screencastmode", Label: "Screencast Mode", Patterns: []string{"screencastMode.*"}},
			},
		},
		{
			ID: "window", Label: "Window", Patterns: []string{"window.*"},
			Children: []TOCEntry{
				{ID: "window/newWindow", Label: "New Window", Patterns: []string{"window.*newwindow*"}},
			},
		},
		{
			ID: "features", Label: "Features",
			Children: []TOCEntry{
				{ID: "features/explorer", Label: "Explorer", Patterns: []string{"explorer.*", "outline.*"}},
				{ID: "features/search", Label: "Search", Patterns: []string{"search.*"}},
				{ID: "features/terminal", Label: "Terminal", Patterns: []string{"terminal.integrated.*"}},
				{ID: "features/problems", Label: "Problems", Patterns: []string{"problems.*"}},
				{ID: "features/output", Label: "Output", Patterns: []string{"output.*"}},
				{ID: "features/mergeEditor", Label: "Merge Editor", Patterns: []string{"mergeEditor.*"}},
			},
		},
		{
			ID: "application", Label: "Application",
			Children: []TOCEntry{
				{ID: "application/keyboard", Label: "Keyboard", Patterns: []string{"keyboard.*"}},
				{ID: "application/telemetry", Label: "Telemetry", Patterns: []string{"telemetry.*"}},
				{ID: "application/other", Label: "Other", Patterns: []string{"application.*"}},
			},
		},
		{ID: "security", Label: "Security", Patterns: []string{"security.*"}},
	}
}

// Category is a group of the settings editor with the settings it holds, in display order.
type Category struct {
	// ID is the id of the group ("editor/font"); it is unique in a tree.
	ID string
	// Label is the text of the group.
	Label string
	// Level is how deep the group is: 1 for a top level group, 2 for a child. The header of the group
	// is the larger the lower the level.
	Level int
	// Keys are the keys of the settings of the group itself (not of its children), in display order.
	Keys []string
	// Children are the groups below.
	Children []*Category
}

// Count is the number of settings in the group and below it.
func (c *Category) Count() int {
	n := len(c.Keys)
	for _, ch := range c.Children {
		n += ch.Count()
	}
	return n
}

// Find returns the group with the id, in this one or below it.
func (c *Category) Find(id string) *Category {
	if c.ID == id {
		return c
	}
	for _, ch := range c.Children {
		if f := ch.Find(id); f != nil {
			return f
		}
	}
	return nil
}

// Walk calls fn for the group and each group below it, in display order.
func (c *Category) Walk(fn func(*Category)) {
	fn(c)
	for _, ch := range c.Children {
		ch.Walk(fn)
	}
}

// patternCache holds the compiled key patterns; it is built for one call and not shared.
type patternCache map[string]*regexp.Regexp

func (p patternCache) matches(pattern, key string) bool {
	re, ok := p[pattern]
	if !ok {
		re = regexp.MustCompile("(?i)^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$")
		p[pattern] = re
	}
	return re.MatchString(key)
}

// BuildCategories places settings (the keys, with the setting each is) in the groups of toc, the way
// VS Code does: each group claims, after the groups below it have claimed theirs, the settings that match its
// patterns and that no group has taken, sorted by key. The groups left without settings and without groups are
// dropped. The settings no group claims are put in the group "Extensions", one child group per first key segment.
// The Commonly Used group comes first and takes copies of its settings.
func BuildCategories(toc []TOCEntry, settings []Setting) []*Category {
	remaining := make([]Setting, len(settings))
	copy(remaining, settings)
	sortSettings(remaining)
	byKey := make(map[string]Setting, len(settings))
	for _, s := range settings {
		byKey[s.Key] = s
	}
	cache := patternCache{}
	var out []*Category

	var common []string
	for _, key := range commonlyUsed {
		if _, ok := byKey[key]; ok {
			common = append(common, key)
		}
	}
	if len(common) > 0 {
		out = append(out, &Category{ID: CommonlyUsedID, Label: "Commonly Used", Level: 1, Keys: common})
	}

	var resolve func(e TOCEntry, level int) *Category
	resolve = func(e TOCEntry, level int) *Category {
		c := &Category{ID: e.ID, Label: e.Label, Level: level}
		for _, child := range e.Children {
			if cc := resolve(child, level+1); cc != nil {
				c.Children = append(c.Children, cc)
			}
		}
		kept := remaining[:0:0]
		for _, s := range remaining {
			claimed := false
			for _, p := range e.Patterns {
				if cache.matches(p, s.Key) {
					claimed = true
					break
				}
			}
			if claimed {
				c.Keys = append(c.Keys, s.Key)
			} else {
				kept = append(kept, s)
			}
		}
		remaining = kept
		if len(c.Keys) == 0 && len(c.Children) == 0 {
			return nil
		}
		return c
	}
	for _, e := range toc {
		if c := resolve(e, 1); c != nil {
			out = append(out, c)
		}
	}

	if len(remaining) > 0 {
		ext := &Category{ID: extensionsID, Label: "Extensions", Level: 1}
		groups := map[string]*Category{}
		for _, s := range remaining {
			segment, _, _ := strings.Cut(s.Key, ".")
			g := groups[segment]
			if g == nil {
				g = &Category{ID: extensionsID + "/" + segment, Label: WordifyKey(segment), Level: 2}
				groups[segment] = g
				ext.Children = append(ext.Children, g)
			}
			g.Keys = append(g.Keys, s.Key)
		}
		out = append(out, ext)
	}
	return out
}

// sortSettings sorts by key, as VS Code's localeCompare does for keys: case does not decide.
func sortSettings(list []Setting) {
	less := func(a, b string) bool {
		la, lb := strings.ToLower(a), strings.ToLower(b)
		if la != lb {
			return la < lb
		}
		return a < b
	}
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && less(list[j].Key, list[j-1].Key); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}
