package kit

import (
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"
)

// Metrics of the separators of an action bar (`.action-label.separator`).
const (
	separatorLength   = 16.0
	separatorMarginH  = 4.0
	separatorMarginV  = 5.0
	separatorVertical = 4.0
)

// ActionsOrientation is the direction of an ActionBar.
type ActionsOrientation int

// The orientations of an action bar.
const (
	// ActionsHorizontal lays the actions out in a row; Left and Right move the focus.
	ActionsHorizontal ActionsOrientation = iota
	// ActionsVertical lays the actions out in a column; Up and Down move the focus.
	ActionsVertical
)

// Action is an item of an ActionBar (VS Code's `IAction`): a Codicon or a text label, or a
// separator.
type Action struct {
	// ID identifies the action to OnRun's caller.
	ID string
	// Label is the text of a text action, and the tooltip of an icon action unless Tooltip is set.
	Label string
	// Icon is the Codicon of an icon action; 0 for a text action.
	Icon int
	// Tooltip is the text of the hover of the action; the default is Label.
	Tooltip string
	// Checked shows the action as toggled on.
	Checked bool
	// Disabled dims the action and stops it from running.
	Disabled bool
	// Separator makes this a separator: a line between the actions, which never takes the focus.
	Separator bool
}

// SeparatorAction is the separator for a list of actions.
func SeparatorAction() Action { return Action{Separator: true} }

// PrepareActions cleans a list of actions the way VS Code does before a toolbar shows them: it
// drops the separators at the start and at the end, and all but one of several in a row.
//
// Ported from VS Code: src/vs/base/browser/ui/actionbar/actionbar.ts (prepareActions)
func PrepareActions(actions []Action) []Action {
	first := -1
	for i, a := range actions {
		if !a.Separator {
			first = i
			break
		}
	}
	if first < 0 {
		return nil
	}
	out := make([]Action, 0, len(actions)-first)
	for _, a := range actions[first:] {
		if a.Separator && len(out) > 0 && out[len(out)-1].Separator {
			continue
		}
		out = append(out, a)
	}
	if n := len(out); n > 0 && out[n-1].Separator {
		out = out[:n-1]
	}
	return out
}

// ActionBarController moves the focus of an ActionBar from outside.
type ActionBarController struct {
	state *actionBarState
}

// NewActionBarController makes a controller; give it to the ActionBarOpts of one bar.
func NewActionBarController() *ActionBarController { return &ActionBarController{} }

// Focus gives the focus to the item that had it last, or to the first one that can have it.
func (c *ActionBarController) Focus() {
	if c.state != nil {
		c.state.focusFirstOrLast()
	}
}

// FocusIndex gives the focus to the item at an index; it does nothing for a separator or an index
// outside the list.
func (c *ActionBarController) FocusIndex(i int) {
	if c.state != nil {
		c.state.focusItem(i, true)
	}
}

// FocusedIndex is the index of the item that has the focus, or -1.
func (c *ActionBarController) FocusedIndex() int {
	if c.state == nil {
		return -1
	}
	return c.state.focused
}

// ActionBarOpts are the options of NewActionBar.
type ActionBarOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Orientation is the direction of the bar; the default is horizontal.
	Orientation ActionsOrientation
	// OnRun is called with the action the user ran: a click, or Enter or Space (on release, as VS
	// Code's bar does) on the focused item. Disabled actions and separators do not run.
	OnRun func(a Action)
	// OnEscape is called when Escape is pressed while an item has the focus; the bar gives up the
	// focus first, so a callback can move it elsewhere. Without it Escape goes on to the widgets above.
	OnEscape func()
	// FocusOnlyEnabledItems keeps the focus off the disabled items (VS Code's option of the same
	// name; off by default there too).
	FocusOnlyEnabledItems bool
	// PreventLoopNavigation stops the focus at the first and last item instead of wrapping around.
	PreventLoopNavigation bool
	// Controller, when set, moves the focus from outside.
	Controller *ActionBarController
	// HoverDelegate is the group of hovers of the items; the default is a group of the bar's own
	// that shows a hover at once when another hid less than 200 ms ago.
	HoverDelegate *HoverDelegate
	// Autofocus gives the focus to the first item that can have it when the bar is shown.
	Autofocus bool
}

// ActionBar is VS Code's action bar: a row (or a column) of icon actions, text actions and
// separators. Its items have the look of an IconButton: the toolbar background under the pointer,
// a darker one while pressed, the toggled one when checked, 60 % for a disabled icon, a focus
// outline and a tooltip. One item at a time is in the tab order; the keyboard moves the focus
// between the items (Left and Right, or Up and Down for a vertical bar, wrapping around unless
// PreventLoopNavigation; Home and End), shows the tooltip of the item that gets the focus, runs
// the focused item on Enter and Space, and calls OnEscape on Escape.
//
// A checked item is always shown toggled (VS Code needs `highlightToggledItems` for that).
//
// Ported from VS Code: src/vs/base/browser/ui/actionbar/actionbar.ts, actionViewItems.ts and actionbar.css
type ActionBar struct {
	widgets.StatefulWidget
	actions []Action
	opts    ActionBarOpts
}

// NewActionBar makes an action bar over a list of actions.
func NewActionBar(actions []Action, opts ...ActionBarOpts) *ActionBar {
	b := &ActionBar{actions: actions}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	b.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: b.opts.Key})
	return b
}

// Actions are the actions of the bar.
func (b *ActionBar) Actions() []Action { return b.actions }

// CreateState implements widgets.StatefulWidget.
func (b *ActionBar) CreateState() widgets.State { return &actionBarState{focused: -1, lastFocused: -1} }

type actionBarState struct {
	widgets.StateOf[*ActionBar]
	nodes    []*widgets.FocusNode
	hovers   []*HoverController
	delegate *HoverDelegate
	ctrl     *ActionBarController
	// focused is the item that has the focus, or -1; lastFocused is the one that had it last, which
	// stays in the tab order.
	focused, lastFocused int
	// triggerDown is true between the press and the release of Enter or Space.
	triggerDown bool
}

// InitState makes the delegate and attaches the controller.
func (s *actionBarState) InitState() {
	s.delegate = s.Widget().opts.HoverDelegate
	if s.delegate == nil {
		s.delegate = NewHoverDelegate(HoverDelegateOpts{Instant: true})
	}
	s.attach()
	if s.Widget().opts.Autofocus {
		scheduler := widgets.WidgetsBindingOf(s.Context()).Scheduler()
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(_ time.Duration) {
			if s.Mounted() {
				s.focusFirstOrLast()
			}
		})
	}
}

func (s *actionBarState) attach() {
	if s.ctrl != nil && s.ctrl.state == s {
		s.ctrl.state = nil
	}
	s.ctrl = s.Widget().opts.Controller
	if s.ctrl != nil {
		s.ctrl.state = s
	}
}

// DidUpdateWidget follows a new controller and a changed number of actions.
func (s *actionBarState) DidUpdateWidget(*ActionBar) {
	s.attach()
	if s.focused >= len(s.Widget().actions) {
		s.focused = -1
	}
}

// Dispose releases the focus nodes.
func (s *actionBarState) Dispose() {
	for _, n := range s.nodes {
		n.Dispose()
	}
	if s.ctrl != nil && s.ctrl.state == s {
		s.ctrl.state = nil
	}
}

// ensure makes a focus node and a hover controller for each action.
func (s *actionBarState) ensure() {
	n := len(s.Widget().actions)
	for len(s.nodes) < n {
		s.nodes = append(s.nodes, widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "ActionBar item"}))
		s.hovers = append(s.hovers, NewHoverController())
	}
	for len(s.nodes) > n {
		last := len(s.nodes) - 1
		s.nodes[last].Dispose()
		s.nodes, s.hovers = s.nodes[:last], s.hovers[:last]
	}
}

// canFocus says whether the item at an index can take the focus.
func (s *actionBarState) canFocus(i int) bool {
	actions := s.Widget().actions
	if i < 0 || i >= len(actions) || actions[i].Separator {
		return false
	}
	return !actions[i].Disabled || !s.Widget().opts.FocusOnlyEnabledItems
}

// tabStop is the item that is in the tab order: the one that had the focus last, else the first
// that can have it.
func (s *actionBarState) tabStop() int {
	if s.canFocus(s.lastFocused) {
		return s.lastFocused
	}
	for i := range s.Widget().actions {
		if s.canFocus(i) {
			return i
		}
	}
	return -1
}

func (s *actionBarState) itemFocusChanged(i int, has bool) {
	switch {
	case has:
		s.focused, s.lastFocused = i, i
	case s.focused == i:
		s.focused = -1
		s.triggerDown = false
	}
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// focusItem gives the focus to an item and, with showHover, shows its tooltip.
func (s *actionBarState) focusItem(i int, showHover bool) {
	s.ensure()
	if !s.canFocus(i) {
		return
	}
	s.nodes[i].RequestFocus()
	if showHover {
		s.hovers[i].Show()
	}
}

func (s *actionBarState) focusFirstOrLast() {
	if i := s.tabStop(); i >= 0 {
		s.focusItem(i, false)
	}
}

// move moves the focus by one item in a direction, skipping separators and, with
// FocusOnlyEnabledItems, disabled items. It says whether it handled the key.
func (s *actionBarState) move(dir int, forceLoop bool) bool {
	n := len(s.Widget().actions)
	if n <= 1 {
		return false
	}
	from := s.focused
	if from < 0 {
		from = s.lastFocused
	}
	if from < 0 {
		if dir > 0 {
			from = n - 1
		} else {
			from = 0
		}
	}
	i := from
	for {
		next := i + dir
		if next < 0 || next >= n {
			if !forceLoop && s.Widget().opts.PreventLoopNavigation {
				return false
			}
			next = (next + n) % n
		}
		i = next
		if i == from {
			break
		}
		if s.canFocus(i) {
			break
		}
	}
	if s.canFocus(i) {
		s.focusItem(i, true)
	}
	return true
}

func (s *actionBarState) first() bool {
	for i := range s.Widget().actions {
		if s.canFocus(i) {
			s.focusItem(i, true)
			return true
		}
	}
	return false
}

func (s *actionBarState) last() bool {
	actions := s.Widget().actions
	for i := len(actions) - 1; i >= 0; i-- {
		if s.canFocus(i) {
			s.focusItem(i, true)
			return true
		}
	}
	return false
}

func (s *actionBarState) runIndex(i int) {
	actions := s.Widget().actions
	if i < 0 || i >= len(actions) || actions[i].Separator || actions[i].Disabled {
		return
	}
	if f := s.Widget().opts.OnRun; f != nil {
		f(actions[i])
	}
}

// handleKey is the keyboard of the bar; the items' key events bubble up to it.
func (s *actionBarState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	o := s.Widget().opts
	key := e.LogicalKey()
	switch e.(type) {
	case *services.KeyUpEvent:
		if isActivateKey(key) && s.focused >= 0 {
			if s.triggerDown {
				s.triggerDown = false
				s.runIndex(s.focused)
			}
			return widgets.KeyEventResultHandled
		}
		return widgets.KeyEventResultIgnored
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	prev, next := services.LogicalKeyboardKeyArrowLeft, services.LogicalKeyboardKeyArrowRight
	if o.Orientation == ActionsVertical {
		prev, next = services.LogicalKeyboardKeyArrowUp, services.LogicalKeyboardKeyArrowDown
	}
	handled := true
	switch {
	case key == prev:
		handled = s.move(-1, false)
	case key == next:
		handled = s.move(1, false)
	case key == services.LogicalKeyboardKeyEscape && o.OnEscape != nil:
		// Glutter applies the last focus request: give the focus up first, so that a callback that
		// moves it elsewhere wins
		for _, n := range s.nodes {
			if n.HasFocus() {
				n.Unfocus()
				break
			}
		}
		o.OnEscape()
	case key == services.LogicalKeyboardKeyHome:
		handled = s.first()
	case key == services.LogicalKeyboardKeyEnd:
		handled = s.last()
	case isActivateKey(key):
		if _, repeat := e.(*services.KeyRepeatEvent); !repeat {
			s.triggerDown = true
		}
	default:
		handled = false
	}
	if handled {
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

// Build implements widgets.State.
func (s *actionBarState) Build(bc widgets.BuildContext) widgets.Widget {
	b := s.Widget()
	s.ensure()
	colors := ThemeOf(bc)
	vertical := b.opts.Orientation == ActionsVertical
	tab := s.tabStop()
	children := make([]widgets.Widget, len(b.actions))
	for i, a := range b.actions {
		if a.Separator {
			children[i] = actionSeparator(colors, vertical)
			continue
		}
		tooltip := a.Tooltip
		if tooltip == "" {
			tooltip = a.Label
		}
		if a.Icon == 0 && a.Tooltip == "" {
			tooltip = "" // a text action says what it is
		}
		index := i
		children[i] = newActionItem(actionItemProps{
			icon: a.Icon, label: a.Label, tooltip: tooltip, checked: a.Checked,
			disabled: a.Disabled, focusable: s.canFocus(i), disabledAlpha: 0x99, alignLeft: vertical,
			run: func() { s.runIndex(index) },
			// the bar handles the keys so that it can run the item on release
			keys: false, node: s.nodes[i], skipTraversal: i != tab,
			onFocusChange: func(has bool) { s.itemFocusChanged(index, has) },
			delegate:      s.delegate, hover: s.hovers[i],
		})
	}
	var layout widgets.Widget
	if vertical {
		layout = widgets.NewIntrinsicWidth(widgets.NewColumn(children, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	} else {
		layout = widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	}
	return widgets.NewFocus(layout, widgets.FocusOpts{
		OnKeyEvent: s.handleKey, CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "ActionBar",
	})
}

// actionSeparator is a separator: a 1 px by 16 px bar with margins of 5 px by 4 px in a row, or a line
// across a column.
func actionSeparator(colors Colors, vertical bool) widgets.Widget {
	line := painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(colors.DisabledForeground)})
	if vertical {
		return widgets.NewPadding(painting.NewEdgeInsetsSymmetric(separatorVertical, 0),
			widgets.NewContainer(nil, widgets.ContainerOpts{Height: foundation.Some(1.0), Decoration: line}))
	}
	return widgets.NewPadding(painting.NewEdgeInsetsSymmetric(separatorMarginV, separatorMarginH),
		widgets.NewContainer(nil, widgets.ContainerOpts{Width: foundation.Some(1.0), Height: foundation.Some(separatorLength), Decoration: line}))
}
