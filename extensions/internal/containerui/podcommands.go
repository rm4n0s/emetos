package containerui

import (
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// The commands, menus and view actions of what only Podman has (SPEC §10.20 "Podman only", step 18.4): pods, connections, Kube and
// Quadlet. They are in the table of the Config only when it has Pods (or Connections).

// podCommands are the commands of pods, Kube and Quadlet.
func podCommands(cfg Config) []cmdDef {
	if !cfg.Pods {
		return nil
	}
	podOp := func(op podOp, verb string, ok func(containerapi.Pod) bool) func(e *ext) extension.CommandHandler {
		return withArgs(func(e *ext, a any) {
			e.withPods(a, verb, ok, func(ps []containerapi.Pod) { e.runPodOp(op, podIDs(ps)) })
		})
	}
	return []cmdDef{
		{name: "pods.start", title: "Start", needsEngine: true, handler: podOp(podOpStart, "start", func(p containerapi.Pod) bool { return !strings.EqualFold(p.Status, "running") })},
		{name: "pods.stop", title: "Stop", needsEngine: true, handler: podOp(podOpStop, "stop", podRunning)},
		{name: "pods.restart", title: "Restart", needsEngine: true, handler: podOp(podOpRestart, "restart", podRunning)},
		{name: "pods.pause", title: "Pause", needsEngine: true, handler: podOp(podOpPause, "pause", func(p containerapi.Pod) bool { return strings.EqualFold(p.Status, "running") })},
		{name: "pods.unpause", title: "Unpause", needsEngine: true, handler: podOp(podOpUnpause, "unpause", func(p containerapi.Pod) bool { return strings.EqualFold(p.Status, "paused") })},
		{name: "pods.remove", title: "Remove...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withPods(a, "remove", nil, func(ps []containerapi.Pod) { e.askRemovePods(podIDs(ps)) })
		})},
		{name: "pods.inspect", title: "Inspect", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withPods(a, "inspect", nil, func(ps []containerapi.Pod) { e.inspectPod(ps[0]) })
		})},
		{name: "pods.copyId", title: "Copy ID", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withPods(a, "copy the id of", nil, func(ps []containerapi.Pod) { e.copyIDs(podIDs(ps)) })
		})},
		{name: "pods.generateKube", title: "Generate Kube YAML", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withPods(a, "generate Kube YAML for", nil, func(ps []containerapi.Pod) { e.generateKube(kubeSubject{kind: "pod", names: podNames(ps)}) })
		})},
		{name: "pods.create", title: "Create Pod...", needsEngine: true, handler: simple((*ext).createPod)},
		{name: "pods.prune", title: "Prune...", needsEngine: true, handler: simple((*ext).askPrunePods)},
		{name: "pods.refresh", title: "Refresh", handler: simple(func(e *ext) { e.refreshAll() })},

		{name: "containers.createPod", title: "Create Pod from Selected Containers...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "put in a new pod", nil, func(cs []containerapi.Container) { e.createPodFrom(idsOf(cs)) })
		})},
		{name: "containers.generateKube", title: "Generate Kube YAML", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "generate Kube YAML for", nil, func(cs []containerapi.Container) { e.generateKube(kubeSubjectsOfContainers(cs)) })
		})},
		{name: "containers.generateQuadlet", title: "Generate Quadlet...", needsEngine: true, handler: withArgs(func(e *ext, a any) {
			e.withContainers(a, "generate a Quadlet unit for", nil, func(cs []containerapi.Container) { e.generateQuadletFor(cs[0]) })
		})},
		{name: "kube.play", title: "Play Kube...", handler: withArgs(func(e *ext, a any) { e.kubeOnFiles(a, false) })},
		{name: "kube.down", title: "Kube Down...", needsEngine: true, handler: withArgs(func(e *ext, a any) { e.kubeOnFiles(a, true) })},
	}
}

// connectionCommands are the commands of the Connections section.
func connectionCommands(cfg Config) []cmdDef {
	if !cfg.Connections {
		return nil
	}
	return []cmdDef{
		{name: "connections.use", title: "Use", handler: withArgs(func(e *ext, a any) {
			if key := keyArg(a); strings.HasPrefix(key, keyConnection) {
				e.useConnection(strings.TrimPrefix(key, keyConnection))
				return
			}
			e.pickConnection()
		})},
		{name: "connections.inspect", title: "Inspect", handler: withArgs(func(e *ext, a any) {
			if key := keyArg(a); strings.HasPrefix(key, keyConnection) {
				e.inspectConnection(strings.TrimPrefix(key, keyConnection))
			}
		})},
		{name: "connections.refresh", title: "Refresh", handler: simple((*ext).loadConnections)},
	}
}

// podMenus are the items of the Explorer's and the editor's menus on a Kubernetes YAML file.
func podMenus(cfg Config) []extension.MenuItem {
	if !cfg.Pods {
		return nil
	}
	var items []extension.MenuItem
	for _, m := range []struct{ menu, when string }{
		{extension.MenuExplorerContext, "resourceLangId == yaml"},
		{extension.MenuEditorContext, "editorTextFocus && editorLangId == yaml"},
	} {
		items = append(items,
			extension.MenuItem{Menu: m.menu, Command: cfg.Command("kube.play"), Title: cfg.MenuTitlePrefix + "Play Kube...", Group: "containers", Order: 10, When: m.when},
			extension.MenuItem{Menu: m.menu, Command: cfg.Command("kube.down"), Title: cfg.MenuTitlePrefix + "Kube Down...", Group: "containers", Order: 11, When: m.when})
	}
	return items
}

// podViews are the views only Podman has; order is the order of the first of them in the side bar.
func podViewActions(cfg Config) []extension.ViewActionSpec {
	act := func(section, name, icon, tooltip, when string, order float64) extension.ViewActionSpec {
		return extension.ViewActionSpec{View: cfg.ViewID(section), Command: cfg.Command(name), Icon: icon, Tooltip: tooltip, When: when, Order: order}
	}
	var out []extension.ViewActionSpec
	if cfg.Pods {
		out = append(out,
			act(secPods, "pods.create", "add", "Create Pod...", "", 1),
			act(secPods, "pods.prune", "clear-all", "Prune...", "", 2),
			act(secPods, "pods.refresh", "refresh", "Refresh", "", 9))
	}
	if cfg.Connections {
		out = append(out,
			act(secConnections, "connections.use", "vm-connect", "Pick a Connection...", "", 1),
			act(secConnections, "connections.refresh", "refresh", "Refresh", "", 9))
	}
	return out
}
