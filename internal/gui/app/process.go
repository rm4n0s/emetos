package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/comment"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/snippet"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/theme"
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
	// Themes is the theme service: it picks the color theme from the settings and the system's
	// brightness (SPEC §8.4).
	Themes *theme.Service
	// Languages is the registry of the languages and their grammars (SPEC §8.1).
	Languages *language.Registry
	// Snippets are the user's snippet files, in the snippets folder of the configuration (SPEC
	// §9.9). The workspace watcher of step 6.13 reloads them when they change.
	Snippets *snippet.Service
	// State reads and writes the UI state files: the recently opened folders and files and the state of each
	// window (SPEC §10.10).
	State *storage.StateStore
	// WatchFiles tells whether the windows start their file watcher (ProcessOptions.WatchFiles).
	WatchFiles bool

	scanTimeout time.Duration
	// backupAfter, backupWrite and backupMax are ProcessOptions.BackupAfter, BackupWriteFile and BackupMaxSize.
	backupAfter storage.AfterFunc
	backupWrite storage.WriteFileFunc
	backupMax   int
	grammarMu   sync.Mutex
	grammars    map[grammarKey]*textmate.Grammar
	regexes     *regex.Cache
	brackets    *brackets.RegistryLanguages
	indenter    *indent.Indenter
	closing     *autoclose.RegistryLanguages
	comments    *comment.RegistryLanguages
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
	// §10.5). The editor sets it; tests leave it off so that no inotify instance is left behind.
	//
	// Glutter: gluttertest.Tester never disposes the widget tree at the end of a test, so the
	// watcher that the shell starts with its lifetime would never stop; Glutter should unmount the
	// tree (or cancel the lifetimes of the states) when the test ends (docs/GLUTTER-ISSUES.md).
	WatchFiles bool
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
	languages, err := language.Bundled()
	if err != nil {
		// The languages are embedded in the program, so this is a broken build.
		panic(err)
	}
	for _, problem := range languages.Problems() {
		logger.Warn("language problem", "err", problem)
	}
	defaults := settings.DefaultRegistry()
	// What the languages ask of the settings (Go indents with tabs, YAML with two spaces).
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
		State:    state,
		Paths:    opts.Paths,
		Logger:   logger,
		Logs:     opts.Logs,
		Channels: channels,
		Settings: defaults,
		Commands: command.DefaultRegistry(),
		Keymap:   keybinding.DefaultRegistry(),
		Notify:   notify.NewService(logger, nil),
		Themes:   theme.NewService(theme.DefaultColorRegistry()),

		WatchFiles:  opts.WatchFiles,
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
	p.Menus = command.DefaultMenus(p.Commands)
	p.Menus.SetProvider(command.MenubarRecentMenu, p.recentMenuItems)
	p.Views = command.DefaultViews()
	registerDevCommands(p.Commands)
	registerDevNotify(p.Commands, p.Notify, logger)
	registerDevLog(p.Commands, p.Channels)
	registerDevMarkers(p.Commands)
	p.loadUserThemes()
	p.loadSnippets()
	return p
}

// loadSnippets makes the snippet service for the folder of the user's snippets and reads the files.
// A folder that is not there is no snippets; a file that cannot be read is logged.
func (p *Process) loadSnippets() {
	dir, err := p.Paths.ConfigDir(storage.DefaultProfile)
	if err != nil {
		p.Logger.Warn("snippets folder", "err", err)
		p.Snippets = snippet.NewService("")
		return
	}
	p.Snippets = snippet.NewService(filepath.Join(dir, "snippets"))
	if err := p.Snippets.Reload(context.Background()); err != nil {
		p.Logger.Warn("snippets", "err", err)
	}
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
	if err := p.Themes.SetUserThemes(themes); err != nil {
		p.Logger.Debug("theme from the settings is not among the user themes", "err", err)
	}
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
