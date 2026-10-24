package quickinput

import (
	"github.com/rm4n0s/emetos/internal/core/fuzzy"
	"github.com/rm4n0s/emetos/internal/core/theme"
)

// Button is a button at the right edge of an item (VS Code's IQuickInputButton).
type Button struct {
	// Icon is the code point of a Codicon.
	Icon int
	// Tooltip is the hover text.
	Tooltip string
	// AlwaysVisible shows the button on every row, not only on the active row and the row under the pointer:
	// the dirty mark of the editor picker.
	AlwaysVisible bool
	// HoverIcon, when set, is the Codicon the button shows while the pointer is over the row: the dirty dot
	// turns into the cross.
	HoverIcon int
}

// TriggerAction says what happens after an item's button was triggered (VS Code's TriggerAction).
type TriggerAction int

// The things that can happen after a button was triggered.
const (
	// TriggerNone does nothing.
	TriggerNone TriggerAction = iota
	// TriggerClose hides the picker.
	TriggerClose
	// TriggerRefresh asks the provider for the items again.
	TriggerRefresh
	// TriggerRemove takes the item out of the list.
	TriggerRemove
)

// KeyMods are the modifier keys held when an item was accepted or a button triggered.
type KeyMods struct {
	Ctrl, Alt bool
}

// Highlights are the parts of an item's texts that matched the filter, in runes.
type Highlights struct {
	Label, Description, Detail []fuzzy.Match
}

// Item is one row of a quick pick (VS Code's IQuickPickItem and IQuickPickSeparator). A separator
// item has only a Label: the list shows it on the row of the next item that is shown.
type Item struct {
	// ID identifies the item for the provider (the id of a command); it is not shown.
	ID string
	// Label is the main text; the filter matches it.
	Label string
	// Description is shown after the label in a dimmer color.
	Description string
	// Detail is shown on a second line below the label.
	Detail string
	// Icon is the code point of a Codicon shown before the label; 0 shows none.
	Icon int
	// FileIcon, when set, is asked for the icon of a file icon theme (Seti) to draw before the label in place
	// of Icon, for the type of the color theme; false draws none. It runs on the UI thread, when the row is
	// built.
	FileIcon func(typ theme.Type) (theme.FileIcon, bool)
	// Italic draws the label in italics: a preview editor in the editor picker.
	Italic bool
	// Separator makes the item the heading of the items after it: it cannot be chosen and its
	// Label is drawn at the right of the next item shown, with a line above that item.
	Separator bool
	// Keybinding is the label of the key of the item, such as "Ctrl+G", drawn as key caps.
	Keybinding string
	// Buttons are drawn at the right edge of the row.
	Buttons []Button
	// AlwaysShow keeps the item in the list when the filter does not match it.
	AlwaysShow bool
	// Highlights are the matches the provider found when it filters the items itself.
	Highlights Highlights
	// Data is the provider's own value.
	Data any
	// Accept runs when the item is chosen, after the picker has been hidden and the focus given
	// back (PickerQuickAccessProvider's accept). An item without it does nothing when chosen and
	// the picker stays open.
	Accept func(mods KeyMods)
	// Trigger runs when a button of the item is triggered; the index counts Buttons.
	Trigger func(button int, mods KeyMods) TriggerAction
}

// Row is an item as the list shows it: the matches of the current filter, the heading of its
// group and whether a line is drawn above it.
type Row struct {
	// Item is the item.
	Item Item
	// Index is the position of the item among the items of the picker.
	Index int
	// Label, Description and Detail are the matches to highlight.
	Label, Description, Detail []fuzzy.Match
	// Separator is the label of the separator before the row, or "".
	Separator string
	// Border tells that a line is drawn above the row (a row with a separator that is not the first).
	Border bool
}

// Height is the height of the row in the list: 22 px, or 44 px with a detail line.
func (r Row) Height() float64 {
	if r.Item.Detail != "" {
		return RowHeightDetail
	}
	return RowHeight
}

// The heights of rows, from VS Code's quickInputList.ts.
const (
	// RowHeight is the height of a row.
	RowHeight = 22.0
	// RowHeightDetail is the height of a row with a detail line.
	RowHeightDetail = 44.0
)
