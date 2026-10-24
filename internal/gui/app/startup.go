package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
)

// Which windows open when the application starts (SPEC §10.10 "Which windows open"): the command line, the windows
// that were open when the application last quit (window.restoreWindows), and the windows that have backups of
// unsaved documents.

// startupPlan is the windows to open at the start, the first of them in the first Glutter window and the others
// with the first frame. It is never empty. last is the folder of the window that was active last, which is the
// active one again once all are open ("" when it had none).
//
//	window.restoreWindows   no folder or file on the command line     a folder or a file on the command line
//	preserve                every window that was open                those, and the folder (a new window, unless
//	                                                                  it was open), the files in the active one
//	all                     every window that was open                only the command line
//	folders                 the windows that had a folder             only the command line
//	one                     the window that was active last           only the command line
//	none                    an empty window                           only the command line
//
// The files of a command line with no folder open in a window without a folder, which has its own state. Whatever the
// setting says, the windows that were open and have backups of unsaved documents come back: the hot exit of a window
// that was not restored is not lost.
//
// Ported from VS Code: src/vs/platform/windows/electron-main/windowsMainService.ts (getPathsToOpen,
// doGetRestoredWindows) and the setting window.restoreWindows
func (p *Process) startupPlan(ctx context.Context, opts Options) (plan []WindowSpec, last string) {
	mode := p.restoreModeAtStart(ctx)
	g, err := p.State.LoadGlobal(ctx)
	if err != nil {
		p.Logger.Warn("the global UI state cannot be used; the window starts as new", "err", err)
	}
	if g.LastWindow != nil {
		last = g.LastWindow.Folder
	}
	var files []string
	if opts.Open != nil {
		for _, f := range opts.Open.Files {
			files = append(files, f.Path)
		}
	}
	if len(files) == 0 {
		files = opts.Files
	}
	restored := p.restoredWindows(ctx, g, mode, last)
	hasArgs := opts.Folder != "" || len(files) > 0
	switch {
	case !hasArgs:
		plan = restored
	case mode == "preserve" && opts.Folder != "":
		plan = restored
		plan = append(plan, WindowSpec{Folder: opts.Folder, Files: files, Launch: opts.Open})
		for i, spec := range plan[:len(plan)-1] {
			if sameFolder(spec.Folder, opts.Folder) {
				// the folder was open before: its window gets the files
				plan[i].Files, plan[i].Launch = files, opts.Open
				plan = plan[:len(plan)-1]
				break
			}
		}
		last = opts.Folder
	case mode == "preserve" && len(restored) > 0:
		plan = restored
		at := 0
		for i, spec := range plan {
			if spec.Folder == last {
				at = i
			}
		}
		plan[at].Files, plan[at].Launch = files, opts.Open
	default:
		plan = []WindowSpec{{Folder: opts.Folder, Files: files, Launch: opts.Open}}
		last = opts.Folder
	}
	if len(plan) > 0 && plan[0].Folder == "" && plan[0].StateID == "" {
		plan[0].StateID = p.primaryEmptyID(ctx)
	}
	plan = p.addWindowsWithBackups(ctx, g, plan)
	if len(plan) == 0 {
		plan = []WindowSpec{{StateID: p.primaryEmptyID(ctx)}}
	}
	if opts.Open != nil && !slices.ContainsFunc(plan, func(w WindowSpec) bool { return w.Launch != nil }) {
		plan[0].Launch = opts.Open // the notices of the parts of the command line that come with later steps
	}
	if opts.Open != nil && opts.Open.Profile != "" {
		plan = p.applyLaunchProfile(ctx, plan, opts.Open.Profile, hasArgs)
	}
	for i := range plan {
		if plan[i].Folder == "" && plan[i].StateID == "" && i > 0 {
			if id, err := storage.NewEmptyWindowID(); err == nil {
				plan[i].StateID = id
			}
		}
	}
	return plan, last
}

// applyLaunchProfile gives --profile to the window the command line opens (the one that carries the launch). A
// window that is new, or the first one of a start with nothing restored, uses the profile (made when it is not there
// yet, when the window is made). A window that came back from the last run keeps the profile it had, as the window of
// a running instance does (Windows.HandleLaunch), and says so in a notice when it has the folder of the command line;
// a command line with no folder and no files then opens a new empty window with the profile.
func (p *Process) applyLaunchProfile(ctx context.Context, plan []WindowSpec, name string, hasArgs bool) []WindowSpec {
	at := slices.IndexFunc(plan, func(w WindowSpec) bool { return w.Launch != nil })
	if at < 0 {
		return plan
	}
	spec := &plan[at]
	profile, ok, err := p.Profiles.Lookup(ctx, name)
	if err != nil {
		p.Logger.Warn("the profile of the command line cannot be found", "profile", name, "err", err)
	}
	switch {
	case spec.Profile == "":
		spec.Profile = name
	case ok && storage.ProfileDisplayName(profile) == spec.Profile:
	case hasArgs && spec.Folder != "":
		// the folder is open with another profile: its window keeps it, and the launch says so
	default:
		launch, files := spec.Launch, spec.Files
		spec.Launch, spec.Files = nil, nil
		plan = append(plan, WindowSpec{Profile: name, Launch: launch, Files: files})
	}
	return plan
}

// restoredProfile is the name of the profile a window that comes back from the last run uses: the profile it had,
// as long as it exists, else the default profile.
func (p *Process) restoredProfile(ctx context.Context, profile string) string {
	if profile == storage.DefaultProfile || !p.Profiles.Exists(ctx, profile) {
		return storage.DefaultProfileName
	}
	return profile
}

// primaryEmptyID is the id of the state of the window without a folder that the first run makes; empty when the
// system has no randomness (the window then keeps no state).
func (p *Process) primaryEmptyID(ctx context.Context) string {
	id, err := p.State.EmptyWindowID(ctx)
	if err != nil {
		p.Logger.Warn("the window keeps no UI state", "err", err)
	}
	return id
}

// restoreModeAtStart reads window.restoreWindows from the user's settings file, before any window exists.
func (p *Process) restoreModeAtStart(ctx context.Context) string {
	userPath, err := settings.UserSettingsPath(p.Paths, storage.DefaultProfile)
	if err != nil {
		return "all"
	}
	store := settings.NewStore(p.Settings, settings.StoreOptions{UserPath: userPath})
	if _, err := store.Reload(ctx); err != nil {
		p.Logger.Warn("the settings cannot be read; the windows are restored as usual", "err", err)
	}
	return restoreMode(store)
}

// restoredWindows are the windows window.restoreWindows reopens, from the list of those that were open when the
// application last quit. A window whose folder is gone is left out. Without a list (a state from before the
// windows were kept) the last window is the one.
func (p *Process) restoredWindows(ctx context.Context, g storage.GlobalState, mode, last string) []WindowSpec {
	if mode == "none" {
		return nil
	}
	open := g.OpenWindows
	if len(open) == 0 && g.LastWindow != nil {
		open = []storage.OpenWindow{{Folder: g.LastWindow.Folder}}
	}
	var out []WindowSpec
	seen := map[string]bool{}
	for _, w := range open {
		if w.Folder != "" {
			if info, err := os.Stat(w.Folder); err != nil || !info.IsDir() {
				continue
			}
			if seen[filepath.Clean(w.Folder)] {
				continue
			}
			seen[filepath.Clean(w.Folder)] = true
		} else if mode == "folders" {
			continue
		}
		out = append(out, WindowSpec{Folder: w.Folder, StateID: stateIDOfEmpty(w), Profile: p.restoredProfile(ctx, w.Profile)})
	}
	if mode == "one" && len(out) > 1 {
		pick := out[len(out)-1]
		for _, spec := range out {
			if spec.Folder == last {
				pick = spec
			}
		}
		out = []WindowSpec{pick}
	}
	return out
}

// stateIDOfEmpty is the state id of a window that has no folder; a window with one gets it from the folder.
func stateIDOfEmpty(w storage.OpenWindow) string {
	if w.Folder != "" {
		return ""
	}
	return w.ID
}

// addWindowsWithBackups adds the windows that were open and have backups of unsaved documents to the plan, when
// it does not have them already.
func (p *Process) addWindowsWithBackups(ctx context.Context, g storage.GlobalState, plan []WindowSpec) []WindowSpec {
	has := func(w storage.OpenWindow) bool {
		for _, spec := range plan {
			if w.Folder != "" && sameFolder(spec.Folder, w.Folder) {
				return true
			}
			if w.Folder == "" && spec.Folder == "" && spec.StateID == w.ID {
				return true
			}
		}
		return false
	}
	for _, w := range g.OpenWindows {
		if has(w) {
			continue
		}
		if w.Folder != "" {
			if info, err := os.Stat(w.Folder); err != nil || !info.IsDir() {
				continue
			}
		}
		store, err := storage.NewBackupStore(p.Paths, w.ID, storage.BackupStoreOptions{Logger: p.Logger})
		if err != nil || !store.HasBackups() {
			continue
		}
		plan = append(plan, WindowSpec{Folder: w.Folder, StateID: stateIDOfEmpty(w), Profile: p.restoredProfile(ctx, w.Profile)})
	}
	return plan
}
