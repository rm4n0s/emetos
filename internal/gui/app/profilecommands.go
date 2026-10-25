package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// The commands of the profiles (SPEC §10.12, step 8.6b): create, switch, rename, delete, export and import. They ask
// with the quick input (a list of profiles, an input box for a name, a question in a dialog) and the file chooser
// of the desktop, and act on storage.Profiles and Workspace.SwitchProfile.
//
// Ported from VS Code: src/vs/workbench/contrib/userDataProfile/browser/userDataProfile.ts (the actions
// switchProfile, createProfile, deleteProfile and exportProfile; VS Code makes and renames profiles in its profiles
// editor, which Emetos does not have)

// Texts of the commands of the profiles.
const (
	// ProfileNamePlaceholder is the hint of the input box that asks for the name of a profile.
	ProfileNamePlaceholder = "Profile name"
	// ProfileNameInvalidText is what the input box says about a name that is not allowed.
	ProfileNameInvalidText = "A profile name has 1 to 64 letters, digits, spaces, - or _, and no space at its start or end."
	// ProfileNameReservedText is what the input box says about the name of the default profile.
	ProfileNameReservedText = "The name 'Default' belongs to the default profile."
	// ProfileExistsText is what the input box says about a name that is taken; the argument is the name.
	ProfileExistsText = "A profile with the name '%s' exists already."
	// ProfileDeleteQuestion asks whether to delete a profile; the argument is its name.
	ProfileDeleteQuestion = "Are you sure you want to delete the profile '%s'? This cannot be undone."
	// NoProfilesToRenameText and NoProfilesToDeleteText are the notices when only the default profile exists.
	NoProfilesToRenameText = "There are no profiles to rename."
	// NoProfilesToDeleteText is the notice of Delete Profile when only the default profile exists.
	NoProfilesToDeleteText = "There are no profiles to delete."
)

// profileFilters are the kinds of file the dialogs of Export Profile and Import Profile offer.
var profileFilters = []desktop.FileFilter{
	{Name: "Emetos profile (*.json)", Patterns: []string{"*.json", "*.JSON"}},
	{Name: "All files", Patterns: []string{"*"}},
}

// profileFileSuffix follows the name of a profile in the name an exported file starts with.
const profileFileSuffix = ".profile.json"

// chooseProfileExportPath shows the save dialog of Export Profile and waits for the user. It must be called from a
// goroutine. A dialog that is closed without a choice gives "" and no error. The error has the tag
// ProfileExportDialogFailed.
func chooseProfileExportPath(ctx context.Context, bc widgets.BuildContext, name string) (string, error) {
	res, err := desktop.SaveFile(ctx, bc, desktop.SaveFileOpts{
		Title: "Export Profile", AcceptLabel: "Export", Filters: profileFilters, CurrentName: name + profileFileSuffix,
	})
	if err != nil {
		return "", errors.NewErr("ProfileExportDialogFailed", err)
	}
	if res.Cancelled || len(res.Paths) == 0 {
		return "", nil
	}
	return res.Paths[0], nil
}

// chooseProfileImportPath shows the open dialog of Import Profile and waits for the user. It must be called from a
// goroutine. A dialog that is closed without a choice gives "" and no error. The error has the tag
// ProfileImportDialogFailed.
func chooseProfileImportPath(ctx context.Context, bc widgets.BuildContext) (string, error) {
	res, err := desktop.OpenFiles(ctx, bc, desktop.OpenFilesOpts{
		Title: "Import Profile", AcceptLabel: "Import", Filters: profileFilters,
	})
	if err != nil {
		return "", errors.NewErr("ProfileImportDialogFailed", err)
	}
	if res.Cancelled || len(res.Paths) == 0 {
		return "", nil
	}
	return res.Paths[0], nil
}

// profileCommands are the handlers of the commands of the profiles.
func (s *shellState) profileCommands() map[string]command.Handler {
	return map[string]command.Handler{
		"workbench.profiles.actions.createProfile": func(command.Context, any) error { s.createProfile(); return nil },
		"workbench.profiles.actions.switchProfile": func(command.Context, any) error { s.switchProfile(); return nil },
		"workbench.profiles.actions.renameProfile": func(command.Context, any) error { s.renameProfile(); return nil },
		"workbench.profiles.actions.deleteProfile": func(command.Context, any) error { s.deleteProfile(); return nil },
		"workbench.profiles.actions.exportProfile": func(command.Context, any) error { s.exportProfile(); return nil },
		"workbench.profiles.actions.importProfile": func(command.Context, any) error { s.importProfile(); return nil },
	}
}

// profileItemID is the id of the item of a profile in a picker; the default profile (the empty string) has one too.
func profileItemID(profile string) string { return "profile:" + profile }

// listProfiles gives the profiles, the default one first. A failure is reported to the user and gives nil.
func (s *shellState) listProfiles() []string {
	win := PeekWorkspace(s.Context())
	list, err := win.Process.Profiles.List(win.ctx)
	if err != nil {
		win.Notify.Error("Emetos", "Cannot list the profiles: "+describe(err))
		return nil
	}
	return list
}

// profileItems are the items of a picker over profiles: the name of each, and "Current" for the profile of the
// window.
func (s *shellState) profileItems(profiles []string, accept func(profile string)) []quickinput.Item {
	win := PeekWorkspace(s.Context())
	current := win.Profile()
	items := make([]quickinput.Item, 0, len(profiles))
	for _, profile := range profiles {
		item := quickinput.Item{
			ID: profileItemID(profile), Label: storage.ProfileDisplayName(profile),
			Accept: func(quickinput.KeyMods) { accept(profile) },
		}
		if profile == current {
			item.Description = "Current"
		}
		items = append(items, item)
	}
	return items
}

// profileNameProblem is what the input box says about a name for a profile: "" when it is fine. except is the
// profile that is renamed, which may keep its name (in another capitalization).
func (s *shellState) profileNameProblem(name, except string) string {
	win := PeekWorkspace(s.Context())
	err := win.Process.Profiles.CheckName(win.ctx, name, except)
	if err == nil {
		return ""
	}
	if appErr, ok := errors.FromError(err); ok {
		switch appErr.Tag {
		case "ProfileNameInvalid":
			return ProfileNameInvalidText
		case "ProfileNameReserved":
			return ProfileNameReservedText
		case "ProfileExists":
			return strings.Replace(ProfileExistsText, "%s", name, 1)
		}
	}
	return describe(err)
}

// askProfileName shows the input box for the name of a profile, validated as the user types; Enter on a name that is
// not valid keeps the box open with the message. accept gets the name.
func (s *shellState) askProfileName(value, except string, accept func(name string)) {
	win := PeekWorkspace(s.Context())
	win.QuickInput.Prompt(quickinput.PromptOptions{
		Placeholder: ProfileNamePlaceholder,
		Value:       value,
		Validate:    func(v string) string { return s.profileNameProblem(v, except) },
		OnAccept: func(name string) {
			if s.Mounted() {
				accept(name)
			}
		},
	})
}

// profileTask runs work on a goroutine and calls then with its outcome on the UI thread, unless the window is gone.
func (s *shellState) profileTask(work func(ctx context.Context) (string, error), then func(name string, err error)) {
	binding := widgets.WidgetsBindingOf(s.Context())
	s.Go(func(life context.Context) {
		name, err := work(life)
		binding.Post(func() {
			if s.Mounted() {
				then(name, err)
			}
		})
	})
}

// switchTo gives the window the profile and tells the user when that fails.
func (s *shellState) switchTo(profile string) {
	win := PeekWorkspace(s.Context())
	if err := win.SwitchProfile(win.ctx, profile); err != nil {
		win.Notify.Error("Emetos", "Cannot switch to the profile '"+storage.ProfileDisplayName(profile)+"': "+describe(err))
	}
}

// ---- create ----

// createProfile asks for a name and for what the profile starts with, makes it and switches the window to it.
func (s *shellState) createProfile() {
	s.askProfileName("", "", s.pickCreateSource)
}

// pickCreateSource asks what the new profile starts with: nothing, or a copy of any of the profiles (the current
// one first).
func (s *shellState) pickCreateSource(name string) {
	win := PeekWorkspace(s.Context())
	list := s.listProfiles()
	if list == nil {
		return
	}
	current := win.Profile()
	create := func(source storage.CreateSource) {
		s.profileTask(func(ctx context.Context) (string, error) {
			return win.Process.Profiles.Create(ctx, name, source)
		}, func(made string, err error) {
			if err != nil {
				win.Notify.Error("Emetos", "Cannot create the profile '"+name+"': "+describe(err))
				return
			}
			s.switchTo(made)
		})
	}
	items := []quickinput.Item{{
		ID: "empty", Label: "Empty Profile", Description: "Start with no settings, keybindings or snippets",
		Accept: func(quickinput.KeyMods) { create(storage.CreateEmpty) },
	}}
	ordered := append([]string{current}, slices.DeleteFunc(slices.Clone(list), func(p string) bool { return p == current })...)
	for _, profile := range ordered {
		item := quickinput.Item{
			ID: profileItemID(profile), Label: "Copy from " + storage.ProfileDisplayName(profile),
			Accept: func(quickinput.KeyMods) { create(storage.CreateFrom(profile)) },
		}
		if profile == current {
			item.Description = "Current"
		}
		items = append(items, item)
	}
	win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Create profile from", Items: items, MatchOnDescription: true})
}

// ---- switch ----

// switchProfile lists the profiles; the one the window has is active, and choosing another switches the window to it.
func (s *shellState) switchProfile() {
	win := PeekWorkspace(s.Context())
	list := s.listProfiles()
	if list == nil {
		return
	}
	current := win.Profile()
	items := s.profileItems(list, func(profile string) {
		if profile != current {
			s.switchTo(profile)
		}
	})
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select Profile", Items: items, MatchOnDescription: true, ActiveID: profileItemID(current),
	})
}

// ---- rename ----

// renameProfile asks which profile to rename (not the default one) and for its new name.
func (s *shellState) renameProfile() {
	win := PeekWorkspace(s.Context())
	list := s.listProfiles()
	if list == nil {
		return
	}
	if len(list) < 2 {
		win.Notify.Info("Emetos", NoProfilesToRenameText)
		return
	}
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select Profile to Rename", MatchOnDescription: true,
		Items: s.profileItems(list[1:], func(profile string) {
			s.askProfileName(profile, profile, func(name string) {
				if name != profile {
					s.doRenameProfile(profile, name)
				}
			})
		}),
	})
}

// doRenameProfile renames a profile. The windows that use it follow in place (Workspace.profileRenamed): the state of
// each is written first, since the folder of the state moves, and nothing closes.
func (s *shellState) doRenameProfile(profile, name string) {
	win := PeekWorkspace(s.Context())
	p := win.Process
	users := p.Workspaces.UsingProfile(profile)
	for _, w := range users {
		if w.flushState != nil {
			w.flushState()
		}
	}
	if err := p.Profiles.Rename(win.ctx, profile, name); err != nil {
		win.Notify.Error("Emetos", "Cannot rename the profile '"+profile+"': "+describe(err))
		return
	}
	p.forgetState(profile)
	p.forgetState(name)
	for _, w := range users {
		if err := w.profileRenamed(w.ctx, name); err != nil {
			w.Notify.Error("Emetos", "Cannot use the profile '"+name+"': "+describe(err))
		}
	}
}

// ---- delete ----

// deleteProfile asks which profile to delete (not the default one) and whether to be sure. The windows that use it
// switch to the default profile first.
func (s *shellState) deleteProfile() {
	win := PeekWorkspace(s.Context())
	list := s.listProfiles()
	if list == nil {
		return
	}
	if len(list) < 2 {
		win.Notify.Info("Emetos", NoProfilesToDeleteText)
		return
	}
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select Profile to Delete", MatchOnDescription: true,
		Items: s.profileItems(list[1:], func(profile string) {
			s.confirm(confirmSpec{
				Type: kit.DialogWarning, Message: strings.Replace(ProfileDeleteQuestion, "%s", profile, 1), Primary: "Delete",
			}, func(confirmed, _ bool) {
				if confirmed && s.Mounted() {
					s.doDeleteProfile(profile)
				}
			})
		}),
	})
}

// doDeleteProfile moves the windows that use the profile to the default profile, in place, and deletes the profile.
func (s *shellState) doDeleteProfile(profile string) {
	win := PeekWorkspace(s.Context())
	p := win.Process
	for _, w := range p.Workspaces.UsingProfile(profile) {
		if err := w.SwitchProfile(w.ctx, storage.DefaultProfileName); err != nil {
			w.Notify.Error("Emetos", "Cannot switch to the default profile: "+describe(err))
		}
	}
	if err := p.Profiles.Delete(win.ctx, profile); err != nil {
		win.Notify.Error("Emetos", "Cannot delete the profile '"+profile+"': "+describe(err))
		return
	}
	p.forgetState(profile)
}

// ---- export ----

// exportProfile asks which profile to export (the current one is active), then for the file, and writes the file.
func (s *shellState) exportProfile() {
	win := PeekWorkspace(s.Context())
	list := s.listProfiles()
	if list == nil {
		return
	}
	current := win.Profile()
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select Profile to Export", MatchOnDescription: true, ActiveID: profileItemID(current),
		Items: s.profileItems(list, s.exportProfileTo),
	})
}

// exportProfileTo asks for the file with the dialog of the desktop, on a goroutine, and writes the profile to it
// there. A dialog that is cancelled does nothing.
func (s *shellState) exportProfileTo(profile string) {
	win := PeekWorkspace(s.Context())
	bc := s.Context()
	name := storage.ProfileDisplayName(profile)
	s.profileTask(func(ctx context.Context) (string, error) {
		path, err := chooseProfileExportPath(ctx, bc, name)
		if err != nil || path == "" {
			return "", err
		}
		export, err := win.Process.Profiles.Export(ctx, profile)
		if err != nil {
			return "", err
		}
		if err := storage.WriteProfileExport(ctx, path, export); err != nil {
			return "", err
		}
		return path, nil
	}, func(path string, err error) {
		switch {
		case err != nil:
			win.Process.Logger.Warn("the profile could not be exported", "profile", name, "err", err)
			win.Notify.Error("Emetos", "Cannot export the profile '"+name+"': "+describe(err))
		case path != "":
			win.Notify.Info("Emetos", "Profile '"+name+"' exported to "+filepath.Clean(path))
		}
	})
}

// ---- import ----

// importProfile asks for the file with the dialog of the desktop, on a goroutine, reads it there and goes on with
// the name of the new profile.
func (s *shellState) importProfile() {
	win := PeekWorkspace(s.Context())
	bc := s.Context()
	var read storage.ProfileExport
	s.profileTask(func(ctx context.Context) (string, error) {
		path, err := chooseProfileImportPath(ctx, bc)
		if err != nil || path == "" {
			return "", err
		}
		read, err = storage.ReadProfileExport(ctx, path)
		return path, err
	}, func(path string, err error) {
		switch {
		case err != nil:
			win.Process.Logger.Warn("the profile could not be read", "path", path, "err", err)
			win.Notify.Error("Emetos", "Cannot import the profile: "+describe(err))
		case path != "":
			s.nameImportedProfile(read)
		}
	})
}

// nameImportedProfile makes the profile of an export with its own name, or, when the name is taken (the name Default
// is), asks for another one in an input box that starts with a free name.
func (s *shellState) nameImportedProfile(export storage.ProfileExport) {
	win := PeekWorkspace(s.Context())
	if s.profileNameProblem(export.Name, "") == "" {
		s.doImportProfile(export, export.Name)
		return
	}
	free, err := win.Process.Profiles.FreeName(win.ctx, export.Name)
	if err != nil {
		win.Notify.Error("Emetos", "Cannot import the profile: "+describe(err))
		return
	}
	s.askProfileName(free, "", func(name string) { s.doImportProfile(export, name) })
}

// doImportProfile makes the profile from the export on a goroutine and switches the window to it.
func (s *shellState) doImportProfile(export storage.ProfileExport, name string) {
	win := PeekWorkspace(s.Context())
	s.profileTask(func(ctx context.Context) (string, error) {
		return win.Process.Profiles.Import(ctx, export, name)
	}, func(made string, err error) {
		if err != nil {
			win.Process.Logger.Warn("the profile could not be imported", "profile", name, "err", err)
			win.Notify.Error("Emetos", "Cannot import the profile: "+describe(err))
			return
		}
		s.switchTo(made)
	})
}
