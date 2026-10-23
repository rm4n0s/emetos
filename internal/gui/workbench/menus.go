package workbench

import (
	"context"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// MenuSourceOptions say what a MenuSource is made of. Menus and Commands are required.
type MenuSourceOptions struct {
	// Menus holds the items of the menus.
	Menus *command.MenuRegistry
	// Commands runs the commands of the items.
	Commands *command.Registry
	// Keybindings gives the keys shown at the right edge of an item; without it the items show none.
	Keybindings *keybinding.Resolver
	// Context gives the context keys the menu bar and the Manage menu are evaluated in, each time one
	// opens; a menu that is not in the window's focus has the keys of the window. An empty context
	// when nil.
	Context func() contextkey.Context
	// Schedule runs a function once the menu is closed and the focus is back where it was; the
	// commands of the editor are bound while the editor has the focus, so a command run at once, while
	// the menu still has it, would find none. Without it the command runs at once.
	Schedule func(func())
	// Ctx is the context of the commands that are run; context.Background() when nil.
	Ctx context.Context
	// OnError is told when a command run by a menu fails.
	OnError func(commandID string, err error)
}

// MenuSource turns the menu registry into the items of the menu widgets: the menu bar, the context
// menus and the Manage menu (SPEC §10.1). The labels have the mnemonics of the menu bar, and each item
// shows the key that runs its command (VS Code's label style on Linux, "Ctrl+N"), a check mark when
// it is toggled and a dimmed look when the precondition of its command does not hold. Running an item
// runs its command through the registry.
//
// Ported from VS Code: src/vs/workbench/browser/parts/titlebar/menubarControl.ts and
// src/vs/platform/actions/browser/menuEntryActionViewItem.ts
type MenuSource struct {
	o MenuSourceOptions
}

// NewMenuSource makes the source of the menus.
func NewMenuSource(o MenuSourceOptions) *MenuSource {
	if o.Context == nil {
		o.Context = func() contextkey.Context { return contextkey.MapContext{} }
	}
	if o.Ctx == nil {
		o.Ctx = context.Background()
	}
	return &MenuSource{o: o}
}

// BarMenus are the menus of the menu bar: the submenus of MenubarMainMenu that have items. Each
// menu is built again, in the context of that moment, when the user opens it.
func (m *MenuSource) BarMenus() []kit.MenuBarMenu {
	var out []kit.MenuBarMenu
	for _, e := range m.o.Menus.Items(command.MenubarMainMenu, m.o.Context()) {
		if e.Submenu == "" {
			continue
		}
		id := e.Submenu
		out = append(out, kit.MenuBarMenu{
			ID:     id,
			Label:  command.MnemonicMenuLabel(e.MnemonicTitle),
			Items:  m.Items(id, m.o.Context()),
			Source: func() []kit.MenuItem { return m.Items(id, m.o.Context()) },
		})
	}
	return out
}

// BarIndex is the position of a menu in the menu bar, or -1 when the bar has no menu with the id (an
// id of a menu that is not a submenu of the main menu, or a menu with nothing in it).
func (m *MenuSource) BarIndex(menu string) int {
	for i, bar := range m.BarMenus() {
		if bar.ID == menu {
			return i
		}
	}
	return -1
}

// Items are the items of a menu in a context, for a Menu or a ContextMenu.
func (m *MenuSource) Items(menu string, ctx contextkey.Context) []kit.MenuItem {
	if ctx == nil {
		ctx = m.o.Context()
	}
	return m.convert(m.o.Menus.Items(menu, ctx), ctx)
}

// ManageItems are the items of the Manage menu, the gear of the activity bar. A menu with nothing in
// it has one dimmed item, as an empty submenu does.
func (m *MenuSource) ManageItems() []kit.MenuItem {
	items := m.Items(command.GlobalActivity, nil)
	if len(items) == 0 {
		return []kit.MenuItem{{Label: "(empty)", Disabled: true}}
	}
	return items
}

func (m *MenuSource) convert(entries []command.MenuEntry, ctx contextkey.Context) []kit.MenuItem {
	out := make([]kit.MenuItem, 0, len(entries))
	for _, e := range entries {
		switch {
		case e.Separator:
			out = append(out, kit.SeparatorItem())
		case e.Submenu != "":
			out = append(out, kit.MenuItem{ID: e.Submenu, Label: command.MnemonicMenuLabel(e.MnemonicTitle), Submenu: true, Items: m.convert(e.Items, ctx)})
		default:
			it := kit.MenuItem{ID: e.Command, Label: command.MnemonicMenuLabel(e.MnemonicTitle), Checked: e.Checked, Disabled: e.Disabled, Data: e.Args}
			if m.o.Keybindings != nil {
				if kb, ok := m.o.Keybindings.LookupPrimaryKeybinding(e.Command, ctx, false); ok && kb.Keybinding != nil {
					it.Keybinding = kb.Keybinding.Label()
				}
			}
			out = append(out, it)
		}
	}
	return out
}

// Run runs the command of an item that the user ran: through the registry, with the arguments of the
// item. A failure goes to OnError.
func (m *MenuSource) Run(item kit.MenuItem) {
	if item.ID == "" || item.Separator || item.Submenu {
		return
	}
	run := func() {
		ctx := command.Context{Ctx: m.o.Ctx, Keys: m.o.Context(), Source: command.SourceMenu}
		if err := m.o.Commands.Run(ctx, item.ID, item.Data); err != nil && m.o.OnError != nil {
			m.o.OnError(item.ID, err)
		}
	}
	if m.o.Schedule != nil {
		m.o.Schedule(run)
		return
	}
	run()
}
