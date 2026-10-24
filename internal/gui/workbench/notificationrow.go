package workbench

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of a notification (VS Code's notificationsList.css and notificationsViewer.ts).
const (
	notificationWidth      = 450.0
	notificationLineHeight = 22.0
	notificationFontSize   = 13.0
	notificationSourceSize = 12.0
	notificationIconSize   = 18.0
	notificationIconBox    = 24.0 // 4 px of margin, the 16 px box and 4 px of margin
	notificationPadV       = 10.0
	notificationPadH       = 5.0
	notificationDetailsH   = 34.0 // a button and its margins of 4 px
	notificationDetailsGap = 8.0  // what the first row keeps below it when there is a details row
	notificationButtonPadV = 4.0
	notificationButtonPadH = 5.0
)

// notificationRowOpts are the options of a row.
type notificationRowOpts struct {
	// Item is the notification the row shows.
	Item notify.Item
	// OnAction is called with the index of the button that was pressed.
	OnAction func(index int)
	// OnClose is called when the close button is pressed.
	OnClose func()
	// OnToggle is called when the expand or collapse button is pressed.
	OnToggle func()
	// OnHover and OnFocus are told when the pointer enters or leaves the row and when the focus enters
	// or leaves it.
	OnHover, OnFocus func(bool)
	// Bind is given the focus scope of the row, and returns the function that gives it up.
	Bind func(node *widgets.FocusScopeNode) (unbind func())
}

// notificationRow is a notification as VS Code draws it, in a toast and in the center: the severity icon, the
// message (one line with an ellipsis, or all of it when the item is expanded), a toolbar with the expand and
// close buttons (shown while the pointer is over the row, the focus is in it or the item is expanded), and
// for an expanded item a row with its source and its buttons, and along the bottom the progress bar.
//
// Ported from VS Code: src/vs/workbench/browser/parts/notifications/notificationsViewer.ts and
// media/notificationsList.css
type notificationRow struct {
	widgets.StatefulWidget
	opts notificationRowOpts
}

// newNotificationRow makes a row; give it a key made of the id of the item, so that it keeps its state.
func newNotificationRow(opts notificationRowOpts) *notificationRow {
	return &notificationRow{
		StatefulWidget: widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: foundation.NewValueKey(opts.Item.ID)}),
		opts:           opts,
	}
}

// CreateState implements widgets.StatefulWidget.
func (r *notificationRow) CreateState() widgets.State { return &notificationRowState{} }

type notificationRowState struct {
	widgets.StateOf[*notificationRow]
	scope        *widgets.FocusScopeNode
	unbind       func()
	hover, focus bool
}

// InitState makes the focus scope of the row.
func (s *notificationRowState) InitState() {
	s.scope = widgets.NewFocusScopeNode(widgets.FocusScopeNodeOpts{FocusNodeOpts: widgets.FocusNodeOpts{DebugLabel: "Notification"}})
	if bind := s.Widget().opts.Bind; bind != nil {
		s.unbind = bind(s.scope)
	}
}

// Dispose gives up the focus scope.
func (s *notificationRowState) Dispose() {
	if s.unbind != nil {
		s.unbind()
	}
	s.scope.Dispose()
}

// set changes the state of the row when it is still there.
func (s *notificationRowState) set(f func()) {
	if s.Mounted() {
		s.SetState(f)
	}
}

// icon is the Codicon and the color of the icon of a severity.
func notificationIcon(c kit.Colors, severity notify.Severity) (int, ui.Color) {
	switch severity {
	case notify.Error:
		return fonts.CodiconError, c.NotificationsErrorIconForeground
	case notify.Warning:
		return fonts.CodiconWarning, c.NotificationsWarningIconForeground
	}
	return fonts.CodiconInfo, c.NotificationsInfoIconForeground
}

// notificationText is a text style of the notification: the size, the color and a line height.
func notificationText(bc widgets.BuildContext, size float64, color ui.Color, lineHeight float64) *painting.TextStyle {
	return painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: kit.UIFontOf(bc), FontFamilyFallback: []string{"sans-serif"},
		FontSize: foundation.Some(size), Height: foundation.Some(lineHeight / size), Color: foundation.Some(color),
	})
}

// messageOverflows says whether a message needs more than one line in a width.
func messageOverflows(bc widgets.BuildContext, message string, style *painting.TextStyle, width float64) bool {
	engine := widgets.WidgetsBindingOf(bc).PlatformDispatcher().TextEngine()
	if engine == nil || width <= 0 {
		return false
	}
	painter := painting.NewTextPainter(painting.TextPainterOpts{
		TextEngine: engine, Text: painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(message), Style: style}),
		TextDirection: ui.TextDirectionLTR, MaxLines: foundation.Some(1),
	})
	defer painter.Dispose()
	painter.Layout(0, width)
	return painter.DidExceedMaxLines()
}

// Build implements widgets.State.
func (s *notificationRowState) Build(bc widgets.BuildContext) widgets.Widget {
	o := s.Widget().opts
	item := o.Item
	colors := kit.ThemeOf(bc)
	fg := colors.NotificationsForeground
	message := notificationText(bc, notificationFontSize, fg, notificationLineHeight)

	row := widgets.NewLayoutBuilder(func(bc widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		// whether a message that is not expanded needs the button that expands it: it is measured without
		// the toolbar, which is hidden then
		overflows := false
		if item.CanCollapse() && !item.Expanded {
			overflows = messageOverflows(bc, item.Message, message, c.MaxWidth-2*notificationPadH-notificationIconBox)
		}
		showToggle := item.CanCollapse() && (item.Expanded || item.Source != "" || overflows)
		showClose := !item.HasActiveProgress()
		return s.content(bc, colors, message, showToggle, showClose)
	})

	var body widgets.Widget = widgets.NewStack([]widgets.Widget{row, s.progress(item)}, widgets.StackOpts{Fit: rendering.StackFitPassthrough})
	body = widgets.NewMouseRegion(body, widgets.MouseRegionOpts{
		OnEnter: func(*gestures.PointerEnterEvent) {
			s.set(func() { s.hover = true })
			if o.OnHover != nil {
				o.OnHover(true)
			}
		},
		OnExit: func(*gestures.PointerExitEvent) {
			s.set(func() { s.hover = false })
			if o.OnHover != nil {
				o.OnHover(false)
			}
		},
	})
	return widgets.NewFocusScope(body, widgets.FocusScopeOpts{
		Node: s.scope, DebugLabel: "Notification",
		OnFocusChange: func(has bool) {
			s.set(func() { s.focus = has })
			if o.OnFocus != nil {
				o.OnFocus(has)
			}
		},
	})
}

// progress is the bar along the bottom of a notification whose progress is running.
func (s *notificationRowState) progress(item notify.Item) widgets.Widget {
	if !item.HasActiveProgress() {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(0.0), Height: foundation.Some(0.0)})
	}
	p := item.Progress
	return widgets.NewPositioned(
		widgets.NewIgnorePointer(kit.NewProgressBar(kit.ProgressBarOpts{Infinite: p.Infinite, Total: p.Total, Worked: p.Worked})),
		widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0), Bottom: foundation.Some(0.0)})
}

// content is the rows of the notification: the message with its toolbar and, when expanded, the details.
func (s *notificationRowState) content(bc widgets.BuildContext, colors kit.Colors, message *painting.TextStyle, showToggle, showClose bool) widgets.Widget {
	o := s.Widget().opts
	item := o.Item
	code, color := notificationIcon(colors, item.Severity)
	fg := colors.NotificationsForeground

	icon := widgets.NewSizedBox(
		widgets.NewCenter(widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(notificationIconSize), Color: foundation.Some(color)})),
		widgets.SizedBoxOpts{Width: foundation.Some(notificationIconBox), Height: foundation.Some(notificationLineHeight)})

	text := widgets.NewText(item.Message, widgets.TextOpts{Style: message})
	if !item.Expanded {
		text = widgets.NewText(item.Message, widgets.TextOpts{Style: message, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}

	first := []widgets.Widget{icon, widgets.NewExpanded(text)}
	if item.Expanded || s.hover || s.focus {
		var tools []widgets.Widget
		if showToggle {
			code, tip := fonts.CodiconChevronUp, "Expand Notification"
			if item.Expanded {
				code, tip = fonts.CodiconChevronDown, "Collapse Notification"
			}
			tools = append(tools, kit.NewIconButton(code, kit.IconButtonOpts{Tooltip: tip, OnPressed: o.OnToggle, Key: foundation.NewValueKey("toggle")}))
		}
		if showClose {
			tools = append(tools, kit.NewIconButton(fonts.CodiconClose, kit.IconButtonOpts{Tooltip: "Clear Notification", OnPressed: o.OnClose, Key: foundation.NewValueKey("close")}))
		}
		if len(tools) > 0 {
			first = append(first, widgets.NewSizedBox(
				widgets.NewRow(tools, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin}),
				widgets.SizedBoxOpts{Height: foundation.Some(notificationLineHeight)}))
		}
	}
	rows := []widgets.Widget{widgets.NewRow(first, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})}

	if item.Expanded && (item.Source != "" || item.HasActions()) {
		var details []widgets.Widget
		var source widgets.Widget = widgets.NewSizedBox(nil)
		if item.Source != "" {
			source = widgets.NewText("Source: "+item.Source, widgets.TextOpts{
				Style: notificationText(bc, notificationSourceSize, fg, notificationSourceSize*1.4), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
		}
		details = append(details, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(notificationPadH)}), widgets.NewExpanded(source))
		for i, a := range item.Actions {
			details = append(details, widgets.NewPadding(painting.NewEdgeInsetsSymmetric(notificationButtonPadV, notificationButtonPadH),
				kit.NewButton(a.Label, kit.ButtonOpts{Key: foundation.NewValueKey(i), Secondary: i > 0, OnPressed: func() { o.OnAction(i) }})))
		}
		rows = append(rows,
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(notificationDetailsGap)}),
			widgets.NewSizedBox(widgets.NewRow(details, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
				widgets.SizedBoxOpts{Height: foundation.Some(notificationDetailsH)}))
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(notificationPadH, notificationPadV, notificationPadH, notificationPadV),
		widgets.NewColumn(rows, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
}
