package app

import (
	"log/slog"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
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
	return &Process{
		Paths:    opts.Paths,
		Logger:   logger,
		Logs:     opts.Logs,
		Settings: settings.DefaultRegistry(),
		Commands: command.DefaultRegistry(),
		Keymap:   keybinding.DefaultRegistry(),
		Notify:   notify.NewService(logger, nil),
	}
}
