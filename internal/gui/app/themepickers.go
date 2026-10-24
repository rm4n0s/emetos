package app

import (
	"context"
	"slices"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// The pickers of the color theme (Ctrl+K Ctrl+T) and of the file icon theme (SPEC §10.8). Moving the active item
// previews the theme in every window of the process without writing a setting; Enter writes the setting; closing
// the picker any other way, or another quick input taking its place, shows the theme of the settings again.
//
// Ported from VS Code: src/vs/workbench/contrib/themes/browser/themes.contribution.ts (SelectColorThemeAction,
// SelectFileIconThemeAction, InstalledThemesPicker) and
// src/vs/workbench/services/themes/common/themeConfiguration.ts (the target of the setting)

// iconPreview is the file icon theme that is shown for a preview, shared by the windows of the process: the
// theme service does the same for the color theme.
type iconPreview struct {
	mu        sync.Mutex
	on        bool
	id        string
	listeners map[int]func()
	next      int
}

// PreviewFileIconTheme shows the file icon theme with an id ("" for none) in every window, until
// EndFileIconThemePreview or a change of workbench.iconTheme in the settings. Nothing is written.
func (p *Process) PreviewFileIconTheme(id string) {
	p.icons.set(true, id)
}

// EndFileIconThemePreview goes back to the file icon theme of the settings. It does nothing when no preview is
// on.
func (p *Process) EndFileIconThemePreview() {
	p.icons.set(false, "")
}

// FileIconThemePreview tells whether a file icon theme is shown for a preview, and which ("" is none).
func (p *Process) FileIconThemePreview() (id string, on bool) {
	p.icons.mu.Lock()
	defer p.icons.mu.Unlock()
	return p.icons.id, p.icons.on
}

// OnFileIconThemePreview calls fn when a preview of a file icon theme starts, changes or ends. It gives a function
// that stops the calls. The calls are made on the goroutine that changed the preview.
func (p *Process) OnFileIconThemePreview(fn func()) (unsubscribe func()) {
	i := &p.icons
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.listeners == nil {
		i.listeners = map[int]func(){}
	}
	id := i.next
	i.next++
	i.listeners[id] = fn
	return func() {
		i.mu.Lock()
		defer i.mu.Unlock()
		delete(i.listeners, id)
	}
}

// set changes the preview and tells the listeners when it is another.
func (i *iconPreview) set(on bool, id string) {
	i.mu.Lock()
	if i.on == on && i.id == id {
		i.mu.Unlock()
		return
	}
	i.on, i.id = on, id
	listeners := make([]func(), 0, len(i.listeners))
	for _, fn := range i.listeners {
		listeners = append(listeners, fn)
	}
	i.mu.Unlock()
	for _, fn := range listeners {
		fn()
	}
}

// themeCommands are the handlers of the commands of the theme pickers.
func (s *shellState) themeCommands() map[string]command.Handler {
	return map[string]command.Handler{
		"workbench.action.selectTheme": func(command.Context, any) error {
			return s.showColorThemePicker()
		},
		"workbench.action.selectIconTheme": func(command.Context, any) error {
			s.showIconThemePicker()
			return nil
		},
	}
}

// profileThemes are the theme services of the windows that use the profile of the window, the window's own first: a
// preview of a theme shows in all of them (SPEC §8.4).
func (w *Window) profileThemes() []*theme.Service {
	out := []*theme.Service{w.Themes}
	profile := w.Profile()
	for _, e := range w.Process.Windows.entries() {
		if e.win != w && e.win.Profile() == profile {
			out = append(out, e.win.Themes)
		}
	}
	return out
}

// themeItemID is the id of the item of a theme in the picker: a bundled theme and a user theme may share an id.
func themeItemID(info theme.Info) string {
	if info.Bundled {
		return "bundled:" + info.ID
	}
	return "user:" + info.ID
}

// settingsTargetFor is the file a theme setting is written to: the workspace's when its settings.json has the
// setting (and counts), else the user's (VS Code's 'auto' target: the highest layer that sets the value).
func (w *Window) settingsTargetFor(key string) settings.Target {
	_, workspace := w.Settings.Paths()
	in := w.Settings.Inspect(key, "")
	if workspace != "" && in.Workspace.Ok && !in.WorkspaceIgnored {
		return settings.TargetWorkspace
	}
	return settings.TargetUser
}

// endThemePicks ends the previews of the pickers of the shell, as when the picker is closed; the shell calls it
// when it goes away with a picker open.
func (s *shellState) endThemePicks() {
	if s.endPicks == nil {
		return
	}
	end := s.endPicks
	s.endPicks = nil
	end()
}

// showColorThemePicker opens the picker of the color theme: the themes in groups, the one in use active. The
// error has the tag of Service.PickerGroups.
func (s *shellState) showColorThemePicker() error {
	win := PeekWindow(s.Context())
	service := win.Themes
	groups, err := service.PickerGroups()
	if err != nil {
		return err
	}
	var items []quickinput.Item
	byID := map[string]theme.PickerEntry{}
	active := themeItemID(service.CurrentInfo())
	for _, g := range groups {
		items = append(items, quickinput.Item{Separator: true, Label: g.Title})
		for _, e := range g.Entries {
			id := themeItemID(e.Info)
			byID[id] = e
			items = append(items, quickinput.Item{
				ID: id, Label: e.Info.Label, Description: e.Description,
				Accept: func(quickinput.KeyMods) { s.chooseColorTheme(e) },
			})
		}
	}
	// the preview shows in every window that uses the profile of this one: they have the same settings.json, so the
	// choice would reach them all
	services := win.profileThemes()
	end := func() {
		for _, sv := range services {
			if err := sv.EndPreview(); err != nil && sv == service {
				win.Notify.Warn("Emetos", "Cannot use the color theme: "+describe(err))
			}
		}
	}
	s.endPicks = end
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select Color Theme (Up/Down Keys to Preview)", Items: items, MatchOnDescription: true, ActiveID: active,
		OnActive: func(item quickinput.Item, ok bool) {
			e, found := byID[item.ID]
			if !ok || !found {
				end()
				return
			}
			for _, sv := range services {
				if err := sv.Preview(e.Value); err != nil && sv == service {
					win.Notify.Warn("Emetos", "Cannot use the color theme: "+describe(err))
					end()
					return
				}
			}
		},
		OnHide: func(accepted bool) {
			if !accepted {
				s.endPicks = nil
				end()
			}
		},
	})
	return nil
}

// chooseColorTheme writes the choice to workbench.colorTheme and keeps the theme that is shown. When the
// setting cannot be written the theme goes back to the one of the settings and the user is told.
func (s *shellState) chooseColorTheme(e theme.PickerEntry) {
	win := PeekWindow(s.Context())
	s.endPicks = nil
	err := win.Settings.Update(context.Background(), "workbench.colorTheme", e.Value, win.settingsTargetFor("workbench.colorTheme"))
	// the change of the setting ended the preview with the same theme; when it was not written the preview
	// goes
	for _, sv := range win.profileThemes() {
		if endErr := sv.EndPreview(); endErr != nil && sv == win.Themes {
			win.Notify.Warn("Emetos", "Cannot use the color theme: "+describe(endErr))
		}
	}
	if err != nil {
		win.Notify.Error("Emetos", "Cannot save the color theme: "+describe(err))
	}
}

// showIconThemePicker opens the picker of the file icon theme: None and the bundled themes, the one in use
// active.
func (s *shellState) showIconThemePicker() {
	win := PeekWindow(s.Context())
	choices := theme.FileIconChoices()
	current, _ := settings.Get[any](win.Settings, "workbench.iconTheme", "").(string)
	items := []quickinput.Item{{Separator: true, Label: theme.PickFileIconThemes}}
	byID := map[string]theme.FileIconChoice{}
	for _, c := range choices {
		byID[activeIconID(c.ID)] = c
		items = append(items, quickinput.Item{
			ID: activeIconID(c.ID), Label: c.Label, Description: c.Description,
			Accept: func(quickinput.KeyMods) { s.chooseFileIconTheme(c) },
		})
	}
	// a theme that is not known shows no icons: None is the active item then
	if !slices.ContainsFunc(choices, func(c theme.FileIconChoice) bool { return c.ID == current }) {
		current = ""
	}
	end := win.Process.EndFileIconThemePreview
	s.endPicks = end
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select File Icon Theme (Up/Down Keys to Preview)", Items: items, MatchOnDescription: true,
		ActiveID: activeIconID(current),
		OnActive: func(item quickinput.Item, ok bool) {
			if c, found := byID[item.ID]; ok && found {
				win.Process.PreviewFileIconTheme(c.ID)
				return
			}
			end()
		},
		OnHide: func(accepted bool) {
			if !accepted {
				s.endPicks = nil
				end()
			}
		},
	})
}

// activeIconID is the id of the item of an icon theme in the picker.
func activeIconID(id string) string {
	if id == "" {
		return noIconThemeID
	}
	return id
}

// noIconThemeID is the id of the item None of the icon theme picker; the id of the choice is empty, which
// PickOptions.ActiveID cannot name.
const noIconThemeID = "none"

// chooseFileIconTheme writes the choice to workbench.iconTheme (null for None).
func (s *shellState) chooseFileIconTheme(c theme.FileIconChoice) {
	win := PeekWindow(s.Context())
	s.endPicks = nil
	err := win.Settings.Update(context.Background(), "workbench.iconTheme", c.SettingValue(), win.settingsTargetFor("workbench.iconTheme"))
	win.Process.EndFileIconThemePreview()
	if err != nil {
		win.Notify.Error("Emetos", "Cannot save the file icon theme: "+describe(err))
	}
}
