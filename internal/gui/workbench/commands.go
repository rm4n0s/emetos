package workbench

import (
	"context"
	"time"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// bindCommands takes over the layout commands in the registry while the workbench is on screen.
// The registry must have them (command.DefaultRegistry does): a missing one is a mistake of the
// caller and panics with the CommandNotFound of command.Registry.SetHandler.
//
// Ported from VS Code: src/vs/workbench/browser/actions/layoutActions.ts,
// src/vs/workbench/browser/parts/panel/panelActions.ts,
// src/vs/workbench/browser/parts/sidebar/sidebarActions.ts,
// src/vs/workbench/browser/parts/statusbar/statusbarActions.ts,
// src/vs/workbench/browser/parts/editor/editorActions.ts,
// src/vs/workbench/browser/parts/activitybar/activitybarPart.ts and
// src/vs/workbench/browser/parts/titlebar/menubarControl.ts
func (s *workbenchState) bindCommands() {
	reg := s.Widget().opts.Commands
	if reg == nil {
		return
	}
	l := s.layout
	handlers := map[string]command.Handler{
		"workbench.action.toggleSidebarVisibility": func(command.Context, any) error { l.ToggleSideBar(); return nil },
		"workbench.action.togglePanel":             func(command.Context, any) error { l.TogglePanel(); return nil },
		"workbench.action.toggleMaximizedPanel":    func(command.Context, any) error { s.toggleMaximizedPanel(); return nil },
		"workbench.action.positionPanelLeft":       func(command.Context, any) error { l.SetPanelPosition(PanelLeft); return nil },
		"workbench.action.positionPanelRight":      func(command.Context, any) error { l.SetPanelPosition(PanelRight); return nil },
		"workbench.action.positionPanelBottom":     func(command.Context, any) error { l.SetPanelPosition(PanelBottom); return nil },
		"workbench.action.positionPanelTop":        func(command.Context, any) error { l.SetPanelPosition(PanelTop); return nil },

		// the commands VS Code keeps in a setting write it, and the layout follows
		"workbench.action.toggleStatusbarVisibility": func(c command.Context, _ any) error {
			if l.ZenActive() {
				// zen mode hides the bar apart from the setting: showing it is a change of the layout, which zen
				// mode leaves as it is
				l.SetStatusBarVisible(!l.State().StatusBarVisible)
				return nil
			}
			return s.updateSetting(c.Ctx, "workbench.statusBar.visible", !l.State().StatusBarVisible, func() { l.SetStatusBarVisible(!l.State().StatusBarVisible) })
		},
		"workbench.action.toggleActivityBarVisibility": func(c command.Context, _ any) error {
			if l.ZenActive() {
				l.SetActivityBarVisible(!l.State().ActivityBarVisible)
				return nil
			}
			value := "hidden"
			if !l.State().ActivityBarVisible {
				value = "default"
			}
			return s.updateSetting(c.Ctx, "workbench.activityBar.location", value, func() { l.SetActivityBarVisible(!l.State().ActivityBarVisible) })
		},
		"workbench.action.toggleMenuBar": func(c command.Context, _ any) error {
			// VS Code's custom menu goes from classic or visible to compact, and back to classic; Emetos has no
			// compact menu, and the nearest is toggle (the menu on the Alt key).
			value := "classic"
			if v := s.settingMenuBarVisibility(); v == kit.MenuBarClassic || v == kit.MenuBarVisible {
				value = "toggle"
			}
			return s.updateSetting(c.Ctx, "window.menuBarVisibility", value, nil)
		},

		"workbench.action.focusSideBar": func(command.Context, any) error {
			s.focusPart(PartSideBar, func() { l.SetSideBarVisible(true) })
			return nil
		},
		"workbench.action.focusActiveEditorGroup": func(command.Context, any) error {
			if l.Visible(PartEditor) {
				s.nodes[PartEditor].RequestFocus()
			}
			return nil
		},
		"workbench.action.focusPanel": func(command.Context, any) error {
			s.focusPart(PartPanel, func() { l.SetPanelVisible(true) })
			return nil
		},
		"workbench.action.focusStatusBar": func(command.Context, any) error {
			if l.State().StatusBarVisible {
				s.nodes[PartStatusBar].RequestFocus()
			}
			return nil
		},
		"workbench.action.focusActivityBar": func(command.Context, any) error {
			if l.State().ActivityBarVisible {
				s.nodes[PartActivityBar].RequestFocus()
			}
			return nil
		},
		"workbench.actions.menubar.focus": func(command.Context, any) error {
			s.menuCtl.ToggleFocus()
			return nil
		},
	}
	if views := s.Widget().opts.Views; views != nil {
		for id, h := range s.panelViewHandlers(views) {
			handlers[id] = h
		}
	}
	for id, h := range handlers {
		remove, err := reg.SetHandler(id, h)
		if err != nil {
			panic(err)
		}
		s.removeCommands = append(s.removeCommands, remove)
	}
}

// updateSetting writes a setting of the user, which the layout then follows. Without a store of
// settings it runs direct, which changes the layout itself.
func (s *workbenchState) updateSetting(ctx context.Context, key string, value any, direct func()) error {
	st := s.Widget().opts.Settings
	if st == nil {
		if direct != nil {
			direct()
		}
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return st.Update(ctx, key, value, settings.TargetUser)
}

// focusPart gives the focus to a part, after show has shown it. A part that was hidden is not
// there to take the focus until the next frame has built it.
func (s *workbenchState) focusPart(p Part, show func()) {
	hidden := !s.layout.Visible(p)
	show()
	if !hidden {
		s.nodes[p].RequestFocus()
		return
	}
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if s.Mounted() && s.layout.Visible(p) {
			s.nodes[p].RequestFocus()
		}
	})
}

// toggleMaximizedPanel maximizes the panel or brings it back. The size the panel has now is what it
// comes back to, so it is taken before the split view reports it (a drag of the sash is reported
// after its frame, and the report of a maximized panel is not its own size).
func (s *workbenchState) toggleMaximizedPanel() {
	if !s.layout.PanelMaximized() {
		if sizes := s.centerCtl.Sizes(); len(sizes) > 0 {
			s.centerSizes(sizes)
		}
	}
	s.layout.ToggleMaximizedPanel()
}

// panelViewHandlers are the handlers of the commands that open, toggle and focus the views of the panel
// (the Problems and the Output).
//
// Ported from VS Code: src/vs/workbench/services/views/browser/viewsService.ts (the open and focus
// actions of a view) and src/vs/workbench/contrib/markers/browser/markers.contribution.ts
// (workbench.actions.view.toggleProblems, workbench.action.showErrorsWarnings)
func (s *workbenchState) panelViewHandlers(views *Views) map[string]command.Handler {
	l := s.layout
	// shown tells whether the container is the tab the panel shows.
	shown := func(container string) bool { return l.State().PanelVisible && views.ActivePanel() == container }
	open := func(container string, focus bool) {
		if !focus {
			views.OpenPanel(container)
			return
		}
		s.focusPart(PartPanel, func() { views.OpenPanel(container) })
		scheduler := s.binding.Scheduler()
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(time.Duration) {
			if !s.Mounted() || !shown(container) {
				return
			}
			if f := s.Widget().opts.FocusPanelView; f != nil {
				f(container)
			}
		})
	}
	// the command of a view: it hides the panel when the view has the focus, else it shows the view and
	// gives it the focus
	toggle := func(container string) command.Handler {
		return func(command.Context, any) error {
			if shown(container) && s.nodes[PartPanel].HasFocus() {
				l.SetPanelVisible(false)
				return nil
			}
			open(container, true)
			return nil
		}
	}
	focus := func(container string) command.Handler {
		return func(command.Context, any) error { open(container, true); return nil }
	}
	return map[string]command.Handler{
		"workbench.actions.view.problems":      toggle(command.ViewContainerMarkers),
		"workbench.action.output.toggleOutput": toggle(command.ViewContainerOutput),
		"workbench.panel.markers.view.focus":   focus(command.ViewContainerMarkers),
		"workbench.panel.output.focus":         focus(command.ViewContainerOutput),
		"workbench.action.problems.focus":      focus(command.ViewContainerMarkers),
		"workbench.action.showErrorsWarnings": func(command.Context, any) error {
			open(command.ViewContainerMarkers, false)
			return nil
		},
		"workbench.actions.view.toggleProblems": func(command.Context, any) error {
			if shown(command.ViewContainerMarkers) {
				l.SetPanelVisible(false)
				return nil
			}
			open(command.ViewContainerMarkers, true)
			return nil
		},
	}
}
