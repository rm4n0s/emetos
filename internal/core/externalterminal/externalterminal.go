// Package externalterminal opens the terminal program of the desktop (xterm, gnome-terminal, konsole...) in a folder: VS Code's
// "Open in External Terminal" (SPEC §10.30, step 24.23). It is not the integrated terminal of the Terminal extension: the program is the
// user's own, in a window of the desktop, and Emetos does not keep it (the user closes it).
//
// Ported from VS Code: src/vs/platform/externalTerminal/node/externalTerminalService.ts (LinuxExternalTerminalService.spawnTerminal and
// getDefaultTerminalLinuxReady)
package externalterminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/rm4n0s/errors"
)

// Options say what to open.
type Options struct {
	// Exec is the program: a name found on the PATH, or a path. Empty chooses one with Default.
	Exec string
	// Dir is the folder the terminal starts in.
	Dir string
	// Env is the environment of the program as NAME=value lines; the environment of the process when nil.
	Env []string
	// LookPath finds a program the way exec.LookPath does; nil is that function.
	LookPath func(file string) (string, error)
	// Getenv and Exists are what Default reads; nil are os.Getenv and a check that the file exists.
	Getenv func(string) string
	Exists func(path string) bool
}

// Default is the terminal VS Code starts when the setting names none: x-terminal-emulator on Debian (where /etc/debian_version exists),
// gnome-terminal in a GNOME session, konsole in KDE Plasma, else the program $COLORTERM or $TERM names, else xterm.
func Default(getenv func(string) string, exists func(string) bool) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	if exists == nil {
		exists = fileExists
	}
	switch session := getenv("DESKTOP_SESSION"); {
	case exists("/etc/debian_version"):
		return "x-terminal-emulator"
	case session == "gnome" || session == "gnome-classic":
		return "gnome-terminal"
	case session == "kde-plasma":
		return "konsole"
	case getenv("COLORTERM") != "":
		return getenv("COLORTERM")
	case getenv("TERM") != "":
		return getenv("TERM")
	}
	return "xterm"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Open starts the terminal in a new session, with no input or output of its own, and returns at once: the program belongs to the user
// from then on (a goroutine only collects it when it ends). It fails with ExternalTerminalFolderInvalid for a folder that is not an
// existing directory, ExternalTerminalNotFound when the program is not found and ExternalTerminalStartFailed when it does not start.
func Open(ctx context.Context, o Options) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr("ExternalTerminalCancelled", err)
	}
	if info, err := os.Stat(o.Dir); err != nil || !info.IsDir() {
		return errors.New("ExternalTerminalFolderInvalid", "the terminal starts in a folder, and this is not one", "dir", o.Dir)
	}
	prog := o.Exec
	if prog == "" {
		prog = Default(o.Getenv, o.Exists)
	}
	look := o.LookPath
	if look == nil {
		look = exec.LookPath
	}
	path, err := look(prog)
	if err != nil {
		return errors.NewErr("ExternalTerminalNotFound", err).SetMetadata("exec", prog)
	}
	// not CommandContext: the terminal outlives the command that opened it, and the cancelling of a context must not close it
	cmd := exec.Command(filepath.Clean(path))
	cmd.Dir = o.Dir
	cmd.Env = o.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return errors.NewErr("ExternalTerminalStartFailed", err).SetMetadata("exec", path, "dir", o.Dir)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
