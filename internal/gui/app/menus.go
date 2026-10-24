package app

import (
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// menuContext is the context the menus are evaluated in (SPEC §10.1): the context keys of a scope, the
// settings as the keys "config.<setting>" (VS Code's configuration keys) and the keys that the window
// keeps for the editor it shows (word wrap, whether an editor is open), which are not in the scope of
// the editor's widget because the menu bar has the focus while it is open.
type menuContext struct {
	base     contextkey.Context
	settings *settings.Store
	extra    map[string]any
}

// Value implements contextkey.Context.
func (c menuContext) Value(key string) any {
	if v, ok := c.extra[key]; ok {
		return v
	}
	if v := c.base.Value(key); v != nil {
		return v
	}
	if name, ok := strings.CutPrefix(key, "config."); ok && c.settings != nil {
		if _, registered := c.settings.Registry().Lookup(name); registered {
			return settings.Get[any](c.settings, name, "")
		}
	}
	return nil
}

// editorKeys are the context keys of the editor the window shows, for the menus: an editor is open and
// can wrap its text, whether it does, and whether it is read-only.
func (s *shellState) editorKeys() map[string]any {
	active := s.activeEditor()
	if active == nil {
		return nil
	}
	return map[string]any{
		"editorIsOpen":      true,
		"canToggleWordWrap": true,
		"editorWordWrap":    active.view.Wrapping(),
		"editorReadonly":    active.doc.ReadOnly(),
	}
}

// menuContext is the context of the menu bar and the Manage menu: the keys of the window.
func (s *shellState) menuContext() contextkey.Context {
	win := PeekWindow(s.Context())
	return menuContext{base: win.Keys, settings: win.Settings, extra: s.editorKeys()}
}

// editorMenuContext is the context of the context menu of the editor: the keys of the widget that has
// the focus, which is the editor when it was clicked.
func (s *shellState) editorMenuContext() contextkey.Context {
	win := PeekWindow(s.Context())
	focus := widgets.WidgetsBindingOf(s.Context()).FocusManager().PrimaryFocus()
	return menuContext{base: win.Scopes.ScopeOf(focus, win.Keys), settings: win.Settings, extra: s.editorKeys()}
}

// editorMenuItems are the items of the context menu of the editor, asked for when it opens.
func (s *shellState) editorMenuItems() []kit.MenuItem {
	return s.menus.Items(command.EditorContext, s.editorMenuContext())
}

// initMenus makes the source of the menus of the window and the controllers that open them from
// outside.
func (s *shellState) initMenus(binding *widgets.WidgetsBinding) {
	win := PeekWindow(s.Context())
	s.menuBar = kit.NewMenuBarController()
	s.manage = workbench.NewManageController()
	s.editorMenu = kit.NewContextMenuController()
	s.explorerMenu = kit.NewContextMenuController()
	resolver := win.Keybindings
	s.menus = workbench.NewMenuSource(workbench.MenuSourceOptions{
		Menus: win.Process.Menus, Commands: win.Process.Commands, Keybindings: resolver,
		Context: s.menuContext, Ctx: win.ctx,
		// the menu has the focus until it closes, and the editor binds its commands while it has the
		// focus itself: the command runs two frames after the item was chosen, when the menu is gone
		// (a context menu gives the focus back when its widgets are disposed, in the frame after it
		// closed) and the focus is back in the editor
		Schedule: func(f func()) {
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
		OnError: func(id string, err error) {
			win.Process.Logger.Warn("command run by a menu failed", "command", id, "error", describe(err))
		},
	})
}

// bindDevOpenMenu gives dev.openMenu (SPEC §15.2) its handler.
func (s *shellState) bindDevOpenMenu() {
	if !DevCommandsEnabled() {
		return
	}
	win := PeekWindow(s.Context())
	remove, err := win.Process.Commands.SetHandler("dev.openMenu", func(_ command.Context, args any) error { return s.devOpenMenu(args) })
	if err != nil {
		win.Process.Logger.Error("cannot bind dev.openMenu", "err", err)
		return
	}
	s.removeOpenMenu = remove
}

// devOpenMenuDefault is the position of a context menu that dev.openMenu opens when it is not told
// where, in window coordinates.
var devOpenMenuDefault = ui.Offset{Dx: 400, Dy: 250}

// parseOpenMenu reads the arguments of dev.openMenu: {"id": "...", "x": n, "y": n}, or just the id as
// a string. The position counts for the context menus.
func parseOpenMenu(args any) (id string, at ui.Offset, err error) {
	at = devOpenMenuDefault
	switch v := args.(type) {
	case string:
		id = v
	case map[string]any:
		id, _ = v["id"].(string)
		if x, ok := v["x"].(float64); ok {
			at.Dx = x
		}
		if y, ok := v["y"].(float64); ok {
			at.Dy = y
		}
	}
	if id == "" {
		return "", at, errors.New("DevOpenMenuInvalid", "dev.openMenu needs an id or {\"id\": ..., \"x\": ..., \"y\": ...}")
	}
	return id, at, nil
}

// devOpenMenu opens a menu: one of the menu bar, the Manage menu, or the context menu of the editor.
func (s *shellState) devOpenMenu(args any) error {
	id, at, err := parseOpenMenu(args)
	if err != nil {
		return err
	}
	switch {
	case s.menus.BarIndex(id) >= 0:
		s.menuBar.Open(s.menus.BarIndex(id))
	case id == command.GlobalActivity:
		s.manage.Open()
	case id == command.EditorContext:
		s.editorMenu.Show(at, s.editorMenuItems())
	default:
		return errors.New("DevOpenMenuUnknown", "no menu of the menu bar, the Manage menu or the editor has this id", "id", id)
	}
	return nil
}
