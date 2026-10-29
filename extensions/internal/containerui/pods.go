package containerui

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// Pods (SPEC §10.20 "Podman only"): the Pods section, the containers of the Containers section grouped under their pod, and the actions
// on pods: create (empty, or from the settings of the selected containers), start, stop, restart, pause, unpause, remove, prune and
// inspect. Container Tools has no pods; the rows follow the design of its other sections.

// keyPod starts the key of the row of a pod in the Pods section; the group of a pod in the Containers section is keyGroup + podGroup.
const (
	keyPod   = "p:"
	podGroup = "pod:"
)

// podNode is a pod in the Pods section.
type podNode struct{ p containerapi.Pod }

// podOp is an action on pods that needs no question.
type podOp int

// The actions on pods that run at once.
const (
	podOpStart podOp = iota
	podOpStop
	podOpRestart
	podOpPause
	podOpUnpause
)

// verb is how the user calls the action.
func (o podOp) verb() string {
	return [...]string{"start", "stop", "restart", "pause", "unpause"}[o]
}

// call makes the engine do the action to one pod.
func (o podOp) call(ctx context.Context, c *containerapi.Client, id string) error {
	switch o {
	case podOpStart:
		return c.PodDo(ctx, id, containerapi.PodStart)
	case podOpStop:
		return c.StopPod(ctx, id, stopTimeout)
	case podOpRestart:
		return c.PodDo(ctx, id, containerapi.PodRestart)
	case podOpPause:
		return c.PodDo(ctx, id, containerapi.PodPause)
	}
	return c.PodDo(ctx, id, containerapi.PodUnpause)
}

// podRunning tells whether a pod has anything that runs: Podman calls its state Running, Degraded (some of its containers run) or
// Paused.
func podRunning(p containerapi.Pod) bool {
	switch strings.ToLower(p.Status) {
	case "running", "degraded", "paused":
		return true
	}
	return false
}

// podIcon is the Codicon and the color of the state of a pod, in the family of the icons of containers.
func podIcon(colors ui.Colors, p containerapi.Pod) (string, gui.Color) {
	switch strings.ToLower(p.Status) {
	case "running":
		return "debug-start", colors.DebugIconStartForeground
	case "paused":
		return "debug-pause", colors.DebugIconPauseForeground
	case "degraded":
		return "warning", colors.ProblemsWarningIconForeground
	}
	return "debug-stop", colors.DebugIconStopForeground
}

// podDescription is the line after the name of a pod: the state and how many containers it has.
func podDescription(p containerapi.Pod) string {
	n := len(p.Containers)
	return strings.Join(nonEmpty(strings.ToLower(p.Status), plural(n, "container")), " · ")
}

// ---- the section ----

type podsSection struct{}

func (*podsSection) name() string  { return secPods }
func (*podsSection) res() resource { return resPods }
func (*podsSection) title() string { return "Pods" }

func (s *podsSection) items(e *ext) ([]ui.TreeItem, string) {
	l := e.loads[resPods]
	if l.err != "" {
		return nil, "Cannot list the pods: " + l.err
	}
	if !l.loaded {
		return nil, ""
	}
	var items []ui.TreeItem
	for _, p := range e.pods {
		items = append(items, ui.TreeItem{Key: keyPod + p.ID, Label: p.Name, Data: podNode{p}})
	}
	if len(items) == 0 {
		return nil, "No pods found."
	}
	return items, ""
}

func (s *podsSection) render(bc widgets.BuildContext, v *sectionView, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	e := v.e
	d, ok := node.Data.(podNode)
	if !ok {
		return text(bc, node.Label, 13, st.Foreground)
	}
	p := d.p
	icon, color := podIcon(colors, p)
	row := []widgets.Widget{ui.Icon(icon, 16, color), gap(6)}
	row = append(row, labelAndDescription(bc, colors, st, p.Name, podDescription(p))...)
	if podRunning(p) {
		row = append(row, rowButton(st, "debug-stop", "Stop", func() { e.runPodOp(podOpStop, []string{p.ID}) }))
	} else {
		row = append(row, rowButton(st, "debug-start", "Start", func() { e.runPodOp(podOpStart, []string{p.ID}) }))
	}
	return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

func (s *podsSection) menu(v *sectionView, keys []string) []ui.MenuItem {
	e := v.e
	ps := e.podsOfKeys(keys)
	if len(ps) == 0 {
		return nil
	}
	return e.podMenu(ps)
}

func (s *podsSection) open(v *sectionView, node ui.TreeNode) {
	if d, ok := node.Data.(podNode); ok {
		v.e.inspectPod(d.p)
	}
}

// podMenu is the menu of one or several pods (the Pods section, and the group of a pod in the Containers section).
func (e *ext) podMenu(ps []containerapi.Pod) []ui.MenuItem {
	ids := podIDs(ps)
	anyRunning, allRunning, anyPaused := false, true, false
	for _, p := range ps {
		anyRunning = anyRunning || podRunning(p)
		allRunning = allRunning && strings.EqualFold(p.Status, "running")
		anyPaused = anyPaused || strings.EqualFold(p.Status, "paused")
	}
	run := func(op podOp) func() { return func() { e.runPodOp(op, ids) } }
	var items []ui.MenuItem
	if len(ps) == 1 {
		p := ps[0]
		items = append(items,
			ui.MenuItem{Label: "Inspect", OnRun: func() { e.inspectPod(p) }},
			ui.MenuItem{Label: "Generate Kube YAML", OnRun: func() { e.generateKube(kubeSubject{kind: "pod", name: p.Name}) }},
			ui.MenuItem{Label: "Copy ID", OnRun: func() { e.copyIDs(ids) }}, ui.SeparatorItem())
	} else {
		items = append(items, ui.MenuItem{Label: "Copy ID", OnRun: func() { e.copyIDs(ids) }}, ui.SeparatorItem())
	}
	if !allRunning {
		items = append(items, ui.MenuItem{Label: "Start", OnRun: run(podOpStart)})
	}
	if anyRunning {
		items = append(items, ui.MenuItem{Label: "Stop", OnRun: run(podOpStop)}, ui.MenuItem{Label: "Restart", OnRun: run(podOpRestart)})
	}
	if allRunning {
		items = append(items, ui.MenuItem{Label: "Pause", OnRun: run(podOpPause)})
	}
	if anyPaused {
		items = append(items, ui.MenuItem{Label: "Unpause", OnRun: run(podOpUnpause)})
	}
	return append(items, ui.SeparatorItem(), ui.MenuItem{Label: "Remove...", OnRun: func() { e.askRemovePods(ids) }})
}

// podIDs lists the ids of pods.
func podIDs(ps []containerapi.Pod) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}

// podNames lists the names of pods.
func podNames(ps []containerapi.Pod) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

// podByID finds a pod of the list.
func (e *ext) podByID(id string) (containerapi.Pod, bool) {
	for _, p := range e.pods {
		if p.ID == id {
			return p, true
		}
	}
	return containerapi.Pod{}, false
}

// podsOfKeys returns the pods the keys name: the rows of the Pods section, and the groups of a pod of the Containers section. Each pod
// once, in the order of the list.
func (e *ext) podsOfKeys(keys []string) []containerapi.Pod {
	want := map[string]bool{}
	for _, k := range keys {
		switch {
		case strings.HasPrefix(k, keyPod):
			want[strings.TrimPrefix(k, keyPod)] = true
		case strings.HasPrefix(k, keyGroup+podGroup):
			name := strings.TrimPrefix(k, keyGroup+podGroup)
			for _, p := range e.pods {
				if p.Name == name {
					want[p.ID] = true
				}
			}
		}
	}
	var out []containerapi.Pod
	for _, p := range e.pods {
		if want[p.ID] {
			out = append(out, p)
		}
	}
	return out
}

// withPods calls fn with the pods a command works on: the row it was run on, else the selection of the Pods view, else the one the
// user picks from the list. ok limits it to the pods the action can be done to; verb says what the action is for the messages.
func (e *ext) withPods(args any, verb string, ok func(containerapi.Pod) bool, fn func([]containerapi.Pod)) {
	if _, ready := e.ready(); !ready {
		return
	}
	var ps []containerapi.Pod
	for _, p := range e.podsOfKeys(e.views[secPods].keysFor(args)) {
		if ok == nil || ok(p) {
			ps = append(ps, p)
		}
	}
	if len(ps) > 0 {
		fn(ps)
		return
	}
	var cands []containerapi.Pod
	for _, p := range e.pods {
		if ok == nil || ok(p) {
			cands = append(cands, p)
		}
	}
	if len(cands) == 0 {
		e.api.Notifications().Info("There is no pod to " + verb + ".")
		return
	}
	items := make([]extension.QuickPickItem, len(cands))
	for i, p := range cands {
		items[i] = extension.QuickPickItem{Label: p.Name, Description: podDescription(p)}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a pod to " + verb, Items: items}, func(i int) {
		if i >= 0 && i < len(cands) {
			fn([]containerapi.Pod{cands[i]})
		}
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}

// ---- actions ----

// runPodOp does the action to the pods, one after the other; what fails is told in one notification.
func (e *ext) runPodOp(op podOp, ids []string) {
	if len(ids) == 0 {
		return
	}
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = e.podName(id)
	}
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var failures []string
		for i, id := range ids {
			if err := op.call(ctx, c, id); err != nil {
				failures = append(failures, names[i]+": "+reasonOf(err))
			}
		}
		return failures
	}, func(failures []string) {
		if len(failures) > 0 {
			e.api.Notifications().Error("Cannot " + op.verb() + " the pod " + strings.Join(failures, "; "))
		}
	})
}

// podName is how a pod is called in a message: its name, else its short id.
func (e *ext) podName(id string) string {
	if p, ok := e.podByID(id); ok {
		return p.Name
	}
	return containerapi.ShortID(id)
}

// inspectPod opens the JSON of a pod in a read-only tab.
func (e *ext) inspectPod(p containerapi.Pod) {
	e.inspectJSON(p.Name+" (inspect)", func(ctx context.Context, c *containerapi.Client) (json.RawMessage, error) {
		info, err := c.InspectPod(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		return info.Raw, nil
	})
}

// askRemovePods asks before pods are removed: how many, which, and how many containers go with them (SPEC §10.20 "Safety"). A pod
// that runs is removed only by the choice that says it is killed first.
func (e *ext) askRemovePods(ids []string) {
	var stopped, running []containerapi.Pod
	for _, id := range ids {
		if p, ok := e.podByID(id); ok {
			if podRunning(p) {
				running = append(running, p)
			} else {
				stopped = append(stopped, p)
			}
		}
	}
	all := append(append([]containerapi.Pod(nil), stopped...), running...)
	if len(all) == 0 {
		return
	}
	n := 0
	for _, p := range all {
		n += len(p.Containers)
	}
	lines := []string{strings.Join(shortList(podNames(all), 8), ", "), plural(n, "container") + " go with " + pluralWord(len(all), "it", "them") + "."}
	if len(running) > 0 {
		lines = append(lines, plural(len(running), "pod")+" running: "+strings.Join(shortList(podNames(running), 3), ", "))
	}
	remove := func(ps []containerapi.Pod, force bool) func() {
		return func() { e.removePods(podIDs(ps), force) }
	}
	var choices []choice
	switch {
	case len(running) == 0:
		choices = []choice{{label: "Remove " + plural(len(all), "pod"), run: remove(all, false)}}
	default:
		choices = []choice{{label: "Force remove " + strconv.Itoa(len(all)) + " (kills " + strconv.Itoa(len(running)) + " running)", run: remove(all, true)}}
		if len(stopped) > 0 {
			choices = append(choices, choice{label: "Remove only the " + strconv.Itoa(len(stopped)) + " stopped", run: remove(stopped, false)})
		}
	}
	e.ask(confirmation{section: resPods, title: "Remove " + plural(len(all), "pod") + "?", lines: lines, choices: choices})
}

// pluralWord is a word for one thing or for several.
func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// removePods removes pods and their containers, killing the running ones when force is set.
func (e *ext) removePods(ids []string, force bool) {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = e.podName(id)
	}
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var failures []string
		for i, id := range ids {
			if err := c.RemovePod(ctx, id, force); err != nil {
				failures = append(failures, names[i]+": "+reasonOf(err))
			}
		}
		return failures
	}, func(failures []string) {
		if len(failures) > 0 {
			e.api.Notifications().Error("Cannot remove the pod " + strings.Join(failures, "; "))
			return
		}
		e.api.Notifications().Info("Removed " + plural(len(ids), "pod") + ".")
	})
}

// askPrunePods asks before the stopped pods are removed.
func (e *ext) askPrunePods() {
	if _, ok := e.ready(); !ok {
		return
	}
	var stopped []containerapi.Pod
	n := 0
	for _, p := range e.pods {
		if !podRunning(p) {
			stopped = append(stopped, p)
			n += len(p.Containers)
		}
	}
	if len(stopped) == 0 {
		e.api.Notifications().Info("There are no stopped pods.")
		return
	}
	e.ask(confirmation{section: resPods, title: "Remove all stopped pods?",
		lines:   []string{strings.Join(shortList(podNames(stopped), 8), ", "), plural(n, "container") + " go with them."},
		choices: []choice{{label: "Remove " + plural(len(stopped), "stopped pod"), run: e.prunePods}}})
}

// prunePods removes the stopped pods.
func (e *ext) prunePods() {
	var res containerapi.PruneResult
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var err error
		if res, err = c.PrunePods(ctx); err != nil {
			return []string{reasonOf(err)}
		}
		return nil
	}, func(f []string) {
		if len(f) > 0 {
			e.api.Notifications().Error("Cannot prune the pods: " + f[0])
			return
		}
		e.api.Notifications().Info("Removed " + plural(len(res.Deleted), "pod") + ".")
	})
}

// ---- create ----

// createPod asks for a name and makes an empty pod.
func (e *ext) createPod() {
	if _, ok := e.ready(); !ok {
		return
	}
	err := e.api.InputBox().Show(extension.InputBoxOptions{Prompt: "Name of the new pod", Validate: nameValidator("pod")}, func(v string, ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(v)
		e.async(func(ctx context.Context, c *containerapi.Client) []string {
			if _, err := c.CreatePod(ctx, containerapi.PodSpec{Name: name}); err != nil {
				return []string{reasonOf(err)}
			}
			return nil
		}, func(f []string) {
			if len(f) > 0 {
				e.api.Notifications().Error("Cannot create the pod " + name + ": " + f[0])
			}
		})
	})
	if err != nil {
		e.api.Log().Warn("the input box cannot be shown", "err", err)
	}
}

// createPodFrom makes a pod from the settings of containers. A container cannot be moved into a pod after it was made, so the pod gets
// new containers made from the image, command, environment, mounts and labels of the chosen ones; the chosen containers are not
// touched. The ports they publish are published by the pod, because the containers of a pod share one network.
func (e *ext) createPodFrom(ids []string) {
	if _, ok := e.ready(); !ok || len(ids) == 0 {
		return
	}
	var picked []containerapi.Container
	for _, id := range ids {
		if c, ok := e.containerByID(id); ok {
			picked = append(picked, c)
		}
	}
	if len(picked) == 0 {
		return
	}
	prompt := "Name of the new pod, with " + plural(len(picked), "container") + " made from " + strings.Join(shortList(containerNames(picked), 3), ", ") +
		" (the chosen containers stay as they are)"
	err := e.api.InputBox().Show(extension.InputBoxOptions{Prompt: prompt, Validate: nameValidator("pod")}, func(v string, ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(v)
		var made int
		e.async(func(ctx context.Context, c *containerapi.Client) []string {
			var failures []string
			made, failures = makePodFrom(ctx, c, name, picked)
			return failures
		}, func(failures []string) {
			if len(failures) > 0 {
				e.api.Notifications().Error("Cannot create the pod " + name + ": " + strings.Join(failures, "; "))
				return
			}
			e.api.Notifications().Info("Created the pod " + name + " with " + plural(made, "container") + ".")
		})
	})
	if err != nil {
		e.api.Log().Warn("the input box cannot be shown", "err", err)
	}
}

// makePodFrom creates the pod and a container in it for each of the containers; it returns how many were made and what failed. When a
// container cannot be made the pod stays with those that were (the user sees which one failed).
func makePodFrom(ctx context.Context, c *containerapi.Client, name string, from []containerapi.Container) (int, []string) {
	specs := make([]containerapi.ContainerSpec, 0, len(from))
	var ports []containerapi.PortMapping
	var failures []string
	for _, src := range from {
		info, err := c.InspectContainer(ctx, src.ID)
		if err != nil {
			failures = append(failures, src.Name()+": "+reasonOf(err))
			continue
		}
		spec, published := specFromInspect(info)
		spec.Name = name + "-" + strings.TrimPrefix(src.Name(), "/")
		spec.Pod = name
		specs = append(specs, spec)
		for _, p := range published {
			if !slices.Contains(ports, p) {
				ports = append(ports, p)
			}
		}
	}
	if len(failures) > 0 {
		return 0, failures
	}
	if _, err := c.CreatePod(ctx, containerapi.PodSpec{Name: name, Ports: ports}); err != nil {
		return 0, []string{reasonOf(err)}
	}
	made := 0
	for i, spec := range specs {
		if _, err := c.CreateContainer(ctx, spec); err != nil {
			failures = append(failures, from[i].Name()+": "+reasonOf(err))
			continue
		}
		made++
	}
	return made, failures
}

// specFromInspect writes what it takes to make a container like the inspected one: its image, command, entrypoint, environment, labels,
// working folder, user, terminal and mounts. The ports it publishes are returned apart, to be published by the pod. The mounts are
// those of the inspect, a bind keeping its read-only option.
func specFromInspect(info *containerapi.ContainerInspect) (containerapi.ContainerSpec, []containerapi.PortMapping) {
	image := info.ImageName
	if image == "" {
		image = info.Image
	}
	spec := containerapi.ContainerSpec{
		Image: image, Cmd: info.Cmd, Entrypoint: info.Entrypoint, Env: info.Env, Labels: cleanLabels(info.Labels),
		WorkingDir: info.WorkingDir, User: info.User, Tty: info.Tty,
	}
	for _, m := range info.Mounts {
		src := m.Source
		if m.Type == "volume" && m.Name != "" {
			src = m.Name
		}
		if src == "" || m.Destination == "" || m.Type == "tmpfs" {
			continue
		}
		bind := src + ":" + m.Destination
		if !m.RW {
			bind += ":ro"
		}
		spec.Binds = append(spec.Binds, bind)
	}
	var ports []containerapi.PortMapping
	keys := make([]string, 0, len(info.Ports))
	for k := range info.Ports {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		port, proto, _ := strings.Cut(k, "/")
		cp, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			continue
		}
		for _, b := range info.Ports[k] {
			hp, _ := strconv.ParseUint(b.HostPort, 10, 16)
			ports = append(ports, containerapi.PortMapping{HostIP: b.HostIP, HostPort: uint16(hp), ContainerPort: uint16(cp), Protocol: proto})
		}
	}
	return spec, ports
}

// cleanLabels leaves out the labels that say where the container that was copied came from, not what it is: Podman's own bookkeeping
// and the Compose project, so that the copy is not shown as a part of that project.
func cleanLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range labels {
		if strings.HasPrefix(k, "io.podman.") || strings.HasPrefix(k, "com.docker.compose.") {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
