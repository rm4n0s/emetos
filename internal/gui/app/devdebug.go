package app

import (
	"context"
	"path/filepath"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// The dev command of Run and Debug (SPEC §15.2, step 25.4): dev.debug.breakpoint sets a breakpoint from the command line of a proof, where
// no editor is clicked. Debug builds only; the fake debugger it is used with is extensions/devdebug.

// registerDevDebug registers dev.debug.breakpoint; the window binds its handler while it is shown.
func registerDevDebug(r *command.Registry) {
	meta := command.Meta{ID: "dev.debug.breakpoint", Description: `Set a breakpoint. Arguments: {"path": "<file, relative to the folder>", "line": <line from 1>}.`}
	if err := RegisterDev(r, meta, func(command.Context, any) error { return errors.New("DevNoWindow", "no window is shown") }); err != nil {
		// the id is fixed, new and starts with dev., so a failure is a mistake here
		panic(err)
	}
}

// bindDevDebug takes over dev.debug.breakpoint in the registry while the shell is shown.
func (s *shellState) bindDevDebug() {
	if !DevCommandsEnabled() {
		return
	}
	win := s.win
	remove, err := win.Commands.SetHandler("dev.debug.breakpoint", func(ctx command.Context, args any) error {
		path, line, err := devBreakpointArgs(args)
		if err != nil {
			return err
		}
		host := win
		if win.isView() {
			host = win.viewOf
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(host.Folder, path)
		}
		_, err = win.DebugService().AddBreakpoints(context.WithoutCancel(win.ctx), path, []debug.BreakpointData{{Line: line}})
		return err
	})
	if err != nil {
		win.Process.Logger.Error("cannot bind dev.debug.breakpoint", "err", err)
		return
	}
	s.debug.removeCommands = append(s.debug.removeCommands, remove)
}

// devBreakpointArgs reads the arguments of dev.debug.breakpoint. The tag is DevDebugBreakpointInvalid.
func devBreakpointArgs(args any) (path string, line int, err error) {
	m, ok := args.(map[string]any)
	if !ok {
		return "", 0, errors.New("DevDebugBreakpointInvalid", "the arguments are an object with a path and a line")
	}
	path, _ = m["path"].(string)
	n, _ := m["line"].(float64)
	if path == "" || n < 1 {
		return "", 0, errors.New("DevDebugBreakpointInvalid", "the arguments need a path and a line from 1", "path", path, "line", n)
	}
	return path, int(n), nil
}
