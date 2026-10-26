package app

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The window tabs of the extensions (SPEC §10.14, §10.15 "As built (step 11.7)").
//
// A tab of an extension is a WindowTab of the kind TabExtension. Its Workspace is the host of the services of the tab (the
// commands and the keys, the notifications, the quick input, the dialogs, the settings, the themes): a workspace with no
// folder, no documents and no UI state file of its own, whose scope in the extension host is the scope of the kind
// (extension.ScopeWindowTab) and holds the extension that owns the kind only. Its page is the shell in "extension mode":
// the workbench with the editor area alone, which holds the box of the extension, under the title bar with the strip.
// What the tab keeps (its kind, title and the state its content saves) is in the list of the open tabs of the global
// state (storage.WorkspaceTab).

// ExtensionTabSpec says how a tab of an extension opens (WorkspaceSpec.Extension).
type ExtensionTabSpec struct {
	// Kind is the id of the kind of the tab.
	Kind string
	// Title is the title the tab had, for a tab that comes back; empty is the title of the kind until the extension sets one.
	Title string
	// State is the state the content saved, for a tab that comes back.
	State json.RawMessage
}

// extTab is what a Workspace that hosts the tab of an extension knows of it. It is used on the UI thread.
type extTab struct {
	kind  exthost.WindowTabKindInfo
	tabID string
	// title and hint are what the extension set (empty: the title of the kind, and the title).
	title, hint string
	// state is the state the tab was opened with, and the state its content saved when its scope closed.
	state json.RawMessage
	// saveStop cancels the write of the state that waits (StateChanged).
	saveStop func()
}

// displayTitle is the name on the tab of the strip.
func (e *extTab) displayTitle() string {
	if e.title != "" {
		return e.title
	}
	return e.kind.Spec.Title
}

// tooltip is the text of the hover of the tab.
func (e *extTab) tooltip() string {
	if e.hint != "" {
		return e.hint
	}
	return e.displayTitle()
}

// windowTitle is the title of the window while the tab is the active one.
func (e *extTab) windowTitle() string { return e.displayTitle() + " - " + Title }

// iconCode is the code point of the icon of the tab.
func (e *extTab) iconCode() int {
	if code, ok := fonts.CodiconByName(e.kind.Spec.Icon); ok {
		return code
	}
	return 0
}

// sharingID names the tab in the links between window tabs: "<extension id>:<tab id>", the same after a restart
// (SPEC §10.15 "Event sharing").
func (e *extTab) sharingID() string { return e.kind.Extension + ":" + e.tabID }

// currentExtState is the state of the content of the tab of an extension now (asked of the extension while it runs).
func (w *Workspace) currentExtState() json.RawMessage {
	if scope := w.Extensions(); scope != nil {
		return scope.WindowTabState()
	}
	return w.ext.state
}

// ---- the scope of the tab ----

// startExtensionTab makes the scope of the tab of an extension in the host and starts it: the extension that owns the
// kind is activated and makes the content, with the state the tab saved. It is the last step of making the workspace.
func (w *Workspace) startExtensionTab() {
	p := w.Process
	e := w.ext
	if p.Extensions == nil {
		return
	}
	w.extKeys = &extensionKeys{w: w}
	scope := p.Extensions.NewScope(exthost.ScopeOptions{
		Info: extension.ScopeInfo{Kind: extension.ScopeWindowTab, Name: e.kind.Spec.Title, WindowTab: e.kind.Spec.ID, ID: e.sharingID()},
		Ctx:  w.ctx, Commands: w.Commands, Menus: w.Menus, Keybindings: w.extKeys, Settings: w.Settings, Notify: w.Notify, Keys: w.Keys,
		Disabled: w.disabledExtensions, Post: w.onUI, Frame: w.extensionFrame, After: w.extensionAfter,
		Logger: p.Logger, AskSharing: w.askSharing, OpenURI: w.openURI,
		Only: e.kind.Extension, WindowTab: extTabHost{w: w}, EditorsChanged: w.extTabRefresh,
	})
	scope.SetWindowTabState(e.state)
	w.extScope.Store(scope)
	// a change of extensions.disabled turns the owner off and on in this tab
	w.stops = append(w.stops, w.Settings.OnChange(func(ev settings.ChangeEvent) {
		if ev.Affects(disabledExtensionsSetting, "") {
			w.onUI(scope.Refresh)
		}
	}))
	// the event of the window tab: it comes when the tab becomes the active one
	if m := p.Workspaces; m != nil {
		was := false
		w.stops = append(w.stops, m.OnChange(func() {
			now := w.managed() && w.activeTab()
			if now && !was {
				if err := scope.PublishCore(extension.TopicWindowTabActivated, extension.WorkspaceInfo(scope.Info())); err != nil {
					p.Logger.Error("a core event cannot be published", "topic", extension.TopicWindowTabActivated, "err", err)
				}
			}
			was = now
		}))
	}
	scope.Start()
}

// extTabHost is what the extension of a tab can do to the tab of the strip.
type extTabHost struct{ w *Workspace }

var _ exthost.WindowTabHost = extTabHost{}

// SetTitle implements exthost.WindowTabHost.
func (h extTabHost) SetTitle(title string) {
	h.w.ext.title = title
	h.w.extTabChanged()
}

// SetHint implements exthost.WindowTabHost.
func (h extTabHost) SetHint(hint string) {
	h.w.ext.hint = hint
	h.w.extTabChanged()
}

// StateChanged implements exthost.WindowTabHost: the state is written once the snapshot delay has passed.
func (h extTabHost) StateChanged() {
	w := h.w
	e := w.ext
	m := w.Process.Workspaces
	if e.saveStop != nil || m.tabOf(w) == nil {
		return
	}
	save := func() {
		e.saveStop = nil
		m.syncTabs()
	}
	if m.binding == nil {
		save()
		return
	}
	e.saveStop = m.binding.Gestures().NewTimer(stateSnapshotDelay, save).Cancel
}

// Close implements exthost.WindowTabHost: the tab closes without a question.
func (h extTabHost) Close() {
	m := h.w.Process.Workspaces
	if t := m.tabOf(h.w); t != nil {
		m.Close(t, nil)
	}
}

// extTabChanged tells the window that the strip's view of the tab changed (its title or hint): the list of the tabs is
// written, the window title follows when the tab is the active one, and the strip and the tray are made again.
func (w *Workspace) extTabChanged() {
	m := w.Process.Workspaces
	t := m.tabOf(w)
	if t == nil { // the extension sets the title in Create, before the tab is in the strip
		return
	}
	m.syncTabs()
	if m.ActiveTab() == t {
		m.pushTitle(t)
	}
	m.changed()
}

// extTabRefresh makes the page of the tab build again: the box of the extension, a notice or the failure takes its place.
// The state the content saved when its extension ended is written to the list of the tabs.
func (w *Workspace) extTabRefresh() {
	w.onUI(func() {
		if m := w.Process.Workspaces; m.tabOf(w) != nil {
			m.syncTabs()
		}
		if s := w.shell; s != nil && s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// ---- opening and restoring ----

// prepareExtension makes the workspace of the tab of an extension for a spec; nothing is on screen yet. The failure has the
// tag WindowTabKindUnknown for a kind no extension of this Emetos has; the error of NewWorkspace is returned as it is.
func (m *Workspaces) prepareExtension(ctx context.Context, spec WorkspaceSpec, from *Workspace) (*preparedWorkspace, error) {
	es := spec.Extension
	var kind exthost.WindowTabKindInfo
	found := false
	if host := m.p.Extensions; host != nil {
		kind, found = host.WindowTabKind(es.Kind)
	}
	if !found {
		return nil, errors.New("WindowTabKindUnknown", "no extension has a window tab of this kind", "kind", es.Kind)
	}
	id := spec.TabID
	if id == "" {
		id = m.newTabID()
	}
	e := &extTab{kind: kind, tabID: id, title: es.Title, state: es.State}
	ws, err := newWorkspace(ctx, m.p, workspaceOptions{ext: e, from: from})
	if err != nil {
		return nil, err
	}
	return &preparedWorkspace{ws: ws, tabID: id, steps: spec.Steps, ext: e}, nil
}

// OpenExtensionTab opens a tab of the kind (the id of a kind of an extension) after the active tab and makes it the
// active one. state is what the content is created with; nil is a new tab. It must run on the UI thread. The failure has
// the tag WindowTabKindUnknown, or WindowTabKindDisabled when the extension that owns the kind is turned off.
func (m *Workspaces) OpenExtensionTab(kindID string, state json.RawMessage) (*WindowTab, error) {
	from := m.Active()
	if info, ok := m.kindInfo(kindID); ok && from != nil && slices.Contains(from.disabledExtensions(), info.Extension) {
		return nil, errors.New("WindowTabKindDisabled", "the extension that owns this window tab is turned off", "kind", kindID, "extension", info.Extension)
	}
	pw, err := m.prepareExtension(m.ctxOr(), WorkspaceSpec{Extension: &ExtensionTabSpec{Kind: kindID, State: state}}, from)
	if err != nil {
		return nil, err
	}
	at := len(m.Tabs())
	if t := m.ActiveTab(); t != nil {
		at = m.IndexOf(t) + 1
	}
	t := m.addTab(pw, at)
	if c, ok := m.implicitController(); ok {
		pw.ws.controller = c
	}
	m.Activate(t)
	return t, nil
}

// kindInfo finds a kind of window tab by its id.
func (m *Workspaces) kindInfo(kindID string) (exthost.WindowTabKindInfo, bool) {
	if m.p.Extensions == nil {
		return exthost.WindowTabKindInfo{}, false
	}
	return m.p.Extensions.WindowTabKind(kindID)
}

// openWindowTabCommandID is the command that opens a tab of an extension: with {"kind": id} the tab of the kind, without
// arguments a picker of the kinds.
const openWindowTabCommandID = "emetos.windowTab.open"

// kindOfArgs reads the kind from the arguments of the commands that open a tab of an extension: {"kind": id}, or the id as
// a string; empty for none.
func kindOfArgs(args any) string {
	switch v := args.(type) {
	case string:
		return v
	case map[string]any:
		kind, _ := v["kind"].(string)
		return kind
	}
	return ""
}

// extensionTabItems are the entries of the "+" menu for the kinds of window tab of the extensions that are turned on, in
// the order of the extensions. Choosing one runs emetos.windowTab.open with its kind.
func (w *Workspace) extensionTabItems() []kit.MenuItem {
	if w.Process.Extensions == nil {
		return nil
	}
	var items []kit.MenuItem
	for _, k := range w.Process.Extensions.WindowTabKinds(w.disabledExtensions()) {
		items = append(items, kit.MenuItem{ID: openWindowTabCommandID, Label: k.Label(), Data: map[string]any{"kind": k.Spec.ID}})
	}
	return items
}

// registerExtensionTabCommands binds the commands that open the tab of an extension in the registry of the workspace:
// emetos.windowTab.open and, in debug builds, dev.openWindowTab.
func (w *Workspace) registerExtensionTabCommands(set func(id string, h command.Handler)) {
	m := w.Process.Workspaces
	set(openWindowTabCommandID, func(_ command.Context, args any) error {
		if kind := kindOfArgs(args); kind != "" {
			if _, err := m.OpenExtensionTab(kind, nil); err != nil {
				w.Process.Logger.Warn("cannot open the window tab", "kind", kind, "err", err)
				w.Notify.Error("Emetos", "Cannot open the window tab: "+describe(err))
			}
			return nil
		}
		w.pickWindowTabKind()
		return nil
	})
	if DevCommandsEnabled() {
		set("dev.openWindowTab", func(_ command.Context, args any) error {
			kind := kindOfArgs(args)
			if kind == "" {
				return errors.New("DevOpenWindowTabInvalid", `dev.openWindowTab needs the id of a kind of window tab or {"kind": ...}`, "arguments", args)
			}
			_, err := m.OpenExtensionTab(kind, nil)
			return err
		})
	}
}

// pickWindowTabKind shows the picker of the kinds of window tab; with none it says so.
func (w *Workspace) pickWindowTabKind() {
	m := w.Process.Workspaces
	kinds := []exthost.WindowTabKindInfo(nil)
	if w.Process.Extensions != nil {
		kinds = w.Process.Extensions.WindowTabKinds(w.disabledExtensions())
	}
	if len(kinds) == 0 {
		w.Notify.Info("Emetos", "No extension that is turned on adds a kind of window tab.")
		return
	}
	items := make([]quickinput.Item, len(kinds))
	for i, k := range kinds {
		kind := k.Spec.ID
		item := quickinput.Item{ID: kind, Label: k.Spec.Title, Description: k.ExtensionName, Icon: fonts.CodiconWindow}
		if code, ok := fonts.CodiconByName(k.Spec.Icon); ok {
			item.Icon = code
		}
		item.Accept = func(quickinput.KeyMods) {
			if _, err := m.OpenExtensionTab(kind, nil); err != nil {
				w.Process.Logger.Warn("cannot open the window tab", "kind", kind, "err", err)
				w.Notify.Error("Emetos", "Cannot open the window tab: "+describe(err))
			}
		}
		items[i] = item
	}
	w.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select the kind of window tab to open", Items: items})
}

// ---- the page of the tab ----

// initExtTab starts the shell in extension mode: the menus and the strip, the quick input with the palette, the follow of the
// modes, and the dev commands of the page. It has no editor groups, panels, Explorer, status bar, backups or state of its
// own.
func (s *shellState) initExtTab(binding *widgets.WidgetsBinding) {
	win := s.win
	win.after = func(d time.Duration, f func()) func() { return binding.Gestures().NewTimer(d, f).Cancel }
	s.initMenus(binding)
	s.initExtQuickInput(binding)
	s.unbindTitleBar = s.bindTitleBar(binding)
	s.initExtClosing(binding)
	s.modesStops = append(s.modesStops, win.OnModesChange(func() {
		binding.Post(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		})
	}))
	s.startDevSteps(binding)
}

// initExtQuickInput registers the providers of the quick input that make sense without a folder or an editor: the help and
// the command palette; the commands that open it are bound as the shell of a workspace binds them.
func (s *shellState) initExtQuickInput(binding *widgets.WidgetsBinding) {
	win := s.win
	s.quickWin = win
	ctl := win.QuickInput
	ctl.Configure(quickinput.ControllerOptions{
		Post: binding.Post,
		After: func(f func()) {
			scheduler := binding.Scheduler()
			scheduler.AddPostFrameCallback(func(time.Duration) {
				scheduler.ScheduleFrame()
				scheduler.AddPostFrameCallback(func(time.Duration) {
					if s.Mounted() {
						f()
					}
				})
			})
			scheduler.ScheduleFrame()
		},
		Invoker:             func() contextkey.Context { return s.editorMenuContext() },
		FallbackPlaceholder: quickInputPlaceholder,
	})
	keyLabel := func(id string, ctx contextkey.Context) string {
		if kb, ok := win.Keybindings.LookupPrimaryKeybinding(id, ctx, false); ok && kb.Keybinding != nil {
			return kb.Keybinding.Label()
		}
		return ""
	}
	reg := win.QuickAccess
	providers := []quickinput.Provider{
		quickinput.NewHelpProvider(reg, func(id string) string { return keyLabel(id, ctl.InvokerContext()) }),
		quickinput.NewCommandsProvider(quickinput.CommandsOptions{
			Commands: win.Commands, Menus: win.Menus, Context: ctl.InvokerContext, Keybinding: keyLabel,
			History:      win.CommandHistory,
			HistoryLimit: func() int { return settings.Get[int](win.Settings, "workbench.commandPalette.history", "") },
			PreserveInput: func() bool {
				return settings.Get[bool](win.Settings, "workbench.commandPalette.preserveInput", "")
			},
			Run: func(id string) error {
				return win.Commands.Run(command.Context{Ctx: win.ctx, Keys: ctl.InvokerContext(), Source: command.SourcePalette}, id, nil)
			},
			OnError: func(id string, err error) {
				title := id
				if m, ok := win.Commands.Lookup(id); ok && m.Title != "" {
					title = m.Title
				}
				win.Process.Logger.Warn("command run by the palette failed", "command", id, "error", describe(err))
				win.Notify.Error("Emetos", "Command '"+title+"' resulted in an error: "+describe(err))
			},
		}),
	}
	for _, p := range providers {
		if err := reg.Register(p); err != nil {
			win.Process.Logger.Error("cannot register a quick access provider", "err", err)
			continue
		}
		s.quickPrefixes = append(s.quickPrefixes, p.Info().Prefix)
	}
	s.bindQuickInputCommands()
}

// initExtClosing binds the commands that close the tab and quit, which the keys reach from the tab of an extension as from
// any tab. The tab has nothing to ask when it closes (win.closer stays nil: its data is its state).
func (s *shellState) initExtClosing(binding *widgets.WidgetsBinding) {
	win := s.win
	win.exit = func() { binding.ExitApplication(ui.AppExitTypeRequired) }
	win.Process.Workspaces.bind(binding)
	for id, reason := range map[string]CloseReason{"workbench.action.closeWindow": CloseReasonTab, "workbench.action.quit": CloseReasonQuit} {
		remove, err := win.Commands.SetHandler(id, func(command.Context, any) error {
			win.RequestClose(reason)
			return nil
		})
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		s.closing.removeCommands = append(s.closing.removeCommands, remove)
	}
	remove, err := win.Commands.SetHandler(showLogCommand, func(command.Context, any) error { return win.showLogFromExtTab() })
	if err != nil {
		panic(err)
	}
	s.closing.removeCommands = append(s.closing.removeCommands, remove)
}

// showLogFromExtTab is what "Show Log" does in the tab of an extension, which has no Output panel: a workspace tab (the
// last one used, or a new one) comes to the front and shows the panel.
func (w *Workspace) showLogFromExtTab() error {
	m := w.Process.Workspaces
	target := m.launchTarget()
	if target == nil {
		var err error
		if target, err = m.openFrom(WorkspaceSpec{}, w); err != nil {
			return err
		}
	}
	target.Raise()
	show := func() {
		if err := target.runCommand(showLogCommand, nil); err != nil {
			w.Process.Logger.Warn("the log cannot be shown", "err", err)
		}
	}
	// the workbench of a tab that was just made binds its commands with its first frame: the command runs after it
	target.whenReady(func() {
		if m.binding == nil {
			show()
			return
		}
		m.binding.RunPostFrame(func(time.Duration) { show() })
		m.binding.Scheduler().ScheduleFrame()
	})
	return nil
}

// showLogCommand is the command the "Show Log" button of a failure runs: it shows the Output panel.
const showLogCommand = "workbench.panel.output.focus"

// disposeExtTab gives back what initExtTab took.
func (s *shellState) disposeExtTab() {
	if s.unbindTitleBar != nil {
		s.unbindTitleBar()
	}
	s.disposeClosing()
	s.disposeModes()
	s.disposeQuickInput()
	if s.win != nil {
		s.win.after = nil
	}
}

// buildExtTab is the page of the tab of an extension: the workbench with the editor area alone, holding the box of the
// extension, under the title bar with the strip, the menu and the buttons of the window. The quick input, the
// notifications and the dialogs of the tab show over it.
func (s *shellState) buildExtTab(bc widgets.BuildContext, win *Workspace) widgets.Widget {
	s.syncExtTitle(bc, win)
	var body widgets.Widget
	if scope := win.Extensions(); scope != nil {
		body = scope.WindowTabBody()
	} else {
		body = widgets.NewSizedBox(nil)
	}
	return workbench.New(workbench.Options{
		Layout: win.Layout, Commands: win.Commands, Keys: win.Keys, Scopes: win.Scopes,
		Settings: win.Settings, Dialogs: win.Dialogs, Notifications: win.Notifications, QuickInput: win.QuickInput, Context: win.ctx,
		Editor: body, FullScreen: win.IsFullScreen(), NoFolder: true, EditorOnly: true,
		Menus: s.menus.BarMenus(), OnRunMenu: s.menus.Run, MenuBar: s.menuBar, TitleBar: s.titleBarOptions(win),
	})
}

// syncExtTitle gives the window the title of the tab when it is the active one and the title changed.
func (s *shellState) syncExtTitle(bc widgets.BuildContext, win *Workspace) {
	defer s.bindController(bc, win)()
	title := win.ext.windowTitle()
	if title == win.title {
		return
	}
	win.title = title
	if s.window != nil && win.activeTab() {
		controller := s.window
		widgets.WidgetsBindingOf(bc).Post(func() {
			if win.activeTab() {
				controller.SetTitle(title)
			}
		})
	}
}
