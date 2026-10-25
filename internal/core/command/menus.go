package command

import (
	"cmp"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// The ids of the menus (VS Code's MenuId). A menu is a list of items that other code adds to by its
// id, so the id of a menu is a plain string.
//
// Ported from VS Code: src/vs/platform/actions/common/actions.ts (MenuId)
const (
	// MenubarMainMenu holds the menus of the menu bar as submenu items, in the order of the bar.
	MenubarMainMenu = "MenubarMainMenu"
	// MenubarFileMenu, MenubarEditMenu, MenubarSelectionMenu, MenubarViewMenu, MenubarGoMenu,
	// MenubarTerminalMenu and MenubarHelpMenu are the menus of the menu bar.
	MenubarFileMenu      = "MenubarFileMenu"
	MenubarEditMenu      = "MenubarEditMenu"
	MenubarSelectionMenu = "MenubarSelectionMenu"
	MenubarViewMenu      = "MenubarViewMenu"
	MenubarGoMenu        = "MenubarGoMenu"
	MenubarTerminalMenu  = "MenubarTerminalMenu"
	MenubarHelpMenu      = "MenubarHelpMenu"
	// MenubarPreferencesMenu is the Preferences submenu of the File menu.
	MenubarPreferencesMenu = "MenubarPreferencesMenu"
	// ThemesSubMenu is the Themes submenu of the Preferences menu and of the Manage menu.
	ThemesSubMenu = "ThemesSubMenu"
	// ProfilesSubMenu is the Profiles submenu of the Preferences menu and of the Manage menu (step 8.6b).
	ProfilesSubMenu = "ProfilesSubMenu"
	// MenubarAppearanceMenu is the Appearance submenu of the View menu.
	MenubarAppearanceMenu = "MenubarAppearanceMenu"
	// MenubarRecentMenu is the Open Recent submenu of the File menu; its items are dynamic.
	MenubarRecentMenu = "MenubarRecentMenu"
	// MenubarLayoutMenu is the Editor Layout submenu of the View menu.
	MenubarLayoutMenu = "MenubarLayoutMenu"
	// MenubarSwitchEditorMenu is the Switch Editor submenu of the Go menu.
	MenubarSwitchEditorMenu = "MenubarSwitchEditorMenu"
	// MenubarSwitchGroupMenu is the Switch Group submenu of the Go menu.
	MenubarSwitchGroupMenu = "MenubarSwitchGroupMenu"
	// PanelPositionMenu is the Panel Position submenu of the Appearance menu.
	PanelPositionMenu = "PanelPositionMenu"
	// EditorContext is the context menu of the editor.
	EditorContext = "EditorContext"
	// EditorTitleContext is the context menu of a tab of the editor.
	EditorTitleContext = "EditorTitleContext"
	// EditorSplitMoveSubmenu is the Split & Move submenu of the context menu of a tab.
	EditorSplitMoveSubmenu = "EditorSplitMoveSubmenu"
	// WorkspaceTabContext is the context menu of a workspace tab in the title bar (step 9b.2, SPEC §10.14).
	WorkspaceTabContext = "WorkspaceTabContext"
	// WorkspaceTabNew is the menu of the "+" button after the workspace tabs: one row for each kind of window tab that
	// can be made (step 9b.2 has Workspace...; step 11.7 adds the extension tabs).
	WorkspaceTabNew = "WorkspaceTabNew"
	// ExplorerContext is the context menu of the Explorer.
	ExplorerContext = "ExplorerContext"
	// CommandPalette is the menu of the command palette: a command is listed there unless an item
	// of this menu hides it with a When that does not hold.
	CommandPalette = "CommandPalette"
	// GlobalActivity is the Manage menu: the gear at the bottom of the activity bar.
	GlobalActivity = "GlobalActivity"
)

// MenuItem is a row of a menu: a command, or a submenu that another menu is shown as. Exactly one of
// Command and Submenu is set.
//
// Ported from VS Code: src/vs/platform/actions/common/actions.ts (IMenuItem, ISubmenuItem)
type MenuItem struct {
	// Menu is the id of the menu the item is in.
	Menu string
	// Command is the id of the command the item runs. An item whose command is not registered is not
	// shown.
	Command string
	// Submenu is the id of the menu this item opens, for a submenu item.
	Submenu string
	// Title is the text of the item; the title of the command when empty (a submenu needs one).
	Title string
	// MnemonicTitle is the text of the item in the menu bar, with the mnemonic marked by "&&" as VS
	// Code writes it ("&&Undo", "Cu&&t"); Title when empty. Context menus have no mnemonics and show
	// Title.
	MnemonicTitle string
	// ToggledTitle replaces Title in a context menu while the item is toggled, and
	// ToggledMnemonicTitle (ToggledTitle when empty) replaces MnemonicTitle in the menu bar whether
	// or not it is (VS Code's `toggled.title` and `toggled.mnemonicTitle`: "Primary Side Bar").
	ToggledTitle, ToggledMnemonicTitle string
	// Group is the group the item belongs to; the groups of a menu are separated by a line and
	// ordered by name ("navigation" first, the items without a group last). VS Code names them
	// "1_do", "2_ccp"...
	Group string
	// Order orders the items of a group, and is 0 when not set.
	Order float64
	// When is the condition under which the item is in the menu; nil is always.
	When contextkey.Expr
	// Precondition, when set, is the condition under which the item is enabled in place of the
	// precondition of the command. An item that is not enabled stays in the menu, dimmed.
	Precondition contextkey.Expr
	// Toggled is the condition under which the item shows a check mark.
	Toggled contextkey.Expr
	// Args are the arguments the command is run with (an item of a dynamic menu).
	Args any
}

// MenuEntry is an item of a menu as the context shows it: the items that apply, in order, with a
// separator entry between two groups.
type MenuEntry struct {
	// Command is the command to run, or empty for a separator and a submenu.
	Command string
	// Args are the arguments of the command.
	Args any
	// Title is the text of the entry; MnemonicTitle is the text with "&&" marking the mnemonic, as
	// the menu bar wants it (see MnemonicMenuLabel).
	Title, MnemonicTitle string
	// Checked tells that the entry shows a check mark.
	Checked bool
	// Disabled tells that the precondition of the command does not hold.
	Disabled bool
	// Separator makes this entry a line between two groups.
	Separator bool
	// Submenu is the id of the menu a submenu entry shows; Items are its entries.
	Submenu string
	Items   []MenuEntry
}

// MenuProvider gives the items of a menu that change with the state of the program (the recently
// opened files): it is asked each time the menu is built. The items have to be of this menu.
type MenuProvider func(ctx contextkey.Context) []MenuItem

// MenuRegistry holds the items of the menus (SPEC §11.2). It is safe for concurrent use. Make one
// with NewMenuRegistry; there is no global instance.
//
// Ported from VS Code: src/vs/platform/actions/common/actions.ts (MenuRegistry) and
// src/vs/platform/actions/common/menuService.ts
type MenuRegistry struct {
	commands *Registry

	mu        sync.RWMutex
	items     map[string][]MenuItem
	providers map[string][]MenuProvider
}

// NewMenuRegistry makes an empty registry of menus over a registry of commands, which gives the
// titles and the preconditions of the items.
func NewMenuRegistry(commands *Registry) *MenuRegistry {
	return &MenuRegistry{commands: commands, items: map[string][]MenuItem{}, providers: map[string][]MenuProvider{}}
}

// Add adds items to their menus. An item needs a menu, and a command or a submenu but not both.
func (r *MenuRegistry) Add(items ...MenuItem) error {
	for _, it := range items {
		if it.Menu == "" {
			return errors.New("MenuItemMenuMissing", "a menu item has no menu", "command", it.Command, "submenu", it.Submenu)
		}
		if (it.Command == "") == (it.Submenu == "") {
			return errors.New("MenuItemTargetInvalid", "a menu item has either a command or a submenu", "menu", it.Menu, "command", it.Command, "submenu", it.Submenu)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, it := range items {
		r.items[it.Menu] = append(r.items[it.Menu], it)
	}
	return nil
}

// MustAdd is Add for the table of built-in items; it panics on an invalid item.
func (r *MenuRegistry) MustAdd(items ...MenuItem) {
	if err := r.Add(items...); err != nil {
		panic(err)
	}
}

// SetProvider adds a provider of dynamic items to a menu; its items are sorted with the others.
func (r *MenuRegistry) SetProvider(menu string, p MenuProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[menu] = append(r.providers[menu], p)
}

// All lists the items added to menus, by menu and then in the order they were added.
func (r *MenuRegistry) All() []MenuItem {
	r.mu.RLock()
	defer r.mu.RUnlock()
	menus := make([]string, 0, len(r.items))
	for m := range r.items {
		menus = append(menus, m)
	}
	slices.Sort(menus)
	var out []MenuItem
	for _, m := range menus {
		out = append(out, r.items[m]...)
	}
	return out
}

// Items gives the entries of a menu in a context: the items whose When holds, in the order of
// VS Code (groups by name with "navigation" first and the items without a group last, then by
// order, then by title), with a separator entry between two groups. A group with nothing in it is
// left out, and so is a submenu with nothing in it, an item whose command is not registered and
// an item with no title. An item whose command is not enabled in the context stays, Disabled.
func (r *MenuRegistry) Items(menu string, ctx contextkey.Context) []MenuEntry {
	return r.items0(menu, ctx, map[string]bool{})
}

// items0 is Items with the menus that are open above, so that a submenu that holds itself is left out.
func (r *MenuRegistry) items0(menu string, ctx contextkey.Context, path map[string]bool) []MenuEntry {
	if path[menu] {
		return nil
	}
	path[menu] = true
	defer delete(path, menu)

	r.mu.RLock()
	items := slices.Clone(r.items[menu])
	providers := slices.Clone(r.providers[menu])
	r.mu.RUnlock()
	for _, p := range providers {
		items = append(items, p(ctx)...)
	}
	type titled struct {
		item  MenuItem
		title string
	}
	sorted := make([]titled, 0, len(items))
	for _, it := range items {
		sorted = append(sorted, titled{it, r.plainTitle(it)})
	}
	slices.SortStableFunc(sorted, func(a, b titled) int { return compareMenuItems(a.item, a.title, b.item, b.title) })

	var out []MenuEntry
	for i := 0; i < len(sorted); {
		// a group is a run of items of one group
		j := i
		for j < len(sorted) && sorted[j].item.Group == sorted[i].item.Group {
			j++
		}
		var group []MenuEntry
		for _, t := range sorted[i:j] {
			if t.item.When != nil && !t.item.When.Evaluate(ctx) {
				continue
			}
			if e, ok := r.entry(t.item, t.title, ctx, path); ok {
				group = append(group, e)
			}
		}
		if len(group) > 0 {
			if len(out) > 0 {
				out = append(out, MenuEntry{Separator: true})
			}
			out = append(out, group...)
		}
		i = j
	}
	return out
}

// plainTitle is the title of an item: its own, or the one of its command.
func (r *MenuRegistry) plainTitle(it MenuItem) string {
	if it.Title != "" || it.Command == "" {
		return it.Title
	}
	m, _ := r.commands.Lookup(it.Command)
	return m.Title
}

// compareMenuItems orders two items as VS Code does.
//
// Ported from VS Code: src/vs/platform/actions/common/menuService.ts (MenuInfo.compareMenuItems)
func compareMenuItems(a MenuItem, aTitle string, b MenuItem, bTitle string) int {
	if a.Group != b.Group {
		switch {
		case a.Group == "":
			return 1
		case b.Group == "":
			return -1
		case a.Group == "navigation":
			return -1
		case b.Group == "navigation":
			return 1
		}
		if c := strings.Compare(a.Group, b.Group); c != 0 {
			return c
		}
	}
	if c := cmp.Compare(a.Order, b.Order); c != 0 {
		return c
	}
	if c := strings.Compare(strings.ToLower(aTitle), strings.ToLower(bTitle)); c != 0 {
		return c
	}
	return strings.Compare(aTitle, bTitle)
}

// entry makes the entry of an item, or says that the item does not show.
func (r *MenuRegistry) entry(it MenuItem, title string, ctx contextkey.Context, path map[string]bool) (MenuEntry, bool) {
	if it.Submenu != "" {
		items := r.items0(it.Submenu, ctx, path)
		if title == "" || len(items) == 0 {
			return MenuEntry{}, false
		}
		mnemonic := it.MnemonicTitle
		if mnemonic == "" {
			mnemonic = title
		}
		return MenuEntry{Submenu: it.Submenu, Title: title, MnemonicTitle: mnemonic, Items: items}, true
	}
	meta, ok := r.commands.Lookup(it.Command)
	if !ok || title == "" {
		return MenuEntry{}, false
	}
	pre := it.Precondition
	if pre == nil {
		pre = meta.Precondition
	}
	e := MenuEntry{Command: it.Command, Args: it.Args, Title: title, MnemonicTitle: it.MnemonicTitle,
		Disabled: pre != nil && !pre.Evaluate(ctx)}
	if e.MnemonicTitle == "" {
		e.MnemonicTitle = title
	}
	if it.Toggled != nil {
		e.Checked = it.Toggled.Evaluate(ctx)
		if e.Checked && it.ToggledTitle != "" {
			e.Title = it.ToggledTitle
		}
		// the menu bar shows the title of the toggle whether or not it is checked
		switch {
		case it.ToggledMnemonicTitle != "":
			e.MnemonicTitle = it.ToggledMnemonicTitle
		case it.ToggledTitle != "":
			e.MnemonicTitle = it.ToggledTitle
		}
	}
	return e, true
}

// MnemonicMenuLabel turns a title written the way VS Code writes mnemonics ("&&File": the F is the
// mnemonic, a single "&" is an ampersand) into a label of the menu widgets, where a single ampersand
// marks the mnemonic and "&&" is an ampersand: "&&File" gives "&File" and "Q&A" gives "Q&&A".
//
// Ported from VS Code: src/vs/base/common/labels.ts (mnemonicMenuLabel, the version for Windows
// and Linux)
func MnemonicMenuLabel(title string) string {
	var b strings.Builder
	for i := 0; i < len(title); i++ {
		if title[i] != '&' {
			b.WriteByte(title[i])
			continue
		}
		if i+1 < len(title) && title[i+1] == '&' {
			b.WriteByte('&')
			i++
			continue
		}
		b.WriteString("&&")
	}
	return b.String()
}
