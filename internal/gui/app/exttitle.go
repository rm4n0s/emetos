package app

import (
	"math"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
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
			extra["resourceScheme"] = win.resourceScheme(uri)
			extra["resourceLangId"] = ed.doc.LanguageID()
			if uri.Scheme == "file" {
				extra["resourceExtname"] = filepath.Ext(uri.Path)
				extra["resourceFilename"] = filepath.Base(uri.Path)
			}
		} else if d, ok := e.(*editorgroup.DiffInput); ok {
			// the resource of a diff editor is its modified side, as VS Code's context keys read it (the primary side)
			uri := d.Modified().URI()
			extra["isInDiffEditor"] = true
			extra["resourceScheme"] = win.resourceScheme(uri)
			extra["resourceLangId"] = d.Modified().LanguageID()
			if uri.Scheme == "file" {
				extra["resourceExtname"] = filepath.Ext(uri.Path)
				extra["resourceFilename"] = filepath.Base(uri.Path)
			}
		}
	}
	return menuContext{base: win.Keys, settings: win.Settings, extra: extra}
}

// resourceScheme is the value of the context key resourceScheme for a document: the scheme of its URI, and for a document that an
// extension provides (extension.ContentURI) the scheme of its provider ("git"), as VS Code's git: documents have.
func (w *Workspace) resourceScheme(uri document.URI) string {
	if uri.Scheme == document.SchemeVirtual {
		if scheme, _, ok := extension.SplitContentURI(uri.String()); ok && w.virtual.HasPrefix("/"+scheme+"/") {
			return scheme
		}
	}
	return uri.Scheme
}

// resourceOfGroup is the URI of the resource of the active editor of a group, which the commands of the editor title menu get as their
// argument (VS Code gives the resource of the editor to the commands of editor/title): the document of a text editor, the modified
// side of a diff editor; empty for another editor.
func (s *shellState) resourceOfGroup(group *editorgroup.Group) string {
	switch v := group.Model.ActiveEditor().(type) {
	case *editorgroup.TextInput:
		return v.Document().URI().String()
	case *editorgroup.DiffInput:
		return v.Modified().URI().String()
	}
	return ""
}

// titleActions makes the function that gives the title actions of a group as a widget for the right end of its strip, for the
// width of the strip: nil when there are none.
func (s *shellState) titleActions(win *Workspace, group *editorgroup.Group) func(width float64) widgets.Widget {
	var actions []exthost.EditorAction
	ctx := s.groupContext(win, group)
	args := map[string]any{"groupId": float64(group.ID())}
	if uri := s.resourceOfGroup(group); uri != "" {
		args["uri"] = uri
	}
	if scope := win.Extensions(); scope != nil {
		actions = scope.EditorActions(ctx)
	}
	menu, menuRest := s.titleMenuActions(win, ctx, args)
	actions = append(actions, menu...)
	// Accept and Reject of a proposed change come first and are never folded into the "..." menu
	proposal := s.proposalButtons(group)
	if len(actions) == 0 && len(menuRest) == 0 && len(proposal) == 0 {
		return nil
	}
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
		rest = append(rest, menuRest...)
		var buttons []widgets.Widget
		for _, b := range proposal {
			buttons = append(buttons, b, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}))
		}
		for _, a := range shown {
			buttons = append(buttons, kit.NewIconButton(a.Icon, kit.IconButtonOpts{
				Tooltip: a.Tooltip, OnPressed: func() { s.runTitleAction(win, a.Command, orArgs(a.Args, args)) },
			}))
		}
		if len(rest) > 0 {
			buttons = append(buttons, kit.NewBoundsProbe(kit.NewIconButton(fonts.CodiconEllipsis, kit.IconButtonOpts{
				Tooltip: "More Actions...",
				OnPressed: func() {
					items := make([]kit.MenuItem, len(rest))
					for i, a := range rest {
						items[i] = kit.MenuItem{ID: a.Command, Label: a.Tooltip, Data: orArgs(a.Args, args)}
					}
					at := ui.OffsetZero
					if r, ok := v.overflow.Rect(); ok {
						at = ui.Offset{Dx: r.Left, Dy: r.Bottom}
					}
					v.tabMenu.Show(at, items)
				},
			}), &v.overflow))
		}
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 4, 0), widgets.NewRow(buttons, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	}
}

// runTitleAction runs the command of an icon, for the group it is in; a failure is reported.
func (s *shellState) runTitleAction(win *Workspace, id string, args any) {
	if err := win.runCommand(id, args); err != nil {
		win.Process.Logger.Warn("an editor title action failed", "command", id, "err", describe(err))
		win.Notify.Warn("Emetos", "Cannot run the action: "+describe(err))
	}
}

// orArgs is the arguments of an action: its own, else the window's.
func orArgs(own, window any) any {
	if own != nil {
		return own
	}
	return window
}

// titleMenuActions are the items of the editor/title menu (command.EditorTitle) whose condition holds: those in the navigation group that
// have an icon are icon buttons, the others go to the "..." menu. The commands get the group and the resource of the editor.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorTabsControl.ts (the editor actions toolbar of MenuId.EditorTitle)
func (s *shellState) titleMenuActions(win *Workspace, ctx contextkey.Context, args map[string]any) (icons, overflow []exthost.EditorAction) {
	for _, e := range win.Menus.Items(command.EditorTitle, ctx) {
		if e.Separator || e.Command == "" {
			continue
		}
		a := exthost.EditorAction{Command: e.Command, Tooltip: e.Title, Args: args}
		if code, ok := fonts.CodiconByName(e.Icon); ok && strings.HasPrefix(e.Group, "navigation") && !e.Disabled {
			a.Icon = code
			icons = append(icons, a)
		} else if !e.Disabled {
			overflow = append(overflow, a)
		}
	}
	return icons, overflow
}
