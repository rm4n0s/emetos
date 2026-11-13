package containerui

import (
	"context"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// What the Docker and Podman extensions need from the system (SPEC §10.36, step 34.3): the engine, which has to answer on its socket, and
// its command line program, which Build Image, Push, Compose and a container's terminal run. A check asks the engine for /_ping and
// /version only: it lists nothing of the user's, pulls nothing, and starts no Podman service of its own (the view starts one when no
// socket answers, so a Podman whose program is there counts as found).

// The ids of the requirements of the extensions.
const (
	RequirementEngine = "engine"
	RequirementCLI    = "cli"
)

// engineInstall says how to get the engine and its program.
func engineInstall(kind containerapi.Kind) extension.Install {
	if kind == containerapi.KindPodman {
		return extension.Install{Docs: "https://podman.io/docs/installation", Commands: []extension.InstallCommand{
			{Family: extension.FamilyFedora, Command: "sudo dnf install podman"},
			{Family: extension.FamilyDebian, Command: "sudo apt install podman"},
			{Family: extension.FamilyArch, Command: "sudo pacman -S podman"},
			{Family: extension.FamilySuse, Command: "sudo zypper install podman"},
			{Family: extension.FamilyMacOS, Command: "brew install podman && podman machine init && podman machine start"},
		}}
	}
	return extension.Install{Docs: "https://docs.docker.com/engine/install/", Commands: []extension.InstallCommand{
		{Family: extension.FamilyFedora, Command: "sudo dnf install moby-engine && sudo systemctl enable --now docker"},
		{Family: extension.FamilyDebian, Command: "sudo apt install docker.io && sudo systemctl enable --now docker"},
		{Family: extension.FamilyArch, Command: "sudo pacman -S docker && sudo systemctl enable --now docker"},
		{Family: extension.FamilySuse, Command: "sudo zypper install docker && sudo systemctl enable --now docker"},
		{Family: extension.FamilyMacOS, Command: "brew install --cask docker"},
	}}
}

// Requirements are the requirements of the manifest of cfg.
func Requirements(cfg Config) []extension.Requirement {
	install := engineInstall(cfg.Kind)
	return []extension.Requirement{
		{
			ID: RequirementEngine, Name: cfg.Engine + " engine", Kind: extension.RequirementService,
			Purpose: "the containers, images, networks and volumes of the view (the engine answers on its socket; " + cfg.HostSetting() + " names another)",
			Install: install, PathSetting: cfg.HostSetting(),
			Check: func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
				return checkEngine(ctx, cfg, env)
			},
		},
		{
			ID: RequirementCLI, Name: cfg.cliName(), Kind: extension.RequirementProgram, Optional: true,
			Purpose: "Build Image, Push, Compose and the terminal of a container",
			Install: install,
			Check: func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
				return langserver.CheckProgram(ctx, langserver.ProgramCheck{
					Name: cfg.cliName(), Find: func() (string, error) { return cfg.lookPath(cfg.cliName()) },
					Missing: cfg.cliName() + " was not found in PATH",
					Version: langserver.VersionCheck{Args: []string{"--version"}}, Folder: env.Folder,
				})
			},
		},
	}
}

// checkEngine is the check of the engine: Find without a service of its own, and closed at once.
func checkEngine(ctx context.Context, cfg Config, env extension.RequirementEnv) extension.RequirementResult {
	found, err := cfg.dial()(ctx, containerapi.FindOptions{Kind: cfg.Kind, Host: env.StringSetting(cfg.HostSetting()), Getenv: cfg.Getenv, NoService: true})
	if err == nil {
		defer found.Close()
		return extension.RequirementResult{State: extension.RequirementFound, Path: found.Client.Endpoint().String(), Version: found.Info.Version, Detail: found.Source}
	}
	if ctx.Err() != nil {
		return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check was stopped"}
	}
	why := err.Error()
	if appErr, ok := errors.FromError(err); ok {
		why = appErr.Message
	}
	_, cliErr := cfg.lookPath(cfg.cliName())
	switch {
	case cfg.Kind == containerapi.KindPodman && cliErr == nil && env.StringSetting(cfg.HostSetting()) == "":
		return extension.RequirementResult{State: extension.RequirementFound, Detail: "no Podman socket answers; the view starts a podman system service of Emetos's own"}
	case cliErr == nil:
		return extension.RequirementResult{State: extension.RequirementUnusable, Detail: cfg.cliName() + " is installed, but " + why}
	}
	return extension.RequirementResult{State: extension.RequirementMissing, Detail: why}
}
