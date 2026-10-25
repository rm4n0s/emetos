package app

import (
	"math"
	"path/filepath"

	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The editor title actions of the extensions (SPEC §10.15 "Editor title action", step 11.6): micro icons at the right end of
// the tab strip of every editor group, in the order of the extensions and their Order, that run a command with the group
// they are in as its argument. The icons that do not fit go into a "..." menu.

const (
	// titleActionsShare is the part of the width of a strip the icons may take before they overflow.
	titleActionsShare = 0.4
	// titleActionWidth is the width of an icon (kit's icon button).
	titleActionWidth = 22.0
)

// groupContext is the context the `when` of the title actions of a group is read in: the keys of the window, with those that
// describe the editor the group shows set for it.
func (s *shellState) groupContext(win *Workspace, group *editorgroup.Group) contextkey.Context {
	extra := map[string]any{"activeEditor": nil}
	if e := group.Model.ActiveEditor(); e != nil {
		extra["activeEditor"] = e.TypeID()
		extra["activeEditorIsDirty"] = e.Dirty()
		if ed := s.group.editors[e]; ed != nil {
			uri := ed.doc.URI()
			extra["resourceScheme"] = uri.Scheme
			extra["resourceLangId"] = ed.doc.LanguageID()
			if uri.Scheme == "file" {
				extra["resourceExtname"] = filepath.Ext(uri.Path)
				extra["resourceFilename"] = filepath.Base(uri.Path)
			}
		}
	}
	return menuContext{base: win.Keys, settings: win.Settings, extra: extra}
}

// titleActions makes the function that gives the title actions of a group as a widget for the right end of its strip, for the
// width of the strip: nil when there are none.
func (s *shellState) titleActions(win *Workspace, group *editorgroup.Group) func(width float64) widgets.Widget {
	scope := win.Extensions()
	if scope == nil {
		return nil
	}
	actions := scope.EditorActions(s.groupContext(win, group))
	if len(actions) == 0 {
		return nil
	}
	args := map[string]any{"groupId": float64(group.ID())}
	v := s.view(group)
	return func(room float64) widgets.Widget {
		capacity := len(actions)
		if room > 0 && !math.IsInf(room, 1) {
			capacity = max(int(room*titleActionsShare/titleActionWidth), 1)
		}
		shown, rest := actions, []exthost.EditorAction(nil)
		if len(actions) > capacity {
			shown, rest = actions[:capacity-1], actions[capacity-1:]
		}
		var buttons []widgets.Widget
		for _, a := range shown {
			buttons = append(buttons, kit.NewIconButton(a.Icon, kit.IconButtonOpts{
				Tooltip: a.Tooltip, OnPressed: func() { s.runTitleAction(win, a.Command, args) },
			}))
		}
		if len(rest) > 0 {
			buttons = append(buttons, kit.NewBoundsProbe(kit.NewIconButton(fonts.CodiconEllipsis, kit.IconButtonOpts{
				Tooltip: "More Actions...",
				OnPressed: func() {
					items := make([]kit.MenuItem, len(rest))
					for i, a := range rest {
						items[i] = kit.MenuItem{ID: a.Command, Label: a.Tooltip, Data: args}
					}
					at := ui.OffsetZero
					if r, ok := v.overflow.Rect(); ok {
						at = ui.Offset{Dx: r.Left, Dy: r.Bottom}
					}
					v.tabMenu.Show(at, items)
				},
			}), &v.overflow))
		}
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 4, 0), widgets.NewRow(buttons, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin}))
	}
}

// runTitleAction runs the command of an icon, for the group it is in; a failure is reported.
func (s *shellState) runTitleAction(win *Workspace, id string, args any) {
	if err := win.runCommand(id, args); err != nil {
		win.Process.Logger.Warn("an editor title action failed", "command", id, "err", describe(err))
		win.Notify.Warn("Emetos", "Cannot run the action: "+describe(err))
	}
}
