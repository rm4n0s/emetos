package ui

import (
	"image"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The theme.
type (
	// Colors are the colors of the theme of the window (the `workbench.colorCustomizations` keys of VS Code's themes).
	Colors = kit.Colors
	// TextRange is a range of the characters of a label.
	TextRange = kit.TextRange
)

// Theme returns the colors of the window's theme and rebuilds the caller when they change.
func Theme(bc widgets.BuildContext) Colors { return kit.ThemeOf(bc) }

// TextStyle is a text style of the UI font of the window with a size and a color.
func TextStyle(bc widgets.BuildContext, size float64, color ui.Color) *painting.TextStyle {
	return kit.TextStyleOf(bc, size, color)
}

// TokenLook is how the token theme draws a token: its color and font styles.
type TokenLook = kit.TokenLook

// TokenLooker returns the function that says how the token theme of the window draws a token with the scopes of a Token
// (outermost first); the fallback color is used when the theme gives none. It rebuilds the caller when the theme changes. Call
// it in Build and keep the function for the rows that are built later.
func TokenLooker(bc widgets.BuildContext) func(scopes []string, fallback ui.Color) TokenLook {
	return kit.TokenLookerOf(bc)
}

// Icons.

// CodiconCode returns the code point of the Codicon with a short name ("checklist", "play"); false when there is none.
func CodiconCode(name string) (int, bool) { return fonts.CodiconByName(name) }

// Icon is the Codicon with the name as a widget of a size and a color. An unknown name shows nothing.
func Icon(name string, size float64, color ui.Color) widgets.Widget {
	code, ok := fonts.CodiconByName(name)
	if !ok {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{})
	}
	return widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(size), Color: foundation.Some(color)})
}

// Buttons, inputs and small widgets.
type (
	// Button is the workbench's text button.
	Button = kit.Button
	// ButtonOpts are the options of NewButton.
	ButtonOpts = kit.ButtonOpts
	// IconButton is a button with one Codicon.
	IconButton = kit.IconButton
	// IconButtonOpts are the options of NewIconButton.
	IconButtonOpts = kit.IconButtonOpts
	// InputBox is the workbench's input box.
	InputBox = kit.InputBox
	// InputBoxOpts are the options of NewInputBox.
	InputBoxOpts = kit.InputBoxOpts
	// InputBoxController reads and changes an input box from outside.
	InputBoxController = kit.InputBoxController
	// Badge is a count in a pill.
	Badge = kit.Badge
	// BadgeOpts are the options of NewBadge.
	BadgeOpts = kit.BadgeOpts
	// Checkbox is the workbench's checkbox.
	Checkbox = kit.Checkbox
	// CheckboxOpts are the options of NewCheckbox.
	CheckboxOpts = kit.CheckboxOpts
	// Hover shows a text when the pointer rests on its child.
	Hover = kit.Hover
	// HoverOpts are the options of NewHover.
	HoverOpts = kit.HoverOpts
	// ProgressBar is the workbench's progress bar.
	ProgressBar = kit.ProgressBar
	// ProgressBarOpts are the options of NewProgressBar.
	ProgressBarOpts = kit.ProgressBarOpts
)

// Images.
type (
	// Picture is an image ready to be drawn, with its own copy of the pixels. Make one with NewPicture and keep it: making one
	// copies every pixel.
	Picture = kit.Picture
	// Image is the widget that draws a Picture scaled to fit its space.
	Image = kit.Image
	// ImageOpts are the options of NewImage.
	ImageOpts = kit.ImageOpts
)

// MaxPictureSide is the largest width or height, in pixels, of an image NewPicture takes.
const MaxPictureSide = kit.MaxPictureSide

// NewPicture copies the pixels of an image into a Picture; it may be called from any goroutine (a goroutine that gets the
// frames of a camera makes the Picture, and hands it to the UI thread). It fails with PictureInvalid for a nil or empty image
// and PictureTooLarge for one with a side over MaxPictureSide.
func NewPicture(img image.Image) (*Picture, error) { return kit.NewPicture(img) }

// NewImage makes a widget that draws a picture, scaled down to fit the box it is given (up too with ImageOpts.Upscale),
// keeping its proportions, in the middle of the box. A nil picture draws nothing.
func NewImage(pic *Picture, opts ...ImageOpts) *Image { return kit.NewImage(pic, opts...) }

// Toggle and its options (the switches in the find input of the editor, of the Search view and of the terminal).
type (
	// Toggle is VS Code's icon toggle: a Codicon in a box that has a border and a tinted background while it is on.
	Toggle = kit.Toggle
	// ToggleOpts are the options of NewToggle.
	ToggleOpts = kit.ToggleOpts
)

// NewToggle makes a toggle that shows the Codicon of the name ("case-sensitive", "whole-word", "regex"); an unknown name makes
// a toggle with no icon.
func NewToggle(name string, opts ...ToggleOpts) *Toggle {
	code, _ := fonts.CodiconByName(name)
	return kit.NewToggle(code, opts...)
}

// History is the list of earlier values of an input box, which Up and Down go through (VS Code's HistoryInputBox).
type History = kit.History

// NewHistory makes a history with the values, the oldest first, keeping at most limit of them (a default when not given).
func NewHistory(values []string, limit ...int) *History { return kit.NewHistory(values, limit...) }

// InputMessage is a message shown below an input box while it has the focus.
type InputMessage = kit.InputMessage

// The severities of an InputMessage.
const (
	// ValidationInfo, ValidationWarning and ValidationError are the severities of a message.
	ValidationInfo    = kit.ValidationInfo
	ValidationWarning = kit.ValidationWarning
	ValidationError   = kit.ValidationError
)

// NewButton makes a text button.
func NewButton(label string, opts ...ButtonOpts) *Button { return kit.NewButton(label, opts...) }

// NewIconButton makes a button with the Codicon of the name; an unknown name makes a button with no icon.
func NewIconButton(name string, opts ...IconButtonOpts) *IconButton {
	code, _ := fonts.CodiconByName(name)
	return kit.NewIconButton(code, opts...)
}

// NewInputBox makes an input box over a controller; see kit.NewInputBox.
func NewInputBox(controller *widgets.TextEditingController, focusNode *widgets.FocusNode, opts ...InputBoxOpts) *InputBox {
	return kit.NewInputBox(controller, focusNode, opts...)
}

// NewInputBoxController makes a controller for an input box with a text.
func NewInputBoxController(text string) *InputBoxController { return kit.NewInputBoxController(text) }

// NewBadge makes a count badge.
func NewBadge(count int, opts ...BadgeOpts) *Badge { return kit.NewBadge(count, opts...) }

// NewCheckbox makes a checkbox with a label.
func NewCheckbox(label string, opts ...CheckboxOpts) *Checkbox {
	return kit.NewCheckbox(label, opts...)
}

// NewHover wraps a child with a hover.
func NewHover(child widgets.Widget, opts ...HoverOpts) *Hover { return kit.NewHover(child, opts...) }

// NewProgressBar makes a progress bar.
func NewProgressBar(opts ...ProgressBarOpts) *ProgressBar { return kit.NewProgressBar(opts...) }

// Select boxes and scrolling.
type (
	// Dropdown is a select box: a field that shows the chosen option and opens a list of the options.
	Dropdown = kit.Dropdown
	// DropdownOpts are the options of NewDropdown.
	DropdownOpts = kit.DropdownOpts
	// DropdownOption is one choice of a Dropdown.
	DropdownOption = kit.DropdownOption
	// ScrollHandle moves and measures the content of a ScrollView from outside.
	ScrollHandle = kit.ScrollHandle
)

// NewDropdown makes a select box of options.
func NewDropdown(options []DropdownOption, opts ...DropdownOpts) *Dropdown {
	return kit.NewDropdown(options, opts...)
}

// NewScrollView scrolls a child vertically, with the overlay scrollbar of the editor's lists and the mouse wheel. The
// handle, when it is not nil, reads and sets the position (ScrollTo clamps to the content, so a very large top scrolls to the
// end; MaxPosition says how far that is).
func NewScrollView(child widgets.Widget, handle *ScrollHandle) widgets.Widget {
	return kit.NewScrollableElement(child, kit.ScrollableElementOpts{Axes: kit.ScrollAxisVertical, Handle: handle})
}

// ScrollableOpts are the options of NewScrollable. They are the options of the element that puts the overlay scrollbars of
// the editor over a child: with a Controller (and a HorizontalController) it draws the scrollbars of Glutter scroll views
// that use those controllers, which is how a grid scrolls rows that are built only when they come into view; with neither
// it scrolls a child of its own in the directions of Axes.
type ScrollableOpts = kit.ScrollableElementOpts

// NewScrollable puts the overlay scrollbars of the editor over a child. A grid whose rows are made by a sliver builder passes
// the controllers of its scroll views in the options, so that the scrollbars follow them and the rows off screen are never
// laid out. (NewScrollView is the same for a child that is small enough to be laid out whole.)
func NewScrollable(child widgets.Widget, opts ScrollableOpts) widgets.Widget {
	return kit.NewScrollableElement(child, opts)
}

// Lists and trees.
type (
	// List is a virtualized list of rows with a keyboard.
	List = kit.List
	// ListOpts are the options of NewList.
	ListOpts = kit.ListOpts
	// ListRenderer builds the contents of a row of a list.
	ListRenderer = kit.ListRenderer
	// ListController reads and moves a list from outside.
	ListController = kit.ListController
	// ListViewModel holds the rows, the focus and the selection of a list.
	ListViewModel = kit.ListViewModel
	// ListViewModelOpts are the options of NewListViewModel.
	ListViewModelOpts = kit.ListViewModelOpts
	// ListViewItem is the data of a row.
	ListViewItem = kit.ListViewItem
	// ListViewRow is a row that is shown.
	ListViewRow = kit.ListViewRow
	// RowState is what a row renderer is told about the look of its row.
	RowState = kit.RowState
	// Tree is a virtualized tree of nodes with a keyboard.
	Tree = kit.Tree
	// TreeOpts are the options of NewTree.
	TreeOpts = kit.TreeOpts
	// TreeRenderer builds the contents of a row of a tree.
	TreeRenderer = kit.TreeRenderer
	// TreeController reads and moves a tree from outside.
	TreeController = kit.TreeController
	// TreeModel holds the nodes, their expansion, the focus and the selection of a tree.
	TreeModel = kit.TreeModel
	// TreeModelOpts are the options of NewTreeModel.
	TreeModelOpts = kit.TreeModelOpts
	// TreeItem is the data of a node.
	TreeItem = kit.TreeItem
	// TreeNode is a row of a tree.
	TreeNode = kit.TreeNode
)

// NewList makes a list that shows the model.
func NewList(model *ListViewModel, opts ...ListOpts) *List { return kit.NewList(model, opts...) }

// NewListViewModel makes the model of a list.
func NewListViewModel(opts ...ListViewModelOpts) *ListViewModel { return kit.NewListViewModel(opts...) }

// NewListController makes a controller for a list.
func NewListController() *ListController { return kit.NewListController() }

// NewTree makes a tree that shows the model.
func NewTree(model *TreeModel, opts ...TreeOpts) *Tree { return kit.NewTree(model, opts...) }

// NewTreeModel makes the model of a tree.
func NewTreeModel(opts ...TreeModelOpts) *TreeModel { return kit.NewTreeModel(opts...) }

// TreeAction is a key command of a tree, and TreeResult what it did.
type (
	TreeAction = kit.TreeAction
	// TreeResult is what a TreeAction or a click did beyond changing the model.
	TreeResult = kit.TreeResult
)

// The tree actions an extension runs from outside with TreeModel.Perform.
const (
	// TreeActionCollapseAll collapses every node.
	TreeActionCollapseAll = kit.TreeActionCollapseAll
	// TreeActionExpand expands the focused node.
	TreeActionExpand = kit.TreeActionExpand
)

// NewTreeController makes a controller for a tree.
func NewTreeController() *TreeController { return kit.NewTreeController() }

// NewHighlightedLabel is the text of a row with the matches of the find highlighted.
func NewHighlightedLabel(bc widgets.BuildContext, text string, st RowState) widgets.Widget {
	return kit.NewHighlightedLabel(bc, text, st)
}

// Split views and context menus.
type (
	// SplitView lays out panes side by side or one above the other with a sash between each two that the user drags.
	SplitView = kit.SplitView
	// SplitPane is a pane of a SplitView: its widget and its starting size.
	SplitPane = kit.SplitPane
	// SplitViewOpts are the options of NewSplitView.
	SplitViewOpts = kit.SplitViewOpts
	// SplitViewController reads and changes the sizes of a SplitView.
	SplitViewController = kit.SplitViewController
	// SplitOrientation is the axis of a SplitView.
	SplitOrientation = kit.SplitOrientation
	// MenuItem is an item of a context menu.
	MenuItem = kit.MenuItem
	// ContextMenu hosts a context menu around a child; a ContextMenuController opens it at a position.
	ContextMenu = kit.ContextMenu
	// ContextMenuOpts are the options of NewContextMenu.
	ContextMenuOpts = kit.ContextMenuOpts
	// ContextMenuController shows and hides the menu of a ContextMenu.
	ContextMenuController = kit.ContextMenuController
)

// The axes of a SplitView.
const (
	// SplitVertical stacks the panes from top to bottom.
	SplitVertical = kit.SplitVertical
	// SplitHorizontal puts the panes side by side.
	SplitHorizontal = kit.SplitHorizontal
)

// NewSplitView makes a split view of panes.
func NewSplitView(panes []SplitPane, opts ...SplitViewOpts) *SplitView {
	return kit.NewSplitView(panes, opts...)
}

// NewSplitViewController makes a controller for a split view.
func NewSplitViewController() *SplitViewController { return kit.NewSplitViewController() }

// NewContextMenu makes the host of a context menu around a child.
func NewContextMenu(child widgets.Widget, opts ...ContextMenuOpts) *ContextMenu {
	return kit.NewContextMenu(child, opts...)
}

// NewContextMenuController makes a controller for a context menu.
func NewContextMenuController() *ContextMenuController { return kit.NewContextMenuController() }

// SeparatorItem is the separator for a list of menu items.
func SeparatorItem() MenuItem { return kit.SeparatorItem() }

// The suggest list (step 17d.6): the list of the editor's suggest widget (SPEC §9.12), for an extension that offers
// completions where it draws them itself, as the terminal does (SPEC §10.25 "Terminal IntelliSense").
type (
	// SuggestRow is a row of the suggest list: the label with the characters that matched, the details and the icon.
	SuggestRow = kit.SuggestRow
	// SuggestListOpts are the options of NewSuggestList: the rows shown, the focused one, the fonts and the callbacks.
	SuggestListOpts = kit.SuggestListOpts
)

// SuggestWidth is the width of the suggest list, and SuggestVerticalPadding the room it keeps from the edge of its place.
const (
	SuggestWidth           = kit.SuggestWidth
	SuggestVerticalPadding = kit.SuggestVerticalPadding
)

// NewSuggestList builds the suggest list in the colors of the theme of bc. The caller places it (a Positioned in a Stack).
func NewSuggestList(bc widgets.BuildContext, opts SuggestListOpts) widgets.Widget {
	return kit.NewSuggestList(kit.ThemeOf(bc), opts)
}

// NewSuggestMessage builds the suggest box with a message instead of rows ("No suggestions.").
func NewSuggestMessage(bc widgets.BuildContext, text string, opts SuggestListOpts) widgets.Widget {
	return kit.NewSuggestMessage(kit.ThemeOf(bc), text, opts)
}

// SuggestMatcher matches typed words against labels as the suggest widgets filter. Its zero value is ready; it keeps tables
// between calls (so score many labels with one matcher) and is for one goroutine at a time. Match(word, label) returns the score
// (higher is better) and the positions (in runes) of the characters of the label that matched; ok is false when the word does not
// match, and an empty word matches everything.
type SuggestMatcher = kit.SuggestMatcher

// NewSuggestBox puts a widget in the frame of the suggest widget (the background, border, rounded corners and shadow), for a pane
// that stands beside the list, such as its details.
func NewSuggestBox(bc widgets.BuildContext, child widgets.Widget, width, height float64) widgets.Widget {
	return kit.SuggestBox(kit.ThemeOf(bc), child, width, height)
}
