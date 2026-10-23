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

// Metrics of VS Code's checkbox (`.monaco-custom-toggle.monaco-checkbox`).
const (
	checkboxSize     = 18.0
	checkboxRadius   = 3.0
	checkboxIconSize = 16.0
	// checkboxLabelGap is the margin at the right of the box, before the label.
	checkboxLabelGap = 9.0
	checkboxFontSize = 13.0
)

// CheckboxOpts are the options of NewCheckbox.
type CheckboxOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Checked says whether the box is checked.
	Checked bool
	// OnChanged is called with the new value when the box or its label is clicked, or when Space or
	// Enter is pressed while the checkbox has the focus. A checkbox without it ignores the pointer and
	// the keyboard.
	OnChanged func(checked bool)
	// Disabled shows the disabled colors, makes the checkbox ignore the pointer and the keyboard and
	// takes it out of the focus order.
	Disabled bool
	// Selected paints the box for a row that is selected (checkbox.selectBackground and selectBorder).
	Selected bool
	// LabelColor is the color of the label; the default is the workbench's foreground.
	LabelColor foundation.Opt[ui.Color]
	// Tooltip is the text of the hover that shows when the pointer rests on the checkbox.
	Tooltip string
	// FocusNode is the focus node of the checkbox, which the caller owns; without one the checkbox makes
	// its own.
	FocusNode *widgets.FocusNode
	// Autofocus asks for the focus when the checkbox is shown.
	Autofocus bool
	// OnFocusChange is called when the checkbox gets or loses the focus.
	OnFocusChange func(focused bool)
	// HoverDelegate is the group of hovers the tooltip belongs to; the default is a group of its own.
	HoverDelegate *HoverDelegate
}

// Checkbox is VS Code's checkbox: an 18 px box with a 1 px border and a check mark while checked, in
// checkbox.background, foreground and border (the disabled and selected colors where they apply),
// and an optional label at its right that a click toggles too. A click gives it the focus, which
// shows as a focusBorder border; Space and Enter flip it.
//
// Ported from VS Code: src/vs/base/browser/ui/toggle/toggle.ts (Checkbox) and toggle.css, and the
// checkbox row of src/vs/base/browser/ui/dialog/dialog.ts. Not ported: the tri-state checkbox.
type Checkbox struct {
	widgets.StatefulWidget
	label string
	opts  CheckboxOpts
}

// NewCheckbox makes a checkbox with a label (which may be empty).
func NewCheckbox(label string, opts ...CheckboxOpts) *Checkbox {
	c := &Checkbox{label: label}
	if len(opts) > 0 {
		c.opts = opts[0]
	}
	c.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: c.opts.Key})
	return c
}

// Label is the text next to the box.
func (c *Checkbox) Label() string { return c.label }

// Checked says whether the box is checked.
func (c *Checkbox) Checked() bool { return c.opts.Checked }

// CreateState implements widgets.StatefulWidget.
func (c *Checkbox) CreateState() widgets.State { return &checkboxState{} }

type checkboxState struct {
	widgets.StateOf[*Checkbox]
	node    *widgets.FocusNode
	ownNode bool
	focus   bool
}

// InitState makes the focus node.
func (s *checkboxState) InitState() { s.pickNode() }

func (s *checkboxState) pickNode() {
	if n := s.Widget().opts.FocusNode; n != nil {
		s.node, s.ownNode = n, false
		return
	}
	s.node, s.ownNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Checkbox"}), true
}

// DidUpdateWidget follows a new focus node.
func (s *checkboxState) DidUpdateWidget(old *Checkbox) {
	if old.opts.FocusNode != s.Widget().opts.FocusNode {
		if s.ownNode {
			s.node.Dispose()
		}
		s.pickNode()
	}
}

// Dispose releases the focus node.
func (s *checkboxState) Dispose() {
	if s.ownNode {
		s.node.Dispose()
	}
}

func (s *checkboxState) enabled() bool {
	o := s.Widget().opts
	return o.OnChanged != nil && !o.Disabled
}

func (s *checkboxState) flip() {
	if o := s.Widget().opts; s.enabled() {
		o.OnChanged(!o.Checked)
	}
}

func (s *checkboxState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	if _, ok := e.(*services.KeyDownEvent); !ok {
		return widgets.KeyEventResultIgnored
	}
	if isActivateKey(e.LogicalKey()) && s.enabled() {
		s.flip()
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

// clicked takes the focus and flips the box.
func (s *checkboxState) clicked() {
	if s.enabled() {
		s.node.RequestFocus()
	}
	s.flip()
}

// Build implements widgets.State.
func (s *checkboxState) Build(bc widgets.BuildContext) widgets.Widget {
	c := s.Widget()
	o := c.opts
	colors := ThemeOf(bc)
	disabled := o.Disabled
	fg, bg, border := colors.CheckboxForeground, colors.CheckboxBackground, colors.CheckboxBorder
	switch {
	case disabled:
		fg, bg, border = colors.CheckboxDisabledForeground, colors.CheckboxDisabledBackground, colors.CheckboxDisabledBackground
	case o.Selected:
		bg, border = colors.CheckboxSelectBackground, colors.CheckboxSelectBorder
	}
	if s.focus && !disabled {
		border = colors.FocusBorder
	}
	var check widgets.Widget
	if o.Checked {
		check = widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconCheck), widgets.IconOpts{Size: foundation.Some(checkboxIconSize), Color: foundation.Some(fg)})
	}
	box := widgets.NewContainer(check, widgets.ContainerOpts{
		Width: foundation.Some(checkboxSize), Height: foundation.Some(checkboxSize), Alignment: painting.AlignmentCenter,
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(bg),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(checkboxRadius),
		}),
	})
	parts := []widgets.Widget{box}
	if c.label != "" {
		labelColor := o.LabelColor.OrElse(colors.Foreground)
		parts = append(parts,
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(checkboxLabelGap)}),
			widgets.NewFlexible(widgets.NewText(c.label, widgets.TextOpts{Style: TextStyleOf(bc, checkboxFontSize, labelColor)})))
	}
	var body widgets.Widget = widgets.NewRow(parts, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	cursor := services.SystemMouseCursors.Basic
	if s.enabled() {
		cursor = services.SystemMouseCursors.Click
	}
	body = widgets.NewMouseRegion(
		widgets.NewGestureDetector(body, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: s.clicked}),
		widgets.MouseRegionOpts{Cursor: cursor})
	body = widgets.NewFocus(body, widgets.FocusOpts{
		FocusNode: s.node, Autofocus: o.Autofocus && s.enabled(), OnKeyEvent: s.handleKey,
		CanRequestFocus: foundation.Some(s.enabled()),
		OnFocusChange: func(has bool) {
			if s.Mounted() {
				s.SetState(func() { s.focus = has })
			}
			if o.OnFocusChange != nil {
				o.OnFocusChange(has)
			}
		},
	})
	if o.Tooltip != "" {
		body = NewHover(body, HoverOpts{Text: o.Tooltip, Compact: true, Delegate: o.HoverDelegate})
	}
	return body
}
