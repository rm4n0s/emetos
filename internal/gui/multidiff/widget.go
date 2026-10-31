package multidiff

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
	"path"

	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Action is a button in the header of a section: an icon with a tooltip that runs a function of the owner (the commands of the file's
// menu, Open File).
type Action struct {
	// Icon is the Codicon (a fonts.Codicon... constant) and Tooltip the hover.
	Icon    int
	Tooltip string
	// Run is called when the button is pressed.
	Run func()
}

// WidgetOpts configure the Widget; every field is optional except Body.
type WidgetOpts struct {
	// Body builds the body of the expanded section i: the diff editor of the file. The widget sizes it to the height the model has for
	// the section; its owner measures the content and reports it with Model.SetBodyHeight. It is called when the section comes near the
	// viewport, and the widget it returns lives as long as the section stays near.
	Body func(bc widgets.BuildContext, i int) widgets.Widget
	// Actions gives the buttons of the header of section i, left of its status letter.
	Actions func(bc widgets.BuildContext, i int) []Action
	// Icons gives the file icon theme for the icon of a header; nil or a nil theme draws no icon.
	Icons func() *theme.FileIconTheme
	// Language gives the language of a file for its icon, as the Explorer and the tabs ask the icon theme with the name, the folder and the
	// language (an icon theme gives a Go file its icon by language); nil for no language.
	Language func(Section) string
	// Message is the text shown in place of the sections while there are none ("There are no changes to show."); empty for nothing.
	Message func() string
	// FocusNode is the node of the keyboard of the editor (headers take its keys); the widget makes its own without one.
	FocusNode *widgets.FocusNode
	// OnFocus is called when the keyboard moves into or out of the editor.
	OnFocus func(has bool)
	// OnToggle is called after a header collapsed or opened its section.
	OnToggle func(i int)
	// Overscan is how far beyond the viewport the sections are built; DefaultOverscan when not positive.
	Overscan float64
}

// Widget is the multi-file diff editor: the sections of a Model in a scrolling column. Only the sections near the viewport are built;
// the others are the numbers of the model.
type Widget struct {
	widgets.StatefulWidget
	model *Model
	opts  WidgetOpts
}

// NewWidget makes the editor over a model.
func NewWidget(model *Model, opts WidgetOpts) *Widget {
	return &Widget{StatefulWidget: widgets.NewStatefulWidgetBase(), model: model, opts: opts}
}

// Model is the layout the widget shows.
func (w *Widget) Model() *Model { return w.model }

// CreateState implements widgets.StatefulWidget.
func (w *Widget) CreateState() widgets.State { return &widgetState{} }

// sectionKey keys the section widgets of the Stack: the state of a body is kept while its section stays near the viewport.
type sectionKey string

type cachedSection struct {
	gen uint64
	w   widgets.Widget
}

type widgetState struct {
	widgets.StateOf[*Widget]
	node     *widgets.FocusNode
	ownNode  bool
	hasFocus bool
	// cache holds the widgets of the sections that were built for a generation of the model, so that a scroll that moves the sections
	// does not build them again.
	cache  map[string]cachedSection
	colors kit.Colors
}

// InitState makes the focus node.
func (s *widgetState) InitState() {
	w := s.Widget()
	s.node = w.opts.FocusNode
	if s.node == nil {
		s.node, s.ownNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Multi-file diff editor"}), true
	}
	s.cache = map[string]cachedSection{}
}

// Dispose frees the focus node when the widget made it.
func (s *widgetState) Dispose() {
	if s.ownNode {
		s.node.Dispose()
	}
}

// Build implements widgets.State.
func (s *widgetState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	colors := kit.ThemeOf(bc)
	if colors != s.colors {
		s.colors = colors
		s.cache = map[string]cachedSection{}
	}
	m := w.model
	content := widgets.NewLayoutBuilder(func(bc widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		m.SetViewport(c.MaxWidth, c.MaxHeight)
		return widgets.NewListenableBuilder(m, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget {
			return s.sections(bc, colors)
		}, nil)
	})
	scroll := kit.NewScrollableElement(widgets.NewClipRect(content), kit.ScrollableElementOpts{
		Model: m.Scroll(), HandleMouseWheel: foundation.Some(true), VerticalSize: foundation.Some(kit.DefaultVerticalScrollbarSize),
	})
	return widgets.NewColoredBox(colors.MultiDiffBackground, widgets.NewFocus(scroll, widgets.FocusOpts{
		FocusNode: s.node, OnKeyEvent: s.handleKey,
		OnFocusChange: func(has bool) {
			s.SetState(func() { s.hasFocus = has })
			if w.opts.OnFocus != nil {
				w.opts.OnFocus(has)
			}
		},
		DebugLabel: "Multi-file diff editor",
	}))
}

// sections builds the sections near the viewport, positioned in it.
func (s *widgetState) sections(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	w := s.Widget()
	m := w.model
	if m.Len() == 0 {
		return s.message(bc, colors)
	}
	overscan := w.opts.Overscan
	if overscan <= 0 {
		overscan = DefaultOverscan
	}
	first, last, ok := m.Visible(overscan)
	if !ok {
		return widgets.NewSizedBox(nil)
	}
	gen, top := m.Generation(), m.ScrollTop()
	live := make(map[string]cachedSection, last-first+1)
	kids := make([]widgets.Widget, 0, last-first+1)
	for i := first; i <= last; i++ {
		key := m.Section(i).Key
		c, ok := s.cache[key]
		if !ok || c.gen != gen {
			c = cachedSection{gen: gen, w: s.section(bc, colors, i)}
		}
		live[key] = c
		kids = append(kids, widgets.NewPositioned(c.w, widgets.PositionedOpts{
			Key: foundation.NewValueKey(sectionKey(key)), Left: foundation.Some(0.0), Right: foundation.Some(0.0),
			Top: foundation.Some(m.Top(i) - top), Height: foundation.Some(m.Height(i)),
		}))
	}
	s.cache = live
	return widgets.NewStack(kids, widgets.StackOpts{Fit: rendering.StackFitExpand, ClipBehavior: ui.ClipHardEdge})
}

// message is the text in the middle of an editor with no sections.
func (s *widgetState) message(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	text := ""
	if s.Widget().opts.Message != nil {
		text = s.Widget().opts.Message()
	}
	if text == "" {
		return widgets.NewSizedBox(nil)
	}
	style := kit.TextStyleOf(bc, 13, colors.EditorForeground.WithOpacity(0.7))
	return widgets.NewCenter(widgets.NewPadding(painting.NewEdgeInsetsAll(24), widgets.NewText(text, widgets.TextOpts{Style: style, TextAlign: ui.TextAlignCenter})))
}

// section is the header of section i and, when it is open, its body.
func (s *widgetState) section(bc widgets.BuildContext, colors kit.Colors, i int) widgets.Widget {
	w := s.Widget()
	m := w.model
	children := []widgets.Widget{s.header(bc, colors, i)}
	if !m.Collapsed(i) && w.opts.Body != nil {
		children = append(children, widgets.NewSizedBox(w.opts.Body(bc, i), widgets.SizedBoxOpts{Height: foundation.Some(m.BodyHeight(i))}))
	}
	return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})
}

const (
	fontSize = 13.0
	gapSmall = 6.0
)

func gap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

func oneLine(text string, style *painting.TextStyle) widgets.Widget {
	return widgets.NewText(text, widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// header is the header of a section: the chevron, the icon of the file, its name and folder, the actions and the letter of its status.
//
// Ported from VS Code: src/vs/editor/browser/widget/multiDiffEditor/diffEditorItemTemplate.ts
func (s *widgetState) header(bc widgets.BuildContext, colors kit.Colors, i int) widgets.Widget {
	w := s.Widget()
	m := w.model
	sec := m.Section(i)
	fg := colors.EditorForeground
	chevron := fonts.CodiconChevronDown
	if m.Collapsed(i) {
		chevron = fonts.CodiconChevronRight
	}
	cells := []widgets.Widget{
		gap(4),
		widgets.NewIcon(fonts.CodiconIcon(chevron), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(fg)}),
		gap(4),
	}
	if w.opts.Icons != nil && sec.Name != "" {
		if th := w.opts.Icons(); th != nil {
			q := theme.FileIconQuery{Kind: theme.FileIconFile, Name: sec.Name, Parent: path.Base(sec.Folder), Type: colors.Type}
			if sec.Folder == "" {
				q.Parent = ""
			}
			if w.opts.Language != nil {
				q.LanguageID = w.opts.Language(sec)
			}
			if ic, ok := th.IconFor(q); ok {
				cells = append(cells, kit.NewFileIcon(ic, fg), gap(gapSmall))
			}
		}
	}
	name := oneLine(sec.Name, kit.TextStyleOf(bc, fontSize, fg).Merge(painting.NewTextStyle(painting.TextStyleOpts{FontWeight: ui.FontWeightW600})))
	label := []widgets.Widget{widgets.NewFlexible(name, widgets.FlexibleOpts{Fit: rendering.FlexFitLoose})}
	if sec.Folder != "" {
		label = append(label, gap(gapSmall), widgets.NewFlexible(oneLine(sec.Folder, kit.TextStyleOf(bc, fontSize*0.9, fg.WithOpacity(0.7))), widgets.FlexibleOpts{Fit: rendering.FlexFitLoose}))
	}
	cells = append(cells, widgets.NewExpanded(widgets.NewRow(label, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})))
	if w.opts.Actions != nil {
		for _, a := range w.opts.Actions(bc, i) {
			run := a.Run
			cells = append(cells, kit.NewIconButton(a.Icon, kit.IconButtonOpts{Tooltip: a.Tooltip, OnPressed: func() {
				if run != nil {
					run()
				}
			}}))
		}
	}
	if sec.Letter != "" {
		color := fg
		if c, ok := colors.Named(sec.Color); ok {
			color = c
		}
		style := kit.TextStyleOf(bc, fontSize*0.9, color).Merge(painting.NewTextStyle(painting.TextStyleOpts{FontWeight: ui.FontWeightW600}))
		badge := widgets.NewSizedBox(widgets.NewCenter(oneLine(sec.Letter, style)), widgets.SizedBoxOpts{Width: foundation.Some(16.0)})
		if sec.Tooltip != "" {
			cells = append(cells, kit.NewHover(badge, kit.HoverOpts{Text: sec.Tooltip, Compact: true}))
		} else {
			cells = append(cells, badge)
		}
	}
	cells = append(cells, gap(8))
	border := painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.MultiDiffBorder), Width: foundation.Some(1.0)})}
	if s.hasFocus && m.Focused() == i {
		border = painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.FocusBorder), Width: foundation.Some(1.0)})
	}
	row := widgets.NewContainer(widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), widgets.ContainerOpts{
		Height:     foundation.Some(HeaderHeight),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(colors.MultiDiffHeaderBackground), Border: border}),
	})
	press := widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{
		Behavior: rendering.HitTestBehaviorOpaque,
		OnTap: func() {
			s.node.RequestFocus()
			m.SetFocused(i)
			m.Toggle(i)
			if w.opts.OnToggle != nil {
				w.opts.OnToggle(i)
			}
		},
	})
	return press
}

// handleKey is the keyboard of the headers: the arrows move between files, Enter and Space open or close the file, Left closes and
// Right opens it, Home and End go to the first and the last file and the page keys scroll.
func (s *widgetState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	if _, ok := e.(*services.KeyDownEvent); !ok {
		return widgets.KeyEventResultIgnored
	}
	w := s.Widget()
	m := w.model
	if m.Len() == 0 {
		return widgets.KeyEventResultIgnored
	}
	at := m.Focused()
	if at < 0 {
		at = m.Current()
	}
	move := func(i int) {
		i = min(max(i, 0), m.Len()-1)
		m.SetFocused(i)
		m.RevealSection(i)
	}
	toggle := func(collapsed bool) {
		if m.Collapsed(at) != collapsed {
			m.SetCollapsed(at, collapsed)
			if w.opts.OnToggle != nil {
				w.opts.OnToggle(at)
			}
		}
	}
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyArrowDown:
		move(at + 1)
	case services.LogicalKeyboardKeyArrowUp:
		move(at - 1)
	case services.LogicalKeyboardKeyHome:
		move(0)
	case services.LogicalKeyboardKeyEnd:
		move(m.Len() - 1)
	case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter, services.LogicalKeyboardKeySpace:
		m.SetFocused(at)
		toggle(!m.Collapsed(at))
	case services.LogicalKeyboardKeyArrowLeft:
		m.SetFocused(at)
		toggle(true)
	case services.LogicalKeyboardKeyArrowRight:
		m.SetFocused(at)
		toggle(false)
	case services.LogicalKeyboardKeyPageDown:
		m.scrollTo(m.ScrollTop() + m.Viewport())
	case services.LogicalKeyboardKeyPageUp:
		m.scrollTo(m.ScrollTop() - m.Viewport())
	default:
		return widgets.KeyEventResultIgnored
	}
	return widgets.KeyEventResultHandled
}
