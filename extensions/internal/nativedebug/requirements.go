package nativedebug

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// RequirementDebugger is the id of the requirement of the native debugger.
const RequirementDebugger = "native-debugger"

// Requirement is what a language pack that debugs native programs (C, C++, Rust) declares (SPEC §10.36, step 34.2): one requirement with
// two alternatives, gdb 14 or newer (gdb.path, else gdb in PATH) or lldb-dap (lldb-dap.executable-path, else lldb-dap or lldb-vscode in
// PATH), found as the adapters find them. languages names the languages in the purpose ("C and C++").
func Requirement(languages string) extension.Requirement {
	install := extension.Install{Docs: gdbInstall.Docs, Commands: append([]extension.InstallCommand(nil), gdbInstall.Commands...)}
	for _, c := range lldbInstall.Commands {
		if c.Family == extension.FamilyMacOS { // gdb does not debug on a Mac with Apple silicon: LLDB comes with the command line tools
			install.Commands = append(install.Commands, c)
		}
	}
	return extension.Requirement{
		ID: RequirementDebugger, Name: "gdb or lldb-dap", Kind: extension.RequirementProgram, Optional: true,
		Purpose: "debugs " + languages + " programs (gdb " + MinGDBVersion + " or newer, or lldb-dap)",
		Install: install, PathSetting: GDBPathSetting, Check: checkDebugger,
	}
}

// checkDebugger takes gdb when it can be used, else lldb-dap, and says which.
func checkDebugger(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	gdbPath, gdbProblem := findProgram(env.StringSetting(GDBPathSetting), env.Folder, "gdb")
	gdbWhy := ""
	switch {
	case gdbProblem != "":
		gdbWhy = "gdb cannot be used: " + gdbProblem
	case gdbPath != "":
		version, err := langserver.QueryVersion(ctx, gdbPath, langserver.VersionCheck{Args: []string{"--version"}, Parse: parseGdbVersion}, env.Folder, nil)
		switch {
		case ctx.Err() != nil:
			return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check was stopped"}
		case err != nil:
			gdbWhy = gdbPath + " did not tell its version"
		case langserver.CompareVersions(version, MinGDBVersion) < 0:
			gdbWhy = "gdb " + version + " is too old: " + MinGDBVersion + " or newer is needed"
		default:
			return extension.RequirementResult{State: extension.RequirementFound, Path: gdbPath, Version: version, Detail: "gdb"}
		}
	}
	lldbPath, lldbProblem := findProgram(env.StringSetting(LLDBPathSetting), env.Folder, "lldb-dap", "lldb-vscode")
	if lldbPath != "" {
		res := extension.RequirementResult{State: extension.RequirementFound, Path: lldbPath, Detail: "lldb-dap", PathSetting: LLDBPathSetting, Install: lldbInstall}
		if gdbWhy != "" {
			res.Detail = "lldb-dap (" + gdbWhy + ")"
		}
		return res
	}
	switch {
	case gdbWhy != "":
		res := extension.RequirementResult{State: extension.RequirementUnusable, Path: gdbPath, Detail: gdbWhy + ", and lldb-dap was not found"}
		if lldbProblem != "" {
			res.Detail = gdbWhy + ", and lldb-dap cannot be used: " + lldbProblem
		}
		return res
	case lldbProblem != "":
		return extension.RequirementResult{State: extension.RequirementUnusable, Detail: "lldb-dap cannot be used: " + lldbProblem + ", and gdb was not found", PathSetting: LLDBPathSetting}
	}
	return extension.RequirementResult{State: extension.RequirementMissing, Detail: "neither gdb nor lldb-dap was found in PATH"}
}
