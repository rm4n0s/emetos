package exthost

import (
	"encoding/json"
	"slices"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The views of the extensions (SPEC §10.15 "As built (step 11.5)"). The manifest declares the containers and the views;
// the scope registers them in the view registry of its window tab when the extension is enabled in it, so the activity bar
// icon and the panel tab are there before the extension is activated. The first time a view is shown its extension is
// activated (onView), provides the content, and the box of the view builds it.

// extensionOrder is the order of the containers and tabs that an extension left at 0: after everything Emetos has.
const extensionOrder = 1000

// viewDecl is a view the manifest of an extension declares, in one scope.
type viewDecl struct {
	id   string
	slot *slot
	// panel says that the view is the tab of a panel container with the id of the view; otherwise container is the
	// container of the side bar it is in.
	panel     bool
	container string
	name      string
	// title is the title the extension set with ViewHandle.SetTitle; empty is the name. It is guarded by the mutex of the scope.
	title string
	// actions are the title actions, with their `when` parsed.
	actions []viewAction

	// content is what the extension provided, inst the activation that provided it, and badge the count on the tab of a
	// panel view. They are guarded by the mutex of the scope.
	content  *extension.ViewContent
	inst     *instance
	badge    int
	badgeTip string
	// boxes are the boxes of the view that are on screen.
	boxes []*viewBoxState
	// tab is the editor tab the declaration stands for, and event the activation event showing its box fires; both are
	// empty for a view of the side bar or the panel (the event is then onView of the id).
	tab   *editorTab
	event string
	// provided says that the extension gave the view its content and restoreView gave it its state: the state is then
	// saved with the window's even while the view is off screen.
	provided bool
}

// viewAction is a title action of a view.
type viewAction struct {
	spec extension.ViewActionSpec
	code int
	when contextkey.Expr
}

// iconCode is the code point of the Codicon with the name; 0 for an unknown name or none.
func iconCode(name string) int {
	code, _ := fonts.CodiconByName(name)
	return code
}

// orderOf is the order of a container or a tab of an extension.
func orderOf(order int) int {
	if order == 0 {
		return extensionOrder
	}
	return order
}

// installViews registers the containers, the views and the tabs that the manifest of an extension declares, and adds the
// commands that show them and their entries of the View menu. It returns the functions that take the registry entries
// away (they stay while the extension is failed, so that its boxes can say so) and those that take the commands and menu
// items away (they go with a failure, as every declared command does). A contribution that cannot be added is logged and
// left out.
func (s *Scope) installViews(sl *slot) (registry, others []func()) {
	views := s.opts.Views
	if views == nil {
		return nil, nil
	}
	reg := views.Registry()
	m := sl.info.Manifest
	c := m.Contributes
	log := s.log.With("extension", m.ID)
	declare := func(d *viewDecl) {
		s.mu.Lock()
		if s.viewDecls == nil {
			s.viewDecls = map[string]*viewDecl{}
		}
		s.viewDecls[d.id] = d
		s.mu.Unlock()
		registry = append(registry, func() {
			s.mu.Lock()
			delete(s.viewDecls, d.id)
			s.mu.Unlock()
		})
	}
	for _, spec := range c.ViewContainers {
		container := command.ViewContainer{ID: spec.ID, Title: spec.Title, Icon: iconCode(spec.Icon), Order: orderOf(spec.Order), Location: command.ViewLocationSideBar}
		if err := reg.RegisterContainer(container); err != nil {
			log.Warn("a view container of an extension is left out", "container", spec.ID, "err", err)
			continue
		}
		id := spec.ID
		registry = append(registry, func() { reg.UnregisterContainer(id) })
		show := extension.ShowViewCommand(id)
		remove, err := s.addViewCommand(command.Meta{ID: show, Title: "Show " + spec.Title, Category: "View"}, func(command.Context, any) error {
			views.Open(id)
			return nil
		})
		if err != nil {
			log.Warn("the command of a view container is left out", "container", spec.ID, "err", err)
			continue
		}
		others = append(others, remove)
	}
	for _, spec := range c.Views {
		when, err := parseWhen(spec.When)
		if err != nil {
			log.Warn("a view of an extension is left out", "view", spec.ID, "err", err)
			continue
		}
		view := command.View{ID: spec.ID, Name: spec.Name, Container: spec.Container, When: when, Order: spec.Order, Collapsed: spec.Collapsed}
		if err := reg.RegisterViews(view); err != nil {
			log.Warn("a view of an extension is left out", "view", spec.ID, "err", err)
			continue
		}
		id := spec.ID
		registry = append(registry, func() { reg.UnregisterView(id) })
		declare(&viewDecl{id: spec.ID, slot: sl, container: spec.Container, name: spec.Name})
	}
	for _, spec := range c.PanelViews {
		container := command.ViewContainer{ID: spec.ID, Title: spec.Title, Icon: iconCode(spec.Icon), Order: orderOf(spec.Order), Location: command.ViewLocationPanel}
		if err := reg.RegisterContainer(container); err != nil {
			log.Warn("a panel view of an extension is left out", "view", spec.ID, "err", err)
			continue
		}
		id := spec.ID
		registry = append(registry, func() { reg.UnregisterContainer(id) })
		if err := reg.RegisterViews(command.View{ID: id, Name: spec.Title, Container: id}); err != nil {
			log.Warn("a panel view of an extension is left out", "view", spec.ID, "err", err)
			continue
		}
		declare(&viewDecl{id: id, slot: sl, panel: true, container: id, name: spec.Title})
		toggle := extension.ToggleViewCommand(id)
		remove, err := s.addViewCommand(command.Meta{ID: toggle, Title: "Toggle " + spec.Title, Category: "View"}, func(command.Context, any) error {
			views.TogglePanel(id)
			return nil
		})
		if err != nil {
			log.Warn("the command of a panel view is left out", "view", spec.ID, "err", err)
			continue
		}
		others = append(others, remove)
		removeMenu, err := s.opts.Menus.AddRemovable(command.MenuItem{Menu: command.MenubarViewMenu, Command: toggle, Title: spec.Title, Group: "5_panel", Order: 10})
		if err != nil {
			log.Warn("the View menu entry of a panel view is left out", "view", spec.ID, "err", err)
			continue
		}
		others = append(others, removeMenu)
	}
	for _, spec := range c.ViewActions {
		when, err := parseWhen(spec.When)
		if err != nil {
			log.Warn("a title action of an extension is left out", "view", spec.View, "command", spec.Command, "err", err)
			continue
		}
		s.mu.Lock()
		if d := s.viewDecls[spec.View]; d != nil {
			d.actions = append(d.actions, viewAction{spec: spec, code: iconCode(spec.Icon), when: when})
			slices.SortStableFunc(d.actions, func(a, b viewAction) int {
				switch {
				case a.spec.Order < b.spec.Order:
					return -1
				case a.spec.Order > b.spec.Order:
					return 1
				}
				return 0
			})
		}
		s.mu.Unlock()
	}
	return registry, others
}

// addViewCommand registers a command of a view of an extension and returns the function that takes it away.
func (s *Scope) addViewCommand(meta command.Meta, h command.Handler) (remove func(), err error) {
	reg := s.opts.Commands
	if err := reg.Register(meta); err != nil {
		return nil, err
	}
	removeHandler, err := reg.SetHandler(meta.ID, h)
	if err != nil {
		reg.Unregister(meta.ID)
		return nil, err
	}
	return func() { removeHandler(); reg.Unregister(meta.ID) }, nil
}

// removeViews takes the containers and views of an extension out of the registry; the views of a failed extension stay
// until this is called (the extension is turned off, or the scope closes). The content a view still has is saved first, so
// that a view that comes back finds its state.
func (s *Scope) removeViews(sl *slot) {
	s.mu.Lock()
	removers := sl.viewRemovers
	sl.viewRemovers = nil
	s.mu.Unlock()
	for i := len(removers) - 1; i >= 0; i-- {
		removers[i]()
	}
}

// decl returns the declaration of a view of any extension of the scope.
func (s *Scope) decl(viewID string) *viewDecl {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewDecls[viewID]
}

// IsExtensionView tells whether the view or panel container with the id belongs to an extension of the scope.
func (s *Scope) IsExtensionView(viewID string) bool { return s.decl(viewID) != nil }

// ViewBody is the box of a view that an extension declared: the widget the workbench puts in the body of the view. It is
// nil for a view that is not an extension's. The workbench builds it whenever the view is on screen; showing it activates
// the extension, which provides the content.
func (s *Scope) ViewBody(viewID string) widgets.Widget {
	d := s.decl(viewID)
	if d == nil {
		return nil
	}
	return newViewBox(s, d)
}

// ViewActions are the title actions of a view of an extension as icon buttons for the header of the view; keys is where
// their `when` is read and run runs the command of a button. It is nil when the view is not an extension's or has no
// action that holds. The row is a kit.TitleActions: the title strip of the panel asks how wide it is and keeps that room for it.
func (s *Scope) ViewActions(viewID string, keys contextkey.Context, run func(commandID string)) kit.TitleActions {
	d := s.decl(viewID)
	if d == nil {
		return nil
	}
	s.mu.Lock()
	actions := slices.Clone(d.actions)
	failed := d.slot.state == StateFailed
	s.mu.Unlock()
	if failed {
		return nil
	}
	var buttons []*kit.IconButton
	for _, a := range actions {
		if a.when != nil && keys != nil && !a.when.Evaluate(keys) {
			continue
		}
		tooltip := a.spec.Tooltip
		if tooltip == "" {
			if meta, ok := s.opts.Commands.Lookup(a.spec.Command); ok {
				tooltip = meta.Title
			}
		}
		cmd := a.spec.Command
		buttons = append(buttons, kit.NewIconButton(a.code, kit.IconButtonOpts{Tooltip: tooltip, OnPressed: func() { run(cmd) }}))
	}
	if len(buttons) == 0 {
		return nil
	}
	return kit.NewIconButtonRow(buttons)
}

// ViewAllowsCommand tells whether a key press bound to the command is run by the window while the keyboard focus is in the
// view: false when the extension that provided the view has an AllowCommand that says no, so that the key goes to the
// focused widget of the view (a terminal, which takes every key but those of the commands that skip the shell). It is true for
// a view that is not an extension's, one without the function, and one whose extension failed. A panic in the function fails
// the extension with ExtensionViewAllowCommandPanicked. UI thread.
func (s *Scope) ViewAllowsCommand(viewID, commandID string) (allowed bool) {
	s.mu.Lock()
	d := s.viewDecls[viewID]
	if d == nil || d.content == nil || d.content.AllowCommand == nil || d.inst == nil || d.slot.state == StateFailed {
		s.mu.Unlock()
		return true
	}
	f, inst := d.content.AllowCommand, d.inst
	s.mu.Unlock()
	if inst.isDead() {
		return true
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionViewAllowCommandPanicked", "it panicked in AllowCommand of the view %s: %v", d.id, r).SetMetadata("extension", inst.id, "view", d.id, "scope", s.opts.Info.Name)
			s.post(func() { s.fail(d.slot, failure, "view "+d.id+" AllowCommand") })
			allowed = true
		}
	}()
	return f(commandID)
}

// ViewTitle is the title the extension gave a view of the side bar with ViewHandle.SetTitle; empty when it gave none (the
// workbench then shows the name of the view).
func (s *Scope) ViewTitle(viewID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.viewDecls[viewID]; d != nil && !d.panel {
		return d.title
	}
	return ""
}

// PanelBadge is the count and the tooltip that the extension put on the tab of a panel view; 0 for none.
func (s *Scope) PanelBadge(containerID string) (count int, tooltip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.viewDecls[containerID]; d != nil && d.panel {
		return d.badge, d.badgeTip
	}
	return 0, ""
}

// ViewShown tells the scope that the box of a view is on screen: the extension that declares it is activated, on the
// UI thread's next turn, if it was not.
func (s *Scope) ViewShown(viewID string) {
	event := extension.OnView(viewID)
	if d := s.decl(viewID); d != nil && d.event != "" {
		event = d.event
	}
	s.post(func() { s.Fire(event) })
}

// SetViewStates gives the scope the state that the views saved in an earlier session (the window state keeps it); the
// views get it back when their content is provided and shown. A view whose content was provided already (an extension that
// the start activated before the window state was read) gets it at once, on the UI thread's next turn: RestoreState is
// called, as the box calls it when it is built.
func (s *Scope) SetViewStates(states map[string]json.RawMessage) {
	s.mu.Lock()
	if s.viewStates == nil {
		s.viewStates = map[string]json.RawMessage{}
	}
	var late []*viewDecl
	for id, raw := range states {
		if _, taken := s.viewStates[id]; taken {
			continue
		}
		s.viewStates[id] = raw
		if d := s.viewDecls[id]; d != nil && d.provided && d.content != nil && d.content.RestoreState != nil && len(raw) > 0 {
			late = append(late, d)
		}
	}
	s.mu.Unlock()
	if len(late) > 0 {
		s.post(func() {
			for _, d := range late {
				s.restoreView(d)
			}
		})
	}
}

// ViewStates is the state of the views to keep with the window state: the state of every view that is on screen is asked
// of its extension now, and the state the others saved when they went off screen is added. Call it on the UI thread.
func (s *Scope) ViewStates() map[string]json.RawMessage {
	s.mu.Lock()
	var shown []*viewDecl
	for _, d := range s.viewDecls {
		if d.content != nil && d.content.SaveState != nil && (len(d.boxes) > 0 || d.provided) {
			shown = append(shown, d)
		}
	}
	s.mu.Unlock()
	for _, d := range shown {
		s.saveView(d)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]json.RawMessage, len(s.viewStates))
	for id, raw := range s.viewStates {
		out[id] = raw
	}
	return out
}

// saveView asks the content of a view for its state and keeps the answer (none removes it). A failure of the call is
// logged; a panic fails the extension with ExtensionViewStatePanicked.
func (s *Scope) saveView(d *viewDecl) { s.saveViewWhile(d, false) }

// saveViewWhile is saveView; with ending it is called while the activation ends (the contributions are taken away after its
// context was cancelled), when the instance counts as dead but is still asked unless it failed: a failed extension is not
// called again.
func (s *Scope) saveViewWhile(d *viewDecl, ending bool) {
	s.mu.Lock()
	content, inst := d.content, d.inst
	failed := d.slot.state == StateFailed
	s.mu.Unlock()
	if content == nil || content.SaveState == nil || inst == nil || (!ending && inst.isDead()) || (ending && failed) {
		return
	}
	value, err := s.callSave(inst, d, content)
	if err != nil {
		s.log.Warn("a view could not save its state", "view", d.id, "err", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.viewStates == nil {
		s.viewStates = map[string]json.RawMessage{}
	}
	if value == nil {
		delete(s.viewStates, d.id)
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		s.log.Warn("the state of a view is not JSON", "view", d.id, "err", err)
		return
	}
	s.viewStates[d.id] = raw
}

// callSave calls SaveState. A panic fails the extension with ExtensionViewStatePanicked and returns no state.
func (s *Scope) callSave(inst *instance, d *viewDecl, content *extension.ViewContent) (value any, err error) {
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionViewStatePanicked", "it panicked in SaveState of the view %s: %v", d.id, r).SetMetadata("extension", inst.id, "view", d.id, "scope", s.opts.Info.Name)
			s.post(func() { s.fail(d.slot, failure, "view "+d.id+" SaveState") })
			value, err = nil, nil
		}
	}()
	return content.SaveState()
}

// restoreView gives the content of a view the state it saved (nothing when it saved none). A failure of the call is logged;
// a panic fails the extension with ExtensionViewStatePanicked.
func (s *Scope) restoreView(d *viewDecl) {
	s.mu.Lock()
	content, inst := d.content, d.inst
	raw := s.viewStates[d.id]
	s.mu.Unlock()
	if content == nil || content.RestoreState == nil || inst == nil || inst.isDead() || len(raw) == 0 {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionViewStatePanicked", "it panicked in RestoreState of the view %s: %v", d.id, r).SetMetadata("extension", inst.id, "view", d.id, "scope", s.opts.Info.Name)
			s.post(func() { s.fail(d.slot, failure, "view "+d.id+" RestoreState") })
		}
	}()
	if err := content.RestoreState(slices.Clone(raw)); err != nil {
		s.log.Warn("a view could not restore its state", "view", d.id, "err", err)
	}
}

// failureBox is what a box of a failed extension shows: the text "<Name> failed" and a Show Log button.
func (s *Scope) failureBox(bc widgets.BuildContext, text string) widgets.Widget {
	colors := kit.ThemeOf(bc)
	show := s.opts.ShowLog
	return widgets.NewCenter(widgets.NewColumn([]widgets.Widget{
		widgets.NewText(text, widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.ListErrorForeground)}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(8.0)}),
		kit.NewButton("Show Log", kit.ButtonOpts{Small: true, OnPressed: func() {
			ctx := command.Context{Ctx: s.ctx, Source: command.SourceMenu}
			if err := s.opts.Commands.Run(ctx, show, nil); err != nil {
				s.log.Warn("the log cannot be shown", "err", err)
			}
		}}),
	}, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin}))
}
