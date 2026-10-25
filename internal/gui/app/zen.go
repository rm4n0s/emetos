package app

import (
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// Full screen, zen mode and the centered layout (SPEC §10.9).
//
// The window knows whether it is in full screen mode as the platform reports it (the controller of the Glutter window tells
// the shell, which tells the window: fullScreenChanged), asks for it through that controller (SetFullScreen), and keeps
// the context key isFullscreen. Zen mode is a state of the layout (workbench.ZenState: what it hid and what it turned on),
// so that the window saves it with the layout and starts in it again; the window turns on and off what the layout does not
// hold (full screen, do not disturb) and what the editors and the tabs derive from it (zenHidesLineNumbers, ShowTabs).
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts (toggleZenMode, centerMainEditorLayout, the full screen
// handling of onFullscreenChanged), src/vs/workbench/browser/workbench.zenMode.contribution.ts,
// src/vs/workbench/browser/actions/layoutActions.ts (toggleCenteredLayout) and
// src/vs/workbench/browser/actions/windowActions.ts (ToggleFullScreenAction)

// modeState is what the window knows of its full screen mode.
type modeState struct {
	// fullscreen is the state the platform reported last.
	fullscreen bool
	// fullScreenSetter, when set, asks for the full screen mode in place of the window controller (tests).
	fullScreenSetter func(on bool)
	// onController holds what waits for the window controller to be known.
	onController []func()
	// listeners are told when the full screen mode changes.
	listeners []*func()
}

// IsFullScreen says whether the window is in full screen mode, as the platform last reported it.
func (w *Workspace) IsFullScreen() bool { return w.modes.fullscreen }

// SetFullScreen asks the platform to put the window in full screen mode, or to bring it back. The platform answers by
// reporting its state (fullScreenChanged), which may be a refusal. It fails with the tag FullScreenUnavailable when the
// window is not on screen yet, which has no controller to ask.
func (w *Workspace) SetFullScreen(on bool) error {
	if set := w.modes.fullScreenSetter; set != nil {
		set(on)
		return nil
	}
	// Glutter: BaseWindowController (what WindowScope gives) has no SetFullscreen, which RegularWindowController has;
	// an interface assertion reaches it (docs/GLUTTER-ISSUES.md). Glutter should put it in the interface.
	if c, ok := w.controller.(interface{ SetFullscreen(bool) }); ok {
		c.SetFullscreen(on)
		return nil
	}
	return errors.New("FullScreenUnavailable", "the window has no controller that can change its full screen mode", "fullscreen", on)
}

// toggleFullScreen is the command Toggle Full Screen.
func (w *Workspace) toggleFullScreen() error { return w.SetFullScreen(!w.modes.fullscreen) }

// OnModesChange calls fn, on the UI thread, after the full screen mode of the window changed, and returns the function
// that stops it.
func (w *Workspace) OnModesChange(fn func()) (unsubscribe func()) {
	p := &fn
	w.modes.listeners = append(w.modes.listeners, p)
	return func() {
		w.modes.listeners = slices.DeleteFunc(w.modes.listeners, func(x *func()) bool { return x == p })
	}
}

// fullScreenChanged takes the full screen state the platform reported: the context key follows it, and when a window that
// zen mode put in full screen leaves it, zen mode ends too (VS Code does the same).
func (w *Workspace) fullScreenChanged(on bool) {
	if w.modes.fullscreen == on {
		return
	}
	w.modes.fullscreen = on
	w.syncLayoutKeys()
	for _, f := range slices.Clone(w.modes.listeners) {
		(*f)()
	}
	if z := w.Layout.State().Zen; !on && z.Active && z.FullScreen {
		w.exitZen()
	}
}

// controllerKnown runs what waited for the controller of the window.
func (w *Workspace) controllerKnown() {
	pending := w.modes.onController
	w.modes.onController = nil
	for _, f := range pending {
		f()
	}
}

// whenController runs f once the window has a controller (at once when it has).
func (w *Workspace) whenController(f func()) {
	if w.controller != nil || w.modes.fullScreenSetter != nil {
		f()
		return
	}
	w.modes.onController = append(w.modes.onController, f)
}

// syncLayoutKeys sets the context keys of the modes: inZenMode, isCenteredLayout and isFullscreen.
func (w *Workspace) syncLayoutKeys() {
	st := w.Layout.State()
	w.Keys.Batch(func() {
		w.Keys.Set("inZenMode", st.Zen.Active)
		w.Keys.Set("isCenteredLayout", st.CenteredLayout)
		w.Keys.Set("isFullscreen", w.modes.fullscreen)
	})
}

// zenConfig is the zenMode.* settings.
type zenConfig struct {
	fullScreen, centerLayout, hideStatusBar, hideActivityBar, hideLineNumbers, restore, silentNotifications bool
	showTabs                                                                                                string
}

func (w *Workspace) zenConfig() zenConfig {
	get := func(key string) bool { return settings.Get[bool](w.Settings, key, "") }
	return zenConfig{
		fullScreen: get("zenMode.fullScreen"), centerLayout: get("zenMode.centerLayout"), hideStatusBar: get("zenMode.hideStatusBar"),
		hideActivityBar: get("zenMode.hideActivityBar"), hideLineNumbers: get("zenMode.hideLineNumbers"), restore: get("zenMode.restore"),
		silentNotifications: get("zenMode.silentNotifications"), showTabs: settings.Get[string](w.Settings, "zenMode.showTabs", ""),
	}
}

// ZenMode says whether zen mode is on.
func (w *Workspace) ZenMode() bool { return w.Layout.ZenActive() }

// ShowTabs is what the editor groups show of their tabs: workbench.editor.showTabs ("multiple", "single" or "none"), or
// zenMode.showTabs while zen mode is on. It is the zen mode's override of the setting, which is not written.
func (w *Workspace) ShowTabs() string {
	if w.Layout.ZenActive() {
		return w.zenConfig().showTabs
	}
	return settings.Get[string](w.Settings, "workbench.editor.showTabs", "")
}

// zenHidesLineNumbers says that the editors show no line numbers because zen mode is on and zenMode.hideLineNumbers says
// so; the setting editor.lineNumbers is not changed.
func (w *Workspace) zenHidesLineNumbers() bool {
	return w.Layout.ZenActive() && w.zenConfig().hideLineNumbers
}

// ToggleZenMode turns zen mode on, or off when it is on.
func (w *Workspace) ToggleZenMode() {
	if w.Layout.ZenActive() {
		w.exitZen()
		return
	}
	w.enterZen()
}

// enterZen turns zen mode on: the side bar and the panel are hidden, the activity bar and the status bar when the settings
// say so, the menu bar always (the workbench does it), and the window goes to full screen, centers the layout and turns do
// not disturb on as zenMode.fullScreen, zenMode.centerLayout and zenMode.silentNotifications say. What it turned on is
// remembered, and is turned off when zen mode ends; what was on already is left as it is. The tabs and the line numbers of
// the editors follow (ShowTabs, zenHidesLineNumbers).
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts (toggleZenMode)
func (w *Workspace) enterZen() {
	cfg := w.zenConfig()
	goFullScreen := !w.modes.fullscreen && cfg.fullScreen
	centered := !w.Layout.CenteredLayout() && cfg.centerLayout
	dndWasOff := !w.Notify.DoNotDisturb()
	w.Layout.EnterZen(cfg.hideActivityBar, cfg.hideStatusBar)
	if cfg.silentNotifications && dndWasOff {
		w.Notify.SetDoNotDisturb(true)
	}
	if cfg.centerLayout {
		w.Layout.SetCenteredLayout(true)
	}
	if goFullScreen {
		if err := w.SetFullScreen(true); err != nil {
			w.Process.Logger.Warn("zen mode cannot go to full screen", "err", err)
			goFullScreen = false
		}
	}
	w.Layout.SetZenTransitions(goFullScreen, centered, dndWasOff)
}

// exitZen turns zen mode off: the parts it hid come back (those the user did not change meanwhile), and what it turned on
// is turned off.
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts (toggleZenMode)
func (w *Workspace) exitZen() {
	z := w.Layout.ExitZen()
	if !z.Active {
		return
	}
	if z.Centered {
		w.Layout.SetCenteredLayout(false)
	}
	if z.DoNotDisturb {
		w.Notify.SetDoNotDisturb(false)
	}
	if z.FullScreen && w.modes.fullscreen {
		if err := w.SetFullScreen(false); err != nil {
			w.Process.Logger.Warn("zen mode cannot leave full screen", "err", err)
		}
	}
}

// restoreZen is called when the saved layout has been put back: a window that was in zen mode when it closed starts in it
// (zenMode.restore), with do not disturb on again and the full screen mode asked for again once the window is on
// screen, or ends it with what it recorded.
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts (restoreWorkbenchLayout)
func (w *Workspace) restoreZen() {
	z := w.Layout.State().Zen
	if !z.Active {
		return
	}
	cfg := w.zenConfig()
	if !cfg.restore {
		w.exitZen()
		return
	}
	if z.DoNotDisturb && cfg.silentNotifications {
		w.Notify.SetDoNotDisturb(true)
	}
	if z.FullScreen && cfg.fullScreen {
		w.whenController(func() {
			if w.modes.fullscreen || !w.Layout.ZenActive() {
				return
			}
			if err := w.SetFullScreen(true); err != nil {
				w.Process.Logger.Warn("zen mode cannot go to full screen", "err", err)
			}
		})
	}
}

// followZen follows the zenMode.* settings while zen mode is on: the activity bar, the status bar, the centered layout and
// do not disturb take the new values (the tabs and the line numbers are read when they are drawn).
func (w *Workspace) followZen() {
	stop := w.Settings.OnChange(func(e settings.ChangeEvent) {
		if !e.Affects("zenMode", "") {
			return
		}
		w.onUI(func() {
			z := w.Layout.State().Zen
			if !z.Active {
				return
			}
			cfg := w.zenConfig()
			w.Layout.UpdateZen(cfg.hideActivityBar, cfg.hideStatusBar)
			if e.Affects("zenMode.centerLayout", "") {
				w.Layout.SetCenteredLayout(cfg.centerLayout)
			}
			if e.Affects("zenMode.silentNotifications", "") && z.DoNotDisturb {
				w.Notify.SetDoNotDisturb(cfg.silentNotifications)
			}
		})
	})
	w.stops = append(w.stops, stop)
}

// ToggleCenteredLayout turns the centered layout on, or off when it is on.
func (w *Workspace) ToggleCenteredLayout() {
	w.Layout.SetCenteredLayout(!w.Layout.CenteredLayout())
}

// registerModeCommands sets the handlers of the commands of full screen, zen mode and the centered layout, and of the zoom.
func (w *Workspace) registerModeCommands(set func(id string, h command.Handler)) {
	set("workbench.action.toggleFullScreen", func(command.Context, any) error { return w.toggleFullScreen() })
	set("workbench.action.toggleZenMode", func(command.Context, any) error { w.ToggleZenMode(); return nil })
	set("workbench.action.exitZenMode", func(command.Context, any) error {
		if w.Layout.ZenActive() {
			w.exitZen()
		}
		return nil
	})
	set("workbench.action.toggleCenteredLayout", func(c command.Context, _ any) error {
		w.ToggleCenteredLayout()
		// VS Code gives the focus to the active group
		if err := w.Commands.Run(command.Context{Ctx: c.Ctx, Keys: c.Keys, Source: c.Source}, "workbench.action.focusActiveEditorGroup", nil); err != nil {
			w.Process.Logger.Debug("the focus of the editor group is not given", "err", err)
		}
		return nil
	})
	w.registerZoomCommands(set)
}
