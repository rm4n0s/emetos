package app

import (
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// themeSettings reads the settings that choose the theme from a store.
func themeSettings(s *settings.Store) theme.Settings {
	return theme.Settings{
		ColorTheme:               settings.Get[string](s, "workbench.colorTheme", ""),
		AutoDetectColorScheme:    settings.Get[bool](s, "window.autoDetectColorScheme", ""),
		PreferredDarkColorTheme:  settings.Get[string](s, "workbench.preferredDarkColorTheme", ""),
		PreferredLightColorTheme: settings.Get[string](s, "workbench.preferredLightColorTheme", ""),
	}
}

// followTheme gives the theme service the theme settings of the window, now and whenever they
// change. A theme that cannot be found or read leaves the default theme in use and is reported
// as a warning.
func (w *Window) followTheme() {
	apply := func() {
		if err := w.Process.Themes.Apply(themeSettings(w.Settings)); err != nil {
			w.Process.Notify.Warn("Emetos", "Cannot use the color theme: "+describe(err))
		}
	}
	apply()
	w.Settings.OnChange(func(e settings.ChangeEvent) {
		if e.Affects("workbench", "") || e.Affects("window", "") {
			apply()
		}
	})
}

// themeHost gives the widgets below it the colors of the theme of the process, and rebuilds them
// when the theme changes. It tells the theme service whether the system is dark, and follows the
// system when that changes. The builder gets the colors, since the app below needs some of them
// (the background of the app itself).
type themeHost struct {
	widgets.StatefulWidget
	build func(colors kit.Colors) widgets.Widget
}

func newThemeHost(build func(colors kit.Colors) widgets.Widget) *themeHost {
	return &themeHost{StatefulWidget: widgets.NewStatefulWidgetBase(), build: build}
}

// CreateState implements widgets.StatefulWidget.
func (h *themeHost) CreateState() widgets.State { return &themeHostState{} }

type themeHostState struct {
	widgets.StateOf[*themeHost]
	binding     *widgets.WidgetsBinding
	service     *theme.Service
	colors      kit.Colors
	observer    *brightnessObserver
	unsubscribe func()
}

// brightnessObserver tells the theme host that the system's color scheme changed.
type brightnessObserver struct {
	widgets.WidgetsBindingObserverBase
	changed func()
}

// DidChangePlatformBrightness implements widgets.WidgetsBindingObserver.
func (o *brightnessObserver) DidChangePlatformBrightness() { o.changed() }

// InitState reads the system's color scheme and the theme in use, and follows both.
func (s *themeHostState) InitState() {
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.service = PeekWindow(s.Context()).Process.Themes
	s.reportBrightness()
	s.colors = kit.ColorsOf(s.service.Current())
	s.observer = &brightnessObserver{changed: s.reportBrightness}
	s.binding.AddObserver(s.observer)
	s.unsubscribe = s.service.OnChange(func(ct *theme.ColorTheme) {
		s.binding.Post(func() {
			if !s.Mounted() {
				return
			}
			s.SetState(func() { s.colors = kit.ColorsOf(ct) })
		})
	})
}

// reportBrightness tells the service whether the system's color scheme is dark. A failure to
// find the theme is reported where the settings are applied, not here.
func (s *themeHostState) reportBrightness() {
	dark := s.binding.PlatformDispatcher().PlatformBrightness() == ui.BrightnessDark
	_ = s.service.SetPlatformBrightness(dark)
}

// Dispose stops following the system and the theme service.
func (s *themeHostState) Dispose() {
	s.binding.RemoveObserver(s.observer)
	s.unsubscribe()
}

// Build implements widgets.State.
func (s *themeHostState) Build(widgets.BuildContext) widgets.Widget {
	return kit.NewTheme(s.colors, s.Widget().build(s.colors))
}
