package containerui

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// ---- networks ----

// createNetwork asks for a name and makes a network.
func (e *ext) createNetwork() {
	if _, ok := e.ready(); !ok {
		return
	}
	err := e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt:   "Name of the new network",
		Validate: nameValidator("network"),
	}, func(v string, ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(v)
		e.async(func(ctx context.Context, c *containerapi.Client) []string {
			if _, err := c.CreateNetwork(ctx, containerapi.NetworkSpec{Name: name}); err != nil {
				return []string{reasonOf(err)}
			}
			return nil
		}, func(f []string) {
			if len(f) > 0 {
				e.api.Notifications().Error("Cannot create the network " + name + ": " + f[0])
			}
		})
	})
	if err != nil {
		e.api.Log().Warn("the input box cannot be shown", "err", err)
	}
}

// nameValidator checks the name of a network or volume.
func nameValidator(what string) func(string) string {
	return func(v string) string {
		v = strings.TrimSpace(v)
		switch {
		case v == "":
			return "Enter a name for the " + what + "."
		case strings.ContainsAny(v, " \t/\\"):
			return "A name has no spaces or slashes."
		}
		return ""
	}
}

// inspectNetwork opens the JSON of a network in a read-only tab.
func (e *ext) inspectNetwork(n containerapi.Network) {
	e.inspectJSON(n.Name+" (inspect)", func(ctx context.Context, c *containerapi.Client) (json.RawMessage, error) {
		info, err := c.InspectNetwork(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		return info.Raw, nil
	})
}

// askRemoveNetworks asks before networks are removed.
func (e *ext) askRemoveNetworks(ns []containerapi.Network) {
	if len(ns) == 0 {
		return
	}
	names := make([]string, len(ns))
	for i, n := range ns {
		names[i] = n.Name
	}
	e.ask(confirmation{section: resNetworks, title: "Remove " + plural(len(ns), "network") + "?",
		lines:   []string{strings.Join(shortList(names, 8), ", ")},
		choices: []choice{{label: "Remove " + plural(len(ns), "network"), run: func() { e.removeNetworks(ns) }}}})
}

// removeNetworks removes networks.
func (e *ext) removeNetworks(ns []containerapi.Network) {
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var failures []string
		for _, n := range ns {
			if err := c.RemoveNetwork(ctx, n.ID); err != nil {
				failures = append(failures, n.Name+": "+reasonOf(err))
			}
		}
		return failures
	}, func(failures []string) {
		if len(failures) > 0 {
			e.api.Notifications().Error("Cannot remove " + strings.Join(failures, "; "))
			return
		}
		e.api.Notifications().Info("Removed " + plural(len(ns), "network") + ".")
	})
}

// unusedNetworks are the networks the prune would remove: not built in, and not used by any container of the list.
func (e *ext) unusedNetworks() []containerapi.Network {
	used := map[string]bool{}
	for _, c := range e.containers {
		for _, n := range c.Networks {
			used[n] = true
		}
	}
	var out []containerapi.Network
	for _, n := range e.networks {
		if !builtinNetwork(n) && !used[n.Name] {
			out = append(out, n)
		}
	}
	return out
}

// askPruneNetworks asks before every unused network is removed.
func (e *ext) askPruneNetworks() {
	if _, ok := e.ready(); !ok {
		return
	}
	un := e.unusedNetworks()
	if len(un) == 0 {
		e.api.Notifications().Info("There are no unused networks.")
		return
	}
	names := make([]string, len(un))
	for i, n := range un {
		names[i] = n.Name
	}
	e.ask(confirmation{section: resNetworks, title: "Remove all unused networks?",
		lines:   []string{plural(len(un), "network") + " no container uses: " + strings.Join(shortList(names, 8), ", ")},
		choices: []choice{{label: "Remove " + plural(len(un), "unused network"), run: e.pruneNetworks}}})
}

// pruneNetworks removes the unused networks.
func (e *ext) pruneNetworks() {
	var res containerapi.PruneResult
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var err error
		if res, err = c.PruneNetworks(ctx, nil); err != nil {
			return []string{reasonOf(err)}
		}
		return nil
	}, func(f []string) {
		if len(f) > 0 {
			e.api.Notifications().Error("Cannot prune the networks: " + f[0])
			return
		}
		e.api.Notifications().Info("Removed " + plural(len(res.Deleted), "network") + ".")
	})
}

// ---- volumes ----

// createVolume asks for a name and makes a volume.
func (e *ext) createVolume() {
	if _, ok := e.ready(); !ok {
		return
	}
	err := e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt:   "Name of the new volume",
		Validate: nameValidator("volume"),
	}, func(v string, ok bool) {
		if !ok {
			return
		}
		name := strings.TrimSpace(v)
		e.async(func(ctx context.Context, c *containerapi.Client) []string {
			if _, err := c.CreateVolume(ctx, containerapi.VolumeSpec{Name: name}); err != nil {
				return []string{reasonOf(err)}
			}
			return nil
		}, func(f []string) {
			if len(f) > 0 {
				e.api.Notifications().Error("Cannot create the volume " + name + ": " + f[0])
			}
		})
	})
	if err != nil {
		e.api.Log().Warn("the input box cannot be shown", "err", err)
	}
}

// inspectVolume opens the JSON of a volume in a read-only tab.
func (e *ext) inspectVolume(v containerapi.Volume) {
	e.inspectJSON(v.Name+" (inspect)", func(ctx context.Context, c *containerapi.Client) (json.RawMessage, error) {
		info, err := c.InspectVolume(ctx, v.Name)
		if err != nil {
			return nil, err
		}
		return info.Raw, nil
	})
}

// askRemoveVolumes asks before volumes are removed: the data in them goes with them, so the sizes are told.
func (e *ext) askRemoveVolumes(vs []containerapi.Volume) {
	if len(vs) == 0 {
		return
	}
	e.withUsage(func(du containerapi.DiskUsage, ok bool) {
		var lines []string
		var total int64
		for _, v := range vs {
			size := v.Size
			if ok {
				if s, found := du.VolumeSizes[v.Name]; found {
					size = s
				}
			}
			if size >= 0 {
				total += size
				lines = append(lines, v.Name+"  "+humanSize(size))
			} else {
				lines = append(lines, v.Name)
			}
		}
		lines = shortList(lines, 8)
		if total > 0 {
			lines = append(lines, "Together "+humanSize(total)+". The data in a volume cannot be brought back.")
		} else {
			lines = append(lines, "The data in a volume cannot be brought back.")
		}
		e.ask(confirmation{section: resVolumes, title: "Remove " + plural(len(vs), "volume") + "?", lines: lines,
			choices: []choice{{label: "Remove " + plural(len(vs), "volume"), run: func() { e.removeVolumes(vs) }}}})
	})
}

// removeVolumes removes volumes.
func (e *ext) removeVolumes(vs []containerapi.Volume) {
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var failures []string
		for _, v := range vs {
			if err := c.RemoveVolume(ctx, v.Name, false); err != nil {
				failures = append(failures, v.Name+": "+reasonOf(err))
			}
		}
		return failures
	}, func(failures []string) {
		if len(failures) > 0 {
			e.api.Notifications().Error("Cannot remove " + strings.Join(failures, "; "))
			return
		}
		e.api.Notifications().Info("Removed " + plural(len(vs), "volume") + ".")
	})
}

// askPruneVolumes asks before every unused volume is removed, with the count and the size from "system df".
func (e *ext) askPruneVolumes() {
	if _, ok := e.ready(); !ok {
		return
	}
	e.withUsage(func(du containerapi.DiskUsage, ok bool) {
		line := "Every volume that no container uses."
		if ok {
			n := du.Volumes.Count - du.Volumes.Active
			if n <= 0 {
				e.api.Notifications().Info("There are no unused volumes.")
				return
			}
			line = plural(n, "volume") + " no container uses, " + humanSize(du.Volumes.Reclaimable) + ". The data in a volume cannot be brought back."
		}
		e.ask(confirmation{section: resVolumes, title: "Remove all unused volumes?", lines: []string{line},
			choices: []choice{{label: "Remove unused volumes", run: e.pruneVolumes}}})
	})
}

// pruneVolumes removes the unused volumes.
func (e *ext) pruneVolumes() {
	var res containerapi.PruneResult
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var err error
		if res, err = c.PruneVolumes(ctx, nil); err != nil {
			return []string{reasonOf(err)}
		}
		return nil
	}, func(f []string) {
		if len(f) > 0 {
			e.api.Notifications().Error("Cannot prune the volumes: " + f[0])
			return
		}
		e.api.Notifications().Info("Removed " + plural(len(res.Deleted), "volume") + ", " + humanSize(res.SpaceReclaimed) + " freed.")
	})
}

// ---- containers and images: prune ----

// stoppedContainers are the containers the prune would remove.
func (e *ext) stoppedContainers() []containerapi.Container {
	var out []containerapi.Container
	for _, c := range e.containers {
		if !c.Running() {
			out = append(out, c)
		}
	}
	return out
}

// askPruneContainers asks before every stopped container is removed.
func (e *ext) askPruneContainers() {
	if _, ok := e.ready(); !ok {
		return
	}
	st := e.stoppedContainers()
	if len(st) == 0 {
		e.api.Notifications().Info("There are no stopped containers.")
		return
	}
	e.withUsage(func(du containerapi.DiskUsage, ok bool) {
		lines := []string{strings.Join(shortList(containerNames(st), 8), ", ")}
		if ok && du.Containers.Reclaimable > 0 {
			lines = append(lines, humanSize(du.Containers.Reclaimable)+" of writable layers are freed.")
		}
		e.ask(confirmation{section: resContainers, title: "Remove all " + plural(len(st), "stopped container") + "?", lines: lines,
			choices: []choice{{label: "Remove " + plural(len(st), "stopped container"), run: e.pruneContainers}}})
	})
}

// pruneContainers removes the stopped containers.
func (e *ext) pruneContainers() {
	var res containerapi.PruneResult
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var err error
		if res, err = c.PruneContainers(ctx, nil); err != nil {
			return []string{reasonOf(err)}
		}
		return nil
	}, func(f []string) {
		if len(f) > 0 {
			e.api.Notifications().Error("Cannot prune the containers: " + f[0])
			return
		}
		e.api.Notifications().Info("Removed " + plural(len(res.Deleted), "container") + ", " + humanSize(res.SpaceReclaimed) + " freed.")
	})
}

// danglingImages are the images with no name.
func (e *ext) danglingImages() (rows []containerapi.Image, size int64) {
	for _, img := range e.images {
		if len(img.RepoTags) == 0 {
			rows = append(rows, img)
			size += img.Size
		}
	}
	return rows, size
}

// unusedImages are the named images no container uses, with their sizes.
func (e *ext) unusedImages() (rows []containerapi.Image, size int64) {
	used := map[string]bool{}
	for _, c := range e.containers {
		used[trimSHA(c.ImageID)] = true
	}
	for _, img := range e.images {
		if !used[trimSHA(img.ID)] && img.Containers <= 0 {
			rows = append(rows, img)
			size += img.Size
		}
	}
	return rows, size
}

// askPruneImages asks before images are removed in bulk: the dangling ones, or every unused one.
func (e *ext) askPruneImages() {
	if _, ok := e.ready(); !ok {
		return
	}
	dangling, dsize := e.danglingImages()
	unused, usize := e.unusedImages()
	if len(unused) == 0 && len(dangling) == 0 {
		e.api.Notifications().Info("There are no unused images.")
		return
	}
	var choices []choice
	if len(dangling) > 0 {
		choices = append(choices, choice{label: "Remove " + plural(len(dangling), "dangling image") + " (" + humanSize(dsize) + ")", run: func() { e.pruneImages(false) }})
	}
	if len(unused) > len(dangling) {
		choices = append(choices, choice{label: "Remove all " + plural(len(unused), "unused image") + " (" + humanSize(usize) + ")", run: func() { e.pruneImages(true) }})
	}
	lines := []string{"Dangling images have no name. Unused images are those no container uses."}
	e.ask(confirmation{section: resImages, title: "Remove unused images?", lines: lines, choices: choices})
}

// pruneImages removes the dangling images, or every unused image with all.
func (e *ext) pruneImages(all bool) {
	var res containerapi.PruneResult
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var err error
		if res, err = c.PruneImages(ctx, containerapi.PruneImagesOptions{All: all}); err != nil {
			return []string{reasonOf(err)}
		}
		return nil
	}, func(f []string) {
		if len(f) > 0 {
			e.api.Notifications().Error("Cannot prune the images: " + f[0])
			return
		}
		e.api.Notifications().Info("Removed " + plural(len(res.Deleted), "image") + ", " + humanSize(res.SpaceReclaimed) + " freed.")
	})
}

// askPruneSystem asks before the whole engine is cleaned: the stopped containers, the unused networks and the dangling images,
// and with the second choice the unused volumes too.
func (e *ext) askPruneSystem() {
	if _, ok := e.ready(); !ok {
		return
	}
	e.withUsage(func(du containerapi.DiskUsage, ok bool) {
		st := e.stoppedContainers()
		un := e.unusedNetworks()
		dangling, dsize := e.danglingImages()
		lines := []string{
			plural(len(st), "stopped container"),
			plural(len(un), "unused network"),
			plural(len(dangling), "dangling image") + ", " + humanSize(dsize),
		}
		if ok {
			n := du.Volumes.Count - du.Volumes.Active
			lines = append(lines, plural(max(n, 0), "unused volume")+", "+humanSize(du.Volumes.Reclaimable)+" (only with the second choice)")
			if du.BuildCache.Reclaimable > 0 {
				lines = append(lines, "The build cache: "+humanSize(du.BuildCache.Reclaimable)+" can be freed.")
			}
		}
		choices := []choice{
			{label: "Prune the system", run: func() { e.pruneSystem(false) }},
			{label: "Prune the system and the volumes", run: func() { e.pruneSystem(true) }},
		}
		e.ask(confirmation{section: resContainers, title: "Remove everything that is not in use?", lines: lines, choices: choices})
	})
}

// pruneSystem does the prune calls one after the other.
func (e *ext) pruneSystem(volumes bool) {
	var freed int64
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var failures []string
		note := func(what string, res containerapi.PruneResult, err error) {
			if err != nil {
				failures = append(failures, what+": "+reasonOf(err))
				return
			}
			freed += res.SpaceReclaimed
		}
		res, err := c.PruneContainers(ctx, nil)
		note("containers", res, err)
		res, err = c.PruneNetworks(ctx, nil)
		note("networks", res, err)
		res, err = c.PruneImages(ctx, containerapi.PruneImagesOptions{})
		note("images", res, err)
		if volumes {
			res, err = c.PruneVolumes(ctx, nil)
			note("volumes", res, err)
		}
		return failures
	}, func(failures []string) {
		if len(failures) > 0 {
			e.api.Notifications().Error("Cannot prune " + strings.Join(failures, "; "))
			return
		}
		e.api.Notifications().Info("Pruned the system, " + humanSize(freed) + " freed.")
	})
}

// ---- contexts ----

// useContext connects to the engine of a Docker context. It is a choice of this view only: the context of the Docker CLI is
// not changed.
func (e *ext) useContext(name string) {
	if name == e.picked {
		return
	}
	e.picked = name
	e.connect()
	e.rebuild(resContexts)
}

// inspectContext opens the description of a context in a read-only tab.
func (e *ext) inspectContext(name string) {
	for _, c := range e.contexts {
		if c.Name == name {
			raw, _ := json.Marshal(map[string]any{
				"Name": c.Name, "Description": c.Description, "Host": c.Host, "SkipTLSVerify": c.SkipTLSVerify, "TLSDir": c.TLSDir, "Current": c.Current,
			})
			e.docs.open(name+" (inspect)", indentJSON(raw), "json")
			return
		}
	}
}

// pickContext asks which context to use (the picker of the title bar of the view).
func (e *ext) pickContext() {
	if len(e.contexts) == 0 {
		e.api.Notifications().Info("There are no Docker contexts.")
		return
	}
	items := make([]extension.QuickPickItem, len(e.contexts))
	cur := e.currentContext()
	for i, c := range e.contexts {
		items[i] = extension.QuickPickItem{Label: c.Name, Description: strings.Join(nonEmpty(c.Host, mark(c.Name == cur, "(in use)")), " "), Detail: c.Description}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Docker context", Items: items, MatchOnDescription: true}, func(i int) {
		if i >= 0 && i < len(e.contexts) {
			e.useContext(e.contexts[i].Name)
		}
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}

// mark returns the text when on holds, else "".
func mark(on bool, text string) string {
	if on {
		return text
	}
	return ""
}
