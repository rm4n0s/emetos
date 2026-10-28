package kit

import (
	"math"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// Metrics of VS Code's input box in the find widget.
const (
	inputBoxHeight   = 25.0
	inputBoxFontSize = 13.0
	inputBoxPadding  = 4.0
	// inputBoxLineHeight is the height of a line of a multi-line box and inputBoxMultiPadV the padding
	// above and below its text; with the 1 px border a box of one line is as high as a single-line box.
	inputBoxLineHeight = 17.0
	inputBoxMultiPadV  = 3.0
	// The validation message (`.monaco-inputbox-message`): 12 px text on a line of 17 px, 0.4em of
	// padding, a 1 px border, one pixel over the box.
	inputMessageFontSize = 12.0
	inputMessageLineH    = 17.0
	inputMessagePadding  = 4.8
)

// ValidationType is the severity of a validation message (VS Code's MessageType).
type ValidationType int

// The severities of a validation message. The message and the border of the box have the colors
// inputValidation.<severity>*.
const (
	// ValidationNone is no message.
	ValidationNone ValidationType = iota
	// ValidationInfo is a message of information (inputValidation.info*).
	ValidationInfo
	// ValidationWarning is a warning (inputValidation.warning*).
	ValidationWarning
	// ValidationError is an error (inputValidation.error*).
	ValidationError
)

// InputMessage is a message shown below an input box while it has the focus (VS Code's IMessage);
// the border of the box has the color of its severity as long as there is a message.
type InputMessage struct {
	// Content is the text of the message; a message without text only colors the border.
	Content string
	// Type is the severity.
	Type ValidationType
}

// InputBoxOpts are the options of NewInputBox.
type InputBoxOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Placeholder is shown while the box is empty.
	Placeholder string
	// Trailing are widgets shown inside the box after the text, such as the toggles of the find
	// input.
	Trailing []widgets.Widget
	// Invalid shows the border of an error.
	Invalid bool
	// OnChanged is called with the text when it changes: when the user changes it, and when
	// InputBoxController.SetValue or a move through the history does (VS Code's onDidChange).
	OnChanged func(text string)
	// OnSubmit is called with the text on Enter (Ctrl+Enter in a multi-line box, where Enter makes a
	// new line). With a History, the text is added to it first.
	OnSubmit func(text string)
	// OnFocusChange is called when the box gets or loses the focus.
	OnFocusChange func(focused bool)
	// Width, when set, is the width of the box; otherwise it fills the width it is given.
	Width foundation.Opt[float64]
	// Validate checks the text after each change (and when InputBoxController.Validate is called) and
	// returns the message to show, or nil when the text is valid (VS Code's validationOptions). The
	// message is shown below the box while the box has the focus; the border has the color of its
	// severity as long as the message is there.
	Validate func(text string) *InputMessage
	// Message is a message the owner sets, in a box without Validate: shown like a validation message.
	Message *InputMessage
	// History makes Up and Down (and Alt+Up and Alt+Down) go through the earlier values of the box
	// (VS Code's HistoryInputBox, see History). The owner keeps it between boxes.
	History *History
	// MultiLine makes the box a text area: Enter makes a new line, long lines wrap and the box grows
	// in height with the text, up to MaxHeight (VS Code's flexibleHeight).
	MultiLine bool
	// MaxHeight is the height past which a multi-line box stops growing and scrolls; without it the
	// box grows without limit.
	MaxHeight foundation.Opt[float64]
	// Disabled makes the box not take the focus or the keyboard and shows its text dimmed.
	Disabled bool
	// Obscure shows a dot for each character, as a password box does: the text cannot be copied or read back from the screen. A
	// multi-line box cannot be obscured.
	Obscure bool
	// Autofocus asks for the focus when the box is shown.
	Autofocus bool
	// SelectAllOnFocus selects the text when the box gets the focus; the default is Glutter's (on, on
	// the desktop, for a single-line box). A box whose owner chooses the selection turns it off.
	SelectAllOnFocus foundation.Opt[bool]
	// Controller reads and changes the box from outside (see InputBoxController); the text and
	// focus node of the controller are used where NewInputBox is given none.
	Controller *InputBoxController
}

// InputBox is VS Code's input box: a line of text on Glutter's EditableText with a background, a
// border that shows the focus, an error or a validation message, a placeholder and trailing widgets.
// It can validate the text and show the message below the box, go through a History with Up and
// Down, and be a multi-line box that grows with its text.
//
// A key is looked at before the text sees it: Enter submits, Up and Down move through the history
// (in a multi-line box only on its first and last line, or with Alt), and every other key goes to
// the text.
//
// Ported from VS Code: src/vs/base/browser/ui/inputbox/inputBox.ts (InputBox, HistoryInputBox),
// inputBox.css and src/vs/platform/history/browser/contextScopedHistoryWidget.ts (the keys). Not
// ported: the actions in the box (use Trailing), the tooltip and the screen reader
// announcements.
type InputBox struct {
	widgets.StatefulWidget
	controller *widgets.TextEditingController
	focusNode  *widgets.FocusNode
	opts       InputBoxOpts
}

// NewInputBox makes an input box over a controller (which holds the text and the selection) and
// the focus node of its text, which the caller owns. A nil controller or focus node is taken from
// opts.Controller, else the box makes its own.
func NewInputBox(controller *widgets.TextEditingController, focusNode *widgets.FocusNode, opts ...InputBoxOpts) *InputBox {
	b := &InputBox{controller: controller, focusNode: focusNode}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	if c := b.opts.Controller; c != nil {
		if b.controller == nil {
			b.controller = c.text
		}
		if b.focusNode == nil {
			b.focusNode = c.node
		}
	}
	b.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: b.opts.Key})
	return b
}

// CreateState implements widgets.StatefulWidget.
func (b *InputBox) CreateState() widgets.State {
	return &inputBoxState{portal: widgets.NewOverlayPortalController(widgets.OverlayPortalControllerOpts{DebugLabel: "InputBox"})}
}

type inputBoxState struct {
	widgets.StateOf[*InputBox]
	text    *widgets.TextEditingController
	node    *widgets.FocusNode
	ownText bool
	ownNode bool
	handle  foundation.ListenerHandle
	// prevKey is the key handler the node had before the box set its own.
	prevKey widgets.FocusOnKeyEventCallback
	focused bool
	// validated is the result of the last validation (nil: valid, or never validated).
	validated *InputMessage
	portal    *widgets.OverlayPortalController
	// ctrl is the controller the state is attached to.
	ctrl *InputBoxController
	// editable is the key of the EditableText, to open its input connection when a click finds the focus without one.
	editable *widgets.GlobalKey[*widgets.EditableTextState]
	// undo is the undo history of the text of the box (editKey).
	undo *widgets.UndoHistoryController
}

// InitState follows the text, to show the placeholder only while it is empty, and takes the keys.
func (s *inputBoxState) InitState() {
	s.editable = widgets.NewGlobalKey[*widgets.EditableTextState]()
	s.undo = widgets.NewUndoHistoryController()
	s.pickText()
	s.pickNode()
	s.attach()
}

func (s *inputBoxState) pickText() {
	if c := s.Widget().controller; c != nil {
		s.text, s.ownText = c, false
	} else {
		s.text, s.ownText = widgets.NewTextEditingController(), true
	}
	s.handle = s.text.AddListener(s.textChanged)
}

func (s *inputBoxState) pickNode() {
	if n := s.Widget().focusNode; n != nil {
		s.node, s.ownNode = n, false
	} else {
		s.node, s.ownNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "InputBox"}), true
	}
	s.prevKey = s.node.OnKeyEvent()
	s.node.SetOnKeyEvent(s.handleKey)
	s.node.SetCanRequestFocus(!s.Widget().opts.Disabled)
}

func (s *inputBoxState) releaseText() {
	s.text.RemoveListener(s.handle)
	if s.ownText {
		s.text.Dispose()
	}
}

func (s *inputBoxState) releaseNode() {
	s.node.SetOnKeyEvent(s.prevKey)
	if s.ownNode {
		s.node.Dispose()
	}
}

func (s *inputBoxState) attach() {
	if s.ctrl != nil && s.ctrl.state == s {
		s.ctrl.state = nil
	}
	s.ctrl = s.Widget().opts.Controller
	if s.ctrl != nil {
		s.ctrl.state = s
	}
}

func (s *inputBoxState) textChanged() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// DidUpdateWidget follows a new controller, focus node, controller or message.
func (s *inputBoxState) DidUpdateWidget(old *InputBox) {
	w := s.Widget()
	if old.controller != w.controller {
		s.releaseText()
		s.pickText()
	}
	if old.focusNode != w.focusNode {
		s.releaseNode()
		s.pickNode()
	}
	if old.opts.Controller != w.opts.Controller {
		s.attach()
	}
	if old.opts.Disabled != w.opts.Disabled {
		s.node.SetCanRequestFocus(!w.opts.Disabled)
		if w.opts.Disabled {
			s.node.Unfocus()
		}
	}
	if old.opts.Message != w.opts.Message {
		// the overlay is shown or hidden after the frame: a portal must not change during the build
		scheduler := widgets.WidgetsBindingOf(s.Context()).Scheduler()
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(time.Duration) { s.syncMessage() })
	}
}

// Dispose stops following the text and gives the node its handler back.
func (s *inputBoxState) Dispose() {
	if s.ctrl != nil && s.ctrl.state == s {
		s.ctrl.state = nil
	}
	s.releaseText()
	s.releaseNode()
	s.undo.Dispose()
}

func (s *inputBoxState) focusChanged(has bool) {
	if s.focused != has && s.Mounted() {
		s.SetState(func() { s.focused = has })
	}
	s.focused = has
	s.syncMessage()
	if f := s.Widget().opts.OnFocusChange; f != nil {
		f(has)
	}
}

// message is the message to show: the result of the validation, or the one the owner sets.
func (s *inputBoxState) message() *InputMessage {
	o := s.Widget().opts
	if o.Validate != nil {
		return s.validated
	}
	return o.Message
}

// syncMessage shows the message below the box while the box has the focus and the message has text,
// and hides it otherwise.
func (s *inputBoxState) syncMessage() {
	if !s.Mounted() {
		return
	}
	m := s.message()
	if show := s.focused && m != nil && m.Content != ""; show != s.portal.IsShowing() {
		if show {
			s.portal.Show()
		} else {
			s.portal.Hide()
		}
	}
}

// validate runs the validation and shows the result.
func (s *inputBoxState) validate() ValidationType {
	o := s.Widget().opts
	if o.Validate == nil {
		if o.Message != nil {
			return o.Message.Type
		}
		return ValidationNone
	}
	s.validated = o.Validate(s.text.Text())
	if s.Mounted() {
		s.SetState(func() {})
	}
	s.syncMessage()
	if s.validated == nil {
		return ValidationNone
	}
	return s.validated.Type
}

// changed is a change of the text: the owner is told and the text is validated.
func (s *inputBoxState) changed(text string) {
	if f := s.Widget().opts.OnChanged; f != nil {
		f(text)
	}
	s.validate()
}

// setValue replaces the text, with the caret after it, and reports the change when there is one.
func (s *inputBoxState) setValue(text string) {
	if s.text.Text() == text {
		return
	}
	s.text.SetValue(services.NewTextEditingValue(services.TextEditingValueOpts{
		Text: text, Selection: foundation.Some(services.NewTextSelectionCollapsed(len(text))),
	}))
	s.changed(text)
}

// addToHistory adds the text to the history unless it is empty or is the value the history is at
// (VS Code's addToHistory).
func (s *inputBoxState) addToHistory(always bool) {
	h := s.Widget().opts.History
	text := s.text.Text()
	if h == nil || text == "" {
		return
	}
	cur, ok := h.Current()
	if !ok {
		// compare with the last value, without moving
		cur, _ = h.Last()
		h.Next()
	}
	if always || text != cur {
		h.Add(text)
	}
}

// showNextValue goes to the next value of the history, or to an empty box past the last one.
func (s *inputBoxState) showNextValue() {
	h := s.Widget().opts.History
	if h == nil {
		return
	}
	text := s.text.Text()
	if !h.Has(text) {
		s.addToHistory(false)
	}
	next, _ := h.Next()
	if next != "" && next == text {
		next, _ = h.Next()
	}
	s.setValue(next)
}

// showPreviousValue goes to the previous value of the history; at the first one the box stays.
func (s *inputBoxState) showPreviousValue() {
	h := s.Widget().opts.History
	if h == nil {
		return
	}
	text := s.text.Text()
	if !h.Has(text) {
		s.addToHistory(false)
	}
	previous := historyPrevious(h)
	if previous != "" && previous == text {
		previous = historyPrevious(h)
	}
	if previous != "" {
		s.setValue(previous)
	}
}

// historyPrevious is the previous value, or else the first one (`previous() || first()`).
func historyPrevious(h *History) string {
	if v, ok := h.Previous(); ok && v != "" {
		return v
	}
	v, _ := h.First()
	return v
}

func (s *inputBoxState) submit() {
	o := s.Widget().opts
	text := s.text.Text()
	s.addToHistory(false)
	if o.OnSubmit != nil {
		o.OnSubmit(text)
	}
}

// onFirstLine and onLastLine say whether the caret is on the first or last line of the text (counting
// the lines the text breaks, not the ones it wraps to).
func (s *inputBoxState) onFirstLine() bool {
	sel, text := s.text.Selection(), s.text.Text()
	return !sel.IsValid() || !strings.Contains(text[:min(sel.Start(), len(text))], "\n")
}

func (s *inputBoxState) onLastLine() bool {
	sel, text := s.text.Selection(), s.text.Text()
	return !sel.IsValid() || !strings.Contains(text[min(sel.End(), len(text)):], "\n")
}

// handleKey is the node's key handler: it sees the keys before the text does.
func (s *inputBoxState) handleKey(node *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	if r := s.ownKey(e); r != widgets.KeyEventResultIgnored {
		return r
	}
	if s.prevKey != nil {
		return s.prevKey(node, e)
	}
	return widgets.KeyEventResultIgnored
}

func (s *inputBoxState) ownKey(e services.KeyEvent) widgets.KeyEventResult {
	o := s.Widget().opts
	if o.Disabled {
		return widgets.KeyEventResultIgnored
	}
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	if kb.IsControlPressed() && !kb.IsAltPressed() && !kb.IsMetaPressed() && s.editKey(e, kb.IsShiftPressed()) {
		return widgets.KeyEventResultHandled
	}
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter:
		if kb.IsShiftPressed() || kb.IsAltPressed() || kb.IsMetaPressed() || (o.MultiLine && !kb.IsControlPressed()) || (!o.MultiLine && kb.IsControlPressed()) {
			return widgets.KeyEventResultIgnored
		}
		if _, ok := e.(*services.KeyRepeatEvent); ok {
			return widgets.KeyEventResultHandled
		}
		s.submit()
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyArrowUp, services.LogicalKeyboardKeyArrowDown:
		if o.History == nil || kb.IsShiftPressed() || kb.IsControlPressed() || kb.IsMetaPressed() {
			return widgets.KeyEventResultIgnored
		}
		up := e.LogicalKey() == services.LogicalKeyboardKeyArrowUp
		// in a multi-line box the keys move the caret, until it is on the first or last line
		if !kb.IsAltPressed() && o.MultiLine && ((up && !s.onFirstLine()) || (!up && !s.onLastLine())) {
			return widgets.KeyEventResultIgnored
		}
		if up {
			s.showPreviousValue()
		} else {
			s.showNextValue()
		}
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

// border is the color of the border: the severity of the message, else an error, the focus, or the
// color of the input's border.
func (s *inputBoxState) border(colors Colors) ui.Color {
	if m := s.message(); m != nil {
		return validationColors(colors, m.Type).border
	}
	switch {
	case s.Widget().opts.Invalid:
		return colors.InputValidationErrorBorder
	case s.focused:
		return colors.FocusBorder
	}
	return colors.InputBorder
}

// validationColor are the colors of the message of a severity.
type validationColor struct{ background, foreground, border ui.Color }

func validationColors(c Colors, t ValidationType) validationColor {
	switch t {
	case ValidationInfo:
		return validationColor{c.InputValidationInfoBackground, c.InputValidationInfoForeground, c.InputValidationInfoBorder}
	case ValidationWarning:
		return validationColor{c.InputValidationWarningBackground, c.InputValidationWarningForeground, c.InputValidationWarningBorder}
	}
	return validationColor{c.InputValidationErrorBackground, c.InputValidationErrorForeground, c.InputValidationErrorBorder}
}

// Build implements widgets.State.
func (s *inputBoxState) Build(bc widgets.BuildContext) widgets.Widget {
	b := s.Widget()
	o := b.opts
	colors := ThemeOf(bc)
	fg := colors.InputForeground
	if o.Disabled {
		fg = colors.DisabledForeground
	}
	style := TextStyleOf(bc, inputBoxFontSize, fg)
	opts := widgets.EditableTextOpts{
		ReadOnly:         o.Disabled,
		ObscureText:      o.Obscure && !o.MultiLine,
		Autofocus:        o.Autofocus && !o.Disabled,
		SelectAllOnFocus: o.SelectAllOnFocus,
		Key:              s.editable,
		UndoController:   s.undo,
		OnChanged:        s.changed,
		SelectionColor:   foundation.Some(colors.EditorSelectionBackground),
		MouseCursor:      services.SystemMouseCursors.Text,
		// The window's key dispatcher handles Enter and the other keys of the find widget before
		// the text sees them; a click elsewhere in the window moves the focus on its own.
		OnTapOutside: func(*gestures.PointerDownEvent) {},
	}
	crossAxis := rendering.CrossAxisAlignmentCenter
	var constraints foundation.Opt[rendering.BoxConstraints]
	height := foundation.Some(inputBoxHeight)
	var body widgets.Widget
	if o.MultiLine {
		style = painting.NewTextStyle(painting.TextStyleOpts{
			FontFamily: UIFontOf(bc), FontFamilyFallback: []string{"sans-serif"}, FontSize: foundation.Some(inputBoxFontSize),
			Height: foundation.Some(inputBoxLineHeight / inputBoxFontSize), Color: foundation.Some(fg),
		})
		opts.MinLines = foundation.Some(1)
		opts.MaxLines = foundation.Some(multiLineMaxLines(o))
		// no scrollbar of Glutter's: the textarea of VS Code hides its own too
		opts.ScrollBehavior = widgets.ScrollConfigurationOf(bc).CopyWith(widgets.ScrollBehaviorCopy{Scrollbars: foundation.Some(false), Overscroll: foundation.Some(false)})
		opts.KeyboardType = foundation.Some(services.TextInputTypeMultiline)
		opts.TextInputAction = services.TextInputActionNewline
		crossAxis, height = rendering.CrossAxisAlignmentStart, foundation.None[float64]()
		constraints = foundation.Some(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinHeight: foundation.Some(inputBoxHeight)}))
	}
	text := widgets.NewEditableText(s.text, s.node, style, fg, colors.InputForeground.WithAlpha(0x80), opts)
	if o.MultiLine {
		body = widgets.NewPadding(painting.NewEdgeInsetsSymmetric(inputBoxMultiPadV, 0), text)
	} else {
		body = widgets.NewAlign(text, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})
	}
	// The tree is the same whether the placeholder shows or not (SPEC §10.2 "As built (step 17b.1)"): the hint is
	// always the second child of the Stack and is an empty box once there is text, so the EditableText is not mounted
	// again at the first key. (Glutter 1.9.2 opens the input connection of a field that is mounted with the focus on its
	// node, so a box that is mounted again keeps taking keys; see TestInputBoxMountedAgainWithTheFocusTakesKeys.)
	var hint widgets.Widget = widgets.NewSizedBox(nil)
	if s.text.Text() == "" && o.Placeholder != "" {
		hintStyle := TextStyleOf(bc, inputBoxFontSize, colors.InputPlaceholderForeground)
		hint = widgets.NewText(o.Placeholder, widgets.TextOpts{Style: hintStyle, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowClip})
		if o.MultiLine {
			hint = widgets.NewPadding(painting.NewEdgeInsetsSymmetric(inputBoxMultiPadV+(inputBoxLineHeight-inputBoxFontSize)/2, 0), hint)
			hint = widgets.NewAlign(hint, widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})
		} else {
			hint = widgets.NewAlign(hint, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})
		}
	}
	fit := rendering.StackFitExpand
	if o.MultiLine {
		fit = rendering.StackFitPassthrough
	}
	body = widgets.NewStack([]widgets.Widget{body, widgets.NewIgnorePointer(hint)}, widgets.StackOpts{Fit: fit})
	row := []widgets.Widget{widgets.NewExpanded(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, inputBoxPadding), body))}
	trailing := o.Trailing
	if o.MultiLine {
		// the trailing widgets stay at the top, in the height of a line
		for _, t := range trailing {
			row = append(row, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 1, 0, 0), t))
		}
	} else {
		row = append(row, trailing...)
	}
	row = append(row, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(2.0)}))
	cOpts := widgets.ContainerOpts{
		Width: o.Width, Height: height, Constraints: constraints,
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.InputBackground),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(s.border(colors)), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(2),
		}),
	}
	box := widgets.NewContainer(widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: crossAxis}), cOpts)
	focus := widgets.NewFocus(box, widgets.FocusOpts{OnFocusChange: s.focusChanged, CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true)})
	return widgets.NewOverlayPortalOverlayChildLayoutBuilder(s.portal, s.buildMessage, focus)
}

// editKey runs Ctrl+A (select all), Ctrl+Z (undo), Ctrl+Y and Ctrl+Shift+Z (redo) on the text of the box, and tells whether
// the key was one of them. The window binds these keys to the commands of the editor (editor.action.selectAll, undo, redo),
// which would otherwise take them from a box and act on a document; the box has them first (desktop test, step 17b.1).
func (s *inputBoxState) editKey(e services.KeyEvent, shift bool) bool {
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyKeyA:
		if st, ok := s.editable.CurrentState(); ok && st != nil && !shift {
			st.SelectAll(services.SelectionChangedCauseKeyboard)
		}
		return !shift
	case services.LogicalKeyboardKeyKeyZ:
		if shift {
			s.undo.Redo()
		} else {
			s.undo.Undo()
		}
		return true
	case services.LogicalKeyboardKeyKeyY:
		s.undo.Redo()
		return true
	}
	return false
}

// multiLineMaxLines is the number of lines past which a multi-line box scrolls.
func multiLineMaxLines(o InputBoxOpts) int {
	h, ok := o.MaxHeight.Get()
	if !ok {
		return rendering.MaxLinesUnlimited
	}
	return max(1, int(math.Floor((h-2-2*inputBoxMultiPadV)/inputBoxLineHeight)))
}

// buildMessage is the validation message, below the box (above it where there is no room).
func (s *inputBoxState) buildMessage(bc widgets.BuildContext, info widgets.OverlayChildLayoutInfo) widgets.Widget {
	m := s.message()
	if m == nil || m.Content == "" {
		return widgets.NewSizedBox(nil)
	}
	colors := ThemeOf(bc)
	vc := validationColors(colors, m.Type)
	fg := vc.foreground
	if fg.Alpha() == 0 {
		fg = colors.Foreground
	}
	target := info.ChildPaintTransform.TransformRect(ui.NewRectFromLTWH(0, 0, info.ChildSize.Width, info.ChildSize.Height))
	style := painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: UIFontOf(bc), FontFamilyFallback: []string{"sans-serif"}, FontSize: foundation.Some(inputMessageFontSize),
		Height: foundation.Some(inputMessageLineH / inputMessageFontSize), Color: foundation.Some(fg),
	})
	box := widgets.NewContainer(widgets.NewText(m.Content, widgets.TextOpts{Style: style}), widgets.ContainerOpts{
		Padding: painting.NewEdgeInsetsAll(inputMessagePadding),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:  foundation.Some(vc.background),
			Border: painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(vc.border), Width: foundation.Some(1.0)}),
		}),
	})
	layout := &inputMessageLayout{SingleChildLayoutDelegateBase: rendering.NewSingleChildLayoutDelegateBase(nil), target: target}
	return widgets.NewPositionedFill(widgets.NewCustomSingleChildLayout(layout, widgets.NewIgnorePointer(box)))
}

// inputMessageLayout puts the message under the box as wide as the box, one pixel over it, or above
// the box when there is no room below.
type inputMessageLayout struct {
	rendering.SingleChildLayoutDelegateBase
	target ui.Rect
}

func (l *inputMessageLayout) GetConstraintsForChild(c rendering.BoxConstraints) rendering.BoxConstraints {
	return rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{
		MinWidth: foundation.Some(l.target.Width()), MaxWidth: foundation.Some(l.target.Width()), MaxHeight: foundation.Some(c.MaxHeight),
	})
}

func (l *inputMessageLayout) GetPositionForChild(size, childSize ui.Size) ui.Offset {
	y := l.target.Bottom - 1
	if y+childSize.Height > size.Height && l.target.Top+1-childSize.Height >= 0 {
		y = l.target.Top + 1 - childSize.Height
	}
	return ui.Offset{Dx: l.target.Left, Dy: y}
}

func (l *inputMessageLayout) ShouldRelayout(old rendering.SingleChildLayoutDelegate) bool {
	return l.target != old.(*inputMessageLayout).target
}

// InputBoxController reads and changes an input box from outside (VS Code's InputBox methods): it
// holds the text controller and the focus node of the box, so it works before the box is built, and
// is attached to the box it is given to through InputBoxOpts.Controller. The caller owns it and
// calls Dispose when the box is gone. It is used from the UI thread only.
type InputBoxController struct {
	text  *widgets.TextEditingController
	node  *widgets.FocusNode
	state *inputBoxState
}

// NewInputBoxController makes a controller of a box that starts with a text.
func NewInputBoxController(text string) *InputBoxController {
	return &InputBoxController{
		text: widgets.NewTextEditingController(text),
		node: widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "InputBox"}),
	}
}

// Dispose releases the text controller and the focus node.
func (c *InputBoxController) Dispose() {
	c.text.Dispose()
	c.node.Dispose()
}

// TextController is the controller of the text of the box.
func (c *InputBoxController) TextController() *widgets.TextEditingController { return c.text }

// FocusNode is the focus node of the text of the box.
func (c *InputBoxController) FocusNode() *widgets.FocusNode { return c.node }

// Value is the text.
func (c *InputBoxController) Value() string { return c.text.Text() }

// SetValue replaces the text, with the caret after it; when the text changes the box reports it to
// OnChanged and validates it, as VS Code's `value` setter does.
func (c *InputBoxController) SetValue(text string) {
	if c.state != nil {
		c.state.setValue(text)
		return
	}
	c.text.SetValue(services.NewTextEditingValue(services.TextEditingValueOpts{
		Text: text, Selection: foundation.Some(services.NewTextSelectionCollapsed(len(text))),
	}))
}

// Focus gives the focus to the box.
func (c *InputBoxController) Focus() { c.node.RequestFocus() }

// Blur takes the focus from the box.
func (c *InputBoxController) Blur() { c.node.Unfocus() }

// HasFocus says whether the text of the box has the focus.
func (c *InputBoxController) HasFocus() bool { return c.node.HasPrimaryFocus() }

// SelectAll selects the whole text.
func (c *InputBoxController) SelectAll() { c.Select(0, len(c.text.Text())) }

// Select selects a range of the text, in bytes; the range is cut to the text.
func (c *InputBoxController) Select(start, end int) {
	n := len(c.text.Text())
	c.text.SetSelection(services.NewTextSelection(min(max(start, 0), n), min(max(end, 0), n)))
}

// Selection is the selected range in bytes (start equals end for a caret), or false when the box has
// no selection yet.
func (c *InputBoxController) Selection() (start, end int, ok bool) {
	sel := c.text.Selection()
	if !sel.IsValid() {
		return 0, 0, false
	}
	return sel.Start(), sel.End(), true
}

// Validate runs the validation of the box and returns the severity of its message (ValidationNone
// when the text is valid or the box has no validation).
func (c *InputBoxController) Validate() ValidationType {
	if c.state == nil {
		return ValidationNone
	}
	return c.state.validate()
}

// AddToHistory adds the text to the history of the box unless it is empty or the value the history
// is at; with always it is added even then (VS Code's addToHistory).
func (c *InputBoxController) AddToHistory(always bool) {
	if c.state != nil {
		c.state.addToHistory(always)
	}
}

// ShowPreviousValue goes to the previous value of the history, as the Up key does.
func (c *InputBoxController) ShowPreviousValue() {
	if c.state != nil {
		c.state.showPreviousValue()
	}
}

// ShowNextValue goes to the next value of the history, as the Down key does.
func (c *InputBoxController) ShowNextValue() {
	if c.state != nil {
		c.state.showNextValue()
	}
}
