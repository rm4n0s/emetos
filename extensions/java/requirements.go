package java

import (
	"context"
	"strconv"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// What the Java extension needs from the system besides jdtls (SPEC §10.36, step 34.2): the Java runtime jdtls runs on, found as a start
// finds it (resolveJava: java.jdt.ls.java.home, JAVA_HOME, then PATH, Java 21 or newer).

// RequirementJava is the id of the requirement of the Java runtime.
const RequirementJava = "java"

// requirements are the requirements of the manifest besides the server's.
func requirements() []extension.Requirement {
	return []extension.Requirement{{
		ID: RequirementJava, Name: "Java " + strconv.Itoa(minJava) + "+", Kind: extension.RequirementProgram,
		Purpose: "runs jdtls, the Java language server (Java " + strconv.Itoa(minJava) + " or newer)",
		Install: extension.Install{Docs: "https://adoptium.net/", Commands: []extension.InstallCommand{
			{Family: extension.FamilyFedora, Command: "sudo dnf install java-21-openjdk-devel"},
			{Family: extension.FamilyDebian, Command: "sudo apt install openjdk-21-jdk"},
			{Family: extension.FamilyArch, Command: "sudo pacman -S jdk21-openjdk"},
			{Family: extension.FamilySuse, Command: "sudo zypper install java-21-openjdk-devel"},
			{Family: extension.FamilyMacOS, Command: "brew install openjdk@21"},
		}},
		PathSetting: JavaHomeSetting, Check: checkJava,
	}}
}

// checkJava is the check of the Java runtime.
func checkJava(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	get := env.Setting
	if get == nil {
		get = func(string) (any, bool) { return nil, false }
	}
	prog, err := resolveJava(ctx, langserver.DefaultEnv(env.Folder, langserver.NewSettings(get)))
	if err == nil {
		return extension.RequirementResult{State: extension.RequirementFound, Path: prog.path, Version: strconv.Itoa(prog.major), Detail: "found through " + prog.source}
	}
	if ctx.Err() != nil {
		return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check was stopped"}
	}
	appErr, _ := errors.FromError(err)
	if appErr == nil {
		return extension.RequirementResult{State: extension.RequirementUnusable, Path: prog.path, Detail: err.Error()}
	}
	switch appErr.Tag {
	case "JavaNotFound":
		return extension.RequirementResult{State: extension.RequirementMissing, Detail: appErr.Message}
	case "JavaTooOld":
		version, _ := appErr.Metadata["version"].(string)
		return extension.RequirementResult{State: extension.RequirementUnusable, Path: prog.path, Version: version, Detail: appErr.Message}
	}
	return extension.RequirementResult{State: extension.RequirementUnusable, Path: prog.path, Detail: appErr.Message}
}
