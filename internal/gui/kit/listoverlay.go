package kit

import (
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of VS Code's lists in a picker.
const (
	listRowHeight   = 22.0
	listFontSize    = 13.0
	listIconGap     = 4.0
	listPadding     = 6.0
	listDefaultRows = 10
	listDefaultWide = 300.0
	listShadowBlur  = 8.0
)

// ListOverlayOpts are the options of NewListOverlay.
type ListOverlayOpts struct {
	// Key is the key of the widget: a list with a new key starts again from its items.
	Key foundation.Key
	// TapGroup, when set, is the group of tap regions (widgets.TapRegionOpts.GroupID) the list
	// belongs to: a click in another region of the group is not a click outside the list, so the
	// widget that opened the list can handle it.
	TapGroup any
	// Filterable lets the user type to filter the items by label: the typed text shows above the
	// rows and Backspace removes the last character.
	Filterable bool
	// Rows is the number of rows in view; the default is 10. More items scroll.
	Rows int
	// Width is the width of the list; the default is 300.
	Width foundation.Opt[float64]
	// Focused is the item that has the focus at first; the default is the first row that is not
	// disabled.
	Focused foundation.Opt[int]
	// Background is the background of the list; the default is the color of editor widgets.
	Background foundation.Opt[ui.Color]
	// StealFocus takes the focus when the list is shown even if another widget has it (the
	// autofocus of a list gives way to the widget that has the focus), as the list of the choices
	// of a snippet does, which opens over an editor that has it.
	StealFocus bool
	// OnSelect is called with the index of the item the user chose (Enter or a click).
	OnSelect func(item int)
	// OnDismiss is called when the user presses Escape, clicks outside the list or moves the focus
	// away from it.
	OnDismiss func()
}

// ListOverlay is a list of rows with a keyboard: a small popup that a widget places where it wants
// it (below a breadcrumbs segment, at a snippet's choices), before the quick input exists. It
// takes the focus when it is shown. Up and Down, PageUp and PageDown, Home and End move the
// focus, Enter chooses the focused row, Escape dismisses the list and, when it is Filterable,
// typing filters the rows.
type ListOverlay struct {
	widgets.StatefulWidget
	items []ListItem
	opts  ListOverlayOpts
}

// NewListOverlay makes a list of items.
func NewListOverlay(items []ListItem, opts ...ListOverlayOpts) *ListOverlay {
	l := &ListOverlay{items: items}
	if len(opts) > 0 {
		l.opts = opts[0]
	}
	l.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: l.opts.Key})
	return l
}

// CreateState implements widgets.StatefulWidget.
func (l *ListOverlay) CreateState() widgets.State { return &listOverlayState{} }

type listOverlayState struct {
	widgets.StateOf[*ListOverlay]
	model *ListModel
	// hover is the item under the pointer, or -1.
	hover int
	node  *widgets.FocusNode
	// dismissed stops a second OnDismiss when Escape also moves the focus.
	dismissed bool
}

// InitState makes the model and the focus node.
func (s *listOverlayState) InitState() {
	l := s.Widget()
	rows := l.opts.Rows
	if rows <= 0 {
		rows = listDefaultRows
	}
	s.model = NewListModel(l.items, rows)
	if first, ok := l.opts.Focused.Get(); ok {
		s.model.FocusItem(first)
		// show the focused row in the middle of the rows, as far as the list allows
		s.model.Scroll(s.model.FocusedRow() - rows/2 - s.model.Top())
	}
	s.hover = -1
	s.node = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "ListOverlay"})
	if l.opts.StealFocus {
		scheduler := widgets.WidgetsBindingOf(s.Context()).Scheduler()
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(time.Duration) {
			if s.Mounted() {
				s.node.RequestFocus()
			}
		})
	}
}

// Dispose releases the focus node.
func (s *listOverlayState) Dispose() { s.node.Dispose() }

// Model is the state of the list, for tests.
func (s *listOverlayState) Model() *ListModel { return s.model }

func (s *listOverlayState) dismiss() {
	if s.dismissed {
		return
	}
	s.dismissed = true
	if f := s.Widget().opts.OnDismiss; f != nil {
		f()
	}
}

func (s *listOverlayState) choose(item int) {
	if item < 0 || item >= len(s.model.Items()) || s.model.Items()[item].Disabled {
		return
	}
	if f := s.Widget().opts.OnSelect; f != nil {
		f(item)
	}
}

// handleKey is the keyboard of the list.
func (s *listOverlayState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	m := s.model
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyArrowDown:
		m.Move(1)
	case services.LogicalKeyboardKeyArrowUp:
		m.Move(-1)
	case services.LogicalKeyboardKeyPageDown:
		m.Page(1)
	case services.LogicalKeyboardKeyPageUp:
		m.Page(-1)
	case services.LogicalKeyboardKeyHome:
		m.First()
	case services.LogicalKeyboardKeyEnd:
		m.Last()
	case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter:
		s.choose(m.Focused())
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyEscape:
		s.dismiss()
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyBackspace:
		if !s.Widget().opts.Filterable || m.Filter() == "" {
			return widgets.KeyEventResultIgnored
		}
		runes := []rune(m.Filter())
		m.SetFilter(string(runes[:len(runes)-1]))
	default:
		ch := e.Character()
		if !s.Widget().opts.Filterable || ch == "" || kb.IsControlPressed() || kb.IsAltPressed() || kb.IsMetaPressed() || strings.ContainsAny(ch, "\r\n\t\x1b") {
			return widgets.KeyEventResultIgnored
		}
		m.SetFilter(m.Filter() + ch)
	}
	s.SetState(func() {})
	return widgets.KeyEventResultHandled
}

func (s *listOverlayState) setHover(item int) {
	if s.hover != item && s.Mounted() {
		s.SetState(func() { s.hover = item })
	}
}

func (s *listOverlayState) wheel(e gestures.PointerSignalEvent) {
	scroll, ok := e.(*gestures.PointerScrollEvent)
	if !ok {
		return
	}
	rows := 0
	switch dy := scroll.ScrollDelta().Dy; {
	case dy > 0:
		rows = 1 + int(dy/(listRowHeight*2.5))
	case dy < 0:
		rows = -1 + int(dy/(listRowHeight*2.5))
	}
	s.model.Scroll(rows)
	s.SetState(func() {})
}

// Build implements widgets.State.
func (s *listOverlayState) Build(bc widgets.BuildContext) widgets.Widget {
	l := s.Widget()
	colors := ThemeOf(bc)
	m := s.model
	var children []widgets.Widget
	if l.opts.Filterable && m.Filter() != "" {
		children = append(children, s.row(bc, colors, listFilterRow(m.Filter()), colors.InputForeground, ui.Color{}, false))
	}
	if len(m.Shown()) == 0 {
		children = append(children, s.row(bc, colors, ListItem{Label: "No results", Disabled: true}, colors.EditorWidgetForeground.WithAlpha(0x99), ui.Color{}, false))
	}
	for pos, item := range m.InView() {
		focused := m.Top()+pos == m.FocusedRow()
		fg := colors.EditorWidgetForeground
		var bg ui.Color
		switch {
		case focused:
			fg, bg = colors.ListActiveSelectionForeground, colors.ListActiveSelectionBackground
		case s.hover == item && !m.Items()[item].Disabled:
			bg = colors.ListHoverBackground
		}
		children = append(children, s.itemRow(bc, colors, item, fg, bg, focused))
	}
	width := l.opts.Width.OrElse(listDefaultWide)
	background := l.opts.Background.OrElse(colors.EditorWidgetBackground)
	box := widgets.NewContainer(
		widgets.NewColumn(children, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin}),
		widgets.ContainerOpts{
			Width: foundation.Some(width),
			Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color:        foundation.Some(background),
				Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)}),
				BorderRadius: painting.NewBorderRadiusCircular(5),
				BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: listShadowBlur, SpreadRadius: 2})},
			}),
		})
	return widgets.NewTapRegion(
		widgets.NewFocus(
			widgets.NewListener(box, widgets.ListenerOpts{OnPointerSignal: s.wheel}),
			widgets.FocusOpts{FocusNode: s.node, Autofocus: true, OnKeyEvent: s.handleKey, OnFocusChange: func(has bool) {
				if !has {
					s.dismiss()
				}
			}}),
		widgets.TapRegionOpts{GroupID: l.opts.TapGroup, OnTapOutside: func(*gestures.PointerDownEvent) { s.dismiss() }})
}

// listFilterRow is the row that shows the typed filter.
func listFilterRow(text string) ListItem { return ListItem{Label: text, Icon: fonts.CodiconSearch} }

// itemRow is the row of an item, with the pointer handling.
func (s *listOverlayState) itemRow(bc widgets.BuildContext, colors Colors, item int, fg, bg ui.Color, focused bool) widgets.Widget {
	it := s.model.Items()[item]
	row := s.row(bc, colors, it, fg, bg, focused)
	if it.Disabled {
		return row
	}
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(row, widgets.GestureDetectorOpts{OnTap: func() { s.choose(item) }}),
		widgets.MouseRegionOpts{
			Cursor:  services.SystemMouseCursors.Click,
			OnEnter: func(*gestures.PointerEnterEvent) { s.setHover(item) },
			OnExit:  func(*gestures.PointerExitEvent) { s.setHover(-1) },
		})
}

// row is one row: icon, label and description on a background.
func (s *listOverlayState) row(bc widgets.BuildContext, colors Colors, it ListItem, fg, bg ui.Color, focused bool) widgets.Widget {
	if it.Disabled {
		fg = fg.WithAlpha(0x66)
	}
	var parts []widgets.Widget
	if it.Icon != 0 {
		parts = append(parts,
			widgets.NewIcon(fonts.CodiconIcon(it.Icon), widgets.IconOpts{Size: foundation.Some(iconSize), Color: foundation.Some(fg)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(listIconGap)}))
	}
	label := widgets.NewText(it.Label, widgets.TextOpts{Style: TextStyleOf(bc, listFontSize, fg), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	if it.Description == "" {
		parts = append(parts, widgets.NewFlexible(label))
	} else {
		parts = append(parts, widgets.NewFlexible(label),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
			widgets.NewText(it.Description, widgets.TextOpts{Style: TextStyleOf(bc, listFontSize-1, fg.WithAlpha(0x99)), MaxLines: foundation.Some(1)}))
	}
	// the outline of the focused row takes room in every row, so that the text does not move
	outline := ui.Color{}
	if focused {
		outline = colors.ListFocusOutline
	}
	deco := painting.NewBoxDecoration(painting.BoxDecorationOpts{
		Color:  foundation.Some(bg),
		Border: painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(outline), Width: foundation.Some(1.0)}),
	})
	return widgets.NewContainer(
		widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, listPadding), widgets.NewRow(parts, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
		widgets.ContainerOpts{Height: foundation.Some(listRowHeight), Decoration: deco})
}
