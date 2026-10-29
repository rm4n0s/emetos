package containerui

import (
	"github.com/rm4n0s/emetos/extension"
)

// Manifest declares what the extension of a Config contributes: the view container with its sections, the panel view for the
// logs, the editor kinds, the commands with their palette entries and the actions in the headers of the views.
func Manifest(cfg Config) extension.Manifest {
	cmdID := cfg.Command
	var cmds []extension.CommandSpec
	var menus []extension.MenuItem
	for _, def := range commandTable(cfg) {
		cmds = append(cmds, extension.CommandSpec{ID: cmdID(def.name), Title: def.title, Category: cfg.Name})
		when := ""
		switch {
		case def.name == "retry":
			when = "!" + cfg.keyConnected()
		case def.needsEngine:
			when = cfg.keyConnected()
		}
		// the commands that stand for a button of the panel are in the palette while there is a tab
		switch def.name {
		case "logs.pause":
			when = cfg.keyLogs() + " && !" + cfg.keyLogsPaused()
		case "logs.resume":
			when = cfg.keyLogsPaused()
		case "logs.clear", "logs.close":
			when = cfg.keyLogs()
		case "images.showDangling":
			when = cfg.keyConnected() + " && !" + cfg.keyDanglingShown()
		case "images.hideDangling":
			when = cfg.keyConnected() + " && " + cfg.keyDanglingShown()
		case "containers.clearFilter":
			when = cfg.keyContainersFiltered()
		case "images.clearFilter":
			when = cfg.keyImagesFiltered()
		}
		if when != "" {
			menus = append(menus, extension.MenuItem{Menu: extension.MenuCommandPalette, Command: cmdID(def.name), When: when})
		}
	}

	view := func(section, name string, collapsed bool, when string, order int) extension.ViewSpec {
		return extension.ViewSpec{ID: cfg.ViewID(section), Name: name, Container: cfg.ContainerID(), When: when, Order: order, Collapsed: collapsed}
	}
	views := []extension.ViewSpec{
		view(secContainers, "Containers", false, "", 1),
		view(secImages, "Images", false, cfg.keyConnected(), 2),
		view(secNetworks, "Networks", true, cfg.keyConnected(), 3),
		view(secVolumes, "Volumes", true, cfg.keyConnected(), 4),
	}
	if cfg.Contexts {
		views = append(views, view(secContexts, "Contexts", true, "", 5))
	}
	act := func(section, name, icon, tooltip, when string, order float64) extension.ViewActionSpec {
		return extension.ViewActionSpec{View: cfg.ViewID(section), Command: cmdID(name), Icon: icon, Tooltip: tooltip, When: when, Order: order}
	}
	c := cfg.keyConnected()
	actions := []extension.ViewActionSpec{
		act(secContainers, "containers.prune", "clear-all", "Prune...", c, 1),
		act(secContainers, "containers.filter", "search", "Filter...", c+" && !"+cfg.keyContainersFiltered(), 2),
		act(secContainers, "containers.clearFilter", "search-stop", "Clear Filter", cfg.keyContainersFiltered(), 2),
		act(secContainers, "containers.refresh", "refresh", "Refresh", "", 9),
		act(secImages, "images.pull", "cloud-download", "Pull...", "", 1),
		act(secImages, "images.prune", "clear-all", "Prune...", "", 2),
		act(secImages, "images.filter", "search", "Filter...", "!"+cfg.keyImagesFiltered(), 3),
		act(secImages, "images.clearFilter", "search-stop", "Clear Filter", cfg.keyImagesFiltered(), 3),
		act(secImages, "images.hideDangling", "eye-closed", "Hide Dangling Images", cfg.keyDanglingShown(), 4),
		act(secImages, "images.showDangling", "eye", "Show Dangling Images", "!"+cfg.keyDanglingShown(), 4),
		act(secImages, "images.refresh", "refresh", "Refresh", "", 9),
		act(secNetworks, "networks.create", "add", "Create...", "", 1),
		act(secNetworks, "networks.prune", "clear-all", "Prune...", "", 2),
		act(secNetworks, "networks.refresh", "refresh", "Refresh", "", 9),
		act(secVolumes, "volumes.create", "add", "Create...", "", 1),
		act(secVolumes, "volumes.prune", "clear-all", "Prune...", "", 2),
		act(secVolumes, "volumes.refresh", "refresh", "Refresh", "", 9),
	}
	if cfg.Contexts {
		actions = append(actions,
			act(secContexts, "contexts.use", "vm-connect", "Pick a Context...", "", 1),
			act(secContexts, "contexts.refresh", "refresh", "Refresh", "", 9))
	}
	logsView := cfg.LogsViewID()
	actions = append(actions,
		extension.ViewActionSpec{View: logsView, Command: cmdID("logs.pause"), Icon: "debug-pause", Tooltip: "Pause", When: cfg.keyLogs() + " && !" + cfg.keyLogsPaused(), Order: 1},
		extension.ViewActionSpec{View: logsView, Command: cmdID("logs.resume"), Icon: "debug-continue", Tooltip: "Resume", When: cfg.keyLogsPaused(), Order: 1},
		extension.ViewActionSpec{View: logsView, Command: cmdID("logs.clear"), Icon: "clear-all", Tooltip: "Clear", When: cfg.keyLogs(), Order: 2},
		extension.ViewActionSpec{View: logsView, Command: cmdID("logs.close"), Icon: "close", Tooltip: "Close Tab", When: cfg.keyLogs(), Order: 3},
	)

	return extension.Manifest{
		ID: cfg.ID, Name: cfg.Name, Version: "0.1.0", Description: cfg.Description,
		ActivationEvents: []string{extension.OnView(cfg.ViewID(secContainers)), extension.OnView(cfg.ViewID(secImages)), extension.OnView(cfg.ViewID(secNetworks)),
			extension.OnView(cfg.ViewID(secVolumes))},
		Contributes: extension.Contributes{
			Commands: cmds,
			Menus:    menus,
			ViewContainers: []extension.ViewContainerSpec{
				{ID: cfg.ContainerID(), Title: cfg.Name, Icon: cfg.Icon, Order: cfg.Order},
			},
			Views:       views,
			PanelViews:  []extension.PanelViewSpec{{ID: logsView, Title: cfg.Name + " Logs", Icon: "output", Order: 6}},
			ViewActions: actions,
			EditorKinds: []extension.EditorKindSpec{
				{ID: cfg.DocumentKind(), Name: cfg.Name, Icon: "file-code"},
				{ID: cfg.RunKind(), Name: "Run " + cfg.Engine + " Image", Icon: "play"},
			},
			Settings: []extension.SettingSpec{{
				Key: cfg.HostSetting(), Type: "string", Default: "",
				Description: "The address of the " + cfg.Engine + " engine: a socket (`unix:///run/user/1000/" + cfg.Prefix + ".sock`) or `tcp://host:2376`. Empty looks for the engine the way the " + cfg.Engine + " command line does.",
				Category:    cfg.Name,
			}},
		},
	}
}
