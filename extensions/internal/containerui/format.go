package containerui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// humanSize writes a number of bytes the way the Docker CLI does, in powers of 1000: 12.3MB.
func humanSize(n int64) string {
	if n < 0 {
		return "unknown size"
	}
	const unit = 1000
	if n < unit {
		return strconv.FormatInt(n, 10) + "B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.3g%cB", float64(n)/float64(div), "kMGTP"[exp])
}

// ago writes how long before now a time was ("3 hours ago"); the zero time is "".
func ago(t, now time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return ""
	}
	return span(now.Sub(t)) + " ago"
}

// span writes a length of time in the largest unit that fits: "less than a minute", "2 hours".
func span(d time.Duration) string {
	plural := func(n int64, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return strconv.FormatInt(n, 10) + " " + unit + "s"
	}
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return plural(int64(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return plural(int64(d/time.Hour), "hour")
	case d < 30*24*time.Hour:
		return plural(int64(d/(24*time.Hour)), "day")
	case d < 365*24*time.Hour:
		return plural(int64(d/(30*24*time.Hour)), "month")
	}
	return plural(int64(d/(365*24*time.Hour)), "year")
}

// statusText is the sentence under a container: the engine's own ("Up 2 hours") or, from an engine that gives none, one made from
// the state and the time of the start.
func statusText(c containerapi.Container, now time.Time) string {
	if c.Status != "" {
		return c.Status
	}
	switch c.State {
	case "running":
		if !c.StartedAt.IsZero() && c.StartedAt.Unix() > 0 {
			return "Up " + span(now.Sub(c.StartedAt))
		}
		return "Up"
	case "paused":
		return "Up (Paused)"
	case "exited", "stopped":
		return "Exited"
	case "":
		return ""
	}
	return strings.ToUpper(c.State[:1]) + c.State[1:]
}

// portsText lists the ports a container publishes as host:container/protocol, the way docker ps does, without the same port twice
// (an engine lists it for IPv4 and again for IPv6); a port that is only exposed is container/protocol.
func portsText(ports []containerapi.Port) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range ports {
		proto := p.Type
		if proto == "" {
			proto = "tcp"
		}
		var s string
		if p.PublicPort != 0 {
			s = fmt.Sprintf("%d:%d/%s", p.PublicPort, p.PrivatePort, proto)
		} else {
			s = fmt.Sprintf("%d/%s", p.PrivatePort, proto)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return strings.Join(out, ", ")
}

// publishedHTTP returns the first published TCP port of a container that a browser can be sent to, with the host to use.
func publishedHTTP(c containerapi.Container) (host string, port uint16, ok bool) {
	for _, p := range c.Ports {
		if p.PublicPort == 0 || (p.Type != "" && p.Type != "tcp") {
			continue
		}
		host = p.IP
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "localhost"
		}
		return host, p.PublicPort, true
	}
	return "", 0, false
}

// otherGroup is the group of the containers that Compose did not make, and noRepository the group of the images that have no name.
const (
	otherGroup   = "Other Containers"
	noRepository = "<none>"
)

// containerGroup is a Compose project (or, for Podman, a pod) and its containers.
type containerGroup struct {
	name       string
	containers []containerapi.Container
	// pod says that the group is a pod, podID is its id.
	pod   bool
	podID string
}

// sortedContainers returns the containers by name.
func sortedContainers(cs []containerapi.Container) []containerapi.Container {
	out := append([]containerapi.Container(nil), cs...)
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name()) < strings.ToLower(out[j].Name()) })
	return out
}

// groupContainers groups the containers by Compose project, the projects by name and the containers that belong to none last. With
// no project at all the result is one unnamed group: the list is flat. With byPod (Podman) the containers of a pod are grouped under
// it, before the projects; a container that a Compose project made stays in its project even when podman-compose put it in a pod, so
// that the Compose commands of the group keep working (the pod is in the Pods section).
func groupContainers(cs []containerapi.Container, byPod bool) []containerGroup {
	by := map[string][]containerapi.Container{}
	pods := map[string][]containerapi.Container{}
	podIDs := map[string]string{}
	var names, podNames []string
	var other []containerapi.Container
	for _, c := range sortedContainers(cs) {
		if p := c.ComposeProject(); p != "" {
			if _, ok := by[p]; !ok {
				names = append(names, p)
			}
			by[p] = append(by[p], c)
			continue
		}
		if byPod && c.PodName != "" {
			if _, ok := pods[c.PodName]; !ok {
				podNames = append(podNames, c.PodName)
				podIDs[c.PodName] = c.Pod
			}
			pods[c.PodName] = append(pods[c.PodName], c)
			continue
		}
		other = append(other, c)
	}
	if len(names) == 0 && len(podNames) == 0 {
		if len(other) == 0 {
			return nil
		}
		return []containerGroup{{containers: other}}
	}
	byName := func(ns []string) {
		sort.Slice(ns, func(i, j int) bool { return strings.ToLower(ns[i]) < strings.ToLower(ns[j]) })
	}
	byName(names)
	byName(podNames)
	out := make([]containerGroup, 0, len(names)+len(podNames)+1)
	for _, n := range podNames {
		out = append(out, containerGroup{name: n, containers: pods[n], pod: true, podID: podIDs[n]})
	}
	for _, n := range names {
		out = append(out, containerGroup{name: n, containers: by[n]})
	}
	if len(other) > 0 {
		out = append(out, containerGroup{name: otherGroup, containers: other})
	}
	return out
}

// splitRepoTag splits "registry:5000/repo/name:tag" into the repository and the tag. A name with no tag has the tag "latest"
// only when asked to by the caller, so the tag is "" here.
func splitRepoTag(ref string) (repo, tag string) {
	slash := strings.LastIndex(ref, "/")
	if colon := strings.LastIndex(ref, ":"); colon > slash {
		return ref[:colon], ref[colon+1:]
	}
	return ref, ""
}

// imageRow is one line of the Images section: an image and one of its names (empty for an image with none).
type imageRow struct {
	image containerapi.Image
	// ref is the whole name ("nginx:1.27"), repo and tag its parts.
	ref, repo, tag string
}

// target is what an action names the image by: its reference when it has one, else its id.
func (r imageRow) target() string {
	if r.ref != "" {
		return r.ref
	}
	return r.image.ID
}

// label is the text of the row: the tag, or the short id of an image with no name.
func (r imageRow) label() string {
	if r.ref == "" {
		return containerapi.ShortID(r.image.ID)
	}
	if r.tag == "" {
		return "latest"
	}
	return r.tag
}

// imageGroup is the images of one repository.
type imageGroup struct {
	repo string
	rows []imageRow
}

// groupImages lists a row for each name of each image and groups them by repository, the repositories by name and the images that
// have no name last, newest first within a group.
func groupImages(images []containerapi.Image, showDangling bool) []imageGroup {
	by := map[string][]imageRow{}
	var names []string
	for _, img := range images {
		if len(img.RepoTags) == 0 {
			if !showDangling {
				continue
			}
			if _, ok := by[noRepository]; !ok {
				names = append(names, noRepository)
			}
			by[noRepository] = append(by[noRepository], imageRow{image: img, repo: noRepository})
			continue
		}
		for _, ref := range img.RepoTags {
			if ref == "<none>:<none>" {
				continue
			}
			repo, tag := splitRepoTag(ref)
			if _, ok := by[repo]; !ok {
				names = append(names, repo)
			}
			by[repo] = append(by[repo], imageRow{image: img, ref: ref, repo: repo, tag: tag})
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == noRepository) != (names[j] == noRepository) {
			return names[j] == noRepository
		}
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	out := make([]imageGroup, 0, len(names))
	for _, n := range names {
		rows := by[n]
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].image.Created.After(rows[j].image.Created) })
		out = append(out, imageGroup{repo: n, rows: rows})
	}
	return out
}

// matchAny reports whether the filter (a text, matched without regard to case) is found in one of the fields; an empty filter
// matches everything.
func matchAny(filter string, fields ...string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return true
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), filter) {
			return true
		}
	}
	return false
}

// plural writes a count and a noun: "1 container", "3 containers".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// builtinNetwork reports whether a network is one the engine makes itself and cannot remove.
func builtinNetwork(n containerapi.Network) bool {
	switch n.Name {
	case "bridge", "host", "none", "podman", "default":
		return true
	}
	return false
}
