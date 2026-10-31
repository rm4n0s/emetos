package scm

import (
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The action button under the input: the primary command of the provider ("Commit", "Sync Changes 1↑ 2↓") as a wide button, with a
// dropdown arrow at its right end when the provider gave more commands.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (ActionButtonRenderer, SCMActionButton),
// src/vs/base/browser/ui/button/button.ts (ButtonWithDropdown) and src/vs/workbench/contrib/scm/browser/media/scm.css
// (.button-container)

// Metrics of the button.
const (
	// actionButtonRowHeight is the height of the row (ActionButtonRenderer.DEFAULT_HEIGHT 28 and the 8 px of the delegate).
	actionButtonRowHeight = 36.0
	// actionButtonHeight is the height of the button: a line of 18 px and 4 px of padding above and below.
	actionButtonHeight = 26.0
	actionButtonFont   = 13.0
	actionButtonLine   = 18.0
	actionButtonRadius = 4.0
	// dropdownWidth is the width of the arrow part (a 16 px icon with 4 px at each side).
	dropdownWidth = 24.0
	// rowInsetLeft and rowInsetRight are the room around the input and the button (`padding-left: 11px`).
	rowInsetLeft  = 11.0
	rowInsetRight = 12.0
)

// splitButtonController opens the dropdown of a button from outside (the proof opens it with a dev command).
type splitButtonController struct{ open func() }

// Open opens the dropdown of the button, when it is on screen and has one.
func (c *splitButtonController) Open() bool {
	if c == nil || c.open == nil {
		return false
	}
	c.open()
	return true
}

// splitButtonOpts are the options of a split button.
type splitButtonOpts struct {
	// Label is the text, with "$(icon)" Codicons in it.
	Label string
	// Tooltip is the text of the hover.
	Tooltip string
	// Disabled dims the button to 40 % and makes it ignore the pointer and the keyboard.
	Disabled bool
	// Dropdown shows the arrow part.
	Dropdown bool
	// OnPress runs the primary command.
	OnPress func()
	// OnDropdown opens the dropdown below the left end of the button; at is that place in window coordinates.
	OnDropdown func(at ui.Offset)
	// Controller lets the owner open the dropdown.
	Controller *splitButtonController
	// FocusNode is the focus node of the primary part, which the owner keeps between builds.
	FocusNode *widgets.FocusNode
}

// splitButton is the button of the action button row.
type splitButton struct {
	widgets.StatefulWidget
	opts splitButtonOpts
}

func newSplitButton(opts splitButtonOpts) *splitButton {
	return &splitButton{StatefulWidget: widgets.NewStatefulWidgetBase(), opts: opts}
}

// CreateState implements widgets.StatefulWidget.
func (b *splitButton) CreateState() widgets.State { return &splitButtonState{} }

type splitButtonState struct {
	widgets.StateOf[*splitButton]
	hover, hoverDrop, focus bool
}

func (s *splitButtonState) InitState() { s.attach() }

func (s *splitButtonState) attach() {
	if c := s.Widget().opts.Controller; c != nil {
		c.open = s.openDropdown
	}
}

func (s *splitButtonState) DidUpdateWidget(old *splitButton) {
	if old.opts.Controller != nil && old.opts.Controller != s.Widget().opts.Controller {
		old.opts.Controller.open = nil
	}
	s.attach()
}

func (s *splitButtonState) Dispose() {
	if c := s.Widget().opts.Controller; c != nil {
		c.open = nil
	}
}

func (s *splitButtonState) set(f func()) {
	if s.Mounted() {
		s.SetState(f)
	}
}

func (s *splitButtonState) enabled() bool { return !s.Widget().opts.Disabled }

// origin is the place of the bottom left corner of the button in window coordinates.
func (s *splitButtonState) origin() ui.Offset {
	box, ok := s.Context().FindRenderObject().(rendering.Box)
	if !ok || box == nil || !box.HasSize() {
		return ui.Offset{}
	}
	size := box.Size()
	return box.LocalToGlobal(ui.Offset{Dx: 0, Dy: size.Height}, nil)
}

func (s *splitButtonState) openDropdown() {
	o := s.Widget().opts
	if o.Dropdown && s.enabled() && o.OnDropdown != nil {
		o.OnDropdown(s.origin())
	}
}

func (s *splitButtonState) press() {
	if o := s.Widget().opts; s.enabled() && o.OnPress != nil {
		o.OnPress()
	}
}

func (s *splitButtonState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	if _, ok := e.(*services.KeyDownEvent); !ok || !s.enabled() {
		return widgets.KeyEventResultIgnored
	}
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter, services.LogicalKeyboardKeySpace:
		s.press()
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyArrowDown:
		if s.Widget().opts.Dropdown {
			s.openDropdown()
			return widgets.KeyEventResultHandled
		}
	}
	return widgets.KeyEventResultIgnored
}

// label builds the text of the button with its Codicons.
func (s *splitButtonState) label(bc widgets.BuildContext, fg ui.Color) widgets.Widget {
	style := painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: kit.UIFontOf(bc), FontFamilyFallback: []string{"sans-serif"}, FontSize: foundation.Some(actionButtonFont),
		Height: foundation.Some(actionButtonLine / actionButtonFont), Color: foundation.Some(fg),
	})
	var cells []widgets.Widget
	// the white space around the runs is the gap between the icons and the text
	for _, run := range workbench.ParseStatusText(s.Widget().opts.Label) {
		switch {
		case run.Icon != "":
			if code, ok := fonts.CodiconByName(run.Icon); ok {
				cells = append(cells, widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(14.0), Color: foundation.Some(fg)}))
			}
		case strings.TrimSpace(run.Text) != "":
			if len(cells) > 0 {
				cells = append(cells, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}))
			}
			cells = append(cells, widgets.NewFlexible(widgets.NewText(strings.TrimSpace(run.Text), widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})))
		}
	}
	return widgets.NewRow(cells, widgets.RowOpts{
		MainAxisSize: rendering.MainAxisSizeMin, MainAxisAlignment: rendering.MainAxisAlignmentCenter, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// Build implements widgets.State.
func (s *splitButtonState) Build(bc widgets.BuildContext) widgets.Widget {
	o := s.Widget().opts
	colors := kit.ThemeOf(bc)
	enabled := s.enabled()
	fg, bg, hover, border := colors.ButtonForeground, colors.ButtonBackground, colors.ButtonHoverBackground, colors.ButtonBorder
	primaryBg, dropBg := bg, bg
	if enabled && (s.hover || s.focus) {
		primaryBg = hover
	}
	if enabled && s.hoverDrop {
		dropBg = hover
	}
	round := ui.NewRadiusCircular(actionButtonRadius)
	left, right := round, round
	if o.Dropdown {
		right = ui.Radius{}
	}
	side := painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)})
	primary := widgets.NewContainer(
		widgets.NewCenter(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 4), s.label(bc, fg))),
		widgets.ContainerOpts{Height: foundation.Some(actionButtonHeight), Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color: foundation.Some(primaryBg), Border: side, BorderRadius: painting.NewBorderRadiusHorizontal(left, right)})})
	var body widgets.Widget = widgets.NewMouseRegion(
		widgets.NewGestureDetector(primary, widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque, OnTap: s.press,
			OnTapDown: func(gestures.TapDownDetails) {
				if s.enabled() && o.FocusNode != nil {
					o.FocusNode.RequestFocus()
				}
			}}),
		widgets.MouseRegionOpts{
			Cursor:  cursorFor(enabled),
			OnEnter: func(*gestures.PointerEnterEvent) { s.set(func() { s.hover = true }) },
			OnExit:  func(*gestures.PointerExitEvent) { s.set(func() { s.hover = false }) },
		})
	body = widgets.NewFocus(body, widgets.FocusOpts{
		FocusNode: o.FocusNode, OnKeyEvent: s.handleKey, CanRequestFocus: foundation.Some(enabled),
		OnFocusChange: func(has bool) { s.set(func() { s.focus = has }) },
	})
	cells := []widgets.Widget{widgets.NewExpanded(body)}
	if o.Dropdown {
		sep, ok := colors.Named("button.separator")
		if !ok {
			sep = fg.WithOpacity(0.4)
		}
		arrow := widgets.NewContainer(
			widgets.NewCenter(widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconChevronDown), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(fg)})),
			widgets.ContainerOpts{Width: foundation.Some(dropdownWidth), Height: foundation.Some(actionButtonHeight),
				Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
					Color: foundation.Some(dropBg), Border: side, BorderRadius: painting.NewBorderRadiusHorizontal(ui.Radius{}, round)})})
		cells = append(cells,
			widgets.NewSizedBox(widgets.NewColoredBox(sep, nil), widgets.SizedBoxOpts{Width: foundation.Some(1.0), Height: foundation.Some(actionButtonHeight)}),
			widgets.NewMouseRegion(widgets.NewGestureDetector(arrow, widgets.GestureDetectorOpts{
				Behavior: rendering.HitTestBehaviorOpaque, OnTap: s.openDropdown}),
				widgets.MouseRegionOpts{
					Cursor:  cursorFor(enabled),
					OnEnter: func(*gestures.PointerEnterEvent) { s.set(func() { s.hoverDrop = true }) },
					OnExit:  func(*gestures.PointerExitEvent) { s.set(func() { s.hoverDrop = false }) },
				}))
	}
	var w widgets.Widget = widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	if o.Tooltip != "" {
		w = kit.NewHover(w, kit.HoverOpts{Text: o.Tooltip, Compact: true})
	}
	if !enabled {
		w = widgets.NewOpacity(0.4, w)
	}
	return w
}

func cursorFor(enabled bool) services.MouseCursor {
	if enabled {
		return services.SystemMouseCursors.Click
	}
	return services.SystemMouseCursors.Basic
}
