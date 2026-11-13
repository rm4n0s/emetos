package terminal

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
)

// What the Terminal extension needs from the system (SPEC §10.36, step 34.3): the shell a new terminal runs, found as the terminal finds
// it (defaultProfileOf: terminal.integrated.defaultProfile.linux, else $SHELL, else /bin/sh). Nothing is run.

// RequirementShell is the id of the requirement of the shell.
const RequirementShell = "shell"

// envSettings is the settingGetter of a check: the settings of its env; a key that is not there reads as null.
type envSettings struct{ env extension.RequirementEnv }

// Get implements settingGetter.
func (e envSettings) Get(key string) (any, error) {
	if e.env.Setting == nil {
		return nil, nil
	}
	v, _ := e.env.Setting(key)
	return v, nil
}

// requirements are the requirements of the manifest; machine gives the environment of this process (osEnviron), a test its own.
func requirements(machine func() environ) []extension.Requirement {
	return []extension.Requirement{{
		ID: RequirementShell, Name: "Shell", Kind: extension.RequirementProgram,
		Purpose: "the program a new terminal runs (terminal.integrated.defaultProfile.linux, else $SHELL, else /bin/sh)",
		Install: extension.Install{Commands: []extension.InstallCommand{
			{Family: extension.FamilyFedora, Command: "sudo dnf install bash"},
			{Family: extension.FamilyDebian, Command: "sudo apt install bash"},
			{Family: extension.FamilyArch, Command: "sudo pacman -S bash"},
			{Family: extension.FamilySuse, Command: "sudo zypper install bash"},
		}},
		Check: func(_ context.Context, env extension.RequirementEnv) extension.RequirementResult {
			return checkShell(envSettings{env}, machine())
		},
	}}
}

// checkShell is the check of the shell.
func checkShell(s settingGetter, env environ) extension.RequirementResult {
	p := defaultProfileOf(s, env, profilesOf(s, env))
	if firstFound(env, p.path) == "" {
		return extension.RequirementResult{State: extension.RequirementMissing, Path: p.path, Detail: p.path + " cannot be run, and it is the last shell a terminal tries"}
	}
	res := extension.RequirementResult{State: extension.RequirementFound, Path: p.path, Detail: p.name}
	if name, err := readSetting[string](s, settingDefaultProfile); err == nil && name != "" && name != p.name {
		res.Detail = p.name + " (the profile " + name + " of " + settingDefaultProfile + " was not found)"
	}
	return res
}
