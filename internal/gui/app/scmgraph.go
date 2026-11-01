package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/views/scmgraph"
)

// The Graph view of Source Control (SPEC §10.30 "The Source Control Graph", step 24.20): the history of the repository as lanes. The
// model is internal/gui/views/scmgraph (Pane) over the history providers of the source controls (internal/core/scm); this file makes it
// with the settings of the window, shows and hides it with its container, keeps the context keys of the view, and binds its commands,
// its title and its menus.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmHistoryViewPane.ts

// graphState is what the shell keeps of the Graph view.
type graphState struct {
	pane *scmgraph.Pane
	ctl  *kit.ListController
	// stops end what the view follows, and removeCommands give its commands back.
	stops          []func()
	removeCommands []func()
	// outdated and header are what the header of the view was last made from.
	outdated bool
	header   string
	// showHover is a commit whose details show at once (a proof image and the tests: the dev command dev.scm.graph.hover).
	showHover string
}

// scmGraphSettings are the settings the view reads.
var scmGraphSettings = []string{"scm.graph.pageSize", "scm.graph.pageOnScroll", "scm.graph.badges", "scm.graph.showIncomingChanges", "scm.graph.showOutgoingChanges"}

// graphVisible says whether the view is on screen: the container is in the side bar and the view is shown and not folded.
func (s *shellState) graphVisible() bool {
	win := s.win
	return win.Layout.State().SideBarVisible && win.Views.Active() == command.ViewContainerSCM &&
		!win.Views.IsCollapsed(command.ViewSCMHistory) && !win.Views.IsHidden(command.ViewSCMHistory)
}

// initGraph makes the model of the view and follows what it depends on.
func (s *shellState) initGraph(binding *widgets.WidgetsBinding) {
	win := s.win
	m := win.SCM
	if m == nil {
		return
	}
	st := &s.graph
	st.ctl = kit.NewListController()
	pane := scmgraph.NewPane(scmgraph.PaneOpts{
		Ctx: win.ctx, Model: m, Post: binding.Post, Logger: win.Process.Logger,
		PageSize:     func() int { return int(settings.Get[float64](win.Settings, "scm.graph.pageSize", "")) },
		PageOnScroll: scmFlag(win, "scm.graph.pageOnScroll"),
		Badges:       func() string { return settings.Get[string](win.Settings, "scm.graph.badges", "") },
		ShowIncoming: scmFlag(win, "scm.graph.showIncomingChanges"), ShowOutgoing: scmFlag(win, "scm.graph.showOutgoingChanges"),
		Visible: s.graphVisible,
		AtTop:   func() bool { return st.ctl.ScrollOffset() < 1 },
		Run:     s.scmRun,
		Filters: win.pendingSCMGraph,
		FiltersChanged: func(map[string]scmgraph.Filter) {
			s.stateChangedIfMounted()
		},
		AfterFunc: func(d time.Duration, f func()) func() {
			t := binding.Gestures().NewTimer(d, f)
			return t.Cancel
		},
	})
	win.pendingSCMGraph = nil
	st.pane = pane
	st.stops = append(st.stops, pane.Dispose,
		pane.OnChange(func() { s.syncGraphParts() }),
		win.Views.OnChange(func() { pane.VisibilityChanged() }),
		// the view is in the list while a source control has a history: the list is made again when the count changes
		win.Keys.OnChange(func(e contextkey.ChangeEvent) {
			if e.AffectsSome([]string{"scm.historyProviderCount"}) {
				binding.Post(func() { win.Views.Changed() })
			}
		}),
		win.Settings.OnChange(func(e settings.ChangeEvent) {
			for _, key := range scmGraphSettings {
				if e.Affects(key, "") {
					binding.Post(func() { pane.Refresh() })
					return
				}
			}
		}),
	)
	s.bindGraphCommands(binding)
	s.syncGraphParts()
	pane.VisibilityChanged()
}

// disposeGraph ends the view and gives the commands back.
func (s *shellState) disposeGraph() {
	st := &s.graph
	for _, stop := range st.stops {
		stop()
	}
	for _, remove := range st.removeCommands {
		remove()
	}
	if win := s.win; win != nil {
		for _, key := range []string{"scmHistoryItemCount", "scmCurrentHistoryItemRefInFilter", "scmCurrentHistoryItemRefHasRemote", "scmCurrentHistoryItemRefHasBase"} {
			win.Keys.Reset(key)
		}
	}
	*st = graphState{}
}

// syncGraphParts keeps the context keys of the view and its header in step with the pane.
func (s *shellState) syncGraphParts() {
	st := &s.graph
	win := s.win
	if st.pane == nil || win == nil {
		return
	}
	ctx := st.pane.TitleContext()
	win.Keys.Batch(func() {
		for _, key := range []string{"scmHistoryItemCount", "scmCurrentHistoryItemRefInFilter", "scmCurrentHistoryItemRefHasRemote", "scmCurrentHistoryItemRefHasBase"} {
			if v, ok := ctx[key]; ok {
				win.Keys.Set(key, v)
			} else {
				win.Keys.Set(key, key == "scmHistoryItemCount")
			}
		}
	})
	// the header shows the filter, the repository and the "Outdated" mark: it is made again when one of them changed
	header := st.pane.FilterLabel() + "|" + st.pane.FilterTooltip()
	if src := st.pane.Source(); src != nil {
		header += "|" + src.Name() + "|" + fmt.Sprint(len(st.pane.Sources()))
	}
	if st.outdated != st.pane.Outdated() || st.header != header {
		st.outdated, st.header = st.pane.Outdated(), header
		win.Views.Changed()
	}
}

// graphDescription is the text after the title of the header: "Outdated" while references changed in the background.
func (s *shellState) graphDescription() string {
	if s.graph.pane != nil && s.graph.pane.Outdated() {
		return "Outdated"
	}
	return ""
}

// graphMenuContext is the context the menus of the Graph are evaluated in.
func (s *shellState) graphMenuContext(extra map[string]any) menuContext {
	return menuContext{base: s.win.Keys, settings: s.win.Settings, extra: extra}
}

// graphBody is the body of the view: the search box and the list, or the message, inside the host of the context menus.
func (s *shellState) graphBody() widgets.Widget {
	st := &s.graph
	if st.pane == nil {
		return nil
	}
	win := s.win
	binding := widgets.WidgetsBindingOf(s.Context())
	body := scmgraph.NewBody(st.pane, scmgraph.BodyOpts{
		Post: binding.Post, Controller: st.ctl, ShowHover: st.showHover,
		Actions: func(row scmgraph.Row) []kit.Action {
			if row.Kind == scmgraph.RowLoadMore {
				return nil
			}
			var out []kit.Action
			for _, e := range win.Menus.Items(command.SCMHistoryItemContext, s.graphMenuContext(st.pane.ItemContext(row))) {
				if e.Command != "" && e.Group == "inline" {
					out = append(out, kit.Action{ID: e.Command, Label: e.Title, Icon: scmIconCode(e.Icon), Disabled: e.Disabled})
				}
			}
			return out
		},
		RunAction: func(row scmgraph.Row, id string) { s.scmRun(id, st.pane.ItemArgs(row, "")) },
		ShowMenu:  s.showGraphMenu,
	})
	return kit.NewContextMenu(body, kit.ContextMenuOpts{
		Controller: s.scm.menu, OnRun: s.menus.Run,
		OnHide: func() { s.afterFrame(binding, func() { st.ctl.Focus() }) },
	})
}

// showGraphMenu opens the context menu of a commit: the rows of the menu of the commits, and, as VS Code does, a submenu with a row
// for each reference of the commit for every action of the menu of the references that applies to one of them (Checkout, Delete
// Branch, Delete Tag).
//
// Ported from VS Code: scmHistoryViewPane.ts (_onContextMenu)
func (s *shellState) showGraphMenu(row scmgraph.Row, at ui.Offset) {
	win := s.win
	pane := s.graph.pane
	if row.Kind != scmgraph.RowItem {
		return // the incoming and the outgoing changes have no menu
	}
	type refAction struct {
		entry command.MenuEntry
		refs  []scmcore.HistoryRef
	}
	var actions []*refAction
	for _, ref := range row.Item.Refs {
		extra := pane.ItemContext(row)
		extra["scmHistoryItemRef"] = ref.ID
		for _, e := range win.Menus.Items(command.SCMHistoryItemRefContext, s.graphMenuContext(extra)) {
			if e.Command == "" {
				continue
			}
			i := slices.IndexFunc(actions, func(a *refAction) bool { return a.entry.Command == e.Command })
			if i < 0 {
				actions = append(actions, &refAction{entry: e})
				i = len(actions) - 1
			}
			actions[i].refs = append(actions[i].refs, ref)
		}
	}
	var removes []func()
	defer func() {
		// the items live as long as the menu is built
		for _, r := range removes {
			r()
		}
	}()
	for n, a := range actions {
		sub := fmt.Sprintf("scm.graph.refs.%d", n)
		items := []command.MenuItem{{Menu: command.SCMHistoryItemContext, Submenu: sub, Title: a.entry.Title,
			Group: ref0Group(win, a.entry.Command), Order: ref0Order(win, a.entry.Command)}}
		for _, ref := range a.refs {
			items = append(items, command.MenuItem{Menu: sub, Command: a.entry.Command, Title: ref.Name, Group: ref.Category, Args: pane.ItemArgs(row, ref.ID)})
		}
		remove, err := win.Menus.AddRemovable(items...)
		if err != nil {
			win.Process.Logger.Warn("the menu of the references of a commit cannot be made", "err", err)
			continue
		}
		removes = append(removes, remove)
	}
	items := s.menus.ItemsWithout(command.SCMHistoryItemContext, s.graphMenuContext(pane.ItemContext(row)), func(e command.MenuEntry) bool { return e.Group == "inline" })
	s.scm.menu.Show(at, withSCMArgs(items, pane.ItemArgs(row, "")))
}

// ref0Group and ref0Order are the group and the place of an action of the menu of the references, which its submenu takes in the menu
// of the commits.
func ref0Group(win *Workspace, cmd string) string {
	for _, it := range win.Menus.ItemsOf(command.SCMHistoryItemRefContext) {
		if it.Command == cmd {
			return it.Group
		}
	}
	return ""
}

func ref0Order(win *Workspace, cmd string) float64 {
	for _, it := range win.Menus.ItemsOf(command.SCMHistoryItemRefContext) {
		if it.Command == cmd {
			return it.Order
		}
	}
	return 0
}

// graphViewActions are the buttons of the title of the view: the repository picker (with several repositories), the filter of the
// references, and the other actions of the menu of the title as icons.
//
// Ported from VS Code: scmHistoryViewPane.ts (SCMRepositoryActionViewItem, SCMHistoryItemRefsActionViewItem, the actions of
// SCMHistoryTitle)
func (s *shellState) graphViewActions() widgets.Widget {
	st := &s.graph
	if st.pane == nil || st.pane.Source() == nil {
		return nil
	}
	pane := st.pane
	win := s.win
	args := pane.TitleArgs()
	var icons []kit.Action
	var cells []widgets.Widget
	labeled := func(icon, text, tooltip, id string) widgets.Widget {
		return widgets.NewGestureDetector(
			kit.NewHover(widgets.NewRow([]widgets.Widget{
				widgets.NewIcon(fonts.CodiconIcon(scmIconCode(icon)), widgets.IconOpts{Size: foundation.Some(14.0)}),
				widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(3, 0, 4, 0), widgets.NewConstrainedBox(
					rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(110.0)}),
					widgets.NewText(text, widgets.TextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis}))),
			}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
				kit.HoverOpts{Text: tooltip, Compact: true}),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { s.scmRun(id, args) }})
	}
	for _, e := range win.Menus.Items(command.SCMHistoryTitle, s.graphMenuContext(pane.TitleContext())) {
		if e.Command == "" || e.Group != "navigation" {
			continue
		}
		switch e.Command {
		case "workbench.scm.action.graph.pickRepository":
			src := pane.Source()
			cells = append(cells, labeled("repo", src.Name(), src.Name(), e.Command))
		case "workbench.scm.action.graph.pickHistoryItemRefs":
			cells = append(cells, labeled("git-branch", pane.FilterLabel(), pane.FilterTooltip(), e.Command))
		default:
			icons = append(icons, kit.Action{ID: e.Command, Label: e.Title, Icon: scmIconCode(e.Icon), Disabled: e.Disabled})
		}
	}
	if len(icons) > 0 {
		cells = append(cells, kit.NewActionBar(icons, kit.ActionBarOpts{OnRun: func(a kit.Action) { s.scmRun(a.ID, args) }}))
	}
	if len(cells) == 0 {
		return nil
	}
	return widgets.NewRow(cells, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// bindGraphCommands takes over the commands of the Graph while the shell is on screen.
func (s *shellState) bindGraphCommands(binding *widgets.WidgetsBinding) {
	win := s.win
	st := &s.graph
	show := func() {
		win.Views.Open(command.ViewContainerSCM)
		win.Views.SetHidden(command.ViewSCMHistory, false)
		win.Views.SetCollapsed(command.ViewSCMHistory, false)
	}
	handlers := map[string]command.Handler{
		"workbench.scm.history.focus": func(command.Context, any) error {
			show()
			s.afterFrame(binding, st.ctl.Focus)
			return nil
		},
		"workbench.scm.action.graph.pickRepository":           func(command.Context, any) error { s.pickGraphRepository(); return nil },
		"workbench.scm.action.graph.pickHistoryItemRefs":      func(command.Context, any) error { s.pickGraphRefs(); return nil },
		"workbench.scm.action.graph.revealCurrentHistoryItem": func(command.Context, any) error { st.pane.RevealCurrent(); return nil },
		"workbench.scm.action.graph.refresh":                  func(command.Context, any) error { st.pane.Refresh(); return nil },
		"workbench.scm.action.graph.viewChanges": func(_ command.Context, args any) error {
			if arg, ok := extension.HistoryArgs(args); ok && arg.Item != nil {
				st.pane.OpenID(arg.Item.ID)
			}
			return nil
		},
	}
	for id, h := range handlers {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		st.removeCommands = append(st.removeCommands, remove)
	}
}

// pickGraphRepository shows the list of the repositories that have a history; Auto follows the active repository.
//
// Ported from VS Code: scmHistoryViewPane.ts (RepositoryPicker)
func (s *shellState) pickGraphRepository() {
	win := s.win
	pane := s.graph.pane
	items := []quickinput.Item{
		{ID: "auto", Label: "Auto", Description: "Show the source control graph for the active repository", Accept: func(quickinput.KeyMods) { pane.SelectSource(nil) }},
		{Separator: true},
	}
	active := "auto"
	if pane.Selected() != nil {
		active = pane.Selected().Key()
	}
	for _, src := range pane.Sources() {
		src := src
		items = append(items, quickinput.Item{ID: src.Key(), Label: src.Name(), Description: src.RootPath(), Icon: scmIconCode("repo"),
			Accept: func(quickinput.KeyMods) { pane.SelectSource(src) }})
	}
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select the repository to view, type to filter all repositories", Items: items, ActiveID: active,
	})
}

// pickGraphRefs shows the list of the references to choose what the graph shows: All, Auto, or some references. The quick input has
// no list with several choices, so a reference is chosen or left out one by one: the list comes back with the references marked until
// "Apply" (or All or Auto) is chosen.
//
// Ported from VS Code: scmHistoryViewPane.ts (HistoryItemRefPicker)
func (s *shellState) pickGraphRefs() {
	pane := s.graph.pane
	pane.Refs(func(refs []scmcore.HistoryRef, err error) {
		if err != nil {
			s.win.Process.Logger.Warn("the references cannot be listed", "err", err)
			return
		}
		f := pane.Filter()
		selected := map[string]bool{}
		if f.Kind == scmgraph.FilterRefs {
			for _, id := range f.Refs {
				selected[id] = true
			}
		}
		s.showRefPicker(refs, selected, f.Kind)
	})
}

// showRefPicker shows the picker of the references with the marks of the chosen ones.
func (s *shellState) showRefPicker(refs []scmcore.HistoryRef, selected map[string]bool, kind scmgraph.FilterKind) {
	pane := s.graph.pane
	win := s.win
	var items []quickinput.Item
	if len(selected) > 0 {
		n := len(selected)
		items = append(items, quickinput.Item{ID: "apply", Label: fmt.Sprintf("$(check) Apply (%d selected)", n), Description: "Show the history of the selected references", AlwaysShow: true,
			Accept: func(quickinput.KeyMods) {
				var ids []string
				for _, r := range refs {
					if selected[r.ID] {
						ids = append(ids, r.ID)
					}
				}
				pane.SetFilter(scmgraph.Filter{Kind: scmgraph.FilterRefs, Refs: ids})
			}})
	}
	mark := func(on bool, text string) string {
		if on {
			return "$(check) " + text
		}
		return text
	}
	items = append(items,
		quickinput.Item{ID: "all", Label: mark(kind == scmgraph.FilterAll && len(selected) == 0, "All"), Description: "All history item references", Accept: func(quickinput.KeyMods) { pane.SetFilter(scmgraph.Filter{Kind: scmgraph.FilterAll}) }},
		quickinput.Item{ID: "auto", Label: mark(kind != scmgraph.FilterAll && len(selected) == 0, "Auto"), Description: "Current history item reference(s)", Accept: func(quickinput.KeyMods) { pane.SetFilter(scmgraph.Filter{}) }},
	)
	category := ""
	for _, r := range refs {
		r := r
		if r.Category != category {
			category = r.Category
			items = append(items, quickinput.Item{Separator: true, Label: category})
		}
		code, _ := fonts.CodiconByName(r.Icon)
		items = append(items, quickinput.Item{ID: r.ID, Label: mark(selected[r.ID], r.Name), Description: r.Description, Icon: code, Accept: func(quickinput.KeyMods) {
			if selected[r.ID] {
				delete(selected, r.ID)
			} else {
				selected[r.ID] = true
			}
			s.afterFrame(widgets.WidgetsBindingOf(s.Context()), func() { s.showRefPicker(refs, selected, scmgraph.FilterRefs) })
		}})
	}
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select one/more history item references to view, type to filter", Items: items, MatchOnDescription: true,
	})
}
