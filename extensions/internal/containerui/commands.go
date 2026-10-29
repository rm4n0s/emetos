package containerui

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// Ported from vscode-containers: extensions/vscode-containers/package.json (contributes.commands: the ids and titles, with the prefix
// of the Config in place of vscode-containers).

// cmdDef declares a command of the extension: its name (after the prefix), its title and what it does.
type cmdDef struct {
	name, title string
	// handler makes the function that runs the command.
	handler func(e *ext) extension.CommandHandler
	// needsEngine hides the command from the command palette while no engine is connected.
	needsEngine bool
}

// simple wraps a function that does not read the arguments.
func simple(fn func(e *ext)) func(e *ext) extension.CommandHandler {
	return func(e *ext) extension.CommandHandler {
		return func(context.Context, any) error { fn(e); return nil }
	}
}

// withArgs wraps a function that reads the arguments of the command (the key of the row it was run on).
func withArgs(fn func(e *ext, args any)) func(e *ext) extension.CommandHandler {
	return func(e *ext) extension.CommandHandler {
		return func(_ context.Context, args any) error { fn(e, args); return nil }
	}
}

// commandTable lists the commands (Container Tools' ids with the prefix of the Config in place of vscode-containers).
func commandTable(cfg Config) []cmdDef {
	t := []cmdDef{
		{name: "containers.start", title: "Start", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "start", func(c containerapi.Container) bool { return !c.Running() }, func(cs []containerapi.Container) { e.runContainerOp(opStart, idsOf(cs)) })
		})},
		{name: "containers.stop", title: "Stop", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "stop", containerapi.Container.Running, func(cs []containerapi.Container) { e.runContainerOp(opStop, idsOf(cs)) })
		})},
		{name: "containers.restart", title: "Restart", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "restart", containerapi.Container.Running, func(cs []containerapi.Container) { e.runContainerOp(opRestart, idsOf(cs)) })
		})},
		{name: "containers.pause", title: "Pause", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "pause", func(c containerapi.Container) bool { return c.State == "running" }, func(cs []containerapi.Container) { e.runContainerOp(opPause, idsOf(cs)) })
		})},
		{name: "containers.unpause", title: "Unpause", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "unpause", func(c containerapi.Container) bool { return c.State == "paused" }, func(cs []containerapi.Container) { e.runContainerOp(opUnpause, idsOf(cs)) })
		})},
		{name: "containers.kill", title: "Kill", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "kill", containerapi.Container.Running, func(cs []containerapi.Container) { e.runContainerOp(opKill, idsOf(cs)) })
		})},
		{name: "containers.remove", title: "Remove...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "remove", nil, func(cs []containerapi.Container) { e.askRemoveContainers(idsOf(cs)) })
		})},
		{name: "containers.viewLogs", title: "View Logs", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "show the logs of", nil, func(cs []containerapi.Container) {
				for _, c := range cs {
					e.viewLogs(c.ID)
				}
			})
		})},
		{name: "containers.inspect", title: "Inspect", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "inspect", nil, func(cs []containerapi.Container) { e.inspectContainer(cs[0].ID) })
		})},
		{name: "containers.browse", title: "Open in Browser", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "open in the browser", func(c containerapi.Container) bool { _, _, ok := publishedHTTP(c); return ok && c.Running() },
				func(cs []containerapi.Container) { e.openInBrowser(cs[0]) })
		})},
		{name: "containers.copyId", title: "Copy ID", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "copy the id of", nil, func(cs []containerapi.Container) { e.copyIDs(idsOf(cs)) })
		})},
		{name: "containers.runCommand", title: "Run Command...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "run a command in", containerapi.Container.Running, func(cs []containerapi.Container) { e.runCommandIn(cs[0].ID) })
		})},
		{name: "containers.exploreFiles", title: "Explore Files", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "explore the files of", containerapi.Container.Running, func(cs []containerapi.Container) { e.exploreFiles(cs[0].ID) })
		})},
		{name: "containers.prune", title: "Prune...", needsEngine: true, handler: simple((*ext).askPruneContainers)},
		{name: "containers.filter", title: "Filter...", needsEngine: true, handler: simple(func(e *ext) { e.askFilter(resContainers) })},
		{name: "containers.clearFilter", title: "Clear Filter", needsEngine: true, handler: simple(func(e *ext) { e.setFilter(resContainers, "") })},
		{name: "containers.refresh", title: "Refresh", handler: simple(func(e *ext) { e.refreshAll() })},

		{name: "images.run", title: "Run...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withImages(a, "run", func(rs []imageRow) { e.runImage(rs[0]) })
		})},
		{name: "images.pull", title: "Pull...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			if rows := e.imageRowsOfKeys(e.views[secImages].keysFor(a)); len(rows) == 1 && rows[0].ref != "" && hasKey(a) {
				e.pullImage(rows[0].ref)
				return
			}
			e.pullImage("")
		})},
		{name: "images.tag", title: "Tag...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withImages(a, "tag", func(rs []imageRow) { e.tagImage(rs[0]) })
		})},
		{name: "images.remove", title: "Remove...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withImages(a, "remove", func(rs []imageRow) { e.askRemoveImages(rs) })
		})},
		{name: "images.inspect", title: "Inspect", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withImages(a, "inspect", func(rs []imageRow) { e.inspectImage(rs[0]) })
		})},
		{name: "images.history", title: "History", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withImages(a, "show the history of", func(rs []imageRow) { e.imageHistory(rs[0]) })
		})},
		{name: "images.copyFullTag", title: "Copy Full Tag", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withImages(a, "copy the tag of", func(rs []imageRow) { e.copyText(rs[0].ref) })
		})},
		{name: "images.prune", title: "Prune...", needsEngine: true, handler: simple((*ext).askPruneImages)},
		{name: "images.filter", title: "Filter...", needsEngine: true, handler: simple(func(e *ext) { e.askFilter(resImages) })},
		{name: "images.clearFilter", title: "Clear Filter", needsEngine: true, handler: simple(func(e *ext) { e.setFilter(resImages, "") })},
		{name: "images.showDangling", title: "Show Dangling Images", needsEngine: true, handler: simple(func(e *ext) { e.setDangling(true) })},
		{name: "images.hideDangling", title: "Hide Dangling Images", needsEngine: true, handler: simple(func(e *ext) { e.setDangling(false) })},
		{name: "images.refresh", title: "Refresh", handler: simple(func(e *ext) { e.refreshAll() })},

		{name: "networks.create", title: "Create...", needsEngine: true, handler: simple((*ext).createNetwork)},
		{name: "networks.remove", title: "Remove...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withNetworks(a, "remove", func(ns []containerapi.Network) { e.askRemoveNetworks(ns) })
		})},
		{name: "networks.inspect", title: "Inspect", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withNetworks(a, "inspect", func(ns []containerapi.Network) { e.inspectNetwork(ns[0]) })
		})},
		{name: "networks.prune", title: "Prune...", needsEngine: true, handler: simple((*ext).askPruneNetworks)},
		{name: "networks.refresh", title: "Refresh", handler: simple(func(e *ext) { e.refreshAll() })},

		{name: "volumes.create", title: "Create...", needsEngine: true, handler: simple((*ext).createVolume)},
		{name: "volumes.remove", title: "Remove...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withVolumes(a, "remove", func(vs []containerapi.Volume) { e.askRemoveVolumes(vs) })
		})},
		{name: "volumes.inspect", title: "Inspect", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withVolumes(a, "inspect", func(vs []containerapi.Volume) { e.inspectVolume(vs[0]) })
		})},
		{name: "volumes.prune", title: "Prune...", needsEngine: true, handler: simple((*ext).askPruneVolumes)},
		{name: "volumes.refresh", title: "Refresh", handler: simple(func(e *ext) { e.refreshAll() })},

		{name: "pruneSystem", title: "Prune System...", needsEngine: true, handler: simple((*ext).askPruneSystem)},
		{name: "retry", title: "Retry Connection", handler: simple((*ext).retry)},
		{name: "logs.pause", title: "Pause Logs", handler: simple(func(e *ext) { e.logs.togglePause() })},
		{name: "logs.resume", title: "Resume Logs", handler: simple(func(e *ext) { e.logs.togglePause() })},
		{name: "logs.clear", title: "Clear Logs", handler: simple(func(e *ext) { e.logs.clear() })},
		{name: "logs.close", title: "Close Logs Tab", handler: simple(func(e *ext) { e.logs.closeActive() })},
	}
	if cfg.Contexts {
		t = append(t,
			cmdDef{name: "contexts.use", title: "Use", handler: withArgs(func(e *ext, a any) {
				if key := keyArg(a); strings.HasPrefix(key, keyContext) {
					e.useContext(strings.TrimPrefix(key, keyContext))
					return
				}
				e.pickContext()
			})},
			cmdDef{name: "contexts.inspect", title: "Inspect", handler: withArgs(func(e *ext, a any) {
				if key := keyArg(a); strings.HasPrefix(key, keyContext) {
					e.inspectContext(strings.TrimPrefix(key, keyContext))
				}
			})},
			cmdDef{name: "contexts.refresh", title: "Refresh", handler: simple(func(e *ext) { e.loadContexts() })},
		)
	}
	return t
}

// keyArg returns the key a command was run on ({"key": ...}), else "".
func keyArg(args any) string {
	if m, ok := args.(map[string]any); ok {
		if key, ok := m["key"].(string); ok {
			return key
		}
	}
	return ""
}

// hasKey tells whether the arguments name a row.
func hasKey(args any) bool { return keyArg(args) != "" }

// ---- finding what a command works on ----

// withContainers calls fn with the containers a command works on: the row it was run on, else the selection of the view, else
// the one the user picks from the list. ok limits it to the containers the action can be done to; verb says what the action is
// for the messages.
func (e *ext) withContainers(args any, verb string, ok func(containerapi.Container) bool, fn func([]containerapi.Container)) {
	if _, ready := e.ready(); !ready {
		return
	}
	var cs []containerapi.Container
	for _, c := range e.containersOfKeys(e.views[secContainers].keysFor(args)) {
		if ok == nil || ok(c) {
			cs = append(cs, c)
		}
	}
	if len(cs) > 0 {
		fn(cs)
		return
	}
	var cands []containerapi.Container
	for _, c := range sortedContainers(e.containers) {
		if ok == nil || ok(c) {
			cands = append(cands, c)
		}
	}
	if len(cands) == 0 {
		e.api.Notifications().Info("There is no container to " + verb + ".")
		return
	}
	items := make([]extension.QuickPickItem, len(cands))
	for i, c := range cands {
		items[i] = extension.QuickPickItem{Label: c.Name(), Description: c.Image, Detail: statusText(c, e.cfg.now())}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a container to " + verb, Items: items, MatchOnDescription: true}, func(i int) {
		if i >= 0 && i < len(cands) {
			fn([]containerapi.Container{cands[i]})
		}
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}

// withImages is withContainers for the names of images.
func (e *ext) withImages(args any, verb string, fn func([]imageRow)) {
	if _, ready := e.ready(); !ready {
		return
	}
	if rows := e.imageRowsOfKeys(e.views[secImages].keysFor(args)); len(rows) > 0 {
		fn(rows)
		return
	}
	var cands []imageRow
	for _, g := range groupImages(e.images, e.showDangling) {
		cands = append(cands, g.rows...)
	}
	if len(cands) == 0 {
		e.api.Notifications().Info("There is no image to " + verb + ".")
		return
	}
	items := make([]extension.QuickPickItem, len(cands))
	for i, r := range cands {
		items[i] = extension.QuickPickItem{Label: nameOr(r.ref, containerapi.ShortID(r.image.ID)), Description: humanSize(r.image.Size)}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select an image to " + verb, Items: items}, func(i int) {
		if i >= 0 && i < len(cands) {
			fn([]imageRow{cands[i]})
		}
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}

// nameOr returns s, or the fallback when it is empty.
func nameOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// withNetworks is withContainers for networks.
func (e *ext) withNetworks(args any, verb string, fn func([]containerapi.Network)) {
	if _, ready := e.ready(); !ready {
		return
	}
	if ns := e.networksOfKeys(e.views[secNetworks].keysFor(args)); len(ns) > 0 {
		fn(ns)
		return
	}
	if len(e.networks) == 0 {
		e.api.Notifications().Info("There is no network to " + verb + ".")
		return
	}
	items := make([]extension.QuickPickItem, len(e.networks))
	for i, n := range e.networks {
		items[i] = extension.QuickPickItem{Label: n.Name, Description: n.Driver}
	}
	ns := e.networks
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a network to " + verb, Items: items}, func(i int) {
		if i >= 0 && i < len(ns) {
			fn([]containerapi.Network{ns[i]})
		}
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}

// withVolumes is withContainers for volumes.
func (e *ext) withVolumes(args any, verb string, fn func([]containerapi.Volume)) {
	if _, ready := e.ready(); !ready {
		return
	}
	if vs := e.volumesOfKeys(e.views[secVolumes].keysFor(args)); len(vs) > 0 {
		fn(vs)
		return
	}
	if len(e.volumes) == 0 {
		e.api.Notifications().Info("There is no volume to " + verb + ".")
		return
	}
	items := make([]extension.QuickPickItem, len(e.volumes))
	for i, v := range e.volumes {
		items[i] = extension.QuickPickItem{Label: v.Name, Description: v.Driver}
	}
	vs := e.volumes
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a volume to " + verb, Items: items}, func(i int) {
		if i >= 0 && i < len(vs) {
			fn([]containerapi.Volume{vs[i]})
		}
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}

// ---- filters, refresh, dangling ----

// askFilter asks for the text a section is filtered by.
func (e *ext) askFilter(r resource) {
	err := e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Show only the " + sectionOf(r) + " that match (name, image, id, state or project; empty shows all)",
		Value:  e.filters[r],
	}, func(v string, ok bool) {
		if ok {
			e.setFilter(r, strings.TrimSpace(v))
		}
	})
	if err != nil {
		e.api.Log().Warn("the input box cannot be shown", "err", err)
	}
}

// setFilter sets the filter of a section.
func (e *ext) setFilter(r resource, text string) {
	e.filters[r] = text
	switch r {
	case resContainers:
		e.setKey(e.cfg.keyContainersFiltered(), text != "")
	case resImages:
		e.setKey(e.cfg.keyImagesFiltered(), text != "")
	}
	e.rebuild(r)
}

// refreshAll reads everything again (also after an engine that went away came back).
func (e *ext) refreshAll() {
	if e.client == nil {
		e.connect()
		return
	}
	e.files.forget()
	e.reloadAll()
}

// setDangling shows or hides the images that have no name, and lists the images again.
func (e *ext) setDangling(show bool) {
	if e.showDangling == show {
		return
	}
	e.showDangling = show
	e.setKey(e.cfg.keyDanglingShown(), show)
	e.load(resImages)
	e.rebuild(resImages)
}
