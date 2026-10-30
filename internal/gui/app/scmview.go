package app

import (
	"encoding/json"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	scmview "github.com/rm4n0s/emetos/internal/gui/views/scm"
	"github.com/rm4n0s/emetos/internal/gui/views/welcome"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The Source Control view of the window (SPEC §10.30, step 24.3): the Changes and Repositories views over the Source Control model,
// their commands, the badge of the activity bar icon, the status bar items of the active repository, the state that outlives the
// window and the decorations of the files. The model and its settings are in scm.go; the view is in internal/gui/views/scm.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scm.contribution.ts, activity.ts and scmViewPane.ts

// scmFileDecorations are the decorations of the files that the Source Control view draws as the letters of its rows. VS Code asks the
// file decoration service for them (the Git extension's provider answers); until that service is built (step 24.6) the window
// keeps what the host is told: the dev command dev.scm.fake sets them. It is safe for concurrent use.
type scmFileDecorations struct {
	mu sync.Mutex
	m  map[string]scmview.Decoration
}

// Set remembers the decoration of a file (a URI).
func (d *scmFileDecorations) Set(uri string, dec scmview.Decoration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.m == nil {
		d.m = map[string]scmview.Decoration{}
	}
	d.m[uri] = dec
}

// Lookup is the decoration of a file.
func (d *scmFileDecorations) Lookup(uri string) (scmview.Decoration, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	dec, ok := d.m[uri]
	return dec, ok
}

// scmState is what the shell keeps of the Source Control view.
type scmState struct {
	view  *scmview.View
	repos *scmview.Repositories
	// menu opens the context menus and the overflow menus of the views; it is hosted by the body of the Changes view.
	menu *kit.ContextMenuController
	// stops end what the view follows, and removeCommands give its commands back.
	stops          []func()
	removeCommands []func()
	// pending is set while a burst of changes of the model waits to be applied.
	pending atomic.Bool
	// status are the items of the status bar of the active repository.
	status []*workbench.StatusAccessor
	// revealedFor is the file the view was last asked to reveal.
	revealedFor string
	// focus tells which views of the container have the keyboard focus.
	focus map[string]bool
}

// scmSettingsKeys are the settings the view reads while it draws.
var scmSettingsKeys = []string{"scm.alwaysShowActions", "scm.alwaysShowRepositories", "scm.compactFolders", "scm.providerCountBadge"}

// scmWindowState is the Source Control part of the saved state of a window.
type scmWindowState struct {
	Model scmcore.State   `json:"model"`
	View  json.RawMessage `json:"view,omitempty"`
}

// restoreSCM gives the model the saved state of the window, before the extensions create their source controls.
func (w *Workspace) restoreSCM(raw json.RawMessage) {
	if w.SCM == nil || len(raw) == 0 {
		return
	}
	st, err := decodeSCMState(raw)
	if err != nil {
		w.Process.Logger.Warn("the saved state of the Source Control view cannot be used", "err", err)
		return
	}
	w.SCM.Restore(st.Model)
	w.pendingSCMView = st.View
}

// decodeSCMState reads the saved state of the view. The tag is ScmStateInvalid.
func decodeSCMState(raw json.RawMessage) (scmWindowState, error) {
	var st scmWindowState
	if err := json.Unmarshal(raw, &st); err != nil {
		return scmWindowState{}, errors.NewErr("ScmStateInvalid", err)
	}
	return st, nil
}

// encodeSCMState writes the state of the view to save. The tag is ScmStateEncodeFailed.
func encodeSCMState(st scmWindowState) (json.RawMessage, error) {
	data, err := json.Marshal(st)
	if err != nil {
		return nil, errors.NewErr("ScmStateEncodeFailed", err)
	}
	return data, nil
}

// scmWindowState is the state of the view to save with the window.
func (s *shellState) scmWindowState() json.RawMessage {
	win := s.win
	if win.SCM == nil {
		return nil
	}
	st := scmWindowState{Model: win.SCM.State()}
	if s.scm.view != nil {
		st.View = s.scm.view.SaveState()
	}
	data, err := encodeSCMState(st)
	if err != nil {
		win.Process.Logger.Warn("the state of the Source Control view cannot be saved", "err", err)
		return nil
	}
	return data
}

// scmFlag reads a boolean setting.
func scmFlag(win *Workspace, key string) func() bool {
	return func() bool { return settings.Get[bool](win.Settings, key, "") }
}

// initSCM makes the views of the Source Control container and follows the model.
func (s *shellState) initSCM(binding *widgets.WidgetsBinding) {
	win := s.win
	m := win.SCM
	if m == nil {
		return
	}
	st := &s.scm
	st.focus = map[string]bool{}
	st.menu = kit.NewContextMenuController()
	opts := scmview.Options{
		Model: m, Post: binding.Post,
		Run: s.scmRun,
		Entries: func(menu string, extra map[string]any) []command.MenuEntry {
			return win.Menus.Items(menu, menuContext{base: win.Keys, settings: win.Settings, extra: extra})
		},
		ShowMenu:     s.showSCMMenu,
		OpenResource: func(uri string, o scmview.OpenOptions) { s.scmOpen(binding, uri, o) },
		Decoration:   win.scmDecorations.Lookup,
		Icons:        win.FileIcons, Language: win.languageOf,
		AlwaysShowActions: scmFlag(win, "scm.alwaysShowActions"), AlwaysShowRepositories: scmFlag(win, "scm.alwaysShowRepositories"),
		AutoReveal: scmFlag(win, "scm.autoReveal"),
		BeginDrag: func(label string) (scmview.DragSession, error) {
			sess, err := extEditorHost{w: win}.BeginDrag(label)
			if err != nil {
				return nil, err
			}
			return scmDrag{sess}, nil
		},
		OpenIn: func(uri string, group, index int) error { return s.scmOpenIn(binding, uri, group, index) },
		Side: func() bool {
			kb := binding.Keyboard()
			return kb.IsControlPressed() || kb.IsAltPressed()
		},
		AfterFrame: func(f func()) { s.afterFrame(binding, f) },
	}
	st.view = scmview.New(opts)
	st.repos = scmview.NewRepositories(st.view)
	if len(win.pendingSCMView) > 0 {
		if err := st.view.RestoreState(win.pendingSCMView); err != nil {
			win.Process.Logger.Warn("the saved folds of the Source Control view cannot be used", "err", err)
		}
		win.pendingSCMView = nil
	}
	// the saved choice of the repositories applies to the ones that come in the next seconds
	timer := binding.Gestures().NewTimer(5*time.Second, func() { m.FinishRestore() })
	st.stops = append(st.stops, timer.Cancel, st.view.Dispose, st.repos.Dispose)
	st.stops = append(st.stops, m.Subscribe(func(e scmcore.Event) {
		if st.pending.Swap(true) {
			return
		}
		binding.Post(func() {
			st.pending.Store(false)
			if s.Mounted() {
				s.syncSCMParts()
				if e.Kind == scmcore.ViewChanged {
					s.stateChangedIfMounted()
				}
			}
		})
	}))
	// a fold of the view changes the keys of its title and the state to save
	st.stops = append(st.stops, st.view.OnChange(func() {
		if st.pending.Swap(true) {
			return
		}
		binding.Post(func() {
			st.pending.Store(false)
			if s.Mounted() && s.scm.view != nil {
				s.syncSCMParts()
			}
		})
	}))
	var keysPending atomic.Bool
	st.stops = append(st.stops, win.Keys.OnChange(func(contextkey.ChangeEvent) {
		if keysPending.Swap(true) {
			return
		}
		binding.Post(func() {
			keysPending.Store(false)
			if s.Mounted() && s.scm.view != nil {
				s.scm.view.InvalidateMenus()
			}
		})
	}))
	st.stops = append(st.stops, win.Settings.OnChange(func(e settings.ChangeEvent) {
		for _, key := range scmSettingsKeys {
			if e.Affects(key, "") {
				binding.Post(func() {
					if s.Mounted() && s.scm.view != nil {
						s.scm.view.Refresh()
					}
				})
				return
			}
		}
	}))
	s.bindSCMCommands(binding)
	s.bindDevSCM()
	s.syncSCMParts()
}

// disposeSCM ends the views and gives the commands back.
func (s *shellState) disposeSCM() {
	st := &s.scm
	for _, stop := range st.stops {
		stop()
	}
	for _, remove := range st.removeCommands {
		remove()
	}
	for _, it := range st.status {
		it.Dispose()
	}
	*st = scmState{}
}

// resetSCMParts takes the badge and the keys of the view away. It runs after the panels stopped following the views of the
// window, which a badge that goes would tell.
func (s *shellState) resetSCMParts() {
	win := s.win
	if win == nil || win.SCM == nil {
		return
	}
	win.Views.SetBadge(command.ViewContainerSCM, workbench.ActivityBadge{})
	for _, key := range []string{"scmViewIsAnyRepositoryCollapsible", "scmViewAreAllRepositoriesCollapsed"} {
		win.Keys.Reset(key)
	}
}

// scmDrag adapts the tab drag of the window to the drag of the view.
type scmDrag struct{ s exthost.DragSession }

func (d scmDrag) Move(at ui.Offset) { d.s.Move(at) }
func (d scmDrag) End(at ui.Offset, drop bool, open func(group, index int) error) (bool, error) {
	return d.s.End(at, drop, open)
}

// scmRun runs a command of the registry for a button or a link of the view.
func (s *shellState) scmRun(id string, args any) {
	win := s.win
	if err := win.Commands.Run(command.Context{Ctx: win.ctx, Keys: win.Keys, Source: command.SourceMenu}, id, args); err != nil {
		win.Process.Logger.Warn("a command of the Source Control view failed", "command", id, "err", err)
	}
}

// scmOpen opens the file of a resource that has no command of its own.
func (s *shellState) scmOpen(binding *widgets.WidgetsBinding, uri string, o scmview.OpenOptions) {
	p := scmcore.PathOfURI(uri)
	if !strings.HasPrefix(uri, "file://") {
		return
	}
	s.openPathAt(binding, p, 0, 0, openOptions{Preview: !o.Pinned, PreserveFocus: o.Preserve, TakeFocus: !o.Preserve})
}

// scmOpenIn opens a file in a group at a place of its strip: the end of the drag of a row.
func (s *shellState) scmOpenIn(binding *widgets.WidgetsBinding, uri string, group, index int) error {
	if !strings.HasPrefix(uri, "file://") {
		return nil
	}
	var g *editorgroup.Group
	if group > 0 {
		g = s.group.grid.Group(group)
	} else {
		g = s.activeGroup()
	}
	if g == nil {
		return errors.New("ScmDropGroupMissing", "the group the file was dropped on is gone", "group", group)
	}
	s.openPathAt(binding, scmcore.PathOfURI(uri), 0, 0, openOptions{Group: g, Sticky: false, Index: max(index, 0), HasIndex: index >= 0, TakeFocus: true})
	return nil
}

// showSCMMenu opens the context menu of a menu of the view at a position, in the context of the row; the commands that have no
// argument of their own get the arguments of the row.
func (s *shellState) showSCMMenu(menu string, extra map[string]any, args []any, at ui.Offset) {
	win := s.win
	ctx := menuContext{base: win.Keys, settings: win.Settings, extra: extra}
	items := s.menus.ItemsWithout(menu, ctx, func(e command.MenuEntry) bool { return e.Group == "inline" })
	s.scm.menu.Show(at, withSCMArgs(items, args))
}

// withSCMArgs gives the items without arguments the arguments of the row.
func withSCMArgs(items []kit.MenuItem, args []any) []kit.MenuItem {
	out := slices.Clone(items)
	for i, it := range out {
		switch {
		case it.Submenu:
			it.Items = withSCMArgs(it.Items, args)
		case !it.Separator && it.ID != "" && it.Data == nil:
			it.Data = args
		}
		out[i] = it
	}
	return out
}

// scmEntries are the entries of a menu in the context of the window with the keys of a row added.
func (s *shellState) scmEntries(menu string, extra map[string]any) []command.MenuEntry {
	win := s.win
	return win.Menus.Items(menu, menuContext{base: win.Keys, settings: win.Settings, extra: extra})
}

// scmTitleContext is the context of the title of the Changes view: the keys of the repository when one is shown, as VS Code sets
// them for the view.
func (s *shellState) scmTitleContext() (extra map[string]any, args []any) {
	extra = map[string]any{"view": scmview.ViewID}
	if vis := s.win.SCM.Visible(); len(vis) == 1 {
		for k, v := range vis[0].Context() {
			extra[k] = v
		}
		args = scmcore.SourceMenuArgs(vis[0])
	}
	return extra, args
}

// scmViewActions are the title actions of the Changes view: the buttons of the group navigation of the menu SCMTitle of the
// repository (View & Sort, collapse and expand all, and what Git adds: Commit, Refresh), and "..." with the other rows.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (the title menu of the view pane)
func (s *shellState) scmViewActions(win *Workspace) widgets.Widget {
	extra, args := s.scmTitleContext()
	entries := s.scmEntries(command.SCMTitle, extra)
	var buttons []kit.Action
	for _, e := range entries {
		if e.Command != "" && e.Group == "navigation" {
			buttons = append(buttons, kit.Action{ID: e.Command, Label: e.Title, Icon: scmIconCode(e.Icon), Disabled: e.Disabled})
		}
	}
	more := s.menus.ItemsWithout(command.SCMTitle, menuContext{base: win.Keys, settings: win.Settings, extra: extra},
		func(e command.MenuEntry) bool { return e.Group == "navigation" })
	cells := []widgets.Widget{}
	if len(buttons) > 0 {
		cells = append(cells, kit.NewActionBar(buttons, kit.ActionBarOpts{OnRun: func(a kit.Action) {
			s.scmRun(a.ID, args)
		}}))
	}
	if len(more) > 0 {
		items := withSCMArgs(more, args)
		cells = append(cells, widgets.NewGestureDetector(
			widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconEllipsis), widgets.IconOpts{Size: foundation.Some(16.0)}),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTapDown: func(d gestures.TapDownDetails) {
				s.scm.menu.Show(ui.Offset{Dx: d.GlobalPosition.Dx, Dy: d.GlobalPosition.Dy + 10}, items)
			}}))
	}
	if len(cells) == 0 {
		return nil
	}
	return widgets.NewRow(cells, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// scmRepositoriesActions is the "..." of the Repositories view: the order of the repositories and how many are shown.
func (s *shellState) scmRepositoriesActions(win *Workspace) widgets.Widget {
	items := s.menus.Items(command.SCMRepositories, menuContext{base: win.Keys, settings: win.Settings, extra: map[string]any{"view": scmview.RepositoriesID}})
	if len(items) == 0 {
		return nil
	}
	return widgets.NewGestureDetector(
		widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconEllipsis), widgets.IconOpts{Size: foundation.Some(16.0)}),
		widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTapDown: func(d gestures.TapDownDetails) {
			s.scm.menu.Show(ui.Offset{Dx: d.GlobalPosition.Dx, Dy: d.GlobalPosition.Dy + 10}, items)
		}})
}

func scmIconCode(name string) int {
	code, _ := fonts.CodiconByName(name)
	return code
}

// scmBody is the body of the Changes view: the list, or the welcome content while no repository exists, inside the host of the
// context menus.
func (s *shellState) scmBody() widgets.Widget {
	win := s.win
	body := scmview.NewBody(s.scm.view, scmview.BodyOpts{Welcome: func(bc widgets.BuildContext) widgets.Widget { return s.scmWelcome(win) }})
	return kit.NewContextMenu(body, kit.ContextMenuOpts{
		Controller: s.scm.menu, OnRun: s.menus.Run,
		OnHide: func() { s.afterFrame(s.binding, func() { s.scm.view.Focus() }) },
	})
}

// scmWelcomeDefault is the content of the Changes view when nothing else applies (VS Code's content with the condition
// "default").
const scmWelcomeDefault = "No source control providers registered."

// scmWelcome is the welcome content of the Changes view: the pieces extensions contribute for the view that hold now, else the
// default text.
func (s *shellState) scmWelcome(win *Workspace) widgets.Widget {
	ctx := menuContext{base: win.Keys, settings: win.Settings}
	var pieces []welcome.Piece
	if scope := win.Extensions(); scope != nil {
		for _, c := range scope.ViewWelcome(command.ViewSCM, ctx) {
			pieces = append(pieces, welcome.Piece{Contents: c.Contents, Disabled: !c.Enabled(ctx)})
		}
	}
	if len(pieces) == 0 {
		pieces = []welcome.Piece{{Contents: scmWelcomeDefault}}
	}
	return welcome.New(welcome.Opts{Pieces: pieces, OnLink: s.welcomeLink})
}

// welcomeLink follows a link or a button of welcome content: a command link runs the command (with the arguments in its query as
// JSON), a web address opens in the browser of the desktop.
func (s *shellState) welcomeLink(target string) {
	win := s.win
	if id, query, ok := welcome.CommandOf(target); ok {
		args, err := linkArguments(query)
		if err != nil {
			win.Process.Logger.Warn("the arguments of a command link are not JSON", "command", id, "err", err)
			return
		}
		s.scmRun(id, args)
		return
	}
	if u, err := url.Parse(target); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		go func() {
			if err := win.openURI(win.ctx, target); err != nil {
				win.Process.Logger.Warn("a link of the welcome content cannot be opened", "err", err)
			}
		}()
	}
}

// linkArguments reads the arguments in the query of a command link: JSON, or none. The tag is ScmWelcomeLinkInvalid.
func linkArguments(query string) (any, error) {
	if query == "" {
		return nil, nil
	}
	var args any
	if err := json.Unmarshal([]byte(query), &args); err != nil {
		return nil, errors.NewErr("ScmWelcomeLinkInvalid", err)
	}
	return args, nil
}

// scmRepositoriesBody is the body of the Repositories view.
func (s *shellState) scmRepositoriesBody() widgets.Widget {
	return kit.NewContextMenu(scmview.NewRepositoriesBody(s.scm.repos), kit.ContextMenuOpts{Controller: s.scm.menu, OnRun: s.menus.Run})
}

// syncSCMParts applies a change of the model to the parts of the window that show it: the badge of the activity bar icon, the status
// bar items of the active repository and the context keys of the title.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/activity.ts (SCMActiveRepositoryController)
func (s *shellState) syncSCMParts() {
	win := s.win
	m := win.SCM
	if m == nil || s.scm.view == nil {
		return
	}
	n := m.ActivityBadge()
	win.Views.SetBadge(command.ViewContainerSCM, workbench.ActivityBadge{Count: n, Tooltip: scmcore.PendingChangesLabel(n)})
	collapsible, all := s.scm.view.RepositoryKeys()
	win.Keys.Batch(func() {
		win.Keys.Set("scmViewIsAnyRepositoryCollapsible", collapsible)
		win.Keys.Set("scmViewAreAllRepositoriesCollapsed", all)
	})
	s.syncSCMStatus()
}

// scmStatusPriority is the priority of the first status bar item of the active repository (VS Code: 10000, at the left end).
const scmStatusPriority = 10000

// syncSCMStatus shows the status bar commands of the active repository (the branch and the sync item of Git), and, when there are
// several repositories, an item with the name of the active one that opens the picker.
func (s *shellState) syncSCMStatus() {
	win := s.win
	st := &s.scm
	for _, it := range st.status {
		it.Dispose()
	}
	st.status = nil
	active, pinned := win.SCM.Active()
	if active == nil {
		return
	}
	label := active.Label()
	if active.RootURI() != "" {
		label = path.Base(active.RootPath()) + " (" + active.Label() + ")"
	}
	for i, c := range active.StatusBarCommands() {
		tip := label
		if c.Tooltip != "" {
			tip += " - " + c.Tooltip
		}
		entry := workbench.StatusEntry{Name: "Source Control", Text: c.Text, AriaLabel: tip, Tooltip: tip, Command: c.Command, Args: c.Args}
		st.status = append(st.status, win.StatusBar.Add("status.scm."+string(rune('0'+i)), workbench.StatusLeft, scmStatusPriority-float64(i)*0.001, entry))
	}
	if len(win.SCM.Sources()) > 1 {
		icon := "repo"
		if pinned {
			icon = "repo-pinned"
		}
		entry := workbench.StatusEntry{Name: "Source Control Provider", Text: "$(" + icon + ") " + active.ActiveRepositoryName(),
			AriaLabel: label, Tooltip: label, Command: "scm.setActiveProvider"}
		st.status = append(st.status, win.StatusBar.Add("status.scm.provider", workbench.StatusLeft, scmStatusPriority-0.0005, entry))
	}
}

// syncSCM follows the file of the active editor: the model learns it (the active repository follows it), and the view selects its
// row when scm.autoReveal is on and the view is on screen. It is called while the shell builds.
func (s *shellState) syncSCM(binding *widgets.WidgetsBinding, win *Workspace) {
	st := &s.scm
	if st.view == nil {
		return
	}
	p := s.activePath()
	if p == st.revealedFor {
		return
	}
	st.revealedFor = p
	uri := ""
	if p != "" {
		uri = "file://" + p
	}
	binding.Post(func() {
		if !s.Mounted() || s.scm.view == nil {
			return
		}
		win.SCM.SetActiveResource(uri)
		if uri != "" && scmFlag(win, "scm.autoReveal")() && win.Layout.State().SideBarVisible && win.Views.Active() == command.ViewContainerSCM {
			s.scm.view.Reveal(uri)
		}
	})
}

// showSCM shows the container of Source Control in the side bar, and gives the keyboard focus to a view of it when focus is set.
func (s *shellState) showSCM(binding *widgets.WidgetsBinding, view string, focus bool) {
	win := s.win
	win.Views.Open(command.ViewContainerSCM)
	win.Views.SetHidden(view, false)
	win.Views.SetCollapsed(view, false)
	if focus {
		s.afterFrame(binding, func() {
			switch {
			case view == command.ViewSCMRepositories && s.scm.repos != nil:
				s.scm.repos.Focus()
			case s.scm.view != nil:
				s.scm.view.Focus()
			}
		})
	}
}

// bindSCMCommands takes over the commands of the Source Control view in the registry while the shell is on screen.
func (s *shellState) bindSCMCommands(binding *widgets.WidgetsBinding) {
	win := s.win
	st := &s.scm
	run := func(f func()) command.Handler { return func(command.Context, any) error { f(); return nil } }
	groupOf := func(args any) *scmcore.Group {
		for _, a := range scmArgsOf(args) {
			if src := win.SCM.Source(a.sourceControl); src != nil && a.group != "" {
				return src.Group(a.group)
			}
		}
		return nil
	}
	handlers := map[string]command.Handler{
		"workbench.view.scm":                           run(func() { s.showSCM(binding, command.ViewSCM, true) }),
		"workbench.scm.focus":                          run(func() { s.showSCM(binding, command.ViewSCM, true) }),
		"workbench.scm.repositories.focus":             run(func() { s.showSCM(binding, command.ViewSCMRepositories, true) }),
		"workbench.scm.action.collapseAllRepositories": run(func() { st.view.CollapseAllRepositories() }),
		"workbench.scm.action.expandAllRepositories":   run(func() { st.view.ExpandAllRepositories() }),
		"workbench.scm.action.collapseAll": func(_ command.Context, args any) error {
			if g := groupOf(args); g != nil {
				st.view.CollapseAll(g)
			}
			return nil
		},
		"workbench.scm.action.toggleRepositoryVisibility": func(_ command.Context, args any) error {
			for _, a := range scmArgsOf(args) {
				if src := win.SCM.Source(a.sourceControl); src != nil {
					win.SCM.ToggleVisibility(src, nil)
				}
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

// scmArg is an object of the arguments that a menu of the view gives a command (extension.SCMArgs).
type scmArg struct{ sourceControl, group string }

// scmArgsOf reads the source control and the group of each object of the arguments of a command.
func scmArgsOf(args any) []scmArg {
	var list []any
	switch v := args.(type) {
	case []any:
		list = v
	case map[string]any:
		list = []any{v}
	}
	var out []scmArg
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			var a scmArg
			a.sourceControl, _ = m["sourceControl"].(string)
			a.group, _ = m["group"].(string)
			out = append(out, a)
		}
	}
	return out
}

// scmViewFocused follows the focus into the views of the container.
func (s *shellState) scmViewFocused(viewID string, has bool) {
	win := s.win
	st := &s.scm
	if st.focus == nil {
		return
	}
	st.focus[viewID] = has
	if has {
		win.Keys.Set("focusedView", viewID)
	} else if win.Keys.Get("focusedView") == viewID {
		win.Keys.Set("focusedView", "")
	}
}

var _ = painting.EdgeInsets{}
