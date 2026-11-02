package debug

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/rm4n0s/errors"
)

// LaunchKind says where a Launch comes from.
type LaunchKind int

const (
	// LaunchFolder is <folder>/.emetos/launch.json.
	LaunchFolder LaunchKind = iota + 1
	// LaunchWorkspace is the launch key of the workspace settings (<folder>/.emetos/settings.json).
	LaunchWorkspace
	// LaunchUser is the launch key of the user settings; it is hidden from "Add Configuration".
	LaunchUser
)

// Launch is a source of launch configurations: a launch file or the launch key of a settings file. It holds what was read; the
// Launches that owns it reads again when told.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugConfigurationManager.ts (AbstractLaunch, Launch, WorkspaceLaunch,
// UserLaunch)
type Launch struct {
	// Kind is where the configurations come from.
	Kind LaunchKind
	// Folder is the folder of a LaunchFolder, and the folder the other kinds belong to.
	Folder string
	// Name is the folder's name, "workspace" or "user settings".
	Name string
	// Path is the file that holds the configurations: launch.json or the settings file.
	Path string
	// Found is false for a launch file that does not exist.
	Found bool
	// File is what was read.
	File LaunchFile
	// Problems are what is wrong in the file, with the places.
	Problems []Problem
}

// Key identifies the launch in the saved state: the folder path for a folder, "workspace" or "user".
func (l *Launch) Key() string {
	switch l.Kind {
	case LaunchWorkspace:
		return "workspace"
	case LaunchUser:
		return "user"
	}
	return "folder:" + filepath.Clean(l.Folder)
}

// Hidden is true for the launch of the user settings.
func (l *Launch) Hidden() bool { return l.Kind == LaunchUser }

// dedup returns the configurations and compounds that have a string name, the repeated names made unique.
func (l *Launch) dedup() (cfgs []Config, compounds []Compound) {
	var names []string
	var kept []Config
	for _, c := range l.File.Configurations {
		if _, ok := c["name"].(string); ok && c != nil {
			kept = append(kept, c)
			names = append(names, c.Name())
		}
	}
	for i, n := range distinguishByName(names) {
		c := kept[i].Clone()
		c["name"] = n
		cfgs = append(cfgs, c)
	}
	names = names[:0]
	var keptC []Compound
	for _, c := range l.File.Compounds {
		if c.Name != "" {
			keptC = append(keptC, c)
			names = append(names, c.Name)
		}
	}
	for i, n := range distinguishByName(names) {
		c := keptC[i]
		c.Name = n
		compounds = append(compounds, c)
	}
	return cfgs, compounds
}

// ConfigurationNames lists the names shown in the dropdown: the configurations and the compounds that have configurations, the
// hidden ones left out and the others ordered by their presentation. With ignoreCompoundsAndPresentation only the names of the
// configurations, in the order of the file.
func (l *Launch) ConfigurationNames(ignoreCompoundsAndPresentation bool) []string {
	cfgs, compounds := l.dedup()
	if ignoreCompoundsAndPresentation {
		out := make([]string, 0, len(cfgs))
		for _, c := range cfgs {
			out = append(out, c.Name())
		}
		return out
	}
	type item struct {
		name string
		p    *Presentation
	}
	var items []item
	for _, c := range cfgs {
		items = append(items, item{c.Name(), effectiveForPlatform(c).Presentation()})
	}
	for _, c := range compounds {
		if len(c.Configurations) > 0 {
			items = append(items, item{c.Name, c.Presentation})
		}
	}
	items = visibleAndSorted(items, func(i item) *Presentation { return i.p })
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.name)
	}
	return out
}

// Configuration returns a copy of the configuration with a name, with the attributes of the platform applied, or false.
func (l *Launch) Configuration(name string) (Config, bool) {
	cfgs, _ := l.dedup()
	for _, c := range cfgs {
		if c.Name() == name {
			return effectiveForPlatform(c), true
		}
	}
	return nil, false
}

// Compound returns the compound with a name.
func (l *Launch) Compound(name string) (Compound, bool) {
	_, compounds := l.dedup()
	for _, c := range compounds {
		if c.Name == name {
			return c, true
		}
	}
	return Compound{}, false
}

// Inputs are the inputs the file defines.
func (l *Launch) Inputs() []Input { return l.File.Inputs }

// LaunchSources tell Launches.Reload where the launches are. The window reads the settings; the launch file of the folder is read
// here.
type LaunchSources struct {
	// Folder is the folder of the window, "" when none is open (then there is no launch file).
	Folder string
	// FolderName is the name of the folder; empty uses its base name.
	FolderName string
	// WorkspaceSetting and UserSetting are the values of the launch key in the workspace and the user settings (nil: none), and
	// WorkspaceSettingsPath and UserSettingsPath the files that hold them.
	WorkspaceSetting, UserSetting           any
	WorkspaceSettingsPath, UserSettingsPath string
}

// DynamicSelection is a dynamic configuration the selection can point at: provided by a debugger, not written in a launch file.
type DynamicSelection struct {
	// Type is the debug type whose provider gave it.
	Type string
	// Name is its name.
	Name string
}

// Selected is the selected configuration of the dropdown.
type Selected struct {
	// Launch is the launch it belongs to, nil when there is none.
	Launch *Launch
	// Name is the name of the configuration, empty when there is none.
	Name string
	// Type is the debug type of the selected configuration (the context key debugConfigurationType).
	Type string
	// Dynamic is true for a configuration a provider gave.
	Dynamic bool
	// Config is the configuration when it was given with the selection (a dynamic one); nil otherwise.
	Config Config
}

// Launches holds the launches of the window and the selected configuration (VS Code's ConfigurationManager).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugConfigurationManager.ts (ConfigurationManager)
type Launches struct {
	mu       sync.Mutex
	svc      *Service
	launches []*Launch
	selected Selected
	want     *stateSelection
	recent   []DynamicSelection
}

func newLaunches(svc *Service) *Launches { return &Launches{svc: svc} }

// Launches returns the launches: the folder's launch.json, the workspace settings, the user settings.
func (l *Launches) Launches() []*Launch {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*Launch(nil), l.launches...)
}

// Launch returns the launch with a key (Launch.Key), or the launch of a folder for a folder path, or nil.
func (l *Launches) Launch(folderOrKey string) *Launch {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, x := range l.launches {
		if x.Key() == folderOrKey || (x.Kind == LaunchFolder && filepath.Clean(x.Folder) == filepath.Clean(folderOrKey) && folderOrKey != "") {
			return x
		}
	}
	return nil
}

// Reload reads the launch file of the folder and takes the settings values, makes the launches again, and keeps the selected
// configuration when it is still there (else it picks the first). It fails with the tag of ReadLaunchFile.
func (l *Launches) Reload(ctx context.Context, src LaunchSources) error {
	var list []*Launch
	if src.Folder != "" {
		name := src.FolderName
		if name == "" {
			name = filepath.Base(src.Folder)
		}
		f, problems, found, err := ReadLaunchFile(ctx, src.Folder)
		if err != nil {
			return err
		}
		list = append(list, &Launch{Kind: LaunchFolder, Folder: src.Folder, Name: name, Path: LaunchFilePath(src.Folder), Found: found, File: f, Problems: problems})
		if src.WorkspaceSetting != nil {
			wf, wp := LaunchFromSetting(src.WorkspaceSetting)
			list = append(list, &Launch{Kind: LaunchWorkspace, Folder: src.Folder, Name: "workspace", Path: src.WorkspaceSettingsPath, Found: true, File: wf, Problems: wp})
		}
	}
	uf, up := LaunchFromSetting(src.UserSetting)
	if src.UserSetting == nil {
		uf, up = LaunchFile{}, nil
	}
	list = append(list, &Launch{Kind: LaunchUser, Name: "user settings", Path: src.UserSettingsPath, Found: src.UserSetting != nil, File: uf, Problems: up})

	l.mu.Lock()
	prev := l.selected
	l.launches = list
	l.mu.Unlock()
	l.mu.Lock()
	wanted := l.want != nil
	l.mu.Unlock()
	if wanted {
		l.applyWanted()
	} else {
		l.selectKeeping(prev)
	}
	l.svc.model.emit(Event{Kind: EventConfiguration})
	return nil
}

// selectKeeping selects again after the launches changed: the previous launch and name when they are still there.
func (l *Launches) selectKeeping(prev Selected) {
	var launch *Launch
	if prev.Launch != nil {
		launch = l.Launch(prev.Launch.Key())
	}
	if prev.Dynamic {
		l.Select(launch, prev.Name, SelectOptions{Dynamic: &DynamicSelection{Type: prev.Type, Name: prev.Name}, Config: prev.Config})
		return
	}
	l.Select(launch, prev.Name, SelectOptions{})
}

// SelectOptions are the optional parts of a selection.
type SelectOptions struct {
	// Config is the configuration for a dynamic selection (the one the user picked).
	Config Config
	// Dynamic marks the selection as a dynamic configuration of a provider.
	Dynamic *DynamicSelection
}

// Select selects a configuration of a launch. A nil launch picks the first launch that has configurations; a name that is not in
// it selects the first configuration (or none).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugConfigurationManager.ts (selectConfiguration)
func (l *Launches) Select(launch *Launch, name string, opts SelectOptions) {
	l.mu.Lock()
	if launch == nil {
		for _, x := range l.launches {
			if len(x.ConfigurationNames(false)) > 0 {
				launch = x
				break
			}
		}
		if launch == nil && len(l.launches) > 0 {
			launch = l.launches[0]
		}
	}
	prev := l.selected
	var names []string
	if launch != nil {
		names = launch.ConfigurationNames(false)
	}
	sel := Selected{Launch: launch}
	switch {
	case name != "" && contains(names, name):
		sel.Name = name
	case opts.Dynamic != nil && opts.Dynamic.Type != "":
		sel.Name, sel.Dynamic, sel.Type, sel.Config = name, true, opts.Dynamic.Type, opts.Config
		l.addRecentLocked(DynamicSelection{Type: opts.Dynamic.Type, Name: name})
	case prev.Name != "" && prev.Launch == launch && contains(names, prev.Name):
		sel.Name = prev.Name
	case len(names) > 0:
		sel.Name = names[0]
	}
	if !sel.Dynamic && launch != nil && sel.Name != "" {
		if c, ok := launch.Configuration(sel.Name); ok {
			sel.Type = c.Type()
		}
	}
	l.selected = sel
	changed := sel.Launch != prev.Launch || sel.Name != prev.Name || sel.Dynamic != prev.Dynamic
	l.mu.Unlock()
	if changed {
		l.svc.model.emit(Event{Kind: EventConfiguration})
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (l *Launches) addRecentLocked(d DynamicSelection) {
	out := []DynamicSelection{d}
	for _, r := range l.recent {
		if r != d {
			out = append(out, r)
		}
	}
	l.recent = out
}

// Selected returns the selected configuration.
func (l *Launches) Selected() Selected {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.selected
}

// RecentDynamic lists the dynamic configurations used lately, the newest first.
func (l *Launches) RecentDynamic() []DynamicSelection {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]DynamicSelection(nil), l.recent...)
}

// RemoveRecentDynamic forgets a recent dynamic configuration; when it is the selected one the selection moves on.
func (l *Launches) RemoveRecentDynamic(name, typ string) {
	l.mu.Lock()
	var kept []DynamicSelection
	for _, r := range l.recent {
		if r.Name != name || r.Type != typ {
			kept = append(kept, r)
		}
	}
	l.recent = kept
	reselect := l.selected.Dynamic && l.selected.Name == name && l.selected.Type == typ
	l.mu.Unlock()
	if reselect {
		l.Select(nil, "", SelectOptions{})
		return
	}
	l.svc.model.emit(Event{Kind: EventConfiguration})
}

// All lists every configuration and compound the dropdown shows, with the launch it is in.
func (l *Launches) All() []struct {
	Launch *Launch
	Name   string
} {
	var out []struct {
		Launch *Launch
		Name   string
	}
	for _, x := range l.Launches() {
		for _, n := range x.ConfigurationNames(false) {
			out = append(out, struct {
				Launch *Launch
				Name   string
			}{x, n})
		}
	}
	return out
}

// SelectedConfig returns the selected configuration: the one in the launch file, or, for a dynamic selection, the one given with
// it or the one the provider gives again under that name.
func (l *Launches) SelectedConfig(ctx context.Context) (Config, error) {
	sel := l.Selected()
	if sel.Name == "" {
		return nil, nil
	}
	if !sel.Dynamic && sel.Launch != nil {
		c, _ := sel.Launch.Configuration(sel.Name)
		return c, nil
	}
	if sel.Config != nil {
		return sel.Config.Clone(), nil
	}
	if l.svc.opts.DynamicConfigurations == nil || sel.Type == "" {
		return nil, nil
	}
	folder := ""
	if sel.Launch != nil {
		folder = sel.Launch.Folder
	}
	cfgs, err := l.svc.opts.DynamicConfigurations(ctx, folder, sel.Type)
	if err != nil {
		return nil, err
	}
	for _, c := range cfgs {
		if c.Name() == sel.Name {
			return c, nil
		}
	}
	return nil, nil
}

// ResolveByProviders runs the resolveDebugConfiguration of the providers of the configuration's type and then of "*", again while
// a provider changes the type (each type once), as VS Code does. A nil configuration with a nil error means a provider cancelled
// the launch; ErrOpenLaunchFile means it asks for launch.json to be shown.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugConfigurationManager.ts (resolveConfigurationByProviders)
func (l *Launches) ResolveByProviders(ctx context.Context, folder, typ string, cfg Config) (Config, error) {
	fn := l.svc.opts.ResolveConfiguration
	if fn == nil {
		return cfg, nil
	}
	resolvedType := cfg.Type()
	if resolvedType == "" {
		resolvedType = typ
	}
	result := cfg
	seen := map[string]bool{}
	for result != nil && !seen[resolvedType] {
		seen[resolvedType] = true
		var err error
		for _, t := range []string{resolvedType, "*"} {
			if t == "*" && resolvedType == "*" {
				continue
			}
			result, err = fn(ctx, folder, t, result)
			if err != nil {
				return nil, err
			}
			if result == nil {
				return nil, nil
			}
		}
		if t := result.Type(); t != "" {
			resolvedType = t
		} else {
			resolvedType = typ
		}
	}
	return result, nil
}

// ErrOpenLaunchFile is what a configuration provider returns to cancel the launch and have launch.json shown (VS Code's null result).
var ErrOpenLaunchFile = &errors.Error{Tag: "DebugOpenLaunchFile"}
