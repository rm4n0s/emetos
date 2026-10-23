package app

import (
	"log/slog"
	"os"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
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
	// Settings is the registry of the settings Emetos has.
	Settings *settings.Registry
	// Commands is the registry of commands.
	Commands *command.Registry
	// Keymap is the default keymap.
	Keymap *keybinding.Registry
	// Notify is the model of notifications.
	Notify *notify.Service
	// Themes is the theme service: it picks the color theme from the settings and the system's
	// brightness (SPEC §8.4).
	Themes *theme.Service
}

// ProcessOptions say what a Process is made of.
type ProcessOptions struct {
	// Paths are the user folders.
	Paths storage.Paths
	// Logs is the open log, or nil. Its logger becomes the process logger.
	Logs *logs.Logs
	// Logger is the process logger when Logs is nil; slog.Default() when both are nil.
	Logger *slog.Logger
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
	p := &Process{
		Paths:    opts.Paths,
		Logger:   logger,
		Logs:     opts.Logs,
		Settings: settings.DefaultRegistry(),
		Commands: command.DefaultRegistry(),
		Keymap:   keybinding.DefaultRegistry(),
		Notify:   notify.NewService(logger, nil),
		Themes:   theme.NewService(theme.DefaultColorRegistry()),
	}
	registerDevCommands(p.Commands)
	p.loadUserThemes()
	return p
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
