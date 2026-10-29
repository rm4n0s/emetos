package containerapitest

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// logEntry is a line of a container's output.
type logEntry struct {
	stream containerapi.Stream
	text   string
	at     time.Time
}

// fakeContainer is a container of the Fake.
type fakeContainer struct {
	id, name, image, imageID string
	cmd, entrypoint, env     []string
	labels                   map[string]string
	ports                    []containerapi.PortMapping
	binds                    []string
	network, pod, user, wd   string
	tty, autoRemove          bool
	state                    string
	exitCode                 int
	created, started         time.Time
	finished                 time.Time
	logs                     []logEntry
	changed                  chan struct{}
	stopTick                 chan struct{}
}

// notify wakes the log followers of the container; the caller holds the Fake's lock.
func (c *fakeContainer) notify() {
	close(c.changed)
	c.changed = make(chan struct{})
}

// fakeExec is an exec instance.
type fakeExec struct {
	id, container string
	cmd           []string
	started, done bool
	exit          int
}

// AddContainer puts a container straight into the Fake, as if it had been made before the test started, and returns its id.
// The state is "created", "running" or "exited".
func (f *Fake) AddContainer(name, image string, labels map[string]string, state string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.newContainerLocked(name, image, labels)
	c.state = state
	if state == "running" {
		c.started = time.Now()
	}
	if state == "exited" {
		c.finished = time.Now() // as if it had just ended: its status is not "Exited ... 15 billion years ago"
	}
	return c.id
}

// newContainerLocked makes a container and registers it.
func (f *Fake) newContainerLocked(name, image string, labels map[string]string) *fakeContainer {
	id := f.newID("container/" + name)
	if name == "" {
		name = "fake_" + id[:8]
	}
	c := &fakeContainer{
		id: id, name: name, image: image, imageID: "sha256:" + f.newID("image/"+image), labels: labels, state: "created",
		created: time.Now(), changed: make(chan struct{}), network: "bridge",
	}
	if c.labels == nil {
		c.labels = map[string]string{}
	}
	for _, im := range f.images {
		if im.has(image) {
			c.imageID = "sha256:" + im.id
		}
	}
	f.containers[id] = c
	return c
}

// Log appends a line to a container's output.
func (f *Fake) Log(id string, stream containerapi.Stream, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.containers[id]; c != nil {
		c.logs = append(c.logs, logEntry{stream: stream, text: text + "\n", at: time.Now()})
		c.notify()
	}
}

// lookupLocked finds a container by id, a prefix of its id, or name.
func (f *Fake) lookupLocked(ref string) *fakeContainer {
	ref = strings.TrimPrefix(ref, "/")
	if c := f.containers[ref]; c != nil {
		return c
	}
	for _, c := range f.containers {
		if c.name == ref || (len(ref) >= 4 && strings.HasPrefix(c.id, ref)) {
			return c
		}
	}
	return nil
}

// notFoundContainer answers a missing container in the engine's words.
func (f *Fake) notFoundContainer(w http.ResponseWriter, ref string) {
	if f.kind == containerapi.KindPodman {
		f.writeError(w, http.StatusNotFound, "no container with name or ID \""+ref+"\" found: no such container")
		return
	}
	f.writeError(w, http.StatusNotFound, "No such container: "+ref)
}

// containersRoute handles /containers/....
func (f *Fake) containersRoute(w http.ResponseWriter, r *http.Request, rest string, body []byte) {
	switch {
	case rest == "json" && r.Method == http.MethodGet:
		f.listContainers(w, r)
		return
	case rest == "create" && r.Method == http.MethodPost:
		f.createContainer(w, r, body)
		return
	case rest == "prune" && r.Method == http.MethodPost:
		f.pruneContainers(w, r)
		return
	}
	ref, action, _ := strings.Cut(rest, "/")
	f.mu.Lock()
	c := f.lookupLocked(ref)
	f.mu.Unlock()
	switch {
	case action == "" && r.Method == http.MethodDelete:
		f.removeContainer(w, r, ref)
	case c == nil:
		f.notFoundContainer(w, ref)
	case action == "json" && r.Method == http.MethodGet:
		f.inspectContainer(w, c)
	case action == "logs" && r.Method == http.MethodGet:
		f.logsStream(w, r, c)
	case action == "exec" && r.Method == http.MethodPost:
		f.createExec(w, c, body)
	case r.Method == http.MethodPost:
		f.containerAction(w, r, c, action)
	default:
		f.writeError(w, http.StatusNotFound, "page not found")
	}
}

// portsOf lists a container's ports with the host ports assigned.
func (c *fakeContainer) portStrings() map[string][]map[string]string {
	out := map[string][]map[string]string{}
	for _, p := range c.ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		key := strconv.Itoa(int(p.ContainerPort)) + "/" + proto
		out[key] = append(out[key], map[string]string{"HostIp": p.HostIP, "HostPort": strconv.Itoa(int(p.HostPort))})
	}
	return out
}

// listContainers answers GET /containers/json.
func (f *Fake) listContainers(w http.ResponseWriter, r *http.Request) {
	all := boolParam(r, "all")
	filters := parseFilters(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	var cs []*fakeContainer
	for _, c := range f.containers {
		if !all && c.state != "running" && c.state != "paused" {
			continue
		}
		if !matchLabels(c.labels, filters["label"]) || !matchAny(filters["name"], c.name) || !matchAny(filters["id"], c.id) || !matchAny(filters["status"], c.state) {
			continue
		}
		cs = append(cs, c)
	}
	sort.Slice(cs, func(i, j int) bool {
		return cs[i].created.Before(cs[j].created) || (cs[i].created.Equal(cs[j].created) && cs[i].name < cs[j].name)
	})
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, f.containerRow(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// matchAny reports whether want is empty or one of its entries is contained in value.
func matchAny(want []string, value string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if strings.Contains(value, w) {
			return true
		}
	}
	return false
}

// status makes Docker's sentence for a state.
func (c *fakeContainer) status() string {
	switch c.state {
	case "running":
		return "Up " + since(c.started)
	case "paused":
		return "Up " + since(c.started) + " (Paused)"
	case "exited":
		return "Exited (" + strconv.Itoa(c.exitCode) + ") " + since(c.finished) + " ago"
	}
	return "Created"
}

// since formats the time since t the way Docker does, roughly.
func since(t time.Time) string {
	d := time.Since(t)
	if d < 2*time.Second {
		return "1 second"
	}
	if d < time.Minute {
		return strconv.Itoa(int(d.Seconds())) + " seconds"
	}
	return strconv.Itoa(int(d.Minutes())) + " minutes"
}

// containerRow makes a row of the list in the engine's shape; the caller holds the lock.
func (f *Fake) containerRow(c *fakeContainer) map[string]any {
	if f.kind == containerapi.KindPodman {
		ports := make([]map[string]any, 0, len(c.ports))
		for _, p := range c.ports {
			proto := p.Protocol
			if proto == "" {
				proto = "tcp"
			}
			ports = append(ports, map[string]any{"host_ip": p.HostIP, "container_port": p.ContainerPort, "host_port": p.HostPort, "range": 1, "protocol": proto})
		}
		row := map[string]any{
			"Id": c.id, "Names": []string{c.name}, "Image": c.image, "ImageID": strings.TrimPrefix(c.imageID, "sha256:"),
			"Command": append(append([]string{}, c.entrypoint...), c.cmd...), "Created": c.created.Format(time.RFC3339Nano),
			"Labels": c.labels, "State": c.state, "Status": "", "Ports": ports, "Pod": "", "PodName": "", "Networks": []string{c.network},
		}
		if !c.started.IsZero() {
			row["StartedAt"] = c.started.Unix()
		}
		if p := f.pods[c.pod]; p != nil {
			row["Pod"], row["PodName"] = p.id, p.name
		}
		return row
	}
	ports := make([]map[string]any, 0, len(c.ports))
	for _, p := range c.ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		ports = append(ports, map[string]any{"IP": p.HostIP, "PrivatePort": p.ContainerPort, "PublicPort": p.HostPort, "Type": proto})
	}
	return map[string]any{
		"Id": c.id, "Names": []string{"/" + c.name}, "Image": c.image, "ImageID": c.imageID, "Command": strings.Join(append(append([]string{}, c.entrypoint...), c.cmd...), " "),
		"Created": c.created.Unix(), "Ports": ports, "Labels": c.labels, "State": c.state, "Status": c.status(),
		"HostConfig": map[string]string{"NetworkMode": c.network}, "NetworkSettings": map[string]any{"Networks": map[string]any{c.network: map[string]any{}}},
		"Mounts": []any{},
	}
}

// createContainer answers POST /containers/create.
func (f *Fake) createContainer(w http.ResponseWriter, r *http.Request, body []byte) {
	var spec containerapi.ContainerSpec
	if f.kind == containerapi.KindPodman {
		var in struct {
			Name         string            `json:"name"`
			Image        string            `json:"image"`
			Command      []string          `json:"command"`
			Entrypoint   []string          `json:"entrypoint"`
			Env          map[string]string `json:"env"`
			Labels       map[string]string `json:"labels"`
			WorkDir      string            `json:"work_dir"`
			User         string            `json:"user"`
			Terminal     bool              `json:"terminal"`
			Remove       bool              `json:"remove"`
			Pod          string            `json:"pod"`
			PortMappings []struct {
				HostIP        string `json:"host_ip"`
				ContainerPort uint16 `json:"container_port"`
				HostPort      uint16 `json:"host_port"`
				Protocol      string `json:"protocol"`
			} `json:"portmappings"`
			Mounts []struct {
				Source      string   `json:"source"`
				Destination string   `json:"destination"`
				Options     []string `json:"options"`
			} `json:"mounts"`
			Volumes []struct {
				Name    string   `json:"Name"`
				Dest    string   `json:"Dest"`
				Options []string `json:"Options"`
			} `json:"volumes"`
			Networks map[string]any `json:"networks"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			f.writeError(w, http.StatusBadRequest, "decode(): "+err.Error())
			return
		}
		spec = containerapi.ContainerSpec{Name: in.Name, Image: in.Image, Cmd: in.Command, Entrypoint: in.Entrypoint, Labels: in.Labels, WorkingDir: in.WorkDir, User: in.User, Tty: in.Terminal, AutoRemove: in.Remove, Pod: in.Pod}
		for k, v := range in.Env {
			spec.Env = append(spec.Env, k+"="+v)
		}
		sort.Strings(spec.Env)
		for _, p := range in.PortMappings {
			spec.Ports = append(spec.Ports, containerapi.PortMapping{HostIP: p.HostIP, HostPort: p.HostPort, ContainerPort: p.ContainerPort, Protocol: p.Protocol})
		}
		for _, m := range in.Mounts {
			spec.Binds = append(spec.Binds, bindOf(m.Source, m.Destination, m.Options))
		}
		for _, v := range in.Volumes {
			spec.Binds = append(spec.Binds, bindOf(v.Name, v.Dest, v.Options))
		}
		for n := range in.Networks {
			spec.Network = n
		}
	} else {
		var in struct {
			Image      string            `json:"Image"`
			Cmd        []string          `json:"Cmd"`
			Entrypoint []string          `json:"Entrypoint"`
			Env        []string          `json:"Env"`
			Labels     map[string]string `json:"Labels"`
			WorkingDir string            `json:"WorkingDir"`
			User       string            `json:"User"`
			Tty        bool              `json:"Tty"`
			HostConfig struct {
				PortBindings map[string][]struct{ HostIp, HostPort string } `json:"PortBindings"`
				Binds        []string                                       `json:"Binds"`
				AutoRemove   bool                                           `json:"AutoRemove"`
				NetworkMode  string                                         `json:"NetworkMode"`
			} `json:"HostConfig"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			f.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		spec = containerapi.ContainerSpec{Name: r.URL.Query().Get("name"), Image: in.Image, Cmd: in.Cmd, Entrypoint: in.Entrypoint, Env: in.Env, Labels: in.Labels, WorkingDir: in.WorkingDir, User: in.User, Tty: in.Tty, Binds: in.HostConfig.Binds, AutoRemove: in.HostConfig.AutoRemove, Network: in.HostConfig.NetworkMode}
		keys := make([]string, 0, len(in.HostConfig.PortBindings))
		for k := range in.HostConfig.PortBindings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			port, proto, _ := strings.Cut(k, "/")
			n, _ := strconv.Atoi(port)
			for _, b := range in.HostConfig.PortBindings[k] {
				hp, _ := strconv.Atoi(b.HostPort)
				spec.Ports = append(spec.Ports, containerapi.PortMapping{HostIP: b.HostIp, HostPort: uint16(hp), ContainerPort: uint16(n), Protocol: proto})
			}
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if spec.Image == "" {
		f.writeError(w, http.StatusBadRequest, "no image given")
		return
	}
	if f.findImageLocked(spec.Image) == nil {
		if f.kind == containerapi.KindPodman {
			f.writeError(w, http.StatusNotFound, "failed to find image "+spec.Image+": "+spec.Image+": image not known")
		} else {
			f.writeError(w, http.StatusNotFound, "No such image: "+spec.Image)
		}
		return
	}
	if spec.Name != "" {
		for _, other := range f.containers {
			if other.name == spec.Name {
				if f.kind == containerapi.KindPodman {
					f.writeError(w, http.StatusConflict, "creating container storage: the container name \""+spec.Name+"\" is already in use by "+other.id+". You have to remove that container to be able to reuse that name: that name is already in use")
				} else {
					f.writeError(w, http.StatusConflict, "Conflict. The container name \"/"+spec.Name+"\" is already in use by container \""+other.id+"\". You have to remove (or rename) that container to be able to reuse that name.")
				}
				return
			}
		}
	}
	if spec.Pod != "" && f.pods[f.podKeyLocked(spec.Pod)] == nil {
		f.writeError(w, http.StatusNotFound, "no pod with name or ID "+spec.Pod+" found: no such pod")
		return
	}
	c := f.newContainerLocked(spec.Name, spec.Image, spec.Labels)
	c.cmd, c.entrypoint, c.env, c.binds, c.tty, c.autoRemove, c.user, c.wd = spec.Cmd, spec.Entrypoint, spec.Env, spec.Binds, spec.Tty, spec.AutoRemove, spec.User, spec.WorkingDir
	if spec.Network != "" {
		c.network = spec.Network
	}
	if spec.Pod != "" {
		c.pod = f.podKeyLocked(spec.Pod)
	}
	for _, p := range spec.Ports {
		if p.HostPort == 0 {
			f.nextPort++
			p.HostPort = uint16(f.nextPort)
		}
		c.ports = append(c.ports, p)
	}
	f.emitLocked("container", "create", c.id, f.containerAttrs(c))
	writeJSON(w, http.StatusCreated, map[string]any{"Id": c.id, "Warnings": []string{}})
}

// bindOf writes a mount as the "source:destination[:options]" of Docker's Binds.
func bindOf(source, dest string, options []string) string {
	bind := source + ":" + dest
	if len(options) > 0 {
		bind += ":" + strings.Join(options, ",")
	}
	return bind
}

// containerAttrs are the attributes of a container's events: its labels, name and image.
func (f *Fake) containerAttrs(c *fakeContainer) map[string]string {
	attrs := map[string]string{"name": c.name, "image": c.image}
	for k, v := range c.labels {
		attrs[k] = v
	}
	return attrs
}

// inspectContainer answers GET /containers/{id}/json.
func (f *Fake) inspectContainer(w http.ResponseWriter, c *fakeContainer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := map[string]any{
		"Status": c.state, "Running": c.state == "running" || c.state == "paused", "Paused": c.state == "paused", "Restarting": false,
		"OOMKilled": false, "Dead": false, "Pid": 0, "ExitCode": c.exitCode, "Error": "",
		"StartedAt": rfc(c.started), "FinishedAt": rfc(c.finished),
	}
	if c.state == "running" {
		state["Pid"] = 4242
	}
	mounts := []map[string]any{}
	for _, b := range c.binds {
		parts := strings.Split(b, ":")
		if len(parts) >= 2 {
			mounts = append(mounts, map[string]any{"Type": "bind", "Source": parts[0], "Destination": parts[1], "RW": len(parts) < 3 || !strings.Contains(","+parts[2]+",", ",ro,")})
		}
	}
	cfg := map[string]any{"Env": c.env, "Cmd": c.cmd, "Entrypoint": nil, "Labels": c.labels, "Tty": c.tty, "Image": c.image, "Hostname": c.id[:12], "WorkingDir": c.wd, "User": c.user}
	out := map[string]any{
		"Id": c.id, "Created": c.created.Format(time.RFC3339Nano), "Image": c.imageID, "State": state, "Config": cfg, "Mounts": mounts,
		"NetworkSettings": map[string]any{"Ports": c.portStrings(), "Networks": map[string]any{c.network: map[string]any{}}},
		"RestartCount":    0,
	}
	if f.kind == containerapi.KindPodman {
		out["Name"] = c.name
		out["ImageName"] = c.image
		out["Pod"] = c.pod
		cfg["Entrypoint"] = strings.Join(c.entrypoint, " ")
	} else {
		out["Name"] = "/" + c.name
	}
	writeJSON(w, http.StatusOK, out)
}

// rfc formats a time the way the engines do, "0001-01-01T00:00:00Z" for none.
func rfc(t time.Time) string {
	if t.IsZero() {
		return "0001-01-01T00:00:00Z"
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// containerAction handles POST /containers/{id}/start|stop|restart|kill|pause|unpause|rename.
func (f *Fake) containerAction(w http.ResponseWriter, r *http.Request, c *fakeContainer, action string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch action {
	case "start":
		if c.state == "running" || c.state == "paused" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		f.startLocked(c)
	case "stop":
		if c.state != "running" && c.state != "paused" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		f.stopLocked(c, 0, "stop")
	case "restart":
		if c.state == "running" || c.state == "paused" {
			f.stopLocked(c, 0, "")
		}
		f.startLocked(c)
		f.emitLocked("container", "restart", c.id, f.containerAttrs(c))
	case "kill":
		if c.state != "running" && c.state != "paused" {
			f.writeError(w, http.StatusConflict, "Container "+c.id+" is not running")
			return
		}
		f.emitLocked("container", "kill", c.id, f.containerAttrs(c))
		f.stopLocked(c, 137, "")
	case "pause":
		if c.state != "running" {
			f.writeError(w, http.StatusConflict, "Container "+c.id+" is not running")
			return
		}
		c.state = "paused"
		f.emitLocked("container", "pause", c.id, f.containerAttrs(c))
	case "unpause":
		if c.state != "paused" {
			f.writeError(w, http.StatusConflict, "Container "+c.id+" is not paused")
			return
		}
		c.state = "running"
		f.emitLocked("container", "unpause", c.id, f.containerAttrs(c))
	case "rename":
		name := r.URL.Query().Get("name")
		for _, other := range f.containers {
			if other != c && other.name == name {
				f.writeError(w, http.StatusConflict, "Conflict. The container name \"/"+name+"\" is already in use by container \""+other.id+"\".")
				return
			}
		}
		old := c.name
		c.name = name
		attrs := f.containerAttrs(c)
		attrs["oldName"] = old
		f.emitLocked("container", "rename", c.id, attrs)
	default:
		f.writeError(w, http.StatusNotFound, "page not found")
		return
	}
	noContent(w)
}

// startLocked starts a container: its script prints, and a script with "while true" keeps printing "tick".
func (f *Fake) startLocked(c *fakeContainer) {
	c.state = "running"
	c.started = time.Now()
	c.exitCode = 0
	f.emitLocked("container", "start", c.id, f.containerAttrs(c))
	out := runScript(c.cmd)
	for _, l := range out.lines {
		s := containerapi.StreamStdout
		if l.stderr {
			s = containerapi.StreamStderr
		}
		c.logs = append(c.logs, logEntry{stream: s, text: l.text + "\n", at: time.Now()})
	}
	c.notify()
	if out.ticks {
		stop := make(chan struct{})
		c.stopTick = stop
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			t := time.NewTicker(25 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-f.stop:
					return
				case <-t.C:
					f.Log(c.id, containerapi.StreamStdout, "tick")
				}
			}
		}()
	}
}

// stopLocked ends a container's run: the state, the exit code, the events (die and, with stopEvent, stop), and a container
// made with AutoRemove goes.
func (f *Fake) stopLocked(c *fakeContainer, code int, stopEvent string) {
	if c.stopTick != nil {
		close(c.stopTick)
		c.stopTick = nil
	}
	c.state = "exited"
	c.exitCode = code
	c.finished = time.Now()
	die := "die"
	if f.kind == containerapi.KindPodman {
		die = "died"
	}
	attrs := f.containerAttrs(c)
	attrs["exitCode"] = strconv.Itoa(code)
	f.emitLocked("container", die, c.id, attrs)
	if stopEvent != "" {
		f.emitLocked("container", stopEvent, c.id, f.containerAttrs(c))
	}
	c.notify()
	if c.autoRemove {
		f.deleteContainerLocked(c)
	}
}

// deleteContainerLocked removes a container.
func (f *Fake) deleteContainerLocked(c *fakeContainer) {
	delete(f.containers, c.id)
	action := "destroy"
	if f.kind == containerapi.KindPodman {
		action = "remove"
	}
	f.emitLocked("container", action, c.id, f.containerAttrs(c))
}

// removeContainer answers DELETE /containers/{id}.
func (f *Fake) removeContainer(w http.ResponseWriter, r *http.Request, ref string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.lookupLocked(ref)
	if c == nil {
		f.notFoundContainer(w, ref)
		return
	}
	if c.state == "running" || c.state == "paused" {
		if !boolParam(r, "force") {
			if f.kind == containerapi.KindPodman {
				f.writeError(w, http.StatusInternalServerError, "cannot remove container "+c.id+" as it is running - running or paused containers cannot be removed without force: container state improper")
			} else {
				f.writeError(w, http.StatusConflict, "cannot remove container \""+c.name+"\": container is running: stop the container before removing or force remove")
			}
			return
		}
		f.stopLocked(c, 137, "")
	}
	if _, still := f.containers[c.id]; still {
		f.deleteContainerLocked(c)
	}
	if f.kind == containerapi.KindPodman {
		writeJSON(w, http.StatusOK, []map[string]any{{"Id": c.id, "Err": nil}})
		return
	}
	noContent(w)
}

// pruneContainers answers POST /containers/prune.
func (f *Fake) pruneContainers(w http.ResponseWriter, r *http.Request) {
	filters := parseFilters(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	var gone []string
	for _, c := range f.containers {
		if c.state == "running" || c.state == "paused" || !matchLabels(c.labels, filters["label"]) {
			continue
		}
		gone = append(gone, c.id)
	}
	sort.Strings(gone)
	for _, id := range gone {
		f.deleteContainerLocked(f.containers[id])
	}
	if f.kind == containerapi.KindPodman {
		reports := make([]map[string]any, 0, len(gone))
		for _, id := range gone {
			reports = append(reports, map[string]any{"Id": id, "Err": nil, "Size": 100})
		}
		writeJSON(w, http.StatusOK, reports)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ContainersDeleted": gone, "SpaceReclaimed": 100 * len(gone)})
}

// logsStream answers GET /containers/{id}/logs with the multiplexed stream (raw for a container with a terminal).
func (f *Fake) logsStream(w http.ResponseWriter, r *http.Request, c *fakeContainer) {
	q := r.URL.Query()
	wantOut, wantErr := boolParam(r, "stdout"), boolParam(r, "stderr")
	follow, stamps := boolParam(r, "follow"), boolParam(r, "timestamps")
	tail := -1
	if t, err := strconv.Atoi(q.Get("tail")); err == nil {
		tail = t
	}
	w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
	w.WriteHeader(http.StatusOK)
	flush(w)
	sent := 0
	first := true
	for {
		f.mu.Lock()
		entries := append([]logEntry(nil), c.logs[sent:]...)
		running := c.state == "running" || c.state == "paused"
		changed := c.changed
		tty := c.tty
		f.mu.Unlock()
		if first && tail >= 0 {
			// only the last tail lines of what is there
			var kept []logEntry
			for _, e := range entries {
				if (e.stream == containerapi.StreamStdout && wantOut) || (e.stream == containerapi.StreamStderr && wantErr) {
					kept = append(kept, e)
				}
			}
			if len(kept) > tail {
				kept = kept[len(kept)-tail:]
			}
			sent += len(entries)
			entries = kept
			for _, e := range entries {
				writeFrame(w, e, stamps, tty)
			}
			entries = nil
		}
		first = false
		for _, e := range entries {
			sent++
			if (e.stream == containerapi.StreamStdout && !wantOut) || (e.stream == containerapi.StreamStderr && !wantErr) {
				continue
			}
			writeFrame(w, e, stamps, tty)
		}
		flush(w)
		if !follow || !running {
			return
		}
		select {
		case <-changed:
		case <-r.Context().Done():
			return
		case <-f.stop:
			return
		}
	}
}

// writeFrame writes a log line as a frame of the multiplexed stream, or raw for a terminal.
func writeFrame(w http.ResponseWriter, e logEntry, stamps, tty bool) {
	text := e.text
	if stamps {
		text = e.at.UTC().Format(time.RFC3339Nano) + " " + text
	}
	if tty {
		_, _ = w.Write([]byte(text))
		return
	}
	var header [8]byte
	header[0] = byte(e.stream)
	binary.BigEndian.PutUint32(header[4:], uint32(len(text)))
	_, _ = w.Write(header[:])
	_, _ = w.Write([]byte(text))
}

// flush sends what was written so far.
func flush(w http.ResponseWriter) {
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
}

// createExec answers POST /containers/{id}/exec.
func (f *Fake) createExec(w http.ResponseWriter, c *fakeContainer, body []byte) {
	var in struct {
		Cmd []string `json:"Cmd"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		f.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.state != "running" {
		f.writeError(w, http.StatusConflict, "container "+c.id+" is not running")
		return
	}
	e := &fakeExec{id: f.newID("exec"), container: c.id, cmd: in.Cmd}
	f.execs[e.id] = e
	writeJSON(w, http.StatusCreated, map[string]string{"Id": e.id})
}

// execRoute handles /exec/{id}/start and /exec/{id}/json.
func (f *Fake) execRoute(w http.ResponseWriter, r *http.Request, rest string, _ []byte) {
	id, action, _ := strings.Cut(rest, "/")
	f.mu.Lock()
	e := f.execs[id]
	fn := f.execFunc
	f.mu.Unlock()
	if e == nil {
		f.writeError(w, http.StatusNotFound, "No such exec instance: "+id)
		return
	}
	switch {
	case action == "start" && r.Method == http.MethodPost:
		var stdout, stderr string
		var code int
		if fn != nil {
			stdout, stderr, code = fn(e.cmd)
		} else {
			out := runScript(e.cmd)
			for _, l := range out.lines {
				if l.stderr {
					stderr += l.text + "\n"
				} else {
					stdout += l.text + "\n"
				}
			}
			code = out.exit
		}
		w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
		w.WriteHeader(http.StatusOK)
		for _, part := range []struct {
			s    containerapi.Stream
			text string
		}{{containerapi.StreamStdout, stdout}, {containerapi.StreamStderr, stderr}} {
			if part.text == "" {
				continue
			}
			var header [8]byte
			header[0] = byte(part.s)
			binary.BigEndian.PutUint32(header[4:], uint32(len(part.text)))
			_, _ = w.Write(header[:])
			_, _ = w.Write([]byte(part.text))
		}
		flush(w)
		f.mu.Lock()
		e.started, e.done, e.exit = true, true, code
		f.mu.Unlock()
	case action == "json" && r.Method == http.MethodGet:
		f.mu.Lock()
		out := map[string]any{"ID": e.id, "ContainerID": e.container, "Running": e.started && !e.done, "ExitCode": e.exit}
		f.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
	default:
		f.writeError(w, http.StatusNotFound, "page not found")
	}
}
