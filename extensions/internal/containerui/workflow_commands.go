package containerui

import (
	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// Ported from vscode-containers: extensions/vscode-containers/package.json (contributes.commands and contributes.menus: the ids, titles
// and groups of Build Image, Compose Up and its relatives, Compose Group, Run Interactive, Push, Attach Shell and Add Docker Files).

// workflowCommands are the commands of step 18.3. Those that run only the CLI (Build Image, Compose Up, Compose Logs, the scaffolding)
// work while no engine answers; those that ask the engine for a container or an image, or that ask before they take a project down,
// need one.
func workflowCommands(cfg Config) []cmdDef {
	file := func(act composeAction) func(e *ext) extension.CommandHandler {
		return withArgs(func(e *ext, a any) { e.composeOnFiles(a, act) })
	}
	group := func(act composeGroupAction) func(e *ext) extension.CommandHandler {
		return withArgs(func(e *ext, a any) { e.composeGroupCommand(a, act) })
	}
	return []cmdDef{
		{name: "images.build", title: "Build Image...", handler: withArgs(func(e *ext, a any) { e.buildImage(a) })},
		{name: "images.push", title: "Push...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withImages(a, "push", func(rs []imageRow) { e.pushImage(rs[0]) })
		})},
		{name: "images.runInteractive", title: "Run Interactive", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withImages(a, "run", func(rs []imageRow) { e.runImageInteractive(rs[0]) })
		})},
		{name: "containers.attachShell", title: "Open Shell", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "open a shell in", containerapi.Container.Running, func(cs []containerapi.Container) { e.openShell(cs[0]) })
		})},
		{name: "compose.up", title: "Compose Up", handler: file(composeUp)},
		{name: "compose.up.subset", title: "Compose Up - Select Services", handler: file(composeUpSubset)},
		{name: "compose.down", title: "Compose Down", needsEngine: true, handler: file(composeDown)},
		{name: "compose.down.subset", title: "Compose Down - Select Services", needsEngine: true, handler: file(composeDownSubset)},
		{name: "compose.restart", title: "Compose Restart", needsEngine: true, handler: file(composeRestart)},
		{name: "compose.logs", title: "Compose Logs", handler: file(composeLogs)},
		{name: "containers.composeGroup.logs", title: "Compose Logs (Project)", needsEngine: true, handler: group(groupLogs)},
		{name: "containers.composeGroup.start", title: "Compose Start (Project)", needsEngine: true, handler: group(groupStart)},
		{name: "containers.composeGroup.stop", title: "Compose Stop (Project)", needsEngine: true, handler: group(groupStop)},
		{name: "containers.composeGroup.restart", title: "Compose Restart (Project)", needsEngine: true, handler: group(groupRestart)},
		{name: "containers.composeGroup.down", title: "Compose Down (Project)", needsEngine: true, handler: group(groupDown)},
		{name: "configure", title: "Add " + cfg.Engine + " Files to Workspace...", handler: withArgs(func(e *ext, _ any) { e.scaffold() })},
	}
}

// workflowMenus are the menu items of step 18.3: the context menus of the Explorer and of the editor on a Dockerfile and on a Compose
// file (VS Code's explorer/context and editor/context groups "containers").
func workflowMenus(cfg Config) []extension.MenuItem {
	var items []extension.MenuItem
	titles := map[string]string{}
	for _, def := range workflowCommands(cfg) {
		titles[def.name] = def.title
	}
	add := func(menu, when, command string, order float64) {
		item := extension.MenuItem{Menu: menu, Command: cfg.Command(command), Group: "containers", Order: order, When: when}
		if cfg.MenuTitlePrefix != "" {
			item.Title = cfg.MenuTitlePrefix + titles[command]
		}
		items = append(items, item)
	}
	for _, m := range []struct{ menu, dockerfile, compose string }{
		{extension.MenuExplorerContext, "resourceLangId == dockerfile", "resourceLangId == dockercompose"},
		{extension.MenuEditorContext, "editorTextFocus && editorLangId == dockerfile", "editorTextFocus && editorLangId == dockercompose"},
	} {
		add(m.menu, m.dockerfile, "images.build", 1)
		add(m.menu, m.compose, "compose.up", 1)
		add(m.menu, m.compose, "compose.up.subset", 2)
		add(m.menu, m.compose, "compose.down", 3)
		add(m.menu, m.compose, "compose.down.subset", 4)
		add(m.menu, m.compose, "compose.restart", 5)
		add(m.menu, m.compose, "compose.logs", 6)
	}
	return items
}

// workflowSettings are the settings of step 18.3 (Container Tools' containers.composeBuild, composeDetached and
// imageBuildContextPath).
func workflowSettings(cfg Config) []extension.SettingSpec {
	return []extension.SettingSpec{
		{Key: cfg.settingComposeBuild(), Type: "boolean", Default: true, Category: cfg.Name,
			Description: "Whether Compose Up builds the images first (`up --build`)."},
		{Key: cfg.settingComposeDetached(), Type: "boolean", Default: true, Category: cfg.Name,
			Description: "Whether Compose Up starts the containers in the background (`up -d`)."},
		{Key: cfg.settingBuildContextPath(), Type: "string", Default: "", Category: cfg.Name,
			Description: "The folder that is the build context of Build Image, relative to the workspace folder. Empty uses the folder of the Dockerfile."},
	}
}

// workflowServers are the language servers of the Dockerfile and Compose languages (SPEC §10.20 "Language support"): each is declared
// only when its program is found on PATH, and never installed. The user may name another program in the setting
// <prefix>.dockerfileLanguageServer.path and <prefix>.composeLanguageServer.path.
func workflowServers(cfg Config) []extension.LanguageServerSpec {
	if !cfg.LanguageServers {
		return nil
	}
	var out []extension.LanguageServerSpec
	add := func(id, name, language, program, pathSetting string) {
		if _, err := cfg.lookPath(program); err != nil {
			return
		}
		out = append(out, extension.LanguageServerSpec{
			ID: id, Name: name, Languages: []string{language}, Program: program, Args: []string{"--stdio"},
			PathSetting: pathSetting, Category: cfg.Name,
		})
	}
	dockerfile, compose := cfg.DockerfileServer, cfg.ComposeServer
	if dockerfile == "" {
		dockerfile = "docker-langserver"
	}
	if compose == "" {
		compose = "docker-compose-langserver"
	}
	add("dockerfile", "Dockerfile Language Server", "dockerfile", dockerfile, cfg.DockerfileServerPathSetting())
	add("compose", "Compose Language Server", "dockercompose", compose, cfg.ComposeServerPathSetting())
	return out
}
