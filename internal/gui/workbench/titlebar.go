package workbench

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

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
}

// TitleBarOptions are the workspace tabs of the title bar row and what the user does to them. A Workbench without them
// shows the menu and the buttons of the window only.
type TitleBarOptions struct {
	// Tabs are the workspace tabs in order, and Active the index of the one that is shown.
	Tabs   []WorkspaceTab
	Active int
	// OnActivate is called with the index of a tab that is pressed; OnClose with the index of a tab whose close button
	// is pressed or that is pressed with the middle button.
	OnActivate func(index int)
	OnClose    func(index int)
	// ContextItems gives the items of the context menu of the tab at an index when it is pressed with the secondary
	// button, and NewItems those of the menu of the "+" button. OnRunItem is called with the item the user runs in
	// either menu (the workbench's OnRunMenu when nil).
	ContextItems func(index int) []kit.MenuItem
	NewItems     func() []kit.MenuItem
	OnRunItem    func(item kit.MenuItem)
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

	var title widgets.Widget = widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(0.0)})
	if titleBarRowShown(c.zen) && c.bar != nil {
		title = newWorkspaceStrip(c.bar, !info.Activated)
	}
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
	return &workspaceStripState{menu: kit.NewContextMenuController()}
}

type workspaceStripState struct {
	widgets.StateOf[*workspaceStrip]
	menu *kit.ContextMenuController
	plus kit.Bounds
}

// tabIcon is the icon of a tab in the color of its text.
func tabIcon(t WorkspaceTab, color ui.Color) widgets.Widget {
	code := fonts.CodiconWindow
	if t.Folder {
		code = fonts.CodiconRootFolder
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
		OnActivate: func(i int) {
			if bar.OnActivate != nil {
				bar.OnActivate(i)
			}
		},
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
	row := widgets.NewRow([]widgets.Widget{
		widgets.NewFlexible(tabs, widgets.FlexibleOpts{Fit: rendering.FlexFitLoose}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(plusButtonGap)}),
		kit.NewBoundsProbe(newPlusButton(func() { s.openNewMenu() }), &s.plus),
	}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	// Glutter: WindowDecoration puts the whole bar (Leading, Title and Actions) under one detector that moves the
	// window on a drag, maximizes it on a double tap and shows the window menu on a secondary press, so a widget in the
	// bar gets them too. The strip takes the three with detectors of its own (the innermost one wins the arena); Glutter
	// should let the app say which part of the bar is the draggable one (see docs/GLUTTER-ISSUES.md).
	var strip widgets.Widget = widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{
		Behavior:           rendering.HitTestBehaviorTranslucent,
		OnPanStart:         func(gestures.DragStartDetails) {},
		OnDoubleTap:        func() {},
		OnSecondaryTapDown: func(gestures.TapDownDetails) {},
	})
	return kit.NewContextMenu(strip, kit.ContextMenuOpts{
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
