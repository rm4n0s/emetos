package app

import (
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	dbgview "github.com/rm4n0s/emetos/internal/gui/views/debug"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The parts of Run and Debug outside its views (step 25.4): the buttons in the title of the container, the toolbar over the editor area
// (or docked in that title), and the launch item of the status bar.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugViewlet.ts (the start action view item), debugToolBar.ts and
// debugStatus.ts

// debugToolbarLocation is the setting debug.toolBarLocation: floating, docked or hidden.
func (s *shellState) debugToolbarLocation() string {
	return settings.Get[string](s.win.Settings, "debug.toolBarLocation", "")
}

// debugToolbarShown tells whether the debug toolbar is on screen in the floating place: a session runs that does not hide the toolbar.
func (s *shellState) debugToolbarShown() bool {
	svc := s.win.DebugService()
	if svc == nil || s.debug.panes == nil || svc.State() == debug.Inactive {
		return false
	}
	for _, sess := range svc.Model().Sessions(false) {
		if !sess.Options().SuppressDebugToolbar {
			return true
		}
	}
	return false
}

// debugToolbarActions are the buttons of the toolbar: the items of group navigation of the toolbar's menu that hold now.
func (s *shellState) debugToolbarActions(colors kit.Colors) []kit.Action {
	win := s.win
	ctx := menuContext{base: win.Keys, settings: win.Settings}
	var out []kit.Action
	for _, e := range win.Menus.Items(command.DebugToolBar, ctx) {
		if e.Command == "" || e.Group != "navigation" {
			continue
		}
		code, _ := fonts.CodiconByName(e.Icon)
		tip := e.Title
		if kb := win.keyLabelOf(e.Command); kb != "" {
			tip += " (" + kb + ")"
		}
		out = append(out, kit.Action{ID: e.Command, Label: e.Title, Tooltip: tip, Icon: code, Disabled: e.Disabled, Color: dbgview.IconColor(colors, e.Command)})
	}
	return out
}

// withDebugToolbar puts the floating toolbar over the editor area while a session runs and debug.toolBarLocation is floating.
func (s *shellState) withDebugToolbar(editor widgets.Widget) widgets.Widget {
	if s.debug.panes == nil || s.debugToolbarLocation() != "floating" || !s.debugToolbarShown() {
		return editor
	}
	bar := widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		return dbgview.NewFloatingToolbar(dbgview.FloatingToolbarOpts{
			Actions: s.debugToolbarActions(kit.ThemeOf(bc)),
			OnRun:   func(a kit.Action) { s.runDebugCommand(a.ID, nil) },
			X:       s.debug.toolbarX,
			OnMoved: func(x float64) { s.debug.toolbarX = x },
		})
	})
	return widgets.NewStack([]widgets.Widget{editor, bar}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// debugSideBarActions are the buttons in the title of the side bar while the container of Run and Debug is shown: the start button, the
// dropdown of the configurations and the gear, and, while the toolbar is docked and a session runs, the toolbar's buttons.
func (s *shellState) debugSideBarActions(bc widgets.BuildContext) widgets.Widget {
	win := s.win
	if s.debug.panes == nil || !s.debugContainerVisible() {
		return nil
	}
	svc := win.DebugService()
	colors := kit.ThemeOf(bc)
	ctx := menuContext{base: win.Keys, settings: win.Settings, extra: map[string]any{"view": command.ViewContainerDebug}}
	var start, rest []kit.Action
	var more []command.MenuEntry
	for _, e := range win.Menus.Items(command.DebugContainerTitle, ctx) {
		if e.Command == "" {
			continue
		}
		if e.Group != "navigation" {
			more = append(more, e)
			continue
		}
		code, _ := fonts.CodiconByName(e.Icon)
		a := kit.Action{ID: e.Command, Label: e.Title, Icon: code, Disabled: e.Disabled, Color: dbgview.IconColor(colors, e.Command)}
		if e.Command == command.DebugStartCommand {
			start = append(start, a)
		} else {
			rest = append(rest, a)
		}
	}
	var cells []widgets.Widget
	run := func(a kit.Action) { s.runDebugCommand(a.ID, nil) }
	docked := svc.State() != debug.Inactive && s.debugToolbarLocation() == "docked"
	if docked && s.debugToolbarShown() {
		if actions := s.debugToolbarActions(colors); len(actions) > 0 {
			cells = append(cells, kit.NewActionBar(actions, kit.ActionBarOpts{OnRun: run}))
		}
	}
	if len(start) > 0 {
		cells = append(cells, kit.NewActionBar(start, kit.ActionBarOpts{OnRun: run}), s.debugConfigurationDropdown())
	}
	if len(rest) > 0 {
		cells = append(cells, kit.NewActionBar(rest, kit.ActionBarOpts{OnRun: run}))
	}
	if len(more) > 0 {
		items := make([]kit.MenuItem, 0, len(more))
		for _, e := range more {
			items = append(items, kit.MenuItem{ID: e.Command, Label: e.Title, Disabled: e.Disabled, Data: e.Args})
		}
		cells = append(cells, widgets.NewGestureDetector(
			widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconEllipsis), widgets.IconOpts{Size: foundation.Some(16.0)}),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTapDown: func(d gestures.TapDownDetails) {
				s.debug.menu.Show(ui.Offset{Dx: d.GlobalPosition.Dx, Dy: d.GlobalPosition.Dy + 10}, items)
			}}))
	}
	if len(cells) == 0 {
		return nil
	}
	return widgets.NewRow(cells, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// DebugConfigurationDropdownKey is the key of the dropdown of the configurations in the title of Run and Debug, which tests find it by.
var DebugConfigurationDropdownKey = foundation.NewValueKey("debug:configurations")

// debugConfigurationDropdown is the select box of the configurations in the title of the container: every configuration and compound of
// the launches, then "Add Config..." and "More...". Choosing a configuration selects it; the next start runs it.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugActionViewItems.ts (StartDebugActionViewItem)
func (s *shellState) debugConfigurationDropdown() widgets.Widget {
	svc := s.win.DebugService()
	launches := svc.Launches()
	all := launches.All()
	sel := launches.Selected()
	many := folderLaunches(launches) > 1
	var options []kit.DropdownOption
	selected := -1
	for i, c := range all {
		label := c.Name
		if many {
			label += " (" + c.Launch.Name + ")"
		}
		options = append(options, kit.DropdownOption{Label: label})
		if c.Launch == sel.Launch && c.Name == sel.Name {
			selected = i
		}
	}
	if sel.Dynamic && sel.Name != "" {
		options = append(options, kit.DropdownOption{Label: sel.Name})
		selected = len(options) - 1
	}
	n := len(options)
	if n == 0 {
		options = append(options, kit.DropdownOption{Label: "No Configurations", Disabled: true})
		selected = 0
	}
	addAt, moreAt := len(options), len(options)+1
	options = append(options, kit.DropdownOption{Label: "Add Config..."}, kit.DropdownOption{Label: "More..."})
	return widgets.NewSizedBox(kit.NewDropdown(options, kit.DropdownOpts{
		Key: DebugConfigurationDropdownKey, Selected: selected, Width: foundation.Some(130.0),
		OnChanged: func(i int) {
			switch {
			case i == addAt:
				s.runDebugCommand(command.DebugAddConfiguration, nil)
			case i == moreAt:
				s.runDebugCommand(command.DebugSelectAndStart, nil)
			case n > 0 && i < len(all):
				launches.Select(all[i].Launch, all[i].Name, debug.SelectOptions{})
			}
		},
	}), widgets.SizedBoxOpts{Width: foundation.Some(130.0)})
}

// syncDebugStatus shows the launch item of the status bar as debug.showInStatusBar says: always, never, or from the first session on.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugStatus.ts (DebugStatusContribution)
func (s *shellState) syncDebugStatus(svc *debug.Service, state debug.State) {
	win := s.win
	st := &s.debug
	show := false
	switch settings.Get[string](win.Settings, "debug.showInStatusBar", "") {
	case "always":
		show = true
	case "onFirstSessionStart":
		show = state != debug.Inactive || svc.HasDebugged()
	}
	if !show {
		if st.status != nil {
			st.status.Dispose()
			st.status = nil
		}
		return
	}
	text := ""
	if sel := svc.Launches().Selected(); sel.Name != "" && sel.Launch != nil {
		text = sel.Name
		if folderLaunches(svc.Launches()) > 1 {
			text += " (" + sel.Launch.Name + ")"
		}
	}
	entry := workbench.StatusEntry{
		Name: "Debug", Text: strings.TrimRight("$(debug-alt-small) "+text, " "), AriaLabel: "Debug: " + text,
		Tooltip: "Select and Start Debug Configuration", Command: command.DebugSelectAndStart,
	}
	if st.status == nil {
		st.status = win.StatusBar.Add("status.debug", workbench.StatusLeft, 30, entry)
		return
	}
	st.status.Update(entry)
}

var _ = painting.EdgeInsets{}

// folderLaunches counts the launches that have a launch.json of a folder: the ones the dropdown and the status item name configurations
// of, as VS Code names a launch when there are several folders. The launch of the settings is not one of them.
func folderLaunches(launches *debug.Launches) int {
	n := 0
	for _, l := range launches.Launches() {
		if l.Kind == debug.LaunchFolder {
			n++
		}
	}
	return n
}
