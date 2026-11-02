package debug

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// ToolbarKey is the key of the floating toolbar, and ToolbarGripKey that of its gripper.
var (
	ToolbarKey     = foundation.NewValueKey("debug:toolbar")
	ToolbarGripKey = foundation.NewValueKey("debug:toolbar-grip")
)

// toolbarIconColors are the colors of the icons of the toolbar's buttons: VS Code colors them with CSS rules on the icons.
var toolbarIconColors = map[string]string{
	command.DebugContinueCommand:   "debugIcon.continueForeground",
	command.DebugPauseCommand:      "debugIcon.pauseForeground",
	command.DebugStopCommand:       "debugIcon.stopForeground",
	command.DebugDisconnectCommand: "debugIcon.disconnectForeground",
	command.DebugDisconnectSuspend: "debugIcon.disconnectForeground",
	command.DebugRestartCommand:    "debugIcon.restartForeground",
	command.DebugStepOverCommand:   "debugIcon.stepOverForeground",
	command.DebugStepIntoCommand:   "debugIcon.stepIntoForeground",
	command.DebugStepOutCommand:    "debugIcon.stepOutForeground",
	command.DebugStepBackCommand:   "debugIcon.stepBackForeground",
	command.DebugReverseContinue:   "debugIcon.stepBackForeground",
	command.DebugStartCommand:      "debugIcon.startForeground",
	command.DebugRestartFrame:      "debugIcon.restartForeground",
}

// IconColor is the color of the icon of a debug command (debugIcon.<name>Foreground); none for a command VS Code leaves in the
// normal icon color.
func IconColor(colors kit.Colors, commandID string) foundation.Opt[ui.Color] {
	id, ok := toolbarIconColors[commandID]
	if !ok {
		return foundation.Opt[ui.Color]{}
	}
	if c, ok := colors.Named(id); ok && c.Alpha() != 0 {
		return foundation.Some(c)
	}
	return foundation.Opt[ui.Color]{}
}

// ToolbarWidth is about how wide a toolbar of n buttons is (the gripper and 22 px for each button, with the margins).
func ToolbarWidth(n int) float64 { return 18 + float64(n)*22 + 8 }

// ToolbarHeight is the height of the floating toolbar.
const ToolbarHeight = 32.0

// DefaultToolbarX is the position of the center of the floating toolbar across the editor area (0.5, the middle).
const DefaultToolbarX = 0.5

// FloatingToolbarOpts are the options of NewFloatingToolbar.
type FloatingToolbarOpts struct {
	// Actions are the buttons, with their icons and colors.
	Actions []kit.Action
	// OnRun runs a button.
	OnRun func(a kit.Action)
	// X is where the center of the toolbar is across the width of the editor area, from 0 to 1; DefaultToolbarX when 0.
	X float64
	// OnMoved is told the new position when the user dragged the toolbar by its gripper, and DefaultToolbarX when a double click on
	// the gripper centers it again.
	OnMoved func(x float64)
}

// FloatingToolbar is the debug toolbar over the editor area: a small bar at the top of the editor area with a gripper that drags it
// sideways (a double click on the gripper centers it) and the buttons of the session: Continue or Pause, Step Over, Step Into, Step Out,
// Restart and Stop (Disconnect for an attach), and the session picker with several sessions. It is `debug.toolBarLocation: floating`.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugToolBar.ts (DebugToolBar) and media/debugToolBar.css
type FloatingToolbar struct {
	widgets.StatefulWidget
	opts FloatingToolbarOpts
}

// NewFloatingToolbar makes the toolbar. It fills the area it is given and draws the bar at the top of it.
func NewFloatingToolbar(opts FloatingToolbarOpts) *FloatingToolbar {
	t := &FloatingToolbar{opts: opts}
	t.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: ToolbarKey})
	return t
}

// CreateState implements widgets.StatefulWidget.
func (t *FloatingToolbar) CreateState() widgets.State { return &toolbarState{} }

type toolbarState struct {
	widgets.StateOf[*FloatingToolbar]
	x       float64
	dragged bool
}

// InitState starts at the given position.
func (s *toolbarState) InitState() { s.x = s.start() }

func (s *toolbarState) start() float64 {
	if x := s.Widget().opts.X; x > 0 {
		return x
	}
	return DefaultToolbarX
}

// DidUpdateWidget follows a position the owner sets, unless the user is dragging.
func (s *toolbarState) DidUpdateWidget(old *FloatingToolbar) {
	if !s.dragged && old.opts.X != s.Widget().opts.X {
		s.x = s.start()
	}
}

// Build implements widgets.State.
func (s *toolbarState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	colors := kit.ThemeOf(bc)
	bg := named(colors, "debugToolBar.background", colors.EditorWidgetBackground)
	border := ui.Color{}
	if c, ok := colors.Named("debugToolBar.border"); ok && c.Alpha() != 0 {
		border = c
	}
	if colors.WidgetBorder.Alpha() != 0 {
		border = colors.WidgetBorder
	}
	grip := widgets.NewGestureDetector(
		widgets.NewMouseRegion(
			widgets.NewContainer(
				widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconGripper), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(colors.IconForeground)}),
				widgets.ContainerOpts{Width: foundation.Some(18.0), Height: foundation.Some(ToolbarHeight), Alignment: painting.AlignmentCenter}),
			widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Grab}),
		widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque,
			OnDoubleTap: func() {
				s.dragged = false
				s.SetState(func() { s.x = DefaultToolbarX })
				if w.opts.OnMoved != nil {
					w.opts.OnMoved(DefaultToolbarX)
				}
			},
			OnHorizontalDragUpdate: func(d gestures.DragUpdateDetails) {
				s.dragged = true
				s.SetState(func() { s.x = s.x + d.Delta.Dx/s.width(bc) })
			},
			OnHorizontalDragEnd: func(gestures.DragEndDetails) {
				if w.opts.OnMoved != nil {
					w.opts.OnMoved(s.x)
				}
			},
		})
	bar := kit.NewActionBar(w.opts.Actions, kit.ActionBarOpts{OnRun: w.opts.OnRun})
	box := widgets.NewDecoratedBox(
		painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(bg),
			BorderRadius: painting.NewBorderRadiusCircular(4),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(border), Width: foundation.Some(1.0)}),
			BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: 4})},
		}),
		widgets.NewSizedBox(
			widgets.NewRow([]widgets.Widget{grip, bar, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)})},
				widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}),
			widgets.SizedBoxOpts{Height: foundation.Some(ToolbarHeight)}))
	x := min(max(s.x, 0), 1)
	return widgets.NewAlign(widgets.NewKeyedSubtree(box, widgets.WidgetOpts{Key: ToolbarKey}),
		widgets.AlignOpts{Alignment: painting.Alignment{X: x*2 - 1, Y: -1}})
}

// width is the width of the area the toolbar moves in.
func (s *toolbarState) width(bc widgets.BuildContext) float64 {
	if box, ok := s.Context().FindRenderObject().(rendering.Box); ok && box.Size().Width > 0 {
		return box.Size().Width
	}
	return 800
}
