package httpclient

import (
	"context"
	"strings"

	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
)

// environmentsView is the Environments view: the list of environments with the active one checked. It is used on the UI thread.
type environmentsView struct {
	e      *Extension
	handle extension.ViewHandle
	tree   *ui.TreeModel
	ctl    *ui.TreeController
	menu   *ui.ContextMenuController
}

// newEnvironmentsView makes the view.
func newEnvironmentsView(e *Extension) *environmentsView {
	return &environmentsView{
		e: e, ctl: ui.NewTreeController(), menu: ui.NewContextMenuController(),
		tree: ui.NewTreeModel(ui.TreeModelOpts{FindMode: ui.FindModeFilter}),
	}
}

// provide gives the view its content.
func (v *environmentsView) provide() error {
	h, err := v.e.api.Views().Provide(EnvironmentsViewID, extension.ViewContent{Build: v.build})
	if err != nil {
		return err
	}
	v.handle = h
	return nil
}

// rebuild makes the rows from the environments of the store.
func (v *environmentsView) rebuild() {
	if v.handle == nil {
		return
	}
	var items []ui.TreeItem
	if t := v.e.tree; t != nil {
		for _, n := range t.Environments {
			items = append(items, ui.TreeItem{Key: n.Path, Data: n, Label: n.Name})
		}
	}
	selected, had := v.tree.Selected()
	if err := v.tree.SetItems(items); err != nil {
		v.e.api.Log().Warn("the list of environments cannot be made", "err", err)
		return
	}
	if had && v.tree.Has(selected) {
		v.tree.SetSelected(selected)
	}
	v.handle.Refresh()
}

// build makes the widget of the view.
func (v *environmentsView) build(bc widgets.BuildContext) widgets.Widget {
	if w := forClient(bc); w != nil {
		return w
	}
	colors := ui.Theme(bc)
	e := v.e
	var children []widgets.Widget
	switch {
	case e.folder == "":
		children = append(children, note(bc, "Open a folder to use environments.", colors.DescriptionForeground))
	case e.loadErr != nil || e.store == nil:
		// the Collections view says why
	case len(e.tree.Environments) == 0:
		children = append(children, note(bc, "There are no environments. An environment holds the variables {{name}} a request uses, such as the address of a server.", colors.DescriptionForeground),
			widgets.NewPadding(paddingH(12), ui.NewButton("New Environment...", ui.ButtonOpts{OnPressed: e.newEnvironment})))
	}
	if e.tree != nil && len(e.tree.Environments) > 0 {
		children = append(children, widgets.NewExpanded(ui.NewTree(v.tree, ui.TreeOpts{
			Controller: v.ctl, Render: v.render, OnOpen: v.onOpen, OnContextMenu: v.onContextMenu,
		})))
	}
	return ui.NewContextMenu(column(children...), ui.ContextMenuOpts{Controller: v.menu})
}

// render builds the contents of a row: a check for the active environment, the name, the number of variables.
func (v *environmentsView) render(bc widgets.BuildContext, tn ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	n, ok := tn.Data.(*store.Node)
	if !ok {
		return rowText(bc, "", st.Foreground)
	}
	active := v.e.activeKey() == n.Key
	var row []widgets.Widget
	if active {
		row = append(row, ui.Icon("check", 16, st.Foreground))
	} else {
		row = append(row, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: optSome(16.0)}))
	}
	row = append(row, gap(6), widgets.NewFlexible(rowText(bc, n.Name, st.Foreground)), widgets.NewSpacer())
	if n.Environment != nil {
		vars, secrets := len(n.Environment.Variables), 0
		for _, x := range n.Environment.Variables {
			if x.Secret {
				secrets++
			}
		}
		row = append(row, gap(8), rowText(bc, plural(vars, "variable", "variables"), colors.DescriptionForeground))
		if secrets > 0 {
			row = append(row, gap(5), ui.Icon("lock", 12, colors.DescriptionForeground))
		}
	}
	switch {
	case n.ReadOnly:
		row = append(row, gap(6), ui.Icon("lock", 12, colors.DescriptionForeground))
	case n.Err != nil:
		row = append(row, gap(6), ui.Icon("error", 12, colors.ErrorForeground))
	}
	row = append(row, gap(8))
	var out widgets.Widget = widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: crossCenter})
	if n.Err != nil && !n.ReadOnly {
		out = ui.NewHover(out, ui.HoverOpts{Text: n.Err.Error(), Compact: true})
	}
	return out
}

// onOpen is Enter or a double click: the environment is opened in its editor.
func (v *environmentsView) onOpen(tn ui.TreeNode) {
	if n, ok := tn.Data.(*store.Node); ok {
		v.e.editEnvironment(n)
	}
}

// onContextMenu opens the menu of a row at the place of the click.
func (v *environmentsView) onContextMenu(key string, at gui.Offset) {
	e := v.e
	if key == "" {
		v.menu.Show(at, []ui.MenuItem{{Label: "New Environment...", OnRun: e.newEnvironment}})
		return
	}
	n := e.tree.Find(key)
	if n == nil {
		return
	}
	v.tree.SetSelected(key)
	active := e.activeKey() == n.Key
	items := []ui.MenuItem{}
	if active {
		items = append(items, ui.MenuItem{Label: "Use No Environment", OnRun: func() { e.setActive("") }})
	} else {
		items = append(items, ui.MenuItem{Label: "Use This Environment", Disabled: n.Err != nil && !n.ReadOnly, OnRun: func() { e.setActive(n.Key) }})
	}
	items = append(items,
		ui.MenuItem{Label: "Edit", OnRun: func() { e.editEnvironment(n) }},
		ui.SeparatorItem(),
		ui.MenuItem{Label: "Rename...", Disabled: n.ReadOnly, OnRun: func() { e.rename(n) }},
		ui.MenuItem{Label: "Duplicate", Disabled: n.ReadOnly, OnRun: func() { e.duplicate(n) }},
		ui.MenuItem{Label: "Delete...", OnRun: func() { e.delete(n) }},
		ui.SeparatorItem(),
		ui.MenuItem{Label: "Copy Name", OnRun: func() { e.copyText(n.Name) }})
	v.menu.Show(at, items)
}

// ---- the active environment ----

// statusItem is the item of the status bar for the state now.
func (e *Extension) statusItem() extension.StatusItem {
	text, tip := "$(globe) HTTP: No Environment", "No environment is active: {{variables}} of environments are not replaced. Click to choose one."
	if n := e.activeEnvironment(); n != nil {
		text, tip = "$(globe) HTTP: "+n.Name, "The active environment of the HTTP client is \""+n.Name+"\". Click to choose another."
	}
	return extension.StatusItem{
		ID: StatusEnvironmentID, Name: "HTTP Client Environment", Text: text, Tooltip: tip,
		Alignment: extension.StatusRight, Priority: 20, Command: CmdSelectEnvironment,
	}
}

// updateStatus puts the active environment in the status bar item; the item is shown when the folder has anything of the HTTP client.
func (e *Extension) updateStatus() {
	if e.status == nil {
		return
	}
	e.status.Update(e.statusItem())
	e.status.SetVisible(e.tree != nil && (len(e.tree.Collections) > 0 || len(e.tree.Environments) > 0))
}

// selectEnvironment makes an environment the active one: the argument names it (its key or name, or {"environment": ...}, or
// {"none": true}), else the picker lists them.
func (e *Extension) selectEnvironment(args any) {
	if !e.ready() {
		return
	}
	if m, ok := args.(map[string]any); ok {
		if none, _ := m["none"].(bool); none {
			e.setActive("")
			return
		}
	}
	want := strings.TrimSpace(argString(args, "environment"))
	if want == "" {
		want = strings.TrimSpace(argString(args, "key"))
	}
	if want != "" {
		for _, n := range e.tree.Environments {
			if strings.EqualFold(n.Key, want) || strings.EqualFold(n.Name, want) {
				e.setActive(n.Key)
				return
			}
		}
		e.api.Notifications().Warn("There is no environment \"" + want + "\".")
		return
	}
	envs := e.tree.Environments
	active := e.activeKey()
	items := []extension.QuickPickItem{{Label: "$(circle-slash) No Environment", Description: markActive(active == "")}}
	for _, n := range envs {
		desc := ""
		if n.Environment != nil {
			desc = plural(len(n.Environment.Variables), "variable", "variables")
		}
		if n.Key == active {
			desc = "active, " + desc
		}
		items = append(items, extension.QuickPickItem{Label: "$(symbol-variable) " + n.Name, Description: strings.TrimSuffix(desc, ", ")})
	}
	if len(envs) == 0 {
		items = append(items, extension.QuickPickItem{Label: "$(add) New Environment...", Command: CmdNewEnvironment, AlwaysShow: true})
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Title: "HTTP Client", Placeholder: "Select the environment for requests", Items: items}, func(i int) {
		switch {
		case i == 0:
			e.setActive("")
		case i >= 1 && i <= len(envs):
			e.setActive(envs[i-1].Key)
		}
	})
	if err != nil {
		e.fail("show the list of environments", err)
	}
}

func markActive(on bool) string {
	if on {
		return "active"
	}
	return ""
}

// environmentRenamed follows the rename of an environment: the current values it had and the choice of the active one move to the new key.
// The values of secret variables stay in the keyring under the old name (reading them would ask the keyring), so a renamed environment
// asks for them again.
func (e *Extension) environmentRenamed(oldKey, newKey string) {
	if oldKey == newKey || newKey == "" {
		return
	}
	oldOwner := environmentOwner(oldKey)
	moved := e.values.All(oldOwner)
	if len(moved) > 0 {
		newOwner := environmentOwner(newKey)
		e.async(func(ctx context.Context) error {
			for name, val := range moved {
				if err := e.values.Set(ctx, newOwner, name, val, false); err != nil {
					return err
				}
			}
			return e.values.ResetOwner(oldOwner)
		}, func(err error) {
			if err != nil {
				e.fail("move the current values of the environment", err)
			}
		})
	}
	if e.activeKey() == oldKey {
		e.setActive(newKey)
	}
}

// environmentFrom is the document of an environment node.
func environmentFrom(n *store.Node) format.Environment {
	if n.Environment != nil {
		return *n.Environment
	}
	return format.Environment{Name: n.Name}
}
