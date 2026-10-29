package containerapitest

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// fakeImage is an image of the Fake.
type fakeImage struct {
	id      string
	tags    []string
	created time.Time
	size    int64
	labels  map[string]string
}

// has reports whether ref is one of the image's names or its id (or the start of it).
func (im *fakeImage) has(ref string) bool {
	for _, t := range im.tags {
		if t == ref || normalizeRef(t) == normalizeRef(ref) {
			return true
		}
	}
	return ref == im.id || ref == "sha256:"+im.id || (len(ref) >= 6 && strings.HasPrefix(im.id, strings.TrimPrefix(ref, "sha256:")))
}

// normalizeRef makes "busybox" and "docker.io/library/busybox:latest" the same name.
func normalizeRef(ref string) string {
	if !strings.Contains(ref, ":") || strings.LastIndex(ref, ":") < strings.LastIndex(ref, "/") {
		ref += ":latest"
	}
	parts := strings.Split(ref, "/")
	if len(parts) == 1 {
		return "docker.io/library/" + ref
	}
	if !strings.Contains(parts[0], ".") && !strings.Contains(parts[0], ":") && parts[0] != "localhost" {
		return "docker.io/" + ref
	}
	return ref
}

// AddImage puts an image into the Fake under the given names and returns its id.
func (f *Fake) AddImage(size int64, tags ...string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	im := &fakeImage{id: f.newID("image/" + strings.Join(tags, ",")), tags: tags, created: time.Now().Add(-time.Hour), size: size}
	f.images = append(f.images, im)
	return im.id
}

// AddLabelledImage is AddImage for an image with labels (a test image has the label of the tests).
func (f *Fake) AddLabelledImage(size int64, labels map[string]string, tags ...string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	im := &fakeImage{id: f.newID("image/" + strings.Join(tags, ",")), tags: tags, created: time.Now().Add(-time.Hour), size: size, labels: labels}
	f.images = append(f.images, im)
	return im.id
}

// findImageLocked finds an image by name or id.
func (f *Fake) findImageLocked(ref string) *fakeImage {
	for _, im := range f.images {
		if im.has(ref) {
			return im
		}
	}
	return nil
}

// imageIDOf returns an id in the form the engine writes it.
func (f *Fake) imageIDOf(im *fakeImage) string {
	if f.kind == containerapi.KindPodman {
		return im.id
	}
	return "sha256:" + im.id
}

// usedByLocked counts the containers that use an image.
func (f *Fake) usedByLocked(im *fakeImage) int {
	n := 0
	for _, c := range f.containers {
		if c.imageID == "sha256:"+im.id {
			n++
		}
	}
	return n
}

// notFoundImage answers a missing image.
func (f *Fake) notFoundImage(w http.ResponseWriter, ref string) {
	if f.kind == containerapi.KindPodman {
		f.writeError(w, http.StatusNotFound, "failed to find image "+ref+": "+ref+": image not known")
		return
	}
	f.writeError(w, http.StatusNotFound, "No such image: "+ref)
}

// imagesRoute handles /images/....
func (f *Fake) imagesRoute(w http.ResponseWriter, r *http.Request, rest string) {
	switch {
	case rest == "json" && r.Method == http.MethodGet:
		f.listImages(w, r)
		return
	case (rest == "create" || rest == "pull") && r.Method == http.MethodPost:
		f.pullImage(w, r)
		return
	case rest == "prune" && r.Method == http.MethodPost:
		f.pruneImages(w, r)
		return
	}
	var name, action string
	switch {
	case strings.HasSuffix(rest, "/json"):
		name, action = strings.TrimSuffix(rest, "/json"), "json"
	case strings.HasSuffix(rest, "/history"):
		name, action = strings.TrimSuffix(rest, "/history"), "history"
	case strings.HasSuffix(rest, "/tag"):
		name, action = strings.TrimSuffix(rest, "/tag"), "tag"
	default:
		name = rest
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	im := f.findImageLocked(name)
	if im == nil {
		f.notFoundImage(w, name)
		return
	}
	switch {
	case action == "json" && r.Method == http.MethodGet:
		out := map[string]any{
			"Id": f.imageIDOf(im), "RepoTags": im.tags, "RepoDigests": []string{}, "Created": im.created.UTC().Format(time.RFC3339Nano),
			"Size": im.size, "Architecture": "amd64", "Os": "linux",
			"Config": map[string]any{"Env": []string{"PATH=/usr/bin"}, "Cmd": []string{"sh"}, "Entrypoint": nil, "Labels": im.labels, "ExposedPorts": map[string]any{"80/tcp": map[string]any{}}},
		}
		writeJSON(w, http.StatusOK, out)
	case action == "history" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, []map[string]any{
			{"Id": f.imageIDOf(im), "Created": im.created.Unix(), "CreatedBy": "CMD [\"sh\"]", "Tags": im.tags, "Size": 0, "Comment": ""},
			{"Id": "<missing>", "Created": im.created.Unix(), "CreatedBy": "ADD rootfs.tar /", "Tags": nil, "Size": im.size, "Comment": "buildkit"},
		})
	case action == "tag" && r.Method == http.MethodPost:
		repo, tag := r.URL.Query().Get("repo"), r.URL.Query().Get("tag")
		if tag == "" {
			tag = "latest"
		}
		im.tags = append(im.tags, repo+":"+tag)
		f.emitLocked("image", "tag", f.imageIDOf(im), map[string]string{"name": repo + ":" + tag})
		w.WriteHeader(http.StatusCreated)
	case action == "" && r.Method == http.MethodDelete:
		f.removeImageLocked(w, r, im, name)
	default:
		f.writeError(w, http.StatusNotFound, "page not found")
	}
}

// removeImageLocked answers DELETE /images/{name}.
func (f *Fake) removeImageLocked(w http.ResponseWriter, r *http.Request, im *fakeImage, name string) {
	force := boolParam(r, "force")
	byName := false
	for _, t := range im.tags {
		if normalizeRef(t) == normalizeRef(name) {
			byName = true
		}
	}
	var untagged, deleted []string
	if byName && len(im.tags) > 1 {
		var kept []string
		for _, t := range im.tags {
			if normalizeRef(t) == normalizeRef(name) {
				untagged = append(untagged, t)
			} else {
				kept = append(kept, t)
			}
		}
		im.tags = kept
	} else {
		if f.usedByLocked(im) > 0 && !force {
			f.writeError(w, http.StatusConflict, "image is being used by a container: unable to delete "+im.id+" (must be forced)")
			return
		}
		untagged = append(untagged, im.tags...)
		deleted = append(deleted, f.imageIDOf(im))
		for i, other := range f.images {
			if other == im {
				f.images = append(f.images[:i], f.images[i+1:]...)
				break
			}
		}
	}
	// the engines tell what went (step 18.2: the view of an extension follows these events)
	for _, u := range untagged {
		f.emitLocked("image", "untag", f.imageIDOf(im), map[string]string{"name": u})
	}
	for _, d := range deleted {
		f.emitLocked("image", "delete", d, map[string]string{"name": d})
	}
	if f.kind == containerapi.KindPodman {
		writeJSON(w, http.StatusOK, map[string]any{"Untagged": untagged, "Deleted": deleted, "Errors": []string{}, "ExitCode": 0})
		return
	}
	var items []map[string]string
	for _, u := range untagged {
		items = append(items, map[string]string{"Untagged": u})
	}
	for _, d := range deleted {
		items = append(items, map[string]string{"Deleted": d})
	}
	writeJSON(w, http.StatusOK, items)
}

// listImages answers GET /images/json.
func (f *Fake) listImages(w http.ResponseWriter, r *http.Request) {
	filters := parseFilters(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := []map[string]any{}
	for _, im := range f.images {
		if !matchLabels(im.labels, filters["label"]) {
			continue
		}
		if refs := filters["reference"]; len(refs) > 0 {
			ok := false
			for _, ref := range refs {
				for _, t := range im.tags {
					if normalizeRef(t) == normalizeRef(ref) || strings.HasPrefix(t, ref+":") {
						ok = true
					}
				}
			}
			if !ok {
				continue
			}
		}
		dangling := len(im.tags) == 0
		if d := filters["dangling"]; len(d) > 0 && (d[0] == "true") != dangling {
			continue
		}
		row := map[string]any{
			"Id": f.imageIDOf(im), "RepoTags": im.tags, "RepoDigests": []string{}, "Created": im.created.Unix(), "Size": im.size,
			"Containers": f.usedByLocked(im), "Labels": im.labels,
		}
		if f.kind == containerapi.KindDocker {
			row["Containers"] = f.usedByLocked(im)
			row["ParentId"] = ""
			row["SharedSize"] = -1
		} else {
			row["Dangling"] = dangling
		}
		if dangling {
			row["RepoTags"] = nil
		}
		rows = append(rows, row)
	}
	writeJSON(w, http.StatusOK, rows)
}

// pullImage answers POST /images/create (Docker) and /images/pull (libpod) with a stream of progress lines.
func (f *Fake) pullImage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ref := q.Get("reference")
	if f.kind == containerapi.KindDocker {
		ref = q.Get("fromImage")
		if tag := q.Get("tag"); tag != "" {
			ref += ":" + tag
		}
	}
	f.mu.Lock()
	failure, fails := f.pullFails[ref]
	if !fails {
		for k, msg := range f.pullFails {
			if normalizeRef(k) == normalizeRef(ref) {
				failure, fails = msg, true
			}
		}
	}
	var id string
	if !fails && f.findImageLocked(ref) == nil {
		im := &fakeImage{id: f.newID("pulled/" + ref), tags: []string{ref}, created: time.Now().Add(-24 * time.Hour), size: 4 << 20}
		f.images = append(f.images, im)
		id = im.id
		f.emitLocked("image", "pull", ref, map[string]string{"name": ref})
	} else if !fails {
		id = f.findImageLocked(ref).id
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	if fails {
		_ = enc.Encode(map[string]any{"error": failure, "errorDetail": map[string]string{"message": failure}})
		flush(w)
		return
	}
	if f.kind == containerapi.KindPodman {
		_ = enc.Encode(map[string]string{"stream": "Trying to pull " + ref + "..."})
		_ = enc.Encode(map[string]string{"stream": "Copying blob sha256:abc"})
		_ = enc.Encode(map[string]any{"images": []string{id}, "id": id})
	} else {
		_ = enc.Encode(map[string]any{"status": "Pulling from library/" + ref, "id": "latest"})
		_ = enc.Encode(map[string]any{"status": "Downloading", "progressDetail": map[string]int{"current": 1024, "total": 4096}, "id": "layer1"})
		_ = enc.Encode(map[string]any{"status": "Pull complete", "progressDetail": map[string]any{}, "id": "layer1"})
		_ = enc.Encode(map[string]any{"status": "Status: Downloaded newer image for " + ref})
	}
	flush(w)
}

// pruneImages answers POST /images/prune.
func (f *Fake) pruneImages(w http.ResponseWriter, r *http.Request) {
	filters := parseFilters(r)
	all := boolParam(r, "all")
	if d := filters["dangling"]; len(d) > 0 && d[0] == "false" {
		all = true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var kept []*fakeImage
	var gone []string
	var freed int64
	for _, im := range f.images {
		unused := f.usedByLocked(im) == 0
		dangling := len(im.tags) == 0
		if unused && (dangling || all) && matchLabels(im.labels, filters["label"]) {
			gone = append(gone, f.imageIDOf(im))
			freed += im.size
			continue
		}
		kept = append(kept, im)
	}
	f.images = kept
	if f.kind == containerapi.KindPodman {
		reports := make([]map[string]any, 0, len(gone))
		for _, id := range gone {
			reports = append(reports, map[string]any{"Id": id, "Err": nil, "Size": 1})
		}
		writeJSON(w, http.StatusOK, reports)
		return
	}
	items := make([]map[string]string, 0, len(gone))
	for _, id := range gone {
		items = append(items, map[string]string{"Deleted": id})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ImagesDeleted": items, "SpaceReclaimed": freed})
}

// fakeNetwork is a network of the Fake.
type fakeNetwork struct {
	id, name, driver string
	labels           map[string]string
	internal, ipv6   bool
	created          time.Time
}

// networksRoute handles /networks....
func (f *Fake) networksRoute(w http.ResponseWriter, r *http.Request, rest string, body []byte) {
	podman := f.kind == containerapi.KindPodman
	switch {
	case ((!podman && rest == "") || (podman && rest == "json")) && r.Method == http.MethodGet:
		f.listNetworks(w, r)
	case rest == "create" && r.Method == http.MethodPost:
		f.createNetwork(w, body)
	case rest == "prune" && r.Method == http.MethodPost:
		f.pruneNetworks(w, r)
	default:
		name := strings.TrimSuffix(rest, "/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		n := f.findNetworkLocked(name)
		switch {
		case n == nil:
			if podman {
				f.writeError(w, http.StatusNotFound, "unable to find network with name or ID "+name+": network not found")
			} else {
				f.writeError(w, http.StatusNotFound, "network "+name+" not found")
			}
		case r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, f.networkJSON(n))
		case r.Method == http.MethodDelete:
			for _, c := range f.containers {
				if c.network == n.name {
					f.writeError(w, http.StatusForbidden, "network "+n.name+" has active endpoints")
					return
				}
			}
			delete(f.networks, n.name)
			f.emitLocked("network", "destroy", n.id, map[string]string{"name": n.name})
			if podman {
				writeJSON(w, http.StatusOK, []map[string]any{{"Name": n.name, "Err": nil}})
				return
			}
			noContent(w)
		default:
			f.writeError(w, http.StatusNotFound, "page not found")
		}
	}
}

// findNetworkLocked finds a network by name or id.
func (f *Fake) findNetworkLocked(ref string) *fakeNetwork {
	if n := f.networks[ref]; n != nil {
		return n
	}
	for _, n := range f.networks {
		if n.id == ref || (len(ref) >= 4 && strings.HasPrefix(n.id, ref)) {
			return n
		}
	}
	return nil
}

// networkJSON makes a network in the engine's shape.
func (f *Fake) networkJSON(n *fakeNetwork) map[string]any {
	if f.kind == containerapi.KindPodman {
		return map[string]any{
			"name": n.name, "id": n.id, "driver": n.driver, "created": n.created.Format(time.RFC3339Nano), "internal": n.internal,
			"ipv6_enabled": n.ipv6, "labels": n.labels, "subnets": []map[string]string{{"subnet": "10.89.0.0/24", "gateway": "10.89.0.1"}},
		}
	}
	return map[string]any{
		"Name": n.name, "Id": n.id, "Created": n.created.Format(time.RFC3339Nano), "Scope": "local", "Driver": n.driver, "EnableIPv6": n.ipv6,
		"IPAM":     map[string]any{"Driver": "default", "Config": []map[string]string{{"Subnet": "172.19.0.0/16", "Gateway": "172.19.0.1"}}},
		"Internal": n.internal, "Labels": n.labels,
	}
}

// listNetworks answers the network list.
func (f *Fake) listNetworks(w http.ResponseWriter, r *http.Request) {
	filters := parseFilters(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.networks))
	for name := range f.networks {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := []map[string]any{}
	for _, name := range names {
		n := f.networks[name]
		if matchLabels(n.labels, filters["label"]) && matchAny(filters["name"], n.name) {
			rows = append(rows, f.networkJSON(n))
		}
	}
	writeJSON(w, http.StatusOK, rows)
}

// createNetwork answers POST /networks/create.
func (f *Fake) createNetwork(w http.ResponseWriter, body []byte) {
	var in struct {
		Name       string            `json:"Name"`
		Driver     string            `json:"Driver"`
		Labels     map[string]string `json:"Labels"`
		Internal   bool              `json:"Internal"`
		EnableIPv6 bool              `json:"EnableIPv6"`
		IPv6       bool              `json:"ipv6_enabled"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		f.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if in.Name == "" {
		f.writeError(w, http.StatusBadRequest, "network name is required")
		return
	}
	if f.networks[in.Name] != nil {
		if f.kind == containerapi.KindPodman {
			f.writeError(w, http.StatusConflict, "network name "+in.Name+" already used: network already exists")
		} else {
			f.writeError(w, http.StatusConflict, "network with name "+in.Name+" already exists")
		}
		return
	}
	n := &fakeNetwork{id: f.newID("network/" + in.Name), name: in.Name, driver: in.Driver, labels: in.Labels, internal: in.Internal, ipv6: in.EnableIPv6 || in.IPv6, created: time.Now()}
	if n.driver == "" {
		n.driver = "bridge"
	}
	if n.labels == nil {
		n.labels = map[string]string{}
	}
	f.networks[n.name] = n
	f.emitLocked("network", "create", n.id, map[string]string{"name": n.name})
	if f.kind == containerapi.KindPodman {
		writeJSON(w, http.StatusOK, f.networkJSON(n))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"Id": n.id, "Warning": ""})
}

// pruneNetworks answers POST /networks/prune.
func (f *Fake) pruneNetworks(w http.ResponseWriter, r *http.Request) {
	filters := parseFilters(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	var gone []string
	for name, n := range f.networks {
		if name == "bridge" || !matchLabels(n.labels, filters["label"]) {
			continue
		}
		used := false
		for _, c := range f.containers {
			if c.network == name {
				used = true
			}
		}
		if !used {
			gone = append(gone, name)
		}
	}
	sort.Strings(gone)
	for _, name := range gone {
		delete(f.networks, name)
	}
	if f.kind == containerapi.KindPodman {
		reports := make([]map[string]any, 0, len(gone))
		for _, name := range gone {
			reports = append(reports, map[string]any{"Name": name, "Error": nil})
		}
		writeJSON(w, http.StatusOK, reports)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"NetworksDeleted": gone})
}

// fakeVolume is a volume of the Fake.
type fakeVolume struct {
	name, driver string
	labels       map[string]string
	created      time.Time
	size         int64
}

// volumesRoute handles /volumes....
func (f *Fake) volumesRoute(w http.ResponseWriter, r *http.Request, rest string, body []byte) {
	podman := f.kind == containerapi.KindPodman
	switch {
	case ((!podman && rest == "") || (podman && rest == "json")) && r.Method == http.MethodGet:
		f.listVolumes(w, r)
	case rest == "create" && r.Method == http.MethodPost:
		f.createVolume(w, body)
	case rest == "prune" && r.Method == http.MethodPost:
		f.pruneVolumes(w, r)
	default:
		name := strings.TrimSuffix(rest, "/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		v := f.volumes[name]
		switch {
		case v == nil:
			if podman {
				f.writeError(w, http.StatusNotFound, "no such volume: "+name+": no volume with name \""+name+"\" found: no such volume")
			} else {
				f.writeError(w, http.StatusNotFound, "get "+name+": no such volume")
			}
		case r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, f.volumeJSON(v))
		case r.Method == http.MethodDelete:
			if f.volumeUsedLocked(name) && !boolParam(r, "force") {
				f.writeError(w, http.StatusConflict, "remove "+name+": volume is in use - [containers]")
				return
			}
			delete(f.volumes, name)
			f.emitLocked("volume", "destroy", name, map[string]string{})
			noContent(w)
		default:
			f.writeError(w, http.StatusNotFound, "page not found")
		}
	}
}

// volumeUsedLocked reports whether a container mounts the volume.
func (f *Fake) volumeUsedLocked(name string) bool {
	for _, c := range f.containers {
		for _, b := range c.binds {
			if strings.HasPrefix(b, name+":") {
				return true
			}
		}
	}
	return false
}

// volumeJSON makes a volume in the engine's shape.
func (f *Fake) volumeJSON(v *fakeVolume) map[string]any {
	return map[string]any{
		"Name": v.name, "Driver": v.driver, "Mountpoint": "/var/lib/fake/volumes/" + v.name + "/_data", "CreatedAt": v.created.Format(time.RFC3339),
		"Labels": v.labels, "Scope": "local", "Options": map[string]string{},
	}
}

// listVolumes answers the volume list.
func (f *Fake) listVolumes(w http.ResponseWriter, r *http.Request) {
	filters := parseFilters(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.volumes))
	for name := range f.volumes {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := []map[string]any{}
	for _, name := range names {
		v := f.volumes[name]
		if matchLabels(v.labels, filters["label"]) && matchAny(filters["name"], v.name) {
			rows = append(rows, f.volumeJSON(v))
		}
	}
	if f.kind == containerapi.KindPodman {
		writeJSON(w, http.StatusOK, rows)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"Volumes": rows, "Warnings": nil})
}

// createVolume answers POST /volumes/create.
func (f *Fake) createVolume(w http.ResponseWriter, body []byte) {
	var in struct {
		Name   string            `json:"Name"`
		Driver string            `json:"Driver"`
		Labels map[string]string `json:"Labels"`
		Label  map[string]string `json:"Label"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		f.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if in.Name == "" {
		in.Name = f.newID("volume")[:32]
	}
	labels := in.Labels
	if f.kind == containerapi.KindPodman {
		labels = in.Label
	}
	if labels == nil {
		labels = map[string]string{}
	}
	if existing := f.volumes[in.Name]; existing != nil && f.kind == containerapi.KindPodman {
		f.writeError(w, http.StatusConflict, "volume with name "+in.Name+" already exists: volume already exists")
		return
	}
	v := &fakeVolume{name: in.Name, driver: "local", labels: labels, created: time.Now()}
	if in.Driver != "" {
		v.driver = in.Driver
	}
	f.volumes[v.name] = v
	f.emitLocked("volume", "create", v.name, map[string]string{})
	writeJSON(w, http.StatusCreated, f.volumeJSON(v))
}

// pruneVolumes answers POST /volumes/prune.
func (f *Fake) pruneVolumes(w http.ResponseWriter, r *http.Request) {
	filters := parseFilters(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	var gone []string
	for name, v := range f.volumes {
		if !f.volumeUsedLocked(name) && matchLabels(v.labels, filters["label"]) {
			gone = append(gone, name)
		}
	}
	sort.Strings(gone)
	for _, name := range gone {
		delete(f.volumes, name)
	}
	if f.kind == containerapi.KindPodman {
		reports := make([]map[string]any, 0, len(gone))
		for _, name := range gone {
			reports = append(reports, map[string]any{"Id": name, "Err": nil, "Size": 10})
		}
		writeJSON(w, http.StatusOK, reports)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"VolumesDeleted": gone, "SpaceReclaimed": 10 * len(gone)})
}

// fakePod is a pod of the Fake.
type fakePod struct {
	id, name string
	labels   map[string]string
	created  time.Time
}

// podKeyLocked returns the id of the pod called ref (a name, an id, or the start of one), or "".
func (f *Fake) podKeyLocked(ref string) string {
	if f.pods[ref] != nil {
		return ref
	}
	for id, p := range f.pods {
		if p.name == ref || (len(ref) >= 4 && strings.HasPrefix(id, ref)) {
			return id
		}
	}
	return ""
}

// podStatusLocked derives the status of a pod from its containers.
func (f *Fake) podStatusLocked(p *fakePod) string {
	running, created, total := 0, 0, 0
	for _, c := range f.containers {
		if c.pod == p.id {
			total++
			switch c.state {
			case "running":
				running++
			case "created":
				created++
			}
		}
	}
	switch {
	case total == 0 || created == total:
		return "Created"
	case running == total:
		return "Running"
	case running == 0:
		return "Exited"
	}
	return "Degraded"
}

// podJSON makes a pod in libpod's shape.
func (f *Fake) podJSON(p *fakePod, inspect bool) map[string]any {
	var cs []map[string]any
	ids := make([]string, 0)
	for _, c := range f.containers {
		if c.pod == p.id {
			ids = append(ids, c.id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		c := f.containers[id]
		if inspect {
			cs = append(cs, map[string]any{"Id": c.id, "Name": c.name, "State": c.state})
		} else {
			cs = append(cs, map[string]any{"Id": c.id, "Names": c.name, "Status": c.state})
		}
	}
	out := map[string]any{"Id": p.id, "Name": p.name, "Created": p.created.Format(time.RFC3339Nano), "Labels": p.labels, "Containers": cs}
	if inspect {
		out["State"] = f.podStatusLocked(p)
	} else {
		out["Status"] = f.podStatusLocked(p)
	}
	return out
}

// podsRoute handles /pods/....
func (f *Fake) podsRoute(w http.ResponseWriter, r *http.Request, rest string, body []byte) {
	switch {
	case rest == "json" && r.Method == http.MethodGet:
		filters := parseFilters(r)
		f.mu.Lock()
		defer f.mu.Unlock()
		ids := make([]string, 0, len(f.pods))
		for id := range f.pods {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		rows := []map[string]any{}
		for _, id := range ids {
			if p := f.pods[id]; matchLabels(p.labels, filters["label"]) && matchAny(filters["name"], p.name) {
				rows = append(rows, f.podJSON(p, false))
			}
		}
		writeJSON(w, http.StatusOK, rows)
		return
	case rest == "create" && r.Method == http.MethodPost:
		var in struct {
			Name   string            `json:"name"`
			Labels map[string]string `json:"labels"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			f.writeError(w, http.StatusBadRequest, "decode(): "+err.Error())
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, p := range f.pods {
			if in.Name != "" && p.name == in.Name {
				f.writeError(w, http.StatusConflict, "adding pod to state: name \""+in.Name+"\" is in use: pod already exists")
				return
			}
		}
		p := &fakePod{id: f.newID("pod/" + in.Name), name: in.Name, labels: in.Labels, created: time.Now()}
		if p.name == "" {
			p.name = "pod_" + p.id[:8]
		}
		if p.labels == nil {
			p.labels = map[string]string{}
		}
		f.pods[p.id] = p
		f.emitLocked("pod", "create", p.id, map[string]string{"name": p.name})
		writeJSON(w, http.StatusCreated, map[string]string{"Id": p.id})
		return
	case rest == "prune" && r.Method == http.MethodPost:
		f.mu.Lock()
		defer f.mu.Unlock()
		var reports []map[string]any
		for id, p := range f.pods {
			if f.podStatusLocked(p) != "Running" {
				delete(f.pods, id)
				reports = append(reports, map[string]any{"Id": id, "Err": nil})
			}
		}
		writeJSON(w, http.StatusOK, reports)
		return
	}
	ref, action, _ := strings.Cut(rest, "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.podKeyLocked(ref)
	if id == "" {
		f.writeError(w, http.StatusNotFound, "no pod with name or ID "+ref+" found: no such pod")
		return
	}
	p := f.pods[id]
	switch {
	case action == "json" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, f.podJSON(p, true))
	case action == "" && r.Method == http.MethodDelete:
		if f.podStatusLocked(p) == "Running" && !boolParam(r, "force") {
			f.writeError(w, http.StatusConflict, "pod "+p.id+" has containers that are running: pod already exists")
			return
		}
		for _, c := range f.containers {
			if c.pod == p.id {
				if c.state == "running" {
					f.stopLocked(c, 137, "")
				}
				if _, still := f.containers[c.id]; still {
					f.deleteContainerLocked(c)
				}
			}
		}
		delete(f.pods, p.id)
		f.emitLocked("pod", "remove", p.id, map[string]string{"name": p.name})
		writeJSON(w, http.StatusOK, map[string]any{"Id": p.id, "Err": nil})
	case r.Method == http.MethodPost && (action == "start" || action == "stop" || action == "restart" || action == "kill" || action == "pause" || action == "unpause"):
		for _, c := range f.containers {
			if c.pod != p.id {
				continue
			}
			switch action {
			case "start":
				if c.state != "running" {
					f.startLocked(c)
				}
			case "stop", "kill":
				if c.state == "running" {
					f.stopLocked(c, 0, "stop")
				}
			case "restart":
				if c.state == "running" {
					f.stopLocked(c, 0, "")
				}
				f.startLocked(c)
			case "pause":
				if c.state == "running" {
					c.state = "paused"
				}
			case "unpause":
				if c.state == "paused" {
					c.state = "running"
				}
			}
		}
		f.emitLocked("pod", action, p.id, map[string]string{"name": p.name})
		writeJSON(w, http.StatusOK, map[string]any{"Errs": nil, "Id": p.id})
	default:
		f.writeError(w, http.StatusNotFound, "page not found")
	}
}

// emitLocked records an event and hands it to the streams; the caller holds the lock.
func (f *Fake) emitLocked(typ, action, id string, attrs map[string]string) {
	ev := containerapi.Event{Type: typ, Action: action, ID: id, Attributes: attrs, Scope: "local", Time: time.Now()}
	f.history = append(f.history, ev)
	if len(f.history) > 1000 {
		f.history = f.history[len(f.history)-1000:]
	}
	for _, ch := range f.subs {
		select {
		case ch <- ev:
		default: // a stream that does not read loses events, as a slow client of a real engine would be cut
		}
	}
}

// Emit sends an event to the event streams, as if the engine had made it.
func (f *Fake) Emit(ev containerapi.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	f.history = append(f.history, ev)
	for _, ch := range f.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// eventMatches reports whether an event passes the filters of a stream.
func (f *Fake) eventMatches(ev containerapi.Event, filters map[string][]string) bool {
	if t := filters["type"]; len(t) > 0 && !matchAny(t, ev.Type) {
		return false
	}
	if e := filters["event"]; len(e) > 0 && !matchAny(e, ev.Action) {
		return false
	}
	if c := filters["container"]; len(c) > 0 && !matchAny(c, ev.ID) && !matchAny(c, ev.Attributes["name"]) {
		return false
	}
	for _, l := range filters["label"] {
		k, v, hasV := strings.Cut(l, "=")
		got, ok := ev.Attributes[k]
		if !ok || (hasV && got != v) {
			return false
		}
	}
	return true
}

// eventsStream answers GET /events: the events since the asked time, then the live ones, until the client leaves or the
// "until" time passes.
func (f *Fake) eventsStream(w http.ResponseWriter, r *http.Request) {
	filters := parseFilters(r)
	var since, until time.Time
	if s, err := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64); err == nil {
		since = time.Unix(s, 0)
	}
	if s, err := strconv.ParseInt(r.URL.Query().Get("until"), 10, 64); err == nil {
		until = time.Unix(s, 0)
	}
	ch := make(chan containerapi.Event, 256)
	f.mu.Lock()
	f.subCount++
	sub := f.subCount
	var replay []containerapi.Event
	if !since.IsZero() {
		for _, ev := range f.history {
			if ev.Time.Unix() >= since.Unix() && (until.IsZero() || ev.Time.Unix() <= until.Unix()) { // the engines compare whole seconds
				replay = append(replay, ev)
			}
		}
	}
	f.subs[sub] = ch
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.subs, sub)
		f.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	flush(w)
	enc := json.NewEncoder(w)
	write := func(ev containerapi.Event) {
		if !f.eventMatches(ev, filters) {
			return
		}
		_ = enc.Encode(map[string]any{
			"status": ev.Action, "id": ev.ID, "Type": ev.Type, "Action": ev.Action,
			"Actor": map[string]any{"ID": ev.ID, "Attributes": ev.Attributes}, "scope": ev.Scope,
			"time": ev.Time.Unix(), "timeNano": ev.Time.UnixNano(),
		})
		flush(w)
	}
	for _, ev := range replay {
		write(ev)
	}
	if !until.IsZero() && !since.IsZero() {
		return // a replay of a closed range ends
	}
	for {
		select {
		case ev := <-ch:
			write(ev)
		case <-r.Context().Done():
			return
		case <-f.stop:
			return
		}
	}
}

// systemDF answers GET /system/df in the engine's shape.
func (f *Fake) systemDF(w http.ResponseWriter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.kind == containerapi.KindPodman {
		var images []map[string]any
		var total int64
		for _, im := range f.images {
			total += im.size
			images = append(images, map[string]any{"ImageID": im.id, "Size": im.size, "UniqueSize": im.size, "SharedSize": 0, "Containers": f.usedByLocked(im)})
		}
		var cs []map[string]any
		for _, c := range f.containers {
			cs = append(cs, map[string]any{"ContainerID": c.id, "Status": c.state, "RWSize": 1000, "Size": 1000})
		}
		var vs []map[string]any
		for name, v := range f.volumes {
			links := 0
			if f.volumeUsedLocked(name) {
				links = 1
			}
			reclaimable := v.size
			if links > 0 {
				reclaimable = 0
			}
			vs = append(vs, map[string]any{"VolumeName": name, "Links": links, "Size": v.size, "ReclaimableSize": reclaimable})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ImagesSize": total, "Images": images, "Containers": cs, "Volumes": vs})
		return
	}
	var images []map[string]any
	var total int64
	for _, im := range f.images {
		total += im.size
		images = append(images, map[string]any{"Id": f.imageIDOf(im), "Size": im.size, "SharedSize": 0, "Containers": f.usedByLocked(im)})
	}
	var cs []map[string]any
	for _, c := range f.containers {
		cs = append(cs, map[string]any{"Id": c.id, "State": c.state, "SizeRw": 1000})
	}
	var vs []map[string]any
	for name, v := range f.volumes {
		refs := 0
		if f.volumeUsedLocked(name) {
			refs = 1
		}
		vs = append(vs, map[string]any{"Name": name, "UsageData": map[string]any{"Size": v.size, "RefCount": refs}})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"LayersSize": total, "Images": images, "Containers": cs, "Volumes": vs,
		"BuildCache": []map[string]any{{"Size": 500, "InUse": false}, {"Size": 300, "InUse": true}},
	})
}

// SetVolumeSize sets the size "system df" reports for a volume.
func (f *Fake) SetVolumeSize(name string, size int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v := f.volumes[name]; v != nil {
		v.size = size
	}
}
