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
