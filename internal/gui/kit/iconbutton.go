package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/widgets"
)

// Metrics of VS Code's icon buttons and toggles (the find widget's `.button` and
// `.monaco-custom-toggle`).
const (
	iconSize         = 16.0
	iconButtonSize   = 22.0
	iconButtonRadius = 5.0
	toggleSize       = 20.0
	toggleRadius     = 3.0
)

// IconButtonOpts are the options of NewIconButton.
type IconButtonOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// OnPressed is called when the button is clicked, or when Enter or Space is pressed while the
	// button has the focus. A button without it is disabled.
	OnPressed func()
	// Disabled dims the button, makes it ignore the pointer and the keyboard and takes it out of
	// the focus order.
	Disabled bool
	// Tooltip is the text of the hover that shows when the pointer rests on the button.
	Tooltip string
	// Checked shows the button as toggled on (the background of a toggled toolbar action).
	Checked bool
	// FocusNode is the focus node of the button, which the caller owns; without one the button
	// makes its own.
	FocusNode *widgets.FocusNode
	// Autofocus asks for the focus when the button is shown.
	Autofocus bool
	// OnFocusChange is called when the button gets or loses the focus.
	OnFocusChange func(focused bool)
	// HoverDelegate is the group of hovers the tooltip belongs to (see HoverDelegate); the default
	// is a group of its own.
	HoverDelegate *HoverDelegate
}

// IconButton is a button that shows one Codicon and has the background of a toolbar action under
// the pointer (a darker one while pressed), a focus outline in focusBorder, a tooltip and a
// keyboard: Enter and Space press it while it has the focus. A click does not move the focus to it.
//
// Ported from VS Code: src/vs/base/browser/ui/actionbar/actionViewItems.ts (ActionViewItem) and the
// `.action-label` rules of src/vs/base/browser/ui/actionbar/actionbar.css
type IconButton struct {
	widgets.StatefulWidget
	code int
	opts IconButtonOpts
}

// NewIconButton makes a button that shows the Codicon of a code point (see the Codicon...
// constants of package fonts).
func NewIconButton(code int, opts ...IconButtonOpts) *IconButton {
	b := &IconButton{code: code}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	b.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: b.opts.Key})
	return b
}

// Code is the code point of the Codicon the button shows.
func (b *IconButton) Code() int { return b.code }

// Tooltip is the text of the hover of the button.
func (b *IconButton) Tooltip() string { return b.opts.Tooltip }

// CreateState implements widgets.StatefulWidget.
func (b *IconButton) CreateState() widgets.State { return &iconButtonState{} }

type iconButtonState struct {
	widgets.StateOf[*IconButton]
}

// Build implements widgets.State.
func (s *iconButtonState) Build(widgets.BuildContext) widgets.Widget {
	b := s.Widget()
	o := b.opts
	enabled := o.OnPressed != nil && !o.Disabled
	return newActionItem(actionItemProps{
		icon: b.code, tooltip: o.Tooltip, checked: o.Checked,
		disabled: !enabled, focusable: enabled,
		// the find widget's disabled buttons are as dim as this
		disabledAlpha: 0x66,
		run:           o.OnPressed, keys: true,
		node: o.FocusNode, autofocus: o.Autofocus, onFocusChange: o.OnFocusChange,
		delegate: o.HoverDelegate,
	})
}
