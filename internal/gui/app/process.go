package app

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/comment"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// Process holds what the whole process shares (SPEC §5.1): the folders, the registries of
// settings, commands and default keybindings, the notifications and the logs. It is made
// once, before the first window, and its fields never change.
type Process struct {
	// Paths are the user folders.
	Paths storage.Paths
	// Logger is the process log.
	Logger *slog.Logger
	// Logs holds the log channels of the Output panel; nil when the process runs without a
	// log file (tests).
	Logs *logs.Logs
	// Channels holds the lines of the log channels that the Output panel shows: the store of Logs, or a
	// store of its own when the process runs without a log file.
	Channels *logs.Store
	// Settings is the registry of the settings Emetos has.
	Settings *settings.Registry
	// Commands is the registry of commands.
	Commands *command.Registry
	// Menus holds the items of the menus: the menu bar, the context menus and the Manage menu
	// (SPEC §10.1, §11.2).
	Menus *command.MenuRegistry
	// Views holds the view containers of the activity bar and the views in them (SPEC §10.4).
	Views *command.ViewRegistry
	// Keymap is the default keymap.
	Keymap *keybinding.Registry
	// Notify is the model of notifications.
	Notify *notify.Service
	// Languages is the registry of the languages and their grammars (SPEC §8.1).
	Languages *language.Registry
	// State reads and writes the UI state files of the default profile: the global state (the recently opened
	// folders and files, the open tabs, the profile of each folder) that all the profiles share (SPEC
	// §10.10, §10.12). The state of the windows is in the store of the profile of each window (StateFor).
	State *storage.StateStore
	// Profiles manages the profiles: the list, creating, renaming, deleting and the profile last used with each
	// folder (SPEC §10.12).
	Profiles *storage.Profiles
	// WatchFiles tells whether the windows start their file watcher (ProcessOptions.WatchFiles).
	WatchFiles bool
	// Launcher hands the launches that other Emetos processes send over the socket (SPEC §10.11) to the windows;
	// nil when the process has no socket.
	Launcher *Launcher
	// InputMode is the insert or overtype mode of the editors of every workspace tab, as VS Code's InputMode is the
	// mode of every editor of its window (SPEC §9.3).
	InputMode *editor.InputMode
	// Extensions is the host of the extensions compiled into Emetos: their manifests, and the scopes (the workspace tabs)
	// they run in (SPEC §10.15).
	Extensions *exthost.Host
	// Workspaces is the manager of the window tabs of the process: it opens, activates, moves, replaces and closes them (workspaces.go, SPEC §10.14).
	Workspaces *Workspaces
	// Virtual holds the providers of the read-only `emetos-virtual:` documents (SPEC §7.1), such as the default
	// settings.
	Virtual *document.VirtualRegistry
	// clipboard holds the texts of the comparisons with the clipboard (compare.go).
	clipboard *clipboardTexts

	icons iconPreview

	// userThemes are the themes of the user's folder of themes, which the profiles share; each window's theme
	// service (Workspace.Themes) gets them.
	userThemes []theme.UserTheme
	// stateMu guards states, the state stores of the profiles other than the default one, made when a window
	// first uses the profile.
	stateMu sync.Mutex
	states  map[string]*storage.StateStore
	// stateOpts are the options the stores of the profiles are made with.
	stateOpts storage.StateStoreOptions

	scanTimeout time.Duration
	// backupAfter, backupWrite and backupMax are ProcessOptions.BackupAfter, BackupWriteFile and BackupMaxSize.
	backupAfter storage.AfterFunc
	backupWrite storage.WriteFileFunc
	backupMax   int
	grammarMu   sync.Mutex
	grammars    map[grammarKey]*textmate.Grammar
	// grammarsAt is the version of the language registry the grammars were made at (language.Registry.Version).
	grammarsAt uint64
	regexes    *regex.Cache
	brackets   *brackets.RegistryLanguages
	indenter   *indent.Indenter
	closing    *autoclose.RegistryLanguages
	comments   *comment.RegistryLanguages
}

// BracketLanguages gives the brackets of the languages (SPEC §8.5), made from their configurations.
func (p *Process) BracketLanguages() *brackets.RegistryLanguages { return p.brackets }

// Indenter applies the indentation rules and the on-enter rules of the languages (SPEC §8.6.2).
func (p *Process) Indenter() *indent.Indenter { return p.indenter }

// AutoClosing gives the auto-closing pairs and the surrounding pairs of the languages (SPEC §8.6.4).
func (p *Process) AutoClosing() *autoclose.RegistryLanguages { return p.closing }

// Comments gives the comment tokens of the languages (SPEC §8.6.5).
func (p *Process) Comments() *comment.RegistryLanguages { return p.comments }

// grammarKey is a grammar of a language with a limit for the length of a line it tokenizes.
type grammarKey struct {
	language string
	maxLine  int
}

// ProcessOptions say what a Process is made of.
type ProcessOptions struct {
	// Paths are the user folders.
	Paths storage.Paths
	// Logs is the open log, or nil. Its logger becomes the process logger.
	Logs *logs.Logs
	// Logger is the process logger when Logs is nil; slog.Default() when both are nil.
	Logger *slog.Logger
	// WatchFiles lets the windows watch their folder, the configuration and the open files (SPEC
	// §10.5). The editor sets it; tests leave it off, and the tests of the watcher and of what it
	// does set it. Many tests stage a change of a file on disk by hand (a file "changed by another
	// program", a file written behind the editor's back) and count on nothing else reacting to it;
	// with a live watcher the reload and the notice of the watcher race with them (step 9b.1b ran
	// the whole suite with it on: with -race the auto save test of a file that changed on disk
	// failed every time). Since Glutter 1.7 the tester unmounts the tree, so a watcher no longer
	// leaks an inotify instance when it is on.
	WatchFiles bool
	// Launcher, when set, receives the launches of other processes (Process.Launcher); the window attaches to it.
	Launcher *Launcher
	// ScanTimeout is the longest a search of one pattern of a grammar may take; zero is the default
	// of SPEC §8.2 (100 ms). Tests that run slowly (the race detector on a loaded machine) lengthen
	// it, because a search that times out leaves its line of text without colors.
	ScanTimeout time.Duration
	// StateDelay is how long after a change a state file is written (SPEC §10.10); one second when zero.
	StateDelay time.Duration
	// StateAfter sets the timers of that delay, and StateWriteFile replaces the function that writes the
	// files; the system clock and the atomic write when nil. Tests move the clock by hand: a test that does
	// not pass one gets timers that never run.
	StateAfter     storage.AfterFunc
	StateWriteFile storage.WriteFileFunc
	// Extensions are the constructors of the extensions to run (extensions.All in the editor); tests pass their own. The
	// language packs among them (Manifest.Contributes.Languages) add their languages to the registry of the process.
	Extensions []extension.Factory
	// Languages is a registry that has languages already, which no extension adds and which stay for the life of the
	// process; nil makes one that has plain text only. The editor leaves it nil, because the bundled languages are
	// extensions; the tests of the packages under internal/ cannot import those and pass languagetest.Bundled().
	Languages *language.Registry
	// BackupAfter sets the timers of the delay of the backups of unsaved documents (hot exit, SPEC §10.10); it is
	// StateAfter when nil. BackupWriteFile replaces the function that writes a backup, and BackupMaxSize is the
	// largest text that is backed up; the atomic write and storage.MaxBackupSize when zero. Tests set them.
	BackupAfter     storage.AfterFunc
	BackupWriteFile storage.WriteFileFunc
	BackupMaxSize   int
}

// NewProcess makes the services of the process: the registries with every setting, command
// and default keybinding Emetos has.
func NewProcess(opts ProcessOptions) *Process {
	logger := opts.Logger
	if opts.Logs != nil {
		logger = opts.Logs.Logger()
	}
	if logger == nil {
		logger = slog.Default()
	}
	languages := opts.Languages
	if languages == nil {
		languages = language.NewRegistry()
	}
	defaults := settings.DefaultRegistry()
	// What the languages that are there already ask of the settings; the language packs of the extensions give theirs when
	// the host adds them (Go indents with tabs, YAML with two spaces).
	for id, values := range languages.ConfigurationDefaults() {
		if err := defaults.RegisterLanguageDefaults(id, values); err != nil {
			logger.Warn("language settings", "language", id, "err", err)
		}
	}
	channels := logs.NewStore(0)
	if opts.Logs != nil {
		channels = opts.Logs.Channels()
	}
	if opts.BackupAfter == nil {
		opts.BackupAfter = opts.StateAfter
	}
	state, err := storage.NewStateStore(opts.Paths, storage.DefaultProfile, storage.StateStoreOptions{
		Delay: opts.StateDelay, After: opts.StateAfter, WriteFile: opts.StateWriteFile, Logger: logger,
	})
	if err != nil {
		// the default profile has a safe folder: this is a broken build
		panic(err)
	}
	p := &Process{
		State:     state,
		Profiles:  storage.NewProfiles(opts.Paths, storage.ProfilesOptions{State: state, Logger: logger}),
		stateOpts: storage.StateStoreOptions{Delay: opts.StateDelay, After: opts.StateAfter, WriteFile: opts.StateWriteFile, Logger: logger},
		Paths:     opts.Paths,
		Logger:    logger,
		Logs:      opts.Logs,
		Channels:  channels,
		Settings:  defaults,
		Commands:  command.DefaultRegistry(),
		Keymap:    keybinding.DefaultRegistry(),
		Notify:    notify.NewService(logger, nil),
		InputMode: editor.NewInputMode(),

		WatchFiles:  opts.WatchFiles,
		Launcher:    opts.Launcher,
		scanTimeout: opts.ScanTimeout,
		backupAfter: opts.BackupAfter,
		backupWrite: opts.BackupWriteFile,
		backupMax:   opts.BackupMaxSize,

		Languages: languages,
		grammars:  map[grammarKey]*textmate.Grammar{},
		regexes:   regex.NewCache(1 << 14),
		brackets:  brackets.NewRegistryLanguages(languages),
	}
	p.indenter = indent.New(indent.NewRegistryLanguages(languages, p.brackets))
	p.closing = autoclose.NewRegistryLanguages(languages)
	p.comments = comment.NewRegistryLanguages(languages)
	// the links between window tabs (event-sharing.json); a file that cannot be used is ignored with a warning
	sharing := storage.NewSharingStore(opts.Paths, storage.SharingStoreOptions{Logger: logger})
	if err := sharing.Load(context.Background()); err != nil {
		logger.Warn("event-sharing.json is ignored; no tab shares events until the user allows it again", "err", err)
	}
	// the language packs register their languages here, before the first window and its first file
	p.Extensions = exthost.NewHost(exthost.HostOptions{Factories: opts.Extensions, Logger: logger, Settings: defaults, Sharing: sharing, Languages: languages})
	for _, problem := range languages.Problems() {
		logger.Warn("language problem", "err", problem)
	}
	p.Workspaces = newWorkspaces(p)
	p.registerVirtualDocuments()
	p.Menus = command.DefaultMenus(p.Commands)
	p.Menus.SetProvider(command.MenubarRecentMenu, p.recentMenuItems)
	p.Views = command.DefaultViews()
	registerDevCommands(p.Commands)
	registerDevNotify(p.Commands, p.Notify, logger)
	registerDevLog(p.Commands, p.Channels)
	registerDevMarkers(p.Commands)
	p.loadUserThemes()
	return p
}

// StateFor is the state store of a profile: the store of the default profile is State, the others are made when
// they are first asked for and kept. The error has the tag ProfilePathUnsafe.
func (p *Process) StateFor(profile string) (*storage.StateStore, error) {
	if profile == storage.DefaultProfile {
		return p.State, nil
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if s := p.states[profile]; s != nil {
		return s, nil
	}
	s, err := storage.NewStateStore(p.Paths, profile, p.stateOpts)
	if err != nil {
		return nil, err
	}
	if p.states == nil {
		p.states = map[string]*storage.StateStore{}
	}
	p.states[profile] = s
	return s, nil
}

// forgetState drops the state store of a profile that was renamed or deleted, so that a profile that gets the name
// again gets a store over its own folder. Nothing is written: the caller wrote the state while the folder was there.
func (p *Process) forgetState(profile string) {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	delete(p.states, profile)
}

// flushStates writes every state file that waits, in the stores of all the profiles that were used. It tries
// them all and returns the first error.
func (p *Process) flushStates(ctx context.Context) error {
	p.stateMu.Lock()
	stores := make([]*storage.StateStore, 0, len(p.states)+1)
	stores = append(stores, p.State)
	for _, s := range p.states {
		stores = append(stores, s)
	}
	p.stateMu.Unlock()
	var first error
	for _, s := range stores {
		if err := s.Flush(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// newThemes makes the theme service of a window: the themes of the user's folder are in it, and the settings of
// the window choose among them (Workspace.followTheme).
func (p *Process) newThemes() *theme.Service {
	service := theme.NewService(theme.DefaultColorRegistry())
	if len(p.userThemes) > 0 {
		if err := service.SetUserThemes(p.userThemes); err != nil {
			p.Logger.Debug("theme from the settings is not among the user themes", "err", err)
		}
	}
	return service
}

// loadUserThemes reads the themes in the user's folder of themes into the theme service. A
// folder that is not there is no themes; a theme that cannot be read is logged.
func (p *Process) loadUserThemes() {
	dir := p.Paths.ThemesDir()
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return
	}
	themes, problems, err := theme.LoadUserThemes(os.DirFS(dir))
	if err != nil {
		p.Logger.Warn("user themes", "dir", dir, "err", err)
		return
	}
	for _, problem := range problems {
		p.Logger.Warn("user theme", "dir", dir, "err", problem)
	}
	p.userThemes = themes
}

// WarmGrammars makes the grammars of the languages of documents, and their rules, on a goroutine, so that
// the first tokenizing of a file of each (on the thread that paints, in the first frame of the editor) finds
// them ready. It is called with the documents of the first window before it is shown: the renderer takes the
// better part of a second to start, which is otherwise idle (step 10.1: the rules of Markdown take 60 ms to
// make, those of HTML and TypeScript 15). store gives editor.maxTokenizationLineLength of each language.
func (p *Process) WarmGrammars(ctx context.Context, store *settings.Store, docs []*document.Document) {
	var languages []string
	for _, d := range docs {
		if d == nil || slices.Contains(languages, d.LanguageID()) || !p.Languages.HasGrammar(d.LanguageID()) {
			continue
		}
		languages = append(languages, d.LanguageID())
	}
	if len(languages) == 0 {
		return
	}
	lines := make([]int, len(languages))
	for i, id := range languages {
		lines[i] = int(settings.Get[float64](store, "editor.maxTokenizationLineLength", id))
	}
	go func() {
		for i, id := range languages {
			if ctx.Err() != nil {
				return
			}
			g, err := p.Grammar(id, lines[i])
			if err != nil {
				return // the editor reports it when it opens the file
			}
			if g != nil {
				g.Warm()
			}
		}
	}()
}

// Grammar gives the grammar of a language for tokenizing, made once and shared by the documents
// of the language (it is safe for concurrent use). maxLineBytes is editor.maxTokenizationLineLength
// (0 for the default). It is nil, with no error, for a language that has no grammar (plain text).
// The error has the tags of language.Registry.NewGrammarWith.
func (p *Process) Grammar(languageID string, maxLineBytes int) (*textmate.Grammar, error) {
	if !p.Languages.HasGrammar(languageID) {
		return nil, nil
	}
	key := grammarKey{languageID, maxLineBytes}
	p.grammarMu.Lock()
	defer p.grammarMu.Unlock()
	// a language pack came or went: the embedded languages of the grammars made so far may be out of date
	if v := p.Languages.Version(); v != p.grammarsAt {
		clear(p.grammars)
		p.grammarsAt = v
	}
	if g, ok := p.grammars[key]; ok {
		return g, nil
	}
	g, err := p.Languages.NewGrammarWith(languageID, language.GrammarSettings{Cache: p.regexes, MaxLineBytes: maxLineBytes, ScanTimeout: p.scanTimeout})
	if err != nil {
		return nil, err
	}
	p.grammars[key] = g
	return g, nil
}
