package langserver

import (
	"context"
	"path/filepath"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Requirements are what the server needs from the system, for the manifest of the pack (SPEC §10.36): one requirement, the server,
// whose check is the search the launcher makes at each start (Resolve: the path setting, the folders before and after PATH, the
// version check), without Customize, which belongs to an activation. With Select the requirement follows the settings, as the launcher does, and names the program they
// chose; its Install and PathSetting are those of the first program, and a result for another program gives that program's. A
// Config that Attach would refuse gives none.
func (c Config) Requirements() []extension.Requirement {
	if !c.valid() {
		return nil
	}
	first := c.Programs[0]
	name := first.Name
	if c.Select != nil && len(c.Programs) > 1 {
		name = c.Name + " language server"
	}
	purpose := c.Purpose
	if purpose == "" {
		purpose = "the language features of " + c.Name
	}
	return []extension.Requirement{{
		ID: c.ID, Name: name, Kind: extension.RequirementProgram, Purpose: purpose,
		Install: first.Install, PathSetting: first.PathSetting,
		Check: func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
			return c.checkRequirement(ctx, env, Env{})
		},
	}}
}

// checkRequirement is the check of the requirement of the server: Resolve in the environment of this machine (or base, when a test
// gives one with a Path), turned into a result.
func (c Config) checkRequirement(ctx context.Context, env extension.RequirementEnv, base Env) extension.RequirementResult {
	get := env.Setting
	if get == nil {
		get = func(string) (any, bool) { return nil, false }
	}
	s := NewSettings(get)
	e := DefaultEnv(env.Folder, s)
	if base.Path != "" || base.Home != "" {
		base.Folder, base.Settings = env.Folder, s
		e = base
	}
	e.State = env.State
	prog, index := c.program(s)
	res := extension.RequirementResult{}
	if index > 0 {
		res.Install, res.PathSetting = prog.Install, prog.PathSetting
	}
	// Customize is the pack's, bound to an activation: it may post to the UI, keep what it decided or clean a folder, and a check
	// changes nothing. What it verifies beyond the program (node for TypeScript, the Java runtime, the .NET SDK) is declared as
	// requirements of its own.
	plain := c
	plain.Customize, plain.CustomizeContext = nil, nil
	l, err := Resolve(ctx, plain, e)
	if err == nil {
		res.State, res.Path, res.Version = extension.RequirementFound, l.Found.Path, l.Version
		res.Detail = foundDetail(prog, l.Found)
		if l.VersionErr != nil {
			res.Detail += "; its version could not be read"
		}
		if c.Select != nil && len(c.Programs) > 1 {
			res.Detail = prog.Name + ", " + res.Detail
		}
		return res
	}
	appErr, _ := errors.FromError(err)
	tag, message := "", err.Error()
	if appErr != nil {
		tag, message = appErr.Tag, appErr.Message
	}
	switch tag {
	case "ServerNotFound":
		res.State, res.Detail = extension.RequirementMissing, prog.Name+" was not found in PATH or the folders the extension searches"
	case "ServerDisabled":
		res.State, res.Detail = extension.RequirementUnknown, message
	case "ServerLaunchCancelled":
		res.State, res.Detail = extension.RequirementUnknown, "the check was stopped"
	default: // ServerPathInvalid, ServerTooOld, ServerCustomizeFailed and the packs' own failures of Customize
		res.State, res.Detail = extension.RequirementUnusable, message
		if appErr != nil {
			if p, ok := appErr.Metadata["path"].(string); ok {
				res.Path = p
			}
			if v, ok := appErr.Metadata["version"].(string); ok {
				res.Version = v
			}
		}
	}
	return res
}

// foundDetail says where a program was found.
func foundDetail(p Program, f Found) string {
	switch f.Source {
	case FromSetting:
		return "found through " + p.PathSetting
	case FromPath:
		return "found in PATH"
	default:
		return "found in " + filepath.Dir(f.Path)
	}
}

// ProgramCheck says how CheckProgram finds a program and asks for its version.
type ProgramCheck struct {
	// Name is the name of the program in the details ("dlv").
	Name string
	// Find finds it: the path, or an error when it is not there. The error's message is the detail of a missing program when
	// Missing is empty.
	Find func() (string, error)
	// Missing is the detail of a program that was not found; the message of Find's error when empty.
	Missing string
	// Version asks the program for its version (nil Args: it is not asked); its Min is the oldest accepted.
	Version VersionCheck
	// Folder is where the version is asked, Env the environment (nil is Emetos's).
	Folder string
	Env    []string
}

// CheckProgram is the check of a program requirement (SPEC §10.36): found with its path and version, missing, or unusable when it is
// older than the minimum. A version that cannot be read is not a failure, as for a server (Resolve); it is said in the detail.
func CheckProgram(ctx context.Context, p ProgramCheck) extension.RequirementResult {
	path, err := p.Find()
	if err != nil {
		detail := p.Missing
		if detail == "" {
			detail = errorText(err)
		}
		return extension.RequirementResult{State: extension.RequirementMissing, Detail: detail}
	}
	res := extension.RequirementResult{State: extension.RequirementFound, Path: path}
	if len(p.Version.Args) == 0 {
		return res
	}
	version, verr := QueryVersion(ctx, path, p.Version, p.Folder, p.Env)
	switch {
	case ctx.Err() != nil:
		return extension.RequirementResult{State: extension.RequirementUnknown, Path: path, Detail: "the check was stopped"}
	case verr != nil:
		res.Detail = "its version could not be read"
	default:
		res.Version = version
		if min := p.Version.Min; min != "" && CompareVersions(version, min) < 0 {
			res.State = extension.RequirementUnusable
			res.Detail = p.Name + " " + version + " is too old: " + min + " or newer is needed"
		}
	}
	return res
}

// errorText is the message of an error of this package's kind, else its text.
func errorText(err error) string {
	if appErr, ok := errors.FromError(err); ok && appErr.Message != "" {
		return appErr.Message
	}
	return err.Error()
}
