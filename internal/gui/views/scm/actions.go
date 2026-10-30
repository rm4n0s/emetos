package scm

import (
	"slices"

	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The actions of the rows. The menus are the registry's: the rows of the group "inline" of the menu of a kind of row are its
// buttons (the group "navigation" of the title menu for a repository), the others are its context menu. The extensions add the
// items (Contributes.Menus), the window evaluates them in the context of the row (its repository, group and resource:
// scmProvider, scmResourceGroup, scmResourceState).
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (RepositoryPaneActionRunner, onListContextMenu)

const inlineGroup = "inline"

// menuOf is the menu of the actions of a kind of row.
func menuOf(r *row) string {
	switch r.kind {
	case kindRepository:
		return command.SCMTitle
	case kindGroup:
		return command.SCMResourceGroupContext
	case kindFolder:
		return command.SCMResourceFolderContext
	}
	return command.SCMResourceContext
}

// contextMenuOf is the menu that opens on a right click on a row.
func contextMenuOf(r *row) string {
	if r.kind == kindRepository {
		return command.SCMSourceControl
	}
	return menuOf(r)
}

// buttonGroup is the group of the rows of the menu that are buttons in the row: "navigation" for the title of a repository,
// "inline" for the others (connectPrimaryMenu's primary group).
func buttonGroup(r *row) string {
	if r.kind == kindRepository {
		return "navigation"
	}
	return inlineGroup
}

// rowContext is the context keys of a row, added to the window's for its menu.
func rowContext(r *row) map[string]any {
	var c map[string]any
	switch r.kind {
	case kindRepository:
		c = r.src.Context()
	case kindGroup, kindFolder:
		c = r.group.Context()
	default:
		c = r.res.Context()
	}
	return c
}

// buttons are the actions of the row that are buttons, as a toolbar draws them: the icon of the command, or its title when it has
// none.
func (v *View) buttons(r *row) []kit.Action {
	if v.opts.Entries == nil {
		return nil
	}
	if cached, ok := v.actions[r]; ok {
		return cached
	}
	want := buttonGroup(r)
	var out []kit.Action
	for _, e := range v.opts.Entries(menuOf(r), rowContext(r)) {
		if e.Separator || e.Submenu != "" || e.Command == "" || e.Group != want {
			continue
		}
		out = append(out, kit.Action{ID: e.Command, Label: e.Title, Icon: iconCode(e.Icon), Disabled: e.Disabled})
	}
	v.actions[r] = out
	return out
}

func iconCode(name string) int {
	code, _ := fonts.CodiconByName(name)
	return code
}

// inlineActions is the buttons of a row: always for a repository, and for the other rows while the pointer is over the row, the
// row has the focus, or scm.alwaysShowActions is on. Nil when there is none to show.
func (v *View) inlineActions(_ widgets.BuildContext, r *row, st kit.RowState) widgets.Widget {
	if r.kind != kindRepository && !st.Hovered && !st.Focused && !v.opts.flag(v.opts.AlwaysShowActions) {
		return nil
	}
	actions := v.buttons(r)
	if len(actions) == 0 {
		return nil
	}
	return widgets.NewPadding(paddingLeft(4), kit.NewActionBar(actions, kit.ActionBarOpts{
		OnRun: func(a kit.Action) { v.runRowAction(r, a.ID) },
	}))
}

// selectedRows are the rows of the selection as the model's menu arguments name them.
func (v *View) selectedRows() []scmcore.Row {
	var out []scmcore.Row
	for _, k := range v.list.SelectedKeys() {
		if it, ok := v.list.Item(k); ok {
			if r := coreRow(it.Data.(*row)); r != (scmcore.Row{}) {
				out = append(out, r)
			}
		}
	}
	return out
}

// coreRow is the row as the model's menu arguments know it.
func coreRow(r *row) scmcore.Row {
	switch r.kind {
	case kindGroup:
		return scmcore.Row{Group: r.group}
	case kindFolder:
		return scmcore.Row{Folder: r.entry.Node, FolderGroup: r.group}
	case kindResource:
		return scmcore.Row{Resource: r.res}
	}
	return scmcore.Row{}
}

// args are the arguments of the commands of the menus of a row: the source control for a repository (and the other selected
// repositories after it), else the row or the selected rows.
func (v *View) args(r *row) []any {
	if r.kind == kindRepository {
		out := []any{scmcore.SourceArg(r.src)}
		sel := v.selectedRepositories()
		if slices.Contains(sel, r.src) {
			for _, s := range sel {
				if s != r.src {
					out = append(out, scmcore.SourceArg(s))
				}
			}
		}
		return out
	}
	return scmcore.SelectionMenuArgs(coreRow(r), v.selectedRows())
}

// selectedRepositories are the repositories whose headers are selected or focused.
func (v *View) selectedRepositories() []*scmcore.Source {
	var out []*scmcore.Source
	keys := v.list.SelectedKeys()
	if k, ok := v.list.Focused(); ok {
		keys = append(keys, k)
	}
	for _, k := range keys {
		if it, ok := v.list.Item(k); ok {
			if r := it.Data.(*row); r.kind == kindRepository && !slices.Contains(out, r.src) {
				out = append(out, r.src)
			}
		}
	}
	return out
}

// runRowAction runs a command of a menu of a row with the arguments of the row.
func (v *View) runRowAction(r *row, commandID string) {
	v.run(commandID, v.args(r))
}

// run runs a command of the registry.
func (v *View) run(commandID string, args any) {
	if v.opts.Run != nil {
		v.opts.Run(commandID, args)
	}
}

// showContextMenu opens the menu of a row at a position: the row is selected first, unless it is in the selection already.
func (v *View) showContextMenu(lr kit.ListViewRow, at ui.Offset) {
	r := rowOf(lr)
	if r == nil {
		return
	}
	if !v.list.IsSelected(lr.Key) {
		v.list.SetSelected(lr.Key)
	}
	if v.opts.ShowMenu != nil {
		v.opts.ShowMenu(contextMenuOf(r), rowContext(r), v.args(r), at)
	}
}

// InvalidateMenus forgets the buttons the rows have, after a context key or a menu changed.
func (v *View) InvalidateMenus() {
	clear(v.actions)
	v.version++
	v.notify()
}
