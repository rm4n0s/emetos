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

// toggleDisabledAlpha is the opacity of a disabled toggle (`.monaco-custom-toggle.disabled`).
const toggleDisabledAlpha = 0.4

// ToggleOpts are the options of NewToggle.
type ToggleOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Checked says whether the toggle is on.
	Checked bool
	// OnChanged is called with the new value when the toggle is clicked, or when Space or Enter is
	// pressed while it has the focus. A toggle without it ignores the pointer and the keyboard.
	OnChanged func(checked bool)
	// Disabled dims the toggle to 40 %, makes it ignore the pointer and the keyboard and takes it out of
	// the focus order.
	Disabled bool
	// Tooltip is the text of the hover that shows when the pointer rests on the toggle.
	Tooltip string
	// NotFocusable keeps the toggle out of the focus order (VS Code's `notFocusable`, which the toggles
	// of an action bar and of the find widget use: they never take the focus from the input).
	NotFocusable bool
	// FocusNode is the focus node of the toggle, which the caller owns; without one the toggle makes
	// its own.
	FocusNode *widgets.FocusNode
	// Autofocus asks for the focus when the toggle is shown.
	Autofocus bool
	// OnFocusChange is called when the toggle gets or loses the focus.
	OnFocusChange func(focused bool)
	// HoverDelegate is the group of hovers the tooltip belongs to; the default is a group of its own.
	HoverDelegate *HoverDelegate
}

// Toggle is VS Code's icon toggle (`monaco-custom-toggle`), such as the match case switch of
// the find widget: a Codicon in a 20 px box that has a border and a tinted background while it is
// on (inputOption.active*), the hover background under the pointer, a tooltip and a focus ring
// (focusBorder; VS Code draws it dashed) while it has the focus. Space and Enter flip it; a click flips
// it too but does not move the focus to it, so that the input next to it keeps it.
//
// Ported from VS Code: src/vs/base/browser/ui/toggle/toggle.ts (Toggle) and toggle.css
type Toggle struct {
	widgets.StatefulWidget
	code int
	opts ToggleOpts
}

// NewToggle makes a toggle that shows the Codicon of a code point.
func NewToggle(code int, opts ...ToggleOpts) *Toggle {
	t := &Toggle{code: code}
	if len(opts) > 0 {
		t.opts = opts[0]
	}
	t.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: t.opts.Key})
	return t
}

// Code is the code point of the Codicon the toggle shows.
func (t *Toggle) Code() int { return t.code }

// Checked says whether the toggle is on.
func (t *Toggle) Checked() bool { return t.opts.Checked }

// CreateState implements widgets.StatefulWidget.
func (t *Toggle) CreateState() widgets.State { return &toggleState{} }

type toggleState struct {
	widgets.StateOf[*Toggle]
	node         *widgets.FocusNode
	ownNode      bool
	hover, focus bool
}

// InitState makes the focus node.
func (s *toggleState) InitState() { s.pickNode() }

func (s *toggleState) pickNode() {
	if n := s.Widget().opts.FocusNode; n != nil {
		s.node, s.ownNode = n, false
		return
	}
	s.node, s.ownNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Toggle"}), true
}

// DidUpdateWidget follows a new focus node.
func (s *toggleState) DidUpdateWidget(old *Toggle) {
	if old.opts.FocusNode != s.Widget().opts.FocusNode {
		if s.ownNode {
			s.node.Dispose()
		}
		s.pickNode()
	}
}

// Dispose releases the focus node.
func (s *toggleState) Dispose() {
	if s.ownNode {
		s.node.Dispose()
	}
}

func (s *toggleState) enabled() bool {
	o := s.Widget().opts
	return o.OnChanged != nil && !o.Disabled
}

func (s *toggleState) set(f func()) {
	if s.Mounted() {
		s.SetState(f)
	}
}

func (s *toggleState) flip() {
	if o := s.Widget().opts; s.enabled() {
		o.OnChanged(!o.Checked)
	}
}

func (s *toggleState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	if _, ok := e.(*services.KeyDownEvent); !ok {
		return widgets.KeyEventResultIgnored
	}
	if isActivateKey(e.LogicalKey()) && s.enabled() {
		s.flip()
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

// Build implements widgets.State.
func (s *toggleState) Build(bc widgets.BuildContext) widgets.Widget {
	t := s.Widget()
	o := t.opts
	colors := ThemeOf(bc)
	enabled := s.enabled()
	foreground := colors.IconForeground
	border := ui.NewColor(0)
	var background ui.Color
	switch {
	case o.Checked:
		foreground, border, background = colors.InputOptionActiveForeground, colors.InputOptionActiveBorder, colors.InputOptionActiveBackground
	case s.hover && enabled:
		background = colors.InputOptionHoverBackground
	}
	if s.focus {
		border = colors.FocusBorder
	}
	decoration := painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Color:        foundation.Some(background),
		Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)}),
		BorderRadius: painting.NewBorderRadiusCircular(toggleRadius),
	})
	var body widgets.Widget = widgets.NewContainer(
		widgets.NewIcon(fonts.CodiconIcon(t.code), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(foreground)}),
		widgets.ContainerOpts{Width: foundation.Some(toggleSize), Height: foundation.Some(toggleSize), Alignment: painting.AlignmentCenter, Decoration: decoration})
	cursor := services.SystemMouseCursors.Basic
	if enabled {
		cursor = services.SystemMouseCursors.Click
	}
	body = widgets.NewMouseRegion(
		widgets.NewGestureDetector(body, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: s.flip}),
		widgets.MouseRegionOpts{
			Cursor:  cursor,
			OnEnter: func(*gestures.PointerEnterEvent) { s.set(func() { s.hover = true }) },
			OnExit:  func(*gestures.PointerExitEvent) { s.set(func() { s.hover = false }) },
		})
	body = widgets.NewFocus(body, widgets.FocusOpts{
		FocusNode: s.node, Autofocus: o.Autofocus && enabled && !o.NotFocusable, OnKeyEvent: s.handleKey,
		CanRequestFocus: foundation.Some(enabled && !o.NotFocusable),
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
	if o.Disabled {
		body = widgets.NewOpacity(toggleDisabledAlpha, body)
	}
	return body
}
