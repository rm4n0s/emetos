package app

import (
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/settings"
	outlineview "github.com/rm4n0s/emetos/internal/gui/views/outline"
)

// The Outline view of the Explorer (SPEC §9.12 "Symbols", "As built (step 12.7)"): the symbols of the active editor. The model
// is in internal/gui/views/outline; this file makes it with the settings of the window, points it at the active editor and
// binds its commands.

// initOutline makes the Outline and follows the settings that shape it.
func (s *shellState) initOutline(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	ex := &s.explorer
	ex.outline = outlineview.New(outlineview.Opts{
		Icons: func() bool { return settings.Get[bool](win.Settings, "outline.icons", "") },
		Show: func(kind int) bool {
			return settings.Get[bool](win.Settings, "outline.show"+langfeatures.SymbolKindSetting(kind), "")
		},
		Collapse: func() string { return settings.Get[string](win.Settings, "outline.collapseItems", "") },
	})
	ex.stops = append(ex.stops, ex.outline.Dispose, win.Settings.OnChange(func(e settings.ChangeEvent) {
		if e.Affects("outline", "") {
			binding.Post(func() {
				if s.Mounted() && ex.outline != nil {
					ex.outline.Refresh()
				}
			})
		}
	}))
	s.syncOutline() // the editors of the command line were made already
}

// syncOutline points the Outline at the text editor in front. A tab that is not a text editor (a diff, a settings page) has
// no symbols. The change is made after the build that is going on, as the editors may be made while the shell builds.
func (s *shellState) syncOutline() {
	if s.explorer.outline == nil || s.group.grid == nil {
		return
	}
	widgets.WidgetsBindingOf(s.Context()).Post(func() {
		ex := &s.explorer
		if !s.Mounted() || ex.outline == nil || s.group.grid == nil {
			return
		}
		if e := s.activeEditor(); e != nil && !e.disposed {
			ex.outline.SetEditor(e.view)
			return
		}
		ex.outline.SetEditor(nil)
	})
}

// bindOutlineCommands takes over the commands of the Outline while the shell is on screen.
func (s *shellState) bindOutlineCommands(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	ex := &s.explorer
	handlers := map[string]command.Handler{
		"outline.focus": func(command.Context, any) error {
			win.Views.Open(command.ViewContainerExplorer)
			win.Views.SetHidden(command.ViewOutline, false)
			win.Views.SetCollapsed(command.ViewOutline, false)
			s.afterFrame(binding, ex.outline.Focus)
			return nil
		},
		"outline.collapse": func(command.Context, any) error { ex.outline.CollapseAll(); return nil },
		"outline.followCursor": func(command.Context, any) error {
			ex.outline.SetFollowing(!ex.outline.Following())
			return nil
		},
	}
	for id, h := range handlers {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		ex.removeCommands = append(ex.removeCommands, remove)
	}
}
