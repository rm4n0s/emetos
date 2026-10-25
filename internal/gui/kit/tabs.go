package kit

import (
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of VS Code's editor tabs (multieditortabscontrol.css).
const (
	// TabHeight is the height of the tab strip (`--editor-group-tab-height`).
	TabHeight = 35.0
	// tabMinWidth is the width of a tab with a short label (`sizing-fit`).
	tabMinWidth = 120.0
	// tabCompactWidth and tabShrinkWidth are the widths of a pinned tab in the compact and shrink
	// sizing (`sticky-compact`, `sticky-shrink`).
	tabCompactWidth = 38.0
	tabShrinkWidth  = 80.0
	// tabPaddingStart is the space before the label (`padding-left`), tabPaddingEnd the one after the
	// actions of a tab that has none.
	tabPaddingStart = 10.0
	tabPaddingEnd   = 10.0
	// tabActionWidth is the width of the box of the actions (`.tab-actions`), tabActionSize the box of
	// the action itself (a 16 px icon and 2 px of padding) and tabActionRadius its radius.
	tabActionWidth  = 28.0
	tabActionSize   = 20.0
	tabActionRadius = 5.0
	// tabFontSize is the size of the label; the description is 0.9 em of it, 70 % opaque, 0.5 em
	// after it.
	tabFontSize        = 13.0
	tabDescriptionSize = tabFontSize * 0.9
	tabDescriptionGap  = tabFontSize * 0.5
	tabDescriptionA    = 0.7
	// tabIconSize is the size of the icon of the file and tabIconGap the space after it.
	tabIconSize = 16.0
	tabIconGap  = 6.0
	// tabDirtyBorder is the height of the line along the top of a modified tab, tabBorderWidth the
	// one of the other lines.
	tabDirtyBorder = 2.0
	tabBorderWidth = 1.0
	// tabsScrollbarSize is the height of the scrollbar of the strip (`titleScrollbarSizing` default).
	tabsScrollbarSize = 3.0
	// tabActionDimmed is the opacity of an action in a tab of a group that is not the active one.
	tabActionDimmed = 0.5
	// tabDoubleClickInterval and tabDoubleClickSlop are what makes two presses a double click.
	tabDoubleClickInterval = 400 * time.Millisecond
	tabDoubleClickSlop     = 5.0
	// singleTabPadding is the space before the title of the header that shows one title.
	singleTabPadding = 8.0
)

// TabCloseButton says where the close button of a tab is, or that there is none
// (`workbench.editor.tabActionLocation` and `workbench.editor.tabActionCloseVisibility`).
type TabCloseButton int

const (
	// TabCloseRight puts the close button after the label (the default).
	TabCloseRight TabCloseButton = iota
	// TabCloseLeft puts it before the label.
	TabCloseLeft
	// TabCloseOff shows no close button; a modified tab still shows its dot.
	TabCloseOff
)

// PinnedTabSizing is the size of the tabs of sticky editors (`workbench.editor.pinnedTabSizing`).
type PinnedTabSizing int

const (
	// PinnedNormal gives a pinned tab the look of the others (the default).
	PinnedNormal PinnedTabSizing = iota
	// PinnedCompact shows a pinned tab as narrow as the icon, or the first letter of the name when
	// there is no icon.
	PinnedCompact
	// PinnedShrink shows a pinned tab at a fixed small width, with the start of the name.
	PinnedShrink
)

// TabsMode is what the strip shows (`workbench.editor.showTabs`).
type TabsMode int

const (
	// TabsMultiple shows every editor as a tab (the default).
	TabsMultiple TabsMode = iota
	// TabsSingle shows the active editor only, as one large title.
	TabsSingle
)

// TabItem is the look of one tab.
type TabItem struct {
	// Key identifies the tab between builds; it must be unique, such as the resource of the editor.
	Key string
	// Name is the label; Description is the dimmer text after it (the folder, when another tab has
	// the same name); Title is the text of the tooltip.
	Name, Description, Title string
	// Icon is the icon of the file, drawn before the label in a 16 px box; nil shows none. The file
	// icon theme of step 6.14 fills it.
	Icon widgets.Widget
	// Preview shows the label in italics: the editor is the preview editor.
	Preview bool
	// Sticky marks a pinned tab: it shows the pin in place of the close button.
	Sticky bool
	// Dirty shows the dot of unsaved changes in place of the close button.
	Dirty bool
	// Deleted marks an editor whose file another program deleted (VS Code's file decoration): the name and the
	// description are struck through, and drawn in list.errorForeground when TabsOpts.DecorationColors is set.
	Deleted bool
}

// TabsOpts are the options of NewTabs.
type TabsOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Active is the index of the active tab; -1 for none.
	Active int
	// Unfocused shows the tabs in the colors of a group that is not the active group.
	Unfocused bool
	// CloseButton is where the close button is.
	CloseButton TabCloseButton
	// PinnedSizing is the size of the tabs of sticky editors.
	PinnedSizing PinnedTabSizing
	// HighlightModified draws a line along the top of modified tabs
	// (`workbench.editor.highlightModifiedTabs`).
	HighlightModified bool
	// DecorationColors draws the label of a deleted tab in the error color (`workbench.editor.decorations.colors`).
	DecorationColors bool
	// Mode chooses between the tabs and the title of the active editor alone.
	Mode TabsMode
	// OnActivate is called with the index of a tab that is pressed with the primary button.
	OnActivate func(index int)
	// OnClose is called with the index of a tab whose close button is clicked or that is clicked with
	// the middle button.
	OnClose func(index int)
	// OnPin is called with the index of a preview tab that is double clicked: the editor is kept.
	OnPin func(index int)
	// OnUnpin is called with the index of a sticky tab whose pin is clicked.
	OnUnpin func(index int)
	// OnContextMenu is called with the index of a tab that is pressed with the secondary button, and
	// the place of the pointer in the window: the window shows the menu of the tab.
	OnContextMenu func(index int, at ui.Offset)
	// OnDragStart is called when the pointer, held down with the primary button on the tab at an
	// index, has moved beyond the slop of 4 px; it says whether the tab is dragged (without it, or
	// when it says no, the tab cannot be dragged). The tab has been activated by the press.
	OnDragStart func(index int) bool
	// OnDragMove is called with each move of the pointer while the tab is dragged, with its position
	// in the window.
	OnDragMove func(index int, at ui.Offset)
	// OnDragEnd is called when the drag ends: dropped is true when the pointer was released, false
	// when Escape or the system cancelled the drag. The position is where the pointer was.
	OnDragEnd func(index int, at ui.Offset, dropped bool)
	// Feedback shows where a dragged tab would go (a line between two tabs); nil shows none.
	Feedback *TabDropFeedback
	// Controller, when set, tells where the strip and its tabs are and scrolls the strip.
	Controller *TabsController
	// Actions, when set, makes the widget placed at the right end of the strip, outside the part that scrolls: the editor
	// title actions of the extensions (SPEC §10.15). It is given the width of the whole strip, so that it can decide how
	// many icons fit, and is centered in the height of the strip; it may return nil for none.
	Actions func(width float64) widgets.Widget
	// TitleBar, when set, makes the strip the workspace tabs of the title bar (SPEC §10.14): tabs between MinWidth and
	// MaxWidth that shrink to MinWidth before the strip scrolls, an inactive tab on the background of the row (it has no
	// background of its own and no lines), the active tab with its top line only, the close button on the active tab
	// and under the pointer, and the dot of a modified tab giving way to the close button while the pointer is over the
	// tab. The strip is as wide as its tabs (at most the room it has) and as high as TabHeight.
	TitleBar *TabsTitleBar
}

// TabsTitleBar is TabsOpts.TitleBar.
type TabsTitleBar struct {
	// MinWidth and MaxWidth are the widths a tab has between (WorkspaceTabMinWidth and WorkspaceTabMaxWidth when 0).
	MinWidth, MaxWidth float64
}

func (b *TabsTitleBar) widths() (minW, maxW float64) {
	minW, maxW = WorkspaceTabMinWidth, WorkspaceTabMaxWidth
	if b.MinWidth > 0 {
		minW = b.MinWidth
	}
	if b.MaxWidth > 0 {
		maxW = b.MaxWidth
	}
	return minW, max(maxW, minW)
}

// Tabs is the strip of tabs of an editor group: one tab for each editor, 35 px high, with the
// label of the editor (its description dimmer after it), an icon slot, the preview tab in
// italics, the pin of a sticky tab and a dot for unsaved changes that becomes the close "x" under
// the pointer. A tab is pressed to activate it, double clicked to keep a preview tab, clicked with
// the middle button or on its "x" to close it. The strip scrolls sideways (the wheel, a
// scrollbar) when the tabs do not fit, and shows the active tab. With TabsSingle it shows the
// title of the active editor alone. A tab is dragged by the primary button (TabsOpts.OnDragStart, OnDragMove
// and OnDragEnd; Escape cancels) and shows where a dragged tab goes (Feedback), and the secondary button
// asks for the context menu (OnContextMenu); the window does what a drop means.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/multiEditorTabsControl.ts,
// src/vs/workbench/browser/parts/editor/singleEditorTabsControl.ts and
// src/vs/workbench/browser/parts/editor/media/multieditortabscontrol.css
type Tabs struct {
	widgets.StatelessWidget
	items []TabItem
	opts  TabsOpts
}

// NewTabs makes the strip for the tabs, in order.
func NewTabs(items []TabItem, opts ...TabsOpts) *Tabs {
	t := &Tabs{items: items}
	if len(opts) > 0 {
		t.opts = opts[0]
	}
	t.StatelessWidget = widgets.NewStatelessWidgetBase(widgets.WidgetOpts{Key: t.opts.Key})
	return t
}

// Items are the tabs.
func (t *Tabs) Items() []TabItem { return t.items }

// Active is the index of the active tab.
func (t *Tabs) Active() int { return t.opts.Active }

// Feedback is the line that shows where a dragged tab would go, or nil for a strip that has none.
func (t *Tabs) Feedback() *TabDropFeedback { return t.opts.Feedback }

// TitleBar is the look of the strip of workspace tabs of the title bar, or nil for the strip of an editor group.
func (t *Tabs) TitleBar() *TabsTitleBar { return t.opts.TitleBar }

// Build implements widgets.StatelessWidget.
func (t *Tabs) Build(bc widgets.BuildContext) widgets.Widget {
	colors := ThemeOf(bc)
	if t.opts.Mode == TabsSingle {
		return t.buildSingle(bc, colors)
	}
	if t.opts.TitleBar != nil {
		return t.buildTitleBar(bc)
	}
	stickyCount := 0
	for _, it := range t.items {
		if it.Sticky {
			stickyCount++
		}
	}
	children := make([]widgets.Widget, len(t.items))
	for i, it := range t.items {
		var probe *Bounds
		if t.opts.Controller != nil {
			probe = t.opts.Controller.bounds(i)
		}
		children[i] = newTabWidget(tabSpec{
			item: it, index: i, count: len(t.items), opts: t.opts, bounds: probe,
			active:        i == t.opts.Active,
			lastSticky:    it.Sticky && i == stickyCount-1 && stickyCount != len(t.items),
			stripBorder:   colors.EditorGroupHeaderTabsBorder,
			compactLetter: compactLetter(it),
		})
	}
	scrollOpts := ScrollableElementOpts{
		Axes: ScrollAxisHorizontal, HorizontalSize: foundation.Some(tabsScrollbarSize), ScrollYToX: true,
		NoSmoothScroll: true,
	}
	if c := t.opts.Controller; c != nil {
		scrollOpts.Handle = &c.scroll
	}
	var strip widgets.Widget = NewScrollableElement(
		widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
		scrollOpts)
	layers := []widgets.Widget{}
	if colors.EditorGroupHeaderTabsBorder.Alpha() != 0 {
		// along the bottom, where there are no tabs; every tab draws its own part of it
		layers = append(layers, widgets.NewPositioned(
			widgets.NewColoredBox(colors.EditorGroupHeaderTabsBorder, widgets.NewSizedBox(nil)),
			widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0), Bottom: foundation.Some(0.0), Height: foundation.Some(tabBorderWidth)}))
	}
	if t.opts.Actions != nil {
		scrolling := strip
		strip = widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
			actions := t.opts.Actions(c.MaxWidth)
			if actions == nil {
				return scrolling
			}
			return widgets.NewRow([]widgets.Widget{widgets.NewExpanded(scrolling), widgets.NewCenter(actions)},
				widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
		})
	}
	layers = append(layers, widgets.NewPositionedFill(strip))
	var box widgets.Widget = widgets.NewSizedBox(
		widgets.NewColoredBox(colors.EditorGroupHeaderTabsBackground, widgets.NewStack(layers, widgets.StackOpts{Fit: rendering.StackFitExpand})),
		widgets.SizedBoxOpts{Height: foundation.Some(TabHeight)})
	if c := t.opts.Controller; c != nil {
		box = NewBoundsProbe(box, &c.strip)
	}
	return box
}

// buildTitleBar is the strip of the workspace tabs of the title bar: every tab has the width FitTabWidths gives it for
// the room the strip has.
func (t *Tabs) buildTitleBar(bc widgets.BuildContext) widgets.Widget {
	tb := t.opts.TitleBar
	minW, maxW := tb.widths()
	natural := make([]float64, len(t.items))
	for i, it := range t.items {
		natural[i] = tabNaturalWidth(bc, it)
	}
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		room := c.MaxWidth
		if math.IsInf(room, 1) {
			room = 0
		}
		widths, _ := FitTabWidths(natural, room, minW, maxW)
		total := 0.0
		children := make([]widgets.Widget, len(t.items))
		for i, it := range t.items {
			var probe *Bounds
			if t.opts.Controller != nil {
				probe = t.opts.Controller.bounds(i)
			}
			total += widths[i]
			children[i] = newTabWidget(tabSpec{
				item: it, index: i, count: len(t.items), opts: t.opts, bounds: probe,
				active: i == t.opts.Active, width: widths[i], titleBar: true,
			})
		}
		scrollOpts := ScrollableElementOpts{
			Axes: ScrollAxisHorizontal, HorizontalSize: foundation.Some(tabsScrollbarSize), ScrollYToX: true,
			NoSmoothScroll: true,
		}
		if ctl := t.opts.Controller; ctl != nil {
			scrollOpts.Handle = &ctl.scroll
		}
		strip := NewScrollableElement(
			widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
			scrollOpts)
		if room > 0 {
			total = min(total, room)
		}
		var box widgets.Widget = widgets.NewSizedBox(strip, widgets.SizedBoxOpts{Width: foundation.Some(total), Height: foundation.Some(TabHeight)})
		if ctl := t.opts.Controller; ctl != nil {
			box = NewBoundsProbe(box, &ctl.strip)
		}
		return box
	})
}

// tabNaturalWidth is the width a workspace tab wants: the space before the label, the icon, the name and the description
// and the box of the close button.
func tabNaturalWidth(bc widgets.BuildContext, it TabItem) float64 {
	w := tabPaddingStart + tabActionWidth + tabLabelWidth(bc, it.Name, tabFontSize)
	if it.Icon != nil {
		w += tabIconSize + tabIconGap
	}
	if it.Description != "" {
		w += tabLabelWidth(bc, " "+it.Description, tabDescriptionSize)
	}
	return w
}

// tabLabelWidth is how wide a text is in the font of the tabs.
func tabLabelWidth(bc widgets.BuildContext, text string, size float64) float64 {
	engine := widgets.WidgetsBindingOf(bc).PlatformDispatcher().TextEngine()
	if engine == nil {
		return float64(utf8.RuneCountInString(text)) * size * 0.55
	}
	painter := painting.NewTextPainter(painting.TextPainterOpts{
		TextEngine: engine, Text: painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text), Style: tabTextStyle(bc, size, ui.Color{}, false, false)}),
		TextDirection: ui.TextDirectionLTR, MaxLines: foundation.Some(1),
	})
	defer painter.Dispose()
	painter.Layout(0, math.Inf(1))
	return math.Ceil(painter.MaxIntrinsicWidth())
}

// compactLetter is what a compact pinned tab without an icon shows: the first letter of the name.
func compactLetter(it TabItem) string {
	if it.Name == "" {
		return ""
	}
	r, _ := utf8.DecodeRuneInString(it.Name)
	return strings.ToUpper(string(r))
}

// buildSingle is the header that shows the title of the active editor alone: the name and the
// description, in italics for a preview editor, with the dot of unsaved changes.
func (t *Tabs) buildSingle(bc widgets.BuildContext, colors Colors) widgets.Widget {
	var body widgets.Widget = widgets.NewSizedBox(nil)
	if t.opts.Active >= 0 && t.opts.Active < len(t.items) {
		it := t.items[t.opts.Active]
		fg := colors.TabActiveForeground
		row := []widgets.Widget{}
		if it.Icon != nil {
			row = append(row, widgets.NewSizedBox(it.Icon, widgets.SizedBoxOpts{Width: foundation.Some(tabIconSize), Height: foundation.Some(tabIconSize)}),
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(tabIconGap)}))
		}
		row = append(row, tabLabel(bc, it, decoratedForeground(bc, it, t.opts.DecorationColors, fg), tabFontSize, false)...)
		if it.Dirty {
			row = append(row, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(tabIconGap, 0, 0, 0),
				widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconCircleFilled), widgets.IconOpts{Size: foundation.Some(tabIconSize), Color: foundation.Some(fg)})))
		}
		body = widgets.NewAlign(
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(singleTabPadding, 0, singleTabPadding, 0),
				widgets.NewRow(row, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
			widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})
		if it.Title != "" {
			body = NewHover(body, HoverOpts{Text: it.Title, Compact: true})
		}
	}
	if t.opts.Actions != nil {
		title := body
		body = widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
			actions := t.opts.Actions(c.MaxWidth)
			if actions == nil {
				return title
			}
			return widgets.NewRow([]widgets.Widget{widgets.NewExpanded(title), widgets.NewCenter(actions)},
				widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
		})
	}
	deco := painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Color:  foundation.Some(colors.EditorGroupHeaderNoTabsBackground),
		Border: painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.EditorGroupHeaderBorder), Width: foundation.Some(tabBorderWidth)})},
	})
	if colors.EditorGroupHeaderBorder.Alpha() == 0 {
		deco = painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(colors.EditorGroupHeaderNoTabsBackground)})
	}
	return widgets.NewContainer(body, widgets.ContainerOpts{Height: foundation.Some(TabHeight), Decoration: deco})
}

// decoratedForeground is the color of the label of a tab: the error color for a deleted editor when the
// decorations use colors, else the one the tab has.
func decoratedForeground(bc widgets.BuildContext, it TabItem, colors bool, fg ui.Color) ui.Color {
	if it.Deleted && colors {
		if c := ThemeOf(bc).ListErrorForeground; c.Alpha() != 0 {
			return c
		}
	}
	return fg
}

// titleBarLabel is the label of a workspace tab: the name and the dimmer description in one line of text, which an
// ellipsis shortens when the tab is narrower than its label (the description goes first).
func titleBarLabel(bc widgets.BuildContext, it TabItem, fg ui.Color) []widgets.Widget {
	spans := []painting.InlineSpan{painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(it.Name)})}
	if it.Description != "" {
		desc := tabTextStyle(bc, tabDescriptionSize, fg.WithOpacity(fg.Opacity()*tabDescriptionA), false, false)
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(" " + it.Description), Style: desc}))
	}
	return []widgets.Widget{widgets.NewFlexible(widgets.NewTextRich(
		painting.NewTextSpan(painting.TextSpanOpts{Style: tabTextStyle(bc, tabFontSize, fg, false, false), Children: spans}),
		widgets.TextOpts{Overflow: painting.TextOverflowEllipsis, SoftWrap: foundation.Some(false), MaxLines: foundation.Some(1)}))}
}

// tabLabel is the name of a tab and, when it has one, its description.
func tabLabel(bc widgets.BuildContext, it TabItem, fg ui.Color, size float64, clip bool, ellipsis ...bool) []widgets.Widget {
	name := tabTextStyle(bc, size, fg, it.Preview, it.Deleted)
	overflow := painting.TextOverflowVisible
	if clip {
		overflow = painting.TextOverflowClip
		if len(ellipsis) > 0 && ellipsis[0] {
			overflow = painting.TextOverflowEllipsis
		}
	}
	// a label that is clipped shares the room it has; one that is not takes what it needs
	fit := func(w widgets.Widget) widgets.Widget {
		if clip {
			return widgets.NewFlexible(w)
		}
		return w
	}
	out := []widgets.Widget{fit(widgets.NewText(it.Name, widgets.TextOpts{Style: name, Overflow: overflow, SoftWrap: foundation.Some(false), MaxLines: foundation.Some(1)}))}
	if it.Description != "" {
		desc := tabTextStyle(bc, tabDescriptionSize, fg.WithOpacity(fg.Opacity()*tabDescriptionA), it.Preview, it.Deleted)
		out = append(out, fit(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(tabDescriptionGap, 0, 0, 0),
			widgets.NewText(it.Description, widgets.TextOpts{Style: desc, Overflow: overflow, SoftWrap: foundation.Some(false), MaxLines: foundation.Some(1)}))))
	}
	return out
}

// tabTextStyle is the text style of a label, in italics for a preview editor and struck through for a deleted one.
func tabTextStyle(bc widgets.BuildContext, size float64, color ui.Color, italic, strikethrough bool) *painting.TextStyle {
	opts := painting.TextStyleOpts{
		FontFamily:         UIFontOf(bc),
		FontFamilyFallback: []string{"sans-serif"},
		FontSize:           foundation.Some(size),
		Color:              foundation.Some(color),
	}
	if italic {
		opts.FontStyle = ui.FontStyleItalic
	}
	if strikethrough {
		opts.Decoration = foundation.Some(ui.TextDecorationLineThrough)
	}
	return painting.NewTextStyle(opts)
}

// tabSpec is everything one tab needs to draw.
type tabSpec struct {
	item  TabItem
	index int
	count int
	opts  TabsOpts
	// bounds tells the controller where the tab is; nil without one.
	bounds *Bounds
	active bool
	// lastSticky marks the last pinned tab when other tabs follow it: it has its own right border.
	lastSticky bool
	// stripBorder is the line along the bottom of the strip, which the tab draws its part of.
	stripBorder ui.Color
	// compactLetter is what a compact pinned tab without an icon shows.
	compactLetter string
	// titleBar says that the tab is a workspace tab of the title bar (TabsOpts.TitleBar), width pixels wide.
	titleBar bool
	width    float64
}

// tabWidget is one tab. It keeps what the pointer does to it: the hover, the hover over its action
// and the presses that make a double click.
type tabWidget struct {
	widgets.StatefulWidget
	spec tabSpec
}

func newTabWidget(spec tabSpec) *tabWidget {
	return &tabWidget{StatefulWidget: widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: foundation.NewKey(spec.item.Key)}), spec: spec}
}

// CreateState implements widgets.StatefulWidget.
func (w *tabWidget) CreateState() widgets.State { return &tabState{} }

type tabState struct {
	widgets.StateOf[*tabWidget]
	hover, actionHover bool
	// pressedOnAction is set by the press on the action, which the press on the tab then ignores.
	pressedOnAction bool
	// middleDown is a press of the middle button waiting for its release.
	middleDown bool
	// lastStamp and lastAt are the last press of the primary button, with hasLast set while it can
	// still be the first of a double click.
	lastStamp time.Duration
	lastAt    ui.Offset
	hasLast   bool

	// The drag: a press of the primary button waits for the pointer to move beyond the slop; once it
	// has, the tab is dragged until the button is released or Escape or the system cancels it.
	pressed   bool
	pointer   int
	start     ui.Offset
	dragging  bool
	cancelled bool
	lastAtPos ui.Offset
	escape    services.KeyHandlerHandle
	hasEscape bool
	feedback  *TabDropFeedback
	feedbackH foundation.ListenerHandle
	// binding is kept for Dispose, which runs when the context is gone.
	binding *widgets.WidgetsBinding
}

// InitState reveals a tab that is active when it appears, and follows the feedback of the drop.
func (s *tabState) InitState() {
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.follow()
	if s.Widget().spec.active {
		s.reveal()
	}
}

// DidUpdateWidget reveals a tab that has become the active one.
func (s *tabState) DidUpdateWidget(old *tabWidget) {
	s.follow()
	if !old.spec.active && s.Widget().spec.active {
		s.reveal()
	}
}

// Dispose ends a drag that goes on and stops following the feedback.
func (s *tabState) Dispose() {
	if s.dragging {
		// the tree is locked while a state is disposed: the window is told when it is not
		spec, at := s.Widget().spec, s.lastAtPos
		s.endDragQuietly()
		if f := spec.opts.OnDragEnd; f != nil {
			s.binding.Post(func() { f(spec.index, at, false) })
		}
	}
	s.unfollow()
}

// follow listens to the feedback of the strip, to draw the line where a dragged tab would go.
func (s *tabState) follow() {
	f := s.Widget().spec.opts.Feedback
	if f == s.feedback {
		return
	}
	s.unfollow()
	if f != nil {
		s.feedback, s.feedbackH = f, f.AddListener(func() { s.set(func() {}) })
	}
}

func (s *tabState) unfollow() {
	if s.feedback != nil {
		s.feedback.RemoveListener(s.feedbackH)
		s.feedback = nil
	}
}

// reveal scrolls the strip, after the frame, just far enough to show the tab.
func (s *tabState) reveal() {
	scheduler := widgets.WidgetsBindingOf(s.Context()).Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if !s.Mounted() {
			return
		}
		for _, policy := range []widgets.ScrollPositionAlignmentPolicy{
			widgets.ScrollPositionAlignmentPolicyKeepVisibleAtEnd, widgets.ScrollPositionAlignmentPolicyKeepVisibleAtStart,
		} {
			widgets.ScrollableEnsureVisible(s.Context(), widgets.EnsureVisibleOpts{AlignmentPolicy: policy})
		}
	})
}

func (s *tabState) set(f func()) {
	if s.Mounted() {
		s.SetState(f)
	}
}

// down is the press of a button on the tab.
func (s *tabState) down(e *gestures.PointerDownEvent) {
	spec := s.Widget().spec
	if s.pressedOnAction {
		s.pressedOnAction = false
		return // a press on the action is not a press on the tab
	}
	switch {
	case e.Buttons()&gestures.TertiaryButton != 0:
		s.middleDown = true
	case e.Buttons()&gestures.SecondaryButton != 0:
		if f := spec.opts.OnContextMenu; f != nil {
			f(spec.index, e.Position())
		}
	case e.Buttons()&gestures.PrimaryButton != 0:
		if f := spec.opts.OnActivate; f != nil {
			f(spec.index)
		}
		s.pressed, s.pointer, s.start, s.cancelled = true, e.Pointer(), e.Position(), false
		at, stamp := e.Position(), e.TimeStamp()
		second := s.hasLast && stamp-s.lastStamp <= tabDoubleClickInterval &&
			abs64(at.Dx-s.lastAt.Dx) <= tabDoubleClickSlop && abs64(at.Dy-s.lastAt.Dy) <= tabDoubleClickSlop
		s.lastStamp, s.lastAt, s.hasLast = stamp, at, !second
		if second && spec.item.Preview && spec.opts.OnPin != nil {
			spec.opts.OnPin(spec.index)
		}
	}
}

// move follows the pointer that is pressed on the tab: past the slop it starts a drag.
//
// The drag of a tab is built on the pointer events of the tab's Listener, with a slop of 4 px and an Escape handler
// of the keyboard, not on widgets.Draggable (Glutter 1.7 has its Slop and CancelOnEscape): the window finds the strips
// and the editor areas the tab can be dropped on by their places on the screen (kit.Bounds, TabsController, and
// shellState.scrollNearEdge in the app), over several groups, which a Draggable with one DragTarget per strip would
// not make simpler, and the press that starts the drag also activates the tab and counts double clicks.
func (s *tabState) move(e *gestures.PointerMoveEvent) {
	if !s.pressed || e.Pointer() != s.pointer || s.cancelled {
		return
	}
	spec := s.Widget().spec
	pos := e.Position()
	s.lastAtPos = pos
	if !s.dragging {
		if math.Hypot(pos.Dx-s.start.Dx, pos.Dy-s.start.Dy) < dragSlop {
			return
		}
		if spec.opts.OnDragStart == nil || !spec.opts.OnDragStart(spec.index) {
			s.pressed = false
			return
		}
		s.dragging = true
		s.hasLast = false // a drag is not a click
		s.escape = s.binding.Keyboard().AddHandler(func(k services.KeyEvent) bool {
			if _, down := k.(*services.KeyDownEvent); down && k.LogicalKey() == services.LogicalKeyboardKeyEscape && s.dragging {
				s.cancelled = true
				s.endDrag(false)
				return true
			}
			return false
		})
		s.hasEscape = true
	}
	if f := spec.opts.OnDragMove; f != nil {
		f(spec.index, pos)
	}
}

// endDragQuietly ends the drag that goes on without telling the window.
func (s *tabState) endDragQuietly() {
	if s.hasEscape {
		s.binding.Keyboard().RemoveHandler(s.escape)
		s.hasEscape = false
	}
	s.dragging = false
}

// endDrag ends the drag that goes on and tells the window.
func (s *tabState) endDrag(dropped bool) {
	s.endDragQuietly()
	spec := s.Widget().spec
	if f := spec.opts.OnDragEnd; f != nil {
		f(spec.index, s.lastAtPos, dropped)
	}
}

// cancelPress is the system taking the pointer away.
func (s *tabState) cancelPress(e *gestures.PointerCancelEvent) {
	if !s.pressed || e.Pointer() != s.pointer {
		return
	}
	s.pressed = false
	if s.dragging {
		s.endDrag(false)
	}
}

// up is the release of a button: the primary button drops a dragged tab, the middle button closes the
// tab.
func (s *tabState) up(e *gestures.PointerUpEvent) {
	if s.pressed && e.Pointer() == s.pointer {
		s.pressed = false
		if s.dragging {
			s.lastAtPos = e.Position()
			s.endDrag(true)
		}
	}
	if !s.middleDown {
		return
	}
	s.middleDown = false
	spec := s.Widget().spec
	if f := spec.opts.OnClose; f != nil {
		f(spec.index)
	}
}

// colors picks the background and the text of the tab.
func (s *tabState) colors(c Colors) (background, foreground ui.Color) {
	spec := s.Widget().spec
	focused := !spec.opts.Unfocused
	defer func() {
		// an inactive workspace tab is on the background of the title bar
		if spec.titleBar && !spec.active && !s.hover {
			background = ui.Color{}
		}
	}()
	switch {
	case spec.active && focused:
		background, foreground = c.TabActiveBackground, c.TabActiveForeground
	case spec.active:
		background, foreground = c.TabUnfocusedActiveBackground, c.TabUnfocusedActiveForeground
	case focused:
		background, foreground = c.TabInactiveBackground, c.TabInactiveForeground
	default:
		background, foreground = c.TabUnfocusedInactiveBackground, c.TabUnfocusedInactiveForeground
	}
	if s.hover {
		hoverBackground, hoverForeground := c.TabHoverBackground, c.TabHoverForeground
		if !focused {
			hoverBackground, hoverForeground = c.TabUnfocusedHoverBackground, c.TabUnfocusedHoverForeground
		}
		if hoverBackground.Alpha() != 0 {
			background = hoverBackground
		}
		if hoverForeground.Alpha() != 0 {
			foreground = hoverForeground
		}
	}
	return background, foreground
}

// modifiedBorder is the color of the line along the top of a modified tab.
func modifiedBorder(c Colors, active, focused bool) ui.Color {
	switch {
	case focused && active:
		return c.TabActiveModifiedBorder
	case focused:
		return c.TabInactiveModifiedBorder
	case active:
		return c.TabUnfocusedActiveModifiedBorder
	}
	return c.TabUnfocusedInactiveModifiedBorder
}

// Build implements widgets.State.
func (s *tabState) Build(bc widgets.BuildContext) widgets.Widget {
	spec := s.Widget().spec
	it, o := spec.item, spec.opts
	colors := ThemeOf(bc)
	focused := !o.Unfocused
	background, foreground := s.colors(colors)

	compact := it.Sticky && o.PinnedSizing == PinnedCompact
	shrink := it.Sticky && o.PinnedSizing == PinnedShrink

	// the lines: the right one of every tab, along the bottom of the active tab or the strip, along
	// the top of the active tab or of a modified one
	rightColor := colors.TabBorder
	if spec.lastSticky && colors.TabLastPinnedBorder.Alpha() != 0 {
		rightColor = colors.TabLastPinnedBorder
	}
	var topColor ui.Color
	topHeight := tabBorderWidth
	if it.Dirty && o.HighlightModified {
		if c := modifiedBorder(colors, spec.active, focused); c.Alpha() != 0 {
			topColor, topHeight = c, tabDirtyBorder
		}
	}
	if topColor.Alpha() == 0 && spec.active {
		topColor = colors.TabActiveBorderTop
		if !focused {
			topColor = colors.TabUnfocusedActiveBorderTop
		}
		if topColor.Alpha() == 0 && spec.titleBar {
			// the title bar has no line of the strip to tell the active tab, so the line falls back to the focus border
			topColor = colors.FocusBorder
		}
	}
	bottomColor := spec.stripBorder
	if spec.active {
		bottomColor = colors.TabActiveBorder
		if !focused {
			bottomColor = colors.TabUnfocusedActiveBorder
		}
	}
	if spec.titleBar {
		// the workspace tabs of the title bar have no lines between them or along the bottom
		rightColor, bottomColor = ui.Color{}, ui.Color{}
	}

	// the label
	var content []widgets.Widget
	switch {
	case compact && it.Icon == nil:
		content = tabLabel(bc, TabItem{Name: spec.compactLetter, Preview: it.Preview, Deleted: it.Deleted}, decoratedForeground(bc, it, o.DecorationColors, foreground), tabFontSize, true)
	case compact:
		content = nil
	case spec.titleBar:
		content = titleBarLabel(bc, it, foreground)
	default:
		content = tabLabel(bc, it, decoratedForeground(bc, it, o.DecorationColors, foreground), tabFontSize, shrink)
	}
	label := []widgets.Widget{}
	if it.Icon != nil {
		label = append(label, widgets.NewSizedBox(it.Icon, widgets.SizedBoxOpts{Width: foundation.Some(tabIconSize), Height: foundation.Some(tabIconSize)}))
		if len(content) > 0 {
			label = append(label, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(tabIconGap)}))
		}
	}
	label = append(label, content...)
	labelRow := widgets.NewRow(label, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	var labelBox widgets.Widget = labelRow

	// the action: the close button, the pin or the dot
	action, hasAction := s.action(bc, colors, foreground, compact, focused)

	closeLeft := o.CloseButton == TabCloseLeft && hasAction
	startPad, endPad := tabPaddingStart, 0.0
	if !hasAction {
		endPad = tabPaddingEnd
	}
	if closeLeft {
		startPad, endPad = 0, tabPaddingEnd
	}
	var cells []widgets.Widget
	labelCell := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(startPad, 0, endPad, 0), labelBox)
	switch {
	case compact:
		cells = []widgets.Widget{widgets.NewExpanded(widgets.NewCenter(labelBox))}
	case closeLeft:
		cells = []widgets.Widget{action, widgets.NewExpanded(labelCell)}
	case hasAction:
		cells = []widgets.Widget{widgets.NewExpanded(labelCell), action}
	default:
		cells = []widgets.Widget{widgets.NewExpanded(labelCell)}
	}
	row := widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})

	var sized widgets.Widget
	switch {
	case compact:
		sized = widgets.NewSizedBox(row, widgets.SizedBoxOpts{Width: foundation.Some(tabCompactWidth)})
	case shrink:
		sized = widgets.NewSizedBox(row, widgets.SizedBoxOpts{Width: foundation.Some(tabShrinkWidth)})
	case spec.titleBar:
		sized = widgets.NewSizedBox(row, widgets.SizedBoxOpts{Width: foundation.Some(spec.width)})
	default:
		// as wide as the label and the action, and at least 120 px (the action then stays at the end)
		sized = widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinWidth: foundation.Some(tabMinWidth)}),
			widgets.NewIntrinsicWidth(row))
	}

	line := func(color ui.Color, opts widgets.PositionedOpts) widgets.Widget {
		return widgets.NewPositioned(widgets.NewColoredBox(color, widgets.NewSizedBox(nil)), opts)
	}
	layers := []widgets.Widget{widgets.NewSizedBox(sized, widgets.SizedBoxOpts{Height: foundation.Some(TabHeight)})}
	if rightColor.Alpha() != 0 {
		layers = append(layers, line(rightColor, widgets.PositionedOpts{Top: foundation.Some(0.0), Bottom: foundation.Some(0.0), Right: foundation.Some(0.0), Width: foundation.Some(tabBorderWidth)}))
	}
	if bottomColor.Alpha() != 0 {
		layers = append(layers, line(bottomColor, widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0), Bottom: foundation.Some(0.0), Height: foundation.Some(tabBorderWidth)}))
	}
	if topColor.Alpha() != 0 {
		layers = append(layers, line(topColor, widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0), Top: foundation.Some(0.0), Height: foundation.Some(topHeight)}))
	}
	// the line that shows where a dragged tab goes: before this tab, or after the last one
	if f := o.Feedback; f != nil && colors.TabDragAndDropBorder.Alpha() != 0 {
		switch at := f.Index(); {
		case at == spec.index:
			layers = append(layers, line(colors.TabDragAndDropBorder, widgets.PositionedOpts{Left: foundation.Some(0.0), Top: foundation.Some(0.0), Bottom: foundation.Some(0.0), Width: foundation.Some(tabDropBorder)}))
		case at == spec.count && spec.index == spec.count-1:
			layers = append(layers, line(colors.TabDragAndDropBorder, widgets.PositionedOpts{Right: foundation.Some(0.0), Top: foundation.Some(0.0), Bottom: foundation.Some(0.0), Width: foundation.Some(tabDropBorder)}))
		}
	}
	var tab widgets.Widget = widgets.NewContainer(
		widgets.NewStack(layers, widgets.StackOpts{Fit: rendering.StackFitPassthrough}),
		widgets.ContainerOpts{Color: foundation.Some(background)})
	if it.Title != "" {
		tab = NewHover(tab, HoverOpts{Text: it.Title, Compact: true})
	}
	if spec.bounds != nil {
		tab = NewBoundsProbe(tab, spec.bounds)
	}
	return widgets.NewMouseRegion(
		widgets.NewListener(tab, widgets.ListenerOpts{
			Behavior:        rendering.HitTestBehaviorOpaque,
			OnPointerDown:   s.down,
			OnPointerMove:   s.move,
			OnPointerUp:     s.up,
			OnPointerCancel: s.cancelPress,
		}),
		widgets.MouseRegionOpts{
			Cursor:  services.SystemMouseCursors.Basic,
			OnEnter: func(*gestures.PointerEnterEvent) { s.set(func() { s.hover = true }) },
			OnExit:  func(*gestures.PointerExitEvent) { s.set(func() { s.hover = false; s.actionHover = false }) },
		})
}

// action builds the close button, the pin of a sticky tab or the dot of a modified one, and tells
// whether the tab has one. It is dimmed in a tab that is neither active nor under the pointer, and
// then hidden unless the tab is modified or sticky.
//
// A modified tab shows a dot and a sticky one a pin; the "x" shows when the pointer is over the
// action itself, or always in the active tab and the tab under the pointer when the tab is neither
// (VS Code's `.tab-actions .action-label` rules).
func (s *tabState) action(bc widgets.BuildContext, colors Colors, foreground ui.Color, compact, focused bool) (widgets.Widget, bool) {
	spec := s.Widget().spec
	it, o := spec.item, spec.opts
	if compact {
		return nil, false // compact pinned tabs have no actions
	}
	closeable := o.CloseButton != TabCloseOff
	switch {
	case it.Sticky:
		// the action of a sticky tab unpins it; the pin shows (the dirty one for a modified tab)
	case closeable:
	case it.Dirty && !o.HighlightModified:
		// no close button: the dot shows, and does nothing
	default:
		return nil, false
	}
	var code int
	switch {
	case it.Sticky && it.Dirty && !s.actionHover:
		code = fonts.CodiconPinnedDirty
	case it.Sticky:
		code = fonts.CodiconPinned
	case it.Dirty && spec.titleBar && closeable && s.hover:
		// the dot of a workspace tab gives way to the close button while the pointer is over the tab
		code = fonts.CodiconClose
	case it.Dirty && (!s.actionHover || !closeable):
		code = fonts.CodiconCircleFilled
	default:
		code = fonts.CodiconClose
	}
	// visible always for a modified or sticky tab, else in the active tab and the one under the pointer
	visible := it.Dirty || it.Sticky || spec.active || s.hover
	opacity := 1.0
	if !focused {
		opacity = tabActionDimmed
	}
	if !visible {
		opacity = 0
	}
	interactive := closeable || it.Sticky
	var bg ui.Color
	if s.actionHover && interactive {
		bg = colors.ToolbarHoverBackground
	}
	box := widgets.NewContainer(
		widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(foreground)}),
		widgets.ContainerOpts{Width: foundation.Some(tabActionSize), Height: foundation.Some(tabActionSize), Alignment: painting.AlignmentCenter,
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(bg), BorderRadius: painting.NewBorderRadiusCircular(tabActionRadius)})})
	var button widgets.Widget = widgets.NewOpacity(opacity, box)
	if interactive {
		button = widgets.NewMouseRegion(
			widgets.NewListener(
				widgets.NewGestureDetector(button, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() {
					if !(opacity > 0) {
						return
					}
					if it.Sticky {
						if f := o.OnUnpin; f != nil {
							f(spec.index)
						}
					} else if f := o.OnClose; f != nil {
						f(spec.index)
					}
				}}),
				widgets.ListenerOpts{Behavior: rendering.HitTestBehaviorOpaque, OnPointerDown: func(*gestures.PointerDownEvent) { s.pressedOnAction = true }}),
			widgets.MouseRegionOpts{
				Cursor:  services.SystemMouseCursors.Click,
				OnEnter: func(*gestures.PointerEnterEvent) { s.set(func() { s.actionHover = true }) },
				OnExit:  func(*gestures.PointerExitEvent) { s.set(func() { s.actionHover = false }) },
			})
	}
	return widgets.NewSizedBox(widgets.NewCenter(button), widgets.SizedBoxOpts{Width: foundation.Some(tabActionWidth), Height: foundation.Some(TabHeight)}), true
}
