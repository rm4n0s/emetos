package kit

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/ui"
)

// MenuItem is an item of a Menu (VS Code's `IAction`, `Separator` and `SubmenuAction`): an action
// with a label, a separator, or a submenu.
type MenuItem struct {
	// ID identifies the item to the code that handles OnRun of the menu.
	ID string
	// Label is the text of the item, with the mnemonic marked by an ampersand as VS Code writes it:
	// "&File" (the F is the mnemonic), "E&xit", "&&" for a literal ampersand and "文件(&F)".
	Label string
	// Keybinding is the label of the keybinding that runs the item ("Ctrl+S"), shown at the right
	// edge. The caller computes it from the keybinding service.
	Keybinding string
	// Checked shows a check mark before the label.
	Checked bool
	// Disabled dims the item; it cannot take the focus or run. It has no effect on a submenu, as in
	// VS Code, where a submenu is always enabled.
	Disabled bool
	// Separator makes this a separator: a line between items that never takes the focus.
	Separator bool
	// Submenu makes this a submenu that opens to the right of the item (a submenu with no Items
	// shows one disabled "(empty)" item, as VS Code's `EmptySubmenuAction`).
	Submenu bool
	// Items are the items of the submenu.
	Items []MenuItem
	// OnRun is called when the item runs, before the OnRun of the menu.
	OnRun func()
	// Data is the caller's value for the item (the arguments of the command it runs), which comes
	// back with the item to the OnRun of the menu.
	Data any
}

// SeparatorItem is the separator for a list of menu items.
func SeparatorItem() MenuItem { return MenuItem{Separator: true} }

// SubmenuItem is a submenu with a label and items.
func SubmenuItem(label string, items []MenuItem) MenuItem {
	return MenuItem{Label: label, Submenu: true, Items: items}
}

// PrepareMenuItems cleans a list of items as the constructor of VS Code's `Menu` does: it drops a
// separator at the start or the end of the list and a separator that follows another one.
//
// Ported from VS Code: src/vs/base/browser/ui/menu/menu.ts (the filter in the constructor of Menu)
func PrepareMenuItems(items []MenuItem) []MenuItem {
	out := make([]MenuItem, 0, len(items))
	for i, it := range items {
		if it.Separator {
			if i == 0 || i == len(items)-1 || items[i-1].Separator {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

// menuMnemonicRegex is MENU_MNEMONIC_REGEX of VS Code: the first of "(&X)" (X is the mnemonic and
// the parentheses are dropped when the mnemonics are off) or "&X" not preceded by another "&".
//
// Ported from VS Code: src/vs/base/browser/ui/menu/menu.ts (MENU_MNEMONIC_REGEX)
var menuMnemonicRegex = regexp.MustCompile(`\(&([^\s&])\)|(^|[^&])&([^\s&])`)

// MnemonicKey is the key of the mnemonic of a label, in lower case, or "" when it has none:
// "&File" gives "f", "文件(&F)" gives "f" and "Save &&All" gives "".
func MnemonicKey(label string) string {
	m := menuMnemonicRegex.FindStringSubmatch(label)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return strings.ToLower(m[1])
	}
	return strings.ToLower(m[3])
}

// CleanMnemonic takes the mnemonic marker out of a label: "&File" gives "File" and "文件(&F)"
// gives "文件". An ampersand that is escaped by doubling stays doubled (see MenuLabelText).
//
// Ported from VS Code: src/vs/base/browser/ui/menu/menu.ts (cleanMnemonic)
func CleanMnemonic(label string) string {
	loc := menuMnemonicRegex.FindStringSubmatchIndex(label)
	if loc == nil {
		return label
	}
	var repl string
	if loc[2] < 0 { // not "(&X)": the mnemonic is in the text, keep the characters around the marker
		repl = label[loc[4]:loc[5]] + label[loc[6]:loc[7]]
	}
	return strings.TrimSpace(label[:loc[0]] + repl + label[loc[1]:])
}

// MenuLabelText is the text a label shows when mnemonics are off: the marker is gone and "&&" is
// "&".
func MenuLabelText(label string) string {
	return strings.TrimSpace(strings.ReplaceAll(CleanMnemonic(label), "&&", "&"))
}

// MnemonicLabel is a label cut around its mnemonic, for showing it with the mnemonic underlined.
type MnemonicLabel struct {
	// Before, Mnemonic and After are the text before the mnemonic character, the character, and
	// the text after it; "&&" is already "&". Mnemonic is "" when the label has none (Before then
	// holds the whole text).
	Before, Mnemonic, After string
}

// Text is the whole label as shown.
func (l MnemonicLabel) Text() string { return l.Before + l.Mnemonic + l.After }

// ParseMenuLabel cuts a label for showing it with its mnemonic: the first single ampersand that
// is followed by a character that is not white space or an ampersand marks the mnemonic, and
// "&&" is a literal ampersand. The text before the mnemonic is trimmed on the left and the text
// after it on the right, as VS Code does. A label with no mnemonic, and every label when
// enableMnemonics is off, is just text (MenuLabelText).
//
// Ported from VS Code: src/vs/base/browser/ui/menu/menu.ts (BaseMenuActionViewItem.updateLabel) and
// menubar.ts (MenuBar.updateLabels)
func ParseMenuLabel(label string, enableMnemonics bool) MnemonicLabel {
	if !enableMnemonics {
		return MnemonicLabel{Before: MenuLabelText(label)}
	}
	unescape := func(s string) string { return strings.ReplaceAll(s, "&&", "&") }
	for i := 0; i < len(label); i++ {
		if label[i] != '&' {
			continue
		}
		if i+1 < len(label) && label[i+1] == '&' { // "&&": a literal ampersand, skip both
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(label[i+1:])
		if i+1 >= len(label) || unicode.IsSpace(r) {
			continue
		}
		return MnemonicLabel{
			Before:   strings.TrimLeft(unescape(label[:i]), " "),
			Mnemonic: string(r),
			After:    strings.TrimRight(unescape(label[i+1+size:]), " "),
		}
	}
	return MnemonicLabel{Before: strings.TrimSpace(unescape(label))}
}

// menuModel is the keyboard state of one level of a menu: which item has the focus and the
// rotation of the items that share a mnemonic.
type menuModel struct {
	items []MenuItem
	// focused is the index of the focused item, or -1.
	focused int
	// cycle counts how often each mnemonic was used, to rotate through items that share one.
	cycle map[string]int
}

func newMenuModel(items []MenuItem) *menuModel {
	return &menuModel{items: items, focused: -1, cycle: map[string]int{}}
}

// canFocus says whether an item can take the focus: menus only focus enabled items
// (`focusOnlyEnabledItems`), and a submenu is always enabled (VS Code's submenu items do not
// observe the enablement of their action).
func (m *menuModel) canFocus(i int) bool {
	return i >= 0 && i < len(m.items) && !m.items[i].Separator && (!m.items[i].Disabled || m.items[i].Submenu)
}

// Move moves the focus to the next (dir 1) or previous (dir -1) item that can take it, wrapping
// around the ends. With no item focused, Down gives the first item and Up the last. It says
// whether the focus moved.
func (m *menuModel) Move(dir int) bool {
	n := len(m.items)
	if n == 0 {
		return false
	}
	from := m.focused
	if from < 0 {
		if dir > 0 {
			from = n - 1
		} else {
			from = 0
		}
	}
	i := from
	for {
		i = (i + dir + n) % n
		if m.canFocus(i) {
			break
		}
		if i == from {
			return false
		}
	}
	changed := i != m.focused
	m.focused = i
	return changed
}

// First focuses the first item that can take the focus.
func (m *menuModel) First() bool {
	for i := range m.items {
		if m.canFocus(i) {
			changed := m.focused != i
			m.focused = i
			return changed
		}
	}
	return false
}

// Last focuses the last item that can take the focus.
func (m *menuModel) Last() bool {
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.canFocus(i) {
			changed := m.focused != i
			m.focused = i
			return changed
		}
	}
	return false
}

// Focus focuses an item if it can take the focus.
func (m *menuModel) Focus(i int) bool {
	if !m.canFocus(i) {
		return false
	}
	changed := m.focused != i
	m.focused = i
	return changed
}

// Mnemonic gives the item a mnemonic key picks: the only enabled item with it, or, when several
// share it, the next of them each time the key is pressed. unique says whether the key has just
// one item (which runs at once; with several the key only moves the focus). It is -1 when no
// enabled item has the key.
func (m *menuModel) Mnemonic(key string) (index int, unique bool) {
	var matches []int
	for i, it := range m.items {
		if it.Separator || (it.Disabled && !it.Submenu) || MnemonicKey(it.Label) != key {
			continue
		}
		matches = append(matches, i)
	}
	switch len(matches) {
	case 0:
		return -1, false
	case 1:
		return matches[0], true
	}
	n := m.cycle[key]
	m.cycle[key] = n + 1
	return matches[n%len(matches)], false
}

// Menu placement.
//
// Ported from VS Code: src/vs/base/common/layout.ts

// LayoutAnchorPosition says on which side of an anchor a view is laid out.
type LayoutAnchorPosition int

// The sides of an anchor.
const (
	// LayoutBefore lays the view out after the anchor, to the right or below it, and flips to
	// before it when there is no room (VS Code's `LayoutAnchorPosition.Before` is the position of
	// the anchor in front of the view).
	LayoutBefore LayoutAnchorPosition = iota
	// LayoutAfter lays the view out before the anchor, to the left or above it, and flips to after
	// it when there is no room.
	LayoutAfter
)

// LayoutAnchorMode says whether a view avoids the anchor or aligns with its edge.
type LayoutAnchorMode int

// The anchor modes.
const (
	// LayoutAvoid keeps the view off the anchor.
	LayoutAvoid LayoutAnchorMode = iota
	// LayoutAlign puts the view's edge on the anchor's edge.
	LayoutAlign
)

// LayoutAnchor is a range on an axis that a view is laid out next to.
type LayoutAnchor struct {
	Offset, Size float64
	Mode         LayoutAnchorMode
	Position     LayoutAnchorPosition
}

// LayoutResult says how Layout placed a view.
type LayoutResult int

// The ways a view can be placed.
const (
	// LayoutOK is the preferred side.
	LayoutOK LayoutResult = iota
	// LayoutFlipped is the other side.
	LayoutFlipped
	// LayoutOverlap is over the anchor: there was no room on either side.
	LayoutOverlap
)

// Layout lays a view of a size out along an axis of a viewport size next to an anchor and returns
// the offset of the view.
//
// Ported from VS Code: src/vs/base/common/layout.ts (layout)
func Layout(viewport, view float64, a LayoutAnchor) (float64, LayoutResult) {
	after, before := a.Offset+a.Size, a.Offset
	if a.Mode == LayoutAlign {
		after, before = a.Offset, a.Offset+a.Size
	}
	if a.Position == LayoutBefore {
		switch {
		case view <= viewport-after:
			return after, LayoutOK
		case view <= before:
			return before - view, LayoutFlipped
		}
		return max(viewport-view, 0), LayoutOverlap
	}
	switch {
	case view <= before:
		return before - view, LayoutOK
	case view <= viewport-after && before < view/2:
		return after, LayoutFlipped
	}
	return 0, LayoutOverlap
}

// pointerAnchorSize is the size VS Code gives the mouse location as an anchor, so that the menu
// is not exactly under the pointer when it shows.
const pointerAnchorSize = 2.0

// PlaceContextMenu gives the top left corner of a context menu of a size opened at a pointer
// position (both in window coordinates). The menu opens to the right of and below the pointer. A
// menu that does not fit below flips above the pointer, one that does not fit to the right
// flips to the left of it, and one that fits on neither side is pushed against the window's
// edge, as VS Code's context view lays it out.
//
// Ported from VS Code: src/vs/base/common/layout.ts (layout2d) and
// src/vs/base/browser/ui/contextview/contextview.ts (getAnchorRect)
func PlaceContextMenu(at ui.Offset, size, window ui.Size) ui.Offset {
	anchor := ui.NewRectFromLTWH(at.Dx, at.Dy, pointerAnchorSize, pointerAnchorSize)
	return PlaceMenuAt(anchor, size, window)
}

// PlaceMenuAt gives the top left corner of a menu of a size placed below and to the right of an
// anchor rectangle (the vertical axis first, then the horizontal one, and the menu avoids the
// anchor when it overlaps it vertically), flipping to the other side of the anchor on an axis
// where it does not fit.
//
// Ported from VS Code: src/vs/base/common/layout.ts (layout2d)
func PlaceMenuAt(anchor ui.Rect, size, window ui.Size) ui.Offset {
	vertical := LayoutAnchor{Offset: anchor.Top, Size: anchor.Height(), Position: LayoutBefore}
	horizontal := LayoutAnchor{Offset: anchor.Left, Size: anchor.Width(), Position: LayoutBefore, Mode: LayoutAlign}
	top, _ := Layout(window.Height, size.Height, vertical)
	// if the view intersects the anchor vertically it must avoid it horizontally
	if max(top, vertical.Offset) < min(top+size.Height, vertical.Offset+vertical.Size) {
		horizontal.Mode = LayoutAvoid
	}
	left, _ := Layout(window.Width, size.Width, horizontal)
	return ui.Offset{Dx: left, Dy: top}
}

// submenuOverlap is how far a submenu that has no room beside its item overlaps the item.
const submenuOverlap = 10.0

// PlaceSubmenu gives the top left corner of a submenu of a size next to the item of its parent
// menu that opens it (all in window coordinates). The item is the box of the row plus the
// padding of the parent above and below it, so that the first row of the submenu lines up with
// the item. The submenu opens to the right; without room it opens to the left, and without room on
// either side it overlaps the item by 10 px. Vertically it starts at the item and, when it does
// not fit below, ends at the item's bottom edge.
//
// Ported from VS Code: src/vs/base/browser/ui/menu/menu.ts (SubmenuMenuActionViewItem.calculateSubmenuMenuLayout)
func PlaceSubmenu(item ui.Rect, size, window ui.Size) ui.Offset {
	entryTop, entryHeight := item.Top, item.Height()
	left, _ := Layout(window.Width, size.Width, LayoutAnchor{Offset: item.Left, Size: item.Width(), Position: LayoutBefore})
	// no room to lay the menu out fully: overlap the item
	if left >= item.Left && left < item.Right {
		if item.Left+submenuOverlap+size.Width <= window.Width {
			left = item.Left + submenuOverlap
		}
		entryTop += submenuOverlap
		entryHeight = 0
	}
	top, _ := Layout(window.Height, size.Height, LayoutAnchor{Offset: entryTop, Position: LayoutBefore})
	// no room below, but room above: move down so that the menu ends at the item's bottom edge
	if top+size.Height == entryTop && top+entryHeight+size.Height <= window.Height {
		top += entryHeight
	}
	return ui.Offset{Dx: left, Dy: top}
}
