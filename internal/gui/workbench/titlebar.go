package workbench

import (
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The title bar row (SPEC §10.14): Emetos draws the title bar of its window (the window asks for DecorationsApp), with
// widgets.WindowDecoration for the borders, the resize edges, the buttons of the window and the drag, the double click
// and the window menu of the empty part of the row. The row holds, from left to right, the menu (the "≡" button of
// window.menuBarVisibility compact, or the titles of the others), the workspace tabs with the "+" button, the empty
// part that moves the window, and the buttons of the window. In full screen mode the window has no title bar (Glutter
// draws none), and the menu then sits in a row of its own over the workbench when it is shown.

// TitleBarHeight is the height of the title bar row.
const TitleBarHeight = kit.TabHeight

// plusButtonSize is the side of the "+" button after the last tab, plusButtonGap the space before it.
const (
	plusButtonSize = 28.0
	plusButtonGap  = 4.0
	// menuGap is the space between the menu and the first tab.
	menuGap = 8.0
)

// WorkspaceTab is the look of one workspace tab of the title bar.
type WorkspaceTab struct {
	// ID identifies the tab between builds (the stable id of the window tab).
	ID string
	// Name and Description are the label (LabelWorkspaceTabs gives them).
	Name, Description string
	// Tooltip is the text of the hover: the path of the folder, and the profile on a second line when it is not the
	// default.
	Tooltip string
	// Dirty shows the dot of unsaved changes in place of the close button.
	Dirty bool
	// Folder says that the tab has a folder (the icon root-folder); a tab without one has the icon window.
	Folder bool
	// IconCode, when it is not 0, is the code point of the Codicon of the tab (the tab of an extension has the icon of its
	// kind); it wins over Folder.
	IconCode int
}

// TitleBarOptions are the workspace tabs of the title bar row and what the user does to them. A Workbench without them
// shows the menu and the buttons of the window only.
type TitleBarOptions struct {
	// Tabs are the workspace tabs in order, and Active the index of the one that is shown.
	Tabs   []WorkspaceTab
	Active int
	// OnActivate is called with the index of a tab that is clicked: for the active tab at the press, for the others when
	// the button is released (a press that becomes a drag does not activate the tab, so the strip that is shown stays the
	// strip the drag goes on in); OnClose with the index of a tab whose close button is pressed or that is pressed with
	// the middle button.
	OnActivate func(index int)
	OnClose    func(index int)
	// OnMove is called when a tab is dropped at another place of the strip, with the index of the tab and the place it
	// goes to (the index it has afterwards). A tab is dragged by the primary button once the pointer has moved 4 px, a
	// line shows where it would go, Escape cancels, and the strip scrolls when the pointer rests at its ends. Without it
	// the tabs cannot be dragged.
	OnMove func(index, to int)
	// ContextItems gives the items of the context menu of the tab at an index when it is pressed with the secondary
	// button, and NewItems those of the menu of the "+" button. OnRunItem is called with the item the user runs in
	// either menu (the workbench's OnRunMenu when nil).
	ContextItems func(index int) []kit.MenuItem
	NewItems     func() []kit.MenuItem
	OnRunItem    func(item kit.MenuItem)
	// NewMenu, when set, opens the menu of the "+" button from outside (the dev command of step 11.7, a test).
	NewMenu *NewMenuController
}

// NewMenuController opens the menu of the "+" button of the strip that is on screen, as a click on the button does.
type NewMenuController struct {
	open func()
}

// NewNewMenuController makes a controller that is not attached to a strip yet.
func NewNewMenuController() *NewMenuController { return &NewMenuController{} }

// Open opens the menu; it reports false when no strip is on screen.
func (c *NewMenuController) Open() bool {
	if c == nil || c.open == nil {
		return false
	}
	c.open()
	return true
}

// titleBarConfig is everything the title bar needs of the workbench for a build.
type titleBarConfig struct {
	bar *TitleBarOptions
	// setting is the value of window.menuBarVisibility.
	setting string
	// zen and fullScreen are the state of the workbench; shown says that the menu was revealed with the Alt key
	// (the visibility toggle in the row of its own).
	zen, fullScreen, shown bool
	// menu makes the menu bar widget for a visibility.
	menu func(v kit.MenuBarVisibility) widgets.Widget
}

// titleBarHost wraps the body of the workbench in the title bar. It follows the state of the window (focused,
// maximized, full screen) that the platform reports with the size.
type titleBarHost struct {
	widgets.StatefulWidget
	cfg   titleBarConfig
	child widgets.Widget
}

func newTitleBarHost(cfg titleBarConfig, child widgets.Widget) *titleBarHost {
	return &titleBarHost{StatefulWidget: widgets.NewStatefulWidgetBase(), cfg: cfg, child: child}
}

// CreateState implements widgets.StatefulWidget.
func (h *titleBarHost) CreateState() widgets.State { return &titleBarState{} }

type titleBarState struct {
	widgets.StateOf[*titleBarHost]
	widgets.WidgetsBindingObserverBase
	binding *widgets.WidgetsBinding
}

// InitState follows the window.
func (s *titleBarState) InitState() {
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.binding.AddObserver(s)
}

// Dispose stops following the window.
func (s *titleBarState) Dispose() { s.binding.RemoveObserver(s) }

// DidChangeMetrics rebuilds when the window got or lost the focus, or changed its state.
func (s *titleBarState) DidChangeMetrics() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// Build implements widgets.State.
func (s *titleBarState) Build(bc widgets.BuildContext) widgets.Widget {
	c := s.Widget().cfg
	colors := kit.ThemeOf(bc)
	var info ui.WindowInfo
	if view := widgets.MaybeViewOf(bc); view != nil {
		info = view.Window()
	}
	appBar := info.Decoration == ui.DecorationApp
	fullScreen := c.fullScreen || info.Fullscreen
	inBar := menuInTitleBar(appBar, fullScreen)
	visibility := effectiveMenuVisibility(c.setting, c.zen, fullScreen)
	parsed, _ := kit.ParseMenuBarVisibility(visibility)
	menu := widgets.NewKeyedSubtree(c.menu(parsed), widgets.WidgetOpts{Key: PartKey(PartMenuBar)})

	// The menu is in the title bar, or, in full screen mode and in a window that has no such title bar, in a row of
	// its own over the workbench. The row is always there (with no height when it is not shown) so that what is below
	// it keeps its state.
	var top widgets.Widget = widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(0.0)})
	var leading []widgets.Widget
	if inBar {
		leading = append(leading, menu, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(menuGap)}))
	} else {
		height := 0.0
		if menuRowShown(visibility, false, c.shown) {
			height = MenuBarHeight
		}
		top = widgets.NewSizedBox(
			bordered(colors.TitleBarActiveBackground, colors.TitleBarBorder, sideBottom, widgets.NewAlign(menu, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})),
			widgets.SizedBoxOpts{Height: foundation.Some(height)})
	}
	content := widgets.NewColumn([]widgets.Widget{top, widgets.NewExpanded(s.Widget().child)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})

	// WindowDecoration (Glutter 1.8) moves, maximizes and shows the menu of the window for the title and the space
	// around it, and leaves Leading and Actions their own gestures. So the menu and the workspace tabs are Leading, and
	// the title is the empty space after them: a drag, a double click and a right click on a tab are the tab's.
	var title widgets.Widget = widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(0.0)})
	if titleBarRowShown(c.zen) && c.bar != nil {
		leading = append(leading, widgets.NewFlexible(newWorkspaceStrip(c.bar, !info.Activated), widgets.FlexibleOpts{Fit: rendering.FlexFitLoose}))
	}
	// WindowDecoration (Glutter 1.8.2) gives the leading widgets the room the bar leaves after its gap and the window
	// buttons: the loose tabs take what they need, the title (the empty part that moves the window) the rest.
	bg, fg := colors.TitleBarActiveBackground, colors.TitleBarActiveForeground
	if !info.Activated && colors.TitleBarInactiveBackground.Alpha() != 0 {
		// the inactive color is often a see-through one: it lies over the workbench's own background
		bg = ui.ColorAlphaBlend(colors.TitleBarInactiveBackground, colors.EditorBackground)
	}
	opts := widgets.WindowDecorationOpts{
		Title: title, Leading: leading,
		BackgroundColor: foundation.Some(bg), ForegroundColor: foundation.Some(fg),
		Height: foundation.Some(TitleBarHeight),
	}
	if colors.TitleBarBorder.Alpha() != 0 {
		opts.BorderColor = foundation.Some(colors.TitleBarBorder)
	}
	return widgets.NewWindowDecoration(content, opts)
}

// workspaceStrip is the workspace tabs and the "+" button: the Title of the window decoration. It sits over the part of
// the row that moves the window, and takes the presses of the pointer that the window's drag, double click and menu
// would else take.
type workspaceStrip struct {
	widgets.StatefulWidget
	bar       *TitleBarOptions
	unfocused bool
}

func newWorkspaceStrip(bar *TitleBarOptions, unfocused bool) *workspaceStrip {
	return &workspaceStrip{StatefulWidget: widgets.NewStatefulWidgetBase(), bar: bar, unfocused: unfocused}
}

// CreateState implements widgets.StatefulWidget.
func (w *workspaceStrip) CreateState() widgets.State {
	return &workspaceStripState{menu: kit.NewContextMenuController(), tabs: kit.NewTabsController(), feedback: kit.NewTabDropFeedback()}
}

// stripScrollInterval is the time between two steps of the scrolling of the strip for a dragged tab (a frame).
const stripScrollInterval = 16 * time.Millisecond

type workspaceStripState struct {
	widgets.StateOf[*workspaceStrip]
	menu *kit.ContextMenuController
	plus kit.Bounds
	// tabs and feedback tell where the tabs are and show where a dragged tab goes.
	tabs     *kit.TabsController
	feedback *kit.TabDropFeedback
	// pending is the id of the tab that was pressed and waits for its release to become the active one.
	pending string
	// dragged is the id of the tab that is dragged ("" for none), and at where the pointer is.
	dragged string
	at      ui.Offset
	scroll  *gestures.Timer
}

// InitState lets the controller of the "+" menu open it.
func (s *workspaceStripState) InitState() { s.attach() }

// DidUpdateWidget keeps the controller attached to this strip.
func (s *workspaceStripState) DidUpdateWidget(*workspaceStrip) { s.attach() }

// attach makes the controller of the options open the menu of this strip.
func (s *workspaceStripState) attach() {
	if c := s.Widget().bar.NewMenu; c != nil {
		c.open = s.openNewMenu
	}
}

// Dispose stops the scrolling of a drag and lets the controller go.
func (s *workspaceStripState) Dispose() {
	s.stopScroll()
	if c := s.Widget().bar.NewMenu; c != nil {
		c.open = nil
	}
}

// press is the press of a tab: the active tab is activated at once (that does nothing), another tab waits for the release
// (see TitleBarOptions.OnActivate).
func (s *workspaceStripState) press(i int) {
	bar := s.Widget().bar
	if i < 0 || i >= len(bar.Tabs) {
		return
	}
	if i == bar.Active {
		s.pending = ""
		if bar.OnActivate != nil {
			bar.OnActivate(i)
		}
		return
	}
	s.pending = bar.Tabs[i].ID
}

// release is the release of a button over the strip: a tab that was pressed and not dragged becomes the active one.
func (s *workspaceStripState) release() {
	id := s.pending
	s.pending = ""
	if id == "" {
		return
	}
	if bar := s.Widget().bar; bar.OnActivate != nil {
		if i := s.indexOf(id); i >= 0 {
			bar.OnActivate(i)
		}
	}
}

// indexOf is the place of the tab with an id, or -1.
func (s *workspaceStripState) indexOf(id string) int {
	for i, t := range s.Widget().bar.Tabs {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// dropIndex is the place between two tabs (0 before the first, the number of tabs after the last) a tab dropped with the
// pointer at x goes to.
func (s *workspaceStripState) dropIndex(x float64) int {
	var spans []editorgroup.Span
	for i := range s.Widget().bar.Tabs {
		r, ok := s.tabs.TabRect(i)
		if !ok {
			break
		}
		spans = append(spans, editorgroup.Span{Start: r.Left, End: r.Right})
	}
	return editorgroup.TabDropIndex(spans, x)
}

// dragStart begins the drag of the tab at an index; it says whether the tab is dragged (not when it is the only tab).
func (s *workspaceStripState) dragStart(i int) bool {
	bar := s.Widget().bar
	if bar.OnMove == nil || len(bar.Tabs) < 2 || i < 0 || i >= len(bar.Tabs) {
		return false
	}
	s.pending = "" // a drag is not a click
	s.dragged = bar.Tabs[i].ID
	return true
}

// dragMove follows the pointer of the drag.
func (s *workspaceStripState) dragMove(_ int, at ui.Offset) {
	if s.dragged == "" {
		return
	}
	s.at = at
	s.dragUpdate()
	s.startScroll()
}

// dragUpdate shows where the tab would go: no line when it would stay where it is.
func (s *workspaceStripState) dragUpdate() {
	from, to := s.indexOf(s.dragged), s.dropIndex(s.at.Dx)
	if from < 0 || to == from || to == from+1 {
		s.feedback.Set(-1)
		return
	}
	s.feedback.Set(to)
}

// dragEnd ends the drag: a drop moves the tab, a cancel (Escape) leaves everything as it was.
func (s *workspaceStripState) dragEnd(_ int, at ui.Offset, dropped bool) {
	s.stopScroll()
	s.feedback.Set(-1)
	id := s.dragged
	s.dragged = ""
	bar := s.Widget().bar
	if !dropped || id == "" || bar.OnMove == nil {
		return
	}
	from, to := s.indexOf(id), s.dropIndex(at.Dx)
	if from < 0 {
		return
	}
	if to > from {
		to-- // the tab leaves its place before it goes to the new one
	}
	if to != from {
		bar.OnMove(from, to)
	}
}

// startScroll starts the scrolling when the pointer of a drag is near an end of the strip.
func (s *workspaceStripState) startScroll() {
	if s.scroll != nil || s.dragged == "" {
		return
	}
	strip, ok := s.tabs.Bounds()
	if !ok || kit.TabEdgeStep(s.at.Dx, strip.Left, strip.Right) == 0 {
		return
	}
	s.scroll = widgets.WidgetsBindingOf(s.Context()).Gestures().NewTimer(stripScrollInterval, s.scrollTick)
}

// scrollTick scrolls the strip a step and finds again where the tab would go.
func (s *workspaceStripState) scrollTick() {
	s.scroll = nil
	if s.dragged == "" || !s.Mounted() {
		return
	}
	if strip, ok := s.tabs.Bounds(); ok {
		if step := kit.TabEdgeStep(s.at.Dx, strip.Left, strip.Right); step != 0 {
			s.tabs.ScrollBy(step)
			s.dragUpdate()
			s.startScroll()
		}
	}
}

// stopScroll cancels the scrolling.
func (s *workspaceStripState) stopScroll() {
	if s.scroll != nil {
		s.scroll.Cancel()
		s.scroll = nil
	}
}

// tabIcon is the icon of a tab in the color of its text.
func tabIcon(t WorkspaceTab, color ui.Color) widgets.Widget {
	code := fonts.CodiconWindow
	if t.Folder {
		code = fonts.CodiconRootFolder
	}
	if t.IconCode != 0 {
		code = t.IconCode
	}
	return widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(color)})
}

// Build implements widgets.State.
func (s *workspaceStripState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	bar := w.bar
	colors := kit.ThemeOf(bc)
	items := make([]kit.TabItem, len(bar.Tabs))
	for i, t := range bar.Tabs {
		fg := colors.TabInactiveForeground
		switch {
		case i == bar.Active && w.unfocused:
			fg = colors.TabUnfocusedActiveForeground
		case i == bar.Active:
			fg = colors.TabActiveForeground
		case w.unfocused:
			fg = colors.TabUnfocusedInactiveForeground
		}
		desc := ""
		if t.Description != "" {
			desc = "— " + t.Description
		}
		items[i] = kit.TabItem{Key: t.ID, Name: t.Name, Description: desc, Title: t.Tooltip, Icon: tabIcon(t, fg), Dirty: t.Dirty}
	}
	tabs := kit.NewTabs(items, kit.TabsOpts{
		Active: bar.Active, Unfocused: w.unfocused, CloseButton: kit.TabCloseRight, TitleBar: &kit.TabsTitleBar{},
		OnActivate: s.press,
		Controller: s.tabs, Feedback: s.feedback,
		OnDragStart: s.dragStart, OnDragMove: s.dragMove, OnDragEnd: s.dragEnd,
		OnClose: func(i int) {
			if bar.OnClose != nil {
				bar.OnClose(i)
			}
		},
		OnContextMenu: func(i int, at ui.Offset) {
			if bar.ContextItems == nil {
				return
			}
			if menu := bar.ContextItems(i); len(menu) > 0 {
				s.menu.Show(at, menu)
			}
		},
	})
	// the release of the press that activates a tab (a drag has taken the press by then)
	released := widgets.NewListener(tabs, widgets.ListenerOpts{
		Behavior:        rendering.HitTestBehaviorTranslucent,
		OnPointerUp:     func(*gestures.PointerUpEvent) { s.release() },
		OnPointerCancel: func(*gestures.PointerCancelEvent) { s.pending = "" },
	})
	row := widgets.NewRow([]widgets.Widget{
		widgets.NewFlexible(released, widgets.FlexibleOpts{Fit: rendering.FlexFitLoose}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(plusButtonGap)}),
		kit.NewBoundsProbe(newPlusButton(func() { s.openNewMenu() }), &s.plus),
	}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	return kit.NewContextMenu(row, kit.ContextMenuOpts{
		Controller: s.menu,
		OnRun: func(item kit.MenuItem) {
			if f := w.bar.OnRunItem; f != nil {
				f(item)
			}
		},
	})
}

// openNewMenu opens the menu of the "+" button under the button.
func (s *workspaceStripState) openNewMenu() {
	bar := s.Widget().bar
	if bar.NewItems == nil {
		return
	}
	items := bar.NewItems()
	if len(items) == 0 {
		return
	}
	at := ui.Offset{}
	if r, ok := s.plus.Rect(); ok {
		at = ui.Offset{Dx: r.Left, Dy: r.Bottom}
	}
	s.menu.Show(at, items)
}

// plusButton is the "+" button after the last workspace tab.
type plusButton struct {
	widgets.StatefulWidget
	onPress func()
}

func newPlusButton(onPress func()) *plusButton {
	return &plusButton{StatefulWidget: widgets.NewStatefulWidgetBase(), onPress: onPress}
}

// CreateState implements widgets.StatefulWidget.
func (b *plusButton) CreateState() widgets.State { return &plusButtonState{} }

type plusButtonState struct {
	widgets.StateOf[*plusButton]
	hover, down bool
}

// Build implements widgets.State.
func (s *plusButtonState) Build(bc widgets.BuildContext) widgets.Widget {
	colors := kit.ThemeOf(bc)
	var bg ui.Color
	if s.hover {
		bg = colors.ToolbarHoverBackground
	}
	box := widgets.NewContainer(
		widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconAdd), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(colors.TitleBarActiveForeground)}),
		widgets.ContainerOpts{
			Width: foundation.Some(plusButtonSize), Height: foundation.Some(plusButtonSize), Alignment: painting.AlignmentCenter,
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(bg), BorderRadius: painting.NewBorderRadiusCircular(5)}),
		})
	// A press and release on the button, with the raw pointer events of the tabs (a gesture detector would wait for the
	// double tap of the window to be ruled out).
	return widgets.NewMouseRegion(
		kit.NewHover(widgets.NewListener(box, widgets.ListenerOpts{
			Behavior: rendering.HitTestBehaviorOpaque,
			OnPointerDown: func(e *gestures.PointerDownEvent) {
				if e.Buttons()&gestures.PrimaryButton != 0 {
					s.down = true
				}
			},
			OnPointerUp: func(*gestures.PointerUpEvent) {
				if s.down && s.hover {
					s.down = false
					s.Widget().onPress()
				}
				s.down = false
			},
			OnPointerCancel: func(*gestures.PointerCancelEvent) { s.down = false },
		}), kit.HoverOpts{Text: "New Workspace Tab", Compact: true}),
		widgets.MouseRegionOpts{
			Cursor:  services.SystemMouseCursors.Basic,
			OnEnter: func(*gestures.PointerEnterEvent) { s.setHover(true) },
			OnExit:  func(*gestures.PointerExitEvent) { s.setHover(false) },
		})
}

func (s *plusButtonState) setHover(v bool) {
	if s.Mounted() {
		s.SetState(func() { s.hover = v })
	}
}
