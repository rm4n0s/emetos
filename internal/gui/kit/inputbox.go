package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"
)

// Metrics of VS Code's input box in the find widget.
const (
	inputBoxHeight   = 25.0
	inputBoxFontSize = 13.0
	inputBoxPadding  = 4.0
)

// InputBoxOpts are the options of NewInputBox.
type InputBoxOpts struct {
	// Placeholder is shown while the box is empty.
	Placeholder string
	// Trailing are widgets shown inside the box after the text, such as the toggles of the find
	// input.
	Trailing []widgets.Widget
	// Invalid shows the border of an error.
	Invalid bool
	// OnChanged is called with the text when the user changes it.
	OnChanged func(text string)
	// OnFocusChange is called when the box gets or loses the focus.
	OnFocusChange func(focused bool)
	// Width, when set, is the width of the box; otherwise it fills the width it is given.
	Width foundation.Opt[float64]
}

// InputBox is VS Code's input box: a single line of text on Glutter's EditableText with a
// background, a border that shows the focus or an error, a placeholder and trailing widgets.
// This is the first version; step 6.1 adds validation messages, history and multiple lines.
type InputBox struct {
	widgets.StatefulWidget
	controller *widgets.TextEditingController
	focusNode  *widgets.FocusNode
	opts       InputBoxOpts
}

// NewInputBox makes an input box over a controller (which holds the text and the selection) and
// the focus node of its text, which the caller owns.
func NewInputBox(controller *widgets.TextEditingController, focusNode *widgets.FocusNode, opts ...InputBoxOpts) *InputBox {
	b := &InputBox{StatefulWidget: widgets.NewStatefulWidgetBase(), controller: controller, focusNode: focusNode}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	return b
}

// CreateState implements widgets.StatefulWidget.
func (b *InputBox) CreateState() widgets.State { return &inputBoxState{} }

type inputBoxState struct {
	widgets.StateOf[*InputBox]
	focused bool
	handle  foundation.ListenerHandle
}

// InitState follows the text, to show the placeholder only while it is empty.
func (s *inputBoxState) InitState() {
	s.handle = s.Widget().controller.AddListener(s.textChanged)
}

func (s *inputBoxState) textChanged() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// DidUpdateWidget follows a new controller.
func (s *inputBoxState) DidUpdateWidget(old *InputBox) {
	if old.controller != s.Widget().controller {
		old.controller.RemoveListener(s.handle)
		s.handle = s.Widget().controller.AddListener(s.textChanged)
	}
}

// Dispose stops following the text.
func (s *inputBoxState) Dispose() { s.Widget().controller.RemoveListener(s.handle) }

func (s *inputBoxState) focusChanged(has bool) {
	if s.focused != has && s.Mounted() {
		s.SetState(func() { s.focused = has })
	}
	if f := s.Widget().opts.OnFocusChange; f != nil {
		f(has)
	}
}

// Build implements widgets.State.
func (s *inputBoxState) Build(bc widgets.BuildContext) widgets.Widget {
	b := s.Widget()
	colors := ThemeOf(bc)
	style := TextStyleOf(bc, inputBoxFontSize, colors.InputForeground)
	border := colors.InputBorder
	switch {
	case b.opts.Invalid:
		border = colors.InputValidationErrorBorder
	case s.focused:
		border = colors.FocusBorder
	}
	text := widgets.NewEditableText(b.controller, b.focusNode, style, colors.InputForeground, colors.InputForeground.WithAlpha(0x80),
		widgets.EditableTextOpts{
			OnChanged:      b.opts.OnChanged,
			SelectionColor: foundation.Some(colors.EditorSelectionBackground),
			MouseCursor:    services.SystemMouseCursors.Text,
			// The window's key dispatcher handles Enter and the other keys of the find widget before
			// the text sees them; a click elsewhere in the window moves the focus on its own.
			OnTapOutside: func(*gestures.PointerDownEvent) {},
		})
	var body widgets.Widget = widgets.NewAlign(text, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})
	if b.controller.Text() == "" && b.opts.Placeholder != "" {
		hint := widgets.NewIgnorePointer(widgets.NewAlign(
			widgets.NewText(b.opts.Placeholder, widgets.TextOpts{Style: TextStyleOf(bc, inputBoxFontSize, colors.InputPlaceholderForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowClip}),
			widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft}))
		body = widgets.NewStack([]widgets.Widget{body, hint}, widgets.StackOpts{Fit: rendering.StackFitExpand})
	}
	row := []widgets.Widget{widgets.NewExpanded(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, inputBoxPadding), body))}
	row = append(row, b.opts.Trailing...)
	row = append(row, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(2.0)}))
	box := widgets.NewContainer(
		widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
		widgets.ContainerOpts{
			Width: b.opts.Width, Height: foundation.Some(inputBoxHeight),
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color:        foundation.Some(colors.InputBackground),
				Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)}),
				BorderRadius: painting.NewBorderRadiusCircular(2),
			}),
		})
	return widgets.NewFocus(box, widgets.FocusOpts{OnFocusChange: s.focusChanged, CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true)})
}
