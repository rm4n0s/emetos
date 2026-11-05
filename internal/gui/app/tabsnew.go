package app

import (
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The new-editor button (step 29.1, SPEC §10.3): a "+" after the last tab of the strip of every editor group, which opens
// the EditorTabsNew menu under it. Emetos's own; VS Code has no such button.

// newEditorTooltip is the tooltip of the button.
const newEditorTooltip = "New..."

// newEditorButton is the "+" of the strip of a group, a title action in size and colors.
func (s *shellState) newEditorButton(group *editorgroup.Group) widgets.Widget {
	v := s.view(group)
	return kit.NewBoundsProbe(kit.NewIconButton(fonts.CodiconAdd, kit.IconButtonOpts{
		Tooltip:   newEditorTooltip,
		OnPressed: func() { s.openTabsNewMenu(group) },
	}), &v.plus)
}

// tabsNewItems are the rows of the menu of the "+" of a group: those of the EditorTabsNew menu whose condition holds in the
// context of the group. Each runs its command with the argument {"groupId": n}.
func (s *shellState) tabsNewItems(group *editorgroup.Group) []kit.MenuItem {
	win := PeekWorkspace(s.Context())
	items := s.menus.Items(command.EditorTabsNew, s.groupContext(win, group))
	withArgs(items, map[string]any{"groupId": float64(group.ID())})
	return items
}

// openTabsNewMenu shows the menu of the "+" of a group under the button.
func (s *shellState) openTabsNewMenu(group *editorgroup.Group) bool {
	v := s.view(group)
	r, ok := v.plus.Rect()
	if !ok {
		return false
	}
	items := s.tabsNewItems(group)
	if len(items) == 0 {
		return false
	}
	v.tabMenu.Show(ui.Offset{Dx: r.Left, Dy: r.Bottom}, items)
	return true
}
