package quickinput

import (
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Metrics of the quick input widget, from VS Code's quickInput.css and quickInputController.ts.
const (
	// MaxWidth is the widest the widget gets; it is 62% of the window when that is less.
	MaxWidth = 600.0
	// MaxListHeight is the height of 20 rows, the most the list shows before it scrolls.
	MaxListHeight = 20 * RowHeight
	widgetRadius  = 6.0
	widgetShadow  = 8.0
	headerPadding = 6.0
	listBottomPad = 7.0
	entryPadding  = 6.0
	rowRadius     = 3.0
	rowFontSize   = 13.0
)

// HostOptions are the options of NewHost.
type HostOptions struct {
	// Controller is what the host shows. Required.
	Controller *Controller
	// Keys is the root scope of the context keys of the window; the host makes a scope below it
	// for the quick input, with inQuickOpen, inQuickInput, inputFocus and quickInputType, and the
	// context key of the provider, set while the quick input is shown.
	Keys *contextkey.Scope
	// Scopes registers the focus node of the input with that scope, so that the key dispatcher
	// resolves keys in it.
	Scopes *kit.FocusScopes
	// Commands is the registry whose workbench.action.closeQuickOpen command the host takes over
	// while it is on screen.
	Commands *command.Registry
	// Settings gives workbench.quickOpen.closeOnFocusLost; without it the quick input closes when it
	// loses the focus.
	Settings *settings.Store
	// TopInset is the height of what the widget sits under: the menu bar.
	TopInset float64
}

// Host puts the quick input over its child, which the workbench places once at its root (as
// kit.DialogHost does with dialogs): a widget centered at the top of the window, under the menu bar,
// with the input box above the list of the picker. It follows the Controller; when the quick input
// opens it takes the focus and when it closes it gives the focus back to the widget that had it.
//
// Ported from VS Code: src/vs/platform/quickinput/browser/quickInputController.ts,
// quickInputList.ts and media/quickInput.css. Not ported: the title bar, the buttons and the
// message of the input, check boxes, the progress bar, dragging the widget.
type Host struct {
	widgets.StatefulWidget
	child widgets.Widget
	opts  HostOptions
}

// NewHost makes the host of the quick input of a controller over a child.
func NewHost(child widgets.Widget, opts HostOptions) *Host {
	return &Host{StatefulWidget: widgets.NewStatefulWidgetBase(), child: child, opts: opts}
}

// CreateState implements widgets.StatefulWidget.
func (h *Host) CreateState() widgets.State { return &hostState{} }

type hostState struct {
	widgets.StateOf[*Host]
	ctl   *Controller
	input *kit.InputBoxController
	// frame is the focus node around the widget: it tells when the focus leaves it.
	frame *widgets.FocusNode
	scope *contextkey.Scope

	picker       *Picker
	removeCtl    func()
	removePicker func()
	unregister   func()
	removeCmd    func()

	wasVisible bool
	hadFocus   bool
	restore    *widgets.FocusNode
	// scroll is how far the list is scrolled, in pixels; listH and maxList are the height of its view
	// and the most it can have; version is the Version of the picker the scroll belongs to.
	scroll, listH, maxList float64
	version                int
	hover                  int
	providerKey            string

	// keyHandle is the handler of the keyboard that sees the release of the keys that opened a switcher,
	// whatever has the focus; observer tells when the window loses the keyboard focus.
	keyHandle services.KeyHandlerHandle
	observer  *viewFocusObserver
	// bound is the binding the handlers were added to, which Dispose cannot look up again.
	bound *widgets.WidgetsBinding
}

// viewFocusObserver tells the host when the window gets or loses the keyboard focus.
type viewFocusObserver struct {
	widgets.WidgetsBindingObserverBase
	onFocus func(focused bool)
}

// DidChangeViewFocus implements widgets.WidgetsBindingObserver.
func (o *viewFocusObserver) DidChangeViewFocus(e ui.ViewFocusEvent) {
	o.onFocus(e.State == ui.ViewFocusStateFocused)
}

// InitState follows the controller and takes the command.
func (s *hostState) InitState() {
	o := s.Widget().opts
	s.ctl = o.Controller
	s.input = kit.NewInputBoxController("")
	s.input.FocusNode().SetOnKeyEvent(s.handleKey)
	s.frame = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "QuickInput", SkipTraversal: true})
	s.frame.SetCanRequestFocus(false)
	s.maxList = MaxListHeight
	s.hover = -1
	if o.Keys != nil {
		s.scope = o.Keys.Child()
		if o.Scopes != nil {
			s.unregister = o.Scopes.Register(s.input.FocusNode(), s.scope)
		}
	}
	if o.Commands != nil {
		if remove, err := o.Commands.SetHandler("workbench.action.closeQuickOpen", func(command.Context, any) error {
			s.ctl.Hide()
			return nil
		}); err == nil {
			s.removeCmd = remove
		}
	}
	s.removeCtl = s.ctl.OnChange(s.controllerChanged)
	s.wasVisible = s.ctl.Visible()
	s.followPicker()
	s.bound = s.binding()
	s.keyHandle = s.bound.Keyboard().AddHandler(s.hardwareKey)
	s.observer = &viewFocusObserver{onFocus: s.windowFocusChanged}
	s.bound.AddObserver(s.observer)
}

// Dispose stops following the controller and gives the command back.
func (s *hostState) Dispose() {
	s.bound.Keyboard().RemoveHandler(s.keyHandle)
	s.bound.RemoveObserver(s.observer)
	if s.removeCtl != nil {
		s.removeCtl()
	}
	if s.removePicker != nil {
		s.removePicker()
	}
	if s.unregister != nil {
		s.unregister()
	}
	if s.removeCmd != nil {
		s.removeCmd()
	}
	if s.scope != nil {
		s.scope.Dispose()
	}
	s.frame.Dispose()
	s.input.Dispose()
}

// followPicker subscribes to the picker the controller shows now.
func (s *hostState) followPicker() {
	if s.removePicker != nil {
		s.removePicker()
		s.removePicker = nil
	}
	s.picker = s.ctl.Picker()
	s.removePicker = s.picker.OnChange(s.pickerChanged)
}

func (s *hostState) binding() *widgets.WidgetsBinding { return widgets.WidgetsBindingOf(s.Context()) }

// controllerChanged is told that the quick input was shown, hidden or given another picker.
func (s *hostState) controllerChanged() {
	if !s.Mounted() {
		return
	}
	if s.picker != s.ctl.Picker() {
		s.followPicker()
	}
	visible := s.ctl.Visible()
	switch {
	case visible && !s.wasVisible:
		s.restore = s.binding().FocusManager().PrimaryFocus()
		s.scroll, s.hadFocus = 0, false
		s.syncKeys(true)
		s.syncInput(true)
		s.focusInputLater()
	case !visible && s.wasVisible:
		s.syncKeys(false)
		s.giveFocusBack()
	case visible:
		s.syncKeys(true)
		s.syncInput(true)
		s.focusInputLater()
	}
	s.wasVisible = visible
	s.SetState(func() {})
}

// pickerChanged follows what the picker shows: its text, its rows and its active row.
func (s *hostState) pickerChanged() {
	if !s.Mounted() || !s.ctl.Visible() || s.picker != s.ctl.Picker() {
		return
	}
	if v := s.picker.Version(); v != s.version {
		s.version = v
		if !s.picker.KeepScrollPosition {
			s.scroll = 0
		}
	}
	s.syncInput(true)
	s.ensureActiveVisible()
	s.SetState(func() {})
}

// syncInput puts the text of the picker in the input. The selection asked for is applied once the
// input has the focus, which would select all of the text on its own.
func (s *hostState) syncInput(applySelection bool) {
	if s.input.Value() != s.picker.Value() {
		s.input.SetValue(s.picker.Value())
	}
	if applySelection && s.input.HasFocus() {
		s.applySelection()
	}
}

// applySelection selects the part of the text that the picker asked for.
func (s *hostState) applySelection() {
	if start, end, ok := s.picker.ValueSelection(); ok {
		s.picker.ClearValueSelection()
		s.input.Select(start, end)
	}
}

// focusInputLater gives the input the focus once the widget is built, then selects what was asked for.
func (s *hostState) focusInputLater() {
	scheduler := s.binding().Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if !s.Mounted() || !s.ctl.Visible() {
			return
		}
		s.input.Focus()
		s.applySelection()
	})
}

// giveFocusBack returns the focus to the widget that had it when the quick input opened.
func (s *hostState) giveFocusBack() {
	node := s.restore
	s.restore = nil
	if node == nil || node.Context() == nil || !node.CanRequestFocus() {
		return
	}
	node.RequestFocus()
}

// syncKeys sets the context keys of the quick input while it is shown.
func (s *hostState) syncKeys(visible bool) {
	if s.scope == nil {
		return
	}
	s.scope.Batch(func() {
		if s.providerKey != "" {
			s.scope.Reset(s.providerKey)
			s.providerKey = ""
		}
		if !visible {
			for _, k := range []string{"inQuickOpen", "inQuickInput", "inputFocus", "quickInputType"} {
				s.scope.Reset(k)
			}
			return
		}
		s.scope.Set("inQuickOpen", true)
		s.scope.Set("inQuickInput", true)
		s.scope.Set("inputFocus", true)
		if s.ctl.Prompting() {
			s.scope.Set("quickInputType", "inputBox")
		} else {
			s.scope.Set("quickInputType", "quickPick")
		}
		if p := s.ctl.Provider(); p != nil {
			if k := p.Info().ContextKey; k != "" {
				s.scope.Set(k, true)
				s.providerKey = k
			}
		}
	})
}

// closeOnFocusLost is workbench.quickOpen.closeOnFocusLost.
func (s *hostState) closeOnFocusLost() bool {
	st := s.Widget().opts.Settings
	if st == nil {
		return true
	}
	return settings.Get[bool](st, "workbench.quickOpen.closeOnFocusLost", "")
}

// mods reads the modifier keys that are held. A switcher opened by held keys ignores them: the keys that
// opened it are held on purpose (VS Code turns the key modifiers off in the quick navigate mode).
func (s *hostState) mods() KeyMods {
	if s.ctl.QuickMode() {
		return KeyMods{}
	}
	kb := s.binding().Keyboard()
	return KeyMods{Ctrl: kb.IsControlPressed(), Alt: kb.IsAltPressed()}
}

// held reads the modifier keys that are held.
func (s *hostState) held() Modifiers {
	kb := s.binding().Keyboard()
	return Modifiers{Ctrl: kb.IsControlPressed(), Shift: kb.IsShiftPressed(), Alt: kb.IsAltPressed(), Meta: kb.IsMetaPressed()}
}

// modifierOf is the modifier a key is, or 0.
func modifierOf(k services.LogicalKeyboardKey) Modifier {
	switch k {
	case services.LogicalKeyboardKeyControl, services.LogicalKeyboardKeyControlLeft, services.LogicalKeyboardKeyControlRight:
		return ModCtrl
	case services.LogicalKeyboardKeyShift, services.LogicalKeyboardKeyShiftLeft, services.LogicalKeyboardKeyShiftRight:
		return ModShift
	case services.LogicalKeyboardKeyAlt, services.LogicalKeyboardKeyAltLeft, services.LogicalKeyboardKeyAltRight:
		return ModAlt
	case services.LogicalKeyboardKeyMeta, services.LogicalKeyboardKeyMetaLeft, services.LogicalKeyboardKeyMetaRight:
		return ModMeta
	}
	return 0
}

// hardwareKey sees every key of the window, whatever has the focus. While a switcher waits for the release of
// the keys that opened it, the release of the modifier chooses the active row. A key that is pressed while
// the modifier is not held means that the release was missed (the focus was somewhere else when it came):
// the switcher stops waiting and works as any picker. It never uses the event.
func (s *hostState) hardwareKey(e services.KeyEvent) bool {
	if !s.Mounted() || !s.ctl.QuickNavigating() {
		return false
	}
	mod := modifierOf(e.LogicalKey())
	switch e.(type) {
	case *services.KeyUpEvent:
		if mod != 0 {
			s.ctl.QuickNavigateKeyUp(mod, s.held())
		}
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
		if mod == 0 && !s.ctl.QuickHeld(s.held()) {
			s.ctl.DisarmQuickNavigate()
		}
	}
	return false
}

// windowFocusChanged follows the keyboard focus of the window. A window that lost it will not send the
// release of the keys any more: the switcher stops waiting for it, and closes when workbench.quickOpen.closeOnFocusLost
// asks for it.
func (s *hostState) windowFocusChanged(focused bool) {
	if focused || !s.Mounted() || !s.ctl.Visible() {
		return
	}
	s.ctl.DisarmQuickNavigate()
	if s.closeOnFocusLost() {
		s.ctl.Hide()
	}
}

// handleKey is the keyboard of the quick input; the input box sees Enter first.
func (s *hostState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	if !s.ctl.Visible() {
		return widgets.KeyEventResultIgnored
	}
	kb := s.binding().Keyboard()
	p := s.picker
	rows := max(int(s.listH/RowHeight), 1)
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyArrowDown:
		p.Move(1)
	case services.LogicalKeyboardKeyArrowUp:
		p.Move(-1)
	case services.LogicalKeyboardKeyPageDown:
		p.Page(1, rows)
	case services.LogicalKeyboardKeyPageUp:
		p.Page(-1, rows)
	case services.LogicalKeyboardKeyHome:
		if !kb.IsControlPressed() {
			return widgets.KeyEventResultIgnored
		}
		p.First()
	case services.LogicalKeyboardKeyEnd:
		if !kb.IsControlPressed() {
			return widgets.KeyEventResultIgnored
		}
		p.Last()
	case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter:
		s.ctl.Accept(s.mods())
	case services.LogicalKeyboardKeyEscape:
		s.ctl.Hide()
	default:
		return widgets.KeyEventResultIgnored
	}
	return widgets.KeyEventResultHandled
}

// listContentHeight is the height of all the rows.
func (s *hostState) listContentHeight() float64 {
	h := 0.0
	for _, r := range s.picker.Rows() {
		h += r.Height()
	}
	return h
}

// rowTop is the offset of a row from the top of the list.
func (s *hostState) rowTop(i int) float64 {
	h := 0.0
	for _, r := range s.picker.Rows()[:min(i, len(s.picker.Rows()))] {
		h += r.Height()
	}
	return h
}

// ensureActiveVisible scrolls the list so that the active row is in view.
func (s *hostState) ensureActiveVisible() {
	i := s.picker.ActiveIndex()
	if i < 0 {
		return
	}
	view := min(s.listContentHeight(), s.maxList)
	top := s.rowTop(i)
	bottom := top + s.picker.Rows()[i].Height()
	switch {
	case top < s.scroll:
		s.scroll = top
	case bottom > s.scroll+view:
		s.scroll = bottom - view
	}
	s.clampScroll()
}

func (s *hostState) clampScroll() {
	view := min(s.listContentHeight(), s.maxList)
	s.scroll = min(max(s.scroll, 0), max(s.listContentHeight()-view, 0))
}

func (s *hostState) wheel(e gestures.PointerSignalEvent) {
	scroll, ok := e.(*gestures.PointerScrollEvent)
	if !ok {
		return
	}
	s.scroll += scroll.ScrollDelta().Dy
	s.clampScroll()
	s.SetState(func() {})
}

func (s *hostState) setHover(i int) {
	if s.hover != i && s.Mounted() {
		s.SetState(func() { s.hover = i })
	}
}

// Build implements widgets.State.
func (s *hostState) Build(bc widgets.BuildContext) widgets.Widget {
	h := s.Widget()
	children := []widgets.Widget{h.child}
	if s.ctl.Visible() {
		children = append(children, widgets.NewPositioned(
			widgets.NewLayoutBuilder(func(bc widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget { return s.layout(bc, c) }),
			widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0), Top: foundation.Some(h.opts.TopInset), Bottom: foundation.Some(0.0)}))
	}
	return widgets.NewStack(children, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// layout sizes the widget for the room it has and builds it.
func (s *hostState) layout(bc widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
	width := min(c.MaxWidth*0.62, MaxWidth)
	// the list takes at most 20 rows, and not more than 60% of what is left of the window
	s.maxList = min(MaxListHeight, max(c.MaxHeight*0.6-60, RowHeight))
	s.clampScroll()
	return widgets.NewAlign(
		widgets.NewSizedBox(s.widget(bc, width), widgets.SizedBoxOpts{Width: foundation.Some(width)}),
		widgets.AlignOpts{Alignment: painting.AlignmentTopCenter})
}

// widget is the quick input: the input over the list.
func (s *hostState) widget(bc widgets.BuildContext, width float64) widgets.Widget {
	colors := kit.ThemeOf(bc)
	pk := s.picker
	var message *kit.InputMessage
	if m := pk.Message(); m != "" {
		message = &kit.InputMessage{Content: m, Type: kit.ValidationError}
	}
	input := kit.NewInputBox(nil, nil, kit.InputBoxOpts{
		Controller:       s.input,
		Placeholder:      pk.Placeholder(),
		Message:          message,
		SelectAllOnFocus: foundation.Some(false),
		OnChanged:        func(text string) { s.ctl.SetValue(text) },
		OnSubmit:         func(string) { s.ctl.Accept(s.mods()) },
	})
	var col []widgets.Widget
	if pk.HideInput {
		// The switcher has no input to see, but the input keeps the keyboard focus.
		col = append(col, widgets.NewSizedBox(widgets.NewClipRect(input), widgets.SizedBoxOpts{Height: foundation.Some(0.0)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(headerPadding)}))
	} else {
		col = append(col, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(headerPadding, headerPadding, headerPadding, 4), input))
	}
	if len(pk.Rows()) > 0 {
		col = append(col, s.list(bc, colors))
	} else {
		col = append(col, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(2.0)}))
	}
	box := widgets.NewContainer(
		widgets.NewColumn(col, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
		widgets.ContainerOpts{
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color:        foundation.Some(colors.QuickInputBackground),
				Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)}),
				BorderRadius: painting.NewBorderRadiusCircular(widgetRadius),
				BoxShadow: []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{
					Color: foundation.Some(colors.WidgetShadow), BlurRadius: widgetShadow, SpreadRadius: 2})},
			}),
		})
	return widgets.NewTapRegion(
		widgets.NewFocus(box, widgets.FocusOpts{FocusNode: s.frame, OnFocusChange: s.frameFocusChanged}),
		widgets.TapRegionOpts{OnTapOutside: func(*gestures.PointerDownEvent) {
			if s.closeOnFocusLost() {
				s.ctl.Hide()
			}
		}})
}

// frameFocusChanged hides the quick input when the focus leaves it.
func (s *hostState) frameFocusChanged(has bool) {
	if has {
		s.hadFocus = true
		return
	}
	if s.hadFocus && s.ctl.Visible() && s.closeOnFocusLost() {
		s.hadFocus = false
		s.ctl.Hide()
	}
}

// list is the rows in view.
func (s *hostState) list(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	rows := s.picker.Rows()
	content := s.listContentHeight()
	view := min(content, s.maxList)
	s.listH = view
	var kids []widgets.Widget
	top := 0.0
	for i, r := range rows {
		h := r.Height()
		if top+h > s.scroll && top < s.scroll+view {
			kids = append(kids, widgets.NewPositioned(s.row(bc, colors, i, r), widgets.PositionedOpts{
				Left: foundation.Some(0.0), Right: foundation.Some(0.0), Top: foundation.Some(top - s.scroll), Height: foundation.Some(h)}))
		}
		top += h
	}
	area := widgets.NewClipRect(widgets.NewStack(kids, widgets.StackOpts{Fit: rendering.StackFitExpand}))
	var outer []widgets.Widget
	outer = append(outer, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(entryPadding, 0, entryPadding, listBottomPad),
		widgets.NewSizedBox(area, widgets.SizedBoxOpts{Height: foundation.Some(view)})))
	if content > view {
		// the scrollbar sits at the right edge of the list, over the padding
		thumb := max(view*view/content, 20)
		y := (view - thumb) * s.scroll / (content - view)
		outer = append(outer, widgets.NewPositioned(
			widgets.NewIgnorePointer(widgets.NewContainer(nil, widgets.ContainerOpts{
				Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(colors.ScrollbarSliderBackground)})})),
			widgets.PositionedOpts{Right: foundation.Some(0.0), Top: foundation.Some(y), Width: foundation.Some(10.0), Height: foundation.Some(thumb)}))
	}
	return widgets.NewListener(
		widgets.NewSizedBox(widgets.NewStack(outer, widgets.StackOpts{Fit: rendering.StackFitPassthrough}), widgets.SizedBoxOpts{Height: foundation.Some(view + listBottomPad)}),
		widgets.ListenerOpts{OnPointerSignal: s.wheel})
}
