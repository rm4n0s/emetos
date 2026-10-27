package app

import (
	"context"
	"slices"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The zoom of a window (SPEC §10.9): every window has a zoom level of its own, which starts at window.zoomLevel (or at the
// level the window had when it closed, with window.zoomPerWindow) and which Zoom In, Zoom Out and Reset Zoom change. The
// level is a number of steps of 20%: the window is scaled by 1.2 to the power of the level (kit.Zoom).
//
// Ported from VS Code: src/vs/workbench/electron-sandbox/actions/windowActions.ts (BaseZoomAction, ZoomInAction,
// ZoomOutAction, ZoomResetAction)

// zoomState is the zoom level of a window and who follows it.
type zoomState struct {
	level     float64
	listeners []*func()
}

// ZoomLevel is the zoom level of the window.
func (w *Workspace) ZoomLevel() float64 { return w.zoom.level }

// SetZoomLevel gives the window a zoom level (kept in the range of settings.MinZoomLevel to settings.MaxZoomLevel);
// the listeners of OnZoomChange are told when it changes. It does not write a setting.
func (w *Workspace) SetZoomLevel(level float64) {
	level = settings.ClampZoomLevel(level)
	if level == w.zoom.level {
		return
	}
	w.zoom.level = level
	for _, f := range slices.Clone(w.zoom.listeners) {
		(*f)()
	}
}

// OnZoomChange calls fn, on the UI thread, after the zoom level of the window changed, and returns the function that
// stops it.
func (w *Workspace) OnZoomChange(fn func()) (unsubscribe func()) {
	p := &fn
	w.zoom.listeners = append(w.zoom.listeners, p)
	return func() { w.zoom.listeners = slices.DeleteFunc(w.zoom.listeners, func(x *func()) bool { return x == p }) }
}

// defaultZoomLevel is window.zoomLevel, the level a window starts at and Reset Zoom goes back to.
func (w *Workspace) defaultZoomLevel() float64 {
	return settings.ClampZoomLevel(settings.Get[float64](w.Settings, "window.zoomLevel", ""))
}

// zoomPerWindow is window.zoomPerWindow: whether the zoom commands change the zoom of this window only (the default) or of
// every window, through window.zoomLevel.
func (w *Workspace) zoomPerWindow() bool {
	if w.isView() {
		return true // a view never changes the zoom of the windows of the host
	}
	return settings.Get[bool](w.Settings, "window.zoomPerWindow", "")
}

// followZoom gives the window the zoom level of window.zoomLevel, and makes every change of that setting set it again:
// a window that was zoomed by itself goes back to the setting, as VS Code applies the setting to every window.
func (w *Workspace) followZoom() {
	w.zoom.level = w.defaultZoomLevel()
	stop := w.Settings.OnChange(func(e settings.ChangeEvent) {
		if e.Affects("window.zoomLevel", "") {
			w.onUI(func() { w.SetZoomLevel(w.defaultZoomLevel()) })
		}
	})
	w.stops = append(w.stops, stop)
}

// zoom changes the zoom level of the window by one step in or out (the commands Zoom In and Zoom Out). A level out of the
// range is not reached: the command does nothing. With window.zoomPerWindow false the level is the setting's, which all
// the windows follow.
func (w *Workspace) zoomStep(ctx context.Context, in bool) error {
	next, ok := settings.ZoomStep(w.zoom.level, in)
	if !ok {
		return nil
	}
	return w.applyZoom(ctx, next)
}

// zoomReset is the command Reset Zoom: the level goes back to window.zoomLevel, or to 0 when the zoom is of every window
// (and the setting is written as 0).
func (w *Workspace) zoomReset(ctx context.Context) error {
	level := w.defaultZoomLevel()
	if !w.zoomPerWindow() {
		level = 0
	}
	return w.applyZoom(ctx, level)
}

// applyZoom sets the level of this window, and with window.zoomPerWindow false writes it as window.zoomLevel to the user's
// settings, from where the other windows take it.
func (w *Workspace) applyZoom(ctx context.Context, level float64) error {
	if !w.zoomPerWindow() {
		if ctx == nil {
			ctx = w.ctx
		}
		if err := w.Settings.Update(ctx, "window.zoomLevel", level, settings.TargetUser); err != nil {
			return err
		}
	}
	w.SetZoomLevel(level)
	return nil
}

// registerZoomCommands sets the handlers of the zoom commands.
func (w *Workspace) registerZoomCommands(set func(id string, h command.Handler)) {
	set("workbench.action.zoomIn", func(c command.Context, _ any) error { return w.zoomStep(c.Ctx, true) })
	set("workbench.action.zoomOut", func(c command.Context, _ any) error { return w.zoomStep(c.Ctx, false) })
	set("workbench.action.zoomReset", func(c command.Context, _ any) error { return w.zoomReset(c.Ctx) })
}

// zoomHost scales the widgets below it by the zoom of the window and follows the changes of it. It sits in the builder of
// the app, above the navigator, so that the pages and the overlays (menus, hovers, dialogs) are all zoomed.
type zoomHost struct {
	widgets.StatefulWidget
	child widgets.Widget
}

func newZoomHost(child widgets.Widget) *zoomHost {
	return &zoomHost{StatefulWidget: widgets.NewStatefulWidgetBase(), child: child}
}

// CreateState implements widgets.StatefulWidget.
func (h *zoomHost) CreateState() widgets.State { return &zoomHostState{} }

type zoomHostState struct {
	widgets.StateOf[*zoomHost]
	win  *Workspace
	stop func()
}

// InitState follows the zoom level of the window.
func (s *zoomHostState) InitState() {
	s.win = PeekWorkspace(s.Context())
	s.stop = s.win.OnZoomChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// Dispose stops following.
func (s *zoomHostState) Dispose() { s.stop() }

// Build implements widgets.State.
func (s *zoomHostState) Build(widgets.BuildContext) widgets.Widget {
	return kit.NewZoom(settings.ZoomFactor(s.win.ZoomLevel()), s.Widget().child)
}
