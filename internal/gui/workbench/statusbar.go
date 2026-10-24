package workbench

import (
	"context"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of the items of the status bar (VS Code's statusbarpart.css).
const (
	// statusFontSize is the size of the text of an item; an icon is statusIconSize.
	statusFontSize = 12.0
	statusIconSize = 16.0
	// statusItemMargin and statusItemPadding are the room outside and inside the box of an item that
	// has a hover background; the first item on the left, the last on the right and the items with a
	// background of their own have no margin and statusEdgePadding of padding, so that they reach the
	// corner.
	statusItemMargin  = 3.0
	statusItemPadding = 5.0
	statusEdgePadding = 8.0
	// statusEdgeInset is the room before the first item on the left and after the last on the right.
	statusEdgeInset = 2.0
)

// StatusBarOptions say how the status bar shows the items of a StatusBar.
type StatusBarOptions struct {
	// Bar holds the items. Required.
	Bar *StatusBar
	// Keybinding gives the label of the key of a command (such as "Ctrl+G"), which is written after the
	// tooltip of an item that runs it; empty or nil for none.
	Keybinding func(commandID string) string
	// OnError is told when the command of an item fails.
	OnError func(commandID string, err error)
}

// StatusItemKey is the key of the widget of an item of the status bar, which a test finds it by.
func StatusItemKey(id string) foundation.Key {
	return foundation.NewValueKey("workbench:status-item:" + id)
}

// statusItems is the content of the status bar: the items of the left side from the left end, and those
// of the right side to the right end. The left items are cut off when the bar is too narrow; the right
// items keep their room.
type statusItems struct {
	widgets.StatefulWidget
	opts StatusBarOptions
	// commands runs the commands of the items and ctx and keys are what they run with.
	commands *command.Registry
	ctx      context.Context
	keys     contextkey.Context
	// delegate is shared by the hovers of the items, so that moving from one to the next shows its
	// tooltip at once.
	delegate *kit.HoverDelegate
}

func newStatusItems(opts StatusBarOptions, commands *command.Registry, ctx context.Context, keys contextkey.Context, delegate *kit.HoverDelegate) *statusItems {
	return &statusItems{StatefulWidget: widgets.NewStatefulWidgetBase(), opts: opts, commands: commands, ctx: ctx, keys: keys, delegate: delegate}
}

// CreateState implements widgets.StatefulWidget.
func (w *statusItems) CreateState() widgets.State { return &statusItemsState{} }

type statusItemsState struct {
	widgets.StateOf[*statusItems]
	binding *widgets.WidgetsBinding
	remove  func()
}

// InitState follows the bar.
func (s *statusItemsState) InitState() {
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.follow()
}

func (s *statusItemsState) follow() {
	if s.remove != nil {
		s.remove()
	}
	s.remove = s.Widget().opts.Bar.OnChange(func() {
		// the items change from any goroutine; the widgets only on the UI thread
		s.binding.Post(func() {
			if s.Mounted() {
				s.SetState(func() {})
			}
		})
	})
}

// DidUpdateWidget follows another bar.
func (s *statusItemsState) DidUpdateWidget(old *statusItems) {
	if old.opts.Bar != s.Widget().opts.Bar {
		s.follow()
	}
}

// Dispose stops following.
func (s *statusItemsState) Dispose() {
	if s.remove != nil {
		s.remove()
	}
}

// statusSlot is where the layout delegate finds the two sides.
type statusSlot int

const (
	slotLeft statusSlot = iota
	slotRight
)

// Build implements widgets.State.
func (s *statusItemsState) Build(widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	side := func(a StatusAlignment) widgets.Widget {
		items := w.opts.Bar.Items(a)
		var shown []StatusItem
		for _, it := range items {
			if it.Entry.Text != "" || it.Entry.ShowProgress {
				shown = append(shown, it)
			}
		}
		children := make([]widgets.Widget, len(shown))
		for i, it := range shown {
			edge := (a == StatusLeft && i == 0) || (a == StatusRight && i == len(shown)-1)
			children[i] = newStatusItem(w, it, edge)
		}
		inset := statusEdgeInset
		pad := painting.NewEdgeInsetsFromLTRB(inset, 0, 0, 0)
		if a == StatusRight {
			pad = painting.NewEdgeInsetsFromLTRB(0, 0, inset, 0)
		}
		return widgets.NewPadding(pad, widgets.NewRow(children, widgets.RowOpts{
			MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch,
		}))
	}
	// the left side is as wide as the right side leaves it and cuts what does not fit
	left := widgets.NewClipRect(widgets.NewOverflowBox(side(StatusLeft), widgets.OverflowBoxOpts{
		Alignment: painting.AlignmentCenterLeft, MinWidth: foundation.Some(0.0), MaxWidth: foundation.Some(1e9),
		Fit: rendering.OverflowBoxFitDeferToChild,
	}))
	right := widgets.NewOverflowBox(side(StatusRight), widgets.OverflowBoxOpts{
		Alignment: painting.AlignmentCenterRight, MinWidth: foundation.Some(0.0), MaxWidth: foundation.Some(1e9),
		Fit: rendering.OverflowBoxFitDeferToChild,
	})
	return widgets.NewCustomMultiChildLayout(newStatusLayout(), []widgets.Widget{
		widgets.NewLayoutId(slotRight, right),
		widgets.NewLayoutId(slotLeft, left),
	})
}

// statusLayout puts the right side at the right end of the bar, at its natural width, and gives the left
// side what is left.
type statusLayout struct {
	rendering.MultiChildLayoutDelegateBase
}

func newStatusLayout() *statusLayout {
	return &statusLayout{MultiChildLayoutDelegateBase: rendering.NewMultiChildLayoutDelegateBase(nil)}
}

// PerformLayout implements rendering.MultiChildLayoutDelegate.
func (d *statusLayout) PerformLayout(size ui.Size) {
	// the right side takes its natural width, up to the bar's
	rightSize := d.LayoutChild(slotRight, rendering.BoxConstraints{MaxWidth: size.Width, MaxHeight: size.Height})
	d.PositionChild(slotRight, ui.Offset{Dx: size.Width - rightSize.Width})
	d.LayoutChild(slotLeft, rendering.BoxConstraints{MaxWidth: max(size.Width-rightSize.Width, 0), MaxHeight: size.Height})
	d.PositionChild(slotLeft, ui.Offset{})
}

// ShouldRelayout implements rendering.MultiChildLayoutDelegate.
func (d *statusLayout) ShouldRelayout(rendering.MultiChildLayoutDelegate) bool { return false }

// statusItem is one item of the bar: an icon or text in a box that has a background under the pointer, and
// the hover with its tooltip. A click runs its command.
type statusItem struct {
	widgets.StatefulWidget
	bar  *statusItems
	item StatusItem
	// edge tells that the item is the first on the left or the last on the right.
	edge bool
}

func newStatusItem(bar *statusItems, item StatusItem, edge bool) *statusItem {
	return &statusItem{StatefulWidget: widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: StatusItemKey(item.ID)}), bar: bar, item: item, edge: edge}
}

// CreateState implements widgets.StatefulWidget.
func (w *statusItem) CreateState() widgets.State { return &statusItemState{} }

type statusItemState struct {
	widgets.StateOf[*statusItem]
	hover, pressed bool
}

func (s *statusItemState) set(f func()) {
	if s.Mounted() {
		s.SetState(f)
	}
}

// colors are the text and the background of an item in its state: the kind decides, and the pointer
// over a clickable item and a press change the background.
func statusItemColors(colors kit.Colors, kind StatusKind, clickable, hover, pressed bool) (fg, bg ui.Color, hasBg bool) {
	transparent := ui.NewColor(0)
	fg, bg = colors.StatusBarForeground, transparent
	hoverFg, hoverBg := colors.StatusBarItemHoverForeground, colors.StatusBarItemHoverBackground
	switch kind {
	case StatusWarning:
		fg, bg = colors.StatusBarItemWarningForeground, colors.StatusBarItemWarningBackground
		hoverFg, hoverBg = colors.StatusBarItemWarningHoverForeground, colors.StatusBarItemWarningHoverBackground
	case StatusError:
		fg, bg = colors.StatusBarItemErrorForeground, colors.StatusBarItemErrorBackground
		hoverFg, hoverBg = colors.StatusBarItemErrorHoverForeground, colors.StatusBarItemErrorHoverBackground
	case StatusProminent:
		fg, bg = colors.StatusBarItemProminentForeground, colors.StatusBarItemProminentBackground
		hoverFg, hoverBg = colors.StatusBarItemProminentHoverForeground, colors.StatusBarItemProminentHoverBackground
	case StatusStandard:
	}
	hasBg = kind != StatusStandard
	if clickable && (hover || pressed) {
		fg, bg, hasBg = hoverFg, hoverBg, true
		if pressed {
			bg = colors.StatusBarItemActiveBackground
		}
	}
	return fg, bg, hasBg
}

// run runs the command of the item.
func (s *statusItemState) run() {
	w := s.Widget()
	e := w.item.Entry
	if e.Command == "" || w.bar.commands == nil {
		return
	}
	ctx := command.Context{Ctx: w.bar.ctx, Keys: w.bar.keys, Source: command.SourceStatusBar}
	if err := w.bar.commands.Run(ctx, e.Command, e.Args); err != nil && w.bar.opts.OnError != nil {
		w.bar.opts.OnError(e.Command, err)
	}
}

// Build implements widgets.State.
func (s *statusItemState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	e := w.item.Entry
	colors := kit.ThemeOf(bc)
	clickable := e.Command != ""
	fg, bg, hasBg := statusItemColors(colors, e.Kind, clickable, s.hover, s.pressed)
	label := statusLabel(bc, e, fg)
	margin, padding := statusItemMargin, statusItemPadding
	if w.edge || e.Kind != StatusStandard {
		margin, padding = 0, statusEdgePadding
	}
	fill := foundation.None[ui.Color]()
	if hasBg {
		fill = foundation.Some(bg)
	}
	box := widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, margin),
		widgets.NewContainer(label, widgets.ContainerOpts{
			Color: fill, Padding: painting.NewEdgeInsetsSymmetric(0, padding),
			Alignment: painting.AlignmentCenterLeft,
		}))
	var body widgets.Widget
	if clickable {
		body = widgets.NewMouseRegion(
			widgets.NewGestureDetector(box, widgets.GestureDetectorOpts{
				Behavior:    rendering.HitTestBehaviorOpaque,
				OnTapDown:   func(gestures.TapDownDetails) { s.set(func() { s.pressed = true }) },
				OnTapUp:     func(gestures.TapUpDetails) { s.set(func() { s.pressed = false }) },
				OnTapCancel: func() { s.set(func() { s.pressed = false }) },
				OnTap:       s.run,
			}),
			widgets.MouseRegionOpts{
				Cursor:  services.SystemMouseCursors.Click,
				OnEnter: func(*gestures.PointerEnterEvent) { s.set(func() { s.hover = true }) },
				OnExit:  func(*gestures.PointerExitEvent) { s.set(func() { s.hover = false }) },
			})
	} else {
		body = widgets.NewMouseRegion(box, widgets.MouseRegionOpts{
			OnEnter: func(*gestures.PointerEnterEvent) {},
		})
	}
	if tip := statusTooltip(e, w.bar.opts.Keybinding); tip != "" {
		body = kit.NewHover(body, kit.HoverOpts{Text: tip, Compact: true, Position: kit.HoverAbove, Delegate: w.bar.delegate})
	}
	return body
}

// statusTooltip is the hover text of an item: its tooltip and, when its command has a key, the key in
// brackets.
func statusTooltip(e StatusEntry, keybinding func(string) string) string {
	if e.Tooltip == "" {
		return ""
	}
	if e.Command != "" && keybinding != nil {
		if label := keybinding(e.Command); label != "" {
			return e.Tooltip + " (" + label + ")"
		}
	}
	return e.Tooltip
}

// statusLabel is the text and icons of an item, in a row.
func statusLabel(bc widgets.BuildContext, e StatusEntry, fg ui.Color) widgets.Widget {
	text := kit.TextStyleOf(bc, statusFontSize, fg)
	var children []widgets.Widget
	icon := func(code int) widgets.Widget {
		return widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(statusIconSize), Color: foundation.Some(fg)})
	}
	if e.ShowProgress {
		children = append(children, icon(fonts.CodiconLoading))
	}
	for _, run := range ParseStatusText(e.Text) {
		switch run.Icon {
		case "":
			children = append(children, widgets.NewText(run.Text, widgets.TextOpts{Style: text, MaxLines: foundation.Some(1), SoftWrap: foundation.Some(false)}))
		default:
			code, ok := fonts.CodiconByName(run.Icon)
			if !ok {
				// an icon that does not exist is shown as the text it was written as
				children = append(children, widgets.NewText("$("+run.Icon+")", widgets.TextOpts{Style: text, MaxLines: foundation.Some(1)}))
				continue
			}
			children = append(children, icon(code))
		}
	}
	return widgets.NewRow(children, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}
