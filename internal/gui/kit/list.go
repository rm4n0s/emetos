package kit

// Ported from VS Code: src/vs/base/browser/ui/list/listWidget.ts and listView.ts (the rows, the mouse and the
// keys of a list; only the rows in view are built), src/vs/base/browser/ui/list/list.css and
// src/vs/platform/theme/common/colors/listColors.ts (the look of the rows). Not ported: type navigation by
// the start of a label (the find takes the letters), horizontal scrolling of wide rows, accessibility.

import (
	"fmt"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// ListRenderer builds the contents of a row of a List. The row is as high as the model says and as wide as the
// list; the renderer is given the row and the state of its look, and draws what it wants (an icon, a label with
// NewHighlightedLabel) in the foreground the state names. It is called while the rows in view are laid out, so
// it must be quick and must not change the model.
type ListRenderer func(bc widgets.BuildContext, row ListViewRow, state RowState) widgets.Widget

// ListOpts are the options of NewList.
type ListOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Controller gives access to the list from outside (reveal, scroll, focus).
	Controller *ListController
	// Render builds the contents of a row. The default shows the label of the row (its Data when that is a
	// string) in one line of 13 px text, with the matches of a find highlighted.
	Render ListRenderer
	// MultiSelect lets Ctrl+click toggle a row in the selection, Shift+click select the range from the anchor
	// (both together add the range), Shift+Up and Shift+Down extend the selection, Ctrl+A select all rows and
	// Ctrl+Space (or Ctrl+Shift+Enter) toggle the focused row; Escape reduces several selected rows to the
	// focused one.
	MultiSelect bool
	// DragAndDrop makes the rows draggable and the list a drop target; see DragAndDrop.
	DragAndDrop *DragAndDrop
	// TypeToFind makes a letter typed into the list start the find box (the default is true).
	TypeToFind foundation.Opt[bool]
	// Autofocus gives the list the keyboard focus when it is first shown.
	Autofocus bool
	// OnSelect is called when the user selects a row: a click on it, Enter or Space. The row is selected in
	// the model already.
	OnSelect func(row ListViewRow)
	// OnOpen is called when the user opens a row: Enter, or a double click.
	OnOpen func(row ListViewRow)
	// OnSelectionChange is called when the user changed the selected rows (a click, Ctrl+click, Shift+click, a
	// key), with their keys and indexes in the order of the rows. It is not called for changes the program
	// makes.
	OnSelectionChange func(keys []string, indexes []int)
}

// List is a virtualized list of rows with a keyboard: VS Code's list. It shows a ListViewModel in a Glutter
// sliver list inside a CustomScrollView with the overlay scrollbars of ScrollableElement; only the rows in view
// (and a margin of 250 px) are built. Rows are 22 px high, or as high as the model's HeightOf says. The
// selected rows, the focused row and the row under the pointer are colored as the rows of a Tree are. With
// MultiSelect several rows can be selected; typing starts a find box that highlights or filters the rows; with
// DragAndDrop rows can be dragged and dropped.
//
// The model is used on the UI thread only.
type List struct {
	widgets.StatefulWidget
	model *ListViewModel
	opts  ListOpts
}

// NewList makes a list that shows the model.
func NewList(model *ListViewModel, opts ...ListOpts) *List {
	l := &List{model: model}
	if len(opts) > 0 {
		l.opts = opts[0]
	}
	l.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: l.opts.Key})
	return l
}

// CreateState implements widgets.StatefulWidget.
func (l *List) CreateState() widgets.State { return &listState{} }

// ListController reads and moves the list from outside. Make one with NewListController and give it to the
// list; it belongs to one list. The focus, the selection and the find are in the ListViewModel.
type ListController struct {
	state *listState
}

// NewListController makes a controller that is not attached to a list yet; until it is, its methods do
// nothing.
func NewListController() *ListController { return &ListController{} }

// Reveal scrolls the list, after the next frame, so that the row of a key is in view (with the least
// scrolling). It reports whether the list has a row with the key.
func (c *ListController) Reveal(key string) bool {
	if c.state == nil {
		return false
	}
	return c.state.reveal(key)
}

// ScrollTo scrolls the list to a position, in pixels from the top of its rows.
func (c *ListController) ScrollTo(offset float64) {
	if c.state != nil {
		c.state.jump(offset)
	}
}

// ScrollOffset is how far the list is scrolled, in pixels.
func (c *ListController) ScrollOffset() float64 {
	if c.state == nil || !c.state.scroll.HasClients() {
		return 0
	}
	return c.state.scroll.Offset()
}

// ViewportHeight is the height of the list's view in pixels, 0 before the first layout.
func (c *ListController) ViewportHeight() float64 {
	if c.state == nil {
		return 0
	}
	_, h := c.state.viewport()
	return h
}

// Focus gives the list the keyboard focus. A list with no focused row focuses the selected row or the first.
func (c *ListController) Focus() {
	if c.state != nil {
		c.state.node.RequestFocus()
	}
}

// HasFocus is whether the list has the keyboard focus.
func (c *ListController) HasFocus() bool { return c.state != nil && c.state.hasFocus }

type listState struct {
	widgets.StateOf[*List]
	model  *ListViewModel
	handle foundation.ListenerHandle
	scroll *widgets.ScrollControllerBase
	node   *widgets.FocusNode

	hasFocus bool
	hover    string
	drag     dragController
	find     *findBoxState
	areaCtx  widgets.BuildContext

	// the press that may be the first of a double click, and how many presses the last one made
	lastKey          string
	lastStamp        time.Duration
	lastAt           ui.Offset
	hasLast          bool
	clicks           int
	revealScheduled  bool
	pendingRevealKey string
}

// InitState subscribes to the model and makes the scroll controller and the focus node.
func (s *listState) InitState() {
	l := s.Widget()
	s.model = l.model
	s.find = newFindBoxState()
	s.handle = s.model.AddListener(s.modelChanged)
	s.scroll = widgets.NewScrollController()
	s.node = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "List"})
	if l.opts.MultiSelect {
		s.model.SetMultiSelect(true)
	}
	s.drag = dragController{host: s, dnd: func() *DragAndDrop { return s.Widget().opts.DragAndDrop }, label: func(key string) string {
		return s.model.Row(s.model.IndexOf(key)).Label
	}}
	if l.opts.Controller != nil {
		l.opts.Controller.state = s
	}
}

// DidUpdateWidget follows a new model and a new controller.
func (s *listState) DidUpdateWidget(old *List) {
	l := s.Widget()
	if l.model != s.model {
		s.model.RemoveListener(s.handle)
		s.model = l.model
		s.handle = s.model.AddListener(s.modelChanged)
	}
	if l.opts.MultiSelect {
		s.model.SetMultiSelect(true)
	}
	if old.opts.Controller != l.opts.Controller {
		if old.opts.Controller != nil {
			old.opts.Controller.state = nil
		}
		if l.opts.Controller != nil {
			l.opts.Controller.state = s
		}
	}
}

// Dispose lets go of the model, the controller, the scroll controller and the focus node.
func (s *listState) Dispose() {
	s.model.RemoveListener(s.handle)
	if c := s.Widget().opts.Controller; c != nil && c.state == s {
		c.state = nil
	}
	s.drag.dispose()
	s.find.dispose()
	s.scroll.Dispose()
	s.node.Dispose()
}

func (s *listState) modelChanged() {
	s.find.sync(s.model.find.pattern)
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// viewport reads where the view is from the scroll position.
func (s *listState) viewport() (top, height float64) {
	if !s.scroll.HasClients() {
		return 0, 0
	}
	p := s.scroll.Positions()[0]
	if !p.HasViewportDimension() || !p.HasPixels() {
		return 0, 0
	}
	return p.Pixels(), p.ViewportDimension()
}

func (s *listState) syncViewport() {
	top, height := s.viewport()
	s.model.SetViewport(top, height)
}

func (s *listState) jump(offset float64) {
	if s.scroll.HasClients() {
		jump(s.scroll, offset)
	}
}

// reveal scrolls to a row once the rows have been laid out.
func (s *listState) reveal(key string) bool {
	if s.model.IndexOf(key) < 0 {
		return false
	}
	s.pendingRevealKey = key
	if !s.revealScheduled {
		s.revealScheduled = true
		scheduler := widgets.WidgetsBindingOf(s.Context()).Scheduler()
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(time.Duration) {
			s.revealScheduled = false
			if !s.Mounted() {
				return
			}
			s.syncViewport()
			if _, to, ok := s.model.Reveal(s.pendingRevealKey); ok {
				if y, has := to.Get(); has {
					s.jump(y)
				}
			}
		})
	}
	return true
}

// dispatch applies what an action did: the scroll, then the callbacks.
func (s *listState) dispatch(res ListResult) {
	if to, ok := res.ScrollTo.Get(); ok {
		s.jump(to)
	}
	l := s.Widget()
	if res.SelectionChanged && l.opts.OnSelectionChange != nil {
		l.opts.OnSelectionChange(s.model.SelectedKeys(), s.model.SelectedIndexes())
	}
	row := s.model.Row(res.Index)
	if res.Select && l.opts.OnSelect != nil && res.Index >= 0 {
		l.opts.OnSelect(row)
	}
	if res.Open && l.opts.OnOpen != nil && res.Index >= 0 {
		l.opts.OnOpen(row)
	}
}

// handleKey is the keyboard of the list: VS Code's list commands (the `list.*` keybindings), and the find
// that a letter starts.
func (s *listState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	m := keyMods{ctrl: kb.IsControlPressed(), shift: kb.IsShiftPressed(), alt: kb.IsAltPressed(), meta: kb.IsMetaPressed()}
	if s.drag.active() {
		if e.LogicalKey() == services.LogicalKeyboardKeyEscape {
			s.drag.cancel()
			return widgets.KeyEventResultHandled
		}
		return widgets.KeyEventResultIgnored
	}
	s.syncViewport()
	if used, scroll := findKey(&s.model.rowNav, e, m, s.Widget().opts.TypeToFind.OrElse(true)); used {
		s.find.sync(s.model.find.pattern)
		if to, ok := scroll.Get(); ok {
			s.jump(to)
		}
		return widgets.KeyEventResultHandled
	}
	nk := commonKey(e.LogicalKey(), m, s.model.MultiSelect())
	action, ok := map[navKey]ListAction{
		navKeyDown: ListActionDown, navKeyUp: ListActionUp, navKeyFirst: ListActionFirst, navKeyLast: ListActionLast,
		navKeyPageDown: ListActionPageDown, navKeyPageUp: ListActionPageUp, navKeySelect: ListActionSelect,
		navKeyToggle: ListActionToggle, navKeyClear: ListActionClear, navKeyExpandDown: ListActionExpandSelectionDown,
		navKeyExpandUp: ListActionExpandSelectionUp, navKeySelectAll: ListActionSelectAll,
		navKeyToggleSelection: ListActionToggleSelection,
	}[nk]
	if !ok {
		return widgets.KeyEventResultIgnored
	}
	res := s.model.Perform(action)
	if !res.Handled {
		return widgets.KeyEventResultIgnored
	}
	s.dispatch(res)
	return widgets.KeyEventResultHandled
}

// focusChanged follows the keyboard focus of the list; a list that takes it with no focused row focuses one.
func (s *listState) focusChanged(has bool) {
	if s.hasFocus == has {
		return
	}
	s.hasFocus = has
	if has {
		s.model.EnsureFocus()
		s.syncViewport()
		if i := s.model.FocusedIndex(); i >= 0 {
			if to, ok := s.model.revealOffset(i).Get(); ok {
				s.jump(to)
			}
		}
	}
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// down is the press of a pointer button on a row: the row is focused and the list takes the keyboard focus;
// a press of the primary button counts toward a double click.
func (s *listState) down(key string, e *gestures.PointerDownEvent) {
	s.model.SetFocus(key)
	s.model.SetFindTyping(false)
	s.node.RequestFocus()
	s.drag.press(e, key)
	if e.Buttons()&gestures.PrimaryButton == 0 {
		return
	}
	at, stamp := e.Position(), e.TimeStamp()
	second := s.hasLast && s.lastKey == key && stamp-s.lastStamp <= treeDoubleClickInterval &&
		abs64(at.Dx-s.lastAt.Dx) <= treeDoubleClickSlop && abs64(at.Dy-s.lastAt.Dy) <= treeDoubleClickSlop
	s.clicks = 1
	if second {
		s.clicks = 2
	}
	s.lastKey, s.lastStamp, s.lastAt, s.hasLast = key, stamp, at, !second
}

// tap is the release of a press that did not move: a click on the row, or the second click of a double click.
func (s *listState) tap(key string) {
	if s.drag.consumeTap() {
		return
	}
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	mods := ClickModifiers{Ctrl: kb.IsControlPressed(), Shift: kb.IsShiftPressed()}
	if s.clicks == 2 && s.lastKey == key && !mods.Ctrl && !mods.Shift {
		s.dispatch(s.model.DoubleClick(key))
		return
	}
	s.dispatch(s.model.Click(key, mods))
}

func (s *listState) setHover(key string) {
	if s.hover != key && s.Mounted() {
		s.SetState(func() { s.hover = key })
	}
}

// closeFind is the close button of the find box.
func (s *listState) closeFind() {
	s.model.CloseFind()
	s.find.sync("")
	s.node.RequestFocus()
}

// The list as the host of a drag (dragHost).

func (s *listState) navigator() *rowNav { return &s.model.rowNav }

func (s *listState) bindingOf() *widgets.WidgetsBinding { return widgets.WidgetsBindingOf(s.Context()) }

func (s *listState) box() rendering.Box {
	if s.areaCtx == nil {
		return nil
	}
	b, _ := s.areaCtx.FindRenderObject().(rendering.Box)
	return b
}

func (s *listState) scrollOffset() float64 { top, _ := s.viewport(); return top }

func (s *listState) viewHeight() float64 { _, h := s.viewport(); return h }

func (s *listState) scrollTo(offset float64) { s.jump(offset) }

func (s *listState) expandable(string) (can, expanded bool) { return false, false }

func (s *listState) expand(string) {}

func (s *listState) refresh() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

func (s *listState) takeFocus() { s.node.RequestFocus() }

// defaultListRender shows the label of the row as one line of text.
func defaultListRender(bc widgets.BuildContext, row ListViewRow, st RowState) widgets.Widget {
	label := row.Label
	if label == "" {
		var ok bool
		if label, ok = row.Data.(string); !ok {
			label = fmt.Sprint(row.Data)
		}
	}
	return NewHighlightedLabel(bc, label, st)
}

// Build implements widgets.State.
func (s *listState) Build(bc widgets.BuildContext) widgets.Widget {
	l := s.Widget()
	m := s.model
	count := m.Len()
	builder := func(c widgets.BuildContext, i int) widgets.Widget {
		if i < 0 || i >= count {
			return nil
		}
		return s.buildRow(c, i)
	}
	itemOpts := widgets.SliverBuilderOpts{ItemCount: foundation.Some(count), AddAutomaticKeepAlives: foundation.Some(false)}
	var sliver widgets.Widget
	if m.opts.HeightOf == nil {
		sliver = widgets.NewSliverFixedExtentListBuilder(builder, m.RowHeight(), itemOpts)
	} else {
		sliver = widgets.NewSliverVariedExtentListBuilder(builder, func(i int, _ rendering.SliverLayoutDimensions) foundation.Opt[float64] {
			return foundation.Some(m.RowHeightAt(i))
		}, itemOpts)
	}
	view := widgets.NewCustomScrollView([]widgets.Widget{sliver}, widgets.ScrollViewOpts{Controller: s.scroll})
	element := NewScrollableElement(view, ScrollableElementOpts{Controller: s.scroll})
	colors := ThemeOf(bc)
	header := buildFindBox(bc, &m.rowNav, s.find, s.refresh, s.closeFind)
	area := composeArea(bc, colors, &s.drag, header, element, func(c widgets.BuildContext) { s.areaCtx = c })
	// Translucent, see treeState.Build.
	content := widgets.NewListener(area, widgets.ListenerOpts{
		OnPointerMove: s.drag.move, OnPointerUp: s.drag.up, OnPointerCancel: s.drag.pointerCancel,
		Behavior: rendering.HitTestBehaviorTranslucent,
	})
	return widgets.NewMouseRegion(
		widgets.NewFocus(content, widgets.FocusOpts{
			FocusNode: s.node, Autofocus: l.opts.Autofocus, OnKeyEvent: s.handleKey, OnFocusChange: s.focusChanged,
		}),
		widgets.MouseRegionOpts{})
}

// buildRow builds the row at an index.
func (s *listState) buildRow(bc widgets.BuildContext, i int) widgets.Widget {
	l := s.Widget()
	m := s.model
	row := m.Row(i)
	height := m.RowHeightAt(i)
	colors := ThemeOf(bc)
	st := rowStateOf(colors, &m.rowNav, row.Key, s.hover, s.hasFocus)
	background, foreground, outline := rowLook(colors, st)
	st.Foreground = foreground
	inside, before, after := s.drag.rowDrop(row.Key)

	var contents widgets.Widget
	if l.opts.Render != nil {
		contents = l.opts.Render(bc, row, st)
	} else {
		contents = defaultListRender(bc, row, st)
	}
	body := widgets.NewRow([]widgets.Widget{
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(listRowPadding)}),
		widgets.NewExpanded(widgets.NewAlign(contents, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	layers := []widgets.Widget{widgets.NewSizedBox(body, widgets.SizedBoxOpts{Height: foundation.Some(height)})}
	if before {
		layers = append(layers, dropLine(colors.ListDropBetweenBackground, true))
	}
	if after {
		layers = append(layers, dropLine(colors.ListDropBetweenBackground, false))
	}
	deco := widgets.NewDecoratedBox(rowDecoration(colors, background, outline, inside), widgets.NewStack(layers))
	key := row.Key
	return widgets.NewSizedBox(widgets.NewMouseRegion(
		widgets.NewListener(
			widgets.NewGestureDetector(deco, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { s.tap(key) }}),
			widgets.ListenerOpts{OnPointerDown: func(e *gestures.PointerDownEvent) { s.down(key, e) }}),
		widgets.MouseRegionOpts{
			Cursor:  services.SystemMouseCursors.Click,
			OnEnter: func(*gestures.PointerEnterEvent) { s.setHover(key) },
			OnExit:  func(*gestures.PointerExitEvent) { s.setHover("") },
		}), widgets.SizedBoxOpts{Height: foundation.Some(height)})
}

// listRowPadding is the room before the contents of a row of a list.
const listRowPadding = 8.0
