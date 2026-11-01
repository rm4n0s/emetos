// Package langserver is what the language packs share to find, start and show a language server (SPEC §10.34 "What every pack
// gets", D47): the finder (the path setting of the pack, the folders it names before and after PATH, a version check), the root
// of the project (the nearest folder with a marker such as Cargo.toml), the options the server starts with and the answers to
// workspace/configuration built from the settings and rebuilt when they change, the status bar item with its menu, the
// notification for a missing server (How to Install, Copy Install Command, Set Path...), and the settings of the pack that name
// its programs. extensions/golang keeps its own code (SPEC §10.17); the other packs describe their server with a Config and the
// pack's extension calls Attach.
//
// The host runs the server (SPEC §9.12): the pack declares it in its manifest with Config.Spec, which has OwnUI, so that the host
// leaves the status item and the "not found" notification to this package, and gives the host a ServerLauncher with Attach, which
// finds the program at each start instead of the fixed Program and PathSetting of the spec. The launcher runs on a goroutine of
// the host; everything else in this package runs on the UI thread, as the extension API asks.
//
// A pack uses it through langpack.Spec.Server:
//
//	langpack.New(langpack.Spec{ID: "emetos.rust", ..., Server: &langserver.Config{
//		ID: "rust-analyzer", Name: "rust-analyzer", Languages: []string{"rust"}, Category: "Rust",
//		Programs: []langserver.Program{{Name: "rust-analyzer", PathSetting: "rust-analyzer.server.path", After: []string{"~/.cargo/bin"}}},
//		RootMarkers: []string{"Cargo.toml"},
//	}})
package langserver

import (
	"encoding/json"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// Config describes the language server of a pack.
type Config struct {
	// ID is the id of the server in the manifest of the pack (rust-analyzer); Name the name shown to the user (the status item,
	// the Output channel, the notifications).
	ID, Name string
	// Languages are the ids of the languages the server serves; the status item shows while an editor of one of them is in front.
	Languages []string
	// Category is the category of the commands of the pack in the command palette ("Rust"); the name of the pack when empty.
	Category string
	// Purpose says in a few words what the server gives (the language features of Rust), for the dialog How to Install.
	Purpose string
	// Programs are the programs that can be the server, the first by default: the pack with one server has one, the Python pack
	// has basedpyright, pyright and pylsp. Select chooses among them. At least one is needed.
	Programs []Program
	// Select returns the index in Programs of the program to run now, from the settings (python.languageServer); the first when
	// nil or when the index is out of range. The settings it reads are watched: a change restarts the server.
	Select func(s *Settings) int
	// RootMarkers are the names (globs allowed: *.sln) of the files that mark a project root: Cargo.toml, pom.xml. The server is
	// started in the folder if it holds one, else in the nearest folder above it that does, as far as the repository or the
	// user's home folder goes (ResolveRoot). Without markers it is started in the folder.
	RootMarkers []string
	// Customize is called at each start after the program was found and passed its version check; it sets the arguments, the
	// environment, the initialization options and the answers to workspace/configuration of the launch from the settings it reads.
	// The settings it reads are watched: a change restarts the server. An error leaves the server missing, with the error in
	// the Output channel.
	Customize func(l *Launch, env Env) error
	// TraceKey is the setting that traces the traffic with the server (rust-analyzer.trace.server): "<pack id>.<id>.trace.server"
	// when empty (extension.LanguageServerSpec.TraceKey).
	TraceKey string
	// ConfigurationSection and WatchedFiles are those of extension.LanguageServerSpec.
	ConfigurationSection string
	WatchedFiles         []string
	// SettingsQuery is what Open Settings of the menu searches for (rust-analyzer); the name of the server when empty.
	SettingsQuery string
	// Menu are the entries the pack adds to the menu of the status item, after Restart, Show Output and Open Settings.
	Menu []MenuEntry
}

// MenuEntry is an entry of the menu of the status item.
type MenuEntry struct {
	// Label is the text, a codicon may be written $(name); Description is shown after it.
	Label, Description string
	// Run is called, on the UI thread, when the entry is chosen.
	Run func()
}

// Program is a program that can be the language server, and where to look for it.
type Program struct {
	// Name is the name shown in messages ("basedpyright"); Exe is the name of the file looked for, Name when empty.
	Name, Exe string
	// Args are the arguments it is started with (--stdio).
	Args []string
	// PathSetting is the setting, written by the user, that holds the path of the program (rust-analyzer.server.path); the pack
	// declares it with Config.SettingSpecs. A value with a slash is the path itself (a relative one is relative to the folder);
	// one without is a name looked for like Exe. It is not searched further when it is set and wrong (ServerPathInvalid).
	PathSetting string
	// Before and After are folders searched before and after PATH, in order: "${workspaceFolder}/node_modules/.bin" before,
	// "~/.cargo/bin" after. ~ and ${userHome} are the home folder, ${workspaceFolder} the folder of the tab, ${env:NAME} a
	// variable of Emetos's environment.
	Before, After []string
	// Dirs gives more folders from the settings and the folder (the bin folder of the interpreter), searched before the Before
	// folders and after the After folders respectively. It may be nil.
	Dirs func(env Env) (before, after []string)
	// Find replaces the whole search when it is not nil (jdtls is a folder with a launcher jar, not a program on PATH). It may
	// call FindExecutable. Its error is returned as it is.
	Find func(env Env) (Found, error)
	// Version says how to ask the program for its version, and the oldest that is accepted.
	Version VersionCheck
	// Install says how to get the program.
	Install Install
}

// exe is the name of the file looked for.
func (p Program) exe() string {
	if p.Exe != "" {
		return p.Exe
	}
	return p.Name
}

// Launch is a start of the server in the making: what Customize changes.
type Launch struct {
	// Found is the program that was found, Version its version ("" when it could not be read) and Program the entry of
	// Config.Programs it is.
	Found   Found
	Version string
	Program Program
	// VersionErr says why the version could not be read; nil when it was or when the program has no way to tell.
	VersionErr error
	// Root is the folder the server works on (ResolveRoot of the folder of the tab).
	Root string
	// Args and Env are the arguments and the extra environment variables of the program; Args start as Program.Args.
	Args, Env []string
	// InitializationOptions are sent in initialize (nil for none).
	InitializationOptions any
	// Configuration are the answers to workspace/configuration, by section ("python": {...}); a section below one of them
	// ("python.analysis") is answered from inside it. A section that is not in it is answered from the settings of the tab.
	Configuration map[string]any
	// ClientExperimental is the "experimental" member of the client capabilities the server is initialized with (nil for none).
	ClientExperimental map[string]any
}

// serverLaunch is the answer for the host.
func (l *Launch) serverLaunch() extension.ServerLaunch {
	out := extension.ServerLaunch{
		Program: l.Found.Path, Args: l.Args, Env: l.Env, Root: l.Root, InitializationOptions: l.InitializationOptions,
		ClientExperimental: l.ClientExperimental,
	}
	if len(l.Configuration) > 0 {
		cfg := l.Configuration
		out.Configuration = func(section string) (json.RawMessage, bool) {
			v, ok := sectionOf(cfg, section)
			if !ok {
				return nil, false
			}
			b, err := json.Marshal(v)
			if err != nil {
				return nil, false
			}
			return b, true
		}
	}
	return out
}

// sectionOf finds a dotted section in a map: the key itself, or a path of keys through nested maps.
func sectionOf(m map[string]any, section string) (any, bool) {
	if section == "" {
		return nil, false
	}
	if v, ok := m[section]; ok {
		return v, true
	}
	parts := strings.Split(section, ".")
	for i := len(parts) - 1; i > 0; i-- {
		head := strings.Join(parts[:i], ".")
		v, ok := m[head]
		if !ok {
			continue
		}
		cur := v
		for _, p := range parts[i:] {
			obj, isObj := cur.(map[string]any)
			if !isObj {
				return nil, false
			}
			if cur, ok = obj[p]; !ok {
				return nil, false
			}
		}
		return cur, true
	}
	return nil, false
}

// Spec is the declaration of the server for the manifest of the pack: the host does not look for the program (Attach gives it a
// launcher), and leaves the status item and the notification for a missing program to this package.
func (c Config) Spec() extension.LanguageServerSpec {
	program := ""
	var args []string
	if len(c.Programs) > 0 {
		program, args = c.Programs[0].exe(), c.Programs[0].Args
	}
	return extension.LanguageServerSpec{
		ID: c.ID, Name: c.Name, Languages: c.Languages, Program: program, Args: args,
		ConfigurationSection: c.ConfigurationSection, WatchedFiles: c.WatchedFiles,
		OwnUI: true, Category: c.Category, TraceKey: c.TraceKey,
	}
}

// SettingSpecs are the settings of the pack that name the programs: one string setting for each PathSetting, in the user's settings
// only (a folder's settings must not decide which program runs).
func (c Config) SettingSpecs() []extension.SettingSpec {
	var out []extension.SettingSpec
	seen := map[string]bool{}
	for _, p := range c.Programs {
		if p.PathSetting == "" || seen[p.PathSetting] {
			continue
		}
		seen[p.PathSetting] = true
		out = append(out, extension.SettingSpec{
			Key: p.PathSetting, Type: "string", Default: "", Scope: "machine",
			Description: "The path of the " + p.Name + " program. When empty, " + p.Name + " is looked for in " + p.searchText() + ".",
		})
	}
	return out
}

// searchText says in words where the program is looked for.
func (p Program) searchText() string {
	var places []string
	places = append(places, p.Before...)
	places = append(places, "PATH")
	places = append(places, p.After...)
	return strings.Join(places, ", then ")
}

// Subscribes are the event topics the pack declares in its manifest for the status item.
func (c Config) Subscribes() []string { return []string{extension.TopicEditorActiveChanged} }

// program is the program Select chose.
func (c Config) program(s *Settings) (Program, int) {
	i := 0
	if c.Select != nil {
		i = c.Select(s)
	}
	if i < 0 || i >= len(c.Programs) {
		i = 0
	}
	return c.Programs[i], i
}

// valid tells whether the Config has what Attach needs.
func (c Config) valid() bool {
	return c.ID != "" && c.Name != "" && len(c.Programs) > 0 && len(c.Languages) > 0
}
