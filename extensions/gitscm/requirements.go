package gitscm

import (
	"context"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// What the Git extension needs from the system (SPEC §10.36, step 34.3): git 2.30 or newer, found as the extension finds it (git.Find,
// with the candidates of git.path). Only "git --version" is run, outside any repository.

// RequirementGit is the id of the requirement of git.
const RequirementGit = "git"

// requirements are the requirements of the manifest.
func requirements() []extension.Requirement {
	return []extension.Requirement{{
		ID: RequirementGit, Name: "git " + git.MinVersion.String() + "+", Kind: extension.RequirementProgram,
		Purpose: "the Source Control view, file decorations, diffs, blame and every Git command (git " + git.MinVersion.String() + " or newer)",
		Install: extension.Install{Docs: "https://git-scm.com/downloads", Commands: []extension.InstallCommand{
			{Family: extension.FamilyFedora, Command: "sudo dnf install git"},
			{Family: extension.FamilyDebian, Command: "sudo apt install git"},
			{Family: extension.FamilyArch, Command: "sudo pacman -S git"},
			{Family: extension.FamilySuse, Command: "sudo zypper install git"},
			{Family: extension.FamilyMacOS, Command: "xcode-select --install"},
		}},
		PathSetting: "git.path", Check: checkGit,
	}}
}

// checkGit is the check of git.
func checkGit(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	if env.Setting != nil {
		if v, ok := env.Setting("git.enabled"); ok && v == false {
			return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "git.enabled is false: Git is turned off"}
		}
	}
	inst, err := git.Find(ctx, gitPathHints(env), git.FindOptions{})
	if err == nil {
		return extension.RequirementResult{State: extension.RequirementFound, Path: inst.Path, Version: inst.Version.String()}
	}
	if ctx.Err() != nil {
		return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check was stopped"}
	}
	appErr, _ := errors.FromError(err)
	if appErr != nil && appErr.Tag == "GitTooOld" {
		path, _ := appErr.Metadata["path"].(string)
		version, _ := appErr.Metadata["version"].(string)
		return extension.RequirementResult{State: extension.RequirementUnusable, Path: path, Version: version, Detail: appErr.Message}
	}
	detail := "git was not found in PATH"
	if hints := gitPathHints(env); len(hints) > 0 {
		detail = "git was not found in git.path (" + strings.Join(hints, ", ") + ") or in PATH"
	}
	return extension.RequirementResult{State: extension.RequirementMissing, Detail: detail}
}

// gitPathHints are the candidates of git.path: a string, or an array of strings.
func gitPathHints(env extension.RequirementEnv) []string {
	if env.Setting == nil {
		return nil
	}
	switch v, _ := env.Setting("git.path"); p := v.(type) {
	case string:
		if strings.TrimSpace(p) != "" {
			return []string{p}
		}
	case []any:
		var out []string
		for _, e := range p {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
