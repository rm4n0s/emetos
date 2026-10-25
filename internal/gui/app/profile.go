package app

import (
	"context"
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
)

// The profile of a window (SPEC §10.12, step 8.6). A window uses one profile: the settings.json, the
// keybindings.json and the snippets of that profile are the user's files of the window, and its UI state is kept
// in the state folder of the profile. The services that read those files belong to the window (the settings store,
// the keybindings, the snippet service and the theme service), so two windows with different profiles do not
// meet, and SwitchProfile gives the window another profile in place: every service moves to the files of the other
// profile, the listeners that follow the settings are told what changed, and the editors, documents and groups stay
// as they are.

// Texts of the notices about profiles.
const (
	// ProfileFolderOpenNotice is what a launch with --profile says when the folder is open in a window with
	// another profile; the argument is the name of that profile.
	ProfileFolderOpenNotice = "The folder is already open with the profile '%s'."
	// ProfileWindowNotice is what a launch with --profile says when the window it opens its files in uses
	// another profile (--reuse-window); the argument is the name of that profile.
	ProfileWindowNotice = "The window uses the profile '%s'."
	// ProfileCreateFailedNotice is what is said when the profile of --profile cannot be made; the window uses the
	// default profile.
	ProfileCreateFailedNotice = "Cannot use the profile '%s': %s"
)

// Profile is the profile of the window as storage.Profiles names it: storage.DefaultProfile (the empty string) for
// the default profile. It may be called from any goroutine.
func (w *Workspace) Profile() string {
	w.profileMu.RLock()
	defer w.profileMu.RUnlock()
	return w.profile
}

// ProfileName is the name the profile of the window is shown with: "Default" for the default profile.
func (w *Workspace) ProfileName() string { return storage.ProfileDisplayName(w.Profile()) }

// OnProfileChange calls fn, on the UI thread, with the new profile after SwitchProfile moved the window to it, and
// returns the function that stops it. The status bar item of the profile (step 8.6b) follows the window with it.
func (w *Workspace) OnProfileChange(fn func(profile string)) (stop func()) {
	p := &fn
	w.profileMu.Lock()
	w.profileListeners = append(w.profileListeners, p)
	w.profileMu.Unlock()
	return func() {
		w.profileMu.Lock()
		defer w.profileMu.Unlock()
		if i := slices.Index(w.profileListeners, p); i >= 0 {
			w.profileListeners = slices.Delete(w.profileListeners, i, i+1)
		}
	}
}

// stateStore is the state store of the profile of the window: the store that keeps the state file of the window.
func (w *Workspace) stateStore() *storage.StateStore {
	s, err := w.Process.StateFor(w.Profile())
	if err != nil {
		// the names of the profiles are checked when they are made: this is a broken profile folder
		w.Process.Logger.Warn("the state of the profile cannot be used", "profile", w.Profile(), "err", err)
		return w.Process.State
	}
	return s
}

// SwitchProfile gives the window another profile, in place (SPEC §10.12). name is the name the user gave: "Default"
// or any capitalization of a profile that exists (a profile that does not exist is not made here: the commands that
// switch make it first). Nothing is closed: the editors, their documents and view states, the groups and the layout
// of the window stay, and so do the documents of the settings.json and keybindings.json of the old profile, which
// are plain files now.
//
// What moves to the files of the new profile: the user layer of the settings store (the listeners are told every
// setting whose value changed, so the theme, the fonts, the tab sizes, the auto save and the rest follow at once;
// the settings editor shows the new values), the keybindings of the user (the resolver of the window is replaced, so
// the new keys work and the old ones are gone), the snippet service, the files the watcher follows, and the state
// file of the window (it is written to the state folder of the new profile from now on; the state of the old
// profile is written first and stays there, and nothing is read back: the UI state of the window is not restored).
// The profile of the folder and the list of the open windows are updated, and OnProfileChange is told.
//
// A switch to the profile the window has is nothing. A settings file or a keybindings file of the new profile that
// cannot be read is reported by the return value after the switch is complete: the window uses the defaults for it.
// It must run on the UI thread. The error has the tags of Profiles.Lookup, of the paths of the profile and of settings.Store.Reload.
func (w *Workspace) SwitchProfile(ctx context.Context, name string) error {
	p := w.Process
	profile, ok, err := p.Profiles.Lookup(ctx, name)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("ProfileSwitchNotFound", "there is no such profile", "profile", name)
	}
	if profile == w.Profile() {
		return nil
	}
	return w.moveToProfile(ctx, profile, true)
}

// profileRenamed moves the window to the profile newName, which is its profile under another name: the folders
// of the profile were renamed (storage.Profiles.Rename), so the files the window follows are at new paths. It is
// SwitchProfile that does not write the state of the window first (the old state folder is gone; the caller wrote
// it before the rename) and tells the observers of OnProfileChange. The error is that of SwitchProfile.
func (w *Workspace) profileRenamed(ctx context.Context, newName string) error {
	return w.moveToProfile(ctx, newName, false)
}

// moveToProfile is the switch of SwitchProfile to a profile that exists and that the window does not have under
// that name; flush says that the state of the window is written to the folder of the old profile first.
func (w *Workspace) moveToProfile(ctx context.Context, profile string, flush bool) error {
	p := w.Process
	userPath, err := settings.UserSettingsPath(p.Paths, profile)
	if err != nil {
		return err
	}
	keysPath, err := keybinding.UserKeybindingsPath(p.Paths, profile)
	if err != nil {
		return err
	}
	snippetsDir, err := snippetsDirOf(p.Paths, profile)
	if err != nil {
		return err
	}
	configDir, err := p.Paths.ConfigDir(profile)
	if err != nil {
		return err
	}

	// the state file of the old profile is up to date before the window leaves it
	if flush && w.flushState != nil {
		w.flushState()
	}
	w.profileMu.Lock()
	w.profile = profile
	w.profileMu.Unlock()

	// the files the watcher follows, and the folder it watches
	st := &w.watch
	st.mu.Lock()
	st.userSettings, st.userKeybindings, st.userDir = userPath, keysPath, configDir
	st.mu.Unlock()
	go w.watchUserFolder()

	var first error
	keep := func(err error) {
		if err != nil {
			p.Logger.Warn("the profile has a file that cannot be read", "profile", w.ProfileName(), "err", err)
			if first == nil {
				first = err
			}
		}
	}
	w.userKeys.setPath(keysPath)
	keep(w.Snippets.SetDir(ctx, snippetsDir))
	// the listeners of the store hear what changed: the theme, the editors, the auto save, ...
	_, err = w.Settings.SetUserPath(ctx, userPath)
	keep(err)
	// the documents that show the settings files of the new profile are the ones read, not those of the old one
	w.settingsDocs.rescan()
	w.userKeys.reload(ctx, false)

	if w.Folder != "" {
		if err := p.Profiles.SetLastProfile(ctx, w.Folder, profile); err != nil {
			p.Logger.Warn("the profile of the folder cannot be noted", "folder", w.Folder, "err", err)
		}
	}
	p.Workspaces.folderNote(w)
	p.Workspaces.syncTabs()
	if s := w.shell; s != nil && s.Mounted() {
		s.stateChanged() // the state of the window is written to the folder of the new profile
	}
	w.profileMu.RLock()
	listeners := slices.Clone(w.profileListeners)
	w.profileMu.RUnlock()
	for _, f := range listeners {
		(*f)(profile)
	}
	return first
}

// UsingProfile lists the windows that use a profile (as storage.Profiles names it), in the order the windows were
// made. The commands that delete or rename a profile refuse while there are some (step 8.6b).
func (m *Workspaces) UsingProfile(profile string) []*Workspace {
	var out []*Workspace
	for _, w := range m.All() {
		if w.Profile() == profile {
			out = append(out, w)
		}
	}
	return out
}

// workspaceProfile works out the profile a new window opens with, from the spec: the profile the spec names (made empty
// when there is none, as --profile does), else the profile last used with its folder, else the default profile. A
// profile that cannot be made leaves the default profile, and the notice says so in the window once it exists.
func (m *Workspaces) workspaceProfile(ctx context.Context, spec WorkspaceSpec) (profile string, problem error) {
	p := m.p
	if spec.Profile != "" {
		profile, created, err := p.Profiles.Ensure(ctx, spec.Profile)
		if err != nil {
			p.Logger.Warn("the profile of a window cannot be used", "profile", spec.Profile, "err", err)
			return storage.DefaultProfile, err
		}
		if created {
			p.Logger.Info("a profile was made", "profile", profile)
		}
		return profile, nil
	}
	if spec.Folder != "" {
		profile, ok, err := p.Profiles.LastProfile(ctx, spec.Folder)
		if err != nil {
			p.Logger.Warn("the profile of a folder cannot be found", "folder", spec.Folder, "err", err)
		}
		if ok {
			return profile, nil
		}
	}
	return storage.DefaultProfile, nil
}
