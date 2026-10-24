package workbench

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of the toasts and of the notification center (VS Code's notificationsToasts.css and
// notificationsCenter.css).
const (
	// toastsRight and toastsBottom are where the stack of toasts is from the window, and toastMargin the
	// room around each toast; centerRight and centerBottom are the same for the center (as far from the
	// window as a toast is, 3 px and 4 px).
	toastsRight, toastsBottom     = 3.0, 3.0
	toastMargin                   = 4.0
	centerRight, centerBottom     = 7.0, 7.0
	centerHeaderHeight            = 35.0
	centerMaxListHeight           = 400.0
	centerMinWindowPad            = 12.0 // the room VS Code keeps above and below
	notificationRadius            = 4.0
	notificationRowHeight         = 42.0 // a row that is not expanded
	notificationShadowBlur        = 8.0
	notificationWindowPad         = 16.0 // 8 px on each side
	notificationCenterHeaderFont  = 11.0
	notificationCenterHeaderLeft  = 8.0
	notificationCenterHeaderRight = 5.0
)

// NotificationsOptions are the options of NotificationsHost.
type NotificationsOptions struct {
	// Presenter is what the host shows: the toasts and the center of its service. Required.
	Presenter *notify.Presenter
	// Commands is the registry whose notifications.* commands the host takes over while it is on
	// screen (command.DefaultRegistry has them) and that runs the command of a button. Without it no
	// command is bound and a button runs only its Run.
	Commands *command.Registry
	// Keys is the scope of the context keys of the window, where the host sets notificationCenterVisible,
	// notificationToastsVisible and notificationFocus.
	Keys *contextkey.Scope
	// Context is the context of the commands the buttons run; context.Background() when nil.
	Context context.Context
	// BottomInset is the height of what is below the toasts, the status bar.
	BottomInset float64
	// TopInset is the height of what is above them, the menu bar, which the center keeps clear.
	TopInset float64
}

// NotificationsHost puts the toasts and the notification center of a Presenter over its child, which the
// workbench places once at its root (as kit.DialogHost does with dialogs, but below them). The toasts stack at
// the bottom right above the status bar, the newest on top, at most notify.MaxToasts of them, 450 px wide at
// most; the center is a panel at the same place with a header and the list of all the notifications. The
// host binds the presenter to the UI thread, follows it, and takes over the notifications.* commands.
//
// Ported from VS Code: src/vs/workbench/browser/parts/notifications/notificationsToasts.ts,
// notificationsCenter.ts, notificationsList.ts and notificationsCommands.ts
type NotificationsHost struct {
	widgets.StatefulWidget
	child widgets.Widget
	opts  NotificationsOptions
}

// NewNotificationsHost makes the host of the notifications of a presenter over a child.
func NewNotificationsHost(child widgets.Widget, opts NotificationsOptions) *NotificationsHost {
	return &NotificationsHost{StatefulWidget: widgets.NewStatefulWidgetBase(), child: child, opts: opts}
}

// CreateState implements widgets.StatefulWidget.
func (h *NotificationsHost) CreateState() widgets.State { return &notificationsHostState{} }

type notificationsHostState struct {
	widgets.StateOf[*NotificationsHost]
	presenter *notify.Presenter
	binding   *widgets.WidgetsBinding
	// unbind stops the presenter following the service; removeChange stops the host following the
	// presenter, and removeCommands gives the commands back.
	unbind         func()
	removeChange   func()
	removeCommands []func()
	// scopes are the focus scopes of the toasts that are on screen, by the id of the item, and center is
	// the one of the center.
	scopes map[uint64]*widgets.FocusScopeNode
	center *widgets.FocusScopeNode
	// focused are the rows that have the focus: the key notificationFocus is on while there is one.
	focused map[uint64]bool
	// restore is the node that had the focus when the center opened.
	restore  *widgets.FocusNode
	wasShown bool
	scroll   *widgets.ScrollControllerBase
	// listCtx is the context of the rows of the center, whose height the list takes; listHeight is what
	// it measured last.
	listCtx    widgets.BuildContext
	listHeight float64
}

// centerFocusID is the key the center has in focused, apart from the ids of the toasts (which start at 1).
const centerFocusID = 0

// InitState binds the presenter to the UI thread and follows it.
func (s *notificationsHostState) InitState() {
	o := s.Widget().opts
	s.presenter = o.Presenter
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.scopes = map[uint64]*widgets.FocusScopeNode{}
	s.focused = map[uint64]bool{}
	s.scroll = widgets.NewScrollController()
	s.center = widgets.NewFocusScopeNode(widgets.FocusScopeNodeOpts{FocusNodeOpts: widgets.FocusNodeOpts{DebugLabel: "NotificationCenter"}})
	after := func(d time.Duration, f func()) (stop func()) { return s.binding.Gestures().NewTimer(d, f).Cancel }
	s.unbind = s.presenter.Bind(s.binding.Post, after)
	s.removeChange = s.presenter.OnChange(s.changed)
	s.bindCommands()
	s.syncKeys()
}

// Dispose stops following and gives the commands back.
func (s *notificationsHostState) Dispose() {
	for _, f := range s.removeCommands {
		f()
	}
	if s.removeChange != nil {
		s.removeChange()
	}
	if s.unbind != nil {
		s.unbind()
	}
	if keys := s.Widget().opts.Keys; keys != nil {
		keys.Batch(func() {
			keys.Set("notificationCenterVisible", false)
			keys.Set("notificationToastsVisible", false)
			keys.Set("notificationFocus", false)
		})
	}
	s.center.Dispose()
	s.scroll.Dispose()
}

// changed follows the presenter: the context keys, the focus of the center, and a new build.
func (s *notificationsHostState) changed() {
	if !s.Mounted() {
		return
	}
	s.syncKeys()
	shown := s.presenter.CenterVisible()
	if shown != s.wasShown {
		s.wasShown = shown
		s.listHeight = 0
		s.centerVisibilityChanged(shown)
	}
	s.SetState(func() {})
}

// centerVisibilityChanged gives the center the focus when it opens, and gives the focus back to what had it
// when it closes while the center has it.
func (s *notificationsHostState) centerVisibilityChanged(shown bool) {
	scheduler := s.binding.Scheduler()
	if shown {
		s.restore = s.binding.FocusManager().PrimaryFocus()
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(time.Duration) {
			if s.Mounted() && s.presenter.CenterVisible() {
				s.center.RequestFocus()
			}
		})
		return
	}
	node := s.restore
	s.restore = nil
	if s.center.HasFocus() && node != nil && node.Parent() != nil {
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(time.Duration) {
			if node.Parent() != nil {
				node.RequestFocus()
			}
		})
	}
}

// syncKeys sets the context keys.
func (s *notificationsHostState) syncKeys() {
	keys := s.Widget().opts.Keys
	if keys == nil {
		return
	}
	keys.Batch(func() {
		keys.Set("notificationCenterVisible", s.presenter.CenterVisible())
		keys.Set("notificationToastsVisible", s.presenter.ToastsVisible())
		keys.Set("notificationFocus", len(s.focused) > 0)
	})
}

// focusChanged records that the focus entered or left a row (a toast, or the center with id 0).
func (s *notificationsHostState) focusChanged(id uint64, has bool) {
	if has {
		s.focused[id] = true
	} else {
		delete(s.focused, id)
	}
	if id != centerFocusID {
		s.presenter.SetFocused(id, has)
	}
	if keys := s.Widget().opts.Keys; keys != nil {
		keys.Set("notificationFocus", len(s.focused) > 0)
	}
}

// bindCommands takes over the notification commands in the registry while the host is on screen. The
// registry must have them (command.DefaultRegistry does): a missing one is a mistake of the caller and
// panics with the tag NotificationCommandMissing.
//
// Ported from VS Code: src/vs/workbench/browser/parts/notifications/notificationsCommands.ts
func (s *notificationsHostState) bindCommands() {
	reg := s.Widget().opts.Commands
	if reg == nil {
		return
	}
	p := s.presenter
	handlers := map[string]command.Handler{
		"notifications.showList":               func(command.Context, any) error { p.ShowCenter(); return nil },
		"notifications.hideList":               func(command.Context, any) error { p.HideCenter(); return nil },
		"notifications.toggleList":             func(command.Context, any) error { p.ToggleCenter(); return nil },
		"notifications.hideToasts":             func(command.Context, any) error { p.HideToasts(); return nil },
		"notifications.clearAll":               func(command.Context, any) error { p.ClearAll(); return nil },
		"notifications.toggleDoNotDisturbMode": func(command.Context, any) error { p.ToggleDoNotDisturb(); return nil },
		"notifications.focusToasts":            func(command.Context, any) error { s.focusToasts(); return nil },
	}
	for id, h := range handlers {
		remove, err := reg.SetHandler(id, h)
		if err != nil {
			panic(errors.NewErr("NotificationCommandMissing", err).SetMetadata("command", id))
		}
		s.removeCommands = append(s.removeCommands, remove)
	}
}

// focusToasts gives the focus to the newest toast (notifications.focusToasts).
func (s *notificationsHostState) focusToasts() {
	if toasts := s.presenter.Toasts(); len(toasts) > 0 {
		if node := s.scopes[toasts[0].ID]; node != nil {
			node.RequestFocus()
		}
	}
}

// runAction runs a button of a notification: its Run, then its command, and then the notification is
// closed unless the action keeps it open.
func (s *notificationsHostState) runAction(item notify.Item, index int) {
	o := s.Widget().opts
	if index < 0 || index >= len(item.Actions) {
		return
	}
	a := item.Actions[index]
	if a.Run != nil {
		a.Run()
	}
	if a.Command != "" && o.Commands != nil {
		ctx := o.Context
		if ctx == nil {
			ctx = context.Background()
		}
		if err := o.Commands.Run(command.Context{Ctx: ctx, Keys: o.Keys, Source: command.SourceAPI}, a.Command, a.Args); err != nil {
			msg := err.Error()
			if appErr, ok := errors.FromError(err); ok {
				msg = appErr.Message
			}
			s.presenter.Service().Error("Emetos", "Cannot run "+a.Command+": "+msg)
		}
	}
	if !a.KeepOpen {
		s.presenter.Service().Dismiss(item.ID)
	}
}

// row makes the row of an item.
func (s *notificationsHostState) row(item notify.Item, toast bool) widgets.Widget {
	svc := s.presenter.Service()
	id := item.ID
	opts := notificationRowOpts{
		Item:     item,
		OnAction: func(i int) { s.runAction(item, i) },
		OnClose:  func() { svc.Dismiss(id) },
		OnToggle: func() {
			if cur, ok := svc.Get(id); ok {
				svc.SetExpanded(id, !cur.Expanded)
			}
		},
	}
	if toast {
		opts.OnHover = func(over bool) { s.presenter.SetHovered(id, over) }
		opts.OnFocus = func(has bool) { s.focusChanged(id, has) }
		opts.Bind = func(node *widgets.FocusScopeNode) func() {
			s.scopes[id] = node
			return func() {
				if s.scopes[id] == node {
					delete(s.scopes, id)
				}
				delete(s.focused, id)
			}
		}
	}
	return newNotificationRow(opts)
}

// Build implements widgets.State.
func (s *notificationsHostState) Build(bc widgets.BuildContext) widgets.Widget {
	h := s.Widget()
	colors := kit.ThemeOf(bc)
	overlay := widgets.NewLayoutBuilder(func(_ widgets.BuildContext, box rendering.BoxConstraints) widgets.Widget {
		width := min(notificationWidth, box.MaxWidth-notificationWindowPad)
		if s.presenter.CenterVisible() {
			return s.centerLayer(colors, box, width)
		}
		return s.toastsLayer(colors, width)
	})
	return widgets.NewStack([]widgets.Widget{
		h.child,
		widgets.NewPositioned(overlay, widgets.PositionedOpts{
			Left: foundation.Some(0.0), Right: foundation.Some(0.0), Top: foundation.Some(0.0), Bottom: foundation.Some(0.0)}),
	}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// panelDecoration is the look of a toast and of the center: the background, the border and the shadow.
func panelDecoration(bg, border, shadow ui.Color) painting.Decoration {
	opts := painting.BoxDecorationOpts{
		Color:        foundation.Some(bg),
		BorderRadius: painting.NewBorderRadiusCircular(notificationRadius),
		BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(shadow), BlurRadius: notificationShadowBlur})},
	}
	if border.Alpha() != 0 {
		opts.Border = painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)})
	}
	return painting.NewBoxDecoration(opts)
}

// toastsLayer is the stack of toasts at the bottom right, the newest on top.
func (s *notificationsHostState) toastsLayer(colors kit.Colors, width float64) widgets.Widget {
	toasts := s.presenter.Toasts()
	if len(toasts) == 0 {
		return widgets.NewSizedBox(nil)
	}
	column := make([]widgets.Widget, 0, len(toasts))
	for _, item := range toasts {
		box := widgets.NewContainer(widgets.NewDefaultTextStyle(painting.NewTextStyle(painting.TextStyleOpts{Color: foundation.Some(colors.NotificationsForeground)}), s.row(item, true)),
			widgets.ContainerOpts{Decoration: panelDecoration(colors.NotificationsBackground, colors.NotificationToastBorder, colors.WidgetShadow)})
		column = append(column, widgets.NewKeyedSubtree(
			widgets.NewPadding(painting.NewEdgeInsetsAll(toastMargin), widgets.NewSizedBox(box, widgets.SizedBoxOpts{Width: foundation.Some(width)})),
			widgets.WidgetOpts{Key: foundation.NewValueKey(item.ID)}))
	}
	return widgets.NewAlign(
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, toastsRight, s.Widget().opts.BottomInset+toastsBottom),
			widgets.NewColumn(column, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})),
		widgets.AlignOpts{Alignment: painting.AlignmentBottomRight})
}

// measureList takes the height of the rows of the center, and builds again when it is not the height the
// list has.
func (s *notificationsHostState) measureList(time.Duration) {
	if !s.Mounted() || s.listCtx == nil || !s.presenter.CenterVisible() {
		return
	}
	box, ok := s.listCtx.FindRenderObject().(rendering.Box)
	if !ok || !box.HasSize() {
		return
	}
	if h := box.Size().Height; math.Abs(h-s.listHeight) > 0.5 {
		s.SetState(func() { s.listHeight = h })
	}
}

// The titles of the center: the header says so when there is nothing in it.
const (
	centerTitle      = "Notifications"
	centerEmptyTitle = "No new notifications"
)

// centerLayer is the notification center at the bottom right: a header with its buttons over the list of the
// notifications, the newest first.
func (s *notificationsHostState) centerLayer(colors kit.Colors, box rendering.BoxConstraints, width float64) widgets.Widget {
	o := s.Widget().opts
	svc := s.presenter.Service()
	items := svc.Items()
	clearable := false
	for _, it := range items {
		if !it.HasActiveProgress() {
			clearable = true
		}
	}
	title := centerTitle
	if len(items) == 0 {
		title = centerEmptyTitle
	}
	headerFg := colors.NotificationCenterHeaderForeground
	if headerFg.Alpha() == 0 {
		headerFg = colors.Foreground
	}
	dnd := svc.DoNotDisturb()
	dndTip := "Enable Do Not Disturb Mode"
	if dnd {
		dndTip = "Disable Do Not Disturb Mode"
	}
	header := widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		var clearAll func()
		if clearable {
			clearAll = s.presenter.ClearAll
		}
		return widgets.NewSizedBox(widgets.NewColoredBox(colors.NotificationCenterHeaderBackground,
			widgets.NewRow([]widgets.Widget{
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(notificationCenterHeaderLeft)}),
				widgets.NewExpanded(widgets.NewText(strings.ToUpper(title), widgets.TextOpts{
					Style: kit.TextStyleOf(bc, notificationCenterHeaderFont, headerFg), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
				kit.NewIconButton(fonts.CodiconClearAll, kit.IconButtonOpts{Tooltip: "Clear All Notifications", OnPressed: clearAll, Key: foundation.NewValueKey("clearAll")}),
				kit.NewIconButton(fonts.CodiconBellSlash, kit.IconButtonOpts{Tooltip: dndTip, Checked: dnd, OnPressed: s.presenter.ToggleDoNotDisturb, Key: foundation.NewValueKey("dnd")}),
				kit.NewIconButton(fonts.CodiconChevronDown, kit.IconButtonOpts{Tooltip: "Hide Notification Center", OnPressed: s.presenter.HideCenter, Key: foundation.NewValueKey("hide")}),
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(notificationCenterHeaderRight)}),
			}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})),
			widgets.SizedBoxOpts{Height: foundation.Some(centerHeaderHeight)})
	})

	body := []widgets.Widget{header}
	if len(items) > 0 {
		rows := make([]widgets.Widget, 0, 2*len(items))
		for i := len(items) - 1; i >= 0; i-- {
			if len(rows) > 0 {
				rows = append(rows, widgets.NewSizedBox(widgets.NewColoredBox(colors.NotificationsBorder, widgets.NewSizedBox(nil)),
					widgets.SizedBoxOpts{Height: foundation.Some(1.0)}))
			}
			rows = append(rows, s.row(items[i], false))
		}
		list := widgets.NewColoredBox(colors.NotificationsBackground,
			widgets.NewDefaultTextStyle(painting.NewTextStyle(painting.TextStyleOpts{Color: foundation.Some(colors.NotificationsForeground)}),
				widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
					s.listCtx = bc
					return widgets.NewColumn(rows, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
				})))
		// The scrollable element fills the room it is given, so the list gets the height of its rows (as
		// measured after the last frame; the first frame guesses 42 px for a row, VS Code's), up to the
		// most the center may take.
		maxList := max(min(centerMaxListHeight, box.MaxHeight-centerHeaderHeight-o.BottomInset-o.TopInset-2*centerMinWindowPad), 0)
		height := s.listHeight
		if height == 0 {
			height = float64(len(items)) * notificationRowHeight
		}
		body = append(body, widgets.NewSizedBox(
			kit.NewScrollableElement(
				widgets.NewSingleChildScrollView(list, widgets.SingleChildScrollViewOpts{Controller: s.scroll, Primary: foundation.Some(false)}),
				kit.ScrollableElementOpts{Controller: s.scroll, AlwaysConsumeMouseWheel: true}),
			widgets.SizedBoxOpts{Height: foundation.Some(min(height, maxList))}))
		s.binding.Scheduler().AddPostFrameCallback(s.measureList)
	}
	panel := widgets.NewContainer(
		widgets.NewClipRRect(
			widgets.NewColumn(body, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
			widgets.ClipRRectOpts{BorderRadius: painting.NewBorderRadiusCircular(notificationRadius)}),
		widgets.ContainerOpts{Decoration: panelDecoration(colors.NotificationsBackground, colors.NotificationCenterBorder, colors.WidgetShadow)})
	focused := widgets.NewFocusScope(widgets.NewSizedBox(panel, widgets.SizedBoxOpts{Width: foundation.Some(width)}), widgets.FocusScopeOpts{
		Node: s.center, DebugLabel: "NotificationCenter", OnFocusChange: func(has bool) { s.focusChanged(centerFocusID, has) },
	})
	return widgets.NewAlign(
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, centerRight, o.BottomInset+centerBottom), focused),
		widgets.AlignOpts{Alignment: painting.AlignmentBottomRight})
}
