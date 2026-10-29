package containerui

import (
	"path"
	"sort"
	"strings"

	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"

	"github.com/rm4n0s/emetos/extension/ui"
)

// Ported from vscode-containers: extensions/vscode-containers/src/tree/containers/ContainersTreeItem.ts, ContainerTreeItem.ts,
// ContainerProperties.ts (getContainerStateIcon), images/ImagesTreeItem.ts, networks/NetworksTreeItem.ts, volumes/VolumesTreeItem.ts,
// contexts/ContextsTreeItem.ts and package.json (menus view/item/context): the sections, their rows, icons and menus.

// ---- the nodes of the trees ----

// The data of the nodes.
type (
	// groupNode is a Compose project or a repository.
	groupNode struct {
		name  string
		count int
		// compose says that the group is a Compose project; a repository group is not.
		compose bool
	}
	// containerNode is a container.
	containerNode struct{ c containerapi.Container }
	// fileNode is a file or a folder of a container.
	fileNode struct {
		container, path string
		dir             bool
	}
	// imageNode is a name of an image.
	imageNode struct{ row imageRow }
	// networkNode is a network.
	networkNode struct{ n containerapi.Network }
	// volumeNode is a volume.
	volumeNode struct{ v containerapi.Volume }
	// contextNode is a Docker context.
	contextNode struct {
		c       containerapi.DockerContext
		current bool
	}
)

// The prefixes of the keys of the nodes.
const (
	keyGroup     = "g:"
	keyContainer = "c:"
	keyFile      = "f:"
	keyImage     = "i:"
	keyNetwork   = "n:"
	keyVolume    = "v:"
	keyContext   = "x:"
)

// ---- Containers ----

type containersSection struct{}

func (*containersSection) name() string  { return secContainers }
func (*containersSection) res() resource { return resContainers }
func (*containersSection) title() string { return "Containers" }

func (s *containersSection) items(e *ext) ([]ui.TreeItem, string) {
	l := e.loads[resContainers]
	if l.err != "" {
		return nil, "Cannot list the containers: " + l.err
	}
	if !l.loaded {
		return nil, ""
	}
	var shown []containerapi.Container
	for _, c := range e.containers {
		if matchAny(e.filters[resContainers], c.Name(), c.Image, c.ID, c.State, c.ComposeProject(), portsText(c.Ports)) {
			shown = append(shown, c)
		}
	}
	if len(shown) == 0 {
		if f := e.filters[resContainers]; f != "" {
			return nil, "No containers match \"" + f + "\"."
		}
		return nil, "No containers found."
	}
	var items []ui.TreeItem
	for _, g := range groupContainers(shown) {
		if g.name == "" {
			for _, c := range g.containers {
				items = append(items, e.containerItem(c))
			}
			continue
		}
		node := ui.TreeItem{Key: keyGroup + g.name, Label: g.name,
			Data: groupNode{name: g.name, count: len(g.containers), compose: g.name != otherGroup}}
		for _, c := range g.containers {
			node.Children = append(node.Children, e.containerItem(c))
		}
		items = append(items, node)
	}
	return items, ""
}

// containerItem makes the node of a container; a running one can be opened to show its files.
func (e *ext) containerItem(c containerapi.Container) ui.TreeItem {
	it := ui.TreeItem{Key: keyContainer + c.ID, Data: containerNode{c}, Label: c.Name()}
	if c.State == "running" {
		if entries, ok := e.files.get(rootKey(c.ID)); ok {
			it.Children = e.fileItems(c.ID, "/", entries)
			it.HasChildren = true
		} else {
			it.Lazy = true
		}
	}
	return it
}

// stateIcon is the Codicon and the color of the state of a container (Container Tools' getContainerStateIcon).
func stateIcon(colors ui.Colors, c containerapi.Container) (string, gui.Color) {
	switch {
	case strings.Contains(c.Status, "(unhealthy)"):
		return "warning", colors.ProblemsWarningIconForeground
	case strings.Contains(c.Status, "(health: starting)"):
		return "debug-rerun", colors.DebugIconStartForeground
	}
	switch strings.ToLower(c.State) {
	case "created", "dead", "exited", "removing", "terminated", "unknown", "waiting", "stopped":
		return "debug-stop", colors.DebugIconStopForeground
	case "paused":
		return "debug-pause", colors.DebugIconPauseForeground
	case "restarting":
		return "debug-restart", colors.DebugIconRestartForeground
	}
	return "debug-start", colors.DebugIconStartForeground
}

func (s *containersSection) render(bc widgets.BuildContext, v *sectionView, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	e := v.e
	center := rendering.CrossAxisAlignmentCenter
	switch d := node.Data.(type) {
	case groupNode:
		icon := "folder"
		if d.compose {
			icon = "layers"
		}
		row := []widgets.Widget{ui.Icon(icon, 16, st.Foreground), gap(6), text(bc, d.name, 13, st.Foreground),
			widgets.NewSpacer(), ui.NewBadge(d.count), gap(4)}
		return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: center})
	case containerNode:
		c := d.c
		icon, color := stateIcon(colors, c)
		desc := strings.Join(nonEmpty(c.Image, portsText(c.Ports), statusText(c, e.cfg.now())), " · ")
		row := []widgets.Widget{ui.Icon(icon, 16, color), gap(6)}
		row = append(row, labelAndDescription(bc, colors, st, c.Name(), desc)...)
		if c.Running() {
			row = append(row, rowButton(st, "debug-stop", "Stop", func() { e.runContainerOp(opStop, []string{c.ID}) }))
		} else {
			row = append(row, rowButton(st, "debug-start", "Start", func() { e.runContainerOp(opStart, []string{c.ID}) }))
		}
		row = append(row, rowButton(st, "output", "View Logs", func() { e.viewLogs(c.ID) }))
		return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: center})
	case fileNode:
		icon := "symbol-file"
		if d.dir {
			icon = "folder"
		}
		row := []widgets.Widget{ui.Icon(icon, 16, st.Foreground), gap(6), text(bc, path.Base(d.path), 13, st.Foreground)}
		return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: center})
	}
	return text(bc, node.Label, 13, st.Foreground)
}

func (s *containersSection) menu(v *sectionView, keys []string) []ui.MenuItem {
	e := v.e
	cs := e.containersOfKeys(keys)
	if len(cs) == 0 {
		return e.fileMenu(keys)
	}
	ids := idsOf(cs)
	allRunning, anyRunning, allStopped, anyPaused := true, false, true, false
	for _, c := range cs {
		allRunning = allRunning && c.State == "running"
		anyRunning = anyRunning || c.Running()
		allStopped = allStopped && !c.Running()
		anyPaused = anyPaused || c.State == "paused"
	}
	run := func(op containerOp) func() { return func() { e.runContainerOp(op, ids) } }
	var items []ui.MenuItem
	if project := composeGroupOf(keys); project != "" {
		items = append(items,
			ui.MenuItem{Label: "Compose Logs", OnRun: func() { e.composeGroup(project, groupLogs) }},
			ui.MenuItem{Label: "Compose Start", OnRun: func() { e.composeGroup(project, groupStart) }},
			ui.MenuItem{Label: "Compose Stop", OnRun: func() { e.composeGroup(project, groupStop) }},
			ui.MenuItem{Label: "Compose Restart", OnRun: func() { e.composeGroup(project, groupRestart) }},
			ui.MenuItem{Label: "Compose Down...", OnRun: func() { e.composeGroup(project, groupDown) }},
			ui.SeparatorItem())
	}
	one := len(cs) == 1 && !hasGroup(e, keys)
	if one {
		c := cs[0]
		items = append(items, ui.MenuItem{Label: "View Logs", OnRun: func() { e.viewLogs(c.ID) }},
			ui.MenuItem{Label: "Inspect", OnRun: func() { e.inspectContainer(c.ID) }})
		if _, _, ok := publishedHTTP(c); ok && c.Running() {
			items = append(items, ui.MenuItem{Label: "Open in Browser", OnRun: func() { e.openInBrowser(c) }})
		}
		if c.Running() {
			items = append(items, ui.MenuItem{Label: "Open Shell", OnRun: func() { e.openShell(c) }},
				ui.MenuItem{Label: "Run Command...", OnRun: func() { e.runCommandIn(c.ID) }},
				ui.MenuItem{Label: "Explore Files", OnRun: func() { e.exploreFiles(c.ID) }})
		}
		items = append(items, ui.MenuItem{Label: "Copy ID", OnRun: func() { e.copyIDs(ids) }}, ui.SeparatorItem())
	} else {
		items = append(items, ui.MenuItem{Label: "Copy ID", OnRun: func() { e.copyIDs(ids) }}, ui.SeparatorItem())
	}
	if allStopped {
		items = append(items, ui.MenuItem{Label: "Start", OnRun: run(opStart)})
	}
	if anyRunning {
		items = append(items, ui.MenuItem{Label: "Stop", OnRun: run(opStop)})
	}
	if allRunning {
		items = append(items, ui.MenuItem{Label: "Restart", OnRun: run(opRestart)},
			ui.MenuItem{Label: "Pause", OnRun: run(opPause)},
			ui.MenuItem{Label: "Kill", OnRun: run(opKill)})
	}
	if anyPaused && len(cs) == 1 {
		items = append(items, ui.MenuItem{Label: "Unpause", OnRun: run(opUnpause)})
	}
	items = append(items, ui.SeparatorItem(), ui.MenuItem{Label: "Remove...", OnRun: func() { e.askRemoveContainers(ids) }})
	return items
}

// composeGroupOf returns the Compose project when the keys are one group of containers of a project (the Compose group of the view),
// else "".
func composeGroupOf(keys []string) string {
	if len(keys) != 1 {
		return ""
	}
	name, ok := strings.CutPrefix(keys[0], keyGroup)
	if !ok || name == "" || name == otherGroup || strings.HasPrefix(name, "image:") {
		return ""
	}
	return name
}

// hasGroup tells whether the keys name a group of containers.
func hasGroup(e *ext, keys []string) bool {
	for _, k := range keys {
		if strings.HasPrefix(k, keyGroup) {
			return true
		}
	}
	return false
}

// fileMenu is the menu of a file or folder of a container.
func (e *ext) fileMenu(keys []string) []ui.MenuItem {
	if len(keys) != 1 || !strings.HasPrefix(keys[0], keyFile) {
		return nil
	}
	cid, p, dir := parseFileKey(keys[0])
	if dir {
		return nil
	}
	return []ui.MenuItem{{Label: "Open", OnRun: func() { e.openFile(cid, p) }}}
}

func (s *containersSection) open(v *sectionView, node ui.TreeNode) {
	switch d := node.Data.(type) {
	case containerNode:
		v.e.viewLogs(d.c.ID)
	case fileNode:
		if !d.dir {
			v.e.openFile(d.container, d.path)
		}
	}
}

// ---- Images ----

type imagesSection struct{}

func (*imagesSection) name() string  { return secImages }
func (*imagesSection) res() resource { return resImages }
func (*imagesSection) title() string { return "Images" }

func (s *imagesSection) items(e *ext) ([]ui.TreeItem, string) {
	l := e.loads[resImages]
	if l.err != "" {
		return nil, "Cannot list the images: " + l.err
	}
	if !l.loaded {
		return nil, ""
	}
	filter := e.filters[resImages]
	var items []ui.TreeItem
	for _, g := range groupImages(e.images, e.showDangling) {
		node := ui.TreeItem{Key: keyGroup + "image:" + g.repo, Label: g.repo}
		for _, r := range g.rows {
			if !matchAny(filter, r.ref, r.image.ID, r.repo) {
				continue
			}
			node.Children = append(node.Children, ui.TreeItem{Key: keyImage + r.image.ID + ":" + r.ref, Data: imageNode{r}, Label: r.label()})
		}
		if len(node.Children) == 0 {
			continue
		}
		node.Data = groupNode{name: g.repo, count: len(node.Children)}
		items = append(items, node)
	}
	if len(items) == 0 {
		if filter != "" {
			return nil, "No images match \"" + filter + "\"."
		}
		return nil, "No images found."
	}
	return items, ""
}

func (s *imagesSection) render(bc widgets.BuildContext, v *sectionView, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	e := v.e
	center := rendering.CrossAxisAlignmentCenter
	switch d := node.Data.(type) {
	case groupNode:
		return widgets.NewRow([]widgets.Widget{ui.Icon("repo", 16, st.Foreground), gap(6), text(bc, d.name, 13, st.Foreground),
			widgets.NewSpacer(), ui.NewBadge(d.count), gap(4)}, widgets.RowOpts{CrossAxisAlignment: center})
	case imageNode:
		r := d.row
		desc := strings.Join(nonEmpty(danglingMark(r), ago(r.image.Created, e.cfg.now()), humanSize(r.image.Size)), ", ")
		row := []widgets.Widget{ui.Icon("tag", 16, st.Foreground), gap(6)}
		row = append(row, labelAndDescription(bc, colors, st, r.label(), desc)...)
		row = append(row, rowButton(st, "play", "Run", func() { e.runImage(r) }))
		return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: center})
	}
	return text(bc, node.Label, 13, st.Foreground)
}

// danglingMark marks an image that has no name.
func danglingMark(r imageRow) string {
	if r.ref == "" {
		return "dangling"
	}
	return ""
}

func (s *imagesSection) menu(v *sectionView, keys []string) []ui.MenuItem {
	e := v.e
	rows := e.imageRowsOfKeys(keys)
	if len(rows) == 0 {
		return nil
	}
	remove := ui.MenuItem{Label: "Remove...", OnRun: func() { e.askRemoveImages(rows) }}
	if len(rows) > 1 {
		return []ui.MenuItem{remove}
	}
	r := rows[0]
	items := []ui.MenuItem{
		{Label: "Run...", OnRun: func() { e.runImage(r) }},
		{Label: "Run Interactive", OnRun: func() { e.runImageInteractive(r) }},
		{Label: "Inspect", OnRun: func() { e.inspectImage(r) }},
		{Label: "History", OnRun: func() { e.imageHistory(r) }},
		{Label: "Pull", OnRun: func() { e.pullImage(r.ref) }, Disabled: r.ref == ""},
		{Label: "Tag...", OnRun: func() { e.tagImage(r) }},
		{Label: "Push...", OnRun: func() { e.pushImage(r) }, Disabled: r.ref == ""},
		{Label: "Copy Full Tag", OnRun: func() { e.copyText(r.ref) }, Disabled: r.ref == ""},
		ui.SeparatorItem(), remove,
	}
	return items
}

func (s *imagesSection) open(v *sectionView, node ui.TreeNode) {
	if d, ok := node.Data.(imageNode); ok {
		v.e.inspectImage(d.row)
	}
}

// ---- Networks ----

type networksSection struct{}

func (*networksSection) name() string  { return secNetworks }
func (*networksSection) res() resource { return resNetworks }
func (*networksSection) title() string { return "Networks" }

func (s *networksSection) items(e *ext) ([]ui.TreeItem, string) {
	l := e.loads[resNetworks]
	if l.err != "" {
		return nil, "Cannot list the networks: " + l.err
	}
	if !l.loaded {
		return nil, ""
	}
	var items []ui.TreeItem
	for _, n := range e.networks {
		items = append(items, ui.TreeItem{Key: keyNetwork + n.ID, Data: networkNode{n}, Label: n.Name})
	}
	if len(items) == 0 {
		return nil, "No networks found."
	}
	return items, ""
}

func (s *networksSection) render(bc widgets.BuildContext, v *sectionView, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	if d, ok := node.Data.(networkNode); ok {
		var subnet string
		if len(d.n.Subnets) > 0 {
			subnet = d.n.Subnets[0].Subnet
		}
		desc := strings.Join(nonEmpty(d.n.Driver, subnet), " · ")
		row := []widgets.Widget{ui.Icon("globe", 16, st.Foreground), gap(6)}
		row = append(row, labelAndDescription(bc, colors, st, d.n.Name, desc)...)
		return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	}
	return text(bc, node.Label, 13, st.Foreground)
}

func (s *networksSection) menu(v *sectionView, keys []string) []ui.MenuItem {
	e := v.e
	ns := e.networksOfKeys(keys)
	if len(ns) == 0 {
		return nil
	}
	var items []ui.MenuItem
	if len(ns) == 1 {
		n := ns[0]
		items = append(items, ui.MenuItem{Label: "Inspect", OnRun: func() { e.inspectNetwork(n) }}, ui.SeparatorItem())
	}
	removable := true
	for _, n := range ns {
		removable = removable && !builtinNetwork(n)
	}
	items = append(items, ui.MenuItem{Label: "Remove...", Disabled: !removable, OnRun: func() { e.askRemoveNetworks(ns) }})
	return items
}

func (s *networksSection) open(v *sectionView, node ui.TreeNode) {
	if d, ok := node.Data.(networkNode); ok {
		v.e.inspectNetwork(d.n)
	}
}

// ---- Volumes ----

type volumesSection struct{}

func (*volumesSection) name() string  { return secVolumes }
func (*volumesSection) res() resource { return resVolumes }
func (*volumesSection) title() string { return "Volumes" }

func (s *volumesSection) items(e *ext) ([]ui.TreeItem, string) {
	l := e.loads[resVolumes]
	if l.err != "" {
		return nil, "Cannot list the volumes: " + l.err
	}
	if !l.loaded {
		return nil, ""
	}
	var items []ui.TreeItem
	for _, v := range e.volumes {
		items = append(items, ui.TreeItem{Key: keyVolume + v.Name, Data: volumeNode{v}, Label: v.Name})
	}
	if len(items) == 0 {
		return nil, "No volumes found."
	}
	return items, ""
}

func (s *volumesSection) render(bc widgets.BuildContext, v *sectionView, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	if d, ok := node.Data.(volumeNode); ok {
		size := ""
		if d.v.Size >= 0 {
			size = humanSize(d.v.Size)
		}
		desc := strings.Join(nonEmpty(d.v.Driver, size, ago(d.v.Created, v.e.cfg.now())), ", ")
		row := []widgets.Widget{ui.Icon("database", 16, st.Foreground), gap(6)}
		row = append(row, labelAndDescription(bc, colors, st, d.v.Name, desc)...)
		return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	}
	return text(bc, node.Label, 13, st.Foreground)
}

func (s *volumesSection) menu(v *sectionView, keys []string) []ui.MenuItem {
	e := v.e
	vs := e.volumesOfKeys(keys)
	if len(vs) == 0 {
		return nil
	}
	var items []ui.MenuItem
	if len(vs) == 1 {
		vol := vs[0]
		items = append(items, ui.MenuItem{Label: "Inspect", OnRun: func() { e.inspectVolume(vol) }}, ui.SeparatorItem())
	}
	return append(items, ui.MenuItem{Label: "Remove...", OnRun: func() { e.askRemoveVolumes(vs) }})
}

func (s *volumesSection) open(v *sectionView, node ui.TreeNode) {
	if d, ok := node.Data.(volumeNode); ok {
		v.e.inspectVolume(d.v)
	}
}

// ---- Contexts ----

type contextsSection struct{}

func (*contextsSection) name() string  { return secContexts }
func (*contextsSection) res() resource { return resContexts }
func (*contextsSection) title() string { return "Contexts" }

func (s *contextsSection) items(e *ext) ([]ui.TreeItem, string) {
	l := e.loads[resContexts]
	if l.err != "" {
		return nil, "Cannot read the Docker contexts: " + l.err
	}
	if !l.loaded {
		return nil, ""
	}
	cur := e.currentContext()
	var items []ui.TreeItem
	for _, c := range e.contexts {
		items = append(items, ui.TreeItem{Key: keyContext + c.Name, Data: contextNode{c: c, current: c.Name == cur}, Label: c.Name})
	}
	if len(items) == 0 {
		return nil, "No contexts found."
	}
	return items, ""
}

func (s *contextsSection) render(bc widgets.BuildContext, v *sectionView, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	e := v.e
	if d, ok := node.Data.(contextNode); ok {
		icon := "vm"
		if d.current {
			icon = "check"
		}
		desc := strings.Join(nonEmpty(d.c.Description, d.c.Host), " · ")
		row := []widgets.Widget{ui.Icon(icon, 16, st.Foreground), gap(6)}
		row = append(row, labelAndDescription(bc, colors, st, d.c.Name, desc)...)
		if !d.current {
			row = append(row, rowButton(st, "check", "Use", func() { e.useContext(d.c.Name) }))
		}
		return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	}
	return text(bc, node.Label, 13, st.Foreground)
}

func (s *contextsSection) menu(v *sectionView, keys []string) []ui.MenuItem {
	e := v.e
	if len(keys) != 1 || !strings.HasPrefix(keys[0], keyContext) {
		return nil
	}
	name := strings.TrimPrefix(keys[0], keyContext)
	return []ui.MenuItem{
		{Label: "Use", OnRun: func() { e.useContext(name) }, Disabled: name == e.currentContext()},
		{Label: "Inspect", OnRun: func() { e.inspectContext(name) }},
	}
}

func (s *contextsSection) open(v *sectionView, node ui.TreeNode) {
	if d, ok := node.Data.(contextNode); ok && !d.current {
		v.e.useContext(d.c.Name)
	}
}

// ---- helpers ----

// nonEmpty returns the strings that are not empty.
func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// idsOf returns the ids of containers.
func idsOf(cs []containerapi.Container) []string {
	ids := make([]string, len(cs))
	for i, c := range cs {
		ids[i] = c.ID
	}
	return ids
}

// containersOfKeys returns the containers the keys name; a group names every container in it. Each container once, in the order
// of the list.
func (e *ext) containersOfKeys(keys []string) []containerapi.Container {
	want := map[string]bool{}
	for _, k := range keys {
		switch {
		case strings.HasPrefix(k, keyContainer):
			want[strings.TrimPrefix(k, keyContainer)] = true
		case strings.HasPrefix(k, keyGroup):
			name := strings.TrimPrefix(k, keyGroup)
			for _, c := range e.containers {
				if p := c.ComposeProject(); p == name || (p == "" && name == otherGroup) {
					want[c.ID] = true
				}
			}
		}
	}
	var out []containerapi.Container
	for _, c := range sortedContainers(e.containers) {
		if want[c.ID] {
			out = append(out, c)
		}
	}
	return out
}

// containerByID finds a container of the list.
func (e *ext) containerByID(id string) (containerapi.Container, bool) {
	for _, c := range e.containers {
		if c.ID == id {
			return c, true
		}
	}
	return containerapi.Container{}, false
}

// imageRowsOfKeys returns the image rows the keys name (a repository names all its rows).
func (e *ext) imageRowsOfKeys(keys []string) []imageRow {
	want := map[string]bool{}
	repos := map[string]bool{}
	for _, k := range keys {
		switch {
		case strings.HasPrefix(k, keyImage):
			want[k] = true
		case strings.HasPrefix(k, keyGroup+"image:"):
			repos[strings.TrimPrefix(k, keyGroup+"image:")] = true
		}
	}
	var out []imageRow
	for _, g := range groupImages(e.images, e.showDangling) {
		for _, r := range g.rows {
			if want[keyImage+r.image.ID+":"+r.ref] || repos[g.repo] {
				out = append(out, r)
			}
		}
	}
	return out
}

// networksOfKeys returns the networks the keys name.
func (e *ext) networksOfKeys(keys []string) []containerapi.Network {
	var out []containerapi.Network
	for _, n := range e.networks {
		for _, k := range keys {
			if k == keyNetwork+n.ID {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// volumesOfKeys returns the volumes the keys name.
func (e *ext) volumesOfKeys(keys []string) []containerapi.Volume {
	var out []containerapi.Volume
	for _, v := range e.volumes {
		for _, k := range keys {
			if k == keyVolume+v.Name {
				out = append(out, v)
				break
			}
		}
	}
	return out
}

var _ = sort.Strings
