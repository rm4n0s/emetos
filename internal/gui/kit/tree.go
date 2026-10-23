package kit

// Ported from VS Code: src/vs/base/browser/ui/tree/abstractTree.ts (TreeRenderer, TreeNodeListMouseController,
// the indent guides), src/vs/base/browser/ui/tree/media/tree.css, src/vs/base/browser/ui/list/listWidget.ts
// and list.css (the look of the rows, the mouse and the keys of a list) and src/vs/platform/theme/common/colors/listColors.ts.
// Step 6.4b added the multi-selection, the find (type to highlight or filter) and the drag and drop of the rows
// (listselection.go, listnav.go, listfind.go, listwidget.go, listdnd.go).
// Not ported: sticky scroll, the horizontal scrolling of wide rows, compressed folders, the spinning of the
// loading twistie, accessibility.

import (
	"fmt"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of the rows of a tree, from VS Code's `TreeRenderer` and `tree.css`.
const (
	// treeBaseIndent is the indent of a root node (`TreeRenderer.DefaultIndent`) and treeIndentStep the
	// indent added by each level (`tree-indent`).
	treeBaseIndent = 8.0
	treeIndentStep = 8.0
	// treeTwistieSize is the width of the twistie, treeTwistieGap the room after it (its padding)
	// and treeTwistieShift how far the chevron is moved to the right (`transform: translateX(3px)`).
	treeTwistieSize  = 16.0
	treeTwistieGap   = 6.0
	treeTwistieShift = 3.0
	// treeGuideStart is where the first indent guide is (`.monaco-tl-indent { left: 16px }`).
	treeGuideStart = 16.0
	// treeDoubleClickInterval and treeDoubleClickSlop are what makes two presses a double click.
	treeDoubleClickInterval = 400 * time.Millisecond
	treeDoubleClickSlop     = 5.0
)

// TreeIndentGuides says when the indent guides are shown (`workbench.tree.renderIndentGuides`).
type TreeIndentGuides int

const (
	// TreeGuidesOnHover shows the guides while the pointer is over the tree (VS Code's default). The
	// active guide, the one that runs through the children of the node that is focused or selected, is
	// always shown.
	TreeGuidesOnHover TreeIndentGuides = iota
	// TreeGuidesAlways shows them all the time.
	TreeGuidesAlways
	// TreeGuidesNone shows none.
	TreeGuidesNone
)

// TreeRenderer builds the contents of a row: the part after the twistie. The row is 22 px high (the
// model's RowHeight) and as wide as the tree; the renderer is given the node and the state of the row, and
// draws what it wants (an icon and a label) in the foreground the state names. It is called while the
// rows are laid out, so it must be quick and must not change the model.
type TreeRenderer func(bc widgets.BuildContext, node TreeNode, state TreeRowState) widgets.Widget

// TreeOpts are the options of NewTree.
type TreeOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Controller gives access to the tree from outside (reveal, scroll, focus).
	Controller *TreeController
	// Render builds the contents of a row. The default shows fmt.Sprint of the node's Data in one line
	// of 13 px text.
	Render TreeRenderer
	// ToggleOnClick makes a click on a row, and Enter, toggle an expandable node besides selecting it;
	// the default is true. Off, only a click on the twistie, Space and a double click toggle (VS Code's
	// `expandOnlyOnTwistieClick`, `workbench.tree.expandMode`: `doubleClick`).
	ToggleOnClick foundation.Opt[bool]
	// IndentGuides says when the indent guides are shown; the default is TreeGuidesOnHover.
	IndentGuides TreeIndentGuides
	// LoadingLabel is the text of the placeholder row below a node that is being loaded; the default is
	// "Loading...".
	LoadingLabel string
	// Autofocus gives the tree the keyboard focus when it is first shown.
	Autofocus bool
	// MultiSelect lets Ctrl+click toggle a node in the selection, Shift+click select the range from the anchor
	// (both together add the range), Shift+Up and Shift+Down extend the selection, Ctrl+A select every node
	// that is shown and Ctrl+Space (or Ctrl+Shift+Enter) toggle the focused node; Escape reduces several
	// selected nodes to the focused one.
	MultiSelect bool
	// DragAndDrop makes the rows draggable and the tree a drop target; see DragAndDrop.
	DragAndDrop *DragAndDrop
	// TypeToFind makes a letter typed into the tree start the find box (the default is true). Its pattern
	// matches the labels of the nodes (TreeItem.Label); the mode (highlight or filter) and the match type are
	// the model's (TreeModelOpts) and can be changed with the toggles of the box.
	TypeToFind foundation.Opt[bool]
	// OnSelect is called when the user selects a node: a click on its row, Enter, or Space on a node
	// that cannot be expanded. The node is selected in the model already.
	OnSelect func(node TreeNode)
	// OnSelectionChange is called when the user changed the selected nodes (a click, Ctrl+click, Shift+click,
	// a key), with the keys of the selected nodes in the order of the rows. It is not called for changes the
	// program makes.
	OnSelectionChange func(keys []string)
	// OnOpen is called when the user opens a node: Enter, or a double click.
	OnOpen func(node TreeNode)
}

// treeContent is what a node of Glutter's TreeSliver holds: the key of the node of the model. Glutter
// looks contents up with reflect.DeepEqual, so it is a small comparable value.
type treeContent struct{ Key string }

// Tree is a tree of rows with a keyboard: VS Code's list with the rules of its tree. It shows a TreeModel
// in a Glutter TreeSliver inside a CustomScrollView with the overlay scrollbars of ScrollableElement.
// Rows are 22 px high and indented by 8 px and 8 px per level; each has a twistie (a chevron, nothing for
// a leaf, the loading icon for a node whose children are being read), the contents the Render function
// builds, and the indent guides of its ancestors. The selected row, the focused row and the row under the
// pointer are colored as VS Code colors them, differently while the tree has the keyboard focus. With MultiSelect
// several nodes can be selected (Ctrl, Shift); typing starts a find box that highlights or filters the nodes
// (TreeItem.Label); with DragAndDrop nodes can be dragged and dropped on, before and after other nodes.
//
// Glutter's TreeSliver has limits that shape this widget. It draws no twistie of its own that we use, and
// toggles only nodes that have children, so a node that is not read has a placeholder child; its nodes
// can not be removed, so the model owns the expansion state (by key) and every change of the model makes
// the nodes again, showing only the expanded part; the toggle animation is off
// (animation.AnimationStyleNoAnimation) because rows come and go with the model; and its contents are
// compared with reflect.DeepEqual, so a node holds only its key. The focus, the selection and the keys
// are Emetos's own: they are in the model.
//
// The model is used on the UI thread only; a reading in the background hands its answer over with the
// binding's Post.
type Tree struct {
	widgets.StatefulWidget
	model *TreeModel
	opts  TreeOpts
}

// NewTree makes a tree that shows the model.
func NewTree(model *TreeModel, opts ...TreeOpts) *Tree {
	t := &Tree{model: model}
	if len(opts) > 0 {
		t.opts = opts[0]
	}
	t.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: t.opts.Key})
	return t
}

// CreateState implements widgets.StatefulWidget.
func (t *Tree) CreateState() widgets.State { return &treeState{} }

// TreeController reads and moves the tree from outside. Make one with NewTreeController and give it to
// the tree; it belongs to one tree. Expanding and collapsing are done on the TreeModel, which also tells
// what is focused and selected.
type TreeController struct {
	state *treeState
}

// NewTreeController makes a controller that is not attached to a tree yet; until it is, its methods do
// nothing.
func NewTreeController() *TreeController { return &TreeController{} }

// Reveal expands the nodes above a node and scrolls the tree, after the next frame, so that its row is in
// view (with the least scrolling). It reports whether the model has the node (see TreeModel.Reveal).
func (c *TreeController) Reveal(key string) bool {
	if c.state == nil {
		return false
	}
	return c.state.reveal(key)
}

// ScrollTo scrolls the tree to a position, in pixels from the top of its rows.
func (c *TreeController) ScrollTo(offset float64) {
	if c.state != nil {
		c.state.jump(offset)
	}
}

// ScrollOffset is how far the tree is scrolled, in pixels.
func (c *TreeController) ScrollOffset() float64 {
	if c.state == nil || !c.state.scroll.HasClients() {
		return 0
	}
	return c.state.scroll.Offset()
}

// ViewportHeight is the height of the tree's view in pixels, 0 before the first layout.
func (c *TreeController) ViewportHeight() float64 {
	if c.state == nil {
		return 0
	}
	_, h := c.state.viewport()
	return h
}

// Focus gives the tree the keyboard focus. A tree with no focused row focuses the selected row or the first.
func (c *TreeController) Focus() {
	if c.state != nil {
		c.state.node.RequestFocus()
	}
}

// HasFocus is whether the tree has the keyboard focus.
func (c *TreeController) HasFocus() bool { return c.state != nil && c.state.hasFocus }

type treeState struct {
	widgets.StateOf[*Tree]
	model  *TreeModel
	handle foundation.ListenerHandle
	scroll *widgets.ScrollControllerBase
	node   *widgets.FocusNode

	hasFocus bool
	drag     dragController
	find     *findBoxState
	areaCtx  widgets.BuildContext
	// hover is the key of the row under the pointer, and overTree whether the pointer is over the tree.
	hover    string
	overTree bool
	// activeGuides are the nodes whose guide is the active one, as of the last build.
	activeGuides map[string]bool

	// nodes are the nodes of Glutter's TreeSliver; they are made again when the model changed.
	nodes      []*widgets.TreeSliverNode[treeContent]
	nodesStale bool

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
func (s *treeState) InitState() {
	t := s.Widget()
	s.model = t.model
	s.find = newFindBoxState()
	s.handle = s.model.AddListener(s.modelChanged)
	s.nodesStale = true
	s.scroll = widgets.NewScrollController()
	s.node = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Tree"})
	s.model.SetToggleOnSelect(t.opts.ToggleOnClick.OrElse(true))
	if t.opts.MultiSelect {
		s.model.SetMultiSelect(true)
	}
	s.drag = dragController{host: s, dnd: func() *DragAndDrop { return s.Widget().opts.DragAndDrop }, label: func(key string) string {
		n, _ := s.model.Node(key)
		return n.Label
	}}
	if t.opts.Controller != nil {
		t.opts.Controller.state = s
	}
}

// DidUpdateWidget follows a new model and a new controller.
func (s *treeState) DidUpdateWidget(old *Tree) {
	t := s.Widget()
	if t.model != s.model {
		s.model.RemoveListener(s.handle)
		s.model = t.model
		s.handle = s.model.AddListener(s.modelChanged)
		s.nodesStale = true
	}
	s.model.SetToggleOnSelect(t.opts.ToggleOnClick.OrElse(true))
	if t.opts.MultiSelect {
		s.model.SetMultiSelect(true)
	}
	if old.opts.Controller != t.opts.Controller {
		if old.opts.Controller != nil {
			old.opts.Controller.state = nil
		}
		if t.opts.Controller != nil {
			t.opts.Controller.state = s
		}
	}
}

// Dispose lets go of the model, the controller, the scroll controller and the focus node.
func (s *treeState) Dispose() {
	s.model.RemoveListener(s.handle)
	if c := s.Widget().opts.Controller; c != nil && c.state == s {
		c.state = nil
	}
	s.drag.dispose()
	s.find.dispose()
	s.scroll.Dispose()
	s.node.Dispose()
}

func (s *treeState) modelChanged() {
	s.nodesStale = true
	s.find.sync(s.model.find.pattern)
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// viewport reads where the view is from the scroll position.
func (s *treeState) viewport() (top, height float64) {
	if !s.scroll.HasClients() {
		return 0, 0
	}
	p := s.scroll.Positions()[0]
	if !p.HasViewportDimension() || !p.HasPixels() {
		return 0, 0
	}
	return p.Pixels(), p.ViewportDimension()
}

func (s *treeState) syncViewport() {
	top, height := s.viewport()
	s.model.SetViewport(top, height)
}

// jump scrolls to a position.
func (s *treeState) jump(offset float64) {
	if !s.scroll.HasClients() {
		return
	}
	jump(s.scroll, offset)
}

// reveal expands above a node and scrolls to it once the rows have been laid out.
func (s *treeState) reveal(key string) bool {
	if _, ok := s.model.Reveal(key); !ok {
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
			if i := s.model.IndexOf(s.pendingRevealKey); i >= 0 {
				if to, ok := s.model.revealOffset(i).Get(); ok {
					s.jump(to)
				}
			}
		})
	}
	return true
}

// dispatch applies what an action did: the scroll, then the callbacks.
func (s *treeState) dispatch(res TreeResult) {
	if to, ok := res.ScrollTo.Get(); ok {
		s.jump(to)
	}
	t := s.Widget()
	if res.SelectionChanged && t.opts.OnSelectionChange != nil {
		t.opts.OnSelectionChange(s.model.SelectedKeys())
	}
	if res.Select && t.opts.OnSelect != nil {
		t.opts.OnSelect(res.Node)
	}
	if res.Open && t.opts.OnOpen != nil {
		t.opts.OnOpen(res.Node)
	}
}

// handleKey is the keyboard of the tree: VS Code's list and tree commands (the `list.*` keybindings), and the
// find that a letter starts.
func (s *treeState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	m := keyMods{ctrl: kb.IsControlPressed(), shift: kb.IsShiftPressed(), alt: kb.IsAltPressed(), meta: kb.IsMetaPressed()}
	key := e.LogicalKey()
	if s.drag.active() {
		if key == services.LogicalKeyboardKeyEscape {
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
	var action TreeAction
	switch nk := commonKey(key, m, s.model.MultiSelect()); {
	case nk != navKeyNone:
		action = map[navKey]TreeAction{
			navKeyDown: TreeActionDown, navKeyUp: TreeActionUp, navKeyFirst: TreeActionFirst, navKeyLast: TreeActionLast,
			navKeyPageDown: TreeActionPageDown, navKeyPageUp: TreeActionPageUp, navKeySelect: TreeActionSelect,
			navKeyToggle: TreeActionToggle, navKeyClear: TreeActionClear, navKeyExpandDown: TreeActionExpandSelectionDown,
			navKeyExpandUp: TreeActionExpandSelectionUp, navKeySelectAll: TreeActionSelectAll,
			navKeyToggleSelection: TreeActionToggleSelection,
		}[nk]
	case m.alt || m.meta || m.shift:
		return widgets.KeyEventResultIgnored
	case m.ctrl && key == services.LogicalKeyboardKeyArrowLeft:
		action = TreeActionCollapseAll
	case m.ctrl:
		return widgets.KeyEventResultIgnored
	case key == services.LogicalKeyboardKeyArrowRight:
		action = TreeActionExpand
	case key == services.LogicalKeyboardKeyArrowLeft:
		action = TreeActionCollapse
	default:
		return widgets.KeyEventResultIgnored
	}
	res := s.model.Perform(action)
	if !res.Handled {
		return widgets.KeyEventResultIgnored
	}
	s.dispatch(res)
	return widgets.KeyEventResultHandled
}

// focusChanged follows the keyboard focus of the tree; a tree that takes it with no focused row focuses
// one.
func (s *treeState) focusChanged(has bool) {
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

// down is the press of a pointer button on a row: the row is focused and the tree takes the keyboard focus;
// a press of the primary button counts toward a double click.
func (s *treeState) down(key string, e *gestures.PointerDownEvent) {
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

// tap is the release of a press that did not move: a click on the twistie or on the row, or the second
// click of a double click.
func (s *treeState) tap(key string, onTwistie bool) {
	if s.drag.consumeTap() {
		return
	}
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	mods := ClickModifiers{Ctrl: kb.IsControlPressed(), Shift: kb.IsShiftPressed()}
	if !onTwistie && s.clicks == 2 && s.lastKey == key && !mods.Ctrl && !mods.Shift {
		s.dispatch(s.model.DoubleClick(key))
		return
	}
	s.dispatch(s.model.ClickWith(key, mods, onTwistie, kb.IsAltPressed()))
}

// closeFind is the close button of the find box.
func (s *treeState) closeFind() {
	s.model.CloseFind()
	s.find.sync("")
	s.node.RequestFocus()
}

// The tree as the host of a drag (dragHost).

func (s *treeState) navigator() *rowNav { return &s.model.rowNav }

func (s *treeState) bindingOf() *widgets.WidgetsBinding { return widgets.WidgetsBindingOf(s.Context()) }

func (s *treeState) box() rendering.Box {
	if s.areaCtx == nil {
		return nil
	}
	b, _ := s.areaCtx.FindRenderObject().(rendering.Box)
	return b
}

func (s *treeState) scrollOffset() float64 { top, _ := s.viewport(); return top }

func (s *treeState) viewHeight() float64 { _, h := s.viewport(); return h }

func (s *treeState) scrollTo(offset float64) { s.jump(offset) }

func (s *treeState) expandable(key string) (can, expanded bool) {
	n, _ := s.model.Node(key)
	return n.Expandable, n.Expanded
}

func (s *treeState) expand(key string) { s.model.Expand(key) }

func (s *treeState) refresh() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

func (s *treeState) takeFocus() { s.node.RequestFocus() }

func (s *treeState) setHover(key string) {
	if s.hover != key && s.Mounted() {
		s.SetState(func() { s.hover = key })
	}
}

func (s *treeState) setOverTree(over bool) {
	if s.overTree != over && s.Mounted() {
		s.SetState(func() { s.overTree = over })
	}
}

// glutterNodes makes the nodes of the TreeSliver from the entries of the model: only the expanded part
// has its children, and a node that is not read has the placeholder as its only child.
//
// Glutter: TreeSliver cannot remove nodes, toggles only nodes that have children and keeps the expansion in
// its nodes, so the model owns the state and the nodes are made again at every change; Glutter should let a
// node be expandable without children and let the app replace children (docs/GLUTTER-ISSUES.md, 6.4).
func (s *treeState) glutterNodes(entries []*treeEntry) []*widgets.TreeSliverNode[treeContent] {
	m := s.model
	filtering := m.find.filtering()
	out := make([]*widgets.TreeSliverNode[treeContent], 0, len(entries))
	for _, e := range entries {
		if filtering && !m.hit[e.key] {
			continue
		}
		expanded := m.shownExpanded(e, filtering)
		var children []*widgets.TreeSliverNode[treeContent]
		switch {
		case e.lazy:
			children = []*widgets.TreeSliverNode[treeContent]{widgets.NewTreeSliverNode(treeContent{Key: PlaceholderKey(e.key)})}
		case expanded:
			children = s.glutterNodes(e.children)
		}
		out = append(out, widgets.NewTreeSliverNode(treeContent{Key: e.key}, widgets.TreeSliverNodeOpts[treeContent]{Children: children, Expanded: expanded}))
	}
	return out
}

// Build implements widgets.State.
func (s *treeState) Build(bc widgets.BuildContext) widgets.Widget {
	t := s.Widget()
	if s.nodesStale {
		s.nodes = s.glutterNodes(s.model.roots)
		s.nodesStale = false
	}
	s.activeGuides = s.model.ActiveGuides()
	height := s.model.RowHeight()
	sliver := widgets.NewTreeSliver(s.nodes, widgets.TreeSliverOpts[treeContent]{
		TreeNodeBuilder: s.buildRow,
		TreeRowExtentBuilder: func(*widgets.TreeSliverNode[treeContent], rendering.SliverLayoutDimensions) float64 {
			return height
		},
		ToggleAnimationStyle:   foundation.Some(animation.AnimationStyleNoAnimation),
		Indentation:            foundation.Some(rendering.TreeSliverIndentationTypeNone),
		AddAutomaticKeepAlives: foundation.Some(false),
	})
	view := widgets.NewCustomScrollView([]widgets.Widget{sliver}, widgets.ScrollViewOpts{Controller: s.scroll})
	element := NewScrollableElement(view, ScrollableElementOpts{Controller: s.scroll})
	colors := ThemeOf(bc)
	header := buildFindBox(bc, &s.model.rowNav, s.find, s.refresh, s.closeFind)
	area := composeArea(bc, colors, &s.drag, header, element, func(c widgets.BuildContext) { s.areaCtx = c })
	// Translucent: ScrollableElement's MouseRegion is not opaque, so the hit test of a press on a row reports "not
	// hit" to what is above it and a defer-to-child Listener would be left out of the path, and would not see the
	// moves of the drag. The path of the press is kept for the whole drag, even when the row scrolls away.
	content := widgets.NewListener(area, widgets.ListenerOpts{
		OnPointerMove: s.drag.move, OnPointerUp: s.drag.up, OnPointerCancel: s.drag.pointerCancel,
		Behavior: rendering.HitTestBehaviorTranslucent,
	})
	return widgets.NewMouseRegion(
		widgets.NewFocus(content, widgets.FocusOpts{
			FocusNode: s.node, Autofocus: t.opts.Autofocus, OnKeyEvent: s.handleKey, OnFocusChange: s.focusChanged,
		}),
		widgets.MouseRegionOpts{
			OnEnter: func(*gestures.PointerEnterEvent) { s.setOverTree(true) },
			OnExit:  func(*gestures.PointerExitEvent) { s.setOverTree(false) },
		})
}

// orColor is c, or the fallback when c is transparent (a color a theme leaves unset).
func orColor(c, fallback ui.Color) ui.Color {
	if c.Alpha() == 0 {
		return fallback
	}
	return c
}

// rowLook is the background, the text color and the outline of a row, from VS Code's list styles: in a tree
// that has the keyboard focus the selected row is in the active selection colors, else the focused row
// in the focus colors, else the row under the pointer in the hover color; in a tree that does not, the
// selected row is in the inactive selection colors and the focused row in the inactive focus ones.
func rowLook(colors Colors, st TreeRowState) (background, foreground, outline ui.Color) {
	foreground = colors.Foreground
	switch {
	case st.Selected && st.Active:
		background, foreground = colors.ListActiveSelectionBackground, orColor(colors.ListActiveSelectionForeground, foreground)
	case st.Selected:
		background, foreground = colors.ListInactiveSelectionBackground, orColor(colors.ListInactiveSelectionForeground, foreground)
	case st.Focused && st.Active:
		background, foreground = colors.ListFocusBackground, orColor(colors.ListFocusForeground, foreground)
	case st.Focused:
		background = colors.ListInactiveFocusBackground
	case st.Hovered:
		background = colors.ListHoverBackground
	}
	switch {
	case st.Focused && st.Active && st.Selected:
		outline = orColor(colors.ListFocusAndSelectionOutline, colors.ListFocusOutline)
	case st.Focused && st.Active:
		outline = colors.ListFocusOutline
	case st.Focused:
		outline = colors.ListInactiveFocusOutline
	}
	return background, foreground, outline
}

// defaultTreeRender shows the data of the node as one line of text.
func defaultTreeRender(bc widgets.BuildContext, n TreeNode, st TreeRowState) widgets.Widget {
	label := n.Label
	if label == "" {
		var ok bool
		if label, ok = n.Data.(string); !ok {
			label = fmt.Sprint(n.Data)
		}
	}
	return NewHighlightedLabel(bc, label, st)
}

// buildRow builds the row of a node of the TreeSliver.
func (s *treeState) buildRow(bc widgets.BuildContext, node *widgets.TreeSliverNode[treeContent], _ animation.AnimationStyle) widgets.Widget {
	t := s.Widget()
	m := s.model
	height := m.RowHeight()
	n, ok := m.Node(node.Content().Key)
	if !ok {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(height)})
	}
	colors := ThemeOf(bc)
	st := rowStateOf(colors, &m.rowNav, n.Key, s.hover, s.hasFocus)
	st.Hovered = st.Hovered && !n.Placeholder
	background, foreground, outline := rowLook(colors, st)
	inside, before, after := s.drag.rowDrop(n.Key)
	st.Foreground = foreground

	var contents widgets.Widget
	switch {
	case n.Placeholder:
		label := t.opts.LoadingLabel
		if label == "" {
			label = "Loading..."
		}
		contents = widgets.NewText(label, widgets.TextOpts{Style: TextStyleOf(bc, listFontSize, colors.DescriptionForeground), MaxLines: foundation.Some(1)})
	case t.opts.Render != nil:
		contents = t.opts.Render(bc, n, st)
	default:
		contents = defaultTreeRender(bc, n, st)
	}

	indent := treeBaseIndent + float64(n.Depth)*treeIndentStep
	body := widgets.NewRow([]widgets.Widget{
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(indent)}),
		s.twistie(n, foreground, height),
		widgets.NewExpanded(widgets.NewAlign(contents, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	layers := append(s.guides(m, n, colors, height), widgets.NewSizedBox(body, widgets.SizedBoxOpts{Height: foundation.Some(height)}))
	if before {
		layers = append(layers, dropLine(colors.ListDropBetweenBackground, true))
	}
	if after {
		layers = append(layers, dropLine(colors.ListDropBetweenBackground, false))
	}
	row := widgets.NewDecoratedBox(rowDecoration(colors, background, outline, inside), widgets.NewStack(layers))
	if n.Placeholder {
		return row
	}
	key := n.Key
	return widgets.NewMouseRegion(
		widgets.NewListener(
			widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { s.tap(key, false) }}),
			widgets.ListenerOpts{OnPointerDown: func(e *gestures.PointerDownEvent) { s.down(key, e) }}),
		widgets.MouseRegionOpts{
			Cursor:  services.SystemMouseCursors.Click,
			OnEnter: func(*gestures.PointerEnterEvent) { s.setHover(key) },
			OnExit:  func(*gestures.PointerExitEvent) { s.setHover("") },
		})
}

// twistie is the chevron of a row, in a box of the twistie's width and the gap after it.
func (s *treeState) twistie(n TreeNode, fg ui.Color, height float64) widgets.Widget {
	var icon widgets.Widget
	switch {
	case n.Loading:
		icon = widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconLoading), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(fg)})
	case n.Expandable && n.Expanded:
		icon = widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconChevronDown), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(fg)})
	case n.Expandable:
		icon = widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconChevronRight), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(fg)})
	}
	box := widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(treeTwistieSize + treeTwistieGap), Height: foundation.Some(height)})
	if icon == nil {
		return box
	}
	box = widgets.NewSizedBox(
		widgets.NewAlign(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(treeTwistieShift, 0, 0, 0), icon), widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft}),
		widgets.SizedBoxOpts{Width: foundation.Some(treeTwistieSize + treeTwistieGap), Height: foundation.Some(height)})
	key := n.Key
	return widgets.NewGestureDetector(box, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { s.tap(key, true) }})
}

// guides are the indent guides of a row: a line, as high as the row, in each of the columns of the nodes above
// it. The line of a node whose guide is the active one is in `tree.indentGuidesStroke`; the others are shown
// as the options say, in `tree.inactiveIndentGuidesStroke`.
func (s *treeState) guides(m *TreeModel, n TreeNode, colors Colors, height float64) []widgets.Widget {
	mode := s.Widget().opts.IndentGuides
	if mode == TreeGuidesNone || n.Depth == 0 {
		return nil
	}
	showInactive := mode == TreeGuidesAlways || s.overTree
	var out []widgets.Widget
	above := m.Ancestors(n.Key)
	if n.Placeholder {
		above = append(m.Ancestors(n.Parent), n.Parent)
	}
	for j, key := range above {
		c := colors.TreeInactiveIndentGuidesStroke
		switch {
		case s.activeGuides[key]:
			c = colors.TreeIndentGuidesStroke
		case !showInactive:
			continue
		}
		out = append(out, widgets.NewPositioned(widgets.NewColoredBox(c, widgets.NewSizedBox(nil)), widgets.PositionedOpts{
			Left: foundation.Some(treeGuideStart + float64(j)*treeIndentStep), Top: foundation.Some(0.0), Width: foundation.Some(1.0), Height: foundation.Some(height),
		}))
	}
	return out
}
