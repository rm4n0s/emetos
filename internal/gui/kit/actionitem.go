package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of a toolbar action (`.monaco-action-bar .action-label`).
const (
	actionLabelFontSize = 11.0
	actionLabelPadding  = 3.0
)

// isActivateKey says whether a key activates a button or an action: Enter, the numpad's Enter and Space.
func isActivateKey(k services.LogicalKeyboardKey) bool {
	return k == services.LogicalKeyboardKeyEnter || k == services.LogicalKeyboardKeyNumpadEnter || k == services.LogicalKeyboardKeySpace
}

// actionItem is the look and the pointer and keyboard handling of a toolbar action: an icon or a
// text label that has the toolbar's background under the pointer, a darker one while pressed, the
// toggled one while checked and a focus outline. IconButton is one on its own and an ActionBar
// is made of them.
type actionItem struct {
	widgets.StatefulWidget
	p actionItemProps
}

type actionItemProps struct {
	// icon is the Codicon of the action; with none the action shows label.
	icon  int
	label string
	// tooltip is the text of the hover, if any.
	tooltip string
	checked bool
	// disabled actions do not run; focusable says whether they can still take the focus.
	disabled, focusable bool
	// disabledAlpha is the alpha of an icon (or the color of a label) that is disabled.
	disabledAlpha int
	// alignLeft puts a label at the left edge (vertical bars); else it is centered.
	alignLeft bool
	// run is what the action does: a click, or Enter and Space when keys is true.
	run  func()
	keys bool
	// node is the focus node of the item; the item makes its own when nil. skipTraversal takes the
	// item out of the tab order.
	node          *widgets.FocusNode
	autofocus     bool
	skipTraversal bool
	onFocusChange func(bool)
	delegate      *HoverDelegate
	hover         *HoverController
}

func newActionItem(p actionItemProps) *actionItem {
	return &actionItem{StatefulWidget: widgets.NewStatefulWidgetBase(), p: p}
}

func (a *actionItem) CreateState() widgets.State { return &actionItemState{} }

type actionItemState struct {
	widgets.StateOf[*actionItem]
	node                  *widgets.FocusNode
	ownNode               bool
	hover, pressed, focus bool
}

func (s *actionItemState) InitState() {
	s.pickNode()
}

func (s *actionItemState) pickNode() {
	if n := s.Widget().p.node; n != nil {
		s.node, s.ownNode = n, false
		return
	}
	s.node, s.ownNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "ActionItem"}), true
}

func (s *actionItemState) DidUpdateWidget(old *actionItem) {
	if old.p.node != s.Widget().p.node {
		if s.ownNode {
			s.node.Dispose()
		}
		s.pickNode()
	}
}

func (s *actionItemState) Dispose() {
	if s.ownNode {
		s.node.Dispose()
	}
}

func (s *actionItemState) enabled() bool {
	p := s.Widget().p
	return !p.disabled && p.run != nil
}

func (s *actionItemState) set(f func()) {
	if s.Mounted() {
		s.SetState(f)
	}
}

func (s *actionItemState) run() {
	if s.enabled() {
		s.Widget().p.run()
	}
}

func (s *actionItemState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	if _, ok := e.(*services.KeyDownEvent); ok && isActivateKey(e.LogicalKey()) && s.Widget().p.keys {
		s.run()
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

// Build implements widgets.State.
func (s *actionItemState) Build(bc widgets.BuildContext) widgets.Widget {
	p := s.Widget().p
	colors := ThemeOf(bc)
	enabled := s.enabled()

	var bg ui.Color
	switch {
	case p.checked:
		bg = colors.ActionBarToggledBackground
	case s.pressed && enabled:
		bg = colors.ToolbarActiveBackground
	case s.hover && enabled:
		bg = colors.ToolbarHoverBackground
	}
	outline := ui.Color{}
	if s.focus {
		outline = colors.FocusBorder
	}
	decoration := painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Color:        foundation.Some(bg),
		Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(outline), Width: foundation.Some(1.0)}),
		BorderRadius: painting.NewBorderRadiusCircular(iconButtonRadius),
	})

	var box widgets.Widget
	if p.icon != 0 {
		color := colors.IconForeground
		if !enabled {
			color = color.WithAlpha(p.disabledAlpha)
		}
		box = widgets.NewContainer(
			widgets.NewIcon(fonts.CodiconIcon(p.icon), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(color)}),
			widgets.ContainerOpts{Width: foundation.Some(iconButtonSize), Height: foundation.Some(iconButtonSize), Alignment: painting.AlignmentCenter, Decoration: decoration})
	} else {
		color := colors.Foreground
		if !enabled {
			color = colors.DisabledForeground
		}
		alignment := painting.AlignmentGeometry(painting.AlignmentCenter)
		if p.alignLeft {
			alignment = painting.AlignmentCenterLeft
		}
		box = widgets.NewContainer(
			widgets.NewPadding(painting.NewEdgeInsetsAll(actionLabelPadding-1),
				widgets.NewText(p.label, widgets.TextOpts{Style: TextStyleOf(bc, actionLabelFontSize, color), MaxLines: foundation.Some(1)})),
			widgets.ContainerOpts{Height: foundation.Some(iconButtonSize), Alignment: alignment, Decoration: decoration})
	}

	cursor := services.SystemMouseCursors.Basic
	if enabled {
		cursor = services.SystemMouseCursors.Click
	}
	var body widgets.Widget = widgets.NewMouseRegion(
		widgets.NewGestureDetector(box, widgets.GestureDetectorOpts{
			Behavior:    rendering.HitTestBehaviorOpaque,
			OnTapDown:   func(gestures.TapDownDetails) { s.set(func() { s.pressed = true }) },
			OnTapUp:     func(gestures.TapUpDetails) { s.set(func() { s.pressed = false }) },
			OnTapCancel: func() { s.set(func() { s.pressed = false }) },
			OnTap:       s.run,
		}),
		widgets.MouseRegionOpts{
			Cursor:  cursor,
			OnEnter: func(*gestures.PointerEnterEvent) { s.set(func() { s.hover = true }) },
			OnExit:  func(*gestures.PointerExitEvent) { s.set(func() { s.hover = false }) },
		})
	body = widgets.NewFocus(body, widgets.FocusOpts{
		FocusNode: s.node, Autofocus: p.autofocus, OnKeyEvent: s.handleKey,
		CanRequestFocus: foundation.Some(p.focusable),
		SkipTraversal:   foundation.Some(p.skipTraversal),
		OnFocusChange: func(has bool) {
			s.set(func() { s.focus = has })
			if p.onFocusChange != nil {
				p.onFocusChange(has)
			}
		},
	})
	if p.tooltip != "" {
		body = NewHover(body, HoverOpts{Text: p.tooltip, Compact: true, Delegate: p.delegate, Controller: p.hover})
	}
	return body
}
