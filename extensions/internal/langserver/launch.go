package langserver

import (
	"context"
	"slices"

	"github.com/rm4n0s/errors"
)

// Resolve decides what to run for the server now: it chooses the program (Config.Select), finds it (Find), checks its version
// (Program.Version), takes the root of the project (ResolveRoot) and lets Config.Customize set the rest from the settings. It runs
// programs, so it belongs on a goroutine, not on the UI thread; ctx ends the version check. Failures are ServerNotFound,
// ServerPathInvalid, ServerTooOld, ServerLaunchCancelled and ServerCustomizeFailed (an error of Customize that is not one of ours).
// A version that cannot be read is not a failure: Launch.Version is empty and Launch.VersionErr says why.
func Resolve(ctx context.Context, cfg Config, env Env) (*Launch, error) {
	if env.Settings == nil {
		env.Settings = NewSettings(func(string) (any, bool) { return nil, false })
	}
	prog, _ := cfg.program(env.Settings)
	found, err := Find(prog, env)
	if err != nil {
		return nil, err
	}
	l := &Launch{
		Found: found, Program: prog, Args: slices.Clone(prog.Args),
		Root: ResolveRoot(env.Folder, cfg.RootMarkers, env.Home),
	}
	if len(prog.Version.Args) > 0 {
		version, verr := QueryVersion(ctx, found.Path, prog.Version, env.Folder, nil)
		switch {
		case ctx.Err() != nil:
			return nil, errors.NewErr("ServerLaunchCancelled", ctx.Err()).SetMetadata("program", prog.Name)
		case verr != nil:
			l.VersionErr = verr
		default:
			l.Version = version
			if min := prog.Version.Min; min != "" && CompareVersions(version, min) < 0 {
				return nil, errors.New("ServerTooOld", prog.Name+" "+version+" is too old: "+min+" or newer is needed", "program", prog.Name, "version", version, "min", min, "path", found.Path)
			}
		}
	}
	if cfg.Customize != nil {
		if err := cfg.Customize(l, env); err != nil {
			if _, ours := errors.FromError(err); ours {
				return nil, err
			}
			return nil, errors.NewErr("ServerCustomizeFailed", err).SetMetadata("program", prog.Name)
		}
	}
	return l, nil
}
