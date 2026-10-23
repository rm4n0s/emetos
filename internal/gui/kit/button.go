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

// Metrics of VS Code's text button (`.monaco-text-button`).
const (
	buttonFontSize       = 12.0
	buttonLineHeight     = 16.0
	buttonPadV           = 4.0
	buttonPadH           = 8.0
	buttonRadius         = 4.0
	buttonSmallFontSize  = 11.0
	buttonSmallLineH     = 14.0
	buttonSmallPadV      = 3.0
	buttonSmallPadH      = 6.0
	buttonIconGap        = 4.8 // the codicon's 0.2em margin on both sides at 12 px
	buttonDisabledAlpha  = 0.4
	buttonFocusOffset    = 2.0
	buttonFocusRingWidth = 1.0
)

// ButtonOpts are the options of NewButton.
type ButtonOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Secondary makes a secondary button (button.secondary* colors) instead of a primary one.
	Secondary bool
	// Small makes the small button: 11 px text and 3 px by 6 px of padding.
	Small bool
	// Icon is a Codicon shown before the label; 0 for none.
	Icon int
	// Disabled dims the button to 40 % and makes it ignore the pointer and the keyboard. A button
	// without OnPressed is disabled.
	Disabled bool
	// Expand makes the button as wide as the space it is given (VS Code's `width: 100%`); else it is as
	// wide as its content (`fit-content`).
	Expand bool
	// Autofocus asks for the focus when the button is shown.
	Autofocus bool
	// FocusNode is the focus node of the button, which the caller owns; without one the button
	// makes its own.
	FocusNode *widgets.FocusNode
	// Tooltip is the text of the hover that shows when the pointer rests on the button.
	Tooltip string
	// HoverDelegate is the group of hovers the tooltip belongs to; the default is a group of its own.
	HoverDelegate *HoverDelegate
	// OnPressed is called when the button is clicked, or when Enter or Space is pressed while it has
	// the focus.
	OnPressed func()
	// OnEscape is called when Escape is pressed while the button has the focus; the button gives up
	// the focus first, so the callback can move it elsewhere.
	OnEscape func()
	// OnFocusChange is called when the button gets or loses the focus.
	OnFocusChange func(focused bool)
}

// Button is VS Code's text button: a label, with an optional Codicon before it, on the
// button.background color (button.hoverBackground under the pointer and while it has the focus) in
// the primary form, or on the button.secondary* colors in the secondary one. It has a border in
// button.border, a focus outline in focusBorder 2 px outside it, and a keyboard: Enter and Space
// press it and Escape gives the focus up. A click gives it the focus. Disabled it is at 40 %.
//
// Ported from VS Code: src/vs/base/browser/ui/button/button.ts and button.css
type Button struct {
	widgets.StatefulWidget
	label string
	opts  ButtonOpts
}

// NewButton makes a button with a label.
func NewButton(label string, opts ...ButtonOpts) *Button {
	b := &Button{label: label}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	b.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: b.opts.Key})
	return b
}

// Label is the text of the button.
func (b *Button) Label() string { return b.label }

// Secondary says whether this is a secondary button.
func (b *Button) Secondary() bool { return b.opts.Secondary }

// CreateState implements widgets.StatefulWidget.
func (b *Button) CreateState() widgets.State { return &buttonState{} }

type buttonState struct {
	widgets.StateOf[*Button]
	node         *widgets.FocusNode
	ownNode      bool
	hover, focus bool
}

// InitState makes the focus node.
func (s *buttonState) InitState() { s.pickNode() }

func (s *buttonState) pickNode() {
	if n := s.Widget().opts.FocusNode; n != nil {
		s.node, s.ownNode = n, false
		return
	}
	s.node, s.ownNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Button"}), true
}

// DidUpdateWidget follows a new focus node.
func (s *buttonState) DidUpdateWidget(old *Button) {
	if old.opts.FocusNode != s.Widget().opts.FocusNode {
		if s.ownNode {
			s.node.Dispose()
		}
		s.pickNode()
	}
}

// Dispose releases the focus node.
func (s *buttonState) Dispose() {
	if s.ownNode {
		s.node.Dispose()
	}
}

func (s *buttonState) enabled() bool {
	o := s.Widget().opts
	return o.OnPressed != nil && !o.Disabled
}

func (s *buttonState) set(f func()) {
	if s.Mounted() {
		s.SetState(f)
	}
}

func (s *buttonState) press() {
	if s.enabled() {
		s.Widget().opts.OnPressed()
	}
}

func (s *buttonState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	if _, ok := e.(*services.KeyDownEvent); !ok {
		return widgets.KeyEventResultIgnored
	}
	switch {
	case isActivateKey(e.LogicalKey()) && s.enabled():
		s.press()
		return widgets.KeyEventResultHandled
	case e.LogicalKey() == services.LogicalKeyboardKeyEscape:
		// the focus goes first, so that a callback that moves it elsewhere wins
		s.node.Unfocus()
		if f := s.Widget().opts.OnEscape; f != nil {
			f()
		}
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

// Build implements widgets.State.
func (s *buttonState) Build(bc widgets.BuildContext) widgets.Widget {
	b := s.Widget()
	o := b.opts
	colors := ThemeOf(bc)
	enabled := s.enabled()

	fg, bg, hoverBg, border := colors.ButtonForeground, colors.ButtonBackground, colors.ButtonHoverBackground, colors.ButtonBorder
	if o.Secondary {
		fg, bg, hoverBg, border = colors.ButtonSecondaryForeground, colors.ButtonSecondaryBackground, colors.ButtonSecondaryHoverBackground, colors.ButtonSecondaryBorder
	}
	// the button also takes the hover color while it has the focus
	if (s.hover || s.focus) && enabled {
		bg = hoverBg
	}

	fontSize, lineH, padV, padH := buttonFontSize, buttonLineHeight, buttonPadV, buttonPadH
	if o.Small {
		fontSize, lineH, padV, padH = buttonSmallFontSize, buttonSmallLineH, buttonSmallPadV, buttonSmallPadH
	}
	style := painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily:         UIFontOf(bc),
		FontFamilyFallback: []string{"sans-serif"},
		FontSize:           foundation.Some(fontSize),
		Height:             foundation.Some(lineH / fontSize),
		Color:              foundation.Some(fg),
	})
	var parts []widgets.Widget
	if o.Icon != 0 {
		parts = append(parts, widgets.NewIcon(fonts.CodiconIcon(o.Icon), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(fg)}))
		if b.label != "" {
			parts = append(parts, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(buttonIconGap)}))
		}
	}
	if b.label != "" {
		parts = append(parts, widgets.NewText(b.label, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis}))
	}
	axis := rendering.MainAxisSizeMin
	if o.Expand {
		axis = rendering.MainAxisSizeMax
	}
	content := widgets.NewRow(parts, widgets.RowOpts{
		MainAxisSize: axis, MainAxisAlignment: rendering.MainAxisAlignmentCenter, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter,
	})
	face := widgets.NewContainer(
		widgets.NewPadding(painting.NewEdgeInsetsSymmetric(padV, padH), content),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(bg),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(buttonRadius),
		})})

	children := []widgets.Widget{face}
	if s.focus {
		// the outline is drawn outside the button, 2 px away from it, as `outline-offset: 2px`
		inset := -(buttonFocusOffset + buttonFocusRingWidth)
		ring := widgets.NewPositioned(
			widgets.NewIgnorePointer(widgets.NewDecoratedBox(painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.FocusBorder), Width: foundation.Some(buttonFocusRingWidth)}),
				BorderRadius: painting.NewBorderRadiusCircular(buttonRadius + buttonFocusOffset + buttonFocusRingWidth),
			}), nil)),
			widgets.PositionedOpts{Left: foundation.Some(inset), Top: foundation.Some(inset), Right: foundation.Some(inset), Bottom: foundation.Some(inset)})
		children = append(children, ring)
	}
	var body widgets.Widget = widgets.NewStack(children, widgets.StackOpts{ClipBehavior: ui.ClipNone, Fit: rendering.StackFitLoose})

	cursor := services.SystemMouseCursors.Basic
	if enabled {
		cursor = services.SystemMouseCursors.Click
	}
	body = widgets.NewMouseRegion(
		widgets.NewGestureDetector(body, widgets.GestureDetectorOpts{
			Behavior:  rendering.HitTestBehaviorOpaque,
			OnTapDown: func(gestures.TapDownDetails) { s.takeFocus() },
			OnTap:     s.press,
		}),
		widgets.MouseRegionOpts{
			Cursor:  cursor,
			OnEnter: func(*gestures.PointerEnterEvent) { s.set(func() { s.hover = true }) },
			OnExit:  func(*gestures.PointerExitEvent) { s.set(func() { s.hover = false }) },
		})
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
	if o.Tooltip != "" {
		body = NewHover(body, HoverOpts{Text: o.Tooltip, Compact: true, Delegate: o.HoverDelegate})
	}
	if !enabled {
		body = widgets.NewOpacity(buttonDisabledAlpha, body)
	}
	return body
}

// takeFocus gives the focus to the button, as a click on a focusable element does.
func (s *buttonState) takeFocus() {
	if s.enabled() {
		s.node.RequestFocus()
	}
}
