package kit

import (
	"regexp"
	"slices"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of VS Code's dialog (`.monaco-dialog-box`).
const (
	dialogWidth        = 480.0
	dialogMaxFraction  = 0.9
	dialogPadding      = 8.0
	dialogRadius       = 12.0
	dialogShadowBlur   = 8.0
	dialogToolbarH     = 24.0
	dialogIconSize     = 24.0
	dialogRowPadLeft   = 12.0
	dialogRowPadRight  = 8.0
	dialogMessageFont  = 14.0
	dialogMessageMinH  = 22.0
	dialogDetailFont   = 13.0
	dialogDetailLineH  = 20.0
	dialogCheckboxTop  = 15.0
	dialogButtonsTop   = 20.0
	dialogButtonsLeft  = 67.0
	dialogButtonMargin = 4.0
)

// dialogScrim is the color of the modal block over the window (`.monaco-dialog-modal-block.dimmed`).
var dialogScrim = ui.NewColor(0x80000000)

// DialogType says what a dialog is about, which sets its icon and the color of the icon.
type DialogType int

// The types of a dialog. VS Code shows the information icon for DialogQuestion too.
const (
	// DialogNone has no icon.
	DialogNone DialogType = iota
	// DialogInfo shows the information icon (problemsInfoIcon.foreground).
	DialogInfo
	// DialogWarning shows the warning icon (problemsWarningIcon.foreground).
	DialogWarning
	// DialogError shows the error icon (problemsErrorIcon.foreground).
	DialogError
	// DialogQuestion shows the information icon, as VS Code does.
	DialogQuestion
)

// DialogResult is the answer to a dialog.
type DialogResult struct {
	// Button is the index in DialogSpec.Buttons of the button that was pressed. When the dialog was
	// cancelled (Escape, the close button, DialogController.Dismiss) it is DialogSpec.CancelButton, or
	// -1 when the spec names none.
	Button int
	// Checked is the state of the checkbox when the dialog closed (false when it has none).
	Checked bool
	// Cancelled says the dialog was closed without pressing a button.
	Cancelled bool
}

// DialogSpec describes a dialog (VS Code's IDialogOptions and the arguments of Dialog).
type DialogSpec struct {
	// Type sets the icon.
	Type DialogType
	// Message is the main text. A dialog without Detail shows it in the regular text of the detail;
	// with a Detail it is the bold title above it.
	Message string
	// Detail is the text under the message; its line breaks are kept.
	Detail string
	// Buttons are the labels of the buttons; the default is one button, "OK". The first is the default
	// button: it is the primary button, has the focus when the dialog opens and is the one Enter
	// presses. Written "&&Save" or "Save (&&S)", the marks of a mnemonic are left out. The buttons
	// show in the order of Linux (see DialogButtonOrder).
	Buttons []string
	// CancelButton is the index of the button that stands for cancelling: it is the answer when the
	// dialog is cancelled, and it comes right before the default button in the row.
	CancelButton foundation.Opt[int]
	// DisableClose makes Escape and the close button do nothing: only a button closes the dialog.
	DisableClose bool
	// CheckboxLabel, when set, adds a checkbox with this label under the text (VS Code's
	// checkboxLabel), checked at first if CheckboxChecked.
	CheckboxLabel   string
	CheckboxChecked bool
	// OnAnswer is called, once, with the answer when the dialog closes.
	OnAnswer func(DialogResult)
}

// buttonLabels are the labels of the buttons of the spec, "OK" where it has none.
func (d DialogSpec) buttonLabels() []string {
	if len(d.Buttons) == 0 {
		return []string{"OK"}
	}
	return d.Buttons
}

// cancelIndex is the index of the button that stands for cancelling, or -1.
func (d DialogSpec) cancelIndex() int {
	if i, ok := d.CancelButton.Get(); ok && i >= 0 && i < len(d.buttonLabels()) {
		return i
	}
	return -1
}

// DialogButtonOrder gives the order the buttons of a dialog show in, as the indexes of the buttons,
// from the left. On Linux (GNOME's guidelines, which VS Code follows) the cancel button comes right
// before the default one and the default button is the last: the buttons "Save", "Don't Save",
// "Cancel" with the third as the cancel button show as "Don't Save", "Cancel", "Save".
//
// Ported from VS Code: src/vs/base/browser/ui/dialog/dialog.ts (rearrangeButtons, Linux)
func DialogButtonOrder(count int, cancel foundation.Opt[int]) []int {
	order := make([]int, count)
	for i := range order {
		order[i] = i
	}
	if count < 2 {
		return order
	}
	if c, ok := cancel.Get(); ok && c >= 0 && c < count {
		order = slices.Delete(order, c, c+1)
		order = slices.Insert(order, 1, c)
	}
	slices.Reverse(order)
	return order
}

var mnemonicMarks = regexp.MustCompile(`\(&&\w\)|&&`)

// dialogButtonLabel is the label of a button without the marks of a mnemonic.
func dialogButtonLabel(label string) string { return mnemonicMarks.ReplaceAllString(label, "") }

// Dialog is VS Code's dialog box: an optional icon by type, a message and a detail, an optional
// checkbox and a row of buttons, in editorWidget.background with a 1 px widget.border, a radius of 12 and
// a shadow, 480 px wide (at most 90 % of the window), with a close button at the top right unless the
// dialog cannot be closed. Button 0 is the default: primary, focused when the dialog opens. The
// keyboard: Tab and Shift+Tab (and Right and Left) move the focus round the checkbox and the buttons and
// never out of the dialog, Enter or Space presses the focused button, Enter on the dialog itself presses
// the default one, Enter or Space on the checkbox flips it and Escape cancels. A DialogHost shows it
// over a modal block.
//
// Ported from VS Code: src/vs/base/browser/ui/dialog/dialog.ts and dialog.css. Not ported: inputs,
// the vertical alignment, custom bodies, footers and links, the button with a dropdown, the pending
// type's spinner and the mnemonics.
type Dialog struct {
	widgets.StatefulWidget
	spec     DialogSpec
	onResult func(DialogResult)
	// register, when set, is given the function that brings the focus into the dialog (the host
	// calls it when the scrim is clicked).
	register func(focus func())
}

// NewDialog makes a dialog box for a spec; onResult is called, once, with the answer. Use a
// DialogHost to show one in a window.
func NewDialog(spec DialogSpec, onResult func(DialogResult)) *Dialog {
	return &Dialog{StatefulWidget: widgets.NewStatefulWidgetBase(), spec: spec, onResult: onResult}
}

// CreateState implements widgets.StatefulWidget.
func (d *Dialog) CreateState() widgets.State { return &dialogState{} }

type dialogState struct {
	widgets.StateOf[*Dialog]
	scope   *widgets.FocusScopeNode
	buttons []*widgets.FocusNode
	box     *widgets.FocusNode
	check   *widgets.FocusNode
	checked bool
	answer  bool
}

// InitState makes the focus nodes and focuses the default button once the dialog is built.
func (s *dialogState) InitState() {
	spec := s.Widget().spec
	s.scope = widgets.NewFocusScopeNode(widgets.FocusScopeNodeOpts{DebugLabel: "Dialog"})
	s.box = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "DialogBox"})
	s.check = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "DialogCheckbox"})
	for range spec.buttonLabels() {
		s.buttons = append(s.buttons, widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "DialogButton"}))
	}
	s.checked = spec.CheckboxChecked
	if r := s.Widget().register; r != nil {
		r(s.refocus)
	}
	scheduler := widgets.WidgetsBindingOf(s.Context()).Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if s.Mounted() {
			s.focusDefault()
		}
	})
}

// Dispose releases the focus nodes.
func (s *dialogState) Dispose() {
	for _, n := range s.buttons {
		n.Dispose()
	}
	s.check.Dispose()
	s.box.Dispose()
	s.scope.Dispose()
}

// focusDefault gives the focus to the default button.
func (s *dialogState) focusDefault() { s.buttons[0].RequestFocus() }

// result closes the dialog with an answer, once.
func (s *dialogState) result(button int, cancelled bool) {
	if s.answer {
		return
	}
	s.answer = true
	if f := s.Widget().onResult; f != nil {
		f(DialogResult{Button: button, Checked: s.checked, Cancelled: cancelled})
	}
}

// cancel closes the dialog as Escape and the close button do, unless the spec forbids it.
func (s *dialogState) cancel() {
	spec := s.Widget().spec
	if spec.DisableClose {
		return
	}
	s.result(spec.cancelIndex(), true)
}

// ring is the focus nodes Tab goes through, in the order of the screen: the checkbox, then the buttons.
func (s *dialogState) ring() []*widgets.FocusNode {
	var out []*widgets.FocusNode
	if s.Widget().spec.CheckboxLabel != "" {
		out = append(out, s.check)
	}
	for _, i := range DialogButtonOrder(len(s.buttons), s.Widget().spec.CancelButton) {
		out = append(out, s.buttons[i])
	}
	return out
}

// move moves the focus round the ring by a step (1 or -1), from the node that has it; when none has it,
// forward goes to the first node and backward to the last.
func (s *dialogState) move(step int) {
	ring := s.ring()
	at := slices.IndexFunc(ring, func(n *widgets.FocusNode) bool { return n.HasFocus() })
	next := 0
	switch {
	case at < 0 && step < 0:
		next = len(ring) - 1
	case at >= 0:
		next = (at + step + len(ring)) % len(ring)
	}
	ring[next].RequestFocus()
}

// handleKey is the keyboard of the dialog, for the keys the checkbox and the buttons leave.
func (s *dialogState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyTab:
		if kb.IsShiftPressed() {
			s.move(-1)
		} else {
			s.move(1)
		}
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyArrowRight:
		s.move(1)
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyArrowLeft:
		s.move(-1)
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyEscape:
		s.cancel()
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter:
		// the focus is on none of the buttons (they press themselves) or the checkbox: the default
		// button answers
		s.result(0, false)
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

// icon is the code point and color of the icon of the type; 0 for none.
func (d DialogSpec) icon(c Colors) (int, ui.Color) {
	switch d.Type {
	case DialogError:
		return fonts.CodiconError, c.ProblemsErrorIconForeground
	case DialogWarning:
		return fonts.CodiconWarning, c.ProblemsWarningIconForeground
	case DialogInfo, DialogQuestion:
		return fonts.CodiconInfo, c.ProblemsInfoIconForeground
	}
	return 0, ui.Color{}
}

// Build implements widgets.State.
func (s *dialogState) Build(bc widgets.BuildContext) widgets.Widget {
	spec := s.Widget().spec
	colors := ThemeOf(bc)
	fg := colors.EditorWidgetForeground
	textStyle := func(size float64, weight ui.FontWeight, lineH float64) *painting.TextStyle {
		o := painting.TextStyleOpts{
			FontFamily: UIFontOf(bc), FontFamilyFallback: []string{"sans-serif"}, FontSize: foundation.Some(size),
			Color: foundation.Some(fg), FontWeight: weight,
		}
		if lineH > 0 {
			o.Height = foundation.Some(lineH / size)
		}
		return painting.NewTextStyle(o)
	}

	// the text: with a detail, the message is a bold title; without one, the message is the detail
	var text []widgets.Widget
	if spec.Detail != "" {
		text = append(text,
			widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinHeight: foundation.Some(dialogMessageMinH)}),
				widgets.NewAlign(widgets.NewText(spec.Message, widgets.TextOpts{Style: textStyle(dialogMessageFont, ui.FontWeightW600, 0)}), widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(4.0)}),
			widgets.NewText(spec.Detail, widgets.TextOpts{Style: textStyle(dialogDetailFont, 0, dialogDetailLineH)}))
	} else {
		text = append(text, widgets.NewText(spec.Message, widgets.TextOpts{Style: textStyle(dialogDetailFont, 0, dialogDetailLineH)}))
	}
	if spec.CheckboxLabel != "" {
		text = append(text, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, dialogCheckboxTop, 0, 0),
			NewCheckbox(spec.CheckboxLabel, CheckboxOpts{
				Checked: s.checked, LabelColor: foundation.Some(fg), FocusNode: s.check,
				OnChanged: func(v bool) { s.SetState(func() { s.checked = v }) },
			})))
	}
	messageRow := []widgets.Widget{}
	if code, color := spec.icon(colors); code != 0 {
		messageRow = append(messageRow,
			widgets.NewAlign(widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(dialogIconSize), Color: foundation.Some(color)}), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(dialogRowPadLeft)}))
	}
	messageRow = append(messageRow, widgets.NewExpanded(widgets.NewColumn(text, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStart})))

	// the buttons, in the order of the screen
	labels := spec.buttonLabels()
	var buttons []widgets.Widget
	for _, i := range DialogButtonOrder(len(labels), spec.CancelButton) {
		buttons = append(buttons, widgets.NewPadding(painting.NewEdgeInsetsAll(dialogButtonMargin),
			NewButton(dialogButtonLabel(labels[i]), ButtonOpts{
				Key: foundation.NewValueKey(i), Secondary: i != 0, FocusNode: s.buttons[i],
				OnPressed: func() { s.result(i, false) },
				OnEscape:  s.cancel,
			})))
	}

	var toolbar widgets.Widget = widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(dialogToolbarH)})
	if !spec.DisableClose {
		toolbar = widgets.NewSizedBox(widgets.NewAlign(
			NewIconButton(fonts.CodiconClose, IconButtonOpts{Tooltip: "Close Dialog", OnPressed: s.cancel}),
			widgets.AlignOpts{Alignment: painting.AlignmentCenterRight}), widgets.SizedBoxOpts{Height: foundation.Some(dialogToolbarH)})
	}
	column := widgets.NewColumn([]widgets.Widget{
		toolbar,
		widgets.NewFlexible(widgets.NewSingleChildScrollView(
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(dialogRowPadLeft, 0, dialogRowPadRight, 0),
				widgets.NewRow(messageRow, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})))),
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(dialogButtonsLeft, dialogButtonsTop, 0, 0),
			widgets.NewRow(buttons, widgets.RowOpts{MainAxisAlignment: rendering.MainAxisAlignmentEnd})),
	}, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})

	box := widgets.NewContainer(widgets.NewPadding(painting.NewEdgeInsetsAll(dialogPadding), column), widgets.ContainerOpts{
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.EditorWidgetBackground),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(dialogRadius),
			BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: dialogShadowBlur})},
		}),
	})
	// a click on the dialog that lands on nothing focusable brings the focus back into it
	body := widgets.NewGestureDetector(box, widgets.GestureDetectorOpts{
		Behavior: rendering.HitTestBehaviorOpaque,
		OnTap:    s.refocus,
	})
	return widgets.NewFocusScope(
		widgets.NewFocus(body, widgets.FocusOpts{FocusNode: s.box, SkipTraversal: foundation.Some(true), CanRequestFocus: foundation.Some(false)}),
		widgets.FocusScopeOpts{Node: s.scope, OnKeyEvent: s.handleKey, DebugLabel: "Dialog"})
}

// refocus gives the focus back to the dialog when it is not inside it.
func (s *dialogState) refocus() {
	if !s.scope.HasFocus() {
		s.focusDefault()
	}
}

// DialogController queues the dialogs of a window and tells the DialogHost which one to show: Show
// adds a dialog, and the one that is showing is followed by the others, one at a time, in the order
// they were added. It is used from the UI thread only; other goroutines hand a call over with the
// binding's Post.
type DialogController struct {
	current *pendingDialog
	queue   []*pendingDialog
	serial  int
	host    *dialogHostState
}

type pendingDialog struct {
	serial int
	spec   DialogSpec
	// refocus brings the focus into the dialog while it shows.
	refocus func()
}

// NewDialogController makes a controller with no dialog.
func NewDialogController() *DialogController { return &DialogController{} }

// Show adds a dialog. It shows at once when no dialog is showing, else after the ones before it. The
// answer comes through spec.OnAnswer.
func (c *DialogController) Show(spec DialogSpec) {
	c.serial++
	p := &pendingDialog{serial: c.serial, spec: spec}
	if c.current != nil {
		c.queue = append(c.queue, p)
		return
	}
	c.current = p
	c.changed()
}

// IsShowing says whether a dialog is showing (the window is modal while it is).
func (c *DialogController) IsShowing() bool { return c.current != nil }

// Pending is the number of dialogs that wait for the one that is showing.
func (c *DialogController) Pending() int { return len(c.queue) }

// Current is the spec of the dialog that is showing.
func (c *DialogController) Current() (DialogSpec, bool) {
	if c.current == nil {
		return DialogSpec{}, false
	}
	return c.current.spec, true
}

// Dismiss closes the dialog that is showing as if it was cancelled (even one that cannot be closed
// with Escape), and the next one shows. The dialogs that wait stay in the queue.
func (c *DialogController) Dismiss() {
	if p := c.current; p != nil {
		c.answer(p, DialogResult{Button: p.spec.cancelIndex(), Checked: p.spec.CheckboxChecked, Cancelled: true})
	}
}

// Clear drops the dialogs that wait, without answering them.
func (c *DialogController) Clear() { c.queue = nil }

// answer closes a dialog with an answer, shows the next one and gives the answer to the dialog's
// OnAnswer.
func (c *DialogController) answer(p *pendingDialog, r DialogResult) {
	if c.current != p {
		return
	}
	c.current = nil
	if len(c.queue) > 0 {
		c.current, c.queue = c.queue[0], c.queue[1:]
	}
	c.changed()
	if p.spec.OnAnswer != nil {
		p.spec.OnAnswer(r)
	}
}

func (c *DialogController) changed() {
	if c.host != nil {
		c.host.changed()
	}
}

// DialogHostOpts are the options of NewDialogHost.
type DialogHostOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
}

// DialogHost puts the dialogs of a DialogController over its child, which the workbench places once at
// its root. A dialog is modal: while it shows, a scrim (50 % black) covers the child, nothing below it
// gets the pointer or the keyboard focus (the child keeps its state), and the focus is inside the
// dialog; when the last dialog closes, the widget that had the focus before the first one gets it back.
// A click on the scrim brings the focus back into the dialog. Keys that no widget of the dialog takes
// still reach the keybinding dispatcher of the window, which asks the controller (IsShowing) whether a
// dialog is open.
type DialogHost struct {
	widgets.StatefulWidget
	child      widgets.Widget
	controller *DialogController
}

// NewDialogHost makes a host of the dialogs of a controller over a child.
func NewDialogHost(child widgets.Widget, controller *DialogController, opts ...DialogHostOpts) *DialogHost {
	h := &DialogHost{child: child, controller: controller}
	var o DialogHostOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	h.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: o.Key})
	return h
}

// CreateState implements widgets.StatefulWidget.
func (h *DialogHost) CreateState() widgets.State { return &dialogHostState{} }

type dialogHostState struct {
	widgets.StateOf[*DialogHost]
	// restore is the node that had the focus when the first dialog opened.
	restore *widgets.FocusNode
	showing bool
	// bound is the binding of the window.
	bound *widgets.WidgetsBinding
}

// InitState attaches the host to the controller.
func (s *dialogHostState) InitState() {
	s.bound = widgets.WidgetsBindingOf(s.Context())
	s.attach(s.Widget().controller)
	s.showing = s.Widget().controller.IsShowing()
}

func (s *dialogHostState) attach(c *DialogController) { c.host = s }

// DidUpdateWidget follows a new controller.
func (s *dialogHostState) DidUpdateWidget(old *DialogHost) {
	if old.controller != s.Widget().controller {
		if old.controller.host == s {
			old.controller.host = nil
		}
		s.attach(s.Widget().controller)
	}
}

// Dispose detaches the host.
func (s *dialogHostState) Dispose() {
	if c := s.Widget().controller; c.host == s {
		c.host = nil
	}
}

// changed follows the controller: it remembers the focus when the first dialog opens and gives it back
// when the last one closes.
func (s *dialogHostState) changed() {
	if !s.Mounted() {
		return
	}
	showing := s.Widget().controller.IsShowing()
	switch {
	case showing && !s.showing:
		s.restore = s.bound.FocusManager().PrimaryFocus()
	case !showing && s.showing:
		node := s.restore
		s.restore = nil
		scheduler := s.bound.Scheduler()
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(time.Duration) {
			if node != nil && node.Parent() != nil {
				node.RequestFocus()
			}
		})
	}
	s.showing = showing
	s.SetState(func() {})
}

// Build implements widgets.State.
func (s *dialogHostState) Build(widgets.BuildContext) widgets.Widget {
	h := s.Widget()
	c := h.controller
	showing := c.IsShowing()
	// the child is always the first child of the stack and always wrapped the same way, so that it
	// keeps its state while a dialog is open
	children := []widgets.Widget{
		widgets.NewExcludeFocus(
			widgets.NewAbsorbPointer(h.child, widgets.AbsorbPointerOpts{Absorbing: foundation.Some(showing)}),
			widgets.ExcludeFocusOpts{NotExcluding: !showing}),
	}
	if showing {
		p := c.current
		dialog := NewDialog(p.spec, func(r DialogResult) { c.answer(p, r) })
		dialog.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: foundation.NewValueKey(p.serial)})
		dialog.register = func(focus func()) { p.refocus = focus }
		layer := widgets.NewLayoutBuilder(func(_ widgets.BuildContext, box rendering.BoxConstraints) widgets.Widget {
			width := min(dialogWidth, box.MaxWidth*dialogMaxFraction)
			return widgets.NewCenter(widgets.NewConstrainedBox(
				rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{
					MinWidth: foundation.Some(width), MaxWidth: foundation.Some(width), MaxHeight: foundation.Some(box.MaxHeight * dialogMaxFraction),
				}), dialog))
		})
		scrim := widgets.NewGestureDetector(
			widgets.NewColoredBox(dialogScrim, layer),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: s.scrimClicked})
		children = append(children, widgets.NewKeyedSubtree(scrim, widgets.WidgetOpts{Key: foundation.NewValueKey(p.serial)}))
	}
	return widgets.NewStack(children, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// scrimClicked brings the focus back into the dialog, as a click on the modal block does in VS Code.
func (s *dialogHostState) scrimClicked() {
	if p := s.Widget().controller.current; p != nil && p.refocus != nil {
		p.refocus()
	}
}
