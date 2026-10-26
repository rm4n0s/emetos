package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// Which tabs open when the application starts (SPEC §10.10 "Which windows open", §10.14): the command line, the tabs
// that were open when the application last quit (window.restoreWindows), and the tabs that have backups of unsaved
// documents.

// startupPlan is the tabs to open at the start, in the order of the strip; the one at active is shown. It is never
// empty.
//
//	window.restoreWindows   no folder or file on the command line     a folder or a file on the command line
//	preserve                every tab that was open                   those, and the folder (a new tab, unless
//	                                                                  it was open), the files in the active one
//	all                     every tab that was open                   only the command line
//	folders                 the tabs that had a folder                only the command line
//	one                     the tab that was active last              only the command line
//	none                    a No Folder tab                           only the command line
//
// The files of a command line with no folder open in a tab without a folder, which has its own state. Whatever the
// setting says, the tabs that were open and have backups of unsaved documents come back: the hot exit of a tab
// that was not restored is not lost.
//
// Ported from VS Code: src/vs/platform/windows/electron-main/windowsMainService.ts (getPathsToOpen,
// doGetRestoredWindows) and the setting window.restoreWindows
func (p *Process) startupPlan(ctx context.Context, opts Options) (plan []WorkspaceSpec, active int) {
	mode := p.restoreModeAtStart(ctx)
	g, err := p.State.LoadGlobal(ctx)
	if err != nil {
		p.Logger.Warn("the global UI state cannot be used; the tabs start as new", "err", err)
	}
	last := ""
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
	restored := p.restoredTabs(ctx, g, mode, last)
	hasArgs := opts.Folder != "" || len(files) > 0
	switch {
	case !hasArgs:
		plan = restored
	case mode == "preserve" && opts.Folder != "":
		plan = restored
		plan = append(plan, WorkspaceSpec{Folder: opts.Folder, Files: files, Launch: opts.Open})
		for i, spec := range plan[:len(plan)-1] {
			if sameFolder(spec.Folder, opts.Folder) {
				// the folder was open before: its tab gets the files
				plan[i].Files, plan[i].Launch = files, opts.Open
				plan = plan[:len(plan)-1]
				break
			}
		}
	case mode == "preserve" && len(restored) > 0:
		plan = restored
		at := activeIndex(plan, g, Options{}, last)
		if plan[at].Extension != nil {
			// the files go to a workspace: the first one that came back, else a tab of its own
			if at = slices.IndexFunc(plan, func(w WorkspaceSpec) bool { return w.Extension == nil }); at < 0 {
				plan = append(plan, WorkspaceSpec{})
				at = len(plan) - 1
			}
		}
		plan[at].Files, plan[at].Launch = files, opts.Open
	default:
		plan = []WorkspaceSpec{{Folder: opts.Folder, Files: files, Launch: opts.Open}}
	}
	if len(plan) > 0 && plan[0].Folder == "" && plan[0].StateID == "" && plan[0].Extension == nil {
		plan[0].StateID = p.primaryEmptyID(ctx)
	}
	plan = p.addTabsWithBackups(ctx, g, plan)
	if len(plan) == 0 {
		plan = []WorkspaceSpec{{StateID: p.primaryEmptyID(ctx)}}
	}
	if opts.Open != nil && !slices.ContainsFunc(plan, func(w WorkspaceSpec) bool { return w.Launch != nil }) {
		// the notices of the parts of the command line that come with later steps
		if at := slices.IndexFunc(plan, func(w WorkspaceSpec) bool { return w.Extension == nil }); at >= 0 {
			plan[at].Launch = opts.Open
		}
	}
	if opts.Open != nil && opts.Open.Profile != "" {
		plan = p.applyLaunchProfile(ctx, plan, opts.Open.Profile, hasArgs)
	}
	for i := range plan {
		if plan[i].Folder == "" && plan[i].StateID == "" && plan[i].Extension == nil && i > 0 {
			if id, err := storage.NewEmptyWindowID(); err == nil {
				plan[i].StateID = id
			}
		}
	}
	return plan, activeIndex(plan, g, opts, last)
}

// activeIndex is the place in the plan of the tab that is shown first: the one that has the folder of the command line,
// else the one that was active when the application quit, else the one of the last folder, else the first.
func activeIndex(plan []WorkspaceSpec, g storage.GlobalState, opts Options, last string) int {
	if opts.Folder != "" {
		if i := slices.IndexFunc(plan, func(s WorkspaceSpec) bool { return sameFolder(s.Folder, opts.Folder) }); i >= 0 {
			return i
		}
	}
	if g.ActiveWorkspaceTab != "" {
		if i := slices.IndexFunc(plan, func(s WorkspaceSpec) bool { return s.TabID == g.ActiveWorkspaceTab }); i >= 0 {
			return i
		}
	}
	if i := slices.IndexFunc(plan, func(s WorkspaceSpec) bool { return s.Extension == nil && s.Folder == last }); i >= 0 {
		return i
	}
	return 0
}

// applyLaunchProfile gives --profile to the window the command line opens (the one that carries the launch). A
// window that is new, or the first one of a start with nothing restored, uses the profile (made when it is not there
// yet, when the window is made). A window that came back from the last run keeps the profile it had, as the window of
// a running instance does (Workspaces.HandleLaunch), and says so in a notice when it has the folder of the command line;
// a command line with no folder and no files then opens a new empty window with the profile.
func (p *Process) applyLaunchProfile(ctx context.Context, plan []WorkspaceSpec, name string, hasArgs bool) []WorkspaceSpec {
	at := slices.IndexFunc(plan, func(w WorkspaceSpec) bool { return w.Launch != nil })
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
		plan = append(plan, WorkspaceSpec{Profile: name, Launch: launch, Files: files})
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

// restoredTabs are the tabs window.restoreWindows reopens, from the list of those that were open when the
// application last quit. A tab whose folder is gone is left out. Without a list (a state from before the windows were
// kept) the last window is the one.
func (p *Process) restoredTabs(ctx context.Context, g storage.GlobalState, mode, last string) []WorkspaceSpec {
	if mode == "none" {
		return nil
	}
	open := g.WorkspaceTabs
	if len(open) == 0 && g.LastWindow != nil {
		open = []storage.WorkspaceTab{{Folder: g.LastWindow.Folder}}
	}
	var out []WorkspaceSpec
	seen := map[string]bool{}
	for _, t := range open {
		if t.Kind != "" && t.Kind != storage.TabKindWorkspace {
			// the tab of an extension comes back with its title and state, unless no extension has its kind any more
			if mode == "folders" {
				continue
			}
			if _, ok := p.windowTabKind(t.Kind); !ok {
				p.Logger.Info("a window tab of an extension is left out: no extension has its kind", "kind", t.Kind)
				continue
			}
			out = append(out, WorkspaceSpec{TabID: t.ID, Extension: &ExtensionTabSpec{Kind: t.Kind, Title: t.Title, State: t.State}})
			continue
		}
		if t.Folder != "" {
			if info, err := os.Stat(t.Folder); err != nil || !info.IsDir() {
				continue
			}
			if seen[filepath.Clean(t.Folder)] {
				continue
			}
			seen[filepath.Clean(t.Folder)] = true
		} else if mode == "folders" {
			continue
		}
		out = append(out, WorkspaceSpec{TabID: t.ID, Folder: t.Folder, StateID: stateIDOfEmpty(t), Profile: p.restoredProfile(ctx, t.Profile)})
	}
	if mode == "one" && len(out) > 1 {
		pick := out[len(out)-1]
		for _, spec := range out {
			if g.ActiveWorkspaceTab != "" && spec.TabID == g.ActiveWorkspaceTab {
				pick = spec
				break
			}
			if g.ActiveWorkspaceTab == "" && spec.Extension == nil && spec.Folder == last {
				pick = spec
			}
		}
		out = []WorkspaceSpec{pick}
	}
	return out
}

// stateIDOfEmpty is the state id of a tab that has no folder; a tab with one gets it from the folder.
func stateIDOfEmpty(t storage.WorkspaceTab) string {
	if t.Folder != "" {
		return ""
	}
	return t.StateID
}

// addTabsWithBackups adds the tabs that were open and have backups of unsaved documents to the plan, when
// it does not have them already.
func (p *Process) addTabsWithBackups(ctx context.Context, g storage.GlobalState, plan []WorkspaceSpec) []WorkspaceSpec {
	has := func(t storage.WorkspaceTab) bool {
		for _, spec := range plan {
			if t.Folder != "" && sameFolder(spec.Folder, t.Folder) {
				return true
			}
			if t.Folder == "" && spec.Folder == "" && spec.StateID == t.StateID {
				return true
			}
		}
		return false
	}
	for _, t := range g.WorkspaceTabs {
		if has(t) || t.StateID == "" {
			continue
		}
		if t.Folder != "" {
			if info, err := os.Stat(t.Folder); err != nil || !info.IsDir() {
				continue
			}
		}
		store, err := storage.NewBackupStore(p.Paths, t.StateID, storage.BackupStoreOptions{Logger: p.Logger})
		if err != nil || !store.HasBackups() {
			continue
		}
		plan = append(plan, WorkspaceSpec{TabID: t.ID, Folder: t.Folder, StateID: stateIDOfEmpty(t), Profile: p.restoredProfile(ctx, t.Profile)})
	}
	return plan
}

// windowTabKind finds the kind of window tab of an extension by its id.
func (p *Process) windowTabKind(kindID string) (exthost.WindowTabKindInfo, bool) {
	if p.Extensions == nil {
		return exthost.WindowTabKindInfo{}, false
	}
	return p.Extensions.WindowTabKind(kindID)
}
