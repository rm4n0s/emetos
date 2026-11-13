package langserver

import (
	"context"
	"path/filepath"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Requirements are what the server needs from the system, for the manifest of the pack (SPEC §10.36): one requirement, the server,
// whose check is the search the launcher makes at each start (Resolve: the path setting, the folders before and after PATH, the
// version check, Customize). With Select the requirement follows the settings, as the launcher does, and names the program they
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
	prog, index := c.program(s)
	res := extension.RequirementResult{}
	if index > 0 {
		res.Install, res.PathSetting = prog.Install, prog.PathSetting
	}
	l, err := Resolve(ctx, c, e)
	if err == nil {
		res.State, res.Path, res.Version = extension.RequirementFound, l.Found.Path, l.Version
		res.Detail = foundDetail(prog, l.Found)
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
