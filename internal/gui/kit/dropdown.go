package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of VS Code's select box (`.monaco-select-box`).
const (
	dropdownHeight   = 22.0
	dropdownMinWidth = 100.0
	dropdownRadius   = 4.0
	dropdownPadLeft  = 8.0
	dropdownFontSize = 13.0
	// dropdownDefaultRows is the number of options in view in the list; more scroll.
	dropdownDefaultRows = 10
)

// DropdownOption is one choice of a Dropdown.
type DropdownOption struct {
	// Label is the text of the option, shown in the field while it is the selected one.
	Label string
	// Description is shown after the label in the list, in a dimmer color (VS Code's option detail).
	Description string
	// Disabled options are shown dimmed in the list and cannot be chosen.
	Disabled bool
}

// DropdownOpts are the options of NewDropdown.
type DropdownOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Selected is the index of the selected option; an index outside the options shows the
	// placeholder.
	Selected int
	// OnChanged is called with the index of the option the user chose, when it is not the selected
	// one. The dropdown does not change its own selection: the caller passes the new Selected.
	OnChanged func(index int)
	// Disabled dims the field, makes it ignore the pointer and the keyboard and takes it out of the
	// focus order.
	Disabled bool
	// Placeholder is shown while no option is selected.
	Placeholder string
	// Width, when set, is the width of the field; otherwise it fills the width it is given (at least
	// 100 px, as VS Code's select box).
	Width foundation.Opt[float64]
	// Rows is the number of options in view in the list; the default is 10. More scroll.
	Rows int
	// FocusNode is the focus node of the field, which the caller owns; without one the dropdown makes
	// its own.
	FocusNode *widgets.FocusNode
	// Autofocus asks for the focus when the dropdown is shown.
	Autofocus bool
	// OnFocusChange is called when the field gets or loses the focus.
	OnFocusChange func(focused bool)
}

// Dropdown is a select box: a field that shows the selected option and a chevron and opens a list of
// the options below it (above it where there is no room), as wide as the field. The field takes the
// focus on a click. Closed, Enter, Space, Up and Down open the list and typing a letter selects the
// next option that starts with it; open, Up, Down, PageUp, PageDown, Home and End move the focus,
// typing a letter moves it to the next option that starts with it, Enter chooses the option (a click
// does too) and Escape closes the list and gives the focus back to the field.
//
// Ported from VS Code: src/vs/base/browser/ui/selectBox/selectBox.ts, selectBoxCustom.ts and
// selectBoxNative.ts (the field and its keys), selectBox.css and the colors dropdown.* of
// src/vs/platform/theme/common/colors/inputColors.ts. Not ported: separators, the details pane and
// the native select.
type Dropdown struct {
	widgets.StatefulWidget
	options []DropdownOption
	opts    DropdownOpts
}

// NewDropdown makes a dropdown of options.
func NewDropdown(options []DropdownOption, opts ...DropdownOpts) *Dropdown {
	d := &Dropdown{options: options}
	if len(opts) > 0 {
		d.opts = opts[0]
	}
	d.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: d.opts.Key})
	return d
}

// Options are the options of the dropdown.
func (d *Dropdown) Options() []DropdownOption { return d.options }

// Selected is the index of the selected option.
func (d *Dropdown) Selected() int { return d.opts.Selected }

// CreateState implements widgets.StatefulWidget.
func (d *Dropdown) CreateState() widgets.State {
	return &dropdownState{portal: widgets.NewOverlayPortalController(widgets.OverlayPortalControllerOpts{DebugLabel: "Dropdown"})}
}

type dropdownState struct {
	widgets.StateOf[*Dropdown]
	node    *widgets.FocusNode
	ownNode bool
	// wrap is the focus node around the list: it has the focus while the list does.
	wrap   *widgets.FocusNode
	portal *widgets.OverlayPortalController
	open   bool
	focus  bool
	// generation tells one opening of the list from the next, to give each its own list.
	generation int
}

// InitState makes the focus nodes.
func (s *dropdownState) InitState() {
	s.pickNode()
	s.wrap = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "DropdownList", CannotRequestFocus: true, SkipTraversal: true})
}

func (s *dropdownState) pickNode() {
	if n := s.Widget().opts.FocusNode; n != nil {
		s.node, s.ownNode = n, false
		return
	}
	s.node, s.ownNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Dropdown"}), true
}

// DidUpdateWidget follows a new focus node and closes the list of a dropdown that was disabled.
func (s *dropdownState) DidUpdateWidget(old *Dropdown) {
	if old.opts.FocusNode != s.Widget().opts.FocusNode {
		if s.ownNode {
			s.node.Dispose()
		}
		s.pickNode()
	}
}

// Dispose releases the focus nodes.
func (s *dropdownState) Dispose() {
	if s.ownNode {
		s.node.Dispose()
	}
	s.wrap.Dispose()
}

func (s *dropdownState) enabled() bool {
	o := s.Widget()
	return !o.opts.Disabled && len(o.options) > 0
}

func (s *dropdownState) set(f func()) {
	if s.Mounted() {
		s.SetState(f)
	}
}

// IsOpen says whether the list is open, for tests.
func (s *dropdownState) IsOpen() bool { return s.open }

func (s *dropdownState) openList() {
	if s.open || !s.enabled() {
		return
	}
	s.generation++
	s.set(func() { s.open = true })
	s.portal.Show()
}

// closeList closes the list; with refocus the field gets the focus back if the list had it.
func (s *dropdownState) closeList(refocus bool) {
	if !s.open {
		return
	}
	hadFocus := s.wrap.HasFocus()
	s.set(func() { s.open = false })
	s.portal.Hide()
	if refocus && hadFocus {
		s.node.RequestFocus()
	}
}

func (s *dropdownState) choose(index int) {
	o := s.Widget()
	s.closeList(true)
	if index != o.opts.Selected && index >= 0 && index < len(o.options) && !o.options[index].Disabled && o.opts.OnChanged != nil {
		o.opts.OnChanged(index)
	}
}

// typeAhead selects the next option that starts with the typed text, without opening the list.
func (s *dropdownState) typeAhead(text string) bool {
	o := s.Widget()
	labels := make([]string, len(o.options))
	disabled := make([]bool, len(o.options))
	for i, opt := range o.options {
		labels[i], disabled[i] = opt.Label, opt.Disabled
	}
	i := nextWithPrefix(labels, disabled, o.opts.Selected, text)
	if i < 0 || i == o.opts.Selected {
		return false
	}
	if o.opts.OnChanged != nil {
		o.opts.OnChanged(i)
	}
	return true
}

func (s *dropdownState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	if !s.enabled() {
		return widgets.KeyEventResultIgnored
	}
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	switch k := e.LogicalKey(); {
	case isActivateKey(k) || k == services.LogicalKeyboardKeyArrowDown || k == services.LogicalKeyboardKeyArrowUp:
		if _, repeat := e.(*services.KeyRepeatEvent); repeat {
			return widgets.KeyEventResultHandled
		}
		s.openList()
		return widgets.KeyEventResultHandled
	default:
		ch := e.Character()
		if ch == "" || kb.IsControlPressed() || kb.IsAltPressed() || kb.IsMetaPressed() {
			return widgets.KeyEventResultIgnored
		}
		if s.typeAhead(ch) {
			return widgets.KeyEventResultHandled
		}
	}
	return widgets.KeyEventResultIgnored
}

// Build implements widgets.State.
func (s *dropdownState) Build(bc widgets.BuildContext) widgets.Widget {
	d := s.Widget()
	o := d.opts
	colors := ThemeOf(bc)
	enabled := s.enabled()
	border := colors.DropdownBorder
	if (s.focus || s.open) && enabled {
		border = colors.FocusBorder
	}
	label, fg := o.Placeholder, colors.DescriptionForeground
	if o.Selected >= 0 && o.Selected < len(d.options) {
		label, fg = d.options[o.Selected].Label, colors.DropdownForeground
	}
	style := TextStyleOf(bc, dropdownFontSize, fg)
	chevron := widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconChevronDown), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(colors.DropdownForeground)})
	field := widgets.NewContainer(
		widgets.NewRow([]widgets.Widget{
			widgets.NewExpanded(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(dropdownPadLeft, 0, 4, 0),
				widgets.NewText(label, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis}))),
			chevron,
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(3.0)}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.ContainerOpts{
			Width: o.Width, Height: foundation.Some(dropdownHeight),
			Constraints: foundation.Some(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinWidth: foundation.Some(dropdownMinWidth)})),
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color:        foundation.Some(colors.DropdownBackground),
				Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)}),
				BorderRadius: painting.NewBorderRadiusCircular(dropdownRadius),
			}),
		})
	cursor := services.SystemMouseCursors.Basic
	if enabled {
		cursor = services.SystemMouseCursors.Click
	}
	var body widgets.Widget = widgets.NewMouseRegion(
		widgets.NewGestureDetector(field, widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque,
			OnTap: func() {
				if !s.enabled() {
					return
				}
				if s.open {
					s.closeList(true)
					return
				}
				s.node.RequestFocus()
				s.openList()
			},
		}),
		widgets.MouseRegionOpts{Cursor: cursor})
	body = widgets.NewFocus(body, widgets.FocusOpts{
		FocusNode: s.node, Autofocus: o.Autofocus && enabled, OnKeyEvent: s.handleKey,
		CanRequestFocus: foundation.Some(enabled),
		OnFocusChange: func(has bool) {
			s.set(func() { s.focus = has })
			if o.OnFocusChange != nil {
				o.OnFocusChange(has)
			}
		},
	})
	// a click on the field is inside the list's group of tap regions: the field closes the list itself
	body = widgets.NewTapRegion(body, widgets.TapRegionOpts{GroupID: s})
	body = widgets.NewOverlayPortalOverlayChildLayoutBuilder(s.portal, s.buildList, body)
	if !enabled {
		body = widgets.NewOpacity(toggleDisabledAlpha, body)
	}
	return body
}

// buildList is the list of options, below the field or above it where it does not fit below.
func (s *dropdownState) buildList(bc widgets.BuildContext, info widgets.OverlayChildLayoutInfo) widgets.Widget {
	d := s.Widget()
	if !s.open {
		return widgets.NewSizedBox(nil)
	}
	colors := ThemeOf(bc)
	items := make([]ListItem, len(d.options))
	for i, opt := range d.options {
		items[i] = ListItem{Label: opt.Label, Description: opt.Description, Disabled: opt.Disabled}
	}
	rows := d.opts.Rows
	if rows <= 0 {
		rows = dropdownDefaultRows
	}
	rows = min(rows, len(items))
	height := float64(rows)*listRowHeight + 2
	origin := info.ChildPaintTransform.TransformPoint(ui.Offset{})
	top := origin.Dy + info.ChildSize.Height
	if top+height > info.OverlaySize.Height && origin.Dy-height >= 0 {
		top = origin.Dy - height
	}
	background := colors.DropdownListBackground
	if background.Alpha() == 0 {
		background = colors.DropdownBackground
	}
	focused := foundation.None[int]()
	if d.opts.Selected >= 0 && d.opts.Selected < len(items) && !items[d.opts.Selected].Disabled {
		focused = foundation.Some(d.opts.Selected)
	}
	list := NewListOverlay(items, ListOverlayOpts{
		Key: foundation.NewValueKey(s.generation), TapGroup: s, TypeAhead: true, Rows: rows,
		Width: foundation.Some(info.ChildSize.Width), Focused: focused,
		Background: foundation.Some(background), Foreground: foundation.Some(colors.DropdownForeground),
		StealFocus: true,
		OnSelect:   s.choose,
		OnDismiss:  func() { s.closeList(true) },
	})
	wrapped := widgets.NewFocusWithExternalFocusNode(s.wrap, list)
	return widgets.NewStack([]widgets.Widget{
		widgets.NewPositioned(wrapped, widgets.PositionedOpts{Left: foundation.Some(origin.Dx), Top: foundation.Some(top)}),
	}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}
