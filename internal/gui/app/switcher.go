package app

import (
	"fmt"
	"path/filepath"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// Switching between editors (SPEC §10.3, §10.6): the commands that open the next and the previous editor, the
// recently used editors, and the editor picker of the quick input that Ctrl+Tab opens.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorActions.ts and
// src/vs/workbench/browser/parts/editor/editorQuickAccess.ts

// neighborEditor is the editor that Open Next Editor or Open Previous Editor shows: the next or the previous
// one of the active group in the order of the tabs, else the first or the last editor of the next or the
// previous group that has editors (going round).
func (s *shellState) neighborEditor(next bool) (*editorgroup.Group, editorgroup.EditorInput) {
	grid := s.group.grid
	active := s.activeGroup()
	editors := active.Model.Editors(editorgroup.Sequential, false)
	index := -1
	if cur := active.Model.ActiveEditor(); cur != nil {
		index = active.Model.IndexOf(cur)
	}
	switch {
	case next && index+1 < len(editors):
		return active, editors[index+1]
	case !next && index > 0:
		return active, editors[index-1]
	}
	loc := editorgroup.LocationNext
	if !next {
		loc = editorgroup.LocationPrevious
	}
	handled := map[int]bool{}
	for cur := active; cur != nil && !handled[cur.ID()]; {
		cur = grid.FindByLocation(cur, loc, true)
		if cur == nil {
			break
		}
		handled[cur.ID()] = true
		if es := cur.Model.Editors(editorgroup.Sequential, false); len(es) > 0 {
			if next {
				return cur, es[0]
			}
			return cur, es[len(es)-1]
		}
	}
	return nil, nil
}

// recentEditors lists the editors most recently used first: those of the active group, or of all the groups
// (the group used last first).
func (s *shellState) recentEditors(inGroup bool) []editorgroup.Ref {
	var refs []editorgroup.Ref
	groups := s.group.grid.Groups(editorgroup.MostRecentlyActiveGroups)
	if inGroup {
		groups = []*editorgroup.Group{s.activeGroup()}
	}
	for _, g := range groups {
		for _, in := range g.Model.Editors(editorgroup.MostRecentlyActive, false) {
			refs = append(refs, editorgroup.Ref{GroupID: g.ID(), Input: in})
		}
	}
	return refs
}

// stepRecent shows the next editor (delta -1) or the previous one (+1) in the order the editors were used,
// walking a list that is kept until anything else changes that order.
func (s *shellState) stepRecent(delta int, inGroup bool) {
	h := s.nav.history
	if h == nil {
		return
	}
	stack := &h.Recent
	if inGroup {
		stack = &h.RecentInGroup
	}
	ref, ok := stack.Step(func() []editorgroup.Ref { return s.recentEditors(inGroup) }, delta)
	if !ok {
		return
	}
	g := s.group.grid.Group(ref.GroupID)
	if g == nil {
		g = s.activeGroup()
	}
	if !g.Model.Contains(ref.Input) {
		return
	}
	stack.SetNavigating(true)
	defer stack.SetNavigating(false)
	s.activateEditor(g, ref.Input)
}

// quickNavigateOf is the state of a switcher opened by a command: the modifiers of its keys, whose release
// chooses the active item. A command without a key of one press opens a picker like any other.
func (s *shellState) quickNavigateOf(id string) *quickinput.QuickNavigate {
	win := PeekWindow(s.Context())
	qn := &quickinput.QuickNavigate{}
	for _, it := range win.Keybindings.LookupKeybindings(id) {
		if len(it.Keybinding) == 1 {
			c := it.Keybinding[0]
			qn.Chords = append(qn.Chords, quickinput.Modifiers{Ctrl: c.Ctrl, Shift: c.Shift, Alt: c.Alt, Meta: c.Meta})
		}
	}
	if len(qn.Chords) == 0 {
		return nil
	}
	return qn
}

// switchCommands are the commands that move between the editors: next and previous (across groups or in the
// group), the recently used editors, and the editor picker with the keys that move in it.
func (s *shellState) switchCommands() map[string]command.Handler {
	win := PeekWindow(s.Context())
	ctl := win.QuickInput
	open := func(g *editorgroup.Group, in editorgroup.EditorInput) {
		if g != nil && in != nil {
			s.activateEditor(g, in)
		}
	}
	inGroup := func(delta int) command.Handler {
		return func(command.Context, any) error {
			g := s.activeGroup()
			editors := g.Model.Editors(editorgroup.Sequential, false)
			if len(editors) == 0 {
				return nil
			}
			index := -1
			if cur := g.Model.ActiveEditor(); cur != nil {
				index = g.Model.IndexOf(cur)
			}
			open(g, editors[((index+delta)%len(editors)+len(editors))%len(editors)])
			return nil
		}
	}
	across := func(next bool) command.Handler {
		return func(command.Context, any) error { open(s.neighborEditor(next)); return nil }
	}
	recent := func(delta int, group bool) command.Handler {
		return func(command.Context, any) error { s.stepRecent(delta, group); return nil }
	}
	editorsPickerOpen := func() bool {
		p := ctl.Provider()
		return ctl.Visible() && p != nil && p.Info().ContextKey == quickinput.EditorsContextKey
	}
	// show opens a mode of the editor picker; the commands that go with keys open it as a switcher that
	// the held keys drive (the second item is active, the last one for back), and move in it when it is
	// open already. Without a key of one press the picker is a normal one, with the same active item.
	show := func(id, prefix string, back, needsEditor bool) command.Handler {
		return func(command.Context, any) error {
			if needsEditor && s.activeGroup().Empty() {
				return nil
			}
			qn := s.quickNavigateOf(id)
			if editorsPickerOpen() {
				ctl.Navigate(!back, qn)
				return nil
			}
			act := quickinput.ItemSecond
			if back && prefix == quickinput.EditorsActiveGroupPrefix {
				act = quickinput.ItemLast
			}
			ctl.Show(prefix, quickinput.ShowOptions{QuickNavigate: qn, Activation: act})
			return nil
		}
	}
	plain := func(prefix string) command.Handler {
		return func(command.Context, any) error { ctl.Show(prefix, quickinput.ShowOptions{}); return nil }
	}
	navigate := func(id string, next bool) command.Handler {
		return func(command.Context, any) error { ctl.Navigate(next, s.quickNavigateOf(id)); return nil }
	}
	const (
		mruAll      = "workbench.action.quickOpenPreviousRecentlyUsedEditor"
		leastAll    = "workbench.action.quickOpenLeastRecentlyUsedEditor"
		mruGroup    = "workbench.action.quickOpenPreviousRecentlyUsedEditorInGroup"
		leastGroup  = "workbench.action.quickOpenLeastRecentlyUsedEditorInGroup"
		nextPicker  = "workbench.action.quickOpenNavigateNextInEditorPicker"
		prevPicker  = "workbench.action.quickOpenNavigatePreviousInEditorPicker"
		fromHistory = "workbench.action.openPreviousEditorFromHistory"
	)
	return map[string]command.Handler{
		"workbench.action.nextEditor":                            across(true),
		"workbench.action.previousEditor":                        across(false),
		"workbench.action.nextEditorInGroup":                     inGroup(1),
		"workbench.action.previousEditorInGroup":                 inGroup(-1),
		"workbench.action.openNextRecentlyUsedEditor":            recent(-1, false),
		"workbench.action.openPreviousRecentlyUsedEditor":        recent(1, false),
		"workbench.action.openNextRecentlyUsedEditorInGroup":     recent(-1, true),
		"workbench.action.openPreviousRecentlyUsedEditorInGroup": recent(1, true),

		quickinput.ShowAllEditorsCommand:           plain(quickinput.EditorsPrefix),
		quickinput.ShowAllEditorsByMRUCommand:      plain(quickinput.EditorsMRUPrefix),
		quickinput.ShowEditorsInActiveGroupCommand: plain(quickinput.EditorsActiveGroupPrefix),
		// The least recently used editor of all the groups starts at the second item too, as in VS Code.
		mruAll:     show(mruAll, quickinput.EditorsMRUPrefix, false, false),
		leastAll:   show(leastAll, quickinput.EditorsMRUPrefix, true, false),
		mruGroup:   show(mruGroup, quickinput.EditorsActiveGroupPrefix, false, true),
		leastGroup: show(leastGroup, quickinput.EditorsActiveGroupPrefix, true, true),
		nextPicker: navigate(nextPicker, true),
		prevPicker: navigate(prevPicker, false),
		fromHistory: func(command.Context, any) error {
			qn := s.quickNavigateOf(fromHistory)
			if ctl.Visible() && ctl.Provider() != nil && ctl.Provider().Info().Prefix == "" {
				ctl.Navigate(true, qn)
				return nil
			}
			// the first item is the active one when the group has no editor (the file that was used last
			// is the first), else the second (the first is the file that is shown)
			if s.activeGroup().Empty() {
				ctl.Show("", quickinput.ShowOptions{})
				return nil
			}
			ctl.Show("", quickinput.ShowOptions{QuickNavigate: qn, Activation: quickinput.ItemSecond})
			return nil
		},
	}
}

// editorsOptions connect the editor modes of the quick input to the window.
func (s *shellState) editorsOptions() quickinput.EditorsOptions {
	grid := func() *editorgroup.Grid { return s.group.grid }
	groupIDs := func() []int {
		var ids []int
		for _, g := range grid().Groups(editorgroup.GridAppearance) {
			ids = append(ids, g.ID())
		}
		return ids
	}
	return quickinput.EditorsOptions{
		Groups: groupIDs,
		GroupLabel: func(id int) string {
			for i, gid := range groupIDs() {
				if gid == id {
					return fmt.Sprintf("Group %d", i+1)
				}
			}
			return fmt.Sprintf("Group %d", id)
		},
		Entries: s.editorEntries,
	}
}

// editorEntries lists the open editors for a mode of the editor picker.
func (s *shellState) editorEntries(mode quickinput.EditorsMode) []quickinput.EditorEntry {
	win := PeekWindow(s.Context())
	grid := s.group.grid
	groups := grid.Groups(editorgroup.GridAppearance)
	order := editorgroup.Sequential
	switch mode {
	case quickinput.EditorsByRecentlyUsed:
		groups, order = grid.Groups(editorgroup.MostRecentlyActiveGroups), editorgroup.MostRecentlyActive
	case quickinput.EditorsInActiveGroup:
		groups, order = []*editorgroup.Group{s.activeGroup()}, editorgroup.MostRecentlyActive
	case quickinput.EditorsByAppearance:
	}
	var entries []quickinput.EditorEntry
	for _, g := range groups {
		for _, in := range g.Model.Editors(order, false) {
			path := ""
			if res, ok := in.Resource(); ok && res.Scheme == document.SchemeFile {
				path = res.Path
			}
			g, in, name := g, in, in.Name()
			entry := quickinput.EditorEntry{
				ID: fmt.Sprintf("%d:%p", g.ID(), in), GroupID: g.ID(), Label: name, Description: in.Description(), Path: path,
				Dirty: in.Dirty(), Preview: !g.Model.IsPinned(in),
				Open: func() {
					if s.Mounted() && s.group.grid.Group(g.ID()) == g && g.Model.Contains(in) {
						s.activateEditor(g, in)
					}
				},
				Close: func() bool {
					if len(editorgroup.PlanClose(s.group.grid, []editorgroup.CloseTarget{{Group: g, Editors: []editorgroup.EditorInput{in}}})) > 0 {
						// the dialog that asks about the changes comes over the picker, which makes way for it
						win.QuickInput.Hide()
					}
					s.closeEditors(g, []editorgroup.EditorInput{in})
					return !g.Model.Contains(in)
				},
			}
			if path != "" {
				entry.FileIcon = func(typ theme.Type) (theme.FileIcon, bool) {
					th := win.FileIcons()
					if th == nil {
						return theme.FileIcon{}, false
					}
					return th.IconFor(theme.FileIconQuery{Kind: theme.FileIconFile, Name: filepath.Base(path), LanguageID: win.languageOf(path), Type: typ})
				}
			}
			entries = append(entries, entry)
		}
	}
	return entries
}

// editorProviders are the providers of the three modes of the editor picker.
func (s *shellState) editorProviders() []quickinput.Provider {
	o := s.editorsOptions()
	return []quickinput.Provider{
		quickinput.NewEditorsProvider(quickinput.EditorsByAppearance, o),
		quickinput.NewEditorsProvider(quickinput.EditorsByRecentlyUsed, o),
		quickinput.NewEditorsProvider(quickinput.EditorsInActiveGroup, o),
	}
}
