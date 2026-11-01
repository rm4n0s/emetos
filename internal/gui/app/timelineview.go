package app

import (
	"encoding/json"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/timeline"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	timelineview "github.com/rm4n0s/emetos/internal/gui/views/timeline"
)

// The Timeline view of the Explorer (SPEC §10.30 "The Timeline view", step 24.21): the history of the file in front, or of the file it is
// pinned to. The model is internal/gui/views/timeline (Pane) over the timeline service of the window (internal/core/timeline); this file
// makes it with the settings of the window, points it at the file of the active editor, follows the views of the workbench (a view that
// is folded loads nothing), keeps the context keys of the view, and binds its commands and its menus.
//
// Ported from VS Code: src/vs/workbench/contrib/timeline/browser/timeline.contribution.ts and timelinePane.ts

// timelineState is what the shell keeps of the Timeline view.
type timelineState struct {
	pane *timelineview.Pane
	// ctl is the list of the body, for the focus; menu hosts the context menus of the view.
	ctl  *kit.ListController
	menu *kit.ContextMenuController
	// height is the height of the list, for the automatic page size.
	height float64
	// description, following and excluded are what the keys and the header were last made from.
	description string
	following   bool
	excluded    string
	// stops end what the view follows, and removeCommands give its commands back.
	stops          []func()
	removeCommands []func()
}

// timelineWindowState is the Timeline part of the saved state of a window: the sources the user filtered out.
type timelineWindowState struct {
	Excluded []string `json:"excluded,omitempty"`
}

// decodeTimelineState reads the saved state of the view. The tag is TimelineStateInvalid.
func decodeTimelineState(raw json.RawMessage) (timelineWindowState, error) {
	var st timelineWindowState
	if err := json.Unmarshal(raw, &st); err != nil {
		return timelineWindowState{}, errors.NewErr("TimelineStateInvalid", err)
	}
	return st, nil
}

// timelineWindowState is the state of the view to save with the window; nothing while the filter is empty.
func (s *shellState) timelineWindowState() json.RawMessage {
	pane := s.timeline.pane
	if pane == nil {
		return s.win.pendingTimeline
	}
	excluded := pane.Excluded()
	if len(excluded) == 0 {
		return nil
	}
	data, err := json.Marshal(timelineWindowState{Excluded: excluded})
	if err != nil {
		s.win.Process.Logger.Warn("the state of the Timeline view cannot be saved", "err", err)
		return nil
	}
	return data
}

// timelineVisible says whether the view is on screen: the Explorer is in the side bar and the view is shown and not folded.
func (s *shellState) timelineVisible() bool {
	win := s.win
	return win.Layout.State().SideBarVisible && win.Views.Active() == command.ViewContainerExplorer &&
		!win.Views.IsCollapsed(command.ViewTimeline) && !win.Views.IsHidden(command.ViewTimeline)
}

// initTimeline makes the model of the view and follows what it depends on.
func (s *shellState) initTimeline(binding *widgets.WidgetsBinding) {
	win := s.win
	svc := win.Timeline
	if svc == nil {
		return
	}
	st := &s.timeline
	st.ctl = kit.NewListController()
	st.menu = kit.NewContextMenuController()
	var excluded []string
	if len(win.pendingTimeline) > 0 {
		if saved, err := decodeTimelineState(win.pendingTimeline); err != nil {
			win.Process.Logger.Warn("the saved state of the Timeline view cannot be used", "err", err)
		} else {
			excluded = saved.Excluded
		}
		win.pendingTimeline = nil
	}
	st.pane = timelineview.NewPane(timelineview.PaneOpts{
		Ctx: win.ctx, Service: svc, Post: binding.Post, Logger: win.Process.Logger,
		PageSize: func() int {
			if n, ok := settings.Get[any](win.Settings, "timeline.pageSize", "").(float64); ok {
				return int(n)
			}
			return 0
		},
		PageOnScroll: func() bool { return settings.Get[bool](win.Settings, "timeline.pageOnScroll", "") },
		RenderHeight: func() float64 { return st.height },
		SCMProviders: func() int {
			if win.SCM == nil {
				return 0
			}
			return win.SCM.Count()
		},
		Visible:  s.timelineVisible,
		Excluded: excluded,
		ExcludedChanged: func([]string) {
			s.syncTimelineParts()
			s.stateChangedIfMounted()
		},
		Run: s.timelineRun,
		AfterFunc: func(d time.Duration, f func()) func() {
			t := binding.Gestures().NewTimer(d, f)
			return t.Cancel
		},
	})
	st.stops = append(st.stops, st.pane.Dispose,
		st.pane.OnChange(func() { s.syncTimelineParts() }),
		win.Views.OnChange(func() { st.pane.VisibilityChanged() }),
		svc.OnProviders(func(timeline.ProvidersChange) { binding.Post(func() { s.syncTimelineParts() }) }),
	)
	s.bindTimelineCommands(binding)
	s.syncTimelineParts()
	s.syncTimeline()
	st.pane.VisibilityChanged()
}

// disposeTimeline ends the view and gives the commands back.
func (s *shellState) disposeTimeline() {
	st := &s.timeline
	for _, stop := range st.stops {
		stop()
	}
	for _, remove := range st.removeCommands {
		remove()
	}
	if win := s.win; win != nil {
		for _, key := range []string{"timelineHasProvider", "timelineFollowActiveEditor", "timelineExcludeSources"} {
			win.Keys.Reset(key)
		}
	}
	*st = timelineState{}
}

// timelineActiveURI is the file the Timeline follows: the file of the active text editor, or the modified side of a diff of a file (VS Code
// shows the primary side of an editor of two). keep says that the editor shows many resources (a commit opened from the Timeline), which
// leaves the view where it is.
func (s *shellState) timelineActiveURI() (uri string, keep bool) {
	if s.group.grid == nil {
		return "", false
	}
	g := s.activeGroup()
	if g == nil {
		return "", false
	}
	switch in := g.Model.ActiveEditor().(type) {
	case *editorgroup.MultiDiffInput:
		return "", true
	case *editorgroup.DiffInput:
		if d := in.Modified(); d != nil && d.URI().Scheme == document.SchemeFile {
			return "file://" + d.URI().Path, false
		}
	case nil:
	default:
		if res, ok := in.Resource(); ok && res.Scheme == document.SchemeFile {
			return "file://" + res.Path, false
		}
	}
	return "", false
}

// syncTimeline points the Timeline at the file of the active editor. The change is made after the build that is going on, as the editors
// may be made while the shell builds.
func (s *shellState) syncTimeline() {
	if s.timeline.pane == nil || s.group.grid == nil {
		return
	}
	widgets.WidgetsBindingOf(s.Context()).Post(func() {
		st := &s.timeline
		if !s.Mounted() || st.pane == nil || s.group.grid == nil {
			return
		}
		uri, keep := s.timelineActiveURI()
		if keep {
			return
		}
		st.pane.SetActive(uri)
	})
}

// syncTimelineParts keeps the context keys of the view and the header of its view in step with the pane.
func (s *shellState) syncTimelineParts() {
	st := &s.timeline
	win := s.win
	if st.pane == nil || win == nil {
		return
	}
	excluded, err := json.Marshal(st.pane.Excluded())
	if err != nil {
		excluded = []byte("[]")
	}
	win.Keys.Batch(func() {
		win.Keys.Set("timelineHasProvider", win.Timeline.HasProvider())
		win.Keys.Set("timelineFollowActiveEditor", st.pane.Following())
		win.Keys.Set("timelineExcludeSources", string(excluded))
	})
	if st.description != st.pane.Description() || st.following != st.pane.Following() || st.excluded != string(excluded) {
		st.description, st.following, st.excluded = st.pane.Description(), st.pane.Following(), string(excluded)
		// the header shows the description, and the buttons of its title follow the state
		win.Views.Changed()
	}
}

// timelineRun runs a command of the registry for an item that was opened or a button of the view.
func (s *shellState) timelineRun(id string, args any) {
	win := s.win
	if err := win.Commands.Run(command.Context{Ctx: win.ctx, Keys: win.Keys, Source: command.SourceMenu}, id, args); err != nil {
		win.Process.Logger.Warn("a command of the Timeline view failed", "command", id, "err", err)
	}
}

// timelineBody is the body of the view: the list, or the message, inside the host of the context menus.
func (s *shellState) timelineBody() widgets.Widget {
	st := &s.timeline
	if st.pane == nil {
		return nil
	}
	win := s.win
	binding := widgets.WidgetsBindingOf(s.Context())
	body := timelineview.NewBody(st.pane, timelineview.BodyOpts{
		Post: binding.Post, Controller: st.ctl,
		OnHeight: func(h float64) { st.height = h },
		Actions: func(row timelineview.Row) []kit.Action {
			var out []kit.Action
			for _, e := range win.Menus.Items(command.TimelineItemContext, menuContext{base: win.Keys, settings: win.Settings, extra: st.pane.ItemContext(row)}) {
				if e.Command != "" && e.Group == "inline" {
					out = append(out, kit.Action{ID: e.Command, Label: e.Title, Icon: scmIconCode(e.Icon), Disabled: e.Disabled})
				}
			}
			return out
		},
		RunAction: func(row timelineview.Row, id string) { s.timelineRun(id, st.pane.ItemArgs(row)) },
		ShowMenu: func(row timelineview.Row, at ui.Offset) {
			ctx := menuContext{base: win.Keys, settings: win.Settings, extra: st.pane.ItemContext(row)}
			items := s.menus.ItemsWithout(command.TimelineItemContext, ctx, func(e command.MenuEntry) bool { return e.Group == "inline" })
			st.menu.Show(at, withSCMArgs(items, st.pane.ItemArgs(row)))
		},
	})
	return kit.NewContextMenu(body, kit.ContextMenuOpts{
		Controller: st.menu, OnRun: s.menus.Run,
		OnHide: func() { s.afterFrame(binding, func() { st.ctl.Focus() }) },
	})
}

// viewDescription is the text after the title of a view's header: the file the Timeline shows.
func (s *shellState) viewDescription(viewID string) string {
	if viewID == command.ViewTimeline && s.timeline.pane != nil {
		return s.timeline.pane.Description()
	}
	if viewID == command.ViewSCMHistory {
		return s.graphDescription()
	}
	return ""
}

// timelineViewActions are the buttons of the title of the view: Refresh, Pin Current Timeline (Unpin while it is pinned), and the filter of
// the sources, which opens a menu with a row for each source, checked while it is shown.
//
// Ported from VS Code: timelinePane.ts (TimelinePaneCommands)
func (s *shellState) timelineViewActions() widgets.Widget {
	st := &s.timeline
	if st.pane == nil {
		return nil
	}
	icon := func(name string) int { code, _ := fonts.CodiconByName(name); return code }
	pin := kit.Action{ID: "timeline.toggleFollowActiveEditor", Label: "Pin Current Timeline", Icon: icon("pin")}
	if !st.pane.Following() {
		pin = kit.Action{ID: "timeline.toggleFollowActiveEditor", Label: "Unpin Current Timeline", Icon: icon("pinned")}
	}
	actions := []kit.Action{
		{ID: "timeline.refresh", Label: "Refresh", Icon: icon("refresh")},
		pin,
	}
	cells := []widgets.Widget{kit.NewActionBar(actions, kit.ActionBarOpts{OnRun: func(a kit.Action) { s.timelineRun(a.ID, nil) }})}
	if sources := s.win.Timeline.Sources(); len(sources) > 0 {
		cells = append(cells, widgets.NewGestureDetector(
			widgets.NewIcon(fonts.CodiconIcon(icon("filter")), widgets.IconOpts{Size: foundation.Some(16.0)}),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTapDown: func(d gestures.TapDownDetails) {
				items := make([]kit.MenuItem, 0, len(sources))
				for _, src := range sources {
					id := src.ID
					items = append(items, kit.MenuItem{Label: src.Label, Checked: !st.pane.IsExcluded(id), OnRun: func() { st.pane.ToggleSource(id) }})
				}
				st.menu.Show(ui.Offset{Dx: d.GlobalPosition.Dx, Dy: d.GlobalPosition.Dy + 10}, items)
			}}))
	}
	return widgets.NewRow(cells, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// timelineFocused is told that the focus entered or left the view: the ages are written again when it comes in.
func (s *shellState) timelineFocused(has bool) {
	if has && s.timeline.pane != nil {
		s.timeline.pane.Touch()
	}
}

// bindTimelineCommands takes over the commands of the Timeline while the shell is on screen.
func (s *shellState) bindTimelineCommands(binding *widgets.WidgetsBinding) {
	win := s.win
	st := &s.timeline
	show := func() {
		win.Views.Open(command.ViewContainerExplorer)
		win.Views.SetHidden(command.ViewTimeline, false)
		win.Views.SetCollapsed(command.ViewTimeline, false)
	}
	handlers := map[string]command.Handler{
		"timeline.focus": func(command.Context, any) error {
			show()
			s.afterFrame(binding, st.ctl.Focus)
			return nil
		},
		"timeline.refresh": func(command.Context, any) error { st.pane.Reset(); return nil },
		"timeline.toggleFollowActiveEditor": func(command.Context, any) error {
			st.pane.SetFollowing(!st.pane.Following())
			return nil
		},
		// Open Timeline: the Explorer and the tab give the file; without one the view opens and keeps what it shows
		"files.openTimeline": func(_ command.Context, args any) error {
			path := s.timelinePathOf(args)
			show()
			if path != "" {
				win.Timeline.Show("file://" + path)
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

// timelinePathOf is the file Open Timeline is for: the path the Explorer gives, the tab that was clicked, else the active editor.
//
// Ported from VS Code: timeline.contribution.ts (OpenTimelineAction), files.ts (getResourceForCommand)
func (s *shellState) timelinePathOf(args any) string {
	if p, ok := args.(string); ok && p != "" {
		return p
	}
	if list, ok := args.([]any); ok {
		for _, a := range list {
			if p, ok := a.(string); ok && p != "" {
				return p
			}
		}
	}
	if _, e := s.commandTarget(args); e != nil {
		if p, ok := fileResource(e); ok {
			return p
		}
	}
	return ""
}
